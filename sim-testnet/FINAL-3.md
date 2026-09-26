# Sim-testnet finalization report 3 — R47 partial failure

**R47 did not complete or qualify its release interval.** The owner signed a
five-epoch measured window, epochs 651–655, starting at finalized block
**8,090,674**, ending at **8,092,174**, with terminal evaluation due at
**8,092,324**. It exited after observing block **8,091,300** in epoch 653.
Its sealed result says `result=fail`, `final_acceptance=false`, with six
assertions recorded, five failed, and 119 open anomalies. The signed attempt
was invalidated as `execution-exited-before-completion`. The scenario and
terminal gates left pending by that exit are **unavailable**, not passes.
[Sealed result, signed envelopes, full observations and checksum manifest](peerreview/evidence/FINAL-3-R47-terminal-20260926/README.md).

The immediate exit was the provisional heartbeat process-log gate. It treated
four release-blocking log classes as a reason to end observation; its first
class was two `operator-1-api/stderr/warning` lines. The exact lines at
15:58:12 UTC report MinIO records-stream TCP reads reset by the peer after
roughly 2½ minutes. Operator 2 logged the same transport class. A later
MinIO health GET returned HTTP 200. That proves service health at the later
sample, not the cause of the resets. The warning lines, byte offsets and
SHA-256 hashes are in the [stop evidence](peerreview/evidence/FINAL-3-R47-terminal-20260926/stop-warning-lines.json).
This was a controller failure: the owner could have retained the findings
while continuing to collect observations to the signed terminal. It does not
excuse the findings at final acceptance. A successor fix must separate
provisional continuation from the strict terminal gate and test this exact
case; it cannot change R47's signed outcome.

The on-chain and owner evidence establish the following bounded timeline:

| Boundary | What was observed | Evidence |
| --- | --- | --- |
| 8,090,674 | Signed measured start, baseline epoch 650 | [R47 signed start](peerreview/evidence/FINAL-3-R47-terminal-20260926/campaign-start.evidence.json) |
| 8,090,680 | Both epoch-650 `RootCommitted` transactions succeeded | [Receipts and hashes](peerreview/evidence/FINAL-3-R47-measured-start-20260926/README.md) |
| 8,090,975–980 | Complete epoch-651 usage source, then both epoch-651 roots committed | [Owner row and LAN receipts](peerreview/evidence/FINAL-3-R47-measured-start-20260926/README.md) |
| 8,090,977 | Both epoch-651 vault captures were **zero** | [Raw vault logs](peerreview/evidence/FINAL-3-R47-zero-entitlement-20260926/onchain-epoch651-zero-entitlement.json) |
| 8,091,127 | Both epoch-651 entitlements finalized for **zero** | [Raw receipts](peerreview/evidence/FINAL-3-R47-zero-entitlement-20260926/onchain-epoch651-zero-entitlement.json) |
| 8,091,156 and 8,091,174 | Sampled successful epoch-651 `Claimed` transactions each claimed **zero**; neither emitted `ClaimPaid` | [Raw claim receipts](peerreview/evidence/FINAL-3-R47-zero-entitlement-20260926/onchain-epoch651-zero-entitlement.json) |
| 8,091,300 | Last owner observation, before end or terminal | [Sealed result](peerreview/evidence/FINAL-3-R47-terminal-20260926/result.json) |

The LAN archive scan over blocks **8,084,000–8,091,280** returned 50 vault
`EmissionCaptured` events. Six were nonzero; the last was epoch 631 at block
8,084,977. All **42 later captures through epoch 652 were zero**.
[Exact filter and response](peerreview/evidence/FINAL-3-R47-zero-entitlement-20260926/capture-history-scan.json).
The immutable vault's zero-capture branch reads zero pool-hotkey stake;
minimum-transfer dust and missed-boundary events use other event paths.
Thus committed roots and finalized claims in R47 do not establish funded
payments. The sampled claims were zero-value executions. The owner's
`value_reconciliation` totals are cumulative and must not be mistaken for
R47 epoch-651 payments.

