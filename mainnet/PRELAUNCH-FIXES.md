# Mainnet prelaunch fixes

## Acceptance-window attribution from R44

R44 terminal diagnostics exposed two ways to draw a false conclusion from
otherwise valid retained evidence. A payout check selected the latest signed
artifact, which could belong to epoch 621 after the accepted [616,621)
window; an older signed claim observation lacked additive discovery fields,
which a diagnostic displayed as zero rather than unavailable. Before mainnet,
bind every tier/cohort assertion to the exact accepted epoch, committed root
and artifact hash. Treat absent legacy fields as unavailable, while preserving
real queue and receipt failures as failures. Qualify with a later conflicting
artifact, a missing historical field, an exact-window match and a changed
root/hash. The testnet repair is SN `f673ca9a`; composed release qualification
and deployment evidence are still required before this item is Done.

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
| PF-05 | Give client-key histories an authenticated policy-scoped rollover | — | Astra | In progress | A scheduled policy change starts a new signed generation-1 segment for each client; old signed rows remain byte-identical and historically readable, current readers select only the active domain, and all miners regain processed-key readiness without bypassing it. |

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

PF-05 follows the 2026-09-24 testnet policy-rate rollover. The new policy
activated and its runtime was published, but all 20 provider swarms reported
zero ready members even though their processes and both operator services were
live. `/connect/control` returned HTTP 200 with the application error
`client-key registration cannot replace a retired or different-domain head`.
The existing head is keyed by client ID while the signed history domain includes
the policy hash; a new policy therefore cannot append to the old segment.
Keep the old signed records immutable and introduce an additive domain-scoped
head and generation namespace. Authenticate policy activation before admitting
the new segment, keep generation-1 and same-domain successor rules strict, and
make current and historical API/validator readers choose their exact domain.
Test populated migration, concurrent rollover, retries, rotation, retirement,
network identity changes and old-epoch replay. The migration monitor's expected
schema must advance with the actual table shape. Retained resume must run both
operator migrations before starting the successor APIs; its older path omitted
that barrier. Any consumer using persistent peer key pins also needs an
authenticated policy transition for its domain ratchet, rather than clearing
pins. Do not treat HTTP success as
processed client-key success or mark a provider ready before its current-domain
registration completes. The active testnet resume retained its supervisor on the
bounded readiness timeout, so the repair should reuse that generation's durable
setup evidence rather than redoing on-chain actions.

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

**2026-09-23 release-interval follow-up.** The live RPC consistency actor
opened fresh native-chain readers for each sample, repeatedly decoding runtime
metadata and discarding an authenticated cache. Several sequential reads then
inherited the nearly exhausted 10-second sample deadline and were reported as
RPC timeouts even while direct LAN reads were fast. Production readers should
reuse an owner-scoped chain client and bounded immutable metadata cache,
authenticate each pinned block and runtime identity, and retry transport reads
inside one measured sample budget. Test the complete multi-call sample under
slow metadata and a one-call timeout; a fast isolated RPC probe is insufficient.

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

**2026-09-23 reconnect follow-up.** Concurrent old/new Connect sessions can
share a reverse egress key. An old session's cleanup must compare its lease
owner before deletion, so it cannot remove the newer session's live mapping
and produce a synthetic verification hop. Cover reconnect overlap, stale TTL
expiry, proxy/direct handoff and replayed multi-hop verification in deterministic
tests. Retain bounded response diagnostics that identify a rejected verification
step without logging secrets.

**2026-09-23 fault-selection follow-up.** A verification probe selected miners
that a scheduled quality fault had deliberately disabled, then treated the
expected missing source lease as a protocol failure. Resolve the exact logical
miner before probe selection, exclude active fault targets and guard a signed
walk against a fault starting mid-request. Continue to reject wrong source,
signature and response content for every request actually issued; fault scope
must not become a blanket waiver for an entire swarm or operator.

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

**2026-09-23 release-heartbeat follow-up.** R31 entered the real release epoch
and then stopped because a heartbeat treated process-log findings as a reason
to terminate before the terminal acceptance block. Production monitoring must
persist classified findings and keep the interval running; the final gate still
rejects unresolved findings. Only evidence-integrity or authorization failures
should stop the heartbeat itself. Attribute a fault-related log to the exact
logical client and its authenticated event-time fault window, since a buffered
line may be scanned only after the fault has been restored. The affected swarm
process may remain healthy while one miner is intentionally disabled. Test both
the continued run and strict terminal rejection of an unrelated error.

**R44 evidence-detail follow-up.** Ten acceptance-scoped `exit-gap-timeout`
findings (14 events) came from the old Connect receiver's bare timeout line.
It did not retain the expected sequence, queued range or handoff state, so the
later closed-hole and ready-rendezvous fixes cannot prove which historical
timeouts they repair. Production gap incidents must include those bounded
sequence and ownership fields, the exact retry/expiry deadline, and whether
the hole was closed by an admitted packet or remains genuinely unresolved.
Keep real missing-packet expiry and incomplete steering continuity visible at
final acceptance. A normal websocket close and a compact artifact read timeout
need bounded retry with the same evidence identity; cancellation from an
intentional owner stop remains a distinct outcome. Test both repaired transient
paths and a true unresolved gap, then verify the live log carries enough detail
to attribute a recurrence without guessing from the error class alone.

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

R35 exposed a remaining scope gap after this improvement: release observations
still re-entered recovery-lineage validation, repeatedly authenticating the
latest two generations in roughly nine-second passes. The controller accumulated
tens of gigabytes of logical reads while observations advanced. Before mainnet,
cache only the sealed predecessor-edge proof across observations under exact
source-byte, file-identity, plan and authority witnesses; invalidate it on any
changed generation or source. The current attempt envelope is rewritten during
observations and must remain freshly authenticated. Keep the per-edge checks
when a new generation is appended, and measure full lineage validations and
logical read bytes per observation in the actual release process. The repeated
edge is material to the observed 100–162-second gaps, but is not yet proven to
be their only cause. A process staying alive is not a throughput proof.

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

**R44 follow-up (in progress, 2026-09-25).** The per-miner recent-first poll did
not make the two relayers fair across miners. One shared, non-cancelable lock
covered reconciliation, signing and finality; a miner recorded `submitting`
before waiting for it. A hashed live census at finalized block 8,081,014
found 744 miners still discovering epoch 614 while 256 had reached 617. All
1,000 eventually reached 617, but epochs 615 and 616 remained almost entirely
pending. Mainnet admission must assign one retained ticket per member, prioritize
the global newest epoch with a bounded historical share, let waiting members
continue discovery, and start the five-minute network budget only after
admission. A timeout after durable `Prepared` must not let the next member sign
the same nonce: seed a shared nonce floor from every validated member queue,
advance it only after the signed intent is fsynced, and reconcile or rebroadcast
the exact old raw transaction before treating its outcome as absent. Reject two
swarm members pointing to the same physical queue directory. Test mixed
discovery cursors, cancellation, stale pending nonces, restart, cross-member
fairness and exact signed-outcome retention in normal and race modes. No R44
runtime change or completed production qualification is claimed here.

