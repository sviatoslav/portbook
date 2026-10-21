# Preparing and publishing a release

[Back to README](../README.md) · [Development](DEVELOPING.md)

This guide covers local snapshot checks and intentional release publication.
The public project uses the MIT license in `LICENSE`. Creating/pushing version
tags and publishing releases require explicit user authorization.

## v0.3.0 release notes and migration

The target tag is `v0.3.0`; check [GitHub Releases](https://github.com/sviatoslav/portbook/releases)
for published assets. Version and commit are injected from the tag, as before.

- Add `config reset` to restore the selected pool to 49152–65535 (16,384 ports)
  in one global-lock transaction and atomic save. It uses the shared defaults,
  preserves bookings and location selection, and fails closed on corrupt or
  unreadable configuration. Like other pool mutations it persists explicit
  defaults even when unchanged; `changed` counts added and removed memberships.
- Scope `--output table|json` to `list`, `cleanup`, and the `config` subtree.
  Put it after a supported command. Get, reserve, release, install-skills,
  root help, and version reject the flag without changing state.
- Always print plain single/bulk release confirmations, on terminals and pipes.
  Release still removes only the selected booking records, without probes or
  stopping listeners. Missing bookings and error/exit-code contracts remain unchanged.

For scripts upgrading from v0.2.0, replace `portbook --output json list` with
`portbook list --output json`, and apply the same placement to config/cleanup.
Remove output flags from plain commands. Check release exit status and stderr;
do not decode its success output as JSON. Use `config reset` for pool contents;
`config path reset` still changes only the saved location choice. Existing config
and booking files remain compatible and require no migration.

The matching agent skill targets v0.3.0+, checks scoped help without unsupported
output flags, and treats release confirmation as plain text. Updating the public
skill or tap is separate from tagging the binary; follow the verification steps below.

## v0.2.0 release notes and migration

The target tag is `v0.2.0`; check [GitHub Releases](https://github.com/sviatoslav/portbook/releases)
for published assets. GoReleaser injects the version and commit from the tag;
checkout builds continue to report `dev`, and snapshots derive their label from
Git history. No independent runtime version constant needs updating.

Release notes:

- Add read-only `get SERVICE`, which prints an existing reserved port.
- Make `reserve SERVICE` create-only: existing bookings report an error. Missing
  single `release` targets now fail; the new `get` also fails when no booking exists.
- Require explicit service names. Historical bookings named `default` remain
  accessible by passing that literal name; no registry migration is performed.
- Make `release SERVICE` delete booking records even while their ports are busy.
  Add `release --all` for only the exact selected directory, preserving children
  and other owners. Release never probes ports or stops listeners; allocation
  still probes and skips externally occupied ports.
- Add `install-skills [--agent codex|claude-code]` for the already published
  Portbook skill. It requires Node.js/npm/npx and network access, preserves upstream
  confirmation/stdio, and bounds installer process lifetime. Reinstallation can
  replace user edits; no Portbook registry/config state is read or changed.
- Provide a shorter README and package linked installation, reference, development,
  release, and agent-setup guides with every macOS/Linux amd64/arm64 archive.

For scripts upgrading from v0.1.0, reserve a named service once, then use
`portbook get NAME` on subsequent starts. Replace unnamed calls with the intended
name (or explicit `default` for historical bookings). Do not use unconditional
`get || reserve` on arbitrary failures. Treat release as record deletion, not a
listener shutdown or proof of OS availability. Existing pool and registry files
remain compatible; the missing-config pool stays 49152–65535.

Before using the agent workflow, check both `--version` and scoped help against
[the setup guide](AGENT-SETUP.md). The v0.2.0 skill targeted Portbook 0.2.0+; the current skill targets v0.3.0+. An authorized
source build with the required features can also be used.

## Local snapshot verification

Use Go matching `go.mod` (currently 1.26 or newer), Python 3.9 or newer, Git,
and [GoReleaser OSS v2.18.3](https://github.com/goreleaser/goreleaser/releases/tag/v2.18.3).
Install GoReleaser following its official release instructions, verifying its
published SHA256 checksum. CI pins the same version. From the repository root:

```sh
go test ./...
go test -race ./...
go vet ./...
go build -o bin/portbook ./cmd/portbook
scripts/release-snapshot.sh
```

The script checks the release tag policy, runs `goreleaser check`, then
`goreleaser release --snapshot --clean`. It replaces only the ignored `dist/`
output directory. It creates no Git tag and never publishes. Snapshot versions
contain `snapshot` and the source commit; an untagged repository starts from
GoReleaser's fallback version. A dirty working tree is allowed for snapshots:
the displayed commit identifies the base, not uncommitted edits. Use a clean
checkout when validating a candidate for publication.

`scripts/verify-release.py dist --commit "$(git rev-parse HEAD)"` checks all four
archives, their exact documentation files and relative links/anchors, executable
modes, SHA256 manifest,
ELF/Mach-O architecture, Go module/build metadata and commit. It extracts and
runs only the host's native binary. The smoke test covers version, table/JSON
help, error output, installer dependency/argv/passthrough with a fake npx,
fresh read-only list/cleanup, missing-config defaults of
49152–65535 (16,384 ports), config changes, bare-port reserve
and read-only get, required-service validation, explicit literal `default`
compatibility, duplicate-reserve refusal, listing, single/bulk release with listener survival,
busy-port allocation refusal after release, missing-release errors, and abandoned-owner cleanup.

Each archive includes README.md, LICENSE, and all five guides in docs/
(INSTALLING.md, REFERENCE.md, DEVELOPING.md, RELEASING.md, and AGENT-SETUP.md). Keep the archive
file list and verifier aligned when moving documentation.

All runtime configuration and bookings use temporary HOME/XDG/config/owner
paths, removed afterwards. It does not access real Portbook data.

Cross-building verifies compilation and packaging, not runtime compatibility.
The workflow runs source tests/race/vet on macOS arm64 and Linux amd64, packages
once on Linux amd64, then downloads those packages and runs the same native smoke
on `macos-15` (arm64), `macos-15-intel` (amd64), `ubuntu-24.04` (amd64), and
`ubuntu-24.04-arm` (arm64). Runner labels follow the
[GitHub runner reference](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).
CI results are evidence only after the workflow actually runs.

## Intentional tag release

Do this only after explicit publication authorization, with the release changes
merged, a clean checkout, the MIT license included in every archive,
and passing candidate checks. Replace `vX.Y.Z` below with a new, unused SemVer
version. Published version tags must not be reused. Check remote tags and
releases first, and never overwrite an existing published tag or release. The
current module path supports v0/v1; v2+ requires a module-path migration first.

```sh
git switch master
git pull --ff-only
git status --short
scripts/release-snapshot.sh
# Stop if the checkout is dirty or any check fails. Only after authorization:
git tag -a vX.Y.Z -m 'Release vX.Y.Z'
git push origin vX.Y.Z
```

Pushing an explicit `v...` tag runs the workflow gates again for that exact commit:
source tests, race detector, vet and build; GoReleaser check; tag packaging with
`goreleaser release --clean --skip=publish`; archive validation; and native smoke
on all four runners. Only after every gate succeeds does `gh release create
--verify-tag` publish the same tested archives and checksum file, without rebuilding
or creating another tag. SemVer prerelease tags produce prereleases. Only this
last job has `contents: write`; other jobs use `contents: read`. Pull requests and
master pushes produce CI snapshot artifacts only, retained for seven days.
The release job uses the built-in GitHub token; it needs no separate release
secret. The Homebrew formula is updated separately after publication.

Do not run `goreleaser release` without `--snapshot` or `--skip=publish` locally:
it can publish. If checks fail, inspect the failed job and repair the candidate;
do not bypass gates. The publish job fails if a release already exists; review
partial publication manually before retrying instead of overwriting assets or
moving a published tag. After a successful run, inspect the release assets and
download/verify the checksums as described in [the installation guide](INSTALLING.md).

Configuration reference: [GoReleaser quick start](https://goreleaser.com/getting-started/quick-start/),
[archives](https://goreleaser.com/customization/package/archives/), and
[snapshots](https://goreleaser.com/customization/publish/snapshots/).

## Updating the Homebrew tap

After verifying the public release assets, update `Formula/portbook.rb` in
[sviatoslav/homebrew-tap](https://github.com/sviatoslav/homebrew-tap) with the four
actual asset URLs and matching values from `checksums.txt`. Keep the MIT metadata
and the isolated functional test. Run `brew style sviatoslav/tap/portbook`,
`brew audit --strict --online --os=all --arch=all sviatoslav/tap/portbook`, and
`brew test sviatoslav/tap/portbook` after installation. Push the formula and verify
the tap's native installation CI on all four targets before announcing Homebrew
availability. The Portbook release workflow does not push to the tap or store a
cross-repository credential.
