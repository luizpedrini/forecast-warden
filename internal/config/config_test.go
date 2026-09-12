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
	want := []string{"HighWAPE", "BiasShift", "UnstableForecast", "LowSupport"}
	for _, w := range want {
		if !ids[w] {
			t.Fatalf("missing detector %s in %v", w, ids)
		}
	}
	for _, gone := range []string{"HighMAPE", "HighRMSE", "CoverageBreak"} {
		if ids[gone] {
			t.Fatalf("%s should not be a default detector", gone)
		}
	}
	if len(cfg.Detectors) != 4 {
		t.Fatalf("want 4 default detectors, got %d", len(cfg.Detectors))
	}
}

func TestDefaultDetectorsTriad(t *testing.T) {
	dets := config.DefaultDetectors()
	if len(dets) != 4 {
		t.Fatalf("len=%d", len(dets))
	}
	byID := map[string]config.DetectorConfig{}
	for _, d := range dets {
		byID[d.ID] = d
	}
	wape := byID["HighWAPE"]
	if wape.Metric != "wape" || wape.Warning == nil || *wape.Warning != 0.30 || wape.Critical == nil || *wape.Critical != 0.45 {
		t.Fatalf("HighWAPE=%+v", wape)
	}
	unst := byID["UnstableForecast"]
	if unst.Metric != "stability" || unst.Warning == nil || *unst.Warning != 0.15 || unst.Critical == nil || *unst.Critical != 0.30 {
		t.Fatalf("UnstableForecast=%+v", unst)
	}
	if unst.Direction != "above" {
		t.Fatalf("UnstableForecast direction=%s", unst.Direction)
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

func TestLoadStoreDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.yaml")
	if err := config.WriteDefaultConfig(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store.Driver != "sqlite" {
		t.Fatalf("driver=%s", cfg.Store.Driver)
	}
	if cfg.Store.DSN != "data/warden.db" {
		t.Fatalf("dsn=%s", cfg.Store.DSN)
	}
	if !cfg.StoreWriteMarkdown() {
		t.Fatal("write_markdown default true")
	}
}

func TestLoadStoreWriteMarkdownFalse(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.yaml")
	content := `schema_version: 2
data_dir: data
incidents_dir: incidents
store:
  driver: sqlite
  dsn: data/custom.db
  write_markdown: false
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store.DSN != "data/custom.db" {
		t.Fatalf("dsn=%s", cfg.Store.DSN)
	}
	if cfg.StoreWriteMarkdown() {
		t.Fatal("want write_markdown false")
	}
}

func TestLoadOmitStoreBlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warden.yaml")
	content := `schema_version: 2
data_dir: data
incidents_dir: incidents
detectors:
  - id: LowSupport
    type: low_support
    min_support: 30
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store.Driver != "sqlite" || cfg.Store.DSN != "data/warden.db" || !cfg.StoreWriteMarkdown() {
		t.Fatalf("omit store should default: %+v", cfg.Store)
	}
}
