# Successor canonical adapter qualification

**Scoped independent qualification complete.** Ten focused and twenty-two
adjacent roots pass normal and race in eight package PASS/exit-zero streams.
Fourteen isolated causal controls reach the intended assertion in normal mode,
and six selected controls do so under race detection. This qualifies the scoped
canonical adapter and unavailable-public-send boundary; it does not supply the
missing production history authenticator or live authority.

The corrected source is `a7186754bb0eae696a54b4942fedd807a4af2c95`, tree
`f86fedd1f5255dbf240c27525e9c8c3e9d011319`. The author worktree is
`/home/by/urnetwork/sn-successor-canonical-provenance-owner-20260930`; Sol's
independent positive worktree is
`/home/by/urnetwork/sn-canonical-provenance-validation-20260930`.
Sol (`gpt-6-sol`, medium reasoning) runs behavioral qualification. Astra
(`gpt-6-astra`, max reasoning) implements, debugs and independently reviews
the evidence; Astra runs no behavioral tests. Author compilation with
`go test -c -p 1 -vet=off ./mainnet`, package vet and formatting checks pass.
This documentation child changes no Go source, test source or module selection.

**Public submission remains unavailable.** The mandatory signed Safe
deployment/storage-history envelope is authenticated review input. There is no
production `bootstrapSuccessorSafeProvenanceAuthenticator`; neither review
files nor command flags install one. Public `--submit` returns exit two before
custody loading, networking or attempt reservation. The full local execution
fixtures explicitly inject a synthetic history capability and do not qualify
an arbitrary deployed Safe's provenance. Read-only historical reconciliation
remains available. No live RPC, signing, transaction, installation or mainnet
authority is part of this qualification.

Raw evidence is retained at
`/home/by/urnetwork/temp/canonical-provenance-validation-a7186754`.
The author handoff remains separately sealed at
`/tmp/successor-canonical-provenance-handoff-20260930`; its forty-eight-file
`SHA256SUMS` hashes to
`d95204387f0d5f754f81e4203487b79a0240d7ba5918ac3b019b474c0bdc3a18`.
Astra independently inspected the eight complete positive logs, exact root
censuses and all twenty complete control logs and named assertions, matched
control patch hashes and source identities, and verified both checksum
manifests. The author and independent positive worktrees remain clean at the
tested commit. All six local replacement commits/trees are clean and match
both source fences; module file and resolved-graph hashes match as well.

| Sealed Sol evidence | SHA-256 |
| --- | --- |
| `manifest.json` | `22f19f0c1a08f7e3a719390f2021aeed3de428d9c65fa4683d0b450821327ae8` |
| `SHA256SUMS` (34 files) | `1db9e549b0b7d8bab1e82de2d7af1d4f02d5378395b37c1e0d24650442e26600` |
| `control-results.jsonl` | `7612e4031ee03b512770a017f984a61313eb74fca5d76dee84a5d50a896c81eb` |
| `control-race-results.jsonl` | `4069c8a1c64d82cd9dddd90100e6e429ec2c073cad8634c7df9efbcdbfced625` |

## Positive streams

Each completed stream has the exact RUN/PASS census, package PASS and no failed
or skipped roots, panic, timeout or race report. Heavy fixtures use separate
package invocations and explicit harness budgets. They do not share one overall
eight-receipt timeout or alter production transaction deadlines.

| Stream | Roots | Package duration | SHA-256 |
| --- | --- | --- | --- |
| `light-normal.log` | 8/8 | 5.446s | `17d2e4f8912b7ff8b0d43ff55be76bb5ba6df7e9d3c50b1fea16ea539fd4d902` |
| `light-race.log` | 8/8 | 33.747s | `9a0c296d7773b90030093109e2fb8caf922e707b3cb6eeb747dff2b569e417b9` |
| `command-normal.log` | 1/1 | 51.406s | `40b47b7e61ae3ccf9ca748b3a533ec19a3838daf4047d7054d3af9f828d09acb` |
| `command-race.log` | 1/1 | 349.951s | `264b88533a2a8e77df7f74df4e0ee4ce7986c8909d666582b85cfc9a229958f6` |
| `adapter-normal.log` | 1/1 | 45.496s | `9bad6eeae451b6f55595a9bcd6c130a976d3ff6aa401fa862db5424513f4dee1` |
| `adapter-race.log` | 1/1 | 297.814s | `d71c06d7879c0c72837f11d2a3bd8c9307bcfc52cf02b6aaa1018db6c8fa8cc7` |
| `adjacent-normal.log` | 22/22 | 94.183s | `c808628f268a5029c5efbe1dac4310e5f94554c7ad36297c9cae560c87fcf2ba` |
| `adjacent-race.log` | 22/22 | 540.635s | `d080307b674671c5807e00ae1d6c6f632bf7206b286f994e002a494701a9e360` |

