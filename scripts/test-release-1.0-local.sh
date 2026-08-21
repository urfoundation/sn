#!/usr/bin/env bash
set -euo pipefail

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace="$(dirname "$sn_repo")"

if ! command -v forge >/dev/null 2>&1 && [[ -x "$HOME/.foundry/bin/forge" ]]; then
  export PATH="$HOME/.foundry/bin:$PATH"
fi

echo "[release-1.0] sn Go tests"
(
  cd "$sn_repo"
  go test ./...
  go test -race ./crv4 ./miner/... ./protocol ./sim-testnet ./validator
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
generated_contracts="$(mktemp)"
trap 'rm -f -- "$generated_contracts"' EXIT
(
  cd "$sn_repo"
  go run ./sim-testnet/gencontracts evm/out "$generated_contracts"
)
if ! cmp -s "$sn_repo/sim-testnet/contracts_gen.go" "$generated_contracts"; then
  echo "generated contract payload is stale; run: go generate ./sim-testnet" >&2
  exit 1
fi

echo "[release-1.0] generated ABI and Go binding freshness"
"$sn_repo/stabi/generate.sh" --check

echo "[release-1.0] operator pure/unit suites"
(
  cd "$workspace/server"
  go test ./st ./startifact
  go test ./controller -run '^Test(StConfig|StCompute|StBuild|StDeposit|StEstimate|StReplacement|StDecode|StBroadcast|StClientStub|VerifyEvidenceRange|VerifyKeyRotation|VerifySyntheticSeedId|VerifySourceIp|VerifyClampM|VerifyCachedResponseRoundTrip|VerifySeedRejectsMissingSignature)'
  go test ./session -run 'Test(ParseTrustedProxyPrefixes|ResolveClientAddress)'
  go test ./router -run 'TestTrie'
  go test ./model -run '^TestVerifyEgressExactIndexAndPrefixScoreAreIndependent$'
  go test ./api/... ./model -run '^$'
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
for repo in sn server connect sdk vault xops; do
  git -C "$workspace/$repo" diff --check
done

echo "[release-1.0] local release gate passed"
