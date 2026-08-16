// Package store provides file-based persistence for the dispatch service's
// state snapshots. Writes are atomic (write-to-temp then rename) so a crash
// never leaves a partially written state file.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// ErrNoSnapshot is returned when no state file exists.
var ErrNoSnapshot = errors.New("no snapshot on disk")

// Store persists state snapshots to a JSON file.
type Store struct {
	mu   sync.Mutex
	path string
}

// New creates a store at the given file path.
func New(path string) *Store {
	return &Store{path: path}
}

// Save atomically writes the snapshot to disk.
func (s *Store) Save(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Dir(s.path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create store dir: %w", err)
		}
	}
	tmp, err := os.CreateTemp(dir, ".tmp-state-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // best-effort cleanup if rename fails
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("rename temp to final: %w", err)
	}
	return nil
}

// Load reads the snapshot from disk. Returns ErrNoSnapshot if the file does
// not exist.
func (s *Store) Load() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoSnapshot
		}
		return nil, fmt.Errorf("read store: %w", err)
	}
	return data, nil
}

// SaveJSON marshals v to JSON and saves it.
func (s *Store) SaveJSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	return s.Save(data)
}

// LoadJSON reads and unmarshals the snapshot into v.
func (s *Store) LoadJSON(v interface{}) error {
	data, err := s.Load()
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("unmarshal snapshot: %w", err)
	}
	return nil
}