The environment is `GOMAXPROCS=2 GOPROXY=off`, with `go test -p 1 -count=1 -v`
and `-race` added for race runs. Normal and light-race package limits are twenty
minutes with twenty-five-minute process limits. Separate heavy and adjacent
race packages have thirty-minute limits with thirty-five-minute process limits.
Selected race controls use those same thirty/thirty-five-minute limits.

The focused roots have the common `TestBootstrapSuccessorCanonical` prefix:

- `IndependentAuthority`
- `CountedAuthoritySurvivesRestart`
- `InterruptedAuthorityPublication`
- `SafeAuthority`
- `ExactPendingLookup`
- `ReceiptFields`
- `ProvenanceBindsExactScope`
- `OrphanAuthorityCannotEnableSubmission`
- `CommandExecutesAndRecovers` (separate heavy invocation)
- `AdapterBoundaries` (separate heavy invocation)

The adjacent census is:

- `TestBootstrapSuccessorExecutionCommandReconstructsAndRetainsV3Custody`
- `TestBootstrapSuccessorExecutionRecoversCountedAttemptInterruptions`
- `TestBootstrapSuccessorExecutionRecoversInterruptedCanonicalOutcome`
- `TestBootstrapSuccessorExecutionRequiresCanonicalEightReceiptAdoption`
- `TestBootstrapSuccessorExecutionRechecksAfterReservation`
- `TestBootstrapSuccessorExecutionRequiresCanonicalInnerSuccessAndBinding`
- `TestEvmCreateRejectsCorruptCanonicalPostconditions`
- `TestEvmEvidenceCreateHistoricalReceiptAfterRuntimeChange`
- `TestFinalizedMappingAuthenticatesUnequalHeightsAndExactRlp`
- `TestFinalizedMappingParsesVariableFrontierVectors`
- `TestFinalizedMappingNormalizesRetainedHeaderHex`
- `TestFinalizedMappingRejectsUnsupportedAndMalformedDigests`
- `TestFinalizedMappingUnavailableRawHeaderHasNoFallback`
- `TestFinalizedMappingRejectsWrongRawHeader`
- `TestFinalizedMappingRejectsChangedCanonicalEvidence`
- `TestFinalizedMappingRejectsWrongApprovedNetworkBeforeEvmReads`
- `TestFinalizedMappingRpcMethodsAreScopedAndBounded`
- `TestFinalizedMappingCancellationReturnsNoPartialProof`
- `TestFinalizedMappingCommandRetainsProofAndUnavailableExit`
- `TestSafeExecutionOutcomesFollowPinnedContractAndNonce`
- `TestSafeExecutionOutcomeRejectsForgedOrIncompleteEvents`
- `TestSafeExecutionReceiptBoundsKeepUnrelatedEventsHonest`

## Causal scope

Each patch is applied alone to a clean isolated control checkout of the corrected
source. All fourteen normal executions and six selected race executions report
the exact intended root/assertion, root/package FAIL and exit one. Build errors,
timeouts, panics and race reports are not accepted as causal evidence. No patch
is part of the positive source. This is fourteen normal controls and six race
controls, not fourteen pairs.

