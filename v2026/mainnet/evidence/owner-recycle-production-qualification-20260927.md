# Initial production owner-recycle qualification

Base: `32b6a19c91e5275bc44a6407ecedf1a8b58e1e16`.
Worktree: `/mnt/data/sn-testnet/worktrees/sn-mainnet-runtime-producer-20260927`.
Date: 2026-09-27 UTC. Go: `go1.26.6 linux/amd64`.

This is local source qualification of the standard V2 validator's initial
owner-recycle producer path and recovery under the same independently signed
config. The [runtime qualification](production-runtime-qualification-20260927.md)
covers its exact-artifact config, capability and historical-read half.
No mainnet keys, endpoint, approval, deployment or transaction were supplied.

## Actual exercised boundaries

The fixture starts with real two-operator provider proof transcripts and a
separately signed production approval. Native responses contain exact SCALE
storage and selective-metagraph API bytes; EVM observations use the actual
canonical coordinator readers. Every instance owns its responses and synthetic
key material. No fixture returns an approved proof, stake, runtime or submission
verdict.

The positive path performs full provider replay, independently observes owner
recipients and operators, observes eligible distinct validators and the pinned
drained activation, and derives the exact 10/90 input row. It then calls the real
CRv4 encryption and atomic source/weights preparation, retains the unchanged V2
provider envelope, creates the additional sr25519 production seal, and replays
the source, sidecar and prepared weights. A config alone cannot acquire the
private exact-prepared grant used by submission.

Deterministic controls cover advancing finalized head, cold native caches,
source-approval loss after retention, continuing the signed epoch window, late
first-intent refusal, cancellation, exact approval/row/source/envelope/prepared
substitution, bounded SCALE lengths and changed consumed storage metadata.
An independently signed replacement config is valid in its own scope but
cannot reinterpret the original sidecar; the original retained proof remains
valid after that refused substitution.
Actual native response faults cover insufficient weighted stake, stale/future
activity, shared validator coldkeys, nonzero pending emissions and a changed
activation epoch. Restoring the real response bytes restores admission.
The original decision proof excludes advancing finality witnesses; canonical
finality is rechecked without changing immutable historical decision facts.

The private-grant control exercises actual measurement/sidecar/native replay,
then the grant and exact prepared-byte verifier in isolation. It is not a claim
that a complete live `RunRelease` worker deployment or on-chain outcome ran.
Existing affected intent, startup and archive regressions are included below.

## Verification

The following completed successfully:

```sh
go test ./validator -run '^Test(OwnerRecycle|IntentV2|ReleaseMeasurementEnvelopeV2|ReleaseEvidenceV2Archive|ReleaseArchiveV2|ReleaseNativeSource|NativeSourceHash)' -count=1 -timeout=600s
# validator 146.652s

go test -race ./validator -run '^TestOwnerRecycleProduction' -count=1 -timeout=600s
# validator 101.868s before the final first-intent/approval-bound guards

go test ./validator -run '^TestOwnerRecycleProduction' -count=1 -timeout=240s
# validator 37.123s with those guards

go test -race ./validator -run '^TestOwnerRecycleProduction' -count=1 -timeout=360s
# validator 119.602s with those guards

go test ./validator -run '^TestOwnerRecycleProductionRejectsDifferentApprovedConfig$' -count=1 -timeout=180s
# validator 4.743s; final additional original-config substitution control

go test -race ./validator -run '^TestOwnerRecycleProductionRejectsDifferentApprovedConfig$' -count=1 -timeout=240s
# validator 17.003s

go vet ./crv4 ./validator
git diff --check
```

Raw command logs are retained under
`/mnt/data/sn-testnet/evidence/mainnet-owner-recycle-production-20260927`.
The final sorted 42-Go-file manifest there has SHA-256
`5819180fbdc6aeff640306161c0d578138ec99ac9654e15caf6cf864ca489fbf`.

## Causal controls

A Go-generated overlay restores only the two old semantic restrictions while
retaining the new interfaces so both tests compile and reach the real readers:

1. Remove production admission from `ownerRecycleProductionBoundary`.
2. Restore the first-epoch-only measurement restriction instead of the finite
   independently signed production interval.

```sh
go test -overlay=/mnt/data/sn-testnet/evidence/mainnet-owner-recycle-production-20260927/old-gates-overlay.json ./validator -run '^TestOwnerRecycleProduction(HistoricalReplaySurvivesHeadAndCacheChanges|ContinuesApprovedEpochWindow)$' -count=1 -timeout=180s
```

This command fails both intended assertions (13.938s): the real signed producer
still reaches the old blanket refusal, and the next approved epoch is rejected
as differing from the first decision. The same selector with `-race` also fails
both intended assertions (38.507s). These are expected causal failures, not
product-suite failures. The overlay sources have SHA-256 values:

- approval: `f3ef051c67055dcc2dd9b5cd9677b8114c3683db53ffc9900a4bd78ca4c8d7d0`
- measurement: `ff1d7f37975780624cb2538fe358cbc92e07e98855db5f914dcda230b5a03994`

## Remaining scope

Finalized inclusion, reveal/application and the actual 10% native-miner /
90% recycle economic result are monitored postconditions. They are not claimed
by the synthetic prepared transaction and must not be required before the first
submission can happen. No reserve credit is implied by recycling.

Changing an already-running complete production config needs durable original
config/approval history. Runtime tuple history alone does not grant authority to
reinterpret an earlier sidecar. The current patch rejects that reinterpretation
and retains the original signed bytes; the concrete next design is recorded in
[the production transition document](../OWNER-RECYCLE-PRODUCTION.md).
MG-04/RT-04 and MG-06 remain open for that migration, downstream admission,
production receipt/restart qualification, real launch inputs and native outcome.
