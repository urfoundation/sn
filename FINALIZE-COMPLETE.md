# Release 1.0 testnet completion handoff

Status: live working document, first written 2026-09-03 UTC and last reconciled
2026-09-05 06:20 UTC before the final source freeze. Refresh every item marked
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
`gpt-6-astra` at reasoning effort `max` to diagnose the root cause, audit
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
   state produce the same approval projection, plan hash, actions, and bounded
   cumulative spend. Their raw diagnostic JSON is expected to differ only in
   generation time and finalized observation checkpoints as the live chain
   advances; any other difference is release-fatal.
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
  - deployment-manifest base coordinator implementation (not the current
    ERC-1967 slot after upgrade history):
    0x8c033865c1cd387a0fa5a8f3369908a20c5a54e4
  - live ERC-1967 implementation slot observed through the public testnet RPC
    at 2026-09-05 01:00 UTC:
    0xe732c2e6dbced5dcc44d1a5524a8af1343c1e2ef
  - the current public manifest's generic `deploy_block` marker is 7,900,646;
    this is the later fleet-batcher deployment and must not be used as the
    coordinator proxy's creation boundary
  - the current coordinator proxy's exact deployment/initial `Upgraded` block
    is 7,895,374; the carried predecessor proxy's is 7,889,308
- The current source must create a plan revision which preserves authenticated
  carried history and upgrades only what the new release lock requires. Never
  redeploy immutable custody simply because the implementation changed.

FREEZE-UPDATE repository revisions:

The first-draft table is retained below as historical provenance. The current
source candidate and dependency snapshot was fetched again on 2026-09-05
06:22 UTC, with no additional upstream changes since 05:52. Clean/equal status
applies only to the rows explicitly marked so:

| Repository | Branch | Current checkout revision | State |
|---|---|---|---|
| sn | main | 20431dd39f5a81b7ff85c18e11042d5e99e1f9d5 | dirty source candidate; upstream documentation commit `46eae44` and release-lock/handoff commits pending |
| server | main | df90d425b9b5aa589db2926d8f32006b2aaa9591 | core allocation repair and initial strict-test checkpoint pushed; ordinary/race pass; adjacent provider-reporting and remaining strict-test fixes dirty, qualification pending |
| operator-proxy | main | 0285a79d87b996bce50f2d18a824c750ad76233f | clean/equal; ordinary/race/vet qualification pass |
| connect | main | 1b81da6668e6a3ec9536ac61a07b27a619738cc7 | clean/equal; incoming auth/TCP and generated-policy focused ordinary/race pass; frozen unsharded certificates pending |
| sdk | main | e1d8dc8d9682daefd86878fea911b7b643634406 | clean/equal; incoming JWT transport and points-display focused ordinary/race pass |
| glog | master | 2bdcce5f8be023947f26a247eb5665c56b69b2e3 | clean/equal |
| goidenticons | main | 325750b38314313dc5f44c880ab6f12f6c1ecb3c | clean/equal |
| proxy | main | 3c2b79c56024268efb45133dbcfcb961e865892d | clean/equal; incoming change only affects filtered test output |
| userwireguard | master | 85fb1ca4086fa5dbfcda526bec7a17a894e691b9 | clean/equal |
| vault | main | f2ac60e3764e845b973ebf7c87a37ab6eb038dff | clean/equal; unrelated encrypted competition credentials integrated |
| xops | main | 252301938040b39e195e69200b66ee650c5dbf44 | clean/equal; documented Ansible suite passes 88 tests |
| config | main | c2837951daa557f4d9ae8fb2d482db0b84fe4add | clean/equal; unrelated competition profile integrated |

Reverify all twelve rows immediately before each exact gate. Any later
documentation-only handoff commit must be recorded separately from the frozen
runtime candidate; any runtime-source edit creates a new candidate and requires
the full lock/freeze/gate cycle.

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

The release lock was refreshed from clean pushed source at 2026-09-04 18:14
UTC as
`sha256:998a86a4c3806e63f7c1c056401b0cb3cefb7601d6579b96f6bfddcbf2135cb5`.
Relative to the prior lock it updates only `connect_go_source_hash` and
`protocol_source_hash`, binding the generated-policy implementation/data and
the widened release selectors. The later full-race failures supersede this
pre-fix render; refresh it after both repairs rather than committing or using
it for launch.

## 3. Current implementation/gate continuation point

The 2026-09-04 16:21 UTC producer attempt passed source/runtime attestation,
the then-exact v451/v452/v453 metadata gate, graph compilation, validator
ordinary/race, lossless-capture ordinary/race, and all 156 Foundry tests before
failing generated-binding freshness with `abigen: command not found`. Its log
is `/home/by/urnetwork/temp/producer-gate-20260904T1621Z.log`, SHA-256
`f4fe1b9a440d6cf257bff1626e85e51b4fcfb6a59fbaabc2e6a7e02fa95591ae`.
The executable existed in GOPATH; the launch PATH omitted GOPATH/bin, the
generator documentation still named go-ethereum 1.16.7 while `go.mod` requires
1.17.0, and neither release gate failed early or release-locked the generator.

Source commit `2519581` fixes the complete failure class. The shared generator
resolves explicit `ABIGEN`, PATH, GOBIN, then the first GOPATH/bin; requires
exact `abigen version 1.17.0-stable`; offers a no-artifact `--preflight`; and is
called by both gates immediately after source-freeze. Hermetic tests cover every
resolution path, precedence, wrong/missing tools, and no-JQ preflight behavior
normally and under the race detector. The release lock now observes the same
shared preflight and binds the generator script into `protocol_source_hash`.
The canonical v1.17.0 generator produces byte-identical checked-in bindings.

The corrected 17:00 UTC producer run then passed every substantive phase:
runtime/metadata attestation, compiled graph, validator and lossless-capture
normal/race suites, 156/0/0 Foundry tests with 4,608 invariant calls, generated
contracts and bindings, operator APIs, operator-proxy ordinary/race suites,
isolated PostgreSQL/Redis paths, and the release-lock test. The final fetched
source fence correctly stopped because Connect advanced during the run from
`fb888dc` to `0dd6ee2`. No testnet transaction was sent. The complete log is
`/home/by/urnetwork/temp/producer-gate-20260904T1700Z.log`, SHA-256
`efcd4fdeeacbb7dbbd6dff3b6d530a26d950ff0877dd09ed663c9ed6e8901db5`.

Reviewing that drift exposed a release-input provenance class, not a reason to
waive the fence: generated blocker/CFAA tables lacked per-feed content hashes
and deterministic dispositions; checked-in tests tolerated empty placeholder
data; blocker documentation claimed the wrong packed record width; diagnostic
stale generation could retain below-floor data; deprecated fetch failure was
tolerated; and IPv6/global/feed-family floors were incomplete. Connect
`d65a05e036ba575bc6055fc0343fc91baed60a8a` fixes the class with ordered
decoded-body SHA-256/byte/count provenance, fail-closed fetch and floor rules,
a dedicated Spamhaus IPv6 floor, explicit deprecated-empty semantics, runtime
minimums, opaque generated payloads, and deterministic adjacent regressions.
SN `20431dd` makes both producer and aggregate gates run one exact 24-test
generated-policy consumer selector plus both generator packages normally and
under race; the selector includes Telegram reflector/fallback collisions and
its static parser rejects duplicate or missing assignments.

