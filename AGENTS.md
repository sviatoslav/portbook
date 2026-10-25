# Portbook development

Use [DEVELOPING.md](docs/DEVELOPING.md) for package structure and checks,
[REFERENCE.md](docs/REFERENCE.md) for command contracts, and
[RELEASING.md](docs/RELEASING.md) for packaging and publication.

## Command behavior

- Keep scope to configuration, single-service get/reserve, exact-owner release,
  listing, explicit cleanup, and skill installation. Feature expansion requires
  an explicit request.
- Owners are canonical existing directories, defaulting to cwd. Each subdirectory
  is a separate owner. Preserve physical symlink/case aliases and case-sensitive
  service names. Get/reserve and single release require one explicit service;
  release `--all` selects exactly that owner's bookings.
- Get and list read one fixed registry snapshot independently of configuration,
  locks, writes, and probes. Get returns saved ports even when busy or outside
  the pool. List sorts by directory then service; `--all` includes missing owners.
- Reserve checks duplicates under the mutation lock, then chooses the lowest
  globally unbooked, OS-bindable TCP port. Duplicate attempts preserve registry
  bytes. Release deletes records independently of listener state and configuration;
  missing bookings fail while preserving the registry and other owners.
- Get/reserve print a decimal port and newline. Release and config mutations print
  plain confirmations. Local `--output` belongs to list, cleanup, config show,
  and config path get, after their complete command paths. Preserve terminal
  defaults, JSON schemas, empty arrays, escaped human identities, and exit codes.
- Define commands and flags in Cobra with explicit handlers and per-command
  policies. Help validates supplied paths, flags, and operands while allowing
  missing required operands. Keep Portbook's diagnostics authoritative.

## State and cleanup

- Acquire `state.Coordinator.WithLock` exactly once per mutation, covering reads,
  decisions, and saves. Use read-only dependencies inside callbacks; return errors
  before saving. Keep the lock and registry at the fixed OS-standard state root.
- Config selection is flag, environment, saved pointer, then default. Resolve
  the saved selection inside mutation locks. Path set/reset change only the fixed
  pointer, preserving configuration files. Reserve the bootstrap filenames
  `location.json`, `bookings.json`, and `state.lock`, their children, and aliases
  from use as config directories. Preserve paths through JSON exactly.
- Missing configuration uses 49152–65535 in memory. Existing pools, including
  empty ones, are authoritative. First mutations operate on the effective pool.
  Config reset persists defaults and counts membership changes; path reset clears
  the saved location. Pool changes preserve bookings.
- Preserve malformed, duplicate-member, unsupported-version, nonregular,
  inaccessible, and invalid-Unicode evidence. Explicit path set/reset may repair
  pointer evidence. Keep atomic replacement, file and directory sync, and bounded
  lock waits.
- Cleanup removes only confirmed missing owners with bindable ports. Retain
  inaccessible paths, changed symlinks, replacement files, missing ancestors,
  mount roots, and uncertain owners or ports. Compare ancestor identities and
  revalidate every proposed owner after all probes, before deleting entries.
- Dry-run cleanup is read-only. Actual cleanup uses one global lock independently
  of pool selection. Preserve registry bytes on no-op cleanup and retain
  preexisting empty owner maps.

## Verification and maintenance

- Isolate tests with temporary absolute state and owner directories. Preserve
  independent-process concurrency and real-socket coverage. Run targeted tests,
  then `go test ./...`, `go test -race ./...`, `go vet ./...`, and a CLI build.
- For release changes, run `goreleaser check`, snapshot packaging, and the archive
  verifier. Keep macOS/Linux amd64/arm64 targets, SHA256 checksums, deterministic
  version/commit injection, and the existing MIT license in every archive.
- Gate publication on source checks and native smoke for every target. Publish
  the checked archives, with write permission confined to the final tag-only job.
  Tags, pushes, releases, Homebrew work, and visibility/license changes require
  explicit user authorization. Report only verification actually executed.
- The canonical skill source is `sviatoslav/portbook-skills`. Keep install-skills
  scoped to that skill, with explicit agent targets, stdio passthrough, bounded
  process-group cancellation, and independence from Portbook state.
- Keep README focused on usage, the reference on current contracts, and contributor
  docs on structure and behavior. Release history belongs with published releases.
