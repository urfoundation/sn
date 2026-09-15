#!/usr/bin/env bash
set -euo pipefail
[[ "$#" -eq 0 ]] || exit 64
exec bash "/home/by/urnetwork/temp/sn-runtime458-gate-fixture-correction-20260914/terra-runtime/qualification-runtime458-ade970a/scripts/compile-sim-testnet-owner.sh" "/home/by/urnetwork/temp/sn-runtime458-gate-fixture-correction-20260914/terra-runtime/qualification-runtime458-ade970a/meta/config.env" "normal"
