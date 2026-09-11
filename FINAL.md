# sim-testnet finalization report

**Verdict: INCOMPLETE. Final acceptance: false.** This is a report in progress under [FINALIZE.md](FINALIZE.md) and [FINALIZE-COMPLETE.md](FINALIZE-COMPLETE.md). Evidence cutoff for the workload findings below is 2026-09-11 02:24 UTC. No complete strict producer/aggregate qualification, accepted release-candidate campaign, three final production epochs, or independent public replay is claimed.

A provisional workload soak is running with a retained 1,000-provider fleet and separately managed validator services. Two genuine validator-1 proof records were locally verified, one for each operator. Sustained proof production is still blocked: validator 2 has no complete proof records, and the first two files have not grown. Native epoch 1381 was explicitly deferred; the inspected local state contains no successful native steering submission. These observations are useful operational evidence and do not satisfy the full release acceptance criteria. See the [workload evidence review](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/workload-blocker.md) and [provisional recorder](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/provisional-r30/observational-soak.jsonl).

## Scope, source and approval

The current task is to finish the full finalization plan. The user's provisional testnet waiver permits the ongoing diagnostic workload and reuse of valid completed work; it does not establish that waived qualification criteria passed. Existing approvals, used plan lineage, receipts, transaction limits and immutable custody must be preserved. Exact approved versus cumulative actual spend remains to be reconciled into this report from the retained approval and transaction artifacts.

The isolated report workspace began from SN `6a8ebde22f11468ed5f5c34ed0e59756e41201fa`. The final candidate composition is being integrated; this report does not declare that starting revision or any live binary to be the final qualified release. The required final inventory is all release-root revisions, clean/upstream state, exact `release.lock` hash, binary SHA/build metadata, configuration/runtime manifests and authenticated finalized runtime identity. These fields are **pending**.

Local artifact links in this draft preserve provenance for review. They must be supplemented by secret-scanned, immutable public locators and hashes before final acceptance. A local file path is not public availability evidence.

## Phase and gate status

| Phase | Observed evidence | Acceptance status |
|---|---|---|
| Source integration and freeze | Isolated finalization workspace prepared; final composition/lock/push and final fences not established in this report. | PENDING |
| Complete strict producer | Latest located v4 run: native exit 1, owner exit 1, all 27 phases joined; `server-db`, `solidity` and final source fence failed. Source was SN `7818a20…`, not the current composition. | No current PASS |
| Complete aggregate | R12 native/outer exits 1; 19/21 phases passed; `sn-simulator-race`, `server-db` and final fence failed. Its candidate-fence wrapper was explicitly not a strict certificate. | No current PASS |
| Component evidence reuse | R12 unchanged contract/static-analysis receipts and a later exact server-proxy fixture recovery are retained; changed SN/Connect inputs require qualification appropriate to the final composition. | PARTIAL; not a whole gate |
| Formal launch admission | Current workload uses a provisional path and external validator services; paired plans, current strict preflight and the complete formal generation criteria are not established here. | PENDING |
| Provisional workload observation | Recorder reports 1,000 ready providers/20 swarms; both managed validator applications running with zero unit restarts at the reviewed cutoff; two validator-1 proofs and zero validator-2 proofs. | RUNNING, provisional only |
| `release-1.0` | No accepted five-epoch capture/semantic closure supplied for this report. | INCOMPLETE |
| `production-soak` | No accepted three-epoch production-policy capture/semantic closure supplied for this report. | INCOMPLETE |
| Public replay and peer review | No clean-checkout full-graph replay agreement supplied for this report. | PENDING |
| Delivery and shutdown | Final report/source/lock publication and no-autostart/actual-join closure remain outstanding. | PENDING |

The [gate evidence review](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/gate-evidence.md) records exact revisions, receipts and failure paths. R12's [cleanup result](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/candidate-aggregate-r12-terminal-closure/CLEANUP-RESULT.json) reports successful ownership cleanup while explicitly recording `native_full_gate_pass:false` and `source_fences_passed:false`; its cleanup status must not be interpreted as qualification success.

Full campaign acceptance requires five complete accelerated 300-block epochs plus a 150-block terminal window, followed by three complete production-policy `360/60/180/6` epochs plus their 180-block terminal window. The first partial observed epoch and terminal tails do not count as complete epochs. Preserve the native/EVM deadlines and finalized checkpoints, all 61 adversarial vectors and seven attributed concurrent actors, required sample/latency/error bounds, per-validator/per-operator proof growth, accounting invariants, intentional recovery evidence and owner-signed closure for both phases. None of those aggregate criteria is inferred from the provisional recorder's elapsed time.

## Verified provisional proof evidence

The existing `validator.VerifyProofRecord` function checked exactly the first two complete records against the configured validator and server public keys. The verifier ran locally without RPC or live-file writes and joined with actual exit 0. Both records passed server FINAL signature, validator FINAL co-signature, validator EXTEND signature, digest/path/identity/hop checks. Both are depth 8, coverage 7, settlement epoch 278.

