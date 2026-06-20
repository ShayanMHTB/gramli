package instagram

import "testing"

const collectionsFixture = `{
  "items": [
    {"collection_id": "111", "collection_name": "Recipes", "collection_media_count": 12},
    {"collection_id": "222", "collection_name": "Travel", "collection_media_count": 3},
    {"collection_name": "", "collection_id": ""},
    {"id": "333", "name": "Design"}
  ],
  "status": "ok"
}`

func TestParseCollectionsJSON(t *testing.T) {
	cols, err := ParseCollectionsJSON([]byte(collectionsFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 collections (empty one skipped), got %d: %+v", len(cols), cols)
	}
	if cols[0].ID != "111" || cols[0].Name != "Recipes" || cols[0].MediaCount != 12 {
		t.Errorf("unexpected first collection: %+v", cols[0])
	}
	if cols[2].ID != "333" || cols[2].Name != "Design" {
		t.Errorf("expected fallback id/name fields parsed: %+v", cols[2])
	}
}

func TestParseCollectionsJSONEmpty(t *testing.T) {
	cols, err := ParseCollectionsJSON([]byte(`{"items":[]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 0 {
		t.Errorf("expected no collections, got %d", len(cols))
	}
}
