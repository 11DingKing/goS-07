package app

import (
	"sync"
	"testing"
	"time"

	"ejina-microgrid/internal/config"
	"ejina-microgrid/internal/domain/battery"
	"ejina-microgrid/internal/domain/grid"
	"ejina-microgrid/internal/domain/workorder"
	"ejina-microgrid/internal/store"
)

func testConfig() config.Config {
	cfg := config.Default()
	cfg.StorePath = ""
	cfg.EscalationTimeout = 4 * time.Hour
	cfg.HeartbeatTimeout = 10 * time.Second
	cfg.OffGridDeadline = 90 * time.Second
	return cfg
}

func TestAppBlackStartPriorityAndLock(t *testing.T) {
	app := New(testConfig(), nil)
	app.LoseExternalGrid()
	// Black start and reconnect arrive concurrently.
	if err := app.BlackStart("bs-100"); err != nil {
		t.Fatalf("black start: %v", err)
	}
	queued, err := app.RequestReconnect()
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if !queued {
		t.Error("reconnect should be queued during black start")
	}
	if !app.IsReconnectLocked() {
		t.Error("reconnect circuit should be locked")
	}
	// Complete black start: queued reconnect proceeds.
	if err := app.CompleteBlackStart("bs-100"); err != nil {
		t.Fatal(err)
	}
	if app.IsReconnectLocked() {
		t.Error("circuit should be unlocked after black start")
	}
	if app.GridSnapshot().State != grid.StateSyncing {
		t.Fatalf("expected syncing, got %s", app.GridSnapshot().State)
	}
	app.CompleteSync()
	if app.GridSnapshot().State != grid.StateConnected {
		t.Fatalf("expected connected, got %s", app.GridSnapshot().State)
	}
}

func TestAppControllerFailoverWithSnapshotReplay(t *testing.T) {
	app := New(testConfig(), nil)
	// Register cabins and set demand so there is meaningful state.
	app.RegisterCabin(battery.Cabin{ID: "c1", Capacity: 100, SOC: 80, Temperature: 25, Insulation: 5, Online: true})
	app.RegisterCabin(battery.Cabin{ID: "c2", Capacity: 100, SOC: 60, Temperature: 25, Insulation: 5, Online: true})
	app.SetFleetDemand(200)
	// Take a snapshot to simulate periodic saves.
	if err := app.SaveSnapshot(); err != nil {
		t.Fatal(err)
	}
	// Simulate heartbeat going stale by advancing the clock.
	base := time.Now()
	app.SetClock(func() time.Time { return base.Add(20 * time.Second) })
	// Failover should trigger and replay the snapshot.
	if !app.CheckControllerHealth() {
		t.Fatal("expected failover to trigger")
	}
	snap := app.ControllerSnapshot()
	if snap.ActiveID != "backup-01" {
		t.Errorf("expected backup-01 active, got %s", snap.ActiveID)
	}
	if snap.FailoverCount != 1 {
		t.Errorf("expected 1 failover, got %d", snap.FailoverCount)
	}
	// State should be replayed from snapshot.
	cabins := app.ListCabins()
	if len(cabins) != 2 {
		t.Errorf("expected 2 cabins after replay, got %d", len(cabins))
	}
}

func TestAppSOCAlarmIntegration(t *testing.T) {
	app := New(testConfig(), nil)
	app.RegisterCabin(battery.Cabin{ID: "c1", Capacity: 100, SOC: 50, Temperature: 25, Insulation: 5, Online: true})
	app.SetFleetDemand(100)
	// Drop SOC below 15%: alarm + priority charge (Rule 1).
	if err := app.UpdateCabinSensors("c1", 10, 25, 5); err != nil {
		t.Fatal(err)
	}
	c, _ := app.ListCabins()[0], error(nil)
	cabins := app.ListCabins()
	for _, cc := range cabins {
		if cc.ID == "c1" {
			c = cc
		}
	}
	if !c.Charging {
		t.Error("low-SOC cabin should be charging")
	}
	if c.Output != 0 {
		t.Errorf("low-SOC cabin should not discharge, output=%.1f", c.Output)
	}
	// Inspection should detect SOC anomaly.
	anomalies, err := app.InspectCabin("c1")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range anomalies {
		if a == battery.AnomalySOC {
			found = true
		}
	}
	if !found {
		t.Error("inspection should detect SOC anomaly")
	}
}

