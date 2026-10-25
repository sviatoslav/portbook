# Portbook

Give each local service a stable TCP port. Portbook keeps reservations by
project directory and service name, so projects, worktrees, and coding agents
can share one machine.

## Install

```sh
brew install sviatoslav/tap/portbook
```

Available for macOS and Linux on x86-64 and ARM64.
See [installation](docs/INSTALLING.md) for binaries, source builds, and updates.

## Usage

From your project directory, reserve a port, read it on later starts, and release
the booking when you're finished:

```sh
portbook reserve api
portbook get api
portbook release api
```

Both `reserve` and `get` print the port number to use in your application's startup
command. Keep the booking between restarts; stop the service through its usual
shutdown command before releasing the booking.

Each directory has its own bookings. By default, Portbook chooses the lowest
available port in **49152–65535**. Start your service promptly after reserving:
other programs can still bind the port.

## Everyday commands

Run each command as `portbook COMMAND`. Brackets mark optional inputs.

| Command | What it does |
| --- | --- |
| `reserve SERVICE` | Reserve a port for a named service. |
| `get SERVICE` | Print the service's saved port. |
| `release SERVICE` or `release --all` | Release one or every booking for this directory. |
| `list [--all]` | Show this directory's bookings, or bookings across all directories. |
| `cleanup [--dry-run]` | Clean up bookings for missing directories, or preview the cleanup. |
| `install-skills [--agent codex\|claude-code]` | Install the Portbook agent skill; defaults to Codex. |

## More

- [Command reference](docs/REFERENCE.md): configuration, flags, output formats, and troubleshooting.
- [Agent setup](docs/AGENT-SETUP.md): install the skill and define project conventions.
- [Development](docs/DEVELOPING.md) and [releasing](docs/RELEASING.md): contributor guides.

[MIT License](LICENSE).
