#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/terra/retry-3/positive/race/body'
source_root='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/workspace/sn'
work_root='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/workspace'
expected_commit='e109ac35c5ea5ff5006040c2987118e99627e863'
binary='/mnt/data/sn-testnet/qualification/resume-campaign-handoff-20260916-r1/terra/retry-1/positive/compile/race/sim-testnet.race.test'
selector="$(cat "$capture/selector.txt")"
expected_exit="$(cat "$capture/expected-body-exit")"
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
[[ "$(git -C "$source_root" rev-parse HEAD)" == "$expected_commit" ]] || exit 125
[[ -z "$(git -C "$source_root" status --porcelain=v1 --untracked-files=all)" ]] || exit 125
git -C "$source_root" diff --check || exit 125
[[ -f "$binary" && ! -L "$binary" ]] || exit 125
sha256sum "$binary" > "$capture/binary.before.sha256"
[[ "$(awk '{print $1}' "$capture/binary.before.sha256")" == "$(awk '{print $1}' "$(dirname "$binary")/binary.sha256")" ]] || exit 125
printf '%s\n' "$(git -C "$source_root" rev-parse HEAD)" > "$capture/source.commit.before"
git -C "$source_root" status --porcelain=v1 --untracked-files=all > "$capture/source.status.before"
git -C "$source_root" diff --check > "$capture/source.diff-check.before"
snapshot_dependencies "$capture/dependencies.before.tsv" || exit 125
cd "$source_root/sim-testnet" || exit 125
"$binary" -test.list . > "$capture/compiled.tests.raw" 2> "$capture/census.stderr"
sort -u "$capture/compiled.tests.raw" > "$capture/compiled.tests.txt"
: > "$capture/census.missing.txt"
while IFS= read -r test_name; do grep -F -x -- "$test_name" "$capture/compiled.tests.txt" >/dev/null || printf '%s\n' "$test_name" >> "$capture/census.missing.txt"; done < "$capture/expected-tests.txt"
if [[ ! -s "$capture/census.missing.txt" ]]; then printf 'true\n' > "$capture/census.valid"; else printf 'false\n' > "$capture/census.valid"; exit 125; fi
set +e
"$binary" -test.v -test.count=1 -test.parallel=4 -test.run "$selector" 2>&1 | /usr/local/go/bin/go tool test2json -t > "$capture/test2json.jsonl"
pipeline=("${PIPESTATUS[@]}")
test_rc="${pipeline[0]}"
json_rc="${pipeline[1]}"
set -e
printf '%s\n' "$test_rc" > "$capture/body.exit.raw"
printf '%s\n' "$json_rc" > "$capture/test2json.exit"
jq -r 'select((.Action == "pass" or .Action == "fail" or .Action == "skip") and (.Test != null) and ((.Test | contains("/")) | not)) | [.Test, (.Action | ascii_upcase)] | @tsv' "$capture/test2json.jsonl" | sort -u > "$capture/observed.top-level.tsv"
jq -r 'select((.Action == "pass" or .Action == "fail") and (.Test == null)) | [.Package, (.Action | ascii_upcase)] | @tsv' "$capture/test2json.jsonl" > "$capture/package.terminal.tsv"
set +e
awk -F '\t' '
  FNR == NR { if (NF != 2 || expected[$1]++) bad = 1; want[$1] = $2; next }
  { if (NF != 2 || observed[$1]++) bad = 1; got[$1] = $2 }
  END {
    for (name in want) { if (!(name in got) || got[name] != want[name]) { print "expected " name "=" want[name] ", observed=" (name in got ? got[name] : "missing") > "/dev/stderr"; bad = 1 } }
    for (name in got) { if (!(name in want)) { print "unexpected " name "=" got[name] > "/dev/stderr"; bad = 1 } }
    exit bad ? 1 : 0
  }' "$capture/expected-outcomes.tsv" "$capture/observed.top-level.tsv" > "$capture/event-validation.stdout" 2> "$capture/event-validation.stderr"
event_rc=$?
set -e
if [[ "$event_rc" == 0 && "$json_rc" == 0 && "$test_rc" == "$expected_exit" ]]; then printf 'true\n' > "$capture/events.valid"; else printf 'false\n' > "$capture/events.valid"; fi
printf '%s\n' "$(git -C "$source_root" rev-parse HEAD)" > "$capture/source.commit.after"
git -C "$source_root" status --porcelain=v1 --untracked-files=all > "$capture/source.status.after"
git -C "$source_root" diff --check > "$capture/source.diff-check.after"
snapshot_dependencies "$capture/dependencies.after.tsv"
sha256sum "$binary" > "$capture/binary.after.sha256"
cmp -s "$capture/binary.before.sha256" "$capture/binary.after.sha256"
binary_rc=$?
cmp -s "$capture/dependencies.before.tsv" "$capture/dependencies.after.tsv"
deps_rc=$?
if [[ -z "$(cat "$capture/source.status.after")" && -z "$(cat "$capture/source.diff-check.after")" && $binary_rc -eq 0 && $deps_rc -eq 0 ]]; then printf 'source_clean=true\ndependencies_unchanged=true\nbinary_unchanged=true\n' > "$capture/fences.after"; else printf 'fence_failure=true\n' > "$capture/fences.after"; event_rc=125; fi
if [[ "$event_rc" != 0 || "$json_rc" != 0 || "$test_rc" != "$expected_exit" ]]; then exit 125; fi
exit "$test_rc"
