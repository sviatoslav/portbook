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
	Coordinator state.Coordinator
}

func Default() (Store, error) {
	coordinator, err := state.Default()
	return Store{Coordinator: coordinator}, err
}

// Read never creates a directory, lock, or config file. Atomic replacement lets
// readers see one complete committed snapshot without taking the mutation lock.
func (s Store) Read() (ports.Pool, error) {
	path := filepath.Join(s.Coordinator.Dir, "config.json")
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
		p, err := s.Read()
		if err != nil {
			return err
		}
		changed, err = change(&p)
		if err != nil {
			return err
		}
		if err := s.save(p); err != nil {
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

func (s Store) save(p ports.Pool) error {
	data, err := encode(p)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Coordinator.Dir, ".config-*")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close config: %w", err)
	}
	if err := os.Rename(f.Name(), filepath.Join(s.Coordinator.Dir, "config.json")); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	dir, err := os.Open(s.Coordinator.Dir)
	if err != nil {
		return fmt.Errorf("open config directory for sync: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync config directory (replacement already committed): %w", err)
	}
	return nil
}
