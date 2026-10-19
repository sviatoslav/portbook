# Set up Portbook for coding agents

Install the Portbook binary separately from the reusable agent skill:

```sh
brew install sviatoslav/tap/portbook
portbook --version
```

The published v0.1.0 binary has neither `get` nor `install-skills` and uses the
older create-or-return lifecycle. This guide describes unreleased source work
based on the merged explicit-service lifecycle. Build from a checkout containing
the installer change to try it now:

```sh
go build -o bin/portbook ./cmd/portbook
./bin/portbook --output table get --help
./bin/portbook --output table reserve --help
./bin/portbook --output table release --help
```

This checkout supports the required named-service lifecycle, booking-only release,
owner-scoped `release --all`, and the installer. Check version and scoped help
when using another binary; published v0.1.0 cannot run this workflow. The reusable
skill is already published in the [skills repository](https://github.com/sviatoslav/portbook-skills)
and can be installed independently. Use it only with a compatible Portbook build.

## Install the skill

Install Node.js with npm/npx, then use a compatible Portbook build:

```sh
portbook install-skills
portbook install-skills --agent claude-code
```

The accepted targets are `codex` (default) and `claude-code`, one per invocation.
Installation is global for the current user, available across projects. Portbook
delegates to the third-party [Vercel Skills CLI](https://github.com/vercel-labs/skills),
with inherited stdin/stdout/stderr and a five-minute timeout. Its equivalent
command is:

```sh
npx --yes skills add sviatoslav/portbook-skills --skill portbook --global --agent codex
```

The first `--yes` allows npm to download/run Skills CLI. Portbook leaves Skills
CLI's confirmation enabled; it does not append a final `--yes`. Run in an
interactive terminal to answer prompts; closed stdin can leave an installation
unperformed even when upstream exits successfully. **Reinstalling can replace
existing skill files, including local edits.** Upstream can also run without confirmation when it
detects a coding agent. Portbook does not add overwrite protection. Review or
back up edits before reinstalling. The installer downloads third-party code and
uses its installation, network, and replacement behavior; it is not bundled.

Only the `portbook` skill is selected, not future skills in the skills repository.
Vercel currently uses `~/.agents/skills/` for Codex and `~/.claude/skills/` for
Claude Code; upstream controls exact destinations and copy/symlink behavior.
See official [Codex skills guidance](https://learn.chatgpt.com/docs/build-skills)
and [Claude Code skills guidance](https://code.claude.com/docs/en/skills).

Installer output passes through unchanged even with `--output json`; do not parse
it as a Portbook JSON result. Flag/help errors retain Portbook's usual diagnostics
and exit 2. Launch, timeout, and external failures exit 1 with a `runtime_error`
diagnostic after any upstream output. Portbook cancels the installer process
group after five minutes or context cancellation or termination; remaining group members are cleaned up if the
launcher exits first. An otherwise successful launcher that leaves active children
reports an error rather than claiming a completed installation. Upstream controls
its own normal interactive behavior.
`--config-dir` is validated if supplied but does not select a skill destination.
Portbook's installer reads or changes no pool, pointer, bookings, or lock files.

To test skill changes locally, substitute an absolute checkout path for
`sviatoslav/portbook-skills` in the direct npx command. For development validation, isolate
HOME, XDG and agent-specific environment paths in a temporary directory; do not
install into a developer's real agent directories.

## Document each project's choices

In AGENTS.md **or linked repository documentation**, record:

- The stable explicit service names, including for a single-port project.
- How to determine the intended owner directory for each worktree, including when
  the agent starts in a subfolder. Use checkout-relative descriptions, not fixed
  developer-machine paths.
- Whether agents may create a missing reservation automatically or must ask.
- How the project accepts a startup port (environment variable or command flag).

Use existing project conventions. Names such as `api` or `web` in examples are
illustrative, not imposed names. If names or scope are unclear, the skill asks.
No `.portbook.json` manifest or global pool setup is required. The skill discovers
bookings with scoped list, reads a named reservation with get, and allocates only
after a positive missing result and permission. Server shutdown preserves stable
reservations; release and cleanup are deliberate operations.

For authorized worktree deletion, the skill automatically removes only that
owner's bookings before removing its existing directory, when the required
release features are available:

```sh
portbook release --all --directory "$owner"
```

The owner comes from the documented worktree scope. Running `release --all`
inside that exact directory is equivalent; running in a subfolder is not. Bulk
release uses one lock and atomic save, preserving other owners. Booking-only
release deletes bookings even for busy/uncertain ports without stopping their
listeners. Stopping services is a separate task-specific responsibility, not a
release prerequisite. No repeated approval is needed for scoped teardown already
covered by authorized deletion. Single-service release still requires its name;
missing single bookings and empty bulk targets report errors. Surface errors
before directory removal, preserving corrupt/inaccessible evidence.

Chat archive alone does not authorize directory deletion or teardown. Keep stable
bookings for stopped servers and existing directories outside an intended removal
task. If the owner is already gone, release cannot resolve it; report the
limitation without inventing flags or substituting global cleanup. No periodic
cleanup, hook, or daemon is added.
