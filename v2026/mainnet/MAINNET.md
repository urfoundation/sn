# Mainnet launch and operations plan

Updated 2026-10-01. **Mainnet activation is blocked.** At the user's direction,
preparation now uses the [Rao Foundation public archive RPC](evidence/public-archive-switch-20261001.md)
until Snow finishes synchronizing. The archive returned mainnet genesis and
EVM ID 964 and supports the tested historical runtime read. Its public method
profile lacks `debug_getRawHeader`; the qualified exact-header fallback now
passes a [live combined native/EVM finalized snapshot](evidence/public-finalized-snapshot-20261001.md).
Independent identity and
runtime-source approval remain open. No transaction has
been sent through either route. The earlier read-only
[Snow/LAN RPC comparison](evidence/snow-rpc-route-20260927.json) showed that
`http://172.28.208.185:9944` served the same **testnet** chain as
`http://192.168.1.162:9944`: EVM chain ID **945** (`0x3b1`), rather than the
expected mainnet ID 964. During the operator's node-data move, the
[13:42–13:47 UTC read-only retry on September 28](evidence/snow-route-observation-20260928-0115.md#follow-up-recheck)
returned HTTP 502 for chain-ID reads across 21 attempts. That is an unavailable
route, not a mainnet identity. Verify the restarted route and independently
approve the mainnet chain identity before admitting any signer.

**September 30 operator report:** Snow mainnet is still synchronizing. This is
the current operator report, not a new verified RPC observation or a mainnet
identity/readiness attestation. Live activation remains closed while sync,
independent chain identity and the other production gates are unresolved.

The [two-UR current-admission route](VALIDATOR-CURRENT-ADMISSION.md) has
[scoped qualification](evidence/validator-current-admission-qualification-20261001.md)
for separately signed initial starts at frozen SN `b8dc332a` with server
`0b8e758d`. It freshly binds original CREATE/anchor custody and EVM scan
floors to native eligibility, strict majority stake capacity, current contract
views and both operator/key/proof domains. Exact independent current-policy and
lifetime signer/host-custody acceptance are required. Empty authenticated local
tails are the initial scope; unfinished intents require separate recovery.
Permanent per-unit claims, post-sync rechecks and responsive generation evidence
preserve one-start semantics. Actual approvals, live admission/systemd rehearsal,
root-service authority and applied 10/90 evidence remain unresolved. No unit or
signer was activated by implementation or tests. Separate bounded SN compatibility
with server `720e7c61` passed; schema 750/subscriber-v2 rollout remains outside
that evidence.

Sim-testnet is closed with known exceptions at the user's direction. The
[original R48 report](../sim-testnet/FINAL-4.md) remains a failed provisional
attempt with **zero completed acceptance epochs**. Its later retained resume
recovered services with no setup actions dispatched and explicitly retained
`final_acceptance=false`; it did not produce final acceptance. The process-log
scanner overrun, missed native epoch, policy-scoped client-key rollover,
historical R46 handoff provenance bug and usage debt are mapped to concrete
production work in [the gate tracker](PRELAUNCH-FIXES.md#production-gates-in-execution-order).
Do not restart testnet or make a passing testnet result a fictional input to
the mainnet plan.

The original bootstrap design was based on SN
`a59294e98ea02d05125015ae02cf32f2c0059c8a`. Subsequent implementation and
qualification must use an explicit composed release, including compatible
SN/server/SDK/Connect/config revisions. Some shared and simulator fixes exist;
the complete mutating bootstrap, root-validator service and operational repair
system remain production work.

**Server `720e7c61` is a separate integration candidate.** The
[static review and qualification gate](evidence/server-720e-integration-review-20261001.md)
requires migration **750** before those binaries take traffic, plus an exact
SN/server compatibility receipt and successor release/source-image inventory.
The [independent disposable-schema check](evidence/server-schema750-qualification-20261001.md)
passes the selected 749-to-750 readiness, default-off, seed-picker, normal/race
and vet scopes. It also proves a mixed-writer v2 activation blocker: an old
UPSERT can retain a prior positive quality attestation after changing a row.
Keep the launch `provider.yml` subscriber policy absent/`0` and the v2 candidate
classifier/MMDB out of active inputs unless both operators' actual miner cohorts
prove fresh trails through the SN Quality/force-minimum seed picker. Under v2,
unknown or legacy connection facts are excluded even from fallback and named
selection; service health alone cannot prove trail progress. The existing
contract/operator/activation qualifications remain scoped to server `0b8e758d`
until the separately recorded compatibility gate passes. This review does not
change live policy, rebuild artifacts or approve deployment.

The [schema-751 mixed-writer successor](evidence/server-schema751-write-guard-20261001.md),
server `a464bb3e`, adds a per-write token/trigger and revokes existing unbound
positives. Its author qualification passes 27 selected roots normal/race and
vet, including the full live guard and warm-cache/rollup behavior; removing the
trigger fails both causal model controls. Independent normal/race/vet and
trigger-omission controls also passed. The correction is merged into server
`main` at `94229abb` but absent from the pinned server-720 release. Keep v2 off while successor
source/image qualification, schema-751 lock-duration checks, complete writer/API
rollout, lookup coverage and both operators' actual miner-trail/load canaries
remain open. Readiness permits an older binary on the newer schema and does not
establish that policy readiness.

[mainnet/main.go](main.go) implements signer-free
`inspect`, `runtime-snapshot`, `finalized-mapping`, `finalized-snapshot`,
`monitor`, `subnet-discover`, `subnet-preview`, `owner-trim-plan`, `owner-trim-recheck`,
`owner-trim-reconcile`, `owner-trim-qualify`, `root-preview`, `root-monitor`,
`check-recycle-mode`, `economic-reference`, offline `source-lock`,
[local `release-inventory`](RELEASE-INVENTORY.md), and the
[signer-free blocked `plan`](PLAN.md). Two narrower executable phases now exist:
[`bootstrap plan/apply/resume`](BOOTSTRAP-ROOT.md) retains local root custody
and imports externally signed payloads, while
[`bootstrap-contracts preview/plan/apply/resume`](BOOTSTRAP-CONTRACTS.md)
prepares the first eight installation actions through unanchored validator
evidence CREATE under the approved transaction journal. The
[`bootstrap-chain plan/apply/resume`](BOOTSTRAP-CHAIN.md) command composes their
offline custody preparation with a retained trim review and two protected UR
role inputs under one restart-safe local journal. Its v2 preparation verifies
both initial schema-3 signed configs against independent role/runtime inputs;
its [offline qualification](evidence/ur-bootstrap-admission-qualification-20260928.md)
keeps live producer eligibility and service activation as separate gates. V3
independently approves the separate root-service configuration. The read-only
`bootstrap-chain readiness` increment binds that accepted v3 preparation and
its original child journals to one current finalized census, with distinct
UR/root generation, permit, window and checkpoint blockers. Its
[exact-source qualification](evidence/bootstrap-readiness-qualification-20260929.md)
passes all ten new normal/race roots, the 148-root adjacent normal/race scope,
vet and four causal controls. The redundant broad race process was intentionally
terminated after its passing prefix; the exact 148-root disjoint race union is
the qualified coverage. The later [composed source check](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/COMPOSED-RESULT.md)
passes focused normal/race, vet and fences on SN `e35771ec` and server `b7c8c743`.
It retains false activation/current-authority flags and all pending chain phases.
The [offline contract prerequisite increment](BOOTSTRAP-CHAIN.md#offline-contract-installation-prerequisites)
exposes the original eight-attempt/nine-action mismatch before signing and
inspects original action custody without replay. Its [focused qualification](evidence/bootstrap-contract-prerequisites-focused-qualification-20260929.md)
passes twelve new roots and six causal controls normal/race. The separate
[partial adjacent battery](evidence/bootstrap-contract-successor-full-v3-qualification-20260929.md#separate-composed-smoke-and-partial-adjacent-coverage)
passes 150/270 roots in both modes; 120 remain unrun. Eight retained successful actions
leave only the evidence anchor unfinished. The separate
[execution custody and owner](BOOTSTRAP-SUCCESSOR-EXECUTION.md) now implements an
independent approval domain, exact eight-action adoption, separate local nonce
registry claims and durable cumulative one-send/reconciliation machinery.
Its [scoped qualification receipt](evidence/bootstrap-successor-execution-qualification-20260929.md)
records twenty-one focused and six adjacent roots passing normal/race on
corrected `75ea2158`, with ten causal control pairs and a verified forty-five-file
evidence manifest.
The first private-input fixture failures and the later ten-minute race
harness timeout remain preserved. The exact race retry passes with an explicit
twenty-minute package budget, without changing production deadlines.
The concrete [canonical execution adapter](BOOTSTRAP-SUCCESSOR-EXECUTION.md#separate-canonical-authority-and-online-resume)
has [scoped independent qualification](evidence/bootstrap-successor-canonical-qualification-20260930.md)
on corrected frozen source `a7186754`: ten focused and twenty-two adjacent roots
pass normal/race, fourteen normal causal controls and six selected race controls
reach their intended assertions, and the sealed source/module fences match.
Preliminary `82da3d40`
passed all eight roots normal/race before the Safe provenance gap was identified;
those results do not qualify the corrected source. Online resume requires a
separate signature binding Safe build review, a reviewed current runtime,
signer-cutover evidence and a separately signed exact Safe deployment/storage
history statement.
It reauthenticates all eight original receipts through the existing native/EVM
adapter, checks scoped finalized/pending Safe and contract state, reconciles the
exact retained transaction and retains one-counted-write machinery. The current
runtime may differ from the historical original runtime under that new approval.
The ten roots include actual pinned Safe proxy/singleton execution and
lost-reply recovery in two full local graph fixtures with an explicitly synthetic
history capability. RPC finality and pending
state remain owned-node assertions; external build review and complete signer
cutover remain independently attested assumptions, not facts proved by a local
registry or a global transaction-pool census. No live authority is established.

**P0 gate: canonical Safe deployment and storage provenance.** The original
complete-history submission route remains unavailable. Without the separate v2
current-only opt-in below, public `--submit` exits before custody loading or attempt
reservation. A signed
history report is necessary review input, but cannot provide the missing
`bootstrapSuccessorSafeProvenanceAuthenticator`. Current Safe owner/module
getters cannot exclude nonzero mapping entries unreachable from their sentinel
lists; tests inject real orphan owner and module entries into the published code
and demonstrate this gap. Implement and independently qualify deployment,
initialization and complete authority-relevant storage/delegatecall history
through finalized and scoped pending state before enabling complete-history sends. The
adapter must bind the exact Safe/profile, approved route and signed evidence;
ordinary getters or a loosely labeled file cannot substitute. Read-only
reconciliation remains available. The separate
[qualified readmission increment](evidence/bootstrap-successor-readmission-qualification-20260930.md)
`cd4261a8` moves expensive history authentication before final pending
Safe/relayer nonce and Safe-state admission, and invalidates earlier admission
when a refresh fails. Both heavy roots and three selected adjacent roots pass
normal/race; both causal controls reproduce their intended failure in both
modes, with exact source/dependency evidence sealed. This does not supply the
missing history authenticator. MG-08 remains open.

The separate [bounded Safe archive census](SAFE-HISTORY-CAPTURE.md) has
[sealed offline qualification](evidence/safe-history-census-qualification-20260930.md)
on paired SN `36fea176` and server `d21492c3`: 68 positive root executions pass
normal/race; ten normal controls and five selected race controls are causal,
including a distinct normal published-Safe fixture-oracle control. Its actual
read-only command retains private durable witnesses, reuses the native
ordered-trie and server receipt decoder, accepts unrelated traffic, and retains
exact direct calls and committed logs over a pinned interval. Complete internal/reverted
execution, native hooks, clean deployment and pending authority remain unproven;
the report keeps every history/send verdict false. This evidence layer does not
implement the missing provenance authenticator or change the public-submit gate.

The [qualified native trace increment](evidence/safe-history-native-trace-qualification-20260930.md)
adds bounded SDK block traces and authenticated parent runtime code proofs to
that retained census, with canonical closing checks. It marks SDK-filtered
ClearPrefix/root events and missing rollback or inner/reverted EVM execution as
unproven. The complete-history authenticator and public submit gate remain
open; no live node trace was qualified.

**Qualified read-only Safe current-authority proposal.** The separate
[current-storage proof](SAFE-CURRENT-AUTHORITY-PROPOSAL.md) at `aa9f715b`
authenticates every storage word under the exact Safe's native account prefix,
the reviewed runtime and published proxy/singleton code plus native metadata at
one canonical finalized snapshot. Missing intersecting branches, orphan owner or
module mappings and any extra storage refuse admission. The
[qualification note](evidence/safe-current-storage-qualification-20260930.md)
records eleven new and ten adjacent roots passing normal/race, all ten normal
and exactly five selected race controls causal, with source/dependency evidence
sealed. This is read-only evidence and a distinct
signed policy proposal, with no history claim, custody import or public-send
capability. Existing native RPC cannot prove a complete pending overlay; the
final scoped recheck reports that limitation explicitly.

**Qualified current-policy custody increment.** Frozen source
`3f88a948` adds the [separate signed acceptance journal](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CUSTODY.md),
exact counted/outcome references and partial-publication recovery under the
original exclusive owner. Later policy or runtime approvals cannot rewrite an
earlier counted outcome, reset attempts, release nonce claims or reduce maximum
liabilities. The original history statement remains retained and no historical
truth is inferred from the current-only proof. The
[qualification note](evidence/safe-current-custody-qualification-20260930.md)
records eight new and thirty-one adjacent roots passing normal/race, all eight
normal and exactly five selected race controls causal, and sealed exact
source/dependency evidence. That custody increment installed no public import
flag or production capability; the separately qualified native capability below
adds read-only import and the internal proof path. The later public v2 route below
requires an explicit new independent risk-policy acceptance.

**Qualified native current-policy capability.** The separate
`95a905d4` [capability](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md)
connects independently signed current-policy custody to the concrete native
complete-prefix verifier. Public online resume can import pinned acceptance
files for local custody and historical reconciliation. That September 30 source
kept public submission closed; its v1 acceptance remains public read-only.
An internal route requires the complete retained runtime tip and both policy
signatures, keeps proof work before final scoped pending checks, and preserves
one exact counted send. Its proof block/hash/root remains separate from any
later admission-window head. The
[qualification note](evidence/safe-current-capability-qualification-20260930.md)
records two new and thirty-five adjacent roots passing normal/race, all eight
normal and exactly five selected race controls causal, and the sealed exact
source/dependency evidence. Two original control-oracle mismatches remain
unresolved in the receipt, with fresh corrected reproductions retained separately.
No policy approval or public-send activation is claimed; the original signed
history statement remains retained and unproven. Complete pending storage and
independent finality are not established by the owned RPC observations.

**Public current-only Safe route, October 1.** The [v2 acceptance interface](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md#explicit-public-v2-acceptance)
adds a separate signature domain and exact `--accept-safe-current-policy` opt-in.
The latest independent acceptance must expressly allow bounded public submission
and accept owned-RPC finality, non-atomic scoped pending checks, signer cutover,
absent complete history/pending proof and changes between reads. V1 cannot be
upgraded by a flag. Both signatures, original custody and the complete runtime tip
remain required; wrong or absent acceptance refuses before reservation or send.
The route uses the existing complete finalized prefix proof and repeated final
readmission, preserves the original exact signed bytes/nonce/attempt/liability,
and leaves historical read-only reconciliation available after interruption.
The [qualification receipt](evidence/safe-current-public-qualification-20261001.md)
records 68 author roots passing normal/race, three causal control pairs and vet;
independent review passes 14 focused roots normal/race and vet on its separately
pinned server graph. The exact source and 79-file evidence manifest are sealed.
Actual independent production
v2 acceptance, signer cutover, live authority and installation remain open MG-08
gates. No risk policy was accepted and no live transaction was sent in this work.

**Original-authority contract installation readback.** The
[anchor/readback producer](BOOTSTRAP-CHAIN.md#original-authority-evidence-anchor-and-installation-readback)
joins all eight original receipts, the exact counted Safe anchor and its binding
event to complete five-contract and Safe storage proofs at anchor inclusion.
Its stable installation identity preserves the original approvals, terminal
journal and receipt across later observations. A separate current snapshot proves
complete Safe storage and the existing executable/domain views without treating
normal accounting or a later observed Safe nonce as new transaction authority.
The typed producer lets service admission share its selected finalized boundary;
a JSON report cannot authorize a service. Readback makes no network write but
can finish the original local terminal journal after interruption. Current-only
v2 policy selection/signature, signer custody, live installation and service
activation remain external gates. Complete history is not inferred from current
storage. See the [scoped evidence](evidence/contract-installation-anchor-20261001.md).

**Contract-to-validator declaration admission — scoped qualification complete.** The
separate signer-free [contract-role plan](BOOTSTRAP-CONTRACT-ROLES.md) binds both
signed UR configs to the approved coordinator proxy, vault and initial policy
identifier, with the evidence journal's exact immutable domain. Pairwise config
agreement cannot substitute for this deployment binding. The original preparation
and recovery scope stay unchanged. This earlier declaration check alone does not
verify canonical installation, the evidence anchor, deployment scan floors,
current state or service activation. It does not advance a durable chain phase
or install a public send route. The corrected `f4470d0d`
[qualification receipt](evidence/bootstrap-contract-role-qualification-20260930.md)
records 40 positive executions (eight focused and twelve adjacent roots, each
normal/race) and three causal controls in both modes. The earlier `43dcd01f`
fixture failure remains separate evidence.

**Original contract receipt admission — scoped qualification complete.** The
[read-only receipt command](BOOTSTRAP-CONTRACT-RECEIPTS.md) reauthenticates all eight
exact original native/EVM inclusions and historical postconditions, with separate
per-receipt budgets and final canonical continuity. Its signed EVM scan-floor
check excludes omission of the original deployment prefix; normal finalized-head
advancement is allowed. The `c6b31fdb`
[qualification receipt](evidence/bootstrap-contract-receipts-qualification-20260930.md)
records five focused and sixteen adjacent roots passing normal/race (42 positive
executions), five normal causal controls and exactly three selected light race
controls. Current state, the evidence anchor, complete indexing, installation
and service activation remain unverified; all original pending phases remain.
No live RPC, transaction or public Safe route is part of this increment.

**Current bootstrap contract fields — scoped qualification complete.** The
[read-only current-state command](BOOTSTRAP-CONTRACT-CURRENT.md) composes exact
original receipt admission with the original five-account bootstrap profile at
one finalized native/EVM mapping. Exact runtime/getter/slot replies remain
owned-RPC assertions. The snapshot stays distinct from later canonical continuity;
normal head advancement is accepted. The `2b87b133`
[qualification receipt](evidence/bootstrap-contract-current-qualification-20260930.md)
records three focused and sixteen adjacent roots passing normal/race (38 positive
executions), all six normal causal controls and exactly two selected light race
controls. Original zero-activity/policy requirements remain strict. An unset or
expected evidence pointer grants no anchor-history claim; complete storage, Safe
authority, installation and activation remain unverified. No public send route or
live mainnet action is introduced.

**Qualified runtime authority increment.** The
[additive revision path](BOOTSTRAP-SUCCESSOR-RUNTIME-REVISIONS.md) at
`3d526830` retains separately signed runtime revisions under the original
independent key and immutable base authorization. Each revision binds reviewed
artifact/codec evidence and its exact predecessor. Original receipts, signed
transaction bytes, both nonce claims, counted attempts and full liabilities stay
intact. Current admission matches approved complete artifacts; historical reads
select the inclusion/parent pair from the full history, including more than ten
retained profiles. [Independent qualification](evidence/bootstrap-successor-runtime-qualification-20260930.md)
passes ten new and thirty-six adjacent roots normal/race, all twelve normal
controls and exactly seven selected light race controls. Exact source/dependency
evidence is sealed; the earlier canonical/readmission receipts remain unchanged.

**Runtime continuity policy proposal — inspection boundary qualified.** The
[signed compatibility envelope and inspector](RUNTIME-CONTINUITY-POLICY.md)
bind original production-validator authority to a separate semantic verifier,
exact future artifact, complete consumed-interface/economic scope and output
provenance. Inspection checks the owned finalized snapshot and later continuity
without changing the original signing view or custody. A genuine semantic
verifier, proof replay and durable production selection remain absent; a matching
metadata profile or signed assertion alone cannot open fresh signing. MG-04 and
RT-04 remain open. The miner's existing original-runtime recovery is unchanged.
The [scoped qualification record](evidence/runtime-continuity-policy-qualification-20260930.md)
records fifty positive root executions, seven normal and three selected race causal
controls, exact source/dependency seals and the separate original failed fixture
attempt. This qualification supplies no semantic verifier or production selection.

**Finite runtime replay — execution boundary qualified.** The separate
[offline transition executor](RUNTIME-SEMANTIC-REPLAY.md) now checks exact old/new
Wasm against explicit finite state cases under a signed executable/rules/evidence
boundary and a bounded subprocess owner. It compares return bytes and full declared
storage effects in on-chain context. This is finite fixture coverage, not complete
economic equivalence or authenticated mainnet state. Automatic selection and fresh
signing stay closed; no production route is installed. The
[sealed qualification](evidence/runtime-semantic-replay-qualification-20260930.md)
records 48 Go positive executions normal/race, 19 Rust tests normally, eight normal
and four selected Go race causal controls, with exact source/build/dependency seals.

**P0 follow-up: automatic compatible runtime admission (RT-04).** The additive
path still requires a new independent signed artifact review for each upgrade.
It does not supply automatic compatibility or eliminate that live approval gate.
Design and qualify a separately approved compatibility authority and preserve
every historical profile, signature, nonce claim, attempt and liability. Same-version
changed artifacts and unsupported codecs remain explicit closed gates. The
missing genuine Safe history authenticator and public-submit gate are unchanged.
The qualified [signed local successor preparation](BOOTSTRAP-SUCCESSOR-PREPARATION.md)
adds a separate approval domain and fixed resumable local claim while preserving
original receipts and additive proposed floors. Its [scoped receipt](evidence/bootstrap-successor-preparation-qualification-20260929.md)
passes thirteen focused and three adjacent roots normal/race, plus thirty-two
intended causal executions. It provides no executable allowance, Safe authority
or signing; a different physical custody root requires approved migration.
Its [separate six-root composed smoke](evidence/bootstrap-successor-preparation-qualification-20260929.md#separate-composed-smoke)
passes normal/race on exact merge `93a0a060`, including both full-v3 commands,
same-root recovery, pure Safe calculations and MG-07 incident recovery.
The [qualified unsigned successor proposal](evidence/bootstrap-contract-successor-qualification-20260929.md)
retains the eight completed seals and any ninth reservation while computing
additive attempt/lifetime ceilings for the unfinished anchor and retry margin.
Six new and twelve inherited roots pass normal/race with six causal controls in
both modes. The [separate full-v3 public-command fixture](evidence/bootstrap-contract-successor-full-v3-qualification-20260929.md)
now passes normal/race and all six intended causal executions on `c294fefd`.
It uses genuine local eight-action execution and original v3 preparation. Its
finite 60-second local send budget is approved before custody; the earlier d7
one-second race timeout remains preserved as a noncausal attempt.
The earlier fifteen-root MG-07/prerequisite composed smoke passes both modes on
`79ff2c6e`, and the later twelve-root successor composed smoke passes both modes
on `1e2b2abb`. These scoped results supply no Safe authority, signed adoption or
live installation; the separate SDK count remains 543/618 with 75 pending.
The [qualified offline Safe release verifier](evidence/safe-release-profile-qualification-20260929.md)
checks explicit 1.4.1/1.5.0 Safe/SafeL2 published proxy/singleton code, ABI,
source/compiler inputs and storage layout. Its six new and four adjacent roots
pass normal/race with sixteen intended causal executions. Independent rebuild,
current account and initializer-owner binding, authority, signing and execution
remain false. Existing versus new Safe is unresolved; a different new address
requires separately authorized ownership migration from the retained owner.
The [qualified pure Safe evidence layer](evidence/safe-execution-evidence-qualification-20260929.md)
binds the exact EIP-712 digest, supplied signature structure/recovery and declared
inner outcome/nonce semantics to those published profiles. Thirteen focused and
four adjacent roots pass normal/race; all twenty-six causal executions reach
their intended assertion. The corrected oracle filters the selected variant
before decoding; the earlier e074 setup failures remain preserved as unqualified.
This layer has no command, signer, current owner/threshold proof, canonical
receipt authentication, custody or execution path. The artifact verifier's
separate nine-root composed smoke also passes normal/race on `18a88db4`.
The pure layer's [separate four-root composed smoke](evidence/safe-execution-evidence-qualification-20260929.md#separate-composed-smoke)
passes both modes on `c648495f`; it predates the signed local preparation merge.
The [offline successor Safe review](BOOTSTRAP-SUCCESSOR-SAFE-REVIEW.md) has
[scoped independent qualification](evidence/bootstrap-successor-safe-review-qualification-20260929.md)
on `d0207448`. It joins the completed signed preparation, all eight original
receipt seals and the full selected published release to the exact evidence
anchor digest. It borrows custody read-only, refuses partial claims and retains
both completed and unexecuted original liabilities in its proposed outer cost.
Test compilation and vet pass. Independent runs pass nine focused and four
adjacent roots in both normal and race modes; all fourteen causal executions
reach their intended assertion. The sealed logs, patches, exact source and
dependency graph were independently checked.
The focused roots include a full-v3 public-command fixture and the published
Safe proxy bytecode oracle.
This is unsigned review: owner signatures and complete outer calldata are absent,
and no nonce, budget, execution approval or global custody is allocated. The
native review window cannot expire a Safe signature because it is outside the
Safe digest. Canonical original receipt adoption, current Safe/evidence authority,
signature lifetime and window enforcement and globally fenced relayer signer
custody remain launch gates. The separate execution owner enforces its conditional
state transitions in code; its concrete canonical adapter has scoped independent
qualification, while the production Safe history authenticator and explicitly
approved live authority remain missing.
No mainnet transaction,
contract installation or validator activation is established by this increment.
The [qualified owner-trim action](evidence/owner-trim-null-storage-repair-20260929.md),
integrated at `ce567305`, adds durable exact-action recovery and actual-subset
reconciliation under original v3 custody. Sol's 25 focused roots and exact
229-root expanded union pass normal/race, with vet and eight causal controls.
The failed first candidate `4033609` and its null-storage failure remain
preserved; the successor corrects the production proxy reader. Its separate
approval cannot replace current authority: production signing and submission
remain blocked on the named enforcement and custody capabilities. The pending
best-effort risk-policy choice is not assumed or enabled. The owned
Snow route still returned HTTP 502 at 06:59 UTC on September 29, providing no
current mainnet identity or authority. The complete bootstrap,
Safe evidence anchor, native signing device and live role
activation remain unfinished. The
[retained Snow inspection](evidence/snow-route-inspect-20260927-1051.json)
observed chain ID 945, which fails the required mainnet ID 964 gate.
This plan and its read-only evidence perform no
mainnet transaction, deployment, UID removal or validator activation.

## Bootstrap prerequisites and current blockers

| Prerequisite | Current disposition and next result required |
| --- | --- |
| Interim public mainnet RPC and independent identity authority | The [October 1 public archive capture](evidence/public-archive-switch-20261001.md) observed mainnet genesis, EVM ID 964 and runtime spec 470. The [qualified exact-header fallback](evidence/public-header-fallback-20261001.md) passed a [live combined native/EVM finalized snapshot](evidence/public-finalized-snapshot-20261001.md) without `debug_getRawHeader`. Independent genesis/runtime/source approval and a current complete SN25 census remain outstanding. Snow remains an unsynced future failover. Pin the public archive for read-only discovery and unsigned plans; recheck finalized identity before signing. |
| Immutable qualified release | Compose the actual SN/server/SDK/Connect/config and contract artifacts, including selected branch fixes and migration order; qualify their real interfaces and publish an approved manifest. Historical R48 builds do not qualify later per-user deposit or zero-price changes. |
| Exact mainnet census and authority | Read SN25 membership, roles, custody, immutable contracts and locks at one finalized snapshot; resolve reset feasibility and all protected identities before making an executable plan. |
| Economic and custody decisions | The user selected **owner-recycle for the remaining 90%**. Implement and qualify that path and the 10% native-miner target on the actual runtime; finalize mainnet policy, tolerance, keys/Safe, root-registration protection and spend/count/expiry ceilings. Recycled value is not reserve custody. No testnet allowance carries over. |
| Production safety and liveness | Close the linked recovery, runtime, policy/identity, settlement and monitoring gates; retain independent history and complete signature/nonce ownership. Testnet provisional exceptions grant no mainnet authority. |
| Operations and staged acceptance | Install monitor/alerts in the existing telemetry stack, name primary/backup on-call, rehearse bounded repair and rollout/rollback, then collect actual native and settlement evidence after an approved activation. |

Read-only inspection and offline implementation can proceed while required
inputs remain unresolved. The planner must expose those blockers and refuse
mutating phases until their exact dependencies and authorization are complete.
The current Snow xops `vars.yml` still selects `testfinney` with EVM ID 945 and
the testnet genesis. Its [prepared cutover guard](https://github.com/urnetwork/xops/commit/ec443da)
is not deployed: it requires the data mount in both full-host and isolated
lightnode rollouts, renders a network-specific bootnode including finney's
`/ws` transport, and rejects mixed testnet/mainnet identity inputs. Before
starting the mainnet node, select distinct reviewed node generations, the
approved finney genesis and runtime pins, EVM ID 964, bootnode host/port/peer,
and reference route as one configuration. Observe the started node's finalized
identity; configuration checks alone do not establish it.

## Requested outcome and decisions

The bootstrap must deliver all four requested outcomes:

1. Reset the existing miner registrations on our UR subnet (Bittensor SN25, netuid 25), with an exact census and an explicit meaning of reset.
2. Install and initialize the production contract set with the approved custody and governance identities.
3. Begin provider rewards at **10% of the native miner allocation**. This is not 10% of all subnet emission, not a validator take, and not the head/tail steering parameter.
4. Operate both an owned **root validator on netuid 0** and an owned **validator on the UR subnet**.

The target UR mainnet netuid is SN25 (netuid 25). The user selected `owner-recycle` for the other 90% on 2026-09-27; this recycles native allocation and does not fund our reserve. The verified mainnet RPC route, keys, spend ceilings, reset mechanism and exact runtime implementation remain inputs to the executable plan. None has a default mainnet address or financial allowance. Existing testnet spend approvals do not authorize mainnet spend.

Two constraints determine the implementation. There is no demonstrated subnet-owner call that arbitrarily clears every miner registration while retaining an arbitrary list of validators. Also, the current UR contracts and validator policy do not provide a standalone switch that changes the native miner allocation to 10%. The planner must expose these as capability decisions, not claim that lowering UID capacity or setting `theta: 0.1` fulfills them. The requested 10% target allows the exact runtime's explicitly established quantization tolerance; a stronger enforceable hard cap is a separate assurance choice, not an additional user requirement.

The standard UR validator now implements the separately approved
[schema-3 production path](OWNER-RECYCLE-PRODUCTION.md) for the measured 10/90
owner-recycle row. Live native economics and migration of already signed work
remain unqualified. The read-only root observer alone cannot activate a root
signing service; qualified current authority, actual native custody and service
wiring remain explicit gates. Zero-price/equal-demand support changes operator
demand/deposit semantics; it does not by itself cap native miner allocation or
choose where the remaining 90% goes.

The miner fleet now has a [mainnet runtime authority gate](../miner/FLEET-MAINNET-RUNTIME.md)
for register, publish, bind, status and revoke. It requires separately approved
genesis, source/build review and exact code/metadata/version bytes before
signing, submission and receipt readback. That source change does not supply
those approvals. Its separately qualified durable recovery retains original
signed transactions and reconciles uncertain sends; partial-scan checkpointing,
automatic runtime admission and live deployment remain separate work.

The draft policy is `reset.mode: unresolved` and **`emissions.remainder: owner-recycle`**. A preview remains non-executable while reset capability, the runtime-specific 10%/90% mechanism or other required inputs are unresolved. Qualify the selected economic mechanism before installing an immutable vault or removing existing registrations. The remainder choice is settled; live economic qualification and the exact signed production policy remain work.

## Source and runtime boundary

The September 14 design inspected Subtensor [commit `67dcf7f791dc495064c293f080a0702cb433e51e`][subtensor-commit], dated 2026-09-07, following the release-455 merge, in `RaoFoundation/subtensor`. The source-specific capability observations below describe that baseline, **not an attestation of the current mainnet Wasm**. Recheck them against the selected live runtime and approved artifacts before planning any action.

The production inspection gate must authenticate one finalized native block and its corresponding canonical EVM block using a separately verified, operator-owned mainnet node. Require an independently approved genesis hash, EVM chain ID 964, native chain identity, complete runtime version, `:code` hash, metadata hash, node build identity, and the reviewed runtime source/artifact mapping. Verify the finalized native header's complete SCALE bytes against its hash before using that hash for state reads; a header number and same-height lookup are insufficient. Decode one complete runtime identity, rejecting contradictory `stateVersion`/`systemVersion` aliases. The `runtime-snapshot` command captures exact finalized code and metadata bytes, verifies code against its storage hash, and repeats canonical/network checks after reading them. Its output remains an unapproved observation. Admission still needs signed-extension, call, storage and precompile review, an independently reviewed source-to-Wasm mapping, and native/EVM finalized mapping. A matching `specVersion` alone is insufficient; [the existing runtime authenticator](../crv4/runtime_identity.go) already binds more than that number.

For native/EVM mapping, the current Snow testnet header carries a Frontier
`fron` consensus digest whose payload names an EVM block hash. The signer-free
[`finalized-mapping` command](FINALIZED-MAPPING.md) authenticates the native
header, decodes the reviewed digest variant, fetches raw EVM RLP by that hash
with canonicality required, reproduces its Keccak hash, and checks canonical
lookup using the EVM header's decoded number. Equal block numbers alone are
not a mapping. Its [Snow evidence](evidence/finalized-mapping-snow-20260927.json)
remains unapproved until the selected mainnet identity, runtime and source
artifact are independently reviewed. The signer-free
[`finalized-snapshot` command](FINALIZED-SNAPSHOT.md) now captures runtime
bytes and this mapping under one authenticated finalized native hash, with
final canonical rechecks after both reads. Its [Snow evidence](evidence/finalized-snapshot-snow-20260927.json)
reproduces code, metadata, native header and EVM header hashes at that one
block. Two separate latest-head observations still cannot be joined into one
launch-plan authority merely because their chain IDs match.

The read-only observation at **2026-09-27 04:16:25 UTC** compared Snow VPN
`http://172.28.208.185:9944` with LAN testnet `http://192.168.1.162:9944`.
Both returned `system_chain=Bittensor`, `eth_chainId=0x3b1` (945), node version
`4.0.0-dev-e18ca67f1a0`, genesis
`0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`,
and finalized native head
`0x3e9119c77dcb7b12557035023f9ad3dbadc60f24443d01c0f32a6c14d81f35d7`.
The [raw identity record](evidence/snow-rpc-route-20260927.json) has
`same_identity_and_head=true`. This is an observed **testnet genesis and route**,
not an approved mainnet genesis. A node's `Bittensor` display name is insufficient
network authority. Both routes must be rejected for mainnet in this state.
The later [07:41 UTC Snow readback](evidence/snow-route-inspect-20260927-0741.json)
still returned EVM ID 945 and the same testnet genesis; no mainnet route cutover
has been observed.

A later mainnet deployment at either address requires the correct owned route,
fresh readback and independent operator approval of the genesis/runtime domain.
Do not guess a different port or inherit a library/public fallback. Bind RPC URLs,
resolved upstreams, TLS identities where applicable and local proxy routes in the
plan. The Foundry configuration no longer defines public `mainnet` or `testnet`
RPC aliases; deployment and probe commands require an explicit owned URL. A
loopback proxy must have the approved owned mainnet node as its sole
upstream. Preserve zero artificial request pacing on that route; bound
concurrency, retries and cancellation. A separately approved read-only comparison
node is an independent observer, never a silent signing/submission fallback.
Protocol block windows and on-chain rate limits still apply.

The standard UR validator's native submission path uses
`author_submitAndWatchExtrinsic` and requires an explicitly approved WS/WSS
route. Its HTTP native-read adapter does not provide subscriptions. Configure
HTTP EVM/read access and WS native access on the same owned node as distinct
explicit routes; never infer a URL conversion or silent fallback. A bootstrap
or observer that submits by a different qualified mechanism retains its own
transport contract.

At admission, record native and EVM clocks separately. Verify their mapping; do not assume equal height or treat an EVM receipt as native finality. Historical reads must remain at the receipt's authenticated block. Until RT-01 through RT-08 are qualified for production, an unknown runtime stops dependent new signing pending explicit adapter admission. The target operating model automatically admits a compatible consumed profile under the approved compatibility policy, retains exact historical identities and suspends only unsupported operations. A testnet provisional profile alone cannot authorize that production behavior.

Retain the authenticated runtime proof with each historical or signing view;
metadata-cache eviction must not revoke that view or force its immutable audit
to run again. The [RT-06 correction](evidence/runtime-proof-eviction-20260927.md)
implements this ownership boundary for provisional consumers while retaining
fresh block/chain identity checks, exact signing domains and strict mainnet
rejection. A new connection must establish its own authority; the correction
does not qualify automatic production runtime admission or durable proof reuse.

Validator stake and schedule reads now have [separate block-bound read
capabilities](evidence/validator-read-capability-20260927.md). An independently
approved future artifact can satisfy its consumed storage and selective API
profile without a compiled spec-version entry. A schedule also requires its
epoch-storage profile; a stake-only read does not. These checks reject changed
interfaces before storage decoding and retain exact historical pins. They grant
no new signing or production configuration authority; wider automatic runtime
admission remains part of RT-01 through RT-08.

The [atomic source capability](evidence/source-runtime-capability-20260927.md)
also removes the source encoder's redundant spec list for independently
approved successors. A private bound view retains its exact artifact/block
witness, then checks the selected atomic calls and ordered signing extensions.
Source preparation refuses a different block before reading storage or taking
a nonce. Offline signed-byte reconstruction establishes encoding and signature
integrity; it cannot grant runtime approval. Original bytes remain reusable
after cold authentication at the same artifact, while a changed signing domain
still requires separate reconciliation.

The standard validator's [qualified source-receipt correction](evidence/validator-source-runtime-qualification-20260929.md)
keeps original preparation, parent execution and post-state runtime views
separate across an independently approved upgrade. Original signed bytes stay
fixed; events use execution metadata and commitment readback uses post-state
metadata. All 103 selected receipt and adjacent roots pass normal and race
qualification. Changed execution signing domains, unsupported consumed
interfaces and absent historical approvals still fail. Automatic runtime
approval, wider current-head consumers, both validator roles and live release
qualification remain open.

The validator now has a separate [mainnet runtime observation admission
path](evidence/mainnet-runtime-observation-20260927.md). A schema-2 config pins
an ordered history of independently reviewed exact artifacts, provenance and
finite native block intervals. Every observation checks the configured route,
fresh native name/genesis/EVM964 identity, finality and canonical block before
selecting that interval's artifact; a later approval cannot reinterpret an
earlier interval or expand an already loaded config. This removes the outer
compiled-version gate for runtime identity reads. It grants no storage profile,
producer, signing or submission authority: those remain blocked until the
complete production successor policy and independently verified mainnet
identity are qualified. Existing schema-1 config and historical authority stay
unchanged.

Current source changes matter to this design:

| Subject | Source-backed observation | Bootstrap consequence |
| --- | --- | --- |
| Subnet emission allocation | The inspected `get_shares` uses price EMA, a `1 - MinerBurned` adjustment, then an emission gate. A flow-based helper also exists but is not the selected `get_shares` path. [Source][subtensor-shares] | Do not assume an older Taoflow formula or a root-validator vote controls our subnet's allocation. Attest the actual runtime path. |
| Current root strategy | Runtime470 has removed `set_root_weights`. Dividends accumulate where earned; optional coldkey/proxy basket trades change holdings. [Current guide][root-reborn-470], [removal migration][root-removal-470] | Select the separately approved [passive root service](ROOT-PASSIVE-SERVICE.md) in a fresh v4 plan alongside both UR validators. Retain legacy signed actions and their recovery history. |
| Miner collateral | Registration collateral can survive deregistration; later earnings can affect release and capture. [Official collateral guide, pinned source][collateral-guide] | A UID reset is not a balance, lock, or stake reset. Pool capture must distinguish emission, locked collateral, and principal. |
| Native versus signed limits | The local whitepaper records runtime-dependent weight-limit behavior and requires a signed policy cap. [Local specification](../WHITEPAPER.md#15-concrete-parameters) | Observe runtime getters and enforce the signed cap independently. Do not assume a successful setter changed native enforcement. |

When documentation and the exact runtime source disagree, record the discrepancy and resolve it against the authenticated runtime. For example, the inspected emission-enable implementation affects pool-side injection while retaining participant-side emission; it cannot serve as an owner-controlled miner payout pause. [Storage contract][subtensor-storage]

## Authority and capability matrix

The word “root” identifies three different things here: the Substrate `Root` origin, a netuid-0 validator, and a UR settlement Merkle root. None grants either of the other authorities.

| Action | Required authority | Real limit and admission check |
| --- | --- | --- |
| Inspect finalized chain state | Read access to the owned node | No signer; authenticate chain and block before interpreting storage. |
| Change supported subnet parameters or request trimming | Subnet-owner coldkey, or a genuinely authorized chain `Root` origin | Owner calls have individual permissions, rate limits and administrative windows. A call name beginning with `sudo_` does not itself mean the owner has chain Sudo. |
| Change maximum validator permits or the global subnet-owner cut | Chain `Root` in the inspected implementation | Root-validator registration and the UR owner wallet do not satisfy `ensure_root`. No automatic governance proposal or Sudo attempt. |
| Remove every selected miner | Depends on an actually supported mechanism | No general owner-authorized arbitrary bulk removal has been established. See the reset alternatives below. |
| Register a UR hotkey and acquire stake | Its coldkey or a proven permitted proxy/contract origin | Registration, burn, collateral, pool price, capacity and eligibility are independent checks. |
| Register the root-validator hotkey | Its coldkey through the root registration path | Does not confer administrative authority. The native call's burn-price limitation needs special handling below. |
| Submit UR consensus weights | The registered UR validator hotkey | Correct permit/eligibility, stake, activity, mechanism and CRv4 timing are required. |
| Observe the root dividend basket | Read-only access plus independently approved existing identity, runtime and finite observation policy | The selected passive service has no transaction authority; current seat, ownership, stake, delegation and runtime must match. |
| Trade a root dividend basket | Its coldkey or explicitly authorized current `BasketTrading` proxy | Current price/budget/liquidity/concentration checks apply; no basket-trading adapter is admitted by this passive strategy. [Current source][root-basket-470] |
| Deploy EVM contracts | Dedicated EVM deployment signer | Exact nonce, creation bytecode, constructor data, gas and value envelopes. |
| Govern the coordinator | Approved EVM 2-of-3 Safe | Safe authorization does not authorize native subnet-owner calls. |
| Pause permitted coordinator actions | Configured guardian under contract rules | Cannot claw back reserve principal, rewrite earned claims or pause valid vault claims. |
| Withdraw immutable reserve or upgrade the settlement vault | No such release-1.0 authority | Reject any proposed action requiring this capability. |

The inspected admin implementation permits owner-limited trimming and selected parameters; maximum validators and owner-cut setters require chain `Root`. The emission-enable setter also requires `Root` and is not a percentage setter. [Pinned admin implementation][subtensor-admin]

Every planned transaction records its actual origin: native account or proxy real account, EVM sender, Safe address and threshold, and the exact role it exercises. Prove ownership and proxy filters from chain state. Do not manufacture an authority assumption from possession of a similarly named key file.

## Exact UID census and reset

### What is being reset

Scope is the approved UR mainnet netuid, SN25 (netuid 25), only. Netuid 0 and other subnets are excluded. A UID is a mutable slot, not a permanent miner identity, and a neuron can perform more than one role. “All miners” must become a signed list of **hotkey identities and registration generations**, not a range such as `1..255` or “all UIDs without a validator permit.”

At finalized block `B`, write `census.json` containing every UID and both directions of its UID/hotkey mapping; coldkey ownership; registration block; owner identity; role classification; permits and activity; native and mechanism-specific emission/weights; immune status and expiry; collateral and other locks; stake positions relevant to custody; commitments and associated EVM identity. Include the block hash and runtime identity for every decoded field. Reconcile the complete cardinality against `SubnetworkN`; missing entries or ambiguous ownership block planning.

The signer-free [SN25 census and reset preview](SUBNET-CENSUS.md) now authenticates
one finalized runtime and complete forward/reverse SN25 and root identity maps,
then compares declared protected/removal generations with the inspected owner
trim selection. It does not yet collect collateral, stake, claims, commitments,
EVM associations or every mechanism-specific weight. Even an exact candidate
set keeps `reset_ready=false`: the trim call cannot bind hotkey generations at
execution, and the source-selected owner cooldown is not a metadata constant.
The separate runtime 470 `subnet-discover` command reads a retained unapproved
runtime/finalized snapshot without inventing an approved owner or generation
policy. Its [current-runtime qualification](evidence/runtime470-subnet-discovery-20261001.md)
checks exact official metadata and synthetic state; the output records complete
SN25/root UID membership, observed owner/generation and raw storage while every
seat stays unclassified. `membership_complete` does not mean a complete custody
or reset census. Runtime/source, role and removal approval remain independent,
and its distinct artifact cannot be consumed as a trim policy or execution plan.
The retained public snapshot completed with 256 SN25 and 64 root registrations
in 13.231 seconds using exact-key batches, after per-key reads exhausted their
15-minute windows under HTTP 429. The owner/hotkey details remain restricted;
this membership observation does not close the current custody/role census or
independent approval gates.
The separate signer-free `owner-trim-plan` command ranks bounded owner
capacities against that authenticated census, predicts removed generations and
survivor UID mapping, and names each old miner that would remain. Its
[algorithm and limits](SUBNET-CENSUS.md#best-effort-owner-trim) retain
`reset_ready=false`, `apply_authority=false` and `full_reset_completed=false`.
The [recheck and reconciliation commands](OWNER-TRIM-GUARD.md) now rebuild a
retained plan from its historical authenticated census, refuse drift before a
prospective call, and compare later exact generations, survivors and root
membership. A matching read is not a transaction receipt or execution token;
the reviewed owner call still accepts only netuid and capacity. Any execution
path must explicitly resolve the protected-identity risk between recheck and
inclusion, record the actual receipt and reconcile effects.

Construct disjoint `remove`, `preserve`, and `unresolved` sets. Preserve explicit owner and validator hotkeys, including a validator currently lacking a permit, and any reserve, pool or escrow identity whose existing custody or earned claims require continuity. Membership in both a requested removal scope and a protected custody/validator role is an explicit conflict requiring a reviewed resolution; it is not silently omitted from “all.” Third-party validator identities receive the same explicit classification. Snapshot netuid-0 membership independently to prove it was untouched.

The owner-key launch target is to remove as many approved old miner registration generations as the runtime safely permits, while preserving every identity in `preserve`. The report must show each requested miner as `removed`, `retained_by_runtime`, or `unresolved`, with a verified old-to-new UID mapping for every survivor. A partial trim is a partial native reset, never a claim that all old UIDs were removed. `unresolved` evidence blocks activation; a known retained native registration needs an explicit launch disposition. Invalidate pre-cutover UR bindings and scoring eligibility prospectively while preserving historical claims. After cutover, the standard validator applies the same current admission and proof rules to retained and newly registered miners; the reset does not introduce a permanent hotkey blacklist. If a removed miner later re-registers, record a new generation; never let reuse of the numeric UID satisfy the old identity's postcondition. Registration-open policy and the cutover window must specify whether re-entry is allowed.

UR scoring exclusion cannot erase a retained hotkey's native registration or prevent an independent validator from weighting it. The 10% provider outcome must therefore be checked against the actual post-trim native incentive rows, including every retained old miner. A residual native payout is disclosed as an observed exception; it is not recast as UR provider earnings or a successful full reset.

The majority SN25 validator will run the standard `sn/validator` binary and
its ordinary evidence-based scoring policy. That can help the owner-key reset
only indirectly: old miner generations that are absent from eligible head and
pool evidence receive no positive weight from our validator, which may lower
their observed emissions over future native intervals and make them more likely
to be chosen by the [emission-ranked owner trim][subtensor-uids]. Do not assume
that ownership of the majority seat implies arbitrary zero weights or that an
old miner with valid current evidence will be excluded. The validator cannot deregister
anyone, bypass immunity or minimum capacity, force the other validators' votes,
or grant chain-Root authority. Reobserve finalized emissions and rerun the
complete protected-identity plan before each proposed trim; never assume a
submitted weight row has already changed the chain's trim ordering. The
netuid-0 role observes its distinct dividend basket; runtime470 has no root
weight setter. [Current root strategy](ROOT-PASSIVE-SERVICE.md),
[subnet weight setter][subtensor-weights]

Deletion of a registration does not delete historical events, refund registration cost, erase coldkey assets, or extinguish collateral and claims. Historical UR bindings continue to use their original block-specific mapping. Invalidate or renew only future bindings that reference displaced UID generations; preserve proof and claim history.

### Supported paths and their limits

| Mode | What it accomplishes | Condition for selection |
| --- | --- | --- |
| `owner-trim` | Lowers capacity, removes runtime-selected low emitters and compresses surviving UIDs. | This is the preferred owner-key path. A pinned simulation and execution-time guard must prove no protected identity can be removed. Record the exact removals and surviving old miners; an incomplete removal set remains an explicit launch exception, not a full reset. |
| `bounded-replacement` | New registrations replace runtime-selected existing neurons as capacity fills. | A finite, budgeted sequence proves every intended replacement and no protected loss, including competing registrations and changed pruning inputs. If the runtime cannot enforce the approved selection at execution, do not automate the destructive sequence. |
| `new-subnet` | Starts a separate metagraph and contract deployment on a new netuid. | Explicitly selected alternative with its own subnet-registration allowance and migration plan. It leaves the old subnet and its registrations in existence; it is not a reset of that subnet. |
| `chain-root-migration` | Can implement the literal removal policy if the chain's authorized governance adopts a suitable migration. | Separately reviewed runtime/call and authentic governance execution. The bootstrap verifies its finalized result; it never pretends the UR owner can grant itself this authority. |
| `ur-generation-only` | Resets UR application admission/scoring/bindings prospectively. | Explicitly accepted narrower outcome. It makes no claim to remove native UIDs. |

The inspected trim implementation enforces minimum and maximum capacity, protects owner-immune and temporarily immune entries, and requires the immune percentage to remain strictly below its runtime threshold. It removes according to emission rank and migrates the survivors' slot-indexed state. The ordinary `set_max_allowed_uids` path cannot set capacity below the occupied count. [Trim implementation][subtensor-uids], [capacity documentation][max-uids]

At the reviewed runtime, `sudo_set_network_registration_allowed` and the
per-block registration limit require chain `Root`; the SN25 owner cannot assume
it can close registration for a trim window. Even an already closed
registration flag does not prevent a coldkey-authorized hotkey swap from
changing the registered generation. A bounded owner-key execution path must
therefore establish its protection invariant under actual registration and
swap/custody behavior through inclusion, or leave the trim as a read-only
proposal. [Registration setter](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs#L728), [hotkey swap](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/swap/swap_hotkey.rs#L101).

Ordinary registration pruning is a different algorithm. In the inspected source it excludes owner-protected identities but can fall back to temporally immune candidates in some capacity conditions. Do not use the trim immunity model to justify a replacement sequence. `clear_neuron` is an internal implementation routine, not evidence of an owner-callable reset endpoint. [Registration implementation][subtensor-registration]

The reset planner must derive the live minimum, timing restrictions, immunity threshold and pruning order, including ties, rather than use the documentation's nominal 64-slot minimum or 30-day trim interval as constants. Select the most effective admissible owner trim by *safe removal of approved old miners*, not merely by the smallest UID count; keep all protected identities and native custody rights intact. An external actor can change the candidate set between preview and execution. A fresh preflight reduces that race but does not eliminate it; destructive automatic apply requires an execution-time guard or a demonstrated invariant under all allowed intervening changes. Otherwise export the unsupported action and report `RESET_CAPABILITY_BLOCKED`. Owner keys are the only available native administrative authority for this launch; do not plan chain-Root calls or imply that a netuid-0 validator seat supplies them.

A narrower owner-key path may be admissible without an atomic hotkey predicate:
prove that every generation the runtime could remove before the submitted call
expires is an explicitly approved old miner, while every protected identity
remains immune throughout that window. Then changes to emission ordering can
change *which approved old miners* are removed, but cannot remove a protected
identity. The proof must cover the entire mortal transaction window, including
earlier same-block actions: registration, hotkey swaps, temporary-immunity
expiry, owner/immune status, native epoch updates and capacity limits. An
already closed registration flag is insufficient if a subnet-owner takeover
can register a neuron directly during an epoch; rule that path out or choose a
window before the next epoch. First hotkey swaps have no cooldown, so a
cooldown proof needs an authenticated nonzero last-swap value for each relevant
coldkey or a concrete custody fence. Majority-validator weights alone do not
establish any of these conditions. Reconcile actual removals from the
finalized receipt rather than treating the preview's predicted list as the
result. [Epoch owner takeover](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/run_coinbase.rs#L389), [hotkey swap](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/swap/swap_hotkey.rs#L101).

The signer-free [`owner-trim-qualify`](OWNER-TRIM-BOUNDED.md) now tests a
conservative version of this condition from exact-block RPC evidence: protected
immunity through a proposed 4–256-block mortal era, closed registration,
authenticated hotkey cooldowns and coldkey-swap delays/announcements, no native
epoch or admin-window closure, no lease, and the runtime capacity/immune-ratio
limits. Approved miners may lose immunity without expanding the approved set.
Every requested generation remains explicit, including unresolved originals
and conditional residuals. Passing predicates remain conditional on unproved
owner/proxy/pending-action, governance/runtime and public subnet-pruning/reuse
fences; source-to-Wasm,
custody, actual signed mortality, receipt and subset reconciliation still block
execution. Exit 0 is evidence only; apply/reset/full-reset remain false.

After a finalized reset, repeat the entire census and compare identities, not just counts. Reconcile commitments, balances and locks separately. Re-establish approved capacity and permitted registration settings before new pool/head registrations; no “temporary” parameter change may remain unreported. Existing settlement/claim service must stay available throughout any migration.

## Ten percent of native miner allocation

### Denominator and accounting

Use alpha's atomic units for reward accounting, distinct from TAO rao and EVM wei. The reference denominator is the UR subnet's **native miner tranche before the chosen 90% withholding mechanism**, over explicitly identified native emission intervals after activation. It excludes the owner cut, validator/root dividends, TAO pool injection, deposits, reserve principal, and collateral principal. A miner's newly earned reward captured into collateral still counts as that miner's economic reward; it is not a way to hide payments from the 10% calculation.

Let `M_i` be the authenticated miner allocation for native interval `i`, using the exact runtime's accumulation, mechanism split, drain and rounding rules. Let `M_total(k) = sum(M_i, i <= k)`. Define the integer reference for the cumulative provider target:

```text
P_reference(k) = floor(M_total(k) / 10)
P_interval(k) = P_reference(k) - P_reference(k - 1)
remainder_reference(k) = M_total(k) - P_reference(k)
```

This conserves reference rounding across intervals; independent floating-point `0.1 * amount` calculations are prohibited. Use checked integer/rational arithmetic. Record `Q(k)`, the absolute alpha-unit tolerance derived from the selected runtime's actual u16, fixed-point and per-recipient rounding over the observation window. The requested target is met when actual provider entitlement differs from `P_reference(k)` by no more than that explicitly reviewed tolerance. Do not invent a large percentage allowance for consensus disagreement or require zero dust when the chain cannot represent the fraction exactly. Track native truncation dust separately from the policy remainder. Allocation at a later UR settlement epoch must reference its underlying native intervals exactly once. Align activation to a proven drain boundary with no unexplained pre-activation `PendingServerEmission`; never relabel old rewards as a new 10% budget.

Under the nominal 18% owner / 41% miner / 41% validator split, this request corresponds to approximately **4.1% of participant-side subnet emission**. The actual owner-cut fraction and fixed-point rounding must be read and reproduced, not hardcoded as exactly 41%. The inspected implementation accumulates a miner half after the owner cut, and zero incentive can redirect the miner allocation to validators. [Participant distribution implementation][subtensor-coinbase]

The existing UR `theta` divides provider distribution between direct head miners and tail pools. Once the 10% mechanism is selected, construct reference allocations `H = floor(theta * P_reference)` and `T = P_reference - H`, then reconcile their actual native outcomes against the approved quantization tolerance. A separately selected hard-cap mode additionally requires actual cumulative entitlement not to exceed its signed ceiling. Changing theta alone leaves the native miner pot intact; row normalization also makes multiplying every validator weight by 0.1 ineffective. [UR steering specification](../WHITEPAPER.md), [existing steering tests](../validator/steer_test.go)

Count entitlement once: native rewards to provider-owned head coldkeys, and provider entitlement funded through tail pools, are the two payment channels. A tail capture followed by a claim is one reward, not two. Target measurement, and any separately selected cap, covers earned entitlement and locked miner reward, not just liquid claims completed so far. Existing valid claims retain their original terms.

### Selected owner-recycle policy for the other 90%

| Policy | Feasibility and consequences | Design disposition |
| --- | --- | --- |
| `owner-recycle` | If final Yuma incentive is directed to the runtime-recognized owner-hotkey set under recycle mode, that portion is recycled. It is not placed in our reserve or retained as a future provider claim. | **Selected by the user, 2026-09-27.** Implement an explicit mainnet policy successor and prove the actual 10% provider / 90% recycle outcome within the approved runtime tolerance. |
| `owner-burn` | The same owner-directed withholding path can burn instead. Burn/recycle supply effects differ. | Not selected; verify the actual on-chain mode is recycle. |
| `reserve-custody` | Routes the remainder into an explicitly defined reserve position, with separately enforced accounting and no provider claim on it. | Not selected. The current vault has no “send 90% to reserve” operation; owner-recycle provides no reserve credit. |
| `deferred-provider-liability` | Pays 10% now while preserving 90% as future provider claims. | Not selected; recycling does not create a deferred provider liability. |
| `native-runtime-cap` | A runtime-enforced miner sub-allocation could provide the strongest whole-subnet boundary. | Not the selected mechanism. No suitable owner-callable primitive was established in the inspected source. |

The existing implementation withholds owner-directed incentive in both burn and recycle modes and records the withheld ratio. Choosing recycle does not avoid that accounting. [Owner-directed distribution][subtensor-coinbase] In the pinned source, recycling decrements `SubnetAlphaOut` and calls the alpha-asset recycle operation; burning calls its distinct burn operation without that decrement. Neither operation credits the owner wallet or our vault. [Alpha accounting][subtensor-alpha-accounting] The withheld ratio reduces the subnet's demand share before the emission gate; a 90% withholding policy is economically consequential and does not imply an exact 90% reduction in final TAO allocation after renormalization and gating. [Allocation formula][subtensor-shares]

The inspected runtime defaults `RecycleOrBurn` to **Burn**. Its `AdminUtils.sudo_set_recycle_or_burn(netuid, Recycle)` call accepts the subnet owner or chain root, subject to the runtime's owner rate limit and admin window; the EVM alpha precompile exposes `setRecycleOrBurn(uint16,uint8)` with mode `1` for recycle. These are alternatives for one reviewed operation, not two changes to submit. The bootstrap must read the exact runtime metadata and current finalized storage, select the authorized route, set recycle if needed, and verify the finalized `RecycleOrBurn[25] == Recycle` value before activating any owner-directed 90% weight proposal. A pending call, a failed call, or the default Burn value is not recycle. Recheck the mode at every activation/recovery boundary and alert on drift. [Admin setter][subtensor-admin], [default storage][subtensor-storage], [EVM precompile][subtensor-alpha-precompile]

Release 1.0 explicitly rejected owner-directed burning as its head/tail steering strategy. The selected owner-recycle launch policy must therefore be encoded as an explicit economic-policy successor, with its activation and accounting independently verified. Preserve the independent-validator objective and signed weight caps: do not raise a cap, create arbitrary owner recipients, or displace validators merely to force a 90% weight destination. [Whitepaper, head/tail decision](../WHITEPAPER.md#138-headtail-split-θ-in-one-mechanism-chosen-not-two-mechanisms-not-owner-burn)

A weight proposal is not an enforceable payout fraction. Independent validator weights, Yuma clipping, bonds, activity, permits, normalization and u16 rounding affect final incentive. For either owner-withholding path, qualify the complete runtime outcome against the admitted validator set and review adjacent/adversarial weight states. The draft assurance mode is `observed-native-target`: demonstrate the actual 10% allocation within `Q(k)`, disclose sensitivity to other validators, and monitor subsequent deviation. It does not promise that other validators can never change the outcome. If a stronger `enforced-cap` mode is selected, prove that ceiling under all admitted conditions or report `EMISSION_CAP_UNENFORCEABLE`; an after-the-fact monitor is not enforcement. Halting our validator does not revoke other validators' weights or stop already queued native emission. [Consensus implementation][subtensor-epoch]

Claim “started at 10%” only after the observed native outcomes meet the target and its runtime-derived tolerance. Report deviations and dust explicitly; a low payout below a hard ceiling alone does not establish that the target was reached. If only a proposed weight target can be shown and the actual outcome cannot yet be established, report `ACTIVATED_AWAITING_EMISSION_OBSERVATION`. A material miss is not relabeled as quantization. Future changes to the target or assurance level require a new reviewed policy.

### Immutable vault implications

[STSettlementVault](../evm/src/STSettlementVault.sol) captures the eligible pool emission through the configured staking precompile and keeps immutable claim accounting. It has no upgrade or treasury sweep. Its conservation checks distinguish total captured, total paid, escrow accounting, pending funding and outstanding liabilities. Publishing payout roots at 10% while leaving 90% in this vault does not make the remainder an owner reserve or erase its accounting obligations.

The [claim-recovery correction](../evm/CLAIM-RECOVERY.md) keeps an accepted
Merkle leaf and provider credit if an exact runtime transfer or balance-delta
check fails. The attempted payment is isolated in a vault self-call so its
state can roll back without rolling back claim acceptance. This changes the
non-upgradeable vault's bytecode and requires a new deployment artifact and
an authenticated runtime rehearsal of nested EVM/native rollback. It does not
repair an already deployed vault or loosen exact payout accounting.

A reserve design must specify a different enforceable custody path before deployment: who owns each head and pool registration, which precompile call transfers each tranche, which principal and collateral are excluded, who can authorize the transfer, and how cumulative provider liabilities are bounded. Moving all head miners behind a new contract would change release 1.0's provider-owned direct-payment model. Such a design requires its own contract/policy qualification and migration plan; a coordinator upgrade cannot retrofit withdrawal authority into an old vault.

Implement the selected owner-recycle path against the exact runtime: authenticate the recognized owner-hotkey set and recycle mode, preserve the signed cap and validator independence, and prove the finalized 10% provider / 90% recycle split with exact interval accounting. Keep the recycle fraction separate from native rounding and supply-side effects. No reserve transfer or new reserve-custody contract is implied. Do not deploy an immutable contract set before the selected path and custody layout are qualified.

The [signer-free successor preview](../validator/OWNER-RECYCLE-PLANNER.md)
(`9a287d92`) is an implementation step, not activation. It binds a proposed
successor to the unchanged parent policy, builds the provider tenth using the
existing head/tail theta, gives the remainder equally to usable recognized
owner UIDs, and refuses rows that violate the signed cap before or after u16
quantization. With the current `32768/65535` per-recipient cap, a single
owner destination cannot carry 90%; at least two usable registered owner
hotkeys are necessary. The pinned source permits multiple hotkeys of the
subnet-owner coldkey and recognizes their incentive for recycling, but their
mainnet registration, masks and protection are unknown. The preview always
refuses submission and reports its row as a proposed weight fraction; an
authenticated owner census, signed policy transition and measured native
Yuma/emission outcome remain separate gates.

The [signed successor admission](../validator/OWNER-RECYCLE-ADMISSION.md)
now verifies an independently pinned approval, immutable local custody and an
exact finalized runtime/owner census under explicit Recycle mode. It does not
start steering: both submission paths remain fenced until measurement,
signatures, pending intents, archive replay, coordinator/client-key policy and
the native drain boundary use one successor authority. The distinct
[schema-3 production path](OWNER-RECYCLE-PRODUCTION.md) now joins those inputs
through the standard V2 producer, exact prepared source/row, a separate hotkey
sidecar and durable intent/archive replay. Old observation approvals stay
fenced. Original pending receipts can still be reconciled. An admitted 10/90 weight row remains a
proposal until independent validators and finalized native allocation prove
the economic result.

Policy rollover must retain the previous epoch's signed payout and its actual
coordinator policy/window. The [operator epoch-policy correction](evidence/operator-policy-custody-qualification-20261001.md)
allows successor deposit sizing after restart without the original policy file:
it authenticates the current deployment, reads immutable historical `policyAt`
at that canonical hash and verifies both finalized boundaries. Fresh payout
issuance refuses a configuration that does not match the requested epoch.
Local two-operator migration, processed registration, fresh proof and deposit
tests do not establish live readiness. Preserve exact old signature bytes and
complete both operators' future-boundary cutover before activation.

Settlement admission also requires the compatible server custody reader and
migrations through 728. The [retained timestamp correction](PRELAUNCH-FIXES.md)
prevents historical terminal NULL close times from disappearing out of every
epoch: it blocks new payout construction before chain reads and retains the
same debt after archive cleanup. Record the exact historical NULL census before
coordinated reader/writer/reaper cutover. Unknown timestamps are unresolved
debt, never guessed epoch assignments or automatic zero-credit exclusions.
Previously retained artifacts stay immutable and are not certified complete by
this prospective fix. NetEscrow writer drain, revision/fence restore policy and
archive capacity remain separate production gates.

The [atomic payer admission candidate](PRELAUNCH-FIXES.md), composed server
`b6f49bdb`, removes Redis from
credit authorization: both origin and companion creation lock payer balances
and read durable reservations after the lock, while settlement commits the
consumed-byte debit with the terminal outcome. Retain its causal evidence and
deploy it only after draining all old creators and asynchronous debit posts.
The same release must use checked settlement arithmetic: an overflow-safe
bilateral mean and negative-grant refusal prevent malformed reports/history
from claiming an outcome without the corresponding consumed-byte debit.
An old terminal outcome or settled escrow marker cannot establish whether a
historical debit post ran; independently reconcile exact historical financial
state before activation. This does not automatically repair old balances or
make participant sweep/account payout posts atomic or replay-safe. Their
durability, full conservation and lock-contention capacity remain open gates;
the migration catalog through 728 is unchanged by this code slice.
In particular, the terminal settlement can commit before its in-memory
participant-sweep post creates `transfer_escrow_sweep` rows consumed by the
payment planner. A lost post can omit a provider payment; terminal-outcome
replay alone cannot recreate it. Qualify a durable correction with causal
lost-post, rollback and replay controls before activation.

## Contract deployment, custody and initialization

Reuse the release-1.0 contracts and reviewed ABI/artifact generation, with mainnet-specific inputs. The existing [Deploy script](../evm/script/Deploy.s.sol) is the ordering reference, not a command the bootstrap blindly shells out to. The Go planner must build exact transaction payloads and independently read back their results.

Prepare against authenticated pinned observations and tolerate ordinary
finalized-head advancement during the operation. Refresh affected current
authority, nonce and custody inputs without discarding the original signed
transaction or completed phase. A failed mapping read remains unavailable
evidence; report a mapping contradiction only after receiving a conflicting
value. Keep retries within the approved operation budget and retain unresolved
signed liabilities for reconciliation.

The [executable contract phase](BOOTSTRAP-CONTRACTS.md) wires
`bootstrap-contracts preview/plan/apply/resume` to exact reserve CREATE preparation,
public signed-byte custody, a finite owned-HTTP submission allowance and
canonical transaction/runtime/getter recovery. The next selected actions now
implement predecessor-bound settlement vault CREATE, coordinator
implementation CREATE, escrow registration and atomic initialized proxy
CREATE under the same approved graph and finite attempts.
Offline preparation requires no deployment outputs.
The [reserve qualification](evidence/bootstrap-contract-qualification-20260928.md)
and [vault qualification](evidence/bootstrap-contract-vault-qualification-20260928.md)
retain their distinct source graphs and test scopes; the vault candidate passed
51 focused roots normal/race and 426 full normal mainnet roots. The
[coordinator qualification](evidence/bootstrap-contract-coordinator-qualification-20260928.md)
passed 71 focused normal roots, both exact race shards and 446 full normal
mainnet roots. [Escrow qualification](evidence/bootstrap-contract-escrow-qualification-20260928.md)
passed 95 selected roots normal/race and 470 full normal roots; live native
burn/refund behavior remains unmeasured. [Proxy qualification](evidence/bootstrap-contract-proxy-qualification-20260928.md)
passed 120 selected roots normal/race and 495 full normal roots. Genuine Safe
evidence anchoring and authenticated live authority remain required. The
[scoped graph qualification](evidence/bootstrap-contract-plan-graph-qualification-20260929.md)
passes five graph, 28 evidence and 177 adjacent roots normally and under race,
with six causal controls. The broader `./mainnet` normal package stopped at its
60-minute window after 299 passing roots and zero assertions; it is incomplete.
The separate [nine-action candidate status](evidence/contract-graph-review-block-20260928.md)
records successful complete-command diagnostics and an unresolved failure test.
Automatic review blocked that test correction; the candidate is not admitted
as the executable production release.
Unsigned `preview` exports the exact independently signable approval bytes after
local structural/artifact review without opening custody or a network route.
Signed commands retain the approval check. Offline reopen preserves completed
receipts as explicitly retained observations, not fresh chain audits; these
[follow-up command regressions passed in both modes](evidence/bootstrap-contract-qualification-20260928.md).

| Identity | Custody/authority |
| --- | --- |
| Subnet owner coldkey | Owners report that the existing account is Ledger-derived. They keep the key on their own Ledger with the Polkadot generic app, have no Snow access, and run the [qualified offline owner-side signing handoff](OWNER-SIGNING.md) on their own device. Snow receives only an exact signed reply for verification, retention and submission. A [pinned Linux SDK artifact](evidence/owner-ledger-native-sdk-qualification-20260930.md) has synthetic-device qualification; the owners' actual platform, physical device, on-chain account, metadata digest and live call still need verification. |
| EVM deployer | Limited bootstrap gas/value; no ongoing governance custody. |
| Coordinator owner | Actual 2-of-3 Safe with three distinct approved owners. |
| Guardian | Separate limited operational authority. |
| Commitment oracle | Separate reviewed signer/service with original and any scheduled route authenticated. |
| Root validator coldkey/hotkey | Existing root identity and stake custody. The current passive observer takes public identity and independent config/host approvals, without a native signing key. Registration, stake or basket actions need separate authority and the appropriate custody device; the owners' Ledger does not substitute for that key. |
| UR validator hotkey and stake coldkey | UR scoring; may be the reviewed reserve target when explicitly selected. |
| Operator demand deposit signer | Each operator keeps its own EVM signing key in its own secrets vault. The coordinator binds that address to its `noId` and deposit hotkey for the active epoch. Owner Ledger and the SN bootstrap never load operator deposit keys; this secrets vault is distinct from the on-chain settlement vault. The [qualified worker custody check](evidence/operator-deposit-custody-qualification-20260930.md) still needs real wallet and coordinator verification. |
| Vault mapped coldkey | Immutable tail-pool and escrow custody. No human holds its private key. |
| Reserve mapped coldkey | Permanent reserve stake under the immutable sink. |

Role equivalence must be deliberate. In particular, naming a UR validator “owner validator” does not prove that it is the native `SubnetOwnerHotkey`. The planner checks the actual mapping and does not assume UID 0. Require the existing deployer's distinct owner/guardian/oracle constraints. Inspect Safe singleton bytecode, owners, threshold, enabled modules, guards, fallback handler and pending transactions; an address merely implementing `getOwners` and `getThreshold` is insufficient.

Freeze compiler and dependency versions, creation and deployed bytecode,
link/immutable locations, constructor encodings, source identities and storage
layout. The [current size check](evidence/contract-size-candidate-20260927.md)
puts `STCoordinator` at 24,564 runtime bytes, just **12 bytes** below Foundry's
24,576-byte limit; any source or build-input change requires a fresh size and
exact-artifact deployment rehearsal. The selected live runtime's code-size rule
still needs authentication. For a dedicated deployer starting at nonce `n`, the
existing core sequence is:

| Nonce | Action | Required postcondition before its dependants |
| --- | --- | --- |
| `n` | CREATE `STReserveSink` | Exact predicted address, bytecode, netuid, reserve hotkey, bootstrap, mapped coldkey. |
| `n+1` | CREATE `STSettlementVault` | Exact custody identities, claim horizon, minimum transfer and bootstrap. |
| `n+2` | CREATE `STCoordinator` implementation | Exact implementation bytecode; implementation initializer disabled. |
| `n+3` | `vault.registerEscrow(maxBurnRao)` | Escrow registration under the vault's mapped coldkey, correct UID/ownership and bounded debit/refund. |
| `n+4` | CREATE ERC1967 proxy with initialization calldata | Initialization occurs in the constructor; approved Safe, guardian, oracle, custody links and initial policy are set atomically. |
| `n+5` | `reserve.setRecorderOnce(proxy)` | Exact one-shot recorder. |
| `n+6` | `vault.setCoordinatorOnce(proxy)` | Exact one-shot coordinator. |
| `n+7` | CREATE `STValidatorEvidence` | Exact approved deployment domain, coordinator/vault identities, predicted address and immutable getter readback; creation leaves the evidence unanchored. |

The escrow registration deliberately consumes a nonce before proxy creation. Predict all addresses before construction because the mapped coldkeys are immutable. Native rao-to-EVM-value conversion uses `1 rao = 10^9 wei` here; bounds and conversions must reject overflow. Authenticate `blake2_256("evm:" || H160)` against the runtime mapping before custody is funded.

The first eight actions through `n+7` have an offline executable path with complete scoped normal and race qualification; no live authority or installation is established. Anchor [STValidatorEvidence](../evm/src/STValidatorEvidence.sol) as a separately planned subsequent action with its genesis/deployment domain and coordinator/vault identities. The current release uses the coordinator's one-shot `fixValidatorEvidence`; an existing foreign anchor is a hard conflict. The Safe's inner nonce and a relayer's outer EVM nonce are distinct from the deployer CREATE graph and each need independently retained custody and canonical postconditions. Use the fresh mainnet nonce graph, not the sim-testnet graph's extra upgrade, fleet-helper or adversarial contracts. Mainnet artifacts must not include those test fixtures by default. [Evidence deployment reference](../sim-testnet/evidence_deployment.go), [readback reference](../sim-testnet/evidence_deployment_runtime.go)

Only then register the approved operator pool hotkeys under vault custody, establish reserve-target eligibility, activate evidence identities and future bindings, and fund reviewed stake/deposit positions. Provider-owned head miners register through their own authorized identities; bootstrap cannot sign for unrelated miners. Every registration is present in the spend/count plan. Reconcile pool/escrow collateral and minimum-transfer semantics before the first production capture; immutable custody must not become stranded by an unqualified runtime change.

Use the actual [mainnet policy validation](../protocol/policy.go): a UR settlement epoch is **50,400 native blocks**, with the reviewed production root-commit/finalization/close windows and claim retention. The deploy script's mainnet reference windows are 1,200 / 14,400 / 120 blocks and 8 claim epochs plus 1 grace epoch. Encode all fields explicitly in the signed mainnet policy; do not inherit accelerated 300- or 360-block testnet settings. Mainnet economic caps, deposit tiers, theta, minimum operator/validator counts and binding horizons need independent review.

The [steady cadence candidate](evidence/mainnet-steady-cadence-candidate-20260928.md)
represents mainnet from epoch zero with `after_accelerated_epochs: 0` and identical
initial/production epoch, root-commit, finalization and close windows. Both epochs
must contain exactly 50,400 blocks. The installer separately requires
`effective_epoch: 0` for its initial complete approved policy; a later approved
steady policy does not invent an accelerated period. Existing positive-count
accelerated policies, including historical mainnet policies, retain their exact
bytes and meaning. Testnet still requires its existing positive-count transition.
The two validator snapshot consumers need no relaxation: they compare the actual
pinned snapshot against that representable canonical policy. The signed public
config loader and both readers are covered by 24 selected protocol/validator
tests passing normally and with race detection, plus vet. Three causal controls
each reproduce their intended failure in normal and race execution. Composed
installer qualification remains pending; the component receipt records its
dependency scope and one missing causal-wrapper exit. Generic Solidity window
checks alone do not establish composed launch acceptance.

Preserve the current guarantees: the coordinator owns neither custody position, the sink has no outbound path, and valid earned vault claims survive coordinator pause or upgrade. Pausing new application activity is not a native emission kill switch. Initial contracts establish their epoch clock at deployment, so the plan must include sufficient time to finish setup and a future activation boundary; it cannot assume a dormant deployment has no running clock.

## Running both validators

The [initial two-UR installation component](VALIDATOR-ACTIVATION.md) now provides
a concrete `activate-validators` command for exact static-unit installation,
role-group-readable runtime copies of the original signed configs, current
bootstrap admission and durable per-unit start/recovery. Its [scoped independent
qualification](evidence/validator-activation-qualification-20260930.md) is sealed:
88 positive root executions pass normal/race, and twelve normal plus five
selected race controls are causal. **No deployment was performed.** It keeps
bootstrap v3 role/generation
and producer approvals, both current permits and original custody separate from
process authority. Public fresh starts remain closed until a qualified current
activation-authority adapter discharges the existing checkpoint, operator,
contract, custody and majority-stake blockers. A systemd acknowledgement or
progress file does not prove weights or the 10/90 outcome. The root signing
service remains a distinct open implementation/deployment gate.

The [qualified native prerequisite reader](evidence/validator-native-admission-qualification-20260930.md)
now authenticates the original signed runtime at both current and activation
checkpoint hashes, exact native epoch and drain facts, current generation,
owner, activity and explicit Recycle. Its canonical anchors and sample age are
rechecked before admission. It does not supply the remaining operator,
contract, signer-custody or effective-majority authority, so public starts
remain closed.

The [qualified `admit-evidence` increment](evidence/validator-current-evidence-qualification-20260930.md)
now reads the original deployed contract graph and both operators' signed
activation/client-key evidence under bounded read-only RPC/API transport. It
records a partial projection without granting start authority. Full producer
proof history and worker health, deployment provenance, global signer custody
and effective majority remain separate launch gates.

The [qualified `admit-stake` command](evidence/validator-stake-capacity-qualification-20260930.md)
observes the original roles' complete native stake census and computes a
conservative capacity lower bound under the pinned runtime's threshold,
normalization, permit and activity rules. It reports capacity and current
activity separately. It cannot prove applied weight influence or grant public
start authority; actual majority behavior must be observed after launch.

The [qualified `admit-health` increment](evidence/validator-proof-health-qualification-20260930.md)
replays each role's original pinned operator activation prefix and records
protected standard-validator progress as a separate health signal. Completed
proof checkpoints remain durable across a later read failure. It leaves the
current mutable proof namespace, live per-operator worker attestation and
global signer custody as explicit gates; a missing heartbeat is not treated
as corrupted proof history.

The [qualified `admit-committed` continuation](evidence/validator-committed-prefix-qualification-20260930.md)
also replays the service UID's current committed control history from both
original operator origins and preserves each completed checkpoint. This is a
read-only subgate. Unsealed ledger/intent state, live worker attestation,
global signer custody, applied weights influence and signed launch authority
remain mandatory before public start.

The [qualified bounded unsealed inventory](evidence/validator-unsealed-inventory-qualification-20261001.md)
adds service-owned signed ledger tails, unfinished trails and the empty intent
boundary to durable read-only custody. The [qualified canonical tail-boundary
extension](evidence/validator-tail-boundary-qualification-20261001.md) also
authenticates every signed tail's bounded EVM epoch/policy/eligibility boundary.
Nonempty intent graphs, historical provider bindings and the other launch gates
remain open.

### Root validator on netuid 0

The [runtime470 source/artifact review](../docs/spec/runtime-470-audit.md) and
[passive root service](ROOT-PASSIVE-SERVICE.md) define the current launch path:
fresh bootstrap schema v4, two independently approved UR production configs,
and a separately approved existing netuid-0 role using
`passive_accumulate_in_place`. Its exact policy binds genesis/full runtime,
source/code/metadata, hotkey/coldkey, seat generation, minimum stake, delegate
take, existing delegation, route, private checkpoint and finite observation
window/cadence. The real bounded `root-passive-service` command reuses the root
monitor after verifying the independent config signature and completed original
preparation. It has no native signing/submission path or heartbeat transaction.
Its `ready` result is observation readiness and `activation_ready` stays false.
The separately signed [`activate-root-passive` host owner](ROOT-PASSIVE-SERVICE.md#independently-approved-static-host-owner)
now provides exact sandboxed static installation and one durable process start,
with invocation recovery and a dedicated writable checkpoint directory. It keeps
the original v4 private approvals unchanged and consumes no UR start allowance.
Historical status and manager liveness do not prove continuing observer health.
Its [qualification](evidence/passive-root-host-20261001.md) supplies no live host
approval or deployment; actual current seat/stake, independent runtime authority,
the exact host signature/acceptance and live monitor evidence remain launch gates.
The earlier SN `6c801a25` release excludes this source successor. The current runtime has no `set_root_weights`; no root
weight action is required for this chosen strategy. [Removal][root-removal-470]

Retain the historical v3 `explicit_root_weights` action/custody capabilities and
their signed bytes without conversion. The [existing-seat action owner](ROOT-ACTION.md) provides an offline-qualified
mortal root basket encoder, durable one-request signing/nonce ownership and
receipt/expiry recovery. Its read-only chain adapter reconstructs canonical
native inclusion and receipt evidence from the approved owned RPC, with exact
historical execution-runtime checks; it does not independently prove GRANDPA
finality or storage. The [offline custody handoff](ROOT-OFFLINE-CUSTODY.md) now
verifies independent exact-action approval and retains one matching public
signature before handing it to this owner; missing receipts remain unresolved
across restart. The [root service decision owner](ROOT-SERVICE.md) now couples
one approved basket decision and its original native intent in one private
durable journal, with a finite joined supervisor and independent signer/submission
ports. Its read-only canonical weight view and pinned-runtime normalization
select an intent, never signing authority. The [owned-RPC submission adapter](ROOT-SUBMISSION.md)
now sends exact signed bytes under separate action/route approval, retains
uncertain numbered attempts and reconciles canonical outcomes before another
approved send. Its local composition with offline custody and the service owner
is qualified. The [bounded root-service command](evidence/root-service-runtime-qualification-20260930.md)
now composes original input admission, observation and issued-signature recovery,
with a closed public activation gate. Production live authority, globally fenced
native custody and a qualified separate hardware signer remain absent; there is
no live root signing command supplied by that legacy path. A signed root
call does not bind registration generation, so pending-action seat changes need
custody exclusion or separately authenticated incident reconciliation. The
legacy action must be reconciled under its original runtime authority. Changing
strategy, signing fees and distributed custody fencing requires separate approval
and qualification; a local reserve is not a native maximum-fee argument.
The current [UR validator config](../validator/config.go) rejects netuid 0 and is
not a root-validator implementation.

For an existing root seat, verify hotkey/coldkey ownership, current membership and registration generation, stake, immunity, delegate take, children/parents, basket configuration and accrued rights before adoption. For a new seat, the inspected runtime uses burn-priced root registration without a prior-stake admission condition; a full root network prunes a lowest-staked eligible seat. Registration alone does not establish sufficient stake to retain a seat. [Current root registration implementation][subtensor-root-470]

One specific budget gap must not be hidden: native `root_register(hotkey)` has no maximum-burn argument, while `register_limit` rejects netuid 0. A fresh quote is not an atomic price ceiling. The inspected Neuron precompile also exposes `rootRegister(bytes32)` without a limit. [Native call definitions][subtensor-dispatches], [registration limits][subtensor-registration], [Neuron interface][neuron-interface]

The future adapter therefore needs one of these explicit admission paths:

- Adopt an already registered, approved root identity and prove its finalized receipt and current ownership.
- Qualify a bounded root registration mechanism. A possible EVM path is an approved root custody account that invokes `rootRegister` and atomically rejects a mapped-balance debit above the signed ceiling. This changes the coldkey custody model and requires proof of EVM/native rollback, fee separation, hotkey ownership, staking/withdrawal authority and child-delegation controls; no such helper is supplied by this design or the existing UR contracts.
- Record a separately authorized native root registration as an external prerequisite with its actual receipt and spend. Do not label an uncapped native call “max-burn protected” or silently replace its intended coldkey with a contract mirror.

Without a proven bounded path or retained seat, full automatic bootstrap is `ROOT_REGISTRATION_CAPABILITY_BLOCKED`. This is a concrete implementation requirement, not permission to omit the requested root validator.

Root registration can automatically delegate child weight to every existing subnet owner unless the identity opts out first. The current passive policy observes existing automatic, current and pending delegation without changing it. A new registration or opt-out needs its own explicit plan, coldkey association and current dispatch review. Historical root weight vectors were removed by the runtime migration; preserve old signed actions and reconcile any outstanding liability instead of inventing a reset transaction. [Current dispatch definitions][subtensor-dispatches-470], [removal migration][root-removal-470]

Select **accumulate in place with no custom weight vector** for the new unsigned launch plan. Dividends are held where earned and no periodic write is required. Optional basket trades are a different coldkey/proxy capability and are not implemented by the passive service. The new observer explicitly rejects metadata that restores the retired root weight call/gates. [Current basket behavior][root-reborn-470]

Stake the root seat from its own explicit TAO allowance, retain fees/ED, and observe the actual retention margin. Stake or basket top-ups, re-registration, claims, take changes and basket trades require separate bounded plans. The passive service monitors finality, seat ownership, stake rank, delegation, basket state and runtime identity. Claiming root yield and unstaking principal are distinct operations with runtime-dependent windows; neither is enabled automatically by “run a root validator.”

### UR subnet validator

Run the production [validator entry point](../cli/validator/main.go), using complete deployment, policy, runtime, genesis, operator API, evidence and signing inputs. It must acquire current UR eligibility and validate finalized usage/evidence before emitting native CRv4 weights. Preserve the signed weight cap, head/tail rules, deposit/quality calculation and full prefix/history admission. Reject testnet provisional-input deferrals on mainnet.

Observe effective alpha/root-stake contribution, child attribution, `TaoWeight`, stake threshold, permits, activity, CRv4 version, reveal schedule and mechanism state. Do not transplant the testnet stake target or an old 0.18/0.018 TAO multiplier. The inspected production epoch path gives the owner UID special eligibility treatment; another owned hotkey still needs its own proper eligibility. A configured process being alive does not prove it has a permit or that its weight row was revealed and applied. [Permit calculation][subtensor-epoch]

Both requested validators do **not** count as two UR validators: a seat on netuid 0 alone does not validate the UR subnet. The current UR safety policy requires at least two live validators and two healthy operators. Include a separately admitted second UR validator before production readiness. Do not generate synthetic peers to satisfy the count.

Capacity is computed from the union of actual UR hotkeys: head miners, one pool per operator, distinct UR validator identities, escrow and owner/other protected identities. Root-only membership consumes no UR slot. A validator-permit limit is not a reserved partition of UID space. Keep the release's one-mechanism requirement and approximately 200-head target only if the live capacity and all additional identities fit. Do not assume “200 miners + 56 validators” leaves space for pools and escrow.

Manage both services with independent state directories, signer permissions, logs, executable/config hashes, and one writer per hotkey. Coldkeys and Safe owners stay out of online validator processes. Supervision has bounded restart policy; ownership transfers require the old process and every child to be joined. Read-only health includes finalized lag, evidence/index gaps, permit activity, revealed weight rows, pool/escrow capture, signed cap violations and root seat/basket drift.

## Go CLI and action model

[mainnet/main.go](main.go) currently contains signer-free `inspect`, `monitor`,
`subnet-preview`, `root-preview`, `root-monitor`, `check-recycle-mode` and `economic-reference`
commands, plus the offline `source-lock`, `release-inventory` and blocked-review `plan` commands. `inspect --rpc URL`
emits a content-hashed identity snapshot. Supplying
any expectation requires all of `--expected-chain`, `--expected-genesis` and
`--expected-evm-chain-id`; `monitor` always requires all three. The monitor emits
JSON lines for `ok`, `rpc-error`, `rpc-integrity`, `identity-mismatch`,
`finality-conflict`, `finality-stalled` or `checkpoint-error`, with a default 30-second interval after
each completed sample and five-minute stall threshold. It detects identity/finality conflicts,
rechecks the prior finalized block when the head advances, validates JSON-RPC
response version/ID, and rejects response bodies exceeding 1 MiB. Hash comparisons
accept equivalent hexadecimal casing. Malformed, inconsistent or oversized RPC
evidence emits terminal `rpc-integrity`; that status, `identity-mismatch` and
`finality-conflict` exit with code 3. Availability failures remain `rpc-error`
observations and do not establish healthy state. Repeated read failures retain
their first observed time; `severity` becomes `warning` after two minutes and
`critical` after five, while the monitor keeps retrying. A host-clock rollback
escalates immediately rather than postponing the page threshold. The command
contains no signer or submitter. Focused normal/race tests and vet pass; the retained Snow rejection
demonstrates actual wrong-network refusal.

`monitor --checkpoint /absolute/path/monitor.json` adds a single-owner local
continuity checkpoint. Before reporting a newly finalized position as healthy,
it atomically persists the approved chain/genesis/EVM identity, last finalized height and hash, and
progress time with a content checksum. The v3 checkpoint also retains the last
successful identity/continuity read and the start of an unresolved read outage,
including an initial outage before any finalized position is known. It clears
that outage only after a complete identity and continuity read. The reader
accepts valid v1/v2 records without inventing their missing success times and
writes v3 on its next state change; a rollback to the old
binary requires an explicit compatible checkpoint migration, not silent file
replacement. Restart loads the retained position and checks
the prior finalized hash against the route; a regression or changed historical
hash is still visible after process restart. A corrupt, foreign or symlinked
checkpoint stops admission; an unavailable write emits `checkpoint-error` and
exits rather than reporting health. The file is local continuity evidence, not
independent node confirmation or an approval. The operator must place it on a
durable, backed-up volume and supervise the monitor.

`monitor --metrics-file /absolute/path/monitor.prom` now exports bounded atomic
textfile gauges to the existing Fluent Bit collector. It exposes completed
sample freshness, last successful read, finalized progress, unresolved outage
and explicit status/severity. The [telemetry guide](MONITOR-TELEMETRY.md) and
tested [alert examples](monitor-alerts.example.yml) include an independently
supplied expected-host roster so one healthy host cannot conceal a missing
second host. Actual deployment, ingestion and alert delivery, independent
supervision and cross-domain health remain open work. A failed export reports
critical status and exits; stale retained metrics never establish fresh health.
Restart retains the prior metrics until a completed sample, including a prior
critical event. Physical path checks prevent checkpoint/metrics lock collisions,
and success timestamps include the final required continuity read.

An unchanged retained head with a checkpoint progress time ahead of the host
clock reports stalled until genuine finalized advancement resets the clock.
`root-preview` and finite `root-monitor` perform the separate [read-only root
census](ROOT-VALIDATOR.md). Their `ready` result means observation policy
readiness only; `activation_ready` is always false. They load no signer and do
not register, stake, submit root weights or authorize basket claims.
`subnet-preview` performs the [read-only SN25 reset feasibility
census](SUBNET-CENSUS.md), with explicit protected and removal generations;
`reset_ready` is always false and no UID is changed.
`owner-trim-plan` adds a bounded ranked partial-trim prediction and explicit
residuals, without changing that admission result.
`owner-trim-recheck` and `owner-trim-reconcile` extend the read-only evidence
through drift detection and exact post-state comparison, with the same blocked
execution status and no signer.
`owner-trim-qualify` separately evaluates a bounded approved-subset invariant;
its conditional result retains unproved window assumptions and no apply authority.

The [existing-seat root action owner](ROOT-ACTION.md) is an offline-qualified
one-action signing and recovery core, not a CLI command or live root validator.
It retains the original signed bytes, nonce and fee reservation across ambiguous
submissions. The [service owner](ROOT-SERVICE.md) now owns the approved decision,
composite intent and finite supervisor; read-only canonical observation and
receipt adapters exist. The separate [submission adapter](ROOT-SUBMISSION.md)
implements owned HTTP writes and durable attempt reconciliation. Production
current-authority and custody adapters, actual route/seat approval and command
activation must still be supplied and qualified before publishing a root basket;
the current accumulate-in-place strategy needs no periodic root transaction.

The [executable local root-custody bootstrap phase](BOOTSTRAP-ROOT.md) now wires
`bootstrap plan/apply/resume` to these existing custody and service owners. It
completes local journal preparation, exports the approved public packet and
imports its verified signature without a native key or network operation.
Resume reconciles actual child state after interrupted progress or output;
completed journals cannot be recreated as fresh allowances. Results separate
local custody completion, signature awaiting import and pending chain phases.
This is one implemented phase, not full bootstrap or root service activation.

The [offline chain composition](BOOTSTRAP-CHAIN.md) now joins that root owner,
reserve CREATE custody, a retained trim review and two distinct protected UR
role inputs under one durable local preparation. Its [component qualification](evidence/bootstrap-chain-qualification-20260928.md)
passed 369 full normal roots, all 87 selected race roots in six disjoint shards,
and four causal families. The original aggregate race timeout remains retained.
Tests used physical Connect `358cefae`, server `0633780c` and SDK `42241118`;
they do not qualify the current composed release graph. Executed trim, complete
contract installation, actual UR producer admission, healthy operators, current
root authority, all live services and native economic acceptance remain open.

The v2 offline admission addition now reuses the standard validator's strict
schema-3 config and production approval verification for the intended majority
and secondary UR roles. It binds exact public identities, independent signer
pins and runtime/source/deployment to the retained protected generations.
Current stake/permit, key possession, operator evidence and healthy services,
deployed contracts and runtime authenticity remain unproven. V1 journals retain
their original limited status and cannot be silently upgraded. This addition
and its shared config decoder extraction are now [qualified](evidence/ur-bootstrap-admission-qualification-20260928.md)
on the frozen composed source graph: 588 root and 183 descendant executions
passed, with six causal controls in normal/race modes. The result covers bounded
offline admission; actual chain effects and a complete release remain open.

The v3 offline addition independently pins the separate netuid-0 root role,
its original action approver and a distinct full-service-config approver. It
verifies a domain-separated signed approval of the exact child root plan and
service configuration, retaining only a public inspection and live-authority-
pending status. V1/v2 journals keep their prior domains and recovery scope.
The [qualification](evidence/root-role-admission-qualification-20260928.md)
passed 403 full normal roots, all 38 selected bootstrap-chain race roots in
three disjoint shards, seven metrics fixture race roots and vet. Live root
eligibility, key custody, service activation and a composed production release
remain separate gates.

`check-recycle-mode --rpc URL --policy FILE` binds the finalized mode read to
independently supplied mainnet genesis, runtime code/metadata and complete
version pins. It validates the runtime-declared map, enum and Burn fallback and
reports whether the finalized value is Recycle. `economic-reference --input FILE`
computes cumulative integer 10% provider / 90% recycle references from
caller-supplied native miner tranches, carrying rounding between intervals.
Both retain `activation_ready=false`: the first proves only mode storage under
the approved artifact, and the second does not authenticate interval inputs or
actual Yuma payouts. [Command contract and remaining gates](ECONOMIC-GATE.md)
cover the source-to-code, owner-hotkey, allocation and observation work. Focused
normal/race tests and vet pass after the 2026-09-27 data-volume recovery.

`inspect`/`monitor` remain single-route identity/finality observers. The
separate `runtime-snapshot` captures raw finalized code/metadata without
granting authority. `inspect` and `monitor` validate the complete runtime tuple
internally, but their v1 JSON reports only spec/transaction numbers; emit a
versioned full-tuple observer artifact before using them as independent runtime
identity evidence. Unknown digest variants require reviewed decoding support.
These commands do not map EVM receipt finality, compare independent nodes, inspect SN25
custody/validator/settlement state, deliver alerts,
or execute repairs. Those are MG-07 and related production gates. An identity
snapshot hash proves the captured bytes, not operator approval or node truth.

The [pure plan foundation](PLAN.md) consumes one `finalized-snapshot`, a source
lock and release inputs by exact hashes. `plan --outline` exposes the unbound
dependency graph while approved mainnet identity is unavailable;
`plan --config FILE` accepts the separate strict JSON review schema and refuses
testnet EVM945 or an unexpected genesis. Both modes keep every action blocked,
with no apply authority. Supplied review manifests remain unvalidated until
their actual semantic/capability/custody checks are implemented.

Keep the executable plan builder pure after authenticated snapshot inputs are supplied.
Separate chain adapters, signer interfaces, state storage and supervisors so
preview cannot reach a transaction submission path.

Target command surface; `inspect`, `runtime-snapshot`, `finalized-mapping`, `finalized-snapshot`, `monitor`, `subnet-preview`, the two root observers and the two
limited economic preconditions above, `source-lock`, `release-inventory` and the blocked-review `plan` foundation exist, while the remaining commands are
designs:

| Command | Behavior |
| --- | --- |
| `inspect` | Extend the existing read-only identity capture with authority, census, capabilities, balances, custody and validators; emit a hashed snapshot. |
| `runtime-snapshot` | Existing signer-free capture of one finalized runtime's exact `:code` and metadata bytes, complete version and node identity; independent approval and source-to-Wasm review remain separate. |
| `finalized-mapping` | Existing signer-free capture of linked finalized native and raw EVM header commitments with an owned-RPC canonicality assertion; no signing or mainnet approval. |
| `finalized-snapshot` | Existing signer-free same-block capture of runtime code/metadata and native/EVM mapping; its output remains unapproved observation. |
| `monitor` | Extend the existing read-only identity/finality loop with durable checkpoints, independent comparisons, complete domain health and existing-stack alert delivery. |
| `subnet-preview` | Existing signer-free finalized SN25/root UID census and owner-trim candidate comparison; full custody and execution-time reset authority remain open. |
| `subnet-discover` | Signer-free runtime 470 membership/owner/generation discovery from a retained unapproved runtime or combined finalized snapshot; leaves every seat unclassified and supplies no trim policy or apply authority. |
| `root-preview` / `root-monitor` | Existing signer-free finalized root seat and strategy census; an offline [existing-seat action core](ROOT-ACTION.md) exists, but production signing and activation remain separate work. |
| `check-recycle-mode` | Existing signer-free finalized storage-mode precondition; extend with an approved mainnet artifact and operational readback at activation/recovery. |
| `economic-reference` | Existing signer-free cumulative integer 10%/90% reference from caller-supplied native intervals; actual chain reconciliation remains a separate gate. |
| `source-lock` | Existing offline lock of clean SN and every local Go replacement Git commit, module checksums, Go version and tool hash. It binds source inputs only; artifacts, rollout approval and qualification remain separate. |
| `release-inventory` | Existing local candidate inventory of exact executable/contract/config/policy/migration/image/dependency/toolchain files, bound to the rechecked source lock; missing categories remain explicit, and release completeness/provenance/deployment approval stay false. |
| `plan` | Existing pure blocked-review graph via `--outline` or strict JSON `--config FILE`; hashes exact finalized-snapshot/source-lock/release inputs. Every action remains non-executable. Full semantic admission, payloads and executable authorization remain future work. |
| `apply --accept-plan HASH` | Execute only the exactly reviewed plan with matching signed authorization, prerequisites and ceilings. |
| `status` / `verify` | Read-only journal reconciliation and current/finalized postcondition verification. |
| `resume --accept-plan HASH` | Recover in-flight actions, verify retained receipts and continue the same approved graph without duplicate spend. |
| `services start` / `services stop` | Run or join the plan's admitted services; starting write-capable validators is an explicit authorized phase. |
| `report` | Produce a complete acceptance or incomplete/blocked report with evidence references and realized spend. |
| `repair plan` / `repair apply --accept-plan HASH` | Produce and execute only the exact approved, bounded repair graph; share the existing durable transaction owner and lifetime ledger. Not implemented. |

`source-lock --sn-dir /absolute/sn/path` emits a content-hashed JSON record of
the clean SN Git HEAD and every local `go.mod` replacement's clean Git HEAD,
the exact `go.mod`/`go.sum` hashes, current Go version and command-binary hash.
It refuses modified or untracked repository files, ignored replacement module
files and active `go.work` overrides; it rechecks each HEAD after hashing. It
reads no RPC and holds no signer. This is one input to the release
manifest, not an approval or a claim that compiled binaries, Foundry bytecode,
generated files, ignored files, configuration, migrations or the running images
match those commits. The release owner must bind those artifacts separately,
qualify the composed source and approve the resulting immutable manifest.

`release-inventory --config FILE` provides the [actual-file candidate
inventory](RELEASE-INVENTORY.md). It rechecks source closure and selected file
bytes, refusing stale per-artifact source bindings. It enumerates missing
categories but does not infer complete coverage from one file per category,
prove a build or image, inspect a deployed migration state, or approve a release.
Ignored Solidity libraries and compiler identities are explicit dependency and
toolchain inputs; they are not included by the Go source lock alone.

The [current release builder](RELEASE-BUILD.md) composes all seventeen selected
SN/server commands, all five production contracts and eight image contexts.
Server `898dc8f3` aligns SDK, Connect and the SCTP fork with SN's exact reviewed
module versions/sums; independent qualification builds all thirteen server
commands after the original nine failures. The builder pins clean source trees,
effective module graphs, module zip bytes, tool identities, binary build info,
retained/selected/compiled contract hashes, recipes and migrations. The default
retained catalogue keeps Coordinator and ValidatorEvidence's historical metadata
bytes, so its exact source-to-bytecode equality remains false. Explicit
`contract_catalog: "fresh"` selects exact creation/runtime compiler output for
all five contracts, preserves the historical catalogue separately and refuses
ABI, constructor, storage-layout or immutable-reference changes. The selected
schema-1 catalogue is consumed through the bootstrap plan's exact file path and
SHA256; the checked-in binding does not need replacement.

The [fresh-catalogue qualification](evidence/fresh-contract-catalogue-build-20260930.md)
now records a complete local candidate: all seventeen binaries built, all ten
contract creation/runtime pairs exact, and all five deployment interfaces
unchanged. Independent Sol qualification passes the 31 builder roots normal/race,
vet, causal controls and complete artifact readback. The receipt pins the selected
catalogue separately from the unchanged historical files.

The [bounded scratch-image qualification](evidence/scratch-image-qualification-20261001.md)
builds and independently reads back the exact `server-competitionworker`
Linux/amd64 image from this candidate. A separate Sol build reproduced its OCI
archive bytes. The [seven-service offline image qualification](evidence/seven-service-image-qualification-20261001.md)
now supplies pinned local Ubuntu OCI and package inputs for the remaining seven
unchanged production recipes. Its independent rebuild produced byte-identical
archives and passed complete OCI readback. This qualifies a local image build,
not a published or deployed release; aggregate release and deployment flags
remain false.

The [offline aggregation command](RELEASE-BUILD.md) can now verify those separate
receipts against one exact original source manifest and emit a sealed eight-image
attestation. Its [frozen-candidate qualification](evidence/release-image-aggregate-qualification-20261001.md)
binds SN `2d53e6f2` and server `ecbf3aad`, rehashes all 337 input artifacts and
replays the OCI/rootfs checks against their exact parent binaries and recipes.
Only the aggregate's local `source_to_image_verified` advances; original evidence
is unchanged and reproducibility, release completion and deployment approval
remain false. This operation needs no Docker service and performs no build,
publication or application execution.

The later [unsigned preparation](evidence/public-unsigned-preparation-20261001.md)
locks SN `de0823ce` and server `0b8e758d`, including the newer validator and
operator custody changes. Its source lock and partial inventory retain the
actual reviewed Connect/SDK module pins separately from observed newer main
heads. It does not inherit the older aggregate's image provenance. That bundle
does not supply its own complete source/image composition, and keeps release
completion and deployment approval false.

The [pre-Safe baseline and permission repair](evidence/release-pre-safe-baseline-and-modes-20261001.md)
subsequently build SN `1806b3b3` / server `0b8e758d` twice with empty compiler
caches: all seventeen executable hashes and ten contract creation/runtime
outputs match. The API image attempt correctly rejects a `0700` executable
created under the build host's private umask. The qualified builder correction
preserves exact requested permissions without weakening OCI verification or
changing retained outputs. All eight corrected OCI exports and their separate
337-artifact aggregate now pass, establishing local source-to-image coverage
for this exact baseline. This baseline predates the later public
Safe-submission implementation; a release of that source needs a fresh exact
source/image build. Same-host repeatability does not establish independent
reproducibility or authorize a deployment.

The [frozen Safe-source release](evidence/release-safe-source-20261001.md)
then binds exact SN `095a2208` / server `0b8e758d`, including the Safe and
permission corrections. It builds all seventeen commands and five fresh
contracts twice and supplies eight new OCI images with a complete local
source-to-image aggregate. All eight platform manifests and OCI archives also
match on repeat. Its 55-file inventory repeats exactly, while
production policy and published/deployed image identity remain absent. The
frozen source commit is distinct from later reporting commits; independent
reproducibility, actual service/configuration/policy qualification and release
approval remain open.

The [current-admission packaging baseline](evidence/release-6c801a25-server720-20261001.md)
now binds exact SN `6c801a25` / server `720e7c61`, retaining the same
Connect/SDK dependency pins. Its two fresh-cache builds match all seventeen
executables and ten contract bytecode outputs; eight OCI images have a complete
local source-to-image aggregate. All 89 builder roots pass normal/race and
vet. Its source migration inventory records 750, without applying it. Any
launch selecting later passive-root host, owner recycle-mode transition or
mixed-writer/migration-751 changes requires a successor source/image build;
this baseline does not attest them.
Independent reproduction, actual configuration/policy, rollout and release
approval remain open.

The [successor preparation](evidence/release-successor-preparation-20261001.md)
checks clean SN `7ee916cc` / server `94229abb`, including passive-root host
and schema 751, with unchanged Connect/SDK pins. Both offline module graphs
and all 89 builder normal/race roots plus vet pass; 40 package payloads and
the local base blobs are rechecked. Its 751-entry migration source inventory
does not apply a migration. Final source/image qualification is held until
the owner recycle-mode transition merges; no successor release seal or
approval is claimed by this preparation.

No signed mainnet deployment plan has been evidenced. The existing catalogue
is release/testnet history, not established mainnet signing authority. Fresh
catalogue review and independent qualification are the preferred path for the
first unsigned mainnet plan. Checking for externally held signed commitments
remains a launch gate; any such plan, artifact or transaction must be preserved
and reconciled before selection changes. A metadata-equivalence exception is
only a conditional fallback, not selected by this path. Earlier independent
image reproduction remains scoped to SN `2d53e6f2` / server `ecbf3aad`; the
later source releases keep their separate independent-builder gate.
Full compiler/config/policy qualification and release approval remain open, as
do published-image identity and running-image readback.

The [earlier composed local candidate](evidence/release-candidate-v11-20260927.md)
locks SN `265231f9`, server `77cb401e` and Connect `c68689c4` with all local
Go replacements. Its [partial actual-file inventory](evidence/release-inventory-candidate-v11-20260927.json)
hashes 85 selected files, including seven locally built executables, all eight
server Dockerfiles, all seven image-build Makefiles, the exact package lock,
40 Ubuntu payloads, six signed-index inputs and four copied contract artifacts.
It has no approved policy or
published/deployed OCI image identity and remains unapproved.
The September 27 server source branch `codex/mainnet-composed-hardening-20260927` at
`b6f49bdb` composes the append-only migration 728 and retained-usage reader,
the operator receipt-census recovery correction, atomic payer admission and
checked settlement arithmetic on the v11 base. The earlier `7bf88d79`
combined controller selector passed normal, race and vet on disposable
PostgreSQL/Redis ([composed evidence](evidence/server-composed-hardening-20260927.md)).
This changes the source identity: the v11 source lock,
inventory and rebuilt artifacts do **not** attest the newer branch. Refresh
the complete manifest and production-path qualification before approving a
deployment. [Timestamp custody](/mnt/data/sn-testnet/evidence/mainnet-usage-time-custody-20260927/RESULT.md),
[operator recovery](evidence/operator-recovery-census-20260927.md).
The subsequent [MG03/R48 source composition](evidence/operator-mg03-r48-composition-20260929.md)
is integrated at server root `05fee56f`. Its full-merge parent preserves both
original histories and the complete qualified census/receipt/controller
lineage; the successor adds only two composition tests. Sol's normal/race
receipts cover 134 affected roots on the merge and, separately, two new plus
52 adjacent model roots on the successor. Their exact pinned physical graph
does not qualify the current dependency roots as a combined release. Refresh
the source/artifact lock and composed build before deployment.
Mainnet inventory implementation passed 177 normal tests and all 177 race
test bodies in a bounded run plus exact continuation; the latter is not one
whole-package race pass. A subsequent validator test-fixture correction passed
87 affected tests normally and under race without changing production seed
custody. The [unbound blocked outline](evidence/blocked-plan-outline-20260927.json)
names ten non-executable actions and 24 missing requirements. Its negative
control rejects retained Snow testnet EVM ID 945; the earlier
[same-block Snow observation](evidence/source-lock-finalized-snapshot-20260927.md)
authenticates code, metadata and linked native/EVM headers but remains
unapproved. The earlier [composition](evidence/source-lock-composed-20260927.md)
separately qualified unchanged validator and receipt selectors. This is
offline qualification of those source paths, not a complete release or an
approved mainnet configuration. The prior
[v7 source and contract build](evidence/source-lock-blocked-plan-20260927.md)
remain linked evidence; subsequent evidence-only commits do not alter the
frozen earlier candidates' Git identities.

The earlier v8 binaries use a local exploratory Go profile. An offline
[production-style probe](/mnt/data/sn-testnet/evidence/mainnet-source-lock-v8-20260927/production-profile/RESULT.md)
also builds static, trimmed, version-stamped Linux/amd64 SN executables, but
they are not in the v8 inventory or an approved image. The pinned server API
and taskworker likewise build and repeat exactly from clean source; their
[build record](/mnt/data/sn-testnet/evidence/mainnet-server-binaries-20260927/RESULT.md)
is retained. Server commit `969d6c74` first pinned the six service Dockerfile
bases that still used a mutable Ubuntu tag. The subsequent [v9 image
probe](/mnt/data/sn-testnet/evidence/mainnet-images-v9-20260927/RESULT.md)
found that `apt-get` still read moving Ubuntu repositories. Server commit
`a211d56c` pins complete package payloads for seven service Dockerfiles,
including proxy's `curl` closure, and installs them offline. The
[package qualification](/mnt/data/sn-testnet/evidence/mainnet-server-package-pins-20260927/RESULT.md)
authenticates signed indexes and 40 payloads; three normal/race contract tests
and vet passed. Independent [amd64 API/proxy image
probes](/mnt/data/sn-testnet/evidence/mainnet-package-pin-probe-20260927/RESULT.md)
built those recipes and extracted exact selected binaries. The
[v10 build and inventory record](/mnt/data/sn-testnet/evidence/mainnet-source-lock-v10-20260927/RESULT.md)
binds the clean composition and replays its 81-file inventory byte-for-byte.
Choose approved production versions and architectures; archive exact package
and base inputs; qualify arm64 image execution, full service behavior,
source-to-image provenance, selected published OCI manifests and running-image
readback before MG-02 can close. Local binaries and image IDs are not an
approved rollout.
An [uncached API rebuild](/mnt/data/sn-testnet/evidence/mainnet-package-pin-probe-20260927/RESULT.md#forced-rebuild-result)
produced a different OCI digest from identical pinned inputs; package logs,
cache and timestamps varied. The release build must either normalize those
outputs and prove exact repeatability, or identify an independently reviewed
immutable image without claiming reproducible bytes.
Server `77cb401e` removes only the two volatile generated files and gives all
seven image recipes a fixed source epoch and timestamp-rewriting exporter.
The [no-cache OCI qualification](/mnt/data/sn-testnet/evidence/mainnet-server-image-repro-20260927/RESULT.md)
repeated API and proxy runnable amd64 platform manifests, configs and layers
exactly. Its top-level indexes remained distinct because provenance described
different invocations. The source-level contract tests passed normal/race,
with offline image smoke. A separate [v11 taskworker image check](/mnt/data/sn-testnet/evidence/mainnet-server-taskworker-image-20260927/RESULT.md)
repeated its exact runnable amd64 platform image twice and verified the embedded
candidate binary in an offline container. Keep arm64, remaining service images, independent builder,
full attestation/SBOM/scanner policy, owned archive, registry publication and
deployed readback open.

The [incremental SDK/MG06 composition](evidence/incremental-source-composition-20260929.md)
preserves the original source histories and the reviewed versioned SDK, Connect
and SCTP pins. Its 37 focused roots pass normal/race and four mainnet smoke roots
pass normally; the original SDK candidate has complete 618-root normal coverage.
Its race root-body union is 618/618. The [corrected SDK snapshot](evidence/bootstrap-contract-successor-qualification-20260929.md#separate-sdk-package-coverage-and-scheduling-lesson)
now records 543/618 backed by package-PASS streams, with 75 pending and no root
failures. This separate source graph still lacks broad race package closure.
The bounded
[`observe-native-miner-emission` reader](ECONOMIC-GATE.md#bounded-native-incentive-observation)
retains canonical event/state evidence with complete and partial outcomes. Its
18 focused and 170 adjacent roots pass normal/race, with eight causal controls.
Native denominator, quantization, recipient generation, entitlement, actual
owner recycling and the 10/90 outcome remain unresolved. This incremental
source integration does not approve a release or mainnet activation.

MG-10 testing lesson (2026-09-29): the SDK-pinned 618-root unsharded race
process reached its one-hour package timeout while
`TestEvmEscrowRegisterClaimRecoveryKeepsFourLocks` had been active for about
25 seconds; no top-level root failure was observed. The subsequent
[audit](evidence/incremental-source-composition-20260929.md#completed-and-pending-qualification)
records 618/618 top-level root-body passes, but only 410/618 have a pass inside
a stream with package PASS; 208 still require terminal package coverage.
Preserve the original unsharded and six initial shard timeouts; wave2/08 had
no package terminal at that snapshot. Use disjoint exact root shards, retain
each shard's source fence, package exit and original timeout, and preserve
completed results without resetting their evidence. Broad race package
qualification remains open.

The [later scheduling review](evidence/bootstrap-contract-successor-qualification-20260929.md#separate-sdk-package-coverage-and-scheduling-lesson)
records 543/618 package-backed SDK roots and 75 pending. The evidence predecessor
checkpoint root runs fourteen serial fixtures and passed in about 28 minutes in
both its shard and exact-root retry; no product failure was established. Give
that root its own package/time budget and group other roots by measured duration
without reducing causal cases. Preserve the earlier timeout evidence.

The [owner-recycle measured decision](../validator/OWNER-RECYCLE-MEASUREMENT.md)
now joins signed successor approval, exact native owner census and fully
replayed original V2 provider proofs in a distinct capsule. It reconstructs
the proposed 10% provider / 90% owner weight row, but emits only a blocked
unsigned intent. It cannot enter the existing native signing/submission path;
validator eligibility, operator health, complete history, drained activation,
custody, archive and observed final incentives remain MG-06 gates. Its affected
139-test race selector and final-source 58-test focused normal selector passed;
the [qualification record](/mnt/data/sn-testnet/evidence/mainnet-owner-recycle-decision-20260927/RESULT.md)
states the exact limits. The next operator-observed variant invokes the actual
canonical coordinator reader after full proof replay: it checks active registry,
pool and provider mappings, exact deposit/conviction amounts, policy and source
root/window against the measurement, then rechecks chain/genesis and the exact
EVM decision hash. Its distinct v2 capsule binds those retained facts to the
original provider bytes; v1 capsules remain byte-compatible. This resolves
decision-time coordinator claims without treating active registration as API
health or independently proving the native/EVM mapping. Native validator
eligibility, API/key/payout history and all activation/signing gates stay open.
Its [qualification record](/mnt/data/sn-testnet/evidence/mainnet-owner-recycle-readiness-20260927/RESULT.md)
retains 138 normal and 138 race passes, vet, cross-compile and the final
naming-only follow-up without claiming launch approval.

The [production transition](OWNER-RECYCLE-PRODUCTION.md) separately enables the
actual standard validator under an independently signed schema-3 config. It
authenticates the exact compatible producer interface, validator stake/permit
and bounded activity, owner census, canonical operator facts and the pinned
zero-pending-emission activation block, then signs the measured row through the
real CRv4 source batch. Its sidecar remains bound through durable intent recovery
and independent archive observation. The same approved config can continue its
finite epoch window and replay historical decisions after head/cache changes.
Bounded content-addressed original config/approval bundles now preserve prior
sidecars across compatible independently signed renewals, source-file loss and
restart. The original drained activation and proof progress remain intact;
current signing never inherits an old grant. Policy, signer and custody migrations
remain separate transitions. Actual mainnet inputs and economic outcome remain
unprovided. Final economic outcome is a monitored
postcondition, not a prerequisite to the first submission.

PH-03 remains open for production continuation across read outages and epoch
changes. The [source-finality read candidate](evidence/source-finality-read-candidate-20260928.md)
preserves transport causes and unknown receipt evidence while reusing the exact
admitted body index/events. The paired
[adjacent-read candidate](evidence/production-read-cause-adjacency-candidate-20260928.md)
covers miner recovery, owner census/eligibility and activation setup. Their
[combined qualification is complete](evidence/source-read-cause-qualification-20260928.md),
with original fixture failures and corrections retained. They do not substitute
for retained-intent reconciliation before fresh snapshots, durable nonempty
intent-owner tests, bounded authenticated receipt prefixes or same-boundary
nonce/expiry decisions. Those changes must preserve original signed bytes,
missed/unknown outcomes and independently approved continuation authority.

The public startup lifecycle now reaches authenticated retained recovery before
requiring fresh signing eligibility. Its former native-runtime, EVM snapshot,
UID and stake ordering could prevent the historical activation/disk/intent
owners from opening. The separate startup scope below exercises the complete
public root through an outage, original receipt and applied row while keeping
current authority checks for new signing.

The [qualified production continuation](evidence/production-continuation-candidate-20260928.md)
moves the actual production `Run` branch and `submitOnceV2` toward the retained
intent before unrelated fresh scheduler/runtime/EVM observations. Its first
qualification scope is real nonempty V2 begin/replay/update/restart, exact
original receipt and application, and one canonical absence/nonce boundary.
Production read waits use 300 seconds overall with 60-second attempts; exhausted
reads remain visible waits, and mixed integrity/custody failures remain hard.
These native signatures use an immortal era: a later native epoch or local
approval deadline cannot revoke already signed bytes. Unknown work retains its
original signature and age until receipt or authenticated foreign nonce use at
the scanned boundary resolves it. Missed opportunities remain missed.
All 10 CRv4 and 22 validator roots now have normal/race coverage, with the original
race timeout and exact six-root completion retained separately. Six regression
control families reached their intended assertions in both modes. Full
`RunRelease` activation, normalized config and dual-upload composition have
their separate component results below. Bounded scan-prefix reuse and
historical-only foreign-nonce resolution now have the separate qualification
below; remaining startup, release and live-authority work keeps MG-04 and PH-03
incomplete. The separately
[qualified native HTTP integration](evidence/production-native-http-integration-20260928.md)
connects physical native causes to phase-owned waits and corrects response/close
boundaries. Composed native/EVM startup failures require their later scope.

The integrated [public startup continuation](evidence/production-startup-continuation-candidate-20260928.md)
reopens original native observation at its signed activation block before
unrelated current preparation. The public `RunRelease` root still authenticates
activation, both operator histories, real disk ownership and original intent
custody; current UID/stake and initial settlement publication gate fresh intents
and trail workers. Local semantic replay uses the caller's lifecycle rather than
a five-minute I/O deadline. Independent transient native/EVM branches compose
without losing their physical causes or hiding mixed integrity errors.
Its affected roots now have normal/race coverage, five causal controls reproduce
the intended failures in both modes, and validator vet passes. The receipt
preserves original fixture failures, reused passing scopes and the missing
pre-execution dependency-content seal; final release composition remains open.
The empty-store case requires independently
provisioned client identities, keys and existing JWTs; first-client registration
is not part of that claim. Both operator server-key/public-object/session routes
remain startup dependencies, and initial missing-JWT registration failure can
still return from the root after disk replay. That potentially mutating identity
operation needs its own durable reconciliation, not generic read retries.
Native production submission uses an explicitly approved WS/WSS route for
`author_submitAndWatchExtrinsic`; the same node may provide HTTP EVM/read RPC,
but HTTP read support alone grants no native subscription or writer capability.

The [operator registration candidate](OPERATOR-REGISTRATION.md) addresses the
separate first/missing-client barrier with durable request/identity custody, a
dedicated idempotent server route and an independent authentication worker in
the actual public root. Retained native observation is independent of live JWT
readiness; new publication, trails, signing and rebroadcast remain gated. Fresh
creation requires signed per-operator `allow_client_registration`, default false
with historical encoding preserved. Existing operations reconcile regardless
of that flag. Its [withdrawal recovery correction](evidence/operator-withdrawal-ownership-qualification-20260928.md)
is integrated: 20 affected roots and both causal controls pass normally and
under race detection. Original failed parent captures remain retained. Final
dependency composition and the additive server-first migration/rollout remain
pending; no live identities or deployment are supplied by this source change.

The qualified and integrated [provider client-registration change](evidence/provider-client-registration-candidate-20260929.md)
migrates `provide` and `auth-provide` to the existing versioned API protocol.
The provider seed and original request are durable before allocation; direct
and proxy slots retain their actual endpoint/key identity without a fabricated
validator or chain identity. Explicit new-registration permission and explicit
legacy-key adoption are separate decisions. Registration retries and required
refresh/logout writes stay bound to the original physical custody directory.
All 114 selected roots pass per normal/race mode (228 executions), and all 17
causal variants are valid in both modes (34 executions). The selection covers
114/2324 package roots; it is not full miner/validator coverage. Original e32
fixture failures remain sealed separately. Daemon fixtures reach authenticated
handoff before serving-device construction, with separate callback fixtures.
The no-config measurement validator's durable primary identity has its separate
qualification below. API deployment, real custody/adoption decisions,
processed-key and proof readiness, chain/contract authority and all live launch gates remain
open; this component does not close MG-04 or PH-13.

The qualified and integrated [measurement primary-client change](evidence/validator-measurement-client-registration-candidate-20260929.md)
addresses that no-config validator path. It retains the original key and a
distinct direct `validator-measurement-v1` scope, always uses `allowCreate=false`,
and adopts an existing JWT or replays its exact retained operation.
Unknown lost identity requires recovery. Registration and refresh borrow the
original physical key directory; completed authentication releases the separate
bootstrap lock while retaining the key owner through joined shutdown. The
runner bypasses key regeneration and returns through cleanup instead of an
inner process exit. The `c3fe0cf2` parent remains unqualified: its CLI fixture
used docopt's process-exiting parser and aborted both focused validator packages.
Test-only child `e33f64d4` preserves production bytes and uses non-exiting fixture
parsers. Independent normal/race qualification passes all 24 focused plus 13
adjacent roots in each mode across twelve package PASS/exit-zero streams, and all
38 causal executions reach their intended assertions. Separate Astra and
integration audits verify the sealed results and exact source/module graph;
integration adds only documentation above that source. This is selected coverage.
Existing ephemeral tunnel-client allocation, legacy release-config callers and separate
proof/stats history ownership remain outside this primary-identity repair.
Its local lifecycle fixture reaches seed discovery and shutdown, not live trail
completion. API deployment, actual custody, approved chain/Safe authority,
economic acceptance and independent monitoring remain required; MG-04 and PH-13
stay open.

The [qualified receipt-prefix recovery](evidence/receipt-prefix-qualification-20260928.md)
now supplies the shared 128-block scan contract to both native recovery owners.
Miner records retain their existing signed semantic proof. The validator keeps
one bounded, original-intent-bound signed acceleration file; cache eviction or
an incompatible cache schema causes a rescan while custody and exact signed
bytes remain intact. Completed chunks and admitted partial prefixes persist
before subsequent reads, including retry after a later timeout. Optional cache
read/write faults disable disk acceleration and report degradation while original
intent reconciliation continues; memory-only progress is not a durability claim.
The separately qualified public diagnostic exporter now exposes degradation. No
per-block full journal rewrite or executable-hash invalidation is introduced.
Pending old approvals can resolve authenticated foreign nonce use at the exact
covered boundary while fresh signing/rebroadcast stays independently gated.
All 56 affected roots have scoped normal/race passes after retained fixture
corrections; nine causal families and package vet pass. The receipt preserves
failed captures and the incomplete pre-execution seal for one fixture scope.
Final release composition, first-client recovery and live authority remain open;
this component does not close MG-04 or PH-03.

The [server upload admission](VALIDATOR-UPLOAD-RUNTIME.md) now consumes an
independently pinned schema-3 configuration and retains only read-only runtime
intervals, routes and deployment scope. Original activation reads use their
signed historical window; current eligibility requires the unexpired current
window. Its detached projection includes validated original authority bundles
without carrying economic configs or signing authority. The ordinary upload signer and server-used admission constructor are
joined in local deterministic tests. This closes the downstream tuple-only
history gap without granting writer capability or proving remote delivery,
original economic authority, mainnet deployment or live approval custody.

No implicit apply, automatic subnet creation, private-key CLI flags, “force” bypass, mutable `latest` artifact, or inherited network defaults. Every mutating command takes an explicit run directory and accepted plan hash. Read-only discovery may run while identity or other gates remain unresolved; executable plans and mutating phases require their actual production prerequisites.

The canonical plan binds schema and action-format versions; exact config/policy bytes; resolved configuration roots and runtime routes; source/dependency/artifact/binary identities; owned-node and runtime identities; native/EVM snapshot hashes; all public roles; census and reset classifications; actual transaction payloads/origins; expected CREATE addresses and nonces; phase dependencies; validity windows; spend/count caps; and the chosen emission-denominator/remainder policy. Hash canonical bytes with domain separation. The signed authorization names that hash, network, expiry, allowed phases and ceilings. Reject duplicate fields, unknown schema versions, overflow, unexpanded substitutions and ambiguous addresses.

Implemented review commands, with no signing or submission:

```sh
sn-mainnet plan --outline > /secure/ur-mainnet/review/outline.json
sn-mainnet plan --config /secure/ur-mainnet/plan-config.json > /secure/ur-mainnet/review/blocked-plan.json
```

The JSON config and release-input schema are in [PLAN.md](PLAN.md). The resulting
blocked-plan hash cannot be passed as executable apply authority. Review schema
v2 separates action preconditions from produced postconditions: deployed getter
proofs follow installation, revealed/applied validator rows follow activation,
and realized native economics follow the first approved submission. None is a
circular prerequisite to its own producer.

The separate implemented local phase uses its own strict JSON schema and exact
independently approved root custody packet:

```sh
sn-mainnet bootstrap plan --config /secure/ur-mainnet/bootstrap-root.json > /secure/ur-mainnet/review/root-custody-plan.json
sn-mainnet bootstrap apply --config /secure/ur-mainnet/bootstrap-root.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$ROOT_CUSTODY_PLAN_HASH"
sn-mainnet bootstrap resume --config /secure/ur-mainnet/bootstrap-root.json --run-dir /secure/ur-mainnet/run --accept-plan-hash "$ROOT_CUSTODY_PLAN_HASH"
```

[BOOTSTRAP-ROOT.md](BOOTSTRAP-ROOT.md) specifies its public-signature import and
exact recovery contract. It does not perform owner trim, contract installation,
UR activation or a root broadcast. Complete contract installation must include
the `STValidatorEvidence` journal and its anchor/coordinator binding; the
existing `Deploy.s.sol` alone is insufficient. The testnet EVM945 deployment
manager is not a mainnet adapter.

The remaining operator examples below are future interfaces requiring an executable
schema and complete semantic admission; the implemented `plan` does not accept
the draft YAML config, `--snapshot`, `--phase` or `--out` flags.

```sh
sn-mainnet inspect --config /secure/ur-mainnet/bootstrap.yml --out /secure/ur-mainnet/inspection
sn-mainnet apply --plan /secure/ur-mainnet/review/plan.json --accept-plan "$REVIEWED_MAINNET_PLAN_HASH" --authorization /secure/ur-mainnet/authorization.json --run-dir /secure/ur-mainnet/run
sn-mainnet status --run-dir /secure/ur-mainnet/run
sn-mainnet resume --run-dir /secure/ur-mainnet/run --accept-plan "$REVIEWED_MAINNET_PLAN_HASH" --authorization /secure/ur-mainnet/authorization.json
sn-mainnet verify --run-dir /secure/ur-mainnet/run --out /secure/ur-mainnet/verification
```

This future executable YAML config sketch is neither current JSON input schema.
It intentionally contains `null` for unapproved identities and monetary values.
A real executable plan must reject them. Values represent required fields, not
suggested budgets or fake addresses; secret material is supplied through local
signer references rather than embedded here.

```yaml
schema: urnetwork-mainnet-bootstrap-v1
network: mainnet
deployment_id: null
netuid: 25
owned_node:
  substrate_url: null
  evm_url: null
  ownership_attestation: null
  expected_genesis_hash: null
  expected_evm_chain_id: 964
  runtime_artifact_manifest: null
  rpc_pacing: none
  fallback_urls: []
release:
  source_lock: null
  contract_manifest: null
  binary_manifest: null
  policy_file: null
  closed_testnet_report: sim-testnet/FINAL-4.md
  known_exceptions_manifest: null
  production_qualification_manifest: null
roles:
  subnet_owner: null
  evm_deployer: null
  coordinator_safe: null
  guardian: null
  commitment_oracle: null
  root_validator: null
  ur_validator: null
  independent_ur_validators: []
  reserve_hotkey: null
  escrow_hotkey: null
reset:
  mode: unresolved
  census_file: null
  remove_generations_file: null
  preserve_identities_file: null
  registration_during_cutover: null
emissions:
  denominator: native_miner_allocation_before_withholding
  provider_fraction: {numerator: 1, denominator: 10}
  assurance: observed-native-target
  quantization_tolerance_manifest: null
  remainder: owner-recycle
  mechanism_manifest: null
  activation_boundary: null
root_validator:
  registration_mode: null
  auto_parent_delegation: false
  basket_strategy: accumulate_in_place
  custom_weights: []
  stake_rao: null
  delegate_take: null
ur_validator:
  release_config: null
  stake_plan: null
  independent_validator_evidence: null
limits:
  total_tao_debit_rao: null
  total_alpha_commitment_units: null
  evm_fee_wei: null
  native_fee_rao: null
  per_registration_burn_rao: null
  root_registration_debit_rao: null
  maximum_registrations: null
  maximum_subnet_creations: 0
  maximum_transactions: null
  stake_price_limits: null
  expiry_finalized_block: null
operations:
  run_dir: /secure/ur-mainnet/run
  signer_manifest: null
  service_manifest: null
  worker_limit: null
  monitor_manifest: null
  independent_reader_manifest: null
  telemetry_and_alert_routes: null
  slo_manifest: null
  repair_authorization: null
  primary_on_call: null
  backup_on_call: null
  incident_evidence_store: null
  rollback_compatibility_manifest: null
```

The complete schema also requires action-level value/gas/fee bounds, collateral exposure, swap price/minimum-output limits, claim/deposit policy caps and fee reserves. Totals aggregate economic debits once across EVM and native representations. Refunds are recorded separately; they do not replenish lifetime authorization unless the plan explicitly defines that rule. Reverted transactions consume fee budget. New attempts, repairs and replacements retain the same lifetime ledger.

## Phases, finality and recovery

| Phase | Admission and work | Completion evidence |
| --- | --- | --- |
| 0. Evidence and production qualification | Preserve failed R48 and known exceptions; compose the launch release and close applicable MG-02 through MG-07 pre-activation checks on a controlled production-path rehearsal. Read-only identity discovery may proceed independently. | Exact release/qualification/exception manifests; no fabricated testnet pass or provisional authority carried into mainnet. |
| 1. Mainnet inspect and review | Verify node/runtime, complete census, owner authority, capabilities, keys, artifacts, reset method, selected owner-recycle mechanism and budgets. | Canonical feasible plan and exact operator authorization; any expected retained old miners have an explicit launch disposition. |
| 2. Cutover/reset | Execute the strongest safely admitted owner-key trim and configuration changes within their native windows. | Complete before/after census, preserved identities, actual removals, retained old miners and accounted locks; never label a partial trim a full reset. |
| 3. Contracts and registration | Deploy exact custody graph, anchor evidence, register approved pool/escrow/head/validator identities. | Canonical finalized receipts, code/getter proofs and registration ownership. |
| 4. Stake and service readiness | Apply bounded stake/deposit plans; admit both services, the second UR validator/operator safety set, independent monitor and on-call. | Current root membership, UR eligibility, authenticated runtime configs, single service ownership, delivered test alerts and qualified repair/rollout policy. |
| 5. Emission activation | At the approved native boundary, activate the qualified 10% mechanism and corresponding signed UR policy. | Native incentive outcome, remainder destination, weights, stake/collateral deltas and policy epoch agree. |
| 6. Acceptance and operations | Observe the specified production interval, settle/claim genuine accrued emission and reconcile all funds. | Self-contained final report; all four requested outcomes satisfied with no open cap/custody exceptions. |

Phases form a dependency graph, not a best-effort list. The final activation boundary may need a new snapshot and plan revision after lengthy setup; revisions authenticate the prior plan and completed receipts, preserve original immutable identities and lifetime caps, and explicitly authorize changed future actions. They do not rewrite the prior plan, retroactively approve execution, or reset spend.

Persist a write-ahead, hash-linked action journal with states such as `planned`, `intent-recorded`, `signed`, `submitted`, `included`, `finalized`, `postcondition-verified`, `failed` and `canceled`. Append and fsync the exact intent and signed payload before broadcast, with mode-0600 protection for recoverable signed bytes. Public evidence contains no secrets. Journal records include parent plan/action hashes, full origin/domain, payload hash, nonce, value/fees, transaction hash, actual block/receipt/event positions and the authenticated postcondition snapshot.

Use atomic manifest/config writes and one exclusive run owner. Native account and EVM/Safe nonce ownership must be explicit, including whether two representations share an underlying account. Serialize one-shot setup dependencies. Pipeline independent operations only after adapter-specific evidence shows nonce, finality and spend recovery remain correct. A timeout, canceled watch or lost RPC response is not proof that a transaction failed; search for the exact signed transaction before deciding to rebroadcast or replace it.

Native success requires finalized inclusion and successful dispatch at the correct extrinsic index. EVM success requires a canonical receipt and the corresponding native finality mapping, plus exact event/getter/code readback. Decode events with that block's authenticated metadata. Where batched calls can partially succeed, record every child result; prefer an actually atomic batch when the desired action requires all-or-nothing behavior. Never infer child success from the outer batch alone.

Recovery rules:

- For CREATE, find the original nonce transaction and compare exact address, code and immutable parameters; do not redeploy because a local marker is missing.
- For registration, compare hotkey ownership and generation, not only UID presence. A foreign occupant is a conflict, not an idempotent success.
- For one-shot links and initialization, an exact existing value is reusable only with authentic receipt/precondition history. A conflicting initialized value stops recovery.
- For stake, deposits and capture, reconcile actual deltas and retained-source balances before any retry. Never repeat an economic action because its terminal response was lost.
- For local rendered configuration, bind format, resolved route, paths and authority/capacity inputs in the action intent. An approved new local render can converge stopped services; it cannot pretend an old receipt already proves new bytes.
- For superseded actions, authenticate the historical postcondition at its source and the specific finalized successor that authorizes current state. Do not demand obsolete live equality after an approved successor, and do not globally waive current checks.
- On cancellation, stop new signing, join submitted transaction owners and service children, persist the actual terminal state, and report `CANCELED`, not `PASS`. An incomplete deployment or native inclusion remains recoverable work.

There is no automatic rollback of a finalized UID removal, registration burn or immutable deployment. Recovery uses a newly reviewed bounded forward action where supported. Old custody contracts and claim artifacts remain served until their obligations have actually ended. An emergency service stop does not imply that native emission stopped or that the vault may stop honoring claims.

## Continuous monitoring and repair

The [stopped-validator repair increment](VALIDATOR-REPAIR.md) adds a concrete
`repair-validator claim|resume|status` path under an independently signed,
expiring fixed-unit/release/host/generation envelope. It can consume one durable
start only after the approved prior generation is stopped and its descendant
cgroup is empty, then retains the acknowledged invocation and exact-source
progress postcondition. An unacknowledged consumed start is explicitly uncertain
and cannot retry automatically. [Offline qualification](evidence/validator-repair-qualification-20260930.md)
is sealed for exact source `af570cdc`: 52 positive normal/race executions passed;
twelve normal and seven selected race controls were causal. Admission includes
the genuine cgroup-v2 filesystem and post-sync authority/sample-age rechecks.
This increment installed no unit and issued no live start. Production active-hang
rehearsal, root/operator services, initial activation, independent RPC, delivered alerts
and monetary repair remain open.
The host deployment owner must exclude concurrent privileged service or file
changes; the local journal lock does not provide that exclusion by itself.

Mainnet operation needs three separate owners: an independent read-only monitor,
service supervisors, and a bounded repair controller. The monitor observes and
reports; supervisors recover an approved process generation; the controller
executes only already authorized actions. Implement and rehearse this separation
under MG-07/PH-28 before production activation. The current `monitor` command
supplies identity/finality observations and an optional read-only
[`--services` consumer](SERVICE-MONITOR.md) for independently configured
validator roles.

The [steering responsiveness increment](SERVICE-MONITOR.md#steering-responsiveness)
adds explicit per-role loop-outcome budgets and persistent critical incidents
for an observed steering loop that stops returning while its publisher remains
fresh. A returned read/reveal/epoch wait remains responsive; startup without a
baseline stays unknown. Checkpoint v4 retains the exact episode through restart,
source loss and policy edits, and requires a later actual outcome for recovery.
Its [source qualification](evidence/steering-liveness-qualification-20261001.md)
includes command/producer boundaries, compatible v3 repair custody and independent
expected-host alert fixtures. No live service was changed. Approved production
budgets and delivered alerts remain open; the stopped-repair controller still
refuses active generations.

The separate [active steering-hang repair](ACTIVE-VALIDATOR-REPAIR.md) adds an
independently signed exact incident/role/release/host/generation capability.
It retains one stop and one start, a finite descendant-cgroup join, permanent
generation custody across alternate envelopes, and the complete original
checkpoint. Current recovered/unknown steering or policy/identity drift refuses
stop, including after durable reservation. Unacknowledged starts remain manual;
completion requires an actual new steering outcome. Its
[source receipt](evidence/active-validator-repair-qualification-20261001.md)
does not grant a production envelope or activation. Real-systemd stop behavior,
exclusive host/signer custody, anti-rollback policy and on-call rehearsal remain
P0 gates. No live service was changed.

The [operator journal monitor increment](OPERATOR-MONITOR.md) has an
[offline-qualified production reader/consumer](evidence/operator-monitor-qualification-20260930.md):
62 Go root executions passed normal/race, four alert fixtures passed, and nine
normal plus five selected race controls were causal.
It observes actual read-only PostgreSQL transaction/attempt and settlement-mirror
projections through the existing monitor owners, retaining domain incidents
across outage/restart. Fresh DB access and empty pending counts do not establish
chain success. Independent RPC, provider/client-key readiness, full liabilities,
root-validator progress and protocol deadlines remain unknown. The separate
stopped-validator capability requires its own independently approved envelope;
monitor observations alone grant no repair authority.
Deployment, dedicated read-only credentials, query-load qualification and alert
delivery remain open MG-07/PH-28 gates.

The [read incident continuity increment](READ-INCIDENTS.md) preserves stable
per-role outage IDs, first/latest failures, successful-read recovery evidence and
recurrence through checkpoint restart. Read recovery does not establish service
health or grant repair/spend authority. The bounded summaries need independently
retained events for complete incident timelines. Normal/race qualification,
covering all 82 affected roots and six causal controls in both modes, is recorded
in the [qualified read incident receipt](evidence/read-incident-continuity-qualification-20260929.md)
for integrated source `1bb311fc`. Compatible checkpoint rollout, deployment and
alert delivery remain pending.

The standard validator now has a qualified optional
[`--progress-file` producer](SERVICE-PROGRESS.md). It reports bounded intent and
settlement observations without acquiring another protocol reader or signer;
publication failures do not cancel validation. Its separate heartbeat,
successful-observation, durable-progress and publication-acknowledgment times
must remain distinct in dashboards. The consumer now retains these facts across
outages and restart, with separate per-role checkpoints and atomic metrics.
[Qualification](evidence/service-monitor-qualification-20260928.md) passed 48
selected normal/race roots, five controls in both modes and service/chain alert
rules. Delivered alerts, other production domains, supervision and the repair
controller remain deployment or implementation work. A fresh file alone is not
proof of healthy validation. The optional [native deadline observer](NATIVE-DEADLINES.md)
adds explicit completion margins, submission-window forecasts and retained
reported epoch misses through the actual per-role worker. It grants no receipt,
signature-expiry or success authority; late applied reports cannot clear a
historical incident. Its [component qualification](evidence/native-deadline-qualification-20260928.md)
passes 35 selected roots and five causal controls in both normal/race modes,
plus native-deadline and inherited service alert fixtures. Production margins,
deployed alerts and authoritative incident resolution remain pending. The integrated
[bounded output correction](evidence/bounded-diagnostic-output-qualification-20260928.md)
now isolates validator startup, steering and runtime diagnostics, plus these
chain/service workers, from blocked log destinations. Its 76 affected roots have passing scoped normal
and race coverage, with six regression controls in both modes, vet and three
offline alert-rule suites. Each role retains finite output capacity and actual
delivery/loss counters. Deploy the progress consumer before the producer's
optional diagnostic extension, and retain independent missing/stale-file alerts.
The [root-service/root-monitor output follow-up](evidence/root-output-qualification-20260928.md)
is now integrated and component qualified: 41 roots have passing normal/race
coverage, six causal families reproduce their intended failures, and vet plus
seven offline alert rules pass. Root-monitor now emits compact event v2 and
optional independent metrics; deploy compatible log consumers first. Finite
preview v1 still supplies the full census. The actual trail worker and its
proof-warning path now have their separate
[bounded-output qualification](evidence/trail-diagnostic-output-qualification-20260928.md):
all 17 affected roots have passing normal/race coverage and five causal controls
reproduce the intended failures. Physical full-pipe checks cover real proof
progress and joined cancellation. Required ledger/proof failures remain hard.
The [miner output and shutdown correction](evidence/miner-diagnostic-output-qualification-20260928.md)
is also integrated: 18 affected miner roots, the actual Warp status reader and
four causal controls passed their normal/race checks. Its optional diagnostic
status extension reports local delivery separately from process liveness;
consumer compatibility must precede rollout. The separate
[diagnostic cause isolation correction](evidence/diagnostic-cause-isolation-qualification-20260928.md)
is integrated and passes 20 affected roots and five causal controls in both
modes. It never invokes arbitrary error methods for optional labels and leaves
opaque causes unknown. SDK/internal logging
and final registration composition remain separate scopes. Local output acknowledgment
does not establish remote ingestion or alert delivery.

### Independent observations and existing telemetry

Run the monitor separately from bootstrap and validator/taskworker lifecycles,
without signing keys, database write credentials or authority to stop those
services. Give it its own bounded read budget, durable finalized-block cursor,
incident store and health signal. Replay from the last verified checkpoint after
restart; never replace missing observations with an assumed healthy interval.
Subscriptions wake readers but do not establish finality. Pin events, storage,
runtime interpretation and native/EVM mapping to the same authenticated block.

Compare the approved owned mainnet RPC against a separately operated,
independently authorized canonical source at the **same finalized block hash**.
Different latest heights alone are lag, not a reorganization. Validate both
identities, their available archive scope and the same transaction/event/storage
facts; preserve disagreements. The second source is read-only and cannot become
a submission fallback. Until it is provisioned, expose `independent_rpc=false`;
Snow and the LAN alias of one backend provide no independent confirmation.

Export SN metrics and structured incident events into the existing xops
Grafana/Mimir/Loki stack, using its Prometheus-compatible exporter and host
Fluent Bit paths. Reuse [deployment infrastructure](../../xops/main/ansible/playbook-dbs.yml)
and [telemetry isolation requirements](../../xops/VULNSCAN2.md): restricted
telemetry identity, scoped credentials, bounded journald retention and durable
log cursors. Do not mount signer material, a full vault or Docker control into
the observer or dashboard. Keep bounded metric labels to deployment, component,
role and error class; put transaction hashes, client-level detail and exact
evidence references in the incident store. Monitor telemetry delivery itself
through a separately hosted dead-man alert and named escalation route.

Every dashboard distinguishes unavailable, pending, healthy and failed facts:

| Domain | Evidence and progress to observe |
| --- | --- |
| Chain and authority | Genesis/EVM domain, approved runtime capabilities and code/metadata, finalized age/height, native/EVM mapping, node agreement, endpoint/config/release drift and archive availability. |
| Validators | Both UR validators' hotkey ownership, permits, non-self eligibility, fresh proof domains through each operator, native source/EMA continuity, durable intents and finalized revealed/applied weight rows. Root seat, stake/retention margin, child delegation and basket are a separate role. |
| Operators and providers | Current policy and evidence activations, migration version, processed client-key registrations and peer pins, ready provider count, fresh signed usage and bounded queues. HTTP 200 and process liveness do not establish registration or proof success. |
| Settlement and treasury | Exact source epoch/root/artifact, immutable usage snapshots and uncredited debt; required/observed deposits under the selected policy; pool capture, carry, commitments, finalization, claims and outstanding liabilities. Reconcile native units, collateral/principal and fee/lifetime allowances. Measure the 10% native target and approved tolerance independently of claimed payouts. |
| Transactions and deadlines | Every signed attempt, nonce owner, uncertain send, replacement/cancellation, canonical receipt and postcondition; blocks remaining to policy, commit, reveal, renewal, claim and evidence-retention deadlines. |
| Services and resources | Process generation and restart count, last useful checkpoint, database/artifact health, CPU/memory, RPC concurrency, queue age, log byte lag and disk bytes/inodes. No healthy status from a stale lock file. |

### Alert taxonomy and initial SLOs

The following are proposed starting targets. Freeze them in the operations
manifest after a representative load/recovery rehearsal and before activation.
They are not claims of measured availability. Protocol deadlines remain exact
block boundaries; human response targets cannot extend them. Use a lightweight
health loop while expensive replay proceeds under a separate finite budget.

| Alert class | Starting detection/SLO target | Response |
| --- | --- | --- |
| Integrity, authority or accounting conflict | Emit immediately on an authenticated wrong-chain/domain, finalized-hash conflict, invalid signature, custody/conservation mismatch or unauthorized spend. No averaging or transient-error allowance. | Critical page; suspend dependent new signing through its owner and preserve evidence. Continue independent observation and valid claim service where safe. Primary acknowledges within 5 minutes; backup escalation after 5 minutes without acknowledgement. |
| Monitor or alert path absent | Target a health/progress event at least every 30 seconds; warn after 90 seconds, page after 2 minutes without one. Test alert delivery before activation and after routing changes. | Independent dead-man page; restore observation first. Missing monitor samples remain a gap, not a healthy interval. |
| RPC/read availability or stalled finality | Record every error as unavailable; warn after 2 minutes of persistent read failure, page after 5. Warn at 3 minutes without finalized advance and page at 5, after calibrating to admitted chain cadence. | Bounded retries/reconnect on approved routes, inspect chain-wide versus node-local failure, and block new actions lacking required fresh evidence. Never compare an unread default value with an approved one. |
| Deadline or readiness risk | Recompute at least every 30 seconds and on each new finalized block. Warn when remaining blocks fall below the greater of 20% of the window and twice measured p95 completion/finality cost. Page when the admitted completion margin is no longer available, or a required role remains unavailable for 2 minutes. | Resume the exact pending action if authorized; otherwise escalate a concrete forward plan. Record a missed boundary as missed. Both UR validators and every required operator/domain must remain independently visible. |
| Settlement and reward deviation | Evaluate every due finalized event/epoch and native emission interval; immediate critical alert for conservation or authority failure, deadline alert for missing work, explicit alert for a 10% result outside approved `Q(k)`. | Trace source usage through liabilities and receipts. Do not fabricate usage, increase a governed deposit to a native minimum, or count a late root as timely. |
| Resource exhaustion or replay backlog | Warn below 20% free bytes/inodes or when forecast capacity is under 24 hours; page below 10% or a shorter time than safe intervention. Alert if log/queue lag exceeds its approved window or foreground work loses its completion margin. | Reduce bounded background admission or restore capacity within policy; never delete signed evidence or increase spend/capacity authority silently. |
| Recovered incident / recurring degradation | Retain first failure, retry count, recovery evidence and recurrence by stable incident ID. Noncritical pages acknowledged within 15 minutes; unresolved incidents carry an owner and next action. | Review open incidents daily and recurrence/capacity/runtime-change trends weekly. Create a scoped fix with causal regression and affected-path qualification. Recovery does not erase the failure. |

Select recovery-time objectives from real replay and protocol windows before
launch. Durable intent and signed-transaction recovery has **zero tolerated loss
of acknowledged records**; a retry may repeat observation but may not repeat an
economic effect. Local crash recovery requires fsync and restore evidence. A
zero-loss host-failure objective additionally requires independent durable
replication before acknowledging/broadcasting signed work; periodic backups
alone cannot provide it. Bind that recovery design before enabling automated
spend. Publish actual recovery times and observation gaps alongside the target
after each exercise or incident.

### Repair authority and durable execution

Service stop/drain must use retained process ownership even when replacement
configuration is invalid or RPC is unavailable. Verify the exact process
generation and join its children; retain journals, uncertain signed work and
volumes. Campaign closure records which services remain necessary and proves
the others stopped. The [testnet shutdown observation](evidence/closed-testnet-service-stop-20260928.md)
shows why publishing a terminal report alone is insufficient. Qualify this
path before production service activation.

Provide a standing signed repair envelope for routine operations the operator
chooses to automate. It binds chain/deployment, immutable release, allowed
action kinds and exact targets, signers/nonce domains, prerequisites, expiry,
maximum attempts and action counts, per-action value/gas/fees, total lifetime
debits, price/minimum-output limits and permitted postconditions. Automation can
continue within that envelope without asking again for each identical retry.
Changing its scope or exceeding a bound requires a new exact reviewed plan.
Default monetary limits are zero until supplied; alert severity never grants
transaction authority.

| Action | Automation boundary |
| --- | --- |
| Reconnect/retry reads, replay authenticated immutable evidence | Allowed within the monitor/worker's finite budget and unchanged authority. Preserve successful checkpoints and typed failures. |
| Restart an approved service or resume a stopped worker | Allowed by its service manifest only after joining the old process/children and retaining signer, volume and configuration identity. Cap restarts; escalate exhaustion. |
| Reconcile a previously signed transaction | Read receipts, dispatch, postconditions and nonce state automatically. Rebroadcast the exact bytes or make a replacement only when the owning approved action explicitly permits it and its bounds still hold. |
| Scheduled renewals, routine claims or approved funding repairs | Automatic only under their own signed targets, amount/count/price/deadline caps and lifetime ledger. A schedule alone is not spending permission. |
| Policy/rate/source/native-history changes, new registration or stake, destructive reset, custody/contracts, runtime admission, release/schema or endpoint changes | Operator-gated exact plan with the actual required coldkey/Safe/governance authority. No self-approval, permission widening, backdated success, historical signature rewrite or guessed SQL credit. |

Use one durable action/nonce owner shared with the production submitter. Append
and fsync an incident-bound intent before signing, then exact signed bytes before
broadcast. Journal `planned → intent-recorded → signed → submitted → finalized
→ postcondition-verified` with explicit failed, canceled and unresolved branches.
Store replacement/cancellation attempts separately; retain all paid fees and
outstanding liabilities across releases and retries. Before any repeat, locate
the original exact hash and reconcile canonical inclusion, dispatch, finality,
nonce and economic postcondition. A timeout is an unknown outcome. A new nonce
or a local database status is not evidence that the old action failed.

The [configured native HTTP read adapter](evidence/native-http-read-causes-20260928.md)
preserves status and physical body failures within the existing finite read
budget. Production consumers can distinguish that typed unavailability from
complete malformed evidence and mixed integrity errors; writes retain their
separate original-byte reconciliation policy. The 30-root component normal/race
qualification passed. The [production integration](evidence/production-native-http-integration-20260928.md)
also passed its 23-root normal/race scope and three regression-control families.
It admits only pure connection failures from HTTP body
close after releasing the response and discarding idle connections; local file
close, cancellation and mixed integrity causes remain hard. Its finite wire cap
includes the existing 16 MiB event value as hex plus 64 KiB of JSON framing.

The [operator receipt-census correction](evidence/operator-recovery-census-20260927.md)
preserves this boundary in the production account reconciler: if a retained
candidate's receipt cannot be read and no other candidate is canonical, the
intent remains unresolved. An advanced nonce cannot erase that unknown outcome,
and elapsed replacement time cannot turn the failed read into new signing
authority. MG-03/PF-03 still require live recovery composition, canonical
historical-status and receipt reconciliation, and actual fee accounting.
The [status-independent census source](evidence/operator-signature-census-qualification-20260928.md)
now reads selected databases and retained RLP stores without status filtering,
preserves original/replacement/cancellation provenance, and supports private
byte-preserving archive restoration. Its normal/race qualification covers both
new roots and adjacent controller/model recovery. Production receipt/finality
joins, actual fees, distributed custody ownership and live restart remain open.
The [conditional offline receipt/fee join](evidence/operator-receipt-fee-qualification-20260928.md)
now reports missing and conflicting candidates and counts observed gas once
per resolved nonce; its recovery and adjacent controller/model roots passed
normal and race modes. A separately authenticated native-to-EVM mapping,
owned-node receipt capability and exact runtime fee evidence are still required
before it can establish canonical production fees or authorize recovery.

The [qualified receipt commitment verifier](evidence/operator-receipt-commitments-qualification-20260929.md)
is now integrated at server `fbe0c039`. Its offline `verify-receipts` command
authenticates exact archived signed bytes and receipt status at the same trie
index, derives gas from the committed receipt and its predecessor, and verifies
consecutive raw Frontier headers through the supplied EVM boundary. All 52
recovery/CLI roots pass normal and race, including 14 new roots and three
private-database roots; four causal controls pass. This closes source-level
verification of those commitments only. Actual fees remain null: the raw Frontier
header does not commit the RPC-rendered base fee, and runtime/native debits
remain unproven. Independent native finality/mapping, boundary account nonce
proofs, owned-node collector capability, service adoption, release
composition and live custody/restart qualification remain open MG-03/PF-03 work.

The [qualified bounded collector](evidence/operator-receipt-collector-qualification-20260929.md)
is integrated at server `b7c8c743`. Its `collect-receipts` command reads every
archived signed candidate, authenticates complete raw block/receipt vectors,
builds the proofs and replays the verifier before publishing one private,
create-only evidence file. `verify-collection` replays it entirely offline.
Transient reads retain exact selectors within configurable 60–900 second retry
windows (default 300), a total deadline and shared request/byte budgets.
All 71 affected recovery/CLI roots pass normal and race, including 19 new roots
and three private-database roots; five causal controls pass. The original failed
publication-mode assertion and its test/documentation correction remain retained.
This qualifies collection source only: node capability and the supplied native/
EVM mapping remain explicitly unapproved, actual fees remain null, and no
finality, canonical-accounting or spending authority is created.

The [qualified native finality proof](evidence/operator-native-finality-proof-qualification-20260929.md)
is integrated at server `44636e5e`. Its offline `verify-finality` command checks
GRANDPA weighted certificates, native header ancestry and delayed scheduled
authority handoffs, then binds the exact native Frontier digest to the
collection's raw EVM header. All 89 affected recovery/CLI roots pass normal
and race, including 18 new roots and three private-database roots; five causal
controls pass. These proofs are relative to a separately pinned checkpoint.
No independent genesis/checkpoint approval or deployed-runtime provenance is
supplied, and all finality, canonical-accounting and spending authorization
flags remain false. Actual fees remain null: native denomination conversion,
debits and best-effort refunds require authenticated runtime evidence beyond
receipt gas or reported prices. Checkpoint admission, account nonce proofs,
service adoption, release composition and live
custody/restart remain open MG-03/PF-03 work.

The [qualified bounded native finality capture](evidence/operator-native-finality-capture-qualification-20260929.md)
is integrated at server `5ff7bf02`. `capture-finality` retains exact native
headers, stored certificates and durable request/byte reservations, then replays
the existing offline verifier before private proof publication. Proof v2 accepts
a bounded certified descendant while preserving the original collection boundary
and its exact Frontier mapping. Completed restart replays offline; partial
evidence and spent budgets survive failed reads. All 112 affected roots pass
normal/race, including 23 new roots and three private-database roots; seven causal
controls discriminate in both modes, and vet/source/module fences pass. Archive
capability and independent checkpoint/runtime authority remain unapproved;
native account and debit/refund proofs, composed release and live custody remain
open. Actual fees remain null, and no accounting or spending authority is added.

The [qualified bounded native StorageProof verifier](evidence/operator-native-storage-proof-qualification-20260929.md)
is integrated at server `6201504e`. Its offline library replays receipt/finality
proofs before checking raw storage bytes at the original collection boundary; a
later certificate cannot move that state root. All 16 focused and six adjacent
roots pass normal/race with package exit zero, and all 21 causal controls
discriminate in both modes. The independent pinned-SDK oracle supplies 18 exact
vectors. Both earlier fixture/control anomalies remain separately preserved.
Its original API covers the collection boundary; the qualified historical
interface below supplies selected receipt/parent reads. Runtime decoding, proof
capture, account/fee authority and live custody remain open; actual fees stay null.
This scoped
qualification creates no owner-window, global-custody or spending authority.

The [pinned-runtime fee dependency review](https://github.com/urnetwork/server/blob/cfcbfcbaa13b4f4d298acfeca761a7252c18ddee/strecovery/ACTUAL-FEE-DEPENDENCIES.md)
is integrated at server `cfcbfcba` as documentation only. Generic balance
events share an extrinsic phase with native precompile effects; block balance
deltas also include non-fee effects, and a failed best-effort refund lacks a
fee-specific record. Neither proves general operator-call gas debits. Actual
fees remain null. The next dependency is bounded capture of historical raw
proofs and execution-runtime/source/metadata admission, followed by
runtime-qualified debit/refund
attribution through historical execution replay or an admitted fee-specific
runtime event. A future event cannot reconstruct historical fees. Independent
genesis/checkpoint and source-to-deployed-runtime admission remain separate;
an approved live checkpoint is not required to implement or qualify offline
proof machinery. This source review supplies no new behavioral qualification,
accounting or spending authority and does not close MG-03/PF-03.

The [qualified historical receipt/native fee-context source](evidence/operator-native-fee-context-qualification-20260929.md)
at server `41527380` passes all ten new and 122 affected roots in normal/race,
including three disposable-database roots; six causal controls discriminate in
both modes. It is integrated after storage as server `1bccc3cd`; a separate
composed smoke passes 30 of 138 available roots per normal/race mode. It derives
each receipt block's exact native commitment candidates and linked parent root
while keeping absent/ambiguous mappings unresolved.
Its source-profile account mapping supplies no runtime or payer admission, and
all actual fee amounts remain null. The original native StorageProof interface
covers collection-boundary reads; the separately qualified historical interface
below joins selected child/parent proofs. Runtime/debit attribution remains
separate. These prerequisite slices do not close MG-03/PF-03.

The [qualified historical native StorageProof API](evidence/operator-historical-native-storage-qualification-20260929.md)
is integrated at server `80c0e1b7`. It freshly replays receipt/native fee contexts
and derives one exact
receipt block's linked parent execution root or child post-state root internally.
A valid checkpoint child remains provable while its missing parent and incomplete
fee context stay explicit; parent fallback, ambiguous mappings and descendant
substitution are refused. All 20 new plus 30 adjacent roots pass normal/race with
package exit zero; all 21 causal controls discriminate in both modes. This is
50 selected roots per mode, not whole-package coverage. Runtime/payer/fee
interpretation, live
authority and custody remain absent, and actual fees stay null.

The independent monitor confirms the repair's postcondition at finalized state.
Only then close the incident, retaining its history and action receipts. A local
repair success with missing chain evidence stays pending. Recovery cannot erase
failed acceptance assertions or turn an observation gap into a completed epoch.

### On-call, incident evidence and rollout

Before activation, name a primary and backup operator, establish the alert route
and access to read-only diagnostics and the appropriate signing process, and
rehearse this runbook:

1. Acknowledge the incident and pin its deployment/release, actual process owners,
   last good checkpoint and current native/EVM blocks. Distinguish an unavailable
   read from an authenticated mismatch before deciding containment.
2. Stop only dependent new signing or unsafe work through its existing owner.
   Keep monitor, immutable history and valid earned claims available. Preserve
   signed and in-flight transactions for reconciliation; stopping a process does
   not stop native emission or remove custody obligations.
3. Seal an incident bundle: exact config/plan/runtime hashes, raw RPC responses
   and receipts, relevant signed artifacts, log byte ranges/cursors, queue state,
   liabilities, alert timeline and attempted repairs. Restrict signed transaction
   bytes and redact credentials; hash public evidence separately.
4. Reproduce the actual failure and inspect adjacent callers. Use the standing
   repair only if every precondition still holds; otherwise prepare the concrete
   bounded forward plan or code fix, qualify it, and obtain its required authority.
5. Reconcile transactions and databases before retry or service replacement.
   Verify the result independently, observe sustained proof/settlement progress,
   and retain unresolved consequences as open incidents. Record detection,
   response and recovery times and the follow-up owner.

Roll out immutable images with the qualified source/dependency/config manifest.
Run read-only shadow checks, then a canary with no duplicate signer and enough
capacity to preserve the required validator/operator quorum. Stage additive
database migrations before compatible consumers; specifically rehearse populated
client-key policy rollover and immutable usage writer/reader compatibility.
Advance only after current-domain readiness, resource bounds, fresh proofs and
finalized chain postconditions are observed. Thresholds and canary duration are
approved in the rollout manifest before execution.

Keep the previous image and an explicit state-format compatibility matrix.
Rollback is allowed only if the old reader/writer can safely interpret the
current schema, policies and signed state. An irreversible migration, finalized
registration, immutable deployment or economic transfer requires forward
recovery; restoring an old filesystem cannot undo it. Always reconcile in-flight
transactions and join old owners before changing images. Restore tests must
prove journals, keys and artifacts remain usable with no duplicate spend. End a
rollout with an independent state comparison and updated incident/capacity
records, not just a green process list.

## Integration with this repository

Reuse importable production packages: [crv4](../crv4) for authenticated runtime/native reads and transaction evidence; [stabi](../stabi) for the release ABI surface; [protocol](../protocol) for policy and domain encoding; [validator](../validator) for UR validation and evidence; and existing cryptographic/address/Merkle primitives where their units and domains match.

Do not import `sim-testnet` as a production dependency: it is a `package main` campaign with fixture, finance-repair, historical migration and adversarial machinery. Extract only a required, qualified generic facility into a neutral internal package when implementation begins. Keep mainnet capability adapters explicit, and keep the new root-validator implementation separate from UR scoring. The old [stctl configuration](../stctl/config.go) identifies itself as legacy pre-1.0 and rejects the current deployment domain; it is not the release-1.0 mainnet control plane.

Suggested future package boundary:

```text
mainnet/main.go                  argument parsing and command dispatch
internal/mainnetbootstrap/       config, snapshots, capabilities, plans, executor, reports
internal/rootvalidator/          root membership/basket observer and approved action loop
internal/chainactions/           only extracted, proven transaction/journal primitives
validator/                      existing production UR validator
```

Use interfaces for `FinalizedReader`, `NativeSigner`, `EvmSigner`, `SafeSigner`, `Submitter`, `Journal` and `ServiceSupervisor`. Follow the [UR Go style guide](../../connect/CODESTYLE.md): owned Go identifiers use `Evm`, `Rpc`, `Uid` and `Id` casing, `self` receivers and the prescribed field naming. Preserve externally required, generated and wire-format names. Packages may import a parent or peer, never their own child; the proposed internal packages are peers of the command package, not a bypass for that rule. A signer returns an identity-bound signed payload; it does not decide policy or fall back to another account. Mainnet configuration decoding must finish before any durable worker or RPC connection starts. Keep testnet provisional admission flags, deterministic fixture keys, fabricated identities, accelerated epochs, simulator routes and faucet/funding assumptions outside this interface.

## Acceptance evidence and implementation qualification

The closed testnet campaign requested 33 roles, 1,000 providers, 202 candidates,
200 head positions and two UR validators, five accelerated epochs and a later
production-policy observation. Those requirements were not completed by R48:
its original result is failed with zero complete acceptance epochs, and later
retained recovery remains non-accepting. Preserve that report and its explicit
exceptions as inputs. Mainnet is not blocked on reopening that campaign; it is
blocked on the [production gates](PRELAUNCH-FIXES.md#production-gates-in-execution-order)
and the actual capabilities, accounting and operating evidence required here.

Qualify the composed production release on a controlled integration deployment
with the relevant failure/recovery cases. Reuse historical tests only with exact
source/dependency and requirement mapping. A simulator-only patch, canceled
setup, signed plan, read-only monitor or partially observed epoch cannot replace
the required evidence. Keep one manifest of implemented, qualified, deployed
and operationally observed states, with every remaining exception explicit.
Mainnet has its own release identity, budgets and 50,400-block policy; testnet
provisional authority and accelerated timing do not carry over.

Current component evidence includes the
[complete server model census and qualified fixture corrections](evidence/server-model-completion-20260928.md)
and [shared receipt/miner recovery qualification](evidence/receipt-recovery-qualification-20260928.md).
The [September 29 server source composition](evidence/operator-mg03-r48-composition-20260929.md)
retains those historical approvals and records the new scoped normal/race
qualification of its full merge and test successor. It does not claim a new
full-model run or a composed production release.
The [registration transaction qualification](evidence/registration-server-model-qualification-20260928.md)
passes all 29 affected roots normally and under race detection. Its full model
body completed all 1,125 roots with 1,118 passes, seven explicit fixture-input
skips and no failures. Retain the original successful package exit separately
from its failed legacy-subtest metadata check; do not claim zero-skip coverage.
The [combined SN registration and diagnostics check](evidence/registration-diagnostics-composed-qualification-20260928.md)
passes all seven selected consumer roots normally and under race detection on
the sealed source graph; all 18 stages and independent after-fences passed.
The later [integrated mainnet source check](evidence/final-composed-source-qualification-20260928.md)
combines that graph with the native deadline observer and offline bootstrap:
379/379 full normal roots and 122/122 selected race roots passed with all 158
declared descendant executions, zero skips, and unchanged source/module seals.
It does not replace release-artifact, deployment, custody or live acceptance
gates.
The subsequent [two-UR-config admission qualification](evidence/ur-bootstrap-admission-qualification-20260928.md)
uses the same physical dependency refs with the v2 bootstrap source. Its full
normal mainnet census and affected mainnet/validator normal/race suites pass
all 588 root and 183 descendant executions. Six original control captures
retain their twelve intended failures; strict maintained resume accepts all
24 stages without rerunning bodies. Earlier metadata and mode refusals remain
recorded. This does not reuse validator package qualification wholesale or
supply live identity, producer eligibility, signing or service activation.
The frozen model invocation ended with three now-corrected fixture failures and
seven optional-configuration skips; it is not recorded as a passing full
invocation. Preserve these results when assembling the final source composition
and qualify its affected changes without restarting unchanged completed scopes.

The future mainnet acceptance bundle contains:

| Outcome | Evidence required |
| --- | --- |
| Reset | Signed target/preserve census, exact finalized removal mechanism, before/after hotkey generations and UID mapping, unchanged required custody/validator ownership, residual stake/lock report. |
| Contracts | Source/toolchain/artifact hashes, predicted/actual addresses and nonces, creation receipts, runtime bytecode and immutable getters, Safe authority, one-shot links and evidence anchor, preserved custody invariants. |
| 10% miner rewards | Explicit denominator and activation boundary; exact native interval accounting; finalized incentive outcomes including collateral; direct-head and tail entitlement reconciliation; verified 90% owner-recycle outcome and mode, with no reserve credit; runtime-derived quantization tolerance and actual target result; proof of a stronger hard cap only if that assurance was selected. |
| Root validator | Real netuid-0 membership and owner mapping, bounded admission receipt, stake/retention observation, child policy, actual basket strategy, live owned service and runtime identity. |
| UR validator | Real UR eligibility and live applied/revealed CRv4 rows, authenticated evidence/usage, service signer, second UR validator and operator safety minima. |
| Financial/finality closure | Actual spend including failures, remaining allowance, all transaction owners joined, native/EVM finality mapping, open liabilities and future operations clearly reported. |

Measure activation across at least three complete native emission intervals, and observe a full mainnet UR settlement/claim cycle before declaring settlement acceptance. The 50,400-block cycle cannot be replaced by accelerated testnet timing. Bootstrap may report `DEPLOYED_AWAITING_SETTLEMENT` while that observation is pending; it must not call the whole requested program accepted early.

Implementation qualification is owned by **Sol medium (`gpt-6-sol`, effort `medium`) for test and gate execution; Astra max (`gpt-6-astra`, effort `max`) for all implementation, debugging and fixes**, following the [Go style guide's bug-fix and testing policy](../../connect/CODESTYLE.md). This assignment was confirmed on 2026-09-28; retain earlier Terra evidence under its original attribution and do not rerun completed work because the executing model changed. Every actual fix needs a regression that deterministically reproduces the pre-fix failure and verifies corrected behavior at the observable failing layer. Use explicit barriers, hooks or state transitions for ordering; sleeps, negative timeouts, queue polling and scheduler luck are not the primary proof. Review surrounding code, sibling call sites and similar patterns before declaring the root cause fixed, and record any affected adjacent paths.

Keep regression data visibly synthetic: generated test-only identities, `.example` hosts and reserved documentation addresses; sanitize captures before turning them into fixtures and retain necessary raw evidence outside source. Tests are top-level `func TestXxx(t *testing.T)` declarations. Use separate top-level tests or plain table loops for ordinary cases; use `t.Run` only when isolating and asserting a deliberately failing subtest is itself the subject. This document does not execute or request execution of new tests. When implementation is authorized, cover meaningful boundaries and recovery:

1. Command-level read-only preview cannot acquire a signer or submit, including malformed/duplicate config, wrong genesis/chain, testnet identity, route substitution, stale snapshots and runtime upgrades. Also cover a separately attested mainnet identity subsequently hosted at a previously testnet address.
2. Real pinned metadata/call encoding proves owner versus Root origins and both root/UR call domains. Negative cases include unsupported trim, wrong signer, root `register_limit`, missing price protection and native/EVM rollback failure for any new registration wrapper.
3. Reset census and actual pruning/trim replay cover dual-role neurons, validator-without-permit preservation, custody identities, immunity boundary, tie ordering, concurrent registration, minimum capacity, UID renumbering and surviving collateral. A smaller UID count alone cannot pass.
4. Full emission-path tests demonstrate why scaling weights or theta does not impose a cap; then prove the selected 10% target and runtime-derived tolerance through quantization, consensus, independent-validator rows, no-incentive fallback, multiple mechanisms, denominator-boundary accrual, locked rewards and cumulative dust. Distinguish actual target observation from any selected hard-cap guarantee. Cover burn and recycle accounting separately and refuse an unsupported reserve transfer.
5. Deployment tests use real creation payloads and contract execution for nonce `n+3` escrow registration, atomic proxy initialization, mapped coldkeys, Safe checks, refund/fee bounds, one-shot conflicts and evidence anchoring. A changed custody contract requires its own conservation/claim and adversarial review.
6. Crash/restart tests inject failure before and after signing, submission, finality and fsync; prove no duplicate burn/deposit/stake and no lost finalized success. Cover nonce collision, reorg before finality, failed batch children, canceled owners and authenticated successor history.
7. Process tests prove both distinct validator roles, no silent endpoint fallback, single signer ownership, complete child joining, current UR permit/reveal checks, root custom/default strategy handling and no unbounded restaking/re-registration loop.

Run bounded normal tests on the frozen implementation, then appropriate race tests for shared state, journal ownership and supervisors. Retain exact source, binary, selector, package working directory and terminal evidence. Diagnose any actual failure on that capture before retry; preserve failed evidence and use the established confirmation protocol. Reuse unaffected qualification only with an explicit source/dependency mapping; changed custody/runtime economics require their relevant full tests and owned-node rehearsal. There are no mainnet tests against public RPC and no broadcast hidden in a test command.

Use the maintained [qualification owner](../scripts/qualification/main.go) for
new captures. A retained test binary must run from its authenticated package
directory, just as `go test` would; the module root is not equivalent. Verify
the binary's actual test list and complete terminal events, including explicit
package success. A wrapper's zero exit after interrupted execution is not a
pass. Preserve valid compiled artifacts and completed work when correcting a
runner, with the failed invocation and the correction recorded separately.
Compare the enumerated roots with the independently supplied expected set before
launching bodies. A nonempty subset is insufficient. Parse declared table schemas
explicitly; never discard a first row merely because another file had a header.

The [literal slash checker correction](evidence/qualification-slash-parent-qualification-20260928.md)
now preserves source-declared Go child names without inventing intermediate
test events. Its 31-root normal/race qualification and two causal controls
passed before it replayed retained Connect evidence. Both original 33-root,
32-descendant captures now match; no test body was repeated. A corrected
checker or declaration must retain the original failed invocation, body exit
and source fences instead of turning metadata repair into a complete rerun.

Bind qualification to the roles and platforms selected for this release.
Unselected services and architectures remain explicitly unqualified and cannot
be deployed from its approval; they do not require speculative build or test
work before the selected release can proceed. Reuse successful unchanged
scopes by their actual consumed source and dependency identities. A new binary
hash alone is not a reason to repeat all historical qualification.

Set the qualification environment explicitly before compiling: `GOWORK=off`,
`GOMAXPROCS=2`, `GOCACHE=/mnt/data/sn-testnet/gocache`, and a capture-specific
`TMPDIR` under `/mnt/data/sn-testnet/evidence`. Create the temporary directory
first. Record the actual Go executable, resolved tool paths and module graph;
the host's default cache symlink is not a physical tool path. A preflight
refusal before any test body executes is a runner failure, not a product test
failure. Correct that environment and retain the refused invocation without
restarting unrelated bodies already in progress.

## Open inputs before an executable mainnet plan

Use `https://archive.chain.opentensor.ai` as the interim mainnet RPC for
read-only discovery and unsigned plans. The [October 1 observation](evidence/public-archive-switch-20261001.md)
reports mainnet genesis/EVM ID 964 and runtime spec 470, but the strict native/EVM
mapping initially lacked a public raw-header method. The
[qualified public fallback](evidence/public-header-fallback-20261001.md) now
passes a [live read-only exact-hash finalized snapshot](evidence/public-finalized-snapshot-20261001.md)
on that archive. The [current unsigned preparation](evidence/public-unsigned-preparation-20261001.md)
rechecks one combined observation at block 9,186,298 and retains the exact
runtime470 hashes, source lock, release input and ten-action blocked plan for
SN `de0823ce` / server `0b8e758d`. Its separately declared network target is a
review input, with independent network/runtime authority still missing. Snow VPN
`172.28.208.185:9944` is a
future failover; it still returned HTTP 502 at the latest retained check.
Do not retarget signed action bytes. The
[earlier frozen offline composition](evidence/release-current-source-20261001.md)
passes its 17-binary, five-contract and 170-hash audit; eight fresh OCI image
readbacks pass as separate supplements for SN `2d53e6f2` / server `ecbf3aad`.
Those images do not establish provenance for the successor sources. No
deployment approval is implied. The later
[pre-Safe baseline](evidence/release-pre-safe-baseline-and-modes-20261001.md)
does establish local 17-binary/five-contract/eight-image coverage for SN
`1806b3b3` / server `0b8e758d`; it does not cover the later public Safe source.
The separate [Safe-source release](evidence/release-safe-source-20261001.md)
builds and verifies its own complete local composition at SN `095a2208` /
server `0b8e758d`, preserving the same remaining independent and production
approval gates.
Obtain
an independently approved mainnet genesis/runtime identity and complete
SN25 census. Qualify the production source/dependency release with
the retained R48/R46 lessons, then implement the bootstrap mutation paths and
separate root-validator service. Resolve the actual reset capability and implement the selected 90% owner-recycle
policy with the observed 10% native allocation and runtime tolerance, root custody and
registration protection, real identities and budgets. Install independent
monitoring, existing-stack alerts, bounded repair authority and the on-call
runbook before activation. Each unresolved item remains visible in the plan and
report; the closed testnet effort is not relabeled as a pass.

[subtensor-commit]: https://github.com/RaoFoundation/subtensor/commit/67dcf7f791dc495064c293f080a0702cb433e51e
[subtensor-admin]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs
[subtensor-storage]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/lib.rs#L1661
[subtensor-uids]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/uids.rs#L171
[subtensor-registration]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/registration.rs
[subtensor-root]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/root.rs#L88
[subtensor-weights]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/weights.rs#L875
[subtensor-dispatches]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/macros/dispatches.rs
[subtensor-coinbase]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/run_coinbase.rs
[subtensor-alpha-accounting]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/alpha.rs#L38
[subtensor-shares]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/subnet_emissions.rs#L354
[subtensor-alpha-precompile]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/precompiles/src/alpha.rs#L297
[subtensor-epoch]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/epoch/run_epoch.rs
[neuron-interface]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/precompiles/src/solidity/neuron.sol#L206
[root-reborn]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/docs/guides/root-reborn.mdx
[root-reborn-470]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/docs/guides/root-reborn.mdx
[root-removal-470]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/migrations/migrate_remove_root_weights.rs
[root-basket-470]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/staking/basket_trade.rs
[subtensor-root-470]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/coinbase/root.rs
[subtensor-dispatches-470]: https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/macros/dispatches.rs
[collateral-guide]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/docs/guides/mining/collateral.mdx
[max-uids]: https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/docs/hyperparameters/max-allowed-uids.mdx
