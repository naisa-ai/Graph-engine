#!/usr/bin/env python3
"""Package the actual server inputs and notices; fail if an input is missing."""

import argparse
import gzip
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import zipfile


DOCUMENTS = ("COPYING", "NOTICE", "THIRD_PARTY_NOTICES", "BUILD_INFO.json",
             "corresponding-source.tar.gz")
NOTICE_NAME = re.compile(r"^(copying|copyright|licen[sc]e|notice|patents)([._-].*)?$", re.I)
MIT_COMPONENTS = ("clients/go", "clients/py", "proto", "gen")


def run(*args, cwd=None):
    return subprocess.check_output(args, cwd=cwd, text=True)


def digest(path):
    with Path(path).open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def write_json(path, value):
    Path(path).write_text(json.dumps(value, indent=2, sort_keys=True) + "\n")


def revision(value):
    if value and not re.fullmatch(r"[0-9a-f]{40}", value):
        raise ValueError("SOURCE_REVISION must be a full lowercase Git commit SHA")
    return value or None


def source_manifest(root):
    result = {}
    for path in sorted(Path(root).rglob("*")):
        if path.is_symlink():
            raise ValueError(f"Unexpected source symlink: {path}")
        if path.is_file():
            result[path.relative_to(root).as_posix()] = digest(path)
    if not result:
        raise ValueError(f"Empty source tree: {root}")
    return result


def archive_notices(path):
    notices = []
    if zipfile.is_zipfile(path):
        with zipfile.ZipFile(path) as archive:
            for name in sorted(archive.namelist()):
                if NOTICE_NAME.match(Path(name).name) and not name.endswith("/"):
                    notices.append((name, archive.read(name)))
    else:
        with tarfile.open(path) as archive:
            for member in sorted(archive.getmembers(), key=lambda item: item.name):
                if member.isfile() and NOTICE_NAME.match(Path(member.name).name):
                    notices.append((member.name, archive.extractfile(member).read()))
    if not notices:
        raise ValueError(f"No license/copyright notices found in {path}")
    return "".join(f"\n--- {name} ---\n{body.decode('utf-8', errors='replace')}\n"
                   for name, body in notices)


def module_dependencies(build_info):
    dependencies = []
    for line in build_info.splitlines():
        fields = line.strip().split("\t")
        if fields[0] == "=>":
            raise ValueError("Module replacements require an explicit source-packaging review")
        if fields[0] == "dep":
            if len(fields) != 4 or not fields[2].startswith("v") or not fields[3].startswith("h1:"):
                raise ValueError(f"Dependency has no immutable version/checksum: {line}")
            dependencies.append(dict(path=fields[1], version=fields[2], sum=fields[3]))
    if not dependencies:
        raise ValueError("Binary contains no Go dependency provenance")
    return dependencies


