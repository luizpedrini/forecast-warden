// Package store provides pluggable persistence for Forecast Incidents.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/luizpedrini/forecast-warden/internal/models"
)

// ErrNotFound is returned by GetIncident / UpdateStatus when the id is missing.
var ErrNotFound = errors.New("incident not found")

// ErrNotImplemented is returned for drivers that are registered but not ready.
var ErrNotImplemented = errors.New("not implemented yet")

// Config selects and configures a Store driver.
type Config struct {
	// Driver is sqlite (default), file (legacy md+json), or postgres (stub).
	Driver string
	// DSN is the sqlite file path (default data/warden.db). Ignored by file.
	DSN string
	// WriteMarkdown also writes incidents/*.md for humans (sqlite default true).
	WriteMarkdown bool
	// IncidentsDir is the directory for markdown/json (file driver + optional md).
	IncidentsDir string
}

// ListFilter narrows ListIncidents. Nil Status means all.
type ListFilter struct {
	Status *models.IncidentStatus
}

// Store persists incidents and findings.
type Store interface {
	Close() error
	SaveIncident(ctx context.Context, incident models.Incident) error
	GetIncident(ctx context.Context, id string) (models.Incident, error)
	ListIncidents(ctx context.Context, filter ListFilter) ([]models.Incident, error)
	UpdateStatus(ctx context.Context, id, status, note string) error
}

// DefaultConfig returns the opinionated sqlite defaults.
func DefaultConfig() Config {
	return Config{
		Driver:        "sqlite",
		DSN:           "data/warden.db",
		WriteMarkdown: true,
		IncidentsDir:  "incidents",
	}
}

// Open constructs a Store for cfg.Driver.
func Open(cfg Config) (Store, error) {
	cfg = applyDefaults(cfg)
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "sqlite", "":
		return openSQLite(cfg)
	case "file":
		return openFile(cfg)
	case "postgres", "postgresql":
		return nil, fmt.Errorf("store driver %q: %w", cfg.Driver, ErrNotImplemented)
	default:
		return nil, fmt.Errorf("unknown store driver %q (want sqlite|file|postgres)", cfg.Driver)
	}
}

func applyDefaults(cfg Config) Config {
	d := DefaultConfig()
	if strings.TrimSpace(cfg.Driver) == "" {
		cfg.Driver = d.Driver
	}
	if strings.TrimSpace(cfg.DSN) == "" {
		cfg.DSN = d.DSN
	}
	if strings.TrimSpace(cfg.IncidentsDir) == "" {
		cfg.IncidentsDir = d.IncidentsDir
	}
	// WriteMarkdown: zero value false is ambiguous; callers should set via
	// config.LoadConfig which applies the true default when the block is omitted.
	return cfg
}
