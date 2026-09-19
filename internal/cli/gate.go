package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/models"
	"github.com/luizpedrini/forecast-warden/internal/store"
)

var gateConfig string

var gateCmd = &cobra.Command{
	Use:     "gate",
	Aliases: []string{"can-promote"},
	Short:   "Check if promotion is allowed (exit 0=ok, exit 2=blocked by critical open incident)",
	Long: `Check if model promotion is allowed based on current incident state.

Exit codes:
  0  Promote OK — no critical-severity incident currently open
  2  Block — at least one critical-severity incident is open

Note: This command never exits with code 1. Warnings do not block promotion.
Incidents with status accepted-risk or wontfix do not block (they are terminal).`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.MustExistOrDefault(gateConfig)
		if err != nil {
			fail(fmt.Sprintf("config: %v", err), 2)
		}

		st, err := openStore(cfg)
		if err != nil {
			fail(err.Error(), 2)
		}
		defer st.Close()

		ctx := context.Background()
		open := models.StatusOpen
		incidents, err := st.ListIncidents(ctx, store.ListFilter{Status: &open})
		if err != nil {
			fail(fmt.Sprintf("list incidents: %v", err), 2)
		}

		var criticalOpen []models.Incident
		for _, inc := range incidents {
			if inc.Severity == models.SeverityCritical {
				criticalOpen = append(criticalOpen, inc)
			}
		}

		if len(criticalOpen) == 0 {
			fmt.Fprintln(Out, "gate: PASS — no critical open incidents")
			ExitFunc(0)
			return
		}

		fmt.Fprintf(Out, "gate: BLOCK — %d critical open incident(s):\n", len(criticalOpen))
		for _, inc := range criticalOpen {
			fmt.Fprintf(Out, "  - %s (run %s): %v\n", inc.ID, inc.RunID, inc.DetectorCodes)
		}
		ExitFunc(2)
	},
}

func init() {
	gateCmd.Flags().StringVar(&gateConfig, "config", "warden.yaml", "Config path")
}
