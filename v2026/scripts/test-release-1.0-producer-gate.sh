#!/usr/bin/env bash
set -euo pipefail

# Release qualification cannot hide an assertion or panic behind a retry.
export WARP_TEST_ENV_FAIL_FAST=1

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace="$(dirname "$sn_repo")"
release_repos=(sn server operator-proxy connect sdk glog goidenticons proxy userwireguard vault xops config)
source "$sn_repo/scripts/release-gate-jobs.sh"
release_gate_jobs_init

# The ordinary entry stays strict. Diagnostics run every original preflight
# under one joined owner while independent unchanged test phases can begin.
release_gate_source_integrity_preflight() {
  (
    cd "$sn_repo"
    go test ./sim-testnet/sourceguard -count=1
    go run ./sim-testnet/sourceguard ./sim-testnet
  )
}
release_gate_initial_preflights() {
  if [[ "$release_gate_diagnostic" == 1 ]]; then
    release_gate_record_preflight repository-inventory release_gate_diagnostic_inventory || release_gate_preflight_refusal "$?"
  fi
  echo "[release-1.0 producer] source-freeze preflight"
  release_source_snapshot="$(release_gate_record_preflight source-freeze "$sn_repo/scripts/check-release-source-freeze.sh" "$workspace")" || release_gate_preflight_refusal "$?"
  printf '%s\n' "$release_source_snapshot"

  echo "[release-1.0 producer] simulator source-integrity preflight"
  release_gate_record_preflight source-integrity bash -c 'set -euo pipefail; release_gate_source_integrity_preflight' || release_gate_preflight_refusal "$?"

  echo "[release-1.0 producer] generated binding toolchain preflight"
  release_gate_record_preflight binding-toolchain "$sn_repo/stabi/generate.sh" --preflight || release_gate_preflight_refusal "$?"

  echo "[release-1.0 producer] runtime 454 source attestation"
  release_gate_record_preflight runtime-source "$sn_repo/scripts/check-runtime-v454-source.sh" || release_gate_preflight_refusal "$?"

  echo "[release-1.0 producer] exact runtime metadata artifact attestation"
  release_gate_record_preflight runtime-metadata "$sn_repo/scripts/check-runtime-metadata-artifacts.sh" || release_gate_preflight_refusal "$?"
  release_gate_preflight_status
}
export -f release_gate_source_integrity_preflight release_gate_initial_preflights
release_source_snapshot=''
if [[ "$release_gate_diagnostic" == 1 ]]; then
  release_gate_start diagnostic-preflights release_gate_initial_preflights
else
  release_gate_initial_preflights
fi

if ! command -v forge >/dev/null 2>&1 && [[ -x "$HOME/.foundry/bin/forge" ]]; then
  export PATH="$HOME/.foundry/bin:$PATH"
fi

release_phase_isolation_regressions() {
  cd "$sn_repo"
  go test ./scripts/server-fixture -count=1
  go test -race ./scripts/server-fixture -count=1
  go test ./sim-testnet -run '^TestReleaseGate(Child|Jobs|Isolation)' -count=1 -parallel=4 -timeout 3m
  go test -race ./sim-testnet -run '^TestReleaseGate(Child|Jobs|Isolation)' -count=1 -parallel=4 -timeout 3m
  cd "$workspace/server"
  go test . -run '^Test(ReleaseGateServices|TestEnvironmentPrivatePortable|TestEnvironmentDoesNotStartSharedHTTPProfiler)' -count=1
  go test -race . -run '^Test(ReleaseGateServices|TestEnvironmentPrivatePortable|TestEnvironmentDoesNotStartSharedHTTPProfiler)' -count=1
}
release_gate_start isolation-regressions release_phase_isolation_regressions

# This is the bounded launch-critical gate. The complete aggregate gate remains
# mandatory for final acceptance, but it is deliberately not repeated on the
# critical path after the release-locked source has passed it. Semantic
# reconstruction, public archive replay, report rendering and broad suites run
# concurrently with the live acceptance windows.
echo "[release-1.0 producer] compile complete simulator and validator graph"
release_phase_compile() {
  cd "$sn_repo"
  go test ./validator ./sim-testnet -run '^$' -count=1
}
release_gate_start compile release_phase_compile

