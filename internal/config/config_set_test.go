package config

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSetValueNestedAndTypes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(DefaultConfigYAML(dir)), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetValue(path, "downloads.concurrency", "4"); err != nil {
		t.Fatalf("set int: %v", err)
	}
	if err := SetValue(path, "logging.color", "false"); err != nil {
		t.Fatalf("set bool: %v", err)
	}
	if err := SetValue(path, "app.active_account", "work"); err != nil {
		t.Fatalf("set string: %v", err)
	}
	if err := SetValue(path, "new.nested.key", "hello"); err != nil {
		t.Fatalf("set new nested: %v", err)
	}

	b, _ := os.ReadFile(path)
	var root map[string]any
	if err := yaml.Unmarshal(b, &root); err != nil {
		t.Fatal(err)
	}
	dl := root["downloads"].(map[string]any)
	if dl["concurrency"] != 4 {
		t.Errorf("concurrency = %#v, want int 4", dl["concurrency"])
	}
	lg := root["logging"].(map[string]any)
	if lg["color"] != false {
		t.Errorf("color = %#v, want bool false", lg["color"])
	}
	app := root["app"].(map[string]any)
	if app["active_account"] != "work" {
		t.Errorf("active_account = %#v, want \"work\"", app["active_account"])
	}
	nested := root["new"].(map[string]any)["nested"].(map[string]any)
	if nested["key"] != "hello" {
		t.Errorf("new.nested.key = %#v", nested["key"])
	}
}

func TestInferType(t *testing.T) {
	cases := map[string]any{
		"true":  true,
		"false": false,
		"42":    int64(42),
		"hello": "hello",
		"3.14":  "3.14", // floats stay strings
		"":      "",
	}
	for in, want := range cases {
		if got := inferType(in); got != want {
			t.Errorf("inferType(%q) = %#v, want %#v", in, got, want)
		}
	}
}
