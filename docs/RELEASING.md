# Preparing and publishing a release

This guide covers local snapshot checks and intentional release publication.
The public project uses the MIT license in `LICENSE`. Creating/pushing version
tags and publishing releases require explicit user authorization.

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
archives, their exact documentation files, executable modes, SHA256 manifest,
ELF/Mach-O architecture, Go module/build metadata and commit. It extracts and
runs only the host's native binary. The smoke test covers version, table/JSON
help, error output, fresh read-only list/cleanup, missing-config defaults of
49152–65535 (16,384 ports), config changes, bare-port reserve
and reuse, listing, release and no-op release, and abandoned-owner cleanup.
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
and passing candidate checks. The commands below use the first release version,
`v0.1.0`; select a new SemVer version for later releases. Check remote tags and
releases first, and never overwrite an existing published tag or release. The
current module path supports v0/v1; v2+ requires a module-path migration first.

```sh
git switch master
git pull --ff-only
git status --short
scripts/release-snapshot.sh
# Stop if the checkout is dirty or any check fails. Only after authorization:
git tag -a v0.1.0 -m 'Release v0.1.0'
git push origin v0.1.0
```

Pushing an explicit `v...` tag runs the workflow gates again for that exact commit:
source tests, race detector, vet and build; GoReleaser check; tag packaging with
`goreleaser release --clean --skip=publish`; archive validation; and native smoke
on all four runners. Only after every gate succeeds does `gh release create
--verify-tag` publish the same tested archives and checksum file, without rebuilding
or creating another tag. SemVer prerelease tags produce prereleases. Only this
last job has `contents: write`; other jobs use `contents: read`. Pull requests and
master pushes produce CI snapshot artifacts only, retained for seven days.
No separate release secret, Homebrew tap, or installer service is needed.

Do not run `goreleaser release` without `--snapshot` or `--skip=publish` locally:
it can publish. If checks fail, inspect the failed job and repair the candidate;
do not bypass gates. The publish job fails if a release already exists; review
partial publication manually before retrying instead of overwriting assets or
moving a published tag. After a successful run, inspect the release assets and
download/verify the checksums as described in README.md.

Configuration reference: [GoReleaser quick start](https://goreleaser.com/getting-started/quick-start/),
[archives](https://goreleaser.com/customization/package/archives/), and
[snapshots](https://goreleaser.com/customization/publish/snapshots/).
