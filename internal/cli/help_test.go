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
		{[]string{"config", "--help"}, "portbook config", "ranges", "--all"},
		{[]string{"config", "ranges", "--help"}, "portbook config ranges", "remove", "--all"},
		{[]string{"config", "show", "--help"}, "portbook config show", "--output", "--all"},
		{[]string{"config", "ranges", "add", "--help"}, "portbook config ranges add", "50000-50499", "--all"},
		{[]string{"config", "ranges", "remove", "--help"}, "portbook config ranges remove", "--all", ""},
		{[]string{"--help", "config", "ranges", "remove"}, "portbook config ranges remove", "--all", ""},
		{[]string{"config", "--help", "ranges", "add"}, "portbook config ranges add", "50000-50499", "--all"},
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
		{"config", "ranges", "typo", "--help"},
		{"config", "show", "extra", "--help"},
		{"config", "show", "--all", "--help"},
		{"config", "ranges", "add", "--all", "--help"},
		{"config", "ranges", "remove", "--all", "1", "--help"},
		{"config", "ranges", "remove", "--all=false", "1", "--help"},
		{"config", "ranges", "add", "bad", "--help"},
		{"config", "ranges", "remove", "0", "--help"},
		{"config", "--unknown", "--help"},
		{"--output=yaml", "--help"},
		{"--help", "--output=json", "--output=table"},
		{"config", "ranges", "remove", "--all", "--all", "--help"},
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
	args := []string{"config", "ranges", "add", "--", "1"}
	if code := Run(context.Background(), args, &out, &diag, false); code != 0 || !strings.Contains(out.String(), `"changed":1`) {
		t.Fatalf("%d %s %s", code, &out, &diag)
	}
	out.Reset()
	diag.Reset()
	args = []string{"config", "ranges", "add", "--", "--help"}
	if code := Run(context.Background(), args, &out, &diag, false); code != 2 || out.Len() != 0 {
		t.Fatalf("literal flag treated as option: %d %s %s", code, &out, &diag)
	}
}
