# Owner-local recycle signing and bounded submission

The `owner-recycle sign`, `import-reply`, `submit-plan` and `submit` commands
complete the command path around the original [recycle transition](OWNER-RECYCLE-TRANSITION.md).
This increment is source-only until its public deterministic tests and adjacent
trim-device regressions are independently executed. It supplies no production
approval, physical-device qualification, live signature or mainnet effect.

The existing approved action, exported request, metadata, owner account, nonce,
mortal era and host custody stay unchanged. The owner signs on their own computer
with their Ledger and the reviewed Polkadot generic app. Snow receives only a
public reply. No owner device or private key is required on Snow.

## Portable owner signing

After the existing `reserve` and `export` phases, transfer the exact request to
the owner. Independently deliver its content hash, the original action approval
key, owner AccountId32 and approved genesis. On the owner's computer, provision
an owner-local durable volume and fresh `mainnet-owner-signing` snapshot with the
same 16 KiB bound as the existing trim signer. Use a separate permanent state
path for this request.

The exact fresh request fields and public `storage-owner-prepare plan`/`apply`
commands are in [OWNER-CUSTODY-PREPARATION.md](OWNER-CUSTODY-PREPARATION.md).
Inspection and Ledger transcript planning can run before that preparation.
Host action custody uses the separate `storage-prepare` daemon declaration;
neither a private directory alone nor the other computer's declaration suffices.

`owner-recycle sign` accepts:

- `--request`, `--accept-request-hash`, `--approval-key`, `--owner-account-id`,
  and `--expected-genesis` for the original portable request and independent pins.
- `--owner-state` for the owner-local journal, and the existing independently
  reviewed `--ledger-python`, `--ledger-helper`, `--ledger-helper-sha256`,
  `--ledger-backend`, `--ledger-backend-sha256`, and `--ledger-app-version` pins.
- The common `--durable-volumes` and `--durable-volumes-sha256` declaration flags.

The command uses a distinct recycle device-journal and reply schema. It shares
the original trim signer's physical one-request ownership and pinned SDK bridge;
old trim journal hashes and wire bytes remain unchanged. The metadata proof is
prepared before any device access. A durable `signing` intent precedes the one
hardware call. The adapter verifies the actual app version and displayed derived
owner account, then persists the public response before returning it. Go verifies
the native Ed25519 signature and complete original extrinsic.

Retain the journal, permanent marker and `.ledger-response` file. A missing
response after issuance is unknown and never permits another hardware call.
Re-running the same command recovers the identical retained reply without opening
the SDK or device. A different request, tool pin or app version cannot replace it.
The actual owner platform/device and current RFC78 artifact still need separate
qualification; synthetic adapter tests establish no physical-device claim.

Back on the host, `owner-recycle import-reply` takes the existing `--config`,
`--approval-key` and `--accept-action-hash`, plus `--accept-request-hash`, `--reply`
and `--reply-sha256`. It verifies the reply against the original exported request
already retained in host custody. Repeating the exact import is safe. A trim
reply, another request or another signature cannot replace those original bytes.
The earlier raw public-signature `import` command remains available.

The executable owner-side invocation is:

```sh
sn-mainnet owner-recycle sign \
  --request /owner/recycle/request.json \
  --accept-request-hash sha256:ORIGINAL_REQUEST_CONTENT_HASH \
  --approval-key 0xINDEPENDENT_ACTION_APPROVAL_KEY \
  --owner-account-id 0xEXISTING_OWNER_ACCOUNT_ID32 --expected-genesis 0xGENESIS \
  --owner-state /owner/recycle/custody/device-state.json \
  --durable-volumes /owner/recycle/durable-volumes.json \
  --durable-volumes-sha256 sha256:EXACT_OWNER_DECLARATION \
  --ledger-python /reviewed/python3 \
  --ledger-helper /owner/recycle/owner_ledger_adapter.py \
  --ledger-helper-sha256 sha256:REVIEWED_HELPER \
  --ledger-backend /owner/sdk/bittensor_core.abi3.so \
  --ledger-backend-sha256 sha256:REVIEWED_NATIVE_EXTENSION \
  --ledger-app-version 100.0.5 > /owner/recycle/reply.json
```

