package booking

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"portbook/internal/state"
)

const File = "bookings.json"

type Registry map[string]map[string]int
type document struct {
	Version  int      `json:"version"`
	Bookings Registry `json:"bookings"`
}

func decode(data []byte) (Registry, error) {
	if err := state.ValidateJSONUnicode(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := unique(dec); err != nil {
		return nil, fmt.Errorf("invalid bookings: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("invalid bookings: trailing JSON data")
	}
	// Decode schema as raw exact keys; directory/service keys are case-sensitive
	// data, including names that happen to resemble schema fields.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if len(fields) != 2 || fields["version"] == nil || fields["bookings"] == nil {
		return nil, errors.New("invalid bookings: expected version and bookings")
	}
	var doc document
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid bookings: %w", err)
	}
	if doc.Version != 1 {
		return nil, fmt.Errorf("unsupported bookings version %d", doc.Version)
	}
	if doc.Bookings == nil {
		return nil, errors.New("invalid bookings: bookings must be an object")
	}
	if err := validate(doc.Bookings); err != nil {
		return nil, err
	}
	return doc.Bookings, nil
}

func validate(r Registry) error {
	used := map[int]bool{}
	for dir, services := range r {
		if err := validatePath(dir); err != nil {
			return fmt.Errorf("invalid booking directory: %w", err)
		}
		if filepath.Clean(dir) != dir {
			return errors.New("invalid booking directory: path must be normalized")
		}
		if services == nil {
			return errors.New("invalid bookings: services must be an object")
		}
		for service, port := range services {
			if err := ValidateService(service); err != nil {
				return err
			}
			if port < 1 || port > 65535 {
				return errors.New("invalid booking port: expected 1..65535")
			}
			if used[port] {
				return fmt.Errorf("duplicate booking port %d", port)
			}
			used[port] = true
		}
	}
	return nil
}

func encode(r Registry) ([]byte, error) {
	if err := validate(r); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(document{1, r}, "", "  ")
	return append(data, '\n'), err
}

// Check decoded keys before map decoding can discard duplicate evidence.
func unique(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := tok.(string)
			if !ok {
				return errors.New("invalid object key")
			}
			if seen[key] {
				return fmt.Errorf("duplicate object member %q", key)
			}
			seen[key] = true
			if err := unique(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	case json.Delim('['):
		for dec.More() {
			if err := unique(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	}
	return nil
}
