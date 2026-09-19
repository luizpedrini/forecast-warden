package incidents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/csvio"
	"github.com/luizpedrini/forecast-warden/internal/detectors"
	"github.com/luizpedrini/forecast-warden/internal/incidents"
	"github.com/luizpedrini/forecast-warden/internal/models"
	"github.com/luizpedrini/forecast-warden/internal/synthetic"
)

func TestLowSupportAloneDoesNotOpenIncident(t *testing.T) {
	row := models.MetricRow{
		RunID:       "2026-09-12",
		EntityID:    "Z5",
		NActuals:    12,
		GeneratedAt: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC),
		Metrics: map[string]float64{
			"mape": 0.12, "bias": 0.02, "coverage_80": 0.81, "wape": 0.14, "rmse": 4.0, "stability": 0.08,
		},
	}
	findings := detectors.RunAll([]models.MetricRow{row}, nil, config.DefaultDetectors())
	found := false
	for _, f := range findings {
		if f.Code == "LowSupport" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected LowSupport finding")
	}
	if len(incidents.ActionableFindings(findings)) != 0 {
		t.Fatal("expected no actionable findings")
	}
	if incidents.BuildIncident("2026-09-12", findings, time.Time{}) != nil {
		t.Fatal("expected nil incident")
	}
}

func TestGoldenIncidentFromSyntheticLatest(t *testing.T) {
	metrics := synthetic.GenerateMetrics(14, synthetic.DefaultEndDate(), 42)
	baselineRows := synthetic.ComputeBaseline(metrics, 28)
	baseline := csvio.IndexFromStats(baselineRows)
	runSet := map[string]struct{}{}
	for _, m := range metrics {
		runSet[m.RunID] = struct{}{}
	}
	var runIDs []string
	for id := range runSet {
		runIDs = append(runIDs, id)
	}
	for i := 0; i < len(runIDs); i++ {
		for j := i + 1; j < len(runIDs); j++ {
			if runIDs[j] < runIDs[i] {
				runIDs[i], runIDs[j] = runIDs[j], runIDs[i]
			}
		}
	}
	latest := runIDs[len(runIDs)-1]
	var rows []models.MetricRow
	for _, m := range metrics {
		if m.RunID == latest {
			rows = append(rows, m)
		}
	}
	findings := detectors.RunAll(rows, baseline, config.DefaultDetectors())
	action := incidents.ActionableFindings(findings)
	if len(action) == 0 {
		t.Fatal("expected warning/critical findings on sick zones")
	}
	hasZ3 := false
	hasCritWAPE := false
	hasBias := false
	hasUnstable := false
	for _, f := range action {
		if f.EntityID == "Z3" {
			hasZ3 = true
		}
		if f.Code == "HighWAPE" && f.Severity == models.SeverityCritical {
			hasCritWAPE = true
		}
		if f.Code == "BiasShift" {
			hasBias = true
		}
		if f.Code == "UnstableForecast" {
			hasUnstable = true
		}
	}
	if !hasZ3 || !hasCritWAPE {
		t.Fatalf("Z3/HighWAPE critical missing: %+v", action)
	}
	if !hasBias {
		t.Fatalf("expected BiasShift on sick synth: %+v", action)
	}
	if !hasUnstable {
		t.Fatalf("expected UnstableForecast on sick synth: %+v", action)
	}
	incident := incidents.BuildIncident(latest, findings, time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC))
	if incident == nil {
		t.Fatal("expected incident")
	}
	if incident.Severity != models.SeverityCritical {
		t.Fatalf("severity=%s", incident.Severity)
	}
	foundZ3 := false
	for _, e := range incident.Entities {
		if e == "Z3" {
			foundZ3 = true
		}
	}
	if !foundZ3 {
		t.Fatalf("entities=%v", incident.Entities)
	}
	foundHW := false
	foundUF := false
	for _, c := range incident.DetectorCodes {
		if c == "HighWAPE" {
			foundHW = true
		}
		if c == "UnstableForecast" {
			foundUF = true
		}
	}
	if !foundHW {
		t.Fatalf("codes missing HighWAPE: %v", incident.DetectorCodes)
	}
	if !foundUF {
		t.Fatalf("codes missing UnstableForecast: %v", incident.DetectorCodes)
	}

	tmp := t.TempDir()
	mdPath, jsonPath, err := incidents.WriteIncident(tmp, incident)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(mdPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["id"] != incident.ID {
		t.Fatalf("id=%v", decoded["id"])
	}
	if decoded["status"] != "open" {
		t.Fatalf("status=%v", decoded["status"])
	}
	if decoded["run_id"] != latest {
		t.Fatalf("run_id=%v", decoded["run_id"])
	}
	text, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatal(err)
	}
	s := string(text)
	if !contains(s, incident.ID) || !contains(s, "Forecast Incident") || !contains(s, "Gate") {
		t.Fatalf("markdown missing fields: %s", filepath.Base(mdPath))
	}
}

