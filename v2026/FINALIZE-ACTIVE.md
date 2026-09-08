# Release 1.0 active work index

Updated 2026-09-08 20:05 UTC. Read this index first; use
[FINALIZE-COMPLETE.md](FINALIZE-COMPLETE.md) for detailed history and evidence.
This is not a source freeze, full-gate certificate or live acceptance report.

Base commits before this checkpoint: SN `c6d93e9`, server `0f5e7f5e`,
connect `58157ab`. The current checkpoint includes the primary naming changes,
the private Go server-fixture tool, monitoring policy and these handoffs.
The fixture source matches its qualified donor bytes; primary-tree execution
confirmation remains pending. Isolated production successors are not promoted
or included in the primary commits merely because their handoffs are recorded.
The SN source checkpoint includes the independently reviewed
closed-census evidence publisher and its17 tests. Subsequent integration changes
below remain in the temporary candidate unless explicitly marked promoted.
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

## Current pre-soak frontier

This section supersedes older pending statuses below.

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
- One genuine V8 failure remains: finishing a short-lived Rpc owner also
  canceled longer-lived retained startup files. The frozen custody2 fix
  separates those lifetimes while preserving parent cancellation, closed-owner
  refusal and joining active reads. Its four regression tests are unqualified.
- Batch26, quota13, population stress1, custody2, registration cohort6 and the
  later source guards are frozen for the next composition. No V9 source has
  yet been installed. The storage stress materializes **296 MiB**, not the
  earlier 72 MiB proposal; its separate 339,968-slot test is metadata coverage,
  not a claim to have materialized the full 66.25 GiB allowance.
- Registration cohorts preserve per-client Sql/signature/publication and use
  two real operator owners for the 1,000-client test. Durable registration
  readiness/retry is still being investigated: a transport acknowledgment is
  not proof of a stored and publicly readable registration.
- Remaining before final soak: compose and qualify these successors and the
  unresolved adjacent families; prepare a clean, pinned, pushed candidate;
  pass both genuine full gates; then complete real setup and the approved
  accelerated live release-candidate phase. Independent offline analysis may
  overlap capture, but is required before final acceptance. Public Rpc remains
  selected; waiting for private-node sync is not a launch dependency.

Frozen donor locations and exact hashes are in the newest section of
FINALIZE-COMPLETE.md. Temporary candidates and test receipts remain under
`/home/by/urnetwork/temp/sn-*`; they are not backed up by this primary checkpoint.

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
