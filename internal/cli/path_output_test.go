package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/sviatoslav/portbook/internal/config"
)

func TestConfigPathOutputEscapesIdentitiesAndPreservesJSON(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprint(terminal), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("PORTBOOK_CONFIG_DIR", "")
			if err := os.Unsetenv("PORTBOOK_CONFIG_DIR"); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"config\nSource: forged\x1b[2J", "tabs\there\rreturn", "unicode\u2028line\u202e", "quote\"slash\\", "trailing "} {
				target := filepath.Join(t.TempDir(), name)
				file := filepath.Join(target, "config.json")
				invoke := func(args ...string) string {
					t.Helper()
					var out, diag bytes.Buffer
					if code := Run(context.Background(), args, &out, &diag, terminal); code != 0 || diag.Len() != 0 {
						t.Fatalf("%v: %d %s %s", args, code, &out, &diag)
					}
					return out.String()
				}
				// A trailing space is internal to the config file path, so only
				// the directory itself needs quoting in this particular case.
				shownFile := strconv.Quote(file)
				if name == "trailing " {
					shownFile = file
				}
				details := fmt.Sprintf("Effective directory: %s\nConfig file: %s\nSource: saved\n", strconv.Quote(target), shownFile)
				if got := invoke("config", "path", "set", target); got != "Saved directory: "+strconv.Quote(target)+"\n"+details {
					t.Fatalf("unescaped set confirmation: %q", got)
				}
				if got := invoke("config", "path", "get", "--output=table"); got != details {
					t.Fatalf("unescaped path report: %q", got)
				}
				var selection config.Selection
				if err := json.Unmarshal([]byte(invoke("config", "path", "get", "--output=json")), &selection); err != nil || selection.Directory != target || selection.ConfigFile != file {
					t.Fatalf("JSON lost path identity: %+v %v", selection, err)
				}
				// Reset confirmations must also escape an active override.
				want := "Saved directory: (none; OS default)\n" + fmt.Sprintf("Effective directory: %s\nConfig file: %s\nSource: flag\n", strconv.Quote(target), shownFile)
				if got := invoke("config", "path", "reset", "--config-dir", target); got != want {
					t.Fatalf("unescaped reset confirmation: %q", got)
				}
			}
		})
	}
}
