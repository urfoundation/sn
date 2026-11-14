# Validator head summary v2

Version 2 preserves the global fleet result of [v1](head-summary-v1.md) and
authenticates the exact measurement cut needed to reproduce its head evidence.
It uses schema `urnetwork-validator-head-summary-v2` and signing domain
`urnetwork/validator/head-summary/v2`. The endpoint, byte limit, result fields,
canonical encoding and sr25519 signature rules are otherwise unchanged.

Deploy an operator with the v2 decoder before upgrading its validator. The
reference decoder continues accepting v1 with byte-identical encoding and
signatures. A v1-only operator rejects v2; the existing bounded, best-effort
uploader records the failure without delaying steering. There is no automatic
downgrade that could silently remove exact evidence.

## Signed evidence

These members appear immediately after `measurement_artifact_hash`, in order:

| Member | Type | Meaning |
|---|---|---|
| `previous_measurement_artifact_hash` | string, omitted when empty | The previous durable steering intent's `sha256:` measurement hash. Empty identifies the first intent; a numeric native-epoch gap does not imply empty folds. |
| `evidence_empty_prior` | bool, omitted when false | True only for a first intent whose complete EMA transcript contains no prior state. A nonempty predecessor forbids this claim. Absence does not authorize an empty-state bootstrap. |
| `evidence_recipient` | u64 | The nonzero operator ID to which this private envelope is addressed. |
| `evidence_operators` | array of u64 | The complete, nonempty operator census for the global head result, strictly increasing, at most 4096. |
| `evidence_cuts` | array | Exactly the recipient's one cut, described below. |
| `evidence_policy` | policy object | The public canonical policy used for the decision. Its hash, policy ID and maximum head fleets must equal the envelope's signed values. |

Each cut has these members in order:

| Member | Type | Meaning |
|---|---|---|
| `no_id` | u64 | Equals `evidence_recipient`, which must belong to `evidence_operators`. |
| `native_block` | u64 | The native block at which this operator's measurement cut was taken, nonzero and at or before `native_snapshot_block`. |
| `native_block_hash` | h256 | That cut's nonzero native block hash. |
| `content_hash` | h256 | SHA-256 of the exact published canonical `AttemptCutV2` header, including its trailing newline. |
| `bindings` | array | The complete eligible client census at this intent's pinned decision, strictly increasing by client ID. Explicit `[]` is valid; `null` or omission is invalid. |

Each binding has `client_id`, `fleet_id`, `hotkey`, `generation`, `uid` in that
order. The client ID is a nonzero canonical lowercase UUID; fleet ID and hotkey
are nonzero h256 values; generation is nonzero; UID zero is valid. At most 65536
bindings are allowed. Eligibility is the same observation check used by the
validator's head fold: the coordinator binding must be active, its generation
and UID current, and its client key equal to the measured client's observed
key. The keys and their private observation inputs are not included. Live
eligible identities with no completed hop remain in this census.

The summary signs these fields along with the global result. It does not
publish the private measurement artifact. `measurement_artifact_hash` remains
an opaque local artifact reference, not a retrievable attempt object.

## Privacy and replay

The validator constructs and signs a separate envelope for each configured
operator. An operator receives only its own cut and eligible client census;
global fleet results, policy and the operator ID census are shared. The
authenticated ingest and private stored body must not be exposed by a public
leaderboard response, diagnostics or telemetry.

Fetch the cut using the configured operator's existing public
`/sn/attempt-artifact?kind=metadata&hash=<content_hash>` route. Verify its exact
content hash before trusting its context; the summary's accepted validator
hotkey attests that context. Also match the cut's operator, validator hotkey,
netuid, deployment and policy to independently configured authority. Replay the
complete referenced record/proof streams with finite caller-owned bounds and
trusted server signing keys. The head interval is the cut's authenticated
`egress_first_sequence` through its terminal sequence. Neither `signed_at` nor
operator receipt time defines an evidence boundary. During replay, each signed
assignment must match the supplied eligible fleet identity and generation.

An exact global replica requires all operator inputs and a complete EMA
history from a first intent with `evidence_empty_prior: true` or an independently
authenticated checkpoint. An empty predecessor alone is insufficient: a first
intent may inherit a migrated EMA store. The producer attests empty prior only
after checking every transcript record, including identities absent from the
live census. A migrated prior remains unavailable without a trusted checkpoint;
the summary does not disclose that private prior state.
This owner-scoped format supplies a complete cut for a single-operator
validator. A consumer with only one part of a multi-operator census must leave
the exact replica unavailable; its local prefix counts cannot reconstruct the
global shared-prefix denominator. Published validator scores remain usable.

For catch-up, process authenticated summaries in native-epoch order, once per
measurement hash, checking the previous measurement hash against the committed
replica checkpoint. A validator may intentionally skip an epoch: do not invent
an empty fold for that gap. A missing predecessor, missing stream object,
unavailable policy/key or failed replay leaves the replica checkpoint unchanged
and exact agreement unavailable. V1 summaries and old approximate leaderboard
state cannot bootstrap an exact replica. The existing bounded summary upload
queue does not guarantee historical delivery; a dropped predecessor needs
backfill before exact replay can continue.
