#!/usr/bin/env bash
set -euo pipefail
render_capture='/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/lock-preview-runtime458-ade970a'
render_previous='/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/lock-preview-runtime458-df98472'
render_source='/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/sn'
render_xops='/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/xops'
trap 'render_outer_exit=$?; printf "%s\n" "$render_outer_exit" >"$render_capture/outer.exit"; date -u +%Y-%m-%dT%H:%M:%SZ >"$render_capture/outer.finished-at"' EXIT
date -u +%Y-%m-%dT%H:%M:%SZ >"$render_capture/outer.started-at"
printf '%s\n' "$$" >"$render_capture/outer.pid"
mkdir -m 700 "$render_capture/tmp" "$render_capture/gotmp"
sha256sum '/home/by/urnetwork/temp/sn-runtime458-history-capacity-20260914/terra-runtime/renderer-cli-runtime458-df98472-r2/sim-testnet' >"$render_capture/executable.sha256"
test "$(cut -d ' ' -f 1 "$render_capture/executable.sha256")" = '2b8e0e0695e75d996e29991fd81f15c43c95dc4d6561523235e8311a6d64c0e5'
while IFS=$'\t' read -r render_repo render_expected render_clean; do
  if [[ "$render_repo" == "$render_source" ]]; then render_expected='846b3b4749767da72c155f824986998e305757fa'; fi
  if [[ "$render_repo" == "$render_xops" ]]; then render_expected='42bfe0be2a7a7c51bbda87fb44886424604f509e'; fi
  render_head="$(git -C "$render_repo" rev-parse HEAD)"
  render_status="$(git -C "$render_repo" status --porcelain=v1 --untracked-files=all | sha256sum | cut -d ' ' -f 1)"
  printf '%s\t%s\t%s\n' "$render_repo" "$render_head" "$render_status" >>"$render_capture/repos.before.tsv"
  test "$render_head" = "$render_expected"
  test "$render_status" = "$render_clean"
done <"$render_previous/repos.before.tsv"
test "$(wc -l <"$render_capture/repos.before.tsv")" -eq 19
sha256sum '/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/sn/deploy/testnet/release.lock.yml' >"$render_capture/lock.before.sha256"
test "$(cut -d ' ' -f 1 "$render_capture/lock.before.sha256")" = 'e72b2146a1cbbd59a54b0424a30478aa9a475f8d82369c028c4fe22e8f2d71c2'
export GOMAXPROCS='1'
export GOENV='off'
export GOWORK='off'
export GOPROXY='off'
export GOSUMDB='off'
export GOTOOLCHAIN='local'
export GOVCS='*:off'
export GOFLAGS='-mod=readonly -p=1'
export TMPDIR='/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/lock-preview-runtime458-ade970a/tmp'
export GOTMPDIR='/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/lock-preview-runtime458-ade970a/gotmp'
export GOMODCACHE='/home/by/go/pkg/mod'
export GOCACHE='/home/by/urnetwork/temp/sn-carried-fleet-qualification-20260913/runtime/gocache'
cd '/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/sn'
'/home/by/urnetwork/temp/sn-runtime458-history-capacity-20260914/terra-runtime/renderer-cli-runtime458-df98472-r2/sim-testnet' 'release-lock' '--config' '/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/sn/sim-testnet/testnet.yml' '--sn-repo' '/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/sn' '--server-repo' '/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/server' '--operator-proxy-repo' '/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/operator-proxy' '--vault-repo' '/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/vault' '--platform-config-repo' '/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/workspace/config' >"$render_capture/candidate.yml" 2>"$render_capture/stderr.log" &
render_pid=$!
printf '%s\n' "$render_pid" >"$render_capture/renderer.pid"
if wait "$render_pid"; then render_body_exit=0; else render_body_exit=$?; fi
printf '%s\n' "$render_body_exit" >"$render_capture/body.exit"
while IFS=$'\t' read -r render_repo render_expected render_clean; do
  render_head="$(git -C "$render_repo" rev-parse HEAD)"
  render_status="$(git -C "$render_repo" status --porcelain=v1 --untracked-files=all | sha256sum | cut -d ' ' -f 1)"
  printf '%s\t%s\t%s\n' "$render_repo" "$render_head" "$render_status" >>"$render_capture/repos.after.tsv"
done <"$render_capture/repos.before.tsv"
sha256sum '/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/sn/deploy/testnet/release.lock.yml' >"$render_capture/lock.after.sha256"
cmp "$render_capture/repos.before.tsv" "$render_capture/repos.after.tsv"
cmp "$render_capture/lock.before.sha256" "$render_capture/lock.after.sha256"
exit "$render_body_exit"
