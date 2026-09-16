#!/usr/bin/env bash
set -euo pipefail
umask 077

stage=/mnt/data/sn-testnet/qualification/runtime460-causal-composition-20260915-r1
captures=/mnt/data/sn-testnet/qualification/runtime460-20260915-r1/terra-causal-normal-20260915-r1
runtime=/mnt/data/sn-testnet/qualification/runtime460-20260915-r1

if [ -e "$stage/RAW-SHA256SUMS" ]; then
  printf '%s\n' 'refusing to overwrite completed staging manifest' >&2
  exit 2
fi

copy_file() {
  local source=$1 destination=$2
  mkdir -p -m 700 "$(dirname "$destination")"
  cp --preserve=mode,timestamps "$source" "$destination"
}

copy_receipt() {
  local id source destination file relative body_exit qualification_exit fences
  id=$1
  source="$captures/$id"
  destination="$stage/raw/$id"
  [ -d "$source" ]
  mkdir -p -m 700 "$destination"
  while IFS= read -r -d '' file; do
    relative=${file#"$source/"}
    copy_file "$file" "$destination/$relative"
  done < <(find "$source" -maxdepth 1 -type f -print0 | sort -z)
  while IFS= read -r -d '' file; do
    relative=${file#"$source/"}
    copy_file "$file" "$destination/$relative"
  done < <(find "$source/inputs" -type f -print0 | sort -z)
  while IFS= read -r -d '' file; do
    relative=${file#"$source/"}
    copy_file "$file" "$destination/$relative"
  done < <(find "$source/compile" -maxdepth 1 -type f ! -name '*.test' -print0 | sort -z)
  while IFS= read -r -d '' file; do
    relative=${file#"$source/"}
    copy_file "$file" "$destination/$relative"
  done < <(find "$source/body" -maxdepth 1 -type f -print0 | sort -z)
  body_exit=$(tr -d '\n' <"$source/body/body.exit")
  qualification_exit=$(tr -d '\n' <"$source/body/qualification.exit")
  fences=$(tr -d '\n' <"$source/fences.after")
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$id" "$source" "raw/$id" "$body_exit" "$qualification_exit" "$fences" >>"$stage/RECEIPT-LOCATORS.tsv"
}

printf 'receipt\tsource_capture\tstaged_subset\tbody_exit\tqualification_exit\tfences_after\n' >"$stage/RECEIPT-LOCATORS.tsv"
copy_receipt current_artifact_admission/miner
copy_receipt current_artifact_admission/validator
copy_receipt current_artifact_admission/sim-testnet
copy_receipt native_encoding_stake_and_capacity/crv4
copy_receipt native_encoding_stake_and_capacity/metadata-constant-replacement
copy_receipt original_configuration_identity/sim-testnet
copy_receipt former_current_archive_authority/sim-testnet
copy_receipt catalog_predecessor_omission/crv4-type-repair
copy_receipt catalog_predecessor_omission/validator
copy_receipt catalog_predecessor_omission/sim-testnet

copy_file "$runtime/causal-selection.json" "$stage/inputs/causal-selection.json"
copy_file "$runtime/causal-variants.json" "$stage/inputs/causal-variants.json"
copy_file "$runtime/causal-worktrees/current_artifact_admission/VARIANT.json" "$stage/inputs/variants/current_artifact_admission.VARIANT.json"
copy_file "$runtime/causal-worktrees/native_encoding_stake_and_capacity/VARIANT.json" "$stage/inputs/variants/native_encoding_stake_and_capacity.VARIANT.json"
copy_file "$runtime/causal-worktrees/original_configuration_identity/VARIANT.json" "$stage/inputs/variants/original_configuration_identity.VARIANT.json"
copy_file "$runtime/causal-worktrees/former_current_archive_authority/VARIANT.json" "$stage/inputs/variants/former_current_archive_authority.VARIANT.json"
copy_file "$runtime/causal-worktrees/catalog_predecessor_omission/VARIANT.json" "$stage/inputs/variants/catalog_predecessor_omission.VARIANT.json"
copy_file "$runtime/causal-worktrees/catalog_predecessor_omission-crv4-type-repair/VARIANT.json" "$stage/inputs/variants/catalog_predecessor_omission-crv4-type-repair.VARIANT.json"
copy_file "$runtime/causal-worktrees/native_encoding-metadata-constant-repair/VARIANT.json" "$stage/inputs/variants/native_encoding-metadata-constant-repair.VARIANT.json"
copy_file "$runtime/capacity-type-repair/exact-test-type-fix.patch" "$stage/inputs/patches/exact-test-type-fix.patch"
copy_file "$runtime/causal-worktrees/native_encoding-metadata-constant-repair/exact-metadata-test-fix.patch" "$stage/inputs/patches/exact-metadata-test-fix.patch"

(cd "$stage" && find COMPOSITION-INDEX.md COMPOSITION.json RECEIPT-LOCATORS.tsv STAGING-ATTEMPT-1.exit STAGING-ATTEMPT-1.stderr commands inputs raw -type f -print0 | sort -z | xargs -0 sha256sum) >"$stage/RAW-SHA256SUMS"
(cd "$stage" && sha256sum -c RAW-SHA256SUMS) >"$stage/RAW-SHA256SUMS.verify.stdout" 2>"$stage/RAW-SHA256SUMS.verify.stderr"
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$stage/STAGED-AT"
find "$stage" -type f -exec chmod 600 {} +
