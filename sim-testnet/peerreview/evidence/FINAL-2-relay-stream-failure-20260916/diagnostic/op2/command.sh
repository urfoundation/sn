#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/mnt/data/sn-testnet/qualification/native-recovery-20260916-r2/relay-artifact-read-20260916-r1/op2
source=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn
server=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/server
expected_sn=541e13cfbe968704fb74c4001506853d2529bfdd
expected_server=0f095a639e111f71d231cd6f792a191cbc6b0a69
hash=0x74d265dccdbcee7a218e229abd199708e15f0035481cc5ec8d0b55f2bb5ed273
url='http://127.0.0.1:18082/sn/attempt-artifact?kind=metadata&hash=0x74d265dccdbcee7a218e229abd199708e15f0035481cc5ec8d0b55f2bb5ed273'
status=125
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/started-at"
printf '%s\n' "$$" >"$capture/outer.pid"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"$capture/outer.process-at-start"
printf '%s\n' "$url" >"$capture/url.txt"
printf '%s\n' 'curl --silent --show-error --connect-timeout 3 --max-time 12 --dump-header headers --output body --write-out writeout GET exact typed artifact URL' >"$capture/command.txt"
sha256sum "$capture/command.sh" >"$capture/command.sha256"
git -C "$source" rev-parse HEAD >"$capture/sn.commit.before"
git -C "$source" status --porcelain=v1 --untracked-files=all >"$capture/sn.status.before"
git -C "$server" rev-parse HEAD >"$capture/server.commit.before"
git -C "$server" status --porcelain=v1 --untracked-files=all >"$capture/server.status.before"
if [ "$(cat "$capture/sn.commit.before")" = "$expected_sn" ] && [ ! -s "$capture/sn.status.before" ] && [ "$(cat "$capture/server.commit.before")" = "$expected_server" ] && [ ! -s "$capture/server.status.before" ]; then
 printf '%s\n' true >"$capture/preflight.ok"
 date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/fetch.started-at"
 curl --silent --show-error --connect-timeout 3 --max-time 12 --dump-header "$capture/headers.txt" --output "$capture/body.bin" --write-out 'http_code=%{http_code}\ncontent_type=%{content_type}\nsize_download=%{size_download}\ntime_connect=%{time_connect}\ntime_starttransfer=%{time_starttransfer}\ntime_total=%{time_total}\nremote_ip=%{remote_ip}\nurl_effective=%{url_effective}\n' "$url" >"$capture/curl.writeout" 2>"$capture/curl.stderr"
 status=$?
 date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/fetch.finished-at"
else
 printf '%s\n' false >"$capture/preflight.ok"
 printf '%s\n' 'source fence refused diagnostic fetch' >"$capture/curl.stderr"
fi
printf '%s\n' "$status" >"$capture/curl.exit"
if [ -f "$capture/body.bin" ]; then wc -c <"$capture/body.bin" >"$capture/body.bytes"; sha256sum "$capture/body.bin" >"$capture/body.sha256"; fi
git -C "$source" rev-parse HEAD >"$capture/sn.commit.after"
git -C "$source" status --porcelain=v1 --untracked-files=all >"$capture/sn.status.after"
git -C "$server" rev-parse HEAD >"$capture/server.commit.after"
git -C "$server" status --porcelain=v1 --untracked-files=all >"$capture/server.status.after"
if cmp -s "$capture/sn.commit.before" "$capture/sn.commit.after" && cmp -s "$capture/sn.status.before" "$capture/sn.status.after" && cmp -s "$capture/server.commit.before" "$capture/server.commit.after" && cmp -s "$capture/server.status.before" "$capture/server.status.after"; then printf '%s\n' true >"$capture/fences.after"; else printf '%s\n' false >"$capture/fences.after"; fi
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/finished-at"
printf '%s\n' "$status" >"$capture/outer.exit"
exit "$status"