echo "[release-1.0 producer] exact runtime client and cancellation boundaries"
release_phase_runtime() {
  cd "$sn_repo"
  # The simulator launches the real miner and validator modules. Certify the
  # exact runtime-455 identity, historical metadata cache, per-endpoint
  # failover budget, and caller cancellation paths before either module can
  # make a testnet write. Prefix selection deliberately admits adjacent
  # regressions added for the same boundary.
  runtime_client_tests='^Test(DialChainContext|FinalizedHeadContext|FinalizedBlock|BlockHashContext|BlockIdentityCache|ExactBlockIdentity|AccountNonceContext|ReleaseStateReaders|ReleaseExactBlock|ReleaseSnapshot|ReleaseSteeringSource|VerifyFinalizedExtrinsicContext|LocateFinalizedExtrinsic|FleetCommitmentAtContext|FleetCommitmentInfoRuntime|RuntimeArtifactMetadata|RuntimeMetadataAtContext|FleetRuntime|FleetFinalizedRuntime|BindFleetRuntime|DialFleetNativeContext|ReleaseEpochStartBlockAtContext|ReleaseConfigRequiresExactNativeRuntimeIdentity|InitialReleaseSnapshot|AuthenticatePinnedNativeRuntime|ReleaseNativeEndpointTimeout|ReleaseRuntime455)'
  go test ./crv4 ./miner ./validator -run "$runtime_client_tests" -count=1
  go test -race ./crv4 ./miner ./validator -run "$runtime_client_tests" -count=1
  runtime455_tests='^Test(Runtime455|ReleaseGatesAttest.*Runtime|ProducerGatePinsRuntime455|ReviewedRuntimeIdentity|ReviewedHistoricalRuntimeArtifacts|RuntimeVersionIdentity|RuntimeCodeHashValidation|RuntimeMetadataHashValidation|ReleaseLockDecodesCanonicalRuntimeFields|ReleaseLockRejectsGeneratedRuntimeDrift|ResolvedConfigPinsReviewedRuntimeArtifactIdentity|PublishedManifestBindsCompleteRuntimeIdentity|ReleaseRuntimeRPCRetry|CurrentRuntimeAuthenticationCannotUseHistoricalCache)'
  go test ./sim-testnet -run "$runtime455_tests" -count=1
  go test -race ./sim-testnet -run "$runtime455_tests" -count=1
}
release_gate_start runtime release_phase_runtime

# Atomic native source custody is independent of the slower signed settlement
# package; its complete source family runs in its own joined gate job.
release_phase_evidence_native() {
  cd "$sn_repo"
  native_evidence_tests='^TestSourceCommitment'
  go test ./crv4 -run "$native_evidence_tests" -count=1
  go test -race ./crv4 -run "$native_evidence_tests" -count=1
}
release_gate_start evidence-native release_phase_evidence_native

echo "[release-1.0 producer] non-replacing validator and hotkey seed custody"
release_phase_seed() {
  cd "$sn_repo"
  seed_custody_tests='^Test(Keypair|LoadOrCreateSeedFile|SeedFileFormats|SeedCustody|VpkSeed|EvmKeyLoadingAndMirror|HotkeyLoadOrCreate|IdentityCustody|ReleaseClientSeed)'
  go test ./crv4 ./validator -run "$seed_custody_tests" -count=1
  go test -race ./crv4 ./validator -run "$seed_custody_tests" -count=1
}
release_gate_start seed release_phase_seed

echo "[release-1.0 producer] raw simulator client-key custody"
release_phase_sim_seed() {
  cd "$sn_repo"
  simulator_seed_custody_tests='^Test(SimulatorClientSeedCustody|SimulatorOperatorPath|InspectValidatorPathProofsRequiresEveryOperatorDomain$|FinalSettlementClosureWaitHonorsPublicationAndCancellation$|FinalLifecycleIntentRequirementsKeepSettlementAndNativeClocksDistinct$)'
  go test ./sim-testnet -run "$simulator_seed_custody_tests" -count=1 -parallel=4 -timeout 3m
  go test -race ./sim-testnet -run "$simulator_seed_custody_tests" -count=1 -parallel=4 -timeout 3m
}
release_gate_start sim-seed release_phase_sim_seed

echo "[release-1.0 producer] synthetic EVM block identity and canonical recovery"
release_phase_evm_identity() {
  cd "$sn_repo"
  # Public Subtensor EVM hashes are supplied by RPC, not reconstructed from
  # Ethereum header fields. Exercise each receipt, capture, and cleanup
  # consumer before a first transaction can strand its durable intent.
  synthetic_evm_identity_tests='^Test(WaitFinalized|EVMBlockIdentity|ClaimReceiptIdentity|FinalizedClaimReceipt|UncertainClaimRetryable|SyntheticEVM|EthEVMBlockReader|EVMFinality|FinalizedEVMHead|BoundFinalizedEVMHead|ReceiptRequiresCanonicalHashAndFinalizedHeight|ProducerGatePinsSyntheticEVMIdentityRegressions)'
  go test ./miner/onchain ./miner ./sim-testnet -run "$synthetic_evm_identity_tests" -count=1 -timeout 5m
  go test -race ./miner/onchain ./miner ./sim-testnet -run "$synthetic_evm_identity_tests" -count=1 -timeout 10m
}
release_gate_start evm-identity release_phase_evm_identity

echo "[release-1.0 producer] authenticated release driver"
release_phase_driver() {
  cd "$sn_repo"
  executable_attestation_tests='^Test(ExecutableAttestation|StopExecutableAttestation|ReleaseExecutable|AuthenticateCommandExecutable|RunMain|ParseReleaseExecutableBuildInfo|PushedSNRevision|ParseCurrentSNRevision|ReleaseGitNetworkEnvironment)'
  go test ./sim-testnet -run "$executable_attestation_tests" -count=1
  go test -race ./sim-testnet -run "$executable_attestation_tests" -count=1
}
release_gate_start driver release_phase_driver

