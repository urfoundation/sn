#!/usr/bin/env bash
# Regenerate the STSubnet Go bindings from the exported forge ABI.
#
# Source of truth: sn/evm (Foundry). Pipeline:
#   cd evm && forge build
#   jq .abi out/STSubnet.sol/STSubnet.json > abi/STSubnet.abi.json
#   ./stabi/generate.sh          (write generated files)
#   ./stabi/generate.sh --check  (verify without modifying the checkout)
#
# Requires the exact abigen release matching go.mod:
#   go install github.com/ethereum/go-ethereum/cmd/abigen@v1.17.0
set -euo pipefail

cd "$(dirname "$0")"

mode="${1:---write}"
if [[ $# -gt 0 ]]; then shift; fi
if [[ "$mode" != "--write" && "$mode" != "--check" && "$mode" != "--preflight" ]]; then
    echo "usage: $0 [--write|--check|--preflight] [--artifacts /absolute/forge/out]" >&2
    exit 2
fi

# A gate checks only its own completed full compilation graph. An explicit
# missing input cannot fall back to another invocation's canonical out tree.
artifact_root="../evm/out"
if [[ $# -ne 0 ]]; then
    if [[ $# -ne 2 || "$1" != "--artifacts" || "$mode" != "--check" ]]; then
        echo "--artifacts requires --check and one absolute directory" >&2
        exit 2
    fi
    artifact_root="$2"
    if [[ "$artifact_root" != /* || ! -d "$artifact_root" || -L "$artifact_root" ]] ||
        [[ "$(cd -- "$artifact_root" && pwd -P)" != "$artifact_root" ]]; then
        echo "explicit artifact directory is not an absolute physical directory: $artifact_root" >&2
        exit 1
    fi
fi

expected_go_ethereum_version="v1.17.0"
expected_abigen_output="abigen version 1.17.0-stable"
go_ethereum_version=""
while read -r first second third _; do
    if [[ "$first" == "github.com/ethereum/go-ethereum" ]]; then
        go_ethereum_version="$second"
    elif [[ "$first" == "require" && "$second" == "github.com/ethereum/go-ethereum" ]]; then
        go_ethereum_version="$third"
    fi
done < ../go.mod
if [[ "$go_ethereum_version" != "$expected_go_ethereum_version" ]]; then
    echo "go.mod requires go-ethereum ${go_ethereum_version:-<missing>}; stabi generation requires $expected_go_ethereum_version" >&2
    echo "update the pinned generator version and regenerate the bindings together" >&2
    exit 1
fi

abigen=""
if [[ -n "${ABIGEN:-}" ]]; then
    if ! abigen="$(command -v "$ABIGEN" 2>/dev/null)"; then
        echo "ABIGEN does not name an executable: $ABIGEN" >&2
        exit 1
    fi
elif abigen="$(command -v abigen 2>/dev/null)"; then
    :
elif command -v go >/dev/null 2>&1; then
    go_bin="$(go env GOBIN)"
    if [[ -n "$go_bin" && -x "$go_bin/abigen" ]]; then
        abigen="$go_bin/abigen"
    else
        go_path="$(go env GOPATH)"
        first_go_path="${go_path%%:*}"
        if [[ -n "$first_go_path" && -x "$first_go_path/bin/abigen" ]]; then
            abigen="$first_go_path/bin/abigen"
        fi
    fi
fi
if [[ -z "$abigen" ]]; then
    echo "required tool '$expected_abigen_output' was not found in ABIGEN, PATH, GOBIN, or the first GOPATH/bin" >&2
    echo "install it with: go install github.com/ethereum/go-ethereum/cmd/abigen@$expected_go_ethereum_version" >&2
    exit 1
fi

if ! actual_abigen_output="$("$abigen" --version 2>&1)"; then
    echo "could not read abigen version from $abigen: $actual_abigen_output" >&2
    exit 1
fi
if [[ "$actual_abigen_output" != "$expected_abigen_output" ]]; then
    echo "wrong abigen version at $abigen: got '$actual_abigen_output', want '$expected_abigen_output'" >&2
    echo "install it with: go install github.com/ethereum/go-ethereum/cmd/abigen@$expected_go_ethereum_version" >&2
    exit 1
fi
if [[ "$mode" == "--preflight" ]]; then
    printf '%s\n' "$actual_abigen_output"
    exit 0
fi

temporary_dir="$(mktemp -d)"
trap 'rm -rf -- "$temporary_dir"' EXIT

jq -c .abi "$artifact_root/STSubnet.sol/STSubnet.json" > "$temporary_dir/STSubnet.abi.json"
jq -c .abi "$artifact_root/STCoordinator.sol/STCoordinator.json" > "$temporary_dir/STCoordinator.abi.json"
jq -c .abi "$artifact_root/STSettlementVault.sol/STSettlementVault.json" > "$temporary_dir/STSettlementVault.abi.json"
jq -c .abi "$artifact_root/STReserveSink.sol/STReserveSink.json" > "$temporary_dir/STReserveSink.abi.json"
jq -c .abi "$artifact_root/STValidatorEvidence.sol/STValidatorEvidence.json" > "$temporary_dir/STValidatorEvidence.abi.json"

"$abigen" --v2 \
    --abi "$temporary_dir/STSubnet.abi.json" \
    --pkg stabi \
    --type STSubnet \
    --out "$temporary_dir/stsubnet.go"

"$abigen" --v2 \
    --abi "$temporary_dir/STCoordinator.abi.json" \
    --pkg stabi \
    --type STCoordinator \
    --out "$temporary_dir/stcoordinator.go"

"$abigen" --v2 \
    --abi "$temporary_dir/STSettlementVault.abi.json" \
    --pkg stabi \
    --type STSettlementVault \
    --out "$temporary_dir/stsettlementvault.go"

"$abigen" --v2 \
    --abi "$temporary_dir/STReserveSink.abi.json" \
    --pkg stabi \
    --type STReserveSink \
    --out "$temporary_dir/streservesink.go"

"$abigen" --v2 \
    --abi "$temporary_dir/STValidatorEvidence.abi.json" \
    --pkg stabi \
    --type STValidatorEvidence \
    --out "$temporary_dir/stvalidatorevidence.go"

generated=(
    "STSubnet.abi.json:../evm/abi/STSubnet.abi.json"
    "STCoordinator.abi.json:../evm/abi/STCoordinator.abi.json"
    "STSettlementVault.abi.json:../evm/abi/STSettlementVault.abi.json"
    "STReserveSink.abi.json:../evm/abi/STReserveSink.abi.json"
    "STValidatorEvidence.abi.json:../evm/abi/STValidatorEvidence.abi.json"
    "stsubnet.go:stsubnet.go"
    "stcoordinator.go:stcoordinator.go"
    "stsettlementvault.go:stsettlementvault.go"
    "streservesink.go:streservesink.go"
    "stvalidatorevidence.go:stvalidatorevidence.go"
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
# coherent with the legacy STSubnet binding; release clients use the four
# generated bindings above and sim-testnet embeds the reviewed bytecode.