def prepare(args):
    source_revision = revision(args.revision)
    source = Path(args.source)
    work = Path(args.work)
    work.mkdir(parents=True, exist_ok=False)
    tree = work / "source" / "graph-engine"
    def ignore(directory, names):
        return [name for name in names if name == "__pycache__" or
                (Path(directory) == source and name == "graph-engined")]
    shutil.copytree(source, tree, ignore=ignore)
    manifest = source_manifest(tree)
    notice = ["Third-party notices for the distributed Graph Engine server.\n"
              "Individual components retain their original licenses and copyrights.\n"]
    for component in MIT_COMPONENTS:
        license_path = tree / component / "LICENSE"
        license_text = license_path.read_text()
        if "MIT License" not in license_text:
            raise ValueError(f"Missing MIT license for {component}")
        notice.append(f"\n=== Independently MIT licensed: {component} ===\n{license_text}")
    build_info = run("go", "version", "-m", args.binary)
    modules = module_dependencies(build_info)
    proxy_root = work / "source" / "go-proxy"
    module_cache = Path(run("go", "env", "GOPATH").strip()) / "pkg/mod/cache/download"
    for module in modules:
        downloaded = json.loads(run("go", "mod", "download", "-json",
                                    f"{module['path']}@{module['version']}", cwd=source))
        if downloaded.get("Error") or downloaded.get("Sum") != module["sum"]:
            raise ValueError(f"Go source checksum mismatch: {module['path']}")
        notice.append(f"\n=== Go module {module['path']} {module['version']} ===\n")
        notice.append(archive_notices(downloaded["Zip"]))
        module["go_mod_sum"] = downloaded["GoModSum"]
        module["source_files"] = {}
        for key in ("Zip", "GoMod", "Info"):
            path = Path(downloaded[key])
            target = proxy_root / path.relative_to(module_cache)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(path, target)
            module["source_files"][target.relative_to(work / "source").as_posix()] = digest(target)
    igraph_target = work / "source" / "igraph" / Path(args.igraph_source).name
    igraph_target.parent.mkdir(parents=True)
    shutil.copyfile(args.igraph_source, igraph_target)
    notice.append(f"\n=== igraph {args.igraph_version} (including bundled components) ===\n")
    notice.append(archive_notices(igraph_target))
    goroot = Path(run("go", "env", "GOROOT").strip())
    go_source = work / "source" / "go-toolchain"
    go_source.mkdir()
    for name in ("src", "lib", "misc", "api", "test", "LICENSE", "PATENTS", "VERSION",
                 "go.env", "README.md", "CONTRIBUTING.md", "SECURITY.md", "codereview.cfg"):
        path = goroot / name
        if path.is_dir():
            shutil.copytree(path, go_source / name)
        elif path.is_file():
            shutil.copyfile(path, go_source / name)
    for name in ("LICENSE", "PATENTS", "VERSION"):
        if not (go_source / name).is_file():
            raise ValueError(f"Missing Go toolchain provenance or notice: {name}")
    for path in sorted(go_source.rglob("*")):
        if path.is_file() and NOTICE_NAME.match(path.name):
            notice.append(f"\n=== Go toolchain / standard library: {path.relative_to(go_source)} ===\n"
                          + path.read_text(errors="replace"))
    write_json(work / "metadata.json", {
        "schema_version": 1,
        "source_repository": "https://github.com/naisa-ai/Graph-engine",
        "source_revision": source_revision,
        "source_revision_kind": "caller-supplied; CI derives from checkout" if args.revision else "local snapshot",
        "server_distribution_license": "GPL-3.0-only",
        "independent_component_licenses": {component: "MIT" for component in MIT_COMPONENTS},
        "version": args.version, "build_time": args.build_time,
        "go_build_info": build_info,
        "go_environment": json.loads(run("go", "env", "-json", "GOOS", "GOARCH", "GOVERSION", "CGO_ENABLED")),
        "builder_debian_packages": run("dpkg-query", "-W", "-f=${binary:Package}\t${Version}\n"),
        "graph_engine_source_sha256": manifest,
        "go_modules": modules,
        "igraph": {"version": args.igraph_version, "source_sha256": digest(igraph_target),
                   "source_file": igraph_target.relative_to(work / "source").as_posix(),
                   "source_url": f"https://github.com/igraph/igraph/releases/download/{args.igraph_version}/{igraph_target.name}"},
        "binary_sha256": digest(args.binary),
    })
    (work / "notices.txt").write_text("".join(notice))


def debian_inventory(text):
    packages = []
    for line in text.splitlines():
        fields = line.split("\t")
        if len(fields) != 4 or not all(fields):
            raise ValueError(f"Incomplete Debian source provenance: {line}")
        package, version, source, source_version = fields
        packages.append(dict(package=package, version=version,
                             source_package=source, source_version=source_version))
    if not packages:
        raise ValueError("Empty runtime package inventory")
    return packages


def deterministic_archive(root, target):
    with Path(target).open("wb") as stream, gzip.GzipFile(fileobj=stream, mode="wb", filename="", mtime=0) as compressed:
        with tarfile.open(fileobj=compressed, mode="w") as archive:
            for path in sorted(Path(root).rglob("*")):
                info = archive.gettarinfo(str(path), path.relative_to(root).as_posix())
                if not (info.isfile() or info.isdir()):
                    raise ValueError(f"Unexpected source archive entry: {path}")
                info.uid = info.gid = info.mtime = 0
                info.uname = info.gname = ""
                if info.isfile():
                    with path.open("rb") as payload:
                        archive.addfile(info, payload)
                else:
                    archive.addfile(info)


