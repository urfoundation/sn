#!/usr/bin/env bash
set +e
EXPECTED_SN='aeda6abbd2dc0abc92bb0f60975cf89b509e8017' "/mnt/data/sn-testnet/qualification/pf01-pf02-physical-cli-final-lock-20260916-r1/capture-final-cli-build.sh"
rc=$?
printf '%s\n' "$rc" >"/mnt/data/sn-testnet/qualification/pf01-pf02-physical-cli-final-lock-20260916-r1/capture/launcher.join.exit"
date -u +'%Y-%m-%dT%H:%M:%SZ' >"/mnt/data/sn-testnet/qualification/pf01-pf02-physical-cli-final-lock-20260916-r1/capture/launcher.joined-at"
exit "$rc"
