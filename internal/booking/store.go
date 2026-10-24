package booking

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sviatoslav/portbook/internal/ports"
	"github.com/sviatoslav/portbook/internal/state"
)

// ErrAlreadyExists and ErrNotFound distinguish lifecycle conflicts from storage failures.
var ErrAlreadyExists = errors.New("reservation already exists")
var ErrNotFound = errors.New("reservation not found")

var ErrExhausted = errors.New("no available ports in the configured pool")

type Store struct {
	Coordinator state.Coordinator
	// ReadPool is required only for new reservations. It resolves and reads the
	// selected pool inside Coordinator's lock; it must not acquire another lock.
	ReadPool func() (ports.Pool, error)
	// Optional dependency for deterministic selection/failure tests.
	Probe func(context.Context, int) (bool, error)
}

func (s Store) read() (Registry, error) {
	if s.Coordinator.Dir == "" || !filepath.IsAbs(s.Coordinator.Dir) {
		return nil, errors.New("state directory must be an absolute path")
	}
	path := filepath.Join(s.Coordinator.Dir, File)
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

// Reserve holds the global coordinator lock across registry reads, duplicate
// detection, pool selection, probes, and saving. Only new bookings read the pool.
func (s Store) Reserve(ctx context.Context, directory, service string) (int, error) {
	if err := ValidateService(service); err != nil {
		return 0, err
	}
	dir, err := Directory(directory)
	if err != nil {
		return 0, err
	}
	port := 0
	err = s.Coordinator.WithLock(ctx, func() error {
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
		if s.ReadPool == nil {
			return errors.New("reservation requires a pool reader")
		}
		pool, err := s.ReadPool()
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
			if err := state.AtomicWrite(s.Coordinator.Dir, File, data); err != nil {
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

// readOwner validates one owner and resolves it against one registry snapshot.
// It never reads the pool, takes a lock, probes ports, or writes state.
func (s Store) readOwner(directory string) (Registry, string, error) {
	dir, err := Directory(directory)
	if err != nil {
		return nil, "", err
	}
	r, err := s.read()
	if err != nil {
		return nil, "", err
	}
	key, err := ownerKey(r, dir)
	return r, key, err
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
