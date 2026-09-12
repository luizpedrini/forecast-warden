package incidents

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/models"
)

func ActionableFindings(findings []models.Finding) []models.Finding {
	var out []models.Finding
	for _, f := range findings {
		if f.Severity == models.SeverityWarning || f.Severity == models.SeverityCritical {
			out = append(out, f)
		}
	}
	return out
}

func MaxSeverity(findings []models.Finding) models.Severity {
	if len(findings) == 0 {
		return models.SeverityInfo
	}
	max := findings[0].Severity
	for _, f := range findings[1:] {
		if f.Severity.Rank() > max.Rank() {
			max = f.Severity
		}
	}
	return max
}

func SuggestHypotheses(findings []models.Finding) []models.Hypothesis {
	codes := map[string]struct{}{}
	for _, f := range findings {
		codes[f.Code] = struct{}{}
	}
	var hyps []models.Hypothesis
	if _, ok := codes["HighMAPE"]; ok {
		hyps = append(hyps, models.HypDemandSpike, models.HypModelStale)
	}
	if _, ok := codes["BiasShift"]; ok {
		hyps = append(hyps, models.HypFeatureBreak, models.HypCalendar)
	}
	if _, ok := codes["CoverageBreak"]; ok {
		hyps = append(hyps, models.HypModelStale)
	}
	if _, ok := codes["LowSupport"]; ok {
		hyps = append(hyps, models.HypDataDelay)
	}
	seen := map[models.Hypothesis]struct{}{}
	var out []models.Hypothesis
	for _, h := range hyps {
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	if len(out) == 0 {
		return []models.Hypothesis{models.HypModelStale}
	}
	return out
}

func SuggestAction(severity models.Severity, findings []models.Finding) models.SuggestedAction {
	codes := map[string]struct{}{}
	for _, f := range findings {
		codes[f.Code] = struct{}{}
	}
	if severity == models.SeverityCritical {
		return models.ActionHoldPromotion
	}
	_, bias := codes["BiasShift"]
	_, mape := codes["HighMAPE"]
	if bias && mape {
		return models.ActionRetrain
	}
	return models.ActionInvestigate
}

func MakeIncidentID(runID string, findings []models.Finding) string {
	compact := strings.ReplaceAll(runID, "-", "")
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		parts = append(parts, fmt.Sprintf("%s:%s:%s", f.Code, f.EntityID, f.Severity))
	}
	sort.Strings(parts)
	sum := sha1.Sum([]byte(strings.Join(parts, "|")))
	short := hex.EncodeToString(sum[:])[:4]
	return fmt.Sprintf("fw-%s-%s", compact, short)
}

