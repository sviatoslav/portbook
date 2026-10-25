# Command reference

[README](../README.md) · [Installation](INSTALLING.md) · [Agent setup](AGENT-SETUP.md)

This reference describes the current source tree. Use `portbook --help` for an
overview or `portbook COMMAND --help` for a command's options on your installed
build. `-h` is the short form of `--help`. Run `portbook --version` to identify
your build.

## Bookings and directories

A booking belongs to a directory and a named service. Commands use the current
directory by default; each subdirectory is a separate owner. Add
`--directory /absolute/path` to select an existing, readable directory. Symlinks
and filesystem case aliases identify the same physical directory.

Choose a service name such as `api`, `web`, or `app`. Names are case-sensitive,
nonempty Unicode strings excluding NUL. Place `--` before a name starting with
`-`, for example `portbook get -- -api`.

| Command | Result |
| --- | --- |
| `portbook reserve SERVICE` | Create a booking using the lowest available port in the pool. |
| `portbook get SERVICE` | Read the port from an existing booking. |
| `portbook release SERVICE` | Remove one booking for the selected owner. |
| `portbook release --all` | Remove every booking for the selected owner. |
| `portbook list` | List the selected owner's bookings. |
| `portbook list --all` | List every owner's bookings, including deleted directories. |

`get`, `reserve`, `release`, and scoped `list` accept `--directory`.
Choose one service or `--all` for release, and choose `--directory` or `--all`
for listing.

`reserve` reports `reservation already exists` for a duplicate. Use `get` to
reuse that booking. `get` and `release` report `reservation not found` when the
requested booking is missing; bulk release reports the same error for an empty owner.

Reservations persist between server runs. `get` returns the saved port even
when it is busy or outside the current pool. New reservations check all bookings
and briefly test IPv4/IPv6 TCP availability. Other programs can bind the port
after that check, so start your service promptly.

Release removes booking records; running services continue until you stop them
through their own shutdown command. Before removing a worktree, run:

```sh
portbook release --all --directory /absolute/path/to/worktree
```

## Cleanup

Preview abandoned bookings, then apply cleanup:

```sh
portbook cleanup --dry-run
portbook cleanup
```

Cleanup removes a booking when the recorded owner's final directory component
is confirmed missing and its port is available. It retains existing owners,
busy ports, and uncertain filesystem evidence, including missing ancestors,
changed symlinks, inaccessible paths, and removable-volume roots. Run cleanup
while project volumes are mounted; its evidence comes from the current filesystem.

The report gives a row for each booking plus `removed`, `would_remove`, and
`retained` totals. Actions are `removed`, `would-remove`, or `retained`.
Reasons are `owner_missing`, `owner_exists`, `owner_not_directory`,
`owner_uncertain`, `port_busy_or_unavailable`, or `port_unknown`.

## Install the agent skill

Run `portbook install-skills --agent codex` or
`portbook install-skills --agent claude-code`. The default target is Codex.
Installation requires Node.js with npm/npx and makes the skill available across
projects. See [agent setup](AGENT-SETUP.md) for installation and project conventions.

## Configure the port pool

The default pool is **49152–65535**, containing 16,384 ports. A saved configuration
supplies the complete pool, including an intentionally empty pool. Pool changes
preserve existing bookings.

| Command | Result |
| --- | --- |
| `portbook config show` | Show normalized ranges and the total port count. |
| `portbook config add PORT\|START-END [...]` | Add ports to the current pool. |
| `portbook config remove PORT\|START-END [...]` | Remove ports from the current pool. |
| `portbook config remove --all` | Save an empty pool, pausing new reservations. |
| `portbook config reset` | Save the default pool. |

Port operands are decimal integers from 1 through 65535. Ranges include both
endpoints. Overlapping ranges form a set; displayed ranges are sorted and merged.
Confirmations count changed port memberships. Repeating an already-applied change
reports zero changes; reset counts both additions and removals.

To replace the default pool with a custom range:

```sh
portbook config remove --all
portbook config add 50000-50499 51000
portbook config show --output table
```

