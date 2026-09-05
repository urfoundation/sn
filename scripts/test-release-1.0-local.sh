#!/usr/bin/env bash
set -euo pipefail

# Release qualification cannot hide an assertion or panic behind a retry.
export WARP_TEST_ENV_FAIL_FAST=1

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace="$(dirname "$sn_repo")"
release_repos=(sn server operator-proxy connect sdk glog goidenticons proxy userwireguard vault xops config)

echo "[release-1.0] source-freeze preflight"
release_source_snapshot="$("$sn_repo/scripts/check-release-source-freeze.sh" "$workspace")"
printf '%s\n' "$release_source_snapshot"

echo "[release-1.0] simulator source-integrity preflight"
(
  cd "$sn_repo"
  go test ./sim-testnet/sourceguard -count=1
  go run ./sim-testnet/sourceguard ./sim-testnet
)

echo "[release-1.0] generated binding toolchain preflight"
"$sn_repo/stabi/generate.sh" --preflight

echo "[release-1.0] runtime 454 source attestation"
"$sn_repo/scripts/check-runtime-v454-source.sh"

echo "[release-1.0] exact runtime metadata artifact attestation"
"$sn_repo/scripts/check-runtime-metadata-artifacts.sh"

if ! command -v forge >/dev/null 2>&1 && [[ -x "$HOME/.foundry/bin/forge" ]]; then
  export PATH="$HOME/.foundry/bin:$PATH"
fi

echo "[release-1.0] sn Go tests"
(
  cd "$sn_repo"
  # Pin the launch-critical synthetic EVM identity boundary explicitly in
  # addition to the aggregate suites, including the onchain transaction tool.
  synthetic_evm_identity_tests='^Test(WaitFinalized|EVMBlockIdentity|ClaimReceiptIdentity|FinalizedClaimReceipt|UncertainClaimRetryable|SyntheticEVM|EthEVMBlockReader|EVMFinality|FinalizedEVMHead|BoundFinalizedEVMHead|ReceiptRequiresCanonicalHashAndFinalizedHeight|ProducerGatePinsSyntheticEVMIdentityRegressions)'
  go test ./miner/onchain ./miner ./sim-testnet -run "$synthetic_evm_identity_tests" -count=1 -timeout 5m
  go test -race ./miner/onchain ./miner ./sim-testnet -run "$synthetic_evm_identity_tests" -count=1 -timeout 10m
  # Reach the exact same framed-input regressions explicitly in the aggregate
  # gate, including their independent source/selector guard.
  parser_framing_tests='^Test((Policy|ReleaseConfig|ClaimDaemonConfig|StrictYAML|RenderedValidatorPolicy)RejectsMalformedTrailingYAML|FinalSemanticPathProofArtifact(Count|RejectsMalformedTrailingJSON)|ParseFleetManifestStrictCanonicalRoundTrip|PolicyStrictAndFailClosed|LoadReleaseConfigStrictAndNormalizesOperatorSecrets|ClaimDaemonConfigStrictAndPortable|StrictYAMLRejectsUnknownAndMultipleDocuments|ReleaseGatesPinProviderAndTransportRegressions)$'
  go test ./protocol ./miner ./validator ./sim-testnet -run "$parser_framing_tests" -count=1 -parallel=4 -timeout 2m
  go test -race ./protocol ./miner ./validator ./sim-testnet -run "$parser_framing_tests" -count=1 -parallel=4 -timeout 2m
  # sim-testnet contains launch-scale 1,000-miner fixtures. The package has an
  # isolated 90-minute race deadline below; do not let Go's implicit 10-minute
  # package deadline terminate the faster ordinary pass while its independent
  # durability tests are still running.
  go test -parallel=4 -timeout 90m ./...
  go test -race ./crv4 ./miner/... ./protocol ./validator
  validator_lifecycle_tests='^Test(TunnelAttemptCloseJoinsPumpBeforeGenerator|TunnelAttemptCloseReleasesPartialConstruction)$'
  go test ./validator -run "$validator_lifecycle_tests" -count=1
  go test -race ./validator -run "$validator_lifecycle_tests" -count=1
  # Keep the launch-scale simulator isolated so its package deadline and any
  # race report remain attributable without weakening the 1,000-miner tests.
  # Three isolated launch-integrity shards require more than 62m when summed,
  # before the rest of the package and concurrent live-campaign load. Keep
  # deterministic headroom for the complete package and slower CI hosts; this
  # changes only the harness deadline, never the test selection.
  go test -race -parallel=4 -timeout 90m ./sim-testnet
)

