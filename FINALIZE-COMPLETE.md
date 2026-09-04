# Release 1.0 testnet completion handoff

Status: live working document, first written 2026-09-03 UTC and last reconciled
2026-09-04 UTC before the final source freeze. Refresh every item marked
FREEZE-UPDATE after the final commits and gates. This document is the
operational continuation point if another agent has to finish the testnet
campaign. Historical green gates in FINALIZE.md are not approval for the
current candidate; section 12 below is the only current execution record.

The objective is not merely to make the simulator exit successfully. The
objective is to produce independently replayable, on-chain evidence that every
in-scope mechanism in WHITEPAPER.md works on Bittensor testnet netuid 521 under
the complete adversarial load, and then to resolve every anomaly before making a
mainnet-readiness claim.

Execution-agent policy: use `gpt-5.6-terra` at reasoning effort `max` to run
tests and release gates. On any failure, preserve the exact output and use
`gpt-5.6-sol` at reasoning effort `max` to diagnose the root cause, audit
similar/adjacent paths, implement the fix, and add its deterministic regression.
Return the resulting tree to Terra for the focused and widened reruns. This
model assignment never weakens a gate or expands testnet write authority.

## 1. Non-negotiable completion definition

Testnet is complete only when all of the following are true:

1. The release source is frozen, committed, rebased onto current upstream,
   pushed, and exactly represented by deploy/testnet/release.lock.yml.
2. The launch-critical producer gate passes before any live campaign write,
   and the complete release gate passes against the same immutable
   multi-repository checkout before final acceptance. The complete gate may run
   concurrently with live acceptance after the producer gate; neither result
   can be borrowed from an older source revision. Together they cover ordinary,
   race, PostgreSQL/Redis, Foundry, Slither, generated-artifact,
   infrastructure, and patch-hygiene checks.
3. Two independent read-only plan builds against the authoritative attempt-4
   state produce byte-identical plans, the same plan hash, and the same bounded
   cumulative spend.
4. The exact approved plan is resumed/launched. The real server/connect,
   miner, and validator modules run; no substitute CLI-only implementation is
   accepted.
5. release-1.0 records five consecutive complete 300-block settlement epochs
   plus terminal finalization. production-soak then applies the
   future-effective production policy and records three consecutive complete
   360-block epochs plus its 180-block settlement window. Only the release
   native handoff can extend beyond its acceptance terminal under the pinned
   profile; production's native deadline is earlier than its terminal. The
   complete scheduler-controlled interval is 3,004--4,380 blocks
   (10:00:48--14:36:00 at 12 seconds per block), and no tail block counts as an
   accepted epoch.
6. All 61 mandatory adversarial vectors are sampled through seven attributed
   actors running concurrently with the happy path. The attempt
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
- No campaign is currently running. A pre-freeze host audit found the inactive
  user-systemd unit still carried a historical `Restart=on-failure` rendering
  even though current source already emits the required stop boundary. The
  installed inactive unit was reconciled to `Restart=no`, `KillMode=mixed` and
  `TimeoutStopSec=60`; it is static, has no install symlink or reverse
  dependency, and user lingering is disabled. The simulator therefore cannot
  survive a host reboot without an explicit resume. The four simulator-owned
  PostgreSQL/Redis containers remain intentionally reusable with `restart=no`;
  final doctor must authenticate their locked specifications rather than
  deleting or trusting them implicitly.
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

The first-draft table is retained below as historical provenance. The current
pre-integration snapshot, fetched and verified clean/equal to upstream on
2026-09-04 UTC, is:

