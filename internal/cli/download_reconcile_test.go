package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDownloadRunAutoReconciles verifies that `download run` syncs media status
// from files on disk at the end, so a downloaded post no longer shows pending.
// Uses a non-matching collection so no network download is attempted; the
// post-run reconcile still scans the downloads dir and updates the row.
func TestDownloadRunAutoReconciles(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "AR0001", Owner: "alice", MediaType: "image", Downloaded: false})

	// Place a downloaded file where reconcile scans.
	dir := filepath.Join(e.dataDir, "downloads", "alice", "AR0001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "01_image.jpg"), []byte("imgdata"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Run against a collection that matches nothing -> no downloads attempted,
	// but the deferred reconcile should still flip the row to downloaded.
	out, _ := e.mustRun("download", "run", "--collection", "no-such-collection")
	if !strings.Contains(out, "Reconcile: synced 1") {
		t.Errorf("expected auto-reconcile to sync 1 row, output:\n%s", out)
	}
	status, _ := e.mustRun("download", "status")
	if !strings.Contains(status, "Downloaded: 1") || !strings.Contains(status, "Pending: 0") {
		t.Errorf("status should reflect the downloaded file:\n%s", status)
	}
}

// TestDownloadRunNoReconcileFlag verifies --no-reconcile leaves status untouched.
func TestDownloadRunNoReconcileFlag(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "AR0002", Owner: "bob", MediaType: "image", Downloaded: false})
	dir := filepath.Join(e.dataDir, "downloads", "bob", "AR0002")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "01_image.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.mustRun("download", "run", "--collection", "no-such-collection", "--no-reconcile")
	status, _ := e.mustRun("download", "status")
	if !strings.Contains(status, "Pending: 1") {
		t.Errorf("--no-reconcile should leave the row pending:\n%s", status)
	}
}
