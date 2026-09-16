# Mainnet prelaunch fixes

Updated 2026-09-16. This is the canonical tracker for fixes to complete before
mainnet launch. The initial workstream is automatic handling of compatible
Subtensor runtime upgrades. Implementation and qualification remain incomplete;
no completed fix is claimed.

For each item, record its implementation commit and relevant test or operational
evidence before marking it done. Add newly discovered adjacent issues here.
Use `Planned`, `In progress`, `Blocked` and `Done` consistently. Keep each ID
stable, and record the concrete blocker and next action for blocked work.
The completion-evidence column below states the required result; it is not a
claim that the result has been achieved. Add links to actual receipts as work
closes, including the source revision and release containing the fix.
For each implementation update, record the remaining action, the affected
checks and any earlier results being reused. Passing tests alone does not mark
a fix done when its required deployment or operational evidence is still pending.
The active testnet finalization continues independently.

## Prelaunch fix tracker

| ID | Fix | Depends on | Owner | Status | Completion evidence |
| --- | --- | --- | --- | --- | --- |
| RT-01 | Immutable runtime views anchored to the correct block and purpose | — | Astra | Planned | Current reads, historical execution and concurrent signing use consistent, separate contexts. |
| RT-02 | Shared capability profiles for calls, storage, signing, CRv4, custom APIs and precompiles | — | Astra | Planned | Compatible versions pass automatically; changed consumed interfaces identify the precise unsupported capability. |
| RT-03 | Construct and sign each native operation from one view; reconcile stale or uncertain attempts | RT-01, RT-02 | Astra | Planned | Deterministic upgrade races preserve signed history and cause no duplicate transaction. |
| RT-04 | Replace version-specific live admission in simulator, miner, both validator paths and bootstrap | RT-01, RT-02, RT-03 | Astra | Planned | A compatible upgrade needs no binary rebuild or manually added version entry. |
| RT-05 | Separate observed runtime from deployment, configuration and approval identity | RT-02, RT-04 | Astra | Planned | A compatible upgrade preserves the plan, approvals, leases, completed actions and observed epochs. |
| RT-06 | Preserve historical proof reuse and make metadata-cache capacity independent of catalog length | RT-01, RT-02 | Astra | Planned | Older proofs remain correctly authenticated; eviction, restart and additional runtime versions retain valid progress. |
| RT-07 | Suspend only operations affected by an unsupported change and expose an actionable reason | RT-02, RT-04 | Astra | Planned | Independent services continue where their dependencies permit; recovery resumes from saved progress. |
| RT-08 | Qualify upgrade handling and record the mainnet-readiness evidence | RT-01 through RT-07 | Terra | Planned | Affected normal/race tests and a controlled upgrade during an active integration campaign pass with unchanged approvals and reconciled transactions. |
| RL-01 | Bind launch attestation to an explicitly approved immutable release and its complete input manifest | — | Astra | Planned | Publishing documentation or advancing main does not invalidate an approved unchanged deployment; changed executable, source, policy, contracts or dependencies still require the appropriate new approval. |
| PF-01 | Reuse an immutable journal index and authenticated historical plans within one reconciliation | — | Astra | In progress | Deterministic work-count tests bound journal indexing by entries and plan authentication by distinct sources, while each receipt retains its own identity and postcondition checks. |
| PF-02 | Give simulator operator taskworkers an explicit workload profile, including retained queue handling | — | Astra | In progress | Required subnet/operator tasks run for both operators; excluded queued tasks and post hooks remain untouched; production defaults and restart behavior pass affected tests and managed startup. |

Prioritize PF-01 and PF-02 for the current testnet recovery. RT-01 through RT-08
form the subsequent runtime-resilience workstream required before mainnet
launch; RL-01 can proceed independently. Link the bootstrap implementation and
launch sequence from [MAINNET.md](../mainnet/MAINNET.md) rather than maintaining
a second launch plan here.

RT-01 and RT-02 can proceed in parallel. After those foundations, cache/history,
transaction handling and operation-specific admission can progress independently
where their inputs are stable. An external manually reviewed artifact catalog
may be an interim aid; it does not complete RT-04's automatic-upgrade requirement.

