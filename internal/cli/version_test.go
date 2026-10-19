package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVersionOutputWithoutState(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "absent-home")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "absent-xdg"))
	t.Setenv("PORTBOOK_CONFIG_DIR", "invalid-unused-selection")
	for _, terminal := range []bool{true, false} {
		for _, format := range []string{"json", "table"} {
			var out, diag bytes.Buffer
			code := Run(context.Background(), []string{"--version", "--output", format}, &out, &diag, terminal)
			if code != 0 || out.String() != "portbook dev (commit unknown)\n" || diag.Len() != 0 {
				t.Fatalf("version: code=%d out=%q diagnostics=%q", code, &out, &diag)
			}
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("version wrote state: %v %v", entries, err)
	}
}

func TestVersionValidationAndHelp(t *testing.T) {
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	for _, args := range [][]string{
		{"--version", "extra"}, {"--version", "--output", "bad"},
		{"--version", "--config-dir", "relative"}, {"--version", "--unknown"},
		{"--version", "--version"}, {"config", "show", "--version"},
	} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), args, &out, &diag, false); code != 2 || out.Len() != 0 || !strings.Contains(diag.String(), `"code":"usage_error"`) {
			t.Fatalf("%v: code=%d out=%q diagnostics=%q", args, code, &out, &diag)
		}
	}
	var out, diag bytes.Buffer
	if code := Run(context.Background(), []string{"--help"}, &out, &diag, false); code != 0 || !strings.Contains(out.String(), "--version") {
		t.Fatalf("version help: %d %s %s", code, &out, &diag)
	}
}

func TestVersionOutputFailure(t *testing.T) {
	var diagnostics bytes.Buffer
	if code := Run(context.Background(), []string{"--version"}, failedWriter{}, &diagnostics, false); code != 1 || !strings.Contains(diagnostics.String(), `"code":"runtime_error"`) {
		t.Fatal(code, diagnostics.String())
	}
}
