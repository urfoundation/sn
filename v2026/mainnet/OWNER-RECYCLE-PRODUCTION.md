# Production owner-recycle transition

The standard validator's V2 `RunRelease` path now accepts an independently
approved production configuration. It measures the unchanged provider workload,
derives a 10% provider / 90% recognized-owner weight row, prepares the real CRv4
atomic source-and-weights transaction, and retains a separately signed production
sidecar in the existing durable intent. Compatible independently approved
runtime renewals retain the complete original authority for prior decisions.
No mainnet identity, key, approval,
transaction or economic outcome was supplied by this implementation.
Local deterministic qualification is recorded in the
[producer evidence](evidence/owner-recycle-production-qualification-20260927.md),
[runtime evidence](evidence/production-runtime-qualification-20260927.md), and
[authority-history evidence](evidence/production-authority-history-qualification-20260928.md).

The old observation approvals and unsigned v1/v2 measured capsules remain
read-only. They cannot select this path. The new schema-3 config requires the
separate v2 production approval and signature domain described in
[production runtime admission](VALIDATOR-PRODUCTION-RUNTIME.md).

The external approver must select the complete resolved config and exact
runtime/source review, finite native block/epoch windows, actual validator
hotkey, sorted validator and recognized-owner hotkey sets, a bounded native
activity age, and `production.activation_native_hash`. The latter is the exact
canonical finalized block at `valid_from_native_block` for an initial config;
its subnet epoch must
equal `first_native_epoch` and `PendingServerEmission` must be zero. A later
decision retains that original activation instead of pretending later rewards
were earned under the new policy. The first durable intent must use that first
native epoch; a later epoch requires its authenticated production predecessor.
The validator still requires complete real
activation, operator API/key/payout and proof-history inputs through the existing
V2 startup. Production approval does not grant registration or staking authority.

Fresh production preparation also requires the independently signed
`production.epoch_schedule_profile: urnetwork-subtensor-tempo-drift-v1` under
the exact reviewed runtime/source authority. The [v470 scheduling correction](evidence/native-economics-context-and-schedule-20261002.md)
uses the actual subnet tempo for the drift fallback and evaluates commit timing
after initialization while preserving pre-coinbase reveal timing. A missing or
unknown profile, generic runtime binding or revoked producer capability cannot
reach a fresh nonce/signature. No spec-number or testnet inference supplies it.

The field is optional solely to preserve original signed JSON and historical
replay. Old pending intents retain their original nonce, signature, epoch and
drand round. Compatible authority renewal must keep the original profile;
switching profiles needs a separately reviewed activation, with all preceding
intent custody and liabilities retained. The monitor's explicit native-deadline
policy must choose the matching profile and exact expected producer/config;
forecasts remain distinct from observed epoch misses or successful application.

Before a decision can be signed, actual native reads authenticate every approved
validator's forward/reverse registration, coldkey, weighted stake and permit.
Coldkeys must be distinct. Bounded `LastUpdate` or registration freshness allows
the first submission without requiring that same submission as a prerequisite.
This is evidence of native eligibility/activity, not a remote process heartbeat.
The minimum validator and operator counts, self/controlled masks and signed
weight cap remain unchanged. Exact canonical coordinator reads and full provider
proof replay authenticate operator, pool, binding, deposit and source-root facts.

The provider artifact and its V2 envelope retain their original bytes and
meaning. A bounded production proof records the original signed approval,
owner census, operator observations, validator eligibility, drain boundary and
derived row. A distinct native source-hash domain commits both that proof and
the provider bytes before preparation. A second sr25519 seal binds the proof,
provider envelope and exact prepared extrinsic. The existing atomic intent
writer retains all of this before any submission. The submission gate requires
a private grant for those exact prepared bytes, issued only after the real V2
intent verifier replays the sources and row; a signed config alone cannot send
an unchanged parent row. Pending recovery keeps the original signed bytes and
uses existing receipt, nonce, epoch and runtime checks.

Archives capture the additional owner/validator/drain native reads and preserve
the sidecar in the original intent. Production archive replay requires fresh
independent source observation as well as the provider proof and both signatures.
Advancing finality witnesses are not embedded as immutable decision facts, so
the same historical decision reproduces after the head advances or metadata
caches are lost. A different signed config cannot reinterpret this proof.

The measured row is the transaction's input, not proof of its final economic
effect. Finalized inclusion, reveal/application, Yuma incentives, actual native
miner allocation, recycled value and source-derived rounding tolerance remain
monitored postconditions. Do not report the requested 10% native-miner outcome
before those observations exist. There is no reserve credit from recycling.

## Durable original production authority

Runtime tuples alone cannot authenticate old sidecars, which name their complete
original config and independently signed approval. `production_authority_history`
now selects an ordered list of exact `ReleaseEvidenceV2File` references. Each
`urnetwork-validator-production-authority-v1` bundle contains the original
normalized config JSON, approval bytes and selected runtime-document bytes.
Each original signed config must select exactly its predecessor prefix; the
new signature selects the entire history. Admission bounds the list to 32
bundles, 6 MiB each and 64 MiB total before reading files. Duplicate configs,
altered bytes, reordered prefixes and conflicting runtime windows are refused.

`BuildOwnerRecycleProductionAuthority` exports already authenticated public
authority without signing or opening keys. `RetainOwnerRecycleProductionAuthority`
writes content-addressed bundles through the existing immutable fsync writer.
Ordinary approval retention also retains the current approval, runtime documents
and all selected bundles. Repeated retention is idempotent after a partial write;
no fixed observation record or original bundle is replaced. The default loader
can reload these exact bytes from private state after provisioning sources are
lost. Archive and restart still authenticate the bytes and original signatures.

A successor's separately signed `production.activation_native_block` retains the
original drained block/hash and first native epoch. Its runtime block interval
may advance without requiring another drain or pretending already earned rewards
belong to a new first epoch. Omitting the field preserves the original wire
meaning. This continuity class holds deployment, economic policy, signer,
operators, masks, proof bounds, endpoints and custody paths fixed; runtime pins,
authority references and polling may advance. Policy/key/path migrations require
their own transition and are not implied by runtime compatibility.

Source, sidecar, intent, capture and archive readers resolve the exact original
approval from the already authenticated history. These original config owners
are permanently read-only: they cannot open a current writer, sign another
sidecar or issue a current prepared grant. Current signing still uses the current
independent approval and exact purpose-bound runtime. Receipt and application
reads use their own approved historical block windows; unavailable evidence
remains an error. Pending recovery first checks the original receipt, preserves
ambiguous signed bytes, and can record genuine epoch expiry. It does not acquire
permission to rebroadcast an old grant from the new approval.
An authenticated pending transaction returns a distinct receipt-wait result.
The actual steering loop reports the transaction and epoch on each poll, keeps
checking receipts beyond the ordinary failure ceiling, and invokes reconciliation
again when the epoch advances. It does not mark that epoch healthy or complete.
Joined integrity errors and earlier hard failures retain their original failure
budget; cancellation admits no further poll. Independent workers stay running.

The loader derives immutable runtime-only windows from complete original bundles
and explicit runtime documents. Original windows end at the next selected runtime
boundary; overlapping documents must agree exactly. Upload admission copies only
these detached windows, routes and deployment scope, then discards economic and
signing authority. Existing proof cursors and signatures remain in place.

This implements bounded compatible config renewal, including cold replay and
source-file loss. It does not supply missing native epochs, close gaps without
their existing authenticated evidence, approve an unknown runtime automatically,
or prove live receipt/application/economic success. Those remaining MG-04/RT-04
and MG-06 qualifications stay explicit.
