package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gofrs/flock"
	"github.com/sviatoslav/portbook/internal/booking"
)

func releaseReceipt(t *testing.T, text string) booking.ReleaseResult {
	t.Helper()
	var port int
	var service, directory string
	if n, err := fmt.Sscanf(text, "Released port %d for %q in %q.\n", &port, &service, &directory); err != nil || n != 3 || text != fmt.Sprintf("Released port %d for %q in %q.\n", port, service, directory) {
		t.Fatal(text, err)
	}
	return booking.ReleaseResult{Directory: directory, Service: service, Port: &port, Released: true}
}

func TestReleaseCLIBehaviorAndConfigIndependence(t *testing.T) {
	cfg := t.TempDir()
	root := registryRoot(t, cfg)
	owner := t.TempDir()
	if got := run(t, cfg, 1, "release", "default", "--directory", owner); !strings.Contains(got, "reservation not found") {
		t.Fatal(got)
	}
	path := filepath.Join(root, booking.File)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("missing release created registry", err)
	}
	p, held := candidateSockets(t, 2)
	configCandidates(t, cfg, p)
	for _, l := range held {
		l.Close()
	}
	run(t, cfg, 0, "reserve", "default", "--directory", owner)
	run(t, cfg, 0, "reserve", "other", "--directory", owner)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(owner, alias); err != nil {
		t.Fatal(err)
	}
	// Release uses neither the selected pool nor the saved pointer, including help.
	os.WriteFile(filepath.Join(cfg, "config.json"), []byte("broken"), 0600)
	os.WriteFile(filepath.Join(root, "location.json"), []byte("broken"), 0600)
	if got := run(t, cfg, 0, "release", "--help"); !strings.Contains(got, "release SERVICE") {
		t.Fatal(got)
	}
	alternate := t.TempDir()
	cmd := command(cfg, "release", "default", "--directory", alias, "--config-dir", alternate)
	// Invalid selection environment is irrelevant without an explicit flag.
	cmd.Env = append(cmd.Env, "PORTBOOK_CONFIG_DIR=relative")
	res := releaseReceipt(t, processResult(t, cmd, 0))
	for _, name := range []string{booking.File, "state.lock", "config.json"} {
		if _, err := os.Stat(filepath.Join(alternate, name)); !os.IsNotExist(err) {
			t.Fatal("release wrote selected config directory", name, err)
		}
	}
	physical, _ := booking.Directory(owner)
	if !res.Released || res.Port == nil || *res.Port != p[0] || res.Directory != physical {
		t.Fatal(res)
	}
	after, err := os.ReadFile(path)
	if err != nil || bytes.Equal(before, after) {
		t.Fatal(err)
	}
	var doc struct{ Bookings map[string]map[string]int }
	if err := json.Unmarshal(after, &doc); err != nil || len(doc.Bookings[physical]) != 1 || doc.Bookings[physical]["other"] != p[1] {
		t.Fatal(string(after), err)
	}
	cmd = command(cfg, "release", "default")
	cmd.Dir = owner
	if got := processResult(t, cmd, 1); !strings.Contains(got, "reservation not found") {
		t.Fatal(got)
	}
	unchanged, _ := os.ReadFile(path)
	if !bytes.Equal(after, unchanged) {
		t.Fatal("no-op rewrote registry")
	}
	if got := run(t, cfg, 0, "release", "other", "--directory", owner); !strings.Contains(got, "Released port") {
		t.Fatal(got)
	}
	if got := run(t, cfg, 1, "release", "other", "--directory", owner); !strings.Contains(got, "reservation not found") {
		t.Fatal(got)
	}
	for _, name := range []string{"config.json", "location.json"} {
		parent := cfg
		if name == "location.json" {
			parent = root
		}
		data, _ := os.ReadFile(filepath.Join(parent, name))
		if string(data) != "broken" {
			t.Fatal("config evidence changed")
		}
	}
}

func TestReleaseCLIBusySucceedsAndCorruptionPreservesState(t *testing.T) {
	cfg := t.TempDir()
	root := registryRoot(t, cfg)
	owner, _ := booking.Directory(t.TempDir())
	p, _ := candidateSockets(t, 1)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, booking.File)
	data, _ := json.Marshal(map[string]any{"version": 1, "bookings": map[string]map[string]int{owner: {"default": p[0]}}})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	res := releaseReceipt(t, run(t, cfg, 0, "release", "default", "--directory", owner))
	if !res.Released || res.Port == nil || *res.Port != p[0] {
		t.Fatal(res)
	}

	for _, bad := range []string{"broken", `{"version":2,"bookings":{}}`, `{"version":1,"bookings":{"/gone":{"x":1,"x":2}}}`} {
		os.WriteFile(path, []byte(bad), 0600)
		for i := 0; i < 2; i++ {
			if got := run(t, cfg, 1, "release", "default", "--directory", owner); !strings.Contains(got, `"code":"storage_error"`) {
				t.Fatal(got)
			}
			after, _ := os.ReadFile(path)
			if string(after) != bad {
				t.Fatal("evidence changed")
			}
		}
	}
}

