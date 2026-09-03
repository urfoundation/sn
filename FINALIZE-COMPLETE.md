# Release 1.0 testnet completion handoff

Status: live working document, first written 2026-09-03 UTC before source
freeze. Refresh every item marked FREEZE-UPDATE after the final gates and
commits. This document is the operational continuation point if another agent
has to finish the testnet campaign.

The objective is not merely to make the simulator exit successfully. The
objective is to produce independently replayable, on-chain evidence that every
in-scope mechanism in WHITEPAPER.md works on Bittensor testnet netuid 521 under
the complete adversarial load, and then to resolve every anomaly before making a
mainnet-readiness claim.

## 1. Non-negotiable completion definition

Testnet is complete only when all of the following are true:

1. The release source is frozen, committed, rebased onto current upstream,
   pushed, and exactly represented by deploy/testnet/release.lock.yml.
2. The launch-critical producer gate and the complete release gate both pass
   against that exact multi-repository checkout, including ordinary, race,
   PostgreSQL/Redis, Foundry, Slither, generated-artifact, infrastructure, and
   patch-hygiene checks.
3. Two independent read-only plan builds against the authoritative attempt-4
   state produce byte-identical plans, the same plan hash, and the same bounded
   cumulative spend.
4. The exact approved plan is resumed/launched. The real server/connect,
   miner, and validator modules run; no substitute CLI-only implementation is
   accepted.
5. release-1.0 records five consecutive complete 300-block settlement epochs
   plus terminal finalization. production-soak then applies the
   future-effective production policy and records three consecutive complete
   360-block epochs plus its 180-block settlement window. The native lifecycle
   handoff/tail may extend either phase only as already bounded in FINALIZE.md;
   tail blocks do not count as accepted epochs.
6. All 56 adversarial actors run concurrently with the happy path. The attempt
   ledger is clean: no unexplained error, panic, failed invariant, missing
   sample, process restart, unresolved latency signal, or state drift.
7. Both phases reach owner-signed capture_closed and semantic_verified states.
   Every public artifact is content-addressed, secret-scanned, replicated to
   both operator archives, fetched back, and hash verified.
8. FINAL.md is generated from the frozen capture, independently replayed
   against public chain checkpoints, and covers every evidence item in section
   10 below.
9. A clean checkout can discover the signed deployment manifest from either
   operator API, run inspect/analyze without local secrets, and obtain the same
   verdict and evidence hashes.
10. The final commits, reports, and release-lock revisions are pulled/rebased
    and pushed. No simulator service or dependency is enabled across host
    reboot.

Any failure invalidates the affected attempt. Diagnose the root cause, add a
deterministic regression following connect/CODESTYLE.md, inspect adjacent
failure modes, rerun the relevant focused normal/race tests, rerun the release
gate, and resume only through the authenticated attempt/journal machinery. Do
not waive a result as public-RPC noise or adversarial noise.

## 2. Current authoritative facts

These facts are safe to use for continuation. Re-read them before mutation.

- Workspace root: /home/by/urnetwork
- Primary repository: /home/by/urnetwork/sn
- Network: Bittensor public testnet
- Netuid: 521
- Substrate RPC: wss://test.finney.opentensor.ai:443
- EVM RPC: https://test.chain.opentensor.ai
- Expected EVM chain ID: 945
- Private fallback authority: sim-testnet:9944; it is not the active
  operational endpoint while the private node catches up.
- MinIO endpoint host: 172.28.208.177, using the existing server/blob service.
- PostgreSQL and Redis: local, simulator-managed containers modeled after
  server/local. Do not redirect them to shared LAN services.
- Operator API origins: the two loopback origins resolved from the
  testnet-prefixed vault setting.
- Wallet directory: vault/subtensor/wallets/testnet_wallet.
- Wallet password file: vault/subtensor/testnet_wallet.password. Never print,
  copy into a command line, commit, or include it in evidence.
- Vault configuration: vault/main/st.yml. Testnet inputs use testnet-prefixed
  keys; unprefixed keys are mainnet-only.
- Testnet governance: single owner with a distinct guardian and bounded value.
  Mainnet governance remains 2-of-3 multisig.