echo "[release-1.0 producer] strict proof and configuration framing"
release_phase_framing() {
  cd "$sn_repo"
  parser_framing_tests='^Test((Policy|ReleaseConfig|ClaimDaemonConfig|StrictYAML|RenderedValidatorPolicy)RejectsMalformedTrailingYAML|FinalSemanticPathProofArtifact(Count|RejectsMalformedTrailingJSON)|ParseFleetManifestStrictCanonicalRoundTrip|PolicyStrictAndFailClosed|LoadReleaseConfigStrictAndNormalizesOperatorSecrets|ClaimDaemonConfigStrictAndPortable|StrictYAMLRejectsUnknownAndMultipleDocuments|ReleaseGatesPinProviderAndTransportRegressions)$'
  go test ./protocol ./miner ./validator ./sim-testnet -run "$parser_framing_tests" -count=1 -parallel=4 -timeout 2m
  go test -race ./protocol ./miner ./validator ./sim-testnet -run "$parser_framing_tests" -count=1 -parallel=4 -timeout 2m
}
release_gate_start framing release_phase_framing

echo "[release-1.0 producer] signed validator evidence and settlement"
release_phase_settlement() {
  cd "$sn_repo"
  # This near-complete validator census exceeds the implicit ten-minute budget.
  # Scope its aggregate allowance here; individual stress deadlines stay fixed.
  producer_tests='^Test(Attempt|DiskAttempt|HTTPAttemptStreamV2|SealAttemptCutV2|TrailPolicyDepth|StatsWrite|StatsSnapshotWrite|StatsMultiBatch|StatsSettlement|Deposited|ReleaseMeasurement|ReleaseStatsV2Runtime|ReleaseHeadV2|IntentStore|SteeringIntent|MeasurementStats|ExactPoolQuality|HeadEMA|ReleaseSteeringLoop|ReleaseSettlementRefresh|ReleaseEvidenceV2|ReleaseActivationV2|ReleaseBootstrapV2|ReleaseActivationHistoryV2|ReleaseInitialBoundaryV2|ReleaseStartupV2|ReleaseStartupOwnerV2|ValidatorEvidenceCensusV2|ChainEvidence|RunRelease|ValidatorEvidence|ValidatorUpload|ReleaseClientKeyHistory|ReleaseClientKeyAuthority|ReleaseRuntimeV2|ArtifactHttpObservation|NativeSourceHash|IntentV2|ReleaseNativeCaptureV2|ReleaseCaptureV2)'
  go test ./validator -run "$producer_tests" -count=1 -parallel=4 -timeout 90m
  go test -race ./validator -run "$producer_tests" -count=1 -parallel=4 -timeout 90m
}
release_gate_start settlement release_phase_settlement

echo "[release-1.0 producer] concurrent adversarial actor and metric closure"
release_phase_adversarial() {
  cd "$sn_repo"
  adversarial_tests='^Test(Adversarial|Adversary|VerifyAdversary|RPCAdversary|ConsensusWeightComparison|Runtime454)'
  go test ./sim-testnet -run "$adversarial_tests" -count=1 -timeout 5m
  go test -race ./sim-testnet -run "$adversarial_tests" -count=1 -timeout 10m
}
release_gate_start adversarial release_phase_adversarial

echo "[release-1.0 producer] exact native, EVM, and ordinary-fleet semantic replay"
release_phase_semantic() {
  cd "$sn_repo"
  semantic_integrity_tests='^Test(FinalNative|FinalPublicNative|FinalSemanticFleetAudit|FinalPublicFleetAudit|FinalSemanticVault|FinalSemanticCycleConviction|FinalSemanticCoordinatorRuntime|FinalSemanticCoordinatorUpgrade|FinalClaimPaymentLedger|FinalSemanticReceiptPayload|PublicFinalSemantic|FinalSemanticPoolOperatorVersion|FinalSemanticEpochDeposit|FinalPublicChainVerificationRejectsV2ReceiptOnlyTranscript|FinalSemanticDishonestDepositReceiptPayload|FinalSemanticEvidenceBuildRenderAndArtifacts|FinalSemanticArtifactVerificationCache|FinalSemantic(CampaignArtifactReferences|ReplicatedEnvelope)|FinalSemanticFixture|FinalSemanticOriginalClosure|FinalSemanticArtifactDeploymentAdmission|FinalAttemptFixtureLedgerMatchesDurableProductionWire|FinalFleetLifecycle|FinalSemanticFleetByUIDAt|FinalPayoutAssignmentsAt|FinalPayoutArtifact|FinalSemanticDeployment|FinalSemanticBuilder|FinalSemanticPoolRegistration|FinalSemantic(Pool|Head|Validator)UIDZero|FinalFleetGeneration|FinalSemanticHistorical|FinalSemanticEvidenceFailsClosed|FinalSemanticPathProofArtifact|FinalSemanticPoolAuditDistinguishesUnderpaymentFromRecovery|FinalSemanticDishonestDepositDecisionsAndPublicReplay|FinalSemanticSettlementAccountingBindsBothHeadsAndEventDeltas|FinalSemanticCarryModelFailsClosedOnAdjacentAccountingErrors|FinalPublicChainVerificationRequiresTwoCanonicalOperatorOrigins|PublicScenarioBundle|SemanticMismatchBranches|StateMismatchError|FinalEVMLogQueryRanges|FinalCollectedCoordinatorBaselines|FinalCollectorIncludesCompletedSettlementTail|FinalSettlementClosure|ReleaseHistoryRuntimeArtifacts|ProducerGatePinsCompleteAdversarialRegressions|ProducerGatePinsSyntheticEVMIdentityRegressions|ProducerGatePinsSemanticIntegrityRegressions|ProducerGatePinsExactBlockRuntimeClientRegressions|ReleaseSemanticCensus)'
  semantic_integrity_census="$sn_repo/sim-testnet/semantic-integrity-tests.txt"
  semantic_integrity_actual="$(go test ./sim-testnet -list "$semantic_integrity_tests" | sed -n '/^Test/p' | LC_ALL=C sort)"
  if [[ -z "$semantic_integrity_actual" ]]; then
    echo "semantic-integrity selector matched no tests" >&2
    exit 1
  fi
  diff -u "$semantic_integrity_census" <(printf '%s\n' "$semantic_integrity_actual")
  go test ./sim-testnet -run "$semantic_integrity_tests" -count=1 -skip '^(TestPublicScenarioBundleRequiresReplicatedOwnerCompletionCommit|TestFinalSemanticFleetAuditProjectionBindsTheExistingArtifact)$' -parallel=4 -timeout 15m
  go test -race ./sim-testnet -run "$semantic_integrity_tests" -count=1 -skip '^(TestPublicScenarioBundleRequiresReplicatedOwnerCompletionCommit|TestFinalSemanticFleetAuditProjectionBindsTheExistingArtifact)$' -parallel=4 -timeout 25m
}
release_gate_start semantic release_phase_semantic