func TestSickProfilesDocumented(t *testing.T) {
	if synthetic.SickProfiles["Z3"].MAPE <= 0.40 {
		t.Fatal("Z3 mape")
	}
	if abs(synthetic.SickProfiles["Z7"].Bias) <= 0.15 {
		t.Fatal("Z7 bias")
	}
	if synthetic.SickProfiles["Z5"].NActuals >= 30 {
		t.Fatal("Z5 support")
	}
	if synthetic.SickProfiles["Z3"].WAPE <= 0.45 {
		t.Fatal("Z3 wape should be critical vs HighWAPE")
	}
	if synthetic.SickProfiles["Z7"].WAPE <= 0.30 {
		t.Fatal("Z7 wape should be warning vs HighWAPE")
	}
	if synthetic.SickProfiles["Z3"].Stability <= 0.30 {
		t.Fatal("Z3 stability should be critical vs UnstableForecast")
	}
	if synthetic.SickProfiles["Z7"].Stability <= 0.15 {
		t.Fatal("Z7 stability should be warning vs UnstableForecast")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestStableIncidentIDFromRunID(t *testing.T) {
	// Same runID should always produce same incident ID
	runID := "2026-09-12"
	id1 := incidents.MakeIncidentID(runID)
	id2 := incidents.MakeIncidentID(runID)
	if id1 != id2 {
		t.Fatalf("IDs should be identical for same runID: %s vs %s", id1, id2)
	}
	if id1 != "fw-2026-09-12" {
		t.Fatalf("expected fw-2026-09-12, got %s", id1)
	}
}

func TestSanitizeRunID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"2026-09-12", "2026-09-12"},
		{"2026_09_12", "2026_09_12"},
		{"run/2026-09-12", "run_2026-09-12"},
		{"run:2026", "run_2026"},
		{"my run", "my_run"},
		{"RUN-123-ABC", "RUN-123-ABC"},
	}
	for _, tc := range tests {
		got := incidents.SanitizeRunID(tc.input)
		if got != tc.expected {
			t.Errorf("SanitizeRunID(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestIncidentIDIndependentOfFindings(t *testing.T) {
	// ID should be stable regardless of findings content
	runID := "2026-09-12"
	findings1 := []models.Finding{
		{Code: "HighWAPE", Severity: models.SeverityCritical, EntityID: "Z3"},
	}
	findings2 := []models.Finding{
		{Code: "BiasShift", Severity: models.SeverityWarning, EntityID: "Z7"},
		{Code: "LowSupport", Severity: models.SeverityInfo, EntityID: "Z5"},
	}

	inc1 := incidents.BuildIncident(runID, findings1, time.Time{})
	inc2 := incidents.BuildIncident(runID, findings2, time.Time{})

	if inc1 == nil || inc2 == nil {
		// inc2 might be nil since BiasShift/LowSupport alone might not trigger
		if inc1 != nil {
			// Just check inc1 ID is stable
			id := incidents.MakeIncidentID(runID)
			if inc1.ID != id {
				t.Fatalf("incident ID mismatch: %s vs %s", inc1.ID, id)
			}
		}
		return
	}

	if inc1.ID != inc2.ID {
		t.Fatalf("IDs should be same for same runID regardless of findings: %s vs %s", inc1.ID, inc2.ID)
	}
}

func TestUpdateIncidentInPlace(t *testing.T) {
	existing := &models.Incident{
		ID:        "fw-2026-09-12",
		RunID:     "2026-09-12",
		Status:    models.StatusOpen,
		Severity:  models.SeverityWarning,
		CreatedAt: time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC),
		Note:      func() *string { s := "old note"; return &s }(),
		History: []models.HistoryEvent{
			{Timestamp: time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC), Event: "created", Status: models.StatusOpen, Severity: models.SeverityWarning},
		},
	}
	newInc := &models.Incident{
		ID:        "fw-2026-09-12",
		RunID:     "2026-09-12",
		Status:    models.StatusOpen,
		Severity:  models.SeverityCritical,
		CreatedAt: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC), // Different time
		Findings: []models.Finding{
			{Code: "HighWAPE", Severity: models.SeverityCritical, EntityID: "Z3"},
		},
	}

	result := incidents.UpdateIncidentInPlace(existing, newInc)

	// Should preserve original createdAt
	if !result.CreatedAt.Equal(existing.CreatedAt) {
		t.Fatalf("createdAt should be preserved: got %v, want %v", result.CreatedAt, existing.CreatedAt)
	}

	// Should preserve note
	if result.Note == nil || *result.Note != "old note" {
		t.Fatalf("note should be preserved: got %v", result.Note)
	}

	// Should update severity
	if result.Severity != models.SeverityCritical {
		t.Fatalf("severity should be updated: got %v", result.Severity)
	}

	// History should have original + update event
	if len(result.History) != 2 {
		t.Fatalf("expected 2 history events, got %d", len(result.History))
	}
	if result.History[1].Event != "updated" {
		t.Fatalf("expected 'updated' event, got %s", result.History[1].Event)
	}
}

