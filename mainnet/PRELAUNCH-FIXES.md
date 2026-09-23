# Mainnet prelaunch fixes

Updated 2026-09-22. This is the canonical tracker for fixes to complete before
mainnet launch. The initial workstream is automatic handling of compatible
Subtensor runtime upgrades. The [production hardening plan](#production-hardening-from-sim-testnet)
adds the lessons from the wider testnet finalization. Implementation and
qualification remain incomplete; no completed production fix is claimed.

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
| PF-03 | Include every retained operator signature in recovery and renewal accounting | — | Astra | Planned | Recovery discovers original, replacement and cancellation attempts across both operator databases and existing evidence stores, reconciles canonical receipts, and resumes without manual signature copying or duplicate actions. |
| PF-04 | Diagnose validator warmup and support bounded, resumable semantic startup | — | Astra | In progress | Retained validators produce fresh proofs through both operators; startup exposes the pending criterion, uses a justified warmup budget, and preserves valid recovery progress without counting stale proofs as acceptance. |

Prioritize PF-01, PF-02 and PF-04 for the current testnet recovery. RT-01 through RT-08
form the subsequent runtime-resilience workstream required before mainnet
launch; RL-01 can proceed independently. Link the bootstrap implementation and
launch sequence from [MAINNET.md](../mainnet/MAINNET.md) rather than maintaining
a second launch plan here.

RT-01 and RT-02 can proceed in parallel. After those foundations, cache/history,
transaction handling and operation-specific admission can progress independently
where their inputs are stable. An external manually reviewed artifact catalog
may be an interim aid; it does not complete RT-04's automatic-upgrade requirement.

The mainnet implementation order is:

1. Protect signing, historical interpretation and recovery first: RT-01,
   RT-02, RT-03 and PF-03. These establish the shared interfaces and preserve
   ownership of transactions across upgrades or interrupted startup.
2. Remove routine restart requirements: RT-04 through RT-07. Work on RL-01
   independently, and finish the remaining operational verification for PF-01
   and PF-02 using the current testnet run.
3. Complete RT-08 against the composed release, reusing unaffected results.
   Batch independent failures, fix their causes and adjacent paths, and rerun
   the affected checks. An interruption does not erase completed observations.

For each update, attach the implementation commit, affected checks, preserved
results, deployment evidence and next action to its stable ID. A new issue only
blocks operations that depend on it. Keep future mainnet work out of the active
testnet launch path unless that run exposes a concrete dependency.

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
`351ece79d9f4dad93888c74c8bdcc699dd4c8dac`; it is now published in SN release
`aeda6abbd2dc0abc92bb0f60975cf89b509e8017`. Verification during managed startup
remains pending. Its [qualification handoff](/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/HANDOFF.md)
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
PF-01 and PF-02 are published in SN release `aeda6abbd2dc0abc92bb0f60975cf89b509e8017`;
server publication is `006e71b997db503604c4ef6bb0c2683dc0d984cd`, preserving the
qualified server commit `6752a8df246c0ee7e1c5a38cbd26b1e849b702ca` used by the
release. The matched executable has been built. Actual managed startup remains
pending, so both items remain in progress. Reuse the completed affected tests;
the remaining verification is operational startup and continuation of the
retained campaign.

PF-03 follows the continuation capture failure at 08:46:41 UTC on 2026-09-16:
operator-1-root nonce 106 had no signature in the collector's retained sources.
The complete census of both operator databases found 230 signed attempts,
including replacements and cancellations. Of those, 226 were already retained;
four original signed transactions were absent from the simulator's transaction
store. All four had successful canonical receipts, documented in the
[partial-start transaction evidence](../sim-testnet/peerreview/evidence/FINAL-2-startup-transactions-20260916/README.md).
The [sealed census and recovery evidence](../sim-testnet/peerreview/evidence/FINAL-2-signature-recovery-20260916/README.md)
records the complete comparison. At 08:57:55, create-only restoration added the
four original signatures while preserving all 2,268 existing RLP files and
the six watched state files. That operational repair submitted no transaction;
automatic collection remains proposed.

Unify the signature census used by continuation, renewal and recovery. Read
every signed attempt independently of its database status, deduplicate exact
hashes, and validate chain, recovered sender, nonce, destination, value and gas
envelope. Preserve distinct same-nonce replacements and cancellations, their
fee liabilities and original evidence. Unsigned intents are a separate class.
Retaining a signature does not authorize broadcasting it. Reconcile receipts
and nonce state before the owning production component retries an action;
missing evidence is not a reason to create a new logical action or rewrite
database status manually.

Use synthetic fixtures to reproduce the missing-store failure and cover both
operators, stale database statuses, multiple signatures for one nonce,
cancellations, malformed or conflicting records, partial export, idempotent
restart and an uncertain submission. Require complete nonce coverage, unchanged
spend limits and no duplicate execution. The current qualified collector's
supported restoration path remains available during implementation; replacing
it is not a prerequisite for the active testnet continuation.

PF-04 follows the managed resume that ran 09:41:17–10:20:25 UTC on
2026-09-16. All 1,000 fleet checks and 4,673 carried-action checks completed.
The new generation reached 33 healthy processes with no restarts, but none of
the four validator/operator proof domains acquired a fresh completed trail.
The owned-node semantic readiness budget was five minutes. The command exited
one with `release topology semantic readiness timeout: every validator must
complete a fresh verified trail through every operator`, then stopped all
33 processes. The process-log gate recorded no findings through 10:20:10.
The saved plan, journal, configuration, public identities, executable and
release lock remained unchanged; the campaign did not start.

The [closed failure evidence](../sim-testnet/peerreview/evidence/FINAL-2-managed-readiness-20260916/README.md)
preserves all three failed exits and the watched-state comparison. Subsequent
shutdown diagnostics place both validators inside retained settlement-history
replay when the parent cancelled them. The current deadline depends on RPC
route, although that authenticated replay is needed with either route.
The bounded warmup correction is qualified at
`8270992eb8fb2b1599a29271ec44426379007306`. It gives
retained strict-history startup the existing 30-minute budget on every RPC
route. Adjacent review also found that an already-cancelled invocation could
admit an already-ready snapshot; the correction checks cancellation before
admission. All eleven affected roots pass normally and under race; separate
causal variants reproduce exactly one intended failure each, with the other
ten roots passing. The correction is integrated locally; deployment and actual
managed startup remain pending. That startup must demonstrate that the budget
suffices for this retained history.
The [closed qualification](../sim-testnet/peerreview/evidence/FINAL-2-retained-startup-qualification-20260916/README.md)
retains the affected results and original compiler-capture failures separately.

The timing review also identified a duplicate preparation pass between strict
resume and the separate campaign command. The explicit same-owner handoff,
`resume --then-release-candidate`, is qualified and integrated at
`e109ac35c5ea5ff5006040c2987118e99627e863`. It preserves all campaign checks and
the completed readiness results; failed or cancelled startup cannot enter the
campaign. All 14 affected roots pass normally and under race, with three
intended failures and three passes in the causal control. The
[qualification](../sim-testnet/peerreview/evidence/FINAL-2-resume-campaign-handoff-20260916/README.md)
retains original harness failures and identifies reused passing results.
The matching release is published at `1860261` and its executable has been
built. Subsequent setup completed all 4,673 carried checks but collected nine
runtime-admission errors after testnet advanced to 461. The
[closed preparation evidence](../sim-testnet/peerreview/evidence/FINAL-2-runtime461-preparation-20260916/README.md)
records that failure and the finalized observation. Actual managed startup and
campaign verification remain pending, so PF-04 is still in progress. This new
update is another concrete instance of RT-04's version-specific admission
problem; adding a reviewed 461 artifact alone will not complete the automatic
upgrade requirement.
The runtime-461 correction is qualified at `8edb3167`: 103 affected roots pass
normally and under race, with nine expected causal failures and nine controls.
It also repairs former-current460 companion history admission. See the
[qualification evidence](../sim-testnet/peerreview/evidence/FINAL-2-runtime461-qualification-20260916/README.md).
Deployment and actual managed startup remain pending; RT-04 and PF-04 are not
closed by this version-specific correction.

Make the warmup requirement and pending proof domains
observable, and distinguish recoverable incomplete startup from invalid
evidence. Assess the supported recovery path for retaining useful live work;
preserve signer ownership, approved budgets and authenticated history.
Add deterministic controls for delayed initialization, an actual initialization
failure, cancellation, partial domain progress, restart and stale proofs.
Fresh proofs from every required domain remain necessary for readiness, and
fully observed epochs remain necessary for final acceptance. Neither a larger
timeout nor healthy process endpoints alone closes this item.

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
originally included plan, release and executable identity in every reuse key.
The 2026-09-21 [descendant-cache correction](../sim-testnet/historical_audit_descendant_cache.go)
admits compatible revisions for two immutable fleet proof kinds while retaining
authenticated authority, input and verifier dependencies. PH-05 below tracks
production adoption and the remaining scope. Eliminating routine rebuilds and
plan migrations also preserves existing exact-context hits; broader reuse still
requires proof that changed verifier and authority inputs invalidate affected
results.

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
(`gpt-5.6-terra`, effort `medium`) runs affected tests normally and under race.
The final integration exercise upgrades a controlled runtime while the
subnet is active and demonstrates continued required observations, reconciled
transactions and unchanged approvals. This architecture work is not an extra
preparation gate for the currently running testnet recovery.

## Production hardening from sim-testnet

Reviewed 2026-09-21 by Astra (`gpt-6-astra`, effort `max`) against source through
SN `eb926565`, the [full finalization requirements](../FINALIZE.md),
[incremental recovery policy](../sim-testnet/README.md#incremental-recovery-and-acceptance),
[first report](../sim-testnet/FINAL.md),
[independent peer review](../sim-testnet/peerreview/verify/README.md), retained
failure bundles, and the corrective commits cited below. The
[September 17 handoff](../FINALIZE-HANDOFF.md) is historical evidence of a stopped
qualification, not the current execution instruction. The testnet run and its
remaining acceptance work continue separately.

The recurring production risk is that an ordinary interruption can cross too
many ownership boundaries: an RPC failure invalidates startup, startup stops
healthy services, a patch changes approval/cache identity, and recovery repeats
history or financial preparation. Mainnet services must retain authenticated
progress, retry their own recoverable work, and suspend only operations whose
required safety conditions are unavailable. Passing final acceptance remains a
separate claim requiring complete evidence.

This section is a production implementation backlog. A committed simulator
repair is supporting evidence, not proof that the operator, miner, validator,
bootstrap or deployed mainnet path has the same protection. All PH items start
`Planned`; existing RT/RL/PF IDs retain their recorded status and evidence.
Do not copy testnet provisional flags or import the `sim-testnet` executable
into production. Extract required generic facilities into neutral packages and
qualify the production consumers described in [MAINNET.md](../mainnet/MAINNET.md#integration-with-this-repository).

### Priority, ownership and parallel delivery

`P0` protects funds, authority or required production liveness and must close
before mainnet activation. `P1` is required operational hardening before an
unattended mainnet launch; it can proceed alongside the P0 implementation.
Neither label adds a new gate to the current testnet run. Astra authors and
reviews the implementation; Terra (`gpt-5.6-terra`, effort `medium`) runs the
affected tests and initial triage. The component column identifies the code
owner boundary, not an additional agent or approval requirement.

| ID | Priority | Production component and outcome | Existing work / dependencies | Status |
| --- | --- | --- | --- | --- |
| PH-01 | P0 | Operator, validators, bootstrap: durable recovery with independent audit and runtime owners | PF-01, PH-02 | Planned |
| PH-02 | P0 | Native/EVM submitters: one logical action, reconciled signed attempts and exact custody | RT-03, PF-03 | Planned |
| PH-03 | P0 | RPC, artifact and HTTP clients: bounded transient recovery without duplicate writes | PH-02 for submission recovery | Planned |
| PH-04 | P0 | Native-chain consumers: compatible upgrades and block-correct historical decoding | RT-01 through RT-08 | Planned |
| PH-05 | P1 | Historical verifiers: durable, dependency-bound successful proof reuse | RT-06, PF-01; PH-04 interfaces | Planned |
| PH-06 | P0 | Release/configuration tooling: explicit release identity and lossless plan migration | RL-01; PH-01, PH-02 | Planned |
| PH-07 | P0 | Service supervision: independent restart, single ownership and meaningful readiness | PF-02, PF-04; PH-01, PH-03 | Planned |
| PH-08 | P1 | Replay and workload scheduling: bounded work, memory and foreground latency | PF-01, PF-02; PH-05 | Planned |
| PH-09 | P1 | State and artifact storage: explicit durable volume, atomic publication and recovery | PH-01; storage adapter precedent | Planned |
| PH-10 | P0 | Epoch, fleet and evidence scheduling: resumable partial renewals and correct windows | PH-01, PH-02, PH-04 | Planned |
| PH-11 | P0 | Treasury and bootstrap: conserved lifetime spend, reserve and funding semantics | PH-02, PH-06 | Planned |
| PH-12 | P0 | Contracts, operator and claims: complete settlement conservation and authorization | PH-02, PH-04, PH-11 | Planned |
| PH-13 | P0 | Provider, operator and validator protocol: identity isolation and durable proof progress | PH-01, PH-03, PH-07 | Planned |
| PH-14 | P0 | Governance/bootstrap: actual capabilities, activated policy and both validator roles | RT-02; PH-10 through PH-12 | Planned |
| PH-15 | P1 | Status/operations: actionable failure classes, progress and evidence-based ETA | All runtime owners | Planned |
| PH-16 | P0 | Qualification and evidence: deterministic faults, composed coverage and independent replay | Every affected implementation | Planned |
| PH-17 | P0 | Plan-derived indexes: bind cached lookup structures to their immutable plan/generation owner | PH-01, PH-05, PH-06 | Planned |
| PH-18 | P0 | Strict readers: re-authorize connection/runtime provenance at every boundary after provisional work | PH-03, PH-04, PH-05 | Planned |
| PH-19 | P0 | Historical snapshots: use the reviewed historical runtime authority without weakening current writes | RT-01, RT-02, PH-18 | Planned |
| PH-20 | P0 | Relay capacity: distinguish funded slots, retained history, scan pages and resident bytes | PH-06, PH-09, PH-11 | Planned |
| PH-21 | P0 | Fault controller: bounded parallel, idempotent component control with durable partial recovery | PH-01, PH-03, PH-07, PH-10 | Planned |
| PH-22 | P0 | Service clients: retryable transport incidents, connection recovery and final error budgets | PH-03, PH-07, PH-13, PH-15 | Planned |
| PH-23 | P0 | Capacity revisions: bind funded slots, history horizon and every finite storage dimension | PH-06, PH-09, PH-11, PH-20 | Planned |
| PH-24 | P1 | Recovery performance: authenticate each retained plan once per immutable lineage | PH-01, PH-05, PH-17 | Planned |
| PH-25 | P1 | Supervisor lifecycle: explicit deployment stop joins every owned workload child | PH-01, PH-07, PH-21 | Planned |
| PH-26 | P1 | Large evidence transport: typed, cancellable public replay with finite admission | PH-03, PH-08, PH-09, PH-20, PH-23 | Planned |

Work in parallel on transaction/recovery (PH-01/02/06/11), chain access and
proofs (PH-03/04/05), service/storage (PH-07/08/09/13), and scheduling/economics
(PH-10/12/14). Agree on action, runtime-view and evidence identities first;
independent changes can then be integrated without rebuilding their consumers
repeatedly. PH-15 and PH-16 follow each change rather than waiting for a final
large cleanup. Mainnet economics and destructive UID operations retain the
specific unresolved choices and capability checks in MAINNET.md.

### PH-01 — Durable progress and separate audit/run ownership

**Lesson.** Setup served as deployment, historical audit, repair controller and
runtime launcher. A later read failure repeated already completed preparation.
The corrections include `31cfaf84` ([read-only audit](../sim-testnet/historical_audit_command.go)),
`fa8f84e4` (traffic independent of setup replay), `3541b3e0`
([durable accounted traffic](../sim-testnet/paid_traffic.go)) and `2269906e`
(provisional epoch completion distinct from strict acceptance).

**Production change.** Persist a dependency graph of logical actions and
per-component checkpoints. Commit each successful independent unit before
moving on. Let the run command resume the first pending unit and let a
read-only audit inspect a consistent immutable snapshot in parallel. Route an
audit-discovered repair through the existing transaction owner and a specific
repair action. A read-only audit cannot acquire a signer, mutate deployment
state or stop unrelated processes. Fresh authority, custody, chain identity,
spend, finality and value-conservation checks remain mandatory at the operation
that depends on them. Deferrable historical review remains visible until final
acceptance; invalid signatures or accounting never become soft failures.

**Closure.** Interrupt after every checkpoint, restart only one component, run
an audit concurrently, and inject a later audit failure. Completed actions and
observations must survive; independent traffic continues; only the invalidated
dependency is suspended. A required continuous epoch interrupted by the fault
must be reacquired with its dependent observations, without discarding earlier
valid phases or financial history. Verify that final acceptance cannot consume
a provisional, missing, canceled or failed result.

**2026-09-22 follow-up.** A successor relay plan accidentally restored an inline
public census despite provisional startup, so the campaign waited for hundreds
of historical publications after its local plan and debit checks had passed.
[The separate relay census](../sim-testnet/evidence_relay_public_audit.go)
retains bounded local manifest parsing, current chain/native authority and
original liabilities before startup. Its read-only worker owns copied source
and horizon state, reuses per-publication authenticated checkpoints, and must
finish successfully at the final gate. The transaction worker still verifies
each publication before sending. Production hardening must apply this separation
to successor plans as well as fresh deployments and service phase transitions
between individual replay items. Deterministic coverage must hold a real public
request open while proving release admission, then separately prove that failed,
canceled, missing or changed audit evidence cannot pass final acceptance.

**Process replacement follow-up.** A replacement driver previously spent its
startup budget reopening a signed interval owned by a dead process, then
invalidated that interval and required a second invocation to publish its
recovery. The [process recovery path](../sim-testnet/campaign_process_recovery.go)
now makes that decision before workers start. The exclusive deployment owner
appends a fresh signed interval under the phase lock, retaining original
observations, journal liabilities, deployment and authenticated fleet lifecycle.
Unstarted preparation keeps its checkpoint; a process gap cannot count toward
continuous acceptance. Qualify duplicate callers, interruption between
invalidation and publication, retained fleet evidence, successful/completed
sources, and read-only ownership before promoting the pattern to production.

### PH-02 — Transaction idempotency, partial failure and custody

**Lesson.** Original signatures were missing from the simulator even though
their transactions finalized; superseded attempts, cancellations and partially
completed generations also escaped narrower recovery scans. See PF-03 and the
[signature census](../sim-testnet/peerreview/evidence/FINAL-2-signature-recovery-20260916/README.md),
`a2f0e12d` (carry finalized transactions), `0da3b1e1` (reconcile superseded spend
once) and `aa8f18e2` ([authenticate probe retirement](../sim-testnet/precompile_probe_retirement.go)).

**Production change.** Fsync logical intent and exact signed bytes before
broadcast; retain all original/replacement/cancellation attempts independently
of database status. Bind chain, signer, nonce, action, destination, value and
fee limits. Enforce one owner per signing/nonce domain across services,
including any native/EVM account aliasing. On uncertain submission, reconcile
the exact hash, canonical inclusion, successful dispatch and postcondition
before rebroadcasting or replacing. Record each batched child outcome. Retire
an unsubmitted action only with evidence that no signed/in-flight attempt
exists; an immutable deployed predecessor requires a proved successor, not
rewritten history. Evidence discovery alone never authorizes broadcasting.

**Closure.** Inject crashes before/after intent fsync, signing, send, lost
response, inclusion, finality and postcondition publication. Cover two operators,
same-nonce replacements, cancellation, rejected dispatch, partial batches,
compacted journals and conflicting receipts. Assert at most one logical economic
effect, complete attempt/fee accounting, preserved original bytes and no
automatic nonce reset or duplicate deposit, stake, registration or claim.

### PH-03 — Retry at the actual failing I/O boundary

**Lesson.** A healthy owned node still produced transport timeouts; repeatedly
sending a large archive batch exhausted its budget. `f57e8d46` added bounded
[EVM reads](../sim-testnet/evm_read_retry.go); `da67c494` split failed historical
batches. `114c9173`/`561ae3bc` addressed nested retry budgets; validator steering
and publication required their own corrections. The
[relay-stream incident](../sim-testnet/peerreview/evidence/FINAL-2-relay-stream-failure-20260916/README.md)
also shows that a successful later read does not establish the original stall's
root cause.

**Production change.** Inventory every direct native/EVM call, response-body
read, artifact upload/download, publication and readiness call. Apply a shared
typed error policy and a single end-to-end budget per logical operation, with
bounded attempts, cancellation, backoff and jitter. On an explicitly owned
unlimited RPC, retain zero request-quota pacing and no public fallback; bounded
in-flight work and recovery delay are still needed to avoid overload. Provider
rate responses must not impose a generic minutes-long cooldown on this route.
Split retryable failed batches, retain successful members, match response IDs
and pinned blocks, and keep all sub-batches inside their parent's deadline.
Do not multiply budgets through nested wrappers. Validate complete streamed
objects before publication; retry an idempotent object by its content hash.

Exhausted transient work becomes a persisted retryable operation with a next
attempt and alert. It must not kill unrelated services or count as success.
Cancellation, malformed data, wrong identity, permanent contract revert and
unavailable pruned history have distinct outcomes. Integrity errors containing
the word "timeout" remain integrity errors. Writes use PH-02 reconciliation,
not the read-retry loop.

**Closure.** Deterministically inject disconnect, DNS/HTTP failures, timeout
during body read, missing/reordered batch responses, partial success and a
large-batch refusal that succeeds when split. Verify exact call/attempt bounds,
shared deadlines, cancellation joins, preserved successes, correct permanent
classification and eventual continuation after a network outage. Exercise the
real caller layers, including both validator paths and the artifact reader.

**2026-09-22 cancellation follow-up.** Generation 24 reached its signed
acceptance scope but a normal client-canceled immutable download became a
blocking process warning. The production artifact handler now distinguishes a
request-owned cancellation from deadline, integrity, storage and write errors;
a partial body still aborts. The simulator recognizes only the exact legacy
handler diagnostic and retains any subsequent joined failure across polls and
restarts. Interrupted runs label pending, never-triggered faults and unexercised
vectors as consequences of the recorded stop while retaining the failed final
verdict. Regression coverage exercises the actual handler, persisted scanner,
and a post-boundary scenario through later lifecycle and terminal snapshots.

The adjacent signed client-key observation path now retries an interrupted
HTTP read once with the same nonce and pinned decision. That retry and the
existing smaller-batch admission fallback share the two-reservation ceiling;
they cannot multiply quota. Complete response/body-close ownership precedes
retry, existing immutable capture slots are authenticated on recovery, and
signature, identity, quota and storage failures stay hard. A missing transport
response no longer adds a false signer-mismatch verdict: exhausted transient
reads remain eligible for the existing in-process steering continuation. Tests
discard a real signed response, authenticate its retry, reuse exact durable
captures without a live session, and continue the real compact-head collector
after a timeout into the next native epoch.

### PH-04 — Runtime changes and historical archive compatibility

**Lesson.** Repeated version-specific admission fixes for 455/458/459/460/461
and later runtimes blocked execution even when consumed interfaces were
compatible. Some historical reads incorrectly demanded the live artifact.
The [runtime/config migration evidence](../sim-testnet/peerreview/evidence/FINAL-2-runtime-config-identity-20260915/README.md)
preserves the original signing identity rather than relabeling it.

**Production change.** Deliver RT-01 through RT-08 across miner, operator, both
validator roles and bootstrap. Construct and sign from one immutable runtime
view; validate consumed call/storage/API/precompile/signing capabilities.
Separate block execution from post-state context at upgrade boundaries. Store
observed runtime versions as evidence, separate from stable deployment policy.
Historical reads bind genesis, block hash, original runtime/metadata and decoder
version. An archive-capability refusal identifies the missing proof and blocks
only dependent work; never substitute a current-state read for a historical
one. Keep artifact caches bounded independently of the number of known versions.

**Closure.** Use the RT-08 controlled upgrade plus historical reads on both
sides of the upgrade, concurrent signing, stale subscriptions, evicted metadata,
wrong genesis and pruned-state responses. A compatible update requires no
manual version entry or repeated funding; an incompatible consumed interface
halts that operation with a precise capability error. An ABI match alone does
not establish unchanged economic semantics.

**Repair admission follow-up (2026-09-22).** Fleet renewal still demanded a
static runtime pin after continuation and diagnostics had authenticated the
same compatible successor. Use one retained-evidence authority model across
read-only planning, repair apply and readiness. The simulator now shares
[the approval source selector](../sim-testnet/provisional_continuation.go):
readers bind the active approval; setup and fleet repair bind an immutable
reviewed successor before activating it. Exact journal/source reconstruction,
custody, signing-domain, current capability and budget checks remain mandatory.
Do not promote a provisional observation into release acceptance.

Production should express these authorities as an evidence dependency ledger:
each durable proof names its immutable inputs, output digest, verifier version
and invalidation scope. Commands consume the same proof authority; they must
not independently invent stricter or weaker versions of it. Invalidate only
proofs dependent on changed code/metadata, chain, custody, policy, intent or
economic observations, preserving unrelated finalized work. Qualify the full
planning → reviewed successor → pre-apply readiness → partial apply → resume
sequence through a compatible runtime update and changed recovery executable,
including missing/altered archive bytes and a journal that advanced outside the
repair. This simulator correction is a regression pattern for RT-08, not proof
that production consumers already implement the ledger.

### PH-05 — Reusable proofs with explicit invalidation

**Lesson.** Executable, release and plan changes invalidated otherwise identical
historical proofs. `67c614f4` added dependency-bound reuse for exactly two fleet
proof kinds in [historical_audit_descendant_cache.go](../sim-testnet/historical_audit_descendant_cache.go).
Earlier fixes indexed journals and authenticated source plans once per
reconciliation; they did not authorize reuse of arbitrary current state.

**Production change.** Cache completed immutable proof units by full consumed
input: chain/checkpoint, action/receipt, target/calldata, expected decoder result,
verifier version, authority/observer profile and authenticated approval lineage.
Store provenance inside the authenticated envelope, write atomically, and
invalidate only changed dependencies. Recheck canonical/finalized identity and
local evidence as required. Re-read live nonce, balance, permit, fee, reserve
and lease observations when their operation needs them. Indexes are lookup
hints, not authority. Retain successful groups when a later group fails; do
not cache transient failures as successful decisions or share failed singleflight
results indefinitely. Legacy opaque entries lacking provenance need their exact
original context or one fresh validation; they cannot be guessed compatible.

**Closure.** A compatible hotfix and process restart perform zero repeated
immutable value calls while still making required freshness checks. Changed
calldata, code/decoder, verifier, policy, observer, signature, receipt or lineage
must invalidate the affected proof. Cover partial two-observer completion,
interruption, tampering, reorg, read-only mode, concurrent consumers and legacy
entry migration. Measure work counts, not a convenient warm-cache runtime.

**2026-09-22 path-proof follow-up.** The first scenario observation after a
driver replacement reverified roughly 550 MB of validator path proofs because
its prefix cache survived only in memory. The
[durable prefix store](../sim-testnet/scenario_path_proof_store.go) authenticates
each complete-record byte cut, SHA-256, verifier/key identity, count and unique
trail census. It checkpoints successful chunks even when a later record fails,
rehashes the source before reuse, and verifies only the appended suffix. A
changed verifier requires full validation; a changed trusted prefix remains an
integrity failure. Production consumers also need fixed snapshot cuts, bounded
line allocation, read-only cache access and atomic publication without letting
concurrent appends extend one observation indefinitely. Final semantic evidence
continues to authenticate the original proof records independently of this cache.

**2026-09-22 recovery-plan follow-up.** A read-only CPU profile found that cold
recovery validation decoded and rehashed the same large archived plans for each
signed generation, even though envelope reads had their own lookup. Share one
[authenticated plan lookup](../sim-testnet/campaign_plan_lookup.go) across root
succession, signed envelopes, approval edges and source reconstruction. Preserve
the exact raw-byte digest, all lineage and custody checks, and a bounded retained
size. Fence each reuse and the final return with directory/file identity and
change-time witnesses; replacement, truncation and same-size writes must fail.
Unavailable metadata or an exhausted memory budget requires the full reader.
Emit progress after each authenticated generation. Production qualification must
count full decodes per distinct approval and force mutations during validation,
so a warm envelope cache cannot conceal repeated work in adjacent readers.

### PH-06 — Release, plan and rendered configuration identity

**Lesson.** Publishing reports invalidated a qualified executable (RL-01),
budget/runtime revisions lost retained custody, and old render receipts were
treated as proof of new configuration. The
[render-convergence failure](../sim-testnet/peerreview/evidence/FINAL-2-render-convergence-20260914/README.md)
also exposed a direct plain-WebSocket route that violated the server's
transport policy.

**Production change.** Approve an immutable release manifest covering executable,
source/dependencies, contract artifacts, schema, policy and security inputs.
Distinguish it from the current branch tip and reporting files. A successor
plan records its predecessor and exact future-action diff, reuses completed
compatible actions and preserves original signatures, limits and custody.
Version rendered configuration by the fields it consumes: route, authority,
schema, service profile and identity. Converge changed local outputs explicitly;
do not overwrite an old receipt or mutate a running service's signed context.
Allow address/path relocation only when its actual identity and security
implications are reconciled. Retain transport authentication requirements.

**Closure.** Cover documentation-only commits, budget-only revisions, approved
runtime transitions, path relocation, stopped/running services, lost render
output, changed endpoint and unauthorized release drift. Unaffected progress
survives; changed executable, policy, contracts or authority cannot borrow an
unrelated approval. Exercise restart on the admitted release while main advances.

### PH-07 — Process ownership, dependency recovery and readiness

**Lesson.** A taskworker log finding stopped all 33 processes; historical replay
consumed the five-minute readiness budget; replaced executable paths prevented
graceful shutdown; Docker restarts stranded dependencies. PF-02/PF-04 and
`6105e22e` ([dependency recovery](../sim-testnet/supervisor_dependency_recovery.go))
address parts of these failures.

**Production change.** Supervise each long-lived service and its dependencies
with explicit ownership, stable process identity and bounded restart/backoff.
Use PID start identity, executable identity and owned process group/cgroup;
a pathname or stale lock alone cannot prove a process is alive or safe to kill.
Dependency recovery must preserve volumes, identities and deployment state.
Keep one writer across restart and handoff. Separate liveness, replay/warmup,
semantic readiness and acceptance health. Retain healthy workers when one
recovers; wait for a canceled owner's children before replacement. A saturated
restart budget surfaces an actionable degraded state, not a green endpoint.

**Closure.** Kill an operator or validator independently, restart an owned
database/object-store container, lose the observer connection, rotate the
executable path and simulate PID reuse. Verify no duplicate signer/sidecar,
no orphan process, no unexpected volume recreation and joined shutdown.
Delayed replay must expose progress; both UR validators must eventually produce
fresh verified trails through every required operator. The root validator's
readiness is its own netuid-0 role, never a substitute for a second UR validator.

### PH-08 — Bound replay and background workload

**Lesson.** Journal validation became repeated full scans; fleet history repeated
source authentication; whole-fleet fixtures performed unnecessary durable
writes; a path-proof reader followed a growing file indefinitely. Evidence:
[44,048-row journal regression](../sim-testnet/peerreview/evidence/FINAL-2-journal-recovery-20260916/README.md),
`91274acc`, `cbf15c3b`, `2c968635` and `74192404`.

**Production change.** Index each consistent journal snapshot once, authenticate
each distinct historical plan once per reconciliation, and bound reads by the
snapshot's initial length. Append-only growth is processed in a later segment.
Use bounded queues, byte/memory limits, RPC concurrency and cancellation-aware
joins. Keep background audits from starving signing, proofs, settlement and
claims. Give subnet operator taskworkers an explicit workload profile; filter
unrelated retained queue rows before claim limits and preserve their post hooks
for the correct worker. Preserve ordinary production defaults. Measure actual
provider/session memory and honor configured capacity instead of hiding leaks
with higher limits or disabling admission.

**Closure.** Assert linear/bounded operation counts under a representative
retained journal and fleet. Force concurrent append, cold cache, queue pressure,
slow archive, provider memory pressure and both operator workloads. Required
foreground work must progress within its deadline, excluded tasks remain
untouched, and cancellation joins without leaked buffers or goroutines. Fixture
optimizations retain at least one representative full integration path.

### PH-09 — Durable storage and usable test/build storage

**Lesson.** Root-volume pressure and scratch/cache placement delayed or stopped
qualification. `a5c23b39` and `2f9ef2b3` introduced data-volume workspaces and
the [storage adapter](../scripts/test-storage.sh).

**Production change.** Configure durable journal/database/artifact storage
separately from disposable scratch and build caches. Check the intended mount,
permissions, free bytes/inodes and I/O health; a missing mount must not silently
write to the root filesystem. Retain temporary/private directory permissions
without changing published artifact modes. Publish state with file fsync,
atomic rename and directory durability; preserve a verifiable prior version.
Back up signed journals, keys and evidence with separate access policies and
test restoration. Relocate old data only with ownership checks and preserved
live paths; paths themselves are not cryptographic identity. Production volume
selection is deployment configuration, not a hardcoded testnet USB path.

**Closure.** Exercise missing mount, read-only/full volume, inode exhaustion,
partial write, crash before/after rename, cache loss and restored backups.
Recover the last authenticated checkpoint without losing a signed attempt or
marking incomplete publication complete. Release/test entry points propagate
selected scratch/cache paths to children and remain usable in isolated CI.

### PH-10 — Epoch boundaries, leases and partial renewal

**Lesson.** Expiring preparation windows repeatedly triggered renewal; forecast
end blocks were confused with minimum waiting periods; compacted and partially
activated fleet generations failed replay. `ed768df3`, `aedb4e74` and
[fleet_renewal_deadline.go](../sim-testnet/fleet_renewal_deadline.go) cover recent
recovery boundaries.

**Production change.** Model policy activation, evidence capacity horizon,
native epoch, settlement epoch, lease validity and claim expiry separately.
Choose fresh execution boundaries after slow preparation/import; derive them
from finalized chain state. Preserve finalized children of a renewal and
reconcile installed, pending, effective, expired and superseded generations
before signing remaining work. Recover compacted history through authenticated
witnesses. Parallelize independent fleet work within nonce/resource ownership.
Keep retention capacity sufficient for startup margin and the full required
window; a forecast end is not a reason to wait until that block to start.

**Concurrent signer follow-up (2026-09-22).** A fleet renewal repeatedly refused
approval because independent root publishers advanced their nonces. Admission
must bind exact nonce state to the transaction owners of the repair, then
reconcile bounded progress of other signers without changing custody, signed
liabilities or the approval hash. Unconfirmed observations may settle or leave
the pool; finalized history may not regress. The simulator now applies this
distinction to renewal checkpoints. Production closure also requires one owner
per actual signing stream and recovery of persisted signed bytes before any
retry; it does not permit silently changing an approved transaction nonce.

**Closure.** Move the finalized head across activation while part of a fleet is
renewed; interrupt and compact midway; delay import past a planned boundary.
Resume without double renewal, lost original lease proof or unauthorized fresh
funding. Assert that acceptance counts actual complete policy epochs and that
claim/commit/reveal deadlines are never inferred from stale wall-clock ETA.
Advance an unrelated signer between approval and apply, then prove the same
approval succeeds without signing or broadcasting twice. Keep changed renewal
signers, custody, liabilities, missing roles and unbounded observations hard.

Also cross the receipt/pool publication boundaries deterministically: finalize
the original transaction between its first receipt lookup and nonce read, lose
an accepted submission response, and delay the preceding pipeline nonce in the
pool. Reconcile the exact hash and persisted bytes under finite read/broadcast
budgets. Missing receipt plus advanced nonce is unresolved observation until
canonical evidence identifies the winning transaction; it is not proof that a
different transaction won. Never sign a replacement nonce to clear that gap.

### PH-11 — Budgets, reserve targets and native funding behavior

**Lesson.** Software changes retriggered a 65% reserve repair despite a valid
prior repair and a live share above the 60% operating floor. Lifetime increases
inflated future campaign allocations (`eceac4aa`), and successor renewals needed
funding reconciliation (`9b874e34`, `19b400ac`). The probe incorrectly transferred
value to a precompile whose staking path debited the caller's native balance
(`e604a8d1`). See the [reserve refusal](../sim-testnet/peerreview/evidence/FINAL-2-release-reserve-recovery-20260916/README.md).

**Production change.** Keep one cumulative ledger across releases and plan
lineage: paid fees/principal, signed outstanding liabilities, reservations,
replacements and remaining authorization. Distinguish EVM wei, TAO rao and
alpha units with checked integer arithmetic. Raising a lifetime ceiling does
not automatically expand each action allocation. Separate the live operating
floor, a repair target at its pinned execution block and any required terminal
target; preserve successful repairs while checking the current floor. Determine
transfer/stake source, value semantics, fees and actual credited amount from
the admitted precompile/runtime behavior, including dual native/EVM views of
one account. Do not assume a successful outer call funded the intended party.

**Closure.** Prove conservation through repeated budget revisions, partial
repairs, superseded signatures, nonce cancellation and renewal successors.
Exercise reserve rounding just below the target, below the floor, insufficient
native balance despite EVM balance, and precompile revert/partial behavior.
Only actual finalized balance/event/postcondition evidence releases liability.
Testnet automatic allowance approval is not a production spending policy;
mainnet uses its own explicitly configured limits, signers and custody rules.

### PH-12 — Settlement, carry and claims remain explainable end to end

**Lesson.** Epoch 309 captured zero but paid carried epoch-308 funds; the first
report omitted `RootMissed(308)`. All 16 payments and 8 alpha-rao of rounding
residue reproduced independently. Shared operator JWTs initially selected the
wrong provider wallet. The first report also records a NetEscrow cross-store
ordering race as a production limitation; detection is not evidence of a fix.
See [peer-review conformance facts](../sim-testnet/peerreview/verify/content.py)
and [claim receipts](../sim-testnet/peerreview/evidence/epoch309-paid-claims-20260912.json).

**Production change.** Trace captured emission, per-operator carry, entitlement,
Merkle root, claim, payment, outstanding liability and residue with exact units
and epoch identities. Use provider-specific authorization for claims and verify
the entitled client independently of a shared network credential. Preserve
zero-entitlement, deferred-payment, missed-root and expired-claim outcomes as
different states. Authenticate the coordinator-authorized commitment and its
artifact hash; recovering an artifact signer is not proof that signer had the
on-chain root role. Review and resolve the known cross-store ordering defect
with its actual production owner, or retain it as an explicit launch blocker;
monitor alerts alone do not close it. Preserve non-upgradeable custody and
already finalized claims across coordinator changes.

**Closure.** Reproduce missing roots followed by carry into the next epoch,
cross-operator isolation, floor division/dust, retry after payment uncertainty,
claim expiry and mismatched provider credentials. Force the NetEscrow ordering
race at the observable store boundary and prove repaired state convergence.
Independently rebuild leaves/root and every payment amount; vault conservation
must hold at each pinned transition, including zero-current-capture payments.

### PH-13 — Protocol identity and proof/traffic continuity

**Lesson.** Stale measurement cuts, skipped settlement rounds, client-key
history deadlines and terminal-publication failures repeatedly stopped
validators while other services continued. Actual transport ACK volume did
not by itself establish eligible usage or a payable root; the shortened run
contained both positive settlement and a separate zero-usage epoch.

**Production change.** Bind every evidence, client-key history and publication
path to its operator, provider, validator, generation and epoch domain. Reconcile
late/stale messages against their own lineage before changing current state.
Resume verified history without fabricating missed measurements or applied
weights. Persist publication progress and retry content-addressed writes.
Preserve canonical serialized bytes, including signed framing/newline rules.
Use bounded flow control with clear buffer/goroutine ownership and cancellation
through SDK/operator/provider boundaries. Keep traffic ownership/accounting
durable while controllers or observers restart. Retain the safety differences
between testnet provisional gap handling and admissible mainnet history.

**Closure.** Inject stale generations, delayed proofs, mixed operator keys,
skipped rounds, partial artifact uploads, backpressure and controller restarts.
Test both normal and replay/fast paths at the layer where identity is consumed.
Require fresh proof progress for every validator/operator domain and connect
traffic to eligible usage, signed roots, native rows and paid entitlement;
bytes acknowledged or a healthy process alone cannot satisfy that chain.

### PH-14 — Governed limits and real on-chain activation

**Lesson.** The first run configured a future production policy but never
scheduled it: on-chain cadence stayed 300/50/150/5 instead of 360/60/180/6.
`max_allowed_validators=64` exceeded the design target of at most 56, and reserve
was 61.449% against its 65% target. These are the peer review's three explicit
findings, not arithmetic/test errors to suppress.

**Production change.** Make policy transitions durable scheduled actions and
prove their effective chain state. Read actual limits and authority; expose
root/governance-only changes and adapt the design to supported constraints.
A retained 64-validator exception must state its capacity consequence rather
than pretend the target was met. Follow MAINNET.md for literal UID-reset
capability, protected identities, actual contract/custody installation,
**10% of native miner allocation**, disposition of the remainder and **both
the netuid-0 root and UR subnet validator roles**. Scaling all weights or theta
alone cannot implement the 10% requirement. Recheck role/permit/registration
eligibility and operating reserve at execution, using approved semantics.

**Closure.** Measure the activated cadence across the required three consecutive
fully observed production epochs; distinguish it from configured intent.
Verify hyperparameters and reserve at pinned blocks and report unresolved
exceptions plainly. Mainnet activation additionally proves the chosen reset
capability, 10% denominator/rounding, actual reward outcome and both validator
roles. Test stale plans, unauthorized calls, competing registrations and policy
activation races without silently substituting a narrower reset or reward goal.

### PH-17 — Bind derived indexes to the exact plan and generation

**Lesson.** During archived-plan recovery, executor copies could retain an
action index built for the current plan. A lookup after the copy switched to a
historical plan could therefore return an action authorized by the wrong plan.
The sim-testnet correction in `701f4456` makes the index owner explicit and
falls back to the copied plan when it differs; its causal control returned a
synthetic current-plan target under the prior implementation.

**Production change.** Every derived index, cache, iterator, batched-work map
and dependency resolver must carry the immutable plan hash and generation that
created it. At each read, verify object identity as well as content shape. A
copied/recovered executor must either reuse an index owned by its exact plan or
rebuild from its own authenticated source. Treat an index as an acceleration
only: it cannot supply authority, dependency order, approval scope or a target
that the bound immutable plan does not contain.

**Closure.** Clone each production reader across current, archived, successor,
cancelled and repaired plans; poison the original index and prove the clone
selects only the target/dependencies from its own plan. Cover concurrent index
publication, restart, eviction and plan migration. Include this in PH-05 proof
reuse and PH-06 migration qualification, with normal and race tests at every
consumer boundary.

### PH-18 — Re-authorize strict readers after provisional work

**Lesson.** A connection that was acceptable for provisional recovery could
otherwise retain its compatibility authority when later reused by a strict
reader. `701f4456` added an explicit strict-after-provisional fence and a
causal regression; connection reuse alone does not establish that the strict
runtime catalogue, metadata and capability decision were rechecked.

**Production change.** Model connection transport, observed chain state,
runtime/metadata catalogue, verification mode and approval lineage as separate
capabilities. Every strict reader must request and validate a fresh strict
capability at its own boundary, including after connection pooling, process
restart, runtime update, handoff and provisional repair. A provisional result
can be retained as labeled evidence but cannot populate a strict cache or
authorize strict historical decoding, signing, settlement, governance or final
acceptance. Invalidate/re-observe the relevant identity whenever the pinned
block, runtime, endpoint/peer, decoder or policy changes.

**Closure.** Reuse one pooled connection across provisional and strict readers,
then inject a changed runtime catalogue, metadata hash, peer identity and
unsupported capability. Prove strict work rejects the provisional authority,
performs its own pinned observation and leaves unrelated provisional traffic
running. Exercise both validators, miner/operator clients, bootstrap and
archive replay under normal and race qualification.

### PH-15 — Operational status that explains forward progress

**Lesson.** Repeated "hours remaining" estimates obscured whether the runtime
was producing transactions, replaying old evidence or waiting for a future
boundary. An observer timeout was also easy to confuse with a stopped owner.

**Production change.** Publish per-component owner identity, last successful
checkpoint/block, current action, attempts, retry-after, queue/backlog, proof
domains and blocking dependency. Use explicit classes: retryable transport,
deferred audit, pending finality, recoverable service, integrity/authorization
failure, accounting failure and acceptance failure. Emit one durable incident
with recurrence counters, retaining original errors and resolution evidence.
Expose real submitted/finalized transaction counts and workload/proof progress.
ETA separates observed preparation throughput, chain-block duration and unknown
repair time; update it from finalized block progress and measured cadence.

**Closure.** During injected outage and live recovery, status must identify the
same surviving owner, its pending operation and next retry. No live-process
claim comes solely from a lock/state file. A completed soft-error recovery
remains in the incident ledger for the improvement batch; missing required
evidence remains visible in acceptance. Verify meaningful signals under both
slow but progressing replay and an actual deadlock.

### PH-16 — Deterministic qualification and reviewable evidence

**Lesson.** Some prior failures were real production defects; others were
incorrect selectors, working directories, fixture assumptions, stale generated
artifacts, missing offline dependencies or observer/capture failures. Repeated
full gates and confirmation runs did not isolate those causes. The handoff's
`[no tests to run]` example and the retained failed bundles must stay distinguishable
from passes.

**Production change.** Follow [CODESTYLE.md](../../connect/CODESTYLE.md): each
root cause needs a deterministic pre-fix failure and corrected result at its
observable layer, using barriers/hooks/state transitions instead of scheduler
luck. Inspect similar callers, alternate/replay/batch paths and adjacent failure
classes. Use synthetic identities and bounded fixtures; keep live custody and
private captures out of tests. Freeze each job's actual inputs, enumerate
selected roots, require nonzero expected membership and record test/build/body
and cleanup outcomes. Run normal/race modes where relevant. Repair the failed
scope and reuse demonstrably unaffected results; repeat only for a named
unresolved timing concern. Rerun the representative failed integration when a
small test cannot establish the workload/resource fix.

Maintain a requirement-to-evidence table for the composed release: original
failure, root cause, patch and adjacent paths, exact source/dependency/toolchain
inputs, causal test, normal/race results, reused scopes, deployment and operational
proof, unresolved work. Keep producer/aggregate requirements and complete live
acceptance in that table without turning each patch into another full restart.
Hash and retain raw receipts and numbered reports; preserve failed/canceled
attempts, not overwritten summaries.

Use the [independent verifier](../sim-testnet/peerreview/verify/README.md) as a
reproduction model: rebuild Merkle roots and signatures independently, pin
native/EVM mapping, decode transactions/events/storage, and declare archive
requirements. Parameterize new run/deployment inputs rather than editing old
expected findings into passes. Distinguish on-chain proof, authenticated artifact
content and off-chain operational assertions. The current testnet policy uses
only the owned LAN RPC and must say `independent_rpc=false`; running an
independent implementation against that node does not create an independent
observer. Preserve the first report's separate public-node comparison with its
original scope. A production independent observer, when provisioned, must have
its own declared endpoint, chain identity and observed checkpoints.

**Closure.** Terra's affected qualifications plus controlled production-path
fault injection must prove the corresponding PH requirements. The final
exercise combines compatible runtime upgrade, interrupted submission, temporary
network loss, service/dependency restart and replay/cache reuse while retaining
financial history. Then acquire the required complete acceptance interval and
verify accounting, policy, both validator duties and graceful shutdown. A clean
test log, peer review of an earlier run, or report publication alone does not
complete mainnet readiness.

### PH-19 — Retained snapshots use historical runtime authority

**Lesson.** The 2026-09-21 provisional resume reached a retained relay
continuation recorded at reviewed runtime `node-subtensor/461/1/1`. Its reader
misclassified that immutable block as a current snapshot and applied the
current-only compatibility fallback, rejecting it before campaign activation.
The current finalized node was runtime 468; no current signing authority was
missing. The correction is SN `d3bbc8f5` and its local
Terra qualification record is `/mnt/data/sn-testnet/qualification/terra-runtime-461-20260921/`:
seven targeted roots pass normally and under race, while the old classification
reproduces the exact 461 rejection.

**Production change.** Give every native read an explicit purpose: immutable
activation/continuation history, newly selected finalized snapshot, or current
head/signing. Historical reads may use only the exact reviewed artifact for
their pinned block; they must not inherit a current-runtime requirement.
New snapshots, writes and signing retain current capability and approval
checks. Imported continuation pins must be canonical and finalized before they
are classified as history. No historical compatibility result may authorize a
new action.

**Closure.** Exercise historical continuation and activation snapshots across
compatible upgrades, including imported pins, changed metadata/code, noncanonical
hashes, cancelled reads, changed hotkeys, stake and permit. Prove a current
read and signing operation still reject the historical artifact. Cover both
validators, miner, bootstrap, settlement and archive readers in normal and
race qualification. This is an implementation input to RT-01, RT-02, RT-05,
RT-06 and PH-18; it is not complete for mainnet merely because the simulator
correction passed.

### PH-20 — Capacity accounting separates approved slots from scan pages

**Lesson.** Immediately after PH-19 passed in the same 2026-09-21 resume, the
relay startup inventory stopped before historical reads with `observed manifest
slots exceed 1024`. Read-only census found 271 closed manifests for each of two
validators with two members per manifest, plus three validator-2 audits: 1,090
prospective member slots. The retained continuation authorizes only
`new_slots=1024` and has no approved journal debits. The fixed 1,024-entry
scanner ceiling exposed the excess early, but merely raising it would later
admit unapproved work and is unsafe. No transaction or journal entry was added
by this failure. Investigation is tracing which entries are historical versus
eligible new work; the correction is active in the sim-testnet run. On 2026-09-21
we selected an explicit 2,048-slot continuation allowance: a 2x margin over
the measured backlog. At the existing 1,000,000-gas / 25-gwei cap it binds
51.2 EVM TAO total, a 25.6 EVM TAO increase over 1,024 slots. It must be a
newly bound finite resource/spend revision, not a scanner-default change. The
testnet revision raises both lifetime EVM and total TAO ceilings to 512. The
2,048-slot relay reserve remains exactly 51.2 EVM TAO; the remaining ceiling is
headroom, not authorized relay spend. Its keeper top-up and relay-reserve
allocation are reconciled exactly once.

**Production change.** Represent separately: (1) immutable aggregate approved
slot/spend capacity, (2) source/member slot cost, (3) historical/previously
admitted evidence, (4) bounded directory/page read size, and (5) bounded
resident memory/byte budget. Enumerate large retained histories in authenticated
pages with a stable snapshot cut. Reconcile every candidate to a retained,
exactly approved slot before it can consume send authority; aggregate genuinely
new work against the approved slot capacity using checked arithmetic. The
selected testnet 2,048-slot allowance is an explicit revision with exact gas,
fee and aggregate-spend bounds; production derives its own approved allowance
from a census plus reviewed margin. Retain only bounded witnesses or streamed
verification state. A malformed directory,
unapproved candidate, changed scan cut, ownership escape, byte violation or
gap/duplicate fails precisely. Do not solve this by lifting a global constant
or silently increasing the approved spend.

**Closure.** Add deterministic pre-fix and fixed tests for exactly-full and
one-over aggregate new-work capacity; the exact selected 2,048 allowance;
more-than-one-page retained history;
per-source/member multiplication; retained-versus-new classification; changed
directory during scan; duplicate and missing pages; cancellation/restart;
imported continuation; malformed entry and byte exhaustion. Run normal and race tests at the startup, continuation,
archive-replay and final-acceptance consumers. A controlled production-path
rehearsal must resume a large authenticated history without redoing completed
work, while refusing unapproved extra work. Link the completed sim-testnet
fix, its Terra evidence and actual resume record before marking PH-20 done.

### Generation-25 follow-up — fault, transport, capacity and recovery hardening

Generation 25 started its acceptance interval at testnet block `8,062,774` on
2026-09-22 and produced a complete terminal evidence bundle. It did not
complete acceptance. The direct terminal error was `disable miner-848: context
deadline exceeded` while applying the 96-member quality cohort. The fault
controller had retained per-member intent and completed work, but dispatched
members serially while holding the campaign callback; a transient local timeout
therefore consumed the remaining fault window. The result also recorded
acceptance-scope TLS handshake timeouts and adversary artifact/API GET
deadlines. The 41 unexercised later faults are explicitly derived from this
interruption, rather than separate production defects. Evidence is retained in
the generation-25 `faults.json`, `process-logs.json`, `anomalies.json`,
`assertions.json` and `result.json` under `sim-testnet/runs`.

**PH-21 — Fault controller.** Persist an idempotent intent and completion
record for every independently controlled member. Dispatch independent service
or swarm controls with a bounded concurrency limit, never one unbounded serial
loop. On a transient timeout, first read and reconcile the member's actual
state, then retry only that pending member with bounded backoff; an already
applied disable or restore is success. Record trigger, first-dispatch,
per-member completion, effective cohort completion and restore boundaries
separately. A temporary control-plane timeout must not erase the durable
completed prefix or require a whole campaign restart. Invalid identities,
conflicting state and exhausted retries remain explicit failures.

The September 22 release also exposed a disagreement between these layers:
the control driver returned a legitimate partial round, but the signed campaign
validator required pending faults to have no process census or diagnostic. Its
rejection canceled observation before the next full snapshot. Both applying and
restoring retries must have explicit checkpoint semantics, with a canonical
first-dispatch boundary, monotonic retry count, exact target census and separate
completed-transition block. Preserve those diagnostics through checkpoint
signing and reopening; incomplete work must neither stop observation nor count
as a completed acceptance fault. Test the complete driver/controller/checkpoint
path together, including a pending heartbeat while a snapshot is still running.

Bounded rounds must also make progress across their completed prefix. Re-reading
every completed member at each ten-second boundary can starve a large batch
indefinitely under load. Retain verified member progress for one in-process
fault/action and owning worker generation, only after the live reconciliation
and durable completion write succeed. Reopen, parent cancellation, hard failure,
worker replacement or the opposite action must require fresh reconciliation;
an old completion file alone is never authority. Test multiple constrained
rounds, restored-state drift, same-PID worker replacement and mixed
cancellation/integrity failures. Keep all acceptance-window and minimum fault
duration checks unchanged.

The September 23 interval exposed a second starvation path: a shared ten-second
round deadline started before its generation census and serial intent fsyncs.
Disk pressure consumed the budget before requests were dispatched, then an
outer deadline was signed as a terminally failed cohort. Request deadlines must
start at actual dispatch. Bound request counts and simultaneous members instead
of charging storage admission to an HTTP timeout. Commit exact pending-target
and attempt intents in bounded batches, and batch completion checkpoints before
admitting later mutations. Keep a sole persistence owner and join every worker.
Temporary storage failures and outer deadlines retain applying/restoring intent;
they never certify completion or backdate the fault. Test a 96-target cohort
with simulated flushes longer than the old round deadline, crashes before and
after rename, no mutation before a failed intent flush, and restart reconciliation
without duplicate side effects. Permission, schema, custody and joined integrity
failures remain hard.

Control admission also needs a bounded readiness state for a checksum-bound
swarm that is temporarily unhealthy or between process generations. Wait for
that same owner before first dispatch, preserve an existing completed prefix,
and re-read its live member state after replacement. A PID change between the
admission read and the control round must defer that round before any request;
it must not cancel the campaign. Missing owners, changed identities, invalid
state and checksum failures remain hard. Tests must force first-dispatch,
partial-prefix and restore restart windows, cancellation, generation turnover,
and a mixed readiness/identity failure without sleeps.

Preserve every semantic failure when its diagnostic checkpoint also fails.
Classify each joined cause; a malformed or foreign status remains hard even
beside a retryable disk error or cancellation. Restoration cleanup has its own
durability boundary: retain the exact completed census before removing active
intent. A failed unlink/rename sync must resume from that checkpoint and observe
each member again, including after process restart; a retained completion alone
does not prove current state. Test both already restored and newly changed
members, partial or substituted checkpoints, and missing recovery evidence.

**PH-22 — Transport recovery and final signal.** Treat connect/read deadlines,
EOF/reset and HTTP `429`, `502`, `503` and `504` as bounded retry candidates
only for idempotent reads or controls with a retained idempotency key. Reuse the
same request identity, reconcile an uncertain outcome, record attempts and
backoff, and preserve cancellation as cancellation rather than retrying it.
Invalid JSON, identity/hash/signature mismatch and semantic API refusal remain
hard failures. Transport clients must repair TLS connections and report health
recovery; a correlated TLS incident remains visible and must be absent from the
final acceptance interval. Adversary probes may continue after a recovered
transient read, but final acceptance evaluates the persistent exhausted-retry
error budget rather than the first timeout.

The retained publication review also found that stream upload/read transports
discarded HTTP status into error text. A protected-quota `429` then consumed the
native failure budget instead of waiting for its hourly reset. Preserve typed
status and bounded server pacing through every wrapping and replica join;
authentication, conflicting content and mixed integrity failures remain hard
even when response text contains a transport-looking phrase. The transport
performs one immutable request; its existing lifecycle owner retries. Startup
honors a single positive integer `Retry-After`, bounded to one hour, within its
existing finite attempt count and cancellation scope. Provisional native
collection retains its cut across ordinary retry polls; strict final acceptance
keeps its original failure budget. Deterministic transport, mixed-cause, reset,
cancellation and strict/provisional tests cover this correction; production
closure still requires exercising actual quota exhaustion and recovery.

Apply the same ownership rule above the relay's individual reads. Its runtime
previously stopped the complete campaign when one closed-publication or deposit
audit step returned a transient error after lower-level recovery. Give each step
a finite operation retry budget, record the failure before retry, and re-enter
the existing exact signed-transaction reconciliation path; an accepted send
with a lost response must resolve to its original winner without a new nonce or
duplicate send. The independent historical census retains partial checkpoints
and retries under the same transport classification without canceling live
traffic. Preserve every independent integrity error, cancellation and terminal
exhaustion. Retry diagnostics remain durable under `evidence-relay-retries/`;
they confer no acceptance authority. Closure requires actual uncertain-send
reconciliation, mixed-failure, exhaustion and audit/runtime isolation tests.

**PH-23 — Funded capacity and physical resource profile.** A capacity revision
must bind four different facts: funded slot/spend allowance, source-history
horizon, upload quotas and finite archive metadata limits. Generation 25 found
that setting 2,048 slots while leaving a 2 GiB metadata document limit would
make the stated workload impossible. The successor profile therefore needs an
explicit source horizon and finite, non-preallocated typed-document, retained
metadata and supplemental-metadata ceilings with at least the reviewed 2x
margin. It must carry an authenticated predecessor reserve exactly when no new
spend is intended; it must never reconstruct fresh economics from the new slot
count. Admission rejects a requested profile that does not fit every bound.

Full fleet-renewal approvals crossed the ordinary proof-file limit: a compact
generated 35 MiB plan could not be imported by its own command. Output, import,
active/runtime reload, immutable archive and historical owner lookup now share
a separate 128 MiB approval bound while ordinary proofs remain at 32 MiB.
Production must qualify each producer-to-consumer path at the selected size,
including closed capture and public replica replay, before declaring the
profile usable. Preserve exact approval hashes, no-follow regular-file reads,
aggregate archive charges and independent cache memory limits. A valid plan
larger than an optional cache must bypass caching, never exhaust an eviction
queue or acquire unbounded retained memory.

The adjacent closure paths needed the same correction: capture bundles,
derived validator plans, fleet lineage, public signing/readback and completed
prior-phase carriers each had a different smaller limit. Use exact producer
paths and schemas to select capacity, retain separate plan/ordinary counters,
and clip their combined use to the configured grant. Capture only the approved
ancestor hashes, not unrelated reviews found in the archive directory. A public
blob write is incomplete until the actual API GET and exact-hash history routes
can authenticate and return it. Keep ordinary upload/proof limits unchanged;
test a generated large plan through capture, signed transport and replay,
alongside invalid aliases, one-byte overages and independent counter exhaustion.

Keep whole-source catch-up forecasts separate from live quota consumption. The
retained source forecast charges all source history and admitted refresh/retry
operations to one hourly bucket; it can exceed a retained deployment's limits
before any actual counter is exhausted. Testnet provisional continuation may
record this forecast as advisory with `final_acceptance=false`, but must preserve
every enforced object, byte and retry counter and deployment/replica owner.
Adopt larger production quotas only through an authenticated config/manifest
successor, with at least 2x all forecast dimensions; do not edit bound retained
configuration or waive a real quota to clear a forecast warning. Record actual
counter usage, resets and recovered throttles so final admission can distinguish
an oversized catch-up estimate from sustained insufficient capacity.

**PH-24 — Recovery-lineage work.** Generation 25 authenticated 24 retained
generations before it could publish its recovery record. The reader repeatedly
decoded and hashed the same archived plans even though the lineage already had
an immutable per-invocation lookup boundary. Cache each fully authenticated
plan by its raw digest, filesystem/source witness and lineage owner; retain
per-edge source and ordering checks on every reuse. Bound the cache, log
generation progress, and fall back to cold authentication after an immutable
source change. A cache must not bridge plans, authorities, generations or
changed bytes.

Runtime rendering exposed the same duplication inside one operation: nested
evidence, staging and manifest readers each revalidated the complete active
plan. [The scoped reader](../sim-testnet/runtime_plan_read_scope.go) retains one
successful proof for exact source bytes, state root, configuration and private
route/assurance fields. Every use still acquires and hashes the bounded source;
each caller receives its own decoded plan. Production qualification should
count full validations per render, then replace/truncate/symlink source files,
change authority and mutate returned values. No failed validation is reusable.

**PH-25 — Deployment workload ownership.** A terminal campaign and its
deployment have distinct lifecycles. A terminal scenario may retain the exact
healthy supervisor, claim relayers, miners, validators, proxies and supporting
services for a successor; it must not silently repurpose them for another
deployment. Explicit deployment stop must retain immutable evidence and the
durable restart/continuation record, then cancel and join every owned process
group before reporting shutdown. Generation 25 confirmed that explicit stop
removed its supervisor and children. Never infer either continuation or cleanup
from a dead parent while a recorded child process group remains live.

**Closure for PH-21 through PH-25.** Add deterministic tests for partial cohort
completion, timeout then state reconciliation, restart from a durable prefix,
already-applied members, bounded swarm concurrency, exhausted retry, and no
duplicate disable/restore. Test recovered and exhausted API/TLS reads,
cancellation without retry, and hard semantic/integrity responses. Test funded
successor capacity, one-byte/one-slot/one-object overages, every metadata
dimension and imported predecessor reserve preservation. Test shared retained
plan lookup under source replacement, truncation, symlink substitution,
concurrent mutation and bounded eviction. Run normal and race suites, then a
full final acceptance interval with a clean TLS and transport incident ledger.
Exercise terminal-scenario continuation with a live child workload, then an
explicit deployment stop that proves every owned process group exits while its
evidence and resumable state remain readable.

**PH-26 — Large evidence transport.** The fleet renewal exposed an evidence
shape that was valid under the selected capacity profile but could exceed the
ordinary 64 MiB HTTP GET deadline and body limit during closed capture or public
replay. Production must admit only explicitly typed plan, bundle and lineage
families to their separately reviewed byte limits. After header admission, the
server and client may use a byte-scaled, finite deadline and a bounded
large-response semaphore; ordinary metadata and ordinary HTTP routes retain
their existing deadline and size limits. Parent cancellation must close an
in-flight blob read and join its worker, so a timed-out reader cannot retain a
large buffer or slot. Every response still verifies the exact body digest,
schema, source identity and lineage ordering.

Generic metadata and manifests must not silently inherit the typed-evidence
exception. Before a production profile can produce metadata above the ordinary
transport limit, give that family its own finite transport owner and either a
streaming/reference representation or an independently tested typed admission
path. Deduplicate immutable lineage references rather than embedding the same
ancestry in plan and prior wrappers repeatedly. Qualification covers admitted
large GET, historical replay, server timeout cancellation, client cancellation,
busy admission, malformed headers, digest mismatch and concurrent ordinary
requests; it must prove finite memory, connection and worker usage under race.
An absent or empty optional completion checkpoint means no completed work yet;
it must initialize a durable empty state rather than crash fixture setup or
recovery. Malformed, substituted or conflicting completion records remain hard
failures.

Historical custody checks must retain bounded, authenticated progress across
sample deadlines. Rewalking every prior payout body made all 235 attack samples
exhaust the ten-second read budget while independent artifact checks passed.
The simulator now scopes a hash-to-epoch metadata cache to the complete payout
domain and checksum-bound API process generation, refreshes history membership,
and verifies the selected latest body and finalized vault state every attempt.
Missing or changed process ownership invalidates cache reuse; signatures,
content identity and same-epoch equivocation remain strict. An interrupted sample
is pending evidence and cannot satisfy the final proof gate. Production adoption
must prove interrupted-prefix continuation, source turnover, new equivocation,
latest-body substitution and finite entry counts with deterministic regressions.

A native-cycle custody proof must not monopolize release startup or a separate
journal writer while it waits for blocks. The simulator's explicit provisional
`scenario --name precompile-prepare` executes and authenticates the approved
transaction prefix through its finalized snapshot, then releases the command's
lock. The release's existing writer continues the remaining exact dividend and
transfer actions in bounded observation turns. Each unfinished read remains
pending; a transient read or interrupted transfer retains the verified frontier.
Final conformance still requires a full native window, a positive dividend,
exact conservation and complete recovery to the approved custody destination.
Never label the preparation result as release acceptance. Production adoption
must cover pending observer survival, incomplete or substituted receipt prefixes,
transfer interruption after dividend verification, source identity changes,
parent cancellation, and refusal of incomplete conformance at interval end.

### Closing and maintaining this hardening plan

For each PH item record the implementation/review commit, affected production
consumers, deterministic regression and adjacent review, qualification receipts,
release/deployment, operational evidence, remaining action and accepted
limitations. Mark `Done` only when its closure criteria are proved on the
production path. Simulator-only success remains partial evidence. Link related
RT/RL/PF rows so one completed implementation can satisfy multiple requirements
without duplicate qualification.

Roll out shared recovery/identity interfaces first, followed by independent
consumer migrations and a composed release. Retain the previous authenticated
release and state-format compatibility for roll-forward recovery; any rollback
must reconcile already submitted transactions and preserve finalized economics.
Fault injection may use a controlled integration network, but it must exercise
the production implementations and the actual capability assumptions; mocks
alone do not establish live precompile, governance or economic behavior.
Keep the current sim-testnet finalization moving while these mainnet items are
implemented, promoting only corrections that resolve a concrete active blocker.

### Precompile stake requests versus native share rounding

The conformance harness treated a requested stake amount as both observed balance
changes. A finalized same-subnet move instead debited and credited the same amount
one alpha-rao below its request: the remaining unit stayed at the source. The
reverse path also tried to spend the original request rather than the amount
actually received. Production acceptance must distinguish requested units, observed
source debit, observed destination credit, and any explicitly recorded remainder.

The testnet repair records separate checksum-bound fields for the request minus
source debit (`native_share_residue_rao`) and source debit minus destination
credit (`native_share_credit_rounding_rao`), each bounded to zero or one rao.
A read-only call at the finalized forward receipt reproduced the adjacent reverse
case: an all-balance request clears its source, while its destination's native
share quote credits one fewer integer unit. Both conversions must be explicitly
accounted for; negative deltas, inflation, unrecorded differences and larger
rounding remain hard failures. Requested amount, pre-state, roles, signer, nonce,
chain, contract and transaction remain exact. Reverse calldata uses actual credit.

The shared accounting governs live reconciliation, retained postconditions,
successor receipt replay and final recovery. Finalized transactions resume without
another broadcast. Historical zero-rounding evidence keeps its original canonical
encoding. Round-trip accounting requires returned stake plus explicit native
credit quantization to equal the initial position and requires zero remaining
stake on the intermediate hotkey. Final transfer requires zero probe custody and
an exactly recorded destination credit plus its bounded conversion; rounding is
reported, never silently counted as a recipient payment.

Before mainnet, exercise native share conversion in move and transfer operations,
including even and odd requests, all-balance withdrawals, full custody recovery,
finalized-before-evidence restart, and negative controls for unmatched accounting,
more than one unit at either conversion, changed requests and arithmetic overflow.
Do not propagate this probe rule into payout accounting without independently
specifying and validating that contract's conservation and principal guarantee.

### Stake observations across blocks and residual recovery

The next live reverse move returned its exact approved principal but occurred
579 blocks after the forward move. Both positions had grown in the meantime:
the move hotkey carried 17,306,833 alpha-rao of additional stake and the sample
hotkey carried 21,110,029. Requiring the later pre-state to equal the earlier
post-state rejected a valid round trip before the snapshot transaction. Native
share rounding is a within-call conversion; it must not absorb inter-block
credits or become a broad numeric tolerance.

The harness records those receipt-proven credits and the unrecovered move
position separately. Snapshot preparation can continue while that liability
remains explicit. Snapshot calldata has no amount argument, so its event baseline
is the authoritative inclusion-time output; a positive credit after the pre-send
read does not change the signed intent. Replay still requires the exact hotkey,
receipt, inclusion block and retained baseline. Final acceptance continues to
require recovery of both positions. An exactly recorded pending recovery keeps
provisional observations running and returns before another transaction intent,
while altered evidence and missing files remain hard failures.

The small residual cannot simply be swept: pinned read-only calls showed the
17.32-million-rao residual and requests up to 100 million rao reverting, whereas
500-million-rao and larger funded operations succeeded. Empty revert data does
not establish a specific runtime minimum. Production recovery must check actual
runtime behavior and support a bounded top-up from existing custody before
sweeping a small residual. Each top-up and sweep needs its own authorized action,
durable nonce, exact receipt and recipient accounting. Do not overwrite the
original reverse receipt or claim that it left zero balance.

Before mainnet, cover stake growth between every pair of observations, including
read-to-inclusion and dividend-to-recovery. Account for a recovery top-up's sample
debit when comparing the eventual sample transfer with the earlier dividend
observation. Force interruption at every signing/finalization boundary, residual
growth during a sweep, and repeated recovery that retains completed actions.
The final proof must include both recovered recipient positions, bounded native
conversion residues, and zero source custody. Test forged extra credits, changed
roles/amounts, duplicate spends and missing repair authorization independently.

### Claim queue write amplification and admission budgets

The first live acceptance interval exposed a storage saturation loop: claim
workers reconciled old entries before checking their retry deadline, then
rewrote and fsynced their complete queue for every repeated not-ready result.
Readiness failures did not increment submission attempts, so their backoff never
grew. The two relayers generated roughly 116 MB/s of queue writes and starved
unrelated durable fault controls. The control round's deadline included its
sequential persistence work, leaving healthy local endpoints little or no
request time. Healthy process status alone did not establish useful progress.

The production queue now checks retry admission before API/RPC work, records
reconciliation attempts separately from transaction submissions, and combines
retry diagnostics into one checkpoint per poll. Historical readiness backoff is
bounded at one hour; the newest two epochs and exact uncertain transactions keep
a one-minute cap. Ordinary historical reconciliation has a small per-poll work
budget, with unvisited entries retained, so faster persistence does not create an
API catch-up burst. Current work and uncertain transaction outcomes remain
eligible. This is queue scheduling, not an RPC endpoint rate limit.

Unchanged saves require a successful acknowledgement from this store plus
matching current bytes in a private regular file. A reopened owner or failed
durability boundary must sync again. Submitting intent, prepared signed bytes,
broadcast checkpoints and finalized receipts remain immediately durable; the
diagnostic batch never grants transaction authority or marks an uncertain send
as absent. Deterministic tests cover historical backlog progress, future retry
deadlines, restart/backoff persistence, recent-epoch readiness, exact uncertain
outcomes, failed writes, cancellation, and changed or missing queue files.

Before mainnet, qualify the control scheduler and queue together under slow
durable writes. A network request's attempt budget must begin after required
intent admission; expired queued work must remain resumable without canceling
the observer. Batch intent where safe, keep one durable owner, and require exact
fresh completion evidence before assigning a fault's applied block. Track queue
write bytes, checkpoint latency, remaining historical work and admitted control
requests independently from process health. Production sizing must reserve the
agreed 2x margin without relying on filesystem stalls to throttle useful work.

### Supplemental repair allocation within lifetime caps

The repair proposal later exposed a separate budget boundary: fleet-renewal
liabilities had consumed the local campaign reserve while approved lifetime
headroom remained. Production must distinguish those two limits. A supplemental
repair approval may allocate its exact documented shortfall within both retained
lifetime caps, signed by the budget and custody owners, without replaying setup
or changing its actions. Retain active and superseded spend in that calculation,
round fractional native units upward, and recheck signed/queued exposure before
execution. Tests must reject cap substitution, omitted historical liabilities,
arbitrary extra margin, duplicate charging after restart, and integer overflow.

### Preparation must not acquire a stopped campaign's transport

Standalone precompile preparation reused completed chain evidence but then opened
a campaign executor, forcing its next call through a loopback EVM proxy owned by
a deliberately stopped supervisor. The authenticated command already had working
RPC and transaction managers. Preparation now borrows those exact owners and
retains their authorized route, journal, plan, native connection, payloads and
nonce management; it neither starts topology nor closes the caller's managers.
Owner or route drift remains a hard error. Release scenarios still perform their
separate retained-topology restart and supervised egress handoff. Deterministic
regressions cover stopped proxies, continued use of the original direct client,
foreign journal/plan/route owners, and unchanged release restart scope.

Retained release startup has the converse ownership requirement: its local-only
executor owns approved metadata but deliberately has no native connection to
lend. After topology restart, campaign construction must acquire that missing
reader through the ordinary authenticated constructor. It may do this only for
the exact provisional release plan, journal, directory, configuration and equal
reloaded credentials; partial or foreign connection owners remain errors. A live
parent's native reader stays borrowed. Supervised EVM egress and its readiness
errors remain mandatory, with no direct-route fallback. Test both absent and
existing native ownership, credential reload and drift, canceled construction,
and refusal to bypass a stopped proxy.

Retained process restart must distinguish approval of new work from continuation
of an approved plan. A completed fleet renewal appends transaction actions, so an
allowance-only classifier cannot admit its later process restart. Authenticate
the exact active and archived approval, reconstruct the fleet append from its
archived predecessor at the original journal checkpoint, then authenticate the
full later journal independently. Valid preparation after that checkpoint must
not be treated as conflicting renewal submission. Keep the original fleet-apply
exclusion for new work. Use the same restart admission for preflight binary and
readiness preparation, process startup and interrupted manifest publication;
none of these paths may replay pending setup or alter final acceptance.

### Recover custody without repeating completed acceptance epochs

The probe's immutable `transferOut` accepts an off-chain amount. A position can
accrue between that quote and inclusion, and small residual positions can fail a
runtime transfer minimum. Final custody cannot be inferred from a successful
receipt or a nearly equal balance. Keep requested units, actual source debit,
actual recipient credit, bounded share conversion and inter-block growth as
separate fields. Preserve successful receipts even when they leave a residual.

Production recovery must use separately signed, finite authority for each affected
position, bounded funding when a residual is below the transfer minimum, and one
durable transaction writer. Bind quotes into action intents; retain signed bytes
through timeout and crash recovery; verify gas, fee and value limits again during
replay. A final record must prove both source positions zero at one finalized
head. Test interrupted signing/finalization/postcondition boundaries, quote edits,
extra credits, receipt/recipient changes, gas-cap changes and reseed exhaustion.

Recovery admission must authenticate only the exact repair authority, custody,
immutable target and retained journal before allowing its durable sender to
reconcile pending bytes. A finalized deployment nonce census cannot precede that
reconciliation: the transaction being recovered may already consume the next
nonce without a finalized journal row. Restrict the recovery executor's dispatch
scope when reusing partial payloads, and enforce the signed fee/value envelope
before rebroadcast as well as during final replay. Test a crash with a signed,
unfinalized call and prove the same bytes finish without allocating another nonce.

For future probe/custody maintenance contracts, provide a narrowly authorized
operation that reads and transfers the full selected position in the same call,
with exact before/after events and an explicit recovery recipient. This removes
the quote-to-inclusion gap; it does not change exact-amount payout entitlements.
The already deployed testnet probe instead uses the bounded mechanism documented
in [PRECOMPILE-RECOVERY.md](../sim-testnet/PRECOMPILE-RECOVERY.md).

A later custody repair must not rewrite a signed interval or force already
observed epochs to repeat. Preserve the original result and add an authenticated
completion for the repaired scope; require the production handoff to understand
that composition explicitly. A new binary cannot silently join an immutable live
interval. Deferred historical audits and unrelated semantic checks remain their
own outstanding requirements until their exact proofs are accepted.

### Keep read availability separate from verified mismatches

Relay startup and receipt reconciliation must return an RPC read error before
comparing the unread value with an approved snapshot, nonce, registration or
transaction. Do not join a fabricated mismatch to a timeout: mixed errors remain
hard by design, so that join prevents the bounded transport retry from running.
The same rule applies to the final registration and canonical-hash rechecks in
the native schedule reader and to retained manifest reads.

Give initial phase admission and each consumed phase-transition request their
own bounded retry. Keep the first successful finalized head and original wall
deadline across admission attempts. A canceled caller releases its request while
the relay remains available; worker shutdown cancels an active request. Retain
hard integrity and local persistence failures even when joined with cancellation.

A permissionless publication race needs a typed canonical-revert outcome,
distinct from a failed journal write. Retry its independent winner, canonical
receipt and transaction-body reads using the original signed bytes and nonce.
Keep the reverted receipt and actual paid gas visible. Test failures at each
read boundary, exhaustion, request cancellation, original-deadline retention,
journal reopen and no duplicate broadcast. Test real mismatch and storage-error
controls beside every transient recovery path.

### Retain unsigned repair liabilities during independent observation

Recovery 29 stopped before its acceptance boundary because the initial snapshot
retried an unsigned probe repair whose estimate exceeded its signed gas-unit
limit. Cancellation then interrupted the independent evidence census. Repeating
the same operation could not supply the missing authority.

For PH-01, PH-02 and PH-05, separate observation, signing permission and final
acceptance. An explicitly provisional observer may retain an accounted repair
liability after a typed refusal raised before signing. Require the exact signed
action and gas cap, validated custody/accounting, a durable failed journal
frontier, and proof that no matching signed transaction exists, including orphan
transaction files saved before their broadcast record. Keep standalone repair,
mixed integrity/storage errors and other budget or fee refusals hard. Any change
to transaction authority still requires its own explicit signed amendment.

Under the same exclusive writer, reuse that refusal only while its plan, action,
authority and action-journal frontier remain unchanged. Reauthenticate those
inputs on each observation; do not append identical intent/failed records or
repeat the full signature census. Preserve the pending liability and completed
receipts in evidence. Strict final acceptance must still require complete,
verified custody recovery.

Simulator commits `8369f96a` and `16e9896a` implement this narrow continuation.
Eight focused normal and race tests passed, covering observer survival without
another send, durable failure requirements, orphan signatures, joined errors,
standalone scope and unchanged final rejection. See
[the regression](../sim-testnet/precompile_recovery_gas_pending_test.go).
Production integration and operational acceptance remain required before closing
the corresponding hardening items.
