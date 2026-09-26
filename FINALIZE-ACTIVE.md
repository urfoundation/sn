# Testnet execution plan

## R47 first measured epoch closed — 2026-09-26 14:51 UTC

The LAN chain crossed the epoch-651 close at block 8,090,974. The active R47
owner then retained its first epoch-652 observation at finalized block
8,090,975, with complete epoch-651 operator usage of 19,396,509 and
18,756,754 bytes and `policy_rate_readiness.ready=true`. Both operator
epoch-651 `RootCommitted` events finalized at block 8,090,980; each receipt
has status `0x1` and its artifact hash matches the observed source hash.
[Exact observation and LAN receipts](sim-testnet/peerreview/evidence/FINAL-3-R47-measured-start-20260926/README.md)
are pushed. These facts prove source closure and payout-root commitments,
not entitlement, claims, native steering, or final acceptance.

At this checkpoint the retained applied native steering intent for each
validator is still epoch 1690. Native epoch 1691 had ten failed attempts per
validator during the dependency outage; validator 1 then logged repeated
`compact head EMA epoch jumped` in native epoch 1692. The owner remains active
without a signed invalidation and continues recording strict process findings.
Do not restart it to insert a candidate fix; retain this gap as a terminal
finding and let all remaining measured epochs and settlement evidence run.
The integration branch has a tested successor transport fix (`d9cd83a6`),
and isolated EMA-gap diagnosis is in progress. The ordinary simulator test
partition exposed a separate cancellation/Close error-loss regression now
under repair; it is not a pass claim.

## R47 measured interval active — 2026-09-26 14:01 UTC

The sole release owner is `urnetwork-sim-release-r47.service` (PID 1255308 at
this checkpoint); fleet supervisor `urnetwork-sim-ur-subnet-testnet-v1.service`
remains active. The signed R47 boundary has not been invalidated. Its five
measured epochs are 651–655, first block 8,090,674, last block 8,092,174,
with terminal settlement target 8,092,324. LAN RPC
`http://192.168.1.162:9944` finalized block 8,090,749 at this checkpoint.
Do not restart the owner merely to apply later code changes or repair soft
errors; it is executing the real fault schedule and must reach terminal
evidence even if strict acceptance fails.

Epoch 650 closed with operator usage 52,450,233 and 50,903,383 bytes; the
owner's authenticated rate check clears twice the native transfer minimum at
every tier. Both payout roots and artifact hashes were committed in finalized
block 8,090,680 with successful receipts. Exact LAN on-chain logs, receipts,
hashes and peer-review instructions are in the
[R47 measured-start bundle](sim-testnet/peerreview/evidence/FINAL-3-R47-measured-start-20260926/README.md).
The owner's epoch-651 observation also reports nonzero deposits for both
operators. These facts establish live measured activity, not terminal
settlement or final acceptance.

The quality-cohort fault activated and restored; both head-boundary faults
and the first fleet-prune pair activated. Postgres-1 fault restored and Redis-1
was active at this checkpoint. The owner continues to record transient RPC
adversary errors from its signed ten-second sample limit and provisional
process-log findings. It also retries transient operator GET failures across
its five-minute budget. Keep these exceptions visible in `FINAL-3.md` and
verify the later fault restores, all five epochs, terminal settlements,
conservation, on-chain receipts, and independent replay before any completion
claim. Candidate 60-second RPC retry and truthful timeout diagnostics are
pushed on `codex/r46-integration-20260926` through `867a13d7`; they are not
in the signed live executable.

At 14:18 UTC, the owner remained active with no signed invalidation. Its
observations at 14:12:18 and 14:16:41 UTC reached finalized blocks 8,090,803
and 8,090,826 in epoch 651, including the owned-RPC-path fault window.
The two PostgreSQL, two Redis and owned-RPC-path fault windows have all been
marked restored by the signed fault controller. During Redis-2's outage,
both validators logged HTTP 500 on immutable attempt uploads with a local
Redis connection refusal, followed by timeout/cancellation attempts. Their
post-restore native steering recovery and eventual epoch settlement remain
unverified. The process-log gate retains those failures provisionally while
the owner continues. The integration branch's ordinary Go test partition is
still running at low priority; its result is not yet a pass claim.

## R45 sealed failure and retained recovery — 2026-09-25 16:56 UTC

The owner exited 1 after sealing `runs/20260925T134346.277250758Z-release-1.0/result.json`:
five of six assertions failed, `final_acceptance=false`. The last completed
owner observation was epoch 629 at finalized block 8,084,357, before the
signed first measured block 8,084,374. The chain crossed that block, but the
owner did **not** produce a measured epoch-630 observation. The actual stop
was `heartbeat process log gate`, with five release-blocking finding rows;
validator-2's native attempt also reported `compact head EMA epoch jumped`.
The signed recovery-45 envelope invalidated acceptance at 16:55:38 UTC with
`execution-exited-before-completion`. Preserve the signed start as history,
not accepted progress. Two validator-view filters remain active in the exact
fault ledger, quality-cohort was restored, and the supervisor is still active.
A subsequent read-only filesystem check found no live active-fault or operator
view-filter files; this does not rewrite the historical terminal fault state.
Do not manually edit fault or process evidence or stop that supervisor.
Both operator scenario bundles were published according to the sealed result.
The exact result, fault ledger, process-log snapshot, invalidated envelope
and steering receipt are in the R45 portable bundle. Isolated provisional
continuation and strict replay fixes are under qualification; the next owner
must authenticate and continue retained actions without repeating setup.

## R45 release boundary signed — 2026-09-25 15:52 UTC

The live R45 owner has signed `campaign-start.evidence.json` at 15:52:25 UTC
after matching both epoch-628 artifacts to nonzero on-chain roots and recording
the bounded provisional low-usage deferral. Its acceptance baseline is
finalized block 8,084,084; measured epochs are 630–634, starting at block
8,084,374, ending at 8,085,874, with terminal block 8,086,024. The exact
signed envelope and baseline observation are in the R45 portable bundle.
The owner remains the only writer. Do not restart it for the isolated fixes.
The LAN RPC finalized the signed first measured block 8,084,374 (hash
`0x00bb53a7d661754e4d66df69cab503eb7a905990f1a2d06d1816c4605493d5c6`,
timestamp 16:45:36 UTC). The interval is underway on-chain; preserve the
owner's later post-start observations before claiming measured behavior.
Final acceptance remains open, including governance drill, relay horizon,
publication capacity and the scoped R44 exception.
The read-only full active-epoch object census has since measured 428 retained
objects and 244,049,506 bytes during 14:46:05–15:51:00. Bucket headroom was
33,969,516,444 bytes; a doubled five-hour projection at the measured rate is
2,255,656,538 bytes, giving about fifteen times that doubled margin. This
supports the observed-workload byte gate, subject to the listing's lower-bound
limit for overwritten, deleted or rejected writes. The pinned API request-rate
advisory remains a separate unclosed strict check. Portable summary is in the
R45 bundle; raw listing stays on `/mnt/data`.

## R45 preparation record — 2026-09-25 13:50 UTC

Round-7 fleet renewal completed with 202 fleets and 1,212 finalized,
postcondition-verified actions. Its
[portable result](sim-testnet/peerreview/evidence/FINAL-2-R45-renewal-20260925/README.md)
includes the journal summary and direct LAN-RPC sample receipt. Retained
resume exited 0 with `setup_actions_dispatched=0`. The single owned
`urnetwork-sim-release-r45.service` is active, authenticated all 44 prior
recovery generations, and signed recovery-45. At this earlier cut acceptance
had not started;
do not launch a second writer or treat the signed attempt as completion.
Before its acceptance boundary, remeasure `blob` storage growth with active
epoch-628 bindings and require a twofold projected-growth margin. Preserve
R44's failed owner verdict and scoped R44-LC-1 exception without widening it.
Both resumed-configuration operator probes passed two exact content/history
readbacks at 13:52 UTC, and the live 64 GiB quota was re-read. The admin usage
snapshot was stale; this does not close the active-epoch storage gate.
The controller logged a provisional, nonblocking protected-publication
forecast shortfall at 13:54 UTC: per replica 32,768 versus 34,553 objects/hour
and 8,388,608 versus 10,947,548 retry requests/hour. Byte capacity passes.
The active limits were not changed; retain the advisory in terminal evidence
and do not treat its waiver as acceptance. Do not mutate the pinned config
under the current owner.
The read-only 14:07 UTC object listing established a 34,505,448,693-byte
baseline and 7,658,670 bytes of preactivation writes. It does not close the
active-binding storage gate. The owner also logged an elapsed relay horizon
forecast and deferred public census; the latter remains required at final
audit. The owner and supervisor are still running.
At 14:08 UTC the owner verified precompile preparation but logged
`governance_drill_startup_waived=true`; its expected public drill evidence was
absent at the later read cut. This is an open strict acceptance failure, not a
reason to interrupt the current release owner before it yields terminal
evidence.
The owned parallel public evidence census finished at 14:32 UTC and logged
`public_census_audit_passed=true`, `pending_public_census=false`. Keep its
result distinct from final acceptance and the remaining capacity, horizon,
governance and active-binding storage gates.
The LAN RPC finalized renewal activation block 8,083,774 at 14:46 UTC,
hash `0x6f6284b275845a8234033f46d5fec486d05294dfbbbb5cf320a0105bf473dc25`.
Fresh admin reads confirmed 64 GiB quota and 464,104,980,480 bytes of healthy
disk availability; usage was cached. Await the owner's next observation for
fleet-binding validity and the independent active object census before
claiming the storage gate or acceptance boundary.
The independent 14:50 UTC full object listing counted 162,366 objects and
34,505,454,565 bytes, with only 5,872 bytes in the first activation minutes.
Keep the active full-epoch growth gate open; this short baseline is not a
healthy-run projection.
The first post-activation observation at finalized block 8,083,792 reports
supervisor healthy and 808/808 fleet bindings valid. Policy-rate readiness is
still false on complete epoch 627 (operator 1 zero bytes/zero tao-rao versus
200,000-rao twice-native threshold), with no provisional low-usage deferral.
Do not count this as a signed acceptance boundary. The exact observation is in
the R45 portable evidence bundle.
At finalized block 8,083,834, canonical LAN-RPC calls proved that both epoch
627 root commitments are zero after their block-8,083,824 commit deadline.
The workers correctly produced no roots for the zero-leaf epoch, while the
current provisional rate deferral requires a payout root from that same
rate-source epoch. This is a pre-acceptance compatibility finding. Preserve
the active owner and its checkpoints; observe epoch 628 and prepare the
isolated fix without changing the pinned binary or treating R44-LC-1 as a
waiver for this separate issue. The receipt is in the R45 portable bundle.
Epoch 628 has now closed. Exact canonical LAN-RPC reads at finalized block
8,084,091 show both operator roots and artifact hashes nonzero, committed at
8,084,080. At this read cut the owner postboundary match and acceptance
boundary were still pending; both are documented at the top of this file.

## R44 sealed result and R45 renewal — 2026-09-25 12:16 UTC

R44's original owner exited at 11:50 UTC after sealing
[its final result](sim-testnet/peerreview/evidence/FINAL-2-R44-terminal-20260925/owner-result.json)
(SHA-256 `b631ca4cd6f8fca591497770f2d066a6a568cc84fe388e08ca6e00f3ccf18c46`):
85 of 182 assertions failed, `result=fail`, `final_acceptance=false`,
finalized head 8,082,861. The five release epochs did finish; no signed
completion or `complete.json` exists. R44-LC-1 records only the bypassed
lifecycle mutation and companion filter's failed early-restore condition;
the hard restore at block 8,082,634 and all other failures remain visible.
The post-owner read-only terminal diagnostic completed with 37 checks:
14 pass, 14 fail, one named exception and eight unavailable. Its
[report](sim-testnet/peerreview/evidence/FINAL-2-R44-terminal-20260925/post-owner-diagnostic.json)
has SHA-256 `80b56cd76704b126ac486a431883b98054f3e6a3289693bd6015e551149b1ddf`.
It was still running after the fleet stop and successor-plan archive, so late
source-health and receipt failures are post-stop availability findings. The
sealed R44 result remains the authority for original-owner acceptance.

The `blob` bucket was above its 32 GiB hard quota during R44 terminal
publication. A bounded admin update raised it to 64 GiB, read back exactly,
with no object or lifecycle mutation. Both operators' new preflight envelopes
then passed two POSTs and exact content/history readbacks using the pinned
handler. The underlying HTTP 400 body was not retained, so the quota is the
strongest evidenced shared-store cause, not a proven original response. The
[quota receipt](sim-testnet/peerreview/evidence/FINAL-2-R44-terminal-20260925/blob-quota-expansion.json)
and [operator probes](sim-testnet/peerreview/evidence/FINAL-2-R44-terminal-20260925/README.md)
are prospective repair evidence; R44 acceptance stays failed. Persist the
64 GiB quota in configuration before any storage redeploy.
The [object census](sim-testnet/peerreview/evidence/FINAL-2-R44-terminal-20260925/blob-growth.json)
measures about 1.326 GB created during R44 and a 0.300 GB peak complete
hour, leaving about 34.225 GB beneath the new quota. Quota refusals and
expired bindings censor this rate; remeasure after renewed bindings become
active and require a twofold projected-growth margin before another full
interval. Do not treat the historical rate as a healthy-run ceiling.

The guarded fleet stop exited 0 after R44 ended, preserving on-chain state.
Vault's reviewed alpha-ceiling edit was committed as `df003713` without
changing the retained plan. Clean SN `6100394b` plus Connect `c98eb715`
built binary SHA-256 `195c12551e72010d868d2e6b3689aa7b17ba58e44eeb7169b37c5986b1bc7491`;
its external-copy doctor has 66 checks, zero hard failures and `ready=true`.
The round-7 plan `0x8bb92697db8f2164e46f6e58848d3407e509382fb61550b919f1d55391ad480e`
contains 202 renewal fleets, 808 signed bindings and 1,212 new actions for
epochs 628–659, no revocations, and remains within the approved 512 TAO /
512 EVM / 47,000 alpha ceilings. The first two exact-plan apply services
failed doctor on incomplete systemd/PATH environments before any round-7
journal action. The corrected owned user service
`urnetwork-sim-r45-round7-renew-envfull.service` adopted the same plan and
its first ten commitment transactions finalized at block 8,083,024, hash
`0x504d3158e83192c4d83ccd18baba0fd4f2a0da2a425b9ab97aff64056a7f9456`,
with ten retained postcondition verifications at that read cut. The completed
renewal and retained resume are recorded in the newer section above.
Its hash-pinned launcher is
`/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/run-round7-apply-6100394b.sh`.

## R44 fourth terminal diagnostic complete — 2026-09-25 11:09 UTC

The read-only `da7689f8` service exited successfully. Its
[report](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/fourth-terminal-da7689f8/report.json)
has SHA-256 `affad93c70b533e1a55f6e439dcec89f5243f5957d6ad76dfbd7ed984678d736`.
Of 38 checks, 18 pass, three fail, one is the named exception, one is a
finding, and 15 are unavailable. The longer exact-chunk capture passed
validator-2 signed-source and native-application coverage; exact relay
readback lacks its historical request owner. Validator 1 still lacks a compact
input journal. Terminal assertions, original process logs and fault timing
remain failed; they were evaluated before the companion hard restore and are
not silently recomputed. Original R44 owner PID 2823030 remains active with
no sealed result; the post-owner watcher remains armed. No diagnostic grants
final acceptance.

## R45 payout-window and fixture repairs integrated — 2026-09-25 11:04 UTC

Main now includes payout-window commit `f673ca9a` and validator-fixture commit
`6a407a1f` after the reviewed core successor stack. The payout reader binds
cohort and tier checks to the signed accepted epochs and matching root/hash;
it cannot substitute a later epoch's artifact. Legacy R44 diagnostics now
report missing additive claim-observation fields as unavailable instead of
inventing zero counts. The fixture repair updates stale policy/runtime
expectations and tests the 300-second deposit read deadline without sleeping.
Sol's focused and adjacent checks passed normally and under race detection on
the isolated patches, and the full fixture-tree validator normal suite passed.
Sol is checking their combined main source now; those results and the native
published-cut recovery patch remain outstanding before a final R45 build.
No R44 executable or live state was changed.

## R44 companion filter hard-restored — 2026-09-25 10:59 UTC

The live fault record shows `fleet-lifecycle-companion-prune` restored at its
scheduled block 8,082,634. The
[read-only observation](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/COMPANION-HARD-RESTORE.json)
has SHA-256 `c7a330fe494e0d8e8037b45f2fb060c7964bb2f98bf5b64aac5a7f7051f641f5`.
The bypassed mutation still supplies no terminal-effective epoch or
`RestoreConditionMet` proof, so R44-LC-1 stays an exception and strict
acceptance stays false. R44 owner PID 2823030 remains active, without
`result.json` or `complete.json`; the fourth read-only collector and post-owner
watcher remain active. Do not stop the owner merely because the filter restored.
Collect its sealed result and the independent reports when available; keep
all other failed checks visible.

## Qualified R45 recovery stack integrated — 2026-09-25 10:53 UTC

The eleven missing successor fixes are now on main through `c745d8b1` in
their reviewed dependency order. Sol independently passed focused and affected
adjacent simulator/validator selectors normally and under race detection on the
clean isolated `54079077` source. The 43-file
[source fence](/mnt/data/sn-testnet/qualification/r45-core-successor-20260925/source.sha256)
matches main byte-for-byte; the [review](/mnt/data/sn-testnet/qualification/r45-core-successor-20260925/REVIEW.md)
records each original patch, causal failure, conflict resolution and rollout
boundary. This integration changes no active R44 executable or live state.
The final R45 image still needs the separately qualified payout-window patch,
the native published-cut retry repair, validator fixture qualification and a
fresh post-R44 doctor/renewal plan. Do not infer live readiness from this source
integration alone.

## R45 composition audit found missing qualified recovery stack — 2026-09-25 10:38 UTC

Source comparison against current main `55454c52` found eight qualified
successor commits present in isolated history but absent from main:
`247c86d0`, `313fd977`, `38397969`, `f10fd309`, `2b78af30`,
`cc6fe1ba`, `9488bfb0`, and `b5211301`. They cover durable snapshot retries,
status transport errors, local validator authority, joined preparation,
exact process identity/termination, pre-termination fault intent, pending
container restore, and terminal RPC error attribution. The existing clean
`da7689f8` diagnostic image does not include this stack and must not be used
as the final R45 mutation runner merely because read-only diagnostics pass.
Astra is composing the dependency-ordered fixes from current main in an
isolated tree, reconciling overlaps such as the already integrated fault-process
wire field, and Sol will run affected normal/race and causal tests. The active
R44 owner and fourth read-only diagnostic remain unchanged.

## R44 fourth read-only diagnostic launched — 2026-09-25 10:16 UTC

Diagnostic-only capture resilience is integrated on main as `da7689f8` and
passed focused/adjacent normal and race tests plus two causal controls. It
raises the source-capture allowance to 60 minutes and reuses exact authenticated
chunks within one invocation; the strict final collector is unchanged. The
clean stamped binary has SHA-256
`25978b8478c6c02b9b7f2edce06e5209b260b22687e63b4f2ce16b13188b8509`,
revision `da7689f82fc80f791edb4d276406fe78671ef1e9`, and
`vcs.modified=false`. The hash-pinned wrapper is
`/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/run-fourth-terminal-diagnostic-da7689f8.sh`
(SHA-256 `1a0b030fc3c1725c1064fec39ee2f819f5346820a2dbad244af618efd2288e91`).
`urnetwork-sim-r44-fourth-terminal-da7689f8.service` is active and will write
to the new external `fourth-terminal-da7689f8` directory. Verify its eventual
service result and `report.json` before interpreting any check outcome. It is
read-only; the original R44 owner and its post-owner watcher remain active.

## R44 third read-only terminal diagnostic complete — 2026-09-25 10:00 UTC

The Git-stamped `d56709aa` diagnostic finished successfully as a read-only
collector; its 38-check [report](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/third-terminal-d56709aa-stamped/report.json)
has SHA-256 `38d7b721aaebc291918053635dd6ba9f014dc42c20c7b7654e457984c6185838`.
Sixteen checks pass, four fail, one is the recorded lifecycle exception, one
is a finding, and 16 are unavailable. Companion evidence capture and ordinary
signed payout artifacts pass. Strict lifecycle payout evidence remains
unavailable, and the active companion filter still fails strict timing.
Validator-2 signed-source capture exhausted its 15-minute stream-read budget;
validator 1 lacks its retained compact input journal. The terminal scenario
assertions and process-log report still fail independently. The original owner
remains active without `result.json`; no strict final acceptance is claimed.

The validator-2 capture work was quantified separately in
`/mnt/data/sn-testnet/qualification/r45-capture-stream-progress-20260925/read-only-census.json`:
62 cuts, 106 nonempty stream references, 94 distinct references, and about
1.63 GB of scheduled chunk GETs across two origins. All 102 native reads
completed before stream capture began. A known 4,187,969-byte chunk returned
HTTP 200 with its expected hash and size but took 18.30 seconds. A bounded,
diagnostic-only capture budget and verified-chunk reuse are being qualified;
they do not change the live owner or strict collector.

## R44 third terminal diagnostic authenticating retained evidence — 2026-09-25 09:47 UTC

The historical fault-process wire fix is on main as `d56709aa`. It restores
the optional `start_time_ticks` field in signed fault process records without
relaxing unknown-field rejection or giving forensic reads process-signaling
authority. Four focused and affected adjacent roots pass normally and under
race detection; the old reader reproduces the exact signed-checkpoint error.
A real-copy probe verified both original signed envelopes, their hashes and
36 retained tick proofs without changing the bytes.

The clean-clone diagnostic image is
`/mnt/data/sn-testnet/qualification/r45-fault-process-wire-20260925/build/sim-testnet-r45-d56709aa-clone-connectc98eb715`,
SHA-256 `171cb6e3b50e3110303a458a78b2930a9a76e193aeaf8a07d5889a1d32704a8d`.
Go build metadata reports revision `d56709aae03d3de2383d37615baedb4b86dfa391`
and `vcs.modified=false`; the external [binary manifest](/mnt/data/sn-testnet/qualification/r45-fault-process-wire-20260925/build/binary-manifest-d56709aa.json)
has SHA-256 `e8c344f789f7d31a57409c95d691511029ebc2b5787edf4b0cd35e812ae706b9`.
An earlier worktree-built image lacked a Git stamp and was refused by
attestation before creating a diagnostic output; it is not the selected image.

`urnetwork-sim-r44-third-terminal-d56709aa-stamped.service` is conducting a
read-only replay into a new external directory. It has authenticated the signed
start/checkpoint, all 44 retained recovery generations, the observation prefix,
the complete five-epoch terminal, both operators' current signed artifacts,
and both validator path/config checks. Strict terminal assertions still fail,
validator 1 lacks the retained compact input journal, and validator 2 capture
is in progress. This is not a final result. The R44 owner and its post-owner
diagnostic watcher remain active and unchanged.

## R44 second terminal diagnostic exposed retained attempt reader gap — 2026-09-25 09:13 UTC

