#!/usr/bin/env bash
set -euo pipefail
[[ "$#" -eq 0 ]] || exit 64
exec bash "/home/by/urnetwork/temp/sn-runtime-config-identity-correction-20260915/terra-runtime/qualification-runtime-config-identity-85c0958/templates/test-sim-testnet-descendant-replay-owner.sh" "/home/by/urnetwork/temp/sn-runtime-config-identity-correction-20260915/terra-runtime/qualification-runtime-config-identity-85c0958/meta/config.env" "/home/by/urnetwork/temp/sn-runtime-config-identity-correction-20260915/terra-runtime/qualification-runtime-config-identity-85c0958/normal/migration-sim-testnet/plan.env"
