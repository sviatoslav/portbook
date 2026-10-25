# Development

[README](../README.md) · [Command reference](REFERENCE.md) · [Releasing](RELEASING.md)

## Structure

| Package | Responsibility |
| --- | --- |
| `cmd/portbook` | Process exit and terminal detection. |
| `internal/cli` | Cobra commands, argument validation, help policies, and output. |
| `internal/booking` | Owner identity, registry snapshots, reservation, release, and cleanup. |
| `internal/config` | Pool persistence and configuration-location selection. |
| `internal/ports` | Port parsing, set operations, and normalized ranges. |
| `internal/state` | Fixed state directory, shared lock, atomic writes, and Unicode validation. |
| `internal/buildinfo` | Version and commit injected at build time. |
| `scripts` | Release packaging and artifact verification. |

Each command declares its help policy and structured-output support through
`options.command`. Handlers perform their own operations; rendering and error
classification remain in `internal/cli`.

`booking.Store` receives a fixed `Coordinator` and, for new reservations, a
`ReadPool` function. The CLI supplies the matching config store's `Read` method.
Get and scoped list share owner resolution; get indexes the service directly.

## Persistence

All mutations acquire `state.Coordinator.WithLock` once around reads, decisions,
and saves. Resolve saved config selection inside this lock. Callbacks return
errors before saving and use read-only dependencies within the held lock.

Atomic writes create and sync a private temporary file, rename it into place,
and sync its directory. New directories use `0700`; data files use `0600`.
Snapshot readers see complete files. A failure after rename or during output can
follow a committed change. Storage assumes cooperating writers on a filesystem
supporting OS locks and atomic rename.

The three JSON documents use version 1:

```json
{"version":1,"ranges":[{"start":49152,"end":65535}]}
```

```json
{"version":1,"directory":"/absolute/config-directory"}
```

```json
{"version":1,"bookings":{"/absolute/owner":{"api":49152}}}
```

These are `config.json`, `location.json`, and `bookings.json`, respectively.
A missing config supplies in-memory defaults; a missing pointer selects the
default location; a missing registry supplies an empty map. Existing documents
are authoritative. Decoders validate versions, fields, duplicate members, values,
and identities while preserving invalid evidence for investigation.

See [AGENTS.md](https://github.com/sviatoslav/portbook/blob/master/AGENTS.md)
for the behavioral invariants governing changes.

## Checks

Use Go matching `go.mod`. Run targeted regressions, then:

```sh
go test ./...
go test -race ./...
go vet ./...
go build -o bin/portbook ./cmd/portbook
```

Tests use temporary absolute directories for HOME, XDG, configuration, owners,
and installer destinations. Integration tests exercise the real executable,
independent-process concurrency, and TCP sockets. Preserve this coverage and
report the platforms actually tested. For packaging changes, run the
[release checks](RELEASING.md#verify-a-snapshot).
