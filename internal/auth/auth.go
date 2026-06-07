package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SaveCookies serializes an already-parsed cookie slice to a session file and
// records the session in the database. Used by the browser-login flow.
func SaveCookies(db *sql.DB, sessionDir string, cookies []Cookie, account string) (string, error) {
	if account == "" {
		account = "default"
	}
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(sessionDir, account+".cookies.json")
	b, err := json.Marshal(cookies)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	res, err := db.Exec(
		`INSERT INTO accounts(username, created_at, updated_at, last_login_at, session_status) VALUES(?, ?, ?, ?, ?)`,
		account, now, now, now, "browser-login",
	)
	if err != nil {
		return "", err
	}
	accountID, _ := res.LastInsertId()
	_, err = db.Exec(
		`INSERT INTO sessions(account_id, session_type, cookie_file_path, authenticated, created_at, updated_at, last_checked_at) VALUES(?, ?, ?, ?, ?, ?, ?)`,
		accountID, "browser", dst, true, now, now, now,
	)
	return dst, err
}

func ImportCookieFile(db *sql.DB, sessionDir, cookieFile, account string) (string, error) {
	if cookieFile == "" {
		return "", errors.New("cookie file is required")
	}
	if _, err := os.Stat(cookieFile); err != nil {
		return "", err
	}
	if account == "" {
		account = "default"
	}
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(sessionDir, account+".cookies.json")
	b, err := os.ReadFile(cookieFile)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, b, 0o600); err != nil {
		return "", err
	}
	now := time.Now().UTC()
	res, err := db.Exec(`INSERT INTO accounts(username, created_at, updated_at, last_login_at, session_status) VALUES(?, ?, ?, ?, ?)`, account, now, now, now, "imported")
	if err != nil {
		return "", err
	}
	accountID, _ := res.LastInsertId()
	_, err = db.Exec(`INSERT INTO sessions(account_id, session_type, cookie_file_path, authenticated, created_at, updated_at, last_checked_at) VALUES(?, ?, ?, ?, ?, ?, ?)`, accountID, "cookie-file", dst, true, now, now, now)
	return dst, err
}

type StatusResult struct {
	Username       string    `json:"username"`
	CookieFilePath string    `json:"cookieFilePath"`
	LastCheckedAt  time.Time `json:"lastCheckedAt"`
	Authenticated  bool      `json:"authenticated"`
	Exists         bool      `json:"exists"`
}

func Status(db *sql.DB, account string) StatusResult {
	q := `SELECT COALESCE(a.username,''), COALESCE(s.cookie_file_path,''), CAST(COALESCE(s.last_checked_at, s.created_at) AS TEXT), s.authenticated
FROM sessions s LEFT JOIN accounts a ON a.id = s.account_id`
	args := []any{}
	if account != "" {
		q += ` WHERE a.username = ?`
		args = append(args, account)
	}
	q += ` ORDER BY s.updated_at DESC LIMIT 1`
	var username, path, checkedText string
	var ok bool
	if err := db.QueryRow(q, args...).Scan(&username, &path, &checkedText, &ok); err != nil {
		return StatusResult{}
	}
	return StatusResult{
		Username:       username,
		CookieFilePath: path,
		LastCheckedAt:  parseSQLiteTime(checkedText),
		Authenticated:  ok,
		Exists:         true,
	}
}

type RemoteStatus struct {
	Authenticated bool      `json:"authenticated"`
	StatusCode    int       `json:"statusCode"`
	CheckedAt     time.Time `json:"checkedAt"`
	Message       string    `json:"message"`
}

type Cookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Secure   bool    `json:"secure"`
	HTTPOnly bool    `json:"httpOnly"`
	Expires  float64 `json:"expirationDate"`
}

