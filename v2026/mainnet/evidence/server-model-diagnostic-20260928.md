# Server-model diagnostic collection

The original run ended at **2026-09-28 00:38:30 UTC** on its configured
90-minute package timeout. Its terminal package result is failure, with
`5400.081s` reported duration. The wallet test active at that deadline did not
report a new assertion failure. This is incomplete diagnostic collection,
not a passing model suite or frozen composed-release qualification.

Observed root events: **1,048 passed, eight failed, seven skipped, 1,070
started**. Six roots had paused and the final wallet root had not completed.
Unstarted tests are not counted as passing or failing. The test and runner exit
codes are both 1; owned disposable-service cleanup exited 0. The original
server checkout remained clean at `b6f49bdbe6a61ef0cca3392b3a3ee47fe4806ec2`.

| Failed roots | Disposition |
| --- | --- |
| `TestPlanPaymentsMaxDuration`, `TestPlanPaymentsMaxDurationLoop` | Historical custody fixtures corrected in server `4468a696`; affected normal/race qualification retained. |
| `TestProbeDueQueueIgnoresTheEgressHealthGate` | Probe-health fixture corrected in `4468a696`; affected normal/race qualification retained. |
| `TestRemoveStragglerContracts`, `TestBackfillContractReapTime` | Terminal/sweep fixtures corrected in `4468a696`; affected normal/race qualification retained. |
| `TestGetProviderEgressLocationDueOrderingIsStableAcrossLimits` | Fixture chronology corrected in `d62f6fcc`; 15 affected roots pass normal/race and causal controls reproduce the original fault. |
| `TestRemoveContractBatchesDrainsDuplicateCandidates`, `TestAssignStragglerReapTimeRespectsBudget` | Original terminal usage and time supplied in the first fixture insert by `936c3d95`; both roots and six custody guards pass normal/race. Original fixtures reproduce both guard refusals. |

All eight asserted failures have isolated fixture repairs, integrated through
server `936c3d9563372e8f424d516ee2dd3525555206de`. The
[integration receipt](server-model-fixture-integration-20260928.md) preserves
their qualification scope. This does not change the original failed result.

Seven skips require optional inputs absent from this capture: one GeoLite2
place-list test, five pro/referral configuration tests, and one onboarding
configuration-repository test. They remain uncovered scopes.

The [raw capture](/mnt/data/sn-testnet/evidence/mainnet-server-model-full-20260927)
retains the executable, JSON events, exit files and `PROVENANCE.md`. Its actual
Go replacements resolved active sibling checkouts, so intended source pins do
not establish a frozen graph. The independently frozen full body under
`/mnt/data/sn-testnet/evidence/server-model-final-20260927/model-run` continues
with its own 120-minute deadline and completion census. Preserve its completed
work and qualify fixture repairs separately instead of restarting it.
