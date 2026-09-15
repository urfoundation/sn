#!/usr/bin/env bash
set -euo pipefail
umask 077
runtime='/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/terra-runtime/final-cli-eccfae8-canonical'
workspace='/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical'
source_pair="$runtime/meta/source-pair-final.tsv"
expected_sn="${EXPECTED_SN:?EXPECTED_SN is required}"
expected_lock='5b8c412454adfde72f2cf51c682b5ce4b9b335a2095eaafc21bf4e92a153a4ab'
lock_file="$workspace/sn/deploy/testnet/release.lock.yml"
warm_cache='/home/by/urnetwork/temp/sn-carried-fleet-qualification-20260913/runtime/gocache'
modcache='/home/by/go/pkg/mod'
source_root="$workspace/sn"
binary="$runtime/sim-testnet"
snapshot() {
  local output="$1" repo root physical head status status_sha gitdir
  : > "$output"
  for repo in sn server operator-proxy connect sdk glog goidenticons proxy userwireguard vault xops config warp; do
    root="$workspace/$repo"
    [[ -d "$root" && -d "$root/.git" && ! -L "$root" ]] || return 125
    physical="$(readlink -f -- "$root")"
    [[ "$physical" == "$root" ]] || return 125
    gitdir="$(git -C "$root" rev-parse --absolute-git-dir)" || return 125
    [[ -d "$gitdir" ]] || return 125
    head="$(git -C "$root" rev-parse --verify 'HEAD^{commit}')" || return 125
    status="$(git -C "$root" status --porcelain=v1 --untracked-files=all)" || return 125
    [[ -z "$status" ]] || return 125
    status_sha="$(printf '%s' "$status" | sha256sum | awk '{print $1}')"
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$repo" "$root" "$physical" "$head" "${#status}" "$status_sha" >> "$output"
  done
}
[[ "$expected_sn" =~ ^[0-9a-f]{40}$ ]] || exit 125
for private_path in "$runtime" "$runtime/tmp" "$runtime/gotmp" "$warm_cache"; do
  [[ -d "$private_path" && ! -L "$private_path" && "$(stat -c '%a' "$private_path")" == 700 ]] || exit 125
done
[[ -d "$modcache" && ! -L "$modcache" ]] || exit 125
[[ -f "$source_pair" && ! -L "$source_pair" && "$(stat -c '%a' "$source_pair")" == 600 ]] || exit 125
[[ -d "$source_root/.git" && ! -L "$source_root" && ! -e "$binary" && ! -L "$binary" ]] || exit 125
[[ "$(git -C "$source_root" rev-parse HEAD)" == "$expected_sn" ]] || exit 125
[[ "$(git -C "$source_root" rev-parse origin/main)" == "$expected_sn" ]] || exit 125
[[ "$(sha256sum "$lock_file" | awk '{print $1}')" == "$expected_lock" ]] || exit 125
snapshot "$runtime/meta/repos.before.tsv"
cmp -s "$source_pair" "$runtime/meta/repos.before.tsv"
sha256sum "$runtime/meta/repos.before.tsv" > "$runtime/meta/repos.before.sha256"
date -u +%Y-%m-%dT%H:%M:%SZ > "$runtime/logs/build.started-at"
set +e
(
  cd -- "$source_root" || exit 125
  exec env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS='-mod=readonly -p=4' GOPROXY=off GOSUMDB=off GOWORK=off GOENV=off GOTOOLCHAIN=local GOVCS='*:off' TMPDIR="$runtime/tmp" GOTMPDIR="$runtime/gotmp" GOCACHE="$warm_cache" GOMODCACHE="$modcache" go build -trimpath -buildvcs=true -o "$binary" ./sim-testnet
) > "$runtime/logs/build.stdout" 2> "$runtime/logs/build.stderr"
build_status=$?
set -e
printf '%s\n' "$build_status" > "$runtime/logs/build.exit"
date -u +%Y-%m-%dT%H:%M:%SZ > "$runtime/logs/build.finished-at"
snapshot "$runtime/meta/repos.after.tsv"
cmp -s "$source_pair" "$runtime/meta/repos.after.tsv"
sha256sum "$runtime/meta/repos.after.tsv" > "$runtime/meta/repos.after.sha256"
(( build_status == 0 )) || exit "$build_status"
[[ -f "$binary" && ! -L "$binary" && "$(stat -c '%a' "$binary")" == 700 ]] || exit 1
sha256sum "$binary" > "$runtime/meta/cli.sha256"
stat -c '%a %s %i %n' "$binary" > "$runtime/meta/cli.stat"
go version -m "$binary" > "$runtime/meta/cli.buildinfo.txt"
rg -q '^\s*build\s+-trimpath=true$' "$runtime/meta/cli.buildinfo.txt"
rg -q '^\s*build\s+vcs=git$' "$runtime/meta/cli.buildinfo.txt"
rg -q "^\s*build\s+vcs.revision=$expected_sn$" "$runtime/meta/cli.buildinfo.txt"
rg -q '^\s*build\s+vcs.modified=false$' "$runtime/meta/cli.buildinfo.txt"
before_sha="$(awk '{print $1}' "$runtime/meta/repos.before.sha256")"
after_sha="$(awk '{print $1}' "$runtime/meta/repos.after.sha256")"
binary_sha="$(awk '{print $1}' "$runtime/meta/cli.sha256")"
binary_bytes="$(stat -c '%s' "$binary")"
printf '{\n  "kind":"final-stamped-sim-testnet-cli-build",\n  "build_exit":0,\n  "repos_before_after_identical":true,\n  "repos_before_sha256":"%s",\n  "repos_after_sha256":"%s",\n  "artifact":{"path":"%s","sha256":"%s","bytes":%s,"mode":"0700"},\n  "vcs":{"revision":"%s","trimpath":true,"buildvcs":true,"modified":false},\n  "release_lock_sha256":"%s",\n  "warm_gocache":"%s",\n  "gomodcache":"%s"\n}\n' "$before_sha" "$after_sha" "$binary" "$binary_sha" "$binary_bytes" "$expected_sn" "$expected_lock" "$warm_cache" "$modcache" > "$runtime/RESULT-FINAL-CLI-BUILD.json"
sha256sum "$runtime/RESULT-FINAL-CLI-BUILD.json" > "$runtime/RESULT-FINAL-CLI-BUILD.json.sha256"
