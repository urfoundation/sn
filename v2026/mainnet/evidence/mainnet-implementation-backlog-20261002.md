# Production implementation backlog — October 2, 2026

## Later verified deltas

The [continuation checkpoint](mainnet-continuation-checkpoint-20261002.md)
records newer source qualification without changing the pinned census below.
Observer `6d398662` is independently qualified. Composed `69f4bbdd` now passes 78
selected roots normal/race and vet, with twelve independent public roots in both
modes and vet; its [sealed scope](durable-owner-composition-qualification-20261002.md)
remains separate from later intake and the full backlog. Startup
`c3707376` preserves healthy miners after a later transient admission failure.
Callback quarantine and provider observation are separate successor work.

Later integration: callback source `36648203` and private-fixture test-only
source `28c970db` are now merged and pushed on main `ba55e0f6`.
[Exact qualification](provider-callback-qualification-20261002.md) retains
40 author / 12 independent selected callback roots per mode and seven fixture
roots per mode under both umasks. Provider observation remains separate work.
Current server `cebf154f` cannot compile against its declared published Connect
`6443417d` because that version lacks `durablevolume`; the compatible published
`e0d75562` successor preserves the peer and upstream callbacks, but requires
actual consumer-graph qualification. Do not repeat the original component
suites or infer release authority from these integration updates.

The new immutable-member source `8d37e7a5` passed 16 core normal controls, then
completed all 20 adjacent normal controls with 14 passes and six failures.
Four positive fixture maps incorrectly include a legitimately mutable census
head. Two failures expose real premature finalization of retained terminal or
policy stages; their ordering assertions must remain unchanged in the fix.
The original failed source and results stay retained.

PH-03 has a concrete additional read boundary: `miner/sn.go` manual `claim`
calls SDK `SnEpochSync` and `SnPoolClaimSync` directly and panics on failure.
The exact SDK `5d37be38` / Connect `0a5cda0e` graph retries only one response
for default 502/503 status, with a 100–1,000 millisecond pause; its GET wrapper
returns a transport error rather than retrying it. Route hedging does not prove
the required minimum read-retry duration: two immediate 503 responses can end
the operation well before 60 seconds. Qualify an owner-level read budget of at
least 60 seconds, normally 300 seconds for expected available data, with typed
permanent/decode refusal and cancellation joins. The separate daemon retry
path is not evidence that this finite command is resilient. Signed POST or
claim broadcasts must not be blindly repeated under the read policy.

The immediate patch order below describes the original census; these deltas
advance its evidence, without closing the entire corresponding PH/MG rows.

This read-only implementation census is pinned to SN
`653061a15ee32b56f8dfb602d3bfcdaea7d30b47` and server
`10a8f4d8ab73822b4c796f9035486f0507e09bd2` (incoming server `6387a012`).
The tracker baseline is documentation commit `3c00c374`. It refines, rather than
replaces, the earlier Sol census at SN `105e54c3`, SHA-256
`09490f3cd5f21973feee09db8dab15094e8223f049d6c1996f6725c9db0be98f`.
The newer source contains components absent from that older census. A listed
callsite is evidence that implementation exists, not evidence that its complete
production behavior is qualified. No table row grants live authority.

Classification is per remaining behavior: **partial** means a production
mechanism exists but integration/coverage remains; **missing** identifies a
specific absent interface or mechanism; **audit** identifies a concrete source
boundary that still needs a causal investigation; **live** needs real authority,
configuration, equipment or observed effects. Mixed rows deliberately keep the
offline work separate from live inputs. Proposed patches must first reproduce a
defect or establish the new public behavior, then qualify only affected scopes.

## Immediate patch order and ownership

1. Finish the exact bounded observer and exclusive-successor composition gate,
   including all actual public declaration/loss and eight successor admission
   controls. Integrate the qualified production owners without repeating the
   unchanged 201/114/111-root component gates. The previously failed `f12`/`ea2`
   results remain part of the record. Observer `fbf806f` passed its ten normal
   and race controls but failed the explicit vet gate; the `653061a` successor
   and complete joined composition remain under qualification.
2. Adopt merged Connect `71df099c` in a separate module-only consumer successor.
   Prove the effective module graph and a real owner startup EIO/EMFILE refusal
   followed by same-policy recovery. The `0a5` composition cannot inherit this
   result retroactively.