# These measured full replays each retain both original mode budgets. Separate
# processes keep their work from consuming the remaining semantic roots' clock.
release_phase_semantic_public_scenario() {
  cd "$sn_repo"
  semantic_public_scenario_tests='^TestPublicScenarioBundleRequiresReplicatedOwnerCompletionCommit$'
  go test ./sim-testnet -run "$semantic_public_scenario_tests" -count=1 -parallel=4 -timeout 15m
  go test -race ./sim-testnet -run "$semantic_public_scenario_tests" -count=1 -parallel=4 -timeout 25m
}
release_gate_start semantic-public-scenario release_phase_semantic_public_scenario

release_phase_semantic_fleet_projection() {
  cd "$sn_repo"
  semantic_fleet_projection_tests='^TestFinalSemanticFleetAuditProjectionBindsTheExistingArtifact$'
  go test ./sim-testnet -run "$semantic_fleet_projection_tests" -count=1 -parallel=4 -timeout 15m
  go test -race ./sim-testnet -run "$semantic_fleet_projection_tests" -count=1 -parallel=4 -timeout 25m
}
release_gate_start semantic-fleet-projection release_phase_semantic_fleet_projection

echo "[release-1.0 producer] lossless capture, completion, and publication"
release_phase_capture() {
  cd "$sn_repo"
  capture_tests='^Test(FinalArchive|FinalCompositeArchive|ArchivePreflight|FinalClaimQueueCapture|FinalCollected(Bundle|File|Chain)|FinalSemantic(PublicCapture|LaunchFoundation)|FinalContractCleanupCapture|VerifyFinalCollected|FleetLifecycle|CanonicalRPCReceiptLogs|ScenarioProcessLogGate|ReleaseAndProductionScenariosRequireProcessLogGate|ScenarioCompletion|ScenarioRunner(WritesCompleteEvidenceOnlyOnPass|FailureHasNoCompleteMarker)|PublishedScenarioCandidateKeepsFrozenHashWhenClockAdvances|PublishedCompletionCommits|CampaignEvidence|DirectScenarioCompletion|EvidenceFileHashes|ArchiveCurrentDeploymentPublication|VerifyPublishedEvidenceOrigin|ReleaseCandidateCampaign|ProductionCampaignCompletion|ReleaseCampaignGate|ExactReleaseCampaignGate|ScenarioCampaignAttempt|ProductionHandoff|InitialScenarioFailure|ProductionPolicyEvidence|PrepareSignedAttemptStateNamespace|ClassifyValidatorAttemptState|ValidatorStateNamespace|QualificationLauncher|SimulatorAttemptCutV2|ProducerGateStateSelection|ProducerGateCustodySelection|ProducerGateCaptureSelection|FinalCaptureV2|FinalCaptureCapacity)'
  # This is deliberately capture-only: typed semantic reconstruction, public
  # replay, supplement publication and FINAL.md rendering run post-capture.
  go test ./sim-testnet -run "$capture_tests" -count=1 -skip '^(TestCampaignEvidence(CapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier|PopulationV2StreamsPhaseCensusWithBoundedOwners)|TestFinalCaptureV2(ReadsActualRenderedSetupAndRejectsChangedSource|PendingPriorClosesOriginalAuthority|PendingPriorRejectsRehashedSourceAndMissingCensus|PendingPriorRejectsWrongHandoffAndSemanticRelabel|PendingJobIsImmutableAndNeverAccepted|PendingPriorRejectsWrongGateBeforeWrites|PendingPriorArtifactCensusHasNoSemanticOutputs)|TestVerifyFinalCollectedPriorPhaseBytesRejectsReopenedHandoffSubstitution)$' -timeout 5m
  go test -race ./sim-testnet -run "$capture_tests" -count=1 -skip '^(TestCampaignEvidence(CapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier|PopulationV2StreamsPhaseCensusWithBoundedOwners)|TestFinalCaptureV2(ReadsActualRenderedSetupAndRejectsChangedSource|PendingPriorClosesOriginalAuthority|PendingPriorRejectsRehashedSourceAndMissingCensus|PendingPriorRejectsWrongHandoffAndSemanticRelabel|PendingJobIsImmutableAndNeverAccepted|PendingPriorRejectsWrongGateBeforeWrites|PendingPriorArtifactCensusHasNoSemanticOutputs)|TestVerifyFinalCollectedPriorPhaseBytesRejectsReopenedHandoffSubstitution)$' -timeout 10m
}
release_gate_start capture release_phase_capture

