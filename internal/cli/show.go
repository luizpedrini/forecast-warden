package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/incidents"
	"github.com/luizpedrini/forecast-warden/internal/store"
)

var showConfig string

var showCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show one incident (markdown body if present)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		incidentID := args[0]
		cfg, err := config.MustExistOrDefault(showConfig)
		if err != nil {
			fail(fmt.Sprintf("config: %v", err), 2)
		}
		db, err := openStore(cfg)
		if err != nil {
			fail(err.Error(), 2)
		}
		defer db.Close()

		inc, err := db.GetIncident(context.Background(), incidentID)
		if errors.Is(err, store.ErrNotFound) {
			fail(fmt.Sprintf("Incident not found: %s", incidentID), 2)
		}
		if err != nil {
			fail(fmt.Sprintf("load: %v", err), 2)
		}

		// Prefer human markdown sidecar when present.
		mds, _ := filepath.Glob(filepath.Join(cfg.IncidentsDir, "*.md"))
		for _, md := range mds {
			text, err := os.ReadFile(md)
			if err != nil {
				continue
			}
			s := string(text)
			if strings.Contains(s, "id: "+inc.ID) || strings.Contains(s, "id: "+incidentID) ||
				strings.Contains(filepath.Base(md), incidentID) {
				fmt.Fprint(Out, s)
				return
			}
		}
		fmt.Fprint(Out, incidents.RenderMarkdown(&inc))
	},
}

func init() {
	showCmd.Flags().StringVar(&showConfig, "config", "warden.yaml", "Config path")
}
