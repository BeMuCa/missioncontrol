// Package transcript reads Claude Code session transcripts.
//
// A session's transcript is a JSONL file that is appended to while the session
// runs, and every subagent it spawns gets its own JSONL beside it. Reading is
// therefore incremental by byte offset: re-parsing the whole file on every poll
// would scale with session length, and sessions run for hours.
package transcript

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry is one line of a transcript. Only the fields the board needs are
// declared; the format carries many more.
type Entry struct {
	Type      string    `json:"type"`
	PromptID  string    `json:"promptId"`
	AgentID   string    `json:"agentId"`
	IsMeta    bool      `json:"isMeta"`
	Timestamp time.Time `json:"timestamp"`
	Origin    *origin   `json:"origin"`
	// Operation and Content carry queue-operation entries: a message typed while
	// the session was busy. Content is top-level on those, not under Message.
	Operation string          `json:"operation"`
	Content   json.RawMessage `json:"content"`
	// Attachment carries the other shape the same thing takes. Which one a
	// session writes depends on how it runs, so both have to be read.
	Attachment *attachment `json:"attachment"`
	Message    struct {
		Content    json.RawMessage `json:"content"`
		StopReason string          `json:"stop_reason"`
	} `json:"message"`
}

type origin struct {
	Kind string `json:"kind"`
}

type attachment struct {
	Type   string  `json:"type"`
	Prompt string  `json:"prompt"`
	Origin *origin `json:"origin"`
}

// Block is one content block. A message's content is either a bare string or a
// list of blocks, so Blocks normalises both into this shape.
type Block struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	ToolUseID string `json:"tool_use_id"`
}

// Blocks returns the entry's content blocks.
func (e Entry) Blocks() []Block {
	raw := e.Message.Content
	if len(raw) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return []Block{{Type: "text", Text: s}}
	}
	var bs []Block
	if err := json.Unmarshal(raw, &bs); err != nil {
		return nil
	}
	return bs
}

// Text is the entry's readable text, blocks joined by blank lines.
func (e Entry) Text() string {
	var parts []string
	for _, b := range e.Blocks() {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// IsHumanPrompt reports whether the entry is something the user typed.
//
// Tool results, injected skill bodies, hook output and agent completion notices
// are all stored as role "user"; origin.kind is the only field that separates a
// typed prompt from those.
func (e Entry) IsHumanPrompt() bool {
	return e.Type == "user" && !e.IsMeta && e.Origin != nil && e.Origin.Kind == "human"
}

// QueuedText is the message of a queue-operation entry — something typed while
// the session was busy, which the transcript records outside the message body.
func (e Entry) QueuedText() string {
	var s string
	if json.Unmarshal(e.Content, &s) != nil {
		return ""
	}
	return s
}

// QueuedCommand is a message typed while the session was busy, in the shape
// used when the transcript records it as an attachment rather than a queue
// operation. Unlike a queue operation there is no matching "picked up" event:
// the entry is only written once the session has taken the message.
func (e Entry) QueuedCommand() (string, bool) {
	a := e.Attachment
	if a == nil || a.Type != "queued_command" || a.Origin == nil || a.Origin.Kind != "human" {
		return "", false
	}
	return a.Prompt, a.Prompt != ""
}

// Reader reads new entries from a growing transcript.
type Reader struct {
	path string
	off  int64
}

func NewReader(path string) *Reader { return &Reader{path: path} }

// Reset makes the next Read start from the top of the file again.
func (r *Reader) Reset() { r.off = 0 }

// Read returns the entries appended since the last call. A trailing partial
// line is left unconsumed: the session may be mid-append.
func (r *Reader) Read() ([]Entry, error) {
	f, err := os.Open(r.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(r.off, io.SeekStart); err != nil {
		return nil, err
	}
	var out []Entry
	br := bufio.NewReader(f)
	for {
		line, err := br.ReadBytes('\n')
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, err
		}
		r.off += int64(len(line))
		var e Entry
		if json.Unmarshal(line, &e) != nil {
			// A line this struct cannot parse is not fatal: the format has
			// entry types the board does not model.
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// ReadAll parses a whole transcript.
func ReadAll(path string) ([]Entry, error) {
	return NewReader(path).Read()
}

// AgentRun is one subagent: its metadata plus the last thing it said.
type AgentRun struct {
	AgentID     string
	Type        string
	Description string
	Model       string
	ToolUseID   string
	PromptID    string
	Result      string
	Started     time.Time
	SpawnDepth  int
	// ToolResults are the tool_use ids this agent got results for. An agent that
	// spawned agents of its own holds their completions here rather than in the
	// session transcript.
	ToolResults []string
}

// LoadAgents reads every subagent transcript in dir, oldest first. A session
// that spawned no subagents has no such directory, which is not an error.
func LoadAgents(dir string) ([]AgentRun, error) {
	metas, err := filepath.Glob(filepath.Join(dir, "agent-*.meta.json"))
	if err != nil {
		return nil, err
	}
	runs := make([]AgentRun, 0, len(metas))
	for _, mp := range metas {
		b, err := os.ReadFile(mp)
		if err != nil {
			return nil, err
		}
		var meta struct {
			AgentType   string `json:"agentType"`
			Description string `json:"description"`
			ToolUseID   string `json:"toolUseId"`
			Model       string `json:"model"`
			SpawnDepth  int    `json:"spawnDepth"`
		}
		if err := json.Unmarshal(b, &meta); err != nil {
			return nil, err
		}
		run := AgentRun{
			AgentID:     strings.TrimSuffix(strings.TrimPrefix(filepath.Base(mp), "agent-"), ".meta.json"),
			Type:        meta.AgentType,
			Description: meta.Description,
			Model:       meta.Model,
			ToolUseID:   meta.ToolUseID,
			SpawnDepth:  meta.SpawnDepth,
		}
		entries, err := ReadAll(strings.TrimSuffix(mp, ".meta.json") + ".jsonl")
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		for _, e := range entries {
			if run.PromptID == "" {
				run.PromptID = e.PromptID
				run.Started = e.Timestamp
			}
			if e.Type == "assistant" {
				if t := e.Text(); t != "" {
					run.Result = t
				}
			}
			for _, b := range e.Blocks() {
				if b.Type == "tool_result" && b.ToolUseID != "" {
					run.ToolResults = append(run.ToolResults, b.ToolUseID)
				}
			}
		}
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Started.Before(runs[j].Started) })
	return runs, nil
}
