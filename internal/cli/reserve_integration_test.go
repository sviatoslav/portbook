package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/sviatoslav/portbook/internal/booking"
)

// Candidate ports come from genuinely bound ephemeral sockets, not assumptions
// about a particular fixed port being free on the test host.
func candidateSockets(t *testing.T, n int) ([]int, map[int]net.Listener) {
	t.Helper()
	ports := []int{}
	held := map[int]net.Listener{}
	for len(ports) < n {
		l, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := l.Addr().(*net.TCPAddr).Port
		if _, exists := held[p]; exists {
			l.Close()
			continue
		}
		ports = append(ports, p)
		held[p] = l
	}
	sort.Ints(ports)
	t.Cleanup(func() {
		for _, l := range held {
			l.Close()
		}
	})
	return ports, held
}
func configCandidates(t *testing.T, dir string, ports []int) {
	t.Helper()
	run(t, dir, 0, "config", "remove", "--all")
	args := []string{"config", "add"}
	for _, p := range ports {
		args = append(args, strconv.Itoa(p))
	}
	run(t, dir, 0, args...)
}
func processResult(t *testing.T, cmd *exec.Cmd, want int) string {
	t.Helper()
	var out, diag bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &diag
	err := cmd.Run()
	actual := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		actual = exit.ExitCode()
	}
	if actual != want {
		t.Fatalf("%v: code=%d want=%d stdout=%q stderr=%q", cmd.Args, actual, want, out.String(), diag.String())
	}
	if want != 0 {
		if out.Len() != 0 {
			t.Fatal("failure stdout", out.String())
		}
		return diag.String()
	}
	if diag.Len() != 0 {
		t.Fatal("success stderr", diag.String())
	}
	return out.String()
}
func registryRoot(t *testing.T, dir string) string {
	t.Helper()
	var loc struct{ Directory string }
	cmd := locationCommand(dir, "config", "path", "get")
	if err := json.Unmarshal([]byte(processResult(t, cmd, 0)), &loc); err != nil {
		t.Fatal(err)
	}
	return loc.Directory
}
func TestReserveCLISelectionCwdAliasesAndGet(t *testing.T) {
	cfg := t.TempDir()
	p, held := candidateSockets(t, 4)
	configCandidates(t, cfg, p)
	for _, port := range p[1:] {
		held[port].Close()
		if ok, err := booking.Available(context.Background(), port); err != nil || !ok {
			t.Fatalf("ephemeral candidate unavailable: %v", err)
		}
	}
	owner := t.TempDir()
	nested := filepath.Join(owner, "nested")
	os.Mkdir(nested, 0700)
	os.Mkdir(filepath.Join(owner, ".git"), 0700)
	// Keep the lowest candidate occupied by an unmanaged loopback listener.
	cmd := command(cfg, "reserve", "default")
	cmd.Dir = nested
	want := fmt.Sprint(p[1]) + "\n"
	if got := processResult(t, cmd, 0); got != want {
		t.Fatal(got, want)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(nested, alias); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"get", "default", "--directory", nested},
		{"get", "default", "--directory", alias},
	} {
		if got := run(t, cfg, 0, args...); got != want {
			t.Fatal(args, got, want)
		}
	}
	if got := run(t, cfg, 0, "reserve", "server", "--directory", nested); got != fmt.Sprint(p[2])+"\n" {
		t.Fatal(got)
	}
	// An idle booking remains unavailable to a different directory, even if it
	// has the same service. The nested cwd did not collapse into its Git root.
	if got := run(t, cfg, 0, "reserve", "default", "--directory", owner); got != fmt.Sprint(p[3])+"\n" {
		t.Fatal(got)
	}
	root := registryRoot(t, cfg)
	path := filepath.Join(root, booking.File)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version  int
		Bookings map[string]map[string]int
	}
	if err := json.Unmarshal(before, &doc); err != nil {
		t.Fatal(err)
	}
	physical, err := booking.Directory(nested)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.Bookings[physical]["default"] != p[1] || len(doc.Bookings) != 2 {
		t.Fatal(string(before))
	}
	run(t, cfg, 0, "config", "remove", "--all")
	if got := run(t, cfg, 0, "get", "default", "--directory", nested); got != want {
		t.Fatal(got)
	}
	if got := run(t, cfg, 1, "reserve", "extra", "--directory", nested); !strings.Contains(got, `"code":"runtime_error"`) {
		t.Fatal(got)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("get/exhaustion rewrote bookings")
	}
	for _, name := range []string{booking.File, "state.lock", "location.json"} {
		if _, err := os.Stat(filepath.Join(cfg, name)); !os.IsNotExist(err) {
			t.Fatal("state leaked into config override", name, err)
		}
	}
}

