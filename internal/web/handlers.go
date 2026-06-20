package web

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/shayanmahtabi/gramli/internal/accounts"
)

type pageData struct {
	Title  string
	Active string // nav highlight
	Data   any
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	stats, err := loadStats(r.Context(), s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	acc, _ := accounts.Get(r.Context(), s.db, "")
	s.render(w, "dashboard", pageData{
		Title:  "Dashboard",
		Active: "dashboard",
		Data:   map[string]any{"Stats": stats, "Account": acc},
	})
}

func (s *Server) handleGallery(w http.ResponseWriter, r *http.Request) {
	q := parseGalleryQuery(r)
	items, total, err := loadGallery(r.Context(), s.db, q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := s.galleryData(r, q, items, total)
	// htmx infinite-scroll / filter requests get just the grid partial.
	if isHTMX(r) {
		s.renderPartial(w, "gallery-grid", data)
		return
	}
	owners, _ := loadOwners(r.Context(), s.db)
	collections, _ := loadCollections(r.Context(), s.db)
	data["Owners"] = owners
	data["Collections"] = collections
	s.render(w, "gallery", pageData{Title: "Gallery", Active: "gallery", Data: data})
}

func (s *Server) galleryData(r *http.Request, q GalleryQuery, items []GalleryItem, total int) map[string]any {
	nextOffset := q.Offset + q.Limit
	hasMore := nextOffset < total
	return map[string]any{
		"Query":          q,
		"Items":          items,
		"Total":          total,
		"NextOffset":     nextOffset,
		"HasMore":        hasMore,
		"RemoteFallback": s.remoteFallback,
	}
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request) {
	shortcode := r.PathValue("shortcode")
	d, err := loadPostDetail(r.Context(), s.db, shortcode)
	if err != nil {
		http.Error(w, "post not found", http.StatusNotFound)
		return
	}
	data := map[string]any{"Post": d, "RemoteFallback": s.remoteFallback}
	if isHTMX(r) {
		s.renderPartial(w, "post-detail", data)
		return
	}
	s.render(w, "post", pageData{Title: "@" + d.Owner + " · " + d.Shortcode, Active: "gallery", Data: data})
}

func (s *Server) handleCollections(w http.ResponseWriter, r *http.Request) {
	cols, err := loadCollections(r.Context(), s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "collections", pageData{Title: "Collections", Active: "collections", Data: map[string]any{"Collections": cols, "RemoteFallback": s.remoteFallback}})
}

func (s *Server) handleOwners(w http.ResponseWriter, r *http.Request) {
	owners, err := loadOwnerStats(r.Context(), s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "owners", pageData{Title: "Creators", Active: "owners", Data: map[string]any{"Owners": owners}})
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	acc, err := accounts.Get(r.Context(), s.db, "")
	if err != nil {
		s.render(w, "account", pageData{Title: "Account", Active: "account", Data: map[string]any{"Error": err.Error()}})
		return
	}
	s.render(w, "account", pageData{Title: "Account", Active: "account", Data: map[string]any{"Account": acc}})
}

func (s *Server) handleDownloads(w http.ResponseWriter, r *http.Request) {
	stats, err := loadStats(r.Context(), s.db)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "downloads", pageData{Title: "Downloads", Active: "downloads", Data: map[string]any{"Stats": stats}})
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	q := parseGalleryQuery(r)
	rows, err := loadGalleryExport(r.Context(), s.db, q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	switch r.URL.Query().Get("format") {
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="gramli-export.csv"`)
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"shortcode", "owner", "type", "downloaded", "url", "caption"})
		for _, row := range rows {
			_ = cw.Write([]string{row.Shortcode, row.Owner, row.MediaType, strconv.FormatBool(row.Downloaded), row.PostURL, row.Caption})
		}
		cw.Flush()
	default:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="gramli-export.json"`)
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rows)
	}
}

func parseGalleryQuery(r *http.Request) GalleryQuery {
	v := r.URL.Query()
	offset, _ := strconv.Atoi(v.Get("offset"))
	if offset < 0 {
		offset = 0
	}
	limit, _ := strconv.Atoi(v.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	return GalleryQuery{
		Collection: v.Get("collection"),
		Owner:      v.Get("owner"),
		MediaType:  v.Get("type"),
		Status:     v.Get("status"),
		Search:     v.Get("q"),
		Sort:       v.Get("sort"),
		Order:      v.Get("order"),
		Limit:      limit,
		Offset:     offset,
	}
}
