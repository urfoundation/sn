#!/usr/bin/env bash
set -euo pipefail

sn_repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workspace="${1:-$(dirname "$sn_repo")}"

release_repos=(sn server operator-proxy connect sdk glog goidenticons proxy userwireguard vault xops config)
live_modules=(
  connect
  glog
  goidenticons
  operator-proxy
  proxy
  sdk
  sdk/build
  sdk/cgo
  sdk/js
  server
  sn
  sn/third_party/npipe
  userwireguard
  xops/echo
  xops/router
)

# This is a content-addressed historical dataset, not a buildable source
# module. Its minimal go.mod prevents preserved calibration test inputs from
# entering server ./...; those inputs require their historical patches and an
# unpublished server revision. The archive's verifier authenticates every
# path and byte, including go.mod, and is the only permitted tidy exception.
archived_modules=(server/connect/sim-latency/baseline)

expected_upstream() {
  case "$1" in
    glog | userwireguard) printf 'origin/master\n' ;;
    sn | server | operator-proxy | connect | sdk | goidenticons | proxy | vault | xops | config) printf 'origin/main\n' ;;
    *) return 1 ;;
  esac
}

expected_origin() {
  case "$1" in
    sn) printf 'github.com/urfoundation/sn\n' ;;
    server) printf 'github.com/urnetwork/server\n' ;;
    operator-proxy) printf 'github.com/urnetwork/operator-proxy\n' ;;
    connect) printf 'github.com/urnetwork/connect\n' ;;
    sdk) printf 'github.com/urnetwork/sdk\n' ;;
    glog) printf 'github.com/urnetwork/glog\n' ;;
    goidenticons) printf 'github.com/urnetwork/goidenticons\n' ;;
    proxy) printf 'github.com/urnetwork/proxy\n' ;;
    userwireguard) printf 'github.com/urnetwork/userwireguard\n' ;;
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

for repo in "${release_repos[@]}"; do
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
    if [[ "$revision" != "$upstream_revision" ]]; then
      echo "release repository $repo revision $revision differs from $upstream at $upstream_revision" >&2
      return 1
    fi
    printf '%s\t%s\t%s\t%s\n' "$repo" "$revision" "$upstream" "$origin"
  done
}

# Establish clean, canonical, freshly fetched sources before executing any
# repository-provided verifier or Go toolchain input.
initial_release_snapshot="$(snapshot_release_repositories true)"

discovered_modules="$({
  for repo in "${release_repos[@]}"; do
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
expected_modules="$(printf '%s\n' "${live_modules[@]}" "${archived_modules[@]}" | LC_ALL=C sort -u)"
if [[ "$discovered_modules" != "$expected_modules" ]]; then
  echo "release Go module inventory differs from its reviewed classification" >&2
  diff -u <(printf '%s\n' "$expected_modules") <(printf '%s\n' "$discovered_modules") >&2 || true
  exit 1
fi

archive_root="$workspace/${archived_modules[0]}"
if [[ ${#archived_modules[@]} -ne 1 || ! -x "$archive_root/verify.sh" || ! -f "$archive_root/MANIFEST.sha256" || ! -f "$archive_root/README.md" ]]; then
  echo "reviewed archived Go module exception is incomplete" >&2
  exit 1
fi
if ! grep -Fq 'measurement inputs are Go test source files' "$archive_root/README.md" ||
  ! grep -Fq 'module file is covered by the manifest' "$archive_root/README.md"; then
  echo "archived Go module exception has lost its non-buildable provenance" >&2
  exit 1
fi
"$archive_root/verify.sh" >&2

for module in "${live_modules[@]}"; do
  echo "verify tidy Go module: $module" >&2
  (
    cd "$workspace/$module"
    GOWORK=off go mod tidy -diff
  ) >&2
done

# Emit one canonical snapshot on stdout. The second validation detects source
# changes caused by a verifier or tool while the enclosing gate repeats the
# fetched check after all long-running tests.
final_release_snapshot="$(snapshot_release_repositories false)"
if [[ "$final_release_snapshot" != "$initial_release_snapshot" ]]; then
  echo "release repository snapshot changed during source-freeze validation" >&2
  diff -u <(printf '%s\n' "$initial_release_snapshot") <(printf '%s\n' "$final_release_snapshot") >&2 || true
  exit 1
fi
printf '%s\n' "$final_release_snapshot"
