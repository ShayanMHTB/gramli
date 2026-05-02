package exporter

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/shayanmahtabi/gramli/internal/posts"
	"github.com/shayanmahtabi/gramli/internal/version"
)

func JSON(w io.Writer, data []posts.Post, pretty bool) error {
	payload := struct {
		ExportedAt time.Time    `json:"exportedAt"`
		Version    string       `json:"version"`
		Posts      []posts.Post `json:"posts"`
	}{time.Now(), version.Version, data}
	enc := json.NewEncoder(w)
	if pretty {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(payload)
}

func CSV(w io.Writer, data []posts.Post) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"shortcode", "owner", "type", "downloaded", "url"}); err != nil {
		return err
	}
	for _, p := range data {
		if err := cw.Write([]string{p.Shortcode, p.OwnerUsername, p.MediaType, fmt.Sprint(p.Downloaded), p.PostURL}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
