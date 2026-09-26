# Sim-testnet finalization report 2

**R46 completed five measured epochs; sealed provisional result failed on
2026-09-26.** The owner recorded **182 assertions, 86 failed**, and **182
open anomalies**. `final_acceptance=false`; this report does not claim a
qualified release or production soak. The independent sealed terminal
diagnostic completed with **19 passes, 9 failures, 9 unavailable checks, one
finding and one documented exception**; it also reports
`final_acceptance=false`. [Exact owner result](peerreview/evidence/FINAL-2-R46-continuation-20260925/owner-result.json)
(SHA-256 `e979cafe568ae1107a32adcf03b086447fce6db9485a968271cb8cdbd38878c9`).
The R46 owner ran as `urnetwork-sim-release-r46.service` from clean
source revision `ad5c05ec` with the unchanged approved plan, 2,048 funded
slots, retained fleet supervisor, and LAN RPC `192.168.1.162:9944`.
Its in-service preflight authenticated R45's sealed failure and signed
invalidation, the retained journal and supervisor identities, pinned input
hashes, and finalized LAN block 8,084,535. The owner subsequently
authenticated all 45 prior campaign generations and signed recovery
generation 46 (run `20260925T172403.199659160Z-release-1.0`), binding
the exact failed R45 result and invalidated envelope. The owner completed
preparation and signed a fresh R46 acceptance boundary at 17:47:11 UTC:
baseline epoch **630** at finalized block **8,084,658**, five measured
epochs **631–635**, start block **8,084,674**, end block **8,086,174**, and
terminal block **8,086,324**. The LAN node finalized block 8,084,682 after
the signed start. The owner completed its first measured observation at
17:47:49 UTC on finalized block **8,084,680** (hash
`0x914e4fd82d14e5f17dd19a4f3eb0455495f88cda1c01487caecef60e80087e4f`),
six blocks after start. It found 808 valid fleet bindings and policy rate
readiness. Its observation hash is
`0x8adaccc861dba35a59e47303a9b5f9f15af51e24f23f1ebeac4699b438a81d81`.
This proves measured observation began; complete epochs and terminal
acceptance are still separate requirements.
[Signed start envelope](peerreview/evidence/FINAL-2-R46-continuation-20260925/generation46-signed-start.evidence.json)
(SHA-256 `4b1ebd67df7c9422d61e998f425a1d5694efaa4dd2dc45b583e9735d1848a4fd`),
[first measured observation](peerreview/evidence/FINAL-2-R46-continuation-20260925/first-measured-observation.json)
(SHA-256 `99cc4fe919e076cde81993fe875d09b240b73e0ef43bb5bb4a48d01dabd03873`).
The displayed contract head hash is the **EVM block hash**: an independent
LAN `eth_getBlockByNumber` read at 8,084,680 returned it exactly. The
Substrate `chain_getBlockHash` at the same height is a different hash by
design; the two should not be compared as if they were the same namespace.
[Exact RPC responses](peerreview/evidence/FINAL-2-R46-continuation-20260925/first-measured-chain-hashes.receipt.json)
(SHA-256 `f456e08e4719854328eaa254dc37e68ef9687ee5bfa4c82a1b6760a042169261`).
One later snapshot at finalized block **8,084,710** marked rate readiness
false because both local operator stats and proofs GETs exceeded their
30-second single-attempt deadline. The owner stayed active. Its next durable
snapshot at block **8,084,740** marked rate readiness true again from a
complete current-policy source epoch, with valid fleet bindings. This is an
intermittent observation error, retained for final review rather than erased
by the recovery. [Timeout](peerreview/evidence/FINAL-2-R46-continuation-20260925/local-get-timeout-observation.json)
(SHA-256 `68482b1d4cb2ee51f01ae19b87430a1f1635b895e3a1f5912d20383cac2af93e`),
[recovery](peerreview/evidence/FINAL-2-R46-continuation-20260925/local-get-recovered-observation.json)
(SHA-256 `6009dcaf3a37e0865e8a373506faf089254100e5132d379985dcea81458d4150`).
The [observation-pair receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/operator-read-recovery.receipt.json)
(SHA-256 `3589c3c75fe4b421ed0d0d74185dba77dfc8a965b6ea39305f0c07f9755535f2`)
retains their exact JSONL offsets and hashes.
The strict anomaly checker walks retained observations, so recovery does not
remove that timeout row. Its `operator-error` is expected to remain an open
final anomaly unless the terminal checker establishes an applicable expected
fault; no such fault target is present in this row. This is a prospective
strict-gate finding, not a reason to stop the continuing partial run.
A second 30-second local GET timeout occurred for operator 2 in an observation
at block **8,084,769**. That row does include `operator-2-api` among expected
fault targets. A read-only latency review correlated the endpoint failures
with scheduled PostgreSQL restarts and found healthy API processes and low
single-digit successful route times outside faults; it does not prove the
exact request's cause. The second row and its fault context remain available
to the terminal checker. [Exact observation](peerreview/evidence/FINAL-2-R46-continuation-20260925/second-local-get-timeout-observation.json)
(SHA-256 `942f450a894b2f48a7dee768ba66bafc2e02c1dda4d947146d13d200c5c24618`),
[latency triage](peerreview/evidence/FINAL-2-R46-continuation-20260925/operator-latency-triage.receipt.json)
(SHA-256 `6d4522af1f38460bf5ac0b69836776b2baacda2600d5caa37609aecb5458367d`).
At 18:03 UTC, the live owner also logged
`public_census_audit_passed=true`; this closes that deferred preparation
audit while strict terminal acceptance remains open. The
[bound owner-journal receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/public-census-passed.receipt.json)
(SHA-256 `2af3a976a778093f1e711a01102fefa43b4d0bf1f61f2ac357fb0513148315d7`)
retains the exact cursor and owner identity.
At 18:08 UTC the owner retained two R46-scoped validator-2 steering classes
as provisional process findings and explicitly continued observing. The raw
lines show native epoch 1674 attempt 1 timed out waiting for headers from
operator 2's local `/sn/attempt-artifact`; attempt 2 reported a compact-head
EMA epoch jump; steering then advanced from incomplete 1674 to 1675. These
are real strict-gate findings, not part of the companion-filter exception.
The auxiliary read-only monitor initially selected the wrong scope field and
underreported them; its corrected v2 summary and original raw owner gate are
preserved in the [finding receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/process-findings-180814.receipt.json)
(SHA-256 `f632ef67a4c21cbd423fd7684b1cbd82a71f9b2916636a353a3ecec94da66308`)
and [gate copy](peerreview/evidence/FINAL-2-R46-continuation-20260925/process-findings-180814.gate.json)
(SHA-256 `8db8ebba9a4e1d7dcc94af7725cad445a39f23e367256a14d0e0e75f29fa169a`).
Read-only reconciliation found the exact 249,748-byte records object on both
publication replicas; the timed-out POST did not leave it missing. Both
native-1674 operator inputs were retained, but validator 2's active steering
intent and EMA are still at native epoch **1661**, with no later intent history.
Thus object recovery does not establish native-1674 steering, and the
continuity failure remains real. This is a strict finding for final review,
while the R46 owner continues. [State triage](peerreview/evidence/FINAL-2-R46-continuation-20260925/native1674-triage.receipt.json)
(SHA-256 `1bc3ac7a1c725958c8f429e094fb569464e6a4a202f09d867a35338fd723da8d`),
[replica readback](peerreview/evidence/FINAL-2-R46-continuation-20260925/native1674-record-replica-readback.receipt.json)
(SHA-256 `ef2f8b1f7c5802ec52caa1858f373f3283384c02abeb6e4ff4dffa075b263165`).
The owner then crossed the first measured epoch boundary without stopping. Its
first completed epoch-632 observation, taken at 18:51:03 UTC on finalized
block **8,084,997**, reports 808 valid fleet bindings and a complete epoch-631
usage source for both operators. The LAN `eth_getBlockByNumber` independently
returned its exact EVM head hash. Policy rate readiness is **false** in this
observation: operator 2 recorded **14,687,835 bytes**, equivalent to **177,208
TAO rao** at the 10,000,000,000-rao tier, below the configured
**200,000-rao** two-times-native threshold. The source marks this as a
provisional low-usage shortfall, not final acceptance. The run continues to
collect later epochs and terminal evidence; this row remains a strict final
review finding. [Exact observation](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch632-first-observation.json)
(SHA-256 `7f1ea3f1ea13f035f125a5df029f92a9e6a6cc10d292df08ad21e36181077c2d`),
[offset and LAN-chain receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch631-closure.receipt.json)
(SHA-256 `b15298ebb333bcc3d8cba380bd9d650947e37c6b494e18498b147688fea320bd`).
Three further R46-scoped process findings came from miner-swarm receivers:
`exit-gap-timeout` on swarms 17, 1 and 9. The raw lines each show an
unresolved sequence 0 after 60 seconds. A read-only comparison found
validator 1's matching sender ACK-lifetime warnings on message numbers 57,
21 and 17. The `queued=58/22/18` field means the earliest retained
out-of-order sequence, not a count of queued packets. The exact cause of the
missing sequence or ACK state is unresolved. The owner explicitly logged
`observation_continues=true final_gate_unchanged=true` with these findings
and remained active. They stay blocking in raw final evidence; provisional
continuation does not convert them to passes.
[Raw-line and continuation receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/exit-gap-three-1856.receipt-v2.json)
(SHA-256 `c1d45234c8ea98037f15ac838c9b903e2767ead1d824343d93991fdbd1a4ef1a`).
By the 19:09 read-only cut, this pattern had grown to **eight timeout events
across six swarms**. It is recurring transport degradation, not a benign
single warning. Continuing still yields useful measured evidence: **996 of
1,000 providers** were running, the four disabled providers were the signed
head-boundary targets, and validator 1 produced **175 fresh proofs** through
both operators during 19:00–19:07:38. Validator 2's native steering gap
remains a separate strict failure. The owner and fleet supervisor remained
active; no live repair or waiver was applied.
[Read-only synthesis](peerreview/evidence/FINAL-2-R46-continuation-20260925/exit-gap-spread-1908.receipt-v2.json)
(SHA-256 `998b2924731a710b9cf0962121cf4e0e8771bcde5ff38c4f64167c214e881667`),
[provider health](peerreview/evidence/FINAL-2-R46-continuation-20260925/swarm-health-1906.receipt.json)
(SHA-256 `fa7abecea4d61206d7f88c1e23553ae7c01e4b19ed76e87197be0672acd32fd6`),
[proof progress](peerreview/evidence/FINAL-2-R46-continuation-20260925/proof-progress-1907.receipt.json)
(SHA-256 `866584729cdce559badb5e0771ca84eda9d4d5372d7e8fa1142fd893914a2699`).
R46 also crossed the second measured boundary. The owner's first epoch-633
observation is pinned to finalized block **8,085,274**. It had valid fleet
bindings but rate readiness false because its source still identified epoch
631; the rate check requires the immediately preceding complete epoch 632.
The next completed owner observation, at block **8,085,300**, selected
complete epoch-632 sources for both operators: **20,824,859** and
**23,593,398 bytes**. It reports rate readiness **true** and valid fleet
bindings. Independent LAN EVM reads matched both observation head hashes.
The boundary lag is preserved; the later recovery does not erase it or grant
final acceptance. [Boundary observation](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch633-boundary-observation.json)
(SHA-256 `44abbf91cb060bc54cfd0644a970c3288c25d61bdf577a232482a0c9c2953233`),
[boundary receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch632-boundary.receipt.json)
(SHA-256 `9606fe03882b7529a950c1d3430896a311c770b135c5432ac8ac418f8ccd6005`),
[recovered observation](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch633-source-recovered-observation.json)
(SHA-256 `c0a47feb3472dbacaf45e982bf2c950ea3132ddd4f796c7db4d70c6a20ec5d5d`),
[recovery receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch632-source-recovery.receipt.json)
(SHA-256 `360d86eb1f8b6df2b1106eb4af771c2e01123426e5a00e43359a6fc9b8ba2d24`).
During epoch 633 the scheduled `head-boundary` miner control was recorded
restored at block **8,085,444**, and the paired
`validator-local-head-boundary` view filter at block **8,085,449**. The
owner remained active and kept observing after both. The read-only
[restoration receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/head-boundary-restored.receipt.json)
(SHA-256 `70137f9ca120522d416bbd1080b8deafb58604fafd4b11373681cb205c72f769`)
binds both rows to the sampled fault-file hash; it does not claim the other
scheduled faults or final interval are complete.
At the 20:49 cut, R46 retained a new validator-1
`release-steering-attempt-failure`: its local client-observation POST was
canceled by a termination signal during the signed `release-rolling-30`
validator-1 restart. The fault ledger records that restart restored at block
**8,085,579**; a replacement validator process was active. The raw finding
remains blocking even though its trigger was scheduled. All 1,000 provider
processes were running in the read-only health sample, and validator 1 had
produced fresh proofs before the restart. No post-restart proof or new
validator-1 intent/weight recovery had been established at this cut.
Validator 2's prior steering gap remained open, and its own scheduled rolling
restart had begun. [Bound restart receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/validator1-restart-2049.receipt.json)
(SHA-256 `eecf23fc6a73e4943cd6eee5692f701400343ad9235ba3d73e00a3f9e40d5f4d`),
[provider health](peerreview/evidence/FINAL-2-R46-continuation-20260925/swarm-health-2047.receipt.json)
(SHA-256 `e872f264cd605908e5abe40df861ba9b3533c878b4e42e196125142548288521`),
[proof progress](peerreview/evidence/FINAL-2-R46-continuation-20260925/proof-progress-2047.receipt.json)
(SHA-256 `3bc0227b5f26129309826f5d3e6f4839f895d9476df19ced46e6410c91e5519e`).
The owner subsequently crossed the third measured epoch boundary. Its first
epoch-634 observation, at finalized block **8,085,598**, selected complete
epoch-633 usage sources of **21,465,073** and **17,885,137 bytes** for
operators 1 and 2. It reports rate readiness true and 808 valid fleet
bindings. An independent LAN `eth_getBlockByNumber` read matched the exact
EVM head hash. This establishes that the owner observed the epoch-633 close;
it does not establish terminal acceptance or post-restart validator steering.
[Exact observation](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch634-first-observation.json)
(SHA-256 `e8be91fb30a95620357e946940a3b427342f1cbc153d9e51e3f4de12dab43b0b`),
[offset and LAN-chain receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch633-closure.receipt.json)
(SHA-256 `1b98ee7a1912f1e2c834df8130085782ba6c44773cefd37c586fdd0e57783a77`).
The owner then crossed the fourth measured boundary. Its first epoch-635
observation at finalized block **8,085,878** selects complete epoch-634
usage sources of only **5,250,701** and **5,732,655 bytes** for operators
1 and 2. Fleet bindings remain valid, but rate readiness is **false**:
operator 1's source yields **105,582 TAO rao** at the zero-conviction tier,
below the configured **200,000-rao** two-times-native threshold. This is an
authenticated low-usage result, not a stale-source identity error. The
rolling validator restarts overlap this period, but the exact traffic-loss
cause is not established by this observation. R46 continues through its
fifth measured epoch and terminal checks despite this strict shortfall.
[Exact observation](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch635-first-observation.json)
(SHA-256 `adda8561024d55080fd8bdfbfa08550886b9e021c60e64fa5ff7a44a2ac0ff85`),
[offset and LAN-chain receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch634-closure.receipt.json)
(SHA-256 `5556ea9345629fb0f393fdefc629f4605c63b071105bb7f1e98728c6bdbe148d`).
The narrow code change permits
authenticated provisional process-log findings to be retained while the
owner continues observing; the strict terminal gate still sees them. No
final acceptance is claimed from the provisional continuation.
[Launch bundle](peerreview/evidence/FINAL-2-R46-continuation-20260925/README.md).
The owner subsequently observed the fifth measured epoch close. Its first
epoch-636 observation is pinned to finalized block **8,086,174**, the signed
measurement end. It selects complete epoch-635 usage of **6,804,866** and
**6,654,281 bytes** for operators 1 and 2, reports 808 valid fleet bindings,
and again fails rate readiness: operator 1 yields **136,834 TAO rao** against
the **200,000-rao** two-times-native threshold. An independent LAN
`eth_getBlockByNumber` read matched its exact EVM head hash. This proves all
five measured epochs were observed. The sealed owner result below records
terminal acceptance as failed.
[Exact observation](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch636-first-observation.json)
(SHA-256 `b7f3ec16f0f3b6802c3c1fc2e8fc48cf67b17848fefc3b6ae419299fb2f79197`),
[offset and LAN-chain receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch635-closure.receipt.json)
(SHA-256 `45db21698da6851ece9eac87faac287dbd88beb3ed72dd5a92bcd9775792c7db`).

