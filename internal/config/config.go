package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type DetectorThresholds struct {
	HighMAPEWarning  float64 `json:"high_mape_warning"`
	HighMAPECritical float64 `json:"high_mape_critical"`
	MinSupport       int     `json:"min_support"`
	BiasAbsWarning   float64 `json:"bias_abs_warning"`
	BiasZWarning     float64 `json:"bias_z_warning"`
	CoverageWarning  float64 `json:"coverage_warning"`
}

func DefaultThresholds() DetectorThresholds {
	return DetectorThresholds{
		HighMAPEWarning:  0.25,
		HighMAPECritical: 0.40,
		MinSupport:       30,
		BiasAbsWarning:   0.15,
		BiasZWarning:     3.0,
		CoverageWarning:  0.60,
	}
}

type WardenConfig struct {
	DataDir          string             `json:"data_dir"`
	IncidentsDir     string             `json:"incidents_dir"`
	MetricsFilename  string             `json:"metrics_filename"`
	BaselineFilename string             `json:"baseline_filename"`
	Thresholds       DetectorThresholds `json:"thresholds"`
}

func DefaultConfig() WardenConfig {
	return WardenConfig{
		DataDir:          "data",
		IncidentsDir:     "incidents",
		MetricsFilename:  "run_metrics.csv",
		BaselineFilename: "baseline_stats.csv",
		Thresholds:       DefaultThresholds(),
	}
}

func (c WardenConfig) MetricsPath() string {
	return filepath.Join(c.DataDir, c.MetricsFilename)
}

func (c WardenConfig) BaselinePath() string {
	return filepath.Join(c.DataDir, c.BaselineFilename)
}

const DefaultConfigYAML = `# forecast-warden configuration
data_dir: data
incidents_dir: incidents
metrics_filename: run_metrics.csv
baseline_filename: baseline_stats.csv

thresholds:
  high_mape_warning: 0.25
  high_mape_critical: 0.40
  min_support: 30
  bias_abs_warning: 0.15
  bias_z_warning: 3.0
  coverage_warning: 0.60
`

func WriteDefaultConfig(path string) error {
	return os.WriteFile(path, []byte(DefaultConfigYAML), 0o644)
}

// LoadConfig loads a minimal YAML subset (same contract as the former Python parser).
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

	raw := map[string]any{}
	type frame struct {
		indent int
		m      map[string]any
	}
	stack := []frame{{indent: 0, m: raw}}

	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		rest := strings.TrimLeft(line, " \t")
		key, val, _ := strings.Cut(rest, ":")
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		for len(stack) > 0 && indent < stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
		}
		parent := stack[len(stack)-1].m
		if val == "" {
			child := map[string]any{}
			parent[key] = child
			stack = append(stack, frame{indent: indent + 2, m: child})
		} else {
			parent[key] = coerce(val)
		}
	}
	if err := sc.Err(); err != nil {
		return cfg, err
	}

	applyRaw(&cfg, raw)
	return cfg, nil
}

func coerce(val string) any {
	low := strings.ToLower(val)
	if low == "true" {
		return true
	}
	if low == "false" {
		return false
	}
	if strings.Contains(val, ".") {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f
		}
	} else if i, err := strconv.Atoi(val); err == nil {
		return i
	}
	return val
}

func applyRaw(cfg *WardenConfig, raw map[string]any) {
	if v, ok := raw["data_dir"].(string); ok {
		cfg.DataDir = v
	}
	if v, ok := raw["incidents_dir"].(string); ok {
		cfg.IncidentsDir = v
	}
	if v, ok := raw["metrics_filename"].(string); ok {
		cfg.MetricsFilename = v
	}
	if v, ok := raw["baseline_filename"].(string); ok {
		cfg.BaselineFilename = v
	}
	th, ok := raw["thresholds"].(map[string]any)
	if !ok {
		return
	}
	setFloat := func(key string, dst *float64) {
		switch v := th[key].(type) {
		case float64:
			*dst = v
		case int:
			*dst = float64(v)
		}
	}
	setInt := func(key string, dst *int) {
		switch v := th[key].(type) {
		case int:
			*dst = v
		case float64:
			*dst = int(v)
		}
	}
	setFloat("high_mape_warning", &cfg.Thresholds.HighMAPEWarning)
	setFloat("high_mape_critical", &cfg.Thresholds.HighMAPECritical)
	setInt("min_support", &cfg.Thresholds.MinSupport)
	setFloat("bias_abs_warning", &cfg.Thresholds.BiasAbsWarning)
	setFloat("bias_z_warning", &cfg.Thresholds.BiasZWarning)
	setFloat("coverage_warning", &cfg.Thresholds.CoverageWarning)
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

func FormatThresholds(t DetectorThresholds) string {
	return fmt.Sprintf(
		"mape_w=%.2f mape_c=%.2f min_n=%d bias_abs=%.2f bias_z=%.1f cov=%.2f",
		t.HighMAPEWarning, t.HighMAPECritical, t.MinSupport,
		t.BiasAbsWarning, t.BiasZWarning, t.CoverageWarning,
	)
}
