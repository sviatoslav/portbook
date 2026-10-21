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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/sviatoslav/portbook/internal/booking"
)

func TestReleaseAllCwdIsolationBusyListenerAndReserve(t *testing.T) {
	cfg := t.TempDir()
	p, held := candidateSockets(t, 4)
	configCandidates(t, cfg, p)
	for _, port := range p[1:] {
		held[port].Close()
	}
	owner, _ := booking.Directory(t.TempDir())
	child := filepath.Join(owner, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	other, _ := booking.Directory(t.TempDir())
	root := registryRoot(t, cfg)
	path := filepath.Join(root, booking.File)
	data, err := json.Marshal(map[string]any{"version": 1, "bookings": booking.Registry{owner: {"default": p[0], "z": p[1]}, child: {"api": p[2]}, other: {"api": p[3]}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := command(cfg, "release", "--all")
	cmd.Dir = owner
	text := processResult(t, cmd, 0)
	if text != fmt.Sprintf("Released 2 bookings in %q: %d, %d.\n", owner, p[0], p[1]) {
		t.Fatal(text)
	}
	rows := listEntries(t, run(t, cfg, 0, "list", "--all"))
	if len(rows) != 2 {
		t.Fatal(rows)
	}
	for _, row := range rows {
		if row.Directory != child && row.Directory != other {
			t.Fatal("wrong owner retained", row)
		}
	}
	connection, err := net.DialTimeout("tcp4", held[p[0]].Addr().String(), time.Second)
	if err != nil {
		t.Fatal("listener stopped", err)
	}
	connection.Close()
	if available, err := booking.Available(context.Background(), p[0]); err != nil || available {
		t.Fatal(available, err)
	}
	if got := run(t, cfg, 0, "reserve", "new", "--directory", other); got != fmt.Sprint(p[1])+"\n" {
		t.Fatal("released busy port reused", got, p)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(owner, alias); err != nil {
		t.Fatal(err)
	}
	if got := run(t, cfg, 1, "release", "--all", "--directory", alias); !strings.Contains(got, "reservation not found") {
		t.Fatal(got)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("missing all rewrote state")
	}
}

func TestReleaseAllAndReserveIndependentProcessSerialization(t *testing.T) {
	cfg, owner, other := t.TempDir(), t.TempDir(), t.TempDir()
	p, held := candidateSockets(t, 3)
	configCandidates(t, cfg, p)
	for _, listener := range held {
		listener.Close()
	}
	run(t, cfg, 0, "reserve", "default", "--directory", owner)
	run(t, cfg, 0, "reserve", "api", "--directory", owner)
	root := registryRoot(t, cfg)
	lock := flock.New(filepath.Join(root, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	cmds := []*exec.Cmd{command(cfg, "release", "--all", "--directory", owner), command(cfg, "reserve", "new", "--directory", other)}
	outs, diags := make([]bytes.Buffer, 2), make([]bytes.Buffer, 2)
	for i, cmd := range cmds {
		cmd.Stdout, cmd.Stderr = &outs[i], &diags[i]
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
	canonicalOwner, err := booking.Directory(owner)
	if err != nil {
		t.Fatal(err)
	}
	if outs[0].String() != fmt.Sprintf("Released 2 bookings in %q: %d, %d.\n", canonicalOwner, p[0], p[1]) {
		t.Fatal(outs[0].String())
	}
	port, err := strconv.Atoi(strings.TrimSpace(outs[1].String()))
	if err != nil {
		t.Fatal(err)
	}
	rows := listEntries(t, run(t, cfg, 0, "list", "--all"))
	canonical, _ := booking.Directory(other)
	if len(rows) != 1 || rows[0] != (booking.Entry{Directory: canonical, Service: "new", Port: port}) || (port != p[0] && port != p[1] && port != p[2]) {
		t.Fatal("lost or unexpected writes", rows, port, p)
	}
}