echo "[release-1.0] deployable Solidity static analysis"
"$sn_repo/scripts/test-solidity-static.sh"

echo "[release-1.0] Solidity format, build, and tests"
(
  cd "$sn_repo/evm"
  forge fmt --check
  forge build --deny warnings --sizes
  forge test --summary
)

echo "[release-1.0] generated contract payload and storage-layout lock"
(
  cd "$sn_repo"
  # Solidity's IPFS metadata digest includes Foundry's compilation graph. A
  # target-only Slither build and a clean full-project build can therefore
  # produce different digests with byte-for-byte identical executable code.
  # Preserve the exact release-locked/live payload while rejecting every
  # difference outside that one structurally validated 32-byte field.
  go run ./sim-testnet/gencontracts --check evm/out sim-testnet/contracts_gen.go
)

echo "[release-1.0] generated ABI and Go binding freshness"
"$sn_repo/stabi/generate.sh" --check

echo "[release-1.0] operator pure/unit suites"
(
  cd "$workspace/server"
  test_env_fail_fast_tests='^Test(DefaultTestEnvReleaseFailFast|RunRetriesUntilPass|RunFailsAfterExhaustion|RunReportsPanicOriginAfterExhaustion)'
  go test . -run "$test_env_fail_fast_tests" -count=1
  go test -race . -run "$test_env_fail_fast_tests" -count=1
  go test . -run '^Test(PgResourcesRedirectMaintenancePoolAndRestore|DatabaseTimeMatchesPostgresPrecision)$'
  go test ./st ./startifact
  go test ./controller -run '^Test(CoreStClient(BlockHashes|FinalizedHead|Epoch)|CoreStClientBindingsAt|DecodeStRPCBlockIdentity|StatsAlphaPriceURLIsMainnetOnly|StatsGaugeVecReplaceDeletesStaleSeries|StConfig|StCompute|StBuild|StDeposit|StEstimate|StReplacement|StDecode|StEvent|StBroadcast|StClientStub|StTransactionCancellation|VerifyEvidenceRange|VerifyKeyRotation|VerifySyntheticSeedId|VerifyUsesUrForwardedAddress|VerifyIgnoresLegacyForwardedAddress|VerifyClampM|VerifyCachedResponseRoundTrip|VerifySeedRejectsMissingSignature|StripeReconcileCredentialsRequireNonblankAPIToken|AppleReconcileCredentialsRequireCompleteServerAPIIdentity|PlayReconcileCredentialsRequireOAuthPackageAndSKUs|SolanaReconcileCredentialsRequireNonblankHeliusAPIKey)'
  provider_input_tests='^Test(StCanonicalProviderUsages|StBuildReleaseProviderInputs)'
  go test ./controller -run "$provider_input_tests" -count=1
  go test -race ./controller -run "$provider_input_tests" -count=1
  payout_allocation_tests='^Test(EvenContractPayoutShare|AllocateContractParticipantPayouts|AllocateContractParticipantPayoutEligibilityMatrix)$'
  go test ./model -run "$payout_allocation_tests" -count=1
  go test -race ./model -run "$payout_allocation_tests" -count=1
  # Keep every file-backed validator filter regression in the aggregate gate,
  # while excluding the two stateful cases until the isolated DB profile below.
  filter_pure_tests='^TestVerifySimulationAssignmentFilter(IsValidatorLocalAndFailClosed|RejectsAmbiguousFiles|V[12].*|Rejects(Leaf|Parent)Symlink|AtomicReplacementPinsOpenedDescriptor)$'
  go test ./controller -run "$filter_pure_tests" -count=1
  go test -race ./controller -run "$filter_pure_tests" -count=1
  go test ./session -run 'Test.*(UrForwardedAddress|LegacyForwardedHeaders|RemoteAddress)'
  go test ./router -run 'TestTrie'
  go test ./model -run '^Test(VerifyEgressExactIndexAndPrefixScoreAreIndependent|StTransactionAdvisoryLockKeyUsesEthereumNonceScope|StHeadBoundCkeysFromEvents|ParseHeadEventCkey)$'
  go test ./taskworker/work -run '^TestStSettlementTasksRejectStaleCoordinatorPayloads$'
  go test ./monitor
  go test -race ./monitor
  # The immutable sim-latency baseline contains manifest-locked reference test
  # inputs that compile only after their archived patches are applied. Verify
  # that dataset with its own checker and compile every executable package.
  "$workspace/server/connect/sim-latency/baseline/verify.sh" >/dev/null
  echo "sim-latency immutable baseline: verified"
  server_package_list="$(go list ./... | grep -v '^github\.com/urnetwork/server/connect/sim-latency/baseline/')"
  [[ -n "$server_package_list" ]]
  mapfile -t server_packages <<<"$server_package_list"
  go test "${server_packages[@]}" -run '^$'
)

