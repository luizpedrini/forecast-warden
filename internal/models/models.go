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

// IsTerminalStatus reports whether s is a closed outcome that must not be
// overwritten back to open by a subsequent check.
func IsTerminalStatus(s IncidentStatus) bool {
	switch s {
	case StatusResolved, StatusAcceptedRisk, StatusWontfix:
		return true
	default:
		return false
	}
}

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

// MetricRow is a wide metrics_v2 row: stable identity columns plus optional
// float metrics (wape, bias, stability, mape, rmse, coverage_80, …). Absent metrics are
// simply missing from Metrics — detectors that reference them no-op.
type MetricRow struct {
	RunID       string             `json:"run_id"`
	EntityType  string             `json:"entity_type"`
	EntityID    string             `json:"entity_id"`
	NActuals    int                `json:"n_actuals"`
	GeneratedAt time.Time          `json:"generated_at"`
	Metrics     map[string]float64 `json:"metrics"`
}

// Metric returns (value, true) if the named optional metric is present.
func (r MetricRow) Metric(name string) (float64, bool) {
	if r.Metrics == nil {
		return 0, false
	}
	v, ok := r.Metrics[name]
	return v, ok
}

// SetMetric stores an optional metric value.
func (r *MetricRow) SetMetric(name string, value float64) {
	if r.Metrics == nil {
		r.Metrics = map[string]float64{}
	}
	r.Metrics[name] = value
}

// BaselineStat is mean/std for one (entity, metric) pair (long baseline).
type BaselineStat struct {
	EntityType string  `json:"entity_type"`
	EntityID   string  `json:"entity_id"`
	Metric     string  `json:"metric"`
	Mean       float64 `json:"mean"`
	Std        float64 `json:"std"`
	WindowDays int     `json:"window_days"`
}

// BaselineIndex is entity_id → metric → BaselineStat.
type BaselineIndex map[string]map[string]BaselineStat

func (b BaselineIndex) Get(entityID, metric string) (BaselineStat, bool) {
	if b == nil {
		return BaselineStat{}, false
	}
	byMetric, ok := b[entityID]
	if !ok {
		return BaselineStat{}, false
	}
	s, ok := byMetric[metric]
	return s, ok
}

func (b BaselineIndex) Put(s BaselineStat) {
	if b == nil {
		return
	}
	byMetric, ok := b[s.EntityID]
	if !ok {
		byMetric = map[string]BaselineStat{}
		b[s.EntityID] = byMetric
	}
	byMetric[s.Metric] = s
}

type Finding struct {
	Code     string         `json:"code"`
	Severity Severity       `json:"severity"`
	EntityID string         `json:"entity_id"`
	Evidence map[string]any `json:"evidence"`
	Hint     string         `json:"hint"`
}

// HistoryEvent records a state change in an incident's lifecycle.
type HistoryEvent struct {
	Timestamp time.Time      `json:"timestamp"`
	Event     string         `json:"event"` // "created", "updated", "resolved", "reopened"
	Status    IncidentStatus `json:"status"`
	Severity  Severity       `json:"severity"`
	Note      string         `json:"note,omitempty"`
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
	// History preserves state changes across updates and reopens.
	// Appended to on each significant state change.
	History []HistoryEvent `json:"history,omitempty"`
}
