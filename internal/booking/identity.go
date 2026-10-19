// Package booking owns persistent directory/service reservations.
package booking

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func ValidateService(service string) error {
	if service == "" || !utf8.ValidString(service) || strings.ContainsRune(service, 0) {
		return errors.New("service must be nonempty valid UTF-8 without NUL")
	}
	return nil
}

func validatePath(dir string) error {
	if dir == "" || !filepath.IsAbs(dir) {
		return errors.New("directory must be an absolute path")
	}
	if !utf8.ValidString(dir) || strings.ContainsRune(dir, 0) {
		return errors.New("directory must be valid UTF-8 without NUL")
	}
	return nil
}

// Directory resolves existing symlink aliases, without changing cwd or finding a
// repository root. Registry decoding deliberately does not require existence.
func Directory(dir string) (string, error) {
	if err := validatePath(dir); err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", errors.New("directory does not exist")
	}
	if err != nil {
		return "", fmt.Errorf("cannot access directory: %w", err)
	}
	if err := validatePath(canonical); err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	// Reading and searching must both be possible. Opening alone does not check
	// search permission on the final directory on Unix.
	f, err := os.Open(canonical)
	if err != nil {
		return "", fmt.Errorf("cannot access directory: %w", err)
	}
	defer f.Close()
	if _, err = f.ReadDir(1); err != nil && err != io.EOF {
		return "", fmt.Errorf("cannot access directory: %w", err)
	}
	if _, err = os.Stat(canonical + string(os.PathSeparator) + "."); err != nil {
		return "", fmt.Errorf("cannot access directory: %w", err)
	}
	return filepath.Clean(canonical), nil
}
