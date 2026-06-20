package auth

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SessionInfo is a queryable view of a stored auth session.
type SessionInfo struct {
	ID             int64     `json:"id"`
	Alias          string    `json:"alias"`
	Type           string    `json:"type"`
	CookieFilePath string    `json:"cookieFilePath"`
	Authenticated  bool      `json:"authenticated"`
	LastChecked    time.Time `json:"lastChecked"`
	FileExists     bool      `json:"fileExists"`
}

// ListSessions returns every stored session, most-recently-updated first.
func ListSessions(db *sql.DB) ([]SessionInfo, error) {
	rows, err := db.Query(`
SELECT s.id, COALESCE(a.username,''), s.session_type, COALESCE(s.cookie_file_path,''),
       s.authenticated, CAST(COALESCE(s.last_checked_at, s.created_at) AS TEXT)
FROM sessions s LEFT JOIN accounts a ON a.id = s.account_id
ORDER BY s.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionInfo
	for rows.Next() {
		var s SessionInfo
		var checked string
		if err := rows.Scan(&s.ID, &s.Alias, &s.Type, &s.CookieFilePath, &s.Authenticated, &checked); err != nil {
			return nil, err
		}
		s.LastChecked = parseSQLiteTime(checked)
		if s.CookieFilePath != "" {
			if _, err := os.Stat(s.CookieFilePath); err == nil {
				s.FileExists = true
			}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ArchiveCookieFile moves a session cookie file into archiveDir under a
// timestamped, alias-prefixed name. It is a no-op (returns "") when the source
// is empty or already gone.
func ArchiveCookieFile(archiveDir, src, alias string, now time.Time) (string, error) {
	if src == "" {
		return "", nil
	}
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	if err := os.MkdirAll(archiveDir, 0o700); err != nil {
		return "", err
	}
	if alias == "" {
		alias = "session"
	}
	dst := filepath.Join(archiveDir, fmt.Sprintf("%s-%d.cookies.json", alias, now.Unix()))
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// DeleteSession removes a session row, optionally deleting its cookie file.
func DeleteSession(db *sql.DB, id int64, cookieFilePath string, removeFile bool) error {
	if removeFile && cookieFilePath != "" {
		if err := os.Remove(cookieFilePath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	_, err := db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}
