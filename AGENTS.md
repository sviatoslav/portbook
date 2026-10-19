# Portbook development

- Current scope is configuration, single-service `reserve`, and explicit `cleanup`.
  Add listing, release, batch reservations, or other features only when explicitly
  requested.
- Reserve prints only the decimal port and newline, regardless of output format.
  Owners are canonical existing directories, defaulting to cwd without Git-root
  discovery. Keep stable reuse even outside the pool or with a listener present.
  New bookings require the lowest globally unbooked and OS-bindable TCP port.
- Preserve the command paths, port-set semantics, table/JSON output, error schema,
  and exit codes documented in README.md. Define commands and flags in Cobra;
  use explicit handlers rather than dispatching mutations by command strings.
- Help is scoped. Resolve paths and validate applicable flags and supplied
  operands before rendering help; missing required operands may be omitted for
  help. Keep Cobra's automatic diagnostics disabled to preserve output contracts.
- All state mutations must use `internal/state.Coordinator.WithLock` and the same
  `state.lock`. Acquire once around reads, decisions, and saves. Do not nest this
  lock or call config mutations from another lock callback. Return callback errors
  before saving. The coordinator does not roll back arbitrary callback writes.
- Keep coordination anchored to the OS-standard Portbook directory. Config
  selection (`--config-dir`, environment, saved `location.json`, default) must
  never move `state.lock` or registry data. Resolve the saved selection
  inside config and reservation mutation locks. Path set/reset use that same
  lock and fixed pointer;
  they select locations without migrating or deleting configuration. Directory
  paths must round-trip through JSON; bootstrap filenames and their children
  are reserved configuration targets. Ordinary
  pointer reads fail closed; only explicit set/reset may repair pointer evidence.
- Keep config separate from registry data. Removing allowed ports must
  never delete or rewrite bookings. No heartbeat, expiry, daemon, proxy, or DB.
- Configuration and booking reads fail closed. Preserve malformed, duplicate-member,
  unsupported-version, and inaccessible files; do not reset or quarantine them.
  Preserve atomic replacement, temp-file sync, directory sync, and bounded waits.
- Tests must isolate all state with temporary absolute directories. Keep real
  independent-process concurrency coverage. Run targeted regressions after fixes,
  then `go test ./...`, `go test -race ./...`, `go vet ./...`, and a CLI build.
  Do not claim Linux runtime or CI results unless actually executed.

- Cleanup checks only recorded owners and removes only confirmed missing owners
  with bindable TCP ports. Retain existing, inaccessible, changed-symlink,
  replacement-file, missing-ancestor, mount-root, and other uncertain owners;
  retain busy/unknown ports. Never infer deletion merely from a failed Stat.
  Dry runs create no directories/files/locks. Actual cleanup reads/checks/saves
  under one global lock, independent of pool/pointer selection; no-op cleanup
  must not rewrite registry bytes. Preserve preexisting empty owner maps.
- Cleanup must retain proposals when ancestor filesystem identities change and
  revalidate all proposed owners after probing, before deleting snapshot entries.
