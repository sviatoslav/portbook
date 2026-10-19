# Portbook development

- Current scope is configuration, single-service `get`/`reserve`, owner-scoped `release`,
  read-only `list`, and explicit `cleanup`. Add batch reservations or other
  features only when explicitly requested.
- Release deletes registry records without TCP probes or listener checks. A single
  service or `--all` removes only the exact canonical owner's bookings, preserving
  other owners and descendants. Missing bookings fail without creating or rewriting
  the registry. Use the fixed coordinator without reading config or saved pointer.
- List reads only the fixed registry snapshot, without config/pointer reads, locks,
  writes, or probes. Scoped listing uses reserve owner identity; `--all` retains
  disappeared owner paths. Sort by directory then service; keep empty JSON arrays.
- Reserve prints only the decimal port and newline, regardless of output format.
  Owners are canonical existing directories, defaulting to cwd without Git-root
  discovery. Get reads existing bookings even outside the pool or with a listener present,
  using only a registry snapshot without locks, config/pointer reads, writes, or
  probes. Duplicate reserve fails under the mutation lock without rewriting data.
  Get/reserve and single release require one explicit service; release --all
  accepts no service. No implicit default. The literal service `default` stays
  valid for existing bookings, without migration.
  New bookings require the lowest globally unbooked and OS-bindable TCP port.
- Preserve the command paths, port-set semantics, table/JSON output, error schema,
  and exit codes documented in docs/REFERENCE.md. Define commands and flags in Cobra;
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

- Release preparation uses `.goreleaser.yaml` and `scripts/release-snapshot.sh`.
  Keep macOS/Linux amd64+arm64 packaging, SHA256 checksums, and deterministic
  version/commit injection. Version output stays plain text and reads no state.
  Preserve the canonical `github.com/sviatoslav/portbook` module/import path.
- Test/package CI snapshots on PR/master without publication rights. Tag releases
  must gate publication on source checks and native extracted-artifact smoke for
  every target, publishing the exact checked archives without rebuilding them.
  Keep write permission confined to the final tag-only publication job.
- Run `goreleaser check`, snapshot packaging, and `scripts/verify-release.py`
  after release changes. Smoke data must use temporary HOME/XDG/config/owner
  paths. Distinguish cross-builds, local native execution, and actual CI results.
  Keep build/dist output ignored. Review installation examples against assets;
  do not claim unpublished versions exist. Include the existing MIT LICENSE in
  every archive and preserve its standard text and copyright notice.
- Follow `docs/RELEASING.md`; tags, pushes, releases, visibility/license changes,
  and Homebrew work need explicit user authorization. Preparation alone does not
  authorize publication. Perform a fresh local review after verification and
  fix actionable findings before handing off release preparation.

- Missing config files use the in-memory default pool 49152-65535 (16,384 ports),
  with no read-time writes. Existing configs, including explicit empty ranges,
  remain authoritative. First mutations modify effective defaults before saving;
  selecting a missing custom config uses defaults too. Corruption/access errors
  must never trigger a fallback. Keep advanced pool setup out of the quick start.

- Pool commands are `config add`, `config remove`, and `config remove --all`,
  without an intermediate subcommand. Preserve set/range operands and scoped
  help/flag validation; keep structured command labels aligned with these paths.

- Documentation navigation: README.md is the short user entry point; detailed
  contracts live in docs/REFERENCE.md, installation in docs/INSTALLING.md, and
  contributor guidance in docs/DEVELOPING.md. Keep release availability truthful.
- Agent setup is documented in docs/AGENT-SETUP.md. Canonical skill source is
  sviatoslav/portbook-skills, not this repository. install-skills delegates only
  that skill to Vercel Skills CLI; preserve explicit agent targets, bounded
  process-group cancellation, stdio passthrough, and state independence.
