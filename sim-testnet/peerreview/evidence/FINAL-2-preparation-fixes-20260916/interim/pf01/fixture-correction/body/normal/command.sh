#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture='/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/normal/body'
source='/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/workspace-frozen/candidate/sn'
compiler='/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/normal/compile'
binary='/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/normal/compile/sim-testnet.normal.test'
selector='/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/inputs/selector.txt'
expected_tests='/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/inputs/expected-tests.txt'
expected_outcomes='/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/inputs/expected-outcomes.tsv'
expected='d52028de6864f7b1c48381fcb7652361d2520932'
expected_exit='0'
expected_package='PASS'
dep_expected='/mnt/data/sn-testnet/qualification/carried-preparation-index-candidate-20260916-r1/terra/fixture-correction/inputs/dependencies.tsv'
body_status=125
final_status=125
source_fence() {
  local phase=$1 rc_head rc_status rc_diff
  git -C "$source" rev-parse HEAD >"$capture/source.commit.$phase"; rc_head=$?
  git -C "$source" status --porcelain=v1 >"$capture/source.status.$phase"; rc_status=$?
  git -C "$source" diff --check >"$capture/source.diff-check.$phase" 2>&1; rc_diff=$?
  printf '%s\n' "$rc_head $rc_status $rc_diff" >"$capture/source.fence.$phase.exit"
}
dep_fence() {
  local phase=$1 dep want actual lines target rc=0
  : >"$capture/dependencies.$phase.tsv"
  while IFS=$'\t' read -r dep want _; do
    actual=$(git -C "$source/../$dep" rev-parse HEAD 2>/dev/null || true)
    lines=$(git -C "$source/../$dep" status --porcelain=v1 2>/dev/null | wc -l | tr -d ' ')
    target=$(readlink -f "$source/../$dep" 2>/dev/null || true)
    printf '%s\t%s\t%s\t%s\t%s\n' "$dep" "$want" "$actual" "$lines" "$target" >>"$capture/dependencies.$phase.tsv"
    if [ "$actual" != "$want" ] || [ "$lines" != 0 ] || [ "$target" != "/home/by/urnetwork/$dep" ]; then rc=1; fi
  done <"$dep_expected"
  if [ "$rc" -eq 0 ]; then printf '%s\n' true >"$capture/dependencies.$phase.ok"; else printf '%s\n' false >"$capture/dependencies.$phase.ok"; fi
  return "$rc"
}
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.started-at"
printf '%s\n' "$$" >"$capture/outer.pid"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"$capture/outer.process-at-start"
printf '%s\n' "$(ps -o sid= -p "$$" | tr -d ' ')" >"$capture/outer.sid"
printf '%s\n' "env GOMAXPROCS=4 GOFLAGS=-mod=readonly GOCACHE=/mnt/data/sn-testnet/gocache timeout --foreground --kill-after=15s 600s go tool test2json -t -p github.com/urnetwork/sn/sim-testnet $binary -test.v -test.run=exact-selector -test.count=1 -test.timeout=10m -test.parallel=4" >"$capture/command.txt"
sha256sum "$capture/command.sh" >"$capture/command.sha256"
sha256sum "$selector" "$expected_tests" "$expected_outcomes" >"$capture/selectors.before.sha256"
source_fence before
dep_fence before || true
if [ -f "$binary" ]; then sha256sum "$binary" >"$capture/binary.before.sha256"; fi
expected_binary=$(awk '{print $1}' "$compiler/binary.sha256" 2>/dev/null || true)
actual_binary=$(awk '{print $1}' "$capture/binary.before.sha256" 2>/dev/null || true)
compiler_exit=$(cat "$compiler/outer.exit" 2>/dev/null || true)
if [ "$(cat "$capture/source.commit.before")" = "$expected" ] && [ ! -s "$capture/source.status.before" ] && [ ! -s "$capture/source.diff-check.before" ] && [ "$(cat "$capture/dependencies.before.ok")" = true ] && [ "$expected_binary" = "$actual_binary" ] && [ "$compiler_exit" = 0 ] && [ -d "$source/sim-testnet" ]; then
  printf '%s\n' true >"$capture/preflight.ok"
  printf '%s\n' 'compiled test census only; selected bodies run from matching sim-testnet package directory with private temporary state' >"$capture/relative-fixtures.txt"
  ( cd "$source/sim-testnet" && env GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 120s "$binary" -test.list . ) >"$capture/compiled.tests.raw" 2>"$capture/census.stderr"
  census_status=$?
  printf '%s\n' "$census_status" >"$capture/census.exit"
  sort -u "$capture/compiled.tests.raw" >"$capture/compiled.tests.txt"
  comm -23 "$expected_tests" "$capture/compiled.tests.txt" >"$capture/census.missing.txt"
  if [ "$census_status" -eq 0 ] && [ ! -s "$capture/census.missing.txt" ]; then printf '%s\n' true >"$capture/census.valid"; else printf '%s\n' false >"$capture/census.valid"; fi
  if [ "$(cat "$capture/census.valid")" = true ]; then
    printf '%s\n' true >"$capture/body.started"
    selector_value=$(cat "$selector")
    ( cd "$source/sim-testnet" && env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 600s go tool test2json -t -p github.com/urnetwork/sn/sim-testnet "$binary" -test.v -test.run="$selector_value" -test.count=1 -test.timeout=10m -test.parallel=4 ) >"$capture/test2json.jsonl" 2>"$capture/test2json.stderr"
    body_status=$?
  else
    printf '%s\n' false >"$capture/body.started"
    printf '%s\n' 'compiled test census did not contain every selected root' >"$capture/test2json.stderr"
  fi
