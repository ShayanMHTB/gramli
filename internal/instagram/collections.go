package instagram

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/shayanmahtabi/gramli/internal/auth"
)

// Collection is a saved collection as returned by Instagram's collections list.
type Collection struct {
	ID         string
	Name       string
	MediaCount int
}

// FetchCollections returns the authenticated user's saved collections. The
// exact response shape is best-effort (this is a private web endpoint); the
// parser tolerates missing fields.
func (c *Client) FetchCollections(ctx context.Context) ([]Collection, error) {
	cookies, err := auth.LoadCookies(c.CookieFile)
	if err != nil {
		return nil, err
	}
	body, err := c.getJSON(ctx, c.url("/api/v1/collections/list/"), cookies)
	if err != nil {
		return nil, err
	}
	if c.CacheDir != "" {
		if err := os.MkdirAll(c.CacheDir, 0o755); err == nil {
			_ = os.WriteFile(filepath.Join(c.CacheDir, "collections.json"), body, 0o600)
		}
	}
	return ParseCollectionsJSON(body)
}

// ParseCollectionsJSON extracts collections from a /api/v1/collections/list/
// response.
func ParseCollectionsJSON(body []byte) ([]Collection, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	items, _ := root["items"].([]any)
	out := make([]Collection, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		col := Collection{
			ID:         first(stringValue(m["collection_id"]), stringValue(m["id"])),
			Name:       first(stringValue(m["collection_name"]), stringValue(m["name"])),
			MediaCount: intValue(m["collection_media_count"]),
		}
		if col.ID == "" && col.Name == "" {
			continue
		}
		out = append(out, col)
	}
	return out, nil
}