Two independent audits matched the earlier Connect tree
`a942fe337a387e15900b2e2155dd662acc2754b2`; its exact generated-policy suites,
vet, format, decoders and full 523.615-second ordinary run passed. The isolated
20-minute full race then failed
`TestP2pTransportAutoSelectsFastPathForCapablePeer` after its 10-second connect
deadline and eventually timed out with `TestWeightedShuffle` active after
roughly 2,000 prior tests. The latter passes alone in 13.381 test seconds, and
the full run contained no race marker. Pre-fix logs are
`/home/by/urnetwork/temp/connect-fast-path-prefixed-main-race-count20-20260904.log`
(SHA-256 `c5fa04780a2f82e86c962e87fbd70b1b927d3dcdca075c5e09c1fccda434e5a2`),
`/home/by/urnetwork/temp/connect-signal-prereg-focused-normal-20260904.log`
(SHA-256 `bfe6feb8359d0f728f5022a33d8650bf9ace96eeb0b271a0bd5a20cd750a24b3`)
and `/home/by/urnetwork/temp/connect-root-full-race-isolated-20260904.log`
(SHA-256 `f220729ac7e1a256f3917701d05349cec4a253b81aee0986fb292e5f795805f8`).

Connect `b22ab0704f6dc3ecf80e91b31b5c7fafca097223` (tree
`000160e9679bb1636621d3b6d990f920866ca582`) fixes the complete observed class.
The direct test carrier now drops an arrival before registration like the
production receiver; the delayed carrier resolves at dispatch, enforces one
257-frame combined ownership bound, prevents Add-after-Wait, joins all admitted
senders and returns all pooled copies before completion. The raw byte-stream
smoke opts into ordered SCTP while the default reliable-unordered message test
accepts only an exact, duplicate-free message multiset. Barrier regressions
cover direct and delayed drops, registration during delay, capacity progress,
send/cancel races and pool witnesses. Helper tests passed 20 times normally
(0.073s) and under race (2.289s); actual fast-path/WebRTC tests passed 20 times
normally (24.041s) and under race (32.606s). Canonical 10-run replays, vet,
format and patch checks pass, and an independent lock/ownership audit found no
blocker. Both release gates pin this affected selector. The aggregate gate also
runs one unsharded default ordinary certificate, one unsharded default race
certificate and fixed alternate race order `4535211000`, each with a 30-minute
package deadline and race runs capped at four execution cores. Those exact
frozen aggregate certificates remain pending.

The two approval-identical plans built immediately before this source change
had 2,298 actions, plan hash
`0x39e2c74bfd93cf8a42f5f3172f3683f85b4a1e45d759096cbcafa4539352fc48`,
and approval-projection SHA-256
`c2611372cb02fb40bf6f7468ce09b6296a4eaefeedcf6f9575bbfa9291fb79ff`.
Their maximum spend was 165,748,236,000 TAO rao, 24,748,499,999,949 alpha
rao, 147,549,500,000,000,000,000 EVM gas wei, and 259 registrations. They are
now superseded and must never be applied. Build two new plans after the
corrected frozen producer gate.

The final frozen gates remain pending. Runtime-evidence candidate
`5f882b20790aba1333664323f3bf748933f62273` and the release-gate selector
follow-up `eb9f0a41eb8c3514cc4d356f539fc7627af3ce66` are committed, pushed, and
passed their focused normal/race/vet qualification. A freeze fence correctly
found new Server, operator-proxy, Connect, SDK, Vault and xops upstream commits;
those clean checkouts were fast-forwarded, affected qualification passed, and
the exact twelve-root fence passed before the final lock refresh. The SDK also
contained five pre-existing canonical-format defects; formatting-only commit
`2f3e7058873498099a88aee3e158caa11aefbda1` fixes them and is pushed after full
root normal, affected normal/race, nested build/cgo normal/race, vet, and
cross-repository simulator compile checks passed. The expensive
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
4. Require the full sim-testnet package ordinary and race on the frozen
   candidate in the aggregate gate (section 4.4). Repaired failures first need
   focused ordinary/race confirmation; do not add a duplicate complete run
   before freezing solely to repeat the same certificate. The aggregate gate
   remains mandatory and may overlap live acceptance only after the producer
   gate passes, as already authorized below.
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
4. From SN, build a fresh `-trimpath -buildvcs=true` executable into a unique
   sibling `temp/` directory, record `sha256sum` and `go version -m`, and invoke
   that exact absolute nonsymlink path for `release-lock --apply`. The driver
   independently requires its build revision, clean checkout, fetched
   `origin/main`, and a bounded live GitHub `main` observation to agree before
   it can replace the lock. Review every changed runtime, source, artifact,
   interface, infrastructure, and config digest; never bless an unexplained
   mismatch.
5. Commit and push the release lock plus the reconciled handoff documents.
6. Fetch again and run `scripts/check-release-source-freeze.sh "$WORKSPACE"`.
   Record its exact twelve-repository output.

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
    SIM_TESTNET_SOURCE_REV="$(git rev-parse --short=12 HEAD)"
    SIM_TESTNET_BUILD_UTC="$(date -u +%Y%m%dT%H%M%SZ)"
    SIM_TESTNET_CANDIDATE_DIR="/home/by/urnetwork/temp/postgate-${SIM_TESTNET_SOURCE_REV}-${SIM_TESTNET_BUILD_UTC}"
    mkdir -p "$SIM_TESTNET_CANDIDATE_DIR"
    go build -trimpath -buildvcs=true \
      -o "$SIM_TESTNET_CANDIDATE_DIR/sim-testnet" ./sim-testnet
    SIM_TESTNET_BINARY="$SIM_TESTNET_CANDIDATE_DIR/sim-testnet"
    sha256sum "$SIM_TESTNET_BINARY" > "$SIM_TESTNET_CANDIDATE_DIR/sim-testnet.sha256"
    go version -m "$SIM_TESTNET_BINARY" > "$SIM_TESTNET_CANDIDATE_DIR/sim-testnet.buildinfo"

    "$SIM_TESTNET_BINARY" doctor \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json \
      > /tmp/ur-subnet-testnet-doctor.json

Keep `SIM_TESTNET_BINARY` set to this exact hashed post-gate binary for every
remaining command in this document. Never resume with the obsolete binary under
the attempt-4 state directory or an untracked repository-root build product.

Doctor must be ready. The public operational/comparison backend distinction may
be reported as the documented non-hard public-testnet limitation; every other
check must pass. Specifically verify wallet identity, netuid ownership, runtime
454 spec/transaction/state versions plus exact Wasm and metadata hashes at one
finalized checkpoint, chain/genesis identity, activation flags,
hyperparameters, budget and balance, MinIO, local Docker, systemd, port
availability, source lock, and authoritative carried history.

