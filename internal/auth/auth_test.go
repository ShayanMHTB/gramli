package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCookiesArray(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cookies.json")
	err := os.WriteFile(path, []byte(`[
		{"name":"sessionid","value":"abc","domain":".instagram.com","path":"/"},
		{"name":"csrftoken","value":"def","domain":".instagram.com","path":"/"}
	]`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := LoadCookies(path)
	if err != nil {
		t.Fatalf("LoadCookies error: %v", err)
	}
	if len(cookies) != 2 || cookies[0].Name != "sessionid" {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}
}

func TestLoadCookiesWrapped(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cookies.json")
	err := os.WriteFile(path, []byte(`{"cookies":[{"name":"sessionid","value":"abc"}]}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := LoadCookies(path)
	if err != nil {
		t.Fatalf("LoadCookies error: %v", err)
	}
	if len(cookies) != 1 || cookies[0].Path != "/" {
		t.Fatalf("unexpected cookies: %#v", cookies)
	}
}

func TestLoadCookiesMap(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cookies.json")
	err := os.WriteFile(path, []byte(`{"sessionid":"abc","csrftoken":"def"}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := LoadCookies(path)
	if err != nil {
		t.Fatalf("LoadCookies error: %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("unexpected cookie count: %d", len(cookies))
	}
}

func TestLoadCookiesWithBrowserStyleEscapes(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cookies.json")
	err := os.WriteFile(path, []byte(`[
		{"name":"sessionid","value":"abc","domain":".instagram.com","path":"/"},
		{"name":"rur","value":"\"RVA\054123\054456:01f\"","domain":".instagram.com","path":"/"}
	]`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cookies, err := LoadCookies(path)
	if err != nil {
		t.Fatalf("LoadCookies error: %v", err)
	}
	if len(cookies) != 2 {
		t.Fatalf("unexpected cookie count: %d", len(cookies))
	}
}

func TestCookieHeaderSkipsUnsafeValues(t *testing.T) {
	t.Parallel()
	header := CookieHeader([]Cookie{
		{Name: "sessionid", Value: "abc"},
		{Name: "bad", Value: "line\nbreak"},
		{Name: "rur", Value: `"RVA\054123\054456:01f"`},
	})
	if header != `sessionid=abc; rur="RVA\054123\054456:01f"` {
		t.Fatalf("unexpected header: %q", header)
	}
}

func TestWriteNetscapeCookieFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cookies.txt")
	err := WriteNetscapeCookieFile([]Cookie{{Name: "sessionid", Value: "abc", Domain: ".instagram.com", Path: "/", Secure: true}}, path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, ".instagram.com\tTRUE\t/\tTRUE\t0\tsessionid\tabc") {
		t.Fatalf("unexpected cookie file:\n%s", got)
	}
}
