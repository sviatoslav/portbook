package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"portbook/internal/booking"
	"portbook/internal/state"
)

func TestReleaseOutputDefaultsAndWriteErrors(t *testing.T) {
	owner := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", "relative ignored")
	for _, terminal := range []bool{false, true} {
		var out, diag bytes.Buffer
		if code := Run(context.Background(), []string{"release", "--directory", owner}, &out, &diag, terminal); code != 0 || diag.Len() != 0 {
			t.Fatal(code, diag.String())
		}
		if terminal {
			if !bytes.Contains(out.Bytes(), []byte("No booking")) {
				t.Fatal(out.String())
			}
		} else {
			var doc map[string]any
			if err := json.Unmarshal(out.Bytes(), &doc); err != nil || len(doc) != 5 || doc["port"] != nil || doc["released"] != false {
				t.Fatal(out.String(), err)
			}
		}
	}
	var diag bytes.Buffer
	if code := Run(context.Background(), []string{"release", "--directory", owner}, releaseFailWriter{}, &diag, false); code != 1 || !bytes.Contains(diag.Bytes(), []byte(`"code":"runtime_error"`)) {
		t.Fatal(code, diag.String())
	}
	port := 45000
	for _, format := range []string{"table", "json"} {
		if err := renderRelease(releaseFailWriter{}, format, booking.ReleaseResult{Directory: owner, Service: "default", Port: &port, Released: true}); !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal(err)
		}
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
		if code := Run(context.Background(), []string{"release", "--help"}, &out, &diag, false); code != 0 || diag.Len() != 0 || !bytes.Contains(out.Bytes(), []byte("release [SERVICE]")) {
			t.Fatalf("selection %q: code=%d out=%q err=%q", selection, code, out.String(), diag.String())
		}
	}
}

func TestReleaseTableEscapesIdentitiesAndOutputFailureKeepsCommit(t *testing.T) {
	// Valid service and directory identities may include terminal control bytes.
	port := 45000
	res := booking.ReleaseResult{Directory: "/tmp/owner\n\x1b[31m\u2028\u202e\"\\", Service: "server\t\x1b[2J\u2029\u2066\"\\", Port: &port, Released: true}
	for _, released := range []bool{false, true} {
		res.Released = released
		var out bytes.Buffer
		if err := renderRelease(&out, "table", res); err != nil {
			t.Fatal(err)
		}
		if strings.Count(out.String(), "\n") != 1 || strings.ContainsAny(strings.TrimSuffix(out.String(), "\n"), "\t\x1b\u2028\u2029\u202e\u2066") {
			t.Fatal("unescaped identity", out.String())
		}
	}
	var jsonOut bytes.Buffer
	if err := renderRelease(&jsonOut, "json", res); err != nil {
		t.Fatal(err)
	}
	var roundTrip booking.ReleaseResult
	if err := json.Unmarshal(jsonOut.Bytes(), &roundTrip); err != nil || roundTrip.Directory != res.Directory || roundTrip.Service != res.Service {
		t.Fatal("JSON changed identities", jsonOut.String(), err)
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
	if code := Run(context.Background(), []string{"release", "--directory", owner}, releaseFailWriter{}, &diag, false); code != 1 || !bytes.Contains(diag.Bytes(), []byte(`"code":"runtime_error"`)) {
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
