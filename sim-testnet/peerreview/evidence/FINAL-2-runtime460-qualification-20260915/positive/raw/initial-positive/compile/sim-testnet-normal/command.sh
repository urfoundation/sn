#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/mnt/data/sn-testnet/qualification/runtime460-affected-matrix-20260915-r1/terra/compile/sim-testnet-normal
source=/mnt/data/sn-testnet/qualification/runtime460-20260915-r1/workspace/sn
selector=/mnt/data/sn-testnet/qualification/runtime460-affected-matrix-20260915-r1/terra/selectors/sim-testnet.selector
expected=0127b7ee08d1cd77262a6dd28b606e2cfee33e6b
binary="/mnt/data/sn-testnet/qualification/runtime460-affected-matrix-20260915-r1/terra/compile/sim-testnet-normal/sim-testnet.normal.test"
status=125
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.started-at"
printf '%s\n' "$$" >"$capture/outer.pid"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"$capture/outer.process-at-start"
printf '%s\n' "$(ps -o sid= -p "$$" | tr -d ' ')" >"$capture/outer.sid"
printf '%s\n' 'env GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GOWORK=off GOENV=off GOTOOLCHAIN=local GOVCS=*:off GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR=private GOTMPDIR=private timeout --foreground --kill-after=15s 600s go test -c -p=1 -o sim-testnet.normal.test ./sim-testnet' >"$capture/command.txt"
sha256sum "$capture/command.sh" >"$capture/command.sha256"
sha256sum "$selector" >"$capture/selector.before.sha256"
git -C "$source" rev-parse HEAD >"$capture/source.commit.before"
git -C "$source" status --porcelain >"$capture/source.status.before"
if [ "$(cat "$capture/source.commit.before")" = "$expected" ] && [ ! -s "$capture/source.status.before" ]; then
  printf '%s\n' true >"$capture/source.fence.before"
  printf '%s\n' true >"$capture/build.started"
  ( cd "$source" && env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 600s go test -c -p=1 -o "$binary" ./sim-testnet ) >"$capture/build.stdout" 2>"$capture/build.stderr"
  status=$?
else
  printf '%s\n' false >"$capture/source.fence.before"
  printf '%s\n' false >"$capture/build.started"
  printf '%s\n' 'source commit or cleanliness fence refused compilation' >"$capture/build.stderr"
fi
printf '%s\n' "$status" >"$capture/build.exit"
git -C "$source" rev-parse HEAD >"$capture/source.commit.after"
git -C "$source" status --porcelain >"$capture/source.status.after"
sha256sum "$selector" >"$capture/selector.after.sha256"
if cmp -s "$capture/source.commit.before" "$capture/source.commit.after" && cmp -s "$capture/source.status.before" "$capture/source.status.after" && cmp -s "$capture/selector.before.sha256" "$capture/selector.after.sha256"; then printf '%s\n' true >"$capture/source.fence.after"; else printf '%s\n' false >"$capture/source.fence.after"; if [ "$status" -eq 0 ]; then status=126; fi; fi
if [ -f "$binary" ]; then sha256sum "$binary" >"$capture/binary.sha256"; fi
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.finished-at"
printf '%s\n' "$status" >"$capture/outer.exit"
exit "$status"
