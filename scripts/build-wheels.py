#!/usr/bin/env python3
"""Packs the release archives of kubestrata into one Python wheel per platform, for PyPI.

Usage: scripts/build-wheels.py <tag> <dir with the release archives and checksums.txt> <out dir>

Each wheel carries the same binary as the signed release archive, under
kubestrata-<version>.data/scripts/, so `pip install`, `uv tool install` and `uvx` put the Go
binary itself on PATH (no Python wrapper). Standard library only; the output is byte-for-byte
reproducible for the same inputs (fixed timestamps from SOURCE_DATE_EPOCH, sorted entries).
The archives are checked against checksums.txt before anything is read from them.
"""

import base64
import hashlib
import io
import os
import re
import sys
import tarfile
import time
import zipfile

NAME = "kubestrata"
SUMMARY = "Read-only, time-layered inventory of Kubernetes clusters for migration planning"

# Go 1.27 binaries need macOS 12 or newer; the Linux binaries are static (CGO_ENABLED=0), so
# they run on glibc and musl alike.
PLATFORMS = {
    "darwin_arm64": ["macosx_12_0_arm64"],
    "darwin_amd64": ["macosx_12_0_x86_64"],
    "linux_arm64": ["manylinux_2_17_aarch64", "manylinux2014_aarch64", "musllinux_1_1_aarch64"],
    "linux_amd64": ["manylinux_2_17_x86_64", "manylinux2014_x86_64", "musllinux_1_1_x86_64"],
}

DESCRIPTION = """\
# kubestrata

Read-only, time-layered inventory of Kubernetes clusters for migration planning: it reads the
contexts of your kubeconfig, collects a sanitized inventory into a local cache and renders an
offline HTML view, newest resources in the center. A comparison view puts two clusters side by
side.

This package ships the signed Go binary of the matching
[GitHub release](https://github.com/kubestrata/kubestrata/releases); there is no Python code.

```sh
uvx kubestrata --version          # run once, without installing
uv tool install kubestrata        # or: pipx install kubestrata
kubestrata contexts
```

Other install paths (Homebrew, `go install`, verified archives), usage and the security analysis:
<https://github.com/kubestrata/kubestrata>.
"""


def pep440(tag):
    """v0.1.4 -> 0.1.4, v0.1.4-rc.1 -> 0.1.4rc1 (also alpha/beta)."""
    m = re.fullmatch(r"v(\d+\.\d+\.\d+)(?:-(rc|alpha|beta|a|b)\.?(\d+))?", tag)
    if not m:
        raise SystemExit(f"unsupported tag: {tag}")
    base, pre, num = m.groups()
    if not pre:
        return base
    return base + {"alpha": "a", "a": "a", "beta": "b", "b": "b", "rc": "rc"}[pre] + num


def sha256_file(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def record_hash(data):
    digest = base64.urlsafe_b64encode(hashlib.sha256(data).digest()).rstrip(b"=").decode()
    return f"sha256={digest}"


def read_checksums(path):
    sums = {}
    with open(path) as f:
        for line in f:
            parts = line.split()
            if len(parts) == 2:
                sums[parts[1]] = parts[0]
    return sums


def extract(archive, member):
    with tarfile.open(archive, "r:gz") as t:
        f = t.extractfile(member)
        if f is None:
            raise SystemExit(f"{member} missing in {archive}")
        return f.read()


def build(tag, src, out):
    version = pep440(tag)
    file_version = tag[1:]  # archive names use the tag without the v
    sums = read_checksums(os.path.join(src, "checksums.txt"))
    epoch = int(os.environ.get("SOURCE_DATE_EPOCH", "315532800"))  # zip cannot go below 1980
    stamp = time.gmtime(max(epoch, 315532800))[:6]
    dist_info = f"{NAME}-{version}.dist-info"
    data_scripts = f"{NAME}-{version}.data/scripts"
    os.makedirs(out, exist_ok=True)
    built = []
    for platform, tags in sorted(PLATFORMS.items()):
        archive_name = f"{NAME}_{file_version}_{platform}.tar.gz"
        archive = os.path.join(src, archive_name)
        want = sums.get(archive_name)
        if want is None or sha256_file(archive) != want:
            raise SystemExit(f"checksum mismatch or missing for {archive_name}")
        binary = extract(archive, NAME)
        license_text = extract(archive, "LICENSE")
        tag_set = ".".join(tags)
        metadata = "\n".join([
            "Metadata-Version: 2.1",
            f"Name: {NAME}",
            f"Version: {version}",
            f"Summary: {SUMMARY}",
            "License: MIT",
            "Project-URL: Homepage, https://github.com/kubestrata/kubestrata",
            "Project-URL: Changelog, https://github.com/kubestrata/kubestrata/blob/main/CHANGELOG.md",
            "Project-URL: Security, https://github.com/kubestrata/kubestrata/blob/main/docs/security-analysis.md",
            "Keywords: kubernetes,inventory,migration,openshift,eks,kubeconfig",
            "Classifier: Environment :: Console",
            "Classifier: License :: OSI Approved :: MIT License",
            "Classifier: Operating System :: MacOS",
            "Classifier: Operating System :: POSIX :: Linux",
            "Classifier: Topic :: System :: Systems Administration",
            "Description-Content-Type: text/markdown",
            "",
            DESCRIPTION,
        ])
        wheel = "\n".join(["Wheel-Version: 1.0", "Generator: kubestrata scripts/build-wheels.py",
                           "Root-Is-Purelib: false"] + [f"Tag: py3-none-{t}" for t in tags]) + "\n"
        files = [
            (f"{data_scripts}/{NAME}", binary, 0o755),
            (f"{dist_info}/LICENSE", license_text, 0o644),
            (f"{dist_info}/METADATA", metadata.encode(), 0o644),
            (f"{dist_info}/WHEEL", wheel.encode(), 0o644),
        ]
        record = "".join(f"{p},{record_hash(d)},{len(d)}\n" for p, d, _ in files)
        record += f"{dist_info}/RECORD,,\n"
        files.append((f"{dist_info}/RECORD", record.encode(), 0o644))
        name = f"{NAME}-{version}-py3-none-{tag_set}.whl"
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as z:
            for path, data, mode in files:
                info = zipfile.ZipInfo(path, date_time=stamp)
                info.compress_type = zipfile.ZIP_DEFLATED
                info.external_attr = (0o100000 | mode) << 16
                z.writestr(info, data)
        with open(os.path.join(out, name), "wb") as f:
            f.write(buf.getvalue())
        built.append(name)
    for name in built:
        print(f"{sha256_file(os.path.join(out, name))}  {name}")


if __name__ == "__main__":
    if len(sys.argv) != 4:
        raise SystemExit(__doc__)
    build(sys.argv[1], sys.argv[2], sys.argv[3])
