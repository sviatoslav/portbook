package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sviatoslav/portbook/internal/booking"
)

func cleanupReport(t *testing.T, s string) booking.CleanupReport {
	t.Helper()
	var r booking.CleanupReport
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		t.Fatal(err, s)
	}
	return r
}
func TestCleanupCLIReadOnlyValidationAndCorruption(t *testing.T) {
	home := t.TempDir()
	root := registryRoot(t, home)
	r := cleanupReport(t, locationRun(t, home, 0, "cleanup", "--dry-run"))
	if !r.DryRun || r.Rows == nil || len(r.Rows) != 0 {
		t.Fatal(r)
	}
	for _, args := range [][]string{{"cleanup", "extra"}, {"cleanup", "--", "--help"}, {"cleanup", "--dry-run", "--dry-run"}, {"cleanup", "--directory", home}, {"cleanup", "--all"}, {"cleanup", "extra", "--help"}, {"cleanup", "--dry-run=invalid"}} {
		if got := locationRun(t, home, 2, args...); !strings.Contains(got, `"code":"usage_error"`) {
			t.Fatal(args, got)
		}
	}
	if got := locationRun(t, home, 0, "cleanup", "--help"); !strings.Contains(got, "--dry-run") {
		t.Fatal(got)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("dry run/help created state", err)
	}
	os.MkdirAll(root, 0700)
	path := filepath.Join(root, booking.File)
	for _, bad := range []string{"broken", `{"version":2,"bookings":{}}`, `{"version":1,"bookings":{"/x":{"x":1,"x":2}}}`} {
		os.WriteFile(path, []byte(bad), 0600)
		for _, args := range [][]string{{"cleanup", "--dry-run"}, {"cleanup"}, {"cleanup"}} {
			if got := locationRun(t, home, 1, args...); !strings.Contains(got, `"code":"storage_error"`) {
				t.Fatal(got)
			}
			data, _ := os.ReadFile(path)
			if string(data) != bad {
				t.Fatal("corrupt evidence changed")
			}
		}
	}
}

func TestCleanupCLIRealSocketsAndConfigIndependence(t *testing.T) {
	home := t.TempDir()
	root := registryRoot(t, home)
	p, held := candidateSockets(t, 3)
	gone, _ := booking.Directory(t.TempDir())
	existing, _ := booking.Directory(t.TempDir())
	os.Remove(gone)
	doc := struct {
		Version  int              `json:"version"`
		Bookings booking.Registry `json:"bookings"`
	}{1, booking.Registry{gone: {"free": p[0], "busy": p[1]}, existing: {"idle": p[2]}}}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(root, 0700)
	path := filepath.Join(root, booking.File)
	os.WriteFile(path, data, 0600)
	held[p[0]].Close()
	held[p[2]].Close()
	// Neither the pool nor saved pointer is needed, including when corrupt.
	os.WriteFile(filepath.Join(root, "location.json"), []byte("broken pointer"), 0600)
	cfg := t.TempDir()
	os.WriteFile(filepath.Join(cfg, "config.json"), []byte("broken pool"), 0600)
	args := []string{"--config-dir", cfg, "cleanup", "--dry-run"}
	r := cleanupReport(t, locationRun(t, home, 0, args...))
	if r.WouldRemove != 1 || r.Retained != 2 || r.Removed != 0 {
		t.Fatal(r)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(data, after) {
		t.Fatal("dry run rewrote registry")
	}
	if _, err := os.Stat(filepath.Join(root, "state.lock")); !os.IsNotExist(err) {
		t.Fatal("dry run created lock")
	}
	r = cleanupReport(t, locationRun(t, home, 0, "cleanup"))
	if r.Removed != 1 || r.Retained != 2 {
		t.Fatal(r)
	}
	before, _ := os.ReadFile(path)
	if got := locationRun(t, home, 0, "cleanup", "--output=table"); !strings.Contains(got, "Retained: 2.") {
		t.Fatal(got)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("no-op rewrote registry")
	}
	pointer, _ := os.ReadFile(filepath.Join(root, "location.json"))
	if string(pointer) != "broken pointer" {
		t.Fatal("changed pointer")
	}
	for _, name := range []string{booking.File, "state.lock"} {
		if _, err := os.Stat(filepath.Join(cfg, name)); !os.IsNotExist(err) {
			t.Fatal("state moved to override")
		}
	}
}

func TestCleanupReserveIndependentProcessContention(t *testing.T) {
	cfg := t.TempDir()
	p, held := candidateSockets(t, 3)
	configCandidates(t, cfg, p)
	for _, l := range held {
		l.Close()
	}
	gone := t.TempDir()
	run(t, cfg, 0, "reserve", "--directory", gone)
	os.Remove(gone)
	live := t.TempDir()
	const workers = 12
	cmds := make([]*exec.Cmd, workers)
	outs := make([]bytes.Buffer, workers)
	diags := make([]bytes.Buffer, workers)
	for i := range cmds {
		args := []string{"cleanup"}
		if i%2 == 0 {
			args = []string{"reserve", "live", "--directory", live}
		}
		cmds[i] = command(cfg, args...)
		cmds[i].Stdout = &outs[i]
		cmds[i].Stderr = &diags[i]
		if err := cmds[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	var port string
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil || diags[i].Len() != 0 {
			t.Fatal(err, diags[i].String())
		}
		if i%2 == 0 {
			if port != "" && port != outs[i].String() {
				t.Fatal("unstable reservation")
			}
			port = outs[i].String()
		}
	}
	if got := run(t, cfg, 0, "reserve", "live", "--directory", live); got != port {
		t.Fatal("lost reservation", got, port)
	}
	root := registryRoot(t, cfg)
	data, err := os.ReadFile(filepath.Join(root, booking.File))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Bookings booking.Registry }
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	physical, _ := booking.Directory(live)
	if len(doc.Bookings) != 1 || len(doc.Bookings[physical]) != 1 {
		t.Fatal("lost or retained abandoned bookings", string(data))
	}
}
