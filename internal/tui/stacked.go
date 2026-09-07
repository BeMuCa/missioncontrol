package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/BeMuCa/missioncontrol/core/board"
)

// The stacked layout turns the board a quarter turn: requests become a strip of
// boxes across the top, walked with left and right, and the selected one opens
// up underneath as a stack of boxes — one per part, each holding the question
// and the answer it got. It suits reading one request closely; the split layout
// suits scanning many.

// stripBox is how wide one request box is in the strip.
const stripBox = 36

// Colour codes are kept as strings so a test can look for them in the output.
const (
	greenCode = "78"
	pinkCode  = "217"
	goldCode  = "220"
)

var (
	colGreen = lipgloss.Color(greenCode)
	colPink  = lipgloss.Color(pinkCode)
	colGold  = lipgloss.Color(goldCode)
	styGreen = lipgloss.NewStyle().Foreground(colGreen).Bold(true)
	styPink  = lipgloss.NewStyle().Foreground(colPink).Bold(true)
	styGold  = lipgloss.NewStyle().Foreground(colGold).Bold(true)
)

// spinner turns once every four seconds: the board polls once a second, and a
// faster animation would mean polling faster for nothing else.
var spinner = []string{"◐", "◓", "◑", "◒"}

// stripHeight is the strip's share of the window: a fifth. Below 15 rows that
// is a border with nothing inside, which is what a window that small deserves.
func stripHeight(height int) int {
	return height / 5
}

func (m *Model) renderStacked(width, height int) string {
	topH := stripHeight(height)
	botH := max(height-topH-3, 1)

	// A box is never under three lines, so a tiny strip is cut to its budget.
	top := clip(m.strip(width, topH), topH)
	bottom := clip(m.stack(width, botH), botH)

	return m.header(width) + "\n" +
		strings.Join(top, "\n") + "\n" +
		strings.Join(bottom, "\n") + "\n" +
		m.footer(width)
}

// strip lays the requests out left to right, windowed so the selected one is
// always on screen.
func (m *Model) strip(width, height int) []string {
	prompts := m.promptRows()
	if len(prompts) == 0 {
		return clip([]string{styMeta.Render("nothing typed yet")}, height)
	}

	// Walk back from the selected box until the window is full, so the selection
	// sits at the right edge when moving forward and scrolls with you.
	perRow := max(width/stripBox, 1)
	sel := m.promptRow()
	cur := 0
	for i, idx := range prompts {
		if idx == sel {
			cur = i
		}
	}
	first := max(cur-perRow+1, 0)

	var boxes []string
	for _, idx := range prompts[first:min(first+perRow, len(prompts))] {
		p := m.rows[idx].prompt
		boxes = append(boxes, m.stripBox(p, idx == sel, height))
	}
	return strings.Split(lipgloss.JoinHorizontal(lipgloss.Top, boxes...), "\n")
}

func (m *Model) stripBox(p *board.Prompt, selected bool, height int) string {
	text := stripBox - 5
	head := p.ID + " · " + p.Time.Local().Format("15:04")
	if p.Queued {
		head += " · queued"
	}
	body := prefix(head, wrap(p.Text, text-2), "  ")
	if n := height - 2; len(body) > n {
		body = body[:max(n, 0)]
		if n > 0 {
			body[n-1] = "  …"
		}
	}

	sty, border := m.rowStyle(row{kind: rowPrompt, prompt: p}, false), colFaint
	if selected {
		sty, border = styGreen, colGreen
	}
	for i, l := range body {
		body[i] = sty.Render(truncate(l, text))
	}
	if m.promptStarred(p) {
		body[0] = sty.Render(truncate(head, text-2)) + " " + styGold.Render("★")
	}
	// Every box is padded to the same line count by hand: lipgloss's own Height
	// does not pad the short ones, and boxes of different heights in one strip
	// read as a broken layout.
	body = clip(body, max(height-2, 0))
	return boxIn(border).Width(stripBox - 2).Render(strings.Join(body, "\n"))
}

// promptStarred says a request or one of its parts is a favorite, so the strip
// can show that there is something kept inside.
func (m *Model) promptStarred(p *board.Prompt) bool {
	if m.starred(row{kind: rowPrompt, prompt: p}) {
		return true
	}
	for _, it := range p.Items {
		if m.starred(row{kind: rowItem, prompt: p, item: it}) {
			return true
		}
	}
	return false
}

// promptRows are the indices of the request rows, in display order.
func (m *Model) promptRows() []int {
	var out []int
	for i, r := range m.rows {
		if r.kind == rowPrompt {
			out = append(out, i)
		}
	}
	return out
}

