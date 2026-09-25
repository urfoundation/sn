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
