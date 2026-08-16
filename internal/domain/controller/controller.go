// Package controller models the master/backup controller pair and the
// heartbeat-based failover mechanism (Rule 5). When the active controller's
// heartbeat is lost, the backup takes over within the configured grace period
// and replays the latest complete state snapshot.
package controller

import (
	"fmt"
	"sync"
	"time"
)

// Role identifies a controller's current role.
type Role string

const (
	RoleMaster Role = "master"
	RoleBackup Role = "backup"
)

// Snapshot is a serialisable copy of the controller pair state.
type Snapshot struct {
	MasterID      string    `json:"master_id"`
	BackupID      string    `json:"backup_id"`
	ActiveID      string    `json:"active_id"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
	FailoverCount int       `json:"failover_count"`
}

// Pair manages the master and backup controllers.
type Pair struct {
	mu            sync.Mutex
	masterID      string
	backupID      string
	activeID      string
	lastHeartbeat time.Time
	timeout       time.Duration // heartbeat timeout before failover
	failoverCount int
	failoverAt    time.Time
}

// NewPair creates a controller pair with the given IDs and heartbeat timeout.
func NewPair(masterID, backupID string, timeout time.Duration) *Pair {
	return &Pair{
		masterID: masterID,
		backupID: backupID,
		activeID: masterID,
		timeout:  timeout,
	}
}

// Heartbeat records a heartbeat from the given controller.
func (p *Pair) Heartbeat(id string, now time.Time) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id != p.masterID && id != p.backupID {
		return fmt.Errorf("unknown controller %s", id)
	}
	p.lastHeartbeat = now
	return nil
}

// NeedsFailover reports whether the active controller's heartbeat is stale
// beyond the timeout and a failover to the backup should be triggered (Rule 5).
func (p *Pair) NeedsFailover(now time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.activeID == p.backupID {
		return false // already failed over to backup
	}
	if p.lastHeartbeat.IsZero() {
		return true // never received a heartbeat
	}
	return now.Sub(p.lastHeartbeat) > p.timeout
}

// PromoteBackup makes the backup controller active. This is called after
// NeedsFailover returns true. Returns the new active controller ID.
func (p *Pair) PromoteBackup(now time.Time) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activeID = p.backupID
	p.failoverCount++
	p.failoverAt = now
	p.lastHeartbeat = now // reset heartbeat for the new active
	return p.activeID
}

// ActiveID returns the currently active controller's ID.
func (p *Pair) ActiveID() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activeID
}

// FailoverCount returns the number of failovers that have occurred.
func (p *Pair) FailoverCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failoverCount
}

// Snapshot returns a serialisable copy of the pair state.
func (p *Pair) Snapshot() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Snapshot{
		MasterID:      p.masterID,
		BackupID:      p.backupID,
		ActiveID:      p.activeID,
		LastHeartbeat: p.lastHeartbeat,
		FailoverCount: p.failoverCount,
	}
}

// Restore replaces the pair state from a snapshot.
func (p *Pair) Restore(s Snapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.masterID = s.MasterID
	p.backupID = s.BackupID
	p.activeID = s.ActiveID
	p.lastHeartbeat = s.LastHeartbeat
	p.failoverCount = s.FailoverCount
}
