package instagram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCookies writes a minimal valid session cookie file and returns its path.
func writeCookies(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.json")
	body := `[{"name":"sessionid","value":"x"},{"name":"csrftoken","value":"c"},{"name":"ds_user_id","value":"42"}]`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newTestClient(t *testing.T, baseURL string) *Client {
	c := NewClient(writeCookies(t), "")
	c.BaseURL = baseURL
	return c
}

const savedPage1 = `{"items":[{"media":{"code":"P1","user":{"username":"alice"},"media_type":1,"image_versions2":{"candidates":[{"url":"https://cdn/p1.jpg"}]}}}],"next_max_id":"page2id","more_available":true}`
const savedPage2 = `{"items":[{"media":{"code":"P2","user":{"username":"bob"},"media_type":1,"image_versions2":{"candidates":[{"url":"https://cdn/p2.jpg"}]}}}],"more_available":false}`

func TestFetchSavedPostsPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/feed/saved/posts/" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("X-IG-App-ID") == "" {
			t.Error("missing X-IG-App-ID header")
		}
		if r.URL.Query().Get("max_id") == "page2id" {
			_, _ = w.Write([]byte(savedPage2))
			return
		}
		_, _ = w.Write([]byte(savedPage1))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)

	p1, err := c.FetchSavedPosts(context.Background(), "")
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(p1.Posts) != 1 || p1.Posts[0].Shortcode != "P1" || p1.Posts[0].OwnerUsername != "alice" {
		t.Fatalf("page1 parse wrong: %+v", p1.Posts)
	}
	if !p1.HasNextPage || p1.NextMaxID != "page2id" {
		t.Fatalf("page1 pagination wrong: next=%q hasNext=%v", p1.NextMaxID, p1.HasNextPage)
	}

	p2, err := c.FetchSavedPosts(context.Background(), p1.NextMaxID)
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(p2.Posts) != 1 || p2.Posts[0].Shortcode != "P2" {
		t.Fatalf("page2 parse wrong: %+v", p2.Posts)
	}
	if p2.HasNextPage {
		t.Errorf("page2 should be the last page")
	}
}

func TestFetchSavedPostsRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	_, err := c.FetchSavedPosts(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "RATE_LIMITED") {
		t.Fatalf("expected RATE_LIMITED, got %v", err)
	}
}

func TestFetchProfileByUsername(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/users/web_profile_info/") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.URL.Query().Get("username") != "alice" {
			t.Errorf("missing username param: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(webProfileInfoFixture))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	p, err := c.FetchProfileByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("fetch profile: %v", err)
	}
	if p.Username != "gramli_test" || p.FollowerCount != 4200 {
		t.Fatalf("profile parse wrong: %+v", p)
	}
}

func TestDetectSelfUsernameFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/accounts/current_user/":
			w.WriteHeader(http.StatusBadRequest) // simulate the observed 400
		case "/api/v1/users/42/info/":
			_, _ = w.Write([]byte(`{"user":{"username":"alice"},"status":"ok"}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	username, err := c.DetectSelfUsername(context.Background())
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if username != "alice" {
		t.Fatalf("expected alice via ds_user_id fallback, got %q", username)
	}
}

func TestFetchCollections(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/collections/list/" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(collectionsFixture))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	cols, err := c.FetchCollections(context.Background())
	if err != nil {
		t.Fatalf("fetch collections: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 collections, got %d", len(cols))
	}
}

func TestFetchPostHTML(t *testing.T) {
	html := `<html><head>
<meta property="og:title" content="Instagram photo by alice">
<meta property="og:image" content="https://cdn/x.jpg">
<meta property="og:description" content="a nice caption">
</head></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/p/") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(html))
	}))
	defer srv.Close()
	c := newTestClient(t, srv.URL)
	meta, err := c.FetchPost(context.Background(), "https://www.instagram.com/p/ABC123/")
	if err != nil {
		t.Fatalf("fetch post: %v", err)
	}
	if meta.Shortcode != "ABC123" || meta.MediaType != "image" || meta.OwnerUsername != "alice" {
		t.Fatalf("post parse wrong: %+v", meta)
	}
	if meta.PostURL != "https://www.instagram.com/p/ABC123/" {
		t.Errorf("PostURL should stay canonical, got %s", meta.PostURL)
	}
}
