// Package investigate builds a rule-based investigation playbook from a
// persisted Forecast Incident. No LLM, no network, no warehouse access.
package investigate

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/luizpedrini/forecast-warden/internal/models"
)

// PlaybookFilename returns incidents/<id>.investigate.md
func PlaybookFilename(incidentID string) string {
	return incidentID + ".investigate.md"
}

// PlaybookPath joins incidentsDir with PlaybookFilename.
func PlaybookPath(incidentsDir, incidentID string) string {
	return filepath.Join(incidentsDir, PlaybookFilename(incidentID))
}

// attackRank prioritizes investigation order:
// UnstableForecast → BiasShift → HighWAPE → other thresholds → LowSupport last.
func attackRank(code string) int {
	switch code {
	case "UnstableForecast":
		return 0
	case "BiasShift":
		return 1
	case "HighWAPE":
		return 2
	case "HighMAPE", "HighRMSE", "CoverageBreak":
		return 3
	case "LowSupport":
		return 100
	default:
		return 50
	}
}

// OrderFindings returns findings in attack order (stable within same rank:
// higher severity first, then entity_id, then code).
func OrderFindings(findings []models.Finding) []models.Finding {
	out := make([]models.Finding, len(findings))
	copy(out, findings)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := attackRank(out[i].Code), attackRank(out[j].Code)
		if ri != rj {
			return ri < rj
		}
		if out[i].Severity.Rank() != out[j].Severity.Rank() {
			return out[i].Severity.Rank() > out[j].Severity.Rank()
		}
		if out[i].EntityID != out[j].EntityID {
			return out[i].EntityID < out[j].EntityID
		}
		return out[i].Code < out[j].Code
	})
	return out
}

type detectorTemplate struct {
	Question    string
	LookOutside []string
}

var templates = map[string]detectorTemplate{
	"UnstableForecast": {
		Question: "Is forecast churn (origin-to-origin change) concentrated in a few entities, or systemic?",
		LookOutside: []string{
			"Compare point forecasts across consecutive origins for the same target horizon",
			"Check whether a freeze / commit policy should damp late revisions",
			"Look for recent feature or pipeline changes that flip predictions between runs",
			"Inspect planning nervousness: did ops act on earlier origins that later flipped?",
		},
	},
	"BiasShift": {
		Question: "Is the bias a level shift (all horizons) or a shape change (near-term vs far)?",
		LookOutside: []string{
			"Plot bias over recent runs vs baseline mean/std",
			"Check calendar / holiday / promo overlays for the flagged entities",
			"Diff feature distributions or known input joins that could systematically tilt predictions",
			"Confirm actuals lag or delayed ground truth is not inventing a fake bias",
		},
	},
	"HighWAPE": {
		Question: "Is WAPE driven by a few high-volume entities, or broad error across the cohort?",
		LookOutside: []string{
			"Compare actuals vs yhat for top volume drivers in the flagged entities",
			"Segment error by volume quintile — is it a head or a long-tail problem?",
			"Check for demand spikes, stockouts, or catalog changes not in the model",
			"Verify metric definition (WAPE numerator/denominator) matches the batch contract",
		},
	},
	"HighMAPE": {
		Question: "Are percentage errors inflated by low-volume entities where MAPE is unstable?",
		LookOutside: []string{
			"Compare MAPE vs WAPE on the same entities — prefer volume-weighted view if they disagree",
			"Review actuals vs yhat on a sample of flagged entities",
			"Check for near-zero actuals that blow up percentage error",
		},
	},
	"HighRMSE": {
		Question: "Is RMSE dominated by a handful of large absolute misses?",
		LookOutside: []string{
			"Rank entities by absolute residual; inspect the top offenders",
			"Confirm units/scale of the metric match training targets",
			"Look for outliers or data quality issues in actuals",
		},
	},
	"CoverageBreak": {
		Question: "Are prediction intervals miscalibrated, or are actuals unusually volatile this run?",
		LookOutside: []string{
			"Check coverage over a rolling window, not a single run",
			"Inspect interval width vs historical — did uncertainty collapse incorrectly?",
			"Verify quantile / interval construction in the forecast batch",
		},
	},
	"LowSupport": {
		Question: "Is low support temporary (data delay) or structural (sparse entity)?",
		LookOutside: []string{
			"Confirm actuals ingestion lag for the entity",
			"Decide whether to exclude from promotion gates until support recovers",
			"Check if the entity is new, retired, or mis-keyed",
		},
	},
}

func templateFor(code string) detectorTemplate {
	if t, ok := templates[code]; ok {
		return t
	}
	return detectorTemplate{
		Question: fmt.Sprintf("What changed for detector `%s` on this run vs recent baseline?", code),
		LookOutside: []string{
			"Compare the flagged metric to the previous few runs for the same entities",
			"Review actuals vs point forecast for a sample of entities",
			"Check calendar, catalog, and pipeline diffs outside the warden",
		},
	}
}

