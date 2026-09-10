# Release 1.0 active work index

Updated 2026-09-09 20:24 UTC. Read this index first; use
[FINALIZE-COMPLETE.md](FINALIZE-COMPLETE.md) for detailed history and evidence.
This is not a source freeze, full-gate certificate or live acceptance report.

Implementation checkpoints SN `91572df`, server `27e5ceb7`, Connect `289bdbd`,
and SDK `6970c5a` are committed, pulled without upstream changes and pushed.
The subsequent lock/document checkpoint is described below. Source identity
and pending qualification must not be reported as testnet acceptance.
The temporary candidate remains an independent qualification workspace.
The user subsequently approved **on-chain hashes + API/MinIO proof bytes** on
2026-09-07 UTC. Section10.1 of the complete handoff now records that decision.
Implement independent immutable validator/operator evidence slots, including
no-payout windows and later audits; do not use a payout-root-only substitute.

## Objective and current state

Complete WHITEPAPER1.0 on public Bittensor testnet, netuid521:1,000 miners,
independent top200 selection, operator pools and fair demand deposits,
validator/miner/operator payments, all proofs/history, and concurrent
adversarial actors. Final acceptance still requires both full gates, the
approved live RC and three final epochs, investigation of every anomaly, and
independently replayable on-chain evidence in `FINAL.md`.

## Current frontier — 2026-09-09 20:24 UTC

The user renewed the one-hour launch objective at20:12:49UTC (deadline21:12:49)
and explicitly raised the reserve repair allowance to6,000testnet alpha.
SN source e7a8b39 and Vault cae4179 are committed and pushed. The active
repair tranche is6,000alpha; the corresponding lifetime ceiling is31,250alpha.
TAO/gas caps, the2,000alpha source minimum, and majority targets are unchanged.

The omitted retired-alpha repair is now reconstructed from authenticated plan
history before reserve sizing. The reviewed donor has21 new/adjacent roots
passing normal/race; old-source normal reproduces the exact omitted-spend
failure, with its race proof finishing independently. Source checkpoint is
not live acceptance. Clean source CLI SHA256 is
71a45926bbcc9e66e64973f2bfee60f67d50c4ba4a4b26b45ead4454eda62b58.
The reviewed lock changes only the SN Go source hash; observed/applied SHA256
is1e6cdbd75f8479c45e045c8272effafd802f0430ac85069b3c54c22df66dacda.

The earlier b4f6301 producer and aggregate were intentionally stopped through
verified native identities before source integration. Both retained143 and
all descendants joined; they are not full-gate PASS. Completed unchanged
component receipts remain under their original identities. The next strict
producer uses supported6 jobs/24CPU with every original test, mode and
deadline, prioritizing launch over the nonblocking aggregate. Serial preflight
may overlap final CLI/focused checks only within actual24CPU capacity;
those workers join before all six phase bodies run. Aggregate restarts as
capacity permits and remains mandatory for final acceptance.

Root owns final CLI, fresh doctor and two matching serialized plans. Keep
attempt4 and its immutable journal; no reset or reduced topology/adversaries.
Producer PASS and repaired-root evidence still precede campaign writes.
Prepare a bounded read-only contract observer as the deadline fallback; its
scope is explicitly separate from real operator/miner/validator RC acceptance.
The first status observation reached finalized7970236 with conservation=true,
but healthy=false (stopped supervisor) and runtime_code_matches=false
(pending coordinator upgrade). Do not relabel those as a healthy live RC.

Current external execution records:
- `temp/sn-soak-deadline-20260909T201249Z/`: approved budget, CLI/lock/readers,
  deadline status, and observer preparation.
- `temp/sn-strict-producer6-deadline2112-v1-u6om9slz/`: sealed six-job gate
  owner ready to start on the next clean pushed lock checkpoint.
- `temp/sn-retired-alpha-qualification-v1-terra-20260909/`: immutable native
  new/adjacent and physical old-source qualification.

No new testnet campaign or transaction is claimed. The complete RC, three
final epochs and independently replayable FINAL.md evidence remain required.

## Previous pre-soak frontier

Predecessor verified delta at 2026-09-09 19:27 UTC:

- SN source a99bd150, server17fba101 and Connecta91c389 are clean/pushed;
  the latest pull introduced no additional changes. The fresh clean CLI
  SHA256 d9f5f5ad13962904151d0950f029255a7c1963828137d3a5ca0e38ad4a59815c
  observed and applied the next lock. Its complete diff changes only the
  expected SN/server Go source hashes. Reviewed/applied lock SHA256 is
  ac06e1d59813feb368889455e6d4b6a3aabd9835bd8434eea2d6c72958fce280.
- The unstarted v4 upgrade approval now authenticates archived history and
  unchanged custody before generating fresh approval; it cannot rewrite old
  plans or bypass consumed nonces/progress. Two new and four adjacent roots
  pass normal/race; the old source reproduces the exact baseline refusal.
  The live read-only nonce observation remained29 at finalized block7969857.
- The cancellation parser now handles the timed reader's empty IFS and
  validates process identity fields. Nine affected roots pass normal/race;
  both old-script causal roots reproduce the exact failure in both modes.
  HTTP old-handler causal normal/race, all three validator-root race
  confirmations and the old server client-fixture causal are also closed.
- No new campaign has launched. Checkpoint this lock, then start producer12CPU,
  aggregate8CPU and final CLI2CPU together, leaving2CPU for genuinely missing
  guard proofs. The full gates retain every mode, deadline and test population.
  Root serializes fresh doctor, plan A and plan B with joined exits and quiet
  public-RPC gaps while both gates run. Producer PASS and launch-relevant
  repair obligations still precede chain writes; aggregate may overlap RC.
- Retain the older failed/terminated producer and missing Connect shuffle
  receipt as non-passing attempts. No component result supplies a full-gate
  certificate. All real RC/final epochs and FINAL.md evidence remain required.
  New immutable owner records: temp/sn-launch-refreshed-1915-v1-bTWU5lri.

Predecessor verified delta at 2026-09-09 18:59 UTC:

- No RC or final soak has launched. The19:30 launch target remains an urgent
  objective, not an achieved or guaranteed outcome. After launch, continue
  the broader tests, evidence analysis and cleanup in parallel as requested.
- Source/lock checkpoints78c0a65 and1817537 were pushed. The latest requested
  pull imported SN777eeb5 (competition API documentation) and server17fba101
  (staging/evaluator/API/migration changes). Connecta91c389 is synchronized.
  Those incoming server changes require a new release observation; the old
  final executable/lock are not approval for the updated source.
- Fresh1817537 doctor passed readiness (62/64, two expected nonblocking
  public-provider independence advisories). Plan A then refused the coordinator
  upgrade baseline. Astra is investigating the authenticated, approved-but-
  unstarted v4 replacement; no migration authority may be fabricated.
- All five strict producer preflights passed. Isolation then failed two new
  completion tests because their expected outer zero contradicted the correctly
  retained child exits23/7. The reviewed test-only correction is now integrated;
  its native qualification is in progress. Production failure propagation and
  all FIFO/ownership/cleanup assertions remain unchanged.
- The already-failed producer was stopped before pulling source. Outer exit143
  is retained and recorded native/phase owners are absent. Shutdown exposed a
  separate process-stat parser error (`fields[2]` unbound); Astra is repairing
  its inherited-IFS/cancellation boundary with deterministic adjacent tests.
  Do not mistake termination or completed component jobs for producer PASS.
- Newly closed checks include Runtime13 normal/race and old-source causal;
  PublicScenario race; server18 normal/race, original failures3N and lifecycle
  controls; current monitor normal/race with joined owners; Connect focus and
  cancellation confirmations. Two focused validator-root race confirmations
  pass. Continue only remaining exact-root/causal obligations, not whole
  completed suites. Detailed live receipts: temp/sn-launch-critical-1930-v1-1yduOMHP.

Predecessor verified delta at 2026-09-09 18:36 UTC:

- The user's renewed launch target is19:30 UTC. Concentrate on a real
  accelerated RC launch; it is not final-soak acceptance. No new campaign
  has launched. Do not add speculative production changes, tooling rewrites,
  or repeat completed unrelated qualification on this critical path.
- SNc096632, Connecta91c389 and server08766e46 are pushed checkpoints.
  The reviewed HTTP upload fixture repair and eight independently owned
  runtime-test scheduling changes are now integrated in primary SN for the
  next checkpoint. Runtime scheduling guards preserve the original seven-root
  capture partition. No production population or test deadline is reduced.
- Corrected capture ordinary/private/prior owners and semantic ordinary285
  pass normal/race. Public-scenario and fleet-projection normal pass; their
  remaining race executions run independently. HTTP affected normal/race and
  the originally failed root's three race confirmations pass.
- Both allegedly normal population/metadata stress failures invoked the
  race binary with a five-minute normal deadline. The retained commands and
  build metadata prove that harness mistake; they do not establish a product
  regression. Correctly bound normal jobs are now running concurrently. Keep
  the original failed attempts and the previously closed race confirmations.
- Terra owns native checks: remaining SN roots and guards in one lane;
  Connect, private server fixtures and current monitor ownership in another.
  Astra handles only actual blocking diagnoses and exact launch instructions.
  Root checkpoints source, refreshes the observed lock and prepares launch
  concurrently. Current captures are listed at the top of the complete handoff.
- After the clean lock checkpoint, start both strict gates and build the final
  CLI concurrently. Producer PASS, launch-relevant repaired-root checks and
  fresh doctor/matching plans precede writes. Aggregate may overlap the real
  RC but must pass before final acceptance. Preserve all final evidence and
  three-epoch obligations; no honest final completion ETA exists yet.

Predecessor verified delta at 2026-09-09 17:28 UTC:

- No live RC or final soak has launched. Both c507 strict gates failed; a
  component pass is not release approval. Full current-state instructions are
  at the top of FINALIZE-COMPLETE.md.
- Twenty reviewed scheduling, evidence-admission, queue-framing and fixture
  fix paths now appear in primary SN/server and match the candidate. These
  are checkpoint edits with qualification pending, not a source freeze.
- Candidate sibling baseline drift was corrected before new compilation:
  126 Go paths plus14 associated assets, all module manifests already equal;
  6,198 non-fix tracked paths now compare exactly with primary. Use the fresh
  candidate fence, not its older Git HEAD or the superseded fence.
- Closed scoped results include metadata3R, anchor4 normal/race plus original
  3N/old-code causal, gate32 normal/race and monitor3N/3R with clean ownership.
  Current-baseline full monitor normal/race remains after dependency sync.
- Open work: corrected capture408/semantic287 complete owners, head-admission
  causal/confirmations, gate66/queue20/remaining closure guards, four validator
  failed-root race confirmations, then both clean strict gates.
- New actual failures are being repaired in parallel: unread HTTP request
  body fixture, shuffled Connect cancellation's wrong acquisition barrier,
  missing active clients in three server ForceClose fixtures, and the full
  simulator90m timeout with late parallel admission. No test population,
  production outage protection or client admission check may be weakened.
- Native/session handles from the prior owners were absent at17:22; no host
  reboot is established. Missing aggregate final receipts and the old race
  compiler's missing exit are retained, not converted to passes. New native
  owners resume incomplete work with exact current source/binary checks.
- After repairs: checkpoint/pull/push, refresh lock/CLI, strict producer PASS,
  fresh doctor and two matching serialized public-RPC plans, real RC and
  three final epochs. Aggregate PASS, all anomaly closure and on-chain/API/
  MinIO evidence in FINAL.md remain mandatory. No credible final ETA yet.

Predecessor verified delta at 2026-09-09 14:31 UTC:

- StrictTidy3 source checkpoints SN1e0dcc3d3f33c508d0a7ddcf56399e8c1e27f2a0
  and serverace1b08dd213c82f928adf2be677247fa4d1652c are committed, pulled
  without upstream changes and pushed. The fresh clean/pushed CLI SHA256
  07bca1654f24123d0180339b0b45bc8f49ae515649b195d11a92d15b7dfa0a51
  produced the next read-only lock proposal. Root reviewed its entire diff:
  ONLY the expected server/SN source hashes change. Same-binary apply exits0
  and changes only the tracked lock, exactly matching reviewed proposal SHA256
  c359e1ccadb74057e21f377168e50d4c77ecef9b575c456672bc29c981c881c9.
  Captures: temp/sn-next-finalization-owners-v1-pswk2f/{clean-cli,lock-observation,lock-apply}.
  Checkpoint/push this lock and handoff, then start BOTH strict gates immediately.
  Their own strict source-freeze preflights supply the exact manifests; do not
  add a duplicate standalone preflight or wait for the final CLI. Rebuild that
  final executable alongside them at2CPU: producer12+aggregate8+metadata2+CLI2
  equals24. The final executable remains required for live doctor/plan/apply.

