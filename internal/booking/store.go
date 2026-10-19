package booking

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/state"
)

// ErrAlreadyExists and ErrNotFound distinguish lifecycle conflicts from storage failures.
var ErrAlreadyExists = errors.New("reservation already exists")
var ErrNotFound = errors.New("reservation not found")

var ErrExhausted = errors.New("no available ports in the configured pool")

type Store struct {
	Config config.Store
	// Optional dependency for deterministic selection/failure tests.
	Probe func(context.Context, int) (bool, error)
}

func (s Store) read() (Registry, error) {
	path := filepath.Join(s.Config.Coordinator.Dir, File)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Registry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect bookings: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("bookings must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bookings: %w", err)
	}
	return decode(data)
}

// Reserve acquires exactly the global config coordinator lock, and resolves the
// selected config inside it. Config.Read does not acquire a lock or write files.
func (s Store) Reserve(ctx context.Context, directory, service string) (int, error) {
	if err := ValidateService(service); err != nil {
		return 0, err
	}
	dir, err := Directory(directory)
	if err != nil {
		return 0, err
	}
	port := 0
	err = s.Config.Coordinator.WithLock(ctx, func() error {
		// Recheck after waiting for the lock: invalid owners must never be booked.
		current, err := Directory(directory)
		if err != nil {
			return err
		}
		if current != dir {
			return errors.New("directory identity changed while waiting for state lock")
		}
		r, err := s.read()
		if err != nil {
			return err
		}
		// EvalSymlinks preserves caller spelling on case-insensitive filesystems.
		// Match physical identity against existing canonical owner paths as well;
		// never case-fold path data or follow a registry path that became a symlink.
		key, err := ownerKey(r, dir)
		if err != nil {
			return err
		}
		if r[key][service] != 0 {
			return fmt.Errorf("%w for service %q in %q", ErrAlreadyExists, service, key)
		}
		pool, err := s.Config.Read()
		if err != nil {
			return err
		}
		used := map[int]bool{}
		for _, services := range r {
			for _, p := range services {
				used[p] = true
			}
		}
		probe := s.Probe
		if probe == nil {
			probe = Available
		}
		for p := 1; p <= 65535; p++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !pool[p] || used[p] {
				continue
			}
			available, err := probe(ctx, p)
			if err != nil {
				return err
			}
			if !available {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if r[key] == nil {
				r[key] = map[string]int{}
			}
			r[key][service] = p
			data, err := encode(r)
			if err != nil {
				return err
			}
			if err := state.AtomicWrite(s.Config.Coordinator.Dir, File, data); err != nil {
				return err
			}
			port = p
			return nil
		}
		return ErrExhausted
	})
	if err != nil {
		return 0, err
	}
	return port, nil
}

// ownerKey preserves the first stored spelling for aliases of one directory.
// Missing old directories stay recorded, and an old path replaced by a symlink
// must not transfer its bookings to that link's new target.
func ownerKey(r Registry, dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("inspect owner directory: %w", err)
	}
	matched := ""
	for key := range r {
		canonical, err := filepath.EvalSymlinks(key)
		if err != nil || canonical != key {
			continue
		}
		old, err := os.Stat(key)
		if err != nil || !os.SameFile(info, old) {
			continue
		}
		if matched != "" {
			return "", errors.New("ambiguous bookings: multiple directory keys identify the same owner")
		}
		matched = key
	}
	if matched != "" {
		return matched, nil
	}
	return dir, nil
}
