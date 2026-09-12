package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luizpedrini/forecast-warden/internal/cli"
)

func runCLI(t *testing.T, dir string, args ...string) (stdout string, code int) {
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
		if errBuf.Len() > 0 && stdout == "" {
			stdout = errBuf.String()
		}
	}()

	cmd := cli.NewRootCmd()
	cmd.SetArgs(args)
	cmd.SetOut(&buf)
	cmd.SetErr(&errBuf)
	err = cmd.Execute()
	if err != nil && !exited {
		t.Fatalf("execute error: %v\nstderr=%s", err, errBuf.String())
	}
	stdout = buf.String()
	return stdout, code
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