The R44 acceptance reader also mixed lifetime claim counts with its signed
five-epoch window. A previous finalized claim could falsely satisfy current
coverage, while a historical uncertain claim could falsely fail it. Keep raw
lifetime history and scope acceptance and anomaly verdicts to the exact signed
epochs, requiring an observed outcome for every configured miner in each epoch.
Keep the whitepaper's claim TTL: `pending` and `retry` may remain after an epoch
finalizes while their value stays in outstanding liability. The phase-level
claim coverage and on-chain conservation checks still apply. Treat a current
`submitting` send as uncertain at the acceptance cut, and reject actual
unreconciled `uncertain` or `failed` outcomes. Do not close a signed uncertain
incident merely because
the local queue later says `finalized` or `no-claim`; first authenticate its
canonical receipt, block hash and Claimed event, or retain the incident open.
The completed window-only claim gate is SN `5615a382` and historical anomaly
scoping is SN `ac2beccd`; receipt-authenticated closure remains separate
qualification work.

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

### Publish complete artifacts for failures before the first observation

An initial snapshot failure left a terminal result and process-log evidence but
no observation file, so the next recovery could not authenticate its predecessor.
For PH-01 and PH-09, publish an explicit zero-observation marker before the
terminal result when no observation or acceptance boundary exists. Propagate
append, sync and publication errors before claiming a complete terminal artifact
set. A failure to record evidence is a separate hard failure.

Legacy repair belongs to the authenticated recovery writer. It may add only a
missing marker after validating the exact failed result, zero recorded
heads/epochs/observation hashes, absent acceptance/start markers and the matching
process-log evidence. Preserve the original result bytes and all existing
observations. The next signed recovery binds the new marker; read-only validators
must continue to reject missing or changed sources.

Simulator commit `2a6340b1` implements this scoped repair. Thirteen focused tests
passed normally and with race detection, including a real initial-snapshot
failure followed by recovery-chain validation, legacy backfill without result
mutation, write-error propagation and observed-progress/source-substitution
rejection. See [the regressions](../sim-testnet/scenario_initial_observation_test.go).
Production crash-publication qualification remains required.

### Keep release intervals running while classifying adversary failures

R34 entered its signed release interval and continued making finalized-block
observations while three adversary probes found hard errors. RPC consistency
timeouts were successfully recorded as pending and then recovered. Separate
operator artifact GET timeouts and verification `503` responses remained hard
findings. A healthy fleet and advancing block head therefore show liveness,
not final acceptance. Production should retain the exact failed probe, actor,
target, block and fault window without terminating an otherwise useful interval;
the final gate must still reject unresolved required probes. Recovery must never
turn an unanswered read into a verified mismatch or a skipped probe into coverage.
Operational status must group pending and recovered rows by recovery ID: historical
pending rows remain after recovery and must not be counted as open incidents.

Give each HTTP operation its full configured attempt deadline before bounded
retry. Dividing a ten-second sample into short attempts canceled artifact reads
that were completing in roughly three seconds, creating failure during normal
load. Retain one overall budget, retry only classified transport and server
availability errors, and require the original content-addressed validation of
every nonempty recovered response. An empty history after timeout carries no
coverage. Test slow successful reads, timeout followed by success, exhausted
retry, empty history, malformed response and cancellation through the full actor.

For expected production GETs, use at least 60 seconds total and default to a
five-minute retry horizon when no tighter protocol deadline applies. Give each
attempt a real response deadline, back off between transient transport failures
and retryable server responses, and preserve the original request identity and
hash expectation throughout. A missing object, authorization refusal, malformed
response or hash mismatch is a semantic finding; repeated transport success
cannot waive it. Long retries must not hold the release heartbeat or silently
extend a signed fault window: persist the pending read, let unrelated work
continue, and complete or fail that exact read within its own bounded horizon.
Test an outage lasting longer than 60 seconds, recovery before five minutes,
exhaustion, cancellation and a fault-window transition during retry.

Fault admission must distinguish a scheduled trigger from a physically active
pre-arm. R34 installed exact validator-view exclusions before the release epoch
so the quality-fault boundary could start safely, but the verification actor
selected those excluded miners while their signed trigger was still pending.
Pre-arm the exclusions before any dependent fleet lifecycle action; publish
their exact target scope before installing physical files, and select probes
only from the eligible census. Across recovery, adopt only the signed filter
rules and verify their bytes, private-file mode and process ownership. Retain an
existing filter file without replacing its inode until the authorized restore;
reject unrelated faults or partial restoration. If an older predecessor removes
the filter during shutdown, record that continuity gap explicitly; a successor
that installs a fresh filter cannot claim uninterrupted protection.

Qualify the whole rollover, not only the candidate binary: authenticate the
sealed predecessor and signed invalidation, pin evidence hashes, verify the
retained fault registry and physical filters before service replacement, then
check their permitted state after the new supervisor starts. Require the exact
binary provenance, manifest, 33-process identity and healthy fleet before the
next interval. The handoff may tolerate a typed provisional predecessor and
recoverable transient errors, but must reject changed authorization, substituted
evidence, unexpected fault controls and missing physical safety rules. Exercise
pending pre-arms, retained filters, intentional absence after old cleanup,
interrupted install, changed registry, partial restore and rollback in tests.

**PH-27 — Policy rollover must include validator evidence activation.** R40
failed its live release interval while relaying a closed-census header for epoch
594. The signed header carried policy hash `0x1526b242cf4908cc31f7e58006664bce6064003c69fd8452eab2d49122fef277`,
but the finalized coordinator `policyAt(594)` returned
`0x41f0c7efe7e1b23b2fd22dac9352ca18be48d2e4d1fb5b41ce89660bc899b0dd`.
The LAN-node `eth_call` returned `InvalidEvidence()` (`0xc9779e3c`); the
write-once slot was empty, so rebroadcasting the same signed bytes cannot
repair it. The old activation was published at block 7,975,571, well before
the policy-v2 effective epoch. This is a policy-era authority mismatch, not
an RPC timeout or a nonce race. The original failed action and result remain
in the R40 evidence bundle.

