# Release 1.0 testnet completion handoff

Status: live working document, first written 2026-09-03 UTC and last reconciled
2026-09-05 09:53 UTC before the final source freeze. Refresh every item marked
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

Launch blockers at this checkpoint:

- The pre-repair 236-root ordinary qualification passed, including all 18
  public replay cases; its race counterpart failed at the 25-minute deadline
  at 08:16:53 UTC with 235/236 roots and 16/18 public cases completed. The
  owned-byte digest/fleet-mutation repair is committed and pushed as
  `140b7ca3ffdb513ea489031e51b8f1b27e7b6e04`. Its deterministic pre-fix RED
  and focused ordinary/race GREEN results are now recorded in section 12.
  The exact 237-root ordinary run passed at 09:04:06 UTC, including all 18
  public cases, in 266.65 seconds. The same capture's complete race run failed
  at 09:30:00 UTC at its unchanged 25-minute deadline: 236/237 roots and all
  18 public cases passed, but `TestFinalSemanticEvidenceBuildRenderAndArtifacts`
  was still executing. This is an actual failed qualification, not a stalled
  observer. Its root-cause profile/repair is in progress; do not rerun the
  unchanged suite or waive the remaining root. Parser integration also requires
  a fresh expanded census. Final lock/freeze and release gates remain outstanding.
- Both Terra-max agents recovered from their earlier usage-limit errors.
  Test execution remains Terra max and diagnosis/repair remains Astra max;
  no assignment change or duplicate simulator test process was needed.
- The proof-parser defect is now reproduced: a valid signed record with a
  malformed suffix was accepted even after the artifact's exact size/hash were
  recomputed. Five adjacent YAML loaders made the same mistake of ignoring a
  non-EOF second-decode error. Six EOF guards and deterministic regressions
  passed the bounded 12-root ordinary/race selection in isolation; race ended
  09:38:43 UTC. Astra integrated the patch and explicit gate coverage,
  including census 237 to 238 with no prior root removed. Terra also verified
  the integrated 19-root framing/semantic-pin cut ordinarily and under race.
  Those focused results
  do not qualify the integrated release source or resolve the separate timeout.
- The user's request for "validator evidence stored in the contract pool"
  needs a launch-scope decision. WHITEPAPER.md section 11.1 explicitly stores
  completed-trail/statistics artifacts in the server API and MinIO. The active
  coordinator/vault stores a payout root and payout-artifact hash, but that
  artifact's provider/operator/fleet snapshot preimages do not commit validator
  trail proofs. Native CRv4 commits the weight payload, not the signed
  measurement envelope which refers back to its extrinsic. Thus there is no
  direct or transitive on-chain commitment to those validator proofs today.
  A question has been sent: retain the current whitepaper on-chain/off-chain
  split, or add an on-chain validator-evidence commitment before launch. No
  answer is recorded. Do not silently treat off-chain proofs as satisfying
  the stronger request, or implement a new contract commitment/validator bounty
  without resolving that choice. Safe existing qualification may continue;
  final freeze/producer/live handoff stays paused pending the decision.

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

The first-draft table is retained below as historical provenance. A fresh
fetch-only inventory at 2026-09-05 08:57 UTC found all twelve repositories
clean, with every HEAD equal to its fetched upstream. This snapshot precedes
the present documentation-only handoff update; it is not a final source fence.

| Repository | Branch | Current checkout revision | State |
|---|---|---|---|
| sn | main | 140b7ca3ffdb513ea489031e51b8f1b27e7b6e04 | clean/equal at the 08:57 fetch; digest/fleet repair has deterministic pre-fix RED, focused ordinary/race GREEN and complete 237-root ordinary PASS; race FAILED at 09:30:00 UTC with 236/237 roots; later documentation-only checkpoint 9fa5109 preserves captured code, parser integration is subsequent and unqualified |
| server | main | b12af6b3aa18adb7b4e84251b2b8ab15c35f7ddc | clean/equal after the 06:47 checkpoint; allocation, adjacent provider-reporting/query-plan, retention and strict-test repairs pass focused ordinary/race; frozen gate pending |
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

The tracked lock reverified at the 08:19 UTC checkpoint has SHA-256
`009566be02a32f77b5a5708432eee71c694668af7c5bded90e4b373c38f143db`;
it already includes the runtime-454 artifact update. It is still not current
for the subsequent source changes. Section 12 records a newer, unapplied
clean-source preview; neither historical lock nor preview authorizes launch.

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

    go test ./sim-testnet -run '^TestFinalSemantic' -count=1 -parallel=4 -timeout=30m
    go test -race ./sim-testnet -run '^TestFinalSemantic' -count=1 -parallel=4 -timeout=45m
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

A preliminary, strictly non-applying doctor may run while these local tests
finish, to find independent infrastructure or wallet blockers early. The
2026-09-05 06:51 check returned 61 passes and three failures: the expected dirty
source/release-lock hard failure, plus advisory `config/independent-rpcs` and
`rpc/substrate-physical-independence` warnings because the public override
shares its provider and peer. Every independent hard host, tool, Docker,
systemd, MinIO, RPC/runtime, wallet, budget and carried-state check passed.
This is not a ready doctor or launch authority. Preserve its owner-only report
at `/home/by/urnetwork/temp/sn-preflight-3e2Yfm/doctor-report.json` (SHA-256
`7080f7a3dfbd3366becaba35efffcd26cd88ed3943922642c3367b32a5cb810f`).
The fresh post-gate executable and ready doctor below remain mandatory; never
apply using the preliminary dirty binary or claim physical RPC independence.

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
| Validator path proofs | both validator identities/stake/permit/trust, fresh signed proof for every validator/operator/epoch pair, measurement and envelope hashes, cut/checkpoint lineage, anti-replay and invalid/tampered rejection; explicitly label these proofs off-chain under current section 11.1, and record the pending user decision before claiming on-chain anchoring |
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
  archive, observability, operational, and multisig mainnet deltas;
- the actual evidence trust boundary: which hashes and values are committed
  on chain, which artifacts are signed off chain, and the recorded resolution
  of the validator-evidence commitment question;
- the configured loopback-only operator archive reachability. The current
  signed origins are http://127.0.0.1:18081 and http://127.0.0.1:18082, as the
  user requested for this simulator. Successful same-host replay does not
  establish internet reachability or readiness to admit off-host miners.

Do not infer success from a local summary. Every material statement must link to
signed artifacts and, where applicable, immutable chain state.

The current validator-proof boundary can be independently checked in
`evm/src/STCoordinator.sol` (`RootCommitment`, `commitOperatorRoot`),
`payoutartifact/artifact.go` (`Artifact`, `Build`),
`../server/controller/st_controller.go` (operator/fleet snapshot preimages),
`crv4/payload.go` (`Payload`), and `validator/release_steer.go` (measurement
envelope referencing the prepared extrinsic). The off-chain back-reference
does not create an on-chain commitment in the opposite direction.

### 10.1 Conditional validator-proof commitment work

This is a read-only implementation proposal prepared on 2026-09-05 UTC while
the user decision remains pending. It is not implemented, approved, or a claim
that current validator proofs are anchored. Do not start it merely because an
automatic goal continuation arrives.

For a precisely frozen proof census available before a real payout-root commit,
the smallest identified route is a versioned payout-artifact extension:

1. Export immutable validator-signed attempt cuts containing completed proof
   records. Pin deployment/genesis/netuid/coordinator, operator, closed window,
   validator VPK and its authenticated native identity; verify the existing
   `VerifyAttemptLedgerCut` and `VerifyProofRecord` signatures/key histories.
   Reject omitted, duplicate, cross-operator, incomplete or late cuts against
   the exact required census, not merely a positive proof count.
2. Publish those bytes and a canonical manifest through an operator-owned
   ingestion/relay path before signing the payout artifact. The operator must
   not manufacture the validator's FINAL co-signature or give its artifact key
   to validators. `StPublishEvidence` currently authorizes only the configured
   operator artifact signer. The public proof-history API uses creation time
   and a 10,000-row cap; it is not an exhaustive closed-window collection API.
   Define signed completion/cutoff semantics and complete snapshot/cursor rules.
3. Put the manifest hash, size and exact census inside a new required artifact
   schema's canonical unsigned body. `payoutartifact.Sign` hashes that body
   with signer/signature/content-hash fields cleared, not the final signed
   blob. `StCommitPayoutRoot` sends that content hash to the existing
   `commitOperatorRoot`; coordinator state/events and finalized vault
   entitlement retain it. The resulting binding is chain artifact hash ->
   canonical payout body -> manifest -> signed cuts/proof bytes. A field added
   only to an outer publication envelope or FINAL.md does not create that link.