Read-only restart analysis found **zero epoch-634 proofs in all four
validator/operator proof files**. Validator 1 stopped producing proofs at
20:43:04 UTC during its scheduled restart; validator 2 resumed both streams
at 22:07:38 UTC after approximately 80 minutes. Validator 1's replacement
process remained alive and advanced its census-replay scratch files, but no
post-restart proof or running announcement had appeared by the 22:33 cut.
This supports restart replay starvation as the likely explanation for the
epoch-634 rate collapse; it does not establish a deadlock or a complete
causal account of every low-usage interval. The swarm exit-gap event count
was unchanged after 20:43:05 UTC in the bounded review. These remain strict
findings in the sealed terminal result.
[Bounded proof and process receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch634-restart-causality.receipt.json)
(SHA-256 `17581a1b35340a4fa1ea2516bf0d7b6518ceaabe9689de4c4d44c1c257e65ed4`),
[validator-1 startup receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/validator1-startup-phase-2233.receipt.json)
(SHA-256 `8aacf6e26f77e3bd61515e9c06dc146d94eced99f98d2b12ffec48ab0801d8d1`).
The post-R46 restart-controller review reproduced two adjacent generation
races: an old proof read could be used for a replacement, and a replacement
arriving after the completion-head read could lose its pending intent. The
fix rechecks the exact signed generation around both reads, keeps a newer
intent pending, and requires that generation's fresh trails and head. Both
pre-fix controls failed deterministically; the focused and adjacent tests
passed normally and with race detection. This is qualified future hardening,
not evidence that R46's missing proofs were restored.
[Review and causal controls](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-restart-generation-fix-review.md)
(SHA-256 `f219d3dafc81c7c6e3b026f6d4934a87b53cedb12c2e80335ebb762be4c8a1c9`),
[qualification receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-restart-generation-fix.receipt.json)
(SHA-256 `9d0d9936fcf2c73a033588fe8ef29fb202a26871d5feeea4d3720d649f7acec2`).
After the fifth epoch and signed terminal block were crossed, the owner kept
polling failed strict assertions. All **42** scheduled faults had been
restored, but the accepted epoch usage and retained process findings could
not be repaired by a later observation. A graceful interrupt requested a
failed partial terminal result rather than waiting several more hours for
the watchdog. The owner sealed its result at 00:08:52 UTC with end epoch
**637**, finalized EVM head **8,086,545**, and no final acceptance; it exited
at 00:10:48 UTC. The signed generation-46 envelope retains the exact
boundary and records `execution-exited-before-completion` at 00:09:56 UTC.
The independent monitor captured **19 files, 184,188,789 bytes**, with zero
copy errors after owner exit. These facts establish a complete measured
window and a sealed failed owner result, not a passing release.
[Signed exit envelope](peerreview/evidence/FINAL-2-R46-continuation-20260925/generation46-signed-exit.evidence.json)
(SHA-256 `5b4e635ae2fcabdb78eb9e9a3bb2766e570b180a1599ddf7f5231a39f91fa4a6`),
[independent capture receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/terminal-capture.receipt.json)
(SHA-256 `40cd5376a6473c1946afd06f1d42a868c5f2055f5757b319a418e55ed47dc35d`).
Independent LAN `eth_getBlockByNumber` reads returned signed terminal block
**8,086,324** and matched the owner's exact end-head hash at **8,086,545**.
[Terminal chain receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/terminal-chain-heads.receipt.json)
(SHA-256 `bb9378a8346fb4d2d3c5233360cbba8c0149fe836c251e26fa9f96ac3821f21f`).
The post-R46 terminal-control fix seals a failed provisional result once an
immutable in-window failure and complete fault cleanup are authenticated,
instead of polling for an adversary sample that can never make that interval
pass. Its real scheduled-fault controller, negative controls and race tests
passed in an isolated source build; final acceptance rules remain strict.
[Terminal fix review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-terminal-completion-fix-review.md)
(SHA-256 `45834e44a5872f9b288a1ea651ddf39a218eb252a95ffe6545ac793d487b5754`).
Composed review caught an adjacent wire-format error before deployment: the
process-log scanner emits bare 64-character line hashes, while the first
terminal fix accepted only `sha256:`-prefixed hashes. Real repeated
exit-gap/TLS findings therefore remained strict but did not trigger prompt
failed sealing. A scanner-produced pre-fix regression failed; the narrow
correction passed normal and race tests, with malformed, foreign-scope and
recovering findings still ineligible. [Hash-format fix review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-terminal-log-hash-fix-review.md)
(SHA-256 `13733f4a26e895e0661fc754f15ac60b12e2557ec5a55b82a97f6293890f607b`).

The sealed **86** failures have been assigned once each to these diagnostic
groups. Counts are failed assertion rows, not independent root causes:

| Diagnostic group | Rows | Current interpretation |
| --- | ---: | --- |
| Adversary actors or prerequisites | 59 | Fifty-four vector rows share a handful of failing actors; consensus sampling was skipped without applied independent validator intent. |
| Validator native decisions missing | 14 | Validator 1 produced no fresh applied decision, vector or deposit audit after restart. |
| Inherited lifecycle exception | 3 | The approved prune/re-register bypass still cannot satisfy strict lifecycle proof. |
| Retained process findings | 2 | Ten miner exit-gap classes and validator steering failures remain in the strict log gate. |
| Claim census at the terminal cut | 2 | The selected census has 200 finalized claims for operator 1 and 199 for operator 2; miner 881's operator-1 epoch-635 submission was unresolved at that cut. |
| Cancellation and publication | 2 | Graceful terminal cancellation interrupted publication; the owner result is still retained. |
| Governance drill, payout tier, cohort separation, open-anomaly aggregate | 4 | One row each, requiring separate source review. |

The [exact failure-cluster receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/failure-clusters.receipt.json)
(SHA-256 `bb861d78cbbb5449c9f2b439e6ecfea231620ae6ad33d2ff5112cfd5b5a5f0bc`)
lists every assertion ID, message and observation hash. This grouping is
triage, not a waiver or a claim that any failed gate passed. The owner
[faults](peerreview/evidence/FINAL-2-R46-continuation-20260925/owner-faults.json),
[process logs](peerreview/evidence/FINAL-2-R46-continuation-20260925/owner-process-logs.json),
[adversary evidence](peerreview/evidence/FINAL-2-R46-continuation-20260925/owner-adversaries.json)
and [anomalies](peerreview/evidence/FINAL-2-R46-continuation-20260925/owner-anomalies.json)
are copied byte-for-byte from the independent terminal capture.

An independent actor review found a concrete observation defect: each healthy
operator surface in the retained samples returned exactly **100,000 stats
rows and 10,000 proof rows**, and the per-operator body hashes stayed
unchanged across R46. The pinned server orders those endpoints oldest-first;
an unbounded request capped at those counts can omit the current interval and
freeze quality-cohort measurements. It also found separate harness defects
where a successful API response inherited unrelated process unhealthiness,
signed verify failures could be excused by a scheduled fault, and later
success obscured earlier actor errors. Isolated fixes request bounded time
ranges, reject a full page, and retain strict provenance and process checks.
Their fixed focused suite passed **61** tests and the race subset **40**;
five controlled old-behavior checks failed as expected. These patches were
not present in R46 and do not revise its verdict. The same review leaves
**35 RPC errors, six artifact errors, 21 health findings, two restart
anomalies and 18 exit-gap events** as strict findings with no proven shared
root yet.
[Exact sealed triage](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-adversary-triage.receipt.json)
(SHA-256 `b0d9cab66764c9ae213d810299a00f257fd046c3303c4f524ce926ca6091b8e1`),
[isolated fix and test receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-adversary-fix-review.receipt.json)
(SHA-256 `b20be41e763ba3b996d33075de1b581d02d6ca1ef57155aff0eb0d1efd1733d9`).
The sealed actor summaries, observations and owner journal do not retain
individual RPC and artifact error chronology, so the **35 RPC** and **six
artifact** error counts cannot yet be assigned a specific shared cause.
The non-faulted EVM egress logged no R46-scoped failures; earlier deadline
lines precede this acceptance window. A read-only census of the diagnostic
capture found 31 epoch closures and 142 referenced streams totaling about
878 MB before overlap. Validator-2 capture was still reading serial source
chunks at the recorded cut. A separate RPC verifier defect was found and
tested afterward: two responses at the same wrong height, or two malformed
32-byte hashes, could be accepted as a common owned block. That is an
adjacent integrity fix, **not established as the cause of R46's 35 RPC
errors**. [Actor and capture triage](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-rpc-artifact-capture-triage.receipt.json)
(SHA-256 `bb20784b39dd38b0d003fbc5ed675724ecb072ae88cf560c699f93e63b3a31fd`).

A separate exact-log review pins **18 receive-sequence exit-gap events across
10 swarms**; these are sequence gaps, not process exits. The pinned receive
idle timeout is **120 seconds**, whereas the sender retains its sequence for
**300 seconds**. A follow-up join found a matching same-peer validator-1
ACK-lifetime exit for **all 18** receive gaps. Each sender's idle interval
before the next write was **154.30–295.32 seconds**, inside that mismatch,
and its first unacknowledged number was one below the queued receiver tail.
That timing suggested premature receiver retirement. A deterministic actual
wire/HMAC/encryption fixture, however, recovered after 155 seconds idle in
plain, encrypted and combined receive/TLS state-loss lanes. The timeout
asymmetry alone is therefore **insufficient to reproduce the failure**.
The retained rows still do not identify
the missing acknowledgement, full head or route for every event, and they do
not establish one shared defect for all 18. Lost feedback, contract-ahead
state and route replacement need separate causal tests before changing
transport behavior. The same
review confirms the epoch-1674 native records object on both replicas and
both durable input cuts, while the applied intent remained at native epoch
1661; a recovered object does not repair that authenticated continuity gap.
[Exact gap and native review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-gap-native-root-review.receipt.json)
(SHA-256 `a5e4a466e3e20a23db821b6bdfe6b9c6c932e6cda09c6616f6d7146811f6aa8d`),
[all 18 sender/receiver pairs](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-exit-gap-idle-pairs.receipt.json)
(SHA-256 `b93eb1ed494b9a118e91963e781af69663335f4c0325d567c29191ffa330cba6`).
A separate prefetch-ordering bug was reproduced on the actual signed and
encrypted wire path after idle, but a bounded scan of all 18 sealed R46 peer
windows found **zero** no-contract or contract-verification failure witnesses
and prior verified cipher evidence in 15 windows. That fix is adjacent
hardening, not an established explanation of R46's exit gaps.
[Contract-witness scan](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-exit-gap-contract-witnesses.receipt.json)
(SHA-256 `dd0242ef057041cc49002621697bf82c49cf5fc5b5ddb082da7218407474e905`).
The adjacent Connect correction defers a non-head `ContractAhead`
announcement while no acknowledged application packet is outstanding, so a
recovering current-contract head can arrive first. Its final **56 normal and
56 race tests passed**; old-source wire regressions failed as expected. The
guard may defer prefetch on sparse or no-ack-only traffic until an
acknowledged packet exists. This bounded performance effect and the absence
of matching R46 contract-failure witnesses prevent a claim that it repaired
the sealed exit gaps. [Connect fix review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-connect-prefetch-fix-review.md)
(SHA-256 `17d662865b9ac684a86fdcf1cb890b8a29815d9586a202b48e92260c547b181b`),
[qualification receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-connect-prefetch-fix.receipt.json)
(SHA-256 `8a4f028e50aefd9b4dbd00aa40b2fefcfa001375ad8a613166731e8c22d1a058`).
The same correction was rebased onto Connect main at `d82db9e0` and
fast-forwarded to main as `b1361a93`. The focused current-main qualification
passed **56 normal and 56 race tests**. The [integration receipt and test
logs](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-connect-main-integration.receipt.json)
bind that result and the observed remote main head. This is a future-run
transport fix; the sealed R46 exit-gap findings remain unchanged.

The sealed read-only diagnostic currently reports
`result-start-and-fault-binding` failed with “lifecycle cleanup completion
appeared without its prior signed request.” The exact retained sequence is
more specific: the signed start has both lifecycle faults pending; the
authenticated observation at block **8,086,331** records their request; the
observation at **8,086,382** records completion; and both final result rows
equal the signed latest checkpoint. The authenticated observation prefix
contains **74 rows and 89,796,733 bytes**, with no uncredited suffix.
The diagnostic reader compared start directly to final using an adjacent
transition rule, so this diagnostic failure does not prove a missing request.
It also does not turn the failed R46 acceptance into a pass. The completed
diagnostic retains this false failure in its original, immutable result.
[Sealed checkpoint comparison](peerreview/evidence/FINAL-2-R46-continuation-20260925/sealed-r46-cleanup-history.receipt.json)
(SHA-256 `97352693576e783b6a81edf42c38341293f20a81b721f2ca47fd0e44403f39ff`).
The post-R46 reader fix walks the authenticated intermediate checkpoints in
order, retaining the strict adjacent transition validator on each edge.
Five focused normal tests, adjacent normal tests and the combined race run
passed; three controlled old-behavior cases failed as expected. The code
change cannot revise R46's sealed owner result.
[Cleanup-reader fix review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-cleanup-history-fix-review.md)
(SHA-256 `fe8b49807072161d0b60510ece8401f5e6999d6a8765eda5df15f611f718b551`).

The independent sealed diagnostic completed at **01:46:49 UTC on September
26** with **39 checks: 19 pass, 9 fail, 9 unavailable, one finding and one
exception**. Its complete report and receipt are copied into the evidence
bundle without changing the owner result. The nine failures include the
signed owner invalidation, absent clean completion, terminal assertions,
strict acceptance, process-log findings, lifecycle timing, validator-2
source capture, the cleanup-history reader's false transition, and historical
contract-census authorization. The last failure needs a precise comparison
with the retained signed corrective request and approved plan; it is not
waived. The nine unavailable checks cover source-dependent validator checks,
lifecycle artifacts, compact capture, the campaign reader's wrong directory,
and the absent original semantic input bundle. The passing adversarial matrix
does not imply a passing captured adversarial campaign. The lifecycle bypass
remains a disclosed exception, not acceptance.
[Complete 39-check report](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-terminal-diagnostic-report.json)
(SHA-256 `59cdb27223fa48bc95184dc19f38674b80bb7d88ebc97f3e5de9183efece6421`),
[terminal receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-terminal-diagnostic.receipt.json)
(SHA-256 `01dbe81a994fa5fa2a9ca5ae9b04616000033a5f94521b68bc9b49b0b4321885`).

