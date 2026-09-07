// Package tui renders the board: prompts and their agents on the left, the
// selected one's text on the right.
//
// The board only reads. It polls the transcript instead of watching it, because
// a poll needs no assumption about how the writer flushes and a second of lag
// is invisible on work that takes minutes.
package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/BeMuCa/missioncontrol/core/board"
	"github.com/BeMuCa/missioncontrol/core/done"
	"github.com/BeMuCa/missioncontrol/core/favorite"
	"github.com/BeMuCa/missioncontrol/core/session"
	"github.com/BeMuCa/missioncontrol/core/transcript"
)

type rowKind int

const (
	rowPrompt rowKind = iota
	rowItem
	rowAgent
)

type row struct {
	kind   rowKind
	depth  int
	prompt *board.Prompt
	item   *board.Item
	agent  *board.Agent
}

var _ tea.Model = (*Model)(nil)

// Model is the board's state.
type Model struct {
	reader  *transcript.Reader
	subDir  string
	session string

	entries []transcript.Entry
	board   *board.Board
	rows    []row

	idx    int
	scroll int // first visible line of the detail pane
	// hideAgents drops the agent rows, for reading back what was asked without
	// the spawned work in between.
	hideAgents bool
	// onText moves the arrow keys from the list to the text beside it, so a long
	// reply can be read without leaving the row it belongs to.
	onText bool
	// stacked turns the board a quarter turn: requests across the top, the
	// selected one opened up underneath as boxes. open holds the boxes unfolded
	// to their whole answer, by row id; frame drives the waiting spinner.
	stacked bool
	open    map[string]bool
	frame   int
	// favs holds the saved pairs; showFavs swaps the board for that list. now is
	// injected so the model stays testable without a real clock.
	favs     *favorite.Store
	showFavs bool
	favIdx   int
	// favOnText moves the arrow keys from the favorites list to the answer
	// beside it, so a long saved answer can be scrolled without leaving it.
	favOnText bool
	now       func() string
	// confirmDel is set while the favorites screen waits for a y to remove the
	// selected pair. done holds the read-through marks.
	confirmDel bool
	done       *done.Set

	// home and cwd let the session picker relist and switch without going back
	// to main. sessions is the picker's contents; showSessions swaps it in.
	home, cwd    string
	sessions     []session.Local
	showSessions bool
	sessIdx      int

	width, height int
	err           string
}

// New builds a model over one session's transcript. home and cwd let it switch
// to another session in the same directory from inside the board.
func New(transcriptPath, subDir, sessionName, home, cwd string, favs *favorite.Store, marks *done.Set, now func() string) *Model {
	return &Model{
		reader:  transcript.NewReader(transcriptPath),
		subDir:  subDir,
		session: sessionName,
		home:    home,
		cwd:     cwd,
		favs:    favs,
		done:    marks,
		now:     now,
	}
}

// switchTo points the board at another session's transcript and reloads from
// scratch. The byte offset, parsed entries and board all reset, since none of
// them mean anything against a different file.
func (m *Model) switchTo(loc session.Local) {
	m.reader = transcript.NewReader(loc.Path)
	m.subDir = session.SubagentsDir(loc.Path)
	m.session = loc.ID
	m.entries = nil
	m.board = nil
	m.idx, m.scroll = 0, 0
	m.reload()
}

// refresh rereads the transcript from the start instead of from the last poll.
// The offset-based poll only sees appended lines, so a file that was rewritten
// or a directory that gained agents after the last poll shows up only this way.
func (m *Model) refresh() {
	m.reader.Reset()
	m.entries = nil
	m.reload()
}

// markdown renders text for the answer pane. It is a thin seam over the local
// renderer, kept so callers read the same as before.
func (m *Model) markdown(text string, width int) []string {
	return renderMarkdown(text, width)
}

// SetStacked chooses the layout the board opens in.
func (m *Model) SetStacked(v bool) { m.stacked = v }