4. Preserve exact legacy-v1 decoding/hashes for carried history and pin an
   activation epoch/profile; a historical v1 artifact cannot satisfy the new
   requirement. Update server construction/readers, validator deposit-audit
   decoding, collection/publication, and FINAL/public replay together. Final
   verification must join every required proof/cut to the authenticated
   on-chain artifact hash and fetch/verify all referenced bytes.

Timing and coverage are hard constraints. Payout computation caches its first
published artifact; retries cannot add later proofs. Freeze the census before
sign/publication and the unchanged root-commit deadline. A later deposit audit
already references an earlier payout artifact: do not make that earlier
artifact commit the later audit or the final public supplement, creating a
hash/timing cycle. Raw completed proofs/cuts must exist independently first.
No-leaf or missed-root epochs have no new payout commitment, and contracts
reject a zero root. Never manufacture payout leaves, change carry economics,
or falsely backdate a later anchor to solve this. If the requirement includes
late/no-payout windows independently of a subsequent genuine payout root,
use a separately authorized write-once evidence commitment instead. That
alternative needs a coordinator upgrade/new anchored contract, ABI/bindings,
release-lock/runtime identity, history/activation handling and contract tests;
it does not require a validator bounty or registry.

Required tests for either approved design include:

- exact v1 golden history plus v2 canonical/domain/census and wrong-key/hash
  rejection, with unchanged provider rows, shares, payout roots and economics;
- real signed-cut/proof corruption, forged co-signatures, server-key rotation,
  identity/epoch/operator substitution, omission, duplication and truncation;
- immutable publication/colliding writers, complete cutoff pagination,
  late-arrival and restart/idempotency controls, missing proofs rejected before
  commit, and explicit no-payout/missed-root behavior;
- FINAL/capture/public-replay cross-links, fake/wrong-domain on-chain anchors,
  missing replicas, omitted validators and post-activation legacy downgrade;
- if contract storage changes: unauthorized, duplicate and wrong-window
  commits, upgrade/history replay, custody invariants and unchanged payouts.

Preserve all current semantic roots and all 18 public replay cases; add the
new deterministic coverage, then regenerate the exact census and release
gates. Hash anchoring proves an immutable byte commitment, not proof truth or
availability: signature replay and actual public byte retrieval remain required.

## 11. Independent peer-review procedure

From a clean compatible checkout with no simulator state or wallet secrets:

The reviewer must also be able to reach the exact origins in the signed
manifest. With the current loopback configuration this means the same host or
explicitly arranged forwarding that preserves those origins. Never rewrite
signed URLs to make an off-host replay appear to pass. Off-host/public service
readiness requires separately configured public DNS/TLS/API/history readback
and a newly authorized manifest; it is not established by this local profile.

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
  `TestStatsQueryPlans` are pinned in both release gates. Focused qualification
  is green; the frozen gates remain pending. The first red run's empty-array fixture was rejected by the schema,
  not the reader; the corrected seven-malformed-array run independently proves
  reader acceptance and must be retained with that distinction. The first
  postfix ordinary run passed the reader regressions but failed the exact-query
  plan guard: modern snapshots still performed unnecessary legacy contract
  lookups, including full-table scans. A guarded, primary-key-only lateral
  lookup and materialized network/time scope fix the measured plans without
  an index or schema change. All 16 exact plan assertions now pass. The full
  15-test provider ordinary/race selections passed in 230.531/250.269 seconds.
  The three newly selected rollup/retention roots passed ordinary/race in
  32.607/38.048 seconds. These repairs and the complete strict-test repair were
  committed, pulled and pushed as server `b12af6b3`. Do not
  treat a correct total as sufficient evidence of a bounded production read
  path or drop the plan check to obtain a pass.
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
  the final expanded pin initially failed because the whole-file provider
  guard also covers three search-rollup/retention tests. Both selectors and
  their exact expectation now include those tests; the corrected gate pin
  passes ordinary/race. No coverage or 15/25-minute producer deadline was
  waived.
- The captured parallel public-bundle ordinary run exposed a distinct fixture
  error after 217.11 seconds: the negative case for missing approved scenario
  assertions did not remove an assertion, because the shared fixture now
  supplies a valid, complete assertion set. It instead reached a later missing
  supplement rejection. Astra repaired the signed per-view negative bundle by
  removing exactly one approved assertion, recalculating its count/hash and
  signing both operator views. Every case now uses the real campaign-result
  validator, with the exact missing-assertion rejection required at both
  operators in that negative case. Accepting an arbitrary rejection is not a
  fix. All 18 cases and the positive control remain. The old-capture complete
  234-test ordinary run finished in 271.37 seconds with only this same fixture
  failure; it is diagnostic, not a pass. The corrected immutable full ordinary
  run passed all 234 roots and 18 public cases in 264.39 seconds, with peak RSS
  1,991,188 KiB. Its complete race run began at 06:51:25 UTC and hit the
  unchanged 25-minute test deadline at 07:16 UTC. All 234 roots were selected
  and started, 233 passed, and only
  `TestPublicScenarioBundleRequiresReplicatedOwnerCompletionCommit` remained
  running after 7m33s. No assertion failure, race report or public per-case
  completion line was emitted. The internal timeout, not the outer watchdog,
  ended the process with exit 2 after 1,500.60 seconds and peak RSS
  4,721,032 KiB. This is a real failed qualification and blocks lock refresh
  and launch. The capture is closed at
  `/home/by/urnetwork/temp/sn-postrepair-public-capture-OfHWG8`, so commits do
  not alter its preserved binaries. Its exact 234-root census is
  `7bf4cfc9865d3976d70fed8f05318d326d22d1702c456850a67ecdbb1e6ad66f`.
- The 07:18 UTC performance investigation preserves that failed run rather
  than retrying it unchanged. The remaining public root had not dispatched its
  18 case workers at timeout. Astra owns the three affected test files and is
  examining its setup work, adding bounded phase timing, and removing a proven
  duplicate uncached derivation of the complete role set after `buildPlan`
  already populated the exact-key detached-copy cache. Terra is profiling the
  original immutable ordinary binary's complete public root in parallel. The
  duplicate alone is not yet proved sufficient to meet the deadline. Require
  all existing 234 roots and 18 public cases, real validator calls and intended errors,
  full cardinalities, detachment/concurrency checks, and a new complete
  ordinary/race qualification. Neither a focused profile nor 233 passed roots
  is a substitute for that pass.
- The resulting original-binary CPU profile completed the public root and all
  18 real case validations in 219.13 seconds. It measures 116.82 cumulative CPU
  seconds in public verification, 86.34 in replicated supplement processing,
  39.30 in fixture construction, and 14.31 in serial supplement preparation;
  these overlapping CPU totals must not be added as elapsed-time estimates.
  The failed race run was still preparing 1,897 supplement files (107,354,306
  bytes) seven seconds before its deadline. The repair uses the existing
  exact-key, detached-map role cache; runs the same 38 isolated mutation cases
  through four workers; and prepares/encodes supplement files with four workers
  calling the unchanged production single-file preparation path. Strict global
  path ordering, uniqueness, exact artifact/output counts, aggregate manifest
  validation, full signing and real public verification remain mandatory.
  Setup phase timings use `public replay setup`; only the distinct
  `public replay "..." completed` lines count the 18 public cases.
- The first performance-cut compile failed before any test executed because
  `supplementBytes` was redeclared with `:=` at observe_test.go:2005. The
  assignment and adjacent declarations were corrected, and the failed ordinary
  and race compile records were sealed separately. A subsequent adjacent
  review reproduced a real false-green in the worker helper: a callback using
  `runtime.Goexit()` left its default nil result and could exhaust the worker
  pool. The preserved pre-fix regression fails with
  `non-returning callback exited=true results=[<nil>]`. The corrected helper
  initializes an explicit error and joins one callback child per worker; a
  non-returning callback cannot kill the pool worker or silently pass. Panics
  are not recovered. Deterministic barriers and a synchronous spawn seam prove
  the exact four-worker bound, all-case completion, and ordered results.