Follow-up source review confirmed the archived plan did not list the later
corrective transaction as a static action. The current descendant plan does
carry the owner's signed repair request and result, whose two finalization
rows match the retained journal. A post-R46 reader change now admits those
actions only through that exact carry, with a deterministic old-source refusal
and controls for changed intent, absent carry and duplicate finalization.
The subsequent post-R46 chronology change adds a separate signed-result proof
for that corrective activation while preserving ordinary postconditions for
ordinary actions. Its deterministic old-source refusal, normal and race tests
passed; the complete R46 archive has not yet been rechecked with this change.
The R46 diagnostic's original census failure remains part of its sealed result.
The post-R46 reader's exact old-source failure, normal and race commands,
negative controls and initial schema boundary are in the
[repair reader review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-repair-history-reader-review.md).
The separate [signed-result proof review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-repair-result-proof-review.md)
records the follow-up fix and its remaining live-archive qualification limit.
An adjacent chronology review then found a second false rejection: the signed
correction follows an ordinary activation, but the timeline builder compared
it with the source plan's older upgrade baseline. The retained prior
activation matches the repair request's signed upgrade address and runtime;
the older baseline matches neither. The corrected checker uses that signed
predecessor only for corrective activations and keeps the ordinary-upgrade
rule intact. Its valid-repair and missing-predecessor regressions failed on
old source; the fixed combined historical selection passed normally and with
race detection. [Timeline review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-repair-timeline-review.md),
[retained field comparison](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-timeline-retained-field-comparison.json),
and [qualification receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-timeline-qualification.json)
bound the conclusion. A full archived semantic replay remains pending; this
code change cannot alter the sealed owner verdict.

The independent sealed diagnostic advanced after its validator-2 source
capture deadline. That capture failed at cut **70/96**, operator 2, while
reading a 3,162,606-byte records chunk; its dependent native-application and
relay-readback checks were marked unavailable. It then retained the strict
process-log and lifecycle activation failures, captured companion evidence,
and passed the signed-payout-artifacts check. At the 36-check checkpoint,
compact validator capture and lifecycle payout artifacts were unavailable;
the adversarial matrix check passed, while its campaign reader was unavailable
because it selected the diagnostic directory instead of the retained run.
This was an intermediate checkpoint; its partial checks do not override
the later complete inventory or owner result. [36-check progress receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-diagnostic-progress-36.receipt.json)
(SHA-256 `ebea67dc3525d7526152c945437d32219154f04461075291ca3515d6f9905360`).
The subsequent read-only capture fix bounds parallel origin readers, retains
durable witness ownership and rejects late success after a deadline; its
isolated normal and race suites passed, with old-behavior controls failing.
[Capture fix review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-terminal-capture-fix-review.md)
(SHA-256 `c13b3fd10d9d5e81380b6df79bd0332869c86423df92c2fccc2848b9e93b5f2f`).
The completed independent replay used that corrected two-origin capture. A
read-only [source census](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-capture-census.json)
at 07:10:52 UTC counted **107** immutable cuts, compared with **96** in the
earlier diagnostic: later retained closures enlarged the archive being
replayed. It found no additional concrete retry or custody defect. The
[bounded review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-capture-review.md)
and [normal/race test receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-capture-review-tests.receipt.json)
did not predict a passing source capture; the replay result is reported next.

The independent **v3 replay completed** at 07:46:02 UTC with **39 checks:
22 pass, eight fail, seven unavailable, one finding and one disclosed
exception**. Its exit code 0 means the read-only report completed;
`final_acceptance=false`. Relative to v2, the retained result binding,
validator-2 signed-source capture and validator-2 native coverage now pass.
The original adversarial campaign is no longer misreported as missing: its
authenticated failed vector remains a strict failure. The corrected
historical chain census still fails on a different journal action,
`repair.precompile-residual.v2.1`, absent from static plan
`0x016027a9eac736eae4db4485eb624011f234eefbd0c6b56d71c94a1cf867ba5b`.
Its authorization must be proved from exact signed carried evidence or the
failure retained. The original strict semantic source is unavailable because
the original run has no `final-inputs` directory; v3 does not create a
passing semantic bundle. [Complete v3 report](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-terminal-diagnostic-v3.report.json)
(SHA-256 `bc298ccb386c5066a7d5c38bd70c113f536c354e4acd8440dc6071288c6549fb`),
[comparison and provenance receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-terminal-diagnostic-v3.receipt.json)
(SHA-256 `cd332db64dfe7fabd8e1e69aa4d5e208304fc4fe735065d79ed91d1a4242e5bd`).
The supplemental precompile repair is now admitted by a narrow historical
reader that verifies its source-plan scope, dual-signed v2 authorization and
completion, journal checkpoints, action intents and exact finalized receipt
tuples. Its old-code regression reproduced the v3 census rejection; the
focused suite passed **73** selected tests normally and under race detection,
including **23** negative repair controls. A separate read-only replay of the sealed v3 inputs then
**passed the corrected historical contract census**: 125 approved plans,
59,979 journal entries, 1,371 relay requests and ten release-contract
addresses from block **7,888,670**. It made **zero RPC reads** and recaptured
no validator data. This establishes the corrected census only; the original
v3 chain check remains failed and no complete canonical/native check or final
acceptance is claimed. Bounded EVM chronology follow-up is separately pending.
[Precompile source review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-precompile-census-qualification/review.md),
[normal/race qualification](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-precompile-census-qualification/qualification.json),
[offline census receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-retained-census-replay/census-replay-receipt.json),
[replay custody review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-retained-census-replay/CENSUS-REPLAY-REVIEW.md),
[address range](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-retained-census-replay/replayed-census.json).
The follow-up historical chronology found a distinct predecessor-plan
admission error: four approved plans precede deployment and therefore name a
zero coordinator proxy. They contain only planned proxy work and preparatory
finalizations; none records a finalized proxy transition. The old timeline
rejected those plans before examining the actual deployed proxies. The
corrected readers share one narrow predeployment census and reject any
finalized proxy initialization, upgrade activation or rounding repair under
a zero-proxy plan. Its pre-fix regression failed; all **58** selected tests
passed normally and under race detection, including **32** mutation controls.
The exact sealed R46 replay passed with **125 plans, 59,979 journal entries,
1,371 relay requests, two deployed proxies and ten emitters**. Its census
bytes match the prior qualified result exactly. This is a scoped historical
admission result, not a reversal of R46's sealed failure or a full semantic
acceptance result. A separate bounded LAN EVM chronology captured **11
`Upgraded` events** and two initializer baselines from block **7,888,670**
through **8,084,595**, using **236 read-only RPC exchanges** and no transport
errors. Every event matched an exact successful transaction receipt and
canonical block; the 11 transactions correspond to two proxy
initializations, eight ordinary activations and one signed rounding repair.
The old source reproduced its zero-proxy timeline refusal after sealing that
capture. An offline replay against the corrected source then authenticated
the same 125 plans and 59,979 journal entries, rebuilt both proxy timelines,
and passed the production timeline builder and artifact verifier using the
sealed capture with **zero network reads**. This closes the bounded EVM
chronology check only. The original v3 diagnostic and R46 owner verdict stay
failed, and a complete canonical/native check and final acceptance remain
unproven.
[Source review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-predeployment-timeline-qualification/review.md),
[normal/race qualification](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-predeployment-timeline-qualification/qualification.json),
[retained replay receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-predeployment-timeline-qualification/retained-census-receipt.json),
[evidence manifest](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-predeployment-timeline-qualification/SHA256SUMS),
[bounded LAN capture](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-evm-chronology-qualification/capture/qualification.json),
[offline replay](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-evm-chronology-qualification/offline/qualification.json),
[portable evidence manifest](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-evm-chronology-qualification/SHA256SUMS).
The v3 validator-2 relay readback is unavailable for a separate, genuine
historical gap: of **66** signed publication members, all **64** closed-census
slots for epochs 604–635 lack the original relay request, result and journal
owner; only two audit members have requests. The predecessor generation has
no epoch-603 closure or publication, so the old single-cursor scheduler
waited at 603 and never routed the authenticated successor beginning at
cutoff 604. The post-R46 scheduler now advances each signed generation from
its own cursor while leaving the missing predecessor and historical capture
strict. Three old-source scheduler regressions failed; **29 focused tests
passed normally and under race detection**. This future-run correction
cannot create the absent R46 requests or pass its sealed readback.
[Exact retained census](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-relay-owner-census.receipt.json),
[root-cause review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-relay-owner-review.md),
[qualification](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-relay-owner-results.json).

The failed adversarial campaign has an additional future-run source-selection
defect, not an R46 acceptance waiver. Its consensus actor skipped all
**4,757** attempts while reading a legacy validator-2 intent path, although
the scenario observer selected the approved provisional/V2 generation. A
deterministic old-code test reproduced that disagreement. The corrected actor
uses one authenticated generation for both vector and metrics, with no legacy
fallback; **17 distinct focused tests passed normally and under race
detection**, including forged and missing-source controls. The retained R46
observation census still has validator 1 absent in all **74** rows, while
validator 2's epoch-1661 local intent predates the signed R46 baseline.
Neither the actor fix nor its synthetic passing samples supply fresh native
receipts, real mask coverage, or a passing original campaign. The original
**59 failed `adversary_*` assertions** and v3 campaign failure remain.
[Source-selection review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-adversarial-source-fix-review.md),
[sealed qualification](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-adversarial-source-qualification/qualification.json),
[retained source census](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-adversarial-source-qualification/retained-observation-census.json).

The R47 preparation review found a launch blocker independent of the R46
terminal verdict: generation 1's activated `handoff.json` is immutable, and
the old code would write a generation-2 handoff to the same path. An exact
pre-fix test reproduced the collision. The corrected path preserves the
original file and sealed runtime inventory, records each successor in a
versioned postcondition, and selects it only after its verified journal
receipt. Fresh validator configs, API contexts and relay cutoff readers then
select the authenticated generation while older namespaces remain available.
The **11 launch-critical tests passed normally and under race detection**;
separate generation-2 source-role/native-slot controls also passed in both
modes. This is source qualification, not a live generation-2 activation or
fresh native history. R46's missing V1 history and stale V2 application are
unchanged. Before a new release interval, the exact successor plan must be
reviewed and staged, the retained supervisor officially stopped for handoff
selection, and the authenticated native source-role continuation applied
before resume. [Successor readiness and commands](peerreview/evidence/FINAL-2-R46-continuation-20260925/r47-successor-readiness/commands-successor.md).

The missed payout is independently visible on-chain. A LAN historical
`eth_call` at the owner's finalized block **8,086,545** returns status **3
(`RootMissed`)** with zero commitment fields for operator 2, epoch **634**.
The `RootMissed(634, 2, 0)` log was emitted in transaction
`0xd7edee5109490c124debd59420de2628c71bcb3c29c32cd45f8e924ab87d35a6`
at block **8,086,027**, inside the signed measurement window. No operator-2
epoch-634 payout tier is present; later epoch artifacts do not prove that
missing root. The selected claim census has a separate timing finding:
miner 881's operator-1 epoch-635 claim was still `submitting` at the owner's
cut, although its queue later finalized at block **8,086,568**, after the
signed terminal block. The later receipt must be recorded as a supplement,
not used to rewrite the terminal verdict. The operator-2 count of 199 is
associated with the missed epoch-634 root, not with miner 881's submission.
[Historical LAN call, event and claim-cut receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/no2-epoch634-rootmissed-claim-cut.receipt.json)
(SHA-256 `1ca99f9efbc4d1d055813fe1468c427ac3b1dc7557beecec6b44e8ad61247ffa`).
The pinned taskworker log closes operator 2's epoch 634 at 21:46:43 UTC with
**zero payout leaves and a zero root**. Operator 1 closed the same epoch with
four leaves and confirmed its nonzero root 32 seconds later. The pinned
server intentionally skips root submission when no leaves exist; the chain's
`RootMissed` is therefore consistent with an empty operator-2 payout census,
not evidence of a timed-out root submission. All four validator/operator
proof streams had zero completed epoch-634 proofs after the scheduled
restarts, making proof starvation a plausible upstream cause. A
provider-by-provider eligibility census is absent, so this evidence does not
establish exclusive causality for the empty census. The original missed root
remains a strict historical failure.
[Taskworker, source and chain receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/epoch634-empty-payout-census.receipt.json)
(SHA-256 `0e32519ae98e26f137b73cb851624f89d9670c2a3c9a6ed157bd5096aebffe7e`).
The post-R46 restart correction now requires a replacement validator to
produce fresh, fully signed proof trails for every approved operator before
the scheduler treats the restart as restored. An isolated **32-test normal
and race suite** passed; old PID-only and final-hop-only controls failed as
expected. A separate server test exercises zero confirmations, zero leaves,
then a newly confirmed exposure producing one leaf, while Solidity tests
cover zero-root refusal and operator-scoped `RootMissed` carry. These are
future-run fixes and controls, not retroactive evidence of R46 acceptance.
[Fix and test review](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-restart-payout-fix-review.md)
(SHA-256 `569b4ff0c0bc55766c0326bbc07bd326ca473858094330da4410cec82107a4ed`),
[exact native steering rows](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-native-steering-rows.receipt.json)
(SHA-256 `195a9ce4e4c15a979cb3dfd4773f7d8d3cf3547919a557fde6aa45dfe02e2858`).
The server zero-leaf regression is now on Server main as `db680048` after
passing its focused normal and race runs against the recorded current source
set. [Current-main qualification receipt](peerreview/evidence/FINAL-2-R46-continuation-20260925/r46-server-main-payout-test.receipt.json)
preserves that narrower result; it does not establish the exclusive cause of
the original operator-2 empty census.
The first durable R46 observation was taken at 17:30:57 UTC on finalized
block **8,084,596** (hash `0x0fd8adbca952fcce06db21ab0f057cf78e16c4309273226e6d66ef84e8ce5192`).
It found **808/808 fleet bindings valid** and current-policy rate readiness
true: complete epoch 629 carried 21,410,245 and 20,543,745 bytes for the two
operators, clearing twice the native minimum at every tier. This is a fresh
readiness observation, not a signed acceptance start.
[Exact observation](peerreview/evidence/FINAL-2-R46-continuation-20260925/first-observation.json)
(SHA-256 `fd20218e9bd65fdde89613e8e198562e4c881a25141c4555344886e23e717073`).
The companion-filter dependency remains known exception **R44-LC-1** below,
with its lifecycle assertion failed rather than waived as a pass.

**R45 sealed owner result, 2026-09-25 16:56 UTC — failed before a measured observation.**
The live owner crossed the signed start block on-chain, but its last completed
observation was still epoch 629 at finalized block **8,084,357**, before the
first measured block 8,084,374. A process-log heartbeat gate then interrupted
the scenario. Its [sealed result](peerreview/evidence/FINAL-2-R45-renewal-20260925/owner-result.json)
(SHA-256 `3a3b78827adadac55cbb037208ddaa31e90587f2dc0a6ee1cd4f83cc9d74c6df`)
reports **five failed assertions of six**, `result=fail`, `provisional=true`,
and `final_acceptance=false`. The failures are incomplete acceptance interval,
114 open anomalies, process-log completion/publication, and scenario context.
The process-log gate counted five release-blocking finding rows, beginning
with miner-swarm-17's `restart-stale-contract`; validator 2 also reported
`compact head EMA epoch jumped` on native epoch 1673. The owner continued
until this gate; the supervisor was still active at the result read cut.
[Process-log snapshot](peerreview/evidence/FINAL-2-R45-renewal-20260925/owner-process-logs.json),
[steering receipt](peerreview/evidence/FINAL-2-R45-renewal-20260925/steering-failure.receipt.json).

