package workorder

import (
	"testing"
	"time"

	"ejina-microgrid/internal/domain/battery"
)

func TestOrderLifecycle(t *testing.T) {
	book := NewBook(4 * time.Hour)
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	o, err := book.Create("c1", battery.AnomalyTemperature, SeverityUrgent, now)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != StatusOpen {
		t.Fatalf("expected open, got %s", o.Status)
	}
	// Accept
	accepted, err := book.Accept(o.ID, now.Add(30*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status != StatusAccepted {
		t.Fatalf("expected accepted, got %s", accepted.Status)
	}
	// Resolve
	resolved, err := book.Resolve(o.ID, "replaced thermal sensor", now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != StatusResolved {
		t.Fatalf("expected resolved, got %s", resolved.Status)
	}
	if resolved.Result != "replaced thermal sensor" {
		t.Errorf("unexpected result: %s", resolved.Result)
	}
}

func TestOrderEscalationAfterDeadline(t *testing.T) {
	book := NewBook(4 * time.Hour)
	created := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	o, _ := book.Create("c2", battery.AnomalyInsulation, SeverityWarning, created)
	// 3h50m: not yet escalated.
	esc := book.CheckEscalations(created.Add(3*time.Hour + 50*time.Minute))
	if len(esc) != 0 {
		t.Fatalf("expected no escalation, got %d", len(esc))
	}
	// 4h1m: escalated.
	esc = book.CheckEscalations(created.Add(4*time.Hour + 1*time.Minute))
	if len(esc) != 1 {
		t.Fatalf("expected 1 escalation, got %d", len(esc))
	}
	if esc[0].ID != o.ID {
		t.Errorf("wrong order escalated: %s", esc[0].ID)
	}
	if !esc[0].Escalated {
		t.Error("order should be marked escalated")
	}
	// Escalated order cannot be accepted.
	_, err := book.Accept(o.ID, created.Add(5*time.Hour))
	if err == nil {
		t.Error("expected error accepting escalated order")
	}
}

func TestOrderIdempotentAccept(t *testing.T) {
	book := NewBook(4 * time.Hour)
	now := time.Now()
	o, _ := book.Create("c3", battery.AnomalySOC, SeverityCritical, now)
	first, err := book.Accept(o.ID, now.Add(1*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	// Accept again with a different timestamp — should return the original.
	second, err := book.Accept(o.ID, now.Add(5*time.Minute))
	if err != nil {
		t.Fatalf("idempotent accept should not error: %v", err)
	}
	if second.AcceptedAt != first.AcceptedAt {
		t.Error("idempotent accept should preserve original AcceptedAt")
	}
}

func TestOrderResolveBeforeAccept(t *testing.T) {
	book := NewBook(4 * time.Hour)
	now := time.Now()
	o, _ := book.Create("c4", battery.AnomalyTemperature, SeverityWarning, now)
	_, err := book.Resolve(o.ID, "done", now.Add(1*time.Hour))
	if err == nil {
		t.Error("expected error resolving an unaccepted order")
	}
}

func TestBookRestorePreservesSeq(t *testing.T) {
	book := NewBook(4 * time.Hour)
	now := time.Now()
	book.Create("c5", battery.AnomalyTemperature, SeverityWarning, now)
	book.Create("c6", battery.AnomalyTemperature, SeverityWarning, now)
	snap := book.Snapshot()

	book2 := NewBook(4 * time.Hour)
	book2.Restore(snap)
	// Next created order should continue the sequence.
	o, _ := book2.Create("c7", battery.AnomalyTemperature, SeverityWarning, now)
	if o.ID != "WO-0003" {
		t.Errorf("expected WO-0003, got %s", o.ID)
	}
}
