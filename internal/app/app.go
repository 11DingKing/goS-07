// Package app orchestrates the battery fleet, work order book, grid state
// machine, and controller pair. It is the single mutation boundary: all state
// changes are serialised through one mutex, snapshots are taken atomically for
// controller failover, and operations are idempotent where the business rules
// require it.
package app

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"ejina-microgrid/internal/config"
	"ejina-microgrid/internal/domain/battery"
	"ejina-microgrid/internal/domain/controller"
	"ejina-microgrid/internal/domain/grid"
	"ejina-microgrid/internal/domain/workorder"
	"ejina-microgrid/internal/store"
)

// ErrFailoverNotNeeded is returned when a manual failover is requested but
// the active controller is still healthy.
var ErrFailoverNotNeeded = errors.New("failover not needed: active controller is healthy")

// StateSnapshot is a complete, serialisable copy of the dispatch service state.
// It is used for persistence and for controller failover replay.
type StateSnapshot struct {
	Cabins      []battery.Cabin     `json:"cabins"`
	FleetDemand float64             `json:"fleet_demand"`
	Orders      []workorder.Order   `json:"orders"`
	Grid        grid.Snapshot       `json:"grid"`
	Controllers controller.Snapshot `json:"controllers"`
	Timestamp   time.Time           `json:"timestamp"`
}

// App is the dispatch orchestration core.
type App struct {
	mu          sync.Mutex
	cfg         config.Config
	fleet       *battery.Fleet
	orders      *workorder.Book
	grid        *grid.Machine
	controllers *controller.Pair
	store       *store.Store
	snapshot    StateSnapshot
	clock       func() time.Time
}

// New creates an App with the given configuration and persistent store.
func New(cfg config.Config, st *store.Store) *App {
	return &App{
		cfg:         cfg,
		fleet:       battery.NewFleet(),
		orders:      workorder.NewBook(cfg.EscalationTimeout),
		grid:        grid.NewMachine(),
		controllers: controller.NewPair("master-01", "backup-01", cfg.HeartbeatTimeout),
		store:       st,
		clock:       time.Now,
	}
}

// SetClock replaces the internal clock (for testing).
func (a *App) SetClock(fn func() time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.clock = fn
}

func (a *App) now() time.Time {
	return a.clock()
}

// --- Battery cabin management ---

// RegisterCabin adds a cabin to the fleet.
func (a *App) RegisterCabin(c battery.Cabin) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if c.ID == "" {
		return errors.New("cabin id is required")
	}
	if c.Capacity <= 0 {
		return errors.New("cabin capacity must be positive")
	}
	a.fleet.Register(c, a.cfg.SOCAlarmThreshold)
	return nil
}

// ListCabins returns all cabins.
func (a *App) ListCabins() []battery.Cabin {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fleet.List()
}

// UpdateCabinSensors applies new sensor readings (SOC, temperature, insulation).
func (a *App) UpdateCabinSensors(id string, soc, temp, insulation float64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fleet.Update(id, soc, temp, insulation, a.cfg.SOCAlarmThreshold)
}

// SetCabinOffline takes a cabin out of service (Rule 4).
func (a *App) SetCabinOffline(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fleet.SetOffline(id, a.cfg.SOCAlarmThreshold)
}

// SetCabinOnline brings a cabin back into service.
func (a *App) SetCabinOnline(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.fleet.SetOnline(id, a.cfg.SOCAlarmThreshold)
}

// SetFleetDemand sets the total power demand for the fleet.
func (a *App) SetFleetDemand(kW float64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.fleet.SetDemand(kW, a.cfg.SOCAlarmThreshold)
}

// InspectCabin performs an inspection and returns detected anomaly types.
func (a *App) InspectCabin(id string) ([]battery.AnomalyType, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.fleet.Get(id)
	if !ok {
		return nil, fmt.Errorf("cabin %s not found", id)
	}
	limits := battery.InspectLimits{
		MaxTemperature: a.cfg.InspectionTempLimit,
		MinInsulation:  a.cfg.InsulationMinMOhm,
		MinSOC:         a.cfg.SOCAlarmThreshold,
	}
	return c.Inspect(limits), nil
}

// --- Work order management ---

// CreateOrder creates an anomaly work order for a cabin.
func (a *App) CreateOrder(cabinID string, anomalyType battery.AnomalyType, severity workorder.Severity) (workorder.Order, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.fleet.Get(cabinID); !ok {
		return workorder.Order{}, fmt.Errorf("cabin %s not found", cabinID)
	}
	return a.orders.Create(cabinID, anomalyType, severity, a.now())
}

// AcceptOrder accepts a work order (idempotent).
func (a *App) AcceptOrder(id string) (workorder.Order, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.orders.Accept(id, a.now())
}

// ResolveOrder resolves a work order with a result description.
func (a *App) ResolveOrder(id, result string) (workorder.Order, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.orders.Resolve(id, result, a.now())
}

