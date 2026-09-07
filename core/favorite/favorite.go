// Package favorite persists question/answer pairs the user wants to keep.
//
// Sessions on disk are deleted after cleanupPeriodDays, so a pair worth keeping
// has to be copied out of the transcript into a store of its own. That store is
// a JSONL file the user owns; it never expires and survives /clear, which only
// starts a new session rather than deleting anything.
package favorite

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

// Favorite is one saved pair, frozen at the moment it was starred.
type Favorite struct {
	Key      string `json:"key"`     // session + row id, so re-starring updates in place
	Session  string `json:"session"` // session name, for display
	PromptID string `json:"prompt_id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	SavedAt  string `json:"saved_at"` // RFC3339, passed in — this package stamps nothing
}

// Store is the on-disk favorites file.
type Store struct {
	path string
	favs []Favorite
}

// DefaultPath is where favorites live: alongside the rest of missioncontrol's
// own state, not inside .claude/projects where sessions get swept.
func DefaultPath(home string) string {
	return filepath.Join(home, ".claude", "missioncontrol", "favorites.jsonl")
}

// Load reads the store. A missing file is an empty store, not an error.
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var fav Favorite
		if json.Unmarshal(sc.Bytes(), &fav) == nil {
			s.favs = append(s.favs, fav)
		}
	}
	return s, sc.Err()
}

// List returns the saved pairs, newest last (the order they were added).
func (s *Store) List() []Favorite { return s.favs }

// Has reports whether a row is already saved.
func (s *Store) Has(key string) bool {
	for _, f := range s.favs {
		if f.Key == key {
			return true
		}
	}
	return false
}

// Toggle adds the pair if its key is new, or removes it if already saved, and
// reports whether it is saved afterwards. The whole store is rewritten: it is a
// handful of entries a person curates by hand, not a hot path.
func (s *Store) Toggle(f Favorite) (saved bool, err error) {
	kept := s.favs[:0:0]
	found := false
	for _, existing := range s.favs {
		if existing.Key == f.Key {
			found = true
			continue
		}
		kept = append(kept, existing)
	}
	if !found {
		kept = append(kept, f)
	}
	s.favs = kept
	return !found, s.save()
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, fav := range s.favs {
		if err := enc.Encode(fav); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
