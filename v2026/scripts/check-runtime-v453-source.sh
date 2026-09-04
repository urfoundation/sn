#!/usr/bin/env bash
set -euo pipefail

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="$sn_repo/docs/spec/runtime-v453-source.sha256"
metadata_manifest="$sn_repo/docs/spec/runtime-metadata-static-source.sha256"
repository="https://github.com/RaoFoundation/subtensor"
raw_repository="https://raw.githubusercontent.com/RaoFoundation/subtensor"
tag="v453"
commit="823bdcbc58a29f60b243be4737a7c72b34ac7d93"
expected_files=12
expected_metadata_files=9

[[ -f "$manifest" ]] || {
  echo "runtime 453 source manifest is missing: $manifest" >&2
  exit 1
}
[[ -f "$metadata_manifest" ]] || {
  echo "runtime metadata source manifest is missing: $metadata_manifest" >&2
  exit 1
}

tag_refs="$(git ls-remote --tags "$repository" "refs/tags/$tag" "refs/tags/$tag^{}")"
resolved="$(printf '%s\n' "$tag_refs" | awk -v ref="refs/tags/$tag^{}" '$2 == ref { print $1; exit }')"
if [[ -z "$resolved" ]]; then
  resolved="$(printf '%s\n' "$tag_refs" | awk -v ref="refs/tags/$tag" '$2 == ref { print $1; exit }')"
fi
if [[ "$resolved" != "$commit" ]]; then
  echo "runtime tag $tag resolves to ${resolved:-missing}, want $commit" >&2
  exit 1
fi

source_checkout="${SUBTENSOR_RUNTIME_SOURCE:-}"
if [[ -n "$source_checkout" ]]; then
  if ! git -C "$source_checkout" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    echo "SUBTENSOR_RUNTIME_SOURCE is not a Git checkout: $source_checkout" >&2
    exit 1
  fi
  checkout_head="$(git -C "$source_checkout" rev-parse HEAD)"
  [[ "$checkout_head" == "$commit" ]] || {
    echo "subtensor checkout HEAD is $checkout_head, want $commit" >&2
    exit 1
  }
  checkout_status="$(git -C "$source_checkout" status --porcelain=v1 --untracked-files=all)"
  [[ -z "$checkout_status" ]] || {
    echo "subtensor checkout has uncommitted or untracked files" >&2
    exit 1
  }
fi

declare -A seen_paths=()
count=0
while read -r expected path extra; do
  [[ -n "$expected" && -n "$path" && -z "${extra:-}" ]] || {
    echo "invalid runtime source manifest row: $expected $path ${extra:-}" >&2
    exit 1
  }
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || {
    echo "invalid runtime source digest for $path" >&2
    exit 1
  }
  [[ "$path" =~ ^[A-Za-z0-9_./-]+$ && "$path" != /* && "$path" != ".." && "$path" != ../* && "$path" != */../* && "$path" != */.. ]] || {
    echo "unsafe runtime source path: $path" >&2
    exit 1
  }
  [[ -z "${seen_paths[$path]:-}" ]] || {
    echo "duplicate runtime source path: $path" >&2
    exit 1
  }
  seen_paths[$path]=1
  if [[ -n "$source_checkout" ]]; then
    observed="$(git -C "$source_checkout" show "$commit:$path" | sha256sum | awk '{print $1}')"
  else
    observed="$(curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$raw_repository/$commit/$path" | sha256sum | awk '{print $1}')"
  fi
  if [[ "$observed" != "$expected" ]]; then
    echo "runtime source digest mismatch for $path: $observed, want $expected" >&2
    exit 1
  fi
  count=$((count + 1))
done < "$manifest"

if [[ "$count" -ne "$expected_files" ]]; then
  echo "runtime source manifest has $count files, want $expected_files" >&2
  exit 1
fi

