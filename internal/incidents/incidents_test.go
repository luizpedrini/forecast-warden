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
			"mape": 0.12, "bias": 0.02, "coverage_80": 0.81, "wape": 0.14, "rmse": 4.0,
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
	hasCrit := false
	hasWAPE := false
	for _, f := range action {
		if f.EntityID == "Z3" {
			hasZ3 = true
		}
		if f.Code == "HighMAPE" && f.Severity == models.SeverityCritical {
			hasCrit = true
		}
		if f.Code == "HighWAPE" {
			hasWAPE = true
		}
	}
	if !hasZ3 || !hasCrit {
		t.Fatalf("Z3/HighMAPE critical missing: %+v", action)
	}
	if !hasWAPE {
		t.Fatalf("expected HighWAPE on sick synth: %+v", action)
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
	foundHM := false
	for _, c := range incident.DetectorCodes {
		if c == "HighMAPE" {
			foundHM = true
		}
	}
	if !foundHM {
		t.Fatalf("codes=%v", incident.DetectorCodes)
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
