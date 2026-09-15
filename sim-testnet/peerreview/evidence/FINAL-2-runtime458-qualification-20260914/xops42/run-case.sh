#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture="${CAPTURE_ROOT:?}"
source_root="${SOURCE_ROOT:?}"
case_name="${1:?case required}"
command_file="${2:?command file required}"
parent="$(dirname "$source_root")"
case_dir="$capture/$case_name"
mkdir -p "$case_dir"
chmod 700 "$case_dir"
printf "%s\n" "$$" > "$case_dir/owner.pid"
fence() {
  printf 'source_head='; git -C "$source_root" rev-parse --verify HEAD^{commit}
  printf 'source_status_bytes='; git -C "$source_root" status --porcelain=v1 --untracked-files=all | wc -c
  printf 'source_diff_check_bytes='; git -C "$source_root" diff --check | wc -c
  sha256sum "$source_root/main/ansible/tests/test_vulnscan2_resolved.py"
  for repo in warp vault config; do
    printf '%s_alias=' "$repo"; readlink -f -- "$parent/$repo"
    printf '%s_head=' "$repo"; git -C "$parent/$repo" rev-parse --verify HEAD^{commit}
    printf '%s_status_bytes=' "$repo"; git -C "$parent/$repo" status --porcelain=v1 --untracked-files=all | wc -c
  done
  sha256sum /home/by/urnetwork/.virtualenv/brien/bin/python3
}
{
  date -u +started_utc=%Y-%m-%dT%H:%M:%SZ
  printf 'cwd=%s\n' "$source_root"
  fence
  /home/by/urnetwork/.virtualenv/brien/bin/python3 --version
  printf 'command_file_sha256='; sha256sum "$command_file" | awk '{print $1}'
  printf 'command='; tr '\n' ' ' < "$command_file"; printf '\n'
} > "$case_dir/prebody.fence"
set +e
bash "$command_file" > "$case_dir/body.stdout" 2> "$case_dir/body.stderr"
status=$?
set -e
printf '%s\n' "$status" > "$case_dir/outer.exit"
{
  date -u +finished_utc=%Y-%m-%dT%H:%M:%SZ
  printf 'body_exit=%s\n' "$status"
  fence
} > "$case_dir/postbody.fence"
exit "$status"
