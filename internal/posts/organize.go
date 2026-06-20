package posts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ErrPostNotFound is returned when a shortcode has no matching post row.
var ErrPostNotFound = errors.New("POST_NOT_FOUND: no post with that shortcode")

// DeletePost removes a post and (optionally) its downloaded files. Deleting the
// post row cascades to media, downloads, and post_collections via foreign keys,
// and the FTS index is kept current by triggers. Returns the number of files
// removed. File deletion is constrained to downloadsDir.
func DeletePost(ctx context.Context, db *sql.DB, downloadsDir, shortcode string, withFiles bool) (int, error) {
	if sc, _, err := ParseInstagramURL(shortcode); err == nil && sc != "" {
		shortcode = sc
	}
	var postID int64
	var owner string
	err := db.QueryRowContext(ctx, `SELECT id, COALESCE(owner_username,'') FROM posts WHERE shortcode=?`, shortcode).Scan(&postID, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrPostNotFound
	}
	if err != nil {
		return 0, err
	}

	removed := 0
	if withFiles {
		rows, err := db.QueryContext(ctx, `SELECT COALESCE(local_path,'') FROM media WHERE post_id=?`, postID)
		if err != nil {
			return 0, err
		}
		var paths []string
		for rows.Next() {
			var p string
			if rows.Scan(&p) == nil && p != "" {
				paths = append(paths, p)
			}
		}
		rows.Close()
		for _, p := range paths {
			if abs, ok := underDir(downloadsDir, p); ok {
				if err := os.Remove(abs); err == nil {
					removed++
				}
			}
		}
		// Best-effort removal of the now-empty post folder.
		if owner != "" {
			folder := filepath.Join(downloadsDir, owner, shortcode)
			if abs, ok := underDir(downloadsDir, folder); ok {
				_ = os.RemoveAll(abs)
			}
		}
	}

	_, err = db.ExecContext(ctx, `DELETE FROM posts WHERE id=?`, postID)
	return removed, err
}

// underDir resolves p and confirms it lives within root, defeating traversal.
func underDir(root, p string) (string, bool) {
	if root == "" || p == "" {
		return "", false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", false
	}
	if abs != absRoot && !strings.HasPrefix(abs, absRoot+string(os.PathSeparator)) {
		return "", false
	}
	return abs, true
}

// Slugify converts a name into a URL/CLI-friendly slug.
func Slugify(name string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "item"
	}
	return slug
}

// CreateCollection creates a local collection, returning its slug. Idempotent on
// slug.
func CreateCollection(ctx context.Context, db *sql.DB, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("collection name is required")
	}
	slug := Slugify(name)
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `INSERT INTO collections(name, slug, discovered_at, created_at, updated_at)
VALUES(?,?,?,?,?) ON CONFLICT(slug) DO UPDATE SET name=excluded.name, updated_at=excluded.updated_at`, name, slug, now, now, now)
	return slug, err
}

// DeleteCollection removes a local collection (membership rows cascade).
func DeleteCollection(ctx context.Context, db *sql.DB, nameOrSlug string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM collections WHERE slug=? OR name=?`, nameOrSlug, nameOrSlug)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("COLLECTION_NOT_FOUND: %q", nameOrSlug)
	}
	return nil
}

// SetPostCollection adds or removes a post's membership in a collection,
// creating the collection on add if needed.
func SetPostCollection(ctx context.Context, db *sql.DB, shortcode, nameOrSlug string, member bool) error {
	postID, err := postIDByShortcode(ctx, db, shortcode)
	if err != nil {
		return err
	}
	if !member {
		_, err := db.ExecContext(ctx, `DELETE FROM post_collections WHERE post_id=? AND collection_id IN (SELECT id FROM collections WHERE slug=? OR name=?)`, postID, nameOrSlug, nameOrSlug)
		return err
	}
	slug, err := CreateCollection(ctx, db, nameOrSlug)
	if err != nil {
		return err
	}
	var colID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM collections WHERE slug=?`, slug).Scan(&colID); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO post_collections(post_id, collection_id, added_at) VALUES(?,?,?)`, postID, colID, time.Now().UTC())
	return err
}

// AddTag attaches a local tag to a post, creating the tag if needed.
func AddTag(ctx context.Context, db *sql.DB, shortcode, tag string) error {
	postID, err := postIDByShortcode(ctx, db, shortcode)
	if err != nil {
		return err
	}
	tag = strings.TrimSpace(strings.TrimPrefix(tag, "#"))
	if tag == "" {
		return errors.New("tag is required")
	}
	slug := Slugify(tag)
	now := time.Now().UTC()
	if _, err := db.ExecContext(ctx, `INSERT INTO tags(name, slug, created_at) VALUES(?,?,?) ON CONFLICT(slug) DO NOTHING`, tag, slug, now); err != nil {
		return err
	}
	var tagID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM tags WHERE slug=?`, slug).Scan(&tagID); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT OR IGNORE INTO post_tags(post_id, tag_id, added_at) VALUES(?,?,?)`, postID, tagID, now)
	return err
}

// RemoveTag detaches a tag from a post.
func RemoveTag(ctx context.Context, db *sql.DB, shortcode, tag string) error {
	postID, err := postIDByShortcode(ctx, db, shortcode)
	if err != nil {
		return err
	}
	tag = strings.TrimSpace(strings.TrimPrefix(tag, "#"))
	_, err = db.ExecContext(ctx, `DELETE FROM post_tags WHERE post_id=? AND tag_id IN (SELECT id FROM tags WHERE slug=? OR name=?)`, postID, Slugify(tag), tag)
	return err
}

// PostTags returns the tag names attached to a post.
func PostTags(ctx context.Context, db *sql.DB, postID int64) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT t.name FROM tags t JOIN post_tags pt ON pt.tag_id=t.id WHERE pt.post_id=? ORDER BY t.name`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

var hashtagRE = regexp.MustCompile(`#([\p{L}0-9_]+)`)

// ExtractHashtags returns the unique, lowercased hashtags found in a caption.
func ExtractHashtags(caption string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range hashtagRE.FindAllStringSubmatch(caption, -1) {
		tag := strings.ToLower(m[1])
		if !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	sort.Strings(out)
	return out
}

func postIDByShortcode(ctx context.Context, db *sql.DB, shortcode string) (int64, error) {
	if sc, _, err := ParseInstagramURL(shortcode); err == nil && sc != "" {
		shortcode = sc
	}
	var id int64
	err := db.QueryRowContext(ctx, `SELECT id FROM posts WHERE shortcode=?`, shortcode).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrPostNotFound
	}
	return id, err
}
