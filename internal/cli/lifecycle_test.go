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

func TestLifecycleOutputContracts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	owner, err := booking.Directory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Dir, booking.File)
	data := []byte(fmt.Sprintf(`{"version":1,"bookings":{%q:{"default":54321}}}`, owner))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, terminal := range []bool{false, true} {
		for _, format := range []string{"", "json", "table"} {
			for _, command := range []string{"get", "reserve", "release"} {
				service, message := "default", "reservation already exists"
				if command != "reserve" {
					service, message = "absent", "reservation not found"
				}
				args := []string{command, service, "--directory", owner}
				if format != "" {
					args = append([]string{"--output", format}, args...)
				}
				var out, diag bytes.Buffer
				if code := Run(context.Background(), args, &out, &diag, terminal); code != 1 || out.Len() != 0 || !strings.Contains(diag.String(), message) {
					t.Fatal(args, code, out.String(), diag.String())
				}
				if format == "json" || (format == "" && !terminal) {
					var doc struct {
						Error diagnostic `json:"error"`
					}
					if err := json.Unmarshal(diag.Bytes(), &doc); err != nil || doc.Error.Code != "runtime_error" {
						t.Fatal(diag.String(), err)
					}
				} else if !strings.HasPrefix(diag.String(), "Error: ") {
					t.Fatal(diag.String())
				}
			}
		}
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(data, after) {
		t.Fatal("failure changed state", err)
	}
}

func TestGetDiagnosticFlagOrdering(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tc := range []struct {
		args []string
		json bool
	}{
		{[]string{"get", "--bogus", "--output=table"}, false},
		{[]string{"get", "--bogus", "--output=json"}, true},
		{[]string{"get", "--directory", "--output=table", "--output=json"}, true},
		{[]string{"get", "--directory", "--output=json", "--output=table"}, false},
		{[]string{"get", "--output=json", "--output=table"}, true},
	} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), tc.args, &out, &diag, false); code != 2 || out.Len() != 0 || strings.HasPrefix(diag.String(), "{") != tc.json {
			t.Fatal(tc.args, code, out.String(), diag.String())
		}
	}
}

func TestReservePlainSuccessFormats(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", filepath.Join(t.TempDir(), "absent"))
	owner := t.TempDir()
	for _, terminal := range []bool{false, true} {
		for _, format := range []string{"", "json", "table"} {
			args := []string{"reserve", fmt.Sprint(terminal, "-", format), "--directory", owner}
			if format != "" {
				args = append(args, "--output", format)
			}
			var out, diag bytes.Buffer
			if code := Run(context.Background(), args, &out, &diag, terminal); code != 0 || diag.Len() != 0 {
				t.Fatal(args, code, diag.String())
			}
			var port int
			if _, err := fmt.Sscanf(out.String(), "%d\n", &port); err != nil || out.String() != fmt.Sprintf("%d\n", port) || port < 49152 || port > 65535 {
				t.Fatal(out.String(), err)
			}
		}
	}
}