- The canonical census now preserves the original 234 roots plus
  `TestFinalSemanticFixtureWorkersJoinAllCasesWithBoundedConcurrency` and
  `TestFinalSemanticFixtureWorkersRejectNonReturningCases`: 236 roots, SHA-256
  `4cea46078feda93564385b28b4ce888eaee1df76b71a8da24beebbb89af6cef5`.
  The sealed capture at
  `/home/by/urnetwork/temp/sn-postguard-public-capture-mh7nBu` compiled both
  ordinary and race binaries with matching pre/post source snapshots and exact
  236-root listings. Its manifest SHA-256 is
  `9039bc88200978111c1f9411147c0d848b762ab09c20334728545b90fcd9e4b3`.
  Three worker/cache roots passed ordinary in 0.59 seconds and race in 8.51
  seconds, without a race report. This does not yet qualify the complete
  performance repair.
- At 07:37 UTC the next ordinary pin check failed with
  `semantic scheduler lost its sole bounded worker loop: bounded=0 starts=1`.
  The static check inspected the wrapper, but the bounded loop now belongs to
  `runFinalSemanticTestCasesWithSpawn`; the wrapper delegates worker creation
  and the helper separately launches and joins one callback child. All five
  census checks passed, but this actual gate failure blocks the full rerun.
  Astra owns only the static-pin repair at this boundary, preserving separate
  worker-bound, delegate and callback-join assertions plus deterministic
  missing-bound/missing-join rejection controls. The other test files and
  census stay unchanged. Terra must recapture the corrected source, run the
  pin ordinary/race, then run the full exact 236 roots ordinary/race from the
  package directory with GOMAXPROCS=24, `-parallel=4`, `-count=1`, and unchanged
  15/25-minute deadlines. No redundant long three-root focus precedes that
  complete run. The following focused result supersedes only the pin blocker,
  not the incomplete full performance qualification.
- The repaired six-root pin/census selection passed ordinary in 0.64 seconds
  and race in 7.39 seconds with no race report. The new sealed capture is
  `/home/by/urnetwork/temp/sn-postpin-public-capture-EFj5jw`, with manifest
  SHA-256 `e90e36d85014c024949c2b480a70101b4e64cd5df6a05b7f46f3073870be9210`.
  Its ordinary/race binaries are respectively
  `720a73ef5589a7cbc320e417d658dfda288677e4a1ad5db60ca9450d650d3ecc`
  and `191e028a3511c136e1de8a1d8e5fb0de240b8922b0ac15d68a8f3cd905566ff2`;
  both enumerate the exact 236-root census and the source snapshots match.
  Terra now proceeds directly to full ordinary/race prequalification. The
  07:46 UTC fetch-only inventory separately confirmed all twelve repository
  HEADs equal their fetched upstreams, with exactly eight dirty SN paths and
  no other dirty repository. Its immutable record is
  `/home/by/urnetwork/temp/sn-pre-freeze-origin-inventory-20260905T074650Z-y5VJnh.log`,
  SHA-256 `24a2a2b4333094d21ed4ba802abfdc1e7a57ba8a1d1478eac3891e00839623eb`.
  A source checkpoint while these immutable binaries run is not a final freeze
  or permission to apply a stale lock.
- The eight-path checkpoint was committed, pulled and pushed as SN
  `5696537320ad7c16bd13210b93ed62ee56820349`. The sealed capture above retained
  the exact Go/test/census bytes; the subsequent handoff wording is not part
  of that captured source snapshot. Full ordinary qualification ended at
  07:51:10 UTC with exit 0, all 236 canonical roots and all 18 distinct public
  cases passed in 239.34 seconds, peak RSS 1,910,548 KiB. Root independently
  compared the passed root names to the canonical census.
- The same sealed race binary ran from 07:51:52 to 08:16:53 UTC. It exited 2
  at its internal 25-minute deadline: elapsed 1,501.09 seconds, peak RSS
  5,370,732 KiB, 235/236 roots passed, and 16/18 distinct public replay cases
  completed. The sole unfinished root was
  `TestPublicScenarioBundleRequiresReplicatedOwnerCompletionCommit`, running
  for 9m37s. There were no explicit assertion failures or race markers, but
  this terminal timeout is itself a blocking failure. Its public setup took
  6m22.104s before dispatch: signed artifact verification 160.686s, semantic
  file publication 87.807s, supplement file preparation 56.691s, and transcript
  sealing 49.465s. All 16 completed rejection cases returned within ten
  seconds; the two full semantic replay cases did not finish. The same run's
  fleet audit took 1,135.12s, BuildRender 727.19s and mutation matrix 268.57s.
  Preserve the full timeout stack before diagnosing work duplication,
  critical-path scheduling or publication cost; do not infer a race report,
  public-service outage, or successful remaining cases from this result.
- Both Terra test agents errored with their service usage limit while that
  race binary was still running. Root rechecked its PID, then independently
  observed terminal process disappearance and the complete exit footer. A
  cross-agent tool-session lookup returned "Unknown process id" before the
  host process finished; that observer error was not used to restart it.
  The raw race log is read-only (0444). No replacement test or live campaign
  was launched while execution capacity was unavailable. Both Terra agents
  subsequently recovered and ran the follow-up qualification recorded below;
  the separate evidence-scope decision remains unresolved.
- The timeout stack's active goroutine 4794 was decoding the 23.6 MB semantic
  JSON in `validateFinalSemanticOutputFiles`; the callback workers were
  correctly joined, not deadlocked. The public root began 15m23s into the
  package budget, leaving only 3m15s for full replay after its measured setup.
  Adjacent inspection found five independent strict fleet-projection mutations
  still serialized inside the 1,135.12-second fleet-audit root. The captured
  ordinary profile also attributes 10.08 of 10.20 cumulative CPU seconds in
  `loadFinalSemanticArtifactUses` to repeated SHA-256 work. Those are diagnostic
  CPU totals, not a measured prediction of race-time savings.
- The implemented follow-up preserves the positive fleet replay and runs all
  five detached, re-hashed, real-verifier projection mutations through the
  existing four-worker ordered join-all helper. The production artifact loader
  owns each distinct URI's bytes and memoizes its digest only within that
  invocation; every reference still checks its own size and hash, with the
  prior source-order error/cancellation behavior. The exact-byte deep-verifier
  cache key is unchanged. Deterministic counters require two loads/two hashes
  for references A,A,B,B, and controls cover changed same-URI claims,
  distinct-URI aliases, reused loader buffers and cancellation/error order.
  The deterministic RED and focused ordinary/race GREEN results for the
  committed repair are recorded below. Its complete 237-root qualification
  remains separate; formatting and static review are not test passes.
- The affected existing cache regression predates the hand-written semantic
  selector and was omitted from it. The selector, its exact constant, required
  name, independent source-group pin and canonical census now include
  `TestFinalSemanticArtifactVerificationCacheBindsExactBytesAndIsConcurrent`:
  exactly 237 roots, preserving all prior 236, with census SHA-256
  `fa80f354972c8a462004e09e1e942f6cca69cad2f3b0dc8a9b5c1c128b185fbc`.
  Nearby fleet-cache and historical loader/alias/tamper roots are already
  selected; unrelated role/campaign filesystem caches remain covered by the
  complete aggregate ordinary/race package gate.
- The repair's prequalification sequence and current boundary are:
  1. Completed: use an isolated temporary checkout under
     `/home/by/urnetwork/temp/` to reproduce the cache root's ordinary-only
     pre-fix RED. The inverse template at
     `/tmp/urnetwork-final-semantic-digest-prememoization.patch` restores only
     repeated hashing while retaining the per-call hash seam and regression.
     Template SHA-256 is
     `991b83f7e6d096e5d206e89dfbdf4be9cf32666dd612348cbaa6b1a059cfcedd`.
     It is explicitly non-executable documentation: extract the patch and
     replace only `__ISOLATED_RED_SNAPSHOT__` with the validated temporary
     checkout. Never apply the template verbatim or target the authoritative
     tree to manufacture the RED. Preserve both source identities. The
     repaired `final_semantic_evidence.go` SHA-256 is
     `d54e72b556b536a5770393fdad15d7cca99409e034669d3fec03190d465cd4a6`;
     the pre-memoization source must hash to
     `2d82bdbeede2d0b4e0f7ec96a9bb8677a75248e627241ee606861f0575816ced`.
     Expected deterministic failure: four digest calls, not two, for A,A,B,B.
  2. Completed: repaired cache, bounded-worker, historical-census and semantic
     pin/census checks passed normally and under race. The corrected quick
     race selection has 14 roots; preserve the canceled broader attempt too.
  3. Ordinary passed; race failed at the 25-minute deadline. The complete
     compiled 237-root census ran normally and under race from
     `/home/by/urnetwork/sn/sim-testnet`, GOMAXPROCS=24, `-parallel=4`,
     `-count=1`, with unchanged 15/25-minute deadlines. All 18 public-case
     completions remain required. The full selector includes the repaired fleet-projection
     root; do not insert another redundant long fleet/public-root run before
     it. A small focused pass cannot substitute for complete qualification.
     Diagnose the remaining root before a fresh repaired-source rerun; the
     parser additions must also be included in that candidate's census.
  4. Only after all of those pass and the evidence-scope question is resolved,
     continue the fresh clean-build lock/freeze/producer/aggregate workflow.
