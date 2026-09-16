#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/terra/retry-1/positive/compile/race'
source_root='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/workspace/sn'
work_root='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/workspace'
expected_commit='e109ac35c5ea5ff5006040c2987118e99627e863'
selector='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/terra/inputs/selector.txt'
expected_tests='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/terra/inputs/expected-tests.txt'
selection='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/selection.json'
variant=''
binary='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/terra/retry-1/positive/compile/race/sim-testnet.race.test'
race='true'
snapshot_dependencies() {
  local output="$1"
  local repo path resolved head status
  printf 'repo\tconfigured_path\tresolved_path\thead\tstatus_bytes\tstatus_sha256\n' > "$output"
  for repo in connect server warp operator-proxy proxy userwireguard sdk glog goidenticons vault config; do
    path="$work_root/$repo"
    resolved="$(readlink -f -- "$path")"
    [[ -d "$path" && -L "$path" && -d "$resolved/.git" ]] || return 125
    head="$(git -C "$resolved" rev-parse 'HEAD^{commit}')" || return 125
    status="$(git -C "$resolved" status --porcelain=v1 --untracked-files=all)" || return 125
    [[ -z "$status" ]] || return 125
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$repo" "$path" "$resolved" "$head" "${#status}" "$(printf '%s' "$status" | sha256sum | awk '{print $1}')" >> "$output"
  done
}
input_fence() {
  local output="$1"
  local files
  files=("$source_root/sim-testnet/main.go" "$source_root/sim-testnet/executor.go" "$source_root/sim-testnet/strict_resume_campaign.go" "$source_root/sim-testnet/strict_resume_campaign_test.go" "$source_root/go.mod" "$source_root/go.sum" "$selection" "$selector" "$expected_tests")
  if [[ -n "$variant" ]]; then files+=("$variant"); fi
  sha256sum "${files[@]}" > "$output"
}
[[ "$(git -C "$source_root" rev-parse HEAD)" == "$expected_commit" ]] || exit 125
[[ -z "$(git -C "$source_root" status --porcelain=v1 --untracked-files=all)" ]] || exit 125
git -C "$source_root" diff --check || exit 125
[[ ! -e "$binary" && ! -L "$binary" ]] || exit 125
printf '%s\n' "$(git -C "$source_root" rev-parse HEAD)" > "$capture/source.commit.before"
git -C "$source_root" status --porcelain=v1 --untracked-files=all > "$capture/source.status.before"
git -C "$source_root" diff --check > "$capture/source.diff-check.before"
snapshot_dependencies "$capture/dependencies.before.tsv" || exit 125
input_fence "$capture/input.before.sha256"
printf '%s\n' "$(/usr/local/go/bin/go version)" > "$capture/go.version"
cd "$source_root" || exit 125
set +e
if [[ "$race" == true ]]; then
  env GOMAXPROCS=4 GOFLAGS='-mod=readonly -p=1' GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOVCS='*:off' GOCACHE='/mnt/data/sn-testnet/gocache' GOMODCACHE='/home/by/go/pkg/mod' TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" /usr/local/go/bin/go test -race -c -o "$binary" ./sim-testnet
else
  env GOMAXPROCS=4 GOFLAGS='-mod=readonly -p=1' GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local GOVCS='*:off' GOCACHE='/mnt/data/sn-testnet/gocache' GOMODCACHE='/home/by/go/pkg/mod' TMPDIR="$capture/tmp" GOTMPDIR="$capture/gotmp" /usr/local/go/bin/go test -c -o "$binary" ./sim-testnet
fi
build_rc=$?
set -e
if (( build_rc == 0 )); then chmod 700 "$binary"; sha256sum "$binary" > "$capture/binary.sha256"; stat -c '%a %s %i %n' "$binary" > "$capture/binary.stat"; fi
printf '%s\n' "$(git -C "$source_root" rev-parse HEAD)" > "$capture/source.commit.after"
git -C "$source_root" status --porcelain=v1 --untracked-files=all > "$capture/source.status.after"
git -C "$source_root" diff --check > "$capture/source.diff-check.after"
snapshot_dependencies "$capture/dependencies.after.tsv"
input_fence "$capture/input.after.sha256"
cmp -s "$capture/dependencies.before.tsv" "$capture/dependencies.after.tsv"
deps_rc=$?
cmp -s "$capture/input.before.sha256" "$capture/input.after.sha256"
inputs_rc=$?
if [[ -z "$(cat "$capture/source.status.after")" && -z "$(cat "$capture/source.diff-check.after")" && $deps_rc -eq 0 && $inputs_rc -eq 0 ]]; then printf 'source_clean=true\ndependencies_unchanged=true\ninputs_unchanged=true\n' > "$capture/fences.after"; else printf 'source_clean=false_or_dependency_or_input_changed\n' > "$capture/fences.after"; build_rc=125; fi
exit "$build_rc"
