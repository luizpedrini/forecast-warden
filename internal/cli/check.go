package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/csvio"
	"github.com/luizpedrini/forecast-warden/internal/detectors"
	"github.com/luizpedrini/forecast-warden/internal/incidents"
	"github.com/luizpedrini/forecast-warden/internal/models"
	"github.com/luizpedrini/forecast-warden/internal/store"
)

var (
	checkRunID   string
	checkMetrics string
	checkConfig  string
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Run detectors on metrics; write incident(s) if warning/critical",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.MustExistOrDefault(checkConfig)
		if err != nil {
			fail(fmt.Sprintf("config: %v", err), 2)
		}
		metricsPath := checkMetrics
		if metricsPath == "" {
			metricsPath = cfg.MetricsPath()
		}
		if _, err := os.Stat(metricsPath); err != nil {
			fail(fmt.Sprintf("Metrics not found: %s. Run warden init.", metricsPath), 2)
		}
		allRows, err := csvio.ReadMetrics(metricsPath)
		if err != nil {
			fail(fmt.Sprintf("read metrics: %v", err), 2)
		}
		if len(allRows) == 0 {
			fail("Metrics CSV is empty.", 2)
		}
		availSet := map[string]struct{}{}
		for _, r := range allRows {
			availSet[r.RunID] = struct{}{}
		}
		available := make([]string, 0, len(availSet))
		for id := range availSet {
			available = append(available, id)
		}
		sort.Strings(available)
		target := checkRunID
		if target == "" {
			target = available[len(available)-1]
		}
		var runRows []models.MetricRow
		for _, r := range allRows {
			if r.RunID == target {
				runRows = append(runRows, r)
			}
		}
		if len(runRows) == 0 {
			fail(fmt.Sprintf("No rows for run_id=%s. Available: %v", target, available), 2)
		}

		baseline, err := csvio.ReadBaseline(cfg.BaselinePath())
		if err != nil {
			fail(fmt.Sprintf("baseline: %v", err), 2)
		}
		findings := detectors.RunAll(runRows, baseline, cfg.Detectors)

		if len(findings) > 0 {
			w := tabwriter.NewWriter(Out, 0, 0, 2, ' ', 0)
			fmt.Fprintf(w, "Findings for run %s\n", target)
			fmt.Fprintln(w, "CODE\tSEVERITY\tENTITY\tHINT")
			for _, f := range findings {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.Code, f.Severity, f.EntityID, f.Hint)
			}
			_ = w.Flush()
		} else {
			fmt.Fprintln(Out, "No findings.")
		}

		newIncident := incidents.BuildIncident(target, findings, time.Time{})
		if newIncident == nil {
			fmt.Fprintln(Out, "No incident opened (info-only or clean).")
			exitWithSeverity("")
		}

		st, err := openStore(cfg)
		if err != nil {
			fail(err.Error(), 2)
		}
		defer st.Close()

		ctx := context.Background()
		existing, err := st.GetIncident(ctx, newIncident.ID)
		notFound := errors.Is(err, store.ErrNotFound)
		if err != nil && !notFound {
			fail(fmt.Sprintf("load incident: %v", err), 2)
		}

		var toSave *models.Incident
		var action string

		if notFound {
			// Brand new incident
			newIncident.History = []models.HistoryEvent{{
				Timestamp: newIncident.CreatedAt,
				Event:     "created",
				Status:    newIncident.Status,
				Severity:  newIncident.Severity,
			}}
			toSave = newIncident
			action = "created"
		} else {
			// Existing incident found
			switch existing.Status {
			case models.StatusOpen:
				// Update in-place: preserve createdAt & history, update findings/severity/etc
				toSave = incidents.UpdateIncidentInPlace(&existing, newIncident)
				action = "updated"
			case models.StatusResolved:
				// Reopen only if new severity >= critical
				if newIncident.Severity == models.SeverityCritical {
					toSave = incidents.ReopenIncident(&existing, newIncident)
					action = "reopened"
				} else {
					fmt.Fprintf(Out, "incident %s resolved; new severity %s (not critical) — not reopening\n",
						existing.ID, newIncident.Severity)
					exitWithSeverity(string(newIncident.Severity))
				}
			case models.StatusAcceptedRisk, models.StatusWontfix:
				// Never reopen terminal acceptance statuses
				fmt.Fprintf(ErrOut, "incident %s is %s; gate still passes but findings exist (severity %s)\n",
					existing.ID, existing.Status, newIncident.Severity)
				fmt.Fprintf(Out, "incident %s already %s; not modifying\n", existing.ID, existing.Status)
				exitWithSeverity(string(newIncident.Severity))
			default:
				// Unknown status, treat as open
				toSave = incidents.UpdateIncidentInPlace(&existing, newIncident)
				action = "updated"
			}
		}

		if err := st.SaveIncident(ctx, *toSave); err != nil {
			fail(fmt.Sprintf("save incident: %v", err), 2)
		}

		where := cfg.Store.DSN
		if cfg.Store.Driver == "file" {
			where = cfg.IncidentsDir
		}
		extra := ""
		if cfg.Store.Driver == "sqlite" && cfg.StoreWriteMarkdown() {
			mdPath, _ := incidents.IncidentPaths(cfg.IncidentsDir, toSave)
			extra = fmt.Sprintf(" + %s", filepath.Base(mdPath))
		} else if cfg.Store.Driver == "file" {
			mdPath, jsonPath := incidents.IncidentPaths(cfg.IncidentsDir, toSave)
			extra = fmt.Sprintf(" → %s + %s", filepath.Base(mdPath), filepath.Base(jsonPath))
			fmt.Fprintf(Out, "Incident %s %s (%s)%s\n", toSave.ID, action, toSave.Severity, extra)
			exitWithSeverity(string(toSave.Severity))
		}
		fmt.Fprintf(Out, "Incident %s %s (%s) → %s%s\n",
			toSave.ID, action, toSave.Severity, where, extra)
		exitWithSeverity(string(toSave.Severity))
	},
}

func init() {
	checkCmd.Flags().StringVar(&checkRunID, "run-id", "", "Check a specific run_id (default: latest in CSV)")
	checkCmd.Flags().StringVar(&checkMetrics, "metrics", "", "Path to metrics CSV")
	checkCmd.Flags().StringVar(&checkConfig, "config", "warden.yaml", "Config path")
}