- SN `140b7ca3ffdb513ea489031e51b8f1b27e7b6e04` was committed, pulled and
  pushed at 08:31 UTC. The ordinary-only RED ran in
  `/home/by/urnetwork/temp/sn-red-digest-DRiUdv`, leaving the primary tree
  untouched. It exited 1 at 08:36:11 UTC with the exact counter mismatch
  `loads=2 hashes=4 owned=2`, where the regression requires `2/2/2`.
  The repaired quick ordinary selection passed 11 roots in 40.107 package
  seconds (76.41 elapsed). It inadvertently included the expensive historical
  upgrade-reader fixture; that successful result is retained. Before any test
  executed in the matching broad quick race process, root authorized stopping
  its verified process group to replace that redundant fixture with the four
  tiny historical-artifact-census controls. The canceled run exited 143 after
  signal 15; it is neither a pass nor a test failure. GNU time misleadingly
  printed `exit_status=0` for that signal termination: use the actual runner
  exit and signal marker, never that metric alone. The four historical census
  controls passed ordinary in 8.82 elapsed seconds, and the resulting exact
  14-root quick race selection passed in 15.745 package seconds (25.81 elapsed)
  at 08:44:16 UTC with no race report. The historical upgrade-reader remains
  mandatory in the full 237-root selector; no release coverage was removed.
- The two newly compiled binaries were correct, but local capture packaging
  initially failed independently of the simulator. The first sealed capture,
  `/home/by/urnetwork/temp/sn-postdigest-public-capture-9hTdWl`, concatenated
  `semantic_census_lines=237` with the selector because the count source lacked
  a final newline. Its `capture_ok=1` did not validate the malformed metadata.
  A first metadata-only repackage at
  `/home/by/urnetwork/temp/sn-postdigest-public-capture-parseable-HRqsoV`
  correctly recorded `capture_ok=0`: an inline Bash helper overwrote the
  caller's loop label, corrupting comparison paths. Both captures are preserved
  unchanged and are nonqualifying; neither defect justifies reusing an
  unverified manifest or rewriting sealed historical records.
- Astra replaced the inline wrapper with the persistent local helper
  `/home/by/urnetwork/temp/sn-capture-metadata.sh`. Scalar writers own their
  newlines, function variables are local, and a strict key schema rejects
  missing, duplicate, unknown, unterminated, concatenated or failed-status
  metadata. Terra's deterministic regression rejected the original manifest,
  accepted the corrected one, and passed the malformed-input and caller-label
  controls. Its sealed regression directory is
  `/home/by/urnetwork/temp/sn-capture-metadata-regression-UIuECf`, index SHA-256
  `6a32915a96144e18f64ff56802b9d96ac2d97082371dec5a4865356af8c69d07`.
  The successful repackage reused the exact compiled binaries without a
  recompile. Astra's final adjacent audit added explicit propagation of chmod
  and per-file hash errors; helper SHA-256 after that change was
  `c6720cc09b438a196d99aef13b93f63eda91e190e1d208f392c85bf82f292fee`.
  No such sealing error occurred in the accepted capture. Separate deterministic
  fault injection then found that both final index and manifest digest reports
  ignored a failing command substitution and could return success with an empty
  digest. Terra reproduced both failures before Astra added one shared explicit
  digest-status guard. All record/binary/index/directory chmod, mid-index hash,
  final index/manifest hash failures and the successful exact-index/mode control
  now pass in 0.41 seconds. This final helper SHA-256 is
  `d1f5c57fa502e66e463d0d8653a2708f6beb400b7a874a81ee1045a6b1af866f`.
  These tiny local-helper tests did not alter, repackage or restart the accepted
  capture or the running simulator qualification.
- A later request to rerun the original `regress` mode after these documentation
  edits used the wrong validation scope: that mode also revalidates the live
  checkout against the capture-time source snapshot. It correctly exited 1
  because only `FINALIZE-COMPLETE.md` and `FINALIZE.md` had changed, reporting
  current-status, unstaged-diff and source-hash mismatches. Its manifest schema,
  original binary/census checks and final sealing all passed. The printed
  `caller_*_expected_failure=FAIL` lines are intentional negative controls,
  not the cause. Preserve this freshness refusal; do not weaken the fence,
  reset documentation, recapture, or restart qualification to make it green.
  The clean-source schema PASS and final isolated sealing PASS above remain
  valid for their respective scopes. The owner-only raw record is
  `/home/by/urnetwork/temp/sn-capture-metadata-regress-final-h4ECOo.log`, SHA-256
  `58111dc936109f2aa0ebb142049c306df73b3a1dea0eeb9aaeba296b159208a4`;
  its detailed failed acceptance is preserved under
  `/home/by/urnetwork/temp/sn-capture-metadata-regression-QEyPaC`.
- The accepted immutable capture is
  `/home/by/urnetwork/temp/sn-postdigest-public-capture-verified-cZwYhy`:
  manifest SHA-256
  `add00e89d6029ec65faf734b18d94581d192c5b9e87241e390508ed071c1bc7a`,
  complete relative file-index SHA-256
  `d676aa98478de055e6da45c1c8c8480d1b7c40c051f7b19c709ec6636f207142`.
  The ordinary/race binary SHA-256 values are
  `755121013f358976f599ab0a5eb0800519f30fd8ca08ef59aaedb55782223bb6`
  and `ddef0a881c6122ec7f7d895d7c1234b149885d64d913ffe67d5441ce27d1a429`.
  Each enumerates the same exact 237-root census, SHA-256
  `fa80f354972c8a462004e09e1e942f6cca69cad2f3b0dc8a9b5c1c128b185fbc`.
  Source snapshots matched clean SN `140b7ca` and server `b12af6b3` before and
  after capture. Terra independently checked the complete index against all
  actual files (no missing or extra file), directory/binaries mode 0555 and
  all other records mode 0444. Full ordinary ran from 08:59:39 to 09:04:06 UTC
  and exited 0: 237/237 exact canonical roots and 18/18 unique public cases
  passed, elapsed 266.65 seconds, peak RSS 2,333,244 KiB, no failure or race
  marker. Its sealed raw record is listed below. Full race ran from
  09:04:58 to 09:30:00 UTC, former PID 3994661, raw
  `/tmp/urnetwork-sn-postdigest-semantic-full237-race-zugmmG.log`.
  The internal timer fired, exit 2, elapsed 1,500.59 seconds, peak RSS
  6,849,084 KiB; the outer timeout did not fire. Exactly 236/237 roots and all
  18 public cases passed. The sole unfinished root was
  `TestFinalSemanticEvidenceBuildRenderAndArtifacts`, running for 10m14s.
  Its sampled stack was actively hashing chain-replay exchanges, not waiting
  on a dead worker. The test itself performs ten serial complete on-chain
  verifier calls; it was in the eighth at timeout. A sampled stack does not
  establish the dominant cost, so Astra's performance lane awaits Terra's
  exact captured-binary CPU profile before choosing a repair. Primary edits
  during the failed run were only the recorded handoff documents, not Go,
  tests, selector or census bytes. All terminal records are now sealed.
- The post-seal fetch-only inventory at 08:57:20 UTC confirms all twelve
  repositories were clean and exactly equal to their freshly fetched
  upstreams, with no pull, merge or checkout. Its immutable log is
  `/home/by/urnetwork/temp/sn-postseal-origin-inventory-20260905T085720Z-wOVr0I.log`,
  SHA-256 `ddb9dd16cf0ada99a8772ad5733c5c6d9591145113fc2489b30b35079a7df8d2`.
  This inventory is not the later source/lock fence or permission to apply.
- A clean trimpath/buildvcs executable of `140b7ca`, with
  `vcs.modified=false`, is preserved at
  `/home/by/urnetwork/temp/sn-release-lock-checkpoint-F6uq5F/sim-testnet`,
  SHA-256 `10765a6f957e38fbf9d0707870997b5d56c4cc6de0653d226c88e10f7a2955e5`.
  Its unapplied `candidate.lock.yml` has SHA-256
  `891bc31f7f641d3d10b5394dec1a643af5c5cfb4fb8314c6ea7a97bc7a02ce0f`.
  Only the expected SN, server, Connect, SDK and protocol source hashes differ
  from the tracked lock; runtime/EVM artifacts are unchanged. Unlike the older
  `0ab39293...` preview, this preview includes the production digest change.
  It is not applied. A later documentation-only commit still requires a fresh
  clean executable of the exact pushed HEAD before eventual lock application.