| Validator/operator | Complete timestamp UTC | Snapshot bytes | Snapshot SHA-256 |
|---|---|---:|---|
| Validator 1 / operator 1 | 2026-09-11 01:31:27.801 | 2,385 | `c8ee74775a17ea882fd0907760980a7edcac38ca6be10fe5b5d13f8bccf5e2a3` |
| Validator 1 / operator 2 | 2026-09-11 01:31:24.799 | 2,372 | `ba652903f8a2f429ac4d23b8b8b32256f8340c04ee9af3a2edfb77f2bfc262d0` |

The [verification result](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/provisional-r31/first-two-proof-signatures/result.json) has SHA-256 `1a0ada991e77b2b8e25447a8cd955b5ccb31df35ab93a5f38e4ace296d6705fd`; the [native exit receipt](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/provisional-r31/first-two-proof-signatures/native-exit.json) records session 99452, actual exit 0 and joined ownership. These two records do not establish a complete epoch census, both validators' coverage, on-chain anchoring or formal RC success.

## Final evidence matrix

The matrix follows FINALIZE-COMPLETE §10. **PENDING** means complete acceptance evidence has not been verified for this report; it does not mean the underlying deployment or action is absent. Each final row needs exact artifact URLs/hashes and replay commands, plus transaction/extrinsic hashes, addresses/IDs and finalized block numbers/hashes for chain claims.

| # | Requirement | Current evidence and remaining acceptance work | Status |
|---:|---|---|---|
| 1 | Chain/deployment | Intended chain 945/netuid 521. Supply exact current genesis/runtime, deploy block, proxy/implementation/custody/probe addresses, code/slot and policy/version/effective-block evidence. | PENDING |
| 2 | Governance/custody | Preserve existing immutable reserve/vault and approvals. Supply authenticated owner/guardian, successful upgrade drill, unchanged-custody and cap evidence; identify mainnet governance delta. | PENDING |
| 3 | Operator pools | Two operator proof identities have partial local evidence. Supply registrations, UID ownership/status, signed API/history origins and independent assignment/traffic/quality evidence. | PENDING |
| 4 | Demand deposits | Supply required-versus-observed tier/rate deposits for both pools in every accepted epoch, underpayment effect and funded recovery receipts. | PENDING |
| 5 | 1,000-miner fleet | Provisional recorder reports 1,000 ready/20 swarms. Supply unique client manifest, full allocation, 202 candidate commitments/bindings/lineage, ownership/pruning and cleanup evidence. | PARTIAL |
| 6 | Top 200 | Supply both signed exact-rational/EMA rankings, 200 positive/two zero outcomes, native applied vector and promotion/demotion/fallback/terminal evidence. Current inputs lack sufficient positive confirmation coverage. | INCOMPLETE |
| 7 | Path proofs | Two validator-1 records locally verified above; validator 2 has zero at cutoff. Need every validator/operator/accepted epoch, signed census/cuts/envelopes, anti-replay/tamper checks and the required on-chain evidence hash commitments. | PARTIAL |
| 8 | CRv4 | Local epoch 1381 was deferred with no successful submission recorded. Need both validators' commit/reveal/application extrinsics, finalized blocks, mask/weight checks and independent consensus reconstruction. | INCOMPLETE |
| 9 | Head/tail accounting | Supply theta and realized sums, head native rewards/exclusion from tail pools, pruned-return and reconciliation evidence. | PENDING |
| 10 | Payout roots | Supply canonical artifacts for every operator/epoch, on-chain roots/events/state and exact leaf proofs; demonstrate wrong-leaf/wrong-operator `InvalidProof` with unchanged finalized state. | PENDING |
| 11 | Contract claims | Supply funded/claimed/liability totals, real `ClaimPaid` in both pools, recipient deltas, replay rejection and pending/outstanding amounts. | PENDING |
| 12 | Validator rewards | Supply native dividend/emission/stake deltas for both validators; do not substitute bounty evidence. | PENDING |
| 13 | Miner rewards | Supply selected native-head rewards, rejected zero allocations and actual tail-pool payments. | PENDING |
| 14 | Reserve | Supply principal/transfers/share-floor and live compounding evidence, 65% target/60% barrier and conservation. | PENDING |
| 15 | Settlement conservation | Reconcile captured = paid + escrow and escrow = pending funding + outstanding liability for all accepted epochs. | PENDING |
| 16 | Adversaries | Supply all 61 vectors/seven actors, attributable seeds/samples/latency/errors, applied/restored faults and a clean attempt ledger. | PENDING |
| 17 | Runtime | Provisional external services and prior topology failures are retained. Supply final binary/config/generation inventory, all error/restart accounting, bounded intentional recovery and no reboot autostart. | PARTIAL |
| 18 | Public history | Supply both archive locators, complete graph sizes/hashes, replica fetch/rehash and secret scan; owner completion/supplement signatures and immutable manifests. | PENDING |
| 19 | Final epochs | Supply five clean accelerated and three complete production-policy epochs with correct terminal windows and signed `capture_closed`/`semantic_verified` for both phases. | INCOMPLETE |
| 20 | Public replay | Supply both authorized signed discovery paths, pinned native/EVM transcript, clean-checkout replay commands and matching verdict/artifact hashes from independent review. | PENDING |

