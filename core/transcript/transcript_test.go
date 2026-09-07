package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Shapes taken from real transcripts: a typed prompt carries content as a bare
// string and origin.kind "human"; everything else that arrives as role "user"
// does not.
const (
	humanLine  = `{"type":"user","uuid":"u1","promptId":"p1","timestamp":"2026-08-25T10:00:00Z","promptSource":"typed","origin":{"kind":"human"},"message":{"role":"user","content":"1. erste Aufgabe\n2. zweite Aufgabe"}}`
	metaLine   = `{"type":"user","uuid":"u2","promptId":"p1","isMeta":true,"message":{"role":"user","content":"Stop hook feedback"}}`
	notifyLine = `{"type":"user","uuid":"u3","promptId":"p1","promptSource":"system","origin":{"kind":"task-notification"},"message":{"role":"user","content":"agent done"}}`
	resultLine = `{"type":"user","uuid":"u4","promptId":"p1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1"}]}}`
	replyLine  = `{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[{"type":"text","text":"hier die Antwort"}]}}`
)

func TestIsHumanPrompt(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want bool
	}{
		{"typed", humanLine, true},
		{"hook output", metaLine, false},
		{"agent notification", notifyLine, false},
		{"tool result", resultLine, false},
		{"assistant", replyLine, false},
	} {
		var e Entry
		if err := json.Unmarshal([]byte(tc.line), &e); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got := e.IsHumanPrompt(); got != tc.want {
			t.Errorf("%s: IsHumanPrompt = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestTextHandlesBothContentShapes(t *testing.T) {
	var human, reply Entry
	if err := json.Unmarshal([]byte(humanLine), &human); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(replyLine), &reply); err != nil {
		t.Fatal(err)
	}
	if want := "1. erste Aufgabe\n2. zweite Aufgabe"; human.Text() != want {
		t.Errorf("string content: got %q, want %q", human.Text(), want)
	}
	if want := "hier die Antwort"; reply.Text() != want {
		t.Errorf("block content: got %q, want %q", reply.Text(), want)
	}
}

func TestReaderIsIncrementalAndSkipsPartialLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(humanLine+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewReader(path)
	first, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("first read: got %d entries, want 1", len(first))
	}

	// A half-written line must not be consumed, or its remainder is lost.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(replyLine + "\n" + `{"type":"assis`); err != nil {
		t.Fatal(err)
	}
	f.Close()

	second, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].Type != "assistant" {
		t.Fatalf("second read: got %+v, want one assistant entry", second)
	}

	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`tant","uuid":"a2","message":{"role":"assistant","content":[{"type":"text","text":"rest"}]}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	third, err := r.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(third) != 1 || third[0].Text() != "rest" {
		t.Fatalf("third read: got %+v, want the completed line", third)
	}
}

func TestLoadAgents(t *testing.T) {
	dir := t.TempDir()
	meta := `{"agentType":"Explore","description":"Trace PPT feature wiring","toolUseId":"toolu_1","spawnDepth":1,"model":"haiku"}`
	if err := os.WriteFile(filepath.Join(dir, "agent-abc.meta.json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	lines := `{"type":"user","agentId":"abc","promptId":"p1","isSidechain":true,"timestamp":"2026-08-25T10:00:01Z","message":{"role":"user","content":"go"}}` + "\n" +
		`{"type":"assistant","agentId":"abc","message":{"role":"assistant","content":[{"type":"text","text":"erste Zwischenmeldung"}]}}` + "\n" +
		`{"type":"assistant","agentId":"abc","message":{"role":"assistant","content":[{"type":"text","text":"das Ergebnis"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "agent-abc.jsonl"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	runs, err := LoadAgents(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d runs, want 1", len(runs))
	}
	r := runs[0]
	if r.AgentID != "abc" || r.Type != "Explore" || r.ToolUseID != "toolu_1" || r.PromptID != "p1" {
		t.Errorf("metadata not carried through: %+v", r)
	}
	if r.Result != "das Ergebnis" {
		t.Errorf("Result = %q, want the last assistant text", r.Result)
	}
}

func TestLoadAgentsOnMissingDir(t *testing.T) {
	runs, err := LoadAgents(filepath.Join(t.TempDir(), "subagents"))
	if err != nil {
		t.Fatalf("a session without subagents is not an error: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("got %d runs, want 0", len(runs))
	}
}
