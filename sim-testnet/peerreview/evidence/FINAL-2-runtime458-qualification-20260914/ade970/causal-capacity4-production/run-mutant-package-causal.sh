#!/usr/bin/env bash
set -euo pipefail
umask 077
[[ $# -eq 1 ]] || exit 64
source "$1"
: "${capture_root:?}" "${candidate_source:?}" "${source_parent:?}" "${expected_sn:?}" "${worktree_source:?}" "${stage:?}" "${patch:?}" "${package_dir:?}" "${package_import:?}" "${binary:?}" "${selector:?}" "${outcomes:?}" "${literals:?}"
snapshot="$capture_root/scripts/snapshot.sh"
checker="$candidate_source/scripts/check-qualification-events.py"
mkdir -p "$stage/tmp" "$stage/gotmp"
for f in "$patch" "$binary" "$selector" "$outcomes" "$literals" "$snapshot" "$checker"; do [[ -f "$f" && ! -L "$f" ]] || { printf 'missing causal input: %s\n' "$f" >&2; exit 64; }; done
[[ "$(git -C "$candidate_source" rev-parse HEAD)" == "$expected_sn" ]]
[[ -z "$(git -C "$candidate_source" status --porcelain)" ]]
[[ "$(git -C "$worktree_source" rev-parse HEAD)" == "$expected_sn" ]]
git -C "$worktree_source" diff --check > "$stage/mutant.diff-check.before"
git -C "$worktree_source" diff > "$stage/mutant.diff.before"
git -C "$worktree_source" apply --reverse --check "$patch"
"$snapshot" "$candidate_source" "$stage/candidate.repos.pre.tsv" "$expected_sn" "$source_parent"
cmp -s "$capture_root/meta/repos.before.tsv" "$stage/candidate.repos.pre.tsv"
sha256sum "$binary" "$patch" "$selector" "$outcomes" "$literals" "$checker" > "$stage/inputs.before.sha256"
sha256sum "$binary" > "$stage/binary.before.sha256"
( cd "$worktree_source/$package_dir" && printf 'cwd=%s\npackage=%s\n' "$PWD" "$package_import" ) > "$stage/cwd.fixture-guard.meta"
if [[ "$package_dir" == sim-testnet ]]; then printf 'fixture=testnet.yml\nfixture=../deploy/testnet/policy-v1.yml\n' >> "$stage/cwd.fixture-guard.meta"; fi
set +e
( cd "$worktree_source/$package_dir" && env GOMAXPROCS=4 TMPDIR="$stage/tmp" GOTMPDIR="$stage/gotmp" timeout --foreground --kill-after=15s 660s "$binary" -test.list "$(cat "$selector")" ) > "$stage/list.raw" 2> "$stage/list.stderr"
list_status=$?
set -e
printf '%s\n' "$list_status" > "$stage/list.command.exit"
awk '/^Test[A-Za-z0-9_]+$/ {print}' "$stage/list.raw" | LC_ALL=C sort > "$stage/list.roots.sorted"
cut -f1 "$outcomes" > "$stage/expected.roots.txt"
if diff -u "$stage/expected.roots.txt" "$stage/list.roots.sorted" > "$stage/list.diff"; then list_diff=0; else list_diff=$?; fi
printf 'compiled_count=%s\nexpected_count=%s\nlist_exit=%s\ndiff_status=%s\n' "$(wc -l < "$stage/list.roots.sorted")" "$(wc -l < "$stage/expected.roots.txt")" "$list_status" "$list_diff" > "$stage/inventory.validation"
[[ "$list_status" == 0 && "$list_diff" == 0 ]]
date -u +%Y-%m-%dT%H:%M:%S.%NZ > "$stage/body.started-at"
set +e
( cd "$worktree_source/$package_dir" && env GOMAXPROCS=4 TMPDIR="$stage/tmp" GOTMPDIR="$stage/gotmp" timeout --foreground --kill-after=15s 660s "$binary" -test.v -test.count=1 -test.parallel=4 -test.timeout=600s -test.run "$(cat "$selector")" ) > "$stage/body.raw" 2> "$stage/body.stderr"
body_status=$?
printf '%s\n' "$body_status" > "$stage/body.exit"
date -u +%Y-%m-%dT%H:%M:%S.%NZ > "$stage/body.finished-at"
( cd "$worktree_source/$package_dir" && go tool test2json -t -p "$package_import" ) < "$stage/body.raw" > "$stage/events.json" 2> "$stage/converter.stderr"
converter_status=$?
printf '%s\n' "$converter_status" > "$stage/converter.exit"
python3 "$checker" --events "$stage/events.json" --outcomes "$outcomes" --failure-literals "$literals" --package "$package_import" --binary-exit "$body_status" > "$stage/events.validation.json" 2> "$stage/events.validation.stderr"
validation_status=$?
printf '%s\n' "$validation_status" > "$stage/validation.exit"
set -e
sha256sum "$binary" > "$stage/binary.after.sha256"
git -C "$worktree_source" diff --check > "$stage/mutant.diff-check.after"
git -C "$worktree_source" diff > "$stage/mutant.diff.after"
cmp -s "$stage/mutant.diff.before" "$stage/mutant.diff.after"
git -C "$worktree_source" apply --reverse --check "$patch"
[[ -z "$(git -C "$candidate_source" status --porcelain)" ]]
"$snapshot" "$candidate_source" "$stage/candidate.repos.post.tsv" "$expected_sn" "$source_parent"
cmp -s "$stage/candidate.repos.pre.tsv" "$stage/candidate.repos.post.tsv"
cmp -s "$stage/binary.before.sha256" "$stage/binary.after.sha256"
printf 'body_exit=%s\nconverter_exit=%s\nvalidation_exit=%s\ncandidate_repos_equal=true\nbinary_equal=true\nmutant_diff_equal=true\nreverse_apply_check=true\n' "$body_status" "$converter_status" "$validation_status" > "$stage/result.status"
[[ "$body_status" == 1 && "$converter_status" == 0 && "$validation_status" == 0 ]]
