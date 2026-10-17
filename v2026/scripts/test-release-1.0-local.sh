#!/usr/bin/env bash
set -euo pipefail

# RELEASE_GATE_DEPENDENCY_MODE selects what the gate qualifies, as the release
# builder's dependency_mode does. pinned, the default, seals the run: clean,
# fetched, at-origin checkouts before and after the tests, the release lock,
# and the runtime source and metadata attestations. local tests the working
# checkouts as they are and skips exactly those checks and the tests that need
# a sealed run or the vault, and prints each skip with its reason first; it
# never reads a vault checkout. A local pass qualifies those checkouts, not a
# sealed release.
release_gate_requested_dependency_mode="${RELEASE_GATE_DEPENDENCY_MODE:-pinned}"
if [[ "$release_gate_requested_dependency_mode" == pinned ]]; then
  echo "[release-1.0] dependency mode pinned: sealed, clean, fetched, at-origin checkouts and the release lock"
elif [[ "$release_gate_requested_dependency_mode" == local ]]; then
  echo "[release-1.0] dependency mode local: the working checkouts as they are, not sealed"
  # Everything local mode skips, each with its reason. Each test skips by
  # name in every job that would run it.
  echo "[release-1.0] source freeze provenance skipped (local mode): it checks the working checkouts' module inventory and tidiness, not that they are clean, fetched and at origin"
  echo "[release-1.0] runtime source and metadata artifact attestations skipped (local mode): they read the published runtime source and the testnet archive node, not the working checkouts"
  echo "[release-1.0] final release-lock and source-freeze checkouts skipped (local mode): only a sealed run's checkouts pass them"
  echo "[release-1.0] vault checkout skipped (local mode): local mode never reads it"
  echo "[release-1.0] TestReleaseLockMatchesCheckout skipped (local mode): only a sealed run's checkouts match the release lock"
  echo "[release-1.0] TestRepositoryDiscoveryUsesModuleIdentity skipped (local mode): it stats the workspace vault's main/st.yml"
else
  echo 'RELEASE_GATE_DEPENDENCY_MODE must be pinned or local' >&2
  exit 1
fi
# Nested gates and their fixtures keep the pinned default.
unset RELEASE_GATE_DEPENDENCY_MODE

# Release qualification cannot hide an assertion or panic behind a retry.
export WARP_TEST_ENV_FAIL_FAST=1
# server/stmonitor/testfixture's admin URL comes only from this gate's private
# PostgreSQL, in sn-e2e, never from the caller's environment or local stack.
unset STMONITOR_TEST_ADMIN_URL
# So do the qualification exports, from sn-mainnet-exports, in the ./mainnet
# shards and server-db's native-fee tests.
unset URNETWORK_NATIVE_CAPTURE_ENGINE URNETWORK_NATIVE_EXECUTION_ENGINE URNETWORK_NATIVE_EXECUTION_FIXTURE \
  URNETWORK_NATIVE_PRODUCER_FIXTURE URNETWORK_NATIVE_CONSERVATION_FIXTURE URNETWORK_NATIVE_PRINCIPAL_FIXTURE_DIR \
  URNETWORK_NATIVE_PRINCIPAL_EFFECTS_FIXTURE_DIR URNETWORK_NATIVE_VAULT_CAPTURE_FIXTURE_DIR \
  URNETWORK_NATIVE_VAULT_CAPTURE_SEQUENCE_FIXTURE_DIR URNETWORK_NATIVE_RUNTIME_RENEWAL_FIXTURE \
  URNETWORK_NATIVE_WHOLE_FEE_FIXTURE_DIR URNETWORK_NATIVE_YUMA_FIXTURE_DIR URNETWORK_NATIVE_YUMA_CAPACITY_FIXTURE_DIR \
  URNETWORK_NATIVE_YUMA_POPULATED_FIXTURE_DIR URNETWORK_NATIVE_TREASURY_FIXTURE \
  URNETWORK_PROVIDER_ATTEMPT_COMPLETED_FIXTURE_DIR URNETWORK_PROVIDER_WORK_FIXTURE_DIR URNETWORK_ECONOMIC_FEE_FIXTURE_DIR \
  URNETWORK_NATIVE_FEE_SETTLEMENT_FIXTURE_DIRECTORY URNETWORK_NATIVE_FEE_SETTLEMENT_VERIFIER URNETWORK_NATIVE_FEE_SETTLEMENT_VERIFIER_SHA256

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace="$(dirname "$sn_repo")"
release_repos=(sn server connect sdk glog goidenticons proxy userwireguard warp gvisor vault xops config)
source "$sn_repo/scripts/test-storage.sh"
sn_test_storage_init
source "$sn_repo/scripts/release-gate-jobs.sh"
release_gate_jobs_init
# The jobs owner starts every gate pinned; only this entry selects local mode.
release_gate_dependency_mode="$release_gate_requested_dependency_mode"
export release_gate_dependency_mode

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
    release_gate_record_preflight repository-inventory release_gate_diagnostic_inventory "$release_gate_dependency_mode" || release_gate_preflight_refusal "$?"
  fi
  echo "[release-1.0] source-freeze preflight"
  release_source_snapshot="$(release_gate_record_preflight source-freeze "$sn_repo/scripts/check-release-source-freeze.sh" --dependency-mode "$release_gate_dependency_mode" "$workspace")" || release_gate_preflight_refusal "$?"
  printf '%s\n' "$release_source_snapshot"

  echo "[release-1.0] simulator source-integrity preflight"
  release_gate_record_preflight source-integrity bash -c 'set -euo pipefail; release_gate_source_integrity_preflight' || release_gate_preflight_refusal "$?"

  echo "[release-1.0] generated binding toolchain preflight"
  release_gate_record_preflight binding-toolchain "$sn_repo/stabi/generate.sh" --preflight || release_gate_preflight_refusal "$?"

  # Both attestations read the published runtime source and the testnet
  # archive node, not the working checkouts, so local mode skips them.
  if [[ "$release_gate_dependency_mode" == local ]]; then
    echo "[release-1.0] runtime source and metadata artifact attestations skipped (local mode)"
  else
    echo "[release-1.0] runtime 454 source attestation"
    release_gate_record_preflight runtime-source "$sn_repo/scripts/check-runtime-v454-source.sh" || release_gate_preflight_refusal "$?"

    echo "[release-1.0] exact runtime metadata artifact attestation"
    release_gate_record_preflight runtime-metadata "$sn_repo/scripts/check-runtime-metadata-artifacts.sh" || release_gate_preflight_refusal "$?"
  fi
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

# The qualification exports that ./mainnet's tests consume, built offline from
# these checkouts into the gate's private storage; the ./mainnet shards wait
# for this job, below. On 2026-10-14, at load 18 with four CPUs, it took seven
# minutes and peaked at 3.3 GiB.
release_phase_sn_mainnet_exports() {
  "$sn_repo/scripts/build-mainnet-exports.sh" "$release_gate_mainnet_exports"
}
release_gate_start sn-mainnet-exports release_phase_sn_mainnet_exports

release_phase_isolation_regressions() {
  cd "$sn_repo"
  go test ./scripts/server-fixture -count=1
  go test -race ./scripts/server-fixture -count=1
  go test ./scripts/test-storage -count=1
  go test -race ./scripts/test-storage -count=1
  # On 2026-10-14 the race run of the gate's own job pins took 172 seconds of
  # its former three minutes, so both modes have ten.
  go test ./sim-testnet -run '^TestReleaseGate(Child|Jobs|Isolation)' -count=1 -parallel=4 -timeout 10m
  go test -race ./sim-testnet -run '^TestReleaseGate(Child|Jobs|Isolation)' -count=1 -parallel=4 -timeout 10m
  cd "$workspace/server"
  go test . -run '^Test(ReleaseGateServices|TestEnvironmentPrivatePortable|TestEnvironmentDoesNotStartSharedHTTPProfiler)' -count=1
  go test -race . -run '^Test(ReleaseGateServices|TestEnvironmentPrivatePortable|TestEnvironmentDoesNotStartSharedHTTPProfiler)' -count=1
}
release_gate_start isolation-regressions release_phase_isolation_regressions