RL-01 follows an actual 2026-09-16 launch interruption: the qualified executable
was built at `541e13cf`, then publishing reports advanced main to `0fd7ffc0`.
All Go source, modules and release-lock bytes were identical, but
[executable attestation](../sim-testnet/executable_attestation.go) required the
running executable, checkout, fetched ref and current GitHub main to share the
same commit. Import correctly enforced that current rule and stopped before
mutation. The immediate recovery is a matching build and publication freeze.
The proposed replacement must authenticate a complete approved release;
matching only Go files is insufficient. Add deterministic controls for
documentation-only publication, unrelated later releases, unauthorized input
drift, revoked releases and restart from the retained approved release.
This proposed change is not an additional gate for the current testnet run.

PF-01 follows source review during the 2026-09-16 managed startup.
[Carried-action preparation](../sim-testnet/carried_preparation.go) looks up each
action through a full journal copy and scan. The
[historical RPC identity check](../sim-testnet/owned_rpc_history.go) also rereads
and authenticates the same archived plan for each original public receipt.
These repeated costs remain after the separate journal-loading repair. Live
process counters establish ongoing work, not attribution of all startup time
to either path. That invocation later failed its taskworker log gate, recorded
separately under PF-02. The isolated correction is frozen at
`351ece79d9f4dad93888c74c8bdcc699dd4c8dac`; release publication and deployment
remain pending. Its [qualification handoff](/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/HANDOFF.md)
specifies 32 affected roots normally and under race, plus eight causal controls.
The initial normal and race runs each passed 31 roots and exposed one existing
cold-cache fixture mismatch. Fixture correction
`d52028de6864f7b1c48381fcb7652361d2520932` then passed the changed test and its
adjacent warm-cache control in both modes. Their recorded body, outer and join
exits are zero, with valid event sets and unchanged input checks. Retain the
other 30 passing roots per mode and the original causal result of two expected
failures and six passes. The [correction handoff](/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/fixture-correction/HANDOFF.md)
defines that reuse; [normal evidence](/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/normal/body)
and [race evidence](/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/race/body)
remain local qualification records. The candidate keeps both indexes local to
one invocation and leaves durable audit-cache authority intact.

Capture a consistent journal snapshot and index its applicable witnesses once.
Authenticate each distinct historical plan once into an immutable object scoped
to that reconciliation and its authority inputs. Keep per-receipt checks and
fresh operational observations. Add deterministic controls for multiple source
plans, ancestry, changed authority, appended journal entries, corrupt evidence,
cancellation and retry; measure operation counts rather than elapsed time.
Do not let reuse hide new state or turn a failed check into a passing result.

PF-02 follows the actual managed-start failure at 06:29:05 UTC on 2026-09-16.
Both operator taskworkers scheduled the full production backend workload.
Each emitted two geolocation certificate-pin rotation errors and one fiat
payment warning for a synthetic account without user authentication. These
six lines produced four blocking process-log classes. All 33 managed processes
then stopped; the release campaign did not start. See the retained chronology
in [FINAL-2.md](../sim-testnet/FINAL-2.md).

Add an explicit subnet-operator workload profile at the
[taskworker entry point](../../server/taskworker/run.go), retaining all tasks
required for operator service and subnet settlement. Restrict initial scheduling
and queue claims, including deferred post hooks; excluding a new schedule alone
does not handle unrelated jobs left in the retained database. Filter before
the claim limit so excluded rows cannot starve required work. Preserve those
rows, the ordinary production default, certificate checks and the process-log
gate. Bind the profile into both operators' launch and restart specifications.
Qualify scheduling, dispatch, retained queues, post hooks and defaults with
deterministic tests, then verify actual managed startup. Frozen SN `8e6d56b5`
and server `6752a8df` pass all 30 affected roots normally and under race, with
seven expected failures/five passes in the controls. The
[qualification evidence](../sim-testnet/peerreview/evidence/FINAL-2-preparation-fixes-20260916/README.md)
preserves the pre-test service refusals as well as successful bodies. Both
PF-01 and PF-02 are integrated locally at `dc90e4c`; publication and actual
managed startup remain pending, so these items remain in progress.

## Runtime-upgrade compatibility proposal

Proposed design, 2026-09-16. Compatible chain upgrades should continue without
a subnet rebuild, plan migration, repeated funding, audit restart or lost soak
progress. The active testnet finalization continues with its qualified release;
this document describes the subsequent implementation.