- The clean `140b7ca` non-applying doctor, generated at 08:37:23 UTC,
  again passed 61/64 checks. The sole hard failure was the expected stale,
  unapplied source lock (server source mismatch in this run); the two soft
  failures identify shared provider and physical Subtensor-peer independence.
  Every independent host, MinIO, RPC/runtime, wallet/budget and attempt-state
  check passed. The owner-only report is
  `/home/by/urnetwork/temp/sn-checkpoint-doctor-140b7ca-q4X1hl/doctor-report.json`,
  SHA-256 `dfba859be85a25c9a928fd4e5f77b105d86581160b28f3a85410760257377cf8`.
  It is not a Ready verdict; repeat doctor and both approval-identical plans
  after the final gates. No new shared-service outage is evidenced by this run.
- To retain closed local evidence beyond temporary-directory cleanup, root
  copied and independently byte/hash-verified 49 exact `/tmp` records into
  `/home/by/urnetwork/temp/sn-qualification-records-zq3xbq` at 08:53:05 UTC.
  A source `/tmp/name` maps to `tmp/name` inside this owner-only sealed archive.
  Its `SHA256SUMS` SHA-256 is
  `a18b922911b6a012deeb934ae3457688df796f2bb18c66cd610a6541eefb999a`;
  `sources.txt` SHA-256 is
  `2a565dbe6e2af7764b7f66974f2a6d6dbde8bdfa8cda5cb57d8dd882c6d68c10`.
  This archive preserves original logs, not a public publication or test pass.
  Later metadata/full-237 records are not in that earlier snapshot; retain
  them separately without editing the sealed archive or original failures.
- The nine subsequently closed `/tmp` records (three metadata records and
  six complete-ordinary result/index files) were separately byte/hash-verified
  and copied by basename into
  `/home/by/urnetwork/temp/sn-qualified-ordinary-records-09Opz2`.
  Its `SHA256SUMS` SHA-256 is
  `3a28a8b7f56a153e93e01104905394052b7a18e216c7016a380b9a72e4389b45`.
  The directory is owner-only mode 0500; all records/index are 0444. The race
  log was still running when that archive closed and is not part of it.
- The eleven closed full-237 race records are independently hash-verified and
  copied by basename into
  `/home/by/urnetwork/temp/sn-full237-race-records-HAU7zp`, directory 0500.
  Its `SHA256SUMS` SHA-256 is
  `a599c4513bdc167e5b1389d5c1714c57d5621a16551d30c709b5393d78f47d5b`.
  The first missing-root derivative used the wrong diff direction and reported
  zero missing roots. It is preserved alongside the correction; the raw log
  and root list never changed. Use `summary-corrected.txt` (SHA-256
  `622a4811a3c33de23b4744bf7ba16ef0d8fb21763e82aa8e2265947e895b2019`)
  and `missing-expected-roots.txt`: exactly one unfinished expected root.
- The exact final local helper and its sealing RED/GREEN, expected indexes
  and post-capture source-freshness refusal are separately archived at
  `/home/by/urnetwork/temp/sn-capture-helper-evidence-20260905T090825Z`.
  `ARCHIVE-MANIFEST.txt` SHA-256 is
  `24deb699b0c2c1a78da568e484872b5e818af6fa637d35d1d201ed297f0329b2`;
  the archived helper SHA-256 remains
  `d1f5c57fa502e66e463d0d8653a2708f6beb400b7a874a81ee1045a6b1af866f`.
- The documentation-only handoff checkpoint was committed, pulled and pushed
  as `9fa5109119b97e77a7e514d58e3e9bca3dbfa1ca` at 09:08 UTC. It changes only
  FINALIZE-COMPLETE.md and FINALIZE.md relative to runtime candidate `140b7ca`;
  the sealed qualification binaries and their Go/test/census inputs remain
  unchanged. This is not the final runtime freeze or a new release-lock apply.
- The other Terra lane found no saved exact-current-source certificates for
  Connect's three unsharded aggregate selections. The clean source is still
  `1b81da6668e6a3ec9536ac61a07b27a619738cc7`; inventory at
  `/home/by/urnetwork/temp/connect-aggregate-cert-VmGolf/inventory.log` has
  SHA-256 `5295555f2c05ff5f53a3c31c536eda3ac4111e51623aa2a675f79cd196b11b22`.
  Ordinary `WARP_TEST_ENV_FAIL_FAST=1 go test -count=1 -timeout=30m .` began
  around 09:10 UTC, parent PID 4003586, child 4003727, log
  `/home/by/urnetwork/temp/connect-aggregate-cert-VmGolf/01-ordinary.log`.
  Ordinary passed at 09:18:55 UTC in 530.545 package seconds, exit 0. Its
  sealed mode-0400 raw log has SHA-256
  `ca268cada530f96cc349a9eede20f55b1225d31a53279e7b8ba36db0ee064c2e`.
  Default race passed at 09:38:50 UTC, 1,169.652 package seconds, exit 0;
  `/home/by/urnetwork/temp/connect-aggregate-cert-VmGolf/02-race-default.log`
  is sealed mode 0400, SHA-256
  `9fc4fb1e5faa1abb5fb6d997b557ae3e9f74eafff5da0c506cc3c3bc45d6dfe5`.
  Fixed-order `-shuffle=4535211000` race is now running as PID 4055806, raw
  `/home/by/urnetwork/temp/connect-aggregate-cert-VmGolf/03-race-shuffle-4535211000.log`.
  Both race modes use GOMAXPROCS=4 and 30-minute deadlines. These are parallel prequalification,
  not a frozen aggregate pass; preserve every actual terminal result.
- The parser reproduction and repair are preserved under
  `/home/by/urnetwork/temp/sn-proof-suffix-repro-jK6XWR`. A deterministic
  signed-proof RED accepted four malformed suffixes after real size/hash and
  signature validation. The corrected combined RED also confirmed both
  incomplete mapping/sequence suffixes in all five YAML loaders. Preserve the
  intervening compile failure (missing `io` import), rather than calling it a
  proof test result. `protocol.ParseFleetManifest` was already protected by
  whole-input canonical equality; its regression now covers the same suffixes
  without changing production behavior. The six EOF guards preserve valid
  signatures, whitespace/comments/document ends, legacy-policy extraction,
  normalization, unknown-field and complete second-document rejection.
  The stable pre-integration 12-file patch has SHA-256
  `6a0ed0fb13a49e7328a7020075c253dd4767bdcc3b973c3682728e22c22902ac`.
  Terra's identical 12-root/four-package ordinary and race cuts passed with
  GOMAXPROCS=4, offline modules, per-package two-minute/outer five-minute bounds;
  race ended 09:38:43 UTC. Their raw records are below. Primary integration is
  now complete in sixteen code/test/gate files; the twelve parser files match
  their isolated qualified hashes. Both gates explicitly run the thirteen-root
  framing selector (twelve parser controls plus its existing gate reachability
  root) ordinarily and under race. The semantic census is exactly 238,
  SHA-256 `abb8ef76b5c091d55513bd5a4174a79badf57e4a48b7a231a0b0b5cec51dc36a`,
  retaining all previous 237 roots plus the new signed-proof framing root.
  An independent source-group pin prevents that proof file from escaping
  selection. Primary framing and semantic-pin qualification now passes all
  nineteen roots ordinarily and under race: thirteen framing roots plus the
  six exact semantic selector/census controls. Actual exits were 0; race ended
  09:49:50 UTC and fresh ordinary ended 09:51:25 UTC. The relevant sixteen-file
  Go/test/script/census manifest stayed identical before and after, SHA-256
  `bce21a06499ec75761bfd2a4f49d1c67ee6b4c02df4640659300f4cc88c2e006`.
  `bash -n` also passed for both gates. The primary source hold is released;
  performance edits still remain isolated. This parser repair and the handoff
  are ready for their next checkpoint, not final source freeze or full gates.
  Authenticate the actual pushed HEAD rather than applying either old binary.