- Topology: 2 operators, 1,000 miners, 20 miner swarms, 2 validators, 202
  independently keyed four-client candidate fleets, exactly 200 head slots,
  and 192 long-tail miners.
- Authoritative existing state directory:
  /home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4
- Preserve that directory and its append-only journal. Do not delete it or
  start a fresh deployment merely to bypass a reconciliation failure.
- The state directory was approximately 505 MiB when this document was first
  written. Its journal ended at sequence 10040 with topology.launch
  postcondition_verified from an earlier, now-stopped source generation.
  Current-plan launch readiness must be proved again.
- No campaign is currently running. The prior user-systemd service is inactive
  and not enabled/installed. This is intentional: the simulator must not
  survive a host reboot without an explicit resume.
- Existing attempt-4 deployment:
  - coordinator proxy:
    0x8e7d2f9a77fec95c7e4875b0bd858d5de2b6def8
  - settlement vault:
    0x09d5d7a5c3e94b6ae42b09889a1cee50f970fc5e
  - reserve sink:
    0x376f98bd7c6b334f7f1cb2685e0970a18bfe7d28
  - current recorded coordinator implementation:
    0x8c033865c1cd387a0fa5a8f3369908a20c5a54e4
  - deployment block: 7,900,646
- The current source must create a plan revision which preserves authenticated
  carried history and upgrades only what the new release lock requires. Never
  redeploy immutable custody simply because the implementation changed.

FREEZE-UPDATE repository revisions:

| Repository | Branch | Revision at first draft | Upstream/state |
|---|---|---|---|
| sn | main | 1cfe20cd883acae79e4a42697d41f1dc7043cbf1 | dirty; 148 paths before freeze |
| server | main | 64366fb526e60ac9320f0a75548e54b38413df09 | dirty; upstream 770051fcb587344dbcfaa4645af4ffacbfe5b08e; rebase required |
| vault | main | b42b8df3491c3030452ef9896413f5d3d18594eb | clean/current |
| config | main | 6e87a696d45463689da895fcf069395cb19fb484 | clean/current |
| connect | main | 450715cd27eb8d107b63d12d91656717c5bcd13c | clean/current |
| sdk | main | 253a24e2efb35b77f92029274b908f777ce3acfe | clean/current |
| glog | master | 93fe11b3e490abde48763b2dd0cc4e9b1e282ab6 | clean/current |
| goidenticons | main | 325750b38314313dc5f44c880ab6f12f6c1ecb3c | clean/current |
| proxy | main | 55a1bff70e94a2f8c0a538c10c24c86db0f823f3 | clean/current |
| userwireguard | master | 85fb1ca4086fa5dbfcda526bec7a17a894e691b9 | clean/current |
| xops | main | fbd291a1849d5769e67efe278c2f4e5da65275aa | clean/current |

The release lock in the pre-freeze tree is stale by design and must not be
treated as an approval. Its hashes and audited commits must be refreshed only
after the source and dependency checkouts are final.

## 3. Current implementation/gate continuation point

At first draft, source freeze is not complete. The expensive
TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper regression is
being used as a narrow walk through the complete 1,000-miner semantic fixture.
The following real defects have already been corrected in the working tree:

- lifecycle cleanup evidence now uses the current v2 schema;
- lifecycle payout evidence binds the signed inner payout content hash;
- singleton and paired payout state records are indexed safely;
- operator ownership follows whole-fleet assignment rather than the obsolete
  alternating-miner fixture;
- payout fixtures cover every lifecycle client while excluding active heads;
- lifecycle variants are canonicalized identically in construction and replay;
- omitted rejected UIDs in Subtensor's sparse weight vector are materialized as
  explicit zero evidence, while a missing positive selected UID still fails;
- lifecycle schedule checkpoints use the canonical fixture block hash rather
  than a second hard-coded hash for the same height;
- the mock public-chain reader returns the real complete submitted native
  vector, allowing lifecycle validation to test candidate entries without
  deleting pool entries.

The next observed narrow-test failure at the time of writing is:

    public native reward UID 1000 emission/stake/incentive/dividends mismatch at 400

