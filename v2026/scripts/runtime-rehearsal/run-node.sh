#!/usr/bin/env bash
# Starts one runtime-rehearsal dev chain (VERSION2-FINAL.md §14 gate 2) on the
# node that build.sh produced: the `dev` chain spec (one authority, Alice as
# sudo, the dev accounts funded), blocks sealed on demand by
# engine_createBlock and finalized at once, the EVM chain ID set to mainnet's
# 964 and contract creation open to every account. The node takes no peers:
# rehearsal chains share a genesis, and two of them on one host would
# otherwise find each other and exchange transactions. The single-state
# transaction pool validates each submission against the newest block; the
# fork-aware pool can still validate it against the block before.
#
# RUNTIME_REHEARSAL_NODE names the node binary. Optional:
# RUNTIME_REHEARSAL_BASE_PATH (default a new temporary directory),
# RUNTIME_REHEARSAL_RPC_PORT (9944), RUNTIME_REHEARSAL_P2P_PORT (30333) and
# RUNTIME_REHEARSAL_SEALING (manual; or instant, or a period in milliseconds).
set -euo pipefail

node="${RUNTIME_REHEARSAL_NODE:?set RUNTIME_REHEARSAL_NODE to the node-subtensor binary from build.sh}"
base_path="${RUNTIME_REHEARSAL_BASE_PATH:-$(mktemp -d)}"
rpc_port="${RUNTIME_REHEARSAL_RPC_PORT:-9944}"
p2p_port="${RUNTIME_REHEARSAL_P2P_PORT:-30333}"
sealing="${RUNTIME_REHEARSAL_SEALING:-manual}"

# Raw genesis keys: twox128("EVM") ++ twox128("DisableWhitelistCheck") = true,
# and twox128("EVMChainId") ++ twox128("ChainId") = 964 as little-endian u64.
disable_whitelist_key="0x1da53b775b270400e7e61ed5cbc5a146caf95274a639095fe69fe6b5df1f1b2e"
chain_id_key="0x0f6342be23148b1fecb28322dcb30aefd8b4519d4aceb8073dbaffde1eef0d79"
chain_id_964="0xc403000000000000"

mkdir -p "$base_path"
spec="$base_path/rehearsal-spec.json"
"$node" build-spec --chain dev --raw --disable-default-bootnode 2>/dev/null |
  jq --arg whitelist "$disable_whitelist_key" --arg chain "$chain_id_key" --arg id "$chain_id_964" \
    '.genesis.raw.top[$whitelist] = "0x01" | .genesis.raw.top[$chain] = $id' >"$spec"

echo "rehearsal chain: base path $base_path, RPC ws://127.0.0.1:$rpc_port and http://127.0.0.1:$rpc_port" >&2
exec "$node" --chain "$spec" --base-path "$base_path" --validator --unsafe-force-node-key-generation \
  --sealing "$sealing" \
  --rpc-port "$rpc_port" --rpc-methods unsafe --rpc-cors all \
  --listen-addr "/ip4/127.0.0.1/tcp/$p2p_port" --no-mdns --reserved-only --in-peers 0 --out-peers 0 \
  --state-pruning archive --blocks-pruning archive --offchain-worker never \
  --pool-type single-state --tx-ban-seconds 0 \
  --no-prometheus --no-telemetry