# Go releases parallel roots only after the package's serial tests finish.
# Admit all seven private durable fixtures together before that shared delay.
release_phase_capture_private() {
  cd "$sn_repo"
  capture_private_tests='^TestFinalCaptureV2(ReadsActualRenderedSetupAndRejectsChangedSource|PendingPriorClosesOriginalAuthority|PendingPriorRejectsRehashedSourceAndMissingCensus|PendingPriorRejectsWrongHandoffAndSemanticRelabel|PendingJobIsImmutableAndNeverAccepted|PendingPriorRejectsWrongGateBeforeWrites|PendingPriorArtifactCensusHasNoSemanticOutputs)$'
  go test ./sim-testnet -run "$capture_private_tests" -count=1 -timeout 5m
  go test -race ./sim-testnet -run "$capture_private_tests" -count=1 -timeout 10m
}
release_gate_start capture-private release_phase_capture_private

# Reopened prior verification constructs and seals the complete semantic graph.
# Keep its original fixture and rejection checks in one independently timed job.
release_phase_capture_prior() {
  cd "$sn_repo"
  capture_prior_tests='^TestVerifyFinalCollectedPriorPhaseBytesRejectsReopenedHandoffSubstitution$'
  go test ./sim-testnet -run "$capture_prior_tests" -count=1 -timeout 5m
  go test -race ./sim-testnet -run "$capture_prior_tests" -count=1 -timeout 10m
}
release_gate_start capture-prior release_phase_capture_prior

# Keep the complete 900-object, 296 MiB publisher/readback in its own process.
# It must not consume the ordinary capture roots' package-wide timeout or run
# alongside their process-global allocation measurements.
release_phase_capture_population() {
  cd "$sn_repo"
  capture_population_tests='^TestCampaignEvidencePopulationV2StreamsPhaseCensusWithBoundedOwners$'
  go test ./sim-testnet -run "$capture_population_tests" -count=1 -timeout 5m
  go test -race ./sim-testnet -run "$capture_population_tests" -count=1 -timeout 10m
}
release_gate_start capture-population release_phase_capture_population

# The unchanged full metadata race census measured 1,839.622 seconds. Give only
# that exact root 45 minutes; normal metadata and ordinary capture keep their
# existing deadlines, population, resource admission and joined cleanup.
release_phase_capture_metadata() {
  cd "$sn_repo"
  capture_metadata_tests='^TestCampaignEvidenceCapacityV2MetadataFullCensusMaterializesFlatWireAndCarrier$'
  go test ./sim-testnet -run "$capture_metadata_tests" -count=1 -timeout 5m
  go test -race ./sim-testnet -run "$capture_metadata_tests" -count=1 -timeout 45m
}
release_gate_start capture-metadata release_phase_capture_metadata

echo "[release-1.0 producer] exact deployable contract behavior"
release_phase_solidity() {
  cd "$sn_repo/evm"
  forge fmt --check
  forge build --deny warnings --sizes
  forge test --summary

  cd "$sn_repo"
  go run ./sim-testnet/gencontracts --check "$FOUNDRY_OUT" sim-testnet/contracts_gen.go
  ./stabi/generate.sh --check --artifacts "$FOUNDRY_OUT"

  # The companion's Go wire/signature, generated ABI, installation/carry and
  # rendered-runtime controls are distinct from the real Solidity behavior.
  validator_evidence_tests='^Test(ValidatorEvidence|RuntimeEvidenceV2|RuntimeEvidence|EvidenceRelay|EvmTxManager|ClientKeyHistory)'
  go test ./protocol ./stabi ./sim-testnet/gencontracts ./sim-testnet -run "$validator_evidence_tests" -count=1
  go test -race ./protocol ./stabi ./sim-testnet/gencontracts ./sim-testnet -run "$validator_evidence_tests" -count=1
}
release_gate_start solidity release_phase_solidity

