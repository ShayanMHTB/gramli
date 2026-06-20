// Package web serves a local, read-only browser UI over the Gramli SQLite
// archive and downloaded media. Everything is embedded into the binary; the
// page makes no external requests except optional remote thumbnail fallbacks.
package web

import (
	"database/sql"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
)

// Server holds the dependencies for the web UI.
type Server struct {
	db             *sql.DB
	dataDir        string
	downloadsDir   string
	thumbCacheDir  string
	remoteFallback bool
	pages          map[string]*template.Template
	partials       *template.Template
}

// Options configures a Server.
type Options struct {
	DataDir        string
	RemoteFallback bool
}

// New builds a Server, parsing all embedded templates up front.
func New(db *sql.DB, opt Options) (*Server, error) {
	s := &Server{
		db:             db,
		dataDir:        opt.DataDir,
		downloadsDir:   filepath.Join(opt.DataDir, "downloads"),
		thumbCacheDir:  filepath.Join(opt.DataDir, "cache", "thumbs"),
		remoteFallback: opt.RemoteFallback,
	}
	if err := s.parseTemplates(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) parseTemplates() error {
	base := template.New("").Funcs(funcMap())
	base, err := base.ParseFS(templateFS, "templates/layout.html", "templates/partials/*.html")
	if err != nil {
		return fmt.Errorf("parse base templates: %w", err)
	}
	s.partials = base

	pageFiles, err := fs.Glob(templateFS, "templates/pages/*.html")
	if err != nil {
		return err
	}
	s.pages = make(map[string]*template.Template, len(pageFiles))
	for _, pf := range pageFiles {
		name := strings.TrimSuffix(filepath.Base(pf), ".html")
		clone, err := base.Clone()
		if err != nil {
			return err
		}
		if _, err := clone.ParseFS(templateFS, pf); err != nil {
			return fmt.Errorf("parse page %s: %w", name, err)
		}
		s.pages[name] = clone
	}
	return nil
}

// Handler returns the configured HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	staticSub, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	mux.HandleFunc("GET /{$}", s.handleDashboard)
	mux.HandleFunc("GET /gallery", s.handleGallery)
	mux.HandleFunc("GET /post/{shortcode}", s.handlePost)
	mux.HandleFunc("GET /collections", s.handleCollections)
	mux.HandleFunc("GET /owners", s.handleOwners)
	mux.HandleFunc("GET /account", s.handleAccount)
	mux.HandleFunc("GET /downloads", s.handleDownloads)
	mux.HandleFunc("GET /media/{id}", s.handleMedia)
	mux.HandleFunc("GET /thumb/{id}", s.handleThumb)
	return mux
}

// render executes a full page (layout + page content).
func (s *Server) render(w http.ResponseWriter, page string, data any) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "unknown page: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderPartial executes a named partial template (for htmx swaps).
func (s *Server) renderPartial(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.partials.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}
