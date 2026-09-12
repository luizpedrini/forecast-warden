package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/luizpedrini/forecast-warden/internal/config"
	"github.com/luizpedrini/forecast-warden/internal/incidents"
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
		inc, err := incidents.LoadIncident(cfg.IncidentsDir, incidentID)
		if err != nil {
			fail(fmt.Sprintf("load: %v", err), 2)
		}
		if inc == nil {
			fail(fmt.Sprintf("Incident not found: %s", incidentID), 2)
		}
		mds, _ := filepath.Glob(filepath.Join(cfg.IncidentsDir, "*.md"))
		for _, md := range mds {
			text, err := os.ReadFile(md)
			if err != nil {
				continue
			}
			s := string(text)
			head := s
			if len(head) > 200 {
				head = head[:200]
			}
			if strings.Contains(s, "id: "+inc.ID) || strings.Contains(s, "id: "+incidentID) ||
				strings.Contains(filepath.Base(md), incidentID) {
				fmt.Fprint(Out, s)
				return
			}
		}
		enc := json.NewEncoder(Out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(inc)
	},
}

func init() {
	showCmd.Flags().StringVar(&showConfig, "config", "warden.yaml", "Config path")
}
