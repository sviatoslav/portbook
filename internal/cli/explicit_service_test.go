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

	"github.com/sviatoslav/portbook/internal/booking"
	"github.com/sviatoslav/portbook/internal/state"
)

func TestExplicitServiceRequiredBeforeStateAccess(t *testing.T) {
	home, xdg, cfg := filepath.Join(t.TempDir(), "home"), filepath.Join(t.TempDir(), "xdg"), filepath.Join(t.TempDir(), "config")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("PORTBOOK_CONFIG_DIR", cfg)
	owner := t.TempDir()
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Dir, booking.File)
	for _, existing := range []bool{false, true} {
		var before []byte
		var info os.FileInfo
		if existing {
			// Historical literal default bookings remain addressable, never implicit.
			canonical, err := booking.Directory(owner)
			if err != nil {
				t.Fatal(err)
			}
			before = []byte(fmt.Sprintf(`{"version":1,"bookings":{%q:{"default":54321}}}`, canonical))
			if err := os.MkdirAll(c.Dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			info, err = os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, terminal := range []bool{false, true} {
			for _, command := range []string{"get", "reserve", "release"} {
				for _, args := range [][]string{
					{command}, {command, "--directory", owner},
					{"--config-dir", cfg, command, "--directory", owner},
					{command, "--directory", owner, "--config-dir", cfg, "--"},
				} {
					var out, diag bytes.Buffer
					if code := Run(context.Background(), args, &out, &diag, terminal); code != 2 || out.Len() != 0 || !strings.Contains(diag.String(), "requires exactly one service") {
						t.Fatal(args, code, out.String(), diag.String())
					}
					if strings.HasPrefix(diag.String(), "{") {
						var doc struct {
							Error diagnostic `json:"error"`
						}
						if err := json.Unmarshal(diag.Bytes(), &doc); err != nil || doc.Error.Code != "usage_error" {
							t.Fatal(diag.String(), err)
						}
					} else if !strings.HasPrefix(diag.String(), "Error: ") {
						t.Fatal(diag.String())
					}
				}
			}
		}
		if !existing {
			for _, path := range []string{home, xdg, cfg} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("omission created state", path, err)
				}
			}
		} else {
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("omission changed registry", err)
			}
			current, err := os.Stat(path)
			if err != nil || !os.SameFile(info, current) || !info.ModTime().Equal(current.ModTime()) {
				t.Fatal("omission replaced registry", err)
			}
			if _, err := os.Stat(filepath.Join(c.Dir, "state.lock")); !os.IsNotExist(err) {
				t.Fatal("omission created lock", err)
			}
			var out, diag bytes.Buffer
			if code := Run(context.Background(), []string{"get", "default", "--directory", owner}, &out, &diag, false); code != 0 || out.String() != "54321\n" || diag.Len() != 0 {
				t.Fatal("historical default inaccessible", code, out.String(), diag.String())
			}
		}
	}
}

func TestExplicitServiceHelpValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	owner := t.TempDir()
	for _, terminal := range []bool{false, true} {
		for _, command := range []string{"get", "reserve", "release"} {
			for _, args := range [][]string{{command, "--help"}, {command, "--directory", owner, "--help"}, {command, "api", "--help"}} {
				var out, diag bytes.Buffer
				if code := Run(context.Background(), args, &out, &diag, terminal); code != 0 || diag.Len() != 0 || !strings.Contains(out.String(), command+" SERVICE") || strings.Contains(out.String(), "[SERVICE]") {
					t.Fatal(args, code, out.String(), diag.String())
				}
			}
			for _, args := range [][]string{{command, "--directory=relative", "--help"}, {command, "", "--help"}, {command, "a", "b", "--help"}, {command, "api", "--all", "--help"}, {command, "--output=yaml", "--help"}} {
				var out, diag bytes.Buffer
				if code := Run(context.Background(), args, &out, &diag, terminal); code != 2 || out.Len() != 0 {
					t.Fatal(args, code, out.String(), diag.String())
				}
			}
		}
	}
}
