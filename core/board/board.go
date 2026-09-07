// Package board turns a transcript into the tree the board shows: the prompts
// the user typed, the numbered items inside each one, and the agents each
// prompt spawned.
//
// An agent belongs to the prompt whose promptId it carries, and it has finished
// once a tool_result for its toolUseId exists — both exact. Two things are not:
// which numbered item an agent serves is nowhere in the data, and an agent whose
// promptId belongs to no typed prompt is placed by time.
package board

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/BeMuCa/missioncontrol/core/transcript"
)

// itemRE matches a list item the user typed. Two positions count: the number at
// the start of a line ("1. do this", "2) then", "3- and"), and — because people
// run parts together in one line — a number that opens a new sentence mid-line
// ("… fertig? 2. dann das"). The separator is loose so the way you number does
// not decide whether the board sees your parts, and the answer's own shape —
// "#1", "[#1]" — counts as well, since people copy what they are shown.
//
// The item text runs to the next such marker or the end of the line, whichever
// comes first, so a mid-line "2." does not swallow the rest of the paragraph.
//
// A number opening a parenthesis — "(5. gibt es ein limit …" — is a part
// squeezed into another part's sentence and counts as well, but only in the
// dotted shape: "(3)" is usually a reference to a part, not a new one.
var itemRE = regexp.MustCompile(`(?m:^[ \t]*|[.?!]\s+)(?:\[?#(\d{1,2})\]?|(\d{1,2})[.):-])\s+|\((\d{1,2})\.\s+`)

// runRE matches a marker run opening a line: the label an answer carries when
// Claude was told to say which numbered part it is answering — "A#4", answer to
// part 4; "[#4]" is the older shape and still counts. Markers arrive wrapped in
// emphasis (`**A#4**`) and sometimes several at once (`A#3, A#4`) when one
// section answers more than one part. Only a run that opens a line counts — the
// same text mid-sentence is prose, not a label.
//
// A sub-numbered marker — "A#1.1", answer to sub-part 1.1, or the lettered
// shape "A#1b" — counts too. There is no sub-item on the board to hand it to,
// so its section lands on item 1. A marker may also open a Markdown heading —
// "## A#2 · Titel" — in which case the heading is the answer's own structure
// and stays with the section; a heading that mentions a marker mid-line is
// still prose.
//
// The numbers are the user's own, not this package's: nothing on the other side
// knows about prompt ids.
var runRE = regexp.MustCompile(`(?m)^[ \t]*(#{1,6}[ \t]+)?((?:(?:\*\*|__|\*)?(?:A#\d{1,2}(?:\.\d{1,2})*[a-z]?|\[#\d{1,2}(?:\.\d{1,2})*[a-z]?\])(?:\*\*|__|\*)?[,;/&+ \t]*)+)`)

var numRE = regexp.MustCompile(`(?:A#|\[#)(\d{1,2})`)

// subRE spots a sub-marker in a run: a digit followed by a dot or letter —
// "A#1.1", "A#1b". Separators, emphasis, and the plain shapes never put either
// after a digit.
var subRE = regexp.MustCompile(`\d[.a-z]`)

// Board is every prompt of one session, oldest first.
type Board struct {
	Prompts []*Prompt
}

// Prompt is one thing the user typed.
type Prompt struct {
	ID       string
	PromptID string
	Text     string
	Time     time.Time
	Items    []*Item
	Agents   []*Agent
	Reply    string
	// Queued marks a message typed while the session was busy. Pending means it
	// is still sitting in the queue, unread.
	Queued  bool
	Pending bool
	// Borrowed says the reply shown here belongs to the turn this message landed
	// in rather than to a turn of its own.
	Borrowed bool
	// Done means the turn is over: its last message stopped with end_turn, or a
	// later prompt has since been typed.
	Done bool
}