- The lock/document checkpoint d97216ff is committed, pulled and pushed. Its
  fresh clean-source executable SHA256 is
  526f78c9dec66fc91e373215003e657b2f45e2c2352fba16856ab597f4d80889,
  at temp/sn-final-revision-cli-v1-lvkf6S/sim-testnet. That revision is now a
  rejected preflight candidate, not the final qualified release.
- Both actual strict source-freeze owners refused before tests or private
  services: server module metadata was not tidy. Producer source-freeze exit1
  is retained at temp/sn-final-strict-producer-v1-ekOxhI/source-freeze;
  aggregate exit1 is retained at temp/sn-strict-aggregate-gate-v1-uybQ4L/tmp/
  urnetwork-release-gate.vjLxIwsY/preflights/source-freeze. No producer gate body
  was launched. The independent fifteen-module audit found only server and SN
  need corrections; all other thirteen live modules are tidy. Captures:
  temp/sn-final-module-tidy-audit-v1-0ZDUvA and its router-only successor
  temp/sn-final-module-tidy-router-v3-jZHTJb. Router legitimately has no go.sum;
  earlier audit-wrapper refusals ran no router product body and remain retained.
- StrictTidy3 is reviewed and integrated in primary and candidate: only
  server/go.mod, server/go.sum and sn/go.sum. It records the already-selected
  RPC client as direct, its exact existing transitive npipe requirement, and
  missing checksums. No dependency version, replacement, Go source, population
  or deadline changes. Frozen donor: temp/sn-server-strict-tidy-v1-astra/HANDOFF.md.
  Both isolated after-copy modules now pass GOWORK=off go mod tidy -diff with
  unchanged before/after module bytes. Native comparison is now complete at
  temp/sn-server-tidy-identity-v1-gAh1RC: all587 semantic module rows,892 SN
  compiled test-package rows,1273 selected local compiled/embed source hashes
  and platform/compiler settings compare identically. Root accepts this narrow
  unchanged compiled-closure evidence; retain the existing immutable R1/streaks
  under their original identities. Raw module output changes only newly
  resolvable checksum metadata. This does not make the new freeze manifest
  identical or waive fresh strict gates. Original failures remain; the existing
  immutable tidy preflight is the regression oracle.
- The fresh read-only doctor from d97216ff passes: ready=true,62 passing checks
  out of64 total, with two nonblocking advisories for the intentionally shared
  public RPC provider and actual same physical peer. This corrects an earlier
  message saying64 passes. MinIO, Docker, wallet/config and runtime455 readiness
  checks pass. Capture: temp/sn-final-cli-doctor-v1-nElTwv/stdout, generated
  2026-09-09T14:14:56Z, command exit0 and all source/binary fences0. It makes no
  chain writes and does not waive the new lock, release gates or repair streak.
- FullCensus corrected R1 is fully PASS:1878.34s test elapsed, body/checker/
  converter and every source/mode/snapshot/binary/census fence0. Peak sampled
  Rss31589152KiB. R2 is LIVE on the identical sealed snapshot/binary/profile at
  temp/sn-fullcensus-racebudget-qualification-Toy4tH/fullcensus-race/pass2:
  timeout owner1433290, actual test child1433292, GOMAX2, parallel4, inner45m,
  outer3000s. Services starts R3 only after joined passing R2 receipts. Retain
  all closed unrelated qualifications. Strict gates overlap these repeats;
  no report or completed test rerun may delay a ready critical-path owner.
- No live RC or soak has started and no new campaign transaction is claimed.
  The urgent30-minute target does not waive producer PASS, all repaired-root
  obligations, fresh doctor and matching plans. Final aggregate PASS, real RC,
  three final epochs and independently replayable FINAL.md remain mandatory.

Predecessor verified delta at 2026-09-09 14:04 UTC:

- All implementation source is checkpointed and pushed. The fresh external
  executable at temp/sn-release-lock-cli-v1-H4zA2B/sim-testnet was built from
  clean SN91572dfa0db5e514664cb1c5c8c6fc4aac6aed13, with clean before/after
  fences, matching upstream and vcs.modified=false. Its SHA256 is
  a133a0995fcf8d884ae22ff991aa8e1442441451b9439a3d885878225dd35c55.
- Root reviewed the entire read-only runtime455 lock proposal and each changed
  digest family. The same executable then applied ONLY the tracked release
  lock, with all identity/status/byte checks0. Applied SHA256 exactly equals
  the reviewed proposal: eb9536398a4e943c0ed6a86dbf430b4c31c634728345fa7c39426ccc9f0bfebf.
  Captures are observation/ and apply/ beneath that external executable root.
  Commit/push this lock and these handoff records, then build a fresh executable
  for the final commit and perform the strict twelve-repository freeze. No
  additional production source edit is currently planned.
- Focus41 is CLOSED:41/41 normal/race pass on V27. Its sole retained timed-out
  root, TestFinalCaptureCapacityPriorCarrierDecodeV2KeepsTypedLimitsAndIntentSchema,
  has three fresh sequential repaired race passes on the identical V27 binary
  and original profile:202.35s (inside41),192.52s and181.72s. All fences pass.
  The last two captures are v27-sim-focus41-priorcarrierdecode-race-streak{2,3}
  under temp/sn-gate-rerun-v11-aHOWKRHL/capture-terra-v1. Do not rerun this streak.
- FullCensusRaceBudget3's four guards pass N/R, all fences0, at
  temp/sn-fullcensus-racebudget-qualification-Toy4tH/guards-successor1.
  The original launcher BINARY/binary typo refused before any body; preserve it.
  Its script-only old-command causal also matches both expected body1/literal
  outcomes with checker0 and all source/binary/census fences0 at
  temp/sn-fullcensus-racebudget-causal-PYeCkT/run/{normal,race}. The outer causal
  coordinator's final missing-bracket error returned2 AFTER those joined body
  receipts. Receipt-only adjudication is now0 at run/adjudication.exit, with
  retained input hashes and run/adjudication.tsv; both recorded body PIDs are
  absent and product_body_reexecuted=false. Original coordinator exit2 remains.
- FullCensus repaired R1 is now LIVE, unprofiled, on the new immutable snapshot:
  temp/sn-fullcensus-racebudget-qualification-Toy4tH/fullcensus-race/pass1.
  Actual test PID1294490, GOMAXPROCS2, parallel4, inner45m, outer3000s; R binary
  SHA25606187d93643bff7e289220736bc8824a279bc43781e99f0b9ded3376fd23d3d1.
  Services owns its three sequential fresh confirmations. Earlier diagnostic
  and original10m failure remain distinct and do not count toward this streak.
- Final strict gates are prepared to overlap those repeats: Main owns producer
  JOBS3 (12CPU), Services aggregate JOBS2 (8CPU) plus metadata2CPU, total22.
  Both use CONCURRENT_GATES=2, RUN_SERVER_DB_TESTS=1 and private existing
  resources. Charge the measured31.24GiB per simultaneous full census and
  preserve memory headroom; there is no preparation CPU reservation. A passing
  immutable source freeze is NOT YET QUALIFIED. No live write precedes producer
  PASS, every repair repeat obligation closed, and fresh doctor/matching
  plans. Aggregate may continue during live RC, but any failure invalidates the
  candidate; Root owns the explicit stop-new-writes monitoring procedure.
- A bounded ownership audit found two orphaned September7 gate containers from
  temp/sn-integration-xOgvEe/capture-carry-config-chain-v1. All eleven recorded
  test invocations ended, no live consumer remained, and no current cleanup
  defect was demonstrated. Root removed ONLY those two exact-ID disposable
  containers/data through the existing custody-checking helper (exit0), verified
  their absence and preserved all source/captures. Deployment4 and shared/local2
  containers remain untouched. Receipt: that capture's
  PREDECESSOR-SERVICE-CLEANUP-20260909.md. Recreate synthetic data by rerunning
  tests; removed temporary data is not recoverable from Docker.
- Neither final strict gate nor live RC/soak has completed. No new campaign
  transaction or on-chain acceptance is claimed. The remaining critical path
  is final lock checkpoint/freeze, overlapping strict gates and metadata R3,
  then admitted real RC and three final epochs with independently replayable FINAL.md.

Predecessor verified delta at 2026-09-09 13:42 UTC:

- The renewed30-minute acceleration checkpoint at12:44UTC was reached without
  a soak start; it did not waive admission. Limit work to the actual critical
  path and adjacent deterministic regressions; preparation owns no native CPU
  reservation. Root integrates; Terra main and Terra services execute independently;
  Astra investigates actual failures and audits only launch-critical dependencies.
- PublicFileDecode5 now qualifies on V26: its six new controls and 27 readback
  controls pass normal/race; prior-carrier14 and metadata9 now pass both modes
  with every integrity fence0. Metadata9 race completed218.363s. The unchanged population900
  root passes normal in136.415s and has THREE fresh sequential repaired race passes
  on the same sealed binary:432.521s,430.259s,431.411s. All source, mode, snapshot,
  binary, census, conversion and checker fences pass. Retain, do not restart,
  this completed streak. Race captures are under temp/sn-v26-pop900-race10-
  aua9GN, temp/sn-v26-pop900-race10-pass2-STLuV4 and
  temp/sn-v26-pop900-race10-pass3-EjdJh8, each in race/.
- RegistrationPublication6 is now reviewed, integrated and formatted in BOTH
  primary/candidate trees. Exact six-file donor:
  temp/sn-registration-publication-v1-astra/HANDOFF.md. It connects the actual
  post-Sql StRegisterClientKey path to finite, ready-only local publication
  windows, preserving each member's30s operation, signatures, both readbacks,
  accounting and final Close. Thirteen deterministic roots cover real64+1
  windows, wire ownership, member/owner cancellation, blocked token, real
  pre-commit cancellation, final census errors, panic cleanup and the actual
  authenticated controller. Root caught and repaired the adjacent owner-
  cancellation gap before integration. Original1000-client/two-operator
  workload and deadlines are unchanged. Server52 now passes BOTH normal/race;
  controller24 passes normal; the original1000-client root has THREE fresh
  sequential repaired race passes on identical source/binary/profile. All
  body, census, checker, converter, private-service/cleanup and integrity fences
  pass. The disjoint remaining23 controller race roots now also pass, with
  exact24-root union proof in controller24-race-union:24 expected,23 remainder,
  1 singleton,24 union,0 overlap,0 difference. Both old-path causal controls
  qualify normal/race: actual body1 at the declared failure and literal,
  checker0 and all source/service/cleanup/binary fences0. Inputs are frozen
  at temp/sn-registration-publication6-causal-inputs-v1-astra/HANDOFF.md.
  Capture: temp/sn-registration-publication6-qualification-ESYA9E, including
  server52-normal3, server52-race, controller24-normal and
  cohort1000-race-pass{1,2,3}. Retain prebody metadata-header/quoting refusals
  separately; none ran a product test. Causal captures are
  causal-old-registration/{normal,race} and causal-old-context/{normal,race}.
  RegistrationPublication6 qualification is CLOSED; do not rerun this streak.
- V26 guard60 really failed three roots in each mode. The private-service
  checker incorrectly counted function-local WARP_ENV declarations globally;
  both evidence selectors also omitted StClientKeyPublication tests.
  ServiceProfileSelector4 repairs those actual omissions and scopes the checker
  to the server phase. RegistryNesting3 independently adds deterministic checks
  against job registrations hidden in dead branches/empty loops. Both are
  integrated/formatted in both trees. Guard60 passes normal/race twice; the
  exact3 previously failed roots then pass a third fresh sequential run in
  each mode on identical V27 binaries/source. Those repaired streaks are CLOSED.
  Captures: v27-sim-captureguard60-{streak1c,streak2}-{normal,race} and
  v27-sim-captureguard3-repair-streak3-{normal,race}. Private-profile negative
  also passes N/R, all fences0 (0.293s/3.424s). Jobs2's changed roots outside60
  now also pass N/R, all fences0 (0.867s/8.299s). Root checked the
  actual60-root census: all4 changed metadata/history registration roots are
  included, so no broader Registry rerun or new source snapshot is needed.
  V27 normal/race builds are complete and independently verified by Root:
  ac6adeb6c6617de1c2fafaf0a1945dbafc4e6a56909efcf39192c35f0e50e424 /
  17fc0bfee15c458e99c3637769fece6126ac164521b4dbce1d7d62f4f1035e72.
  An earlier message accidentally repeated V26's hash; actual V27 compiler
  inputs/cwd/source/output and binary hashes are correct. Guard admission
  rejected a copied selector that omitted `Covers` from one declared root;
  retain that prebody-only refusal and correct the command from the exact
  source-derived60-root census, without recompiling or reducing coverage.
  These checks run independently of services and the large metadata diagnostic.
