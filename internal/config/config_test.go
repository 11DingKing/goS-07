package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultMatchesProductionRules(t *testing.T) {
	cfg := Default()
	if cfg.ListenAddr != ":49495" {
		t.Errorf("ListenAddr = %q, want :49495", cfg.ListenAddr)
	}
	if cfg.SOCAlarmThreshold != 15.0 {
		t.Errorf("SOCAlarmThreshold = %v, want 15", cfg.SOCAlarmThreshold)
	}
	if cfg.EscalationTimeout != 4*time.Hour {
		t.Errorf("EscalationTimeout = %v, want 4h", cfg.EscalationTimeout)
	}
	if cfg.HeartbeatTimeout != 10*time.Second {
		t.Errorf("HeartbeatTimeout = %v, want 10s", cfg.HeartbeatTimeout)
	}
	for _, d := range []time.Duration{
		cfg.OffGridDeadline, cfg.FailoverGrace, cfg.SnapshotInterval,
		cfg.HealthCheckInterval, cfg.EscalationCheckInterval, cfg.BlackStartTransition,
	} {
		if d <= 0 {
			t.Errorf("rule-relevant duration must be positive, got %v", d)
		}
	}
}

func TestLoadValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	content := `{
  "listen_addr": ":9999",
  "store_path": "/tmp/state.json",
  "escalation_timeout": "2h",
  "off_grid_deadline": "60s",
  "heartbeat_timeout": "5s",
  "failover_grace": "8s",
  "soc_alarm_threshold": 20.0,
  "snapshot_interval": "3s",
  "health_check_interval": "2s",
  "escalation_check_interval": "15s",
  "black_start_transition": "120s",
  "inspection_temp_limit": 50.0,
  "insulation_min_mohm": 1.0
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != ":9999" {
		t.Errorf("ListenAddr = %q, want :9999", cfg.ListenAddr)
	}
	if cfg.StorePath != "/tmp/state.json" {
		t.Errorf("StorePath = %q", cfg.StorePath)
	}
	if cfg.EscalationTimeout != 2*time.Hour {
		t.Errorf("EscalationTimeout = %v, want 2h", cfg.EscalationTimeout)
	}
	if cfg.OffGridDeadline != 60*time.Second {
		t.Errorf("OffGridDeadline = %v, want 60s", cfg.OffGridDeadline)
	}
	if cfg.SOCAlarmThreshold != 20.0 {
		t.Errorf("SOCAlarmThreshold = %v, want 20", cfg.SOCAlarmThreshold)
	}
	if cfg.InspectionTempLimit != 50.0 {
		t.Errorf("InspectionTempLimit = %v, want 50", cfg.InspectionTempLimit)
	}
	if cfg.InsulationMinMOhm != 1.0 {
		t.Errorf("InsulationMinMOhm = %v, want 1", cfg.InsulationMinMOhm)
	}
}

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if cfg.ListenAddr != ":49495" {
		t.Errorf("default ListenAddr = %q, want :49495", cfg.ListenAddr)
	}
	if cfg.EscalationTimeout != 4*time.Hour {
		t.Errorf("default EscalationTimeout = %v, want 4h", cfg.EscalationTimeout)
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"escalation_timeout":"not-a-duration"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for invalid escalation_timeout")
	}
}

func TestLoadZeroDurationRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"heartbeat_timeout":"0s"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for zero heartbeat_timeout")
	}
}

func TestLoadMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"listen_addr":":1111"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EJINA_LISTEN_ADDR", ":7777")
	t.Setenv("EJINA_STORE_PATH", "/custom/state.json")
	t.Setenv("EJINA_SOC_THRESHOLD", "25")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ListenAddr != ":7777" {
		t.Errorf("env ListenAddr = %q, want :7777", cfg.ListenAddr)
	}
	if cfg.StorePath != "/custom/state.json" {
		t.Errorf("env StorePath = %q", cfg.StorePath)
	}
	if cfg.SOCAlarmThreshold != 25.0 {
		t.Errorf("env SOCAlarmThreshold = %v, want 25", cfg.SOCAlarmThreshold)
	}
}