echo "[release-1.0 producer] operator proof and artifact APIs"
release_phase_server_unit() {
  cd "$workspace/server"
  test_env_fail_fast_tests='^Test(DefaultTestEnvReleaseFailFast|RunRetriesUntilPass|RunFailsAfterExhaustion|RunReportsPanicOriginAfterExhaustion)'
  go test . -run "$test_env_fail_fast_tests" -count=1
  go test -race . -run "$test_env_fail_fast_tests" -count=1
  go test ./st ./startifact -count=1
  go test ./controller -run '^Test(CoreStClient|StConfig|StCompute|StBuild|StDeposit|StEstimate|StReplacement|StDecode|StEvent|StBroadcast|StClientStub|StTransactionCancellation|VerifyEvidenceRange|VerifyKeyRotation|VerifySyntheticSeedId|VerifyUsesUrForwardedAddress|VerifyIgnoresLegacyForwardedAddress|VerifyClampM|VerifyCachedResponseRoundTrip|VerifySeedRejectsMissingSignature)' -count=1
  provider_input_tests='^Test(StCanonicalProviderUsages|StBuildReleaseProviderInputs)'
  go test ./controller -run "$provider_input_tests" -count=1
  go test -race ./controller -run "$provider_input_tests" -count=1
  # Requested depth must fit its signed byte before settings or state admission.
  seed_admission_tests='^Test(VerifySeedAdmission|VerifySeedRejectsMissingSignature|VerifyClampM)'
  go test ./controller -run "$seed_admission_tests" -count=1
  go test -race ./controller -run "$seed_admission_tests" -count=1
  payout_allocation_tests='^Test(EvenContractPayoutShare|AllocateContractParticipantPayouts|AllocateContractParticipantPayoutEligibilityMatrix)$'
  go test ./model -run "$payout_allocation_tests" -count=1
  go test -race ./model -run "$payout_allocation_tests" -count=1
  # The seed/future-assignment cases below are database-backed and run only in
  # the isolated profile. Keep this selector explicit so a broad prefix cannot
  # silently admit a new *_db_test.go case before WARP_ENV is configured.
  filter_pure_tests='^TestVerifySimulationAssignmentFilter(IsValidatorLocalAndFailClosed|RejectsAmbiguousFiles|V[12].*|Rejects(Leaf|Parent)Symlink|AtomicReplacementPinsOpenedDescriptor)$'
  go test ./controller -run "$filter_pure_tests" -count=1
  go test -race ./controller -run "$filter_pure_tests" -count=1
}
release_gate_start server-unit release_phase_server_unit
release_phase_connect() {
  cd "$workspace/connect"
  client_key_registration_tests='^Test(ClientKeyRegistration|ApiOutOfBandControl|ControlSyncOob|ClientKeyManager|VerifyClientKeySignature|CanonicalCertChainBytes|ClientCloseAndWaitJoinsClientKeyPublisher)'
  go test . -run "$client_key_registration_tests" -count=1
  go test -race . -run "$client_key_registration_tests" -count=1
  go test . -run '^Test(Verify|Sn|PlatformPacketConnClampsQuic)' -count=1
  go test . -run '^TestCreateContractReportsSenderSequenceRole$' -count=1

  transport_identity_tests='^Test(PlatformTransportAuthSnapshotsAreAtomicAndOwned|PlatformTransportH[13]ReconnectUsesUpdatedAuthSnapshot|TunTcpInboundFlowUsesStableBoundedShards|TunTcpInboundShardHandoffCadenceIsBounded|TunWriteCompletesFiniteTcpInboundHandoffBeforeReturn|TunWriteRetainsTcpInboundYieldCadence|TunWriteBatchFinishesEveryTcpInboundHandoff)$'
  go test . -run "$transport_identity_tests" -count=1
  go test -race . -run "$transport_identity_tests" -count=1

  # Generated policy data is executable release input. Exercise both its
  # structural/runtime invariants and the generator contract before a source
  # refresh can reach the live launch path.
  policy_tests='^Test(BlockerGeneratedTables|BlockerDefaultDataSmoke|BlockerDataGuards|BlockerHashVectors|BlockerZeroAlloc|BlockerFalsePositiveProbe|BlockerIp4|BlockerIp6|BlockerToggleRace|CfaaPortClassification|CfaaBlockedIps|CfaaDisabled|CfaaIngressMirrorsSourceDrops|CfaaBlockedPrefixInvariant|CfaaBlockedIp4BruteForce|CfaaBlockedIp4ZeroAlloc|CfaaSearch6|CfaaInspectV6|CfaaBlockedPrefix6Invariant|CfaaInspectIcmp|TelegramCallReflectorRanges|TelegramCallV12TcpFallback|CfaaTelegramCallException|SecurityPolicyAllowsTelegramCallReflectors)$'
  go test . -run "$policy_tests" -count=1
  go test -race . -run "$policy_tests" -count=1
  go test ./blocker ./security -count=1
  go test -race ./blocker ./security -count=1

  # Freeze the real P2P fast path and the test carrier semantics used to prove
  # it: reliable-unordered delivery, dispatch-time registration, hard bounded
  # admission, cancellation joins, and exact pooled-buffer ownership.
  p2p_signal_tests='^Test(WebRtc|WebRtcMessageRoundTrip|P2pTransportAutoSelectsFastPathForCapablePeer|SignalPipeDropsBeforeDestinationRegistration|DelayedSignalPipe(ResolvesDestinationAtDispatch|DropsMissingDestinationAtDispatch|CancellationReturnsOwnedFrames|FullQueueDoesNotBlockDispatch|CancellationUnblocksCapacitySender)|MatchExpectedUnorderedP2pMessages(AcceptsPermutation|RejectsInvalidContent))$'
  go test . -run "$p2p_signal_tests" -count=1
  go test -race . -run "$p2p_signal_tests" -count=1
}
release_gate_start connect release_phase_connect
release_phase_sdk() {
  cd "$workspace/sdk"
  provider_registration_tests='^Test(DeviceLocalProviderRegistration|DeviceLocalProviderConnected)'
  go test . -run "$provider_registration_tests" -count=1
  go test -race . -run "$provider_registration_tests" -count=1
  token_transport_tests='^Test(ApiTokenManager|DeviceRemoteRpcPublicationWakesOnlyOutstandingRefresh|ApiCloseAndWaitJoinsRefreshWorker|DeviceLocalAppliesApiRefreshAndLogout|DeviceRemoteAppliesStandaloneApiRefreshAndLogout)'
  go test . -run "$token_transport_tests" -count=1
  go test -race . -run "$token_transport_tests" -count=1
}
release_gate_start sdk release_phase_sdk

