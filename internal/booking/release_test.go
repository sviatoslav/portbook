package booking

import (
	"bytes"
	"context"
	"errors"
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
)

func seedRelease(t *testing.T, s Store, r Registry) string {
	t.Helper()
	data, err := encode(r)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Config.Coordinator.Dir, File)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReleasePreservesOtherBookingsAndIgnoresConfig(t *testing.T) {
	s, dir := fixture(t)
	dir, _ = Directory(dir)
	other, _ := Directory(t.TempDir())
	path := seedRelease(t, s, Registry{dir: {"default": 45000, "server": 45001}, other: {"default": 45002}})
	// Neither corrupt config nor the corrupt saved pointer is needed for release.
	os.WriteFile(filepath.Join(s.Config.Coordinator.Dir, "config.json"), []byte("bad"), 0600)
	os.WriteFile(filepath.Join(s.Config.Coordinator.Dir, "location.json"), []byte("bad"), 0600)
	s.Config.Location = &config.Location{EnvSet: true, Env: "relative"}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	res, err := s.Release(context.Background(), alias, "default")
	if err != nil || !res.Released || res.Port == nil || *res.Port != 45000 || res.Directory != dir {
		t.Fatal(res, err)
	}
	r, err := s.read()
	if err != nil || len(r) != 2 || len(r[dir]) != 1 || r[dir]["server"] != 45001 || r[other]["default"] != 45002 {
		t.Fatal(r, err)
	}
	before := contents(t, path)
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Probe = func(context.Context, int) (bool, error) { t.Fatal("missing booking probed"); return false, nil }
	res, err = s.Release(context.Background(), dir, "default")
	if !errors.Is(err, ErrNotFound) || res.Released || res.Port != nil || !bytes.Equal(before, contents(t, path)) {
		t.Fatal(res, err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil || !os.SameFile(beforeInfo, afterInfo) || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatal("no-op replaced or wrote registry", err)
	}
	s.Probe = func(context.Context, int) (bool, error) { return true, nil }
	if _, err := s.Release(context.Background(), dir, "server"); err != nil {
		t.Fatal(err)
	}
	r, _ = s.read()
	if _, exists := r[dir]; exists || r[other]["default"] != 45002 {
		t.Fatal(r)
	}
}

func TestReleaseNoopAndFailuresPreserveRegistry(t *testing.T) {
	s, dir := fixture(t)
	dir, _ = Directory(dir)
	res, err := s.Release(context.Background(), dir, "default")
	if !errors.Is(err, ErrNotFound) || res.Released || res.Port != nil {
		t.Fatal(res, err)
	}
	path := filepath.Join(s.Config.Coordinator.Dir, File)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("created missing registry", err)
	}
	seedRelease(t, s, Registry{dir: {"default": 45000}})
	before := contents(t, path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Release(ctx, dir, "default"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("cancel changed registry")
	}

	lock := flock.New(filepath.Join(s.Config.Coordinator.Dir, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Release(context.Background(), dir, "default"); err == nil {
		t.Error("lock bypassed")
	}
	lock.Close()
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("lock error changed registry")
	}
	for _, bad := range []string{"bad", `{"version":2,"bookings":{}}`, `{"version":1,"bookings":{"/gone":{"x":1,"x":2}}}`} {
		os.WriteFile(path, []byte(bad), 0600)
		for i := 0; i < 2; i++ {
			if _, err := s.Release(context.Background(), dir, "default"); err == nil {
				t.Fatal("corruption accepted")
			}
			if string(contents(t, path)) != bad {
				t.Fatal("evidence changed")
			}
		}
	}
}

func TestReleaseRealOccupiedTCPFamilies(t *testing.T) {
	for _, network := range []string{"tcp4", "tcp6"} {
		t.Run(network, func(t *testing.T) {
			host := "127.0.0.1:0"
			if network == "tcp6" {
				host = "[::1]:0"
			}
			l, err := net.Listen(network, host)
			if err != nil {
				if network == "tcp6" && (errors.Is(err, syscall.EAFNOSUPPORT) || errors.Is(err, syscall.EPROTONOSUPPORT) || errors.Is(err, syscall.EADDRNOTAVAIL)) {
					t.Skipf("IPv6 loopback unavailable: %v", err)
				}
				t.Fatal(err)
			}
			defer l.Close()
			port := l.Addr().(*net.TCPAddr).Port
			s, dir := fixture(t, ports.Range{Start: port, End: port})
			dir, _ = Directory(dir)
			s.Probe = nil
			path := seedRelease(t, s, Registry{dir: {"default": port}})
			if res, err := s.Release(context.Background(), dir, "default"); err != nil || !res.Released {
				t.Fatal(res, err)
			}
			if r, err := s.read(); err != nil || len(r) != 0 {
				t.Fatal(r, err, string(contents(t, path)))
			}
			connection, err := net.DialTimeout(network, l.Addr().String(), time.Second)
			if err != nil {
				t.Fatal("release stopped listener", err)
			}
			connection.Close()
			if p, err := s.Reserve(context.Background(), dir, "new"); p != 0 || !errors.Is(err, ErrExhausted) {
				t.Fatal("reused released busy port", p, err)
			}
			if available, err := Available(context.Background(), port); err != nil || available {
				t.Fatal("listener lost", available, err)
			}

		})
	}
}

func TestReleaseSerializesReserveAndConfig(t *testing.T) {
	s, dir := fixture(t, ports.Range{Start: 45000, End: 45000})
	dir, _ = Directory(dir)
	path := seedRelease(t, s, Registry{dir: {"default": 45000}})
	lock := flock.New(filepath.Join(s.Config.Coordinator.Dir, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	done := make(chan error, 1)
	// Give this release a longer wait than the competitors' deliberate timeouts.
	waiting := s
	waiting.Config.Coordinator.LockTimeout = time.Second
	go func() { _, err := waiting.Release(context.Background(), dir, "default"); done <- err }()
	select {
	case err := <-done:
		t.Fatal("release bypassed shared lock", err)
	case <-time.After(10 * time.Millisecond):
	}

	competitor := s
	competitor.Probe = func(context.Context, int) (bool, error) { return true, nil }
	if _, err := competitor.Reserve(context.Background(), dir, "other"); err == nil {
		t.Error("reserve bypassed shared lock")
	}
	if _, _, err := s.Config.Mutate(context.Background(), func(*ports.Pool) (int, error) { t.Error("config bypassed shared lock"); return 0, nil }); err == nil {
		t.Error("config did not wait")
	}
	lock.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if r, err := s.read(); err != nil || len(r) != 0 {
		t.Fatal(r, err, string(contents(t, path)))
	}
	if port := reserve(t, competitor, dir, "other"); port != 45000 {
		t.Fatal(port)
	}
}

func TestReleaseOwnerValidation(t *testing.T) {
	s, dir := fixture(t)
	dir, _ = Directory(dir)
	path := seedRelease(t, s, Registry{dir: {"default": 45000}})
	for _, owner := range []string{"", "relative", filepath.Join(dir, "absent"), path} {
		if _, err := s.Release(context.Background(), owner, "default"); err == nil {
			t.Fatal("invalid directory", owner)
		}
	}
	if _, err := s.Release(context.Background(), dir, ""); err == nil {
		t.Fatal("empty service accepted")
	}

}

func TestReleaseRegistryAccessAndCaseAliases(t *testing.T) {
	s, dir := fixture(t)
	path := filepath.Join(s.Config.Coordinator.Dir, File)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Release(context.Background(), dir, "default"); err == nil {
		t.Fatal("followed registry symlink")
	}
	if string(contents(t, target)) != "evidence" {
		t.Fatal("symlink target changed")
	}
	os.Remove(path)
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Release(context.Background(), dir, "default"); err == nil {
		t.Fatal("accepted registry directory")
	}
	os.Remove(path)
	dir, _ = Directory(dir)
	seedRelease(t, s, Registry{dir: {"default": 45000}})
	if err := os.Chmod(path, 0000); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		if _, err := s.Release(context.Background(), dir, "default"); err == nil {
			t.Fatal("accepted unreadable registry")
		}
	}
	os.Chmod(path, 0600)
	parent := t.TempDir()
	upper := filepath.Join(parent, "MixedCase")
	lower := filepath.Join(parent, "mixedcase")
	if err := os.Mkdir(upper, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lower); os.IsNotExist(err) {
		t.Skip("case-sensitive filesystem")
	} else if err != nil {
		t.Fatal(err)
	}
	upper, _ = Directory(upper)
	seedRelease(t, s, Registry{upper: {"default": 45000}})
	res, err := s.Release(context.Background(), lower, "default")
	if err != nil || !res.Released || res.Directory != upper {
		t.Fatal(res, err)
	}
}

func TestReleaseDoesNotAdoptReplacedOwnerSymlink(t *testing.T) {
	s, current := fixture(t)
	old := filepath.Join(t.TempDir(), "old")
	if err := os.Mkdir(old, 0700); err != nil {
		t.Fatal(err)
	}
	old, err := Directory(old)
	if err != nil {
		t.Fatal(err)
	}
	path := seedRelease(t, s, Registry{old: {"default": 45000}})
	before := contents(t, path)
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Release(context.Background(), old, "default"); err == nil {
		t.Fatal("unavailable owner silently fell back")
	}
	if err := os.Symlink(current, old); err != nil {
		t.Fatal(err)
	}
	s.Probe = func(context.Context, int) (bool, error) { t.Fatal("adopted replaced owner booking"); return true, nil }
	res, err := s.Release(context.Background(), old, "default")
	if !errors.Is(err, ErrNotFound) || res.Released || res.Port != nil || !bytes.Equal(before, contents(t, path)) {
		t.Fatal(res, err)
	}
}

func TestReleaseCancellationPreservesSnapshotAndUnlocks(t *testing.T) {
	s, dir := fixture(t)
	dir, _ = Directory(dir)
	emptyOwner := filepath.Join(t.TempDir(), "missing-empty-owner")
	path := seedRelease(t, s, Registry{dir: {"default": 45000}, emptyOwner: {}})
	before := contents(t, path)
	lock := flock.New(filepath.Join(s.Config.Coordinator.Dir, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if res, err := s.Release(ctx, dir, "default"); !errors.Is(err, context.Canceled) || res.Released {
		t.Fatal(res, err)
	}
	lock.Unlock()
	lock.Close()
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("cancel changed snapshot")
	}
	if res, err := s.Release(context.Background(), dir, "default"); err != nil || !res.Released {
		t.Fatal("lock retained", res, err)
	}
	r, err := s.read()
	if empty, ok := r[emptyOwner]; err != nil || !ok || len(empty) != 0 || len(r) != 1 {
		t.Fatal("unrelated empty owner lost", r, err)
	}
}

func TestReleaseUnwritableStatePreservesRegistryAndUnlocks(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	s, dir := fixture(t)
	dir, err := Directory(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := seedRelease(t, s, Registry{dir: {"default": 45000, "other": 45001}})
	before := contents(t, path)
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	root := s.Config.Coordinator.Dir
	defer os.Chmod(root, 0700)
	if err := os.Chmod(root, 0500); err != nil {
		t.Fatal(err)
	}

	res, err := s.Release(context.Background(), dir, "default")
	if err == nil || !errors.Is(err, os.ErrPermission) || res.Released || res.Port != nil {
		t.Fatal(res, err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil || !os.SameFile(beforeInfo, afterInfo) || !bytes.Equal(before, contents(t, path)) {
		t.Fatal("failed save changed registry", err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".portbook-") {
			t.Fatal("temporary file leaked", entry.Name())
		}
	}
	s.Probe = func(context.Context, int) (bool, error) { return true, nil }
	if res, err := s.Release(context.Background(), dir, "default"); err != nil || !res.Released {
		t.Fatal("save error retained lock", res, err)
	}
	r, err := s.read()
	if err != nil || len(r) != 1 || len(r[dir]) != 1 || r[dir]["other"] != 45001 {
		t.Fatal(r, err)
	}
}
