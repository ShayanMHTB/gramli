package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedSome creates an initialised workspace with three posts across two owners,
// two collections, and mixed download states.
func seedSome(e *env) {
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "AAA111", Owner: "alice", MediaType: "image", Downloaded: true, Collection: "saved", Caption: "sunset beach"})
	e.seedPost(seedOpts{Shortcode: "BBB222", Owner: "bob", MediaType: "video", Downloaded: false, Collection: "saved", Caption: "cooking pasta"})
	e.seedPost(seedOpts{Shortcode: "CCC333", Owner: "alice", MediaType: "album", Downloaded: false, Collection: "travel", Caption: "mountains"})
}

func TestPostsListFilters(t *testing.T) {
	e := newEnv(t)
	seedSome(e)

	all, _ := e.mustRun("posts", "list")
	for _, sc := range []string{"AAA111", "BBB222", "CCC333"} {
		if !strings.Contains(all, sc) {
			t.Errorf("list missing %s:\n%s", sc, all)
		}
	}

	byOwner, _ := e.mustRun("posts", "list", "--owner", "alice")
	if strings.Contains(byOwner, "BBB222") || !strings.Contains(byOwner, "AAA111") {
		t.Errorf("--owner alice wrong:\n%s", byOwner)
	}

	byType, _ := e.mustRun("posts", "list", "--media-type", "video")
	if !strings.Contains(byType, "BBB222") || strings.Contains(byType, "AAA111") {
		t.Errorf("--media-type video wrong:\n%s", byType)
	}

	dl, _ := e.mustRun("posts", "list", "--downloaded")
	if !strings.Contains(dl, "AAA111") || strings.Contains(dl, "BBB222") {
		t.Errorf("--downloaded wrong:\n%s", dl)
	}
	notdl, _ := e.mustRun("posts", "list", "--not-downloaded")
	if strings.Contains(notdl, "AAA111") || !strings.Contains(notdl, "BBB222") {
		t.Errorf("--not-downloaded wrong:\n%s", notdl)
	}

	byCol, _ := e.mustRun("posts", "list", "--collection", "travel")
	if !strings.Contains(byCol, "CCC333") || strings.Contains(byCol, "AAA111") {
		t.Errorf("--collection travel wrong:\n%s", byCol)
	}

	out, _ := e.mustRun("posts", "list", "--format", "json")
	var arr []map[string]any
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("--format json not valid: %v\n%s", err, out)
	}
	if len(arr) != 3 {
		t.Errorf("json list len = %d, want 3", len(arr))
	}
}

func TestPostsShowAndSearch(t *testing.T) {
	e := newEnv(t)
	seedSome(e)

	out, _ := e.mustRun("posts", "show", "AAA111")
	if !strings.Contains(out, "AAA111") {
		t.Errorf("show missing shortcode:\n%s", out)
	}
	if _, _, err := e.run("posts", "show", "NOPE999"); err == nil {
		t.Error("show of unknown shortcode should error")
	}

	search, _ := e.mustRun("posts", "search", "pasta")
	if !strings.Contains(search, "BBB222") || strings.Contains(search, "AAA111") {
		t.Errorf("search pasta wrong:\n%s", search)
	}
}

func TestPostsMediaAddAndList(t *testing.T) {
	e := newEnv(t)
	seedSome(e)
	e.mustRun("posts", "media", "add", "CCC333", "--url", "https://cdn.example/extra.jpg", "--type", "image", "--index", "2")
	out, _ := e.mustRun("posts", "media", "list", "CCC333")
	if !strings.Contains(out, "extra.jpg") {
		t.Errorf("media list missing added url:\n%s", out)
	}
}

func TestPostsClean(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "KEEP01", Owner: "a", Downloaded: true})
	e.seedOrphanPost("ORPH01", "manual-import")
	if e.count("posts") != 2 {
		t.Fatalf("expected 2 posts")
	}

	dry, _ := e.mustRun("posts", "clean", "--dry-run")
	if !strings.Contains(dry, "Would remove 1") {
		t.Errorf("dry-run wrong: %s", dry)
	}
	if e.count("posts") != 2 {
		t.Error("dry-run must not delete")
	}
	if _, _, err := e.run("posts", "clean"); err == nil {
		t.Error("clean without --yes should refuse")
	}
	e.mustRun("posts", "clean", "--yes")
	if e.count("posts") != 1 {
		t.Errorf("after clean expected 1 post, got %d", e.count("posts"))
	}
}

func TestCollectionsListShowRename(t *testing.T) {
	e := newEnv(t)
	seedSome(e)

	list, _ := e.mustRun("collections", "list")
	if !strings.Contains(list, "saved") || !strings.Contains(list, "travel") {
		t.Errorf("collections list wrong:\n%s", list)
	}
	show, _ := e.mustRun("collections", "show", "saved")
	if !strings.Contains(show, "AAA111") {
		t.Errorf("collections show saved missing post:\n%s", show)
	}
	e.mustRun("collections", "rename-local", "travel", "trips")
	list2, _ := e.mustRun("collections", "list")
	if !strings.Contains(list2, "trips") || strings.Contains(list2, "travel") {
		t.Errorf("rename-local failed:\n%s", list2)
	}
}

