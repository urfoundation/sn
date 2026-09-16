#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/runtime461-renderer-cli
source=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/workspace/sn
siblings=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/SIBLINGS.tsv
expected=8edb3167a6261bfd82ecbc5f3c0ac2c787beec7c
binary=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/terra/runtime461-renderer-cli/sim-testnet
warm_cache=/home/by/urnetwork/temp/sn-carried-fleet-qualification-20260913/runtime/gocache
modcache=/home/by/go/pkg/mod
status=125
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/capture/outer.started-at"
printf '%s\n' "$$" >"$capture/capture/outer.pid"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"$capture/capture/outer.process-at-start"
printf '%s\n' "$(ps -o sid= -p "$$" | tr -d ' ')" >"$capture/capture/outer.sid"
printf '%s\n' 'go build -trimpath -buildvcs=true -o sim-testnet ./sim-testnet; read-only renderer candidate only, no CLI invocation' >"$capture/command.txt"
sha256sum "$capture/command.sh" >"$capture/command.sha256"
snapshot() {
  local output=$1 name target want linked actual dirty
  {
    printf 'sn\t%s\t%s\t%s\n' "$source" "$(git -C "$source" rev-parse HEAD)" "$(git -C "$source" status --porcelain=v1 | sha256sum | awk '{print $1}')"
    while IFS=$'\t' read -r name target want; do
      linked=$(readlink -f "$source/../$name" 2>/dev/null || true)
      actual=$(git -C "$target" rev-parse HEAD 2>/dev/null || true)
      dirty=$(git -C "$target" status --porcelain=v1 2>/dev/null | sha256sum | awk '{print $1}')
      printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$source/../$name" "$linked" "$want" "$actual" "$dirty"
    done <"$siblings"
  } >"$output"
}
check_siblings() {
  local name target want linked actual dirty bad=0
  while IFS=$'\t' read -r name target want; do
    linked=$(readlink -f "$source/../$name" 2>/dev/null || true)
    actual=$(git -C "$target" rev-parse HEAD 2>/dev/null || true)
    dirty=$(git -C "$target" status --porcelain=v1 2>/dev/null || true)
    if [ "$linked" != "$target" ] || [ "$actual" != "$want" ] || [ -n "$dirty" ]; then bad=1; fi
  done <"$siblings"
  return "$bad"
}
printf '%s\n' "$(go version)" >"$capture/meta/go.version"
sha256sum "$siblings" >"$capture/meta/siblings.sha256"
snapshot "$capture/meta/source-pair.before.tsv"
git -C "$source" rev-parse HEAD >"$capture/meta/source.commit.before"
git -C "$source" status --porcelain=v1 >"$capture/meta/source.status.before"
if [ "$(cat "$capture/meta/source.commit.before")" = "$expected" ] && [ ! -s "$capture/meta/source.status.before" ] && [ "$(wc -l <"$siblings" | tr -d ' ')" = 11 ] && check_siblings && [ -d "$warm_cache" ] && [ -d "$modcache" ] && [ ! -e "$binary" ]; then
  printf '%s\n' true >"$capture/meta/input.fence.before"
  printf '%s\n' true >"$capture/logs/build.started"
  date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/logs/build.started-at"
  ( cd "$source" && exec env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS='-mod=readonly -p=4' GOPROXY=off GOSUMDB=off GOWORK=off GOENV=off GOTOOLCHAIN=local GOVCS='*:off' GOMODCACHE="$modcache" GOCACHE="$warm_cache" TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" go build -trimpath -buildvcs=true -o "$binary" ./sim-testnet ) >"$capture/logs/build.stdout" 2>"$capture/logs/build.stderr"
  status=$?
else
  printf '%s\n' false >"$capture/meta/input.fence.before"
  printf '%s\n' false >"$capture/logs/build.started"
  printf '%s\n' 'candidate source, exact eleven dependencies, cache, or fresh-output fence refused renderer build' >"$capture/logs/build.stderr"
fi
printf '%s\n' "$status" >"$capture/logs/build.exit"
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/logs/build.finished-at"
snapshot "$capture/meta/source-pair.after.tsv"
git -C "$source" rev-parse HEAD >"$capture/meta/source.commit.after"
git -C "$source" status --porcelain=v1 >"$capture/meta/source.status.after"
sha256sum "$capture/meta/source-pair.before.tsv" >"$capture/meta/source-pair.before.sha256"
sha256sum "$capture/meta/source-pair.after.tsv" >"$capture/meta/source-pair.after.sha256"
if cmp -s "$capture/meta/source-pair.before.tsv" "$capture/meta/source-pair.after.tsv" && cmp -s "$capture/meta/source.commit.before" "$capture/meta/source.commit.after" && cmp -s "$capture/meta/source.status.before" "$capture/meta/source.status.after"; then printf '%s\n' true >"$capture/meta/input.fence.after"; else printf '%s\n' false >"$capture/meta/input.fence.after"; if [ "$status" -eq 0 ]; then status=126; fi; fi
if [ -f "$binary" ]; then
  chmod 700 "$binary"
  sha256sum "$binary" >"$capture/meta/renderer.sha256"
  stat -c 'mode=%a size=%s mtime=%y path=%n' "$binary" >"$capture/meta/renderer.stat"
  go version -m "$binary" >"$capture/meta/renderer.buildinfo.txt"
elif [ "$status" -eq 0 ]; then status=126; fi
if [ "$status" -eq 0 ]; then
  before=$(awk '{print $1}' "$capture/meta/source-pair.before.sha256")
  after=$(awk '{print $1}' "$capture/meta/source-pair.after.sha256")
  artifact=$(awk '{print $1}' "$capture/meta/renderer.sha256")
  bytes=$(stat -c %s "$binary")
  printf '{"kind":"runtime461-candidate-readonly-renderer-cli","build_exit":0,"source_commit":"%s","source_pair_before_sha256":"%s","source_pair_after_sha256":"%s","artifact":{"path":"%s","sha256":"%s","bytes":%s,"mode":"0700"},"native_or_renderer_execution":"none"}\n' "$expected" "$before" "$after" "$binary" "$artifact" "$bytes" >"$capture/RESULT-READONLY-RENDERER.json"
  sha256sum "$capture/RESULT-READONLY-RENDERER.json" >"$capture/RESULT-READONLY-RENDERER.json.sha256"
fi
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/capture/outer.finished-at"
printf '%s\n' "$status" >"$capture/capture/outer.exit"
exit "$status"