| Repository | Branch | Current pre-integration revision | State |
|---|---|---|---|
| sn | main | f5f1e46890240c4ca2fd44ebcc973faa897e2428 | clean/equal; final candidate pending |
| server | main | 2b09692ac256fbc380a46bc7f957fdf8c510add6 | clean/equal |
| operator-proxy | main | e1a76e03a60e6f81c49376556aacb3f0f9289d8a | clean/equal |
| connect | main | 86715ee66950c2386b0aa5ce45459fb6911c3582 | clean/equal; generated blocker/CFAA policy refresh reviewed |
| sdk | main | 2fca65408c9f8b52f5bd20d2c957e67b828d746c | clean/equal |
| glog | master | 2bdcce5f8be023947f26a247eb5665c56b69b2e3 | clean/equal |
| goidenticons | main | 325750b38314313dc5f44c880ab6f12f6c1ecb3c | clean/equal |
| proxy | main | 1c72dfd66f8c7fbe72120b6657c8a445f7f25499 | clean/equal |
| userwireguard | master | 85fb1ca4086fa5dbfcda526bec7a17a894e691b9 | clean/equal |
| vault | main | 992f69a4ba744dabf43c71401a3f355f05f46428 | clean/equal |
| xops | main | 0a215e96348027c40fad3569fa7c422cdc4d57aa | clean/equal |
| config | main | 205eae72d13527e71bb895923a1782fffe0d9ed2 | clean/equal |

FREEZE-UPDATE the SN row to the final pushed commit and reverify all twelve
rows immediately before each exact gate. The candidate at
`temp/sn-test-runtime-fix` is deliberately separate from these release roots
until it is committed, reviewed, and integrated.

Historical first-draft snapshot:

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

Source freeze is not complete. The reviewed implementation candidate
`3f3118979c1b9bf51f7e4168553ddf54057955bf` was cherry-picked onto canonical
`main` as implementation checkpoint
`8c9a0716d16432f568550443bdc9c5bb7cb7ee27`; documentation reconciliation,
push, release-lock refresh and both frozen gates remain pending. The expensive
TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper regression now
passes and provides a narrow walk through the complete 1,000-miner semantic
fixture. The following real defects have already been corrected in the working
tree:

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

The final evidence and release-boundary audit also corrected five blocking
integration defects before any new testnet write:

- capture manifests exclude all post-capture semantic outputs, while an
  owner-signed supplement binds, replicates, and rereads every derived byte;
- exact public evidence reads bind deployment, netuid, kind, run, hash,
  canonical storage key, a one-object response, and closed pagination; new
  deployment history uses `_deployment`, while legacy `deployment` history is
  readable only by exact hash;
- payout history traverses canonical 256-object pages under a 4,096-object
  global cap and rejects short continuation pages, cursor drift, duplicates,
  cross-scope keys, and malformed hashes before object fetch;
- `evm_evidence_deadline = max(acceptance_terminal_block,
  native_application_deadline_block)` is inclusive. Native application and EVM
  evidence retain independent bounds; release can have a native handoff tail,
  while pinned production has no post-terminal tail;
- the stopped-topology migration verifier no longer carries a two-proxy count
  from before the public EVM egress process existed. It derives the exact
  33-child ID/role/identity inventory through the real server and client process
  builders, rejects missing and same-count substituted processes, and proves
  that the 32-process rolling-fault lane is precisely that inventory minus the
  deliberately non-faulted public egress quota owner.

The generated public manifest now advertises a runnable exact-run `analyze`
command and bounded query-complete history locators. Current manifests require
one coherent strict locator generation across both operators. The exact signed
pre-fix v1 locator/command encoding remains accepted only as immutable legacy
metadata, with same-plan byte retention; mixed or tampered legacy/current forms
fail. A release-lock-driven plan revision must publish the strict current
format before final peer-review evidence is cited.

The former `public native reward UID 1000` mismatch was a stale fixture view and
is fixed; the full mocked semantic supplement passed in 226.948 seconds. Rerun
it from the frozen checkout before launch:

    go test ./sim-testnet \
      -run '^TestFinalSemanticSupplementPublishesResumesAndRejectsLooseTamper$' \
      -count=1 -timeout=30m

