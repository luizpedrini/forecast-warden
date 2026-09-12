package cli

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/models"
	"github.com/luizpedrini/forecast-warden/internal/store"
)

var (
	listStatus string
	listConfig string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List incidents (optionally by status)",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.MustExistOrDefault(listConfig)
		if err != nil {
			fail(fmt.Sprintf("config: %v", err), 2)
		}
		var statusPtr *models.IncidentStatus
		if listStatus != "" {
			st := models.IncidentStatus(listStatus)
			switch st {
			case models.StatusOpen, models.StatusResolved, models.StatusAcceptedRisk, models.StatusWontfix:
				statusPtr = &st
			default:
				fail(fmt.Sprintf("Invalid status: %s", listStatus), 2)
			}
		}
		db, err := openStore(cfg)
		if err != nil {
			fail(err.Error(), 2)
		}
		defer db.Close()

		incs, err := db.ListIncidents(context.Background(), store.ListFilter{Status: statusPtr})
		if err != nil {
			fail(fmt.Sprintf("list: %v", err), 2)
		}
		if len(incs) == 0 {
			fmt.Fprintln(Out, "No incidents.")
			return
		}
		w := tabwriter.NewWriter(Out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tRUN_ID\tSTATUS\tSEVERITY\tENTITIES\tDETECTORS")
		for _, inc := range incs {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				inc.ID, inc.RunID, inc.Status, inc.Severity,
				strings.Join(inc.Entities, ","),
				strings.Join(inc.DetectorCodes, ","),
			)
		}
		_ = w.Flush()
	},
}

func init() {
	listCmd.Flags().StringVar(&listStatus, "status", "", "Filter: open|resolved|accepted-risk|wontfix")
	listCmd.Flags().StringVar(&listConfig, "config", "warden.yaml", "Config path")
}
