#!/usr/bin/env bash
set -u -o pipefail
umask 077
native_capture='/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/native-setup-preview-c572d993'
native_state='/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4'
native_source='/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn'
printf '%s\n' "$$" > "$native_capture/outer.pid"
date -u +%FT%TZ > "$native_capture/outer.started-at"
cd "$native_source" || exit 125
test "$(git rev-parse HEAD)" = 'c572d993de3116a68b1b2875d91a67c05c839e22' || exit 125
test "$(git rev-parse origin/main)" = 'c572d993de3116a68b1b2875d91a67c05c839e22' || exit 125
test -z "$(git status --porcelain=v1 --untracked-files=all)" || exit 125
sha256sum '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/plan.json' '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/journal.jsonl' '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/supervisor.state.json' '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/supervisor.json' '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/config.redacted.yml' '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/public/identities.json' > "$native_capture/state.before.sha256"
sha256sum '/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/terra-runtime/final-cli-c572d993-canonical-r3/sim-testnet' > "$native_capture/binary.before.sha256"
sha256sum deploy/testnet/release.lock.yml > "$native_capture/lock.before.sha256"
sha256sum "$native_capture/REQUEST.json" "$0" > "$native_capture/command.inputs.sha256"
date -u +%FT%TZ > "$native_capture/body.started-at"
env 'GOMAXPROCS=1' 'GOFLAGS=-mod=readonly -p=2' 'GOENV=off' 'GOWORK=off' 'GOPROXY=off' 'GOSUMDB=off' 'GOTOOLCHAIN=local' 'GOVCS=*:off' 'GOMODCACHE=/home/by/go/pkg/mod' 'GOCACHE=/home/by/urnetwork/temp/sn-carried-fleet-qualification-20260913/runtime/gocache' 'GOTMPDIR=/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/native-setup-preview-c572d993/gotmp' 'TMPDIR=/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/native-setup-preview-c572d993/tmp' '/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/terra-runtime/final-cli-c572d993-canonical-r3/sim-testnet' 'setup' '--config' '/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn/sim-testnet/testnet.yml' '--state-dir' '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4' '--sn-repo' '/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn' '--server-repo' '/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/server' '--operator-proxy-repo' '/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/operator-proxy' '--vault-repo' '/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/vault' '--platform-config-repo' '/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/workspace/config' '--owned-rpc-authority' '192.168.1.162:9944' '--format' 'json' > "$native_capture/stdout.json" 2> "$native_capture/stderr"
native_exit=$?
printf '%s\n' "$native_exit" > "$native_capture/body.exit"
date -u +%FT%TZ > "$native_capture/body.finished-at"
sha256sum '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/plan.json' '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/journal.jsonl' '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/supervisor.state.json' '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/supervisor.json' '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/config.redacted.yml' '/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4/public/identities.json' > "$native_capture/state.after.sha256"
sha256sum '/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/terra-runtime/final-cli-c572d993-canonical-r3/sim-testnet' > "$native_capture/binary.after.sha256"
sha256sum deploy/testnet/release.lock.yml > "$native_capture/lock.after.sha256"
cmp -s "$native_capture/state.before.sha256" "$native_capture/state.after.sha256"
native_state_equal=$?
cmp -s "$native_capture/binary.before.sha256" "$native_capture/binary.after.sha256"
native_binary_equal=$?
cmp -s "$native_capture/lock.before.sha256" "$native_capture/lock.after.sha256"
native_lock_equal=$?
printf 'body_exit=%s\nstate_compare_exit=%s\nbinary_compare_exit=%s\nlock_compare_exit=%s\n' "$native_exit" "$native_state_equal" "$native_binary_equal" "$native_lock_equal" > "$native_capture/result.status"
date -u +%FT%TZ > "$native_capture/outer.finished-at"
if (( native_state_equal != 0 || native_binary_equal != 0 || native_lock_equal != 0 )); then native_exit=1; fi
printf '%s\n' "$native_exit" > "$native_capture/outer.exit"
exit "$native_exit"
