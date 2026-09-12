package detectors

import (
	"math"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/models"
)

type Detector interface {
	Code() string
	Detect(metrics []models.MetricRow, baseline map[string]models.BaselineStats) []models.Finding
}

func DefaultDetectors(t config.DetectorThresholds) []Detector {
	return []Detector{
		HighMAPE{Thresholds: t},
		BiasShift{Thresholds: t},
		LowSupport{Thresholds: t},
		CoverageBreak{Thresholds: t},
	}
}

func RunAll(metrics []models.MetricRow, baseline map[string]models.BaselineStats, t config.DetectorThresholds) []models.Finding {
	if baseline == nil {
		baseline = map[string]models.BaselineStats{}
	}
	var findings []models.Finding
	for _, d := range DefaultDetectors(t) {
		findings = append(findings, d.Detect(metrics, baseline)...)
	}
	return findings
}

type HighMAPE struct {
	Thresholds config.DetectorThresholds
}

func (d HighMAPE) Code() string { return "HighMAPE" }

func (d HighMAPE) Detect(metrics []models.MetricRow, _ map[string]models.BaselineStats) []models.Finding {
	t := d.Thresholds
	var findings []models.Finding
	for _, row := range metrics {
		if row.NActuals < t.MinSupport {
			continue
		}
		var sev models.Severity
		switch {
		case row.MAPE > t.HighMAPECritical:
			sev = models.SeverityCritical
		case row.MAPE > t.HighMAPEWarning:
			sev = models.SeverityWarning
		default:
			continue
		}
		findings = append(findings, models.Finding{
			Code:     d.Code(),
			Severity: sev,
			EntityID: row.EntityID,
			Evidence: map[string]any{
				"mape":               row.MAPE,
				"n_actuals":          row.NActuals,
				"warning_threshold":  t.HighMAPEWarning,
				"critical_threshold": t.HighMAPECritical,
			},
			Hint: "MAPE elevated vs threshold; check demand spike or model stale.",
		})
	}
	return findings
}

type BiasShift struct {
	Thresholds config.DetectorThresholds
}

func (d BiasShift) Code() string { return "BiasShift" }

func (d BiasShift) Detect(metrics []models.MetricRow, baseline map[string]models.BaselineStats) []models.Finding {
	t := d.Thresholds
	var findings []models.Finding
	for _, row := range metrics {
		absBias := math.Abs(row.Bias)
		var z any
		var zVal float64
		var hasZ bool
		stats, ok := baseline[row.EntityID]
		var baselineMean any
		var baselineStd any
		if ok && stats.BiasStd > 0 {
			zVal = math.Abs((row.Bias - stats.BiasMean) / stats.BiasStd)
			z = zVal
			hasZ = true
			baselineMean = stats.BiasMean
			baselineStd = stats.BiasStd
		} else {
			z = nil
			baselineMean = nil
			baselineStd = nil
		}
		triggeredAbs := absBias > t.BiasAbsWarning
		triggeredZ := hasZ && zVal > t.BiasZWarning
		if !(triggeredAbs || triggeredZ) {
			continue
		}
		findings = append(findings, models.Finding{
			Code:     d.Code(),
			Severity: models.SeverityWarning,
			EntityID: row.EntityID,
			Evidence: map[string]any{
				"bias":          row.Bias,
				"abs_bias":      absBias,
				"z_score":       z,
				"abs_threshold": t.BiasAbsWarning,
				"z_threshold":   t.BiasZWarning,
				"baseline_mean": baselineMean,
				"baseline_std":  baselineStd,
			},
			Hint: "Systematic over/under-forecast; check feature break or calendar.",
		})
	}
	return findings
}

type LowSupport struct {
	Thresholds config.DetectorThresholds
}

func (d LowSupport) Code() string { return "LowSupport" }

func (d LowSupport) Detect(metrics []models.MetricRow, _ map[string]models.BaselineStats) []models.Finding {
	t := d.Thresholds
	var findings []models.Finding
	for _, row := range metrics {
		if row.NActuals >= t.MinSupport {
			continue
		}
		findings = append(findings, models.Finding{
			Code:     d.Code(),
			Severity: models.SeverityInfo,
			EntityID: row.EntityID,
			Evidence: map[string]any{
				"n_actuals":   row.NActuals,
				"min_support": t.MinSupport,
			},
			Hint: "Sample size too small for reliable MAPE/coverage alerts.",
		})
	}
	return findings
}

type CoverageBreak struct {
	Thresholds config.DetectorThresholds
}

func (d CoverageBreak) Code() string { return "CoverageBreak" }

func (d CoverageBreak) Detect(metrics []models.MetricRow, _ map[string]models.BaselineStats) []models.Finding {
	t := d.Thresholds
	var findings []models.Finding
	for _, row := range metrics {
		if row.NActuals < t.MinSupport {
			continue
		}
		if row.Coverage80 >= t.CoverageWarning {
			continue
		}
		findings = append(findings, models.Finding{
			Code:     d.Code(),
			Severity: models.SeverityWarning,
			EntityID: row.EntityID,
			Evidence: map[string]any{
				"coverage_80": row.Coverage80,
				"n_actuals":   row.NActuals,
				"threshold":   t.CoverageWarning,
			},
			Hint: "Prediction intervals undercovering; check variance / calibration.",
		})
	}
	return findings
}
