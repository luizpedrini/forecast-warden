package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"

	"github.com/luizpedrini/forecast-warden/internal/incidents"
	"github.com/luizpedrini/forecast-warden/internal/models"
)

// BigQueryConfig holds the parsed connection parameters for the BigQuery driver.
// Populated from store.Config.DSN ("project/dataset") or its zero value defaults.
type BigQueryConfig struct {
	// ProjectID is the GCP project that owns the dataset.
	ProjectID string
	// Dataset is the BigQuery dataset name (default: "forecast_warden").
	Dataset string
	// IncidentsTable is the BQ table for incidents (default: "incidents").
	IncidentsTable string
	// FindingsTable is the BQ table for findings (default: "findings").
	FindingsTable string
}

// parseBigQueryDSN parses a DSN in the form:
//
//	project               → project=project
//	project/dataset       → project=project, dataset=dataset
//
// Empty segments keep their defaults.
func parseBigQueryDSN(dsn string) BigQueryConfig {
	cfg := BigQueryConfig{
		Dataset:        "forecast_warden",
		IncidentsTable: "incidents",
		FindingsTable:  "findings",
	}
	parts := strings.SplitN(strings.TrimSpace(dsn), "/", 2)
	if len(parts) >= 1 && parts[0] != "" {
		cfg.ProjectID = parts[0]
	}
	if len(parts) >= 2 && parts[1] != "" {
		cfg.Dataset = parts[1]
	}
	return cfg
}

type bigQueryStore struct {
	client        *bigquery.Client
	bqCfg         BigQueryConfig
	writeMarkdown bool
	incidentsDir  string
}

func openBigQuery(cfg Config) (Store, error) {
	bqCfg := parseBigQueryDSN(cfg.DSN)
	if bqCfg.ProjectID == "" {
		return nil, fmt.Errorf(
			"store bigquery: project ID is required — set DSN to \"<project>\" or \"<project>/<dataset>\"")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := bigquery.NewClient(ctx, bqCfg.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("store bigquery: create client: %w", err)
	}

	s := &bigQueryStore{
		client:        client,
		bqCfg:         bqCfg,
		writeMarkdown: cfg.WriteMarkdown,
		incidentsDir:  cfg.IncidentsDir,
	}

	// Ensure dataset and tables exist.
	if err := s.migrate(ctx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("store bigquery: migrate: %w", err)
	}

	return s, nil
}

// migrate creates the dataset (if absent) and both tables using DDL queries.
func (s *bigQueryStore) migrate(ctx context.Context) error {
	// Ensure dataset exists.
	ds := s.client.Dataset(s.bqCfg.Dataset)
	if _, err := ds.Metadata(ctx); err != nil {
		if err := ds.Create(ctx, &bigquery.DatasetMetadata{}); err != nil {
			// Race-safe: another process may have created it between Metadata and Create.
			if !strings.Contains(err.Error(), "Already Exists") {
				return fmt.Errorf("create dataset %q: %w", s.bqCfg.Dataset, err)
			}
		}
	}

	ddls := []string{
		fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s.%s.%s (
  id                STRING NOT NULL,
  run_id            STRING NOT NULL,
  status            STRING NOT NULL,
  severity          STRING NOT NULL,
  entities          STRING,
  detector_codes    STRING,
  suggested_action  STRING NOT NULL,
  note              STRING,
  created_at        TIMESTAMP NOT NULL,
  resolved_at       TIMESTAMP,
  hypotheses        STRING,
  raw_json          STRING
)`, s.bqCfg.ProjectID, s.bqCfg.Dataset, s.bqCfg.IncidentsTable),
		fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s.%s.%s (
  id          STRING NOT NULL,
  incident_id STRING NOT NULL,
  code        STRING NOT NULL,
  severity    STRING NOT NULL,
  entity_id   STRING NOT NULL,
  evidence    STRING,
  hint        STRING NOT NULL
)`, s.bqCfg.ProjectID, s.bqCfg.Dataset, s.bqCfg.FindingsTable),
	}

	for _, ddl := range ddls {
		q := s.client.Query(ddl)
		job, err := q.Run(ctx)
		if err != nil {
			return fmt.Errorf("run DDL: %w", err)
		}
		if _, err := job.Wait(ctx); err != nil {
			return fmt.Errorf("wait DDL: %w", err)
		}
	}
	return nil
}

func (s *bigQueryStore) Close() error {
	if s.client == nil {
		return nil
	}
	return s.client.Close()
}

// qualRef returns the fully-qualified table reference for use in DML.
func (s *bigQueryStore) qualRef(table string) string {
	return fmt.Sprintf("`%s.%s.%s`", s.bqCfg.ProjectID, s.bqCfg.Dataset, table)
}

