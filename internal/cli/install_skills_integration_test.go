package cli_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Python's standard-library pty provides a real controlling terminal without
// adding a PTY library to the CLI. CI and release verification already use Python.
func TestInstallSkillsInteractiveTerminal(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 required for controlling-terminal regression")
	}
	cmd := exec.Command(python, "-c", installerTTYCheck, binary, t.TempDir())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("TTY installation: %v\n%s", err, output)
	}
}

const installerTTYCheck = `
import os, pathlib, pty, select, signal, subprocess, sys, time
binary, root = sys.argv[1], pathlib.Path(sys.argv[2])
fake = root / "npx"
fake.write_text('#!/bin/sh\necho INSTALLER_PROMPT\nread answer\ntest "$answer" = confirmed || exit 7\necho INSTALLER_DONE\n')
fake.chmod(0o700)
pid, master = pty.fork()
if pid == 0:
    env = dict(os.environ, PATH=str(root), HOME=str(root / 'home'),
               XDG_CONFIG_HOME=str(root / 'xdg'), PORTBOOK_CONFIG_DIR='unused-relative')
    before = os.tcgetpgrp(0)
    code = subprocess.call([binary, 'install-skills'], env=env)
    assert code == 0, code
    assert os.tcgetpgrp(0) == before, 'terminal foreground not restored'
    print('HARNESS_PASS', flush=True)
    os._exit(0)
deadline, data, sent, status = time.monotonic() + 8, b'', False, None
try:
    while time.monotonic() < deadline:
        if select.select([master], [], [], 0.05)[0]:
            try: chunk = os.read(master, 65536)
            except OSError: break
            data += chunk
            if b'INSTALLER_PROMPT' in data and not sent:
                os.write(master, b'confirmed\n')
                sent = True
        done, current = os.waitpid(pid, os.WNOHANG)
        if done:
            status = current
            break
    if status is None:
        done, status = os.waitpid(pid, os.WNOHANG)
        if not done:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
            raise AssertionError('terminal prompt hung: ' + repr(data))
    assert os.waitstatus_to_exitcode(status) == 0, repr(data)
    assert b'HARNESS_PASS' in data and b'INSTALLER_DONE' in data, repr(data)
finally:
    os.close(master)
`

func TestInstallSkillsTerminationStopsDescendants(t *testing.T) {
	root := t.TempDir()
	script := "#!/bin/sh\n/bin/sh -c 'echo ready > \"$1\"; /bin/sleep 2; echo survived > \"$2\"' child \"${0%/*}/ready\" \"${0%/*}/survived\" &\nwait\n"
	if err := os.WriteFile(filepath.Join(root, "npx"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := command(root, "install-skills")
	cmd.Env = append(cmd.Env, "PATH="+root)
	var out, diag bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &diag
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	deadline := time.After(8 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatal("installer never started", err, diag.String())
		case <-deadline:
			t.Fatal("installer startup timed out")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || cmd.ProcessState.ExitCode() != 1 || !strings.Contains(diag.String(), `"code":"runtime_error"`) {
			t.Fatal(err, diag.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("termination hung")
	}
	time.Sleep(2200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(root, "survived")); !os.IsNotExist(err) {
		t.Fatal("installer child survived Portbook termination", err)
	}
}
