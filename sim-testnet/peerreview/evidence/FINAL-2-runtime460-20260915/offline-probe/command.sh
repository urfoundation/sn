#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/mnt/data/sn-testnet/qualification/runtime460-terra-offline-probe-20260915-r1
binary=/home/by/urnetwork/temp/runtime-metadata-probe-target/debug/runtime-metadata-probe
wasm=/mnt/data/sn-testnet/qualification/runtime460-20260915-r1/lan-artifacts/runtime460.code.wasm
metadata=/mnt/data/sn-testnet/qualification/runtime460-20260915-r1/lan-artifacts/runtime460.metadata.scale
manifest=/mnt/data/sn-testnet/qualification/runtime460-20260915-r1/lan-artifacts/artifact.json
status=125
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.started-at"
printf '%s\n' "$$" >"$capture/outer.pid"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"$capture/outer.process-at-start"
printf '%s\n' "$(ps -o sid= -p "$$" | tr -d ' ')" >"$capture/outer.sid"
printf '%s\n' 'runtime-metadata-probe runtime460.code.wasm node-subtensor 460 1 1 14 2525524 336358 0x12b9affec176cbb79c7e5db253d3d0e47f4cb575ce4501ef10de6578afbb817f 0xa2ba599cc0ee97abaa078cf54498ad020957a32cdc2cb7c1e5b9fa14bf5cad3d 0x0e18eed4701255a567411bdc646c76eba355cf41bcb5fcbed8f673a458118e1a 0x98574118d8447c31b72c57402bdda481203f58273ae175a3b6c1da44400e934c' >"$capture/command.txt"
sha256sum "$capture/command.sh" >"$capture/command.sha256"
stat -c 'mode=%a size=%s mtime=%y path=%n' "$binary" "$wasm" "$metadata" "$manifest" >"$capture/inputs.before.stat"
sha256sum "$binary" "$wasm" "$metadata" "$manifest" >"$capture/inputs.before.sha256"
b2sum -l 256 "$wasm" "$metadata" >"$capture/artifacts.before.blake2b-256"
if [ "$(awk 'NR == 1 {print $1}' "$capture/inputs.before.sha256")" = cf9c6a08ba3e1a3b75b842f83553eb40cf84afa83bdb50747e477705d63845be ] && [ "$(awk 'NR == 2 {print $1}' "$capture/inputs.before.sha256")" = 12b9affec176cbb79c7e5db253d3d0e47f4cb575ce4501ef10de6578afbb817f ] && [ "$(awk 'NR == 3 {print $1}' "$capture/inputs.before.sha256")" = 0e18eed4701255a567411bdc646c76eba355cf41bcb5fcbed8f673a458118e1a ] && [ "$(awk 'NR == 4 {print $1}' "$capture/inputs.before.sha256")" = 6ebcdbb8cb8bbaa8ec5e8421d336c5e2ef5d95aceba1b5b9a133d013ee601aa6 ] && [ "$(awk 'NR == 1 {print $1}' "$capture/artifacts.before.blake2b-256")" = a2ba599cc0ee97abaa078cf54498ad020957a32cdc2cb7c1e5b9fa14bf5cad3d ] && [ "$(awk 'NR == 2 {print $1}' "$capture/artifacts.before.blake2b-256")" = 98574118d8447c31b72c57402bdda481203f58273ae175a3b6c1da44400e934c ] && [ "$(stat -c %s "$wasm")" = 2525524 ] && [ "$(stat -c %s "$metadata")" = 336358 ]; then
  printf '%s\n' true >"$capture/preflight.ok"
  printf '%s\n' true >"$capture/probe.started"
  timeout --foreground --kill-after=15s 120s "$binary" "$wasm" node-subtensor 460 1 1 14 2525524 336358 0x12b9affec176cbb79c7e5db253d3d0e47f4cb575ce4501ef10de6578afbb817f 0xa2ba599cc0ee97abaa078cf54498ad020957a32cdc2cb7c1e5b9fa14bf5cad3d 0x0e18eed4701255a567411bdc646c76eba355cf41bcb5fcbed8f673a458118e1a 0x98574118d8447c31b72c57402bdda481203f58273ae175a3b6c1da44400e934c >"$capture/probe.stdout" 2>"$capture/probe.stderr"
  status=$?
else
  printf '%s\n' false >"$capture/preflight.ok"
  printf '%s\n' false >"$capture/probe.started"
  printf '%s\n' 'admitted probe binary, exact Wasm, metadata, or raw manifest input did not match its required hash/size' >"$capture/probe.stderr"
fi
printf '%s\n' "$status" >"$capture/probe.exit"
stat -c 'mode=%a size=%s mtime=%y path=%n' "$binary" "$wasm" "$metadata" "$manifest" >"$capture/inputs.after.stat"
sha256sum "$binary" "$wasm" "$metadata" "$manifest" >"$capture/inputs.after.sha256"
b2sum -l 256 "$wasm" "$metadata" >"$capture/artifacts.after.blake2b-256"
if cmp -s "$capture/inputs.before.sha256" "$capture/inputs.after.sha256" && cmp -s "$capture/artifacts.before.blake2b-256" "$capture/artifacts.after.blake2b-256"; then
  printf '%s\n' true >"$capture/inputs.unchanged"
else
  printf '%s\n' false >"$capture/inputs.unchanged"
  if [ "$status" -eq 0 ]; then status=126; fi
fi
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.finished-at"
printf '%s\n' "$status" >"$capture/outer.exit"
exit "$status"
