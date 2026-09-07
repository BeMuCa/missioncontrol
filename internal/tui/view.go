package tui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

var (
	colDim    = lipgloss.Color("244")
	colFaint  = lipgloss.Color("240")
	colAccent = lipgloss.Color("39")
	colOpen   = lipgloss.Color("214")
	colErr    = lipgloss.Color("203")

	styMeta     = lipgloss.NewStyle().Foreground(colDim)
	styFaint    = lipgloss.NewStyle().Foreground(colFaint)
	stySelected = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	styHead     = lipgloss.NewStyle().Bold(true)
	styFaintSel = lipgloss.NewStyle().Foreground(colFaint).Bold(true)
	styOpen     = lipgloss.NewStyle().Foreground(colOpen)
	styErr      = lipgloss.NewStyle().Foreground(colErr)
)

// View satisfies tea.Model.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render(m.width, m.height))
	v.AltScreen = true
	v.WindowTitle = "missioncontrol"
	return v
}

// Snapshot renders the board once at a fixed size, for a terminal that is not
// one. sel preselects a row by its id ("P3", "P3.2", "A7") so a single answer
// can be read, or piped, without a keyboard.
func (m *Model) Snapshot(sel string) string {
	m.reload()
	if sel != "" && !m.selectID(sel) {
		return "no row with id " + sel + "\n"
	}
	return m.render(120, 40) + "\n"
}

// selectID moves the cursor to the row carrying id.
func (m *Model) selectID(id string) bool {
	for i, r := range m.rows {
		if rowID(r) == id {
			m.idx, m.scroll = i, 0
			return true
		}
	}
	return false
}

func (m *Model) render(width, height int) string {
	if width == 0 || height == 0 {
		return "loading…"
	}
	if m.board == nil {
		return "no transcript read yet"
	}
	if m.showSessions {
		return m.renderSessions(width, height)
	}
	if m.showFavs {
		return m.renderFavorites(width, height)
	}
	if m.stacked {
		return m.renderStacked(width, height)
	}

	leftW := min(max(width*2/5, 30), 56)
	rightW := width - leftW - 3
	bodyH := max(height-3, 1)

	left := clip(m.list(leftW, bodyH), bodyH)
	right := clip(m.detail(rightW), bodyH)
	return m.header(width) + "\n" + splitBody(leftW, rightW, bodyH, left, right) + "\n" + m.footer(width)
}

func (m *Model) header(width int) string {
	agents := 0
	for _, p := range m.board.Prompts {
		agents += len(p.Agents)
	}
	open := m.board.Waiting()

	name := m.session
	if name == "" {
		name = "session"
	}
	rest := fmt.Sprintf(" · %s · %d prompts · %d agents", name, len(m.board.Prompts), agents)
	openPart := ""
	if open > 0 {
		openPart = fmt.Sprintf(" · %d agents open", open)
	}
	if q := m.board.Pending(); q > 0 {
		openPart += fmt.Sprintf(" · %d queued", q)
	}
	errPart := ""
	if m.err != "" {
		errPart = " · " + m.err
	}

	// Styling is applied per part, so the plain text is what gets measured; a
	// header cut mid-escape-sequence would corrupt the rest of the line.
	plain := "missioncontrol" + rest + openPart + errPart
	if lipgloss.Width(plain) > width {
		return styMeta.Render(truncate(plain, width))
	}
	return styHead.Render("missioncontrol") + styMeta.Render(rest) + styOpen.Render(openPart) + styErr.Render(errPart)
}

