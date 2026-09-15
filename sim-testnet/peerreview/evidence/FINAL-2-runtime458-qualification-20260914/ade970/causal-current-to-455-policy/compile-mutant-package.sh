#!/usr/bin/env bash
set -euo pipefail
umask 077
[[ $# -eq 2 ]] || exit 64
config=$1; package_name=$2; source "$config"
case "$package_name" in
  crv4) package=./crv4; binary="$stage/normal/compile3-crv4/crv4.normal.test";;
  miner) package=./miner; binary="$stage/normal/compile3-miner/miner.normal.test";;
  validator) package=./validator; binary="$stage/normal/compile3-validator/validator.normal.test";;
  sim-testnet) package=./sim-testnet; binary="$stage/normal/compile3-sim-testnet/sim-testnet.normal.test";;
  *) exit 64;;
esac
build_stage=$(dirname "$binary")
mkdir -p "$build_stage/tmp" "$build_stage/gotmp"
[[ "$(git -C "$candidate_source" rev-parse HEAD)" == "$expected_sn" ]]
[[ -z "$(git -C "$candidate_source" status --porcelain)" ]]
[[ "$(git -C "$worktree_source" rev-parse HEAD)" == "$expected_sn" ]]
git -C "$worktree_source" diff --check > "$build_stage/mutant.diff-check.pre"
git -C "$worktree_source" diff > "$build_stage/mutant.diff.pre"
"$capture_root/scripts/snapshot.sh" "$candidate_source" "$build_stage/candidate.repos.pre.tsv" "$expected_sn" "$source_parent"
cmp -s "$stage/candidate.repos.before.tsv" "$build_stage/candidate.repos.pre.tsv"
set +e
( cd "$worktree_source" && env PATH="/home/by/.foundry/bin:$PATH" GOMAXPROCS=4 GOFLAGS=-mod=readonly GOPROXY=off GOSUMDB=off GONOPROXY=none GONOSUMDB=none GOPRIVATE='' GOVCS='*:off' GOWORK=off GOENV=off GOTOOLCHAIN=local GOMODCACHE="$gomodcache" GOCACHE="$warm_gocache" TMPDIR="$build_stage/tmp" GOTMPDIR="$build_stage/gotmp" timeout --foreground --kill-after=15s 600s go test -c -p=4 -o "$binary" "$package" )
status=$?
set -e
printf '%s\n' "$status" > "$build_stage/build.command.exit"
[[ -f "$binary" ]] && sha256sum "$binary" > "$build_stage/binary.postbuild.sha256"
git -C "$worktree_source" diff --check > "$build_stage/mutant.diff-check.post"
git -C "$worktree_source" diff > "$build_stage/mutant.diff.post"
cmp -s "$build_stage/mutant.diff.pre" "$build_stage/mutant.diff.post"
"$capture_root/scripts/snapshot.sh" "$candidate_source" "$build_stage/candidate.repos.post.tsv" "$expected_sn" "$source_parent"
cmp -s "$build_stage/candidate.repos.pre.tsv" "$build_stage/candidate.repos.post.tsv"
printf 'candidate_repos_equal=true\nmutant_diff_equal=true\n' > "$build_stage/build.fences"
exit "$status"