The signed recovery-45 envelope was updated with
`acceptance_invalidated_at=2026-09-25T16:55:38.185272726Z` and reason
`execution-exited-before-completion`. The earlier signed boundary remains
historical evidence, **not** authority to count this attempt as accepted.
[Invalidated envelope](peerreview/evidence/FINAL-2-R45-renewal-20260925/recovery-45-invalidated.evidence.json)
(SHA-256 `1f5a459a7bd0d5e5c9c4534c51a68f7c6e17a516f2671ac1ffc45a7409a5201f`).
At the seal, 39 scheduled faults were pending, two validator-view filters
remained active, and `quality-cohort` was restored. Preserve their exact
[fault record](peerreview/evidence/FINAL-2-R45-renewal-20260925/owner-faults.json)
for owned recovery; no fault is deemed restored merely because the owner
exited. A later read-only
[cleanup receipt](peerreview/evidence/FINAL-2-R45-renewal-20260925/postfailure-filter-cleanup.receipt.json)
(SHA-256 `3e07876cf7dbfadb2575e98eee6656dade3216731226ca5e1328b3dabd470be3`)
found no live active-fault file, operator view-filter files or filter receipts,
while the original fault record still shows their terminal-time status. This
documents live cleanup without rewriting R45's failed assertions. Both
operator scenario bundles were published according to the sealed
result, unlike R44's failed publication. This is prospective publication
repair evidence, not R45 acceptance.

**R45 continuation, 2026-09-25 15:52 UTC — release boundary signed; measured interval pending.**
The round-7 renewal of 202 fleets for epochs 628–659 completed with
`postcondition_verified` status on the unchanged plan
`0x8bb92697db8f2164e46f6e58848d3407e509382fb61550b919f1d55391ad480e`.
Its retained journal records all 1,212 new actions through intent, broadcast,
inclusion, finalization, and postcondition verification. One EVM mirror
transaction, `0xc424bf210d337f82b70c5e3fb0de42868288797b80a0380ab9324c58e578d044`,
has a direct LAN-RPC receipt with status `0x1` at block 8,083,027.
[Renewal result and journal](peerreview/evidence/FINAL-2-R45-renewal-20260925/README.md)
include the exact hashes and a portable RPC response. The retained resume
then dispatched zero setup actions. The R45 controller authenticated all 44
prior recovery generations and signed a new recovery-45 attempt at 13:45 UTC.
That first signed attempt was a preparation checkpoint. The controller remains
active, and the renewed bindings became valid at epoch 628. A full active-epoch
storage census is in progress; no R45 final acceptance result is claimed here.

At 13:52 UTC, separate nonaccepting publication probes using each operator's
resumed runtime configuration and the pinned HTTP handler each passed two
POSTs with exact content/history readbacks. The fresh admin read confirmed a
64 GiB hard quota. The admin usage report was cached at 13:39 UTC, so it cannot
measure the active-binding write rate. These probes establish prospective
publication behavior only; the active-epoch storage gate remains open.
[Operator 1](peerreview/evidence/FINAL-2-R45-renewal-20260925/operator1-publication-preflight.json),
[operator 2](peerreview/evidence/FINAL-2-R45-renewal-20260925/operator2-publication-preflight.json),
[quota readback](peerreview/evidence/FINAL-2-R45-renewal-20260925/blob-quota-readback.json).

At 13:54 UTC, the live controller logged a **nonblocking provisional forecast
advisory** for all four validator publication replicas. The pinned older rate
configuration sets 32,768 objects/hour and 8,388,608 retry requests/hour;
the current source forecast requires 34,553 and 10,947,548 respectively.
Byte capacity exceeds its forecast. The provisional waiver leaves runtime
limits unchanged and `final_acceptance=false`; it is not evidence that the
publication workload will fit. The current repository configuration has
higher limits, but substituting it mid-attempt would change the pinned
configuration. Keep R45 running and evaluate any capacity correction against
its immutable continuation boundary. [Exact advisory](peerreview/evidence/FINAL-2-R45-renewal-20260925/publication-capacity-advisory.txt).

The owner also recorded an elapsed evidence-relay horizon forecast and a
pending public census at 13:58–13:59 UTC. Both are provisional advisories;
the public census remains a final-audit requirement, and no accepted relay
result is implied. [Exact relay log](peerreview/evidence/FINAL-2-R45-renewal-20260925/relay-forecast-advisory.txt).
An independent read-only bucket listing completed at 14:07 UTC with **162,362
objects and 34,505,448,693 bytes**. In the 13:38–14:00 UTC preactivation
window it identified 12 new objects totaling 7,658,670 bytes, including the
nonaccepting publication probes. This gives a fresh baseline, not an
active-binding rate. The complete 38,669,627-byte listing remains at the
external path in the [bundle README](peerreview/evidence/FINAL-2-R45-renewal-20260925/README.md);
the [summary](peerreview/evidence/FINAL-2-R45-renewal-20260925/blob-early-census-summary.json)
contains its `census_sha256`. The initial listing attempt
hit its own three-minute timeout; a read-only retry with a 20-minute bound
completed without touching the live run.

At 14:08 UTC, the R45 owner verified precompile preparation but explicitly
skipped the governance drill at startup under its provisional continuation.
Its own log states that incomplete proof cannot pass. A read-only 14:15 UTC
check found the expected `public/governance-drill.json` absent. The release
still continues; final acceptance must independently account for this missing
drill rather than infer it from native dividend activity.
[Owner log](peerreview/evidence/FINAL-2-R45-renewal-20260925/governance-startup-advisory.txt),
[read cut](peerreview/evidence/FINAL-2-R45-renewal-20260925/governance-readcut.json).

At 14:32 UTC the same live owner completed its parallel read-only evidence
relay public census and logged `public_census_audit_passed=true` with
`pending_public_census=false`. This closes that deferred preparation check; it
does not close the other provisional findings or establish final acceptance.
The pinned worker retains its successful audit result in memory for the final
join and emits no standalone signed completion receipt. An external read-only
capture binds the exact systemd journal cursor, owner PID, signed attempt,
binary and plan; it is a log observation rather than an acceptance signature.
[Owner audit result](peerreview/evidence/FINAL-2-R45-renewal-20260925/public-census-audit-result.txt),
[bound read cut](peerreview/evidence/FINAL-2-R45-renewal-20260925/public-census-audit-readcut.json).

**Renewed-binding activation head observed 2026-09-25 14:46 UTC.** The owned LAN
RPC finalized exactly block **8,083,774**, hash
`0x6f6284b275845a8234033f46d5fec486d05294dfbbbb5cf320a0105bf473dc25`.
The read-only storage check at that head confirmed the 64 GiB hard quota,
healthy MinIO disk state and 464,104,980,480 physical bytes available. The
admin bucket-usage value was cached from 14:38 UTC; an independent current
object listing is underway. A finalized activation block makes the renewed
bindings eligible for observation, but does not itself prove the scenario's
fleet-binding assertion or start its acceptance interval.
[LAN head](peerreview/evidence/FINAL-2-R45-renewal-20260925/binding-activation-lan.json),
[quota](peerreview/evidence/FINAL-2-R45-renewal-20260925/binding-activation-quota.json),
[disk](peerreview/evidence/FINAL-2-R45-renewal-20260925/binding-activation-storage.json),
[cached usage](peerreview/evidence/FINAL-2-R45-renewal-20260925/binding-activation-usage-cached.json).
The independent full object listing finished at 14:50 UTC: **162,366 objects,
34,505,454,565 bytes**, with listing SHA-256
`2805ad102d5d5f9c85159785841ca054934eb7a8cb0fc263fcc384042bccaeb5`.
Only four objects totaling 5,872 bytes were created during the first
14:46:05–14:50:00 activation minutes. This is a fresh baseline, not a complete
healthy source epoch or a twofold growth-margin proof.
[Census summary](peerreview/evidence/FINAL-2-R45-renewal-20260925/blob-activation-census-summary.json).
The first owner observation pinned after activation was taken at 14:50:01 UTC,
finalized block **8,083,792**. It reports a healthy supervisor and **808/808
fleet bindings valid**; the raw observation hash is
`0xd1784a3c947a994d7a29bcaaf9362fd339df8f2374e03b1b078bed769ac250d4`.
Its rate proof is still not ready: complete epoch 627 has zero usage for
operator 1 and yields zero tao-rao against the 200,000-rao twice-native
threshold. The typed provisional low-usage deferral is absent at this cut.
This observation proves the renewed binding check, not acceptance start.
[Exact observation](peerreview/evidence/FINAL-2-R45-renewal-20260925/first-postactivation-observation.json)
(raw SHA-256 `1181150cc6096d64c6f365a7f71fbfae948c247f880effe4f6ee5c19672156d3`).

At finalized block **8,083,834**, after epoch 627's root-commit deadline
8,083,824, exact canonical LAN-RPC reads returned zero root commitments for
both operators. The workers closed that zero-leaf epoch without roots, as the
contract requires; the current readiness check expects the latest payout root
to match the latest signed rate-source epoch. That producer/consumer mismatch
prevents the provisional low-usage deferral for epoch 627. It is an R45
pre-acceptance finding, **not** an additional R44 exception or evidence that
R45 has started acceptance. The owner remains active while a qualified
successor fix is prepared and the next complete epoch is observed.
[Canonical root receipt](peerreview/evidence/FINAL-2-R45-renewal-20260925/epoch627-zero-root-block8083834.receipt.json)
(SHA-256 `a0286b25b729d3f71e251e2053abd48bbafe6fcb0a89ec1dd05f7896b5def0af`).

Epoch 628 crossed its boundary at block 8,084,074. A separate exact
canonical LAN-RPC read at finalized block **8,084,091** found nonzero payout
roots and artifact hashes for both operators, committed at block **8,084,080**.
This establishes on-chain commitments for the new epoch; the owner's signed
artifact match, policy-rate threshold and acceptance boundary are still
separate checks. [Epoch-628 root receipt](peerreview/evidence/FINAL-2-R45-renewal-20260925/epoch628-committed-roots-block8084091.receipt.json)
(SHA-256 `360b693569649fb3f36107f68f8cd721f1f0f46eff09dbda787d20625f7696b5`).

The owner's first post-boundary observation at 15:48:31 UTC, finalized block
**8,084,084**, matched both signed epoch-628 artifact hashes to those chain
commitments. Operator usage was 4,251,058 and 4,168,806 bytes, yielding
85,481 and 83,827 tao-rao at tier 0, each below the 200,000-rao twice-native
margin. The owner recorded the designed **provisional low-usage deferral**,
not strict rate readiness. [Exact baseline observation](peerreview/evidence/FINAL-2-R45-renewal-20260925/acceptance-baseline-observation.json)
(raw SHA-256 `a45f5ccf9e26e1e34c85ec5427f711d3c3fa6f12f2e5e5f0707a501c590f2612`).

At **15:52:25 UTC**, the owner wrote its signed
[campaign-start envelope](peerreview/evidence/FINAL-2-R45-renewal-20260925/campaign-start.evidence.json)
(raw SHA-256 `082d17a8781226c28514ce35b03c82d2597724c0e9b2adc2fca5b5cbfdbfcd7b`,
content hash `sha256:84d3d58d3e8a16cdb9e0f24b0ceb3d4032a1b578d1c515bb493456c97cd35b4f`).
It binds that exact baseline to five measured epochs **630–634**: start block
**8,084,374**, end block **8,085,874**, and terminal block **8,086,024**.
This is the release acceptance boundary, not completed final validation.
The earlier R44-LC-1 exception, R45 governance-drill gap and remaining strict
checks retain their identities while the owner runs to terminal evidence.
The signed first measured block **8,084,374** was later confirmed canonical
and finalized by the LAN RPC. Its block hash is
`0x00bb53a7d661754e4d66df69cab503eb7a905990f1a2d06d1816c4605493d5c6`
and timestamp is 16:45:36 UTC. The owner remained active as the chain crossed
it; its first post-start observation and terminal result are separate evidence.
[Start-block LAN receipt](peerreview/evidence/FINAL-2-R45-renewal-20260925/release-start-block8084374-lan.json)
(SHA-256 `65ae97503bbfa8279e5525d1850615683d260f78dc3052253c58e9a2937f9fe6`).

The independent full-bucket listing completed at 15:56 UTC and measured the
14:46:05–15:51:00 active-epoch window: **428 retained new objects and
244,049,506 bytes**. The full bucket then held 34,749,960,292 bytes under a
68,719,476,736-byte quota, leaving **33,969,516,444 bytes**. The measured
rate is about 225.6 MB/hour; doubling it across the five-hour measured release
window projects **2.256 GB**, about fifteen times below that headroom. This
supports the twofold byte-capacity margin for the observed workload. Object
listing is a lower bound if writes were overwritten, deleted or rejected; it
does not itself prove the pinned API request-rate forecast or final publication.
[Census summary](peerreview/evidence/FINAL-2-R45-renewal-20260925/blob-epoch628-census-summary.json)
(SHA-256 `81ef30d35f78234df3403c77d863d9114a38100139b6aca8aeede87b39b30191`;
full listing SHA-256 `f746220a9b5b0e1e48d329d382f89b292069dbfe99784026695cc6aaffcf4f95`).

**R44 sealed owner result, 2026-09-25 11:50 UTC — failed, retained for review.**
The original owner exited after publishing its final
[result](peerreview/evidence/FINAL-2-R44-terminal-20260925/owner-result.json)
(SHA-256 `b631ca4cd6f8fca591497770f2d066a6a568cc84fe388e08ca6e00f3ccf18c46`).
It reports **85 failed assertions of 182**, `result=fail`,
`provisional=true`, and `final_acceptance=false` at finalized block
**8,082,861** (`0xac30508c90362291e2326eb14b174a1ecc1586223766ef28e8b774e93d885fc9`).
The five release epochs reached their terminal observation, but this did not
complete the testnet qualification. The signed attempt records exit before
completion; no `complete.json` exists. The owner additionally failed evidence
publication (operator 1 HTTP 400) and process-log publication. The remaining
assertion failures retain their original identities and messages.

The operator taskworker also logged `Bucket quota exceeded` during that
publication window. The API did not retain its underlying HTTP 400 cause, so
the quota is a strongly supported shared-store cause rather than a proven
historical response body. An admin read showed the `blob` bucket's 32 GiB hard
quota already exceeded by about 122 MiB. The approved bounded repair raised
it to 64 GiB with exact admin readback and no object deletion. Both operators'
separate new preflight envelopes then passed two POSTs and exact immutable
content/history readbacks through the pinned handler. These prospective
checks do not retroactively pass R44's failed `evidence_publication` assertion.
[Quota receipt](peerreview/evidence/FINAL-2-R44-terminal-20260925/blob-quota-expansion.json),
[operator-1 probe](peerreview/evidence/FINAL-2-R44-terminal-20260925/operator1-publication-preflight.json),
[operator-2 probe](peerreview/evidence/FINAL-2-R44-terminal-20260925/operator2-publication-preflight.json).
The [post-repair object census](peerreview/evidence/FINAL-2-R44-terminal-20260925/blob-growth.json)
finds about 1.326 GB of R44 interval writes and a 0.300 GB peak complete hour.
That measured rate leaves substantial room under 64 GiB, but it is a lower
bound because the full bucket and expired bindings suppressed work. Recheck
headroom against actual R45 growth with a twofold margin before the next
release interval.