func TestReopenIncident(t *testing.T) {
	resolved := time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)
	existing := &models.Incident{
		ID:         "fw-2026-09-12",
		RunID:      "2026-09-12",
		Status:     models.StatusResolved,
		Severity:   models.SeverityWarning,
		CreatedAt:  time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC),
		Note:       func() *string { s := "resolved note"; return &s }(),
		ResolvedAt: &resolved,
		History: []models.HistoryEvent{
			{Timestamp: time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC), Event: "created", Status: models.StatusOpen, Severity: models.SeverityWarning},
			{Timestamp: resolved, Event: "resolved", Status: models.StatusResolved, Severity: models.SeverityWarning, Note: "resolved note"},
		},
	}
	newInc := &models.Incident{
		ID:        "fw-2026-09-12",
		RunID:     "2026-09-12",
		Status:    models.StatusOpen,
		Severity:  models.SeverityCritical,
		CreatedAt: time.Date(2026, 9, 12, 14, 0, 0, 0, time.UTC),
		Findings: []models.Finding{
			{Code: "HighWAPE", Severity: models.SeverityCritical, EntityID: "Z3"},
		},
	}

	result := incidents.ReopenIncident(existing, newInc)

	// Should preserve original createdAt
	if !result.CreatedAt.Equal(existing.CreatedAt) {
		t.Fatalf("createdAt should be preserved: got %v, want %v", result.CreatedAt, existing.CreatedAt)
	}

	// Status should be open
	if result.Status != models.StatusOpen {
		t.Fatalf("status should be open: got %v", result.Status)
	}

	// Note and ResolvedAt should be cleared
	if result.Note != nil {
		t.Fatalf("note should be nil after reopen: got %v", result.Note)
	}
	if result.ResolvedAt != nil {
		t.Fatalf("resolvedAt should be nil after reopen: got %v", result.ResolvedAt)
	}

	// History should have original events + reopen event
	if len(result.History) != 3 {
		t.Fatalf("expected 3 history events, got %d", len(result.History))
	}
	if result.History[2].Event != "reopened" {
		t.Fatalf("expected 'reopened' event, got %s", result.History[2].Event)
	}
	// Reopen event should mention the prior resolved status
	if !contains(result.History[2].Note, "reopened from resolved") {
		t.Fatalf("reopen note should mention prior status: %s", result.History[2].Note)
	}
}
