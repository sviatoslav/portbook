package booking

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"github.com/sviatoslav/portbook/internal/state"
)

func cleanupFixture(t *testing.T, r Registry) Store {
	t.Helper()
	dir := t.TempDir()
	data, err := encode(r)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, File), data, 0600); err != nil {
		t.Fatal(err)
	}
	return Store{Coordinator: state.Coordinator{Dir: dir, LockTimeout: 30 * time.Millisecond}, Probe: func(context.Context, int) (bool, error) { return true, nil }}
}
func cleanupPath(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCleanupMixedSnapshotAndIdempotency(t *testing.T) {
	parent := cleanupPath(t)
	gone := filepath.Join(parent, "gone")
	existing := cleanupPath(t)
	s := cleanupFixture(t, Registry{gone: {"free": 41001, "busy": 41002, "unknown": 41003}, existing: {"idle": 41004}})
	s.Probe = func(_ context.Context, p int) (bool, error) {
		switch p {
		case 41002:
			return false, nil
		case 41003:
			return false, errors.New("uncertain")
		case 41004:
			t.Fatal("existing owner probed")
		}
		return true, nil
	}
	path := filepath.Join(s.Coordinator.Dir, File)
	before := contents(t, path)
	r, err := s.Cleanup(context.Background(), true)
	if err != nil || r.Removed != 0 || r.WouldRemove != 1 || r.Retained != 3 {
		t.Fatal(r, err)
	}
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("dry run rewrote registry")
	}
	if _, err := os.Lstat(filepath.Join(s.Coordinator.Dir, "state.lock")); !os.IsNotExist(err) {
		t.Fatal("dry run created lock", err)
	}
	r, err = s.Cleanup(context.Background(), false)
	if err != nil || r.Removed != 1 || r.WouldRemove != 0 || r.Retained != 3 {
		t.Fatal(r, err)
	}
	saved, err := s.read()
	if err != nil || len(saved[gone]) != 2 || saved[existing]["idle"] != 41004 {
		t.Fatal(saved, err)
	}
	before = contents(t, path)
	r, err = s.Cleanup(context.Background(), false)
	if err != nil || r.Removed != 0 || !bytes.Equal(before, contents(t, path)) {
		t.Fatal(r, err, "idempotency")
	}
}

func TestCleanupOwnerEvidence(t *testing.T) {
	parent := cleanupPath(t)
	file := filepath.Join(parent, "file")
	os.WriteFile(file, []byte("keep"), 0600)
	link := filepath.Join(parent, "link")
	os.Symlink(filepath.Join(parent, "absent"), link)
	liveLink := filepath.Join(parent, "live-link")
	os.Symlink(parent, liveLink)
	locked := filepath.Join(parent, "locked")
	os.Mkdir(locked, 0700)
	os.Chmod(locked, 0000)
	defer os.Chmod(locked, 0700)
	for _, tc := range []struct{ path, want string }{
		{parent, "owner_exists"}, {filepath.Join(parent, "gone"), "owner_missing"},
		{filepath.Join(parent, "absent", "child"), "owner_uncertain"}, {file, "owner_not_directory"},
		{filepath.Join(file, "child"), "owner_not_directory"}, {link, "owner_uncertain"},
		{liveLink, "owner_uncertain"}, {filepath.Join(liveLink, "gone"), "owner_uncertain"},
		{"/Volumes/Unavailable/owner", "owner_uncertain"}, {"/mnt/absent/owner", "owner_uncertain"},
	} {
		if got := cleanupOwner(tc.path); got != tc.want {
			t.Errorf("%s: %s want %s", tc.path, got, tc.want)
		}
	}
	if os.Geteuid() != 0 && cleanupOwner(filepath.Join(locked, "gone")) != "owner_uncertain" {
		t.Fatal("inaccessible parent established deletion")
	}
}

