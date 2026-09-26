# R46 retained continuation launch

R46 started at 2026-09-25 17:18:19 UTC as user service
`urnetwork-sim-release-r46.service` (initial PID 4123621). It uses the
unchanged signed plan `0x8bb92697db8f2164e46f6e58848d3407e509382fb61550b919f1d55391ad480e`,
the retained fleet supervisor, and LAN RPC `192.168.1.162:9944`.

The executable is SHA-256
`e8d2c017760e17bc9fb72c6b8d1802c18c7686c2c39a4f2b2ba75b32ff448cfa`,
built with clean Git revision `ad5c05eca00a73bada9fdc35dfa516487a616177`.
The launcher SHA-256 is
`2f71bf2e84ceb5caa0bb6ae4f14b22f798c6372b9f0a74a3799b1c6255945e34`.
It verifies its pinned inputs and the predecessor checkpoint before the
scenario CLI acquires ownership. `preflight-launch.json` is the actual
in-service check at 17:18:22 UTC. Its finalized LAN head was block 8,084,535.

The qualified source change allows a narrow set of authenticated process-log
findings to remain provisional while observations continue. It does not turn
those findings or the lifecycle companion exception into acceptance passes.
At 17:25:12 UTC the owner signed generation 46 with run ID
`20260925T172403.199659160Z-release-1.0`, after authenticating all 45 prior
generations. The copied signed envelope and its compact receipt bind the exact
R45 sealed failure and invalidation. They show `preparation_complete=false`
and no acceptance boundary. This bundle records launch and preparation, not a
measured R46 interval or final result. Those must come from later owner evidence.

`first-observation.json` is the owner's first durable observation after the
signed preparation checkpoint. At finalized block 8,084,596 it proves 808
valid fleet bindings and current-policy rate readiness from complete epoch
629. It is not itself a signed acceptance boundary.

`generation46-signed-start.evidence.json` is an exact signed copy captured
after the owner set its acceptance boundary at 17:47:11 UTC. It records
baseline epoch 630 and measured epochs 631–635, start block 8,084,674,
end block 8,086,174 and terminal block 8,086,324. This is start evidence,
not proof that every measured epoch or terminal check completed.
`signed-window.receipt.json` records the source and copy hashes and the
signed window fields for a compact independent comparison.

`first-measured-observation.json` is the exact owner observation at finalized
block 8,084,680, six blocks into the window. Its SHA-256 is
`99cc4fe919e076cde81993fe875d09b240b73e0ef43bb5bb4a48d01dabd03873`.
It reports 808 valid fleet bindings and policy rate readiness, but cannot
establish complete epoch or terminal outcomes by itself.
`first-measured-chain-hashes.receipt.json` independently reads the same height
from the LAN RPC. The owner's contract head uses the EVM block hash; the
Substrate hash at that height is a different value, as expected.

`local-get-timeout-observation.json` and
`local-get-recovered-observation.json` preserve consecutive owner snapshots:
the former at block 8,084,710 has a local stats/proofs GET deadline failure;
the latter at block 8,084,740 restores policy rate readiness without changing
the signed acceptance window. This transient remains visible to final review.
`operator-read-recovery.receipt.json` binds both rows to exact offsets and
hashes in the owner's observation log.
`second-local-get-timeout-observation.json` records a later operator-2 GET
timeout at block 8,084,769 with `operator-2-api` among expected fault targets.
`operator-latency-triage.receipt.json` documents the overlapping scheduled
dependency faults and resource checks without assigning unproved causation.
`public-census-passed.receipt.json` binds the 18:03 UTC owner's deferred
public-census pass to its exact journal cursor, process and signed window.
`process-findings-180814.gate.json` and its receipt preserve the raw
validator-2 steering findings at the 18:08 cut. The receipt also documents
correction of the auxiliary monitor's scope-field selector. The live owner
continued under its provisional process-finding path; strict final review
must retain these findings.
`native1674-triage.receipt.json` and the replica-readback receipt distinguish
successful recovery of the exact public object from the missing native steering
intent: the active validator-2 intent and EMA remain at native epoch 1661.

`epoch632-first-observation.json` is the owner's first completed observation
after crossing the measured epoch-631 boundary. The paired
`epoch631-closure.receipt.json` binds its exact JSONL offset and SHA-256 and
independently matches its EVM block hash through the LAN RPC. It records a
provisional operator-2 rate shortfall for complete epoch 631 and does not
establish final acceptance.

`exit-gap-three-1856.receipt-v2.json` preserves the three exact swarm
receiver lines, their authenticated gate offsets/hashes and scope, matching
validator-1 sender ACK-lifetime warnings, and the owner's explicit
continuation record. The mechanism is still under investigation; the raw
blocking process findings remain in the live owner's terminal gate.
The later `exit-gap-spread-1908.receipt-v2.json` joins its exact raw gate copy,
`swarm-health-1906.receipt.json` and `proof-progress-1907.receipt.json` to
show that the recurring failures had spread to six swarms while useful
validator-1 traffic and provider health continued. These read-only receipts
support continuing the partial run; they do not certify acceptance.

