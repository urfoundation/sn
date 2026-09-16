#!/usr/bin/env bash
set -u -o pipefail
# Prepared read-only release-lock capture; NOT EXECUTED.
# Bind EXPECTED_SN to the final clean formatted candidate after root moves the physical checkout.
umask 077
native_capture='/mnt/data/sn-testnet/qualification/retained-readiness-handoff-release-lock-preview-20260916-r1'
native_state='/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4'
native_source='/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/sn'
native_cli='/mnt/data/sn-testnet/qualification/pf01-pf02-physical-cli-final-lock-20260916-r1/sim-testnet'
native_vault='/home/by/urnetwork/temp/sn-launch-preparation-failure-batch-20260915/budget-candidate-205-225-r3/private-vault'
native_config='/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/workspace/config'
test ! -e "$native_capture/outer.started-at" || exit 125
mkdir -p "$native_capture/tmp" "$native_capture/gotmp" || exit 125
chmod 0700 "$native_capture/tmp" "$native_capture/gotmp" || exit 125
printf '%s\n' "$$" > "$native_capture/outer.pid"
date -u +%FT%TZ > "$native_capture/outer.started-at"
cd "$native_source" || exit 125
test "$(git rev-parse HEAD)" = "${EXPECTED_SN:?EXPECTED_SN is required}" || exit 125
test -z "$(git status --porcelain=v1 --untracked-files=all)" || exit 125
test "$(git -C "$native_vault" rev-parse HEAD)" = 'fb1c124b7a681ef0bc08a5b6930e694bffe28bc7' || exit 125
test -z "$(git -C "$native_vault" status --porcelain=v1 --untracked-files=all)" || exit 125
test "$(git -C "$native_config" rev-parse HEAD)" = 'b5a7b4d8438e80ebc4e8529ba00a872495717109' || exit 125
test -z "$(git -C "$native_config" status --porcelain=v1 --untracked-files=all)" || exit 125
test "$(git -C /home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/server rev-parse HEAD)" = '6752a8df246c0ee7e1c5a38cbd26b1e849b702ca' || exit 125
# These unchanged compiled observer/schema/artifact inputs make the qualified prior CLI valid for rendering current source observations.
git diff --exit-code aeda6abbd2dc0abc92bb0f60975cf89b509e8017 "$EXPECTED_SN" -- sim-testnet/release_lock.go sim-testnet/release_lock_render.go sim-testnet/config.go sim-testnet/runtime_identity.go sim-testnet/contracts_gen.go stabi go.mod go.sum > "$native_capture/renderer-inputs.diff" || exit 125
git rev-parse HEAD > "$native_capture/source.before.commit" || exit 125
python3 - "$native_capture" "$EXPECTED_SN" <<'REQUEST_PY'
from pathlib import Path
import json,sys
out=Path(sys.argv[1])
request=json.loads((out/'REQUEST.template.json').read_text())
request['source_commit']=sys.argv[2]
request['status']='bound for this read-only invocation'
(out/'REQUEST.json').write_text(json.dumps(request,indent=2)+'\n')
REQUEST_PY
native_request_exit=$?
if (( native_request_exit != 0 )); then exit 125; fi
cp -- deploy/testnet/release.lock.yml "$native_capture/original.lock.yml" || exit 125
sha256sum "$native_state/plan.json" "$native_state/journal.jsonl" "$native_state/supervisor.state.json" "$native_state/supervisor.json" "$native_state/config.redacted.yml" "$native_state/public/identities.json" > "$native_capture/state.before.sha256"
sha256sum "$native_cli" > "$native_capture/binary.before.sha256"
sha256sum deploy/testnet/release.lock.yml > "$native_capture/lock.before.sha256"
sha256sum "$native_capture/REQUEST.json" "$0" > "$native_capture/command.inputs.sha256"
test "$(cut -d ' ' -f 1 "$native_capture/binary.before.sha256")" = '8fc61a65cd0524413a7ba70c61bcdb15962fa87ad7ab347b653abb27f8913f0b' || exit 125
test "$(cut -d ' ' -f 1 "$native_capture/lock.before.sha256")" = 'bf417189d4c0a62f8116606f84f9c5509b3afe2dab611429c9b781998b3198fc' || exit 125
date -u +%FT%TZ > "$native_capture/body.started-at"
env GOMAXPROCS=4 'GOFLAGS=-mod=readonly -p=1' GOENV=off GOWORK=off GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local 'GOVCS=*:off' GOMODCACHE=/home/by/go/pkg/mod GOCACHE=/home/by/urnetwork/temp/sn-carried-fleet-qualification-20260913/runtime/gocache GOTMPDIR=/mnt/data/sn-testnet/qualification/retained-readiness-handoff-release-lock-preview-20260916-r1/gotmp TMPDIR=/mnt/data/sn-testnet/qualification/retained-readiness-handoff-release-lock-preview-20260916-r1/tmp "$native_cli" release-lock --config "$native_source/sim-testnet/testnet.yml" --sn-repo "$native_source" --server-repo /home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/server --operator-proxy-repo /home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/operator-proxy --vault-repo "$native_vault" --platform-config-repo "$native_config" > "$native_capture/stdout.yml" 2> "$native_capture/stderr"
native_exit=$?
printf '%s\n' "$native_exit" > "$native_capture/body.exit"
date -u +%FT%TZ > "$native_capture/body.finished-at"
sha256sum "$native_state/plan.json" "$native_state/journal.jsonl" "$native_state/supervisor.state.json" "$native_state/supervisor.json" "$native_state/config.redacted.yml" "$native_state/public/identities.json" > "$native_capture/state.after.sha256"
sha256sum "$native_cli" > "$native_capture/binary.after.sha256"
sha256sum deploy/testnet/release.lock.yml > "$native_capture/lock.after.sha256"
git rev-parse HEAD > "$native_capture/source.after.commit" || exit 125
cmp -s "$native_capture/source.before.commit" "$native_capture/source.after.commit"
native_source_equal=$?
cmp -s "$native_capture/state.before.sha256" "$native_capture/state.after.sha256"
native_state_equal=$?
cmp -s "$native_capture/binary.before.sha256" "$native_capture/binary.after.sha256"
native_binary_equal=$?
cmp -s "$native_capture/lock.before.sha256" "$native_capture/lock.after.sha256"
native_lock_equal=$?
printf 'body_exit=%s\nstate_compare_exit=%s\nbinary_compare_exit=%s\nlock_compare_exit=%s\nsource_compare_exit=%s\n' "$native_exit" "$native_state_equal" "$native_binary_equal" "$native_lock_equal" "$native_source_equal" > "$native_capture/result.status"
date -u +%FT%TZ > "$native_capture/outer.finished-at"
if (( native_state_equal != 0 || native_binary_equal != 0 || native_lock_equal != 0 || native_source_equal != 0 )); then native_exit=1; fi
printf '%s\n' "$native_exit" > "$native_capture/outer.exit"
exit "$native_exit"
