package booking

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gofrs/flock"
	"github.com/sviatoslav/portbook/internal/config"
)

func TestReleaseAllExactOwnerNoProbesAndSortedPorts(t *testing.T) {
	s, owner := fixture(t)
	owner, _ = Directory(owner)
	child := filepath.Join(owner, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	other, _ := Directory(t.TempDir())
	missing := filepath.Join(t.TempDir(), "missing")
	path := seedRelease(t, s, Registry{owner: {"z": 45002, "default": 45000, "api": 45001}, child: {"api": 45003}, other: {"api": 45004}, missing: {}})
	s.Probe = func(context.Context, int) (bool, error) {
		t.Fatal("release probed")
		return false, errors.New("unknown")
	}
	s.Config.Location = &config.Location{EnvSet: true, Env: "relative"}
	os.WriteFile(filepath.Join(s.Config.Coordinator.Dir, "config.json"), []byte("broken"), 0600)
	os.WriteFile(filepath.Join(s.Config.Coordinator.Dir, "location.json"), []byte("broken"), 0600)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(owner, alias); err != nil {
		t.Fatal(err)
	}
	res, err := s.ReleaseAll(context.Background(), alias)
	if err != nil || res.Directory != owner || res.Released != 3 || !reflect.DeepEqual(res.Ports, []int{45000, 45001, 45002}) {
		t.Fatal(res, err)
	}
	r, err := s.read()
	if err != nil || len(r) != 3 || r[child]["api"] != 45003 || r[other]["api"] != 45004 {
		t.Fatal(r, err)
	}
	if _, ok := r[missing]; !ok {
		t.Fatal("unrelated empty owner lost")
	}
	before, info := contents(t, path), mustStat(t, path)
	if _, err := s.ReleaseAll(context.Background(), alias); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	after := mustStat(t, path)
	if !bytes.Equal(before, contents(t, path)) || !os.SameFile(info, after) || !info.ModTime().Equal(after.ModTime()) {
		t.Fatal("missing release rewrote registry")
	}
}

func mustStat(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}

func TestReleaseAllEmptyOwnerPreserved(t *testing.T) {
	s, owner := fixture(t)
	owner, _ = Directory(owner)
	path := seedRelease(t, s, Registry{owner: {}})
	before := contents(t, path)
	if _, err := s.ReleaseAll(context.Background(), owner); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("empty owner changed")
	}
}

func TestSingleAndAllReleaseNeverProbe(t *testing.T) {
	for _, all := range []bool{false, true} {
		s, owner := fixture(t)
		owner, _ = Directory(owner)
		seedRelease(t, s, Registry{owner: {"default": 45000}})
		s.Probe = func(context.Context, int) (bool, error) {
			t.Fatal("release probed")
			return false, errors.New("unknown TCP error")
		}
		if all {
			if _, err := s.ReleaseAll(context.Background(), owner); err != nil {
				t.Fatal(err)
			}
		} else {
			if _, err := s.Release(context.Background(), owner, "default"); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestReleaseAllFailsClosedAndUnlocks(t *testing.T) {
	s, owner := fixture(t)
	owner, _ = Directory(owner)
	path := seedRelease(t, s, Registry{owner: {"default": 45000, "api": 45001}})
	before := contents(t, path)
	lock := flock.New(filepath.Join(s.Config.Coordinator.Dir, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReleaseAll(context.Background(), owner); err == nil {
		t.Fatal("bypassed lock")
	}
	lock.Close()
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("lock failure changed state")
	}
	for _, bad := range []string{"broken", `{"version":2,"bookings":{}}`, `{"version":1,"bookings":{"/gone":{"a":1,"a":2}}}`} {
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ReleaseAll(context.Background(), owner); err == nil || errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if string(contents(t, path)) != bad {
			t.Fatal("corruption destroyed")
		}
	}
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() != 0 {
		root := s.Config.Coordinator.Dir
		if err := os.Chmod(root, 0500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(root, 0700)
		if _, err := s.ReleaseAll(context.Background(), owner); !errors.Is(err, os.ErrPermission) {
			t.Fatal(err)
		}
		if !bytes.Equal(before, contents(t, path)) {
			t.Fatal("save failure changed registry")
		}
		if err := os.Chmod(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if res, err := s.ReleaseAll(context.Background(), owner); err != nil || res.Released != 2 {
		t.Fatal("error retained lock", res, err)
	}
}
