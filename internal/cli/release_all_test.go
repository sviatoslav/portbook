package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sviatoslav/portbook/internal/booking"
	"github.com/sviatoslav/portbook/internal/state"
)

func TestReleaseAllValidationAndHelp(t *testing.T) {
	home, xdg := filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "xdg")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("PORTBOOK_CONFIG_DIR", "relative-unused")
	owner := t.TempDir()
	for _, terminal := range []bool{false, true} {
		for _, args := range [][]string{
			{"release", "api", "--all"}, {"release", "--all", "api"}, {"release", "--all", "--", "api"},
			{"release", "api", "--all=false"}, {"release", "--all=false"},
			{"release", "--all", "--all"}, {"release", "--all", "--directory=relative"},
			{"release", "--all", "--directory", owner, "--directory", owner},
			{"release", "--all", "--config-dir=relative"}, {"release", "--all", "--output=yaml"},
			{"release", "--all", "--dry-run"}, {"release", "--all", "api", "--help"},
			{"release", "--all", "--directory=relative", "--help"},
		} {
			var out, diag bytes.Buffer
			if code := Run(context.Background(), args, &out, &diag, terminal); code != 2 || out.Len() != 0 || diag.Len() == 0 {
				t.Fatal(args, code, out.String(), diag.String())
			}
		}
		for _, args := range [][]string{{"release", "--all", "--help"}, {"release", "--all", "--directory", owner, "--help"}} {
			var out, diag bytes.Buffer
			if code := Run(context.Background(), args, &out, &diag, terminal); code != 0 || diag.Len() != 0 || !strings.Contains(out.String(), "--all") {
				t.Fatal(args, code, out.String(), diag.String())
			}
		}
	}
	for _, path := range []string{home, xdg} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("validation/help wrote state", path, err)
		}
	}
	var out, diag bytes.Buffer
	if code := Run(context.Background(), []string{"release", "--all", "--directory", owner}, &out, &diag, false); code != 1 || out.Len() != 0 || !strings.Contains(diag.String(), `"code":"runtime_error"`) || !strings.Contains(diag.String(), "reservation not found") {
		t.Fatal(code, out.String(), diag.String())
	}
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, booking.File)); !os.IsNotExist(err) {
		t.Fatal("missing release created registry", err)
	}
}

func TestReleaseAllRenderingAndCommittedOutputFailure(t *testing.T) {
	res := booking.ReleaseAllResult{Directory: "/projects/owner\n\x1b[31m", Ports: []int{45000, 45002}, Released: 2}
	var out bytes.Buffer
	if err := renderReleaseAll(&out, res); err != nil || out.String() != fmt.Sprintf("Released 2 bookings in %q: 45000, 45002.\n", res.Directory) {
		t.Fatal(out.String(), err)
	}
	if err := renderReleaseAll(releaseFailWriter{}, res); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", "relative-unused")
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := booking.Directory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Dir, booking.File)
	data := []byte(fmt.Sprintf(`{"version":1,"bookings":{%q:{"default":45000,"api":45002}}}`, owner))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var diag bytes.Buffer
	if code := Run(context.Background(), []string{"release", "--all", "--directory", owner}, releaseFailWriter{}, &diag, false); code != 1 || !strings.Contains(diag.String(), `"code":"runtime_error"`) {
		t.Fatal(code, diag.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var registry struct{ Bookings booking.Registry }
	if err := json.Unmarshal(after, &registry); err != nil || len(registry.Bookings) != 0 {
		t.Fatal("output error undid bulk deletion", string(after), err)
	}
}

func TestReleaseFlagLookingServiceIsNotBulk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", "relative-unused")
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := booking.Directory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Dir, booking.File)
	data := []byte(fmt.Sprintf(`{"version":1,"bookings":{%q:{"--all":45000,"default":45001}}}`, owner))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var out, diag bytes.Buffer
	if code := Run(context.Background(), []string{"release", "--directory", owner, "--", "--all"}, &out, &diag, false); code != 0 || diag.Len() != 0 {
		t.Fatal(code, out.String(), diag.String())
	}
	if out.String() != fmt.Sprintf("Released port 45000 for %q in %q.\n", "--all", owner) {
		t.Fatal(out.String())
	}
	var registry struct{ Bookings booking.Registry }
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &registry); err != nil || len(registry.Bookings[owner]) != 1 || registry.Bookings[owner]["default"] != 45001 {
		t.Fatal(string(after), err)
	}
}