**Known exception R44-LC-1 remains narrow.** The approved bypass omitted the
terminal-effective lifecycle mutation required by the companion filter's
early-restore condition. The filter was hard-restored at finalized block
**8,082,634**, but `RestoreConditionMet=false`; the owner's
`fleet_lifecycle_fault_tail_bounded` assertion is therefore failed. This is a
documented exception to conformance, not a pass or a waiver of any other
failed assertion. The post-owner read-only diagnostic completed its external
capture after the official fleet stop: its
[report](peerreview/evidence/FINAL-2-R44-terminal-20260925/post-owner-diagnostic.json)
(SHA-256 `80b56cd76704b126ac486a431883b98054f3e6a3289693bd6015e551149b1ddf`)
has 37 checks: **14 pass, 14 fail, one named exception and eight unavailable**,
with `final_acceptance=false`. It cannot turn the original result into
acceptance. Some later source-health and receipt checks encountered the
stopped fleet or successor-plan archive; they are post-stop availability
findings, not new original-owner assertions. The
[scope assessment](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/POST-OWNER-ASSESSMENT.md)
keeps those observations distinct from the sealed R44 result. R45 planning produced
candidate `0x8bb92697db8f2164e46f6e58848d3407e509382fb61550b919f1d55391ad480e`
for epochs 628–659. Two launch-environment doctor failures occurred before
any round-7 transaction; the corrected user service adopted the same plan.
Its first ten commitment transactions finalized and passed exact postcondition
checks at block **8,083,024** (`0x504d3158e83192c4d83ccd18baba0fd4f2a0da2a425b9ab97aff64056a7f9456`).
At that earlier read cut, the rest of the renewal and successor qualification
were pending; the later renewal result is reported at the top of this file.

**Fourth independent diagnostic completed 2026-09-25 11:08 UTC.** The
clean Git-stamped `da7689f8` collector exited 0 after a read-only capture.
Its [report](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/fourth-terminal-da7689f8/report.json)
has SHA-256 `affad93c70b533e1a55f6e439dcec89f5243f5957d6ad76dfbd7ed984678d736`:
38 checks, **18 pass, three fail, one named exception, one finding and 15
unavailable**, with `final_acceptance=false`. Validator 2's signed source
capture and native application coverage now pass; its relay publication
readback remains unavailable because the exact historical request owner is
absent. Validator 1's compact input journal remains unavailable. Companion
capture and ordinary signed payout artifacts pass. Terminal assertions,
the original process-log report and fault timing fail independently. The
diagnostic evaluated those assertions at its 10:19 UTC read cut, before the
companion's later hard restore; it has not reclassified that failure. At this
diagnostic's read cut, the owner had not sealed `result.json`, so
result-dependent checks were unavailable then. The later sealed failure is
reported above.

**Companion hard restore observed 2026-09-25 after finalized block 8,082,634.**
The owner fault record now shows `fleet-lifecycle-companion-prune` restored
at its scheduled hard deadline, with no `RestoreConditionMet` proof. The
read-only [hard-restore observation](peerreview/evidence/FINAL-2-R44-terminal-20260925/hard-restore-observation.json)
has SHA-256 `c7a330fe494e0d8e8037b45f2fb060c7964bb2f98bf5b64aac5a7f7051f641f5`
and preserves the source file hash and LAN finalized head. This is the named
R44-LC-1 exception, not a successful lifecycle assertion. At this observation
cut, the owner was still active without `result.json` or signed completion;
the later sealed result and post-owner diagnostic are reported above.

**Third read-only terminal diagnostic, completed 2026-09-25 10:00 UTC:**
The clean Git-stamped successor authenticated the same signed R44 start,
checkpoint, all 44 recovery generations, observation prefix and completed
five-epoch terminal without changing the live owner. Its separate
[report](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/third-terminal-d56709aa-stamped/report.json)
has SHA-256 `38d7b721aaebc291918053635dd6ba9f014dc42c20c7b7654e457984c6185838`
and 38 checks: **16 pass, four fail, one named exception, one finding and 16
unavailable**. Both operators' current signed artifacts and both validators'
path/config checks pass. The [companion evidence bundle](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/third-terminal-d56709aa-stamped/final-inputs/bundles/validator-evidence-companion.json)
is retained with content SHA-256
`0faed897bd8de3d7dcb90f7fac827d52e3c48447580072af9949ad47b48ea3f4`;
the companion capture and ordinary signed payout-artifact checks both pass.
This improves the first diagnostic's reader-limited outcomes, but it does not
qualify the bypassed lifecycle mutation or erase the companion filter's failed
restoration timing. The lifecycle payout index remains unavailable.

The four failed groups are the terminal scenario assertions, validator-2
signed-source capture, the original process-log report and acceptance fault
timing. Validator-2's capture authenticated native/path/config inputs but
exhausted its 15-minute budget while reading retained stream data; it reported
an incomplete HTTP body and deadline errors, so native application and relay
readback remain unavailable. Validator 1's compact input journal is absent in
its retained generation, leaving its capture and dependent checks unavailable.
The owner has not sealed `result.json` or signed completion; strict acceptance,
result-dependent semantic checks and full finalization remain unavailable.
These outcomes are distinct from exception R44-LC-1 below. The diagnostic is
`read_only=true` and `final_acceptance=false`.

**Second read-only diagnostic, 2026-09-25 09:13 UTC:** A composed successor
replayed the same retained R44 terminal checkpoint without changing the owner.
Its [separate report](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/second-terminal-5c2ee88c/report.json)
(SHA-256 `5449a651f5ffb841e55e7a08303ea74afde63d3a641c1891ac50db13ca3b3d23`)
passed the signed start but failed the latest-checkpoint reader on the retained
`start_time_ticks` field. Its 30 dependent checks are unavailable, so this
second result adds a diagnostic reader defect and does not supersede the first
37-check report below. The reader is being repaired and will be rerun against
the original signed bytes. R44 remains live and has no owner-sealed result.

**Provisional R44 terminal evidence, updated 2026-09-25 08:27 UTC — not a final
result.** The signed recovery-44 release attempt completed its five 300-block
epochs at block 8,081,674 and its owner recorded the required terminal
observation at block 8,081,824. The original owner remains active; no owner
result or invalidation has been sealed. The
signed attempt is
[`release-1.0.recovery.44.evidence.json`](runs/ur-subnet-testnet-v1-attempt-4/campaign-attempts/release-1.0.recovery.44.evidence.json).

The independent [terminal diagnostic progress](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/first-terminal-206d8958/progress.json)
has passed the signed start, latest checkpoint, complete 44-generation
lineage, signed observation prefix and complete-epoch terminal checks. Its
terminal assertions fail, and the owner result and signed completion are
unavailable. The separate [terminal supplement](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/first-terminal-supplement/manifest.json)
is an unsigned external copy, SHA-256
`7a5f9209d9a4e42cc0d28c05bc90f7866f4459b57f40c54a232b24f27e0ebce4`.
It retained 1,073 source files, including all 1,000 decodable claim queues,
the authenticated observation prefix, the full process report and 144,427,524
accepted process-log bytes. Its two copy findings are the absent original
`result.json` and `complete.json`; it did not synthesize either file. The
completed read-only [terminal diagnostic report](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/first-terminal-206d8958/report.json)
has SHA-256
`c207225f44bb5962f231345770b9a8aa71c293917c596c7bda26c6efe8e7e384`
and 37 checks: 14 pass, six fail, one finding, one named exception and 15
unavailable. The six failed groups are terminal scenario assertions,
validator-2 signed-source capture, the original process-log report, acceptance
fault timing, companion evidence capture and signed payout artifacts. The
validator-1 local-intents check is a finding. Result-dependent checks remain
unavailable because the original owner has not sealed its result. R44-LC-1
does not convert any of those outcomes into a pass.
The terminal assertion group lists 23 failed assertions, including invalid
fleet binding evidence despite 808/808 bindings, no fresh applied native
weight decision or vector for validator 1, an eligible/selected/rejected
count of 0/0/0 where 202/200/2 was required, 423 `uncertain_or_failed`
claim outcomes, and a duplicate or invalid operator-1 terminal status for
epoch 620. The lifecycle-tail assertion is only one member of this group.
These are the diagnostic's reported conditions, pending comparison with the
owner's eventual sealed result.

**Additional known exception R44-LC-1 — companion filter depends on the bypassed lifecycle mutation.**
The external [exception record](peerreview/evidence/FINAL-2-R44-terminal-20260925/known-exception.json)
uses identifier `R44-COMPANION-LIFECYCLE-FILTER` for this same exception;
its SHA-256 is
`b28e5bbef7d9faa4399ceb7e0a794e08e73548b35f4bb84142fd66d9daa7dcde`.
The previously approved omission of the older lifecycle step is one noted
exception. Its dependent companion-filter restoration is this additional
known exception: the bypass leaves no terminal-effective mutation epoch, so
the companion validator-view filter's early restoration condition cannot be
proved. Keep that filter and the lifecycle
assertion failed in strict acceptance; do not infer a mutation from a
`release-handoff` stage. These two report-level exceptions remain one
dependency finding in the diagnostic count; neither is a passing check.
At owner observation 77, finalized block 8,081,388,
the old binary incorrectly restored the *target* filter with
`RestoreConditionMet=true` solely because it counted the bypass stage as
provider-paid. That flag is preserved as a finding, not lifecycle conformance.
The separate read-only diagnostic authenticated the signed terminal window;
its report remains diagnostic until the owner seals a result. The qualified
successor cleanup can restore the two
local filters after a complete signed terminal observation while retaining
`RestoreConditionMet=false`, the failed strict assertions, and
`final_acceptance=false`; it has not been installed in R44.
An [independent terminal capture review](/mnt/data/sn-testnet/qualification/r44-terminal-exception-review-20260925/REVIEW.md)
documents the exact read-only commands and evidence limits.

This exception does not cover other failures. At the terminal supplement cut,
all 1,000 miners had `last_discovered=617`; no queue entry exists for accepted
epochs 618–620. Epoch 616 still has 359 `submitting` entries and epoch 617
has 47. The retained process report contains 15 blocking acceptance-scoped
rows: 10 `exit-gap-timeout` rows (14 events), two validator
steering-attempt rows (24 events), two steering-continuity rows (three
events), and one TLS handshake timeout. The later final diagnostic process
scan records 26 steering-attempt events across the same two rows; this is a
later read cut, not a rewrite of the supplement. The earlier read-only
[interim inventory](/mnt/data/sn-testnet/qualification/r44-live-triage-20260925/inventory.json)
at 07:17:59 UTC records SHA-256
`017fb1603eea127593b2a6e1f0f6115d86d1a9554640d9fd2a452cb219aafb9c`
and hashes the signed start, all 1,000 claim queues, observations, faults,
process logs, and watcher progress. Its
[read-only assessment](/mnt/data/sn-testnet/qualification/r44-live-triage-20260925/ASSESSMENT.md)
distinguishes the known Connect/transport repairs from historical gaps they
cannot prove repaired, and identifies expired bindings, absent positive native
weights, below-threshold rate readiness, and missing current-window claims.
Preserve both raw cuts and compare them with the eventual owner result. No skipped check is
reported as passed; the full campaign, production interval, accounting replay
and go/no-go decision remain open. See [the active execution record](../FINALIZE-ACTIVE.md).

**Latest update, 2026-09-24 21:17 UTC:** Two R43 startup retries also stopped
before a signed interval. The first failed executable Git attestation because
its binary was built before the fix was committed. The second used committed
revision `ce5f45a8`, passed the corrected operator resource preflight, and
found a retained runtime-manifest reader that used the selected source-role
config path to infer the original validator-2 seed path. The sealed original
manifest is intact; the fleet is stopped, no recovery-43 attempt exists, and
the reader fix is being qualified. See [FINALIZE-ACTIVE.md](../FINALIZE-ACTIVE.md).

**Current status, 2026-09-24 21:04 UTC: R43 recovery startup failed before
signing an interval.**
After R42's signed early failure, the fleet was stopped with on-chain state
preserved and an owner-signed native source-role overlay selected for validator
2. Both native slots were checked at finalized runtime 471 using the approved
consumed-interface profile. The supervised R43 runner authenticated 9,859
retained receipts with zero failures, then stopped at a stale operator overlay
resource check before topology startup. It has not signed a new acceptance
boundary or completed a release interval. The overlay expects `geolite2.mmdb`;
the pinned server requests `ip-ipinfo.mmdb`, which exists in the live config
repository.
The retained R42 active-fault record must be restored by the recovery driver.
The exact service, binary hash and selection receipt are in
[FINALIZE-ACTIVE.md](../FINALIZE-ACTIVE.md).

**Current status, 2026-09-24: R42 started its real release interval but failed
before terminal; `final_acceptance=false`.** The signed attempt
`campaign-attempts/release-1.0.recovery.42.evidence.json` began at finalized
block 8,077,774 (epoch 608) and covers five 300-block epochs through block
8,079,274, with terminal block 8,079,424. Independent read-only terminal
diagnostic watchers use the LAN RPC at
`192.168.1.162:9944`. The inherited UID-churn exercise was explicitly
bypassed because the chain would prune UID 1 rather than the planned UID 7;
no churn transaction was sent. R42's owner exited at 20:42 UTC after observing
through epoch 609, before terminal block 8,079,424. Its signed result failed
with nine blocking process-log classes, 914 open anomalies, and an interrupted
`miner-swarm-8` restart fault. The missing provisional deferral for validator
steering continuity caused the heartbeat to reject the full class set early.
Read-only watchers still collect terminal diagnostics, while recovery fixes
and a future-boundary R43 are prepared. This exception cannot be
reported as a passing strict acceptance check. The exact current state and
watcher artifact path are in [FINALIZE-ACTIVE.md](../FINALIZE-ACTIVE.md).
An independent finalized read at block **8,078,129** (hash
`0xea7d377b2479e183f1900b1df979b900916837be9dce86a2fb142dd1627793e7`)
found `policyCount()=3` on coordinator
`0x8e7d2f9a77fec95c7e4875b0bd858d5de2b6def8`. The three stored policy
hashes are `0x2cbb4cdd991d9463f321f0de7f7bd77d028da611c2b5eb656626eac31ab5a356`,
`0x1526b242cf4908cc31f7e58006664bce6064003c69fd8452eab2d49122fef277`,
and `0x41f0c7efe7e1b23b2fd22dac9352ca18be48d2e4d1fb5b41ce89660bc899b0dd`.
The production scheduler's current two-policy admission is therefore a known
next-stage blocker in the live executable. A narrow authenticated-history
correction is on main as `402e6b1b` and has focused normal/race evidence;
the combined policy-history, provisional handoff, release-gate and
postcondition selectors also passed on current main normally and under race
detection. Production adoption still requires R42's signed terminal result
and restored fault inventory.
R42 validator 2 later restarted after its current source could not authenticate
an occupied native commitment slot. At finalized block **8,078,196**, both
`CommitmentOf` and `LastCommitment` matched the old source generation's applied
intent (finalized block **7,987,774**), while the active generation-1 intent
file was absent. The exact read-only RPC keys/results and source hashes are in
the [native-slot evidence bundle](peerreview/evidence/FINAL-2-r42-native-slot-20260924/README.md).
The validator's refusal and restart remain R42 findings; a predecessor-proof
repair is pending for later continuation.

