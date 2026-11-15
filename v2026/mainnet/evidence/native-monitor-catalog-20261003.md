# Native monitor catalog revisions

This candidate extends the separate native archive candidate. Behavioral
qualification and current-source integration are pending. It does not activate
a monitor, authorize a chain transaction, or prove delivered alerts.

An optional `history_catalog` field in the original native services policy pins
the independent operational Ed25519 approval key, original review digest and
initial resource capacities. These fields are part of the original policy hash.
Existing checkpoints without that policy keep the original 128-segment profile;
a revision request cannot enroll a key or replace an existing key.

`monitor-native-catalog plan --request FILE --request-sha256 HASH` exports the
exact domain-separated public signing bytes. Planning requires the reviewed
durable declaration, exact checkpoint bytes, the stopped-and-joined writer
fence, and shared leases over the checkpoint and every immutable archive.
The command has no signer or private-key input.

Revision payloads are bounded at 16 KiB; the complete approval frame, including
its schema, signature and final newline, is bounded at 17 KiB. Preview checks the
complete importable frame before exposing signing bytes. A synthetic protected
long-path control crosses the old payload-only import bound through the actual
public preview, signing-frame export and approval importer.

`monitor-native-catalog apply --plan FILE --plan-sha256 HASH --approval FILE
--approval-sha256 HASH` verifies the independently produced signature. Adoption
takes only the affected checkpoint's exclusive lease. Every previous resource
approval and archive reference remains retained. Original cursor, pending range,
batch chain, totals, runtime reviews, incidents and timestamps remain unchanged.
Other roles own independent leases and continue. The plan never grants restart
authority; the existing reviewed service configuration governs startup.

The initial revision profile is deliberately finite: at most 512 segments,
128 KiB of serialized catalog metadata, 512 held guarded segment readers and
16 retained capacity approvals. Segment payloads and the active checkpoint
remain at most 1 MiB each. Each reader can retain multiple physical descriptors;
the deployment must provision its descriptor limit separately. Descriptor
exhaustion remains unavailable custody, never permission to omit archives.

Every capacity dimension grows monotonically. A two-times forecast covers the
retained and future segment count, metadata bytes, physical bytes and inodes.
Metadata forecasting includes worst-case JSON escaping of the already bounded
paths. The unchanged checkpoint envelope must still fit the accepted active
event history, reviewed runtime catalog and retention metadata. Count growth
alone cannot make an oversized checkpoint admissible. Metrics warn before the
remaining segment, metadata, reader or approval capacity is exhausted.

After publication uncertainty the old owner joins and closes. A repeated exact
approval can authenticate and reconcile only the exact old checkpoint or its
one approved next checkpoint. A completed repeat does not publish again. Later
progress or another approval is never replaced by an older request. Short
stdout is a lost report, not a reset of the completed revision.

The controls exercise the public dispatcher and real retained snapshot owners:
signed growth followed by original pending-range continuation, lost directory
sync acknowledgment, complete replay and short output, foreign key/signature
and predecessor refusal, absence of a legacy enrollment path, separate logical
and physical forecast bounds, active-writer and cancellation refusal, and
retention of all approvals across a second capacity revision.

This profile is not an indefinite-retention claim. Growth beyond its explicit
limits needs a separately reviewed successor that keeps the original archives
and approval lineage. Claim and EVM archive adapters, whole-archive backup and
restore, current composed qualification, approved activation, ingestion and
delivered-alert evidence remain separate requirements.
