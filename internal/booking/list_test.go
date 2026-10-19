package booking

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gofrs/flock"
	"portbook/internal/config"
	"portbook/internal/state"
)

func TestListSnapshotIdentityAndNoWrites(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	s := Store{Config: config.Store{Coordinator: state.Coordinator{Dir: root}}}
	entries, err := s.List("")
	if err != nil || entries == nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("created state", err)
	}
	owner, err := Directory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(t.TempDir(), "gone")
	data, err := encode(Registry{owner: {"z": 2, "a": 1}, gone: {"server": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, File)
	if err := os.WriteFile(path, data, 0400); err != nil {
		t.Fatal(err)
	}
	// Configuration and pointer are deliberately corrupt: neither is consulted.
	for _, name := range []string{"config.json", "location.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("broken"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	held := flock.New(filepath.Join(root, "state.lock"))
	if err := held.Lock(); err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(owner, alias); err != nil {
		t.Fatal(err)
	}
	want := []Entry{{owner, "a", 1}, {owner, "z", 2}}
	for _, dir := range []string{owner, alias} {
		got, err := s.List(dir)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal(got, err)
		}
	}
	all, err := s.List("")
	if err != nil || len(all) != 3 {
		t.Fatal(all, err)
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].Directory > all[i].Directory || all[i-1].Directory == all[i].Directory && all[i-1].Service > all[i].Service {
			t.Fatal(all)
		}
	}
	if !bytes.Equal(data, contents(t, path)) {
		t.Fatal("list changed registry")
	}
	if _, err := s.List(gone); err == nil {
		t.Fatal("accepted missing scoped owner")
	}
}

func TestListFailsClosed(t *testing.T) {
	for _, data := range []string{"bad", `{"version":2,"bookings":{}}`, `{"version":1,"bookings":{"/gone":{"a":1,"a":2}}}`, `{"version":1,"bookings":{"/gone":{"a":1},"/other":{"b":1}}}`, `{"version":1,"bookings":{"/gone":{"\ud800":1}}}`} {
		root := t.TempDir()
		path := filepath.Join(root, File)
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		s := Store{Config: config.Store{Coordinator: state.Coordinator{Dir: root}}}
		for i := 0; i < 2; i++ {
			if _, err := s.List(""); err == nil {
				t.Fatal("accepted", data)
			}
			if string(contents(t, path)) != data {
				t.Fatal("destroyed evidence")
			}
		}
		if _, err := os.Stat(filepath.Join(root, "state.lock")); !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"directory", "symlink", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, File)
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
			s := Store{Config: config.Store{Coordinator: state.Coordinator{Dir: root}}}
			if _, err := s.List(""); err == nil {
				t.Fatal("accepted", kind)
			}
		})
	}
}

func TestListCaseIdentityAndChangedSymlink(t *testing.T) {
	s, _ := fixture(t)
	owner := filepath.Join(t.TempDir(), "Owner")
	if err := os.Mkdir(owner, 0700); err != nil {
		t.Fatal(err)
	}
	canonical, err := Directory(owner)
	if err != nil {
		t.Fatal(err)
	}
	data, err := encode(Registry{canonical: {"server": 1}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Config.Coordinator.Dir, File)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(filepath.Dir(owner), "owner")
	if info, err := os.Stat(alias); err == nil {
		original, _ := os.Stat(owner)
		if os.SameFile(info, original) {
			got, err := s.List(alias)
			if err != nil || len(got) != 1 || got[0].Directory != canonical {
				t.Fatal(got, err)
			}
		}
	}
	if err := os.Remove(owner); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, owner); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(target)
	if err != nil || len(got) != 0 {
		t.Fatal("adopted changed symlink", got, err)
	}
	got, err = s.List("")
	if err != nil || len(got) != 1 || got[0].Directory != canonical {
		t.Fatal(got, err)
	}
}

func TestListRejectsInvalidStateRoot(t *testing.T) {
	for _, root := range []string{"", "relative"} {
		s := Store{Config: config.Store{Coordinator: state.Coordinator{Dir: root}}}
		if _, err := s.List(""); err == nil {
			t.Fatalf("accepted invalid state root %q", root)
		}
	}
}
