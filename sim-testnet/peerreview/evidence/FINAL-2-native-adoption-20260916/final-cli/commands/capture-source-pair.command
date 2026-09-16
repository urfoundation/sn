#!/usr/bin/env bash
set -euo pipefail
umask 077
workspace='/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical'
output="$1"
: > "$output"
for repo in sn server operator-proxy connect sdk glog goidenticons proxy userwireguard vault xops config warp; do
  root="$workspace/$repo"
  [[ -d "$root" && -d "$root/.git" && ! -L "$root" ]] || exit 125
  physical="$(readlink -f -- "$root")"
  [[ "$physical" == "$root" ]] || exit 125
  gitdir="$(git -C "$root" rev-parse --absolute-git-dir)"
  [[ -d "$gitdir" ]] || exit 125
  head="$(git -C "$root" rev-parse --verify 'HEAD^{commit}')"
  status="$(git -C "$root" status --porcelain=v1 --untracked-files=all)"
  [[ -z "$status" ]] || exit 125
  status_sha="$(printf '%s' "$status" | sha256sum | awk '{print $1}')"
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$repo" "$root" "$physical" "$head" "${#status}" "$status_sha" >> "$output"
done
[[ "$(wc -l < "$output")" == 13 ]]
