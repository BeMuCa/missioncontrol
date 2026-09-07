package project

import (
	"os"
	"path/filepath"
	"testing"
)

// withHome points the registry at a temporary directory, so a test never reads
// or writes the real ~/.missioncontrol.
func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MISSIONCONTROL_HOME", dir)
	return dir
}

func TestRememberAddsThenRefreshes(t *testing.T) {
	withHome(t)
	root := t.TempDir()

	added, err := Remember(root)
	if err != nil || !added {
		t.Fatalf("first Remember: added=%v err=%v, want added", added, err)
	}
	ps := Load()
	if len(ps) != 1 || ps[0].Root != Canonical(root) {
		t.Fatalf("Load: %+v, want the one project", ps)
	}
	if ps[0].Name != filepath.Base(Canonical(root)) {
		t.Errorf("Name = %q, want the directory base", ps[0].Name)
	}

	// Registering the same project again refreshes it rather than listing it twice.
	added, err = Remember(root + string(filepath.Separator))
	if err != nil || added {
		t.Fatalf("second Remember: added=%v err=%v, want already known", added, err)
	}
	if ps := Load(); len(ps) != 1 {
		t.Fatalf("Load after re-register: %d projects, want 1", len(ps))
	}
}

func TestLoadDropsProjectsWhoseDirectoryIsGone(t *testing.T) {
	withHome(t)
	gone := filepath.Join(t.TempDir(), "removed")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	kept := t.TempDir()
	if _, err := Remember(gone); err != nil {
		t.Fatal(err)
	}
	if _, err := Remember(kept); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}

	ps := Load()
	if len(ps) != 1 || ps[0].Root != Canonical(kept) {
		t.Fatalf("Load: %+v, want only the surviving project", ps)
	}
}

func TestLoadSortsMostRecentlyOpenedFirst(t *testing.T) {
	withHome(t)
	older, newer := t.TempDir(), t.TempDir()
	if _, err := Remember(older); err != nil {
		t.Fatal(err)
	}
	// The stored stamp is whole seconds, so two calls in the same second would
	// tie. Writing the older one by hand keeps the test off the clock.
	ps := Load()
	ps[0].LastOpen = "2020-01-01T00:00:00Z"
	if err := write(filepath.Join(Dir(), "projects.json"), ps); err != nil {
		t.Fatal(err)
	}
	if _, err := Remember(newer); err != nil {
		t.Fatal(err)
	}

	got := Load()
	if len(got) != 2 || got[0].Root != Canonical(newer) {
		t.Fatalf("Load: %+v, want the newer project first", got)
	}
}

func TestForget(t *testing.T) {
	withHome(t)
	root := t.TempDir()
	if _, err := Remember(root); err != nil {
		t.Fatal(err)
	}

	removed, err := Forget(root)
	if err != nil || !removed {
		t.Fatalf("Forget: removed=%v err=%v, want removed", removed, err)
	}
	if ps := Load(); len(ps) != 0 {
		t.Fatalf("Load after Forget: %+v, want empty", ps)
	}
	if removed, _ := Forget(root); removed {
		t.Error("Forget on an unknown project must report nothing removed")
	}
}

func TestMissingRegistryIsEmpty(t *testing.T) {
	withHome(t)
	if ps := Load(); ps != nil {
		t.Fatalf("Load with no registry: %+v, want nil", ps)
	}
}
