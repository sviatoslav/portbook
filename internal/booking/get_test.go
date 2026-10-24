package booking

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofrs/flock"
	"github.com/sviatoslav/portbook/internal/ports"
	"github.com/sviatoslav/portbook/internal/state"
)

func get(t *testing.T, s Store, dir, service string) int {
	t.Helper()
	p, err := s.Get(dir, service)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestGetSnapshotIgnoresConfigProbeAndLock(t *testing.T) {
	s, dir := fixture(t)
	canonical, _ := Directory(dir)
	path := seedRelease(t, s, Registry{canonical: {"default": 45000, "api": 45001}})
	before := contents(t, path)
	info, _ := os.Stat(path)
	os.WriteFile(filepath.Join(s.Coordinator.Dir, "config.json"), []byte("broken"), 0600)
	os.WriteFile(filepath.Join(s.Coordinator.Dir, "location.json"), []byte("broken"), 0600)
	s.ReadPool = func() (ports.Pool, error) { t.Fatal("get read the pool"); return ports.Pool{}, nil }
	s.Probe = func(context.Context, int) (bool, error) { t.Fatal("get probed"); return false, nil }
	lock := flock.New(filepath.Join(s.Coordinator.Dir, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if p := get(t, s, alias, "api"); p != 45001 {
		t.Fatal(p)
	}
	if p := get(t, s, dir, "default"); p != 45000 {
		t.Fatal(p)
	}
	if p, err := s.Get(dir, "absent"); p != 0 || !errors.Is(err, ErrNotFound) {
		t.Fatal(p, err)
	}
	after, _ := os.Stat(path)
	if !bytes.Equal(before, contents(t, path)) || !os.SameFile(info, after) || !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("get changed registry")
	}
}

func TestGetMissingAndCorruptStatePreservesEvidence(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(t.TempDir(), "uncreated")
	s := Store{Coordinator: state.Coordinator{Dir: root}}
	if p, err := s.Get(dir, "default"); p != 0 || !errors.Is(err, ErrNotFound) {
		t.Fatal(p, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("get created state", err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, File)
	for _, data := range []string{"broken", `{"version":9,"bookings":{}}`, `{"version":1,"bookings":{"/x":{"a":1,"a":2}}}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if p, err := s.Get(dir, "default"); p != 0 || err == nil || errors.Is(err, ErrNotFound) {
			t.Fatal(p, err, data)
		}
		if string(contents(t, path)) != data {
			t.Fatal("changed corrupt state")
		}
	}
	for _, invalid := range []string{"", "relative", filepath.Join(dir, "missing")} {
		if _, err := s.Get(invalid, "default"); err == nil {
			t.Fatal(invalid)
		}
	}
	if _, err := s.Get(dir, ""); err == nil {
		t.Fatal("empty service")
	}
}

func TestDuplicateReservePreservesSnapshotWithoutProbeOrConfig(t *testing.T) {
	s, dir := fixture(t)
	canonical, _ := Directory(dir)
	path := seedRelease(t, s, Registry{canonical: {"default": 45000}})
	before := contents(t, path)
	info, _ := os.Stat(path)
	os.WriteFile(filepath.Join(s.Coordinator.Dir, "config.json"), []byte("broken"), 0600)
	s.ReadPool = func() (ports.Pool, error) { t.Fatal("duplicate read the pool"); return ports.Pool{}, nil }
	s.Probe = func(context.Context, int) (bool, error) { t.Fatal("duplicate probed"); return false, nil }
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{dir, alias} {
		if p, err := s.Reserve(context.Background(), owner, "default"); p != 0 || !errors.Is(err, ErrAlreadyExists) {
			t.Fatal(p, err)
		}
	}
	after, _ := os.Stat(path)
	if !bytes.Equal(before, contents(t, path)) || !os.SameFile(info, after) || !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("duplicate changed registry")
	}
}

func TestGetRejectsNonregularAndInaccessibleRegistry(t *testing.T) {
	for _, kind := range []string{"directory", "symlink", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			s, dir := fixture(t)
			path := filepath.Join(s.Coordinator.Dir, File)
			target := filepath.Join(t.TempDir(), "evidence")
			data := []byte(`{"version":1,"bookings":{}}`)
			if err := os.WriteFile(target, data, 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "unreadable":
				if os.Geteuid() == 0 {
					t.Skip("root can read mode 0000")
				}
				if err := os.WriteFile(path, data, 0000); err != nil {
					t.Fatal(err)
				}
				defer os.Chmod(path, 0600)
			}
			if p, err := s.Get(dir, "default"); p != 0 || err == nil || errors.Is(err, ErrNotFound) {
				t.Fatal(p, err)
			}
			if !bytes.Equal(contents(t, target), data) {
				t.Fatal("get changed evidence")
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "symlink" && info.Mode()&os.ModeSymlink == 0 {
				t.Fatal("replaced symlink")
			}
			if kind == "directory" && !info.IsDir() {
				t.Fatal("replaced directory")
			}
			if kind == "unreadable" && info.Mode().Perm() != 0 {
				t.Fatal("changed permissions")
			}
		})
	}
}
