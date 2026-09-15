#!/usr/bin/env bash
set -u -o pipefail
umask 077
runtime=/home/by/urnetwork/temp/sn-runtime-config-identity-correction-20260915/terra-runtime/renderer-cli-runtime-config-identity-readonly-prepared
body="$runtime/commands/renderer-cli-build.command"
pair="$runtime/meta/source-pair-renderer.tsv"
for path in "$runtime" "$runtime/commands" "$runtime/capture" "$runtime/meta" "$runtime/logs" "$runtime/tmp" "$runtime/gotmp" "$body" "$0"; do
  test -e "$path" && test ! -L "$path" || exit 125
done
for artifact in outer.stdout outer.stderr outer.pid outer.exit outer.exit.pending outer.started-at outer.finished-at launcher.sha256 launcher.stat source-pair.sha256; do
  test ! -e "$runtime/capture/$artifact" && test ! -L "$runtime/capture/$artifact" || exit 125
done
printf '%s\n' "$$" > "$runtime/capture/outer.pid"
date -u +%Y-%m-%dT%H:%M:%SZ > "$runtime/capture/outer.started-at"
sha256sum "$body" "$0" > "$runtime/capture/launcher.sha256"
stat -c '%a %s %i %n' "$body" "$0" > "$runtime/capture/launcher.stat"
set +e
bash "$body" > "$runtime/capture/outer.stdout" 2> "$runtime/capture/outer.stderr"
status=$?
set -e
if test -f "$pair"; then sha256sum "$pair" > "$runtime/capture/source-pair.sha256"; fi
printf '%s\n' "$status" > "$runtime/capture/outer.exit.pending"
mv -- "$runtime/capture/outer.exit.pending" "$runtime/capture/outer.exit"
date -u +%Y-%m-%dT%H:%M:%SZ > "$runtime/capture/outer.finished-at"
exit "$status"
