package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"portbook/internal/state"
)

const LocationFile = "location.json"

// Location keeps configuration selection separate from fixed shared coordination.
// Overrides are captured once; the persistent selection is read on each Resolve.
type Location struct {
	Coordinator state.Coordinator
	Flag        string
	FlagSet     bool
	Env         string
	EnvSet      bool
}

type Selection struct {
	Directory  string `json:"directory"`
	ConfigFile string `json:"config_file"`
	Source     string `json:"source"`
}

type locationDocument struct {
	Version   int    `json:"version"`
	Directory string `json:"directory"`
}

func DefaultLocation(flag string, supplied bool) (Location, error) {
	c, err := state.Default()
	if err != nil {
		return Location{}, err
	}
	env, set := os.LookupEnv("PORTBOOK_CONFIG_DIR")
	l := Location{Coordinator: c, Flag: flag, FlagSet: supplied, Env: env, EnvSet: set}
	if supplied {
		err = l.ValidateSelection(flag)
	} else if set {
		err = l.ValidateSelection(env)
	}
	return l, err
}

// ValidateDirectory permits absent directories but rejects files in any component.
func ValidateDirectory(dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return errors.New("config directory must be a nonempty absolute path")
	}
	if !utf8.ValidString(dir) {
		return errors.New("config directory must be valid UTF-8")
	}
	for path := filepath.Clean(dir); ; path = filepath.Dir(path) {
		info, err := os.Stat(path)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("config directory component %q is not a directory", path)
			}
			return nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect config directory: %w", err)
		}
		// Stat follows directory symlinks, but a dangling link is an existing
		// unusable component rather than an absent directory we may create.
		if info, linkErr := os.Lstat(path); linkErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("config directory component %q is a dangling symlink", path)
		} else if linkErr != nil && !errors.Is(linkErr, os.ErrNotExist) {
			return fmt.Errorf("inspect config directory component: %w", linkErr)
		}
		if filepath.Dir(path) == path {
			return err
		}
	}
}

// ValidateSelection validates a config directory and reserves bootstrap filenames before they
// exist. Otherwise selecting location.json (or a child) could be invalidated by
// the pointer save itself, or range writes could create a directory in its place.
func (l Location) ValidateSelection(dir string) error {
	if err := ValidateDirectory(dir); err != nil {
		return err
	}
	root := filepath.Clean(l.Coordinator.Dir)
	rootInfo, _ := os.Stat(root)
	child := ""
	for path := filepath.Clean(dir); ; path = filepath.Dir(path) {
		sameRoot := path == root
		if !sameRoot && rootInfo != nil {
			info, err := os.Stat(path)
			sameRoot = err == nil && os.SameFile(rootInfo, info)
		}
		if sameRoot {
			// Reserve case aliases consistently across macOS and Linux filesystems.
			if strings.EqualFold(child, LocationFile) || strings.EqualFold(child, "state.lock") || strings.EqualFold(child, "bookings.json") {
				return fmt.Errorf("config directory conflicts with reserved bootstrap file %q", child)
			}
			return nil
		}
		if filepath.Dir(path) == path {
			return nil
		}
		child = filepath.Base(path)
	}
}

func selection(dir, source string) Selection {
	dir = filepath.Clean(dir)
	return Selection{Directory: dir, ConfigFile: filepath.Join(dir, "config.json"), Source: source}
}

func (l Location) Resolve() (Selection, error) {
	dir, source := l.Coordinator.Dir, "default"
	if l.FlagSet {
		dir, source = l.Flag, "flag"
	} else if l.EnvSet {
		dir, source = l.Env, "environment"
	} else {
		saved, err := l.readSaved()
		if err != nil {
			return Selection{}, err
		}
		if saved != "" {
			dir, source = saved, "saved"
		}
	}
	if err := l.ValidateSelection(dir); err != nil {
		return Selection{}, err
	}
	return selection(dir, source), nil
}

func (l Location) readSaved() (string, error) {
	path := filepath.Join(l.Coordinator.Dir, LocationFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect saved config location: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("saved config location must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read saved config location: %w", err)
	}
	if err := uniqueMembers(json.NewDecoder(bytes.NewReader(data))); err != nil {
		return "", fmt.Errorf("invalid saved config location: %w", err)
	}
	var doc locationDocument
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return "", fmt.Errorf("invalid saved config location: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return "", errors.New("invalid saved config location: trailing JSON data")
	}
	if err := state.ValidateJSONUnicode(data); err != nil {
		return "", fmt.Errorf("invalid saved config location: %w", err)
	}
	if doc.Version != 1 {
		return "", fmt.Errorf("unsupported saved config location version %d", doc.Version)
	}
	if err := ValidateDirectory(doc.Directory); err != nil {
		return "", fmt.Errorf("invalid saved config location: %w", err)
	}
	return filepath.Clean(doc.Directory), nil
}

// Change updates only the pointer, under the same lock as every config mutation.
// Empty dir explicitly resets the choice. Neither operation reads the old pointer,
// so either can repair corrupt/unreadable content without following a symlink.
// The returned selection describes the effective location at the commit point.
func (l Location) Change(ctx context.Context, dir string) (effective Selection, err error) {
	if dir != "" {
		if err := l.ValidateSelection(dir); err != nil {
			return Selection{}, err
		}
	}
	// Validate active overrides before creating lock infrastructure as well as
	// again inside the lock, before any pointer write.
	if l.FlagSet {
		if err := l.ValidateSelection(l.Flag); err != nil {
			return Selection{}, err
		}
	} else if l.EnvSet {
		if err := l.ValidateSelection(l.Env); err != nil {
			return Selection{}, err
		}
	}
	err = l.Coordinator.WithLock(ctx, func() error {
		// Compute the post-change selection without reading the old pointer. Any
		// validation error must happen before the pointer is replaced or removed.
		selected, source := dir, "saved"
		if dir == "" {
			selected, source = l.Coordinator.Dir, "default"
		}
		if l.FlagSet {
			selected, source = l.Flag, "flag"
		} else if l.EnvSet {
			selected, source = l.Env, "environment"
		}
		if err := l.ValidateSelection(selected); err != nil {
			return err
		}
		effective = selection(selected, source)
		if dir != "" {
			if err := l.ValidateSelection(dir); err != nil {
				return err
			}
			data, err := json.MarshalIndent(locationDocument{1, filepath.Clean(dir)}, "", "  ")
			if err != nil {
				return err
			}
			if err := state.AtomicWrite(l.Coordinator.Dir, LocationFile, append(data, '\n')); err != nil {
				return err
			}
		} else {
			path := filepath.Join(l.Coordinator.Dir, LocationFile)
			info, err := os.Lstat(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("inspect saved config location: %w", err)
			}
			if err == nil && info.IsDir() {
				return errors.New("saved config location must not be a directory")
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("reset saved config location: %w", err)
			}
			parent, err := os.Open(l.Coordinator.Dir)
			if err != nil {
				return err
			}
			defer parent.Close()
			if err := parent.Sync(); err != nil {
				return fmt.Errorf("sync location directory (reset already committed): %w", err)
			}
		}
		return nil
	})
	return effective, err
}
