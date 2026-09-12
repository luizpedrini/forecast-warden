package store

import (
	"context"

	"github.com/luizpedrini/forecast-warden/internal/incidents"
	"github.com/luizpedrini/forecast-warden/internal/models"
)

// fileStore is the legacy markdown + JSON filesystem driver.
type fileStore struct {
	incidentsDir string
}

func openFile(cfg Config) (Store, error) {
	return &fileStore{incidentsDir: cfg.IncidentsDir}, nil
}

func (s *fileStore) Close() error { return nil }

func (s *fileStore) SaveIncident(_ context.Context, incident models.Incident) error {
	_, _, err := incidents.WriteIncident(s.incidentsDir, &incident)
	return err
}

func (s *fileStore) GetIncident(_ context.Context, id string) (models.Incident, error) {
	inc, err := incidents.LoadIncident(s.incidentsDir, id)
	if err != nil {
		return models.Incident{}, err
	}
	if inc == nil {
		return models.Incident{}, ErrNotFound
	}
	return *inc, nil
}

func (s *fileStore) ListIncidents(_ context.Context, filter ListFilter) ([]models.Incident, error) {
	return incidents.ListIncidents(s.incidentsDir, filter.Status)
}

func (s *fileStore) UpdateStatus(_ context.Context, id, status, note string) error {
	st := models.IncidentStatus(status)
	updated, err := incidents.ResolveIncident(s.incidentsDir, id, note, st)
	if err != nil {
		return err
	}
	if updated == nil {
		return ErrNotFound
	}
	return nil
}
