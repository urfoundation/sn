#!/usr/bin/env bash
set -euo pipefail

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace="$(dirname "$sn_repo")"
release_repos=(sn server operator-proxy connect sdk glog goidenticons proxy userwireguard vault xops config)

echo "[release-1.0 producer] source-freeze preflight"
release_source_snapshot="$("$sn_repo/scripts/check-release-source-freeze.sh" "$workspace")"
printf '%s\n' "$release_source_snapshot"

echo "[release-1.0 producer] runtime 453 source attestation"
"$sn_repo/scripts/check-runtime-v453-source.sh"

echo "[release-1.0 producer] exact runtime metadata artifact attestation"
"$sn_repo/scripts/check-runtime-metadata-artifacts.sh"

if ! command -v forge >/dev/null 2>&1 && [[ -x "$HOME/.foundry/bin/forge" ]]; then
  export PATH="$HOME/.foundry/bin:$PATH"
fi

# This is the bounded launch-critical gate. The complete aggregate gate remains
# mandatory for final acceptance, but it is deliberately not repeated on the
# critical path after the release-locked source has passed it. Semantic
# reconstruction, public archive replay, report rendering and broad suites run
# concurrently with the live acceptance windows.
echo "[release-1.0 producer] compile complete simulator and validator graph"
(
  cd "$sn_repo"
  go test ./validator ./sim-testnet -run '^$' -count=1
)

echo "[release-1.0 producer] signed validator evidence and settlement"
(
  cd "$sn_repo"
  producer_tests='^Test(Attempt|Deposited|ReleaseMeasurement|IntentStore|SteeringIntent|MeasurementStats|ExactPoolQuality|ReleaseSteeringLoop)'
  go test ./validator -run "$producer_tests" -count=1
  go test -race ./validator -run "$producer_tests" -count=1
)

echo "[release-1.0 producer] lossless capture, completion, and publication"
(
  cd "$sn_repo"
  capture_tests='^Test(FinalArchive|FinalCompositeArchive|ArchivePreflight|FinalClaimQueueCapture|FinalCollected(Bundle|File|Chain)|FinalSemantic(PublicCapture|LaunchFoundation)|FinalContractCleanupCapture|VerifyFinalCollected|FleetLifecycle|CanonicalRPCReceiptLogs|ScenarioProcessLogGate|ReleaseAndProductionScenariosRequireProcessLogGate|ScenarioCompletion|ScenarioRunner(WritesCompleteEvidenceOnlyOnPass|FailureHasNoCompleteMarker)|PublishedScenarioCandidateKeepsFrozenHashWhenClockAdvances|PublishedCompletionCommits|CampaignEvidence|DirectScenarioCompletion|EvidenceFileHashes|ArchiveCurrentDeploymentPublication|VerifyPublishedEvidenceOrigin|ReleaseCandidateCampaign|ProductionCampaignCompletion|ReleaseCampaignGate|ExactReleaseCampaignGate|ScenarioCampaignAttempt|ProductionHandoff|InitialScenarioFailure|ProductionPolicyEvidence|PrepareSignedAttemptStateNamespace|ClassifyValidatorAttemptState|ProducerGateCaptureSelection)'
  # This is deliberately capture-only: typed semantic reconstruction, public
  # replay, supplement publication and FINAL.md rendering run post-capture.
  go test ./sim-testnet -run "$capture_tests" -count=1 -timeout 5m
  go test -race ./sim-testnet -run "$capture_tests" -count=1 -timeout 10m
)

echo "[release-1.0 producer] exact deployable contract behavior"
(
  cd "$sn_repo/evm"
  forge fmt --check
  forge build --deny warnings --sizes
  forge test --summary
)
(
  cd "$sn_repo"
  go run ./sim-testnet/gencontracts --check evm/out sim-testnet/contracts_gen.go
  ./stabi/generate.sh --check
)