echo "[release-1.0] shared verify wire and public SDK suites"
(
  cd "$workspace/connect"
  go test ./... -run '^$'
  go test . -run '^Test(Verify|Sn)'

  transport_identity_tests='^Test(PlatformTransportAuthSnapshotsAreAtomicAndOwned|PlatformTransportH[13]ReconnectUsesUpdatedAuthSnapshot|TunTcpInboundFlowUsesStableBoundedShards|TunTcpInboundShardHandoffCadenceIsBounded|TunWriteCompletesFiniteTcpInboundHandoffBeforeReturn|TunWriteRetainsTcpInboundYieldCadence|TunWriteBatchFinishesEveryTcpInboundHandoff)$'
  go test . -run "$transport_identity_tests" -count=1
  go test -race . -run "$transport_identity_tests" -count=1

  contract_sender_diagnostic_tests='^TestCreateContractReportsSenderSequenceRole$'
  go test . -run "$contract_sender_diagnostic_tests" -count=1
  go test -race . -run "$contract_sender_diagnostic_tests" -count=1

  # The 1,000-device simulator depends on Connect retaining its per-device
  # socket budget even when quic-go requests a process-oriented 7 MiB buffer.
  socket_cap_tests='^TestPlatformPacketConnClampsQuic(SocketRequests|RequestToDeviceMemoryTarget)$'
  go test . -run "$socket_cap_tests" -count=1
  go test -race . -run "$socket_cap_tests" -count=1

  # Generated blocker/CFAA data is release-critical. Exercise the checked-in
  # table invariants, concurrent readers, and both generator contracts whenever
  # the pinned Connect source changes.
  security_table_tests='^Test(BlockerGeneratedTables|BlockerDefaultDataSmoke|BlockerDataGuards|BlockerHashVectors|BlockerZeroAlloc|BlockerFalsePositiveProbe|BlockerIp4|BlockerIp6|BlockerToggleRace|CfaaPortClassification|CfaaBlockedIps|CfaaDisabled|CfaaIngressMirrorsSourceDrops|CfaaBlockedPrefixInvariant|CfaaBlockedIp4BruteForce|CfaaBlockedIp4ZeroAlloc|CfaaSearch6|CfaaInspectV6|CfaaBlockedPrefix6Invariant|CfaaInspectIcmp|TelegramCallReflectorRanges|TelegramCallV12TcpFallback|CfaaTelegramCallException|SecurityPolicyAllowsTelegramCallReflectors)$'
  go test . -run "$security_table_tests" -count=1
  go test -race . -run "$security_table_tests" -count=1
  go test ./blocker ./security -count=1
  go test -race ./blocker ./security -count=1

  # The release simulator depends on the real unordered P2P carrier and its
  # in-memory signaling counterpart. Keep the original fast-path failure and
  # every adjacent registration, capacity, cancellation, and ownership
  # regression in both frozen gates.
  p2p_signal_tests='^Test(WebRtc|WebRtcMessageRoundTrip|P2pTransportAutoSelectsFastPathForCapablePeer|SignalPipeDropsBeforeDestinationRegistration|DelayedSignalPipe(ResolvesDestinationAtDispatch|DropsMissingDestinationAtDispatch|CancellationReturnsOwnedFrames|FullQueueDoesNotBlockDispatch|CancellationUnblocksCapacitySender)|MatchExpectedUnorderedP2pMessages(AcceptsPermutation|RejectsInvalidContent))$'
  go test . -run "$p2p_signal_tests" -count=1
  go test -race . -run "$p2p_signal_tests" -count=1

  # Cancellation is only a stop request: every admitted strategy/dial/stream
  # worker must be joined before its owner publishes lifecycle completion.
  lifecycle_tests='^Test(ClientStrategyParentCancellationClosesIdleConnections|ClientStrategyCloseClosesIdleConnections|SerialEvalReservesRequestBudgetFromStalePreferredDialer|ParallelEvalReservesRequestBudgetFromStalePreferredDialer|ParallelEvalCancellationJoinsAttemptWorker|PlatformTransportCloseAndWaitJoinsPendingDial|PlatformTransportCloseAndWaitJoinsRouteWriterAndReceiveCleanup|StreamReplacementReceiveDoesNotJoinAndPublishesAfterOldExit|AddrGeneratorCloseJoinsBlockedProducer|ClientCancelClosesContractManagerAdmission|PeerConnectionResolveNetCancelsStunAndTurnLookups|WebRtcPeerTeardownCancelsBlockedStunResolution|PeerConnPionStartupAndTeardownAreSerialized|WebRtcManagerCloseAndWaitReleasesOwnedResources|WebRtcTestManagersHaveJoiningOwners|P2pStreamProbeStreamSequenceCancelSynchronouslyWithdrawsReadiness|ZZZNoPerInstanceLifecycleResidue)$'
  go test . -run "$lifecycle_tests" -count=1
  go test -race . -run "$lifecycle_tests" -count=1

  # Shards cannot expose package-global residue carried between otherwise
  # unrelated root tests. Certify one unsharded default-order process and one
  # reproducible alternate order, with bounded CPU so race/WebRTC timings are
  # not distorted by host-wide saturation.
  go test -count=1 -timeout 30m .
  GOMAXPROCS=4 go test -race -count=1 -timeout 30m .
  GOMAXPROCS=4 go test -race -count=1 -timeout 30m -shuffle=4535211000 .
)
(
  cd "$workspace/sdk"
  go test ./... -run '^$'
  go test . -run '^Test(ApiSubnet|ProviderLocalUserNatSettings)'
  token_transport_tests='^Test(ApiTokenManager|DeviceRemoteRpcPublicationWakesOnlyOutstandingRefresh|ApiCloseAndWaitJoinsRefreshWorker|DeviceLocalAppliesApiRefreshAndLogout|DeviceRemoteAppliesStandaloneApiRefreshAndLogout)'
  go test . -run "$token_transport_tests" -count=1
  go test -race . -run "$token_transport_tests" -count=1
  sdk_lifecycle_tests='^Test(ApiCloseAndWaitJoinsRefreshWorker|NetworkSpaceCloseJoinsClientStrategyRelease|NetworkSpaceManagerReplacementDoesNotHoldStateLockDuringClose|NetworkSpaceManagerStaleRemovePreservesReplacement|NetworkSpaceManagerStaleActiveSelectionPreservesReplacement|NetworkSpaceCloseJoinsAsyncLocalStateAndRejectsLateJob|NetworkSpaceManagerCloseRejectsRacingUpdate|SimProviderDisconnectJoinsPendingTransportDial|SimProviderCloseJoinsPendingTransportDial|DeviceLocalProviderCloseAndWaitJoinsAdmittedMigration|DeviceLocalCloseAndWaitJoinsOwnedApiRefresh|DeviceLocalCloseAndWaitJoinsDestinationGeneration|SecurityPolicyMonitorCloseAndWaitJoinsRun|DeviceLocalRpcManagerCloseAndWaitJoinsAccept|DeviceRemoteRpcCloseAndWaitJoinsAdmittedCallback|DeviceRemoteCloseAndWaitJoinsBlockedDial|DeviceRemoteSetRpcServerRejectsAfterClose|RpcClientCallParentCancellationClosesTransport|DeviceLocalAppliesApiRefreshAndLogout|DeviceRemoteAppliesStandaloneApiRefreshAndLogout|ApiSubnetHeadlessBindings|ApiHeadlessAuthAndProviderBindings|ApiSubnetPoolClaimEscapesNoUserInputIntoQuery|SubscriptionBalanceDecode|CheckBalanceCode|CheckBalanceCodeUnknownCode|RedeemBalanceCodeDecodeAndClassify|Stripe.*|VerifyPlayPurchase|VerifyAppleTransaction)$'
  go test . -run "$sdk_lifecycle_tests" -count=1
  go test -race . -run "$sdk_lifecycle_tests" -count=1
)
(
  cd "$workspace/sdk/build"
  # The mobile build is a separate Go module, so the SDK-root ./... pattern
  # cannot exercise its fail-closed gomobile export policy.
  go test ./cmd/mobileexports -count=1
  go test -race ./cmd/mobileexports -count=1
)

