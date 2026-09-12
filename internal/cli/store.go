package cli

import (
	"fmt"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/store"
)

func openStore(cfg config.WardenConfig) (store.Store, error) {
	st, err := store.Open(store.Config{
		Driver:        cfg.Store.Driver,
		DSN:           cfg.Store.DSN,
		WriteMarkdown: cfg.StoreWriteMarkdown(),
		IncidentsDir:  cfg.IncidentsDir,
	})
	if err != nil {
		return nil, fmt.Errorf("store: %w", err)
	}
	return st, nil
}
