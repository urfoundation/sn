# R46 epoch-634 shortfall and validator restart triage

Read-only cut: 2026-09-25 22:30:15–22:30:27 UTC. Continue R46 unchanged. There is no established need for a live repair to collect its signed terminal and honest failed result. Validator 1 is alive and doing expensive replay work, but has not resumed proof production; process health must not be reported as collection health.

Receipt: `epoch634-restart-causality.receipt.json`, SHA256 `17581a1b35340a4fa1ea2516bf0d7b6518ceaabe9689de4c4d44c1c257e65ed4` (130,284 bytes). It contains immutable gate/fault/supervisor snapshot hashes, the exact acceptance scope, raw process lines and matching hashes, complete proof-file cut hashes and counts, observation offsets/hashes, and two `/proc` samples. All four proof cuts end on complete JSONL records. The receipt is an external read-only analysis, not a replacement for authenticated campaign evidence.

## Most likely causal chain

The scheduled validator restart exposed a long retained-history replay/publication phase before trail workers can run. Validator 1 last completed proofs at 20:43:04, exactly when rolling restart 30 interrupted it. Replacement PID 163939 started at 20:43:07 and remains supervised healthy. Its exact retained failure is `terminated signal received`, not a spontaneous endpoint timeout: `processes/validator-1.stdout.log` byte offset **30,518,248**, line SHA256 `0a572d34b6ab80fad7d89d3ab847f99c656e8756c7fa4e87471d0d55ac6dda3d`.

At 22:30, validator 1 still has no post-restart path proofs. During an 11.34-second read-only sample it gained 5.88 user CPU seconds, 9,270,382 read characters and 10,449,554 written characters. Its two active `census-replay`/`census-replica-two` LevelDB log files advanced from `000004.log` to `000006.log`. This is affirmative work progress, not proof of a deadlock. It does not establish a completion deadline or exclude an algorithmic performance problem.

Validator 2 replacement PID 169276 started at 20:48:04 after rolling restart 31. Both proof streams resumed at 22:07:38, approximately 79.6 minutes later, and reached 522/526 epoch-635 proofs by 22:30:24. Its earlier proof stall started before that restart; the restart did not create all of validator 2's missing activity. Native steering remains separately unsuccessful (`compact head EMA epoch jumped`), so fresh path proofs must not be presented as recovered native intents or weights.

All four complete proof-file cuts contain **zero epoch-634 rows**. Operator source usage declined from 21,465,073/17,885,137 bytes in epoch 633 to 5,250,701/5,732,655 in epoch 634, declines of 75.5%/67.9%. Operator 1 then produced only 105,582 TAO rao against the required 200,000. The concurrent proof blackout is the strongest explanation for reduced useful traffic. The retained evidence does not allocate every missing byte to one cause, so this is a causal inference rather than a measured per-byte attribution.

The 18 exit-gap events across 10 swarm processes are unchanged since 20:43:05 and exactly match the prior 20:48:54 gate. All 20 swarm processes remain healthy. The warnings therefore do not show an expanding whole-fleet failure during epoch 634. Their silence while validator 1 sends no new trails also does not establish that its transport problem is fixed. The previous exact sender/receiver correlation still supports a separate idle-state recovery hypothesis: receiver lifetime 120 seconds, sender lifetime 300 seconds, resumed nonzero sequence while receiver expects zero. A sequence exit is not a miner process exit; `queued` in this line is a sequence number.

## Exact evidence locations

State root: `/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4`.

The run observations are `runs/20260925T172403.199659160Z-release-1.0/observations.jsonl`:

| Byte offset | Observation | Evidence |
| --- | --- | --- |
| 55,578,143 | 21:42:06 / block 8,085,852 | Epoch-633 rate sources; readiness true |
| 56,798,578 | 21:47:18 / block 8,085,878 | First epoch-634 shortfall; readiness false |
| 62,895,556 | 22:13:28 / block 8,086,009 | Same closed sources and failed readiness |
| 65,334,699 | 22:24:24 / block 8,086,064 | Owner still observing, epoch 635, 808 valid bindings |

Proof files share `runtime/validator-{v}/evidence-generations/generation-00000000000000000001/state/operators/no-{o}/proofs.jsonl`:

| Validator/operator | Byte offset | Exact witness |
| --- | --- | --- |
| 1/1 | 35,302,811 | Last proof, 20:43:04.101, epoch 633 |
| 1/2 | 36,190,665 | Last proof, 20:43:03.943, epoch 633 |
| 2/1 | 21,204,402 | First resumed proof, 22:07:37.981, epoch 635 |
| 2/2 | 21,478,704 | First resumed proof, 22:07:38.272, epoch 635 |

The receipt contains every scoped gap's first/last raw line. Representative latest witnesses: `processes/miner-swarm-16.stderr.log` offset **92,579,320**, and `processes/miner-swarm-18.stderr.log` offset **94,382,708**. Validator 2's current retained native failure is `processes/validator-2.stdout.log` offset **39,689,889**, `subnet epoch 1678 attempt 2: compact head EMA epoch jumped`.

## Terminal consequence and next run

The source under deployed owner commit `ad5c05eca00a73bada9fdc35dfa516487a616177` leaves the final gate strict. `validator/release_run.go:841–859` completes native startup and initial terminal publication before starting proof workers; `validator/release_runtime_v2.go:266–339` owns the observed census replay directories. The supervised validator binary itself remains based on `6100394b`.

The R46 observer is independent of those proof workers. `sim-testnet/scenario_interval.go:23–33` requires epoch 636 and finalized block **8,086,324**. `scenario_lifecycle_cleanup.go:215–232` permits provisional completion after exact cleanup and custody/chain integrity checks; proof freshness and rate readiness are not those integrity checks. `scenario.go:4581–4591` invokes the strict validator settlement-closure wait only after all acceptance assertions already pass. `scenario.go:4689–4696` persists outputs before provisional finish, and `provisional_epoch.go:190–231` records bounded publication failure in that result. Thus missing validator proofs impair evidence completeness and strict acceptance, but do not themselves require an operator intervention to preserve a failed terminal result. Owner PID 4123621, supervisor PID 3780072, and durable monitor PID 4187925 were active at this cut. The two lifecycle filters remain owned by the authenticated terminal cleanup path.

1. **Prioritize replay progress and restart readiness.** Add an authenticated resume checkpoint for retained census replay/publication, binding exact source cut, plan, both replica roots and retained publication identity; revalidate changed tails. Test restart with a large frozen prefix and a moving current epoch, require bounded replay work and fresh proofs on both operator streams before declaring restart readiness. Changed prefixes, foreign checkpoints and divergent replicas must still fail. Do not bypass authentication, reset native EMA history or mark a healthy PID as proof recovery.
2. **Test the idle sequence recovery hypothesis.** Exercise two real in-memory peers with a negotiated compact head across 121–299 seconds of virtual idle, then resume the same sequence. Cover a lost first missing-contract ACK and a carrier change; require exact-once payload delivery and a completed trail. Diagnose the broken state owner before selecting a narrow fix. Do not increase the gap timeout or weaken terminal log classification merely to hide the symptom.

No source or live state changed, and no new patch or broad test suite was run for this read-only triage. Prior focused deployed process-log policy tests and their command remain in `EXIT-GAP-REVIEW.md`; raw strict findings remain intact.