# Pin corroborating source and upstream integration tests for the three
# reviewed artifacts. The separate exact-Wasm checker is the authoritative
# state-independence and byte-identity gate for metadata reuse.
declare -A resolved_metadata_refs=()
declare -A seen_metadata_paths=()
metadata_count=0
while read -r metadata_ref_kind metadata_ref_name metadata_commit expected path extra; do
  [[ -n "$metadata_ref_kind" && -n "$metadata_ref_name" && -n "$metadata_commit" && -n "$expected" && -n "$path" && -z "${extra:-}" ]] || {
    echo "invalid runtime metadata source manifest row" >&2
    exit 1
  }
  metadata_ref="$metadata_ref_kind:$metadata_ref_name"
  case "$metadata_ref:$metadata_commit" in
    head:release-v451:d78d9cc6a6ee4d805f74a35414baaef8be025a5f|tag:v452:da06f033663896ef2fdbbfc3ecc68ca908fba0f5|tag:v453:823bdcbc58a29f60b243be4737a7c72b34ac7d93) ;;
    *)
      echo "unreviewed runtime metadata source identity: $metadata_ref_kind $metadata_ref_name $metadata_commit" >&2
      exit 1
      ;;
  esac
  [[ "$expected" =~ ^[0-9a-f]{64}$ ]] || {
    echo "invalid runtime metadata source digest for $metadata_ref:$path" >&2
    exit 1
  }
  [[ "$path" == "runtime/src/lib.rs" || "$path" == "runtime/tests/metadata.rs" || "$path" == "support/procedural-fork/src/construct_runtime/expand/metadata.rs" ]] || {
    echo "unexpected runtime metadata source path: $path" >&2
    exit 1
  }
  metadata_key="$metadata_ref:$path"
  [[ -z "${seen_metadata_paths[$metadata_key]:-}" ]] || {
    echo "duplicate runtime metadata source path: $metadata_key" >&2
    exit 1
  }
  seen_metadata_paths[$metadata_key]=1
  if [[ -z "${resolved_metadata_refs[$metadata_ref]:-}" ]]; then
    if [[ "$metadata_ref_kind" == "head" ]]; then
      metadata_refs="$(git ls-remote --heads "$repository" "refs/heads/$metadata_ref_name")"
      metadata_resolved="$(printf '%s\n' "$metadata_refs" | awk -v ref="refs/heads/$metadata_ref_name" '$2 == ref { print $1; exit }')"
    else
      metadata_refs="$(git ls-remote --tags "$repository" "refs/tags/$metadata_ref_name" "refs/tags/$metadata_ref_name^{}")"
      metadata_resolved="$(printf '%s\n' "$metadata_refs" | awk -v ref="refs/tags/$metadata_ref_name^{}" '$2 == ref { print $1; exit }')"
      if [[ -z "$metadata_resolved" ]]; then
        metadata_resolved="$(printf '%s\n' "$metadata_refs" | awk -v ref="refs/tags/$metadata_ref_name" '$2 == ref { print $1; exit }')"
      fi
    fi
    [[ "$metadata_resolved" == "$metadata_commit" ]] || {
      echo "runtime metadata $metadata_ref resolves to ${metadata_resolved:-missing}, want $metadata_commit" >&2
      exit 1
    }
    resolved_metadata_refs[$metadata_ref]="$metadata_resolved"
  elif [[ "${resolved_metadata_refs[$metadata_ref]}" != "$metadata_commit" ]]; then
    echo "runtime metadata $metadata_ref has inconsistent commits" >&2
    exit 1
  fi
  if [[ -n "$source_checkout" && "$metadata_commit" == "$commit" ]]; then
    observed="$(git -C "$source_checkout" show "$metadata_commit:$path" | sha256sum | awk '{print $1}')"
  else
    observed="$(curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 "$raw_repository/$metadata_commit/$path" | sha256sum | awk '{print $1}')"
  fi
  [[ "$observed" == "$expected" ]] || {
    echo "runtime metadata source digest mismatch for $metadata_ref:$path: $observed, want $expected" >&2
    exit 1
  }
  metadata_count=$((metadata_count + 1))
done < "$metadata_manifest"

if [[ "$metadata_count" -ne "$expected_metadata_files" ]]; then
  echo "runtime metadata source manifest has $metadata_count files, want $expected_metadata_files" >&2
  exit 1
fi

echo "runtime source verified tag=$tag commit=$commit files=$count metadata_files=$metadata_count"