echo "[release-1.0] sn Go tests"
release_phase_sn_go() {
  cd "$sn_repo"
  # Pin the launch-critical synthetic EVM identity boundary explicitly in
  # addition to the aggregate suites, including the onchain transaction tool.
  synthetic_evm_identity_tests='^Test(WaitFinalized|EVMBlockIdentity|ClaimReceiptIdentity|FinalizedClaimReceipt|SyntheticEVM|EthEVMBlockReader|EVMFinality|FinalizedEVMHead|BoundFinalizedEVMHead|ReceiptRequiresCanonicalHashAndFinalizedHeight|ProducerGatePinsSyntheticEVMIdentityRegressions)'
  go test ./miner/onchain ./miner ./sim-testnet -run "$synthetic_evm_identity_tests" -count=1 -timeout 5m
  go test -race ./miner/onchain ./miner ./sim-testnet -run "$synthetic_evm_identity_tests" -count=1 -timeout 10m
  # Reach the exact same framed-input regressions explicitly in the aggregate
  # gate, including their independent source/selector guard.
  parser_framing_tests='^Test((Policy|ReleaseConfig|ClaimDaemonConfig|StrictYAML|RenderedValidatorPolicy)RejectsMalformedTrailingYAML|FinalSemanticPathProofArtifact(Count|RejectsMalformedTrailingJSON)|ParseFleetManifestStrictCanonicalRoundTrip|PolicyStrictAndFailClosed|LoadReleaseConfigStrictAndNormalizesOperatorSecrets|ClaimDaemonConfigStrictAndPortable|StrictYAMLRejectsUnknownAndMultipleDocuments|ReleaseGatesPinProviderAndTransportRegressions)$'
  go test ./protocol ./miner ./validator ./sim-testnet -run "$parser_framing_tests" -count=1 -parallel=4 -timeout 2m
  go test -race ./protocol ./miner ./validator ./sim-testnet -run "$parser_framing_tests" -count=1 -parallel=4 -timeout 2m
  # Provisioned release keys use the same strict custody as hotkeys and VPKs.
  # Keep the actual runtime's two-read identity boundary explicit in both modes.
  seed_custody_tests='^Test(Keypair|LoadOrCreateSeedFile|SeedFileFormats|SeedCustody|VpkSeed|EvmKeyLoadingAndMirror|HotkeyLoadOrCreate|IdentityCustody|ReleaseClientSeed)'
  go test ./crv4 ./validator -run "$seed_custody_tests" -count=1
  go test -race ./crv4 ./validator -run "$seed_custody_tests" -count=1
  swarm_wallet_tests='^TestSetSwarmMemberWallet'
  go test ./miner -run "$swarm_wallet_tests" -count=1
  go test -race ./miner -run "$swarm_wallet_tests" -count=1
  # The simulator's provisioned client keys retain their narrower raw32 grammar.
  simulator_seed_custody_tests='^Test(SimulatorClientSeedCustody|SimulatorOperatorPath|InspectValidatorPathProofsRequiresEveryOperatorDomain$|FinalSettlementClosureWaitHonorsPublicationAndCancellation$|FinalLifecycleIntentRequirementsKeepSettlementAndNativeClocksDistinct$)'
  go test ./sim-testnet -run "$simulator_seed_custody_tests" -count=1 -parallel=4 -timeout 3m
  # The complete serial census measured 211s with race instrumentation. Keep
  # its selection and fixture deadlines; allow headroom for the package clock.
  go test -race ./sim-testnet -run "$simulator_seed_custody_tests" -count=1 -parallel=4 -timeout 10m
  # sim-testnet contains launch-scale 1,000-miner fixtures. The package has an
  # isolated 90-minute race deadline below; do not let Go's implicit 10-minute
  # package deadline terminate the faster ordinary pass while its independent
  # durability tests are still running.
  # The complete uncached ./validator shards below, in both modes, own every
  # settlement-closure root. Do not repeat that census under an implicit 10m
  # package deadline before admitting unrelated focused checks.
  validator_lifecycle_tests='^Test(TunnelAttemptCloseJoinsPumpBeforeGenerator|TunnelAttemptCloseReleasesPartialConstruction)$'
  go test ./validator -run "$validator_lifecycle_tests" -count=1
  go test -race ./validator -run "$validator_lifecycle_tests" -count=1
  # The complete ./sim-testnet shards below, in both modes, retain every
  # launch-scale test.
}
release_gate_start sn-go release_phase_sn_go

# The E suite needs the server's database, so the aggregate jobs skip it;
# sn-e2e runs every TestSettlementV2E2E test on the gate's private services.
# So do the four mainnet roots of server/stmonitor/testfixture, skipped by
# name. Local mode also skips its two named sim-testnet tests.
# sn-all-normal runs every package but ./mainnet, ./validator and
# ./sim-testnet, and each of their shards below runs this same skip.
release_gate_aggregate_skip='^(TestSettlementV2E2E|TestMonitorOperatorCredentialFenceAndPublicationAmbiguity$|TestMonitorOperatorCommandReadsProductionJournalAndRestartsOutage$|TestMonitorStorageStoppedOperatorReleasesOwnersBeforePeerStops$|TestMonitorStorageOperatorLostAckRetainsIncident$)'
if [[ "$release_gate_dependency_mode" == local ]]; then
  release_gate_aggregate_skip='^(TestSettlementV2E2E|TestMonitorOperatorCredentialFenceAndPublicationAmbiguity$|TestMonitorOperatorCommandReadsProductionJournalAndRestartsOutage$|TestMonitorStorageStoppedOperatorReleasesOwnersBeforePeerStops$|TestMonitorStorageOperatorLostAckRetainsIncident$|TestReleaseLockMatchesCheckout$|TestRepositoryDiscoveryUsesModuleIdentity$)'
