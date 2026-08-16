package job

import (
	"context"
	"os"
	"testing"
	"time"

	"ejina-microgrid/internal/app"
	"ejina-microgrid/internal/config"
	"ejina-microgrid/internal/domain/battery"
	"ejina-microgrid/internal/store"
)

func testApp(t *testing.T) *app.App {
	t.Helper()
	cfg := config.Default()
	cfg.StorePath = ""
	return app.New(cfg, nil)
}

func TestWithDefaultsRegistersJobs(t *testing.T) {
	s := New(testApp(t)).WithDefaults()
	if len(s.jobs) != 4 {
		t.Fatalf("expected 4 jobs, got %d", len(s.jobs))
	}
	names := make(map[string]bool, len(s.jobs))
	for _, j := range s.jobs {
		names[j.name] = true
		if j.interval <= 0 {
			t.Errorf("job %q has non-positive interval %v", j.name, j.interval)
		}
		if j.fn == nil {
			t.Errorf("job %q has nil fn", j.name)
		}
	}
	for _, want := range []string{"escalation", "health", "snapshot", "grid-transition"} {
		if !names[want] {
			t.Errorf("missing job %q", want)
		}
	}
}

func TestSchedulerStartStop(t *testing.T) {
	s := New(testApp(t)).WithDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	cancel()
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not stop within 2s")
	}
}

func TestRunEscalationEscalatesStaleOrder(t *testing.T) {
	a := testApp(t)
	a.RegisterCabin(battery.Cabin{ID: "c1", Capacity: 100, SOC: 80, Temperature: 25, Insulation: 5, Online: true})
	o, _ := a.CreateOrder("c1", battery.AnomalyTemperature, "warning")
	base := o.CreatedAt
	// Advance past the 4h escalation deadline (Rule 3).
	a.SetClock(func() time.Time { return base.Add(4*time.Hour + 5*time.Minute) })
	runEscalation(context.Background(), a)
	orders := a.ListOrders()
	for _, or := range orders {
		if or.ID == o.ID {
			if !or.Escalated {
				t.Error("order should be escalated after runEscalation")
			}
			return
		}
	}
	t.Fatalf("order %s not found after escalation", o.ID)
}

func TestRunHealthTriggersFailover(t *testing.T) {
	a := testApp(t)
	a.RegisterCabin(battery.Cabin{ID: "c1", Capacity: 100, SOC: 80, Temperature: 25, Insulation: 5, Online: true})
	a.SetFleetDemand(100)
	// Persist a snapshot so replay restores valid operational state (Rule 5).
	if err := a.SaveSnapshot(); err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	a.SetClock(func() time.Time { return base.Add(20 * time.Second) })
	runHealth(context.Background(), a)
	snap := a.ControllerSnapshot()
	if snap.ActiveID != "backup-01" {
		t.Errorf("expected backup-01 active after failover, got %s", snap.ActiveID)
	}
	if snap.FailoverCount != 1 {
		t.Errorf("expected 1 failover, got %d", snap.FailoverCount)
	}
}

func TestRunSnapshotPersists(t *testing.T) {
	path := t.TempDir() + "/state.json"
	cfg := config.Default()
	cfg.StorePath = path
	a := app.New(cfg, store.New(path))
	a.RegisterCabin(battery.Cabin{ID: "c1", Capacity: 100, SOC: 80, Temperature: 25, Insulation: 5, Online: true})
	runSnapshot(context.Background(), a)
	if _, err := os.ReadFile(path); err != nil {
		t.Errorf("snapshot not persisted to %s: %v", path, err)
	}
}

func TestRunGridTransitionNoDeadline(t *testing.T) {
	a := testApp(t)
	// Grid is connected: no deadline exceeded; must not panic.
	runGridTransition(context.Background(), a)
}

func TestRunGridTransitionAfterDeadline(t *testing.T) {
	a := testApp(t)
	base := time.Now()
	a.SetClock(func() time.Time { return base })
	if err := a.LoseExternalGrid(); err != nil {
		t.Fatal(err)
	}
	// Advance past the 90s off-grid deadline.
	a.SetClock(func() time.Time { return base.Add(120 * time.Second) })
	if !a.CheckGridTransition() {
		t.Error("expected grid transition deadline exceeded")
	}
	// runGridTransition must not panic when the deadline is exceeded.
	runGridTransition(context.Background(), a)
}
