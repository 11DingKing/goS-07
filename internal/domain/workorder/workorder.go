// Package workorder models anomaly work orders created during battery cabin
// inspections, including acceptance, resolution, and the four-hour escalation
// rule (Rule 3).
package workorder

import (
	"fmt"
	"sync"
	"time"

	"ejina-microgrid/internal/domain/battery"
)

// Status represents the lifecycle state of a work order.
type Status string

const (
	StatusOpen      Status = "open"
	StatusAccepted  Status = "accepted"
	StatusResolved  Status = "resolved"
	StatusEscalated Status = "escalated"
)

// Severity classifies the urgency of an anomaly.
type Severity string

const (
	SeverityWarning  Severity = "warning"
	SeverityUrgent   Severity = "urgent"
	SeverityCritical Severity = "critical"
)

// Order is an anomaly work order.
type Order struct {
	ID         string              `json:"id"`
	CabinID    string              `json:"cabin_id"`
	Type       battery.AnomalyType `json:"type"`
	Severity   Severity            `json:"severity"`
	Status     Status              `json:"status"`
	Escalated  bool                `json:"escalated"`
	CreatedAt  time.Time           `json:"created_at"`
	AcceptedAt time.Time           `json:"accepted_at,omitempty"`
	ResolvedAt time.Time           `json:"resolved_at,omitempty"`
	AcceptBy   string              `json:"accept_by,omitempty"`
	Result     string              `json:"result,omitempty"`
}

// New creates a work order with the given parameters.
func New(id, cabinID string, anomalyType battery.AnomalyType, severity Severity, now time.Time, escalationTimeout time.Duration) Order {
	return Order{
		ID:        id,
		CabinID:   cabinID,
		Type:      anomalyType,
		Severity:  severity,
		Status:    StatusOpen,
		CreatedAt: now,
		AcceptBy:  now.Add(escalationTimeout).Format(time.RFC3339),
	}
}

// Book manages the collection of work orders.
type Book struct {
	mu                sync.Mutex
	orders            map[string]*Order
	seq               int
	escalationTimeout time.Duration
}

// NewBook creates a work order book with the given escalation timeout.
func NewBook(escalationTimeout time.Duration) *Book {
	return &Book{
		orders:            make(map[string]*Order),
		escalationTimeout: escalationTimeout,
	}
}

// Create adds a new work order and returns a copy.
func (b *Book) Create(cabinID string, anomalyType battery.AnomalyType, severity Severity, now time.Time) (Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	id := fmt.Sprintf("WO-%04d", b.seq)
	o := New(id, cabinID, anomalyType, severity, now, b.escalationTimeout)
	b.orders[id] = &o
	return o, nil
}

// Accept transitions an open order to accepted. Accepting an already-accepted
// order is idempotent and returns the current state.
func (b *Book) Accept(id string, now time.Time) (Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.orders[id]
	if !ok {
		return Order{}, fmt.Errorf("order %s not found", id)
	}
	switch o.Status {
	case StatusAccepted:
		return *o, nil // idempotent
	case StatusResolved:
		return Order{}, fmt.Errorf("order %s already resolved", id)
	case StatusEscalated:
		return Order{}, fmt.Errorf("order %s escalated, cannot accept", id)
	case StatusOpen:
		o.Status = StatusAccepted
		o.AcceptedAt = now
		return *o, nil
	default:
		return Order{}, fmt.Errorf("cannot accept order in status %s", o.Status)
	}
}

// Resolve transitions an accepted order to resolved.
func (b *Book) Resolve(id, result string, now time.Time) (Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.orders[id]
	if !ok {
		return Order{}, fmt.Errorf("order %s not found", id)
	}
	switch o.Status {
	case StatusResolved:
		return *o, nil // idempotent
	case StatusAccepted:
		o.Status = StatusResolved
		o.ResolvedAt = now
		o.Result = result
		return *o, nil
	case StatusOpen:
		return Order{}, fmt.Errorf("order %s not yet accepted", id)
	default:
		return Order{}, fmt.Errorf("cannot resolve order in status %s", o.Status)
	}
}

// CheckEscalations scans for open orders that have exceeded the escalation
// timeout and marks them as escalated (Rule 3). Returns the escalated orders.
func (b *Book) CheckEscalations(now time.Time) []Order {
	b.mu.Lock()
	defer b.mu.Unlock()
	var escalated []Order
	for _, o := range b.orders {
		if o.Status != StatusOpen {
			continue
		}
		if now.Sub(o.CreatedAt) > b.escalationTimeout {
			o.Status = StatusEscalated
			o.Escalated = true
			escalated = append(escalated, *o)
		}
	}
	return escalated
}

// Get returns a copy of an order.
func (b *Book) Get(id string) (Order, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.orders[id]
	if !ok {
		return Order{}, false
	}
	return *o, true
}

// List returns copies of all orders.
func (b *Book) List() []Order {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Order, 0, len(b.orders))
	for _, o := range b.orders {
		out = append(out, *o)
	}
	return out
}

// Snapshot returns a serialisable copy of all orders.
func (b *Book) Snapshot() []Order {
	return b.List()
}

// Restore replaces the book state from a snapshot.
func (b *Book) Restore(orders []Order) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.orders = make(map[string]*Order, len(orders))
	maxSeq := 0
	for i := range orders {
		o := orders[i]
		b.orders[o.ID] = &o
		var n int
		if _, err := fmt.Sscanf(o.ID, "WO-%04d", &n); err == nil && n > maxSeq {
			maxSeq = n
		}
	}
	b.seq = maxSeq
}
