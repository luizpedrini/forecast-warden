package llm_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/investigate"
	"github.com/luizpedrini/forecast-warden/internal/llm"
	"github.com/luizpedrini/forecast-warden/internal/models"
)

type mockClient struct {
	resp string
	err  error
	saw  struct {
		system, user string
	}
}

func (m *mockClient) Complete(ctx context.Context, system, user string) (string, error) {
	m.saw.system = system
	m.saw.user = user
	if m.err != nil {
		return "", m.err
	}
	return m.resp, nil
}

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
				Evidence: map[string]any{"wape": 0.55},
				Hint:     "WAPE above threshold",
			},
			{
				Code:     "UnstableForecast",
				Severity: models.SeverityCritical,
				EntityID: "Z3",
				Evidence: map[string]any{"stability": 0.35},
				Hint:     "forecast churn",
			},
		},
		Hypotheses:      []models.Hypothesis{models.HypModelStale, models.HypDemandSpike},
		SuggestedAction: models.ActionHoldPromotion,
	}
}

func TestParseEnrichmentJSON(t *testing.T) {
	raw := `{
  "synthesis": "Critical WAPE and forecast churn on Z3 point to unstable demand modeling.",
  "hypotheses": [
    {"name": "model_stale", "why": "bias+WAPE together on Z3"},
    {"name": "demand_spike", "why": "high WAPE with volume risk"},
    {"name": "feature_break", "why": "stability churn suggests pipeline flip"}
  ],
  "note": "investigated Z3; hold promotion until stability recovers"
}`
	e, err := llm.ParseEnrichment(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.Synthesis, "Critical WAPE") {
		t.Fatalf("synthesis=%q", e.Synthesis)
	}
	if len(e.Hypotheses) != 3 {
		t.Fatalf("hypotheses=%d", len(e.Hypotheses))
	}
	if e.Hypotheses[0].Name != "model_stale" {
		t.Fatalf("first=%v", e.Hypotheses[0])
	}
	if e.Note == "" {
		t.Fatal("expected note")
	}
}

func TestParseEnrichmentFencedAndCapsAtThree(t *testing.T) {
	raw := "```json\n{\"synthesis\":\"ok\",\"hypotheses\":[" +
		"{\"name\":\"a\",\"why\":\"1\"}," +
		"{\"name\":\"b\",\"why\":\"2\"}," +
		"{\"name\":\"c\",\"why\":\"3\"}," +
		"{\"name\":\"d\",\"why\":\"4\"}" +
		"]}\n```"
	e, err := llm.ParseEnrichment(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Hypotheses) != 3 {
		t.Fatalf("want 3, got %d", len(e.Hypotheses))
	}
}

func TestParseEnrichmentBadJSON(t *testing.T) {
	if _, err := llm.ParseEnrichment("not json"); err == nil {
		t.Fatal("expected error")
	}
}

func TestFormatSection(t *testing.T) {
	e := &llm.Enrichment{
		Synthesis: "Paragraph here.",
		Hypotheses: []llm.RankedHypothesis{
			{Name: "model_stale", Why: "evidence"},
		},
		Note: "hold promotion",
	}
	md := llm.FormatSection(e)
	for _, want := range []string{
		"## LLM synthesis",
		"Paragraph here.",
		"**Ranked hypotheses**",
		"`model_stale`",
		"**Optional refined `--note`:** hold promotion",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("missing %q in:\n%s", want, md)
		}
	}
}

func TestEnrichPlaybookSuccess(t *testing.T) {
	inc := goldenIncident()
	playbook := investigate.Render(inc)
	mock := &mockClient{resp: `{
  "synthesis": "Z3 shows critical WAPE with forecast churn; triage stability first.",
  "hypotheses": [
    {"name": "model_stale", "why": "critical WAPE + UnstableForecast on Z3"},
    {"name": "demand_spike", "why": "volume-weighted error elevated"}
  ],
  "note": "investigated Z3; hold promotion until stability/WAPE recover"
}`}
	out, err := llm.EnrichPlaybook(context.Background(), mock, inc, playbook)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "## LLM synthesis") {
		t.Fatal("missing LLM synthesis section")
	}
	if !strings.Contains(out, "## Context") || !strings.Contains(out, "## Attack order") {
		t.Fatal("rule-based sections must remain")
	}
	// LLM section should appear after Context, before Attack order.
	ctxIdx := strings.Index(out, "## Context")
	llmIdx := strings.Index(out, "## LLM synthesis")
	atkIdx := strings.Index(out, "## Attack order")
	if !(ctxIdx < llmIdx && llmIdx < atkIdx) {
		t.Fatalf("section order Context < LLM < Attack; got %d %d %d", ctxIdx, llmIdx, atkIdx)
	}
	if !strings.Contains(mock.saw.system, "Do NOT invent") {
		t.Fatal("system prompt not passed")
	}
	if !strings.Contains(mock.saw.user, "fw-20260912-a1b2") {
		t.Fatal("user prompt missing incident id")
	}
	if !strings.Contains(mock.saw.user, "## Rule-based playbook") {
		t.Fatal("user prompt missing playbook")
	}
}

func TestEnrichPlaybookClientError(t *testing.T) {
	inc := goldenIncident()
	playbook := investigate.Render(inc)
	mock := &mockClient{err: errors.New("network down")}
	_, err := llm.EnrichPlaybook(context.Background(), mock, inc, playbook)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "network down") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveProviderDefaults(t *testing.T) {
	t.Setenv("WARDEN_LLM_PROVIDER", "")
	if got := llm.ResolveProvider(""); got != llm.ProviderOpenAI {
		t.Fatalf("got %s", got)
	}
	t.Setenv("WARDEN_LLM_PROVIDER", "anthropic")
	if got := llm.ResolveProvider(""); got != llm.ProviderAnthropic {
		t.Fatalf("got %s", got)
	}
	if got := llm.ResolveProvider("OpenAI"); got != "openai" {
		t.Fatalf("flag override got %s", got)
	}
}

func TestAPIKeyForMissing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	if _, err := llm.APIKeyFor("openai"); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestSystemPromptForbidsInvention(t *testing.T) {
	for _, phrase := range []string{
		"Do NOT invent",
		"SQL",
		"company systems",
	} {
		if !strings.Contains(llm.SystemPrompt, phrase) {
			t.Fatalf("system prompt missing %q", phrase)
		}
	}
}

func TestAppendSynthesisPreservesPlaybook(t *testing.T) {
	base := "# Title\n\n## Context\n\n- line\n\n## Attack order\n\n1. x\n"
	out := llm.AppendSynthesis(base, "## LLM synthesis\n\nHi.\n")
	if !strings.Contains(out, "## LLM synthesis") {
		t.Fatal(out)
	}
	if !strings.Contains(out, "## Attack order") {
		t.Fatal(out)
	}
}
