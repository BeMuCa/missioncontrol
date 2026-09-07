package tui

import (
	"strings"

	"github.com/BeMuCa/missioncontrol/core/favorite"
)

// The favorites screen shows the saved question/answer pairs across every
// session, so a pair survives both /clear and the eventual sweep of old
// transcripts. The list is on the left, the selected pair's answer on the right,
// the same shape as the main board.

func (m *Model) favList() []favorite.Favorite {
	if m.favs == nil {
		return nil
	}
	// Newest first, to match the board.
	src := m.favs.List()
	out := make([]favorite.Favorite, len(src))
	for i, f := range src {
		out[len(src)-1-i] = f
	}
	return out
}

func (m *Model) renderFavorites(width, height int) string {
	favs := m.favList()
	head := styHead.Render("★ favorites") + styMeta.Render(" · saved question/answer pairs, kept across sessions")
	if len(favs) == 0 {
		empty := clip([]string{"", styMeta.Render("  none yet — press f on a request or a numbered part to keep it")}, height-2)
		return head + "\n" + strings.Join(empty, "\n") + "\n" + m.footer(width)
	}

	if m.favIdx >= len(favs) {
		m.favIdx = len(favs) - 1
	}
	if m.favIdx < 0 {
		m.favIdx = 0
	}

	leftW := min(max(width*2/5, 30), 56)
	rightW := width - leftW - 3
	bodyH := max(height-3, 1)

	left := clip(m.favLines(favs, leftW, bodyH), bodyH)
	lines := m.markdown(favBody(favs[m.favIdx]), rightW)
	if m.scroll >= len(lines) {
		m.scroll = max(0, len(lines)-1)
	}
	right := clip(lines[m.scroll:], bodyH)
	return head + "\n" + splitBody(leftW, rightW, bodyH, left, right) + "\n" + m.footer(width)
}

// favLines draws the saved pairs as closed boxes, the same shape as the parts
// under a request: the question as header, one line of the answer, a gold star.
// The window scrolls so the selected box is always on screen.
func (m *Model) favLines(favs []favorite.Favorite, width, height int) []string {
	const boxH = 4
	perScreen := max(height/boxH, 1)
	first := max(m.favIdx-perScreen+1, 0)
	var out []string
	for i := first; i < len(favs) && i < first+perScreen; i++ {
		f := favs[i]
		inner := max(width-5, 8)
		star := " " + styGold.Render("★")
		head := truncate(strings.Join(strings.Fields(f.Session+" · "+f.Question), " "), inner-2)
		sty, border := styHead, colFaint
		if i == m.favIdx {
			sty, border = styPink, colPink
		}
		body := []string{sty.Render(head) + star}
		body = append(body, firstText(m.markdown(f.Answer, inner))...)
		out = append(out, strings.Split(boxIn(border).Width(width-2).Render(strings.Join(body, "\n")), "\n")...)
	}
	return out
}

func favBody(f favorite.Favorite) string {
	return "**Q · " + f.Session + "**\n\n" + f.Question + "\n\n---\n\n" + f.Answer
}

// renderSessions is the picker: every session transcript in this directory,
// newest first, so a session read after /clear — or one already ended — can be
// opened without leaving the board.
func (m *Model) renderSessions(width, height int) string {
	head := styHead.Render("sessions") + styMeta.Render(" · every conversation in this directory, newest first · enter opens · esc cancels")
	if len(m.sessions) == 0 {
		return head + "\n\n" + styMeta.Render("  none found") + "\n" + m.footer(width)
	}
	bodyH := max(height-3, 1)
	first := 0
	if m.sessIdx >= bodyH {
		first = m.sessIdx - bodyH + 1
	}
	var lines []string
	for i := first; i < len(m.sessions) && len(lines) < bodyH; i++ {
		s := m.sessions[i]
		title := s.Title
		if title == "" {
			title = "(no prompt)"
		}
		mark := "  "
		if s.ID == m.session {
			mark = "◆ " // the one currently shown
		}
		text := mark + title
		if i == m.sessIdx {
			lines = append(lines, stySelected.Render(truncate("▸ "+text, width)))
		} else {
			lines = append(lines, truncate("  "+text, width))
		}
	}
	return head + "\n" + strings.Join(clip(lines, bodyH), "\n") + "\n" + m.footer(width)
}
