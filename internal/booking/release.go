package booking

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/sviatoslav/portbook/internal/state"
)

type ReleaseResult struct {
	Directory string `json:"directory"`
	Service   string `json:"service"`
	Port      *int   `json:"port"`
	Released  bool   `json:"released"`
}

type ReleaseAllResult struct {
	Directory string `json:"directory"`
	Ports     []int  `json:"ports"`
	Released  int    `json:"released"`
}

// Release deletes one registry booking. It never probes ports or stops listeners.
func (s Store) Release(ctx context.Context, directory, service string) (ReleaseResult, error) {
	if err := ValidateService(service); err != nil {
		return ReleaseResult{}, err
	}
	res, err := s.releaseRecords(ctx, directory, service, false)
	if err != nil {
		return ReleaseResult{}, err
	}
	return ReleaseResult{Directory: res.Directory, Service: service, Port: &res.Ports[0], Released: true}, nil
}

// ReleaseAll deletes only this exact canonical owner's bookings, never descendants.
func (s Store) ReleaseAll(ctx context.Context, directory string) (ReleaseAllResult, error) {
	return s.releaseRecords(ctx, directory, "", true)
}

// The fixed global lock covers owner matching, reads, decisions, and one atomic
// save. Configuration selection and TCP availability are irrelevant to deletion.
func (s Store) releaseRecords(ctx context.Context, directory, service string, all bool) (ReleaseAllResult, error) {
	dir, err := Directory(directory)
	if err != nil {
		return ReleaseAllResult{}, err
	}
	var res ReleaseAllResult
	err = s.Coordinator.WithLock(ctx, func() error {
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
		var ports []int
		if all {
			for _, port := range r[key] {
				ports = append(ports, port)
			}
			if len(ports) == 0 {
				return fmt.Errorf("%w in %q", ErrNotFound, key)
			}
			sort.Ints(ports)
		} else {
			port := r[key][service]
			if port == 0 {
				return fmt.Errorf("%w for service %q in %q", ErrNotFound, service, key)
			}
			ports = []int{port}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if all {
			delete(r, key)
		} else {
			delete(r[key], service)
			if len(r[key]) == 0 {
				delete(r, key)
			}
		}
		data, err := encode(r)
		if err != nil {
			return err
		}
		if err := state.AtomicWrite(s.Coordinator.Dir, File, data); err != nil {
			return err
		}
		res = ReleaseAllResult{Directory: key, Ports: ports, Released: len(ports)}
		return nil
	})
	if err != nil {
		return ReleaseAllResult{}, err
	}
	return res, nil
}