Then run all final semantic tests ordinary and race before widening to the
producer and aggregate gates. The implementation and focused tests now cover
validator-view divergence, head transitions, dual-origin exact-run analysis,
atomic MinIO create-or-verify behavior, and direct server evidence
publish/get/history. Their final frozen producer/aggregate results and live
dual-origin replay remain required; focused prequalification is not a substitute
for either gate.

The complete server model package historically reached a 30-minute command
deadline amid full-suite database churn. On current server `2b09692a`, every
release-selected PostgreSQL/Redis controller, model, and taskworker test passed
normally and under the race detector, and the complete proxy package passed in
529.073 seconds; both services remained healthy. This is strong
prequalification but does not claim a new complete unselected `server/model`
package run. The exact frozen producer and aggregate selections must still pass,
and any repeat timeout is a root-cause item rather than permission to extend a
deadline silently.

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

### 4.2 Create the immutable release candidate

The exact gates refuse a dirty or unpushed release root. After focused
correctness is green:

1. Review and commit every intended implementation, regression, documentation,
   ABI, contract, config, and gate change. Exclude run state, logs, caches,
   build products, and secrets.
2. Fetch and pull/rebase each changed repository, resolve semantically, rerun
   conflict-adjacent tests, and push.
3. Verify all twelve release roots are clean and `HEAD == @{upstream}`.
4. From SN, run `go run ./sim-testnet release-lock --apply`. Review every
   changed runtime, source, artifact, interface, infrastructure, and config
   digest; never bless an unexplained mismatch.
5. Commit and push the release lock plus the reconciled handoff documents.
6. Fetch again and run `scripts/check-release-source-freeze.sh
   /home/by/urnetwork`. Record its exact twelve-repository output.

Any later source edit creates a new candidate: regenerate the lock, commit,
push, and restart both gates. A passing test from the dirty staging worktree is
prequalification only.

### 4.3 Run the launch-critical producer gate

Run from /home/by/urnetwork/sn:

    PATH=/home/by/.foundry/bin:$PATH \
      ./scripts/test-release-1.0-producer-gate.sh

This gate must cover the complete compiled simulator/validator graph, signed
attempt transitions, terminal cuts, atomic settlement, lossless capture,
process-log fencing, direct publication, operator proof APIs, isolated
PostgreSQL/Redis evidence paths, deployable contract behavior, generated
contract/ABI freshness, and the final source/release-lock fence. It is the
bounded launch-critical fence; no live campaign write precedes it.

### 4.4 Run the complete aggregate gate

With local PostgreSQL/Redis healthy:

    PATH=/home/by/.foundry/bin:$PATH \
      RUN_SERVER_DB_TESTS=1 \
      ./scripts/test-release-1.0-local.sh

Required results:

- all SN Go packages pass;
- full sim-testnet race suite passes within its checked-in 90-minute deadline;
- all four deployable Solidity roots have zero Slither high/medium findings;
- forge fmt/build/test pass, with all 156 or more current tests passing;
- generated contract payload, storage layout, ABI, and Go bindings are fresh;
- operator pure and DB-backed suites pass;
- complete server proxy package passes under its explicit 20-minute bound;
- affected Connect/SDK normal and race suites pass;
- xops Subtensor infrastructure regressions pass;
- every release repository passes staged and unstaged diff checks;
- the final release-lock checkout test passes after every other long gate.

The complete gate is mandatory exactly once against this locked source for the
final acceptance record. Under the user-approved accelerated schedule, start it
as soon as the producer gate passes; it may run concurrently with the live
acceptance windows because the checkout is immutable and the campaign uses
separate managed runtime state. Do not run its PostgreSQL/Redis section
concurrently with another suite against the same test databases. Any aggregate
failure invalidates the live candidate, stops new mutations, and invokes the
failure protocol even if chain progress has already begun.

