package booking

import "fmt"

// Get reads one atomic registry snapshot using the same owner identity as List.
// It takes no lock, reads no configuration, probes no ports, and writes nothing.
func (s Store) Get(directory, service string) (int, error) {
	if err := ValidateService(service); err != nil {
		return 0, err
	}
	r, dir, err := s.readOwner(directory)
	if err != nil {
		return 0, err
	}
	if port := r[dir][service]; port != 0 {
		return port, nil
	}
	return 0, fmt.Errorf("%w for service %q in %q", ErrNotFound, service, dir)
}