- Primary integration evidence is sealed under
  `/home/by/urnetwork/temp/sn-parser-integration-RuCjIW`.
  Ordinary `parser-gate-ordinary-rerun.raw.log` has SHA-256
  `961d4ca2f927a55fd8b8a6b9bff3b09e4ed0f2f4702c406b65b93d357d7abe32`;
  its actual-exit terminal record has SHA-256
  `16fff664687dca05ce84e13c2191f9a9b2ea326151b7331ebcb815d28060df86`.
  Race `parser-gate-race.raw.log` has SHA-256
  `aa13c92d426d1c6a0542063e24742ec9337b4785ff86929651d375553f042e5c`;
  its actual-exit terminal record has SHA-256
  `db18e86f4071b0633eedb006cd9c95f217a40a1ae309d0137989b1068736c0b4`.
  The first ordinary invocation printed nineteen passing roots but its runner
  discarded the structured session/exit result. Preserve that raw record
  (`parser-gate-ordinary.raw.log`, SHA-256
  `1adb0a808d61de59136883fbd4087cf6357fa8cd9257b7c0c77a5802cb627b0d`)
  as `TEST_OUTPUT_PASS_EXIT_UNVERIFIED`, not a qualifying result. The repaired
  wrapper preserves actual completion independently of stdout; a synthetic
  command printing PASS and exiting 17 recorded both actual exit codes as 17
  before the authorized fresh ordinary rerun. Synthetic terminal SHA-256 is
  `62a0f78a55d491cac900480b30fe7746a6dab34e6e6ce9cf0098e9b638185aee`.
  All four owned test/control PIDs are terminal. No live process was restarted.
- Terra profiled only `TestFinalSemanticEvidenceBuildRenderAndArtifacts`
  in the unchanged captured ordinary binary, in the package working directory,
  GOMAXPROCS=24, parallel 4, count 1, inner 15/outer 16-minute bounds.
  It passed from 09:39:41 to 09:41:44 UTC, elapsed 123.30 seconds, exit 0.
  Raw log and CPU/memory profiles are sealed under
  `/home/by/urnetwork/temp/sn-cZwYhy-evidence-profile-86FD8P`;
  `PROFILE-MANIFEST.txt` SHA-256 is
  `a2d9d623a8aa9c215fbebddab250f081b73320c952f26eef4162ca3d4e1b5027`.
  Raw SHA-256 is
  `fb16a8dacbb3d15fc5ebee2689103b13caa437cab8d97b1473a403d8677643fa`,
  CPU profile SHA-256
  `91cc810dd65c23957d8eead4c67a8226c35d10c432ec555e32f2e1ab13b0bd8e`,
  memory profile SHA-256
  `4dccd7dfab33b426bbf02085147833578a943616c4d51277a37cd59de75c028e`.
  This is a diagnostic, not a release pass. The profile confirms repeated
  SS58 decoding in the synthetic reader (about 14% of sampled CPU) and
  repeated generation-list construction (1,874 MiB of flat allocations).
  `finalRPCExchangeHashes` is only 2.67% of sampled CPU, so the timeout stack
  alone would have misidentified the dominant work. The same profile exposes
  repeated whole-plan copies in the production `recordPlan`/`record` path
  (15,670 MiB cumulative allocations, not resident memory). Astra is preparing
  exact-byte ownership/work-count regressions and repairs for these related
  paths. Cumulative CPU/allocation totals overlap and are not additive elapsed
  time or proof of future wall-clock savings.
- The CPU's portable SHA-256 path is expected, not an identified service or
  crypto configuration defect: the host reports Intel Xeon E5-2697 v2 with
  24 logical cores, AVX but no AVX2/BMI2/SHA flags. The installed Go 1.26.6
  `crypto/internal/fips140/sha256/sha256block_amd64.go` selects accelerated
  blocks only with the corresponding runtime CPU capabilities. Read-only Go
  settings show amd64/v1, no `purego` flag or experiment, and GOFIPS140=off.
  Do not force unsupported instructions, weaken verification or change crypto
  libraries to hide the cost. The profiled duplicate-work repair and bounded
  independent-case scheduling retain portability on this host.
- Real pre-fix performance regressions are preserved under
  `/home/by/urnetwork/temp/sn-chain-replay-perf-1gOqvC`.
  `hotkey-decode-red-ordinary.log` (SHA-256
  `c1d79b9e108865e4b8a7f73eaf5bd1170321e809e82e7560450b5f79628c9766`)
  fails because each of the three fixture surfaces decodes the same address
  eight times instead of once; changed-byte/state/ownership controls pass.
  `fleet-generation-copy-red-ordinary.log` (SHA-256
  `8836e3d1c3e45c096c1cf4bd39eb9ecfdc35209188df0f1ac07187d7b549982d`)
  also reproduces concurrent duplicate decoding, four generation-lookup
  allocations where only three owned transcript buffers are required, and two
  indexed-plan copies where no allocation is needed. In-place/replacement,
  disappearance, path and caller-ownership controls pass. Two provenance
  fields in that invocation used shortened filenames and were blank; retain
  its sealed correction, `fleet-generation-copy-red-provenance-correction.log`
  (SHA-256 `c2e5932987753ebe5f7a43bbf7bdc6379607c5c97ab6133d63989b57b16d3968`),
  recording the exact unchanged source hashes. These are diagnostic REDs,
  not release qualification. Astra's isolated repair and Terra GREEN follow-up
  remain pending. All ten success/negative chain cases must remain when moved
  through the existing four-worker join-all helper, with private readers and
  unchanged rejection reasons.

Current repair's closed raw records (all mode 0444):

| Record | Path | SHA-256 |
|---|---|---|
| Isolated pre-memoization RED, expected exit 1 | `/tmp/urnetwork-sn-isolated-digest-red-93DdxQ.log` | `a6dbe9fb39b38dd4e32597d0dba7c24b3bd14d4e0b275244e858546bed8ac63b` |
| Quick ordinary, 11 roots pass | `/tmp/urnetwork-sn-postdigest-quick-ordinary-8cUlya.log` | `c54665c9ceec10d2ee5cfcf65a95479aaaddbb5ba39f056bedb91926b340cdf5` |
| Broad quick race canceled before tests, exit 143 | `/tmp/urnetwork-sn-postdigest-quick-race-Ly0SoU.log` | `0d153e9f1ca99d285c72bf82113358eba792101cdf08e72c45a6fd731c32491e` |
| Explicit cancellation record | `/tmp/urnetwork-sn-postdigest-quick-race-canceled-Dj94Nt.txt` | `9003332b6064904a1b4419f4e4785b7fbd919ff3df4b7f11e214f557c46421ca` |
| Historical census ordinary, four roots pass | `/tmp/urnetwork-sn-postdigest-historical-census-ordinary-SzH5NI.log` | `abc7156d4572897730980e1121e5bd8415f533af97666efc24a0f5fb47ca9025` |
| Adjusted quick race, 14 roots pass | `/tmp/urnetwork-sn-postdigest-quick-adjusted-race-wBXxdJ.log` | `ce417c036a05a8255aaf6644845b63a430bdb0874de681997b60af164785bb4f` |
| Metadata schema/scoping regressions pass | `/tmp/urnetwork-sn-capture-metadata-regress-DOnAHH.log` | `2fd2e16b5501780453bd2f435d905003947ba4fef9b6658e3b1ce4162d772306` |
| Corrected metadata-only repackage, exit 0 | `/tmp/urnetwork-sn-capture-metadata-repackage-NlUvck.log` | `4105b088b4b12441f61b8ebf2ac37837e420b9235c910664da77f7f84b55dd1a` |
| Independent complete index/mode audit, exit 0 | `/tmp/urnetwork-sn-capture-metadata-final-audit-sepfeP.log` | `75a0ed15f34e069dcb830a24de3aabd97ab0a1be60b44110e8b881abadbebd8a` |
| Complete 237-root ordinary, all 18 public cases pass | `/tmp/urnetwork-sn-postdigest-semantic-full237-ordinary-BBWB0k.log` | `2710e4745aeae16716f9c67c216ee4c6a0436171c658220dc9977fa28ee10232` |
| Complete 237-root race, internal timeout after 236 roots/all 18 public cases | `/tmp/urnetwork-sn-postdigest-semantic-full237-race-zugmmG.log` | `c889980a679081eea74659db95830ab4b26a417d49aea97f37c4fc8700845600` |

Parser records are owner-only, mode 0400, under
`/home/by/urnetwork/temp/sn-proof-suffix-repro-jK6XWR`:

