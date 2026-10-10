# Original production authority qualification

Date: 2026-09-28 UTC. Worktree:
`/mnt/data/sn-testnet/worktrees/sn-mainnet-production-history-20260927`.
Base producer source: `cbbd7e89f7a47870b4316d2ca4983a5735d4382d`.
Sealed upload source: `da733df9` plus `61472087`, replayed locally as
`24f0b019` and `a7770d79`. The integration branch already contains those changes;
the authority-history commit is the only additional commit to integrate.

This is local source qualification with synthetic independently signed configs,
fixture keys, real proof/signature/SCALE readers, scripted native storage and
local EVM HTTP. No live node, custody key, chain transaction, service deployment
or mainnet approval was used. Tests create real fixture signatures and prepared
transactions; they do not submit them or assert a native economic outcome.

## Scope

`production_authority_history` selects bounded complete original configs,
signed approval bytes and runtime documents. Exact signed predecessor prefixes
preserve lineage. Current signing and original read authority remain separate.
Content-addressed retention is immutable and idempotent, including after partial
progress. Restart can reload after provisioning sources disappear. This continuity
class preserves economic policy, identities, operators, route/custody and proof
bounds; it permits independently approved runtime/approval renewal and polling
changes. It does not approve arbitrary policy or custody migration.

The regressions cover:

- A genuine signed production source/sidecar reproduces byte-for-byte under its
  original complete authority after the current runtime changes. Its old private
  prepared grant cannot be promoted to current signing authority.
- A new genuine prepared decision retains the original drain, despite nonzero
  pending emissions at the new decision block. The original runtime window is
  derived from its retained bundle without a duplicate tuple-only document.
- The default YAML loader survives loss of current/original provisioning approval,
  authority-bundle and runtime-document files after durable retention.
- Multiple independently signed renewals preserve the exact prefix and detached
  runtime windows. Altered config/approval/schema, changed policy, reordered
  history, unknown sidecars, conflicting artifacts, excessive count/bytes and
  cancelled export are refused.
- Cold source capture uses original economic authority. Original pending recovery
  reaches the actual canonical receipt reader. Later receipt and old application
  reads reach their real native readers through the correct approved windows.
  Deliberate receipt/body/weight unavailability is preserved as an error; these
  controls do not fabricate successful inclusion, application or missed epochs.
- A real empty finalized receipt scan produces a distinct pending wait for both
  same-artifact and upgraded renewals. The actual loop polls 13 times without
  spending its ten-hard-failure budget or classifying the epoch as complete.
  A later epoch invokes reconciliation again; its error remains visible.
  Joined integrity, previous hard causes, wrong epoch and cancellation controls
  retain their original ownership and failure semantics. Verbose qualification
  retains each actual wait message, including transaction identity and epoch.
- A typed receipt timeout after an empty scan and across an epoch boundary keeps
  the same read-only pending owner. The original timeout remains accessible via
  `errors.Is` and in the wait diagnostic. A timeout joined with integrity, or
  cancellation alone, cannot acquire this retry outcome. No scan-prefix cache is
  added; complete block-response validation remains separate PH-03 follow-up.
- The ordinary signed upload crosses the real server-used admission constructor,
  refresh and lease using a renewal-bundle-only historical activation window.
  Current eligibility and expiry remain separate; no producer capability is
  acquired, and the detached projection does not reopen deleted authority files.

## Checks

The initial expanded owner/runtime/history selection passed normally (177.036s)
and under race detection (292.706s). Its subsequent late-receipt and application
regressions passed independently (4.181s and 5.085s), as did the upload renewal
regression (1.107s). An initial upload fixture accidentally changed only the
native hash, leaving its old height; the strict reader rejected it. The fixture
now changes both coordinates, with no runtime relaxation.

The 78-root affected normal selection passed in 188.935s. Its race results are
recorded below. The selection includes all upload, intent,
archive, historical-runtime and native-observation roots affected by propagation.
The subsequent pending-wait correction receives its own complete affected loop
normal/race selection; prior unaffected results are retained rather than rerun.

```sh
go test ./validator -run '^Test(ProductionAuthorityHistory|ValidatorUpload|IntentV2|ReleaseArchiveV2|ReleaseEvidenceV2HistoricalRuntime|ReleaseNativeObservationV2)' -count=1 -timeout=600s
go test -race ./validator -run '^Test(ProductionAuthorityHistory|ValidatorUpload|IntentV2|ReleaseArchiveV2|ReleaseEvidenceV2HistoricalRuntime|ReleaseNativeObservationV2)' -count=1 -timeout=600s
go test ./validator -run '^Test(ProductionAuthorityHistoryPendingWait|ReleaseSteeringLoop|ReleasePreparation|ReleaseShutdownSteering)' -count=1 -timeout=300s
go test -race ./validator -run '^Test(ProductionAuthorityHistoryPendingWait|ReleaseSteeringLoop|ReleasePreparation|ReleaseShutdownSteering)' -count=1 -timeout=300s
go test ./validator -run '^TestProductionAuthorityHistoryPendingReceiptTimeout' -count=1 -timeout=180s
go test -race ./validator -run '^TestProductionAuthorityHistoryPendingReceiptTimeout' -count=1 -timeout=180s
go vet ./crv4 ./validator
git diff --check
```

