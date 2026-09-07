package board

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/BeMuCa/missioncontrol/core/transcript"
)

func entries(t *testing.T, lines ...string) []transcript.Entry {
	t.Helper()
	var out []transcript.Entry
	for _, l := range lines {
		var e transcript.Entry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		out = append(out, e)
	}
	return out
}

const (
	prompt1 = `{"type":"user","promptId":"p1","timestamp":"2026-08-25T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. baue den parser für transcripts\n2. starte agent der die tests prüft"}}`
	reply1  = `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"mache ich"}]}}`
	// The tool_result for toolu_a is what marks agent A1 as finished.
	done1   = `{"type":"user","promptId":"p1","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_a"}]}}`
	prompt2 = `{"type":"user","promptId":"p2","timestamp":"2026-08-25T10:05:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"und jetzt weiter"}}`
	noise   = `{"type":"user","promptId":"p1","isMeta":true,"message":{"role":"user","content":"skill body"}}`
)

func runs() []transcript.AgentRun {
	base := time.Date(2026, 8, 25, 10, 0, 1, 0, time.UTC)
	return []transcript.AgentRun{
		{AgentID: "aaa", Type: "Explore", Description: "parser für transcripts lesen", ToolUseID: "toolu_a", PromptID: "p1", Result: "gefunden", Started: base},
		{AgentID: "bbb", Type: "general-purpose", Description: "tests prüfen und starten", ToolUseID: "toolu_b", PromptID: "p1", Started: base.Add(time.Second)},
		{AgentID: "ccc", Type: "Explore", Description: "etwas völlig anderes", ToolUseID: "toolu_c", PromptID: "p2", Started: base.Add(2 * time.Second)},
	}
}

func TestBuildGroupsPromptsItemsAndAgents(t *testing.T) {
	b := Build(entries(t, prompt1, noise, reply1, done1, prompt2), runs())

	if len(b.Prompts) != 2 {
		t.Fatalf("got %d prompts, want 2 (injected meta entries must not count)", len(b.Prompts))
	}
	p1 := b.Prompts[0]
	if p1.ID != "P1" || len(p1.Items) != 2 {
		t.Fatalf("P1: id=%s items=%d, want P1 with 2 items", p1.ID, len(p1.Items))
	}
	if p1.Items[0].ID != "P1.1" || p1.Items[1].ID != "P1.2" {
		t.Errorf("item ids = %s, %s", p1.Items[0].ID, p1.Items[1].ID)
	}
	if p1.Reply != "mache ich" {
		t.Errorf("Reply = %q", p1.Reply)
	}
	if len(p1.Agents) != 2 {
		t.Fatalf("P1 agents = %d, want the 2 with promptId p1", len(p1.Agents))
	}
	if len(b.Prompts[1].Agents) != 1 {
		t.Errorf("P2 agents = %d, want 1", len(b.Prompts[1].Agents))
	}
}

func TestDoneComesFromToolResult(t *testing.T) {
	doneC := `{"type":"user","promptId":"p2","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_c"}]}}`
	b := Build(entries(t, prompt1, done1, prompt2, doneC), runs())
	p1 := b.Prompts[0]
	if !p1.Agents[0].Done {
		t.Error("A1 has a tool_result and must count as done")
	}
	if p1.Agents[1].Done {
		t.Error("A2 has no tool_result and must count as still running")
	}
	if got := b.Waiting(); got != 1 {
		t.Errorf("Waiting = %d, want 1", got)
	}
}

func TestAgentsAreAssignedToItemsByOverlap(t *testing.T) {
	b := Build(entries(t, prompt1, prompt2), runs())
	p1 := b.Prompts[0]
	if len(p1.Items[0].Agents) != 1 || p1.Items[0].Agents[0].ID != "A1" {
		t.Errorf("item 1 agents = %+v, want A1", p1.Items[0].Agents)
	}
	if len(p1.Items[1].Agents) != 1 || p1.Items[1].Agents[0].ID != "A2" {
		t.Errorf("item 2 agents = %+v, want A2", p1.Items[1].Agents)
	}
}

