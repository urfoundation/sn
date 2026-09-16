# Runtime 460 affected-selector composition index

This index preserves the initial receipts. It does not authorize a rerun or a release build.

| scope | source | compile/census | body outcome | receipt |
|---|---|---|---|---|
| CRV4 original | 0127b7ee08d1cd77262a6dd28b606e2cfee33e6b | normal/race compiler failed on byte/int test typing; preserved | no body admitted | compile/crv4-{normal,race} |
| CRV4 type repair | 27eecfdf49cf90593de4a0e5ff905af8657c5d94 | normal/race compile and 13-root census passed | each failed only TestSourceCommitmentRuntime460MetadataChangesOnlySpecConstant | runtime460-crv4-type-repair-20260915-r1/terra |
| miner | 0127b7ee08d1cd77262a6dd28b606e2cfee33e6b | normal/race compile and 16-root census passed | each failed only TestFleetRuntimeArtifactMatchesReleaseManifests | normal/race/miner |
| validator | 0127b7ee08d1cd77262a6dd28b606e2cfee33e6b | normal/race compile and 25-root census passed | PASS normal and race; events and all fences true | normal/race/validator |
| simulator normal | 0127b7ee08d1cd77262a6dd28b606e2cfee33e6b | compile and 54-root census passed | failed TestProducerGatePinsSemanticIntegrityRegressions and TestRuntime458ArtifactCheckerKeepsHistoricalProvenanceOffFreshRoute | normal/sim-testnet |
| simulator race | 0127b7ee08d1cd77262a6dd28b606e2cfee33e6b | compile and 54-root census passed | failed the same two roots as normal: TestProducerGatePinsSemanticIntegrityRegressions and TestRuntime458ArtifactCheckerKeepsHistoricalProvenanceOffFreshRoute | race/sim-testnet |

Failed-root union admitted for source-patch retry, normal and race only:
1. TestSourceCommitmentRuntime460MetadataChangesOnlySpecConstant (CRV4 repair).
2. TestFleetRuntimeArtifactMatchesReleaseManifests (miner).
3. TestProducerGatePinsSemanticIntegrityRegressions (simulator).
4. TestRuntime458ArtifactCheckerKeepsHistoricalProvenanceOffFreshRoute (simulator).

The simulator failed-root set is identical in normal and race mode. No passed validator root and no unaffected selected root is in retry scope. Causal receipt ownership is separate.
