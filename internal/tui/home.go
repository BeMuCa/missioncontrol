package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/BeMuCa/missioncontrol/core/project"
	"github.com/BeMuCa/missioncontrol/core/session"
)

var _ tea.Model = (*Home)(nil)

// HomeEntry is one registered project, with the two facts worth knowing before
// opening it: how many sessions it has and when the newest one last moved.
type HomeEntry struct {
	Root     string
	Name     string
	Sessions int
	Newest   time.Time
}

// Home is the launcher: the mark, and every project the board can be opened on.
// It is what `missioncontrol` shows when it is started outside a project, which
// is the point of the registry — the board is reachable from anywhere.
type Home struct {
	entries []HomeEntry
	idx     int
	home    string

	// Chosen is the project the user picked; the caller opens the board on it.
	// Quit is set when the user left instead of picking. Keeping them apart
	// matters: an empty Chosen after a pick would silently look like a quit.
	Chosen string
	Quit   bool

	// msg reports why the list is not what the user expected — a project with
	// no sessions is the case that otherwise looks like a broken enter key.
	msg string

	// hint says that the directory missioncontrol was run in has sessions but
	// is not on the list. That is the one confusing state the launcher has: the
	// board you wanted is right here and not shown.
	hint string

	width, height int
}

// NewHome builds the launcher over the registered projects, selecting the one
// the command was run in when it happens to be one of them.
func NewHome(home, cwd string) *Home {
	h := &Home{home: home}
	h.refresh()
	here := project.Canonical(cwd)
	for i, e := range h.entries {
		if e.Root == here {
			h.idx = i
			return h
		}
	}
	if n, _ := session.Summary(home, here); n > 0 {
		h.hint = fmt.Sprintf("%s has %d sessions but is not registered — run  missioncontrol init  in it", filepath.Base(here), n)
	}
	return h
}

func (h *Home) refresh() {
	h.entries = nil
	for _, p := range project.Load() {
		n, newest := session.Summary(h.home, p.Root)
		h.entries = append(h.entries, HomeEntry{
			Root:     project.Canonical(p.Root),
			Name:     p.Name,
			Sessions: n,
			Newest:   newest,
		})
	}
	if h.idx >= len(h.entries) {
		h.idx = max(len(h.entries)-1, 0)
	}
}

func (h *Home) Init() tea.Cmd { return nil }

func (h *Home) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width, h.height = msg.Width, msg.Height
	case tea.KeyPressMsg:
		return h.key(msg)
	}
	return h, nil
}

func (h *Home) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	h.msg = ""
	switch k.String() {
	case "q", "esc", "ctrl+c":
		h.Quit = true
		return h, tea.Quit
	case "up", "k":
		h.idx = max(h.idx-1, 0)
	case "down", "j":
		if h.idx < len(h.entries)-1 {
			h.idx++
		}
	case "g":
		h.idx = 0
	case "G":
		h.idx = max(len(h.entries)-1, 0)
	case "r":
		h.refresh()
	case "enter":
		if h.idx >= len(h.entries) {
			return h, nil
		}
		e := h.entries[h.idx]
		// Opening a project with no transcripts would drop into an empty board
		// with nothing to explain it, so the launcher says so and stays put.
		if e.Sessions == 0 {
			h.msg = e.Name + " has no sessions yet — run claude in it first"
			return h, nil
		}
		h.Chosen = e.Root
		return h, tea.Quit
	}
	return h, nil
}

// View satisfies tea.Model.
func (h *Home) View() tea.View {
	v := tea.NewView(h.render(h.width, h.height))
	v.AltScreen = true
	v.WindowTitle = "missioncontrol"
	return v
}

func (h *Home) render(width, height int) string {
	if width == 0 || height == 0 {
		return "loading…"
	}
	centre := lipgloss.NewStyle().Width(width).Align(lipgloss.Center)

	var b strings.Builder
	// The mark is the first thing dropped when the window cannot hold it: a
	// wrapped logo is worse than no logo, and the list is the working part.
	if width >= logoWidth+2 && height >= 28 {
		b.WriteString(centre.Render(logoArt()) + "\n\n")
	}
	if width >= wordmarkWidth+2 && height >= 16 {
		b.WriteString(centre.Render(wordmark()) + "\n\n")
	} else {
		b.WriteString(centre.Render(styHead.Render("missioncontrol")) + "\n\n")
	}

	if len(h.entries) == 0 {
		b.WriteString(centre.Render(styMeta.Render("no projects yet")) + "\n\n")
		b.WriteString(centre.Render(styFaint.Render("run  missioncontrol init  in a project to add it")) + "\n")
		if h.hint != "" {
			b.WriteString("\n" + centre.Render(styFaint.Render(truncate(h.hint, width))))
		}
		b.WriteString("\n" + styMeta.Render(truncate("q quit", width)))
		return b.String()
	}

	b.WriteString(centre.Render(styMeta.Render("Projects:")) + "\n\n")
	for i, e := range h.entries {
		b.WriteString(centre.Render(h.row(e, i == h.idx, min(width-4, 72))) + "\n")
	}
	if h.msg != "" {
		b.WriteString("\n" + centre.Render(styErr.Render(truncate(h.msg, width))))
	}
	if h.hint != "" {
		b.WriteString("\n" + centre.Render(styFaint.Render(truncate(h.hint, width))))
	}
	b.WriteString("\n" + styMeta.Render(truncate("↑↓ move · ⏎ open · r refresh · q quit", width)))
	return b.String()
}

// row renders one project: what it is called, what it holds, and where it is.
func (h *Home) row(e HomeEntry, selected bool, width int) string {
	marker, sty := "  ", styMeta
	if selected {
		marker, sty = "▸ ", stySelected
	}
	stat := fmt.Sprintf("%d sessions · %s", e.Sessions, ago(e.Newest))
	if e.Sessions == 0 {
		stat = "no sessions"
	}
	line := marker +
		pad(truncate(e.Name, 22), 22) + " " +
		pad(truncate(stat, 24), 24) + " " +
		tilde(h.home, e.Root)
	// Padded to the full row width before centring: rows centred at their own
	// lengths give the list a ragged left edge and the columns stop lining up.
	return sty.Render(pad(truncate(line, width), width))
}

// tilde shortens a path under the home directory, which is where projects live.
func tilde(home, p string) string {
	if home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// ago is a coarse "when did this last move". Minutes matter while a session is
// running and nothing finer does, so the scale stops at days.
func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
