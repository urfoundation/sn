#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/runtime461-renderer-cli
printf '%s\n' "$$" >"$capture/capture/launcher.pid"
"$capture/command.sh" >"$capture/capture/launcher.stdout" 2>"$capture/capture/launcher.stderr"
rc=$?
printf '%s\n' "$rc" >"$capture/capture/launcher.join.exit"
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/capture/launcher.joined-at"
ps -p "$$" -o pid=,ppid=,sid=,pgid=,stat=,comm= >"$capture/capture/launcher.process-before-exit" || true
exit "$rc"