echo "[release-1.0] operator-proxy module"
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

echo "[release-1.0] operator Connect ingress and owned-session lifecycle regressions"
(
  cd "$workspace/server"
  direct_h3_tests='^TestRun(SettingsDirectH3LoopbackBypassesProxyProtocol|RouterRetainsDirectH3LoopbackSettings|RouterDirectH3LoopbackCompletesHandshake|DirectH3LoopbackModeRejectsNonLoopbackListener)$'
  go test ./connect -run "$direct_h3_tests" -count=1
  go test -race ./connect -run "$direct_h3_tests" -count=1
  go test ./connect -run '^TestConnectionVerifyEgressDisabledAvoidsVerifySettings$' -count=1
  go test -race ./connect -run '^TestConnectionVerifyEgressDisabledAvoidsVerifySettings$' -count=1
  go test ./connect/sim-latency -run '^TestClientDriverProbeMatchmakingUsesPoolIdentityAndQualitySpec$' -count=1
  go test -race ./connect/sim-latency -run '^TestClientDriverProbeMatchmakingUsesPoolIdentityAndQualitySpec$' -count=1
  proxy_lifecycle_tests='^Test(ProxyDeviceManagerSharesOneNetworkSpaceLifetime|ProxyDeviceManagerCloseAndWaitJoinsOwnedNetworkSpace|ProxyDeviceManagerCloseJoinsAdmittedOpenAndRejectsLateOpen|ProxyDeviceManagerPreservesInjectedNetworkSpace|WgClientStackCloseAndWaitJoinsTCPDispatcher)$'
  go test ./proxy -run "$proxy_lifecycle_tests" -count=1
  go test -race ./proxy -run "$proxy_lifecycle_tests" -count=1
)

