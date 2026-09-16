#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/causal/compile/prior_native_abi_gates/crv4
printf '%s\n' "$$" >"$capture/launcher.pid"
"$capture/command.sh" >"$capture/launcher.stdout" 2>"$capture/launcher.stderr"
rc=$?
printf '%s\n' "$rc" >"$capture/launcher.join.exit"
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/launcher.joined-at"
ps -p "$$" -o pid=,ppid=,sid=,pgid=,stat=,comm= >"$capture/launcher.process-before-exit" || true
exit "$rc"
