#!/usr/bin/env bash
set -euo pipefail

# Builds every qualification export that ./mainnet's tests consume into one new
# private directory, from this checkout and the sibling server checkout as they
# are: the two engines and the Rust-exported jobs of
# tools/runtime-metadata-probe, the provider originals that ./validator and
# ./payoutartifact export, and the server's chain-964 fee inputs. It runs
# offline: Cargo builds --locked --offline and rustup may not install a
# toolchain, so the toolchain that rust-toolchain.toml pins and every crate of
# its Cargo.lock must already be installed (about 2 GB in CARGO_HOME; `cargo
# fetch --locked` once, online), and Go needs its module cache.
# manifest records each export file's source commit and SHA-256, and
# environment.sh, written last, exports the variable that names each export.

if (( $# != 1 )) || [[ "$1" != /* || -e "$1" ]]; then
  echo 'usage: build-mainnet-exports.sh NEW-ABSOLUTE-DIRECTORY' >&2
  exit 64
fi
output="$1"
sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace="$(dirname "$sn_repo")"

# On 2026-10-14 the build peaked at 3.3 GiB, nearly all of it the Cargo target,
# which is removed once the Rust exports are written, leaving 0.5 GiB of
# exports; require about twice the peak free.
required_kib=$((7 * 1024 * 1024))
available_kib="$(df -Pk -- "$(dirname "$output")" | awk 'NR == 2 { print $4 }')"
if ! [[ "$available_kib" =~ ^[0-9]+$ ]] || (( available_kib < required_kib )); then
  printf 'mainnet exports: %s KiB free for %s, need %s KiB; not building\n' "$available_kib" "$output" "$required_kib" >&2
  exit 1
fi
mkdir -m 700 -- "$output"

# Each export: the variable that names it, its path in the directory, and the
# checkout it is built from.
exports='URNETWORK_NATIVE_CAPTURE_ENGINE runtime-historical-capture sn
URNETWORK_NATIVE_EXECUTION_ENGINE runtime-historical-proof sn
URNETWORK_NATIVE_EXECUTION_FIXTURE execution/native-job.json sn
URNETWORK_NATIVE_PRODUCER_FIXTURE producer sn
URNETWORK_NATIVE_CONSERVATION_FIXTURE conservation sn
URNETWORK_NATIVE_PRINCIPAL_FIXTURE_DIR principal sn
URNETWORK_NATIVE_PRINCIPAL_EFFECTS_FIXTURE_DIR principal-effects sn
URNETWORK_NATIVE_VAULT_CAPTURE_FIXTURE_DIR vault-capture sn
URNETWORK_NATIVE_VAULT_CAPTURE_SEQUENCE_FIXTURE_DIR vault-capture-sequence sn
URNETWORK_NATIVE_RUNTIME_RENEWAL_FIXTURE runtime-renewal sn
URNETWORK_NATIVE_WHOLE_FEE_FIXTURE_DIR whole-fee sn
URNETWORK_NATIVE_YUMA_FIXTURE_DIR yuma sn
URNETWORK_NATIVE_YUMA_CAPACITY_FIXTURE_DIR yuma-capacity sn
URNETWORK_NATIVE_YUMA_POPULATED_FIXTURE_DIR yuma-populated sn
URNETWORK_NATIVE_TREASURY_FIXTURE treasury/treasury-credit.json sn
URNETWORK_PROVIDER_ATTEMPT_COMPLETED_FIXTURE_DIR provider-attempt sn
URNETWORK_PROVIDER_WORK_FIXTURE_DIR provider-work sn
URNETWORK_ECONOMIC_FEE_FIXTURE_DIR economic-fee server'

# Each Rust export's directory: the variable that gives it to the libtest root
# that writes it, the directory, and the root, under
# historical::tests::native_execution_tests.
rust_exports='URNETWORK_NATIVE_EXECUTION_FIXTURE_OUT execution historical_native_execution_exports_actual_original_program_for_go_consumer
URNETWORK_NATIVE_PRODUCER_FIXTURE_OUT producer historical_native_producer_exports_contiguous_original_jobs
URNETWORK_NATIVE_CONSERVATION_FIXTURE_OUT conservation historical_native_conservation_exports_proof_drained_contiguous_jobs
URNETWORK_NATIVE_PRINCIPAL_FIXTURE_OUT principal historical_native_principal_exports_original_parent_jobs
URNETWORK_NATIVE_PRINCIPAL_EFFECTS_FIXTURE_OUT principal-effects historical_native_principal_effects_export_original_causal_jobs
URNETWORK_NATIVE_VAULT_CAPTURE_FIXTURE_OUT vault-capture capture_join_tests::historical_native_vault_capture_exports_original_transaction_causes
URNETWORK_NATIVE_VAULT_CAPTURE_SEQUENCE_FIXTURE_OUT vault-capture-sequence capture_join_tests::historical_native_vault_capture_exports_original_capture_sequences
URNETWORK_NATIVE_RUNTIME_RENEWAL_FIXTURE_OUT runtime-renewal runtime_renewal_tests::historical_native_runtime_renewal_exports_actual_upgrade_and_principal_jobs
URNETWORK_NATIVE_WHOLE_FEE_FIXTURE_OUT whole-fee fee_census_tests::historical_native_whole_fee_exports_complete_and_unresolved_original_jobs
URNETWORK_NATIVE_YUMA_FIXTURE_OUT yuma yuma_tests::historical_native_yuma_exports_original_complete_branches
URNETWORK_NATIVE_YUMA_CAPACITY_FIXTURE_OUT yuma-capacity yuma_capacity_tests::historical_native_yuma_exports_complete_1024_and_2048_uid_jobs
URNETWORK_NATIVE_YUMA_POPULATED_FIXTURE_OUT yuma-populated yuma_populated_tests::historical_native_yuma_exports_populated_64_by_1024_and_2048
URNETWORK_NATIVE_TREASURY_FIXTURE_OUT treasury treasury_tests::historical_native_treasury_exports_original_credit_and_custody_jobs'

# The engines and the Rust exports, from the pinned toolchain alone.
unset RUSTUP_TOOLCHAIN
export RUSTUP_AUTO_INSTALL=0
target="$output/cargo-target"
outputs=() roots=()
while read -r variable directory root; do
  mkdir -m 700 -- "$output/$directory"
  outputs+=("$variable=$output/$directory")
  roots+=("historical::tests::native_execution_tests::$root")
done <<< "$rust_exports"
(
  cd "$sn_repo/tools/runtime-metadata-probe"
  CARGO_TARGET_DIR="$target" cargo build --offline --locked --bin runtime-historical-capture --bin runtime-historical-proof
  cp -- "$target/debug/runtime-historical-capture" "$target/debug/runtime-historical-proof" "$output/"
  env "${outputs[@]}" CARGO_TARGET_DIR="$target" cargo test --offline --locked --lib -- --exact "${roots[@]}"
)
rm -rf -- "${target:?}"

# The provider originals: the completed attempt first, whose artifact the
# whole-work export reads. Both refuse an output that is not a new 0700
# directory.
mkdir -m 700 -- "$output/provider-attempt" "$output/provider-work" "$output/economic-fee"
(
  cd "$sn_repo"
  URNETWORK_PROVIDER_ATTEMPT_COMPLETED_FIXTURE_DIR="$output/provider-attempt" go test ./validator -run '^TestProviderAttemptSourceExportsActualCompletedFailedIdleOriginals$' -count=1
  URNETWORK_PROVIDER_WORK_ATTEMPT_ARTIFACT="$output/provider-attempt/artifact.json" URNETWORK_PROVIDER_WORK_FIXTURE_DIR="$output/provider-work" go test ./payoutartifact -run '^TestWholeWorkExportsCompleteOriginalsForActualTrialProviderUniverse$' -count=1
)
(
  cd "$workspace/server"
  URNETWORK_ECONOMIC_FEE_EXPORT_DIR="$output/economic-fee" go test ./strecovery -run '^TestExportEconomicConservationFeeInputs$' -count=1
)

# A root that matches no test passes without writing: every export must be a
# nonempty file or a directory with a file.
while read -r variable path repository; do
  if [[ -f "$output/$path" && -s "$output/$path" ]] || [[ -d "$output/$path" && -n "$(find "$output/$path" -type f -print -quit)" ]]; then
    continue
  fi
  printf 'mainnet exports: %s is absent or empty: %s\n' "$variable" "$output/$path" >&2
  exit 1
done <<< "$exports"

# Each checkout's HEAD, marked when its working tree differs from it. Other
# jobs share the checkouts, so status takes no index lock.
declare -A commits
for repository in sn server; do
  commit="$(git -C "$workspace/$repository" rev-parse HEAD)"
  changes="$(git -C "$workspace/$repository" --no-optional-locks status --porcelain)"
  [[ -z "$changes" ]] || commit+=+modified
  commits[$repository]="$commit"
done
(
  cd "$output"
  while read -r variable path repository; do
    find "${path%%/*}" -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | while read -r digest file; do
      printf '%s %s@%s sha256:%s %s\n' "$variable" "$repository" "${commits[$repository]}" "$digest" "$file"
    done
  done <<< "$exports"
) > "$output/manifest"
cat -- "$output/manifest"
while read -r variable path repository; do
  printf 'export %s=%q\n' "$variable" "$output/$path"
done <<< "$exports" > "$output/environment.sh.pending"
mv -- "$output/environment.sh.pending" "$output/environment.sh"
printf 'mainnet exports: %s\n' "$output/environment.sh"
