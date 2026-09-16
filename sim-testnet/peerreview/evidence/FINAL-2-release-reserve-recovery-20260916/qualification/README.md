# Reserve software-revision qualification

The production correction is frozen in `2ffbab2bb3b2251e3eab1d79f687fba50c2b57b5`.
Its `plan_revision.go` SHA-256 remains
`e1b389eac847fa8a9b6bfdaa22ca6e5a08d495868cfb153a88cfcf702fffd262`
through the test-only corrections ending at
`5237cb373f61227f3d576c958168f32cb75fd615`.
Terra executes qualification; Astra diagnoses and corrects failures. These are
synthetic local tests, not on-chain or live-campaign acceptance.

## Actual invocations

| Capture | Normal | Race | Meaning |
| --- | --- | --- | --- |
| `original-positive/` | 25 pass, 1 fail | 25 pass, 1 fail | The carry test compared raw numeric-zero representations. Both invocations exited 1. |
| `original-causal/` | 3 expected failures, 3 passing controls | Not scheduled | Restoring the original planner reproduces the exhausted repair-capacity refusal. Exit 1 is expected. |
| `supplemental/` | 2 pass | 2 pass | Adjacent fleet-recovery fee and historical-batcher behavior; exits 0. |
| `correction-e639/` | 2 pass, 1 fail | Compiler passed; body not run | Numeric equality was corrected. The intermediate fixture then incorrectly validated a new outer release with its predecessor's fresh companion. |
| `correction-13bc/` | 2 pass, 1 fail | Compiler passed; body not run | Validation was scoped to the reconstructed predecessor, and independent assertions were batched. This exposed the fixture's inconsistent source/wallet/capacity observation. |
| `correction-5237/` | 3 pass | 3 pass | All related moving alpha balances now advance consistently. Both invocations exited 0; the production implementation is unchanged. |

The final confirmation selection is exactly the direct completed-repair carry,
the full authenticated revision renderer, and the exact-prior-approval negative
control. The full renderer explicitly validates the complete new plan's budget.
The direct test retains its full budget, action-order, predecessor-immutability
and approval-hash assertions. Numeric comparison checks every component rather
than equating Go's empty representation with a different economic amount.

## Composed acceptance

Affected qualification is accepted with **28 distinct top-level roots per mode**:
the original 26, with
the three affected roots replaced by the final confirmation, plus two adjacent
controls. Keep original failures as failures and unrun bodies as unrun. Exact
selectors, compiled censuses, JSON test events, actual exits and before/after
source, binary and selector hashes accompany each closed invocation.
Root independently rebuilt both accepted sets from the raw JSON terminal events
and checked exact membership, closed exits and input fences in `root-review/`.
This qualification does not establish successful native adoption or a running soak.

The original-planner causal results remain valid: all three intended failures
stop at the exhausted-capacity error before the corrected postconditions.
The three negative controls retain the same production behavior. No additional
causal run is required by these test-only corrections. Earlier journal, runtime,
producer and aggregate qualification remains retained according to its scope.

`source/` preserves the production patch, successive test corrections and the
full original planner as text. Captured commands retain their original private
workspace paths as provenance. They are not portable commands and must not be
used to overwrite these closed captures. Compiled binaries and temporary test
directories are excluded from this published evidence.