func formatEvidence(ev map[string]any) string {
	if len(ev) == 0 {
		return "(no numeric evidence on finding)"
	}
	keys := make([]string, 0, len(ev))
	for k := range ev {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, ev[k]))
	}
	return strings.Join(parts, ", ")
}

func codeSet(findings []models.Finding) map[string]struct{} {
	s := map[string]struct{}{}
	for _, f := range findings {
		s[f.Code] = struct{}{}
	}
	return s
}

// ComboHints returns cross-detector hypotheses when multiple codes co-occur.
func ComboHints(findings []models.Finding) []string {
	codes := codeSet(findings)
	var hints []string
	_, wape := codes["HighWAPE"]
	_, mape := codes["HighMAPE"]
	_, unstable := codes["UnstableForecast"]
	_, bias := codes["BiasShift"]
	_, coverage := codes["CoverageBreak"]
	_, low := codes["LowSupport"]

	if (wape || mape) && unstable {
		hints = append(hints,
			"**HighWAPE/MAPE + UnstableForecast:** volume error may be coupled with origin churn — hypothesize planning nervousness plus misscaled demand; freeze late revisions before retuning error thresholds.")
	}
	if (wape || mape) && bias {
		hints = append(hints,
			"**HighWAPE/MAPE + BiasShift:** systematic level error with large abs % — strong retrain / feature-break candidate; hold promotion until bias direction is explained.")
	}
	if unstable && bias {
		hints = append(hints,
			"**UnstableForecast + BiasShift:** predictions both drift in level and flip between origins — check feature pipeline or training data cuts that changed mid-week.")
	}
	if coverage && (wape || mape || bias) {
		hints = append(hints,
			"**CoverageBreak + error/bias:** intervals and point forecast both degraded — treat as model health, not a single-threshold flake.")
	}
	if low && (wape || mape || bias || unstable) {
		hints = append(hints,
			"**LowSupport alongside actionable findings:** treat sparse entities as secondary; do not let info-only support drive the resolve decision.")
	}
	return hints
}

// SuggestDecision maps incident severity + finding mix to a resolve-oriented action.
func SuggestDecision(inc *models.Incident) models.SuggestedAction {
	if inc.SuggestedAction != "" {
		// Prefer the incident's own suggestion when set, but refine for investigate playbook clarity.
		switch inc.SuggestedAction {
		case models.ActionHoldPromotion, models.ActionRetrain, models.ActionRetuneThreshold,
			models.ActionAcceptedRisk, models.ActionInvestigate:
			// Fall through to severity/code refinement below when useful.
		}
	}
	codes := codeSet(inc.Findings)
	_, wape := codes["HighWAPE"]
	_, mape := codes["HighMAPE"]
	_, unstable := codes["UnstableForecast"]
	_, bias := codes["BiasShift"]

	if inc.Severity == models.SeverityCritical {
		if unstable && (wape || mape) {
			return models.ActionHoldPromotion
		}
		if bias && (wape || mape) {
			return models.ActionRetrain
		}
		return models.ActionHoldPromotion
	}
	if bias && (wape || mape) {
		return models.ActionRetrain
	}
	if unstable && !wape && !mape && !bias {
		return models.ActionRetuneThreshold
	}
	if inc.SuggestedAction != "" {
		return inc.SuggestedAction
	}
	return models.ActionInvestigate
}

func readyNote(inc *models.Incident, decision models.SuggestedAction) string {
	ents := strings.Join(inc.Entities, ",")
	codes := strings.Join(inc.DetectorCodes, "+")
	switch decision {
	case models.ActionHoldPromotion:
		return fmt.Sprintf("investigated %s on run %s (%s); hold promotion until stability/WAPE recover", ents, inc.RunID, codes)
	case models.ActionRetrain:
		return fmt.Sprintf("investigated %s on run %s (%s); bias+error → schedule retrain", ents, inc.RunID, codes)
	case models.ActionRetuneThreshold:
		return fmt.Sprintf("investigated %s on run %s (%s); thresholds noisy — retune before reopen", ents, inc.RunID, codes)
	case models.ActionAcceptedRisk:
		return fmt.Sprintf("investigated %s on run %s (%s); accepted risk for this cycle", ents, inc.RunID, codes)
	default:
		return fmt.Sprintf("investigated %s on run %s (%s); need more evidence outside warden", ents, inc.RunID, codes)
	}
}

