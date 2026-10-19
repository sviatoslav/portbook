# Portbook

Portbook is a standalone Go CLI for cooperating coding agents on one machine.
This first feature configures the set of allowed TCP ports. The pool starts empty.

Only configuration commands are implemented. Booking, release, owner detection,
OS availability checks, cleanup, and agent skills are future work. There is no
daemon, proxy, database, heartbeat, or expiry mechanism.

## Build and install

Requires Go 1.26 or newer. From this checkout:

```sh
go build -o bin/portbook ./cmd/portbook
./bin/portbook --output table --help
```

To install into your Go binary directory (`GOBIN`, or `$(go env GOPATH)/bin`):

```sh
go install ./cmd/portbook
```

macOS and Linux are the target platforms. The CI workflow runs the tests on both;
local verification in this checkout was performed on macOS. Windows is untested.

## Commands

```sh
portbook config path get
portbook config path set /absolute/path
portbook config path reset
portbook --config-dir /temporary/path config show
portbook config show
portbook config ranges add 50000-50499
portbook config ranges add 51000
portbook config ranges remove 50000-50499
portbook config ranges remove 51000
portbook config ranges remove --all
```

`add` and `remove` accept one or more ports or inclusive `START-END` ranges.
Only decimal integers from 1 through 65535 are allowed; zero, reversed ranges,
whitespace within an operand, signs, and other syntax are rejected. All operands
are validated before any storage operation. `--all` is valid only for `remove`
and cannot be mixed with operands. It clears only the configured pool.

The configuration is one **set of port numbers**. Additions form a union;
removals subtract ports, even across gaps or within a previously added range.
Overlaps are accepted. Counts report actual changes, so adding present ports or
removing absent ports succeeds with zero changes. Stored and displayed ranges
are sorted, disjoint, and merge adjacent intervals. Single ports display as one
number. Configuration changes do not touch other state files; future booking
data will be separate, including bookings outside the configured pool.

An illustrative sequence on a fresh pool, using explicit table output so it is
reproducible when captured:

```text
$ portbook --output table config ranges add 50000-50099
Added 100 ports. Total: 100 ports.
$ portbook --output table config ranges add 50050-50149
Added 50 ports. Total: 150 ports.
$ portbook --output table config show
RANGE        PORTS
50000-50149  150
Total: 150 ports.
$ portbook --output table config ranges remove 50040-50059
Removed 20 ports. Total: 130 ports.
$ portbook --output table config show
RANGE        PORTS
50000-50039  40
50060-50149  90
Total: 130 ports.
$ portbook --output table config ranges remove 50149
Removed 1 port. Total: 129 ports.
$ portbook --output table config ranges add 50000
Added 0 ports. Total: 129 ports.
$ portbook --output table config ranges remove --all
Removed 129 ports. Total: 0 ports.
$ portbook --output table config show
No port ranges configured.
Total: 0 ports.
```

## Output and exit codes

When stdout is a terminal, output is human-readable text/tables. When stdout is
piped, redirected, or otherwise noninteractive, it is JSON. `--output table` or
`--output json` overrides this anywhere in the command; `--output=json` also
works. `--help` / `-h` describes commands and examples and follows this convention
(JSON help has `command` and `help` fields). Help is scoped to the resolved
command: `portbook config ranges remove --help` describes removal and `--all`.
Unknown command paths, unknown/inapplicable flags, invalid supplied operands,
and `--all` mixed with operands still fail with exit 2 when help is requested.
Recognized commands can show help without required operands. Global `--output` and `--config-dir`
can appear before or after the command path; `--all` belongs to `ranges remove`.
Repeated `--output`, `--config-dir`, or `--all` flags are rejected. `--` ends flag parsing, so
`config ranges add -- 51000` works and `--help` after `--` is an operand.
There are no prompts.

