#!/usr/bin/env python3
"""Small actual-filesystem controls; no application test result is inferred."""

import hashlib
import os
from pathlib import Path
import shutil
import stat
import tempfile
import unittest

import go_test_image as guard


class TestGoTestImage(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.root.chmod(0o700)
        # A real small ELF is sufficient for image admission/copy controls.
        # These tests do not pretend that it contains any Go test root.
        self.original = self.root / "compiler-output"
        shutil.copyfile(Path("/usr/bin/true").resolve(), self.original)
        self.original.chmod(0o755)
        self.sha = hashlib.sha256(self.original.read_bytes()).hexdigest()

    def test_hardlinked_original_becomes_exact_owned_unique_image(self):
        os.link(self.original, self.root / "cache-alias")
        self.original.chmod(0o775)
        self.assertEqual(self.original.stat().st_nlink, 2)
        result = guard.stage_image(self.original, self.sha, self.root / "retained")
        self.assertEqual(result["original"]["links"], 2)
        self.assertEqual(result["original"]["mode"], 0o775)
        self.assertTrue(result["original"]["ancestors"])
        retained = Path(result["retained"]["path"])
        self.assertEqual(retained.stat().st_nlink, 1)
        self.assertEqual(stat.S_IMODE(retained.stat().st_mode), 0o500)
        self.assertEqual(retained.read_bytes(), self.original.read_bytes())
        guard.verify_image(result["retained"])

    def test_wrong_digest_never_becomes_an_admitted_image(self):
        with self.assertRaisesRegex(guard.Refused, "differs from exact pin"):
            guard.stage_image(self.original, "0" * 64, self.root / "wrong")

    def test_fifo_and_symlink_are_refused_without_blocking(self):
        fifo = self.root / "fifo"
        os.mkfifo(fifo)
        with self.assertRaisesRegex(guard.Refused, "bounded regular"):
            guard.stage_image(fifo, self.sha, self.root / "fifo-image")
        link = self.root / "symlink"
        link.symlink_to(self.original)
        with self.assertRaises(OSError):
            guard.stage_image(link, self.sha, self.root / "link-image")

    def test_declared_size_bound_precedes_destination_creation(self):
        destination = self.root / "too-big"
        with self.assertRaisesRegex(guard.Refused, "bounded regular"):
            guard.stage_image(self.original, self.sha, destination, maximum=4)
        self.assertFalse(destination.exists())

    def test_later_hardlink_or_permission_change_refuses_readback(self):
        for mutation in ("link", "mode"):
            destination = self.root / mutation
            result = guard.stage_image(self.original, self.sha, destination)
            if mutation == "link":
                os.link(destination, self.root / "new-alias")
            else:
                destination.chmod(0o700)
            with self.assertRaisesRegex(guard.Refused, "protected ownership"):
                guard.verify_image(result["retained"])

    def test_named_replacement_even_with_same_bytes_is_not_original_image(self):
        destination = self.root / "retained"
        result = guard.stage_image(self.original, self.sha, destination)
        destination.rename(self.root / "retained-original")
        shutil.copyfile(self.original, destination)
        destination.chmod(0o500)
        with self.assertRaisesRegex(guard.Refused, "protected ownership"):
            guard.verify_image(result["retained"])


if __name__ == "__main__":
    unittest.main()
