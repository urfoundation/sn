#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/causal/compile/prior_native_abi_gates/crv4
source=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/causal/prior_native_abi_gates/workspace/sn
selector=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/causal/compile/prior_native_abi_gates/crv4/selector.txt
siblings=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/causal/prior_native_abi_gates/workspace/sn/../../SIBLINGS.tsv
variant_json=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/causal/prior_native_abi_gates/workspace/sn/../../VARIANT.json
expected=38d7236e7af1d48f354844b894898b65ce6c345c
package=./crv4
binary=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/causal/compile/prior_native_abi_gates/crv4/crv4.causal.test
status=125
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.started-at"
printf '%s\n' "$$" >"$capture/outer.pid"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"$capture/outer.process-at-start"
printf '%s\n' "$(ps -o sid= -p "$$" | tr -d ' ')" >"$capture/outer.sid"
printf '%s\n' "env GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off; timeout --foreground --kill-after=15s 900s go test -c -p=1 -o $binary $package" >"$capture/command.txt"
sha256sum "$capture/command.sh" "$variant_json" >"$capture/command-and-variant.sha256"
sha256sum "$selector" "$capture/expected-tests.txt" "$capture/expected-pass-tests.txt" "$capture/expected-fail-tests.txt" >"$capture/selectors.before.sha256"
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
if [ "$(cat "$capture/source.commit.before")" = "$expected" ] && [ ! -s "$capture/source.status.before" ] && [ -f "$variant_json" ] && check_deps; then
  printf '%s\n' true >"$capture/source-dependency.fence.before"
  printf '%s\n' true >"$capture/build.started"
  ( cd "$source" && env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS='-mod=readonly' GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/mnt/data/sn-testnet/gocache TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" timeout --foreground --kill-after=15s 900s go test -c -p=1 -o "$binary" "$package" ) >"$capture/build.stdout" 2>"$capture/build.stderr"
  status=$?
else
  printf '%s\n' false >"$capture/source-dependency.fence.before"
  printf '%s\n' false >"$capture/build.started"
  printf '%s\n' 'causal source, sibling dependency, or variant fence refused compilation' >"$capture/build.stderr"
fi
printf '%s\n' "$status" >"$capture/build.exit"
git -C "$source" rev-parse HEAD >"$capture/source.commit.after"
git -C "$source" status --porcelain=v1 >"$capture/source.status.after"
snapshot_deps "$capture/dependencies.after.tsv"
sha256sum "$selector" "$capture/expected-tests.txt" "$capture/expected-pass-tests.txt" "$capture/expected-fail-tests.txt" >"$capture/selectors.after.sha256"
sha256sum "$variant_json" >"$capture/variant.after.sha256"
if cmp -s "$capture/source.commit.before" "$capture/source.commit.after" && cmp -s "$capture/source.status.before" "$capture/source.status.after" && cmp -s "$capture/dependencies.before.tsv" "$capture/dependencies.after.tsv" && cmp -s "$capture/selectors.before.sha256" "$capture/selectors.after.sha256"; then printf '%s\n' true >"$capture/source-dependency-selector.fence.after"; else printf '%s\n' false >"$capture/source-dependency-selector.fence.after"; if [ "$status" -eq 0 ]; then status=126; fi; fi
if [ -f "$binary" ]; then sha256sum "$binary" >"$capture/binary.sha256"; stat -c 'mode=%a size=%s mtime=%y path=%n' "$binary" >"$capture/binary.stat"; elif [ "$status" -eq 0 ]; then status=126; fi
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.finished-at"
printf '%s\n' "$status" >"$capture/outer.exit"
exit "$status"