echo "[release-1.0 producer] operator-proxy source and behavior"
release_phase_operator_proxy() {
  cd "$workspace/operator-proxy"
  go mod tidy -diff
  go build ./...
  go vet ./...
  unformatted="$(gofmt -l .)"
  if [[ -n "$unformatted" ]]; then
    echo "gofmt needed for:"
    echo "$unformatted"
    gofmt -d .
    exit 1
  fi
  go test -count=1 -timeout 20m ./...
  go test -race -count=1 -timeout 20m ./...
}
release_gate_start operator-proxy release_phase_operator_proxy

echo "[release-1.0 producer] isolated PostgreSQL/Redis evidence path"
release_gate_open_services() {
  local status=0
  {
    release_gate_services_start "$release_gate_root" "$workspace" "$sn_repo/deploy/testnet/release.lock.yml"
  } || status=$?
  (( status == 0 )) || return "$status"
  export RELEASE_GATE_SERVICE_ENV="$release_gate_service_root/environment.sh"
}
release_gate_services_ready=0
if release_gate_record_preflight private-services release_gate_open_services; then
  release_gate_services_ready=1
else
  release_gate_services_error=$?
  export release_gate_services_error
  release_gate_preflight_refusal "$release_gate_services_error"
fi
release_phase_server_db() {
  cd "$workspace/server"
  export WARP_ENV=local
  export WARP_SERVICE=test
  export WARP_DOMAIN=bringyour.com
  export WARP_BLOCK=test
  export WARP_VERSION=0.0.0
  source "$RELEASE_GATE_SERVICE_ENV"
  source "$workspace/server/test-env.sh"
  test_env_validate_suite_resource_manifest "$TEST_ENV_SUITE_RESOURCE_MANIFEST" "$WARP_VAULT_HOME" "$WARP_CONFIG_HOME"
  # Real JWT/body ownership, signed key history and reserved Redis quotas use
  # this gate's private services; neither a compile nor Forge exercises them.
  evidence_source_tests='^Test(StAttempt|StReserved|StClientKeyHistory|StClientKeyPublication|StClientKeyRegistrationCohort|StClientKeyRegistrationReadiness|SnAttempt|SnReserved|SnClientKey|MigrationCatalog|PublishedMigration|TransferContractOpenPlanRepairMigrationOrder|ApplyDbMigrations|ContractResultErrorSeparatesReliabilityFromAccountFailures)'
  go test ./api -run "$evidence_source_tests" -count=1
  go test -race ./api -run "$evidence_source_tests" -count=1
  go test ./api/handlers -run "$evidence_source_tests" -count=1
  go test -race ./api/handlers -run "$evidence_source_tests" -count=1
  go test ./controller -run "$evidence_source_tests" -count=1 -skip '^TestStClientKeyHistoryBatchActualFullPopulation(SharedBoundary|DistinctBoundaries)$' -timeout 10m
  go test -race ./controller -run "$evidence_source_tests" -count=1 -skip '^TestStClientKeyHistoryBatchActualFullPopulation(SharedBoundary|DistinctBoundaries)$' -timeout 10m
  go test ./model -run "$evidence_source_tests" -count=1
  go test -race ./model -run "$evidence_source_tests" -count=1
  go test . -run "$evidence_source_tests" -count=1
  go test -race . -run "$evidence_source_tests" -count=1
  controller_tests='^Test(VerifyController(FullTrailFlow|PoisonAndFailurePaths|ConcurrentExtendReloadsAfterLock|ReplayCannotReadANewerCachedResponse)|VerifySimulationAssignmentFilter(BlocksSeedPendingAndFutureAssignments|DoesNotAffectAnotherValidator)|StAccountReconcile|StSyncChainEventsBatchesCanonicalEventBlocks|StSyncChainEventsRejectsIncompleteCanonicalBatchBeforeMutation)'
  model_tests='^Test(VerifyEgressExactIndexAndPrefixScoreAreIndependent|VerifyTrailLockMutualExclusion|VerifyTrailLockStaleReleasePreservesSuccessor|StDeploymentStateIsIsolatedAcrossCoordinatorReplacements|StTransactionIntentReservationUsesChainAccountNonceScope|StTransactionAttemptCandidatesConvergeOnOneWinner|StTransactionFinalizedAttemptCannotRegress)'
  go test ./controller -run "$controller_tests" -count=1
  go test ./model -run "$model_tests" -count=1
  provider_attribution_tests='^Test(ContractPayout|CompanionContractPayout|ContractParticipant|StEpochProviderUsage|StatsProviderPayouts|StatsProviders|StatsQueryPlans|SearchProviderStatsRollup|RemoveOldSearchProviderStats|RemoveOldVerifyProviderStats)'
  go test ./model -run "$provider_attribution_tests" -count=1
  go test -race ./model -run "$provider_attribution_tests" -count=1
  go test ./taskworker -count=1
}
# Each full history population has its own process and ten-minute modes.
# TestEnv owns a unique Pg database and an exclusive renewable Redis lease;
# the gate retains the private containers until every phase has joined.
release_phase_server_history_shared() {
  cd "$workspace/server"
  export WARP_ENV=local
  export WARP_SERVICE=test
  export WARP_DOMAIN=bringyour.com
  export WARP_BLOCK=test
  export WARP_VERSION=0.0.0
  source "$RELEASE_GATE_SERVICE_ENV"
  source "$workspace/server/test-env.sh"
  test_env_validate_suite_resource_manifest "$TEST_ENV_SUITE_RESOURCE_MANIFEST" "$WARP_VAULT_HOME" "$WARP_CONFIG_HOME"
  server_history_shared_tests='^TestStClientKeyHistoryBatchActualFullPopulationSharedBoundary$'
  go test ./controller -run "$server_history_shared_tests" -count=1 -timeout 10m
  go test -race ./controller -run "$server_history_shared_tests" -count=1 -timeout 10m
}
release_phase_server_history_distinct() {
  cd "$workspace/server"
  export WARP_ENV=local
  export WARP_SERVICE=test
  export WARP_DOMAIN=bringyour.com
  export WARP_BLOCK=test
  export WARP_VERSION=0.0.0
  source "$RELEASE_GATE_SERVICE_ENV"
  source "$workspace/server/test-env.sh"
  test_env_validate_suite_resource_manifest "$TEST_ENV_SUITE_RESOURCE_MANIFEST" "$WARP_VAULT_HOME" "$WARP_CONFIG_HOME"
  server_history_distinct_tests='^TestStClientKeyHistoryBatchActualFullPopulationDistinctBoundaries$'
  go test ./controller -run "$server_history_distinct_tests" -count=1 -timeout 10m
  go test -race ./controller -run "$server_history_distinct_tests" -count=1 -timeout 10m
}
if [[ "$release_gate_services_ready" == 1 ]]; then
  release_gate_start server-db release_phase_server_db
  release_gate_start server-history-shared release_phase_server_history_shared
  release_gate_start server-history-distinct release_phase_server_history_distinct
