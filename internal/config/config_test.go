package config

import "testing"

func TestResolveDefaults(t *testing.T) {
	t.Parallel()
	got := Resolve(Settings{})
	if got.DataDir != "./.gramli" {
		t.Fatalf("DataDir = %q", got.DataDir)
	}
	if got.ConfigPath != ".gramli/config.yaml" {
		t.Fatalf("ConfigPath = %q", got.ConfigPath)
	}
	if got.DBPath != ".gramli/gramli.db" {
		t.Fatalf("DBPath = %q", got.DBPath)
	}
	if got.LogFile != ".gramli/logs/gramli.log" {
		t.Fatalf("LogFile = %q", got.LogFile)
	}
}