func checklist(ordered []models.Finding, codes map[string]struct{}) []string {
	items := []string{
		"[ ] Read Context + Attack order; confirm severity and suggested action make sense",
		"[ ] Walk Per-finding questions in attack order (stability before WAPE when both present)",
	}
	if _, ok := codes["UnstableForecast"]; ok {
		items = append(items, "[ ] Outside warden: sample origin-to-origin forecast diffs for flagged entities")
	}
	if _, ok := codes["BiasShift"]; ok {
		items = append(items, "[ ] Outside warden: check calendar/promo and feature diffs vs bias direction")
	}
	if _, ok := codes["HighWAPE"]; ok || hasCode(codes, "HighMAPE") {
		items = append(items, "[ ] Outside warden: actuals vs yhat for top volume drivers")
	}
	items = append(items,
		"[ ] Decide: hold_promotion / retrain / retune_threshold / accepted_risk / keep investigating",
		"[ ] Close with `warden resolve <id> --note \"...\"` using the ready-made note (edit as needed)",
	)
	// Cap at 7; ensure at least 5.
	if len(items) > 7 {
		items = items[:7]
	}
	return items
}

func hasCode(codes map[string]struct{}, c string) bool {
	_, ok := codes[c]
	return ok
}

// Render builds the full markdown investigation playbook.
func Render(inc *models.Incident) string {
	if inc == nil {
		return ""
	}
	ordered := OrderFindings(inc.Findings)
	codes := codeSet(inc.Findings)
	decision := SuggestDecision(inc)
	note := readyNote(inc, decision)

	ents := make([]string, len(inc.Entities))
	for i, e := range inc.Entities {
		ents[i] = "`" + e + "`"
	}
	detCodes := make([]string, len(inc.DetectorCodes))
	for i, c := range inc.DetectorCodes {
		detCodes[i] = "`" + c + "`"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Investigation playbook `%s`\n\n", inc.ID)

	b.WriteString("## Context\n\n")
	fmt.Fprintf(&b, "- **Run** `%s` · **status** `%s` · **severity** **%s**\n",
		inc.RunID, inc.Status, inc.Severity)
	fmt.Fprintf(&b, "- **Entities:** %s · **detectors:** %s\n",
		strings.Join(ents, ", "), strings.Join(detCodes, ", "))
	fmt.Fprintf(&b, "- **Incident suggested action:** `%s` → playbook decision **`%s`**\n\n",
		inc.SuggestedAction, decision)

	b.WriteString("## Attack order\n\n")
	b.WriteString("Investigate in this order (stability before WAPE; LowSupport last):\n\n")
	if len(ordered) == 0 {
		b.WriteString("_No findings on incident._\n\n")
	} else {
		for i, f := range ordered {
			fmt.Fprintf(&b, "%d. **%s** (`%s`, entity `%s`)\n", i+1, f.Code, f.Severity, f.EntityID)
		}
		b.WriteString("\n")
	}

	combos := ComboHints(inc.Findings)
	if len(combos) > 0 {
		b.WriteString("### Combo hints\n\n")
		for _, h := range combos {
			fmt.Fprintf(&b, "- %s\n", h)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Per-finding questions\n\n")
	if len(ordered) == 0 {
		b.WriteString("_Nothing to probe._\n\n")
	} else {
		seen := map[string]int{}
		for _, f := range ordered {
			seen[f.Code]++
			suffix := ""
			if seen[f.Code] > 1 || countCode(ordered, f.Code) > 1 {
				suffix = fmt.Sprintf(" · `%s`", f.EntityID)
			}
			tpl := templateFor(f.Code)
			fmt.Fprintf(&b, "### %s%s (%s)\n\n", f.Code, suffix, f.Severity)
			fmt.Fprintf(&b, "**Key question:** %s\n\n", tpl.Question)
			fmt.Fprintf(&b, "**Evidence already on the incident:** %s\n\n", formatEvidence(f.Evidence))
			if f.Hint != "" {
				fmt.Fprintf(&b, "**Detector hint:** %s\n\n", f.Hint)
			}
			b.WriteString("**Look outside the warden** (generic — no company SQL):\n\n")
			for _, step := range tpl.LookOutside {
				fmt.Fprintf(&b, "- %s\n", step)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("## 15-min checklist\n\n")
	for _, item := range checklist(ordered, codes) {
		fmt.Fprintf(&b, "%s\n", item)
	}
	b.WriteString("\n")

	b.WriteString("## Suggested resolve decision\n\n")
	fmt.Fprintf(&b, "`%s`\n\n", decision)
	b.WriteString("Map to `warden resolve` status as appropriate: `resolved` / `accepted-risk` / `wontfix`. ")
	b.WriteString("Action enums: `hold_promotion` · `retrain` · `retune_threshold` · `accepted_risk` · `investigate`.\n\n")

	b.WriteString("## Ready-made `--note` line\n\n")
	fmt.Fprintf(&b, "```\nwarden resolve %s --note \"%s\"\n```\n", inc.ID, note)

	return b.String()
}

func countCode(findings []models.Finding, code string) int {
	n := 0
	for _, f := range findings {
		if f.Code == code {
			n++
		}
	}
	return n
}
