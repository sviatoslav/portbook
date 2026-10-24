package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestTerminalDefaultAndOverride(t *testing.T) {
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	for _, tc := range []struct {
		terminal bool
		args     []string
		want     string
	}{
		{true, []string{"config", "show"}, "RANGE        PORTS\n49152-65535  16384\nTotal: 16384 ports.\n"},
		{false, []string{"config", "show"}, "{\"command\":\"config show\",\"ranges\":[{\"start\":49152,\"end\":65535}],\"total\":16384}\n"},
		{false, []string{"config", "show", "--output=table"}, "RANGE        PORTS\n49152-65535  16384\nTotal: 16384 ports.\n"},
		{true, []string{"config", "show", "--output", "json"}, "{\"command\":\"config show\",\"ranges\":[{\"start\":49152,\"end\":65535}],\"total\":16384}\n"},
	} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), tc.args, &out, &diag, tc.terminal); code != 0 || out.String() != tc.want || diag.Len() != 0 {
			t.Fatalf("%v: code=%d out=%q err=%q", tc.args, code, out.String(), diag.String())
		}
	}
}

func TestHelpAndUsage(t *testing.T) {
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	for _, args := range [][]string{nil, {"book"}, {"config", "set", "1"}, {"config", "show", "extra"}, {"--output", "yaml"}, {"--output"}, {"--output=json", "--output=table"}} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), args, &out, &diag, false); code != 2 || out.Len() != 0 || !strings.Contains(diag.String(), `"code":"usage_error"`) {
			t.Fatalf("%v: code=%d out=%s err=%s", args, code, out.String(), diag.String())
		}
	}
	var out, diag bytes.Buffer
	if code := Run(context.Background(), []string{"--help"}, &out, &diag, false); code != 0 || !strings.Contains(out.String(), "Usage:") || !strings.Contains(out.String(), "config") {
		t.Fatalf("help: %d %s %s", code, &out, &diag)
	}
}
