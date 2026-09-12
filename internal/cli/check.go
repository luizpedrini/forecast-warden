package cli

import (
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
		findings := detectors.RunAll(runRows, baseline, cfg.Thresholds)

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

		incident := incidents.BuildIncident(target, findings, time.Time{})
		if incident == nil {
			fmt.Fprintln(Out, "No incident opened (info-only or clean).")
			exitWithSeverity("")
		}

		existing, err := incidents.LoadIncident(cfg.IncidentsDir, incident.ID)
		if err != nil {
			fail(fmt.Sprintf("load incident: %v", err), 2)
		}
		if existing != nil && models.IsTerminalStatus(existing.Status) {
			fmt.Fprintf(Out, "incident %s already %s; not overwriting\n", existing.ID, existing.Status)
			exitWithSeverity(string(incident.Severity))
		}

		mdPath, jsonPath, err := incidents.WriteIncident(cfg.IncidentsDir, incident)
		if err != nil {
			fail(fmt.Sprintf("write incident: %v", err), 2)
		}
		fmt.Fprintf(Out, "Incident %s (%s) → %s + %s\n",
			incident.ID, incident.Severity, filepath.Base(mdPath), filepath.Base(jsonPath))
		exitWithSeverity(string(incident.Severity))
	},
}

func init() {
	checkCmd.Flags().StringVar(&checkRunID, "run-id", "", "Check a specific run_id (default: latest in CSV)")
	checkCmd.Flags().StringVar(&checkMetrics, "metrics", "", "Path to metrics CSV")
	checkCmd.Flags().StringVar(&checkConfig, "config", "warden.yaml", "Config path")
}
