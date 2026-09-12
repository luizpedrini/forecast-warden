package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/luizpedrini/forecast-warden/internal/config"
)

func TestLoadDefaultDetectorsYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.yaml")
	if err := config.WriteDefaultConfig(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != 2 {
		t.Fatalf("schema=%d", cfg.SchemaVersion)
	}
	ids := map[string]bool{}
	for _, d := range cfg.Detectors {
		ids[d.ID] = true
	}
	for _, want := range []string{"HighMAPE", "HighWAPE", "HighRMSE", "BiasShift", "LowSupport", "CoverageBreak"} {
		if !ids[want] {
			t.Fatalf("missing detector %s", want)
		}
	}
}

func TestLoadLegacyThresholds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.yaml")
	content := `data_dir: data
incidents_dir: incidents
metrics_filename: run_metrics.csv
baseline_filename: baseline_stats.csv
thresholds:
  high_mape_warning: 0.30
  high_mape_critical: 0.50
  min_support: 20
  bias_abs_warning: 0.20
  bias_z_warning: 2.5
  coverage_warning: 0.55
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Detectors) != 4 {
		t.Fatalf("want 4 classic detectors, got %d", len(cfg.Detectors))
	}
	var mape *config.DetectorConfig
	for i := range cfg.Detectors {
		if cfg.Detectors[i].ID == "HighMAPE" {
			mape = &cfg.Detectors[i]
		}
	}
	if mape == nil || mape.Warning == nil || *mape.Warning != 0.30 {
		t.Fatalf("HighMAPE warning not migrated: %+v", mape)
	}
	if mape.Critical == nil || *mape.Critical != 0.50 {
		t.Fatalf("HighMAPE critical=%v", mape.Critical)
	}
}
