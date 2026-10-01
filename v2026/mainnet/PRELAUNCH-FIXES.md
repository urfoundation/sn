# Mainnet prelaunch fixes

Updated 2026-10-01. This is the production gate tracker for UR mainnet
SN25 (netuid 25). Sim-testnet is **closed with known exceptions, without final
acceptance**. There is no R49 requirement or instruction to resume it. Mainnet
hardening may proceed; launch readiness must be established on the selected
production release. No mainnet deployment or spend is authorized by this tracker.

**October 1 current-runtime root scope (MG-01/MG-04/MG-08):** the
[runtime470 review](../docs/spec/runtime-470-audit.md) binds the official immutable
source/release to the observed code and executed metadata. The full local rebuild
differs only in 22 hash-table seed constants in one Wasmi function; exact source
reproducibility failed and requires an explicit independently reviewed exception
for the exact official artifact. Runtime470 has removed
`set_root_weights` and its old enable/cap storage; the historical v3 root service
is not a launch-capable weight writer on that artifact. The additive
[v4 passive root service](ROOT-PASSIVE-SERVICE.md) selects
`passive_accumulate_in_place`, retains both independently approved UR roles,
and observes one separately approved existing netuid-0 identity under a finite
policy and independent full-config signature. It requires no heartbeat write,
root native signature, nonce or spend budget. Existing signed v3 actions and
custody remain unchanged; verify any externally held commitment before choosing
the first new plan. Native Sr25519/owner Ed25519 wire shapes pass current metadata
checks; native ECDSA signing remains unqualified by the 64-byte adapter. Real
root registration/stake, independently approved genesis/runtime/checkpoint,
qualified mapping, actual service installation/monitoring and complete UR
production admission remain open. No transaction or live deployment is implied.

**October 1 current-runtime census discovery (MG-01/MG-08):** the separate
[`subnet-discover` route](SUBNET-CENSUS.md) consumes a sealed but unapproved
runtime/finalized snapshot, rechecks its exact runtime and retained canonical
block, then collects bounded SN25/root forward/reverse membership and observed
owner/generation. Its [qualification](evidence/runtime470-subnet-discovery-20261001.md)
exercises v470's exact official metadata outside the repository and an
identity-free protocol projection with synthetic state in committed tests.
This resolves the initial discovery dependency on a pre-existing approved
owner/generation policy without manufacturing one: every seat remains
unclassified, membership completeness excludes custody/roles, and reset/apply
authority remains false. Independent network/runtime/source, protected-role,
removal and custody approvals, the complete launch census and trim execution
remain open. MG-01 and MG-08 are not closed by this increment.
The exact-key batch path completed the retained public snapshot with 256 SN25
and 64 root registrations in 13.231 seconds, after two 15-minute per-key
attempts failed under HTTP 429. Final affected coverage is 69 roots normal/race,
exact full-metadata tests normal/race, and six causal controls in each mode.

The [public EVM mapping fixture correction](evidence/evm-public-mapping-fixture-20261001.md)
restores meaningful outage and fallback coverage after adding the public header
route. Forty-two selected roots pass normal/race and three causal controls;
production bytes are unchanged. The unpartitioned 964-root `./mainnet` baseline
exceeded its 20/30-minute package limits, so a full-package verdict remains
unproven until its roots are qualified in complete disjoint partitions or with
a measured larger deadline.

The [October 1 read-only public finalized snapshot](evidence/public-finalized-snapshot-20261001.md)
closes the operational raw-header-method gap for the selected Rao archive:
the reconstructed Frontier header matched the native digest at finalized block
9,185,377. It retains `unapproved_observation` and does not close independent
runtime-source admission or authorize signing. That capture's frozen SN
source is `2d53e6f2`; server is `ecbf3aad`, with immutable Connect `e1b5d77b`
and SDK `5d37be38` pins in the server release graph. The
[fresh exact-source composition](evidence/release-current-source-20261001.md)
has passed for 17 binaries, five selected contracts and eight image contexts;
all eight local OCI image builds/readbacks and their independent archive audit
also passed as separate supplements. The earlier
image candidate is superseded. Local builds are not a published or approved
production release.

The [current unsigned preparation](evidence/public-unsigned-preparation-20261001.md)
binds clean SN `de0823ce` and server `0b8e758d` to another successful combined
Rao capture at finalized block 9,186,298 with unchanged runtime470 code and
metadata hashes. Both effective API module graphs still consume the reviewed
Connect `e1b5d77b` / SDK `5d37be38` pins; newer observed main heads are recorded
separately. Its exact source lock, partial release inventory, release input and
blocked plan are retained together. All ten actions remain blocked; network
and runtime authority remain missing. The earlier complete eight-image
aggregate does not cover these successor SN/server commits. Complete current
release composition/provenance and independent approval remain open.

## Closed testnet evidence and remaining lessons

Keep the original result, later recovery, and code qualification distinct:

| Evidence | Established result | Production obligation |
| --- | --- | --- |
| [Original R48 terminal report](../sim-testnet/FINAL-4.md) and [result](../sim-testnet/peerreview/evidence/FINAL-4-R48/result.json) | Run `20260926T202718.915754659Z-release-1.0` stopped on 2026-09-26 at 22:48:54 UTC with **zero complete acceptance epochs**, five failed assertions out of six, and `final_acceptance=false`. The signed boundary is not an accepted interval. | Preserve the failed result and excluded observations. Qualify actual production behavior independently. |
| R48 process-log gate | Two API stderr backlogs exceeded a scanner's 64 MiB delta; cursors did not advance. Validator steering also reported a missed approved native epoch 1696. | Adopt durable chunked scanning without starving foreground work; reconcile a separately authorized future native boundary for both validators. |
| Post-R48 scanner repair, SN `df7ae2c4` | Implemented in [process_log_gate.go](../sim-testnet/process_log_gate.go), with a causal regression and adjacent tests in the aligned R48 workspace. | This is simulator code and qualification, not a repaired R48 verdict or proof of the production image. Integrate and measure backlog/fault-heartbeat behavior on the release. |
| Retained recovery and client-key rollover | Recovery initially had zero ready providers despite live APIs. Server branch `fix/r48-client-key-policy-runtime-20260926` contains `4b2c4587` and `9da52551`; its policy-domain history fix removed that registration blocker. The [retained resume receipt](/mnt/data/sn-testnet/qualification/r48-continuation-20260926/resume-server-key-fix.json) records `retained_runtime=true`, `setup_actions_dispatched=0`, and **`final_acceptance=false`**. | PF-05 has a concrete server fix and a retained recovery result. Merge/lock the compatible server, migrations and consumers; prove processed-key and proof readiness prospectively. A successful resume cannot supply missed acceptance epochs. |
| R46 handoff replay during later recovery | [Continuation stderr](/mnt/data/sn-testnet/qualification/r48-continuation-20260926/release-r48c1.stderr) reports `provisional lifecycle cleanup observation differs from retained handoff bytes`. The [R46 handoff](../sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/runs/20260925T172403.199659160Z-release-1.0/fleet-lifecycle-handoff.json) retains its earlier approved lifecycle. The reader used the current recovery approval while validating historical provenance. | Treat this as a historical-approval validator bug, not demonstrated byte corruption. Candidate SN fixes `93949ee3` and `bbed208d` retain the authenticated historical approval through cold and cached replay; composed qualification and release inclusion remain open. Never rewrite the old handoff or relax current authority. |
| R48 immutable usage repair | Server `74893863` and the [guarded repair evidence](../sim-testnet/peerreview/evidence/FINAL-4-R48/repair-review.json) quarantine 4,183 epoch-658 contracts and retain 3,082,728 reported bytes as uncredited debt. Both epoch-658 roots missed their on-chain commit window; later epoch-659 commits are separate chain progress. | Deploy compatible snapshot writers/readers, retain the debt exception, and prove new usage, deposits, roots, capture and claims end to end. Neither a local close nor later roots repair the missed interval. |

The local recovery links are retained operational evidence, not portable release
artifacts. Hash and include the relevant sanitized records in the production
qualification manifest before relying on them outside this workspace. Closure
of testnet does not erase native-history, economic, archive, custody or coverage
exceptions.

The [closed testnet service cleanup](evidence/closed-testnet-services-20260928.md)
also stopped its four unused PostgreSQL/Redis containers after preserving their
state. Production shutdown must account for owned containers as well as process
children; retained evidence must not depend on keeping obsolete services alive.

## Production gates in execution order

These gates consolidate the stable RT/RL/PF/PH IDs below. `Planned` means the
production work is missing; `In progress` means identified implementation or
qualification exists but closure is incomplete; `Blocked` names a required
input or demonstrated incompatibility; `Done` requires the stated evidence.
An implemented simulator fix is not a completed production gate. Owners below
are component responsibilities; assign a named operator before rollout.

The release manifest must name the production roles and platforms being
admitted. Qualify every selected artifact and its consumed dependencies, while
leaving unselected services/platforms explicitly unqualified and unavailable
for deployment. For example, an explicitly amd64-only release does not require
an arm64 execution pass. This narrows qualification scope without weakening
custody, economic, recovery or runtime checks for the deployed roles.

**September 30 custody decisions.** The subnet owners report that SN25's
existing owner account is Ledger-derived. They have no Snow access and will run
the signing command on their own device using the Polkadot generic app; Snow
may import only an exact signed reply after checking the current owner,
signature scheme, runtime metadata digest, nonce, era and approved action. The
current sr25519-only owner-trim v1 packet is not a Ledger signing path; retain
its original liabilities. The [offline Ed25519 owner command](OWNER-SIGNING.md)
and [independent software qualification](evidence/owner-ledger-signing-qualification-20260930.md)
are integrated. A [pinned Linux native SDK artifact](evidence/owner-ledger-native-sdk-qualification-20260930.md)
has actual-extension and synthetic-device qualification; the owners' platform,
physical device and deployed runtime digest remain unqualified. A separately
approved v2 action is still required before a live owner call.
The netuid-0 root hotkey uses a **different hardware signer** whose device/API
is still unspecified. Each operator keeps its own EVM demand-deposit signing
key in that operator's **secrets vault**, separate from the on-chain settlement
vault and from owner/root custody. Verify both operator `depositSigner`
bindings and deposit hotkeys against the current coordinator version before a
new funded attempt; do not infer deployment or funding from this decision.

The [qualified server deposit-custody increment](evidence/operator-deposit-custody-qualification-20260930.md),
integrated at `45e11196`, binds each new or replacement demand deposit to the
current operator signer/hotkey and canonical economic graph before staging. It
preserves the original principal, bounds the two-rao reserve allowance and
one-rao transfer loss, and reconciles retained intents before another attempt.
Fifty-four roots pass normal/race and eight fault controls are causal. Actual
operator vault provisioning, two distinct funded addresses, live deposit
receipts and independent monitor deployment remain open.

