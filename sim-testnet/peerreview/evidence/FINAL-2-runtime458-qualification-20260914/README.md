# Runtime-458 qualification and closed-gate evidence

Status at 2026-09-15 00:09 UTC: affected positive qualification and all
required causal controls are complete. Neither replacement full
release gate nor the actual RC/production soak has completed.

These are local test/build artifacts, except the explicitly identified LAN
health observation. They are not on-chain acceptance evidence.

## Positive qualification

| Source | Package | Normal | Race | Location |
| --- | --- | ---: | ---: | --- |
| df98472bd88dc8e29856c172fba4731d52a6308d | CRV4 | 100 roots | 100 roots | df984/normal and df984/race |
| same | Miner | 15 roots | 15 roots | df984/normal and df984/race |
| same | Validator | 19 roots | 19 roots | df984/normal and df984/race |
| ade970aeba5a3b544609bc3074eb357b0c0cd40f | Simulator | 47 roots | 47 roots | ade970/normal and ade970/race |

The composed total is **181 top-level roots per mode**. CRV4's 19 declared
descendants are additional events, not additional top-level roots. Client
production/test inputs are unchanged by ade970a's four-path simulator/script
correction. The original source and binary identities stay attached to each
execution; reused results are not relabeled as later executions.

Exact selectors, compiled lists, raw verbose bodies, event streams, process
exits and before/after source/binary observations are retained. The required
normal confirmation processes are under each source's normal directory.
The repaired simulator's two formerly failing roots and three runtime
representatives passed p1, p2 and p3 as fresh sequential processes.

## Retained failures and evidence corrections

The original df984 simulator41 body had **39 PASS / 2 FAIL**, with binary exit1.
The stale semantic inventory named the former five-version test. Separately,
mock response/request files overlapped the helper's scratch files. The ade970
fixture change separates these paths and adds response-shape controls; it does
not loosen the production RPC helper. The original raw output is in
df984/normal; ade970/replay-retained-df984-simulator41 records the existing Go
verifier's attribution of its 805 events. Expected failures remain failures.

The first CRV4 evidence checker rejected declared descendant events after a
successful binary execution. The source-derived descendant list and replay are
retained under inputs/descendants and df984/normal/body-crv4-retry2-descendant-replay.
The original checker failure remains visible. The existing verifier accepted
all 100 roots and 19 descendants without rerunning the successful body.
The race capture's comparable input receipt was completed separately; its
original receipt is retained. Prepared owner files do not prove execution:
only actual terminal exits and matching events establish a completed run.

Other retained setup refusals include a non-executable copied owner and a
newline-only empty literal file. They occurred before test bodies. No-body
refusals are distinguished from the executed failed simulator41 run.

## Deterministic causal controls

| Mutation | Expected failures | Passing controls | Evidence |
| --- | ---: | ---: | --- |
| Five-identity production capacity | 2 | 2 | ade970/causal-capacity4-production |
| Stale semantic/gateway inventory | 2 | 1 | ade970/causal-stale-gate-inventory |
| Old public/paced artifact RPC script | 3 | 1 | ade970/causal-public-paced-rpc |
| Old current-runtime455 production policy | 7 | 6 | ade970/causal-current-to-455-policy |

Each listed causal matched the exact declared roots, failure literals and
passing controls. Test binaries exited1 as expected; conversion and outcome
verification exited0. Mutations were confined to disposable worktrees with
before/after source and binary checks. Source-only controls reuse the repaired
normal binary; the production capacity mutant was compiled separately.
All curl, cargo and sleep behavior in the RPC tests was mocked. Strings naming
public endpoints in those raw outputs are not actual network requests.

The production-policy causal compiled four separate normal binaries with an
exact six-file mutation and ran 13 roots: CRV4 4, miner 3, validator 3 and
simulator 3. It matched seven expected failures and six passing controls;
all four owners and event checkers exited0 with the expected body exit1.
The exact mutant paths, full source/dependency manifests, candidate repository
observations and binary hashes stayed fixed. Its earlier minimal-patch versus
generated-diff comparison refusal and shell parse refusal are retained; neither
started a test body.