- FullCensus has three repaired normal passes, but its unchanged race10m body
  genuinely failed at603.658s; all source/ownership fences passed. Peak sampled
  Rss22369864KiB, about21.33GiB. Capture:
  temp/sn-v24-fullcensus-race10-fcbsmd/race; authoritative stack is raw/body.stderr.
  At timeout the standard Json decoder was framing the1473236140-byte index;
  a single stack does not attribute all600s to that stage. Astra's source audit
  found no remaining repeated-subtree parsing and no safe drop-in removal of
  strict full-document/duplicate/number semantics. The unchanged-binary race90m
  diagnostic now completes PASS:1839.622s owner wall,1833.96s test body and
  peak32756132KiB (about31.24GiB), all integrity/census/checker fences0.
  Capture: temp/sn-v26-fullcensus-race90m-diagnostic-F1mZOn/race, including the
  original CPU profile and saved top/cumulative/line reports. Root/Astra read
  those reports: race/external instrumentation dominates sampled CPU; the Json
  decoder accounts for5.84% cumulative and canonical wire marshal0.14%.
  This supports a narrowly scoped dedicated race-test budget correction, not
  a parser/hash rewrite. FullCensusRaceBudget3 is reviewed, integrated and
  formatted in both trees: the45m metadata-only race budget and deterministic
  scope guards (4 affected roots,2 new). Normal5m, ordinary capture5/10m, aggregate90m,
  every byte/object limit and full population remain unchanged. The original
  race10m failure remains a failure and the profiled diagnostic is NONQUALIFYING.
  Three fresh sequential corrected-profile race confirmations remain required.
- MetadataRows5's old-allocation causal is now qualified: fresh external
  causal-normal2 fails at the exact expected allocation assertion
  (6158827 bytes versus270284), checker0 and every source/binary/census fence0.
  Capture: temp/sn-v26-metadatarows5-causal-v1-zuoNtw/capture/causal-normal2.
  The earlier inherited-relative-snapshot prebody refusal is retained; no
  body ran there. PublicFileDecode5's old-default causal is now CLOSED in both
  modes: exact body1/allocation assertion, checker0 and every integrity fence0.
  Capture: temp/sn-publicfiledecode5-causal-ZF5NsJ/run/{normal,race}. It proves
  the old decode path exceeds its declared allocation bound for1048576 raw
  bytes; it is not a new product failure or a rerun of the old population stress.
- Bounded metadata caller review found no demonstrated live5m/10m finalization
  timeout: publication graph discovery runs after the acceptance watchdog,
  under command/campaign cancellation; public replay's30s limit is per response,
  before graph decoding. Cancellation cannot interrupt the current finite Json
  decode/tree walk; publication rechecks it before external writes. Retain this
  cancellation-granularity limitation and observe actual live behavior. The
  full-census local timing does not separately qualify the largest carrier's
  complete two-origin HTTP transfer. Do not claim it does, or infer a production
  deadline breach from the synthetic full-root timing alone.
- Current native allocation is main12/services12, bounded24 total. Unused main
  capacity was transferred to services so its prepared race compiler can run
  beside its normal compiler, not wait for the normal owner to finish.
  FullCensus diagnostic, V27 and registration compilers are now complete, and
  their qualified work is retained. Terra main owns the remaining exact41-root
  simulator qualification: normal41/41 now passes with all fences0; race is
  independently live on V27 under its original10m profile. Services owns the
  fresh FullCensusRaceBudget3 snapshot/build, four guards, quick old-command
  causal and three corrected45m race confirmations. Source checkpoint/runtime-
  lock preparation proceeds independently; no report-packaging wait is required.
  V27 has7777 files/13 Git roots and every copy fence0. Source copies,
  builds, independent guard bodies, service bodies and diagnostics need no
  cross-lane result wait; only exact source ownership and shared resource limits
  can require a local wait. Existing live bodies keep their original owners.
  Completed Legacy/checker/population streaks are not rerun for unrelated edits.
- Twelve primary release repositories were fetched concurrently at12:32UTC;
  all HEADs still equal their fresh upstreams. Four working trees contain the
  intended uncommitted successor (SN379 paths, server80, Connect8, SDK2); eight
  are clean. Source-freeze covers15 live Go modules and one separately verified
  archive. These read-only checks remove no source or qualification obligation.
- Still mandatory: close actual remaining stress/affected/causal qualifications;
  clean committed/pushed source and freshly generated runtime455 lock. Section4
  now explicitly distinguishes immutable source freeze from qualification:
  reviewed source plus the small guard checks may be locked while unchanged-
  source repeat confirmations continue. BOTH strict gates can run concurrently
  with those repeats using measured resources, without admitting live writes.
  Charge the31.24GiB peak for each overlapping full census. Per
  the approved detailed plan sections1.2/4.4, producer PASS plus fresh doctor
  and two matching approved plans, AND every failed-root repeat obligation
  closed, can admit live RC while the aggregate gate
  continues; do NOT add aggregate completion as another pre-RC wait. An
  aggregate failure invalidates the candidate and requires stopping new writes;
  do not assume the independent scripts automatically supervise the campaign.
  Aggregate PASS, live RC, all3 final epochs and independently replayable FINAL.md
  remain mandatory for final acceptance. No new campaign transaction, source
  freeze, completed strict gate or started soak is claimed.

Predecessor verified delta at 2026-09-09 11:47 UTC:

- The reviewed implementation is in the primary working trees: 354 SN paths,
  73 server paths and 10 Connect/SDK paths, followed by FixtureMode2,
  CanonicalFast4, Checker6, LocalBatch12, MetadataRows5, LegacyBarrier3,
  HttpBodyClose6, Schedule2, CaptureMetadataIsolation3, CheckerName3 and
  HistoryIsolation5, PublicFileDecode5 and RegistryNesting3. Reviewed upstream
  overlaps and primary-only documents are preserved. This remains uncommitted
  working source, not a clean/pushed source freeze.
- Confirmed earlier component qualifications remain: validator1469 normal/race;
  local-settlement374 normal/race at its original10m; server publication43
  normal/race plus3 repaired allocation race passes; real1000-client/two-operator
  DB root3 normal passes. Checker6 qualified66 normal/race roots and replayed
  the retained DB20 roots/5 declared subtests without another body run.
  V23 qualified focused34/canonical5/fixture-mode1 in both modes, focused41/
  fixture15/guard23 normally, and RelayGenerated35 normal/race under077/022.
  These are component results, not certificates for later changed source.
- V24 FullCensus now has3 consecutive repaired normal passes on identical
  source and binary, within the unchanged5m limit. Actual body results:
  N1 276.96s; N2 258.61s; N3 251.39s (owner timings include final process exit).
  Every source/mode/snapshot/binary/census/conversion/checker fence passed.
  All1191936 slots,1473236140-byte flat index,1964315711-byte original carrier,
  graph, hashes, signatures and real local publication remain. N2 peak process
  RSS12728120KiB; N3 peak12110728KiB. Captures under capture-terra-v1:
  v24-sim-metadata-fullcensus-normal{1,2,3}/normal.
  Original V23 normal5m timeout301.489s remains retained; race10m still needs
  qualification. MetadataRows5's7 new controls also pass normal/race with
  exact7-root census and all integrity checks0; causal control remains.
- V24 population900 normal passes:165.41s, full1000-miner/200-head geometry,
  4 source owners,900 data objects,310378496 raw bytes, both replicas and
  all immutable content/history/public reads. Its race successor genuinely
  timed out at600.438s under the actual10m limit; retained runnable stack is
  strict Json decoding in readPublicCampaignSourceV2. PublicFileDecode5 is now
  integrated and formatted in both trees: canonical file readback avoids repeated
  payload-sized Json decoding while retaining fresh signature/hash checks,
  both public replicas, owned Close/cancellation and strict legacy fallback.
  Six new actual-default-path controls and the unchanged population await V26
  execution; no repaired race pass is claimed. Captures:
  v24-sim-population900-normal2/normal and v24-sim-population900-race1/race.
  Do not classify the actual race failure as a passing normal result.
- LocalBatch12 storage qualification passes all46 roots normal/race, including
  all12 new root-store controls. Qualified Go replay accepted original normal
  events after the old checker refused malformed admission metadata; neither
  body nor converter was rerun. Capture:
  temp/sn-local-blob-batch-qualification-uQB4Rg/server46-{normal,race}-checker-reanalysis1.
  Real controller20 DB normal passes20/20 with service/body/cleanup and all
  integrity checks0. Race passed17 roots, then hit the original10m aggregate
  deadline while an18th root had run only4s in setup. The first2 full-history
  roots passed251.93s and276.13s; their528.06s serial total left insufficient
  time for the remaining cases. This is not evidence of a Pg deadlock.
  HistoryIsolation5 gives these2 exact roots separate registered processes in
  BOTH gates, preserving10m modes and every source. Each TestEnv has a unique
  Pg database and exclusive renewable Redis lease inside the gate-owned private
  containers; teardown waits for all phases. New2 deterministic source and
  execution/admission controls await V26. Old-scan causal N/R are qualified
  at causal-old-scan4/{normal,race}: exact intended failure/literal, all fences0.
  The earlier accidentally edited causal copy failed compilation and is retained
  separately; frozen donor diff/file were consistent. Original DB repaired-root
  streaks and final partitioned integration execution remain.
  A separate actual failure is now reproduced twice: Controller18 remainder
  race passes17/18, while its1000-client/two-operator root fails39.53s with
  `create immutable artifact key: context deadline exceeded`; the same root
  alone on independent private services fails39.98s with the identical assertion.
  Every source/binary/service/cleanup fence passed. Captures:
  temp/sn-local-blob-batch-qualification-uQB4Rg/controller18-remainder-race
  and cohort1000-race-1. This is not explained by aggregate history scheduling.
  Astra owns the30s per-registration publication deadline root cause;
  Terra services owns one retained profile diagnostic, not blind qualification
  retries. All1000 clients, signed evidence and original operation bound remain.
- LegacyBarrier3's complete202-root normal AND race bodies pass, including all
  7 formerly failing recovery roots and4 new deterministic graph/ownership
  controls. The original checkers refused65 legitimate source-declared children;
  this was not a product-body failure. Frozen source-derived267-identity census:
  temp/sn-legacy202-descendants-v1-astra. CheckerName3 fixes descendant-only
  printable literal-name admission without changing root grammar, exact
  membership, bounds, ancestry, lifecycle or failure attribution. Its3-file/
  6-test donor is frozen and reviewed at temp/sn-checker-descendant-names-v1-astra;
  integrated/formatted with exact donor hashes. Its72 normal/race roots pass;
  the one-newline optional-literal input refusal was repaired with a separate
  zero-byte input and original-event replay only, without rerunning bodies or
  converters. Old-grammar causal normal/race controls are qualified, as are3
  fresh repaired checker-root passes in each mode. Current CheckerName3 replay
  accepts the original Legacy202 roots plus65 declared children in both modes,
  without body/converter reruns:
  temp/sn-local-blob-batch-qualification-uQB4Rg/checker-name3/legacy202-replay.
  All7 formerly failing LegacyBarrier3 roots now have3 consecutive repaired
  passes in EACH mode on identical V24 source/binaries: first full202 run,
  then exact7-root streak2b and streak3. All integrity checks pass.
  Captures: v24-sim-legacybarrier202/{normal,race} and
  v24-sim-legacybarrier7-{streak2b,streak3}/{normal,race}.
  The earlier streak2 path-owner refusal remains prebody-only evidence.
- V23 focused41 race10m really timed out: three completed serial heavy roots
  consumed420.99s before another active root. Schedule2 parallelizes exactly
  4 independently owned heavy roots; allocation measurements remain serial.
  Fresh FullCensus276.96s plus population165.41s also prove their combined
  serial work cannot fit ordinary capture5m. CaptureMetadataIsolation3 now
  excludes ONLY the exact full-census root from ordinary capture and gives it
  its own registered concurrent phase, still normal5m/race10m. Two new guards
  cover actual source selection, exact execution/admission and old combined/
  missing/broadened/hidden-failure mutations. No coverage or deadline reduction.