Continue by comparing the historical reward rows and the full
FinalCollectedRewardStakeSnapshot artifacts after attachFinalFleetLifecycleFixture
rewrites epoch heads and historical fleet ownership. The public test reader
must model the frozen chain snapshot, not a lifecycle-only projection. Fix the
fixture or production logic according to the real wire semantics; do not weaken
the verifier. Add a deterministic adjacent test for any newly identified root
cause. Then rerun:

    go test ./sim-testnet \
      -run '^TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper$' \
      -count=1 -timeout=30m

After it passes, run all final semantic tests ordinary and race before widening
to the producer and aggregate gates.

The complete server model package previously reached a 30-minute command
deadline amid full-suite database churn. The test active at timeout,
TestIndexSearchLocationsSkipUnchanged, passed alone in about 11 seconds and
PostgreSQL stayed healthy. This does not count as a complete suite pass. Rerun
the unchanged package with an explicit adequate timeout after the server rebase,
and treat any repeat as a root-cause item rather than merely extending the
deadline.

## 4. Source-freeze procedure

Perform these steps in order.

### 4.1 Finish focused correctness

1. Make the complete semantic supplement regression pass.
2. Run all TestFinalSemantic tests ordinary.
3. Run focused race coverage for every modified lifecycle, capture, replay,
   settlement, attempt-ledger, and publication path.
4. Run the full sim-testnet package ordinary and race.
5. Run validator attempt/measurement/steering suites ordinary and race.
6. Run gofmt on changed Go files, forge fmt on Solidity, generated ABI/binding
   checks, go vet, and git diff --check.
7. Preserve exact failure output and the deterministic regression for each
   defect in FINALIZE.md.

Useful focused commands:

    go test ./sim-testnet -run '^TestFinalSemantic' -count=1 -timeout=30m
    go test -race ./sim-testnet -run '^TestFinalSemantic' -count=1 -timeout=45m
    go test ./validator -count=1
    go test -race ./validator -count=1
    go vet ./...
    git diff --check

### 4.2 Run the launch-critical producer gate

Run from /home/by/urnetwork/sn:

    PATH=/home/by/.foundry/bin:$PATH \
      ./scripts/test-release-1.0-producer-gate.sh

This gate must cover the complete compiled simulator/validator graph, signed
attempt transitions, terminal cuts, atomic settlement, lossless capture,
process-log fencing, direct publication, operator proof APIs, isolated
PostgreSQL/Redis evidence paths, deployable contract behavior, generated
contract/ABI freshness, and the final source/release-lock fence.

Because the release lock is intentionally stale while code changes, run the
body gates first if necessary, refresh the lock only after all source is
settled, then rerun the exact checked-in script end to end.

### 4.3 Run the complete aggregate gate

With local PostgreSQL/Redis healthy:

    PATH=/home/by/.foundry/bin:$PATH \
      RUN_SERVER_DB_TESTS=1 \
      ./scripts/test-release-1.0-local.sh

Required results:

- all SN Go packages pass;
- full sim-testnet race suite passes within its checked-in 15-minute deadline;
- all four deployable Solidity roots have zero Slither high/medium findings;
- forge fmt/build/test pass, with all 156 or more current tests passing;
- generated contract payload, storage layout, ABI, and Go bindings are fresh;
- operator pure and DB-backed suites pass;
- complete server proxy package passes under its explicit 20-minute bound;
- affected Connect/SDK normal and race suites pass;
- xops Subtensor infrastructure regressions pass;
- every release repository passes staged and unstaged diff checks;
- the final release-lock checkout test passes after every other long gate.

Do not call source frozen until both gate scripts pass from the same unchanged
filesystem tree.

### 4.4 Commit, update upstream, revalidate, and push

The user explicitly requested committing all changes after source freeze.

1. Review every dirty and untracked path in sn and server. Exclude generated
   run state, logs, secrets, caches, and build products; include every intended
   implementation, test, documentation, ABI, contract, config, and gate change.
2. Commit the server changes and the SN changes in their respective
   repositories with release-1.0 checkpoint messages.
