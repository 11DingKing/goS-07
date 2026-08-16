package controller

import (
	"testing"
	"time"
)

func newTestPair(t *testing.T) *Pair {
	t.Helper()
	return NewPair("master-01", "backup-01", 10*time.Second)
}

func TestPairInitialState(t *testing.T) {
	p := newTestPair(t)
	if p.ActiveID() != "master-01" {
		t.Errorf("expected master-01 active, got %s", p.ActiveID())
	}
	if p.FailoverCount() != 0 {
		t.Errorf("expected 0 failovers, got %d", p.FailoverCount())
	}
}

func TestNeedsFailoverBeforeHeartbeat(t *testing.T) {
	// Rule 5: a master that has never heartbeated is considered dead, so a
	// failover is required immediately (fail-safe on cold start).
	p := newTestPair(t)
	now := time.Now()
	if !p.NeedsFailover(now) {
		t.Error("expected failover needed before any heartbeat")
	}
}

func TestHeartbeatPreventsFailover(t *testing.T) {
	p := newTestPair(t)
	now := time.Now()
	if err := p.Heartbeat("master-01", now); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	// Within the timeout window the master is alive.
	if p.NeedsFailover(now.Add(5 * time.Second)) {
		t.Error("expected no failover within heartbeat timeout")
	}
}

func TestHeartbeatStaleTriggersFailover(t *testing.T) {
	p := newTestPair(t)
	now := time.Now()
	p.Heartbeat("master-01", now)
	// Advance past the 10s timeout.
	if !p.NeedsFailover(now.Add(11 * time.Second)) {
		t.Error("expected failover needed after stale heartbeat")
	}
}

func TestHeartbeatFromUnknownController(t *testing.T) {
	p := newTestPair(t)
	if err := p.Heartbeat("rogue", time.Now()); err == nil {
		t.Error("expected error for unknown controller")
	}
}

func TestHeartbeatFromBackupAccepted(t *testing.T) {
	// Both controllers may send heartbeats; both refresh the liveness timer.
	p := newTestPair(t)
	now := time.Now()
	if err := p.Heartbeat("backup-01", now); err != nil {
		t.Fatalf("backup heartbeat: %v", err)
	}
	if p.NeedsFailover(now.Add(5 * time.Second)) {
		t.Error("backup heartbeat should keep master alive")
	}
}

func TestPromoteBackupSwitchesActiveAndCounts(t *testing.T) {
	p := newTestPair(t)
	now := time.Now()
	p.Heartbeat("master-01", now)

	if p.NeedsFailover(now) {
		t.Fatal("did not expect failover needed yet")
	}
	// Stale heartbeat → failover.
	newActive := p.PromoteBackup(now.Add(15 * time.Second))
	if newActive != "backup-01" {
		t.Errorf("expected backup-01, got %s", newActive)
	}
	if p.ActiveID() != "backup-01" {
		t.Errorf("expected backup-01 active, got %s", p.ActiveID())
	}
	if p.FailoverCount() != 1 {
		t.Errorf("expected 1 failover, got %d", p.FailoverCount())
	}
}

func TestNoFailoverAfterAlreadyOnBackup(t *testing.T) {
	// Once the backup has taken over, a stale heartbeat must not trigger
	// another failover (idempotent failover target).
	p := newTestPair(t)
	now := time.Now()
	p.Heartbeat("master-01", now)
	p.PromoteBackup(now.Add(15 * time.Second))
	if p.NeedsFailover(now.Add(99 * time.Hour)) {
		t.Error("should not failover again once on backup")
	}
}

func TestPromoteBackupResetsHeartbeat(t *testing.T) {
	// The newly promoted controller's heartbeat clock resets so it is given
	// a full grace window before another health check.
	p := newTestPair(t)
	now := time.Now()
	p.Heartbeat("master-01", now)
	p.PromoteBackup(now.Add(15 * time.Second))
	if p.NeedsFailover(now.Add(20 * time.Second)) {
		t.Error("heartbeat should be reset after failover")
	}
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	p := newTestPair(t)
	now := time.Now()
	p.Heartbeat("master-01", now)
	p.PromoteBackup(now.Add(15 * time.Second))
	snap := p.Snapshot()
	if snap.ActiveID != "backup-01" {
		t.Errorf("snapshot active: got %s", snap.ActiveID)
	}
	if snap.FailoverCount != 1 {
		t.Errorf("snapshot count: got %d", snap.FailoverCount)
	}

	restored := NewPair("m", "b", 5*time.Second)
	restored.Restore(snap)
	if restored.ActiveID() != "backup-01" {
		t.Errorf("restore active: got %s", restored.ActiveID())
	}
	if restored.FailoverCount() != 1 {
		t.Errorf("restore count: got %d", restored.FailoverCount())
	}
	rsnap := restored.Snapshot()
	if rsnap.MasterID != "master-01" || rsnap.BackupID != "backup-01" {
		t.Errorf("restore ids: master=%s backup=%s", rsnap.MasterID, rsnap.BackupID)
	}
}