fi
export release_gate_aggregate_skip
release_phase_sn_all_normal() {
  cd "$sn_repo"
  sn_package_list="$(go list ./... | grep -Evx 'github[.]com/urfoundation/sn/(mainnet|validator|sim-testnet)')"
  [[ -n "$sn_package_list" ]]
  mapfile -t sn_packages <<<"$sn_package_list"
  go test -parallel=4 -timeout 90m "${sn_packages[@]}" -count=1 -skip "$release_gate_aggregate_skip"
}
# Measured with one job's four CPUs at load 33-85, the serial miner package
# alone ran 41 minutes under race before settlement v2 and 55 on main, mostly
# in real fsyncs of its durable claim and fleet custody; crv4 ran 8 of the
# inherited ten minutes. Allow about twice the slowest measurement.
release_phase_sn_core_race() {
  cd "$sn_repo"
  go test -race -timeout 120m ./crv4 ./miner/... ./protocol -count=1
}
# A package too large for one job's deadline runs in shards, each its own
# job. Shard i of n runs the tests of the package's own list, from go test
# -list in the shard's build mode, whose name's CRC-32 mod n is i - 1, so a
# new test joins a shard without an edit, with the aggregate skip, which names
# only tests of sn, in its job's own private TMPDIR and GOTMPDIR on the storage
# root: durable-volume custody admits only ext4, xfs and btrfs. Each sharded
# package has a named skip list, which takes tests out of every one of its
# shards; the selector refuses a name that is no test of the package's list.
#
# The -run selector of shard $2 of $3 of package $1, without the names in $4,
# for the `go test -list` output on stdin.
release_gate_shard_selector() {
  python3 -c '
import re, sys, zlib
package, shard, shards, skips = sys.argv[1], int(sys.argv[2]), int(sys.argv[3]), sys.argv[4].split()
names = []
for line in sys.stdin.read().splitlines():
    if re.fullmatch("(Test|Fuzz|Example)[A-Za-z0-9_]*", line):
        names.append(line)
    elif re.match("(Test|Fuzz|Example)", line):
        sys.exit("release gate: unexpected %s test name %r" % (package, line))
for skip in skips:
    if skip not in names:
        sys.exit("release gate: the %s skip names no test: %s" % (package, skip))
own = [name for name in names if name not in skips and zlib.crc32(name.encode()) % shards == shard - 1]
if not own:
    sys.exit("release gate: %s shard %d of %d selects no test" % (package, shard, shards))
print("^(" + "|".join(own) + ")$", end="")
' "$1" "$2" "$3" "$4"
}
# Run shard $3 of $4 of the package $1 of the current directory's module, in
# mode $2, normal or race, without the names in $5, with the go test flags that
# follow. A failed list or selection returns before any test runs, even where
# errexit does not reach it.
release_gate_shard() {
  local package="$1" mode="$2" shard="$3" shards="$4" skips="$5" selector
  local -a build=()
  shift 5
  if ! [[ "$shards" =~ ^[1-9][0-9]?$ && "$shard" =~ ^[1-9][0-9]?$ ]] || (( shard > shards )); then
    printf 'release gate: unknown %s shard %s of %s\n' "$package" "$shard" "$shards" >&2
    return 1
  fi
  if [[ "$mode" == race ]]; then
    build=(-race)
  elif [[ "$mode" != normal ]]; then
    printf 'release gate: unknown %s shard mode %s\n' "$package" "$mode" >&2
    return 1
  fi
  if [[ -n "$skips" ]]; then
    printf '[release-1.0] %s shards skip by name: %s\n' "$package" "$skips"
  fi
  selector="$(go test "${build[@]}" -list . "$package" | release_gate_shard_selector "$package" "$shard" "$shards" "$skips")" || return 1
  go test "${build[@]}" "$package" -run "$selector" -count=1 "$@" -skip "$release_gate_aggregate_skip"
}
# ./mainnet runs in its own jobs. Its tests run one at a time, bound by fsync
# and latency at about 0.46 core per second, and separate processes scale it,
# about 3.5 times at four and 5 to 6 at eight. On 2026-10-14, at load 30 to
# 58, the package's serial test time was about 27 hours (each test's median
# over that day's runs; 2,599 of 2,759 tests measured, the rest counted at the
# mean); the heaviest shard, 6, has 255 minutes of it, and each shard's
# deadline is about twice that.
release_gate_mainnet_shards=8
# Tests that no shard runs, by exact name, separated by spaces; each must be
# a test of the package's list. None yet.
release_gate_mainnet_skip_names=''
# Run shard $1 of ./mainnet, with the exports of sn-mainnet-exports, whose
# exit the gate gives every shard.
release_gate_mainnet_shard() {
  if [[ "${release_gate_mainnet_exports_exit:-}" != 0 ]]; then
    printf 'release gate: ./mainnet shard %s has no qualification exports: sn-mainnet-exports exit %s\n' "$1" "${release_gate_mainnet_exports_exit:-unknown}" >&2
    return 1
  fi
  source "$release_gate_mainnet_exports/environment.sh"
  cd "$sn_repo"
  release_gate_shard ./mainnet normal "$1" "$release_gate_mainnet_shards" "$release_gate_mainnet_skip_names" -timeout 510m
}
# ./validator runs in its own jobs, normal and race. On 2026-10-14, at load 3
# to 8 with nice 19, its two normal shards took 38 and 35 minutes, about 55 at
# the official run's pace, which was 1.45 times slower in its race run. Under
# race the package is about eight hours of test time at that pace (each test's
# time from a four-hour race run of the package where it reached the test,
# otherwise 4.5 times its normal time), in 12 shards: the heaviest projects to
# 55 minutes, and took 32 alone at load 3 to 4, 46 at the official pace. Each
# mode's deadline is about twice its heaviest shard's.
release_gate_validator_normal_shards=2
release_gate_validator_race_shards=12
# Tests that no ./validator shard runs, in either mode, by exact name,
# separated by spaces; each must be a test of the package's list. None yet.
release_gate_validator_skip_names=''
# Run normal shard $1 of ./validator.
release_gate_validator_normal_shard() {
  cd "$sn_repo"
  release_gate_shard ./validator normal "$1" "$release_gate_validator_normal_shards" "$release_gate_validator_skip_names" -parallel=4 -timeout 110m
}
# Run race shard $1 of ./validator.
release_gate_validator_race_shard() {
  cd "$sn_repo"
  release_gate_shard ./validator race "$1" "$release_gate_validator_race_shards" "$release_gate_validator_skip_names" -parallel=4 -timeout 110m
}
# ./sim-testnet runs in its own jobs, normal and race. Its race run was five
# jobs partitioned by name until the official run of 2026-10-14, at load 16 to
# 50, where the ordinary complement and the history family each used their
# whole 90 minutes, the final family took 66 and the supplements 73. From that
# day's normal run of the package at load about 20, the heaviest of five
# normal shards projects to 39 minutes. Under race, at about 4.5 times its
# normal time as those race jobs ran, the package is about 11 hours of test
# time, in 17 shards; the three with a TestFinalSemanticSupplement test, each
# of which took 73 minutes beside the other two, project to 100 to 106. Each
# mode's deadline is about twice its heaviest shard's.
release_gate_simulator_normal_shards=5
release_gate_simulator_race_shards=17
# Tests that no ./sim-testnet shard runs, in either mode, by exact name,
# separated by spaces; each must be a test of the package's list. None yet.
release_gate_simulator_skip_names=''
# Run normal shard $1 of ./sim-testnet.
release_gate_simulator_normal_shard() {
  cd "$sn_repo"
  release_gate_shard ./sim-testnet normal "$1" "$release_gate_simulator_normal_shards" "$release_gate_simulator_skip_names" -parallel=4 -timeout 80m
}
# Run race shard $1 of ./sim-testnet.
release_gate_simulator_race_shard() {
  cd "$sn_repo"
  release_gate_shard ./sim-testnet race "$1" "$release_gate_simulator_race_shards" "$release_gate_simulator_skip_names" -parallel=4 -timeout 210m
}
# connect's race run and its one reproducible shuffled ordering, with the
# same fixed seed, run in shards; its normal run stays one process, below. On
# 2026-10-14, at load 6 to 28 with nice 19, connect's 48 TestWindowPath tests
# ran about eight times slower under race than in normal mode, which makes the
# race run about ten hours, and TestWindowPathSdkProfiles alone about 140
# minutes. The heaviest of 15 shards, which holds that test, projects to 170
# minutes at the official run's pace, 1.18 times that of these measurements,
# and the next to 80; each shard's deadline is about twice the heaviest.
release_gate_connect_race_shards=15
# Tests that no connect shard runs, by exact name, separated by spaces; each
# must be a test of the package's list. None yet.
release_gate_connect_skip_names=''
# Run race shard $1 of connect.
release_gate_connect_race_shard() {
  cd "$workspace/connect"
  GOMAXPROCS=4 release_gate_shard github.com/urnetwork/connect race "$1" "$release_gate_connect_race_shards" "$release_gate_connect_skip_names" -timeout 340m
}
# Run race shard $1 of connect in the shuffled ordering.
release_gate_connect_shuffle_shard() {
  cd "$workspace/connect"
  GOMAXPROCS=4 release_gate_shard github.com/urnetwork/connect race "$1" "$release_gate_connect_race_shards" "$release_gate_connect_skip_names" -timeout 340m -shuffle=4535211000
}
export release_gate_mainnet_shards release_gate_mainnet_skip_names
export release_gate_validator_normal_shards release_gate_validator_race_shards release_gate_validator_skip_names
export release_gate_simulator_normal_shards release_gate_simulator_race_shards release_gate_simulator_skip_names
export release_gate_connect_race_shards release_gate_connect_skip_names
export -f release_gate_shard_selector release_gate_shard release_gate_mainnet_shard
export -f release_gate_validator_normal_shard release_gate_validator_race_shard release_gate_simulator_normal_shard release_gate_simulator_race_shard
export -f release_gate_connect_race_shard release_gate_connect_shuffle_shard
release_phase_sn_mainnet_1() {
  release_gate_mainnet_shard 1
}
release_phase_sn_mainnet_2() {
  release_gate_mainnet_shard 2
}
release_phase_sn_mainnet_3() {
  release_gate_mainnet_shard 3
}
release_phase_sn_mainnet_4() {
  release_gate_mainnet_shard 4
}
release_phase_sn_mainnet_5() {
  release_gate_mainnet_shard 5
}
release_phase_sn_mainnet_6() {
  release_gate_mainnet_shard 6
}
release_phase_sn_mainnet_7() {
  release_gate_mainnet_shard 7
}
release_phase_sn_mainnet_8() {
  release_gate_mainnet_shard 8
}
release_phase_sn_validator_normal_1() {
  release_gate_validator_normal_shard 1
}
release_phase_sn_validator_normal_2() {
  release_gate_validator_normal_shard 2
}
release_phase_sn_validator_race_1() {
  release_gate_validator_race_shard 1
}
release_phase_sn_validator_race_2() {
  release_gate_validator_race_shard 2
}
release_phase_sn_validator_race_3() {
  release_gate_validator_race_shard 3
}
release_phase_sn_validator_race_4() {
  release_gate_validator_race_shard 4
}
release_phase_sn_validator_race_5() {
  release_gate_validator_race_shard 5
}
release_phase_sn_validator_race_6() {
  release_gate_validator_race_shard 6
}
release_phase_sn_validator_race_7() {
  release_gate_validator_race_shard 7
}
release_phase_sn_validator_race_8() {
  release_gate_validator_race_shard 8
}
release_phase_sn_validator_race_9() {
  release_gate_validator_race_shard 9
}
release_phase_sn_validator_race_10() {
  release_gate_validator_race_shard 10
}
release_phase_sn_validator_race_11() {
  release_gate_validator_race_shard 11
}
release_phase_sn_validator_race_12() {
  release_gate_validator_race_shard 12
}
release_phase_sn_simulator_normal_1() {
  release_gate_simulator_normal_shard 1
}
release_phase_sn_simulator_normal_2() {
  release_gate_simulator_normal_shard 2
}
release_phase_sn_simulator_normal_3() {
  release_gate_simulator_normal_shard 3
}
release_phase_sn_simulator_normal_4() {
  release_gate_simulator_normal_shard 4
}
release_phase_sn_simulator_normal_5() {
  release_gate_simulator_normal_shard 5
}
release_phase_sn_simulator_race_1() {
  release_gate_simulator_race_shard 1
}
release_phase_sn_simulator_race_2() {
  release_gate_simulator_race_shard 2
}
release_phase_sn_simulator_race_3() {
  release_gate_simulator_race_shard 3
}
release_phase_sn_simulator_race_4() {
  release_gate_simulator_race_shard 4
}
release_phase_sn_simulator_race_5() {
  release_gate_simulator_race_shard 5
}
release_phase_sn_simulator_race_6() {
  release_gate_simulator_race_shard 6
}
release_phase_sn_simulator_race_7() {
  release_gate_simulator_race_shard 7
}
release_phase_sn_simulator_race_8() {
  release_gate_simulator_race_shard 8
}
release_phase_sn_simulator_race_9() {
  release_gate_simulator_race_shard 9
}
release_phase_sn_simulator_race_10() {
  release_gate_simulator_race_shard 10
}
release_phase_sn_simulator_race_11() {
  release_gate_simulator_race_shard 11
}
release_phase_sn_simulator_race_12() {
  release_gate_simulator_race_shard 12
}
release_phase_sn_simulator_race_13() {
  release_gate_simulator_race_shard 13
}
release_phase_sn_simulator_race_14() {
  release_gate_simulator_race_shard 14
}
release_phase_sn_simulator_race_15() {
  release_gate_simulator_race_shard 15
}
release_phase_sn_simulator_race_16() {
  release_gate_simulator_race_shard 16
}
release_phase_sn_simulator_race_17() {
  release_gate_simulator_race_shard 17
}
release_gate_start sn-all-normal release_phase_sn_all_normal
# The shards wait for the exports: a failed build fails every shard.
release_gate_await sn-mainnet-exports
release_gate_mainnet_exports_exit="$RELEASE_GATE_AWAITED_EXIT"
export release_gate_mainnet_exports_exit
release_gate_start sn-mainnet-1 release_phase_sn_mainnet_1
release_gate_start sn-mainnet-2 release_phase_sn_mainnet_2
release_gate_start sn-mainnet-3 release_phase_sn_mainnet_3
release_gate_start sn-mainnet-4 release_phase_sn_mainnet_4
release_gate_start sn-mainnet-5 release_phase_sn_mainnet_5
release_gate_start sn-mainnet-6 release_phase_sn_mainnet_6
release_gate_start sn-mainnet-7 release_phase_sn_mainnet_7
release_gate_start sn-mainnet-8 release_phase_sn_mainnet_8
release_gate_start sn-core-race release_phase_sn_core_race
release_gate_start sn-validator-normal-1 release_phase_sn_validator_normal_1
release_gate_start sn-validator-normal-2 release_phase_sn_validator_normal_2
release_gate_start sn-simulator-normal-1 release_phase_sn_simulator_normal_1
release_gate_start sn-simulator-normal-2 release_phase_sn_simulator_normal_2
release_gate_start sn-simulator-normal-3 release_phase_sn_simulator_normal_3
release_gate_start sn-simulator-normal-4 release_phase_sn_simulator_normal_4
release_gate_start sn-simulator-normal-5 release_phase_sn_simulator_normal_5
release_gate_start sn-validator-race-1 release_phase_sn_validator_race_1
release_gate_start sn-validator-race-2 release_phase_sn_validator_race_2
release_gate_start sn-validator-race-3 release_phase_sn_validator_race_3
release_gate_start sn-validator-race-4 release_phase_sn_validator_race_4
release_gate_start sn-validator-race-5 release_phase_sn_validator_race_5
release_gate_start sn-validator-race-6 release_phase_sn_validator_race_6
release_gate_start sn-validator-race-7 release_phase_sn_validator_race_7
release_gate_start sn-validator-race-8 release_phase_sn_validator_race_8
release_gate_start sn-validator-race-9 release_phase_sn_validator_race_9
release_gate_start sn-validator-race-10 release_phase_sn_validator_race_10
release_gate_start sn-validator-race-11 release_phase_sn_validator_race_11
release_gate_start sn-validator-race-12 release_phase_sn_validator_race_12
release_gate_start sn-simulator-race-1 release_phase_sn_simulator_race_1
release_gate_start sn-simulator-race-2 release_phase_sn_simulator_race_2
release_gate_start sn-simulator-race-3 release_phase_sn_simulator_race_3
release_gate_start sn-simulator-race-4 release_phase_sn_simulator_race_4
release_gate_start sn-simulator-race-5 release_phase_sn_simulator_race_5
release_gate_start sn-simulator-race-6 release_phase_sn_simulator_race_6
release_gate_start sn-simulator-race-7 release_phase_sn_simulator_race_7
release_gate_start sn-simulator-race-8 release_phase_sn_simulator_race_8
release_gate_start sn-simulator-race-9 release_phase_sn_simulator_race_9
release_gate_start sn-simulator-race-10 release_phase_sn_simulator_race_10
release_gate_start sn-simulator-race-11 release_phase_sn_simulator_race_11
release_gate_start sn-simulator-race-12 release_phase_sn_simulator_race_12
release_gate_start sn-simulator-race-13 release_phase_sn_simulator_race_13
release_gate_start sn-simulator-race-14 release_phase_sn_simulator_race_14
release_gate_start sn-simulator-race-15 release_phase_sn_simulator_race_15
release_gate_start sn-simulator-race-16 release_phase_sn_simulator_race_16
release_gate_start sn-simulator-race-17 release_phase_sn_simulator_race_17