func (m *Model) footer(width int) string {
	pane := "list"
	if m.onText {
		pane = "text"
	}
	hints := "←→ pane (" + pane + ") · ↑↓ move · t rotate · a hide agents · s sessions · r refresh · q quit"
	if m.stacked {
		hints = "←→ request · ↑↓ part · ⏎ open/close · m all · d done · t rotate · a agents · s sessions · r refresh · q quit"
	}
	if m.confirmDel {
		return styErr.Render(truncate("remove this favorite? y = yes · any other key = keep", width))
	}
	if m.hideAgents {
		hints = "agents hidden · " + hints
	}
	if m.showSessions {
		return styMeta.Render("↑↓ move · enter open · esc cancel")
	}
	if m.showFavs {
		hints = "★ favorites · ↑↓ move · ⏎ read answer · f remove · F back · q quit"
		if m.favOnText {
			hints = "★ favorites · ↑↓ scroll · ⏎/esc back to list · q quit"
		}
	} else {
		hints += " · f star · F favorites"
	}
	// Prepended, not appended: the hint chain is already longer than an ordinary
	// terminal and gets truncated, and the way back to the launcher is the one
	// key a first-time user has no way to guess.
	if m.allowBack {
		hints = "esc projects · " + hints
	}
	// A footer wider than the window wraps onto a second row and pushes the
	// board off the top.
	return styMeta.Render(truncate(hints, width))
}

// blockCap is how many lines an unselected request contributes. Without it one
// long request fills the pane and the list stops being a list; the selected one
// is always shown whole, which is where reading happens.
const blockCap = 6

func (m *Model) list(width, height int) []string {
	if len(m.rows) == 0 {
		return []string{styMeta.Render("nothing typed yet")}
	}
	// A box is drawn per request, around the request and everything it spawned,
	// so the eye can tell where one ends without counting indents. Text is
	// pre-wrapped three columns narrower than the box declares, or lipgloss wraps
	// it a second time against border and padding and the indentation goes
	// ragged.
	inner := width - 4
	text := inner - 3

	var lines []string
	selStart, selEnd := 0, 0
	for i := 0; i < len(m.rows); {
		start := i
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		for i++; i < len(m.rows) && m.rows[i].kind != rowPrompt; i++ {
		}

		var body []string
		selIn := false
		selOff, selLen := 0, 0
		for j := start; j < i; j++ {
			r := m.rows[j]
			block := blockLines(r, text, j == m.idx, m.starred(r))
			if j == m.idx {
				selIn, selOff, selLen = true, len(body), len(block)
			}
			sty := m.rowStyle(r, j == m.idx)
			for _, l := range block {
				body = append(body, sty.Render(truncate(l, text)))
			}
		}

		boxed := strings.Split(boxStyle(selIn).Width(inner).Render(strings.Join(body, "\n")), "\n")
		if selIn {
			selStart = len(lines) + 1 + selOff
			selEnd = selStart + selLen
		}
		lines = append(lines, boxed...)
	}

	// Page by line so a tall box stays whole on screen.
	first := 0
	if selEnd > height {
		first = selEnd - height
	}
	if first > selStart {
		first = selStart
	}
	return lines[first:]
}

// splitBody lays a left and right column side by side with a faint gutter, the
// two-pane shape both the board and the favorites screen share.
func splitBody(leftW, rightW, height int, left, right []string) string {
	gutter := make([]string, height)
	for i := range gutter {
		gutter[i] = styFaint.Render(" │ ")
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(leftW).Render(strings.Join(left, "\n")),
		strings.Join(gutter, "\n"),
		lipgloss.NewStyle().Width(rightW).Render(strings.Join(right, "\n")),
	)
}

func boxStyle(selected bool) lipgloss.Style {
	c := colFaint
	if selected {
		c = colAccent
	}
	return boxIn(c)
}

func boxIn(c color.Color) lipgloss.Style {
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(c).PaddingLeft(1)
}

// rowStyle dims the selection while the text pane has focus, so it stays
// findable without competing with the pane being read.
func (m *Model) rowStyle(r row, selected bool) lipgloss.Style {
	switch {
	case selected && m.onText:
		return styFaintSel
	case selected:
		return stySelected
	case r.kind == rowAgent && !r.agent.Done:
		return styOpen
	case r.kind == rowAgent:
		return styMeta
	case r.kind == rowPrompt && r.prompt.Pending:
		return styOpen
	}
	return lipgloss.NewStyle()
}

