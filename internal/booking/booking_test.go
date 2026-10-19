package booking

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/ports"
	"github.com/sviatoslav/portbook/internal/state"
)

func fixture(t *testing.T, ranges ...ports.Range) (Store, string) {
	t.Helper()
	c := state.Coordinator{Dir: t.TempDir(), LockTimeout: 40 * time.Millisecond}
	cfg := config.Store{Coordinator: c}
	if _, _, err := cfg.Mutate(context.Background(), func(p *ports.Pool) (int, error) { *p = ports.Pool{}; return p.Apply(ranges, true), nil }); err != nil {
		t.Fatal(err)
	}
	return Store{Config: cfg, Probe: func(context.Context, int) (bool, error) { return true, nil }}, t.TempDir()
}
func contents(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func reserve(t *testing.T, s Store, dir, service string) int {
	t.Helper()
	p, err := s.Reserve(context.Background(), dir, service)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSelectionAndStableReuse(t *testing.T) {
	s, dir := fixture(t, ports.Range{Start: 40000, End: 40002}, ports.Range{Start: 50000, End: 50000})
	calls := []int{}
	s.Probe = func(_ context.Context, p int) (bool, error) { calls = append(calls, p); return p != 40000, nil }
	if p := reserve(t, s, dir, "server"); p != 40001 {
		t.Fatal(p)
	}
	if p := reserve(t, s, dir, "default"); p != 40002 {
		t.Fatal(p)
	}
	if p := reserve(t, s, t.TempDir(), "server"); p != 50000 {
		t.Fatal(p)
	}
	if fmt.Sprint(calls) != "[40000 40001 40000 40002 40000 50000]" {
		t.Fatal(calls)
	}
	path := filepath.Join(s.Config.Coordinator.Dir, File)
	before := contents(t, path)
	if _, _, err := s.Config.Mutate(context.Background(), func(p *ports.Pool) (int, error) { *p = ports.Pool{}; return 0, nil }); err != nil {
		t.Fatal(err)
	}
	s.Probe = func(context.Context, int) (bool, error) { t.Fatal("reuse probed"); return false, nil }
	if p := reserve(t, s, dir, "server"); p != 40001 {
		t.Fatal(p)
	}
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("reuse rewrote registry")
	}
	if p, err := s.Reserve(context.Background(), dir, "new"); p != 0 || !errors.Is(err, ErrExhausted) {
		t.Fatal(p, err)
	}
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("exhaustion changed registry")
	}
}

func TestIdentityAndDirectoryValidation(t *testing.T) {
	s, dir := fixture(t, ports.Range{Start: 42000, End: 42010})
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if a, b := reserve(t, s, dir, "服務 / Version"), reserve(t, s, alias, "服務 / Version"); a != b {
		t.Fatal(a, b)
	}
	if a, b := reserve(t, s, dir, "version"), reserve(t, s, dir, "VERSION"); a == b {
		t.Fatal("case-folded service data")
	}
	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, []byte("keep"), 0600)
	before := contents(t, filepath.Join(s.Config.Coordinator.Dir, File))
	for _, target := range []string{"", "relative", filepath.Join(t.TempDir(), "missing"), file, dir + "\x00", dir + string([]byte{255})} {
		if _, err := s.Reserve(context.Background(), target, "server"); err == nil {
			t.Fatalf("accepted %q", target)
		}
	}
	for _, service := range []string{"", "\x00", string([]byte{255})} {
		if _, err := s.Reserve(context.Background(), dir, service); err == nil {
			t.Fatalf("accepted %q", service)
		}
	}
	if !bytes.Equal(before, contents(t, filepath.Join(s.Config.Coordinator.Dir, File))) {
		t.Fatal("validation changed registry")
	}
	for _, mode := range []os.FileMode{0000, 0400, 0100} {
		target := t.TempDir()
		if err := os.Chmod(target, mode); err != nil {
			t.Fatal(err)
		}
		_, err := Directory(target)
		os.Chmod(target, 0700)
		if os.Geteuid() != 0 && err == nil {
			t.Fatalf("accepted inaccessible mode %o", mode)
		}
	}
}

