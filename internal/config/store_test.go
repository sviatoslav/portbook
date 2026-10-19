package config

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gofrs/flock"
	"portbook/internal/ports"
	"portbook/internal/state"
)

func TestMissingShowReadOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "absent")
	p, err := (Store{Coordinator: state.Coordinator{Dir: dir}}).Read()
	if err != nil || p.Count() != 0 {
		t.Fatalf("%d %v", p.Count(), err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("read created directory: %v", err)
	}
}

func TestFailClosed(t *testing.T) {
	for _, bad := range []string{``, `{`, `null`, `{ "version": 2, "ranges": [] }`, `{ "version": 1 }`, `{ "version": 1, "ranges": null }`, `{ "version": 1, "ranges": [{"start":0,"end":5}] }`, `{ "version": 1, "ranges": [], "other": 1 }`, `{ "version": 1, "ranges": [] } {}`,
		`{"version":1,"ranges":[{"start":50000,"end":50099}],"ranges":[]}`,
		`{"version":2,"version":1,"ranges":[]}`,
		`{"version":1,"version":1,"ranges":[]}`,
		`{"version":1,"ranges":[{"start":1,"end":2,"start":2}]}`,
		`{"version":1,"ranges":[{"start":1,"end":2,"end":1}]}`,
		`{"version":1,"ranges":[{"start":1,"end":2,"st\u0061rt":2}]}`,
		`{"version":1,"VERSION":1,"ranges":[]}`,
		`{"version":1,"ranges":[{"start":1,"START":2,"end":2}]}`,
	} {
		t.Run(bad, func(t *testing.T) {
			s := Store{Coordinator: state.Coordinator{Dir: t.TempDir()}}
			path := filepath.Join(s.Coordinator.Dir, "config.json")
			if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if _, err := s.Read(); err == nil {
					t.Fatal("accepted invalid config")
				}
				called := false
				if _, _, err := s.Mutate(context.Background(), func(p *ports.Pool) (int, error) { called = true; return 0, nil }); err == nil {
					t.Fatal("mutation succeeded")
				}
				if called {
					t.Fatal("callback ran before successful read")
				}
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, []byte(bad)) {
					t.Fatalf("evidence modified: %q %v", got, err)
				}
			}
		})
	}
}

func TestLockTimeoutAndRecovery(t *testing.T) {
	s := Store{Coordinator: state.Coordinator{Dir: t.TempDir(), LockTimeout: 50 * time.Millisecond}}
	lock := flock.New(filepath.Join(s.Coordinator.Dir, "state.lock"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	called := false
	start := time.Now()
	if _, _, err := s.Mutate(context.Background(), func(p *ports.Pool) (int, error) { called = true; return 0, nil }); err == nil {
		t.Fatal("mutation ignored lock")
	}
	if called || time.Since(start) > time.Second {
		t.Fatal("lock timeout not bounded")
	}
	if err := lock.Unlock(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Mutate(context.Background(), func(p *ports.Pool) (int, error) { return p.Apply([]ports.Range{{Start: 1, End: 1}}, true), nil }); err != nil {
		t.Fatal(err)
	}
}

func TestUnrelatedFileSurvivesClear(t *testing.T) {
	s := Store{Coordinator: state.Coordinator{Dir: t.TempDir()}}
	path := filepath.Join(s.Coordinator.Dir, "unrelated.txt")
	want := []byte("unrelated content\n")
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Mutate(context.Background(), func(p *ports.Pool) (int, error) { *p = ports.Pool{}; return 0, nil }); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("unrelated file changed: %q %v", got, err)
	}
}

func TestInvalidFileType(t *testing.T) {
	s := Store{Coordinator: state.Coordinator{Dir: t.TempDir()}}
	if err := os.Mkdir(filepath.Join(s.Coordinator.Dir, "config.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(); err == nil {
		t.Fatal("accepted directory")
	}
}

func TestStoredNormalizationAndPermissions(t *testing.T) {
	s := Store{Coordinator: state.Coordinator{Dir: filepath.Join(t.TempDir(), "state")}}
	_, total, err := s.Mutate(context.Background(), func(p *ports.Pool) (int, error) {
		return p.Apply([]ports.Range{{Start: 20, End: 30}, {Start: 1, End: 10}, {Start: 10, End: 19}, {Start: 50, End: 50}}, true), nil
	})
	if err != nil || total != 31 {
		t.Fatalf("total %d: %v", total, err)
	}
	path := filepath.Join(s.Coordinator.Dir, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || !reflect.DeepEqual(doc.Ranges, []ports.Range{{Start: 1, End: 30}, {Start: 50, End: 50}}) {
		t.Fatalf("stored noncanonical document: %+v", doc)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("config permissions: %v %v", info, err)
	}
	info, err = os.Stat(s.Coordinator.Dir)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("directory permissions: %v %v", info, err)
	}
}

func TestInaccessibleConfigPreserved(t *testing.T) {
	s := Store{Coordinator: state.Coordinator{Dir: t.TempDir()}}
	path := filepath.Join(s.Coordinator.Dir, "config.json")
	want := []byte(`{"version":1,"ranges":[]}`)
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0600)
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("current user can read files with mode 000")
	}
	for i := 0; i < 2; i++ {
		if _, err := s.Read(); err == nil {
			t.Fatal("read ignored permissions")
		}
		if _, _, err := s.Mutate(context.Background(), func(p *ports.Pool) (int, error) { return 0, nil }); err == nil {
			t.Fatal("mutation ignored unreadable config")
		}
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("inaccessible evidence changed: %q %v", got, err)
	}
}

func TestOverrideMustBeAbsolute(t *testing.T) {
	for _, value := range []string{"", "relative"} {
		t.Setenv("PORTBOOK_CONFIG_DIR", value)
		if _, err := Default(); err == nil {
			t.Fatalf("accepted override %q", value)
		}
	}
	dir := t.TempDir()
	t.Setenv("PORTBOOK_CONFIG_DIR", dir)
	s, err := Default()
	if err != nil || s.Coordinator.Dir != dir {
		t.Fatalf("override ignored: %+v %v", s, err)
	}
}
