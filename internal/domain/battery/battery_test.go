package battery

import (
	"testing"
)

func TestCabinLowSOCAlarmAndPriorityCharge(t *testing.T) {
	f := NewFleet()
	f.Register(Cabin{ID: "c1", Capacity: 100, SOC: 10, Temperature: 25, Insulation: 2, Online: true}, 15)
	c, ok := f.Get("c1")
	if !ok {
		t.Fatal("cabin not found")
	}
	if !c.Charging {
		t.Error("low-SOC cabin should be priority charging")
	}
	if len(c.Alarms) == 0 {
		t.Error("low-SOC cabin should have an alarm")
	}
	// SOC recovers above threshold: charging should stop.
	if err := f.Update("c1", 80, 25, 2, 15); err != nil {
		t.Fatal(err)
	}
	c, _ = f.Get("c1")
	if c.Charging {
		t.Error("cabin above threshold should not be charging")
	}
	if len(c.Alarms) != 0 {
		t.Errorf("expected no alarms, got %v", c.Alarms)
	}
}

func TestCabinInspectAnomalies(t *testing.T) {
	limits := DefaultLimits()
	cases := []struct {
		name   string
		cabin  Cabin
		expect []AnomalyType
	}{
		{"healthy", Cabin{SOC: 50, Temperature: 25, Insulation: 5, Online: true}, nil},
		{"high_temp", Cabin{SOC: 50, Temperature: 60, Insulation: 5, Online: true}, []AnomalyType{AnomalyTemperature}},
		{"low_insulation", Cabin{SOC: 50, Temperature: 25, Insulation: 0.1, Online: true}, []AnomalyType{AnomalyInsulation}},
		{"low_soc", Cabin{SOC: 10, Temperature: 25, Insulation: 5, Online: true}, []AnomalyType{AnomalySOC}},
		{"multiple", Cabin{SOC: 10, Temperature: 60, Insulation: 0.1, Online: true}, []AnomalyType{AnomalyTemperature, AnomalyInsulation, AnomalySOC}},
		{"offline_low_soc", Cabin{SOC: 5, Temperature: 25, Insulation: 5, Online: false}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.cabin.Inspect(limits)
			if len(got) != len(tc.expect) {
				t.Fatalf("expected %d anomalies, got %d: %v", len(tc.expect), len(got), got)
			}
			for i, a := range got {
				if a != tc.expect[i] {
					t.Errorf("anomaly[%d]: expected %s, got %s", i, tc.expect[i], a)
				}
			}
		})
	}
}

func TestFleetLoadRedistribution(t *testing.T) {
	f := NewFleet()
	f.Register(Cabin{ID: "a", Capacity: 200, SOC: 100, Temperature: 25, Insulation: 5, Online: true}, 15)
	f.Register(Cabin{ID: "b", Capacity: 100, SOC: 100, Temperature: 25, Insulation: 5, Online: true}, 15)
	f.SetDemand(300, 15)
	a, _ := f.Get("a")
	b, _ := f.Get("b")
	// a has 2x capacity of b, so a should carry 2x the load.
	if a.Output < 195 || a.Output > 205 {
		t.Errorf("cabin a output = %.1f, want ~200", a.Output)
	}
	if b.Output < 95 || b.Output > 105 {
		t.Errorf("cabin b output = %.1f, want ~100", b.Output)
	}
	// Take cabin a offline: b should pick up the full demand.
	if err := f.SetOffline("a", 15); err != nil {
		t.Fatal(err)
	}
	b, _ = f.Get("b")
	if b.Output < 295 || b.Output > 305 {
		t.Errorf("after offline, cabin b output = %.1f, want ~300", b.Output)
	}
	a, _ = f.Get("a")
	if a.Output != 0 {
		t.Errorf("offline cabin output = %.1f, want 0", a.Output)
	}
}

func TestFleetLowSOCExcludesFromDischarge(t *testing.T) {
	f := NewFleet()
	f.Register(Cabin{ID: "a", Capacity: 100, SOC: 100, Temperature: 25, Insulation: 5, Online: true}, 15)
	f.Register(Cabin{ID: "b", Capacity: 100, SOC: 10, Temperature: 25, Insulation: 5, Online: true}, 15)
	f.SetDemand(200, 15)
	a, _ := f.Get("a")
	b, _ := f.Get("b")
	// b is in low-SOC alarm: it charges and does not discharge.
	if !b.Charging {
		t.Error("low-SOC cabin should be charging")
	}
	if b.Output != 0 {
		t.Errorf("low-SOC cabin output = %.1f, want 0", b.Output)
	}
	// a picks up the full demand.
	if a.Output < 195 || a.Output > 205 {
		t.Errorf("healthy cabin output = %.1f, want ~200", a.Output)
	}
}

func TestFleetSetOfflineIdempotent(t *testing.T) {
	f := NewFleet()
	f.Register(Cabin{ID: "x", Capacity: 100, SOC: 80, Temperature: 25, Insulation: 5, Online: true}, 15)
	f.SetDemand(100, 15)
	if err := f.SetOffline("x", 15); err != nil {
		t.Fatal(err)
	}
	// Calling again should not error (idempotent).
	if err := f.SetOffline("x", 15); err != nil {
		t.Errorf("second SetOffline should be idempotent, got %v", err)
	}
	c, _ := f.Get("x")
	if c.Online {
		t.Error("cabin should be offline")
	}
}

func TestFleetUpdateNotFound(t *testing.T) {
	f := NewFleet()
	err := f.Update("missing", 50, 25, 5, 15)
	if err == nil {
		t.Error("expected error for missing cabin")
	}
}
