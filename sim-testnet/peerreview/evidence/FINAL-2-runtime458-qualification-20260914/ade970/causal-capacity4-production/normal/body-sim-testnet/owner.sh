#!/usr/bin/env bash
set -euo pipefail
umask 077
stage=/home/by/urnetwork/temp/sn-runtime458-gate-fixture-correction-20260914/terra-runtime/qualification-runtime458-ade970a/causal-capacity4-production/normal/body-sim-testnet
date -u +%Y-%m-%dT%H:%M:%S.%NZ > "$stage/outer.started-at"
set +e
/home/by/urnetwork/temp/sn-runtime458-gate-fixture-correction-20260914/terra-runtime/qualification-runtime458-ade970a/causal-capacity4-production/run-mutant-package-causal.sh "$stage/plan.env" > "$stage/outer.stdout" 2> "$stage/outer.stderr"
status=$?
set -e
printf '%s\n' "$status" > "$stage/outer.exit"
date -u +%Y-%m-%dT%H:%M:%S.%NZ > "$stage/outer.finished-at"
exit "$status"