if [[ "${RUN_SERVER_DB_TESTS:-0}" == "1" ]]; then
  echo "[release-1.0] operator PostgreSQL/Redis integration suites"
  (
    cd "$workspace/server"
    # Never inherit a main/canary server identity into tests which create and
    # drop databases. These names resolve to server/local's dedicated 10.213.0.1
    # containers; 127.0.0.1 is intentionally forbidden by that stack.
    export WARP_ENV=local
    export WARP_SERVICE=test
    export WARP_DOMAIN=bringyour.com
    export WARP_BLOCK=test
    export WARP_VERSION=0.0.0
    export BRINGYOUR_POSTGRES_HOSTNAME=local-pg.bringyour.com
    export BRINGYOUR_REDIS_HOSTNAME=local-redis.bringyour.com
    controller_db_tests='^Test(CreateContractRejectsInactiveClient|VerifyController(FullTrailFlow|PoisonAndFailurePaths|ConcurrentExtendReloadsAfterLock|ReplayCannotReadANewerCachedResponse)|VerifySimulationAssignmentFilter(BlocksSeedPendingAndFutureAssignments|DoesNotAffectAnotherValidator)|AuthNetworkClientFeedsConfiguredProxyEgressNamespace|PaymentReconcile(SkipsStripeWithSKUOnlyVault|MalformedCredentialResourcesSkipAllStores)|StAccountReconcile|StSyncChainEventsBatchesCanonicalEventBlocks|StSyncChainEventsRejectsIncompleteCanonicalBatchBeforeMutation)'
    model_db_tests='Test(FindActiveClientNetwork|StreamHopListenerPrunesInactiveAdjacentClients|ActiveStreamHopsBoundsConcurrentStaleReAdd|ForceCloseRequiresPositiveParallelism|ForceCloseDisputedContract|ForceCloseDirectSettlementRemovesStream|ForceCloseMalformedContractRemovesStreamAndReturnsError|SweepOrphanClearsProxyConfigRedis|SweepOrphanReapsProxyClients|VerifyEgressIndexStoresNoRawIp|VerifyTrailLockMutualExclusion|VerifyTrailLockStaleReleasePreservesSuccessor|SweepExpiredVerifyTrails|VerifyTrailMutationLockTtlCoversLoadedTrail|StDeploymentStateIsIsolatedAcrossCoordinatorReplacements|StTransactionIntentReservationUsesChainAccountNonceScope|StTransactionRevertRetryCreatesOneImmutableSuccessor|StTransactionAttemptCandidatesConvergeOnOneWinner|StTransactionCancellationCannotRegress|StTransactionFinalizedAttemptCannotRegress)'
    go test ./controller -run "$controller_db_tests"
    go test ./model -run "$model_db_tests"
    provider_attribution_tests='^Test(ContractPayout|CompanionContractPayout|ContractParticipant|StEpochProviderUsage|StatsProviderPayouts|StatsProviders|StatsQueryPlans|SearchProviderStatsRollup|RemoveOldSearchProviderStats|RemoveOldVerifyProviderStats)'
    go test ./model -run "$provider_attribution_tests" -count=1
    go test -race ./model -run "$provider_attribution_tests" -count=1
    # Task registration is part of the operator startup boundary. Keep its
    # database-backed stale-chain cleanup regressions in the managed profile;
    # running them without this profile only exercises TestEnv's retry path.
    go test ./taskworker -count=1
    # Account-wide nonce reconciliation, canonical event batching and the
    # validator-local assignment filter all mutate shared database state. Keep
    # their focused race coverage in the repeatable release gate as well as the
    # deterministic ordinary suite.
    go test -race ./controller -run "$controller_db_tests"
    go test -race ./model -run "$model_db_tests"
    go test -race ./taskworker -count=1
    go test ./connect -run '^TestConnectionVerifyEgressUsesControllerHashNamespace$' -count=1
    go test -race ./connect -run '^TestConnectionVerifyEgressUsesControllerHashNamespace$' -count=1
    go test . -run '^TestIncrementRateLimitWindowResetsAtExactBoundary$' -count=1
    go test -race . -run '^TestIncrementRateLimitWindowResetsAtExactBoundary$' -count=1
    # Run the complete operator proxy surface. Its real network/DB roots have a
    # measured lower bound above Go's implicit ten-minute package deadline, so
    # the explicit deadline supplies deterministic headroom without changing
    # selection. Every hosted-device and client netstack is synchronously joined
    # before the following root is admitted.
    go test -timeout 20m ./proxy -count=1
  )
