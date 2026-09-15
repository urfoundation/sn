#!/usr/bin/env bash
set -euo pipefail
[[ "$#" -eq 0 ]] || exit 64
exec bash "/home/by/urnetwork/temp/sn-historical-lock-admission-correction-20260915/terra-runtime/qualification-historical-lock-3ffc1277/reviewed-owner/test-sim-testnet-owner.sh" "/home/by/urnetwork/temp/sn-historical-lock-admission-correction-20260915/terra-runtime/qualification-historical-lock-3ffc1277/meta/config.env" "/home/by/urnetwork/temp/sn-historical-lock-admission-correction-20260915/terra-runtime/qualification-historical-lock-3ffc1277/normal/confirmation-sim-testnet-p3/plan.env"