3. Fetch and pull/rebase each dirty repository. Server is known to be behind
   upstream and has overlapping changed paths, including controller Connect/ST
   code, network-client model code, and taskworker tests. Resolve semantically;
   never discard either side wholesale.
4. Rerun all conflict-adjacent tests, then both full gates.
5. Refresh release.lock.yml with the final commit pins and observed source,
   generated artifact, interface, infrastructure, and config hashes. Review
   every mismatch; do not mechanically bless an unexplained digest.
6. Commit the refreshed lock and this updated handoff document.
7. Pull --rebase once more, rerun the final lock/patch gates, then push each
   changed repository.
8. Record the pushed full revisions in section 2 and the immutable gate results
   in section 12.

If upstream moves after a passing gate, the checkout is no longer frozen.
Rebase, rerun affected gates plus the final complete gate, and issue a new lock.

## 5. Read-only prelaunch procedure

Use the authoritative state explicitly in every command:

    cd /home/by/urnetwork/sn
    go build -trimpath -o build/sim-testnet ./sim-testnet

    ./build/sim-testnet doctor \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json \
      > /tmp/ur-subnet-testnet-doctor.json

Doctor must be ready. The public operational/comparison backend distinction may
be reported as the documented non-hard public-testnet limitation; every other
check must pass. Specifically verify wallet identity, netuid ownership, runtime
452 and exact Wasm hash, chain/genesis identity, activation flags,
hyperparameters, budget and balance, MinIO, local Docker, systemd, port
availability, source lock, and authoritative carried history.

Build the plan twice without any intervening write:

    ./build/sim-testnet plan \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json \
      > /tmp/ur-subnet-testnet-plan-a.json

    ./build/sim-testnet plan \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json \
      > /tmp/ur-subnet-testnet-plan-b.json

    cmp /tmp/ur-subnet-testnet-plan-a.json \
        /tmp/ur-subnet-testnet-plan-b.json

Review and record:

- plan_hash;
- schema and authenticated ancestor/revision lineage;
- action count and every new/carry/superseded action class;
- cumulative TAO rao, alpha rao, EVM gas wei, registrations, and subnet
  creations;
- exact coordinator implementation upgrade and unchanged immutable vault and
  reserve addresses;
- no unexpected registration, subnet creation, duplicate conviction, custody,
  alpha transfer, or fleet mutation;
- no spend exceeds the testnet-prefixed vault ceilings;
- current finalized economic facts still satisfy reserve target/minimum,
  independent validator stake, retained source alpha, burn, fee, and gas bounds.

The user has already authorized executing the ready testnet plan; no additional
spend confirmation pause is required. The hash must still be mechanically exact
and recorded here before apply.

FREEZE-UPDATE approved plan:

- plan hash: pending
- plan file SHA-256: pending
- action count: pending
- maximum cumulative TAO rao: pending
- maximum cumulative alpha rao: pending
- maximum EVM gas wei: pending
- maximum registrations: pending
- subnet creations: must remain 0
- finalized native/EVM plan checkpoints: pending

## 6. Exact launch/resume

Set the reviewed hash only after section 5 succeeds:

    SIM_TESTNET_APPROVED_PLAN_HASH=0xREPLACE_WITH_REVIEWED_HASH

Resume the existing attempt rather than creating a new state root:

    ./build/sim-testnet resume \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --apply --plan-hash $SIM_TESTNET_APPROVED_PLAN_HASH \
      --detach

If the revision requires setup before topology readiness, resume is still the
preferred journal-aware entry point. Use launch with the same arguments only if
the current CLI explicitly reports that launch is the required operation:

    ./build/sim-testnet launch \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --apply --plan-hash $SIM_TESTNET_APPROVED_PLAN_HASH \
      --detach

Never run Forge scripts, btcli writes, ad hoc SQL, or direct contract calls as a
substitute for a failed action. The harness must reconcile signed bytes, nonce,
broadcast, inclusion, finality, and postcondition itself.

Launch must prove:

- carried history replays exactly;
- immutable custody is unchanged and the approved implementation is active;
- managed PostgreSQL/Redis containers have exact release/config labels and
  restart policy no;
- runtime config manifest covers every static input with no extra/symlinked
  file;