3. Implement the missing provider observation path: connect the real provider
   runtime's progress to an independently configured expected-provider census,
   bounded versioned source identity, actual readiness/proof progress, per-role
   incident retention and monitoring output. `ProviderSwarm.status` presently
   exports running counts/maps; `monitorServicesPolicy` accepts validators and
   operators only. A source-bound provider monitor is an implementable MG-07
   gap. It must not infer eligibility or economic delivery from connectivity,
   and observation must not acquire repair/signing authority. Author owns this
   miner/monitor slice; bootstrap/preparation owner remains separate.
4. Audit then implement the fleet runtime-continuation gap at
   `fleetMainnetRuntimeAuthority.authenticateAt`. The current production policy
   accepts one exact artifact; validator historical intervals and bootstrap
   successor selection already exist separately. Add a consumed-capability and
   semantic-authority selection interface shared by current admission,
   historical replay and repair. Unknown semantic changes must remain refused;
   metadata/ABI equivalence alone cannot approve economics or a signing domain.
5. Keep offline preparation, member census and the sibling's bootstrap/validator
   work moving independently. Storage is not a reason to postpone all remaining
   runtime, retry, monitoring, economic and evidence work. A new composed release
   comes after the required source increments, preserving earlier artifacts.

## Latest qualified read recovery

SN main integrates spool68 and finite40d while preserving retained-member
recovery. [Guarded-reader evidence](current-graph-reader-progress-20261002.md)
records 15 affected independent tests in each mode.
[Finite-claim evidence](finite-claim-independent-20261002.json) records nine
affected independent tests in each mode, vet, and two causal old-body refusals
per mode. All miner and module bytes in merge `d5c5df7bf368221253a514cae046e4cd56d55d33` match frozen40d.
The minute-outage and deadline tests use deterministic clocks with real SDK
reads; they do not establish a wall-clock outage rehearsal. These integrations
advance PH-03/09 without closing all callers, PH requirements or mainnet gates.

## PH requirements and next concrete work

Paths are relative to SN unless prefixed `server/`. The next-step column names
the smallest next patch or discriminating source test, not an assertion that
the entire requirement can close in one change.

