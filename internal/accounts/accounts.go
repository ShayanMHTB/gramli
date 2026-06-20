// Package accounts persists account-level Instagram profile metadata into the
// local SQLite store. It builds on the accounts row created at login time,
// enriching it with the data fetched from the web profile endpoint.
package accounts

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/shayanmahtabi/gramli/internal/instagram"
)

// ErrNoAccount is returned when there is no local account row to attach a
// profile to (i.e. the user has not logged in yet).
var ErrNoAccount = errors.New("AUTH_SESSION_MISSING: no local account found; run gramli login first")

// Account is the stored, queryable view of a synced Instagram account.
type Account struct {
	ID              int64      `json:"id"`
	Alias           string     `json:"alias"`
	Handle          string     `json:"handle,omitempty"`
	InstagramuserID string     `json:"instagramUserId,omitempty"`
	FullName        string     `json:"fullName,omitempty"`
	Biography       string     `json:"biography,omitempty"`
	FollowerCount   int64      `json:"followerCount"`
	FollowingCount  int64      `json:"followingCount"`
	MediaCount      int64      `json:"mediaCount"`
	IsPrivate       bool       `json:"isPrivate"`
	IsVerified      bool       `json:"isVerified"`
	ExternalURL     string     `json:"externalUrl,omitempty"`
	Category        string     `json:"category,omitempty"`
	ProfilePicURL   string     `json:"profilePicUrl,omitempty"`
	ProfileSyncedAt *time.Time `json:"profileSyncedAt,omitempty"`
}

// ActiveAccountID returns the account row tied to the most recently updated
// session, optionally filtered by the local alias. This mirrors how auth.Status
// chooses the "current" account.
func ActiveAccountID(db *sql.DB, alias string) (int64, error) {
	q := `SELECT a.id FROM sessions s JOIN accounts a ON a.id = s.account_id`
	args := []any{}
	if alias != "" {
		q += ` WHERE a.username = ?`
		args = append(args, alias)
	}
	q += ` ORDER BY s.updated_at DESC LIMIT 1`
	var id int64
	if err := db.QueryRow(q, args...).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNoAccount
		}
		return 0, err
	}
	return id, nil
}

// SaveProfile writes a fetched profile onto an existing account row.
func SaveProfile(ctx context.Context, db *sql.DB, accountID int64, p instagram.Profile) error {
	now := time.Now().UTC()
	_, err := db.ExecContext(ctx, `
UPDATE accounts SET
  handle = ?,
  instagram_user_id = COALESCE(NULLIF(?, ''), instagram_user_id),
  full_name = ?,
  biography = ?,
  follower_count = ?,
  following_count = ?,
  media_count = ?,
  is_private = ?,
  is_verified = ?,
  external_url = ?,
  category = ?,
  profile_pic_url = COALESCE(NULLIF(?, ''), profile_pic_url),
  profile_synced_at = ?,
  updated_at = ?
WHERE id = ?`,
		p.Username, p.UserID, p.FullName, p.Biography,
		p.FollowerCount, p.FollowingCount, p.MediaCount,
		p.IsPrivate, p.IsVerified, p.ExternalURL, p.Category,
		p.ProfilePicURL, now, now, accountID)
	return err
}

// Get loads the stored account for the given alias (or the active one when
// alias is empty).
func Get(ctx context.Context, db *sql.DB, alias string) (Account, error) {
	q := `SELECT a.id, COALESCE(a.username,''), COALESCE(a.handle,''), COALESCE(a.instagram_user_id,''),
  COALESCE(a.full_name,''), COALESCE(a.biography,''),
  COALESCE(a.follower_count,0), COALESCE(a.following_count,0), COALESCE(a.media_count,0),
  COALESCE(a.is_private,0), COALESCE(a.is_verified,0),
  COALESCE(a.external_url,''), COALESCE(a.category,''), COALESCE(a.profile_pic_url,''),
  CAST(a.profile_synced_at AS TEXT)
FROM sessions s JOIN accounts a ON a.id = s.account_id`
	args := []any{}
	if alias != "" {
		q += ` WHERE a.username = ?`
		args = append(args, alias)
	}
	q += ` ORDER BY s.updated_at DESC LIMIT 1`
	var (
		acc       Account
		syncedRaw sql.NullString
	)
	err := db.QueryRowContext(ctx, q, args...).Scan(
		&acc.ID, &acc.Alias, &acc.Handle, &acc.InstagramuserID,
		&acc.FullName, &acc.Biography,
		&acc.FollowerCount, &acc.FollowingCount, &acc.MediaCount,
		&acc.IsPrivate, &acc.IsVerified,
		&acc.ExternalURL, &acc.Category, &acc.ProfilePicURL,
		&syncedRaw,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Account{}, ErrNoAccount
		}
		return Account{}, err
	}
	if syncedRaw.Valid && syncedRaw.String != "" {
		if t := parseTime(syncedRaw.String); !t.IsZero() {
			acc.ProfileSyncedAt = &t
		}
	}
	return acc, nil
}

func parseTime(value string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	} {
		if t, err := time.Parse(layout, value); err == nil {
			return t
		}
	}
	return time.Time{}
}
