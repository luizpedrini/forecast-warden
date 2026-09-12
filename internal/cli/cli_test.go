package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/luizpedrini/forecast-warden/internal/cli"
)

func resetInvestigateFlags(cmd *cobra.Command) {
	for _, c := range cmd.Commands() {
		if c.Name() == "investigate" {
			_ = c.Flags().Set("write", "false")
			_ = c.Flags().Set("llm", "false")
			_ = c.Flags().Set("provider", "")
		}
	}
}

func runCLI(t *testing.T, dir string, args ...string) (stdout string, code int) {
	t.Helper()
	stdout, _, code = runCLIFull(t, dir, args...)
	return stdout, code
}

func runCLIFull(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(oldWD) }()

	var buf bytes.Buffer
	var errBuf bytes.Buffer
	oldOut, oldErr, oldExit := cli.Out, cli.ErrOut, cli.ExitFunc
	cli.Out = &buf
	cli.ErrOut = &errBuf
	code = 0
	exited := false
	cli.ExitFunc = func(c int) {
		code = c
		exited = true
		panic("cli-exit")
	}
	defer func() {
		cli.Out, cli.ErrOut, cli.ExitFunc = oldOut, oldErr, oldExit
		if r := recover(); r != nil {
			if r != "cli-exit" {
				panic(r)
			}
		}
		stdout = buf.String()
		stderr = errBuf.String()
		if errBuf.Len() > 0 && stdout == "" {
			stdout = errBuf.String()
		}
	}()

	cmd := cli.NewRootCmd()
	resetInvestigateFlags(cmd)
	cmd.SetArgs(args)
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	err = cmd.Execute()
	if err != nil && !exited {
		t.Fatalf("execute error: %v\nstderr=%s", err, errBuf.String())
	}
	stdout = buf.String()
	stderr = errBuf.String()
	return stdout, stderr, code
}

