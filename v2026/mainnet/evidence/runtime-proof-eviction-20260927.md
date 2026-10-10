# RT-06: retain runtime proof authority across metadata eviction

Date: 2026-09-27. Source baseline: `cfa796ab`. Toolchain: Go 1.26.6,
linux/amd64. This is a source-level, deterministic qualification of the
in-process proof lifetime fix. No live chain, running campaign, release lock,
owner-trim code or NetEscrow code was changed.

## Failure and correction

The separate eight-entry provisional runtime metadata cache doubled as an
authorization registry. `RuntimeArtifactCompatible` required residency, and
`CurrentRuntimeCompatibilityProfile` searched resident metadata pointers.
Admitting a ninth compatible runtime could therefore revoke an already
authenticated artifact between authentication and binding, or reject a valid
retained source signature using a bound historical view. An exact prior pin
also lost authority when its metadata left the cache.

An authenticated artifact now carries an unexported immutable proof containing
its connection owner, exact runtime/code/metadata identity and admitted metadata
pointer. `BindRuntimeArtifact` retains that proof with its independently owned
chain view. Cache eviction only removes reuse entries; it cannot revoke held
proofs. No unbounded authority registry was added.

Reusing a bound proof still performs four fresh identity RPC reads: runtime
version, genesis, consumed runtime API versions and runtime code hash. The
exact-block code/version tuple and any explicit metadata pin must agree. It
reuses the admitted metadata/profile result and durable observation rather
than repeating them. A new connection establishes its own authority and
observation; it cannot inherit another owner's proof or turn an unobserved
provisional pin into its own admission baseline.

The shared chain, miner, validator and simulator binding paths preserve this
proof. Strict bindings clear it. A missing proof, modified identity, altered
metadata pointer, stripped compatibility marker or foreign owner fails before
changing the prior view. Signature validation retains the original runtime
domain; this does not make a signature valid under a different runtime.

## Deterministic coverage

`crv4/runtime_compatibility_proof_test.go` forces actual bounded-cache eviction
and chooses the evicted entry without depending on map iteration order. It
checks authentication-to-binding eviction, held historical views, a real
synthetic source signature, exact historical reuse without metadata or observer
repetition, fresh identity rejection, cold-owner reauthentication, and rejection
of forged or relabeled proofs. Two explicitly joined workers test cache churn
concurrently with reads of one immutable bound view. No sleeps or live RPCs
control these state transitions.

`sim-testnet/runtime_compatibility_test.go` also checks the wrapper boundary:
the original authenticated artifact must agree with its wrapper, and failed
binding preserves the prior runtime view.

Negative control: a Go source overlay restored only the baseline
`RuntimeArtifactCompatible` and `CurrentRuntimeCompatibilityProfile` bodies.
`TestProvisionalRuntimeProofSurvivesMetadataEviction` then failed with
`metadata eviction revoked an already authenticated runtime proof` (1.398s).
The same regression passes with the correction. The overlay never modified
the checkout or any campaign state.

## Validation

Run from the repository root. The broader affected normal selection passed:

```sh
go test ./crv4 ./chain ./miner ./validator \
  -run 'Test.*(Runtime|Provisional|FleetMainnet)' -count=1 -timeout=240s
```

Results: CRv4 13.942s, chain 0.232s, miner 18.184s, validator 225.817s.
The same selection with `-race` passed CRv4 (108.556s), chain (3.417s) and
miner (138.840s). The validator package exceeded the total 240s budget while
`TestReleaseRuntimeV2EvidenceMetadataLargerThanStreamPages` had been running
for seven seconds. That broad race run is not claimed as passing.

The focused validator race checks are reproducible with:

```sh
go test -race ./validator \
  -run '^Test(AuthenticatePinnedNativeRuntime|ReleaseRuntime(459Retains|461Preserves|467Requires|461Rejects)|ReleaseCurrentRuntime|ReleaseEvidenceV2HistoricalRuntime|ReleaseEvidenceV2DecisionReadsReviewedHistoricalRuntime|ProvisionalRuntime)' \
  -count=1 -timeout=180s
go test -race ./validator \
  -run '^Test(ReleaseProvisionalRuntime|ValidatorUploadProvisionalRuntime|ReleaseSourceRoleReadbackRequiresOwnedRuntimeCompatibility)' \
  -count=1 -timeout=120s
```

Both passed: identity/history selection 97.697s; provisional selection 64.167s.

The exact simulator selection, run normally and with `-race`, is:

```sh
runtime_proof_tests='^Test(ProvisionalRuntimeCompatibility(CurrentHistoricalAndDurableEvidence|BindingRetainsExactProof|RejectsStrictAndInvalidApproval|FinalizedStorageUsesPrivateView|NativeSigningArtifactFence|NativeRebroadcastRejectsUpgrade)|ProvisionalDoctor(RequiresReadOnlyExactApproval|RetainsApprovedRouteAndAuthenticatesSuccessor|RejectsApprovalDriftBeforeWriting|StrictDefaultKeepsAuthority|AuthenticatesNonAdjacentSuccessors)|AuthenticatedFinalizedExtrinsic(BindsExactMetadataAndContext|RejectsMismatchedAndIncompleteIdentity)|ProvisionalRelayCaptureRuntime468SnapshotCannotAuthorizeCurrentOperation|CurrentRuntimeAuthenticationCannotUseHistoricalCache|ReleaseRuntimeRPCRetryCancellationDoesNotPoisonCache|FleetMirrorRecoveryRequiresExactHistoricalAndCurrentState)$'
go test ./sim-testnet -run "$runtime_proof_tests" -count=1 -timeout=180s
go test -race ./sim-testnet -run "$runtime_proof_tests" -count=1 -timeout=240s
```

Results: normal PASS, 9.088s; race PASS, 71.156s.

An earlier broader simulator selection hit its 180s package timeout in
`TestProvisionalRuntimeCompatibilityPreviewMatchesActualRender`, with the
stack in config rendering's `atomicWrite` / `os.File.Sync`. It is not counted
as passing. The exact selection above avoids that unrelated rendering test.

`go vet ./crv4 ./chain ./miner ./validator` passed. Including `./sim-testnet`
reports two pre-existing lock-copy warnings in the untouched file
`sim-testnet/evidence_relay_provisional_continuation_test.go`, at lines 139
and 218. They copy `evidenceRelayRuntime`, which contains `sync.Mutex`.
No unrelated test cleanup is included. `git diff --check` passed.

Local command logs and the negative-control helper/overlay are under
`/mnt/data/sn-testnet/evidence/runtime-proof-eviction-20260927/`.

## Remaining qualification

RT-06 remains in progress. This correction establishes in-process proof
ownership independently of cache capacity. It does not establish durable
cross-process trust, single-flight provisional admission, automatic production
runtime admission or composed release qualification. Cold caches still
authenticate under explicit authority. Strict mainnet consumers continue to
reject a provisional profile, and unsupported interfaces and changed signing
domains continue to fail.
