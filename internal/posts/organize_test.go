package posts

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shayanmahtabi/gramli/internal/storage"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db.DB
}

func insertPost(t *testing.T, db *sql.DB, shortcode, owner, caption string) int64 {
	t.Helper()
	now := time.Now().UTC()
	res, err := db.Exec(`INSERT INTO posts(shortcode, post_url, owner_username, caption, media_type, discovered_at, last_seen_at, source, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, shortcode, "https://www.instagram.com/p/"+shortcode+"/", owner, caption, "image", now, now, "saved", now, now)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestUpsertOwnVsSaved(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	taken := time.Unix(1700000000, 0).UTC()

	if err := UpsertOwn(ctx, db, MetadataUpdate{
		Shortcode: "OWN1", OwnerUsername: "me", MediaType: "video", IsVideo: true,
		ThumbnailURL: "https://cdn/own1.jpg", TakenAt: &taken,
		Media: []Media{{MediaIndex: 1, MediaType: "video", RemoteURL: "https://cdn/own1.mp4"}},
	}, "https://www.instagram.com/p/OWN1/"); err != nil {
		t.Fatalf("UpsertOwn: %v", err)
	}
	if err := UpsertSaved(ctx, db, MetadataUpdate{
		Shortcode: "SAV1", OwnerUsername: "alice", MediaType: "image",
		ThumbnailURL: "https://cdn/sav1.jpg",
		Media:        []Media{{MediaIndex: 1, MediaType: "image", RemoteURL: "https://cdn/sav1.jpg"}},
	}, "https://www.instagram.com/p/SAV1/"); err != nil {
		t.Fatalf("UpsertSaved: %v", err)
	}

	var source string
	var savedAt, takenAt sql.NullString
	if err := db.QueryRow(`SELECT source, CAST(saved_at AS TEXT), CAST(taken_at AS TEXT) FROM posts WHERE shortcode='OWN1'`).
		Scan(&source, &savedAt, &takenAt); err != nil {
		t.Fatalf("query OWN1: %v", err)
	}
	if source != "own" {
		t.Errorf("own post source = %q, want own", source)
	}
	if savedAt.Valid && savedAt.String != "" {
		t.Errorf("own post should not stamp saved_at, got %q", savedAt.String)
	}
	if !takenAt.Valid || takenAt.String == "" {
		t.Errorf("own post should stamp taken_at, got %q", takenAt.String)
	}

	if err := db.QueryRow(`SELECT source, CAST(saved_at AS TEXT) FROM posts WHERE shortcode='SAV1'`).
		Scan(&source, &savedAt); err != nil {
		t.Fatalf("query SAV1: %v", err)
	}
	if source != "saved" {
		t.Errorf("saved post source = %q, want saved", source)
	}
	if !savedAt.Valid || savedAt.String == "" {
		t.Errorf("saved post should stamp saved_at")
	}

	// Media rows were attached for the own post.
	var mediaCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM media m JOIN posts p ON p.id=m.post_id WHERE p.shortcode='OWN1'`).Scan(&mediaCount); err != nil {
		t.Fatal(err)
	}
	if mediaCount != 1 {
		t.Errorf("own post media rows = %d, want 1", mediaCount)
	}
}

func TestExtractHashtags(t *testing.T) {
	got := ExtractHashtags("Loving the #Sunset at the #beach #sunset again @friend no#3")
	want := []string{"3", "beach", "sunset"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hashtag[%d] = %q want %q", i, got[i], want[i])
		}
	}
}

func TestDeletePostCascadeAndFiles(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	downloads := filepath.Join(dir, "downloads")
	postDir := filepath.Join(downloads, "alice", "ABC123")
	if err := os.MkdirAll(postDir, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(postDir, "01_image.jpg")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := insertPost(t, db, "ABC123", "alice", "a #cat photo")
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO media(post_id, media_index, media_type, remote_url, local_path, download_status, created_at, updated_at)
		VALUES(?,1,'image','https://cdn/x.jpg',?,'downloaded',?,?)`, id, file, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateCollection(ctx, db, "Saved"); err != nil {
		t.Fatal(err)
	}
	if err := SetPostCollection(ctx, db, "ABC123", "Saved", true); err != nil {
		t.Fatal(err)
	}

	removed, err := DeletePost(ctx, db, downloads, "ABC123", true)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 file removed, got %d", removed)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("file should be gone")
	}
	for _, table := range []string{"posts", "media", "post_collections", "posts_fts"} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s should be empty after delete, got %d", table, n)
		}
	}

	if _, err := DeletePost(ctx, db, downloads, "GONE", true); err != ErrPostNotFound {
		t.Errorf("expected ErrPostNotFound, got %v", err)
	}
}

func TestCollectionsAndTags(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	insertPost(t, db, "P1", "bob", "hello")

	if err := SetPostCollection(ctx, db, "P1", "Ideas", true); err != nil {
		t.Fatal(err)
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM post_collections`).Scan(&n)
	if n != 1 {
		t.Errorf("expected membership, got %d", n)
	}
	if err := SetPostCollection(ctx, db, "P1", "Ideas", false); err != nil {
		t.Fatal(err)
	}
	db.QueryRow(`SELECT COUNT(*) FROM post_collections`).Scan(&n)
	if n != 0 {
		t.Errorf("expected removal, got %d", n)
	}

	if err := AddTag(ctx, db, "P1", "design"); err != nil {
		t.Fatal(err)
	}
	if err := AddTag(ctx, db, "P1", "#Design"); err != nil { // same slug, idempotent
		t.Fatal(err)
	}
	if err := AddTag(ctx, db, "P1", "ux"); err != nil {
		t.Fatal(err)
	}
	var pid int64
	db.QueryRow(`SELECT id FROM posts WHERE shortcode='P1'`).Scan(&pid)
	tags, err := PostTags(ctx, db, pid)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Errorf("expected 2 tags (design, ux), got %v", tags)
	}
	if err := RemoveTag(ctx, db, "P1", "design"); err != nil {
		t.Fatal(err)
	}
	tags, _ = PostTags(ctx, db, pid)
	if len(tags) != 1 || tags[0] != "ux" {
		t.Errorf("expected [ux], got %v", tags)
	}
}
