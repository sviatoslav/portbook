package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sviatoslav/portbook/internal/booking"
	"github.com/sviatoslav/portbook/internal/state"
)

func TestCleanupFormatsOutputFailuresAndEscaping(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(parent, "gone")
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(c.Dir, 0700)
	data, _ := json.Marshal(map[string]any{"version": 1, "bookings": booking.Registry{gone: {"idle": 41001}}})
	path := filepath.Join(c.Dir, booking.File)
	// Existing owner keeps the real probe out of this output-only test.
	os.Mkdir(gone, 0700)
	os.WriteFile(path, data, 0600)
	for _, terminal := range []bool{true, false} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), []string{"cleanup", "--dry-run"}, &out, &diag, terminal); code != 0 || diag.Len() != 0 || strings.HasPrefix(out.String(), "{") == terminal {
			t.Fatal(code, out.String(), diag.String())
		}
	}
	var diag bytes.Buffer
	if code := Run(context.Background(), []string{"cleanup"}, failedWriter{}, &diag, false); code != 1 || !strings.Contains(diag.String(), `"code":"runtime_error"`) {
		t.Fatal(code, diag.String())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("output error changed retained booking")
	}
	var out bytes.Buffer
	r := booking.CleanupReport{Rows: []booking.CleanupRow{{Directory: "/x\nDIRECTORY", Service: "a\tb", Port: 1, Action: "retained", Reason: "owner_exists"}}}
	if err := renderCleanup(&out, "table", r); err != nil || !strings.Contains(out.String(), `"/x\nDIRECTORY"`) || !strings.Contains(out.String(), `"a\tb"`) {
		t.Fatal(out.String(), err)
	}
}

func TestCleanupDoesNotResolveIrrelevantConfigSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", "relative-invalid-config")
	for _, args := range [][]string{{"cleanup", "--dry-run"}, {"cleanup", "--help"}} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), args, &out, &diag, false); code != 0 || diag.Len() != 0 {
			t.Fatal(args, code, out.String(), diag.String())
		}
	}
	for _, args := range [][]string{{"cleanup", "--config-dir=relative", "--help"}, {"cleanup", "--dry-run", "--dry-run", "--output=table"}, {"cleanup", "--dry-run=invalid", "--output=json"}, {"cleanup", "--dry-run", "--", "--output=table"}} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), args, &out, &diag, false); code != 2 || out.Len() != 0 {
			t.Fatal(args, code, out.String(), diag.String())
		}
	}
}

func TestCleanupTableEscapesTerminalControlsAndAmbiguousIdentities(t *testing.T) {
	identity := "quotes\" backslash\\ tab\t newline\n escape\x1b[31m c1\u009b31m separators\u2028\u2029 bidi\u202e\u2066"
	r := booking.CleanupReport{Command: "cleanup", DryRun: true, Rows: []booking.CleanupRow{{Directory: "/" + identity, Service: identity, Port: 1, Action: "would-remove", Reason: "owner_missing"}}, WouldRemove: 1}
	var out bytes.Buffer
	if err := renderCleanup(&out, "table", r); err != nil {
		t.Fatal(err)
	}
	for _, control := range []string{"\x1b", "\u009b", "\u2028", "\u2029", "\u202e", "\u2066"} {
		if strings.Contains(out.String(), control) {
			t.Fatalf("unescaped terminal control %q in %q", control, out.String())
		}
	}
	if strings.Count(out.String(), "\n") != 3 || !strings.Contains(out.String(), `quotes\" backslash\\`) {
		t.Fatal("ambiguous or multiline identity", out.String())
	}
	out.Reset()
	if err := renderCleanup(&out, "json", r); err != nil {
		t.Fatal(err)
	}
	var decoded booking.CleanupReport
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || decoded.Rows[0].Service != identity || decoded.Rows[0].Directory != "/"+identity {
		t.Fatal("JSON lost identity", err, out.String())
	}
}

func TestCleanupSuccessfulMutationSurvivesOutputFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// This test verifies save/report ordering, not OS availability. A port chosen
	// by Listen(":0") can become occupied after closing it, legitimately making
	// cleanup retain the booking. Use the store's existing probe dependency.
	const port = 54321

	data, err := json.Marshal(map[string]any{"version": 1, "bookings": booking.Registry{filepath.Join(parent, "gone"): {"default": port}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Dir, booking.File)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	probes := 0
	store := booking.Store{Coordinator: c, Probe: func(context.Context, int) (bool, error) { probes++; return true, nil }}
	err = executeCleanup(context.Background(), store, false, failedWriter{}, "json")
	var classified *commandError
	if !errors.As(err, &classified) || classified.code != "runtime_error" || probes != 1 {
		t.Fatal(err, probes)
	}
	var diagnostics bytes.Buffer

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Bookings booking.Registry `json:"bookings"`
	}
	if err := json.Unmarshal(after, &saved); err != nil || len(saved.Bookings) != 0 {
		t.Fatal("output failure undid committed cleanup", string(after), err)
	}
	var out bytes.Buffer
	diagnostics.Reset()
	if code := Run(context.Background(), []string{"cleanup"}, &out, &diagnostics, false); code != 0 || diagnostics.Len() != 0 {
		t.Fatal(code, out.String(), diagnostics.String())
	}
	var report booking.CleanupReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Removed != 0 || len(report.Rows) != 0 {
		t.Fatal("retry did not observe committed result", report, err)
	}
}