| Requirement | Current production boundary | Remaining classification and next concrete work |
| --- | --- | --- |
| PH-01 durable progress and separate audit/run owners | `miner/fleet_recovery_native.go: fleetRecoveryResumeNativeRange`; `mainnet/evm_action.go: evmCreateOwner.advance`; `mainnet/root_service_bounded_recovery.go`; guarded snapshot/native owners | **Partial.** Finish exact observer/successor composition and sibling immutable-member heads. Test closed-owner loss and recovery against actual retained authority, including steering/registry members still outside a snapshot. Never reconstruct missing completed history or reset unrelated owners. |
| PH-02 one logical action and all signed attempts | `chain/submit.go: SubmitCall`; `miner/fleet_recovery_evm.go: fleetRecoverableEvm`; `server/strecovery/receipt_reconcile.go`; `mainnet/bootstrap_successor_execution_store.go: append` | **Partial/audit.** Census each current public send adapter for original/replacement/cancellation joins and pre-sign/pre-broadcast request cancellation. Add only missing causal seams. Live global signer custody and actual fee attribution remain separate. |
| PH-03 transient client recovery | `miner/sn_rpc_retry.go: retryEthRpcRead` (90 seconds, 64 attempts); `mainnet/rpc_read_retry.go`; `validator/release_http_get_retry.go: retryReleaseHttpGet` (300 seconds); `validator/chain_read_retry.go` | **Partial.** Existing retry code must not be labelled absent. Census actual service/HTTP/native callers against these helpers; force mixed permanent+transient causes, canceled bodies and reconnect admission. Patch uncovered callers, retaining immutable request bytes and final cause/budget. |
| PH-04 compatible runtime continuation | `miner/fleet_mainnet_runtime.go: authenticateAt`; `chain/runtime.go: AuthenticateFinalizedRuntimeContext`; `validator/production_runtime_history.go: releaseProductionRuntimeAt`; `mainnet/bootstrap_successor_runtime_selection.go` | **Missing for fleet automatic selection; partial elsewhere.** Add the shared consumed-interface/approved-semantic selection described above, then force upgrade between preparation, signing and send, original receipt replay and current-read recovery. Live source/checkpoint authority is not supplied by those tests. |
| PH-05 reusable authenticated proofs | `validator/production_receipt_checkpoint.go`; `validator/production_receipt_cache_state.go`; `validator/attempt_boundary_cache.go: Resolve` | **Partial/audit.** Count actual immutable verification work across restart and a changed executable; ensure full dependency keys and invalidation. Add a durable cache only where a real production path repeats work. Never reuse current balance/nonce/permit observations as immutable evidence. |
| PH-06 release/plan/config identity | `mainnet/source_lock.go: buildSourceLock`; `mainnet/release_inventory.go: buildReleaseInventory`; `mainnet/plan.go: buildBootstrapPlan`; activation unit rendering | **Partial + live.** Compose the required new sources into an exact release; qualify lossless original-plan authority under module/render successors. Independent compiler provenance, reviewed production configuration and acceptance remain live/review inputs, not claims from repeated local binaries. |
| PH-07 ownership, supervision, readiness | `miner/swarm.go: ProviderSwarm.Run`; `miner/swarm_control.go: controlMember/stopMembers`; `mainnet/validator_activation.go: advanceValidatorActivation`; `mainnet/root_passive_host.go` | **Partial/audit + live.** Force one member's transient start failure and explicit stop during in-flight control; check whether unaffected members survive and every owned child joins. Patch only a confirmed failure. Qualify real host units separately without treating synthetic manager calls as deployment. |
| PH-08 bounded replay/fair scheduling | `miner/fleet_recovery_native.go: fleetRecoveryResumeNativeRange`; `mainnet/bootstrap_successor_execution.go`; validator retained upload/replay owners | **Partial.** Add measured foreground/background contention at actual worker boundaries with bounded memory and cancellation. Make any missing per-owner work budget explicit before broad fleet-size rehearsal. Simulator fairness is not production evidence. |
| PH-09 durable storage | `internal/durablepath`, `internal/durablehead`, native/miner/monitor owners, sibling validator/bootstrap/blob adopters | **Partial/missing + live.** Finish composition/module adoption; implement the reviewed offline root/lease/nonce/owner preparation API and known owner adapters. Retained member census, capacity/rotation and actual restored-volume acceptance stay open. Inventory is report-only. |
| PH-10 resumable epoch/fleet renewal | `mainnet/owner_recycle_action.go`; `validator/release_runtime_intent_v2.go`; `mainnet/monitor_native_deadline.go: monitorNativeEpochBoundaryWithProfile` | **Partial.** Exercise a retained partial renewal across the existing tempo-drift schedule and runtime boundary with unchanged signed limits. Patch a demonstrated stale window/repeated effect; keep uncompleted roles independent. |
| PH-11 lifetime spend/reserve | `mainnet/evm_action.go: prerequisite/advance`; contract installation reserve admission; native/fleet recovery counters | **Partial/audit + live.** Join per-action maximum cost and retained attempts across an interrupted multi-action installation. Prove conservation and inherited ceilings without rerunning the already fixed first-CREATE reserve case. Actual funding and runtime fee evidence remain separate. |
| PH-12 settlement conservation | `mainnet/economic_emission_observe.go: observeEconomicEmission`; vault claim recovery; server NetEscrow/capture paths | **Partial + live.** Compose entitlement, carry, captured reserves, claim/revert recovery and owner recycle using actual contract/model paths; add a missing conservation assertion/adapter only after the path census. Native quantization and observed 10/90 outcomes still need exact runtime/live evidence. |
| PH-13 provider/operator/validator isolation | `miner/swarm.go: startSwarmMember`; `validator/transport_client.go: PostVerify/newRegisteredClient`; `validator/production_operator_authentication.go` | **Partial.** Add the provider progress source and cross-operator/client-key continuity control with exact identity domains; connectivity alone is not provider readiness. Confirm mainnet admission cannot choose a legacy transport path. |
| PH-14 activated policy and validator roles | `mainnet/bootstrap_chain_validators.go`; `mainnet/validator_activation.go`; root passive/active role commands | **Partial + live.** Finish actual composed preparation/admission with distinct UR/root capability domains. Production signatures, role devices, installed Safe authority and activation are required live inputs; no observation can supply them. |
| PH-15 actionable status/progress | `mainnet/monitor_output.go`; `mainnet/monitor_operator.go: conditions`; native-deadline/steering incident projection | **Partial/missing.** Extend expected provider and uncovered reserve/claim domains with bounded typed status and retained incident counters. Derive ETA from observed remaining work/rate with explicit unknown state, not from retry delay or assumed health. Delivered pages are a separate live gate. |
| PH-16 causal/composed qualification | `mainnet/source_lock.go`; exact release builders; current component/causal gates | **Partial.** Maintain the matrix of exact source, direct public tests, controls and exclusions. Run only affected composed seams after each source change. Preserve the broader server-model failure and optional skips; do not replace them with selected-pass counts. |
| PH-17 immutable plan-derived indexes | `mainnet/bootstrap_successor_execution_store.go: loadEvents`; validator attempt boundary/cache maps; fleet recovery plan projection | **Audit.** Trace owner/generation identity of each retained lookup used in resume. Force plan mutation or an externally advanced journal between lookup and use; add a bound immutable index where the actual caller lacks one. |
| PH-18 strict reader re-admission | `mainnet/runtime_snapshot.go: readRuntimeSnapshotAtIdentity`; `miner/fleet_mainnet_runtime.go: authenticateAt`; `validator/attempt_boundary_cache.go: chainAttemptBoundaryRPC.Validate` | **Partial/audit.** Inventory all production pooled/reconnected readers; force a route/genesis/runtime change after provisional work and before final return. Reuse the strict immutable view where already present and patch only unclosed boundaries. |
| PH-19 historical runtime purpose | `validator/production_runtime_history.go: releaseProductionRuntimeAt/releaseHistoricalRuntimeArtifactsAt`; fleet runtime view; server historical native capture | **Partial.** Join exact historical authority to the original parent execution versus post-state at upgrade blocks across fleet/contract receipt readers. Earlier-runtime decoding must never authorize a present send. |
| PH-20 distinct capacity dimensions | `mainnet/owner_trim_bounded.go: qualifyOwnerTrimWindowTarget`; claim queue limits; validator retained uploads | **Partial/missing.** Add a reviewed capacity profile separating active/funded slots, retained records/bytes, scan pages and resident work; make admission/reporting expose each exhausted dimension. Measure proposed fleet size separately. |
| PH-21 bounded component controller | `miner/swarm_control.go: startMemberOperation/stopMemberOperation`; `mainnet/repair_validator.go: resumeRepairValidator`; `mainnet/repair_active_validator.go` | **Partial/missing.** Existing per-member control is not a complete durable multi-component campaign controller. Force concurrent repeated requests and interruption after one child completes; add retained per-action ownership/partial resume only where missing, preserving unaffected children. |
| PH-22 service connection recovery | `validator/transport_client.go: waitForClientRegistration/retireClient/CloseAndWait`; miner/mainnet retry helpers | **Partial/audit.** Force transient registration/connection loss and a final failed incident through each public role, retaining final budget/cause. Distinguish auth rejection from transport failure and prove cleanup joins before connection replacement. |
| PH-23 capacity revisions | approved plan/budget schemas; `mainnet/owner_trim_bounded.go`; durable snapshot/native ceilings | **Missing joined profile.** Bind every finite dimension in the proposed capacity profile to an explicit revision and retained-head migration. Reject lower caps that cannot contain acknowledged history; no implicit truncation/reinitialization. |
| PH-24 lineage validation work | `mainnet/bootstrap_successor_execution_store.go: loadEvents`; original-authority readers; validator authority/history loaders | **Audit.** Count full decoding/authentication per distinct immutable approval during actual resume. Share a bounded authenticated lookup only after a duplicate-work witness; retain exact path/inode/change-time/source dependencies. |
| PH-25 deployment-stop joins | `miner/swarm.go: Run`; `miner/swarm_control.go: stopMembers`; `validator/transport_client.go: CloseAndWait`; activation/repair managers | **Partial + live.** Test public cancellation while owned starts, network registration and publication are in flight. Assert every admitted child joins and no late effect survives. Rehearse actual unit/cgroup stop later on the approved host. |
| PH-26 large evidence transport | `mainnet/safe_history_capture.go: captureSafeHistory`; `validator/release_attempt_upload_v2.go`; `server/strecovery/receipt_historical_native_capture.go: CaptureReceiptHistoricalNativeState` | **Partial.** Add selected-size public capture/upload/replay controls with bounded count/bytes, context cancellation and retained partial evidence. Test malformed/oversized input before allocation. Do not claim generic streaming support from a small fixture. |
| PH-27 coherent evidence/client-key activation | `validator/production_authority_history.go`; `validator/release_client_key_history_batch_authority_v2.go`; both operator authentication paths | **Partial + live.** Compose two independent operators and validator roles across a reviewed authority/client-key transition; force one stale role without activating a mixed policy. Actual owners still supply signed production domains. |
| PH-28 independent monitoring/repair | `mainnet/monitor_services.go: runMonitorServices`; operator/validator workers; stopped/active-validator repair interfaces | **Partial/missing + live.** Add provider and remaining economic/contract domains, then narrowly approved repair envelopes only where required. Deployment ingestion, independent expected-host roster, delivered alerts and on-call exercises remain live gates. |

