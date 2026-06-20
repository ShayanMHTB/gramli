package web

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

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
	Name    string
	Slug    string
	Count   int
	ThumbID int64
	Remote  string
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
}

// PostDetail is the full view of a single post.
type PostDetail struct {
	ID          int64
	Shortcode   string
	PostURL     string
	Owner       string
	Caption     string
	MediaType   string
	Source      string
	Media       []PostMedia
	Collections []string
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
		where = append(where, "(p.caption LIKE ? OR p.owner_username LIKE ? OR p.shortcode LIKE ?)")
		s := "%" + q.Search + "%"
		args = append(args, s, s, s)
	}
	return where, args
}

func loadCollections(ctx context.Context, db *sql.DB) ([]CollectionItem, error) {
	rows, err := db.QueryContext(ctx, `
SELECT c.name, c.slug,
  (SELECT COUNT(*) FROM post_collections pc WHERE pc.collection_id=c.id),
  (SELECT m.id FROM media m JOIN post_collections pc ON pc.post_id=m.post_id WHERE pc.collection_id=c.id ORDER BY m.media_index LIMIT 1),
  (SELECT COALESCE(p.thumbnail_url,'') FROM posts p JOIN post_collections pc ON pc.post_id=p.id WHERE pc.collection_id=c.id LIMIT 1)
FROM collections c ORDER BY c.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CollectionItem
	for rows.Next() {
		var ci CollectionItem
		var thumb sql.NullInt64
		if err := rows.Scan(&ci.Name, &ci.Slug, &ci.Count, &thumb, &ci.Remote); err != nil {
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

func loadPostDetail(ctx context.Context, db *sql.DB, shortcode string) (PostDetail, error) {
	var d PostDetail
	err := db.QueryRowContext(ctx, `
SELECT id, shortcode, post_url, COALESCE(owner_username,''), COALESCE(caption,''), COALESCE(NULLIF(media_type,''),'unknown'), COALESCE(source,'')
FROM posts WHERE shortcode=?`, shortcode).Scan(&d.ID, &d.Shortcode, &d.PostURL, &d.Owner, &d.Caption, &d.MediaType, &d.Source)
	if err != nil {
		return PostDetail{}, err
	}
	rows, err := db.QueryContext(ctx, `
SELECT id, media_index, COALESCE(media_type,''), COALESCE(remote_url,''), COALESCE(local_path,''), COALESCE(thumbnail_url,''), download_status
FROM media WHERE post_id=? ORDER BY media_index`, d.ID)
	if err != nil {
		return PostDetail{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var m PostMedia
		if err := rows.Scan(&m.ID, &m.Index, &m.Type, &m.RemoteURL, &m.LocalPath, &m.Thumbnail, &m.Status); err != nil {
			return PostDetail{}, err
		}
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