func (s *bigQueryStore) SaveIncident(ctx context.Context, incident models.Incident) error {
	entitiesJSON, err := json.Marshal(incident.Entities)
	if err != nil {
		return err
	}
	codesJSON, err := json.Marshal(incident.DetectorCodes)
	if err != nil {
		return err
	}
	hypsJSON, err := json.Marshal(incident.Hypotheses)
	if err != nil {
		return err
	}
	rawJSON, err := json.Marshal(incident)
	if err != nil {
		return err
	}

	createdAt := incident.CreatedAt.UTC().Format(time.RFC3339Nano)
	var resolvedAt interface{} = nil
	if incident.ResolvedAt != nil {
		resolvedAt = incident.ResolvedAt.UTC().Format(time.RFC3339Nano)
	}
	var note interface{} = nil
	if incident.Note != nil {
		note = *incident.Note
	}

	// MERGE (upsert) — BigQuery supports MERGE DML.
	mergeSQL := fmt.Sprintf(`
MERGE %s AS T
USING (SELECT
  @id             AS id,
  @run_id         AS run_id,
  @status         AS status,
  @severity       AS severity,
  @entities       AS entities,
  @detector_codes AS detector_codes,
  @action         AS suggested_action,
  @note           AS note,
  TIMESTAMP(@created_at) AS created_at,
  IF(@resolved_at IS NULL, NULL, TIMESTAMP(CAST(@resolved_at AS STRING))) AS resolved_at,
  @hypotheses     AS hypotheses,
  @raw_json       AS raw_json
) AS S ON T.id = S.id
WHEN MATCHED THEN UPDATE SET
  run_id=S.run_id, status=S.status, severity=S.severity,
  entities=S.entities, detector_codes=S.detector_codes,
  suggested_action=S.suggested_action, note=S.note,
  created_at=S.created_at, resolved_at=S.resolved_at,
  hypotheses=S.hypotheses, raw_json=S.raw_json
WHEN NOT MATCHED THEN INSERT (
  id, run_id, status, severity, entities, detector_codes,
  suggested_action, note, created_at, resolved_at, hypotheses, raw_json
) VALUES (
  S.id, S.run_id, S.status, S.severity, S.entities, S.detector_codes,
  S.suggested_action, S.note, S.created_at, S.resolved_at, S.hypotheses, S.raw_json
)`, s.qualRef(s.bqCfg.IncidentsTable))

	q := s.client.Query(mergeSQL)
	q.Parameters = []bigquery.QueryParameter{
		{Name: "id", Value: incident.ID},
		{Name: "run_id", Value: incident.RunID},
		{Name: "status", Value: string(incident.Status)},
		{Name: "severity", Value: string(incident.Severity)},
		{Name: "entities", Value: string(entitiesJSON)},
		{Name: "detector_codes", Value: string(codesJSON)},
		{Name: "action", Value: string(incident.SuggestedAction)},
		{Name: "note", Value: note},
		{Name: "created_at", Value: createdAt},
		{Name: "resolved_at", Value: resolvedAt},
		{Name: "hypotheses", Value: string(hypsJSON)},
		{Name: "raw_json", Value: string(rawJSON)},
	}
	job, err := q.Run(ctx)
	if err != nil {
		return fmt.Errorf("bigquery upsert incident: %w", err)
	}
	if _, err := job.Wait(ctx); err != nil {
		return fmt.Errorf("bigquery upsert incident: %w", err)
	}

	// Replace findings: delete existing rows then insert new ones.
	delSQL := fmt.Sprintf(`DELETE FROM %s WHERE incident_id = @incident_id`,
		s.qualRef(s.bqCfg.FindingsTable))
	dq := s.client.Query(delSQL)
	dq.Parameters = []bigquery.QueryParameter{
		{Name: "incident_id", Value: incident.ID},
	}
	dJob, err := dq.Run(ctx)
	if err != nil {
		return fmt.Errorf("bigquery delete findings: %w", err)
	}
	if _, err := dJob.Wait(ctx); err != nil {
		return fmt.Errorf("bigquery delete findings: %w", err)
	}

	for i, f := range incident.Findings {
		evJSON, err := json.Marshal(f.Evidence)
		if err != nil {
			return err
		}
		if evJSON == nil {
			evJSON = []byte("{}")
		}
		findingID := fmt.Sprintf("%s_%d", incident.ID, i)
		insSQL := fmt.Sprintf(`
INSERT INTO %s (id, incident_id, code, severity, entity_id, evidence, hint)
VALUES (@id, @incident_id, @code, @severity, @entity_id, @evidence, @hint)`,
			s.qualRef(s.bqCfg.FindingsTable))
		iq := s.client.Query(insSQL)
		iq.Parameters = []bigquery.QueryParameter{
			{Name: "id", Value: findingID},
			{Name: "incident_id", Value: incident.ID},
			{Name: "code", Value: f.Code},
			{Name: "severity", Value: string(f.Severity)},
			{Name: "entity_id", Value: f.EntityID},
			{Name: "evidence", Value: string(evJSON)},
			{Name: "hint", Value: f.Hint},
		}
		iJob, err := iq.Run(ctx)
		if err != nil {
			return fmt.Errorf("bigquery insert finding %d: %w", i, err)
		}
		if _, err := iJob.Wait(ctx); err != nil {
			return fmt.Errorf("bigquery insert finding %d: %w", i, err)
		}
	}

	if s.writeMarkdown {
		if err := os.MkdirAll(s.incidentsDir, 0o755); err != nil {
			return err
		}
		mdPath, _ := incidents.IncidentPaths(s.incidentsDir, &incident)
		if err := os.WriteFile(mdPath, []byte(incidents.RenderMarkdown(&incident)), 0o644); err != nil {
			return fmt.Errorf("write markdown sidecar: %w", err)
		}
	}
	return nil
}