else
  release_gate_start server-db release_gate_unavailable_services
  release_gate_start server-history-shared release_gate_unavailable_services
  release_gate_start server-history-distinct release_gate_unavailable_services
fi

# Join every admitted phase before inspecting the source again, including a
# failed phase. Retain its failure even when all final fences pass.
release_gate_status=0
release_gate_complete || release_gate_status=$?
if (( release_gate_active != 0 )); then
  echo 'release gate: source fences cannot precede an unproven child join' >&2
  exit 1
fi
release_gate_fence_status=0
if [[ "$release_gate_diagnostic" == 1 ]]; then
  if [[ -f "$release_gate_root/preflights/source-freeze/stdout" ]]; then
    release_source_snapshot="$(< "$release_gate_root/preflights/source-freeze/stdout")"
  else
    echo 'diagnostic source preflight did not complete; original snapshot is unavailable' >&2
    release_gate_fence_status=1
  fi
fi

echo "[release-1.0 producer] source and release-lock fence"
for repo in "${release_repos[@]}"; do
  git -C "$workspace/$repo" diff --check || release_gate_fence_status=1
  git -C "$workspace/$repo" diff --cached --check || release_gate_fence_status=1
done
(
  cd "$sn_repo"
  fence_status=0
  go test ./sim-testnet -run '^TestReleaseGatesPinProviderAndTransportRegressions$' -count=1 || fence_status=1
  go test ./sim-testnet -run '^TestReleaseLockMatchesCheckout$' -count=1 || fence_status=1
  exit "$fence_status"
) || release_gate_fence_status=1

echo "[release-1.0 producer] final source-freeze checkout"
if ! final_release_source_snapshot="$(release_gate_record_preflight final-source-freeze "$sn_repo/scripts/check-release-source-freeze.sh" "$workspace")"; then
  release_gate_fence_status=1
fi
if [[ "$final_release_source_snapshot" != "$release_source_snapshot" ]]; then
  echo "release source revisions changed while the gate was running" >&2
  diff -u <(printf '%s\n' "$release_source_snapshot") <(printf '%s\n' "$final_release_source_snapshot") >&2 || true
  release_gate_fence_status=1
fi
printf '%s\n' "$final_release_source_snapshot"

if [[ "$release_gate_diagnostic" == 1 ]]; then
  diagnostic_status=0
  release_gate_diagnostic_finish "$release_gate_status" "$release_gate_fence_status" || diagnostic_status=$?
  exit "$diagnostic_status"
fi

if (( release_gate_status != 0 || release_gate_fence_status != 0 )); then
  printf 'release gate failed: phases/cleanup=%s source-fences=%s\n' "$release_gate_status" "$release_gate_fence_status" >&2
  if (( release_gate_status != 0 )); then exit "$release_gate_status"; fi
  exit "$release_gate_fence_status"
fi

echo "[release-1.0 producer] launch-critical gate passed"
