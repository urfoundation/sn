# Settlement v2 adoption of the validator config

Step 10 of the launch runbook, `VERSION2-LAUNCH.md`: the validator adopts the
deployed v2 contracts under a re-signed treasury approval. It needs no new
tooling. The sequence is the one
`TestTreasuryApprovalPlanResignsSettlementV2Adoption` (VA-6) runs, and
`TestSettlementV2AdoptionDocumentIsTheVA6Sequence` holds this page to it.
Every path is absolute, and every file is written under `umask 077`. On a
command line each `…_SHA256` is `sha256:` followed by the file's SHA-256; a
bare digest is refused. Adoption is one-way: once adopted, the block can be
neither re-pointed nor removed.

## 1. Write the adopted config

Copy the running config to `$ADOPTED_CONFIG` and make exactly these three
changes:

```yaml
settlement_v2:
  settlement: "$SETTLEMENT2_ADDRESS"
  vault: "$VAULT2_ADDRESS"
production_authority_history:
  - path: "$PREDECESSOR_AUTHORITY"
    bytes: $PREDECESSOR_AUTHORITY_BYTES
    sha256: "$PREDECESSOR_AUTHORITY_SHA256"
treasury_approval:
  approval:
    path: "$ADOPTED_ENVELOPE"
  signer: "$TREASURY_APPROVAL_KEY"
```

- `settlement_v2` names step 8's `settlement2_address`, the proxy, and its
  `vault2_address`, both lowercase `0x`.
- The history entry is appended after any the config already has. It is the
  running config's authority bundle, which `validator run` retains in its
  `state_dir` as `owner-recycle-production-authority-sha256:<hex>.json`: give
  its path, its size and `0x<hex>`, the form the config's own `sha256` fields
  take.
- `treasury_approval` keeps its signer and names a new envelope path. Step 4
  supplies the envelope's size and hash.

## 2. Draft the approval

```sh
sn-mainnet treasury approval-plan --input "$TREASURY_INPUT" \
  --config "$ADOPTED_CONFIG" --config-sha256 "$ADOPTED_CONFIG_SHA256" \
  --policy-plan "$TREASURY_POLICY_PLAN" --policy-plan-sha256 "$TREASURY_POLICY_PLAN_SHA256" \
  --approval-key "$TREASURY_APPROVAL_KEY" --out "$ADOPTED_APPROVAL"
```

The input and the policy plan are the files the live approval was drafted
from, unchanged (O14). The command writes the approval file and prints its
`signing_digest`.

## 3. Sign the digest

```sh
printf 'urnetwork-native-treasury-approval-signature-v1\n%s' "$(cat "$ADOPTED_APPROVAL")" | shasum -a 256
```

This prints the hex digits of the `signing_digest`. The approval key signs
those 32 bytes, not their hex, with Ed25519. `$SIGNATURE` is the 128 lowercase
hex digits of the signature.

## 4. Assemble the envelope

```sh
sn-mainnet treasury approval-envelope --approval "$ADOPTED_APPROVAL" \
  --signature "$SIGNATURE" --approval-key "$TREASURY_APPROVAL_KEY" --out "$ADOPTED_ENVELOPE"
```

Replace the adopted config's `treasury_approval` with the one this prints. The
approval equals the live one except `config_hash`.

## 5. Restart the validator

Install the [auditor drop-in](validator-ledger-auditor.example.conf), whose
`ExecStart` is this command line, and restart the unit:

```sh
validator run --config="$ADOPTED_CONFIG" --progress-file="$PROGRESS_FILE" \
  --durable-volumes="$VALIDATOR_DECLARATION" --durable-volumes-sha256="$VALIDATOR_DECLARATION_SHA256" \
  --ledger_auditor_key_file="$AUDITOR_KEY_FILE"
```

## Refusals

The loader refuses a wrong adoption when the validator starts:

| Wrong adoption | Refusal |
| --- | --- |
| Another change rides with the block | `settlement v2 adoption changes signed configuration beyond the added settlement_v2 block` |
| The approval differs beyond `config_hash` | `settlement v2 adoption changes the approval beyond its config hash` |
| More than the one bundle is appended | `original production config is not canonical or changes its signed predecessor prefix` |

The loader never reaches the class's own history check, `settlement v2
adoption must append exactly one authority history entry`. It does not refuse a
config that adds the block and appends no entry. That config loads, and the
validator then refuses every decision made under the running config:
`production intent original authority is absent from the signed config
history`. So check the appended entry by eye.
`TestSettlementV2AdoptionDocumentNamesTheLoaderRefusals` triggers each one.
