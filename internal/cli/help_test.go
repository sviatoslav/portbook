package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopedHelpBeforeRequiredOperands(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	t.Setenv("PORTBOOK_CONFIG_DIR", dir)
	for _, tc := range []struct {
		args                   []string
		path, include, exclude string
	}{
		{[]string{"--help"}, "portbook", "config", "--all"},
		{[]string{"config", "--help"}, "portbook config", "add", "--all"},
		{[]string{"config", "reset", "--help"}, "portbook config reset", "49152-65535", "--all"},
		{[]string{"config", "show", "--help"}, "portbook config show", "--output", "--all"},
		{[]string{"config", "add", "--help"}, "portbook config add", "50000-50499", "--all"},
		{[]string{"config", "remove", "--help"}, "portbook config remove", "--all", ""},
		{[]string{"--help", "config", "remove"}, "portbook config remove", "--all", ""},
		{[]string{"config", "--help", "add"}, "portbook config add", "50000-50499", "--all"},
	} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), tc.args, &out, &diag, false); code != 0 || diag.Len() != 0 {
			t.Fatalf("%v: %d %s", tc.args, code, &diag)
		}
		var res struct{ Command, Help string }
		if err := json.Unmarshal(out.Bytes(), &res); err != nil {
			t.Fatal(err)
		}
		if res.Command != "help" || !strings.Contains(res.Help, tc.path) || !strings.Contains(res.Help, tc.include) || (tc.exclude != "" && strings.Contains(res.Help, tc.exclude)) {
			t.Fatalf("%v: unexpected help %s", tc.args, res.Help)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("help accessed storage: %v", err)
	}
}

func TestHelpDoesNotBypassValidation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	t.Setenv("PORTBOOK_CONFIG_DIR", dir)
	for _, args := range [][]string{
		{"does-not-exist", "--help"},
		{"config", "typo", "--help"},
		{"config", "ranges", "add", "1", "--help"},
		{"config", "ranges", "remove", "--all"},
		{"config", "reset", "extra", "--help"},
		{"config", "reset", "--all", "--help"},
		{"config", "reset", "--directory", dir, "--help"},
		{"config", "show", "extra", "--help"},
		{"config", "show", "--all", "--help"},
		{"config", "add", "--all", "--help"},
		{"config", "remove", "--all", "1", "--help"},
		{"config", "remove", "--all=false", "1", "--help"},
		{"config", "add", "bad", "--help"},
		{"config", "remove", "0", "--help"},
		{"config", "--unknown", "--help"},
		{"--output=yaml", "--help"},
		{"--help", "--output=json", "--output=table"},
		{"config", "remove", "--all", "--all", "--help"},
	} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), args, &out, &diag, false); code != 2 || out.Len() != 0 || !strings.Contains(diag.String(), `"code":"usage_error"`) {
			t.Fatalf("%v: %d stdout=%s stderr=%s", args, code, &out, &diag)
		}
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("invalid invocation accessed storage: %v", err)
	}
}

func TestEndOfFlags(t *testing.T) {
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	var out, diag bytes.Buffer
	args := []string{"config", "add", "--", "1"}
	if code := Run(context.Background(), args, &out, &diag, false); code != 0 || out.String() != "Added 1 port. Total: 16385 ports.\n" {
		t.Fatalf("%d %s %s", code, &out, &diag)
	}
	out.Reset()
	diag.Reset()
	args = []string{"config", "add", "--", "--help"}
	if code := Run(context.Background(), args, &out, &diag, false); code != 2 || out.Len() != 0 {
		t.Fatalf("literal flag treated as option: %d %s %s", code, &out, &diag)
	}
}
