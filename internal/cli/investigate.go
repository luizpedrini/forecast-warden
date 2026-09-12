package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/incidents"
	"github.com/luizpedrini/forecast-warden/internal/investigate"
)

var (
	investigateWrite  bool
	investigateLLM    bool
	investigateConfig string
)

var investigateCmd = &cobra.Command{
	Use:   "investigate <id>",
	Short: "Print a rule-based investigation playbook for an incident",
	Long: `Generate an actionable investigation playbook from a persisted Forecast Incident.

Rule-based templates only (no LLM, no network, no warehouse). Use --write to also
save incidents/<id>.investigate.md. Flag --llm is reserved for a future hook and is
currently a no-op.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		incidentID := args[0]
		cfg, err := config.MustExistOrDefault(investigateConfig)
		if err != nil {
			fail(fmt.Sprintf("config: %v", err), 2)
		}
		inc, err := incidents.LoadIncident(cfg.IncidentsDir, incidentID)
		if err != nil {
			fail(fmt.Sprintf("load: %v", err), 2)
		}
		if inc == nil {
			fail(fmt.Sprintf("Incident not found: %s", incidentID), 2)
		}
		if investigateLLM {
			fmt.Fprintln(ErrOut, "note: --llm is reserved; using rule-based templates (no-op)")
		}
		md := investigate.Render(inc)
		fmt.Fprint(Out, md)
		if investigateWrite {
			if err := os.MkdirAll(cfg.IncidentsDir, 0o755); err != nil {
				fail(fmt.Sprintf("mkdir: %v", err), 2)
			}
			path := investigate.PlaybookPath(cfg.IncidentsDir, inc.ID)
			if err := os.WriteFile(path, []byte(md), 0o644); err != nil {
				fail(fmt.Sprintf("write: %v", err), 2)
			}
			fmt.Fprintf(ErrOut, "Wrote %s\n", path)
		}
	},
}

func init() {
	investigateCmd.Flags().BoolVar(&investigateWrite, "write", false, "Also save incidents/<id>.investigate.md")
	investigateCmd.Flags().BoolVar(&investigateLLM, "llm", false, "Reserved for future LLM narration (currently no-op)")
	investigateCmd.Flags().StringVar(&investigateConfig, "config", "warden.yaml", "Config path")
}