def finish(args):
    work, output = Path(args.work), Path(args.output)
    output.mkdir(parents=True, exist_ok=False)
    metadata = json.loads((work / "metadata.json").read_text())
    if digest(args.binary) != metadata["binary_sha256"]:
        raise ValueError("Runtime server does not match the prepared binary")
    packages = debian_inventory(Path(args.inventory).read_text())
    debian_root = work / "source" / "debian"
    debian_root.mkdir()
    notices = [(work / "notices.txt").read_text()]
    for package in packages:
        copyright_path = Path(args.doc_root) / package["package"].split(":")[0] / "copyright"
        content = copyright_path.read_text()
        if not content.strip():
            raise ValueError(f"Empty Debian copyright notice: {package['package']}")
        target = debian_root / "copyright" / package["package"]
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(content)
        notices.append(f"\n=== Debian {package['package']} {package['version']} ===\n{content}\n")
        package["copyright_sha256"] = digest(target)
    for source, version in sorted({(item["source_package"], item["source_version"]) for item in packages}):
        target = debian_root / "sources" / source / version
        target.mkdir(parents=True)
        run("apt-get", "source", "--download-only", f"{source}={version}", cwd=target)
        dsc_files = list(target.glob("*.dsc"))
        if len(dsc_files) != 1 or not list(target.glob("*.tar.*")):
            raise ValueError(f"Missing Debian source archive: {source}={version}")
        # APT verifies repository checksums during download. Preserve every source input.
    common = Path(args.common_licenses)
    shutil.copytree(common, debian_root / "common-licenses", symlinks=False)
    for path in sorted(common.iterdir()):
        if path.is_file():
            notices.append(f"\n=== Debian common license: {path.name} ===\n{path.read_text()}\n")
    metadata["runtime_debian_packages"] = packages
    metadata["runtime_shared_libraries_sha256"] = {
        str(path): digest(path) for path in sorted(Path(args.library_root).glob("libigraph.so*")) if path.is_file()
    }
    if not metadata["runtime_shared_libraries_sha256"]:
        raise ValueError("Runtime igraph shared library is missing")
    shutil.copyfile(work / "source/graph-engine/LICENSES/GPL-3.0.txt", output / "COPYING")
    (output / "THIRD_PARTY_NOTICES").write_text("".join(notices))
    (output / "NOTICE").write_text(
        "Graph Engine server\nCopyright (c) 2025-2026 Naisa AI, Inc.\n\n"
        "This server distribution is provided under GNU GPL version 3.\n"
        "Naisa server sources and igraph are GPL-2.0-or-later; this combined\n"
        "server uses GPLv3 to accommodate Apache-2.0 dependencies. Third-party\n"
        "components retain their own copyrights and licenses. See COPYING\n"
        "and THIRD_PARTY_NOTICES. There is no warranty; see COPYING.\n\n"
        "The Go/Python clients, proto definitions and generated stubs retain\n"
        "their independent MIT licenses; this notice does not relicense them.\n\n"
        "Complete corresponding source accompanies this image at\n"
        "/usr/share/doc/graph-engine/corresponding-source.tar.gz.\n"
        "Copy it out with docker create and docker cp, then extract with\n"
        "tar -xzf corresponding-source.tar.gz. Read REBUILD.md in the archive.\n"
        "BUILD_INFO.json identifies the snapshot, dependency versions and SHA256\n"
        "checksums. You may modify and redistribute under the applicable licenses.\n"
        "Repository: https://github.com/naisa-ai/Graph-engine\n"
    )
    (work / "source/REBUILD.md").write_text(
        "# Rebuilding the supplied source\n\n"
        "graph-engine/ contains the exact application inputs, Dockerfile and build scripts.\n"
        "SOURCE_MANIFEST.json records SHA256 for every supplied file. BUILD_INFO.json\n"
        "alongside the archive records the compiler, target, dependencies and binary.\n\n"
        "igraph/ contains the unmodified release archive actually compiled. Extract it\n"
        "and use the CMake flags in graph-engine/Dockerfile to install it to /usr/local.\n"
        "go-toolchain/ contains the matching Go toolchain and standard library sources;\n"
        "install the GOVERSION from BUILD_INFO.json or build these with a Go bootstrap\n"
        "compiler and src/make.bash. go-proxy/ contains exact module .zip/.mod/.info\n"
        "inputs in Go file-proxy format. Set GOPROXY=file:///absolute/path/go-proxy\n"
        "and GOSUMDB=off to build from these locally checksum-verified sources.\n\n"
        "In graph-engine/, with the recorded Go compiler and igraph installed:\n"
        "    CGO_ENABLED=1 CGO_CFLAGS=\"$(pkg-config --cflags igraph)\" \\\n"
        "    CGO_LDFLAGS=\"$(pkg-config --libs igraph)\" \\\n"
        "    go build -mod=readonly -tags cgo -o graph-engined ./cmd/graph-engined\n\n"
        "For matching version/time strings and linker settings, use the go_build_info\n"
        "and version/build_time in BUILD_INFO.json and the command in Dockerfile.\n"
        "debian/sources/ contains matching .dsc, original tarballs and Debian changes\n"
        "for every runtime Debian package. Run dpkg-source -x on a .dsc, then follow\n"
        "its debian/rules and package build instructions (dpkg-buildpackage).\n"
        "debian/copyright/ and debian/common-licenses/ preserve their notices.\n"
        "The Dockerfile records the installation and runtime configuration. Rebuilding\n"
        "does not require access to Naisa's image registry or proprietary code.\n"
    )
    metadata["corresponding_source_files_sha256"] = source_manifest(work / "source")
    write_json(work / "source/SOURCE_MANIFEST.json", metadata["corresponding_source_files_sha256"])
    deterministic_archive(work / "source", output / "corresponding-source.tar.gz")
    metadata["corresponding_source_sha256"] = digest(output / "corresponding-source.tar.gz")
    metadata["notice_files_sha256"] = {name: digest(output / name) for name in ("COPYING", "NOTICE", "THIRD_PARTY_NOTICES")}
    write_json(output / "BUILD_INFO.json", metadata)
    verify(output, args.binary, args.library_root)


