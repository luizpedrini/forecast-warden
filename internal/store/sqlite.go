package store

import (
	"context"
	"database/sql"
	"errors"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/luizpedrini/forecast-warden/internal/incidents"
	"github.com/luizpedrini/forecast-warden/internal/models"
)

const sqliteSchema = `
CREATE TABLE IF NOT EXISTS incidents (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  status TEXT NOT NULL,
  severity TEXT NOT NULL,
  entities TEXT NOT NULL,
  detector_codes TEXT NOT NULL,
  suggested_action TEXT NOT NULL,
  note TEXT,
  created_at TEXT NOT NULL,
  resolved_at TEXT,
  hypotheses TEXT NOT NULL,
  raw_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_incidents_run_id ON incidents(run_id);
CREATE INDEX IF NOT EXISTS idx_incidents_status ON incidents(status);

CREATE TABLE IF NOT EXISTS findings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  incident_id TEXT NOT NULL,
  code TEXT NOT NULL,
  severity TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  evidence TEXT NOT NULL,
  hint TEXT NOT NULL,
  FOREIGN KEY(incident_id) REFERENCES incidents(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_findings_incident_id ON findings(incident_id);
`

type sqliteStore struct {
	db            *sql.DB
	writeMarkdown bool
	incidentsDir  string
}

func openSQLite(cfg Config) (Store, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DSN), 0o755); err != nil && filepath.Dir(cfg.DSN) != "." {
		return nil, fmt.Errorf("mkdir store dsn dir: %w", err)
	}
	db, err := sql.Open("sqlite", cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single-writer friendly; enable FK for findings cascade.
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(sqliteSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	return &sqliteStore{
		db:            db,
		writeMarkdown: cfg.WriteMarkdown,
		incidentsDir:  cfg.IncidentsDir,
	}, nil
}

func (s *sqliteStore) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *sqliteStore) SaveIncident(ctx context.Context, incident models.Incident) error {
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
	var note any
	if incident.Note != nil {
		note = *incident.Note
	}
	var resolvedAt any
	if incident.ResolvedAt != nil {
		resolvedAt = incident.ResolvedAt.UTC().Format(time.RFC3339Nano)
	}
	createdAt := incident.CreatedAt.UTC().Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
INSERT INTO incidents (
  id, run_id, status, severity, entities, detector_codes,
  suggested_action, note, created_at, resolved_at, hypotheses, raw_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  run_id=excluded.run_id,
  status=excluded.status,
  severity=excluded.severity,
  entities=excluded.entities,
  detector_codes=excluded.detector_codes,
  suggested_action=excluded.suggested_action,
  note=excluded.note,
  created_at=excluded.created_at,
  resolved_at=excluded.resolved_at,
  hypotheses=excluded.hypotheses,
  raw_json=excluded.raw_json
`, incident.ID, incident.RunID, string(incident.Status), string(incident.Severity),
		string(entitiesJSON), string(codesJSON), string(incident.SuggestedAction),
		note, createdAt, resolvedAt, string(hypsJSON), string(rawJSON))
	if err != nil {
		return fmt.Errorf("upsert incident: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM findings WHERE incident_id = ?`, incident.ID); err != nil {
		return err
	}
	for _, f := range incident.Findings {
		evJSON, err := json.Marshal(f.Evidence)
		if err != nil {
			return err
		}
		if evJSON == nil {
			evJSON = []byte("{}")
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO findings (incident_id, code, severity, entity_id, evidence, hint)
VALUES (?, ?, ?, ?, ?, ?)`,
			incident.ID, f.Code, string(f.Severity), f.EntityID, string(evJSON), f.Hint)
		if err != nil {
			return fmt.Errorf("insert finding: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if s.writeMarkdown {
		if err := os.MkdirAll(s.incidentsDir, 0o755); err != nil {
			return err
		}
		mdPath, _ := incidents.IncidentPaths(s.incidentsDir, &incident)
		if err := os.WriteFile(mdPath, []byte(incidents.RenderMarkdown(&incident)), 0o644); err != nil {
			return fmt.Errorf("write markdown: %w", err)
		}
	}
	return nil
}

func (s *sqliteStore) GetIncident(ctx context.Context, id string) (models.Incident, error) {
	inc, err := s.getByExactID(ctx, id)
	if err == nil {
		return inc, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return models.Incident{}, err
	}
	// Allow short-hash / stem lookup like the file driver.
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

func (s *sqliteStore) GetIncidentByRunID(ctx context.Context, runID string) (models.Incident, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, run_id, status, severity, entities, detector_codes,
       suggested_action, note, created_at, resolved_at, hypotheses, raw_json
FROM incidents WHERE run_id = ?`, runID)
	inc, err := scanIncident(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.Incident{}, ErrNotFound
		}
		return models.Incident{}, err
	}
	findings, err := s.loadFindings(ctx, inc.ID)
	if err != nil {
		return models.Incident{}, err
	}
	inc.Findings = findings
	return inc, nil
}

