#!/usr/bin/env bash
set -euo pipefail
umask 077
restore_root=/mnt/data/sn-testnet/qualification/operator-signed-attempt-census-20260916-r1
restore_capture="$restore_root/restore"
restore_state=/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4
restore_source="$restore_root/private/missing-rlp"
restore_store="$restore_state/transactions"
test ! -e "$restore_capture/body.started-at"
date -u +%FT%TZ > "$restore_capture/body.started-at"
printf '%s\n' "$$" > "$restore_capture/owner.pid"
trap 'restore_exit=$?; printf "%s\n" "$restore_exit" > "$restore_capture/body.exit"; date -u +%FT%TZ > "$restore_capture/body.finished-at"' EXIT
sha256sum "$restore_capture/REQUEST.json" "$0" > "$restore_capture/inputs.sha256"
test -d "$restore_store"
test ! -L "$restore_store"
sha256sum --check --status /mnt/data/sn-testnet/qualification/native-recovery-20260916-r3/relay-capture-r2/state.after.sha256
sha256sum "$restore_state/plan.json" "$restore_state/journal.jsonl" "$restore_state/supervisor.state.json" "$restore_state/supervisor.json" "$restore_state/config.redacted.yml" "$restore_state/public/identities.json" > "$restore_capture/state.before.sha256"
(
  cd "$restore_store"
  rg --files --hidden -g '*.rlp' -0 | sort -z | xargs -0 -r sha256sum
) > "$restore_capture/store.before.sha256"
cat > "$restore_capture/candidates.sha256" <<'HASHES'
c98109203e61c6e8597fe36195fc78f036bb31342c78d291574cfe104b851865  50cc7f2745b22052f91a3ce124f18fcbbe0fb0d297f59a9f3f9d2bd33e702dd8.rlp
8bc15c6baeee94bbdeb8a424aa8465c6e67cd6e82b36f799da827d77cd2ef0f0  62a2464cf084c69c81040d1374abcc20c8a99bb34e4788bd6e8b34cbe804d1df.rlp
834d779597e8283807e3db53cb3ec623fef1e7c1b4c0baf9c3b3e3722dc9c16d  a62a69f38c314fce4efdc6cf9084ae5a2efa7178faca0ba25b0c63bd8e5368d9.rlp
92428962da63b6aacdeef9c03fd4dc53aa90e29687347192d3d5dd42259fe951  e95f3f78dad211d12ced0583a47d9af610b9699d68ab08f73b2fc5c3b7acabd8.rlp
HASHES
(
  cd "$restore_source"
  sha256sum --check --status "$restore_capture/candidates.sha256"
)
while read -r restore_sha restore_name; do
  [[ "$restore_name" =~ ^[0-9a-f]{64}\.rlp$ ]]
  test -f "$restore_source/$restore_name"
  test ! -L "$restore_source/$restore_name"
  test "$(stat -c '%a %h %s' "$restore_source/$restore_name")" = '600 1 178'
  test ! -e "$restore_store/$restore_name"
  test ! -L "$restore_store/$restore_name"
done < "$restore_capture/candidates.sha256"
while read -r restore_sha restore_name; do
  dd if="$restore_source/$restore_name" of="$restore_store/$restore_name" bs=65536 conv=excl,fsync status=none
  cmp -- "$restore_source/$restore_name" "$restore_store/$restore_name"
  test "$(stat -c '%a %h %s' "$restore_store/$restore_name")" = '600 1 178'
done < "$restore_capture/candidates.sha256"
sync -f "$restore_store"
(
  cd "$restore_store"
  sha256sum --check --status "$restore_capture/candidates.sha256"
  sha256sum --check --status "$restore_capture/store.before.sha256"
  rg --files --hidden -g '*.rlp' -0 | sort -z | xargs -0 -r sha256sum
) > "$restore_capture/store.after.sha256"
sha256sum "$restore_state/plan.json" "$restore_state/journal.jsonl" "$restore_state/supervisor.state.json" "$restore_state/supervisor.json" "$restore_state/config.redacted.yml" "$restore_state/public/identities.json" > "$restore_capture/state.after.sha256"
cmp -- "$restore_capture/state.before.sha256" "$restore_capture/state.after.sha256"
diff -u "$restore_capture/store.before.sha256" "$restore_capture/store.after.sha256" > "$restore_capture/store.diff" || restore_diff_exit=$?
test "${restore_diff_exit:-0}" = 1
printf 'original_records_unchanged=true\nrestored_files=4\nrestored_bytes=712\nstate_files_unchanged=true\nchain_transactions=0\njournal_mutation=false\ndatabase_mutation=false\n' > "$restore_capture/result.status"