`epoch633-boundary-observation.json` and `epoch632-boundary.receipt.json`
pin the owner's first epoch-633 observation to LAN EVM block 8,085,274. Its
rate source still identified epoch 631 and therefore failed the last-complete
epoch identity check. `epoch633-source-recovered-observation.json` and
`epoch632-source-recovery.receipt.json` pin the later complete epoch-632
sources and restored rate readiness at block 8,085,300. The paired receipts
include exact JSONL offsets, hashes and LAN block responses; neither row is
a final-acceptance result.

`head-boundary-restored.receipt.json` preserves the sampled, hash-bound fault
file rows showing restoration of both signed head-boundary faults while the
owner remained active. Other faults and final acceptance remain open.

`validator1-restart-2049.receipt.json` binds a new validator-1 steering
failure to the signed rolling-30 termination and replacement process. Its
paired gate, fault and supervisor copies retain exact raw identities.
`swarm-health-2047.receipt.json` reports all 1,000 providers running, and
`proof-progress-2047.receipt.json` records proof growth before the restart.
These do not establish post-restart validator-1 steering or final acceptance.

`epoch634-first-observation.json` and `epoch633-closure.receipt.json` bind
the owner's first epoch-634 observation, its exact JSONL offset and SHA-256,
the complete epoch-633 usage sources and a matching LAN EVM block response.
They show the third measured epoch boundary was owner-observed, but do not
certify terminal acceptance.

`epoch635-first-observation.json` and `epoch634-closure.receipt.json` bind
the owner's first epoch-635 observation to its exact JSONL offset and a
matching LAN EVM block response. Complete epoch-634 usage is low for both
operators and fails the two-times-native rate threshold. The fifth measured
epoch is evidenced below; terminal acceptance remains open.

`epoch636-first-observation.json` and `epoch635-closure.receipt.json` bind
the owner's first epoch-636 observation to exact JSONL bytes and an
independent matching LAN EVM block response at signed measurement end block
8,086,174. Complete epoch-635 usage also falls below the two-times-native
threshold. This establishes that all five measured epochs were observed; it
does not establish a passing terminal gate or a sealed owner result.

`epoch634-restart-causality.receipt.json` bounds all four proof-file scans and
the validator and swarm process samples used to investigate the epoch-634
rate collapse. `validator1-startup-phase-2233.receipt.json` pins the
replacement validator's still-incomplete startup phase. The paired
`EPOCH634-RESTART-REVIEW.md` explains the inference and its limits. These
read-only records neither repair the running owner nor waive strict findings.

R46 subsequently crossed the signed terminal block and sealed a failed
provisional owner result after graceful cancellation. `owner-result.json`,
`owner-faults.json`, `owner-process-logs.json`, `owner-adversaries.json`,
`owner-anomalies.json`, `owner-analysis.json`, and
`generation46-signed-exit.evidence.json` are exact bytes from the independent
terminal capture, not regenerated summaries. `terminal-capture.receipt.json`
hashes every copied source and records zero capture errors. The owner result
contains 182 assertions, 86 failures, 182 open anomalies and
`final_acceptance=false`; the signed generation records
`execution-exited-before-completion`. `terminal-chain-heads.receipt.json`
independently retrieves signed terminal block 8,086,324 and verifies the
owner's end-head hash at 8,086,545 through the LAN RPC.

`failure-clusters.receipt.json` assigns all 86 failed assertion rows to
bounded diagnostic groups without treating derived adversary-vector rows as
independent root causes or converting any failure to a pass. A fresh
read-only terminal diagnostic is running against the sealed files; its
completed report will be added separately.

`r46-adversary-triage.receipt.json` binds the capped oldest-first operator
surfaces and the unresolved actor, process and restart findings to sealed
samples. `r46-adversary-fix-review.receipt.json` lists the isolated fixes,
normal/race tests and controlled old-behavior failures. Those code changes
postdate R46 and cannot change its historical result.

`r46-gap-native-root-review.receipt.json` pins every retained exit-gap class
and the native-1674 continuity evidence. Its 120-second receiver versus
300-second sender idle-rule mismatch is a bounded hypothesis requiring a
real lifecycle regression, not an established repair.

`sealed-r46-cleanup-history.receipt.json` compares both final lifecycle
cleanup rows with the signed latest checkpoint and binds their request and
completion observations inside the authenticated prefix. It identifies the
diagnostic reader's direct start-to-final transition error without changing
the strict failed R46 result.

`no2-epoch634-rootmissed-claim-cut.receipt.json` independently combines the
owner's exact final observation with LAN historical EVM reads. The vault
emitted `RootMissed(634, 2, 0)` at block 8,086,027 inside the signed window;
at the observation block its entitlement still has status 3 and zero root.
The selected claim projection separately records miner 881's operator-1
epoch-635 submission as unresolved at the terminal cut, even though a later
queue receipt finalized after the signed terminal block. Neither later
artifact nor later claim changes the original acceptance verdict.

`epoch634-empty-payout-census.receipt.json` binds the pinned taskworker's
zero-leaf operator-2 close, operator-1's four-leaf close and root confirmation,
the server's deliberate no-leaf submission skip, the proof blackout and the
historical LAN `RootMissed` evidence. It supports an empty payout census,
without proving the exclusive upstream provider eligibility cause.