## Why upgrades currently interrupt us

Current admission selects one compiled runtime artifact in
[crv4/reviewed_runtime.go](../crv4/reviewed_runtime.go), with separate consumers in
[validator/runtime_identity.go](../validator/runtime_identity.go),
[miner/fleet_runtime.go](../miner/fleet_runtime.go) and
[sim-testnet/runtime_identity.go](../sim-testnet/runtime_identity.go).
[runtime_config_identity.go](../sim-testnet/runtime_config_identity.go) also lists
specific permitted version transitions. Consequently a compatible chain
upgrade can require source edits, a release lock, a build and a new plan.

The [459-to-460 review](../docs/spec/runtime-460-audit.md) found unchanged interfaces
used by SN and a metadata change confined to the version constant. Runtime
staking internals nevertheless changed. That distinction matters: a metadata
comparison can establish encoding compatibility, but cannot prove all economic
behavior equivalent.

The SDK treats `spec_version` as a runtime specification identifier; even bug
fixes can change it. `transaction_version` describes dispatchable-call
compatibility under the SDK's versioning contract. Neither value alone proves
compatibility of every storage reader, custom runtime API or precompile we use.
[SDK runtime-version semantics](https://paritytech.github.io/polkadot-sdk/master/sp_version/struct.RuntimeVersion.html).

## Proposed operation model

Use a shared resolver in the existing native-chain layer. An operation asks
for the capabilities it needs and receives an immutable runtime view:

- Chain genesis, anchored block hash and observed runtime versions.
- Code and metadata hashes, with the actual metadata and supported encoders.
- The compatibility-policy revision and admitted operation profiles.
- The context's purpose: post-state reads, historical block execution, or
  construction of a new transaction.

Exact hashes remain evidence. Supported operation profiles determine whether
work can proceed. Do not mutate a connection-wide decoder while historical
readers or transaction constructors still use it. At upgrade boundaries,
distinguish the runtime executing a block from the runtime installed in its
post-state; historical events and extrinsics must use their execution context.

An unknown version number with an admitted profile proceeds automatically.
An unsupported capability produces a specific error and suspends its dependent
operations. Other capabilities continue where their dependencies permit it.
Keep completed actions and observed epochs; a missing required observation
still cannot be counted as a fully observed acceptance epoch.

## Compatibility checks

Check only the interfaces an operation consumes, using canonical structural
types rather than portable metadata type numbers or a hash of all metadata.
Unrelated pallets, documentation and version constants should not invalidate
an otherwise identical interface.

| Boundary | Required checks |
| --- | --- |
| Native calls | Call identity and argument order/types; resolve indices from admitted metadata where possible. |
| Storage | Keys, hashers, value types, optional/default behavior and relevant constants. |
| Signing | Extrinsic format and complete ordered signed-extension encoding and semantics supported by the signer. |
| Runtime APIs | Method/version and response encoding, including the selective metagraph API. |
| CRv4 | Prepared payload and source-commitment encoding, call indices, reveal rules and signing domain. |
| EVM integration | EVM chain identity, receipt/checkpoint mapping, required precompile interfaces and behavior. |
| Economic operations | Existing permission, fee, balance, collateral, reserve and scheduling bounds, plus action postconditions. |

Existing [CheckMetadata](../crv4/chain.go) needs stronger shape validation; storage
presence and an unknown extension having zero encoded size are insufficient
for automatic write admission. [preparedSourceEncoding](../crv4/source_commitment.go)
and the [selective-metagraph reader](../crv4/validator_stake.go) need explicit
encoding/API profiles to replace their version-number lists. Metadata format
v14 alone does not establish the custom API's return layout.

Automatic admission accepts upgrades authorized by the chain's governance
within these supported capabilities and operational bounds. It is not a proof
that arbitrary new runtime code preserves every economic rule. A semantic
change can retain its wire encoding; dry runs where available and ongoing
postcondition checks improve detection but do not eliminate that limitation.

## Upgrade and transaction handling

1. Observe an upgrade through the owned node and anchor the runtime context to
   a canonical block. A subscription is a notification; fetch matching version,
   code identity and metadata from the appropriate block context.
2. Validate the required profiles once per artifact and policy revision. Record
   the result and atomically make the context available to new operations.
3. Construct and sign a native transaction from the same immutable context,
   using its observed signing versions. Recheck the context before publishing
   signed bytes; handle an upgrade racing broadcast through reconciliation.
4. Preserve signed attempts. Establish inclusion, failure and nonce state before
   replacing a stale attempt. An uncertain submission must never trigger a
   blind repeat of a transfer or another non-idempotent action.
5. Append the new runtime observation and continue the existing campaign.

The SDK's `CheckSpecVersion` rejects transactions carrying an obsolete signing
version, so automatic compatibility cannot mean retaining stale signing data.
[SDK signing-version check](https://paritytech.github.io/polkadot-sdk/master/frame_system/struct.CheckSpecVersion.html).

In particular, [SubstrateManager](../sim-testnet/substrate.go) currently authenticates
a fresh head separately from the shared metadata used to construct calls.
`SendAsWithRecoveryPrecondition` accepts an already-built call. Change this
boundary so the admitted runtime view controls both construction and signing.
Widening the current allowlist alone would leave this gap.

Existing prepared CRv4 work retains its original bytes, runtime identity and
signatures. Any replacement is an explicit reconciled attempt; historical
proofs are never rewritten to name the newest runtime. EVM signatures use EVM
chain identity, so a native spec bump alone does not invalidate their bytes.
Relevant precompile and checkpoint behavior still requires admission.

## Stable identity, history and caches

Bind future deployment/configuration identity to chain identity, approved
economics and the compatibility policy. Store runtime observations separately
in the journal. A compatible observation should not change the plan hash,
activation identity, fleet leases, reserve-repair liabilities or approvals.
Every new signed artifact still records its own exact runtime and block
identity; the stable deployment policy does not replace its signing domain.
Existing signed manifests keep their original identity through an explicit
initial schema transition; do not reinterpret old hashes in place.

Historical reads select the applicable original artifact and adapter. Cache
their evidence with its block/artifact identity, verifier policy and actual
dependencies. Reuse a successful proof when those inputs are unchanged; a new
live runtime is not itself a reason to revalidate old facts. Continue checking
fresh balances, permits, fees and nonce state when an operation needs them.

The durable [historical audit cache](../sim-testnet/historical_audit_cache.go)
currently includes plan, release and executable identity. Eliminating routine
rebuilds and plan migrations preserves those existing cache hits. Any later
narrowing of cache keys must separately prove that verifier and authority
changes still invalidate affected proofs.

Replace the [metadata cache](../crv4/runtime_identity.go)'s catalog-sized lifetime
ceiling with fixed memory/byte limits and safe eviction. Runtime discovery and
admissible history must not stop merely because more versions have appeared.
Persist content-addressed artifacts when needed; an evicted entry can be loaded
and authenticated again. Cache bounded successful compatibility decisions;
do not let transient RPC failures poison admission.

## Delivery and acceptance

Implement immutable runtime views and shared profiles first, with comparison
against current admission during qualification. Then migrate live operations
and future plan identity to the policy model. An external reviewed artifact
catalog can remove recompilation as an interim step, but a manually maintained
catalog alone does not satisfy automatic upgrade handling.

Add deterministic synthetic tests following [CODESTYLE.md](../../connect/CODESTYLE.md):

- A higher spec version with compatible interfaces continues reads, signing and
  a running campaign without a new plan or repeated completed action.
- Unrelated metadata changes and type-number renumbering remain compatible.
- An upgrade between construction, signing and broadcast never mixes contexts
  or duplicates a submitted transaction, including uncertain outcomes.
- Upgrade-boundary and older-history decoding preserve their original runtime
  and signing domains while current work uses the new context.
- Changed consumed storage/call/API shapes and unsupported signed extensions
  suspend the affected operation with a precise reason.
- Semantic bounds still reject unsafe fee, permission, reserve or scheduling
  results even when their wire format is unchanged.
- Artifact eviction, RPC outage/reconnect and process restart retain valid
  progress and cannot turn stale or failed evidence into a passing result.

Astra (`gpt-6-astra`, effort `max`) diagnoses and implements; Terra
(`gpt-5.6-terra`, effort `max`) runs affected tests normally and under race.
The final integration exercise upgrades a controlled runtime while the
subnet is active and demonstrates continued required observations, reconciled
transactions and unchanged approvals. This architecture work is not an extra
preparation gate for the currently running testnet recovery.
