package store_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/luizpedrini/forecast-warden/internal/models"
	"github.com/luizpedrini/forecast-warden/internal/store"
)

func sampleIncident(id string) models.Incident {
	note := ""
	return models.Incident{
		ID:            id,
		RunID:         "2026-09-12",
		Status:        models.StatusOpen,
		Severity:      models.SeverityCritical,
		Entities:      []string{"Z3", "Z7"},
		DetectorCodes: []string{"BiasShift", "HighWAPE"},
		CreatedAt:     time.Date(2026, 9, 12, 6, 0, 0, 0, time.UTC),
		Findings: []models.Finding{
			{
				Code:     "HighWAPE",
				Severity: models.SeverityCritical,
				EntityID: "Z3",
				Evidence: map[string]any{"wape": 0.55, "threshold": 0.45},
				Hint:     "WAPE critical",
			},
			{
				Code:     "BiasShift",
				Severity: models.SeverityWarning,
				EntityID: "Z7",
				Evidence: map[string]any{"bias": -0.22},
				Hint:     "bias shift",
			},
		},
		Hypotheses:      []models.Hypothesis{models.HypDemandSpike, models.HypModelStale},
		SuggestedAction: models.ActionHoldPromotion,
		Note:            &note,
		ResolvedAt:      nil,
	}
}