The following September 16 checkpoint is retained as historical evidence; it
does not describe R42's current state. The corrected
provisional topology admission passed at **19:57:09 UTC on September 16**. A
qualified incremental release retry subsequently reopened the signed durable
successor and reauthenticated all 4,674 retained receipts. It reached the real
LAN execution reader, then stopped before preparation or any scenario action
because testnet upgraded directly from runtime 461/1/1 at block **8,020,753**
to 463/1/1 at block **8,020,754**. No transaction or spend was pending; the
plan, journal, configuration, roles and signed successor are unchanged. Both
validators later exhausted their five restart attempts against the old 461 pin;
the other 31 processes did not restart. A compatibility fix and retained-state
topology resume are in progress against `192.168.1.162:9944`. The actual release
campaign has not yet opened a measured phase. The approved **6,000-alpha reserve repair has finalized**
and reached
**65.5997247163%** at the retained preparation snapshot. The latest complete
LAN census, at block **8,015,417** on September 16, shows **64.9994918065%**:
above the 60% operating floor and below the 65% repair target. The
user has approved **205 EVM TAO within 225 total TAO** for the required fleet
renewal; the 37,250-alpha lifetime limit and 6,000-alpha per-repair limit remain
unchanged. Earlier checkpoints below retain their historical approvals and
failures; they do not describe the current repair or allowance status.

**Latest recovery checkpoint, September 16, 19:40 UTC:** driver SHA-256
`b85ab8529118c9bc9f2848865882dfa1291651a1c08de8ad28bffcb72857707d`
contains the combined campaign-succession, retained-context rendering and local
intent corrections. Its 19 selected test roots pass normally and under race
detection, while seven causal controls reproduce exactly their intended
failures. This qualification does not establish campaign or on-chain acceptance.

The first corrected live resume authenticated all 4,674 receipts, started 33
processes and exited zero, but its outer wrapper rejected a single transiently
unhealthy miner snapshot and rolled back. The second corrected resume ran from
**19:07:14 through 19:25:32 UTC**. Its product body again passed with all 4,674
receipts and a 33-process, zero-restart generation. Across 24 five-second
samples, both validators and every non-miner service were healthy in all 24;
each miner was healthy in at least 22, and nine samples were fully 33/33 healthy.
Both operator replicas emitted the corrected startup-authority marker and the
refusal scan was empty. The wrapper timed out only because no caller invoked the
internal path that emits the separate `local-readiness` marker. Its rollback
passed, and retained contexts, executable and release-lock bytes were unchanged.

The third wrapper records local readiness without blocking on it and permits
only miner-swarm health flicker. It still requires stable live process identities
and OS start ticks, zero restarts, continuously healthy validators and other
critical services, every process observed healthy, at least three full-health
samples, both startup-authority markers and no refusal. Terra medium passed
**26/26** deterministic producer cases, including replay of both real 16-sample
windows, and **52/52** campaign-consumer cases. The separately qualified 43-case
fail-closed finalizer and the full campaign workload are byte-identical. Retry
three completed under request SHA-256
`0c3b723efdb34dcfb609b8be2352867886fd10069e10352a87ec3fce9458fb9f`.
Its 16 samples covered 82 seconds, with seven 33/33 samples, every process
observed healthy, continuously healthy validators and critical services, stable
PIDs and OS start ticks, zero restarts and no refusal. The result SHA-256 is
`4f2740bd3b9cb542636c75f42608a232c7c90643176d6bbe85497a2cedebeae6`.

The release-candidate owner ran from **19:58:56 through 20:00:05 UTC**. It
authenticated all **4,674** retained receipts with zero failures and wrote the
signed successor attempt for run
`20260916T195955.196642218Z-release-1.0`, SHA-256
`9aa94b86effb8aafc37bee904ff1e909a14edc6630a67f0a5588728ad7900417`.
That record is immutable and is selected first on the next invocation, so this
failure does not restart setup or create another attempt. Before preparation,
the campaign rejected protected publication capacity. The source-capacity test
had exercised the public route's 60-second poll, while the LAN-only runtime
correctly renders a 15-second validator poll. Objects and bytes remain within
their configured ceilings, but the worst-case retry forecast increases from
2,555,000 to **6,012,260 requests/hour**, above the protected **4,194,304**
ceiling. No transaction or spend was pending. A narrow provisional-only
advisory is being qualified; strict admission and hard runtime quotas remain.
No measured campaign result is claimed yet.

**Runtime-463 retry checkpoint, September 16, 20:43 UTC:** the capacity change
passed **21/21 normal** and **21/21 race** roots, with one exact pre-fix causal
failure. The incremental retry wrapper passed **81/81** deterministic cases.
Request SHA-256
`ac01e73b1833f2232ac8a0da2474b543749dda4556bb85f77d474a51b7064204`
ran from **20:43:00 through 20:45:18 UTC**. It authenticated all retained
receipts and recorded that the resumed successor had no pending transaction or
spend. The execution reader then rejected exact runtime identity 463/1/1;
body and outer exits are one, and result SHA-256 is
`e688eaffe3153b45d0a3b55cd06f0f25d745baa34d8da97d10a5a94c2c35a002`.

The LAN archive proves the transition boundary: block 8,020,753 is the reviewed
461 artifact and block 8,020,754 is 463. Runtime 463 retains transaction and
state versions 1/1 but has different exact code and metadata. Its code is
2,536,695 bytes with BLAKE2b-256
`0x9745e3f66053c3c7cb30ea45b88c66438b5076da78154f477e8660b0ded43869`;
its 338,396-byte metadata hashes to
`0xe9af0fcab804e08c0f6cc2c13715b1e366a916eda6a61aec6fb2601bc2a66b4c`.
The same capture rechecked the known 461 hashes, so the hash procedure is
anchored to the existing catalog. The failed retry changed only live supervisor
state among the wrapper's watched state files. It did not replace the signed
successor or begin release/production measurements.

Validator-2's original process first ended on a transient post-checkpoint RPC
timeout at block 8,020,753. Validator-1 later exhausted its steering retries
after an operator timeout and 463 identity refusals. Their replacement processes
all rejected the old 461 pin, leaving each validator at five recorded restarts;
the other 31 services stayed on their original zero-restart processes. The
supervisor has no supported per-child retry reset, so the recovery must perform
a controlled stop and retained-plan provisional resume after the compatible
binary is qualified. Durable validator ledgers, proofs, statistics, setup
receipts, plan/journal and campaign successor remain inputs to that recovery.

**Previous recovery checkpoint, September 16, 17:02 UTC:** the provisional resume
authenticated all **4,674 retained local receipts**, completed the setup prefix,
launched 33 processes and adopted the live topology. The release-candidate
command used `192.168.1.162:9944`, then closed with body and outer exit one on
`open durable release-1.0 attempt: campaign succession requires the strict
approved deployment owner`. The executable and release lock are unchanged;
only the watched supervisor-state digest changed. This exposed an unconditional
provisional-mode exclusion before the existing exact-plan, custody, journal,
signed-predecessor and failed-result succession checks.

Validator-1 restarted twice and validator-2 once before command closure. Their
initial terminal publications received HTTP 403 because regenerated operator
staging configurations omitted the explicit provisional retained-context
authority and kept an ordinary 16,384-block discovery window that had already
expired. A replacement PID being present is not proof of terminal publication.
Both exact failures are retained while narrow succession and render corrections
and deterministic adjacent tests are prepared. The 33-process topology was not
discarded or relaunched after the failed command.

The receipt-authentication optimization is qualified and integrated locally.
Its affected scope passes **14/14 normal roots by composition** and **14/14 race
roots**; all three causal controls reproduce their intended failures. It caches
only immutable source-plan decoding and one journal index within a read-only
invocation, then checks a final journal-equality fence. The separately built
candidate containing this optimization was not used by the active command.

**Previous recovery checkpoint, September 16, 16:38 UTC:** the previous strict
startup ended at **15:41:52 UTC** after the 30-minute fresh-proof readiness
window expired. Both validators were replaying retained history; no fresh
complete validator/operator trails were observed. Native cleanup stopped its
33 processes. Eight operator transactions nevertheless finalized successfully;
their actual fees total **0.016528555545105692 EVM TAO**. The
[recovery evidence](peerreview/evidence/FINAL-2-provisional-recovery-20260916/README.md)
includes all eight receipts and their transaction hashes, plus the local
signature-restoration result. Recovery restored the original signed bytes
without signing or submitting new transactions.

The user's instruction to finish the actual run now permits explicit
provisional continuation with deferred historical replay and acceptance checks.
The patched driver retains the approved plan, spending limits, signed history,
transaction reconciliation and owned LAN routing. It can adopt a healthy fresh
topology without claiming missing proof trails passed. Its 17 affected/adjacent
test roots pass normally and under race by composition; five causal controls
reproduce the original failures. A separate runtime-render fix passes 18 roots
in both modes. The original fixture failures remain recorded. Neither source
publication nor another full preparation pass gates the provisional run.
At approximately **16:31 UTC**, the active resume finished authenticating
**4,674 retained local receipts**. Services and the release-candidate handoff
subsequently completed as described above. Actual campaign observations and
production cadence remain required; provisional execution does not establish
final acceptance.

**Previous recovery checkpoint, September 16, 13:23 UTC:** runtime 461 is qualified
at `8edb3167a6261bfd82ecbc5f3c0ac2c787beec7c` and integrated locally. All
**103 affected tests pass normally and under race detection**. Four causal
restorations reproduce **nine expected failures and nine passing controls**.
The exact-Wasm probe passed. Root verified raw results, compiled membership,
actual exits and unchanged inputs. The batch also fixes retained runtime-460
companion approval handling; tests preserve original bytes, budgets and signer
authority. A stale positive test fixture was corrected before compilation.

The [qualification bundle](peerreview/evidence/FINAL-2-runtime461-qualification-20260916/README.md)
contains those results and source/artifact provenance. Its read-only lock
preview passed, yielding SHA-256
`aad35e8488e48190071889b3dec47c184ed9d2deedc30c44e3b52ee6f17afd84` with only the
two expected observed source-digest changes. Publication and a matching final
executable precede native recovery. All prior signed history and approvals are
retained. The managed campaign remains unexecuted; this qualification does not
establish final acceptance.

**Previous recovery checkpoint, September 16, 12:03 UTC:** the startup and direct
campaign handoff are published at `1860261`, and the matching executable built
successfully with SHA-256
`114bede0b30a9bc9fdb946f3075d1e1f3ff9b3e36daf57d1c084f6144e43bb07`.
Software planning passed. After a detached-checkout prerequisite was repaired
without changing source or executable bytes, setup ran **11:43:40–11:57:57** and
completed all **4,673** carried-action checks. It collected **nine failures**,
all caused by unreviewed runtime **461/1/1**. Eight hard preparation checks
passed; one failed with those nine errors. Body, outer and joined exits are one.
Only the saved plan and redacted configuration changed; the journal, supervisor
manifest/state, public identities, executable and lock remained unchanged.
The new software approval is
`0xf6e8c46e6a6a79c7c67deb8304e387f4bc9821ad96513c0ab6851d954d0d3bc6`.

A separate finalized LAN observation confirmed runtime 461 at block
**8,018,145**, native hash
`0x43093d12230005ca09a38835fb1506e7b018fb52233597e68ad50c440c2d7272`
and EVM hash
`0xe7570097180720fb03a9d6cf15b7244bdd34e0ec03e9307f7eaaea936b945889`.
This uses the owned node with `independent_rpc=false`. The runtime check stopped
before selecting a continuation END or running native capture/import. Four
temporary helpers passed readiness and then stopped with all actual child and
owner/join exits zero. The [closed preparation bundle](peerreview/evidence/FINAL-2-runtime461-preparation-20260916/README.md)
contains exact RPC responses, failed setup output, state comparisons and teardown
receipts. Runtime 461 compatibility review and affected qualification are in
progress. The managed campaign has not started; earlier successful qualification
and approved limits remain retained.

**Previous recovery checkpoint, September 16:** strict managed resume
session **92055** failed at **10:20:25 UTC**, with body, outer and joined exits
all **one**. All **1,000 fleet checks** and **4,673 carried-action checks**
completed. The new supervisor, PID 3676320/start ticks 184001647, reached
33 healthy processes with zero restarts, but the four validator/operator proof
domains did not produce fresh completed trails before the five-minute owned-node
semantic-readiness deadline. The exact error was `release topology semantic
readiness timeout: every validator must complete a fresh verified trail through
every operator`. Cleanup stopped all 33 processes. The last process-log gate
scan, at 10:20:10, had no findings; later shutdown logs are separate evidence.
The plan, simulator journal, configuration, public identities, executable and
release lock are unchanged. The release-candidate campaign did not start.
The [closed failure receipts](peerreview/evidence/FINAL-2-managed-readiness-20260916/README.md)
retain the actual failed result and changed supervisor state. Root verified all
27 manifest payloads; the seal is
`81fd7d5c8140fa02bb58184cdb562906ad7386dcd33a078316f40d42a3a2bc2d`.
The validators' shutdown diagnostics place both inside retained settlement-history
replay when the parent cancelled them. The bounded startup correction is
qualified below; completed historical checks remain retained.
The complete operator census found **14 new signed attempts** during this
generation, all with canonical status-1 receipts below native/EVM finalized
block **8,017,664**. They comprise six emission deferrals and eight missed-root
finalizations, with no payout root created. Their actual fees total
**0.029033073172513564 EVM TAO**; maximum signed envelopes total
**0.090515040996642416 EVM TAO**. The original validator histories are unchanged:
zero intents for validator 1 and three for validator 2, ending at native epoch
1,405. No new decision or preparation for epoch 1,488 occurred.
At **10:35:53 UTC**, root restored the 14 original 178-byte signatures by
exclusive creation, preserving all 2,272 existing RLP files and all six watched
state files. The recovery submitted no transaction and changed no database or
journal status. Body and joined exits are zero. The complete retained transaction
union now contains **2,532** signatures; all previous 2,518 remain intact.
The closed census is in `managed-readiness-recovery-census-20260916-r1`, with
35 verified safe payloads and manifest SHA-256
`b9070c8ee744b2cfc54d8d24ba8baa38144a3f4aadd1306300b1edc0e5c9da18`.
The separate restoration capture is
`native-recovery-20260916-r4/signature-restoration`, with 16 sealed payloads and
manifest SHA-256 `de3e77ec83fa8b3abc929492a33c23c97f2e8d3896a97c951c4db340ed9f1c95`.
Both paths are under `/mnt/data/sn-testnet/qualification`. The
[portable census, restoration and cause evidence](peerreview/evidence/FINAL-2-managed-recovery-census-20260916/README.md)
preserves all original manifests and exact safe copies. Root verified its
67 payloads and manifest SHA-256
`6425b3cfdf0d6fd993a81aeacd73931d8c299982da08d9ede9bd9124a6f4cbcb`.

The narrow startup correction is frozen at
`8270992eb8fb2b1599a29271ec44426379007306`: retained strict-history startup gets
the existing 30-minute allowance on every RPC route, and cancellation is checked
before readiness admission. Fresh-proof, health, generation, restart and log
requirements remain active. Terra's 11 affected roots pass normally and under
race, with body, outer and joined exits zero. Each of two causal variants
reproduces exactly its pinned failure with the other ten roots passing.
Root independently checked the event streams, selected compiled membership,
exits and unchanged inputs, then fast-forwarded primary to this qualified
revision. Physical deployment and the active executable remain on `aeda6abb`;
the patched managed startup has not run.
The [closed startup qualification](peerreview/evidence/FINAL-2-retained-startup-qualification-20260916/README.md)
preserves the successful results, causal controls and earlier compiler refusals.
Root verified all 70 payloads; manifest SHA-256 is
`f4c67339990c97bc2cac866bc049074291b6f869891d01a86361e06ce6fd4577`.

