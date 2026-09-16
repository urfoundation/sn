#!/usr/bin/env bash
set -u -o pipefail
runtime=$(cd "$(dirname "$0")" && pwd)
native_vault=/home/by/urnetwork/temp/sn-launch-preparation-failure-batch-20260915/budget-candidate-205-225-r3/private-vault
native_config=/home/by/urnetwork/temp/sn-soak-deadline-20260909T221044Z/finalization-20260911T0215/workspace/config
printf '%s\n' "$$" > "$runtime/capture/launcher.pid"
date -u +%Y-%m-%dT%H:%M:%SZ > "$runtime/capture/launcher.started-at"
"$runtime/capture-final-cli-build.sh"
status=$?
: > "$runtime/meta/native-overrides.after.tsv"
for item in "native-vault:$native_vault" "platform-config:$native_config"; do
  label=${item%%:*}
  path=${item#*:}
  head=$(git -C "$path" rev-parse HEAD)
  if test -z "$(git -C "$path" status --porcelain=v1 --untracked-files=all)"; then clean=true; else clean=false; fi
  printf '%s\t%s\t%s\t%s\n' "$label" "$path" "$head" "$clean" >> "$runtime/meta/native-overrides.after.tsv"
done
if cmp -s "$runtime/meta/native-overrides.before.tsv" "$runtime/meta/native-overrides.after.tsv"; then printf '%s\n' true > "$runtime/meta/native-overrides.fence.after"; else printf '%s\n' false > "$runtime/meta/native-overrides.fence.after"; if test "$status" -eq 0; then status=126; fi; fi
printf '%s\n' "$status" > "$runtime/capture/launcher.join.exit"
date -u +%Y-%m-%dT%H:%M:%SZ > "$runtime/capture/launcher.joined-at"
exit "$status"