else
  echo "[release-1.0] DB suites deferred to managed launch profile (set RUN_SERVER_DB_TESTS=1 when WARP_ENV/PostgreSQL/Redis are available)"
fi

echo "[release-1.0] Subtensor infrastructure regressions"
(
  cd "$workspace/xops"
  python3 -m unittest \
    main/ansible/tests/test_subtensor_playbook.py \
    main/ansible/tests/test_vulnscan2_resolved.py
)

echo "[release-1.0] patch hygiene"
for repo in "${release_repos[@]}"; do
  git -C "$workspace/$repo" diff --check
  git -C "$workspace/$repo" diff --cached --check
done

echo "[release-1.0] final release-lock checkout"
(
  cd "$sn_repo"
  # The full Go suite checks this near the start. Repeat it after every long
  # gate so an independently updated sibling checkout cannot escape the run.
  go test ./sim-testnet -run '^TestReleaseLockMatchesCheckout$' -count=1
)

echo "[release-1.0] final source-freeze checkout"
final_release_source_snapshot="$("$sn_repo/scripts/check-release-source-freeze.sh" "$workspace")"
if [[ "$final_release_source_snapshot" != "$release_source_snapshot" ]]; then
  echo "release source revisions changed while the gate was running" >&2
  diff -u <(printf '%s\n' "$release_source_snapshot") <(printf '%s\n' "$final_release_source_snapshot") >&2 || true
  exit 1
fi
printf '%s\n' "$final_release_source_snapshot"

echo "[release-1.0] local release gate passed"