The four initial compiler bodies exited zero but their wrappers exited 126:
a generated literal `$capture/` output path created untracked artifacts inside
the isolated sources. Those binaries were mistakenly cleaned up before reuse;
the corrected builds reproduced their exact hashes and passed all input checks.
The original refusals and cleanup inventories remain recorded. No test body
ran from the refused captures. The harness now explicitly preserves artifacts
before cleanup and permits separately evidenced reuse after capture-only errors.

The continuation timing review identified repeated preparation between strict
resume and the separate campaign command. The maximum supported fresh span is
8,065 blocks, leaving 495 above the unchanged 7,570-block campaign allowance.
Measured prior overhead leaves 32m49s for retained replay, campaign preparation
and handoffs combined. This is a timing estimate, not proof of completion or
authority to reduce required observations. A narrow explicit same-owner handoff
from successful strict resume into the full release-candidate campaign is now
qualified at `e109ac35c5ea5ff5006040c2987118e99627e863` and integrated locally.
It retains all campaign checks and the completed readiness tests. All 14 new
or adjacent affected roots pass normally and under race. Normal acceptance
composes four retained passes, nine corrected-capture passes and one omitted
root; the causal control reproduces exactly three intended failures and three
passes. Root independently checked raw events, unique root membership, actual
exits and unchanged source/dependency/binary inputs. The
[closed handoff qualification](peerreview/evidence/FINAL-2-resume-campaign-handoff-20260916/README.md)
preserves the original launcher-generation, compiler-directory, fixture-directory
and selector errors separately. No production source change was needed for
those capture errors, and valid binaries and passing roots were reused.

The [read-only lock preview](peerreview/evidence/FINAL-2-retained-startup-release-20260916/README.md)
ran **11:09:05–11:09:07 UTC**, with root session 84281 body/outer/join exits zero.
All watched state, executable, installed lock and source fences remained
unchanged. Its exact candidate changes only the SN source hash and has SHA-256
`42a48da4d4fd267c496b8558cd9838f68b3f5e4117a9e681352129069df2df22`.
Those bytes are now installed in the primary checkout. Matching publication,
build, plan adoption and the actual combined campaign remain pending.

**Earlier recovery checkpoint, September 16 at 09:41 UTC:** both preparation
fixes and their evidence are published in SN release
`aeda6abbd2dc0abc92bb0f60975cf89b509e8017`. The matching executable has SHA-256
`8fc61a65cd0524413a7ba70c61bcdb15962fa87ad7ab347b653abb27f8913f0b`;
its build exited zero with unchanged source inputs. The native read-only plan
revision passed at **08:02:31 UTC**. The complete lossless diff preserves all
**4,733 actions**, approved limits, spending, both renewals, continuation,
custody, config and policy. Changes are limited to release/plan identity,
ancestry, generation time and fresh finalized head/balance observations.
The proposed plan is
`0x49ddbc495a51c7c089ed5838299d6d65fb40cfb9b35be3ee38cd4ca1de7aa876`.
The [completed build and plan-review evidence](peerreview/evidence/FINAL-2-preparation-adoption-20260916/README.md)
contains exact raw receipts and the full lossless diff; root verified its manifest.
Prepare-only setup/adoption ran **08:12:47–08:27:00 UTC** and exited **zero**,
with `ready=true`, `prepare_only=true` and `stopped_before_actions=true`.
All **4,673 carried-action checks** completed. Only the saved plan and redacted
configuration changed; journal, supervisor, public identities, executable and
lock hashes are unchanged. All nine hard preparation checks passed;
launch-runtime-inputs is explicitly deferred to resume. Its capture is in
`/mnt/data/sn-testnet/qualification/native-recovery-20260916-r3/setup-revision`.
The four temporary helpers passed readiness. The APIs returned 200 at `/status`;
their initial incorrect `/healthz` probes are retained as diagnostics. These
helpers were orphaned processes, so shutdown was witnessed through exact
PID/start-time and listener observations; terminal exit codes are unavailable.
A fresh finalized LAN observation at
**08:27:57 UTC**, native block **8,017,083** with runtime **460/1/1**, selects
continuation end **8,025,143** and full-work start cutoff **8,017,573**. The
required work remains **7,570 blocks**. Continuation capture ran
**08:31:32–08:46:41 UTC** under root session 52913 and exited **one**, without
emitting a plan. The exact error is `renewal gas accounting is incomplete: role
operator-1-root nonce 106 has no retained signed transaction`. Captured state,
executable and release-lock hashes are unchanged. That transaction is among the
four previously confirmed partial-start writes. The complete census of both
operator databases verified **230 signed attempts**, including replacements and
cancellations: 226 were already retained and exactly four were missing.
Create-only restoration completed at **08:57:55 UTC**, adding four original
178-byte RLPs while preserving all 2,268 existing files and the six watched
state files. No transaction was submitted and neither journal nor database
status was changed. The [portable signature recovery evidence](peerreview/evidence/FINAL-2-signature-recovery-20260916/README.md)
retains the closed failure, sealed census and separate restoration receipt.
The four known maximum envelopes total **0.025635775234311880 EVM TAO**, while
their previously confirmed actual fees total **0.008264277772552846 EVM TAO**.
A fresh finalized LAN observation at **08:58:59 UTC**, native block **8,017,238**,
selects continuation end **8,025,298** and full-work start cutoff **8,017,728**.
The retry ran **09:00:03–09:15:28 UTC** under root session 35694 in
`native-recovery-20260916-r3/relay-capture-r3` and exited **zero**. All six watched
state files, executable and lock remain unchanged. The emitted successor is
`0x17e49d00a7ce6aafac856e81a4ccf9eb37e4714ba7570a24cfa1c97a2d941f37`;
its exact output SHA-256 is
`e40de369ee48a5bbc7c4b752295187cdc1f44e49117d2d324744f62b3ad90a48`.
Root's complete comparison found exactly **27 changed paths**, confined to the
continuation window/source observations, restored-transaction census/nonces,
plan identity and ancestry. Astra's independent token-preserving comparison
agrees; all actions, limits, spending, renewals, custody/config/policy and signed
ledger history are unchanged. The [portable capture and complete review](peerreview/evidence/FINAL-2-relay-capture-r3-20260916/README.md)
retain the original receipts and all 27 changes, with large-file omissions hashed.
Exact import ran **09:19:38–09:35:50 UTC** under root session 30705 and passed,
reporting **zero chain transactions**. Only the watched plan changed; its saved
bytes equal the captured successor exactly.

The finalized LAN schedule at **09:36:23 UTC**, block **8,017,425**, selects first
native epoch **1,488**, spanning **8,017,451–8,017,810**. At selection, 386 blocks
remain through that epoch and 303 until the full-work start cutoff. History
capture ran **09:37:02–09:37:28 UTC**, exit zero, with all watched state unchanged
and both original validator intent prefixes preserved. Its exact 2,201-byte
bundle is retained under the state-owned `history-adoptions/` directory with
SHA-256 `f2a7e1a24fc02cb9fbc3ee85af757b5795982ec42731f2b9fd34564485c479c9`.
Temporary-helper teardown completed at **09:40:08 UTC**: exact identities were
checked before signals, all four processes disappeared and all six listener
ports closed. The teardown owner joined zero; helper exit codes are unavailable.
Two pre-signal wrapper/preflight failures and the original self-referential
manifest are retained separately from the corrected closed receipt manifest.
Root independently rechecked process absence and closed ports, then launched
strict managed resume at **09:41:17 UTC**, session **92055**. Its later semantic
readiness failure is recorded above. The release campaign remains unexecuted.
The [closed launch handoff evidence](peerreview/evidence/FINAL-2-launch-handoff-r3-20260916/README.md)
contains import, complete history/schedule records and witnessed helper teardown.
Root verified all 93 manifest entries; its seal is
`be4fcaaf28ff19df7c9834a680f4abab605d1256daedfdbd20301ab5abe4abc3`.
It excludes the running managed resume and the unstarted campaign.
Reuse the completed
affected qualification below; no new full preparation test cycle is required.

**Earlier recovery: the software plan revision stopped at 02:48:30 UTC on September 16.**
The qualified journal patch and matching executable are ready, but the native
planner requested another 471,808,849-alpha-rao repair when revising the
software identity. Current and superseded liabilities already consume the
approved 37,250-alpha lifetime limit. This read-only command exited 1 with
unchanged state, executable and lock bytes; no successor plan or transaction
was created. The completed repair's historical target proof remains valid.
A bounded correction now retains verified repair history during an exact
software-only revision while still checking the current operating floor.
Qualification passes **28 affected roots per mode**, normally and under race,
by composing retained results with three corrected test roots. The original
planner reproduces **three expected failures and three passing controls**.
Failed fixture invocations remain recorded with their actual exits. The
matching release is published at `541e13cfbe968704fb74c4001506853d2529bfdd`;
its executable SHA-256 is
`45455c69687d88287a200979ee914677bf0f39e000274e7fc5fb618339c0fc8a`.
The native retry passed at **04:06:54 UTC**, preserving all **4,733 actions**,
limits, spending and renewals without adding a reserve repair. The new plan
`0x0d24a3f1dfc8ea5bc6a2f59c80a7580a761d9e3410dd4833bda3843304ce6f86`
is now saved. Setup/adoption completed at **04:30:51 UTC** with
`ready=true`, `prepare_only=true` and `stopped_before_actions=true`.
All **4,673 carried-action checks** completed; only the saved plan and
redacted configuration changed. The journal and supervisor files are unchanged,
and no setup transaction was submitted. The launch-runtime-inputs check is
explicitly deferred to resume. The subsequent relay capture ran from
**04:35:04 to 04:48:52 UTC** and exited **1**, with unchanged state, executable
and lock bytes and no plan output. Both operator APIs timed out after 30
seconds streaming the same metadata object; the CLI reported incomplete
authenticated EOF. Both APIs subsequently returned the complete matching
157,602-byte object in under one second. Source review found no deterministic
defect; the original stall's underlying cause remains unproven. A new capture
started at **04:59:42 UTC** with the same qualified build and fresh end block
**8,024,100**, and passed at **05:14:43 UTC** with unchanged watched state,
executable and lock. The complete raw diff preserves all actions, spending,
renewals and signed history. No timeout or authentication rule changed.
Import then exited **1 at 05:15:55 UTC**, before mutation: root's report-only
publication advanced GitHub main beyond the executable's revision. The physical
checkout now matches `0fd7ffc0`; all Go source, modules and release-lock bytes
are unchanged. The matched executable, SHA-256
`d50a4612ed4bd34838bd4a5b24f91b79e5b3d1ff55f76198178c26a76234dfbf`,
built successfully at **05:21:25 UTC**. Reusing the exact captured plan,
import passed at **05:39:02 UTC**, adopting
`0xb7fd2eb5030f73b424b3449d21302e6b3d17cd85142f4ca61b18aa1e0c2b5b17`
with **zero chain transactions**. Only the saved plan changed; the journal,
supervisor files, redacted config and public identities are unchanged.
Strict history capture passed at **05:41:45 UTC** with all watched bytes
unchanged. At finalized native block **8,016,244**, the actual schedule selects
first native epoch **1,485**, blocks **8,016,371–8,016,731**. The exact bundle's
SHA-256 is `e556044d5cf4b584856df3dc7c0a199a582ce14130f5818c09190ac54673eb53`.
The four temporary helper tool sessions were recorded as joined with exit 0
in the r2 `temporary-services/STOPPED.json`, with their original PIDs absent.
Separate raw helper-join results are not in the portable continuation bundle. Managed
resume started at **05:47:36 UTC** and exited **1 at 06:29:05 UTC**. It processed
all **4,673 carried-action audits**, rendered runtime inputs and started the
managed topology. At **06:28:53 UTC**, the process-log gate recorded **four
blocking classes** in the new generation: each of the two operator taskworkers
emitted **two unclassified errors and one warning**. Cleanup stopped all **33
managed processes**. Plan, journal, redacted config, public identity, executable
and lock bytes are unchanged; the watched supervisor files changed. Unchanged
deployment-journal bytes alone do not establish that every background actor
made zero transactions. Read-only reconciliation subsequently found **four new
operator EVM transactions** in the retained databases. LAN receipts at 06:59 UTC
show all four succeeded in canonical blocks **8,016,488**, **8,016,489** and
**8,016,491**, below finalized height **8,016,641**. The hashes are
`0xa62a69f38c314fce4efdc6cf9084ae5a2efa7178faca0ba25b0c63bd8e5368d9`,
`0x50cc7f2745b22052f91a3ce124f18fcbbe0fb0d297f59a9f3f9d2bd33e702dd8`,
`0x62a2464cf084c69c81040d1374abcc20c8a99bb34e4788bd6e8b34cbe804d1df`,
and `0xe95f3f78dad211d12ced0583a47d9af610b9699d68ab08f73b2fc5c3b7acabd8`.
Both signers have finalized and pending nonce 107. Their retained database
states include one mined and two broadcast entries; normal reconciliation must
retain those exact signed attempts and learn their finalized outcome.
The [startup transaction evidence](peerreview/evidence/FINAL-2-startup-transactions-20260916/README.md)
preserves the read-only SQL projection and original LAN RPC requests/responses.
The first two transactions call `finalizeOperatorEpoch(316,1)` and
`finalizeOperatorEpoch(316,2)`, each producing `RootMissed(316,noId,0)` and an
uncommitted-root finalization. The last two call `deferMissedEmission(402,2)`
and `deferMissedEmission(317,2)`, recording zero-funded missed boundaries while
leaving stake for a later timely capture. They create no payout root and do
not capture a multi-epoch stake delta. Their total actual gas fee is
**0.008264277772552846 EVM TAO**. These startup transactions do not establish
a completed campaign epoch; `independent_rpc=false`.
The prepared release-candidate command was **not run**. The taskworker failures
come from the full production backend workload: geolocation certificate-pin
rotation errors and fiat-payment warnings for synthetic accounts. An explicit
operator workload profile is implemented in frozen SN `8e6d56b5` and server
`6752a8df`, including retained queue and post-hook handling. All **30 affected
roots pass normally and under race**: nine simulator, eight server task and
13 taskworker roots. The complete controls reproduce **seven expected failures
and five passes**. Initial service censuses refused before tests; a subsequent
disposable-service launcher cleaned up before joining its children. Those
refusals are preserved. Corrected process ownership allowed the same compiled
binaries to complete all selected bodies. Service setup and cleanup each exited
zero; the enclosing owner exited one because it aggregates the expected control
failures. Accepted composition is recorded separately from that raw exit.
The preparation-cost candidate `351ece79` now qualifies **32 roots per mode**:
30 retained unaffected passes plus two passing roots on fixture correction
`d52028de`, normally and under race. The original 31-pass/one-failure runs and
the control's two expected failures/six passes remain retained. The exact
production and fixture changes are integrated locally, with both fixes composed
at `dc90e4c`. The
[qualification evidence](peerreview/evidence/FINAL-2-preparation-fixes-20260916/README.md)
preserves the original failures, raw test events and exact reuse mapping.
Neither fix is deployed or claimed operationally complete.
The read-only release-lock preview passed at **07:39:57 UTC**, with unchanged
deployment state and no transaction. The installed candidate changes only the
SN and server Go source hashes; runtime, contracts, dependencies and all other
lock fields remain unchanged. The lock SHA-256 is
`bf417189d4c0a62f8116606f84f9c5509b3afe2dab611429c9b781998b3198fc`.
Publication, the matched executable and native recovery retain their own
pending results. [Exact release-lock receipt](peerreview/evidence/FINAL-2-preparation-release-20260916/README.md).
Startup and the soak remain pending; no passing readiness or campaign result
is claimed. Root verified all 115 entries in the private failure bundle;
the [public failure projection](peerreview/evidence/FINAL-2-managed-start-failure-20260916/README.md)
retains exact counts, line hashes, original byte comparisons and terminal exits.
The capture/import/startup receipts are retained locally in
`/mnt/data/sn-testnet/qualification/native-recovery-20260916-r2`; publication
will be batched with the qualified code and lock before the next matching
release build. These are local preparation
results; they do not establish new on-chain acceptance.
[Actual refusal and fresh complete census](peerreview/evidence/FINAL-2-release-reserve-recovery-20260916/README.md).
[Completed build, software revision and setup receipts](peerreview/evidence/FINAL-2-native-adoption-20260916/README.md).
[Failed relay capture and paired stream errors](peerreview/evidence/FINAL-2-relay-stream-failure-20260916/README.md).
[Successful continuation and history adoption](peerreview/evidence/FINAL-2-continuation-20260916/README.md).