// openSessions loads the sessions in this directory and shows the picker.
func (m *Model) openSessions() {
	ss, err := session.ListLocal(m.home, m.cwd)
	if err != nil {
		m.err = err.Error()
		return
	}
	m.sessions = ss
	m.sessIdx = 0
	for i, s := range ss {
		if s.ID == m.session {
			m.sessIdx = i
		}
	}
	m.showSessions = true
}

type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// Init satisfies tea.Model.
func (m *Model) Init() tea.Cmd {
	m.reload()
	return tick()
}

// Update satisfies tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		m.frame++
		m.reload()
		return m, tick()
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

func (m *Model) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.confirmDel {
		m.confirmDel = false
		if k.String() == "y" {
			m.removeFavorite()
		}
		return m, nil
	}
	switch k.String() {
	case "esc":
		if m.showSessions {
			m.showSessions = false
			return m, nil
		}
		if m.showFavs && m.favOnText {
			m.favOnText = false
			return m, nil
		}
		return m, tea.Quit
	case "q", "ctrl+c":
		return m, tea.Quit
	case "t":
		m.stacked = !m.stacked
		m.scroll = 0
		if m.stacked && len(m.rows) > 0 {
			m.snapToPrompt()
		}
	case "right", "l":
		if m.stacked {
			m.movePrompt(1)
		} else {
			m.onText = true
		}
	case "left", "h":
		if m.stacked {
			m.movePrompt(-1)
		} else {
			m.onText = false
		}
	case "up", "k":
		switch {
		case m.showSessions:
			m.sessIdx = max(m.sessIdx-1, 0)
		case m.showFavs && m.favOnText:
			m.scrollBy(-1)
		case m.showFavs:
			m.favIdx = max(m.favIdx-1, 0)
			m.scroll = 0
		case m.stacked:
			m.moveBox(-1)
		case m.onText:
			m.scrollBy(-1)
		default:
			m.move(-1)
		}
	case "down", "j":
		switch {
		case m.showSessions:
			if m.sessIdx < len(m.sessions)-1 {
				m.sessIdx++
			}
		case m.showFavs && m.favOnText:
			m.scrollBy(1)
		case m.showFavs:
			m.favIdx++
			m.scroll = 0
		case m.stacked:
			m.moveBox(1)
		case m.onText:
			m.scrollBy(1)
		default:
			m.move(1)
		}
	case "g":
		if m.onText {
			m.scroll = 0
		} else {
			m.idx, m.scroll = 0, 0
		}
	case "G":
		if m.onText {
			// The detail pane clamps an overlong offset to its last line.
			m.scroll = 1 << 20
		} else {
			m.idx, m.scroll = max(len(m.rows)-1, 0), 0
		}
	case "ctrl+d":
		m.scrollBy(10)
	case "ctrl+u":
		m.scrollBy(-10)
	case "a":
		m.hideAgents = !m.hideAgents
		m.rebuild()
	case "s":
		m.openSessions()
	case "r":
		m.refresh()
	case "enter":
		switch {
		case m.showSessions && m.sessIdx < len(m.sessions):
			m.switchTo(m.sessions[m.sessIdx])
			m.showSessions = false
		case m.showFavs:
			// Enter hands the arrow keys to the opened answer and back, the
			// favorites-screen counterpart of ←→ on the split board.
			m.favOnText = !m.favOnText
		case m.stacked:
			m.toggleBox()
		}
	case "m":
		if m.stacked {
			m.toggleAll()
		}
	case "f":
		if m.showFavs {
			m.confirmDel = len(m.favList()) > 0
		} else {
			m.toggleFavorite()
		}
	case "d":
		m.toggleDone()
	case "F":
		m.showFavs = !m.showFavs
		m.favIdx, m.scroll = 0, 0
		m.favOnText = false
	}
	return m, nil
}

// toggleFavorite saves the selected question/answer pair, or removes it if it
// was already saved. Only rows that pair a request with a reply can be saved;
// an agent row or a bare prompt without a reply has nothing to keep.
func (m *Model) toggleFavorite() {
	if m.favs == nil || len(m.rows) == 0 {
		return
	}
	r := m.rows[m.idx]
	q, a, id := favoriteContent(r)
	if q == "" || a == "" {
		return
	}
	key := m.session + "#" + id
	saved := ""
	if m.now != nil {
		saved = m.now()
	}
	if _, err := m.favs.Toggle(favorite.Favorite{
		Key: key, Session: m.session, Question: q, Answer: a, SavedAt: saved,
	}); err != nil {
		m.err = err.Error()
	}
}

