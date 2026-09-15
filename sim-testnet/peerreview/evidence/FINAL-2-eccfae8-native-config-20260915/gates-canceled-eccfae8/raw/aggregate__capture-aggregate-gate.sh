#!/usr/bin/env bash
set -u -o pipefail
umask 077
runtime='/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/terra-runtime/aggregate-eccfae8-canonical-r1'
capture_root="$runtime/capture"
command_file="$runtime/commands/aggregate-gate.command"
cache_record="$runtime/meta/gomodcache.path"
: "${FINAL_WORKSPACE:?FINAL_WORKSPACE is required}" "${SOURCE_PAIR:?SOURCE_PAIR is required}" "${EXPECTED_SN:?EXPECTED_SN is required}" "${GOMODCACHE:?GOMODCACHE is required}"
for path in "$runtime" "$runtime/tmp" "$capture_root" "$command_file" "$0" "$SOURCE_PAIR" "$cache_record"; do [[ -e "$path" && ! -L "$path" ]] || exit 125; done
for path in "$runtime" "$runtime/tmp" "$capture_root"; do [[ "$(stat -c '%a' "$path")" == 700 ]] || exit 125; done
[[ -f "$SOURCE_PAIR" && "$(stat -c '%a' "$SOURCE_PAIR")" == 600 ]] || exit 125
[[ -f "$cache_record" && "$(stat -c '%a' "$cache_record")" == 600 && "$(<"$cache_record")" == "$GOMODCACHE" ]] || exit 125
for artifact in outer.stdout outer.stderr outer.pid outer.exit outer.exit.pending outer.started-at outer.finished-at launcher.sha256 launcher.stat; do [[ ! -e "$capture_root/$artifact" && ! -L "$capture_root/$artifact" ]] || exit 125; done
printf '%s\n' "$$" > "$capture_root/outer.pid"
date -u +%Y-%m-%dT%H:%M:%SZ > "$capture_root/outer.started-at"
sha256sum "$command_file" "$0" "$SOURCE_PAIR" "$cache_record" > "$capture_root/launcher.sha256"
stat -c '%a %s %i %n' "$command_file" "$0" "$SOURCE_PAIR" "$cache_record" > "$capture_root/launcher.stat"
set +e
bash "$command_file" > "$capture_root/outer.stdout" 2> "$capture_root/outer.stderr"
status=$?
set -e
printf '%s\n' "$status" > "$capture_root/outer.exit.pending"
mv -- "$capture_root/outer.exit.pending" "$capture_root/outer.exit"
date -u +%Y-%m-%dT%H:%M:%SZ > "$capture_root/outer.finished-at"
exit "$status"
