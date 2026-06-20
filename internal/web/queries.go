package web

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

// parseTime tolerates the several datetime serializations SQLite may hold.
func parseTime(value string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}
	return time.Time{}
}

// Stats is the dashboard summary of the local archive.
type Stats struct {
	Posts        int
	Media        int
	Downloaded   int
	Pending      int
	Failed       int
	Missing      int
	Collections  int
	Owners       int
	StorageBytes int64
	MediaTypes   []Bucket
	DownloadMix  []Bucket
	TopOwners    []Bucket
	Timeline     []Bucket
}

// Bucket is a generic labelled count used for breakdowns and charts.
type Bucket struct {
	Label string
	Count int
}

// GalleryItem is one card in the gallery grid.
type GalleryItem struct {
	PostID     int64
	Shortcode  string
	Owner      string
	MediaType  string
	MediaCount int
	Downloaded bool
	ThumbID    int64  // representative media row id (for /thumb/{id})
	RemoteThmb string // remote thumbnail fallback
}

// GalleryQuery captures the gallery's filter/sort/paging state.
type GalleryQuery struct {
	Collection string
	Owner      string
	MediaType  string
	Status     string // downloaded | pending | missing | any
	Search     string
	Sort       string // discovered_at | saved_at | taken_at | owner
	Order      string // asc | desc
	Limit      int
	Offset     int
}

// CollectionItem is a collection card on the collections page.
type CollectionItem struct {
	Name       string
	Slug       string
	Count      int
	ThumbID    int64
	Remote     string
	Downloaded int
	Storage    int64
}

// PostMedia is one media row in the post detail view.
type PostMedia struct {
	ID         int64
	Index      int
	Type       string
	RemoteURL  string
	LocalPath  string
	Thumbnail  string
	Status     string
	Downloaded bool
	Width      int64
	Height     int64
	Duration   float64
	FileSize   int64
}

// PostDetail is the full view of a single post.
type PostDetail struct {
	ID           int64
	Shortcode    string
	PostURL      string
	Owner        string
	Caption      string
	MediaType    string
	Source       string
	TakenAt      *time.Time
	SavedAt      *time.Time
	LikeCount    *int64
	CommentCount *int64
	RawJSONPath  string
	Media        []PostMedia
	Collections  []string
}