// removeFavorite drops the pair selected on the favorites screen.
func (m *Model) removeFavorite() {
	favs := m.favList()
	if m.favIdx >= len(favs) {
		return
	}
	if _, err := m.favs.Toggle(favs[m.favIdx]); err != nil {
		m.err = err.Error()
	}
}

// toggleDone marks the selected box as worked through, or clears the mark.
func (m *Model) toggleDone() {
	if m.done == nil || len(m.rows) == 0 {
		return
	}
	at := ""
	if m.now != nil {
		at = m.now()
	}
	if _, err := m.done.Toggle(m.session+"#"+rowID(m.rows[m.idx]), at); err != nil {
		m.err = err.Error()
	}
}

// checked reports whether a row carries a read-through mark.
func (m *Model) checked(r row) bool {
	return m.done != nil && m.done.Has(m.session+"#"+rowID(r))
}

// starred reports whether a row is saved, for the ★ shown beside it.
func (m *Model) starred(r row) bool {
	if m.favs == nil {
		return false
	}
	_, _, id := favoriteContent(r)
	return id != "" && m.favs.Has(m.session+"#"+id)
}

// favoriteContent pulls the question, answer and id out of a row, or returns
// empties when the row is not a savable pair.
func favoriteContent(r row) (question, answer, id string) {
	switch r.kind {
	case rowItem:
		if r.item.Answer != "" {
			return r.item.Text, r.item.Answer, r.item.ID
		}
	case rowPrompt:
		if r.prompt.Reply != "" {
			return r.prompt.Text, r.prompt.Reply, r.prompt.ID
		}
	}
	return "", "", ""
}

func (m *Model) scrollBy(d int) {
	m.scroll = max(m.scroll+d, 0)
}

func (m *Model) move(d int) {
	if len(m.rows) == 0 {
		return
	}
	m.idx += d
	if m.idx < 0 {
		m.idx = 0
	}
	if m.idx >= len(m.rows) {
		m.idx = len(m.rows) - 1
	}
	m.scroll = 0
}

// reload picks up whatever the session has written since the last poll.
func (m *Model) reload() {
	entries, err := m.reader.Read()
	if err != nil {
		m.err = err.Error()
		return
	}
	m.entries = append(m.entries, entries...)
	runs, err := transcript.LoadAgents(m.subDir)
	if err != nil {
		m.err = err.Error()
		return
	}
	m.err = ""
	m.board = board.Build(m.entries, runs)
	m.rebuild()
}

func (m *Model) rebuild() {
	if m.board == nil {
		return
	}
	if m.open == nil {
		m.open = map[string]bool{}
	}
	m.rows = nil
	// Newest first: the request you are waiting on is the one you look at, and
	// scrolling to the bottom of a long session to find it is the opposite of
	// keeping track. Ids stay chronological so they can still be referenced.
	for i := len(m.board.Prompts) - 1; i >= 0; i-- {
		p := m.board.Prompts[i]
		m.rows = append(m.rows, row{kind: rowPrompt, prompt: p})
		assigned := map[*board.Agent]bool{}
		for _, it := range p.Items {
			m.rows = append(m.rows, row{kind: rowItem, depth: 1, prompt: p, item: it})
			if m.hideAgents {
				continue
			}
			for _, a := range it.Agents {
				assigned[a] = true
				m.rows = append(m.rows, row{kind: rowAgent, depth: 2, prompt: p, item: it, agent: a})
			}
		}
		if m.hideAgents {
			continue
		}
		for _, a := range p.Agents {
			if !assigned[a] {
				m.rows = append(m.rows, row{kind: rowAgent, depth: 1, prompt: p, agent: a})
			}
		}
	}
	if m.idx >= len(m.rows) {
		m.idx = len(m.rows) - 1
	}
	if m.idx < 0 {
		m.idx = 0
	}
}
