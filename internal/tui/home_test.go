package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/BeMuCa/missioncontrol/core/project"
)

// fakeHome builds a home directory holding both halves the launcher reads: the
// registry it lists from, and the ~/.claude/projects transcripts it counts.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("MISSIONCONTROL_HOME", filepath.Join(home, project.DirName))
	return home
}

// addProject registers root and gives it n transcripts, the way a real project
// with sessions looks on disk.
func addProject(t *testing.T, home, root string, n int) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Remember(root); err != nil {
		t.Fatal(err)
	}
	slug := strings.ReplaceAll(project.Canonical(root), string(filepath.Separator), "-")
	dir := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		p := filepath.Join(dir, "sess"+string(rune('a'+i))+".jsonl")
		if err := os.WriteFile(p, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// pressHome sends one key to the launcher. stacked_test.go already has a press
// for the board; this one takes a key code, since enter has no text form.
func pressHome(h *Home, code rune) {
	h.Update(tea.KeyPressMsg{Code: code})
}

func TestHomeListsRegisteredProjectsWithSessionCounts(t *testing.T) {
	home := fakeHome(t)
	root := filepath.Join(home, "work", "alpha")
	addProject(t, home, root, 3)

	h := NewHome(home, home)
	if len(h.entries) != 1 {
		t.Fatalf("%d entries, want 1", len(h.entries))
	}
	if got := h.entries[0]; got.Name != "alpha" || got.Sessions != 3 {
		t.Errorf("entry = %+v, want alpha with 3 sessions", got)
	}
}

func TestHomePreselectsTheDirectoryItWasRunIn(t *testing.T) {
	home := fakeHome(t)
	first := filepath.Join(home, "work", "alpha")
	second := filepath.Join(home, "work", "beta")
	addProject(t, home, first, 1)
	addProject(t, home, second, 1)

	h := NewHome(home, second)
	if h.entries[h.idx].Root != project.Canonical(second) {
		t.Errorf("selected %s, want the directory it was run in", h.entries[h.idx].Root)
	}
}

func TestHomeEnterChoosesTheProject(t *testing.T) {
	home := fakeHome(t)
	root := filepath.Join(home, "work", "alpha")
	addProject(t, home, root, 1)

	h := NewHome(home, home)
	pressHome(h, tea.KeyEnter)
	if h.Chosen != project.Canonical(root) {
		t.Fatalf("Chosen = %q, want %q", h.Chosen, project.Canonical(root))
	}
	if h.Quit {
		t.Error("picking a project must not look like a quit")
	}
}

func TestHomeRefusesAProjectWithNoSessions(t *testing.T) {
	home := fakeHome(t)
	root := filepath.Join(home, "work", "empty")
	addProject(t, home, root, 0)

	h := NewHome(home, home)
	pressHome(h, tea.KeyEnter)
	if h.Chosen != "" {
		t.Errorf("Chosen = %q, want the launcher to stay put", h.Chosen)
	}
	if h.msg == "" {
		t.Error("the launcher must say why nothing opened")
	}
}

func TestHomeQuitIsNotAChoice(t *testing.T) {
	home := fakeHome(t)
	addProject(t, home, filepath.Join(home, "work", "alpha"), 1)

	h := NewHome(home, home)
	pressHome(h, 'q')
	if !h.Quit || h.Chosen != "" {
		t.Errorf("Quit=%v Chosen=%q, want a quit with no choice", h.Quit, h.Chosen)
	}
}

func TestHomeHintsWhenTheCurrentDirectoryIsUnregistered(t *testing.T) {
	home := fakeHome(t)
	// Sessions on disk, but never registered: the case where the board the user
	// wants is right here and not on the list.
	unregistered := filepath.Join(home, "work", "stray")
	addProject(t, home, unregistered, 2)
	if _, err := project.Forget(unregistered); err != nil {
		t.Fatal(err)
	}

	h := NewHome(home, unregistered)
	if h.hint == "" {
		t.Fatal("want a hint that this directory is not registered")
	}
	if !strings.Contains(h.hint, "missioncontrol init") {
		t.Errorf("hint = %q, want it to name the command that fixes it", h.hint)
	}
}

func TestHomeRendersLogoWordmarkAndProjects(t *testing.T) {
	home := fakeHome(t)
	addProject(t, home, filepath.Join(home, "work", "alpha"), 2)

	out := NewHome(home, home).render(100, 40)
	for _, want := range []string{"Projects:", "alpha", "2 sessions"} {
		if !strings.Contains(out, want) {
			t.Errorf("render is missing %q", want)
		}
	}
	if !strings.Contains(out, "▀") && !strings.Contains(out, "▄") {
		t.Error("render is missing the logo")
	}
	if !strings.Contains(out, "█") {
		t.Error("render is missing the wordmark")
	}
}

// Every project row has to be the same width, or centring them gives the list a
// ragged left edge and the columns stop lining up.
func TestHomeProjectRowsAreAllTheSameWidth(t *testing.T) {
	home := fakeHome(t)
	addProject(t, home, filepath.Join(home, "work", "a-very-long-project-name"), 3)
	addProject(t, home, filepath.Join(home, "b"), 1)

	h := NewHome(home, home)
	first := lipgloss.Width(h.row(h.entries[0], true, 60))
	for i, e := range h.entries {
		if got := lipgloss.Width(h.row(e, false, 60)); got != first {
			t.Errorf("row %d is %d columns, want %d like the rest", i, got, first)
		}
	}
	if first != 60 {
		t.Errorf("rows are %d columns, want the full row width of 60", first)
	}
}