- all 32 expected long-lived processes start in one current supervisor
  generation;
- both operator APIs and direct Connect ingress are reachable;
- all 1,000 miners and both validators produce fresh proofs;
- precompile-conformance passes;
- topology smoke and process-log gates are clean;
- the user unit is started but not enabled.

Immediately save read-only status and inspect output. Do not edit evidence:

    ./build/sim-testnet status \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json

    ./build/sim-testnet inspect \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json

## 7. Live campaign

Run the composite orchestration so it adopts only authenticated clean phase
markers and resumes the first incomplete phase:

    ./build/sim-testnet scenario \
      --name release-candidate \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --apply --plan-hash $SIM_TESTNET_APPROVED_PLAN_HASH

The command is designed to run release-1.0 and production-soak without a manual
pause. If interrupted, invoke the same exact command and hash. Do not select a
new run ID and do not delete an open attempt.

Expected irreducible live duration from a clean acceptance start:

- release fixed geometry: 5 x 300 + 150 = 1,650 blocks;
- production fixed geometry: 3 x 360 + 180 = 1,260 blocks;
- combined fixed geometry: 2,910 blocks, about 9 hours 42 minutes at 12 seconds
  per block;
- boundary alignment plus native CRv4 release-handoff/terminal-active tails:
  expected total about 12 to 15 hours;
- semantic analysis is asynchronous after capture closure and should overlap
  the next live phase.

The release phase must not be marked complete merely because five settlement
epochs elapsed. Its first three causal native milestones and terminal binding
must also satisfy the runtime-452 schedule. Production must obtain the later
terminal-active native decision. The bound is evidence-driven from live tempo
and reveal-period state.

Monitor without bypassing the shared public-provider egress gate:

    ./build/sim-testnet status \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json

    ./build/sim-testnet tail \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4

Do not run uncoordinated high-rate RPC polling while the campaign is active.

## 8. Parallel work allowed during live protocol waits

After a phase reaches capture_closed, its graph is immutable. Run these tasks
in parallel with the next live acceptance window:

1. typed semantic reconstruction from captured files;
2. public Substrate/EVM checkpoint replay;
3. transcript sealing and public artifact replication/readback;
4. FINAL.md rendering;
5. clean-checkout secretless inspect/analyze;
6. complete aggregate test gate if it has not already been recorded against the
   frozen commit;
7. draft the mainnet delta/risk appendix.

Do not perform any work in parallel which mutates the same state directory,
journal, operator database, chain nonce, plan, or evidence path. Read-only
analysis must use the capture-pinned public/deployment/runtime manifests, not a
mutable preparation pointer from the next phase.

## 9. Failure protocol and service escalation

On any nonzero exit, panic, anomaly, missing artifact, restart, RPC discrepancy,
latency breach, or unexpected state:

1. Stop issuing new mutations. Preserve the state directory, journal, process
   logs, attempt ledger, receipts, and captured RPC exchanges.
2. Use status/inspect/analyze and targeted read-only chain queries to determine
   whether a transaction was unsigned, signed, broadcast, included, finalized,
   or postcondition-verified.
3. Reduce the failure to a deterministic local test. Follow
   connect/CODESTYLE.md: assert the causal invariant, not timing luck.
4. Search for similar and adjacent ownership, cancellation, replay, epoch,
   sparse-vector, hash-domain, overflow, finality, and recovery defects.
5. Patch the root cause. Run focused ordinary and race tests, then the producer
   gate and any affected aggregate sections. Refresh/recommit the release lock
   if source changes.
6. Rebuild two identical read-only plans. Resume the same authenticated attempt
   only if its state machine permits it.
7. Record the incident, evidence, root cause, regression, adjacent audit, and
   clean rerun in FINALIZE.md and FINAL.md.

Raise an infrastructure issue to the user immediately if PostgreSQL, Redis,
MinIO, Subtensor/public RPC, Loki, or Grafana is actually misconfigured or
broken. Distinguish an application-test timeout from a service failure with
health, logs, and an isolated probe. Loki/Grafana are observability support;
their absence must be reported and resolved before mainnet readiness even when
release-critical signed evidence remains available elsewhere.