Raw logs, root list, exact predecessor overlays and their Go driver are retained
at `/mnt/data/sn-testnet/qualification/production-authority-history-20260928`.
The affected loop selection has 26 roots: normal 19.693s, race 68.384s. The
receipt-timeout extension passes normal 4.410s and race 19.219s. The final visible
wait assertion passes normal 8.989s and race 35.608s; its verbose normal log
contains 26 actual transaction/epoch wait messages from the two renewal cases.
Vet and formatting checks pass.

The combined broad race selection hit its cumulative 600s deadline during
`TestReleaseArchiveV2CancellationCannotPublishHistory`, 15s into that fixture's
ordinary signature verification. The retained stack is CPU work, not a race or
assertion failure. It is retained as `affected-race.log`, not relabeled as a pass.
The same affected source is qualified in three bounded selections:

```sh
go test -race ./validator -run '^Test(ProductionAuthorityHistory|ValidatorUpload)' -count=1 -timeout=600s
go test -race ./validator -run '^TestReleaseArchiveV2' -count=1 -timeout=900s
go test -race ./validator -run '^Test(IntentV2|ReleaseEvidenceV2HistoricalRuntime|ReleaseNativeObservationV2)' -count=1 -timeout=600s
```

All three split selections pass: history/upload in 280.852s, archive in
506.965s, and intent/historical/native readers in 111.467s. Together with the
separate affected loop selection, these qualify the final source under race
detection without discarding the retained cumulative-timeout result.
Already compiled qualification bodies were left intact when the physical-cache
profile was requested. Subsequent Go invocations use
`GOCACHE=/mnt/data/sn-testnet/gocache`, capture-owned `TMPDIR`, `GOWORK=off` and
`GOMAXPROCS=2`; the last narrow timeout control uses that explicit profile.
The 27-file source manifest `source.sha256` has SHA-256
`d313054989dd445a97f25a5d544c5cc2a8706b687bb0d7c77dde71ba74d74778`.

## Exact predecessor controls

Each Go overlay restores one complete predecessor file while retaining the new
regression. The controls require the named test assertion to fail; compilation
failure, a race report or timeout cannot count as a successful causal control.

| Predecessor file | Regression | Required observed defect |
| --- | --- | --- |
| `cbbd7e89:validator/release_capture_native_v2.go` | `TestProductionAuthorityHistoryCaptureRoutesOriginalSidecar` | Old source is interpreted under the current approval and its original block is rejected. |
| `cbbd7e89:validator/recycle_production_observation.go` | `TestProductionAuthorityHistoryContinuesWithoutAnotherDrain` | A renewed decision loses the original economic drain boundary. |
| `61472087:validator/validator_upload_runtime.go` | `TestValidatorUploadProductionRuntimeRetainsRenewedAuthorityWindow` | Tuple-only projection omits the original bundle's runtime window and rejects the signed upload. |
| `cbbd7e89:validator/release_history_adoption_native_v2.go` | `TestProductionAuthorityHistoryApplicationReachesOriginalWeightReader` | The compiled artifact lookup rejects the original approved application read before actual weight evidence. |
| `cbbd7e89:validator/release_steer.go` | `TestProductionAuthorityHistoryPendingWaitKeepsLoopAlive` | Authenticated old-pending receipt waits consume the fatal failure budget and stop observation after ten polls. |

Reproduce with `go test [-race] -overlay <overlay.json> ./validator -run
'^<regression>$' -count=1 -timeout=240s`; each overlay selects the corresponding
`git show <revision>:<path>` bytes without changing the worktree.
All five controls reproduce the specified assertion in normal and race modes.
A sixth narrow semantic overlay restores only the old plain receipt-error branch
in `reconcilePendingV2`, leaving the empty-scan wait intact. Both modes fail
`TestProductionAuthorityHistoryPendingReceiptTimeoutKeepsObservation` at the
second actual receipt scan because its timeout loses the typed pending outcome.

## Remaining limits

This retains approved progress across compatible config renewal; each new
artifact/window still requires independently approved exact config authority.
Automatic admission under a preapproved interface/semantic policy is separate
RT-04 work. Missing native epochs still require their existing authenticated
gap/adoption evidence. Full live producer/server bootstrap, actual archive RPC,
receipt/application success, approval provisioning, source/code provenance and
the requested native-miner/recycle economic outcome remain live qualifications.
