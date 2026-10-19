package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/sviatoslav/portbook/internal/ports"
)

const Version = 1

type document struct {
	Version int           `json:"version"`
	Ranges  []ports.Range `json:"ranges"`
}

func decode(data []byte) (ports.Pool, error) {
	if err := uniqueMembers(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return ports.Pool{}, fmt.Errorf("invalid config: %w", err)
	}
	var doc document
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return ports.Pool{}, fmt.Errorf("invalid config: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return ports.Pool{}, errors.New("invalid config: trailing JSON data")
	}
	if doc.Version != Version {
		return ports.Pool{}, fmt.Errorf("unsupported config version %d", doc.Version)
	}
	if doc.Ranges == nil {
		return ports.Pool{}, errors.New("invalid config: ranges must be an array")
	}
	p, err := ports.FromRanges(doc.Ranges)
	if err != nil {
		return ports.Pool{}, fmt.Errorf("invalid config: %w", err)
	}
	return p, nil
}

func encode(p ports.Pool) ([]byte, error) {
	data, err := json.MarshalIndent(document{Version: Version, Ranges: p.Ranges()}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// uniqueMembers walks JSON tokens before struct decoding can discard evidence.
// Object scopes are independent; decoded keys also catch escaped duplicates.
func uniqueMembers(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			keyToken, err := dec.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object member name must be a string")
			}
			// encoding/json matches struct field names case-insensitively.
			// Canonicalize those schema names so aliases cannot overwrite them.
			canonical := canonicalKey(key)
			if seen[canonical] {
				return fmt.Errorf("duplicate object member %q", key)
			}
			seen[canonical] = true
			if err := uniqueMembers(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	case json.Delim('['):
		for dec.More() {
			if err := uniqueMembers(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	}
	return nil
}

func canonicalKey(key string) string {
	for _, name := range []string{"version", "ranges", "start", "end", "directory"} {
		if strings.EqualFold(key, name) {
			return name
		}
	}
	return key
}
