package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/state"
)

func resetInvoke(t *testing.T, code int, args ...string) string {
	t.Helper()
	var out, diag bytes.Buffer
	if got := Run(context.Background(), args, &out, &diag, false); got != code {
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

func TestConfigResetSelectionAndPreservedState(t *testing.T) {
	for _, mode := range []string{"default", "saved", "environment", "flag"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("PORTBOOK_CONFIG_DIR", "")
			if err := os.Unsetenv("PORTBOOK_CONFIG_DIR"); err != nil {
				t.Fatal(err)
			}
			c, err := state.Default()
			if err != nil {
				t.Fatal(err)
			}
			saved, env, flag := t.TempDir(), t.TempDir(), t.TempDir()
			selected := c.Dir
			if mode != "default" {
				if _, err := (config.Location{Coordinator: c}).Change(context.Background(), saved); err != nil {
					t.Fatal(err)
				}
				selected = saved
			}
			if mode == "environment" || mode == "flag" {
				t.Setenv("PORTBOOK_CONFIG_DIR", env)
				selected = env
			}
			args := []string{"config", "reset"}
			if mode == "flag" {
				args = append(args, "--config-dir", flag)
				selected = flag
			}
			before := map[string][]byte{}
			for _, dir := range []string{c.Dir, saved, env, flag} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				p := filepath.Join(dir, "config.json")
				data := []byte(`{"version":1,"ranges":[{"start":1,"end":2}]}`)
				if err := os.WriteFile(p, data, 0600); err != nil {
					t.Fatal(err)
				}
				if dir != selected {
					before[p] = data
				}
			}
			for name, data := range map[string][]byte{
				"bookings.json": []byte(`{"version":1,"bookings":{"/recorded/owner":{"api":1,"web":50000}}}`),
				"unrelated.txt": []byte("keep"),
			} {
				p := filepath.Join(c.Dir, name)
				if err := os.WriteFile(p, data, 0600); err != nil {
					t.Fatal(err)
				}
				before[p] = data
			}
			if mode != "default" {
				p := filepath.Join(c.Dir, "location.json")
				before[p], err = os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
			}
			got := resetInvoke(t, 0, args...)
			if got != "Reset 16386 ports. Total: 16384 ports.\n" {
				t.Fatal(got)
			}
			pool, err := (config.Store{Coordinator: c, Location: &config.Location{Flag: selected, FlagSet: true}}).Read()
			if err != nil || pool.Count() != 16384 || !pool[49152] || !pool[65535] || pool[1] {
				t.Fatal(pool.Ranges(), err)
			}
			for p, data := range before {
				got, err := os.ReadFile(p)
				if err != nil || !bytes.Equal(got, data) {
					t.Fatalf("reset changed %s: %v", p, err)
				}
			}
			if mode == "default" {
				if _, err := os.Lstat(filepath.Join(c.Dir, "location.json")); !os.IsNotExist(err) {
					t.Fatal("reset created pointer", err)
				}
			}
			got = resetInvoke(t, 0, args...)
			if got != "Reset 0 ports. Total: 16384 ports.\n" {
				t.Fatal(got)
			}
		})
	}
}

func TestConfigResetHelpAndValidation(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "xdg"))
	dir := filepath.Join(base, "config")
	t.Setenv("PORTBOOK_CONFIG_DIR", dir)
	for _, args := range [][]string{{"config", "--help"}, {"config", "reset", "--help"}, {"--help", "config", "reset"}} {
		if got := resetInvoke(t, 0, args...); !strings.Contains(got, "reset") {
			t.Fatal(got)
		}
	}
	for _, args := range [][]string{
		{"config", "reset", "1"}, {"config", "reset", "extra", "--help"}, {"config", "reset", "--all"},
		{"config", "reset", "--all", "--help"}, {"config", "reset", "--unknown"}, {"config", "reset", "--", "--help"},
		{"config", "reset", "--config-dir=relative", "--help"}, {"config", "reset", "--output=yaml"},
		{"config", "reset", "--config-dir", dir, "--config-dir", dir}, {"config", "reset", "--directory", dir},
	} {
		if got := resetInvoke(t, 2, args...); !strings.Contains(got, `"code":"usage_error"`) {
			t.Fatal(got)
		}
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 0 {
		t.Fatal("help/invalid request created state", entries, err)
	}
}

func TestConfigResetDoesNotRepairPointer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", "")
	if err := os.Unsetenv("PORTBOOK_CONFIG_DIR"); err != nil {
		t.Fatal(err)
	}
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	pointer := filepath.Join(c.Dir, "location.json")
	before := []byte("broken pointer")
	if err := os.WriteFile(pointer, before, 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "reset"}, {"config", "reset", "--help"}} {
		if got := resetInvoke(t, 1, args...); !strings.Contains(got, `"code":"storage_error"`) {
			t.Fatal(got)
		}
	}
	after, err := os.ReadFile(pointer)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("reset repaired pointer", err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "config.json")); !os.IsNotExist(err) {
		t.Fatal("reset created config", err)
	}
}

func TestConfigResetCommittedBeforeOutputFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	t.Setenv("PORTBOOK_CONFIG_DIR", dir)
	resetInvoke(t, 0, "config", "remove", "--all")
	var diag bytes.Buffer
	if code := Run(context.Background(), []string{"config", "reset"}, failedWriter{}, &diag, false); code != 1 || !strings.Contains(diag.String(), `"code":"runtime_error"`) {
		t.Fatal(code, diag.String())
	}
	if got := resetInvoke(t, 0, "config", "show"); !strings.Contains(got, `"total":16384`) {
		t.Fatal("output failure undid reset", got)
	}
}
