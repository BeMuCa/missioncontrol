package favorite

import (
	"path/filepath"
	"testing"
)

func TestToggleAddsAndRemovesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites.jsonl")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	fav := Favorite{Key: "sess#P3.2", Session: "sess", Question: "frage", Answer: "antwort", SavedAt: "2026-08-26T10:00:00Z"}
	if saved, err := s.Toggle(fav); err != nil || !saved {
		t.Fatalf("first toggle: saved=%v err=%v, want saved", saved, err)
	}
	if !s.Has("sess#P3.2") {
		t.Error("Has must report the saved key")
	}

	// A fresh load sees it — it was written, not just held in memory.
	if again, err := Load(path); err != nil || len(again.List()) != 1 {
		t.Fatalf("reload: %d entries err=%v, want 1", len(again.List()), err)
	}

	if saved, err := s.Toggle(fav); err != nil || saved {
		t.Fatalf("second toggle: saved=%v err=%v, want removed", saved, err)
	}
	if s.Has("sess#P3.2") {
		t.Error("the key must be gone after the second toggle")
	}
	if again, _ := Load(path); len(again.List()) != 0 {
		t.Errorf("reload after removal: %d entries, want 0", len(again.List()))
	}
}

func TestMissingFileIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil {
		t.Fatalf("a missing store is not an error: %v", err)
	}
	if len(s.List()) != 0 {
		t.Errorf("got %d, want empty", len(s.List()))
	}
}
