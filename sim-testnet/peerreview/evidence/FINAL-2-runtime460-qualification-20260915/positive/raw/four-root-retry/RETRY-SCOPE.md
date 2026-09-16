# Runtime 460 failed-root retry scope

This capture is preallocated only. It must not run until Astra hands over the frozen combined test-only source commit.

Fresh package binaries required after source admission:
- crv4, normal and race: TestSourceCommitmentRuntime460MetadataChangesOnlySpecConstant.
- miner, normal and race: TestFleetRuntimeArtifactMatchesReleaseManifests.
- sim-testnet, normal and race: TestProducerGatePinsSemanticIntegrityRegressions and TestRuntime458ArtifactCheckerKeepsHistoricalProvenanceOffFreshRoute.

Validator remains accepted from the initial receipt. No full selector replay is admitted.