func TestUnrelatedAgentStaysUnassigned(t *testing.T) {
	b := Build(entries(t, prompt1, prompt2), runs())
	p2 := b.Prompts[1]
	if len(p2.Items) != 0 {
		t.Fatalf("P2 has no numbered lines, got %d items", len(p2.Items))
	}
	if len(p2.Agents) != 1 {
		t.Errorf("the agent must still show under its prompt: %+v", p2.Agents)
	}
}

func TestAgentDroppedWhenTranscriptHasNoPrompts(t *testing.T) {
	orphan := []transcript.AgentRun{{AgentID: "zzz", PromptID: "gone", ToolUseID: "toolu_z"}}
	b := Build(entries(t, reply1), orphan)
	if len(b.Prompts) != 0 {
		t.Fatalf("got %d prompts, want none", len(b.Prompts))
	}
}

const (
	enqueue = `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-08-25T10:01:00Z","content":"und noch was dazu"}`
	dequeue = `{"type":"queue-operation","operation":"remove","timestamp":"2026-08-25T10:02:00Z","content":"und noch was dazu"}`
	waiting = `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-08-25T10:03:00Z","content":"das wartet noch"}`
)

func TestQueuedMessagesBecomePrompts(t *testing.T) {
	b := Build(entries(t, prompt1, enqueue, reply1, dequeue, waiting), nil)

	if len(b.Prompts) != 3 {
		t.Fatalf("got %d prompts, want the typed one plus two queued", len(b.Prompts))
	}
	picked, still := b.Prompts[1], b.Prompts[2]
	if !picked.Queued || picked.Pending {
		t.Errorf("a queued message with a matching remove is picked up: %+v", picked)
	}
	if !still.Queued || !still.Pending {
		t.Errorf("a queued message with no remove is still waiting: %+v", still)
	}
	if got := b.Pending(); got != 1 {
		t.Errorf("Pending = %d, want 1", got)
	}
	// The reply belongs to the prompt that was running, not to what was queued
	// while it ran.
	if b.Prompts[0].Reply != "mache ich" {
		t.Errorf("P1 reply = %q", b.Prompts[0].Reply)
	}
	// A queued message has no turn of its own, so it shows the reply of the turn
	// it landed in — marked as borrowed rather than left blank.
	if picked.Reply != "mache ich" || !picked.Borrowed {
		t.Errorf("picked-up message: reply=%q borrowed=%v", picked.Reply, picked.Borrowed)
	}
	if still.Reply != "" || still.Borrowed {
		t.Errorf("a message still in the queue has nothing to borrow: reply=%q borrowed=%v", still.Reply, still.Borrowed)
	}
}

func TestTaskNotificationsAreNotPrompts(t *testing.T) {
	notify := `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-08-25T10:01:30Z","content":"<task-notification> <task-id>abc</task-id> agent done"}`
	b := Build(entries(t, prompt1, notify), nil)
	if len(b.Prompts) != 1 {
		t.Errorf("an agent reporting back is not a prompt, got %d prompts", len(b.Prompts))
	}
}

func TestAgentFallsBackToEnclosingPrompt(t *testing.T) {
	// A turn woken by a task notification spawns agents whose promptId no typed
	// prompt owns; they belong to the prompt that was running.
	orphan := []transcript.AgentRun{{
		AgentID: "zzz", Description: "irgendwas", PromptID: "woken-by-notification",
		ToolUseID: "toolu_z", Started: time.Date(2026, 8, 25, 10, 6, 0, 0, time.UTC),
	}}
	b := Build(entries(t, prompt1, prompt2), orphan)
	if len(b.Prompts[1].Agents) != 1 {
		t.Fatalf("P2 agents = %d, want the orphan attached to the prompt it ran under", len(b.Prompts[1].Agents))
	}
	if len(b.Prompts[0].Agents) != 0 {
		t.Errorf("P1 must not claim it: %+v", b.Prompts[0].Agents)
	}
}

