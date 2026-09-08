#!/usr/bin/env bash
set -euo pipefail

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_manifest="$sn_repo/docs/spec/runtime-metadata-artifacts.json"
probe_dir="$sn_repo/tools/runtime-metadata-probe"
probe_target_dir="${RUNTIME_METADATA_PROBE_TARGET_DIR:-$(dirname "$sn_repo")/temp/runtime-metadata-probe-target}"
rpc_request_id=1

if ! command -v cargo >/dev/null 2>&1 && [[ -x "$HOME/.cargo/bin/cargo" ]]; then
  export PATH="$HOME/.cargo/bin:$PATH"
fi
for command_name in cargo curl jq sha256sum stat xxd; do
  if ! command -v "$command_name" >/dev/null 2>&1; then
    echo "runtime metadata artifact checker requires $command_name" >&2
    exit 1
  fi
done
[[ -f "$source_manifest" ]] || {
  echo "runtime metadata artifact manifest is missing: $source_manifest" >&2
  exit 1
}
[[ -f "$probe_dir/Cargo.toml" && -f "$probe_dir/Cargo.lock" && -f "$probe_dir/rust-toolchain.toml" ]] || {
  echo "runtime metadata exact-Wasm probe is incomplete: $probe_dir" >&2
  exit 1
}
mkdir -p -- "$probe_target_dir"

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/runtime-metadata-artifacts.XXXXXX")"
declare -A probe_expected_outputs=()
declare -A probe_output_paths=()
declare -A probe_error_paths=()
declare -A probe_process_ids=()
probe_versions=()
cleanup() {
  local process_id
  for process_id in "${probe_process_ids[@]:-}"; do
    kill "$process_id" 2>/dev/null || true
  done
  for process_id in "${probe_process_ids[@]:-}"; do
    wait "$process_id" 2>/dev/null || true
  done
  rm -rf -- "$work_dir"
}
trap cleanup EXIT HUP INT TERM
manifest="$work_dir/runtime-metadata-artifacts.json"
cp -- "$source_manifest" "$manifest"

# Reject unknown fields, alternate endpoints, relaxed identities, malformed
# digests and reordered/duplicated versions before any network or Cargo work.
if ! jq -e '
  def hash256: type == "string" and test("^0x[0-9a-f]{64}$");
  def sha256: type == "string" and test("^[0-9a-f]{64}$");
  def git_commit: type == "string" and test("^[0-9a-f]{40}$");
  (type == "object") and
  (keys == ["artifacts", "genesis_hash", "metadata_version", "network", "polkadot_sdk_revision", "runtime_code_storage_key", "runtime_spec_name", "schema_version", "state_version", "substrate_rpc_url", "transaction_version"]) and
  (.schema_version == 1) and
  (.network == "bittensor-testnet") and
  (.substrate_rpc_url == "https://test.finney.opentensor.ai:443") and
  (.genesis_hash == "0x8f9cf856bf558a14440e75569c9e58594757048d7b3a84b5d25f6bd978263105") and
  (.runtime_spec_name == "node-subtensor") and
  (.transaction_version == 1) and
  (.state_version == 1) and
  (.metadata_version == 14) and
  (.runtime_code_storage_key == "0x3a636f6465") and
  (.polkadot_sdk_revision == "cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a") and
  (.artifacts | type == "array" and length == 4 and map(.spec_version) == [451, 452, 453, 454]) and
  all(.artifacts[];
    (type == "object") and
    (keys == ["code_blake2b_256", "code_sha256", "code_size", "code_source", "code_url", "metadata_blake2b_256", "metadata_sha256", "metadata_size", "observation_block", "observation_block_hash", "source_commit", "source_ref_kind", "source_ref_name", "spec_version"]) and
    (.spec_version | type == "number" and floor == . and . >= 451 and . <= 454) and
    (.source_ref_kind | type == "string") and
    (.source_ref_name | type == "string" and length > 0) and
    (.source_commit | git_commit) and
    (.observation_block | type == "number" and floor == . and . > 0) and
    (.observation_block_hash | hash256) and
    (.code_source == "substrate-storage" or .code_source == "github-release") and
    (.code_url == null or (.code_url | type == "string" and startswith("https://github.com/RaoFoundation/subtensor/releases/download/"))) and
    (.code_size | type == "number" and floor == . and . > 0) and
    (.code_sha256 | sha256) and
    (.code_blake2b_256 | hash256) and
    (.metadata_size | type == "number" and floor == . and . > 0) and
    (.metadata_sha256 | sha256) and
    (.metadata_blake2b_256 | hash256)
  )
' "$manifest" >/dev/null; then
  echo "runtime metadata artifact manifest is malformed or unreviewed" >&2
  exit 1
fi

