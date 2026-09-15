# Sim-testnet finalization report 2

**Current status: in progress; `final_acceptance=false`.** The soak remains
stopped. The approved **6,000-alpha reserve repair has finalized**, and the
latest retained complete census shows **65.5997247163%** reserve share. The
user has approved **205 EVM TAO within 225 total TAO** for the required fleet
renewal; the 37,250-alpha lifetime limit and 6,000-alpha per-repair limit remain
unchanged. Earlier checkpoints below retain their historical approvals and
failures; they do not describe the current repair or allowance status.

The repair transaction is
`0xb6468a8c03886ef4d3c207ba08348b3219267b90d7e569b82d9a81b9da96ebed`,
included at extrinsic index 6 in native block **8,009,634**, hash
`0x201b281662dceed0baf5b2d1efae66373867ffa252565417b8c7fb1d4380dbc3`.
Pinned parent/block storage shows exactly **6,000,000,000,000 alpha-rao**
debited from the source hotkey and credited to the reserve hotkey. The later
census at native block **8,010,632** includes all **256 registered UIDs**:
reserve stake **59,254,248,215,327** divided by registered stake
**90,326,976,937,105 alpha-rao** gives the percentage above. This establishes
the preparation snapshot's reserve target, not the eventual end-of-run share.
[Public RPC requests, responses, transaction inclusion and stake arithmetic](peerreview/evidence/FINAL-2-approved-renewal-and-reserve-20260915/README.md).

The allowance is published in vault commit
`9651a13062af2fd25dcd9e98db8c8871d148d114` and adopted by native plan
`0x922e280318f33cb20f5b15082bb6329890e9d8778f1effabf8baae521a57f4ea`.
Its setup preparation passed all nine hard checks and authenticated all
**3,449 carried actions**. Full native resume preparation subsequently passed
**all 18 hard checks** at **13:58:20 UTC**, including both validator namespaces,
host readiness and runtime inputs; the six recorded campaign-state files
remained unchanged. The native renewal preview passed for **202 fleets**
and **2,020 new actions**, capped at **13.13 EVM TAO plus 0.606 native TAO**;
no renewal transaction was submitted by that preview. Existing keeper/oracle
balances cover those ceilings. Its F388/T419 window is diagnostic only.
The new producer attempt has recorded a capture-cohort race timeout, and
the complete gates are still collecting their independent results. Full
qualification, actual renewal, the complete RC/production campaign, final
accounting and shutdown remain outstanding. These preparation claims are
local artifact evidence; all new chain observations use the owned LAN node
and record **`independent_rpc=false`**.

## Historical checkpoint: September 15, 02:38 UTC

**Historical status: in progress; `final_acceptance=false`.** At **02:38 UTC on
2026-09-15**, the soak remains stopped and the approved reserve repair is
unsubmitted. The runtime configuration-identity correction is qualified. No new
setup plan or transaction has resulted from this local qualification.
All 1,212 finalized renewals, 3,521 action identities, generation 3 custody and
the original signed probe anchor and nonce 34 remain retained.

The migration explicitly preserves the original 455 configuration identity
while requiring the exact reviewed 458 runtime for current authority. The
optional public `config_identity_runtime_spec: 455` pin is also bound into the
new setup plan hash. Existing historical signature and hash comparisons remain
intact; the original signed archives are not rewritten. Source
`85c09583ce8a0728b4b33b04b19c9e7d7ea1272a` completed the 70 selected tests in
both normal and race modes: **69 PASS, one FAIL per mode**. The two adjacent
50-test groups passed. The sole failed root authenticated the original signed
repair, then incorrectly compared plans with different RPC routes: its HTTP
fixture had replaced the approved `OperationalEVM` endpoint after planning.

The one-file fixture correction is
`5a411d2bf57e03c60a6ea3b3998d1cf401877342`. It preserves the approved endpoint,
reproduces the original resolved-input hash, and verifies that changing a
synthetic route changes only the `config.render` action intent. The corrected
root passed **three consecutive fresh normal runs and three consecutive fresh
race runs**, with exact one-test lists, seven events per run and all body,
converter, verifier and outer exits0. All thirteen repository observations
and each mode's binary remained unchanged. The normal binary SHA-256 is
`011e9497babf39ee8760a8fcaed7e3921a2403eac9f2ed92e43d2abeb6f77657`;
the race binary is
`7dc0224ad65db3cacb61172d0b6fab13e14f10d16c3704de0379372035b1c06c`.
The final race owner closed at **02:38:10 UTC**. The other 69 test bodies per
mode are unchanged and retain their original qualified scope. Together these
provide passing scoped coverage of the 70 affected roots per mode across the
two named sources; the failed 85c0958 matrices remain failures.
[Implementation, original failures, corrected confirmations and scoped reuse](peerreview/evidence/FINAL-2-runtime-config-identity-20260915/README.md).
The portable bundle contains680 checksum entries; its manifest SHA-256 is
`09713bbd8a396831b926cda1fe1ed758d7fc2b384fdedea1dfade5e186361fe9`.
Initial event-verifier invocations passed a numeric
status where the existing verifier requires an absolute `body.exit` path.
Replaying the retained events with that operand corrected verified all four
actual outcomes without rerunning any test body.

The separate production control restores only the old runtime hashing. It
reproduces **three expected failures with two passing controls**: body exit1,
converter/checker/outer exits0 and 42 events. Source, dependency, binary and
mutation observations remain equal. Its repair failure occurs at the unchanged
authentication call before the corrected fixture's planning assertions, so this
control retains its scope across the one-file fixture correction.
[Original-hashing control](peerreview/evidence/FINAL-2-runtime-config-identity-20260915/causal-original-runtime-hashing/RESULT.json).

The updated read-only parser built successfully at **01:51:24 UTC**, SHA-256
`5cee8ea3fe7dd23678d8ee9b646433ee2666390ad61cb1ace833caecebf327e6`.
It rendered the successor lock at **01:58:24–01:58:28 UTC**, with 19 equal
repository/library observations and no RPC or campaign-state action. The YAML
SHA-256 is `c437900d8cb2d29ff0d363ef629b88ceb8ace35976bd28bfd194a2d8dc6eb639`.
Only `repositories.sn_go_source_hash` changed; all other lock fields are equal.
The test-only fixture correction does not require another render. Publication,
the canonical executable, actual setup, both complete gates and the real
RC/production soak remain outstanding.
[Parser and exact lock-render receipts](peerreview/evidence/FINAL-2-runtime-config-identity-20260915/final-lock/RESULT.json).

## Historical native attempt: September 15, 01:26 UTC

The runtime455 historical-lock correction is qualified and published at
`eccfae8a5f4176ccaa6099ce5ce555924057f3d3`. Its next actual setup attempt
reached a different retained-history check and stopped without changing the
six recorded campaign-state files or submitting a transaction.

The matching canonical executable built with actual build/outer exits0 at
**01:24:29 UTC**, a genuine clean eccfae8 Git stamp and equal thirteen-repository
observations. Its SHA-256 is
`73264ab8365c6cf538390c53ed0a6a71a39d3972e405b916088559b486218e99`.
Using the same retained campaign and LAN RPC options, read-only setup ran at
**01:25:42–01:26:11 UTC** and exited1 with
`coordinator repair completed source authority: coordinator repair configured
strict domain differs`. It emitted no new plan. The prior archived-lock refusal
did not recur.

The refusing comparison is the configuration identity: changing the public
manifest's expected runtime from 455 to 458 changed ConfigHash. The original
signed repair, probe and activation evidence still authenticates its previous
identity. The explicit configuration-identity correction described above
addresses that comparison while retaining the signed originals, action intents
and budgets. It has not yet been exercised against the actual retained state.

Once that actual failure proved a production successor was necessary, both
eccfae8 full-gate attempts were intentionally canceled before any test phase.
Producer closed143 at **01:32:11 UTC** and aggregate at **01:33:33 UTC**. Both
passed source-freeze, source-integrity, binding-toolchain and runtime-source
preflights, then were interrupted during runtime-metadata attestation. Both
owners and their private process records are gone. These are recorded
cancellations, not passing full gates or executed-test failures.
[Canonical build, native refusal and gate cancellation evidence](peerreview/evidence/FINAL-2-eccfae8-native-config-20260915/README.md).
The 123-entry portable manifest SHA-256 is
`b6b1dfc4fa43778945fe3c4d1910d5c69bf2ee96654cc2d6c74ad761ab13ce35`.

## Historical checkpoint: September 15, 01:15 UTC

**Current status: in progress; `final_acceptance=false`.** At **01:15 UTC on
2026-09-15**, the soak remains stopped and the **6,000-alpha repair remains
unsubmitted**. The next actual setup preview exposed a historical-evidence
admission defect in the simulator. It did not change the six recorded campaign
state files or submit a transaction. The **1,212 finalized renewals**,
**3,521 action identities**, generation3 custody, original signed probe anchor,
nonce34 and existing spending approvals remain preserved.

The canonical `c572d993` executable built successfully at **00:19:36 UTC**,
with an authentic clean Git stamp and matching thirteen-repository observations.
The actual read-only setup ran at **00:24:42–00:24:56 UTC** and exited **1**:
`validator evidence original release lock: release lock runtime identity is not
the reviewed testnet runtime 458 release`. No successor plan was produced.
The archived validator-evidence lock had already matched its original approval
hash, but the historical reader then applied the current-runtime validator.
The retained v12 source archives use the exact reviewed **455** identity;
current activity uses **458**. This is a simulator compatibility failure, not
evidence that the node stopped syncing.
[Build, actual preview and unchanged-state receipts](peerreview/evidence/FINAL-2-native-history-458-20260915/README.md).

The narrow correction is frozen at
`3ffc1277d1acd1e21b908c3b2cfd37612602e07b`. Archived companion locks admit
their exact reviewed original455 or current458 provenance and retain the common
structural/build checks. Current lock rendering, final anchors and new runtime
authority remain458-only. Seven new deterministic regressions cover the actual
archive and persisted-plan readers, original payloads/actions/budgets, adjacent
lineage readers and altered approval/runtime rejection. **All 36 affected tests
pass normally and with race detection.** Both actual archive/restart readers
also pass three consecutive normal executions on the same source and binary.
The normal binary SHA-256 is
`dc3b2ed96df3cfec634ee481b1d4494b51dff2683230a146c3ae3f17b64046dd`;
the race binary is
`dce59c07d098f66c0b99a7a7a5c3a6e397a900fd61c52d900e2d7072f080f740`.
The final race owner closed at **01:13:12 UTC** with all thirteen source and
dependency observations and its binary unchanged.

In a separate normal control, restoring only the old production dispatch
reproduces **both original reader failures**, while the static-validation and
current-authority controls both pass. Its actual body exits1 with exactly
2 FAIL/2 PASS; event verification and the enclosing owner exit0. This is an
expected-failure control, not a passing old implementation. Initial control
setup attempts referenced the wrong handoff path and omitted the existing
owner's `capture_root` input; both refusals occurred before compilation and
are retained. No new test or source correction was needed for those operands.
The earlier181-root runtime qualification retains its original scope.
[Affected qualification, confirmations and causal evidence](peerreview/evidence/FINAL-2-native-history-458-20260915/README.md).
The combined portable bundle contains382 checksum entries; its manifest SHA-256
is `ff16207e3a05ef65785c32d94896648c765bcb58f1ad2967e236a47363158238`.

The two `c572d993` full-gate attempts also ended before phase admission.
Both passed source-freeze, source-integrity and binding-toolchain preflights,
then failed the runtime-source preflight with curl exit **28** while downloading
upstream source from GitHub. Producer ended at **00:21:09 UTC** and aggregate
at **00:23:59 UTC**; neither started a test phase or service owner. At **00:31 UTC**,
the exact failed source URL returned HTTP200 and its bytes matched the pinned
manifest. Exact local upstream checkouts are prepared for the next primary
source audits. These observations do not pass the failed gates.
[Closed preflights and recovered source-fetch evidence](peerreview/evidence/FINAL-2-native-history-458-20260915/README.md).

The existing executable rendered the successor lock read-only at
**00:54:33–00:54:37 UTC**, with all nineteen repository/library observations
equal. Its YAML SHA-256 is
`5b8c412454adfde72f2cf51c682b5ce4b9b335a2095eaafc21bf4e92a153a4ab`.
Only `repositories.sn_go_source_hash` changes from the previous lock, binding
the historical-reader correction. All contract, runtime, protocol, node/gateway
and other dependency fields are equal. The exact lock was integrated at
`628f29a`; it has not changed the retained campaign plan.
Coherent publication, a matching canonical executable, both
complete gates and the real RC/production soak remain required.
[Successor lock and render receipts](peerreview/evidence/FINAL-2-native-history-458-20260915/final-lock/RESULT.json).

## Historical checkpoint: September 15, 00:09 UTC

**Current status: in progress; `final_acceptance=false`.** At **00:09 UTC on
2026-09-15**, the soak remains stopped and the approved **6,000-alpha reserve
repair remains unsubmitted**. All **1,212 finalized renewals**, **3,521 action
identities**, generation3 custody and the original signed probe anchor with
nonce34 are preserved. The approvals remain **37,250 alpha lifetime** and
**180 EVM TAO within 200 total TAO**. No new campaign transaction was submitted
during this qualification.

The fresh owned-node check at **00:03:15 UTC** returned HTTP 200 for both
direct-LAN batches, **16 peers**, `isSyncing=false`, runtime **458/1/1** and
chain **945**. Finalized block **8,007,358**, hash
`0xb6cd48185f3b1587ad9d9a071b6712763d4b50150a7c7f17782d8e6b008c60a9`,
is later than the previous observation at block 8,006,843. Subtensor is serving
requests and advancing finality. The stale deployment expectation of runtime
455 was corrected in xops; **the corrected deployment assertion has not been
rerun on the node**. These observations use `192.168.1.162:9944`, without
pacing, retry, proxy, redirect or public fallback; `independent_rpc=false`.
[Exact requests and raw replies](peerreview/evidence/FINAL-2-runtime458-qualification-20260914/lan-health-20260915T0003Z/).