func TestMarkedAnswersLandOnTheirItem(t *testing.T) {
	marked := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Vorwort ohne Marker.\n\n[#2] Die Tests laufen jetzt.\nZweite Zeile dazu.\n\n[#1] Der Parser ist gebaut.\n\n[#9] Punkt gibt es nicht."}]}}`
	b := Build(entries(t, prompt1, marked), nil)
	p := b.Prompts[0]

	if got := p.Items[0].Answer; got != "Der Parser ist gebaut." {
		t.Errorf("item 1 answer = %q", got)
	}
	if got := p.Items[1].Answer; got != "Die Tests laufen jetzt.\nZweite Zeile dazu." {
		t.Errorf("item 2 answer = %q", got)
	}
	if !strings.Contains(p.Reply, "Vorwort ohne Marker.") {
		t.Error("the reply must stay whole on the prompt")
	}
}

func TestUnmarkedReplyLeavesItemsEmpty(t *testing.T) {
	b := Build(entries(t, prompt1, reply1), nil)
	for _, it := range b.Prompts[0].Items {
		if it.Answer != "" {
			t.Errorf("%s claimed an answer from an unmarked reply: %q", it.ID, it.Answer)
		}
	}
}

// The shapes below are copied out of real transcripts: markers arrive wrapped in
// emphasis, sometimes several to a section, and the same text mid-sentence is
// prose that must not be mistaken for a label.
func TestMarkersAsTheyActuallyArrive(t *testing.T) {
	five := `{"type":"user","promptId":"p1","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. eins\n2. zwei\n3. drei\n4. vier\n5. fuenf"}}`
	reply := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":` +
		`"**[#0]** Vorwort ohne Punkt.\n\n**[#2]** Zwei ist fertig.\n\n**[#3]**, **[#4]** teilen sich diesen Abschnitt.\n\nDazu noch, es kommen **[#1] Doppelcheck** mitten im Satz.\n\n**[#5]** Fuenf zuletzt."}]}}`

	b := Build(entries(t, five, reply), nil)
	it := b.Prompts[0].Items

	if got := it[1].Answer; got != "Zwei ist fertig." {
		t.Errorf("emphasised marker: item 2 = %q", got)
	}
	shared := "teilen sich diesen Abschnitt.\n\nDazu noch, es kommen **[#1] Doppelcheck** mitten im Satz."
	if it[2].Answer != shared || it[3].Answer != shared {
		t.Errorf("grouped markers must share the section:\n 3 = %q\n 4 = %q", it[2].Answer, it[3].Answer)
	}
	if it[0].Answer != "" {
		t.Errorf("a marker mid-sentence is prose, not a label — item 1 = %q", it[0].Answer)
	}
	if it[4].Answer != "Fuenf zuletzt." {
		t.Errorf("item 5 = %q", it[4].Answer)
	}
}

func TestMarkerInsideEmphasisKeepsItBalanced(t *testing.T) {
	// `**[#1] Kein fester Wert.**` puts the label inside the bold span; dropping
	// the opener with the label would leave the closer stranded mid-sentence.
	reply := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"**[#1] Kein fester Wert.** Der Rest folgt.\n\n**[#2]** Sauber geschlossen."}]}}`
	b := Build(entries(t, prompt1, reply), nil)
	it := b.Prompts[0].Items

	if got, want := it[0].Answer, "**Kein fester Wert.** Der Rest folgt."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got, want := it[1].Answer, "Sauber geschlossen."; got != want {
		t.Errorf("a closed marker needs no repair: got %q, want %q", got, want)
	}
}

func TestBorrowedReplyIsNotSplitAcrossItems(t *testing.T) {
	// The queued message has two numbered parts of its own, and the reply it
	// borrows carries markers meant for the host prompt. Those must not be
	// mistaken for answers to this message's parts.
	q := `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-08-26T10:01:00Z","content":"1. erstes ding\n2. zweites ding"}`
	dq := `{"type":"queue-operation","operation":"remove","timestamp":"2026-08-26T10:02:00Z","content":"1. erstes ding\n2. zweites ding"}`
	hostReply := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"**[#1]** gehoert zum Host.\n\n**[#2]** auch."}]}}`

	b := Build(entries(t, prompt1, q, hostReply, dq), nil)
	queued := b.Prompts[1]

	if !queued.Borrowed || queued.Reply == "" {
		t.Fatalf("expected a borrowed reply, got borrowed=%v reply=%q", queued.Borrowed, queued.Reply)
	}
	for _, it := range queued.Items {
		if it.Answer != "" {
			t.Errorf("%s claimed part of a borrowed reply: %q", it.ID, it.Answer)
		}
	}
	// The host keeps its own split.
	if b.Prompts[0].Items[0].Answer != "gehoert zum Host." {
		t.Errorf("host item 1 = %q", b.Prompts[0].Items[0].Answer)
	}
}

func TestAttachmentFormOfQueuedMessage(t *testing.T) {
	// Some sessions record a mid-turn message as an attachment instead of a queue
	// operation. Shape copied from a real transcript.
	att := `{"type":"attachment","timestamp":"2026-08-26T10:01:00Z","attachment":{"type":"queued_command","prompt":"und dreh das board bitte","commandMode":"prompt","origin":{"kind":"human"}}}`
	notify := `{"type":"attachment","timestamp":"2026-08-26T10:01:10Z","attachment":{"type":"queued_command","prompt":"<task-notification> agent fertig","origin":{"kind":"human"}}}`
	other := `{"type":"attachment","timestamp":"2026-08-26T10:01:20Z","attachment":{"type":"file","prompt":"nicht getippt"}}`

	b := Build(entries(t, prompt1, att, notify, other, reply1), nil)
	if len(b.Prompts) != 2 {
		t.Fatalf("got %d prompts, want the typed one plus the queued command", len(b.Prompts))
	}
	q := b.Prompts[1]
	if q.Text != "und dreh das board bitte" || !q.Queued || q.Pending {
		t.Errorf("queued command: %+v", q)
	}
	if q.Reply != "mache ich" || !q.Borrowed {
		t.Errorf("it must borrow the reply of the turn it landed in: reply=%q borrowed=%v", q.Reply, q.Borrowed)
	}
}

func TestDashNumberingIsRecognised(t *testing.T) {
	p := `{"type":"user","promptId":"p1","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1- erstes\n2- zweites\n3) drittes\n4. viertes"}}`
	b := Build(entries(t, p), nil)
	if got := len(b.Prompts[0].Items); got != 4 {
		t.Fatalf("got %d items, want 4 across . ) and - separators", got)
	}
	if b.Prompts[0].Items[0].Text != "erstes" {
		t.Errorf("item 1 text = %q", b.Prompts[0].Items[0].Text)
	}
}

func TestMidLineNumberIsAPart(t *testing.T) {
	// Your real P15: "3." opens a line, "4." sits mid-line after a question mark.
	// Both must count, and neither must swallow the other's text.
	p := `{"type":"user","promptId":"p1","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. erstes\n3. wie trennen wir das? 4. und was ist X?"}}`
	b := Build(entries(t, p), nil)
	it := b.Prompts[0].Items
	if len(it) != 3 {
		t.Fatalf("got %d items, want 3 (1, 3, and the mid-line 4)", len(it))
	}
	if it[1].Text != "wie trennen wir das?" {
		t.Errorf("item 2 = %q, want it to stop before the mid-line 4.", it[1].Text)
	}
	if it[2].Text != "und was ist X?" {
		t.Errorf("item 3 = %q", it[2].Text)
	}
}

func TestDecimalIsNotSplit(t *testing.T) {
	// A number that is not opening a sentence — "20.072064s" — must not become a
	// part. The marker needs a line start or sentence end before it.
	p := `{"type":"user","promptId":"p1","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. der lauf braucht 20.072064 sekunden und 3.5 gb ram"}}`
	b := Build(entries(t, p), nil)
	if got := len(b.Prompts[0].Items); got != 1 {
		t.Errorf("got %d items, want 1 — decimals mid-word are not parts", got)
	}
}

// Your real P3: the first question is typed without a number and the numbering
// starts at 2. The answer carries [#1]..[#4]. Matching by position hands answer
// 1 to question 2; matching by the number the user wrote does not.
func TestAnswersMatchTheNumberTheUserWrote(t *testing.T) {
	p := `{"type":"user","promptId":"p1","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"ist die diagnose richtig? 2. hilft ein websocket? 3. überlappen 7 und 8? 4. was wird akzeptiert?"}}`
	a := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"[#1] ja.\n\n[#2] nein.\n\n[#3] teilweise.\n\n[#4] zahlen."}]}}`
	b := Build(entries(t, p, a), nil)
	it := b.Prompts[0].Items
	if len(it) != 4 {
		t.Fatalf("got %d items, want 4 (the unnumbered lead counts as 1)", len(it))
	}
	want := []struct {
		num      int
		id, q, a string
	}{
		{1, "P1.1", "ist die diagnose richtig?", "ja."},
		{2, "P1.2", "hilft ein websocket?", "nein."},
		{3, "P1.3", "überlappen 7 und 8?", "teilweise."},
		{4, "P1.4", "was wird akzeptiert?", "zahlen."},
	}
	for i, w := range want {
		if it[i].Num != w.num || it[i].ID != w.id || it[i].Text != w.q || it[i].Answer != w.a {
			t.Errorf("item %d = {Num:%d ID:%s Text:%q Answer:%q}, want %+v", i, it[i].Num, it[i].ID, it[i].Text, it[i].Answer, w)
		}
	}
}

func TestLeadTextBeforeAOneIsNotAnItem(t *testing.T) {
	p := `{"type":"user","promptId":"p1","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"zwei dinge.\n1. erstes\n2. zweites"}}`
	b := Build(entries(t, p), nil)
	it := b.Prompts[0].Items
	if len(it) != 2 || it[0].Num != 1 || it[1].Num != 2 {
		t.Fatalf("items = %+v, want just 1 and 2 — a preamble before '1.' is not a part", it)
	}
}

