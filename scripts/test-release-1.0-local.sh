#!/usr/bin/env bash
set -euo pipefail

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace="$(dirname "$sn_repo")"
release_repos=(sn server connect sdk glog goidenticons proxy userwireguard vault xops config)

if ! command -v forge >/dev/null 2>&1 && [[ -x "$HOME/.foundry/bin/forge" ]]; then
  export PATH="$HOME/.foundry/bin:$PATH"
fi

echo "[release-1.0] sn Go tests"
(
  cd "$sn_repo"
  go test ./...
  go test -race ./crv4 ./miner/... ./protocol ./validator
  validator_lifecycle_tests='^Test(TunnelAttemptCloseJoinsPumpBeforeGenerator|TunnelAttemptCloseReleasesPartialConstruction)$'
  go test ./validator -run "$validator_lifecycle_tests" -count=1
  go test -race ./validator -run "$validator_lifecycle_tests" -count=1
  # Keep the launch-scale simulator isolated so its package deadline and any
  # race report remain attributable without weakening the 1,000-miner tests.
  # The launch-scale race suite's measured baseline already exceeds 9m40s.
  # Keep deterministic headroom for required regressions and slower CI hosts;
  # this changes only the harness deadline, never the test selection.
  go test -race -timeout 15m ./sim-testnet
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
  go test . -run '^Test(PgResourcesRedirectMaintenancePoolAndRestore|DatabaseTimeMatchesPostgresPrecision)$'
  go test ./st ./startifact
  go test ./controller -run '^Test(CoreStClientEpochUsesOneFinalizedBlock|StatsAlphaPriceURLIsMainnetOnly|StatsGaugeVecReplaceDeletesStaleSeries|StConfig|StCompute|StBuild|StDeposit|StEstimate|StReplacement|StDecode|StEvent|StBroadcast|StClientStub|VerifyEvidenceRange|VerifyKeyRotation|VerifySyntheticSeedId|VerifyUsesUrForwardedAddress|VerifyIgnoresLegacyForwardedAddress|VerifyClampM|VerifyCachedResponseRoundTrip|VerifySeedRejectsMissingSignature)'
  go test ./session -run 'Test.*(UrForwardedAddress|LegacyForwardedHeaders|RemoteAddress)'
  go test ./router -run 'TestTrie'
  go test ./model -run '^TestVerifyEgressExactIndexAndPrefixScoreAreIndependent$'
  go test ./monitor
  go test -race ./monitor
  # The immutable sim-latency baseline contains manifest-locked reference test
  # inputs that compile only after their archived patches are applied. Verify
  # that dataset with its own checker and compile every executable package.
  "$workspace/server/connect/sim-latency/baseline/verify.sh" >/dev/null
  echo "sim-latency immutable baseline: verified"
  mapfile -t server_packages < <(go list ./... | grep -v '^github\.com/urnetwork/server/connect/sim-latency/baseline/')
  go test "${server_packages[@]}" -run '^$'
)

echo "[release-1.0] shared verify wire and public SDK suites"
(
  cd "$workspace/connect"
  go test ./... -run '^$'
  go test . -run '^Test(Verify|Sn)'

  # Generated blocker/CFAA data is release-critical. Exercise both the
  # checked-in table invariants and their concurrent readers whenever the
  # pinned Connect source changes.
  security_table_tests='^Test(BlockerGeneratedTables|BlockerDefaultDataSmoke|BlockerDataGuards|BlockerHashVectors|CfaaBlockedIps|CfaaBlockedPrefixInvariant|CfaaBlockedPrefix6Invariant)$'
  go test . -run "$security_table_tests" -count=1
  go test -race . -run "$security_table_tests" -count=1

  # Cancellation is only a stop request: every admitted strategy/dial/stream
  # worker must be joined before its owner publishes lifecycle completion.
  lifecycle_tests='^Test(ClientStrategyParentCancellationClosesIdleConnections|ClientStrategyCloseClosesIdleConnections|SerialEvalReservesRequestBudgetFromStalePreferredDialer|ParallelEvalReservesRequestBudgetFromStalePreferredDialer|ParallelEvalCancellationJoinsAttemptWorker|PlatformTransportCloseAndWaitJoinsPendingDial|PlatformTransportCloseAndWaitJoinsRouteWriterAndReceiveCleanup|StreamReplacementReceiveDoesNotJoinAndPublishesAfterOldExit|AddrGeneratorCloseJoinsBlockedProducer|ClientCancelClosesContractManagerAdmission|PeerConnPionStartupAndTeardownAreSerialized|WebRtc|WebRtcManagerCloseAndWaitReleasesOwnedResources|WebRtcTestManagersHaveJoiningOwners|P2pStreamProbeStreamSequenceCancelSynchronouslyWithdrawsReadiness|ZZZNoPerInstanceLifecycleResidue)$'
  go test . -run "$lifecycle_tests" -count=1
  go test -race . -run "$lifecycle_tests" -count=1
)
(
  cd "$workspace/sdk"
  go test ./... -run '^$'
  go test . -run '^Test(ApiSubnet|ProviderLocalUserNatSettings)'
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

echo "[release-1.0] operator Connect ingress and owned-session lifecycle regressions"
(
  cd "$workspace/server"
  direct_h3_tests='^TestRun(SettingsDirectH3LoopbackBypassesProxyProtocol|RouterRetainsDirectH3LoopbackSettings|RouterDirectH3LoopbackCompletesHandshake|DirectH3LoopbackModeRejectsNonLoopbackListener)$'
  go test ./connect -run "$direct_h3_tests" -count=1
  go test -race ./connect -run "$direct_h3_tests" -count=1
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
    go test ./controller -run 'TestVerifyController(FullTrailFlow|PoisonAndFailurePaths|ConcurrentExtendReloadsAfterLock|ReplayCannotReadANewerCachedResponse)'
    go test ./model -run 'Test(SweepOrphanClearsProxyConfigRedis|SweepOrphanReapsProxyClients|VerifyEgressIndexStoresNoRawIp|VerifyTrailLockMutualExclusion|VerifyTrailLockStaleReleasePreservesSuccessor|SweepExpiredVerifyTrails|VerifyTrailMutationLockTtlCoversLoadedTrail)'
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

echo "[release-1.0] local release gate passed"
