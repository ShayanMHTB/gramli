package cli

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shayanmahtabi/gramli/internal/storage"
)

// env is a hermetic test environment: a temp data dir plus helpers to run the
// CLI and seed/inspect the database. Nothing touches the real ./.gramli.
type env struct {
	t       *testing.T
	dataDir string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	return &env{t: t, dataDir: t.TempDir()}
}

// run executes the root command with --data-dir pinned to the temp dir and
// returns captured stdout, stderr, and the command error.
func (e *env) run(args ...string) (stdout, stderr string, err error) {
	e.t.Helper()
	root := NewRootCommand()
	var out, errb bytes.Buffer
	full := append([]string{"--data-dir", e.dataDir, "--quiet"}, args...)
	root.SetArgs(full)
	root.SetOut(&out)
	root.SetErr(&errb)
	err = root.Execute()
	return out.String(), errb.String(), err
}

// runStdin is like run but feeds the given string as the command's stdin.
func (e *env) runStdin(input string, args ...string) (stdout, stderr string, err error) {
	e.t.Helper()
	root := NewRootCommand()
	var out, errb bytes.Buffer
	full := append([]string{"--data-dir", e.dataDir, "--quiet"}, args...)
	root.SetArgs(full)
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetIn(bytes.NewBufferString(input))
	err = root.Execute()
	return out.String(), errb.String(), err
}

// mustRun fails the test if the command returns an error.
func (e *env) mustRun(args ...string) (stdout, stderr string) {
	e.t.Helper()
	out, errb, err := e.run(args...)
	if err != nil {
		e.t.Fatalf("command %v failed: %v\nstderr: %s", args, err, errb)
	}
	return out, errb
}

func (e *env) dbPath() string { return filepath.Join(e.dataDir, "gramli.db") }

// openDB opens the migrated test database for seeding or assertions.
func (e *env) openDB() *storage.DB {
	e.t.Helper()
	db, err := storage.Open(e.dbPath())
	if err != nil {
		e.t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		e.t.Fatalf("migrate: %v", err)
	}
	return db
}

func (e *env) count(table string) int {
	db := e.openDB()
	defer db.Close()
	return db.Count(table)
}

// seedOpts describes a synthetic post to insert.
type seedOpts struct {
	Shortcode  string
	Owner      string
	Caption    string
	MediaType  string // image | video | album
	Source     string // saved | manual-import
	Downloaded bool
	Collection string
}

// seedPost inserts a post with one media row (and optional collection), used to
// give read commands realistic data to operate on.
func (e *env) seedPost(o seedOpts) {
	e.t.Helper()
	db := e.openDB()
	defer db.Close()
	now := time.Now().UTC()
	if o.MediaType == "" {
		o.MediaType = "image"
	}
	if o.Source == "" {
		o.Source = "saved"
	}
	url := "https://www.instagram.com/p/" + o.Shortcode + "/"
	res, err := db.Exec(`INSERT INTO posts(shortcode, post_url, owner_username, caption, media_type, is_video, is_album, discovered_at, last_seen_at, source, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		o.Shortcode, url, o.Owner, o.Caption, o.MediaType, o.MediaType == "video", o.MediaType == "album", now, now, o.Source, now, now)
	if err != nil {
		e.t.Fatalf("seed post: %v", err)
	}
	postID, _ := res.LastInsertId()
	status := "pending"
	if o.Downloaded {
		status = "downloaded"
	}
	if _, err := db.Exec(`INSERT INTO media(post_id, media_index, media_type, remote_url, download_status, file_size_bytes, created_at, updated_at)
		VALUES(?,1,?,?,?,?,?,?)`, postID, o.MediaType, "https://cdn.example/"+o.Shortcode+".jpg", status, 1024, now, now); err != nil {
		e.t.Fatalf("seed media: %v", err)
	}
	if o.Collection != "" {
		e.attachCollection(db.DB, postID, o.Collection, now)
	}
}

// seedOrphanPost inserts a post with no media rows (a clean target).
func (e *env) seedOrphanPost(shortcode, source string) {
	e.t.Helper()
	db := e.openDB()
	defer db.Close()
	now := time.Now().UTC()
	if source == "" {
		source = "manual-import"
	}
	if _, err := db.Exec(`INSERT INTO posts(shortcode, post_url, media_type, discovered_at, last_seen_at, source, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?)`, shortcode, "https://www.instagram.com/p/"+shortcode+"/", "unknown", now, now, source, now, now); err != nil {
		e.t.Fatalf("seed orphan: %v", err)
	}
}

// setMediaStatus forces the download_status of a post's media rows.
func (e *env) setMediaStatus(shortcode, status string) {
	e.t.Helper()
	db := e.openDB()
	defer db.Close()
	if _, err := db.Exec(`UPDATE media SET download_status=? WHERE post_id=(SELECT id FROM posts WHERE shortcode=?)`, status, shortcode); err != nil {
		e.t.Fatalf("set media status: %v", err)
	}
}

// seedSession inserts an account + session row and writes a cookie file on disk,
// returning the cookie file path. Used to exercise auth/account/logout paths.
func (e *env) seedSession(alias string) string {
	e.t.Helper()
	sessionDir := filepath.Join(e.dataDir, "sessions")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		e.t.Fatalf("mkdir sessions: %v", err)
	}
	cookiePath := filepath.Join(sessionDir, alias+".cookies.json")
	if err := os.WriteFile(cookiePath, []byte(`[{"name":"sessionid","value":"x"},{"name":"ds_user_id","value":"42"}]`), 0o600); err != nil {
		e.t.Fatalf("write cookie: %v", err)
	}
	db := e.openDB()
	defer db.Close()
	now := time.Now().UTC()
	res, err := db.Exec(`INSERT INTO accounts(username, created_at, updated_at, last_login_at, session_status) VALUES(?,?,?,?,?)`, alias, now, now, now, "imported")
	if err != nil {
		e.t.Fatalf("seed account: %v", err)
	}
	accountID, _ := res.LastInsertId()
	if _, err := db.Exec(`INSERT INTO sessions(account_id, session_type, cookie_file_path, authenticated, created_at, updated_at, last_checked_at) VALUES(?,?,?,?,?,?,?)`,
		accountID, "cookie-file", cookiePath, true, now, now, now); err != nil {
		e.t.Fatalf("seed session: %v", err)
	}
	return cookiePath
}

func (e *env) attachCollection(db *sql.DB, postID int64, name string, now time.Time) {
	e.t.Helper()
	var colID int64
	err := db.QueryRow(`SELECT id FROM collections WHERE slug=? OR name=?`, name, name).Scan(&colID)
	if err == sql.ErrNoRows {
		res, ierr := db.Exec(`INSERT INTO collections(name, slug, discovered_at, created_at, updated_at) VALUES(?,?,?,?,?)`, name, name, now, now, now)
		if ierr != nil {
			e.t.Fatalf("seed collection: %v", ierr)
		}
		colID, _ = res.LastInsertId()
	} else if err != nil {
		e.t.Fatalf("lookup collection: %v", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO post_collections(post_id, collection_id, added_at) VALUES(?,?,?)`, postID, colID, now); err != nil {
		e.t.Fatalf("attach collection: %v", err)
	}
}
