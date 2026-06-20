package web

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shayanmahtabi/gramli/internal/storage"
)

// seedWebDB builds a temp migrated DB with a couple of posts for the handlers
// to render.
func seedWebDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := storage.Open(filepath.Join(dir, "gramli.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seed := func(shortcode, owner, caption, mtype, status string) {
		res, err := db.Exec(`INSERT INTO posts(shortcode, post_url, owner_username, caption, media_type, discovered_at, last_seen_at, source, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`, shortcode, "https://www.instagram.com/p/"+shortcode+"/", owner, caption, mtype, now, now, "saved", now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		if _, err := db.Exec(`INSERT INTO media(post_id, media_index, media_type, remote_url, download_status, file_size_bytes, created_at, updated_at)
			VALUES(?,1,?,?,?,?,?,?)`, id, mtype, "https://cdn/"+shortcode+".jpg", status, 2048, now, now); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO collections(name, slug, discovered_at, created_at, updated_at) VALUES('Saved','saved',?,?,?) ON CONFLICT(slug) DO NOTHING`, now, now, now); err != nil {
			t.Fatal(err)
		}
		_, _ = db.Exec(`INSERT OR IGNORE INTO post_collections(post_id, collection_id, added_at) SELECT ?, id, ? FROM collections WHERE slug='saved'`, id, now)
	}
	seed("AAA111", "alice", "sunset beach vibes", "image", "downloaded")
	seed("BBB222", "bob", "cooking pasta tonight", "video", "pending")
	return db.DB, dir
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	db, dir := seedWebDB(t)
	s, err := New(db, Options{DataDir: dir, RemoteFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, srv *httptest.Server, path string, htmx bool) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// assertNoTemplateError fails if a rendered page leaked a template error.
func assertNoTemplateError(t *testing.T, path, body string) {
	t.Helper()
	for _, marker := range []string{"wrong type for value", "no such template", "can't evaluate", "executing \""} {
		if strings.Contains(body, marker) {
			t.Errorf("%s leaked template error (%q):\n%s", path, marker, body)
		}
	}
}

func TestPagesRender(t *testing.T) {
	srv := newTestServer(t)
	for _, path := range []string{"/", "/gallery", "/collections", "/owners", "/downloads", "/account"} {
		code, body := get(t, srv, path, false)
		if code != 200 {
			t.Errorf("%s = %d", path, code)
		}
		assertNoTemplateError(t, path, body)
	}
}

func TestGalleryShowsPosts(t *testing.T) {
	srv := newTestServer(t)
	_, body := get(t, srv, "/gallery", false)
	if !strings.Contains(body, "AAA111") || !strings.Contains(body, "BBB222") {
		t.Errorf("gallery missing seeded posts")
	}
}

func TestGalleryFTSSearch(t *testing.T) {
	srv := newTestServer(t)
	_, body := get(t, srv, "/gallery?q=pasta", true)
	if !strings.Contains(body, "BBB222") || strings.Contains(body, "AAA111") {
		t.Errorf("FTS search 'pasta' wrong:\n%s", body)
	}
	// Owner-prefix match.
	_, body2 := get(t, srv, "/gallery?q=ali", true)
	if !strings.Contains(body2, "AAA111") {
		t.Errorf("FTS prefix search 'ali' should match alice's post")
	}
	// Gibberish: no results, no error.
	code, body3 := get(t, srv, "/gallery?q=zzqqx", true)
	if code != 200 || strings.Contains(body3, "AAA111") || strings.Contains(body3, "BBB222") {
		t.Errorf("gibberish search should be empty and clean")
	}
}

func TestPostDetailRenders(t *testing.T) {
	srv := newTestServer(t)
	code, body := get(t, srv, "/post/AAA111", true)
	if code != 200 {
		t.Fatalf("post detail = %d", code)
	}
	assertNoTemplateError(t, "/post/AAA111", body)
	for _, want := range []string{"AAA111", "carousel", "Copy URL"} {
		if !strings.Contains(body, want) {
			t.Errorf("post detail missing %q", want)
		}
	}
	if code, _ := get(t, srv, "/post/NOPE", true); code != 404 {
		t.Errorf("unknown post should 404, got %d", code)
	}
}

func TestExportEndpoints(t *testing.T) {
	srv := newTestServer(t)
	code, body := get(t, srv, "/export?format=json", false)
	if code != 200 || !strings.Contains(body, "AAA111") {
		t.Errorf("json export wrong: %d", code)
	}
	code, csv := get(t, srv, "/export?format=csv", false)
	if code != 200 || !strings.HasPrefix(csv, "shortcode,owner,type") {
		t.Errorf("csv export wrong: %d\n%s", code, csv)
	}
	// Filtered export respects gallery filters.
	_, filtered := get(t, srv, "/export?format=csv&owner=bob", false)
	if !strings.Contains(filtered, "BBB222") || strings.Contains(filtered, "AAA111") {
		t.Errorf("filtered export wrong:\n%s", filtered)
	}
}

func TestThumbPlaceholderAndMissingMedia(t *testing.T) {
	srv := newTestServer(t)
	code, body := get(t, srv, "/thumb/0", false)
	if code != 200 || !strings.Contains(body, "svg") {
		t.Errorf("placeholder thumb wrong: %d", code)
	}
	// A media row with no local file should 404 on /media.
	if code, _ := get(t, srv, "/media/1", false); code != 404 {
		t.Errorf("media with no local file should 404, got %d", code)
	}
}