## 10. Required FINAL.md evidence matrix

FINAL.md must give enough public identifiers, transaction/extrinsic hashes,
block numbers and hashes, contract addresses, artifact hashes/URLs, and replay
commands for a separate agent to validate each claim on chain.

| Whitepaper mechanism | Minimum independently verifiable evidence |
|---|---|
| Chain/deployment identity | chain ID 945, genesis/runtime 452 identity, netuid 521, deployment block, proxy/implementation/vault/reserve/probe addresses, runtime code hashes, ERC-1967 implementation slot, policy version/hash/effective block |
| Governance/custody | testnet single-owner and guardian identities, successful governance upgrade drill, unchanged immutable vault/reserve custody, owner/value caps; explicit mainnet 2-of-3 delta |
| Operator pools | both NO registrations and UIDs, ownership, pool status, API origin, public history, independent assignment and traffic/quality evidence |
| Demand deposits | tier/rate inputs, required versus observed conviction for both pools in every covered epoch, deposit transaction/event, dishonest underpayment, zero pool weight/penalty, corrected deposit, and positive-weight recovery |
| 1,000 miners and fleets | public topology/identity manifest, 1,000 unique clients, whole-fleet operator assignment, 202 candidate commitments/manifests/bindings, generation lineage, registration/prune ownership, and cleanup evidence |
| Top-200 selection | each validator's independently signed 202-candidate ranking, exact rational/EMA inputs, 200 selected positive weights, 2 rejected zero weights, native applied vector at an immutable block, promotion/demotion and fallback/provider/terminal lifecycle |
| Validator path proofs | both validator identities/stake/permit/trust, fresh signed proof for every validator/operator/epoch pair, measurement and envelope hashes, cut/checkpoint lineage, anti-replay and invalid/tampered rejection |
| CRv4 | commit, reveal, application extrinsics and finalized blocks for both validators, exact vector hashes and native state, max-weight cap, self-dealing mask, independent consensus behavior |
| Head/tail split | policy theta, realized head/pool sums, selected/rejected rewards, active-head exclusion from pool leaves, pruned-provider return to pool eligibility |
| Pool payout Merkle roots | canonical payout artifacts for each NO/epoch, content hash and on-chain root transaction/event/state, every tested leaf/proof, wrong-leaf/wrong-NO invalid-proof eth_call with unchanged state |
| Contract payouts | funded/total/claimed liability, successful ClaimPaid receipts for both pools, recipient/balance deltas, duplicate/replay rejection, pending/outstanding accounting |
| Validator rewards | native dividends/emission/stake deltas at pinned before/after native blocks for both validators; no out-of-scope effort bounty claim |
| Miner rewards | native head emission/incentive/stake evidence for selected heads, zero native head channel for rejected candidates, pool claim payout evidence for tails |
| Reserve/buyback | principal additions, stake transfer receipts, exact share-floor allowance, reserve live stake and compounding yield, 65% target/60% barrier evidence, conservation before/after |
| Settlement conservation | total captured = total paid + escrow accounted; escrow accounted = pending funding + outstanding liability at pinned contract checkpoints |
| Adversarial resilience | all 56 actors, seed/matrix hash, samples, latency/error metrics, applied/restored faults, no cross-operator or cross-validator contamination, clean attempt ledger |
| Process/runtime resilience | exact binaries/config manifest, current supervisor generation, expected process inventory, zero unexplained restarts/panics/errors, bounded intentional fault recoveries, stopped-on-reboot policy |
| Public artifact history | both operator archive roots, MinIO locators, byte sizes/content hashes, replica readback, secret-scan result, owner completion/supplement signatures, capture/input/evidence manifests |
| Three clean production blocks | three consecutive complete production UR epochs under 360/60/180/6 policy with zero failures, plus the preceding five clean accelerated epochs; clearly separate lifecycle tail blocks |
| Independent replay | public manifest locators for both NOs, transcript hash and pinned JSON-RPC exchanges, clean-checkout inspect/analyze commands and matching result hashes |

Also include:

