package storage

import "testing"

// TestFTS5Available guards that the pure-Go sqlite driver keeps FTS5 compiled
// in, since search depends on it.
func TestFTS5Available(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir + "/probe.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE VIRTUAL TABLE ft USING fts5(content)`); err != nil {
		t.Fatalf("FTS5 not available: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO ft(content) VALUES('hello world')`); err != nil {
		t.Fatalf("FTS5 insert: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM ft WHERE ft MATCH 'world'`).Scan(&n); err != nil {
		t.Fatalf("FTS5 match: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 match, got %d", n)
	}
}