The current server candidate adds migration 631 for exact per-client provider
allocations on each network payment sweep. Launch already runs the real
server `db migrate` command for both isolated operator databases before starting
their APIs/workers. Verify both `operator-*-db-migrate.log` files show successful
completion at this candidate's migration head; the migration monitor must also
find the nullable JSONB column and shape constraint. Do not start the operator
against schema 630 or reinterpret ambiguous legacy multi-provider rows as one
provider's usage. Preserve historical rows; this migration does not backfill or
rewrite them.

Build the plan twice without any intervening write:

    "$SIM_TESTNET_BINARY" plan \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json \
      > /tmp/ur-subnet-testnet-plan-a.json

    "$SIM_TESTNET_BINARY" plan \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json \
      > /tmp/ur-subnet-testnet-plan-b.json

    jq -S 'del(
      .generated_at,
      .live_facts.finalized_block,
      .live_facts.finalized_block_hash,
      .live_facts.evm_finalized_block,
      .live_facts.evm_finalized_block_hash,
      .coordinator_upgrade_baseline.finalized_block,
      .coordinator_upgrade_baseline.finalized_block_hash
    )' /tmp/ur-subnet-testnet-plan-a.json \
      > /tmp/ur-subnet-testnet-plan-a.approval.json

    jq -S 'del(
      .generated_at,
      .live_facts.finalized_block,
      .live_facts.finalized_block_hash,
      .live_facts.evm_finalized_block,
      .live_facts.evm_finalized_block_hash,
      .coordinator_upgrade_baseline.finalized_block,
      .coordinator_upgrade_baseline.finalized_block_hash
    )' /tmp/ur-subnet-testnet-plan-b.json \
      > /tmp/ur-subnet-testnet-plan-b.approval.json

    cmp /tmp/ur-subnet-testnet-plan-a.approval.json \
        /tmp/ur-subnet-testnet-plan-b.approval.json

This projection exactly mirrors the observation-only fields normalized by
`SetupPlan.hash`. Keep both raw files as evidence: their moving checkpoints
prove independent live reads. `TestPlanHashExcludesGenerationTimeButIncludesLiveFacts`
deterministically proves that observation-only drift preserves approval while
an approval-bearing economic or topology change does not. Never replace this
allowlist with a broad or recursive field deletion.

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

Most recent verified but now superseded plan candidate, independently rebuilt
twice on 2026-09-04 UTC:

- plan hash: `0x39e2c74bfd93cf8a42f5f3172f3683f85b4a1e45d759096cbcafa4539352fc48`
- canonical approval-projection SHA-256:
  `c2611372cb02fb40bf6f7468ce09b6296a4eaefeedcf6f9575bbfa9291fb79ff`
- raw plan A SHA-256:
  `0b18055e947f23c0adda65cf9a54efb4e64f774336a3a15b5fd9714289d4af5b`
- raw plan B SHA-256:
  `02f26eb9aadd4e0a1b6786555d521525f3bf3494bbc0a63c7c98061cc87e7ef4`
- plan A/B log SHA-256:
  `f8b3cbeeb45b7fb165274574419ff7db3dd3f100fb190de8d4fbb98572e39f47` /
  `4cdf0eb7b32d9a86492d8adb14a2548d655524edb718f1acc69bbc54f061bf93`
- action count: 2,298
- maximum cumulative TAO rao: 165,748,236,000
- maximum cumulative alpha rao: 24,748,499,999,949
- maximum EVM gas wei: 147,549,500,000,000,000,000
- maximum registrations: 259
- subnet creations: 0
- plan A was generated at 17:06:07 UTC with native/EVM checkpoints
  7,933,271 / 7,933,272 and coordinator baseline 7,931,698
- plan B was generated at 17:09:41 UTC with native/EVM checkpoints
  7,933,289 / 7,933,289 and coordinator baseline 7,931,698
- the raw diff contains only `generated_at` and the four finalized native/EVM
  checkpoint fields; plan hash, approval projection, all 2,298 actions,
  cumulative spend, limits, roles, code identities, and lineage are identical
- the retained projections are
  `/home/by/urnetwork/temp/plan-a-7a4d97e.approval.json` and
  `/home/by/urnetwork/temp/plan-b-7a4d97e.approval.json`

This candidate predates the generated-policy fixes and refreshed release lock.
It must never be applied. Build, compare, review, and record two new plans only
after the frozen producer gate passes.

## 6. Exact launch/resume

Set the reviewed hash only after section 5 succeeds:

    SIM_TESTNET_APPROVED_PLAN_HASH=0xREPLACE_WITH_REVIEWED_HASH

Resume the existing attempt rather than creating a new state root:

    "$SIM_TESTNET_BINARY" resume \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --apply --plan-hash $SIM_TESTNET_APPROVED_PLAN_HASH \
      --detach

If the revision requires setup before topology readiness, resume is still the
preferred journal-aware entry point. Use launch with the same arguments only if
the current CLI explicitly reports that launch is the required operation:

    "$SIM_TESTNET_BINARY" launch \
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

    "$SIM_TESTNET_BINARY" status \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json

    "$SIM_TESTNET_BINARY" inspect \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json

## 7. Live campaign

Run the composite orchestration so it adopts only authenticated clean phase
markers and resumes the first incomplete phase:

    "$SIM_TESTNET_BINARY" scenario \
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
must also satisfy the runtime-454 schedule. Production must obtain the later
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

    "$SIM_TESTNET_BINARY" status \
      --config sim-testnet/testnet.yml \
      --state-dir sim-testnet/runs/ur-subnet-testnet-v1-attempt-4 \
      --format json

    "$SIM_TESTNET_BINARY" tail \
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
| Chain/deployment identity | chain ID 945, genesis/runtime 454 identity, netuid 521, deployment block, proxy/implementation/vault/reserve/probe addresses, runtime code hashes, ERC-1967 implementation slot, policy version/hash/effective block |
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

       "$SIM_TESTNET_BINARY" inspect \
         --config sim-testnet/testnet.yml \
         --manifest 'https://OPERATOR/sn/evidence?hash=sha256:...' \
         --format json

       "$SIM_TESTNET_BINARY" analyze \
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
  pending. Against Connect
  `fb888dc8883efb12dd570e5514e866dba14d987e`, canonical SN compiled the
  simulator graph and focused sender-role plus generated blocker/CFAA
  invariants passed normally and under race; the frozen aggregate remains
  pending;
- server `2b09692a` passed focused artifact/history suites, full compile/vet,
  release-selected controller/model/taskworker ordinary and race suites, and
  the complete proxy package in 529.073 seconds. PostgreSQL and Redis were
  healthy before and after;
- operator-proxy `e1a76e03`, SDK `2fca654`, and its mobile export policy passed
  their checked-in ordinary/race gate selections; Connect
  `fb888dc8883efb12dd570e5514e866dba14d987e` passed focused sender-role and
  generated blocker/CFAA ordinary/race qualification plus SN, server, and
  operator-proxy consumer compilation; its complete compile/vet and aggregate
  gate remain pending; xops `0a215e9` passed all 38 selected infrastructure
  regressions in 9.393 seconds.
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
- frozen SN runtime candidate `5d779cd` passed the launch-critical producer
  gate on 2026-09-04 UTC. Signed validator ordinary/race suites passed in
  32.903/277.286 seconds; lossless capture ordinary/race passed in
  63.745/356.492 seconds; all 156 Foundry tests and 4,608 invariant calls
  passed; operator proof/artifact APIs, operator-proxy ordinary/race suites,
  isolated PostgreSQL/Redis controller/model/taskworker paths, the release-lock
  test, and both source-freeze fences passed. The authoritative doctor passed
  all 64 checks; only the two declared non-hard same-public-provider
  independence caveats remain.