- all failed/pre-campaign attempts and why they do not count;
- every anomaly found during this campaign and its root-cause/regression commit;
- exact frozen revisions and release-lock hash;
- plan hash and maximum/cumulative actual spend;
- start/end UTC timestamps and native/EVM terminal checkpoints for both phases;
- explicit limitations of shared public RPC and the remaining private-node,
  archive, observability, operational, and multisig mainnet deltas.

Do not infer success from a local summary. Every material statement must link to
signed artifacts and, where applicable, immutable chain state.

## 11. Independent peer-review procedure

From a clean compatible checkout with no simulator state or wallet secrets:

1. Obtain either signed deployment-manifest URL from
   public/deployment-manifest.locators.json or FINAL.md.
2. Build sim-testnet from the exact frozen SN commit.
3. Run:

       ./build/sim-testnet inspect \
         --config sim-testnet/testnet.yml \
         --manifest 'https://OPERATOR/sn/evidence?hash=sha256:...' \
         --format json

       ./build/sim-testnet analyze \
         --config sim-testnet/testnet.yml \
         --manifest 'https://OPERATOR/sn/evidence?hash=sha256:...' \
         --format json

4. Repeat with the other operator locator.
5. Fetch and rehash the full evidence graph. Verify owner/operator/validator
   signatures, artifact envelopes, completion and supplement linkage.
6. Replay every pinned Substrate and EVM query at its recorded block hash.
7. Independently query contract code/implementation, pool epochs, deposits,
   roots, claims, reserve/accounting, native UIDs/weights/rewards, and CRv4
   receipts.
8. Confirm both analyses produce the same release verdict and hashes as
   FINAL.md.

Any peer-review discrepancy reopens testnet validation.

## 12. Freeze and execution record

FREEZE-UPDATE this table as work completes.

| Item | Result | UTC / immutable reference |
|---|---|---|
| Narrow 1,000-miner semantic supplement test | pending | current failure recorded in section 3 |
| All final semantic ordinary tests | pending | |
| All final semantic race tests | pending | |
| Full sim-testnet ordinary | pending | |
| Full sim-testnet race | pending | |
| Producer gate | pending | |
| Aggregate gate with DB tests | pending | |
| Foundry | prior 156/0/0; final pending | |
| Slither | prior 0 high/medium; final pending | |
| Server full DB/package qualification | pending after rebase | |
| SN frozen commit | pending | |
| Server frozen commit | pending | |
| Other changed repo commits | pending | |
| Release-lock hash | pending | |
| Two identical plan builds | pending | |
| Approved plan hash/spend | pending | |
| Resume/launch | pending | |
| release-1.0 capture_closed | pending | |
| release-1.0 semantic_verified | pending | |
| production-soak capture_closed | pending | |
| production-soak semantic_verified | pending | |
| release-candidate verdict | pending | |
| FINAL.md | pending | |
| Independent replay | pending | |
| Final pull/rebase/push | pending | |

## 13. Final shutdown and mainnet gate

After evidence and peer review complete, preserve public history and chain state.
Stop local processes only if they are no longer needed:

    ./build/sim-testnet stop \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4

Confirm the unit and Docker dependencies are not enabled for restart. Do not run
retire unless a separately reviewed retirement plan is explicitly requested.

Testnet success is necessary but not by itself mainnet authorization. Before
mainnet:

- repeat archive-depth, sustained-load, and independent-observer checks against
  the synced private Subtensor node;
- configure and prove Loki/Grafana/general-stats production observability;
- replace testnet single-owner governance with the reviewed 2-of-3 multisig and
  complete signer/guardian operational drills;
- fill and independently review unprefixed mainnet vault values and spend caps;
- perform production secret, backup/restore, key rotation, incident-response,
  rollback, contract verification, and monitoring drills;
- compare live mainnet runtime/hyperparameters/precompiles/fees/burn/liquidity
  against the release assumptions;
- resolve every item in the FINAL.md mainnet-delta section;
- create a separate byte-identical mainnet plan approval. Never reuse the
  testnet plan hash or keys.

The final handoff is complete only when another agent can start with this file,
authenticate the exact frozen checkout and attempt-4 state, and execute the
remaining steps without relying on private conversational context.