func TestQuotedNumberCannotStealAnAnswer(t *testing.T) {
	// "8." inside pasted text becomes a spurious item; it must not receive [#3].
	p := `{"type":"user","promptId":"p1","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. eins\n2. zwei\n3. text: \"regel.\n8. Reporting\"\n4. vier"}}`
	a := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"[#3] drei.\n\n[#4] vier."}]}}`
	b := Build(entries(t, p, a), nil)
	byNum := map[int]*Item{}
	for _, it := range b.Prompts[0].Items {
		byNum[it.Num] = it
	}
	if byNum[3] == nil || byNum[3].Answer != "drei." {
		t.Errorf("item 3 = %+v, want answer 'drei.'", byNum[3])
	}
	if byNum[4] == nil || byNum[4].Answer != "vier." {
		t.Errorf("item 4 = %+v, want answer 'vier.'", byNum[4])
	}
	if byNum[8] != nil && byNum[8].Answer != "" {
		t.Errorf("the quoted 8 took an answer: %q", byNum[8].Answer)
	}
}

// A turn is over when its last assistant message stopped with end_turn, or once
// the next prompt has been typed. A section is final when a later marker follows
// it, or the turn is over — until then Claude may still be writing it.
func TestDoneAndFinalFollowTheTurn(t *testing.T) {
	partial := `{"type":"assistant","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"text","text":"[#1] parser steht.\n\n[#2] tests laufen gerade"}]}}`
	b := Build(entries(t, prompt1, partial), nil)
	p := b.Prompts[0]
	if p.Done {
		t.Error("a turn that stopped for tool_use is not done")
	}
	if !p.Items[0].Final {
		t.Error("item 1 is followed by [#2], so its section is final")
	}
	if p.Items[1].Final {
		t.Error("item 2 is the last section of an unfinished turn — not final")
	}

	ended := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"fertig."}]}}`
	b = Build(entries(t, prompt1, partial, ended), nil)
	p = b.Prompts[0]
	if !p.Done {
		t.Error("end_turn must mark the turn done")
	}
	if !p.Items[1].Final {
		t.Error("once the turn is done every section is final")
	}
	if !strings.HasSuffix(p.Items[1].Answer, "fertig.") {
		t.Errorf("text after the last marker belongs to that section: %q", p.Items[1].Answer)
	}

	b = Build(entries(t, prompt1, partial, prompt2), nil)
	if !b.Prompts[0].Done {
		t.Error("a later prompt means the earlier turn is over")
	}
	if b.Prompts[1].Done {
		t.Error("the new prompt has no reply yet and is not done")
	}
}

// Your real P8: parts written as "#1 …", "#2 …" — the same shape the answer
// markers use. "[#1]" at the start of a part counts too.
func TestHashNumberingIsRecognised(t *testing.T) {
	p := `{"type":"user","promptId":"p1","timestamp":"2026-08-27T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"#1 die Station bietet vier Knöpfe. Wieso?\n#2 Das Ambiguous gefällt mir nicht. #3 und das hier?\n[#4] noch eins"}}`
	a := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"[#1] Server und Station.\n\n[#3] hier.\n\n[#4] eins."}]}}`
	b := Build(entries(t, p, a), nil)
	it := b.Prompts[0].Items
	if len(it) != 4 {
		t.Fatalf("got %d items, want 4: %+v", len(it), it)
	}
	want := []struct {
		num  int
		q, a string
	}{
		{1, "die Station bietet vier Knöpfe. Wieso?", "Server und Station."},
		{2, "Das Ambiguous gefällt mir nicht.", ""},
		{3, "und das hier?", "hier."},
		{4, "noch eins", "eins."},
	}
	for i, w := range want {
		if it[i].Num != w.num || it[i].Text != w.q || it[i].Answer != w.a {
			t.Errorf("item %d = {Num:%d Text:%q Answer:%q}, want %+v", i, it[i].Num, it[i].Text, it[i].Answer, w)
		}
	}
}

func TestHashInProseIsNotAPart(t *testing.T) {
	// "#1" needs a line start or a sentence end before it, like "1." does;
	// an issue reference mid-sentence is prose.
	p := `{"type":"user","promptId":"p1","timestamp":"2026-08-27T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. schau dir ticket #12 an und PR #3 auch"}}`
	b := Build(entries(t, p), nil)
	if got := len(b.Prompts[0].Items); got != 1 {
		t.Errorf("got %d items, want 1", got)
	}
}

// The answer marker is "A#n" — "A" for answer, matching the "#n" the user writes
// in front of a part. "[#n]" is the older shape and still counts, so transcripts
// written before the change keep their pairing.
func TestAnswerMarkerAHash(t *testing.T) {
	p := `{"type":"user","promptId":"p1","timestamp":"2026-08-27T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"#1 eins\n#2 zwei\n#3 drei\n#4 vier\n#5 fünf"}}`
	a := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"Vorwort.\n\nA#1 erste Antwort.\n\n**A#2** zweite.\n\nA#3, A#4 dritte und vierte zusammen.\n\n[#5] alte Form.\n\nSiehe A#1 oben — das ist Prosa, kein Marker."}]}}`
	b := Build(entries(t, p, a), nil)
	it := b.Prompts[0].Items
	want := []string{"erste Antwort.", "zweite.", "dritte und vierte zusammen.", "dritte und vierte zusammen.", "alte Form.\n\nSiehe A#1 oben — das ist Prosa, kein Marker."}
	for i, w := range want {
		if it[i].Answer != w {
			t.Errorf("item %d answer = %q, want %q", it[i].Num, it[i].Answer, w)
		}
	}
}

// A prompt numbered "1., 1.1, 1.2" is answered as "A#1, A#1.1, A#1.2". The
// board has no sub-items — the user's 1.1 already sits inside item 1's text —
// so the sub-sections land on item 1 too: appended in order, each keeping its
// own marker so they stay tellable apart.
func TestSubNumberedMarkersShareTheParentBlock(t *testing.T) {
	a := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"A#1 der Parser steht.\n\n**A#1.1** die Items zuerst.\n\nA#1.2 dann die Marker.\n\nA#2 die Tests laufen."}]}}`
	b := Build(entries(t, prompt1, a), nil)
	it := b.Prompts[0].Items

	want := "der Parser steht.\n\n**A#1.1** die Items zuerst.\n\nA#1.2 dann die Marker."
	if it[0].Answer != want {
		t.Errorf("item 1 answer = %q, want %q", it[0].Answer, want)
	}
	if !it[0].Final {
		t.Error("item 1 must be final — a later marker followed its last section")
	}
	if it[1].Answer != "die Tests laufen." {
		t.Errorf("item 2 answer = %q", it[1].Answer)
	}
}

