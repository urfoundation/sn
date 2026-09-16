#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/positive/body/crv4-normal
source=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/workspace/sn
compiler=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/positive/compile/crv4-normal
binary=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/positive/compile/crv4-normal/crv4.normal.test
selector=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/positive/body/crv4-normal/selector.txt
expected_tests=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/positive/body/crv4-normal/expected-tests.txt
siblings=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/SIBLINGS.tsv
expected=8edb3167a6261bfd82ecbc5f3c0ac2c787beec7c
package_dir=crv4
expected_count=15
status=125
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.started-at"
printf '%s\n' "$$" >"$capture/outer.pid"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"$capture/outer.process-at-start"
printf '%s\n' "$(ps -o sid= -p "$$" | tr -d ' ')" >"$capture/outer.sid"
printf '%s\n' 'go tool test2json -t <compiled-binary> -test.v -test.run=<exact selector> -test.count=1 -test.parallel=4' >"$capture/command.txt"
sha256sum "$capture/command.sh" >"$capture/command.sha256"
sha256sum "$selector" "$expected_tests" >"$capture/selectors.before.sha256"
git -C "$source" rev-parse HEAD >"$capture/source.commit.before"
git -C "$source" status --porcelain=v1 >"$capture/source.status.before"
snapshot_deps() {
  local out=$1 name target expected_target linked actual dirty
  {
    printf 'sn\t%s\t%s\t%s\n' "$source" "$(git -C "$source" rev-parse HEAD)" "$(git -C "$source" status --porcelain=v1 | sha256sum | awk '{print $1}')"
    while IFS=$'\t' read -r name target expected_target; do
      linked=$(readlink -f "$source/../$name" 2>/dev/null || true)
      actual=$(git -C "$target" rev-parse HEAD 2>/dev/null || true)
      dirty=$(git -C "$target" status --porcelain=v1 2>/dev/null | sha256sum | awk '{print $1}')
      printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$source/../$name" "$linked" "$expected_target" "$actual" "$dirty"
    done <"$siblings"
  } >"$out"
}
check_deps() {
  local name target expected_target linked actual dirty bad=0
  while IFS=$'\t' read -r name target expected_target; do
    linked=$(readlink -f "$source/../$name" 2>/dev/null || true)
    actual=$(git -C "$target" rev-parse HEAD 2>/dev/null || true)
    dirty=$(git -C "$target" status --porcelain=v1 2>/dev/null || true)
    if [ "$linked" != "$target" ] || [ "$actual" != "$expected_target" ] || [ -n "$dirty" ]; then bad=1; fi
  done <"$siblings"
  return "$bad"
}
snapshot_deps "$capture/dependencies.before.tsv"
if [ -f "$binary" ]; then sha256sum "$binary" >"$capture/binary.before.sha256"; else : >"$capture/binary.before.sha256"; fi
compiler_binary=$(awk 'NR==1 {print $1}' "$compiler/binary.sha256" 2>/dev/null || true)
actual_binary=$(awk 'NR==1 {print $1}' "$capture/binary.before.sha256" 2>/dev/null || true)
selector_value=$(tr -d '\n' <"$selector")
printf '%s\n' "$selector_value" >"$capture/selector.single-line.txt"
if [ "$(cat "$capture/source.commit.before")" = "$expected" ] && [ ! -s "$capture/source.status.before" ] && check_deps && [ -d "$source/$package_dir" ] && [ -n "$compiler_binary" ] && [ "$compiler_binary" = "$actual_binary" ] && [ "$(cat "$compiler/build.exit" 2>/dev/null || true)" = 0 ] && [ "$(cat "$compiler/outer.exit" 2>/dev/null || true)" = 0 ] && [ "$(cat "$compiler/launcher.join.exit" 2>/dev/null || true)" = 0 ] && [ "$(cat "$compiler/source-dependency-selector.fence.after" 2>/dev/null || true)" = true ]; then
  printf '%s\n' true >"$capture/preflight.ok"
  ( cd "$source/$package_dir" && timeout --foreground --kill-after=15s 60s "$binary" -test.list . ) >"$capture/compiled-census.txt" 2>"$capture/compiled-census.stderr"
  census_status=$?
  printf '%s\n' "$census_status" >"$capture/compiled-census.exit"
  if [ "$census_status" -eq 0 ]; then
    grep -E "$selector_value" "$capture/compiled-census.txt" | LC_ALL=C sort -u >"$capture/compiled-selected-tests.txt" || true
    if cmp -s "$expected_tests" "$capture/compiled-selected-tests.txt" && [ "$(wc -l <"$capture/compiled-selected-tests.txt" | tr -d ' ')" = "$expected_count" ]; then
      printf '%s\n' true >"$capture/compiled-census.valid"
      printf '%s\n' true >"$capture/body.started"
      ( cd "$source/$package_dir" && env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS='-mod=readonly' GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 900s go tool test2json -t "$binary" -test.v -test.run="$selector_value" -test.count=1 -test.timeout=12m -test.parallel=4 ) >"$capture/test2json.jsonl" 2>"$capture/test2json.stderr"
      status=$?
    else
      printf '%s\n' false >"$capture/compiled-census.valid"
      printf '%s\n' false >"$capture/body.started"
      printf '%s\n' 'compiled census does not equal final selected root list' >"$capture/test2json.stderr"
    fi
  else
    printf '%s\n' false >"$capture/compiled-census.valid"
    printf '%s\n' false >"$capture/body.started"
    printf '%s\n' 'compiled test census failed' >"$capture/test2json.stderr"
  fi
