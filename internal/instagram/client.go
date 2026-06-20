package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shayanmahtabi/gramli/internal/auth"
	"github.com/shayanmahtabi/gramli/internal/posts"
)

var ErrMetadataUnavailable = errors.New("metadata unavailable in fetched Instagram page")

// DefaultBaseURL is the production Instagram web origin. Tests override
// Client.BaseURL to point at a local httptest server.
const DefaultBaseURL = "https://www.instagram.com"

type Client struct {
	HTTP       *http.Client
	CookieFile string
	CacheDir   string
	// BaseURL is the origin all requests are sent to. Empty means DefaultBaseURL.
	BaseURL string
}

// url joins the client's base origin with an absolute request path.
func (c *Client) url(path string) string {
	base := c.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	return strings.TrimRight(base, "/") + path
}

type Media struct {
	Index        int
	Type         string
	URL          string
	ThumbnailURL string
}

type Metadata struct {
	Shortcode     string
	PostURL       string
	OwnerUsername string
	Caption       string
	MediaType     string
	ThumbnailURL  string
	RawPath       string
	Media         []Media
}

type SavedPost struct {
	Shortcode     string
	PostURL       string
	OwnerUsername string
	Caption       string
	MediaType     string
	ThumbnailURL  string
	TakenAt       *time.Time
	Media         []Media
}

type SavedPage struct {
	Posts       []SavedPost
	NextMaxID   string
	HasNextPage bool
	RawPath     string
}

func NewClient(cookieFile, cacheDir string) *Client {
	return &Client{
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		CookieFile: cookieFile,
		CacheDir:   cacheDir,
		BaseURL:    DefaultBaseURL,
	}
}

func (c *Client) FetchPost(ctx context.Context, postURL string) (Metadata, error) {
	shortcode, canonicalURL, err := posts.ParseInstagramURL(postURL)
	if err != nil {
		return Metadata{}, err
	}
	cookies, err := auth.LoadCookies(c.CookieFile)
	if err != nil {
		return Metadata{}, err
	}
	// canonicalURL is the stored instagram.com permalink; the fetch target is
	// resolved against BaseURL so tests can redirect it.
	fetchURL := canonicalURL
	if u, perr := url.Parse(canonicalURL); perr == nil {
		fetchURL = c.url(u.Path)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return Metadata{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cookie", auth.CookieHeader(cookies))

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Metadata{}, fmt.Errorf("NETWORK_FAILED: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return Metadata{}, fmt.Errorf("RATE_LIMITED: Instagram returned HTTP 429")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Metadata{}, fmt.Errorf("NETWORK_FAILED: Instagram returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return Metadata{}, err
	}
	rawPath := ""
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0o755); err == nil {
			rawPath = filepath.Join(c.CacheDir, shortcode+".html")
			_ = os.WriteFile(rawPath, body, 0o600)
		}
	}
	meta := ParsePostHTML(shortcode, canonicalURL, string(body))
	meta.RawPath = rawPath
	if meta.MediaType == "unknown" && len(meta.Media) == 0 && meta.OwnerUsername == "" {
		if strings.Contains(string(body), `"pageID":"httpErrorPage"`) || strings.Contains(string(body), `PolarisErrorRoot.entrypoint`) {
			return Metadata{}, fmt.Errorf("%w: Instagram returned an error page for %s", ErrMetadataUnavailable, shortcode)
		}
		return Metadata{}, fmt.Errorf("%w: no media metadata found for %s", ErrMetadataUnavailable, shortcode)
	}
	return meta, nil
}

func (c *Client) FetchSavedPosts(ctx context.Context, maxID string) (SavedPage, error) {
	cookies, err := auth.LoadCookies(c.CookieFile)
	if err != nil {
		return SavedPage{}, err
	}
	endpoint := c.url("/api/v1/feed/saved/posts/")
	if maxID != "" {
		u, _ := url.Parse(endpoint)
		q := u.Query()
		q.Set("max_id", maxID)
		u.RawQuery = q.Encode()
		endpoint = u.String()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return SavedPage{}, err
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
		return SavedPage{}, fmt.Errorf("NETWORK_FAILED: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return SavedPage{}, fmt.Errorf("RATE_LIMITED: Instagram returned HTTP 429")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return SavedPage{}, fmt.Errorf("NETWORK_FAILED: Instagram returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return SavedPage{}, err
	}
	rawPath := ""
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0o755); err == nil {
			name := "saved-first.json"
			if maxID != "" {
				name = "saved-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".json"
			}
			rawPath = filepath.Join(c.CacheDir, name)
			_ = os.WriteFile(rawPath, body, 0o600)
		}
	}
	page, err := ParseSavedPostsJSON(body)
	if err != nil {
		return SavedPage{}, err
	}
	page.RawPath = rawPath
	return page, nil
}

