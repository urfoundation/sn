#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/terra/positive/normal/body
source=/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/workspace/sn
compiler=/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/terra/positive/compile/normal
binary="/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/terra/positive/compile/normal/sim-testnet.normal.test"
selector=/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/terra/selectors/positive.selector
expected_tests=/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/terra/selectors/positive.expected-tests.txt
expected=2ffbab2bb3b2251e3eab1d79f687fba50c2b57b5
status=125
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.started-at"
printf '%s\n' "$$" >"$capture/outer.pid"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"$capture/outer.process-at-start"
printf '%s\n' "$(ps -o sid= -p "$$" | tr -d ' ')" >"$capture/outer.sid"
printf '%s\n' 'env GOMAXPROCS=4 GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR=private GOTMPDIR=private timeout --foreground --kill-after=15s 600s go tool test2json -t sim-testnet.normal.test -test.v -test.run=exact-union-selector -test.count=1 -test.timeout=10m -test.parallel=4' >"$capture/command.txt"
sha256sum "$capture/command.sh" >"$capture/command.sha256"
sha256sum "$selector" "$expected_tests" >"$capture/selectors.before.sha256"
git -C "$source" rev-parse HEAD >"$capture/source.commit.before"
git -C "$source" status --porcelain >"$capture/source.status.before"
sha256sum "$binary" >"$capture/binary.before.sha256"
expected_binary=$(awk '{print $1}' "$compiler/binary.sha256")
actual_binary=$(awk '{print $1}' "$capture/binary.before.sha256")
if [ "$(cat "$capture/source.commit.before")" = "$expected" ] && [ ! -s "$capture/source.status.before" ] && [ "$expected_binary" = "$actual_binary" ] && [ -d "$source/sim-testnet" ]; then
  printf '%s\n' true >"$capture/preflight.ok"
  printf '%s\n' 'selected tests use checked synthetic fixtures and private temporary state from the package working directory' >"$capture/relative-fixtures.txt"
  printf '%s\n' true >"$capture/body.started"
  selector_value=$(cat "$selector")
  ( cd "$source/sim-testnet" && env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 600s go tool test2json -t "$binary" -test.v -test.run="$selector_value" -test.count=1 -test.timeout=10m -test.parallel=4 ) >"$capture/test2json.jsonl" 2>"$capture/test2json.stderr"
  status=$?
else
  printf '%s\n' false >"$capture/preflight.ok"
  printf '%s\n' false >"$capture/body.started"
  printf '%s\n' 'source, binary, selector, or package working-directory preflight refused body' >"$capture/test2json.stderr"
fi
printf '%s\n' "$status" >"$capture/body.exit"
if [ "$status" -eq 0 ]; then
  jq -r 'select(.Action == "pass" and .Test != null) | .Test' "$capture/test2json.jsonl" | sort -u >"$capture/observed.tests.txt" 2>"$capture/event-validation.stderr"
  jq -r 'select(.Action == "pass" and .Test == null) | .Package' "$capture/test2json.jsonl" >"$capture/package.pass.events.txt" 2>>"$capture/event-validation.stderr"
  if cmp -s "$expected_tests" "$capture/observed.tests.txt" && [ "$(wc -l <"$capture/package.pass.events.txt" | tr -d ' ')" = 1 ]; then printf '%s\n' true >"$capture/events.valid"; else printf '%s\n' false >"$capture/events.valid"; status=127; fi
fi
git -C "$source" rev-parse HEAD >"$capture/source.commit.after"
git -C "$source" status --porcelain >"$capture/source.status.after"
sha256sum "$selector" "$expected_tests" >"$capture/selectors.after.sha256"
sha256sum "$binary" >"$capture/binary.after.sha256"
if cmp -s "$capture/source.commit.before" "$capture/source.commit.after" && cmp -s "$capture/source.status.before" "$capture/source.status.after" && cmp -s "$capture/selectors.before.sha256" "$capture/selectors.after.sha256" && cmp -s "$capture/binary.before.sha256" "$capture/binary.after.sha256"; then printf '%s\n' true >"$capture/fences.after"; else printf '%s\n' false >"$capture/fences.after"; if [ "$status" -eq 0 ]; then status=126; fi; fi
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.finished-at"
printf '%s\n' "$status" >"$capture/outer.exit"
exit "$status"
