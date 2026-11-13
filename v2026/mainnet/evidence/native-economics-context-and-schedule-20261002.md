# Native economic context and v470 scheduling — 2026-10-02

MG-06 remains open. These source increments repair observable archive and
preparation failures; they do not establish the native miner denominator,
quantization tolerance, actual provider entitlement, recycled alpha or the
10%/90% outcome. No live signing, transmission, deployment, start or release
rebuild occurred. The exact v470 artifact exception remains planning-only.

## Source and dependency scope

| Increment | Exact source | Tree | Parent |
| --- | --- | --- | --- |
| Native emission context | `dd21ed002df3e11de2630f799b4084c070160604` | `d8154c938539c0aca51be6518d65fd1959524784` | `1320845da313b43b0f20fd6f7207d838e2d9aa13` |
| Signed scheduling profile | `258e25b4dd2c8b210bd8932b415e7d39776e23a0` | `253bc2b79016c06d4d7d5f06ce828e22660b8bb6` | `dd21ed002df3e11de2630f799b4084c070160604` |

Both scopes use Go 1.26.6 linux/amd64, server
`ac86855df7f63a1d85a1c57a70298f9c7f9ced4d`, warp
`7498864c7cd3605aad3c43eabfab9008ed7f7228`, proxy
`6204ae7df2a9868bbb3a7b61231917a36e4f5c9f`, userwireguard
`85fb1ca4086fa5dbfcda526bec7a17a894e691b9`, glog
`892ade4a6be396b32ea82a550f243190b5992180`, and goidenticons
`325750b38314313dc5f44c880ab6f12f6c1ecb3c`. Every source and sibling checkout was
clean at its fence; all scratch and caches were on `/mnt/data`. The reporting
branch merges documentation-only main `9238d8d8` (including `1413c78a`) without
retargeting either source pin or the retained release, owner-trim and
recycle-custody receipts.

## Native observer

