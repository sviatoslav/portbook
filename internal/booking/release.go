package booking

import (
	"context"
	"errors"
	"fmt"

	"github.com/sviatoslav/portbook/internal/state"
)

// ErrUnavailable means a booking must be retained because its port is occupied
// or the probe could not establish that it is available.
var ErrUnavailable = errors.New("booked TCP port is occupied or availability is uncertain")

type ReleaseResult struct {
	Directory string `json:"directory"`
	Service   string `json:"service"`
	Port      *int   `json:"port"`
	Released  bool   `json:"released"`
}

// Release uses only the fixed coordinator, never the selected configuration.
// The lock covers owner matching, the conservative probe, and the atomic save.
func (s Store) Release(ctx context.Context, directory, service string) (ReleaseResult, error) {
	if err := ValidateService(service); err != nil {
		return ReleaseResult{}, err
	}
	dir, err := Directory(directory)
	if err != nil {
		return ReleaseResult{}, err
	}
	res := ReleaseResult{Directory: dir, Service: service}
	err = s.Config.Coordinator.WithLock(ctx, func() error {
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
		key, err := ownerKey(r, dir)
		if err != nil {
			return err
		}
		res.Directory = key
		port := r[key][service]
		if port == 0 {
			return ctx.Err()
		}
		probe := s.Probe
		if probe == nil {
			probe = Available
		}
		available, err := probe(ctx, port)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		if !available {
			return fmt.Errorf("%w: %d", ErrUnavailable, port)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		delete(r[key], service)
		if len(r[key]) == 0 {
			delete(r, key)
		}
		data, err := encode(r)
		if err != nil {
			return err
		}
		if err := state.AtomicWrite(s.Config.Coordinator.Dir, File, data); err != nil {
			return err
		}
		res.Port, res.Released = &port, true
		return nil
	})
	if err != nil {
		return ReleaseResult{}, err
	}
	return res, nil
}
