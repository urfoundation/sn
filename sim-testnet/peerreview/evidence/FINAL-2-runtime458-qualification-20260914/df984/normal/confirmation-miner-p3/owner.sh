#!/usr/bin/env bash
set -euo pipefail
[[ "$#" -eq 0 ]] || exit 64
exec bash "/home/by/urnetwork/temp/sn-runtime458-history-capacity-20260914/terra-runtime/qualification-runtime458-df98472/scripts/test-miner-owner.sh" "/home/by/urnetwork/temp/sn-runtime458-history-capacity-20260914/terra-runtime/qualification-runtime458-df98472/meta/config.env" "/home/by/urnetwork/temp/sn-runtime458-history-capacity-20260914/terra-runtime/qualification-runtime458-df98472/normal/confirmation-miner-p3/plan.env"