## Closed aggregate and xops correction

aggregate-fc908 contains the complete final outer log, 25 phase logs and
preflight records. The full aggregate closed at 2026-09-14T23:33:25Z with
outer exit1: **24 phases passed and one failed**. Its final source observations
and release-lock check passed. The failure was the xops vulnerability test's
obsolete positive quota assertions. The old artifact script contained a public
route; this capture did not trace each request's destination, so no claim that
all of this historical gate's RPC traffic used LAN is made.

Xops correction42bfe0be2a7a7c51bbda87fb44886424604f509e replaces those obsolete
assertions with quota refusal and synthetic adjacent controls. xops42 retains
the complete17-test pass, sequential7-test p2/p3 passes, and old-assertion
causal1FAIL/1PASS. This correction is pushed to xops main.

xops42-prior retains three earlier attempts: r1 refused a metadata ordering
comparison before a body; r2 actually ran with12PASS/5ERROR because the
projection lacked Warp/Vault aliases; r3 actually ran with16PASS/1ERROR because
Config was still absent. Those original outcomes remain errors. Correct
physical aliases restored the approved dependency projection; the seven-test
confirmation union covers the formerly errored methods as well as the changed
gateway tests.

The earlier runtime-pin correction446cbdb remains qualified by its separately
published30-test module, affected confirmations and old-pin causal evidence.
The corrected deployment assertion has not been rerun on the actual node.
The earlier producer36PASS/1FAIL and completed typed-prior correction remain
in the sibling FINAL-2-producer-fc908-20260914 and FINAL-2-typed-prior-907-20260914
bundles. These failed full gates require replacement full runs.

## Release lock and live node

final-lock retains the readonly render against the reviewed merged source and
xops42. The final YAML SHA-256 is
d11b2a41ca6e836f9267088f8899c4fb0faf53b78b3b9ca8804bab589cd63e7b.
Relative to the previous runtime-458 lock, only
repositories.protocol_source_hash changed to bind the added gateway test.
All19 repository/library observations matched. The renderer binary was reused;
this operation made no campaign mutation. The exact lock was committed in
idle integration2b907a4bc58459c75218649e90d0c4d521aa8597.

lan-health-20260915T0003Z contains six read-only RPC calls in two direct batches
to http://192.168.1.162:9944, ending2026-09-15T00:03:15Z. Both HTTP statuses
were200 and curl exits0. The node reported16 peers, isSyncing=false,
runtime458/1/1 and chain945; the pinned finalized header is block8,007,358,
hash0xb6cd48185f3b1587ad9d9a071b6712763d4b50150a7c7f17782d8e6b008c60a9.
There was no pacing, retry, proxy, redirect or public fallback.
independent_rpc=false. No transaction was submitted.

## Portable integrity

The top-level SHA256SUMS addresses the files in this portable
bundle relative to its root. Verify it with `sha256sum -c SHA256SUMS`.
Original owner manifests are preserved as SHA256SUMS.original. Some name
absolute original locations and are not portable verification commands;
the policy13 manifest uses relative paths. The original manifests were verified
before copying. Large compiled test binaries, caches and worktree copies are
omitted; their recorded hashes and build identities are retained. No signed
transaction payload or custody state is included.

Original captures:

- /home/by/urnetwork/temp/sn-runtime458-history-capacity-20260914/terra-runtime/qualification-runtime458-df98472
- /home/by/urnetwork/temp/sn-runtime458-gate-fixture-correction-20260914/terra-runtime/qualification-runtime458-ade970a
- /home/by/urnetwork/temp/xops-rpc-vulnerability-assertion-correction-20260914/terra-runtime/vulnerability-assertion-qualification-20260914T232905Z-r4
- /home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/terra-runtime/aggregate-final37-standalone-fc908685-r1
- /home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/lock-preview-runtime458-ade970a
- /home/by/urnetwork/temp/sn-rpc-gateway-unpaced-20260914/status-lan-20260915T0003Z