| Control | Modes | Removed barrier and observed assertion |
| --- | --- | --- |
| `independent_signature` | Normal | Canonical approval signature; `canonical authority accepted old signature domain`. |
| `counted_authority` | Normal | Counted-event authority correspondence; `missing counted authority renewed execution custody`. |
| `safe_proxy` | Normal | Exact proxy runtime; `canonical Safe authority admitted proxy runtime`. |
| `exact_pending_hash` | Normal | Retained-hash pending disposition; `exact pending lookup lost scoped disposition retained pending`. |
| `receipt_gas_bound` | Normal | Retained outer gas ceiling; `canonical receipt accepted gas bound`. |
| `current_aggregate_budget` | Normal | Independent current-read deadlines; `canonical current reads share one aggregate timeout`. |
| `original_runtime_gate` | Normal | Separate current-runtime approval; `unrelated pending account blocked canonical admission` with the runtime-mismatch diagnostic. |
| `post_reservation_admission` | Normal, race | Admission sequence after counted reservation; `canonical submit reused admission from before reservation`. |
| `canonical_position` | Normal | Exact signed bytes at canonical transaction position; `canonical receipt accepted changed inclusion authority transaction position`. |
| `original_receipt_authentication` | Normal, race | Original full canonical receipt authentication; `canonical adapter adopted a disappeared original receipt`. |
| `provenance_exact_safe` | Normal, race | Exact signed Safe address; `canonical provenance accepted swapped signed scope other Safe`. |
| `provenance_independent_signature` | Normal, race | Separate provenance signature domain; `canonical provenance reused execution signature domain`. |
| `provenance_capability_required` | Normal, race | Distinct history authenticator; `signed provenance review substituted for canonical history capability`. |
| `public_provenance_gate` | Normal, race | Public rejection before custody access; `complete signed review unlocked public submission`. |

The last control reaches the fully signed public command path. A later internal
observation still rejects the absent history capability, but cannot substitute
for the required early exit-two boundary before custody mutation. The malicious
Safe fixture uses the pinned published proxy/singleton and actual nonzero owner
and module mapping entries unreachable from otherwise clean sentinel lists.

## Source and dependency custody

Go is version 1.26.6 on Linux/amd64. The unchanged `go.mod` SHA-256 is
`2a948e40658bb403c440ea649140b5ffae53d8b338b0dfc61c90625c708dbc5c`;
`go.sum` is
`2a8d74108e1c331b4b9ebdd8595c02f4b8a98e76f51fedf4c7498c7b41d974ca`.
The resolved `GOPROXY=off go list -m all` output is
`19db0607a41328b8dc09c13292e40a0ee7024650c80ffc3e85e9a870ae225f9b`.
The six local replacements independently match the author and final Sol fences
at these unchanged clean commits and trees:

| Repository | HEAD | Tree |
| --- | --- | --- |
| glog | `892ade4a6be396b32ea82a550f243190b5992180` | `4b3c242905aed44db5879b3d22e860697f666abe` |
| goidenticons | `325750b38314313dc5f44c880ab6f12f6c1ecb3c` | `553487079352d9544edb62850328305e97668790` |
| proxy | `6204ae7df2a9868bbb3a7b61231917a36e4f5c9f` | `730ffeba453bea95f5f23fcf4b3fbc0b45f204ce` |
| server | `80c0e1b7d9fb48ee6f929bca8158ac925b114fe0` | `e78d5078d1b5d5cc39c6167c8e94639bd4f97ea0` |
| userwireguard | `85fb1ca4086fa5dbfcda526bec7a17a894e691b9` | `e83cf65488b192388571b25b1ac85ecebc36eea0` |
| warp | `7498864c7cd3605aad3c43eabfab9008ed7f7228` | `5a54f2db5582403b7b732c8602adeabaacc1a269` |

The preliminary `82da3d40f242b41175ce58fde3b3cf5eabf7fc2c` adapter passed its
eight roots normal/race before discovery of the orphan-mapping authority gap.
Those separate logs are preserved as preliminary evidence and do not qualify
the corrected source or the missing production provenance capability.

## Remaining gates

The exact approved owned RPC supplies canonicality and scoped pending assertions.
Independent Safe build review and complete signer cutover remain explicit
attested assumptions. A local registry cannot establish cross-host exclusivity
or absence of off-node signatures. Current linked-list getters do not prove
complete Safe storage authority.

Before public submission can be enabled, implement and independently qualify
the distinct production authenticator under the signed policy. Expensive history
verification must precede final pending Safe/relayer nonce and Safe-state
admission, or cause an immediate repeat of that admission before sending. A
separate ordering correction is not included in this frozen source or receipt.

Retained canonical authorization contains one current-runtime profile. A later
upgrade before submission still needs a separately signed additive revision
with immutable predecessor linkage, all original receipts and previous profiles,
unchanged signed transaction and nonce claims, counted attempts and full
liabilities. Historical inclusion remains reconcilable. These concrete P0
requirements remain open in [PRELAUNCH-FIXES](../PRELAUNCH-FIXES.md), independently
of live genesis/runtime approval, selected Safe authority, signatures and funding.