- the first aggregate run from docs head `5257b2f` reached the ordinary
  simulator package with no assertion failure, then Go terminated the package
  at its implicit 10-minute total deadline while three independent semantic
  supplement tests were concurrently inside durable `fsync`. A scheduler and
  CPU profile proved the process had 30 threads, CPUs 0-23 available, no cgroup
  quota, and only transient serial plan work; the isolated profiled revision
  regression completed in 1.580 seconds. Runtime source `8c29d56` gives the
  ordinary aggregate pass the same explicit 90-minute ceiling as the complete
  race pass. It also separates PostgreSQL's persistent-data identity from
  reviewed comments, host paths, port bindings, tuning, and restart policy.
  Both retained operator volumes passed the live Docker probe against their
  exact cryptographic August predecessor identities; no durable data was
  removed or copied. The deterministic ordinary/race compatibility selections
  and simulator vet pass. This source change supersedes the earlier freeze,
  producer result, release lock, doctor, and plan; all must be regenerated.
- the next attempt-4 resume used SN `6a6c74f` and read-only plan
  `0xc6780c45cb140eb4bd24a8bcbe21ae9a6a3cce706f8da581c7094efef7202b0f`
  (2,298 actions, including 2,198 carried-history audits). It stopped before
  its first mutation when the public Substrate provider returned `Historical
  work rate limit exceeded` while authenticating `fleet.fund.167`. The journal
  remains exactly 10,040 entries; its last entry is the 2026-09-02
  `topology.launch` postcondition, so this failed preflight consumed no nonce or
  spend. Root-cause review found that every carried receipt downloaded the
  complete 334-KiB historical metadata response, often through two paths, and
  the concurrent audit scheduler dispatched every later receipt after the
  earliest failure. The adjacent lineage audit also found that a verified
  ancestor intent could be accepted while transaction lookup incorrectly used
  the descendant intent.
- the current candidate repair rechecks complete runtime version and
  `System.Code` at every historical block but singleflights exact metadata per
  independently dialed provider and complete `(version, code, metadata)` tuple.
  Successful bytes alone are cached; cancellation and failures cannot poison
  the cache; it is bounded to the active v454 plus exact v451/v452/v453
  identities and cannot satisfy the strict v454 signing boundary. Carried audits now run in ordered
  eight-item batches and stop before dispatching the next batch after failure;
  exact verified ancestor intent selects its own transaction. All affected RPC
  reads carry the five-minute per-audit cancellation boundary. Deterministic
  tests cover singleflight, provider isolation, drift, failure/cancellation,
  the fifth-identity rejection bound, exact historical throttling/retry, strict-current
  isolation, both serial and concurrent fail-fast barriers, and ancestor-intent
  lookup. The broad affected selection passed normally in 55.380 seconds and
  under the race detector in 205.110 seconds with `GOMAXPROCS=24`; simulator
  and CRv4 vet passed in 5.397 seconds. The later adjacent live-write audit
  also found context loss and latest-metadata dispatch verification in native
  watch/recovery. Finalized-head, block/hash/header, nonce, block body, event
  and read-checkpoint RPCs now preserve caller cancellation. Recovery locates
  inclusion first, then verifies dispatch exactly once with metadata
  authenticated at that receipt's block on a private chain copy before a
  finalized journal append. Deterministic ordinary/race tests cover the
  cancellation, exact-block association, malformed identity and immutable
  signing-client boundaries. Frozen-gate qualification remains mandatory.
- static-source review found an important proof gap before launch: generated
  metadata includes executable constant getters, so source equality and a
  no-`TestExternalities` upstream test alone cannot prove state independence.
  `docs/spec/runtime-metadata-artifacts.json`,
  `scripts/check-runtime-metadata-artifacts.sh`, and the pinned Rust tool under
  `tools/runtime-metadata-probe` close that gap. The mandatory producer and
  aggregate gates authenticate the testnet genesis and observation block,
  match each exact compressed artifact to on-chain `System.Code`, and execute
  `Core_version`/`Metadata_metadata` with only allocator, logging and hashing
  host functions. They require exact version, size, SHA-256, BLAKE2b-256,
  metadata-v14 and no trailing SCALE bytes; invoking a storage host function is
  a deterministic hard-failure test. Separate clean detached upstream builds
  passed `runtime/tests/metadata.rs` 1/1 at each exact v451/v452/v453 commit.
  Those builds emitted raw, not compact deployed, Wasm and are correctly used
  only as source-test corroboration. The exact compressed artifact gate passed
  all three then-active versions against the public testnet endpoint at 2026-09-04 15:36
  UTC; the frozen producer and aggregate gates must repeat it after the source
  and release lock are committed.
- the 17:00 UTC producer attempt on SN `7a4d97e` passed every substantive
  launch-critical phase and stopped only at its final fetched source fence when
  Connect advanced to `0dd6ee2`; it made no testnet write. The resulting
  generated-policy audit produced Connect `d65a05e` (tree `a942fe337a38...`)
  and SN `20431dd`. Two independent audits verified all 9 blocker and 10 CFAA
  source records, fail-closed dispositions/floors, decoded packed-table
  invariants, reserved exclusions and plausible base deltas. Exact 24-test
  runtime plus generator ordinary/race qualification, vet, format and patch
  checks pass; full root ordinary passed in 523.615 seconds. The later isolated
  full race exposed a 10-second fast-path peer-connect failure and a package
  timeout after roughly 2,000 tests; isolated `TestWeightedShuffle` race passed
  in 13.381 test seconds. Connect `b22ab0704f6dc3ecf80e91b31b5c7fafca097223`
  fixes the production-parity, unordered-carrier, bounded ownership and
  cancellation classes with deterministic adjacent regressions. Repeated
  actual-path normal/race tests and two independent reviews pass. The frozen
  aggregate must still run its new 30-minute unsharded default and fixed-order
  certificates.
  The twelve-root pre-lock freeze passed at 18:12 UTC with log SHA-256
  `106381197cc2441055ab86361b2d9956c76d793017ccd3a1be5f7f3dafd83c2f`.
  The refreshed lock is
  `sha256:998a86a4c3806e63f7c1c056401b0cb3cefb7601d6579b96f6bfddcbf2135cb5`;
  it is superseded by the completed Connect and final-evidence source changes
  and must be refreshed. The SN source commit, lock commit, frozen gates and
  live execution remain pending.
