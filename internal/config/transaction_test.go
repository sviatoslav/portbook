package config

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"portbook/internal/ports"
	"portbook/internal/state"
)

func TestCallbackFailureDoesNotSaveAndReleasesLock(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(fmt.Sprint(exists), func(t *testing.T) {
			s := Store{Coordinator: state.Coordinator{Dir: t.TempDir(), LockTimeout: 100 * time.Millisecond}}
			path := filepath.Join(s.Coordinator.Dir, "config.json")
			before := []byte(`{"version":1,"ranges":[{"start":1,"end":1}]}`)
			if exists {
				if err := os.WriteFile(path, before, 0600); err != nil {
					t.Fatal(err)
				}
			}
			abort := errors.New("operation refused")
			changed, total, err := s.Mutate(context.Background(), func(p *ports.Pool) (int, error) { p.Apply([]ports.Range{{Start: 2, End: 2}}, true); return 1, abort })
			if !errors.Is(err, abort) || changed != 0 || total != 0 {
				t.Fatalf("%d %d %v", changed, total, err)
			}
			after, err := os.ReadFile(path)
			if exists {
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("failed callback saved: %s %v", after, err)
				}
			} else if !os.IsNotExist(err) {
				t.Fatalf("failed callback created config: %v", err)
			}
			// Use an independent coordinator, not the same lock instance.
			other := state.Coordinator{Dir: s.Coordinator.Dir, LockTimeout: 100 * time.Millisecond}
			if err := other.WithLock(context.Background(), func() error { return nil }); err != nil {
				t.Fatalf("callback failure leaked lock: %v", err)
			}
		})
	}
}

func TestReadFailureReleasesSharedLock(t *testing.T) {
	s := Store{Coordinator: state.Coordinator{Dir: t.TempDir(), LockTimeout: 100 * time.Millisecond}}
	if err := os.WriteFile(filepath.Join(s.Coordinator.Dir, "config.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Mutate(context.Background(), func(*ports.Pool) (int, error) { t.Fatal("callback ran"); return 0, nil }); err == nil {
		t.Fatal("accepted broken config")
	}
	if err := s.Coordinator.WithLock(context.Background(), func() error { return nil }); err != nil {
		t.Fatalf("read error leaked lock: %v", err)
	}
}

// TestIndependentStateOperation is a subprocess-only fixture: it holds the
// shared lock while touching an unrelated test file, without calling config.
func TestIndependentStateOperation(t *testing.T) {
	dir := os.Getenv("PORTBOOK_STATE_HELPER_DIR")
	if dir == "" {
		t.Skip("subprocess helper")
	}
	coordinator := state.Coordinator{Dir: dir}
	if err := coordinator.WithLock(context.Background(), func() error {
		if err := os.WriteFile(filepath.Join(dir, "simulated-state"), []byte("independent operation"), 0600); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(os.Stdout, "locked"); err != nil {
			return err
		}
		_, err := io.Copy(io.Discard, os.Stdin)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestConfigAndIndependentProcessShareOneLock(t *testing.T) {
	dir := t.TempDir()
	s := Store{Coordinator: state.Coordinator{Dir: dir, LockTimeout: 50 * time.Millisecond}}
	path := filepath.Join(dir, "config.json")
	before := []byte(`{"version":1,"ranges":[]}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIndependentStateOperation$")
	cmd.Env = append(os.Environ(), "PORTBOOK_STATE_HELPER_DIR="+dir)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diag bytes.Buffer
	cmd.Stderr = &diag
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		input.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	if line, err := bufio.NewReader(output).ReadString('\n'); err != nil || line != "locked\n" {
		t.Fatalf("helper: %q %v %s", line, err, &diag)
	}
	called := false
	if _, _, err := s.Mutate(context.Background(), func(*ports.Pool) (int, error) { called = true; return 0, nil }); err == nil || called {
		t.Fatalf("config bypassed shared lock: called=%v err=%v", called, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("simulated operation rewrote config: %q %v", after, err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper failed: %v %s", err, &diag)
	}
	if _, _, err := s.Mutate(context.Background(), func(p *ports.Pool) (int, error) { return p.Apply([]ports.Range{{Start: 1, End: 1}}, true), nil }); err != nil {
		t.Fatalf("independent process leaked lock: %v", err)
	}
}
