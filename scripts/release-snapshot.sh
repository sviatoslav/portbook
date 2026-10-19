#!/bin/sh
# Local packaging only. Never tags, pushes, or publishes.
set -eu
cd "$(dirname "$0")/.."
python3 -m unittest discover -s scripts -p "*_test.py"
goreleaser check
goreleaser release --snapshot --clean
python3 scripts/verify-release.py dist --commit "$(git rev-parse HEAD)"
