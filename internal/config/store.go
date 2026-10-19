// Package config owns configuration persistence. Booking data belongs in a
// separate file; future mutations must use the same state.lock.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"portbook/internal/ports"
	"portbook/internal/state"
)

type Store struct {
	// Coordinator owns both the fixed bootstrap root and shared mutation lock.
	Coordinator state.Coordinator
	// Location supplies optional selection overrides. Its coordinator is ignored;
	// one store must never resolve a pointer under a different root from its lock.
	Location *Location
}

func Default() (Store, error) {
	return WithOverride("", false)
}

func WithOverride(dir string, supplied bool) (Store, error) {
	l, err := DefaultLocation(dir, supplied)
	if err != nil {
		return Store{}, err
	}
	return Store{Coordinator: l.Coordinator, Location: &l}, nil
}

// Read never creates a directory, lock, or config file. Atomic replacement lets
// readers see one complete committed snapshot without taking the mutation lock.
func (s Store) Read() (ports.Pool, error) {
	dir, err := s.directory()
	if err != nil {
		return ports.Pool{}, err
	}
	return readConfig(dir)
}

func (s Store) directory() (string, error) {
	location := Location{}
	if s.Location != nil {
		location = *s.Location
	}
	location.Coordinator = s.Coordinator
	selection, err := location.Resolve()
	return selection.Directory, err
}

func readConfig(dir string) (ports.Pool, error) {
	path := filepath.Join(dir, "config.json")
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return ports.Pool{}, nil
	}
	if err != nil {
		return ports.Pool{}, fmt.Errorf("inspect config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ports.Pool{}, errors.New("config must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ports.Pool{}, fmt.Errorf("read config: %w", err)
	}
	return decode(data)
}

// Mutate holds the shared OS lock across read/modify/save. A callback error
// discards the in-memory changes without saving. It must not reacquire the lock.
func (s Store) Mutate(ctx context.Context, change func(*ports.Pool) (int, error)) (changed, total int, err error) {
	err = s.Coordinator.WithLock(ctx, func() error {
		dir, err := s.directory()
		if err != nil {
			return err
		}
		p, err := readConfig(dir)
		if err != nil {
			return err
		}
		changed, err = change(&p)
		if err != nil {
			return err
		}
		if err := saveConfig(dir, p); err != nil {
			return err
		}
		total = p.Count()
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return changed, total, nil
}

func saveConfig(dir string, p ports.Pool) error {
	data, err := encode(p)
	if err != nil {
		return err
	}
	return atomicWrite(dir, "config.json", data)
}

// atomicWrite syncs both the replacement and its parent directory.
func atomicWrite(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	f, err := os.CreateTemp(dir, ".portbook-*")
	if err != nil {
		return fmt.Errorf("create temporary %s: %w", name, err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Rename(f.Name(), filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("replace %s: %w", name, err)
	}
	parent, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	defer parent.Close()
	if err := parent.Sync(); err != nil {
		return fmt.Errorf("sync directory (replacement already committed): %w", err)
	}
	return nil
}
