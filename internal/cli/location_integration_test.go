package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// locationCommand isolates the OS default root while leaving selection unset.
func locationCommand(home string, args ...string) *exec.Cmd {
	cmd := command(home, args...)
	var env []string
	for _, value := range cmd.Env {
		if !strings.HasPrefix(value, "PORTBOOK_CONFIG_DIR=") {
			env = append(env, value)
		}
	}
	cmd.Env = env
	return cmd
}

func locationRun(t *testing.T, home string, code int, args ...string) string {
	t.Helper()
	cmd := locationCommand(home, args...)
	var out, diag bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diag
	err := cmd.Run()
	actual := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		actual = exit.ExitCode()
	}
	if actual != code {
		t.Fatalf("%v: code=%d want=%d out=%s err=%s", args, actual, code, &out, &diag)
	}
	if code != 0 {
		if out.Len() != 0 {
			t.Fatal("error stdout", &out)
		}
		return diag.String()
	}
	if diag.Len() != 0 {
		t.Fatal("success stderr", &diag)
	}
	return out.String()
}

func TestLocationCLIAndPersistence(t *testing.T) {
	home := filepath.Join(t.TempDir(), "isolated")
	var initial struct{ Directory string }
	// Use a map because config_file has a deliberately explicit JSON name.
	var get map[string]any
	if err := json.Unmarshal([]byte(locationRun(t, home, 0, "config", "path", "get")), &get); err != nil {
		t.Fatal(err)
	}
	initial.Directory = get["directory"].(string)
	if get["source"] != "default" || get["config_file"] != filepath.Join(initial.Directory, "config.json") {
		t.Fatal(get)
	}
	if _, err := os.Stat(initial.Directory); !os.IsNotExist(err) {
		t.Fatal("get wrote files", err)
	}
	target := filepath.Join(t.TempDir(), "custom")
	got := locationRun(t, home, 0, "config", "path", "set", target)
	if !strings.Contains(got, `"source":"saved"`) || !strings.Contains(got, `"saved_directory":`) {
		t.Fatal(got)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("set created destination", err)
	}
	locationRun(t, home, 0, "config", "ranges", "add", "50123")
	got = locationRun(t, home, 0, "config", "show")
	if !strings.Contains(got, `"total":1`) {
		t.Fatal(got)
	}
	other := t.TempDir()
	got = locationRun(t, home, 0, "config", "path", "set", other, "--config-dir", target)
	if !strings.Contains(got, `"source":"flag"`) || !strings.Contains(got, `"saved_directory":"`+other+`"`) {
		t.Fatal(got)
	}
	got = locationRun(t, home, 0, "--config-dir", target, "config", "path", "reset", "--output=table")
	if !strings.Contains(got, "Saved directory: (none; OS default)") || !strings.Contains(got, "Source: flag") {
		t.Fatal(got)
	}
	got = locationRun(t, home, 0, "config", "path", "get")
	if !strings.Contains(got, `"source":"default"`) {
		t.Fatal(got)
	}
	got = locationRun(t, home, 0, "config", "show", "--config-dir", target)
	if !strings.Contains(got, `"total":1`) {
		t.Fatal("reset deleted config", got)
	}
	locationRun(t, home, 0, "config", "path", "reset")
}

func TestLocationCLIValidationAndHelp(t *testing.T) {
	home := filepath.Join(t.TempDir(), "isolated")
	target := t.TempDir()
	for _, args := range [][]string{
		{"config", "path", "set"}, {"config", "path", "set", "relative"}, {"config", "path", "get", "extra"},
		{"config", "path", "reset", "extra"}, {"config", "path", "bad", "--help"},
		{"config", "path", "set", "relative", "--help"}, {"--config-dir=relative", "config", "path", "get", "--help"},
		{"--config-dir", target, "config", "path", "get", "--config-dir", target},
		{"config", "path", "get", "--config-dir"}, {"config", "path", "get", "--config-dir=", "--help"},
		{"config", "path", "get", "--unknown", "--output=json"},
		{"config", "path", "get", "--", "--config-dir", target},
		{"config", "path", "set", "--", "--help"},
	} {
		got := locationRun(t, home, 2, args...)
		if !strings.Contains(got, `"code":"usage_error"`) {
			t.Fatal(args, got)
		}
	}
	for _, args := range [][]string{{"config", "path", "--help"}, {"config", "path", "set", "--help"}, {"config", "path", "get", "--help"}, {"config", "path", "reset", "--help"}} {
		locationRun(t, home, 0, args...)
	}
	locationRun(t, home, 0, "config", "path", "set", "--", target)
}

func TestConcurrentLocationSwitchesAndRangeProcesses(t *testing.T) {
	home := filepath.Join(t.TempDir(), "isolated")
	var get struct{ Directory string }
	if err := json.Unmarshal([]byte(locationRun(t, home, 0, "config", "path", "get")), &get); err != nil {
		t.Fatal(err)
	}
	targets := []string{get.Directory, t.TempDir(), t.TempDir()}
	var commands []*exec.Cmd
	var diagnostics []*bytes.Buffer
	const workers = 24
	for i := 0; i < workers; i++ {
		for _, args := range [][]string{
			{"config", "ranges", "add", fmt.Sprint(52000 + i)},
			{"config", "path", "set", targets[1+i%2]},
			{"config", "path", "reset"},
		} {
			cmd := locationCommand(home, args...)
			diag := new(bytes.Buffer)
			cmd.Stderr = diag
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			commands = append(commands, cmd)
			diagnostics = append(diagnostics, diag)
		}
	}
	for i, cmd := range commands {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("%v %s", err, diagnostics[i])
		}
	}
	total := 0
	for _, target := range targets {
		var result struct{ Total int }
		if err := json.Unmarshal([]byte(locationRun(t, home, 0, "--config-dir", target, "config", "show")), &result); err != nil {
			t.Fatal(err)
		}
		total += result.Total
	}
	if total != workers {
		t.Fatalf("lost concurrent config updates during switches: %d", total)
	}
	for _, target := range targets[1:] {
		if _, err := os.Stat(filepath.Join(target, "state.lock")); !os.IsNotExist(err) {
			t.Fatal("split lock", err)
		}
	}
}

func TestBootstrapConflictsFailWithoutCreatingState(t *testing.T) {
	home := filepath.Join(t.TempDir(), "isolated")
	var initial struct{ Directory string }
	if err := json.Unmarshal([]byte(locationRun(t, home, 0, "config", "path", "get")), &initial); err != nil {
		t.Fatal(err)
	}
	reserved := filepath.Join(initial.Directory, "location.json", "nested")
	for _, args := range [][]string{
		{"config", "path", "set", reserved},
		{"config", "path", "set", reserved, "--help"},
		{"--config-dir", reserved, "config", "path", "get"},
		{"config", "path", "get", "--config-dir", reserved, "--help"},
		{"--config-dir", reserved, "config", "ranges", "add", "1234"},
	} {
		got := locationRun(t, home, 1, args...)
		if !strings.Contains(got, `"code":"storage_error"`) || !strings.Contains(got, "reserved bootstrap") {
			t.Fatal(args, got)
		}
	}
	if _, err := os.Stat(initial.Directory); !os.IsNotExist(err) {
		t.Fatal("bootstrap conflict created state", err)
	}
}
