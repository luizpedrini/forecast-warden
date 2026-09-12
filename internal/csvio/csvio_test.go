package csvio_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/csvio"
	"github.com/luizpedrini/forecast-warden/internal/detectors"
	"github.com/luizpedrini/forecast-warden/internal/models"
)

func TestV1CSVCompat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run_metrics.csv")
	// Old fatia-1 header: no wape/rmse
	content := `run_id,entity_type,entity_id,mape,bias,coverage_80,n_actuals,generated_at
2026-09-12,zone,Z3,0.42,0.22,0.55,80,2026-09-12T06:00:00+00:00
2026-09-12,zone,Z1,0.12,0.01,0.85,50,2026-09-12T06:00:00+00:00
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := csvio.ReadMetrics(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	z3 := rows[0]
	if _, ok := z3.Metric("mape"); !ok {
		t.Fatal("mape missing")
	}
	if _, ok := z3.Metric("wape"); ok {
		t.Fatal("wape should be absent")
	}
	// Classic detectors (no HighWAPE) should still fire on mape/bias/coverage
	findings := detectors.RunAll(rows, nil, config.ClassicDetectorsFromLegacy(config.LegacyThresholds{}))
	codes := map[string]bool{}
	for _, f := range findings {
		codes[f.Code] = true
	}
	if !codes["HighMAPE"] || !codes["BiasShift"] || !codes["CoverageBreak"] {
		t.Fatalf("classic findings missing: %+v", findings)
	}
	// Default triad detectors that need wape/stability — must no-op when columns absent
	all := detectors.RunAll(rows, nil, config.DefaultDetectors())
	for _, f := range all {
		if f.Code == "HighWAPE" || f.Code == "UnstableForecast" {
			t.Fatalf("unexpected finding for missing metric: %+v", f)
		}
	}
	// BiasShift still fires (bias present)
	codes2 := map[string]bool{}
	for _, f := range all {
		codes2[f.Code] = true
	}
	if !codes2["BiasShift"] {
		t.Fatalf("expected BiasShift on v1 CSV with bias: %+v", all)
	}
}

func TestUnknownColumnsIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.csv")
	content := `run_id,entity_type,entity_id,mape,bias,coverage_80,n_actuals,generated_at,notes,extra_flag
2026-09-12,zone,Z1,0.10,0.01,0.85,50,2026-09-12T06:00:00+00:00,hello,true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := csvio.ReadMetrics(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d", len(rows))
	}
	if _, ok := rows[0].Metric("notes"); ok {
		t.Fatal("non-float notes should be ignored")
	}
}

func TestWriteReadRoundTripWithWAPE(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.csv")
	row := models.MetricRow{
		RunID: "2026-09-12", EntityType: "zone", EntityID: "Z3",
		NActuals: 80, GeneratedAt: time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC),
		Metrics: map[string]float64{
			"mape": 0.42, "bias": 0.22, "coverage_80": 0.55, "wape": 0.48, "rmse": 22.0,
		},
	}
	if err := csvio.WriteMetrics(path, []models.MetricRow{row}); err != nil {
		t.Fatal(err)
	}
	got, err := csvio.ReadMetrics(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	wape, ok := got[0].Metric("wape")
	if !ok || wape != 0.48 {
		t.Fatalf("wape=%v ok=%v", wape, ok)
	}
}

func TestBaselineLongRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.csv")
	rows := []models.BaselineStat{
		{EntityType: "zone", EntityID: "Z1", Metric: "mape", Mean: 0.12, Std: 0.02, WindowDays: 28},
		{EntityType: "zone", EntityID: "Z1", Metric: "bias", Mean: 0.0, Std: 0.03, WindowDays: 28},
		{EntityType: "zone", EntityID: "Z1", Metric: "wape", Mean: 0.13, Std: 0.02, WindowDays: 28},
	}
	if err := csvio.WriteBaseline(path, rows); err != nil {
		t.Fatal(err)
	}
	idx, err := csvio.ReadBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	s, ok := idx.Get("Z1", "wape")
	if !ok || s.Mean != 0.13 {
		t.Fatalf("wape baseline=%v ok=%v", s, ok)
	}
}

func TestBaselineWideLegacy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.csv")
	content := `entity_type,entity_id,mape_mean,mape_std,bias_mean,bias_std,window_days
zone,Z1,0.120000,0.020000,0.000000,0.030000,28
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := csvio.ReadBaseline(path)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := idx.Get("Z1", "mape")
	if !ok || m.Mean != 0.12 {
		t.Fatalf("mape=%v", m)
	}
	b, ok := idx.Get("Z1", "bias")
	if !ok || b.Std != 0.03 {
		t.Fatalf("bias=%v", b)
	}
}
