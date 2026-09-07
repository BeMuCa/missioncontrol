package tui

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/BeMuCa/missioncontrol/core/board"
	"github.com/BeMuCa/missioncontrol/core/done"
	"github.com/BeMuCa/missioncontrol/core/favorite"
	"github.com/BeMuCa/missioncontrol/core/transcript"
)

func press(t *testing.T, m *Model, keys ...rune) {
	t.Helper()
	for _, r := range keys {
		k := tea.KeyPressMsg{Code: r, Text: string(r)}
		if got := k.String(); got != string(r) {
			t.Fatalf("key %q renders as %q — the test is not pressing what it thinks", r, got)
		}
		m.Update(k)
	}
}

func enter(t *testing.T, m *Model) {
	t.Helper()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
}

// twoPrompts is two requests; the first has two numbered parts of which only the
// second was answered with a marker.
func twoPrompts(t *testing.T) *Model {
	t.Helper()
	lines := []string{
		`{"type":"user","promptId":"p1","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. parser bauen\n2. tests starten"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Vorwort.\n\n[#2] Die Tests laufen."}]}}`,
		`{"type":"user","promptId":"p2","timestamp":"2026-08-26T10:05:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"und jetzt weiter"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"weiter gemacht"}]}}`,
	}
	var es []transcript.Entry
	for _, l := range lines {
		var e transcript.Entry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatal(err)
		}
		es = append(es, e)
	}
	m := &Model{board: board.Build(es, nil)}
	m.rebuild()
	return m
}

func TestRotateSnapsToRequestAndWalksWithLeftRight(t *testing.T) {
	m := twoPrompts(t)

	// Newest first, so rows are: P2, P1, P1.1, P1.2.
	m.idx = 3
	if m.rows[m.idx].kind != rowItem {
		t.Fatalf("row 3 is %v, expected a part", m.rows[m.idx].kind)
	}

	press(t, m, 't')
	if !m.stacked {
		t.Fatal("t must rotate the board")
	}
	if m.rows[m.idx].kind != rowPrompt {
		t.Error("rotating must snap the selection onto the request that owns the part")
	}
	if m.rows[m.idx].prompt.ID != "P1" {
		t.Errorf("snapped to %s, want P1", m.rows[m.idx].prompt.ID)
	}

	// Left and right walk requests only, and stop at the ends.
	press(t, m, 'h')
	if m.rows[m.idx].prompt.ID != "P2" {
		t.Errorf("left went to %s, want P2", m.rows[m.idx].prompt.ID)
	}
	press(t, m, 'h')
	if m.rows[m.idx].prompt.ID != "P2" {
		t.Error("left at the first request must stay put")
	}
	press(t, m, 'l', 'l')
	if m.rows[m.idx].prompt.ID != "P1" {
		t.Errorf("right went to %s, want P1 and no further", m.rows[m.idx].prompt.ID)
	}

	press(t, m, 't')
	if m.stacked {
		t.Error("t must rotate back")
	}
}

// longAnswer is one request with two parts; part 1 has a finished multi-line
// answer, part 2 is still being written (no later marker, no end_turn).
func longAnswer(t *testing.T) *Model {
	t.Helper()
	lines := []string{
		`{"type":"user","promptId":"p1","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. parser bauen\n2. tests starten"}}`,
		`{"type":"assistant","message":{"role":"assistant","stop_reason":"tool_use","content":[{"type":"text","text":"[#1] Erste Zeile der Antwort.\n\nZweite Zeile der Antwort.\n\nDritte Zeile der Antwort.\n\n[#2] Tests laufen noch"}]}}`,
	}
	var es []transcript.Entry
	for _, l := range lines {
		var e transcript.Entry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatal(err)
		}
		es = append(es, e)
	}
	m := &Model{board: board.Build(es, nil), stacked: true}
	m.rebuild()
	return m
}