The output override also applies to diagnostics when an earlier flag is invalid.
For malformed or repeated output options, diagnostics use the first selection
before `--` if its value is `table` or `json`; otherwise they use the terminal
default. Malformed/repeated selections still fail with exit 2. Tokens after
`--` never select an output format.

Each success is written to stdout, with an empty stderr. Each failure writes a
diagnostic to stderr. Stable JSON schemas:

```json
{"command":"config show","ranges":[],"total":0}
```

```json
{"command":"config show","ranges":[{"start":50000,"end":50149}],"total":150}
```

```json
{"command":"config ranges add","changed":100,"total":100}
```

```json
{"command":"config ranges remove","changed":0,"total":100}
```

```json
{"error":{"code":"usage_error","message":"..."}}
```

`changed` and `total` always count ports. `remove --all` uses the same removal
schema. Empty range arrays are `[]`, never `null`. Error codes are `usage_error`,
`storage_error`, or `runtime_error`; message text provides details and may vary
with OS errors. Exit codes are 0 for success, 1 for runtime/storage failures,
and 2 for invalid commands, flags, or operands. If output fails after saving,
the mutation may already be committed even though the process returns 1.

## Configuration and locking

The versioned JSON config is `config.json` in Go's standard per-user OS config
directory, under `portbook`:

| OS | Default file |
| --- | --- |
| macOS | `~/Library/Application Support/portbook/config.json` |
| Linux | `$XDG_CONFIG_HOME/portbook/config.json`, or `~/.config/portbook/config.json` if unset |

Configuration directory selection, from highest to lowest priority:

1. Global `--config-dir /absolute/path` for this invocation.
2. `PORTBOOK_CONFIG_DIR` for this process environment.
3. A directory saved by `config path set /absolute/path`.
4. The OS-standard Portbook directory above.

All paths name a **directory containing `config.json`**, not a custom filename.
Paths must be nonempty, absolute, and valid UTF-8 so JSON persistence preserves
them exactly. Existing targets and their components must
be directories; the destination need not exist. Paths are cleaned but symlinks
in directory components are not resolved to a canonical physical path. A higher
priority selection bypasses lower priority choices, including invalid environment
values or a corrupt/unreadable saved pointer. The fixed OS-standard directory is
still required for mutation coordination. Its `location.json` and `state.lock`
paths (including children and case aliases) are reserved and cannot serve as
config directories. Existing directory-symlink aliases of that root are checked
as well. Such bootstrap conflicts report a storage error.

`config path get` reports the effective directory, config filename, and source
(`flag`, `environment`, `saved`, or `default`). It works before any file exists
and creates no directories, files, or locks. Set/reset report both the saved
choice and the still-effective location, so active temporary overrides are visible:

```sh
portbook config path set /absolute/path
portbook --config-dir /temporary/path config path reset
portbook config path get --output json
```

```json
{"command":"config path get","directory":"/absolute/path","config_file":"/absolute/path/config.json","source":"saved"}
```

Set/reset use the same effective fields plus `saved_directory`, a string for set
and `null` for reset. Repeating set or reset is successful. Reset removes only
the saved choice; environment and flag overrides remain active. These commands
select configuration; they never copy, move, merge, clear, or delete configuration
files. Selecting an uninitialized directory means an empty allowed pool. Returning
to an earlier directory restores access to its unchanged configuration.

The saved choice lives in `location.json` in the **fixed OS-standard Portbook
directory**, alongside `state.lock`, even when configuration is relocated:

```json
{"version":1,"directory":"/absolute/path"}
```

Ordinary resolution fails closed on malformed, duplicate-member, unknown-field,
unsupported-version, invalid Unicode (including unpaired surrogate escapes),
non-regular (including symlink), inaccessible, or invalid-path
pointer files. It does not fall back to another configuration. Explicit set/reset
can repair a corrupt or unreadable pointer: set atomically replaces that entry;
reset unlinks it. A symlink target is never followed by these repairs. Pointer
directories are rejected. Help validates selected paths and supplied operands;
set/reset help permits a broken pointer so repairs remain discoverable.