Before mainnet, make a policy transition atomically schedule a new validator
evidence activation for each controlled operator and both validators, with
dual-key consent, publication and finalized readback before the first epoch
whose evidence uses the new policy. Bind the new activation to that policy and
its actual future epoch; do not rewrite historical signed headers or backdate
activation. The relay must compare each header's policy with finalized
`policyAt(header.epoch)` before budget admission or gas estimation, retain an
explicit unpublishable historical gap when they differ, and continue to valid
future slots without misreporting the gap as accepted coverage. Acceptance
must require every counted epoch's headers to have valid policy-era activation
and on-chain commitments. Test rollover at the exact effective boundary,
delayed deployment, interrupted activation, mixed old/new validators,
historical mismatch, and resumption after an unpublishable gap.
The existing activation-history file replays only legacy-to-V2 closures; it
cannot attest a V2-to-V2 rollover. The production handoff must verify the
latest signed V2 terminal closure and bind each new activation's
`firstSequence` and `priorRoot` to that operator's exact terminal cut while
writers are fenced. Preserve the old activation and ledger as immutable
history; verify the new policy at the activation epoch and start the new
producer only after all four new publications are finalized.
Do not restore the old policy merely to make old activation signatures
publishable: testnet policy v2 materially changed pool rates, and labeling
those rates with the old hash would misstate the governed policy. A fresh VPK
and namespace is a different measurement source, not a continuity proof;
admit it only through explicit source-generation and public lineage controls.
Classify an early root-commit deadline alert as a pending retry, retaining its
exact log evidence. A warning while time remains is not a missed deadline:
testnet epoch 597 warned with 7m48s left and both roots confirmed 37 seconds
later. Keep imminent/passed deadlines blocking and independently verify the
finalized root and deadline before accepting the epoch.
If an on-chain policy change makes an old signed measurement source
unpublishable, an explicitly new VPK/source generation can start at a future
untouched epoch without pretending its sequence, EMA, or ledger continues the
old source. Production needs one authenticated handoff controlling validator
directories, client identities/JWTs, API admission contexts, relay routing and
collector identity selection; preserve the old signed evidence and account
for the excluded gap separately.

### Count paid and free provider usage equally

For PH-12 and PH-13, keep the SN usage ledger independent of customer billing.
R35's signed artifacts for both operators in epochs 585 and 586 contained no
provider rows and zero total usage, although each operator had settled hundreds
of thousands of same-network contracts that day. The producer read only
`transfer_escrow_sweep`; those contracts correctly created no customer charge or
billing sweep and were consequently invisible to SN. The last positive billing
sweeps predated the campaign by eleven days. Creating paid sidecar traffic would
mask this accounting defect and is not its repair.

The required rule is that all valid paid and free provider bytes contribute
equally. Record immutable per-contract/provider usage independently of balances,
revenue and financial payout suppression, including same-network participants.
Bind direction, participants and settled byte evidence before mutable stream
membership or contract cleanup can change their attribution. Preserve exact
byte conservation, one-time settlement, explicit dispute outcomes, nonnegative
amounts and the exclusion of unfinished work. Customer billing and its existing
payment rules remain a separate consumer.

Keep the governed NO deposit/rate/quality formula unchanged. The signed artifact's
`total_usage_bytes` feeds the existing prior-epoch required-deposit calculation;
validators authenticate the artifact and audit that amount before weighting
`deposit / rate × Q`. A free-service operator funds the same required deposit
for the same usage. Customer payment status must not affect usage, relative
provider shares, required deposit or weight. Head exclusion, reliability floors,
deposit caps, tier snapshots and mismatch rejection remain strict.

Implementation and qualification are pending in the server usage producer.
Required deterministic regressions compare otherwise identical paid, free and
same-network transfers; cover forward/companion and multihop attribution,
duplicate/concurrent close, partial and disputed close, cleanup, exact epoch
boundaries, mixed financial allocations and zero usage; and trace equal usage
through artifact totals, required deposits and validator audit. Preserve every
already signed historical artifact and its original interpretation; deployment
must establish an explicit prospective accounting boundary. Earlier same-network
reply contracts could lose their companion/origin role during billing
normalization, so historical endpoint rows cannot safely recover the provider.
Do not backfill guessed usage or replace the signed zero-usage artifacts. Start
the new accepted measurement window after deployment of the usage producer;
an epoch crossing activation cannot silently count as complete. Closure requires
nonzero eligible provider artifacts, timely root commits and successful claims
for both operators in the accepted interval. Connect idle recovery alone does
not establish this settlement coverage.

### Size governed rates against the native movement minimum

Positive valid usage does not guarantee an executable demand deposit. The
coordinator moves the exact governed amount, while the native runtime and
immutable vault enforce a TAO-denominated transfer floor after alpha conversion.
A rate chosen without measured epoch bytes can produce a valid amount several
orders of magnitude below that floor. Increasing the transfer to the floor
would break the deposit audit and must remain forbidden.

Before mainnet launch, preflight the exact integer floor, epoch cap and reserve
rounding across every conviction tier and conservative observed epoch usage and
price states. Require at least twice the native minimum as operating headroom,
then repeat the exact minimum check immediately before each actual deposit.
Govern future rate changes explicitly and authenticate both old and new policy
documents; preserve the prior signed artifacts and original activation domain.
The deposit/rate/quality weighting rule remains unchanged. A uniform rate
increase changes operator funding economics even when uncapped relative demand
is preserved, so it requires a new reviewed policy and future activation.

The testnet correction retains both policy files, existing caps and one-epoch
usage lag. Its [rollout procedure](../sim-testnet/POLICY-RATE-AMENDMENT.md)
requires the two additive database migrations before service adoption, a
prospective immutable usage boundary, legacy-contract drainage, a complete new
policy source epoch, and a signed source/price/minimum readiness proof. There is
no historical backfill or acceptance waiver. Mainnet parameters require their
own measured economic and native-minimum qualification.

### R42 continuation: scheduler, recovery cache and fixture authority

The live R42 release interval exposed validator steering exits after repeated
native scheduler reads timed out during the injected RPC-proxy fault. A read
timeout creates no intent and does not establish a changed epoch or bad
evidence. Production steering must retry typed transport failures across
normal polls without spending the native submission-failure budget or clearing
the completed epoch, pending cut or unresolved submission. Cancellation still
ends owner work; epoch regression, joined integrity errors and actual
submission failures remain hard errors. The offline correction is covered by
deterministic transport, pending-cut, cancellation and regression tests; R42's
already-running fleet keeps its original executable and its failures remain in
the run evidence.
Another R42 exit occurred after a compact live-head binding census timed out
on an `eth_call` at a pinned canonical block while advancing an incomplete
native epoch. The timeout supplied no differing block hash and must be
retried as a transport interruption using the same pinned read context and
retained pending cut. A retry must not fabricate a completed census or turn
the timeout into a reorg/integrity error. Cover batched EVM reads, individual
GETs, source snapshots and native scheduler reads with the same typed policy;
keep actual changed canonical hashes and malformed results hard failures.

R42 validator 2 also exhausted steering retries after switching to a fresh
measurement source while retaining its native hotkey. The finalized native
source commitment slot still matches an applied, signed intent in the older
source generation, but the new generation's local intent store is empty. The
current role check therefore treats its own historical commitment as an
unretained write. Before mainnet, authenticate a narrow predecessor-source
handoff (exact hotkey, finalized commitment hash/block, owner and immutable
intent) across source generations. Never accept an occupied slot merely
because the hotkey matches, and never invent a missing local intent. Cover
legitimate retained predecessor, different role, altered hash/block, missing
signature and retry after interruption in deterministic tests. The R42 exit
and restart remain findings even if a later executable fixes this class.

