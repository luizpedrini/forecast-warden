package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/luizpedrini/forecast-warden/internal/cli"
)

func resetCLIFlags(cmd *cobra.Command) {
	for _, c := range cmd.Commands() {
		switch c.Name() {
		case "investigate":
			_ = c.Flags().Set("write", "false")
			_ = c.Flags().Set("llm", "false")
			_ = c.Flags().Set("provider", "")
			_ = c.Flags().Set("config", "warden.yaml")
		case "list":
			_ = c.Flags().Set("status", "")
			_ = c.Flags().Set("config", "warden.yaml")
		case "check":
			_ = c.Flags().Set("run-id", "")
			_ = c.Flags().Set("metrics", "")
			_ = c.Flags().Set("config", "warden.yaml")
		case "show":
			_ = c.Flags().Set("config", "warden.yaml")
		case "resolve":
			_ = c.Flags().Set("note", "")
			_ = c.Flags().Set("status", "resolved")
			_ = c.Flags().Set("config", "warden.yaml")
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
	resetCLIFlags(cmd)
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

var incidentIDRe = regexp.MustCompile(`fw-\d{8}-[0-9a-f]{4}`)

func firstIncidentID(s string) string {
	return incidentIDRe.FindString(s)
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
	// sqlite file must NOT exist until first check
	if _, err := os.Stat(filepath.Join(dir, "data", "warden.db")); err == nil {
		t.Fatal("sqlite should be created on first check, not init")
	}

	out, code = runCLI(t, dir, "check")
	if code != 2 {
		t.Fatalf("check exit=%d (want 2) out=%s", code, out)
	}
	if !strings.Contains(out, "Incident") {
		t.Fatalf("expected Incident in output: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "data", "warden.db")); err != nil {
		t.Fatalf("expected sqlite after check: %v", err)
	}
	// default write_markdown: true
	mds, _ := filepath.Glob(filepath.Join(dir, "incidents", "*.md"))
	if len(mds) < 1 {
		t.Fatal("expected markdown sidecar")
	}

	incidentID := firstIncidentID(out)
	if incidentID == "" {
		t.Fatalf("missing incident id in check output: %s", out)
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
	incidentID := firstIncidentID(out)
	if incidentID == "" {
		t.Fatalf("missing id: %s", out)
	}

	out, code = runCLI(t, dir, "list")
	if code != 0 || !strings.Contains(out, "open") {
		t.Fatalf("list after check: exit=%d out=%s", code, out)
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

	out, code = runCLI(t, dir, "list", "--status", "resolved")
	if code != 0 {
		t.Fatalf("list resolved exit=%d out=%s", code, out)
	}
	if !strings.Contains(out, incidentID) || !strings.Contains(out, "resolved") {
		t.Fatalf("status must remain resolved: %s", out)
	}
	out, code = runCLI(t, dir, "list", "--status", "open")
	if code != 0 {
		t.Fatalf("list open exit=%d", code)
	}
	if strings.Contains(out, incidentID) {
		t.Fatalf("incident must not be open after clobber skip: %s", out)
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
	incidentID := firstIncidentID(out)
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
	incidentID := firstIncidentID(out)
	if incidentID == "" {
		t.Fatal("missing id")
	}

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

func TestFileDriverStillWorks(t *testing.T) {
	dir := t.TempDir()
	out, code := runCLI(t, dir, "init")
	if code != 0 {
		t.Fatalf("init: %s", out)
	}
	cfgPath := filepath.Join(dir, "warden.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(data), "driver: sqlite", "driver: file", 1)
	if err := os.WriteFile(cfgPath, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code = runCLI(t, dir, "check")
	if code != 2 {
		t.Fatalf("check file driver exit=%d out=%s", code, out)
	}
	jsons, _ := filepath.Glob(filepath.Join(dir, "incidents", "*.json"))
	if len(jsons) < 1 {
		t.Fatal("file driver should write json")
	}
	incidentID := firstIncidentID(out)
	out, code = runCLI(t, dir, "list")
	if code != 0 || !strings.Contains(out, incidentID) {
		t.Fatalf("list file: %s", out)
	}
}
