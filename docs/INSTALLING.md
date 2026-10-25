# Install Portbook

[README](../README.md) · [Command reference](REFERENCE.md)

Choose Homebrew, a release archive, or a source build. These guides describe the
current source tree; use `portbook COMMAND --help` for your installed build's
options and output formats.

## Homebrew

```sh
brew install sviatoslav/tap/portbook
portbook --version
```

Homebrew selects the binary for your system. To update:

```sh
brew update
brew upgrade sviatoslav/tap/portbook
```

## Release archive

Download an archive and `checksums.txt` from the same
[GitHub release](https://github.com/sviatoslav/portbook/releases) into a fresh directory.
Choose the archive suffix for your system:

| System | Processor | Suffix |
| --- | --- | --- |
| macOS | Apple Silicon | `darwin_arm64.tar.gz` |
| macOS | Intel | `darwin_amd64.tar.gz` |
| Linux | ARM64 | `linux_arm64.tar.gz` |
| Linux | Intel/AMD x86-64 | `linux_amd64.tar.gz` |

Set `archive` below to the downloaded filename. The commands verify its checksum,
extract it, and install it into your user's executable directory:

```sh
archive=portbook_VERSION_OS_ARCH.tar.gz
awk -v name="$archive" '$2 == name { print }' checksums.txt > selected-checksum.txt
test "$(wc -l < selected-checksum.txt | tr -d ' ')" = 1 &&
  shasum -a 256 --check selected-checksum.txt &&
  tar -xzf "$archive" &&
  mkdir -p "$HOME/.local/bin" &&
  install -m 755 portbook "$HOME/.local/bin/portbook"
```

On Linux, `sha256sum --check selected-checksum.txt` is an alternative to `shasum`.
Add `export PATH="$HOME/.local/bin:$PATH"` to your shell startup file, open a new
shell, and run `portbook --version`. To update, repeat these steps with your
chosen release. macOS archives contain unsigned binaries; follow macOS's app
approval prompt when one appears.

## Build from source

Use Go matching `go.mod`. From the repository root:

```sh
go build -o bin/portbook ./cmd/portbook
./bin/portbook --help
go install ./cmd/portbook
```

`go install` places the executable in `GOBIN`, or `$(go env GOPATH)/bin` by default.
Add that directory to your `PATH`. Source builds report `portbook dev (commit unknown)`;
release builds report their version and commit.

## Check or remove an installation

Use `command -v portbook` and `portbook --version` to identify the active executable.

For Homebrew, run `brew uninstall portbook`. For an archive installation, remove
`$HOME/.local/bin/portbook`. For a Go installation, remove the executable from
`GOBIN` or your Go bin directory. Saved configuration and bookings remain in the
[Portbook state directory](REFERENCE.md#configuration-locations).