func TestStackShowsOnlyFinishedAnswers(t *testing.T) {
	m := twoPrompts(t)
	m.stacked = true

	// P1's turn is over (P2 followed it): part 2 has its answer, part 1 was
	// never marked — its box stays, showing the open question, but the whole
	// reply is not dumped in as a fallback.
	m.idx = 1
	out := plain(m.stack(90, 30))
	for _, want := range []string{"#1 · parser bauen", "no marked answer", "#2 · tests starten", "A#2 ", "Die Tests laufen."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	for _, bad := range []string{"P1.", "full reply", "Vorwort"} {
		if strings.Contains(out, bad) {
			t.Errorf("unwanted %q in:\n%s", bad, out)
		}
	}

	// P2 has no parts, so the request itself is the one box; its turn has not
	// ended, so the answer is held back behind the spinner.
	m.idx = 0
	out = plain(m.stack(90, 30))
	if !strings.Contains(out, "P2") || !strings.Contains(out, "und jetzt weiter") {
		t.Errorf("the request must be the box's header:\n%s", out)
	}
	if !strings.Contains(out, "waiting") || strings.Contains(out, "weiter gemacht") {
		t.Errorf("an unfinished answer must show as waiting, not as text:\n%s", out)
	}
}

func TestClosedBoxIsFourLinesAndEnterOpensIt(t *testing.T) {
	m := longAnswer(t)
	m.idx = 1 // P1.1
	box := m.stack(90, 40)
	// Every closed box is header + one answer line inside a border: four lines
	// each, two boxes, nothing else.
	if len(box) != 8 {
		t.Fatalf("two closed boxes are %d lines, want 8:\n%s", len(box), plain(box))
	}
	out := plain(box)
	if !strings.Contains(out, "Erste Zeile") || strings.Contains(out, "Zweite Zeile") {
		t.Errorf("closed box must show exactly the first answer line:\n%s", out)
	}

	enter(t, m)
	out = plain(m.stack(90, 40))
	if !strings.Contains(out, "Zweite Zeile") || !strings.Contains(out, "Dritte Zeile") {
		t.Errorf("enter must open the box to the whole answer:\n%s", out)
	}
	enter(t, m)
	if got := len(m.stack(90, 40)); got != 8 {
		t.Errorf("enter again must close it: %d lines, want 8", got)
	}
}

func TestMTogglesEveryBox(t *testing.T) {
	m := longAnswer(t)
	m.idx = 1
	press(t, m, 'm')
	for _, id := range []string{"P1.1", "P1.2"} {
		if !m.open[id] {
			t.Errorf("m must open %s", id)
		}
	}
	press(t, m, 'm')
	if len(m.open) != 0 {
		t.Errorf("m again must close everything, still open: %v", m.open)
	}
}

func TestUpDownWalkBoxesAndScrollAnOpenOne(t *testing.T) {
	m := longAnswer(t)
	m.width, m.height = 90, 40
	m.idx = 0 // the request: nothing underneath selected yet
	press(t, m, 'j')
	if m.rows[m.idx].kind != rowItem || m.rows[m.idx].item.ID != "P1.1" {
		t.Fatalf("down from the request must select the first box, got row %d", m.idx)
	}
	press(t, m, 'j', 'j')
	if m.rows[m.idx].item.ID != "P1.2" {
		t.Errorf("down stops at the last box, got %s", m.rows[m.idx].item.ID)
	}
	press(t, m, 'k', 'k')
	if m.rows[m.idx].kind != rowPrompt {
		t.Error("up from the first box must go back to the request")
	}

	press(t, m, 'j')
	enter(t, m) // open P1.1
	before := m.idx
	press(t, m, 'j')
	if m.idx != before || m.scroll != 1 {
		t.Errorf("with the box open, down must scroll it (idx %d→%d, scroll %d)", before, m.idx, m.scroll)
	}
}

func TestStripIsAFifthOfTheWindowAtMost(t *testing.T) {
	for h, want := range map[int]int{40: 8, 100: 20, 24: 4, 10: 2} {
		if got := stripHeight(h); got != want {
			t.Errorf("stripHeight(%d) = %d, want %d", h, got, want)
		}
	}
}

func TestSelectionColours(t *testing.T) {
	m := longAnswer(t)
	m.idx = 1
	strip := strings.Join(m.strip(120, 8), "\n")
	if !strings.Contains(strip, "38;5;"+greenCode) {
		t.Error("the selected request in the strip must be green")
	}
	stack := strings.Join(m.stack(90, 40), "\n")
	if !strings.Contains(stack, "38;5;"+pinkCode) {
		t.Error("the selected box must be pink")
	}
}

func TestStripKeepsBoxesTheSameHeight(t *testing.T) {
	m := twoPrompts(t)
	m.stacked, m.idx = true, 1
	lines := m.strip(120, 9)
	if len(lines) != 9 {
		t.Fatalf("strip is %d lines, want the 9 it was given", len(lines))
	}
	// Both boxes must close on the same line, or the strip looks broken.
	last := plain([]string{lines[len(lines)-1]})
	if strings.Count(last, "╰") != 2 {
		t.Errorf("expected both boxes to close on the last line, got %q", last)
	}
}

func TestNavigationKeysGoThroughUpdate(t *testing.T) {
	m := testModel(t) // one request, two parts, two agents
	if len(m.rows) != 5 {
		t.Fatalf("rows = %d, want request + 2 parts + 2 agents", len(m.rows))
	}

	press(t, m, 'j', 'j')
	if m.idx != 2 {
		t.Errorf("after two j the cursor is at %d, want 2", m.idx)
	}
	press(t, m, 'k')
	if m.idx != 1 {
		t.Errorf("after k the cursor is at %d, want 1", m.idx)
	}

	press(t, m, 'a')
	if !m.hideAgents {
		t.Error("a must hide the agents")
	}
	for _, r := range m.rows {
		if r.kind == rowAgent {
			t.Fatal("an agent row survived the a key")
		}
	}
	press(t, m, 'a')
	if m.hideAgents {
		t.Error("a must bring them back")
	}

	// Right hands the arrows to the text pane; down then scrolls instead of moving.
	press(t, m, 'l')
	if !m.onText {
		t.Fatal("l must focus the text pane")
	}
	before := m.idx
	press(t, m, 'j')
	if m.idx != before {
		t.Errorf("with the text focused, j must scroll, not move the cursor (idx %d → %d)", before, m.idx)
	}
	if m.scroll != 1 {
		t.Errorf("scroll = %d, want 1", m.scroll)
	}
	press(t, m, 'h')
	if m.onText {
		t.Error("h must hand the arrows back to the list")
	}
}

// Findings of the acceptance pass: a closed box must be four lines whatever the
// question looks like, and nothing rendered may be wider than the window.
func TestClosedBoxStaysFourLinesForAwkwardQuestions(t *testing.T) {
	long := strings.Repeat("warum ist der build so langsam ", 8)
	multi := "erstes\nzweites\ndrittes"
	for _, q := range []string{long, multi} {
		m := stackedWith(t, "1. "+q+"\n2. zwei", "[#1] "+strings.Repeat("antwort ", 60)+"\n\n[#2] ok", "end_turn")
		m.idx = 1
		got := m.stack(90, 40)
		first := plain(got[:min(len(got), 4)])
		if n := len(got); n != 8 {
			t.Errorf("q=%q: two closed boxes are %d lines, want 8:\n%s", q[:12], n, first)
		}
	}
}

func TestRenderNeverExceedsTheWindow(t *testing.T) {
	m := longAnswer(t)
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 20}, {40, 12}, {40, 9}, {40, 7}} {
		w, h := size[0], size[1]
		m.width, m.height = w, h
		out := strings.Split(m.render(w, h), "\n")
		if len(out) > h {
			t.Errorf("%dx%d: rendered %d lines", w, h, len(out))
		}
		for i, l := range out {
			if lw := lipgloss.Width(l); lw > w {
				t.Errorf("%dx%d: line %d is %d cells wide: %q", w, h, i, lw, plain([]string{l}))
			}
		}
	}
}