The composed runtime correction now has **181 selected top-level tests passing
normally and under race detection**: 100 CRV4, 15 miner and 19 validator tests
on `df98472`, plus 47 simulator tests on `ade970a`. The relevant client
source is unchanged between those revisions. Required fresh-process
confirmations are complete. An earlier simulator run actually failed two of
41 tests: its semantic inventory still named the five-version test, and its
RPC mock response/request files overlapped the helper's scratch outputs.
Correction `ade970a` separates those fixture paths, updates the inventory and
retains stricter malformed-response and infrastructure-scope controls. The
original **39 PASS / 2 FAIL** output remains preserved; replaying its events
does not turn that run into a pass.
[Qualification scope, raw outcomes and retained failures](peerreview/evidence/FINAL-2-runtime458-qualification-20260914/README.md).

The causal checks for the old five-identity capacity, stale gate inventory and
public/paced artifact route all produced their expected failures with passing
adjacent controls. The 13-test production-policy causal also completed with
exactly **seven expected failures and six passing controls**, across CRV4,
miner, validator and simulator. Restoring the old current-runtime assumptions
reproduces rejection of runtime458 while the unaffected controls still pass.
All four compiled binaries, the exact six-file mutation and dependency
observations remained stable. The earlier setup and shell-parse refusals are
preserved separately; neither started a test body.

