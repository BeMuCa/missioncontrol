// Package done keeps the marks a reader sets on question/answer pairs they have
// worked through. Like favorites it lives outside .claude/projects, where
// sessions get swept, and is a JSONL file of a size a person curates by hand.
package done

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

type mark struct {
	Key string `json:"key"` // session + row id
	At  string `json:"at"`  // RFC3339, passed in — this package stamps nothing
}

// Set is the on-disk file of marks.
type Set struct {
	path  string
	marks []mark
}

// DefaultPath is where marks live, beside the favorites.
func DefaultPath(home string) string {
	return filepath.Join(home, ".claude", "missioncontrol", "done.jsonl")
}

// Load reads the set. A missing file is an empty set, not an error.
func Load(path string) (*Set, error) {
	s := &Set{path: path}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m mark
		if len(sc.Bytes()) > 0 && json.Unmarshal(sc.Bytes(), &m) == nil {
			s.marks = append(s.marks, m)
		}
	}
	return s, sc.Err()
}

// Has reports whether a row is marked.
func (s *Set) Has(key string) bool {
	for _, m := range s.marks {
		if m.Key == key {
			return true
		}
	}
	return false
}

// Toggle marks the row or clears the mark, and reports whether it is marked
// afterwards. The whole file is rewritten; it is small.
func (s *Set) Toggle(key, at string) (on bool, err error) {
	kept := s.marks[:0:0]
	found := false
	for _, m := range s.marks {
		if m.Key == key {
			found = true
			continue
		}
		kept = append(kept, m)
	}
	if !found {
		kept = append(kept, mark{Key: key, At: at})
	}
	s.marks = kept
	return !found, s.save()
}

func (s *Set) save() error {
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
	for _, m := range s.marks {
		if err := enc.Encode(m); err != nil {
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
