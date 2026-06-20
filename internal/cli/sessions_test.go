package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func archiveCount(t *testing.T, dataDir string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dataDir, "sessions", "archive"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read archive dir: %v", err)
	}
	return len(entries)
}

func TestSessionsList(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedSession("personal")
	out, _ := e.mustRun("sessions", "list")
	if !strings.Contains(out, "personal") {
		t.Errorf("sessions list missing alias:\n%s", out)
	}
}

func TestSessionsArchive(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	cookie := e.seedSession("personal")

	e.mustRun("sessions", "archive", "personal")
	if e.count("sessions") != 0 {
		t.Errorf("archive should drop the session row, have %d", e.count("sessions"))
	}
	if e.count("accounts") != 1 {
		t.Errorf("archive should keep the account row, have %d", e.count("accounts"))
	}
	if _, err := os.Stat(cookie); !os.IsNotExist(err) {
		t.Errorf("original cookie file should be moved, stat err = %v", err)
	}
	if n := archiveCount(t, e.dataDir); n != 1 {
		t.Errorf("expected 1 archived cookie file, got %d", n)
	}
	// Unknown alias errors.
	if _, _, err := e.run("sessions", "archive", "ghost"); err == nil {
		t.Error("archiving unknown alias should error")
	}
}

func TestSessionsRemove(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	cookie := e.seedSession("personal")

	if _, _, err := e.run("sessions", "remove", "personal"); err == nil {
		t.Error("remove without --yes should refuse")
	}
	if e.count("sessions") != 1 {
		t.Error("refused remove must not delete")
	}
	e.mustRun("sessions", "remove", "personal", "--yes")
	if e.count("sessions") != 0 {
		t.Errorf("remove should drop the row, have %d", e.count("sessions"))
	}
	if _, err := os.Stat(cookie); !os.IsNotExist(err) {
		t.Errorf("remove should delete the cookie file, stat err = %v", err)
	}
}

func TestSessionsPrune(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedSession("personal")
	// Deactivate it, then prune inactive.
	e.mustRun("logout", "--all")
	dry, _ := e.mustRun("sessions", "prune", "--dry-run")
	if !strings.Contains(dry, "Would remove 1") {
		t.Errorf("prune dry-run wrong: %s", dry)
	}
	e.mustRun("sessions", "prune", "--yes")
	if e.count("sessions") != 0 {
		t.Errorf("prune should remove inactive sessions, have %d", e.count("sessions"))
	}
}

func TestLogoutArchiveAndRemove(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")

	cookie := e.seedSession("personal")
	e.mustRun("logout", "--archive")
	if e.count("sessions") != 0 {
		t.Errorf("logout --archive should drop the row")
	}
	if _, err := os.Stat(cookie); !os.IsNotExist(err) {
		t.Errorf("logout --archive should move the cookie file")
	}
	if archiveCount(t, e.dataDir) != 1 {
		t.Errorf("logout --archive should produce an archived file")
	}

	cookie2 := e.seedSession("work")
	e.mustRun("logout", "--remove")
	if e.count("sessions") != 0 {
		t.Errorf("logout --remove should drop the row")
	}
	if _, err := os.Stat(cookie2); !os.IsNotExist(err) {
		t.Errorf("logout --remove should delete the cookie file")
	}

	// --archive and --remove are mutually exclusive.
	e.seedSession("third")
	if _, _, err := e.run("logout", "--archive", "--remove"); err == nil {
		t.Error("logout --archive --remove should error")
	}
}