For an isolated configuration selection (coordination still uses the fixed root):

```sh
export PORTBOOK_CONFIG_DIR="$(mktemp -d)"
portbook --output table config ranges add 50000-50499
portbook --output json config show
```

The on-disk format is:

```json
{
  "version": 1,
  "ranges": [{"start": 50000, "end": 50499}]
}
```

A missing file means an empty pool. `config show` is read-only, including on a
fresh installation: it creates no directory, config, or lock. Corrupt JSON,
unknown fields, duplicate object members (including nested range fields,
escaped names, or case aliases), unsupported versions, invalid ranges, non-regular files
(including config symlinks), and inaccessible configuration fail closed. They
are never reset, quarantined, moved, or overwritten by a command. Resolve these
errors explicitly before retrying; the original config remains available.

Every mutation holds the same OS-backed `state.lock` in the fixed OS-standard
Portbook directory across read, modification, and save, using [gofrs/flock](https://github.com/gofrs/flock).
Lock acquisition waits at most five seconds. Future booking mutations must use
this same lock. Changing config directory never relocates or splits coordination
or future registry data. Range mutations resolve the saved selection inside the
lock, so a concurrent path change and range update have a defined serial order.
Path set/reset update the fixed pointer under that same lock, without nesting.
The lock file stays in place: do not delete or replace it while
processes may be running. The OS releases a held lock when its process exits.

Saves use a private temporary file in the same directory, sync it, then atomically
rename it over `config.json` (or `location.json` for a saved choice) and sync the
directory. Reset also syncs the directory after removing the pointer. New
directories use mode 0700 and config/pointer files use 0600. Readers see a complete
snapshot without taking
the mutation lock. A directory-sync failure after rename returns an error but
the replacement has already committed; inspect the config or saved choice before
retrying. A reset directory-sync failure likewise follows committed removal.
These guarantees assume a local filesystem supporting OS locks and atomic
rename, and cooperating writers using this lock. Do not manually edit files
while mutations are running.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
```

The integration suite builds the real executable and exercises independent
processes, including concurrent additions, removals, and location switches. Tests use isolated
temporary directories and never write to the normal user configuration.

The command tree uses [Cobra](https://cobra.dev/docs/how-to-guides/working-with-flags/)
for scoped commands/help, inherited output flags, local flags, and standard flag
parsing. A help validation hook preserves Portbook's stricter rule that help must
not hide invalid command paths, flags, or supplied operands. Cobra's automatic
error/usage printing is disabled so Portbook controls JSON, stderr, and exit codes.
No CLI scaffolding tool or configuration framework is required.

Code responsibilities:

| Location | Responsibility |
| --- | --- |
| `cmd/portbook` | Process exit and terminal detection |
| `internal/cli/commands.go` | Command definitions, flags, operand validation, scoped help |
| `internal/cli/execute.go` | Explicit config and path handlers |
| `internal/cli/render.go` | Stable success/error JSON and table output |
| `internal/ports` | Port parsing and set operations |
| `internal/config/codec.go` | Versioned JSON encoding and fail-closed decoding |
| `internal/config/store.go` | Read and atomic config/pointer persistence |
| `internal/config/location.go` | Config selection and fixed bootstrap pointer |
| `internal/state` | Fixed OS-standard coordination directory and OS lock |

`state.Coordinator.WithLock(ctx, func() error)` owns only the shared lock. Its
callback may fail; the lock is released on return or panic, and the OS releases
it on process exit. `config.Store.Mutate` uses it once across read/modify/save.
A config callback error discards the in-memory changes without saving. Other
state operations can use the same coordinator without opening or rewriting
configuration. Callbacks must never reacquire this lock or call another locking
operation. The coordinator provides serialization, not automatic rollback of
arbitrary callback writes or a transaction spanning multiple files.
