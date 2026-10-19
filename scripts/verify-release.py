#!/usr/bin/env python3
"""Verify all packaged targets, then execute only the native archive in isolated state."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import struct
import subprocess
import tarfile
import tempfile


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def verify(dist, expected_version, commit):
    metadata = json.loads((dist / "metadata.json").read_text())
    version = metadata["version"]
    if expected_version:
        require(version == expected_version, f"version {version!r} != {expected_version!r}")
    else:
        require(re.fullmatch(r".+-snapshot-[0-9a-f]+", version), "expected a snapshot version")
        require(commit.startswith(version.rsplit("-", 1)[1]), "snapshot commit mismatch")
    require(metadata["commit"] == commit, "metadata commit mismatch")
    targets = {(system, arch) for system in ("darwin", "linux") for arch in ("amd64", "arm64")}
    names = {f"portbook_{version}_{system}_{arch}.tar.gz" for system, arch in targets}
    require({p.name for p in dist.glob("*.tar.gz")} == names, "unexpected archive set")
    checksums = {}
    for line in (dist / "checksums.txt").read_text().splitlines():
        digest, name = line.split()
        require(re.fullmatch(r"[0-9a-f]{64}", digest), "invalid SHA256")
        require(name not in checksums, "duplicate checksum")
        checksums[name] = digest
    require(set(checksums) == names, "checksum manifest must cover exactly the archives")
    native = (platform.system().lower(), {"x86_64": "amd64", "aarch64": "arm64"}.get(platform.machine(), platform.machine()))
    require(native in targets, f"unsupported smoke host {native}")
    root = Path(__file__).resolve().parent.parent
    docs = {"README.md", "docs/RELEASING.md", "LICENSE"}
    with tempfile.TemporaryDirectory(prefix="portbook-artifact-") as temporary:
        temp = Path(temporary).resolve()
        binary = temp / "portbook"
        for system, arch in sorted(targets):
            name = f"portbook_{version}_{system}_{arch}.tar.gz"
            archive = dist / name
            require(hashlib.sha256(archive.read_bytes()).hexdigest() == checksums[name], f"checksum failed: {name}")
            with tarfile.open(archive) as package:
                members = package.getmembers()
                require(len(members) == len(docs) + 1, f"unexpected member count: {name}")
                require({m.name for m in members} == docs | {"portbook"}, f"unexpected members: {name}")
                require(all(m.isfile() for m in members), "archives must contain only regular files")
                for doc in docs:
                    require(package.extractfile(doc).read() == (root / doc).read_bytes(), f"document mismatch: {doc}")
                member = package.getmember("portbook")
                require(member.mode & 0o111 == 0o111 and not member.mode & 0o6000, "executable permissions")
                data = package.extractfile(member).read()
            if system == "linux":
                require(data[:6] == b"\x7fELF\x02\x01", f"expected little-endian ELF64: {name}")
                require(struct.unpack_from("<H", data, 18)[0] == {"amd64": 62, "arm64": 183}[arch], "ELF architecture mismatch")
            else:
                require(data[:4] == b"\xcf\xfa\xed\xfe", f"expected Mach-O64: {name}")
                require(struct.unpack_from("<I", data, 4)[0] == {"amd64": 0x1000007, "arm64": 0x100000C}[arch], "Mach-O architecture mismatch")
            inspected = temp / f"inspect-{system}-{arch}"
            inspected.write_bytes(data)
            build = subprocess.check_output(["go", "version", "-m", str(inspected)], text=True)
            for field in ("path\tgithub.com/sviatoslav/portbook/cmd/portbook", f"GOOS={system}", f"GOARCH={arch}", "CGO_ENABLED=0"):
                require(field in build, f"missing build metadata {field}: {name}")
            if (system, arch) == native:
                binary.write_bytes(data)
                binary.chmod(0o755)
            print(f"verified archive/checksum/platform: {system}/{arch}")
        smoke(binary, temp, version, commit)


def smoke(binary, temp, version, commit):
    env = dict(os.environ, HOME=str(temp / "home"), XDG_CONFIG_HOME=str(temp / "xdg"), PORTBOOK_CONFIG_DIR=str(temp / "config"))
    def run(*args, code=0):
        result = subprocess.run([str(binary), *args], env=env, cwd=temp, capture_output=True, text=True, timeout=30)
        require(result.returncode == code, f"{args}: exit {result.returncode}, {result.stderr}")
        require(not result.stderr if code == 0 else not result.stdout, f"unexpected output: {result}")
        return result.stdout if code == 0 else result.stderr
    require(run("--version") == f"portbook {version} (commit {commit})\n", "release version output mismatch")
    require("Usage:" in json.loads(run("--help"))["help"], "JSON help mismatch")
    require("Usage:" in run("--output", "table", "--help"), "table help mismatch")
    require(json.loads(run("list", "--all"))["bookings"] == [], "fresh list")
    require("reservation not found" in run("get", code=1), "fresh missing get")
    require(json.loads(run("cleanup", "--dry-run"))["rows"] == [], "fresh cleanup")
    require(all(not Path(env[key]).exists() for key in ("HOME", "XDG_CONFIG_HOME", "PORTBOOK_CONFIG_DIR")), "read-only commands wrote state")
    require(json.loads(run("--output", "invalid", code=2))["error"]["code"] == "usage_error", "error contract")
    config = json.loads(run("config", "show"))
    require(config["ranges"] == [{"start": 49152, "end": 65535}] and config["total"] == 16384, "missing config defaults")
    require(not Path(env["PORTBOOK_CONFIG_DIR"]).exists(), "config show wrote defaults")
    # Execute the README quick start in a genuinely unconfigured installation.
    default = run("reserve")
    api = run("reserve", "api")
    for port in (default, api):
        require(re.fullmatch(r"[0-9]+\n", port) and 49152 <= int(port) <= 65535, "default bare port output")
    require(default != api, "distinct services shared a port")
    registry = next(temp.rglob("bookings.json"))
    before = registry.read_bytes()
    identity = registry.stat()
    require(run("get", "api") == api, "saved port lookup")
    duplicate = json.loads(run("reserve", "api", code=1))["error"]
    require(duplicate["code"] == "runtime_error" and "reservation already exists" in duplicate["message"], "duplicate reserve")
    require(registry.read_bytes() == before and registry.stat().st_ino == identity.st_ino and registry.stat().st_mtime_ns == identity.st_mtime_ns, "get/duplicate reserve rewrote state")
    require(not Path(env["PORTBOOK_CONFIG_DIR"]).exists(), "reserve persisted default config")
    rows = json.loads(run("list"))["bookings"]
    require(rows == [{"directory": str(temp), "service": "api", "port": int(api)}, {"directory": str(temp), "service": "default", "port": int(default)}], "README list bookings")
    require(json.loads(run("config", "remove", "--all"))["changed"] == 16384, "explicit empty pool")
    require(run("get", "api", "--output", "json") == api, "empty-pool get")
    require(json.loads(run("reserve", "new", code=1))["error"]["code"] == "runtime_error", "empty pool must disable new allocations")
    require(json.loads(run("release", "api"))["released"], "README release")
    before = registry.read_bytes()
    identity = registry.stat()
    for command in ("release", "get"):
        missing = json.loads(run(command, "api", code=1))["error"]
        require(missing["code"] == "runtime_error" and "reservation not found" in missing["message"], f"missing {command}")
    require(registry.read_bytes() == before and registry.stat().st_ino == identity.st_ino and registry.stat().st_mtime_ns == identity.st_mtime_ns, "missing lifecycle command rewrote state")
    require(json.loads(run("release"))["released"], "release default")
    require(json.loads(run("config", "add", "49152-65535"))["total"] == 16384, "custom pool")
    owner = temp / "owner"
    owner.mkdir()
    run("reserve", "abandoned", "--directory", str(owner))
    owner.rmdir()
    require(json.loads(run("cleanup", "--dry-run"))["would_remove"] == 1, "cleanup preview")
    require(json.loads(run("cleanup"))["removed"] == 1, "cleanup mutation")
    require(json.loads(run("list", "--all"))["bookings"] == [], "final empty list")
    print(f"native extracted binary lifecycle passed: {platform.system()}/{platform.machine()}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("dist", type=Path)
    parser.add_argument("--version", help="required exact version for a tag build; otherwise require a snapshot")
    parser.add_argument("--commit", required=True, help="expected full Git commit")
    args = parser.parse_args()
    verify(args.dist.resolve(), args.version, args.commit)
