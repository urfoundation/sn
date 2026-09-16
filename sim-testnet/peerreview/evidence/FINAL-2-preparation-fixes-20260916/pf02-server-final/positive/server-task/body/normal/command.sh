#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture='/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/portable-server-retry-2/positive/server-task/body/normal'
source='/home/by/urnetwork/temp/sn-taskworker-profile-20260916/workspace/server'
expected='6752a8df246c0ee7e1c5a38cbd26b1e849b702ca'
pkg='./task'
packageid='github.com/urnetwork/server/task'
testenv='server'
compiler='/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/positive/server-task/compile/normal'
binary='/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/positive/server-task/compile/normal/server-task.normal.test'
selector='/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/positive/server-task/selector.txt'
expected_tests='/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/positive/server-task/expected-tests.txt'
expected_outcomes='/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/positive/server-task/expected-outcomes.tsv'
deps='/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/admission/positive-server-task.dependencies.expected.tsv'
expected_exit='0'
expected_package='PASS'
body_status=125
final_status=125
source_fence() {
  local phase=$1 rc1 rc2 rc3
  git -C "$source" rev-parse HEAD >"$capture/source.commit.$phase"; rc1=$?
  git -C "$source" status --porcelain=v1 >"$capture/source.status.$phase"; rc2=$?
  git -C "$source" diff --check >"$capture/source.diff-check.$phase" 2>&1; rc3=$?
  printf '%s\n' "$rc1 $rc2 $rc3" >"$capture/source.fence.$phase.exit"
}
dep_fence() {
  local phase=$1 dep want_path want_commit actual target lines rc=0
  : >"$capture/dependencies.$phase.tsv"
  while IFS=$'\t' read -r dep want_path want_commit _; do
    target=$(readlink -f "$source/../$dep" 2>/dev/null || true)
    actual=$(git -C "$target" rev-parse HEAD 2>/dev/null || true)
    lines=$(git -C "$target" status --porcelain=v1 2>/dev/null | wc -l | tr -d ' ')
    printf '%s\t%s\t%s\t%s\t%s\n' "$dep" "$target" "$want_commit" "$actual" "$lines" >>"$capture/dependencies.$phase.tsv"
    if [ "$target" != "$want_path" ] || [ "$actual" != "$want_commit" ] || [ "$lines" != 0 ]; then rc=1; fi
  done <"$deps"
  if [ "$rc" -eq 0 ]; then printf '%s\n' true >"$capture/dependencies.$phase.ok"; else printf '%s\n' false >"$capture/dependencies.$phase.ok"; fi
  return "$rc"
}
prepare_server_test_env() {
  local phase=$1 rc=0 name
  if [ "$testenv" != server ]; then printf '%s\n' not-required >"$capture/test-env.$phase.status"; return 0; fi
  (
    cd "$source"
    export WARP_ENV=local
    export WARP_TEST_ENV_FAIL_FAST=1
    source "$source/test-env.sh" >"$capture/test-env.$phase.stdout" 2>"$capture/test-env.$phase.stderr"
  )
  rc=$?
  printf '%s\n' "$rc" >"$capture/test-env.$phase.exit"
  {
    for name in WARP_ENV WARP_TEST_ENV_FAIL_FAST WARP_TEST_ENV_PORTABLE_ROOT WARP_TEST_ENV_USE_PORTABLE_RESOURCES WARP_TEST_ENV_ALLOW_UNMANAGED_PORTABLE_SERVICES WARP_VAULT_HOME WARP_CONFIG_HOME BRINGYOUR_POSTGRES_HOSTNAME BRINGYOUR_REDIS_HOSTNAME; do
      if [ -v "$name" ]; then printf '%s\tset\n' "$name"; else printf '%s\tunset\n' "$name"; fi
    done
  } >"$capture/test-env.$phase.safe.tsv"
  if [ "$rc" -eq 0 ]; then printf '%s\n' true >"$capture/test-env.$phase.status"; else printf '%s\n' false >"$capture/test-env.$phase.status"; fi
  return "$rc"
}
run_census() {
  if [ "$testenv" = server ]; then
    ( cd "$source" && export WARP_ENV=local WARP_TEST_ENV_FAIL_FAST=1 && source "$source/test-env.sh" >"$capture/test-env.census.stdout" 2>"$capture/test-env.census.stderr" && cd "$source/${pkg#./}" && env GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 180s "$binary" -test.list . )
  else
    ( cd "$source/${pkg#./}" && env GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 180s "$binary" -test.list . )
  fi
}
run_body() {
  local selector_value=$1
  if [ "$testenv" = server ]; then
    ( cd "$source" && export WARP_ENV=local WARP_TEST_ENV_FAIL_FAST=1 && source "$source/test-env.sh" >"$capture/test-env.body.stdout" 2>"$capture/test-env.body.stderr" && cd "$source/${pkg#./}" && env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 1200s go tool test2json -t -p "$packageid" "$binary" -test.v -test.run="$selector_value" -test.count=1 -test.timeout=20m -test.parallel=4 )
  else
    ( cd "$source/${pkg#./}" && env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 1200s go tool test2json -t -p "$packageid" "$binary" -test.v -test.run="$selector_value" -test.count=1 -test.timeout=20m -test.parallel=4 )
  fi
}
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.started-at"
printf '%s\n' "$$" >"$capture/outer.pid"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"$capture/outer.process-at-start"
printf '%s\n' "$(ps -o sid= -p "$$" | tr -d ' ')" >"$capture/outer.sid"
printf '%s\n' "WARP_ENV=local WARP_TEST_ENV_FAIL_FAST=1; go tool test2json -t -p $packageid $binary -test.v -test.run=exact-selector -test.count=1 -test.timeout=20m -test.parallel=4; server packages source test-env.sh and use disposable local PostgreSQL/Redis test isolation" >"$capture/command.txt"
sha256sum "$capture/command.sh" >"$capture/command.sha256"
sha256sum "$selector" "$expected_tests" "$expected_outcomes" >"$capture/selectors.before.sha256"
source_fence before
dep_fence before || true
if [ -f "$binary" ]; then sha256sum "$binary" >"$capture/binary.before.sha256"; fi
expected_binary=$(awk '{print $1}' "$compiler/binary.sha256" 2>/dev/null || true)
actual_binary=$(awk '{print $1}' "$capture/binary.before.sha256" 2>/dev/null || true)
compiler_exit=$(cat "$compiler/outer.exit" 2>/dev/null || true)
if [ "$(cat "$capture/source.commit.before")" = "$expected" ] && [ ! -s "$capture/source.status.before" ] && [ ! -s "$capture/source.diff-check.before" ] && [ "$(cat "$capture/dependencies.before.ok")" = true ] && [ "$expected_binary" = "$actual_binary" ] && [ "$compiler_exit" = 0 ] && [ -d "$source/${pkg#./}" ]; then
  printf '%s\n' true >"$capture/preflight.ok"
  run_census >"$capture/compiled.tests.raw" 2>"$capture/census.stderr"
  census_status=$?
  printf '%s\n' "$census_status" >"$capture/census.exit"
  sort -u "$capture/compiled.tests.raw" >"$capture/compiled.tests.txt"
  comm -23 "$expected_tests" "$capture/compiled.tests.txt" >"$capture/census.missing.txt"
  if [ "$census_status" -eq 0 ] && [ ! -s "$capture/census.missing.txt" ]; then printf '%s\n' true >"$capture/census.valid"; else printf '%s\n' false >"$capture/census.valid"; fi
  if [ "$(cat "$capture/census.valid")" = true ]; then
    printf '%s\n' true >"$capture/body.started"
    selector_value=$(cat "$selector")
    run_body "$selector_value" >"$capture/test2json.jsonl" 2>"$capture/test2json.stderr"
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
  if cmp -s "$expected_outcomes" "$capture/observed.top-level.tsv" && [ "$(wc -l <"$capture/package.terminal.tsv" | tr -d ' ')" = 1 ] && grep -Fqx "$packageid"$'\t'"$expected_package" "$capture/package.terminal.tsv"; then printf '%s\n' true >"$capture/events.valid"; else printf '%s\n' false >"$capture/events.valid"; fi
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
if ! cmp -s "$capture/source.commit.before" "$capture/source.commit.after" || ! cmp -s "$capture/source.status.before" "$capture/source.status.after" || ! cmp -s "$capture/source.diff-check.before" "$capture/source.diff-check.after" || ! cmp -s "$capture/selectors.before.sha256" "$capture/selectors.after.sha256" || ! cmp -s "$capture/dependencies.before.tsv" "$capture/dependencies.after.tsv" || ! cmp -s "$capture/binary.before.sha256" "$capture/binary.after.sha256" || [ "$(cat "$capture/dependencies.after.ok")" != true ]; then printf '%s\n' false >"$capture/fences.after"; final_status=126; else printf '%s\n' true >"$capture/fences.after"; fi
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.finished-at"
printf '%s\n' "$final_status" >"$capture/outer.exit"
exit "$final_status"