R42 also reauthenticated 42 historical recovery generations on each live
checkpoint because mutable current journal and attempt files invalidated a
cache witness for otherwise immutable predecessor evidence. A mainnet recovery
cache must authenticate immutable generation sources once, verify the retained
journal prefix and only the appended suffix on continuation, and check the
current envelope afresh. It must reject modified old bytes, missing or
reordered entries and forged tails, including concurrent append during a cold
audit. Cache identity must follow authenticated evidence and authority rather
than executable hash or a mutable file's whole hash. Keep cache loss
recoverable by full verification, with bounded work and memory.

Finally, repository fixture generation must select an explicit supported
server manifest profile before comparing resources. The current pinned server
uses the 30-resource legacy geography profile; a proposed 28-resource
GeoLite profile is not an available server API. Reject mixed, incomplete and
unknown profiles, and guard missing decoded configuration or policy before
projection. Fixture tests must validate the selected source's real schema and
bytes; exporter parity remains pending until the exporter exists in the pinned
server source. These are build and qualification safeguards, not evidence that
R42's live acceptance has passed.

Terminal diagnostics must distinguish the chain's terminal block from the
runner's signed terminal result. A failed provisional interval may continue
until its watchdog after the block, so a short result-file wait can produce an
early inventory that lacks the final failure set. Keep the live runner and
read-only auditor independent; collect at the block for timely diagnosis, then
collect again after the exact signed result appears. Bind both inventories to
the same run ID, plan, boundary and source hash. A diagnostic report never
creates a pass marker or substitutes for the original signed result.

R44's first terminal capture also compared EVM addresses by presentation text:
a checksum-case validator configuration and a lowercase signed measurement
named the same coordinator and vault, but the collector rejected them. Validate
each complete address before comparing its 20-byte identity. Do not repair this
by remarshal, relabeling signed bytes, permissive padding/truncation, or ignoring
chain, genesis, policy and intent checks. Apply the same rule to historical
client-key decisions and artifact observation requests. Regression tests must
accept checksum/lowercase equivalents with original bytes unchanged and reject
different deployments, malformed addresses and wrong chain domains.

An external non-accepting terminal diagnostic must retain its admitted plan
when reading provisional companion and relay evidence. That read authority
does not reconcile the plan for strict acceptance or authorize any mutation.
Creation, anchoring and activation transactions keep their original approved
ancestor, ordered broadcast/finality/verification rows, exact signed transaction
and hashed postcondition. Authenticate the immutable archive and current
approved ancestry before selecting them; closed pre-broadcast attempts cannot
replace or invalidate a later authenticated original transaction. Keep the
ordinary collector strict and test both read scopes, changed command/config/RPC
authority, competing finality and tampered archives, signatures and receipts.

Keep independent diagnostic obligations separate. A missing lifecycle payout
index must remain unavailable, but it must not prevent collection of ordinary
signed acceptance-window payouts. Preserve the original observation and every
exception, validate ordinary signatures, content hashes and epoch coverage,
and report malformed lifecycle evidence as failed rather than merely absent.
The strict combined collector must continue requiring both scopes. Test this
with original signed bytes and an accepting-owner negative control; a useful
partial diagnostic is never evidence that the full qualification passed.

The production cadence scheduler and its receipt verifier must share one
finalized policy-history reader. R42's coordinator already has three versions
because an approved rate amendment added one before production; a fixed
two-version gate rejected that legitimate history. Admit only the exact
approved predecessor and amended policy, then one future production version;
pin every read to one finalized block and verify the append-only indices,
effective coordinates, active snapshot and receipt. Reentry after a partial
write must recover the same transaction without scheduling a fifth or using
an unreviewed policy version. The testnet correction is `402e6b1b`; mainnet
must rehearse its own approved policy sequence before launch.

An operational continuation from a failed testnet release must retain every
failed assertion and a distinct non-accepting gate. Before production can
start, authenticate the exact signed terminal source and result, recompute
custody and identity checks from the signed observation, and require every
scheduled release fault to be restored in both records with a subsequent
signed observation. The active-fault recovery ledger must be empty. Keep
historical lifecycle plan identity separate from the current plan authorizing
new actions. This permits diagnosis to continue without laundering a failed
release into a strict pass or overlapping old fault injection with production.

The R45 renewal review exposed the same authority distinction in retained
validator generation readers. Round seven appends a new fleet approval, but
the activated generation and its owner-signed source-role overlay still name
the original round-six approval. Requiring their source hash to equal the new
active plan would reject an otherwise unchanged retained restart. Resolve the
original approval only through its immutable archive and the current approved
ancestry; authenticate the exact deployment, evidence custody, configuration,
policy and owned RPC authority before reading the original generation bytes.
Never substitute a hash onto different configuration semantics. New rollover
or source-role mutations still require the exact current approval.

Qualify this boundary with real signed round-six to round-seven renewal
fixtures, unchanged manifest inventory and overlay bytes, repeated retained
reads, and refusal of a fresh mutation under the historical approval. Missing
or tampered archives, unrelated lineage, changed deployment custody and changes
to configuration, policy or RPC authority must remain hard failures. Include
launcher, manifest, observation and relay readers in the same migration test;
a successful doctor before renewal does not exercise the descendant-plan seam.

R44 exposed an impossible restoration predicate after its explicit testnet
lifecycle bypass: the bypass correctly retained no terminal-effective mutation
epoch, while a local companion filter required that epoch before removal.
Separate operational cleanup from proof that a lifecycle transition occurred.
A diagnostic successor may remove only the exact two authenticated local
filters after the full signed interval and their minimum durations, retaining
`RestoreConditionMet=false`, the original failed assertions, and
`final_acceptance=false`. Mainnet acceptance must still prove the actual
lifecycle transitions; diagnostic cleanup is not a substitute. Installed, paid,
and effective mutation predicates must not infer success from an operational
handoff stage reached through a bypass.

Checkpoint the exact cleanup request before touching the filter, retain the
removed target/role/identity census, and date completion from a subsequent
complete observation. If removal outlives its active ledger entry, reconcile
only through the retained plan/operator/rule-bound removal receipt and proof
that the exact rule is absent. Ordinary restore must not acquire this special
missing-ledger authority. A public evidence file written before its owner
checkpoint is not authoritative: recovery reads the signed fault state and
independently reconciles the physical outcome. Rehearse both interruption
windows, foreign receipts, reappeared rules, and the failed-release to
non-accepting production handoff without changing strict acceptance.

R42 ended before terminal because a provisional heartbeat treated a known
validator steering-continuity finding as a reason to stop the entire interval.
Production should keep collecting through recognized provisional findings and
report the full set at final acceptance; unknown process-log classes and
integrity failures remain hard. An interrupted process-restart fault must
checkpoint its exact signed target generation and retry a bounded health
observation on later heartbeats. A stuck child may need a targeted operator
repair, but neither a retry nor a restart may erase the active-fault ledger
without observing a different healthy supervised PID. Add deterministic tests
for mixed known findings, an unchanged unhealthy PID, replacement recovery,
and a changed manifest identity.

