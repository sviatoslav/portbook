# Releasing

[README](../README.md) · [Development](DEVELOPING.md) · [Installation](INSTALLING.md)

## Verify a snapshot

Use Go matching `go.mod`, Python 3.9+, Git, and the GoReleaser version pinned in
`.github/workflows/test.yml`. After the [source checks](DEVELOPING.md#checks), run:

```sh
scripts/release-snapshot.sh
```

The script runs release-tool tests, `goreleaser check`, local snapshot packaging,
and `scripts/verify-release.py`. Output goes to ignored `dist/`.

Verification checks the four macOS/Linux x86-64/ARM64 archives, SHA256 checksums,
build metadata, executable permissions, MIT license, and documentation links.
It runs the extracted native binary through a lifecycle smoke test using temporary
state directories. Cross-builds verify packaging; native runs verify execution.

Each archive includes README.md, LICENSE, and the five guides in `docs/`.
Keep `.goreleaser.yaml` and `scripts/verify-release.py` aligned when changing this list.

## Publish

Publication starts with explicit authorization, a clean checkout of the intended
commit, passing checks, and a fresh review of the candidate. Choose an unused
`v0.MINOR.PATCH` or `v1.MINOR.PATCH` tag; optional SemVer prerelease suffixes are
supported. The canonical module is `github.com/sviatoslav/portbook`.
A v2 release requires a Go module-path update.

Replace `vX.Y.Z` with the chosen tag after checking existing tags and releases:

```sh
git tag -a vX.Y.Z -m 'Release vX.Y.Z'
git push origin vX.Y.Z
```

CI runs source tests, race detection, vet, and build checks, then packages once
and smoke-tests the extracted artifacts natively on all four targets. The final
tag-only job publishes those exact archives and checksums. Write permission is
confined to that publication job. Pull requests and main pushes produce
snapshot artifacts with read-only repository permissions.

Review the completed workflow and published assets. Preserve published tags and
release assets; investigate a partial publication before retrying it.

## Coordinate distribution

As separately authorized release work:

- Check the [agent skill](https://github.com/sviatoslav/portbook-skills) against
  the released commands and output. Update its examples and compatibility guidance.
- Update the [Homebrew formula](https://github.com/sviatoslav/homebrew-tap/blob/main/Formula/portbook.rb)
  with the verified archive URLs and checksums. Align its functional test with
  the released behavior, then run style, audit, and installation tests.
- Verify the tap's native installation checks before announcing availability.

Tags, pushes, releases, Homebrew changes, and repository visibility or license
changes each require explicit user authorization.
