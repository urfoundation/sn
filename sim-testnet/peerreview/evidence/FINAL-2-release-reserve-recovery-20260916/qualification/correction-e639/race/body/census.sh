#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/terra/correction-e639/race/body
source=/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/workspace/sn
compiler=/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/terra/correction-e639/compile/race
binary=/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/terra/correction-e639/compile/race/sim-testnet.correction.race.test
selector=/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/terra/correction-e639/confirmation.selector
expected_tests=/mnt/data/sn-testnet/qualification/reserve-software-revision-20260916-r1/terra/correction-e639/confirmation.expected-tests.txt
expected=e639e439a224384c3881421abdbbc7015f1be799
status=125
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/census.started-at"
printf '%s\n' "$$" >"$capture/census.pid"
printf '%s\n' 'timeout --foreground --kill-after=15s 120s binary -test.list=exact-selector' >"$capture/census.command.txt"
sha256sum "$selector" "$expected_tests" "$binary" >"$capture/census.inputs.before.sha256"
git -C "$source" rev-parse HEAD >"$capture/census.source.commit.before"
git -C "$source" status --porcelain >"$capture/census.source.status.before"
if [ "$(cat "$capture/census.source.commit.before")" = "$expected" ] && [ ! -s "$capture/census.source.status.before" ] && [ "$(awk '{print $1}' "$compiler/binary.sha256")" = "$(sha256sum "$binary" | awk '{print $1}')" ]; then
  selector_value=$(cat "$selector")
  (cd "$source/sim-testnet" && env GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 120s "$binary" -test.list="$selector_value") >"$capture/compiled.tests.raw" 2>"$capture/census.stderr"
  status=$?
else
  printf '%s\n' 'source/binary fence refused compiled census' >"$capture/census.stderr"
fi
if [ "$status" -eq 0 ]; then
  sort -u "$capture/compiled.tests.raw" >"$capture/compiled.tests.txt"
  if cmp -s "$expected_tests" "$capture/compiled.tests.txt"; then printf '%s\n' true >"$capture/census.valid"; else printf '%s\n' false >"$capture/census.valid"; status=127; fi
fi
printf '%s\n' "$status" >"$capture/census.exit"
git -C "$source" rev-parse HEAD >"$capture/census.source.commit.after"
git -C "$source" status --porcelain >"$capture/census.source.status.after"
sha256sum "$selector" "$expected_tests" "$binary" >"$capture/census.inputs.after.sha256"
if cmp -s "$capture/census.source.commit.before" "$capture/census.source.commit.after" && cmp -s "$capture/census.source.status.before" "$capture/census.source.status.after" && cmp -s "$capture/census.inputs.before.sha256" "$capture/census.inputs.after.sha256"; then :; else [ "$status" -ne 0 ] || status=126; printf '%s\n' "$status" >"$capture/census.exit"; fi
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/census.finished-at"
exit "$status"
