package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// DetectorConfig is one pluggable detector entry in warden.yaml.
type DetectorConfig struct {
	ID         string   `yaml:"id" json:"id"`
	Type       string   `yaml:"type" json:"type"` // threshold | zscore | low_support
	Metric     string   `yaml:"metric,omitempty" json:"metric,omitempty"`
	Warning    *float64 `yaml:"warning,omitempty" json:"warning,omitempty"`
	Critical   *float64 `yaml:"critical,omitempty" json:"critical,omitempty"`
	MinSupport int      `yaml:"min_support,omitempty" json:"min_support,omitempty"`
	Direction  string   `yaml:"direction,omitempty" json:"direction,omitempty"` // above | below
	AbsWarning *float64 `yaml:"abs_warning,omitempty" json:"abs_warning,omitempty"`
	ZWarning   *float64 `yaml:"z_warning,omitempty" json:"z_warning,omitempty"`
	Severity   string   `yaml:"severity,omitempty" json:"severity,omitempty"` // low_support
}

// LegacyThresholds maps the fatia-1 flat thresholds block (still accepted).
type LegacyThresholds struct {
	HighMAPEWarning  *float64 `yaml:"high_mape_warning"`
	HighMAPECritical *float64 `yaml:"high_mape_critical"`
	MinSupport       *int     `yaml:"min_support"`
	BiasAbsWarning   *float64 `yaml:"bias_abs_warning"`
	BiasZWarning     *float64 `yaml:"bias_z_warning"`
	CoverageWarning  *float64 `yaml:"coverage_warning"`
}

type WardenConfig struct {
	SchemaVersion    int              `yaml:"schema_version" json:"schema_version"`
	DataDir          string           `yaml:"data_dir" json:"data_dir"`
	IncidentsDir     string           `yaml:"incidents_dir" json:"incidents_dir"`
	MetricsFilename  string           `yaml:"metrics_filename" json:"metrics_filename"`
	BaselineFilename string           `yaml:"baseline_filename" json:"baseline_filename"`
	Detectors        []DetectorConfig `yaml:"detectors" json:"detectors"`
	// Thresholds is legacy; if present and Detectors empty, converted on load.
	Thresholds *LegacyThresholds `yaml:"thresholds,omitempty" json:"thresholds,omitempty"`
}

func f64(v float64) *float64 { return &v }

// DefaultDetectors preserves HighMAPE / BiasShift / LowSupport / CoverageBreak
// behavior from fatia 1, plus optional HighWAPE / HighRMSE (logistics-friendly).
func DefaultDetectors() []DetectorConfig {
	return []DetectorConfig{
		{
			ID: "HighMAPE", Type: "threshold", Metric: "mape",
			Warning: f64(0.25), Critical: f64(0.40), MinSupport: 30, Direction: "above",
		},
		{
			ID: "HighWAPE", Type: "threshold", Metric: "wape",
			Warning: f64(0.30), Critical: f64(0.45), MinSupport: 30, Direction: "above",
		},
		{
			ID: "HighRMSE", Type: "threshold", Metric: "rmse",
			Warning: f64(10.0), Critical: f64(20.0), MinSupport: 30, Direction: "above",
		},
		{
			ID: "BiasShift", Type: "zscore", Metric: "bias",
			AbsWarning: f64(0.15), ZWarning: f64(3.0), MinSupport: 30,
		},
		{
			ID: "LowSupport", Type: "low_support", MinSupport: 30, Severity: "info",
		},
		{
			ID: "CoverageBreak", Type: "threshold", Metric: "coverage_80",
			Warning: f64(0.60), MinSupport: 30, Direction: "below",
		},
	}
}

// ClassicDetectors is the fatia-1 set only (no WAPE/RMSE) — used when migrating
// a legacy thresholds block so behavior stays identical.
func ClassicDetectorsFromLegacy(t LegacyThresholds) []DetectorConfig {
	minN := 30
	if t.MinSupport != nil {
		minN = *t.MinSupport
	}
	mapeW, mapeC := 0.25, 0.40
	if t.HighMAPEWarning != nil {
		mapeW = *t.HighMAPEWarning
	}
	if t.HighMAPECritical != nil {
		mapeC = *t.HighMAPECritical
	}
	biasAbs, biasZ := 0.15, 3.0
	if t.BiasAbsWarning != nil {
		biasAbs = *t.BiasAbsWarning
	}
	if t.BiasZWarning != nil {
		biasZ = *t.BiasZWarning
	}
	cov := 0.60
	if t.CoverageWarning != nil {
		cov = *t.CoverageWarning
	}
	return []DetectorConfig{
		{
			ID: "HighMAPE", Type: "threshold", Metric: "mape",
			Warning: f64(mapeW), Critical: f64(mapeC), MinSupport: minN, Direction: "above",
		},
		{
			ID: "BiasShift", Type: "zscore", Metric: "bias",
			AbsWarning: f64(biasAbs), ZWarning: f64(biasZ), MinSupport: minN,
		},
		{
			ID: "LowSupport", Type: "low_support", MinSupport: minN, Severity: "info",
		},
		{
			ID: "CoverageBreak", Type: "threshold", Metric: "coverage_80",
			Warning: f64(cov), MinSupport: minN, Direction: "below",
		},
	}
}