The read-only diagnostic using composed successor `5c2ee88c` ran against the
same original signed R44 terminal checkpoint. Its independent
[`report.json`](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/second-terminal-5c2ee88c/report.json)
has SHA-256 `5449a651f5ffb841e55e7a08303ea74afde63d3a641c1891ac50db13ca3b3d23`.
It passed the signed start but failed `signed-latest-checkpoint` because the
new reader rejects the retained attempt's `start_time_ticks` field. The 30
dependent checks are unavailable in this second report; they are not evidence
that the previously authenticated first report changed. Preserve both reports,
fix the exact retained-format reader, and rerun into a third new output path.
The R44 owner and its armed post-owner diagnostic remain active and unchanged.

The composed source's diagnostic-only payout split is independently qualified:
four focused tests and affected adjacent selectors pass normally and with race
detection; the old-behavior causal overlay fails at the missing lifecycle index
as expected. Its three-file source hash fence matches the committed patch.
This qualification does not overcome the checkpoint reader failure or grant
strict acceptance.

## R45 capture contract-address fix qualified — 2026-09-25 08:50 UTC

The R44 terminal diagnostic's validator-2 signed-source capture failure was
caused by comparing checksum-cased config EVM addresses with lowercase
measurement addresses as raw strings. The production fix `26038272`
compares validated 20-byte addresses in capture, client-key request admission
and artifact observation; signed measurement bytes remain unchanged. Focused
normal/race (0.319/1.814 seconds) and affected adjacent normal/race
(76.580/119.246 seconds) pass. An old-production overlay reproduces both
positive-case failures. A pre-existing stale `Http 500` test assertion was
corrected separately in `d17fa421` and passes in both adjacent modes. All
seven committed file bytes match the independently qualified isolated source.
This code is not in R44's pinned executable or the older `2662f5f0` successor
image. A further companion read-only authority fix and a newly stamped clean
image are required before R45 adoption or a fresh diagnostic replay.

## R45 retained-renewal reader race qualification complete — 2026-09-25 08:39 UTC

The frozen round-7 historical authority fix in main `bbd33365` has passed
focused normal and all three focused race cases: generation/inventory/launcher
1,842.99 seconds, source-role retained approval 1,904.69 seconds, and negative
history/custody 32.31 seconds. Affected adjacent normal/race tests also pass,
and both old-reader causal overlays fail at the intended prior rejection.
The combined race command hit its 60-minute package bound because its two
positive fixtures alone take more than 60 minutes; independent bounded runs
passed without race findings. The clean `2662f5f0` image and 66-check external
doctor below are valid for this reader fix. New terminal-diagnostic findings
have exposed separate capture-reader issues under qualification, so do not yet
select that image for live R45 renewal or resume. R44 remains active.

## R44 signed terminal reached; external supplement retained — 2026-09-25 08:24 UTC

R44 crossed the five-epoch release end at block 8,081,674 and recorded its
signed terminal observation at block 8,081,824. The original owner remains
active without a sealed result. The qualified separate diagnostic has passed
the signed start, latest checkpoint, all 44 retained lineage generations,
observation prefix and complete-epoch terminal checks. Terminal scenario
assertions fail; absent original result and completion remain unavailable.
This is a read-only diagnostic, not a substituted owner seal.

The independent supplement completed successfully with explicit absent-result
findings. Its immutable
`/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/first-terminal-supplement/manifest.json`
has SHA-256 `7a5f9209d9a4e42cc0d28c05bc90f7866f4459b57f40c54a232b24f27e0ebce4`.
It retained 1,073 files including all 1,000 claim queues, the authenticated
observation prefix, the complete process-log report and 144,427,524 accepted
log bytes. All queues still show last discovery epoch 617; epochs 618–620
have no entries. Fifteen acceptance-scoped blocking process rows remain.
These are independent findings beyond the named lifecycle companion-filter
exception. The separate diagnostic has now completed with immutable
`/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/first-terminal-206d8958/report.json`
(SHA-256 `c207225f44bb5962f231345770b9a8aa71c293917c596c7bda26c6efe8e7e384`).
Of 37 checks, 14 pass, six fail, one is a finding, one is the named exception
and 15 are unavailable without the owner result or other source evidence.
The six failed groups and exact evidence are listed in `sim-testnet/FINAL-2.md`;
two capture-reader mismatches are under separate root-cause review for R45.
Keep the live owner running to its original
result or bounded watchdog, then compare exact owner artifacts and rerun the
post-owner diagnostic without rewriting this first terminal copy.
`urnetwork-sim-r44-post-owner-diagnostic.service` is armed for the original
owner becoming inactive with `result.json` present. It verifies the same
qualified image SHA-256, runs the reviewed read-only post-owner command into a
new external directory, and cannot start while the owner remains active. Its
existence does not imply an owner result; verify its exit and report after the
watchdog.

## R45 clean successor built; race gate pending — 2026-09-25 07:57 UTC

The retained policy generation and source-role readers now resolve their
original approval through its immutable archived ancestor when round-7 fleet
renewal appends a descendant plan. SN code/tests `bbd33365` and mainnet lesson
`c2171654` are on main; two separate old-reader overlays reproduced the exact
round-7 rejection. Focused normal and affected adjacent normal/race tests pass.
The focused three-test signed-renewal race run remains active; do not adopt the
image for live renewal until it exits successfully.

The new clean image is
`/mnt/data/sn-testnet/qualification/r45-compose-ready-20260925/build/sim-testnet-r45-2662f5f0-connectc98eb715`,
SHA-256 `a79072fa7a03db3452cec4c5bef942fb2424e05b5dbd79ed6cfbfa219006e2ef`.
Its [external binary manifest](/mnt/data/sn-testnet/qualification/r45-compose-ready-20260925/build/binary-manifest-2662f5f0.json)
records SN `2662f5f0`, Connect `c98eb715`, a clean VCS stamp and the pinned
module inputs. An external-copy doctor passed 66 checks in 67.225 seconds,
`ready=true`, no hard failures, no new live provenance and unchanged live
plan/journal prefix. The same three provisional soft findings remain: shared
physical RPC, owned-node-only verification and deferred source qualification.
This is read-only successor preflight, not a round-7 plan, transaction or
signed acceptance result. R44 remains the active owner.

## R44 independent terminal capture armed — 2026-09-25 07:42 UTC

The owner remains active. The named `R44-LC-1` bypass exception is recorded in
`sim-testnet/FINAL-2.md`; it does not convert the strict lifecycle assertion
into a pass or waive any other finding. The read-only external review is
`/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/REVIEW.md`
(SHA-256 `96527e79611ef920d0711781acbe69233dd98c1879d96dd39f1ad5d94a4a75bf`).
Its qualified diagnostic image has SHA-256
`2953bc1e135e5704e2b5f321ef4eed6336bf4a07c917276103390eb840f26ade`.

`urnetwork-sim-r44-automatic-terminal-capture.service` is armed for the exact
signed checkpoint at or after block 8,081,824. It performs the reviewed
read-only diagnostic without the original-result wait, then starts a bounded
external supplement after the diagnostic has begun. The supplement requires
four authenticated terminal checks before copying the signed observation
prefix, all 1,000 claim queues, the complete process-log report and accepted
log ranges. These are unsigned, point-in-time review artifacts, not an owner
seal. The pre-existing watcher and live owner are unchanged. Verify the
service's actual result and artifact hashes after terminal; an armed service
is not evidence of a completed capture.

## R45 renewal plan-history blocker — 2026-09-25 07:13 UTC

Static successor review found that the retained activated policy-rollover and
source-role readers compare their original `SourcePlanHash` with the current
plan hash. Round-7 fleet renewal necessarily appends a descendant plan, so
the clean `18b13aea` image's 66-check doctor does not prove it can restart
after renewal. Do not use that image for a live round-7 apply or retained
resume. Astra is fixing read-only historical source selection from the exact
approved plan archive, with a deterministic descendant-plan test; new rollover
or overlay mutations must still require current-plan approval. Rebuild and
requalify a composed successor after this fix before the post-R44 handoff.
R44 remains active and is not changed by this finding.

## R45 composed successor qualified externally — 2026-09-25 07:01 UTC

The clean successor binary is
`/mnt/data/sn-testnet/qualification/r45-compose-ready-20260925/build/sim-testnet-r45-18b13aea-connectc98eb715`,
SHA-256 `cafccd167b70c6d9fcb79a08f541d0fe3dcbbb298cdc1d060485db95bf5b8e5a`.
It reports SN `18b13aea`, `vcs.modified=false`, `trimpath=true`; its ten source
repositories and pinned module inputs are recorded in the external
`build/binary-manifest.json`. A fresh external-copy doctor passed all 66 checks
in 73.162 seconds against the LAN RPC, `ready=true`, with no hard failures,
unchanged live plan/journal prefix, and no new live provenance. Its three soft
findings remain explicitly provisional: shared physical RPC, owned-node-only
verification, and deferred current-source qualification. This is successor
preflight, not a live R45 renewal or signed acceptance result.

The bypass-aware lifecycle cleanup patch is SN `101ed51f`; its mainnet
hardening note is `18b13aea`. Focused and affected adjacent normal/race tests
and four old-behavior causal checks passed in the isolated source. The combined
focused and affected adjacent selectors also passed on composed main normally
(39.378 seconds) and under race detection (202.982 seconds), with all eight
lifecycle files matching the qualified source fence. R44 is still the active owner;
do not use this binary to alter its live state before its sealed result.

## R44 lifecycle predicate finding — 2026-09-25 06:52 UTC

R44 observation 77 reached finalized block 8,081,388. The owner is still
active and has no sealed result. Its old predicate restored
`fleet-lifecycle-target-prune` at that block with
`RestoreConditionMet=true`; the same observation's lifecycle evidence has
`stage=release-handoff`, `ProvisionalBypass=true`, and no terminal-effective
epoch. This condition was satisfied by the bypass stage, not by proof of a
provider payout. The companion filter remains active, so 41 of 42 faults are
restored. Preserve the false condition flag as an R44 evidence finding; do not
promote it into lifecycle conformance. The R45 successor patch makes
installed, paid and effective mutation predicates false under this bypass and
tests the old behavior causally. Do not stop the live owner.

## R44 continuation and R45 claim repair — 2026-09-25 06:17 UTC

R44 remains live under its original release owner. Observation 65 reached
finalized block 8,081,204 in epoch 619; 40 faults were restored and the two
signed post-acceptance lifecycle view filters remained active. There was no
sealed owner result or invalidation. Do not stop the owner on the basis of the
separate terminal watcher or the expected lifecycle-tail failure; retain its
complete diagnostic result. The successor-only bypass-aware cleanup is under
qualification and has not changed the live process.

The shared miner claim admission repair is now SN main `2aacdf22`. It admits
recent and historical claim work fairly, seeds the shared nonce floor from
authenticated signed queues before workers start, and reconciles signed
receipts before trusting a local API status. Focused miner and on-chain tests
passed normally and under race detection on composed main; three old-behavior
causal overlays failed as intended. The four broader fleet runtime manifest
fixture failures reproduce on the unchanged base and are being repaired
separately. This code is not in R44's pinned executable. Current acceptance
must continue to allow `pending` and `retry` through claim TTL while keeping
outstanding liability conserved; `submitting`, unreconciled `uncertain` and
`failed` outcomes remain blocking at the acceptance cut.

## R44 evidence tail and successor gates — 2026-09-25 05:58 UTC

R44's release owner and read-only terminal watcher remain active. Observation
59 reached block 8,081,111 in epoch 619; 39 faults were restored and three
active, with none pending. Two active lifecycle validator-view filters are
explicit `post_acceptance_evidence_tail` faults. The signed five-epoch terminal
block remains 8,081,824, but the scenario owner also waits for the tail faults
to restore. The inherited approved lifecycle bypass has stage `release-handoff`
and `TerminalEffectiveEpoch=0`; the companion filter's early condition requires
a nonzero terminal-effective epoch. It cannot meet that condition in R44 and
has a hard restore bound at 8,082,634. The strict
`fleet_lifecycle_fault_tail_bounded` assertion will therefore fail even if the
body reaches its terminal block. The live image's timeout is 33,360 seconds
from its post-boundary loop start, placing its wall deadline between
11:46:04 and 11:47:38 UTC on 2026-09-25, plus any in-flight read and cleanup.
Do not treat the separate watcher report at 8,081,824 as a sealed owner result
or stop a still progressing owner. The live image has no authenticated
in-place restore/exception command; an external restore would diverge from
the owner's in-memory signed fault record. A successor-only bypass-aware
schedule is being prepared. No live fault or service has been changed.
The separate watcher waits only 16 minutes for the original result after
block 8,081,824, so its first output is expected to be an early external
inventory. Preserve it, then rerun the same qualified read-only diagnostics
after the owner's signed result appears, into a new external output directory.
The companion filter's hard restore at block 8,082,634 does not satisfy its
`RestoreConditionMet` assertion. The old owner has no early-seal command and
continues to wait on that failed assertion. Label terminal diagnostics at
block 8,081,824 as external evidence with the owner result pending; retain
the live owner until it seals its result or reaches its bounded watchdog.

The tested signed-window claim checks are now SN main `5615a382`; historical
claim anomalies are also scoped to the new window in `ac2beccd`, with actual
in-window uncertain and failed claims still open. The R44 live queue census
at finalized block 8,081,014 showed 256 miners at discovery epoch 617 and 744
at 614; a later read-only census found all 1,000 had reached 617, but epochs
615–617 still had a substantial submission backlog. A fair shared admission
and durable nonce-floor successor patch is in development. These fixes are not
in the R44 executable. Round-7 renewal, authenticated traffic warmup and a
fresh signed release boundary remain required after the actual R44 result.

## Current continuation — 2026-09-25 05:26 UTC

R44 remains owned by `urnetwork-sim-release-r44.service` (PID 2823030), with
the fleet and the separate read-only terminal diagnostic watcher active. The
signed attempt has no result or invalidation. Observation 50 reached finalized
block 8,080,944 in epoch 618; the LAN node had finalized block 8,080,972.
Thirty-three faults were restored, three active and six pending. The signed
five-epoch window still requires terminal block 8,081,824. Preserve the live
run through that block and collect its exact result and terminal inventory.
Known process-log findings remain final-blocking but are being retained as
provisional observations, not used to interrupt the interval.

Epoch 617 finalized with zero funded settlement for both operators. The
[accounting evidence](/mnt/data/sn-testnet/qualification/r44-native-weight-lineage-20260925/REVIEW.md)
shows the later operator-2 stake arrived after its epoch-617 capture and
remained in that operator's vault pool; conservation holds. A cross-epoch
regression was pushed as commit `7e1782a6` after 48 Forge tests and Go model
normal/race passed, with an emission-carry mutation failing the new test.

The [R45 renewal proposal](/mnt/data/sn-testnet/qualification/r45-connect-successor-20260925/migration-review/RENEWAL-PROPOSAL.md)
is ready for a fresh post-terminal plan. Its clean composed image is SN
`d9ab57c9` plus Connect `c98eb715`, SHA-256
`53b26bdc9e1778f105872dfb103dd8e5350c623ae9668719f4b40411ad295b4a`;
an external-copy doctor passed all 66 checks. Round 7 must renew 808 expired
candidate bindings before another acceptance boundary; its projected totals
remain below the approved 512 TAO and 512 EVM limits. No renewal transaction
or live R45 write has occurred. A separate claim-queue investigation found
that a shared submission mutex can let hundreds of older claims delay current
epoch discovery. The bounded admission and acceptance-window fixes are in
progress; they must be qualified and included in a new image before R45.

## R44 live release interval — 2026-09-25 02:59 UTC

R43 ended before its terminal block when a post-restart
`restart-stale-contract` process-log finding was not eligible for provisional
observation continuation. Its signed generation-43 attempt and failed result
remain immutable in `sim-testnet/runs/ur-subnet-testnet-v1-attempt-4`. The
minimal correction, commit `6d9c8d38`, passed normal, race and old-source
causal tests; it continues the diagnostic interval while retaining the
finding as unexplained and final-blocking. The composed successor is clean SN
`8edc91ea` with Connect `3b7eca94` and server `4b2c4587`, executable SHA-256
`0c1bd59d469ef9f985f2efd00595b34e3986e8dbb551abf6b7175997b368ad59`.
Its pre-stop evidence copy and qualification are under
`/mnt/data/sn-testnet/qualification/r44-retained-relaunch-20260925/`.

The controlled fleet stop and retained resume succeeded with zero setup
actions. The fleet supervisor is `urnetwork-sim-ur-subnet-testnet-v1.service`;
the live release owner is `urnetwork-sim-release-r44.service`. The owner uses
the approved plan
`0xcf285ebcc8d72cdf8edfd01e5e950157c58108ae027d7a9dbf7bfebb965a55eb`
and owned LAN RPC `192.168.1.162:9944`. Its signed attempt is
`sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/campaign-attempts/release-1.0.recovery.44.evidence.json`;
the run is
`sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/runs/20260925T021136.926474885Z-release-1.0/`.
The signed boundary began at 02:30:04 UTC, covers five complete epochs
616–620 from block 8,080,174 through 8,081,674, and requires terminal block
8,081,824. This is a provisional run: `final_acceptance=false`; prior fleet
binding and rate-readiness findings remain visible for later strict review.

At this snapshot, R44 has no signed invalidation or result and no blocking
acceptance-scoped process-log finding. The quality-cohort control and first
PostgreSQL restart restored; the Redis-1 restart and four longer controls were
active, with 35 faults pending. Keep the live owner and fleet running. Read
their actual current service, signed attempt, observations, fault ledger and
process-log scope before any intervention. A stale snapshot or transient RPC
timeout is not evidence of a stopped run. The separate typed process-log
catalog, fault chronology and journal replay fixes are isolated future-build
work; they are not in the R44 executable.

## R43 recovery startup — 2026-09-24 21:02 UTC

Two additional unsigned startup retries were attempted after the first local
resource failure. The first, `urnetwork-sim-release-r43-retry1.service`, exited
at 21:13:30 because its review binary was built from an uncommitted worktree
and failed Git executable attestation; it performed no topology action. The
second, `urnetwork-sim-release-r43-retry2.service`, used committed revision
`ce5f45a8` and passed the corrected operator resource check. It exited at
21:17:34 before topology because the retained runtime manifest reader inferred
the original validator-2 generation's `hotkey.seed` location from the newly
selected source-role overlay config path. The sealed 3,138-file manifest still
correctly names the original generation inputs. The reader must authenticate
the signed overlay separately while deriving historical inventory paths from
the original handoff. No recovery-43 attempt or new acceptance boundary exists;
the fleet remains stopped and the active fault ledger is unchanged.

`urnetwork-sim-release-r43.service` started with PID 2374365, using the
qualified runner SHA-256
`6fdf0dcc1728362476edc83f6f27fa6cbba85e7f238c82d261bfe32f2bbb5e58`
and the owned LAN RPC `192.168.1.162:9944`. The controlled fleet stop
completed with `on_chain_state_preserved=true`. The reviewed source-role plan
`0x007e8168129004e81afcfcad799d993eb86692f570470f81f4a53bb131630dc0`
was applied and produced an owner-signed selection receipt at
`policy-rollover/source-role/generation-00000000000000000001/handoff.evidence.json`
(SHA-256 `0x50cd5b10cbe20b61994553c6b2e32da6cdfd617d1d1d778961c759cf46359053`).
The original R42 failed result, original rollover handoff and active-fault
ledger remain intact. This R43 startup exited at 21:04:10 UTC before a new
signed acceptance boundary or transaction. Its local audit covered all 11,181
actions, authenticated 9,859 receipts and found zero failures, then the
`release-host` preflight failed: `operator config overlay: required versioned
config resource all/mmdb/*/geolite2.mmdb is unavailable`. The pinned server
actually reads `mmdb/ip-ipinfo.mmdb`; the live config repository has that
resource and `ip.mmdb`, but no `geolite2.mmdb` or `places.yml`. The overlay's
resource list was stale. The next startup must correct that list, recover
`release-rolling-15`, relaunch the fleet with the selected validator config,
and sign a future-boundary attempt before the interval can be called running.
Focused normal/race/causal tests for native role, heartbeat continuation,
retained preparation, and process-restart recovery passed. The integrated
native-role, policy-history, handoff and continuity selectors passed normally
and under race detection (validator 29.653s/135.541s; simulator
70.846s/338.366s) with the pinned `server684.mod`.

## R42 failed early; recover in next signed interval — 2026-09-24

Recovery generation 42 ran as `urnetwork-sim-release-608.service`
against the owned LAN RPC `192.168.1.162:9944`. Its signed campaign attempt is
`sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/campaign-attempts/release-1.0.recovery.42.evidence.json`,
and its run ID is `20260924T181314.486418853Z-release-1.0`. The retained
acceptance boundary starts with epoch 608 at block 8,077,774, covers five full
epochs through block 8,079,274, and has terminal block 8,079,424. The LAN node
finalized the start block on 2026-09-24 at about 18:46 UTC. The release owner
exited with status 1 at 20:42 UTC, before terminal; the fleet remained active
until the later controlled R43 restart.
Its signed result is `fail`, `final_acceptance=false`, with invalidation
`execution-exited-before-completion`. The result SHA-256 is
`7288e099489a1525b4f2f80ff1f77e76766031c007c0c7f364e1884e0f7961f4`;
the signed attempt SHA-256 is
`c3a9f6b0511eb0f7fe9dbd5de38af19ed5a24449acc870984ad68e4c554ab305`.
The last end head was block 8,078,257 in epoch 609. R42 cannot qualify for
production handoff or final acceptance. Preserve it and start a future-boundary
R43 after fault recovery and the early-exit repairs qualify.

`urnetwork-sim-r42-terminal-diagnostics.service` is a separate read-only
watcher. It authenticated the exact signed R42 start, waits on the LAN node for
block 8,079,424, then collects independent terminal checks into
`/mnt/data/sn-testnet/qualification/policy-rollover-20260924/terminal-diagnostics/report-20260924T192429.130612934Z-3a3ae49fdfb2`.
It neither replaces the live runner nor rewrites its signed evidence. The
watcher's executable SHA-256 is
`3a3ae49fdfb2c073d22bdc4e57198d6e24c2d10011da91aca550025cdc34cfff`
from main commit `e9002acd`, built with the pinned `server684.mod` source. Its
`progress.json` is expected to remain `running` until the terminal inventory;
inspect `report.json` only after that inventory completes.
The separate `urnetwork-sim-r42-signed-result-diagnostics.service` waits for this exact
run's `result.json` and then invokes the same pinned, read-only diagnostic
binary again into a new report directory. Its script is
`/mnt/data/sn-testnet/qualification/policy-rollover-20260924/terminal-diagnostics/r42-signed-result-followup.sh`.
The result file appeared before owner exit, so this watcher is not a post-exit
assertion. `urnetwork-sim-r42-owner-exit-diagnostics.service` waits for owner
exit before its read-only terminal diagnostic. None writes live campaign state.
R42 left `release-rolling-15` active: its process-restart recovery awaited a
different healthy `miner-swarm-8` PID. The original child stuck after graceful
shutdown; it was killed after verifying its PID, parent, command, and retained
fault record. The fault driver must still restore the ledger entry. R42's last
heartbeat saw nine blocking process-log classes and 914 open anomalies; the
provisional deferral omitted validator steering continuity and ended the run
early. Final acceptance must retain these findings while a provisional live
heartbeat allows the full interval to finish. The current coordinator's finalized `policyCount()` is
three at block 8,078,129; the production scheduler's older two-policy gate is
repaired in main commit `402e6b1b`. Its normal/race focused tests passed in an
isolated worktree. The combined production policy-history, provisional handoff,
release gate and postcondition selectors passed on current main normally and
with race detection (81.948s / 420.484s, pinned `server684.mod`). The live R42
executable has not changed.

