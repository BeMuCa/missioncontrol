package done

import (
	"path/filepath"
	"testing"
)

func TestToggleAddsRemovesAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "done.jsonl")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Has("sess#P1.2") {
		t.Fatal("empty store must have nothing")
	}
	if on, err := s.Toggle("sess#P1.2", "2026-08-28T10:00:00Z"); err != nil || !on {
		t.Fatalf("first toggle: on=%v err=%v", on, err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Has("sess#P1.2") {
		t.Error("the mark must survive a reload")
	}
	if on, _ := again.Toggle("sess#P1.2", ""); on {
		t.Error("second toggle must clear it")
	}
	if third, _ := Load(path); third.Has("sess#P1.2") {
		t.Error("the cleared mark must be gone from disk")
	}
}

func TestMissingFileIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil || s == nil || s.Has("x") {
		t.Fatalf("missing file must load as empty: %v", err)
	}
}