func TestStrictCodec(t *testing.T) {
	valid := `{"version":1,"bookings":{"/gone":{"version":1,"VERSION":2,"雪":3},"/other":{"server":65535}}}`
	r, err := decode([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	data, err := encode(r)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := decode(data)
	if err != nil || len(r2["/gone"]) != 3 {
		t.Fatal(r2, err)
	}
	for _, bad := range []string{
		`null`, `[]`, `{}`, `{"version":2,"bookings":{}}`, `{"version":1,"bookings":null}`,
		`{"version":1,"version":1,"bookings":{}}`, `{"version":1,"Version":1,"bookings":{}}`,
		`{"version":1,"bookings":{},"extra":0}`, `{"version":1,"bookings":{}} {}`,
		`{"version":1,"bookings":{"/x":{"a":1,"\u0061":2}}}`,
		`{"version":1,"bookings":{"/x":{"a":1},"/x":{"b":2}}}`,
		`{"version":1,"bookings":{"/x":{"a":1},"/y":{"b":1}}}`,
		`{"version":1,"bookings":{"relative":{"a":1}}}`,
		`{"version":1,"bookings":{"/x/../y":{"a":1}}}`,
		`{"version":1,"bookings":{"/x":{"":1}}}`, `{"version":1,"bookings":{"/x":null}}`,
		`{"version":1,"bookings":{"/x":{"a":0}}}`, `{"version":1,"bookings":{"/x":{"a":65536}}}`,
		`{"version":1,"bookings":{"/x":{"a":1.5}}}`, `{"version":1,"bookings":{"/x":{"a":"1"}}}`,
		`{"version":1,"bookings":{"/x":{"a":null}}}`, `{"version":1,"bookings":{"/x":{"\ud800":1}}}`,
		`{"version":1,"bookings":{"/\udc00":{"a":1}}}`, "{\"version\":1,\"bookings\":{\"/x\":{\"\xff\":1}}}",
	} {
		if _, err := decode([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestFailClosedPreservesEvidence(t *testing.T) {
	for _, bad := range []string{"broken", `{"version":9,"bookings":{}}`, `{"version":1,"bookings":{"/x":{"a":1,"a":2}}}`} {
		s, dir := fixture(t, ports.Range{Start: 41000, End: 41000})
		path := filepath.Join(s.Config.Coordinator.Dir, File)
		os.WriteFile(path, []byte(bad), 0600)
		for i := 0; i < 2; i++ {
			if _, err := s.Reserve(context.Background(), dir, "a"); err == nil {
				t.Fatal("accepted corrupt registry")
			}
			if string(contents(t, path)) != bad {
				t.Fatal("destroyed evidence")
			}
		}
	}
	for _, kind := range []string{"directory", "symlink", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			// Keep a selectable candidate so a broken read cannot pass this test
			// merely by returning a later pool-exhaustion error.
			s, dir := fixture(t, ports.Range{Start: 41001, End: 41001})
			s.Probe = func(context.Context, int) (bool, error) {
				t.Fatal("probed after accepting nonregular/inaccessible registry")
				return false, nil
			}
			path := filepath.Join(s.Config.Coordinator.Dir, File)
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(t.TempDir(), "data")
				if err := os.WriteFile(target, []byte(`{"version":1,"bookings":{}}`), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "unreadable":
				if os.Geteuid() == 0 {
					t.Skip("root bypasses permissions")
				}
				if err := os.WriteFile(path, []byte(`{"version":1,"bookings":{}}`), 0000); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Reserve(context.Background(), dir, "a"); err == nil {
				t.Fatal("accepted nonregular/inaccessible registry")
			}
		})
	}
}

func TestFailuresNeverSaveAndReleaseLock(t *testing.T) {
	s, dir := fixture(t, ports.Range{Start: 43000, End: 43002})
	reserve(t, s, dir, "a")
	path := filepath.Join(s.Config.Coordinator.Dir, File)
	before := contents(t, path)
	abort := errors.New("probe failure")
	s.Probe = func(context.Context, int) (bool, error) { return false, abort }
	if _, err := s.Reserve(context.Background(), dir, "b"); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Reserve(ctx, dir, "b"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	held := flock.New(filepath.Join(s.Config.Coordinator.Dir, "state.lock"))
	if err := held.Lock(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(context.Background(), dir, "b"); err == nil {
		t.Fatal("bypassed global lock")
	}
	held.Close()
	// Config and pointer reads are fail-closed, including reuse.
	os.WriteFile(filepath.Join(s.Config.Coordinator.Dir, "config.json"), []byte("bad"), 0600)
	if _, err := s.Reserve(context.Background(), dir, "a"); err == nil {
		t.Fatal("ignored corrupt config")
	}
	os.Remove(filepath.Join(s.Config.Coordinator.Dir, "config.json"))
	os.WriteFile(filepath.Join(s.Config.Coordinator.Dir, "location.json"), []byte("bad pointer"), 0600)
	if _, err := s.Reserve(context.Background(), dir, "a"); err == nil {
		t.Fatal("ignored corrupt pointer")
	}
	os.Remove(filepath.Join(s.Config.Coordinator.Dir, "location.json"))
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("failure changed prior bookings")
	}
	if err := s.Config.Coordinator.WithLock(context.Background(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestSaveFailurePreservesPriorBookings(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	s, dir := fixture(t, ports.Range{Start: 44000, End: 44002})
	reserve(t, s, dir, "a")
	path := filepath.Join(s.Config.Coordinator.Dir, File)
	before := contents(t, path)
	root := s.Config.Coordinator.Dir
	// Drop parent write permission only after the lock is acquired and read,
	// exercising the actual atomic writer's precommit failure.
	s.Probe = func(context.Context, int) (bool, error) { return true, os.Chmod(root, 0500) }
	_, err := s.Reserve(context.Background(), dir, "b")
	os.Chmod(root, 0700)
	if err == nil {
		t.Fatal("save succeeded")
	}
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("failed save destroyed bookings")
	}
	s.Probe = nil
	if err := s.Config.Coordinator.WithLock(context.Background(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestRealTCPProbeAndOccupiedReuse(t *testing.T) {
	for _, network := range []string{"tcp4", "tcp6"} {
		t.Run(network, func(t *testing.T) {
			host := "127.0.0.1:0"
			if network == "tcp6" {
				host = "[::1]:0"
			}
			l, err := net.Listen(network, host)
			if errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EPROTONOSUPPORT) || errors.Is(err, syscall.EADDRNOTAVAIL) {
				t.Skipf("family unavailable: %v", err)
			}
			if err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			p := l.Addr().(*net.TCPAddr).Port
			available, err := Available(context.Background(), p)
			if err != nil || available {
				t.Fatal(available, err)
			}
			s, dir := fixture(t, ports.Range{Start: p, End: p})
			s.Probe = nil
			if _, err := s.Reserve(context.Background(), dir, "new"); !errors.Is(err, ErrExhausted) {
				t.Fatal(err)
			}
			if _, err := os.Stat(filepath.Join(s.Config.Coordinator.Dir, File)); !os.IsNotExist(err) {
				t.Fatal("exhaustion saved", err)
			}
			// Seed a valid pre-existing booking, then verify listener presence has no
			// effect on reuse and the registry is not rewritten.
			canonical, err := Directory(dir)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := encode(Registry{canonical: {"server": p}})
			path := filepath.Join(s.Config.Coordinator.Dir, File)
			os.WriteFile(path, data, 0600)
			if got := reserve(t, s, dir, "server"); got != p {
				t.Fatal(got)
			}
			if !bytes.Equal(data, contents(t, path)) {
				t.Fatal("reuse rewrote file")
			}
		})
	}
}

func TestSelectionUsesGlobalRegistryAndDoesNotWriteConfigOrPointer(t *testing.T) {
	s, dir := fixture(t, ports.Range{Start: 45000, End: 45000})
	root := s.Config.Coordinator.Dir
	custom := t.TempDir()
	location := config.Location{Coordinator: s.Config.Coordinator}
	if _, err := location.Change(context.Background(), custom); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Config.Mutate(context.Background(), func(p *ports.Pool) (int, error) { return p.Apply([]ports.Range{{Start: 45001, End: 45001}}, true), nil }); err != nil {
		t.Fatal(err)
	}
	// Saved choice is read again at reservation time; no store caches it.
	pointer := contents(t, filepath.Join(root, "location.json"))
	cfg := contents(t, filepath.Join(custom, "config.json"))
	if p := reserve(t, s, dir, "server"); p != 45001 {
		t.Fatal(p)
	}
	s.Config.Location = &config.Location{Flag: root, FlagSet: true, Env: "relative ignored", EnvSet: true}
	if p := reserve(t, s, dir, "default"); p != 45000 {
		t.Fatal(p)
	}
	if p := reserve(t, s, dir, "server"); p != 45001 {
		t.Fatal(p)
	}
	if !bytes.Equal(pointer, contents(t, filepath.Join(root, "location.json"))) || !bytes.Equal(cfg, contents(t, filepath.Join(custom, "config.json"))) {
		t.Fatal("reserve changed config/pointer")
	}
	for _, name := range []string{File, "state.lock"} {
		if _, err := os.Stat(filepath.Join(custom, name)); !os.IsNotExist(err) {
			t.Fatal("split global state", err)
		}
	}
	for _, name := range []string{File, strings.ToUpper(File), filepath.Join(File, "child")} {
		if err := location.ValidateSelection(filepath.Join(root, name)); err == nil {
			t.Fatal("accepted registry as config directory")
		}
	}
	// Confirm the precise nested public document shape.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(contents(t, filepath.Join(root, File)), &doc); err != nil || doc["bookings"] == nil {
		t.Fatal(doc, err)
	}
}

func TestReserveBlocksConfigAndPointerMutations(t *testing.T) {
	s, dir := fixture(t, ports.Range{Start: 46000, End: 46000})
	entered, release := make(chan struct{}), make(chan struct{})
	s.Probe = func(context.Context, int) (bool, error) { close(entered); <-release; return true, nil }
	done := make(chan error, 1)
	go func() { _, err := s.Reserve(context.Background(), dir, "a"); done <- err }()
	<-entered
	if _, _, err := s.Config.Mutate(context.Background(), func(*ports.Pool) (int, error) { t.Error("config callback bypassed reservation lock"); return 0, nil }); err == nil {
		t.Error("config did not wait")
	}
	l := config.Location{Coordinator: s.Config.Coordinator}
	if _, err := l.Change(context.Background(), t.TempDir()); err == nil {
		t.Error("pointer did not wait")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Config.Coordinator.Dir, "location.json")); !os.IsNotExist(err) {
		t.Fatal("pointer mutation committed during reservation", err)
	}
}

func TestCancellationDuringProbeDoesNotSave(t *testing.T) {
	s, dir := fixture(t, ports.Range{Start: 47000, End: 47000})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Probe = func(context.Context, int) (bool, error) { cancel(); return true, nil }
	if p, err := s.Reserve(ctx, dir, "a"); p != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal(p, err)
	}
	if _, err := os.Stat(filepath.Join(s.Config.Coordinator.Dir, File)); !os.IsNotExist(err) {
		t.Fatal("cancelled reservation saved", err)
	}
}

func TestCaseAliasesFollowFilesystemIdentity(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "MixedCase")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "mixedcase")
	if _, err := os.Stat(alias); os.IsNotExist(err) {
		t.Skip("case-sensitive filesystem")
	} else if err != nil {
		t.Fatal(err)
	}
	s, _ := fixture(t, ports.Range{Start: 48000, End: 48002})
	if a, b := reserve(t, s, dir, "server"), reserve(t, s, alias, "server"); a != b {
		t.Fatalf("same filesystem directory has duplicate bookings: %d != %d", a, b)
	}
}

func TestOwnerKeysPreserveCaseAndDoNotAdoptChangedSymlinks(t *testing.T) {
	s, dir := fixture(t, ports.Range{Start: 49000, End: 49005})
	old := filepath.Join(t.TempDir(), "old")
	if err := os.Mkdir(old, 0700); err != nil {
		t.Fatal(err)
	}
	first := reserve(t, s, old, "server")
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dir, old); err != nil {
		t.Fatal(err)
	}
	second := reserve(t, s, dir, "server")
	if first == second {
		t.Fatal("adopted vanished owner's booking")
	}
	// If both case variants exist as separate directories, preserve both keys.
	parent := t.TempDir()
	upper := filepath.Join(parent, "Owner")
	lower := filepath.Join(parent, "owner")
	if err := os.Mkdir(upper, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lower, 0700); os.IsExist(err) {
		return
	} else if err != nil {
		t.Fatal(err)
	}
	if a, b := reserve(t, s, upper, "server"), reserve(t, s, lower, "server"); a == b {
		t.Fatal("case-sensitive owners collapsed")
	}
}

func TestSymlinkCannotIntroduceLossyOwnerIdentity(t *testing.T) {
	target := filepath.Join(t.TempDir(), string([]byte{'b', 255}))
	if err := os.Mkdir(target, 0700); err != nil {
		if errors.Is(err, syscall.EILSEQ) || errors.Is(err, syscall.EINVAL) {
			t.Skip("filesystem rejects invalid UTF-8 filenames")
		}
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "valid-link")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := Directory(alias); err == nil {
		t.Fatal("symlink target bypassed Unicode identity validation")
	}
}

func TestAmbiguousPhysicalOwnersFailClosed(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "MixedCase")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(parent, "mixedcase")
	if _, err := os.Stat(alias); os.IsNotExist(err) {
		t.Skip("case-sensitive filesystem")
	}
	canonical, err := Directory(dir)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Directory(alias)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := fixture(t)
	data, err := encode(Registry{canonical: {"server": 48000}, other: {"server": 48001}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Config.Coordinator.Dir, File)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reserve(context.Background(), dir, "server"); err == nil {
		t.Fatal("accepted ambiguous physical owner")
	}
	if !bytes.Equal(data, contents(t, path)) {
		t.Fatal("ambiguous registry changed")
	}
}

func TestMissingConfigAllocatesLowestDefaultAndSkipsBookedBusy(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	s := Store{Config: config.Store{Coordinator: state.Coordinator{Dir: root}}}
	owner := t.TempDir()
	var probed []int
	s.Probe = func(_ context.Context, p int) (bool, error) { probed = append(probed, p); return p != 49152, nil }
	if p := reserve(t, s, owner, "default"); p != 49153 {
		t.Fatal(p)
	}
	if p := reserve(t, s, t.TempDir(), "default"); p != 49154 {
		t.Fatal(p)
	}
	if fmt.Sprint(probed) != "[49152 49153 49152 49154]" {
		t.Fatal(probed)
	}
	if _, err := os.Stat(filepath.Join(root, "config.json")); !os.IsNotExist(err) {
		t.Fatal("reserve wrote config", err)
	}
}

func TestMissingConfigDefaultIncludesLastTCPPort(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	s := Store{Config: config.Store{Coordinator: state.Coordinator{Dir: root}}}
	probes := 0
	s.Probe = func(_ context.Context, p int) (bool, error) {
		if p < 49152 || p > 65535 {
			t.Fatalf("outside default pool: %d", p)
		}
		probes++
		return p == 65535, nil
	}
	if port := reserve(t, s, t.TempDir(), "last"); port != 65535 || probes != 16384 {
		t.Fatal(port, probes)
	}
	if _, err := os.Stat(filepath.Join(root, "config.json")); !os.IsNotExist(err) {
		t.Fatal("reserve persisted defaults", err)
	}
}