The source-role rollout's read-only native precheck met a new runtime 471 while
its retained config pinned 467. Every current-runtime reader, including
review/apply commands, must install the explicitly approved provisional
compatibility profile before authenticating the live artifact; historical
source signatures and blocks remain exact-pinned. Test a consumed-interface
successor and a real metadata/API incompatibility separately. A precheck must
not rewrite the original config or signed campaign evidence.

R45 qualification exposed a second runtime-version trap in tests rather than
the fleet code: four current-runtime fixtures still expected spec 461 while
the production constants, lockfile and reviewed artifact already agreed on
467. Mainnet's current-runtime tests must authenticate the independently
reviewed source commit and metadata hashes for the selected launch artifact,
then require production selection to match that evidence. Keep older versions
as explicit historical decode/rejection cases; an old fixture must not silently
become current authority. Run the full miner and on-chain suites normally and
under race detection after changing the launch runtime pin. The testnet repair
is SN `5a53b33c`, with the old-fixture tests causally reproducing all four
failures.

R43 startup stopped at a stale operator overlay resource list: the current
pinned server reads `mmdb/ip-ipinfo.mmdb` and `arindb/arin.mmdb`, but the
simulator demanded future `geolite2.mmdb` and `places.yml` files absent from
the selected config repository. The production preflight must derive required
resources from its pinned server/config profile, validate every required file
before topology stop when possible, and test both the supported legacy and
future profiles without mixing their manifests. A missing resource must be
reported precisely; it must not be fabricated or silently linked to a
different database schema.

R44's second terminal diagnostic could authenticate the signed start but its
newer checkpoint reader rejected `start_time_ticks` inside retained fault
process records. Historical signed evidence is a compatibility contract:
retain known optional nested wire fields even when the producing runtime
feature is no longer active. Decode them with bounded types, preserve original
signed bytes and hashes, and reject unknown or malformed fields and generation
rewrites. Test both legacy omission and a fully signed historical checkpoint
through the current forensic and recovery readers. Recognizing an old process
identity in evidence never grants authority to signal that process.

R44's later read-only terminal capture exhausted a 15-minute deadline while
reading a retained validator source: 62 cuts scheduled about 1.63 GB of chunk
GETs across two origins, and a verified 4 MB chunk alone took 18.30 seconds.
Production evidence readers should bound the whole job from measured bytes and
throughput, expose per-cut progress, retain completed authenticated chunks,
and reuse only exact immutable origin/kind/hash/size matches. Retry incomplete
HTTP bodies within that finite budget and distinguish budget exhaustion from
invalid signatures or conflicting content. Test slow, interrupted, duplicate
and conflicting chunks without reducing final integrity checks or restarting
unrelated runtime work.

R46's sealed diagnostic reached its one-hour source deadline at cut 70/96;
the next independent checks still ran and preserved their failures. Diagnostic
capture now permits exactly two origin readers with one synchronous archive
owner, a fresh one-minute context per immutable read attempt and a five-minute
read budget inside the unchanged job deadline. Completed origin/kind/hash/size
witnesses survive retries only within that invocation; a new invocation must
prove custody again. The other origin may finish after one origin fails, but
the complete source result still fails. Require deterministic overlap, broken
body recovery, unchanged prefix custody, cancellation joins, global byte/object
limits, hard integrity/close failures and downstream checks after timeout.
`TestReleaseCaptureOrigins*`, `TestReleaseCaptureStreamRetry*` and
`TestTerminalDiagnosticCaptureTimeoutKeepsLaterChecks` enforce these boundaries.
Strict capture defaults remain serial without these diagnostic retries; a
late nil return after a check deadline is a failure, never a passing closure.

A diagnostic output directory is never an authority source. R46's terminal
collector incorrectly looked there for the original adversarial campaign and
reported it missing. Bind each read to the selected original run, authenticate
its exact raw campaign against the retained result and canonical matrix, then
copy those bytes into the external archive. Retain the original path/hash and
failed vector disposition; a valid matrix does not make its campaign pass.
`TestTerminalDiagnosticAdversaries*` requires distinct source/output roots,
rejects an output decoy, missing or substituted original, symlink and matrix
mismatch, and proves failed originals stay failed without writing into the run.

R45 source review found eleven previously qualified recovery fixes absent from
the candidate main branch. A passing component test or isolated branch is not
deployment evidence. Before mainnet launch, derive the release image from a
reviewed dependency-ordered commit inventory, compare every changed source
file to its qualified hash fence, and run the affected composed normal/race
tests on that exact source. Include durable snapshot retry, transport error
classification, original-child signaling proof, write-ahead fault intent,
pending container restore and post-transition completion heads in the
composed recovery rehearsal. A documentation-only main advance should not
change the approved executable, but it must not conceal a missing code patch.

### Require proof production before completing a validator restart

R46 marked replacement validators restored while they were still replaying
startup evidence. The next sequential restart could then remove the remaining
proof source. Require the exact healthy replacement PID and kernel start ticks,
plus a complete signed trail begun after its recorded start in every approved
operator/VPK namespace. Use bounded tail reads for this readiness check; keep
the full authenticated proof history audit as a separate terminal requirement.
A restarted controller must recheck the same durable fault and current process
generation without signaling again or resetting the freshness boundary. Keep
the restart pending and its successor blocked while proofs are missing. Test
the actual restore/scheduler with a healthy but proof-starved child, reentry,
one missing operator, stale namespace, a late final hop on an old trail,
foreign signatures, changed kernel generation, incomplete rows and cancellation.

Streamed V2 replay must retain the same bounded exact ASSIGN-signature reuse as
legacy replay. Two signed M8 trails otherwise repeat 70 checks for 14 unique
tuples. The 64-entry cache belongs to one cut invocation, keys on full public
key/message/signature bytes and retains successful checks only. Independent
replicas, retries and cuts must authenticate again; record signatures, source
hashes, EOF, proof projection and cancellation remain mandatory. Require the
real-stream old-behavior control to fail, plus changed-key and late-error tests.
This reduces duplicate work; it does not establish a measured production
startup speedup or eliminate all capture and replay costs.

R46 operator 2 closed an empty epoch-634 payout census, while operator 1 closed
four leaves. The pinned server intentionally skips submission with no leaves;
the later RootMissed status is consistent with that disposition. Usage bytes
alone do not grant payout eligibility: active heads remain excluded and pool
providers need assignment and confirmation exposure. Test proof-starved usage
through the actual provider join and signed artifact, then recovery with fresh
confirmed exposure. Test zero-root rejection and exact operator-scoped carry to
a later real root, preserving the original RootMissed status. Do not invent a
leaf, backdate a commit or interpret an observation timeout as a missed root.
The complete proof blackout supports restart starvation as an upstream cause;
exclusive attribution still requires the exact historical provider census.