// FetchUserFeed pages through a single user's own timeline media via
// /api/v1/feed/user/{id}/. The response shares the saved-feed item shape, so it
// reuses ParseSavedPostsJSON; pass the authenticated account's ds_user_id (or
// the stored instagram_user_id) to fetch your own posts and reels.
func (c *Client) FetchUserFeed(ctx context.Context, userID, maxID string) (SavedPage, error) {
	if userID == "" {
		return SavedPage{}, fmt.Errorf("USER_ID_MISSING: cannot fetch own posts without an Instagram user id")
	}
	cookies, err := auth.LoadCookies(c.CookieFile)
	if err != nil {
		return SavedPage{}, err
	}
	endpoint := c.url("/api/v1/feed/user/" + userID + "/")
	if maxID != "" {
		u, _ := url.Parse(endpoint)
		q := u.Query()
		q.Set("max_id", maxID)
		u.RawQuery = q.Encode()
		endpoint = u.String()
	}
	body, err := c.getJSON(ctx, endpoint, cookies)
	if err != nil {
		return SavedPage{}, err
	}
	rawPath := ""
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0o755); err == nil {
			name := "own-first.json"
			if maxID != "" {
				name = "own-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".json"
			}
			rawPath = filepath.Join(c.CacheDir, name)
			_ = os.WriteFile(rawPath, body, 0o600)
		}
	}
	page, err := ParseSavedPostsJSON(body)
	if err != nil {
		return SavedPage{}, err
	}
	page.RawPath = rawPath
	return page, nil
}

func ParseSavedPostsJSON(body []byte) (SavedPage, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return SavedPage{}, err
	}
	items, _ := root["items"].([]any)
	page := SavedPage{
		NextMaxID:   stringValue(root["next_max_id"]),
		HasNextPage: boolValue(root["more_available"]) || boolValue(root["has_more"]),
	}
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		mediaObj := obj
		if nested, ok := obj["media"].(map[string]any); ok {
			mediaObj = nested
		}
		post := savedPostFromMedia(mediaObj)
		if post.Shortcode == "" {
			continue
		}
		page.Posts = append(page.Posts, post)
	}
	if page.NextMaxID == "" {
		page.HasNextPage = false
	}
	return page, nil
}

func savedPostFromMedia(media map[string]any) SavedPost {
	shortcode := first(stringValue(media["code"]), stringValue(media["shortcode"]))
	if shortcode == "" {
		return SavedPost{}
	}
	owner := ""
	if user, ok := media["user"].(map[string]any); ok {
		owner = stringValue(user["username"])
	}
	caption := ""
	if capObj, ok := media["caption"].(map[string]any); ok {
		caption = stringValue(capObj["text"])
	}
	mediaType := mediaTypeName(media["media_type"])
	thumb := ""
	if imageVersions, ok := media["image_versions2"].(map[string]any); ok {
		if candidates, ok := imageVersions["candidates"].([]any); ok && len(candidates) > 0 {
			if firstCandidate, ok := candidates[0].(map[string]any); ok {
				thumb = stringValue(firstCandidate["url"])
			}
		}
	}
	mediaItems := mediaItemsFromSavedMedia(media, mediaType, thumb)
	return SavedPost{
		Shortcode:     shortcode,
		PostURL:       "https://www.instagram.com/p/" + shortcode + "/",
		OwnerUsername: owner,
		Caption:       caption,
		MediaType:     mediaType,
		ThumbnailURL:  thumb,
		TakenAt:       unixTime(media["taken_at"]),
		Media:         mediaItems,
	}
}

// unixTime converts Instagram's `taken_at` (Unix seconds) into a UTC time, or
// nil when the value is absent or non-positive.
func unixTime(value any) *time.Time {
	secs := int64Value(value)
	if secs <= 0 {
		return nil
	}
	t := time.Unix(secs, 0).UTC()
	return &t
}