## Mainnet gate mapping

| Gate | Production boundary and concrete next work | Live inputs kept separate |
| --- | --- | --- |
| MG-01 chain identity/runtime authority | `runtime-snapshot`, `finalized-snapshot`, source/profile readers and server GRANDPA verification exist. Wire an approved checkpoint into the exact runtime/route consumers and qualify mismatched route/checkpoint rejection; do not treat two Rao routes as independent finality. | Owned synced node/proof path, independent genesis/checkpoint/runtime authority and host cutover. The exact v470 user exception is planning-only. |
| MG-02 release/provenance | `buildSourceLock` and `buildReleaseInventory` plus exact repeated source/OCI workflow exist. Compose the final required SN/server/Connect successors after scoped source gates, preserving all earlier release IDs. | Independent compiler provenance, production config acceptance and distribution approval. |
| MG-03 retained evidence/recovery | Native/EVM journals and server `VerifyReceiptNativeState`, `VerifyReceiptFeeContexts`, `CaptureReceiptHistoricalNativeState` now exist in current server intake. Verify exact qualified handoffs and wire appropriate consumers. Transaction-attributed native debit/refund decoding remains missing; raw state proof and mapped account are not actual gas fee. | Approved checkpoint/runtime source, global signer custody and actual fee/finality witnesses. |
| MG-04 runtime compatibility | Shared immutable views and selected historical readers exist; fleet production `authenticateAt` remains one exact artifact. Implement consumed-capability/semantic selection and original receipt versus current-write separation, with causal upgrade tests. | Independent semantic/source approval for the runtime actually selected; never infer unknown economics from ABI equality. |
| MG-05 production topology/transport | Miner swarm, validator transport registration and operator API auth paths exist. Qualify the actual both-operator/four-role topology, strict routes, key domains and independent readiness; patch uncovered fallback/reconnect defects. | Actual hosts, role keys/devices, endpoint configuration and eligible independent validators. |
| MG-06 economics/settlement | `observeEconomicEmission`, owner recycle action/runtime, contract capture/carry/claims and server escrow paths exist. Compose conservation and exact native-rounding evidence; keep unresolved entitlement and runtime fee attribution explicit. | Approved economic policy, funded reserves, deposits and observed native/miner/owner outcomes. Recycling does not fund reserve. |
| MG-07 monitoring/repair | `monitorServicesPolicy` lacks provider roles while validator/operator observers exist. Implement bounded expected-provider progress/incident monitoring, then remaining reserve/claim/domain monitors and explicit repair scope. Existing xops unit/collector work needs new declaration assets. | Approved deployment, actual ingestion/delivery, independent roster and on-call/repair approvals. |
| MG-08 bootstrap/roles/contracts | Eight contract actions, original evidence anchor, root role, successor execution and validator activation paths exist. Finish fixed observer/successor composition and the sibling's retained-member/preparation adapters, then compose original Safe/evidence history. | Original approvals/devices/custody, actual installed contracts, Safe authority, funded accounts and signed activation. |
| MG-09 capacity/storage | Guarded owners and bounded report-only inventory exist; offline preparation and a joined capacity/retention profile remain missing. Complete known owner adapters and explicit restored-root rebind/verification, then test actual selected capacity. | Production volume/backup inventory, approved rebound declarations, restore rehearsal and resource sizing. |
| MG-10 accepted rollout | Exact qualification/plan machinery exists; no complete accepted mainnet composition. Add the final source/release/host rehearsal once prerequisites are implemented, preserving partial failures and authority gates. | Controlled approved activation, three native emission intervals, full 50,400-block settlement/claim cycle and actual acceptance. |

The launch remains blocked. Neither missing production keys nor an unapproved
checkpoint blocks the offline source work above. Conversely, a synthetic source
test cannot satisfy the independent authority, device, host or observed-economic
requirements in the last column.

The optional-context audit is also deliberately bounded. The earlier result
covered four named monitor/claim constructors, not every storage constructor.
A broader AST census on `653061a` finds 36 non-method variadic-context functions
and 117 direct same-package non-test calls, including 14 omitted arguments in
validator callers. Those leads include `NewProofStore` in measurement/release
startup and retained record/directory/stats helpers. Mainnet reachability and
independent admission must be traced before classifying any lead as a bypass;
import aliases, methods and dynamic calls are outside that syntactic census.
