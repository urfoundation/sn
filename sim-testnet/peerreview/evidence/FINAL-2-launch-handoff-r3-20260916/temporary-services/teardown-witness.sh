#!/usr/bin/env bash
set -euo pipefail
umask 077

# This script is intentionally inert unless the caller provides the explicit
# authorization token below. It witnesses shutdown of the four isolated
# r3 helpers. Their transient launch parent has exited, so a service wait(2)
# exit status cannot exist and is never synthesized here.
if [ "${AUTHORIZE_HELPER_TEARDOWN:-}" != "r3-history-closed" ]; then
  printf '%s\n' 'refusing teardown: set AUTHORIZE_HELPER_TEARDOWN=r3-history-closed after root authorization' >&2
  exit 64
fi

root=/mnt/data/sn-testnet/qualification/native-recovery-20260916-r3
ts="$root/temporary-services"
status="$ts/OWNER-STATUS.json"
out="$ts/teardown-witness"
timeout=${TEARDOWN_TIMEOUT_SECONDS:-60}
mkdir -p "$out"
chmod 0700 "$out"

if ! jq -e '.terminal_exit_status_available == false' "$status" >/dev/null; then
  printf '%s\n' 'owner status does not declare non-waitable helpers' >&2
  exit 65
fi

started=$(date -u +%FT%TZ)
printf '%s\n' "$started" >"$out/started-at"
sha256sum "$status" >"$out/owner-status.sha256"
printf 'clock_ticks=%s\n' "$(getconf CLK_TCK)" >"$out/clock.txt"

# Bind every intended target before sending any signal. These helpers were
# started in their own PGID/SID, so signaling the exact PGID remains confined
# to that helper's temporary session.
: >"$out/pre-signal.tsv"
while IFS=$'\t' read -r name pid expected_ticks expected_pgid expected_sid; do
  test -r "/proc/$pid/stat"
  actual_ticks=$(awk '{print $22}' "/proc/$pid/stat")
  actual_ppid=$(awk '{print $4}' "/proc/$pid/stat")
  actual_pgid=$(ps -o pgid= -p "$pid" | tr -d ' ')
  actual_sid=$(ps -o sid= -p "$pid" | tr -d ' ')
  kill -0 "$pid"
  if [ "$actual_ticks" != "$expected_ticks" ] || [ "$actual_pgid" != "$expected_pgid" ] || [ "$actual_sid" != "$expected_sid" ]; then
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\tidentity-mismatch\n' "$name" "$pid" "$expected_ticks" "$actual_ticks" "$expected_pgid" "$actual_pgid" "$actual_sid" >>"$out/pre-signal.tsv"
    exit 66
  fi
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\tbound\n' "$name" "$pid" "$expected_ticks" "$actual_ticks" "$actual_ppid" "$actual_pgid" "$actual_sid" >>"$out/pre-signal.tsv"
done < <(jq -r '.services[] | [.name,.pid,.startticks,.pgid,.sid] | @tsv' "$status")

# Snapshot only owned local listeners before signaling.
ss -ltn '( sport = :18081 or sport = :18082 or sport = :19944 or sport = :19945 or sport = :19948 or sport = :19949 )' >"$out/listeners.before.txt"

: >"$out/signal-dispatch.tsv"
while IFS=$'\t' read -r name pid expected_ticks pgid sid; do
  dispatched=$(date -u +%FT%TZ)
  if kill -TERM -- "-$pgid"; then rc=0; else rc=$?; fi
  printf '%s\t%s\t%s\t%s\tTERM-group\t%s\n' "$dispatched" "$name" "$pid" "$expected_ticks" "$rc" >>"$out/signal-dispatch.tsv"
  if [ "$rc" -ne 0 ]; then
    printf '%s\n' 'signal dispatch failed; no escalation was attempted' >&2
    exit 67
  fi
done < <(jq -r '.services[] | [.name,.pid,.startticks,.pgid,.sid] | @tsv' "$status")

: >"$out/termination-observation.tsv"
verified=1
for attempt in $(seq 0 "$timeout"); do
  all_absent=1
  while IFS=$'\t' read -r name pid expected_ticks pgid sid; do
    observed=$(date -u +%FT%TZ)
    if [ -r "/proc/$pid/stat" ]; then
      actual_ticks=$(awk '{print $22}' "/proc/$pid/stat")
      if [ "$actual_ticks" = "$expected_ticks" ]; then state=still-original; else state=pid-reused-or-identity-changed; fi
      all_absent=0
    else
      state=proc-absent
    fi
    if kill -0 "$pid" 2>/dev/null; then k0=present; else k0=absent; fi
    printf '%s\t%s\t%s\t%s\t%s\n' "$observed" "$name" "$pid" "$state" "$k0" >>"$out/termination-observation.tsv"
  done < <(jq -r '.services[] | [.name,.pid,.startticks,.pgid,.sid] | @tsv' "$status")
  if [ "$all_absent" -eq 1 ]; then
    verified=0
    break
  fi
  sleep 1
done

ss -ltn '( sport = :18081 or sport = :18082 or sport = :19944 or sport = :19945 or sport = :19948 or sport = :19949 )' >"$out/listeners.after.txt"
listeners_after=$(wc -l <"$out/listeners.after.txt")
# ss always prints a header, so exactly one line means no listener remains.
if [ "$verified" -eq 0 ] && [ "$listeners_after" -eq 1 ]; then
  result=terminated-observed
  code=0
else
  result=teardown-incomplete-no-escalation
  code=68
fi
jq -n \
  --arg started_at "$started" \
  --arg completed_at "$(date -u +%FT%TZ)" \
  --arg result "$result" \
  --arg terminal_exit_status 'unavailable: helpers were orphaned after their transient launcher exited' \
  --argjson listener_lines_after "$listeners_after" \
  --argjson timeout_seconds "$timeout" \
  '{started_at:$started_at,completed_at:$completed_at,result:$result,terminal_exit_status:$terminal_exit_status,listener_lines_after:$listener_lines_after,timeout_seconds:$timeout_seconds}' >"$out/RESULT.json"
find "$out" -maxdepth 1 -type f -print0 | sort -z | xargs -0 sha256sum >"$out/SHA256SUMS"
exit "$code"