func TestInitCheckListShowResolve(t *testing.T) {
	dir := t.TempDir()
	out, code := runCLI(t, dir, "init")
	if code != 0 {
		t.Fatalf("init exit=%d out=%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "warden.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "run_metrics.csv")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "baseline_stats.csv")); err != nil {
		t.Fatal(err)
	}

	out, code = runCLI(t, dir, "check")
	if code != 2 {
		t.Fatalf("check exit=%d (want 2) out=%s", code, out)
	}
	if !strings.Contains(out, "Incident") {
		t.Fatalf("expected Incident in output: %s", out)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "incidents", "*.json"))
	if len(matches) < 1 {
		t.Fatal("expected incident json")
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	incidentID, _ := payload["id"].(string)
	if incidentID == "" {
		t.Fatal("missing id")
	}

	out, code = runCLI(t, dir, "list")
	if code != 0 {
		t.Fatalf("list exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "fw-") {
		t.Fatalf("list missing fw-: %s", out)
	}

	out, code = runCLI(t, dir, "show", incidentID)
	if code != 0 {
		t.Fatalf("show exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, incidentID) {
		t.Fatalf("show missing id: %s", out)
	}

	out, code = runCLI(t, dir, "resolve", incidentID, "--note", "looked fine after review")
	if code != 0 {
		t.Fatalf("resolve exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "resolved") {
		t.Fatalf("resolve output: %s", out)
	}

	out, code = runCLI(t, dir, "list", "--status", "resolved")
	if code != 0 {
		t.Fatalf("list resolved exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, "resolved") {
		t.Fatalf("list resolved: %s", out)
	}
}

func TestCheckDoesNotClobberResolvedIncident(t *testing.T) {
	dir := t.TempDir()
	out, code := runCLI(t, dir, "init")
	if code != 0 {
		t.Fatalf("init exit=%d out=%s", code, out)
	}

	out, code = runCLI(t, dir, "check")
	if code != 2 {
		t.Fatalf("first check exit=%d (want 2) out=%s", code, out)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "incidents", "*.json"))
	if len(matches) != 1 {
		t.Fatalf("expected 1 incident json, got %d", len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	incidentID, _ := payload["id"].(string)
	if incidentID == "" {
		t.Fatal("missing id")
	}
	if payload["status"] != "open" {
		t.Fatalf("status after first check=%v want open", payload["status"])
	}

	out, code = runCLI(t, dir, "resolve", incidentID, "--note", "reviewed; false alarm")
	if code != 0 {
		t.Fatalf("resolve exit=%d out=%s", code, out)
	}

	out, code = runCLI(t, dir, "check")
	if code != 2 {
		t.Fatalf("second check exit=%d (want 2 from findings) out=%s", code, out)
	}
	if !strings.Contains(out, "already resolved") || !strings.Contains(out, "not overwriting") {
		t.Fatalf("expected skip-overwrite message, got: %s", out)
	}

	data, err = os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "resolved" {
		t.Fatalf("status after second check=%v want resolved (must not clobber)", payload["status"])
	}
	if payload["id"] != incidentID {
		t.Fatalf("id changed: %v", payload["id"])
	}
	matchesAfter, _ := filepath.Glob(filepath.Join(dir, "incidents", "*.json"))
	if len(matchesAfter) != 1 {
		t.Fatalf("expected still 1 incident json, got %d", len(matchesAfter))
	}
}

func TestInvestigatePlaybook(t *testing.T) {
	dir := t.TempDir()
	out, code := runCLI(t, dir, "init")
	if code != 0 {
		t.Fatalf("init exit=%d out=%s", code, out)
	}
	out, code = runCLI(t, dir, "check")
	if code != 2 {
		t.Fatalf("check exit=%d out=%s", code, out)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "incidents", "*.json"))
	if len(matches) < 1 {
		t.Fatal("expected incident")
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	incidentID, _ := payload["id"].(string)
	if incidentID == "" {
		t.Fatal("missing id")
	}

	out, code = runCLI(t, dir, "investigate", incidentID)
	if code != 0 {
		t.Fatalf("investigate exit=%d out=%s", code, out)
	}
	for _, section := range []string{
		"## Context",
		"## Attack order",
		"## Per-finding questions",
		"## 15-min checklist",
		"## Suggested resolve decision",
		"Ready-made",
	} {
		if !strings.Contains(out, section) {
			t.Fatalf("missing %q in investigate output:\n%s", section, out)
		}
	}
	// Synthetic golden has UnstableForecast + HighWAPE critical — stability before WAPE in attack order.
	attackIdx := strings.Index(out, "## Attack order")
	perIdx := strings.Index(out, "## Per-finding questions")
	if attackIdx < 0 || perIdx <= attackIdx {
		t.Fatal("attack/per-finding sections")
	}
	block := out[attackIdx:perIdx]
	u := strings.Index(block, "UnstableForecast")
	w := strings.Index(block, "HighWAPE")
	if u < 0 || w < 0 || u > w {
		t.Fatalf("want UnstableForecast before HighWAPE in attack order:\n%s", block)
	}

	out, code = runCLI(t, dir, "investigate", incidentID, "--write")
	if code != 0 {
		t.Fatalf("investigate --write exit=%d out=%s", code, out)
	}
	playbook := filepath.Join(dir, "incidents", incidentID+".investigate.md")
	if _, err := os.Stat(playbook); err != nil {
		t.Fatalf("expected playbook file %s: %v", playbook, err)
	}

	out, code = runCLI(t, dir, "investigate", "does-not-exist-xyz")
	if code == 0 {
		t.Fatal("expected non-zero exit for missing id")
	}
	if !strings.Contains(out, "not found") && !strings.Contains(out, "Incident not found") {
		t.Fatalf("expected clear not-found error, got: %s", out)
	}
}

func TestInvestigateLLMFallbackWithoutKey(t *testing.T) {
	dir := t.TempDir()
	out, code := runCLI(t, dir, "init")
	if code != 0 {
		t.Fatalf("init exit=%d out=%s", code, out)
	}
	out, code = runCLI(t, dir, "check")
	if code != 2 {
		t.Fatalf("check exit=%d out=%s", code, out)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "incidents", "*.json"))
	if len(matches) < 1 {
		t.Fatal("expected incident")
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	incidentID, _ := payload["id"].(string)

	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("WARDEN_LLM_PROVIDER", "")

	stdout, stderr, code := runCLIFull(t, dir, "investigate", incidentID, "--llm")
	if code != 0 {
		t.Fatalf("investigate --llm without key should exit 0, got %d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, "--llm") {
		t.Fatalf("expected stderr warning about --llm, got: %s", stderr)
	}
	if !strings.Contains(stdout, "## Context") {
		t.Fatalf("expected rule-based playbook on stdout: %s", stdout)
	}
	if strings.Contains(stdout, "## LLM synthesis") {
		t.Fatal("must not include LLM synthesis when key missing")
	}
}