func TestCleanupMissingCorruptionCancellationAndLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	s := Store{Coordinator: state.Coordinator{Dir: root}}
	r, err := s.Cleanup(context.Background(), true)
	if err != nil || r.Rows == nil || len(r.Rows) != 0 {
		t.Fatal(r, err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatal("missing dry run wrote state", err)
	}
	gone := filepath.Join(cleanupPath(t), "gone")
	s = cleanupFixture(t, Registry{gone: {"x": 41001}})
	path := filepath.Join(s.Coordinator.Dir, File)
	before := contents(t, path)
	lock := flock.New(filepath.Join(s.Coordinator.Dir, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cleanup(context.Background(), false); err == nil {
		t.Fatal("ignored lock")
	}
	lock.Close()
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("lock failure changed registry")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.Probe = func(context.Context, int) (bool, error) { cancel(); return true, nil }
	if _, err := s.Cleanup(ctx, false); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !bytes.Equal(before, contents(t, path)) {
		t.Fatal("cancellation changed registry")
	}
	for _, bad := range []string{"broken", `{"version":2,"bookings":{}}`, `{"version":1,"bookings":{"/x":{"x":1,"x":2}}}`} {
		os.WriteFile(path, []byte(bad), 0600)
		for _, dry := range []bool{true, false, true, false} {
			if _, err := s.Cleanup(context.Background(), dry); err == nil {
				t.Fatal("accepted corruption")
			}
			if string(contents(t, path)) != bad {
				t.Fatal("changed evidence")
			}
		}
	}
}

func TestCleanupRechecksOwnerAfterProbeAndPreservesOnSaveFailure(t *testing.T) {
	gone := filepath.Join(cleanupPath(t), "gone")
	s := cleanupFixture(t, Registry{gone: {"x": 41001}})
	s.Probe = func(context.Context, int) (bool, error) { return true, os.Mkdir(gone, 0700) }
	r, err := s.Cleanup(context.Background(), false)
	if err != nil || r.Removed != 0 || r.Retained != 1 {
		t.Fatal(r, err)
	}
	os.Remove(gone)
	// Replacing the registry with a directory during the probe makes atomic rename
	// fail, exercising pre-commit failure without weakening production persistence.
	path := filepath.Join(s.Coordinator.Dir, File)
	before := contents(t, path)
	s.Probe = func(context.Context, int) (bool, error) {
		if err := os.Rename(path, path+".original"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		return true, nil
	}
	if _, err := s.Cleanup(context.Background(), false); err == nil {
		t.Fatal("ignored save failure")
	}
	if !bytes.Equal(before, contents(t, path+".original")) {
		t.Fatal("lost original")
	}
}

func TestCleanupPreservesEmptyOwnersAndDoesNotRewriteNoop(t *testing.T) {
	parent := cleanupPath(t)
	gone := filepath.Join(parent, "gone")
	empty := filepath.Join(parent, "empty")
	s := cleanupFixture(t, Registry{gone: {"x": 41001}, empty: {}})
	if r, err := s.Cleanup(context.Background(), false); err != nil || r.Removed != 1 {
		t.Fatal(r, err)
	}
	saved, err := s.read()
	if err != nil {
		t.Fatal(err)
	}
	if services, ok := saved[empty]; !ok || len(services) != 0 {
		t.Fatal("removed preexisting empty owner", saved)
	}
	if _, ok := saved[gone]; ok {
		t.Fatal("retained newly emptied owner")
	}
	path := filepath.Join(s.Coordinator.Dir, File)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Cleanup(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || before.ModTime() != after.ModTime() {
		t.Fatal("no-op replaced registry", err)
	}
	if _, err := (Store{}).Cleanup(context.Background(), true); err == nil {
		t.Fatal("accepted relative state root")
	}
}

func TestCleanupRetainsOwnerRecreatedByLaterProbe(t *testing.T) {
	for _, dry := range []bool{true, false} {
		t.Run(map[bool]string{true: "dry", false: "save"}[dry], func(t *testing.T) {
			parent := cleanupPath(t)
			gone := filepath.Join(parent, "gone")
			s := cleanupFixture(t, Registry{gone: {"a": 41001, "b": 41002}})
			path := filepath.Join(s.Coordinator.Dir, File)
			before := contents(t, path)
			s.Probe = func(_ context.Context, p int) (bool, error) {
				if p == 41002 {
					if err := os.Mkdir(gone, 0700); err != nil {
						t.Fatal(err)
					}
				}
				return true, nil
			}
			r, err := s.Cleanup(context.Background(), dry)
			if err != nil || r.Removed != 0 || r.WouldRemove != 0 || r.Retained != 2 {
				t.Fatal(r, err)
			}
			if !bytes.Equal(before, contents(t, path)) {
				t.Fatal("recreated owner lost a prior booking")
			}
			for _, row := range r.Rows {
				if row.Reason != "owner_exists" || row.Action != "retained" {
					t.Fatal(row)
				}
			}
		})
	}
}

func TestCleanupRetainsChangedParentIdentity(t *testing.T) {
	for _, dry := range []bool{true, false} {
		t.Run(map[bool]string{true: "dry", false: "save"}[dry], func(t *testing.T) {
			base := cleanupPath(t)
			parent := filepath.Join(base, "parent")
			if err := os.Mkdir(parent, 0700); err != nil {
				t.Fatal(err)
			}
			gone := filepath.Join(parent, "gone")
			s := cleanupFixture(t, Registry{gone: {"x": 41001}})
			path := filepath.Join(s.Coordinator.Dir, File)
			before := contents(t, path)
			s.Probe = func(context.Context, int) (bool, error) {
				if err := os.Rename(parent, parent+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(parent, 0700); err != nil {
					t.Fatal(err)
				}
				return true, nil
			}
			r, err := s.Cleanup(context.Background(), dry)
			if err != nil || r.Removed != 0 || r.WouldRemove != 0 || r.Retained != 1 || r.Rows[0].Reason != "owner_uncertain" {
				t.Fatal(r, err)
			}
			if !bytes.Equal(before, contents(t, path)) {
				t.Fatal("changed parent caused removal")
			}
		})
	}
}

func TestCleanupRevalidatesEarlierOwnerBeforeSavingOtherRemovals(t *testing.T) {
	parent := cleanupPath(t)
	first := filepath.Join(parent, "a")
	last := filepath.Join(parent, "z")
	s := cleanupFixture(t, Registry{first: {"x": 41001}, last: {"y": 41002}})
	s.Probe = func(_ context.Context, p int) (bool, error) {
		if p == 41002 {
			if err := os.Mkdir(first, 0700); err != nil {
				t.Fatal(err)
			}
		}
		return true, nil
	}
	r, err := s.Cleanup(context.Background(), false)
	if err != nil || r.Removed != 1 || r.Retained != 1 {
		t.Fatal(r, err)
	}
	saved, err := s.read()
	if err != nil || len(saved) != 1 || saved[first]["x"] != 41001 {
		t.Fatal("final save dropped recreated owner", saved, err)
	}
}

func TestCleanupCancellationAfterEarlierProposalReleasesLockAndPreservesSnapshot(t *testing.T) {
	for _, dry := range []bool{true, false} {
		t.Run(map[bool]string{true: "dry", false: "save"}[dry], func(t *testing.T) {
			gone := filepath.Join(cleanupPath(t), "gone")
			s := cleanupFixture(t, Registry{gone: {"a": 41001, "b": 41002}})
			path := filepath.Join(s.Coordinator.Dir, File)
			before := contents(t, path)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			entered := make(chan struct{})
			s.Probe = func(ctx context.Context, p int) (bool, error) {
				if p == 41002 {
					close(entered)
					<-ctx.Done()
				}
				// Even a slow callback returning success after cancellation must not commit
				// an earlier proposal or the just-finished probe's proposal.
				return true, nil
			}
			done := make(chan error, 1)
			go func() { _, err := s.Cleanup(ctx, dry); done <- err }()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("second probe never entered")
			}
			lockPath := filepath.Join(s.Coordinator.Dir, "state.lock")
			if !dry {
				lock := flock.New(lockPath)
				held, err := lock.TryLock()
				if held || err != nil {
					lock.Close()
					t.Fatal("cleanup did not retain lock around probe", held, err)
				}
				if err := lock.Close(); err != nil {
					t.Fatal(err)
				}
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("cleanup did not return after cancelled probe")
			}
			if !bytes.Equal(before, contents(t, path)) {
				t.Fatal("cancellation saved a partial snapshot")
			}
			if dry {
				if _, err := os.Lstat(lockPath); !os.IsNotExist(err) {
					t.Fatal("cancelled dry run created lock", err)
				}
			} else {
				lock := flock.New(lockPath)
				defer lock.Close()
				held, err := lock.TryLock()
				if !held || err != nil {
					t.Fatal("cleanup leaked lock after cancellation", held, err)
				}
			}
		})
	}
}
