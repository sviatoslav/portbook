# Portbook

Portbook coordinates local TCP port reservations across projects, worktrees, and
coding agents. Each directory and service ID keeps its assigned port; new
reservations skip ports already booked or in use.

From `/projects/my-app` (port numbers are illustrative):

```console
$ portbook reserve api
49152
$ portbook get api
49152
$ portbook reserve web
49153
$ portbook release web --output table
Released port 49153 for "web" in "/projects/my-app".
```

[Install Portbook](#installation).

## Installation

The public repository is available now under the [MIT License](#license).
[v0.1.0 is available](https://github.com/sviatoslav/portbook/releases/tag/v0.1.0).
Install with Homebrew, download a binary, or build from source. The command
reference below describes the next source version: published v0.1.0 still uses
create-or-return `reserve`, has no top-level `get`, and succeeds on missing
`release`. Build from source to use the lifecycle shown here.

### Homebrew

```sh
brew install sviatoslav/tap/portbook
portbook --version
```

The [tap](https://github.com/sviatoslav/homebrew-tap) selects the released binary
for macOS or Linux on x86-64 or ARM64. No Go toolchain is required.

### Download a release

Standalone executables require no Go installation. Release archives use these suffixes:

| System | CPU (`uname -m`) | Archive suffix |
| --- | --- | --- |
| macOS | Apple Silicon (`arm64`) | `darwin_arm64.tar.gz` |
| macOS | Intel (`x86_64`) | `darwin_amd64.tar.gz` |
| Linux | ARM64 (`aarch64`/`arm64`) | `linux_arm64.tar.gz` |
| Linux | x86-64 (`x86_64`) | `linux_amd64.tar.gz` |

These four targets are cross-built with CGO disabled. CI is configured to run
extracted native artifacts on each target; a successful cross-build alone does
not prove runtime support. Windows and other platforms are outside this release
scope. macOS binaries are unsigned and not notarized; downloaded files may be
subject to macOS security checks.

Open the
[GitHub Releases page](https://github.com/sviatoslav/portbook/releases), select a
specific version, and download its matching archive and `checksums.txt` into a
new temporary download directory. Release assets are public and can be
downloaded without signing in.

The following example uses `v0.1.0` on macOS arm64.
Replace the version and archive suffix using the table. Run these commands from
the directory containing the two downloads. Stop if checksum verification fails:

```sh
archive=portbook_0.1.0_darwin_arm64.tar.gz
# Select the exact downloaded archive's SHA256 entry.
awk -v name="$archive" '$2 == name { print }' checksums.txt > selected-checksum.txt
test "$(wc -l < selected-checksum.txt | tr -d ' ')" = 1 &&
  shasum -a 256 --check selected-checksum.txt &&
  tar -xzf "$archive" &&
  mkdir -p "$HOME/.local/bin" &&
  install -m 755 portbook "$HOME/.local/bin/portbook"
```

On Linux, `sha256sum --check selected-checksum.txt` may replace `shasum`.
Checksums detect corruption; they do not authenticate a compromised download
source. Obtain both files from the intended GitHub release.

Add `export PATH="$HOME/.local/bin:$PATH"` to your shell startup file if needed,
open a new shell, then run `portbook --version` and `portbook --help`.
No `sudo` is required. Release builds print the release version and Git commit;
checkout and plain `go install` builds print `portbook dev (commit unknown)`.
Version output is one plain-text line even with `--output json`; it reads no
configuration or booking data. Other command output contracts are unchanged.

### Build or install from source

Requires Go 1.26 or newer and macOS or Linux. From an accessible checkout:

```sh
go build -o bin/portbook ./cmd/portbook
./bin/portbook --version
./bin/portbook --output table --help
go install ./cmd/portbook
```

`go install` writes to `GOBIN`, or `$(go env GOPATH)/bin` when GOBIN is unset.
Add that directory to PATH. To install a specific published version from source:

```sh
# Choose a published version tag.
go install github.com/sviatoslav/portbook/cmd/portbook@v0.1.0
```

### Update or uninstall

For a Homebrew installation:

```sh
brew update
brew upgrade sviatoslav/tap/portbook
# To uninstall:
brew uninstall portbook
```

To update a downloaded binary, download a chosen newer version, verify its
checksum, and repeat the install command into the same PATH directory. For a
source install, repeat your source installation procedure at the desired version.
Use `command -v portbook` and `portbook --version` to check which binary runs.

To uninstall the downloaded binary:

```sh
rm "$HOME/.local/bin/portbook"
```

For a Go installation, remove the binary from your GOBIN/Go binary directory.
Uninstalling removes the installed program files. Config, the saved config selection,
and bookings remain in the locations documented below; no automatic data
cleanup or migration occurs.

Maintainers: see [release preparation and publication](docs/RELEASING.md) for
local snapshot checks, CI gates, and intentional tag steps.

## Usage

Ask for a port from your project directory:

```sh
portbook reserve server           # 49152
portbook reserve api              # 49153
portbook get api                  # 49153 again
portbook list                     # see this project's bookings
portbook release api              # release it after stopping your server
```

Works out of the box with 16,384 candidate ports (49152–65535). Busy or already
booked ports are skipped. Numbers above are illustrative; new reservations use
the lowest available allowed port. Customize the pool only if needed.

A booking belongs to your current directory and explicit service name. Create it
with `reserve`, read it with `get`, and remove it with
`release` after stopping the listener. Projects share one registry,
so they avoid each other's booked ports. Run from another directory for another
project, or use `--directory /absolute/project/path`.

`get` and `reserve` print just the port, ready for a script or coding agent:

```sh
PORT=$(portbook get api)
```

Portbook also provides conservative `cleanup` for abandoned bookings. It runs
without a daemon, proxy, database, heartbeat, or expiry. Batch reservations and
agent skills are future work.

## Commands

```sh
portbook get SERVICE [--directory ABSOLUTE_DIRECTORY]
portbook reserve SERVICE [--directory ABSOLUTE_DIRECTORY]
portbook release SERVICE [--directory ABSOLUTE_DIRECTORY]
portbook list [--directory ABSOLUTE_DIRECTORY | --all]
portbook cleanup [--dry-run]
portbook config path get
portbook config path set /absolute/path
portbook config path reset
portbook --config-dir /temporary/path config show
portbook config show
portbook config add 50000-50499
portbook config add 51000
portbook config remove 50000-50499
portbook config remove 51000
portbook config remove --all
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
number. Configuration changes do not touch other state files; booking
data stays separate, including bookings outside the configured pool.

An illustrative sequence on a fresh installation, starting by explicitly clearing
the effective default pool,
using table output so it is reproducible when captured:

```text
$ portbook --output table config remove --all
Removed 16384 ports. Total: 0 ports.
$ portbook --output table config add 50000-50099
Added 100 ports. Total: 100 ports.
$ portbook --output table config add 50050-50149
Added 50 ports. Total: 150 ports.
$ portbook --output table config show
RANGE        PORTS
50000-50149  150
Total: 150 ports.
$ portbook --output table config remove 50040-50059
Removed 20 ports. Total: 130 ports.
$ portbook --output table config show
RANGE        PORTS
50000-50039  40
50060-50149  90
Total: 130 ports.
$ portbook --output table config remove 50149
Removed 1 port. Total: 129 ports.
$ portbook --output table config add 50000
Added 0 ports. Total: 129 ports.
$ portbook --output table config remove --all
Removed 129 ports. Total: 0 ports.
$ portbook --output table config show
No port ranges configured.
Total: 0 ports.
```

## Getting a reserved port

`portbook get SERVICE [--directory ABSOLUTE_DIRECTORY]` reads an existing
reservation and prints its decimal port plus newline, including with
`--output json`. It uses the same owner/service validation and aliases as `reserve`.
Missing reservations fail with `reservation not found`; it never allocates.

Like `list`, `get` reads one atomically replaced registry snapshot without locks,
TCP probes, config/pointer reads, or writes. It creates no state on a fresh
installation. A concurrent mutation can make the returned snapshot stale.
Explicit `--config-dir` still receives path validation; unused configuration
environment and saved selections do not affect lookup or its help. Corrupt or
inaccessible registry data fails closed.

Missing reservations (`get`/`release`) and duplicate reservations (`reserve`)
write diagnostics only to stderr, leave stdout empty, and exit 1 with the existing
`runtime_error` code. Their messages distinguish `reservation not found` from
`reservation already exists`, and include the service and owner directory.

## Reserving a port

```text
$ portbook reserve server --directory /projects/my-app
50000
$ portbook get server --directory /projects/my-app
50000
$ portbook reserve default --directory /projects/my-app
50001
```

These ports are illustrative: a new booking uses the lowest available configured
port. `get`, `reserve`, and `release` each require exactly one explicit service.
Omission is a usage error; help may omit the service. The literal name `default`
remains valid for historical bookings; existing entries are never renamed or migrated.
An existing reservation makes `reserve` fail with `reservation already exists`,
even when its port is occupied or outside the current pool.
Service identities are nonempty, case-sensitive Unicode strings without NUL;
use `--` before a service beginning with a hyphen.

The owner defaults to the absolute current working directory, including nested
directories. Portbook never searches for a Git/worktree root or changes cwd.
`--directory` must be a nonempty absolute path to an existing, readable and
searchable directory. Paths are normalized and existing symlink aliases resolve
to the same physical directory. Existing bookings also match filesystem case
aliases without folding distinct directory or service names. Missing, inaccessible,
or file targets fail;
Portbook never creates the owner directory or silently falls back to cwd.

`get` returns the saved port of an existing directory/service booking, even if an OS
listener occupies it, the configured pool is empty, or that port has been removed
from the pool. Reading a booking does not claim a listener belongs to Portbook or its caller.
New reservations skip every port booked by any directory/service, including idle
bookings, and every candidate that cannot be bound. Probes briefly bind wildcard
IPv4 and IPv6 TCP sockets (where the family is supported), then close them.
Forbidden candidates are skipped; unexpected probe failures stop the command
conservatively. Exhaustion fails without changing bookings.

Sockets are not held after probing. An unmanaged process can bind a port between
the probe and your application's listen call; reservations coordinate cooperating
Portbook callers and cannot prevent this OS race. Portbook never automatically
switches, expires, or reclaims a booking. Use explicit release for idle bookings.

Bookings live in `bookings.json` beside `state.lock` in the fixed OS-standard
Portbook directory: `~/Library/Application Support/portbook/bookings.json` on
macOS, `$XDG_CONFIG_HOME/portbook/bookings.json` on Linux, or
`~/.config/portbook/bookings.json` when XDG_CONFIG_HOME is unset. Overrides and
saved config locations never move this global registry. The format is:

```json
{"version":1,"bookings":{"/projects/my-app":{"server":50000,"default":50001},"/projects/another-app":{"server":50002}}}
```

Missing registry means empty. Every port is globally unique; every directory key
must be a normalized absolute path. Decoding preserves valid Unicode exactly and
rejects malformed JSON, duplicate keys, unknown fields, unsupported versions,
invalid identities/ports, duplicate port assignments, invalid Unicode, and
nonregular (including symlink) or inaccessible registry files. Errors preserve
the original evidence on repeated commands. Recorded directories may subsequently
disappear; decoding retains their bookings. Reservation still requires a valid
current owner directory. Do not reset the registry to resolve corruption.

Reservation holds the existing global lock once across registry read,
duplicate detection, selected configuration resolution/read, probing, and atomic
registry save. Concurrent reserves for one identity yield exactly one success;
the rest fail with `reservation already exists`. It never writes the config or
saved pointer. Config updates and path switching use the same lock. Success is emitted only after saving. Atomic replacement uses the
same temporary-file, file-sync, rename, and directory-sync protocol as config.
As with config, a directory-sync error after rename reports failure after a
committed replacement. Inspect the registry before retrying.

## Releasing a booking

```sh
portbook release server --directory /projects/my-app
portbook release server              # owner is cwd
portbook --output table release server --directory /projects/my-app
```

`release` requires exactly one service and uses the same directory validation,
canonical symlink/case aliases, service identity, and `--` rules as `reserve`.
It removes only that booking, and removes its owner map when no services remain.
A missing booking fails with `reservation not found` without creating or rewriting
`bookings.json`. Release may create the fixed coordination directory and lock.

Release is conservative: it refuses with `runtime_error` if the booked port is
occupied, forbidden, or cannot be proven available by the shared IPv4/IPv6 TCP
probe. Stop your listener before releasing. It never kills processes or infers
listener ownership. The check is an instant observation; unmanaged processes can
bind after the probe. Every other booking is preserved, including absent owners.

The fixed global lock covers registry read, owner matching, probing, and atomic
save. Release does not read the selected pool or saved config pointer, so an
out-of-pool booking remains releasable even with broken configuration. Explicit
`--config-dir` flags still undergo normal path validation, including for help.
Unused configuration environment values do not block release or its help. Corrupt registry data
fails closed without modification, including on repeated release attempts.

JSON success always has `command`, `directory`, `service`, `port`, and
`released`. A release reports the removed port and `released: true`.
Table output is a short `Released port ...` message. Success is printed only after the save completes.

```json
{"command":"release","directory":"/projects/my-app","service":"server","port":50000,"released":true}
```

## Listing bookings

```sh
portbook list
portbook list --directory /projects/my-app
portbook list --all --output json
```

`list` shows the current directory's bookings; `--directory` selects another
existing accessible absolute directory using the same symlink/case identity rules
as `reserve`. The default is cwd, including nested directories, without Git-root
discovery. `--all` includes every stored owner path, even disappeared directories.
`--all` and `--directory` cannot be combined, including `--all=false`.
No positional operands are accepted.

Listing displays saved bookings unchanged, including ports outside the configured
pool and ports with active listeners. It does not probe sockets, infer process
ownership, or read configuration or the saved config pointer. Config overrides
never relocate the registry. Explicit global flags still undergo validation.
Listing reads one complete atomic snapshot without acquiring the mutation lock
and creates no directories or files, including on a fresh installation. A missing
registry yields an empty list; malformed or inaccessible registries fail closed
and remain untouched.

Rows sort lexically by stored directory then service; scoped rows sort by service.
Human output has `SERVICE`/`PORT` columns, with a leading `DIRECTORY` column for
`--all`; an empty list prints `No bookings found.`. Names containing nonprinting
characters, quotes, backslashes, or leading/trailing spaces are quoted and escaped in tables to keep
bookings distinct and each booking on one row.
JSON always includes the stored directory in each row, including scoped output:

```json
{"command":"list","bookings":[{"directory":"/projects/my-app","service":"server","port":50000}]}
```

```json
{"command":"list","bookings":[]}
```

## Cleaning abandoned bookings

```sh
portbook cleanup --dry-run
portbook --output table cleanup
```

Cleanup scans only owner paths already recorded in the global registry. It removes
an individual service booking only when its owner is confirmed missing and the
saved TCP port can be bound using the same IPv4/IPv6 probe as reservation. An
existing directory retains all bookings even when no service is listening. Busy,
forbidden, unsupported, or otherwise uncertain ports retain their bookings.
Cleanup does not consult the pool or saved configuration pointer; corrupt pool or
pointer data does not prevent cleanup. An explicitly supplied `--config-dir` still
must pass normal flag/path validation, but never relocates the registry.

Deletion evidence requires a missing final path component, an accessible immediate
parent, no symlink components, and a recognized local filesystem. Missing ancestors,
permission failures, replacement files, new or dangling symlinks, network or unknown
filesystems, and paths under `/Volumes`, `/mnt`, `/media`, or `/run/media` are retained.
If a whole parent tree was removed, cleanup conservatively retains its bookings.
Local filesystem recognition covers APFS/HFS on macOS and ext, XFS, Btrfs, tmpfs,
overlay, and ramfs on Linux. Other filesystems are reported as uncertain.

This is path-based evidence, not a volume-history detector: a registry stores no
mount identity. An arbitrary custom mount that disappears can expose an accessible
local parent indistinguishable from an ordinary deletion. Run cleanup only when such
volumes are available; standard removable mount roots
and missing ancestors are always skipped. Filesystem changes and unmanaged binds
can also race the checks. Cleanup records ancestor filesystem identities, rechecks after probing, and
revalidates proposed owners after all probes before saving. A replaced parent
retains its bookings as uncertain. These checks cannot
provide a universal deletion or future port-availability guarantee.

`--dry-run` is read-only, including before installation: it creates no registry,
directory, or lock and preserves registry bytes. It previews a snapshot; a later
cleanup checks again. Actual cleanup acquires the shared global lock once around
registry read, owner checks, probes, and atomic save. It never rewrites the registry
when no booking is removed. Retained entries stay intact; an owner map is removed
only after its final booking is removed. Corrupt registries fail closed and retain
all original evidence. There is no cleanup during reserve, expiry, process killing,
owner-directory deletion, force flag, or scheduling.

Rows are sorted by directory then service. JSON success always has `command`,
`dry_run`, `rows` (including `[]` when empty), `removed`, `would_remove`, and
`retained`. Counts refer to service bookings. Each row has `directory`, `service`,
`port`, `action` (`removed`, `would-remove`, or `retained`), and `reason`:
`owner_missing`, `owner_exists`, `owner_not_directory`, `owner_uncertain`,
`port_busy_or_unavailable`, or `port_unknown`. Table identities are quoted and
escape control characters, line separators, and bidirectional formatting; JSON
preserves the original values. Per-owner/port uncertainty is a
successful conservative skip; registry, lock, save, and output failures use the
normal error schema and exit codes. For example:

```json
{"command":"cleanup","dry_run":true,"rows":[{"directory":"/projects/deleted-app","service":"server","port":50000,"action":"would-remove","reason":"owner_missing"}],"removed":0,"would_remove":1,"retained":0}
```

## Output and exit codes

`get` and `reserve` success always write only a decimal port plus newline, on
terminals and pipes and even with `--output json` or `--output table`. Their help and errors
follow the normal format rules below.

For configuration, list, release, and cleanup commands, terminal stdout is human-readable text/tables.
When stdout is piped, redirected, or otherwise noninteractive, it is JSON. `--output table` or
`--output json` overrides this anywhere in the command; `--output=json` also
works. `--help` / `-h` describes commands and examples and follows this convention
(JSON help has `command` and `help` fields). Help is scoped to the resolved
command: `portbook config remove --help` describes removal and `--all`.
Unknown command paths, unknown/inapplicable flags, invalid supplied operands,
`config remove --all` mixed with operands, and `list --all --directory` still
fail with exit 2 when help is requested.
Recognized commands can show help without required operands. Global `--output` and `--config-dir`
can appear before or after the command path; `--all` belongs to `config remove`
and `list`; `--directory` belongs to `get`, `reserve`, `list`, and `release`; and
`--dry-run` belongs to `cleanup`.
Repeated `--output`, `--config-dir`, `--directory`, `--dry-run`, or `--all` flags are rejected. `--` ends flag parsing, so
`config add -- 51000` works and `--help` after `--` is an operand.
There are no prompts.

The output override also applies to diagnostics when an earlier flag is invalid.
For malformed or repeated output options, diagnostics use the first selection
before `--` if its value is `table` or `json`; otherwise they use the terminal
default. Malformed/repeated selections still fail with exit 2. Tokens consumed as values for `--config-dir` or `--directory`, and tokens after
`--`, never select an output format.

Each success is written to stdout, with an empty stderr. Each failure writes a
diagnostic to stderr. Stable JSON schemas:

```json
{"command":"config show","ranges":[],"total":0}
```

```json
{"command":"config show","ranges":[{"start":50000,"end":50149}],"total":150}
```

```json
{"command":"config add","changed":100,"total":100}
```

```json
{"command":"config remove","changed":0,"total":100}
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

## Advanced configuration and locking

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
still required for mutation coordination. Its `location.json`, `bookings.json`, and `state.lock`
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
files. Selecting an uninitialized directory uses the same default pool. Returning
to an earlier directory restores access to its unchanged configuration.

The saved choice lives in `location.json` in the **fixed OS-standard Portbook
directory**, alongside `state.lock` and `bookings.json`, even when configuration is relocated:

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
portbook --output table config remove --all
portbook --output table config add 50000-50499
portbook --output json config show
```

The on-disk format is:

```json
{
  "version": 1,
  "ranges": [{"start": 50000, "end": 50499}]
}
```

A missing config file uses 49152–65535 (16,384 candidate TCP ports) in memory.
This is the full [IANA dynamic/private range](https://www.iana.org/assignments/service-names-port-numbers/service-names-port-numbers.xhtml).
It can overlap OS ephemeral use; Portbook checks availability when allocating,
but cannot guarantee a particular port or prevent a later unmanaged bind.
An existing valid config is authoritative, including `"ranges": []`: no defaults
are added to saved ranges. `config remove --all` explicitly persists an
empty pool and disables new allocations while preserving existing bookings for `get` and `release`.
The first config mutation unions/subtracts from the effective defaults and saves
the result. To replace defaults with a custom pool, clear first and then add.

`config show` is read-only, including on a
fresh installation: it creates no directory, config, or lock. Corrupt JSON,
unknown fields, duplicate object members (including nested range fields,
escaped names, or case aliases), unsupported versions, invalid ranges, non-regular files
(including config symlinks), and inaccessible configuration fail closed. They
are never reset, quarantined, moved, or overwritten by a command. Resolve these
errors explicitly before retrying; the original config remains available.

Every mutation holds the same OS-backed `state.lock` in the fixed OS-standard
Portbook directory across read, modification, and save, using [gofrs/flock](https://github.com/gofrs/flock).
Lock acquisition waits at most five seconds. Reservations use
this same lock. Changing config directory never relocates or splits coordination
or registry data. Range mutations resolve the saved selection inside the
lock, so a concurrent path change and range update have a defined serial order.
Path set/reset update the fixed pointer under that same lock, without nesting.
The lock file stays in place: do not delete or replace it while
processes may be running. The OS releases a held lock when its process exits.

Saves use a private temporary file in the same directory, sync it, then atomically
rename it over `config.json`, `location.json`, or `bookings.json` and sync the
directory. Reset also syncs the directory after removing the pointer. New
directories use mode 0700 and config/pointer/booking files use 0600. Configuration
readers see a complete snapshot without taking the mutation lock. A directory-sync failure after rename returns an error but
the replacement has already committed; inspect the config, saved choice, or
registry before retrying. A reset directory-sync failure likewise follows committed removal.
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
processes, including concurrent additions, removals, location switches,
reservations, releases, cleanup, and list snapshots. Tests use isolated temporary directories
and never write to the normal user configuration.

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
| `internal/cli/get.go` | Read-only bare-port lookup command |
| `internal/booking/get.go` | Existing reservation lookup from a registry snapshot |
| `internal/cli/list.go` | Read-only list command, scoped validation, and rendering |
| `internal/booking/list.go` | Sorted registry snapshots and scoped owner matching |
| `internal/cli/cleanup.go` | Cleanup command, preview flag, and deterministic reports |
| `internal/booking/cleanup.go` | Conservative owner evidence, probes, and locked cleanup |
| `internal/cli/render.go` | Stable success/error JSON and table output |
| `internal/ports` | Port parsing and set operations |
| `internal/config/codec.go` | Versioned JSON encoding and fail-closed decoding |
| `internal/config/store.go` | Read and atomic config/pointer persistence |
| `internal/config/location.go` | Config selection and fixed bootstrap pointer |
| `internal/booking` | Canonical owner identity, strict nested registry, TCP probes, reservation |
| `internal/state` | Fixed OS-standard coordination directory, OS lock, atomic writes, Unicode validation |

`state.Coordinator.WithLock(ctx, func() error)` owns only the shared lock. Its
callback may fail; the lock is released on return or panic, and the OS releases
it on process exit. `config.Store.Mutate` uses it once across read/modify/save.
A config callback error discards the in-memory changes without saving. Other
state operations can use the same coordinator without opening or rewriting
configuration. Callbacks must never reacquire this lock or call another locking
operation. The coordinator provides serialization, not automatic rollback of
arbitrary callback writes or a transaction spanning multiple files.

## License

Portbook is available under the [MIT License](LICENSE).
