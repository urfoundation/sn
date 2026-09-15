#!/usr/bin/env bash
set -euo pipefail
umask 077
runtime='/home/by/urnetwork/temp/sn-warp-admission-integration-20260913/workspace-physical/terra-runtime/producer38-eccfae8-canonical-r1'
: "${FINAL_WORKSPACE:?FINAL_WORKSPACE is required}" "${SOURCE_PAIR:?SOURCE_PAIR is required}" "${EXPECTED_SN:?EXPECTED_SN is required}"
lock="$FINAL_WORKSPACE/sn/deploy/testnet/release.lock.yml"
[[ "$EXPECTED_SN" =~ ^[0-9a-f]{40}$ && -d "$FINAL_WORKSPACE/sn/.git" && ! -L "$FINAL_WORKSPACE/sn" && -f "$SOURCE_PAIR" && ! -L "$SOURCE_PAIR" && "$(stat -c '%a' "$SOURCE_PAIR")" == 600 ]] || exit 125
[[ -f "$lock" && ! -L "$lock" && "$(sha256sum "$lock" | awk '{print $1}')" == '5b8c412454adfde72f2cf51c682b5ce4b9b335a2095eaafc21bf4e92a153a4ab' ]] || exit 125
[[ "$(wc -l < "$SOURCE_PAIR")" == 13 && "$(git -C "$FINAL_WORKSPACE/sn" rev-parse HEAD)" == "$EXPECTED_SN" ]] || exit 125
cd "$FINAL_WORKSPACE/sn"
exec env TMPDIR="$runtime/tmp" RELEASE_GATE_CONCURRENT_GATES=1 RELEASE_GATE_JOBS=4 RUN_SERVER_DB_TESTS=1 RELEASE_GATE_DIAGNOSTIC=0 bash "$FINAL_WORKSPACE/sn/scripts/test-release-1.0-producer-gate.sh"
