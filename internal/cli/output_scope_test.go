package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStructuredOutputScopes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	target := t.TempDir()
	for _, args := range [][]string{
		{"list", "--output", "json"}, {"cleanup", "--output=table", "--dry-run"},
		{"config", "--output=json", "show"}, {"config", "show", "--output", "table"},
		{"config", "--output=table", "path", "get"}, {"config", "path", "--output=json", "get"},
		{"config", "path", "get", "--output=table"},
		{"config", "--output=json", "path", "set", target},
		{"config", "path", "set", target, "--output=table"},
		{"config", "path", "reset", "--output=json"}, {"config", "reset", "--output=json"},
		{"config", "--output=json", "add", "1"}, {"config", "add", "--output=json", "2"},
		{"config", "add", "3", "--output=table"}, {"config", "remove", "1", "--output=json"},
		{"config", "--output=table", "remove", "--all"},
		{"config", "--output=table", "reset", "--help"}, {"list", "--output=json", "--help"},
	} {
		resetInvoke(t, 0, args...)
	}
}

func TestUnsupportedOutputNeverRunsOrCreatesState(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "xdg"))
	t.Setenv("PORTBOOK_CONFIG_DIR", filepath.Join(base, "pool"))
	owner := t.TempDir()
	for _, terminal := range []bool{false, true} {
		// Choose the opposite of the default, proving an unsupported flag is not
		// silently honored even for diagnostics.
		format := "table"
		if terminal {
			format = "json"
		}
		for _, args := range [][]string{
			{"--output", format, "list"}, {"--config-dir", filepath.Join(base, "pool"), "--output=" + format, "config", "reset"},
			{"--output=" + format, "config", "show", "--help"}, {"--output=" + format, "cleanup", "--dry-run"},
			{"--help", "--output=" + format}, {"--version", "--output=" + format},
			{"get", "api", "--directory", owner, "--output=" + format}, {"reserve", "api", "--directory", owner, "--output=" + format},
			{"get", "--output=" + format, "--help"}, {"reserve", "--help", "--output=" + format},
			{"release", "api", "--directory", owner, "--output=" + format},
			{"release", "--all", "--directory", owner, "--output=" + format},
			{"release", "--help", "--output=" + format},
			{"install-skills", "--agent=codex", "--output=" + format}, {"install-skills", "--help", "--output=" + format},
			{"install-skills", "--bogus", "--output=" + format}, {"--output=" + format, "install-skills"},
		} {
			var out, diag bytes.Buffer
			installer := skillInstaller{lookup: func(string) (string, error) { t.Fatal("invalid flags invoked installer"); return "", nil }}
			if code := run(context.Background(), args, &out, &diag, terminal, installer); code != 2 || out.Len() != 0 || strings.HasPrefix(diag.String(), "{") == terminal {
				t.Fatalf("%v terminal=%v: %d %s %s", args, terminal, code, &out, &diag)
			}
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid output flag wrote state", entries, err)
	}
}

func TestHelpListsOnlyApplicableOutputFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	for _, command := range []string{"", "get", "reserve", "install-skills", "release", "list", "cleanup", "config"} {
		args := []string{"--help"}
		if command != "" {
			args = []string{command, "--help"}
		}
		got := resetInvoke(t, 0, args...)
		want := command == "list" || command == "cleanup" || command == "config"
		if strings.Contains(got, "--output string") != want {
			t.Fatal(command, got)
		}
	}
}
