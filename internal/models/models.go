package models

import "time"

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

func (s Severity) Rank() int {
	switch s {
	case SeverityInfo:
		return 0
	case SeverityWarning:
		return 1
	case SeverityCritical:
		return 2
	default:
		return -1
	}
}

type IncidentStatus string

const (
	StatusOpen         IncidentStatus = "open"
	StatusResolved     IncidentStatus = "resolved"
	StatusAcceptedRisk IncidentStatus = "accepted-risk"
	StatusWontfix      IncidentStatus = "wontfix"
)

type SuggestedAction string

const (
	ActionInvestigate     SuggestedAction = "investigate"
	ActionHoldPromotion   SuggestedAction = "hold_promotion"
	ActionRetuneThreshold SuggestedAction = "retune_threshold"
	ActionRetrain         SuggestedAction = "retrain"
	ActionAcceptedRisk    SuggestedAction = "accepted_risk"
)

type Hypothesis string

const (
	HypDemandSpike  Hypothesis = "demand_spike"
	HypFeatureBreak Hypothesis = "feature_break"
	HypCalendar     Hypothesis = "calendar"
	HypModelStale   Hypothesis = "model_stale"
	HypDataDelay    Hypothesis = "data_delay"
)

type MetricRow struct {
	RunID       string    `json:"run_id"`
	EntityType  string    `json:"entity_type"`
	EntityID    string    `json:"entity_id"`
	MAPE        float64   `json:"mape"`
	Bias        float64   `json:"bias"`
	Coverage80  float64   `json:"coverage_80"`
	NActuals    int       `json:"n_actuals"`
	GeneratedAt time.Time `json:"generated_at"`
}

type BaselineStats struct {
	EntityType string  `json:"entity_type"`
	EntityID   string  `json:"entity_id"`
	MAPEMean   float64 `json:"mape_mean"`
	MAPEStd    float64 `json:"mape_std"`
	BiasMean   float64 `json:"bias_mean"`
	BiasStd    float64 `json:"bias_std"`
	WindowDays int     `json:"window_days"`
}

type Finding struct {
	Code     string         `json:"code"`
	Severity Severity       `json:"severity"`
	EntityID string         `json:"entity_id"`
	Evidence map[string]any `json:"evidence"`
	Hint     string         `json:"hint"`
}

type Incident struct {
	ID              string          `json:"id"`
	RunID           string          `json:"run_id"`
	Status          IncidentStatus  `json:"status"`
	Severity        Severity        `json:"severity"`
	Entities        []string        `json:"entities"`
	DetectorCodes   []string        `json:"detector_codes"`
	CreatedAt       time.Time       `json:"created_at"`
	Findings        []Finding       `json:"findings"`
	Hypotheses      []Hypothesis    `json:"hypotheses"`
	SuggestedAction SuggestedAction `json:"suggested_action"`
	Note            *string         `json:"note"`
	ResolvedAt      *time.Time      `json:"resolved_at"`
}