rpc_url="$(jq -r '.substrate_rpc_url' "$manifest")"
genesis_hash="$(jq -r '.genesis_hash' "$manifest")"
spec_name="$(jq -r '.runtime_spec_name' "$manifest")"
transaction_version="$(jq -r '.transaction_version' "$manifest")"
state_version="$(jq -r '.state_version' "$manifest")"
metadata_version="$(jq -r '.metadata_version' "$manifest")"
code_storage_key="$(jq -r '.runtime_code_storage_key' "$manifest")"
sdk_revision="$(jq -r '.polkadot_sdk_revision' "$manifest")"

# Writes one structurally valid JSON-RPC result. Transport and explicit
# capacity errors receive four bounded attempts; every other RPC error fails.
rpc_call() {
  local method="$1"
  local params="$2"
  local output="$3"
  local request="$work_dir/request.json"
  local candidate="$work_dir/response.json"
  local attempt
  local delay=2
  jq -nc --arg method "$method" --argjson params "$params" --argjson id "$rpc_request_id" \
    '{jsonrpc:"2.0", id:$id, method:$method, params:$params}' >"$request"
  for attempt in 1 2 3 4; do
    if curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
      --connect-timeout 10 --max-time 180 --retry 2 --retry-all-errors \
      --header 'content-type: application/json' --data-binary "@$request" "$rpc_url" >"$candidate"; then
      if jq -e --argjson id "$rpc_request_id" \
        '.jsonrpc == "2.0" and .id == $id and has("result") and (has("error") | not)' "$candidate" >/dev/null; then
        mv "$candidate" "$output"
        return 0
      fi
      if ! jq -er '(.error.message // "") | ascii_downcase | test("rate limit|too many requests|temporarily unavailable|timeout|overload|try again")' "$candidate" >/dev/null; then
        echo "runtime metadata RPC $method returned a permanent or malformed error" >&2
        jq -c '{jsonrpc, id, error}' "$candidate" >&2 || true
        return 1
      fi
    fi
    if [[ "$attempt" -eq 4 ]]; then
      echo "runtime metadata RPC $method exhausted four attempts" >&2
      return 1
    fi
    sleep "$delay"
    delay=$((delay * 2))
  done
}

genesis_response="$work_dir/genesis.json"
rpc_call "chain_getBlockHash" '[0]' "$genesis_response"
observed_genesis="$(jq -r '.result' "$genesis_response")"
[[ "$observed_genesis" == "$genesis_hash" ]] || {
  echo "runtime metadata endpoint genesis $observed_genesis, want $genesis_hash" >&2
  exit 1
}

(
  cd "$probe_dir"
  CARGO_TARGET_DIR="$probe_target_dir" cargo build --quiet --locked
)
probe_binary="$probe_target_dir/debug/runtime-metadata-probe"
[[ -x "$probe_binary" ]] || {
  echo "runtime metadata exact-Wasm probe binary is unavailable after build" >&2
  exit 1
}

