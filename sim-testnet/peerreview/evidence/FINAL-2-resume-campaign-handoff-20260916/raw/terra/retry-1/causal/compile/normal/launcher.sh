#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/terra/retry-1/causal/compile/normal'
[[ ! -e "$capture/outer.started-at" && ! -e "$capture/outer.exit" ]] || exit 125
printf '%s\n' "$$" > "$capture/outer.pid"
awk '{print $22}' /proc/$$/stat > "$capture/outer.startticks"
date -u +%Y-%m-%dT%H:%M:%SZ > "$capture/outer.started-at"
set +e
"$capture/command.sh" > "$capture/build.stdout" 2> "$capture/build.stderr"
rc=$?
set -e
printf '%s\n' "$rc" > "$capture/build.exit"
printf '%s\n' "$rc" > "$capture/outer.exit"
date -u +%Y-%m-%dT%H:%M:%SZ > "$capture/outer.finished-at"
printf '%s\n' "$rc" > "$capture/launcher.join.exit"
date -u +%Y-%m-%dT%H:%M:%SZ > "$capture/launcher.joined-at"
exit "$rc"