Do not call testnet validated until both gate scripts pass from the same
unchanged, pushed filesystem tree. If upstream moves after either gate, the
checkout is no longer frozen: rebase, rerun affected tests and both final source
fences, regenerate the lock if necessary, and issue a new plan.

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
453 spec/transaction/state versions plus exact Wasm and metadata hashes at one
finalized checkpoint, chain/genesis identity, activation flags,
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
- all 33 expected managed child processes start in one current supervisor
  generation;
- both operator APIs and direct Connect ingress are reachable;
- all 1,000 miner identities are live through their 20 production swarms, and
  both validators produce fresh proofs for every operator/epoch domain;
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

Expected irreducible live duration from the release scheduler observation after
preparation:

- release fixed geometry: 5 x 300 + 150 = 1,650 blocks;
- production fixed geometry: 3 x 360 + 180 = 1,260 blocks;
- combined fixed accepted-epoch geometry: 2,910 blocks, about 9 hours 42 minutes
  at 12 seconds per block; it excludes boundary alignment before each
  acceptance start;
- release alignment plus its authenticated CRv4 handoff yields 1,743--2,760
  scheduler-controlled blocks; production alignment plus its fixed terminal
  yields 1,261--1,620 blocks;
- the exact combined scheduler-controlled range is therefore 3,004--4,380
  blocks, or 10:00:48--14:36:00 at 12 seconds per block; its phase-neutral
  planning midpoint is 3,692 blocks, or 12:18:24;
- semantic analysis is asynchronous after capture closure and should overlap
  the next live phase.

The release phase must not be marked complete merely because five settlement
epochs elapsed. Its first three causal native milestones and terminal binding
must also satisfy the runtime-453 schedule. Production must obtain the later
terminal-active native decision by its fixed acceptance terminal. The inclusive
EVM evidence deadline is the later of the acceptance terminal and native
application deadline; the two domains are checked independently. Under the
pinned production profile the native deadline is no later than start+820 while
the terminal is start+1,260, so production has no post-acceptance temporal tail.

These are exact chain-clock bounds after preparation. RPC/finality work,
transaction inclusion, capture publication, and semantic replay are not covered
by a campaign-wide wall-clock SLA. Monitor their explicit state transitions and
watchdogs; never reinterpret the 14:36:00 scheduler maximum as permission to
ignore a stalled process.

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
| Chain/deployment identity | chain ID 945, genesis/runtime 453 identity, netuid 521, deployment block, proxy/implementation/vault/reserve/probe addresses, runtime code hashes, ERC-1967 implementation slot, policy version/hash/effective block |
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
| Adversarial resilience | all 61 mandatory vectors through seven attributed concurrent actors, seed/matrix hash, samples, latency/error metrics, applied/restored faults, no cross-operator or cross-validator contamination, clean attempt ledger |
| Process/runtime resilience | exact binaries/config manifest, current supervisor generation, expected process inventory, zero unexplained restarts/panics/errors, bounded intentional fault recoveries, stopped-on-reboot policy |
| Public artifact history | both operator archive roots, MinIO locators, byte sizes/content hashes, replica readback, secret-scan result, owner completion/supplement signatures, capture/input/evidence manifests |
| Three clean production epochs | three consecutive complete production UR epochs under 360/60/180/6 policy with zero failures, plus the preceding five clean accelerated epochs; clearly separate the bounded release lifecycle tail from accepted epochs |
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
         --run-id 'SIGNED_CAMPAIGN_RUN_ID' \
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

FINAL.md and every clean-checkout command must cite the latest authorized
strict-format deployment-manifest revision. Revision-zero manifests with the
exact pre-fix locator/command encoding are immutable archive/lineage evidence,
not the final discovery pointer; they remain analyzable only when the reviewer
supplies the exact signed campaign run ID explicitly.

## 12. Freeze and execution record

FREEZE-UPDATE this table as work completes.

Prequalification recorded 2026-09-03/04 UTC is diagnostic evidence, not final
freeze approval:

