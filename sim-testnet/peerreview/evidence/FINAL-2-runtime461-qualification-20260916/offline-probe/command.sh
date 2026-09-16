#!/usr/bin/env bash
set -u -o pipefail
umask 077
capture=/mnt/data/sn-testnet/qualification/runtime461-terra-offline-probe-20260916-r1
binary=/home/by/urnetwork/temp/runtime-metadata-probe-target/debug/runtime-metadata-probe
wasm=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/lan-artifacts/runtime.wasm
metadata=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/lan-artifacts/metadata.scale
manifest=/mnt/data/sn-testnet/qualification/runtime461-20260916-r1/lan-artifacts/artifact.json
status=125
date -u +'%Y-%m-%dT%H:%M:%SZ' >"$capture/outer.started-at"
printf '%s\n' "$$" >"$capture/outer.pid"
ps -o pid=,ppid=,sid=,pgid=,stat=,comm= -p "$$" >"$capture/outer.process-at-start"
printf '%s\n' "$(ps -o sid= -p "$$" | tr -d ' ')" >"$capture/outer.sid"
printf '%s\n' 'runtime-metadata-probe runtime.wasm node-subtensor 461 1 1 14 2534293 344267 0xa236f7d2ac285615ee1789953e5e009464cc96f357d48278a848f82cdc771cc4 0x15cf19d2f4f8e2a8a6f46cb735db8f9f03ba3775866188fa93799ad3a040da2e 0xddeffac09b36b85f584ad08b11441f7eae184728a67fa286c0c9b919d27f0405 0x98b2cfd0d6633488dfe5b3b70b869d5753aa3c42396533013df131e4e0e5ca68' >"$capture/command.txt"
sha256sum "$capture/command.sh" >"$capture/command.sha256"
stat -c 'mode=%a size=%s mtime=%y path=%n' "$binary" "$wasm" "$metadata" "$manifest" >"$capture/inputs.before.stat"
sha256sum "$binary" "$wasm" "$metadata" "$manifest" >"$capture/inputs.before.sha256"
b2sum -l 256 "$wasm" "$metadata" >"$capture/artifacts.before.blake2b-256"
if [ "$(awk 'NR == 1 {print $1}' "$capture/inputs.before.sha256")" = cf9c6a08ba3e1a3b75b842f83553eb40cf84afa83bdb50747e477705d63845be ] && [ "$(awk 'NR == 2 {print $1}' "$capture/inputs.before.sha256")" = a236f7d2ac285615ee1789953e5e009464cc96f357d48278a848f82cdc771cc4 ] && [ "$(awk 'NR == 3 {print $1}' "$capture/inputs.before.sha256")" = ddeffac09b36b85f584ad08b11441f7eae184728a67fa286c0c9b919d27f0405 ] && [ "$(awk 'NR == 4 {print $1}' "$capture/inputs.before.sha256")" = 306f27a51b13b67ef3ddaa1990fc1ea1d85671530e3a649c0ec4086abbaa0b9b ] && [ "$(awk 'NR == 1 {print $1}' "$capture/artifacts.before.blake2b-256")" = 15cf19d2f4f8e2a8a6f46cb735db8f9f03ba3775866188fa93799ad3a040da2e ] && [ "$(awk 'NR == 2 {print $1}' "$capture/artifacts.before.blake2b-256")" = 98b2cfd0d6633488dfe5b3b70b869d5753aa3c42396533013df131e4e0e5ca68 ] && [ "$(stat -c %s "$wasm")" = 2534293 ] && [ "$(stat -c %s "$metadata")" = 344267 ]; then
  printf '%s\n' true >"$capture/preflight.ok"
  printf '%s\n' true >"$capture/probe.started"
  timeout --foreground --kill-after=15s 120s "$binary" "$wasm" node-subtensor 461 1 1 14 2534293 344267 0xa236f7d2ac285615ee1789953e5e009464cc96f357d48278a848f82cdc771cc4 0x15cf19d2f4f8e2a8a6f46cb735db8f9f03ba3775866188fa93799ad3a040da2e 0xddeffac09b36b85f584ad08b11441f7eae184728a67fa286c0c9b919d27f0405 0x98b2cfd0d6633488dfe5b3b70b869d5753aa3c42396533013df131e4e0e5ca68 >"$capture/probe.stdout" 2>"$capture/probe.stderr"
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
