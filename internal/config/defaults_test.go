package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sviatoslav/portbook/internal/ports"
	"github.com/sviatoslav/portbook/internal/state"
)

func TestMissingDefaultsForEverySelectionWithoutWrites(t *testing.T) {
	for _, selection := range []string{"default", "saved", "flag", "environment"} {
		t.Run(selection, func(t *testing.T) {
			base := t.TempDir()
			c := state.Coordinator{Dir: filepath.Join(base, "bootstrap")}
			l := Location{Coordinator: c}
			selected := c.Dir
			if selection != "default" {
				selected = filepath.Join(base, "custom")
			}
			switch selection {
			case "saved":
				if _, err := l.Change(context.Background(), selected); err != nil {
					t.Fatal(err)
				}
			case "flag":
				l.Flag, l.FlagSet = selected, true
			case "environment":
				l.Env, l.EnvSet = selected, true
			}
			s := Store{Coordinator: c, Location: &l}
			p, err := s.Read()
			if err != nil || p.Count() != 16384 || !reflect.DeepEqual(p.Ranges(), []ports.Range{{Start: 49152, End: 65535}}) {
				t.Fatalf("defaults: %v %v", p.Ranges(), err)
			}
			if _, err := os.Stat(selected); !os.IsNotExist(err) {
				t.Fatalf("read created config directory: %v", err)
			}
			// Mutating the returned value must not affect subsequent reads.
			p[49152] = false
			again, err := s.Read()
			if err != nil || !again[49152] {
				t.Fatal("shared default mutated", err)
			}
		})
	}
}

func TestSavedConfigAuthoritative(t *testing.T) {
	for _, ranges := range []string{`[]`, `[{"start":51000,"end":51001}]`} {
		t.Run(ranges, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "config.json")
			data := []byte(`{"version":1,"ranges":` + ranges + `}`)
			if err := os.WriteFile(file, data, 0600); err != nil {
				t.Fatal(err)
			}
			p, err := (Store{Coordinator: state.Coordinator{Dir: dir}}).Read()
			want := 0
			if ranges != "[]" {
				want = 2
			}
			if err != nil || p.Count() != want || p[49152] {
				t.Fatal(p.Ranges(), err)
			}
			after, err := os.ReadFile(file)
			if err != nil || string(after) != string(data) {
				t.Fatal("config changed", err)
			}
		})
	}
}

func TestFirstMutationUsesEffectiveDefaults(t *testing.T) {
	for _, tc := range []struct {
		name           string
		ranges         []ports.Range
		add            bool
		changed, total int
	}{
		{"union overlap", []ports.Range{{Start: 49150, End: 49153}}, true, 2, 16386},
		{"subtract", []ports.Range{{Start: 49150, End: 49153}}, false, 2, 16382},
		{"clear", []ports.Range{{Start: 1, End: 65535}}, false, 16384, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Store{Coordinator: state.Coordinator{Dir: filepath.Join(t.TempDir(), "state")}}
			changed, total, err := s.Mutate(context.Background(), func(p *ports.Pool) (int, error) { return p.Apply(tc.ranges, tc.add), nil })
			if err != nil || changed != tc.changed || total != tc.total {
				t.Fatal(changed, total, err)
			}
			p, err := s.Read()
			if err != nil || p.Count() != tc.total {
				t.Fatal(p.Count(), err)
			}
		})
	}
}