// Markers arrive as Markdown headings too — "## A#2 · Y3WQN4 reviewen". The
// heading is the answer's own structure, so the section keeps the whole line.
// A letter suffix — "A#1b" — is a sub-part like "A#1.1" and appends. A heading
// that merely mentions a marker mid-line stays prose.
func TestHeadingMarkersCollectAnswers(t *testing.T) {
	a := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"## Vorwort A#1 mittendrin ist Prosa.\n\n## A#2 · Tests reviewen\n\nSchritt eins.\n\n## A#1 · Parser\n\nSteht.\n\n## A#1b · Nachtrag\n\nMehr dazu."}]}}`
	b := Build(entries(t, prompt1, a), nil)
	it := b.Prompts[0].Items

	if got, want := it[1].Answer, "## A#2 · Tests reviewen\n\nSchritt eins."; got != want {
		t.Errorf("item 2 answer = %q, want %q", got, want)
	}
	if got, want := it[0].Answer, "## A#1 · Parser\n\nSteht.\n\n## A#1b · Nachtrag\n\nMehr dazu."; got != want {
		t.Errorf("item 1 answer = %q, want %q", got, want)
	}
}

// A number opening a parenthesis mid-sentence — "(5. gibt es ein limit" — is a
// part of its own; "(3)" stays a reference.
func TestParenthesisNumberIsAnItem(t *testing.T) {
	p := `{"type":"user","promptId":"p1","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. eins\n2. zwei, wie in (1) gesagt (5. gibt es ein limit?) und weiter"}}`
	a := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"A#5 kein Limit."}]}}`
	b := Build(entries(t, p, a), nil)
	it := b.Prompts[0].Items
	if len(it) != 3 || it[0].Num != 1 || it[1].Num != 2 || it[2].Num != 5 {
		t.Fatalf("items = %+v, want 1, 2, 5", it)
	}
	if !strings.Contains(it[1].Text, "wie in (1) gesagt") || strings.Contains(it[1].Text, "limit") {
		t.Errorf("item 2 text = %q", it[1].Text)
	}
	if it[2].Answer != "kein Limit." {
		t.Errorf("item 5 answer = %q", it[2].Answer)
	}
}

