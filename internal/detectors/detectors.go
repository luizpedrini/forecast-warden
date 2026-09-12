package detectors

import (
	"fmt"
	"math"
	"strings"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/models"
)

type Detector interface {
	Code() string
	Detect(metrics []models.MetricRow, baseline models.BaselineIndex) []models.Finding
}

// Build constructs detectors from config. Unknown types are skipped.
func Build(cfgs []config.DetectorConfig) []Detector {
	var out []Detector
	for _, c := range cfgs {
		switch strings.ToLower(c.Type) {
		case "threshold":
			out = append(out, ThresholdDetector{Cfg: c})
		case "zscore":
			out = append(out, ZScoreDetector{Cfg: c})
		case "low_support":
			out = append(out, LowSupportDetector{Cfg: c})
		}
	}
	return out
}

func RunAll(metrics []models.MetricRow, baseline models.BaselineIndex, cfgs []config.DetectorConfig) []models.Finding {
	if baseline == nil {
		baseline = models.BaselineIndex{}
	}
	var findings []models.Finding
	for _, d := range Build(cfgs) {
		findings = append(findings, d.Detect(metrics, baseline)...)
	}
	return findings
}

// --- threshold ---

type ThresholdDetector struct {
	Cfg config.DetectorConfig
}

func (d ThresholdDetector) Code() string { return d.Cfg.ID }

func (d ThresholdDetector) Detect(metrics []models.MetricRow, _ models.BaselineIndex) []models.Finding {
	c := d.Cfg
	metric := c.Metric
	if metric == "" {
		return nil
	}
	dir := strings.ToLower(c.Direction)
	if dir == "" {
		dir = "above"
	}
	var findings []models.Finding
	for _, row := range metrics {
		if c.MinSupport > 0 && row.NActuals < c.MinSupport {
			continue
		}
		val, ok := row.Metric(metric)
		if !ok {
			continue // missing metric → no finding
		}
		sev, triggered := thresholdSeverity(val, dir, c.Warning, c.Critical)
		if !triggered {
			continue
		}
		ev := map[string]any{
			"metric":    metric,
			metric:      val,
			"n_actuals": row.NActuals,
			"direction": dir,
		}
		if c.Warning != nil {
			ev["warning_threshold"] = *c.Warning
		}
		if c.Critical != nil {
			ev["critical_threshold"] = *c.Critical
		}
		findings = append(findings, models.Finding{
			Code:     d.Code(),
			Severity: sev,
			EntityID: row.EntityID,
			Evidence: ev,
			Hint:     thresholdHint(metric, dir),
		})
	}
	return findings
}

func thresholdSeverity(val float64, dir string, warning, critical *float64) (models.Severity, bool) {
	switch dir {
	case "below":
		if critical != nil && val < *critical {
			return models.SeverityCritical, true
		}
		if warning != nil && val < *warning {
			return models.SeverityWarning, true
		}
		return "", false
	default: // above
		if critical != nil && val > *critical {
			return models.SeverityCritical, true
		}
		if warning != nil && val > *warning {
			return models.SeverityWarning, true
		}
		return "", false
	}
}

func thresholdHint(metric, dir string) string {
	switch metric {
	case "mape":
		return "MAPE elevated vs threshold; check demand spike or model stale."
	case "wape":
		return "WAPE elevated vs threshold; logistics volume-weighted error — check demand spike or model stale."
	case "rmse":
		return "RMSE elevated vs threshold; check scale of residuals / outliers."
	case "coverage_80":
		return "Prediction intervals undercovering; check variance / calibration."
	case "stability":
		return "High forecast churn vs prior origin; planning nervousness — consider freeze policy or origin smoothing."
	default:
		if dir == "below" {
			return fmt.Sprintf("%s below threshold; investigate.", metric)
		}
		return fmt.Sprintf("%s above threshold; investigate.", metric)
	}
}

// --- zscore ---

type ZScoreDetector struct {
	Cfg config.DetectorConfig
}

func (d ZScoreDetector) Code() string { return d.Cfg.ID }

func (d ZScoreDetector) Detect(metrics []models.MetricRow, baseline models.BaselineIndex) []models.Finding {
	c := d.Cfg
	metric := c.Metric
	if metric == "" {
		return nil
	}
	var findings []models.Finding
	for _, row := range metrics {
		if c.MinSupport > 0 && row.NActuals < c.MinSupport {
			continue
		}
		val, ok := row.Metric(metric)
		if !ok {
			continue
		}
		absVal := math.Abs(val)
		var z any
		var zVal float64
		var hasZ bool
		var baselineMean any
		var baselineStd any
		if stats, ok := baseline.Get(row.EntityID, metric); ok && stats.Std > 0 {
			zVal = math.Abs((val - stats.Mean) / stats.Std)
			z = zVal
			hasZ = true
			baselineMean = stats.Mean
			baselineStd = stats.Std
		} else {
			z = nil
			baselineMean = nil
			baselineStd = nil
		}
		triggeredAbs := c.AbsWarning != nil && absVal > *c.AbsWarning
		triggeredZ := c.ZWarning != nil && hasZ && zVal > *c.ZWarning
		if !(triggeredAbs || triggeredZ) {
			continue
		}
		ev := map[string]any{
			"metric":        metric,
			metric:          val,
			"abs_value":     absVal,
			"z_score":       z,
			"baseline_mean": baselineMean,
			"baseline_std":  baselineStd,
			"n_actuals":     row.NActuals,
		}
		if c.AbsWarning != nil {
			ev["abs_threshold"] = *c.AbsWarning
		}
		if c.ZWarning != nil {
			ev["z_threshold"] = *c.ZWarning
		}
		hint := "Systematic shift vs baseline; check feature break or calendar."
		if metric == "bias" {
			hint = "Systematic over/under-forecast; check feature break or calendar."
		}
		findings = append(findings, models.Finding{
			Code:     d.Code(),
			Severity: models.SeverityWarning,
			EntityID: row.EntityID,
			Evidence: ev,
			Hint:     hint,
		})
	}
	return findings
}

// --- low_support ---

type LowSupportDetector struct {
	Cfg config.DetectorConfig
}

func (d LowSupportDetector) Code() string { return d.Cfg.ID }

func (d LowSupportDetector) Detect(metrics []models.MetricRow, _ models.BaselineIndex) []models.Finding {
	c := d.Cfg
	minN := c.MinSupport
	if minN <= 0 {
		minN = 30
	}
	sev := models.SeverityInfo
	switch strings.ToLower(c.Severity) {
	case "warning":
		sev = models.SeverityWarning
	case "critical":
		sev = models.SeverityCritical
	case "info", "":
		sev = models.SeverityInfo
	}
	var findings []models.Finding
	for _, row := range metrics {
		if row.NActuals >= minN {
			continue
		}
		findings = append(findings, models.Finding{
			Code:     d.Code(),
			Severity: sev,
			EntityID: row.EntityID,
			Evidence: map[string]any{
				"n_actuals":   row.NActuals,
				"min_support": minN,
			},
			Hint: "Sample size too small for reliable metric alerts.",
		})
	}
	return findings
}
