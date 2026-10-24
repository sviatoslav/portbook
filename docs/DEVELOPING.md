# Developing Portbook

[Back to README](../README.md) · [Command and storage reference](REFERENCE.md) · [Releasing](RELEASING.md)

Use Go matching `go.mod` (1.26 or newer). Run targeted regressions after fixes,
then the full checks below. See [AGENTS.md](https://github.com/sviatoslav/portbook/blob/master/AGENTS.md) for development constraints.
This guide describes current development sources; see the README for release availability.

```sh
go test ./...
go test -race ./...
go vet ./...
go build -o bin/portbook ./cmd/portbook
```

The integration suite builds the real executable and exercises independent
processes, including concurrent additions, removals, location switches,
reservations, releases, cleanup, and list snapshots. Tests use isolated temporary directories
and never write to the normal user configuration.

The command tree uses [Cobra](https://cobra.dev/docs/how-to-guides/working-with-flags/)
for scoped commands/help, local output flags, and standard flag parsing. Each
command declares its help validation policy and whether it supports structured
output beside its definition. The shared help hook preserves Portbook's stricter
rule that help must not hide invalid command paths, flags, or supplied operands. Cobra's automatic
error/usage printing is disabled so Portbook controls JSON, stderr, and exit codes.
No CLI scaffolding tool or configuration framework is required.

Code responsibilities:

| Location | Responsibility |
| --- | --- |
| `cmd/portbook` | Process exit and terminal detection |
| `internal/cli/commands.go` | Command definitions, flags, operand validation, scoped help |
| `internal/cli/command_policy.go` | Per-command help policies and local structured-output registration |
| `internal/cli/execute.go` | Explicit config and path handlers |
| `internal/cli/install_skills.go` | External installer, stdio, TTY ownership, bounded process cancellation |
| `internal/cli/get.go` | Read-only bare-port lookup command |
| `internal/booking/get.go` | Existing reservation lookup from a registry snapshot |
| `internal/cli/list.go` | Read-only list command, scoped validation, and rendering |
| `internal/booking/list.go` | Sorted registry snapshots and scoped owner matching |
| `internal/cli/cleanup.go` | Cleanup command, preview flag, and deterministic reports |
| `internal/booking/cleanup.go` | Conservative owner evidence, probes, and locked cleanup |
| `internal/cli/render.go` | Stable success/error JSON and table output |
| `internal/cli/identity_output.go` | Shared escaping of names and paths in human output |
| `internal/ports` | Port parsing and set operations |
| `internal/config/codec.go` | Versioned JSON encoding and fail-closed decoding |
| `internal/config/store.go` | Read and atomic config/pointer persistence |
| `internal/config/location.go` | Config selection and fixed bootstrap pointer |
| `internal/booking` | Canonical owner identity, strict nested registry, TCP probes, reservation |
| `internal/state` | Fixed OS-standard coordination directory, OS lock, atomic writes, Unicode validation |

`booking.Store` receives its fixed `Coordinator` directly. Only creating a new
reservation requires a `ReadPool` function, called after duplicate detection
inside that coordinator's lock. The CLI supplies the matching config store's
read-only `Read` method so saved location selection happens under the same lock.
Get, list, release, and cleanup need no configuration dependency. Get and scoped
list share owner validation and snapshot resolution; get indexes the service
directly without constructing or sorting list rows.

`state.Coordinator.WithLock(ctx, func() error)` owns only the shared lock. Its
callback may fail; the lock is released on return or panic, and the OS releases
it on process exit. `config.Store.Mutate` uses it once across read/modify/save.
A config callback error discards the in-memory changes without saving. Other
state operations can use the same coordinator without opening or rewriting
configuration. Callbacks must never reacquire this lock or call another locking
operation. The coordinator provides serialization, not automatic rollback of
arbitrary callback writes or a transaction spanning multiple files.