This run is explicitly provisional. An inherited lifecycle handoff records
that Subtensor would have pruned UID 1 instead of the planned UID 7, so the
churn registration/pruning exercise was bypassed with zero mutations. Preserve
that signed exception in R43 and complete its interval and terminal inventory to
expose all other failures; do not claim the skipped exercise passed or that
strict final acceptance is true. Early active-generation observations also
reported missing legacy validator handoff and excess path-proof rows because
the original collector read a retired generation. A separate, tested
generation-aware read-only probe verified both validators' current proofs and
removed those collector errors. It still found no recorded local native
intents, which is a separate diagnostic finding. One operator verification GET
timed out in an observation and succeeded on independent retry; preserve both
facts. The terminal diagnostic command is described in
`sim-testnet/TERMINAL_DIAGNOSTICS.md`. The next required steps are R42 terminal
diagnostics, fault-ledger restoration, qualified early-exit fixes, R43's full
interval and terminal inventory, the production soak, on-chain reconciliation,
and `sim-testnet/FINAL-2.md`.

Updated 2026-09-18. The user has requested full finalization and fixes for
previously ignored failures, flakiness and issues exposed by the shortened run.
The full functional requirements in [FINALIZE.md](FINALIZE.md) govern completion.
The user's 2026-09-15 instruction requires recovery by patching and retaining
incremental progress. The [harness recovery policy](sim-testnet/README.md#incremental-recovery-and-acceptance)
supersedes historical full-restart and blanket three-confirmation requirements.
The user explicitly confirmed SN testnet finalization under `sn/FINALIZE.md`;
all qualification and the current report at `sn/sim-testnet/FINAL-2.md` concern
this simulator and its runtime dependencies. Other simulation references were
mistaken and do not add work to this goal.
The earlier shortened-run instructions below are retained as historical scope
for those attempts, whose `final_acceptance=false` results remain unchanged.

R40 terminal incident, 2026-09-24 09:27 UTC: the signed release interval began
at block 8,074,774 and observed epoch 598, then ended with
`final_acceptance=false` before its five-epoch terminal block. The exact result
is `sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/runs/20260924T082911.840323196Z-release-1.0/result.json`.
The fleet remains active. Both epoch-597 root commits succeeded before their
deadline after an RPC timeout and a successful retry. The release-blocking
relay action `evidence.relay.40daf579caf54edd403c73af4f925a553893787b1d43a1cd0881eea09ffc2840`
failed gas estimation with contract `InvalidEvidence()` (`0xc9779e3c`). Its
epoch-594 header is signed under policy hash `0x1526b242cf4908cc31f7e58006664bce6064003c69fd8452eab2d49122fef277`,
but finalized `policyAt(594)` is
`0x41f0c7efe7e1b23b2fd22dac9352ca18be48d2e4d1fb5b41ce89660bc899b0dd`.
The write-once slot is empty. Retrying the same transaction cannot succeed.
The original activation and signed headers cannot be rewritten or backdated.
Both validator state directories retain closed manifests through epoch 600, and
their configured activation files still name the original policy-era
activation. The mismatch is therefore a continuing source problem, not one
bad epoch-594 transaction. An isolated repair branch at
`/mnt/data/sn-testnet/qualification/policy-era-evidence-20260924/source/sn`
adds a finalized `policyAt(epoch)` preflight and a deterministic transaction
test; it is only a diagnostic guard until new future activations are published
and both validators select them for future evidence.
The new activation must attest the actual ledger prefix at handoff
(`firstSequence` and `priorRoot`), so merely publishing a far-future activation
against today's prefix is unsafe while the validator continues appending.
Implement an append-only rollover checkpoint that snapshots the prefix,
publishes and finalizes all four consents, then switches both validators at its
future epoch. The relay must retain a separate explicit record for each
unpublishable old-policy header and resume at valid future headers; strict
acceptance must exclude those gaps from counted coverage.
The first producer primitive now verifies a complete signed V2 terminal and
derives each next-policy activation from its exact terminal sequence/root.
It does not publish the activation or switch a running validator.
At 2026-09-24 13:16 UTC the LAN RPC finalized block 8,076,129 in epoch 602;
both validators had sealed epoch 601, while their live ledgers had already
appended epoch-602 records. Epoch 602 ends at block 8,076,274 (300-block
cadence). A candidate derived from the epoch-601 root is therefore stale for
a handoff after those appends. For any rollover, fence the writers after a
newly finalized terminal, verify all four latest prefixes, and publish that
terminal's adjacent-epoch activations before the adjacent epoch ends. Missing
that window means waiting for the next terminal, not backdating signatures.
An independent fresh-VPK source generation is now the preferred recovery for
the next full interval. It keeps the four old signed namespaces immutable and
records explicit lineage, but begins new sequence-1 ledgers and makes no
continuity or carried-EMA claim. This path avoids replaying old-policy compact
attempts under the new policy; it still requires four finalized activations,
new validator client identities/JWTs, API upload-context refresh, relay source
routing, and a generation-aware final collector before acceptance.
Do not start another acceptance interval against this relay source until the
policy-era activation/relay transition is fixed and qualified. Preserve the
failed action, R40 result, signed boundary invalidation, completed receipts,
approved plan, and healthy fleet. The mainnet prevention requirement is PH-27
in `mainnet/PRELAUNCH-FIXES.md`.
The R40 process-log gate also reported two taskworker `error` classes at
epoch 597. Their exact lines were early `commit DEADLINE ALERT` warnings with
7m48s left; both roots finalized 37 seconds later. The classifier fix retains
these as pending-commit warnings when at least one minute remains and keeps
imminent or passed deadlines blocking. Finalized root evidence remains the
authority for whether the commit actually succeeded.

Reports are numbered at the user's request: `sim-testnet/FINAL.md` remains
report 1, `sim-testnet/FINAL-2.md` covers this full finalization, and later
finalizations use `FINAL-3.md`, `FINAL-4.md`, and so on. Preserve each earlier
report and its underlying evidence. The [report 2 closure table](sim-testnet/FINAL-2.md)
tracks the first report's peer-review findings. A compatibility exception,
historical replay, pending check or artifact-only assertion is not proof that
the next run met an on-chain target.

The user explicitly directed execution against the real chain limits after
the peer review: retain the root-controlled
`max_allowed_validators=64`. Lowering it to 56 is not a prerequisite for this
testnet run. The existing compatibility policy already requires exactly 64;
prove the 200-head topology against actual UID occupancy, permits and native
selection under that value. Report the difference from the whitepaper's ≤56
target explicitly, without treating a permit limit as a fixed UID partition.

Current work:

1. The retained campaign, signer-authority and gate corrections are composed
   and published; their focused qualification and required failure confirmations
   are complete. Preserve the existing deployment, wallets, approvals and journals.
2. Use Sol (`gpt-6-sol`, reasoning effort `medium`) for all tests and gate
   execution. Use Astra (`gpt-6-astra`, reasoning effort `max`) to diagnose and
   fix failures and flakiness, then return corrected source to Sol for reruns.
3. Complete producer and aggregate coverage using valid retained phase results
   plus failed, missing or patch-affected checks. Collect independent failures
   in a batch; preserve completed phases when a gate is interrupted.
   Retain earlier confirmations and every failed or interrupted result. Record
   accepted composition separately from each gate invocation's actual exit.
4. Complete the required real release campaign and production soak, then
   reconcile on-chain outcomes, LAN-node replay, the final report and shutdown.
   Reuse valid completed evidence; unrun, failed and waived checks are not passes.

R30 live campaign, 2026-09-18: generation 5 ran from
`/mnt/data/sn-testnet/qualification/r30-inherited-handoff-20260918T0210Z` as
PID 35174 against the owned LAN RPC at `192.168.1.162:9944`. It created the
append-only recovery record
`campaign-attempts/release-1.0.recovery.5.evidence.json` and then signed the
new acceptance interval at blocks 8,029,774 through 8,031,274, with terminal
settlement at block 8,031,424. Its run directory is
`runs/20260918T022128.652575174Z-release-1.0`. As of the initial live fault
cycle, `quality-cohort`, `release-dependency-postgres-1`, and
`release-dependency-redis-1` restored without a fault error; the remaining
scheduled faults continue independently. This is a live provisional campaign,
not final acceptance. Do not replace its executable or replay setup while it
is live. It ended provisionally at `2026-09-18T03:10:08Z` with
`final_acceptance=false`; setup, deployment, receipts and the recovery-5
record remain reusable.

The terminal process-log gate exposed a concrete attribution gap. The active
`release-dependency-redis-2` fault enumerated logical `miner-*` consumers but
the transport diagnostics originate from `miner-swarm-*` supervisor processes,
so the 03:07 UTC delayed `exit gap timeout` records for swarms 19 and 20 were
incorrectly unscoped. The next approved plan must retain both logical miners
and their owning swarm process identities in each dependency-fault scope. A
deterministic regression test now covers that mapping. The 03:10 UTC
seed-provider records occurred after the result's completion instant while
fault cleanup was stopping workers; they remain evidence and the recovery
finalizer must exclude them from the acceptance-interval verdict.

A staged recovery-only optimization is qualified separately at
`/mnt/data/sn-testnet/qualification/r30-inherited-handoff-20260918T0210Z/ADMISSION-INDEX-QUALIFICATION.md`.
It builds one authenticated evidence-relay admission index for the retained
journal, avoiding a full journal scan for each relay request. The focused
normal and race suites passed; it is not in the active R30 executable and may
be composed only into a later append-only recovery if R30 terminates.

Retain the approved 6,000-alpha repair allowance, 37,250-alpha lifetime limit,
205 EVM within 225 total TAO, 262 registrations and zero new subnets. Use
`192.168.1.162:9944` without RPC rate limits. Full acceptance remains pending.

R25 launch checkpoint, 2026-09-17 19:53 UTC: the append-only pre-boundary
recovery and provisional startup-gate correction passed 22 focused recovery,
26 recovery/topology-adjacent and 11 succession roots normally and under race
detection, with no data races. Two builds were byte-identical at SHA-256
`e50b4dc96cdaeebf02acffc8ea7fba70716e2bc9ef47b47ae2cfee61ee6821ea`;
the sealed qualification is
`/mnt/data/sn-testnet/qualification/runtime464-r25-preboundary-recovery-20260917T1924Z`
and its `SHA256SUMS` hash is
`6e72b9454c6d285368e8b5b498c8d498c7b868d084029d50703a72bf3398172f`.
The attached controller started at 19:53:25 from the exact R24 terminal seal,
generation-1 recovery, unchanged plan and journal. It is authorized to create
only the append-only generation-2 recovery and continue the provisional run;
it does not replay setup and remains `final_acceptance=false`.
At 19:54:48 the controller created
`release-1.0.recovery.2.evidence.json` with SHA-256
`3d5be805097f3cffd84693c3a5c2fa70bcc49eef08b91dfba393068505c3a00c`.
It binds the exact generation-1 attempt and R24 failed result, complete
770,326-byte observation log, no-boundary process log, 18,726,946-byte journal
prefix and unchanged approved plan. The signed record has no invented prior
campaign-start hash. Its checkpoint `RECOVERY-CHECKPOINT.SHA256SUMS` is
`910651cf61e3f405e21606a97e300b479e434118a2026bcaf6248f407899a069`;
no transaction or spend had begun at that checkpoint.

Before R25 started, validator 2 recovered once after a transient historical
RPC call exceeded its deadline. The same supervisor generation remained live,
all 33 identities were healthy, and direct LAN plus both retained RPC proxy
layers subsequently answered 12/12 finalized-head probes. R25 records that one
cumulative restart as its pre-boundary topology baseline instead of discarding
earlier work. Strict acceptance will measure its own interval from a signed
boundary. The launch capture is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r25/release-candidate-r25`.

R25 completed its retained receipt and relay preparation at 20:18 UTC, then
stopped before a signed acceptance boundary while reopening the persisted
fleet lifecycle. The reported error was `release fleet lifecycle provisional
bypass contains production state`; inspection shows the retained state is a
release handoff with no serialized production fields, while its authenticated
release run ID belongs to the earlier completed invocation. The validator had
grouped that ordinary successor run-ID mismatch with its production-state
check under one misleading error. Do not rewrite the earlier handoff or replay
setup. The correction must authenticate that completed handoff as durable
lineage for the new pre-boundary provisional successor, while leaving strict
final acceptance unchanged.

The plan and journal remained byte-identical at SHA-256
`d10956f9b9cb078d7924af4a528e57233b8147292cc45d79bb579029e4787fab`
and `a355a5dfc6c6202056b1435e3199181c92700f08426533b20da5e014e8c76bbe`.
R25 submitted no transaction, spent no funds and created no campaign-start
evidence. Its generation-2 recovery record was finalized with
`preparation_complete=true` at SHA-256
`36f99aebdb2f89b1e058d32a9f800a3e0aefefd25f1c2229b27405783a126bb5`.
The same supervisor generation and all 33 processes remained healthy, with the
single validator-2 restart retained as the baseline. The self-contained R25
terminal seal has `TERMINAL-SHA256SUMS` SHA-256
`6b3a44751acc905bf2192b08e89c9d879d1b0dd5b78f425b689ce160cb07128a`.
The next controller must authenticate this seal and create generation 3; it
must not restart from preparation.

R26 qualified the lifecycle-handoff, relay-startup-cache and validator RPC
resilience changes together. Fifteen simulator and five validator focused
roots passed normally and under race detection with no data race. Two builds
were byte-identical at SHA-256
`ea6b58400fd634b56d94f73865ac94190f2723464754c3cc059fbf1d3610b7c3`.
The corrected, independently verified qualification manifest is
`/mnt/data/sn-testnet/qualification/runtime465-r26-lifecycle-resilience-20260917T2030Z/r26-integration/SHA256SUMS`
with SHA-256
`6fbf288ff53fca64771019cf3da5aaf94cd38ffe1360c3b930658db538513358`.
The original self-including manifest is retained with a correction receipt;
tests and builds were not repeated to repair that evidence-only seal.

Before R26, both validators were updated independently to the qualified RPC
resilience binary
`8cd0f5cb1aebfde3f65300cc389b73605cb9ce7e916a607842ef3331b6647d7f`.
The first rollout checker observed validator 1's replacement PID one state
write before its incremented restart count and failed closed. It restored the
pinned supervisor path while retaining the healthy replacement. The
continuation then updated validator 2 only. All 33 processes are healthy; the
unchanged supervisor is PID 2,849,990 with start ticks 195,540,640, while the
validator restart baseline is now one and two. The failed and resumed captures
are `/mnt/data/sn-testnet/qualification/rolling-validator-hotpatch-r26-20260917T213930Z`
and `/mnt/data/sn-testnet/qualification/rolling-validator-hotpatch-r26-resume-20260917T2143Z`.

The R26 controller started at 21:45:48 UTC against the owned LAN RPC. It
created append-only generation 3 with run ID
`20260917T214645.133882754Z-release-1.0` and evidence SHA-256
`ac66c15b12241272fcc1fb9cdca42914bd6835c67737c3536563e29daa604518`.
The record authenticates generation 2 at
`36f99aebdb2f89b1e058d32a9f800a3e0aefefd25f1c2229b27405783a126bb5`,
carries `preparation_complete=true`, and does not replay setup. Its atomic
checkpoint manifest verifies at SHA-256
`904d5a922cb8b5710f790e6a153491a7e2da61b1dc38026a7bb93a0d140639b4`.
R26 passed the prior lifecycle-handoff blocker and completed the first
authenticated relay-startup cache at 22:33:29 UTC. The cache binds the exact
R26 executable, plan/context, 25,251-entry journal prefix, 706 relay actions,
all retained file witnesses and its wallet-derived MAC. At 22:47 UTC R26
committed the signed acceptance boundary. The accepted five-epoch window is
blocks 8,028,574 through 8,030,074 (epochs 444--448), with terminal settlement
at block 8,030,224. The baseline is finalized block 8,028,547, and the process
log boundary is
`0x690cdad99b9cb14e128427c8d95574cda754c6dc5d30f8d8b4e96d61aee99d9f`.
At 22:54 UTC the invocation failed before applying its first fault. The process
log gate classified two bounded TLS handshake timeouts and two validator exit-gap
timeouts, caused during the transient network interruption, as four unexplained
release blockers. Its post-boundary scan also rehashed the complete sealed prefix
of every process log on every poll, rereading approximately 1.43 GB and delaying
fault advancement by several minutes. The signed boundary is now explicitly
invalidated with reason `execution-exited-before-completion`; it is evidence of
the failed attempt and cannot be reused as an R27 acceptance interval. The
generation-3 preparation checkpoint remains valid and the unchanged supervisor
still has all 33 processes healthy at its retained restart baseline. R26 submitted
no transaction and spent no funds.

The self-contained R26 terminal capture is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r26/release-candidate-r26`.
Its 52-file checksum manifest verifies and hashes to
`7db851644c63e9e5a9ec29f25d01244632023eceef9f402a5e28405d9c0d9af0`;
the terminal record hashes to
`0b3f881c285d79a5bdf37a269554377a97c441d407e8112a9842636ca3feeddd`
and the failed result to
`6c43156a1e598735a1aa775cc0f4a3b74e5bdced37ce5c8d24ea537625034c37`.
R27 must create append-only generation 4, retain `preparation_complete=true`,
start a new future acceptance interval, and reuse the authenticated relay cache
through a narrowly versioned verifier identity. Its process-log gate must verify
the sealed prefix once, authenticate append-only suffixes incrementally, and
accept a transient timeout only after bounded recovery evidence proves the
affected process healthy. Tamper, truncation, inode replacement and unresolved
timeouts remain blocking.

The cache-identity correction has independent Terra-medium qualification under
`/mnt/data/sn-testnet/qualification/startup-cache-semantic-identity-r26-20260917T215412Z/independent-terra-medium-20260917T2230Z`.
Its 23 focused and 23 adjacent tests pass normally and under race detection;
the exact two legacy-migration tests also pass twice. `RESULT.json` hashes to
`ea0cbd86049c097995b7898df47fbdec5b17ad06aec9346b80c33e100b8dce2a`
and the verified checksum manifest to
`f5047b45b854d5295756c4115c27793e67ec077afdcb506604f192d4b3131c00`.
Migration remains restricted to the exact R26 executable and authenticated
legacy HMAC; every other identity misses and performs the ordinary cold audit.
The qualified cache overlay is composed into the isolated R27 workspace at
`/mnt/data/sn-testnet/qualification/r27-combined-20260917T2300Z/workspace/sn`.
The process-log authentication performance change and the recovered-timeout plus
lifecycle-handoff corrections are being produced as separate overlays so they
can be reviewed and tested independently before one combined build.

R27 launched against the owned LAN RPC at 23:53:15 UTC on 2026-09-17. The
running candidate is SHA-256
`fffd71bda98d6b33b82ac3891980423bb1b0572d053fbc6d57637faafbf25e9c`;
its final Terra-medium incremental qualification result hashes to
`0da088076ef41472a6d990006bc8ab8b61bc3eae8b4852d25c4e1a9b4b1e2968`
and its verified checksum manifest to
`af544787b4fbb6997d36af1e0371461529e775347ef6347617e729f182787bbe`.
The launch capture is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r27/release-candidate-r27`.
It created generation 4, authenticated the exact failed generation-3 lineage,
retained `preparation_complete=true`, and did not reuse the invalidated R26
acceptance interval. Its pre-boundary atomic checkpoint manifest hashes to
`3d41ba39d1a85ba66aa9ce53358cfc1a0272a0d201753b73093e6070f3883084`.
Setup and approved spending were not replayed. The live worker is making the
expected new evidence-relay transactions; finalized examples include
`0x57b040ce5c1958e02632dbd2a59b8c413dd6f67ea45dae45c9341de119a1dbed`
at block 8,028,928 and
`0x72cd6247a9bd9fe28ce487b88ddf32c7c81bb4d0c85ccacbf4920c2fbad682b2`
at block 8,028,934.

At 00:11:42 UTC on 2026-09-18 R27 signed a new acceptance boundary. Its
baseline was finalized block 8,028,971, the proposed five-epoch interval was
blocks 8,029,174 through 8,030,673 (epochs 446--450), and terminal settlement
was block 8,030,824. The process-log boundary was
`0xb98a61fe64b639d1ea4938b741c760d9621c35605791036e67de989d3ce6bcb6`.
The signed campaign-start evidence hashes to
`ed93961d7f25daa5224abec773a3d77187e1e75a9c2b2af39c594467b41078d6`.
At 00:18 UTC the incremental process-log gate had authenticated 2,424,832
post-boundary bytes across all 66 stream cursors with zero finding scoped to
that boundary and no negative cursor movement. Its durable report retained 33
earlier provisional findings as historical inventory and correctly excluded
them from the proposed interval.

R27 exited before acceptance began. At 00:25:55 UTC generation 4 invalidated
the signed boundary with reason `execution-exited-before-completion`, and the
controller sealed its failed terminal result at 00:26:06 UTC. The post-boundary
gate found one unresolved item: `miner-swarm-15/stderr/tls-handshake-timeout`.
The timeout line was timestamped 00:24:23.982536; the scanner first observed it
30.164 seconds later and immediately expired its 30-second pending window. The
same transfer recorded `contract set` and `contract wait 61ms ok=true` at
00:24:25.854099, but that path can run before a cipher is usable and remains
insufficient recovery proof. The next exact authenticated marker, `peer
identity proof verified — cipher is now usable`, was appended to the same
process stream at 00:25:59.243202, 95.261 seconds after the timeout. Other
validator and miner timeout findings in that scan were authenticated as
recovered. The correction keeps that exact cryptographic marker and the same
process, stream, byte-distance and acceptance-scope checks, allows one bounded
two-minute replacement-handshake interval, and gives a final scan the
remaining interval to re-scan before it rejects an unresolved timeout.

The self-contained R27 terminal capture is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r27/release-candidate-r27`.
Its terminal record reports `body_exit=1`, `capture_complete=true`,
`final_acceptance=false`, generation 4 and `setup_replayed=false`. The
generation retains `preparation_complete=true`; all 33 supervised processes
remain healthy with the same restart baseline. R28 must authenticate the R27
terminal seal, create append-only generation 5, choose a new future boundary
and leave the invalidated R27 boundary as failed evidence.

This launch also exposed a process-local startup-cache stampede: three
same-context relay sessions entered cold authentication before the first
durable proof was published. It delayed startup but did not alter the signed
boundary. A separate isolated correction adds singleflight, a per-context
durable lock, a cache re-read after waiting and deterministic
contention/cancellation tests. A follower must also refresh its bounded
journal and inventory snapshots after acquiring the lease so an authenticated
suffix written by the leader cannot turn a valid cache hit into another cold
audit. Both corrections must be independently qualified and composed into the
R28 candidate without mutating the retained topology or R27 evidence.

Runtime 465/1/1 activated during R25 and the controller first observed it at
finalized block 8,027,798
(`0x50046d184a5aaf883712082f5b80fa477cdfea28231cd6087d05847bca8706ba`).
The controller and validator 1 each
authenticated its exact consumed-interface profile independently: code hash
`0x5188d76f7ac3a6b78ab1f57d6a9784baa1df01426280978157f121abb694e0ed`
and metadata hash
`0xe7553ea82a25a6a14e901e39bb01b0f915020118ed15d82243c2c229d3eba3af`.
Those immutable observations are included in the R25 terminal seal and remain
explicitly provisional; the upgrade does not require replaying deployment.

Live checkpoint, 2026-09-17 12:39 UTC: the R18 epoch-boundary validation at
block 8,025,371 proved that waiting cannot recover the originally authorized
UID-7 lifecycle transition. A full read-only LAN census at finalized block
8,025,406 found 256/256 occupied UIDs, immunity 50,000, 255 nonimmune UIDs and
minimum nonimmune occupancy 10. UID 7 retained emission 1,806,659,935 rao,
while the runtime's internally consistent prune candidate remained UID 1 with
zero emission. The recovery registrations' immunity window has expired. R18
was therefore stopped gracefully without a lifecycle mutation, transaction or
spend; the 33-process systemd topology remained active with zero restarts.

At 12:09 UTC the chain moved to runtime 464/1/1. Its consumed-interface
compatibility probe passed under the existing provisional authority at finalized head
`0x3b0e44d527d50bb09d619c9cd52db1dbd4f02f8d7d08b1ff502c6bea2ca06232`,
with code hash
`0x637844a3ad94d3bdbea45664b67bbfa07a31f21c087834a56a772ba27f612b9f`
and metadata hash
`0x0b146ba30795422702623180d636d022967212833fdd25fd692d6df2156314e4`.
Strict runtime admission remains unchanged.

R19 adds a narrow provisional-resume lifecycle bypass for this exact state. It
requires a complete, internally consistent launch census proving that the
runtime would prune a different nonimmune UID. It records the census and an
authenticated release-to-production handoff, performs zero lifecycle
registration mutations, and marks `final_acceptance=false`; strict execution,
partial or changed census data, tampering and mutation claims still fail.
The full affected lifecycle suite passed normally (261.916 seconds) and under
race detection (397.088 seconds), followed by sealed focused normal and race
runs. Qualified binary SHA-256 is
`c30ebf08889e2353dd43b12a4095aab1b0827ef59f5e7165924a255c00e45e3e`;
sealed evidence is
`/mnt/data/sn-testnet/qualification/runtime464-r19-lifecycle-bypass-20260917T1232Z`.
Its controller began at 12:39:12 against the live retained topology and LAN
RPC. Continue through the actual release campaign and production soak from
this controller; the bypass itself is a validation checkpoint, not acceptance.
The checkpoint completed at 12:59:50. The complete 256-UID census at finalized
block 8,025,642 (`0xb3206e70bbefeb709a77d0510d3346c1244ec76301a5e051bb4572f9e4bb2164`)
authenticated target UID 7, runtime prune UID 1 and nonimmune occupancy 255/10.
The controller recorded the provisional bypass with zero lifecycle mutations
and continued into the real release campaign on the same topology. Its two
planned pre-acceptance validator-view fault filters are active. The immutable
checkpoint is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r19/release-candidate-r19/checkpoint-20260917T125950Z`;
its `SHA256SUMS` hash is
`51649b8f81ad7266e05ce67b2650ea1d8c3bdfe0fb6a97929eb0bfd2bd4f11fe`.
R19 then completed the post-preparation snapshot but exited before signing an
acceptance boundary at 13:06:17. The append-only run log still contains an
early failed-invocation observation with no contract view; the boundary reader
incorrectly required that retained record to be a current campaign
observation. No `campaign-start.evidence.json` exists and the signed successor
still has no `acceptance_boundary`, so no accepted epoch was started or lost.
Pre-acceptance fault recovery removed `active-faults.json`; systemd retained
the same healthy supervisor and zero restarts. The sealed failure bundle is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r19/release-candidate-r19/failure-20260917T130617Z`;
its `SHA256SUMS` hash is
`227cf8103099c7b7ea3cb9e72c885974659f986250fc5ce94ac714138e3e4717`.
Repair acceptance ownership by binding the exact current-invocation suffix
while retaining and hashing every earlier log byte; do not delete or rewrite
the failed observations.

R20 implements that ownership boundary. The signed acceptance record now
authenticates the complete retained prefix separately from the current
invocation suffix: 3,856,384 retained bytes have SHA-256
`94e8ad4bc276486146bc98fa700a36c800def35f833f3a7460980632def8795b`.
Incomplete or changed current-invocation records and unsigned tails still
fail. The same patch treats only the validator's sole stale settlement
snapshot during a fresh native-epoch rollover as retryable; joined or mixed
errors remain fatal. Eight test commands and all 11 qualification gates passed
normally and under race detection. The qualified controller SHA-256 is
`1dba12b2cf35583586cc4197d2b08ba0f8d5b41f4e9046ec7bcf914509720824`;
sealed qualification is
`/mnt/data/sn-testnet/qualification/runtime464-r20-observation-log-cut-20260917T132210Z`,
whose `SHA256SUMS` hash is
`6a47ddd5f08d9988ecf0eb44bca7bc8fe0bc3488f6d839f9edf4a27bdda2d2ee`.

Before acceptance, both validators were restarted individually under the R20
binary so the epoch-rollover repair is active while the other 31 managed
processes and supervisor remain in their existing generation. The original
supervisor binary was restored at its manifest path after the two running
executables were verified. The sealed hotpatch evidence is
`/mnt/data/sn-testnet/qualification/runtime464-r20-validator-hotpatch-20260917T133930Z`;
its `SHA256SUMS` hash is
`242cff6e63e6922a7cd744d1a04b4cfc6ddbfdcbe80d27b172a0bc752a1bde9a`.

The R20 controller started at 13:40:31 against `192.168.1.162:9944`, reused
4,674 authenticated receipts with zero failures and signed the real campaign
boundary at 14:06:08. The window covers epochs 436 through 440: start block
8,026,174, exclusive end block 8,027,674 and terminal block 8,027,824. There
is no acceptance invalidation. The immutable start checkpoint is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r20/release-candidate-r20/acceptance-start-checkpoint-20260917T142458Z`;
its `SHA256SUMS` hash is
`be090a41631d5daa1bd3b1f844ff0e3a9d8221eb0bad8b6db4ff147c88de3a2d`.
This invocation remains provisional and cannot establish strict release
acceptance; its deferred startup gates and `final_acceptance=false` status must
remain explicit. Continue the five-epoch workload and preserve its actual
fault, lifecycle, adversary, accounting and terminal evidence.

The provisional process-log evidence currently carries seven source findings
as blocking even though the runtime view relabels them observation-only. The
gate was generated at 11:24:20, while the signed acceptance boundary began at
14:06:08; every first/last source-line endpoint for those findings predates the
acceptance boundary and its stored line hash reproduces exactly. Keep the raw
findings and fix the acceptance scoping so historical bytes remain
authenticated without being represented as failures from the new accepted
window. Do not suppress a finding produced after the boundary. The immutable
read-only audit is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r20/release-candidate-r20/process-log-preboundary-audit-20260917T144121Z`;
its `SHA256SUMS` hash is
`3b3d53700594fa4dc6b8ffe2dd7d814938905dfad96a0b4aa34b5fc041e35fa4`.

The first real in-window fault transition was recorded at finalized block
8,026,186. The 96-miner quality cohort and both pre-armed lifecycle view
filters became active with exact process evidence and no ledger error. The
quality cohort was restored at block 8,026,220; the head-boundary filters and
first PostgreSQL outage then became active. The controller retained its signed
boundary and no acceptance-invalidation file exists. The first-transition
checkpoint is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r20/release-candidate-r20/first-fault-transition-20260917T150019Z`;
its `SHA256SUMS` hash is
`c2df924393988848999991e859c545e1ee5dc1075c099c9c74fe43a9aaad32ad`.

That transition also exposed a separate real validator failure. Validator 2
treated a joined authenticated-attempt stream timeout as a normal failed
native submission, then exited when native epoch 1511 advanced to 1512. The
supervisor restarted it at 14:55:07 from the restored R17 binary, increasing
its restart count from three to four. Preserve that failure: the single-error
R20 regression did not cover the live joined timeout tree. Its sealed evidence
is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r20/release-candidate-r20/validator-2-runtime-failure-20260917T1502Z`;
its `SHA256SUMS` hash is
`77c11089fd66cfcfcafc81b59dab553feb28f5bf97375a10b32790c3bb326833`.
Validator 2 was then restarted alone under the already qualified R20 binary;
PID 2432088 became healthy with executable hash
`1dba12b2cf35583586cc4197d2b08ba0f8d5b41f4e9046ec7bcf914509720824`,
while the supervisor path was restored to its original hash. That recovery did
not restart the controller or alter the fault ledger, but the two real
post-boundary validator restarts prevent a zero-restart strict claim. The
re-hotpatch evidence is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r20/release-candidate-r20/validator-2-rehotpatch-20260917T1502Z`;
its final `SHA256SUMS` hash is
`0743de90c2e94ea67e33e79fb5b30da93d65b7ef2764904bfc75e14572348172`.

The next completed R20 observation exposed delayed-snapshot fault overlap.
The snapshot was pinned at block 8,026,272 after the observer had crossed
several 25-block schedule boundaries. It restored PostgreSQL-1, then activated
Redis-1 and PostgreSQL-2 together at that same block even though those outage
windows are nominally sequential. Both records remain active with no ledger
error, the controller and validators remain live, and no acceptance-
invalidation file exists. This interval cannot prove isolated dependency
recovery. Preserve it as the causal reproduction: the fault state machine must
serialize non-overlapping scheduled faults and measure each ordinary outage's
minimum duration from its actual application block, rather than its missed
nominal deadline. The sealed overlap evidence is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r20/release-candidate-r20/sequential-fault-overlap-20260917T1517Z`;
its `SHA256SUMS` hash is
`c4de586b1a3f18e562307e1daaecf685de885739979dc062886b5be97f05e70f`.

That observation also produced four adjacent, post-boundary log classes whose
raw endpoints reproduce but whose fault attribution is incomplete: validator
1's contract-creation exits, exit-gap timeouts from miner swarms 17 and 20,
and validator 2's network-JWT refresh timeout. Each exact process ID is in the
impact set of an active scheduled dependency fault. Keep these signatures
blocking when no matching fault is active; with an exact active process impact,
classify only these narrow outage forms as expected-fault evidence. The sealed
line, hash, offset and active-impact audit is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r20/release-candidate-r20/process-log-live-fault-attribution-20260917T1521Z`;
its `SHA256SUMS` hash is
`de5d9e762a2f1f68f275482530e8ae0990c11b80c14c04b4c44c21c581ae690c`.

The following R20 observation confirmed that the slow, uncached path-proof
scan is operationally coupled to fault restoration. The RPC-path pause was
applied at block 8,026,324 with a five-block minimum, but it remained active
past block 8,026,392 while the controller computed its next snapshot.
Validator 1 then exited on a canonical-block RPC timeout and exhausted the
recovered five-restart burst while both simulator-owned RPC proxies were still
paused; its cumulative counter reached six and PID became zero. Validator 2
remained live. The same snapshot recorded eight exact `seed-unavailable`
findings from each validator as unexplained even though both IDs occur in the
active RPC-path impact set. R21 must retain those signatures as blocking
outside an exact active impact, attribute them narrowly inside one, cache the
authenticated append-only proof prefix, and serialize ordinary dependency
faults so observation delay cannot overlap or substantially extend their
windows. Do not present the R20 restart churn as scheduled restart evidence.
The sealed fault ledger, raw line endpoints and hashes, supervisor census,
validator fatal lines and LAN head are in
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r20/release-candidate-r20/rpc-path-validator-restart-20260917T153128Z`;
its `SHA256SUMS` hash is
`86992544c57ee1b7f75622b22159a4d3d032db654581fb1ca543cb20557dfa4e`.

R20's next completed observation restored Redis-2 and the RPC path together at
pinned block 8,026,372. Both proxy processes returned healthy, but validator 1
had already exhausted its restart burst and validator 2 incurred another
restart at recovery. In the same transition R20 applied `release-rolling-01`
and `release-rolling-02` together, although their configured windows are
8,026,334--8,026,354 and 8,026,359--8,026,379. This is a second direct
reproduction of delayed observations collapsing nominally separated fault
windows. The controller and signed boundary remain live and no persisted
acceptance invalidation exists, but these facts do not make the interval clean
acceptance evidence. The sealed before/after ledger and process census are in
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r20/release-candidate-r20/rpc-restore-rolling-overlap-20260917T153523Z`;
its `SHA256SUMS` hash is
`86c36c8eb8ff63ec527b53abb5c8d3a6e1e51b66c35c1a04f7aee66b9f97be6e`.

The first retained observation beyond the epoch-436 boundary is now sealed.
It was observed at 15:48:23 UTC and pins finalized block 8,026,509, 35 blocks
past the signed boundary at 8,026,474. The exact 28-record observation prefix,
fault ledger, process-log inventory, supervisor state and owned-LAN finalized
head all reproduce. At that cut the controller remained live with no persisted
acceptance invalidation, but validator 1 had exhausted its restart burst and
remained at PID zero; the fault ledger contained 11 restored, six active and
25 pending entries. This is real provisional epoch evidence, not clean or
strict acceptance. The sealed capture is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r20/release-candidate-r20/epoch436-boundary-observation-20260917T155600Z`;
its `SHA256SUMS` hash is
`9584d3301852e6263483ebf47dce1c0394bc5d1512fd99ea4746924f3ade1fb9`.

The first retained observation beyond the epoch-437 boundary is also sealed.
It was observed at 16:51:04 UTC and pins finalized block 8,026,798, 24 blocks
past boundary 8,026,774. The exact 37-record prefix and all captured state pass
their stored checksums. The controller still has no persisted acceptance
invalidation; validator 1 remains stopped at cumulative restart count six and
validator 2 remains live. The fault ledger contains 24 restored, five active
and 13 pending entries. This is a second real provisional epoch checkpoint,
not clean acceptance. The sealed capture is
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r20/release-candidate-r20/epoch437-boundary-observation-20260917T165732Z`;
its `SHA256SUMS` hash is
`0cd998c80fb20aae1abef2a270bf915b9fbcba428e74a8993bde1b481596df9b`.

R21 addresses the failures exposed before this cut with an authenticated
append-only path-proof cache, actual-duration serialized faults, exact
post-boundary process-log attribution, joined publication retry handling and
failed-predecessor recovery evidence. Its focused simulator and validator
tests pass normally and under race detection. Both broad validator modes and
the broad simulator normal mode reached Go's package-wide ten-minute timeout
under the loaded live host; each timeout occurred while the named current test
had run for only a few seconds and hundreds of parallel tests were still
queued. Preserve those actual failures and distinguish them with longer
fenced commands if the diagnostics are continued; do not claim a broad pass
from them. The unpartitioned simulator package is not an admitted release
gate: the harness retains its exact disjoint owners and 5/10/45/90-minute
budgets, while incremental promotion uses the passing affected R21 selectors.
The exact R21 worktree has been
copied to
`/mnt/data/sn-testnet/qualification/runtime464-r21-frozen-workspace-20260917T1601Z`;
its source-before, source-after and snapshot manifests are byte-identical, and
the manifest SHA-256 is
`a702e2b1b81a352ec606f8b591a719d5a1344f28b14e45ae211b02dde9046b0f`.
The simulator qualification is sealed at
`/mnt/data/sn-testnet/qualification/runtime464-r21-simulator-20260917T154318Z`:
its focused slice is qualified, its two whole-package timeouts remain
non-gating diagnostics, and its `RESULT.json` and `SHA256SUMS` hashes are
`57ddf49ab1370136f6248baece53003a05b73236374217c4c50e5769b5503791`
and
`eec6b15d948188427b2b1a606176dc6b844f0cf92259cec82e38e5c599063e7c`.
Retain that frozen copy for any subsequent R21 check while R22 repairs the
independent live validator binding-timeout and exhausted-child recovery paths
in the primary tree.

The R22 supervisor repair is qualified from an immutable frozen workspace. An
exhausted, stopped child now receives a fresh bounded restart burst only after
a ten-minute recovery window; generation, PID, prior-burst and configured-limit
guards prevent stale callbacks or an unbounded restart loop. Cumulative restart
accounting remains unchanged, cancellation is bounded, and a zero restart limit
remains terminal. All 11 exact supervisor roots pass normally and under race
detection with identical before/after source fences. The sealed qualification is
`/mnt/data/sn-testnet/qualification/runtime464-r22-supervisor-frozen-20260917T163010Z/qualification`;
its `RESULT.json` and `SHA256SUMS` hashes are
`2672c6ffbfc9a25764f7dddee94828afcc1d1c40d5e05dd9afb308f0ce8c0c9c`
and
`af988a2ead46110712ab93957a617dca9691ebbc4326ffd8c24f9ac9c0bb75b7`.
This repair is staged for the next required generation and does not alter the
running R20 supervisor or retroactively restart validator 1.

R22 also fixes the validator failure that exhausted that restart burst. Only a
typed failure originating in the finalized binding RPC read is eligible for a
retry, and every leaf of a wrapped or joined error must be a recognized
transient transport failure. The validator retains the authenticated assignment
and exact pinned boundary, waits two seconds, and repeats only the binding read;
it does not replay SEED/EXTEND traffic or mutate the attempt ledger before a
successful read. Cancellation, authorization, integrity, pin and binding
mismatches remain hard. The compile-only lane and the focused deterministic
normal and race suites all pass with byte-identical source fences. Sealed
qualification is
`/mnt/data/sn-testnet/qualification/runtime464-r22-binding-retry-20260917T164554Z`;
its `RESULT.json` and `SHA256SUMS` hashes are
`7e5535d564fb615e7c5fa6eb195c7c09adcd78f76f4436b759caea3bf7d24999`
and
`b7843734fa5a0bdea5f7a46a02b3ea2daf6cf2a113953e21243d1b61424a9f8c`.
The running R20 validator binary does not contain this repair; preserve its
failure and use the qualified slice in the incremental successor.

The combined R22 successor binary is built from the immutable R21 workspace
with exactly those five qualified supervisor and validator files overlaid. A
first frozen build attempt copied `sim-testnet/process.go` into `validator/` and
failed before linking with a mixed-package error; that tooling failure is
preserved and did not touch primary source or the live topology. The clean retry
asserted the exact five-file diff, reproduced all sealed source hashes, held a
byte-identical source manifest through the build and produced candidate SHA-256
`e989fd2908a5e7ce08ae81dca871d7fa725de20dfb03d1b64434c9c7a832e5f6`.
The build bundle is
`/mnt/data/sn-testnet/qualification/runtime464-r22-combined-frozen-20260917T165502Z-retry`;
its `RESULT.json` and `SHA256SUMS` hashes are
`dbcdfd7a7b7742f121765195efcfc4918cbefb9ed3054a2a2d94cc3164a8dd9f`
and
`68431fb6cf489ef9cc576a5e193f357b6321291d606955e641832477c34a0108`.
Do not replace the live R20 generation merely to adopt it; use this binary for
the next required incremental successor after preserving R20's terminal state.

The guarded R20-to-R22 rollover is prepared at
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r22/prepared-rollover-20260917T1705Z`.
It refuses to mutate the managed generation until the R20 controller and runner
have exited, the failed provisional result exactly matches its last signed
observation, the acceptance interval has a signed invalidation, all faults are
restored, the LAN genesis and head reproduce at or beyond that failed head, and
the approved plan, old generation and qualified R22 hashes remain exact. A
failed predecessor need not continue to its originally signed terminal block;
the successor still has to run a distinct fresh full interval. Its current read-only
preflight exited 75 because the live controller still owns `deployment.lock`,
which is the required behavior. A recovery audit found and fixed two partial
rollover holes: a stop command that fails after stopping the service and a
binary replacement followed by a manifest-write failure now both restore the
old binary and manifest, restart the old service, and verify the restored
generation. Terra medium's six deterministic rollback and predecessor-admission
tests, Python compile checks and all three shell syntax checks pass. `PREPARED.json` and
`SHA256SUMS` have SHA-256
`ed172c79379102a4271c159afafeaf20a9b63618a8cdacfbdb7f46eff254e531`
and
`5224266949f2153cb8017caa3e6d501563518807a3b65cd716208576e99f5f66`.
No rollover or R22 campaign launch has occurred.

The post-epoch-437 suffix audit then found a successor blocker in the frozen
R22 candidate: a restarted miner swarm emits an exact three-line stale
Network-contract rejection while its own rolling process-restart fault is
active, but classifier v3 still records that attributable transient as a
generic blocking error. Do not apply the prepared candidate until the narrow
classifier-v4 repair and its lossless migration tests are qualified and the
candidate, qualification, and guarded-rollover hashes are resealed. Rejections
with no active fault, the wrong target or fault kind, a non-Network primary, or
a malformed shape must remain blocking.

A read-only recorder is also armed for the first complete retained R20
observation at or beyond epoch-438 boundary block 8,027,074. It binds the
existing controller PID/start-time/executable, copies the exact observation
prefix and signed attempt, captures the LAN head and runtime ledgers, and
verifies a self-contained checksum manifest before publishing the directory.
Its script SHA-256 is
`d47768612b58ccdc2425aefc7c3445dd5272a3f39c4be8ec0ab0267601307750`;
at 17:17 UTC it was waiting under PID 2741385 and had not altered the live run.

Live checkpoint, 2026-09-17 11:45 UTC: the retained campaign is running again
against the LAN RPC. R17 corrected provisional native-epoch rollover,
retryable attempt-stream interruption, serial operator collection and the
supervisor's lifetime restart cap. Its focused normal and race suites and the
broader affected validator suites pass. A controlled supervisor generation
replacement at 10:52 preserved the plan, journal and signed successor and
started all 33 processes from qualified binary SHA-256
`64b0f7f5fcee78d032ebdb6c9e25169a18a102626338e64301f72c65a6540b62`.
The current generation has supervisor PID 2032750, both validators live and
zero child restarts. Preserve the earlier restart evidence; the zero counts are
for this replacement generation only. The sealed qualification is
`/mnt/data/sn-testnet/qualification/runtime463-r17-restart-recovery-20260917T1049Z`.

The first R17 controller authenticated all 4,674 verified receipts and stopped
before a scenario action, transaction or spend because its completed
provisional topology pointer still named the prior supervisor generation. R18
now adopts the current generation before campaign execution and carries the
exact append-only process-log cursor into a distinct generation-specific gate.
The prior gate stays immutable. All 29 selected adoption, process-log and
supervisor-readiness tests pass normally and under race detection. Qualified
controller SHA-256 is
`c537b4bdfd2119c428627c994f65239698cea218b3a223dc4d78e39d2c2084cb`;
its sealed evidence is
`/mnt/data/sn-testnet/qualification/runtime463-r18-generation-adoption-20260917T1118Z`.

R18 started at 11:23:19, authenticated the same 4,674 receipts with zero
failures, proved no pending transaction or spend, and adopted the R17
generation at 11:24:28. It then authenticated the four-source retained relay
continuation (2,376 files) and reached the real fleet lifecycle gate at
11:43:40. The target UID 7 still has one rao of emission while runtime prune
UID 1 is internally consistent, so the controller retains prepared state and
polls each finalized block without replaying preparation. The finalized subnet
schedule at block 8,025,273 had tempo 360, last epoch block 8,025,011 and next
normal epoch block 8,025,371, approximately 12:04 UTC at the observed cadence.
This is a live pending campaign, not acceptance; `final_acceptance=false`.

Live checkpoint, 2026-09-17 09:22 UTC: the persistent supervisor has remained
up since 04:57 with no systemd restart and all 33 managed processes healthy.
The R15 campaign controller retains the prepared successor and both
pre-acceptance lifecycle filters while waiting for target UID 7 to reach zero
emission. Validator 1 has one managed restart; validator 2 has four. Validator
2 restarts one, three and four came from the strict release-steering guard
exiting when ongoing recovery remained incomplete at a native epoch change;
the first also retained the then-unfixed deposit-audit mismatch. Restart two
was the controlled binary replacement also applied to validator 1. The scheduled
fault lane has not begun and caused none of this churn.

The provisional-only correction now continues an exclusively typed pending or
stale-snapshot cut in the fresh native epoch without terminating the validator.
Strict mode, mixed real failures, scheduler failures and ordinary submission
failures retain the existing continuity refusal. Five focused tests pass
normally and under race detection. Qualified candidate SHA-256
`3187bd93063356fbfb73fb7040fbf57ba1ffe3ec6380a61d1c80ba03144fd461`
is staged atomically at the supervisor child path for the next already-required
managed restart; no live PID or restart count changed. Its sealed evidence is
`/mnt/data/sn-testnet/qualification/runtime463-r16-provisional-cut-rollover-20260917T0915Z`.
Do not spend validator 2's fifth restart solely to activate this fix. The next
native checkpoint is block 8,024,651; verify actual filtered weight submission
there before changing the live topology.

Latest recovery checkpoint, 2026-09-17 UTC: the combined provisional
succession, retained-context rendering and local-intent correction is qualified
in driver SHA-256
`b85ab8529118c9bc9f2848865882dfa1291651a1c08de8ad28bffcb72857707d`.
All 19 selected roots pass normally and under race detection; seven causal
controls reproduce their intended failures. Two live corrected resumes then
proved the product path and exposed wrapper-only admission issues. Both
authenticated all 4,674 receipts, generated the exact corrected configurations,
started all 33 processes with zero restarts and reproduced no authority or range
refusal. The first wrapper rejected one transient miner health sample. The
second recorded 24 five-second samples: both validators and every non-miner
service were healthy in all 24, every miner was healthy in at least 22, and nine
samples were 33/33. It timed out only because `local-readiness` is emitted by an
internal `WaitReady` caller that this provisional startup does not invoke.
Both failed owners rolled back successfully without changing retained contexts,
the executable or release lock.

The third admission wrapper observed 16 samples over 82 seconds, permitted
health flicker only for miner swarms, and requires every process to be observed
healthy, at least three 33/33 samples, stable live PIDs and OS start ticks, zero
restarts, continuously healthy validators and other critical services, both
startup-authority markers and no refusal. Local readiness is recorded and
deferred to the actual campaign. Terra medium passed all 26 deterministic cases,
including both real 16-sample windows from the second run. The matching campaign
consumer passed 52/52 targeted cases and retains the separately qualified
43-case fail-closed finalizer unchanged. Live retry three completed at 19:57:09:
all 33 identities remained alive and stable with zero restarts, every process
was observed healthy, critical services were healthy throughout, and seven
samples were 33/33. Its result SHA-256 is
`4f2740bd3b9cb542636c75f42608a232c7c90643176d6bbe85497a2cedebeae6`.

The bound campaign owner then ran from 19:58:56 through 20:00:05 against the
LAN RPC. It authenticated all 4,674 retained receipts with zero failures and
created the signed durable successor attempt, SHA-256
`9aa94b86effb8aafc37bee904ff1e909a14edc6630a67f0a5588728ad7900417`.
Before preparation, source-capacity admission rejected the faster 15-second
owned-RPC poll profile: its deliberately conservative retry forecast is
6,012,260 requests/hour versus the unchanged 4,194,304 protected ceiling.
Objects and bytes still fit, no transaction or spend was pending, and the live
topology remains in place. The next invocation reopens that same successor.
The narrow provisional-only advisory then passed 21/21 normal and 21/21 race
roots; its pre-fix causal control reproduced the original capacity refusal.
The incremental retry wrapper passed 81/81 cases and reopened the same successor.

The real retry ran from 20:43:00 through 20:45:18. It reauthenticated all 4,674
receipts, recorded that no transaction or spend was pending, and reached the
owned-LAN execution reader. The chain had upgraded directly from runtime
461/1/1 at block 8,020,753 to 463/1/1 at block 8,020,754. The compiled exact
runtime catalog therefore refused 463 before preparation or any scenario
action. Plan, journal, config, roles, predecessor evidence and the signed
successor remain unchanged. The observed 463 code and metadata BLAKE2b-256
hashes are `0x9745e3f66053c3c7cb30ea45b88c66438b5076da78154f477e8660b0ded43869`
and `0xe9af0fcab804e08c0f6cc2c13715b1e366a916eda6a61aec6fb2601bc2a66b4c`.
Both validators later exhausted five restart attempts because their retained
binary also pins 461; the other 31 processes did not restart. Sol max fixed the
two deterministic fixture defects exposed while qualifying runtime 463 and
reviewed their adjacent callers. Terra medium passed the final exact 7-root
CRV4 and 6-root simulator matrix normally and under race detection, with every
source, dependency and frozen-manifest fence unchanged. The admitted candidate
SHA-256 is
`92f25f05f6e98dddee36146cdcd6ffe338e2c1d46090f0f6042098285724ff56`;
the qualification `RESULT.json` SHA-256 is
`03d3139ccd27df848e73383a1c01a80ea05513bd630e22bab60af72b9d59fa23`.
The R5 retained-state recovery bundle remains sealed and unexecuted: it has no
`REQUEST.json` or start marker. The next agent should bind that request, perform
the qualified stop/resume, preserve all durable work and reopen the same
campaign successor. `final_acceptance=false`.

Previous recovery checkpoint, 2026-09-16 17:02 UTC: provisional resume completed
at 16:52:13 under the existing approved plan and LAN RPC, launching and adopting
all 33 processes. The first release-candidate command authenticated 4,674
retained receipts, then closed at 17:01:52 with body/outer exit one before a
measured phase: campaign succession unconditionally refused provisional mode.
The executable and release lock are unchanged. Validator 1 restarted twice and
validator 2 once after HTTP 403 staging refusals. Regenerated
operator staging configuration omitted the explicit provisional retained-context
authority and retained an expired 16,384-block complete-discovery window. Keep
the 33-process topology while Astra max batches narrow succession and render
corrections and Terra medium qualifies them. Resume from retained state without
repeating setup. Process presence alone is not terminal publication evidence.
`final_acceptance=false`.

Previous recovery checkpoint, 2026-09-16 16:38 UTC: provisional resume was active
under the existing approved plan and LAN RPC. The prior strict startup timed
out at 15:41:52 without fresh proof trails; its eight finalized operator
transactions and original signed bytes have been reconciled. The provisional
driver's targeted checks are complete, with retained passes reused. Defer
historical replay, release packaging and final acceptance audits; proceed from
successful service startup directly into the actual campaign. Keep spending
limits, transaction reconciliation and saved progress. The measured campaign
has not started at this checkpoint; `final_acceptance=false`. Details and chain
receipts are in [report 2](sim-testnet/FINAL-2.md).

Previous recovery checkpoint, 2026-09-16 13:23 UTC: the runtime-461 correction is
qualified and integrated at `8edb3167a6261bfd82ecbc5f3c0ac2c787beec7c`.
All 103 affected roots pass normally and under race; four causal restorations
produce exactly nine expected failures and nine passes. Root verified all
15 raw body streams, exact compiled membership, actual exits and unchanged
inputs. The exact-Wasm probe also passed. The review caught and fixed the
adjacent historical companion check that would reject former-current runtime
460 approvals, plus a stale doctor test fixture before compilation.
The [closed qualification](sim-testnet/peerreview/evidence/FINAL-2-runtime461-qualification-20260916/README.md)
retains the complete evidence. The read-only release-lock preview passed;
the exact candidate, SHA-256
`aad35e8488e48190071889b3dec47c184ed9d2deedc30c44e3b52ee6f17afd84`, changes only
observed protocol/SN Go digests beyond the qualified static461 identity.
Publication, final matching build and native recovery follow. No native owner,
helper or campaign is running. Current saved plan remains f6e8c46e, retaining
the original history, limits and completed actions. Reuse unaffected tests.

Previous recovery checkpoint, 2026-09-16 12:03 UTC: startup and direct campaign
handoff are published at `1860261f524054b3d8132a48f75307ffdb792322`; the matching
CLI built successfully, SHA-256
`114bede0b30a9bc9fdb946f3075d1e1f3ff9b3e36daf57d1c084f6144e43bb07`.
Read-only plan revision passed. The first setup failed before preparation
because the physical checkout was detached; attaching the same commit to its
`origin/main` tracking branch repaired that prerequisite without rebuilding.
The retry ran 11:43:40–11:57:57, completed all 4,673 carried checks, and collected
nine failures from newly observed runtime 461. Its body/outer/join exits are one.
Eight hard preparation checks passed; the single hard failure aggregates those
nine runtime-admission errors. Setup adopted plan `0xf6e8c46e6a6a79c7c67deb8304e387f4bc9821ad96513c0ab6851d954d0d3bc6`
before stopping; only plan/config changed, with journal, supervisor state and
manifest, identities, executable and lock preserved. A fresh LAN read confirmed
461/1/1 at finalized block 8,018,145. No new END or native continuation was
created. All four temporary helpers stopped and joined with exit zero.
The [closed preparation evidence](sim-testnet/peerreview/evidence/FINAL-2-runtime461-preparation-20260916/README.md)
preserves both failures and actual results. Astra is reviewing the 461 artifact,
consumed interfaces and adjacent admission paths; Terra will qualify the
affected correction. Reuse completed startup/handoff results and retain all
approvals, signed history and full campaign requirements. RC remains unexecuted.

Previous recovery checkpoint, 2026-09-16: strict managed resume 92055 failed its
five-minute owned-node semantic-readiness wait at 10:20:25. Body/outer/join exits
are 1. All 1,000 fleet and 4,673 carried-action checks completed; the new generation
reached 33 healthy processes with zero restarts, but all four fresh proof domains
remained pending. Both validators were still replaying retained settlement
history when cancellation reached them. Cleanup stopped all 33 processes. The
last process-log scan at 10:20:10 had no findings; no later log scan is claimed.
Only watched supervisor manifest/state changed; plan, journal, config, public
identities, executable and lock are unchanged. RC remains unexecuted. The narrow
startup correction is qualified below; its deployment remains pending. Reuse
completed qualification. Reconciliation confirmed all 14 new
operator attempts have canonical status-1 receipts; actual fees total
0.029033073172513564 EVM TAO. Both validator prefixes are unchanged and no
N1488 decision/preparation occurred. Root restored only the 14 original signed
RLPs at 10:35:53, preserving all 2,272 originals and all six watched files,
with no broadcast or DB/journal mutation. The complete collector union is now
2,532 signatures. The separate closed capture is
`/mnt/data/sn-testnet/qualification/native-recovery-20260916-r4/signature-restoration`.
Terra qualified candidate `8270992e`: all 11 affected roots pass in normal/race;
each causal variant has exactly one intended failure and ten passes. Root
checked raw events, membership, exits and unchanged inputs, then fast-forwarded
primary to that commit. The retained strict startup allowance is 30 minutes
across RPC routes; fresh-proof and all other readiness gates remain. Physical
deployment still uses the `aeda6abb` executable. The explicit handoff from strict
resume into the full campaign is now qualified and integrated at `e109ac35`.
All 14 affected roots pass normally and under race; the causal control produces
exactly three intended failures and three passes. Normal acceptance composes
four retained passes, nine corrected-capture passes and one previously omitted
root; every failed invocation remains recorded. The earlier 11 readiness roots
are reused. Terra will own the combined native command, which retains all full
campaign checks and avoids a second preparation pass. The
[closed orchestration qualification](sim-testnet/peerreview/evidence/FINAL-2-resume-campaign-handoff-20260916/README.md)
records the source, composition and original harness errors.
The [read-only release-lock preview](sim-testnet/peerreview/evidence/FINAL-2-retained-startup-release-20260916/README.md)
passed under root session 84281; only the SN source hash changes. Its exact
candidate lock, SHA-256
`42a48da4d4fd267c496b8558cd9838f68b3f5e4117a9e681352129069df2df22`, is installed
in the primary checkout. Matching publication/build and native adoption remain
pending. R4 native wrappers and four temporary RPC/API helpers are prepared but
unexecuted; no new plan is adopted yet. Original compiler bodies that wrote literal `$capture/` outputs
exited zero, while their wrappers exited 126; corrected builds reproduced the
same hashes. The original failures and premature cleanup inventories remain.
The [closed startup qualification](sim-testnet/peerreview/evidence/FINAL-2-retained-startup-qualification-20260916/README.md)
contains 70 verified payloads with manifest SHA-256
`f4c67339990c97bc2cac866bc049074291b6f869891d01a86361e06ce6fd4577`.
The [closed failure evidence](sim-testnet/peerreview/evidence/FINAL-2-managed-readiness-20260916/README.md)
has 27 verified payloads and seal
`81fd7d5c8140fa02bb58184cdb562906ad7386dcd33a078316f40d42a3a2bc2d`.

Previous checkpoint, 2026-09-16 09:41 UTC: both preparation fixes, their evidence
and the release lock are published at `aeda6abbd2dc0abc92bb0f60975cf89b509e8017`.
The matched executable built successfully with SHA-256
`8fc61a65cd0524413a7ba70c61bcdb15962fa87ad7ab347b653abb27f8913f0b`.
Physical server remains the qualified `6752a8df`; publication merge `006e71b9`
preserves that commit. Native read-only planning passed at 08:02:31 with all
watched state, binary and lock bytes unchanged. Root reviewed the complete
lossless diff: all 4,733 actions, limits, spending, renewals, continuation,
custody, config and policy remain unchanged. The successor is
`0x49ddbc495a51c7c089ed5838299d6d65fb40cfb9b35be3ee38cd4ca1de7aa876`.
Prepare-only setup/adoption ran 08:12:47–08:27:00 and passed, with root session
49000 joined zero. All 4,673 carried-action checks completed; only the saved
plan and redacted config changed. All nine hard preparation checks passed;
launch-runtime-inputs remains explicitly deferred to resume. All four temporary
helpers passed readiness; proxy readiness uses `/healthz` and API readiness uses
`/status`. Initial API probes at the wrong path are retained as diagnostics.
A finalized LAN observation at 08:27:57, block 8,017,083,
selects continuation END=8,025,143 and full-work start cutoff=8,017,573, preserving
the required 7,570 blocks of work. Continuation capture ran 08:31:32–08:46:41
under root session 52913 and exited one: the nonce census could not find
operator-1-root nonce 106 in its retained signed-transaction sources. It emitted
no plan; state, binary and lock hashes are unchanged. The transaction is one
of the four already finalized partial-start writes documented below. The complete
census of both operator databases verified all 230 signed attempts, including
replacements and cancellations: 226 were already retained and exactly four were
missing. At 08:57:55, create-only restoration added their original 178-byte RLPs
while preserving all 2,268 existing RLP files and the six watched state files.
No transaction, journal/DB-status change, additional authority or source rebuild
was needed. The sealed census is at
`/mnt/data/sn-testnet/qualification/operator-signed-attempt-census-20260916-r1`;
root's separate `restore/` receipt records the additions. A fresh finalized LAN
observation at 08:58:59, block 8,017,238, selects END=8,025,298 and full-work cutoff
8,017,728. The fresh capture ran 09:00:03–09:15:28 under root session 35694 and
passed with unchanged state, binary and lock hashes. Its successor plan is
`0x17e49d00a7ce6aafac856e81a4ccf9eb37e4714ba7570a24cfa1c97a2d941f37`,
captured in `relay-capture-r3` using `relay-end-restored.env`. The complete
review found exactly 27 changed paths, confined to continuation window/source
observations, restored-transaction census/nonces, plan identity and ancestry.
All 4,733 actions, limits, spending, renewals, custody/config/policy and retained
signed ledger history remain unchanged. Exact import ran 09:19:38–09:35:50 under
root session 30705 and passed, reporting zero chain transactions. Only the
watched saved plan changed; its bytes equal the captured successor exactly.
A finalized LAN observation at 09:36:23, block 8,017,425, selects first native
epoch 1,488, spanning blocks 8,017,451 through 8,017,810. It leaves 386 blocks
for startup/first decisions and 303 until the full-work cutoff. History capture
passed at 09:37:28 with all watched state unchanged and both original validator
intent prefixes preserved. Its exact 2,201-byte bundle is stored privately under
`history-adoptions/native-1488-f2a7e1a24fc02cb9fbc3ee85af757b5795982ec42731f2b9fd34564485c479c9.json`.
The [closed signature recovery evidence](sim-testnet/peerreview/evidence/FINAL-2-signature-recovery-20260916/README.md)
preserves the failed capture, all-attempt census and restoration separately.
Terra stopped all four helpers at 09:40:08. Exact PID/start-time and process/listener
observations prove termination; their terminal exit codes are unavailable.
The teardown owner joined zero. Two pre-signal launcher/preflight errors and the
original invalid receipt manifest remain preserved with a corrected closed seal.
Root independently checked process absence and port closure, then started strict
managed resume at 09:41:17 under session 92055. The release-candidate request is
bound to the same final plan and waits for healthy resume before Terra executes it.
The [closed launch handoff evidence](sim-testnet/peerreview/evidence/FINAL-2-launch-handoff-r3-20260916/README.md)
preserves import, history selection and helper teardown. Its 93-file manifest,
and the [capture/review package](sim-testnet/peerreview/evidence/FINAL-2-relay-capture-r3-20260916/README.md),
were independently checked by root; neither claims a startup or campaign outcome.
Captures are in `/mnt/data/sn-testnet/qualification/native-recovery-20260916-r3`.
The [completed build and plan-review receipts](sim-testnet/peerreview/evidence/FINAL-2-preparation-adoption-20260916/README.md)
preserve their exact results and lossless comparison; setup has its separate
terminal receipt under the native capture above.
Keep publication frozen during these dependent commands. The soak remains
stopped, and actual managed-startup verification of both fixes is pending.

Previous checkpoint, 2026-09-16: strict resume exited 1 at 06:29:05
after processing all 4,673 carried-action audits and starting the managed
topology. Its process-log gate recorded four blocking classes in the new
generation: each operator taskworker emitted two unclassified errors and one
warning. Cleanup stopped all 33 managed processes. The saved plan, journal,
redacted config, public identities, executable and release lock are unchanged;
the watched supervisor files changed. Preserve this failed invocation and its
exact generation logs. The release-candidate request was not executed.
The four classes came from production backend jobs outside this simulator's
operator workload: geolocation certificate-pin rotation errors and fiat-payment
warnings for synthetic accounts. The explicit taskworker profile is frozen at
SN `8e6d56b5` / server `6752a8df`; all 30 affected roots pass normally and
under race, with seven expected failures/five passes in the controls. Preserve
the earlier service-census refusals, including a disposable launcher that
cleaned up before joining its children. Corrected ownership let the unchanged
binaries complete. The preparation-cost correction now
qualifies 32 roots normally and under race by composing 30 unaffected passes
with two fixture-correction passes. Its original two expected failures/six
passing controls remain retained. Root integrated both exact fixes at
`dc90e4c`; publication and deployment remain pending. Preparation now removes
repeated journal scans and historical-plan authentication within one
collection. The [affected qualification](sim-testnet/peerreview/evidence/FINAL-2-preparation-fixes-20260916/README.md)
retains the original failed and accepted receipts. All 66 generation log slices are
retained; root verified the private bundle's 115 sealed entries. See the
[public failure projection](sim-testnet/peerreview/evidence/FINAL-2-managed-start-failure-20260916/README.md).
The [read-only lock preview](sim-testnet/peerreview/evidence/FINAL-2-preparation-release-20260916/README.md)
passed at 07:39:57 UTC. The reviewed candidate changes only SN/server source
hashes and has SHA-256 `bf417189d4c0a62f8116606f84f9c5509b3afe2dab611429c9b781998b3198fc`.
Publish the complete source/docs/evidence/lock batch, build one matched CLI,
then use the prepared native recovery commands in
`/mnt/data/sn-testnet/qualification/native-recovery-20260916-r3`.
Partial-start reconciliation found four successful operator EVM writes in
blocks 8,016,488/489/491, confirmed below finalized height 8,016,641 through the
LAN node. Preserve their retained signed attempts; the production reconciler
must update its mined/broadcast rows from canonical receipts before retrying.
The [portable transaction evidence](sim-testnet/peerreview/evidence/FINAL-2-startup-transactions-20260916/README.md)
records all four calls, logs and the 0.008264277772552846 EVM TAO gas fee. No full gate
restart or duplicate funding is authorized by this failure. Refresh the time
window and first native epoch late through the supported continuation path
after necessary fixes; the soak and full acceptance remain pending.

Previous checkpoint, 2026-09-16 05:48 UTC: continuation import passed at 05:39:02
using the matching `0fd7ffc0` executable (SHA-256 `d50a4612ed4bd34838bd4a5b24f91b79e5b3d1ff55f76198178c26a76234dfbf`).
It adopted plan `0xb7fd2eb5030f73b424b3449d21302e6b3d17cd85142f4ca61b18aa1e0c2b5b17`
with zero chain transactions; only the saved plan changed. History capture
passed at 05:41:45 with all watched bytes unchanged. Finalized native block
8,016,244 selects first native epoch 1,485, spanning blocks 8,016,371–8,016,731.
The exact history bundle is retained under the state-owned `history-adoptions/`
directory with SHA-256 `e556044d5cf4b584856df3dc7c0a199a582ce14130f5818c09190ac54673eb53`.
All four temporary helper tool sessions were recorded as joined with exit 0
in the r2 `temporary-services/STOPPED.json`, with their original PIDs absent.
Separate raw helper-join results are not in the portable continuation bundle.
Strict managed resume
started at 05:47:36 under root session 97810; join this owner before entering
the release campaign. END=8,024,100, required work=7,570 and start cutoff=8,016,530
remain unchanged. Keep publication frozen through dependent native calls.
The soak and final acceptance remain pending.

Previous checkpoint, 2026-09-16 05:18 UTC: continuation capture passed at 05:14:43
with unchanged watched state, executable and lock. Root reviewed its complete
raw diff; all actions, spending, renewals and signed history are unchanged.
It emitted plan `0xb7fd2eb5030f73b424b3449d21302e6b3d17cd85142f4ca61b18aa1e0c2b5b17`.
Import then failed before mutation because root's report-only publication had
advanced GitHub main beyond the executable's revision. Physical source is now
clean at `0fd7ffc0`, with all Go code/modules and release-lock bytes unchanged.
Terra is building one matching stamped executable. Reuse the captured plan
and qualification; do not repeat setup/capture. Freeze further publication
during the attested apply/resume/RC sequence. Soak and final acceptance remain
pending; the original HTTP timeout and actual import refusal remain recorded.

Previous checkpoint, 2026-09-16 05:00 UTC: both exact-object reads passed through
the local APIs in under one second, with identical 157,602-byte bodies and the
expected content hash. Two source reviews found no deterministic defect; the
original transfer stall remains unproven. A new capture is running under root
session 46348 from 04:59:42, in `native-recovery-20260916-r2/relay-capture-r2`.
It retains the qualified build and saved setup plan. Finalized LAN block
8,016,040 selects END=8,024,100, full-work cutoff=8,016,530, with unchanged
7,570-block work and 490-block initial preparation margin. Join this exact
owner and, on success, import its exact output before late history selection,
strict resume and RC. The soak remains stopped. No timeout, authentication
rule, spending limit or required campaign work was weakened.
[Preserved failure and exact-object diagnostics](sim-testnet/peerreview/evidence/FINAL-2-relay-stream-failure-20260916/README.md).

Previous checkpoint, 2026-09-16 04:55 UTC: relay capture exited 1 at 04:48:52
without emitting a plan. State, binary and release-lock bytes are unchanged.
Both operator APIs timed out after 30 seconds while streaming the same
metadata object; the CLI reported incomplete authenticated EOF. Astra is
diagnosing the stream path and adjacent timeout handling, while Terra checks
the exact object through both live local APIs. Preserve the failed command and
successful setup. No continuation was imported, no new epoch selected and no
transaction submitted. The soak is stopped. Choose the next capture window
late after the cause is resolved; do not rerun an unchanged failing command.

Previous checkpoint, 2026-09-16 04:42 UTC: setup/adoption completed successfully
at 04:30:51 with `ready=true`, `prepare_only=true` and
`stopped_before_actions=true`. All 4,673 carried-action checks completed;
only the saved plan and redacted configuration changed. No setup transaction
was submitted. The launch-runtime-inputs check is explicitly deferred to
resume. Four temporary artifact helpers are healthy. The existing relay
capture owner (root session 6586) is processing retained signed history with
END=8,023,974, selected from finalized LAN block 8,015,914. The full-work
start cutoff is 8,016,404; native capacity admission still controls the actual
request. Join that owner, import its exact output, select the first native
epoch late and capture history, then join the helpers and resume directly into
the campaign. The soak is still stopped. Captures and prepared commands are
in `/mnt/data/sn-testnet/qualification/native-recovery-20260916-r2`.

Previous checkpoint, 2026-09-16 04:16 UTC: the reserve-preserving release and lock
are published at 541e13cf, and its matched CLI built with all fences passing.
The native plan retry exited 0 at 04:06:54; all watched state, binary and lock
bytes were unchanged. Root reviewed the full byte-preserving diff: all 4,733
actions, limits, spending, renewals and continuation are unchanged. The new
plan is 0x0d24a3f1dfc8ea5bc6a2f59c80a7580a761d9e3410dd4833bda3843304ce6f86.
Setup has saved that revision and is still checking retained history under
root session 55890; no new journal entries are observed. Join its actual result,
retain any stopped-namespace refusal, then continue directly through temporary
helpers, late continuation capture/import, late native-history adoption,
strict resume and RC. Do not run another setup or doctor. The operational
capture is /mnt/data/sn-testnet/qualification/native-recovery-20260916-r2.
No new continuation end or first native epoch is selected yet; the soak is stopped.

Previous checkpoint, 2026-09-16 03:54 UTC: the exact software-only reserve
correction is qualified with 28 affected roots normally and under race. Root
checked both exact accepted sets against raw terminal events. The original
planner produces three expected failures and three passing controls; failed
fixture invocations remain preserved. Publish the qualified correction and
evidence, preview and publish its release lock with the existing CLI, then build
one final matched executable. Adopt one native revision, select continuation
and native epoch late, and resume directly into the actual campaign. Do not
repeat prior qualification or finalized funding. The 60% operating floor,
historical 65% repair proofs, full approval comparison and all limits remain
required. Native adoption and soak acceptance remain pending.
[Qualification and retained refusal](sim-testnet/peerreview/evidence/FINAL-2-release-reserve-recovery-20260916/README.md).

Previous checkpoint, 2026-09-16 02:57 UTC: the journal patch and matched CLI are
ready on `1270adc`, but the read-only native plan revision exited 1 at 02:48:30.
Its generic reserve-target step requested another 471,808,849 alpha rao, while
the approved 37,250-alpha lifetime ceiling is fully committed. No plan was
adopted, no transaction was submitted and all watched bytes are unchanged.
The fresh complete census at native block 8,015,417 shows 64.9994918065%:
the 60% operating floor passes, the current 65% target does not. Retain the
completed repair's pinned target proof. Astra is preparing a narrowly scoped
software-only revision correction with deterministic and adjacent controls;
ordinary initial, unfinished, changed-economic and below-floor planning retain
the target and budget checks. Then adopt the qualified revision, choose the
continuation and native epoch late, and resume into the actual campaign.
The soak remains stopped; no additional allowance is authorized or assumed.
[Refusal and current reserve evidence](sim-testnet/peerreview/evidence/FINAL-2-release-reserve-recovery-20260916/README.md).

Previous checkpoint, 2026-09-16 02:30 UTC: runtime460 strict resume naturally
exited 1 at 02:13:45 after its full-work window expired. All 4,672 carried
checks, configuration verification and migrations completed; temporary
processes were cleaned up and no soak began. Preserve that failed invocation
and its verified render. The bounded journal-validation repair passes all
15 affected roots normally and under race, with two expected failures and
13 passing controls on the original scan. Retain prior qualification by scope.
Publish the patch and matched lock/build, adopt one native plan revision,
refresh the continuation from a late finalized head, then capture history
with a late first native epoch and resume into the actual campaign.
The existing fleet leases 393–424 can support this path: a relay capacity
forecast beyond 424 does not itself require renewal. Actual five/three-epoch
acceptance guards and all approved economic limits remain unchanged.
[Startup refusal and journal repair evidence](sim-testnet/peerreview/evidence/FINAL-2-journal-recovery-20260916/README.md).

Previous checkpoint, 2026-09-15 23:52 UTC: runtime460's affected qualification is
accepted by composition on effective source `7989fa78`: 108 roots per mode
(13 CRV4, 16 miner, 25 validator, 54 simulator), normally and under race.
Compatibility controls reproduce 13 expected failures and 11 passing controls.
Original compiler and fixture failures remain preserved; only the four failed
roots per mode and one affected causal control reran. Root checked the exact
passing root unions against the affected selection. Publish the qualified
source, render and publish its release lock, build the matching executable,
then revise the saved plan and refresh its continuation/history before native
resume and the real release-candidate campaign. The soak remains stopped;
renewal, funding, approvals and unaffected qualification remain retained.
[Affected qualification and raw receipts](sim-testnet/peerreview/evidence/FINAL-2-runtime460-qualification-20260915/README.md).

Previous checkpoint, 2026-09-15 23:13 UTC: the released 459 executable at
`36d3093` could not revise the saved plan because the owned chain had advanced
to 460. The plan attempt and diagnostic doctor joined with exit 1, with all watched state,
binary and lock bytes unchanged. All four doctor hard failures identify the
same 460-versus-459 admission mismatch. No intermediate 459 plan was adopted;
the next qualified release can revise the retained plan directly.

Runtime 460's pinned LAN Wasm equals the authoritative upstream CI artifact.
The existing exact-Wasm probe passed at 23:07:53 with all exits 0 and unchanged
inputs. The 459-to-460 source delta fixes share-pool accounting and preserves
our native call layouts; metadata differs only in its spec-version constant.
The catalog, current signing and historical replay correction was prepared
for the affected qualification now completed above. Retain the completed recovery qualification
below and all 1,212 renewal actions. The 23:05 pinned settlement epoch 396 still
supports the full proposed continuation under existing 393–424 leases. END and the first native
epoch remain unchosen until their required late capture. No new funding or
renewal is part of this recovery, and the soak remains stopped.
[Runtime460 artifact, compatibility and closed probe receipts](sim-testnet/peerreview/evidence/FINAL-2-runtime460-20260915/EVIDENCE.md).

Previous checkpoint, 2026-09-15 22:23 UTC: the startup recovery batch is qualified
on effective source `53b0e950` by composition: 154 affected roots normally and
under race (21 CRV4, 13 miner, 45 validator, 75 simulator). Six compatibility
control binaries reproduce exactly 12 expected failures and 6 passing controls.
Preserve the original compiler, test-fixture and interrupted capture failures;
only their affected replacements reran. Runtime459's exact CI/Wasm/metadata
review is complete, with original451–455/458 history retained. The same batch
shares stopped-ledger verification within startup, propagates cancellation,
checks full-run runway and refreshes the cumulative continuation through v4.
The stopped native deployment still needs the published release/lock, plan
revision, fresh continuation window and history, then strict resume and the
real release-candidate campaign. No completed funding or fleet action is reset.
Keep native vaultfb1/configb5 and LAN-only RPC. Full acceptance remains false.
[Runtime459 provenance](sim-testnet/peerreview/evidence/FINAL-2-runtime459-20260915/README.md).
[Qualified composition and raw test receipts](sim-testnet/peerreview/evidence/FINAL-2-startup-recovery-qualification-20260915/README.md).

Previous checkpoint, 2026-09-15 21:36 UTC: the old strict resume is stopped and
joined with actual exit 137. It retained 12 verified setup postconditions,
including one newly finalized native deployer funding transfer in block
8,013,647, alongside the completed renewal. The plan, supervisor and watched
configuration/identity files remain unchanged; the journal records progress.
The LAN node now reports runtime459 at finalized block8,013,770, where the
adopted continuation leaves only7,472 of the required7,570 full-run blocks.
The current build cannot complete the full campaign. Two Astra lanes are
fixing bounded continuation refresh/cancellation and runtime459 compatibility
in parallel, preserving historical458 authority and all spent allowances.
Terra's independent14 simulator checks pass normally; the cache's validator
test exposed an incomplete callback-storage initialization, now included in
the same correction batch. No full gate restart is required. The soak is
stopped and final acceptance remains false.
[Interruption and additional on-chain progress](sim-testnet/peerreview/evidence/FINAL-2-startup-interruption-20260915/README.md).

Previous checkpoint, 2026-09-15 21:23 UTC: strict resume remains active under
the same owner started at 20:14:29. It authenticated all 1,000 historical
fleets and 4,660 carried actions, verified both campaign reserve actions, and
entered `config.render`. The supervisor remains stopped. Source inspection
and repeated live ledger-descriptor transitions identified nine full signed
ledger replays before service startup; one observed replay took about twelve
minutes. Astra's isolated patch gives one startup an in-memory cache of
successful replays, with fresh hashing of all storage bytes and unchanged
caller/history/configuration checks. Terra is qualifying 11 validator and
14 simulator tests normally and under race detection on formatted candidate
`c9bf5e6`. Those results and deployment are pending. The physical c5 runtime
and current owner remain unchanged. Preserve completed setup, funding and
on-chain actions when adopting any required patch; reuse unaffected accepted
gate coverage. Final acceptance remains false and the soak has not started.

Previous checkpoint, 2026-09-15 20:19 UTC: the renewal completed at 19:09:41,
with all **1,212 actions** verified across **202 fleets**. The corrected relay
continuation was adopted at 20:04:13 with zero chain transactions, retaining
all required work through block **8,021,242**. Final restart history selects
native epoch **1,477** and is saved with its exact hash. All four temporary
services stopped and joined cleanly. Actual strict resume is running from
the retained state, using the admitted c5 executable. Its first attempt
refused a missing private temporary directory; creating the required
directories also fixed the adjacent campaign command's preparation. The retry
started at **20:14:29 UTC**, with no source, approval or plan change.
Continue directly to `release-candidate` after successful startup; no new
test suite, preparation gate, renewal or repair is queued. The soak has not
started and final acceptance remains false.
[Completed renewal and continuation evidence](sim-testnet/FINAL-2.md).

Storage relief completed at **20:44:30 UTC**: the existing Go build cache was
copied to the user-mounted `/mnt/data` volume and verified with no checksum
or metadata differences before removing the original copy. Its old path is
preserved by a link. This frees about **56 GiB** on `/`, which now has about
**78 GiB** available. Active simulator state, binaries and command captures
remain at their existing paths; startup continues under the same owner.

Previous checkpoint, 2026-09-15 17:48 UTC: producer and aggregate coverage are
**accepted by composition**. The succession fixture correction at `93f6d35`
passes its nine affected normal tests and 29 children. The complete capture
race passes all 378 tests and 140 children in 479.526 seconds, within the
unchanged 600-second limit. The old-fixture control reproduces the expected
work-bound failure. All actual exits, thirteen-repository source comparisons,
binary comparisons and cleanup checks pass. These are local test results;
the original failed and interrupted gates retain their recorded outcomes.
The full cohort ran with four processors on a lighter host load than the
previous simultaneous full gates; that execution condition is retained.
[Replacement qualification receipts](sim-testnet/peerreview/evidence/FINAL-2-incremental-capture-20260915/README.md).

The unchanged 39 producer phases and unchanged aggregate scopes retain their
complete earlier receipts. No full gate restart or extra confirmation is
required. The approved exact Q09ac683b fleet renewal apply started at
17:27:28 UTC using the existing canonical c5 executable and LAN RPC node.
It stopped at 17:33:09 on the host disk check, before any transaction; campaign
state, executable and release lock comparisons were unchanged. Removing two
generated fixture trees from a terminal test owner recovered 3,681,648,640
bytes and restored free space to 22.28 GiB. All 61 parent evidence artifacts
remained unchanged. The same exact plan resumed in a fresh capture at
17:46:51 UTC. One journal writer owns this operation; retain the exact saved
plan and any persisted transactions on interruption. Completion and new
finalized actions are not yet claimed. The soak remains stopped, with strict continuation and
the actual release campaign next. Keep canonical/source c5 frozen while this
native owner runs; local test and report publication can be prepared separately.

Previous checkpoint, 2026-09-15 17:12 UTC: recover the interrupted c5 gate
attempts through composed coverage. Root verified both outer processes and
their test descendants absent; the retained logs record stop143, with no
final outer exit or final source receipt. Producer retained 38 passing joins,
the capture failure and one interrupted database phase. Aggregate retained
five passing joins and two interrupted race bodies. Reuse unchanged phases
from the earlier completed 330ba512 gates. No full gate restart is planned.
The test-only succession fixture repair is frozen at `93f6d35`; Terra is
building the corrected normal/race and old-fixture normal executables for
the three admitted bodies. Qualification remains pending.

Actual read-only renewal planning passed at 17:07:03 UTC for epochs 393–424:
plan `0x09ac683bae8bf99362bfc427776987fce951db58b71b3f01966236abbf7c91f1`,
all 202 fleets, 1,212 new actions, bounded at 9.09 EVM TAO plus 0.606 native TAO.
Expired predecessor bindings require no revocations. State, executable and
lock comparisons passed unchanged; no chain transaction was submitted.
The exact saved plan and apply command are ready. Complete producer coverage
precedes apply; reuse the existing canonical c5 runtime while its source and
approved production inputs remain unchanged. The 18 native preparation passes,
approved allowance and finalized reserve repair remain retained.

Previous checkpoint, 2026-09-15 16:15 UTC: the published `c5db71a` producer's
capture phase passed normally in 120.920 seconds and timed out under race at
600.377 seconds. The active succession test repeatedly builds and authenticates
a full 1,000-provider plan; Astra is correcting that test fixture while Terra
keeps both full gates collecting their remaining results. The fix is not yet
qualified. Retain unaffected completed phases, the 18 passing native preparation
checks, adopted plan and finalized reserve repair. The next qualification is
the corrected capture scope and affected consumers, not two restarted full
gates. Renewal is unsubmitted; the soak remains stopped. Publish patches after
live source owners release their checkouts, and reuse the admitted runtime
build if its production inputs remain unchanged.

Previous checkpoint, 2026-09-15 15:05 UTC: the user approved the lifetime
increase to 205 EVM within 225 total TAO for one fleet renewal capped at
13.13 EVM plus 0.606 native TAO. Alpha limits remain unchanged. The vault
setting is published at `9651a13062af2fd25dcd9e98db8c8871d148d114`.
Native plan `0x922e280318f33cb20f5b15082bb6329890e9d8778f1effabf8baae521a57f4ea`
is adopted: setup preparation passed all nine hard checks and authenticated
all 3,449 carried actions, without dispatching an action. The complete renewal
preview passed for 202 fleets and 2,020 new actions within those ceilings.
Existing keeper and oracle balances cover that maximum; an extra funding
transfer is not a prerequisite. Its F388/T419 window is diagnostic and must
not be adopted as the final window.

The approved 6,000-alpha repair finalized in native block 8,009,634. The later
complete registered-stake census at block 8,010,632 shows a 65.5997247163%
reserve share, above the 65% target. Do not repeat the repair. Both temporary
artifact APIs and both RPC proxies stopped and joined cleanly. Full strict
resume preparation passed all 18 hard checks at 13:58:20 UTC, including both
validator namespaces, host readiness and runtime inputs. All 1,000 fleet
records and 3,449 carried actions were authenticated, with unchanged recorded
campaign state. The soak remains stopped.

On source `330ba512`, the complete producer closed with 39 passing phases and
one capture-race timeout; its final source checks passed. The complete aggregate
closed at 15:05 UTC with all 25 phases, cleanup and final source checks passing.
The two-test-file correction `03d529b` removes repeated whole-script scans.
Its 30 affected tests pass normally and under race, and the two defect controls
each complete three fresh passing processes in both modes. The actual 376-test
capture race, including 140 declared subtests, passes in 567.382 seconds within
its original 600-second limit. Both causal controls reproduce the old scan
failure. Existing Go verification reuses the successful raw results; earlier
compiler-owner and missing-metadata refusals remain recorded. The original
failed producer remains failed.

Production code, scripts, the release lock and approved native plan identity
are unchanged by this correction. Publish it with these report updates, build
one matching CLI and complete the final candidate gates. The producer receives
16 processors and the aggregate eight, retaining four processors per job and
all test limits. Start the CLI alongside the producer and admit the aggregate
when the CLI joins, keeping the combined reservation at or below 24. Reuse the
completed native preparation. Choose the actual renewal window, final relay
end and fresh native history epoch immediately before their native apply/resume
steps. The canonical executable and producer pass still precede chain apply.
[Approval, adopted allowance, renewal bounds and on-chain repair evidence](sim-testnet/peerreview/evidence/FINAL-2-approved-renewal-and-reserve-20260915/README.md).

Historical checkpoint, 2026-09-15 02:38 UTC: the explicit runtime configuration
identity correction is frozen at85c0958. Its normal/race70 matrices each have
69PASS/oneFAIL; both adjacent50 bodies pass. The sole failed root already
authenticated the original repair but then planned with a temporary mock RPC
route, correctly changing resolved inputs and config.render intent. The one-file
fixture correction5a411d2 preserves the approved route and adds a deterministic
changed-route control. Its three fresh normal passes and three fresh race passes
are complete with exact one-test lists, seven events each and all outer/body/
converter/verifier exits0. All13 repository/source observations and per-mode
binaries remain unchanged; the final owner closed02:38:10UTC. The680-entry
portable bundle is sealed with manifest SHA-256
`09713bbd8a396831b926cda1fe1ed758d7fc2b384fdedea1dfade5e186361fe9`.
Retain the69
unchanged passing roots per mode and earlier181/36 scopes. All
four original matrix bodies and their capture-only verifier refusals are saved;
corrected replay of their retained events passes without body reruns.

The original-hashing control closed3 expectedFAIL/2PASS with body1 and
converter/checker/outer0; all source/dependency/binary/diff observations match.
Its failing authentication occurs before the fixture's changed planning section,
so no new causal compile is needed. The updated read-only parser and lock render
also closed0; lock SHA-256 is
`c437900d8cb2d29ff0d363ef629b88ceb8ace35976bd28bfd194a2d8dc6eb639`.
Only SN's production Go hash changes. Idle integrationf9970d6 includes that lock
and the qualified fixture correction; it is not published. Seal retained
evidence and publish. Reuse the accepted
parser for actual read-only setup while the canonical stamped CLI and both full
gates run. The canonical executable and producer PASS still gate approved apply.
Soak stopped; repair unsubmitted; approvals and retained custody unchanged.

Historical checkpoint, 2026-09-15 01:39 UTC: the historical455 lock correction is
qualified and published at eccfae8a5f4176ccaa6099ce5ce555924057f3d3. Its final
CLI built cleanly and passed, SHA-256
`73264ab8365c6cf538390c53ed0a6a71a39d3972e405b916088559b486218e99`.
Actual read-only setup advanced past that check, then exited1 at01:26:11 with
`coordinator repair configured strict domain differs`. All six state files,
binary and lock remained unchanged; no successor plan or transaction resulted.

The public expected runtime455→458 change also changed ConfigHash, while
original signed repair/probe/activation history retains the previous identity.
Astra is implementing an explicit reviewed configuration-identity pin, bound
into the new setup plan while current runtime authority remains exact458.
The shared hash/admission correction and deterministic transition/adjacent
controls are not qualified yet. Preserve action intents, ResolvedInputsHash,
signed originals,1212 finalized renewals,3521 actions, generation3 and nonce34.

Both eccfae8 full gates were intentionally canceled once the native failure
proved a production successor was required. Producer closed143 at01:32:11;
aggregate closed143 at01:33:33. Each passed its first four preflights, was
interrupted during runtime metadata, and admitted zero test phases. All owners
and private process records are gone. The123-entry portable capture manifest
is `b6b1dfc4fa43778945fe3c4d1910d5c69bf2ee96654cc2d6c74ad761ab13ce35`.
These attempts are cancellations, not full-gate passes or executed-test failures.
An added public manifest field requires an updated parser for the next lock
render; overlap that one read-only build with the frozen-source qualification.
Then publish the successor, build the canonical CLI, retry actual setup and run
both full gates. Producer PASS still gates approved apply; soak remains stopped.

Historical checkpoint, 2026-09-15 01:15 UTC: the canonical c572 build passed.
The actual read-only setup then exited1 at00:24:56 because the historical
validator-evidence reader applied current458 lock validation to an authentic
original455 archive. All six campaign-state hashes, binary and lock were
unchanged; no plan or transaction was produced. Astra's narrow historical
admission correction is frozen at3ffc1277d1acd1e21b908c3b2cfd37612602e07b.
Terra's36-root normal and race matrices pass. Both reader/restart roots have
three consecutive normal passes on the same binary and source. Restoring only
the old historical dispatch reproduces both failures while both controls pass;
all source, dependency and binary fences close unchanged. Reuse the
completed181-root runtime qualification within its recorded scope.

Both c572 full gates closed with transport preflight exit28 before any phase or
service owner. Their first three preflights passed; GitHub source downloads
timed out. The exact source fetch recovered at00:31UTC and matched its pinned
hash. TF has clean exact v454/v455/v458 source overrides for the next primary
source loops. No source-fetch feature change or extra transport probes are
needed.

Root reused the c572 executable for the successor lock render, closed0 at
00:54:37 with19 equal before/after observations. Exact YAML SHA-256 is
`5b8c412454adfde72f2cf51c682b5ce4b9b335a2095eaafc21bf4e92a153a4ab`,
included in the qualified successor after integration628f29a. Only SN's production
Go hash changes; all other fields are equal. Publish this coherent successor
and retained evidence, build one matching stamped CLI and launch the full
producer38/aggregate concurrently with16+4CPU and the prepared source overrides.
Review one actual retained-state setup preview; producer PASS gates apply.
Soak stopped, repair unsubmitted, all1212 renewals/3521 action identities,
generation3, original signed probe anchor/nonce34 and approvals preserved.

Historical checkpoint, 2026-09-15 00:09 UTC: the soak remains stopped and the
6,000-alpha repair remains unsubmitted. Fresh LAN observations at 00:03:15 UTC
show a synced node, 16 peers, runtime 458/1/1 and finalized block8,007,358.
Xops main includes both the runtime-pin correction and qualified quota-test
correction at `42bfe0b`; the corrected deployment assertion has not been rerun
on the node. No further rollout confirmation is needed for simulator work.

The composed runtime correction passes 181 selected top-level tests normally
and under race, with required fresh-process confirmations complete. The old
capacity, stale gate inventory and public/paced route causal checks closed with
their expected failures and passing controls. The 13-test production-policy
causal also closed with exactly seven expected failures and six passing
controls. The original simulator41 failure remains recorded
as39PASS/2FAIL; the repaired simulator47 passes both modes. All unchanged client
and typed-prior qualification is reused within its recorded scope.

Both FC908 gates are closed failed: producer36/37 and aggregate24/25. Their
known failures are corrected and qualified; replacement complete gates are
still required. The final exact release-lock SHA-256 is
`d11b2a41ca6e836f9267088f8899c4fb0faf53b78b3b9ca8804bab589cd63e7b`,
committed only in the idle integration at `2b907a4`. Publish the coherent
candidate and report, build one canonical stamped
CLI, and run producer38 with16CPU alongside the aggregate with4CPU. Producer
success gates campaign writes. Preserve all3,521 action identities,1,212
renewals, generation3, original probe anchor and nonce34 in the single successor
setup preview. The repair and real RC/production soak remain outstanding.

Historical checkpoint, 2026-09-14 23:01 UTC: the soak remains stopped and the approved
6,000-alpha repair remains unsubmitted. The operator verified removal of the
nginx limits at 21:01:55 UTC. Fresh direct-LAN observations found a synced node
with 16 peers, runtime 458/1/1 and advancing finalized blocks; the separate
deployment failure was its stale expected-runtime 455 pin. Correction
`446cbdb` passed its complete 30-test module, required confirmations and the
old-pin causal control, and is now pushed to xops main. The corrected deployment
check has not been rerun on the node. No SSH credential or rollout confirmation
is needed to continue simulator preparation.

The FC908 producer closed with 36 passes and one cumulative capture race
timeout. Its typed-prior process correction `907d186` is fully qualified:
26 affected roots in both modes, three fresh race confirmations of the active
boundary and the expected old-script causal failure with passing controls.
Reuse those results. The original FC908 aggregate remains live with 21 phases
passed and no reported failure; its physical source remains fixed.

Runtime-458 source `df98472` includes exact artifact admission, historical455
retention, unpaced LAN artifact checks and a six-identity cache/allowlist bound.
Its 175-root affected qualification is in progress. The readonly CLI build and
combined release-lock render passed. The exact YAML SHA-256 is
`e72b2146a1cbbd59a54b0424a30478aa9a475f8d82369c028c4fe22e8f2d71c2`,
committed only in the idle integration checkout as `151b515`; all contract
hashes remain unchanged. Finish affected qualification, publish the coherent
source, then use a genuine canonical stamped CLI and both complete gates.
Preserve all 3,521 action identities, 1,212 renewals, generation3 custody,
original signed probe anchor and nonce34 during the one successor setup
preview. The earlier runtime455 preview was never executed and is obsolete.
No new plan hash or chain transaction has been produced by this preparation.

Historical checkpoint, 2026-09-14 18:50 UTC: the soak remained stopped and the approved
6,000-alpha reserve repair remains unsubmitted. All 1,212 renewal transactions
across 202 fleets have finalized and passed their native postconditions. Two
setup attempts on the retained approved plan stopped on nginx HTTP 429 before
any new transaction. The qualified unpaced gateway correction is published in
xops main at `d33d417`; deployment is awaiting an SSH identity accepted by the
RPC node. Do not retry setup until the deployed gateway has been corrected.

The `7eab049` producer closed with 34 passing and two failed phases. The probe
test correction passed a strict build, all 215 contract tests and three strict
lint processes. The combined scheduling/source-census successor `713eae3`
passed all 81 affected tests normally and under race, three confirmations of
both failed guards in each mode, and three race confirmations of all four
timeout roots. Both original-defect causal controls reproduced their expected
failures with passing adjacent controls. These completed scopes are retained.
The obsolete aggregate was stopped and joined at 18:49:42 UTC: 14 phases passed,
the same strict Solidity lint failed, and two Connect phases were canceled.
Its outer exit was 143; cancellation did not reach the final source fence.

The successor lock was rendered locally with unchanged production Go and
contract bytes. Its only changes bind the producer scheduling script and both
gateway/node configuration groups. The reviewed YAML SHA-256 is
`76cd7fa7031ee5566301173a94e1a4369ccbc871542de05119b46bbcb1461f35`.
Publish the coherent candidate, build one stamped final CLI, and run the full
37-phase producer and complete aggregate with private services. Producer PASS
precedes traffic; both gates and the full live campaign/production soak remain
required for acceptance. Use one subsequent setup preview and reviewed successor
hash; preserve every completed renewal, receipt, custody record and spend limit.
See [report 2](sim-testnet/FINAL-2.md) and its linked evidence bundles.

Previous checkpoint, 2026-09-13 23:48 UTC: the soak remains stopped. The full
producer on `e3d3539` passed with 36/36 phase joins at 21:44 UTC. Its native
setup apply adopted plan `0xdbeb584008bbdbc6607a49a5118c1c82fdfa18a15ca8fe5b5c8cea9d2775c37a`
but failed at 22:09 UTC during carried-history verification, before action
execution. The journal and both supervisor files are unchanged; the approved
6,000-alpha repair is still unsubmitted. Do not retry the old executable.

The user now requires every actual testnet RPC call to use the LAN node,
including historical and final verification, with no RPC request pacing.
The `7de62c7` correction introduces the explicit owned-node observation profile
and reuses already authenticated historical inputs within an invocation.
Historical receipts retain their original labels. New evidence identifies
`independent_rpc=false`; public-node verification is no longer required for
this run. Read-only LAN probes have returned the tested historical EVM and
native storage successfully. The seven source/routing guards pass normally
and under race; the 33 historical-input checks pass normally. All four roots
active at the original race timeout have completed their three sequential
race confirmations on the same binary. Other affected checks remain pending.

The original aggregate closed with outer exit 1 at 23:14:29 UTC: all 23 phases
joined, with 20 passes and three failures. These were the cumulative simulator
race-package timeout, missing server migration-monitor entries and a context
deadline in the full 1,000-client registration cohort. Its final source check
also refused because canonical SN main advanced during execution. The
[complete original capture](sim-testnet/peerreview/evidence/FINAL-2-aggregate-e3d3539-20260913/README.md)
is retained; do not restart the old gate.
The corrected aggregate partitions all 2,198 simulator roots among five
disjoint owners. Server candidate `0f095a6` includes the published monitor
correction, a real PostgreSQL regression and one synced publication source
shared by both immutable writes. It keeps the original 1,000-client population
and 30-second operation deadline. Terra's qualification remains pending;
the diagnostic passed without reproducing the original deadline, so it is
not evidence that the correction closes that failure.
SN main also advanced independently to `928b7d5`; its provider memory-budget
fix is included in the next integrated candidate. Complete focused checks,
publish one coherent release and start both final gates. Then use one native
setup preview/review and exact hash-bound apply, followed by renewal and launch.
No duplicate preview or standalone full partition diagnostic is required.

Previous checkpoint, 2026-09-13 19:32 UTC: the soak remains stopped. The user
approved one 6,000-alpha replacement for the unsubmitted 3,750-alpha repair,
within 37,250 alpha lifetime. The single-setting vault change is published at
`8b2f481dbe87092d0c1742274712a6f805c1c375` in the independent final candidate
checkout. The final race owner has closed and released its primary-vault
source hold. The primary vault has also adopted that exact approved commit;
no additional repair transaction has been submitted.

The reserve-succession correction's causal check and 30-root normal/race
qualification are complete. The private-fixture correction passed all seven
selected roots normally and under race; its three timeout roots also completed
all three required normal confirmations on unchanged binary bytes. All three
e999 race confirmations have passed; the last closed at 19:05:50 UTC with all
four roots and actual outer/body/replay exits 0. Reuse those closed scopes.

The matching current upstream dependencies require Warp in source admission.
The reviewed `02ba4c7` integration preserves archived lock formats and extends
the current graph to 13 repositories and 16 live modules. Formatting and module
metadata are clean after the three-line Proxy reconciliation published at
`6204ae7df2a9868bbb3a7b61231917a36e4f5c9f`. The exact 24-root normal/race
integration passed at 18:40 and 18:41 UTC. Published candidate `e0a4542` has a
successful stamped bootstrap driver and native release-lock preview. Native
apply refused detached SN tracking metadata without changing the lock.
Corrected clone tracking exposed newer upstream dependency commits; all tested
dependency heads remain clean published ancestors. Astra's two-file correction
retains those exact commits as upstream advances, keeping SN's exact current-main
check and all before/after source checks. Its 12 affected checks passed normally
at 19:24:06 and under race at 19:25:14 UTC, with actual outer/native exits 0 and
unchanged 13-repository observations. Candidate `29be68f` is published. Its
stamped bootstrap driver built at 19:30:10 UTC, and native lock apply succeeded
at 19:32:03 UTC with exact reviewed SHA-256
`ddb22d0e1e525affac5b87cbba29cc70cb8d3e4afb9668507033d6fee907b51c`.
Publish the lock and start Terra's prepared full producer, aggregate and final
driver build concurrently. Preserve the final source snapshot throughout.
The older `cd036cee` executable and unapplied plan are
historical; fresh native planning must use the qualified current candidate and
approved replacement. Producer success can admit that repair while a clean
aggregate continues; both complete gates remain required for acceptance.
The [report and portable evidence](sim-testnet/FINAL-2.md) distinguish actual
passes, historical failures and remaining work.

Previous checkpoint, 2026-09-13 17:32 UTC: the soak remains stopped. The complete
`cd036cee` producer timed out in capture-private normal after 300.104 seconds.
Both superseded gates were stopped and joined: producer outer 143 at 17:19:25
with 23 passed phases, one failed and three interrupted; aggregate outer 143
at 17:19:27 with five passed and two interrupted, without an observed aggregate
product failure. The one-file fixture correction `f6cfd797` preserves all 1,000
miners and actual publication checks. Terra is preparing its exact seven-root
normal/race qualification and three sequential normal confirmations for the
three actual timeout roots. The separate e999 race confirmations continue on
unchanged source and binary; their first pass is complete.

The finalized 17:10 census at block 7,998,093 found 60.4855132935% reserve;
the approved 3,750-alpha repair now projects only 64.9713567471%. Native
read-only setup refused it with exit 1 at 17:14:23 UTC. The approved lifetime
cap remains 35,000 alpha. Correction `6311cb8` permits a provably unsubmitted,
insufficient terminal repair to be replaced through a new plan while preserving
the original history and all started or credited liabilities. Its focused
qualification is being launched independently. Neither correction is deployed,
the original journal is unchanged and no repair transaction has been submitted.

Previous checkpoint, 2026-09-13 16:25 UTC: clean publication `cd036cee` contains
the qualified scheduling correction and reviewed release lock. Its complete
producer and aggregate gates started at 15:57:41 and 16:01:22 UTC with private
test services; both are still running with no observed phase failure. Keep
their complete source and dependency snapshots unchanged. The final native
CLI built with exit 0 and unchanged observations of all 12 repositories;
two read-only setup reconstructions completed with exit 0 and agree on plan
`0x814d362c650dcdb86f1a57e4f266acd789e6be5703c9e65ad753576199bc3358`.
All actions and approved spending ceilings are unchanged. The plan remains
unapplied; the fleet and soak are stopped. The four interrupted roots'
required confirmations continue on their isolated source.

The 16:09:29 UTC complete reserve census at block 7,997,791 found
60.5324340452%; the approved 3,750-alpha repair projected 65.0285723985%.
Fresh native admission must still establish that the repair reaches 65%.
The 60% operating floor does not replace that target. If the approved amount
becomes insufficient, preserve the native refusal and do not exceed the
35,000-alpha lifetime cap. No new transfer has been submitted.

Previous checkpoint, 2026-09-13 15:45 UTC: the producer gate on published
`90f67b1` passed at 14:30:28 UTC with all 36 native phase joins exiting 0.
The aggregate's simulator race complement subsequently timed out after
90 minutes; its separate population race phase passed. The aggregate finished
with exit 1 at 15:40:56 UTC: 21 phases passed, one timed out, and its final source
check passed. All owned processes and private services are joined.
Correction `e99954a`, now pulled and pushed, separates the three full supplement publication roots
from the complement, preserving all 2,186 race roots and existing limits.
The ten affected guards now pass normally and under race; the compiled inventory
confirms all 2,186 roots. Terra continues three sequential race confirmations
for each of the four active timeout roots on unchanged isolated source. Prepare
the refreshed lock, final native CLI and corrected full gates concurrently with
those confirmations. The next full aggregate supplies broad
integration; an extra full development rerun is omitted.

The published `e99954a` bootstrap CLI applied the reviewed release lock with
exit 0 at 15:49:37 UTC. YAML SHA-256 is
`bd5e492077edc01acfa452d67ce1e437deec6d5e1add7ed8eab41dfd722b254f`;
only the protocol-script digest changed. The final CLI and corrected complete
gates follow publication of that lock and the updated evidence. Spending
limits and runtime code are unchanged; this update sent no chain transaction.

Setup began after the producer pass and adopted plan
`0xd4525b8da2da4f786f4990beb2285ac09b42e033c3c7fdd3c45473a7c9336507`
locally. After the aggregate failure, setup was interrupted and joined at
15:00:54 UTC with exit 1 and explicit context cancellation. The post-stop
journal contains no new-plan or reserve-repair entries. Preserve the adopted
plan and prior history for supported recovery; no additional reserve transfer,
renewal, continuation, adoption or fleet launch has occurred.
At the then-current complete census (15:10:42 UTC, block 7,997,497), the reserve was
60.5794990444%; the unapplied approved repair projected 65.0859768022% at that
snapshot. Actual finalized credit and a fresh target census remain required.
See the exact native results and public evidence locators in
[report 2](sim-testnet/FINAL-2.md).

Previous checkpoint, 2026-09-13 12:25 UTC: correction `b78b672` is published.
Its affected 443 capture roots and twelve coverage guards pass normally and
under race. The sole root active at the prior capture timeout, including all
four subtests, completed three fresh sequential race confirmations on identical
binary bytes and unchanged source. The eleven unchanged separately owned
capture roots retain their prior scoped qualification. Both corrected complete
gates still include the full selections.

The `0dcb5c8` producer remains a failed gate (21 passed phases, one failure,
three interrupted); its superseded aggregate was stopped with ten phases passed
and two interrupted, without an observed test failure. Their actual outer exits
are 143 following owned cleanup. All owners are joined and source is released.
Neither incomplete gate is reported as passing. See the portable raw outcomes
and qualification evidence in [report 2](sim-testnet/FINAL-2.md).

The clean, pushed `b78b672` native executable applied exactly the reviewed lock
with exit 0. YAML SHA-256 is
`776f6cf9d57d1c8427ac981f3cf2222ddc1441371c90cbded2789d8ea1299767`;
only the protocol-script digest changed. Final publication is followed by both
complete gates and the final stamped CLI build in parallel, then two fresh
matching setup plans. Producer success can admit the approved reserve repair
while a clean aggregate continues; both gates remain necessary for acceptance.
The historical 11:25 UTC census at block 7,996,371 found 60.7215670220% reserve
share and projected 65.2593333302% after the approved repair. The later
observation above supersedes that moving projection.

Earlier approval, 2026-09-13: the user explicitly approved raising the
lifetime cap from 31,250 to 35,000 alpha for one additional 3,750-alpha reserve
repair. The vault change is committed and pushed at
`d4ea0cbdf49630d8e1afc3e2184858cb58940fd3`. This approval remains valid but
the pending amount is insufficient at the later snapshot above. It does not
authorize exceeding 35,000 alpha lifetime. The per-repair maximum remains
6,000 alpha.

Two actual read-only setup builds on SN `3af4251` completed successfully at
01:48:04 and 01:58:00 UTC, both with plan hash
`0x06116ddc5cdc6945c7d96c8920f2b4cfbaa9a6f04bfd211503cd51803286182a`.
The reviewed revision adds the exact 3,750-alpha repair and authenticated
zero-spend carry of the existing coordinator repair. It retains all prior
positive-alpha actions. Neither plan was applied; the original plan, journal
and signed transaction bytes remain retained. New production corrections
require a refreshed release lock, stamped CLI and fresh bound plans before
application. [Read-only plan review](../temp/sn-approved-alpha-repair-review-20260913/setup-v5-review.json).

The original full producer gate ended with exit 1 at 02:50:47 UTC; the
aggregate ended with exit 1 at 03:23:25 UTC. Their source snapshots remained
clean. Both failed private-service startup, and their actual completed test
failures are being corrected. Jobs joined with exit 143 during cleanup are
recorded as interrupted, not completed test verdicts. [Producer receipt](../temp/sn-final-execution-20260912/runtime/producer-gate/capture/RESULT.json),
[aggregate receipt](../temp/sn-final-execution-20260912/aggregate-gate-prepared-20260912T2254Z/capture/RESULT.json).

The previously pending focused qualifications are now complete on their
recorded immutable sources. The Go 1.26 qualification launcher correction passed its four
regression roots normally and under race. Corrected CRV4 and server artifact
test binaries subsequently exited 0 in both modes, but their captures failed
because expected-outcome files omitted legitimate subtests. Existing offline
replay has now checked both retained streams against corrected exact declarations;
the original failed captures remain unchanged. The isolated PostgreSQL control
reproduced `Permission denied` on the copied mode-0700 initialization directory.
After restoring public fixture permissions, the actual PostgreSQL 18, Redis
and fixture preflight passed with successful owned cleanup at 03:43:02 UTC.
[Corrected service preflight exit](../temp/sn-private-services-qualification-20260913/runtime/preflight-0755/capture/outer.exit).
Service18, monitor14 and cache/provisional23 integration checks passed normally
and under race on SN `ebe70a3` and server `e2358826`. Their previously failed
normal roots have three fresh passing confirmations on the recorded immutable
binaries. [Sealed scoped qualification](../temp/sn-private-services-qualification-20260913/runtime/RESULT-service-monitor-cache-corrected.json)
(`sha256:c98dbe1e98943cf2eb7087e362b4c0b17e08ec08006007d87c21ef2b39d493cf`).
Connect's actual plain-WebSocket resolver bypass is corrected at `3d29e1f`.
Its 15-root integration passed normally and under race; all eight previously
failed roots completed three sequential passes in each mode on the same
recorded binaries. The remaining 18 simulator normal confirmations, both
CRV4 roots in both modes, three private-capture normal confirmations and the
seven-root private-capture race integration also passed. Earlier simulator,
validator, stabi, service, monitor and cache results retain their original
source scope. See the [current qualification evidence](sim-testnet/FINAL.md).

The integrated source includes server `bbfe4296`, Connect `3d29e1f`, SDK
`169d4c2c`, operator-proxy `714f10f0`, proxy `c11c7eb4` and xops `ec84346`.
All 15 live modules passed dependency validation. The xops update leaves all
15 Subtensor infrastructure lock inputs unchanged; the normal infrastructure
gate still covers its affected inputs. The new release-lock YAML hash is
`sha256:ddcd0ec9f44f11c86c22b6b9ff73a31a09c0d0b910e07a1d8b966a8c7af40923`.
The native stamped `5a79b62` renderer applied exactly the reviewed bytes with
exit 0. Runtime, EVM, interface and infrastructure lock fields are unchanged.
The final stamped CLI build and both full gates follow this publication in
parallel, using the existing physical workspace and separate private services.
Completed historical qualification remains reusable within its recorded scope;
the complete current candidate still needs both full-gate results.

The fleet and soak remain stopped. There has been no new reserve transfer,
renewal, relay continuation or live campaign during this preparation. The
critical path is to close the actual failures, publish the composed source and
lock, obtain launch admission, apply the approved reserve repair, then perform
renewal, relay continuation and retained-history adoption before the full run.
Producer success can admit the live campaign while a clean aggregate is still
running, as specified by the complete plan; any actual aggregate failure stops
new mutations. Both full gates are required for final acceptance.

Historical checkpoint, 2026-09-12 23:13 UTC: the full fleet is stopped. Both
operator APIs and temporary payout-recovery proxies were also stopped after
all 16 funded epoch309 claims finalized, paying 103.320655346 alpha with eight
alpha-rao of accounted rounding residue. The [final peer-review report](sim-testnet/FINAL.md)
contains the receipts and pinned state; the historical 15/17 scenario remains
`final_acceptance=false`.

Actual read-only doctor on local source `02dfe50` passed 63 of 64 checks. Its
sole failure was systemd's degraded state from 43 stopped simulator units.
Their metadata and all 621 available journal entries were preserved before
resetting only those historical failure flags; the manager now reports running,
without restarting any process. Actual read-only setup first refused the full
retained repair audit budget's `observed_at` field. Its corrected reader preserves
the signed projection and complete document hash. The next attempt exposed the
missing recovery case for the original finalized repair transactions. Candidate
`9c444e4` passed that gate, then refused the original companion's predecessor
CREATE while binding the later repaired coordinator. Source `cf3ccd6` corrected
the combined carry path and reached reserve-majority planning. Its fourth actual
attempt stopped at 21:48:11 UTC: the target needs another 2,855.249565922 alpha,
but signed transfers already consume the 31,250-alpha lifetime allowance.
The prior 5,999.806443325-alpha repair is already credited. The configured
6,000-alpha allowance applies per repair and does not replenish the lifetime
budget. User approval is pending to raise that lifetime cap to 35,000 alpha for
one additional 3,750-alpha tranche. At pinned native block 7,992,355, that would
raise the observed 61.449% reserve share to at least 66.113%; fresh planning
must recheck stake and transferable source capacity. No revised plan or
transaction was emitted; the original plan and journal remain byte-identical.
[Latest actual admission error](../temp/sn-full-finalization-20260912/readonly-admission-20260912/setup-v4.stderr).

The final execution workspace and pinned Solidity libraries are prepared at
`temp/sn-final-execution-20260912/workspace`, with real Git directories for
authentic executable VCS stamping. Every tracked source file was compared to
the prior workspace for byte and mode equivalence. The existing unlocked vault
is reused. No key or plaintext secret was copied.
Candidate `9c444e4` is committed and pushed on its review branch, with a reviewed
source lock and an independently built read-only CLI. Five repair-carry and four
relay-continuation roots, the offline authority root and six archive roots each
completed three fresh normal passes and a race pass. Public checkpoint, native
capture and CRV4 checkpoint checks also passed in both modes.
[Completed affected qualification](../temp/sn-final-release-20260912/runtime-9c444e4-20260912T2118Z/RESULT.md).
Successor `eca9e19` passed native coverage, companion carry, adjacent evidence,
archive/payout and native consumers in both modes. Its reward-reader cancellation
failure is corrected: all three reward roots passed three fresh normal runs and
one race run on `94cb3dd`. Simulator and validator V2 observation also pass both
modes. Automatic native readiness and preparation share one absolute deadline;
the full remaining-work bound is 7,570 blocks, preserving both acceptance phases.
The retained-ledger capacity checks pass, including the exact 8,065-block limit
and refusal at 8,066. The 43-root matrix's outdated horizon fixture was its sole
normal failure, with that three-root race partition initially unrun.
[Exact completed matrix and retained failure](../temp/sn-final-release-20260912/runtime-94cb3dd-20260912T221700Z/RESULT-94-GO-MATRIX.md).

On `da27b85`, the corrected horizon root has three fresh normal passes, and its
original three-root partition passes under race. The exact final semantic census
contains 310 roots. Two of 27 gate guards failed in both modes: a stale direct-call
assumption across the real startup delegation chain and three renewal consumers
without the required parallel marker. Astra corrected those three test files;
on `e8bceaaa62e6d1c3ad2f5a30535f7a7a3806661d`, Terra completed three fresh normal
and three fresh race passes of both failed guards. The three affected renewal
consumers also pass together normally and under race. Production source and
the release lock remain unchanged. These confirmations are complete; historical
streaks and horizon checks will not restart.
[Exact partial qualification and stamped CLI](../temp/sn-final-execution-20260912/runtime-da27b85-20260912T2245Z/RESULT-PARTIAL.md).
[Completed guard corrections and adjacent integration](../temp/sn-final-execution-20260912/runtime-e8bceaaa-20260912T230158Z/RESULT-E8-GUARD-CORRECTION-CORRECTED.md).

Both full-gate launch commands are prepared with separate private mutable
resources. The twelve-repository snapshot includes the vault budget. Therefore
the pending spending decision and any approved vault edit, commit and push must
precede the final source freeze and concurrent producer/aggregate launch. A
mid-gate budget change would invalidate the final snapshot. Focused correction
and qualification are complete; final publication and CLI preparation proceed
while approval is pending. After both gates,
proceed through fresh setup-plan admission, renewed fleet authorizations, bounded
relay continuation, retained-history adoption and the actual full campaign.

Earlier component checkpoints, superseded by the completed results above:

Terra passed the strict V2 history-adoption core and corrected EMA bridge
normally and under race, including three fresh confirmations in each failed
mode. Those fixes are integrated. The ten focused renewal roots also passed
normally and under race on source `006c0c0`, including three fresh confirmations
in each failed mode for the two repaired roots. [Renewal validation](../temp/sn-renewal-006c-validation-20260912T1820Z/runtime/renewal-006c-20260912T1828Z/RESULT.md).
The combined strict CLI, owned-LAN routing, repair carry, plan revision and
renewed lifecycle code is assembled. On source `2984c9b`, all 71 selected
validator roots pass normally and under race; the corrected cadence root also
has three fresh passes per failed mode. The 98-root simulator matrix exposed
renewal-evidence, lifecycle and history-adoption fixture failures. Its race
process exhausted the shared ten-minute budget; the terminal lifecycle root
had run for 53 seconds. Preserve that timeout and qualify the complete selected
population in bounded partitions with unchanged deadlines. A mode-775 TMPDIR
also caused one invalid launcher refusal, which is not a product diagnosis.
[Combined validation and original failures](../temp/sn-strict-composed-fixes-validation-20260912/runtime/preflight-20260912T191953Z/RESULT.md).

Contract generation is complete at `5c4c546`. The revised full Forge build,
18 binding-policy tests, generator consistency checks, and generator/stabi
tests normally and under race pass. The coordinator creation and runtime bytes
exactly match the retained deployed repair; runtime size is 24,564 bytes.
The original oversized test harness and first overflow-fixture failure remain
retained. [Exact artifact comparison](../temp/sn-contract-generation-20260912T1841Z/runtime/generation-20260912T1845Z/full-build-revised-coordinator-compare.stdout).
The generated payload is integrated. Both corrected renewal-evidence roots
have three fresh normal and three fresh race passes on `73ad855`.
[Renewal evidence confirmations](../temp/sn-semantic-renewal-generation-validation-20260912/runtime/RESULT.md).
Composed `bcb1ce0` passes the selected native/EVM checkpoint, capture, history-read
and startup populations in both modes. Its three simulator fixture failures and
two archive fixture failures are preserved; their corrections and the frozen
public publication/consumer/recorder code are composed in successor `e06f055`
for Terra qualification. [Composed results](../temp/sn-finalization-integration-20260912/runtime/RESULT.md).
The later native interval and reward/application coverage results are above.
Neither full gate nor either complete live acceptance phase has passed.
The unrelated calibration prerequisites were removed from SN qualification;
Terra passed all 12 affected guard roots normally and under race. The existing
runtime dependency census and full SN gates remain required. [Scope validation](../temp/sn-scope-validation-20260912T1830Z/runtime/scope-guard-20260912T1831Z/RESULT.md).

Read-only copies of all four retained source ledgers were inspected without
changing their file metadata. The largest source contains 134,673 records,
16,958 trails and 698,568,804 raw record bytes, within the original limits of
655,360 records, 81,920 trails and 10 GiB. This observation does not replay record
signatures or certify the remaining campaign. All four activation contexts bind
block 7,975,563; the existing 10,080-block relay allowance formula therefore ends
at 7,985,643, before the observed finalized block 7,991,348. No `evidence.relay.*`
entry exists in the retained journal. The retained locators contain 182 pending
members; the corrected 7,570-block full-work forecast requires another 200.
Locator counts still require complete signature and immutable-slot authentication. Astra has
frozen an explicit plan-bound continuation at `a52758b` with up to 512 relay slots at
50 gwei per 1,000,000-gas action, within the existing 25.6-TAO relay reserve.
It must preserve original activation and liabilities, bind the actual pending
census and finite remaining run window, and retain every original source-storage
and lifetime monetary limit. The continuation and capacity code has passed its
affected checks above; no continuation plan has been applied. Choose its fixed
end only after gates and renewal so preparation does not consume the remaining
source-capacity margin.

## Historical shortened execution — 2026-09-11 17:33 UTC

The user directed us to stop preparation tests, run the actual simulation on the
real testnet, and fix issues found by that run. The former producer and aggregate
gate prerequisites, repeated failed-test confirmation sequences, duplicate plan
comparisons, separate pre-launch smoke rehearsals, and repeated audit work are
removed from the launch path. Do not start another preparation test cycle.
Previously completed evidence remains reusable within its recorded scope;
waived or unrun checks must never be described as passing. Full preparation
gates are no longer conditions of completing this testnet exercise.

## Execute now

1. Reuse the existing attempt-4 plan and completed receipts. The carry repair,
   database migration and configuration rendering are complete. Do not repeat
   them or regenerate the plan for a provisional driver correction.
2. Keep the existing `epoch` scenario running with explicit --provisional-resume,
   using the retained configuration and corrected driver. The first timed run
   started at 15:56:51 UTC. Current ownership and the latest run ID are recorded
   in the external finalization directory's CURRENT.json. This scenario observes
   an epoch transition on the working fleet; it does not
   certify the full release or production acceptance window. Fix failures
   observed by this run and report its actual outcome.
   The full release attempt completed all 16 lifecycle preparation actions,
   then stopped before acceptance because its planned pruning target was UID 7
   while the computed and recorded target was UID 1. Preserve that attempt and
   failure; defer the pruning/fault campaign while the epoch scenario runs.
   When an actual runtime correction requires a new worker image, preserve the
   interrupted result and completed state, join its owner and fleet, deploy the
   corrected image to both simulator and dedicated Connect, and resume the
   existing epoch scenario. Never describe an interrupted interval as passing.
   Keep native custody,
   spending limits, journal serialization, process ownership and live health.
   Process log classifications are observations in this mode: preserve every
   finding and its original classification without stopping the fleet for it.
   Provisional startup may use ready providers while other live swarms catch up;
   retain actual health values. A new controller may adopt the existing fleet
   without replacing its binary or manifest, or restarting its processes.
   Bound actual process readiness to 30 seconds; retain exact generation and
   process identities, live PIDs, and non-provider health probes.
   Provisional scenario startup uses the same authenticated live-fleet
   preparation as resume to omit the full doctor when no spend is pending.
   It reuses the completed topology handoff through the existing process log
   gate; it must not manufacture completion or new verified setup receipts.
   The nested provisional campaign executor reuses its exact parent's already
   authenticated deployment payloads. Omit the duplicate historical deployment
   preflight while retaining current scenario actions and their postconditions.
   Provisional relay startup and preparation require only the next block to
   fit the original paid horizon; log the full requested forecast as waived.
   Skip the duplicate pending-public-census preview, while authenticating each
   actual publication before its relay admission and send. Keep the original
   activation/native anchors, 256-slot ceiling, debits and all spending caps.
   Full phase coverage is not established by this provisional admission.
   Provisional launch omits precompile conformance and the pre-launch
   governance drill. Preserve their actual failed/unrun evidence and report
   both prerequisites as waived. Do not repeat probe funding or commitments
   to enter the traffic run; keep actual takeover binding actions and their
   spending/transaction postconditions. Full conformance remains unproven.
   The owned LAN RPC at 192.168.1.162:9944 has been verified against testnet
   chain 945 and the original native genesis. Native and EVM traffic now use
   that route through an invocation-only provisional transport override and
   the existing workload fault proxies, with all RPC rate limits removed.
   Record the actual endpoints and zero RPC rate limits. Retain the approved
   plan, signed inputs, receipts and spending limits; omit independent public
   RPC comparison in this mode and keep final_acceptance=false.
   Fresh signed proof coverage is an observation during the run, not a
   provisional campaign startup prerequisite. Record
   fresh_proof_startup_waived=true, the original proof baseline, and observed
   counts with observed_proof_counts_verified=false. Do not fill verified proof
   counts or describe old coverage as a fresh-proof pass. Continue actual proof
   validation during scenario observations and completion reporting.
   Provisional adoption also waives strict public deployment evidence
   publication, which revalidates superseded historical manifests. Record
   deployment_evidence_publication_waived=true and final_acceptance=false in the
   provisional handoff. Preserve existing public files and publication errors;
   do not create a substitute published manifest or describe publication as
   passing. Keep approved topology actions, journal entries and spending caps.
   The four exact private activation contexts may grant testnet staging directly under
   the explicit retained-context allowance. Upload signatures, session/object
   binding, finite intent expiry and quotas remain enforced. Historical and
   current-chain admission checks are waived/unrun, never reported as passing.
3. Observe real transactions, provider traffic, validator proofs and accepted
   epochs. Fix concrete runtime failures and resume supported completed work.
   Keep original errors and actual completion markers; never invent a pass.
4. Report the achieved coverage, transactions, epochs, failures and remaining
   gaps from the actual run. Public replay and release certification work must
   not delay launch; describe any omitted validation honestly.

## Limits and current state

Use public Bittensor testnet, netuid 521, with the existing 1,000 providers,
20 swarms, two operators and two validators. Retain attempt-4, its keys,
used activations, signed setup, journal, approvals and deployed contracts.
Caps remain: 6,000 alpha reserve-repair allowance, 31,250 alpha lifetime,
180 EVM within 200 total TAO, 262 registrations, and no new subnets.
Do not reset state or repeat funding/registration transactions.

The provisional fleet has run real testnet work. All twenty swarms passed live
startup on September 11 before a process-log gate stopped that generation for
onboarding-metric SQL warnings. The provisional driver now records those
classifications without making them launch conditions. The SQL correction is
prepared separately and must not delay launch. No preparation tests are running.
This mode records final_acceptance=false; do not claim strict certification.

Deployed in CLI25: after the hash-pinned testnet handoff validates,
enable existing closed-native-input deferral in memory. Preserve signed subnet
1391 inputs from settlement 290; report deferral without native submission and
continue at the next native epoch. This does not establish successful trail
proofs.

Also deployed in CLI25: validated provisional shared boundary preparation receives a
120-second canonical-read budget within its existing producer deadline (240
seconds in this run). Trail/packet deadlines and ordinary reads remain 30
seconds. The owned LAN route now has no RPC request quota; canonical checks
remain in place. CLI27 also corrected the stale dedicated Connect binary,
which had disabled subnet egress attribution. This produced 660 additional
proof rows across all four validator/operator paths before the epoch scenario.
That scenario exposed a settlement rollover failure. CLI29 excluded local
signature verification and scratch writes from the HTTP I/O deadline; both
settlement 292 closures completed. Public evidence replay then encountered a
truncated stream; its interaction with server deadlines is the inferred cause.
CLI32 gives that bounded route a ten-minute
request/write allowance and records abort causes. It also uses existing
same-nonce cancellation for an expired close intent, and schedules ST sync and
close retries every five seconds to reach the five-block close window. The
owned-LAN deployment restores the original taskworker count 8 / batch size 4.
Both operators finalized their epoch293 closes before the cutoff; captured
emission and payout remained zero. Both validators subsequently published
epoch293 evidence. CLI33 normalized validated in-memory contract address text,
allowing native1394 measurements to seal without changing retained files.
CLI34 added a bounded observation of the actual retained V2 intent records;
it leaves strict authenticated counters and acceptance claims unchanged.
The next actual native submission failure exposed GSRPC's handling of JSON
null storage results. Decode those results as nullable strings so an absent
slot remains distinguishable from malformed responses. Deploy this correction
through the same retained-state resume procedure; keep all original failures.

The bounded consumer run completed once and joined successfully. All eight
escrow contracts settled, producing 1,052,426 and 1,052,424 provider usage bytes
for the two operators from existing credits. No new account, credit or chain
funding was created. These byte sweeps establish actual usage, not a chain
payout; their fiat revenue is zero. Do not repeat this traffic as preparation.
Observe epoch294 provider eligibility and a nonempty payout commitment, then
the existing epoch295 deposit and subsequent pool scoring. A nonempty usage
root can be committed even when captured emission is zero. The sealed294
measurements correctly gave zero pool weight because source293 had no root;
preserve those measurements. Native application, positive capture and payout
remain actual-run outcomes to establish. Record their results in CURRENT.json
and the report while the fleet continues working.

Historical evidence remains in [FINALIZE-COMPLETE.md](FINALIZE-COMPLETE.md),
[FINAL.md](FINAL.md), and the external finalization directory. The native
campaign's full epoch windows remain real elapsed time; there is no renewed
14-hour completion promise before an actual campaign start.


## Active recovered release interval — 2026-09-17 18:50 UTC

R20 reached the real release campaign and failed naturally when rolling fault
`release-rolling-30` targeted `validator-1` after that manifest process had
stopped. Its signed failed attempt remains immutable at run
`20260916T195955.196642218Z-release-1.0`. The attempt hash is
`9858b897eb4217c8f17e5dc0ad351d3dadfadb949c96e16637b7378d299d2ad7`;
the failed result hash is
`3f626a32b6ea64bfbb15fe8cea9ea4318e18ccd8c191ad338c99578a3cf8ce89`.
The signed observation boundary is 16,415,645 bytes with content hash
`sha256:00fa367cda71d5f7b20fbdf3fd0918bace1683b830f01cccdf24a240450ed123`.
Two valid observations appended during the failure path remain preserved as an
opaque suffix, making the complete 17,227,515-byte file hash
`sha256:778805c56f8ad9c3f0c2f57e0d3c3b8ff471b6713fad868e6fc8620592bddcf3`.

R24 authenticates runtime state only through that signed boundary after proving
the predecessor is an invalidated provisional failure. It separately binds the
complete append-only file, including the suffix, and never truncates or rewrites
it. Ten focused recovery tests and eleven adjacent recovery/topology tests pass
normally and under race. The detached-startup retention suites also pass normally
and under race. The reproducible R24 binary is
`01de1e797aa12edc3bb57169608dc83f9d08376538c0032e97b0b21670501161`;
its qualification is under
`/mnt/data/sn-testnet/qualification/runtime464-r24-recovery-resilience-20260917T1838Z`.

The R23 supervisor generation remains unchanged: PID 2,849,990, start ticks
195,540,640, canonical manifest
`0xc137ba3f1c57b40e5810891ec0f63930780c500e5095a5468403de55604e233a`,
33/33 healthy processes and zero restarts. R24 uses only the owned LAN RPC
`192.168.1.162:9944`. Its signed recovery record was created at 18:51 UTC with
new run ID `20260917T185114.663596192Z-release-1.0` and file hash
`8c52af26f7b819edec9333d742346c78050dfa4adc1463c07d0535d177d0060c`.
It binds the exact R20 attempt, start marker, result, full observation log,
process-log evidence and unchanged approved plan. No setup, topology, transaction
or spend was replayed before this checkpoint. The launch and recovery checkpoint
are sealed under
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r24/release-candidate-r24`.

The controller passed receipt and topology admission. Its first evidence-relay
forecast was at finalized block 8,027,402; the expensive retained-history pass
closed at block 8,027,459. The following forecasts were bounded relay recovery,
not process restarts. The controller then reached live scenario preparation and
terminated at 19:11:23 UTC because gas estimation for `precompile.seed` reverted.
The preceding `precompile.read-battery` action passed at finalized block
8,027,499. The seed action wrote only intent and failure journal rows: no EVM
transaction was broadcast and no value or gas was spent. R24 did not create a
campaign-start marker or signed acceptance boundary. Its terminal result remains
provisional with `final_acceptance=false`.

The exact terminal capture is sealed at
`/mnt/data/sn-testnet/qualification/native-recovery-20260917-r24/release-candidate-r24/TERMINAL.json`
(`sha256:a568a9f450adca942b32a04f57c4da41fbfda7a4e6f8d299f51ff9e7241a5193`),
with a verified checksum manifest
`TERMINAL-SHA256SUMS`
(`sha256:955fdf1ba21f62fac88b4165f4d3425269678e5fddd9f282f4ee0adec5d15ba8`).
The seal contains immutable copies of the exact terminal journal and plan, so
later append-only recovery work cannot invalidate R24's checksum manifest.
The result hash is
`dda759c72e3c5de22d060c4f510bc7d5bb9e8f12b98f3cc8a35867d83595f875`;
the journal advanced only from sequence 25,160 through 25,163 and now hashes to
`a355a5dfc6c6202056b1435e3199181c92700f08426533b20da5e014e8c76bbe`.
Before/after topology snapshots contain the same 33 process identities and
generation, with zero restarts. The R23 supervisor remains live and unchanged.

Resilience work continues independently of the stopped R24 controller. A
detached provisional startup timeout already retains an exactly authenticated
live supervisor generation so its children can recover independently. The next
candidate adds an append-only chain of numbered recovery attempts; its initial
18 focused and 39 adjacent tests pass normally and under race. R24 exposed an
adjacent requirement: a terminal recovery attempt that fails before acceptance
also needs a signed next generation, without inventing a campaign-start marker
or replaying setup. That extension and the seed failure are being repaired as
one affected scope. Relay startup also needs an authenticated exact-plan
checkpoint: R24 reread 684 historical relay actions and 694 published requests,
then replayed retained epochs without a durable `through` marker. The checkpoint
must validate only append-only suffixes during provisional recovery and must be
ignored for strict final acceptance. None of these source changes alters the
retained supervisor or rewrites R20/R24 evidence.


## Future recovered-read accounting — R44/R45 review

R44's first interval epoch cannot meet strict acceptance. Both validators
reported `release steering advanced from incomplete epoch 1662 to 1663` at the
first interval boundary (settlement epoch 616). The exact stderr source offsets
are 300896970 for validator-1 and 243603097 for validator-2 in
`sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/processes/`. R44 continues through
terminal capture by user direction; the continuity failures remain blocking.
The preserved offset-bounded error blocks and individually indexed stdout
events are recorded in
`/mnt/data/sn-testnet/evidence/r44-compact-replay-retry-20260925/boundary-manifest.json`.

These continuity failures are distinct from a read interruption that later
recovers. Validator-1 retained a finalized-scheduler WebSocket close before 27
no-submission weight rejections for one positive weight under limit 32768.
Validator-2 retained compact settlement/operator replay deadlines, a runtime
version read deadline, a finalized-scheduler WebSocket close, a refused artifact
Get, and later replica Post/native-operator deadlines. Longer read and steering
budgets do not by themselves establish that all native epoch work can finish.
R45 needs the independent scheduler and infeasible-weight dispositions reviewed
and a complete mature-history replay/native-completion qualification under the
fault schedule, while preserving immutable evidence and strict continuity.

R44 keeps its recorded process-log findings, v12 terminal policy, raw line
hashes, counters, and signed acceptance boundary unchanged. A later generic
progress event does not discharge a `release-steering-attempt-failure`.
Current findings aggregate by process/stream/class and acceptance scope; they
do not identify each failed read by native epoch and immutable artifact. The
existing successful steering return also carries no independently verifiable
recovery identity. Consequently this repair does not add a recovered-read
disposition or relax terminal acceptance.

For future runs, a transport-only read failure may become nonblocking only
under a separately versioned recovery protocol with all of these properties:

- Retain every original event, count, source offset/hash, process identity and
  acceptance scope. Native-write, continuity, integrity, custody and independent
  close failures remain terminal-blocking, including mixed error trees.
- Emit a typed read-interruption identity covering the native epoch, exact
  immutable artifact/cut, operator and replay purpose. Preserve the distinction
  between a canceled service, an expired read attempt and a permanent refusal.
- Bind recovery to complete authenticated replay of those same inputs, after
  all dependent projections and closes succeed. Where the interval requires a
  native result, additionally verify the exact finalized/applied intent and
  receipt; a log string or progress in another epoch is insufficient.
- Reconcile every occurrence individually. One successful read must not clear
  another operator, artifact, epoch, process, restart, pending native write or
  later interruption. Unresolved or exhausted retries remain blocking.
- Version the classifier for new runs while preserving earlier findings and
  signed acceptance-scope hashes. Do not reinterpret an old signed boundary or
  drop raw errors to obtain a clean report.
- Add deterministic tests for complete recovery plus wrong-process, wrong-epoch,
  wrong-artifact, partial-census, mixed-close, canceled-parent, restart, missing
  native finality and unrelated-success counterexamples. Race qualification
  must exercise the real event/replay/terminal-accounting integration.

The compact replay repair and ordinary artifact/server-key Get retries are
future-build work in isolated worktrees. Public artifact retries happen before
one final HTTP exchange is signed; once that observation is retained, later
replay consumes its exact bytes and cannot replace a historical negative with
another live request. No live R44 process, configuration or evidence is changed
by these fixes.

## R45 scoped systemd doctor qualification — 2026-09-25

The external-copy doctor for clean SN `6ec90bf2` plus Connect `c98eb715`
completed 66 checks; its only hard failure was `supervisor/systemd-user`.
The user manager was degraded by ten historical release units while the exact
owned fleet service was loaded, active/running, and startable. Original argv,
failed-unit names, report and live-prefix checks remain under
`/mnt/data/sn-testnet/qualification/r45-connect-successor-20260925/migration-review/external-doctor/`.
No failed latch, live state, service or transaction was changed.

The corrected doctor retains the complete failed-unit inventory and explicitly
observes the exact deployment service's identity, load state and startability.
An unrelated historical failure no longer rejects current launch capability.
An unavailable manager, failed read, malformed or aliased unit observation,
masked unit, or unstartable loaded service remains a hard failure. A startable
owned failed service remains eligible for the existing recovery route; the
separate live service ownership, terminal-state and child-generation readiness
checks are unchanged. Adjacent review found no other global manager-health
gate. The tests permit only the three exact read-only status queries.

Focused and adjacent simulator tests passed normally in 15.136s and under
race detection in 104.124s using the pinned integration module. Restoring the
old doctor function made four intended scoped-unit tests fail; overlay evidence
is `/mnt/data/sn-testnet/qualification/r45-doctor-pool-causal-20260925/doctor.json`.
This qualifies the source fix. A newly stamped composed image and another
external-copy doctor are still required; it does not establish final acceptance
or authenticate the whole retained startup. R44 continues through its terminal
capture with all strict findings preserved.
