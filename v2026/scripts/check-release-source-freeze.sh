#!/usr/bin/env bash
set -euo pipefail

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# pinned, the default, checks every checkout's provenance before and after the
# module checks. local, the aggregate gate's local dependency mode, keeps the
# module inventory and tidy checks over the working checkouts as they are, and
# never reads the vault checkout.
dependency_mode=pinned
if [[ "${1:-}" == --dependency-mode ]]; then
  if (( $# < 2 )); then
    echo 'source freeze --dependency-mode needs pinned or local' >&2
    exit 2
  fi
  dependency_mode="$2"
  shift 2
fi
case "$dependency_mode" in
  pinned | local) ;;
  *)
    echo "source freeze dependency mode must be pinned or local: $dependency_mode" >&2
    exit 2
    ;;
esac
workspace="${1:-$(dirname "$sn_repo")}"

release_repos=(sn server connect sdk glog goidenticons proxy userwireguard warp gvisor vault xops config)
# A module is live when a released binary or library builds from it: a release
# build's main module, or a module whose packages one links. connect/sctp backs
# every github.com/pion/sctp replacement, and SN's substrate RPC fork backs
# SN's and the server's replacement of the centrifuge client.
live_modules=(
  connect
  connect/sctp
  glog
  goidenticons
  proxy
  sdk
  sdk/build
  sdk/cgo
  sdk/js
  server
  sn
  sn/third_party/go-substrate-rpc-client
  sn/third_party/npipe
  userwireguard
  warp
  xops/echo
)

# These modules are outside SN release qualification: no release build links
# or requires them. Repository provenance and the complete module inventory
# still include them.
# - connect/tools/flightgate-devices drives the physical two-phone rig.
# - connect/vlessinterop tests the VLESS client against Xray-core.
# - sdk/cgo/build holds native build output and no package.
# - sdk/packaging builds and publishes the language packages around sdk/cgo.
# - server/connect/sim-latency/baseline is a separate dataset. Its module
#   boundary keeps preserved analysis inputs out of executable server packages.
non_release_modules=(
  connect/tools/flightgate-devices
  connect/vlessinterop
  sdk/cgo/build
  sdk/packaging
  server/connect/sim-latency/baseline
)

# Release builds compile the gVisor fork through the gvisor.dev/gvisor
# replacements of SN and the server, so its repository provenance is checked
# like every other. Its go.mod is the fork's and is not kept tidy. No gate job
# or release build uses it as a main module, and the tidy checks of SN, the
# server and Connect cover the module graphs that consume it, so it is
# inventoried and not tidied.
fork_modules=(gvisor)

expected_upstream() {
  case "$1" in
    glog | userwireguard) printf 'origin/master\n' ;;
    gvisor) printf 'origin/go\n' ;;
    sn | server | connect | sdk | goidenticons | proxy | warp | vault | xops | config) printf 'origin/main\n' ;;
    *) return 1 ;;
  esac
}

expected_origin() {
  case "$1" in
    sn) printf 'github.com/urfoundation/sn\n' ;;
    server) printf 'github.com/urnetwork/server\n' ;;
    connect) printf 'github.com/urnetwork/connect\n' ;;
    sdk) printf 'github.com/urnetwork/sdk\n' ;;
    glog) printf 'github.com/urnetwork/glog\n' ;;
    goidenticons) printf 'github.com/urnetwork/goidenticons\n' ;;
    proxy) printf 'github.com/urnetwork/proxy\n' ;;
    userwireguard) printf 'github.com/urnetwork/userwireguard\n' ;;
    warp) printf 'github.com/urnetwork/warp\n' ;;
    gvisor) printf 'github.com/urnetwork/gvisor\n' ;;
    vault) printf 'github.com/urnetwork/vault\n' ;;
    xops) printf 'github.com/urnetwork/xops\n' ;;
    config) printf 'github.com/urnetwork/config\n' ;;
    *) return 1 ;;
  esac
}

