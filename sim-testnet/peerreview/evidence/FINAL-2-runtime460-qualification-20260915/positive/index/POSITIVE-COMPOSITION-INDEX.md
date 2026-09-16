# Runtime 460 positive qualification composition

Selection authority is the reviewed affected-test selection: 13 CRV4, 16 miner, 25 validator, and 54 simulator roots in each of normal and race mode. The accepted map has 108 roots per mode and 216 rows total.

| mode | roots | retained initial PASS events | replacement PASS events | membership check |
|---|---:|---:|---:|---|
| normal | 108 | 104 | 4 | true |
| race | 108 | 104 | 4 | true |

The full root-to-receipt map is ACCEPTED-ROOT-MAP.tsv. COMPOSITION-MEMBERSHIP.valid records independent pass-event confirmation for all 216 mapped roots; its error file is empty.

## Source lineage

| source | role |
|---|---|
| 0127b7ee08d1cd77262a6dd28b606e2cfee33e6b | formatted initial positive admission |
| 27eecfdf49cf90593de4a0e5ff905af8657c5d94 | isolated CRV4 integer-boundary test repair |
| 7989fa78faae81b82138c79e43dded16aad449dc | final clean combined test/fixture correction |

Formatter admission on the three Go files newly changed by the final correction had zero diff, so the final source remains 7989fa78. The 0127-to-final delta contains only crv4/runtime_identity_test.go, crv4/source_commitment_runtime460_test.go, miner/fleet_runtime_test.go, sim-testnet/release_runtime458_source_test.go, and sim-testnet/semantic-integrity-tests.txt. The final correction's four-file hash manifest and patch are retained in inputs/.

## Initial failures retained and replacement scope

- Original CRV4 compilers on 0127 failed from byte/int test typing. The 27e isolated repair compiled and ran all 13 roots; only TestSourceCommitmentRuntime460MetadataChangesOnlySpecConstant then failed.
- Miner compiled and censused 16 roots on 0127; only TestFleetRuntimeArtifactMatchesReleaseManifests failed.
- Validator completed all 25 roots in normal and race with event and fence checks true.
- Simulator compiled and censused 54 roots on 0127; only TestProducerGatePinsSemanticIntegrityRegressions and TestRuntime458ArtifactCheckerKeepsHistoricalProvenanceOffFreshRoute failed in both modes.

Only those four roots were replaced on 7989fa78. RETRY-OUTCOMES.tsv records six fresh package/mode compilers, their binary hashes, exact census counts, body exits, event membership, and fences. Every retry row is PASS.

All initial compiler/body failures, source fences, binaries, selectors, test2json streams, and launcher joins remain in their original capture roots. This composition does not rerun the accepted validator or unaffected roots.
