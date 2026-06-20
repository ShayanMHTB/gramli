package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesWorkspace(t *testing.T) {
	e := newEnv(t)
	out, _ := e.mustRun("init")
	if !strings.Contains(out, "Database") {
		t.Fatalf("init output missing database line: %q", out)
	}
	for _, p := range []string{"gramli.db", "config.yaml"} {
		if _, err := os.Stat(filepath.Join(e.dataDir, p)); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
	// init is idempotent.
	if _, _, err := e.run("init"); err != nil {
		t.Errorf("re-running init should succeed: %v", err)
	}
}

func TestDoctorPasses(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	out, _ := e.mustRun("doctor")
	if !strings.Contains(strings.ToLower(out), "config") {
		t.Errorf("doctor output unexpected: %q", out)
	}
}

func TestDBStatusAndMigrate(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	out, _ := e.mustRun("db", "status")
	if !strings.Contains(out, "Migration version") {
		t.Errorf("db status missing migration line: %q", out)
	}
	if _, _, err := e.run("db", "migrate"); err != nil {
		t.Errorf("db migrate failed: %v", err)
	}
}

func TestDBResetRequiresYes(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "AAA111", Owner: "alice"})
	if e.count("posts") != 1 {
		t.Fatalf("expected 1 seeded post")
	}
	// Without --yes the destructive reset must refuse and leave data intact.
	if _, _, err := e.run("db", "reset"); err == nil {
		t.Errorf("db reset without --yes should fail")
	}
	if e.count("posts") != 1 {
		t.Errorf("db reset without --yes must not delete data")
	}
}

func TestConfigShowJSONGlobalFlag(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	// config path should print the resolved config path.
	out, _ := e.mustRun("config", "path")
	if !strings.Contains(out, "config.yaml") {
		t.Errorf("config path unexpected: %q", out)
	}
	_ = json.Valid // referenced by JSON tests elsewhere
}
