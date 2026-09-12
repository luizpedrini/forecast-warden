package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/models"
)

// SystemPrompt is the hard constraint prompt for investigate enrichment.
const SystemPrompt = `You enrich a forecast-warden investigation playbook. You receive ONLY the incident JSON and the rule-based playbook markdown.

Hard rules:
- Do NOT invent tables, SQL, company systems, warehouses, internal tool names, or root causes not grounded in the provided evidence.
- Prefer ranking the existing hypothesis enums from the incident (demand_spike, feature_break, calendar, model_stale, data_delay) and codes already in the playbook.
- Stay in English, consistent with the playbook.
- Do NOT change detector thresholds, auto-resolve, or invent metrics.
- Output a single JSON object only (no markdown fences) with keys:
  {
    "synthesis": "<1 short paragraph summarizing the situation from the evidence>",
    "hypotheses": [
      {"name": "<enum or short label>", "why": "<one-line why grounded in evidence>"},
      ... up to 3, ranked best-first
    ],
    "note": "<optional refined one-line for warden resolve --note; omit or empty if nothing better>"
  }`

// RankedHypothesis is one ranked hypothesis from the LLM.
type RankedHypothesis struct {
	Name string `json:"name"`
	Why  string `json:"why"`
}

// Enrichment is structured LLM output for "## LLM synthesis".
type Enrichment struct {
	Synthesis  string             `json:"synthesis"`
	Hypotheses []RankedHypothesis `json:"hypotheses"`
	Note       string             `json:"note"`
}

// ParseEnrichment extracts Enrichment from model text (raw JSON or fenced).
func ParseEnrichment(raw string) (*Enrichment, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, fmt.Errorf("empty LLM response")
	}
	// Strip optional ```json ... ``` fences.
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSpace(s)
		if strings.HasPrefix(strings.ToLower(s), "json") {
			s = strings.TrimSpace(s[4:])
		}
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
	}
	// If extra prose wraps JSON, take the outermost object.
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var e Enrichment
	if err := json.Unmarshal([]byte(s), &e); err != nil {
		return nil, fmt.Errorf("parse enrichment JSON: %w", err)
	}
	e.Synthesis = strings.TrimSpace(e.Synthesis)
	e.Note = strings.TrimSpace(e.Note)
	cleaned := make([]RankedHypothesis, 0, len(e.Hypotheses))
	for _, h := range e.Hypotheses {
		h.Name = strings.TrimSpace(h.Name)
		h.Why = strings.TrimSpace(h.Why)
		if h.Name == "" && h.Why == "" {
			continue
		}
		cleaned = append(cleaned, h)
		if len(cleaned) >= 3 {
			break
		}
	}
	e.Hypotheses = cleaned
	if e.Synthesis == "" && len(e.Hypotheses) == 0 {
		return nil, fmt.Errorf("enrichment missing synthesis and hypotheses")
	}
	return &e, nil
}

// FormatSection renders the "## LLM synthesis" markdown block.
func FormatSection(e *Enrichment) string {
	if e == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("## LLM synthesis\n\n")
	if e.Synthesis != "" {
		b.WriteString(e.Synthesis)
		b.WriteString("\n\n")
	}
	if len(e.Hypotheses) > 0 {
		b.WriteString("**Ranked hypotheses**\n\n")
		for i, h := range e.Hypotheses {
			name := h.Name
			if name == "" {
				name = "hypothesis"
			}
			why := h.Why
			if why == "" {
				why = "(no rationale)"
			}
			fmt.Fprintf(&b, "%d. `%s` — %s\n", i+1, name, why)
		}
		b.WriteString("\n")
	}
	if e.Note != "" {
		fmt.Fprintf(&b, "**Optional refined `--note`:** %s\n\n", e.Note)
	}
	return b.String()
}

// BuildUserPrompt sends only incident JSON + rule-based playbook.
func BuildUserPrompt(inc *models.Incident, playbookMD string) (string, error) {
	if inc == nil {
		return "", fmt.Errorf("nil incident")
	}
	raw, err := json.MarshalIndent(inc, "", "  ")
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("## Incident JSON\n\n```json\n")
	b.Write(raw)
	b.WriteString("\n```\n\n## Rule-based playbook\n\n")
	b.WriteString(playbookMD)
	return b.String(), nil
}

// AppendSynthesis inserts the LLM section after Context (or at top after title).
func AppendSynthesis(playbookMD, section string) string {
	section = strings.TrimSpace(section)
	if section == "" {
		return playbookMD
	}
	if !strings.HasSuffix(section, "\n") {
		section += "\n"
	}
	// Prefer after ## Context block: find next ## after Context.
	const ctx = "## Context\n"
	idx := strings.Index(playbookMD, ctx)
	if idx < 0 {
		// Prepend after first heading line.
		if nl := strings.Index(playbookMD, "\n"); nl >= 0 {
			return playbookMD[:nl+1] + "\n" + section + "\n" + playbookMD[nl+1:]
		}
		return section + "\n" + playbookMD
	}
	rest := playbookMD[idx+len(ctx):]
	next := strings.Index(rest, "\n## ")
	if next < 0 {
		return playbookMD + "\n" + section
	}
	insertAt := idx + len(ctx) + next + 1 // position of "## "
	return playbookMD[:insertAt] + section + "\n" + playbookMD[insertAt:]
}

// EnrichPlaybook calls the client and merges "## LLM synthesis" into the playbook.
// On any error, returns ("", err) so the caller can fall back to rule-based only.
func EnrichPlaybook(ctx context.Context, client Client, inc *models.Incident, playbookMD string) (string, error) {
	if client == nil {
		return "", fmt.Errorf("nil LLM client")
	}
	user, err := BuildUserPrompt(inc, playbookMD)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
	}
	raw, err := client.Complete(ctx, SystemPrompt, user)
	if err != nil {
		return "", err
	}
	e, err := ParseEnrichment(raw)
	if err != nil {
		return "", err
	}
	section := FormatSection(e)
	return AppendSynthesis(playbookMD, section), nil
}
