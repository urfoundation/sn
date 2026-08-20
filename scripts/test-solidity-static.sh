#!/usr/bin/env bash
set -euo pipefail

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
evm_repo="$sn_repo/evm"
slither_bin="${SLITHER_BIN:-slither}"
account_home="$(getent passwd "$(id -u)" | cut -d: -f6)"

if ! command -v forge >/dev/null 2>&1 && [[ -x "$account_home/.foundry/bin/forge" ]]; then
  export PATH="$account_home/.foundry/bin:$PATH"
fi
if ! command -v forge >/dev/null 2>&1; then
  echo "Foundry is required by Slither's pinned compilation frontend" >&2
  exit 1
fi

if ! command -v "$slither_bin" >/dev/null 2>&1; then
  echo "Slither 0.11.6 is required; install scripts/security-requirements.txt in an isolated Python environment" >&2
  exit 1
fi
if [[ "$($slither_bin --version 2>&1)" != "0.11.6" ]]; then
  echo "release gate requires Slither 0.11.6" >&2
  exit 1
fi

solc_bin="${SOLC_BIN:-}"
if [[ -z "$solc_bin" ]]; then
  if command -v solc >/dev/null 2>&1; then
    solc_bin="$(command -v solc)"
  else
    solc_bin="$account_home/.svm/0.8.24/solc-0.8.24"
  fi
fi
if [[ ! -x "$solc_bin" ]]; then
  echo "solc 0.8.24 was not found; run forge build once or set SOLC_BIN" >&2
  exit 1
fi
if ! "$solc_bin" --version | grep -q 'Version: 0.8.24'; then
  echo "release gate requires solc 0.8.24" >&2
  exit 1
fi

report="$(mktemp)"
rm -f -- "$report"
trap 'rm -f -- "$report"' EXIT

cd "$evm_repo"
# STSubnet.sol is the retained pre-1.0 monolith and is not installed by
# sim-testnet. Starting at STCoordinator traverses the complete deployable
# release graph: coordinator, immutable vault, reserve sink, proxy libraries,
# and every runtime-precompile interface they import.
if ! "$slither_bin" src/STCoordinator.sol \
  --solc "$solc_bin" \
  --filter-paths 'lib|test|script' \
  --exclude-low \
  --exclude-informational \
  --disable-color \
  --json "$report"; then
  if [[ -s "$report" ]]; then
    jq -r '.results.detectors[]? | "[\(.impact)] \(.check): \(.description)"' "$report" >&2
  fi
  exit 1
fi

if [[ "$(jq '[.results.detectors[]? | select(.impact == "High" or .impact == "Medium")] | length' "$report")" != "0" ]]; then
  echo "Slither emitted a high/medium finding despite a successful exit" >&2
  exit 1
fi

echo "release Solidity static gate passed (Slither 0.11.6; no high/medium findings)"
