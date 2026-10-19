package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocationEnvironmentPrecedenceAndDiagnostics(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	env, flag := t.TempDir(), t.TempDir()
	t.Setenv("PORTBOOK_CONFIG_DIR", env)
	invoke := func(code int, args ...string) string {
		t.Helper()
		var out, diag bytes.Buffer
		got := Run(context.Background(), args, &out, &diag, false)
		if got != code {
			t.Fatalf("%v: %d want %d out=%s diag=%s", args, got, code, &out, &diag)
		}
		if code == 0 {
			if diag.Len() != 0 {
				t.Fatal(&diag)
			}
			return out.String()
		}
		if out.Len() != 0 {
			t.Fatal(&out)
		}
		return diag.String()
	}
	got := invoke(0, "config", "path", "set", flag)
	if !strings.Contains(got, `"source":"environment"`) || !strings.Contains(got, `"directory":"`+env+`"`) {
		t.Fatal(got)
	}
	got = invoke(0, "config", "path", "get", "--config-dir", flag)
	if !strings.Contains(got, `"source":"flag"`) {
		t.Fatal(got)
	}
	t.Setenv("PORTBOOK_CONFIG_DIR", "relative")
	invoke(1, "config", "path", "get")
	invoke(1, "config", "path", "get", "--help")
	invoke(0, "--config-dir", flag, "config", "path", "get")
	// An output-shaped config-dir value is consumed as a value, not a format.
	got = invoke(2, "config", "path", "get", "--config-dir", "--output=table")
	if !strings.Contains(got, `"code":"usage_error"`) {
		t.Fatal(got)
	}
	got = invoke(2, "config", "path", "get", "--config-dir", "--output=table", "--output=json")
	if !strings.Contains(got, `"code":"usage_error"`) {
		t.Fatal(got)
	}
}

func TestPathHelpPointerRepairAndNoWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	// Obtain the OS root without a production test-only override.
	l, err := configLocationForTest()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(l, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(l, "location.json")
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORTBOOK_CONFIG_DIR", "")
	if err := os.Unsetenv("PORTBOOK_CONFIG_DIR"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"config", "path", "get", "--help"}, 1},
		{[]string{"config", "path", "set", "--help"}, 0},
		{[]string{"config", "path", "reset", "--help"}, 0},
	} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), tc.args, &out, &diag, false); code != tc.code {
			t.Fatalf("%v: %d %s %s", tc.args, code, &out, &diag)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "broken" {
		t.Fatal("help repaired evidence", err)
	}
	if _, err := os.Stat(filepath.Join(l, "state.lock")); !os.IsNotExist(err) {
		t.Fatal("help created lock", err)
	}
}

func configLocationForTest() (string, error) {
	base, err := os.UserConfigDir()
	return filepath.Join(base, "portbook"), err
}

func TestDiagnosticConfigDirEndBoundary(t *testing.T) {
	for _, args := range [][]string{
		{"--config-dir", "--", "--output=table"},
		{"--config-dir", "--", "--output", "table"},
	} {
		if got := diagnosticOutput(args, "json"); got != "json" {
			t.Fatalf("%v: %s", args, got)
		}
	}
}

func TestNonUTF8ConfigSelectionsFailBeforeWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	invalid := filepath.Join(t.TempDir(), "invalid-") + string([]byte{0xff})
	t.Setenv("PORTBOOK_CONFIG_DIR", invalid)
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"config", "path", "set", invalid}, 2},
		{[]string{"config", "path", "get", "--config-dir", invalid}, 2},
		{[]string{"config", "path", "get", "--config-dir", invalid, "--help"}, 2},
		{[]string{"config", "ranges", "add", "1234"}, 1},
	} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), tc.args, &out, &diag, false); code != tc.code || out.Len() != 0 || !strings.Contains(diag.String(), "UTF-8") {
			t.Fatalf("%v: exit=%d out=%s diag=%s", tc.args, code, &out, &diag)
		}
	}
	root, err := configLocationForTest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("invalid selection wrote bootstrap state", err)
	}
	valid := filepath.Join(t.TempDir(), "valid")
	var out, diag bytes.Buffer
	if code := Run(context.Background(), []string{"--config-dir", valid, "config", "path", "get"}, &out, &diag, false); code != 0 || !strings.Contains(out.String(), `"source":"flag"`) {
		t.Fatalf("ignored invalid lower-priority env blocks flag: %d %s %s", code, &out, &diag)
	}
}
