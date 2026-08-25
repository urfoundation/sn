#!/usr/bin/env bash
# Regenerate the STSubnet Go bindings from the exported forge ABI.
#
# Source of truth: sn/evm (Foundry). Pipeline:
#   cd evm && forge build
#   jq .abi out/STSubnet.sol/STSubnet.json > abi/STSubnet.abi.json
#   ./stabi/generate.sh          (write generated files)
#   ./stabi/generate.sh --check  (verify without modifying the checkout)
#
# Requires abigen matching go.mod's go-ethereum version:
#   go install github.com/ethereum/go-ethereum/cmd/abigen@v1.16.7
set -euo pipefail

cd "$(dirname "$0")"

ABIGEN="${ABIGEN:-abigen}"
mode="${1:---write}"
if [[ "$mode" != "--write" && "$mode" != "--check" ]]; then
    echo "usage: $0 [--write|--check]" >&2
    exit 2
fi

temporary_dir="$(mktemp -d)"
trap 'rm -rf -- "$temporary_dir"' EXIT

jq -c .abi ../evm/out/STSubnet.sol/STSubnet.json > "$temporary_dir/STSubnet.abi.json"
jq -c .abi ../evm/out/STCoordinator.sol/STCoordinator.json > "$temporary_dir/STCoordinator.abi.json"
jq -c .abi ../evm/out/STSettlementVault.sol/STSettlementVault.json > "$temporary_dir/STSettlementVault.abi.json"
jq -c .abi ../evm/out/STReserveSink.sol/STReserveSink.json > "$temporary_dir/STReserveSink.abi.json"

"$ABIGEN" --v2 \
    --abi "$temporary_dir/STSubnet.abi.json" \
    --pkg stabi \
    --type STSubnet \
    --out "$temporary_dir/stsubnet.go"

"$ABIGEN" --v2 \
    --abi "$temporary_dir/STCoordinator.abi.json" \
    --pkg stabi \
    --type STCoordinator \
    --out "$temporary_dir/stcoordinator.go"

"$ABIGEN" --v2 \
    --abi "$temporary_dir/STSettlementVault.abi.json" \
    --pkg stabi \
    --type STSettlementVault \
    --out "$temporary_dir/stsettlementvault.go"

"$ABIGEN" --v2 \
    --abi "$temporary_dir/STReserveSink.abi.json" \
    --pkg stabi \
    --type STReserveSink \
    --out "$temporary_dir/streservesink.go"

generated=(
    "STSubnet.abi.json:../evm/abi/STSubnet.abi.json"
    "STCoordinator.abi.json:../evm/abi/STCoordinator.abi.json"
    "STSettlementVault.abi.json:../evm/abi/STSettlementVault.abi.json"
    "STReserveSink.abi.json:../evm/abi/STReserveSink.abi.json"
    "stsubnet.go:stsubnet.go"
    "stcoordinator.go:stcoordinator.go"
    "stsettlementvault.go:stsettlementvault.go"
    "streservesink.go:streservesink.go"
    "STSubnet.abi.json:../stctl/st_abi.json"
)

for entry in "${generated[@]}"; do
    source_name="${entry%%:*}"
    destination="${entry#*:}"
    if [[ "$mode" == "--check" ]]; then
        if ! cmp -s "$temporary_dir/$source_name" "$destination"; then
            echo "generated binding/ABI is stale: $destination; run ./stabi/generate.sh" >&2
            exit 1
        fi
        if [[ "$(stat -c '%a' "$destination")" != "644" ]]; then
            echo "generated binding/ABI has non-portable mode: $destination; run ./stabi/generate.sh" >&2
            exit 1
        fi
    else
        install -m 0644 "$temporary_dir/$source_name" "$destination"
    fi
done

# stctl is a quarantined pre-1.0 monolith diagnostic. Keep its packaged ABI
# coherent with the legacy STSubnet binding; release clients use the three
# generated bindings above and sim-testnet embeds the reviewed bytecode.