# Settlement v2 (VERSION2-FINAL.md §14 gate 3, §15): the ledger package (GL),
# the sum tree (GT) and allocation (GA) additions, the claim daemon (E-5),
# snclaim (CL) and the validator's v2 code (VA), its ledger audit and custody
# included, in both modes, with gate 1's coverage checker. The aggregate jobs above run them too; this job is gate
# 3's own verdict. The claim daemon's durable volumes need the umask 022 and
# private TMPDIR that every phase gets.
release_phase_sn_v2() {
  cd "$sn_repo"
  go test ./ledger ./scripts/solidity-coverage -count=1
  go test -race ./ledger ./scripts/solidity-coverage -count=1
  # At load 27 to 46 the selection's ./miner, its claim v2 tests, took 592
  # seconds under race and 566 without, and a later race run timed out at
  # Go's implicit ten minutes, so both modes have an explicit deadline; the
  # whole ledger and checker packages above take seconds.
  settlement_v2_tests='^Test(Ledger|ClaimV2|MainUsageClaimDaemonV2|V2|Withdraw|SubmitCarriesValue|SettlementV2|ProductionSettlementV2|ProductionRenderedSuccessorKeepsSettlementV2|ReleaseRuntimeRunsLedgerAudit)'
  go test ./merkle ./protocol ./miner ./miner/onchain ./validator -run "$settlement_v2_tests" -count=1 -timeout 30m
  go test -race ./merkle ./protocol ./miner ./miner/onchain ./validator -run "$settlement_v2_tests" -count=1 -timeout 30m
}
release_gate_start sn-v2 release_phase_sn_v2

# GL-10 (VERSION2-FINAL.md §15): the scale-tagged tests, which ./... never
# compiles, build and verify ledgers of a million providers, assert the commit
# and challenge windows' budgets, and log time, peak memory and bytes fetched.
# On 2026-10-11 the package took 170 seconds on four CPUs and peaked at 2.7 GiB.
# The server's scale-tagged tests (SV-6) are this shard's database half, below.
# The runtime rehearsal (activation gate 2, runtime_rehearsal tag) needs a node
# built from runtime 479's source, so it runs beside this gate, on the same
# checkout (scripts/runtime-rehearsal/README.md); R-1, the v2 contract phase,
# runs from ./mainnet with the same tag:
#   scripts/runtime-rehearsal/build.sh /path/to/subtensor-checkout
#   RUNTIME_REHEARSAL_NODE=/path/to/target/release/node-subtensor \
#     go test -tags runtime_rehearsal ./rehearsal/ -v -timeout 60m
#   RUNTIME_REHEARSAL_NODE=/path/to/target/release/node-subtensor \
#     go test -tags runtime_rehearsal ./mainnet/ -run TestR1SettlementV2Phase -v
release_phase_sn_scale() {
  cd "$sn_repo"
  go test -tags scale -v -timeout 90m ./ledger -count=1
}
release_gate_start sn-scale release_phase_sn_scale

echo "[release-1.0] deployable Solidity static analysis"
release_phase_solidity_static() {
  "$sn_repo/scripts/test-solidity-static.sh"
}
release_gate_start solidity-static release_phase_solidity_static

echo "[release-1.0] Solidity format, build, and tests"
release_phase_solidity() {
  cd "$sn_repo/evm"
  forge fmt --check
  # Gate 1 (VERSION2-FINAL.md §14): this release build fails on a contract over
  # a size limit, and its table of sizes and margins stays in this job's log.
  forge build --deny warnings --sizes
  forge test --summary
  # I-1 to I-6 (VERSION2-FINAL.md §15.5): the plain suite ran the settlement v2
  # lifecycle campaign short; its own profile runs it deep, with metrics, on
  # the same build.
  FOUNDRY_PROFILE=v2_invariant forge test --match-path test/V2Invariant.t.sol

  echo "[release-1.0] generated contract payload and storage-layout lock"
  cd "$sn_repo"
  # Solidity's IPFS metadata digest includes Foundry's compilation graph. A
  # target-only Slither build and a clean full-project build can therefore
  # produce different digests with byte-for-byte identical executable code.
  # Preserve the exact release-locked/live payload while rejecting every
  # difference outside that one structurally validated 32-byte field.
  go run ./sim-testnet/gencontracts --check "$FOUNDRY_OUT" sim-testnet/contracts_gen.go
  echo "[release-1.0] generated ABI and Go binding freshness"
  "$sn_repo/stabi/generate.sh" --check --artifacts "$FOUNDRY_OUT"
}
release_gate_start solidity release_phase_solidity