All uppercase pins are placeholders for independently delivered values; the app
version must match the separately qualified installed device. The command uses
only those owner-local files. Its embedded host state path is an inert namespace
label and need not exist on the owner's computer.

## Separate production submission approval

Use `owner-recycle submit-plan` with the original config/action/approval flags,
an independently reviewed `--production-authority-hash`, and optional
`--maximum-posts` from 1 through 8 (default 1). This makes no network request and
outputs an unsigned `approval_template` and exact hex `signing_bytes`.

The new signature domain is `urnetwork-mainnet-owner-recycle-submission-v1`.
Its approval binds the complete original config, exported request, signed
extrinsic, production-authority review, finite finalized-block window and
cumulative post ceiling. Every listed residual must be accepted explicitly.
The authority review covers production runtime provenance, release, finality and
global owner-key custody. The exact v470 artifact exception approved for planning
cannot supply this new signature or authorize a post.

Obtain an independent Ed25519 signature over the exact signing bytes and retain
the completed approval as a private file. `owner-recycle submit` requires the
original config/action/approval flags and all of:

- `--submission-policy` and `--submission-policy-sha256` for the signed file.
- `--submission-approval-key`, supplied independently on every invocation.
- `--production-authority-hash`, independently matching that approval.

For every host invocation below, also pass the original `--config`,
`--approval-key`, `--accept-action-hash`, and its paired daemon
`--durable-volumes`/`--durable-volumes-sha256` flags:

1. `import-reply --accept-request-hash sha256:ORIGINAL_REQUEST_CONTENT_HASH
   --reply /private/owner-reply.json --reply-sha256 sha256:EXACT_REPLY_FILE`.
2. `submit-plan --production-authority-hash sha256:INDEPENDENT_PRODUCTION_REVIEW`.
   Retain its exact template and signing bytes. The default ceiling is one post.
3. After separate approval, `submit --submission-policy /private/submission.json
   --submission-policy-sha256 sha256:SIGNED_SUBMISSION_FILE
   --submission-approval-key 0xINDEPENDENT_SUBMISSION_APPROVAL_KEY
   --production-authority-hash sha256:INDEPENDENT_PRODUCTION_REVIEW`.
4. `reconcile` retains and verifies that original transaction's receipt and mode.
   It needs the original action approval, but neither a device nor submission
   authority. Continue that original reconciliation after an uncertain post.

Once attached to original custody, the submission policy and ceiling cannot be
replaced, renewed or increased. The original nonce, era and signature remain
fixed. A different approval requires a separately designed migration; this
increment provides none. Older binaries refuse records containing the new
submission member; they cannot reopen them while discarding consumed allowance.

## One post and original recovery

Every invocation first reconciles the original signed extrinsic through complete
canonical mortal-era bodies. A receipt, nonce conflict or expiry cannot cause a
post. Current admission rereads the exact approved birth observation, runtime,
owner, subnet registration, nonce, Burn mode, balance, rate limit and admin
window. Every remaining possible inclusion block must satisfy the observed
window. The same exact approved public HTTPS route uses ordinary hostname/TLS
verification plus the signed SPKI pin; no Snow fallback is inferred.

Before one HTTP post, the command durably consumes a numbered reservation,
rechecks the finalized mapping and exact original physical custody, and submits
the retained bytes once. A changed head, lost custody, cancellation or uncertain
acknowledgment keeps that reservation consumed. The transport never retries a
write. A successful RPC acknowledgment remains pending and is not inclusion.

Retrying `submit` first reconciles the original transaction, including after its
post ceiling is exhausted. `reconcile` remains available without submission
approval and can complete original receipt/readback recovery. No recovery path
resigns, refreshes mortality or clears consumed attempts. A readback outage keeps
the financial receipt; later reconciliation can enrich only that same receipt.

Current reads cannot make generation, governance, owner/proxy behavior or
inclusion-time windows atomic. The signed policy states these residuals and that
the native fee can exceed its local reserve. Global custody across machines and
rollback protection remain external requirements. Finalized Recycle mode still
does not prove the separate 10% provider / 90% owner-recycle economic outcome;
every result retains `activation_ready: false`.
