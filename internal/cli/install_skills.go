package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// Dependencies belong to each invocation so tests need no shared mutable hooks.
type skillInstaller struct {
	lookup  func(string) (string, error)
	run     func(context.Context, string, ...string) error
	timeout time.Duration
}

func defaultSkillInstaller(in io.Reader, out, diagnostics io.Writer) skillInstaller {
	return skillInstaller{
		lookup:  exec.LookPath,
		timeout: 5 * time.Minute,
		run: func(ctx context.Context, name string, args ...string) (runErr error) {
			ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer stop()
			cmd := exec.CommandContext(ctx, name, args...)
			// npm spawns the Skills CLI. Cancel the whole group on our supported
			// macOS/Linux targets so installation cannot continue after timeout.
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			if terminal, ok := in.(*os.File); ok && term.IsTerminal(int(terminal.Fd())) {
				fd := int(terminal.Fd())
				foreground, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
				if err != nil {
					return fmt.Errorf("inspect installer terminal: %w", err)
				}
				if foreground != syscall.Getpgrp() {
					return fmt.Errorf("run install-skills from the foreground terminal")
				}
				// A new background group would be stopped by SIGTTIN on prompt
				// input. Lend it the foreground terminal, then restore our group.
				cmd.SysProcAttr.Foreground, cmd.SysProcAttr.Ctty = true, fd
				defer func() {
					// Ignore SIGTTOU for this ioctl; catching it is insufficient
					// for orphaned groups (the kernel returns EIO). The CLI has
					// no custom SIGTTOU handler; retain an inherited ignore.
					ignored := signal.Ignored(syscall.SIGTTOU)
					signal.Ignore(syscall.SIGTTOU)
					if !ignored {
						defer signal.Reset(syscall.SIGTTOU)
					}
					if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPGRP, foreground); err != nil {
						runErr = errors.Join(runErr, fmt.Errorf("restore installer terminal: %w", err))
					}
				}()
			}
			cmd.Cancel = func() error {
				err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				if err == syscall.ESRCH {
					return os.ErrProcessDone
				}
				return err
			}
			cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, diagnostics
			// Bound pipe draining if a descendant keeps inherited streams open.
			cmd.WaitDelay = time.Second
			runErr = cmd.Run()
			// A launcher can exit before descendants, including when stdio is wired
			// directly to terminal files. No installation work may outlive this command.
			if cmd.Process != nil {
				if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err == nil && runErr == nil {
					runErr = errors.New("installer launcher exited before its child processes; stopped remaining installer processes")
				} else if err != nil && err != syscall.ESRCH {
					runErr = errors.Join(runErr, fmt.Errorf("clean up installer processes: %w", err))
				}
			}
			return runErr
		},
	}
}

func installSkillsCommand(o *options) *cobra.Command {
	var agent string
	cmd := o.command(&cobra.Command{
		Use: "install-skills", Short: "Install the Portbook agent skill globally using npx skills",
		Long:    "Install only the portbook skill from sviatoslav/portbook-skills using the third-party Vercel Skills CLI. Requires Node.js/npx and network access. Targets codex (default) or claude-code globally. Reinstallation can replace edited skill files. Installer prompts and output pass through unchanged; --output is not accepted; upstream may skip prompts inside coding agents. No Portbook config or bookings are read or changed. Stops after five minutes.",
		Example: "  portbook install-skills\n  portbook install-skills --agent claude-code",
		Args: func(cmd *cobra.Command, args []string) error {
			if err := cobra.NoArgs(cmd, args); err != nil {
				return err
			}
			if agent != "codex" && agent != "claude-code" {
				return fmt.Errorf("invalid agent %q; expected codex or claude-code", agent)
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := o.validate(); err != nil {
				return err
			}
			if err := o.installer.install(cmd.Context(), agent); err != nil {
				return &commandError{code: "runtime_error", err: fmt.Errorf("install skills: %w", err)}
			}
			return nil
		},
	}, commandPolicy{help: helpIndependent})
	cmd.Flags().StringVar(&agent, "agent", "codex", "Target agent: codex or claude-code")
	once(cmd.Flags().Lookup("agent"))
	return cmd
}

func (s skillInstaller) install(parent context.Context, agent string) error {
	ctx, cancel := context.WithTimeout(parent, s.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.lookup("npx")
	if err != nil {
		return fmt.Errorf("npx not found; install Node.js (including npm/npx) and ensure npx is on PATH")
	}
	// --yes allows npm to fetch the CLI. Skills CLI confirmation remains
	// enabled; no final --yes and no shell interpolation are used.
	args := []string{"--yes", "skills", "add", "sviatoslav/portbook-skills", "--skill", "portbook", "--global", "--agent", agent}
	err = s.run(ctx, path, args...)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
