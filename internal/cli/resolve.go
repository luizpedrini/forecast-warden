package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/models"
	"github.com/luizpedrini/forecast-warden/internal/store"
)

var (
	resolveNote   string
	resolveStatus string
	resolveConfig string
)

var resolveCmd = &cobra.Command{
	Use:   "resolve <id>",
	Short: "Mark an incident resolved / accepted-risk / wontfix",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		incidentID := args[0]
		cfg, err := config.MustExistOrDefault(resolveConfig)
		if err != nil {
			fail(fmt.Sprintf("config: %v", err), 2)
		}
		st := models.IncidentStatus(resolveStatus)
		switch st {
		case models.StatusResolved, models.StatusAcceptedRisk, models.StatusWontfix:
		case models.StatusOpen:
			fail("Cannot resolve to open.", 2)
		default:
			fail(fmt.Sprintf("Invalid status: %s", resolveStatus), 2)
		}
		db, err := openStore(cfg)
		if err != nil {
			fail(err.Error(), 2)
		}
		defer db.Close()

		ctx := context.Background()
		if err := db.UpdateStatus(ctx, incidentID, string(st), resolveNote); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				fail(fmt.Sprintf("Incident not found: %s", incidentID), 2)
			}
			fail(fmt.Sprintf("resolve: %v", err), 2)
		}
		updated, err := db.GetIncident(ctx, incidentID)
		if err != nil {
			fail(fmt.Sprintf("resolve reload: %v", err), 2)
		}
		fmt.Fprintf(Out, "Updated %s → %s\n", updated.ID, updated.Status)
	},
}

func init() {
	resolveCmd.Flags().StringVar(&resolveNote, "note", "", "Resolution note (required)")
	_ = resolveCmd.MarkFlagRequired("note")
	resolveCmd.Flags().StringVar(&resolveStatus, "status", "resolved", "resolved | accepted-risk | wontfix")
	resolveCmd.Flags().StringVar(&resolveConfig, "config", "warden.yaml", "Config path")
}
