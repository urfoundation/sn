#!/usr/bin/env bash
set -euo pipefail
umask 077
base=/home/by/urnetwork/temp/sn-runtime-config-identity-correction-20260915
runtime="/home/by/urnetwork/temp/sn-runtime-config-identity-correction-20260915/terra-runtime/renderer-cli-runtime-config-identity-readonly-prepared"
source_pair="$runtime/meta/source-pair-renderer.tsv"
source_root="$base/sn"
binary="$runtime/sim-testnet"
warm_cache=/home/by/urnetwork/temp/sn-carried-fleet-qualification-20260913/runtime/gocache
modcache=/home/by/go/pkg/mod
expected_sn="${EXPECTED_SN:?EXPECTED_SN is required}"
[[ "$expected_sn" =~ ^[0-9a-f]{40}$ ]] || exit 125
snapshot() {
  local output="$1" repo alias physical head status status_sha want want_physical
  : > "$output"
  for repo in sn server operator-proxy connect sdk glog goidenticons proxy userwireguard vault xops config warp; do
    alias="$base/$repo"
    test -e "$alias" || return 125
    test -e "$alias/.git" || return 125
    physical="$(readlink -f -- "$alias")"
    test -e "$physical/.git" || return 125
    head="$(git -C "$alias" rev-parse --verify HEAD^{commit})"
    case "$repo" in
      sn) want="$expected_sn"; want_physical="$base/sn" ;;
      server) want=0f095a639e111f71d231cd6f792a191cbc6b0a69; want_physical=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/server ;;
      operator-proxy) want=448369740c70eb2e642bde9d36f3708fe9ad4b5d; want_physical=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/operator-proxy ;;
      connect) want=2a0f603e8076224fdab4d9f56e5a0a557ed513d9; want_physical=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/connect ;;
      sdk) want=0dc2fb0f6f8a7e484e96bbdde09a690ae108dc8b; want_physical=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sdk ;;
      glog) want=892ade4a6be396b32ea82a550f243190b5992180; want_physical=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/glog ;;
      goidenticons) want=325750b38314313dc5f44c880ab6f12f6c1ecb3c; want_physical=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/goidenticons ;;
      proxy) want=6204ae7df2a9868bbb3a7b61231917a36e4f5c9f; want_physical=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/proxy ;;
      userwireguard) want=85fb1ca4086fa5dbfcda526bec7a17a894e691b9; want_physical=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/userwireguard ;;
      vault) want=8b2f481dbe87092d0c1742274712a6f805c1c375; want_physical=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/vault ;;
      xops) want=42bfe0be2a7a7c51bbda87fb44886424604f509e; want_physical=/home/by/urnetwork/temp/xops-rpc-vulnerability-assertion-correction-20260914/xops ;;
      config) want=268f11b7e7d9c43eda3fd6f34447d26c2f0c21dc; want_physical=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/config ;;
      warp) want=7498864c7cd3605aad3c43eabfab9008ed7f7228; want_physical=/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/warp ;;
    esac
    test "$head" = "$want" || return 125
    test "$physical" = "$want_physical" || return 125
    status="$(git -C "$alias" status --porcelain=v1 --untracked-files=all)"
    test -z "$status" || return 125
    status_sha="$(printf '%s' "$status" | sha256sum | awk '{print $1}')"
    printf '%s\t%s\t%s\t%s\t0\t%s\n' "$repo" "$alias" "$physical" "$head" "$status_sha" >> "$output"
  done
}
test -d "$runtime" && test ! -L "$runtime"
test -d "$runtime/tmp" && test ! -L "$runtime/tmp" && test "$(stat -c '%a' "$runtime/tmp")" = 700
test -d "$runtime/gotmp" && test ! -L "$runtime/gotmp" && test "$(stat -c '%a' "$runtime/gotmp")" = 700
test -d "$warm_cache" && test ! -L "$warm_cache" && test "$(stat -c '%a' "$warm_cache")" = 700
test -d "$modcache" && test ! -L "$modcache"
test -d "$source_root" && test ! -L "$source_root"
test ! -e "$binary" && test ! -L "$binary"
snapshot "$source_pair"
test "$(wc -l < "$source_pair")" = 13
sha256sum "$source_pair" > "$runtime/meta/source-pair-renderer.before.sha256"
date -u +%Y-%m-%dT%H:%M:%SZ > "$runtime/logs/build.started-at"
set +e
(
  cd "$source_root" || exit 125
  exec env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS='-mod=readonly -p=4' GOPROXY=off GOSUMDB=off GOWORK=off GOENV=off GOTOOLCHAIN=local GOVCS='*:off' TMPDIR="$runtime/tmp" GOTMPDIR="$runtime/gotmp" GOCACHE="$warm_cache" GOMODCACHE="$modcache" go build -trimpath -buildvcs=true -o "$binary" ./sim-testnet
) > "$runtime/logs/build.stdout" 2> "$runtime/logs/build.stderr"
build_status=$?
set -e
printf '%s\n' "$build_status" > "$runtime/logs/build.exit"
date -u +%Y-%m-%dT%H:%M:%SZ > "$runtime/logs/build.finished-at"
snapshot "$runtime/meta/source-pair-renderer.after.tsv"
cmp -s "$source_pair" "$runtime/meta/source-pair-renderer.after.tsv"
sha256sum "$runtime/meta/source-pair-renderer.after.tsv" > "$runtime/meta/source-pair-renderer.after.sha256"
test "$build_status" = 0 || exit "$build_status"
test -f "$binary" && test ! -L "$binary" && test "$(stat -c '%a' "$binary")" = 700
sha256sum "$binary" > "$runtime/meta/renderer.sha256"
stat -c '%a %s %i %n' "$binary" > "$runtime/meta/renderer.stat"
go version -m "$binary" > "$runtime/meta/renderer.buildinfo.txt"
before_sha="$(awk '{print $1}' "$runtime/meta/source-pair-renderer.before.sha256")"
after_sha="$(awk '{print $1}' "$runtime/meta/source-pair-renderer.after.sha256")"
binary_sha="$(awk '{print $1}' "$runtime/meta/renderer.sha256")"
binary_bytes="$(stat -c '%s' "$binary")"
printf '{\n  "kind":"readonly-runtime-config-identity-renderer-cli-build",\n  "build_exit":0,\n  "repos_before_after_identical":true,\n  "repos_before_sha256":"%s",\n  "repos_after_sha256":"%s",\n  "artifact":{"path":"%s","sha256":"%s","bytes":%s,"mode":"0700"},\n  "source_head":"%s",\n  "build_flags":{"trimpath":true,"buildvcs":true},\n  "vcs_attestation_required":false,\n  "native_or_rpc_action":"none"\n}\n' "$before_sha" "$after_sha" "$binary" "$binary_sha" "$binary_bytes" "$expected_sn" > "$runtime/RESULT-READONLY-RENDERER.json"
sha256sum "$runtime/RESULT-READONLY-RENDERER.json" > "$runtime/RESULT-READONLY-RENDERER.json.sha256"
