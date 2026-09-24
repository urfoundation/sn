# Retained native source role for a fresh evidence generation

A fresh policy generation keeps the validator hotkey but starts independent
measurement and coordinator state. If the native source slot still contains
the predecessor's finalized write, the validator may authenticate that exact
write through a role-only descriptor. This grants no ledger continuity,
measurement acceptance, EMA, policy, or steering-intent state to the new source.

`policy-rollover --rollover-source-role` prepares an immutable subplan under
`STATE/policy-rollover/source-role/generation-NNNNNNNNNNNNNNNNNNNN/`. The plan
binds the existing deployment plan, original generation handoff hash, both
original config hashes, old intent-store hashes, and new descriptor/config
references. The original handoff and configs are never rewritten. Each derived
config may change only `source_role_predecessor_v2`.

Prepare and review using the same config, repository, RPC authority and exact
deployment plan options as the active campaign:

```sh
sim-testnet policy-rollover \
  --config "$CONFIG" --state-dir "$STATE" --plan-hash "$DEPLOYMENT_PLAN_HASH" \
  --provisional-resume --owned-rpc-authority "$OWNED_RPC_AUTHORITY" \
  --rollover-source-role --format json
```

The plan is inactive. Preparation reads retained source files and writes only
new immutable review files; it does not acquire the deployment writer, publish
transactions, restart a process, or change the selected validator config.

For a campaign successor, first retain the actual signed terminal result and
restore every scheduled fault. After the ordinary launcher/supervisor is
stopped, apply the reviewed plan with its exact `plan_hash`:

```sh
sim-testnet policy-rollover \
  --config "$CONFIG" --state-dir "$STATE" --plan-hash "$DEPLOYMENT_PLAN_HASH" \
  --provisional-resume --owned-rpc-authority "$OWNED_RPC_AUTHORITY" \
  --rollover-source-role --rollover-plan "$SOURCE_ROLE_PLAN" \
  --rollover-plan-hash "$SOURCE_ROLE_PLAN_HASH" --apply --format json
```

Apply owns the normal deployment journal lock, requires the supervisor and
recorded validators to be stopped, and independently verifies the original
signed atomic source/weights extrinsic and finalized receipt. The current
finalized native slot must still have the exact predecessor source hash and
write block. A changed source, another hotkey, malformed artifact, or changed
reference rejects before publishing the selection receipt.

The new owner-signed `handoff.evidence.json` is the immutable selection receipt.
The original rollover activation journal remains intact; this role-only action
adds no activation transactions or budget. Normal launch/continuation selectors
authenticate both receipts and select the derived configs. Runtime startup
rechecks the historical native proof; later writes are authenticated through
the fresh generation's own intent store. An exact already-selected apply retry
retains the original signed receipt and does not require the old slot to remain
current after those later writes.

This overlay does not authorize a production campaign or alter any prior
assertion/result. The provisional production handoff separately requires the
exact signed terminal result, restored faults, ownership/integrity checks, and
`final_acceptance=false`.