// promptRow is the request row that owns the selection.
func (m *Model) promptRow() int {
	i := m.idx
	for i > 0 && m.rows[i].kind != rowPrompt {
		i--
	}
	return i
}

// boxRows are the rows shown as boxes under the selected request: its parts and
// its agents, minus agents and requests that will never have an answer to
// show. A numbered part keeps its box even once the turn ended it unanswered —
// the open question is worth seeing. A request with no numbered parts is a box
// itself — its whole text is the question, its whole reply the answer.
func (m *Model) boxRows() []int {
	if len(m.rows) == 0 {
		return nil
	}
	start := m.promptRow()
	var out []int
	if len(m.rows[start].prompt.Items) == 0 {
		out = append(out, start)
	}
	for i := start + 1; i < len(m.rows) && m.rows[i].kind != rowPrompt; i++ {
		out = append(out, i)
	}
	kept := out[:0]
	for _, idx := range out {
		if _, _, state := m.boxContent(m.rows[idx]); state != never || m.rows[idx].kind == rowItem {
			kept = append(kept, idx)
		}
	}
	return kept
}

// stack renders the boxes under the selected request as one scrollable page,
// then nudges the scroll so the selected box is on it.
func (m *Model) stack(width, height int) []string {
	var lines []string
	selStart, selEnd, selOpen := -1, -1, false
	for _, idx := range m.boxRows() {
		r := m.rows[idx]
		open := m.open[rowID(r)]
		box := m.box(r, width, idx == m.idx, open)
		if idx == m.idx {
			selStart, selEnd, selOpen = len(lines), len(lines)+len(box), open
		}
		lines = append(lines, box...)
	}
	if len(lines) == 0 {
		return nil
	}

	if selStart >= 0 {
		switch {
		case !selOpen && selStart < m.scroll:
			m.scroll = selStart
		case !selOpen && selEnd > m.scroll+height:
			m.scroll = selEnd - height
		// An open box may be taller than the window and is read by scrolling
		// through it: a viewport that has moved on to it from above jumps to its
		// top, and scrolling stops once its last line is the first on screen.
		case selOpen && selStart >= m.scroll+height:
			m.scroll = selStart
		case selOpen && m.scroll > selEnd-1:
			m.scroll = selEnd - 1
		}
	}
	if m.scroll >= len(lines) {
		m.scroll = max(0, len(lines)-1)
	}
	return lines[m.scroll:]
}

// box is one row as a full-width box: the question as its header, the answer
// beneath. Closed it is always four lines — header, one line of answer, two of
// border — so the stack stays a stack; open it is as long as the answer.
func (m *Model) box(r row, width int, selected, open bool) []string {
	// The box is width-2 wide including its border and padding.
	inner := max(width-5, 8)
	head, answer, state := m.boxContent(r)

	// Marks sit at the end of the header line: a gold star for a favorite, a
	// green check for a box that has been worked through.
	marks := ""
	if m.starred(r) {
		marks += " " + styGold.Render("★")
	}
	if m.checked(r) {
		marks += " " + styGreen.Render("✓")
	}
	room := inner - lipgloss.Width(marks)

	var body []string
	if open {
		body = wrap(head, room)
	} else {
		body = []string{truncate(strings.Join(strings.Fields(head), " "), room)}
	}
	sty := styHead
	if selected {
		sty = styPink
	}
	for i, l := range body {
		body[i] = sty.Render(l)
	}
	body[0] += marks

	switch state {
	case ready:
		// The answer keeps Claude's own marker, so a reply that says "see A#3"
		// can be followed by eye. The label costs width, not a line. An answer
		// that opens with its own marker — a sub-marker "A#1.1 …" or a heading
		// "## A#2 · …" — is already labelled.
		label := ""
		if r.kind == rowItem && !strings.HasPrefix(strings.TrimLeft(answer, "#*_ \t"), "A#") {
			label = fmt.Sprintf("A#%d ", r.item.Num)
		}
		md := m.markdown(answer, inner-len(label))
		if !open {
			md = firstText(md)
		}
		pad := strings.Repeat(" ", len(label))
		for i, l := range md {
			if i == 0 {
				md[i] = styMeta.Render(label) + l
			} else {
				md[i] = pad + l
			}
		}
		body = append(body, md...)
	case waiting:
		body = append(body, styOpen.Render(spinner[m.frame%len(spinner)]+" waiting for the answer…"))
	default:
		// The turn ended without an answer carrying this part's marker. The box
		// stays — the open question is the point — but the reply is not dumped
		// in as a guess.
		body = append(body, styMeta.Render("no marked answer"))
	}

	border := colFaint
	if selected {
		border = colPink
	}
	return strings.Split(boxIn(border).Width(width-2).Render(strings.Join(body, "\n")), "\n")
}

