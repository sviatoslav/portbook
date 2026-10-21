package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofrs/flock"
	"github.com/sviatoslav/portbook/internal/booking"
)

func TestGetCLIReadOnlyLifecycleAndValidation(t *testing.T) {
	cfg, owner := t.TempDir(), t.TempDir()
	root := registryRoot(t, cfg)
	if got := run(t, cfg, 1, "get", "default", "--directory", owner); !strings.Contains(got, "reservation not found") || !strings.Contains(got, `"code":"runtime_error"`) {
		t.Fatal(got)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("missing get created state", err)
	}
	p, held := candidateSockets(t, 1)
	configCandidates(t, cfg, p)
	held[p[0]].Close()
	want := run(t, cfg, 0, "reserve", "default", "--directory", owner)
	path := filepath.Join(root, booking.File)
	before, _ := os.ReadFile(path)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(owner, alias); err != nil {
		t.Fatal(err)
	}
	// Get remains usable with a broken pool/pointer and the mutation lock held.
	os.WriteFile(filepath.Join(cfg, "config.json"), []byte("broken"), 0600)
	os.WriteFile(filepath.Join(root, "location.json"), []byte("broken"), 0600)
	lock := flock.New(filepath.Join(root, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"get", "default", "--directory", alias}, {"get", "default", "--directory", owner},
	} {
		if got := run(t, cfg, 0, args...); got != want {
			t.Fatal(args, got)
		}
	}
	cmd := command(cfg, "get", "default")
	cmd.Dir = owner
	cmd.Env = append(cmd.Env, "PORTBOOK_CONFIG_DIR=relative")
	if got := processResult(t, cmd, 0); got != want {
		t.Fatal(got)
	}
	if got := run(t, cfg, 0, "get", "--help"); !strings.Contains(got, "get SERVICE") {
		t.Fatal(got)
	}
	lock.Unlock()
	for _, args := range [][]string{
		{"get", "a", "b"}, {"get", ""}, {"get", "default", "--directory=relative"}, {"get", "--all"},
		{"get", "default", "--directory", owner, "--directory", owner}, {"get", "--output=yaml"},
		{"get", "--output=json", "--output=table"}, {"get", "a", "b", "--help"},
		{"get", "default", "--directory=relative", "--help"}, {"get", "--config-dir=relative"},
		{"get", "default", "--directory", filepath.Join(owner, "absent")},
	} {
		run(t, cfg, 2, args...)
	}
	for _, service := range []string{"--help", "--output=table", "api"} {
		if got := run(t, cfg, 1, "get", "--directory", owner, "--", service); !strings.Contains(got, "reservation not found") {
			t.Fatal(got)
		}
	}
	if got := run(t, cfg, 1, "reserve", "default", "--directory", alias); !strings.Contains(got, "reservation already exists") {
		t.Fatal(got)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("read or failure changed registry")
	}
	for _, corrupt := range []string{`{"version":99,"bookings":{}}`, `{"version":1,"version":1,"bookings":{}}`, `broken`} {
		os.WriteFile(path, []byte(corrupt), 0600)
		if got := run(t, cfg, 1, "get", "default", "--directory", owner); !strings.Contains(got, `"code":"storage_error"`) {
			t.Fatal(got)
		}
		after, _ := os.ReadFile(path)
		if string(after) != corrupt {
			t.Fatal("get changed corrupt registry")
		}
	}
}
