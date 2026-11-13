# Server settlement statistics fixture qualification — October 2

The original full model run at server `025802a50e2dc56dc56c3cb749db7375aa9f72be`
and SN `6cfc4773038fae8b1de07807eeeb4a97c32d07d3` executed all 1,345 eligible
roots: 1,334 passed, one failed, and ten optional-input tests skipped. No root
lacked a terminal result. Private PostgreSQL/Redis cleanup succeeded. The run
completed in 7,192.793 seconds; its package verdict remains failed.

`TestOpenContractStatsKeepsExactSmallPopulation` failed when its shared fixture
inserted a terminal contract without the immutable provider-usage snapshot
required by the settlement trigger (`P0001`). The production constraint was
correctly enforced. The allocated 1,024 bytes are not evidence of completed work.

The isolated test-only repair is server
`a3fc427066f037447971c90a51a80c527ee83162`, tree
`0408736564f2eabded81037067d6d2547c1acd50`, directly based on `025802a5`.
Only `model/contract_stats_model_test.go` changes. Terminal fixture rows now carry
an explicit zero-byte, zero-provider usage snapshot and terminal timestamp;
open/disputed rows retain null terminal facts. No production, module, trigger,
allocation, or statistics behavior changes.

Independent qualification reproduced the original failure in both normal and
race modes. All eight repair/helper-caller roots pass normal and race tests,
with `./model` vet exit zero, no skips, and successful private fixture cleanup:

- `TestContractStatsFixtureRetainsExplicitZeroSettlement`
- `TestCountOpenContracts`
- `TestContractHourWindowCountsAndCache`
- `TestContractHourBucketSettles`
- `TestContractHourBucketsAreSharedAcrossWindows`
- `TestOpenContractStatsCapsLargePopulation`
- `TestOpenContractStatsSamplesNewestRowsBeforeExtenderMembership`
- `TestOpenContractStatsKeepsExactSmallPopulation`

The byte-identical [original full-model receipt](server025-full-model-independent-20261002.json)
has SHA256 `4e580a88539860f503f8ac0f0d77a3978634c731a9ce5a3dd18565c7a6e92373`.
The separate [repair receipt](server-stats-fixture-independent-20261002.json)
has SHA256 `dc951c14366b4afac730ecbc8e68606c23c008c1dac4edaff0cea924d2d7ba4e`.
Raw logs, exact commands, optional-skip reasons, dependency pins and cleanup
receipts remain under
`/mnt/data/sn-testnet/sol-server025-integration-independent-20261002/`;
each receipt binds its own raw evidence hashes, with the three historical runner
path corrections explicitly recorded below.

## Current integration

Fresh server origin advanced to `2bbea1f128e0d87b65266cf688e9175542819916`.
The exact test-only repair integrates as
`0739cab5b74f5d0defcfd17c9194277842dfc521`, tree
`910e98d2185436918ec9572425a7580e84e78a65`. Its one-file delta and unchanged
`go.mod`/`go.sum` match the earlier repair. Upstream adds an escrow-revision
trigger replacement on the same table, so the affected eight roots were checked
again on this exact integration: normal/race pass, package vet exits zero, and
private fixture cleanup succeeds. The separate [integration receipt](server-stats-fixture-integrated-20261002.json)
has SHA256 `2adc095af94c4abf6081b6a7157daf52540c633c21d358619abd994288cad38b`.
This scope does not qualify the other upstream changes across 15 files.

The final pushed integration is server
`047a266496709e9410cba3b7f08cd3c2703bf207`, tree
`92f7251d3f6d5e7da448ba91ec21fecb79106034`, joining qualified `0739cab5`
with upstream `b3ba233f`. The [final source join](server-stats-fixture-final-join-20261002.json),
SHA256 `6ddec7f96953aecb2f0dddd809ba4954f8e5431094e9ad7ea3e4b7592f6e95e2`,
checks seven exact helper/statistics/usage-guard/module files and unchanged
previous escrow source and migration bytes. The only new database registration
appends the reservation-snapshot table; it does not change this helper's raw
INSERT or immutable-usage/contract-trigger paths. No tests were repeated on this
join. The eight-root execution receipt remains scoped to `0739cab5`, and the
new reservation cache and other upstream changes need separate qualification.

## Historical runner binding correction

The audit reused `run_private_stage.sh` while adding later stages after earlier
receipts had sealed different bytes at that path. This caused three historical
path/hash mismatches. Exact earlier byte sequences were recovered at immutable
scope-specific paths: `run_private_stage-focused-sealed.sh`,
`run_private_stage-full-sealed.sh`, and `run_private_stage-fixture-sealed.sh`.
Their hashes match the three original receipt bindings. The separate
[binding correction](server025-evidence-binding-correction-20261002.json), SHA256
`9ce6ac74279fa5d993ddcdb61199abd98b5bdadcf8a5c45cfed86a8d9f5ee59d`, records those
remaps and readback of all 80 bindings across four original receipts. Original
receipt JSON, source pins, logs, fixture records and outcomes are unchanged;
the current shared runner path does not match all historical hashes.

PH-06/PH-16 evidence practice: seal runners at immutable content-addressed or
scope-specific paths before qualification. Never reuse a sealed mutable path
for a later stage. Any correction must preserve the original receipt and exact
raw bytes, with a separate explicit binding addendum.

There was no patched-source full-model rerun. The ten original skips remain
unqualified. This test-only repair does not qualify the later local-storage
adoption, a new composed release, a remote physical restore, or any live
deployment. The frozen SN `258e25b4` / server `0aa1e244` release remains unchanged.
