package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sviatoslav/portbook/internal/state"
)

func TestInstallSkillsArgumentsAndNoState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "missing-home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "missing-xdg"))
	t.Setenv("PORTBOOK_CONFIG_DIR", "invalid-unused-selection")
	for _, tc := range []struct {
		args  []string
		agent string
	}{
		{[]string{"install-skills"}, "codex"},
		{[]string{"install-skills", "--agent", "claude-code"}, "claude-code"},
		{[]string{"install-skills", "--agent=codex", "--config-dir", filepath.Join(root, "unused")}, "codex"},
	} {
		var out, diag bytes.Buffer
		called := false
		s := skillInstaller{
			lookup: func(name string) (string, error) {
				if name != "npx" {
					t.Fatalf("lookup %q", name)
				}
				return "/fake path/npx", nil
			},
			timeout: time.Minute,
			run: func(ctx context.Context, name string, args ...string) error {
				called = true
				want := []string{"--yes", "skills", "add", "sviatoslav/portbook-skills", "--skill", "portbook", "--global", "--agent", tc.agent}
				if name != "/fake path/npx" || !reflect.DeepEqual(args, want) {
					t.Fatalf("%q %q", name, args)
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("no deadline")
				}
				out.WriteString("installer output\n")
				return nil
			},
		}
		if code := run(context.Background(), tc.args, &out, &diag, false, s); code != 0 || !called || out.String() != "installer output\n" || diag.Len() != 0 {
			t.Fatalf("code/output: %d %q %q", code, &out, &diag)
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("created state: %v %v", entries, err)
	}
}

func TestInstallSkillsValidationAndHelp(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("PORTBOOK_CONFIG_DIR", "invalid-unused-selection")
	for _, args := range [][]string{
		{"install-skills", "extra"}, {"install-skills", "--agent", "other"},
		{"install-skills", "--agent=codex", "--agent=claude-code"},
		{"install-skills", "--yes"}, {"install-skills", "--output=yaml"},
		{"install-skills", "--config-dir=relative"}, {"install-skills", "--unknown"},
		{"install-skills", "--help", "--agent=other"}, {"install-skills", "--help", "extra"},
		{"install-skills", "--help", "--config-dir=relative"},
	} {
		var out, diag bytes.Buffer
		if code := run(context.Background(), args, &out, &diag, false, skillInstaller{}); code != 2 || out.Len() != 0 || !strings.Contains(diag.String(), `"code":"usage_error"`) {
			t.Fatalf("%v: %d %q %q", args, code, &out, &diag)
		}
	}
	var out, diag bytes.Buffer
	if code := run(context.Background(), []string{"install-skills", "--help"}, &out, &diag, false, skillInstaller{}); code != 0 || !strings.Contains(out.String(), "claude-code") || diag.Len() != 0 {
		t.Fatalf("help: %d %q %q", code, &out, &diag)
	}
	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("help/validation wrote state", entries)
	}
}

func TestInstallSkillsIgnoresCorruptPortbookFiles(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "xdg"))
	t.Setenv("PORTBOOK_CONFIG_DIR", filepath.Join(root, "pool"))
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"bookings.json", "location.json", "config.json", "state.lock"} {
		if err := os.WriteFile(filepath.Join(c.Dir, file), []byte("corrupt evidence"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s := skillInstaller{lookup: func(string) (string, error) { return "/fake/npx", nil }, timeout: time.Minute, run: func(context.Context, string, ...string) error { return nil }}
	for _, args := range [][]string{{"install-skills"}, {"install-skills", "--help"}} {
		var out, diag bytes.Buffer
		if code := run(context.Background(), args, &out, &diag, false, s); code != 0 {
			t.Fatalf("%v: %d %s", args, code, &diag)
		}
	}
	entries, _ := os.ReadDir(c.Dir)
	if len(entries) != 4 {
		t.Fatal("changed state directory", entries)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(c.Dir, entry.Name()))
		if err != nil || string(data) != "corrupt evidence" {
			t.Fatalf("changed %s: %q %v", entry.Name(), data, err)
		}
	}
}

func TestInstallSkillsFailuresAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		lookupErr error
		runErr    error
		timeout   time.Duration
		cancel    bool
		want      string
	}{
		{"missing npx", errors.New("missing"), nil, time.Minute, false, "install Node.js"},
		{"external exit", nil, errors.New("exit status 7"), time.Minute, false, "exit status 7"},
		{"timeout", nil, nil, time.Millisecond, false, "context deadline exceeded"},
		{"cancel", nil, nil, time.Minute, true, "context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			s := skillInstaller{
				lookup:  func(string) (string, error) { return "/fake/npx", tc.lookupErr },
				timeout: tc.timeout,
				run: func(ctx context.Context, _ string, _ ...string) error {
					if tc.runErr != nil {
						return tc.runErr
					}
					<-ctx.Done()
					return ctx.Err()
				},
			}
			var out, diag bytes.Buffer
			if code := run(ctx, []string{"install-skills"}, &out, &diag, false, s); code != 1 || out.Len() != 0 || !strings.Contains(diag.String(), `"code":"runtime_error"`) || !strings.Contains(diag.String(), tc.want) {
				t.Fatalf("failure: %d %q %q", code, &out, &diag)
			}
		})
	}
}