def verify(output, binary, library_root=None, expected_revision=None):
    output = Path(output)
    for name in DOCUMENTS:
        if not (output / name).is_file() or not (output / name).stat().st_size:
            raise ValueError(f"Missing compliance file: {name}")
    info = json.loads((output / "BUILD_INFO.json").read_text())
    if info["schema_version"] != 1:
        raise ValueError("Unsupported compliance metadata schema")
    if set(info["notice_files_sha256"]) != {"COPYING", "NOTICE", "THIRD_PARTY_NOTICES"}:
        raise ValueError("Incomplete notice checksums")
    if info["independent_component_licenses"] != {item: "MIT" for item in MIT_COMPONENTS}:
        raise ValueError("Independent MIT component licenses changed")
    revision(info["source_revision"])
    if expected_revision and info["source_revision"] != revision(expected_revision):
        raise ValueError("Image source revision differs from checked-out commit")
    if digest(binary) != info["binary_sha256"]:
        raise ValueError("Server binary checksum mismatch")
    if digest(output / "corresponding-source.tar.gz") != info["corresponding_source_sha256"]:
        raise ValueError("Corresponding source archive checksum mismatch")
    for name, checksum in info["notice_files_sha256"].items():
        if digest(output / name) != checksum:
            raise ValueError(f"Notice checksum mismatch: {name}")
    if library_root:
        for path, checksum in info["runtime_shared_libraries_sha256"].items():
            if digest(Path(library_root) / Path(path).name) != checksum:
                raise ValueError(f"igraph checksum mismatch: {path}")
    with tarfile.open(output / "corresponding-source.tar.gz") as archive:
        files = {}
        for member in archive:
            name = member.name
            if name.startswith("/") or ".." in Path(name).parts or not (member.isfile() or member.isdir()):
                raise ValueError(f"Unsafe source archive entry: {name}")
            if member.isfile():
                if name in files:
                    raise ValueError(f"Duplicate source archive file: {name}")
                files[name] = hashlib.sha256(archive.extractfile(member).read()).hexdigest()
        manifest = info["corresponding_source_files_sha256"]
        for name, checksum in manifest.items():
            if files.get(name) != checksum:
                raise ValueError(f"Source file checksum mismatch: {name}")
        if set(files) != set(manifest) | {"SOURCE_MANIFEST.json"}:
            raise ValueError("Source archive contains unrecorded files")
        embedded = json.load(archive.extractfile("SOURCE_MANIFEST.json"))
        if embedded != manifest:
            raise ValueError("Embedded source manifest mismatch")
    return info


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    subparsers = parser.add_subparsers(dest="command", required=True)
    preparing = subparsers.add_parser("prepare")
    for name in ("source", "work", "binary", "igraph-source", "igraph-version"):
        preparing.add_argument(f"--{name}", required=True)
    preparing.add_argument("--revision", default="")
    preparing.add_argument("--version", default="dev")
    preparing.add_argument("--build-time", default="unknown")
    finishing = subparsers.add_parser("finish")
    for name in ("work", "output", "binary", "inventory"):
        finishing.add_argument(f"--{name}", required=True)
    finishing.add_argument("--doc-root", default="/usr/share/doc")
    finishing.add_argument("--common-licenses", default="/usr/share/common-licenses")
    finishing.add_argument("--library-root", default="/usr/local/lib")
    verifying = subparsers.add_parser("verify")
    verifying.add_argument("--output", required=True)
    verifying.add_argument("--binary", required=True)
    verifying.add_argument("--library-root")
    verifying.add_argument("--expected-revision")
    args = parser.parse_args()
    if args.command == "prepare":
        prepare(args)
    elif args.command == "finish":
        finish(args)
    else:
        verify(args.output, args.binary, args.library_root, args.expected_revision)
        print("Compliance bundle, binary and source checksums verified")


if __name__ == "__main__":
    main()