echo "[release-1.0 producer] operator proof and artifact APIs"
(
  cd "$workspace/server"
  go test ./st ./startifact -count=1
  go test ./controller -run '^Test(CoreStClient|StConfig|StCompute|StBuild|StDeposit|StEstimate|StReplacement|StDecode|StEvent|StBroadcast|StClientStub|StTransactionCancellation|VerifyEvidenceRange|VerifyKeyRotation|VerifySyntheticSeedId|VerifyUsesUrForwardedAddress|VerifyIgnoresLegacyForwardedAddress|VerifyClampM|VerifyCachedResponseRoundTrip|VerifySeedRejectsMissingSignature)' -count=1
  # The seed/future-assignment cases below are database-backed and run only in
  # the isolated profile. Keep this selector explicit so a broad prefix cannot
  # silently admit a new *_db_test.go case before WARP_ENV is configured.
  filter_pure_tests='^TestVerifySimulationAssignmentFilter(IsValidatorLocalAndFailClosed|RejectsAmbiguousFiles|V[12].*|Rejects(Leaf|Parent)Symlink|AtomicReplacementPinsOpenedDescriptor)$'
  go test ./controller -run "$filter_pure_tests" -count=1
  go test -race ./controller -run "$filter_pure_tests" -count=1
)
(
  cd "$workspace/connect"
  go test . -run '^Test(Verify|Sn|PlatformPacketConnClampsQuic)' -count=1
)

echo "[release-1.0 producer] operator-proxy source and behavior"
(
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
)

echo "[release-1.0 producer] isolated PostgreSQL/Redis evidence path"
(
  cd "$workspace/server"
  export WARP_ENV=local
  export WARP_SERVICE=test
  export WARP_DOMAIN=bringyour.com
  export WARP_BLOCK=test
  export WARP_VERSION=0.0.0
  export BRINGYOUR_POSTGRES_HOSTNAME=local-pg.bringyour.com
  export BRINGYOUR_REDIS_HOSTNAME=local-redis.bringyour.com
  controller_tests='^Test(VerifyController(FullTrailFlow|PoisonAndFailurePaths|ConcurrentExtendReloadsAfterLock|ReplayCannotReadANewerCachedResponse)|VerifySimulationAssignmentFilter(BlocksSeedPendingAndFutureAssignments|DoesNotAffectAnotherValidator)|StAccountReconcile|StSyncChainEventsBatchesCanonicalEventBlocks|StSyncChainEventsRejectsIncompleteCanonicalBatchBeforeMutation)'
  model_tests='^Test(VerifyEgressExactIndexAndPrefixScoreAreIndependent|VerifyTrailLockMutualExclusion|VerifyTrailLockStaleReleasePreservesSuccessor|StDeploymentStateIsIsolatedAcrossCoordinatorReplacements|StTransactionIntentReservationUsesChainAccountNonceScope|StTransactionAttemptCandidatesConvergeOnOneWinner|StTransactionFinalizedAttemptCannotRegress)'
  go test ./controller -run "$controller_tests" -count=1
  go test ./model -run "$model_tests" -count=1
  go test ./taskworker -count=1
)

echo "[release-1.0 producer] source and release-lock fence"
for repo in "${release_repos[@]}"; do
  git -C "$workspace/$repo" diff --check
  git -C "$workspace/$repo" diff --cached --check
done
(
  cd "$sn_repo"
  go test ./sim-testnet -run '^TestReleaseLockMatchesCheckout$' -count=1
)

echo "[release-1.0 producer] final source-freeze checkout"
final_release_source_snapshot="$("$sn_repo/scripts/check-release-source-freeze.sh" "$workspace")"
if [[ "$final_release_source_snapshot" != "$release_source_snapshot" ]]; then
  echo "release source revisions changed while the gate was running" >&2
  diff -u <(printf '%s\n' "$release_source_snapshot") <(printf '%s\n' "$final_release_source_snapshot") >&2 || true
  exit 1
fi
printf '%s\n' "$final_release_source_snapshot"

echo "[release-1.0 producer] launch-critical gate passed"