To restore defaults, run `portbook config reset`.

## Configuration locations

Use `portbook config path get` to see the selected directory, `config.json` path,
and selection source. Configuration selection follows this priority:

1. `--config-dir /absolute/path` for the current command.
2. `PORTBOOK_CONFIG_DIR` from the environment.
3. The directory saved by `config path set`.
4. The OS default below.

| OS | Portbook state directory |
| --- | --- |
| macOS | `~/Library/Application Support/portbook` |
| Linux | `$XDG_CONFIG_HOME/portbook`, or `~/.config/portbook` by default |

`bookings.json`, `location.json`, and `state.lock` stay in this state directory.
The selected configuration directory contains the pool's `config.json`.

Save a location or return to the default selection:

```sh
portbook config path set /absolute/path/to/config-directory
portbook config path get
portbook config path reset
```

Use an absolute UTF-8 directory path. A new location starts with the default
pool. Each location keeps its own configuration; switching back uses its saved
pool. Path reset removes the saved choice, while environment and flag overrides
remain active. `config reset` restores pool contents in the selected location.

For a temporary selection:

```sh
portbook --config-dir /absolute/path/to/config-directory config show
```

## Output and scripting

| Commands | Successful output |
| --- | --- |
| `get`, `reserve` | Decimal port plus newline. |
| `release`, config add/remove/reset, config path set/reset | Plain-text confirmation. |
| `list`, `cleanup`, `config show`, `config path get` | Table/text in a terminal; JSON when piped. |
| `install-skills` | Installer prompts and output. |
| `--version` | Version and commit as plain text. |

The four structured commands accept `--output table` or `--output json` after
the complete command path:

```sh
portbook list --all --output json
portbook config show --output json
portbook config path get --output table
```

Supply each option once. Global `--config-dir` can appear before or after the
command path. `--` ends option parsing. Help validates supplied options and
operands, and permits missing required operands.

Success goes to stdout; errors go to stderr. Help and errors use text when stdout is a terminal, and JSON otherwise. A supported `--output` also selects their format.
The skill installer passes through its own streams.

| Exit code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | Storage or runtime error, including a duplicate or missing booking. |
| `2` | Invalid command, option, or operand. |

A JSON error has this shape; `code` is `usage_error`, `storage_error`, or `runtime_error`:

```json
{"error":{"code":"runtime_error","message":"reservation not found for service ..."}}
```

Structured success fields:

| Command | JSON fields |
| --- | --- |
| `list` | `command`, `bookings`: rows with `directory`, `service`, `port`. |
| `config show` | `command`, `ranges`: rows with `start`, `end`; `total`. |
| `config path get` | `command`, `directory`, `config_file`, `source`. |
| `cleanup` | `command`, `dry_run`, `rows`, `removed`, `would_remove`, `retained`; each row has `directory`, `service`, `port`, `action`, `reason`. |

`command` contains the command path, such as `config show`. Path sources are
`flag`, `environment`, `saved`, or `default`. Empty arrays remain `[]`.
Booking and cleanup rows sort by directory, then service. Human output escapes
control characters and ambiguous names; JSON preserves the exact values.

## Troubleshooting

| Situation | Next step |
| --- | --- |
| Booking already exists | Read it with `get SERVICE`. To choose a new port, release it and reserve again. |
| Booking is missing | Check the owner directory and service with `list` or `list --all`. |
| Pool is exhausted | Inspect `config show` and `list --all`; restore defaults or extend the pool as appropriate. |
| Cleanup retains a booking | Read its reason and check the owner, parent directories, mounts, and running service. |
| Storage error | Inspect the reported path and permissions. Preserve the file while investigating its contents. |
| Saved location is invalid | Select a valid directory with `config path set`, or use `config path reset`. |
| Lock timeout | Retry after the active mutation finishes; lock acquisition waits up to five seconds. |
| Save or output error | Read the resulting state before retrying: the change may already have been saved. |
| Installed behavior differs | Check `command -v portbook`, `portbook --version`, and scoped help; follow the [installation guide](INSTALLING.md) to update or build this source. |