func TestReserveCLIValidationHelpAndBoundaries(t *testing.T) {
	cfg := t.TempDir()
	owner := t.TempDir()
	file := filepath.Join(owner, "file")
	os.WriteFile(file, []byte("keep"), 0600)
	for _, args := range [][]string{
		{"reserve", "a", "b"}, {"reserve", ""}, {"reserve", "default", "--directory="},
		{"reserve", "default", "--directory", "relative"}, {"reserve", "default", "--directory", file},
		{"reserve", "default", "--directory", filepath.Join(owner, "absent")},
		{"reserve", "default", "--directory", owner, "--directory", owner},
		{"reserve", "--all"}, {"config", "show", "--directory", owner},
		{"reserve", "--output=yaml"}, {"reserve", "--output=json", "--output=table"},
		{"reserve", "a", "b", "--help"}, {"reserve", "default", "--directory=relative", "--help"},
	} {
		got := run(t, cfg, 2, args...)
		if !strings.Contains(got, `"code":"usage_error"`) {
			t.Fatal(args, got)
		}
	}
	for _, args := range [][]string{{"reserve", "--help"}, {"reserve", "server", "--directory", owner, "--help"}} {
		got := run(t, cfg, 0, args...)
		if !strings.Contains(got, "reserve SERVICE") || !strings.Contains(got, "--directory") {
			t.Fatal(got)
		}
	}
	root := registryRoot(t, cfg)
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("validation/help created global state", err)
	}
	p, held := candidateSockets(t, 2)
	configCandidates(t, cfg, p)
	for _, l := range held {
		l.Close()
	}
	// A flag-looking service is valid data after --. The output format is still
	// selected only by flags before the delimiter.
	got := run(t, cfg, 0, "reserve", "--directory", owner, "--", "--output=table")
	if _, err := strconv.Atoi(strings.TrimSpace(got)); err != nil {
		t.Fatal(got)
	}
	got = run(t, cfg, 0, "reserve", "--directory", owner, "--", "--help")
	if _, err := strconv.Atoi(strings.TrimSpace(got)); err != nil {
		t.Fatal(got)
	}
	run(t, cfg, 2, "reserve", "--", "--help", "another")
	noAccess := t.TempDir()
	os.Chmod(noAccess, 0000)
	defer os.Chmod(noAccess, 0700)
	if os.Geteuid() != 0 {
		if got := run(t, cfg, 2, "reserve", "default", "--directory", noAccess); !strings.Contains(got, "cannot access directory") {
			t.Fatal(got)
		}
	}
}

func TestReserveIndependentConcurrentProcesses(t *testing.T) {
	cfg := t.TempDir()
	owner := t.TempDir()
	const workers = 12
	p, held := candidateSockets(t, workers+8)
	configCandidates(t, cfg, p)
	for _, l := range held {
		l.Close()
	}
	root := registryRoot(t, cfg)
	runBatch := func(same bool) {
		cmds := make([]*exec.Cmd, workers)
		outs := make([]bytes.Buffer, workers)
		diags := make([]bytes.Buffer, workers)
		for i := range cmds {
			service := "same"
			if !same {
				service = fmt.Sprint("service-", i)
			}
			// All processes use one registry even when selected config paths differ.
			args := []string{"reserve", service, "--directory", owner}
			if i%2 == 0 {
				args = append(args, "--config-dir", cfg)
			}
			cmds[i] = command(cfg, args...)
			cmds[i].Stdout = &outs[i]
			cmds[i].Stderr = &diags[i]
			if err := cmds[i].Start(); err != nil {
				t.Fatal(err)
			}
		}
		seen := map[string]bool{}
		failures := 0
		for i, cmd := range cmds {
			if err := cmd.Wait(); err != nil {
				if same && cmd.ProcessState.ExitCode() == 1 && outs[i].Len() == 0 && strings.Contains(diags[i].String(), "reservation already exists") && strings.Contains(diags[i].String(), `"code":"runtime_error"`) {
					failures++
					continue
				}
				t.Fatalf("worker %d: %v %s", i, err, &diags[i])
			}
			if diags[i].Len() != 0 {
				t.Fatal(&diags[i])
			}
			port := outs[i].String()
			n, err := strconv.Atoi(strings.TrimSuffix(port, "\n"))
			if err != nil || n < 1 {
				t.Fatal(port, err)
			}
			if !same && seen[port] {
				t.Fatal("double booked", port)
			}
			seen[port] = true
		}
		if same && (len(seen) != 1 || failures != workers-1) {
			t.Fatal("same identity diverged", seen)
		}
	}
	runBatch(true)
	runBatch(false)
	data, err := os.ReadFile(filepath.Join(root, booking.File))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Bookings map[string]map[string]int }
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	physical, _ := booking.Directory(owner)
	if len(doc.Bookings[physical]) != workers+1 {
		t.Fatal("lost writes", string(data))
	}
}