func mediaItemsFromSavedMedia(media map[string]any, mediaType, thumb string) []Media {
	if mediaType == "album" {
		children, _ := media["carousel_media"].([]any)
		out := make([]Media, 0, len(children))
		for i, child := range children {
			childMap, ok := child.(map[string]any)
			if !ok {
				continue
			}
			childType := mediaTypeName(childMap["media_type"])
			childThumb := bestImageURL(childMap)
			remote := childThumb
			if childType == "video" {
				remote = bestVideoURL(childMap)
			}
			if remote == "" {
				continue
			}
			out = append(out, Media{Index: i + 1, Type: childType, URL: remote, ThumbnailURL: childThumb})
		}
		return out
	}
	if mediaType == "video" {
		if videoURL := bestVideoURL(media); videoURL != "" {
			return []Media{{Index: 1, Type: "video", URL: videoURL, ThumbnailURL: thumb}}
		}
	}
	if thumb != "" {
		return []Media{{Index: 1, Type: "image", URL: thumb, ThumbnailURL: thumb}}
	}
	return nil
}

func bestImageURL(media map[string]any) string {
	imageVersions, ok := media["image_versions2"].(map[string]any)
	if !ok {
		return ""
	}
	candidates, ok := imageVersions["candidates"].([]any)
	if !ok || len(candidates) == 0 {
		return ""
	}
	if firstCandidate, ok := candidates[0].(map[string]any); ok {
		return stringValue(firstCandidate["url"])
	}
	return ""
}

func bestVideoURL(media map[string]any) string {
	versions, ok := media["video_versions"].([]any)
	if !ok || len(versions) == 0 {
		return ""
	}
	if firstVersion, ok := versions[0].(map[string]any); ok {
		return stringValue(firstVersion["url"])
	}
	return ""
}

func mediaTypeName(value any) string {
	switch intValue(value) {
	case 1:
		return "image"
	case 2:
		return "video"
	case 8:
		return "album"
	default:
		return "unknown"
	}
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatInt(int64(v), 10)
	default:
		return ""
	}
}

func boolValue(value any) bool {
	v, _ := value.(bool)
	return v
}

func intValue(value any) int {
	switch v := value.(type) {
	case float64:
		return int(v)
	case json.Number:
		i, _ := strconv.Atoi(v.String())
		return i
	case int:
		return v
	default:
		return 0
	}
}

var metaTagRE = regexp.MustCompile(`(?is)<meta\s+[^>]*(?:property|name)=["']([^"']+)["'][^>]*content=["']([^"']*)["'][^>]*>`)

func ParsePostHTML(shortcode, postURL, body string) Metadata {
	values := map[string]string{}
	for _, match := range metaTagRE.FindAllStringSubmatch(body, -1) {
		key := strings.ToLower(strings.TrimSpace(match[1]))
		if _, ok := values[key]; !ok {
			values[key] = html.UnescapeString(match[2])
		}
	}
	imageURL := first(values["og:image"], values["twitter:image"])
	videoURL := first(values["og:video"], values["og:video:url"], values["og:video:secure_url"])
	description := first(values["og:description"], values["description"], values["twitter:description"])
	title := first(values["og:title"], values["twitter:title"])

	mediaType := "unknown"
	media := []Media{}
	if videoURL != "" {
		mediaType = "video"
		media = append(media, Media{Index: 1, Type: "video", URL: videoURL, ThumbnailURL: imageURL})
	} else if imageURL != "" {
		mediaType = "image"
		media = append(media, Media{Index: 1, Type: "image", URL: imageURL, ThumbnailURL: imageURL})
	}
	return Metadata{
		Shortcode:     shortcode,
		PostURL:       postURL,
		OwnerUsername: parseOwner(title, description),
		Caption:       description,
		MediaType:     mediaType,
		ThumbnailURL:  imageURL,
		Media:         media,
	}
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseOwner(title, description string) string {
	for _, text := range []string{title, description} {
		text = strings.TrimSpace(text)
		for _, marker := range []string{"Instagram photo by ", "Instagram video by ", "Instagram reel by "} {
			if strings.HasPrefix(text, marker) {
				rest := strings.TrimPrefix(text, marker)
				return cleanOwner(strings.Split(rest, " ")[0])
			}
		}
		if idx := strings.Index(text, " on Instagram"); idx > 0 {
			return cleanOwner(text[:idx])
		}
	}
	return ""
}

func cleanOwner(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "@")
	value = strings.Trim(value, `"'():,`)
	return value
}
