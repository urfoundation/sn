#!/usr/bin/env bash
set -u -o pipefail
umask 077
runtime='/mnt/data/sn-testnet/qualification/reserve-software-physical-cli-final-lock-20260916-r1'
body="$runtime/commands/final-cli-build.command"
source_pair="$runtime/meta/source-pair-final.tsv"
: "${EXPECTED_SN:?EXPECTED_SN is required}"
[[ "$EXPECTED_SN" =~ ^[0-9a-f]{40}$ ]] || exit 125
for path in "$runtime" "$runtime/commands" "$runtime/capture" "$runtime/meta" "$runtime/logs" "$runtime/tmp" "$runtime/gotmp" "$body" "$0" "$source_pair"; do
  [[ -e "$path" && ! -L "$path" ]] || exit 125
done
[[ "$(stat -c '%a' "$source_pair")" == 600 ]] || exit 125
for path in "$runtime" "$runtime/commands" "$runtime/capture" "$runtime/meta" "$runtime/logs" "$runtime/tmp" "$runtime/gotmp"; do
  [[ "$(stat -c '%a' "$path")" == 700 ]] || exit 125
done
for artifact in outer.stdout outer.stderr outer.pid outer.exit outer.exit.pending outer.started-at outer.finished-at launcher.sha256 launcher.stat source-pair.sha256; do
  [[ ! -e "$runtime/capture/$artifact" && ! -L "$runtime/capture/$artifact" ]] || exit 125
done
printf '%s\n' "$$" > "$runtime/capture/outer.pid"
date -u +%Y-%m-%dT%H:%M:%SZ > "$runtime/capture/outer.started-at"
sha256sum "$body" "$0" > "$runtime/capture/launcher.sha256"
stat -c '%a %s %i %n' "$body" "$0" > "$runtime/capture/launcher.stat"
sha256sum "$source_pair" > "$runtime/capture/source-pair.sha256"
set +e
bash "$body" > "$runtime/capture/outer.stdout" 2> "$runtime/capture/outer.stderr"
status=$?
set -e
printf '%s\n' "$status" > "$runtime/capture/outer.exit.pending"
mv -- "$runtime/capture/outer.exit.pending" "$runtime/capture/outer.exit"
date -u +%Y-%m-%dT%H:%M:%SZ > "$runtime/capture/outer.finished-at"
exit "$status"