func CheckRemote(ctx context.Context, db *sql.DB, session StatusResult) (RemoteStatus, error) {
	if !session.Exists {
		return RemoteStatus{}, errors.New("AUTH_SESSION_MISSING: no local session found")
	}
	cookies, err := LoadCookies(session.CookieFilePath)
	if err != nil {
		return RemoteStatus{}, err
	}
	hasSessionID := false
	for _, c := range cookies {
		if c.Name == "sessionid" && c.Value != "" {
			hasSessionID = true
			break
		}
	}
	if !hasSessionID {
		return RemoteStatus{}, errors.New("AUTH_SESSION_INVALID: cookie file does not contain a sessionid cookie")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.instagram.com/accounts/edit/", nil)
	if err != nil {
		return RemoteStatus{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cookie", CookieHeader(cookies))

	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return RemoteStatus{}, fmt.Errorf("NETWORK_FAILED: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, 4096)

	checkedAt := time.Now().UTC()
	remote := RemoteStatus{StatusCode: resp.StatusCode, CheckedAt: checkedAt}
	location := resp.Header.Get("Location")
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		remote.Authenticated = true
		remote.Message = "authenticated"
	case resp.StatusCode == http.StatusTooManyRequests:
		remote.Message = "rate limited"
	case resp.StatusCode >= 300 && resp.StatusCode < 400 && strings.Contains(location, "/accounts/login"):
		remote.Message = "redirected to login"
	default:
		remote.Message = resp.Status
	}

	_, _ = db.Exec(`UPDATE sessions SET authenticated = ?, last_checked_at = ?, updated_at = ? WHERE cookie_file_path = ?`, remote.Authenticated, checkedAt, checkedAt, session.CookieFilePath)
	_, _ = db.Exec(`UPDATE accounts SET session_status = ?, updated_at = ? WHERE username = ?`, remote.Message, checkedAt, session.Username)
	return remote, nil
}

func CookieHeader(cookies []Cookie) string {
	parts := make([]string, 0, len(cookies))
	for _, c := range cookies {
		if c.Name == "" || c.Value == "" {
			continue
		}
		if strings.ContainsAny(c.Name, "=\r\n;") || strings.ContainsAny(c.Value, "\r\n;") {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

func LoadCookies(path string) ([]Cookie, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("AUTH_SESSION_MISSING: cannot read cookie file: %w", err)
	}
	return parseCookiesJSON(b)
}

func WriteNetscapeCookieFile(cookies []Cookie, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Netscape HTTP Cookie File\n")
	b.WriteString("# Generated by Gramli from the local session cookie file.\n")
	for _, c := range cookies {
		if c.Name == "" || c.Value == "" {
			continue
		}
		domain := c.Domain
		if domain == "" {
			domain = ".instagram.com"
		}
		includeSubdomains := "FALSE"
		if strings.HasPrefix(domain, ".") {
			includeSubdomains = "TRUE"
		}
		cookiePath := c.Path
		if cookiePath == "" {
			cookiePath = "/"
		}
		secure := "FALSE"
		if c.Secure {
			secure = "TRUE"
		}
		expires := int64(c.Expires)
		if expires < 0 {
			expires = 0
		}
		if strings.ContainsAny(c.Name+c.Value+domain+cookiePath, "\r\n\t") {
			continue
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", domain, includeSubdomains, cookiePath, secure, expires, c.Name, c.Value)
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func parseCookiesJSON(b []byte) ([]Cookie, error) {
	cookies, err := parseCookiesJSONStrict(b)
	if err == nil {
		return cookies, nil
	}
	sanitized := escapeInvalidJSONBackslashes(b)
	if string(sanitized) == string(b) {
		return nil, errors.New("AUTH_SESSION_INVALID: unsupported cookie JSON format")
	}
	cookies, err = parseCookiesJSONStrict(sanitized)
	if err != nil {
		return nil, errors.New("AUTH_SESSION_INVALID: unsupported cookie JSON format")
	}
	return cookies, nil
}

func parseCookiesJSONStrict(b []byte) ([]Cookie, error) {
	var cookies []Cookie
	if err := json.Unmarshal(b, &cookies); err == nil {
		return validCookies(cookies)
	}
	var wrapped struct {
		Cookies []Cookie `json:"cookies"`
	}
	if err := json.Unmarshal(b, &wrapped); err == nil && len(wrapped.Cookies) > 0 {
		return validCookies(wrapped.Cookies)
	}
	var mapCookies map[string]string
	if err := json.Unmarshal(b, &mapCookies); err == nil && len(mapCookies) > 0 {
		for name, value := range mapCookies {
			cookies = append(cookies, Cookie{Name: name, Value: value, Domain: ".instagram.com", Path: "/"})
		}
		return validCookies(cookies)
	}
	return nil, errors.New("AUTH_SESSION_INVALID: unsupported cookie JSON format")
}

func escapeInvalidJSONBackslashes(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' {
			out = append(out, b[i])
			continue
		}
		if i+1 >= len(b) {
			out = append(out, '\\', '\\')
			continue
		}
		switch b[i+1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't', 'u':
			out = append(out, b[i])
		default:
			out = append(out, '\\', '\\')
		}
	}
	return out
}

func validCookies(cookies []Cookie) ([]Cookie, error) {
	out := make([]Cookie, 0, len(cookies))
	for _, c := range cookies {
		if c.Name == "" || c.Value == "" {
			continue
		}
		if c.Path == "" {
			c.Path = "/"
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, errors.New("AUTH_SESSION_INVALID: cookie file contains no usable cookies")
	}
	return out, nil
}

func parseSQLiteTime(value string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t
		}
	}
	var unix int64
	if _, err := fmt.Sscan(value, &unix); err == nil && unix > 0 {
		return time.Unix(unix, 0)
	}
	return time.Time{}
}