// Item is a numbered line inside a prompt.
type Item struct {
	ID string
	// Num is the number the user wrote in front of this part. It is what a
	// [#n] marker in the reply refers to, not the part's position.
	Num  int
	Text string
	// Answer is the part of the reply that carried this item's marker — or
	// several parts, when sub-numbered markers (A#1.1, A#1.2) share this item.
	// Empty unless Claude labelled its answer. Final says Claude has moved on
	// from it — a later marker follows, or the turn is done — so it will not
	// grow.
	Answer string
	Final  bool
	// Orphan marks an item made from the reply alone: its marker names a
	// number no typed part carries, so there is no question text.
	Orphan bool
	Agents []*Agent
}

// Agent is one subagent run.
type Agent struct {
	ID          string
	AgentID     string
	Type        string
	Description string
	Model       string
	Result      string
	Started     time.Time
	// Depth is 1 for an agent this session spawned, 2 for one an agent spawned.
	Depth int
	Done  bool
}

// Build assembles the board. runs must be ordered by start time.
func Build(entries []transcript.Entry, runs []transcript.AgentRun) *Board {
	// An agent has finished once someone has its result. For a nested agent that
	// someone is the agent that spawned it, so agent transcripts count too.
	done := map[string]bool{}
	for _, e := range entries {
		for _, b := range e.Blocks() {
			if b.Type == "tool_result" && b.ToolUseID != "" {
				done[b.ToolUseID] = true
			}
		}
	}
	for _, r := range runs {
		for _, id := range r.ToolResults {
			done[id] = true
		}
	}

	// A queued message is recorded twice: once when typed and once when the
	// session picks it up. What is enqueued and never removed is still waiting.
	removed := map[string]int{}
	for _, e := range entries {
		if e.Type == "queue-operation" && e.Operation == "remove" {
			removed[e.QueuedText()]++
		}
	}

	b := &Board{}
	byID := map[string]*Prompt{}
	// landed pairs each queued message with the turn that was running when it
	// arrived, so it can show that turn's reply instead of nothing.
	landed := map[*Prompt]*Prompt{}
	var cur *Prompt
	for _, e := range entries {
		switch {
		case e.Type == "queue-operation" && e.Operation == "enqueue" && isTaskNotification(e.QueuedText()):
			// An agent reporting back is queued the same way a typed message is.
			// It is not something the user asked for, so it is not a prompt.
		case e.Type == "queue-operation" && e.Operation == "enqueue":
			// A queued message deliberately does not become the current prompt:
			// the reply to it is interleaved into the turn that was already
			// running, and splitting that prose apart would be a guess.
			q := &Prompt{
				ID:     fmt.Sprintf("P%d", len(b.Prompts)+1),
				Text:   e.QueuedText(),
				Time:   e.Timestamp,
				Queued: true,
			}
			if removed[q.Text] > 0 {
				removed[q.Text]--
			} else {
				q.Pending = true
			}
			q.Items = splitItems(q.ID, q.Text)
			landed[q] = cur
			b.Prompts = append(b.Prompts, q)
		case queuedCommand(e) != "":
			// The same thing in the other shape the transcript uses. This one is
			// only written once the session has taken the message, so it is never
			// pending.
			q := &Prompt{
				ID:     fmt.Sprintf("P%d", len(b.Prompts)+1),
				Text:   queuedCommand(e),
				Time:   e.Timestamp,
				Queued: true,
			}
			q.Items = splitItems(q.ID, q.Text)
			landed[q] = cur
			b.Prompts = append(b.Prompts, q)
		case e.IsHumanPrompt():
			if cur != nil {
				cur.Done = true
			}
			cur = &Prompt{
				ID:       fmt.Sprintf("P%d", len(b.Prompts)+1),
				PromptID: e.PromptID,
				Text:     e.Text(),
				Time:     e.Timestamp,
			}
			cur.Items = splitItems(cur.ID, cur.Text)
			b.Prompts = append(b.Prompts, cur)
			byID[e.PromptID] = cur
		case e.Type == "assistant" && cur != nil:
			// Assistant turns carry no promptId, so a reply belongs to the last
			// prompt seen. That is position, not attribution — splitReply can only
			// do better where the answer carries a marker.
			cur.Done = e.Message.StopReason == "end_turn"
			if t := e.Text(); t != "" {
				if cur.Reply != "" {
					cur.Reply += "\n\n"
				}
				cur.Reply += t
			}
		}
	}

	for _, p := range b.Prompts {
		splitReply(p)
	}

	// Filled after splitting, and never split itself: the numbers in a borrowed
	// reply belong to the prompt it was written for, not to this one.
	for q, host := range landed {
		if q.Reply == "" && !q.Pending && host != nil && host.Reply != "" {
			q.Reply, q.Borrowed = host.Reply, true
		}
	}

	for i, r := range runs {
		p := byID[r.PromptID]
		if p == nil {
			// An agent spawned in a turn that a task notification woke — one
			// agent finishing and the session picking up its report — carries a
			// promptId no typed prompt owns. It still belongs on the board, so
			// it lands under the prompt that was running when it started.
			p = b.enclosing(r.Started)
		}
		if p == nil {
			continue
		}
		a := &Agent{
			ID:          fmt.Sprintf("A%d", i+1),
			AgentID:     r.AgentID,
			Type:        r.Type,
			Description: r.Description,
			Model:       r.Model,
			Result:      r.Result,
			Started:     r.Started,
			Depth:       r.SpawnDepth,
			Done:        done[r.ToolUseID],
		}
		p.Agents = append(p.Agents, a)
		if it := guessItem(p.Items, a.Description); it != nil {
			it.Agents = append(it.Agents, a)
		}
	}
	return b
}