// An answer whose number no typed part carries becomes an item of its own —
// question-less, marked Orphan, appended in the order the reply produced it.
func TestOrphanAnswerBecomesItem(t *testing.T) {
	a := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"A#1 eins erledigt.\n\nA#7 sieben dazu.\n\n## A#9 · Neun\n\nneun auch."}]}}`
	b := Build(entries(t, prompt1, a), nil)
	it := b.Prompts[0].Items
	if len(it) != 4 {
		t.Fatalf("items = %d, want 2 typed + 2 orphans", len(it))
	}
	if it[0].Orphan || it[1].Orphan {
		t.Error("typed parts must not be orphans")
	}
	seven, nine := it[2], it[3]
	if seven.Num != 7 || !seven.Orphan || seven.ID != "P1.7" || seven.Text != "" || seven.Answer != "sieben dazu." {
		t.Errorf("orphan 7 = %+v", seven)
	}
	if nine.Num != 9 || !nine.Orphan || nine.Answer != "## A#9 · Neun\n\nneun auch." {
		t.Errorf("orphan 9 = %+v", nine)
	}
}

// Grouped siblings — "A#1.1, A#1.2" in one run — name the same item twice; the
// shared section must land there once, not doubled.
func TestGroupedSubMarkersLandOnce(t *testing.T) {
	a := `{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":[{"type":"text","text":"A#1.1, A#1.2 beide zusammen."}]}}`
	b := Build(entries(t, prompt1, a), nil)
	if got, want := b.Prompts[0].Items[0].Answer, "A#1.1, A#1.2 beide zusammen."; got != want {
		t.Errorf("item 1 answer = %q, want %q", got, want)
	}
}
