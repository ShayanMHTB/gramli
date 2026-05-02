package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	*sql.DB
	Path string
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;`); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &DB{DB: db, Path: path}, nil
}

func (db *DB) Migrate() error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at DATETIME NOT NULL);`); err != nil {
		return err
	}
	var current int
	_ = db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current)
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(m.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d failed: %w", m.version, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, m.version, time.Now().UTC()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) MigrationVersion() int {
	var v int
	_ = db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&v)
	return v
}

func (db *DB) Count(table string) int {
	var n int
	_ = db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&n)
	return n
}

type migration struct {
	version int
	sql     string
}

var migrations = []migration{{
	version: 1,
	sql: `
CREATE TABLE IF NOT EXISTS accounts (
  id INTEGER PRIMARY KEY,
  instagram_user_id TEXT,
  username TEXT,
  full_name TEXT,
  profile_pic_url TEXT,
  is_private BOOLEAN,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  last_login_at DATETIME,
  session_status TEXT
);
CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY,
  account_id INTEGER,
  session_type TEXT NOT NULL,
  cookie_file_path TEXT,
  authenticated BOOLEAN NOT NULL DEFAULT 0,
  expires_at DATETIME NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  last_checked_at DATETIME,
  FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE SET NULL
);
CREATE TABLE IF NOT EXISTS posts (
  id INTEGER PRIMARY KEY,
  instagram_post_id TEXT UNIQUE,
  shortcode TEXT UNIQUE NOT NULL,
  post_url TEXT NOT NULL,
  owner_username TEXT,
  owner_id TEXT,
  caption TEXT,
  media_type TEXT,
  is_video BOOLEAN NOT NULL DEFAULT 0,
  is_album BOOLEAN NOT NULL DEFAULT 0,
  taken_at DATETIME NULL,
  saved_at DATETIME NULL,
  discovered_at DATETIME NOT NULL,
  last_seen_at DATETIME NOT NULL,
  thumbnail_url TEXT,
  like_count INTEGER NULL,
  comment_count INTEGER NULL,
  source TEXT NOT NULL,
  raw_json_path TEXT,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS media (
  id INTEGER PRIMARY KEY,
  post_id INTEGER NOT NULL,
  media_index INTEGER NOT NULL DEFAULT 1,
  media_type TEXT,
  remote_url TEXT,
  local_path TEXT,
  thumbnail_url TEXT,
  width INTEGER NULL,
  height INTEGER NULL,
  duration_seconds REAL NULL,
  file_size_bytes INTEGER NULL,
  download_status TEXT NOT NULL DEFAULT 'pending',
  checksum TEXT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_media_post_index ON media(post_id, media_index);
CREATE TABLE IF NOT EXISTS collections (
  id INTEGER PRIMARY KEY,
  instagram_collection_id TEXT UNIQUE NULL,
  name TEXT NOT NULL,
  slug TEXT UNIQUE NOT NULL,
  description TEXT NULL,
  discovered_at DATETIME NOT NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS post_collections (
  post_id INTEGER NOT NULL,
  collection_id INTEGER NOT NULL,
  added_at DATETIME NULL,
  PRIMARY KEY(post_id, collection_id),
  FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
  FOREIGN KEY(collection_id) REFERENCES collections(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS downloads (
  id INTEGER PRIMARY KEY,
  post_id INTEGER NOT NULL,
  media_id INTEGER NULL,
  status TEXT NOT NULL,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NULL,
  destination_path TEXT,
  started_at DATETIME NULL,
  completed_at DATETIME NULL,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL,
  FOREIGN KEY(post_id) REFERENCES posts(id) ON DELETE CASCADE,
  FOREIGN KEY(media_id) REFERENCES media(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS idx_posts_owner ON posts(owner_username);
CREATE INDEX IF NOT EXISTS idx_posts_source ON posts(source);
CREATE INDEX IF NOT EXISTS idx_media_post ON media(post_id);
`,
}, {
	version: 2,
	sql: `
CREATE UNIQUE INDEX IF NOT EXISTS idx_media_post_index ON media(post_id, media_index);
`,
}}
