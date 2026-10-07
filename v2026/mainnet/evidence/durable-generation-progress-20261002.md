# Durable generation and read-admission checkpoint

MG-09 / PH-09 remains in progress. Existing `cb2e3ffe`, `c339095c`, `8f459b14`
and earlier receipts keep their exact source and test scopes. Their 35-root
qualification does not prove cross-restart physical-root continuity or qualify
the pending production-owner adoption. No production deployment, automatic
recovery, restore rebind, signing or service start is authorized here.

## Confirmed gaps and causal scope

The old declaration authenticates the filesystem and an external lease but
does not bind the state-root inode. After closing an owner, replacing its root
with an empty directory on the same filesystem can therefore look like fresh
custody. Exact `cb2e3ffe` plus test-only controls fails five selected roots in
both normal and race modes: daemon and owner-local empty-root reopening, read
admission below the write reserve, canceled expected-inventory admission, and
nil-context admission. Read API aliases in this old-source overlay only call
the previous checks; they add no fix.

Named-file observation had a related classification error: unavailable
`EIO`/`EMFILE` results could acquire `ErrIdentity`, permanently poisoning the
owner despite no successfully observed replacement. A preserved intermediate
package with instance-local observation-failure seams reproduces this in two
normal/race controls; the closed borrowed-descriptor positive control passes.
This is an intermediate-source causal control, not an unchanged-`cb2e3ffe`
rerun. The exact overlay and file hashes are retained in the author receipt.

The native-journal adopter remains unqualified. Its first implementation uses
write admission for reads; an unchanged full/read-only volume therefore blocks
retained inspection. Review also requires actual named-leaf inode checks before
and after reads, appends and sync, and retention of ambiguous append/rename
failures. Directory identity alone cannot acknowledge a detached file. A
temporary refusal before mutation must remain recoverable on the same owner;
uncertain publication must stop further mutation pending joined reconciliation
of the original bytes. These consumer corrections are pending source work.

Further adopter review confirmed that an in-memory leaf census does not retain
native journal membership after close/reopen: missing log/raw members must not
become an empty journal, even when the original root inode and nonce survive.
Request cancellation also needs checking at the actual native-journal operation
boundary. The shared author is implementing a mandatory preprovisioned journal
anchor with a completed-member census and pending-write protocol. This is still
unqualified adopter work; Connect's 48-root physical-directory receipt does not
prove leaf survival or close these two consumer gaps.

## Separate author-qualified primitive successor

Connect `6cd720cf50a43530c298ed8f931e341b00e8913c`, tree
`010533a40c31403c14a810828fee3c47e324c143`, parent `cb2e3ffe`, passes
48 normal roots, 48 race roots and vet. The [author receipt](durable-generation-author-20261002.json)
is copied byte-for-byte from
`/mnt/data/sn-testnet/mainnet-durable-volume-20261002/evidence/guard-generation-receipt.json`:
SHA-256 `7a45df12511cf760cb307ed5a771741f9c99df3ed53851e7b0e61023ac037e37`.
It binds the exact clean source, compiler bytes, terminal logs and causal source
overlays. Raw logs and source workspaces remain external at the recorded paths.
Independent verification was pending at the author seal. The separate terminal
readback below completes that primitive scope; it does not qualify full Connect
or downstream consumers.

Both external declaration schemas advance to v2 and reject v1 or missing
generation authority. Each precreated root requires an externally declared
inode and digest of exactly 32 bytes in the fixed
`user.urnetwork.durable-root-generation` xattr. Filesystem UUID, retained
descriptor and no-follow ancestry remain checked. Admission reads the xattr
through the descriptor and never provisions or enrolls it. The inode refuses a
copied root; the nonce refuses a fresh generation even with the same inode
number. The deterministic reuse control changes/misses the nonce on the same
inode; it does not force the filesystem allocator to recycle a number.

Missing/malformed/different nonce metadata is observed identity loss. Injected
unsupported-xattr and transient observation errors refuse admission without a
weaker fallback or inferred identity change. Original signed protocol records
are unchanged. Identity-only `CheckRead`/`CheckReadDirectory` permits inspection
under write pressure while mutation remains refused. Nil/canceled inventory
admission precedes external evidence open/read/hash, with cancellation between
finite chunks.

Inventory v2 retains the required nonce metadata. Ordinary verification still
requires the same declaration. Separately named `VerifyReboundInventory`
compares exact original local bytes and metadata with a root admitted by an
explicitly supplied reviewed target declaration; it reports original and target
identity and always keeps restart authorization false. Copied nonce bytes do
not grant rebind authority. This local comparison proves no cross-host,
PostgreSQL, Redis or remote MinIO restoration.

## Remaining owner and operational work

Miner fleet/claim, monitor, validator, bootstrap/root and explicit server local
blob owners still need composed qualification at their actual public stateful
entry points, with policy required before custody creation or host effects.
Stateless inspection stays portable. Daemons cannot infer the owner-local
system-filesystem exception. Reserve/busy refusal should pause only affected
work; uncertain publication retains original pending/completed bytes, and any
same-approved-root recovery must join the old owner before reopening that root.
Bounded root recovery is still pending implementation/qualification, not an
automatic reset or whole-campaign restart.

Deployment declarations and unit plumbing, exact module/source composition,
independent consumer tests, capacity policy and production backup/restore
rehearsal remain open. The frozen `258e25b4` / server `0aa1e244` artifacts and
all ten blocked unsigned actions remain unchanged.

Offline provisioning is also an implementation gap. Operators need an explicit
preparation command that creates reviewed private roots, external leases, root
nonce metadata and the final native-journal anchor, then emits exact declarations
and a custody manifest for review. Runtime constructors must never enroll absent
roots or journals, invent replacement anchors, or silently clean old state.
There is currently no qualified production preparation command; test fixture
provisioning is not an operator workflow. Its schema must follow the final
journal-anchor contract before consumer release qualification.

## Independent primitive addendum

The [independent v2 receipt](durable-generation-independent-20261002.json)
retains SHA-256
`3c134a458a542b4e423b2d0d7119462aba14af583e5ea707becd56fe7b0e07e4`,
copied byte-for-byte from
`/mnt/data/sn-testnet/sol-connect-durable-core-independent-20261002/v2-receipt.json`.
It verifies the exact physical clean `6cd720cf` / `010533a4` source, 48 normal
and 48 race roots, vet exit 0, five exact-`cb2e3ffe` causal failures per mode,
and the hash-verified intermediate observation overlay's two intended failures
plus one caller-error positive per mode. Raw logs remain external at the
receipt's retained paths. This is independent scoped primitive testing, not an
independent compiler build or a consumer/release/deployment qualification.

Root review checked source/receipt bindings and current origin ancestry before
merging this exact source into Connect main. Original receipts remain unchanged.
Native-journal, fleet/claim, monitor, validator/bootstrap/root and local-blob
adoption, uncertain publication handling, bounded root recovery and operational
backup/restore evidence remain separate unfinished work.

## Subsequent owner-custody checkpoint

The [separate native/snapshot/miner and monitor checkpoint](durable-owner-custody-qualification-20261002.md) now advances the owner work described above. Native `695f6683`, snapshot `a5c765c4` and miner `b3c3d66` have scoped author and independent receipts; monitor `f1b445f9` has an author receipt. Separate Connect `0a5cda0e` adds owner-attribute inventory-v3 under its own author scope. These candidate sources do not enlarge the original `6cd720cf` receipt or qualify a composed release. Offline preparation, independent remaining adopters, deployment declarations, capacity/rotation and actual restore remain open.
