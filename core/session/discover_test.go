package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Copied from a real `claude agents --json`: background entries carry no cwd
// match and interactive ones are keyed by sessionId, not id.
const agentsJSON = `[
  {"id":"ef5d403c","cwd":"/home/berk/git/Other","kind":"background","startedAt":1782849960747,"sessionId":"ef5d403c-f65a-4e40-ac93-32ec221bb026","state":"blocked"},
  {"pid":1954,"cwd":"/home/berk/git/CleanClaude","kind":"interactive","startedAt":1787665459895,"sessionId":"old-session","name":"cleanclaude-1","status":"idle"},
  {"pid":8245,"cwd":"/home/berk/git/Other","kind":"interactive","startedAt":1787665604221,"sessionId":"other-session","name":"other-36","status":"busy"},
  {"pid":142431,"cwd":"/home/berk/git/CleanClaude","kind":"interactive","startedAt":1787668301001,"sessionId":"new-session","name":"cleanclaude-92","status":"busy"}
]`

func TestInDirPicksNewestPreferringInteractive(t *testing.T) {
	ss, err := parse([]byte(agentsJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(ss) != 4 {
		t.Fatalf("got %d sessions, want 4", len(ss))
	}
	s, ok := InDir(ss, "/home/berk/git/CleanClaude")
	if !ok {
		t.Fatal("no session found")
	}
	if s.SessionID != "new-session" {
		t.Errorf("SessionID = %q, want the most recently started one", s.SessionID)
	}
	if _, ok := InDir(ss, "/home/berk/git/Nowhere"); ok {
		t.Error("a directory with no session must report none")
	}
}

func TestTranscriptPathUsesSlugThenGlob(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/berk/git/CleanClaude"
	slugDir := filepath.Join(home, ".claude", "projects", "-home-berk-git-CleanClaude")
	if err := os.MkdirAll(slugDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(slugDir, "sess.jsonl")
	if err := os.WriteFile(want, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := TranscriptPath(home, cwd, "sess")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// A session whose project directory does not follow the slug rule is still
	// findable by its id.
	oddDir := filepath.Join(home, ".claude", "projects", "something-else")
	if err := os.MkdirAll(oddDir, 0o755); err != nil {
		t.Fatal(err)
	}
	odd := filepath.Join(oddDir, "elsewhere.jsonl")
	if err := os.WriteFile(odd, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = TranscriptPath(home, cwd, "elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	if got != odd {
		t.Errorf("glob fallback: got %q, want %q", got, odd)
	}

	if _, err := TranscriptPath(home, cwd, "missing"); err == nil {
		t.Error("a session with no transcript must be an error")
	}
}

func TestSubagentsDir(t *testing.T) {
	got := SubagentsDir("/h/.claude/projects/-p/sess.jsonl")
	want := "/h/.claude/projects/-p/sess/subagents"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBackgroundSessionIsUsedWhenNothingElseRuns(t *testing.T) {
	// A --bg session is the same conversation without a terminal attached, so the
	// board must still find it.
	only := `[{"cwd":"/home/berk/git/CleanClaude","kind":"background","startedAt":1,"sessionId":"bg-one"}]`
	ss, err := parse([]byte(only))
	if err != nil {
		t.Fatal(err)
	}
	s, ok := InDir(ss, "/home/berk/git/CleanClaude")
	if !ok || s.SessionID != "bg-one" {
		t.Errorf("got %+v ok=%v, want the background session", s, ok)
	}
}

func TestInteractiveWinsOverBackground(t *testing.T) {
	both := `[
	  {"cwd":"/d","kind":"background","startedAt":99,"sessionId":"bg"},
	  {"cwd":"/d","kind":"interactive","startedAt":1,"sessionId":"fg"}
	]`
	ss, err := parse([]byte(both))
	if err != nil {
		t.Fatal(err)
	}
	if s, _ := InDir(ss, "/d"); s.SessionID != "fg" {
		t.Errorf("picked %q, want the interactive one even though it is older", s.SessionID)
	}
}

func TestListLocalIncludesEndedSessionsNewestFirst(t *testing.T) {
	home := t.TempDir()
	cwd := "/home/berk/git/Demo"
	dir := filepath.Join(home, ".claude", "projects", "-home-berk-git-Demo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(dir, "old.jsonl")
	newer := filepath.Join(dir, "new.jsonl")
	human := `{"type":"user","origin":{"kind":"human"},"message":{"role":"user","content":"erste frage hier"}}` + "\n"
	if err := os.WriteFile(older, []byte(`{"type":"summary"}`+"\n"+human), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte(human), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make newer actually newer on disk.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(older, old, old); err != nil {
		t.Fatal(err)
	}

	list, err := ListLocal(home, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d sessions, want 2", len(list))
	}
	if list[0].ID != "new" || list[1].ID != "old" {
		t.Errorf("order = %s,%s — want newest first", list[0].ID, list[1].ID)
	}
	if list[0].Title != "erste frage hier" {
		t.Errorf("title = %q, want the first human prompt", list[0].Title)
	}
}