func loadStats(ctx context.Context, db *sql.DB) (Stats, error) {
	var s Stats
	s.Posts = scalarInt(ctx, db, `SELECT COUNT(*) FROM posts`)
	s.Media = scalarInt(ctx, db, `SELECT COUNT(*) FROM media`)
	s.Downloaded = scalarInt(ctx, db, `SELECT COUNT(*) FROM media WHERE download_status='downloaded'`)
	s.Pending = scalarInt(ctx, db, `SELECT COUNT(*) FROM media WHERE download_status='pending'`)
	s.Failed = scalarInt(ctx, db, `SELECT COUNT(*) FROM media WHERE download_status='failed'`)
	s.Missing = scalarInt(ctx, db, `SELECT COUNT(*) FROM media WHERE download_status='missing'`)
	s.Collections = scalarInt(ctx, db, `SELECT COUNT(*) FROM collections`)
	s.Owners = scalarInt(ctx, db, `SELECT COUNT(DISTINCT owner_username) FROM posts WHERE owner_username IS NOT NULL AND owner_username<>''`)
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(SUM(file_size_bytes),0) FROM media WHERE download_status='downloaded'`).Scan(&s.StorageBytes)

	s.MediaTypes = buckets(ctx, db, `SELECT COALESCE(NULLIF(media_type,''),'unknown') AS k, COUNT(*) FROM posts GROUP BY k ORDER BY COUNT(*) DESC`)
	s.DownloadMix = buckets(ctx, db, `SELECT download_status, COUNT(*) FROM media GROUP BY download_status ORDER BY COUNT(*) DESC`)
	s.TopOwners = buckets(ctx, db, `SELECT owner_username, COUNT(*) FROM posts WHERE owner_username IS NOT NULL AND owner_username<>'' GROUP BY owner_username ORDER BY COUNT(*) DESC LIMIT 10`)
	// Bucket by month using a substring of the stored timestamp (YYYY-MM),
	// which is robust regardless of the exact datetime serialization.
	s.Timeline = buckets(ctx, db, `SELECT substr(COALESCE(saved_at, taken_at, discovered_at),1,7) AS k, COUNT(*) FROM posts WHERE k IS NOT NULL AND k<>'' GROUP BY k ORDER BY k ASC LIMIT 24`)
	return s, nil
}

func loadGallery(ctx context.Context, db *sql.DB, q GalleryQuery) ([]GalleryItem, int, error) {
	where, args := galleryWhere(q)
	total := scalarInt(ctx, db, `SELECT COUNT(*) FROM posts p WHERE `+strings.Join(where, " AND "), args...)

	sort := map[string]string{
		"discovered_at": "p.discovered_at",
		"saved_at":      "p.saved_at",
		"taken_at":      "p.taken_at",
		"owner":         "p.owner_username",
	}[q.Sort]
	if sort == "" {
		sort = "p.discovered_at"
	}
	order := "DESC"
	if strings.EqualFold(q.Order, "asc") {
		order = "ASC"
	}
	if q.Limit <= 0 {
		q.Limit = 60
	}
	query := fmt.Sprintf(`
SELECT p.id, p.shortcode, COALESCE(p.owner_username,''), COALESCE(NULLIF(p.media_type,''),'unknown'),
  COALESCE(p.thumbnail_url,''),
  (SELECT COUNT(*) FROM media m WHERE m.post_id=p.id),
  (SELECT m.id FROM media m WHERE m.post_id=p.id ORDER BY m.media_index LIMIT 1),
  (SELECT COALESCE(m.thumbnail_url,m.remote_url,'') FROM media m WHERE m.post_id=p.id ORDER BY m.media_index LIMIT 1),
  EXISTS(SELECT 1 FROM media m WHERE m.post_id=p.id AND m.download_status='downloaded')
FROM posts p
WHERE %s
ORDER BY %s %s
LIMIT ? OFFSET ?`, strings.Join(where, " AND "), sort, order)
	args = append(args, q.Limit, q.Offset)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []GalleryItem
	for rows.Next() {
		var it GalleryItem
		var thumbID sql.NullInt64
		var mediaThumb string
		if err := rows.Scan(&it.PostID, &it.Shortcode, &it.Owner, &it.MediaType, &it.RemoteThmb, &it.MediaCount, &thumbID, &mediaThumb, &it.Downloaded); err != nil {
			return nil, 0, err
		}
		it.ThumbID = thumbID.Int64
		if it.RemoteThmb == "" {
			it.RemoteThmb = mediaThumb
		}
		out = append(out, it)
	}
	return out, total, rows.Err()
}

func galleryWhere(q GalleryQuery) ([]string, []any) {
	where := []string{"1=1"}
	var args []any
	if q.Collection != "" {
		where = append(where, `EXISTS (SELECT 1 FROM post_collections pc JOIN collections c ON c.id=pc.collection_id WHERE pc.post_id=p.id AND (c.slug=? OR c.name=?))`)
		args = append(args, q.Collection, q.Collection)
	}
	if q.Owner != "" {
		where = append(where, "p.owner_username=?")
		args = append(args, q.Owner)
	}
	if q.MediaType != "" && q.MediaType != "any" {
		where = append(where, "p.media_type=?")
		args = append(args, q.MediaType)
	}
	switch q.Status {
	case "downloaded":
		where = append(where, `EXISTS (SELECT 1 FROM media m WHERE m.post_id=p.id AND m.download_status='downloaded')`)
	case "pending":
		where = append(where, `EXISTS (SELECT 1 FROM media m WHERE m.post_id=p.id AND m.download_status='pending')`)
	case "missing":
		where = append(where, `EXISTS (SELECT 1 FROM media m WHERE m.post_id=p.id AND m.download_status='missing')`)
	}
	if q.Search != "" {
		if match, ok := ftsMatch(q.Search); ok {
			where = append(where, "p.id IN (SELECT rowid FROM posts_fts WHERE posts_fts MATCH ?)")
			args = append(args, match)
		} else {
			where = append(where, "(p.caption LIKE ? OR p.owner_username LIKE ? OR p.shortcode LIKE ?)")
			s := "%" + q.Search + "%"
			args = append(args, s, s, s)
		}
	}
	return where, args
}

// ftsMatch turns free-form user input into a safe FTS5 prefix query
// ("sunset beach" -> "sunset* beach*"). Returns false when nothing usable
// remains, so the caller can fall back to LIKE.
func ftsMatch(search string) (string, bool) {
	var terms []string
	for _, field := range strings.Fields(search) {
		var b strings.Builder
		for _, r := range field {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		if b.Len() > 0 {
			terms = append(terms, b.String()+"*")
		}
	}
	if len(terms) == 0 {
		return "", false
	}
	return strings.Join(terms, " "), true
}

// ExportRow is one post in an exported gallery view.
type ExportRow struct {
	Shortcode  string `json:"shortcode"`
	Owner      string `json:"owner"`
	MediaType  string `json:"mediaType"`
	Downloaded bool   `json:"downloaded"`
	PostURL    string `json:"postUrl"`
	Caption    string `json:"caption"`
}

// loadGalleryExport returns every post matching the current gallery filters
// (no paging), for download as JSON/CSV.
func loadGalleryExport(ctx context.Context, db *sql.DB, q GalleryQuery) ([]ExportRow, error) {
	where, args := galleryWhere(q)
	query := `
SELECT p.shortcode, COALESCE(p.owner_username,''), COALESCE(NULLIF(p.media_type,''),'unknown'), p.post_url, COALESCE(p.caption,''),
  EXISTS(SELECT 1 FROM media m WHERE m.post_id=p.id AND m.download_status='downloaded')
FROM posts p WHERE ` + strings.Join(where, " AND ") + ` ORDER BY p.discovered_at DESC`
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExportRow
	for rows.Next() {
		var r ExportRow
		if err := rows.Scan(&r.Shortcode, &r.Owner, &r.MediaType, &r.PostURL, &r.Caption, &r.Downloaded); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadCollections(ctx context.Context, db *sql.DB) ([]CollectionItem, error) {
	rows, err := db.QueryContext(ctx, `
SELECT c.name, c.slug,
  (SELECT COUNT(*) FROM post_collections pc WHERE pc.collection_id=c.id),
  (SELECT m.id FROM media m JOIN post_collections pc ON pc.post_id=m.post_id WHERE pc.collection_id=c.id ORDER BY m.media_index LIMIT 1),
  (SELECT COALESCE(p.thumbnail_url,'') FROM posts p JOIN post_collections pc ON pc.post_id=p.id WHERE pc.collection_id=c.id LIMIT 1),
  (SELECT COUNT(DISTINCT m.post_id) FROM media m JOIN post_collections pc ON pc.post_id=m.post_id WHERE pc.collection_id=c.id AND m.download_status='downloaded'),
  COALESCE((SELECT SUM(m.file_size_bytes) FROM media m JOIN post_collections pc ON pc.post_id=m.post_id WHERE pc.collection_id=c.id AND m.download_status='downloaded'),0)
FROM collections c ORDER BY c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CollectionItem
	for rows.Next() {
		var ci CollectionItem
		var thumb sql.NullInt64
		if err := rows.Scan(&ci.Name, &ci.Slug, &ci.Count, &thumb, &ci.Remote, &ci.Downloaded, &ci.Storage); err != nil {
			return nil, err
		}
		ci.ThumbID = thumb.Int64
		out = append(out, ci)
	}
	return out, rows.Err()
}