// answerState says whether a box has its answer yet: ready to show, still
// waiting for Claude, or never coming — the turn is over and nothing was marked
// for it, so there is no box.
type answerState int

const (
	waiting answerState = iota
	ready
	never
)

// boxContent is what a row's box shows: its header, its answer, and whether
// that answer is there yet.
func (m *Model) boxContent(r row) (head, answer string, state answerState) {
	over := func(has bool) answerState {
		switch {
		case has:
			return ready
		case r.prompt.Done:
			return never
		}
		return waiting
	}
	switch r.kind {
	case rowItem:
		// The user's own number, not the board's id: it is what the reply's
		// A#n answers and what the user typed. An orphan has no question to
		// show, so its header carries Claude's marker instead of the user's.
		head = fmt.Sprintf("#%d · %s", r.item.Num, r.item.Text)
		if r.item.Orphan {
			head = fmt.Sprintf("A#%d · (answer without a question)", r.item.Num)
		}
		answer, state = r.item.Answer, over(r.item.Answer != "" && r.item.Final)
	case rowPrompt:
		head = r.prompt.ID + " · " + r.prompt.Text
		answer, state = r.prompt.Reply, over(r.prompt.Reply != "" && r.prompt.Done)
	default:
		what := "running"
		if r.agent.Done {
			what = "done"
		}
		head = fmt.Sprintf("%s · agent %s · %s — %s", r.agent.ID, what, r.agent.Type, r.agent.Description)
		answer = r.agent.Result
		switch {
		case r.agent.Done && r.agent.Result != "":
			state = ready
		case r.agent.Done:
			state = never
		}
	}
	return
}

// firstText is the first rendered line that says something, as a one-line slice.
func firstText(lines []string) []string {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return []string{l}
		}
	}
	return nil
}

func rowID(r row) string {
	switch r.kind {
	case rowPrompt:
		return r.prompt.ID
	case rowItem:
		return r.item.ID
	default:
		return r.agent.ID
	}
}

// movePrompt walks the strip. Requests are the unit here; the boxes underneath
// are stepped through with up and down.
func (m *Model) movePrompt(d int) {
	prompts := m.promptRows()
	if len(prompts) == 0 {
		return
	}
	sel := m.promptRow()
	cur := 0
	for i, idx := range prompts {
		if idx == sel {
			cur = i
		}
	}
	next := min(max(cur+d, 0), len(prompts)-1)
	m.idx, m.scroll = prompts[next], 0
}

// moveBox steps the selection through the boxes under the request — or, when
// the selected box is open, scrolls it: reading it is then what up and down
// mean. Up from the first box lands on the request itself, with nothing
// underneath selected.
func (m *Model) moveBox(d int) {
	boxes := m.boxRows()
	if len(boxes) == 0 {
		return
	}
	if m.open[rowID(m.rows[m.idx])] {
		m.scrollBy(d)
		return
	}
	cur := -1
	for i, idx := range boxes {
		if idx == m.idx {
			cur = i
		}
	}
	next := cur + d
	switch {
	case next < 0:
		m.idx = m.promptRow()
	case next < len(boxes):
		m.idx = boxes[next]
	}
}

// toggleBox opens the selected box or closes it again.
func (m *Model) toggleBox() {
	for _, idx := range m.boxRows() {
		if idx == m.idx {
			id := rowID(m.rows[idx])
			if m.open[id] {
				delete(m.open, id)
			} else {
				m.open[id] = true
			}
		}
	}
}

// toggleAll opens every box under the request, or closes them all if none is
// still closed.
func (m *Model) toggleAll() {
	boxes := m.boxRows()
	all := len(boxes) > 0
	for _, idx := range boxes {
		if !m.open[rowID(m.rows[idx])] {
			all = false
		}
	}
	for _, idx := range boxes {
		id := rowID(m.rows[idx])
		if all {
			delete(m.open, id)
		} else {
			m.open[id] = true
		}
	}
}

// snapToPrompt moves the selection off a part or an agent and onto the request
// that owns it, so rotating always starts at the strip.
func (m *Model) snapToPrompt() {
	m.idx = m.promptRow()
}