func TestStripIsNeverOverAFifth(t *testing.T) {
	for h := 5; h <= 60; h++ {
		if got := stripHeight(h); got > h/5 {
			t.Errorf("stripHeight(%d) = %d exceeds a fifth", h, got)
		}
	}
}

// A part that never got a marked answer keeps its box once the turn is over:
// the open question is worth seeing, marked as unanswered instead of hidden.
func TestUnansweredPartKeepsItsBox(t *testing.T) {
	m := twoPrompts(t) // P1: part 1 unmarked, turn over
	m.stacked, m.idx = true, 1
	out := plain(m.stack(90, 30))
	if !strings.Contains(out, "#1 ·") || !strings.Contains(out, "no marked answer") {
		t.Errorf("unanswered part 1 must keep its box:\n%s", out)
	}
	if !strings.Contains(out, "#2 ·") {
		t.Errorf("answered part 2 must keep its box:\n%s", out)
	}
	press(t, m, 'j')
	if m.rows[m.idx].item.ID != "P1.1" {
		t.Errorf("down must land on the unanswered part's box, landed on %s", rowID(m.rows[m.idx]))
	}
}

// An orphan answer — its marker names no typed part — gets a box whose header
// carries Claude's marker in place of the missing question.
func TestOrphanAnswerBox(t *testing.T) {
	m := stackedWith(t, "1. parser bauen", "A#1 gebaut.\n\nA#3 und das noch dazu.", "end_turn")
	m.idx = 0
	out := plain(m.stack(90, 30))
	for _, want := range []string{"#1 · parser bauen", "A#3 · (answer without a question)", "und das noch dazu."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestScrollingAnOpenBoxStopsAtItsEnd(t *testing.T) {
	m := longAnswer(t)
	m.width, m.height = 90, 40
	m.idx = 1
	enter(t, m)
	n := len(m.stack(90, 40))
	prev := m.scroll
	for i := 0; i < n+5; i++ {
		press(t, m, 'j')
		m.stack(90, 40)
		if m.scroll < prev {
			t.Fatalf("press %d: scroll went back from %d to %d — snapped to the top", i, prev, m.scroll)
		}
		prev = m.scroll
	}
	if m.scroll >= n {
		t.Errorf("scroll %d ran past the box (%d lines)", m.scroll, n)
	}
}

func stackedWith(t *testing.T, prompt, reply, stop string) *Model {
	t.Helper()
	q, _ := json.Marshal(prompt)
	a, _ := json.Marshal(reply)
	lines := []string{
		`{"type":"user","promptId":"p1","timestamp":"2026-08-26T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":` + string(q) + `}}`,
		`{"type":"assistant","message":{"role":"assistant","stop_reason":"` + stop + `","content":[{"type":"text","text":` + string(a) + `}]}}`,
	}
	var es []transcript.Entry
	for _, l := range lines {
		var e transcript.Entry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatal(err)
		}
		es = append(es, e)
	}
	m := &Model{board: board.Build(es, nil), stacked: true}
	m.rebuild()
	return m
}

// A box carries the user's own marker on the question and Claude's on the
// answer, so a reply that says "see A#3" can be followed by eye. The board's
// P-ids stay on the requests, where nothing else names them.
func TestBoxShowsUserAndAnswerMarkers(t *testing.T) {
	m := stackedWith(t, "#1 warum?\n#2 wie?", "A#1 darum.\n\nA#2 so, siehe A#1.", "end_turn")
	m.idx = 2 // part 2
	out := plain(m.stack(80, 20))
	for _, want := range []string{"#1 · warum?", "A#1 darum.", "#2 · wie?", "A#2 so, siehe A#1."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "P1.") {
		t.Errorf("P-ids must not label the parts:\n%s", out)
	}
	if n := len(m.stack(80, 20)); n != 8 {
		t.Errorf("labels must not cost a line: %d lines, want 8", n)
	}
}

// starModel is longAnswer with a favorites store and a done store attached.
func starModel(t *testing.T) *Model {
	t.Helper()
	m := longAnswer(t)
	favs, err := favorite.Load(filepath.Join(t.TempDir(), "f.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	marks, err := done.Load(filepath.Join(t.TempDir(), "d.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	m.favs, m.done, m.session = favs, marks, "sess"
	m.now = func() string { return "2026-08-28T10:00:00Z" }
	m.width, m.height = 100, 30
	return m
}

func TestStarredBoxIsGoldAndTheRequestInheritsIt(t *testing.T) {
	m := starModel(t)
	m.idx = 1 // #1, answered
	press(t, m, 'f')
	box := strings.Join(m.stack(100, 20), "\n")
	if !strings.Contains(plain([]string{box}), "★") || !strings.Contains(box, "38;5;"+goldCode) {
		t.Errorf("a starred box must carry a gold star:\n%s", plain([]string{box}))
	}
	strip := plain(m.strip(100, 6))
	if !strings.Contains(strip, "★") {
		t.Errorf("the request in the strip must show that a part inside is starred:\n%s", strip)
	}
	press(t, m, 'f')
	if strings.Contains(plain(m.strip(100, 6)), "★") {
		t.Error("unstarring must take the star off the request again")
	}
}

func TestDMarksABoxDoneAndPersists(t *testing.T) {
	m := starModel(t)
	m.idx = 1
	press(t, m, 'd')
	if !strings.Contains(plain(m.stack(100, 20)), "✓") {
		t.Errorf("d must put a check on the box:\n%s", plain(m.stack(100, 20)))
	}
	if !m.done.Has("sess#P1.1") {
		t.Error("the mark must be in the store")
	}
	press(t, m, 'd')
	if m.done.Has("sess#P1.1") || strings.Contains(plain(m.stack(100, 20)), "✓") {
		t.Error("d again must clear the mark")
	}
	if n := len(m.stack(100, 20)); n != 8 {
		t.Errorf("marks must not cost a line: %d lines, want 8", n)
	}
}

func TestFavoritesScreenShowsBoxesAndConfirmsRemoval(t *testing.T) {
	m := starModel(t)
	m.idx = 1
	press(t, m, 'f', 'F')
	out := plain(strings.Split(m.render(100, 30), "\n"))
	if !strings.Contains(out, "╭") || !strings.Contains(out, "parser bauen") || !strings.Contains(out, "Zweite Zeile") {
		t.Fatalf("favorites must show as a box on the left with the answer on the right:\n%s", out)
	}

	press(t, m, 'f') // ask to remove
	out = plain(strings.Split(m.render(100, 30), "\n"))
	if !strings.Contains(out, "y") || !strings.Contains(strings.ToLower(out), "remove") {
		t.Fatalf("f on a favorite must ask for confirmation:\n%s", out)
	}
	press(t, m, 'n')
	if len(m.favs.List()) != 1 {
		t.Fatal("any key but y must keep the favorite")
	}
	press(t, m, 'f', 'y')
	if len(m.favs.List()) != 0 {
		t.Error("f then y must remove it")
	}
}
