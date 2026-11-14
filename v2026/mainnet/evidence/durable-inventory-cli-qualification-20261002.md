# Bounded custody inventory commands

MG-09 / PH-09 remains in progress. This isolated SN candidate makes the new
inventory-v3 primitive usable through the actual public command dispatcher. It
does not compose or deploy a production release, prepare storage, or authorize
writer restart. Earlier source and frozen release receipts remain unchanged;
all ten unsigned actions stay blocked.

## Exact source and qualification

SN `7fb6b6a1bd52cf243fcbd717f0f48a9cf86027ab`, tree
`ba8e0f0de97b3b6f49c4e3f87ad39e592a64ddc2`, is based on monitor source
`f1b445f9`. Its module declaration selects exact Connect
`0a5cda0ebe78f6200c4cff6fd3d1aa172a2b172c` with pseudo-version
`v0.0.0-20261002121440-0a5cda0ebe78`. The source change consists of the inventory
command/dispatcher, additive owner-local root opener, three public-command tests,
and the two module files. No existing signed protocol bytes change.

The [byte-identical author receipt](durable-inventory-cli-author-20261002.json)
has SHA-256
`ddab1e139aa5fcb62b01a58e66af4a21d2bf0f2fbea3d6de0f2227f1d38936ef`.
All nine selected `TestStorageInspection*` roots pass normally and under race;
vet passes for mainnet, internal/durablepath and internal/durablefixture. This
is not a new full monitor, miner, validator, server, Connect or release run.
Independent primitive and CLI checking are queued as separate scopes.

The dependency zip and module h1 values were constructed from exact retained Git
source through `golang.org/x/mod/zip.CreateFromVCS` and read through a private
file proxy. The receipt binds those archive bytes, module declarations and the
compiler. This establishes neither remote module publication/availability,
sumdb corroboration nor independent compiler provenance. Initial helper module
resolution and linked-worktree VCS refusals are recorded separately; no runtime
source was changed to bypass them.

## Public behavior and causal boundaries

`storage-inventory` and `storage-verify` retain strict daemon-volume policy.
They pass finite `--max-owner-attributes` and `--max-owner-attribute-bytes`
values to inventory-v3: defaults 1,000 and 4 MiB; hard ceilings 10,000 and
16 MiB. Known owner attributes are retained as exact opaque bytes and hashes.
Unknown custody metadata prevents a complete report, and missing retained owner
metadata fails verification even when the ordinary file bytes are unchanged.

`storage-owner-inventory` and `storage-owner-verify` deliberately select only the
owner-local policy schema. Each command family rejects the other's declaration;
there is no automatic system-filesystem fallback. Missing/canceled context and
declaration authority, active writers, exhausted traversal bounds and short
stdout writes remain refusals. Tests use real files/xattrs and instance-owned
synthetic kernel facts, not live mounts or owner devices.

Ordinary verification requires the original root/declaration. The verifier's
explicit `--compare-reviewed-rebound` option permits a comparison against the
separately supplied target declaration. It does not perform a rebind. The test
retains copied original owner metadata while changing the root inode and nonce;
the resulting report records the changed physical/declaration/generation
identity and keeps both restart flags false. Opaque copied anchors may still
bind old inodes and be correctly refused by the runtime writer.

Test-only SN `d6e72e85` adds the three new roots to unchanged `f1b445f9`
production commands while selecting the new `0a5cda0e` API. All three fail in
normal and race runs. The original command omits the two newly required metadata
limits and has no explicit owner-local dispatch. The rebound test also stops
at the first missing-limits boundary on that baseline; its later explicit
comparison is a positive feature control, not an independently isolated old
rebound defect. These composition controls do not retroactively change the
already qualified `f1b445f9`/`6cd720cf` scope.

The first fixed normal test invocation passed all nine roots and exited zero.
Its wrapper incorrectly expected ten roots and then stopped. That wrapper
failure and raw normal output are retained. With source unchanged, continuation
ran only race and vet; the final sealer reparsed and validated the exact nine
normal roots. Passing source checks were not relabeled or needlessly rerun.

## Remaining production work

The [offline preparation proposal](durable-storage-preparation-design-20261002.md)
is a design boundary, not qualified implementation. It separates exact staged
fresh bytes from target mutation, fixes the owner-kind registry, reserves a
plan-owned namespace before effects, and calls for bounded no-replace and
XATTR_CREATE publication with exact lost-ack continuation after join. Retained
inspection must verify original protocol semantics; fresh preparation cannot
reclassify missing prior custody. Its proposed stage API is not yet frozen.

Production root/lease/nonce/owner-anchor preparation, semantic restore/rebind,
DB/MinIO recovery, declaration deployment assets, source composition, capacity
forecasts/rotation and actual host/device rehearsal remain open. File reports
cannot close those gates or grant deployment, signing or restart authority.

During this scope a further [narrow cache reclaim](durable-bootstrap-finality-cache-reclaim-20261002.json)
retained SHA-256
`de284d09c55695fed48deb1bfa5725b9f823a2d7e1555b31d0359243272e27d7`.
It removed only two completed `28ebfced`/`ac86855d` release Go build caches after
a privileged 381-process reference census and manifest-exclusion check. Roughly
4.36 GB of reproducible cache was reclaimed; source, module caches, OCI bytes,
logs and receipts were unchanged. This preserves qualification headroom and
does not establish production storage capacity or restore readiness.