| Record | Basename | SHA-256 |
|---|---|---|
| Signed-proof malformed-suffix pre-fix RED | `proof-suffix-red-ordinary.log` | `94449ca93e65497b8edaa82b7dd0e15a1a622a0404cb4c5bef1ce79a5e0710eb` |
| First combined cut, partial compile failure, not a proof pass | `proof-yaml-combined-red-ordinary.log` | `f41ed3161d5baa6e7fbfc425f53c9e647945e4f716d9894637e44024ad307151` |
| Corrected combined cut: proof GREEN, five YAML loaders RED | `proof-yaml-combined-red-corrected-ordinary.log` | `31aaf0560b73f81f98c8067cfc27d1897557297d70b50b2948f68a02645f67c6` |
| Six-guard fixed ordinary, all 12 roots pass | `proof-yaml-fixed-green-ordinary.log` | `a00bc85e87c4848e64c9facd89ac38cdf2874bac321231039738de03606dec00` |
| Six-guard fixed race, all 12 roots pass | `proof-yaml-fixed-green-race.log` | `02170a5f5305df08faf1a51e41720d25caed124b3200b59ff1442f9843ff4c01` |

The helper's owner-only sealing regression records are
`/home/by/urnetwork/temp/sn-capture-metadata-seal-regress-red-0PPNbo.log`
(expected RED exit 1, SHA-256
`0c7b0815741b027a9ed7c5bb100803abe36577d3cb7510bb717f020b02490398`)
and `/home/by/urnetwork/temp/sn-capture-metadata-seal-regress-green-xMuhC6.log`
(GREEN exit 0, SHA-256
`c412b27b9e57893b2275b77d2920099d828a7167905fbb0ce61da6792097972d`).

- Historical pre-repair clean executable/doctor observations follow. They do
  not represent the digest repair and must not be applied or called current.
- A clean trimpath/buildvcs executable of checkpoint `5696537`, with
  `vcs.modified=false`, is preserved at
  `/home/by/urnetwork/temp/sn-release-lock-checkpoint-ejN9owMT/sim-testnet`,
  SHA-256 `e8c110f41cf489409afb2592f8738593b76453ddd996945adfcc5ed8a34ca962`.
  It rendered, but did not apply, `candidate.lock.yml` in the same directory,
  SHA-256 `0ab39293d8b7136c4d08b9d9b5a45c1393692e5bc8b6e2ffa6b271bae1a9a2a8`.
  Review found only the expected SN, server, Connect, SDK and protocol source
  hash changes, with runtime/EVM artifacts unchanged. These candidate bytes
  match the older preview because the intervening changes are test/docs-only;
  this does not permit using an older or dirty executable for lock apply.
  Any later commit, including this handoff update, requires a fresh clean
  executable of the exact pushed HEAD before apply.
- The same clean checkpoint's non-applying doctor, generated at 07:54:59 UTC,
  reported 61 passed checks of 64. Its sole hard failure is the expected
  unapplied release lock (protocol source hash mismatch). The two soft failures
  explicitly report a shared operational/postcondition RPC provider and the
  same physical Subtensor peer. All other hard infrastructure, wallet, budget,
  chain/runtime and attempt-lineage checks passed. The owner-only report is
  `/home/by/urnetwork/temp/sn-checkpoint-doctor-EWS0md/doctor-report.json`,
  SHA-256 `580401b4e900e199dc0b1541560f67304a78b554b62023ffa937e3121f4a43d2`.
  This diagnostic is not a Ready verdict or a replacement for the fresh
  post-gate doctor and approval-identical plans.
- The clean `d450fe5` pre-lock source check passed all twelve freshly fetched
  repositories, all fifteen live module tidy checks and the authenticated
  archive exception in 18.06 seconds. Its immutable record is
  `/tmp/urnetwork-sn-prelock-source-freeze-6GMclQ.log`, SHA-256
  `2ad158581b9700590463bd6fa4bd5a7815cae24ae4fa789179a4045179a654d8`.
  The clean trimpath binary at
  `/home/by/urnetwork/temp/sn-release-lock-build-n3lQUa/sim-testnet` (SHA-256
  `0954ed233b1a8c030d8f2e2c79aa00d310a95ffe360c1472ef79527de058885c`)
  rendered an unapplied candidate lock, SHA-256
  `0ab39293d8b7136c4d08b9d9b5a45c1393692e5bc8b6e2ffa6b271bae1a9a2a8`.
  Only the expected SN, server, Connect, SDK and protocol source hashes changed;
  runtime and EVM artifacts did not. This remains a preview, not approval.
  After the performance repair, build from its new clean pushed revision and
  repeat observation; do not apply using this now-obsolete executable.