- HttpBodyClose6 is integrated/reviewed: bounded body ownership at7 real read
  sites joins read/Close/cancellation errors and rejects truncated receipt
  prefixes;7 new tests include both signed replicas and payout cancellation.
  V25's7771-file/13-root snapshot passed all copy/fence checks, but both actual
  compilers failed on scenario.go's unused io import left by HttpBodyClose6.
  The import is now removed in both trees; adjacent Http imports remain used.
  No V25 body pass is claimed. Fresh V26 includes this repair and HistoryIsolation5;
  its expanded source/phase guard census is60 roots. PublicFileDecode5 is also
  integrated from temp/sn-public-file-decoding-v1-astra. V26's7774-file/13-root
  copy and every copy fence passed; its normal/race compiler owners were admitted,
  but their terminal compiler/body results must still be checked.
  After that immutable copy released the candidate, RegistryNesting3 updated
  only3 test files: exact job registrations cannot hide inside an extra branch
  or empty loop. Existing metadata/history/full-validator controls cover the
  reviewed blind spot and adjacent scopes; the executable gates were already
  correct. Qualify these with the next required source successor, without
  restarting the sealed V26 work.
- Public runtime was read-only verified at09:52:27UTC: finalized7967086,
  hash0xdd6da96dc940ff024287e94551703cb31aa43fa4632ec7270bfae637133efd8b,
  node-subtensor455/tx1/state1, Evm945. Exact code/metadata and probe receipts:
  temp/sn-public-runtime-lock-prep-v1-rzIFib/RESULT.md. Runtime455 lock seed is
  integrated; stock repository/source hashes still require final regeneration.
  Installed Rust1.89 and pinned offline runtime-probe compilation are available:
  temp/sn-rust-toolchain-prep-v1-eomUEH. No final runtime-lock certificate yet.
- Both old V16 diagnostic gates are terminal failed, not strict certificates.
  Terra main owns simulator execution; Terra services owns storage/DB/checker
  qualification; Astra owns the cohort publication repair; Root owns review/integration
  and handoff. Total native allowance24 logical CPUs; preparation reserves0.
  Scope leases to actual consumers, and never restart a live child merely
  because its observation session yields or expires.
- Remaining admission: repair the cohort publication deadline and qualify
  the integrated public-file decoder; finish original
  stress, affected integration, causal/repaired-root qualification; regenerate
  the actual runtime455 lock; commit/pull/push clean final source; pass both
  complete strict release gates. Then run live RC and all3 final epochs and
  write independently replayable on-chain evidence in FINAL.md. No new
  campaign transaction or started soak is claimed.

Predecessor verified delta at 07:09 UTC:

- V17 shared-root quota ownership is integrated and formatted; all28 declared
  storage roots pass normal and race, including all9 new quota controls, real
  cross-process contention, prior usage/list/reaper/immutable-write adjacencies.
  Checked captures: capture-terra-v1/v17-blob-quota-adjacency/{normal,race}.
  The causal-old compiler's first isolated copy omitted a replacement go.mod;
  retain that pre-body refusal. Its fresh successor must include module graph
  metadata. The original1000-client/two-operator DB regression still needs its
  rebuilt real DB run and required three consecutive repaired passes.
- V16 population900 normal passes with every admission/body/census/source/mode/
  binary check; race was last observed live at07:07UTC. Captures:
  v16-sim-population900-packagingfix1/{normal,race}. Do not mix predecessor and
  changed-source streaks. Counts, bytes, both replicas and deadlines are intact.
- V16 full metadata census normal still genuinely times out at302.385s, with
  peak sampled RSS15,090,632KiB (about14.4GiB). The stack is json.Marshal of
  envelope.Payload in fresh prior-carrier verification. Its capture is
  v16-sim-metadata-fullcensus-packagingfix1/normal. Root's streaming successor
  is NOT integrated: temp/sn-evidence-streaming-fix-v1-WKKhJWNk. It retains fresh
  json.Valid/hash/signature checks and exact prior wire comparison, replacing
  only payload-sized canonical buffers with bounded64KiB streaming. Astra owns
  independent lexical/signature/allocation regression authoring and review.
- Typed attempt-record cloning plus5 deterministic ownership/codec/allocation/
  real-ledger regressions, and4 reviewed independent test-scheduling additions,
  are integrated and formatted. V18 validator race compilation passes on its
  1105-file actual Go/test closure; binary SHA256 is
  9b765a6564de26da5e1add3a1187c059de9aac48d38f98a9a5508e41c16058aa.
  Full1469-root race runtime-input fencing is in progress, not a body pass.
- A proved harness-budget defect is corrected in the candidate: V15's880
  passing serial race roots took831.18s before parallel work could start,
  exceeding the combined core race's implicit600s. The complete validator race
  now has its own registered phase with explicit parallel4/timeout90m, matching
  the other full-package budgets; other core packages and all focused deadlines
  stay unchanged. The2 new source/phase regressions and actual old-script causal
  control require qualification. Donor: temp/sn-validator-race-budget-v1-astra.
- Both V16 diagnostic gate workloads continue in
  temp/sn-v16-diagnostic-gates-QKEPuZ/{local,producer}/workspace. Their missing
  empty dependency directory entries were restored with explicit action and
  before/after receipts:9 empty directories, no source/Git file-byte changes.
  Fresh stress successors pass all7 generic gitlink checks. The ongoing gates
  are diagnostic-with-directory-packaging-correction, not whole-run immutable
  strict certificates. Missing vault/xops snapshot refusals remain recorded.
- Read-only promotion inventory is refreshed to416 candidate changes across
  SN338/server68/connect8/SDK2, with only2 upstream-overlapping paths requiring
  merges (connect/transfer.go and the server monitor documentation test).
  Primary-only whitepaper/plan/README updates must be preserved. Latest fresh
  origin fetches for these4 primary repos show HEAD==upstream; no new
  production promotion, own commit or push is claimed. Current candidate work
  remains under temp/sn-gate-rerun-v11-aHOWKRHL.
- The host-wide native-job cap is now24 on the measured24-core/125GiB host:
  gates16 + population2 + storage2 + validator4, with unchanged per-job limits.
  Preparation queues reserve no CPU. README records the observed pressure and
  resource rule. No live soak/new testnet transaction, strict gate approval,
  clean source freeze, real455 lock regeneration, RC or FINAL.md acceptance is
  claimed. Those release requirements remain unchanged.

Predecessor verified delta at 06:00 UTC:

- The renewed 04:26 UTC acceleration target was missed. No new live campaign
  or soak has started, and neither complete strict gate is approved.
- V15 full validator normal passed all 1,464 roots, with checked body,
  converter, census, source, mode and binary receipts. Fixture11 reached
  three fresh consecutive passes in both modes, including the four repaired
  original fixture failures. Captures: `v15-validator-full-actual/normal`
  and `v15-validator-fixture11-streak{1,2,3}/{normal,race}`.
- The full validator race capture omitted explicit `-test.timeout`; a direct
  test binary does not inherit `go test`'s default. It therefore ran until its
  outer 1,200-second timeout: 1,464 roots started, 1,032 passed, no per-root
  failure was emitted, and 432 remained incomplete. This is an aggregate
  timeout, not a passing gate or an observed 10-minute Go timeout. Future
  qualification must supply the actual timeout explicitly.
- V15 artifact storage passed all 28 roots normal/race; auth/service isolation
  passed all 25 normal/race; the subnet-monitor documentation root passed
  three fresh normal runs. Corrected static capture `v15-solidity-workload-rerun2`
  passed Forge's 199 tests, gencontracts and stabi output checks, and all five
  Slither targets. Earlier capture-layout/selector refusals are retained and
  are not product failures.
- Genuine open failures: population900 race hit 10 minutes; full metadata
  census normal hit five minutes (peak sampled process-tree RSS about
  9.51 GiB); the real thousand-client/two-operator database test hit ENOENT
  while a capacity scan inspected another writer's removed private partial.
  Original populations, bytes, replicas and deadlines remain unchanged.
- The reviewed two-file DB fix is integrated in the candidate: classify a
  private partial before metadata lookup. Five deterministic real-writer
  controls and causal-old comparison are going through Terra qualification.
  Frozen donor: `temp/sn-blob-usage-walk-v1-astra/BLOB-USAGE-HANDOFF.md`.
  Independent shared-root quota ownership is a confirmed adjacent defect;
  Astra is preparing a separate fix, not claiming it resolved by traversal.
- Root's simulator successor removes repeated encoding/hashing of its own
  freshly marshaled payload. Incoming mutable proofs still receive fresh
  syntax, hash and signature checks. Four paths include two new encoding
  files and six deterministic parity/operation-count/mutation controls;
  formatting, peer review, normal/race qualification and stress reruns remain.
  Existing sealed V15 binaries are being profiled as diagnostics, independently
  of source edits; those profiles cannot qualify the changed source.
- Test lanes use consumed-source scopes and sealed-binary provenance so DB
  qualification and simulator fixes can overlap. Never rewrite an active
  declared input set. Primary promotion, real runtime-455 lock generation,
  clean pushed source freeze, both complete gates, RC and live final epochs
  still remain; the temporary candidate is not the primary release.

Predecessor verified delta at 04:08 UTC:

- The user renewed the30-minute acceleration exercise; target04:26 UTC.
  Acceptance requirements remain intact. No final soak or new campaign
  transaction has started.
- V15 population900 normal passes within the original5m limit:237.26s versus
  V14's340.44s. Exact900 objects/310,378,496 original bytes, both stores and
  direct/public readbacks remain. All admission/body/converter/checker/input
  checks pass. This is normal1/3 only; race is still live at04:08 UTC.
  Capture: `capture-terra-v1/v15-sim-population900/normal`.
- V15 full server/startifact normal suite passes28/28, including5 new
  PreparedEvidence controls, with checked terminal outcomes and inputs.
  Capture: `capture-terra-v1/v15-server-startifact-suite/normal`.
- Fresh V15 simulator and validator normal/race binaries compile successfully.
  Further fixture streaks, full-validator, metadata-census/RSS and release
  qualification remain. Schedule4 is reviewed/frozen but not integrated.
- DB pre-Go failure was the one-off capture putting its fixture inside source;
  the isolation guard correctly refused it. The fresh real DB owner is outside
  source at `temp/sn-v15-server-db-producer-rerun1-d5fsi4` (session62661).
  Static's nested-index owner was corrected; session10043 is the fresh run.
  Neither refused prior attempt is an executed product/database failure.
- Earlier unit processes inherited primary WARP_HOME and logged empty-settings
  fallback. Preserve those raw observations; do not claim complete candidate-
  local runtime custody from cwd alone. Future unit captures explicitly pin
  candidate WARP_HOME and unset ambient config/site/vault home overrides;
  real DB captures keep their explicit generated private fixture environment.

The following03:52 UTC frontier retains details and unchanged obligations.

- The acceleration target was soak admission by03:53 UTC, not a waiver of
  gates, failures or evidence. It has not been met: repaired-source execution,
  complete strict gates, truthful lock/freeze and the accelerated RC remain.
  Continue the fastest safe path; no live campaign or soak is claimed.
- Candidate `temp/sn-gate-rerun-v11-aHOWKRHL` now contains V11 through V14,
  reviewed Metadata17, Wire7, three validator-fixture repairs and three
  parallel-scheduling changes. All donor preimages/raw postimages were checked
  before integration. Terra formatted exactly27 unique Go files successfully.
  Fresh7731-file/hash/mode and seven-Gitlink checks are at
  `capture-terra-v1/v15-fence/FORMATTED-v15-ALL-SOURCE.*`.
  Terra owns this source lease and fresh simulator/validator binaries.
- Qualified V14 results: Audit16 reached3/3 normal/race, Audit39 adjacent,
  Lock8, Startup8, Source-capacity20 and fixture-generator19 pass normal/race.
  Earlier component passes remain tied to their recorded inputs. They are
  not a complete release gate or automatic V15 affected-source streak.
