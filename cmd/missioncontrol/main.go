// Command missioncontrol shows what a running Claude Code session is doing: the prompts
// you typed, the numbered items in each, and the agents each one spawned.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/BeMuCa/missioncontrol/core/done"
	"github.com/BeMuCa/missioncontrol/core/favorite"
	"github.com/BeMuCa/missioncontrol/core/session"
	"github.com/BeMuCa/missioncontrol/internal/tui"
)

func main() {
	once := flag.Bool("print", false, "render the board once as text and exit")
	sel := flag.String("select", "", "with -print: preselect a row by id (P3, P3.2, A7)")
	stacked := flag.Bool("stacked", true, "start rotated: requests across the top, the selected one opened up below (-stacked=false for the split layout)")
	id := flag.String("session", "", "session id to observe (default: the interactive session in this directory)")
	flag.Parse()

	if err := run(*once, *id, *sel, *stacked); err != nil {
		fmt.Fprintln(os.Stderr, "missioncontrol:", err)
		os.Exit(1)
	}
}

func run(once bool, id, sel string, stacked bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	name := ""
	if id == "" {
		sessions, err := session.List()
		if err != nil {
			return err
		}
		s, ok := session.InDir(sessions, cwd)
		if !ok {
			return fmt.Errorf("no claude session running in %s", cwd)
		}
		id, name = s.SessionID, s.Name
	}

	path, err := session.TranscriptPath(home, cwd, id)
	if err != nil {
		return err
	}

	favs, err := favorite.Load(favorite.DefaultPath(home))
	if err != nil {
		return err
	}
	marks, err := done.Load(done.DefaultPath(home))
	if err != nil {
		return err
	}
	m := tui.New(path, session.SubagentsDir(path), name, home, cwd, favs, marks, func() string {
		return time.Now().Format(time.RFC3339)
	})
	m.SetStacked(stacked)
	if once {
		fmt.Print(m.Snapshot(sel))
		return nil
	}
	_, err = tea.NewProgram(m).Run()
	return err
}