// splitReply hands each marked section of a reply to the item it names. The
// reply itself is left whole: the markers are a hint, not a guarantee that
// every part got one.
func splitReply(p *Prompt) {
	if p.Reply == "" || len(p.Items) == 0 {
		return
	}
	byNum := map[int]*Item{}
	for _, it := range p.Items {
		if byNum[it.Num] == nil {
			byNum[it.Num] = it
		}
	}
	runs := runRE.FindAllStringSubmatchIndex(p.Reply, -1)
	for i, r := range runs {
		end := len(p.Reply)
		final := p.Done
		if i+1 < len(runs) {
			end, final = runs[i+1][0], true
		}
		run := p.Reply[r[4]:r[5]]
		// A sub-numbered section keeps its marker — once it shares the item's
		// block, the label is what tells 1.1 from 1.2 — and appends, so A#1.2
		// does not wipe out A#1.1. A heading keeps its whole line too: stripping
		// "## A#2" would leave the heading's title as a stray line.
		sub := subRE.MatchString(run)
		heading := r[2] >= 0
		var section string
		if sub || heading {
			section = strings.TrimSpace(p.Reply[r[0]:end])
		} else {
			section = strings.TrimSpace(p.Reply[r[1]:end])
			// A marker written inside its emphasis — `**[#3] the answer**` — leaves the
			// closer behind when the opener is consumed with the label. Put it back, or
			// the section renders with a stray `**` in the middle of a sentence.
			if op := openEmphasis(run); op != "" {
				section = op + section
			}
		}
		if section == "" {
			continue
		}
		// A grouped run of siblings — "A#1.1, A#1.2" — names the same item twice.
		seen := map[int]bool{}
		for _, num := range numRE.FindAllStringSubmatch(run, -1) {
			n, _ := strconv.Atoi(num[1])
			if seen[n] {
				continue
			}
			seen[n] = true
			it := byNum[n]
			if it == nil {
				// An answer whose number no typed part carries still belongs
				// on the board: it becomes an item of its own, without question
				// text, in the order the reply produced it.
				it = &Item{ID: fmt.Sprintf("%s.%d", p.ID, n), Num: n, Orphan: true}
				byNum[n] = it
				p.Items = append(p.Items, it)
			}
			if sub && it.Answer != "" {
				it.Answer += "\n\n" + section
			} else {
				it.Answer = section
			}
			it.Final = final
		}
	}
}

// openEmphasis reports the emphasis a marker run opens but never closes.
func openEmphasis(run string) string {
	if t := strings.TrimRight(run, ",;/&+ \t"); strings.HasSuffix(t, "*") || strings.HasSuffix(t, "_") {
		return ""
	}
	for _, o := range []string{"**", "__", "*"} {
		if strings.HasPrefix(run, o) {
			return o
		}
	}
	return ""
}

