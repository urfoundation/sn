"""Deterministic source-custody regressions; no compiler or application runs."""

import copy
import errno
import hashlib
import json
import os
from pathlib import Path
import tempfile
import threading
import unittest
from unittest import mock

import source_verification as source


class SourceVerificationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.stage = self.root / "stage"
        self.stage.mkdir()
        (self.stage / "nested").mkdir()
        (self.stage / "empty").mkdir()
        self.first = self.stage / "first.go"
        self.second = self.stage / "nested" / "second.h"
        self.first.write_bytes(b"original first source\n")
        self.second.write_bytes(b"original nested C source\n")
        self.files = {str(path): hashlib.sha256(path.read_bytes()).hexdigest()
                      for path in (self.first, self.second)}
        self.census = {str(self.stage): ["first.go", "nested/second.h"]}
        self.context = {"source_closure": {"sha256": "1" * 64},
                        "dependency_graph": {"sha256": "2" * 64},
                        "tools": [{"sha256": "3" * 64}],
                        "configuration": {"GOWORK": "off", "packages": ["."]},
                        "mode": "normal", "trust_profile": source.TRUST_PROFILE}
        self.retry = source.ReadPolicy(delay_seconds=0, timeout_seconds=30)

    def capture(self, **kwargs):
        return source.SourceVerification.capture(self.files, self.census, self.context,
                                                 retry=kwargs.get("retry", self.retry))

    def test_repeated_phases_reuse_full_content_and_closed_census_proof(self):
        original_hash = source._hash_file
        with mock.patch.object(source, "_hash_file", wraps=original_hash) as hashed:
            proof = self.capture()
            self.assertEqual(hashed.call_count, 2)
            with mock.patch.object(source.os, "scandir", side_effect=AssertionError("census reread")):
                first = proof.verify(self.context)
                second = proof.verify(self.context)
            self.assertEqual(first, second)
            self.assertEqual(first["files"], 2)
            self.assertEqual(hashed.call_count, 2)
            self.assertIn("no execution", first["claim"])

    def test_saved_proof_requires_original_pin_and_exact_complete_context(self):
        proof = self.capture()
        reference = proof.save(self.root / "proof.json")
        hashed_paths = []
        original_hash = source._hash_file

        def observe(path, *args, **kwargs):
            hashed_paths.append(path)
            return original_hash(path, *args, **kwargs)

        with mock.patch.object(source, "_hash_file", side_effect=observe):
            loaded = source.SourceVerification.load(reference, self.files, self.census,
                                                    self.context, retry=self.retry)
            loaded.verify(self.context)
        self.assertEqual(hashed_paths, [reference["path"]])
        self.assertEqual(loaded.summary()["proof"], reference)
        self.assertLess(len(json.dumps(loaded.summary())), 1024)
        bad = {**reference, "sha256": "0" * 64}
        with self.assertRaises(source.SourceIntegrityError):
            source.SourceVerification.load(bad, self.files, self.census, self.context, retry=self.retry)
        with self.assertRaises(source.SourceIntegrityError):
            source.SourceVerification.load({"path": reference["path"]}, self.files,
                                           self.census, self.context, retry=self.retry)

    def test_every_context_family_changes_the_key_and_refuses_old_proof(self):
        proof = self.capture()
        reference = proof.save(self.root / "proof.json")
        for field in ("source_closure", "dependency_graph", "tools", "configuration", "mode"):
            changed = copy.deepcopy(self.context)
            changed[field] = "different " + field
            with self.assertRaisesRegex(source.SourceIntegrityError, "request differs"):
                source.SourceVerification.load(reference, self.files, self.census, changed,
                                               retry=self.retry)
        changed = copy.deepcopy(self.context)
        changed["mode"] = "race"
        with self.assertRaisesRegex(source.SourceIntegrityError, "context changed"):
            proof.verify(changed)
        with self.assertRaisesRegex(source.SourceIntegrityError, "context changed"):
            proof.verify(self.context)

    def test_changed_source_digest_or_closed_names_cannot_hit_saved_proof(self):
        reference = self.capture().save(self.root / "proof.json")
        files = {**self.files, str(self.first): "0" * 64}
        with self.assertRaisesRegex(source.SourceIntegrityError, "request differs"):
            source.SourceVerification.load(reference, files, self.census, self.context,
                                           retry=self.retry)
        with self.assertRaisesRegex(source.SourceIntegrityError, "request differs"):
            source.SourceVerification.load(reference, self.files, {}, self.context, retry=self.retry)

    def test_same_size_write_with_restored_mtime_is_sticky_integrity(self):
        proof = self.capture()
        before = self.first.stat()
        original = self.first.read_bytes()
        self.first.write_bytes(b"X" * len(original))
        os.utime(self.first, ns=(before.st_atime_ns, before.st_mtime_ns))
        self.assertNotEqual(self.first.stat().st_ctime_ns, before.st_ctime_ns)
        with self.assertRaisesRegex(source.SourceIntegrityError, "files identity changed"):
            proof.verify(self.context)
        self.first.write_bytes(original)
        os.utime(self.first, ns=(before.st_atime_ns, before.st_mtime_ns))
        with self.assertRaisesRegex(source.SourceIntegrityError, "files identity changed"):
            proof.verify(self.context)

    def test_chmod_then_restore_cannot_erase_physical_mutation(self):
        proof = self.capture()
        mode = self.first.stat().st_mode & 0o777
        self.first.chmod(0o400)
        self.first.chmod(mode)
        with self.assertRaises(source.SourceIntegrityError):
            proof.verify(self.context)

    def test_hardlink_count_changes_outside_census_are_detected(self):
        proof = self.capture()
        alias = self.root / "outside-link"
        os.link(self.first, alias)
        with self.assertRaisesRegex(source.SourceIntegrityError, "files identity changed"):
            proof.verify(self.context)
        alias.unlink()
        with self.assertRaises(source.SourceIntegrityError):
            proof.verify(self.context)

    def test_existing_hardlink_custody_detects_writes_through_other_alias(self):
        alias = self.root / "outside-link"
        os.link(self.first, alias)
        proof = self.capture()
        proof.verify(self.context)
        alias.write_bytes(b"X" * self.first.stat().st_size)
        with self.assertRaisesRegex(source.SourceIntegrityError, "files identity changed"):
            proof.verify(self.context)

    def test_new_name_inside_initially_empty_directory_invalidates_census(self):
        proof = self.capture()
        (self.stage / "empty" / "unlisted.go").write_bytes(b"package injected\n")
        with mock.patch.object(source.os, "scandir", side_effect=AssertionError("census reread")):
            with self.assertRaisesRegex(source.SourceIntegrityError, "directories identity changed"):
                proof.verify(self.context)

    def test_deleted_or_replaced_name_is_not_content_equality(self):
        proof = self.capture()
        original = self.first.read_bytes()
        self.first.unlink()
        self.first.write_bytes(original)
        with self.assertRaises(source.SourceIntegrityError):
            proof.verify(self.context)

    def test_removed_nested_empty_directory_invalidates_closed_namespace(self):
        proof = self.capture()
        (self.stage / "empty").rmdir()
        with self.assertRaises(source.SourceIntegrityError):
            proof.verify(self.context)

    def test_ancestor_replacement_refuses_even_with_identical_contents(self):
        proof = self.capture()
        self.stage.rename(self.root / "old-stage")
        self.stage.mkdir()
        (self.stage / "nested").mkdir()
        (self.stage / "empty").mkdir()
        self.first.write_bytes((self.root / "old-stage" / "first.go").read_bytes())
        self.second.write_bytes((self.root / "old-stage" / "nested" / "second.h").read_bytes())
        with self.assertRaises(source.SourceIntegrityError):
            proof.verify(self.context)

    def test_unrelated_output_siblings_do_not_change_source_ancestry(self):
        proof = self.capture()
        (self.root / "logs").mkdir()
        (self.root / "logs" / "phase.stdout").write_bytes(b"ordinary output")
        (self.root / "new-image").write_bytes(b"independently owned image")
        proof.verify(self.context)
        reference = proof.save(self.root / "proof.json")
        self.assertEqual(proof.verify(self.context)["proof"], reference)

    def test_content_inputs_refuse_symlinks_and_special_files(self):
        alias = self.root / "alias.go"
        alias.symlink_to(self.first)
        with self.assertRaisesRegex(source.SourceIntegrityError, "bounded regular data"):
            source.SourceVerification.capture({str(alias): self.files[str(self.first)]}, {},
                                               self.context, retry=self.retry)
        fifo = self.root / "fifo"
        os.mkfifo(fifo)
        with self.assertRaisesRegex(source.SourceIntegrityError, "bounded regular data"):
            source.SourceVerification.capture({str(fifo): "0" * 64}, {},
                                               self.context, retry=self.retry)

    def test_unhashed_namespace_symlink_is_bound_without_following_target(self):
        outside = self.root / "outside"
        outside.write_bytes(b"not a declared content input")
        link = self.stage / "namespace-link"
        link.symlink_to(outside)
        self.census[str(self.stage)] = ["first.go", "namespace-link", "nested/second.h"]
        proof = self.capture()
        outside.write_bytes(b"still not a declared input")
        proof.verify(self.context)
        link.unlink()
        link.symlink_to(self.root / "other")
        with self.assertRaises(source.SourceIntegrityError):
            proof.verify(self.context)

    def test_initial_hash_and_census_mismatches_never_create_proof(self):
        with self.assertRaisesRegex(source.SourceIntegrityError, "SHA256"):
            source.SourceVerification.capture({str(self.first): "0" * 64}, {},
                                               self.context, retry=self.retry)
        self.census[str(self.stage)] = ["first.go"]
        with self.assertRaisesRegex(source.SourceIntegrityError, "filename census differs"):
            self.capture()

    def test_actual_write_during_read_refuses_mixed_generation_proof(self):
        ready, changed = threading.Event(), threading.Event()
        original_read = source.os.read
        original_inode = self.first.stat().st_ino
        failures = []

        def writer():
            try:
                if not ready.wait(5):
                    raise AssertionError("read barrier not reached")
                self.first.write_bytes(b"X" * self.first.stat().st_size)
            except BaseException as error:
                failures.append(error)
            finally:
                changed.set()

        worker = threading.Thread(target=writer)
        worker.start()

        def read(descriptor, count):
            value = original_read(descriptor, count)
            if value and os.fstat(descriptor).st_ino == original_inode and not ready.is_set():
                ready.set()
                if not changed.wait(5):
                    raise AssertionError("mutation barrier not joined")
            return value

        try:
            with mock.patch.object(source.os, "read", side_effect=read):
                with self.assertRaisesRegex(source.SourceIntegrityError, "changed while reading"):
                    self.capture()
        finally:
            ready.set()
            worker.join(5)
        self.assertFalse(worker.is_alive())
        self.assertEqual(failures, [])

    def test_transient_metadata_failure_retries_and_preserves_equality(self):
        proof = self.capture()
        original = source.os.lstat
        attempts = []

        def observe(path):
            if str(path) == str(self.first):
                attempts.append(path)
                if len(attempts) == 1:
                    raise OSError(errno.EIO, "synthetic temporary read")
            return original(path)

        with mock.patch.object(source.os, "lstat", side_effect=observe):
            proof.verify(self.context)
        self.assertEqual(len(attempts), 2)

    def test_exhausted_transient_read_is_unavailable_and_not_a_cached_pass(self):
        proof = self.capture()
        original = source.os.lstat
        failures = []

        def observe(path):
            if str(path) == str(self.first):
                error = OSError(errno.ESTALE, "synthetic transient original")
                failures.append(error)
                raise error
            return original(path)

        with mock.patch.object(source.os, "lstat", side_effect=observe):
            with self.assertRaises(source.SourceUnavailableError) as caught:
                proof.verify(self.context)
        self.assertEqual(len(failures), 3)
        self.assertEqual(caught.exception.__cause__.exceptions, tuple(failures))
        proof.verify(self.context)

    def test_hard_refusal_after_transient_cannot_be_retried_into_equality(self):
        proof = self.capture()
        original = source.os.lstat
        attempts = []
        failures = []

        def observe(path):
            if str(path) == str(self.first):
                attempts.append(path)
                error = OSError(errno.EIO if len(attempts) == 1 else errno.EACCES, "synthetic refusal")
                failures.append(error)
                raise error
            return original(path)

        with mock.patch.object(source.os, "lstat", side_effect=observe):
            with self.assertRaisesRegex(source.SourceIntegrityError, "source read refused") as caught:
                proof.verify(self.context)
        self.assertEqual(len(attempts), 2)
        self.assertEqual(caught.exception.__cause__.exceptions, tuple(failures))
        with self.assertRaises(source.SourceIntegrityError) as repeated:
            proof.verify(self.context)
        self.assertIs(repeated.exception.__cause__, caught.exception)

    def test_cancellation_and_deadline_do_not_publish_equality(self):
        canceled = threading.Event()
        policy = source.ReadPolicy(delay_seconds=0, cancel_event=canceled)
        proof = self.capture(retry=policy)
        canceled.set()
        with self.assertRaisesRegex(source.SourceUnavailableError, "canceled"):
            proof.verify(self.context)
        with mock.patch.object(source.time, "monotonic", side_effect=[0, 301]):
            with self.assertRaisesRegex(source.SourceUnavailableError, "budget exhausted"):
                self.capture()

    def test_mount_identity_change_invalidates_same_inode_content(self):
        proof = self.capture()
        original = source._Mounts.select

        def changed(mounts, path):
            row = original(mounts, path)
            return {**row, "id": row["id"] + 1000000}

        with mock.patch.object(source._Mounts, "select", changed):
            with self.assertRaisesRegex(source.SourceIntegrityError, "mount mapping changed"):
                proof.verify(self.context)

    def test_saved_proof_cannot_omit_file_or_nested_directory_custody(self):
        reference = self.capture().save(self.root / "proof.json")
        original = json.loads(Path(reference["path"]).read_bytes())
        for kind, name in (("files", str(self.first)), ("directories", str(self.stage / "nested"))):
            data = copy.deepcopy(original)
            del data[kind][name]
            path = self.root / (kind + ".json")
            path.write_bytes(source._canonical(data))
            pin = {"path": str(path), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
            with self.assertRaises(source.SourceIntegrityError):
                source.SourceVerification.load(pin, self.files, self.census, self.context,
                                               retry=self.retry)

    def test_proof_is_create_once_and_cannot_be_written_inside_source(self):
        proof = self.capture()
        with self.assertRaisesRegex(source.SourceIntegrityError, "closed source root"):
            proof.save(self.stage / "proof.json")
        reference = proof.save(self.root / "proof.json")
        with self.assertRaises(FileExistsError):
            proof.save(self.root / "proof.json")
        self.assertEqual(hashlib.sha256(Path(reference["path"]).read_bytes()).hexdigest(),
                         reference["sha256"])

    def test_partial_directory_read_retries_from_first_name(self):
        original = source.os.scandir
        attempts = []

        class PartialRead:
            def __init__(self, descriptor):
                self.iterator = original(descriptor)

            def __enter__(self):
                return self

            def __exit__(self, *args):
                self.iterator.close()

            def __iter__(self):
                next(self.iterator)
                raise OSError(errno.EIO, "synthetic partial directory read")

        def entries(descriptor):
            attempts.append(descriptor)
            return PartialRead(descriptor) if len(attempts) == 1 else original(descriptor)

        with mock.patch.object(source.os, "scandir", side_effect=entries):
            proof = self.capture()
        self.assertGreaterEqual(len(attempts), 2)
        proof.verify(self.context)

    def test_descriptor_close_failure_preserves_original_read_failure(self):
        original_close = source.os.close
        original_read = source.os.read
        selected = self.first.stat().st_ino
        read_failure = OSError(errno.EACCES, "synthetic original read refusal")
        close_failure = OSError(errno.EIO, "synthetic close failure")
        closed = []

        def read(descriptor, count):
            if os.fstat(descriptor).st_ino == selected:
                raise read_failure
            return original_read(descriptor, count)

        def close(descriptor):
            is_selected = os.fstat(descriptor).st_ino == selected
            original_close(descriptor)
            if is_selected:
                closed.append(descriptor)
                raise close_failure

        with mock.patch.object(source.os, "read", side_effect=read):
            with mock.patch.object(source.os, "close", side_effect=close):
                with self.assertRaisesRegex(source.SourceIntegrityError, "close refused") as caught:
                    self.capture()
        self.assertEqual(len(closed), 1)
        causes = caught.exception.__cause__.exceptions
        self.assertIs(causes[0].__cause__, read_failure)
        self.assertIs(causes[1], close_failure)

    def test_unsupported_filesystem_never_reuses_metadata_as_content(self):
        proof = self.capture()
        original = source._Mounts.__init__

        def replace(mounts, owner):
            original(mounts, owner)
            for row in mounts.rows:
                row["filesystem"] = "overlay"

        with mock.patch.object(source._Mounts, "__init__", replace):
            with self.assertRaisesRegex(source.SourceIntegrityError, "trusted local filesystem"):
                proof.verify(self.context)

    def test_unadmitted_trust_profile_cannot_capture_or_load_reuse(self):
        reference = self.capture().save(self.root / "proof.json")
        context = copy.deepcopy(self.context)
        del context["trust_profile"]
        with self.assertRaisesRegex(source.SourceIntegrityError, "exclusive source-owner profile"):
            source.SourceVerification.capture(self.files, self.census, context, retry=self.retry)
        with self.assertRaisesRegex(source.SourceIntegrityError, "exclusive source-owner profile"):
            source.SourceVerification.load(reference, self.files, self.census, context,
                                           retry=self.retry)

    def test_directory_entry_count_and_bytes_stop_before_iterator_overrun(self):
        for kind, limit, names, expected_calls, expected_error in (
                ("MAXIMUM_DIRECTORY_ENTRIES", 2, ["one", "two", "three"], 3,
                 "closed directory entry count exceeds bound"),
                ("MAXIMUM_CENSUS_BYTES", 2, ["three"], 1,
                 "closed directory name bytes exceed bound")):
            calls = []

            class Entries:
                def __enter__(self):
                    return self

                def __exit__(self, *args):
                    pass

                def __iter__(self):
                    return self

                def __next__(self):
                    if len(calls) == len(names):
                        raise AssertionError("enumeration exceeded admitted bound")
                    entry = mock.Mock()
                    entry.name = names[len(calls)]
                    calls.append(entry.name)
                    return entry

            with mock.patch.object(source, kind, limit):
                with mock.patch.object(source.os, "scandir", return_value=Entries()):
                    with self.assertRaises(source.SourceIntegrityError) as caught:
                        self.capture()
            self.assertEqual(str(caught.exception), expected_error)
            self.assertEqual(len(calls), expected_calls)

    def test_directory_enumeration_observes_owner_cancellation(self):
        canceled = threading.Event()
        calls = []

        class Entries:
            def __enter__(self):
                return self

            def __exit__(self, *args):
                pass

            def __iter__(self):
                return self

            def __next__(self):
                if len(calls) == 2:
                    raise AssertionError("enumeration continued after cancellation")
                entry = mock.Mock()
                entry.name = "synthetic-name"
                calls.append(entry.name)
                if len(calls) == 2:
                    canceled.set()
                return entry

        policy = source.ReadPolicy(delay_seconds=0, cancel_event=canceled)
        with mock.patch.object(source.os, "scandir", return_value=Entries()):
            with self.assertRaisesRegex(source.SourceUnavailableError, "canceled"):
                self.capture(retry=policy)
        self.assertEqual(len(calls), 2)

    def test_recursive_filename_append_is_bounded_before_census_comparison(self):
        (self.stage / "third.go").write_bytes(b"unexpected source")
        with mock.patch.object(source, "MAXIMUM_CENSUS_LEAVES", 2):
            with self.assertRaisesRegex(source.SourceIntegrityError, "leaf count exceeds bound"):
                self.capture()
        (self.stage / "third.go").unlink()
        self.first.unlink()
        (self.stage / "empty").rmdir()
        self.files = {str(self.second): self.files[str(self.second)]}
        self.census = {str(self.stage): ["nested/second.h"]}
        with mock.patch.object(source, "MAXIMUM_CENSUS_BYTES", 10):
            with self.assertRaisesRegex(source.SourceIntegrityError, "filename bytes exceed bound"):
                self.capture()

    def test_unrelated_mount_stacks_preserve_capture_saved_proof_and_reuse(self):
        original = source._mountinfo(self.retry.owner())
        rows = []
        for index, point in enumerate((self.root / "unused-mount", Path(str(self.stage) + "-neighbor"))):
            first = 990000001 + index * 2
            rows.extend((f"{first} 1 0:991 / {point} rw - autofs synthetic-source rw\n",
                         f"{first + 1} {first} 0:992 / {point} rw - tmpfs synthetic-source rw\n"))
        with mock.patch.object(source, "_mountinfo", return_value=original + "".join(rows).encode()):
            proof = self.capture()
            reference = proof.save(self.root / "proof.json")
            loaded = source.SourceVerification.load(reference, self.files, self.census,
                                                    self.context, retry=self.retry)
            self.assertEqual(loaded.verify(self.context)["proof"], reference)
        loaded.verify(self.context)

    def test_selected_mount_stack_refuses_capture_for_source_and_ancestor(self):
        original = source._mountinfo(self.retry.owner())
        for point in (self.stage, self.root):
            added = (f"990000001 1 0:991 / {point} rw - ext4 synthetic-source rw\n"
                     f"990000002 990000001 0:992 / {point} rw - ext4 synthetic-source rw\n")
            with mock.patch.object(source, "_mountinfo", return_value=original + added.encode()):
                with self.assertRaisesRegex(source.SourceIntegrityError, "ambiguous selected"):
                    self.capture()

    def test_selected_mount_stack_invalidates_reuse_without_rebaseline(self):
        proof = self.capture()
        original = source._mountinfo(self.retry.owner())
        added = (f"990000001 1 0:991 / {self.stage} rw - ext4 synthetic-source rw\n"
                 f"990000002 990000001 0:992 / {self.stage} rw - ext4 synthetic-source rw\n")
        with mock.patch.object(source, "_mountinfo", return_value=original + added.encode()):
            with self.assertRaisesRegex(source.SourceIntegrityError, "ambiguous selected") as first:
                proof.verify(self.context)
        with self.assertRaisesRegex(source.SourceIntegrityError, "ambiguous selected") as repeated:
            proof.verify(self.context)
        self.assertIs(repeated.exception.__cause__, first.exception)


if __name__ == "__main__":
    unittest.main()
