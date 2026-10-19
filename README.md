# Portbook

Portbook coordinates local TCP port reservations across directories, projects,
worktrees, and coding agents. Give each service a name and keep its port booked
until you release it, so cooperating projects avoid each other's ports.

## Installation

```sh
brew install sviatoslav/tap/portbook
```

No Go installation is required. Homebrew and [release downloads](https://github.com/sviatoslav/portbook/releases)
support macOS and Linux on Intel/AMD x86-64 and ARM64.
See [installation, source builds, updates, and uninstalling](docs/INSTALLING.md).

These commands target **Portbook v0.2.0 or newer**. Check `portbook --version`
and see the [release notes and migration](docs/RELEASING.md#v020-release-notes-and-migration)
when upgrading from v0.1.0. Select an available version from
[GitHub Releases](https://github.com/sviatoslav/portbook/releases).

## Commands

Run commands as `portbook COMMAND`. Brackets mark optional inputs.

### Everyday commands

| Command | Arguments/options | Description |
| --- | --- | --- |
| `reserve` | `SERVICE` | Create a reservation; fail if it already exists. |
| `get` | `SERVICE` | Print an existing reservation's port; fail if missing. |
| `release` | `SERVICE` or `--all` | Remove one booking or all bookings for this directory; fail if none match. |
| `list` | `[--directory ABSOLUTE_DIRECTORY \| --all]` | Show bookings for this directory or all directories. |
| `cleanup` | `[--dry-run]` | Remove safe-to-clean bookings for confirmed missing directories. |
| `install-skills` | `[--agent codex\|claude-code]` | Install the published Portbook skill globally; requires Node.js/npx. |

`get`, `reserve`, and `release` also accept `--directory ABSOLUTE_DIRECTORY`.
Omit `--directory` to use the current directory. A service name is required,
even for a project with one port; use a name such as `app` or follow your project’s conventions.
`release --all` removes every service booking for only the selected directory.
`list` also defaults to the current directory.

### Configuration

The default pool is **49152–65535** (16,384 ports). No setup is needed.

| Command | Arguments/options | Description |
| --- | --- | --- |
| `config show` | — | Show the allowed pool and port count. |
| `config add` | `PORT\|START-END [PORT\|START-END ...]` | Add ports to the pool. |
| `config remove` | `PORT\|START-END [PORT\|START-END ...]` or `--all` | Remove ports or clear the pool. |
| `config path get` | — | Show the effective configuration location. |
| `config path set` | `ABSOLUTE_DIRECTORY` | Save a configuration location without moving files. |
| `config path reset` | — | Remove the saved location choice. |

Ranges are inclusive. Additions form a union; removals subtract ports.
Pool changes preserve existing bookings. See the [configuration guide](docs/REFERENCE.md#configuring-the-port-pool)
for custom pools and location selection.

### Shared flags and help

| Flag | Arguments/options | Description |
| --- | --- | --- |
| `--output` | `table` or `json` | Choose output format; `get` and `reserve` still print only a port. |
| `--config-dir` | `ABSOLUTE_DIRECTORY` | Override pool configuration for this invocation. |
| `--help`, `-h` | On any command | Show help for that command. |
| `--version` | On `portbook` only | Print the version and commit. |

Other Portbook results default to tables on a terminal and JSON when piped.
`install-skills` passes through third-party prompts and output unchanged.
Configuration overrides never move the shared booking registry.

## Usage examples

Create a booking, read it from another shell or agent in the same directory, then
release its booking when you no longer need it. These examples run from
`/projects/my-app`; ports are illustrative:

```console
$ portbook reserve app
49152
$ portbook get app
49152
$ portbook --output table release app
Released port 49152 for "app" in "/projects/my-app".
```

Keep separate services in the same project:

```console
$ portbook reserve api
49152
$ portbook reserve web
49153
$ portbook --output table list
SERVICE  PORT
api      49152
web      49153
```

Use an existing reservation in a script, or inspect bookings across projects:

```sh
PORT=$(portbook get api)
portbook get web --directory /absolute/path/to/project
portbook list --all --output json
portbook cleanup --dry-run
```

Before deleting a worktree, remove its bookings while the directory still exists:

```sh
portbook release --all --directory /absolute/path/to/worktree
```

## Practical rules

- Run from the intended directory, or pass an absolute path to an existing
  directory. Each nested directory is a separate owner; Portbook does not find Git roots.
- Release removes bookings even if a port is busy; it never stops a listener.
  Stop services separately when finished. Bookings do not expire.
- New reservations choose the lowest allowed port that is unbooked and bindable.
  Portbook does not keep a socket open; start your service promptly.
- Preview cleanup first. Existing directories keep their bookings even when idle;
  cleanup only removes confirmed missing owners with available ports.

## Further reading

- [Agent setup](docs/AGENT-SETUP.md): install the skill and document service names and permissions.
- [Installation](docs/INSTALLING.md): binaries, checksums, source builds, updates, uninstalling.
- [Reference and troubleshooting](docs/REFERENCE.md): command behavior, cleanup rules,
  JSON/errors, configuration locations, storage, and concurrency.
- [Development](docs/DEVELOPING.md) and [releasing](docs/RELEASING.md).

Portbook is available under the [MIT License](LICENSE).