func TestSQLiteSaveGetListUpdate(t *testing.T) {
	dir := t.TempDir()
	dsn := filepath.Join(dir, "warden.db")
	incDir := filepath.Join(dir, "incidents")
	st, err := store.Open(store.Config{
		Driver:        "sqlite",
		DSN:           dsn,
		WriteMarkdown: true,
		IncidentsDir:  incDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	inc := sampleIncident("fw-20260912-abcd")
	if err := st.SaveIncident(ctx, inc); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dsn); err != nil {
		t.Fatalf("expected sqlite file: %v", err)
	}
	mds, _ := filepath.Glob(filepath.Join(incDir, "*.md"))
	if len(mds) != 1 {
		t.Fatalf("expected 1 markdown with write_markdown, got %d", len(mds))
	}

	got, err := st.GetIncident(ctx, inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != inc.ID || got.Status != models.StatusOpen || got.Severity != models.SeverityCritical {
		t.Fatalf("get mismatch: %+v", got)
	}
	if len(got.Findings) != 2 {
		t.Fatalf("findings=%d want 2", len(got.Findings))
	}
	if got.Findings[0].Evidence["wape"] == nil {
		t.Fatalf("evidence roundtrip failed: %+v", got.Findings[0].Evidence)
	}
	if len(got.Entities) != 2 || got.Entities[0] != "Z3" {
		t.Fatalf("entities=%v", got.Entities)
	}

	// short id lookup
	short, err := st.GetIncident(ctx, "abcd")
	if err != nil || short.ID != inc.ID {
		t.Fatalf("short lookup: %v %+v", err, short)
	}

	listed, err := st.ListIncidents(ctx, store.ListFilter{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("list: %v len=%d", err, len(listed))
	}
	open := models.StatusOpen
	listedOpen, err := st.ListIncidents(ctx, store.ListFilter{Status: &open})
	if err != nil || len(listedOpen) != 1 {
		t.Fatalf("list open: %v", err)
	}

	if err := st.UpdateStatus(ctx, inc.ID, string(models.StatusResolved), "reviewed"); err != nil {
		t.Fatal(err)
	}
	got, err = st.GetIncident(ctx, inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != models.StatusResolved {
		t.Fatalf("status=%s", got.Status)
	}
	if got.Note == nil || *got.Note != "reviewed" {
		t.Fatalf("note=%v", got.Note)
	}
	if got.ResolvedAt == nil {
		t.Fatal("expected resolved_at")
	}

	resolved := models.StatusResolved
	listedRes, err := st.ListIncidents(ctx, store.ListFilter{Status: &resolved})
	if err != nil || len(listedRes) != 1 {
		t.Fatalf("list resolved: %v", err)
	}
	listedOpen, err = st.ListIncidents(ctx, store.ListFilter{Status: &open})
	if err != nil || len(listedOpen) != 0 {
		t.Fatalf("list open after resolve: %v len=%d", err, len(listedOpen))
	}
}

func TestSQLiteUpsertAndClobberRespect(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(store.Config{
		Driver: "sqlite",
		DSN:    filepath.Join(dir, "w.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	inc := sampleIncident("fw-20260912-clob")
	if err := st.SaveIncident(ctx, inc); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateStatus(ctx, inc.ID, string(models.StatusResolved), "done"); err != nil {
		t.Fatal(err)
	}
	existing, err := st.GetIncident(ctx, inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !models.IsTerminalStatus(existing.Status) {
		t.Fatal("expected terminal")
	}

	// Simulate check: load first, skip save when terminal.
	fresh := sampleIncident("fw-20260912-clob")
	fresh.Severity = models.SeverityWarning
	loaded, err := st.GetIncident(ctx, fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if models.IsTerminalStatus(loaded.Status) {
		// do not save — status must remain resolved
	} else {
		_ = st.SaveIncident(ctx, fresh)
	}
	after, err := st.GetIncident(ctx, fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != models.StatusResolved {
		t.Fatalf("clobber: status=%s", after.Status)
	}

	// Upsert when open should replace findings.
	openInc := sampleIncident("fw-20260912-updt")
	if err := st.SaveIncident(ctx, openInc); err != nil {
		t.Fatal(err)
	}
	openInc.Findings = openInc.Findings[:1]
	openInc.Severity = models.SeverityWarning
	if err := st.SaveIncident(ctx, openInc); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetIncident(ctx, openInc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Findings) != 1 {
		t.Fatalf("upsert findings=%d", len(got.Findings))
	}
	if got.Severity != models.SeverityWarning {
		t.Fatalf("severity=%s", got.Severity)
	}
}

func TestSQLiteNotFound(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(store.Config{Driver: "sqlite", DSN: filepath.Join(dir, "x.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	_, err = st.GetIncident(context.Background(), "missing")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	err = st.UpdateStatus(context.Background(), "missing", "resolved", "n")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestFileDriverRoundtrip(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(store.Config{
		Driver:       "file",
		IncidentsDir: dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	inc := sampleIncident("fw-20260912-file")
	if err := st.SaveIncident(ctx, inc); err != nil {
		t.Fatal(err)
	}
	jsons, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	mds, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	if len(jsons) != 1 || len(mds) != 1 {
		t.Fatalf("file driver artifacts json=%d md=%d", len(jsons), len(mds))
	}
	got, err := st.GetIncident(ctx, inc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != inc.ID {
		t.Fatalf("id=%s", got.ID)
	}
}

func TestPostgresNotImplemented(t *testing.T) {
	_, err := store.Open(store.Config{Driver: "postgres", DSN: "postgres://localhost/w"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, store.ErrNotImplemented) {
		t.Fatalf("want ErrNotImplemented, got %v", err)
	}
}

func TestUnknownDriver(t *testing.T) {
	_, err := store.Open(store.Config{Driver: "redis"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteMarkdownFalseSkipsMD(t *testing.T) {
	dir := t.TempDir()
	incDir := filepath.Join(dir, "incidents")
	st, err := store.Open(store.Config{
		Driver:        "sqlite",
		DSN:           filepath.Join(dir, "w.db"),
		WriteMarkdown: false,
		IncidentsDir:  incDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.SaveIncident(context.Background(), sampleIncident("fw-20260912-nomd")); err != nil {
		t.Fatal(err)
	}
	mds, _ := filepath.Glob(filepath.Join(incDir, "*.md"))
	if len(mds) != 0 {
		t.Fatalf("expected no md, got %v", mds)
	}
}