**Runtime-460 startup stopped at 02:13:45 UTC on September 16.** Its 4,672
carried-action checks finished, configuration rendering was verified and both
database migrations completed. The native process then refused the expired
continuation: block **8,015,211** exceeded the full-work start cutoff
**8,015,205**. It exited 1 naturally and cleaned up all temporary processes;
the persistent supervisor and soak did not start. The saved plan and prior
transactions remain intact, with the completed render recorded at journal
sequence 22,426. These are local execution receipts, not new on-chain acceptance.
[Actual startup result and retained progress](peerreview/evidence/FINAL-2-journal-recovery-20260916/native-resume/README.md).

Repeated full-journal validation scanned all earlier rows for every row. The
repair preserves the existing integrity and rejection checks while limiting
history comparisons to four witnesses per plan/action. All **15 affected
tests passed normally and under race**; restoring the original scan produces
exactly **two expected failures and 13 passing compatibility controls**. A
44,048-row synthetic replay checks the work bound without a timing threshold.
The syntax-only keyed-field followup preserves the tested behavior. Earlier
runtime and release qualification is retained by scope.
[Affected test receipts and source provenance](peerreview/evidence/FINAL-2-journal-recovery-20260916/README.md).

Recovery can refresh the continuation under existing fleet epochs **393–424**.
Its capacity forecast may extend beyond 424; the actual five accelerated and
three production acceptance windows must still fit those leases. No additional
renewal, spending approval or reduction of required work follows from the
expired preparation window. The new end and first native epoch are selected
late through the supported native commands.

**The chain advanced to runtime 460 before the revised launch.** The released 459
executable's plan attempt stopped at 22:38:46 UTC on September 15. A diagnostic
doctor reproduced four hard failures, all caused by 460-versus-459 admission at
the same finalized block. Both commands exited 1; their watched state, binary
and lock bytes remained unchanged. No new plan was emitted or adopted.

The 460 artifact observed at block **8,014,242** matches upstream CI source
`8d5f20ec1a5e5d90295d43046dacdefc54aaed06`. The existing offline Wasm verifier
passed at 23:07:53 UTC. The source delta fixes share-pool accounting while
preserving the native interfaces we use; the metadata has one changed byte,
its runtime-version constant. This admission refusal does not demonstrate an
ABI break. The qualified runtime-460 release was used for the startup attempt
above; no new live acceptance is claimed. Original runtime histories,
finalized actions and unchanged completed qualification remain retained.
[Actual admission failures, pinned 460 provenance and closed probe receipts](peerreview/evidence/FINAL-2-runtime460-20260915/EVIDENCE.md).

Runtime460 qualification completed on effective source `7989fa78`:
**108 selected roots per mode**, normally and under race (13 CRV4, 16 miner,
25 validator, 54 simulator), plus **13 expected failures and 11 passing
compatibility controls**. Acceptance composes unchanged passing results with
four corrected tests per mode and one corrected compatibility control. The
original compiler, fixture and control failures remain preserved with their
actual exits. Root checked exact positive root membership against the selected
tests. The fixes preserve all native progress and do not establish live soak
acceptance.
[Runtime460 test composition and raw receipts](peerreview/evidence/FINAL-2-runtime460-qualification-20260915/README.md).

**Runtime 459 source and artifact review is complete.** The pinned LAN code at
block 8,013,770 is byte-identical to the upstream CI artifact built from commit
`70378404b56c12a85bc8cd163aca2f32cf4d1b80`. The retained offline probe reproduced
its runtime tuple and metadata, with both exits 0. Our native transaction
formats and selective metagraph interface remain compatible; stake accounting,
child eligibility, root backing and proxy behavior changed. The candidate signs
with the new artifact while historical receipts retain their original runtime
authority. The recovery batch also shares successful signed-ledger replay
within startup, propagates cancellation, and refreshes a continuation without
resetting cumulative spending or finalized actions. Qualification is accepted
by composition: **154 affected roots per mode**, normally and under race,
including 21 CRV4, 13 miner, 45 validator and 75 simulator roots. The six
compatibility-control binaries produced their exact **12 expected failures
and 6 passing controls**. The original failed runs remain failed; three
test-only corrections required only their affected replacement checks.
The qualified 459 release was published; its native plan admission then refused
the chain's new runtime 460, as described above. This does not establish live acceptance.
[Pinned runtime observations, upstream linkage and offline probe](peerreview/evidence/FINAL-2-runtime459-20260915/README.md).
[Exact test composition, raw outcomes and preserved failures](peerreview/evidence/FINAL-2-startup-recovery-qualification-20260915/README.md).

**Startup interrupted at 21:30:06 UTC on September 15.** The attempted strict
resume verified 12 setup postconditions and finalized one native deployer
funding transaction in block **8,013,647**, extrinsic index 7. Its hash is
`0x800b72a73f4ce722a6d125541f2bcaf53593a218cfde7327a58171348a5f4279`;
the owned node's canonical block and a separate BLAKE2b-256 calculation
reproduce inclusion. Completed funding and earlier renewal actions are retained.
The supervisor never started. At finalized block **8,013,770**, the adopted
continuation had **7,472 blocks** left against the full run's **7,570-block**
requirement, and a pinned runtime query returned **459** against this build's
458 requirement. Repeated signed-ledger replay also delayed startup, and its
background context ignored graceful cancellation. The local renderer was
terminated after confirming its pending configuration-render intent and no
children; the actual joined result is **137**, not a passing startup. The
ledger cache, continuation refresh, cancellation and runtime migration are
being fixed and qualified together. New evidence uses the LAN node and records
`independent_rpc=false`.
[Raw chain observations, funding inclusion and interruption receipts](peerreview/evidence/FINAL-2-startup-interruption-20260915/README.md).

**Renewal 2 completed at 19:09:41 UTC on September 15.** The native command
returned `postcondition_verified`; its body, wrapper and joined process all
exited 0. The journal records all **1,212 new actions** broadcast and
postcondition-verified, with **zero failed entries**, covering all **202 fleets**
for epochs **393–424**. Its exact approved plan
`0x09ac683bae8bf99362bfc427776987fce951db58b71b3f01966236abbf7c91f1`
is retained by the adopted relay continuation described below. These completion counts come from the native command and
retained journal. The public completion bundle includes all **1,212 transaction
hashes and inclusion blocks**: **202 native commitments and 1,010 EVM mirror or
binding transactions**.
[Completion receipts and action list](peerreview/evidence/FINAL-2-renewal-2-completed-20260915/README.md).
The first separately sampled native commitment is included in block
**8,012,725**, and the sampled EVM mirror has a successful receipt in block
**8,012,728**. Canonical block lookups match both; the observed native
finalized head is **8,012,747**. The full native extrinsic hash also reproduces
using `b2sum`, separately from the project's implementation. These are two
verified transaction samples from the completed renewal; their direct RPC
reproduction covers those two samples.
[Raw chain evidence, transaction hashes and reproduction inputs](peerreview/evidence/FINAL-2-renewal-2-first-onchain-20260915/README.md).

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
On source `330ba512`, the producer finished with **39 passing phases and one
capture-race timeout**; the aggregate finished at **15:05 UTC** with **25/25
phases passing**, including cleanup and unchanged final source checks. The
two-test-file correction `03d529b` passes its 30 affected tests normally and
under race, three fresh confirmations of both defect controls in each mode,
and the actual **376-parent/140-subtest capture race in 567.382 seconds** under
the original 600-second limit. Both causal controls reproduce the old repeated
scan failure. Existing Go verification checks the retained raw results without
rerunning successful bodies. Two pre-body compiler-owner timeouts and one
missing-metadata refusal remain recorded. The composed capture's compiled
membership was checked after admission against its exact retained executable;
this is disclosed as a capture correction. The original producer remains a
failed attempt.

The correction changes no production code, scripts, release lock or approved
native plan identity. The published `c5db71a` producer subsequently completed
normal capture in 120.920 seconds but its race capture timed out again at
600.377 seconds on September 15 at 16:14 UTC. The active test was
`TestScenarioCampaignAttemptSuccessionReopenRejectsChangedAndMissingSources/another-plan`.
The seven-case parent had run for 90 seconds; the last child was still opening
its initial fixture. Investigation identifies repeated full-plan fixture work,
not a demonstrated chain or historical-decoder defect. The test-only correction
and replacement qualification are described below.
Both original gates subsequently stopped during agent usage-limit failures.
At 17:01–17:03 UTC root verified their processes absent; their worker logs record
stop 143 and joined descendants. No final outer exit or final source receipt
exists for either interrupted c5 invocation. Producer retained 38 passing phase
joins, the capture failure and an interrupted server-db phase. Aggregate
retained five passing joins and two interrupted race bodies. Preserve those
results and the earlier complete 330ba512 phase receipts; neither c5 invocation
is a full-gate PASS.

The next actual renewal preview passed at 17:07:03 UTC without sending a chain
transaction or changing campaign state. Plan
`0x09ac683bae8bf99362bfc427776987fce951db58b71b3f01966236abbf7c91f1`
covers epochs 393–424 and all 202 fleets, with 1,212 new actions capped at
**9.09 EVM TAO plus 0.606 native TAO**. Predecessor expiry removes the need for
revocations in that window. The exact saved plan entered actual apply at
17:27:28 UTC through the owned LAN node, after producer coverage was accepted
by composition. Its native admission rechecks the future window, custody,
nonces, liabilities and fees. This records dispatch of the apply command, not
yet successful completion or a new finalized transaction. One journal writer
retains the exact plan and transaction history for partial recovery. That first
apply stopped at **17:33:09 UTC** with `host/default-state-disk`: available
space was below the existing 20 GiB floor. The six campaign-state files,
runtime binary and release lock remained unchanged; no transaction was sent.
Cleanup removed only two generated fixture trees owned by a terminal test
run, recovering **3,681,648,640 bytes** and retaining all 61 parent evidence
artifacts unchanged. With **22.28 GiB** available, the same approved plan
resumed in a fresh capture at **17:46:51 UTC** and completed successfully at
**19:09:41 UTC**, with all 1,212 actions verified. Binary and release-lock
comparisons remained unchanged; campaign-state changes record the adopted
plan and completed transactions. The four temporary artifact services supported
the relay capture and import. The first actual relay-continuation capture, using end block
**8,021,610**, stopped at **19:18:46 UTC** with
`relay continuation exceeds unchanged source lifetime or storage bounds`.
Its campaign-state, binary and release-lock comparisons are unchanged; no
continuation was adopted. The busiest source retained 16,958 audit trails;
the unchanged 81,920-trail lifetime limit permits at most 8,065 future blocks
under the configured forecast. The corrected capture started at **19:28:36 UTC**
with an **8,060-block horizon**, ending at block **8,021,242**. It retains all
7,570 required campaign blocks and 490 blocks of capture/import/startup margin.
The adjacent record, byte, file and relay-slot limits also fit. No production
code, spending limit or completed renewal action changed. The corrected
capture completed at **19:44:33 UTC** with unchanged campaign state, binary
and release lock. Its exact saved output was imported successfully at
**20:04:13 UTC**, adopting continuation plan
`0xe128f2988512285a6270f45031f4a8af16e04009ceca385259869af02c50be30`
through block **8,021,242** with **zero chain transactions**. Both native
commands and their joined wrappers exited 0. Import changes reflect plan
adoption; binary and release-lock comparisons remained unchanged.
[Relay refusal, corrected capture and zero-transaction adoption receipts](peerreview/evidence/FINAL-2-relay-continuation-20260915/README.md).

The final history-adoption capture completed at **20:09:43 UTC**, with all
state, binary and release-lock comparisons unchanged. The finalized LAN
schedule at native block **8,013,383** reports epoch **1,476**, tempo **360**,
last epoch block **8,013,131** and no pending earlier boundary. The saved
request selects **1,477** as the first fresh steering epoch and has SHA-256
`554ace3866c4264b821eaa980396ecf19e7836b5fa4f55902bf0d70956c89507`.
The first decision and preparation snapshots must occur during that epoch;
startup may finish before or during it. All four temporary services stopped
and joined with exit 0 before actual strict resume. That first startup attempt
stopped at **20:12:07 UTC** because its configured private temporary directory
did not exist. Campaign state, runtime binary and release lock remained
unchanged. The recovery creates the required directories and retries the same
plan and saved history; it does not repeat completed preparation or renewal.
Simulator startup and campaign acceptance remain pending.

The user's September 15 instruction permits incremental recovery and composed
acceptance. Under the [harness recovery policy](README.md#incremental-recovery-and-acceptance),
retain unaffected completed phases, native preparation and finalized actions,
then replace only failed or affected coverage after a patch. Original failed
gate results remain failed. No blanket three-run confirmation or complete gate
restart is required solely for a test or documentation change.

At **17:27 UTC**, producer and aggregate coverage are **accepted by composition**.
The correction at `93f6d352d979c3dfb5fe068d4c07f400a227bc56` changes only
`campaign_succession_test.go`: it bounds repeated fixture plan decoding while
retaining all 1,000 provider identities and the authenticated budget, intent,
history and custody assertions. Its nine affected normal tests and 29 children
PASS. The complete corrected capture race passes **378/378 tests and 140/140
subtests in 479.526 seconds**, within the original 600-second deadline. The
old-fixture control produces the expected deterministic failure: 1,375,813
encoded bytes and 2,288 actions exceed the new work bound; the corrected
fixture contains about 151,500 bytes and 136 actions. Actual body, conversion,
membership and outer results match their expected outcomes; all thirteen
source comparisons, binary checks and cleanup pass. The complete race cohort
used four processors and overlapped the short normal body on a lighter host
load; the earlier simultaneous full-gate load was not recreated.
[Test qualification and original-result boundaries](peerreview/evidence/FINAL-2-incremental-capture-20260915/README.md).

These replacement runs cover the changed succession fixtures in normal/race
and close the failed complete capture phase. Unchanged phases retain the
completed 330ba512 receipts: 39 producer phases and the aggregate's unaffected
scopes. C5's complete normal result and the earlier qualified scan correction
retain their unaffected coverage. Every required producer and aggregate phase
therefore has valid coverage on its effective inputs; neither interrupted c5
invocation is relabeled as a passing full invocation. Production inputs and
the release lock are unchanged, so the existing canonical c5 executable runs
the renewal without repeating the 18 passed native preparation checks.

The complete RC/production campaign, final accounting and shutdown remain outstanding.
These preparation and qualification claims are local artifact evidence; all
new chain observations use the owned LAN node and record
**`independent_rpc=false`**.

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