func loadOwners(ctx context.Context, db *sql.DB) ([]Bucket, error) {
	return buckets(ctx, db, `SELECT owner_username, COUNT(*) FROM posts WHERE owner_username IS NOT NULL AND owner_username<>'' GROUP BY owner_username ORDER BY COUNT(*) DESC, owner_username ASC`), nil
}

// OwnerStat is a per-creator rollup for the creators page.
type OwnerStat struct {
	Owner      string
	Posts      int
	Images     int
	Videos     int
	Albums     int
	Downloaded int
	Storage    int64
}

func loadOwnerStats(ctx context.Context, db *sql.DB) ([]OwnerStat, error) {
	rows, err := db.QueryContext(ctx, `
SELECT owner_username, COUNT(*),
  SUM(CASE WHEN media_type='image' THEN 1 ELSE 0 END),
  SUM(CASE WHEN media_type='video' THEN 1 ELSE 0 END),
  SUM(CASE WHEN media_type='album' THEN 1 ELSE 0 END)
FROM posts WHERE owner_username IS NOT NULL AND owner_username<>''
GROUP BY owner_username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stats := map[string]*OwnerStat{}
	var order []string
	for rows.Next() {
		var s OwnerStat
		if err := rows.Scan(&s.Owner, &s.Posts, &s.Images, &s.Videos, &s.Albums); err != nil {
			return nil, err
		}
		cp := s
		stats[s.Owner] = &cp
		order = append(order, s.Owner)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	mrows, err := db.QueryContext(ctx, `
SELECT p.owner_username, COUNT(*), COALESCE(SUM(m.file_size_bytes),0)
FROM media m JOIN posts p ON p.id=m.post_id
WHERE m.download_status='downloaded' AND p.owner_username IS NOT NULL AND p.owner_username<>''
GROUP BY p.owner_username`)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var owner string
		var dl int
		var storage int64
		if err := mrows.Scan(&owner, &dl, &storage); err != nil {
			return nil, err
		}
		if s := stats[owner]; s != nil {
			s.Downloaded = dl
			s.Storage = storage
		}
	}
	out := make([]OwnerStat, 0, len(order))
	for _, o := range order {
		out = append(out, *stats[o])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Posts != out[j].Posts {
			return out[i].Posts > out[j].Posts
		}
		return out[i].Owner < out[j].Owner
	})
	return out, mrows.Err()
}

func loadPostDetail(ctx context.Context, db *sql.DB, shortcode string) (PostDetail, error) {
	var d PostDetail
	var taken, saved, rawPath sql.NullString
	var likes, comments sql.NullInt64
	err := db.QueryRowContext(ctx, `
SELECT id, shortcode, post_url, COALESCE(owner_username,''), COALESCE(caption,''), COALESCE(NULLIF(media_type,''),'unknown'), COALESCE(source,''),
  CAST(taken_at AS TEXT), CAST(saved_at AS TEXT), like_count, comment_count, COALESCE(raw_json_path,'')
FROM posts WHERE shortcode=?`, shortcode).Scan(&d.ID, &d.Shortcode, &d.PostURL, &d.Owner, &d.Caption, &d.MediaType, &d.Source,
		&taken, &saved, &likes, &comments, &rawPath)
	if err != nil {
		return PostDetail{}, err
	}
	if taken.Valid {
		if t := parseTime(taken.String); !t.IsZero() {
			d.TakenAt = &t
		}
	}
	if saved.Valid {
		if t := parseTime(saved.String); !t.IsZero() {
			d.SavedAt = &t
		}
	}
	if likes.Valid {
		v := likes.Int64
		d.LikeCount = &v
	}
	if comments.Valid {
		v := comments.Int64
		d.CommentCount = &v
	}
	d.RawJSONPath = rawPath.String

	rows, err := db.QueryContext(ctx, `
SELECT id, media_index, COALESCE(media_type,''), COALESCE(remote_url,''), COALESCE(local_path,''), COALESCE(thumbnail_url,''), download_status,
  width, height, duration_seconds, COALESCE(file_size_bytes,0)
FROM media WHERE post_id=? ORDER BY media_index`, d.ID)
	if err != nil {
		return PostDetail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var m PostMedia
		var w, h sql.NullInt64
		var dur sql.NullFloat64
		if err := rows.Scan(&m.ID, &m.Index, &m.Type, &m.RemoteURL, &m.LocalPath, &m.Thumbnail, &m.Status, &w, &h, &dur, &m.FileSize); err != nil {
			return PostDetail{}, err
		}
		m.Width, m.Height, m.Duration = w.Int64, h.Int64, dur.Float64
		m.Downloaded = m.Status == "downloaded"
		d.Media = append(d.Media, m)
	}
	if err := rows.Err(); err != nil {
		return PostDetail{}, err
	}
	crows, err := db.QueryContext(ctx, `
SELECT c.name FROM collections c JOIN post_collections pc ON pc.collection_id=c.id WHERE pc.post_id=? ORDER BY c.name`, d.ID)
	if err == nil {
		defer crows.Close()
		for crows.Next() {
			var name string
			if crows.Scan(&name) == nil {
				d.Collections = append(d.Collections, name)
			}
		}
	}
	return d, nil
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) int {
	var n int
	_ = db.QueryRowContext(ctx, query, args...).Scan(&n)
	return n
}

func buckets(ctx context.Context, db *sql.DB, query string, args ...any) []Bucket {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Bucket
	for rows.Next() {
		var b Bucket
		if err := rows.Scan(&b.Label, &b.Count); err != nil {
			return out
		}
		out = append(out, b)
	}
	return out
}