Keep the R46 native steering gap explicit: both replicas later held the timed-out
immutable object and both input cuts were durable, but the prior applied intent
and EMA were still thirteen native epochs behind. Exact publication recovery
does not prove a finalized intent and cannot waive EMA continuity. A later run
needs authenticated complete native history or an explicitly authorized fresh
generation. Signed-source diagnostic capture is also a separate owner: it
captures original RPC, controls and stream bytes before independent replay, so
a replay-signature optimization cannot explain or fix its HTTP/RPC latency.

### Bind RPC agreement to the requested block

R46 follow-up found an adjacent actor validation defect: two endpoints could
return equal hashes at an unrequested height, and equal malformed hash strings
passed the block decoder. Require both returned heights to equal the exact
common finalized height, and require prefixed 32-byte hexadecimal hashes
before runtime or precompile evidence is admitted.
`TestAdversaryRpcCommonBlockRejectsReturnedHeightDrift`,
`TestAdversaryRpcCommonBlockRejectsMalformedHashAgreement`, and
`TestAdversaryRpcBlockDecoderRequiresCanonicalHash` reproduce the old
acceptance; the exact-height positive also covers the explicitly owned shared
RPC route. This refusal-only hardening does not attribute or waive the 35
sealed R46 RPC errors, whose individual chronology was not retained.

### R46 measured-run lessons

R46 completed all five measured epochs and crossed its terminal block, then
sealed a failed provisional result with 86 failed assertions. Both validators
had zero epoch-634 proofs during scheduled rolling restarts. Validator 2 took
about 80 minutes to resume proof production; validator 1 remained in census
replay at the terminal cut. Process liveness and a successful restart receipt
therefore do not establish release readiness. Mainnet restart admission must
require fresh proof production through both operators and an applied native
decision from each validator before scheduling the next disruptive fault.
Persist authenticated replay checkpoints and reuse exact already verified
stream chunks or signature results within their bounded authority; test
interrupted replay, a changed origin/key/body, and the point at which proof
workers actually start. A restart that cannot recover within the signed
interval must fail that interval explicitly instead of being counted healthy
from a PID alone.

The R46 restart recovery review also found two generation races in the
readiness controller. A proof-read failure from an old restart could be
mistaken for evidence about its replacement, and a generation change after
the completion-head read could leave a stale PID or remove the newer intent.
The mainnet controller must bind each readiness read and completion decision
to the current signed generation, retry only with that generation's fresh
trails and head, and retain the newer intent until its own completion. Use
barrier-driven regressions for both orderings; a prior generation's apparent
success must never satisfy the replacement's readiness gate.

R46 operator 2 epoch 634 has on-chain `RootMissed` with no committed root,
and the terminal payout census lacks that epoch's authenticated tier. The
pinned taskworker closed with zero payout leaves and the server deliberately
skipped submission; this is an empty eligibility census, not a demonstrated
failed transaction. Four proof streams had zero completed epoch-634 proofs,
but the exact provider eligibility cause still needs a source census. Mainnet
must reconcile every expected epoch through capture, carry, root commit,
payment and claim state, including a missed-root path. A later artifact cannot
repair the missing original root silently. Keep the exact missed epoch in
the result and test a validator restart crossing payout eligibility and root
submission, including a zero-leaf control. Separately,
one claim was still uncertain at the terminal cut; reconcile its durable
transaction through finalized on-chain state with a bounded post-terminal
read, preserving the original terminal verdict and recording later resolution
as a supplement rather than rewriting history.

R46 also showed a terminal control problem: after the signed interval and all
42 faults were complete, failed strict assertions and an adversary actor with
zero samples kept the owner polling for hours. Separate collection completion
from acceptance. Once an authenticated in-window outcome is immutable and all
fault/lifecycle cleanup is retained, seal a failed result promptly with the
full strict assertion and adversary inventory; keep retrying mutable reads
while they can still satisfy acceptance. Test both paths, including an actor
whose prerequisite never appears. Diagnostic readers must run against the
sealed result: an earlier read-only inventory that timed out waiting for the
result is historical, not a substitute for a fresh final check.

The sealed R46 diagnostic also rejected legitimate lifecycle cleanup because
its final-result reader compared the original start fault record directly to
the completed record using a validator meant for adjacent checkpoints. The
live owner had signed the intermediate request before cleanup. Mainnet
history verification must replay each authenticated checkpoint in order,
applying the strict adjacent transition validator at every edge, and then
bind the final result to the last checkpoint. It must still reject a
completed cleanup that lacks the signed request in that history. Test both
the valid multi-step path and a forged start-to-complete jump through
recovery and terminal diagnostics.

The complete R46 diagnostic then found a second historical-reader gap:
`repair.coordinator-rounding.deploy` had an owner-signed corrective request,
signed finalized result and exact source-plan journal rows, but the release
capture census looked only in static plan actions and relay requests. Mainnet
historical readers must share one action admission rule for static, relay and
signed corrective actions. The corrective path must require the exact
predecessor plan, signatures, action intents and unique finalized journal
rows; an action-name prefix alone grants nothing. Chronology must recognize
the signed activation as an upgrade transition and verify its distinct
repair-result postcondition rather than inventing an ordinary `StageVerified`
row. Test capture, receipts and chronology against a valid carried repair,
changed intent, missing carry and duplicate finalization.
The testnet follow-up introduced an explicit signed-result artifact for this
one corrective activation, with ordinary postconditions unchanged. Before
mainnet, exercise the same branch against the complete archived R46 evidence,
including exact transaction calldata, emitted implementation, source-plan
signatures, unique journal finalization, altered result bytes and an invented
verified row. Keep a failed archive check strict even if synthetic unit tests
pass.
R46 also exposed an adjacent chronology rule: after ordinary activations,
the signed corrective request retains the actual upgrade being replaced,
while its source plan still carries an older ordinary-upgrade baseline.
For a corrective transition, compare the immediately preceding observed
implementation and runtime with the signed retained upgrade; keep the
source-baseline rule for ordinary upgrades. Test a valid constructor →
ordinary upgrade → correction chain, a missing predecessor and a changed
retained runtime, then replay the exact archived timeline before mainnet.

