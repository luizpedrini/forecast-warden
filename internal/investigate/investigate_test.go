package investigate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/investigate"
	"github.com/luizpedrini/forecast-warden/internal/models"
)

func goldenIncident() *models.Incident {
	return &models.Incident{
		ID:            "fw-20260912-a1b2",
		RunID:         "2026-09-12",
		Status:        models.StatusOpen,
		Severity:      models.SeverityCritical,
		Entities:      []string{"Z3", "Z7"},
		DetectorCodes: []string{"BiasShift", "HighWAPE", "UnstableForecast"},
		CreatedAt:     time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC),
		Findings: []models.Finding{
			{
				Code:     "HighWAPE",
				Severity: models.SeverityCritical,
				EntityID: "Z3",
				Evidence: map[string]any{"metric": "wape", "wape": 0.55, "n_actuals": 80},
				Hint:     "WAPE above threshold",
			},
			{
				Code:     "UnstableForecast",
				Severity: models.SeverityCritical,
				EntityID: "Z3",
				Evidence: map[string]any{"metric": "stability", "stability": 0.35, "n_actuals": 80},
				Hint:     "forecast churn",
			},
			{
				Code:     "BiasShift",
				Severity: models.SeverityWarning,
				EntityID: "Z3",
				Evidence: map[string]any{"metric": "bias", "bias": 0.22},
				Hint:     "bias shift",
			},
			{
				Code:     "HighWAPE",
				Severity: models.SeverityWarning,
				EntityID: "Z7",
				Evidence: map[string]any{"metric": "wape", "wape": 0.32},
				Hint:     "WAPE warning",
			},
			{
				Code:     "LowSupport",
				Severity: models.SeverityInfo,
				EntityID: "Z5",
				Evidence: map[string]any{"n_actuals": 12},
				Hint:     "low support",
			},
		},
		Hypotheses:      []models.Hypothesis{models.HypModelStale, models.HypDemandSpike},
		SuggestedAction: models.ActionHoldPromotion,
	}
}

func TestOrderFindingsPrioritizesStabilityBeforeWAPE(t *testing.T) {
	inc := goldenIncident()
	ordered := investigate.OrderFindings(inc.Findings)
	if len(ordered) < 3 {
		t.Fatalf("expected findings, got %d", len(ordered))
	}
	// First actionable in attack order must be UnstableForecast before any HighWAPE.
	firstUnstable, firstWAPE, firstLow := -1, -1, -1
	for i, f := range ordered {
		switch f.Code {
		case "UnstableForecast":
			if firstUnstable < 0 {
				firstUnstable = i
			}
		case "HighWAPE":
			if firstWAPE < 0 {
				firstWAPE = i
			}
		case "LowSupport":
			if firstLow < 0 {
				firstLow = i
			}
		}
	}
	if firstUnstable < 0 || firstWAPE < 0 {
		t.Fatalf("missing codes in order: %+v", ordered)
	}
	if firstUnstable > firstWAPE {
		t.Fatalf("UnstableForecast at %d should precede HighWAPE at %d", firstUnstable, firstWAPE)
	}
	if firstLow >= 0 && firstLow != len(ordered)-1 {
		t.Fatalf("LowSupport should be last, got index %d of %d", firstLow, len(ordered)-1)
	}
	// BiasShift between Unstable and WAPE
	var firstBias int = -1
	for i, f := range ordered {
		if f.Code == "BiasShift" {
			firstBias = i
			break
		}
	}
	if firstBias < 0 || !(firstUnstable < firstBias && firstBias < firstWAPE) {
		t.Fatalf("want Unstable < Bias < WAPE, got U=%d B=%d W=%d", firstUnstable, firstBias, firstWAPE)
	}
}

func TestRenderContainsSectionsAndCombo(t *testing.T) {
	md := investigate.Render(goldenIncident())
	for _, section := range []string{
		"## Context",
		"## Attack order",
		"## Per-finding questions",
		"## 15-min checklist",
		"## Suggested resolve decision",
		"## Ready-made `--note` line",
		"### Combo hints",
	} {
		if !strings.Contains(md, section) {
			t.Fatalf("missing section %q in:\n%s", section, md)
		}
	}
	if !strings.Contains(md, "UnstableForecast") || !strings.Contains(md, "HighWAPE") {
		t.Fatal("expected detector names")
	}
	// Attack order listing: UnstableForecast line before HighWAPE line in Attack order section
	attackIdx := strings.Index(md, "## Attack order")
	perIdx := strings.Index(md, "## Per-finding questions")
	if attackIdx < 0 || perIdx < 0 {
		t.Fatal("section markers")
	}
	block := md[attackIdx:perIdx]
	u := strings.Index(block, "UnstableForecast")
	w := strings.Index(block, "HighWAPE")
	if u < 0 || w < 0 || u > w {
		t.Fatalf("attack order block should list Unstable before WAPE:\n%s", block)
	}
	if !strings.Contains(md, "HighWAPE/MAPE + UnstableForecast") {
		t.Fatal("expected combo hint for WAPE+stability")
	}
	if !strings.Contains(md, "warden resolve fw-20260912-a1b2 --note") {
		t.Fatal("expected ready-made resolve line")
	}
	if !strings.Contains(md, "hold_promotion") {
		t.Fatal("expected hold_promotion decision for critical golden")
	}
	// Generic outside guidance, no SQL
	if strings.Contains(strings.ToLower(md), "select ") || strings.Contains(md, "FROM ") {
		t.Fatal("playbook must not contain company SQL")
	}
}

func TestPlaybookPath(t *testing.T) {
	p := investigate.PlaybookPath("incidents", "fw-20260912-a1b2")
	if filepath.Base(p) != "fw-20260912-a1b2.investigate.md" {
		t.Fatalf("path=%s", p)
	}
}

func TestSuggestDecision(t *testing.T) {
	inc := goldenIncident()
	d := investigate.SuggestDecision(inc)
	if d != models.ActionHoldPromotion {
		t.Fatalf("decision=%s", d)
	}
}

func TestWritePlaybookFile(t *testing.T) {
	dir := t.TempDir()
	inc := goldenIncident()
	md := investigate.Render(inc)
	path := investigate.PlaybookPath(dir, inc.ID)
	if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "## Context") {
		t.Fatal("written file missing Context")
	}
}
