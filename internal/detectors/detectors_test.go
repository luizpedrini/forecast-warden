package detectors_test

import (
	"testing"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/detectors"
	"github.com/luizpedrini/forecast-warden/internal/models"
)

func f64(v float64) *float64 { return &v }

func row(opts ...func(*models.MetricRow)) models.MetricRow {
	r := models.MetricRow{
		RunID:       "2026-09-12",
		EntityType:  "zone",
		EntityID:    "Z1",
		NActuals:    50,
		GeneratedAt: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC),
		Metrics: map[string]float64{
			"mape":        0.10,
			"bias":        0.01,
			"coverage_80": 0.85,
			"wape":        0.12,
			"rmse":        4.0,
		},
	}
	for _, o := range opts {
		o(&r)
	}
	return r
}

func classic() []config.DetectorConfig {
	return config.ClassicDetectorsFromLegacy(config.LegacyThresholds{})
}

func TestThresholdAboveWarning(t *testing.T) {
	det := detectors.ThresholdDetector{Cfg: config.DetectorConfig{
		ID: "HighMAPE", Type: "threshold", Metric: "mape",
		Warning: f64(0.25), Critical: f64(0.40), MinSupport: 30, Direction: "above",
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.SetMetric("mape", 0.30)
		r.NActuals = 40
	})}, nil)
	if len(findings) != 1 || findings[0].Severity != models.SeverityWarning || findings[0].Code != "HighMAPE" {
		t.Fatalf("unexpected: %+v", findings)
	}
	if findings[0].Evidence["metric"] != "mape" {
		t.Fatalf("evidence metric=%v", findings[0].Evidence["metric"])
	}
}

func TestThresholdAboveCritical(t *testing.T) {
	det := detectors.ThresholdDetector{Cfg: config.DetectorConfig{
		ID: "HighMAPE", Type: "threshold", Metric: "mape",
		Warning: f64(0.25), Critical: f64(0.40), MinSupport: 30, Direction: "above",
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.SetMetric("mape", 0.45)
		r.NActuals = 40
	})}, nil)
	if len(findings) != 1 || findings[0].Severity != models.SeverityCritical {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestThresholdAboveSkipsLowSupport(t *testing.T) {
	det := detectors.ThresholdDetector{Cfg: config.DetectorConfig{
		ID: "HighMAPE", Type: "threshold", Metric: "mape",
		Warning: f64(0.25), Critical: f64(0.40), MinSupport: 30, Direction: "above",
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.SetMetric("mape", 0.50)
		r.NActuals = 10
	})}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestThresholdAboveHealthy(t *testing.T) {
	det := detectors.ThresholdDetector{Cfg: config.DetectorConfig{
		ID: "HighMAPE", Type: "threshold", Metric: "mape",
		Warning: f64(0.25), Critical: f64(0.40), MinSupport: 30, Direction: "above",
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.SetMetric("mape", 0.20)
		r.NActuals = 40
	})}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestThresholdBelowWarning(t *testing.T) {
	det := detectors.ThresholdDetector{Cfg: config.DetectorConfig{
		ID: "CoverageBreak", Type: "threshold", Metric: "coverage_80",
		Warning: f64(0.60), MinSupport: 30, Direction: "below",
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.SetMetric("coverage_80", 0.50)
		r.NActuals = 40
	})}, nil)
	if len(findings) != 1 || findings[0].Severity != models.SeverityWarning || findings[0].Code != "CoverageBreak" {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestThresholdBelowHealthy(t *testing.T) {
	det := detectors.ThresholdDetector{Cfg: config.DetectorConfig{
		ID: "CoverageBreak", Type: "threshold", Metric: "coverage_80",
		Warning: f64(0.60), MinSupport: 30, Direction: "below",
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.SetMetric("coverage_80", 0.70)
		r.NActuals = 40
	})}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestThresholdBelowSkipsLowSupport(t *testing.T) {
	det := detectors.ThresholdDetector{Cfg: config.DetectorConfig{
		ID: "CoverageBreak", Type: "threshold", Metric: "coverage_80",
		Warning: f64(0.60), MinSupport: 30, Direction: "below",
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.SetMetric("coverage_80", 0.40)
		r.NActuals = 5
	})}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestZScoreAbs(t *testing.T) {
	det := detectors.ZScoreDetector{Cfg: config.DetectorConfig{
		ID: "BiasShift", Type: "zscore", Metric: "bias",
		AbsWarning: f64(0.15), ZWarning: f64(3.0), MinSupport: 30,
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) { r.SetMetric("bias", 0.20) })}, nil)
	if len(findings) != 1 || findings[0].Severity != models.SeverityWarning || findings[0].Code != "BiasShift" {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestZScoreVsBaseline(t *testing.T) {
	det := detectors.ZScoreDetector{Cfg: config.DetectorConfig{
		ID: "BiasShift", Type: "zscore", Metric: "bias",
		AbsWarning: f64(0.15), ZWarning: f64(3.0), MinSupport: 30,
	}}
	baseline := models.BaselineIndex{}
	baseline.Put(models.BaselineStat{
		EntityID: "Z1", Metric: "bias", Mean: 0.0, Std: 0.02,
	})
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) { r.SetMetric("bias", 0.10) })}, baseline)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	z, ok := findings[0].Evidence["z_score"].(float64)
	if !ok || z != 5.0 {
		t.Fatalf("z_score=%v", findings[0].Evidence["z_score"])
	}
}