func TestAppLoadSharingOnCabinExit(t *testing.T) {
	app := New(testConfig(), nil)
	app.RegisterCabin(battery.Cabin{ID: "a", Capacity: 200, SOC: 100, Temperature: 25, Insulation: 5, Online: true})
	app.RegisterCabin(battery.Cabin{ID: "b", Capacity: 200, SOC: 100, Temperature: 25, Insulation: 5, Online: true})
	app.SetFleetDemand(400)
	// Both carry ~200 kW.
	// Take cabin a offline: b should carry the full 400 kW (Rule 4).
	if err := app.SetCabinOffline("a"); err != nil {
		t.Fatal(err)
	}
	cabins := app.ListCabins()
	for _, c := range cabins {
		if c.ID == "b" && (c.Output < 395 || c.Output > 405) {
			t.Errorf("cabin b output=%.1f, want ~400", c.Output)
		}
		if c.ID == "a" && c.Output != 0 {
			t.Errorf("offline cabin a output=%.1f, want 0", c.Output)
		}
	}
}

func TestAppWorkOrderEscalationIntegration(t *testing.T) {
	app := New(testConfig(), nil)
	app.RegisterCabin(battery.Cabin{ID: "c1", Capacity: 100, SOC: 80, Temperature: 60, Insulation: 5, Online: true})
	// Inspection finds high temperature; create an order.
	o, err := app.CreateOrder("c1", battery.AnomalyTemperature, workorder.SeverityUrgent)
	if err != nil {
		t.Fatal(err)
	}
	// Advance time past 4 hours.
	base := o.CreatedAt
	app.SetClock(func() time.Time { return base.Add(4*time.Hour + 10*time.Minute) })
	esc := app.CheckEscalations()
	if len(esc) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(esc))
	}
	// Escalated order cannot be accepted.
	_, err = app.AcceptOrder(o.ID)
	if err == nil {
		t.Error("expected error accepting escalated order")
	}
}

func TestAppConcurrentOrders(t *testing.T) {
	app := New(testConfig(), nil)
	app.RegisterCabin(battery.Cabin{ID: "c1", Capacity: 100, SOC: 80, Temperature: 25, Insulation: 5, Online: true})
	var wg sync.WaitGroup
	orders := make([]workorder.Order, 20)
	errs := make([]error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			orders[i], errs[i] = app.CreateOrder("c1", battery.AnomalyTemperature, workorder.SeverityWarning)
		}(i)
	}
	wg.Wait()
	seen := make(map[string]bool, 20)
	for i := 0; i < 20; i++ {
		if errs[i] != nil {
			t.Fatalf("goroutine %d error: %v", i, errs[i])
		}
		if seen[orders[i].ID] {
			t.Fatalf("duplicate order ID: %s", orders[i].ID)
		}
		seen[orders[i].ID] = true
	}
	if len(app.ListOrders()) != 20 {
		t.Errorf("expected 20 orders, got %d", len(app.ListOrders()))
	}
}

func TestAppSnapshotPersistence(t *testing.T) {
	path := t.TempDir() + "/state.json"
	app := New(testConfig(), store.New(path))
	app.RegisterCabin(battery.Cabin{ID: "c1", Capacity: 100, SOC: 80, Temperature: 25, Insulation: 5, Online: true})
	o, _ := app.CreateOrder("c1", battery.AnomalyTemperature, workorder.SeverityWarning)
	app.SetFleetDemand(100)
	if err := app.SaveSnapshot(); err != nil {
		t.Fatal(err)
	}
	// Create a fresh app and load from disk.
	app2 := New(testConfig(), store.New(path))
	if err := app2.LoadSnapshot(); err != nil {
		t.Fatal(err)
	}
	cabins := app2.ListCabins()
	if len(cabins) != 1 || cabins[0].ID != "c1" {
		t.Errorf("cabin not restored: %+v", cabins)
	}
	loaded, ok := app2.orders.Get(o.ID)
	if !ok {
		t.Fatal("order not restored")
	}
	if loaded.Status != workorder.StatusOpen {
		t.Errorf("expected open, got %s", loaded.Status)
	}
	// Next order should continue sequence.
	o2, _ := app2.CreateOrder("c1", battery.AnomalyTemperature, workorder.SeverityWarning)
	if o2.ID == o.ID {
		t.Error("new order should have different ID")
	}
}
