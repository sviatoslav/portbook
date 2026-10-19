package cli_test

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/sviatoslav/portbook/internal/booking"
)

func TestCleanupListReleaseReserveCombinedLifecycle(t *testing.T) {
	cfg := t.TempDir()
	candidates, held := candidateSockets(t, 5)
	configCandidates(t, cfg, candidates)
	for _, listener := range held {
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
	}
	physical := func(path string) string {
		t.Helper()
		owner, err := booking.Directory(path)
		if err != nil {
			t.Fatal(err)
		}
		return owner
	}
	live := physical(t.TempDir())
	gone := physical(t.TempDir())
	busy := physical(t.TempDir())
	uncertainParent := physical(t.TempDir())
	uncertain := filepath.Join(uncertainParent, "owner")
	if err := os.Mkdir(uncertain, 0700); err != nil {
		t.Fatal(err)
	}
	uncertain = physical(uncertain)
	for i, owner := range []string{gone, busy, uncertain, live} {
		service := "default"
		if owner == live {
			service = "server"
		}
		if got := run(t, cfg, 0, "reserve", service, "--directory", owner); got != fmt.Sprint(candidates[i])+"\n" {
			t.Fatal("unexpected initial allocation", got, candidates[i])
		}
	}
	if got := run(t, cfg, 0, "reserve", "default", "--directory", live); got != fmt.Sprint(candidates[4])+"\n" {
		t.Fatal(got)
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", candidates[1]))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(busy); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(uncertainParent); err != nil {
		t.Fatal(err)
	}
	root := registryRoot(t, cfg)
	registryPath := filepath.Join(root, booking.File)
	before, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	poolPath := filepath.Join(cfg, "config.json")
	pool, err := os.ReadFile(poolPath)
	if err != nil {
		t.Fatal(err)
	}
	pointerPath := filepath.Join(root, "location.json")
	pointer := []byte("broken pointer retained")
	if err := os.WriteFile(pointerPath, pointer, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(poolPath, []byte("broken pool retained"), 0600); err != nil {
		t.Fatal(err)
	}
	alternate := t.TempDir()
	if err := os.WriteFile(filepath.Join(alternate, "config.json"), []byte("also broken"), 0600); err != nil {
		t.Fatal(err)
	}
	dry := cleanupReport(t, run(t, cfg, 0, "--config-dir", alternate, "cleanup", "--dry-run"))
	if dry.WouldRemove != 1 || dry.Removed != 0 || dry.Retained != 4 {
		t.Fatal(dry)
	}
	reasons := map[string]string{}
	for _, row := range dry.Rows {
		reasons[row.Directory] = row.Reason
	}
	if reasons[gone] != "owner_missing" || reasons[busy] != "port_busy_or_unavailable" || reasons[uncertain] != "owner_uncertain" || reasons[live] != "owner_exists" {
		t.Fatal(reasons)
	}
	after, err := os.ReadFile(registryPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("preview modified registry", err)
	}
	if rows := listEntries(t, run(t, cfg, 0, "list", "--all", "--config-dir", alternate)); len(rows) != 5 {
		t.Fatal(rows)
	}
	released := releaseJSON(t, run(t, cfg, 0, "release", "default", "--directory", live, "--config-dir", alternate))
	if !released.Released || released.Port == nil || *released.Port != candidates[4] {
		t.Fatal(released)
	}
	actual := cleanupReport(t, run(t, cfg, 0, "cleanup", "--config-dir", alternate))
	if actual.Removed != 1 || actual.Retained != 3 || actual.WouldRemove != 0 {
		t.Fatal(actual)
	}
	rows := listEntries(t, run(t, cfg, 0, "list", "--all"))
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	// All registry-only commands and their help bypass irrelevant broken config.
	for _, feature := range []string{"list", "release", "cleanup"} {
		cmd := command(cfg, feature, "--help")
		cmd.Env = append(cmd.Env, "PORTBOOK_CONFIG_DIR=relative-invalid-selection")
		processResult(t, cmd, 0)
	}
	after, err = os.ReadFile(pointerPath)
	if err != nil || !bytes.Equal(pointer, after) {
		t.Fatal("changed saved pointer", err)
	}
	after, err = os.ReadFile(poolPath)
	if err != nil || string(after) != "broken pool retained" {
		t.Fatal("changed selected pool", err)
	}
	for _, name := range []string{booking.File, "state.lock", "location.json"} {
		if _, err := os.Stat(filepath.Join(alternate, name)); !os.IsNotExist(err) {
			t.Fatal("global state followed override", name, err)
		}
	}
	if err := os.WriteFile(poolPath, pool, 0600); err != nil {
		t.Fatal(err)
	}
	if got := run(t, cfg, 0, "reserve", "reclaimed", "--directory", live); got != fmt.Sprint(candidates[0])+"\n" {
		t.Fatal("cleanup port not reusable", got)
	}
	rows = listEntries(t, run(t, cfg, 0, "list", "--all"))
	if len(rows) != 4 {
		t.Fatal(rows)
	}
	found := map[string]bool{}
	for _, row := range rows {
		found[row.Directory] = true
		if row.Directory == gone {
			t.Fatal("deleted owner survived", row)
		}
	}
	if !found[busy] || !found[uncertain] || !found[live] {
		t.Fatal("retained owners lost", rows)
	}
}