else
  printf '%s\n' false >"$capture/preflight.ok"
  printf '%s\n' false >"$capture/body.started"
  printf '%s\n' 'source, sibling dependency, compiler, binary, selector, or package working-directory preflight refused body' >"$capture/test2json.stderr"
fi
printf '%s\n' "$status" >"$capture/body.exit"
if [ "$status" -eq 0 ]; then
  jq -r 'select(.Action == "pass" and .Test != null) | .Test' "$capture/test2json.jsonl" | LC_ALL=C sort -u >"$capture/observed.tests.txt" 2>"$capture/event-validation.stderr"
  jq -r 'select(.Action == "pass" and .Test == null) | .Package' "$capture/test2json.jsonl" >"$capture/package.pass.events.txt" 2>>"$capture/event-validation.stderr"
  if cmp -s "$expected_tests" "$capture/observed.tests.txt" && [ "$(wc -l <"$capture/package.pass.events.txt" | tr -d ' ')" = 1 ]; then printf '%s\n' true >"$capture/events.valid"; else printf '%s\n' false >"$capture/events.valid"; status=127; fi
fi
git -C "$source" rev-parse HEAD >"$capture/source.commit.after"
git -C "$source" status --porcelain=v1 >"$capture/source.status.after"
snapshot_deps "$capture/dependencies.after.tsv"
sha256sum "$selector" "$expected_tests" >"$capture/selectors.after.sha256"
if [ -f "$binary" ]; then sha256sum "$binary" >"$capture/binary.after.sha256"; else : >"$capture/binary.after.sha256"; fi
if cmp -s "$capture/source.commit.before" "$capture/source.commit.after" && cmp -s "$capture/source.status.before" "$capture/source.status.after" && cmp -s "$capture/dependencies.before.tsv" "$capture/dependencies.after.tsv" && cmp -s "$capture/selectors.before.sha256" "$capture/selectors.after.sha256" && cmp -s "$capture/binary.before.sha256" "$capture/binary.after.sha256"; then printf '%s\n' true >"$capture/fences.after"; else printf '%s\n' false >"$capture/fences.after"; if [ "$status" -eq 0 ]; then status=126; fi; fi
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.finished-at"
printf '%s\n' "$status" >"$capture/outer.exit"
exit "$status"