echo "[release-1.0] settlement v2 Solidity coverage"
# Activation gate 1 (VERSION2-FINAL.md §14, §15) pins this coverage command:
# the default mode stops on a stack-too-deep in STCoordinator and
# --ir-minimum fails on the v1 evidence library. Its build stays in this job's
# private directory; contract sizes come from the release build above. Forge
# writes the report even when a test fails, so the settlement v2 bar is checked
# either way, and a failing test still fails this phase.
release_phase_solidity_coverage() {
  cd "$sn_repo/evm"
  coverage_status=0
  FOUNDRY_OUT="$TMPDIR/coverage-out" FOUNDRY_CACHE_PATH="$TMPDIR/coverage-cache" forge coverage --ir-minimum --report lcov --skip STValidatorEvidence.sol --skip ValidatorEvidenceStorage.t.sol --skip ValidatorEvidenceActivation.t.sol --report-file "$TMPDIR/lcov.info" || coverage_status=$?
  cd "$sn_repo"
  go run ./scripts/solidity-coverage evm "$TMPDIR/lcov.info"
  return "$coverage_status"
}
release_gate_start solidity-coverage release_phase_solidity_coverage

echo "[release-1.0] operator pure/unit suites"
release_phase_server_unit() {
  cd "$workspace/server"
  test_env_fail_fast_tests='^Test(DefaultTestEnvReleaseFailFast|RunRetriesUntilPass|RunFailsAfterExhaustion|RunReportsPanicOriginAfterExhaustion)'
  go test . -run "$test_env_fail_fast_tests" -count=1
  go test -race . -run "$test_env_fail_fast_tests" -count=1
  go test . -run '^Test(PgResourcesRedirectMaintenancePoolAndRestore|DatabaseTimeMatchesPostgresPrecision)$' -count=1
  go test ./st ./startifact -count=1
  # The SN25 go-live tooling: its gas params, verify.yml and st.yml checks.
  go test ./local/sn25golive -count=1
  go test ./controller -run '^Test(CoreStClient(BlockHashes|FinalizedHead|Epoch)|CoreStClientBindingsAt|DecodeStRPCBlockIdentity|StatsAlphaPriceURLIsMainnetOnly|StatsGaugeVecReplaceDeletesStaleSeries|StConfig|StCompute|StBuild|StDeposit(ArtifactUsagePinsSignerIdentityAndFinalizedBoundaries|AttemptRejectsChangedAuthorityWithoutRewritingIntent|Custody(BindsTwoOperatorSecretsAtCanonicalBoundary|RejectsDustAndKeepsPrincipalCeilings|RejectsSharedRoleKeysBeforeRpc|RejectsWrongOperatorGraphAndAuthority)|DeadlinePinsIntendedEpoch|Funding(KeepsExactPrincipalAndBoundedRounding|RejectsInvalidAndOverflowAmounts)|RetainedCalldataRejectsChangedRetryAmount|SizeRao|SizingZeroPriceSendsNothing|TierForConvictionUsesPreEpochTierExactly)$|StEstimate|StReplacementFee$|StDecode|StEvent|StBroadcast|StClientStub|StTransactionCancellation|VerifyEvidenceRange|VerifyKeyRotation|VerifySyntheticSeedId|VerifyUsesUrForwardedAddress|VerifyIgnoresLegacyForwardedAddress|VerifyClampM|VerifyCachedResponseRoundTrip|VerifySeedRejectsMissingSignature|StripeReconcileCredentialsRequireNonblankAPIToken|AppleReconcileCredentialsRequireCompleteServerAPIIdentity|PlayReconcileCredentialsRequireOAuthPackageAndSKUs|SolanaReconcileCredentialsRequireNonblankHeliusAPIKey)' -count=1
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
  # Keep every file-backed validator filter regression in the aggregate gate,
  # while excluding the two stateful cases until the isolated DB profile below.
  filter_pure_tests='^TestVerifySimulationAssignmentFilter(IsValidatorLocalAndFailClosed|RejectsAmbiguousFiles|V[12].*|Rejects(Leaf|Parent)Symlink|AtomicReplacementPinsOpenedDescriptor)$'
  go test ./controller -run "$filter_pure_tests" -count=1
  go test -race ./controller -run "$filter_pure_tests" -count=1
  go test ./session -run 'Test.*(UrForwardedAddress|LegacyForwardedHeaders|RemoteAddress)' -count=1
  go test ./router -run 'TestTrie' -count=1
  go test ./model -run '^Test(VerifyEgressExactIndexAndPrefixScoreAreIndependent|StTransactionAdvisoryLockKeyUsesEthereumNonceScope|StHeadBoundCkeysFromEvents(WindowOverlap|UnbindDodge)|ParseHeadEventCkey)$' -count=1
  go test ./taskworker/work -run '^TestStSettlementTasksRejectStaleCoordinatorPayloads$' -count=1
  # The separate calibration harness and its archived inputs are outside SN
  # finalization. Keep a nonempty executable server package census.
  server_package_list="$(go list ./... | grep -Ev '^github[.]com/urnetwork/server/connect/sim-latency(/|$)')"
  [[ -n "$server_package_list" ]]
  mapfile -t server_packages <<<"$server_package_list"
  go test "${server_packages[@]}" -run '^$'
}
release_gate_start server-unit release_phase_server_unit

echo "[release-1.0] shared verify wire and public SDK suites"
release_phase_connect() {
  cd "$workspace/connect"
  client_key_registration_tests='^Test(ClientKeyRegistration|ApiOutOfBandControl|ControlSyncOob|ClientKeyManager|VerifyClientKeySignature|CanonicalCertChainBytes|ClientCloseAndWaitJoinsClientKeyPublisher)'
  go test . -run "$client_key_registration_tests" -count=1
  go test -race . -run "$client_key_registration_tests" -count=1
  go test ./... -run '^$'
  go test . -run '^Test(Verify|Sn)' -count=1

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

}
release_gate_start connect release_phase_connect