while IFS= read -r artifact_json; do
  spec_version="$(jq -r '.spec_version' <<<"$artifact_json")"
  source_ref_kind="$(jq -r '.source_ref_kind' <<<"$artifact_json")"
  source_ref_name="$(jq -r '.source_ref_name' <<<"$artifact_json")"
  source_commit="$(jq -r '.source_commit' <<<"$artifact_json")"
  observation_block="$(jq -r '.observation_block' <<<"$artifact_json")"
  observation_block_hash="$(jq -r '.observation_block_hash' <<<"$artifact_json")"
  code_source="$(jq -r '.code_source' <<<"$artifact_json")"
  code_url="$(jq -r '.code_url // ""' <<<"$artifact_json")"
  code_size="$(jq -r '.code_size' <<<"$artifact_json")"
  code_sha256="$(jq -r '.code_sha256' <<<"$artifact_json")"
  code_blake2b_256="$(jq -r '.code_blake2b_256' <<<"$artifact_json")"
  metadata_size="$(jq -r '.metadata_size' <<<"$artifact_json")"
  metadata_sha256="$(jq -r '.metadata_sha256' <<<"$artifact_json")"
  metadata_blake2b_256="$(jq -r '.metadata_blake2b_256' <<<"$artifact_json")"

  case "$spec_version:$source_ref_kind:$source_ref_name:$source_commit:$code_source:$code_url" in
    "451:head:release-v451:d78d9cc6a6ee4d805f74a35414baaef8be025a5f:substrate-storage:") ;;
    "452:tag:v452:da06f033663896ef2fdbbfc3ecc68ca908fba0f5:github-release:https://github.com/RaoFoundation/subtensor/releases/download/v452/subtensor.wasm") ;;
    "453:tag:v453:823bdcbc58a29f60b243be4737a7c72b34ac7d93:github-release:https://github.com/RaoFoundation/subtensor/releases/download/v453/subtensor.wasm") ;;
    "454:tag:v454:14cde6410fe8ec81a940e290c56f94a632a0988d:github-release:https://github.com/RaoFoundation/subtensor/releases/download/v454/subtensor.wasm") ;;
    *)
      echo "runtime metadata artifact $spec_version has unreviewed source provenance" >&2
      exit 1
      ;;
  esac

  block_response="$work_dir/v${spec_version}-block.json"
  rpc_call "chain_getBlockHash" "[$observation_block]" "$block_response"
  observed_block_hash="$(jq -r '.result' "$block_response")"
  [[ "$observed_block_hash" == "$observation_block_hash" ]] || {
    echo "runtime $spec_version block $observation_block hash $observed_block_hash, want $observation_block_hash" >&2
    exit 1
  }

  code_hash_response="$work_dir/v${spec_version}-code-hash.json"
  code_hash_params="$(jq -nc --arg key "$code_storage_key" --arg block_hash "$observation_block_hash" '[$key, $block_hash]')"
  rpc_call "state_getStorageHash" "$code_hash_params" "$code_hash_response"
  observed_code_hash="$(jq -r '.result' "$code_hash_response")"
  [[ "$observed_code_hash" == "$code_blake2b_256" ]] || {
    echo "runtime $spec_version on-chain code hash $observed_code_hash, want $code_blake2b_256" >&2
    exit 1
  }

  wasm_path="$work_dir/v${spec_version}.wasm"
  if [[ "$code_source" == "substrate-storage" ]]; then
    code_response="$work_dir/v${spec_version}-code.json"
    rpc_call "state_getStorage" "$code_hash_params" "$code_response"
    expected_hex_length=$((code_size * 2 + 2))
    if ! jq -e --argjson expected "$expected_hex_length" \
      '.result | type == "string" and length == $expected and test("^0x[0-9a-f]+$")' "$code_response" >/dev/null; then
      echo "runtime $spec_version on-chain code is malformed or has the wrong size" >&2
      exit 1
    fi
    jq -r '.result[2:]' "$code_response" | xxd -r -p >"$wasm_path"
  else
    curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 \
      --connect-timeout 10 --max-time 180 --retry 4 --retry-all-errors \
      --output "$wasm_path" "$code_url"
  fi

  observed_code_size="$(stat -c '%s' "$wasm_path")"
  observed_code_sha256="$(sha256sum "$wasm_path" | awk '{print $1}')"
  [[ "$observed_code_size" == "$code_size" && "$observed_code_sha256" == "$code_sha256" ]] || {
    echo "runtime $spec_version code bytes size/SHA-256 $observed_code_size/$observed_code_sha256, want $code_size/$code_sha256" >&2
    exit 1
  }

  probe_versions+=("$spec_version")
  probe_output_paths[$spec_version]="$work_dir/v${spec_version}-probe.stdout"
  probe_error_paths[$spec_version]="$work_dir/v${spec_version}-probe.stderr"
  probe_expected_outputs[$spec_version]="runtime metadata verified sdk_revision=$sdk_revision spec_name=$spec_name spec_version=$spec_version transaction_version=$transaction_version state_version=$state_version metadata_version=$metadata_version code_size=$code_size metadata_size=$metadata_size code_sha256=0x$code_sha256 code_blake2b_256=$code_blake2b_256 metadata_sha256=0x$metadata_sha256 metadata_blake2b_256=$metadata_blake2b_256"
  "$probe_binary" \
    "$wasm_path" "$spec_name" "$spec_version" "$transaction_version" "$state_version" \
    "$metadata_version" "$code_size" "$metadata_size" \
    "0x$code_sha256" "$code_blake2b_256" "0x$metadata_sha256" "$metadata_blake2b_256" \
    >"${probe_output_paths[$spec_version]}" 2>"${probe_error_paths[$spec_version]}" &
  probe_process_ids[$spec_version]=$!
done < <(jq -c '.artifacts[]' "$manifest")

# Runtime compilation/execution is CPU-bound. Run the four immutable probes
# concurrently, but consume their results in manifest order for stable output.
for spec_version in "${probe_versions[@]}"; do
  probe_status=0
  wait "${probe_process_ids[$spec_version]}" || probe_status=$?
  unset 'probe_process_ids[$spec_version]'
  if [[ "$probe_status" -ne 0 ]]; then
    echo "runtime $spec_version exact-Wasm probe failed" >&2
    sed -n '1,120p' "${probe_error_paths[$spec_version]}" >&2
    exit 1
  fi
  probe_output="$(<"${probe_output_paths[$spec_version]}")"
  expected_probe_output="${probe_expected_outputs[$spec_version]}"
  [[ "$probe_output" == "$expected_probe_output" ]] || {
    echo "runtime $spec_version probe output does not exactly match its manifest" >&2
    printf 'observed: %s\nexpected: %s\n' "$probe_output" "$expected_probe_output" >&2
    exit 1
  }
  if [[ -s "${probe_error_paths[$spec_version]}" ]]; then
    echo "runtime $spec_version exact-Wasm probe emitted unexpected diagnostics" >&2
    sed -n '1,120p' "${probe_error_paths[$spec_version]}" >&2
    exit 1
  fi
  printf '%s\n' "$probe_output"
done

echo "runtime metadata artifacts verified versions=451,452,453,454 sdk_revision=$sdk_revision"