The exact reviewed runtime source
[`923fd1fa`](https://github.com/RaoFoundation/subtensor/tree/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d)
can reset `LastEpochBlock` through an authenticated `TempoSet` without incrementing
`SubnetEpochIndex`. The old observer incorrectly required a terminal epoch
event. It also assumed Yuma's vector length always equaled the parent UID count,
although an initialization owner takeover can append one UID and emits
`SubnetOwnerChanged`, not `NeuronRegistered`.

The correction authenticates both context-event shapes and `Tempo`, preserves
their phase/order/raw evidence, and admits only the source-supported state
transition. Unexplained anchor resets, epoch drift, wrong final tempo,
contradictory deferral, duplicate/late takeover or a larger UID change fail.
No recipient identity or economic amount is inferred from that context.

| Observer check | Outcome |
| --- | --- |
| Author exact132 baseline plus frozen causal/fixture overlay | Both intended causal roots fail |
| Author normal | 28 committed roots plus one external full470-metadata profile pass |
| Author race / vet | 28 roots pass / exit 0 |
| Independent Sol exact132 baseline | Both intended causal roots fail |
| Independent Sol normal / race / vet | 28 / 28 roots pass, zero skips / exit 0 |

The full metadata control uses the retained 354,056-byte observed v470 metadata,
BLAKE2b-256 `8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34`.
A reduced projection lacked consumed event types; that was a fixture gap, not
a demonstrated production failure. An initial deferred-then-reset positive
scenario was also discarded after source review: the ordinary setters respect
the pending-epoch freeze. The final regression rejects that conflict. Neither
discarded experiment is counted as a fixed production root.

- [Author receipt](/mnt/data/sn-testnet/native-emission-context-20261002/evidence/receipt.json), SHA256 `7c62cfe45203fb741d9cf73e6fa19119e4ecfa3340a063d1c4d1b36316ab7d40`.
- Author manifest SHA256 `44de43f6f0987a7c6526d068452134eb03ec6f9c8af46ca90475ec6653227a69`; [bundle](/mnt/data/sn-testnet/native-emission-context-20261002/native-emission-context-author-evidence.tar.gz) SHA256 `e695e6c7dc7bf31011ff792941ae50149b05719186abc338d89189d8c2908648`.
- [Independent Sol receipt](/mnt/data/sn-testnet/sol-mainnet-native-emission-independent-20261002/receipt.json), SHA256 `bb3d20418db1be7648e22e4133ee4d021bea544fbc99d43fb94ee7ce297fa55a`. It separately rehashed all 28 author manifest entries and the bundle.

## Scheduling and original signed intent

The reviewed source's
[`should_run_epoch_with_tempo`](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/coinbase/run_coinbase.rs#L1208)
tests `BlocksSinceLastStep > tempo`, while the old Go scheduler and monitor used
`MaxTempo`. Coinbase increments/clamps the counter before this predicate.
[`block_step`](https://github.com/RaoFoundation/subtensor/blob/923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d/pallets/subtensor/src/coinbase/block_step.rs#L18)
reveals before coinbase; the commit extrinsic executes after initialization.
Changing only the threshold is therefore insufficient at counter equality.

For synthetic head/anchor 1000, tempo/counter 100 and epoch 7, the corrected
first included commit belongs to epoch 8 and reveals at block 1101 for a
one-epoch period. The old implementation predicts 1100; a threshold-only patch
incorrectly predicts 1002. Fixed wall time at drand genesis yields round 416,
instead of 412. With counter 99, the corrected prediction is block 1003 while
the old implementation still predicts 1100. Deterministic roots cover below,
equal and above tempo, periods,
bounded root-set tempo, unknown profiles and native-height/epoch overflow.

Fresh production selects `urnetwork-subtensor-tempo-drift-v1` only from an
independently authenticated signed approval and requires the exact private
producer runtime capability before encryption/nonce allocation. The signed
approval and prepared record use an optional field; omission preserves the
original JSON/hash/signature interpretation. Historical replay authenticates
the original runtime and source, preserves nonce/signature/epoch/round, and
grants only those retained bytes. Renewal cannot retarget the original profile.
Switching requires a separately reviewed activation and retained prior custody.

Monitor policy explicitly selects the same profile alongside the exact expected
producer/config. No runtime number or metadata shape supplies that choice.
Forecasting warns at the corrected next possible epoch; only actual observed
epoch crossing retains a missed-window incident. Checkpoint reopening also
accepts valid root-set u16 tempos above the owner setter's 50,400 bound.

| Scheduling check | Outcome |
| --- | --- |
| Author exact dd21 implementation plus two frozen causal test files | Five intended failed roots: three CRv4 and two monitor |
| Author normal / race | 55 / 55 roots pass: CRv4 23, validator 19, mainnet 13; zero skips |
| Author vet | `./crv4 ./validator ./mainnet`, exit 0 |
| Post-initialization commit phase omitted | Two intended failures, predicting block 1002 instead of 1101 |
| Root-tempo checkpoint correction omitted | The retained observed incident fails reopening |
| Original signed-profile equality omitted | A new production approval wrongly accepts an unprofiled retained record |
| Independent Sol | Exact-parent five-root causal baseline reproduced; fixed 16/16 normal and 16/16 race pass, zero skips; vet exit 0 |

The scope contains 16 new roots and 39 adjacent roots. The initial runner
declared 18 validator roots, while its prefix correctly ran 19, including the
existing transport-cause test. All 19 passed with exit 0. The original result is
retained with its count mismatch; `census-correction.json` explicitly declares
19 and a disjoint continuation completes remaining jobs without rerunning any
passing work. Earlier development fixture/compile failures remain in the bundle
and are excluded from frozen-source qualification.

- [Author scheduling receipt](/mnt/data/sn-testnet/native-schedule-20261002/evidence/receipt.json), SHA256 `fca632ebc7bd122a9486ed37d3cd97d317dee033b940d6a818c0654adacf0271`.
- [63-entry manifest](/mnt/data/sn-testnet/native-schedule-20261002/evidence/manifest.json), SHA256 `ecb7a0c06b57234c37dc00cbe939eb69afb5e5c650e2c92434d6923dce930114`.
- [Author scheduling bundle](/mnt/data/sn-testnet/native-schedule-20261002/native-schedule-author-evidence.tar.gz), SHA256 `e2c1e3e7ac765fa62310b20d1ef8f73159026ab458952f3b620d97b0acf533c0`.
- [Independent Sol scheduling receipt](/mnt/data/sn-testnet/sol-mainnet-native-schedule-independent-20261002/receipt.json), SHA256 `5835480892b2973e0567ffd8ff5f30b7b83b6b37979f7bc1a0cce990731c625d`. This is the separate 16-root scope; Sol rehashed all 63 author manifest entries and the bundle without relabeling them as independent executions.

New-root selector: `^(TestTempoDrift|TestProductionSchedule|TestMonitorTempoDrift)`
across `./crv4 ./validator ./mainnet`. The baseline copies only
`crv4/schedule_tempo_drift_test.go` and `mainnet/monitor_tempo_drift_test.go` onto
exact `dd21ed00`; the other new tests require the new API. Author compile
overlays retain the exact parent production/shared-fixture bytes. The control
manifest and complete command/root census are retained in the bundle.

## Adjacent review and remaining gates

`owner_trim_bounded.go` already uses the actual tempo to exclude an epoch before
the original era expires. `subnet_preview.go` correctly uses `LastEpochBlock`
for the runtime's admin-freeze scheduling anchor. Both remain unchanged. The
search budget keeps `MaxTempo` as a conservative bound and expands to an actual
larger root-set u16 tempo; it is not the epoch predicate. Both production fresh
preparation callers use the signed selection. Generic/testnet callers and
retained old approvals keep their original model.

Prediction still assumes successful epochs and inclusion at head plus one.
It does not predict per-block epoch-cap deferral, inconsistent-input skips or
future control changes. Actual finalized inclusion, reveal/application and
economic observations remain required. The observer supplies owned-RPC archive
completeness, not independent finality/storage authority. `M`, `Q`, recipient
generations, provider entitlement and actual recycled value stay unknown/null.

Mainnet source/Wasm/metadata authority, actual owner/device approvals, finalized
Recycle mode, exact signed activation and eligible independent validators remain
live gates. Snow's testfinney471 report is not mainnet470 evidence. These source
increments are now included in the separately [qualified exact SN258/server0aa release](release-258e25b4-server0aa1-20261002.md). Its 83 normal/race roots on the current source pair and independent nine-root scope preserve the older-server scopes above. No retained release artifact was relabeled, and local qualification does not grant launch authority.
