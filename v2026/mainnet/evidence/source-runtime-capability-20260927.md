# MG-04 / RT-03, RT-04: atomic source runtime capability

Date: 2026-09-27. Baseline: `b15ad611bab1f9d47567ec5c636e0b61fd31ce8e`.
Toolchain: Go 1.26.6, linux/amd64. This is deterministic source qualification
using synthetic exact approvals, actual metadata encoders and sr25519
signatures. No live chain, active campaign state, owner-trim, root-service or
server files were changed.

## Remaining-gate audit

The miner's `fleet_runtime.go` gate is the default testnet release path.
Independently approved mainnet fleet artifacts already enter through
`fleet_mainnet_runtime.go`; removing the former's default pin would change
approval policy rather than repair the production path.

The validator's `runtime_identity.go` still fixes the release configuration to
the reviewed tuple. It is a remaining production-policy boundary, and cannot
be replaced by trusting whatever the endpoint returns.

The next independently reproducible inner blocker was `source_commitment.go`.
Even after exact caller-approved artifact authentication, a successor failed
both offline source-byte reconstruction and live atomic-call construction
solely because its spec was absent from the compiled encoding list. Encoding
syntax and runtime authority were conflated.

## Correction and invariants

The persisted source schema fixes the atomic call and signed-payload layout.
Its spec is signed domain data. Offline reconstruction now accepts a nonzero
spec within the supported transaction encoding, and still verifies the complete
framing, signer, nonce, calls, payload and real signature. No schema or durable
record was rewritten; no new compatibility marker is needed for strict work.

Strict artifact authentication now issues an unexported immutable witness only
after matching the caller's exact version/code/metadata allowance. The witness
records the connection/cache owner, genesis, selected block, exact identity
and admitted metadata object. Binding preserves it independently of cache
residency. Exported fields cannot move it to another owner or relabel its
identity; failed binding leaves the prior view intact. Cancellation does not
publish a witness. Legacy manual bindings remain usable for their existing
adapters but cannot create a strict successor capability.

A strict successor's live source operation checks that witness and a narrow
profile: `Utility.batch_all`, `Commitments.set_commitment`, the selected normal
or mechanism CRv4 call, extrinsic version, and complete ordered signed-extension
identifiers/types/additional-signed types. Changed indices, argument shapes or
extensions fail. An unused alternate call, unrelated storage or events do not
participate in this encoding capability; their consumers need their own
profiles and exact artifact authority.

The public explicit-block source preparation boundary checks that its block
equals the authenticated witness before any storage read, encryption or nonce
allocation. Retained signed-byte validation instead uses the independently
authenticated current/historical view selected by its caller. A later block
with the same exact artifact can verify the original bytes without re-signing;
a different runtime signing domain cannot. Provisional source markers must
match independently authenticated provisional authority at the live boundary,
including when a caller strips a marker from an otherwise valid signature.

## Deterministic controls

`crv4/source_runtime_capability_test.go` covers:

- Live normal/mechanism atomic calls for an exact-approved future spec,
  compared with independently constructed fixed bytes.
- Real future sr25519 signatures, serialization and cold same-artifact
  authentication at a later block without changing signed bytes.
- Valid offline candidates with no authentication proof, fabricated exported
  artifacts, foreign connections and relabeled block/version/code/metadata
  coordinates; no candidate reaches a submission Rpc.
- Changed call indices/arguments, pallet indices, extension order/identifiers,
  an extra zero-size extension and extrinsic version.
- Operation-specific admission when the unused mechanism call changes, and
  stale-block refusal at the actual public preparation boundary before I/O.
- Original signing-domain rejection, cancellation before witness publication,
  and explicitly joined concurrent validation during bounded cache eviction.

Historical regression assertions now test real signature or live authority
refusal instead of treating the pure byte encoder as an authority check.
Their retained signatures still fail after runtime relabeling. The provisional
regression checks both the original provisional view and the strict view after
a marker is stripped.

Negative control: a Go source overlay restored only baseline
`crv4/source_commitment.go`. Both causal regressions failed as expected:
`BuildsApprovedFutureCall` reported `source metadata is not the reviewed runtime`,
and `RetainsFutureSignedBytes` reported `source runtime or CRv4 parameters are
unsupported` (package 0.530s). No checkout or live file was replaced.

## Reproduction and results

Run from the repository root:

```sh
source_capability_tests='^Test(SourceRuntimeCapability|SourceCommitment|ProvisionalRuntime|RuntimeArtifact|Fleet(Mainnet|Provisional|Runtime)|BindFleetRuntime|Release(ProvisionalRuntime|EvidenceV2HistoricalRuntime|CurrentRuntime|Runtime[0-9])|AuthenticatePinnedNativeRuntime)'
go test ./crv4 ./chain ./miner ./validator -run "$source_capability_tests" -count=1 -timeout=180s
go test -race ./crv4 ./chain ./miner ./validator -run "$source_capability_tests" -count=1 -timeout=300s
go test ./chain -count=1 -timeout=120s
go test -race ./chain -count=1 -timeout=120s
go vet ./crv4 ./chain ./miner ./validator
```

Affected normal selection: PASS — CRv4 18.300s, miner 18.161s, validator
28.727s. The same selection passed with race detection: CRv4 151.983s,
miner 146.247s, validator 159.380s. The selection compiled chain without
matching tests; the separate complete chain package passed normally (0.362s)
and with race detection (3.649s). Vet passed. Simulator smoke passed normally
(3.518s) and with race detection (31.762s). Whitespace checks passed.

Simulator binding/history smoke selection:

```sh
source_binding_tests='^Test(ProvisionalRuntimeCompatibility(CurrentHistoricalAndDurableEvidence|BindingRetainsExactProof|RejectsStrictAndInvalidApproval|FinalizedStorageUsesPrivateView|NativeSigningArtifactFence|NativeRebroadcastRejectsUpgrade)|AuthenticatedFinalizedExtrinsic(BindsExactMetadataAndContext|RejectsMismatchedAndIncompleteIdentity)|CurrentRuntimeAuthenticationCannotUseHistoricalCache|ReleaseRuntimeRPCRetryCancellationDoesNotPoisonCache)$'
go test ./sim-testnet -run "$source_binding_tests" -count=1 -timeout=120s
go test -race ./sim-testnet -run "$source_binding_tests" -count=1 -timeout=180s
git diff --check
```

Local logs and the causal overlay are under
`/mnt/data/sn-testnet/evidence/source-runtime-capability-20260927/`.

## Limits

MG-04 / RT-01 through RT-08 remain in progress. This capability assumes the
caller's independent exact artifact approval; it cannot mint a production
approval from observed Rpc bytes. Existing reviewed historical adapters and
testnet provisional admission retain their own boundaries. The validator
release configuration gate, broader production upgrade policy, remaining
native operations, source storage/API profiles, upgrade-block execution
metadata, uncertain-send reconciliation and a composed controlled upgrade
remain separate work. No new write was launched or runtime version approved
by this patch.
