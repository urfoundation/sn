#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/home/by/urnetwork/temp/xops-rpc-vulnerability-assertion-correction-20260914/terra-runtime/vulnerability-assertion-qualification-20260914T232905Z-r4
causal="$capture/causal/old-quota-assertions"
worktree="$causal/worktree"
command_file="$causal/causal.command"
body="$causal/body"
mkdir -p "$body"
chmod 700 "$body"
printf "%s\n" "$$" > "$body/owner.pid"
{
 date -u +started_utc=%Y-%m-%dT%H:%M:%SZ
 printf 'cwd=%s\n' "$worktree"
 printf 'candidate_head='; cat "$causal/candidate.head"
 printf 'mutant_head='; git -C "$worktree" rev-parse HEAD
 printf 'mutant_status_bytes='; git -C "$worktree" status --porcelain=v1 --untracked-files=all | wc -c
 printf 'mutant_diff_check_bytes='; git -C "$worktree" diff --check | wc -c
 sha256sum "$worktree/main/ansible/tests/test_vulnscan2_resolved.py"
 sha256sum "$command_file" "$capture/meta/causal.patch"
 /home/by/urnetwork/.virtualenv/brien/bin/python3 --version
} > "$body/prebody.fence"
set +e
bash "$command_file" > "$body/body.stdout" 2> "$body/body.stderr"
status=$?
set -e
printf '%s\n' "$status" > "$body/outer.exit"
{
 date -u +finished_utc=%Y-%m-%dT%H:%M:%SZ
 printf 'body_exit=%s\n' "$status"
 printf 'mutant_head='; git -C "$worktree" rev-parse HEAD
 printf 'mutant_status_bytes='; git -C "$worktree" status --porcelain=v1 --untracked-files=all | wc -c
 printf 'mutant_diff_check_bytes='; git -C "$worktree" diff --check | wc -c
 sha256sum "$worktree/main/ansible/tests/test_vulnscan2_resolved.py"
} > "$body/postbody.fence"
exit "$status"
