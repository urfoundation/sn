#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture='/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/positive/server-taskworker/compile/race'
source='/home/by/urnetwork/temp/sn-taskworker-profile-20260916/workspace/server'
expected='6752a8df246c0ee7e1c5a38cbd26b1e849b702ca'
pkg='./taskworker'
selector='/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/positive/server-taskworker/selector.txt'
deps='/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/admission/positive-server-taskworker.dependencies.expected.tsv'
binary='/mnt/data/sn-testnet/qualification/taskworker-profile-20260916-r1/terra/final/positive/server-taskworker/compile/race/server-taskworker.race.test'
raceflag='-race'
status=125
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
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.started-at"
printf '%s\n' "$$" >"$capture/outer.pid"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"$capture/outer.process-at-start"
printf '%s\n' "$(ps -o sid= -p "$$" | tr -d ' ')" >"$capture/outer.sid"
printf '%s\n' "env GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GOWORK=off GOENV=off GOTOOLCHAIN=local GOVCS=*:off GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache timeout --foreground --kill-after=15s 900s go test -c${raceflag} -p=1 -o $binary $pkg" >"$capture/command.txt"
sha256sum "$capture/command.sh" >"$capture/command.sha256"
sha256sum "$selector" >"$capture/selector.before.sha256"
source_fence before
dep_fence before || true
if [ "$(cat "$capture/source.commit.before")" = "$expected" ] && [ ! -s "$capture/source.status.before" ] && [ ! -s "$capture/source.diff-check.before" ] && [ "$(cat "$capture/dependencies.before.ok")" = true ]; then
  printf '%s\n' true >"$capture/preflight.ok"
  printf '%s\n' true >"$capture/build.started"
  ( cd "$source" && env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 900s go test -c $raceflag -p=1 -o "$binary" "$pkg" ) >"$capture/build.stdout" 2>"$capture/build.stderr"
  status=$?
else
  printf '%s\n' false >"$capture/preflight.ok"
  printf '%s\n' false >"$capture/build.started"
  printf '%s\n' 'source, dependency, selector, or cleanliness fence refused compilation' >"$capture/build.stderr"
fi
printf '%s\n' "$status" >"$capture/build.exit"
source_fence after
dep_fence after || true
sha256sum "$selector" >"$capture/selector.after.sha256"
if [ -f "$binary" ]; then sha256sum "$binary" >"$capture/binary.sha256"; fi
if cmp -s "$capture/source.commit.before" "$capture/source.commit.after" && cmp -s "$capture/source.status.before" "$capture/source.status.after" && cmp -s "$capture/source.diff-check.before" "$capture/source.diff-check.after" && cmp -s "$capture/selector.before.sha256" "$capture/selector.after.sha256" && cmp -s "$capture/dependencies.before.tsv" "$capture/dependencies.after.tsv" && [ "$(cat "$capture/dependencies.after.ok")" = true ]; then printf '%s\n' true >"$capture/fences.after"; else printf '%s\n' false >"$capture/fences.after"; if [ "$status" -eq 0 ]; then status=126; fi; fi
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.finished-at"
printf '%s\n' "$status" >"$capture/outer.exit"
exit "$status"