func TestReserveCorruptRegistryEvidenceAcrossProcesses(t *testing.T) {
	cfg := t.TempDir()
	root := registryRoot(t, cfg)
	os.MkdirAll(root, 0700)
	path := filepath.Join(root, booking.File)
	owner := t.TempDir()
	for _, bad := range []string{"broken", `{"version":2,"bookings":{}}`, `{"version":1,"bookings":{"/gone":{"x":1,"x":2}}}`} {
		os.WriteFile(path, []byte(bad), 0600)
		for i := 0; i < 2; i++ {
			got := run(t, cfg, 1, "reserve", "default", "--directory", owner)
			if !strings.Contains(got, `"code":"storage_error"`) {
				t.Fatal(got)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != bad {
				t.Fatal("evidence changed", err)
			}
		}
	}
}

func TestReserveConfigPrecedenceKeepsOneGlobalRegistry(t *testing.T) {
	home := filepath.Join(t.TempDir(), "isolated")
	owner := t.TempDir()
	configs := []string{t.TempDir(), t.TempDir(), t.TempDir()}
	p, held := candidateSockets(t, 3)
	for i, dir := range configs {
		locationRun(t, home, 0, "--config-dir", dir, "config", "remove", "--all")
		locationRun(t, home, 0, "--config-dir", dir, "config", "add", fmt.Sprint(p[i]))
	}
	root := registryRoot(t, home)
	locationRun(t, home, 0, "config", "path", "set", configs[0])
	pointerPath := filepath.Join(root, "location.json")
	pointer, err := os.ReadFile(pointerPath)
	if err != nil {
		t.Fatal(err)
	}
	configBefore := make([][]byte, len(configs))
	for i, dir := range configs {
		configBefore[i], err = os.ReadFile(filepath.Join(dir, "config.json"))
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, l := range held {
		l.Close()
	}
	// Saved < environment < flag, while the fixed HOME/XDG root remains the same.
	saved := locationRun(t, home, 0, "reserve", "saved", "--directory", owner)
	cmd := locationCommand(home, "reserve", "environment", "--directory", owner)
	cmd.Env = append(cmd.Env, "PORTBOOK_CONFIG_DIR="+configs[1])
	env := processResult(t, cmd, 0)
	cmd = locationCommand(home, "reserve", "flag", "--directory", owner, "--config-dir", configs[2])
	cmd.Env = append(cmd.Env, "PORTBOOK_CONFIG_DIR="+configs[1])
	flag := processResult(t, cmd, 0)
	for i, got := range []string{saved, env, flag} {
		if got != fmt.Sprint(p[i])+"\n" {
			t.Fatal(i, got, p[i])
		}
	}
	// Get is independent of whichever pool is now selected.
	if got := locationRun(t, home, 0, "get", "flag", "--directory", owner); got != flag {
		t.Fatal(got, flag)
	}
	after, _ := os.ReadFile(pointerPath)
	if !bytes.Equal(pointer, after) {
		t.Fatal("reserve wrote pointer")
	}
	for i, dir := range configs {
		after, err := os.ReadFile(filepath.Join(dir, "config.json"))
		if err != nil || !bytes.Equal(configBefore[i], after) {
			t.Fatal("reserve wrote config", err)
		}
		for _, name := range []string{booking.File, "state.lock"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Fatal("state follows override", name, err)
			}
		}
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
	if len(doc.Bookings[physical]) != 3 {
		t.Fatal("registry split", string(data))
	}
}
