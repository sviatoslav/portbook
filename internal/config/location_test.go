package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"portbook/internal/ports"
	"portbook/internal/state"
)

func testLocation(t *testing.T) Location {
	t.Helper()
	return Location{Coordinator: state.Coordinator{Dir: filepath.Join(t.TempDir(), "bootstrap"), LockTimeout: 40 * time.Millisecond}}
}

func TestLocationPrecedenceAndReadOnly(t *testing.T) {
	l := testLocation(t)
	root := l.Coordinator.Dir
	assert := func(want, source string) {
		t.Helper()
		got, err := l.Resolve()
		if err != nil || got != selection(want, source) {
			t.Fatalf("%+v %v", got, err)
		}
	}
	assert(root, "default")
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("discovery wrote: %v", err)
	}
	saved := filepath.Join(t.TempDir(), "saved")
	if _, err := l.Change(context.Background(), saved); err != nil {
		t.Fatal(err)
	}
	assert(saved, "saved")
	l.Env, l.EnvSet = t.TempDir(), true
	assert(l.Env, "environment")
	l.Flag, l.FlagSet = t.TempDir(), true
	assert(l.Flag, "flag")
	// The ignored environment and pointer must not be validated or opened.
	l.Env = "relative"
	if err := os.WriteFile(filepath.Join(root, LocationFile), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	assert(l.Flag, "flag")
	l.FlagSet = false
	l.Env = t.TempDir()
	assert(l.Env, "environment")
	l.EnvSet = false
	if _, err := l.Resolve(); err == nil {
		t.Fatal("accepted corrupt pointer")
	}
}

func TestLocationChangesPreserveConfigsAndMasking(t *testing.T) {
	l := testLocation(t)
	target := t.TempDir()
	l.Env, l.EnvSet = t.TempDir(), true
	for _, dir := range []string{target, l.Env} {
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("original evidence"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		got, err := l.Change(context.Background(), target)
		if err != nil || got.Source != "environment" || got.Directory != l.Env {
			t.Fatalf("%+v %v", got, err)
		}
	}
	l.EnvSet = false
	got, err := l.Resolve()
	if err != nil || got.Directory != target {
		t.Fatalf("%+v %v", got, err)
	}
	for i := 0; i < 2; i++ {
		got, err := l.Change(context.Background(), "")
		if err != nil || got.Source != "default" {
			t.Fatalf("%+v %v", got, err)
		}
	}
	for _, dir := range []string{target, l.Env} {
		got, err := os.ReadFile(filepath.Join(dir, "config.json"))
		if err != nil || string(got) != "original evidence" {
			t.Fatalf("config modified: %s %v", got, err)
		}
	}
}

func TestBadPointersPreservedAndExplicitlyRepairable(t *testing.T) {
	for _, bad := range []string{"{", `null`, `{"version":2,"directory":"/tmp"}`, `{"version":1,"directory":"relative"}`, `{"version":1,"directory":"/tmp","DIRECTORY":"/other"}`, `{"version":1,"directory":"/tmp","dir\u0065ctory":"/other"}`, `{"version":1,"directory":"/tmp","other":1}`, `{"version":1,"directory":"/tmp"} {}`} {
		t.Run(bad, func(t *testing.T) {
			l := testLocation(t)
			root := l.Coordinator.Dir
			if err := os.MkdirAll(root, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, LocationFile)
			if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
				t.Fatal(err)
			}
			s := Store{Coordinator: l.Coordinator, Location: &l}
			if _, err := s.Read(); err == nil {
				t.Fatal("bad pointer read")
			}
			if _, _, err := s.Mutate(context.Background(), func(*ports.Pool) (int, error) { t.Fatal("callback ran"); return 0, nil }); err == nil {
				t.Fatal("bad pointer mutation")
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, []byte(bad)) {
				t.Fatal("evidence changed", err)
			}
			if _, err := l.Change(context.Background(), t.TempDir()); err != nil {
				t.Fatal("explicit set repair", err)
			}
			if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := l.Change(context.Background(), ""); err != nil {
				t.Fatal("explicit reset repair", err)
			}
		})
	}
}