- Prequalification uses two Terra-max test lanes: one owns all PostgreSQL/Redis
  suites and source-pin checks, while the other owns complete semantic replay
  timing. Astra-max owns reproduced-failure repairs. An independent read-only
  test lane does not stop merely because another local selection fails, but
  every failure still blocks freeze/launch. Never duplicate or restart a live
  test because an observation expired. The first captured-binary pin invocation
  also used the wrong working directory; the Go-test-equivalent directory is
  `/home/by/urnetwork/sn/sim-testnet`, not the SN repository root. Retain that
  failed invocation separately from the subsequent real selector failure.

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
| Exact provider query plans after repair, pass | `/tmp/urnetwork-server-provider-query-plan-postfix-l8LCtc.log` | `53f765df328571271f7fcd2d124262fd849fe4cc0662a743b3c275e5f61cf9d5` |
| Full 15-test provider ordinary after repair, pass | `/tmp/urnetwork-server-provider-stats-full15-ordinary-postfix-odblnL.log` | `d88d71b16edadbe249dbc2f02134b581220ffa2fb23e382b1169eec5f2429c26` |
| Full 15-test provider race after repair, pass | `/tmp/urnetwork-server-provider-stats-full15-race-postfix-Rp8aYv.log` | `a0e8a859e29e1c686fa1c97847e6ce393f3f9c4f9420545e51c717f4cd5f8a05` |
| Provider rollup/retention ordinary, pass | `/tmp/urnetwork-server-provider-rollup-retention-ordinary-final-RGK0Bg.log` | `5c8af8575b668a85a48adafd05de666f9ef0f146081efa7728510ec5392cf67d` |
| Provider rollup/retention race, pass | `/tmp/urnetwork-server-provider-rollup-retention-race-final-vzMej3.log` | `25ad41f82001eb307cfc29c52776208d020c09a204709b83656d97b943d26ccd` |
| Captured pin invocation with wrong working directory | `/tmp/urnetwork-sn-immutable-scheduling-pins-ordinary-1xnfFp.log` | `9aaa523bee6274906fbefc7bde932466ce64a468922beb3ff72b69bab6b12f7d` |
| Provider whole-file selector omission, pre-fix failure | `/tmp/urnetwork-sn-immutable-scheduling-pins-ordinary-packagecwd-83XmNt.log` | `088fd19f291f1b6531c37e01164c442b7a5e556af25a06ad1f7a8aa920ad4bd1` |
| Corrected provider gate pin ordinary, pass | `/tmp/urnetwork-sn-release-gate-provider-pin-ordinary-y721kb.log` | `58f1d0dc4525c11aec0eb8260b94574a812f8a825ad93167350cfbcdb8cb72af` |
| Corrected provider gate pin race, pass | `/tmp/urnetwork-sn-release-gate-provider-pin-race-xjYuke.log` | `73abe92fed4a399c5dd23922cdb254156daf64dc2cd97c549be8764c897773c1` |
| Parallel public replay with uninjected negative fixture, failure | `/tmp/urnetwork-sn-immutable-edge-public-ordinary-5T6Dhr.log` | `810efcd849c118c81f64554a0ffecbbdf717437ba8652f4dc7f1d146e35e9211` |
| Complete 234-root ordinary before fixture repair, same sole failure | `/tmp/urnetwork-sn-immutable-semantic-full-ordinary-Vr3gKp.log` | `e27f2d3d7703e038d5d4aaac1db3afe559e5f5b5e0b21ccbb0342d147dbddb38` |
| Complete corrected 234-root ordinary, pass | `/tmp/urnetwork-sn-postrepair-semantic-full-ordinary-FzHJmq.log` | `b200c2c3c51dc3c919d90408c293aae98b8f5b98e3ba33448c5b445469474734` |
| Complete corrected 234-root race, internal 25-minute timeout | `/tmp/urnetwork-sn-postrepair-semantic-full-race-TpqJCU.log` | `92e8a2381f7d9e5424e22db3b68cb4010f177a3f5f2e1c82c4ea0fd536e5d295` |
| Race timeout and root-count extract | `/tmp/urnetwork-sn-postrepair-semantic-full-race-timeout-extract-DiCEwc.log` | `0b8dd76b5d729031f7bc7a67f63a21294ae41d1879378f6deffb354aba9d87e9` |
| Original public root ordinary CPU-profile run, diagnostic pass | `/tmp/urnetwork-sn-postrepair-public-root-ordinary-profile-gNsNae.log` | `943ce6f9e37edae01e49e7421492cf413b261ffd24702938db3585687d7194c9` |
| Original public root CPU profile | `/tmp/urnetwork-sn-postrepair-public-root-ordinary-cpu-nQroOu.pprof` | `ec4203e2fb818c690545845e161e53d7895cd41a2dbd2882097eecf95cf35208` |
| First performance cut ordinary compile failure, no tests ran | `/home/by/urnetwork/temp/sn-postperf-public-capture-dyqet2/ordinary-compile.log` | `309f2a784d7029cc0fdd70e70468557e7bca027a524181dc8e3eb4c2a7865a7a` |
| First performance cut race compile failure, no tests ran | `/home/by/urnetwork/temp/sn-postperf-public-capture-dyqet2/race-compile.log` | `c8aae1e39170945d9bbb432a9c012628bdcf7d445455dae6ed16dda4f351e463` |
| Non-returning callback pre-fix false-green regression, red | `/tmp/urnetwork-sn-pre-guard-nonreturn-red-PdqiEn.log` | `bd5d815e2d947ed4c6e6dff4227f9295c71ad8466dae21a90d66f9d0c0200a17` |
| Guarded worker/cache ordinary, three roots pass | `/tmp/urnetwork-sn-postguard-helper-cache-ordinary-IXcQM8.log` | `6225f3816655c20b3cb81dd0278cbae0eaf16a71bff9921c4df4fbf7825b8f41` |
| Guarded worker/cache race, three roots pass | `/tmp/urnetwork-sn-postguard-helper-cache-race-tbFgCH.log` | `7884e514d03a519b8e48996aa2f3143f8ce84cf134ca2688e6b044c1e87b0193` |
| Post-guard static pin failure; five census roots pass | `/tmp/urnetwork-sn-postguard-semantic-pin-ordinary-g1MC2l.log` | `27f760ed773b89e08581a05534ec795bcb32442711e33e2f48869b3a0316cd6b` |
| Corrected static pin/census ordinary, six roots pass | `/tmp/urnetwork-sn-postpin-semantic-pin-ordinary-ZragfL.log` | `0264aea70f78e8270813c9a1cec39404e839352ba11d7d5f2c44fb73c6119630` |
| Corrected static pin/census race, six roots pass | `/tmp/urnetwork-sn-postpin-semantic-pin-race-JLqeE8.log` | `d3b700575c3ab7214e69729de7cd4c9027c5fe562c4ab9bdb0a2b21d2f55b6e9` |
| Complete current 236-root ordinary, all 18 public cases pass | `/tmp/urnetwork-sn-postpin-semantic-full-ordinary-iBvYY2.log` | `1d5ad0a88ea8df2ccdc148e5308149a5d9db3ce70bb0fe0513fa4b2c3c49f40a` |
| Complete current 236-root race, internal 25-minute timeout | `/tmp/urnetwork-sn-postpin-semantic-full-race-AFWhVW.log` | `6b312bc45137f1daf5af576f85d1bc4ace67e6de02adb04e08357d9bb50aac08` |
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
| Canonical semantic/replay selection, ordinary | current 237-root prequalification PASS, including all 18 public cases | ended 09:04:06 UTC, elapsed 266.65s; raw SHA-256 `2710e4745aeae16716f9c67c216ee4c6a0436171c658220dc9977fa28ee10232` |
| Canonical semantic/replay selection, race | 237-root run FAILED at the internal 25-minute deadline; 236/237 roots and all 18 public cases passed | ended 09:30:00 UTC, exit 2, raw SHA-256 `c889980a679081eea74659db95830ab4b26a417d49aea97f37c4fc8700845600`; remaining BuildRenderAndArtifacts root under diagnosis; full repaired candidate pending |
| JSON proof/five YAML EOF guards | deterministic RED, isolated 12-root and integrated 19-root ordinary/race GREEN | primary race ended 09:49:50 UTC, verified fresh ordinary 09:51:25 UTC; exact census 238 and explicit framing gates retained; full candidate gate remains separate |
| Bounded worker, non-return and detached-cache regressions | focused ordinary/race pass | exact three roots, 0.59/8.51s; immutable records above |
| Post-guard semantic static pin | repaired, focused ordinary/race pass | six roots in 0.64/7.39s; prior 07:37 UTC failure and exact corrective results preserved above |
| Full sim-testnet ordinary | pending on current candidate | prior aggregate was killed only by the corrected implicit 10-minute package deadline |
| Full sim-testnet race | pending | |
| Producer gate | prior candidate passed; current rerun pending | `5d779cd`; 2026-09-04 UTC |
| Aggregate gate with DB tests | current rerun pending | `5257b2f` attempt reached ordinary simulator tests and timed out at exactly 600.146s with no assertion failure |
| Foundry | pre-freeze pass; frozen aggregate rerun pending | Foundry 1.7.1; format/build clean; 156/0/0; 4,608 invariant calls at 2026-09-04 22:52 UTC |
| Slither | pre-freeze pass; frozen rerun pending | Slither 0.11.6 analyzed all four deployable roots with 0 high/medium findings at 2026-09-04 22:54 UTC |
| Exact v451/v452/v453/v454 metadata artifacts | prequalification pass; frozen gate pending | public-chain exact-Wasm and decoded-metadata gate passed all four versions at 2026-09-04 22:18 UTC and again at 22:52 UTC; v454 static source and all selected exact-upstream Subtensor qualifications pass (the upstream test suite is Rust; UR remains Go/Solidity) |
| Server release-selected DB/proxy qualification | pass; frozen gate pending | `d184121d6b33ecf0253be92167f74e672ff7229f`; affected normal/race/vet, managed controller 108.45/164.81s, and proxy lifecycle 19.97/43.19s |
| Server unselected full model/repository suites | pending if required by final gate/diagnosis | no broad pass inferred from focused selection |
| SN runtime candidate | digest/fleet repair committed; complete ordinary PASS, race FAILED; parser integration and remaining replay performance repair in progress | pushed runtime `140b7ca3ffdb513ea489031e51b8f1b27e7b6e04`, documentation checkpoint `9fa5109`; captured census 237; all subsequent source must qualify separately |
| Server candidate commit | affected allocation, reporting/query-plan, retention and strict-test ordinary/race pass; frozen gate pending | clean/pushed `b12af6b3aa18adb7b4e84251b2b8ab15c35f7ddc` |
| Connect candidate commit | affected focused suites and complete default-order ordinary/race pass; fixed-shuffle race in progress | clean/pushed `1b81da6668e6a3ec9536ac61a07b27a619738cc7`; ordinary 530.545s, default race 1,169.652s (ended 09:38:50 UTC); fixed-shuffle PID 4055806, frozen aggregate pending |
| SDK candidate commit | affected token/points ordinary/race and root build/vet pass; frozen aggregate pending | clean/pushed `e1d8dc8d9682daefd86878fea911b7b643634406` |
| Other candidate repository commits | all twelve clean/equal at the post-seal fetch; frozen gates pending | 08:57:20 UTC inventory SHA-256 `ddb9dd16cf0ada99a8772ad5733c5c6d9591145113fc2489b30b35079a7df8d2`; this handoff edit is later; fresh lock/source fence still required |
| Release-lock hash | source-stale tracked lock; current clean-build preview unapplied | tracked `sha256:009566be02a32f77b5a5708432eee71c694668af7c5bded90e4b373c38f143db`; clean `140b7ca` preview `891bc31f7f641d3d10b5394dec1a643af5c5cfb4fb8314c6ea7a97bc7a02ce0f`; eventual apply requires a fresh executable of exact clean pushed HEAD |
| Preliminary doctor | 61/64 pass; expected source-lock hard failure and two shared-RPC soft failures | clean `140b7ca` report generated 08:37:23 UTC, SHA-256 `dfba859be85a25c9a928fd4e5f77b105d86581160b28f3a85410760257377cf8`; fresh post-gate Ready doctor pending |
| Test execution capacity | Terra max executes tests; Astra max diagnoses and repairs | primary parser lane complete, isolated performance repair and independent Connect fixed-shuffle race proceed in parallel; all former full237 PIDs are terminal |
| Validator evidence commitment scope | pending user choice; launch handoff paused | current section 11.1 proofs are off-chain and not transitively anchored by the payout-artifact hash or native weights |
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
- configure public operator DNS/TLS/API/history and publish a new authorized
  manifest before off-host miners or public peer-review access; loopback-only
  simulator replay does not certify that deployment;
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
