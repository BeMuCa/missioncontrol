// Package session locates the running Claude Code session to observe.
//
// The CLI already knows which sessions are live and where their transcripts
// are, so the board asks it rather than guessing from process tables.
package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Session is one live session as `claude agents --json` reports it.
type Session struct {
	SessionID string `json:"sessionId"`
	PID       int    `json:"pid"`
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	StartedAt int64  `json:"startedAt"`
}

// List asks the CLI which sessions are live.
func List() ([]Session, error) {
	out, err := exec.Command("claude", "agents", "--json").Output()
	if err != nil {
		return nil, fmt.Errorf("claude agents --json: %w", err)
	}
	return parse(out)
}

func parse(b []byte) ([]Session, error) {
	var ss []Session
	if err := json.Unmarshal(b, &ss); err != nil {
		return nil, err
	}
	return ss, nil
}

// InDir picks the most recently started session running in dir, preferring an
// interactive one. Background sessions are worth observing too — a session
// started with --bg is the same conversation, just without a terminal attached.
func InDir(ss []Session, dir string) (Session, bool) {
	var best Session
	found := false
	for _, s := range ss {
		if s.Cwd != dir {
			continue
		}
		switch {
		case !found,
			s.Kind == "interactive" && best.Kind != "interactive",
			s.Kind == best.Kind && s.StartedAt > best.StartedAt:
			best, found = s, true
		}
	}
	return best, found
}

// TranscriptPath returns the transcript file for session id.
//
// The project directory is the cwd with its separators replaced by dashes, but
// that mapping is not documented; the glob keeps a session findable when a path
// contains something the rule does not cover.
func TranscriptPath(home, cwd, id string) (string, error) {
	slug := strings.ReplaceAll(cwd, string(filepath.Separator), "-")
	p := filepath.Join(home, ".claude", "projects", slug, id+".jsonl")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	matches, err := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", id+".jsonl"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no transcript on disk for session %s", id)
	}
	return matches[0], nil
}

// SubagentsDir is where a session's subagent transcripts live.
func SubagentsDir(transcriptPath string) string {
	return filepath.Join(strings.TrimSuffix(transcriptPath, ".jsonl"), "subagents")
}

// Local is a session transcript on disk, live or long ended.
type Local struct {
	ID    string
	Path  string
	Title string // first human prompt, for the picker
}

// ListLocal enumerates every session transcript for dir, newest first. It reads
// the project directory rather than `claude agents`, so ended sessions — the
// whole point of switching back — are included, not just running ones.
func ListLocal(home, cwd string) ([]Local, error) {
	slug := strings.ReplaceAll(cwd, string(filepath.Separator), "-")
	matches, err := filepath.Glob(filepath.Join(home, ".claude", "projects", slug, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	type entry struct {
		loc Local
		mod int64
	}
	var es []entry
	for _, p := range matches {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		es = append(es, entry{
			loc: Local{
				ID:    strings.TrimSuffix(filepath.Base(p), ".jsonl"),
				Path:  p,
				Title: firstHumanPrompt(p),
			},
			mod: fi.ModTime().UnixNano(),
		})
	}
	sort.Slice(es, func(i, j int) bool { return es[i].mod > es[j].mod })
	out := make([]Local, len(es))
	for i, e := range es {
		out[i] = e.loc
	}
	return out, nil
}

// firstHumanPrompt reads just enough of a transcript to label it. It stops at
// the first typed prompt rather than parsing the whole file.
func firstHumanPrompt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var e struct {
			Type    string                 `json:"type"`
			IsMeta  bool                   `json:"isMeta"`
			Origin  *struct{ Kind string } `json:"origin"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		if e.Type != "user" || e.IsMeta || e.Origin == nil || e.Origin.Kind != "human" {
			continue
		}
		var s string
		if json.Unmarshal(e.Message.Content, &s) == nil {
			return firstLineOf(s)
		}
	}
	return ""
}

func firstLineOf(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

// Summary counts the session transcripts belonging to a project root and
// reports when the newest was last written. It globs and stats rather than
// reading, so the launcher can describe twenty projects without parsing a
// single transcript.
func Summary(home, root string) (n int, newest time.Time) {
	slug := strings.ReplaceAll(root, string(filepath.Separator), "-")
	matches, err := filepath.Glob(filepath.Join(home, ".claude", "projects", slug, "*.jsonl"))
	if err != nil {
		return 0, time.Time{}
	}
	for _, p := range matches {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		n++
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	return n, newest
}
