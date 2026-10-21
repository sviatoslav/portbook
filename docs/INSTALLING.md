# Installing Portbook

[Back to README](../README.md) · [Command reference](REFERENCE.md)

The public repository is available now under the [MIT License](../LICENSE).
Install with Homebrew, download an available binary from
[GitHub Releases](https://github.com/sviatoslav/portbook/releases), or build from
source. This guide targets v0.3.0. Check `portbook --version` and read the
[migration notes](RELEASING.md#v030-release-notes-and-migration) when upgrading
from earlier versions.

## Homebrew

```sh
brew install sviatoslav/tap/portbook
portbook --version
```

The [tap](https://github.com/sviatoslav/homebrew-tap) selects the released binary
for macOS or Linux on x86-64 or ARM64. No Go toolchain is required.

## Download a release

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

The following example uses `v0.3.0` on macOS arm64.
Replace the version and archive suffix using the table. Run these commands from
the directory containing the two downloads. Stop if checksum verification fails:

```sh
archive=portbook_0.3.0_darwin_arm64.tar.gz
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
Version output is one plain-text line and reads no configuration or booking data.
See the [command reference](REFERENCE.md#output-and-exit-codes) for output flags
and version migration notes.

## Build or install from source

Requires Go 1.26 or newer and macOS or Linux. A source build includes only
the commands present in that checkout. The v0.3.0 source includes `get`,
strict reservation creation, required service names, record-only release, owner-scoped `release --all`, and the
`install-skills` command. Version 0.3.0 also adds `config reset`, command-scoped
`--output`, and plain release confirmations. These features vary in older versions.
From an accessible checkout:

```sh
go build -o bin/portbook ./cmd/portbook
./bin/portbook --version
./bin/portbook --help
go install ./cmd/portbook
```

`go install` writes to `GOBIN`, or `$(go env GOPATH)/bin` when GOBIN is unset.
Add that directory to PATH. To install a specific published version from source:

```sh
# Choose a published version tag.
go install github.com/sviatoslav/portbook/cmd/portbook@v0.3.0
```

## Update or uninstall

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
and bookings remain in the [documented storage locations](REFERENCE.md#configuration-locations-storage-and-locking); no automatic data
cleanup or migration occurs.

Maintainers: see [release preparation and publication](RELEASING.md) for
local snapshot checks, CI gates, and intentional tag steps.