func TestDefaultSkillInstallerProcessStreamsAndTimeout(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "npx")
	// A real subprocess tests stream wiring, argv boundaries, and cancellation.
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nread line\nprintf '%s\\n' \"$line\" \"$@\"\nprintf 'external stderr\\n' >&2\nexit 7\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var out, diag bytes.Buffer
	s := defaultSkillInstaller(strings.NewReader("input\n"), &out, &diag)
	err := s.install(context.Background(), "codex")
	if err == nil || !strings.Contains(err.Error(), "exit status 7") || !strings.HasPrefix(out.String(), "input\n--yes\nskills\nadd\nsviatoslav/portbook-skills\n") || diag.String() != "external stderr\n" {
		t.Fatalf("streams/failure: %v %q %q", err, &out, &diag)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nwhile :; do :; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s.timeout = 30 * time.Millisecond
	start := time.Now()
	if err := s.install(context.Background(), "codex"); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 3*time.Second {
		t.Fatalf("timeout: %v", err)
	}
}

func TestDefaultSkillInstallerCancelsDescendants(t *testing.T) {
	dir := t.TempDir()
	// The child would write after the cancellation if only npx were killed.
	script := "#!/bin/sh\n/bin/sh -c 'echo ready > \"$1\"; /bin/sleep 0.5; echo survived > \"$2\"' child \"${0%/*}/ready\" \"${0%/*}/survived\" &\nwait\n"
	if err := os.WriteFile(filepath.Join(dir, "npx"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	var output bytes.Buffer
	s := defaultSkillInstaller(nil, &output, &output)
	s.timeout = 5 * time.Second
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.install(ctx, "codex") }()
	for {
		if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("child never started: %v %s", err, &output)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "survived")); !os.IsNotExist(err) {
		t.Fatal("descendant survived cancellation", err)
	}
}

func TestDefaultSkillInstallerStopsDescendantAfterLauncherExit(t *testing.T) {
	dir := t.TempDir()
	script := "#!/bin/sh\n/bin/sh -c '/bin/sleep 2; echo survived > \"$1\"' child \"${0%/*}/survived\" &\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "npx"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	// Files avoid os/exec's pipe-draining wait: the launcher has already exited
	// successfully while its installer descendant is still running.
	output, err := os.Create(filepath.Join(dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	installer := defaultSkillInstaller(nil, output, output)
	if err := installer.install(context.Background(), "codex"); err == nil || !strings.Contains(err.Error(), "exited before its child processes") {
		t.Fatal(err)
	}
	time.Sleep(2200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "survived")); !os.IsNotExist(err) {
		t.Fatal("installer descendant continued after launcher exited", err)
	}
}

func TestInstallSkillsDiagnosticValueBoundaries(t *testing.T) {
	for _, tc := range []struct {
		args []string
		json bool
	}{
		{[]string{"install-skills", "--agent", "--output=table"}, true},
		{[]string{"install-skills", "--agent", "--output=json", "--output=table"}, true},
		{[]string{"install-skills", "--bogus", "--agent", "--output=table", "--output=json"}, true},
		{[]string{"install-skills", "--output=table", "--agent=unknown"}, true},
	} {
		var out, diag bytes.Buffer
		if code := run(context.Background(), tc.args, &out, &diag, false, skillInstaller{}); code != 2 || out.Len() != 0 || strings.HasPrefix(diag.String(), "{") != tc.json {
			t.Fatal(tc.args, code, out.String(), diag.String())
		}
	}
}
