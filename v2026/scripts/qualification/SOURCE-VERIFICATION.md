# Complete source proof reuse

`source_verification.py` avoids repeating the content reads and recursive
filename censuses for a fixed qualification owner. It grants source custody
only. Test results, compiler results, executable custody, actual waits, logs,
resource admission, and per-phase tool checks keep their existing owners.

Build `files` from the complete admitted closure, including embedded files,
nested C inputs, modules, selected generated cache files, and the exact tool and
configuration file pins. Pass the unchanged closed directory inventories.
`context` must contain `source_closure`, `dependency_graph`, `tools`,
`configuration`, and `mode`; include the full admitted environment, cwd,
package/phase selection, options, and configuration in those fields. It is an
identity, not a way to infer missing dependency coverage.
The recipe must also explicitly admit
`trust_profile: trusted-local-exclusive-source-owner-v1`; absence refuses reuse.

```python
proof = SourceVerification.capture(files, closure['stage_file_names'], context)
proof_pin = proof.save(evidence / 'source-proof.json')
for phase in phases:
    proof.verify(context)
    # Existing admitted child, image, actual-wait, and output custody.
    run_phase(phase)
    proof.verify(context)
```

Retain only `proof.summary()` in bounded terminal receipts. The potentially
large proof is a separate file. A later owner may call
`SourceVerification.load(proof_pin, files, inventories, context)` only when its
recipe independently authenticates that exact proof pin. An unpinned file or a
matching executable digest is never a cache hit. A different dependency,
configuration, source pin, or mode is a different request.

Capture checks every content hash, exact filename census, file identity, and
traversed directory. Later verification observes those physical identities and
mount mappings without reading global source contents or rescanning names.
Any selected file or directory change invalidates the proof, including ctime,
mtime, hardlink count, ownership, mode, size, type, or inode. Ancestor-only
directories bind path mapping and ownership while permitting unrelated output
siblings. Empty directories inside a closed root remain part of its namespace
guard. Selected symlink content inputs are refused; namespace-only symlinks are
not followed.

Reuse requires a trusted Linux kernel and an ext4, XFS, Btrfs, or tmpfs mount
with trustworthy inode and timestamp observations, without privileged rollback.
The qualification source owner must exclusively control source publication and
ancestor rebinding throughout the proof lifetime. Ordinary output siblings may
change, but hostile same-UID rename-away/restore of an entire ancestor between
checks is outside this trust profile; plain metadata cannot prove isolation
against that operation while permitting output siblings. Use complete checks
or a stronger immutable mount boundary if this precondition is not admitted.
Complete hardlink staging before capture: adding an alias afterward changes the
retained link count and invalidates the source proof even if bytes stay equal.
Both kernel mount identities and `fstatfs` types are checked. Read-only modes do
not establish this trust: the current stages contain owner-writable files and
retained hardlinks. Other filesystem profiles must retain complete verification
until a suitable custody mechanism is implemented and reviewed.

A proved mismatch is sticky for that proof object. Do not catch it and silently
capture a new baseline. Missing/type/refusal errors are integrity failures.
Bounded transient filesystem reads retain their original causes and retry;
exhaustion or cancellation returns unavailable, never equality. A fresh read
owner may retry unavailable observations while the original proof remains
unchanged. Source-proof creation and publication have their own create-once
destination and never modify a closed source root.
