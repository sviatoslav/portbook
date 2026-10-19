package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "portbook-integration-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "portbook")
	build := exec.Command("go", "build", "-o", binary, "./cmd/portbook")
	build.Dir = "../.."
	if output, err := build.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		fmt.Fprintf(os.Stderr, "build CLI: %v\n%s", err, output)
		os.Exit(1)
	}
	// Isolate default coordination for in-process CLI tests too.
	os.Setenv("HOME", filepath.Join(dir, "home"))
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func command(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command(binary, args...)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "PORTBOOK_CONFIG_DIR=") && !strings.HasPrefix(env, "HOME=") && !strings.HasPrefix(env, "XDG_CONFIG_HOME=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "PORTBOOK_CONFIG_DIR="+dir, "HOME="+dir+"-home", "XDG_CONFIG_HOME="+dir+"-xdg")
	return cmd
}

func run(t *testing.T, dir string, wantCode int, args ...string) string {
	t.Helper()
	var out, diag bytes.Buffer
	cmd := command(dir, args...)
	cmd.Stdout = &out
	cmd.Stderr = &diag
	err := cmd.Run()
	code := 0
	if err != nil {
		exit, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatal(err)
		}
		code = exit.ExitCode()
	}
	if code != wantCode {
		t.Fatalf("%v: code %d want %d; out=%s err=%s", args, code, wantCode, &out, &diag)
	}
	if code == 0 {
		if diag.Len() != 0 {
			t.Fatalf("success stderr: %s", &diag)
		}
		return out.String()
	}
	if out.Len() != 0 {
		t.Fatalf("failure stdout: %s", &out)
	}
	return diag.String()
}