The complete R46 v3 diagnostic found another historical approval seam:
`repair.precompile-residual.v2.1` is finalized in the journal but absent
from its source plan's static actions. The corrected historical reader now
authenticates its separate recovery documents with exact source-plan binding,
both required signatures, approved steps and intents, signed completion,
journal checkpoints and unique finalized receipt tuples. A repair-name
prefix or operator assertion remains insufficient. The 73-test normal/race
suite includes 23 negative controls; an offline replay of sealed R46 v3
inputs passed the corrected contract
census, but neither substitutes for the full canonical/native chain check.
Before mainnet, exercise this same source path through bounded EVM chronology
and final artifact verification, retaining changed source, step, signature,
calldata and duplicate controls.
The same R46 chronology uncovered four approved predecessor plans that were
signed before any coordinator proxy existed. Historical readers must retain
their planned actions and finalized implementation preparation without
treating the zero address as an emitter or rejecting the whole archive. A
finalized proxy initialization, ordinary upgrade or signed repair under any
zero-proxy plan must still fail at every height. Use one authenticated proxy
census for the timeline, receipt selector, release-contract census and
baseline capture; keep the original 125-plan replay and exact census bytes as
a regression. The focused normal/race tests and sealed census replay pass,
and a bounded 196-interval LAN EVM capture found 11 authenticated upgrade
events with two initializer baselines. Its old-source timeline failed at the
known zero-proxy admission; the corrected source then passed offline timeline
building and artifact verification on the same sealed capture with HTTP
disabled. Preserve this source-and-capture replay as the mainnet regression,
but do not present it as proof of the still-incomplete canonical/native or
full acceptance checks.

The R46 replay also exposed an operational attestation trap: Go did not stamp
VCS build information when the diagnostic executable was built from a linked
worktree, even with `-buildvcs=true`. The release driver correctly rejected
that executable before reading historical state. Mainnet build preparation
must preflight `go version -m` for the exact revision and `vcs.modified=false`
before invoking any long-running command. If the linked worktree cannot
produce a stamped binary, build from a clean full clone of the approved
revision. Record the source revision, executable digest and preflight result
with the diagnostic; a later script edit must not be presented as evidence
that an already-running process passed that preflight.
The follow-up current-main payout test first failed to compile because its
Server worktree lacked the required sibling SN checkout, then because its
sibling operator-proxy checkout lagged the Server API. Mainnet qualification
must pin and record the complete Go replacement-module revision set before
testing or building the release. A missing or mismatched sibling source is a
workspace preflight failure, not a payout failure; after repairing the
workspace, rerun the exact normal and race tests against the recorded set.

R46's operator stats and proof reads hit their 100,000-row and 10,000-row
caps while the APIs returned oldest-first history. A healthy response could
therefore show stale scoring and hide the current proof interval. Mainnet
observers must request an explicit, pinned time range for every sampled
operator, reject a full page that might be truncated, and retain the queried
range with the result. A transient GET may retry within its approved budget
without changing that range. API response liveness must be judged from the
response itself; unrelated process-health failures remain separate strict
findings. Verification faults must not excuse a semantically invalid signed
response. Exercise saturated history, changed signing keys, replayed samples,
and later success after earlier errors with deterministic regression tests.

R46 retained 18 receive-sequence exit gaps across 10 swarm processes. The
deployed receive idle rule retires a sequence after 120 seconds while the
sender retains it for 300 seconds; all 18 matched sender/receiver pairs have
their next write inside that mismatch. Actual plain, encrypted and combined
receive/TLS wire fixtures nevertheless recover after 155 seconds idle; the
asymmetry alone is not a demonstrated root cause. Before mainnet, force lost
first acknowledgement, contract-ahead state, carrier replacement,
cancellation and foreign session rejection through the production path.
Preserve exit-gap findings until a causal test proves a fix. The native-1674
gap is separate: durable input objects did not
create an applied validator intent. Mainnet admission must require complete
authenticated native history or a specifically approved fresh generation.

R46's validator-2 relay also exposed a generation scheduling trap. Its
predecessor lacks epoch-603 closure/publication, while the authenticated
successor starts at cutoff 604. A single cursor waited for 603 and never
admitted the successor's 64 closed-census publications through epoch 635;
all ten acceptance-window closed slots lack original requests and journal
owners. Schedule each installed generation from its own cursor and signed
cutoff, never route a predecessor across that cutoff, and keep completion
watermarks monotonic. Test missing predecessor, independently missing
successor, foreign owner, cutoff replay and strict historical missing-file
refusal. This prevents future blockage but does not reconstruct missing R46
custody or satisfy its original acceptance gate.
The next-run review found that the activated generation-1 handoff occupied a
fixed immutable `handoff.json`; a generation-2 activation previously tried
to replace it and failed before selection. Mainnet must keep the original
handoff and its sealed 31-file runtime inventory unchanged, publish each
successor as a separate immutable postcondition, and select it only through
a verified append-only journal receipt. Bind the new handoff to the exact
prior bytes, advancing cutoff, four consents and native custody. Relay,
worker, API and observation readers must agree on the selected generation
while retaining older namespaces at their signed cutoffs. Test duplicate,
torn, foreign and missing selectors, a crash between file and receipt,
hex/raw native hotkey seeds and raw-only client keys. For an already occupied
native slot, complete the authenticated generation-specific source-role
continuation before resuming workers; an absent local intent cannot excuse
an occupied chain slot. The focused successor and source-role normal/race
tests pass, but live generation-2 activation and fresh native history remain
unproven.
The live testnet successor published all four zero-value consents before its
approved activation boundary, with finalized receipts and contiguous keeper
nonces. The apply command then exited with the explicit instruction to resume
after that boundary finalizes; it had completed publication but had not yet
staged the new clients or selected the successor. Mainnet automation must
record this as durable staged progress, reapply the **same** immutable plan
after the exact finalized boundary, and reconcile the four old receipts
without rebroadcast. It must not interpret the nonzero waiting exit as a
reason to replan, abandon the generation or stop the serving supervisor.
The live generation-2 resume exposed a second boundary: both sealed operator
`st.yml` files still pin the generation-1 reserved-upload activation contexts,
while the selected validator handoff uses four new contexts. The original
runtime manifest correctly preserves those old bytes, so a normal fresh
startup refused the mismatch before starting any worker. The provisional
R47 continuation recognizes only the exact authenticated predecessor context
census, leaving every other capacity and authority field unchanged. It is a
diagnostic exception with `final_acceptance=false`: the old operator admission
cannot establish new-generation upload acceptance. Before mainnet, implement
an append-only, generation-specific operator staging configuration and runtime
manifest, authenticate the exact successor context files and budgets, switch
the API process to that config during the stopped-topology cutover, and prove
all four fresh uploads end to end. A source-role or validator-only rotation is
not sufficient. Test stopped/retried cutovers, mixed generations, forged
contexts, changed quotas, and rollback refusal. Cache authenticated runtime
inputs within one invocation so this check does not repeatedly read the full
historical journal while the fleet is stopped.
R47 demonstrated the consequence: both generation-2 validators repeatedly
received HTTP 403 for initial terminal publication while the operator APIs
still admitted only generation-1 activation digests. Each validator exhausted
three five-restart bursts before the mismatch was found. A local diagnostic
repair replaced only the four context references in each operator `st.yml`,
retained byte-exact backups and before/after hashes under the R47 qualification
directory, and restarted only the two API processes. The original runtime
manifest was intentionally left unchanged, so this repair cannot satisfy
final acceptance. Mainnet cutover must rotate the operator configuration and
its manifest atomically before validators start, and an end-to-end test must
assert that generation-2 initial uploads succeed without a restart burst.
Epoch 648 reached on-chain RootMissed status for both operators while the
validators were restarting. Both vault events report a zero funded amount,
even though the coordinator snapshot records nonzero captured deposits. Test
that exact failed-upload, unfunded-root path through the next epoch and
reconcile pool custody and any carry without inventing a funded entitlement.
R47's provisional startup also forecast 34,553 protected publication objects
per hour against a configured 32,768, and 10,947,548 retry requests per hour
against 8,388,608, for each of the four validator/operator pairs. The
diagnostic owner waived that forecast without changing runtime limits. Mainnet
must size both quotas with the approved 2× resource margin against the
worst-case source/retry workload (at least 69,106 objects/hour and 21,895,096
retry requests/hour at this workload) and rerun the forecast using the actual
rendered operator configuration before any accepting interval. A forecast
warning is not evidence that the constrained service can sustain load.
R47's continuous RPC adversary recorded transient LAN-RPC timeouts before the
measured interval. The configured `request_timeout_milliseconds: 10000` bounds
an entire sample containing up to 20 sequential JSON-RPC reads, and its
`rpcAdversary.call` performs one HTTP POST per read without transient retry.
The owned-LAN transport already removes its internal QPS gate; the short
sample deadline and lack of retry remain. Before an accepting mainnet run,
give expected idempotent RPC reads at least 60 seconds of bounded retry,
separate availability recovery from the latency measurement, count every
actual retry request in adversary evidence, and test deterministic timeout,
recovery and exhausted-deadline cases. A single transient read timeout must
not silently turn an otherwise complete multi-hour interval into a hard stop.
The post-R47 candidate raises the configured whole-sample budget to 60 seconds
and retries typed transport failures and transient HTTP responses on direct
idempotent RPC reads while counting recovered wire attempts. This candidate is
not the signed R47 executable. The separate native-runtime metadata probe still
requires equivalent bounded retry and exact request accounting before mainnet
acceptance; its R47 timeout was the first observed RPC error.
The post-R47 package test also exposed two maintenance risks: an isolated
checkout lacked the sibling vault/config repositories required by discovery,
and source-capacity tests pinned old exact byte totals after the approved
census geometry changed. Keep the full module workspace reproducible and
assert the finite document boundary and aggregate safety property directly.
The release gate already partitions the large evidence-census tests and runs
them with a 90-minute timeout. An ad hoc unpartitioned `go test` used Go's
default ten-minute timeout and interrupted that census; production verification
must use the existing partitioned gate rather than treating this as a failure
of the census or weakening its full-size coverage.
R47 also labeled a timed-out public common-block read as a "common-height
disagreement" because the actor decoded the empty response after the transport
error. The post-R47 candidate separates failed reads from successfully decoded
block disagreement and has a deterministic timeout regression. Apply that
ordering to other paired chain comparisons before mainnet so a provider outage
cannot be presented as conflicting finalized chain evidence.
At 2026-09-26 11:42:09 UTC, the R47 verify adversary recorded one HTTP 400
at EXTEND depth 7. The operator-1 API log identifies the rejection as
`source-egress-unresolved` for the assigned pending hop. The owner continued
and retained the error. Before mainnet acceptance, reproduce a disappearing or
unresolved source egress during an otherwise valid trail, specify whether the
controller should return a retryable response or reassign the hop, and test the
adversary's bounded retry and evidence accounting at that boundary. Do not
silently classify this HTTP 400 as an invalid validator signature.