else
  printf '%s\n' false >"$capture/preflight.ok"
  printf '%s\n' false >"$capture/body.started"
  printf '%s\n' 'source, dependency, compiler, binary, selector, or package working-directory preflight refused body' >"$capture/test2json.stderr"
fi
printf '%s\n' "$body_status" >"$capture/body.exit"
if [ -f "$capture/test2json.jsonl" ]; then
  jq -r 'select(.Test != null and (.Action == "pass" or .Action == "fail")) | [.Test, (.Action | ascii_upcase)] | @tsv' "$capture/test2json.jsonl" >"$capture/observed.top-level.raw.tsv" 2>"$capture/event-validation.stderr" || true
  awk -F '\t' 'NR==FNR { want[$1]=$2; next } ($1 in want) { seen[$1]=$2 } END { for (name in want) print name "\t" ((name in seen) ? seen[name] : "MISSING") }' "$expected_outcomes" "$capture/observed.top-level.raw.tsv" | sort >"$capture/observed.top-level.tsv"
  jq -r 'select(.Test == null and (.Action == "pass" or .Action == "fail")) | [.Package, (.Action | ascii_upcase)] | @tsv' "$capture/test2json.jsonl" >"$capture/package.terminal.tsv" 2>>"$capture/event-validation.stderr" || true
  if cmp -s "$expected_outcomes" "$capture/observed.top-level.tsv" && [ "$(wc -l <"$capture/package.terminal.tsv" | tr -d ' ')" = 1 ] && grep -Fqx $'github.com/urnetwork/sn/sim-testnet\t'"$expected_package" "$capture/package.terminal.tsv"; then printf '%s\n' true >"$capture/events.valid"; else printf '%s\n' false >"$capture/events.valid"; fi
else
  : >"$capture/observed.top-level.raw.tsv"
  awk -F '\t' '{print $1 "\tMISSING"}' "$expected_outcomes" >"$capture/observed.top-level.tsv"
  : >"$capture/package.terminal.tsv"
  printf '%s\n' false >"$capture/events.valid"
fi
source_fence after
dep_fence after || true
sha256sum "$selector" "$expected_tests" "$expected_outcomes" >"$capture/selectors.after.sha256"
if [ -f "$binary" ]; then sha256sum "$binary" >"$capture/binary.after.sha256"; fi
final_status=$body_status
if [ "$body_status" != "$expected_exit" ] || [ "$(cat "$capture/events.valid")" != true ]; then final_status=127; fi
if ! cmp -s "$capture/source.commit.before" "$capture/source.commit.after" || ! cmp -s "$capture/source.status.before" "$capture/source.status.after" || ! cmp -s "$capture/source.diff-check.before" "$capture/source.diff-check.after" || ! cmp -s "$capture/selectors.before.sha256" "$capture/selectors.after.sha256" || ! cmp -s "$capture/dependencies.before.tsv" "$capture/dependencies.after.tsv" || ! cmp -s "$capture/binary.before.sha256" "$capture/binary.after.sha256" || [ "$(cat "$capture/dependencies.after.ok")" != true ]; then
  printf '%s\n' false >"$capture/fences.after"
  final_status=126
else
  printf '%s\n' true >"$capture/fences.after"
fi
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.finished-at"
printf '%s\n' "$final_status" >"$capture/outer.exit"
exit "$final_status"