- the dirty post-`20431dd` evidence candidate closes three final-review gaps
  before source freeze. Native proof replay now binds each call/extrinsic/event
  identity, CRv4 lineage and exact parent-to-reveal stake/reward transition;
  historical EVM replay binds the coordinator implementation/runtime, operator
  version, epoch deposits, reserve principal, vault carry/credit/claim state
  and exact `ClaimPaid` receipt payload at their recorded blocks; and the
  ordinary terminal fleet audit reconstructs every signed validator cycle,
  lifecycle generation and selected/rejected top-200 boundary. The heavy
  native fixture passed normally in 53.763 seconds and under race in 477.193
  seconds. Historical EVM/receipt qualification passed normally in about 29
  seconds and under race in 175.890 seconds. Full fleet artifact qualification
  passed normally in 82.628 seconds; its isolated projection passed under race
  in 326.00 seconds. A combined ad hoc fleet race shard reached its final
  projection after every preceding test passed, then hit that command's
  undersized ten-minute package deadline while CPU-active in authenticated
  artifact hashing; the checked-in producer/aggregate deadlines are 25/90
  minutes and a static regression pins them. No product assertion, deadlock or
  race report occurred.
- a selector audit found that the first producer semantic selector omitted 15
  direct lifecycle/top-200/payout-membership regressions even though its broad
  evidence builder exercised the same code indirectly. Those 15 tests passed
  normally in 23.517 seconds. The selector now includes the exact
  `FinalSemanticFixture`, `FinalFleetLifecycle`, `FinalSemanticFleetByUIDAt`
  and `FinalPayoutAssignmentsAt` families, and a static test proves six
  critical declarations both exist and match it. The resulting complete
  selector passed normally in 87.692 seconds; its exact 25-minute race run and
  the frozen producer rerun remain pending.
- two unsafe local formatting attempts were detected before they could become
  a source commit. One broad identifier rewrite changed package and unrelated
  identifiers in four semantic-evidence files; the files were reconstructed
  from the complete edit transcript, and independent symbol/function/test
  inventories plus focused normal/race qualifications proved the recovery.
  A later comment-only automation collapsed eight native files; they were
  restored byte-for-byte in executable content from the independently verified
  snapshot and then documented manually. Both incidents share the root cause
  of applying an unscoped source rewrite to reviewed Go files. The new
  `sim-testnet/sourceguard` package parses every simulator Go file before
  simulator compilation in both release gates, requires package `main`, and
  rejects top-level function/type/value declarations named `self`. Its
  deterministic tests cover complete and partial package/function/type/value
  corruption plus the adjacent valid `self` receiver case. The recovered
  native files have no executable diff from the proven snapshot; compile, vet,
  normal and race qualifications pass. No generated rewrite is permitted in
  the remaining freeze work.
- the subsequent independent adversarial review rejected this still-dirty
  candidate before freeze for four evidence-domain gaps. First, a canonical
  payout root could be re-signed after crossing an operator/provider identity,
  changing a provider payout coldkey, changing the signed measurement window
  or reliability floor, or omitting a row from the independently audited
  provider snapshot. Second, mutually consistent semantic evidence and public
  state could authenticate unreviewed proxy/vault/reserve bytecode because the
  deployment verifier did not join its runtime hash map to the decoded plan and
  canonical release lock. Third, the terminal ordinary-fleet projection did
  not prove setup generation history: the real plan requires generation 1 to 2
  for all 200 setup head fleets, later lifecycle lineage for fleets 5/6, and
  generation-1 plus tournament evidence for challengers 201/202. Fourth, the
  producer selector omitted several direct fail-closed tests and did not pin
  the exact test census it executed. The old prelaunch estimate and both active
  long qualifications were invalidated; no chain write occurred. Three
  parallel fix lanes now add plan/release-lock bytecode anchoring, exact payout
  domain joins, and complete 200+2 generation partition/lineage. The producer
  gate now carries a reviewed sorted census and fails on zero, added, missing
  or renamed selected tests. Do not source-freeze until all four lanes and
  their adjacent normal/race regressions pass.
- public testnet upgraded at block 7,934,387 to runtime 454 after the preceding
  candidate was assembled. The release was repinned to tag `v454`, commit
  `14cde6410fe8ec81a940e290c56f94a632a0988d`, exact code hash
  `0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef`
  and metadata hash
  `0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702`.
  The exact-Wasm/metadata gate passed all four versions at 22:18 UTC, passed a
  fresh rerun after the client repair at 22:52 UTC, and the 24-file v454 source
  manifest passed against a clean tagged checkout. The selected upstream
  share-pool, pallet-subtensor claim-root/migration, runtime claim-root, and
  runtime metadata compatibility regressions (implemented by upstream in Rust)
  all pass against that exact commit. The
  adjacent four-entry cache regression and seven continuous v454 custody
  decision cases pass. The refreshed frozen producer/aggregate gates remain
  mandatory before launch.
- the current historical-EVM evidence review found a separate P0 chronology
  defect before live capture. `finalSemanticHeads` combines current campaign
  checkpoints with carried setup receipts, while the first v6 runtime audit
  required the terminal eight-executable census at every such head. That
  assertion is impossible: authoritative attempt-4 fleet generation-one and
  operator-registration receipts predate later coordinator upgrades, the
  fleet batcher, and the still-unexecuted current implementation. The repair
  must preserve canonical-head replay for every receipt, apply the complete
  current release-root census only from the signed campaign boundary onward,
  and bind each carried mutation to its archived predecessor plan's exact
  proxy/implementation identity. It must also range-audit ERC1967 `Upgraded`
  events across the acceptance window so an upgrade/restore between sampled
  heads cannot be hidden. Deterministic old-receipt/current-runtime,
  current-refresh/batcher, and transient-upgrade-between-heads regressions are
  mandatory before the semantic census is frozen.
- the follow-on production-shaped chronology review found additional P0
  omissions before any new testnet write. The exact allowed plan/journal
  lineage contains seven completed coordinator upgrades, two bootstrap policy
  schedules, and the fleet-oracle activation/restore pair that were not all
  reachable from the original receipt-family selector. The target-derived
  census now requires every finalized pre-campaign coordinator call; its
  artifact replay must prove the exact plan, intent, sender, value, canonical
  ABI calldata, complete event graph, postcondition and runtime identity. The
  temporary fleet oracle proof must additionally seal activation,
  await-active, every one of the twenty generation-two batch positions,
  restoration and await-restored, with one batcher target and strict journal,
  finalized-head and transaction ordering. Historical receipt blocks belong
  in the canonical EVM-head census. Implementation identity must be derived
  per proxy from ordered upgrade history: the deployment-manifest base address
  is stale after an upgrade and cannot authenticate later calls. A combined
  immutable-snapshot artifact-plus-public-RPC verifier is required so a peer
  reviewer cannot accidentally run only the RPC half. These repairs and their
  omission, substitution, ordering, same-block ambiguity and target-mismatch
  regressions remain pre-freeze blockers.
- a live read-only capability probe at 2026-09-05 01:22 UTC established the
  official EVM provider's exact log-range boundary: `eth_getLogs` succeeds for
  an inclusive 1,000-block range and rejects 1,001 or more blocks with its
  `rpc_shape`/`shape_denylist` policy. The complete coordinator-upgrade census
  must therefore issue contiguous, non-overlapping ranges of at most 1,000
  blocks, include every inclusive endpoint exactly once, and bind every chunk
  into the public transcript. Deterministic regressions must reject a missing,
  duplicated, overlapping, reordered or oversized chunk. A single full-history
  range is not compatible with the configured public RPC.
