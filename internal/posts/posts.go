package posts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Post struct {
	ID            int64      `json:"id"`
	Shortcode     string     `json:"shortcode"`
	PostURL       string     `json:"postUrl"`
	OwnerUsername string     `json:"ownerUsername,omitempty"`
	Caption       string     `json:"caption,omitempty"`
	MediaType     string     `json:"mediaType,omitempty"`
	IsVideo       bool       `json:"isVideo"`
	IsAlbum       bool       `json:"isAlbum"`
	DiscoveredAt  time.Time  `json:"discoveredAt"`
	LastSeenAt    time.Time  `json:"lastSeenAt"`
	Source        string     `json:"source"`
	Downloaded    bool       `json:"downloaded"`
	TakenAt       *time.Time `json:"takenAt,omitempty"`
	SavedAt       *time.Time `json:"savedAt,omitempty"`
}

type Media struct {
	ID           int64  `json:"id"`
	PostID       int64  `json:"postId"`
	MediaIndex   int    `json:"mediaIndex"`
	MediaType    string `json:"mediaType"`
	RemoteURL    string `json:"remoteUrl"`
	LocalPath    string `json:"localPath,omitempty"`
	ThumbnailURL string `json:"thumbnailUrl,omitempty"`
	Status       string `json:"downloadStatus"`
}

type MetadataUpdate struct {
	Shortcode     string
	OwnerUsername string
	Caption       string
	MediaType     string
	IsVideo       bool
	IsAlbum       bool
	ThumbnailURL  string
	RawPath       string
	Media         []Media
}

var shortcodeRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func ParseInstagramURL(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", errors.New("empty URL")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		if shortcodeRE.MatchString(raw) {
			return raw, "https://www.instagram.com/p/" + raw + "/", nil
		}
		return "", "", fmt.Errorf("invalid Instagram URL: %q", raw)
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	if host != "instagram.com" {
		return "", "", fmt.Errorf("not an Instagram URL: %q", raw)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 || (parts[0] != "p" && parts[0] != "reel" && parts[0] != "tv") {
		return "", "", fmt.Errorf("unsupported Instagram URL path: %q", raw)
	}
	if !shortcodeRE.MatchString(parts[1]) {
		return "", "", fmt.Errorf("invalid shortcode in URL: %q", raw)
	}
	return parts[1], "https://www.instagram.com/" + parts[0] + "/" + parts[1] + "/", nil
}

