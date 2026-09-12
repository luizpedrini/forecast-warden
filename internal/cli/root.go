package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

const Version = "0.6.0"

// ExitFunc is overridable in tests (defaults to os.Exit).
var ExitFunc = os.Exit

// Out is the primary writer (overridable in tests).
var Out io.Writer = os.Stdout

// ErrOut is the error writer (overridable in tests).
var ErrOut io.Writer = os.Stderr

var rootCmd = &cobra.Command{
	Use:   "warden",
	Short: "Rule-based forecast pipeline ops: metrics → detectors → Forecast Incident",
	Long:  "forecast-warden — synthetic metrics → detectors → Forecast Incident (markdown + JSON) → human resolve.",
}

func init() {
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("forecast-warden {{.Version}}\n")
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(resolveCmd)
	rootCmd.AddCommand(investigateCmd)
}

func Execute() error {
	return rootCmd.Execute()
}

// NewRootCmd returns the root command (for tests).
func NewRootCmd() *cobra.Command {
	return rootCmd
}

func exitWithSeverity(sev string) {
	switch sev {
	case "critical":
		ExitFunc(2)
	case "warning":
		ExitFunc(1)
	default:
		ExitFunc(0)
	}
}

func fail(msg string, code int) {
	fmt.Fprintln(ErrOut, msg)
	ExitFunc(code)
}
