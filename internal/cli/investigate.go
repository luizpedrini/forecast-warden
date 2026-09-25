package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/investigate"
	"github.com/luizpedrini/forecast-warden/internal/llm"
	"github.com/luizpedrini/forecast-warden/internal/store"
)

var investigateConfig string

var investigateCmd = &cobra.Command{
	Use:   "investigate <id>",
	Short: "Print an investigation playbook for an incident",
	Long: `Generate an actionable investigation playbook from a persisted Forecast Incident.

Always builds the rule-based playbook first (templates + combo hints; no warehouse).
Optional --llm enriches with a "## LLM synthesis" section (paragraph + ranked
hypotheses + optional note) via OpenAI, Anthropic, or Gemini. API keys come from
the environment only (OPENAI_API_KEY / ANTHROPIC_API_KEY / GEMINI_API_KEY). On any
LLM failure, a warning is printed to stderr and the rule-based playbook is still
emitted (exit 0 if the investigate itself succeeded).

Use --write to also save incidents/<id>.investigate.md.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		incidentID := args[0]
		write, _ := cmd.Flags().GetBool("write")
		useLLM, _ := cmd.Flags().GetBool("llm")
		provider, _ := cmd.Flags().GetString("provider")

		cfg, err := config.MustExistOrDefault(investigateConfig)
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

		md := investigate.Render(&inc)

		if useLLM {
			client, err := llm.NewClientFromEnv(provider)
			if err != nil {
				fmt.Fprintf(ErrOut, "warning: --llm skipped: %v\n", err)
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
				enriched, err := llm.EnrichPlaybook(ctx, client, &inc, md)
				cancel()
				if err != nil {
					fmt.Fprintf(ErrOut, "warning: --llm failed, using rule-based only: %v\n", err)
				} else {
					md = enriched
				}
			}
		}

		fmt.Fprint(Out, md)
		if write {
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
	investigateCmd.Flags().Bool("write", false, "Also save incidents/<id>.investigate.md")
	investigateCmd.Flags().Bool("llm", false, "Enrich playbook with LLM synthesis (env API keys)")
	investigateCmd.Flags().String("provider", "", "LLM provider: openai|anthropic|gemini (default: WARDEN_LLM_PROVIDER or openai)")
	investigateCmd.Flags().StringVar(&investigateConfig, "config", "warden.yaml", "Config path")
}
