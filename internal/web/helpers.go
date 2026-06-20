package web

import (
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"
)

func funcMap() template.FuncMap {
	return template.FuncMap{
		"humanCount":  humanCount,
		"humanBytes":  humanBytes,
		"pct":         pct,
		"truncate":    truncate,
		"mediaIcon":   mediaIcon,
		"add":         func(a, b int) int { return a + b },
		"sub":         func(a, b int) int { return a - b },
		"max":         func(a, b int) int { return maxInt(a, b) },
		"min":         func(a, b int) int { return minInt(a, b) },
		"queryString": queryString,
		"dict":        dict,
		"bmax":        bmax,
		"date":        fmtDate,
	}
}

// fmtDate renders a nullable timestamp in local time, or "" when absent.
func fmtDate(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

// bmax returns the largest count in a bucket slice, for scaling chart bars.
func bmax(bs []Bucket) int {
	m := 0
	for _, b := range bs {
		if b.Count > m {
			m = b.Count
		}
	}
	return m
}

// humanCount formats an int/int64 (or pointer thereto) compactly (4.2K, 1.5M).
func humanCount(v any) string {
	var n int64
	switch t := v.(type) {
	case int:
		n = int64(t)
	case int64:
		n = t
	case *int64:
		if t == nil {
			return "—"
		}
		n = *t
	case *int:
		if t == nil {
			return "—"
		}
		n = int64(*t)
	case nil:
		return "—"
	default:
		return fmt.Sprintf("%v", v)
	}
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// pct returns part/total as an integer percentage, guarding divide-by-zero.
func pct(part, total int) int {
	if total <= 0 {
		return 0
	}
	return int(float64(part) / float64(total) * 100)
}

func truncate(n int, s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return strings.TrimSpace(string(r[:n])) + "…"
}

func mediaIcon(mediaType string) string {
	switch mediaType {
	case "video":
		return "▶"
	case "album":
		return "▦"
	case "image":
		return "▣"
	default:
		return "•"
	}
}

// queryString builds a gallery query string from the current filters plus an
// override map, so templates can render filter/pagination links.
func queryString(q GalleryQuery, overrides map[string]any) template.URL {
	v := url.Values{}
	set := func(k, val string) {
		if val != "" {
			v.Set(k, val)
		}
	}
	set("collection", q.Collection)
	set("owner", q.Owner)
	set("type", q.MediaType)
	set("status", q.Status)
	set("q", q.Search)
	set("sort", q.Sort)
	set("order", q.Order)
	for k, val := range overrides {
		s := fmt.Sprint(val)
		if s == "" {
			v.Del(k)
		} else {
			v.Set(k, s)
		}
	}
	return template.URL(v.Encode())
}

// dict builds a map from alternating key/value pairs, for passing multiple
// values into a sub-template.
func dict(pairs ...any) map[string]any {
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		key, _ := pairs[i].(string)
		m[key] = pairs[i+1]
	}
	return m
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
