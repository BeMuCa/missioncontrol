package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/BeMuCa/missioncontrol/core/board"
	"github.com/BeMuCa/missioncontrol/core/favorite"
	"github.com/BeMuCa/missioncontrol/core/transcript"
)

func testModel(t *testing.T) *Model {
	t.Helper()
	line := `{"type":"user","promptId":"p1","timestamp":"2026-08-25T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. parser bauen\n2. tests starten"}}`
	var e transcript.Entry
	if err := json.Unmarshal([]byte(line), &e); err != nil {
		t.Fatal(err)
	}
	runs := []transcript.AgentRun{
		{AgentID: "a", Description: "parser bauen und lesen", ToolUseID: "t1", PromptID: "p1", Started: time.Now()},
		{AgentID: "b", Description: "etwas ganz anderes", ToolUseID: "t2", PromptID: "p1", Started: time.Now()},
	}
	m := &Model{board: board.Build([]transcript.Entry{e}, runs)}
	m.rebuild()
	return m
}

func TestHideAgentsDropsOnlyAgentRows(t *testing.T) {
	m := testModel(t)
	var prompts, items, agents int
	for _, r := range m.rows {
		switch r.kind {
		case rowPrompt:
			prompts++
		case rowItem:
			items++
		case rowAgent:
			agents++
		}
	}
	if prompts != 1 || items != 2 || agents != 2 {
		t.Fatalf("with agents shown: %d prompts, %d items, %d agents", prompts, items, agents)
	}

	m.hideAgents = true
	m.rebuild()
	for _, r := range m.rows {
		if r.kind == rowAgent {
			t.Fatalf("agent row survived hiding: %+v", r)
		}
	}
	if len(m.rows) != 3 {
		t.Errorf("got %d rows, want the prompt and its 2 items", len(m.rows))
	}
}

func TestSelectedRequestIsNotCapped(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "wort "
	}
	p := &board.Prompt{ID: "P1", Text: long}
	r := row{kind: rowPrompt, prompt: p}
	if got := len(blockLines(r, 40, false, false)); got != blockCap+2 {
		t.Errorf("unselected block = %d lines, want head + %d + ellipsis", got, blockCap)
	}
	if got := len(blockLines(r, 40, true, false)); got <= blockCap+2 {
		t.Errorf("selected block = %d lines, want the whole text", got)
	}
}

func TestFocusChangesSelectionStyleAndFooter(t *testing.T) {
	m := testModel(t)
	sel := row{kind: rowPrompt, prompt: m.board.Prompts[0]}

	onList := m.rowStyle(sel, true)
	m.onText = true
	onTextStyle := m.rowStyle(sel, true)
	if onList.GetForeground() == onTextStyle.GetForeground() {
		t.Error("the selection must look different once the text pane has focus")
	}
	if !strings.Contains(m.footer(200), "pane (text)") {
		t.Errorf("footer = %q, want it to name the focused pane", m.footer(200))
	}

	m.onText = false
	if !strings.Contains(m.footer(200), "pane (list)") {
		t.Errorf("footer = %q", m.footer(200))
	}
	m.hideAgents = true
	if !strings.Contains(m.footer(200), "agents hidden") {
		t.Errorf("footer = %q, want the hidden state shown", m.footer(200))
	}
}