# The normal run remains one unsharded process, so package-global residue is
# still observable across every test. On 2026-10-14, at load 12 to 18 with
# nice 19, it took 100 minutes, 68 of them in its 48 TestWindowPath tests; the
# official run's was 1.18 times slower, so its deadline is about twice 118.
release_phase_connect_all_normal() {
  cd "$workspace/connect"
  go test -count=1 -timeout 240m .
}
release_gate_start connect-all-normal release_phase_connect_all_normal
release_phase_connect_race_1() {
  release_gate_connect_race_shard 1
}
release_phase_connect_race_2() {
  release_gate_connect_race_shard 2
}
release_phase_connect_race_3() {
  release_gate_connect_race_shard 3
}
release_phase_connect_race_4() {
  release_gate_connect_race_shard 4
}
release_phase_connect_race_5() {
  release_gate_connect_race_shard 5
}
release_phase_connect_race_6() {
  release_gate_connect_race_shard 6
}
release_phase_connect_race_7() {
  release_gate_connect_race_shard 7
}
release_phase_connect_race_8() {
  release_gate_connect_race_shard 8
}
release_phase_connect_race_9() {
  release_gate_connect_race_shard 9
}
release_phase_connect_race_10() {
  release_gate_connect_race_shard 10
}
release_phase_connect_race_11() {
  release_gate_connect_race_shard 11
}
release_phase_connect_race_12() {
  release_gate_connect_race_shard 12
}
release_phase_connect_race_13() {
  release_gate_connect_race_shard 13
}
release_phase_connect_race_14() {
  release_gate_connect_race_shard 14
}
release_phase_connect_race_15() {
  release_gate_connect_race_shard 15
}
release_phase_connect_shuffle_1() {
  release_gate_connect_shuffle_shard 1
}
release_phase_connect_shuffle_2() {
  release_gate_connect_shuffle_shard 2
}
release_phase_connect_shuffle_3() {
  release_gate_connect_shuffle_shard 3
}
release_phase_connect_shuffle_4() {
  release_gate_connect_shuffle_shard 4
}
release_phase_connect_shuffle_5() {
  release_gate_connect_shuffle_shard 5
}
release_phase_connect_shuffle_6() {
  release_gate_connect_shuffle_shard 6
}
release_phase_connect_shuffle_7() {
  release_gate_connect_shuffle_shard 7
}
release_phase_connect_shuffle_8() {
  release_gate_connect_shuffle_shard 8
}
release_phase_connect_shuffle_9() {
  release_gate_connect_shuffle_shard 9
}
release_phase_connect_shuffle_10() {
  release_gate_connect_shuffle_shard 10
}
release_phase_connect_shuffle_11() {
  release_gate_connect_shuffle_shard 11
}
release_phase_connect_shuffle_12() {
  release_gate_connect_shuffle_shard 12
}
release_phase_connect_shuffle_13() {
  release_gate_connect_shuffle_shard 13
}
release_phase_connect_shuffle_14() {
  release_gate_connect_shuffle_shard 14
}
release_phase_connect_shuffle_15() {
  release_gate_connect_shuffle_shard 15
}
release_gate_start connect-race-1 release_phase_connect_race_1
release_gate_start connect-race-2 release_phase_connect_race_2
release_gate_start connect-race-3 release_phase_connect_race_3
release_gate_start connect-race-4 release_phase_connect_race_4
release_gate_start connect-race-5 release_phase_connect_race_5
release_gate_start connect-race-6 release_phase_connect_race_6
release_gate_start connect-race-7 release_phase_connect_race_7
release_gate_start connect-race-8 release_phase_connect_race_8
release_gate_start connect-race-9 release_phase_connect_race_9
release_gate_start connect-race-10 release_phase_connect_race_10
release_gate_start connect-race-11 release_phase_connect_race_11
release_gate_start connect-race-12 release_phase_connect_race_12
release_gate_start connect-race-13 release_phase_connect_race_13
release_gate_start connect-race-14 release_phase_connect_race_14
release_gate_start connect-race-15 release_phase_connect_race_15
release_gate_start connect-shuffle-1 release_phase_connect_shuffle_1
release_gate_start connect-shuffle-2 release_phase_connect_shuffle_2
release_gate_start connect-shuffle-3 release_phase_connect_shuffle_3
release_gate_start connect-shuffle-4 release_phase_connect_shuffle_4
release_gate_start connect-shuffle-5 release_phase_connect_shuffle_5
release_gate_start connect-shuffle-6 release_phase_connect_shuffle_6
release_gate_start connect-shuffle-7 release_phase_connect_shuffle_7
release_gate_start connect-shuffle-8 release_phase_connect_shuffle_8
release_gate_start connect-shuffle-9 release_phase_connect_shuffle_9
release_gate_start connect-shuffle-10 release_phase_connect_shuffle_10
release_gate_start connect-shuffle-11 release_phase_connect_shuffle_11
release_gate_start connect-shuffle-12 release_phase_connect_shuffle_12
release_gate_start connect-shuffle-13 release_phase_connect_shuffle_13
release_gate_start connect-shuffle-14 release_phase_connect_shuffle_14
release_gate_start connect-shuffle-15 release_phase_connect_shuffle_15
release_phase_sdk() {
  cd "$workspace/sdk"
  provider_registration_tests='^Test(DeviceLocalProviderRegistration|DeviceLocalProviderConnected)'
  go test . -run "$provider_registration_tests" -count=1
  go test -race . -run "$provider_registration_tests" -count=1
  go test ./... -run '^$'
  go test . -run '^Test(ApiSubnet|ProviderLocalUserNatSettings)' -count=1
  token_transport_tests='^Test(ApiTokenManager|DeviceRemoteRpcPublicationWakesOnlyOutstandingRefresh|ApiCloseAndWaitJoinsRefreshWorker|DeviceLocalAppliesApiRefreshAndLogout|DeviceRemoteAppliesStandaloneApiRefreshAndLogout)'
  go test . -run "$token_transport_tests" -count=1
  go test -race . -run "$token_transport_tests" -count=1
  sdk_lifecycle_tests='^Test(ApiCloseAndWaitJoinsRefreshWorker|NetworkSpaceCloseJoinsClientStrategyRelease|NetworkSpaceManagerReplacementDoesNotHoldStateLockDuringClose|NetworkSpaceManagerStaleRemovePreservesReplacement|NetworkSpaceManagerStaleActiveSelectionPreservesReplacement|NetworkSpaceCloseJoinsAsyncLocalStateAndRejectsLateJob|NetworkSpaceManagerCloseRejectsRacingUpdate|SimProviderDisconnectJoinsPendingTransportDial|SimProviderCloseJoinsPendingTransportDial|DeviceLocalProviderCloseAndWaitJoinsAdmittedMigration|DeviceLocalCloseAndWaitJoinsOwnedApiRefresh|DeviceLocalCloseAndWaitJoinsDestinationGeneration|SecurityPolicyMonitorCloseAndWaitJoinsRun|DeviceLocalRpcManagerCloseAndWaitJoinsAccept|DeviceRemoteRpcCloseAndWaitJoinsAdmittedCallback|DeviceRemoteCloseAndWaitJoinsBlockedDial|DeviceRemoteSetRpcServerRejectsAfterClose|RpcClientCallParentCancellationClosesTransport|DeviceLocalAppliesApiRefreshAndLogout|DeviceRemoteAppliesStandaloneApiRefreshAndLogout|ApiSubnetHeadlessBindings|ApiHeadlessAuthAndProviderBindings|ApiSubnetPoolClaimEscapesNoUserInputIntoQuery|SubscriptionBalanceDecode|CheckBalanceCode|CheckBalanceCodeUnknownCode|RedeemBalanceCodeDecodeAndClassify|Stripe.*|VerifyPlayPurchase|VerifyAppleTransaction)$'
  go test . -run "$sdk_lifecycle_tests" -count=1
  go test -race . -run "$sdk_lifecycle_tests" -count=1
  # CL-2 and CL-4 (VERSION2-FINAL.md §11, §15.7): the settlement v2 balances
  # the apps show, in every account state and beside the v1 claims, and the
  # v2 claims with their vault2 bindings and ledger proofs.
  sdk_ledger_v2_tests='^Test(SnLedger|BuildTestLedgerTreeMatchesTheVectors|SnChainSettingsVault2Address|Ledger|PackLedger|DecodeLedger|EthCallFrom|TransactionReceiptLogs)'
  go test . ./sn/evm ./sn/merkle -run "$sdk_ledger_v2_tests" -count=1
  go test -race . ./sn/evm ./sn/merkle -run "$sdk_ledger_v2_tests" -count=1
  # The SDK's contracts with the sibling checkouts, which these tests read:
  # every api url it builds and its admin route classes against ../server's
  # route tables, each of its modules' TLS and DTLS fingerprint dependencies
  # against ../connect's, and license.yml against its modules.
  sdk_sibling_contract_tests='^Test(SdkApiUrlsMatchServerRoutes|ApiAdminRoutesMatchTheServer|SdkModulesKeepConnectFingerprintDependencies|LicenseYmlUpToDate)$'
  go test . -run "$sdk_sibling_contract_tests" -count=1
  go test -race . -run "$sdk_sibling_contract_tests" -count=1
}
release_gate_start sdk release_phase_sdk
release_phase_sdk_build() {
  cd "$workspace/sdk/build"
  # The mobile build is a separate Go module, so the SDK-root ./... pattern
  # cannot exercise its fail-closed gomobile export policy.
  go test ./cmd/mobileexports -count=1
  go test -race ./cmd/mobileexports -count=1
  # So is the JavaScript SDK, whose committed TypeScript must be what its
  # generators make: the api client from connect's OpenAPI spec of record,
  # the types from the Go sdk, and lag.dev's tables and vectors from the sdk
  # and connect. Each generator is a go run program of sdk/js, and -check
  # compares without writing.
  cd "$workspace/sdk/js"
  go run gen_openapi.go -spec "$workspace/connect/api/bringyour.yml" -check
  go run gen_types.go -check
  go run gen_lag.go -check
}
release_gate_start sdk-build release_phase_sdk_build

echo "[release-1.0] operator Connect ingress and owned-session lifecycle regressions"
release_phase_server_connect() {
  cd "$workspace/server"
  direct_h3_tests='^TestRun(SettingsDirectH3LoopbackBypassesProxyProtocol|RouterRetainsDirectH3LoopbackSettings|RouterDirectH3LoopbackCompletesHandshake|DirectH3LoopbackModeRejectsNonLoopbackListener)$'
  go test ./connect -run "$direct_h3_tests" -count=1
  go test -race ./connect -run "$direct_h3_tests" -count=1
  go test ./connect -run '^TestConnectionVerifyEgressDisabledAvoidsVerifySettings$' -count=1
  go test -race ./connect -run '^TestConnectionVerifyEgressDisabledAvoidsVerifySettings$' -count=1
  # A QUIC source is charged only after its handshake proves its address, and
  # unvalidated handshakes are capped; the authorization lease attributes only
  # its admission check to the handshake. The listener and alt-front cases use
  # the private services below.
  h3_admission_tests='^Test(ConnectQuicHandshakeLimitCountsUnvalidatedHandshakes|ConnectHandlerBuildsUnvalidatedHandshakeLimitFromSettings)$'
  go test ./connect -run "$h3_admission_tests" -count=1
  go test -race ./connect -run "$h3_admission_tests" -count=1
  go test ./session -run '^TestAuthorizationLeaseAttributesOnlyItsAdmissionCheck$' -count=1
  go test -race ./session -run '^TestAuthorizationLeaseAttributesOnlyItsAdmissionCheck$' -count=1
  proxy_lifecycle_tests='^Test(ProxyDeviceManagerSharesOneNetworkSpaceLifetime|ProxyDeviceManagerCloseAndWaitJoinsOwnedNetworkSpace|ProxyDeviceManagerCloseJoinsAdmittedOpenAndRejectsLateOpen|ProxyDeviceManagerPreservesInjectedNetworkSpace|WgClientStackCloseAndWaitJoinsTCPDispatcher)$'
  go test ./proxy -run "$proxy_lifecycle_tests" -count=1
  go test -race ./proxy -run "$proxy_lifecycle_tests" -count=1
}
release_gate_start server-connect release_phase_server_connect