// blockLines renders one row as a wrapped block: a heading that names it and the
// text underneath, so a request can be read on the left instead of guessed at
// from a cut-off line.
func blockLines(r row, width int, full, starred bool) []string {
	indent := strings.Repeat("  ", r.depth)
	body := indent + "  "
	avail := max(width-lipgloss.Width(body), 8)

	switch r.kind {
	case rowPrompt:
		head := r.prompt.ID + " · " + r.prompt.Time.Local().Format("15:04")
		if r.prompt.Queued {
			head += " · queued"
			if r.prompt.Pending {
				head += " · waiting"
			}
		}
		if starred {
			head += " ★"
		}
		text := wrap(r.prompt.Text, avail)
		if !full && len(text) > blockCap {
			text = append(text[:blockCap:blockCap], "…")
		}
		return prefix(head, text, body)

	case rowItem:
		head := indent + r.item.ID
		if r.item.Answer != "" {
			head += " · answered"
		}
		if starred {
			head += " ★"
		}
		return prefix(head, wrap(r.item.Text, avail), body)

	default:
		state := "running"
		if r.agent.Done {
			state = "done"
		}
		// An agent line stays on one line where it fits: a session can hold
		// dozens, and a two-line block each buries the requests between them.
		lines := wrap(fmt.Sprintf("%s⤷ agent %s · %s · %s", indent, r.agent.ID, state, r.agent.Description), width)
		for i := 1; i < len(lines); i++ {
			lines[i] = body + lines[i]
		}
		return lines
	}
}

func prefix(head string, text []string, indent string) []string {
	out := make([]string, 0, len(text)+1)
	out = append(out, head)
	for _, l := range text {
		out = append(out, indent+l)
	}
	return out
}

// detail is the answer pane. It deliberately does not repeat the question: that
// is what the left column shows in full, and repeating it pushed the answer off
// the bottom. The body is rendered as markdown, since that is what Claude
// writes — tables, headings and code fences included.
func (m *Model) detail(width int) []string {
	if len(m.rows) == 0 {
		return nil
	}
	r := m.rows[m.idx]
	var head, body string
	switch r.kind {
	case rowPrompt:
		head = r.prompt.ID + " · reply"
		if r.prompt.Borrowed {
			head += " (of the turn this landed in)"
		}
		switch {
		case r.prompt.Reply != "":
			body = r.prompt.Reply
		case r.prompt.Pending:
			body = "*still in the queue — not picked up yet*"
		default:
			body = "*no reply yet*"
		}

	case rowItem:
		switch {
		case r.item.Answer != "":
			head, body = r.item.ID+" · answer", r.item.Answer
		case r.prompt.Reply != "":
			head, body = r.item.ID+" · answer not marked — whole turn follows", r.prompt.Reply
		default:
			head, body = r.item.ID+" · answer", "*no reply yet*"
		}
		if len(r.item.Agents) > 0 {
			body += "\n\n**agents (inferred)**\n"
			for _, a := range r.item.Agents {
				body += "\n- " + a.ID + " " + a.Description
			}
		}

	default:
		a := r.agent
		state := "running"
		if a.Done {
			state = "done"
		}
		head = a.ID + " · " + a.Type
		body = fmt.Sprintf("%s · %s · %s · started %s", state, a.Model, a.AgentID, a.Started.Local().Format("15:04:05"))
		if a.Depth > 1 {
			body += fmt.Sprintf(" · spawned by another agent (depth %d)", a.Depth)
		}
		body += "\n\n" + a.Description
		if a.Result != "" {
			body += "\n\n---\n\n" + a.Result
		}
	}

	lines := append([]string{styHead.Render(truncate(head, width))}, m.markdown(body, width)...)
	if m.scroll >= len(lines) {
		m.scroll = max(0, len(lines)-1)
	}
	return lines[m.scroll:]
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func wrap(s string, width int) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if lipgloss.Width(line) <= width {
			out = append(out, line)
			continue
		}
		cur := ""
		for _, w := range strings.Fields(line) {
			switch {
			case cur == "":
				cur = w
			case lipgloss.Width(cur+" "+w) <= width:
				cur += " " + w
			default:
				out = append(out, cur)
				cur = w
			}
		}
		out = append(out, cur)
	}
	return out
}

func clip(lines []string, height int) []string {
	if len(lines) > height {
		return lines[:height]
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return lines
}

func truncate(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)+"…") > width {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}
