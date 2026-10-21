package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sviatoslav/portbook/internal/booking"
	"github.com/sviatoslav/portbook/internal/state"
)

func TestReleaseOutputDefaultsAndWriteErrors(t *testing.T) {
	owner := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", "relative ignored")
	for _, terminal := range []bool{false, true} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), []string{"release", "default", "--directory", owner}, &out, &diag, terminal); code != 1 || out.Len() != 0 || !strings.Contains(diag.String(), "reservation not found") {
			t.Fatal(code, out.String(), diag.String())
		}
		if !terminal && !strings.Contains(diag.String(), `"code":"runtime_error"`) {
			t.Fatal(diag.String())
		}
	}

	var diag bytes.Buffer
	if code := Run(context.Background(), []string{"release", "default", "--directory", owner}, releaseFailWriter{}, &diag, false); code != 1 || !bytes.Contains(diag.Bytes(), []byte(`"code":"runtime_error"`)) {
		t.Fatal(code, diag.String())
	}
	port := 45000
	if err := renderRelease(releaseFailWriter{}, booking.ReleaseResult{Directory: owner, Service: "default", Port: &port, Released: true}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
}

type releaseFailWriter struct{}

func (releaseFailWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestReleaseHelpIgnoresUnusedConfigEnvironment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, selection := range []string{"relative", "", filepath.Join(t.TempDir(), "file")} {
		if filepath.IsAbs(selection) {
			if err := os.WriteFile(selection, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		t.Setenv("PORTBOOK_CONFIG_DIR", selection)
		var out, diag bytes.Buffer
		if code := Run(context.Background(), []string{"release", "--help"}, &out, &diag, false); code != 0 || diag.Len() != 0 || !bytes.Contains(out.Bytes(), []byte("release SERVICE")) {
			t.Fatalf("selection %q: code=%d out=%q err=%q", selection, code, out.String(), diag.String())
		}
	}
}

func TestReleaseTableEscapesIdentitiesAndOutputFailureKeepsCommit(t *testing.T) {
	// Valid service and directory identities may include terminal control bytes.
	port := 45000
	res := booking.ReleaseResult{Directory: "/tmp/owner\n\x1b[31m\u2028\u202e\"\\", Service: "server\t\x1b[2J\u2029\u2066\"\\", Port: &port, Released: true}
	var table bytes.Buffer
	if err := renderRelease(&table, res); err != nil {
		t.Fatal(err)
	}
	if strings.Count(table.String(), "\n") != 1 || strings.ContainsAny(strings.TrimSuffix(table.String(), "\n"), "\t\x1b\u2028\u2029\u202e\u2066") {
		t.Fatal("unescaped identity", table.String())
	}

	var gotPort int
	var gotService, gotDirectory string
	if _, err := fmt.Sscanf(table.String(), "Released port %d for %q in %q.\n", &gotPort, &gotService, &gotDirectory); err != nil || gotPort != port || gotService != res.Service || gotDirectory != res.Directory {
		t.Fatal("plain confirmation changed identities", table.String(), err)
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", "relative ignored")
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := booking.Directory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port = listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"version": 1, "bookings": booking.Registry{owner: {"default": port}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(c.Dir, booking.File)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	var diag bytes.Buffer
	if code := Run(context.Background(), []string{"release", "default", "--directory", owner}, releaseFailWriter{}, &diag, false); code != 1 || !bytes.Contains(diag.Bytes(), []byte(`"code":"runtime_error"`)) {
		t.Fatal(code, diag.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ Bookings booking.Registry }
	if err := json.Unmarshal(after, &doc); err != nil || len(doc.Bookings) != 0 {
		t.Fatal("output failure undid committed release", string(after), err)
	}
}

func TestReleasePlainSuccessOnTerminalAndPipe(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := booking.Directory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(c.Dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, terminal := range []bool{false, true} {
		for _, all := range []bool{false, true} {
			data, err := json.Marshal(map[string]any{"version": 1, "bookings": booking.Registry{owner: {"api": 45000, "web": 45002}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(c.Dir, booking.File), data, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{"release", "api", "--directory", owner}
			want := fmt.Sprintf("Released port 45000 for %q in %q.\n", "api", owner)
			if all {
				args[1] = "--all"
				want = fmt.Sprintf("Released 2 bookings in %q: 45000, 45002.\n", owner)
			}
			var out, diag bytes.Buffer
			if code := Run(context.Background(), args, &out, &diag, terminal); code != 0 || diag.Len() != 0 || out.String() != want {
				t.Fatal(terminal, all, code, out.String(), diag.String())
			}
		}
	}
}
