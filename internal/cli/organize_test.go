package cli

import (
	"strings"
	"testing"
)

func TestPostsDeleteCommand(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "DEL001", Owner: "alice", Downloaded: true})
	if e.count("posts") != 1 {
		t.Fatal("expected seeded post")
	}

	dry, _ := e.mustRun("posts", "delete", "DEL001", "--dry-run")
	if !strings.Contains(dry, "Would delete DEL001") {
		t.Errorf("dry-run wrong: %s", dry)
	}
	if e.count("posts") != 1 {
		t.Error("dry-run must not delete")
	}
	if _, _, err := e.run("posts", "delete", "DEL001"); err == nil {
		t.Error("delete without --yes should refuse")
	}
	e.mustRun("posts", "delete", "DEL001", "--yes")
	if e.count("posts") != 0 {
		t.Errorf("post should be deleted, have %d", e.count("posts"))
	}
	if e.count("media") != 0 {
		t.Errorf("media should cascade-delete, have %d", e.count("media"))
	}
	if _, _, err := e.run("posts", "delete", "NOPE"); err == nil {
		t.Error("deleting unknown post should error")
	}
}

func TestPostsTagUntagCommand(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "TAG001", Owner: "bob"})

	e.mustRun("posts", "tag", "TAG001", "design", "inspiration")
	if e.count("post_tags") != 2 {
		t.Errorf("expected 2 tags, got %d", e.count("post_tags"))
	}
	e.mustRun("posts", "untag", "TAG001", "design")
	if e.count("post_tags") != 1 {
		t.Errorf("expected 1 tag after untag, got %d", e.count("post_tags"))
	}
}

func TestCollectionsManageCommands(t *testing.T) {
	e := newEnv(t)
	e.mustRun("init")
	e.seedPost(seedOpts{Shortcode: "COL001", Owner: "carol"})

	e.mustRun("collections", "create", "Ideas")
	e.mustRun("collections", "add-post", "ideas", "COL001")
	show, _ := e.mustRun("collections", "show", "ideas")
	if !strings.Contains(show, "COL001") {
		t.Errorf("collection show missing post:\n%s", show)
	}
	e.mustRun("collections", "remove-post", "ideas", "COL001")
	if e.count("post_collections") != 0 {
		t.Errorf("remove-post should clear membership, got %d", e.count("post_collections"))
	}
	// delete is gated by --yes.
	if _, _, err := e.run("collections", "delete", "ideas"); err == nil {
		t.Error("collections delete without --yes should refuse")
	}
	e.mustRun("collections", "delete", "ideas", "--yes")
	if e.count("collections") != 0 {
		t.Errorf("collection should be deleted, got %d", e.count("collections"))
	}
}
