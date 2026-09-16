#!/usr/bin/env bash
set +e
"/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/normal/compile/command.sh"
rc=$?
printf '%s\n' "$rc" >"/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/normal/compile/launcher.join.exit"
date -u +'%Y-%m-%dT%H:%M:%SZ' >"/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/normal/compile/launcher.joined-at"
exit "$rc"
