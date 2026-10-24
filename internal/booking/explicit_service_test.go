package booking

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sviatoslav/portbook/internal/state"
)

func TestStoreNeverDefaultsEmptyService(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent")
	s := Store{Coordinator: state.Coordinator{Dir: root}, Probe: func(context.Context, int) (bool, error) { t.Fatal("empty service probed"); return false, nil }}
	owner := t.TempDir()
	if _, err := s.Get(owner, ""); err == nil {
		t.Fatal("get defaulted empty service")
	}
	if _, err := s.Reserve(context.Background(), owner, ""); err == nil {
		t.Fatal("reserve defaulted empty service")
	}
	if _, err := s.Release(context.Background(), owner, ""); err == nil {
		t.Fatal("release defaulted empty service")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatal("empty service accessed state", err)
	}
}