// ListOrders returns all work orders.
func (a *App) ListOrders() []workorder.Order {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.orders.List()
}

// --- Grid management ---

// LoseExternalGrid transitions to off-grid independent operation.
func (a *App) LoseExternalGrid() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.grid.LoseExternalGrid(a.now())
}

// BlackStart issues a black start command (Rule 2: priority over reconnect).
func (a *App) BlackStart(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.grid.BlackStart(id, a.now())
}

// RequestReconnect issues an auto grid-connect command. Returns true if queued.
func (a *App) RequestReconnect() (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.grid.RequestReconnect()
}

// CompleteBlackStart finishes a black start.
func (a *App) CompleteBlackStart(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.grid.CompleteBlackStart(id, a.now())
}

// CompleteSync finalizes reconnection to the external grid.
func (a *App) CompleteSync() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.grid.CompleteSync(a.now())
}

// GridSnapshot returns the current grid state.
func (a *App) GridSnapshot() grid.Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.grid.Snapshot()
}

// IsReconnectLocked reports whether the reconnect circuit is locked (Rule 2).
func (a *App) IsReconnectLocked() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.grid.IsReconnectLocked()
}

// --- Controller management ---

// Heartbeat records a heartbeat from a controller.
func (a *App) Heartbeat(controllerID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.controllers.Heartbeat(controllerID, a.now())
}

// ControllerSnapshot returns the current controller pair state.
func (a *App) ControllerSnapshot() controller.Snapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.controllers.Snapshot()
}

// TriggerFailover manually triggers a controller failover.
func (a *App) TriggerFailover() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.controllers.NeedsFailover(a.now()) {
		return ErrFailoverNotNeeded
	}
	a.controllers.PromoteBackup(a.now())
	a.replayLatestSnapshotLocked()
	return nil
}

// --- Background job callbacks ---

// CheckEscalations escalates open work orders past the 4-hour deadline (Rule 3).
func (a *App) CheckEscalations() []workorder.Order {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.orders.CheckEscalations(a.now())
}

// CheckControllerHealth detects a stale heartbeat and triggers failover with
// snapshot replay (Rule 5). Returns true if a failover occurred.
func (a *App) CheckControllerHealth() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.controllers.NeedsFailover(a.now()) {
		return false
	}
	a.controllers.PromoteBackup(a.now())
	// Replay operational state from the latest snapshot, but keep the
	// controller pair's own failover state (activeID, failover count).
	a.replayLatestSnapshotLocked()
	return true
}

// CheckGridTransition reports whether the off-grid transition deadline has
// been exceeded.
func (a *App) CheckGridTransition() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.grid.CheckOffGridDeadline(a.now(), a.cfg.OffGridDeadline)
}

// SaveSnapshot captures the current state into memory and persists it to disk.
func (a *App) SaveSnapshot() error {
	a.mu.Lock()
	snap := a.snapshotLocked()
	a.snapshot = snap
	a.mu.Unlock()
	if a.store != nil {
		if err := a.store.SaveJSON(snap); err != nil {
			return fmt.Errorf("persist snapshot: %w", err)
		}
	}
	return nil
}

// LoadSnapshot loads the persisted state from disk and restores it.
func (a *App) LoadSnapshot() error {
	if a.store == nil {
		return nil
	}
	var snap StateSnapshot
	if err := a.store.LoadJSON(&snap); err != nil {
		if errors.Is(err, store.ErrNoSnapshot) {
			return nil
		}
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.restoreLocked(snap)
	a.snapshot = snap
	return nil
}

// Snapshot returns a copy of the current complete state.
func (a *App) Snapshot() StateSnapshot {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.snapshotLocked()
}

func (a *App) snapshotLocked() StateSnapshot {
	cabins, demand := a.fleet.Snapshot()
	return StateSnapshot{
		Cabins:      cabins,
		FleetDemand: demand,
		Orders:      a.orders.Snapshot(),
		Grid:        a.grid.Snapshot(),
		Controllers: a.controllers.Snapshot(),
		Timestamp:   a.now(),
	}
}

// replayLatestSnapshotLocked restores the operational state that the backup
// controller has to continue from (Rule 5). A snapshot is only usable once it
// has actually been captured: before the first capture the live in-memory state
// is already the most recent state and must be kept as-is.
func (a *App) replayLatestSnapshotLocked() {
	if a.snapshot.Timestamp.IsZero() {
		return
	}
	a.restoreOperationalLocked(a.snapshot)
}

func (a *App) restoreOperationalLocked(snap StateSnapshot) {
	a.fleet.Restore(snap.Cabins, snap.FleetDemand)
	a.orders.Restore(snap.Orders)
	a.grid.Restore(snap.Grid)
}

func (a *App) restoreLocked(snap StateSnapshot) {
	a.restoreOperationalLocked(snap)
	a.controllers.Restore(snap.Controllers)
}

// Config returns the app's configuration.
func (a *App) Config() config.Config {
	return a.cfg
}
