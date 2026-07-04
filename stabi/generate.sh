#!/usr/bin/env bash
# Regenerate the STSubnet Go bindings from the exported forge ABI.
#
# Source of truth: sn/evm (Foundry). Pipeline:
#   cd evm && forge build
#   jq .abi out/STSubnet.sol/STSubnet.json > abi/STSubnet.abi.json
#   ./stabi/generate.sh          (this script; or `go generate ./stabi/...`)
#
# Requires abigen matching go.mod's go-ethereum version:
#   go install github.com/ethereum/go-ethereum/cmd/abigen@v1.16.7
set -euo pipefail

cd "$(dirname "$0")"

ABIGEN="${ABIGEN:-abigen}"

"$ABIGEN" --v2 \
    --abi ../evm/abi/STSubnet.abi.json \
    --pkg stabi \
    --type STSubnet \
    --out stsubnet.go

# stctl ships the same ABI json; it is generated output, kept in sync here.
cp ../evm/abi/STSubnet.abi.json ../stctl/st_abi.json
