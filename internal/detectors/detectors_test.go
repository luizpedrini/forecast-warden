package detectors_test

import (
	"testing"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/detectors"
	"github.com/luizpedrini/forecast-warden/internal/models"
)

func row(opts ...func(*models.MetricRow)) models.MetricRow {
	r := models.MetricRow{
		RunID:       "2026-09-12",
		EntityType:  "zone",
		EntityID:    "Z1",
		MAPE:        0.10,
		Bias:        0.01,
		Coverage80:  0.85,
		NActuals:    50,
		GeneratedAt: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC),
	}
	for _, o := range opts {
		o(&r)
	}
	return r
}

func TestHighMAPEWarning(t *testing.T) {
	det := detectors.HighMAPE{Thresholds: config.DefaultThresholds()}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.MAPE = 0.30
		r.NActuals = 40
	})}, nil)
	if len(findings) != 1 || findings[0].Severity != models.SeverityWarning || findings[0].Code != "HighMAPE" {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestHighMAPECritical(t *testing.T) {
	det := detectors.HighMAPE{Thresholds: config.DefaultThresholds()}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.MAPE = 0.45
		r.NActuals = 40
	})}, nil)
	if len(findings) != 1 || findings[0].Severity != models.SeverityCritical {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestHighMAPESkipsLowSupport(t *testing.T) {
	det := detectors.HighMAPE{Thresholds: config.DefaultThresholds()}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.MAPE = 0.50
		r.NActuals = 10
	})}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestHighMAPEHealthy(t *testing.T) {
	det := detectors.HighMAPE{Thresholds: config.DefaultThresholds()}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.MAPE = 0.20
		r.NActuals = 40
	})}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestBiasShiftAbs(t *testing.T) {
	det := detectors.BiasShift{Thresholds: config.DefaultThresholds()}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) { r.Bias = 0.20 })}, nil)
	if len(findings) != 1 || findings[0].Severity != models.SeverityWarning || findings[0].Code != "BiasShift" {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestBiasShiftZScore(t *testing.T) {
	det := detectors.BiasShift{Thresholds: config.DefaultThresholds()}
	baseline := map[string]models.BaselineStats{
		"Z1": {
			EntityID: "Z1",
			MAPEMean: 0.12,
			MAPEStd:  0.02,
			BiasMean: 0.0,
			BiasStd:  0.02,
		},
	}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) { r.Bias = 0.10 })}, baseline)
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	z, ok := findings[0].Evidence["z_score"].(float64)
	if !ok || z != 5.0 {
		t.Fatalf("z_score=%v", findings[0].Evidence["z_score"])
	}
}

func TestBiasShiftHealthy(t *testing.T) {
	det := detectors.BiasShift{Thresholds: config.DefaultThresholds()}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) { r.Bias = 0.05 })}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestLowSupportInfoOnly(t *testing.T) {
	det := detectors.LowSupport{Thresholds: config.DefaultThresholds()}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) { r.NActuals = 12 })}, nil)
	if len(findings) != 1 || findings[0].Severity != models.SeverityInfo || findings[0].Code != "LowSupport" {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestLowSupportOK(t *testing.T) {
	det := detectors.LowSupport{Thresholds: config.DefaultThresholds()}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) { r.NActuals = 30 })}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestCoverageBreakWarning(t *testing.T) {
	det := detectors.CoverageBreak{Thresholds: config.DefaultThresholds()}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.Coverage80 = 0.50
		r.NActuals = 40
	})}, nil)
	if len(findings) != 1 || findings[0].Severity != models.SeverityWarning || findings[0].Code != "CoverageBreak" {
		t.Fatalf("unexpected: %+v", findings)
	}
}

func TestCoverageBreakSkipsLowSupport(t *testing.T) {
	det := detectors.CoverageBreak{Thresholds: config.DefaultThresholds()}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.Coverage80 = 0.40
		r.NActuals = 5
	})}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestCoverageHealthy(t *testing.T) {
	det := detectors.CoverageBreak{Thresholds: config.DefaultThresholds()}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.Coverage80 = 0.70
		r.NActuals = 40
	})}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}

func TestCustomThresholds(t *testing.T) {
	tsh := config.DefaultThresholds()
	tsh.HighMAPEWarning = 0.50
	tsh.HighMAPECritical = 0.80
	det := detectors.HighMAPE{Thresholds: tsh}
	findings := det.Detect([]models.MetricRow{row(func(r *models.MetricRow) {
		r.MAPE = 0.30
		r.NActuals = 40
	})}, nil)
	if len(findings) != 0 {
		t.Fatalf("expected empty, got %+v", findings)
	}
}
