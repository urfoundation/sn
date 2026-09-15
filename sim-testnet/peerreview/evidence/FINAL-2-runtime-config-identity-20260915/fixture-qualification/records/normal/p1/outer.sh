#!/usr/bin/env bash
set -euo pipefail
stage=/home/by/urnetwork/temp/sn-runtime-config-fixture-correction-20260915/terra-runtime/qualification-runtime-config-fixture-5a411d2/normal/p1
source_root=/home/by/urnetwork/temp/sn-runtime-config-fixture-correction-20260915/sn
source_manifest=/home/by/urnetwork/temp/sn-runtime-config-fixture-correction-20260915/terra-runtime/qualification-runtime-config-fixture-5a411d2/inputs/source.manifest.sha256
owner=/home/by/urnetwork/temp/sn-runtime-config-fixture-correction-20260915/terra-runtime/qualification-runtime-config-fixture-5a411d2/normal/p1/owner.sh
date -u +%Y-%m-%dT%H:%M:%S.%NZ > "$stage/outer.started-at"
printf 'bash %s %s %s %s\n' "$source_root/scripts/run-qualification-capture.sh" "$source_root" "$source_manifest" "$owner" > "$stage/outer.command.txt"
sha256sum "$owner" > "$stage/outer.owner.sha256"
set +e
bash "$source_root/scripts/run-qualification-capture.sh" "$source_root" "$source_manifest" "$owner" > "$stage/outer.stdout" 2> "$stage/outer.stderr"
status=$?
set -e
printf '%s\n' "$status" > "$stage/outer.exit"
date -u +%Y-%m-%dT%H:%M:%S.%NZ > "$stage/outer.finished-at"
exit "$status"
