package grid

import (
	"testing"
	"time"
)

func TestBlackStartPriorityOverReconnect(t *testing.T) {
	m := NewMachine()
	now := time.Now()
	// Lose external grid first.
	if err := m.LoseExternalGrid(now); err != nil {
		t.Fatal(err)
	}
	if m.State() != StateOffGrid {
		t.Fatalf("expected off_grid, got %s", m.State())
	}
	// Black start and reconnect arrive concurrently.
	if err := m.BlackStart("bs-1", now); err != nil {
		t.Fatalf("black start failed: %v", err)
	}
	queued, err := m.RequestReconnect()
	if err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}
	if !queued {
		t.Error("reconnect should be queued during black start")
	}
	if !m.IsReconnectLocked() {
		t.Error("reconnect circuit should be locked during black start")
	}
	if m.State() != StateBlackStarting {
		t.Fatalf("expected black_starting, got %s", m.State())
	}
	// Complete black start: queued reconnect should proceed to syncing.
	if err := m.CompleteBlackStart("bs-1", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if m.IsReconnectLocked() {
		t.Error("reconnect circuit should be unlocked after black start")
	}
	if m.State() != StateSyncing {
		t.Fatalf("expected syncing after queued reconnect, got %s", m.State())
	}
}

func TestBlackStartIdempotent(t *testing.T) {
	m := NewMachine()
	now := time.Now()
	m.LoseExternalGrid(now)
	if err := m.BlackStart("bs-2", now); err != nil {
		t.Fatal(err)
	}
	// Same ID: idempotent.
	if err := m.BlackStart("bs-2", now); err != nil {
		t.Errorf("idempotent black start should not error: %v", err)
	}
	// Different ID: conflict.
	if err := m.BlackStart("bs-3", now); err != ErrBlackStartInProgress {
		t.Errorf("expected ErrBlackStartInProgress, got %v", err)
	}
}

func TestReconnectFromOffGrid(t *testing.T) {
	m := NewMachine()
	now := time.Now()
	m.LoseExternalGrid(now)
	queued, err := m.RequestReconnect()
	if err != nil {
		t.Fatal(err)
	}
	if queued {
		t.Error("reconnect from off-grid should not be queued")
	}
	if m.State() != StateSyncing {
		t.Fatalf("expected syncing, got %s", m.State())
	}
	m.CompleteSync(now.Add(time.Second))
	if m.State() != StateConnected {
		t.Fatalf("expected connected, got %s", m.State())
	}
}

func TestReconnectIdempotentWhenConnected(t *testing.T) {
	m := NewMachine()
	queued, err := m.RequestReconnect()
	if err != nil {
		t.Fatal(err)
	}
	if queued {
		t.Error("should not be queued when already connected")
	}
}

func TestLoseExternalGridIdempotent(t *testing.T) {
	m := NewMachine()
	now := time.Now()
	m.LoseExternalGrid(now)
	m.LoseExternalGrid(now) // should not error
	if m.State() != StateOffGrid {
		t.Fatalf("expected off_grid, got %s", m.State())
	}
}

func TestCompleteBlackStartWrongID(t *testing.T) {
	m := NewMachine()
	now := time.Now()
	m.LoseExternalGrid(now)
	m.BlackStart("bs-4", now)
	if err := m.CompleteBlackStart("wrong", now); err == nil {
		t.Error("expected error for wrong black start ID")
	}
}
