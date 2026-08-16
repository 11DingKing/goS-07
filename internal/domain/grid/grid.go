// Package grid implements the microgrid state machine governing external grid
// connection, off-grid independent operation, black start, and auto-sync
// reconnection. Rule 2 is enforced here: black start takes priority over
// reconnection commands, and the reconnect circuit is locked until black start
// completes.
package grid

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// State represents the operational state of the microgrid.
type State string

const (
	StateConnected     State = "connected"
	StateOffGrid       State = "off_grid"
	StateBlackStarting State = "black_starting"
	StateSyncing       State = "syncing"
)

// ErrAlreadyConnected is returned when a black start is requested while the
// grid is already connected.
var ErrAlreadyConnected = errors.New("already connected to external grid")

// ErrBlackStartInProgress is returned when a black start with a different ID
// is already in progress.
var ErrBlackStartInProgress = errors.New("black start already in progress")

// ErrNotBlackStarting is returned when completing a black start that isn't active.
var ErrNotBlackStarting = errors.New("not in black start state")

// Machine is the grid state machine.
type Machine struct {
	mu              sync.Mutex
	state           State
	offGridSince    time.Time
	blackStartID    string
	reconnectQueued bool
	reconnectLocked bool
	blackStartAt    time.Time
	history         []Event
}

// Event records a state transition for audit.
type Event struct {
	Type   string    `json:"type"`
	State  State     `json:"state"`
	At     time.Time `json:"at"`
	Detail string    `json:"detail,omitempty"`
}

// NewMachine creates a machine in the connected state.
func NewMachine() *Machine {
	return &Machine{state: StateConnected}
}

// State returns the current state.
func (m *Machine) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state
}

// LoseExternalGrid transitions from Connected to OffGrid. This is idempotent;
// calling it when already off-grid or black-starting is a no-op.
func (m *Machine) LoseExternalGrid(now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.state {
	case StateOffGrid, StateBlackStarting:
		return nil
	case StateConnected:
		m.state = StateOffGrid
		m.offGridSince = now
		m.recordLocked("lose_external_grid", "external grid lost, entering off-grid")
		return nil
	default:
		return fmt.Errorf("cannot lose external grid from state %s", m.state)
	}
}

// BlackStart initiates a black start. If a black start with the same ID is
// already in progress, the call is idempotent. The reconnect circuit is locked
// for the duration of the black start (Rule 2).
func (m *Machine) BlackStart(id string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.state {
	case StateBlackStarting:
		if id == m.blackStartID {
			return nil
		}
		return ErrBlackStartInProgress
	case StateConnected:
		return ErrAlreadyConnected
	case StateOffGrid, StateSyncing:
		m.state = StateBlackStarting
		m.blackStartID = id
		m.blackStartAt = now
		m.reconnectLocked = true
		m.reconnectQueued = false
		m.recordLocked("black_start", fmt.Sprintf("black start %s initiated", id))
		return nil
	default:
		return fmt.Errorf("cannot black start from state %s", m.state)
	}
}

// RequestReconnect issues an auto grid-connect command. If a black start is in
// progress, the command is queued and the circuit lock prevents immediate
// reconnection (Rule 2). Returns queued=true when the command was queued.
func (m *Machine) RequestReconnect() (queued bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.state {
	case StateConnected, StateSyncing:
		return false, nil
	case StateBlackStarting:
		m.reconnectQueued = true
		m.recordLocked("reconnect_queued", "reconnect queued during black start")
		return true, nil
	case StateOffGrid:
		m.state = StateSyncing
		m.recordLocked("reconnect_begin", "beginning synchronization")
		return false, nil
	default:
		return false, fmt.Errorf("cannot reconnect from state %s", m.state)
	}
}

// CompleteBlackStart finishes the black start. The reconnect circuit is
// unlocked. If a reconnect was queued, the machine transitions to syncing
// (Rule 2: queued reconnect proceeds after black start).
func (m *Machine) CompleteBlackStart(id string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != StateBlackStarting {
		return ErrNotBlackStarting
	}
	if id != m.blackStartID {
		return fmt.Errorf("black start id mismatch: expected %s, got %s", m.blackStartID, id)
	}
	m.reconnectLocked = false
	m.blackStartID = ""
	if m.reconnectQueued {
		m.reconnectQueued = false
		m.state = StateSyncing
		m.recordLocked("reconnect_resume", "queued reconnect resumed after black start")
	} else {
		m.state = StateOffGrid
		m.recordLocked("black_start_complete", "black start complete, running off-grid")
	}
	return nil
}

// CompleteSync finalizes the reconnection to the external grid.
func (m *Machine) CompleteSync(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == StateSyncing {
		m.state = StateConnected
		m.offGridSince = time.Time{}
		m.recordLocked("grid_connected", "synchronized and reconnected")
	}
}

// IsReconnectLocked reports whether the reconnect circuit is locked (Rule 2).
func (m *Machine) IsReconnectLocked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reconnectLocked
}

// IsReconnectQueued reports whether a reconnect command is waiting.
func (m *Machine) IsReconnectQueued() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reconnectQueued
}

// CheckOffGridDeadline returns true if the system has been off-grid longer
// than the allowed transition deadline without completing the transition.
func (m *Machine) CheckOffGridDeadline(now time.Time, deadline time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == StateConnected || m.offGridSince.IsZero() {
		return false
	}
	return now.Sub(m.offGridSince) > deadline
}

// History returns a copy of the transition log.
func (m *Machine) History() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.history))
	copy(out, m.history)
	return out
}

// Snapshot is a serialisable copy of the machine state.
type Snapshot struct {
	State           State     `json:"state"`
	OffGridSince    time.Time `json:"off_grid_since"`
	BlackStartID    string    `json:"black_start_id"`
	ReconnectQueued bool      `json:"reconnect_queued"`
	ReconnectLocked bool      `json:"reconnect_locked"`
}

// Snapshot returns a serialisable copy of the machine state.
func (m *Machine) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Snapshot{
		State:           m.state,
		OffGridSince:    m.offGridSince,
		BlackStartID:    m.blackStartID,
		ReconnectQueued: m.reconnectQueued,
		ReconnectLocked: m.reconnectLocked,
	}
}

// Restore replaces the machine state from a snapshot.
func (m *Machine) Restore(s Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = s.State
	m.offGridSince = s.OffGridSince
	m.blackStartID = s.BlackStartID
	m.reconnectQueued = s.ReconnectQueued
	m.reconnectLocked = s.ReconnectLocked
}

func (m *Machine) recordLocked(typ, detail string) {
	m.history = append(m.history, Event{Type: typ, State: m.state, At: time.Now(), Detail: detail})
	if len(m.history) > 200 {
		m.history = m.history[len(m.history)-200:]
	}
}
