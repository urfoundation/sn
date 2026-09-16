#!/usr/bin/env bash
set +e
"/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/causal/unfiltered-claims-taskworker/compile/normal/command.sh"
rc=$?
printf '%s\n' "$rc" >"/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/causal/unfiltered-claims-taskworker/compile/normal/launcher.join.exit"
date -u +'%Y-%m-%dT%H:%M:%SZ' >"/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/causal/unfiltered-claims-taskworker/compile/normal/launcher.joined-at"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/causal/unfiltered-claims-taskworker/compile/normal/launcher.process-after" 2>/dev/null || true
exit "$rc"