if [[ "${RUN_SERVER_DB_TESTS:-0}" == "1" ]]; then
  echo "[release-1.0] operator PostgreSQL/Redis integration suites"
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
  # The server's native-fee settlement tests: its denomination policy and
  # schema, stnativefees, the controller and the durable model. The public
  # ones settle the fixtures that sn-mainnet-exports exports, with the
  # mainnet.test image that exported them, which they pin by path and
  # SHA-256. Like a ./mainnet shard, they need that job's exit zero and run
  # with its environment, last in server-db.
  release_gate_server_native_fee() {
    if [[ "${release_gate_mainnet_exports_exit:-}" != 0 ]]; then
      printf 'release gate: server-db has no native-fee exports: sn-mainnet-exports exit %s\n' "${release_gate_mainnet_exports_exit:-unknown}" >&2
      return 1
    fi
    source "$release_gate_mainnet_exports/environment.sh"
    cd "$workspace/server"
    native_fee_settlement_tests='^Test(StNativeFee|NativeFeeCli|NativeFeeController)'
    go test -timeout 40m . ./cli/stnativefees ./controller ./model -run "$native_fee_settlement_tests" -count=1
    go test -race -timeout 40m . ./cli/stnativefees ./controller ./model -run "$native_fee_settlement_tests" -count=1
  }
  export -f release_gate_server_native_fee
  release_phase_server_db() {
    cd "$workspace/server"
    # Tests create and drop only this gate's private disposable databases.
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
    evidence_source_tests='^Test(StAttempt|StReserved|StClientKeyHistory|StClientKeyPublication|StClientKeyRegistrationCohort|StClientKeyRegistrationReadiness|SnAttempt|SnReserved|SnClientKey|MigrationCatalog|PublishedMigration|MainVersion650MigratesThroughClientKeyAppend|TransferContractOpenPlanRepairMigrationOrder|ApplyDbMigrations|ContractResultErrorSeparatesReliabilityFromAccountFailures)'
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
    controller_db_tests='^Test(CreateContractRejectsInactiveClient|VerifyController(FullTrailFlow|PoisonAndFailurePaths|ConcurrentExtendReloadsAfterLock|ReplayCannotReadANewerCachedResponse)|VerifySimulationAssignmentFilter(BlocksSeedPendingAndFutureAssignments|DoesNotAffectAnotherValidator)|AuthNetworkClientFeedsConfiguredProxyEgressNamespace|PaymentReconcile(SkipsStripeWithSKUOnlyVault|MalformedCredentialResourcesSkipAllStores)|StAccountReconcile|StSyncChainEventsBatchesCanonicalEventBlocks|StSyncChainEventsRejectsIncompleteCanonicalBatchBeforeMutation|StHeadExcludedClients(EqualV1|Refusals|Configured)|CoreStClientFleetBoundEvents|StFleetBoundTopicIsTheCoordinatorEvent|StDepositCustody(ReconcilesPreparedFundingBeforeNewSizing|StagesRoundingOnceAndCreditsExactPrincipal|LostFundingReplyDoesNotRepeatTransfer|RetainsUnderfundedSuccessWithoutAnotherTransfer)|StReplacementWaitReadErrorCannotAuthorizeReplacement)'
    model_db_tests='Test(FindActiveClientNetwork|StreamHopListenerPrunesInactiveAdjacentClients|ActiveStreamHopsBoundsConcurrentStaleReAdd|ForceCloseRequiresPositiveParallelism|ForceCloseDisputedContract|ForceCloseDirectSettlementRemovesStream|ForceCloseMalformedContractRemovesStreamAndReturnsError|SweepOrphanClearsProxyConfigRedis|SweepOrphanReapsProxyClients|VerifyEgressIndexStoresNoRawIp|VerifyTrailLockMutualExclusion|VerifyTrailLockStaleReleasePreservesSuccessor|SweepExpiredVerifyTrails|VerifyTrailMutationLockTtlCoversLoadedTrail|StDeploymentStateIsIsolatedAcrossCoordinatorReplacements|StTransactionIntentReservationUsesChainAccountNonceScope|StTransactionRevertRetryCreatesOneImmutableSuccessor|StTransactionAttemptCandidatesConvergeOnOneWinner|StTransactionCancellationCannotRegress|StTransactionFinalizedAttemptCannotRegress|St(Retained|Roster)WalletResolutions(EqualV1|Refusals)|StWalletResolutionsVerifyEachHeadOnce|StPayoutWalletResolutionsBatched(EqualV1|KeepV1PrefixOnMismatch|Duplicates|Refusals)|StFleetBound(NextBlock|ClientIds|ClientIdsRefuseMalformedRows)|StRosterProviderPinRefusals)'
    go test ./controller -run "$controller_db_tests" -count=1
    go test ./model -run "$model_db_tests" -count=1
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
    go test -race ./controller -run "$controller_db_tests" -count=1
    go test -race ./model -run "$model_db_tests" -count=1
    go test -race ./taskworker -count=1
    go test ./connect -run '^TestConnectionVerifyEgressUsesControllerHashNamespace$' -count=1
    go test -race ./connect -run '^TestConnectionVerifyEgressUsesControllerHashNamespace$' -count=1
    # Spoofed QUIC sources on the direct H3 listener and the alt front spend no
    # per-source limit and hold a bounded count of handshakes; a validated
    # source is admitted as before, and every handshake state query is
    # attributed.
    h3_admission_db_tests='^Test(ConnectQuicSpoofedSourceSpendsNoRateLimit|ConnectQuicValidatedSourceIsChargedAsBefore|ConnectH3ListenerBoundsUnvalidatedHandshakeFlood|ConnectStateQueryMetricActualHandshakes)$'
    go test ./connect -run "$h3_admission_db_tests" -count=1
    go test -race ./connect -run "$h3_admission_db_tests" -count=1
    alt_admission_tests='^TestAlt(SpoofedSourceTakesNoConnectionSlot|ServesAnEarlyApiRequestOnlyAfterTheHandshake|BoundsUnvalidatedHandshakeFlood)$'
    go test ./alt -run "$alt_admission_tests" -count=1
    go test -race ./alt -run "$alt_admission_tests" -count=1
    go test . -run '^TestIncrementRateLimitWindowResetsAtExactBoundary$' -count=1
    go test -race . -run '^TestIncrementRateLimitWindowResetsAtExactBoundary$' -count=1
    # Settlement v2 (VERSION2-FINAL.md §8, §15.7): config, migration 821, the
    # mirror, records, objects, the ledger builder and its epoch commands, the
    # epoch driver, the ledger endpoints, the v2 earnings readers, the SN25
    # prune alert, the v1 closes' and the deposit key's gas floors and liability
    # cap (SV-4), and the deposit key's expired reserved deposits (SV-1), mostly
    # on this gate's private services. Under load the race run's ./controller
    # reached Go's implicit ten-minute deadline, at 600.6 and 601.6 seconds,
    # and normal mode took 509 to 521, so both modes have an explicit one.
    ledger_v2_tests='^Test(StLedgerV2|LedgerV2|LedgerObject|StConfigSelectsLedgerV2|StConfigRefusesMalformedLedgerV2|StSyncEventsMirrorsLedgerV2|StV1DriverStopsAtFirstV2Epoch|StSyncChainKeepsV1BelowFirstV2Epoch|WorkloadsRunLedgerV2|SnLedger|StSubtensorStorageKeys|StConfigSelectsPruneAlertPositions|StV1FinalizationOfAV2EpochNotifiesNothing|StEpochEarningsKeepV1ForV1Epochs|EpochEarningsTemplateWithAV2Amount|RouterInstallsTheLedgerBuilder|StV2EpochCommands|ProviderWalletEpochRetainedRefusesUnusableSubnet|StV1Close|StV1GasFloor|StV1Deposit|StV1PendingReservation|StOperatorGasRemainingLiability|StOperatorGasPending|StOperatorGasRevertOfAPendingExpiredDeposit|StDepositReservedAttemptAdmitsOnlyAnExpiredDeposit)'
    go test . ./api ./bringyourctl ./controller ./model ./startifact ./taskworker ./taskworker/work -run "$ledger_v2_tests" -count=1 -timeout 30m
    go test -race . ./api ./bringyourctl ./controller ./model ./startifact ./taskworker ./taskworker/work -run "$ledger_v2_tests" -count=1 -timeout 30m
    # The monitor's schema contracts of the published migration suffix, from
    # 794 on, where settlement v2's migrations 821 and 822 are appended, read a
    # real database's catalog.
    migration_suffix_tests='^Test(MigrationsCurrentSuffix)'
    go test ./monitor -run "$migration_suffix_tests" -count=1
    go test -race ./monitor -run "$migration_suffix_tests" -count=1
    # Run the complete operator proxy surface. Its real network/DB roots have a
    # measured lower bound above Go's implicit ten-minute package deadline, so
    # the explicit deadline supplies deterministic headroom without changing
    # selection. Every hosted-device and client netstack is synchronously joined
    # before the following root is admitted. The package took 438 to 492
    # seconds at normal load, and 1,008 at load 60 in a pre-check, 84% of its
    # former 20 minutes, so its deadline is more than twice the slowest.
    go test -timeout 40m ./proxy -count=1
    # The monitor's whole package, in both modes, one after the other. Its SQL
    # fixtures fail closed without the test environment this phase sources
    # (server monitor/RUN-MAIN.md), so it cannot run among the unit suites.
    # On 2026-10-14, on a build host at load 40 to 58, the normal package took
    # 43 minutes. Most of it is PostgreSQL time, which the race detector does
    # not slow. Each catalog fixture's budget ends 30 seconds before the
    # package deadline, so these deadlines bound the fixtures too.
    go test -timeout 90m ./monitor -count=1
    go test -race -timeout 180m ./monitor -count=1
    release_gate_server_native_fee
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
  # Settlement v2 scale (VERSION2-FINAL.md §15, SV-6): bulk wallet selection,
  # batched resolutions, bounded head exclusion and the ledger builder at 100k
  # and 1M providers, behind the scale build tag, each inside its share of the
  # one-hour build. It is the half of the scale shard that needs the gate's
  # private services. On 2026-10-11 the wallet and head-exclusion tests took 11
  # minutes and peaked at 1.3 GiB, and the builder's took 18 minutes and
  # peaked at 2.4 GiB.
  release_phase_server_scale() {
    cd "$workspace/server"
    export WARP_ENV=local
    export WARP_SERVICE=test
    export WARP_DOMAIN=bringyour.com
    export WARP_BLOCK=test
    export WARP_VERSION=0.0.0
    source "$RELEASE_GATE_SERVICE_ENV"
    source "$workspace/server/test-env.sh"
    test_env_validate_suite_resource_manifest "$TEST_ENV_SUITE_RESOURCE_MANIFEST" "$WARP_VAULT_HOME" "$WARP_CONFIG_HOME"
    go test -tags scale ./controller ./model -run '^TestStV2Scale' -count=1 -p 1 -v -timeout 180m
  }
  # E-1 to E-7 (VERSION2-FINAL.md §14 gate 4, §15.6): settlement v2 end to end
  # in geth, from the reviewed catalogs to the operator server, the validator,
  # the claim daemon and snclaim, in both modes. E-1 and E-3 run the server's
  # driver and builder on this gate's private services, so the suite runs in
  # the server-database shard. It builds its test coordinator upgrade in a
  # private directory, never reading the Solidity job's build; the daemon's
  # durable volumes need the umask 022 and private TMPDIR every phase gets.
  # On 2026-10-12 an idle host ran the package in 219 seconds and under race
  # in 401, so an explicit deadline leaves a loaded gate headroom.
  release_phase_sn_e2e() {
    cd "$workspace/server"
    export WARP_ENV=local
    export WARP_SERVICE=test
    export WARP_DOMAIN=bringyour.com
    export WARP_BLOCK=test
    export WARP_VERSION=0.0.0
    source "$RELEASE_GATE_SERVICE_ENV"
    source "$workspace/server/test-env.sh"
    test_env_validate_suite_resource_manifest "$TEST_ENV_SUITE_RESOURCE_MANIFEST" "$WARP_VAULT_HOME" "$WARP_CONFIG_HOME"
    cd "$sn_repo"
    settlement_v2_e2e_tests='^Test(SettlementV2E2E)'
    go test ./mainnet -run "$settlement_v2_e2e_tests" -count=1 -timeout 30m
    go test -race ./mainnet -run "$settlement_v2_e2e_tests" -count=1 -timeout 30m
    # server/stmonitor/testfixture creates and drops its own database and role
    # for each of these roots, on this gate's private PostgreSQL only.
    stmonitor_fixture_tests='^(TestMonitorOperatorCredentialFenceAndPublicationAmbiguity|TestMonitorOperatorCommandReadsProductionJournalAndRestartsOutage|TestMonitorStorageStoppedOperatorReleasesOwnersBeforePeerStops|TestMonitorStorageOperatorLostAckRetainsIncident)$'
    export STMONITOR_TEST_ADMIN_URL="postgres://postgres:urnetwork-local-test@$WARP_TEST_ENV_PORTABLE_POSTGRES_AUTHORITY/postgres?sslmode=disable"
    go test ./mainnet -run "$stmonitor_fixture_tests" -count=1
    go test -race ./mainnet -run "$stmonitor_fixture_tests" -count=1
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
  # Gates 3 and 4 need SV-6 and the E suite, so the scale shard's database half
  # and the end-to-end suite run whenever the database suites do.
  if [[ "$release_gate_services_ready" != 1 ]]; then
    release_gate_start server-scale release_gate_unavailable_services
    release_gate_start sn-e2e release_gate_unavailable_services
  else
    release_gate_start server-scale release_phase_server_scale
    release_gate_start sn-e2e release_phase_sn_e2e
  fi
else
  echo "[release-1.0] DB suites deferred (set RUN_SERVER_DB_TESTS=1 to create private disposable PostgreSQL/Redis)"
fi

echo "[release-1.0] Subtensor infrastructure regressions"
release_phase_xops() {
  cd "$workspace/xops"
  # Keep the complete node/playbook suite and both gateway security regressions.
  # Grafana, edge deployment and Planetoid backup reviews are outside the SN
  # runtime inventory and keep their own infrastructure qualification.
  python3 -m unittest \
    main/ansible/tests/test_subtensor_playbook.py \
    main.ansible.tests.test_vulnscan2_resolved.Vulnscan2ResolvedInfrastructureTests.test_vs2_011_subtensor_local_rpc_and_restricted_gateway_render \
    main.ansible.tests.test_vulnscan2_resolved.Vulnscan2ResolvedInfrastructureTests.test_vs2_011_unpaced_gateway_rejects_request_and_connection_quotas
}
release_gate_start xops release_phase_xops

# Join every admitted phase before inspecting the source again, including a
# failed phase. Retain its failure even when all final fences pass.
release_gate_status=0
release_gate_complete || release_gate_status=$?
if (( release_gate_active != 0 )); then
  echo 'release gate: source fences cannot precede an unproven child join' >&2
  exit 1
fi
release_gate_fence_status=0
if [[ "$release_gate_diagnostic" == 1 && "$release_gate_dependency_mode" == pinned ]]; then
  if [[ -f "$release_gate_root/preflights/source-freeze/stdout" ]]; then
    release_source_snapshot="$(< "$release_gate_root/preflights/source-freeze/stdout")"
  else
    echo 'diagnostic source preflight did not complete; original snapshot is unavailable' >&2
    release_gate_fence_status=1
  fi
fi

echo "[release-1.0] patch hygiene"
for repo in "${release_repos[@]}"; do
  # Local mode never touches a vault checkout, not even its Git metadata.
  if [[ "$release_gate_dependency_mode" == local && "$repo" == vault ]]; then
    continue
  fi
  git -C "$workspace/$repo" diff --check || release_gate_fence_status=1
  git -C "$workspace/$repo" diff --cached --check || release_gate_fence_status=1
done

if [[ "$release_gate_dependency_mode" == local ]]; then
  echo "[release-1.0] final release-lock and source-freeze checkouts skipped (local mode)"
else
  echo "[release-1.0] final release-lock checkout"
  (
    cd "$sn_repo"
    # The full Go suite checks this near the start. Repeat it after every long
    # gate so an independently updated sibling checkout cannot escape the run.
    go test ./sim-testnet -run '^TestReleaseLockMatchesCheckout$' -count=1
  ) || release_gate_fence_status=1

  echo "[release-1.0] final source-freeze checkout"
  if ! final_release_source_snapshot="$(release_gate_record_preflight final-source-freeze "$sn_repo/scripts/check-release-source-freeze.sh" "$workspace")"; then
    release_gate_fence_status=1
  fi
  if [[ "$final_release_source_snapshot" != "$release_source_snapshot" ]]; then
    echo "release source revisions changed while the gate was running" >&2
    # Here-strings, which bash writes itself, not process substitutions,
    # which it never waits for, so the gate leaves no child of its own.
    diff -u /dev/fd/3 /dev/fd/4 3<<<"$release_source_snapshot" 4<<<"$final_release_source_snapshot" >&2 || true
    release_gate_fence_status=1
  fi
  printf '%s\n' "$final_release_source_snapshot"
fi

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

if [[ "$release_gate_dependency_mode" == local ]]; then
  echo "[release-1.0] local release gate passed (local mode: working checkouts, not sealed)"
else
  echo "[release-1.0] local release gate passed"
fi