- the resulting chronology v8 candidate now distinguishes execution and
  post-transaction implementation/runtime identities, transaction-index order,
  proxy initialization baselines, every historical upgrade, and the temporary
  fleet-oracle activation/restoration window. It binds the historical proxy
  plus await-active/await-restored operational and observer checkpoints through
  EIP-1898 public calls. Focused normal passed in 22.902 seconds and focused
  race passed in 238.049 seconds; vet, format and patch checks are clean. An
  independent source audit found no remaining production/public fail-open in
  this v8 path. This is prequalification, not the frozen producer gate.
- the same public-provider probe exposed two adjacent release blockers: the
  final live collector still used 2,000-block ranges and the validator deposit
  scanner used 10,000-block ranges. Both exceed the provider's exact 1,000-block
  ceiling. The collector is being unified with the chronology splitter; the
  validator scanner requires the same inclusive ceiling and contiguous-range
  regressions before launch.
- the public `ContractDeployment.deploy_block` currently records 7,900,646,
  the later fleet-batcher deployment, while the active coordinator proxy was
  created at 7,895,374. Server event sync documents this input as the first
  coordinator event block, whereas semantic capture also used it as a
  current-release-complete boundary. These meanings must be separated and the
  event-sync boundary reconstructed from the authenticated
  `evm.coordinator-proxy` journal/receipt; otherwise a clean operator database
  can skip earlier registrations and deposits.
- the chronology hardening deliberately made the release-scale semantic fixture
  fail rather than exempting it: the fixture did not contain the mandatory
  production-shaped 200-fleet generation, historical timeline and oracle-window
  source graph. Closing that fixture already exposed two production defects:
  fleet-generation mutation lookup returned a finalized row without the
  verified postcondition path, and the historical capture census misclassified
  finalized native actions as EVM actions. Both fixes require the complete
  production-shaped fixture and widened gate to pass.
- the producer gate now includes a canonical 234-test semantic census,
  `sim-testnet/semantic-integrity-tests.txt`, SHA-256
  `7bf4cfc9865d3976d70fed8f05318d326d22d1702c456850a67ecdbb1e6ad66f`.
  The source declarations and compiled test listing match. The audit also
  closed omissions for builder/deposit recovery, UID zero, public completion,
  historical chronology, runtime identity and nil-wrapping diagnostics.
  Independent source-group checks prevent regenerating the census from a
  weakened selector. Terra independently matched the exact 234 source and
  compiled declarations to that digest; pinning passed in 8.946 seconds
  normally and 13.720 seconds under race. Evidence is preserved under
  `/home/by/urnetwork/temp/terra-census-gate.U6pYjR/`.
- the read-only doctor at 2026-09-05 00:55 UTC reached MinIO and both official
  public testnet RPCs, authenticated runtime 454 and netuid 521, and passed the
  wallet, budget, subnet-owner, UID-capacity and historical-RPC checks. Its
  only hard failure was the intended release-lock dirty-tree fence. The two
  independence checks were soft failures because the configured public
  override deliberately uses the same official provider for operational and
  comparison reads until the private node is synchronized.
- a 2026-09-05 read-only live probe proved that Subtensor's EVM RPC block hash
  cannot be reconstructed with go-ethereum's `types.Header.Hash()`: finalized
  block 7,936,619 reported
  `0xaad46c25ee81b4f9f636677c1b9197a146733e8f16d57114269030ddf26790e2`,
  while the decoded header recomputed to
  `0x5003547fb9327ffdcaca8b57bf0cea6db9e08efbcabaae898635e22e778fd52d`.
  The server and simulator's primary EVM readers already preserve the explicit
  JSON-RPC hash. The validator exact-block reader, miner transaction finality,
  claim-daemon receipt recovery, reward-stake/final-log capture and fleet
  lifecycle parent checks contained adjacent local-hash uses. Replace every
  identity check with explicit `eth_getBlockByNumber`/`eth_getBlockByHash`
  number-and-hash decoding and deterministic synthetic-hash regressions before
  source freeze; no `Header.Hash()` result may authenticate a Subtensor block.

The following 2026-09-05 05:40 UTC checkpoint supersedes the open-fix wording
above; all results are prequalification until committed and release-locked:

- Explicit RPC block identity is implemented across validator, miner finality,
  claim recovery, reward/log/baseline capture and fleet cleanup. Terra passed
  the complete miner/onchain and miner suites normally and under race, and
  the complete validator suite in 34.480/291.464 seconds. CRv4 and the remaining
  SN packages also passed ordinary/race/build/vet.
- The production-shaped 200+2 fleet fixture now retains both challenger
  registration postconditions, all 402 commitment proofs, and every carried
  write proof. Historical address comparison preserves checksummed immutable
  plan targets while enforcing canonical published evidence. The verifier
  loads nested historical receipt proofs and withdrawal-only claim-payment
  proofs. An independent recursive census checks every locator occurrence;
  fixture replay uses only the production-loaded cache. Terra's combined
  fixture passed in 135.311 seconds normally and 1,152.135 seconds under race.
- Full ordinary simulator qualification ended with failures after 2,036.909
  seconds, log
  `/home/by/urnetwork/temp/terra-sim-full-20260905T044358Z.log`. These were
  investigated rather than waived: runtime/UID-zero/public-bundle and archive
  boundary/emitter fixtures were stale; empty bytecode diagnostics wrapped a
  nil cause; the semantic census was absent; the dirty release-lock fence
  correctly rejected the staging tree. Source-builder review additionally
  reproduced real acceptance flaws for stale cumulative deposit prefixes,
  missing recovery validators and unchecked per-validator observation heads.
  Those fixes now require the complete deposit sum through each signed head,
  exact configured validator membership and canonical positive event amounts.
  Terra's builder ordinary/race runs passed in 1.034/6.017 seconds. Its
  remaining public-bundle/edge race confirmation is running.
- The full server controller suite passed in 1,718.134 seconds normally and
  1,987.628 seconds under race with the matching vault-backed local profile.
  These results cover the pre-integration `d184121d` base plus local fixes. The failed
  portable-profile invocation used a password different from the healthy
  existing local PostgreSQL container. No service, volume or credential was
  changed. The launcher now checks application authentication over the Docker
  bridge route, avoiding PostgreSQL's trusted localhost shortcut, and checks
  CREATEDB capability. Deterministic launcher ordinary/race tests pass. Three
  Brevo protocol tests now isolate dummy credentials as well as HTTP endpoints.
- Foundry passed 156 tests with zero failures/skips and 4,608 invariant calls;
  all four deployable roots passed Slither with zero high/medium findings.
  Exact v451-v454 Wasm/metadata, v454 source and the documented upstream Rust
  compatibility tests passed. Operator-proxy ordinary/race/build/vet passed.
  Detailed Terra logs are under
  `/home/by/urnetwork/temp/terra-gates.sqQPsP/` and
  `/home/by/urnetwork/temp/terra-server-gate2.OzFSaY/`.
