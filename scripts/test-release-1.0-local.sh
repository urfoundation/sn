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
  # Keep the launch-scale simulator isolated so its package deadline and any
  # race report remain attributable without weakening the 1,000-miner tests.
  go test -race -timeout 10m ./sim-testnet
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
  go test . -run '^TestPgResourcesRedirectMaintenancePoolAndRestore$'
  go test ./st ./startifact
  go test ./controller -run '^Test(CoreStClientEpochUsesOneFinalizedBlock|StatsAlphaPriceURLIsMainnetOnly|StatsGaugeVecReplaceDeletesStaleSeries|StConfig|StCompute|StBuild|StDeposit|StEstimate|StReplacement|StDecode|StEvent|StBroadcast|StClientStub|VerifyEvidenceRange|VerifyKeyRotation|VerifySyntheticSeedId|VerifyUsesUrForwardedAddress|VerifyIgnoresLegacyForwardedAddress|VerifyClampM|VerifyCachedResponseRoundTrip|VerifySeedRejectsMissingSignature)'
  go test ./session -run 'Test.*(UrForwardedAddress|LegacyForwardedHeaders|RemoteAddress)'
  go test ./router -run 'TestTrie'
  go test ./model -run '^TestVerifyEgressExactIndexAndPrefixScoreAreIndependent$'
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
)
(
  cd "$workspace/sdk"
  go test ./... -run '^$'
  go test . -run '^Test(ApiSubnet|ProviderLocalUserNatSettings)'
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
