#!/usr/bin/env bash
set -euo pipefail
[[ "$#" -eq 0 ]] || exit 64
exec bash "/home/by/urnetwork/temp/sn-runtime-config-fixture-correction-20260915/terra-runtime/qualification-runtime-config-fixture-5a411d2/templates/test-sim-testnet-replay-owner.sh" "/home/by/urnetwork/temp/sn-runtime-config-fixture-correction-20260915/terra-runtime/qualification-runtime-config-fixture-5a411d2/meta/config.env" "/home/by/urnetwork/temp/sn-runtime-config-fixture-correction-20260915/terra-runtime/qualification-runtime-config-fixture-5a411d2/race/p1/plan.env"