- Capacity24 had a real performance failure: the900-object/296MiB population
  root took340.44s normal (over the producer family's5m normal budget) and
  race timed out at10m in repeated evidence verification/Json serialization.
  Wire7 now prepares one private immutable verified canonical carrier per
  object; both replicas, all eight direct reads and independent public reads
  remain. Eight new deterministic controls cover mutation, reauthentication,
  signature/receipt/canonical bytes, read/Close failures and duplicate source
  marshaling. The original population root now calls the actual producer helper.
  Applied donor `temp/sn-evidence-wire-owner-v1-astra`, handoff SHA256:
  `d224651a08a19293a774f1491d9c3e2ced2f053c3114db9133ab5e098e04ca42`.
  New speed/qualification and three-pass failure streaks are still pending.
- Metadata17 is also integrated and source-reviewed:143 selected roots,
  including11 new controls. It keeps finite typed current/prior/derived
  metadata owners separate from ordinary32MiB files and256MiB aggregates.
  The full1,191,936-slot metadata census still needs normal/race execution,
  process-tree memory measurements and unchanged budget compliance.
  Donor: `temp/sn-evidence-metadata-fit-v1-fV5xbN9J`.
- Full V14 validator1461-root normal/race captures both timed out after10m.
  Four additional failing tests were diagnosed: intent fixtures omitted the
  explicit control-byte owner; a public-file fixture became private under
  umask077; library-stream assertions ran after Go's intentional test2json
  stream alias. Repairs retain production refusals, establish real adversarial
  file mode, and observe all library initialization in TestMain before m.Run.
  Three new controls bring the full validator census to1464.
  Donor `temp/sn-validator-full-fixture-fix-v1-HLupGmyh/HANDOFF.md`,
  SHA256 `9a1b83cc976fcfa96a86b0845140d06c8d883e39e4b2478ac097bbeb4294df8a`.
- The timeout trace exposed a serial barrier: normal spent466.60s before
  releasing parallel tests; race never released them. The three largest
  isolated serial roots now call t.Parallel, retaining all fixtures/assertions.
  Their old normal durations total134.27s, not a measured new speedup.
  Ad-hoc full captures used10m/parallel2; actual local normal uses90m/parallel4
  and core race retains10m. Keep these different profiles explicit.
- Second Terra completed generator19, then found two pre-body capture errors:
  wrong anchored auth selector and an ancestor-resolving nested Gitlink check.
  Those are being corrected without product-pass claims. Real private Pg/Redis
  setup/cleanup succeeded, but the database payload exited before Go output;
  a fresh traced real setup is required to identify the exact failure.
  Edited static/Forge/generated-contract workload has not yet run.
- Execution owners: `terra_v14_resume` (simulator/validator) and
  `terra_parallel_services_v14` (services/contracts), sharing actual CPU and
  compiler reservations. Astra reviews further safe scheduling/root causes
  outside the active source; Root owns review/integration and primary merges.
  Start ready independent bodies before packaging completed receipts.
- All12 primary origins were fetched and seven clean repositories were
  fast-forwarded without touching the held candidate: server c4faf3f9,
  Connect95a0566, SDKd450797, config082d105, vault152941c, xops6ba95f5,
  glog892ade4. No service was restarted. Preserve this newer upstream work
  when promoting; the old390-path inventory must be refreshed.
- Before admission: resolve every remaining integration and all original
  failed-root streaks (V10 census has25 causal roots, plus later failures);
  promote safely, regenerate the real runtime455 lock on clean/pushed source,
  pass both complete strict gates, and perform funded setup/accelerated RC.
  The prepared lock bootstrap is only a seed; actual lock remains454.
  Three final epochs and independently on-chain-verifiable `FINAL.md` remain.

## Historical frontier (2026-09-08 22:04 UTC)

The following record is retained as history, not current pending work.

- The user explicitly requested both complete gates run concurrently with
  fixes, then three consecutive passes for each failed test. Terra actually
  invoked both scripts in parallel; both exited1 at source-freeze before any
  gate body because the held candidate is not a Git checkout. Original logs
  remain under held/capture-terra-v1/v9-full-gates-preflight/{local,producer}.
  These are release-preflight refusals, not product-test failures or passes.
  Astra is adding an explicit diagnostic mode that runs the unchanged full
  workloads while retaining attestation failures and never certifying a release.
  Independent admitted body jobs continue. Final strict full gates remain required.
- The harness README now requires three uncached, sequential confirmations
  per failed root and failed mode on one unchanged source/binary. Any renewed
  failure or changed input resets that streak. Original evidence, deterministic
  root-cause controls and adjacent integration coverage remain mandatory.
- V9 Connect readiness30, SDK readiness3 and Stabi6 pass in normal/race modes;
  event checks and final source/mode/binary fences are complete. Batch23 N/R
  both timed out at the unchanged10m package limit in
  TestReleaseClientKeyHistoryBatchCancellationJoinsNoAuthority. The client had
  returned context.Canceled; the test's Http handler never consumed its large
  request body and could not observe disconnect. Astra is repairing the
  deterministic fixture and adjacent cancellation controls, not extending time.
  Paused roots are not passes. Original streams/stacks and the three-pass
  failure ledger remain under held/capture-terra-v1. All V9 readers are joined
  and Terra released the source lease; root installed18 V10 raw postimages
  after exact preimage checks. Diagnostic-mode and batch-fixture deltas remain
  pending; no V10 build or full diagnostic workload has started yet.
- V8 is fully joined and its held-source lease is released. Qualified normal
  and race groups are budget5, capacity20, runtime455-21, capture21, dispatch2,
  reserved-predecessor1, rendered-setup1, identity3 and gate10. Their exact
  root/event checks and source/mode/binary fences pass. These are component
  qualifications, not either full release gate.
- The full-population template diagnostic also passes under the existing
  semantic-gate bounds: normal 206.30 seconds / 106,628 KiB peak RSS; race
  429.54 seconds / 269,060 KiB. Both complete receipts pass. The earlier
  120-second focused timeout does not establish a production hang; no gate
  deadline or population was relaxed. Repeated plan/config authentication
  dominates the measured CPU paths, with no observed lock bottleneck.
- One genuine V8 failure awaits its V9 regression run: finishing a short-lived Rpc owner also
  canceled longer-lived retained startup files. The frozen custody2 fix
  separates those lifetimes while preserving parent cancellation, closed-owner
  refusal and joining active reads. Its four regression tests are unqualified.
- V9 is composed and handed to Terra's exclusive qualification lease:
  65 owned paths plus 61 disjoint upstream paths, 62 owned Go files, 67 new
  top-level regression roots and no removed roots. All source hashes, modes
  and 83 saved preimages were verified. Batch26, quota13, stress1, custody2,
  cohort6, readiness12, upstream merge3 and both actual gate selections are
  joined. Validator, server/controller and Connect normal/race compiles pass;
  SDK normal compiles. Simulator normal/race compilation both fail Go vet on
  a new duplicate-constant inequality assertion. The frozen one-file repair
  preserves both restart-limit checks; no vet option or production limit changes.
- V9 custody6 executes in both modes: five roots pass, including the original
  retained-recovery failure; the new parent-cancellation root fails because it
  incorrectly expects physical close to return the context error. Cancellation
  admission correctly refuses. The one-file correction is frozen; outer
  cancellation joins were reviewed and actual descriptor-close, idempotence
  and late-error preservation assertions were added. Qualification is pending.
  Source/mode/binary fences pass. Earlier two shell parser refusals occurred
  before any body and remain distinct retained launcher failures, not passes.
- Durable registration readiness now follows the actual processed server
  response, current key generation and one provider snapshot. The real swarm
  opts in, and its existing startup barrier waits for readiness. Retry,
  cancellation, rotation and real publication-failure tests are included.
  A transport acknowledgment alone no longer qualifies readiness in this path.
- Pulled upstream migration history is preserved through index634; new key
  history appends at635/version636. Qualification requires fresh private
  databases; an old draft catalog cannot be relabeled into the new history.
- Three definite launch issues have isolated frozen fixes awaiting qualification:
  an unconditional staged validator refusal; relay plan authentication using
  a derived proxy config instead of the canonical approved config; and relay
  slot budgeting that omitted the operator dimension. None is waived. The
  latter correction uses128 slots without increasing existing absolute spending
  caps. Review found that its15-closed/13-audit envelope assumes a nominal14h
  lifetime that the actual scenario does not enforce. Astra is deriving a
  bounded admission/horizon check from actual configured phase work, retained
  pending slots and journal debits;128 is not certified sufficient for the run.
- Storage stress materializes **296 MiB** and measures both replicas. Its
  separate metadata census is not a materialized 66.25 GiB corpus. The current
  V9 census is339,968; the pending relay-slot correction will change that
  derived count and requires requalification of the exact affected roots.
- Remaining before final soak: qualify V9, join the three launch fixes and the
  unresolved adjacent families; prepare a clean, pinned, pushed candidate;
  pass both genuine full gates; then complete real setup and the approved
  accelerated live release-candidate phase. Independent offline analysis may
  overlap capture, but is required before final acceptance. Public Rpc remains
  selected; waiting for private-node sync is not a launch dependency.

Frozen donor locations and exact hashes are in the newest section of
FINALIZE-COMPLETE.md. Temporary candidates and test receipts remain under
`/home/by/urnetwork/temp/sn-*`; they are not backed up by this primary checkpoint.
Current handoff: `temp/sn-composition-v9-nDs6pEae/V9-HANDOFF.md`,
SHA `786fcd7672ebc53323e579c792dea17f3cd413250158c5b8d88e35cd67e8a5a0`.
Do not edit the held candidate while Terra formats, builds or tests it.

## Launch-critical priority

The following 18:18 record retains composition details; use the frontier above
for current qualification status.

The user escalated the week-late soak on2026-09-08. Stop expanding standalone
subsystems. Reuse existing qualified primitives and connect one real runtime.
The following work supersedes earlier broad lane descriptions:

| Launch dependency | Owner | Required outcome |
| --- | --- | --- |
| Bounded capture/publication | Astra runtime/publication | Capacity22 is integrated in V8; add actual 900-object streaming/high-water and full 339,968-slot metadata tests separately |
| Bounded historical chain reads | Astra native/key-history | Finish actual bounded batch paths and plural-only request deadline; qualify shared and 404-distinct registration boundaries with per-client authentication |
| Quotas, integration and failure triage | Root | Quota13 is frozen with 8 new roots; compose with its real batch dependency after V8 qualification; retain every failure |
| Qualification and launch gates | Terra max | V8 exclusive format/build/test lease; validator/simulator normal/race groups run independently; full gates need actual clean/pushed repositories |

Current candidate is V8, composed only after Terra released every V7 reader.
35 unique files (34 Go;25 existing/10 new) combine authority7, capacity22,
identity2, null1, dispatch1, case2 and gate2. All raw hashes/modes and26,445
selected lines were checked;25 exact V7 preimages are retained. Two shared
files preserve both independent changes; four bridges are formatting only.
Handoff: temp/sn-composition-v8-OcE8OCEt/V8-HANDOFF.md,
SHA7b9e105204a9217cb0b2696aeb38404e32436bcdc5524cce24cb5df9bd3c9a5b.
RAW-SOURCE.sha256:9bffd22ed7d97bf604dabf115d9d728deacc2afa29be952d1cb0d616deef8353.
Terra owns formatting, parallel normal/race builds and47 independently counted
new roots plus adjacent families. Root/Astra never run Go in these lanes.
This is integration, not qualification, source promotion or a full gate.

Retained V7 qualified body results: runtime5, head4, streaming12 and provision8
all pass in normal and race mode, with actual body/converter/checker and
source/mode/binary fences. Runtime455 crv4 18 and validator4 N/R have passing
bodies/converters/checkers; final fences are closing. Runtime455 simulator21
N/R has passing bodies but refused event receipts because24 existing children
were undeclared. The proposed child declaration also refused the checker's
top-level-only grammar. V8 case2 follows CODESTYLE with plain loops retaining
all24 inputs/assertions; rerun the same21 roots, without changing the adapter.

V7 capture20 N/R has one genuine test-only mismatch: pinned Gsrpc storage
facade returns a nonnil pointer to empty bytes for both null and empty hex.
Production capture now preserves their distinct original wire bytes. Frozen
null-facade1 aligns the test with the actual unwrapped facade and adds an
adjacent original-wire control; it is composed in V8 but not yet tested. Audit compliant1
failed because the fixture formatted hexutil.Bytes through its Stringer when
looking up a plain-byte hex key. Frozen decision-dispatch1 uses explicit hex
encoding and adds two real Http-dispatch controls; composed in V8, not tested.
V8 identity2 also fixes the persisted setup-plan/origin mismatch and the fixture's
old runtime lock, without rewriting the actual lock. Renderer race exhausted
a cumulative120s group budget after its first three roots consumed114.63s;
independent singleton pairs preserve each deadline. Two V7 singleton pairs
passed N/R. Separate full-template N/R120s timeouts have no proved deadlock:
at least7,000 atomic writes/14,000 syncs are performed. Profile the exact root
under existing15m normal/25m race gate bounds; do not weaken durable writes.

The user requested Rpc/quota/storage improvements in parallel. Root quota13
is frozen at temp/sn-observation-quota-v1-wxWfSp3n, with six startup/workload
tests and two real Redis tests (1,000 miners, both validators, 4,000 logical
observations plus64 hostile reservations). Full-V2 admission rejects deficient
quotas before vault access/launch mutation. New counter allowances are 2 TiB
per deployment/hour and1 TiB per account/hour, derived from actual population,
native cadence, retries, restarts and bounded adversarial traffic. These are
conservative reservation counters, not allocations or expected stored bytes.
The Rpc donor must enforce at most two reservations/member and charge hostile
batches by logical members. Quota13 waits for that actual dependency; capacity22
and authority7 are already in V8. None is yet qualified. Keep per-object,
in-flight, disk/history and replica limits intact.

Important remaining performance risk: the shared40/min gate charges actual
Http requests, not individual Json-Rpc batch members. Existing batches have
finite member/byte bounds. Distinct historical registration boundaries defeat
same-block sharing: the current conservative fallback projection for404
distinct boundaries/operator is approximately1,470 paced requests across both
operators/validators (36.75 idle-gate minutes), not a measured result. Correction:
30 seconds is each observation request, not the whole decision. The latter
uses its service context and must still submit in its original native epoch.
Four contenders each needing8 admissions take46.5s at the current gate; the
batch successor is adding a finite180s plural-only deadline across controller,
client and response writer, leaving ordinary30s calls unchanged. Its144+36s
planning envelope is not immunity to arbitrary external contention. Actual
server/validator full-population tests must measure Http and method counts
separately. Never drop signatures/history or assume a fabricated common block.
Private Rpc was reachable and progressing at17:57 UTC, still syncing:
current7,448,525/highest7,962,334. It is not yet a current-head replacement.
No endpoint switch or transaction was made for these changes.

Historical V6 records (not current V7 status): pending12 normal/race pass with12/12 exact roots,
51 events/11120 bytes each; head4 race passes4/4. Head4 normal genuinely failed
an empty-to-empty hash mutation control, fixed by selecting an authenticated
active nonempty binding. Capture15 normal/race failed only actual null storage:
upstream Json decoding through an interface bypassed the old capture decoder.
Null2 adds nullable destination handling and five deterministic exact/overflow/
omitted-result/archive-refusal controls, preserving all production bounds.
Renderer/reserved7 failed explicit private-state mode admission; mode3 repairs
only fixture-owned roots and tests actual Journal refusal at0750/0775.

Subsequent v6 runtime5/finalcapture8/audit9/audit2/relay11/publication10/
transaction11 attempts are RAW-ONLY, not qualified per-root passes: their
direct command omitted mandatory -test.v=test2json. All raw attempts remain;
there is no silent correctedv6 rerun. V7 uses the existing proven literal
adapter with pre-launch flag/census checks. Diagnosed raw failures: runtime5
assumed ordinal source IDs1/2 instead of signed2/3; finalcapture/relay shared
a fixture advertising64KiB headers over a4KiB metadata reader. Fixture-census2
repairs both and strengthens complete restart-census/boundary tests. Audit9
has a missing independently defined Evm view at2415; diagnostic1 adds bounded
selector/target detail, not a fabricated response or claimed fix. Rejected
head-control-v3 was a truncated donor copy and never compiled; correctedv4
retains all14 original roots and changes only the intended13-line control.

V7 also joins runtime455 compatibility/source28, fresh config/template7,
streaming10 and its executable source guard2. The complete checked-in YAML
retains1000/202/top200 and5×300+3×360 geometry. Canonical M8 source arithmetic
is2.038GiB release and3.506GiB cumulative production before other controls;
these are source-derived workload bounds, not measured samples or admission
for arbitrary hostile input. Default256MiB/4096 archive caps remain strict
until the separate cfg-derived continuation is qualified. Prior signed carrier
bytes must be authenticated through a complete original manifest/census and
both replicas; simply excluding their path prefix would lose evidence.

Another source-derived bottleneck is repeated historical authority getters:
808 active clients × two validators ×24 getters estimates38,784 Evm calls,
before other reads, under the unchanged40/minute budget. The per-operation
reader successor must retain full independent authority and show actual request
counts; no quota increase or claim that dedup removes unique-boundary costs.
Host filesystem had508GiB available at16:25, not a reserved resource guarantee.
No full gate, clean source freeze, new live transaction or soak is claimed.

Earlier v6 integration record follows; v7 above supersedes its pending statuses. After all
baseline readers joined, root composed15 unique paths from pending8,
renderer2 then reserved-CREATE3, head-binding2 and native-capture-fixture1.
Ten existing preimages and five new absences were checked; every final readback
matches its donor. RAW-COMPOSED-v6-SOURCE.sha256 has SHA256
0c3eb509428adcd2039eab3956f298aefddf0f57c9d6f9cf1c372fea7d0acf65.
Terra formatted only those paths, fenced the full source/module/modes, and
validator/simulator normal AND race compiles all pass with before/after0.
The next body matrix is head4, capture15, renderer/reserved7 and pending12 in
both modes. Pending includes one pre-existing adjacent root beyond11 authored
roots; the initial11-versus12 census refusal remains retained.

New actual body qualifications: keyhistory6 normal/race, native11 race and
winner3 race all pass with exact converter/checker/fences0 on v4 binaries.
Both original head fixture roots then genuinely failed N/R: their pure
measurement oracle omitted the independently signed client observation hash.
The test-only repair authenticates retained signed bytes/historical authority
and leaves production equality strict. Original renderer N/R fails for missing
protected capacity/approved CREATE setup. The composed fix uses actual generated
constructor bytes, signed Evm transaction, reopened journal and verified source
postcondition; it also rejects forged/foreign/conflicting creation history.
Capture13 N/R passed12 and failed the native-stake root because its typed
fixture header setter bypassed Json decoding. The one-file correction follows
the real Json destination contract and adds actual Http header/null controls.
Original failures, compiler errors and pre-body launcher mistakes are retained;
successful compiles do not qualify these new body fixes by themselves.

Public testnet upgraded to node-subtensor/455/1/1. Root read finalized7961421
and matched code bytes to official CI artifact10034099580 on source67dcf7f.
Terra's exact-Wasm probe reproduced455 metadata/version/code hashes with exit0;
its initial nonexistent-filename preflight failure is retained separately.
Audit inputs: temp/sn-runtime-455-audit-v1-OlFoph89/AUDIT.md. Compatibility15
is frozen at temp/sn-runtime-455-compat-v1-unFVtJCw; root's source/artifact/
public-config/producer-coverage join is in sn-runtime-455-source-v1-HGMVcyqZ.
These455 changes are not yet composed or qualified. Historical451–454 replay
stays exact; there is no invented455 tag/mainnet timepoint or future-spec bypass.
Read-only public queries still returned all four pinned historical code hashes.

Current config also exposed real capacity/bootstrapping dependencies: a static
pre-plan file cannot truthfully contain future CREATE addresses; V2 accepted
source bounds exceed the old fixed campaign archive limits and its all-bytes
memory ownership. The active fixes derive only deployment identities after
verified setup and stream retained objects with finite authenticated capacity.
Do not fix these by lowering population/epoch coverage or merely increasing
an unbounded memory allocation. Approved geometry remains five300-block RC
epochs plus three360-block production epochs:2580 blocks before transition,
boundary and settlement overhead.72 minutes describes one360-block epoch at
12 seconds/block, not the entire campaign.

The combined launch candidate now exists at
temp/sn-launch-integrated-v1-yLRCb2DC. Its INTEGRATED-LAUNCH-HANDOFF-v1.md
has SHA2567b60260eb69b5d2700d62584ff7c889bfe3105f22c4b19dc40b59c5353089bd9.
It contains the real activation producer, native submission, protected upload,
signed key history, receipt fixes and the funded relay17. Terra formatted only
the126 changed Go files after raw readers joined; both raw preflight refusals
and new FORMATTED-v1 source/module fences are retained. Actual normal compiles
for crv4, validator and protocol exited0. Native commitment4, validator native11
and winner3 normal bodies pass. Publication10 and transaction11 normal bodies
also pass with exact root checkers and all source/closure/binary fences0.
The corrected physical-CWD runtime3 body reached actual startup and failed
all three roots: its fixture overwrote the reviewed native runtime pins.
The two-file correction preserves production cfg validation and routes the
independently encoded fixture artifact through the existing private startup
boundary. It adds two adjacent roots and is composed, not yet qualified.
Keyhistory4 normal passed three roots and failed the real live-head recovery
root: its old 1 MiB fixture budget omitted the new signed response owners.
Test-only budget4 preserves production limits and adds actual HTTP/refusal/
immutable-retry coverage; its complete six-root normal/race family now passes.
Both failures and the earlier launcher-CWD failure remain retained.

Both server compilers exposed one unused fmt import. After all readers joined,
root removed only that import. Fresh FORMATTED-v2 api/controller compiles now
pass with all source/module/mode fences0. The aggregate v2 manifest hash is
40ede8bd448c1ab14c973e70f87720d7a1f5409e76a7e5d61010c9242c0929bf.
After all held readers joined, root applied seven exact frozen donors:46
unique files,23 existing preimages and23 previously absent files. All final
raw SHA checks pass; RAW-COMPOSED-v3-SOURCE.sha256 has SHA256
3e57a8b8cca21cf184c9eeea8910a39fb69f8b47207dd6a2a3e80b26bec0ae7e.
Terra formatted the selected Go files and joined the four key-history tests.
Later v4 simulator compiles found two actual source mistakes: uint64 reserve
fields used IsZero and a test mutated a signature on the wrong nested field.
Root's exact two-file correction plus adjacent native-spend refusal root led
to successful v5 normal/race compiles. The newer v6 state is recorded above;
earlier binaries do not qualify later source. No build is a full release gate.

Latest verified results: primary EvmTxManager rename builds in normal/race,
with four focused roots passing in both modes. CODESTYLE now requires Evm,
Tcp, Rpc, Http, Api, Json and Id in owned identifiers; wire contracts are
unchanged. Actual handler16 passes normal/race, and the original handler's
causal failure is retained and checked in both modes. The three heavy simulator
roots now pass together in normal AND race mode under the existing semantic
gate's 15m/25m classification (parallel4, GOMAXPROCS2). Both checkers match all
three expected passes and all source/binary/input fences0. Exact receipts:
temp/sn-fixture-closure-qualification-v1-1RoFwd/capture/semantic-gate-exact3-v1.
The original three cold120-second race timeouts remain preserved, as does the
338.09-second diagnostic. Profiling identified mandatory full fixture setup
repeated in separate processes, not a demonstrated deadlock. Population,
assertions and full-gate deadlines are unchanged. Detached-cache normal/race
bodies also pass on the same repaired binaries. Race has all fences/checker0;
normal's initial converter misuse is retained, with corrected conversion and
checker0 over the unchanged raw body (no test rerun). Exact receipts are under
the same fixture root's capture/semantic-gate-detached1-v1 directory.
No full gate or live acceptance is implied by these results.

Frozen follow-ons now include gate6 at
temp/sn-launch-gate-coverage-v2-QjA630DI/GATE-COVERAGE-HANDOFF-v1.md
(SHA256 fef56410b939f9870324294ef8e935fc58e7cd6ca2edabb2044fed95b5068a75)
and the original relay admission/capture6 at
temp/sn-relay-capture-admission-v1-0iXbrlGO/RELAY-CAPTURE-ADMISSION-HANDOFF-v1.md
(SHA256 e6ad5bfd745843a2e06157aebacdf531fe16025d15bfe909f58ae780ca349c0a).
Their new guards and five real signed-request regressions are now composed
and await Terra qualification. Chronology7 is also composed from
temp/sn-relay-chronology-v2-mNIbQlHn/RELAY-CHRONOLOGY-HANDOFF-v1.md
(SHA256 ab20f27d68b427830c6a5e5943f3aaca97a25f7f65c0549b025f9e34179c4b3b).
It retains exact original relay requests in launch-foundation and the actual
fleet-generation artifact, joins the independent offline readers, and
classifies authenticated native actions before EVM-height comparisons.

Capture also exposed an approved-origin/renderer mismatch for non-loopback
origins. The current vault values were read-only checked and are exactly
http://127.0.0.1:18081 and :18082, matching the current renderer. It is not an
additional blocker for this profile. Root's four-file configured-origin join
is now composed; both actual rendering entrypoints validate the same census
before writes, and validator/provider/claim APIs use the approved origins.
Its two new roots plus the existing full renderer round-trip remain to run.

The user explicitly requested parallel race/integration/full-gate work. Terra
runs ready bodies and compilers concurrently; Astra agents independently
resolve integration failures. Three attempted additional v2 launches failed
shell parsing before any Go child/fence/body admission. They are retained as
launcher failures, not race passes or running tests. Use minimal literal
commands and actual process admission, not new elaborate quoted wrappers.
Both full scripts require clean, pushed,
frozen source across their repository census. The temporary combined candidate
has no .git and cannot honestly pass that preflight. Run both complete gates
concurrently only after the real freeze, using separate PG/Redis fixtures and
ports; do not disable the preflight or call constituent tests a full gate.

Activation setup was genuinely absent: the harness consumed files it never
created. The frozen launch23 overlay now includes real preparation/signatures,
four fixed keeper publication actions, finalized boundary inputs and rendering.
Its handoff is temp/sn-release-launch-v2-8oE5ya83/RELEASE-LAUNCH-HANDOFF-v1.md;
apply the separate sn-release-publication-bounds-v2-D3yP9ONj correction after it.
Native14 is frozen in sn-submit-native-source-v2-8e0jlFki. These authored paths
are composed but still require test qualification. Funded relay17 is composed:
it retains original signed transaction bytes, authenticates canonical winners,
reserves a finite plan-bound slot allowance and joins its campaign worker.
Later audit publication/drain and lossless V2 terminal capture source are now
composed, but their normal/race qualification remains. Complete working launch
configuration and independent final V2 semantic analysis remain unfinished.
Capture10 deliberately emits pending_offline_verification and preserves v6
intent bytes; it neither down-converts V2 evidence nor supplies a final verdict.
The prior analyzer treated intentional pending as fatal and capture waited for
prior RC semantic output too early. The composed pending8 join performs one
analysis attempt per phase, retains a discovery-only immutable pending job,
and joins both captures/workers without granting semantic acceptance. Actual
source/readback/cancellation failures stay fatal and final acceptance stays
fail-closed. Offline interpreter completion is not a new pre-launch gate;
qualified runtime/capture, actual source freeze and both full gates still are.
No new campaign or testnet writes.

The newly drafted standalone relay account store is parked at
temp/sn-evidence-relay-runtime-v1-tkvK9l95/PARKED-ACCOUNT-STORE.md and is NOT a
launch dependency. A working existing transaction manager already supplies
funded signing, action gas bounds, persisted RLP and recovery. It still needs
the actual evidence caller and reviewed receipt fixes; reuse is not a pass.

Protected upload source25 is now frozen at
temp/sn-attempt-reserved-v2-r32pLEu2/RESERVED-UPLOAD-HANDOFF-v1.md,
SHA2568916a57d42d61fdfbfff7a70a0d211435a9f9fc82bd6d568c1183de14cff53bd.
It has28 new top-level roots, unexecuted. Actual runtime joining continues in
temp/sn-release-launch-v2-8oE5ya83; do not edit the frozen upload donor.
RunRelease's unconditional guard remains an actual blocker, not a paperwork
gate. Both full release gates still precede campaign writes. Report polish,
nonblocking refactors and additional analysis can overlap the live campaign;
known correctness failures, deterministic regressions, full population/census
and final evidence requirements are not waived. No new launch ETA is asserted
before the integrated runtime and gate results exist.

## Verified implementation progress

- Primary SN checkpoint is `de06691`. Qualified Head/EMA53, Stats28,
  Gate10, Terminal33 and ordinary retained-authority3 are now promoted; those
  last stages and subsequent primary evidence work are included in this checkpoint. Newer native/runtime29
  and shutdown fixes remain. Historical native schedule2 is now promoted, with
  exact9 normal/race tests independently confirmed from the primary tree. Closed-census
  publisher2 is now promoted, independently reviewed, and confirmed from primary
  with exact17 new and27 retained replica tests passing normal/race.
- Server checkpoint is `9b582d91`, with qualified private-service/profiler changes. The
  disposable two-pair PostgreSQL/Redis Docker smoke passed; shared services
  were not changed. No new live campaign or transaction is claimed here.
- One integration candidate: `temp/sn-integration-xOgvEe/sn`. Its outside-source
  `INTEGRATION.md` identifies exact stages, artifacts and ownership.

Latest verified progress: selected upload/session SN-side128 roots now pass
normal/race: validator120, clientauth3 and SDK5. Writer-misbinding original-source
causal1 matches both modes. Root read the actual new and retained checker
summaries; these128 do not include the selected server115 or either full gate.

The private Go authentication fixture now passes12 normal/race roots and
earliest-source causal2 in each mode. The real tool created fresh private JWT
and account-password resources without host vault/config fallback. Its exact
formatted main.go/main_test.go bytes are promoted to scripts/server-fixture;
the standalone go.mod and generated credentials are not promoted. The first
fixture version's5PASS/5FAIL bodies in each mode remain retained: its helper
wrongly assumed testing.TempDir guaranteed0700. Source-containment and missing
password-pepper controls also remain causal evidence, not hidden setup retries.

After all upload-stage readers joined, root composed reviewed Carry4, outer
upload config8 and pinned chain decision10. All22 raw donor hashes and the
existing preimages matched. Terra formatted only22; complete SN262-path
manifest SHA256 is d5a18d2c5301f75b7e9d7aa4aab156003bc5c61977b0ddf3acd29bd7af157992,
with source fence/diff0. Server21 remains985805e9, with fence/diff0. Three existing
simulator files retain0644 while their donors are0664; this is recorded, not
silently chmodded. The initial0664-versus664 string-comparison preflight error
is retained separately and is not a Go test failure.

Current capture is temp/sn-integration-xOgvEe/capture-carry-config-chain-v1.
Root independently verified49 selected server roots PASS normal/race (root14,
API12, controller23), plus three new light simulator roots PASS normal/race.
These are focused results, not all server115, simulator187 or either full gate.
The real handler deadline test exposed a joined-error parser returning500
instead of408; its failure is retained. A separately owned HTTP successor at
temp/sn-http-status-qualification-v1-tiH2tS/server preserves the original held
graph. Its original-parser9 causal controls now match2PASS/7 expectedFAIL in
both modes. The repaired router's first real23-root bodies encountered missing
WARP_ENV in retained TestRouterBasic: preserve that environment failure, and
qualify again only with the explicit private fixture. Repaired handler normal
and race binaries have been produced; their body results are not yet claimed.

Root and independent Astra also reviewed frozen receipt/readback source8 at
temp/sn-evidence-receipt-manager-v2-833Ox9dt/RECEIPT-READBACK-HANDOFF-v2.md.
Its handoff SHA256 is22e8ad71019905f54c4db72c892f9efa56263970bfaaacd67dcd0192d9441133;
source8.tsv is07a492adba735a8c5d9b7213f6a6a44f877e15aae5fe0cb34a9de09df2659b22.
Exact52 selected roots include27 new roots; qualification/composition remain
pending. It adds exact signed transaction/finalized evidence readback and fixes
real simulator/server receipt identity, height bounds and candidate ordering.
It is not the funded durable broadcaster or its production runtime caller.

Pending selected obligations: Carry139 plus outer48 have zero overlap and an
exact187-root simulator union, with every old timeout/census/deadline preserved.
Pinned chain92 retains genuine M8 live controls and original-source causal3,
plus two new historical participant-Active controls with causal2. The general
complete registry observer may report inactive members when two healthy peers
remain; historical intent admission must still require every configured
participant Active at that exact boundary. Upload server115 and causal4 are
independent runnable packages, not blocked on these simulator/validator groups.

Root's outside-source COMPOSITION-CARRY-CONFIG-CHAIN-v1.md records all22 donors,
preimages, ownership, exact selection unions and mandatory limits. Candidate
and server remain held by Terra; later changes stay in isolated directories.
Multi-account staging resilience, historical signed key observations, complete
native submission/recovery, relay/finalized readback and live provisioning remain
unfinished. RunRelease's production integration guard remains in place.

## Active owners and next actions

The user approved two Astra max implementation/fix agents, one Terra max
executor driving concurrent isolated jobs, and root integration. Current owners:
`astra_receipt_review_v3` (review completed; protected uploads/runtime joining),
`astra_key_history_resume_v3` (signed key-history capture and native joining),
`terra_qualification_resume_v3` (all execution), and root (durable evidence
relay/integration). Earlier v2 owners are not live. Each independent package
starts when its own source, binary and private resources are ready; do not
introduce a whole-phase or all-compilers barrier.

| Work | Owner | Current boundary |
| --- | --- | --- |
| Gate composition | Root promotion complete; Terra max evidence | Original63 failed the274/276 census; exact repair adds two rows and widens existing omission controls. Focus3 and widened63 pass normal/race; generator11 passes both. All10 gate/census paths promoted; no full-gate claim |
| Go qualification runner | Astra max review/repair; Terra max execution | Module-alias repair and parent-owned fixture correction pass exact54 N/R (219 events each). Root read both summaries; causal1 matches its expected failure. Configured roots remain physical and aliases resolve only to declared sources. Shared simulator compilation now passes N/R within normal300/race360, using retained caches. Service-backed matrices and real whole-matrix smoke remain unqualified |
| Terminal V2 | Root promotion complete; Terra max evidence | Exact123 passes normal/race in groups26/37/30/30, plus Stats/ordinary99, affected+Head51, focused transport1, simulator4/2. Root verified137-source fence, exact preimages and event summaries; promoted33 formatted files. Original causal/full-package obligations remain separate |
| Explicit production V2 config + ordinary binding | Terra max qualification complete; root composition | Ordinary5/widened50/SIM2 pass N/R; old-production causal5 is1PASS/4FAIL both modes; root promoted3. Config47 is now fully matched N/R: simulator6+20 in repair-v4, validator14+7 in repair-v5. Private fixtures are inert; nine retained cases use a plain loop without dropping assertions. Root independently read all four v5 checker summaries and source fence0. Temporary RunRelease guard remains unfinished startup |
| Actual startup-to-submission | Astra startup implementation/fixes; root integration; Terra qualification | Metadata repair validator17/runtime6 and semantic startup23 now pass N/R; original config causal2 matches. Immutable-close2 is composed, original production causal7 matches, repaired24/retained42 pass N/R. Reviewed chain10 is composed and chain92 awaits qualification. Astra now owns actual signed key-history capture and native source-commitment/submission recovery; negative API audit history remains unproven. Copying artifact claims is forbidden. Production activation/submission, upload capacity/liveness and RunRelease guard removal remain open |
| On-chain evidence hashes | Terra max Solidity execution; Astra max failures | Full Solidity17 suites/199 tests PASS, zero failures/skips, exact census and source/artifact fences. Gencontracts24 N/R pass after normalizer5 repairs the14-type graph. Current private-graph v4 generation/check/wrapper0; exact generated output62e173ac is composed with only artifact/layout hashes changed, no ABI or bytecode change. Generator causal1 and simulator causal1 reproduce13-versus14 lost types N/R; repaired simulator layout4 passes N/R. Strict build retains coordinator24492/84 spare and append-only layout |
| Evidence Go bindings/readback | Root composition; Terra max concurrent execution | Evidence22 stabi20/reader12 N/R pass. Primary RPC4 new19/union54 N/R pass with causal1PASS/5 expectedFAIL. Combined validator76 N/R now passes exact root/event/source checks after assertion-preserving plain-loop repairs. First75 normal body passed but checker refused positive subtests; malformed selector/outcome attempts remain recorded. Miner/simulator HTTP siblings remain open |
| Simulator companion installation | Astra canonical fixes; root composition; Terra execution | Carry106 has96 normal/95 race passes, nine unrun roots per mode, heavy10 timeouts N/R and08a timeout under race. Separate normal08a CPU diagnostic passed; reviewed fixture-work4 successor is composed, with exact139 qualification pending. Original-fixture causal5 matches both modes; old failures remain. No promotion or full-gate claim |
| Closed-census publication | Root promotion complete; Astra independent review; Terra primary confirmation | New2/17 and retained replica27 pass exact N/R in candidate and primary, with source/dependency/binary fences. Independent review found no blocking issue. It owns the complete signed closure and keys, replays both actual public origins concurrently, publishes content-addressed payload/census/consent metadata with readback, then returns dual-signed ABI calldata. Production authenticated upload provisioning, durable relay/submission and finalized readback remain incomplete; no live publication is claimed |

The candidate is held only while its admitted readers run. Prepare later deltas
outside it. New Go runner source has separate ownership and does not hold the
candidate. All test execution stays with Terra max; failures go to Astra max
with deterministic root/adjacent regressions following `connect/CODESTYLE.md`.

CPU isolation is separate from port/directory isolation. On this host root
verified24 online/allowed CPUs, not hundreds. Earlier Carry28 body processes
plus4 compiles and startup8-body groups omitted explicit body GOMAXPROCS;
preserve those captures and do not infer a product deadlock from their timeouts.
New admitted bodies must record an explicit CPU allowance and share one
cross-mode resource budget. Current cross-package admission permits at most4 light
bodies at GOMAXPROCS2 plus2 compilers at GOMAXPROCS4/-p2:16 CPU lanes total.
Heavy Carry bodies retain the stricter two-total/GOMAXPROCS2/parallel1 profile
without causal compiler overlap. No body waits for an unrelated compile.
This is admission control, not larger test deadlines or a waiver
of any root, failure, complete unsharded release gate or final live requirement.

The checkpoint is not a release-ready certificate. The Go runner's reviewed
54-test result supersedes its earlier48-test-only status, but it does not yet
start private services. Existing qualified ownership adapters remain until an
equivalently tested Go replacement. Preserve actual pre-Go launcher failures;
they are not product test failures or passes. Reuse the frozen explicit-root
adapter's literal path, not a path guessed from a new capture directory.

Next production work is still substantive: promote the qualified evidence
installer, complete and qualify authenticated existing-journal carry, and lock the final
bytecode. The real generated simulator payload is composed into the candidate,
not yet primary. Its earlier current-graph check FAILED on the evidence artifact
hash; the repaired generator now passes the actual v4 regeneration/check and the
exact output is composed; simulator layout/causal qualification is now complete.
The current262-path candidate manifest isd5a18d2c (Carry4/config8/chain10);
the previous247-path manifest wasee46b81b (upload/session);
the preceding242-path manifest was0be0580e (combined Carry/metadata/close).
Gate2 completed on237/07824659, and the original failed semantic startup was
tested on236/feed53e3. Startup custody10+9 N/R
and journal causal4 N/R matched on the earlier207-path manifestbb7e9537.
Current carry97 has assertion failures and timeouts and must not be promoted
as qualified. Its readers released before semantic startup composition; source changes still require an owned
repair handoff. Old source/binary
identities remain preserved, not silently reused. The producer-gate selector
repair now covers the newer evidence/bootstrap/history/boundary/census/chain-
evidence families plus companion Go evidence tests, with13 guard tests passing
N/R. Original-script causal2 also matches N/R; complete gates remain outstanding;
aggregate normal package coverage is not a substitute for either.
Provision/authenticate real activations and both public proof replicas.
The source-reviewed transport direction is typed client-authenticated uploads
using the existing release API sessions, server-owned immutable storage and
explicit upload capacity limits. Do not lend operator artifact keys or MinIO
credentials to validators, or treat mutable API credentials as historical
validator authority. Use a separately funded permissionless relay for the
dual-signed evidence calldata and finalized inclusion checks. Typed transport
and real API-session binding have selected128 SN-side roots passing N/R;
server115/causal4 now have a qualified private JWT/password fixture and fresh
services. Outer upload config8 is composed but unqualified. Real measured
budgets, reserved staging capacity, startup/submission and relay remain open.
Wire V2 startup through native submission, and prove all-pair
capacity. Then both full gates, source freeze, live RC/final epochs and FINAL.md.
No new live campaign or testnet transaction was performed in this work phase.

## Evidence shortcuts

- Head integration: `temp/sn-integration-xOgvEe/capture/`.
- Stats integration: `temp/sn-integration-xOgvEe/capture-stats-v1/`:validator99/
  simulator4 plus affected39+Head12 and simulator2 pass both modes; exact event
  verification and source/binary fences pass. Root verified and promoted28.
- Gate components: ACK `temp/sn-gate-ack-deadline-repair-qualification-v2-jn07pj/
  capture-formatted-v1`; repair4/combined29 and both causal3 controls complete.
- Terminal integration and runner48: `temp/sn-integration-xOgvEe/capture-terminal-v1/`.
- Config/ordinary current: `temp/sn-integration-xOgvEe/capture-config-ordinary-v1/`.
- Contracts: `temp/sn-integration-xOgvEe/capture-evidence-contract-v1/recapture-mutability-v2/`.
- Reader repaired12: `temp/sn-integration-xOgvEe/capture-chain-evidence-absence-v1/`;
  causal preimage/handoff `temp/sn-evidence-reader-absence-v1-THR4lcvj/`.
- RPC4 handoff: `temp/sn-chain-http-bound-v1-DPhza2qn/CHAIN-HTTP-HANDOFF-v1.md`.
- Evidence22 integration: `temp/sn-integration-xOgvEe/EVIDENCE-PREREQUISITES-INTEGRATION-v1.md`.
- Current candidate21: `temp/sn-integration-xOgvEe/INSTALLER-RPC-ACTIVATION-INTEGRATION-v1.md`; live capture `capture-installer-rpc-activation-v1`.
- Successor: `temp/sn-integration-xOgvEe/capture-installer-rpc-activation-v2/successor` (validator76 N/R complete); `capture-storage-layout-normalizer-v2` (gencontracts24 N/R complete).
- Normalizer5/fixture2/adjacent-loop1: `temp/sn-evidence-install-v1-TUBN0C6f/{storage-layout-next,fixture-next,native-timeout-next}`. All8 paths composed; preserve their exact preimages and failed captures.
- Bootstrap2/12: `temp/sn-release-activation-v2-rqGDGa/startup-next/BOOTSTRAP-V2-HANDOFF.md`; exact88 N/R complete in `capture-bootstrap-v2/successor`.
- Regenerated v4: contracts capture `recapture-expectation-order-v1/generated-contracts-current-v4`; generator causal1: `capture-storage-layout-normalizer-causal-v1`. Retain both preflight failures and real body results.
- Installer reviewed source: `temp/sn-evidence-install-v1-TUBN0C6f/EVIDENCE-INSTALL-REVIEW-HANDOFF-v1.md`.
- Activation3 source: `temp/sn-release-activation-v2-rqGDGa/validator/`; exact12 roots in the integration handoff's `ACTIVATION12.tsv`.
- Historical UID35: `temp/sn-integration-xOgvEe/capture-historical-uid-v1`; history57/causal2: `capture-activation-history-v1`. Keep the first assertion failure and race timeout.
- Runner54: `temp/sn-integration-xOgvEe/capture-qualification-runner-module-v3/body-reuse-v1`; causal corrected analysis: `capture-qualification-runner-module-causal1-v3/corrected-analysis-v1`. Reuse the qualified checker, not new ad-hoc census variants.
- Shared simulator binaries/normalizer failure: `temp/sn-integration-xOgvEe/capture-shared-sim-binary-v3`; installer3/25: `capture-installer-shared-v3`.
- History/installer fixture successors: `temp/sn-integration-xOgvEe/capture-history-installer-fixture-v1`; normal57 and installer25 N/R matched. Exact race57 union: `shards-v2/manifests/summary.txt`; all4 groups passed, original full57 race timeout remains recorded.
- Disk2/17 + original-production causal6 N/R: `temp/sn-integration-xOgvEe/capture-disk-state-v2/manifests/summary.txt`; handoff `temp/sn-release-state-v2-tGo6X9hX/DISK-STATE-HANDOFF-v2.md`.
- Closed publication2/17 and retained replica27: `temp/sn-integration-xOgvEe/capture-census-schedule-v1/manifests/summary.txt`; primary confirmation `temp/sn-primary-census-publication-v1/manifests/summary.txt` (exact N/R matched, independent review complete, promoted).
- Native schedule2/9: the same candidate summary and `temp/sn-primary-schedule-confirm-v1/manifests/summary.txt` (primary N/R matched, promoted).
- Simulator layout causal1: `temp/sn-integration-xOgvEe/capture-storage-layout-causal-sim-v2/manifests/summary.txt` (exact expected failure N/R, original-production overlay only).
- Startup custody6: `temp/sn-startup-history-v2-NC2FVF55/STARTUP-CUSTODY-HANDOFF-v1.md`; `temp/sn-integration-xOgvEe/capture-startup-custody-v1/manifests/summary.txt` (exact10+9 and original-journal causal4 matched N/R). The same Astra lane owns semantic history/current-cursor recovery and the selector-coverage repair.
- Carry14: `temp/sn-evidence-install-v1-TUBN0C6f/carry-next/CARRY-HANDOFF-v1.md` plus `carry-custody-next/CARRY-CUSTODY-HANDOFF-v2.md`; `temp/sn-integration-xOgvEe/capture-carry-custody-v2/manifests/summary.corrected.txt` (repaired70 failed; causal1+2 matched N/R). Four initial pre-body proof-path failures and a metadata-recording error remain retained; corrected neutral executions reused the original compiler-issued binaries and absolute proofs, independently reviewed.
- Adjacent campaign reader2: `temp/sn-campaign-evidence-custody-v1-eDT2Hz8j/CAMPAIGN-CUSTODY-HANDOFF-v1.md` (composed; focused5 and original-source causal3 pass N/R, retained campaign failures remain). Proof/campaign capture: `temp/sn-integration-xOgvEe/capture-proof-campaign-v1/manifests/final.machine-summary-v1.tsv`.
- Root proof-store join2/9: `temp/sn-release-proof-state-v2-6Wn19mJS/PROOF-STATE-HANDOFF-v1.md` (independently reviewed; new9+retained14 pass N/R in the same proof/campaign capture).
- Carry recovery14: `temp/sn-carry-recovery-repair-v3-1hMICsnh/CARRY-RECOVERY-HANDOFF-v3.md`; `temp/sn-integration-xOgvEe/capture-carry-recovery-v3/` (selected97 failed; causal5+1 matched N/R; every timeout and unrun root retained).
- Combined repair13: `temp/sn-integration-xOgvEe/capture-carry-metadata-close-v1/` (242-path source0be0580e). Metadata17/runtime6/startup23/disk24/retained42 and causal config2/disk7/fixture5 match N/R. Carry light44 passes N/R; `carry/{normal,race}/10-campaign-heavy1/` retains both120s timeouts. Full Carry106 qualification remains incomplete.
- Semantic startup10: `temp/sn-startup-history-v2-NC2FVF55/STARTUP-SEMANTIC-HANDOFF-v1.md` (original23 now pass N/R after metadata5). This authenticates Stats/cursor/reference recovery, not historical head/deposit/weight decisions.
- Gate evidence2: `temp/sn-integration-xOgvEe/capture-gate-evidence-v1/` (exact13 guards and physical-original-script causal2 match N/R; full gates remain outstanding). Runtime mirror: `temp/sn-gate-runtime-causal-v1-H6Xk3Jyn/RUNTIME-CAUSAL-HANDOFF.md`.
- Authenticated upload15/39: `temp/sn-attempt-upload-v2-fc9kAOv3/ATTEMPT-UPLOAD-HANDOFF-v2.md`; session binding4/10: `temp/sn-release-transport-v2-zdkQvRbH/RELEASE-TRANSPORT-HANDOFF-v2.md` (source-reviewed/composed; SN-side128 pass N/R, server115 pending).
- Current composition22: `temp/sn-integration-xOgvEe/COMPOSITION-CARRY-CONFIG-CHAIN-v1.md`; current capture `capture-carry-config-chain-v1` (SN262 d5a18d2c, server21 985805e9).
- Carry4: `temp/sn-carry-fixture-work-v5-RaVAPAAG`; outer config8: `temp/sn-upload-config-outer-v2-KzsxkMbv/UPLOAD-CONFIG-HANDOFF-v2.md`; chain10: `temp/sn-decision-eligibility-v2-mL6LE8Lg/DECISION-ELIGIBILITY-HANDOFF-v2.md` (all reviewed/composed, qualification pending).
- Private Go fixture12: `temp/sn-private-jwt-fixture-v2-B7bffcho/capture-terra-v1`; exact normal/race positives, original-source causal2 and actual tool/source/mode evidence. Primary scripts/server-fixture has the qualified main.go/main_test.go bytes; primary confirmation pending.
- Reserved staging implementation: `temp/sn-attempt-reserved-v2-r32pLEu2` (unreleased/unqualified); root's relay work: `temp/sn-evidence-relay-v2-DiaqewsI` (unreleased/unqualified).

Use compact machine-verified stage summaries for ordinary updates. Retain full
logs, failed captures and immutable command/source identities on disk; open
them for failures, suspicious signals and independent review. Do not repeatedly
reload or restate the entire historical handoff. Neither compact reporting nor
development sharding replaces complete release-gate or on-chain evidence.
