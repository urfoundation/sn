#!/usr/bin/env bash
# Builds the runtime-rehearsal node (VERSION2-FINAL.md §14 gate 2): upstream
# subtensor at the runtime-482 candidate commit with fast blocks, and records the runtime
# Wasm hash. Usage: build.sh <subtensor-checkout-dir>
#
# CARGO_TARGET_DIR selects the build directory (default <checkout>/target) and
# RUNTIME_REHEARSAL_JOBS the cargo parallelism (default 10). protoc must be on
# PATH or named by PROTOC.
set -euo pipefail

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
repository="https://github.com/RaoFoundation/subtensor.git"
# The v482 source candidate and tag must name this exact tree. These build
# pins do not establish runtime compatibility; the source review, exact
# on-chain Wasm execution and complete R battery remain separate gates.
tag="v482"
commit="8cf229f8ef969389d054a4c161b53ffd164a041f"
tree="4cfd29d8d12499775e2b1cbabf64e6fa41f1f7cf"
# Upstream's localnet (scripts/localnet.sh, fast_runtime=True) builds the
# workspace with "pow-faucet metadata-hash fast-runtime". metadata-hash is also
# in mainnet's release build and fast-runtime gives fast blocks. pow-faucet only
# adds a proof-of-work faucet call and drops the default subnet-registration
# rate limit to 0, so it is left out: the runtime keeps mainnet's call set.
features="metadata-hash fast-runtime"
manifest="$sn_repo/docs/spec/runtime-v482-source.sha256"

source_dir="${1:?usage: build.sh <subtensor-checkout-dir>}"
jobs="${RUNTIME_REHEARSAL_JOBS:-10}"

fail() {
  echo "$*" >&2
  exit 1
}

# The peeled commit of an annotated tag, or a lightweight tag's own target.
tagged="$(git ls-remote --tags "$repository" "refs/tags/$tag" "refs/tags/$tag^{}" | awk '$2 ~ /\^\{\}$/ { peeled = $1 } { last = $1 } END { print (peeled != "" ? peeled : last) }')"

if [[ ! -e "$source_dir" ]]; then
  git clone --quiet --no-checkout "$repository" "$source_dir"
fi
git -C "$source_dir" cat-file -e "$commit^{commit}" 2>/dev/null || git -C "$source_dir" fetch --quiet origin "$commit"
git -C "$source_dir" checkout --quiet --detach "$commit"
[[ "$(git -C "$source_dir" rev-parse "$commit^{tree}")" == "$tree" ]] || fail "commit $commit does not have the reviewed tree $tree"
if [[ -n "$tagged" ]]; then
  git -C "$source_dir" cat-file -e "$tagged^{commit}" 2>/dev/null || git -C "$source_dir" fetch --quiet origin "refs/tags/$tag"
  [[ "$(git -C "$source_dir" rev-parse "$tagged^{tree}")" == "$tree" ]] || fail "tag $tag names $tagged, whose tree is not the reviewed $tree"
fi
source_dir="$(cd "$source_dir" && pwd)"
target_dir="${CARGO_TARGET_DIR:-$source_dir/target}"
mkdir -p "$target_dir"
target_dir="$(cd "$target_dir" && pwd)"
[[ "$(git -C "$source_dir" rev-parse HEAD)" == "$commit" ]] || fail "checkout HEAD is not $commit"
[[ -z "$(git -C "$source_dir" status --porcelain=v1 --untracked-files=all)" ]] || fail "checkout $source_dir is not clean"
grep -Eq '^[[:space:]]*spec_version: 482,$' "$source_dir/runtime/src/lib.rs" || fail "runtime/src/lib.rs does not declare spec_version 482"
(cd "$source_dir" && sha256sum --check --quiet --strict "$manifest") || fail "checkout differs from $manifest"
command -v "${PROTOC:-protoc}" >/dev/null || fail "protoc is missing: install protobuf-compiler or set PROTOC"

start="$(date +%s)"
# Run inside the checkout so rustup selects its rust-toolchain.toml (1.89).
# The workspace hint makes the runtime's Wasm build use the checkout's
# Cargo.lock even when the target directory lies outside the checkout.
(cd "$source_dir" && CARGO_TARGET_DIR="$target_dir" WASM_BUILD_WORKSPACE_HINT="$source_dir" cargo build --locked \
  --profile release -p node-subtensor --features "$features" -j "$jobs")
seconds="$(($(date +%s) - start))"

node="$target_dir/release/node-subtensor"
wasm="$target_dir/release/wbuild/node-subtensor-runtime/node_subtensor_runtime.compact.compressed.wasm"
[[ -x "$node" && -f "$wasm" ]] || fail "build did not produce $node and $wasm"

# The dev chain's genesis :code must be the recorded Wasm, byte for byte.
genesis_code="$("$node" build-spec --chain dev --raw --disable-default-bootnode 2>/dev/null | jq -r '.genesis.raw.top["0x3a636f6465"]')"
[[ "$(printf '%s' "${genesis_code#0x}" | xxd -r -p | sha256sum | awk '{ print $1 }')" == "$(sha256sum "$wasm" | awk '{ print $1 }')" ]] ||
  fail "the node's genesis :code differs from $wasm"

record="$target_dir/release/runtime-rehearsal-build.txt"
{
  echo "commit=$commit"
  echo "tree=$tree"
  echo "features=$features"
  echo "node=$node"
  echo "wasm=$wasm"
  echo "wasm_bytes=$(stat -c %s "$wasm")"
  echo "wasm_sha256=$(sha256sum "$wasm" | awk '{ print $1 }')"
  echo "wasm_blake2b_256=$(b2sum -l 256 "$wasm" | awk '{ print $1 }')"
  echo "build_seconds=$seconds"
} >"$record"
cat "$record"