func TestInvalidLocationNoMutation(t *testing.T) {
	l := testLocation(t)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"relative", file, filepath.Join(file, "child"), ""} {
		if err := ValidateDirectory(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
		if path != "" {
			if _, err := l.Change(context.Background(), path); err == nil {
				t.Fatal("invalid set succeeded")
			}
		}
	}
	if _, err := os.Stat(l.Coordinator.Dir); !os.IsNotExist(err) {
		t.Fatal("invalid selection wrote bootstrap", err)
	}
}

func TestRelocatedMutationsShareLockAndResolveAfterAcquisition(t *testing.T) {
	l := testLocation(t)
	first, second := t.TempDir(), t.TempDir()
	if _, err := l.Change(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	store := Store{Coordinator: l.Coordinator, Location: &l}
	lock := flock.New(filepath.Join(l.Coordinator.Dir, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	for _, dir := range []string{first, second} {
		override := l
		override.Flag, override.FlagSet = dir, true
		s := Store{Coordinator: l.Coordinator, Location: &override}
		if _, _, err := s.Mutate(context.Background(), func(*ports.Pool) (int, error) { t.Fatal("bypassed lock"); return 0, nil }); err == nil {
			t.Fatal("ignored shared lock")
		}
	}
	if _, err := l.Change(context.Background(), second); err == nil {
		t.Fatal("path set bypassed lock")
	}
	if _, err := l.Change(context.Background(), ""); err == nil {
		t.Fatal("path reset bypassed lock")
	}
	// Queue a mutation while the lock is held, then change the pointer before
	// releasing it. This simulates the serialized commit of a preceding switch.
	store.Coordinator.LockTimeout = time.Second
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, _, err := store.Mutate(context.Background(), func(p *ports.Pool) (int, error) { return p.Apply([]ports.Range{{Start: 42, End: 42}}, true), nil })
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("queued mutation bypassed lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	data := []byte(`{"version":1,"directory":` + `"` + second + `"}`)
	if err := state.AtomicWrite(l.Coordinator.Dir, LocationFile, data); err != nil {
		t.Fatal(err)
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(first, "config.json")); !os.IsNotExist(err) {
		t.Fatal("used stale selection", err)
	}
	p, err := readConfig(second)
	if err != nil || p.Count() != 1 {
		t.Fatal(p, err)
	}
}

func TestPointerFileTypesPermissionsAndAtomicFailure(t *testing.T) {
	l := testLocation(t)
	root := l.Coordinator.Dir
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, LocationFile)
	target := filepath.Join(t.TempDir(), "evidence")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Resolve(); err == nil {
		t.Fatal("followed pointer symlink")
	}
	if _, err := l.Change(context.Background(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "keep" {
		t.Fatal("changed symlink target", err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(path); err != nil {
		if _, err := l.Resolve(); err == nil {
			t.Fatal("read inaccessible pointer")
		}
		if _, _, err := (Store{Coordinator: l.Coordinator, Location: &l}).Mutate(context.Background(), func(*ports.Pool) (int, error) { t.Fatal("callback ran"); return 0, nil }); err == nil {
			t.Fatal("mutated inaccessible pointer")
		}
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Change(context.Background(), t.TempDir()); err == nil {
		t.Fatal("replaced pointer directory")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != LocationFile && entry.Name() != "state.lock" {
			t.Fatal("temporary file leaked", entry.Name())
		}
	}
	if _, err := l.Resolve(); err == nil {
		t.Fatal("accepted directory pointer")
	}
	if _, err := l.Change(context.Background(), ""); err == nil {
		t.Fatal("reset removed directory pointer")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		t.Fatal("reset changed directory pointer", err)
	}
}

func TestSelectedConfigCallbackFailure(t *testing.T) {
	l := testLocation(t)
	target := t.TempDir()
	if _, err := l.Change(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	s := Store{Coordinator: l.Coordinator, Location: &l}
	abort := errors.New("abort")
	if _, _, err := s.Mutate(context.Background(), func(p *ports.Pool) (int, error) { p.Apply([]ports.Range{{Start: 1, End: 1}}, true); return 1, abort }); !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "config.json")); !os.IsNotExist(err) {
		t.Fatal("callback error saved", err)
	}
}

func TestDanglingConfigDirectoryRejected(t *testing.T) {
	link := filepath.Join(t.TempDir(), "dangling")
	if err := os.Symlink(filepath.Join(t.TempDir(), "absent"), link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{link, filepath.Join(link, "child")} {
		if err := ValidateDirectory(path); err == nil {
			t.Fatalf("accepted dangling component %q", path)
		}
	}
}

func TestLocationValidationErrorPrecedesPointerSave(t *testing.T) {
	l := testLocation(t)
	if _, err := l.Change(context.Background(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(l.Coordinator.Dir, LocationFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Env, l.EnvSet = "relative", true
	for _, target := range []string{t.TempDir(), ""} {
		if _, err := l.Change(context.Background(), target); err == nil {
			t.Fatal("accepted invalid effective override")
		}
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("validation error changed pointer", err)
		}
	}
}

func TestStoreCoordinatorOwnsSelectionAndLockRoot(t *testing.T) {
	primary, other := testLocation(t), testLocation(t)
	target, ignoredTarget := t.TempDir(), t.TempDir()
	for _, tc := range []struct {
		location Location
		target   string
	}{{primary, target}, {other, ignoredTarget}} {
		if _, err := tc.location.Change(context.Background(), tc.target); err != nil {
			t.Fatal(err)
		}
	}
	// Existing coordinator-only construction must obey the saved pointer too.
	// Supplied selection options must never introduce a second bootstrap root.
	for _, options := range []*Location{nil, &other} {
		store := Store{Coordinator: primary.Coordinator, Location: options}
		_, _, err := store.Mutate(context.Background(), func(p *ports.Pool) (int, error) { return p.Apply([]ports.Range{{Start: 1234, End: 1234}}, true), nil })
		if err != nil {
			t.Fatal(err)
		}
		p, err := store.Read()
		if err != nil || p.Count() != 1 {
			t.Fatalf("store read disagrees with selected config: count=%d err=%v", p.Count(), err)
		}
	}
	p, err := readConfig(target)
	if err != nil || p.Count() != 1 {
		t.Fatalf("ignored authoritative saved selection: count=%d err=%v", p.Count(), err)
	}
	for _, dir := range []string{primary.Coordinator.Dir, other.Coordinator.Dir, ignoredTarget} {
		if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
			t.Fatalf("unexpected config in %s: %v", dir, err)
		}
	}
}

func TestUnreadablePointerIgnoredByOverrides(t *testing.T) {
	l := testLocation(t)
	if _, err := l.Change(context.Background(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(l.Coordinator.Dir, LocationFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("current user can read files with mode 000")
	}
	if _, err := l.Resolve(); err == nil {
		t.Fatal("unreadable saved pointer did not fail closed")
	}
	for _, source := range []string{"environment", "flag"} {
		override := l
		dir := t.TempDir()
		if source == "environment" {
			override.Env, override.EnvSet = dir, true
		} else {
			override.Flag, override.FlagSet = dir, true
		}
		got, err := override.Resolve()
		if err != nil || got != selection(dir, source) {
			t.Fatalf("irrelevant unreadable pointer blocks override: %+v %v", got, err)
		}
		s := Store{Coordinator: l.Coordinator, Location: &override}
		if _, _, err := s.Mutate(context.Background(), func(p *ports.Pool) (int, error) { return p.Apply([]ports.Range{{Start: 4321, End: 4321}}, true), nil }); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("override changed unreadable pointer", err)
	}
}

func TestConfigPathsMustRoundTripThroughJSON(t *testing.T) {
	l := testLocation(t)
	invalid := filepath.Join(t.TempDir(), "invalid-") + string([]byte{0xff})
	if err := ValidateDirectory(invalid); err == nil {
		t.Fatal("accepted path that JSON encoding would change")
	}
	if _, err := l.Change(context.Background(), invalid); err == nil {
		t.Fatal("persisted path that JSON encoding would change")
	}
	if _, err := os.Stat(l.Coordinator.Dir); !os.IsNotExist(err) {
		t.Fatal("invalid path created bootstrap state", err)
	}
}

func TestPointerUnicodeMustDecodeWithoutReplacement(t *testing.T) {
	for _, bad := range []string{
		`invalid-` + string([]byte{0xff}),
		`invalid-\ud800`,
		`invalid-\udc00`,
		`invalid-\ud800\u0041`,
	} {
		t.Run(fmt.Sprintf("%x", []byte(bad)), func(t *testing.T) {
			l := testLocation(t)
			prefix := strconv.Quote(filepath.ToSlash(t.TempDir()) + "/")
			raw := `{"version":1,"directory":` + strings.TrimSuffix(prefix, `"`) + bad + `"}`
			if err := os.MkdirAll(l.Coordinator.Dir, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(l.Coordinator.Dir, LocationFile)
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := l.Resolve(); err == nil {
				t.Fatal("accepted pointer that JSON decoding would repair")
			}
			s := Store{Coordinator: l.Coordinator}
			if _, _, err := s.Mutate(context.Background(), func(*ports.Pool) (int, error) { t.Fatal("callback ran with repaired path"); return 0, nil }); err == nil {
				t.Fatal("mutated through repaired path")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, []byte(raw)) {
				t.Fatal("changed pointer evidence", err)
			}
			l.Flag, l.FlagSet = t.TempDir(), true
			if _, err := l.Resolve(); err != nil {
				t.Fatal("irrelevant invalid pointer blocked flag", err)
			}
			if _, err := l.Change(context.Background(), t.TempDir()); err != nil {
				t.Fatal("explicit repair failed", err)
			}
		})
	}
}

func TestPointerUnicodeAndLiteralEscapesRemainSupported(t *testing.T) {
	for _, suffix := range []string{`\ud83d\ude00`, `replacement-\ufffd`, `literal-\\ud800`, `escaped-\/dir`} {
		l := testLocation(t)
		raw := `{"version":1,"directory":` + strconv.Quote(filepath.ToSlash(t.TempDir())+"/")
		raw = strings.TrimSuffix(raw, `"`) + suffix + `"}`
		if err := os.MkdirAll(l.Coordinator.Dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(l.Coordinator.Dir, LocationFile), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := l.Resolve()
		if err != nil || got.Source != "saved" {
			t.Fatalf("rejected valid Unicode/escape %s: %+v %v", raw, got, err)
		}
	}
}

func TestBootstrapFilesCannotBecomeConfigDirectories(t *testing.T) {
	l := testLocation(t)
	for _, name := range []string{LocationFile, "state.lock", "LOCATION.JSON", "bookings.json", "BOOKINGS.JSON"} {
		for _, tail := range []string{"", "nested"} {
			target := filepath.Join(l.Coordinator.Dir, name, tail)
			if _, err := l.Change(context.Background(), target); err == nil {
				t.Fatalf("saved reserved bootstrap path %s", target)
			}
			override := l
			override.Flag, override.FlagSet = target, true
			if _, err := override.Resolve(); err == nil {
				t.Fatalf("selected reserved bootstrap path %s", target)
			}
		}
	}
	if _, err := os.Stat(l.Coordinator.Dir); !os.IsNotExist(err) {
		t.Fatal("reserved path created bootstrap state", err)
	}
}

func TestBootstrapFileAliasesCannotBecomeConfigDirectories(t *testing.T) {
	l := testLocation(t)
	if err := os.MkdirAll(l.Coordinator.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(l.Coordinator.Dir, alias); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{LocationFile, "state.lock", "LOCATION.JSON", "bookings.json", "BOOKINGS.JSON"} {
		target := filepath.Join(alias, name, "nested")
		if _, err := l.Change(context.Background(), target); err == nil {
			t.Fatalf("saved bootstrap file alias %s", target)
		}
		override := l
		override.Env, override.EnvSet = target, true
		if _, err := override.Resolve(); err == nil {
			t.Fatalf("selected bootstrap file alias %s", target)
		}
	}
	// A temporary selection must not be invalidated by writing a saved choice.
	masked := l
	masked.Flag, masked.FlagSet = filepath.Join(alias, LocationFile), true
	if _, err := masked.Change(context.Background(), t.TempDir()); err == nil {
		t.Fatal("pointer write invalidated active config selection")
	}
	entries, err := os.ReadDir(l.Coordinator.Dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("reserved aliases created state: entries=%v err=%v", entries, err)
	}
}
