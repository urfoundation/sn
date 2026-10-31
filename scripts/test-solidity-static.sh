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

# Slither's Foundry frontend runs `forge clean` for every analysis root. Keep
# those target-only compilation graphs below the already-ignored canonical
# out/cache directories so static analysis cannot delete or replace the full
# release artifacts consumed by the generated-payload freshness gate.
slither_foundry_out="${SLITHER_FOUNDRY_OUT:-out/slither}"
slither_foundry_cache="${SLITHER_FOUNDRY_CACHE_PATH:-cache/slither}"

cd "$evm_repo"
# STSubnet.sol is the retained pre-1.0 monolith and is not installed by
# sim-testnet. The coordinator root traverses immutable custody, proxy libraries,
# and runtime-precompile interfaces. The fleet batcher, disposable precompile
# probe, hostile governance-drill implementation, immutable validator evidence
# journal, settlement v2 coordinator and its phase C upgrade, immutable
# settlement v2 vault and its optional claim batcher are separate deployment
# roots analyzed independently.
# The settlement v2 ledger sum tree library is analyzed as a root of its own.
contracts=(
  src/STCoordinator.sol
  src/STFleetBatcher.sol
  src/STValidatorEvidence.sol
  src/probe/STSubnetProbe.sol
  src/testnet/STCoordinatorAdversary.sol
  src/LedgerTree.sol
  src/STSettlement2.sol
  src/STSettlement2PhaseC.sol
  src/STSettlementVault2.sol
  src/STClaimBatcher.sol
)
# Every root is analyzed even after another fails, so that one root's finding
# cannot hide the next root's; any failure still fails the gate at the end.
# An accepted finding carries a slither-disable-next-line comment in its
# source, except in a release-locked contract, whose source cannot change.
# Slither's triage database, slither.db.json, accepts those by finding id.
# Its one entry is locked-ether in STSubnetProbe, whose bytecode
# sim-testnet/contracts_gen.go pins: receive and seedFromTao fund the probe's
# mapped account, and the staking precompile's addStake debits that account
# natively, an outflow Slither cannot see. The id hashes the contract name and
# its payable function signatures, so any change to them fails the gate again.
failed=()
for contract in "${contracts[@]}"; do
  rm -f -- "$report"
  if ! FOUNDRY_OUT="$slither_foundry_out" FOUNDRY_CACHE_PATH="$slither_foundry_cache" "$slither_bin" "$contract" \
    --solc "$solc_bin" \
    --filter-paths 'lib|test|script' \
    --exclude-low \
    --exclude-informational \
    --disable-color \
    --triage-database slither.db.json \
    --json "$report"; then
    if [[ -s "$report" ]]; then
      jq -r '.results.detectors[]? | "[\(.impact)] \(.check): \(.description)"' "$report" >&2
    fi
    failed+=("$contract")
    continue
  fi

  if [[ "$(jq '[.results.detectors[]? | select(.impact == "High" or .impact == "Medium")] | length' "$report")" != "0" ]]; then
    echo "Slither emitted a high/medium finding despite a successful exit for $contract" >&2
    failed+=("$contract")
  fi
done

# The settlement v2 coordinator is upgradeable, so its storage layout is pinned
# (VERSION2-FINAL.md §15.4, S-9). The snapshot drops only compiler AST ids.
# Like Slither, the check compiles in the Slither tree, never the release one.
if ! FOUNDRY_OUT="$slither_foundry_out" FOUNDRY_CACHE_PATH="$slither_foundry_cache" \
  forge inspect STSettlement2 storageLayout --json | jq -S 'del(.storage[].astId)' |
  diff -u layout/STSettlement2.json -; then
  echo "STSettlement2 storage layout differs from layout/STSettlement2.json" >&2
  failed+=(layout/STSettlement2.json)
fi

# The phase C upgrade (VERSION2-FINAL.md §16) is pinned the same way, with the
# compiler's ids also dropped from its struct's members and type names. It must
# keep every STSettlement2 entry in place and declare its own only past the
# end of STSettlement2's gap. jq 1.6 takes no keyword, such as label, as an
# object key shorthand.
phase_c_filter='def strip: if type == "string" then gsub("t_struct\\((?<n>[A-Za-z0-9_]+)\\)[0-9]+_storage"; "t_struct(\(.n))_storage") else . end;
  def walkall: if type == "object" then with_entries(.key |= strip | .value |= walkall) elif type == "array" then map(walkall) else strip end;
  del(.storage[].astId, .types[].members[]?.astId) | walkall'
phase_c_layout="$(FOUNDRY_OUT="$slither_foundry_out" FOUNDRY_CACHE_PATH="$slither_foundry_cache" \
  forge inspect STSettlement2PhaseC storageLayout --json | jq -S "$phase_c_filter")" || phase_c_layout=""
if ! diff -u layout/STSettlement2PhaseC.json - <<<"$phase_c_layout"; then
  echo "STSettlement2PhaseC storage layout differs from layout/STSettlement2PhaseC.json" >&2
  failed+=(layout/STSettlement2PhaseC.json)
elif ! jq -e --slurpfile base layout/STSettlement2.json '
  ($base[0].storage | map({"label": .label, "slot": .slot, "offset": .offset, "type": .type})) as $kept
  | ($base[0].storage[-1] | (.slot | tonumber) + (($base[0].types[.type].numberOfBytes | tonumber) / 32)) as $free
  | (.storage | map({"label": .label, "slot": .slot, "offset": .offset, "type": .type})) as $all
  | $all[0:($kept | length)] == $kept and ([$all[($kept | length):][] | (.slot | tonumber) >= $free] | all)' \
  <<<"$phase_c_layout" >/dev/null; then
  echo "STSettlement2PhaseC storage layout does not extend STSettlement2's past its gap" >&2
  failed+=(layout/STSettlement2PhaseC.json)
fi

if (( ${#failed[@]} != 0 )); then
  echo "release Solidity static gate failed for: ${failed[*]}" >&2
  exit 1
fi
echo "release Solidity static gate passed for all ${#contracts[@]} deployable roots (Slither 0.11.6; no high/medium findings; STSettlement2 and STSettlement2PhaseC storage layouts pinned)"
