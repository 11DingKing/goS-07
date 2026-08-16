package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ejina-microgrid/internal/app"
	"ejina-microgrid/internal/config"
	"ejina-microgrid/internal/server"
	"ejina-microgrid/internal/store"
)

// TestShippedConfigIsValid verifies that the config.json shipped in the repo
// (and copied into the Docker image) parses correctly and matches the values
// documented in the README: listen port 49495, 4h escalation, 15% SOC alarm.
// This keeps the Dockerfile, README, and runtime configuration consistent.
func TestShippedConfigIsValid(t *testing.T) {
	cfg, err := config.Load("config.json")
	if err != nil {
		t.Fatalf("shipped config.json failed to load: %v", err)
	}
	if cfg.ListenAddr != ":49495" {
		t.Errorf("ListenAddr = %q, want :49495", cfg.ListenAddr)
	}
	if cfg.EscalationTimeout != 4*time.Hour {
		t.Errorf("EscalationTimeout = %v, want 4h", cfg.EscalationTimeout)
	}
	if cfg.SOCAlarmThreshold != 15.0 {
		t.Errorf("SOCAlarmThreshold = %v, want 15", cfg.SOCAlarmThreshold)
	}
	if cfg.HeartbeatTimeout != 10*time.Second {
		t.Errorf("HeartbeatTimeout = %v, want 10s", cfg.HeartbeatTimeout)
	}
}

// TestServerWiringHealth mirrors main()'s wiring (config → store → app →
// server) and confirms the /health endpoint responds 200. It is an end-to-end
// smoke test of the production code path without external services.
func TestServerWiringHealth(t *testing.T) {
	cfg, err := config.Load("config.json")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	cfg.StorePath = t.TempDir() + "/state.json"
	st := store.New(cfg.StorePath)
	application := app.New(cfg, st)
	srv := server.New(application)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
