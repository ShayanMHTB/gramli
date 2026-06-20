package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/shayanmahtabi/gramli/internal/auth"
)

// ErrSelfUsernameUnknown is returned when Gramli cannot determine the handle of
// the logged-in account from the current session. The caller should ask the
// user to pass an explicit --username.
var ErrSelfUsernameUnknown = errors.New("could not detect the logged-in username from the current session")

// Profile holds the account-level metadata Gramli stores for an Instagram user.
// It is populated from the web profile endpoint and is the foundation for later
// fetches of that user's own posts, reels, and stories.
type Profile struct {
	UserID         string `json:"userId"`
	Username       string `json:"username"`
	FullName       string `json:"fullName"`
	Biography      string `json:"biography"`
	FollowerCount  int64  `json:"followerCount"`
	FollowingCount int64  `json:"followingCount"`
	MediaCount     int64  `json:"mediaCount"`
	IsPrivate      bool   `json:"isPrivate"`
	IsVerified     bool   `json:"isVerified"`
	ExternalURL    string `json:"externalUrl"`
	Category       string `json:"category"`
	ProfilePicURL  string `json:"profilePicUrl"`
	RawPath        string `json:"rawPath,omitempty"`
}

// FetchSelfProfile resolves the logged-in account's handle from the session and
// returns its full profile. It is the canonical "who am I" call.
func (c *Client) FetchSelfProfile(ctx context.Context) (Profile, error) {
	username, err := c.DetectSelfUsername(ctx)
	if err != nil {
		return Profile{}, err
	}
	return c.FetchProfileByUsername(ctx, username)
}

// DetectSelfUsername asks Instagram which account the current cookies belong to.
// It tries the current_user endpoint first, then resolves the ds_user_id cookie
// via the user-info endpoint, before giving up with ErrSelfUsernameUnknown.
func (c *Client) DetectSelfUsername(ctx context.Context) (string, error) {
	cookies, err := auth.LoadCookies(c.CookieFile)
	if err != nil {
		return "", err
	}
	if body, err := c.getJSON(ctx, c.url("/api/v1/accounts/current_user/"), cookies); err == nil {
		if username, _ := ParseCurrentUserJSON(body); username != "" {
			return username, nil
		}
	}
	if pk := dsUserID(cookies); pk != "" {
		if body, err := c.getJSON(ctx, c.url("/api/v1/users/"+pk+"/info/"), cookies); err == nil {
			if username, _ := ParseCurrentUserJSON(body); username != "" {
				return username, nil
			}
		}
	}
	return "", ErrSelfUsernameUnknown
}

// dsUserID extracts the numeric account id from the ds_user_id session cookie.
func dsUserID(cookies []auth.Cookie) string {
	for _, ck := range cookies {
		if ck.Name == "ds_user_id" {
			return ck.Value
		}
	}
	return ""
}

// FetchProfileByUsername returns the profile for any account visible to the
// authenticated session via the web profile endpoint.
func (c *Client) FetchProfileByUsername(ctx context.Context, username string) (Profile, error) {
	cookies, err := auth.LoadCookies(c.CookieFile)
	if err != nil {
		return Profile{}, err
	}
	endpoint := c.url("/api/v1/users/web_profile_info/?username=" + url.QueryEscape(username))
	body, err := c.getJSON(ctx, endpoint, cookies)
	if err != nil {
		return Profile{}, err
	}
	rawPath := ""
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0o755); err == nil {
			rawPath = filepath.Join(c.CacheDir, "profile-"+username+".json")
			_ = os.WriteFile(rawPath, body, 0o600)
		}
	}
	profile, err := ParseWebProfileInfoJSON(body)
	if err != nil {
		return Profile{}, err
	}
	profile.RawPath = rawPath
	return profile, nil
}

// getJSON performs an authenticated GET against an Instagram private web API
// endpoint using the same header set the saved-posts sync relies on.
func (c *Client) getJSON(ctx context.Context, endpoint string, cookies []auth.Cookie) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", "https://www.instagram.com/")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-IG-App-ID", "936619743392459")
	for _, cookie := range cookies {
		if cookie.Name == "csrftoken" && cookie.Value != "" {
			req.Header.Set("X-CSRFToken", cookie.Value)
			break
		}
	}
	req.Header.Set("Cookie", auth.CookieHeader(cookies))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("NETWORK_FAILED: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("RATE_LIMITED: Instagram returned HTTP 429")
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("PROFILE_NOT_FOUND: Instagram returned %s", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("NETWORK_FAILED: Instagram returned %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// ParseCurrentUserJSON extracts the logged-in username from the response of
// /api/v1/accounts/current_user/.
func ParseCurrentUserJSON(body []byte) (string, error) {
	var root struct {
		User map[string]any `json:"user"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return "", err
	}
	return stringValue(root.User["username"]), nil
}

// ParseWebProfileInfoJSON converts a /api/v1/users/web_profile_info/ response
// into a Profile.
func ParseWebProfileInfoJSON(body []byte) (Profile, error) {
	var root struct {
		Data struct {
			User map[string]any `json:"user"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &root); err != nil {
		return Profile{}, err
	}
	user := root.Data.User
	if len(user) == 0 {
		return Profile{}, fmt.Errorf("%w: profile payload contained no user", ErrMetadataUnavailable)
	}
	profile := Profile{
		UserID:         stringValue(user["id"]),
		Username:       stringValue(user["username"]),
		FullName:       stringValue(user["full_name"]),
		Biography:      stringValue(user["biography"]),
		FollowerCount:  edgeCount(user["edge_followed_by"]),
		FollowingCount: edgeCount(user["edge_follow"]),
		MediaCount:     edgeCount(user["edge_owner_to_timeline_media"]),
		IsPrivate:      boolValue(user["is_private"]),
		IsVerified:     boolValue(user["is_verified"]),
		ExternalURL:    stringValue(user["external_url"]),
		Category:       first(stringValue(user["category_name"]), stringValue(user["category"])),
		ProfilePicURL:  first(stringValue(user["profile_pic_url_hd"]), stringValue(user["profile_pic_url"])),
	}
	if profile.Username == "" {
		return Profile{}, fmt.Errorf("%w: profile payload missing username", ErrMetadataUnavailable)
	}
	return profile, nil
}

// edgeCount reads the {"count": N} shape Instagram uses for follower/following/
// media tallies.
func edgeCount(value any) int64 {
	edge, ok := value.(map[string]any)
	if !ok {
		return 0
	}
	return int64Value(edge["count"])
}

func int64Value(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case json.Number:
		n, _ := strconv.ParseInt(v.String(), 10, 64)
		return n
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	default:
		return 0
	}
}