func TestExportFormats(t *testing.T) {
	e := newEnv(t)
	seedSome(e)

	js, _ := e.mustRun("export", "--format", "json", "--stdout")
	if !json.Valid([]byte(js)) {
		t.Errorf("json export invalid:\n%s", js)
	}
	csv, _ := e.mustRun("export", "--format", "csv", "--stdout")
	if !strings.Contains(csv, "AAA111") {
		t.Errorf("csv export missing data:\n%s", csv)
	}
	md, _ := e.mustRun("export", "--format", "markdown", "--stdout")
	if md == "" {
		t.Error("markdown export empty")
	}

	// File output: refuse to clobber without --overwrite, succeed with it.
	outPath := filepath.Join(e.dataDir, "exports", "out.json")
	e.mustRun("export", "--format", "json", "--output", outPath)
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("export output not written: %v", err)
	}
	if _, _, err := e.run("export", "--format", "json", "--output", outPath); err == nil {
		t.Error("export should refuse to overwrite without --overwrite")
	}
	if _, _, err := e.run("export", "--format", "json", "--output", outPath, "--overwrite"); err != nil {
		t.Errorf("export --overwrite failed: %v", err)
	}
}

func TestDownloadStatusAndRetry(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "FAIL01", Owner: "a"})
	e.setMediaStatus("FAIL01", "failed")
	e.seedPost(seedOpts{Shortcode: "MISS01", Owner: "b"})
	e.setMediaStatus("MISS01", "missing")

	status, _ := e.mustRun("download", "status")
	if !strings.Contains(status, "Failed: 1") || !strings.Contains(status, "Missing: 1") {
		t.Errorf("download status wrong:\n%s", status)
	}

	dry, _ := e.mustRun("download", "retry", "--failed", "--missing", "--dry-run")
	if !strings.Contains(dry, "Would re-queue 2") {
		t.Errorf("retry dry-run wrong: %s", dry)
	}
	e.mustRun("download", "retry", "--failed", "--missing")
	status2, _ := e.mustRun("download", "status")
	if !strings.Contains(status2, "Pending: 2") {
		t.Errorf("after retry expected 2 pending:\n%s", status2)
	}
}

func TestDownloadCleanDryRun(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	if _, _, err := e.run("download", "clean", "--cache", "--empty-dirs", "--dry-run"); err != nil {
		t.Errorf("download clean dry-run failed: %v", err)
	}
}

func TestConfigSetIntegration(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.mustRun("config", "set", "downloads.concurrency", "8")
	show, _ := e.mustRun("config", "show")
	if !strings.Contains(show, "concurrency: 8") {
		t.Errorf("config set not reflected:\n%s", show)
	}
}

func TestAccountAndAuthErrorPaths(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")

	// No session yet.
	if _, _, err := e.run("auth", "status"); err == nil {
		t.Error("auth status without session should error")
	}
	if _, _, err := e.run("account", "show"); err == nil {
		t.Error("account show without account should error")
	}
	if _, _, err := e.run("account", "switch", "--account", "ghost"); err == nil {
		t.Error("account switch to unknown alias should error")
	}
}

func TestAccountSwitchAndLogoutDelete(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	cookie := e.seedSession("personal")

	// switch to an existing alias succeeds and records it in config.
	e.mustRun("account", "switch", "--account", "personal")
	show, _ := e.mustRun("config", "show")
	if !strings.Contains(show, "active_account: personal") {
		t.Errorf("active_account not set:\n%s", show)
	}

	// auth status (local) now works.
	st, _ := e.mustRun("auth", "status")
	if !strings.Contains(st, "personal") {
		t.Errorf("auth status missing account:\n%s", st)
	}

	// logout --delete-session-files removes the cookie file.
	e.mustRun("logout", "--delete-session-files")
	if _, err := os.Stat(cookie); !os.IsNotExist(err) {
		t.Errorf("expected cookie file deleted, stat err = %v", err)
	}
}

func TestLoginUpsertNoDuplicateAccounts(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	// Write a cookie file to import twice under the same alias.
	cookiePath := filepath.Join(e.dataDir, "in.cookies.json")
	if err := os.WriteFile(cookiePath, []byte(`[{"name":"sessionid","value":"abc"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	e.mustRun("login", "--cookie-file", cookiePath, "--account", "personal")
	e.mustRun("login", "--cookie-file", cookiePath, "--account", "personal")
	if n := e.count("accounts"); n != 1 {
		t.Errorf("re-login created duplicate accounts: got %d, want 1", n)
	}
	if n := e.count("sessions"); n != 1 {
		t.Errorf("re-login created duplicate sessions: got %d, want 1", n)
	}
}