- canonical SN `f5f1e46` passed the four-root Solidity static gate with zero
  high/medium findings, Forge format/build and 156/0/0 tests, generated-contract
  and ABI freshness, plus validator ordinary/race/vet (33.821s/282.409s/green);
- the dirty SN candidate compiled all packages in 8.37 seconds and passed full
  vet in 4.90 seconds. Its evidence-integrity selector passed 33/33 normally in
  380.477 seconds. The same exact 33-test union passed under the race detector
  in three non-overlapping shards: 2 tests in 403.618 seconds, 10 tests in
  1,592.441 seconds, and 21 tests in 1,740.076 seconds, with zero skips and no
  race report. Their 3,736.135-second sequential lower bound is why the complete
  package has a 90-minute harness deadline; the final frozen aggregate remains
  pending. After cherry-pick, canonical SN against Connect `86715ee` passed the
  seven exact topology/gate regressions normally and under race in 1.312 and
  11.199 seconds, compiled the simulator graph, and passed simulator vet;
- server `2b09692a` passed focused artifact/history suites, full compile/vet,
  release-selected controller/model/taskworker ordinary and race suites, and
  the complete proxy package in 529.073 seconds. PostgreSQL and Redis were
  healthy before and after;
- operator-proxy `e1a76e03`, SDK `2fca654`, and its mobile export policy passed
  their checked-in ordinary/race gate selections; Connect `86715ee` passed its
  generated blocker/CFAA table, lookup, policy-hash, consumer, race, compile and
  vet checks; xops `0a215e9` passed all 38 selected infrastructure regressions
  in 9.393 seconds.
- canonical SN `7d634c4` replaces the serial final-evidence hot path with
  bounded `GOMAXPROCS` workers for independent lifecycle decisions, accepted
  and dishonest-deposit measurements, and lineage edges while retaining
  canonical first-error order. The release-scale build/render/artifact test
  fell from 84.950 to 33.766 seconds and reached 464-664% observed process CPU
  on this 24-CPU host. The widened ordinary semantic selection passed 204
  unique test/subtest names in 416.901 seconds (447.81 seconds wall), the
  affected heavy race selection passed in 524.891 seconds, and the final
  worker/cache/stdio race regression passed in 222.138 seconds. Simulator vet
  remained green. The prior 942.675-second full ordinary run had only two
  failures: the deliberately stale release lock and a test which mistook Go's
  `-json` stdout/stderr multiplexing for an imported-module alias. The latter
  now uses a child process with independently wired descriptors and passes both
  ordinary and `-json` invocation; a frozen full rerun remains mandatory.

Do not infer an unselected full Server model/repository pass, source freeze,
producer pass, aggregate pass, or live campaign result from these focused
records.

| Item | Result | UTC / immutable reference |
|---|---|---|
| Narrow 1,000-miner semantic supplement test | pass before freeze; frozen rerun pending | 226.948s mocked semantic replay; section 3 |
| All final semantic ordinary tests | prequalified; frozen rerun pending | `7d634c4`; 204 selected test/subtest names, 416.901s package / 447.81s wall |
| All final semantic race tests | prequalified in shards; frozen aggregate pending | `7d634c4`; affected heavy selection 524.891s; latest worker/cache/stdio selection 222.138s |
| Full sim-testnet ordinary | pending | |
| Full sim-testnet race | pending | |
| Producer gate | pending | |
| Aggregate gate with DB tests | pending | |
| Foundry | prior 156/0/0; final pending | |
| Slither | prior 0 high/medium; final pending | |
| Server release-selected DB/proxy qualification | prequalified; frozen gate pending | `2b09692a`; controller 187.290/209.749s, model 204.658/225.727s, taskworker 42.164/49.247s, proxy 529.073s |
| Server unselected full model/repository suites | pending if required by final gate/diagnosis | no broad pass inferred from focused selection |
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
