# Native observer history rollover candidate

The new `monitor-native-archive plan` and `apply` commands move acknowledged
history out of the active checkpoint while retaining every original checkpoint
byte. They need the exact durable declaration, public request/plan digest and
protected stopped-and-joined writer fence. Only the affected native observer is
quiesced. The public commands do not sign, contact a chain or activate a service.

Both original and archive owners must already be provisioned. The existing
`storage-prepare` fixed kind `mainnet-monitor-checkpoint` supplies the archive's
private lock and explicit fresh-absent head (1 MiB, basename at most 155 bytes).
Rollover never creates a missing marker or infers fresh authority from absence.
Paths have a separate 1024-byte bound. A plan pins the original bytes, policy,
declaration and exact derived next checkpoint; `restart_authorized` stays false.

Apply acquires both nonblocking exclusive file leases. It publishes and verifies
the original archive before publishing the compact active checkpoint. A failed
publication closes the affected owners; a repeated exact plan may reconcile the
shared snapshot primitive's authenticated pending state once after join. It
accepts only exact original/derived heads, never unknown progress. A completed
repeat does not republish either head. All older archive references remain.

The compact checkpoint preserves cursor, batch count/hash, pending high-water,
cumulative alpha/fees, timestamps and retained runtime reviews. Reopen decodes
one archive payload at a time and authenticates the complete predecessor chain
against the compact summary. Shared leases remain held; unchanged samples check
named physical custody and checkpoint attributes without rereading old payloads.
Deletion or changed identity stops only this observer. Other roles continue.
The new optional archive field leaves historical checkpoint bytes unchanged
when absent.

This first executable profile has 128 segments of at most 1 MiB each. The plan
requires an explicit positive future-segment forecast and two-times margins
for retained plus forecast segment count, bytes and inodes. The declaration
must carry the byte/inode reserve floors for both target owners. Metrics expose
segment count, capacity and an early warning; the operator must revise capacity
before the retained-plus-future margin is exhausted. There is no deletion or
automatic forgetting at the bound.

Source controls cover actual public continuation of the original pending
range, two successive archives, lost acknowledgments after each real directory
sync, repeat apply, active-owner/missing-declaration/tamper/cancellation refusal,
affected-role custody loss with healthy-peer continuation, omitted/forged prefix
rejection, and forecast refusal before publication. A Linux inotify control
observes actual payload access at admission and no payload access during
unchanged checks or confirmed immutable-member refusal. Behavioral execution is
delegated to Sol; this document alone makes no qualification claim.

Still required: signed archive catalog-capacity revision/adoption, claim and EVM
archive adapters preserving their own unresolved records and review histories,
current composed-role qualification, backup/restore of the entire archive
union, actual deployed ingestion and alert delivery. The finite profile is not
an indefinite-operation or PH/MG closure claim.