| Gate / priority | Owner and linked items | Current state | Next action and completion evidence |
| --- | --- | --- | --- |
| MG-01 / P0 — Mainnet identity | Node operator; RT-01/02, PH-04/14/18/19 | **Open for activation:** the [current unsigned preparation](evidence/public-unsigned-preparation-20261001.md) retains the Rao archive's combined native/EVM observation at block 9,186,298, Bittensor/EVM964 and the unchanged exact runtime470 code/metadata hashes. Its ten-action plan is fully blocked, with network/runtime authority missing. The [qualified public-header fallback](evidence/public-header-fallback-20261001.md) succeeds while an exact-hash raw-method probe still returns `-32601`; independent genesis/runtime/source approval remains pending. Snow VPN remains a future failover and last returned HTTP 502. The earlier [Snow/testnet comparison](evidence/finalized-snapshot-snow-20260927.json) remains historical evidence, not a current mainnet route. | Review the exact unsigned bundle, independently approve genesis/runtime/source identity, and repeat finalized checks before signing. Apply finite public-RPC concurrency; do not silently retarget signed bytes or treat the declared review target as approval. |
| MG-02 / P0 — Reproducible production release | Release owner; RL-01, PH-06/16 | **In progress:** the [current unsigned preparation](evidence/public-unsigned-preparation-20261001.md) locks clean SN `de0823ce` / server `0b8e758d`, records both exact effective Connect `e1b5d77b` / SDK `5d37be38` graphs, and distinguishes newer observed main heads. Its partial inventory and blocked plan leave release completion/provenance/approval false. The [eight-image aggregate](evidence/release-image-aggregate-qualification-20261001.md) proves local source-to-image linkage only for frozen SN `2d53e6f2` / server `ecbf3aad`; it does not cover the successor validator/operator changes. The [earlier SDK/MG06 composition](evidence/incremental-source-composition-20260929.md) retains its 618/618 normal roots and 37 focused normal/race passes; its historical race package closure remains incomplete. The [v11 inventory](evidence/release-inventory-candidate-v11-20260927.json) and [taskworker reproduction](/mnt/data/sn-testnet/evidence/mainnet-server-taskworker-image-20260927/RESULT.md) retain their own exact source scopes. Policy and published/deployed OCI identity are missing; arm64 execution, package archive, independent builder, full attestation/SBOM/scanner policy and remaining service coverage remain open. | Compose the complete successor source release and its exact generated artifacts, contract bytecode, source-to-image provenance, config, policy, migrations, images and role coverage; qualify actual production paths and approve one immutable manifest. Preserve earlier receipts without inheriting their claims across changed sources. Exact source/file hashes and local platform digests are not deployment authorization. |
| MG-03 / P0 — Durable recovery and complete evidence | Transaction/recovery owner; PF-01/03/04, PH-01/02/05/07/17/21/24/25/26 | **In progress:** the [mainnet miner fleet](../miner/FLEET-MAINNET-RUNTIME.md) now persists signed register/publish/bind/revoke intents and reconciles their original canonical outcomes before any identical-byte retry; affected miner/onchain/chain normal, race and vet pass. The [operator receipt-census fix](evidence/operator-recovery-census-20260927.md) preserves signed candidates after an inconclusive read; 19 affected test roots pass normal/race, with package vet and formatting checks. A [qualified status-independent signature census](evidence/operator-signature-census-qualification-20260928.md) preserves original, replacement and cancellation bytes across selected operator databases and evidence stores with private create-only restoration. The [conditional offline receipt/fee join](evidence/operator-receipt-fee-qualification-20260928.md) retains missing/conflicting candidates and counts observed gas once per resolved nonce. The [qualified receipt commitment verifier](evidence/operator-receipt-commitments-qualification-20260929.md), integrated at server `fbe0c039`, now authenticates exact signed transaction/receipt bytes, status, cumulative-gas differences and raw-header ancestry relative to a supplied EVM boundary; all 52 affected roots pass normal/race. The [qualified bounded collector](evidence/operator-receipt-collector-qualification-20260929.md) is integrated at server `b7c8c743`; all 71 recovery/CLI roots pass normal/race and five causal controls pass. The [qualified native finality proof](evidence/operator-native-finality-proof-qualification-20260929.md), integrated at `44636e5e`, verifies weighted GRANDPA certificates, scheduled authority handoffs and the exact native/EVM commitment relative to a pinned checkpoint; all 89 roots pass normal/race and five causal controls pass. Checkpoint/genesis/runtime authority remains unapproved and actual fees remain null. The [pinned-runtime fee dependency review](https://github.com/urnetwork/server/blob/cfcbfcbaa13b4f4d298acfeca761a7252c18ddee/strecovery/ACTUAL-FEE-DEPENDENCIES.md), integrated as documentation at `cfcbfcba`, keeps generic phase-bound balance events and block deltas unqualified for gas attribution; failed/partial refunds require exact runtime evidence. Bounded native-state proofs and runtime-qualified debit/refund attribution remain separate from checkpoint approval. The [qualified bounded native finality capture](evidence/operator-native-finality-capture-qualification-20260929.md), integrated at server `5ff7bf02`, preserves the exact collection through durable native proof capture; all 112 affected roots pass normal/race and seven causal controls discriminate in both modes. Independent checkpoint admission, owned-node capability, account nonce and native debit/refund proofs, service adoption, historical approval correction, journal retention and cross-host custody remain open. | Migrate the retained-evidence model into every production owner; reconcile every original/replacement/cancellation signature and historical approval. Crash/restart and cold/warm-cache qualification must preserve finalized work, custody, failed evidence and single ownership without repeated spend. |
| MG-04 / P0 — Runtime and native continuity | Chain/validator owner; RT-01 through RT-08, PH-03/04/10/18/19/22 | **In progress:** the [standard validator production path](OWNER-RECYCLE-PRODUCTION.md) uses separately signed schema-3 authority, an exact block/purpose-bound producer interface and original authority through startup, preparation, recovery and archive readers. Bounded content-addressed complete config/approval history now preserves signed sidecars, the original drain and proof progress across compatible independently approved renewals and source-file loss. Old configs remain read-only. The [qualified source-receipt correction](evidence/validator-source-runtime-qualification-20260929.md) separates original signing, parent execution and post-state views across an approved upgrade; 103 selected roots pass normal/race. [Downstream upload admission](VALIDATOR-UPLOAD-RUNTIME.md) projects exact runtime windows from those bundles without retaining producer authority. The [miner fleet mainnet gate](../miner/FLEET-MAINNET-RUNTIME.md) retains exact-artifact and uncertain-send recovery for its four mutations. The shared nonce reader requires the exact reviewed 56-byte Subtensor account layout. The [signed continuity policy and inspector](RUNTIME-CONTINUITY-POLICY.md) have scoped independent qualification; [finite offline replay](RUNTIME-SEMANTIC-REPLAY.md) checks exact supplied artifact/state cases. Complete semantic proof and automatic production selection remain absent. No live mainnet authority or deployment is supplied. | Complete remaining consumers, both validator roles, automatic compatible-upgrade and missed-boundary qualification. Arbitrary policy/key/custody changes require separate transitions. Preserve original pending bytes and finalized work; no backdated native success. |
| MG-05 / P0 — Policy and identity rollover | Server/validator owner; PF-02/05, PH-07/13/27 | **In progress:** server policy-domain rollover and retained resume remain evidenced above; the v651→v724 migration-monitor namespace bug is corrected. The [qualified MG03/R48 composition](evidence/operator-mg03-r48-composition-20260929.md) is integrated at server `05fee56f`, preserving both original histories and exact signed approvals through registration replay, policy rollover and deletion. The [operator epoch-policy correction](evidence/operator-policy-custody-qualification-20261001.md) authenticates retained payout policy/window independently of the current configuration and composes both operators' populated migration, processed registration, restart and fresh proof reads. Live operator cutover and readiness remain open. | Migrate both operators before APIs, retain old signed histories, activate all validator/operator evidence domains and authenticate persistent peer-key transitions. Prove production processed-key readiness and fresh proof progress through a future policy boundary; retain prior-epoch payout policy authority for successor deposit sizing. |
| MG-06 / P0 — Economics, settlement and custody | Protocol/contracts/treasury owner; PH-11/12/14 and R48 usage lessons | **Blocked for live activation:** the selected 90% owner-recycle has a distinct [production transition](OWNER-RECYCLE-PRODUCTION.md), separate from the unchanged read-only admission/capsule formats. It joins genuine provider proof replay, canonical coordinator facts, native owner/validator eligibility and a signed drained activation block, then binds the 10/90 row through real CRv4 preparation, a hotkey sidecar and durable intent/archive replay. Complete original authority history now preserves these signed decisions under compatible approved renewals without reinterpreting their economic policy or requiring another drain. The [bounded native incentive observer](evidence/incremental-source-composition-20260929.md) now retains canonical event/state evidence and sealed partial results: 18 focused and 170 adjacent roots pass normal/race, with eight causal controls. Native denominator, quantization, recipient generation, provider entitlement, actual owner recycling and the 10/90 outcome remain unresolved. No live approval, native payout or 10% outcome exists. The non-upgradeable [vault claim repair](../evm/CLAIM-RECOVERY.md) preserves accepted credit after an exact runtime payment failure; Forge 226/226 and focused receipt normal/race passed, but runtime rollback and deployment remain unqualified. NetEscrow migrations through 728 and fenced publishers are not deployed. | Obtain actual mainnet identity, reviewed source/code mapping, signed production approval, recognized owner recipients and eligible independent validators. Supply complete real activation, operator API/key/payout custody and authenticated history; qualify production receipt/restart and the runtime's exact native rounding. Monitor inclusion, reveal/application and the actual 10% native-miner / 90% recycle outcome after first submission; that outcome is not a circular first-send prerequisite. Complete vault rollback, reserve funding, deposits/capture/carry/claims and NetEscrow cutover. Recycling does not fund the reserve. |
| MG-07 / P0 — Continuous monitoring and bounded repair | Operations owner; PH-15/28, PH-01/02/07/09/11/16 | **In progress:** signer-free `inspect`/`monitor` identity and finality commands exist. The v3 checkpoint retains finalized continuity, last successful read and initial or later read outages across restart; warnings begin at two minutes and critical status at five, with immediate escalation on clock rollback. [Monitor telemetry](MONITOR-TELEMETRY.md) wires atomic textfile gauges into the actual command; [alert examples](monitor-alerts.example.yml) detect missing expected hosts, stale samples and explicit severity. Source qualification does not establish deployment or delivered alerts. The [qualified operator journal increment](evidence/operator-monitor-qualification-20260930.md) adds bounded read-only transaction/settlement observations with incident continuity: 62 Go root executions and four alert fixtures pass; nine normal and five selected race controls are causal. The [qualified stopped-validator resume](evidence/validator-repair-qualification-20260930.md) adds one incident-bound, independently signed start under exact release/unit/generation custody: 52 positive executions pass normal/race, and twelve normal plus seven selected race controls are causal. No unit was installed or started. The separate [active-hang capability](ACTIVE-VALIDATOR-REPAIR.md) adds an independently signed one-generation stop/join/start interface. Production approval, real-systemd rehearsal, deployment, unobserved domains, root/operator services and broader repair coverage remain open. | Deploy and verify actual collector ingestion and delivered alerts against an independent expected-host roster. Implement the remaining [operating model](MAINNET.md#continuous-monitoring-and-repair), approve its SLOs and repair envelopes, provision primary/backup on-call, and rehearse outage, wrong chain, missed deadline, uncertain send, full disk and monitor failure. Independent alerts must survive a stopped application and a stopped controller. |
| MG-08 / P0 — Mainnet bootstrap and both validator roles | Bootstrap/governance owner; PH-14, [MAINNET.md](MAINNET.md) | **Blocked for activation:** the [owner-trim planner](SUBNET-CENSUS.md), [recheck/reconciliation](OWNER-TRIM-GUARD.md) and [bounded qualification](OWNER-TRIM-BOUNDED.md) retain safe partial candidates and old-miner residuals; no full reset is claimed. The [offline chain composition](BOOTSTRAP-CHAIN.md) has [qualified v2 UR config admission](evidence/ur-bootstrap-admission-qualification-20260928.md) for exactly two initial schema-3 signed UR configs matching independent role/signer/runtime/source/deployment pins and protected generations. [V3 root-role admission](evidence/root-role-admission-qualification-20260928.md) independently pins the netuid-0 generation, action approver and signed full-service approver while retaining live authority as pending. Durable local plan/apply/resume preserves child signatures and allowances; v1/v2 recovery keeps its original scope. The [qualified read-only readiness phase](evidence/bootstrap-readiness-qualification-20260929.md), integrated at `6627d15f`, binds original v3 custody to current finalized UR/root prerequisites; all 148 affected roots pass normal/race with four causal controls. The [qualified durable owner-trim action](evidence/owner-trim-null-storage-repair-20260929.md), integrated at `ce567305`, preserves original v3 custody and passes 25 focused plus 229 expanded roots normal/race with eight causal controls; the failed R1 null-storage qualification remains preserved. The qualified [contract-role declarations](evidence/bootstrap-contract-role-qualification-20260930.md), [original receipt prefix](evidence/bootstrap-contract-receipts-qualification-20260930.md), and [current five-account field checks](evidence/bootstrap-contract-current-qualification-20260930.md) now retain exact original authority with 40, 42 and 38 positive normal/race executions respectively. Current fields are owned-RPC assertions; complete storage/history, evidence anchor and activation remain unverified. The [qualified archive census](evidence/safe-history-census-qualification-20260930.md) now retains complete bounded native/EVM witnesses with 68 positive normal/race executions and ten normal/five selected race causal controls; internal/reverted execution and complete Safe history remain unproven and public submission stays closed. The [qualified two-UR host component](evidence/validator-activation-qualification-20260930.md) implements static installation, exact runtime config copies and durable separate process starts/recovery: 88 positive root executions pass normal/race, with twelve normal and five selected race causal controls. Its public fresh-start authority remains deliberately unavailable; no unit was deployed or started. Production enforcement/custody and the pending best-effort risk-policy choice remain open. Live eligibility, chain effects, service activation and full release qualification remain pending, with no live mainnet authority, signing device or global custody fence supplied. [Review schema v2](PLAN.md) keeps preconditions distinct from produced facts. | Complete live identity/census and custody effects; use the ranked owner trim, exact recheck/reconciliation and bounded protected-identity conditions, retaining explicit old-miner dispositions. Finish production safe-trim authority/execution and the remaining durable chain phases: complete contract installation including the evidence journal/anchor, two UR validators plus netuid-0 role, Safe authority and bounded funding. Qualify production current authority, custody/device and owned route, wire service activation, and observe actual 10/90 native outcomes after approved activation. |
| MG-09 / P1 — Sustained resource and storage capacity | Service/storage owner; PH-08/09/20/23/26 | **Planned:** bounded simulator mechanisms exist; production sizing and restoration receipts are missing. | Measure backlog, bytes, memory, RPC work and queue fairness with the proposed fleet and retention. Bind finite capacity with reviewed margin; prove missing/full-volume behavior, backup restore, multi-gigabyte log drainage and foreground deadline headroom. Required before unattended operation. |
| MG-10 / P0 — Qualification, rollout and actual acceptance | Release/operations owner; PH-16 and every affected gate | **Planned:** no accepted composed mainnet release. | Before activation, complete production-path causal regressions, affected normal/race suites and a controlled upgrade/outage/restart/repair rehearsal; retain failed and reused scopes. After bounded activation, observe at least three complete native emission intervals and one full 50,400-block UR settlement/claim cycle before declaring program acceptance. Record pending live evidence as pending. |

MG-09 has a concrete [build-host storage observation](evidence/build-host-scratch-migration-20260930.md): the root volume reached 100% with 4.1 GiB available during release preparation. Two inactive, verified scratch trees were moved to `/mnt/data` while their original paths remained readable through symlinks, leaving about 54 GiB free on `/`. Continue placing qualification scratch and caches on `/mnt/data`; this cleanup does not close production capacity, retention, restore, or full-volume tests.

MG-03 also includes a [qualified miner claim-queue owner fix](evidence/miner-claim-queue-owner-qualification-20260929.md).

It locks the physical queue directory across read, publication and joined
shutdown so duplicate daemons or a replaced pathname cannot split signed
outcome custody. The separately [qualified retained-byte successor](evidence/miner-claim-queue-capacity-qualification-20260929.md)
caps each queue at 16 MiB and preserves oversized retained bytes on refusal.
Both sources are integrated; 76 affected roots pass normal/race and all 292
miner roots pass plain normal. The composed release still needs custody,
restart and aggregate fleet-capacity qualification before deployment.

MG-01's [read-only Snow route checks through October 1 01:18 UTC](evidence/snow-route-observation-20260930-1914.md)
returned HTTP 502 for both native genesis and EVM chain ID in the earlier
and latest probes, and for native genesis alone in the intervening probes.
They provide no new mainnet identity or sync evidence; the route remains a
live launch gate.

MG-08 now also has an [independently qualified native prerequisite reader](evidence/validator-native-admission-qualification-20260930.md)
for both UR validators. It checks the original signed runtime at the current
and activation-checkpoint hashes, native epoch/drain facts, owner and generation,
activity, explicit Recycle, canonical anchors and sample age. The public
fresh-start authority is still nil: operator proof/client-key readiness,
contracts, global signer custody and effective-majority stake remain open.
The original schema-3 bootstrap config cannot silently absorb a later runtime
upgrade; a separately approved continuity and activation rollover is needed.

The [qualified `admit-committed` increment](evidence/validator-committed-prefix-qualification-20260930.md)
reads each standard validator's service-owned committed control history against
both original operator origins and retains completed checkpoints. Its inherited
startup fixture failures were repaired in tests without changing production
bytes; the original failed receipt remains preserved. This closes only the
committed-control-prefix subgate. Unsealed ledger/intent state, live worker
attestation, signer custody, applied weights influence and launch authority
remain open; neither validator is authorized to start publicly.

The [independently qualified unsealed inventory](evidence/validator-unsealed-inventory-qualification-20261001.md)
extends that read-only observation to the actual signed ledger tails,
unfinished trails, import receipts and protected empty intent boundary.
Selected normal/race and privileged UID tests pass, and seven causal guards
refuse their intended mutations. The subsequent
[qualified bounded tail-boundary implementation](evidence/validator-tail-boundary-qualification-20261001.md)
derives every distinct boundary from signed tail replay and authenticates its
canonical finalized hash, epoch, policy window and operator eligibility. Its
new optional checkpoint scope preserves the earlier inventory's original
authority and retains explicit refusals for nonempty intent graphs. Independent
qualification of this extension passed; historical provider bindings, live
workers and the remaining launch gates stay open. Neither validator is
authorized to start.

The [qualified validator current-evidence increment](evidence/validator-current-evidence-qualification-20260930.md)
authenticates original dual-signed operator activation and fresh nonce-bound
client-key responses, and checks the approved five-contract graph at one
native-header-bound EVM point. Its first frozen build exposed an EVM RPC
allowlist mismatch; the corrected build uses the bounded EVM read profile and
passes the formerly failing contract fixtures. The failed and corrected receipts
are both retained. This is partial admission only: full proof-prefix/worker
health, deployment/source provenance, global signer custody and effective
majority remain open; public fresh start remains nil.

The [qualified stake-capacity admission](evidence/validator-stake-capacity-qualification-20260930.md)
adds an original-plan `admit-stake` observation for both UR validators.
Its complete census conservatively bounds weighted-stake floors through the
pinned runtime's threshold, owner exception, quantization and factor×tempo
activity rule. Independent frozen and merged suites pass, including a causal
pre-mask denominator control. A capacity lower bound is not applied-weight
influence or live effective majority; public starts and that outcome remain
open with producer health and signer custody.

The [qualified validator proof-health increment](evidence/validator-proof-health-qualification-20260930.md)
replays both roles' original pinned operator activation prefixes and retains
completed proof checkpoints if a later read fails. It reports standard process
progress separately from proof integrity: missing/stale progress is a warning,
while wrong ownership, source/generation or contradictory proof bytes refuse.
The service-UID ownership guard remains intact. Current mutable-prefix and
per-operator live-worker completeness, global signer custody, applied influence
and signed launch authority still block public start.

The [MG03/R48 server source composition](evidence/operator-mg03-r48-composition-20260929.md)
is integrated at server root `05fee56f`. Its parent full merge preserves both
original histories and matches the qualified MG03 tree byte-for-byte; the
successor adds only two deterministic composition tests. Sol qualified all
134 affected base roots and, separately, the two new roots plus 52 adjacent
model roots on the successor, each normally and under race. The six conflict
resolutions, complete controller lineage and original approvals remain intact.
The composed release build, actual dependency/artifact lock and live rollout
remain pending; the qualification's pinned dependency graph remains explicit.

MG-08 also exposed a production bootstrap scaling failure: action-seven
validation repeatedly expanded a shared predecessor graph, making one call
copy 128 projections and hash the same 106 KB approval configuration 254 times.
The [integrated graph correction](evidence/bootstrap-contract-plan-graph-qualification-20260929.md)
keeps one private copy and one validation per distinct object within each
invocation while retaining the exact approval, journal and output encoding.
Its scoped qualification passed 5 graph, 28 evidence and 177 adjacent roots in
both normal and race modes, with six causal controls. The independent full
`./mainnet` normal package reached its 60-minute timer after 299 passing roots
and zero assertions, so that broader check remains incomplete. Live deployment
is a separate gate; do not treat a package timer or a cached success from a
prior invocation as approval.

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

The [corrected SDK coverage and scheduling review](evidence/bootstrap-contract-successor-qualification-20260929.md#separate-sdk-package-coverage-and-scheduling-lesson)
now records 543/618 roots backed by package-PASS race streams, 75 pending and no
root failures. The evidence predecessor checkpoint root executes fourteen serial
fixtures and passed in about 28 minutes in both its shard and exact-root retry.
Isolate that long root into its own package/time budget and partition other roots
by observed duration; retain all causal cases and the original timeout records.
This is a qualification scheduling lesson, not a demonstrated product failure.

The MG-08 [offline contract prerequisite increment](BOOTSTRAP-CHAIN.md#offline-contract-installation-prerequisites)
adds `bootstrap-chain contract-plan` before custody and `contract-readiness`
over the original v3 preparation and retained action journals. It exposes missing
anchor approval, the original eight-attempt/nine-action mismatch, and unresolved
Safe-inner authority, code/runtime provenance, relayer funding and custody.
Completed canonical-action receipts remain historical retained facts; eight
completed actions leave only the anchor unfinished and do not require replay.
A cap change needs a new independently signed successor that adopts the original
prefix, reconciles unfinished signed nonces, and conserves cumulative attempts
and lifetime financial exposure. The later conditional execution custody owner
is described below; its concrete canonical adapter has scoped independent
qualification.
The [focused qualification](evidence/bootstrap-contract-prerequisites-focused-qualification-20260929.md)
passes all twelve new roots normal/race with six causal controls in both modes.
The separate [partial adjacent battery](evidence/bootstrap-contract-successor-full-v3-qualification-20260929.md#separate-composed-smoke-and-partial-adjacent-coverage)
passes 150/270 roots in both modes, with ten package PASS/exit-zero terminals
and no root failures or skips; 120 roots remain unrun. This does not close MG-08 or
establish installation, role activation or native 10/90 acceptance.

The separate [unsigned contract successor proposal](BOOTSTRAP-CONTRACT-SUCCESSOR.md)
has [scoped Sol qualification](evidence/bootstrap-contract-successor-qualification-20260929.md):
six new and twelve inherited roots pass normal/race, with six causal controls
in both modes. It can retain eight
completed receipt seals, carry original spend forward, propose additive attempt
and lifetime ceilings, and preserve an original ninth reservation. It does not
sign or execute a successor. The later durable adoption owner has its own domain;
current Safe authority and canonical receipt proof remain required before live
execution. Changed original v1 approvals cannot adopt old custody.
The qualified [signed local preparation](BOOTSTRAP-SUCCESSOR-PREPARATION.md) adds
one fixed original-root claim, a distinct independently verified preparation
approval and resumable publication. Its [scoped receipt](evidence/bootstrap-successor-preparation-qualification-20260929.md)
passes thirteen focused and three adjacent roots normal/race, with thirty-two
intended causal executions. It preserves the eight retained receipts and
cumulative proposed floors; no executable allowance, Safe authority or signing
path is created. Copy/restore/move onto a different physical root requires a
separately approved migration. The original full-v3 command remains an explicit
adjacent root, and an eighth-action control checks its extracted fixture helper.
The [separate six-root composed smoke](evidence/bootstrap-successor-preparation-qualification-20260929.md#separate-composed-smoke)
passes normal/race on exact merge `93a0a060`. It composes both full-v3 commands,
same-root resume, pure Safe calculations and MG-07 incident recovery without
expanding the separately pending broader package coverage or live authority.
The [separate full-v3 public-command fixture](evidence/bootstrap-contract-successor-full-v3-qualification-20260929.md)
closes that specific gap on `c294fefd`: its positive root passes normal/race,
and all six causal executions reach the intended assertion. It approves the
successful full-metadata fixture's finite 60-second send budget before custody.
The original d7 control's one-second local POST timeout remains a preserved
noncausal attempt; production deadlines and targeted lost-reply fixtures are
unchanged. Give successful full-graph fixtures enough bounded local execution
time while retaining every causal case and the separate long-root scheduling
lesson above. The fifteen-root MG-07/prerequisite smoke on `79ff2c6e` and the
twelve-root successor smoke on `1e2b2abb` pass both modes on their own exact
graphs; neither expands the partial adjacent battery or SDK package coverage.

The [offline Safe release verifier](SAFE-RELEASE-VERIFY.md) now has [scoped Sol qualification](evidence/safe-release-profile-qualification-20260929.md):
six new and four adjacent roots pass normal/race, and eight causal controls each
reach their intended assertion in both modes. Explicit version/variant profiles
bind unchanged published archives, proxy/singleton code, ABI, compiler inputs,
source provenance and storage layout. Independent compiler rebuild, live Safe
binding/authority and signing/execution remain unresolved. Existing versus new
Safe remains a user decision; a new address must match retained initializerOwner
or have separately authorized ownership migration. This artifact-only increment
does not grant signed successor adoption, nonce custody or evidence-anchor execution.

The [pure Safe evidence increment](SAFE-EXECUTION-EVIDENCE.md) has
[scoped independent qualification](evidence/safe-execution-evidence-qualification-20260929.md)
on `c648495f`: thirteen focused and four adjacent roots pass normal/race,
with all twenty-six intended causal executions. Its [separate four-root composed smoke](evidence/safe-execution-evidence-qualification-20260929.md#separate-composed-smoke)
also passes both modes on that exact source; it predates the signed local
preparation merge. It calculates exact digests,
inspects supplied signature forms and classifies declared inner outcomes and
nonce rollback. Current owner membership/threshold, contract callbacks, approved
hash storage, canonical receipts, Safe authority, signing, custody and execution
remain unresolved. E074's twelve setup EOF failures are retained separately;
the corrected fixture filters catalog variants before decoding and tests absent,
empty and malformed unselected members. The earlier artifact verifier's separate
nine-root composed smoke passes both modes on `18a88db4`. These results do not
close the 75 SDK package gaps or 120 unrun MG-08 adjacent roots.

The [offline successor Safe review](BOOTSTRAP-SUCCESSOR-SAFE-REVIEW.md) has
[scoped independent qualification](evidence/bootstrap-successor-safe-review-qualification-20260929.md)
on `d0207448`. It connects the original v3 graph, completed independent preparation and
full selected Safe release to the exact zero-value evidence-anchor CALL digest.
Its new read-only preparation consumer rejects absent, partial, forged, staged,
unsafe or actively published claims without repairing their bytes. The proposed
relayer liability includes both completed maximum envelopes and any original
ninth reservation; original attempts and additive ceilings remain unchanged.
Test compilation and vet pass. Nine new and four adjacent roots pass independent
normal/race runs, including the full-v3 public command and published-bytecode
digest oracle. All fourteen causal executions reach their intended assertion;
the sealed logs, patches, exact source and dependency graph were independently
checked. This does not close MG-08 or broaden prior package coverage.
Safe owner signatures and complete outer calldata remain absent, as do
execution approval, nonce/budget allocation and live authority. The review's native
window is outside the Safe digest and cannot expire a signature. Canonical
eight-receipt adoption, Safe/evidence state, signature lifetime/window enforcement,
globally fenced relayer signer custody and qualified canonical execution remain
required before evidence anchoring or activation. The concrete adapter described
below does not supply the external live authority by itself.

The [successor execution custody and owner](BOOTSTRAP-SUCCESSOR-EXECUTION.md) now
binds independently approved signatures and an exact outer envelope to the
original eight-receipt adoption, cumulative attempt/financial floors and distinct
Safe-inner/relayer-outer nonce claims. Its qualified initial public commands are
offline; the one-send machine requires an authenticated adapter for every
historical, current-state and canonical receipt decision. Its
[scoped qualification receipt](evidence/bootstrap-successor-execution-qualification-20260929.md)
records twenty-one focused and six adjacent roots passing normal/race on
corrected `75ea2158`, including interrupted publication, counted attempts, ambiguous send recovery,
nonce conflicts, exact inner success and the full public-v3 original graph.
Ten isolated causal controls each reach their intended assertion in normal/race,
and the forty-five-file evidence manifest verifies. This does not close MG-08 or
broaden earlier package coverage.

The [live execution-custody follow-up](evidence/bootstrap-successor-live-custody-20261001.md)
closes an in-process gap: an owner could send after losing a counted intent, or
report installation after losing its terminal record, while using its cached
event. Read-only checkpoints now authenticate the complete intent/record prefix,
claim/ready markers and interrupted outcome bytes before send/result admission.
All 55 affected roots pass normal/race with package PASS, all three checkpoint
bypass controls fail at the intended assertions in both modes, and vet passes.
The original eight-action custody, exact signatures, cumulative attempts and
financial reservations stay unchanged. Public submission and current-only policy
approval remain closed gates; this correction supplies no live authority.

The initial twenty fixture failures remain preserved: private binary files had
shared temporary parent directories. The test-only correction explicitly uses
`0700` parents; production readers remain strict. The first corrected race run
hit the default ten-minute package timer after fifteen passing roots and zero
failed root assertions. The unchanged twenty-one-root retry passes in 662.949s
with an explicit twenty-minute harness budget. The public-v3 and thirty-boundary
recovery roots together account for about 398 seconds under race. Size the whole
qualification from measured fixture cost; preserve timed-out attempts and keep
production transaction deadlines unchanged.

The concrete canonical adapter has
[scoped independent qualification](evidence/bootstrap-successor-canonical-qualification-20260930.md)
on corrected frozen source `a7186754`: ten focused and twenty-two adjacent roots
pass normal/race, fourteen normal causal controls and six selected race controls
reach their intended assertions. The thirty-four-file Sol manifest and separate
forty-eight-file author handoff verify with matching source/module fences.
Preliminary `82da3d40`
passed all eight roots normal/race before discovery of the Safe storage-provenance
gap; preserve those results without treating them as corrected-source evidence.
A separate signed canonical authorization
binds the exact execution plan, pinned Safe build review, reviewed current-runtime
profile and its evidence, every Safe/relayer signer's cutover plus retained
original reservations, and a separately signed exact Safe deployment/storage
history statement. The adapter borrows all eight original marker locks and
reauthenticates the original signed bytes, receipts and postconditions through the
existing historical native/EVM adapter. Each original receipt receives its own
approved retry budget. Successor admission and inclusion use the separately
approved current runtime; an ordinary later runtime upgrade cannot erase a
historical original or counted successor receipt.

Online `contract-successor-execution-resume` requires the separately pinned
canonical approval. Public `--submit` is unavailable until a distinct canonical
Safe history authenticator is implemented; it exits before custody loading or
attempt reservation even when all independent review files are signed.
The [bounded archive census](SAFE-HISTORY-CAPTURE.md) supplies a real read-only
command with private create-only witness retention, complete native-body/EVM
transaction/receipt commitment checks and later-head continuity. Its
[paired-source qualification](evidence/safe-history-census-qualification-20260930.md)
is sealed: 68 positive root executions pass normal/race, ten normal and five
selected race controls are causal, including a distinct normal fixture-oracle
control. It accepts unrelated traffic but does not prove internal/reverted
actions, native hook effects, clean initialization
or complete Safe history. Public submission and MG-08 remain open gates; the
unchanged signed history policy cannot be discharged by these raw archives alone.
The [qualified native trace increment](evidence/safe-history-native-trace-qualification-20260930.md)
adds bounded block traces, parent runtime code proofs, retry/cancellation and
canonical closing checks without changing original custody. The SDK's filtered
keyless events and missing rollback/inner EVM boundaries keep complete Safe
history unproven and public successor submission closed. A qualified node
extension or independent full replay is still needed before that action.
The adapter checks actual pinned Safe proxy/singleton code and scoped
finalized/pending authority, both nonce domains, funding, current contracts and
the exact one-shot evidence binding. Finalized reads keep one canonical hash
while later heads advance; later native-window and runtime checks do not require
head equality or restarting the snapshot. Current state RPCs retain their own
bounded retries. Exact-hash lookup uses `eth_getTransactionByHash`; the adapter
requires neither `txpool_content` nor `author_pendingExtrinsics`.

Eight light roots cover independent authority, immutable authority recovery,
published Safe state, scoped pending lookup, strict receipt fields, signed
provenance scope and real orphan owner/module mappings. Two
separate heavy roots run the full original v3 graph and actual pinned Safe
execution under an explicitly injected synthetic history capability, including
lost reply/restart, approved runtime change, renewed read
deadlines, advancing canonical heads and refusal of a changed canonical hash.
Author compile-only, vet and formatting checks pass. Sol's independent heavy
normal packages take 51.406s and 45.496s; their separate race packages take
349.951s and 297.814s. The adjacent race package passes in 540.635s. Explicit
twenty-minute normal and thirty-minute heavy/adjacent race harness budgets leave
production transaction and individual read deadlines unchanged.

Enforced signer cutover to one registry, independently approved mainnet
genesis/runtime and Safe authority, funding, actual owner/relayer signatures and
live readback remain separate gates. The owned RPC's finality and account-pending
responses are assertions. Independent build and cutover evidence explicitly
attests external assumptions; local locks cannot establish cross-host signer
exclusivity or the absence of off-node signatures. MG-08 remains open, without
installation, activation or native 10/90 acceptance.

**P0 gate — Canonical Safe deployment and complete storage provenance.** The
independent provenance statement binds the exact plan, Safe/profile, published
proxy/singleton runtimes, deployment transaction, reviewed native snapshot and
separately pinned history evidence. Its signature is necessary review input and
does not implement the distinct `bootstrapSuccessorSafeProvenanceAuthenticator`.
Safe sentinel-list getters cannot prove the absence of enabled owner/module
mapping entries outside those lists. The real-code malicious-storage fixture
demonstrates both kinds of orphan authority while ordinary getters remain clean.
Before enabling the original complete-history submission route, implement and independently qualify canonical
deployment/initialization and every authority-relevant storage/delegatecall
mutation through finalized and scoped pending state, bound to the exact approved
route/account/profile and signed evidence. Reports or flags must never inject
this capability. The separate
[qualified readmission increment](evidence/bootstrap-successor-readmission-qualification-20260930.md)
`cd4261a8` puts expensive proof before the final scoped pending Safe/relayer nonce
and Safe-state admission, and clears earlier admission even when a refresh's
checkpoint fails. Deterministic fixture barriers change actual Safe/relayer
nonces and a later runtime during proof; a canceled refresh also cannot reuse an
earlier counted-send admission. Both heavy roots and three selected adjacent
roots pass normal/race; both causal controls reproduce their intended failure
in both modes, with source/dependency evidence sealed. The production history
capability remains absent. Preserve read-only
historical reconciliation and test missing,
swapped, incomplete and malicious history refusals. This is an open MG-08
implementation gate, separate from independent build review and signer cutover.

Complete current authority and historical provenance are distinct properties.
The separate `aa9f715b` [current-authority proposal](SAFE-CURRENT-AUTHORITY-PROPOSAL.md)
implements a complete native account-storage-prefix verifier, exact runtime and
published proxy/singleton code/metadata binding, and strict Safe storage layout.
It rejects omitted intersecting branches, genuine orphan owner/module mappings
and all extra words. It preserves one canonical finalized hash as later heads
advance. The [qualification note](evidence/safe-current-storage-qualification-20260930.md)
records eleven new and ten adjacent roots passing normal/race, all ten normal
and exactly five selected race controls causal, with the complete source/module
and local-dependency evidence sealed. This supplies read-only observations only.
A current snapshot does not prove clean past initialization/delegatecalls or a
complete pending overlay. The reviewed native RPC exposes neither a pending
proof root nor an atomic multi-read token; final known-word/code rechecks retain
that explicit limitation and cannot authorize a send.

**Current-policy custody — scoped qualification complete.** The separate `3f88a948`
[custody increment](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CUSTODY.md) implements an
immutable independently signed acceptance journal under the existing exclusive
owner, exact counted/outcome references and partial-publication recovery. Both
proposal and acceptance signatures are required. Runtime-prefix order and
interrupted terminal events constrain authority import; incomplete policy stages
block reservations and cross-imports. Original history statements, runtime
predecessors, receipts, signed bytes, nonces, attempts and liabilities remain
retained. Its [qualification note](evidence/safe-current-custody-qualification-20260930.md)
records eight new and thirty-one adjacent roots passing normal/race, all eight
normal and exactly five selected race controls causal, and the sealed exact
source/module/local-dependency evidence. That isolated custody increment did not
enable public policy import or submission; read-only import is supplied by the
separately qualified capability below.

**Current-policy approval and public release route.** The concrete native proof
path is independently qualified below. The October 1 v2 interface adds a separate
public route while actual production risk-policy acceptance remains a P0 gate.
The route requires
the complete independently signed proposal and acceptance, retain expensive proof
before final scoped pending re-admission, preserve the exact proof snapshot
separately from a later admission head, and keep the one retained exact-byte send.
The current-only policy must not reinterpret the existing signed complete-history
attestation or claim current proof establishes historical truth.

**Native current-policy capability — scoped qualification complete.** Frozen
`95a905d4` [implements the distinct native route](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md)
on the signed custody journal. That September 30 source kept public acceptance-file
import read-only and public submission closed. Its internal route requires both
independent signatures and the exact
completed runtime tip, refuses mixing with the history capability, proves the
complete finalized Safe prefix, then rechecks scoped pending Safe/relayer state,
nonce, funding and exact transaction identity after expensive proof/artifact work.
It preserves the original proof snapshot separately from the final admission
head and repeats complete observation after durable reservation. Its
[qualification note](evidence/safe-current-capability-qualification-20260930.md)
records two new and thirty-five adjacent roots passing normal/race, eight normal
and exactly five selected race controls causal, and sealed source/module/local
dependency evidence. Two original normal oracle mismatches remain unresolved;
fresh reproductions under the corrected assertions are sealed separately without
changing source, tests or mutation patches. The capability does not prove
historical initialization/delegatecalls, independent finality or complete pending
storage.
Explicit production acceptance of those current-only assumptions remains an open
P0 gate. No mainnet action is authorized by local qualification.

**Public current-only acceptance v2, October 1.** The separate
[public route](BOOTSTRAP-SUCCESSOR-SAFE-CURRENT-CAPABILITY.md#explicit-public-v2-acceptance)
requires the original independent reviewer's new v2 signature and an exact
`--accept-safe-current-policy` acceptance object hash on every submit invocation.
V1 and proposal signatures cannot grant that authority. The signed policy names
owned-RPC finality, non-atomic pending assertions, exclusive signer/relayer cutover,
absent complete history/pending proof and between-read changes. Exact original
custody, complete runtime authority, native prefix proof and final scoped
readmission remain mandatory. Missing/wrong acceptance, incomplete publication,
runtime drift and mixed history capabilities refuse. Timeout and restart retain
the same signed bytes, nonce claims, counted attempts and maximum liability;
historical outcomes keep their original policy even after later imports.
The [qualification receipt](evidence/safe-current-public-qualification-20261001.md)
seals frozen `f3141591`: all 68 author-selected roots pass normal/race, three causal
control pairs reproduce their intended failures, and vet passes. Independent
review passes all 14 focused roots normal/race and vet with no blocker on its
separately pinned server graph. The 79-file manifest verifies. This implementation supplies
no production acceptance, signer or live transaction; MG-08 remains blocked for
actual authority, installation and activation.

**Contract-role declaration admission — scoped qualification complete.** The separate
[offline contract-role plan](BOOTSTRAP-CONTRACT-ROLES.md) addresses a cross-component
gap: two signed UR configs can agree with each other while targeting a foreign
vault/coordinator or the implementation instead of the approved proxy. The new
admission reconstructs the eight approved projections, binds both configs to the
exact proxy/vault/initial policy identifier and retains the evidence domain.
It preserves original v1/v2/v3 custody and does not grant live readiness. Sol
qualified corrected source `f4470d0d`: eight focused and twelve adjacent roots
passed normal/race (40 positive executions), and all three causal controls
reached their assigned assertions in both modes. The
[receipt](evidence/bootstrap-contract-role-qualification-20260930.md) retains
the original `43dcd01f` fixture failure separately; production guards were
unchanged by the fixture correction.

**Original contract receipt admission — scoped qualification complete.** The
[read-only historical increment](BOOTSTRAP-CONTRACT-RECEIPTS.md) borrows exact
original preparation and eight completed action records, reauthenticates their
canonical native/EVM receipts and historical postconditions, and checks each
signed EVM scan floor against the earliest original inclusion. Initial snapshot
and final checked-through head remain separate; ordinary advancement is accepted
while changed original inclusions fail. Original custody, attempts and pending
phases remain unchanged. Sol qualified frozen `c6b31fdb`: five focused and sixteen
adjacent roots passed normal/race (42 positive executions), all five normal
controls and exactly three selected light race controls were causal. The sealed
[receipt](evidence/bootstrap-contract-receipts-qualification-20260930.md) retains
raw results and source/dependency fences. Current installation, complete indexing,
evidence anchor and activation remain explicitly unverified.

**Current bootstrap contract fields — scoped qualification complete.** The
[five-account current-state increment](BOOTSTRAP-CONTRACT-CURRENT.md) checks the
original proxy implementation/owner/policy, reserve/vault links and evidence
domain at one finalized mapping after original receipt admission. It lists exact
getters/slots and labels their results owned-RPC assertions. Later-head continuity
preserves the observation block. Sol qualified frozen `2b87b133`: three focused
and sixteen adjacent roots passed normal/race (38 positive executions), all six
normal controls and exactly two selected light race controls were causal. The
sealed [receipt](evidence/bootstrap-contract-current-qualification-20260930.md)
retains raw results and source/dependency fences. Original zero-activity/policy
requirements remain strict. Complete storage, absent hidden mappings, governance
history and the evidence anchor remain unverified; no installation, activation,
public-route or live-action gate closes.

**P0 follow-up — Canonical installation-to-service admission.** Wire the verified
contract-role relationship into a separately qualified activation boundary that
authenticates the original CREATE and anchor receipts, validates each declared
deployment scan floor against actual history, rechecks current code/getters and
the anchored evidence journal, and admits both UR roles/operators plus the
separate root service. A declaration-only report is not production authority.
Safe current-policy approval/public-route installation and live chain identity
remain independent gates; this increment authorizes no mainnet action.

**Additive canonical runtime authority — scoped qualification complete.** The
`3d526830` [runtime revision increment](BOOTSTRAP-SUCCESSOR-RUNTIME-REVISIONS.md)
implements independently signed artifact additions while retaining the immutable
base, predecessor chain, original receipts, exact signed transaction, both nonce
claims, counted attempts and full liabilities. Hash-bound partial stages cannot
switch approvals; interrupted counted reservations remain consumed, and pending
terminal intents must finish before importing new authority. New event references
are monotonic while omitted references preserve old v1 bytes and seals. Historical
reads pass only their independently approved inclusion/parent pair to CRv4, with
no ten-profile lifecycle cap. The full local fixture retains twelve revisions.
The [qualification note](evidence/bootstrap-successor-runtime-qualification-20260930.md)
records ten new and thirty-six adjacent roots passing normal/race, twelve normal
and exactly seven selected light race controls causal, and the final sealed
source/dependency evidence. The CRv4 top-level census correction preserves its
original passing raw streams and false-failure harness ledger. Earlier `a7186754`
and `cd4261a8` receipts remain immutable and scoped to their own source.

**RT-04 signed continuity policy — inspection boundary qualified.** The
[production-aligned proposal](RUNTIME-CONTINUITY-POLICY.md) adds an independently
signed envelope bound to the original schema-3 validator authority and a separate
semantic-verifier certificate interface. The read-only inspector checks exact
candidate artifacts, consumed metadata/APIs, hashed headers, finite windows and
closing canonical continuity while preserving original config and signing views.
Certificates bind source/build evidence, verifier executable/rules and all consumed
execution/economic domains. A signature authenticates the assertion; no genuine
semantic verifier or independently replayed equivalence proof is supplied. Fresh
production selection stays exact and closed to unapproved successors. Automatic
selection/retention, original pending-byte custody and broader role/miner/bootstrap
adoption still require their own implementation and qualification. This increment
does not close MG-04 or RT-04 and performs no live chain action. The
[scoped qualification record](evidence/runtime-continuity-policy-qualification-20260930.md)
records fifty positive root executions, seven normal and three selected race causal
controls on the corrected exact source. The original typed-reply fixture failure
is retained separately; passing inspection never installs production authority.

**RT-04 finite executable replay — boundary qualified.** A separate
[offline SDK executor and bounded process owner](RUNTIME-SEMANTIC-REPLAY.md)
bind exact original/candidate Wasm, signed rules, evidence and executable identity.
On-chain transition cases compare complete declared storage effects, including
insertion/deletion, alongside return bytes. Explicit host/storage budgets and
joined cancellation qualify only this finite execution boundary. Real approved
source/build/state inputs, all-domain semantic proof and durable production
selection remain P0; this increment does not complete automatic compatibility.
[Independent qualification](evidence/runtime-semantic-replay-qualification-20260930.md)
records 48 Go positive executions normal/race, 19 Rust tests normally and eight
normal/four selected Go race causal controls. Rust has no race qualification.

**P0 follow-up — Automatic compatible runtime admission (RT-04).** A new runtime
still needs independently reviewed code/metadata and a signed revision. This
incremental authority path is not automatic runtime compatibility. Specify and
independently qualify a separately approved compatible-change policy and verifier
before claiming unattended routine upgrades. Preserve the complete historical
authority chain and all original custody, with no node self-approval or
provisional-runtime fallback. Same-version changed artifacts and unsupported
codecs remain closed explicit gates. Genuine Safe history authentication and the
public-submit gate remain unchanged; MG-08 stays open.
Later successors, filesystem migration, fee replacement and independently proved
external sends require separate approved liability-preserving transitions.

MG-07 now includes the qualified standard-validator
[service-progress producer](SERVICE-PROGRESS.md): 47 selected roots pass normal
and race, with four causal controls and the integrated authority-projection
check. Actual custody/settlement owners supply its facts; an isolated exporter
keeps heartbeat, useful progress and confirmed publication separate. Native
and steering hooks are covered by the separate
[continuation qualification](evidence/production-continuation-candidate-20260928.md).
The [read-only service consumer](SERVICE-MONITOR.md) is also implemented and
[qualified](evidence/service-monitor-qualification-20260928.md): 48 selected
roots pass normal/race, five controls reproduce their required failures in both
modes, and service plus inherited alert-rule fixtures pass. Each validator
role retains its own source checks, evidence ages and output files while the
chain worker retries independently. Application/reveal and settlement deadline
inference, other domain coverage, delivered alerts and the repair controller remain
open; source qualification does not establish live monitoring.

The MG-07 [read incident continuity increment](READ-INCIDENTS.md) now carries
stable outage IDs, first/latest failures, successful-read recovery and recurrence
through per-role checkpoint restart. Legacy history remains explicitly unknown;
recovery does not attest to service health or authorize repair/spend. The
[qualified receipt](evidence/read-incident-continuity-qualification-20260929.md)
records all 82 affected roots and six causal controls passing their required
normal/race outcomes at integrated SN `1bb311fc`. Complete incident retention,
compatible rollout and actual alert delivery remain open.

The MG-07 [steering responsiveness increment](SERVICE-MONITOR.md#steering-responsiveness)
closes the fresh-publisher/blocked-loop detection gap with explicit per-role
margins, a distinct durable incident and critical alert. Detection requires a
previously observed responsive steering instance; fresh read/receipt/reveal waits
do not imply a hang or protocol success. Restart, source loss, publisher restart
and policy removal preserve unresolved incidents until a real later loop outcome.
The [qualification receipt](evidence/steering-liveness-qualification-20261001.md)
retains source tests and controls. Checkpoint v4 preserves legacy history and
independently signed v3 stopped-validator repair scope. This diagnostic performs
no service mutation or signing.

The separate MG-07 [active steering-hang repair](ACTIVE-VALIDATOR-REPAIR.md) now
has a reviewable independent incident/role/release/host/generation envelope,
one durable stop, bounded descendant join and one durable start. Permanent
generation claims reject a second signed envelope or journal path; shared unit
ownership includes stopped repair and activation. Current healthy/unknown
steering, changed policy/source and expired recovery headroom refuse a stop,
including after fsync. Unknown starts never retry. The
[source receipt](evidence/active-validator-repair-qualification-20261001.md)
records deterministic effect and crash boundaries. Production SLOs and envelopes,
exclusive host/signer custody, real-systemd qualification, anti-rollback policy,
deployment/alert delivery and root/operator recovery remain P0 gates. No live
service was installed, stopped or started.

The MG-03 [qualified bounded native finality capture](evidence/operator-native-finality-capture-qualification-20260929.md)
is integrated at server `5ff7bf02`. Its retained request/byte reservations,
partial native headers/certificates and offline completed replay preserve the
original receipt collection through exact-boundary or bounded-descendant proof
capture. All 112 affected roots pass normal/race, including 23 new roots and
three private-database roots; seven causal controls discriminate in both modes.
Vet and source/module fences pass. This qualifies capture source only; archive
capability, independent checkpoint/genesis/runtime admission, native account
and fee proofs, production service adoption, composed release and live custody
remain open, and actual fees remain null.

The MG-03 [qualified bounded native StorageProof verifier](evidence/operator-native-storage-proof-qualification-20260929.md)
is integrated at server `6201504e`. It derives the original collection-boundary
state root by replaying receipt/finality proofs, then checks raw storage proofs
against it. All 16 focused and six adjacent roots pass normal/race with package
exit zero; all 21 causal controls discriminate in both modes. The pinned SDK
oracle supplies 18 exact vectors, and the two prior fixture/control anomalies
remain separate. Its API covers the collection boundary; the qualified historical
interface below adds selected receipt/parent reads. Runtime decoding, checkpoint
approval, proof capture, fee attribution, owner-window/global-
custody authority and service adoption remain open, with actual fees null.

The MG-03/PF-03 [qualified historical receipt/native fee-context source](evidence/operator-native-fee-context-qualification-20260929.md)
at server `41527380` passes all ten new and 122 affected roots normal/race,
including three disposable-database roots; all six causal controls discriminate
in both modes. It is integrated after storage as server `1bccc3cd`; a separate
composed smoke passes 30 of 138 available roots per normal/race mode. It preserves
all signed history while deriving exact receipt-native
child/parent contexts; missing or ambiguous coverage remains unresolved. Actual
fees remain null and source-profile mapping grants no payer/runtime authority.
The original native StorageProof interface covers the collection-boundary root;
the qualified historical interface below adds selected child/parent reads.
Runtime-qualified withdrawal/refund attribution remains a separate dependency.

The MG-03/PF-03 [qualified historical native StorageProof API](evidence/operator-historical-native-storage-qualification-20260929.md)
is integrated at server `80c0e1b7`. Fresh receipt/native context replay derives
the exact selected parent
execution or child post-state root. A unique checkpoint child can prove raw reads
while its parent stays unavailable and nested fee coverage stays incomplete;
that child cannot become an execution-parent substitute. All 20 new and 30
adjacent roots pass normal/race with package exit zero; all 21 causal controls
discriminate in both modes. This 50-root scope does not claim whole-package
coverage. Runtime decoding, payer/fee attribution,
owner-window/global-custody authority and live action remain absent; fees are null.

The optional [native submission deadline observer](NATIVE-DEADLINES.md) now
wires explicit per-role completion margins into the actual service worker.
It distinguishes schedule forecasts and unavailable reads from a completed
receipt-pending report after the original intent's native epoch. First/latest
missed-window evidence survives restart, renewal and late application reports.
It has no success or incident-clear authority. The integrated
[qualification](evidence/native-deadline-qualification-20260928.md) passes all
35 selected roots normally and with race detection, five causal controls in
both modes, and both offline alert suites. Production margins, deployed
collection, alert delivery and authoritative incident resolution remain pending.

The integrated [bounded diagnostic exporter](evidence/bounded-diagnostic-output-qualification-20260928.md)
now isolates validator startup/steering/runtime diagnostics and chain/service-monitor work from stalled log
destinations. Each role has finite queue capacity; one joined destination owner
reports completed writes, dropped records and unavailable output independently
of protocol progress. All 76 affected roots have passing normal/race coverage,
six causal controls reproduce the intended failures in both modes, and vet plus
three offline alert-rule suites pass. The receipt preserves the original
maximum-ID fixture failure and its isolated correction. Consumer-first rollout,
actual alert delivery and other operational domains remain separate work.

The [root-service/root-monitor follow-up](evidence/root-output-qualification-20260928.md)
is also integrated and component qualified. All 41 affected roots have passing
normal/race coverage; six causal families, mainnet vet and seven offline alert
rules pass. The receipt retains the refused-sink assertion correction, incomplete
panicking control capture and exact corrected reuse. Root-monitor's compact
event v2 needs a consumer-first rollout; finite preview v1 retains the complete
census. Optional metrics failure preserves independent observations, while
original custody errors remain hard. Live root activation, deployed collection,
alert delivery and the repair controller remain open.

**Trail-worker output correction (September 28; integrated and component qualified).**
The actual `TrailEngine.Run` loop and proof-signature warning now use the same
bounded exporter, with closed scalar facts and explicit operator/epoch identity.
All 17 affected roots have passing normal/race coverage, and five causal controls
reproduce their intended failures in both modes
([evidence](evidence/trail-diagnostic-output-qualification-20260928.md)). The
physical full-pipe test proves actual trail progress and cancellation before
stdout drains. The receipt retains the original poisoned-ledger cleanup failure
and exact fixture correction. Required custody failures still stop the affected
work. SDK/internal logging, registration composition and live
delivery remain separate work; component coverage is not universal output isolation.

**Miner output and shutdown (September 28; integrated and component qualified).**
The actual provide owner now separates required authentication/file callbacks
from blocked stdout, joins admitted HTTP status handlers, and retains panic
cleanup causes. All 18 affected miner roots and the actual unchanged Warp status
reader pass normal/race checks; four causal controls reproduce their intended
failures ([evidence](evidence/miner-diagnostic-output-qualification-20260928.md)).
Optional versioned status counters report output delivery, while `status: ok`
remains process liveness. Consumer rollout, readiness and alert delivery remain
open. The [diagnostic cause isolation correction](evidence/diagnostic-cause-isolation-qualification-20260928.md)
also passed all 20 affected roots and five causal controls in both modes and
is integrated. PH-15 records its scope; the completed output tests remain retained.

**Versioned registration grammar (September 28; component qualified).** Server
candidate `736d7b8f` rejects ambiguous request-field aliases and duplicate or
invalid values before allocation. Its focused test passes normally and with
race detection; the exact old parser reproduces the expected failure in both
modes ([evidence](evidence/registration-request-grammar-qualification-20260928.md)).
The [underlying transaction qualification](evidence/registration-server-model-qualification-20260928.md)
now has 29 passing affected roots normally and under race detection. Its full
model execution finished with 1,118 passes, seven disclosed configuration/data
skips and no test failures. The retained checker rejection for undeclared legacy
subtests is separate from the successful original package exit. The
[SDK/Connect transport checks](evidence/registration-request-transport-qualification-20260928.md)
and [actual SDK-to-production-API/database checks](evidence/registration-production-api-qualification-20260928.md)
now pass normally and under race detection, including their causal controls.
The [final combined SN consumer check](evidence/registration-diagnostics-composed-qualification-20260928.md)
now passes all seven selected roots normally and under race detection on one
sealed registration/diagnostics source graph; all 18 maintained stages and the
independent after-fences passed. Server-first migration/rollout remains a
separate gate; these source results do not establish a deployed release.

The [concurrent-allocation fixture correction](evidence/registration-allocation-attempts-qualification-20260928.md)
also passes normally and with race detection, with the original isolation
control reproducing its expected failure in both modes. The fixture counts
allocation attempts across real transaction rollback/retry; final identity
equality alone had hidden the extra work. The original ineffective control is
retained. This test-only correction preserves the completed full-model capture
and does not qualify the newer composed client release.

**MG-04/MG-08 direct production cadence (September 28; source integrated,
component checks pass).** The [bounded representability fix](evidence/mainnet-steady-cadence-candidate-20260928.md)
permits zero accelerated epochs only for mainnet with four identical
initial/production windows and a 50,400-block period. It preserves every existing
positive-count transition, historical mainnet approval, and checked-in testnet
policy hash. Shared JSON schema tooling can now express that mode; the shared Go
validator enforces exact cross-field equality. Both current-steering and historical
decision snapshot consumers remain unchanged and compare actual epoch-zero RPC
bytes against an independently signed public production config in the new tests.
The installer must derive all initial policy fields from its complete approved
body and require effective epoch zero. Eleven protocol and thirteen validator
roots pass normal/race, and vet passes. Three causal controls each reproduce
their intended assertion in both modes. Composed installer acceptance remains
pending; the receipt distinguishes the tested dependencies from the future
release and retains one missing causal-wrapper exit. This is a source-only
correction; no mainnet configuration or deployment changed.

The September 30 [current production release builder](RELEASE-BUILD.md) preserves
seventeen commands, five production contracts and eight image contexts. The
original complete census found nine server build failures caused by stale local
SDK/Connect overrides; server `898dc8f3` fixes the exact module graph, and
independent Sol qualification rebuilds all thirteen server commands and passes
three source-graph roots in normal/race modes plus vet. The four SN commands
also compile in the author's census. The subsequent
[fresh-catalogue qualification](evidence/fresh-contract-catalogue-build-20260930.md)
records all seventeen binaries built in one sealed candidate, ten exact contract
byte pairs and unchanged deployment interfaces. Independent Sol qualification
passes all 31 builder roots normal/race, vet, four causal controls and all 170
artifact readbacks. Executable compile results remain separate from application
behavioral qualification.
The [bounded scratch-image qualification](evidence/scratch-image-qualification-20261001.md)
adds a repeatable Linux/amd64 OCI build and independent binary readback for
`server-competitionworker` from that exact candidate. Both author and Sol
independently produced the same archive; 51 builder roots pass normal/race.
The [seven-service qualification](evidence/seven-service-image-qualification-20261001.md)
closes the local Ubuntu base and remote `ADD` inputs with exact pinned offline
sources. Sol independently rebuilt all seven remaining images byte-identically,
verified their OCI/rootfs/binary content and passed 73 focused roots normally
and with race detection, vet and five causal controls. Aggregate release and
deployment flags remain false.
The [offline image aggregation](evidence/release-image-aggregate-qualification-20261001.md)
now verifies one original source manifest plus both supplements and emits a
separate complete eight-image attestation. It rehashes all 337 artifacts of the
frozen SN `2d53e6f2` / server `ecbf3aad` candidate, checks binary source/module
metadata, and replays all OCI/rootfs and source-input joins. Only local
`source_to_image_verified` becomes true; the original three receipts are
unchanged. Reproducibility, release completion and deployment approval stay
false. This closes the receipt-composition gap, while MG-02 remains open for
the independent builder, archive/restore, runtime/configuration and policy gates.
The retained-mode manifest keeps historical and compiled contract hashes:
Coordinator and ValidatorEvidence have metadata drift associated with the changed
imported SettlementVault source, so that selection's source-to-bytecode equality
stays false. Explicit `contract_catalog: "fresh"` now exports exact compiler
creation/runtime bytes for all five contracts to a separate schema-1 catalogue,
retains the old catalogue/history and checks unchanged ABI, constructors, layout
and semantic immutable references. Selected hashes are separate from historical
hashes; fresh mode refuses nonexact bytecode and retained mode refuses silent
replacement.

No signed mainnet plan has been evidenced; the existing release/testnet catalogue
does not establish a mainnet commitment. Independent qualification of fresh
selection is the preferred path for the first unsigned mainnet plan, consumed by
its exact file path and SHA256. Checking for externally held signed commitments
remains a launch gate; preserve and reconcile any such commitments before
changing selection. A metadata-equivalence exception remains a conditional
fallback and is not selected. Local OCI digest/readback and independent rebuild
are complete for the frozen candidate. Published/deployed image identity,
compiler installation/config/policy qualification and release approval remain
open; the historical v11 inventory does not attest this current composition.

The newer server source branch `codex/mainnet-composed-hardening-20260927` at
`b6f49bdb` includes migration 728, the operator receipt-census correction,
atomic payer admission and checked settlement arithmetic on the v11 server
base. Its earlier `7bf88d79` combined controller selector passed normal, race
and vet on disposable PostgreSQL/Redis
([evidence](evidence/server-composed-hardening-20260927.md)). This source has no refreshed complete
release lock, binary/image inventory or deployment approval; the v11 inventory
does not attest these changes. MG-02, MG-03 and MG-06 remain open.

The first broad server-model run under
`/mnt/data/sn-testnet/evidence/mainnet-server-model-full-20260927` ended on its
90-minute deadline with **1,048 passed, eight failed and seven skipped roots**.
It is [incomplete diagnostic collection](evidence/server-model-diagnostic-20260928.md),
not frozen composed-release qualification. Its
launcher entered the original server directory, whose relative Go replacements
resolved active sibling checkouts rather than the prepared pinned workspace.
The actual executable and provenance correction are retained alongside that
directory's `PROVENANCE.md`; disposable-service cleanup completed successfully.
For MG-02/MG-10, validate the compiler's actual `go list -m -json all` module
directories, physical targets, exact revisions and clean state before and after
qualification. Use real isolated worktrees for every local replacement and
retain the compiled executable. A prepared workspace or an intended source lock
does not establish which dependency bytes the compiler used.

The corrected full model body finished separately under
`/mnt/data/sn-testnet/evidence/server-model-final-20260927`, with SN `615a7675`
and server `4468a696` in real frozen worktrees. The guard checks the actual
resolved module graph before compilation and after completion; the runner
retains model/controller/handler executables and build metadata. Its complete
census is **1,108 passed, three failed and seven skipped; package fail**, with
no missing roots. This is not an accepted composed release. The
[completion receipt](evidence/server-model-completion-20260928.md) records
terminal evidence, qualified corrections and capture limitations. Server
`4468a696` corrects the
historical payment, retention and probe fixtures without changing production
guards or scheduling; 18 affected roots passed normal and race qualification
([receipt](https://github.com/urnetwork/server/blob/4468a6961c00cf0ff8b84986259fa9698a7a8441/local/model-fixture-qualification-20260927.md)).
Its three remaining fixture failures have separate normal/race qualification
on the integrated corrections; the original full-body result remains retained.

All eight asserted diagnostic failures now have bounded fixture corrections in
the composed server branch through `936c3d9563372e8f424d516ee2dd3525555206de`.
The additional egress chronology selection passes 15 roots normally and with
race detection. The final two retention fixtures and six custody guards pass
both modes; old-fixture causal controls fail at the intended assertions.
[Integration evidence](evidence/server-model-fixture-integration-20260928.md)
records the exact commits and receipts. These changes preserve production
guards and probe policy; they do not turn the earlier deadline failure or the
completed frozen body's failing package result into a passing full invocation.

The frozen qualification's first direct-binary invocation used the module
directory instead of the test package directory. Its 15-test prefix is retained
and disqualified; the corrected body under `server-model-final-20260927/model-run`
reuses the same compiled binaries from `server/model`. Source and service cleanup
checks passed before that correction. Terminating the old `go tool test2json`
wrapper also returned zero without a complete run: exit status alone is not
qualification. Require all **1,118 roots listed by the actual Linux binary**
and the terminal package outcome, recording skips separately. The source tree's
1,122 function declarations are not the executed census. For future captures,
use the maintained [qualification owner](../scripts/qualification/main.go),
which already checks actual package/module paths, executes from the package
directory and verifies terminal membership. The retained body finished
naturally, so there is no missing prefix to restart. Its original shell wrapper
was lost; terminal events, source/binary checks and independently verified
disposable-service cleanup are retained without inventing a shell exit code.
The maintained runner also requires physical Go tool paths. A later focused
capture was refused before compilation because the default host cache path
traversed a symlink. Use the explicit `/mnt/data` cache and temporary-directory
profile in [MAINNET.md](MAINNET.md#acceptance-evidence-and-implementation-qualification)
for new captures; retain that preflight failure separately from executed tests.

For MG-08, the majority SN25 validator runs the standard `sn/validator` binary
with its ordinary evidence-based scoring policy. It is an indirect reset aid,
not a native removal authority. Old miners absent from eligible head and pool
evidence receive no positive weight from our validator; old miners with valid
current evidence may still be weighted. The resulting weights may move some old
miners toward the bottom of the emission-ranked trim order after native
processing, but majority control is not a promise of zero weight. Exact finalized
emission rows, all other eligible neurons, immunity and protected roles must
be re-censused before proposing a capacity. Root-subnet validator weights do
not perform SN25 deregistration. [Runtime trim source](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/uids.rs#L171), [subnet weight source](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/weights.rs#L865).
The reviewed registration-allowed setter is chain-Root-only, beyond the owned
SN25 keys, and a coldkey-authorized hotkey swap can change an existing UID
despite a closed registration flag. Admission must account for both facts
through inclusion. [Registration setter](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs#L728), [swap implementation](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/swap/swap_hotkey.rs#L101).
The owner-trim execution gate can use a bounded safe-set proof instead of an
atomic hotkey predicate only when every generation removable through call
expiry is an approved old miner and every protected identity stays immune.
Prove registration and swap/custody fences, immunity expiry and capacity over
the complete mortal call window, including earlier same-block actions. A
closed registration flag alone does not rule out subnet-owner takeover during
an epoch, which can directly register a neuron; first hotkey swaps also have
no cooldown. Reconcile actual removals from the finalized receipt.
Majority-validator weights may change trim order but cannot prove this gate.
The signer-free [`owner-trim-qualify`](OWNER-TRIM-BOUNDED.md) now checks a
conservative no-epoch window from authenticated census, metadata and storage,
including immunity-through-expiry, registration, both swap paths, admin timing,
lease absence and capacity. It preserves every requested old generation and
conditional residual count. Favorable predicates leave owner/proxy/pending
actions, governance/runtime and public subnet-pruning/reuse fences explicitly
unproved; executable apply,
actual signed mortality, custody effects and actual-subset receipt/reconciliation
remain blocked. [Qualification evidence](evidence/owner-trim-bounded-20260927.md).

MG-08 now has a separate signer-free [root observation foundation](ROOT-VALIDATOR.md):
`root-preview` and finite `root-monitor` bind the approved mainnet domain and
runtime artifacts to a complete root seat census, mapping/generation/ownership,
stake/pruning risk, stored strategy and delegation evidence. Do not equate a
read-only `ready` sample with mainnet activation. The observer never loads a
signer, automatically re-registers a pruned seat or mistakes a fresh burn quote
for a transaction cap. Missing full custom-weight eligibility, historical
basket/delegation custody and the separate root signing/capability service remain
explicit blockers; UR validator readiness remains independent.

The [SN25 reset preview](SUBNET-CENSUS.md) supplies a bounded, finalized
forward/reverse UID census and exact generation-scoped trim comparison. It
retains all owner identities as protected even when runtime immunity is
narrower, and refuses a candidate claim when the trim-call metadata changes.
It does not infer an arbitrary-removal owner call or permit destructive apply
without an execution-time guard and complete custody/history reconciliation.

Contract installation has a narrow build margin: the [2026-09-27 candidate
size check](evidence/contract-size-candidate-20260927.md) measures
`STCoordinator` at 24,564 runtime bytes, 12 bytes below Foundry's 24,576-byte
limit. Bind that exact build into MG-02/MG-08 and qualify creation on the
selected live runtime; a passing local build does not establish live
deployability after a source/toolchain change.

The separate nine-action installation candidate has successful complete-command
normal/race diagnostics, but its [failure-test correction is blocked by automatic
review](evidence/contract-graph-review-block-20260928.md), citing possible
cybersecurity risk. The frozen selection has finished its bodies with 47 of 48
roots passing in each mode, retaining the failed Safe inner-outcome test and
its non-discriminating control. No corrected fixture,
complete graph qualification or production integration is claimed. MG-08 stays
open; unrelated recovery and monitoring work continues.

MG-02 still needs actual OCI image identities and running-image readback.
Clean server `9f860731` API and taskworker Linux/amd64 binaries built and
repeated byte-for-byte under their Makefile profile; the
[external build record](/mnt/data/sn-testnet/evidence/mainnet-server-binaries-20260927/RESULT.md)
retains both hashes. Adjacent review found six server Dockerfiles still naming
mutable `ubuntu:24.04` while API pinned its multi-platform digest. Server
branch `codex/mainnet-server-image-pins-20260927` commit `969d6c74` pins all
six to that existing digest; both operator binaries remained byte-identical
after the source-only change. The branch is pushed and server `969d6c74` is
now part of the clean v9 source lock. The v9 composition built fresh server
API/taskworker binaries against SN `265231f9` and inventories all eight server
Dockerfiles. No selected or published OCI image digest, approved production architecture set,
deployment manifest or running-image readback exists. A Dockerfile pin and
local binary do not supply an image.

The [v9 image probe](/mnt/data/sn-testnet/evidence/mainnet-images-v9-20260927/RESULT.md)
found that the pinned base still installed packages from moving Ubuntu
repositories. Server commit `a211d56c` closes that input drift for seven
service Dockerfiles: the [package lock and signed-index proof](/mnt/data/sn-testnet/evidence/mainnet-server-package-pins-20260927/RESULT.md)
bind 40 exact payloads for amd64/arm64, and the final install runs without
network access. The [independent API/proxy image probes](/mnt/data/sn-testnet/evidence/mainnet-package-pin-probe-20260927/RESULT.md)
built the amended amd64 recipes and extracted exact selected binaries. The
[v10 source and file inventory](evidence/release-candidate-v10-20260927.md)
includes the lock, payloads and signed-index inputs. Keep MG-02 open for an
owned package archive/restore, actual arm64 image qualification, selected and
published OCI manifests, full source-to-image provenance, production rollout
and running-image readback. A local image digest does not prove publication.
An [uncached API rebuild](/mnt/data/sn-testnet/evidence/mainnet-package-pin-probe-20260927/RESULT.md#forced-rebuild-result)
with identical source inputs produced a different OCI digest: package install
logs/cache embedded wall-clock data and hundreds of file timestamps changed.
This is a demonstrated reproducibility defect to fix before claiming a
byte-identical production image build.
Server `77cb401e` addresses that defect for the tested package closures:
it removes only the two volatile build outputs and makes the seven image
recipes supply a fixed epoch and timestamp-rewriting export. The
[no-cache comparison](/mnt/data/sn-testnet/evidence/mainnet-server-image-repro-20260927/RESULT.md)
repeated API and proxy runnable amd64 platform bytes exactly, while keeping
distinct run-specific provenance-bearing indexes. The [v11 candidate](evidence/release-candidate-v11-20260927.md)
binds the new source and all seven Makefiles. Keep the other five services,
arm64, archive/restore, builder identity, attestation policy and deployed-image
readback as open gates; this local comparison does not approve an image.

The local validator init fixture initially failed under host umask `0002`
because `testing.TempDir` supplied a group-writable numbered seed parent.
The [causal correction](/mnt/data/sn-testnet/evidence/mainnet-seed-fixtures-20260927/RESULT.md)
creates an explicit private fixture child and passes real init under umasks
`0000`, `0002`, `0022` and `0077`; 87 affected tests pass normally and under
race. Preserve the production refusal of unsafe seed parents. Production
service manifests must independently provision and verify private state and
seed directories; passing a fixture is not a deployment permission audit.

The MG-08 action core supplies an offline-qualified
[existing-seat action owner](ROOT-ACTION.md): mortal root basket encoding,
one-request signing/nonce ownership, private durable state, exact-byte retries,
and retained finalized dispatch/fee/runtime-deviation or expiry evidence. Missing
or empty required state cannot resurrect an allowance, and a runtime change
blocks new effects while old receipts remain recoverable. The new
[offline custody handoff](ROOT-OFFLINE-CUSTODY.md) authenticates independent
exact-action approval, durably imports one matching public native signature and
recovers the same bytes after interruption; local absence never proves that no
signature was issued. The [root service decision owner](ROOT-SERVICE.md) now
retains a bounded approved-basket decision and original native intent atomically,
owns finite joined supervision and separates observation, current authority,
custody and submission capabilities. Exact-block read-only weight observation
and pinned-runtime normalization supply necessary decision checks; they never
authorize effects. The separate [owned-RPC submission adapter](ROOT-SUBMISSION.md)
now authenticates its own action/route approval, retains numbered uncertain sends
and reconciles exact original bytes before any later approved attempt. The
composed offline-custody/service/HTTP/receipt path passes deterministic local
qualification. The [qualified bounded root-service command](evidence/root-service-runtime-qualification-20260930.md)
now admits original input and recovers issued signatures without another
signing or broadcast allowance: 12 focused and 100 adjacent roots pass
normal/race; five normal and two race controls are causal. Production live
authority, globally fenced native custody, a separate hardware signing device
and activation remain absent. Native fee
quotes are not atomic caps; source/policy hashes are not on-chain runtime locks.
Do not activate signing from a read-only-ready sample or invent a heartbeat for
the accumulation strategy. Mainnet identity, existing seat, complete eligibility,
approved custody and limits remain explicit gates.

The [executable contract bootstrap phase](BOOTSTRAP-CONTRACTS.md) now
prepares and resumes reserve CREATE through original public EVM signed bytes,
bounded durable attempts, shared owned-HTTP transport and canonical
runtime/getter recovery. The [reserve qualification](evidence/bootstrap-contract-qualification-20260928.md)
records passed normal/race scopes, causal controls and the subsequent composed
dependency check. The [second vault CREATE action](evidence/bootstrap-contract-vault-qualification-20260928.md)
is predecessor-bound, shares the signed graph's attempts and funding ceiling,
and passed 51 focused normal/race roots, 426 full normal roots and five causal
controls on its frozen source graph. The [third coordinator implementation
CREATE action](evidence/bootstrap-contract-coordinator-qualification-20260928.md)
binds both predecessors and authenticates the disabled-initializer storage; its
71 focused roots, split race shards, 446 full normal roots and five causal
controls passed. The [fourth escrow-registration
action](evidence/bootstrap-contract-escrow-qualification-20260928.md) binds
the derived vault and three completed predecessors; 95 selected normal/race
roots, 470 full normal roots and eight causal controls passed. The [fifth atomic
proxy CREATE action](evidence/bootstrap-contract-proxy-qualification-20260928.md)
passed 120 selected normal/race roots, 495 full normal roots and ten causal
controls for its initializer, storage and recovery guards. Healthy head advancement is
revalidated within the operation; an unavailable mapping read does not become a
successful-value mismatch. EVM signature liability survives local approval
expiry. The [sixth reserve-binding action](evidence/bootstrap-contract-reserve-link-qualification-20260928.md)
passed all 27 roots normally and under race detection, the 523-root full normal
package, ten adjacent low-gas roots in both modes, and four causal controls.
The [seventh vault-binding action](evidence/bootstrap-contract-vault-link-qualification-20260929.md)
passed its corrected checkpoint normally and all 29 roots under race detection,
plus six adjacent checkpoint roots in both modes and a selector causal control.
The other two installation actions, Safe inner-call success/getter
verification and authenticated live custody/network inputs remain open.
The [approval-preview/offline recovery follow-up](evidence/bootstrap-contract-preview-20260928.md)
adds a read-only unsigned CLI export of exact approval bytes and preserves
terminal receipts on offline reopen with an explicit retained-observation label.
Preview opens no journal or route; signed execution remains independently
approved. Its nine affected command roots and five causal cases pass normally
and with race detection; this is local qualification, not live installation.

The [offline chain composition receipt](evidence/bootstrap-chain-qualification-20260928.md)
adds 369 passing full normal roots, all 87 adjacent race roots across six disjoint
shards, and four fixed/mutant causal controls. All 49 source/module fence
comparisons passed. The original aggregate race timeout remains failed evidence.
The tested physical graph used Connect `358cefae`, server `0633780c` and SDK
`42241118`; it does not qualify the current composed Connect `b163f9dd`, server
`5dc11761`, SDK `516521fb` release graph. Offline preparation leaves every
chain, producer/service activation, custody approval and actual-economic gate
open; no live effect or full release qualification is claimed.

The subsequent [integrated source qualification](evidence/final-composed-source-qualification-20260928.md)
does cover the current Connect `b163f9dd`, server `5dc11761`, SDK `516521fb`
physical graph: all 379 full normal mainnet roots and 79 descendants passed,
and all 122 selected monitor/bootstrap race roots plus 79 descendants passed
across nine bounded shards. Source and module fences passed. The prior
component graph limitation remains in its own receipt; the newer result is
source qualification, not a release image, live activation or accepted mainnet.

The MG-08 v2 addition verifies two initial schema-3 production configs and
their domain-separated approvals offline, using the existing validator loader
rules. Independent role/signer/runtime/source/deployment pins and retained
protected generations must agree; declared custody namespaces remain separate.
The intended majority/secondary labels do not establish live stake, permits,
key possession or service identity. Existing v1 custody remains resumable at
its original unadmitted scope. The [v2 admission and shared-decoder qualification](evidence/ur-bootstrap-admission-qualification-20260928.md)
now passes all 24 joined stages, 588 root and 183 descendant executions on the
frozen composed graph. All six original causal captures retain their normal/race
named failures; R3 resumed all 24 stages without rerunning bodies and passed
input/source fences. The original unsorted-census refusal, global input-seal
refusal and R2 binary-mode refusal remain recorded. These bounded offline
results keep all live chain/service/economic and release gates open.

The September 29 MG-08 `bootstrap-chain readiness` source increment adds a
bounded read-only phase after accepted v3 preparation. It checks original child
journals under shared read-only locks, then observes both UR generations,
activity, permits and signed block windows plus the separate root seat and
mortal checkpoint at one finalized snapshot. Conflicting generations and
subnet scope remain per-role blockers; transport/runtime/integrity gaps return
explicit unresolved output without partial eligibility. It preserves exact
signed approvals, child allowances and all five pending chain phases. Its
[Sol qualification](evidence/bootstrap-readiness-qualification-20260929.md)
passes ten new normal/race roots and the exact 148-root adjacent normal/race
scope, with vet, unchanged source/module fences and four causal controls.
The root integrated exact qualified source `6627d15f`; the receipt records the
exact race union. Sol later intentionally terminated the redundant broad process
after its passing prefix; that process has no claimed successful terminal exit.
The [composed receipt](/mnt/data/sn-testnet/qualification/mg08-bootstrap-readiness-20260929/COMPOSED-RESULT.md)
passes focused 10/10 normal/race, vet and fences on SN `e35771ec` plus server
`b7c8c743`, without changing the earlier exact 148-root qualification.
Current authority, effective stake/eligibility, global custody and signing-device
fencing, contract completion, service activation and actual native 10/90
outcomes remain open gates.

The September 29 [qualified owner-trim action increment](evidence/owner-trim-null-storage-repair-20260929.md),
integrated at exact source `ce567305`, adds a separately approved coldkey action
and owned route, a fixed sixth journal
under original v3 custody locks, exact signature/nonce/era recovery, canonical
dispatch/fee receipts and before/after actual-subset correspondence with explicit
old-miner residuals. Transaction finality and a completed native reset are
separate outcomes; unavailable post-state admits only readback continuation.
Current runtime predicates include proxy absence, nonce, bounded selection,
protected generations and subnet immunity through expiry. Independent enforced
owner/governance/custody/provenance/exposure authority remains required; a
conditional pass never supplies it. Sol's 25/25 focused and 229/229 expanded
roots pass normal/race, with vet, unchanged source/module fences and eight
intended causal controls. The exact race union is complete; the redundant
unsharded bootstrap process was intentionally terminated after its passing
29-root prefix and has no claimed successful terminal exit. The failed R1
candidate retains 21/22 focused and 225/226 expanded results in both modes;
its valid-null proxy-reader bug was fixed in R2, not relabeled as a pass.
The best-effort risk-policy decision and its implementation remain pending.
The owned Snow read returned HTTP 502 at 06:59 UTC on September 29;
no current mainnet identity or live authority was established.

Start with MG-01's read-only route correction and MG-02's source composition.
Then close the recovery, runtime, policy, settlement and monitoring dependencies
before MG-08 can apply a reviewed bootstrap plan. MG-09 must close before
unattended operation. MG-10 separates pre-activation qualification from the
post-activation observations that can only be collected on a running mainnet.
Record implementation, exact qualification selector/results, deployed image,
independent observation, remaining action and evidence owner for every gate.
Historical testnet gates below are lessons and reusable evidence, not an active
instruction to restart the closed campaign.

## Implemented building blocks and missing production work

| Surface | Already present | Still required |
| --- | --- | --- |
| Native-chain authentication | [Exact artifacts](../crv4/runtime_identity.go), [reviewed catalog](../crv4/reviewed_runtime.go), [consumed-interface profile](../crv4/runtime_profile.go), [provisional admission](../crv4/runtime_compatibility.go), bounded per-connection metadata reuse and the [five-command miner fleet mainnet authority gate](../miner/FLEET-MAINNET-RUNTIME.md). | Supply independently approved mainnet genesis and runtime source/build/code/metadata pins; extend immutable operation views to every consumer, qualify semantic/economic compatibility, durable uncertain-send reconciliation and controlled upgrades. Provisional admission is explicitly testnet-only. |
| Recovery and service profile | Simulator journal/plan caches and retained warmup cited under PF-01/04; shared server [subnet-operator workload profile](../../server/taskworker/workload_profile.go) under PF-02. | Production package ownership, complete signature discovery, migration/readiness composition, deployed evidence and finite foreground latency. |
| Client-key rollover | Server `4b2c4587`, projection regression `9da52551`, successful non-accepting retained resume; composed current-main migrations 724–726 at `9f860731`. | Bind the composed release and coordinate live operator cutover with all consumers; prove current-domain readiness, retained pins and historical replay on the deployed image. |
| Usage and logs | Qualified epoch-658 usage quarantine `74893863`; immutable usage guard and append-only archive in server `9f860731`; scanner `df7ae2c4`. | Compatible writer/reader fleet and prospective complete settlement; independent streaming observability with durable byte coverage. Historical debt remains uncredited. |
| Economic-policy observation | Signer-free [finalized recycle-mode gate and cumulative integer reference](ECONOMIC-GATE.md), with 37 focused mainnet tests passing normally and with race detection. | Approved mainnet genesis/source-to-code pins, runtime-recognized owner census, authorized Recycle setting, weight mechanism and observed 10%/90% native outcome; the reference is not payout evidence. |
| Historical lifecycle provenance | Candidate `93949ee3` and `bbed208d` preserve original approved context, including cache reconstruction. | Integrate and qualify the actual recovery reader; include corruption, wrong-approval, changed-config and strict/current-use negative controls. |
| Production control plane | [Executable local root custody](BOOTSTRAP-ROOT.md), root service/owned submission owners, independently approved [standard validator 10/90 producer](OWNER-RECYCLE-PRODUCTION.md), contract/protocol packages and signer-free identity/census/economic observers. | Remaining executable mainnet chain bootstrap, production current-authority/device and protected registration, live approved 10/90 activation/history and observed native outcomes, independent domain monitor, authorized repair controller and deployed alerts/runbooks. |

**Server integration (MG-02/05/06; source qualified, 2026-09-27).** Server
branch `codex/mainnet-current-main-20260927` at `9f860731` carries all eleven
custody prerequisites on `origin/main` base `af17d1d2`, preserving current-main
settlement batching and provider probes. Migrations 724–726 retain policy
domains, immutable usage and the append-only archive; migration 727 adds durable
per-balance reservation revisions and Redis revision-fenced snapshots. The
v651→v724 migration-monitor namespace correction is included. The
[composed qualification](</mnt/data/sn-testnet/qualification/mainnet-current-main-20260927/RESULT.md>)
retains causal failures, exact source locks and 291 identical selected server
tests in normal and race modes with zero skips, plus SN/Connect integration and
cross-module compile. The server branch is pushed for review. This qualifies
source composition, not a live migration or production release.

The [offline composed-source lock and SN checks](/mnt/data/sn-testnet/evidence/mainnet-source-lock-20260927/RESULT.md)
then bind clean SN `c2d7685d`, this server candidate `9f860731`, compatible
Connect `c68689c4` and every local Go replacement without a `go.work`
override. Cross-module compile, selected SN normal tests and composed miner/
mainnet race tests passed. This is a candidate snapshot only: subsequent SN,
contract, generated-artifact or config changes require a new source/artifact
lock and affected qualification before MG-02 can close.

The independent [NetEscrow qualification](/mnt/data/sn-testnet/evidence/mainnet-netescrow-ordering-20260927/RESULT.md)
reproduces delayed-create/release and zero-byte preimage failures. The composed
tests cover those paths and current-main settlement amplification. Deployment
must stop and drain every additive cache writer, migrate the matching catalog,
upgrade every publisher together, verify actual database guards, and reconcile
before traffic. Persistent Redis fences and PostgreSQL revision tombstones need
explicit restoration and capacity policy. That original mirror-only correction
did not make admission atomic; the subsequent custody slice below moves the
admission authority into the database.
Live operator cutover, economics, reserve/claim conservation, capacity and the
complete production release gate remain open. The older `0633780c` branch and
its qualification remain as causal history, not the proposed current-main pin.

**Atomic payer admission (MG-06/PH-12; source-qualified, 2026-09-27).** Composed
server `b6f49bdb` includes admission `fbadd281` and its required arithmetic
successor `04e65680` (isolated commit identities). The
[causal qualification](/mnt/data/sn-testnet/evidence/mainnet-netescrow-admission-20260927/RESULT.md)
reproduces three failures on composed server `7bf88d79`: a missing create post,
two deliberately interleaved creators, and a missing settlement debit post can
all admit the same payer credit twice. Origin and companion creation now lock
eligible payer balances in stable ID order and read durable reservations in a
separate read-committed statement after the lock. The terminal settlement owner
debits consumed payer bytes in the same PostgreSQL commit as its outcome.
Cache restoration, lost posts, transaction rollback/retry and duplicate
settlement cannot authorize that credit twice. Migration history through 728,
signed usage attribution and zero-byte/zero-credit history remain unchanged.

The same review reproduced two settlement arithmetic bypasses: adding two
maximum signed reports wrapped the mean negative, and a negative legacy grant
could wrap the cumulative allocation into a false completed debit. Use the
overflow-safe floor mean and refuse negative consumed reports/grants before
claiming an outcome. Exact maximum-credit, split-grant, checked reservation
sum and debit-rollback tests retain the storage-limit boundaries. Negative
payer balances are already excluded by the database-generated active column;
that is an adjacent verified control, not another reproduced admission bug.

Qualification retains 123 distinct model normal passes across documented
slices and the same 123 passing together with race detection on the admission
source. The arithmetic successor passes 35 affected tests normally and with
race detection; five monitor checks pass both modes and final model/monitor
vet passes. Exact sources, causal failures, fixture corrections and separate
compiled-source boundaries are retained; this is not a claim that the full
server model suite or production capacity passed. Both disposable PG/Redis
pairs were removed after qualification. The composed server tree matches the
qualified final isolated tree exactly; release locks/artifacts must name the
new composed revision.

This is prospective source qualification, not a deployed financial repair.
Drain and replace every creator and asynchronous old debit writer together;
mixed versions cannot preserve this guarantee. Historical terminal outcomes
and settled escrow markers do not prove an old debit post ran: require exact
historical balance/debit reconciliation before activation, without guessed
charges or automatic post replay. Participant sweep publication, Redis account
payout increments and statistics remain asynchronous residual work. Keep their
durability/idempotence, complete settlement conservation, per-payer contention,
archive capacity and coordinated database/cache restore as open launch gates.
Source review of `b6f49bdb` found the next specific provider-payment gap:
the terminal settlement transaction can commit before its in-memory post writes
`transfer_escrow_sweep`. The payment planner reads those PostgreSQL sweep rows,
so losing the post can omit a provider payment and a duplicate terminal claim
does not create it. Require a causal lost-post/rollback/replay test and a durable
same-transaction or journaled correction before MG-06 closure. Redis summary
counters are a separate, lower-priority publication path.
An earlier automatic approval review rejected the implementation action for
this provider-payment correction, citing possible cybersecurity risk. No
durable correction or successful qualification is claimed for that open item.

**Retained timestamp custody (MG-06/PH-12; source-qualified, 2026-09-27).** Server
`6e2bcfa7` appends this correction to the v11 server candidate `77cb401e`. The previous
live/archive reader filtered both stores by `close_time`, silently omitting
historical terminal rows whose timestamp is NULL. That could produce a partial
or empty payout despite retained unresolved work. The corrected single-statement
reader probes at most one unknown-time identity per store and refuses the new
payout before operator chain reads or artifact construction. Migration 728
appends a restartable online partial index and exact schema admission; the
catalog through 727 remains byte-identical. Canceled/open work stays excluded,
paid/free attribution is unchanged, and archived NULL debt stays unresolved.
The [causal qualification](/mnt/data/sn-testnet/evidence/mainnet-usage-time-custody-20260927/RESULT.md)
records red/green live and archived failures, retention rollback/retry, the
operator barrier, 140 distinct selected tests passing normally and with race
detection across documented qualification slices, and vet. Index admission also
accepts the two exactly verified PostgreSQL dump/restore predicate renderings
without admitting a changed predicate or key order. This does not deploy the
fix, authorize a historical exclusion
or make the prior artifacts complete. The coordinated 728 reader/writer/reaper
cutover, exact historical NULL census, archive capacity and NetEscrow restore
gates remain required; no close time may be guessed to unblock them.

**RPC read admission (MG-04/MG-07; In progress, 2026-09-27).** The shared
[JSON validator](../protocol/json_unique.go) and bounded
[miner](../miner/sn_rpc.go)/[validator](../validator/chain_http_envelope.go) RPC
readers now reject duplicate keys, including equivalent escaped spellings,
case-folded Unicode aliases, trailing JSON and HTTP redirects. The miner
transient-read operation has a tested 90-second retry budget. Full miner normal
and race suites passed; all 1,762 validator tests passed in both modes with zero
skips, and the final Unicode admission change passed focused consumer normal and
race tests. MG-04 and MG-07 remain open for broader production runtime and
deployed-monitor qualification; this source result does not close MG-02.

The September 27 SN root includes newer per-user deposit and zero-price policy
interfaces (`9b386fe8`). Old R48 pricing and build receipts cannot qualify those
new semantics. Freeze the intended launch policy and matching server/SDK APIs
under MG-02/06; do not resolve an interface mismatch by silently changing an
already signed policy or relabeling old economic evidence.

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

The detailed RT/RL/PF tracker began on 2026-09-22. Its requirements and historical
qualification receipts remain below; the production gates above determine the
current execution order. Automatic handling of compatible Subtensor upgrades
and the wider [hardening requirements](#production-hardening-from-sim-testnet)
still need production integration and qualification.

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
Dated incident accounts below describe their state at the time. They do not
reopen testnet or override the September 27 closure and implementation inventory.

## Prelaunch fix tracker

| ID | Fix | Depends on | Owner | Status | Completion evidence |
| --- | --- | --- | --- | --- | --- |
| RT-01 | Immutable runtime views anchored to the correct block and purpose | — | Astra | In progress | The [validator-read correction](evidence/validator-read-capability-20260927.md) anchors stake and schedule capabilities to the caller-approved exact historical artifact before their first storage decode. The [qualified source-receipt views](evidence/validator-source-runtime-qualification-20260929.md) bind original preparation, execution parent and post-state independently, including an approved upgrade block. Wider historical execution and concurrent signing contexts remain open. |
| RT-02 | Shared capability profiles for calls, storage, signing, CRv4, custom APIs and precompiles | — | Astra | In progress | [Stake/schedule read profiles](evidence/validator-read-capability-20260927.md) admit an independently approved compatible successor without another compiled spec entry and identify changed consumed storage/API interfaces. Production admission policy and other operation profiles remain open. |
| RT-03 | Construct and sign each native operation from one view; reconcile stale or uncertain attempts | RT-01, RT-02 | Astra | In progress | The [source capability correction](evidence/source-runtime-capability-20260927.md) refuses a strict successor's stale preparation view before storage/nonce work and preserves retained signatures across cold reauthentication and metadata eviction. [Approved-upgrade receipt recovery](evidence/validator-source-runtime-qualification-20260929.md) preserves original signed bytes and historical authority without rebroadcast or new signing authority. Wider native operation coverage and uncertain-send reconciliation remain open. |
| RT-04 | Replace version-specific live admission in simulator, miner, both validator paths and bootstrap | RT-01, RT-02, RT-03 | Astra | In progress | The [standard production validator](OWNER-RECYCLE-PRODUCTION.md) accepts independently signed schema-3 authority through exact producer capability checks and measured source/intent binding. Complete original authority bundles now preserve old sidecars, pending bytes and the economic activation across compatible approved renewals. [Downstream upload admission](VALIDATOR-UPLOAD-RUNTIME.md) projects their exact historical runtime windows without producer authority. Schema-2 observation remains read-only. A concrete [independently signed compatibility policy and semantic-certificate inspector](RUNTIME-CONTINUITY-POLICY.md) has scoped qualification; it grants no signing view or automatic selection. [Finite offline replay](RUNTIME-SEMANTIC-REPLAY.md) qualifies supplied cases without claiming semantic-rules coverage. Other roles, arbitrary policy/custody transitions and automatic upgrade qualification remain open. |
| RT-05 | Separate observed runtime from deployment, configuration and approval identity | RT-02, RT-04 | Astra | Planned | A compatible upgrade preserves the plan, approvals, leases, completed actions and observed epochs. |
| RT-06 | Preserve historical proof reuse and make metadata-cache capacity independent of catalog length | RT-01, RT-02 | Astra | In progress | The CRv4 per-connection metadata cache has fixed resident capacity, least-recent-use eviction, and exact-hash uncached admission when every slot is loading. The [proof-ownership correction](evidence/runtime-proof-eviction-20260927.md) separates an authenticated provisional view's authority from metadata residency: eviction preserves retained signatures and exact historical reuse, while fresh identity checks and strict/foreign-owner rejection remain. Composed release and production compatibility qualification remain open; no testnet profile grants mainnet authority. |
| RT-07 | Suspend only operations affected by an unsupported change and expose an actionable reason | RT-02, RT-04 | Astra | Planned | Independent services continue where their dependencies permit; recovery resumes from saved progress. |
| RT-08 | Qualify upgrade handling and record the mainnet-readiness evidence | RT-01 through RT-07 | Sol medium | Planned | Affected normal/race tests and a controlled upgrade during an active integration campaign pass with unchanged approvals and reconciled transactions. |
| RL-01 | Bind launch attestation to an explicitly approved immutable release and its complete input manifest | — | Astra | Planned | Publishing documentation or advancing main does not invalidate an approved unchanged deployment; changed executable, source, policy, contracts or dependencies still require the appropriate new approval. |
| PF-01 | Reuse an immutable journal index and authenticated historical plans within one reconciliation | — | Astra | In progress | Deterministic work-count tests bound journal indexing by entries and plan authentication by distinct sources, while each receipt retains its own identity and postcondition checks. |
| PF-02 | Give simulator operator taskworkers an explicit workload profile, including retained queue handling | — | Astra | In progress | Required subnet/operator tasks run for both operators; excluded queued tasks and post hooks remain untouched; production defaults and restart behavior pass affected tests and managed startup. |
| PF-03 | Include every retained operator signature in recovery and renewal accounting | — | Astra | In progress | The [receipt-census correction](evidence/operator-recovery-census-20260927.md) keeps original/replacement/cancellation attempts recoverable after read failures. The [qualified source census](evidence/operator-signature-census-qualification-20260928.md) supplies status-independent discovery across selected operator databases and evidence stores, plus byte-preserving local archive/restoration. The [qualified conditional observation join](evidence/operator-receipt-fee-qualification-20260928.md) adds offline receipt/fee accounting. Their [complete MG03/R48 lineage](evidence/operator-mg03-r48-composition-20260929.md) was composed and qualified at `05fee56f`, preserving the [original integration receipt](evidence/operator-mg03-integration-20260928.md) as history. The [qualified commitment verifier](evidence/operator-receipt-commitments-qualification-20260929.md) is now integrated at `fbe0c039`: 52 roots pass normal/race and four causal controls expose forged gas/status, substituted signed bytes and disconnected ancestry. The [qualified bounded collector](evidence/operator-receipt-collector-qualification-20260929.md), integrated at `b7c8c743`, now produces those proofs from explicit owned-RPC reads and replays them before private create-only publication; all 71 affected roots pass normal/race. The [qualified native finality proof](evidence/operator-native-finality-proof-qualification-20260929.md) is integrated at `44636e5e`: all 89 roots pass normal/race and five causal controls pass for weighted certificates, scheduled set changes and exact native/EVM mapping relative to a separately pinned checkpoint. Actual fees remain null; independent checkpoint/genesis/runtime admission and finality/account-nonce authority remain absent. The documentation-only [pinned-runtime fee dependency review](https://github.com/urnetwork/server/blob/cfcbfcbaa13b4f4d298acfeca761a7252c18ddee/strecovery/ACTUAL-FEE-DEPENDENCIES.md) at server `cfcbfcba` identifies generic-event and failed-refund attribution gaps. Bounded authenticated native-state proofs plus qualified execution replay or fee-specific runtime evidence are needed; a live approved checkpoint is not required for offline proof development. The [qualified native finality capture](evidence/operator-native-finality-capture-qualification-20260929.md) at server `5ff7bf02` supplies bounded exact-boundary/descendant capture with preserved partial evidence and lifetime read budgets; all 112 affected roots pass normal/race with seven causal controls in both modes. Full closure still requires independent checkpoint admission, owned-node capability, authenticated native debit/refund and account nonce proofs, service adoption, composed release artifacts and live restart without duplicate actions or manual signature copying. |
| PF-04 | Diagnose validator warmup and support bounded, resumable semantic startup | — | Astra | In progress | Retained validators produce fresh proofs through both operators; startup exposes the pending criterion, uses a justified warmup budget, and preserves valid recovery progress without counting stale proofs as acceptance. |
| PF-05 | Give client-key histories an authenticated policy-scoped rollover | — | Astra | In progress | A scheduled policy change starts a new signed generation-1 segment for each client; old signed rows remain byte-identical and historically readable, current readers select only the active domain, and all miners regain processed-key readiness without bypassing it. |

PF-01, PF-02 and PF-04 have simulator implementation evidence; their remaining
work is production adoption and verification under MG-03/05. RT-01 through RT-08
form MG-04; RL-01 is part of MG-02. Link the bootstrap implementation and
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
   independently, and finish production verification for PF-01 and PF-02 on the
   selected release in the controlled rehearsal.
3. Complete RT-08 against the composed release, reusing unaffected results.
   Batch independent failures, fix their causes and adjacent paths, and rerun
   the affected checks. An interruption does not erase completed observations.

For each update, attach the implementation commit, affected checks, preserved
results, deployment evidence and next action to its stable ID. A new issue only
blocks operations that depend on it. Preserve testnet evidence as a closed source; qualify changed mainnet consumers
without restarting its acceptance campaign.

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
registration completes. That testnet resume retained its supervisor on the
bounded readiness timeout. The later server branch and retained resume receipt
above establish recovery progress without repeated setup; production closure
still requires the composed migration and readiness evidence.

The [2026-10-01 operator qualification](evidence/operator-policy-custody-qualification-20261001.md)
also corrects a separate payout rollover defect: deposit sizing compared the
previous epoch's signed artifact with the newly active configuration hash and
could infer its window using the new epoch length. The server now authenticates
the current deployment, reads immutable `policyAt(sourceEpoch)` and the actual
epoch window at that same canonical hash, and repeats the native/network and
canonical boundary witness. Retained artifacts keep their original signatures
and need no original local policy file. Fresh issuance requires the configured
policy to match the authenticated epoch; it cannot backdate a successor policy.
The causal two-operator fixture includes the populated namespace migration,
processed control, restart, fresh validator proof reads, partial/foreign cohort
refusal, tombstones, deletion and exact successor deposit/reserve sizing. This
local qualification supplies no live migration, signing or deployment approval;
both production operators still need the future-boundary cutover and readiness
evidence above. MG-06 financial-history reconciliation and participant-sweep
durability remain separate open gates.

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
Its production implementation is tracked by MG-02; the historical launch remains
an unchanged failed attempt.

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

**2026-09-27 bounded production correction.** The operator account reconciler
discarded a receipt-read error when no receipt remained pending. It then
interpreted an advanced finalized nonce as unknown consumption and marked all
signed attempts `superseded`; subsequent scans excluded the entire intent.
The [qualified correction](evidence/operator-recovery-census-20260927.md)
keeps an incomplete census unresolved. The adjacent finality wait also
requires a complete census before offering a fee replacement. A known
canonical candidate can still resolve the nonce after another candidate's read
fails. The 19 affected test roots pass normally and with the race detector;
package vet and formatting checks pass. No historical status is rewritten
automatically, no signature is copied or broadcast by this fix, and complete
cross-store discovery remains open.

Use synthetic fixtures to reproduce the missing-store failure and cover both
operators, stale database statuses, multiple signatures for one nonce,
cancellations, malformed or conflicting records, partial export, idempotent
restart and an uncertain submission. Require complete nonce coverage, unchanged
spend limits and no duplicate execution. The current qualified collector's
supported restoration path remains available during implementation; replacing
the automated production replacement remains open under MG-03.

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
progress. The closed testnet evidence supplies regressions; this document
describes the subsequent production implementation.

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
now separates schema-level signed-byte validation from its independently
authenticated source-call/signing capability. The
[selective-metagraph reader](../crv4/validator_stake.go) has the narrow read
profile below. Metadata format v14 alone does not establish the custom API's
return layout.

**2026-09-27 validator read capability (MG-04 / RT-01, RT-02).** The shared
stake reader previously rejected an independently approved exact artifact
solely because its spec was absent from a seven-version list. The
[correction and causal tests](evidence/validator-read-capability-20260927.md)
admit compatible successors through a block-bound read profile: exact consumed
storage types, hashers, defaults and prefix, plus the selective-metagraph API's
declared version and complete response decoding. Schedule reads additionally
require `SubnetEpochIndex` compatibility before reading any storage. Unrelated
calls, events and signed extensions do not revoke these read capabilities.
Exact historical pins and reviewed legacy adapters remain; the profile grants
no signing authority. Release configuration still requires separate production
admission, so this closes a redundant read gate without claiming RT-04 complete.

**2026-09-27 atomic source capability (MG-04 / RT-03, RT-04).** The
[source correction and causal controls](evidence/source-runtime-capability-20260927.md)
remove the next inner version-only refusal: an approved compatible successor
could not reconstruct its signed source bytes or construct its atomic call.
The persisted source schema now controls byte reconstruction; live use requires
an opaque exact-block artifact witness and matching selected calls plus complete
ordered signing extensions. The preparation block must match that witness;
retained-byte validation can use another authenticated block of the same
artifact without re-signing. Metadata eviction does not revoke the witness.
Unproved/foreign artifacts, incompatible calls/extensions, changed signature
domains and stripped provisional authority still fail at their actual admission
boundaries. This does not widen fleet or validator production configuration
approval or authorize new runtime tuples. The later [source-receipt
qualification](evidence/validator-source-runtime-qualification-20260929.md)
covers the standard validator's independently approved upgrade boundary;
wider native-operation and live upgrade qualification remain MG-04 work.

**2026-09-27 mainnet runtime observation (MG-04 / RT-04).** The
[outer admission correction](evidence/mainnet-runtime-observation-20260927.md)
accepts independently approved exact mainnet runtime identities through a
separate schema-2 config. Its bounded history pins each document's size/SHA,
revision, predecessor bytes, deployment/policy domain, source/review provenance
and finite native block interval. Successors append disjoint intervals;
spec-version ordering grants no authority. Reads check fresh native
name/genesis/EVM964, the approved route, canonical block/finality and the exact
artifact; config mutations and provisional connections fail. Existing producer,
bootstrap and archive loaders reject this observation config, and explicit key,
journal, startup-eligibility, signing and submission guards preserve that split.
This is a runtime identity observation API, not a storage/call compatibility
grant or a complete production successor policy. No actual mainnet approval,
identity or chain write was introduced; MG-04 and RT-04 remain open.

**2026-09-27 initial production successor (MG-04 / RT-04, MG-06).** The
[standard V2 producer transition](OWNER-RECYCLE-PRODUCTION.md) adds a separately
signed schema-3 production config, purpose-bound exact runtime admission and
finite original runtime history. Full measured provider proofs, native
owner/validator eligibility, canonical operator facts and the signed drained
activation block produce a 10/90 row. A distinct source hash and hotkey sidecar
bind that row to real CRv4 preparation, durable intent recovery and independently
observed archive replay. Observation-only configs/approvals remain unable to
send. [Producer qualification](evidence/owner-recycle-production-qualification-20260927.md)
and [runtime qualification](evidence/production-runtime-qualification-20260927.md)
record deterministic normal/race controls without live keys; actual native
economics remains a postcondition.
The subsequent [original-authority qualification](evidence/production-authority-history-qualification-20260928.md)
retains complete config/approval bundles for compatible independently approved
renewals. No mainnet approval, write or launch qualification is inferred.
MG-04/RT-04 and MG-06 remain open.

**2026-09-27 downstream production upload history (MG-04 / RT-04).** The
[read-only upload projection](VALIDATOR-UPLOAD-RUNTIME.md) requires an independent
content pin for the signed schema-3 configuration. It keeps exact signed runtime
intervals, route and deployment values after discarding the full config and its
producer capsule. Actual historical activation and current eligibility readers
now select their respective approved intervals, including current-window expiry.
Ordinary signed uploads cross the real server-used constructor, refresh and lease
in deterministic local fixtures. Three controls reproduce lost history, expired
current authority and an absent-field wire regression. No server writer, private
key access, live call or service activation is introduced. Original economic
authority verification, live trust and the remaining runtime consumers stay open.

**2026-09-28 durable original production authority (MG-04 / RT-04, MG-06).**
The signed config now selects bounded, content-addressed original config,
approval and runtime-document bundles with an exact signed predecessor prefix.
Source, intent, sidecar, capture and archive readers resolve their original
complete authority; current signing remains distinct. Receipt and application
readers use independently approved exact block windows, retaining uncertainty
as an error. Current renewal preserves the original drained activation and
first native epoch, so nonzero later pending rewards do not demand a restart.
Content-addressed fsync retention survives provisioning source loss without
overwriting original signatures. The upload observer copies only the validated
runtime windows. [Qualification](evidence/production-authority-history-qualification-20260928.md)
covers real signed proofs, actual native readers, cold capture, multiple renewals,
source loss, scope/substitution/conflict controls and exact predecessor-source
regressions. This continuity class holds policy, signer, operators, routes,
bounds and custody fixed. Automatic unknown-runtime approval, arbitrary policy
or custody migration, missing native epochs and live outcome remain separate;
no skipped interval becomes a successful native decision.

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

The [metadata cache](../crv4/runtime_identity.go) now has a fixed 24-entry
resident bound independent of catalog length, evicts least-recently-used
completed entries, and authenticates without caching if all slots are loading.
Runtime discovery and admissible history no longer stop merely because more
versions have appeared. An evicted entry is loaded and authenticated again.
Measure the decoded-byte footprint and add a byte cap or durable
content-addressed artifacts if the measured bound requires them. Cache bounded
successful compatibility decisions; do not let transient RPC failures poison
admission. This cache correction alone does not grant production runtime
compatibility authority.

**2026-09-27 proof ownership correction.** The separate eight-entry provisional
metadata cache also acted as the authorization registry. Admitting another
compatible runtime could evict the artifact between authentication and binding,
or invalidate a retained source signature's independent runtime view. The
[qualified correction](evidence/runtime-proof-eviction-20260927.md) gives each
authenticated artifact and bound view an immutable, connection-owned proof.
It survives eviction without an unbounded authority registry. Reusing a held
view repeats exact block version, genesis, consumed API and code checks but
does not repeat metadata/profile validation or its durable observation.
Miner, validator, shared chain and simulator binding paths retain the proof;
strict bindings clear it. A new connection still authenticates against its
explicit authority and records its own observation. This fixes in-process
proof lifetime, not durable cross-process trust or production compatibility
admission; cold caches and restart do not inherit testnet authority.

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

Astra (`gpt-6-astra`, effort `max`) owns all implementation, debugging and fixes; Sol
(`gpt-6-sol`, effort `medium`) runs affected tests normally and under race.
The final integration exercise upgrades a controlled runtime while the
subnet is active and demonstrates continued required observations, reconciled
transactions and unchanged approvals. It is a production qualification gate,
not a request to reopen testnet.

## Production hardening from sim-testnet

Reviewed 2026-09-21 by Astra (`gpt-6-astra`, effort `max`) against source through
SN `eb926565`, the [full finalization requirements](../FINALIZE.md),
[incremental recovery policy](../sim-testnet/README.md#incremental-recovery-and-acceptance),
[first report](../sim-testnet/FINAL.md),
[independent peer review](../sim-testnet/peerreview/verify/README.md), retained
failure bundles, and the corrective commits cited below. The
[September 17 handoff](../FINALIZE-HANDOFF.md) is historical evidence of a stopped
qualification, not the current execution instruction. The September 27 closure
and MG gates above supersede its instructions to continue testnet.

The recurring production risk is that an ordinary interruption can cross too
many ownership boundaries: an RPC failure invalidates startup, startup stops
healthy services, a patch changes approval/cache identity, and recovery repeats
history or financial preparation. Mainnet services must retain authenticated
progress, retry their own recoverable work, and suspend only operations whose
required safety conditions are unavailable. Passing final acceptance remains a
separate claim requiring complete evidence.

This section is a production implementation backlog. A committed simulator
repair is supporting evidence, not proof that the operator, miner, validator,
bootstrap or deployed mainnet path has the same protection. PH status denotes
remaining production work; the implementation inventory above records available
code without claiming production closure. Existing RT/RL/PF evidence is retained.
Do not copy testnet provisional flags or import the `sim-testnet` executable
into production. Extract required generic facilities into neutral packages and
qualify the production consumers described in [MAINNET.md](../mainnet/MAINNET.md#integration-with-this-repository).

### Priority, ownership and parallel delivery

`P0` protects funds, authority or required production liveness and must close
before mainnet activation. `P1` is required operational hardening before an
unattended mainnet launch; it can proceed alongside the P0 implementation.
These are production gates following the closed testnet effort. Astra authors and
reviews the implementation; Sol (`gpt-6-sol`, effort `medium`) runs the
affected tests and reports their exact failures to Astra max for debugging.
Preserve prior Terra receipts without repeating completed work for this model
change. The component column identifies the code
owner boundary, not an additional agent or approval requirement.

| ID | Priority | Production component and outcome | Existing work / dependencies | Status |
| --- | --- | --- | --- | --- |
| PH-01 | P0 | Operator, validators, bootstrap: durable recovery with independent audit and runtime owners | PF-01, PH-02 | Planned |
| PH-02 | P0 | Native/EVM submitters: one logical action, reconciled signed attempts and exact custody | RT-03, PF-03 | Planned |
| PH-03 | P0 | RPC, artifact and HTTP clients: bounded transient recovery without duplicate writes | PH-02 for submission recovery | Planned |
| PH-04 | P0 | Native-chain consumers: compatible upgrades and block-correct historical decoding | RT-01 through RT-08 | In progress |
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
| PH-15 | P0 | Status/operations: actionable failure classes, progress and evidence-based ETA | All runtime owners; PH-28 | Planned |
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
| PH-27 | P0 | Policy activation: coherent validator evidence and client-key domains across operators | PF-05; PH-10, PH-13, PH-14 | In progress |
| PH-28 | P0 | Continuous operations: independent monitoring, bounded authorized repair and exercised on-call | PH-01/02/07/09/11/15/16; MG-01/02 | In progress |

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

**2026-09-28 production recovery follow-up (integrated and component qualified).** The composed
registration candidate exposed this coupling again in the actual public root.
`requireReleaseEvidenceV2Runtime` rebuilt the reserved-attempt replica census
before native intent reconciliation, and that census rejected an upload
session's canceled context. Withdrawing an admitted API session therefore
stopped otherwise valid retained native recovery after ten polling failures.
The invalid-reply and revocation regressions reproduced this in normal and
race execution of SN `9a5b2638`; the original captures remain in
`/mnt/data/sn-testnet/evidence/operator-registration-composed-20260928`.
Separate immutable configured-source/custody validation from permission to use
an active publication session. Keep the latter at new publication, preparation,
signing and rebroadcast boundaries. A revoked API must not erase native
liabilities; invalid retained authority or custody must still block recovery.

The [corrected production ownership](evidence/operator-withdrawal-ownership-qualification-20260928.md)
now passes all 20 affected roots normally and under race detection. Restoring
the old active-session gate and removing the API-local failure latch each
reproduce their intended failure in both modes on the corrected fixture.
Original signed intent, receipt recovery and active-write refusal remain
separate assertions. Preserve the parent's two real failures and unchanged
passing work; final dependency composition and deployment remain open.

The same tests used optional diagnostic delivery to release their receipt
fixtures. Bounded output may drop records, so that is not a reliable operation
barrier. Observe the real state transition through a nonblocking test hook,
retain the real HTTP/native operations, and assert diagnostic behavior
separately. Do not make logs part of transaction or recovery authority.

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

**Immortal native liability (2026-09-28).** Current validator preparation signs
an immortal era. A local epoch crossing or approval deadline is not transaction
mortality. Keep the exact original bytes pending through an outage/restart;
before another signature, resolve them with a canonical receipt or prove a
foreign nonce consumption at the exact fully scanned boundary. A newer advertised
head may contain our own transaction and cannot supply a nonce against an older
absence scan. See the [continuation candidate](evidence/production-continuation-candidate-20260928.md);
its affected normal/race qualification is complete and does not grant old
decisions new epoch authority. Public startup and durable partial-scan recovery
retain their separate qualification scopes.

**Closure.** Inject crashes before/after intent fsync, signing, send, lost
response, inclusion, finality and postcondition publication. Cover two operators,
same-nonce replacements, cancellation, rejected dispatch, partial batches,
compacted journals and conflicting receipts. Assert at most one logical economic
effect, complete attempt/fee accounting, preserved original bytes and no
automatic nonce reset or duplicate deposit, stake, registration or claim.

**Current implementation boundary.** The [root action owner](ROOT-ACTION.md)
now reconstructs finalized native ancestry, body commitments, exact signed
bytes and phase-matched dispatch/fee evidence under an approved historical
execution profile. Its read-only port has no production signer or submitter;
the separate [approved submission port](ROOT-SUBMISSION.md) now owns actual
HTTP writes, immutable bytes and uncertain-attempt reconciliation, with no live
activation supplied by its qualification. In both adapters,
the owned RPC remains the finality/storage trust authority. Root UID and
registration generation are local approval context, not signed call arguments,
so pending seat changes require custody exclusion or incident reconciliation.

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

**2026-09-28 native HTTP cause preservation.** The pinned GSRPC HTTP client
flattened statuses to strings and returned decoder EOF without physical origin.
The [configured native-read adapter](evidence/native-http-read-causes-20260928.md)
now preserves typed status and incomplete-body causes before that flattening,
inside the existing 300-second total/60-second attempt budget. Complete malformed
JSON, permanent RPC errors, cancellation and mixed integrity
causes stay hard; writes never gain retry. All 30 selected roots passed normal
and race qualification, with ten intended causal failures retained.
The [production integration](evidence/production-native-http-integration-20260928.md)
consumes its strict physical-origin classifier without generic fallback for hard
native causes. It corrects the finite cap to include the existing 16 MiB event
field as hex plus 64 KiB of JSON framing, and distinguishes pure HTTP connection
failures during body close from local-file or mixed integrity failures. A retry
releases its body and discards idle connections first. The integration's genuine
configured-client deadline and close controls passed all 23 selected roots
normally and under race, with six intended control failures retained. This
transport slice alone does not close PH-03.

**2026-09-28 production steering review.** The standard validator's outer
steering loop tied several transient continuation branches to testnet
provisional permissions, which production disables. A receipt timeout outside
an explicitly classified recovery path could spend the hard-failure budget;
a later epoch change could reject the unfinished epoch before reconciliation.
The original-config pending branch is now covered by
the [authority-history qualification](evidence/production-authority-history-qualification-20260928.md).
The separately qualified continuation below covers the affected actual steering
owners. Public startup and durable partial scans now have separate component
qualification below; first-client recovery and final release composition keep
PH-03 open.
Preserve unknown or missed
outcomes and original signed bytes, keep unrelated workers running, and require
reconciliation before another send. A successful retry never manufactures a
missed emission interval. Review repeated finalized-block scans for reuse of
authenticated completed prefixes so retries do not perpetually repeat the same
history.

**Receipt coverage boundary.** A successful null or partial block response is
unknown evidence, not proof that a signed transaction is absent. Admission must
authenticate the complete canonical header and ordered extrinsics commitment;
known commitment layouts do not grant new runtime execution authority. Retry
unavailable evidence while retaining the exact signed attempt, and distinguish
it from a proved identity or commitment contradiction. Receipt absence, nonce
consumption and mortality must use the same authenticated finalized coverage
boundary. Reading a newer nonce after an older scan can falsely attribute our
transaction's inclusion to another transaction. Extend the scan before making
that inference, and never advance a reusable prefix on an incomplete read.
The shared complete-body admission and miner cursor correction are now
[qualified and integrated](evidence/receipt-recovery-qualification-20260928.md).
The production waits and durable partial prefixes are qualified separately below.

The same review found the miner fleet's native recovery cursor accepted an
explicit empty/truncated extrinsics vector without authenticating its body
commitment. Reuse the shared complete-body reader before advancing that cursor.
Bind new absence checkpoints to a semantic proof version and the existing
signed attempt; old checkpoints without that proof may require one rescan from
their original start. Preserve signed bytes, allowances and finalized outcomes.
Do not invalidate a qualified prefix merely because the executable changed.
Exercise the actual miner restart path, including a runtime-update digest,
beside the validator recovery tests.

The continuation review also found `VerifyFinalizedSourceContext` attaching a
canonicality/finality contradiction to a failed read before any contradictory
value was returned, then fetching the receipt body again to derive its event
index. Return the I/O cause first and reuse the admitted body/index for exact
dispatch and source-event checks. Miner scan ranges currently publish their
cursor only after up to 4,096 blocks: add bounded durable subranges so a late
timeout cannot repeatedly discard thousands of verified reads. Reuse the
header authenticated with each complete body instead of issuing duplicate
header/hash reads, while retaining canonical boundary checks and bounded fsync
work. The following prefix qualification covers these continuation improvements.

**2026-09-28 bounded prefix recovery (component qualified).** The
[shared chunk/checkpoint slice](evidence/receipt-prefix-qualification-20260928.md)
adds at most 128 fully authenticated bodies per chunk and preserves completed
chunks plus admitted partial prefixes before retrying a later unavailable read.
Miner recovery keeps its existing signed
`ScanProof` v1 and 4,096-block command bound. Validator recovery writes one small,
domain-signed disposable checkpoint tied to the exact original intent/config,
transaction and contiguous canonical range, without rewriting intent history.
Missing, empty or stale cache state requires a rescan, never a fresh launch or
signature; executable changes do not affect its key. Individual chunk reads keep
the 60-second attempt/300-second total budgets. Deterministic actual-owner
deadline barriers exercise progress within one interrupted chunk. An incomplete
body cannot advance coverage, and the saved prefix is rejoined to a canonical
parent before reuse. Optional cache read/save failures disable disk reuse and
emit one closed degradation observation; original custody and authority errors
remain hard. Memory-only progress never claims a successful durable write, and
the separately qualified public bounded diagnostic exporter exposes degradation.
Historical-only original approvals now reach same-boundary nonce reconciliation
before the no-rebroadcast wait. All 56 affected roots have scoped normal/race
passes after retained fixture corrections; nine causal families reached their
intended assertions in both modes and package vet passes. The receipt records
original failures, disqualified diagnostic captures and the incomplete
pre-execution seal of one fixture scope. Final release composition, automatic
runtime approval and mainnet activation remain open.

The new contract installer review found the same failed-read/contradiction join
in its native-to-EVM mapping check, plus admission that required the finalized
head to remain identical throughout preparation. Return failed reads before
evaluating values. Normal finalized-head advancement must not invalidate an
otherwise authorized original transaction or require restarting its phase.
Keep historical checks pinned, verify ancestry, and refresh only affected
current runtime/nonce/custody inputs within the existing bounded operation.
Test a head advancing during real HTTP readback; the chain must not need to
stand still for bootstrap to complete. Real identity, authority or ancestry
changes remain distinct from ordinary progress.

The [qualified 2026-09-28 continuation](evidence/production-continuation-candidate-20260928.md)
adds a production-only outer `Run` branch that reaches durable intent custody
before current scheduling, plus current-config receipt/application waits and
same-boundary nonce observation. Its actual nonempty owner fixtures use genuine
M8 work under a separately signed zero-price policy; they do not claim paid
capture or economic acceptance. Typed waits preserve original intent bytes and
age, expose operational unavailability, and do not spend the service's hard-error
budget. Real mixed integrity/custody failures and cancellation stay distinct.
All 10 CRv4 and 22 validator roots have normal/race coverage, preserving the
original race timeout and its six-root completion separately. All six control
families reached their intended assertions in both modes. Full fresh
`RunRelease` activation/config/dual-upload composition has its separate results
below. Historical-only foreign-nonce resolution and authenticated durable scan
chunks (including miner partial ranges) have the separate prefix qualification
above. The native HTTP integration is separately qualified;
no callback-only loop test closes the remaining physical ownership requirements.

The integrated [public startup continuation](evidence/production-startup-continuation-candidate-20260928.md)
adds the actual public-root composition with signed historical activation,
concrete disk/intent owners and real dual-operator sessions. Original liabilities
are reconciled before current UID/stake/preparation; fresh work waits for the
current eligibility and settlement-publication owner. Its tests distinguish
retained nonempty recovery from empty durable stores with already provisioned
client identities. All original 67 validator roots now have passing scoped
normal/race coverage, plus two handler regressions and four CRv4 roots. Five
causal controls reproduce their intended failures in both modes; validator vet
passes. Original failed packages, cleanup interruptions and a corrected checker
literal remain retained. The capture fenced source and module paths but omitted
a contemporaneous physical dependency-content seal; final release composition
remains open. Local semantic reconstruction must
not restart merely because the independent remote-read budget elapses. Parallel
native and EVM transient errors remain independently classified, while a hard
native physical subtree cannot be unwrapped into a retryable leaf.

Still open: deployment and live readiness of the operator API dependency and
first/missing-client JWT recovery. Registration can mutate identity and must
retain ambiguous outcomes; wrapping the whole operator constructor in read
retries or transferring a canceled startup context to its service is unsafe.

The [operator registration candidate](OPERATOR-REGISTRATION.md) now implements
the separate mutation owner: persistent opaque request plus first-send anchor,
atomic server allocation/dedup and stable-scope tombstone, exact replay and
durable credential handoff. The actual public root constructs local evidence
owners before its independent authentication worker, so retained receipt and
application observation can continue while first/legacy client recovery waits.
New publication, trails, fresh signing and rebroadcast still require readiness.
Signed `allow_client_registration` defaults false and preserves older omitted/
false configuration hashes; it authorizes one new operation, never renewal or
revocation bypass. Existing requests replay without that flag. Shared SDK
refresh rejects null/duplicate/mixed responses before both startup and background
callers; complete bad responses do not become success or confirmed logout.
The original [handoff](evidence/operator-registration-candidate-20260928.md)
is followed by the qualified [withdrawal correction](evidence/operator-withdrawal-ownership-qualification-20260928.md)
and [real API/database fixture](evidence/registration-production-api-qualification-20260928.md).
Final combined SN consumers remain under qualification. The additive DB migration and every approved operator's
versioned route must deploy before fresh clients; legacy missing identities
cannot be guessed from an empty native history. Public server-key and immutable
evidence availability remain separate startup inputs.
Native production writers continue to require explicit WS/WSS, independently of
the owned node's HTTP EVM/read capabilities. Helper-only fixture success did not
prove public config admission: the real-root diagnostics caught WS capability,
normalized signed-config representation, complete capacity relationships and
private scratch namespace assumptions; those corrections do not weaken gates.

**Registration transport follow-up (2026-09-28; component qualified).**
A mutation retry must preserve the reviewed destination as well as the original
request identity. Automatic redirects could otherwise send registration to a
different path or origin. Scope redirect refusal to the versioned mutation,
retain ordinary request behavior, and enforce limits on the encoded body after
escaping. Inspect physical error causes outside shared locks with finite
depth/node bounds; a mixed hard cause must not disappear behind a timeout.
Nil headers, browser request copies, incomplete replies, cyclic error graphs
and callbacks that block are adjacent cases. The
[transport qualification](evidence/registration-request-transport-qualification-20260928.md)
covers 47 roots plus 32 legacy descendants and seven causal roots in both
modes. Real-browser execution is not claimed. The separate actual API/DB
fixture proves lost-commit-response replay through the production route;
neither result supplies production rollout or live identity authority.

The bounded [source-finality read candidate](evidence/source-finality-read-candidate-20260928.md)
separates physical read errors from finality/schedule contradictions and reuses
one admitted body, index and event vector for dispatch and source proof. Missing
receipt wire data remains typed unknown evidence; complete contradictory data
and dispatch failures remain hard errors. The paired
[adjacent-read candidate](evidence/production-read-cause-adjacency-candidate-20260928.md)
applies the same rule to real miner recovery, owner census/eligibility and
activation setup reads. Their [combined qualification is complete](evidence/source-read-cause-qualification-20260928.md),
including normal/race causal controls and the corrected source-role fixture.
That read component does not replace the separate continuation qualification
above or the remaining composed-release and first-client startup work.

Full startup review found the same ordering defect above the steering loop:
[`runReleaseWithStartupAndProgressV2`](../validator/release_run.go) previously
dialed current native authority and requested a fresh EVM snapshot, UID and stake
before opening the historical activation, disk and intent owners. Correcting `Run` or
`submitOnceV2` alone cannot recover a process restart through that barrier.
Separate preparation-only prerequisites from historical custody recovery. Open
authenticated retained state first where its original authority permits it;
unavailable current reads must leave an observable recovery wait and preserve
independent monitoring. Current chain identity, capability and custody still
gate new signing. Require actual fresh-start and restart tests through the
public lifecycle, including activation, both upload owners and original receipt
reconciliation; callback-only loop tests do not close this requirement.

The startup composition exposed two adjacent retry defects. Parallel native and
EVM checks can both fail transiently, but applying the native-only classifier
to their entire joined error makes an ordinary EVM HTTP failure terminal.
Classify independent branches at their owning boundary, retaining each native
subtree's hard verdict for local, integrity or permanent failures. Also keep
the 300-second I/O retry budget separate from total semantic recovery: valid
local M8/history replay may take longer. An I/O timer must not repeatedly
discard and restart that work. Local reconstruction follows caller cancellation
and retained checkpoints; actual network operations retain finite retry owners.
Both corrections now have public-startup component qualification above.

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

**Qualified validator source receipts (2026-09-29).** The standard production
validator previously rebound a retained source to the inclusion block's
post-state runtime, rejecting an unchanged signature when that block installed
an approved successor. The [integrated correction and scoped evidence](evidence/validator-source-runtime-qualification-20260929.md)
keep original signed preparation, parent execution and post-state decoding
separate. All 103 selected roots pass normal and race qualification; restoring
the old post-state call reproduces the intended signing-authority mismatch in
both modes. Exact signed runtime windows and consumed interface checks still
reject unsupported or unapproved artifacts. Historical views grant no current
signing authority. The complete-header receipt path covers an upgrade digest;
other SDK current-head readers, both validator roles, automatic admission and
live upgrade qualification remain open.

The [qualified test-style follow-up](evidence/validator-source-runtime-qualification-20260929.md)
keeps all seven receipt roots top-level and replaces the digest subtests with
a plain loop. Those seven roots pass normal/race and validator vet passes;
both digest variants still fail the isolated old-post-state control.
Production bytes are unchanged.

**Production change.** Deliver RT-01 through RT-08 across miner, operator, both
validator roles and bootstrap. Construct and sign from one immutable runtime
view; validate consumed call/storage/API/precompile/signing capabilities.
Separate block execution from post-state context at upgrade boundaries. Store
observed runtime versions as evidence, separate from stable deployment policy.
Historical reads bind genesis, block hash, original runtime/metadata and decoder
version. An archive-capability refusal identifies the missing proof and blocks
only dependent work; never substitute a current-state read for a historical
one. Keep artifact caches bounded independently of the number of known versions.
Authenticate the complete finalized header against its announced hash, including
parent, roots and digest, before using that hash to select runtime or storage.
Decode the complete runtime version through one shared parser; when an RPC
supplies both `stateVersion` and `systemVersion`, contradictory values are an
integrity error. An RPC's same-height hash lookup and matching version numbers
alone do not prove the header or state layout.

**Closure.** Use the RT-08 controlled upgrade plus historical reads on both
sides of the upgrade, concurrent signing, stale subscriptions, evicted metadata,
wrong genesis and pruned-state responses. A compatible update requires no
manual version entry or repeated funding; an incompatible consumed interface
halts that operation with a precise capability error. An ABI match alone does
not establish unchanged economic semantics.

**Current observation boundary.** `sn-mainnet runtime-snapshot` authenticates
the complete finalized SCALE header, retains raw `:code` and metadata at that
hash, confirms code bytes against `state_getStorageHash`, compares the complete
runtime tuple across reads, and repeats canonical/genesis/EVM/chain checks.
It accepts unfamiliar metadata bytes for review instead of pretending an old
decoder authorizes them. Unknown digest variants require an explicit profile
review. `inspect`/`monitor` check the complete runtime tuple internally but
their v1 JSON still exposes only spec/transaction numbers; serialize the full
tuple in a separately versioned observer artifact before treating that output
as independently auditable runtime identity. The [latest Snow sample](evidence/runtime-snapshot-header-auth-snow-20260927.json)
is testnet runtime 471; raw observation is not mainnet runtime admission or a
verified source-to-Wasm mapping.

The owned Snow testnet also exposes a Frontier `fron` digest in finalized
native headers. At six sampled historical/current heights, its leading EVM
hash matched the EVM RPC, while payload lengths differed. The signer-free
[`finalized-mapping` command](FINALIZED-MAPPING.md) now decodes the reviewed
SCALE/PostLog variants, authenticates exact raw EVM RLP by the digest-derived
hash and corroborates EVM canonicality using the RLP-decoded number. The
[Snow mapping observation](evidence/finalized-mapping-snow-20260927.json)
retains both headers and independently reproduced hashes. The newer
[`finalized-snapshot` command](FINALIZED-SNAPSHOT.md) captures those headers
and exact runtime bytes under one selected native hash; its [Snow observation](evidence/finalized-snapshot-snow-20260927.json)
reproduces all four hashes in one record. Finality remains an owned-RPC
assertion and runtime source is not proven. Repeat with independently approved
mainnet identity after route cutover; no testnet mapping approves it.

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

**Current implementation boundary.** Offline `source-lock` records clean SN
and local replacement Git heads, module hashes, Go toolchain and executable
hash. The [composed candidate](evidence/source-lock-composed-20260927.json)
pins SN `f321ba7c`, server `9f860731` and Connect `c68689c4`. It is an input
to a release manifest, not a lock of generated artifacts, signed policy,
Solidity bytecode, configuration, images or approved rollout identity.

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

**2026-09-28 closure follow-up.** The closed testnet campaign left its supervisor
and 31 workers running after both validators stopped. The original `stop`
command then refused unrelated current launch settings before reaching its
shutdown handler. The verified service owner completed a graceful shutdown;
the [retained observation](evidence/closed-testnet-service-stop-20260928.md)
records both outcomes. Campaign closure must explicitly retain or stop each
owned service, with a reason and owner. Dispatch stop/drain from authenticated
retained process identity independently of new-launch configuration and live
RPC availability. Preserve journals and volumes; prove old workers exited
before admitting a replacement. This operational cleanup does not establish
that the production shutdown path is qualified.

**Closure.** Kill an operator or validator independently, restart an owned
database/object-store container, lose the observer connection, rotate the
executable path and simulate PID reuse. Verify no duplicate signer/sidecar,
no orphan process, no unexpected volume recreation and joined shutdown.
Include invalid replacement configuration and an unavailable RPC during stop;
neither may prevent terminating the exact already-owned process generation.
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
On 2026-09-27 at 07:21 UTC the test-data USB SSD disconnected during isolated
mainnet-gate work. The kernel aborted its ext4 journal and `/mnt/data`
disappeared; the device returned under a different `/dev/sd*` name. Recovery
used the configured filesystem UUID, `e2fsck -p` journal replay (reported
clean), and remount; the isolated uncommitted worktree reappeared. This is a
storage-availability incident, not proof that every in-flight write survived.

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
The Snow xops guard in branch `codex/mainnet-subtensor-mount-guard-20260927`
(commits `ca49e00`, `ec443da`) requires the configured data mount before either
playbook inspects or creates a node generation, and at each systemd start. It
also rejects a finney cutover retaining testnet chain ID, genesis or bootnode,
and renders the reviewed `/ws` bootnode form for both containers; the monitor
helper accepts that form. The 36 affected Python tests and both Ansible syntax
checks passed. The branch is not deployed and its checked-in chain selection
still targets testnet. During the operator's data move, verify the actual mount
path and volume UUID before applying that branch; then switch chain, genesis,
runtime, bootnode and reference pins together for mainnet.

**Closure.** Exercise missing mount, read-only/full volume, inode exhaustion,
device disconnect/re-enumeration, journal replay, partial write, crash
before/after rename, cache loss and restored backups. Check the mount by UUID
and filesystem identity before any write after recovery; rehash uncommitted
artifacts and rerun interrupted tests rather than treating directory
reappearance as completed work.
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

**Current implementation boundary.** The [claim-recovery correction](../evm/CLAIM-RECOVERY.md)
atomically rolls back a failed runtime payment while preserving an accepted
leaf and provider credit, and the receipt verifier accepts the actual
zero-based deferral enum. The non-upgradeable vault requires a new deployment;
its nested EVM/native rollback and exact stake deltas must be rehearsed against
the authenticated production runtime. Existing vaults are not patched.

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

**Provider registration follow-up (2026-09-29; qualified and integrated).** The
[provider registration change](evidence/provider-client-registration-candidate-20260929.md)
replaces first-client allocation in `provide`/`auth-provide` with the existing
versioned request protocol. One retained seed owns all direct/proxy slots;
key/request publication precedes POST, replay retains the original operation,
and required registration/refresh/logout custody stays on that owner's physical
directory. New allocation needs explicit permission. First-upgrade legacy-key
adoption is a separate operator assertion, not a key-to-JWT proof or permission
to replace a lost identity. The source is qualified and integrated: 114 selected
roots pass per normal/race mode (228 executions), and all 17 causal variants
are valid in both modes (34 executions). This is 114/2324 package roots, not
full miner/validator coverage. The sealed e32 fixture-failure receipt remains
separate from child qualification. Public daemon fixtures stop at authenticated
handoff, with refresh/logout tested separately, so full serving, processed-key
and proof readiness remain open. The no-config measurement validator's durable
primary identity has its separate qualification below; this provider slice
cannot claim all role startup paths are repaired. Approved live
API deployment, actual custody, native/contract admission, economic acceptance
and independent operational monitoring remain external gates. PH-13 stays open.

**Measurement primary-client follow-up (2026-09-29; qualified and integrated).**
The [no-config validator change](evidence/validator-measurement-client-registration-candidate-20260929.md)
removes the legacy allocator from durable primary `.validator.jwt` startup. Its
closed measurement/direct scope always uses `allowCreate=false`, with original
key custody, explicit first legacy adoption or exact retained-operation replay;
missing unowned identity stays a recovery refusal. Refresh validates the original
identity and persists before publication. Actual API/transport/measurement users
join before key ownership ends, and successful replay releases only the shared
bootstrap lock. Parent `c3fe0cf2` remains unqualified after its process-exiting
docopt CLI fixture aborted both focused validator packages; its 62-file anomaly
receipt is preserved. Test-only parser child `e33f64d4` keeps production bytes
unchanged. All 37 selected roots pass normally and under race detection (74
executions, twelve package PASS/exit-zero streams), and all 38 causal executions
reach their intended assertions. Independent audits verify the sealed receipt,
exact source, ten local modules and eight physical roots; integration changes
only documentation above the qualified source. No full-package coverage or live
authority is inferred. The final-save fixture excludes an earlier periodic
snapshot at a real worker-join barrier. Existing ephemeral
tunnel-client allocation and separate proof/stats history recovery are unchanged;
seed discovery and local shutdown do not establish completed trails or live
readiness. Mainnet identity, deployment/custody, contract/Safe authority,
native/economic outcomes and independent monitoring gates remain open.

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
**10% of native miner allocation**, the user-selected **90% owner-recycle**
policy (no reserve credit), and **both
the netuid-0 root and UR subnet validator roles**. Scaling all weights or theta
alone cannot implement the 10% requirement. Recheck role/permit/registration
eligibility and operating reserve at execution, using approved semantics.

**Closure.** Preserve the unmet testnet requirement for three fully observed
accelerated production-policy epochs. Mainnet uses MAINNET.md's own acceptance
scope: at least three complete native emission intervals and a full mainnet UR
settlement/claim cycle, with configured intent distinguished from actual cadence.
Verify hyperparameters and reserve at pinned blocks and report unresolved
exceptions plainly. Mainnet activation additionally proves the chosen reset
capability, 10% denominator/rounding, actual reward outcome and both validator
roles. Test stale plans, unauthorized calls, competing registrations and policy
activation races without silently substituting a narrower reset or reward goal.

**Current implementation boundary.** The [signed owner-recycle admission](../validator/OWNER-RECYCLE-ADMISSION.md)
checks an independently pinned approval, immutable retained bytes and finalized
runtime/owner/Recycle-mode census. Those observation formats remain fenced.
The distinct [production successor](OWNER-RECYCLE-PRODUCTION.md) now carries
independent authority through measured proofs, native preparation, signed
sidecars, durable intents and archive observation in the standard V2 runtime.
It still needs actual production activation/API/key/history inputs and live
approval. An admitted weight row alone does not establish a 10% native outcome;
that outcome is a measured postcondition. Migration of old sidecars to a new
approved production config remains separate unresolved work.

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

**2026-09-28 optional cause-classification follow-up (integrated and component qualified).**
Bounded output queues alone do not isolate a callback if it first invokes an
arbitrary error's `Unwrap`, `Is` or `As` method to choose a diagnostic label.
The miner, trail and release-read diagnostic classifiers contained this
coupling. Read only concrete owned or standard-library error fields with
finite traversal; classify custom and opaque wrappers as unknown without
calling their methods. Retain the original error for its required custody or
retry owner, whose decision policy remains separate. Required cancellation
must not wait for an optional diagnostic offer. Root scalar events and the
shared output queue do not traverse producer errors and are outside this
correction. The [20-root correction and five causal controls](evidence/diagnostic-cause-isolation-qualification-20260928.md)
pass normally and under race detection, including blocking-method barriers,
typed-nil and cyclic wrappers and actual authentication/file callbacks. Opaque
wrappers report `unknown`; no diagnostic cause grants retry authority. Final
combined dependency checks and deployment remain separate work, without
restarting the frozen output qualification.

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

### PH-28 — Continuous monitoring and authorized repair

The [one-shot stopped-validator capability](VALIDATOR-REPAIR.md) now has a
concrete fixed systemctl action, independently signed expiry/release/unit/boot
and generation authority, existing incident binding, permanent one-start custody
and generation/source postconditions. [Offline qualification](evidence/validator-repair-qualification-20260930.md)
is sealed for `af570cdc`: 52 positive normal/race executions passed, with twelve
normal and seven selected race controls causal. Genuine cgroup-v2 admission and
the post-sync expiry/sample-age gate retain the one-start boundary. Crashes before
durable start acknowledgement remain explicitly consumed and uncertain; no
automatic repeat can restore the allowance. Deployment and trusted exclusive
host service control, actual systemd rehearsal, alert delivery, production active
hang recovery, operator/root roles, initial activation and monetary repairs remain
open. The separate [active-hang increment](ACTIVE-VALIDATOR-REPAIR.md) supplies a
bounded independently signed stop/join/start interface and retained generation
custody; it does not approve or rehearse production service mutation.

**Production change.** Implement the [mainnet operating model](MAINNET.md#continuous-monitoring-and-repair)
as three separate owners: a signer-free finalized-chain monitor, bounded service
supervisors, and a repair controller that consumes an approved action envelope.
The monitor must survive stopped application processes and failed repairs. It
compares authenticated state at the same finalized native/EVM checkpoints on
the owned node and a separately provisioned canonical source, and retains raw
evidence independently of the transaction owner's claims. Until that second
source exists, report `independent_rpc=false`; another process reading Snow's
same backend is not node independence.

The [operator journal increment](OPERATOR-MONITOR.md) is independently qualified
offline on exact SN `070ec769` / server `99130c2d` sources. It observes the existing
transaction/attempt, publication and scan journals through one read-only database
snapshot, even while the operator process is stopped. Retained incident history
does not turn these database assertions into canonical receipts, complete
liabilities, provider/client-key evidence or current deadline admission. Its
[qualification receipt](evidence/operator-monitor-qualification-20260930.md)
keeps the unobserved domains and deployment/alert-delivery gates explicit.

Publish bounded metrics and structured incident records to the existing xops
Grafana/Mimir/Loki and host Fluent Bit infrastructure, with the established
exporter and credential boundaries. See [deployment infrastructure](../../xops/main/ansible/playbook-dbs.yml)
and [telemetry isolation](../../xops/VULNSCAN2.md). SN needs domain-specific
dashboards and alerts, not a second logging service. Telemetry has no signing,
database-mutation or full-host control authority. A separate dead-man alert
detects loss of the monitor and of alert delivery.

| Gate | Required operating evidence |
| --- | --- |
| SLOs and alert classification | Before activation, bind poll intervals, freshness/deadline margins, finite retry budgets, severity and primary/backup on-call in the operations manifest. Starting targets in MAINNET.md are proposed values, not measured availability. Distinguish unavailable reads, actual identity/integrity mismatches, missing progress, deadline risk and accounting failures. |
| Complete chain and application view | Prove current node/runtime identity; native/EVM finality mapping; both UR validators' permit, source and applied/revealed rows; root seat/delegation; every operator's policy/client-key/proof domain; usage, deposit, capture, root, carry, claim and liability accounting; release/config drift and pending signed attempts. HTTP 200, process liveness and acknowledged traffic are insufficient. |
| Automatic actions within authority | Reconnect/retry exact idempotent reads, resume bounded authenticated replay, restart the same approved service after joining its old owner, and reconcile already signed transactions. Any rebroadcast/replacement or scheduled renewal must be explicitly authorized, expiring and capped, with one writer and retained original bytes. A monitor finding alone is never permission to spend. |
| Operator-gated actions | Changed policy, runtime capability, contract/custody, endpoint authority, release, schema, native-history boundary, allowance, stake, UID reset or new registration requires its exact reviewable action and appropriate signer authority. No blanket autonomous repair, hidden funding or direct historical SQL rewrite. |
| Repair correctness | Append/fsync intent and signed attempt before effects; reconcile canonical receipts, dispatch, postcondition and nonce before retry; enforce lifetime/per-action/count/deadline caps across restart. Verify the result with the independent monitor and preserve unresolved or rejected repairs as incidents. |
| Incident and deployment discipline | Retain source/config/plan and process identities, pinned blocks, raw responses, log byte ranges, debt and liabilities, actions and terminal receipts. Reproduce and fix the observed cause, qualify affected/adjacent paths, canary a composed release, and check state-format compatibility before rollback. Finalized transactions and database migrations require a forward recovery plan where rollback is unsafe. |
| On-call rehearsal | Deliver a real test alert to designated operators, exercise primary/backup escalation and the stop/reconcile/recover/verify runbook, and show that no custody or claim obligation is discarded when a component stops. Preserve the incident timeline and independent recovery evidence. |

**Closure.** On the exact production image, inject RPC loss and wrong-chain
responses separately; stop the monitor, alert path, an operator and a validator;
stall a signed transaction; miss a native or policy boundary; exhaust log/disk
capacity; and create an accounting mismatch. Read outages must remain unknown
observations, known integrity failures must stop dependent signing, and neither
may create a successful acceptance sample. Prove alert delivery and durable
single-owner recovery within the selected SLOs. Review the repair allowlist,
caps and on-call roster before bounded activation; promote to unattended
operation only with the required live observations and no open critical
incidents. Record recurrence and near-miss trends for subsequent fixes without
erasing the original failures.

### PH-16 — Deterministic qualification and reviewable evidence

**Lesson.** Some prior failures were real production defects; others were
incorrect selectors, working directories, fixture assumptions, stale generated
artifacts, missing offline dependencies or observer/capture failures. Repeated
full gates and confirmation runs did not isolate those causes. The handoff's
`[no tests to run]` example and the retained failed bundles must stay distinguishable
from passes.

Blocking HTTP fixtures must consume and close bounded request input before
waiting on `request.Context()` cancellation, and retain an independent cleanup
release joined before the test server closes. An unread POST body can prevent
the expected cancellation notification. This lesson recurred in the monitor
and public-startup seed handlers; actual runtime completion and a hung fixture
cleanup are separate results. The startup correction audits both seed and EVM
outage barriers and preserves prior retained-intent normal/race passes.

The September 27 combined-source validator package run hit Go's default
10-minute deadline under `-parallel 2`. Its original PTY output was truncated,
so the exact active test cannot be established. The matching compact replay
error text is deliberately emitted by interrupted-read fixtures; their
six-case family passed in isolation. A [retained diagnostic](</mnt/data/sn-testnet/evidence/mainnet-validator-timeout-20260927/RESULT.md>)
was stopped after 350 seconds with 275 top-level passes and no assertion
failures, while serial, fsync-heavy tests were still running. This is **not** a
full-suite pass or a demonstrated production replay defect. Use a measured
package deadline and persistent per-test log for the frozen release, while
keeping changed-path normal/race results distinct from the incomplete broad run.

The September 28 continuation selector repeated the deadline problem at a
smaller scope: its 120.755-second normal run passed all 22 roots, but the race
run reached a 600.190-second package timeout after 16 passes, without an
assertion failure. Preserve that package failure and resume only the interrupted
root plus five unstarted roots. Choose future deadlines from the measured
workload **in the same mode**, with at least 2× headroom; real compact replay
has a materially larger race cost than its normal timing. Enumerate selectors
before running: a terminal `$` on a test-family prefix selects no descriptive
test names. Neither a zero-root invocation nor a runner timeout is a product
regression result.

A later monitor capture assembled its selector from a headerless outcome table
as though the first row were a header, omitting one of 48 expected roots. Keep
that invocation as a 47-root scope and run the missing root separately. Require
exact expected-versus-selected membership before bodies, including the first
and last entries; nonzero enumeration alone does not establish complete scope.
Prefer the maintained qualification owner over another untyped selector wrapper.

Go permits one literal `t.Run` name to contain slashes without emitting every
intermediate prefix as a test. The old declaration parser rejected complete
retained HTTP results by requiring those nonexistent events. The
[qualified correction](evidence/qualification-slash-parent-qualification-20260928.md)
uses the nearest explicitly source-declared ancestor, preserving exact event
membership, genuine parent ordering, failure literals and original binary exit.
Its own 31-root normal/race checks and two controls passed before read-only
replay recovered both original 33-root/32-descendant results. Keep declaration
repair separate from body execution; do not rerun successful unchanged bodies
to repair a checker. Ambiguous flat sibling prefixes remain an explicit limit.

The monitor's blocked HTTP fixture then consumed its whole package deadline in
server cleanup after the real monitor workers had exited. Its handler waited
for request cancellation without reading the POST body, preventing HTTP/1's
background disconnect read from starting. A blocked-read fixture must consume
and close the bounded real request before advertising its cancellation barrier.
Join the actual handler; manually releasing a separate test channel cannot
establish production cancellation. Keep the failed capture and qualify the
corrected shared fixture and its adjacent restart consumer.

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

A fixture's SDK JSON encoder is not necessarily the RPC wire encoder. Receipt
qualification exposed an SDK that emits unprefixed block-number hex although
Substrate serves a `0x` quantity. Keep genuine SCALE header/body commitments,
serialize the actual wire format explicitly, and require malformed-response
tests to reach their injected read. An unrelated early fixture rejection is
not evidence that the intended failure was handled.

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

**Closure.** The affected qualifications plus controlled production-path
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
Preserve the closed sim-testnet evidence. Use controlled production-path
rehearsals for new fixes and record actual mainnet observations after an approved
activation; neither can rewrite the original R48 outcome.

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

The original incident required a new server usage producer. By R48 the
prospective usage path and the separate `74893863` legacy-quarantine fix had
implementation evidence; production composition and end-to-end qualification
remain open. Required regressions compare otherwise identical paid, free and
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

### R48 terminal: process-log throughput, native history and immutable usage

The post-R48 scanner patch on `main` drains the observed log size in bounded
64 MiB segments, checkpoints each completed cursor before the next read, and
resumes from that cursor after an interrupted read. Its causal regression
fails against the R48 scanner and passes with the patch; nearby log-gate tests
pass in the aligned R48 source workspace. This is a code repair, **not** a
retroactive R48 acceptance result or proof that the live supervisor uses the
new binary. The retained R48 process-log journal uses classifier v13; the
patch carries that classifier forward so a later reader can load it without
downgrading its findings. The missed native-1696 history and incomplete
acceptance interval remain open.

The post-R48 root composition could not compile `go test ./sim-testnet` with its
adjacent `server` and `sdk` checkouts: the captured server called the older
three-argument `protocol.RequiredDepositRao`, and the captured SDK lacked the wallet
challenge context method and `Purpose` field used by SN. Lock compatible
revisions of the complete production dependencies and qualify the combined
release. The new signer-free mainnet observer has its own passing focused
normal/race tests; it does not establish that combined build. The scanner regression was run in the already aligned
R48 source workspace with the changed files overlaid; that result must not be
misreported as a passing build of the current root checkout.

[R48's final report](../sim-testnet/FINAL-4.md) records a signed acceptance
boundary but no fully observed acceptance epoch. The immediate stop was the
process-log heartbeat: both operator API stderr streams had already exceeded
the 64 MiB scanner delta before acceptance, and the unchanged cursors produced
fresh `log-overrun` findings after the boundary. Mainnet's process-log reader
must consume large growth in bounded, hashed chunks with a durable cursor,
not classify an ordinary large backlog as permanently unscannable. Keep a
separate hard failure for inode changes, truncation, changed prefix, unreadable
bytes and overlong individual lines. Drain and authenticate any pre-boundary
backlog before binding the acceptance boundary, or disclose and sign an exact
exception; do not redate the same old gap as a new runtime event. Test a
multi-gigabyte noisy stream, concurrent append, a scanner crash between chunk
and cursor commit, rotation/truncation, restart, and a short scheduled fault
while the backlog is drained. Completion must show every byte accounted for,
bounded memory, and a fault heartbeat that still meets its block timing.

The other release-blocking class was validator steering repeatedly reporting
that strict history adoption missed the approved first native epoch 1696.
There is no authority to backdate that application. Before mainnet, provide
an owner-signed forward selection and handoff for **both** validators: retain
the old intent and EMA as historical evidence, choose a future native epoch
with a measured startup margin, checkpoint the exact source and transaction
outcome, then prove finalized on-chain weights from both validators at that
new boundary. A restart, stale runtime change or interrupted read must resume
that same authorized plan without inventing a prior success. The test must
cover a missed first epoch, pending transaction reconciliation, process
restart, exact signed history, and independently queried final weights.

R48 also found 4,183 settled epoch-658 contracts closed by a mixed-version
operator fleet without immutable `provider_usage`. The guarded one-time
testnet repair retained at least 3,082,728 bilateral-report bytes as explicit
uncredited debt (`final_acceptance=false`), allowing ordinary close to proceed
without guessing providers. Mainnet must deploy the snapshot writer and
reader as a compatible fleet, check for missing snapshots before settlement
and before release admission, and reject positive credit reconstructed from
mutable membership. If a historical repair is unavoidable, it needs an exact
whole-row/report manifest, serializable changed-row and complete-census
guards, a canonical per-row debt receipt, replay refusal, and a final report
that distinguishes omitted usage from paid usage. Rehearse a mixed-version
rollout and rollback, a canceled contract, altered bilateral report, expanded
NULL census, wrong epoch/ID/close time, and interrupted commit. The qualified
testnet server source is `74893863`; it is evidence for the design, not proof
that the mainnet release image contains the fix.

**P0 retention follow-up (MG-06, PH-09/12; In progress, 2026-09-27).**
Before the correction, seven-day completed-payment retention deleted
`transfer_contract` rows and their `provider_usage`, while the epoch reader
queried only the live table. Server `9f860731` now implements an append-only
archive and one-snapshot live/archive reader. Exact usage is captured atomically
with deletion; failed capture aborts deletion and duplicate credit-bearing
identities fail the window. Pinned and combined normal/race selectors passed,
as recorded in the integration note above. This remains an open production gate:
coordinate reader/writer/reaper cutover, prove the deployed schema and replay,
and qualify durable archive capacity. Already deleted rows remain an explicit
historical gap; do not backfill guessed providers or reconstruct positive credit.

R48's local epoch-658 close missed its on-chain commitment window, while
epoch-659 roots later committed on-chain. The mainnet gate must distinguish
that real chain progress from successful acceptance. It should continue
collecting a terminal report after recoverable process findings and clean up
exact pre-armed faults after any terminal failure, while keeping the original
failed assertions and signed boundary immutable. A partial or provisional
release must never be silently promoted to mainnet launch approval.