The later explicit on-chain-hash decision in FINALIZE-COMPLETE §10.1 controls the older matrix's “pending decision” wording. Full proof bytes remain signed off chain and publicly available; commitment hashes alone establish neither availability nor signature truth. Required signed settlement-tail closure (§10.2) and bounded chunk/census storage and replay (§10.3) must also be demonstrated, including complete accepted projections and legacy-byte/signature preservation. Their final implementation and evidence status are **pending review**.

## Anomalies, exclusions and recovery

| Finding | Retained evidence and consequence | Closure status |
|---|---|---|
| Strict producer failed | Server DB fixture failures; simulator race package deadline; final source fence failure. All actual phase/owner results remain preserved in the gate review. | Not a passing gate |
| Aggregate R12 failed | Full simulator race package timed out at 90 minutes; server-proxy MMDB fixtures were missing; final remote-ref source fence failed. Candidate wrapper is explicitly not a strict certificate. | Not a passing gate |
| Specific proxy fixture recovery | Same captured proxy binary passed all 84 normal roots and two exact 20-root confirmations with actual exits 0 and joined cleanup. Original wrapper failure is retained. | Bounded component recovery only |
| Tunnel setup race | Prior per-request lazy setup could not accommodate processed key registration within its expansion budget. Explicit pinned-client change reached real `/verify`; the first two signed production workload proofs then completed. | Causal repair has partial live evidence; sustained workload still blocked |
| Repeated registration and reserve-boundary deadlines | Fresh intervals since 02:10 contain registration and trail/RPC timeouts, without counted HTTP 429. No proof growth after the first pair. The workload review records current causal investigation and selected follow-up. | OPEN |
| Insufficient native inputs / stale settlement | Epoch-1381 local inputs contain no scored providers for validator 1 and only a zero-confirmation provider for validator 2. Signed inputs referred to settlement 278 after active settlement reached 279; both validators explicitly deferred with no submission. | OPEN; not native completion |
| Provisional topology exception | Validators run in separate managed services after the retained supervisor exhausted their restart allowance. This is authorized provisional operation but does not silently satisfy the formal single-generation criteria. | Formal admission unresolved |

The two successful proof records were produced during a bounded pause of four background roles, 01:29:12.385272–01:33:18.486146 UTC. This supports shared-capacity relief as a causal test; it does not establish that capacity is the sole remaining cause. Background roles were resumed. Preserve the raw logs, counters, original failures, pause owner exit and subsequent results; do not discard failed attempts or label a frozen historical prefix as a current error.

The full excluded-attempt inventory, causal fixes, regression commits and current-composition rerun evidence remain to be assembled. A formal passing run requires a clean attributable anomaly ledger; this draft does not certify one.

## Public replay, completion and delivery

Required final artifacts include `public.json`, `plan.json`, `journal.jsonl`, `config.redacted.yml`, process logs, native/EVM receipts, operator/validator artifacts, `assertions.json`, `adversaries.json`, `anomalies.json`, `analysis.json`, self-contained `analysis.html`, `junit.xml`, `result.json` and `complete.json`. Completion requires authentic owner-signed capture and semantic verification, the complete immutable graph, secret scanning, both archive replica readbacks and exact hashes. Missing/truncated logs, unresolved transactions, missing artifacts or violated invariants preclude a successful result.

Independent review must use a clean compatible checkout, no simulator DB or wallet secrets, either operator's latest authorized signed manifest and exact run ID, and all pinned Substrate/EVM reads needed to reproduce code/implementation, deposits, roots, claims, reserve and native UID/weight/reward/CRv4 results. Both analyses must match this report's final verdict and hashes. Actual replay commands and results are **pending**. Loopback origins `127.0.0.1:18081` and `127.0.0.1:18082` require same-host access or explicit forwarding preserving signed origins; they do not demonstrate off-host availability.

After evidence and peer review finish, retain public history, chain deployments and claim paths. Stop local processes no longer needed through the retained harness state, join actual owners/children, record terminal results and prove user units are not enabled and dependency restart policies are `no`. Do not retire a deployment without a separate explicit retirement request. Final source/report/release-lock commits and push receipts are **pending**.

## Mainnet deltas

Testnet RC completion would not authorize mainnet transactions. Record and close the applicable separate requirements: independent private archival RPC and sustained-load capacity; DNS/TLS and authorized public history; Loki/Grafana/general statistics and alerts; 2-of-3 Safe/timelock and recovery drills; distinct mainnet keys/caps and an approval-identical mainnet plan; secret rotation and least-privilege storage; backup/restore and RPC/archive outage exercises; runtime, fee/burn/liquidity and economic review; unresolved anomaly/security closure; and the prescribed mainnet rehearsal and capacity evidence. Do not reuse testnet approval or credentials as mainnet authority.

The operational [requirements checklist](/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/requirements.md) tracks the remaining acceptance obligations. This report must be updated from actual frozen evidence as they close; elapsed provisional run time and successful process starts cannot replace them.
