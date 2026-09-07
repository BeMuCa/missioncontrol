// Command missioncontrol shows what a running Claude Code session is doing: the
// prompts you typed, the numbered items in each, and the agents each one spawned.
//
// Started inside a project it opens that project's board. Started anywhere else
// it opens the launcher, which lists the projects registered with
// `missioncontrol init` and opens the board on the one you pick.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/BeMuCa/missioncontrol/core/done"
	"github.com/BeMuCa/missioncontrol/core/favorite"
	"github.com/BeMuCa/missioncontrol/core/project"
	"github.com/BeMuCa/missioncontrol/core/session"
	"github.com/BeMuCa/missioncontrol/internal/tui"
)

func main() {
	// The subcommands are matched before the flags, so `init` does not have to
	// be spelled as a flag and cannot collide with one.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			if err := initProject(); err != nil {
				fail(err)
			}
			return
		case "forget":
			if err := forgetProject(); err != nil {
				fail(err)
			}
			return
		}
	}

	once := flag.Bool("print", false, "render the board once as text and exit")
	sel := flag.String("select", "", "with -print: preselect a row by id (P3, P3.2, A7)")
	stacked := flag.Bool("stacked", true, "start rotated: requests across the top, the selected one opened up below (-stacked=false for the split layout)")
	id := flag.String("session", "", "session id to observe (default: the interactive session in this directory)")
	here := flag.Bool("here", false, "skip the launcher and open this directory's session")
	flag.Parse()

	if err := run(*once, *id, *sel, *stacked, *here); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "missioncontrol:", err)
	os.Exit(1)
}

// initProject registers the current directory, so the launcher can reach it
// from anywhere. The registry lives in ~/.missioncontrol, beside ~/.claude,
// and holds nothing but paths: a project's sessions are found from its path.
func initProject() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	added, err := project.Remember(cwd)
	if err != nil {
		return err
	}
	root := project.Canonical(cwd)
	verb := "already registered"
	if added {
		verb = "registered"
	}
	fmt.Printf("%s %s\n", verb, root)

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	n, _ := session.Summary(home, root)
	switch n {
	case 0:
		fmt.Println("no sessions here yet — they show up once claude has run in this directory")
	case 1:
		fmt.Println("1 session found")
	default:
		fmt.Printf("%d sessions found\n", n)
	}
	fmt.Println("registry: " + filepath.Join(project.Dir(), "projects.json"))
	return nil
}

// forgetProject is init's counterpart: a project registered by mistake, or one
// that has moved, would otherwise stay on the launcher for good.
func forgetProject() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	removed, err := project.Forget(cwd)
	if err != nil {
		return err
	}
	if !removed {
		fmt.Println("not registered: " + project.Canonical(cwd))
		return nil
	}
	fmt.Println("forgot " + project.Canonical(cwd))
	return nil
}

func run(once bool, id, sel string, stacked, here bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// -print, -session and -here all name one board directly, so they bypass
	// the launcher: a pipe has no one to pick from a list.
	if once || id != "" || here {
		m, err := boardHere(home, cwd, id, stacked)
		if err != nil {
			return err
		}
		if once {
			fmt.Print(m.Snapshot(sel))
			return nil
		}
		_, err = tea.NewProgram(m).Run()
		return err
	}

	// Everything else lands on the launcher, including a run from inside a
	// project: the project it was run in is preselected there, so opening it is
	// one keypress, and the list stays the way in from anywhere else.
	return launcher(home, cwd, stacked)
}

// launcher runs the project list, opening the board on whatever is picked and
// coming back to the list when the board is left with esc.
func launcher(home, cwd string, stacked bool) error {
	for {
		h := tui.NewHome(home, cwd)
		if _, err := tea.NewProgram(h).Run(); err != nil {
			return err
		}
		if h.Quit || h.Chosen == "" {
			return nil
		}
		root := h.Chosen
		if _, err := project.Remember(root); err != nil {
			return err
		}
		m, err := boardFor(home, root, stacked)
		if err != nil {
			return err
		}
		m.AllowBack()
		if _, err := tea.NewProgram(m).Run(); err != nil {
			return err
		}
		if !m.WentBack() {
			return nil
		}
	}
}

// boardHere builds the board for the session running in cwd, or for id when one
// is named.
func boardHere(home, cwd, id string, stacked bool) (*tui.Model, error) {
	name := ""
	if id == "" {
		sessions, err := session.List()
		if err != nil {
			return nil, err
		}
		s, ok := session.InDir(sessions, cwd)
		if !ok {
			return nil, fmt.Errorf("no claude session running in %s", cwd)
		}
		id, name = s.SessionID, s.Name
	}
	path, err := session.TranscriptPath(home, cwd, id)
	if err != nil {
		return nil, err
	}
	return build(home, cwd, path, name, stacked)
}

// boardFor builds the board for a project picked on the launcher, opening its
// most recent transcript. The session picker inside the board reaches the rest.
func boardFor(home, root string, stacked bool) (*tui.Model, error) {
	locs, err := session.ListLocal(home, root)
	if err != nil {
		return nil, err
	}
	if len(locs) == 0 {
		return nil, fmt.Errorf("no sessions in %s", root)
	}
	return build(home, root, locs[0].Path, filepath.Base(root), stacked)
}

func build(home, cwd, path, name string, stacked bool) (*tui.Model, error) {
	favs, err := favorite.Load(favorite.DefaultPath(home))
	if err != nil {
		return nil, err
	}
	marks, err := done.Load(done.DefaultPath(home))
	if err != nil {
		return nil, err
	}
	m := tui.New(path, session.SubagentsDir(path), name, home, cwd, favs, marks, func() string {
		return time.Now().Format(time.RFC3339)
	})
	m.SetStacked(stacked)
	return m, nil
}