func TestZScoreHealthy(t *testing.T) {
	det := detectors.ZScoreDetector{Cfg: config.DetectorConfig{
		ID: "BiasShift", Type: "zscore", Metric: "bias",
		AbsWarning: f64(0.15), ZWarning: f64(3.0), MinSupport: 30,
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) { r.SetMetric("bias", 0.05) })}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestLowSupportInfoOnly(t *testing.T) {
	det := detectors.LowSupportDetector{Cfg: config.DetectorConfig{
		ID: "LowSupport", Type: "low_support", MinSupport: 30, Severity: "info",
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) { r.NActuals = 12 })}, nil)
	if len(findings) != 1 || findings[0].Severity != models.SeverityInfo || findings[0].Code != "LowSupport" {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestLowSupportOK(t *testing.T) {
	det := detectors.LowSupportDetector{Cfg: config.DetectorConfig{
		ID: "LowSupport", Type: "low_support", MinSupport: 30, Severity: "info",
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) { r.NActuals = 30 })}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestMissingMetricNoOp(t *testing.T) {
	det := detectors.ThresholdDetector{Cfg: config.DetectorConfig{
		ID: "HighWAPE", Type: "threshold", Metric: "wape",
		Warning: f64(0.30), Critical: f64(0.45), MinSupport: 30, Direction: "above",
	}}
	r := row(func(r *models.MetricRow) {
		delete(r.Metrics, "wape")
		r.SetMetric("mape", 0.50) // mape sick, but detector watches wape
	})
	findings := det.Detect([]models.MetricRow{r}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected no-op for missing wape, got %+v", findings)
	}
}

func TestMissingMetricZScoreNoOp(t *testing.T) {
	det := detectors.ZScoreDetector{Cfg: config.DetectorConfig{
		ID: "BiasShift", Type: "zscore", Metric: "bias",
		AbsWarning: f64(0.15), ZWarning: f64(3.0), MinSupport: 30,
	}}
	r := row(func(r *models.MetricRow) { delete(r.Metrics, "bias") })
	findings := det.Detect([]models.MetricRow{r}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected no-op, got %+v", findings)
	}
}

func TestHighWAPEFinding(t *testing.T) {
	det := detectors.ThresholdDetector{Cfg: config.DetectorConfig{
		ID: "HighWAPE", Type: "threshold", Metric: "wape",
		Warning: f64(0.30), Critical: f64(0.45), MinSupport: 30, Direction: "above",
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.SetMetric("wape", 0.48)
		r.NActuals = 80
	})}, nil)
	if len(findings) != 1 || findings[0].Severity != models.SeverityCritical || findings[0].Code != "HighWAPE" {
		t.Fatalf("unexpected: %+v", findings)
	}
	if findings[0].Evidence["metric"] != "wape" {
		t.Fatalf("metric evidence=%v", findings[0].Evidence["metric"])
	}
}

func TestCustomThresholds(t *testing.T) {
	det := detectors.ThresholdDetector{Cfg: config.DetectorConfig{
		ID: "HighMAPE", Type: "threshold", Metric: "mape",
		Warning: f64(0.50), Critical: f64(0.80), MinSupport: 30, Direction: "above",
	}}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.SetMetric("mape", 0.30)
		r.NActuals = 40
	})}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestRunAllClassicPreservesBehavior(t *testing.T) {
	findings := detectors.RunAll([]models.MetricRow{row(func(r *models.MetricRow) {
		r.SetMetric("mape", 0.42)
		r.SetMetric("bias", 0.22)
		r.SetMetric("coverage_80", 0.55)
		r.NActuals = 80
	})}, nil, classic())
	codes := map[string]bool{}
	for _, f := range findings {
		codes[f.Code] = true
	}
	for _, want := range []string{"HighMAPE", "BiasShift", "CoverageBreak"} {
		if !codes[want] {
			t.Fatalf("missing %s in %+v", want, findings)
		}
	}
}