func (s *bigQueryStore) GetIncident(ctx context.Context, id string) (models.Incident, error) {
	inc, err := s.getByExactID(ctx, id)
	if err == nil {
		return inc, nil
	}
	if err != ErrNotFound {
		return models.Incident{}, err
	}
	// Short-hash / stem fallback — scan all incidents.
	all, err := s.ListIncidents(ctx, ListFilter{})
	if err != nil {
		return models.Incident{}, err
	}
	for _, inc := range all {
		if matchIncidentID(inc, id) {
			return inc, nil
		}
	}
	return models.Incident{}, ErrNotFound
}

func (s *bigQueryStore) GetIncidentByRunID(ctx context.Context, runID string) (models.Incident, error) {
	sql := fmt.Sprintf(`
SELECT id, run_id, status, severity, entities, detector_codes,
       suggested_action, note, created_at, resolved_at, hypotheses, raw_json
FROM %s
WHERE run_id = @run_id
LIMIT 1`, s.qualRef(s.bqCfg.IncidentsTable))
	q := s.client.Query(sql)
	q.Parameters = []bigquery.QueryParameter{
		{Name: "run_id", Value: runID},
	}
	it, err := q.Read(ctx)
	if err != nil {
		return models.Incident{}, fmt.Errorf("bigquery get by run_id: %w", err)
	}
	return s.scanOne(ctx, it)
}

func (s *bigQueryStore) ListIncidents(ctx context.Context, filter ListFilter) ([]models.Incident, error) {
	sql := fmt.Sprintf(`
SELECT id, run_id, status, severity, entities, detector_codes,
       suggested_action, note, created_at, resolved_at, hypotheses, raw_json
FROM %s`, s.qualRef(s.bqCfg.IncidentsTable))
	var params []bigquery.QueryParameter
	if filter.Status != nil {
		sql += ` WHERE status = @status`
		params = append(params, bigquery.QueryParameter{Name: "status", Value: string(*filter.Status)})
	}
	sql += ` ORDER BY created_at ASC, id ASC`

	q := s.client.Query(sql)
	q.Parameters = params
	it, err := q.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("bigquery list incidents: %w", err)
	}

	var out []models.Incident
	for {
		var row bqIncidentRow
		if err := it.Next(&row); err == iterator.Done {
			break
		} else if err != nil {
			return nil, fmt.Errorf("bigquery list scan: %w", err)
		}
		inc, err := row.toIncident()
		if err != nil {
			return nil, err
		}
		findings, err := s.loadFindings(ctx, inc.ID)
		if err != nil {
			return nil, err
		}
		inc.Findings = findings
		out = append(out, inc)
	}
	return out, nil
}

