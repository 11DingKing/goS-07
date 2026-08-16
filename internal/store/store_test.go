package store

import (
	"path/filepath"
	"testing"
)

func TestStoreSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "state.json")
	s := New(path)
	type state struct {
		Count int    `json:"count"`
		Name  string `json:"name"`
	}
	in := state{Count: 42, Name: "ejina"}
	if err := s.SaveJSON(in); err != nil {
		t.Fatal(err)
	}
	var out state
	if err := s.LoadJSON(&out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 42 || out.Name != "ejina" {
		t.Errorf("round-trip mismatch: got %+v", out)
	}
	// Overwrite and reload to confirm atomic replacement.
	in.Count = 99
	if err := s.SaveJSON(in); err != nil {
		t.Fatal(err)
	}
	if err := s.LoadJSON(&out); err != nil {
		t.Fatal(err)
	}
	if out.Count != 99 {
		t.Errorf("expected 99, got %d", out.Count)
	}
}

func TestStoreLoadMissing(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "missing.json"))
	_, err := s.Load()
	if err != ErrNoSnapshot {
		t.Errorf("expected ErrNoSnapshot, got %v", err)
	}
	var v interface{}
	if err := s.LoadJSON(&v); err != ErrNoSnapshot {
		t.Errorf("expected ErrNoSnapshot, got %v", err)
	}
}

func TestStoreSaveRaw(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "raw.json"))
	if err := s.Save([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	data, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("expected hello, got %s", string(data))
	}
}
