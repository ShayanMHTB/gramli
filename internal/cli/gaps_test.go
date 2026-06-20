package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPostsImportFileAndStdin(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")

	urls := "https://www.instagram.com/p/IMP001/\nhttps://www.instagram.com/p/IMP002/\n"
	file := filepath.Join(e.dataDir, "urls.txt")
	if err := os.WriteFile(file, []byte(urls), 0o644); err != nil {
		t.Fatal(err)
	}
	e.mustRun("posts", "import", file)
	if e.count("posts") != 2 {
		t.Fatalf("file import expected 2 posts, got %d", e.count("posts"))
	}

	// stdin import of a third URL.
	if _, _, err := e.runStdin("https://www.instagram.com/p/IMP003/\n", "posts", "import", "--stdin"); err != nil {
		t.Fatalf("stdin import failed: %v", err)
	}
	if e.count("posts") != 3 {
		t.Errorf("stdin import expected 3 posts total, got %d", e.count("posts"))
	}
}

func TestDownloadReconcileApply(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "RECON1", Owner: "rick", MediaType: "image", Downloaded: false})

	// Place a downloaded file where reconcile scans: downloads/<owner>/<shortcode>/.
	dir := filepath.Join(e.dataDir, "downloads", "rick", "RECON1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "01_image.jpg"), []byte("imgdata"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Preview makes no changes.
	e.mustRun("download", "reconcile")
	status, _ := e.mustRun("download", "status")
	if !strings.Contains(status, "Downloaded: 0") {
		t.Errorf("reconcile preview should not change state:\n%s", status)
	}

	// Apply marks the media downloaded.
	e.mustRun("download", "reconcile", "--apply")
	status2, _ := e.mustRun("download", "status")
	if !strings.Contains(status2, "Downloaded: 1") {
		t.Errorf("reconcile --apply should mark 1 downloaded:\n%s", status2)
	}
}

func TestDBVacuum(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	if _, _, err := e.run("db", "vacuum"); err != nil {
		t.Errorf("db vacuum failed: %v", err)
	}
}

func TestDBResetYesWipesData(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "WIPE01", Owner: "a", Downloaded: true})
	if e.count("posts") != 1 {
		t.Fatal("expected seeded post")
	}
	e.mustRun("db", "reset", "--yes")
	if e.count("posts") != 0 {
		t.Errorf("db reset --yes should wipe posts, got %d", e.count("posts"))
	}
	if e.count("media") != 0 {
		t.Errorf("db reset --yes should wipe media, got %d", e.count("media"))
	}
}
