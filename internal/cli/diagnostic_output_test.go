package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiagnosticOutputOnUsageFailure(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprintf("terminal=%v", terminal), func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "absent")
			t.Setenv("PORTBOOK_CONFIG_DIR", dir)
			for _, tc := range []struct {
				args []string
				want string // Empty means the terminal-dependent default.
			}{
				{[]string{"config", "show", "--bogus"}, ""},
				{[]string{"reserve", "default", "--directory", "--output=table", "--output=json"}, "json"},
				{[]string{"reserve", "default", "--directory", "--output=json", "--output=table"}, "table"},
				{[]string{"reserve", "--bogus", "--directory", "--output=table", "--output=json"}, "json"},
				{[]string{"reserve", "default", "--directory", "--output=json"}, ""},
				{[]string{"reserve", "default", "--directory", "--", "--output=table"}, ""},
				{[]string{"reserve", "default", "--directory=--output=table", "--output=json", "--help"}, "json"},
				{[]string{"config", "show", "--directory", "--output=table", "--output=json"}, "table"},
				{[]string{"--output", "json", "config", "show", "--bogus"}, "json"},
				{[]string{"config", "show", "--bogus", "--output", "json"}, "json"},
				{[]string{"--output=table", "config", "show", "--bogus"}, "table"},
				{[]string{"config", "show", "--bogus", "--output=table"}, "table"},
				{[]string{"config", "show", "--bogus", "--output", "table"}, "table"},
				{[]string{"config", "show", "--bogus", "--output=json"}, "json"},
				{[]string{"config", "show", "--bogus", "--help", "--output=json"}, "json"},
				{[]string{"config", "add", "--all", "--help", "--output=table"}, "table"},
				{[]string{"config", "remove", "--all", "1", "--help", "--output=json"}, "json"},
				{[]string{"unknown", "--help", "--output=table"}, "table"},
				{[]string{"config", "--output=json"}, "json"},
				{[]string{"config", "show", "--", "--output", "json"}, ""},
				{[]string{"config", "show", "--bogus", "--", "--output=table"}, ""},
				{[]string{"--output=json", "config", "show", "--", "--output=table"}, "json"},
				{[]string{"config", "show", "--bogus", "--output=table", "--", "--output=json"}, "table"},
				{[]string{"config", "show", "--bogus=--output=table"}, ""},
				{[]string{"--output=yaml", "--help"}, ""},
				{[]string{"config", "show", "--bogus", "--output", "yaml"}, ""},
				{[]string{"config", "show", "--bogus", "--output="}, ""},
				{[]string{"config", "show", "--bogus", "--output"}, ""},
				{[]string{"config", "show", "--output"}, ""},
				{[]string{"config", "show", "--output", "--help"}, ""},
				{[]string{"config", "show", "--bogus", "--output", "--", "--output=json"}, ""},
				{[]string{"--output=json", "--output=table", "config", "show"}, "json"},
				{[]string{"config", "show", "--bogus", "--output=table", "--output=json"}, "table"},
				{[]string{"config", "show", "--bogus", "--output=json", "--output"}, "json"},
				{[]string{"config", "show", "--bogus", "--output=yaml", "--output=json"}, ""},
			} {
				var out, diag bytes.Buffer
				code := Run(context.Background(), tc.args, &out, &diag, terminal)
				if code != 2 || out.Len() != 0 {
					t.Fatalf("%v: exit=%d stdout=%s stderr=%s", tc.args, code, &out, &diag)
				}
				want := tc.want
				if want == "" {
					want = "json"
					if terminal {
						want = "table"
					}
				}
				if want == "json" {
					var res struct {
						Error diagnostic `json:"error"`
					}
					if err := json.Unmarshal(diag.Bytes(), &res); err != nil || res.Error.Code != "usage_error" || res.Error.Message == "" {
						t.Fatalf("%v: expected JSON usage error, got %s (%v)", tc.args, &diag, err)
					}
				} else if !strings.HasPrefix(diag.String(), "Error: ") {
					t.Fatalf("%v: expected table/text diagnostic, got %s", tc.args, &diag)
				}
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("usage failure accessed storage: %v", err)
			}
		})
	}
}
