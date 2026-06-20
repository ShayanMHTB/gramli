package instagram

import "testing"

const webProfileInfoFixture = `{
  "data": {
    "user": {
      "id": "1234567890",
      "username": "gramli_test",
      "full_name": "Gramli Test",
      "biography": "Local-first archival.\nLine two.",
      "external_url": "https://example.com",
      "category_name": "Software",
      "is_private": false,
      "is_verified": true,
      "profile_pic_url": "https://example.com/pic.jpg",
      "profile_pic_url_hd": "https://example.com/pic_hd.jpg",
      "edge_followed_by": {"count": 4200},
      "edge_follow": {"count": 311},
      "edge_owner_to_timeline_media": {"count": 87}
    }
  }
}`

func TestParseWebProfileInfoJSON(t *testing.T) {
	got, err := ParseWebProfileInfoJSON([]byte(webProfileInfoFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := Profile{
		UserID:         "1234567890",
		Username:       "gramli_test",
		FullName:       "Gramli Test",
		Biography:      "Local-first archival.\nLine two.",
		FollowerCount:  4200,
		FollowingCount: 311,
		MediaCount:     87,
		IsPrivate:      false,
		IsVerified:     true,
		ExternalURL:    "https://example.com",
		Category:       "Software",
		ProfilePicURL:  "https://example.com/pic_hd.jpg",
	}
	if got != want {
		t.Fatalf("profile mismatch\n got: %+v\nwant: %+v", got, want)
	}
}

func TestParseWebProfileInfoJSONMissingUser(t *testing.T) {
	if _, err := ParseWebProfileInfoJSON([]byte(`{"data": {}}`)); err == nil {
		t.Fatal("expected error for empty user payload")
	}
}

func TestParseWebProfileInfoJSONFallbackProfilePic(t *testing.T) {
	body := `{"data":{"user":{"username":"u","profile_pic_url":"https://example.com/std.jpg"}}}`
	got, err := ParseWebProfileInfoJSON([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ProfilePicURL != "https://example.com/std.jpg" {
		t.Fatalf("expected fallback to standard profile pic, got %q", got.ProfilePicURL)
	}
}

func TestParseCurrentUserJSON(t *testing.T) {
	body := `{"status":"ok","user":{"pk":"1234567890","username":"gramli_test"}}`
	got, err := ParseCurrentUserJSON([]byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "gramli_test" {
		t.Fatalf("expected gramli_test, got %q", got)
	}
}

func TestParseCurrentUserJSONNoUser(t *testing.T) {
	got, err := ParseCurrentUserJSON([]byte(`{"status":"fail"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty username, got %q", got)
	}
}