func TestReleaseCLIValidationAndFlagBoundaries(t *testing.T) {
	cfg := t.TempDir()
	owner := t.TempDir()
	file := filepath.Join(owner, "file")
	os.WriteFile(file, []byte("keep"), 0600)
	for _, args := range [][]string{
		{"release", "a", "b"}, {"release", ""}, {"release", "default", "--directory="},
		{"release", "default", "--directory=relative"}, {"release", "default", "--directory", file},
		{"release", "default", "--directory", filepath.Join(owner, "absent")},
		{"release", "default", "--directory", owner, "--directory", owner}, {"release", "default", "--all"},
		{"release", "--output=yaml"}, {"release", "--output=json", "--output=table"},
		{"release", "a", "b", "--help"}, {"release", "default", "--directory=relative", "--help"},
		{"release", "--config-dir=relative"}, {"release", "--unknown", "--output=json"},
	} {
		if got := run(t, cfg, 2, args...); !strings.Contains(got, `"code":"usage_error"`) {
			t.Fatal(args, got)
		}
	}
	for _, service := range []string{"--help", "--output=table", "服务"} {
		if got := run(t, cfg, 1, "release", "--directory", owner, "--", service); !strings.Contains(got, "reservation not found") {
			t.Fatal(got)
		}
	}
	noAccess := t.TempDir()
	os.Chmod(noAccess, 0000)
	defer os.Chmod(noAccess, 0700)
	if os.Geteuid() != 0 {
		run(t, cfg, 2, "release", "default", "--directory", noAccess)
	}
}

func TestReleaseReserveIndependentProcessOrdering(t *testing.T) {
	cfg := t.TempDir()
	root := registryRoot(t, cfg)
	owner := t.TempDir()
	p, held := candidateSockets(t, 2)
	configCandidates(t, cfg, p)
	for _, l := range held {
		l.Close()
	}
	run(t, cfg, 0, "reserve", "default", "--directory", owner)
	// Queue a real release process behind the same OS lock. It cannot finish
	// while held; after unlocking, both mutations must produce a serial result.
	lock := flock.New(filepath.Join(root, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	cmds := []*exec.Cmd{command(cfg, "release", "default", "--directory", owner), command(cfg, "reserve", "new", "--directory", owner)}
	outs := make([]bytes.Buffer, 2)
	diags := make([]bytes.Buffer, 2)
	for i, cmd := range cmds {
		cmd.Stdout = &outs[i]
		cmd.Stderr = &diags[i]
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil || diags[i].Len() != 0 {
			t.Fatal(err, diags[i].String())
		}
	}
	res := releaseReceipt(t, outs[0].String())
	if !res.Released || res.Port == nil || *res.Port != p[0] {
		t.Fatal(res)
	}
	data, err := os.ReadFile(filepath.Join(root, booking.File))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Bookings map[string]map[string]int }
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	physical, _ := booking.Directory(owner)
	// Either serial order must preserve the new reservation and remove default.
	want, err := strconv.Atoi(strings.TrimSpace(outs[1].String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Bookings) != 1 || len(doc.Bookings[physical]) != 1 || doc.Bookings[physical]["new"] != want || (want != p[0] && want != p[1]) {
		t.Fatal(string(data))
	}
}

func TestReleaseListReserveCombinedLifecycle(t *testing.T) {
	cfg := t.TempDir()
	root := registryRoot(t, cfg)
	owner := t.TempDir()
	canonical, err := booking.Directory(owner)
	if err != nil {
		t.Fatal(err)
	}
	p, held := candidateSockets(t, 2)
	configCandidates(t, cfg, p)
	for _, l := range held {
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
	}
	cmd := command(cfg, "reserve", "default")
	cmd.Dir = owner
	if got := processResult(t, cmd, 0); got != strconv.Itoa(p[0])+"\n" {
		t.Fatal(got)
	}
	if got := run(t, cfg, 0, "reserve", "server", "--directory", owner); got != strconv.Itoa(p[1])+"\n" {
		t.Fatal(got)
	}
	alias := filepath.Join(t.TempDir(), "owner-alias")
	if err := os.Symlink(owner, alias); err != nil {
		t.Fatal(err)
	}
	rows := listEntries(t, run(t, cfg, 0, "list", "--directory", alias))
	if len(rows) != 2 || rows[0] != (booking.Entry{Directory: canonical, Service: "default", Port: p[0]}) || rows[1] != (booking.Entry{Directory: canonical, Service: "server", Port: p[1]}) {
		t.Fatal(rows)
	}
	res := releaseReceipt(t, run(t, cfg, 0, "release", "default", "--directory", alias))
	if !res.Released || res.Port == nil || *res.Port != p[0] {
		t.Fatal(res)
	}
	rows = listEntries(t, run(t, cfg, 0, "list", "--all"))
	if len(rows) != 1 || rows[0].Service != "server" || rows[0].Port != p[1] {
		t.Fatal(rows)
	}
	if got := run(t, cfg, 0, "reserve", "default", "--directory", owner); got != strconv.Itoa(p[0])+"\n" {
		t.Fatal("released port not reusable", got)
	}
	run(t, cfg, 0, "config", "remove", "--all")
	if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte("broken pool"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "location.json"), []byte("broken pointer"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, feature := range []string{"get", "list", "release"} {
		cmd := command(cfg, feature, "--help")
		cmd.Env = append(cmd.Env, "PORTBOOK_CONFIG_DIR=relative unused")
		if got := processResult(t, cmd, 0); !strings.Contains(got, "--directory") {
			t.Fatal(feature, got)
		}
	}
	rows = listEntries(t, run(t, cfg, 0, "list", "--all"))
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	if got := run(t, cfg, 1, "reserve", "new", "--directory", owner); !strings.Contains(got, `"code":"storage_error"`) {
		t.Fatal(got)
	}
	res = releaseReceipt(t, run(t, cfg, 0, "release", "server", "--directory", alias))
	if !res.Released || res.Port == nil || *res.Port != p[1] {
		t.Fatal(res)
	}
	rows = listEntries(t, run(t, cfg, 0, "list", "--directory", alias))
	if len(rows) != 1 || rows[0].Service != "default" || rows[0].Port != p[0] {
		t.Fatal(rows)
	}
}
