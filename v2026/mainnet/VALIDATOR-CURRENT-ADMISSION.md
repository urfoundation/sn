# Independently accepted current admission for the two UR validators

`activate-validators admit-current` and `start` have a concrete current-authority
adapter for the original majority and secondary services. It requires a separate
signed acceptance, exact original installation custody, and fresh observations.
The ordinary process approval and earlier `admit-*` reports remain insufficient.
No approval, signer operation, mainnet transaction, service installation or live
start was performed by this implementation.

This bounded initial-launch profile requires both service-owned operator ledgers
to be fully imported and independently authenticated, with empty tails and no
nonempty steering intent graph. Missing ledgers do not mean empty history.
Unfinished work, producer authority/runtime rollover, ownership migration and
root-service activation require separate authority.

## Independent signature and accepted assumptions

The envelope is `validatorActivationCurrentApproval` in
[`validator_activation_current_authority.go`](validator_activation_current_authority.go).
Its authorization schema is `urnetwork-mainnet-validator-current-admission-v1`.
Sign the literal `urnetwork-mainnet-validator-current-admission-approval-v1`, one
NUL byte, and Go `encoding/json` of the complete typed authorization in declaration
order. The envelope adds the 64-byte Ed25519 signature as lowercase hex. Supply
the trusted verification key independently as `0x` plus 32 bytes. It must differ
from the process/producer approval keys and native hotkeys. Different bytes alone
do not establish independent organizational control.

The authorization binds the original process approval object hash/key and
bootstrap plan, exact successor/Safe/execution/canonical input files, execution
plan and stable installation identity, selected v2 Safe current-policy hash,
both native hotkeys, nonempty pinned custodian review evidence, and a finite
window contained within the process approval. The original approval binds host,
boot, both roles, release, units, service identities, configs and paths. The
stable installation identity includes eight original custody seals and the exact
terminal anchor event/receipt; it excludes the moving observation head.

Both exact policy strings in that source file must be signed. They accept
owned-RPC finality, non-atomic current reads, the separately signed current-only
Safe policy without complete governance history, and no prestart worker liveness
for a never-started unit. Strict majority **stake capacity** is required; first
weight activity, applied influence, emissions and the 10/90 outcome remain
unproven. A newly registered eligible validator need not already have applied
its first weights.

The separate custody statement attests exclusive hotkey/protocol-state and
privileged host custody for the lifetime of the original activation and its
acknowledged invocations, with no transfer until fenced shutdown and new
independent authority. This is a signed custodian attestation, **not a demonstrated
distributed lock or chain proof of exclusion**. This task supplies neither that
attestation nor an approver's underlying custody evidence.

## Fresh composition and durable ownership

Each invocation reconstructs the original successor owner and already retained
canonical/runtime/current-policy histories. It cannot import new authority,
replace the owned route, consume a readback JSON file as capability, or send a
transaction. Readback may finish an already consumed terminal journal event;
original nonces, sends, signed bytes and liability remain unchanged.

Before each unit's reservation and again after its sync, the route freshly reads:

- Original runtime, signed activation/drain checkpoint, native epoch, registration
  generations, Active/Permit, Recycle mode, age bounds and complete conservative
  majority-capacity stake census.
- Both operator activation contexts and nonce-bound client-key responses,
  approved/committed proof histories, service-owned ledgers and empty authenticated
  tails. A retained partial checkpoint cannot replace either current role.
- Original contract-role binding and both declared EVM scan floors, each no later
  than the earliest of the eight original EVM inclusions. The actual installation
  producer authenticates all original CREATE/link receipts, evidence-anchor
  receipt/event, and complete initial five-account/Safe storage.
- Current Safe storage and five executable/domain views at the **same native/EVM
  mapping** as native, operator and stake observations. Ordinary mutable
  operator/accounting counters are allowed; code, owner, guardian, oracle,
  recorder, proxy slots, policy and evidence binding remain exact.
- Exact host/config/unit state and genuinely absent prestart progress. An
  unavailable read of an existing file is not absence. An already acknowledged
  first role must publish responsive steering before the second can start.

The original 60–900 second owned-read budget bounds each admission. Original
sample age, operation count and manager deadlines also apply. Source files,
counted journal, clock, both windows and host state are rechecked around sync.
The journal ceiling is 128 KiB; oversize evidence refuses before a manager start.

Permanent `<unit-file>.sn-first-start.claim` and companion `.lock` bind both
approvals independently of boot, release and journal path. Exclusive durable
creation precedes `StartAt` publication. Alternate envelopes and single missing
markers cannot renew capacity; partial creation needs explicit disposition.
The common per-unit control lock excludes cooperating repair controllers.
Privileged custody must protect claims/journals against rollback, copy or deletion.

The temporary stopped view belongs only to the same synchronous newly consumed
reservation. Reopening nonzero `StartAt` without an acknowledged generation is
uncertain and never retries. A lost manager reply stays consumed. Acknowledged
invocations can reconcile after expiry under original approval and permanent
claims, without a new start. Completion requires attributable PID/invocation and
fresh published steering; responsive `read_wait` proves liveness only.

## Invocation and remaining gates

Provision original installation, producer evidence/credentials/ledgers and both
approved envelopes first; use the existing process `claim`/`install` flow.

```sh
sn-mainnet activate-validators admit-current \
  --approval /approved/process-approval.json \
  --accept-approval-hash sha256:PROCESS_FILE_SHA256 \
  --independent-public-key 0xPROCESS_APPROVER_KEY \
  --current-approval /approved/current-admission.json \
  --accept-current-approval-hash sha256:CURRENT_FILE_SHA256 \
  --current-independent-public-key 0xCURRENT_CUSTODIAN_KEY
```

An authorized `start` uses the same six file/key arguments and
`--execute-approved-starts`. It performs fresh composition again. `resume` and
`status` use original process arguments and cannot issue fresh starts. No prior
successful report supplies the missing current approval or caller opt-in.

The exact current envelope is immutable once retained. Later Safe/runtime
revisions do not extend it. Partial-pair progress persists and the remaining role
still needs fresh admission under the same acceptance. This grants no stop,
restart, root-service or transaction authority. `activation_ready`,
`root_service_active` and `chain_success_proven` remain false.

The [sealed qualification](evidence/validator-current-admission-qualification-20261001.md)
pins combined SN `b8dc332a` and server `0b8e758d`; separately bounded server
`720e7c61` compatibility keeps its own graph and scope. Tests use synthetic
identities, fake manager transport, real journals/files, and separately exercised
concrete chain/proof readers. Signed custody/launch approvals, live installation
and producer evidence, root-service admission and actual systemd/mainnet rehearsal
remain separate gates. Schema 750 and subscriber-v2 rollout are not approved by
either receipt.
