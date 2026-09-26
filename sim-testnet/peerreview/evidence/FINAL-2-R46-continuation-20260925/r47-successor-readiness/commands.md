# Proposed commands — NOT executed

These commands preserve the original R46 runtime arguments and plan. The fresh-generation route below is blocked on a qualified successor-handoff fix; its epoch/generation/hash must come from a new reviewed plan, not this static receipt.

## Build an isolated cf0 driver for the minimum provisional path

```bash
set -euo pipefail
umask 0077
r47_source=/mnt/data/sn-testnet/qualification/r47-launch-source-cf0a86be-20260926
r47_output=/mnt/data/sn-testnet/qualification/r47-launch-runtime-20260926
r47_binary="$r47_output/sim-testnet-cf0a86be"
mkdir -p "$r47_output"
git -C /home/by/urnetwork/sn worktree add --detach "$r47_source" cf0a86befbefa76597c0f6dd2dc4641400b29275
cd "$r47_source"
go build -modfile=/mnt/data/sn-testnet/qualification/r45-final-candidate-20260925/r45-final.mod -trimpath -buildvcs=true -o "$r47_binary" ./sim-testnet
go version -m "$r47_binary" > "$r47_output/build-info.txt"
sha256sum "$r47_binary" > "$r47_output/binary.sha256"
```

The pinned R45 graph avoids current sibling/SN25 drift; it does not include the separate Connect prefetch correction. A newly fixed successor build must use its qualified commit and matching binary hash instead. Retain clean source/build info and all exact input hashes before admission.

## Original arguments used by every runtime command

```bash
r47_state=/home/by/urnetwork/sn/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4
r47_plan=0x8bb92697db8f2164e46f6e58848d3407e509382fb61550b919f1d55391ad480e
r47_common=(
  --config /mnt/data/sn-testnet/qualification/release37-release-20260923/rate-config/sim-testnet/testnet.yml
  --state-dir "$r47_state"
  --sn-repo /home/by/urnetwork/sn
  --server-repo /home/by/urnetwork/server
  --operator-proxy-repo /home/by/urnetwork/operator-proxy
  --vault-repo /home/by/urnetwork/vault
  --platform-config-repo /home/by/urnetwork/config
  --owned-rpc-authority 192.168.1.162:9944
  --format json
)
```

## A. Minimum new provisional interval, keep supervisor live

After fresh R46→47 checkpoint/authentication and confirming no competing owner, invoke the following under a durable owner service (the command itself is the exact harness entry point):

```bash
"$r47_binary" scenario --name release-1.0 "${r47_common[@]}"   --provisional-resume --apply --plan-hash "$r47_plan"   > "$r47_output/release-r47.stdout.json"
```

No preceding `setup`, `launch`, `release-lock`, `retire`, manual invalidation or regeneration is required. Allow CLI to assign the new run ID/generation/window. A healthy live adoption does not replace workers. Preserve any returned hard error exactly; do not edit state to bypass it.

## B. Deploy workers while preserving current generation (not native-history repair)

Only after the current owner/diagnostic readers are handled and the exact binary is qualified:

```bash
"$r47_binary" stop "${r47_common[@]}" > "$r47_output/stop.json"
"$r47_binary" resume "${r47_common[@]}"   --provisional-resume --apply --plan-hash "$r47_plan"   > "$r47_output/resume.json"
```

Require authenticated stopped generation before resume; resumed result must report zero setup actions. Then run A. Do not use raw PID kills or overwrite `state/build/sim-testnet` while children are live.

## C. Fresh-source generation route — blocked in cf0, qualify fix first

Preconditions: successor-handoff code and focused normal/race are qualified; explicit approval for a distinct generation (does not claim old ledger/EMA continuity); existing exact two-validator/two-operator source and hotkeys are eligible; enough immutable activation-epoch time; all liabilities/nonces/source bounds fit unchanged lifetime caps. Fresh history must be measured, not synthesized.

Choose `r47_activation_epoch` from a new finalized LAN schedule and `r47_generation` from the unused generation inventory. Current inventory has only generation1/cutoff604. These two values intentionally have no static default.

```bash
: "${r47_activation_epoch:?fresh reviewed activation epoch required}"
: "${r47_generation:?fresh approved generation required}"
"$r47_binary" policy-rollover "${r47_common[@]}" --provisional-resume   --plan-hash "$r47_plan" --rollover-epoch "$r47_activation_epoch"   --rollover-generation "$r47_generation" > "$r47_output/rollover-plan.json"
```

This planning command writes immutable local plan/config snapshots even without `--apply`. Review output's exact `plan_hash`, four consent members, actual EVM/native snapshots, source journal, maximum gas/lifetime caps and paths. Use the exact archived plan path and hash; do not manually change its epoch.

```bash
: "${r47_rollover_plan:?exact archived absolute plan path required}"
: "${r47_rollover_hash:?exact reviewed rollover plan hash required}"
"$r47_binary" policy-rollover "${r47_common[@]}" --provisional-resume   --plan-hash "$r47_plan" --rollover-plan "$r47_rollover_plan"   --rollover-plan-hash "$r47_rollover_hash" --apply   > "$r47_output/rollover-staged.json"
```

This apply can spend on four activation transactions and provision new clients while the old supervisor runs. Exact retries reconcile signed bytes; the activation boundary must become finalized inside its approved epoch. Expect staged-awaiting-stopped-topology until the official stop. After a qualified successor implementation and successful staging:

```bash
"$r47_binary" stop "${r47_common[@]}" > "$r47_output/stop.json"
"$r47_binary" policy-rollover "${r47_common[@]}" --provisional-resume   --plan-hash "$r47_plan" --rollover-plan "$r47_rollover_plan"   --rollover-plan-hash "$r47_rollover_hash" --apply   > "$r47_output/rollover-activated.json"
"$r47_binary" resume "${r47_common[@]}" --provisional-resume   --apply --plan-hash "$r47_plan" > "$r47_output/resume.json"
```

Then verify new V1/V2 source selection, fresh applied native decisions and signed proof trails through both operators, and start A for a fresh owner window. Preserve generation1 files and every failed R46 assertion. If a bounded relay/fleet successor changes the setup plan, use its reviewed exact plan hash consistently; never silently substitute it.
