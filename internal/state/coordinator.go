// Package state owns the per-user state directory and its shared mutation lock.
// Coordinator does not read or write configuration or registry data.
package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

type Coordinator struct {
	Dir         string
	LockTimeout time.Duration
}

func Default() (Coordinator, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Coordinator{}, fmt.Errorf("locate user config directory: %w", err)
	}
	dir := filepath.Join(base, "portbook")
	return Coordinator{Dir: dir, LockTimeout: 5 * time.Second}, nil
}

// WithLock serializes cooperating mutations, including their reads and saves.
// It guarantees unlock on error or panic, but does not roll back callback writes.
// Callbacks must not acquire this lock again or call another locking operation.
func (c Coordinator) WithLock(ctx context.Context, operation func() error) (err error) {
	if c.Dir == "" || !filepath.IsAbs(c.Dir) {
		return errors.New("state directory must be an absolute path")
	}
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	lock := flock.New(filepath.Join(c.Dir, "state.lock"))
	defer func() {
		if closeErr := lock.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("release state lock: %w", closeErr))
		}
	}()
	timeout := c.LockTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	locked, err := lock.TryLockContext(ctx, 10*time.Millisecond)
	if err != nil {
		return fmt.Errorf("acquire state lock: %w", err)
	}
	if !locked {
		return errors.New("timed out waiting for state lock")
	}
	return operation()
}