func (s *sqliteStore) getByExactID(ctx context.Context, id string) (models.Incident, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, run_id, status, severity, entities, detector_codes,
       suggested_action, note, created_at, resolved_at, hypotheses, raw_json
FROM incidents WHERE id = ?`, id)
	inc, err := scanIncident(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return models.Incident{}, ErrNotFound
		}
		return models.Incident{}, err
	}
	findings, err := s.loadFindings(ctx, inc.ID)
	if err != nil {
		return models.Incident{}, err
	}
	inc.Findings = findings
	return inc, nil
}

func (s *sqliteStore) ListIncidents(ctx context.Context, filter ListFilter) ([]models.Incident, error) {
	q := `
SELECT id, run_id, status, severity, entities, detector_codes,
       suggested_action, note, created_at, resolved_at, hypotheses, raw_json
FROM incidents`
	var args []any
	if filter.Status != nil {
		q += ` WHERE status = ?`
		args = append(args, string(*filter.Status))
	}
	q += ` ORDER BY created_at ASC, id ASC`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Incident
	for rows.Next() {
		inc, err := scanIncident(rows)
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
	return out, rows.Err()
}

func (s *sqliteStore) UpdateStatus(ctx context.Context, id, status, note string) error {
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

func (s *sqliteStore) loadFindings(ctx context.Context, incidentID string) ([]models.Finding, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT code, severity, entity_id, evidence, hint
FROM findings WHERE incident_id = ? ORDER BY id ASC`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Finding
	for rows.Next() {
		var f models.Finding
		var sev, evJSON string
		if err := rows.Scan(&f.Code, &sev, &f.EntityID, &evJSON, &f.Hint); err != nil {
			return nil, err
		}
		f.Severity = models.Severity(sev)
		f.Evidence = map[string]any{}
		if evJSON != "" {
			_ = json.Unmarshal([]byte(evJSON), &f.Evidence)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanIncident(row scannable) (models.Incident, error) {
	var (
		inc                          models.Incident
		status, severity, action     string
		entitiesJSON, codesJSON      string
		hypsJSON, rawJSON            sql.NullString
		note, createdAt, resolvedAt  sql.NullString
	)
	err := row.Scan(
		&inc.ID, &inc.RunID, &status, &severity, &entitiesJSON, &codesJSON,
		&action, &note, &createdAt, &resolvedAt, &hypsJSON, &rawJSON,
	)
	if err != nil {
		return models.Incident{}, err
	}
	inc.Status = models.IncidentStatus(status)
	inc.Severity = models.Severity(severity)
	inc.SuggestedAction = models.SuggestedAction(action)
	_ = json.Unmarshal([]byte(entitiesJSON), &inc.Entities)
	_ = json.Unmarshal([]byte(codesJSON), &inc.DetectorCodes)
	if hypsJSON.Valid {
		_ = json.Unmarshal([]byte(hypsJSON.String), &inc.Hypotheses)
	}
	if note.Valid {
		n := note.String
		inc.Note = &n
	}
	if createdAt.Valid {
		if t, err := time.Parse(time.RFC3339Nano, createdAt.String); err == nil {
			inc.CreatedAt = t
		} else if t, err := time.Parse(time.RFC3339, createdAt.String); err == nil {
			inc.CreatedAt = t
		}
	}
	if resolvedAt.Valid && resolvedAt.String != "" {
		if t, err := time.Parse(time.RFC3339Nano, resolvedAt.String); err == nil {
			inc.ResolvedAt = &t
		} else if t, err := time.Parse(time.RFC3339, resolvedAt.String); err == nil {
			inc.ResolvedAt = &t
		}
	}
	if rawJSON.Valid && rawJSON.String != "" {
		// Prefer structured columns; raw_json is optional backup.
		_ = rawJSON
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

func matchIncidentID(inc models.Incident, query string) bool {
	if inc.ID == query {
		return true
	}
	parts := strings.Split(inc.ID, "-")
	short := parts[len(parts)-1]
	stem := fmt.Sprintf("%s__%s", inc.RunID, short)
	return query == short || query == stem
}

