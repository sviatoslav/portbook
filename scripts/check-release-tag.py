#!/usr/bin/env python3
"""Accept SemVer v0/v1 tags matching the unsuffixed Go module path."""
import re
import sys

number = r"(?:0|[1-9][0-9]*)"
identifier = r"(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
pattern = rf"v[01]\.{number}\.{number}(?:-{identifier}(?:\.{identifier})*)?"
if len(sys.argv) != 2 or not re.fullmatch(pattern, sys.argv[1]):
    sys.exit("expected v0.MINOR.PATCH or v1.MINOR.PATCH, optionally with a SemVer prerelease; v2+ needs a module-path migration")