Authenticated native history identifies two different upstream allocation
failures. Around the last nonzero epoch-631 capture, validator UID 255 was
active and had positive weights for pool UIDs 3 and 4. By epoch 632, both
validators were inactive after the 5,000-block activity cutoff, although
UID 255 still stored positive pool weights. After native-1690 application,
both validators were active again, but both applied vectors omitted pool UIDs
3 and 4. Their signed source-epoch-648 audits marked the pools
`zero_pool_weight` because the lagged payout root was unavailable on-chain.
Pool emissions were zero in both later states. The exact native storage,
applied-row and source-audit evidence is in the
[R48 recovery evidence and proposal](peerreview/R48-NATIVE-RECOVERY-PROPOSAL.md).
Restoring native history alone will not fund the vault: a successor must prove
eligible positive weights, pool emission and stake growth, a nonzero capture,
and the resulting entitlement and payment chain.

The owner reported epoch-651 complete usage of **19,396,509** and
**18,756,754 bytes** and rate readiness. Its first complete epoch-652 source
reported only **5,737,506** and **5,299,429 bytes** and rate readiness false;
the former produced 115,371 TAO rao against the configured 200,000-rao
two-times-native threshold. Read-only comparison reproduced all six epoch
650–652 artifact totals from immutable provider-usage snapshots at contract
close. Completed credited contracts fell from 8,602/8,285 in epoch 650 to
2,343/2,251 in epoch 652; 12,838/13,164 epoch-652 contracts were
`expired_unconfirmed` with zero credited bytes. At the pinned price, the
unchanged all-tier margin requires **16,576,936** complete-source bytes per
operator. Later closures added only 1,211,287/1,309,553 bytes from contracts
created in 652, with no older open cohort crossing the boundary; they cannot
repair that immutable source. The [exact SQL, aggregate responses, artifact
comparisons and fault/proof context](peerreview/evidence/FINAL-3-R47-rate-triage-20260926/README.md)
support an actual completed-work shortfall, not an accounting mismatch. They
do not independently measure network traffic or prove that every missing
byte was caused by a particular signed fault. Other retained strict findings
include a native-1691 steering gap after a dependency outage, valid adversary
probes selecting excluded miners, an unplanned source-egress `EXTEND` 400,
and artifact GET exhaustion. The [sealed adversary, fault and process records](peerreview/evidence/FINAL-3-R47-terminal-20260926/README.md)
preserve their scope. Candidate fixes developed after R47 began cannot be
credited to its signed executable or used to erase those failures.

The post-R47 heartbeat correction has a
[deterministic red/green and race receipt](peerreview/evidence/FINAL-3-R47-artifact-transport-continuation-20260926/README.md).
The pre-fix test reproduced early exit at block 3,200 instead of the synthetic
5,050 terminal; the fixed test reached terminal and still failed strict
process-log acceptance. This qualifies the candidate behavior, not R47.

This report is a **partial diagnostic**, not acceptance under the whitepaper.
The prior independent peer review found that the coordinator's actual cadence
was 300/50/150/5 rather than the whitepaper's 360/60/180/6, and that
`max_allowed_validators=64` exceeded the ≤56 target. Neither discrepancy is
waived here. R47 did not complete even its configured five measured epochs,
much less prove three consecutive fully observed production-cadence epochs.
The earlier peer-review scripts in
[peerreview/verify](peerreview/verify/README.md) verify earlier pinned chain
state; they have **not** been represented as an independent verification of
R47. R47 on-chain receipts here were read from the LAN archive RPC on chain
945. EVM block hashes and Substrate block hashes are separate namespaces.
Artifact usage, process logs and fault classifications remain owner evidence;
the linked raw RPC responses let a reviewer independently requery the chain.

The next run must preserve R47's sealed failed result, repair provisional
continuation, recover authorized native history and positive pool funding,
and proceed to terminal even when soft findings remain. Final acceptance
still has to adjudicate every retained finding and economic receipt.
