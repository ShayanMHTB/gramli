package posts

import "testing"

func TestParseInstagramURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		input     string
		shortcode string
		canonical string
		wantErr   bool
	}{
		{"post", "https://www.instagram.com/p/CxAbCdEf123/?utm_source=x", "CxAbCdEf123", "https://www.instagram.com/p/CxAbCdEf123/", false},
		{"reel", "https://instagram.com/reel/CyReelEf456/", "CyReelEf456", "https://www.instagram.com/reel/CyReelEf456/", false},
		{"shortcode", "CzAlbum789_", "CzAlbum789_", "https://www.instagram.com/p/CzAlbum789_/", false},
		{"wrong host", "https://example.com/p/CxAbCdEf123/", "", "", true},
		{"wrong path", "https://instagram.com/stories/name/1", "", "", true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			shortcode, canonical, err := ParseInstagramURL(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if shortcode != tt.shortcode || canonical != tt.canonical {
				t.Fatalf("got (%q, %q), want (%q, %q)", shortcode, canonical, tt.shortcode, tt.canonical)
			}
		})
	}
}
