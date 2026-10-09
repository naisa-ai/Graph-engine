import argparse
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import package_compliance as compliance


class ComplianceTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.source = self.root / "application"
        self.source.mkdir()
        self.write(self.source / "cmd/graph-engined/main.go", "package main\n")
        self.write(self.source / "LICENSES/GPL-3.0.txt", "GNU GENERAL PUBLIC LICENSE\nVersion 3\n")
        for component in compliance.MIT_COMPONENTS:
            self.write(self.source / component / "LICENSE", f"MIT License\nCopyright {component}\n")
        self.binary = self.source / "graph-engined"
        self.binary.write_bytes(b"compiled-server")
        self.goroot = self.root / "goroot"
        self.write(self.goroot / "LICENSE", "Go BSD copyright\n")
        self.write(self.goroot / "PATENTS", "Go patent grant\n")
        self.write(self.goroot / "VERSION", "go1.24.13\n")
        self.write(self.goroot / "src/runtime/runtime.go", "package runtime\n")
        self.write(self.goroot / "src/vendor/example/LICENSE", "Vendored standard library notice\n")
        self.cache = self.root / "gopath/pkg/mod/cache/download/example.org/module/@v"
        self.cache.mkdir(parents=True)
        self.module_zip = self.cache / "v1.2.3.zip"
        with zipfile.ZipFile(self.module_zip, "w") as archive:
            archive.writestr("example.org/module@v1.2.3/LICENSE", "Apache License\n")
            archive.writestr("example.org/module@v1.2.3/NOTICE.txt", "Required attribution\n")
            archive.writestr("example.org/module@v1.2.3/vendor/library/COPYING", "Nested copyright\n")
            archive.writestr("example.org/module@v1.2.3/main.go", "package module\n")
        self.write(self.cache / "v1.2.3.mod", "module example.org/module\n")
        self.write(self.cache / "v1.2.3.info", '{"Version":"v1.2.3"}\n')
        self.igraph = self.root / "igraph-0.10.15.tar.gz"
        with tarfile.open(self.igraph, "w:gz") as archive:
            data = b"igraph GPL-2.0-or-later\n"
            member = tarfile.TarInfo("igraph-0.10.15/COPYING")
            member.size = len(data)
            archive.addfile(member, io.BytesIO(data))
        self.work = self.root / "work"
        self.output = self.root / "output"
        self.inventory = self.root / "runtime.tsv"
        self.inventory.write_text("library:arm64\t1.0-2+b1\tlibrary-source\t1.0-2\n"
                                  "library-tools\t1.0-2\tlibrary-source\t1.0-2\n")
        self.doc = self.root / "doc"
        self.write(self.doc / "library/copyright", "LGPL library copyright\n")
        self.write(self.doc / "library-tools/copyright", "GPL tools copyright\n")
        self.common = self.root / "licenses"
        self.write(self.common / "LGPL-2.1", "LGPL license text\n")
        self.lib = self.root / "lib"
        self.lib.mkdir()
        (self.lib / "libigraph.so.3").write_bytes(b"igraph-shared-library")
        self.source_downloads = []

    @staticmethod
    def write(path, content):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)

    def command(self, *args, cwd=None):
        if args[:3] == ("go", "version", "-m"):
            return "server: go1.24.13\n\tdep\texample.org/module\tv1.2.3\th1:checksum\n"
        if args == ("go", "env", "GOPATH"):
            return str(self.root / "gopath") + "\n"
        if args == ("go", "env", "GOROOT"):
            return str(self.goroot) + "\n"
        if args[:3] == ("go", "env", "-json"):
            return json.dumps({"GOOS": "linux", "GOARCH": "arm64", "GOVERSION": "go1.24.13", "CGO_ENABLED": "1"})
        if args[:3] == ("go", "mod", "download"):
            return json.dumps(dict(Zip=str(self.module_zip), GoMod=str(self.cache / "v1.2.3.mod"),
                                   Info=str(self.cache / "v1.2.3.info"), Sum="h1:checksum", GoModSum="h1:modsum"))
        if args[0] == "dpkg-query":
            return "gcc\t12.2.0\n"
        if args[:3] == ("apt-get", "source", "--download-only"):
            self.source_downloads.append(args[3])
            self.write(Path(cwd) / "library_1.0-2.dsc", "Source: library-source\n")
            self.write(Path(cwd) / "library_1.0.orig.tar.xz", "original source\n")
            self.write(Path(cwd) / "library_1.0-2.debian.tar.xz", "Debian build scripts\n")
            return ""
        raise AssertionError(f"Unexpected invocation {args}")

    def prepare(self, revision="a" * 40):
        compliance.prepare(argparse.Namespace(source=str(self.source), work=str(self.work),
                           binary=str(self.binary), igraph_source=str(self.igraph), igraph_version="0.10.15",
                           revision=revision, version="v1.0", build_time="2026-10-09T00:00:00Z"))

    def finish(self):
        compliance.finish(argparse.Namespace(work=str(self.work), output=str(self.output),
                          binary=str(self.binary), inventory=str(self.inventory), doc_root=str(self.doc),
                          common_licenses=str(self.common), library_root=str(self.lib)))

    def build_bundle(self):
        with patch.object(compliance, "run", self.command):
            self.prepare()
            self.finish()

    def test_complete_bundle_preserves_exact_source_and_independent_licenses(self):
        self.build_bundle()
        info = compliance.verify(self.output, self.binary, self.lib, "a" * 40)
        self.assertEqual(info["independent_component_licenses"], {item: "MIT" for item in compliance.MIT_COMPONENTS})
        self.assertEqual(self.source_downloads, ["library-source=1.0-2"])
        self.assertEqual(info["runtime_debian_packages"][0]["version"], "1.0-2+b1")
        notices = (self.output / "THIRD_PARTY_NOTICES").read_text()
        for text in ("Required attribution", "Nested copyright", "Vendored standard library notice", "LGPL library copyright"):
            self.assertIn(text, notices)
        with tarfile.open(self.output / "corresponding-source.tar.gz") as archive:
            self.assertEqual(archive.extractfile("graph-engine/cmd/graph-engined/main.go").read(), b"package main\n")
            self.assertNotIn("graph-engine/graph-engined", archive.getnames())
            for item in compliance.MIT_COMPONENTS:
                self.assertIn(b"MIT License", archive.extractfile(f"graph-engine/{item}/LICENSE").read())

    def test_local_snapshot_does_not_claim_a_git_revision(self):
        with patch.object(compliance, "run", self.command):
            self.prepare(revision="")
        self.assertIsNone(json.loads((self.work / "metadata.json").read_text())["source_revision"])

    def test_rejects_short_or_non_sha_revisions(self):
        for value in ("main", "abcd123", "a" * 39, "A" * 40, "../secret"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                compliance.revision(value)

    def test_missing_mit_license_prevents_packaging(self):
        (self.source / "proto/LICENSE").unlink()
        with patch.object(compliance, "run", self.command), self.assertRaises(FileNotFoundError):
            self.prepare()

    def test_application_symlink_cannot_copy_unrelated_files_into_source(self):
        unrelated = self.root / "unrelated.conf"
        unrelated.write_text("unrelated private configuration\n")
        link = self.source / "internal/linked.conf"
        link.parent.mkdir(parents=True)
        link.symlink_to(unrelated)
        with patch.object(compliance, "run", self.command), self.assertRaisesRegex(ValueError, "Unexpected source symlink"):
            self.prepare()

    def test_missing_dependency_notice_prevents_packaging(self):
        with zipfile.ZipFile(self.module_zip, "w") as archive:
            archive.writestr("example.org/module/main.go", "package module\n")
        with patch.object(compliance, "run", self.command), self.assertRaisesRegex(ValueError, "No license"):
            self.prepare()

    def test_missing_go_toolchain_license_prevents_packaging(self):
        (self.goroot / "LICENSE").unlink()
        with patch.object(compliance, "run", self.command), self.assertRaisesRegex(ValueError, "Missing Go toolchain"):
            self.prepare()

    def test_empty_runtime_copyright_prevents_packaging(self):
        (self.doc / "library/copyright").write_text("\n")
        with patch.object(compliance, "run", self.command):
            self.prepare()
            with self.assertRaisesRegex(ValueError, "Empty Debian copyright"):
                self.finish()

    def test_go_source_must_match_binary_module_checksum(self):
        command = self.command
        def wrong_checksum(*args, **kwargs):
            value = command(*args, **kwargs)
            if args[:3] == ("go", "mod", "download"):
                data = json.loads(value)
                data["Sum"] = "h1:other"
                return json.dumps(data)
            return value
        with patch.object(compliance, "run", wrong_checksum), self.assertRaisesRegex(ValueError, "checksum mismatch"):
            self.prepare()

    def test_replacements_and_unversioned_modules_fail_closed(self):
        for info in ("\tdep\texample.org/module\tv1.2.3\n",
                     "\tdep\texample.org/module\t(devel)\th1:checksum\n",
                     "\tdep\texample.org/module\tv1.2.3\th1:checksum\n\t=>\t../proprietary\n"):
            with self.subTest(info=info), self.assertRaises(ValueError):
                compliance.module_dependencies(info)

    def test_empty_or_incomplete_package_provenance_fails_closed(self):
        for text in ("", "lib\t1.0\n", "lib\t1.0\tsource\t\n"):
            with self.subTest(text=text), self.assertRaises(ValueError):
                compliance.debian_inventory(text)

    def test_runtime_copyright_symlink_is_preserved_as_text(self):
        target = self.doc / "library-tools/copyright"
        target.unlink()
        target.symlink_to("../library/copyright")
        self.build_bundle()
        self.assertIn("LGPL library copyright", (self.output / "THIRD_PARTY_NOTICES").read_text())

    def test_missing_runtime_source_stops_the_build(self):
        command = self.command
        def missing_source(*args, **kwargs):
            if args[0] == "apt-get":
                return ""
            return command(*args, **kwargs)
        with patch.object(compliance, "run", missing_source):
            self.prepare()
            with self.assertRaisesRegex(ValueError, "Missing Debian source"):
                self.finish()

    def test_tampered_deliverables_are_rejected(self):
        self.build_bundle()
        for target in (self.binary, self.output / "NOTICE", self.output / "corresponding-source.tar.gz", self.lib / "libigraph.so.3"):
            original = target.read_bytes()
            target.write_bytes(original + b"changed")
            with self.subTest(target=target), self.assertRaises(ValueError):
                compliance.verify(self.output, self.binary, self.lib)
            target.write_bytes(original)
        with self.assertRaisesRegex(ValueError, "source revision"):
            compliance.verify(self.output, self.binary, expected_revision="b" * 40)

    def test_source_tampering_is_rejected_even_if_archive_hash_is_updated(self):
        self.build_bundle()
        self.write(self.work / "source/graph-engine/cmd/graph-engined/main.go", "different source\n")
        compliance.deterministic_archive(self.work / "source", self.output / "corresponding-source.tar.gz")
        info = json.loads((self.output / "BUILD_INFO.json").read_text())
        info["corresponding_source_sha256"] = compliance.digest(self.output / "corresponding-source.tar.gz")
        compliance.write_json(self.output / "BUILD_INFO.json", info)
        with self.assertRaisesRegex(ValueError, "Source file checksum mismatch"):
            compliance.verify(self.output, self.binary)

    def test_missing_required_file_is_rejected(self):
        self.build_bundle()
        (self.output / "COPYING").unlink()
        with self.assertRaisesRegex(ValueError, "Missing compliance file"):
            compliance.verify(self.output, self.binary)

    def test_missing_notice_checksum_and_changed_mit_scope_are_rejected(self):
        self.build_bundle()
        original = json.loads((self.output / "BUILD_INFO.json").read_text())
        altered = json.loads(json.dumps(original))
        del altered["notice_files_sha256"]["NOTICE"]
        compliance.write_json(self.output / "BUILD_INFO.json", altered)
        with self.assertRaisesRegex(ValueError, "Incomplete notice"):
            compliance.verify(self.output, self.binary)
        altered = json.loads(json.dumps(original))
        altered["independent_component_licenses"]["proto"] = "GPL-3.0-only"
        compliance.write_json(self.output / "BUILD_INFO.json", altered)
        with self.assertRaisesRegex(ValueError, "MIT component licenses changed"):
            compliance.verify(self.output, self.binary)

    def test_tar_is_deterministic_and_rejects_external_symlinks(self):
        directory = self.root / "arbitrary-source"
        self.write(directory / "odd name/λ.go", "arbitrary contents\n")
        first, second = self.root / "first.tar.gz", self.root / "second.tar.gz"
        compliance.deterministic_archive(directory, first)
        (directory / "odd name/λ.go").touch()
        compliance.deterministic_archive(directory, second)
        self.assertEqual(first.read_bytes(), second.read_bytes())
        (directory / "external").symlink_to(self.binary)
        with self.assertRaises(ValueError):
            compliance.deterministic_archive(directory, second)


if __name__ == "__main__":
    unittest.main()