// enclosing is the last prompt that had started by t.
func (b *Board) enclosing(t time.Time) *Prompt {
	var p *Prompt
	for _, cand := range b.Prompts {
		if cand.Time.After(t) {
			break
		}
		p = cand
	}
	if p == nil && len(b.Prompts) > 0 {
		return b.Prompts[0]
	}
	return p
}

// queuedCommand is the attachment form of a message typed mid-turn.
func queuedCommand(e transcript.Entry) string {
	if text, ok := e.QueuedCommand(); ok && !isTaskNotification(text) {
		return text
	}
	return ""
}

func isTaskNotification(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "<task-notification>")
}

// Pending counts queued messages the session has not picked up yet.
func (b *Board) Pending() int {
	n := 0
	for _, p := range b.Prompts {
		if p.Pending {
			n++
		}
	}
	return n
}

// Waiting counts agents that have not reported back yet.
func (b *Board) Waiting() int {
	n := 0
	for _, p := range b.Prompts {
		for _, a := range p.Agents {
			if !a.Done {
				n++
			}
		}
	}
	return n
}

func splitItems(promptID, text string) []*Item {
	locs := itemRE.FindAllStringSubmatchIndex(text, -1)
	var items []*Item
	// People start typing and only begin numbering at the second thought: "is
	// this right? 2. and what about…". That lead is part number one.
	if len(locs) > 0 {
		if first := itemNum(text, locs[0]); first >= 2 {
			// A mid-line marker matches its leading ".?!", which is the lead's own
			// sentence end.
			end := locs[0][0]
			if end < len(text) && strings.ContainsRune(".?!", rune(text[end])) {
				end++
			}
			if lead := strings.TrimSpace(text[:end]); lead != "" {
				items = append(items, &Item{ID: fmt.Sprintf("%s.%d", promptID, first-1), Num: first - 1, Text: lead})
			}
		}
	}
	for i, m := range locs {
		// The text of a part runs from the end of its marker to the start of the
		// next marker, or to the end of the whole prompt for the last one.
		end := len(text)
		if i+1 < len(locs) {
			// A mid-line marker matches its leading ".?!" — that punctuation ends
			// the previous part's sentence, so it stays with the previous part.
			nb := locs[i+1][0]
			if nb < len(text) && strings.ContainsRune(".?!", rune(text[nb])) {
				nb++
			}
			end = nb
		}
		body := strings.TrimSpace(text[m[1]:end])
		if body == "" {
			continue
		}
		num := itemNum(text, m)
		items = append(items, &Item{
			ID:   fmt.Sprintf("%s.%d", promptID, num),
			Num:  num,
			Text: body,
		})
	}
	return items
}

// itemNum is the number in an itemRE match, from whichever of its shapes
// matched.
func itemNum(text string, m []int) int {
	for _, g := range [][2]int{{m[2], m[3]}, {m[4], m[5]}, {m[6], m[7]}} {
		if g[0] >= 0 {
			n, _ := strconv.Atoi(text[g[0]:g[1]])
			return n
		}
	}
	return 0
}

// guessItem picks the item whose wording an agent's description overlaps most.
// Two shared words is the floor; below that the match says nothing.
func guessItem(items []*Item, desc string) *Item {
	dw := words(desc)
	var best *Item
	score := 0
	for _, it := range items {
		n := 0
		for w := range words(it.Text) {
			if dw[w] {
				n++
			}
		}
		if n > score {
			best, score = it, n
		}
	}
	if score < 2 {
		return nil
	}
	return best
}

// words reduces a phrase to four-rune stems. Comparing whole words fails on
// inflection — an agent described as "tests prüfen" shares only one word with
// an item that says "tests prüft" — and a real stemmer is more machinery than a
// guess this weak deserves.
func words(s string) map[string]bool {
	out := map[string]bool{}
	split := func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }
	for _, w := range strings.FieldsFunc(strings.ToLower(s), split) {
		if r := []rune(w); len(r) >= 4 {
			out[string(r[:4])] = true
		}
	}
	return out
}