- A fresh fetch before source freeze found additional upstream work. SN is
  one documentation commit behind; server is 17 commits behind
  `b61797b5617e1e754bc016327fa6588f881f1835`; Connect is eight behind
  `1b81da6668e6a3ec9536ac61a07b27a619738cc7`; SDK is three behind
  `e1d8dc8d9682daefd86878fea911b7b643634406`; proxy and xops are each one
  behind. The server fixes were checkpointed and rebased cleanly as `987c7756`
  on `b61797b5`; Connect, SDK, proxy and xops were fast-forwarded to those
  fetched heads. SN's documentation-only upstream commit is still pending.
  Terra is checking affected client/infrastructure tests. Do not treat the
  old-revision results as a certificate for these new inputs.
- Reviewing the incoming server multi-hop payout allocator found a blocking
  cross-module attribution defect: it combines same-network participants into
  one network sweep row under the lowest client ID, while
  `GetStEpochProviderUsage` groups by that client ID. Two service clients can
  therefore lose individual usage, wallet/reliability assignment and head-tier
  exclusion semantics despite conserving the network payment total. Astra owns
  the repair on canonical server: preserve exact per-client allocations in the
  same atomic sweep, distinguish legacy rows, reject ambiguous legacy
  attribution, and add mixed-history/head-exclusion/conservation/idempotence
  tests. Incoming allocator, Connect auth/TCP and SDK JWT tests also need to be
  included in the release selectors. This repair and its independent Terra
  qualification remain blockers before the release-lock refresh.
  Terra reproduced the pre-fix error through real PostgreSQL settlement:
  `TestContractPayoutPreservesSharedNetworkProviderUsage` returned one client
  with 121 bytes instead of two clients with 61/60 bytes. Exact red record:
  `/tmp/urnetwork-server-contract-payout-prefix-red-20260905T054341Z.log`,
  SHA-256 `7c0d983b37a9857d79c9398cc7a0132c292142a155dcdb1e7bdd0de385f0a42b`.
- The 06:20 UTC continuation checkpoint narrows the remaining pre-freeze work:
  Connect's 22 auth/TCP tests, 24 canonical policy tests and both generator
  packages passed ordinary/race; SDK's 22 token/points tests passed both ways;
  Connect/SDK root build/vet and xops's documented 88-test Ansible suite pass.
  The provider repair is implemented as migration 631 plus an atomic per-client
  byte/revenue snapshot. Expanded model ordinary/race passed in 98.807/111.130
  seconds; controller provider-input ordinary/race and migration-monitor
  ordinary/race also passed. This repair and the initial strict-test fix were
  committed, rebased and pushed as server `df90d425`. The first
  expanded run caught a new test incorrectly expecting duplicate `CloseContract`
  success; it now asserts the existing terminal rejection and verifies unchanged
  allocations after the idempotent `SettleEscrow` boundary. Its preserved red log
  is `/tmp/urnetwork-server-expanded-model-ordinary-330tHa.log`, SHA-256
  `a51ee31e720d37efd8d58545680ac1fe6b6015067fe02a316f12354e5016986f`.
  Adjacent review also proved that republishing mutable stream membership after
  a client changes network could launder an ambiguous legacy sweep into apparent
  sole-provider credit. The reader now refuses NULL-allocation stream sweeps;
  only immutable endpoint-backed non-stream legacy rows can use the fallback.
  The deterministic pre-fix red log is
  `/tmp/urnetwork-server-legacy-membership-prefix-red-rMQgeF.log`, SHA-256
  `a930c866a4fcb14eb7ad87f8f4d124ea28c37b81b56e2088d0d671f7a5bb8fa5`.
- The adjacent provider-reporting audit reproduced the same representative-ID
  error in all three provider statistics APIs, historical revenue leaking when
  clients change networks, a missing upper time-window boundary, and malformed
  allocations being accepted. The repair shares the exact settlement
  allocation/legacy validation CTE with subnet usage and reads all client/day
  totals in one network-scoped query. It preserves the existing active,
  top-level-provider API selection: the account ledger retains historical
  network revenue even when a moved provider no longer appears in that view.
  Invalid scoped allocations return an error without partial totals. Six new
  regressions, the existing three provider roots and the real-query
  `TestStatsQueryPlans` are pinned in both release gates; qualification is
  pending. The first red run's empty-array fixture was rejected by the schema,
  not the reader; the corrected seven-malformed-array run independently proves
  reader acceptance and must be retained with that distinction. The first
  postfix ordinary run passed the reader regressions but failed the exact-query
  plan guard: modern snapshots still performed unnecessary legacy contract
  lookups, including full-table scans. Astra owns the measured query repair;
  Terra must rerun the unchanged plan guard and full affected ordinary/race
  selection. Do not treat a correct total as sufficient evidence of a bounded
  production read path or drop the plan check to obtain a pass.
- Release gates now set `WARP_TEST_ENV_FAIL_FAST=1`; normal developer retries
  remain available. Independent subprocess tests also reproduced abandoned
  teardown, failure followed by skip, invalid/overflowed attempt counts,
  callback/setup/teardown `Goexit`, compatibility `panic(nil)`, and late/cleanup
  assertions incorrectly yielding success. One bounded patch closes these
  demonstrated paths; an independent Astra review found no remaining concrete
  correctness/race issue in that patch. The complete ordinary/race strict-test
  matrix passed in 2.278/7.269 seconds; the frozen gates must still rerun it.
- The old edge/public-bundle race command has terminated with a real 30-minute
  timeout, not an observation timeout. Its stack was CPU-active in complete
  artifact hashing/replay; no race was reported. The corrected test scheduler
  retains all 234 canonical semantic roots, all 18 public-bundle cases, and the
  full 1,000-miner/202-candidate graph, with four simultaneous heavy roots and
  four independent views inside the public-bundle root. Each adversarial view
  must reach its intended owner/replication/hash rejection and cannot count
  transport/body failures as success. Quick ordinary/race pin checks passed;
  final expanded pins and immutable-binary ordinary/race replay measurements
  remain pending. No coverage or 15/25-minute producer deadline was waived.

Immutable focused records for this checkpoint (all local prequalification,
not a frozen gate or live-chain certificate):

