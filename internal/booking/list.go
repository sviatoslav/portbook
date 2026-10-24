package booking

import "sort"

// Entry retains the stored owner spelling, including owners that disappeared.
type Entry struct {
	Directory string `json:"directory"`
	Service   string `json:"service"`
	Port      int    `json:"port"`
}

// List reads one atomic registry snapshot without creating or locking anything.
// An empty directory selects all owners; scoped reads use Reserve's identity rules.
// Configuration selection is irrelevant here.
func (s Store) List(directory string) ([]Entry, error) {
	var r Registry
	var err error
	if directory == "" {
		r, err = s.read()
	} else {
		r, directory, err = s.readOwner(directory)
	}
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0)
	for owner, services := range r {
		if directory != "" && owner != directory {
			continue
		}
		for service, port := range services {
			entries = append(entries, Entry{Directory: owner, Service: service, Port: port})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Directory != entries[j].Directory {
			return entries[i].Directory < entries[j].Directory
		}
		return entries[i].Service < entries[j].Service
	})
	return entries, nil
}