func TestAgreedSequence(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	for _, s := range []struct {
		args []string
		want string
	}{
		{[]string{"config", "show"}, "No port ranges configured.\nTotal: 0 ports.\n"},
		{[]string{"config", "ranges", "add", "50000-50099"}, "Added 100 ports. Total: 100 ports.\n"},
		{[]string{"config", "ranges", "add", "50050-50149"}, "Added 50 ports. Total: 150 ports.\n"},
		{[]string{"config", "show"}, "RANGE PORTS 50000-50149 150 Total: 150 ports."},
		{[]string{"config", "ranges", "remove", "50040-50059"}, "Removed 20 ports. Total: 130 ports.\n"},
		{[]string{"config", "show"}, "RANGE PORTS 50000-50039 40 50060-50149 90 Total: 130 ports."},
		{[]string{"config", "ranges", "remove", "50149"}, "Removed 1 port. Total: 129 ports.\n"},
		{[]string{"config", "ranges", "add", "50000"}, "Added 0 ports. Total: 129 ports.\n"},
		{[]string{"config", "ranges", "remove", "--all"}, "Removed 129 ports. Total: 0 ports.\n"},
	} {
		got := run(t, dir, 0, append([]string{"--output", "table"}, s.args...)...)
		if strings.Join(strings.Fields(got), " ") != strings.Join(strings.Fields(s.want), " ") {
			t.Fatalf("%v: %q != %q", s.args, got, s.want)
		}
	}
	run(t, dir, 0, "config", "ranges", "add", "51000")
	if got := run(t, dir, 0, "--output=table", "config", "show"); !strings.Contains(got, "Total: 1 port.") {
		t.Fatal(got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"version": 1`) {
		t.Fatal(string(data))
	}
}

func TestJSONAndInvalidInputsNoMutation(t *testing.T) {
	dir := t.TempDir()
	if got := run(t, dir, 0, "config", "ranges", "add", "1", "65535"); got != "{\"command\":\"config ranges add\",\"changed\":2,\"total\":2}\n" {
		t.Fatal(got)
	}
	before, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"config", "ranges", "add", "2", "bad"},
		{"config", "ranges", "add", "0"},
		{"config", "ranges", "add", "65536"},
		{"config", "ranges", "remove", "10-1"},
		{"config", "ranges", "remove", "--all", "1"},
		{"config", "ranges", "remove", "1", "--all"},
		{"config", "ranges", "remove", "--all", "--all"},
		{"config", "ranges", "add", "--all"},
		{"config", "ranges", "remove"},
	} {
		got := run(t, dir, 2, args...)
		var diagnostic struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(got), &diagnostic); err != nil || diagnostic.Error.Code != "usage_error" || diagnostic.Error.Message == "" {
			t.Fatal(got, err)
		}
		after, err := os.ReadFile(filepath.Join(dir, "config.json"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("invalid request mutated config", err)
		}
	}
	got := run(t, dir, 0, "config", "show")
	if got != "{\"command\":\"config show\",\"ranges\":[{\"start\":1,\"end\":1},{\"start\":65535,\"end\":65535}],\"total\":2}\n" {
		t.Fatal(got)
	}
}

func TestCorruptConfigPreservedAcrossProcesses(t *testing.T) {
	for _, bad := range []string{
		"{broken evidence\n",
		`{"version":1,"ranges":[{"start":50000,"end":50099}],"ranges":[]}`,
		`{"version":1,"ranges":[{"start":50000,"start":50001,"end":50099}]}`,
	} {
		t.Run(bad, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.json")
			if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				for _, args := range [][]string{{"config", "show"}, {"config", "ranges", "add", "50000"}, {"config", "ranges", "remove", "--all"}} {
					if got := run(t, dir, 1, args...); !strings.Contains(got, `"code":"storage_error"`) {
						t.Fatal(got)
					}
					got, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(got, []byte(bad)) {
						t.Fatal("corrupt file changed", err)
					}
				}
			}
		})
	}
}

func TestIndependentConcurrentProcesses(t *testing.T) {
	dir := t.TempDir()
	const workers = 24
	runBatch := func(remove bool) {
		cmds := make([]*exec.Cmd, workers)
		outs := make([]bytes.Buffer, workers)
		diags := make([]bytes.Buffer, workers)
		for i := 0; i < workers; i++ {
			action := "add"
			operands := []string{fmt.Sprint(51000 + i), "50000-50023"}
			if remove {
				action = "remove"
				operands = []string{fmt.Sprint(51000 + i)}
			}
			cmds[i] = command(dir, append([]string{"config", "ranges", action}, operands...)...)
			cmds[i].Stdout = &outs[i]
			cmds[i].Stderr = &diags[i]
			if err := cmds[i].Start(); err != nil {
				t.Fatal(err)
			}
		}
		changed := 0
		for i, cmd := range cmds {
			if err := cmd.Wait(); err != nil {
				t.Fatalf("worker %d: %v %s", i, err, &diags[i])
			}
			var res struct {
				Changed int `json:"changed"`
			}
			if err := json.Unmarshal(outs[i].Bytes(), &res); err != nil {
				t.Fatal(err)
			}
			changed += res.Changed
		}
		want := workers * 2
		if remove {
			want = workers
		}
		if changed != want {
			t.Fatalf("changed %d want %d", changed, want)
		}
	}
	runBatch(false)
	var state struct {
		Ranges []struct{ Start, End int }
		Total  int
	}
	if err := json.Unmarshal([]byte(run(t, dir, 0, "config", "show")), &state); err != nil {
		t.Fatal(err)
	}
	if state.Total != 48 || len(state.Ranges) != 2 || state.Ranges[1].End != 51023 {
		t.Fatalf("lost writes: %+v", state)
	}
	runBatch(true)
	if err := json.Unmarshal([]byte(run(t, dir, 0, "config", "show")), &state); err != nil {
		t.Fatal(err)
	}
	if state.Total != 24 || !reflect.DeepEqual(state.Ranges, []struct{ Start, End int }{{50000, 50023}}) {
		t.Fatalf("lost removals: %+v", state)
	}
}

func TestStoragePathFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := run(t, path, 1, "config", "ranges", "add", "1"); !strings.Contains(got, `"code":"storage_error"`) {
		t.Fatal(got)
	}
}