func DefaultConfig() WardenConfig {
	return WardenConfig{
		SchemaVersion:    2,
		DataDir:          "data",
		IncidentsDir:     "incidents",
		MetricsFilename:  "run_metrics.csv",
		BaselineFilename: "baseline_stats.csv",
		Detectors:        DefaultDetectors(),
	}
}

func (c WardenConfig) MetricsPath() string {
	return filepath.Join(c.DataDir, c.MetricsFilename)
}

func (c WardenConfig) BaselinePath() string {
	return filepath.Join(c.DataDir, c.BaselineFilename)
}

const DefaultConfigYAML = `# forecast-warden configuration (schema_version 2 — pluggable detectors)
schema_version: 2
data_dir: data
incidents_dir: incidents
metrics_filename: run_metrics.csv
baseline_filename: baseline_stats.csv

# Detectors are pluggable. Types: threshold | zscore | low_support.
# Threshold direction: above (default) or below. Missing metrics → no finding.
# HighWAPE is a logistics-opinionated option (WAPE often preferred over MAPE).
detectors:
  - id: HighMAPE
    type: threshold
    metric: mape
    warning: 0.25
    critical: 0.40
    min_support: 30
    direction: above
  - id: HighWAPE
    type: threshold
    metric: wape
    warning: 0.30
    critical: 0.45
    min_support: 30
    direction: above
  - id: HighRMSE
    type: threshold
    metric: rmse
    warning: 10.0
    critical: 20.0
    min_support: 30
    direction: above
  - id: BiasShift
    type: zscore
    metric: bias
    abs_warning: 0.15
    z_warning: 3.0
    min_support: 30
  - id: LowSupport
    type: low_support
    min_support: 30
    severity: info
  - id: CoverageBreak
    type: threshold
    metric: coverage_80
    direction: below
    warning: 0.60
    min_support: 30
`

func WriteDefaultConfig(path string) error {
	return os.WriteFile(path, []byte(DefaultConfigYAML), 0o644)
}

// LoadConfig loads warden.yaml (schema v2 detectors, or legacy thresholds).
func LoadConfig(path string) (WardenConfig, error) {
	cfg := DefaultConfig()
	if path == "" {
		path = "warden.yaml"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}

	var raw WardenConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	if raw.DataDir != "" {
		cfg.DataDir = raw.DataDir
	}
	if raw.IncidentsDir != "" {
		cfg.IncidentsDir = raw.IncidentsDir
	}
	if raw.MetricsFilename != "" {
		cfg.MetricsFilename = raw.MetricsFilename
	}
	if raw.BaselineFilename != "" {
		cfg.BaselineFilename = raw.BaselineFilename
	}
	if raw.SchemaVersion != 0 {
		cfg.SchemaVersion = raw.SchemaVersion
	}

	switch {
	case len(raw.Detectors) > 0:
		cfg.Detectors = normalizeDetectors(raw.Detectors)
	case raw.Thresholds != nil:
		cfg.Detectors = ClassicDetectorsFromLegacy(*raw.Thresholds)
		cfg.SchemaVersion = 2
	default:
		// keep DefaultDetectors
	}
	return cfg, nil
}

func normalizeDetectors(in []DetectorConfig) []DetectorConfig {
	out := make([]DetectorConfig, len(in))
	for i, d := range in {
		d.Type = strings.ToLower(strings.TrimSpace(d.Type))
		d.Direction = strings.ToLower(strings.TrimSpace(d.Direction))
		if d.Type == "threshold" && d.Direction == "" {
			d.Direction = "above"
		}
		if d.MinSupport == 0 && d.Type != "low_support" {
			// leave 0 = no min_support gate for threshold/zscore when omitted
		}
		if d.Type == "low_support" && d.Severity == "" {
			d.Severity = "info"
		}
		if d.Type == "low_support" && d.MinSupport == 0 {
			d.MinSupport = 30
		}
		out[i] = d
	}
	return out
}

func MustExistOrDefault(path string) (WardenConfig, error) {
	if path == "" {
		path = "warden.yaml"
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return WardenConfig{}, err
	}
	return LoadConfig(path)
}

func FormatDetectors(dets []DetectorConfig) string {
	parts := make([]string, 0, len(dets))
	for _, d := range dets {
		parts = append(parts, fmt.Sprintf("%s(%s)", d.ID, d.Type))
	}
	return strings.Join(parts, ",")
}