normalize_github_origin() {
  local url="$1"
  local slug
  case "$url" in
    git@github.com:*.git)
      slug="${url#git@github.com:}"
      slug="${slug%.git}"
      ;;
    git@github.com:*)
      slug="${url#git@github.com:}"
      ;;
    https://github.com/*.git)
      slug="${url#https://github.com/}"
      slug="${slug%.git}"
      ;;
    https://github.com/*)
      slug="${url#https://github.com/}"
      ;;
    *)
      return 1
      ;;
  esac
  if [[ "$slug" != */* || "$slug" == */*/* || "$slug" == */ || "$slug" == /* ]]; then
    return 1
  fi
  printf 'github.com/%s\n' "$slug"
}

checkout_repos=()
for repo in "${release_repos[@]}"; do
  if [[ "$dependency_mode" == local && "$repo" == vault ]]; then
    continue
  fi
  checkout_repos+=("$repo")
done

for repo in "${checkout_repos[@]}"; do
  if [[ ! -e "$workspace/$repo/.git" ]]; then
    echo "release repository is missing or not a Git checkout: $workspace/$repo" >&2
    exit 1
  fi
done

snapshot_release_repositories() {
  local refresh_remote="$1"
  local repo root status revision origin_url origin want_origin upstream want_upstream remote_branch upstream_revision
  for repo in "${release_repos[@]}"; do
    root="$workspace/$repo"
    status="$(git -C "$root" status --porcelain=v1 --untracked-files=all)"
    if [[ -n "$status" ]]; then
      echo "release repository has tracked, staged, or untracked changes: $repo" >&2
      printf '%s\n' "$status" >&2
      return 1
    fi
    revision="$(git -C "$root" rev-parse --verify 'HEAD^{commit}')"
    if [[ ! "$revision" =~ ^[0-9a-f]{40}$ ]]; then
      echo "release repository returned a non-canonical revision: $repo" >&2
      return 1
    fi
    origin_url="$(git -C "$root" config --get remote.origin.url)"
    if ! origin="$(normalize_github_origin "$origin_url")"; then
      echo "release repository $repo has unsupported origin URL: $origin_url" >&2
      return 1
    fi
    want_origin="$(expected_origin "$repo")"
    if [[ "$origin" != "$want_origin" ]]; then
      echo "release repository $repo has origin $origin, want $want_origin" >&2
      return 1
    fi
    upstream="$(git -C "$root" rev-parse --abbrev-ref --symbolic-full-name '@{upstream}')"
    want_upstream="$(expected_upstream "$repo")"
    if [[ "$upstream" != "$want_upstream" ]]; then
      echo "release repository $repo tracks $upstream, want $want_upstream" >&2
      return 1
    fi
    if [[ "$refresh_remote" == true ]]; then
      remote_branch="${want_upstream#origin/}"
      if ! git -C "$root" fetch --quiet --no-tags origin "+refs/heads/$remote_branch:refs/remotes/origin/$remote_branch"; then
        echo "release repository $repo could not refresh $want_upstream" >&2
        return 1
      fi
    fi
    upstream_revision="$(git -C "$root" rev-parse --verify "$upstream^{commit}")"
    # SN remains the current published entry point. Dependencies may retain
    # the exact tested commit while their canonical upstream advances.
    # Both enclosing snapshots still record the checked-out commit itself.
    if [[ "$repo" == sn ]]; then
      if [[ "$revision" != "$upstream_revision" ]]; then
        echo "release repository $repo revision $revision differs from $upstream at $upstream_revision" >&2
        return 1
      fi
    elif ! git -C "$root" merge-base --is-ancestor "$revision" "$upstream_revision"; then
      echo "release repository $repo revision $revision is not reachable from $upstream at $upstream_revision" >&2
      return 1
    fi
    printf '%s\t%s\t%s\t%s\n' "$repo" "$revision" "$upstream" "$origin"
  done
}

# Establish clean, canonical, freshly fetched sources before executing any
# Go toolchain input.
if [[ "$dependency_mode" == pinned ]]; then
  initial_release_snapshot="$(snapshot_release_repositories true)"
else
  echo 'source freeze: local mode checks the module inventory and tidiness of the working checkouts, not their provenance' >&2
fi

discovered_modules="$({
  for repo in "${checkout_repos[@]}"; do
    tracked_files="$(git -C "$workspace/$repo" ls-files --cached)"
    while IFS= read -r tracked_file; do
      if [[ "$tracked_file" == go.mod ]]; then
        printf '%s\n' "$repo"
      elif [[ "$tracked_file" == */go.mod ]]; then
        printf '%s/%s\n' "$repo" "${tracked_file%/go.mod}"
      fi
    done <<<"$tracked_files"
  done
} | LC_ALL=C sort -u)"
expected_modules="$(printf '%s\n' "${live_modules[@]}" "${non_release_modules[@]}" "${fork_modules[@]}" | LC_ALL=C sort -u)"
if [[ "$discovered_modules" != "$expected_modules" ]]; then
  echo "release Go module inventory differs from its reviewed classification" >&2
  diff -u <(printf '%s\n' "$expected_modules") <(printf '%s\n' "$discovered_modules") >&2 || true
  exit 1
fi

for module in "${live_modules[@]}"; do
  echo "verify tidy Go module: $module" >&2
  (
    cd "$workspace/$module"
    GOWORK=off go mod tidy -diff
  ) >&2
done

# Emit one canonical snapshot on stdout. The second validation detects source
# changes caused by the toolchain while the enclosing gate repeats the
# fetched check after all long-running tests. Local mode emits none.
if [[ "$dependency_mode" == local ]]; then
  exit 0
fi
final_release_snapshot="$(snapshot_release_repositories false)"
if [[ "$final_release_snapshot" != "$initial_release_snapshot" ]]; then
  echo "release repository snapshot changed during source-freeze validation" >&2
  diff -u <(printf '%s\n' "$initial_release_snapshot") <(printf '%s\n' "$final_release_snapshot") >&2 || true
  exit 1
fi
printf '%s\n' "$final_release_snapshot"