func (s *bigQueryStore) UpdateStatus(ctx context.Context, id, status, note string) error {
	inc, err := s.GetIncident(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	inc.Status = models.IncidentStatus(status)
	inc.Note = &note
	inc.ResolvedAt = &now
	return s.SaveIncident(ctx, inc)
}

// ── internal helpers ──────────────────────────────────────────────────────────

func (s *bigQueryStore) getByExactID(ctx context.Context, id string) (models.Incident, error) {
	sql := fmt.Sprintf(`
SELECT id, run_id, status, severity, entities, detector_codes,
       suggested_action, note, created_at, resolved_at, hypotheses, raw_json
FROM %s
WHERE id = @id
LIMIT 1`, s.qualRef(s.bqCfg.IncidentsTable))
	q := s.client.Query(sql)
	q.Parameters = []bigquery.QueryParameter{
		{Name: "id", Value: id},
	}
	it, err := q.Read(ctx)
	if err != nil {
		return models.Incident{}, fmt.Errorf("bigquery get by id: %w", err)
	}
	return s.scanOne(ctx, it)
}

func (s *bigQueryStore) scanOne(ctx context.Context, it *bigquery.RowIterator) (models.Incident, error) {
	var row bqIncidentRow
	if err := it.Next(&row); err == iterator.Done {
		return models.Incident{}, ErrNotFound
	} else if err != nil {
		return models.Incident{}, fmt.Errorf("bigquery scan: %w", err)
	}
	inc, err := row.toIncident()
	if err != nil {
		return models.Incident{}, err
	}
	findings, err := s.loadFindings(ctx, inc.ID)
	if err != nil {
		return models.Incident{}, err
	}
	inc.Findings = findings
	return inc, nil
}

func (s *bigQueryStore) loadFindings(ctx context.Context, incidentID string) ([]models.Finding, error) {
	sql := fmt.Sprintf(`
SELECT code, severity, entity_id, evidence, hint
FROM %s
WHERE incident_id = @incident_id
ORDER BY id ASC`, s.qualRef(s.bqCfg.FindingsTable))
	q := s.client.Query(sql)
	q.Parameters = []bigquery.QueryParameter{
		{Name: "incident_id", Value: incidentID},
	}
	it, err := q.Read(ctx)
	if err != nil {
		return nil, fmt.Errorf("bigquery load findings: %w", err)
	}
	var out []models.Finding
	for {
		var row bqFindingRow
		if err := it.Next(&row); err == iterator.Done {
			break
		} else if err != nil {
			return nil, fmt.Errorf("bigquery finding scan: %w", err)
		}
		f := models.Finding{
			Code:     row.Code,
			Severity: models.Severity(row.Severity),
			EntityID: row.EntityID,
			Hint:     row.Hint,
			Evidence: map[string]any{},
		}
		if row.Evidence != "" {
			_ = json.Unmarshal([]byte(row.Evidence), &f.Evidence)
		}
		out = append(out, f)
	}
	return out, nil
}

// ── BQ row value types ────────────────────────────────────────────────────────

type bqIncidentRow struct {
	ID              string              `bigquery:"id"`
	RunID           string              `bigquery:"run_id"`
	Status          string              `bigquery:"status"`
	Severity        string              `bigquery:"severity"`
	Entities        string              `bigquery:"entities"`
	DetectorCodes   string              `bigquery:"detector_codes"`
	SuggestedAction string              `bigquery:"suggested_action"`
	Note            bigquery.NullString `bigquery:"note"`
	CreatedAt       string              `bigquery:"created_at"`
	ResolvedAt      bigquery.NullString `bigquery:"resolved_at"`
	Hypotheses      bigquery.NullString `bigquery:"hypotheses"`
	RawJSON         bigquery.NullString `bigquery:"raw_json"`
}

func (r bqIncidentRow) toIncident() (models.Incident, error) {
	inc := models.Incident{
		ID:              r.ID,
		RunID:           r.RunID,
		Status:          models.IncidentStatus(r.Status),
		Severity:        models.Severity(r.Severity),
		SuggestedAction: models.SuggestedAction(r.SuggestedAction),
	}
	_ = json.Unmarshal([]byte(r.Entities), &inc.Entities)
	_ = json.Unmarshal([]byte(r.DetectorCodes), &inc.DetectorCodes)
	if r.Hypotheses.Valid {
		_ = json.Unmarshal([]byte(r.Hypotheses.StringVal), &inc.Hypotheses)
	}
	if r.Note.Valid {
		n := r.Note.StringVal
		inc.Note = &n
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, r.CreatedAt); err == nil {
			inc.CreatedAt = t
			break
		}
	}
	if r.ResolvedAt.Valid && r.ResolvedAt.StringVal != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(layout, r.ResolvedAt.StringVal); err == nil {
				inc.ResolvedAt = &t
				break
			}
		}
	}
	if inc.Entities == nil {
		inc.Entities = []string{}
	}
	if inc.DetectorCodes == nil {
		inc.DetectorCodes = []string{}
	}
	if inc.Hypotheses == nil {
		inc.Hypotheses = []models.Hypothesis{}
	}
	return inc, nil
}

type bqFindingRow struct {
	Code     string `bigquery:"code"`
	Severity string `bigquery:"severity"`
	EntityID string `bigquery:"entity_id"`
	Evidence string `bigquery:"evidence"`
	Hint     string `bigquery:"hint"`
}