func BuildIncident(runID string, findings []models.Finding, createdAt time.Time) *models.Incident {
	action := ActionableFindings(findings)
	if len(action) == 0 {
		return nil
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	severity := MaxSeverity(action)
	entitySet := map[string]struct{}{}
	codeSet := map[string]struct{}{}
	for _, f := range action {
		entitySet[f.EntityID] = struct{}{}
		codeSet[f.Code] = struct{}{}
	}
	entities := make([]string, 0, len(entitySet))
	for e := range entitySet {
		entities = append(entities, e)
	}
	sort.Strings(entities)
	codes := make([]string, 0, len(codeSet))
	for c := range codeSet {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	return &models.Incident{
		ID:              MakeIncidentID(runID, action),
		RunID:           runID,
		Status:          models.StatusOpen,
		Severity:        severity,
		Entities:        entities,
		DetectorCodes:   codes,
		CreatedAt:       createdAt,
		Findings:        findings,
		Hypotheses:      SuggestHypotheses(findings),
		SuggestedAction: SuggestAction(severity, action),
		Note:            nil,
		ResolvedAt:      nil,
	}
}

func IncidentPaths(incidentsDir string, incident *models.Incident) (mdPath, jsonPath string) {
	parts := strings.Split(incident.ID, "-")
	short := parts[len(parts)-1]
	stem := fmt.Sprintf("%s__%s", incident.RunID, short)
	return filepath.Join(incidentsDir, stem+".md"), filepath.Join(incidentsDir, stem+".json")
}

func WriteIncident(incidentsDir string, incident *models.Incident) (mdPath, jsonPath string, err error) {
	if err := os.MkdirAll(incidentsDir, 0o755); err != nil {
		return "", "", err
	}
	mdPath, jsonPath = IncidentPaths(incidentsDir, incident)
	data, err := json.MarshalIndent(incident, "", "  ")
	if err != nil {
		return "", "", err
	}
	data = append(data, '\n')
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(mdPath, []byte(RenderMarkdown(incident)), 0o644); err != nil {
		return "", "", err
	}
	return mdPath, jsonPath, nil
}

func RenderMarkdown(incident *models.Incident) string {
	ents := make([]string, len(incident.Entities))
	for i, e := range incident.Entities {
		ents[i] = fmt.Sprintf("`%s`", e)
	}
	entities := strings.Join(ents, ", ")
	codes := strings.Join(incident.DetectorCodes, ", ")
	hypLines := make([]string, 0, len(incident.Hypotheses))
	for _, h := range incident.Hypotheses {
		hypLines = append(hypLines, fmt.Sprintf("- `%s`", h))
	}
	rows := make([]string, 0, len(incident.Findings))
	for _, f := range incident.Findings {
		evParts := make([]string, 0, len(f.Evidence))
		keys := make([]string, 0, len(f.Evidence))
		for k := range f.Evidence {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			evParts = append(evParts, fmt.Sprintf("%s=%v", k, f.Evidence[k]))
		}
		rows = append(rows, fmt.Sprintf("| %s | %s | %s | %s | %s |",
			f.Code, f.Severity, f.EntityID, strings.Join(evParts, ", "), f.Hint))
	}
	table := strings.Join(rows, "\n")
	if table == "" {
		table = "| — | — | — | — | — |"
	}
	gate := "No gate."
	if incident.Severity == models.SeverityWarning || incident.Severity == models.SeverityCritical {
		gate = "Model promotion / config change blocked while status is `open` (resolve or accept-risk first)."
	}
	noteBlock := ""
	if incident.Note != nil && *incident.Note != "" {
		noteBlock = fmt.Sprintf("\n**Resolution note:** %s\n", *incident.Note)
	}
	entitiesYAML, _ := json.Marshal(incident.Entities)
	codesYAML, _ := json.Marshal(incident.DetectorCodes)
	return fmt.Sprintf(`---
id: %s
run_id: "%s"
status: %s
severity: %s
entities: %s
detector_codes: %s
created_at: %s
suggested_action: %s
---

# Forecast Incident `+"`%s`"+`

## Symptom

Run `+"`%s`"+` flagged %s via %s
(severity **%s**). Review evidence before promoting the model.
%s
## Evidence

| detector | severity | entity | evidence | hint |
|----------|----------|--------|----------|------|
%s

## Hypotheses (candidate)

%s

## Suggested action

`+"`%s`"+`

## Gate

%s
`,
		incident.ID,
		incident.RunID,
		incident.Status,
		incident.Severity,
		string(entitiesYAML),
		string(codesYAML),
		incident.CreatedAt.Format(time.RFC3339Nano),
		incident.SuggestedAction,
		incident.ID,
		incident.RunID,
		entities,
		codes,
		incident.Severity,
		noteBlock,
		table,
		strings.Join(hypLines, "\n"),
		incident.SuggestedAction,
		gate,
	)
}

func ListIncidents(incidentsDir string, status *models.IncidentStatus) ([]models.Incident, error) {
	entries, err := filepath.Glob(filepath.Join(incidentsDir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(entries)
	var out []models.Incident
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var inc models.Incident
		if err := json.Unmarshal(data, &inc); err != nil {
			return nil, err
		}
		if status != nil && inc.Status != *status {
			continue
		}
		out = append(out, inc)
	}
	return out, nil
}

func LoadIncident(incidentsDir, incidentID string) (*models.Incident, error) {
	incs, err := ListIncidents(incidentsDir, nil)
	if err != nil {
		return nil, err
	}
	for i := range incs {
		if incs[i].ID == incidentID {
			return &incs[i], nil
		}
	}
	entries, err := filepath.Glob(filepath.Join(incidentsDir, "*.json"))
	if err != nil {
		return nil, err
	}
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var inc models.Incident
		if err := json.Unmarshal(data, &inc); err != nil {
			continue
		}
		stem := strings.TrimSuffix(filepath.Base(path), ".json")
		short := ""
		if parts := strings.Split(stem, "__"); len(parts) > 1 {
			short = parts[len(parts)-1]
		}
		if incidentID == inc.ID || incidentID == stem || incidentID == short {
			return &inc, nil
		}
	}
	return nil, nil
}

func ResolveIncident(incidentsDir, incidentID, note string, status models.IncidentStatus) (*models.Incident, error) {
	inc, err := LoadIncident(incidentsDir, incidentID)
	if err != nil {
		return nil, err
	}
	if inc == nil {
		return nil, nil
	}
	now := time.Now().UTC()
	inc.Status = status
	inc.Note = &note
	inc.ResolvedAt = &now
	if _, _, err := WriteIncident(incidentsDir, inc); err != nil {
		return nil, err
	}
	return inc, nil
}