During R47's measured epoch 651, the scheduled second Redis outage made both
validators' native steering uploads return HTTP 500 with a Redis connection
refusal. The owner continued and restored Redis, but the process-log gate
retained the failed attempts as blocking findings. Before mainnet, test the
complete outage-to-recovery sequence: a failed immutable upload must reconcile
its durable object and retry after Redis returns, and the final gate must
distinguish a recovered, fault-scoped attempt from a permanently skipped native
epoch. Keep the missed-epoch and custody checks strict even if an individual
attempt is classified as expected during the signed fault window.
The R47 steering restart exposed a narrower code defect: the upload client
already marks 5xx and transport failures as retryable, but the native steering
loop counted them against its ten consecutive semantic-failure attempts when
the broader provisional deferral permission was absent. Two validators then
restarted despite the owner remaining live. A zero-byte upload acknowledgement
timeout was also joined to an invented unexpected-byte error, erasing its
retryable type. Preserve the exact semantic-failure budget and strict
incomplete-epoch boundary, but let typed transport/service failures retry in
the same process without consuming that budget; test the outage, recovery,
malformed nonempty acknowledgement, and persistent semantic failure cases.
The capacity forecasts in `runtime_client_key_upload_capacity.go` and
`runtime_evidence_source_capacity.go` still multiply native windows by the ten
semantic-failure limit. Transport retries can poll throughout a native window,
so that multiplier is not a bound on outage traffic. Before mainnet, use the
larger of the existing native estimate and the actual horizon divided by the
configured validator poll interval, plus restart allowance, for each native
and settlement path; include retry requests and retained objects separately.
Recheck the rendered server hard caps and the approved resource margin against
that forecast. R47's provisional quota waiver does not establish capacity.

The ordinary post-R47 package partition exposed evidence GET response
ownership lost during cancellation: the request owner returned only the
context error after a response body read or Close had already failed. This
hid a real close failure in the replica evidence gate. Preserve the complete
bounded response read/Close error when cancellation or retry-wait interruption
wins, and distinguish mixed permanent body errors from retryable transport
errors. Deterministic normal and race tests now cover read, Close, retry wait,
and next-attempt boundaries; include this behavior in mainnet evidence clients
and operational diagnostics. The correction is not in the signed R47 binary.

R46's adversarial consensus sampler read the legacy validator-2 intent file
while the scenario observer selected its approved provisional/V2 generation.
Select one authenticated generation for both the attack vector and metrics;
reject missing or forged selected sources without falling back to legacy
state. An actor-only emulation success does not prove fresh native application,
mask coverage, or independent cohort coverage. R46's retained validator-2
local intent predates its signed baseline, validator-1 still lacks local
intent, and the original adversarial campaign remains failed. The separate
production-soak dishonest-deposit helper also reads a legacy intent path.
Before production soak, bind that helper to strict applied source authority
and test missing/forged generation controls. It is outside the release
interval path and must not be used to loosen R46 or R47 acceptance.

An isolated sync with newer SN25 main exposed a separate cross-repository
release boundary. SN25 changes `protocol.RequiredDepositRao` from three to
four arguments by adding user count, while the available Server checkout
still calls the three-argument form; the available SDK lacks the wallet
challenge API now required by the SN25 miner. Its validator also scores
baseline-priced demand in rao, while the terminal semantic verifier still
requires the older byte-derived score. Pin compatible SN, Server and SDK
revisions together, update the verifier's value model, and exercise positive,
zero-price and per-user-only cases before adopting that mainline version for
a testnet release or mainnet. The R46 integration branch has not adopted this
unqualified sync.
