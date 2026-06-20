package cli

import (
	"strings"
	"testing"
)

// TestDownloadRunPendingExcludesDownloaded proves --pending filters out
// already-downloaded posts. A collection containing only a downloaded post
// yields zero work under --pending (and so makes no network call), which only
// happens if the filter is wired correctly.
func TestDownloadRunPendingExcludesDownloaded(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "PD0001", Owner: "alice", MediaType: "image", Downloaded: true, Collection: "saved"})

	out, _ := e.mustRun("download", "run", "--collection", "saved", "--pending", "--no-reconcile")
	if !strings.Contains(out, "found 0 posts") {
		t.Errorf("--pending should exclude the downloaded post (expected 0 posts):\n%s", out)
	}
}