// answerModel is a prompt with two numbered parts where only the second answer
// carried a marker.
func answerModel(t *testing.T) *Model {
	t.Helper()
	lines := []string{
		`{"type":"user","promptId":"p1","timestamp":"2026-08-25T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"1. parser bauen\n2. tests starten"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Vorwort.\n\n[#2] Die Tests laufen."}]}}`,
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

func TestDetailShowsAnswerNotQuestion(t *testing.T) {
	m := answerModel(t)

	// rows: prompt, item 1, item 2
	if len(m.rows) != 3 || m.rows[2].kind != rowItem {
		t.Fatalf("unexpected rows: %d", len(m.rows))
	}

	m.idx = 2 // the marked part
	got := plain(m.detail(60))
	if !strings.Contains(got, "Die Tests laufen.") {
		t.Errorf("marked part must show its answer, got %q", got)
	}
	if strings.Contains(got, "tests starten") {
		t.Errorf("the question must not be repeated on the right, got %q", got)
	}

	m.idx, m.scroll = 1, 0 // the unmarked part falls back to the whole turn
	got = plain(m.detail(60))
	if !strings.Contains(got, "not marked") || !strings.Contains(got, "Vorwort.") {
		t.Errorf("unmarked part must fall back to the full reply, got %q", got)
	}

	m.idx, m.scroll = 0, 0 // the prompt itself shows the reply, not its own text
	got = plain(m.detail(60))
	if strings.Contains(got, "parser bauen") {
		t.Errorf("prompt pane must not repeat the prompt, got %q", got)
	}
	if !strings.Contains(got, "Vorwort.") {
		t.Errorf("prompt pane must show the reply, got %q", got)
	}
}

// plain drops the styling so an assertion sees the text a reader sees.
func plain(lines []string) string {
	return ansiRE.ReplaceAllString(strings.Join(lines, "\n"), "")
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestMarkdownRendersTables(t *testing.T) {
	m := &Model{}
	md := "Vorher.\n\n| ID | Was |\n|----|-----|\n| P1 | erste Anfrage |\n| P2 | zweite Anfrage |\n\nNachher.\n"
	out := plain(m.markdown(md, 60))
	t.Log("\n" + out)

	if strings.Contains(out, "|----") {
		t.Error("the table separator is still raw markdown")
	}
	if !strings.ContainsAny(out, "─│┌└├") {
		t.Errorf("no table drawn:\n%s", out)
	}
	for _, want := range []string{"erste Anfrage", "zweite Anfrage", "Vorher.", "Nachher."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func favModel(t *testing.T) *Model {
	t.Helper()
	m := answerModel(t) // request + 2 parts, item 2 marked-answered
	store, err := favorite.Load(filepath.Join(t.TempDir(), "f.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	m.favs = store
	m.session = "sess"
	m.now = func() string { return "2026-08-26T10:00:00Z" }
	return m
}

func TestFavoriteTogglesOnlySavablePairs(t *testing.T) {
	m := favModel(t)

	// Find the answered part (item 2) and an unanswered part (item 1).
	var answered, unanswered int = -1, -1
	for i, r := range m.rows {
		if r.kind == rowItem && r.item.Answer != "" {
			answered = i
		}
		if r.kind == rowItem && r.item.Answer == "" {
			unanswered = i
		}
	}
	if answered < 0 || unanswered < 0 {
		t.Fatalf("fixture missing rows: answered=%d unanswered=%d", answered, unanswered)
	}

	// A part with no answer of its own cannot be saved.
	m.idx = unanswered
	press(t, m, 'f')
	if len(m.favs.List()) != 0 {
		t.Fatalf("an unanswered part must not be savable, got %d", len(m.favs.List()))
	}

	// The answered part saves, and stars.
	m.idx = answered
	press(t, m, 'f')
	if len(m.favs.List()) != 1 {
		t.Fatalf("expected 1 favorite, got %d", len(m.favs.List()))
	}
	if !m.starred(m.rows[answered]) {
		t.Error("the saved row must show as starred")
	}

	// f again removes it.
	press(t, m, 'f')
	if len(m.favs.List()) != 0 {
		t.Errorf("second f must remove it, got %d", len(m.favs.List()))
	}
}

func TestFavoritesScreenTogglesAndRenders(t *testing.T) {
	m := favModel(t)
	m.width, m.height = 100, 30
	for i, r := range m.rows {
		if r.kind == rowItem && r.item.Answer != "" {
			m.idx = i
		}
	}
	press(t, m, 'f') // save one
	press(t, m, 'F') // open the favorites screen
	if !m.showFavs {
		t.Fatal("F must open the favorites screen")
	}
	out := plain(strings.Split(m.render(100, 30), "\n"))
	if !strings.Contains(out, "favorites") {
		t.Errorf("favorites screen missing its heading:\n%s", out)
	}
	if !strings.Contains(out, "Die Tests laufen.") {
		t.Errorf("the saved answer must show on the favorites screen:\n%s", out)
	}
	press(t, m, 'F')
	if m.showFavs {
		t.Error("F must close it again")
	}
}

func TestFavoritesEnterScrollsTheAnswer(t *testing.T) {
	m := favModel(t)
	m.width, m.height = 100, 30
	for i, r := range m.rows {
		if r.kind == rowItem && r.item.Answer != "" {
			m.idx = i
		}
	}
	press(t, m, 'f') // save one
	press(t, m, 'F') // open the favorites screen

	// Enter hands the arrow keys to the answer: down scrolls instead of moving.
	enter(t, m)
	if !m.favOnText {
		t.Fatal("enter must focus the answer pane")
	}
	press(t, m, 'j')
	if m.scroll != 1 || m.favIdx != 0 {
		t.Fatalf("j must scroll the answer, not the list: scroll=%d favIdx=%d", m.scroll, m.favIdx)
	}
	press(t, m, 'k')
	if m.scroll != 0 {
		t.Fatalf("k must scroll back up, got %d", m.scroll)
	}

	// esc steps back to the list instead of quitting.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.favOnText || !m.showFavs {
		t.Fatalf("esc must return to the list: favOnText=%v showFavs=%v", m.favOnText, m.showFavs)
	}
	press(t, m, 'j')
	if m.scroll != 0 {
		t.Fatalf("back on the list, j must move the selection, not scroll: scroll=%d", m.scroll)
	}
}

// GitHub tables need no leading pipe, and the [L1] tags the operating rules put
// in front of the first cell leave every body row without one.
func TestMarkdownTableRowsWithoutLeadingPipe(t *testing.T) {
	m := &Model{}
	md := "| | Commit | was |\n|---|---|---|\n[L1] | `4ba8` | Lücke weg |\n[L2] | `7c24` | Handquelle zählt |\n"
	out := plain(m.markdown(md, 80))
	t.Log("\n" + out)
	if strings.Contains(out, "] |") {
		t.Errorf("body rows fell through as raw markdown:\n%s", out)
	}
	for _, want := range []string{"[L1]", "4ba8", "Lücke weg", "[L2]", "Handquelle zählt"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "│") < 4 {
		t.Errorf("body rows are not drawn as table cells:\n%s", out)
	}
}

// A cell wider than its column wraps onto further lines of the same row; nothing
// Claude wrote is cut away.
func TestMarkdownTableWrapsLongCells(t *testing.T) {
	m := &Model{}
	long := "ein sehr langer zelleninhalt der auf keinen fall in eine einzige spalte passen wird und deshalb umgebrochen werden muss"
	md := "| id | text |\n|---|---|\n| a | " + long + " |\n| b | kurz |\n"
	out := plain(m.markdown(md, 60))
	t.Log("\n" + out)
	if strings.Contains(out, "…") {
		t.Errorf("cell was truncated:\n%s", out)
	}
	for _, w := range strings.Fields(long) {
		if !strings.Contains(out, w) {
			t.Errorf("word %q lost:\n%s", w, out)
		}
	}
	for i, l := range strings.Split(out, "\n") {
		if lipgloss.Width(l) > 60 {
			t.Errorf("line %d is %d wide: %q", i, lipgloss.Width(l), l)
		}
	}
}

// Shrinking a too-wide table takes room from the columns that have it. A tag
// column of four characters stays four; the prose column wraps.
func TestMarkdownTableShrinksTheWideColumnFirst(t *testing.T) {
	m := &Model{}
	long := strings.Repeat("wort ", 40)
	md := "| | Commit | was |\n|---|---|---|\n[L1] | `4ba80bc7` | " + long + "|\n"
	out := plain(m.markdown(md, 80))
	t.Log("\n" + out)
	for _, want := range []string{"[L1]", "4ba80bc7"} {
		if !strings.Contains(out, want) {
			t.Errorf("narrow column was cut, %q missing:\n%s", want, out)
		}
	}
}

func TestMarkdownTableCellsStyleInlineMarkup(t *testing.T) {
	m := &Model{}
	md := "| a | b |\n|---|---|\n| **fett** | `code` |\n"
	out := plain(m.markdown(md, 60))
	if strings.Contains(out, "**") || strings.Contains(out, "`") {
		t.Errorf("inline markers left raw inside cells:\n%s", out)
	}
	if !strings.Contains(out, "fett") || !strings.Contains(out, "code") {
		t.Errorf("cell text lost:\n%s", out)
	}
}

func TestRefreshRereadsARewrittenTranscript(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`{"type":"user","promptId":"p1","timestamp":"2026-08-25T10:00:00Z","origin":{"kind":"human"},"message":{"role":"user","content":"`+content+`"}}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("alte frage")
	m := New(path, filepath.Join(t.TempDir(), "none"), "s", "", "", nil, nil, nil)
	m.reload()
	if len(m.board.Prompts) != 1 || m.board.Prompts[0].Text != "alte frage" {
		t.Fatalf("prompts = %+v", m.board.Prompts)
	}

	// A rewritten file is shorter than the old offset: the poll sees nothing.
	write("neue frage")
	m.reload()
	if m.board.Prompts[0].Text != "alte frage" {
		t.Fatalf("poll should not have seen the rewrite, got %q", m.board.Prompts[0].Text)
	}

	m.key(tea.KeyPressMsg{Code: 'r', Text: "r"})
	if len(m.board.Prompts) != 1 || m.board.Prompts[0].Text != "neue frage" {
		t.Errorf("after r: prompts = %+v, want the rewritten transcript", m.board.Prompts)
	}
	if !strings.Contains(m.footer(200), "r refresh") {
		t.Errorf("footer = %q, want the r hint", m.footer(200))
	}
}

func TestEscQuitsUnlessTheLauncherIsBehindIt(t *testing.T) {
	// A board opened directly on a session has nowhere to go back to, so esc
	// still means quit there.
	m := testModel(t)
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.WentBack() {
		t.Error("esc must not report a return when there is no launcher")
	}

	m = testModel(t)
	m.AllowBack()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if !m.WentBack() {
		t.Error("esc must return to the launcher once AllowBack is set")
	}
}

func TestEscClosesAnOverlayBeforeLeavingTheBoard(t *testing.T) {
	m := testModel(t)
	m.AllowBack()
	m.showSessions = true
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.showSessions {
		t.Error("esc must close the session picker first")
	}
	if m.WentBack() {
		t.Error("closing an overlay must not also leave the board")
	}
}

// The hint chain is longer than an ordinary terminal, so anything appended to
// its end is never seen. The way back to the launcher has to survive the cut.
func TestBackHintSurvivesFooterTruncation(t *testing.T) {
	m := testModel(t)
	m.AllowBack()
	m.stacked = true
	if got := m.footer(100); !strings.Contains(got, "esc projects") {
		t.Errorf("footer at 100 columns = %q, want the way back to the launcher in it", got)
	}

	// A board opened directly on a session has no launcher behind it and must
	// not claim otherwise.
	direct := testModel(t)
	direct.stacked = true
	if got := direct.footer(100); strings.Contains(got, "esc projects") {
		t.Errorf("footer = %q, want no way-back hint when there is no launcher", got)
	}
}
