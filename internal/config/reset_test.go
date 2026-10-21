package config

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sviatoslav/portbook/internal/ports"
	"github.com/sviatoslav/portbook/internal/state"
)

func TestResetEffectivePool(t *testing.T) {
	for _, tc := range []struct {
		name, ranges string
		changed      int
	}{
		{"missing", "", 0},
		{"empty", `[]`, 16384},
		{"custom", `[{"start":1,"end":2},{"start":50000,"end":50001}]`, 16384},
		{"subset", `[{"start":50000,"end":50001}]`, 16382},
		{"superset", `[{"start":49151,"end":65535}]`, 1},
		{"default", `[{"start":49152,"end":65535}]`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Store{Coordinator: state.Coordinator{Dir: filepath.Join(t.TempDir(), "state")}}
			path := filepath.Join(s.Coordinator.Dir, "config.json")
			if tc.ranges != "" {
				if err := os.MkdirAll(s.Coordinator.Dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(`{"version":1,"ranges":`+tc.ranges+`}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for i, want := range []int{tc.changed, 0} {
				changed, total, err := s.Reset(context.Background())
				if err != nil || changed != want || total != 16384 {
					t.Fatalf("reset %d: %d %d %v", i, changed, total, err)
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var doc document
				if err := json.Unmarshal(data, &doc); err != nil || doc.Version != 1 || !reflect.DeepEqual(doc.Ranges, []ports.Range{{Start: 49152, End: 65535}}) {
					t.Fatalf("saved defaults: %s %v", data, err)
				}
				info, err := os.Stat(path)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal(info, err)
				}
			}
		})
	}
}

func TestResetWriteFailurePreservesConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	dir := t.TempDir()
	s := Store{Coordinator: state.Coordinator{Dir: dir}}
	path := filepath.Join(dir, "config.json")
	before := []byte(`{"version":1,"ranges":[]}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	// Create the shared lock first so the failure occurs at the atomic save.
	if err := s.Coordinator.WithLock(context.Background(), func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	changed, total, err := s.Reset(context.Background())
	if err == nil || changed != 0 || total != 0 {
		t.Fatal(changed, total, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("failed reset changed config", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Reset(context.Background()); err != nil {
		t.Fatal("failed save leaked lock", err)
	}
}
