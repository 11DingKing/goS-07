// Package battery models grid-forming energy storage battery cabins and the
// fleet-level rules that govern charge/discharge behaviour.
package battery

import (
	"fmt"
	"sync"
	"time"
)

// AnomalyType categorises inspection findings for a cabin.
type AnomalyType string

const (
	AnomalyTemperature AnomalyType = "temperature"
	AnomalyInsulation  AnomalyType = "insulation"
	AnomalySOC         AnomalyType = "soc"
)

// Cabin represents a single grid-forming energy storage battery cabin.
type Cabin struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Capacity    float64   `json:"capacity"`    // rated capacity in kWh
	SOC         float64   `json:"soc"`         // state of charge, percentage 0-100
	Temperature float64   `json:"temperature"` // degrees Celsius
	Insulation  float64   `json:"insulation"`  // insulation resistance in MOhm
	Online      bool      `json:"online"`
	Output      float64   `json:"output"`   // current real power output in kW
	Charging    bool      `json:"charging"` // priority charge flag (Rule 1)
	Alarms      []string  `json:"alarms"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// InspectLimits defines thresholds for anomaly detection during inspection.
type InspectLimits struct {
	MaxTemperature float64 // °C
	MinInsulation  float64 // MOhm
	MinSOC         float64 // percentage
}

// DefaultLimits returns conservative inspection thresholds.
func DefaultLimits() InspectLimits {
	return InspectLimits{MaxTemperature: 55.0, MinInsulation: 0.5, MinSOC: 15.0}
}

// HasLowSOC reports whether the cabin triggers the low-SOC alarm (Rule 1).
func (c *Cabin) HasLowSOC(threshold float64) bool {
	return c.Online && c.SOC < threshold
}

// Inspect returns anomaly types based on current sensor readings.
func (c *Cabin) Inspect(limits InspectLimits) []AnomalyType {
	var out []AnomalyType
	if c.Temperature > limits.MaxTemperature {
		out = append(out, AnomalyTemperature)
	}
	if c.Insulation < limits.MinInsulation {
		out = append(out, AnomalyInsulation)
	}
	if c.HasLowSOC(limits.MinSOC) {
		out = append(out, AnomalySOC)
	}
	return out
}

// remainingUsable is the usable energy in kWh based on SOC.
func (c *Cabin) remainingUsable() float64 {
	return c.Capacity * (c.SOC / 100.0)
}

// clone returns an independent copy of the cabin. The Alarms slice is copied
// as well so a value handed to a caller can never observe or be affected by
// later in-place rewrites of the fleet's own alarm list.
func (c *Cabin) clone() Cabin {
	out := *c
	if c.Alarms != nil {
		out.Alarms = append(make([]string, 0, len(c.Alarms)), c.Alarms...)
	}
	return out
}

// Snapshot is a serialisable copy of a cabin's state.
type CabinSnapshot struct {
	Cabin
}

// Fleet manages the set of battery cabins and enforces fleet-wide rules.
type Fleet struct {
	mu     sync.Mutex
	cabins map[string]*Cabin
	demand float64 // total power demand in kW to be served by online cabins
}

// NewFleet creates an empty fleet.
func NewFleet() *Fleet {
	return &Fleet{cabins: make(map[string]*Cabin)}
}

// Register adds or replaces a cabin in the fleet.
func (f *Fleet) Register(c Cabin, socThreshold float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c.UpdatedAt = time.Now()
	c.Alarms = c.Alarms[:0]
	if c.HasLowSOC(socThreshold) {
		c.Alarms = append(c.Alarms, fmt.Sprintf("low_soc:%.1f", c.SOC))
		c.Charging = true
	}
	f.cabins[c.ID] = &c
	f.rebalanceLocked(socThreshold)
}

// Update applies new sensor readings to a cabin and recomputes alarms and
// charging state (Rule 1).
func (f *Fleet) Update(id string, soc, temp, insulation float64, socThreshold float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cabins[id]
	if !ok {
		return fmt.Errorf("cabin %s not found", id)
	}
	c.SOC = soc
	c.Temperature = temp
	c.Insulation = insulation
	c.UpdatedAt = time.Now()
	c.Alarms = c.Alarms[:0]
	if c.HasLowSOC(socThreshold) {
		c.Alarms = append(c.Alarms, fmt.Sprintf("low_soc:%.1f", soc))
		c.Charging = true // Rule 1: priority charge
	} else {
		c.Charging = false
	}
	f.rebalanceLocked(socThreshold)
	return nil
}

// SetOffline takes a cabin out of service and redistributes its share of the
// load among the remaining online cabins (Rule 4). The operation is idempotent.
func (f *Fleet) SetOffline(id string, socThreshold float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cabins[id]
	if !ok {
		return fmt.Errorf("cabin %s not found", id)
	}
	if !c.Online {
		return nil
	}
	c.Online = false
	c.Output = 0
	c.Charging = false
	c.Alarms = c.Alarms[:0]
	f.rebalanceLocked(socThreshold)
	return nil
}

// SetOnline brings a cabin back into service and rebalances the fleet.
func (f *Fleet) SetOnline(id string, socThreshold float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cabins[id]
	if !ok {
		return fmt.Errorf("cabin %s not found", id)
	}
	c.Online = true
	if c.HasLowSOC(socThreshold) {
		c.Charging = true
		c.Alarms = append(c.Alarms, fmt.Sprintf("low_soc:%.1f", c.SOC))
	}
	f.rebalanceLocked(socThreshold)
	return nil
}

// SetDemand sets the total power demand for the fleet and rebalances.
func (f *Fleet) SetDemand(kW float64, socThreshold float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if kW < 0 {
		kW = 0
	}
	f.demand = kW
	f.rebalanceLocked(socThreshold)
}

// Get returns a copy of a cabin.
func (f *Fleet) Get(id string) (Cabin, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.cabins[id]
	if !ok {
		return Cabin{}, false
	}
	return c.clone(), true
}

// List returns copies of all cabins.
func (f *Fleet) List() []Cabin {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Cabin, 0, len(f.cabins))
	for _, c := range f.cabins {
		out = append(out, c.clone())
	}
	return out
}

// OnlineCount returns the number of online cabins.
func (f *Fleet) OnlineCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.cabins {
		if c.Online {
			n++
		}
	}
	return n
}

// rebalanceLocked redistributes the fleet's total demand among online cabins
// proportionally to their remaining usable capacity (Rule 4). Cabins in a
// low-SOC alarm state do not discharge; they charge instead (Rule 1).
func (f *Fleet) rebalanceLocked(socThreshold float64) {
	var totalUsable float64
	for _, c := range f.cabins {
		if c.Online && !c.HasLowSOC(socThreshold) {
			totalUsable += c.remainingUsable()
		}
	}
	for _, c := range f.cabins {
		switch {
		case !c.Online:
			c.Output = 0
		case c.HasLowSOC(socThreshold):
			c.Output = 0
		case totalUsable == 0:
			c.Output = 0
		default:
			c.Output = f.demand * (c.remainingUsable() / totalUsable)
		}
	}
}

// Snapshot returns a serialisable copy of the fleet state.
func (f *Fleet) Snapshot() ([]Cabin, float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Cabin, 0, len(f.cabins))
	for _, c := range f.cabins {
		out = append(out, c.clone())
	}
	return out, f.demand
}

// Restore replaces the fleet state from a snapshot.
func (f *Fleet) Restore(cabins []Cabin, demand float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cabins = make(map[string]*Cabin, len(cabins))
	for i := range cabins {
		c := cabins[i].clone()
		f.cabins[c.ID] = &c
	}
	f.demand = demand
}