| Scope | Raw record | SHA-256 |
|---|---|---|
| Core allocation ordinary, pass | `/tmp/urnetwork-server-expanded-model-ordinary-postfix-hBzP6t.log` | `4a710f646c42e9ff254a3916f725b6ea1c2262b8327b9adc2cefecc83d3cca8e` |
| Core allocation race, pass | `/tmp/urnetwork-server-expanded-model-race-postfix-91GnxN.log` | `abc76b77bd87ed738cd57c29a9653c21e90bdcb14c9aab77c8697c1449e6bf99` |
| Controller provider inputs ordinary, pass | `/tmp/urnetwork-server-controller-provider-inputs-ordinary-HTCedY.log` | `5903ea225e87dd0c65dc6d0d0506090844f53ad170e68a26fc946919c2982894` |
| Controller provider inputs race, pass | `/tmp/urnetwork-server-controller-provider-inputs-race-XkgSKv.log` | `cdab49d2c7603d2db0194a65abaa9894600feafa1784e3c2dc18ca87e681c89f` |
| Migration monitor ordinary, pass | `/tmp/urnetwork-server-monitor-migrations-ordinary-yUaxvB.log` | `ff4d6bee05e1d293e09ac36edc4d77162e18fbaf8566770583b85cde67d2d9c5` |
| Migration monitor race, pass | `/tmp/urnetwork-server-monitor-migrations-race-x1x0Ra.log` | `8adf5005303db4a3600260df257c0f156884c5b8b5be1277726e5ba8533bb32d` |
| Abandoned-teardown pre-fix red | `/tmp/urnetwork-server-teardown-prefix-red-d2eP9A.log` | `221c520a0176c40574cba5be483e91b83551e8ac9db41f1df1ffdcd4dd40bd5c` |
| Nine false-green pre-fix classes | `/tmp/urnetwork-server-falsegreen-prefix-red-ZDsMXO.log` | `336f1ae309b23c18cc8e61ca1fabf26d8aa02d2cc1c3eb049f37e9996e0a2c17` |
| Complete strict-test matrix ordinary, pass | `/tmp/urnetwork-server-strict-meta-ordinary-final-Hr3oNj.log` | `4b762dee0688de7651e393e048b0cdf5126d766f3d289cae0fcee0326c83a11b` |
| Complete strict-test matrix race, pass | `/tmp/urnetwork-server-strict-meta-race-final-vHyItH.log` | `b0a503acce226690330087751c5e5acad6e8ff4077bde190f2b651e68103ee9b` |
| Provider reporting pre-fix red, including one fixture error | `/tmp/urnetwork-server-provider-stats-prefix-red-XZM9yq.log` | `dc73d6cfa32d92e35554d02f1691dd640e516e60d308f84b023ed52909407d02` |
| Corrected malformed-allocation reader pre-fix red | `/tmp/urnetwork-server-provider-invalid-prefix-red-Wap1M8.log` | `d8076b2553445912d2d4685111d4409cb82d17fb77bfd513c85fdac2391fdee3` |
| Provider postfix ordinary, query-plan failure | `/tmp/urnetwork-server-provider-stats-ordinary-postfix-qYgAdA.log` | `465d2d30b7a7cb0663ee9375dfa82151d2ddf5b474aa0dc6f2924351a90d4873` |
| Old serial edge/public-bundle race timeout | `/home/by/urnetwork/temp/terra-gates.sqQPsP/sim-edge-fix-race.log` | `93ce17308caa0d043d3f116abb6737cf3af89f2e31ad39dae109b3c74fbe02c0` |

- No final live campaign is running. The exact remaining chain-clock range is
  still 10:00:48--14:36:00 after preparation. A conversational claim that the
  complete campaign takes only 4h36 was incorrect: that is the production
  phase's upper bound and omits release qualification. FINAL.md and final
  public-chain replay remain required after both live phases.

Do not infer an unselected full Server model/repository pass, aggregate pass,
or live campaign result from the focused records. The source freeze and
producer pass are separately recorded above.

| Item | Result | UTC / immutable reference |
|---|---|---|
| Narrow 1,000-miner semantic supplement test | pass before freeze; frozen rerun pending | 226.948s mocked semantic replay; section 3; current exact widened producer selector normal pass 87.692s |
| All final semantic ordinary tests | prequalified; frozen rerun pending | `7d634c4`; 204 selected test/subtest names, 416.901s package / 447.81s wall |
| All final semantic race tests | prequalified in shards; frozen aggregate pending | `7d634c4`; affected heavy selection 524.891s; latest worker/cache/stdio selection 222.138s |
| Full sim-testnet ordinary | pending on current candidate | prior aggregate was killed only by the corrected implicit 10-minute package deadline |
| Full sim-testnet race | pending | |
| Producer gate | prior candidate passed; current rerun pending | `5d779cd`; 2026-09-04 UTC |
| Aggregate gate with DB tests | current rerun pending | `5257b2f` attempt reached ordinary simulator tests and timed out at exactly 600.146s with no assertion failure |
| Foundry | pre-freeze pass; frozen aggregate rerun pending | Foundry 1.7.1; format/build clean; 156/0/0; 4,608 invariant calls at 2026-09-04 22:52 UTC |
| Slither | pre-freeze pass; frozen rerun pending | Slither 0.11.6 analyzed all four deployable roots with 0 high/medium findings at 2026-09-04 22:54 UTC |
| Exact v451/v452/v453/v454 metadata artifacts | prequalification pass; frozen gate pending | public-chain exact-Wasm and decoded-metadata gate passed all four versions at 2026-09-04 22:18 UTC and again at 22:52 UTC; v454 static source and all selected exact-upstream Subtensor qualifications pass (the upstream test suite is Rust; UR remains Go/Solidity) |
| Server release-selected DB/proxy qualification | pass; frozen gate pending | `d184121d6b33ecf0253be92167f74e672ff7229f`; affected normal/race/vet, managed controller 108.45/164.81s, and proxy lifecycle 19.97/43.19s |
| Server unselected full model/repository suites | pending if required by final gate/diagnosis | no broad pass inferred from focused selection |
| SN runtime candidate | generated-policy and abigen qualification pass; frozen gate pending | pushed source `20431dd`; exact selector/static normal/race, stabi, shell syntax, and v1.17 generated-byte checks pass |
| Server candidate commit | affected qualification pass; frozen gate pending | `d184121d6b33ecf0253be92167f74e672ff7229f` |
| Connect candidate commit | focused fix qualification pass; frozen unsharded certificates pending | pushed `b22ab0704f6dc3ecf80e91b31b5c7fafca097223`, tree `000160e9679bb1636621d3b6d990f920866ca582`; deterministic adjacent helpers and actual fast-path/WebRTC paths passed 20x normal/race, canonical 10x replay and two independent reviews pass; aggregate now requires default ordinary/race plus fixed-order race with 30-minute deadlines |
| SDK candidate commit | affected qualification pass; frozen aggregate pending | `2f3e7058873498099a88aee3e158caa11aefbda1`; full root normal 443.170s, changed focus 243.280/245.255s, nested build/cgo normal/race/vet, all Go files formatted |
| Other candidate repository commits | clean/equal; affected qualification and frozen gates pending | exact eleven non-SN revisions in section 2; twelve-root pre-lock fence passed at 18:12 UTC |
| Release-lock hash | superseded pre-fix render; refresh pending | `sha256:998a86a4c3806e63f7c1c056401b0cb3cefb7601d6579b96f6bfddcbf2135cb5`; only Connect Go and protocol-source hashes changed before the full-race blockers were found |
| Two approval-identical plan builds | superseded; current rerun pending | prior approval projection `sha256:c2611372cb02fb40bf6f7468ce09b6296a4eaefeedcf6f9575bbfa9291fb79ff` |
| Approved plan hash/spend | superseded; current rerun pending | prior 2,298-action plan `0x39e2c74bfd93cf8a42f5f3172f3683f85b4a1e45d759096cbcafa4539352fc48`; never use it with the current lock |
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

    "$SIM_TESTNET_BINARY" stop \
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
- create a separate approval-identical mainnet plan review. Never reuse the
  testnet plan hash or keys.

The final handoff is complete only when another agent can start with this file,
authenticate the exact frozen checkout and attempt-4 state, and execute the
remaining steps without relying on private conversational context.
