// Package project keeps the list of projects the board can be opened on, so
// missioncontrol can be started from anywhere instead of only from inside the
// directory a session runs in.
//
// A project is just a directory that has had Claude Code sessions in it; the
// transcripts are found from the root path alone (see core/session). The
// registry is therefore a convenience list, not a source of truth: deleting it
// loses nothing but the shortcuts.
package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// DirName is the per-user directory the registry lives in, beside ~/.claude.
const DirName = ".missioncontrol"

// Project is one directory the board has been opened on.
type Project struct {
	Root     string `json:"root"`
	Name     string `json:"name"`
	LastOpen string `json:"last_open"`
}

// Dir is the per-user directory holding the registry. MISSIONCONTROL_HOME
// overrides it, which is what the tests use instead of writing to a real home.
func Dir() string {
	if v := os.Getenv("MISSIONCONTROL_HOME"); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, DirName)
}

func projectsPath() string {
	dir := Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "projects.json")
}

// Load returns the registered projects, most recently opened first, dropping
// any whose directory is gone. A project that still exists but has no sessions
// yet is kept: the sessions come and go, the project does not.
func Load() []Project {
	path := projectsPath()
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var ps []Project
	if err := json.Unmarshal(b, &ps); err != nil {
		return nil
	}
	var out []Project
	for _, p := range ps {
		if fi, err := os.Stat(p.Root); err == nil && fi.IsDir() {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastOpen > out[j].LastOpen })
	return out
}

// Remember records a project, or refreshes its position if it is already known.
// It reports whether the project was new, so `init` can say which of the two it
// did rather than looking like nothing happened.
func Remember(root string) (added bool, err error) {
	path := projectsPath()
	if path == "" {
		return false, os.ErrNotExist
	}
	root = Canonical(root)
	ps := Load()
	now := time.Now().UTC().Format(time.RFC3339)
	for i := range ps {
		if Canonical(ps[i].Root) == root {
			ps[i].LastOpen = now
			return false, write(path, ps)
		}
	}
	ps = append(ps, Project{Root: root, Name: filepath.Base(root), LastOpen: now})
	return true, write(path, ps)
}

// Forget drops a project from the registry, reporting whether it was there.
func Forget(root string) (removed bool, err error) {
	path := projectsPath()
	if path == "" {
		return false, os.ErrNotExist
	}
	root = Canonical(root)
	var out []Project
	for _, p := range Load() {
		if Canonical(p.Root) == root {
			removed = true
			continue
		}
		out = append(out, p)
	}
	if !removed {
		return false, nil
	}
	return true, write(path, out)
}

func write(path string, ps []Project) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

// Canonical reduces a path to one spelling, so the same project registered
// twice — once with a trailing slash, once relative, once through a symlink —
// is recognised as the same project rather than listed twice.
func Canonical(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}
