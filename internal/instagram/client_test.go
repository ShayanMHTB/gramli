package instagram

import "testing"

func TestParsePostHTML(t *testing.T) {
	t.Parallel()
	body := `<html><head>
<meta property="og:title" content="Instagram photo by someowner">
<meta property="og:description" content="A useful caption">
<meta property="og:image" content="https://cdn.example/image.jpg">
</head></html>`
	got := ParsePostHTML("ABC123", "https://www.instagram.com/p/ABC123/", body)
	if got.OwnerUsername != "someowner" {
		t.Fatalf("OwnerUsername = %q", got.OwnerUsername)
	}
	if got.MediaType != "image" {
		t.Fatalf("MediaType = %q", got.MediaType)
	}
	if len(got.Media) != 1 || got.Media[0].URL != "https://cdn.example/image.jpg" {
		t.Fatalf("Media = %#v", got.Media)
	}
}

func TestParsePostHTMLUnknownForErrorShell(t *testing.T) {
	t.Parallel()
	got := ParsePostHTML("ABC123", "https://www.instagram.com/p/ABC123/", `{"pageID":"httpErrorPage"}`)
	if got.MediaType != "unknown" || len(got.Media) != 0 {
		t.Fatalf("unexpected metadata: %#v", got)
	}
}

func TestParseSavedPostsJSON(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"items": [{
			"media": {
				"code": "ABC123",
				"media_type": 2,
				"user": {"username": "ownername"},
				"caption": {"text": "caption text"},
				"image_versions2": {"candidates": [{"url": "https://cdn.example/thumb.jpg"}]}
			}
		}],
		"more_available": true,
		"next_max_id": "NEXT"
	}`)
	page, err := ParseSavedPostsJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Posts) != 1 {
		t.Fatalf("post count = %d", len(page.Posts))
	}
	post := page.Posts[0]
	if post.Shortcode != "ABC123" || post.OwnerUsername != "ownername" || post.MediaType != "video" {
		t.Fatalf("unexpected post: %#v", post)
	}
	if len(post.Media) != 1 || post.Media[0].Type != "image" {
		t.Fatalf("unexpected media: %#v", post.Media)
	}
	if !page.HasNextPage || page.NextMaxID != "NEXT" {
		t.Fatalf("unexpected pagination: %#v", page)
	}
}
