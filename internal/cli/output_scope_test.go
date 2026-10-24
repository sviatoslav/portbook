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

func TestStructuredOutputScopes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	for _, args := range [][]string{
		{"list", "--output", "json"}, {"cleanup", "--output=table", "--dry-run"},
		{"config", "show", "--output", "table"}, {"config", "show", "--output=json"},
		{"config", "path", "get", "--output=table"}, {"config", "path", "get", "--output", "json"},
		{"config", "show", "--output=table", "--help"}, {"list", "--output=json", "--help"},
	} {
		resetInvoke(t, 0, args...)
	}
}

func TestOutputScopeWithPrefixLeafValues(t *testing.T) {
	base, owner := t.TempDir(), t.TempDir()
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "xdg"))
	t.Setenv("PORTBOOK_CONFIG_DIR", filepath.Join(base, "pool"))
	for _, terminal := range []bool{false, true} {
		format := "table"
		if terminal {
			format = "json"
		}
		for _, prefix := range [][]string{{"--directory", owner}, {"--directory=" + owner}} {
			for _, output := range [][]string{{"--output", format}, {"--output=" + format}} {
				for _, help := range []bool{false, true} {
					args := append(append([]string{}, prefix...), output...)
					args = append(args, "list")
					if help {
						args = append(args, "--help")
					}
					var out, diag bytes.Buffer
					if code := Run(context.Background(), args, &out, &diag, terminal); code != 2 || out.Len() != 0 || strings.HasPrefix(diag.String(), "{") == terminal {
						t.Fatalf("%v terminal=%v: %d %s %s", args, terminal, code, &out, &diag)
					}
				}
			}
		}
		// Correctly scoped output still controls diagnostics after a prefix leaf
		// flag, including when that flag's value resembles a command or output.
		for _, value := range []string{owner, "list", "--output=" + format} {
			args := []string{"--directory", value, "list", "--bogus", "--output", format}
			var out, diag bytes.Buffer
			if code := Run(context.Background(), args, &out, &diag, terminal); code != 2 || out.Len() != 0 || strings.HasPrefix(diag.String(), "{") != terminal {
				t.Fatalf("%v terminal=%v: %d %s %s", args, terminal, code, &out, &diag)
			}
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 {
		t.Fatal("invalid flag placement wrote state", entries, err)
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
			{"config", "add", "1", "--output=" + format}, {"config", "remove", "49152", "--output=" + format},
			{"config", "remove", "--all", "--output=" + format}, {"config", "reset", "--output=" + format},
			{"config", "path", "set", owner, "--output=" + format}, {"config", "path", "reset", "--output=" + format},
			{"config", "--output=" + format, "show"}, {"config", "path", "--output=" + format, "get"},
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
	for _, command := range []string{"", "get", "reserve", "install-skills", "release", "list", "cleanup", "config", "config path", "config show", "config add", "config remove", "config reset", "config path get", "config path set", "config path reset"} {
		args := []string{"--help"}
		if command != "" {
			args = append(strings.Fields(command), "--help")
		}
		got := resetInvoke(t, 0, args...)
		want := command == "list" || command == "cleanup" || command == "config show" || command == "config path get"
		if strings.Contains(got, "--output string") != want {
			t.Fatal(command, got)
		}
	}
}

func TestConfigOutputRejectedBeforeAnyStateChange(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	pool, target := t.TempDir(), t.TempDir()
	t.Setenv("PORTBOOK_CONFIG_DIR", pool)
	resetInvoke(t, 0, "config", "path", "set", target)
	resetInvoke(t, 0, "config", "reset")
	root, err := configLocationForTest()
	if err != nil {
		t.Fatal(err)
	}
	// Invalid booking evidence must not be read or repaired by flag rejection.
	if err := os.WriteFile(filepath.Join(root, "bookings.json"), []byte("booking evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	type entrySnapshot struct {
		contents string
		info     os.FileInfo
	}
	snapshot := func() map[string]entrySnapshot {
		t.Helper()
		files := map[string]entrySnapshot{}
		for _, dir := range []string{root, pool, target} {
			err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					files[path] = entrySnapshot{contents: "directory"}
					return nil
				}
				data, err := os.ReadFile(path)
				info, statErr := os.Stat(path)
				if statErr != nil {
					return statErr
				}
				files[path] = entrySnapshot{string(data), info}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		return files
	}
	before := snapshot()
	commands := [][]string{
		{"config", "add", "1"}, {"config", "remove", "49152"}, {"config", "remove", "--all"},
		{"config", "reset"}, {"config", "path", "set", target}, {"config", "path", "reset"},
		{"config"}, {"config", "path"},
	}
	for _, terminal := range []bool{false, true} {
		for _, command := range commands {
			for _, flag := range [][]string{{"--output=json"}, {"--output", "table"}, {"--output=yaml"}, {"--output"}, {"--output=json", "--output=table"}} {
				// Before operands, after operands, and combined with help.
				for _, position := range []int{len(command), len(command) - 1} {
					args := append([]string{}, command[:position]...)
					args = append(args, flag...)
					args = append(args, command[position:]...)
					for _, help := range []bool{false, true} {
						argv := append([]string{}, args...)
						if help {
							argv = append(argv, "--help")
						}
						var out, diag bytes.Buffer
						if code := Run(context.Background(), argv, &out, &diag, terminal); code != 2 || out.Len() != 0 || strings.HasPrefix(diag.String(), "{") == terminal {
							t.Fatalf("%v terminal=%v: %d %s %s", argv, terminal, code, &out, &diag)
						}
					}
				}
			}
		}
		for _, args := range [][]string{
			{"config", "--output=json", "show"}, {"config", "--output=table", "path", "get"},
			{"config", "path", "--output=json", "get"}, {"config", "path", "--output=table", "get", "--help"},
			{"config", "--config-dir", pool, "--output=json", "show"},
			{"config", "path", "--config-dir", pool, "--output=json", "get"},
		} {
			var out, diag bytes.Buffer
			if code := Run(context.Background(), args, &out, &diag, terminal); code != 2 || out.Len() != 0 || strings.HasPrefix(diag.String(), "{") == terminal {
				t.Fatalf("%v: %d %s %s", args, code, &out, &diag)
			}
		}
	}
	after := snapshot()
	if len(before) != len(after) {
		t.Fatal("unsupported output created or removed entries")
	}
	for path, original := range before {
		current, ok := after[path]
		if !ok || current.contents != original.contents {
			t.Fatalf("unsupported output changed %s", path)
		}
		if original.info != nil && (!os.SameFile(original.info, current.info) || !original.info.ModTime().Equal(current.info.ModTime())) {
			t.Fatalf("unsupported output rewrote %s", path)
		}
	}
}

func TestPlainConfigConfirmationsOnTerminalsAndPipes(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(fmt.Sprint(terminal), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			pool, target := t.TempDir(), t.TempDir()
			t.Setenv("PORTBOOK_CONFIG_DIR", pool)
			invoke := func(args []string, want string) {
				t.Helper()
				var out, diag bytes.Buffer
				if code := Run(context.Background(), args, &out, &diag, terminal); code != 0 || diag.Len() != 0 || out.String() != want {
					t.Fatalf("%v: %d %q %s; want %q", args, code, out.String(), &diag, want)
				}
			}
			for _, tc := range []struct {
				args []string
				want string
			}{
				{[]string{"config", "remove", "--all"}, "Removed 16384 ports. Total: 0 ports.\n"},
				{[]string{"config", "remove", "--all"}, "Removed 0 ports. Total: 0 ports.\n"},
				{[]string{"config", "add", "1"}, "Added 1 port. Total: 1 port.\n"},
				{[]string{"config", "add", "1"}, "Added 0 ports. Total: 1 port.\n"},
				{[]string{"config", "remove", "1"}, "Removed 1 port. Total: 0 ports.\n"},
				{[]string{"config", "remove", "1"}, "Removed 0 ports. Total: 0 ports.\n"},
				{[]string{"config", "reset"}, "Reset 16384 ports. Total: 16384 ports.\n"},
				{[]string{"config", "reset"}, "Reset 0 ports. Total: 16384 ports.\n"},
			} {
				invoke(tc.args, tc.want)
			}
			for _, mode := range []string{"environment", "flag", "saved"} {
				t.Setenv("PORTBOOK_CONFIG_DIR", pool)
				source, effective := "environment", pool
				prefix := []string{}
				if mode == "flag" {
					source, effective = "flag", t.TempDir()
					prefix = []string{"--config-dir", effective}
				}
				if mode == "saved" {
					if err := os.Unsetenv("PORTBOOK_CONFIG_DIR"); err != nil {
						t.Fatal(err)
					}
					source, effective = "saved", target
				}
				details := fmt.Sprintf("Effective directory: %s\nConfig file: %s\nSource: %s\n", effective, filepath.Join(effective, "config.json"), source)
				for i := 0; i < 2; i++ {
					invoke(append(append([]string{}, prefix...), "config", "path", "set", target), "Saved directory: "+target+"\n"+details)
				}
				if mode == "saved" {
					effective, err := configLocationForTest()
					if err != nil {
						t.Fatal(err)
					}
					details = fmt.Sprintf("Effective directory: %s\nConfig file: %s\nSource: default\n", effective, filepath.Join(effective, "config.json"))
				}
				for i := 0; i < 2; i++ {
					invoke(append(append([]string{}, prefix...), "config", "path", "reset"), "Saved directory: (none; OS default)\n"+details)
				}
			}
		})
	}
}

func TestStructuredLeafDefaultsAndOverrides(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	for _, command := range [][]string{{"list"}, {"cleanup", "--dry-run"}, {"config", "show"}, {"config", "path", "get"}} {
		for _, terminal := range []bool{false, true} {
			for _, format := range []string{"", "json", "table"} {
				args := append([]string{}, command...)
				if format != "" {
					args = append(args, "--output", format)
				}
				var out, diag bytes.Buffer
				wantJSON := format == "json" || format == "" && !terminal
				if code := Run(context.Background(), args, &out, &diag, terminal); code != 0 || diag.Len() != 0 || json.Valid(out.Bytes()) != wantJSON {
					t.Fatalf("%v terminal=%v: %d %s %s", args, terminal, code, &out, &diag)
				}
			}
		}
	}
}
