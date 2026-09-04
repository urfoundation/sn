#!/usr/bin/env bash
set -euo pipefail

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="$sn_repo/docs/spec/runtime-v453-source.sha256"
repository="https://github.com/RaoFoundation/subtensor"
raw_repository="https://raw.githubusercontent.com/RaoFoundation/subtensor"
tag="v453"
commit="823bdcbc58a29f60b243be4737a7c72b34ac7d93"
expected_files=12

[[ -f "$manifest" ]] || {
  echo "runtime 453 source manifest is missing: $manifest" >&2
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

echo "runtime source verified tag=$tag commit=$commit files=$count"
