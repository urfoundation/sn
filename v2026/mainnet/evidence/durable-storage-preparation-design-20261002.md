# Offline storage preparation boundary

This is the proposed next implementation boundary. It does not qualify a
provisioner or authorize applying a plan. Existing runtime source and receipts
remain immutable. The target interface must be tested before it is frozen.

## Authority and placement

The base volume/root preparer belongs in the stdlib-only Connect durablevolume
peer. Connect's parent package must not import that peer. Runtime Open and
OpenOwnerLocal retain their existing refusal of unenrolled roots. Preparation
uses an explicitly different request schema and entry point; it is never a
constructor fallback. Daemon and owner-local commands select separate policy
scopes before reading their declaration, including the system-device exception.

The SN peer internal/durableprepare coordinates fixed owner adapters. Its public
mainnet dispatcher accepts only a bounded registry of known owner kinds and
their exact public protocol inputs. Validator, native journal and snapshot
adapters may inspect borrowed descriptors, but never call a runtime constructor
that signs, creates default history or starts a service. Server consumes the
resulting Connect volume declaration without importing SN. DB/MinIO restore is
not inferred from local blob or file preparation.

## Reviewable plan

A request binds the exact filesystem UUID/type/mount, reserve floors, protected
metadata paths, target root names, and an external stopped-and-joined former
writer assertion. It explicitly distinguishes fresh namespace preparation,
retained custody enrollment, and restoration; there is no generic empty-root
adoption. Missing old custody cannot be reclassified as fresh.

Plan construction does not mutate a target or retained custody. It may create
an owned offline staging bundle and a report in the caller-selected private
output directory. All generated nonce bytes and proposed marker/lease bytes,
source input hashes, prepared file bytes, bounded namespace census, and observed
target/parent identities are part of the exact plan hash accepted by apply.
The plan records the literal authority limits of its external former-writer
assertion; a local lease cannot prove legacy writers were joined.

Fresh LevelDB initialization occurs only in this owned staging bundle. The
ledger adapter takes public identity/coordinator/limits, no private key. It
closes and syncs that backend, verifies the empty canonical identity/head, and
returns a bounded exact file census. Apply copies those reviewed bytes; it does
not ask LevelDB to invent an unreviewed target layout during a partially
completed apply. Retained ledger inspection uses read-only/ErrorIfMissing and
verifies actual signatures, prefix and immutable import/ready witnesses.

## Plan-owned apply stages

The proposed opaque stage owns an independently retained context, physical
directory/metadata descriptors, and the separate per-root preparation lease.
It is concurrency safe; admission and close protect descriptor lifetime.
Acquisition is nonblocking. A different plan/active writer returns typed busy,
and unavailable observations or reserve pressure refuse the current operation
without claiming identity loss. Identity mismatches remain sticky.

The caller supplies a bounded plan-owned stage specification containing:

```go
type OwnerStageSpec struct {
    Kind string
    RelativePath string
    Mode PreparationMode // fresh, retained, or explicit restored target
    PublicInputsSha256 string
    ReviewedCensusSha256 string
    PreparedFiles []PreparedFile // exact relative name, mode, size and hash
    MaximumEntries uint64
    MaximumBytes uint64
}
```

These are proposed wire fields, not permission to accept arbitrary kinds at the
CLI. Known adapters produce them from the fixed registry and parsed public
inputs. Mainnet package-private adapters retain each original marker grammar.

Before any target effect, the common apply control reserves that exact
namespace, plan hash and expected initial census and fsyncs the reservation.
Existing unrelated files are never removed or enrolled. All copies use
descriptor-relative, protected, no-replace publication with bounded reads,
explicit context checks, file sync, parent sync, and named-inode postchecks.
Created objects are marked/recorded as belonging to the particular plan before
being acknowledged. Runtime custody anchors use XATTR_CREATE, not replacement
of an existing authority. The final owner checkpoint binds actual retained
target inodes only after all reviewed files and semantic owner checks pass.

A prepared owner adapter receives borrowed physical descriptors and an opaque
stage guard. The narrow operations will be equivalent to:

```go
stage.Check(ctx) error
stage.Directory() *os.File // borrowed; no close/unlock authority
stage.PublishPrepared(ctx, fileSpec) error
stage.CreateCustodyAttribute(ctx, name, exactBytes) error
stage.Complete(ctx, semanticCensus) (OwnerPreparationResult, error)
```

The shared stage independently validates actual file bytes and approved
attribute names/bounds. A caller-supplied digest or success boolean alone is
not a completion witness. The adapter validates protocol semantics and produces
the exact checkpoint bytes; shared preparation validates physical custody and
publication. Neither can silently reconstruct missing signed history.

The base v2 volume declaration is published only after its marker, root
generation, lease and each requested owner stage pass exact readback. The
result records restart_authorized=false; applying a storage plan cannot imply
chain, signer, service-manager, activation or deployment approval.

## Crash continuation and tests

After the former preparation owner joins, the same accepted plan may reopen
only its exact recorded pending/completed stages. A fully synced and exactly
observed publication with a lost acknowledgement can be acknowledged and
continued. Missing prior completed members, foreign/unknown temporaries,
partial unowned files, changed inputs, or a different plan stay refused. There
is no cleanup/reset of the campaign and no signing or rebroadcast. An invalid
owner blocks only that root; independent roots remain usable.

Required causal controls include missing/read-only/full/replaced volume,
replaced root after restart, canceled plan/apply before effects, wrong accepted
plan hash, unknown existing custody, active former writer, and forced child
exit before/after every durable stage boundary. Tests must distinguish
prepublication refusal from uncertain publication, prove exact lost-ack resume,
retain original signed bytes, and preserve unrelated roots. An all-valid prior
state rollback is not detected merely by local xattrs/control files; no
antirollback or cross-host database restore claim follows from this interface.
