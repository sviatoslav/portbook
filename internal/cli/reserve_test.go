package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sviatoslav/portbook/internal/config"
	"github.com/sviatoslav/portbook/internal/ports"
	"github.com/sviatoslav/portbook/internal/state"
)

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }
func TestGetPlainOutputAndOutputFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	c, err := state.Default()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := cfg.Mutate(context.Background(), func(p *ports.Pool) (int, error) { *p = ports.Pool{}; return 0, nil }); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(c.Dir, 0700)
	owner := t.TempDir()
	physical, err := filepath.EvalSymlinks(owner)
	if err != nil {
		t.Fatal(err)
	}
	// Seed a valid booking to isolate output from availability and terminal state.
	data := []byte(fmt.Sprintf(`{"version":1,"bookings":{%q:{"default":54321}}}`, physical))
	path := filepath.Join(c.Dir, "bookings.json")
	os.WriteFile(path, data, 0600)
	for _, terminal := range []bool{true, false} {
		args := []string{"get", "default", "--directory", owner}
		var out, diag bytes.Buffer
		if code := Run(context.Background(), args, &out, &diag, terminal); code != 0 || out.String() != "54321\n" || diag.Len() != 0 {
			t.Fatal(code, out.String(), diag.String())
		}
	}
	var diag bytes.Buffer
	if code := Run(context.Background(), []string{"get", "default", "--directory", owner}, failedWriter{}, &diag, false); code != 1 {
		t.Fatal(code, diag.String())
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("stdout failure removed booking", err)
	}
	// A pre-existing booking is preserved even when the configured pool is empty.
	s, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	pool, err := s.Read()
	if err != nil || pool.Count() != 0 {
		t.Fatal(err)
	}
}

func TestReserveNewBookingSurvivesOutputFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	var out, diag bytes.Buffer
	if code := Run(context.Background(), []string{"config", "remove", "--all"}, &out, &diag, false); code != 0 {
		t.Fatal(code, diag.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"config", "add", fmt.Sprint(port)}, &out, &diag, false); code != 0 {
		t.Fatal(code, diag.String())
	}
	owner := t.TempDir()
	args := []string{"reserve", "default", "--directory", owner}
	diag.Reset()
	if code := Run(context.Background(), args, failedWriter{}, &diag, false); code != 1 || !strings.Contains(diag.String(), `"code":"runtime_error"`) {
		t.Fatal(code, diag.String())
	}
	out.Reset()
	diag.Reset()
	args[0] = "get"
	if code := Run(context.Background(), args, &out, &diag, false); code != 0 || out.String() != fmt.Sprint(port)+"\n" {
		t.Fatal(code, out.String(), diag.String())
	}
}

func TestReserveDiagnosticFormats(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("PORTBOOK_CONFIG_DIR", t.TempDir())
	for _, terminal := range []bool{false, true} {
		for _, tc := range []struct {
			args []string
			json bool
		}{
			{[]string{"reserve", "a", "b"}, !terminal},
			{[]string{"reserve", "default", "--directory=relative", "--output=table"}, !terminal},
			{[]string{"reserve", "default", "--directory=relative", "--output=json"}, !terminal},
			{[]string{"reserve", "--bogus", "--output=table", "--output=json"}, !terminal},
			{[]string{"reserve", "a", "--", "--output=json"}, !terminal},
		} {
			var out, diag bytes.Buffer
			if code := Run(context.Background(), tc.args, &out, &diag, terminal); code != 2 || out.Len() != 0 {
				t.Fatal(code, out.String(), diag.String())
			}
			if strings.HasPrefix(diag.String(), "{") != tc.json {
				t.Fatal(tc.args, diag.String())
			}
		}
	}
}
