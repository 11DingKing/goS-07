// Package config loads runtime configuration for the Ejina microgrid
// dispatch service from a JSON file with environment variable overrides.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Config holds all tunable parameters for the dispatch service.
type Config struct {
	ListenAddr              string        `json:"listen_addr"`
	StorePath               string        `json:"store_path"`
	EscalationTimeout       time.Duration `json:"-"`
	OffGridDeadline         time.Duration `json:"-"`
	HeartbeatTimeout        time.Duration `json:"-"`
	FailoverGrace           time.Duration `json:"-"`
	SOCAlarmThreshold       float64       `json:"soc_alarm_threshold"`
	SnapshotInterval        time.Duration `json:"-"`
	HealthCheckInterval     time.Duration `json:"-"`
	EscalationCheckInterval time.Duration `json:"-"`
	BlackStartTransition    time.Duration `json:"-"`
	InspectionTempLimit     float64       `json:"inspection_temp_limit"`
	InsulationMinMOhm       float64       `json:"insulation_min_mohm"`
}

type fileConfig struct {
	ListenAddr              string  `json:"listen_addr"`
	StorePath               string  `json:"store_path"`
	EscalationTimeout       string  `json:"escalation_timeout"`
	OffGridDeadline         string  `json:"off_grid_deadline"`
	HeartbeatTimeout        string  `json:"heartbeat_timeout"`
	FailoverGrace           string  `json:"failover_grace"`
	SOCAlarmThreshold       float64 `json:"soc_alarm_threshold"`
	SnapshotInterval        string  `json:"snapshot_interval"`
	HealthCheckInterval     string  `json:"health_check_interval"`
	EscalationCheckInterval string  `json:"escalation_check_interval"`
	BlackStartTransition    string  `json:"black_start_transition"`
	InspectionTempLimit     float64 `json:"inspection_temp_limit"`
	InsulationMinMOhm       float64 `json:"insulation_min_mohm"`
}

// Default returns production defaults matching the dispatch duty rules.
func Default() Config {
	return Config{
		ListenAddr:              ":49495",
		StorePath:               "data/state.json",
		EscalationTimeout:       4 * time.Hour,
		OffGridDeadline:         90 * time.Second,
		HeartbeatTimeout:        10 * time.Second,
		FailoverGrace:           10 * time.Second,
		SOCAlarmThreshold:       15.0,
		SnapshotInterval:        5 * time.Second,
		HealthCheckInterval:     1 * time.Second,
		EscalationCheckInterval: 30 * time.Second,
		BlackStartTransition:    90 * time.Second,
		InspectionTempLimit:     55.0,
		InsulationMinMOhm:       0.5,
	}
}

// Load reads a JSON config file (if present) and applies environment variable
// overrides. A missing file is not an error; defaults are used.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return applyEnv(cfg), nil
		}
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	if fc.ListenAddr != "" {
		cfg.ListenAddr = fc.ListenAddr
	}
	if fc.StorePath != "" {
		cfg.StorePath = fc.StorePath
	}
	if d, err := time.ParseDuration(fc.EscalationTimeout); err == nil && d > 0 {
		cfg.EscalationTimeout = d
	} else if fc.EscalationTimeout != "" {
		return cfg, fmt.Errorf("invalid escalation_timeout: %q", fc.EscalationTimeout)
	}
	if d, err := time.ParseDuration(fc.OffGridDeadline); err == nil && d > 0 {
		cfg.OffGridDeadline = d
	} else if fc.OffGridDeadline != "" {
		return cfg, fmt.Errorf("invalid off_grid_deadline: %q", fc.OffGridDeadline)
	}
	if d, err := time.ParseDuration(fc.HeartbeatTimeout); err == nil && d > 0 {
		cfg.HeartbeatTimeout = d
	} else if fc.HeartbeatTimeout != "" {
		return cfg, fmt.Errorf("invalid heartbeat_timeout: %q", fc.HeartbeatTimeout)
	}
	if d, err := time.ParseDuration(fc.FailoverGrace); err == nil && d > 0 {
		cfg.FailoverGrace = d
	} else if fc.FailoverGrace != "" {
		return cfg, fmt.Errorf("invalid failover_grace: %q", fc.FailoverGrace)
	}
	if d, err := time.ParseDuration(fc.SnapshotInterval); err == nil && d > 0 {
		cfg.SnapshotInterval = d
	} else if fc.SnapshotInterval != "" {
		return cfg, fmt.Errorf("invalid snapshot_interval: %q", fc.SnapshotInterval)
	}
	if d, err := time.ParseDuration(fc.HealthCheckInterval); err == nil && d > 0 {
		cfg.HealthCheckInterval = d
	} else if fc.HealthCheckInterval != "" {
		return cfg, fmt.Errorf("invalid health_check_interval: %q", fc.HealthCheckInterval)
	}
	if d, err := time.ParseDuration(fc.EscalationCheckInterval); err == nil && d > 0 {
		cfg.EscalationCheckInterval = d
	} else if fc.EscalationCheckInterval != "" {
		return cfg, fmt.Errorf("invalid escalation_check_interval: %q", fc.EscalationCheckInterval)
	}
	if d, err := time.ParseDuration(fc.BlackStartTransition); err == nil && d > 0 {
		cfg.BlackStartTransition = d
	} else if fc.BlackStartTransition != "" {
		return cfg, fmt.Errorf("invalid black_start_transition: %q", fc.BlackStartTransition)
	}
	if fc.SOCAlarmThreshold > 0 {
		cfg.SOCAlarmThreshold = fc.SOCAlarmThreshold
	}
	if fc.InspectionTempLimit > 0 {
		cfg.InspectionTempLimit = fc.InspectionTempLimit
	}
	if fc.InsulationMinMOhm > 0 {
		cfg.InsulationMinMOhm = fc.InsulationMinMOhm
	}
	return applyEnv(cfg), nil
}

func applyEnv(cfg Config) Config {
	if v := os.Getenv("EJINA_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("EJINA_STORE_PATH"); v != "" {
		cfg.StorePath = v
	}
	if v := os.Getenv("EJINA_SOC_THRESHOLD"); v != "" {
		if f, err := parseFloat(v); err == nil {
			cfg.SOCAlarmThreshold = f
		}
	}
	return cfg
}

func parseFloat(s string) (float64, error) {
	var f float64
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
