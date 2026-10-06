// Package labels records the label the plugin last wrote for each worktree
// checkout, so automatic updates can tell plugin-written labels from labels
// the user chose.
//
// Entries are keyed by canonical checkout path because herdr assigns a new
// workspace id when a closed worktree is reopened.
package labels

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/sungjunyoung/herdr-linear-title/internal/fileutil"
)

const (
	fileName = "labels.json"
	lockName = "labels.lock"
)

// Store is a labels.json file in a plugin state directory.
type Store struct {
	Dir string
}

// Get returns the label recorded for checkoutPath.
func (s Store) Get(checkoutPath string) (string, bool, error) {
	m, err := s.read()
	if err != nil {
		return "", false, err
	}
	label, ok := m[Canonical(checkoutPath)]
	return label, ok, nil
}

// Set records label for checkoutPath.
func (s Store) Set(checkoutPath, label string) error {
	unlock, err := fileutil.Lock(filepath.Join(s.Dir, lockName))
	if err != nil {
		return err
	}
	defer unlock()

	m, err := s.read()
	if err != nil {
		return err
	}
	m[Canonical(checkoutPath)] = label
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.WriteAtomic(filepath.Join(s.Dir, fileName), data, 0o600)
}

func (s Store) read() (map[string]string, error) {
	path := filepath.Join(s.Dir, fileName)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return m, nil
}

// Canonical resolves symlinks so the same checkout reported as /tmp/x and
// /private/tmp/x (macOS) maps to one key. Paths that no longer exist are only
// cleaned.
func Canonical(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
