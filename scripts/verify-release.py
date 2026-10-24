#!/usr/bin/env python3
"""Verify all packaged targets, then execute only the native archive in isolated state."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import posixpath
import re
import struct
import socket
import subprocess
import tarfile
import tempfile
from urllib.parse import unquote, urlsplit


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def verify_doc_links(documents):
    """Check relative documentation links against the actual archive contents."""
    for name, data in documents.items():
        if not name.endswith(".md"):
            continue
        text = re.sub(r"```.*?```", "", data.decode("utf-8"), flags=re.S)
        for target in re.findall(r"\[[^\]]+\]\(([^)]+)\)", text):
            link = urlsplit(target)
            if link.scheme or link.netloc:
                continue
            path = posixpath.normpath(posixpath.join(posixpath.dirname(name), unquote(link.path))) if link.path else name
            require(path in documents, f"broken packaged link: {name}: {target}")
            if link.fragment:
                headings = re.findall(r"^#+ (.+)$", documents[path].decode("utf-8"), re.M)
                anchors = {re.sub(r"[^\w\- ]", "", heading.lower()).replace(" ", "-") for heading in headings}
                require(unquote(link.fragment) in anchors, f"broken packaged anchor: {name}: {target}")


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
    docs = {"README.md", "docs/RELEASING.md", "docs/INSTALLING.md", "docs/REFERENCE.md", "docs/DEVELOPING.md", "docs/AGENT-SETUP.md", "LICENSE"}
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
                documents = {doc: package.extractfile(doc).read() for doc in docs}
                for doc, content in documents.items():
                    require(content == (root / doc).read_bytes(), f"document mismatch: {doc}")
                verify_doc_links(documents)
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
    require("Usage:" in run("config", "show", "--output", "table", "--help"), "table help mismatch")
    require("claude-code" in json.loads(run("install-skills", "--help"))["help"], "installer help")
    fake_bin = temp / "fake-bin"
    fake_bin.mkdir()
    previous_path = env.get("PATH")
    env["PATH"] = str(fake_bin)
    try:
        require("install Node.js" in run("install-skills", code=1), "missing installer dependency")
        fake_npx = fake_bin / "npx"
        fake_npx.write_text("#!/bin/sh\nprintf '%s\\n' \"$@\"\n")
        fake_npx.chmod(0o700)
        for agent in ("codex", "claude-code"):
            expected = "\n".join(["--yes", "skills", "add", "sviatoslav/portbook-skills", "--skill", "portbook", "--global", "--agent", agent]) + "\n"
            require(run("install-skills", "--agent", agent) == expected, "installer argv/passthrough")
    finally:
        if previous_path is None:
            env.pop("PATH", None)
        else:
            env["PATH"] = previous_path
    require(json.loads(run("list", "--all"))["bookings"] == [], "fresh list")
    for command in ("get", "reserve", "release"):
        missing = json.loads(run(command, code=2))["error"]
        require(missing["code"] == "usage_error" and "requires exactly one service" in missing["message"], f"omitted service: {command}")
    require("reservation not found" in run("get", "default", code=1), "fresh missing get")
    require(json.loads(run("cleanup", "--dry-run"))["rows"] == [], "fresh cleanup")
    require(all(not Path(env[key]).exists() for key in ("HOME", "XDG_CONFIG_HOME", "PORTBOOK_CONFIG_DIR")), "read-only commands wrote state")
    require(json.loads(run("--output", "invalid", code=2))["error"]["code"] == "usage_error", "error contract")
    config = json.loads(run("config", "show"))
    require(config["ranges"] == [{"start": 49152, "end": 65535}] and config["total"] == 16384, "missing config defaults")
    require(not Path(env["PORTBOOK_CONFIG_DIR"]).exists(), "config show wrote defaults")
    # Exercise named services in a genuinely unconfigured installation.
    default = run("reserve", "default")
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
    require(run("config", "remove", "--all") == "Removed 16384 ports. Total: 0 ports.\n", "explicit empty pool")
    require(run("get", "api") == api, "empty-pool get")
    require(json.loads(run("reserve", "new", code=1))["error"]["code"] == "runtime_error", "empty pool must disable new allocations")
    require(run("config", "reset") == "Reset 16384 ports. Total: 16384 ports.\n", "pool reset defaults")
    require(registry.read_bytes() == before, "pool reset rewrote bookings")
    require(run("config", "reset") == "Reset 0 ports. Total: 16384 ports.\n", "idempotent pool reset")
    require(json.loads(run("config", "show"))["ranges"] == [{"start": 49152, "end": 65535}], "reset normalized ranges")
    require(run("config", "add", "49152") == "Added 0 ports. Total: 16384 ports.\n", "idempotent add")
    target = temp / "saved-choice"
    details = f'Effective directory: {env["PORTBOOK_CONFIG_DIR"]}\nConfig file: {env["PORTBOOK_CONFIG_DIR"]}/config.json\nSource: environment\n'
    for _ in range(2):
        require(run("config", "path", "set", str(target)) == f"Saved directory: {target}\n" + details, "plain path set with override")
    state_before = {p: p.read_bytes() for p in temp.rglob("*") if p.is_file()}
    for args in (
        ("config", "add", "1"), ("config", "remove", "49152"), ("config", "remove", "--all"),
        ("config", "reset"), ("config", "path", "set", str(target)), ("config", "path", "reset"),
        ("config",), ("config", "path"),
    ):
        for flag in (("--output", "json"), ("--output=table", "--help")):
            require(json.loads(run(*args, *flag, code=2))["error"]["code"] == "usage_error", f"unsupported config output: {args}")
    for args in (("config", "--output=json", "show"), ("config", "path", "--output=table", "get")):
        require(json.loads(run(*args, code=2))["error"]["code"] == "usage_error", "nested prefix output")
    require(state_before == {p: p.read_bytes() for p in temp.rglob("*") if p.is_file()}, "unsupported output mutated state")
    for _ in range(2):
        require(run("config", "path", "reset") == "Saved directory: (none; OS default)\n" + details, "plain path reset with override")
    require(run("config", "remove", "49152") == "Removed 1 port. Total: 16383 ports.\n", "plain remove")
    require(run("config", "remove", "49152") == "Removed 0 ports. Total: 16383 ports.\n", "idempotent remove")
    require(run("config", "remove", "--all") == "Removed 16383 ports. Total: 0 ports.\n", "plain clear")
    require(run("config", "remove", "--all") == "Removed 0 ports. Total: 0 ports.\n", "idempotent clear")
    for args in (("list",), ("cleanup", "--dry-run"), ("config", "show"), ("config", "path", "get")):
        require(isinstance(json.loads(run(*args, "--output=json")), dict), "leaf JSON override")
        require(not run(*args, "--output=table").startswith("{"), "leaf table override")
    previous_path = env.get("PATH")
    env["PATH"] = str(temp / "no-executables")
    try:
        for command in ("get", "reserve", "release", "install-skills"):
            require(json.loads(run(command, "--output", "json", code=2))["error"]["code"] == "usage_error", f"unsupported output: {command}")
    finally:
        if previous_path is None:
            env.pop("PATH", None)
        else:
            env["PATH"] = previous_path
    require(json.loads(run("--output", "json", "list", code=2))["error"]["code"] == "usage_error", "root-prefix output")
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", int(api)))
        listener.listen()
        require(run("release", "api") == f'Released port {int(api)} for "api" in "{temp}".\n', "busy single release")
        with socket.create_connection(listener.getsockname(), timeout=5):
            pass
    before = registry.read_bytes()
    identity = registry.stat()
    for command in ("release", "get"):
        missing = json.loads(run(command, "api", code=1))["error"]
        require(missing["code"] == "runtime_error" and "reservation not found" in missing["message"], f"missing {command}")
    require(registry.read_bytes() == before and registry.stat().st_ino == identity.st_ino and registry.stat().st_mtime_ns == identity.st_mtime_ns, "missing lifecycle command rewrote state")
    require(run("release", "default") == f'Released port {int(default)} for "default" in "{temp}".\n', "release default")
    require(run("config", "add", "49152-65535") == "Added 16384 ports. Total: 16384 ports.\n", "custom pool")
    busy = run("reserve", "busy")
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
        listener.bind(("127.0.0.1", int(busy)))
        listener.listen()
        idle = run("reserve", "idle")
        bulk = run("release", "--all")
        ports = ", ".join(str(port) for port in sorted([int(busy), int(idle)]))
        require(bulk == f'Released 2 bookings in "{temp}": {ports}.\n', "owner bulk release")
        with socket.create_connection(listener.getsockname(), timeout=5):
            pass
        replacement = run("reserve", "replacement")
        require(re.fullmatch(r"[0-9]+\n", replacement) and replacement != busy, "released busy port must be skipped")
        run("release", "replacement")
    before = registry.read_bytes()
    require("reservation not found" in run("release", "--all", code=1), "missing bulk release")
    require(registry.read_bytes() == before, "missing bulk release rewrote state")
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