func Upsert(ctx context.Context, db *sql.DB, shortcode, postURL, source string) (bool, error) {
	now := time.Now().UTC()
	res, err := db.ExecContext(ctx, `
INSERT INTO posts(shortcode, post_url, media_type, discovered_at, last_seen_at, source, created_at, updated_at)
VALUES(?, ?, 'unknown', ?, ?, ?, ?, ?)
ON CONFLICT(shortcode) DO UPDATE SET post_url=excluded.post_url, last_seen_at=excluded.last_seen_at, updated_at=excluded.updated_at
`, shortcode, postURL, now, now, source, now, now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func UpsertSaved(ctx context.Context, db *sql.DB, update MetadataUpdate, postURL string) error {
	if _, err := Upsert(ctx, db, update.Shortcode, postURL, "saved"); err != nil {
		return err
	}
	if err := ApplyMetadata(ctx, db, update); err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
UPDATE posts
SET owner_username = COALESCE(NULLIF(?, ''), owner_username),
    caption = COALESCE(NULLIF(?, ''), caption),
    media_type = COALESCE(NULLIF(?, ''), media_type),
    is_video = ?,
    is_album = ?,
    thumbnail_url = COALESCE(NULLIF(?, ''), thumbnail_url),
    saved_at = COALESCE(saved_at, ?),
    last_seen_at = ?,
    updated_at = ?
WHERE shortcode = ?`, update.OwnerUsername, update.Caption, update.MediaType, update.IsVideo, update.IsAlbum, update.ThumbnailURL, now, now, now, update.Shortcode)
	return err
}

type ListOptions struct {
	Limit      int
	Offset     int
	All        bool
	Collection string
	Owner      string
	MediaType  string
	Downloaded *bool
	Query      string
	Sort       string
	Order      string
	Format     string
}

func List(ctx context.Context, db *sql.DB, opt ListOptions) ([]Post, error) {
	if opt.Limit <= 0 && !opt.All {
		opt.Limit = 50
	}
	if opt.Sort == "" {
		opt.Sort = "discovered_at"
	}
	allowedSort := map[string]string{"discovered_at": "p.discovered_at", "saved_at": "p.saved_at", "taken_at": "p.taken_at", "owner": "p.owner_username", "shortcode": "p.shortcode"}
	sort, ok := allowedSort[opt.Sort]
	if !ok {
		sort = "p.discovered_at"
	}
	order := "DESC"
	if strings.EqualFold(opt.Order, "asc") {
		order = "ASC"
	}
	where := []string{"1=1"}
	var args []any
	if opt.Collection != "" {
		where = append(where, `EXISTS (SELECT 1 FROM post_collections pc JOIN collections c ON c.id = pc.collection_id WHERE pc.post_id = p.id AND (c.slug = ? OR c.name = ?))`)
		args = append(args, opt.Collection, opt.Collection)
	}
	if opt.Owner != "" {
		where = append(where, "p.owner_username = ?")
		args = append(args, opt.Owner)
	}
	if opt.MediaType != "" && opt.MediaType != "any" {
		where = append(where, "p.media_type = ?")
		args = append(args, opt.MediaType)
	}
	if opt.Downloaded != nil {
		if *opt.Downloaded {
			where = append(where, `EXISTS (SELECT 1 FROM media m WHERE m.post_id = p.id AND m.download_status = 'downloaded')`)
		} else {
			where = append(where, `NOT EXISTS (SELECT 1 FROM media m WHERE m.post_id = p.id AND m.download_status = 'downloaded')`)
		}
	}
	if opt.Query != "" {
		where = append(where, "(p.caption LIKE ? OR p.owner_username LIKE ? OR p.shortcode LIKE ?)")
		q := "%" + opt.Query + "%"
		args = append(args, q, q, q)
	}
	query := fmt.Sprintf(`
SELECT p.id, p.shortcode, p.post_url, COALESCE(p.owner_username,''), COALESCE(p.caption,''), COALESCE(p.media_type,''), p.is_video, p.is_album, p.discovered_at, p.last_seen_at, p.source,
EXISTS (SELECT 1 FROM media m WHERE m.post_id = p.id AND m.download_status = 'downloaded') AS downloaded
FROM posts p
WHERE %s
ORDER BY %s %s`, strings.Join(where, " AND "), sort, order)
	if !opt.All {
		query += "\nLIMIT ? OFFSET ?"
		args = append(args, opt.Limit, opt.Offset)
	} else if opt.Offset > 0 {
		query += "\nLIMIT -1 OFFSET ?"
		args = append(args, opt.Offset)
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.Shortcode, &p.PostURL, &p.OwnerUsername, &p.Caption, &p.MediaType, &p.IsVideo, &p.IsAlbum, &p.DiscoveredAt, &p.LastSeenAt, &p.Source, &p.Downloaded); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func Get(ctx context.Context, db *sql.DB, key string) (Post, error) {
	shortcode, _, _ := ParseInstagramURL(key)
	if shortcode == "" {
		shortcode = key
	}
	var p Post
	err := db.QueryRowContext(ctx, `
SELECT id, shortcode, post_url, COALESCE(owner_username,''), COALESCE(caption,''), COALESCE(media_type,''), is_video, is_album, discovered_at, last_seen_at, source
FROM posts WHERE shortcode = ?`, shortcode).Scan(&p.ID, &p.Shortcode, &p.PostURL, &p.OwnerUsername, &p.Caption, &p.MediaType, &p.IsVideo, &p.IsAlbum, &p.DiscoveredAt, &p.LastSeenAt, &p.Source)
	return p, err
}

func ApplyMetadata(ctx context.Context, db *sql.DB, update MetadataUpdate) error {
	now := time.Now().UTC()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
UPDATE posts
SET owner_username = COALESCE(NULLIF(?, ''), owner_username),
    caption = COALESCE(NULLIF(?, ''), caption),
    media_type = COALESCE(NULLIF(?, ''), media_type),
    is_video = ?,
    is_album = ?,
    thumbnail_url = COALESCE(NULLIF(?, ''), thumbnail_url),
    raw_json_path = COALESCE(NULLIF(?, ''), raw_json_path),
    last_seen_at = ?,
    updated_at = ?
WHERE shortcode = ?`, update.OwnerUsername, update.Caption, update.MediaType, update.IsVideo, update.IsAlbum, update.ThumbnailURL, update.RawPath, now, now, update.Shortcode)
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return sql.ErrNoRows
	}
	var postID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM posts WHERE shortcode = ?`, update.Shortcode).Scan(&postID); err != nil {
		return err
	}
	for _, m := range update.Media {
		if m.RemoteURL == "" {
			continue
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO media(post_id, media_index, media_type, remote_url, thumbnail_url, download_status, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, 'pending', ?, ?)
ON CONFLICT(post_id, media_index) DO UPDATE SET
  media_type = excluded.media_type,
  remote_url = excluded.remote_url,
  thumbnail_url = excluded.thumbnail_url,
  updated_at = excluded.updated_at`, postID, m.MediaIndex, m.MediaType, m.RemoteURL, m.ThumbnailURL, now, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ListMedia(ctx context.Context, db *sql.DB, postID int64) ([]Media, error) {
	rows, err := db.QueryContext(ctx, `
SELECT id, post_id, media_index, COALESCE(media_type,''), COALESCE(remote_url,''), COALESCE(local_path,''), COALESCE(thumbnail_url,''), download_status
FROM media WHERE post_id = ? ORDER BY media_index`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Media
	for rows.Next() {
		var m Media
		if err := rows.Scan(&m.ID, &m.PostID, &m.MediaIndex, &m.MediaType, &m.RemoteURL, &m.LocalPath, &m.ThumbnailURL, &m.Status); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func MarkMediaDownloaded(ctx context.Context, db *sql.DB, mediaID int64, localPath string, size int64) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
UPDATE media SET local_path = ?, file_size_bytes = ?, download_status = 'downloaded', updated_at = ? WHERE id = ?`, localPath, size, now, mediaID)
	return err
}

func RecordDownload(ctx context.Context, db *sql.DB, postID, mediaID int64, status, destination, lastErr string) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
INSERT INTO downloads(post_id, media_id, status, attempt_count, last_error, destination_path, started_at, completed_at, created_at, updated_at)
VALUES(?, ?, ?, 1, NULLIF(?, ''), ?, ?, ?, ?, ?)`, postID, mediaID, status, lastErr, destination, now, now, now, now)
	return err
}
