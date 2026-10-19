package booking

import "fmt"

// Get reads one atomic registry snapshot using the same owner identity as List.
// It takes no lock, reads no configuration, probes no ports, and writes nothing.
func (s Store) Get(directory, service string) (int, error) {
	if err := ValidateService(service); err != nil {
		return 0, err
	}
	// List's empty directory means all owners; Get always requires one owner.
	dir, err := Directory(directory)
	if err != nil {
		return 0, err
	}
	entries, err := s.List(dir)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if entry.Service == service {
			return entry.Port, nil
		}
	}
	return 0, fmt.Errorf("%w for service %q in %q", ErrNotFound, service, dir)
}