Both original FC908 full gates have now closed with exit **1**. The producer
passed **36 of 37 phases**; its typed-prior scheduling correction remains
qualified as recorded below. The aggregate closed at **23:33:25 UTC on
September 14**, passing **24 of 25 phases** and its final source/release-lock
checks. Its single failure came from four obsolete positive nginx quota
assertions in the xops vulnerability test. Correction
[`42bfe0b`](https://github.com/urnetwork/xops/commit/42bfe0be2a7a7c51bbda87fb44886424604f509e)
is pushed to xops main: its complete **17-test module**, two additional
**seven-test** confirmation processes, and the old-assertion causal control
completed with the expected results. Earlier projection errors remain in the
evidence. The aggregate now explicitly retains both gateway regression tests.
**Neither failed full gate establishes release acceptance.**
[Closed aggregate and xops correction](peerreview/evidence/FINAL-2-runtime458-qualification-20260914/README.md).

The reviewed final release-lock YAML has SHA-256
`d11b2a41ca6e836f9267088f8899c4fb0faf53b78b3b9ca8804bab589cd63e7b`
and is committed in the idle integration checkout at `2b907a4`. Relative to
the earlier runtime-458 lock, only `repositories.protocol_source_hash` changes,
binding the aggregate's added gateway regression. All contract, runtime,
production Go, node/gateway and other dependency fields remain unchanged.
The existing readonly CLI rendered this lock with matching before/after
observations; it was not rebuilt. This lock has not been applied to the
retained campaign. Publication, a canonical stamped CLI, both replacement full
gates, the retained-state setup transition and the actual RC/production soak
remain required.
[Exact final lock and render receipts](peerreview/evidence/FINAL-2-runtime458-qualification-20260914/final-lock/).

## Historical checkpoint: September 14, 22:57 UTC

**Status: in progress; `final_acceptance=false`.** This report covers the next
full finalization of testnet chain **945**, subnet **521**, under
[FINALIZE.md](../FINALIZE.md). At the **22:57 UTC on 2026-09-14** observation
cutoff, all **1,212 renewal transactions** across **202 fleets** remain
finalized. The approved **6,000-alpha reserve repair remains unsubmitted** and
the soak remains stopped. The lifetime approvals remain **37,250 alpha** and
**180 EVM TAO within 200 total TAO**. No transaction was sent during the runtime
adoption work below.

The owned LAN node is healthy. The operator reported that nginx's effective
configuration had no RPC rate-limit directives and its configuration check and
reload both succeeded at **21:01:55 UTC**. This supersedes the undeployed status
at the earlier checkpoint below; the remote deployment log itself was not read
by this agent. A fresh direct-LAN observation ending at **22:20:21 UTC** found
**16 peers**, `isSyncing=false`, runtime **458/1/1**, chain 945 and finalized
block **8,006,843**. The reported deployment failure expected runtime **455**;
it was not a failure to sync or finalize blocks.
[Operator provenance and raw LAN evidence](peerreview/evidence/FINAL-2-runtime458-20260914/README.md).

At finalized block **8,006,567**, hash
`0xc814242904668bad31b388b36ed31a0c1ffd3b180b173727f5e3d20ea5c8aba4`,
the exact on-chain Wasm has BLAKE2b-256
`2fdb28e5c3fe4e79844b25dee09ed960e90004432ea2bd98079aba4c5530c51a`
and metadata BLAKE2b-256
`040088e73e34ed5561372aa51b07b56e41cf7f390312837b074434f30452593d`.
Executing that exact captured Wasm offline reproduced the metadata bytes.
The upstream source review uses exact commit
`a7ae07e5dd37b552f27aa8e4d7716c522eef9aa7`; it claims no release tag or mainnet
proposal. The authenticated upstream CI Wasm is **not byte-identical** to the
LAN artifact: the reviewed difference is 22 constants in one hash-state
initializer, consistent with compile-time random seeds. Only the exact LAN
artifact is admitted. Fresh evidence uses our own node and is **not an
independent public-node replication**; report 1's historical independent checks
retain their original scope.
[Captured replies and bytes](peerreview/evidence/FINAL-2-runtime458-20260914/lan-artifacts/RESULT.json),
[offline execution](peerreview/evidence/FINAL-2-runtime458-20260914/offline-probe/RESULT.json),
[compatibility review and provenance limits](../docs/spec/runtime-458-audit.md).

The expected-runtime correction is published on
[xops main at 446cbdb](https://github.com/urnetwork/xops/commit/446cbdb56e0dc5004b66d7e0cbf05a4d9c49224c).
Its complete **30-test module passed**, and both affected controls passed two
additional fresh processes. Restoring only the old expected-runtime value
caused the expected runtime-control failure while the adjacent network/backend
control passed. The node image, chain identity, ports and nginx policy are
unchanged. **The corrected deployment check has not been rerun on the node.**
[Raw results and exact causal mutation](peerreview/evidence/FINAL-2-runtime458-20260914/xops-qualification/SUMMARY.md).

The FC908 full producer closed at **21:28:46 UTC**, exit **1**, with **36 of 37
phases passed**. Its sole failure was the ordinary capture race package reaching
its ten-minute cumulative deadline while the typed-prior 32 MiB boundary test
was active. Correction `907d18698f3464da8193894a3ad42101184a6a56` gives that
boundary its own process with the existing workload and deadline. All **26
affected roots passed normally and under race**; the formerly active boundary
passed three consecutive fresh race processes on unchanged binary bytes.
Restoring the old script produced the expected guard failure and two passing
adjacent controls. These completed results are reused within their recorded
scope. The original FC908 aggregate is still running: the retained partial
observation contains **20 passed phases**, no failed phase and no final verdict.
[Failed full producer](peerreview/evidence/FINAL-2-producer-fc908-20260914/README.md),
[completed correction](peerreview/evidence/FINAL-2-typed-prior-907-20260914/README.md),
[explicitly partial aggregate log](peerreview/evidence/FINAL-2-runtime458-20260914/aggregate-fc908-partial.stdout).

Runtime-458 client source `df98472bd88dc8e29856c172fba4731d52a6308d` admits the
new exact current artifact while retaining 451–455 only for historical
evidence. It also corrects the shared metadata/authority limit from five to
**six exact identities**, including the actual simulator history constructor,
all six hot entries and seventh/duplicate/incomplete rejection. Its **175-root
affected qualification remains in progress**; implementation and successful
compilation are not recorded as passing tests.

A readonly runtime-458 CLI built successfully on that source. Its complete
thirteen-repository before/after observations match. The reviewed combined
release lock has SHA-256
`e72b2146a1cbbd59a54b0424a30478aa9a475f8d82369c028c4fe22e8f2d71c2`:
six runtime fields, two SN source hashes and the node configuration hash changed
relative to FC908. **All contract hashes and other dependency fields are
unchanged.** The exact YAML is committed only in the idle integration checkout
as `151b515be82cedd02cdae7346b9340cb2cef91aa`; it has not changed the active
campaign plan or the source under qualification. A final canonical stamped CLI,
both complete release gates, the retained-state setup transition and the actual
RC/production soak are still required. The earlier FC908 stamped CLI remains
historical build evidence and cannot authorize runtime-458 writes.
[Renderer and combined lock evidence](peerreview/evidence/FINAL-2-runtime458-20260914/README.md),
[earlier stamped CLI and retained build refusals](peerreview/evidence/FINAL-2-final-cli-fc908-20260914/README.md).

## Historical checkpoint: September 14, 18:56 UTC

At that observation cutoff, all **1,212 renewal transactions** across **202 fleets** had finalized
and passed their native postcondition checks. The approved **6,000-alpha repair
has not been submitted**, and the soak remains stopped. Earlier published candidate
`7eab04905dbc274ae6d2546b6802f1097c1fc3fe` includes the qualified historical
restore correction, runtime-455 battery correction, probe replacement path and
stable plan-approval checkpoint. Its clean release executable has built
successfully. That approval-checkpoint correction passed all **40 affected tests normally
and under race detection**, including five new deterministic regressions.
Three fresh normal executions close the advancing-finality confirmation, and
restoring the original defect produces the expected failure with three passing
controls. Earlier failed and canceled attempts remain recorded below.

The new preview completed at **15:25:40 UTC**. Approved setup admitted the exact
reviewed plan
`0x2b5527989bd2ca38b58d3046e82f2d7fdb5ab1b5b86f3af74ba58e5957ced95d`.
It then stopped on an nginx HTTP 429 at **15:47:17 UTC**. An identical-plan,
identical-build retry stopped on another 429 at **16:09:25 UTC**. Neither sent
a new transaction. The canonical owned-node gateway template contains matching
request and connection limits. Their isolated removal passed the related
**29-test module**, three executions of both new deterministic regressions, and
the expected old-template failure control. The exact qualified correction is
published on [the owned-RPC correction branch](https://github.com/urnetwork/xops/tree/sim-testnet/owned-rpc-unpaced-20260914)
and is now included in [xops main at d33d417](https://github.com/urnetwork/xops/commit/d33d4173767ad9dab2f301bc0b0e040579bee896).
The Subtensor files match the qualified correction exactly.
It is **not deployed**: the available SSH key was rejected, and the deployed
configuration has not been inspected.
No further setup retry is planned before the gateway correction.

The full producer closed with exit **1** at **17:30:39 UTC**: all **36** phases
joined, with **34 passes and two failures**. Its final source and release-lock
checks passed. Strict Solidity compilation rejected a narrowing cast in the
runtime-455 regression test before any Solidity test body ran. The simulator
evidence race package also exceeded its unchanged ten-minute deadline after a
new full-population renderer had been added to its ordinary group. The obsolete
aggregate was stopped and joined at **18:49:42 UTC**, with outer exit **143**:
**14 phases passed**, the same strict Solidity lint failed, and two Connect
phases were canceled. All known owners and workers were gone; its final source
fence was not reached. Full producer success remains a traffic launch
prerequisite, and both complete gates are required for
acceptance. Successful focused checks, renewal and historical payments do not
establish full acceptance.
[Closed producer, exact phase results and original failure locators](peerreview/evidence/FINAL-2-producer-7eab-20260914/SUMMARY.md).

The isolated correction `1e491b61a841c652f3155152bf845c519e2c171a` changes only
the probe test, producer scheduling script and scheduling regression tests.
The corrected probe source passed the strict 85-file build and all **215**
contract tests across **18** suites, including **19** probe tests. Two additional
fresh strict-lint processes passed; restoring the original assertion reproduces
the unsafe-typecast failure. New synthetic controls reject short or long
responses and a change to any byte of the known answer. Generated payload
comparison and binding verification passed. The earlier binding-check refusal
is retained: temporary checkout file permissions differed from the canonical
checkout and were corrected without changing their contents.
[Strict build, 215-test result, causal lint failure and adjacent controls](peerreview/evidence/FINAL-2-probe-reference-lint-20260914/SUMMARY.md).

The renderer receives a separate normal/race process with the same ten-minute
limit. Both compiled inventories confirm **193 roots: 190 ordinary, two
existing slow roots and one renderer**; the two child-package generator tests
retain separate Solidity-phase coverage. Source-derived guards reject dropped,
duplicated, conditional or broadened owners and identify future full-launch
wrappers through helper calls. The 18-root guard matrix then found an adjacent
source-census defect in both modes: **16 passed and two failed**. An older text
scanner counted `TestRuntimeEvidenceSyntheticDirect` inside a fixture string
as an executable declaration. The parser-based correction and four deterministic
literal/declaration/build-file controls are committed in clean source
`713eae3fcf83615d81d185fda3f3ec42a441a26b`. All **81 affected tests passed
normally and under race detection**. Both failed guards completed three fresh
sequential passes in each mode. All four original timeout roots completed
three sequential race passes on the same corrected binary; the slowest root
took **303.93, 435.95 and 394.89 seconds**. Source, dependency and binary checks
matched before and after every accepted execution.

Restoring only the old declaration scanner produced the expected **three
failures and three passing controls**. Restoring only the old scheduling script,
using the retained normal binary, produced **three failures and one passing
control**. The earlier `1e491` timeout body's four raw passes remain distinct
from its failed capture-metadata checks and do not count toward the completed
`713eae3` streak. Compiler, patch-direction and list-adapter admission refusals
are retained separately from actual test failures.
[Corrected matrices, confirmations, causal controls and obsolete aggregate closure](peerreview/evidence/FINAL-2-evidence-render-owner-qualification-20260914/README.md).

Production Go, contract artifacts, the installed approved plan and completed
renewals remain unchanged. The reviewed successor release lock was rendered
locally with no RPC or native-state mutation. Its only changes are the protocol
source hash for the producer scheduling script and both gateway/node configuration
hashes; the YAML SHA-256 is
`76cd7fa7031ee5566301173a94e1a4369ccbc871542de05119b46bbcb1461f35`.
All sixteen repository/library snapshots and the original lock were stable during
rendering. This prepares the next canonical build and complete gates; it does
not establish deployment, a full gate PASS or a soak result. Applying the new
release requires a reviewed successor setup hash preserving the completed work
and approved limits.
[Exact three-field lock diff, source snapshots and retained layout refusals](peerreview/evidence/FINAL-2-infrastructure-lock-20260914/README.md).

The earlier published candidate
`a59294e98ea02d05125015ae02cf32f2c0059c8a` introduced the corrected strict
runtime render and the renewal oracle predicate. An earlier defect treated a
completed oracle restoration as a pending reroute: at finalized EVM block
**8,002,200**, the active, immutable and stored pending oracle were the same
original address, with effective epoch **273** already past at current epoch
**356**. The contract retains its scheduled fields after activation. The
correction accepts this completed state while preserving rejection of foreign,
future or inconsistent schedules. All seven affected roots passed normally and
with race detection; actual outer completion times were **07:01:39** and
**07:13:15 UTC**. The final executable built with clean VCS revision `a59294e9`,
SHA-256 `998777328adfad2c394a3aec9c9df92f3bb477d3c49a7bc509d9457251fc4efd`,
and identical before/after observations across all 13 repositories.
[Pinned oracle state, patch, complete focused outputs and final build evidence](peerreview/evidence/FINAL-2-oracle-renewal-preparation-20260914/README.md).

The render correction `cf4eae3` also passed its seven affected roots normally
and with race detection. The original plain-LAN WebSocket admission failure
and a separate test-binary working-directory failure remain retained; the latter
subsequently passed three consecutive executions on the same binary. The
superseded e312 full gates were canceled at **07:00:33 UTC**, with actual outer
exit **143**: producer **13/13** admitted owners joined, including three canceled
owners; aggregate **5/5** joined, including two canceled owners. These are not
full-gate passes. Their complete outputs, original refusals, correction and
qualification are preserved in the
[render convergence bundle](peerreview/evidence/FINAL-2-render-convergence-20260914/README.md).

The a592 producer and aggregate gates both executed a stale test-census
failure, then were canceled at **08:33:56–57 UTC** with outer exit **143**.
All **26/26** producer and **7/7** aggregate admitted children joined; canceled
children remain distinct from the actual failing phases. Neither gate passed.
The `43cc2b2` correction replaces stale open-family counts and also fixes the
adjacent evidence-family count. Its original census root completed three normal
confirmations; deterministic growth controls reproduce the pre-fix failures.
A separate nested Go-tool enumeration timeout led to `ff62e1c`, which lists the
already compiled test binary. That correction passed **24/24 normal and 24/24
race tests**, plus three fresh normal confirmations of the failed root. The
old binary fails immediately without Go on PATH, proving the removed toolchain
dependency without relying on a timeout. These are scoped correction results.
[Complete failed gates, deterministic controls and focused results](peerreview/evidence/FINAL-2-census-correction-20260914/README.md).

The native renewal completed with exit **0** at **08:56:04 UTC**: **202 native
commitments, 202 EVM mirrors and 808 bindings**, for epochs **359–390**. The
[closure and transaction index](peerreview/evidence/FINAL-2-renewal-finality-20260914/closure/operations-index.json)
records each transaction hash, finalized block/hash, finality and verification
journal positions, and postcondition identity. This closes the native receipts;
it is not a second independent audit of all 1,212 transactions. The first native
transaction `0x9be9f4334b44d9c6f4bcddf2285be953e100c6980b63a24432c9c5b583075ce7`
has a separately reproduced canonical inclusion at block **8,002,458**, index
**14**, block hash
`0xc03a239da6e042b4bfc583854b38e15b7efd2257dbb8ab41f4f4f4a9274b6d3b`.
[Native results, evidence scope and first-inclusion RPC](peerreview/evidence/FINAL-2-renewal-finality-20260914/README.md).

The queued 6,000-alpha repair started after renewal completion and exited **1**
at **09:10:02 UTC**, before any transaction or runtime-state change. For
`fleet.mirror.26`, serial historical replay added three modern batch-provenance
fields to the legacy observation. Adding exactly those fields reproduces the
recorded mismatch: expected
`0x10cd341a9cb9993407584399a270792f9cc89fe8cf600d709d2447fbbc165479`, replayed
`0x7003cbdd3e896e2b69d5fff99bb1c8137c53e868f0858f5e827a7ad24e658ae3`.
Pinned LAN state at EVM block **7,900,891** matches the historical receipt. The
correction preserves the recorded format for both mirror and member-binding
aliases while retaining exact observation hashes, canonical checkpoints and
both observers. [Exact native refusal and unchanged-state proof](peerreview/evidence/FINAL-2-renewal-finality-20260914/repair-refusal/RESULT.json).

The first correction's normal run exposed a separate fixture error: the fake
RPC decoded `data`, while the pinned client encodes contract calls as `input`.
That run retains **80 passes and 6 failures**; its canceled race run is not a
qualification pass. The corrected source `4029396` adds a direct wire-format
regression and passed **87/87 normal tests**. Each of the six failed roots also
passed three fresh normal processes. The combined 87-root race run exhausted
its unchanged package deadline after **85 explicit terminal passes**, leaving
two metadata roots unresolved. Those two roots subsequently passed three
fresh race processes on the same binary, ending at **10:19:30 UTC**, with all
source, binary, list, execution and conversion checks passing. The original
timeout remains a failed run. A disposable pre-fix variant reproduces the
codec error and both historical replay failures while its modern-format
control passes. Adjacent checks cover member bindings, both observers,
partial provenance, changed checkpoints and state, shared providers, and the
separate batch codec. These are scoped regression results; the final complete
gates remain required.
[Deterministic reproductions, adjacent coverage and focused qualification](peerreview/evidence/FINAL-2-mirror-qualification-20260914/README.md).

The qualified production and test bytes are included in published `abae9a6`.
Its final native executable has SHA-256
`71fca036f0aa6876565fbc38f1d138f0ed2268e19f9488d280eaf95c49ae191e`,
with clean VCS revision `abae9a6` and identical observations across all 13
repositories. The source pair has SHA-256
`f7871bf5e6b53a939b8ea8add2b2b3abbbfd0bf58f9c1bdc3474fd42248b0089`.
The reviewed replacement plan
`0xf6348cfdb032e658b8ce46d764ab7c65476250959fdbabc2a658563759e17585`
preserves all **3,521 actions**, the **1,212 completed renewals**, and all
spending limits. The repair remains exactly **6,000 alpha**, with minimum
destination credit **5,999,999,999,999 alpha-rao** and lifetime allowance
**37,250 alpha**. Adoption and historical replay do not establish a finalized
transfer or a passing reserve census.
[Final build, native lock, publication and plan review evidence](peerreview/evidence/FINAL-2-final-build-20260914/README.md).

The replacement repair attempt ran from **10:15:46 to 10:32:15 UTC**, exiting
**1** after completing the batched **1,000/1,000** historical checks and reaching
the **950/3,456** carried-action progress marker. For `fleet.commitment.34`,
historical native state and hashes succeeded, but EVM replay then required its
generation-1 commitment to remain current. The replay executor had narrowed
consumption lookup to the original plan: the consumer failed there, then the
same consumer intent finalized and verified in an approved descendant plan.
That valid later completion was invisible to the narrowed lookup. Only native
plan/config admission changed; the journal, supervisor state/config and public
identities remained byte-identical. The accepted plan hash and every action
match the reviewed preview; generation time and live observations differ.

The adjacent review examined **802 generation-consumer relationships** across
all **202 fleets** and identified the same defect in **17 generation-1
commitments**: fleets **34–40 and 91–100**. Every affected consumer has its exact
intent verified in the approved current ancestry. At the earlier **10:38 UTC**
cutoff, the correction and its regressions were still pending. The canceled
`abae9a6` producer retained **13/13 child joins**
(10 successful, 3 canceled); aggregate retained **3/3 joins** (1 successful,
2 canceled). Both outer exits are **143**, and neither is a full-gate pass.
[Native refusal, exact state changes and adjacent-consumer review](peerreview/evidence/FINAL-2-commitment-scope-refusal-20260914/README.md).
[Closed gate commands, complete joins and cleanup](peerreview/evidence/FINAL-2-abae-gates-canceled-20260914/README.md).

The qualified correction preserves the original replay plan, observation and
receipt hash. If that original scope has no completed consumer, an immutable
scope from the current approved plan can authenticate the exact consumer for a
fleet whose renewal has completed. It requires the same deployment, approved
ancestry, unchanged action intent and the persisted verified receipt. It does
not accept a different consumer through an accepted-prior-intent alias. The
adjacent review also examined **1,466 verified historical actions**, finding no
source-intent, persisted-receipt-identity or successor-order discrepancy. The
**14 unverified future lifecycle actions** are outside that completed-history
review. These graph checks use retained artifacts; they are not another live
RPC replay.

Source `6271dcfceb1af8257b626aba79a6864eb3b7c986` completed a disjoint **33 + 69
test** selection in both modes: normal execution ended by **11:00:41 UTC**, and
race execution by **11:11:27 UTC**. All seven new top-level regressions passed.
The constructor-path regression also passed three consecutive fresh normal
processes on the same source and binary. A disposable variant restoring the
entire old consumption predicate produced the expected **two failures and two
passing controls**. Synthetic cases cover authorized descendant completion,
missing or tampered receipts, foreign plans and deployments, changed or aliased
intents, missing renewal successors, both readers and adjacent action families.
These are scoped correction results, distinct from the subsequently canceled
full gates.
[Deterministic reproductions, exact test outcomes and adjacent review](peerreview/evidence/FINAL-2-commitment-scope-qualification-20260914/README.md).

The same qualified Go bytes were published in `84b6ca6`, followed by the
release-lock-only commit `afd7b26`. The final executable has SHA-256
`dc516d630ace555889e7c0705381ff0ed1d305824f45ad18ddf8d825ed5b46d4`, clean VCS
revision `afd7b26`, and identical before/after observations across all 13
repositories, with source-pair SHA-256
`47b08d8d42383a9b2a4d3def6f21ba22b4fef6a8d431de54e13d158e7e751c2e`.
The adopted plan
`0x3bd2e4ea748c73df475937b81b722c4dd3306fb83ce6e5f29ef3444e0e8abd95`
preserves all **3,521 actions**, all **1,212 completed renewals**, and the
existing spending limits. The repair remains **6,000 alpha**, with minimum
credit **5,999,999,999,999 alpha-rao**, within **37,250 alpha lifetime**. Full
producer success remains a launch prerequisite; the aggregate may overlap the
live campaign but must also pass for final acceptance.
[Closed bootstrap, release-lock, publication, final CLI and plan-review evidence](peerreview/evidence/FINAL-2-afd7-preparation-20260914/SUMMARY.md).

The `afd7b26` repair attempt completed the batched **1,000/1,000** checks and
reached the **2,200/3,456** carried-action progress marker before exiting **1**.
The failing action is `precompile.commitment-restore`: its original transaction
`0x39b6517b172e031ee2cda35cf3e2e0ba85119328de1502fb2984a9e66833b126`
finalized at native block **7,983,155** and was verified at journal sequence
**10,170**, before fleet 1's completed generation-3 renewal. The non-fleet action
was excluded from renewal-aware historical routing, and its live postcondition
still demanded the restored generation-2 commitment as current state. The
correction and deterministic regressions are described below. This
diagnosis uses the native refusal, source and retained receipts; it does not
claim a fresh independent replay of the original restore transaction.

The native result preserves the journal, supervisor state/config and public
identity bytes exactly. Only plan/config admission changed, and all **3,521
actions** remain equal to the reviewed preview. There are **zero journal
entries** for repair `alpha.repair.validator.1.7`. The canceled producer retained
**13/13 joins** (10 successful, 3 canceled); aggregate retained **5/5 joins**
(3 successful, 2 canceled). Both outer exits are **143**, all owned processes
and services were reaped, and no original failing test phase was reported.
These cancellations are not full-gate passes.
[Exact closed native refusal and unchanged-state record](peerreview/evidence/FINAL-2-afd7-preparation-20260914/native-refusal/README.md),
[closed gate commands, joins and cleanup](peerreview/evidence/FINAL-2-afd7-gates-canceled-20260914/SUMMARY.md).

The restore correction authenticates the original generation-2 native receipt
and both recorded observation hashes at their historical checkpoint. Completed,
authenticated generation-3 renewals supply the scope that permits this historical
check; ordinary live conformance still requires current generation 2. The
adjacent evidence-file issue is also covered: later battery/value phases may
extend the file, while replay reconstructs the exact original restore phase
without changing the file. Source `52def6334e72f77a0b2e3b655a37ec2d0df8d3fc`
passed **44 top-level roots and 48 terminal test events in each mode**. The first
full pass and two further exact 24-parent/28-event runs close the affected
confirmation sequence. Two isolated old-behavior controls produced respectively
**three expected failures/two passes** and **one expected dispatch failure/two
passes**. Incorrect working-directory and missing-verbose-event attempts remain
recorded separately and are not qualifying passes. The published review material
contains the causal patches, exact test membership and expected/actual outcome
tables; retained raw execution files are referenced by hash and are not copied
into this compact bundle.
[Restore correction and deterministic qualification](peerreview/evidence/FINAL-2-precompile-restore-20260914/SUMMARY.md).

The battery's original failure was recorded at journal sequence **10,172** on
**2026-09-11 at 15:22:15 UTC**: `Blake2b: blake2f failed`. Bounded diagnosis on the
owned LAN node reproduced it at pinned EVM block **0x7a206e**. Runtime 455's
pinned dispatcher maps address **0x09** to `Bn128Add`, whereas the old library
assumed Ethereum's Blake2f precompile. Its native address mapping is at
**0x080c**. The correction uses that mapping with an exact 32-byte result, checks
a known answer, and refuses missing, failed, malformed or zero self mappings
without querying zero custody. Other battery diagnostics remain observable.
These reads share the owned backend (`independent_rpc=false`); they are not an
independent public-node reproduction.

Battery source `d7142cc` passed **212 Solidity tests across 18 suites**, but its
static scan found a real Medium finding for the conditionally assigned
`selfMapped` local. Corrected source `bac57484` explicitly initializes it to
false: its **16 probe tests**, fresh artifact build and **three probe static
checks** pass. The other deployable roots' creation and runtime bytes remain
exact; the probe changes from **7,265 to 6,755 runtime bytes**, with unchanged
ABI, constructor and storage layout. Isolated old-library/old-probe variants
reproduce the whole-battery revert, failed-self-mapping revert and zero-custody
assertion failure. A causal wrapper's early stop is retained as a command
incident; only its unexecuted body was resumed. These local results do not
establish that the replacement probe is deployed or passes on-chain conformance.
[Runtime provenance, original static failure, corrected results and causal controls](peerreview/evidence/FINAL-2-precompile-battery-20260914/SUMMARY.md).

The cumulative candidate adds **22 deterministic probe-successor tests**, using
synthetic identities and persisted fixtures under
[Connect's test policy](../../connect/CODESTYLE.md). They cover preservation of
the completed native write/restore, exact core artifacts, original signed repair
authority, unchanged spend limits, receipt/evidence substitution, and restart
after each probe value phase. Adjacent review found that value calls advance the
deployer nonce beyond CREATE, and that same-plan recovery can record the same
finalization again. The correction admits only the exact ordered probe-call
prefix and identical recovery records; gaps, unrelated calls, changed identities
and resending the native drill remain rejected.

The combined normal body closed with exit **1 at 13:44:28 UTC**, and race with
exit **1 at 13:51:59 UTC**. Each recorded **99 top-level passes, one failure and
29 descendant passes**. Their sole failure,
`TestPrecompileProbeSuccessorConstructsOriginalNativeReplay`, reports that the
authenticated archive differs from the current fixture's original approval or
retained custody. Both source/binary identities remained unchanged, conversion
stderr was empty, and the race run reported no data race. The same compiled
normal binary separately reproduced both stale runtime-attestation tests. The
33-file correction retains the exact historical 29-file runtime-454 census and
adds synthetic missing-path, duplicate, substitution and digest controls.
The constructor's source fixture used an empty Go `DecimalUint`, whose signed
JSON is numeric zero. Reading that archive produces canonical string `"0"`;
the wire bytes match, but comparison with the unpersisted fixture fails.
The final correction reloads the complete synthetic persisted plan before
deriving its successor. It also checks all four adjacent zero-wei native
actions, preserving their intents and rejecting altered repair/native amounts.
Production approval and custody checks remain unchanged.

Corrected source `511a09e8c9fad91be09c49ad31327a05c6648568` changes only two test
files. Its **eight-root normal and race matrices passed**, closing at
**14:07:03** and **14:11:00 UTC**. The failed constructor completed three fresh
processes in each mode; the two failed census roots and two restart roots also
completed their three normal confirmations. The last race confirmation closed
at **14:16:57 UTC**. A separate four-root causal variant restoring the old
probe/nonce assumptions produced the exact **two expected failures and two
passing controls**. The original 100-root failures remain failed runs; unchanged
passes retain their original source scope. The complete final candidate gates
are still required.
[Constructor diagnosis, deterministic cases, adjacent controls and exact outcomes](peerreview/evidence/FINAL-2-precompile-successor-20260914/SUMMARY.md).

The earlier executable uses clean revision `e982b3f`, SHA-256
`2977471bafbddc7f1568db84c823f2c169ec1bc83857cc046a72baed2f820c99`, with identical
before/after observations of all 13 repositories. The source-pair hash is
`519744ac4ad704e69a8f7e5500f4dddfbb479ca9a8021cea6b66c60d6000ade7`.
The release lock changes only the probe artifact/runtime and their affected
source hashes; core deployed artifacts and spending approvals are preserved.
The read-only preview completed at **14:22:47 UTC**, with all six retained
state-file hashes unchanged. Reviewed plan
`0xa0f74d318b11170fc8287560c9aa239000ad7572dfb7dc416d86a8b3c537297a`
retains all **3,521 action IDs**, the **1,212 completed renewals**, and the
original native write/restore intents. It binds one probe replacement at
`0x500c8955E0b76848F1f32C0E67c7e0715c1Ecf00`, using deployer nonce **34**.
Only eight probe action intents and the EVM campaign gas reserve change.
Active plus superseded spending remains within **200 TAO**, **180 EVM TAO**,
**37,250 alpha**, **262 registrations** and **zero subnet creations**.
The same **6,000-alpha** repair retains its minimum credit of
**5,999,999,999,999 alpha-rao**. Setup application ran on the owned LAN node from
**14:35:48 to 14:37:49 UTC** and exited **1**: its recomputed hash was
`0xee290d52e2ef0bcb0c3d5dfc5c5070f7ef7fc4b25d2e465c42c7cdabea763f6c`,
so the reviewed approval was refused. No transaction was sent, and all six
retained state-file hashes remain unchanged. The journal still has zero rows
for repair `alpha.repair.validator.1.7`.
[Exact release preparation, preview and closed refusal](peerreview/evidence/FINAL-2-e982-preparation-20260914/SUMMARY.md).

The confirmed cause is the new descriptor's `FinalizedHead`: it was refreshed
on each invocation and included in the approval hash. The installed source plan
has no successor until application, so preview and apply constructed different
descriptors. The refused invocation emitted no rendered plan; a complete field
comparison against its computed hash is unavailable. The correction binds the
checkpoint already covered by the original signed coordinator repair result,
while retaining it inside both typed and persisted approval hashes. Before
CREATE, it also checks the old probe's EVM balance and both approved alpha
positions at the fresh head, rejecting funds received since that older anchor.
Completed CREATE and exact subsequent transaction-prefix recovery keep their
existing historical custody scope.

The two-file correction adds [five deterministic regressions](precompile_probe_approval_test.go).
They exercise the production constructor, action binder, actual approval guard
and persisted hash under advancing finality, plus altered signed checkpoints,
receipt/journal/runtime identity, fresh custody and completed-CREATE recovery.
Formatted source `0a94b7d` passed the **40-root normal matrix** from
**15:01:57 to 15:02:29 UTC**, with body exit **0**, exact expected/actual root
outcomes and unchanged source, dependency and binary identities. Its normal
binary SHA-256 is
`e67e2f362068e7e8648273e18f6d6f6d206581f048e429950b5d02012343daed`.
The **40-root race matrix** also passed, from **15:17:18 to 15:20:42 UTC**,
with race binary SHA-256
`23e7466ce21bc71d14d25a96b41c2aabc3bde264f9080b9fa999538e85702cf2`.
Each mode has exactly **40 top-level PASS terminals**; the separately recorded
116 descendant events are not asserted to be 116 descendant passes. All source,
dependency, binary and membership checks remain exact. Two additional fresh
normal processes passed the advancing-finality root at **15:04:37** and
**15:10:44 UTC**. The disposable one-assignment moving-head variant completed
with **one expected failure and three passing controls**, reproducing the actual
approval mismatch. These scoped results do not establish repair completion.
[Exact case-to-failure mapping, outcomes, causal patch and input identities](peerreview/evidence/FINAL-2-probe-approval-anchor-20260914/SUMMARY.md).

The correction was published through `9e258f8`; lock-only successor `7eab049`
changes only `repositories.sn_go_source_hash` to
`sha256:5f0b285194e4359b0fd806aa9f136c3656eec1bf480a793c761f88d8e2abd199`.
The lock file has SHA-256
`7d77c0016491c85e966797f994b6a7c368ad55800c2cb838156439c71b479f84`.
Native lock preview and apply both exited 0 and sent no chain transaction.
The final clean `7eab049` executable built with exit 0 at **15:21:02 UTC**,
SHA-256 `feb8890a60efff80cada37a9015b8edeb2d49f20476b1e7ee2c9d4c0a3176b9e`.
All 13 before/after repository observations match, with source-pair SHA-256
`05d8d9d50e2904ccaeef66a8ffea9f693d8a06e340423a66eb2032c1a473b109`.

The new native preview ran **15:23:41–15:25:40 UTC**, exited 0 and preserved
all six retained state-file hashes. Its plan retains every action intent and
all three budget objects from the earlier reviewed replacement plan. It binds
the original signed checkpoint at EVM block **7,986,580**, hash
`0xeb101aeb317fee5b3f27c44540b63c4f4eeb46882ead59654e36b37dd57f22f9`,
while checking current custody and nonce. The same 3,521 action IDs, completed
1,212 renewals, nonce-34 replacement, native write/restore intents and approved
6,000-alpha repair remain. Setup application started at **15:27:17 UTC** and
the installed plan was observed to match the reviewed `0x2b552798...` approval
at **15:36 UTC**. The two later transport failures below added no journal row
or repair transaction. Both full gates passed their five preflights. The
producer later closed with two failures; the aggregate remains live at this
report's cutoff.
[Closed release preparation and reviewed preview](peerreview/evidence/FINAL-2-7eab-preparation-20260914/SUMMARY.md).

The first `7eab049` application completed the **1,000/1,000** batched audit and
reached **2,450/3,455** carried-action checks before exiting **1 at 15:47:17 UTC**.
The error was `fleet.renew.1.34.bind.2: current postcondition: 429 Too Many Requests`,
with an nginx HTML body. Only plan/config admission changed; journal,
supervisors and public identities remained unchanged. All action objects and
the three nonempty spend/limit objects exactly match the reviewed preview.
Source tracing keeps the failed path on the guarded owned LAN client. Bounded
LAN diagnostic reads subsequently returned HTTP 200, chain ID 945 and the
binding's canonical block/receipt with success status. Those observations were
retained from the tool transcript; original raw HTTP capture files do not
exist, and the exact individual request that received 429 is unidentified.
[Closed first refusal, unchanged custody and diagnostic provenance](peerreview/evidence/FINAL-2-7eab-transport-20260914/SUMMARY.md).

The direct retry used the same admitted plan and executable, with no new
preview, build or source change. It ran **15:51:31–16:09:25 UTC**, reached
**2,400/3,455** carried checks, and exited **1** at
`fleet.renew.1.31.bind.4` with the same nginx HTTP 429. All six retained state-file
hashes are identical before and after this retry. The journal remains at
sequence **16,318**, and repair `alpha.repair.validator.1.7` remains unsubmitted.

The follow-up infrastructure review found **100 requests/second**, **burst 200**
and **128 connections/client** in xops `a9d2eb4`'s canonical snow nginx template,
with HTTP 429 configured for both limit types on the archive and lightnode
gateways. This source configuration can produce the observed response; actual
deployed configuration and request-versus-connection attribution remain
unverified because SSH rejected the available identity. Isolated xops correction
`51325d00e18a157b2ceb5d5ac44bb0645550b289` removes those directives, their obsolete
settings and their playbook assertions. Both exact listeners, source allowlists,
GET/POST controls, WebSocket paths, body bounds and native node capacity remain.
The adjacent overlay and node templates contain no request-rate setting.

Terra ran the complete affected Python/Jinja module: **29/29 passed in 3.926s**.
Both new synthetic renderer tests then passed two more fresh processes on the
same source/interpreter. Restoring only the old template produces exactly
**one expected failure and one passing route control**, with no timing or network
dependency. The rendered configuration has SHA-256
`6371976bddf174874083f22897bb072977c5218f7b161a95f1e1c02a13edd453`.
Strict Jinja and route checks passed; a local nginx binary was unavailable, so
`nginx -t` was not performed. These results qualify the isolated source change;
they do not establish rollout or cessation of 429 responses. The correction was
published as branch `sim-testnet/owned-rpc-unpaced-20260914`, then integrated
over current upstream as `d33d417` and pushed to xops main at **18:03 UTC**.
The Subtensor contents match the qualified source. The full gates' original
xops checkout remains unchanged at its recorded pin.
[Repeated refusal, infrastructure cause, deterministic regressions and rollout limits](peerreview/evidence/FINAL-2-owned-rpc-throttle-20260914/SUMMARY.md).

The canceled `e982b3f` producer ran **14:21:59–14:42:32 UTC**, with **13/13**
admitted children joined: **10 successful and 3 canceled**. Aggregate ran
**14:23:00–14:42:33 UTC**, with **5/5** joined: **3 successful and 2 canceled**.
Both outer exits are **143**. All five initial preflights passed for each gate,
and the **14:46:37 UTC** process census found no remaining matching gate, test
or service owner. No original test failure preceded cancellation. Full gates
on the corrected final source are still required.
[Exact gate commands, joins, cancellation and cleanup evidence](peerreview/evidence/FINAL-2-e982-gates-canceled-20260914/SUMMARY.md).

The prior setup attempt exited 1 at **06:41:20 UTC** because its old refresh
postcondition expected binding version count 2 while two fleets had later,
authenticated lifecycle generation-3 bindings. The supported recovery performs
renewal first, allowing completed successor evidence to authenticate historical
state at its original checkpoint. Native setup admitted the corrected source
plan, then was intentionally canceled and joined at **07:30:05 UTC**, before
action execution. It is recorded as **CANCELED, not PASS**, with the complete
native input writes verified and journal/supervisor state unchanged. No journal,
receipt or version-count predicate was manually changed.
[Original refusal, native admission and exact renewal review](peerreview/evidence/FINAL-2-oracle-renewal-preparation-20260914/README.md).

At the historical **05:22 UTC on 2026-09-14** checkpoint, the published SN candidate was
`ca4281201077b6e3e9cde1004568efa219d0e12c`. Its complete producer gate has
**passed: 36/36 phase joins and outer exit 0**, from **00:22:38 to 02:22:22 UTC**.
Initial and final source-freeze observations are byte-identical across all
13 repository heads, with SHA-256
`aa9d12bf8740a4bf9a73883de6e46a9c841107ab6cd0a3bd8f0dd558dc2eb54a`.
[Current producer commands, logs and source checks](peerreview/evidence/FINAL-2-producer-ca42812-20260914/README.md),
[byte manifest](peerreview/evidence/FINAL-2-producer-ca42812-20260914/MANIFEST.json).
The complete aggregate also **passed: 25/25 phase joins and outer exit 0**,
from **00:26:03 to 04:28:36 UTC**. All seven preflights passed, and its initial
and final source observations are byte-identical to the producer's observations.
[Aggregate commands, complete logs and original startup refusal](peerreview/evidence/FINAL-2-aggregate-ca42812-20260914/README.md),
[byte manifest](peerreview/evidence/FINAL-2-aggregate-ca42812-20260914/MANIFEST.json).
Its original launch refused before any test body because concurrent Git fetches
collided on a moving server remote-tracking reference. The successful replacement
used the same source, lock and workload with fresh output paths; that refusal
remains separate from test results. These local passes do not establish live
campaign acceptance.

The corrected components have completed their affected qualification:

- SN `7de62c7`, with server dependency `b67ea7a`: routing/source guards,
  historical-input checks, observation-profile and renderer checks passed
  normally and under race. The four roots active at the earlier simulator
  timeout also passed three sequential race executions on the same binary.
  [Exact scopes, original attempts and source identities](peerreview/evidence/FINAL-2-owned-rpc-7de62c-qualification-20260914/README.md).
- Server `0f095a6`, with SN dependency `e3d3539`: all 12 focused captures
  completed with actual outer, body, verification, source and cleanup exits 0.
  This includes normal/race publication and controller coverage, the three
  normal confirmations of the original 1,000-client failure, monitor coverage
  and confirmations, and the real PostgreSQL migration regression in both
  modes. [Exact captures and confirmation events](peerreview/evidence/FINAL-2-server-0f095a-qualification-20260914/README.md).

These component passes retain their recorded source scope. The complete
`ca42812` producer and aggregate results above are separate complete executions.
The final graph still pins the qualified server `0f095a6` as a published ancestor;
unrelated newer server commits are not imported into this qualification.

The native executable at that checkpoint built successfully with unchanged before/after
source observations. Its SHA-256 is
`b440fcaf46821278950cefc85bbcba751397cbd1870380e002b05109f1b2b5d4`.
Its LAN-only doctor finished at **00:28:41 UTC**, exit 0 and `ready=true`.
Exactly **62 of 64** checks have `ok=true`; all hard checks pass. The two soft
results disclose that the aliases share one physical backend. This is
`independent_rpc=false`, not a 64/64 independent-observer pass.
[Native build, doctor and source evidence](peerreview/evidence/FINAL-2-runtime-preparation-ca42812-20260914/README.md).

One setup preview finished at **00:31:58 UTC**, exit 0. The reviewed revision is
`0xae15ecdd37cac2a223533b4a1b3d9fa6431da33d78cfd9ccac006a98e9d8f414`.
All **2,309 actions** and all spending limits are unchanged from the adopted
replacement plan below. Only source/input hashes, prior-plan identity and
fresh observation/generation fields changed. The approved 6,000-alpha repair
command started automatically at **02:22:23 UTC**, immediately after the
complete producer pass and exact reviewed state, source and budget checks.
It adopted the reviewed `ae15ecdd` plan, then **exited 1 at 02:33:52 UTC** while
verifying the existing `config.render` action: the operator overlay links named
the historical config checkout, while this invocation expected the new path.
The journal and supervisor files remained byte-identical; no transaction was
submitted.
[Exact review](peerreview/evidence/FINAL-2-runtime-preparation-ca42812-20260914/setup/REVIEW.json),
[preview before/after state hashes](peerreview/evidence/FINAL-2-runtime-preparation-ca42812-20260914/setup/RESULT.json).

The supported recovery retains the historical `--platform-config-repo` path.
All seven release-bound local file contents and both shared trees match the
qualified checkout exactly. Filesystem permissions differ, but the Git modes
and locked content digests agree. The release binds the config contents and
shared tree; resolved plan identity excludes repository paths. The review
records both distinct config commit IDs rather than relabelling the historical
checkout as the qualified commit. A native preview **passed at 05:08:04 UTC**,
returning the identical approved plan and all **2,309 unchanged actions**, with
all four observed state files unchanged. No code, checkout, link or journal
edit was needed.
[Recovery diagnosis and exact native results](peerreview/evidence/FINAL-2-config-path-recovery-20260914/README.md),
[source-backed path and RPC analysis](peerreview/evidence/FINAL-2-config-path-recovery-20260914/review/HANDOFF.md).
The approved repair retry ran from **05:09:51 to 05:22:14 UTC** with only that
path argument changed. It passed the overlay-path check but **exited 1** at the
next `config.render` check: reserved staging differed from the current approved
capacity or authority configuration. All four observed state files remained
byte-identical; the journal still has **10,258 rows** and no entry for
`alpha.repair.validator.1.7`. The stored configuration retains provisional
discovery flags and contexts; its ordinary render receipt cannot establish
current strict configuration. A render-version and owned-discovery routing
correction was subsequently integrated into the current candidate. The successful preview established
plan identity, not successful setup. Finalized repair credit and a complete
reserve census remain pending.

Earlier checkpoint, **23:48 UTC on 2026-09-13**: the full producer gate on `e3d3539` had
passed with **36/36 phase joins and outer exit 0**, ending at 21:44 UTC.
[Exact producer commands, logs and source identities](peerreview/evidence/FINAL-2-producer-e3d3539-20260913/MANIFEST.json).
The original aggregate **failed**, ending at **23:14:29 UTC with outer exit 1**.
All 23 phases joined: **20 passed and three failed**. The failures were a
cumulative simulator race-package timeout, missing server migration-monitor
entries, and a context deadline in the full 1,000-client registration cohort.
Its final source check separately refused because canonical SN `main` advanced
independently to `928b7d5` during execution.
[Original aggregate commands, complete phase logs and refusal](peerreview/evidence/FINAL-2-aggregate-e3d3539-20260913/README.md),
[byte manifest](peerreview/evidence/FINAL-2-aggregate-e3d3539-20260913/MANIFEST.json).
At that checkpoint the corrected source was still being qualified. Later
focused passes do not change the original aggregate's failure. The later
`ca42812` producer and aggregate passes have their own source scope;
live acceptance remains pending.

The user approved **37,250 alpha lifetime for one 6,000-alpha replacement**
of the unsubmitted 3,750-alpha repair. Native setup adopted replacement plan
`0xdbeb584008bbdbc6607a49a5118c1c82fdfa18a15ca8fe5b5c8cea9d2775c37a`,
then failed at **22:09 UTC** during historical verification, before action
execution. The transaction journal and both supervisor files remained
byte-identical; **no repair transaction was submitted**.
[Native result and before/after state hashes](peerreview/evidence/FINAL-2-owned-rpc-transition-20260913/failed-native-apply/RESULT.json),
[original error](peerreview/evidence/FINAL-2-owned-rpc-transition-20260913/failed-native-apply/stderr).
The earlier driver, doctor and dry-run receipts remain
[preparation evidence](peerreview/evidence/FINAL-2-runtime-preparation-e3d3539-20260913/README.md).

The user's latest instruction requires **all actual testnet RPC** to use
`192.168.1.162:9944`, including historical and final verification, with no RPC
pacing. The corrected candidate records `owned-node` and `independent_rpc=false`;
new observations do not claim an independently operated backend. Old public-node
evidence below retains its original scope and provenance. The tested historical
EVM state and native storage are available on the LAN node.
[Raw capability probes and the corrected native hash namespace](peerreview/evidence/FINAL-2-owned-rpc-transition-20260913/README.md).

[FINAL.md](FINAL.md) remains report 1, preserved at SHA-256
`489fe5a367af6ce17541a0626fc052455f373d7792316593cc752d501cefd996`.
Later finalizations will use `FINAL-3.md`, `FINAL-4.md`, and so on. This report
records corrections to report 1 without rewriting its original results.

## Peer-review findings and required closure

The peer reviewer supplied an independently queried chain review and committed
the [review scripts and instructions](peerreview/verify/README.md) at
`580831d0b483229d4d41d44d9dabfe04bc906035`, alongside
[the first review's HTML report](final.html). Their supplied narrative reports
67 of 70 assertions reproduced; the committed two-stage suite has a different
declared census of **59 passing checks out of 62**, with the three findings
below. These are separate populations, not interchangeable totals. Terra
reproduced **59/62** against the public endpoint at **07:08 UTC**, with exactly
the same three findings and actual zero exits for both stages. This establishes
reproduction of the findings, not an all-check pass.
[Fresh complete results](peerreview/evidence/FINAL-2-independent-review-20260913/results.json)
(`sha256:987037ccea00ee0bdc8653ad815c40d56659518751221aa1f593712cf4de1efa`),
[actual endpoint and working directory](peerreview/evidence/FINAL-2-independent-review-20260913/environment.tsv),
[stage 1 output](peerreview/evidence/FINAL-2-independent-review-20260913/stage1.stdout),
[stage 2 output](peerreview/evidence/FINAL-2-independent-review-20260913/stage2.stdout).

| Finding | Understanding | Closure required for this finalization |
| --- | --- | --- |
| Production cadence was never scheduled | The first run used 300/50/150/5. A `production_cadence` YAML entry does not prove scheduling or activation. | Retain the successful policy-scheduling transaction, effective epoch, finalized policy state showing **360/60/180/6**, and **three consecutive fully observed epochs** under that active policy. The five accelerated epochs remain a separate prerequisite. Pending. |
| `max_allowed_validators=64`, target ≤56 | The [whitepaper](../WHITEPAPER.md) calls this root-controlled/runtime-dependent. The [compatibility policy](../deploy/testnet/hyperparams.yml) already requires exactly 64. The user has explicitly directed this run to work with the real limit. | **Use 64; reaching 56 is not a testnet prerequisite.** Retain finalized value, actual permits, UID occupancy and 200-head selection evidence from the run. Report the difference from the whitepaper target without claiming ≤56 compliance. No parameter change is needed. |
| Reserve 61.449%, below 65% target | The historical 60% floor passed; the repair target did not. | The approved **6,000-alpha** replacement finalized at native block **8,009,634**, with exact equal debit/credit. The complete 256-UID census at **8,010,632** proves **65.5997247163%**, closing the preparation reserve target. [Pinned on-chain proof](peerreview/evidence/FINAL-2-approved-renewal-and-reserve-20260915/README.md). Monitor the 60% floor during the actual campaign and report its end-of-run share separately; final acceptance remains pending. |
| Epoch 309 paid despite capturing zero | `RootMissed(308)` carried each operator's funded amount into its own epoch-309 entitlement. | The missing historical transition is reproduced below from both nodes. Every new paid epoch must similarly explain its funding source, carry, payments and remainder per operator. Historical reporting omission closed; fresh-run accounting pending. |
| Artifact signers differ from registered root signers | A recoverable artifact signature establishes provenance. The coordinator authorizes the root commitment transaction using the epoch's registered `rootSigner`; these are separate checks. | Preserve each recovered artifact signer, committed artifact hash/root, transaction sender and epoch-specific registered root signer. The collector/verifier correction is integrated into candidate `4fda909` and its affected tests passed normally and under race; retained keys and old signatures stay unchanged. Fresh-run evidence remains pending. |
| Chain verification cannot establish off-chain usage or lifecycle | A committed hash authenticates bytes, not the truth of usage, restart or gate assertions within them. | Label chain-reproduced, independently recomputed, artifact-only, and locally executed evidence separately. Link exact artifacts, executable/source identity, commands, actual exits, process generations and shutdown outcomes. Pending full-run evidence. |

The runtime-455 source pinned by the release lock is commit
`67dcf7f791dc495064c293f080a0702cb433e51e`. Its
[`sudo_set_max_allowed_validators` implementation](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs#L888)
calls `ensure_root(origin)`. This is a chain-root governance dependency; owning
the subnet or serving its RPC does not satisfy that origin check. The existing
testnet compatibility policy and the user's explicit direction allow the bounded
experiment to proceed at 64; the ≤56 target remains unmet and is not a blocker
for this run. Actual validator permits and native head selection
must still be reported; a configured maximum of 64 is not evidence of 64 active
validators or a fixed 64-slot partition.

A stopped-state snapshot at **07:45 UTC on 2026-09-13** reproduced byte-identical
results on the owned and public RPC nodes at finalized native block **7,995,269**,
hash `0xe046f55170e00ee58aab564a71cbeb540cd021248477d136adcfb56f311cc821`.
It shows `max_allowed_validators=64`, `max_allowed_uids=256`, and
`SubnetworkN=256`. The 256-entry permit vector has eight true entries, at
UIDs **0, 2, 7, 8, 50, 52, 254 and 255**. Holding a permit does not establish
that a UID submitted weights. This snapshot establishes the actual starting
limits and permit census; the new campaign must still prove its 200-head
selection and capture the permits in effect during that run.
[Exact requests](peerreview/evidence/FINAL-2-real-limits-20260913/requests.json),
[owned responses](peerreview/evidence/FINAL-2-real-limits-20260913/owned.json),
[public responses](peerreview/evidence/FINAL-2-real-limits-20260913/public.json),
[decoded comparison](peerreview/evidence/FINAL-2-real-limits-20260913/summary.json).

## Missing epoch-308 to epoch-309 funding transition

On **2026-09-13 at 06:52 UTC**, read-only calls to both
`http://192.168.1.162:9944` and `https://test.finney.opentensor.ai` reproduced the
same historical results. Both identify chain 945 and genesis
`0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105`.
The comparison covers **19 identical historical/identity results**, including
four canonical blocks, eight `carry(noId)` reads, the settlement logs, both
epoch-309 entitlements and both successful `RootMissed` receipts. Each endpoint
also supplied its current finalized block; moving heads are recorded separately.
[Exact requests](peerreview/evidence/FINAL-2-carry-20260913/requests.json),
[owned-node responses](peerreview/evidence/FINAL-2-carry-20260913/owned.json),
[public-node responses](peerreview/evidence/FINAL-2-carry-20260913/public.json),
[comparison](peerreview/evidence/FINAL-2-carry-20260913/comparison.json).

All amounts in this table are **alpha-rao**; 1 alpha = 1,000,000,000 alpha-rao.

| Transition / finalized EVM block | Operator 1 | Operator 2 |
| --- | ---: | ---: |
| Epoch-308 `EmissionCaptured`, 7,988,077 | 51,653,232,130 | 51,667,423,224 |
| `carry(noId)` before missed-root handling, 7,988,226 | 0 | 0 |
| `RootMissed(308)` and resulting carry, 7,988,227 | 51,653,232,130 | 51,667,423,224 |
| Epoch-309 `EmissionCaptured`, 7,988,377 | 0 | 0 |
| Carry immediately before entitlement finalization, 7,988,526 | 51,653,232,130 | 51,667,423,224 |
| Epoch-309 entitlement total, 7,988,527 | 51,653,232,130 | 51,667,423,224 |
| Carry after entitlement finalization, 7,988,527 | 0 | 0 |
| Rounding residue after the 16 retained claims | 5 | 3 |

The missed-root transactions are
`0x7c39d45b0c4d6f31db3d322bfe7a6171a2a688576d58f1d940646760170e510c`
and
`0xb771d9f296eaab244e7255765ce5762ec2046ac14616bc6fe4dd2e82500ebb1e`,
both in block **7,988,227**, hash
`0x31daecc50a6f78ddb9904da3b22c800c9632c5fd7abad7fb8d8d84d0080af8ee`.
Their complete receipts are retained under `root_missed_1_receipt` and
`root_missed_2_receipt` in the [public RPC capture](peerreview/evidence/FINAL-2-carry-20260913/public.json).

Thus **103.320655354 alpha** was captured in epoch 308, carried per operator,
and allocated to epoch 309. The [16 retained payments and pinned vault state](peerreview/evidence/epoch309-paid-claims-20260912.json)
account for **103.320655346 alpha paid plus 8 alpha-rao residue**. This is
value-preserving carry, not fresh epoch-309 emission or a second capture.
The [vault implementation](../evm/src/STSettlementVault.sol) records
`carry[noId] += record.funded` on a missed root and consumes only that same
operator's carry when finalizing its next entitlement.

## Independent reproduction and its limits

The committed review suite requires Python 3.9+ and an archive RPC. Its original
default remains the public Opentensor service. For this finalization, explicitly
select the LAN node for both stages. Run in order in an isolated copy because
they write `results.json` and `topics.json`:

```sh
cd sim-testnet/peerreview/verify
export SN_RPC_URL=http://192.168.1.162:9944
python3 verify_all.py
python3 verify2.py
```

These commands replay report 1's fixed historical assertions. The successful
07:08 UTC public-node run below is retained evidence; it need not be repeated
for the current run. A new LAN replay must be labeled as owned-node-only, and
calling the same node through a second alias does not establish independence.

Retain the two actual exits, exact source revision, endpoint and complete check
IDs. A zero process exit does not mean all assertions passed. The expected
three findings are `wp-cadence`, `hp-maxval`, and `reserve-target`; a different
census or failure set requires explanation. Do not overwrite report 1's
`final.html` with `build.py` during report-2 execution.

Source inspection found three scope limits to address when reporting the next
run. At the reproduced revision `580831d`, `meta.endpoint` was hardcoded to
the LAN address even though the RPC client defaulted to the public endpoint;
the external execution record therefore preserves the actual endpoint. The
current source records the configured RPC client's endpoint directly, without
changing the original rerun's bytes or the verification assertions.
`wp-cadence` checks whether any
scheduled policy has a 360-block epoch; it does not establish all four policy
values, activation or three observed epochs. `reserve-target` replays the
historical block-7,992,355 census and will remain false after a later repair.
The old root, entitlement and receipt constants also belong to epoch 309.
Historical reproduction remains useful; new acceptance requires fresh,
explicitly identified epochs, finalized state, roots and transactions.

The successful rerun used exact script revision `580831d`; script hashes
remained unchanged. Its committed baseline outputs were preserved separately
and removed from the working copy before execution, so stage 2 could append
only to newly emitted stage-1 results. An earlier command mistakenly added
port 19 to the endpoint and failed; its
[failed exits and stale summary](peerreview/evidence/FINAL-2-independent-review-20260913/rejected-endpoint-attempt/)
are retained as a rejected attempt and do not establish any chain result.

The independent Python Keccak and secp256k1 implementations retain their
separation from the project under review. A second cryptographic implementation
can cross-check them without substituting the project's own Merkle calculation
for the independent result. Script output, direct chain reads and off-chain
artifact claims will be reported with their actual verification scope.

## Current execution and remaining acceptance

The full producer gate on SN `9133805` ended with exit 1 at **07:52 UTC on
2026-09-13**. It recorded a **25-minute semantic race-package timeout**, a
**10-minute capture race-package timeout**, and a final source check that
correctly refused the older snapshot after publication of newer source.
The aggregate gate on the same snapshot ended with exit 1 at **09:09 UTC**,
recording a **90-minute simulator race-package timeout**, the infrastructure
errors described below, and the same final source refusal. Its database and
history phases passed. These remain failed gates. None is converted into a pass by later focused
qualification.

Candidate `4fda909` contains authenticated succession for the failed
pre-acceptance campaign, the artifact/root-signer correction, and independent
execution of the producer's observed expensive test groups. The old signed
attempt, failed result and approvals remain intact. Campaign succession's
failed normal scopes completed their required confirmations on their recorded
sources. All **37 adjacent campaign/attempt/analyzer roots** then passed normally
and under race on `4fda909`. The lifecycle, runtime-configuration,
pool-registration and full-artifact race confirmation streaks all closed.
The aggregate's four roots that were active at its timeout completed all
three race confirmations on the same retained binary at **09:49 UTC**.
The scheduling correction splits the two whole-population
tests from the complete complementary selection, retaining the 90-minute
timeout, parallelism and every selected root.

The combined scheduling and infrastructure-scope correction at `f2a87d2`
passed its **12 affected guard roots normally and under race**. The compiled
inventory confirms **2,186 roots**, partitioned into exactly two whole-population
roots and their 2,184-root complement, with no omissions or duplicates. This
inventory check did not execute the complete suite; the final aggregate must
still run both selections. The corresponding xops test correction is
`a9d2eb4`.

At **10:01 UTC**, the clean, pushed `58b251f` native CLI installed the reviewed
release-lock bytes with exit 0. Their SHA-256 is
`ddfd939ac49a465957e3aeaac3ecb49ddd892a2cf6d1821ad9d5d3a583c69249`.
Only the simulator production-source and protocol-script digests changed;
the runtime, EVM artifact, interface and infrastructure pins stayed unchanged.
This local lock update sent no chain transactions. Both final gates and the
live campaign remain pending.

The aggregate alarm occurred while its four active roots had run for only
**37 seconds, 39 seconds, 2 minutes 15 seconds, and 45 seconds**. The log contains
no assertion failure or race report before the alarm, and many tests were
still waiting for their parallel execution slot. This supports shared package
clock exhaustion; it does not establish that any one root hung for 90 minutes.
The correction must preserve the complete selected population and retain the
original timeout. There is no final source/gate or new live-acceptance pass yet.

The same aggregate's infrastructure phase ran **43 Python checks with four
errors**. Three errors required Grafana source outside the simulator's declared
runtime dependencies; the fourth was a Subtensor test using the obsolete
singular `gateway_bind_address` field. The current configuration instead has
`gateway_bind_addresses`, including both management and LAN listeners.
The next SN gate selects the complete **27 Subtensor checks plus
the gateway regression**, with that stale test corrected. All **28 passed**,
and the corrected gateway test completed three fresh successful executions
on the same source, with unchanged source checks before and after. The other 15
infrastructure checks concern services outside this finalization; they are
excluded from its scope, not reported as passing. The original four errors
remain recorded. No node or gateway deployment change follows from this test
correction.

The producer gate on `0dcb5c8` hit another **10-minute capture race-package
timeout at 11:17 UTC**. Its 443 selected roots had passed normally. At the
alarm, `TestFleetRenewalRevisionRefusesCustodyFeeOrLiabilityChanges` was active
and 43 roots were still waiting at `t.Parallel`; the log records runnable
signature verification, with no preceding assertion failure or race warning.
The failed producer was stopped and joined at **11:34 UTC**: 21 phases passed,
one failed, and three were interrupted. The interrupted phases do not establish
test verdicts. [Original timeout](peerreview/evidence/FINAL-2-capture-timeout-20260913/capture.log),
[failure record](peerreview/evidence/FINAL-2-capture-timeout-20260913/failure.json),
[actual phase outcomes](peerreview/evidence/FINAL-2-capture-timeout-20260913/producer-phases.json).

Correction `b78b672` changes only the producer's scheduling and its existing
coverage guards. It assigns the affected 443 roots to four disjoint groups of
**343, 87, 11 and 2**, retaining their five-minute normal and ten-minute race
deadlines. All four groups passed normally and under race, with the native
owner exiting 0 at **12:06:50 UTC** and identical source checks before and
after. The compiled inventory still contains exactly **454 roots**; the eleven
unchanged separately owned roots retain their prior qualification and remain
in the complete gate. They were not re-executed as part of this focused run.
All twelve affected coverage guards also passed normally and under race.
[Exact four-group commands](peerreview/evidence/FINAL-2-capture-qualification-20260913/affected443/capture-affected443.native.sh),
[compiled inventory](peerreview/evidence/FINAL-2-capture-qualification-20260913/affected443/capture-list.actual.txt),
[native exit](peerreview/evidence/FINAL-2-capture-qualification-20260913/affected443/native.status),
[normal guard results](peerreview/evidence/FINAL-2-capture-qualification-20260913/guards12-normal-p1-source-v2/report.json),
[race guard results](peerreview/evidence/FINAL-2-capture-qualification-20260913/guards12-race-p1-source-v2/report.json).
The active timeout root and all four of its subtests completed three fresh,
sequential successful race executions on the same source and binary SHA-256
`9a027c38dd93146086026039d58856889a0f8b22f40dc1e8dfe11406627c9f17`.
Every outer and test owner exited 0, with unchanged source checks. The last
confirmation completed at **12:23 UTC**.
[Confirmation 1](peerreview/evidence/FINAL-2-capture-qualification-20260913/active-revision1-race-p1-source-v2/report.json),
[confirmation 2](peerreview/evidence/FINAL-2-capture-qualification-20260913/active-revision1-race-p2-source-v2/report.json),
[confirmation 3](peerreview/evidence/FINAL-2-capture-qualification-20260913/active-revision1-race-p3-source-v2/report.json).

The concurrent aggregate on `0dcb5c8` was stopped and joined at **12:14:51 UTC**
to replace the superseded candidate. Ten phases had passed; its simulator race
and complete Connect normal phases were interrupted with exit 143. No actual
aggregate test failure had been observed at the stop, but the incomplete gate
does not establish an aggregate pass. [Raw phase joins](peerreview/evidence/FINAL-2-superseded-aggregate-20260913/outer.stdout),
[actual outer exit](peerreview/evidence/FINAL-2-superseded-aggregate-20260913/outer.exit).

After publication of the qualified `b78b672` correction, its clean native
executable applied the reviewed release lock with exit 0, confirmed at
**12:25 UTC**. The resulting YAML SHA-256 is
`776f6cf9d57d1c8427ac981f3cf2222ddc1441371c90cbded2789d8ea1299767`.
Only `repositories.protocol_source_hash` changed, to
`sha256:83f8fd02ccd0cb8333bade3124aeab1bb3f480a008ceaaa3e74287a3b0d67ceb`;
production Go, runtime, EVM artifact, interface and infrastructure digests
remain unchanged. This was a local file update and sent no chain transaction.
[Actual invocation and result](peerreview/evidence/FINAL-2-capture-lock-20260913/RESULT.json).
The replacement complete gates started at **12:29:35 UTC** on published SN
`90f67b1859368d34b0404870f46819f012712674`. The **producer passed at
14:30:28 UTC**, with all 36 native phase joins exiting 0 and its final source
and release-lock checks passing. This includes the previously failing capture
phase, which completed in 167.019 seconds normally and 579.892 seconds under
race. [Complete producer output](peerreview/evidence/FINAL-2-producer-90f67b1-20260913/capture/outer.stdout),
[actual exit](peerreview/evidence/FINAL-2-producer-90f67b1-20260913/capture/outer.exit),
[finish time](peerreview/evidence/FINAL-2-producer-90f67b1-20260913/capture/outer.finished-at).

The aggregate's complementary simulator race phase **failed its 90-minute
deadline**, with native exit 1 observed at **14:59 UTC**. Its separate two-root
population phase had passed in 1,967.533 seconds. At the alarm, three full
supplement publication roots had been active for 19m25s, 17m20s and 19m22s;
the fourth active root had just entered its transport-bound fixture. Astra's
stack census found 188 roots still queued for parallel execution. The active
publication stacks were traversing local artifact stores, and retained file
timestamps show progressing writes. The alarm does not establish a 90-minute
hang in any one test. The aggregate finished with **exit 1 at 15:40:56 UTC**:
21 native phases passed and only the simulator race phase failed. Its final
source check passed, and the owned processes and private services were joined.
The gate remains failed. [Actual aggregate output](peerreview/evidence/FINAL-2-aggregate-timeout-20260913/capture/outer.stdout),
[actual exit](peerreview/evidence/FINAL-2-aggregate-timeout-20260913/capture/outer.exit),
[original timeout output](peerreview/evidence/FINAL-2-aggregate-timeout-20260913/sn-simulator-race.log),
[failure and active-root record](peerreview/evidence/FINAL-2-aggregate-timeout-20260913/failure.json).

Correction `e99954a`, published at **15:44 UTC**, gives those three whole
publication roots a separate
package clock and retains the complete disjoint race census: **2,181 ordinary,
three publication and two population roots**. It changes scheduling and
existing ownership guards, preserving test bodies, payloads, cryptography,
parallelism, uncached execution and 90-minute deadlines. At that historical
checkpoint, the interrupted roots and complete aggregate still needed
qualification; later results retain their separately stated source scope.
The correction's ten affected scheduling guards passed normally and under
race, with actual outer exits 0 at **15:37:34** and **15:39:58 UTC**, respectively.
Both retained source checks match before and after. The actual compiled
inventory also passed: the 2,186-name list, declared inventory and three-group
union are byte-identical at SHA-256
`6fd7093a8121b06c4cf892df4d524172ba9fe3dca1173d65e4158d5d3ed8f213`.
This establishes complete selection, not execution of all those tests.
[Normal guards](peerreview/evidence/FINAL-2-aggregate-supplement-qualification-20260913/guards10-normal-p1-v2/report.json),
[race guards](peerreview/evidence/FINAL-2-aggregate-supplement-qualification-20260913/guards10-race-p1-v2/report.json),
[compiled inventory result](peerreview/evidence/FINAL-2-aggregate-supplement-qualification-20260913/compiled2186/status.json),
[exact execution plan](peerreview/evidence/FINAL-2-aggregate-supplement-qualification-20260913/QUALIFICATION-PLAN-v2.txt).
The four interrupted roots' three sequential uncached race confirmations
are complete on the same isolated source and binary. The first full confirmation
passed all four roots with outer and suite exits 0, in **4,001.935 seconds**.
Its 35 parsed events contain exactly four root passes, and its before/after
source records match at SHA-256
`e59585ab1e7068b3f2b77d9806324e4d6ec64fa1d67eeaa50b791373760a5615`.
The retained executable SHA-256 is
`f7df8d9246db832cfcafe3bb4466500535bebc2750dac4e5ea5e1ee8b388700c`.
The second confirmation completed at **17:54:07 UTC** with outer, native body
and evidence-replay exits 0, all four roots passing, and **3,976.640 seconds**
of body execution. Its before/after source records match, and the executable
hash is the same as the first pass. The third ran from **17:57:09 to 19:05:50
UTC**, with outer, list, body, events and replay exits **0**. All four roots
passed, with **4,110.962 seconds** of body execution and the same executable.
Its before/after source SHA-256 is
`6611f683e1e685a506a061537e229087e65f3222cea10daae43a29f008d471c2`.
This closes the three-pass confirmation obligation at its recorded scope;
the final candidate still needs a complete aggregate pass.
[Third confirmation terminal summary](peerreview/evidence/FINAL-2-supplement-race-p3-20260913/TERMINAL-SUMMARY.json),
[raw body](peerreview/evidence/FINAL-2-supplement-race-p3-20260913/e999/active4-race-p3/body.stdout),
[evidence replay](peerreview/evidence/FINAL-2-supplement-race-p3-20260913/e999/active4-race-p3/replay.stdout).
[Second confirmation events](peerreview/evidence/FINAL-2-focused-corrections-20260913/e999/active4-race-p2/events.stdout),
[second confirmation exits](peerreview/evidence/FINAL-2-focused-corrections-20260913/e999/active4-race-p2/outer.exit),
[first confirmation report](peerreview/evidence/FINAL-2-supplement-race-confirmations-20260913/p1/report.json),
[raw events](peerreview/evidence/FINAL-2-supplement-race-confirmations-20260913/p1/suite-active4-race-p1-events.stdout),
[actual outer exit](peerreview/evidence/FINAL-2-supplement-race-confirmations-20260913/p1/outer/outer.exit).
Preparation omits a duplicate
2,181-root execution; the next full aggregate owns that broad coverage.
Three earlier correction launch attempts were refused during source verification,
before compilation or any test body. Their error matches the helper's internal
30-second Git-command deadline; the specific operation was not recorded.
Staggered persistent-session launches passed the same source checks without
changing source, plans or limits. These attempts executed zero tests and are
preserved as refusals. [Original launch records](peerreview/evidence/FINAL-2-aggregate-supplement-qualification-20260913/prebody-refusals/).

The clean, published `e99954a` bootstrap CLI built successfully at **15:47:04
UTC**, with unchanged observations of all 12 repositories. Its native
release-lock review and apply both exited 0; apply completed at **15:49:37 UTC**
and installed exactly the reviewed YAML, SHA-256
`bd5e492077edc01acfa452d67ce1e437deec6d5e1add7ed8eab41dfd722b254f`.
Only the protocol-script digest changed, to
`sha256:f21b86f2ad38ab2aea7698190ed69cc6ac0fd19ab1e883720ab98a0529eb57c8`.
The runtime code, chain pins, other repository digests and spending limits
remain unchanged. This local update sent no chain transaction. A final stamped
CLI and corrected complete gates use the resulting publication, `cd036cee`,
which was pushed at **15:53:56 UTC**. The producer started at **15:57:41 UTC**
and the aggregate at **16:01:22 UTC** on that source with private test services.
Both were subsequently stopped and joined after the producer's actual failure
and the necessary reserve-repair source correction described below. Neither
gate passed on this candidate.
[Bootstrap build result](peerreview/evidence/FINAL-2-supplement-lock-20260913/bootstrap/RESULT-BOOTSTRAP-CLI-BUILD.json),
[reviewed candidate](peerreview/evidence/FINAL-2-supplement-lock-20260913/review/candidate.yml),
[native apply result](peerreview/evidence/FINAL-2-supplement-lock-20260913/apply/RESULT.json).

The final `cd036cee` native CLI build completed with build and outer exits 0 at
**16:00:37 UTC**. Its SHA-256 is
`3b76bbf3f43034ddc8c23d6248aa4e272bbbb44580e589df533ba27b6573d482`.
The executable carries that clean VCS revision; all 12 repository observations
match before and after. Two read-only setup reconstructions completed with
exit 0 at **16:07:16 UTC** and agree on plan
`0x814d362c650dcdb86f1a57e4f266acd789e6be5703c9e65ad753576199bc3358`.
Their only pair difference is generation time. All 2,309 actions, spending
ceilings and limits match the preceding plan, including the approved 3,750-alpha
repair. This plan remains **unapplied**. The later 17:14 UTC native refusal
supersedes its earlier reserve feasibility; it is not currently executable.
These reads sent no transaction.
[Build result](peerreview/evidence/FINAL-2-native-admission-cd036ce-20260913/cli/RESULT-FINAL-CLI-BUILD.json),
[actual outer exit](peerreview/evidence/FINAL-2-native-admission-cd036ce-20260913/cli/capture/outer.exit),
[VCS build information](peerreview/evidence/FINAL-2-native-admission-cd036ce-20260913/cli/meta/cli.buildinfo.txt),
[exact setup comparison](peerreview/evidence/FINAL-2-native-admission-cd036ce-20260913/REVIEW-SETUP-PAIR.json).

The preceding `90f67b1` native CLI build completed with actual build and outer exits 0 at
**12:31:52 UTC**. Its SHA-256 is
`9b42372c2ff768a21d7d117d716dcb4dfe00be422c8570880a423708a8a3d6e2`;
its clean VCS stamp names that revision, and all 12 repository observations
match before and after the build. [Build result](peerreview/evidence/FINAL-2-native-admission-20260913/cli/RESULT-FINAL-CLI-BUILD.json),
[actual outer exit](peerreview/evidence/FINAL-2-native-admission-20260913/cli/capture/outer.exit),
[before](peerreview/evidence/FINAL-2-native-admission-20260913/cli/meta/repos.before.tsv)
and [after](peerreview/evidence/FINAL-2-native-admission-20260913/cli/meta/repos.after.tsv).

Two read-only setup reconstructions completed with exit 0 at **12:36:58** and
**12:37:00 UTC**. Both bind plan
`0xd4525b8da2da4f786f4990beb2285ac09b42e033c3c7fdd3c45473a7c9336507`
and contain the same 2,309 action objects, including the approved 3,750-alpha
repair. Their only pair differences are generation time and observations of
adjacent finalized heads; the native plan hash explicitly excludes those
moving fields and apply rechecks them. Their total spend ceilings, including
superseded actions, are 185.748236 TAO, 35,000 alpha, 180 EVM TAO and 262
registrations, with no subnet creation. These are approved ceilings, not new
spending. [Exact comparison and native execution records](peerreview/evidence/FINAL-2-native-admission-20260913/REVIEW-SETUP-PAIR.json).
Setup apply started at **14:32:17 UTC**, after the producer passed. It adopted
this plan locally at **14:36:28 UTC** and began authenticating carried history.
After the aggregate failure, the operator interrupted it at **15:00:54 UTC**;
the process joined with exit 1 and an explicit `context canceled` error.
The post-stop journal still contains 10,258 entries, with **zero entries for
this plan or the additional reserve repair**. No new transaction or repair
credit resulted. Preserve the adopted plan and all prior history for supported
recovery. The original invocation's preparation label is retained; its actual
start, terminal result and cancellation records establish what ran.
[Native result](peerreview/evidence/FINAL-2-setup-canceled-20260913/RESULT.json),
[stop reason](peerreview/evidence/FINAL-2-setup-canceled-20260913/STOP-REQUEST.json),
[stderr](peerreview/evidence/FINAL-2-setup-canceled-20260913/setup.stderr),
[post-stop journal observation](peerreview/evidence/FINAL-2-setup-canceled-20260913/POST-STOP.json).

A fresh complete 256-UID reserve census at **11:25 UTC**, finalized native
block **7,996,371**, found **82,639.777928818 alpha** of registered stake and
**50,180.168141913 alpha** at reserve UID 254: **60.7215670220%**. The block hash
is `0x878ff4aeb7cb63cf858b1287d834cf23c705bdbeee85da1d497ebe90b3e56f15`.
At that snapshot, the approved 3,750-alpha transfer between registered hotkeys,
allowing one alpha-rao of rounding, projects **65.2593333302%**. This is a
projection, not a credited repair or a current target pass; ongoing emissions
change the denominator. A finalized debit/credit and another complete census
remain necessary. [Raw requests and responses](peerreview/evidence/FINAL-2-reserve-before-20260913/rpc.json),
[decoded census and calculation](peerreview/evidence/FINAL-2-reserve-before-20260913/SUMMARY.json).


A later complete census at **12:41:51 UTC**, native block **7,996,753**,
found **82,830.982610680 alpha** of registered stake and
**50,256.924044690 alpha** at the reserve: **60.6740647771%**. At that
snapshot the same approved transfer projects **65.2013562347%** after allowing
one alpha-rao of rounding. It is still unapplied; neither observation proves
the 65% repair target. The observations use the LAN node at one finalized hash
per census and do not include replayed storage proofs.
[Raw later census](peerreview/evidence/FINAL-2-reserve-before-1241-20260913/rpc.json),
[decoded values and projection](peerreview/evidence/FINAL-2-reserve-before-1241-20260913/SUMMARY.json).

A fresh complete census at **15:10:42 UTC**, native block **7,997,497**, hash
`0xa27f1b7924405971de344e295faafdc80c8c0454c1b1a2a659b2e923900a8104`,
found **83,213.547287627 alpha** registered and **50,410.350083881 alpha**
at reserve UID 254: **60.5794990444%**. The still-unapplied approved transfer
projects **65.0859768022%** at this snapshot. It remains a projection, with
the same finalized-credit and complete post-repair census requirements.
[Raw 15:10 census](peerreview/evidence/FINAL-2-reserve-before-1510-20260913/rpc.json),
[values and projection](peerreview/evidence/FINAL-2-reserve-before-1510-20260913/SUMMARY.json).

At **16:09:29 UTC**, the complete 256-UID census at block **7,997,791**, hash
`0x2ba78a99ee6ccdd6311b0628e9f4baadb89e01f39cd77a5c877ec2461928a0d4`,
found **83,404.906730330 alpha** registered and **50,487.020157029 alpha** at the
reserve: **60.5324340452%**. The approved repair projected **65.0285723985%**
after allowing one alpha-rao of rounding. This narrow margin is a snapshot,
not assurance that a later transfer reaches the target. Native planning and
signing check the fresh projection, and the finalized postcondition requires
65%. The 60% operating floor does not satisfy the repair target. There is no
additional spending authority beyond the then-approved 35,000-alpha lifetime cap.
[Raw 16:09 census](peerreview/evidence/FINAL-2-reserve-before-1609-20260913/rpc.json),
[values and projection](peerreview/evidence/FINAL-2-reserve-before-1609-20260913/SUMMARY.json).

A fresh feasibility read at **16:43:28 UTC**, block **7,997,961**, hash
`0x965aad0dc92e4370a83b7f44e73c1eddafa487373a2da1ba3bd7cc60e8901f27`,
found the same stake values and projection. The approved repair was still
sufficient at that snapshot. [Raw read](peerreview/evidence/FINAL-2-reserve-before-1643-20260913/rpc.json),
[snapshot feasibility](peerreview/evidence/FINAL-2-reserve-before-1643-20260913/SUMMARY.json).

At **17:10:00 UTC**, finalized block **7,998,093**, hash
`0xee143397cadcb2346644daf53bb47ef2d7d7f29f90fc1ed17fe632b66fac8498`,
the complete 256-UID census found **83,596.318926064 alpha** registered and
**50,563.662596936 alpha** at the reserve: **60.4855132935%**. After the
approved 3,750-alpha transfer with one alpha-rao of credit allowance, the
projection is **64.9713567471%**, below the 65% repair target. The minimum
single transfer at that snapshot is **3,773.944705007 alpha**. These are
finalized-node reads and arithmetic, not a submitted transfer or replayed
storage-proof result.
[Raw 17:10 census](peerreview/evidence/FINAL-2-reserve-before-1710-20260913/rpc.json),
[complete values and projection](peerreview/evidence/FINAL-2-reserve-before-1710-20260913/SUMMARY.json).

A fresh native read-only setup ran from **17:11:35.329494371** to
**17:14:23.011772741 UTC** and exited **1**. It refused an additional
23,944,705,008 alpha-rao after reserving the existing pending transfer against
the 35,000-alpha lifetime ceiling. Its one-alpha-rao rounding margin explains
the difference from the single-transfer minimum above. No `--apply` or plan
adoption was requested, and the original journal remained unchanged.
[Exact native invocation and refusal](peerreview/evidence/FINAL-2-reserve-refusal-20260913/RESULT.json),
[stderr](peerreview/evidence/FINAL-2-reserve-refusal-20260913/stderr).

Source review also found a repair-revision defect: if the pending 3,750-alpha
repair becomes insufficient, the planner carries it unchanged and may append
another repair after it when a larger allowance is available. The first repair
still independently requires 65% before signing, so that dependency can prevent
the later repair from executing. The pending repair has no journal entries;
a narrow correction, `6311cb8`, removes only an insufficient terminal repair
with no journal entry from the active replacement chain. The original plan,
intent and history remain archived; any matching action-ID or intent-hash
journal row prevents retirement. Credited liabilities remain charged and new
repair IDs are never reused. The old source plus the regression produced the
expected native body exit 1, with the literal failure that the insufficient,
never-started repair still blocked the revised chain. The corrected source
passed all **30 selected roots and 12 subtests**, normally and under race;
outer and native suite exits were 0, and the source snapshots were unchanged.
The race qualification closed at **17:48:04 UTC**. Two earlier launch attempts
were refused before compilation because of private-path and vault-projection
mismatches; those records are retained and executed zero tests. That qualified
correction was later included in the published replacement plans described
above. The original journal and credited transfer were preserved.
[Causal result](peerreview/evidence/FINAL-2-reserve-succession-qualification-20260913/captures/causal1-normal-p1-retry2/report.json),
[normal result](peerreview/evidence/FINAL-2-reserve-succession-qualification-20260913/captures/integration30-normal-p1-retry2/report.json),
[race result](peerreview/evidence/FINAL-2-reserve-succession-qualification-20260913/captures/integration30-race-p1-retry2/report.json),
[complete evidence manifest](peerreview/evidence/FINAL-2-reserve-succession-qualification-20260913/MANIFEST.sha256).

The user subsequently approved **37,250 alpha lifetime**, permitting **one
6,000-alpha replacement instead of the unsubmitted 3,750-alpha repair**.
The prior charge is 31,250 alpha; the per-repair maximum remains 6,000 alpha
and the source minimum remains 2,000 alpha. The one-setting vault change is
committed and pushed at `8b2f481dbe87092d0c1742274712a6f805c1c375`.
It was prepared in an independent checkout so the running race confirmation's
old source remained unchanged. At the 17:10 census, a 6,000-alpha transfer
allowing one alpha-rao of rounding projects **67.6628628193%** reserve share.
That projection is not a transfer or target pass: a fresh native plan, actual
finalized debit/credit and complete post-transfer census remain required.
[Approval and published setting receipt](peerreview/evidence/FINAL-2-approved-reserve-allowance-20260913/CHANGE.json).

The producer on `cd036cee` timed out in its **capture-private normal** phase
after **300.104 seconds**, at **17:13:49 UTC**. Three of seven selected roots
were active in `fsync` while creating thousands of tiny private fixture files.
The other four outcomes are unclassified in the nonverbose output; the race
command was never dispatched. The test-only correction `f6cfd797` preserves
all 1,000 miners and real manifest, mode, content and archive checks, while
removing unnecessary durable commits for opaque temporary placeholders.
Production writes and both deadlines remain unchanged. All **seven selected
roots passed normally and under race**, with outer/native exits 0, unchanged
source and 59 parsed events in each run. Normal body time was **99.711 seconds**;
race body time was **121.477 seconds**. The first normal run supplies the first
confirmation for the three timeout roots. Those three roots then passed two
further sequential executions on the same normal binary, closing at
**17:52:24 UTC** and **17:56:25 UTC**. Source snapshots match before/after;
the retained mapping explains the equivalent JSON and TSV snapshot formats.
This closes the fixture correction's scoped qualification; the combined
candidate still needs the full release gates.
[Normal result](peerreview/evidence/FINAL-2-focused-corrections-20260913/f6/capture7-normal/report.json),
[race result](peerreview/evidence/FINAL-2-focused-corrections-20260913/f6/capture7-race/report.json),
[second confirmation events](peerreview/evidence/FINAL-2-focused-corrections-20260913/f6/active3/p2/events.stdout),
[third confirmation events](peerreview/evidence/FINAL-2-focused-corrections-20260913/f6/active3/p3/events.stdout),
[source-format mapping](peerreview/evidence/FINAL-2-focused-corrections-20260913/f6/active3/SOURCE-FORMAT-MAPPING.json).
[Original timeout](peerreview/evidence/FINAL-2-producer-cd036ce-20260913/capture-private.log),
[selected failure scope](peerreview/evidence/FINAL-2-producer-cd036ce-20260913/RESULT.json).

After that failure and identification of the required production correction,
both superseded gates were stopped through their owned cleanup. The producer
joined at **17:19:25 UTC**, outer exit **143**, with **23 passed phases, one
failed phase and three interrupted phases**. The aggregate joined at
**17:19:27 UTC**, outer exit **143**, with **five passed phases and two
interrupted phases**; no aggregate product failure had been observed.
Intentional interruptions are not new failed-test confirmation obligations.
These incomplete gates are preserved and do not qualify the next candidate.
[Producer raw joins](peerreview/evidence/FINAL-2-producer-cd036ce-20260913/capture/outer.stdout),
[aggregate raw joins and terminal classification](peerreview/evidence/FINAL-2-aggregate-cd036ce-20260913/RESULT.json).

The September 13 integration also brought matching Server, Connect and SDK
dependencies. Server's module graph requires Warp. The reviewed integration
`02ba4c7` adds Warp to current source admission and the explicit local module
replacement while preserving archived lock formats. The current graph therefore
has **13 repositories and 16 live Go modules**. Terra's formatting inspection
was clean. Module reconciliation found one metadata-only omission in Proxy:
the indirect qpack requirement and its two checksum rows. Those three additions
are published at `6204ae7df2a9868bbb3a7b61231917a36e4f5c9f`; all 16 module
metadata checks are now clean. The exact **24-root integration passed in both
modes**: normal closed at **18:40:04 UTC**, race at **18:41:54 UTC**, each with
24 root passes, zero descendants, 99 events and native/outer exits 0. The
before/after 13-repository observations match. Their actual bodies took
14.103 and 16.258 seconds; these are selected integration results.
[Normal report](peerreview/evidence/FINAL-2-warp-admission-qualification-20260913/normal/report.json),
[race report](peerreview/evidence/FINAL-2-warp-admission-qualification-20260913/race/report.json),
[portable manifest](peerreview/evidence/FINAL-2-warp-admission-qualification-20260913/SHA256SUMS).
Earlier qualification results retain their recorded source/dependency scope.
[Proxy reconciliation and publication](peerreview/evidence/FINAL-2-proxy-module-reconciliation-20260913/PUBLISHED.json),
[exact metadata diff](peerreview/evidence/FINAL-2-proxy-module-reconciliation-20260913/proxy-module.diff).

Published candidate `e0a454248051a24d81c11d166054570cb4d20b5e` includes those
qualified corrections and their evidence. Its clean, VCS-stamped bootstrap
driver built with exit 0 at **18:48:57 UTC**, SHA-256
`732e34ede35692082c3d4f87c4e03553c5fc75ad92b58d2d11208aa416667de4`.
A native release-lock preview passed at **18:54:36 UTC**. The apply attempt
refused the private SN checkout's detached upstream metadata before changing
the lock. Repairing that metadata and fetching canonical branches exposed
newer unrelated dependency commits; the qualified local source bytes remain
unchanged and are proven ancestors of their canonical branches. The narrow
source-freeze correction `17286251723b05a00717a642a6b5141cb02c2e12`, integrated
as `c4185f7`, qualifies those recorded dependency commits while preserving exact
current-main attestation for SN and exact before/after source snapshots.
All **12 affected checks passed normally and under race**, with outer/native
exits 0, 12 root passes, zero descendants and 51 events per mode. Normal closed
at **19:24:06 UTC**, race at **19:25:14 UTC**; their bodies took **20.504** and
**22.211 seconds**. All four before/after source records have SHA-256
`ac3fda7c5435d35880008337d5b7b808af607dd97c80161c2abf7c00c5f45ff8`.
The new behavioral control covers canonical main/master advancement without
changing the recorded candidate, and rejects unpublished, divergent or
rewound-out dependency history. No chain transaction resulted from this
qualification.
[Normal correction report](peerreview/evidence/FINAL-2-dependency-ancestry-qualification-20260913/normal/report.json),
[race correction report](peerreview/evidence/FINAL-2-dependency-ancestry-qualification-20260913/race/report.json),
[exact patch and declarations](peerreview/evidence/FINAL-2-dependency-ancestry-qualification-20260913/handoff/handoff.json),
[source and evidence manifest](peerreview/evidence/FINAL-2-dependency-ancestry-qualification-20260913/PORTABLE-MANIFEST.json).
[Bootstrap result](peerreview/evidence/FINAL-2-bootstrap-e0a4542-20260913/RESULT-BOOTSTRAP-CLI-BUILD.json),
[reviewed preview](peerreview/evidence/FINAL-2-lock-preparation-e0a4542-20260913/release-lock-e0a4542-20260913T185435Z/REVIEW.json),
[actual apply refusal](peerreview/evidence/FINAL-2-lock-preparation-e0a4542-20260913/release-lock-e0a4542-20260913T185435Z/apply/RESULT.json),
[canonical ancestry observations](peerreview/evidence/FINAL-2-lock-preparation-e0a4542-20260913/private-canonical-tracking-20260913T1908Z/ANCESTOR-TRACKING.json).

Candidate `29be68fdf1e201f9622aedb5caef6fa180ff8fe5` was published cleanly at
**19:28:12 UTC**. Its fresh stamped bootstrap driver built with exit 0 at
**19:30:10 UTC**, SHA-256
`723be7b9c88a2ef834d56a5b7e1d2c8b8431940df73b7dae4d32855867a74658`.
The 13 before/after repository records match. Native preview reproduced the
same nine reviewed repository fields, and **native lock apply exited 0 at
19:32:03 UTC**, installing exactly
`ddb22d0e1e525affac5b87cbba29cc70cb8d3e4afb9668507033d6fee907b51c`.
Runtime, contract artifacts, interfaces and infrastructure fields are unchanged.
This local lock update sent no chain transaction. Publication, the final
driver build and complete gates followed in the later checkpoints above;
this historical lock record alone establishes none of those results.
[Published candidate](peerreview/evidence/FINAL-2-ancestry-lock-20260913/publication/RESULT.json),
[driver build and identity](peerreview/evidence/FINAL-2-ancestry-lock-20260913/bootstrap/RESULT-BOOTSTRAP-CLI-BUILD.json),
[exact lock review](peerreview/evidence/FINAL-2-ancestry-lock-20260913/native/REVIEW.json),
[actual native apply](peerreview/evidence/FINAL-2-ancestry-lock-20260913/native/apply/RESULT.json).

The next run must retain both complete gate results on its final candidate,
five accelerated epochs, the activated production policy and three consecutive
complete production epochs, both validators' fresh native applications,
required traffic/proof/adversarial and lifecycle evidence, final accounting,
secretless pinned replay on the owned LAN node and actual shutdown results. This section will link their
terminal artifacts as they become available. Pending or failed work stays
visible; this report cannot establish acceptance until that evidence exists.
