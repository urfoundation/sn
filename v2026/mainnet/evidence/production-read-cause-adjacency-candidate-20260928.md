# Production read-cause adjacency — qualification pending

The [subsequent qualification receipt](source-read-cause-qualification-20260928.md)
records completed normal/race scopes, causal controls and integration. The
original author handoff below retains its earlier status.

Date: 2026-09-28 UTC. Worktree:
`/mnt/data/sn-testnet/worktrees/sn-mainnet-production-read-waits-20260928`.
This follows source-read candidates `9ae7f1f4` and `08bb86fc`; together they are
one bounded PH-03 RPC-cause batch for Terra medium qualification. Astra authored
the source and fixtures; no test pass, live mainnet effect or completed PH-03
claim is made. Formatting and diff checks pass.

The adjacent production callers had the same causal defect: they combined an
RPC failure with a statement about values that had not been returned. The
strict all-causes retry classifier then treated a pure outage as an integrity
incident. This slice returns the original wrapped read error first and keeps
the existing contradiction check only after a successful read:

- Miner EVM recovery: chain-id read, finalized receipt head, canonical receipt
  block and finalized nonce. Dial failure aggregation retains its actual causes.
  ABI readback decoding and signed transaction serialization remain hard; their
  preceding RPC reads already returned errors separately.
- Validator owner census: exact runtime authentication; RecycleOrBurn,
  SubnetOwner, MechanismCountCurrent and reverse Uids storage.
- Production eligibility: the signed activation header and its exact
  PendingServerEmission/SubnetEpochIndex storage reads.
- Activation setup: final EVM snapshot recheck after actual preparation, and
  finalized/canonical EVM reads when reopening retained signed preparation.

No RPC approval, metadata compatibility, ownership, event, nonce or economic
check is relaxed. Missing and pruned inputs remain distinct from returned
contradictory values. Local filesystem, decoding, signature and private custody
error joins are unchanged. This batch does not modify retry duration, grants,
service failure counters or epoch transitions.

The new miner regression invokes real bind commands, persists/signs/sends one
synthetic transaction and loses its acknowledgment. It removes fresh consent
inputs, injects HTTP 503 at the exact next recovery read, then reopens the
signed journal and finishes the original inclusion. It checks the complete
error tree, unchanged raw bytes/nonce/hash, and exactly one send/gas-price
preparation. The HTTP failure hook is instance-owned and acts only at the
physical response boundary.

Validator census tests retain actual signed approval and inject native RPC
deadlines at each affected reader. Production eligibility tests first build
the real measured production stage, then fail the late activation reads.
Activation tests run actual native stake/schedule readers and exact coordinator
ABI views, exhaust the existing four-attempt physical read budget without
sleep-based assertions, and recover the same private file and dual signatures.
Returned contradictions and mixed decoder/transport failures remain hard.
These tests do not claim successful nonempty V2 intent begin/update/restart or
full standard RunRelease continuation; that remains the next bounded batch.

Run the following on the final combined candidate with a physical per-capture
`TMPDIR`, `GOCACHE=/mnt/data/sn-testnet/gocache`, `GOWORK=off`, `GOMAXPROCS=2`:

```sh
go test ./crv4 -run '^Test(Receipt|VerifyFinalizedExtrinsicContext|LocateFinalizedExtrinsic|SourceCommitment|SourceRuntimeCapability)' -count=1 -timeout=300s
go test -race ./crv4 -run '^Test(Receipt|VerifyFinalizedExtrinsicContext|LocateFinalizedExtrinsic|SourceCommitment|SourceRuntimeCapability)' -count=1 -timeout=600s
go test ./validator -run '^Test(ReleaseSource|ReleaseEvidenceV2HistoricalRuntime|OwnerRecycleAdmission|OwnerRecycleProduction(Read|Eligibility|Cancellation)|OwnerRecycleRead|ReleaseActivation(Preparation|Retained|Setup|V2))' -count=1 -timeout=600s
go test -race ./validator -run '^Test(ReleaseSource|ReleaseEvidenceV2HistoricalRuntime|OwnerRecycleAdmission|OwnerRecycleProduction(Read|Eligibility|Cancellation)|OwnerRecycleRead|ReleaseActivation(Preparation|Retained|Setup|V2))' -count=1 -timeout=1200s
go test ./miner -run '^TestFleet(Mainnet|Recovery)' -count=1 -timeout=600s
go test -race ./miner -run '^TestFleet(Mainnet|Recovery)' -count=1 -timeout=600s
go vet ./crv4 ./validator ./miner
git diff --check
```

The source-read causal controls are documented in
[the preceding receipt](source-finality-read-candidate-20260928.md). Additional
normal and race controls overlay only the named predecessor source from
`08bb86fc`, keeping new fixtures and tests:

| Predecessor source file | Exact causal test selector |
| --- | --- |
| `miner/fleet_recovery_evm.go` | `^TestFleetRecoveryEvmReadFailuresPreserveOriginalWork$` |
| `validator/recycle_observation.go` | `^TestOwnerRecycleAdmissionReadPreservesTransportCause$` |
| `validator/recycle_production_observation.go` | `^TestOwnerRecycleProductionReadPreservesTransportCause$` |
| `validator/release_activation_setup_v2.go` | `^TestReleaseActivation(PreparationReadPreservesTransportCause|RetainedReadPreservesSignedWork)$` |

Each control must reach its actual failing RPC and fail the named assertion
that the pure read failure became a contradiction. Compile errors, setup
failures, unexpected nil/panic, race reports or test timeouts do not reproduce
the cause. Run independent diagnostics to completion and report all failures
together; do not keep restarting an unchanged qualified body.

After qualification, PH-03 still needs retained intent reconciliation before
unrelated current snapshots, explicit production waits and visible missed/unknown
epochs, a real nonempty durable V2 owner fixture, receipt-prefix persistence,
and exact common receipt/nonce/expiry coverage. Miner bounded subrange progress
also remains open. The qualified monitor producer is a separate change; this
batch does not import its hooks or make an observation callback into authority.
