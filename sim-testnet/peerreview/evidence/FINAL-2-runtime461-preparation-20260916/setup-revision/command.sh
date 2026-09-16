#!/usr/bin/env bash
set -u -o pipefail
# Prepared setup-revision capture from the retained strict-resume owner.
# Requires a concrete REQUEST.json and late operands; preparation does not execute it.
umask 077
native_capture='/mnt/data/sn-testnet/qualification/native-recovery-20260916-r4/setup-revision'
native_state='/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4'
native_source="${NATIVE_SN_SOURCE:?bind the reviewed source checkout}"
native_cli="${NATIVE_CLI:?bind the matching reviewed executable}"
native_vault='/home/by/urnetwork/temp/sn-launch-preparation-failure-batch-20260915/budget-candidate-205-225-r3/private-vault'
native_config='/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/workspace/config'
test ! -e "$native_capture/outer.started-at" || exit 125
mkdir -p "$native_capture/tmp" "$native_capture/gotmp" || exit 125
chmod 0700 "$native_capture/tmp" "$native_capture/gotmp" || exit 125
printf '%s\n' "$$" > "$native_capture/outer.pid"
date -u +%FT%TZ > "$native_capture/outer.started-at"
cd "$native_source" || exit 125
test "$(git rev-parse HEAD)" = "${EXPECTED_SN:?EXPECTED_SN is required}" || exit 125
test "$(git rev-parse origin/main)" = "${EXPECTED_SN:?EXPECTED_SN is required}" || exit 125
test -z "$(git status --porcelain=v1 --untracked-files=all)" || exit 125
test "$(git -C "$native_vault" rev-parse HEAD)" = 'fb1c124b7a681ef0bc08a5b6930e694bffe28bc7' || exit 125
test -z "$(git -C "$native_vault" status --porcelain=v1 --untracked-files=all)" || exit 125
test "$(git -C "$native_config" rev-parse HEAD)" = 'b5a7b4d8438e80ebc4e8529ba00a872495717109' || exit 125
test -z "$(git -C "$native_config" status --porcelain=v1 --untracked-files=all)" || exit 125
test -f "$native_capture/REQUEST.json" || exit 125
sha256sum "$native_state/plan.json" "$native_state/journal.jsonl" "$native_state/supervisor.state.json" "$native_state/supervisor.json" "$native_state/config.redacted.yml" "$native_state/public/identities.json" > "$native_capture/state.before.sha256"
sha256sum "$native_cli" > "$native_capture/binary.before.sha256"
sha256sum deploy/testnet/release.lock.yml > "$native_capture/lock.before.sha256"
sha256sum "$native_capture/REQUEST.json" "$0" > "$native_capture/command.inputs.sha256"
test "$(cut -d ' ' -f 1 "$native_capture/binary.before.sha256")" = "${NATIVE_CLI_SHA256:?bind executable SHA256}" || exit 125
test "$(cut -d ' ' -f 1 "$native_capture/lock.before.sha256")" = "${NATIVE_RELEASE_LOCK_SHA256:?bind release-lock SHA256}" || exit 125
date -u +%FT%TZ > "$native_capture/body.started-at"
env GOMAXPROCS=4 'GOFLAGS=-mod=readonly -p=1' GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local 'GOVCS=*:off' GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/home/by/urnetwork/temp/sn-carried-fleet-qualification-20260913/runtime/gocache GOTMPDIR=/mnt/data/sn-testnet/qualification/native-recovery-20260916-r4/setup-revision/gotmp TMPDIR=/mnt/data/sn-testnet/qualification/native-recovery-20260916-r4/setup-revision/tmp "$native_cli" setup --prepare-only --apply --plan-hash "${PLAN_HASH:?bind exact emitted revision hash}" --config "$native_source/sim-testnet/testnet.yml" --state-dir "$native_state" --sn-repo "$native_source" --server-repo /home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/server --operator-proxy-repo /home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/operator-proxy --vault-repo "$native_vault" --platform-config-repo "$native_config" --owned-rpc-authority 192.168.1.162:9944 --format json > "$native_capture/stdout.json" 2> "$native_capture/stderr"
native_exit=$?
printf '%s\n' "$native_exit" > "$native_capture/body.exit"
date -u +%FT%TZ > "$native_capture/body.finished-at"
sha256sum "$native_state/plan.json" "$native_state/journal.jsonl" "$native_state/supervisor.state.json" "$native_state/supervisor.json" "$native_state/config.redacted.yml" "$native_state/public/identities.json" > "$native_capture/state.after.sha256"
sha256sum "$native_cli" > "$native_capture/binary.after.sha256"
sha256sum deploy/testnet/release.lock.yml > "$native_capture/lock.after.sha256"
cmp -s "$native_capture/state.before.sha256" "$native_capture/state.after.sha256"
native_state_equal=$?
cmp -s "$native_capture/binary.before.sha256" "$native_capture/binary.after.sha256"
native_binary_equal=$?
cmp -s "$native_capture/lock.before.sha256" "$native_capture/lock.after.sha256"
native_lock_equal=$?
printf 'body_exit=%s\nstate_compare_exit=%s\nbinary_compare_exit=%s\nlock_compare_exit=%s\n' "$native_exit" "$native_state_equal" "$native_binary_equal" "$native_lock_equal" > "$native_capture/result.status"
date -u +%FT%TZ > "$native_capture/outer.finished-at"
if (( native_binary_equal != 0 || native_lock_equal != 0 )); then native_exit=1; fi
printf '%s\n' "$native_exit" > "$native_capture/outer.exit"
exit "$native_exit"
