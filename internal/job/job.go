// Package job runs background tasks for the dispatch service: work order
// escalation checks, controller heartbeat monitoring with automatic failover,
// periodic state snapshots, and off-grid transition deadline monitoring.
package job

import (
	"context"
	"log"
	"sync"
	"time"

	"ejina-microgrid/internal/app"
)

// Scheduler runs a set of periodic jobs. Each job runs in its own goroutine
// and respects context cancellation for graceful shutdown.
type Scheduler struct {
	app  *app.App
	jobs []periodic
	wg   sync.WaitGroup
}

type periodic struct {
	name     string
	interval time.Duration
	fn       func(context.Context, *app.App)
}

// New creates a scheduler wired to the given App.
func New(a *app.App) *Scheduler {
	return &Scheduler{app: a}
}

// WithDefaults registers the standard set of background jobs using the App's
// configuration.
func (s *Scheduler) WithDefaults() *Scheduler {
	cfg := s.app.Config()
	s.jobs = append(s.jobs,
		periodic{name: "escalation", interval: cfg.EscalationCheckInterval, fn: runEscalation},
		periodic{name: "health", interval: cfg.HealthCheckInterval, fn: runHealth},
		periodic{name: "snapshot", interval: cfg.SnapshotInterval, fn: runSnapshot},
		periodic{name: "grid-transition", interval: cfg.HealthCheckInterval, fn: runGridTransition},
	)
	return s
}

// Start launches all registered jobs. It returns immediately. The jobs run for
// the lifetime of ctx: each fires on its own interval while ctx is active and
// stops as soon as ctx is cancelled. Call Wait to block until every job
// goroutine has returned; in main the same context backs the signal handler,
// so cancelling it (SIGINT/SIGTERM) both begins and is awaited by shutdown.
func (s *Scheduler) Start(ctx context.Context) {
	for _, j := range s.jobs {
		s.wg.Add(1)
		go s.runLoop(ctx, j)
	}
}

// Wait blocks until all jobs have stopped.
func (s *Scheduler) Wait() {
	s.wg.Wait()
}

func (s *Scheduler) runLoop(ctx context.Context, j periodic) {
	defer s.wg.Done()
	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()
	log.Printf("[job:%s] started (interval=%s)", j.name, j.interval)
	for {
		select {
		case <-ctx.Done():
			log.Printf("[job:%s] stopped", j.name)
			return
		case <-ticker.C:
			j.fn(ctx, s.app)
		}
	}
}

func runEscalation(ctx context.Context, a *app.App) {
	escalated := a.CheckEscalations()
	for _, o := range escalated {
		log.Printf("[job:escalation] order %s escalated to station chief (cabin=%s)", o.ID, o.CabinID)
	}
}

func runHealth(ctx context.Context, a *app.App) {
	if a.CheckControllerHealth() {
		log.Printf("[job:health] controller failover triggered, backup took over with snapshot replay")
	}
}

func runSnapshot(ctx context.Context, a *app.App) {
	if err := a.SaveSnapshot(); err != nil {
		log.Printf("[job:snapshot] save failed: %v", err)
	}
}

func runGridTransition(ctx context.Context, a *app.App) {
	if a.CheckGridTransition() {
		log.Printf("[job:grid-transition] WARNING: off-grid transition deadline exceeded")
	}
}
