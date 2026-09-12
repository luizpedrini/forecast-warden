package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/csvio"
	"github.com/luizpedrini/forecast-warden/internal/synthetic"
)

var (
	initForce bool
	initDays  int
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create config + ~14d synthetic zone metrics (some sick) + baseline",
	Run: func(cmd *cobra.Command, args []string) {
		cfgPath := "warden.yaml"
		if _, err := os.Stat(cfgPath); err == nil && !initForce {
			fmt.Fprintln(Out, "warden.yaml already exists (use --force to overwrite).")
		} else {
			if err := config.WriteDefaultConfig(cfgPath); err != nil {
				fail(fmt.Sprintf("write config: %v", err), 2)
			}
			fmt.Fprintf(Out, "Wrote %s\n", cfgPath)
		}

		cfg, err := config.LoadConfig(cfgPath)
		if err != nil {
			fail(fmt.Sprintf("load config: %v", err), 2)
		}
		metricsPath := cfg.MetricsPath()
		baselinePath := cfg.BaselinePath()

		if _, err := os.Stat(metricsPath); err == nil && !initForce {
			fmt.Fprintf(Out, "%s exists (use --force).\n", metricsPath)
		} else {
			rows := synthetic.GenerateMetrics(initDays, synthetic.DefaultEndDate(), 42)
			if err := csvio.WriteMetrics(metricsPath, rows); err != nil {
				fail(fmt.Sprintf("write metrics: %v", err), 2)
			}
			fmt.Fprintf(Out, "Wrote %s (%d rows, %d days)\n", metricsPath, len(rows), initDays)
		}

		metrics, err := csvio.ReadMetrics(metricsPath)
		if err != nil {
			fail(fmt.Sprintf("read metrics: %v", err), 2)
		}
		baselineRows := synthetic.ComputeBaseline(metrics, 28)
		if err := csvio.WriteBaseline(baselinePath, baselineRows); err != nil {
			fail(fmt.Sprintf("write baseline: %v", err), 2)
		}
		fmt.Fprintf(Out, "Wrote %s (%d entities)\n", baselinePath, len(baselineRows))

		if err := os.MkdirAll(cfg.IncidentsDir, 0o755); err != nil {
			fail(fmt.Sprintf("mkdir incidents: %v", err), 2)
		}
		// keep .gitkeep if present
		_ = filepath.Join(cfg.IncidentsDir, ".gitkeep")
		fmt.Fprintf(Out, "Ready incidents dir: %s/\n", cfg.IncidentsDir)
		fmt.Fprintln(Out, "Next: warden check")
	},
}

func init() {
	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite existing data/config")
	initCmd.Flags().IntVar(&initDays, "days", 14, "Days of synthetic metrics")
}
