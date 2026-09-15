#!/usr/bin/env bash
set -euo pipefail
[[ "$#" -eq 0 ]] || exit 64
exec bash "/home/by/urnetwork/temp/sn-historical-lock-admission-correction-20260915/terra-runtime/qualification-historical-lock-3ffc1277/scripts/compile-sim-testnet-owner.sh" "/home/by/urnetwork/temp/sn-historical-lock-admission-correction-20260915/terra-runtime/qualification-historical-lock-3ffc1277/meta/config.env" "normal"
