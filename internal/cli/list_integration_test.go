package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gofrs/flock"
	"portbook/internal/booking"
)

func listEntries(t *testing.T, raw string) []booking.Entry {
	t.Helper()
	var result struct {
		Command  string          `json:"command"`
		Bookings []booking.Entry `json:"bookings"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil || result.Command != "list" || result.Bookings == nil {
		t.Fatal(raw, err)
	}
	return result.Bookings
}

func TestListCLIReadOnlySnapshotAndCwd(t *testing.T) {
	home := filepath.Join(t.TempDir(), "isolated")
	root := registryRoot(t, home)
	if got := locationRun(t, home, 0, "list", "--all"); got != "{\"command\":\"list\",\"bookings\":[]}\n" {
		t.Fatal(got)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("fresh list created state", err)
	}
	owner, err := booking.Directory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(owner, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	// A Git marker makes accidental root discovery observable.
	if err := os.Mkdir(filepath.Join(owner, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(t.TempDir(), "disappeared")
	ports, held := candidateSockets(t, 1)
	busy := ports[0]
	_ = held // listener remains bound throughout list
	registry := map[string]any{"version": 1, "bookings": map[string]map[string]int{owner: {"z": busy, "a": 1}, nested: {"nested": 2}, gone: {"old": 3}}}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bookings.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "location.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("broken"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	lock := flock.New(filepath.Join(root, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	cmd := locationCommand(home, "list")
	cmd.Dir = nested
	if got := listEntries(t, processResult(t, cmd, 0)); !reflect.DeepEqual(got, []booking.Entry{{Directory: nested, Service: "nested", Port: 2}}) {
		t.Fatal(got)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(owner, alias); err != nil {
		t.Fatal(err)
	}
	want := []booking.Entry{{Directory: owner, Service: "a", Port: 1}, {Directory: owner, Service: "z", Port: busy}}
	if got := listEntries(t, locationRun(t, home, 0, "list", "--directory", alias)); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	raw := locationRun(t, home, 0, "list", "--all")
	got := listEntries(t, raw)
	if len(got) != 4 {
		t.Fatal(got)
	}
	found := false
	for i, e := range got {
		if e.Directory == gone {
			found = true
		}
		if i > 0 && (got[i-1].Directory > e.Directory || got[i-1].Directory == e.Directory && got[i-1].Service > e.Service) {
			t.Fatal(got)
		}
	}
	if !found {
		t.Fatal("lost disappeared owner")
	}
	scoped := locationRun(t, home, 0, "list", "--directory", owner, "--output=table")
	if strings.Join(strings.Fields(scoped), " ") != fmt.Sprintf("SERVICE PORT a 1 z %d", busy) {
		t.Fatal(scoped)
	}
	table := locationRun(t, home, 0, "list", "--all", "--output=table")
	if !strings.HasPrefix(table, "DIRECTORY") || !strings.Contains(table, gone) {
		t.Fatal(table)
	}
	if got := locationRun(t, home, 0, "list", "--help"); !strings.Contains(got, "--all") {
		t.Fatal(got)
	}
	cmd = locationCommand(home, "list", "--all", "--config-dir", t.TempDir())
	cmd.Env = append(cmd.Env, "PORTBOOK_CONFIG_DIR=invalid")
	if got := processResult(t, cmd, 0); got != raw {
		t.Fatal(got)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("rewrote registry", err)
	}
	if got := locationRun(t, home, 2, "list", "--directory", gone); !strings.Contains(got, "usage_error") {
		t.Fatal(got)
	}
}

func TestListCLIConcurrentAtomicSnapshots(t *testing.T) {
	home := filepath.Join(t.TempDir(), "isolated")
	root := registryRoot(t, home)
	ports, held := candidateSockets(t, 8)
	args := []string{"config", "ranges", "add"}
	for _, p := range ports {
		held[p].Close()
		args = append(args, fmt.Sprint(p))
	}
	locationRun(t, home, 0, args...)
	owner := t.TempDir()
	// Independent executable writers overlap independent executable readers.
	var wg sync.WaitGroup
	errors := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := locationCommand(home, "reserve", fmt.Sprintf("service-%02d", i), "--directory", owner)
			if out, err := cmd.CombinedOutput(); err != nil {
				errors <- fmt.Errorf("reserve: %v %s", err, out)
			}
		}(i)
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := locationCommand(home, "list", "--all")
			out, err := cmd.Output()
			if err != nil {
				errors <- err
				return
			}
			var result struct{ Bookings []booking.Entry }
			if err := json.Unmarshal(out, &result); err != nil {
				errors <- err
				return
			}
			seen := map[int]bool{}
			for _, e := range result.Bookings {
				if seen[e.Port] {
					errors <- fmt.Errorf("duplicate port in snapshot")
					return
				}
				seen[e.Port] = true
			}
		}()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	entries := listEntries(t, locationRun(t, home, 0, "list", "--all"))
	if len(entries) != 8 {
		t.Fatal(entries)
	}
	data, err := os.ReadFile(filepath.Join(root, "bookings.json"))
	if err != nil {
		t.Fatal(err)
	}
	locationRun(t, home, 0, "config", "ranges", "remove", "--all")
	if got := listEntries(t, locationRun(t, home, 0, "list", "--all")); !reflect.DeepEqual(got, entries) {
		t.Fatal(got)
	}
	after, err := os.ReadFile(filepath.Join(root, "bookings.json"))
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("pool removal rewrote bookings", err)
	}
}

func TestListCLIRegistryFailurePreservesEvidence(t *testing.T) {
	home := filepath.Join(t.TempDir(), "isolated")
	root := registryRoot(t, home)
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "bookings.json")
	bad := []byte(`{"version":1,"bookings":{"/gone":{"a":1,"a":2}}}`)
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if got := locationRun(t, home, 1, "list", "--all"); !strings.Contains(got, "storage_error") {
			t.Fatal(got)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(bad, after) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "state.lock")); !os.IsNotExist(err) {
		t.Fatal("failure created lock", err)
	}
}

func TestListAllDoesNotRequireCurrentDirectory(t *testing.T) {
	home := filepath.Join(t.TempDir(), "isolated")
	root := registryRoot(t, home)
	owner := t.TempDir()
	// Start the actual executable after its shell has removed its own cwd.
	// --all must not call Getwd or validate a current owner.
	cmd := exec.Command("/bin/sh", "-c", `cd "$1" && rmdir "$1" && exec "$2" list --all`, "list-removed-cwd", owner, binary)
	cmd.Env = locationCommand(home).Env
	if got := processResult(t, cmd, 0); got != "{\"command\":\"list\",\"bookings\":[]}\n" {
		t.Fatal(got)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("list created state", err)
	}
}
