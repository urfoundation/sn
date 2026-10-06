# Owner-side Ledger signing and public handoff

The subnet owner runs `sn-mainnet owner-signing sign` on their own computer.
The command consumes one approved public request, confirms the existing owner
account on the Ledger, and emits one exact signed reply. It does not require
Snow access, a network endpoint, a key file or a private-key export. The operator
exports the request from retained preparation custody and imports the returned
public reply into that same custody.

The implemented setup action is the native
`AdminUtils.sudo_trim_to_max_allowed_uids(netuid, max_n)` call for SN25. The
selected maximum comes from the retained owner-trim review. This command does
not implement another subnet-owner action or EVM contract signing. The root
hotkey retains separate hardware custody; its device model is not selected
here. Reviewed v470 retires `SetRootWeights`, and unchanged registered/staked
root participation needs no periodic native signature. Any required current
root lifecycle mutation uses its independently identified owning/staker coldkey
or admitted proxy; it cannot inherit this owner Ledger or its approval. See
[current root participation](ROOT-CURRENT-PARTICIPANT.md). Operator demand-deposit
wallets remain separate vault-held keys.

This increment supplies a real pinned-SDK adapter and deterministic hardware
boundary fixtures. A concrete Linux SDK build and its separate behavioral
qualification procedure are recorded in [OWNER-LEDGER-SDK.md](OWNER-LEDGER-SDK.md).
These do **not** establish a qualified physical device, firmware or deployed
runtime. See the open gates below before treating output as launch evidence.

## Exact native contract

The selected launch reset is [best-effort owner trim](OWNER-TRIM-BEST-EFFORT.md).
Its fresh execution/action domains are
`urnetwork-mainnet-owner-trim-best-effort-execution-v1` and
`urnetwork-mainnet-owner-trim-best-effort-action-v1`. They use the same Ledger
fields and portable handoff below, with the best-effort selection rule and a
separate submission-risk signature. The strict v1/v2 formats described here
remain their original historical contracts; they do not establish an enforced
window on the selected runtime.

The original owner-trim v1 contract uses sr25519 `MultiSignature` variant 1 and
disabled `CheckMetadataHash`. Its signed bytes, approval domain, journals and
liabilities remain unchanged. A Ledger-derived account cannot be represented by
silently changing that action's signature type.

Strict Ledger setup instead requires a fresh independently signed execution schema
`urnetwork-mainnet-owner-trim-execution-v2` and action schema
`urnetwork-mainnet-owner-trim-action-v2`. The v2 template additionally supplies:

| Field | Meaning |
| --- | --- |
| `signature_scheme` | Exactly `ed25519`; native `MultiSignature` variant 0 |
| `signer_derivation_path` | The owner's existing `m/44'/354'/account'/0'/index'` path |
| `check_metadata_hash` | Independently approved RFC78 digest, canonical `0x` plus 32 bytes |
| `ledger_metadata_blake2b_256` | BLAKE2b-256 of the exact raw unwrapped metadata15 artifact |

The existing runtime profile separately pins the original metadata14 artifact,
runtime code, source commit and version. Both metadata artifacts must represent
the same approved runtime. A raw metadata hash is **not** the RFC78 digest. The
native payload carries mode 1 and `Some(digest)` for `CheckMetadataHash`, the
original call, compact nonce, zero tip, mortal era, genesis and original birth
hash. Import reconstructs and verifies the exact variant-0 extrinsic.

The independent approver signs the selected execution schema, a zero byte, and
canonical Go JSON with `approval_signature_ed25519` empty. V1 approval cannot
authorize v2. A claimed v1 owner-trim journal cannot be replaced with v2, even
with a fresh approval or after expiry. Existing signatures and unresolved
issuance must remain with their original owner.

## Prepare and export on the operator host

Use the original v3 or passive-root v4 chain preparation, run directory and
accepted plan hash from [BOOTSTRAP-CHAIN.md](BOOTSTRAP-CHAIN.md). The unsigned
best-effort template includes the
four fields above and the existing explicitly selected nonce, finalized birth,
mortal period, fee reserve, attempt limit, custody and owned-route fields.
`runtime14.hex` and `runtime15.hex` contain canonical lowercase `0x` hex. The
metadata15 file starts with raw `meta` and version 15; a SCALE
`Option<OpaqueMetadata>` RPC wrapper must be decoded by the artifact producer.
No conversion from metadata14 is synthesized.

```sh
sn-mainnet bootstrap-chain trim-plan --config /private/chain.json \
  --run-dir /private/custody --accept-plan-hash sha256:ORIGINAL_PLAN \
  --durable-volumes /private/durable-volumes.json \
  --durable-volumes-sha256 sha256:EXACT_HOST_DECLARATION \
  --trim-config /private/trim-best-effort-template.json \
  --metadata /private/runtime14.hex --ledger-metadata /private/runtime15.hex \
  --trim-approval-key 0xINDEPENDENT_APPROVAL_KEY
```

After the independent approval is attached to the emitted config, retain it as
`trim-best-effort.json` and use `trim-apply` with the same original custody flags
and `--trim-config /private/trim-best-effort.json`, without metadata flags. Then
export:

```sh
sn-mainnet bootstrap-chain trim-export --config /private/chain.json \
  --run-dir /private/custody --accept-plan-hash sha256:ORIGINAL_PLAN \
  --durable-volumes /private/durable-volumes.json \
  --durable-volumes-sha256 sha256:EXACT_HOST_DECLARATION \
  --trim-config /private/trim-best-effort.json --trim-approval-key 0xINDEPENDENT_APPROVAL_KEY \
  --metadata /private/runtime14.hex --ledger-metadata /private/runtime15.hex \
  > /private/owner-request.json
```

Export only accepts the original reserved, unsigned trim journal. It opens no
RPC connection and does not change the five original preparation journals or
the trim's nonce, era, allowance or retained identity. The request includes the
complete approved config, both metadata artifacts, scheme and exact signing
bytes. Its `content_hash` is an object seal; it is not the SHA256 of the JSON
file. The owner receives that seal and the approval public key, genesis and
existing owner AccountId32 through an independently authenticated channel.
Embedded operator paths are data; the owner command never opens them.

## Reviewed software on the owner's computer

Build `sn-mainnet` from the qualified source. Provision Python 3.10 or later,
this directory's `owner_ledger_adapter.py`, and a reviewed native
`bittensor_core` extension built from Subtensor commit
`67dcf7f791dc495064c293f080a0702cb433e51e`. The exact source is required: a
package name or version alone does not prove that commit. The upstream
[Python extension manifest](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/sdk/bittensor-core-py/pyproject.toml)
uses Maturin and Python's stable ABI; its
[Cargo features](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/sdk/bittensor-core-py/Cargo.toml)
enable Ledger support by default. The retained Linux build uses locked offline
Maturin release builds of that verified checkout. Exact source, lock, toolchain,
artifact pins and platform/reproducibility limits are in
[OWNER-LEDGER-SDK.md](OWNER-LEDGER-SDK.md). Its ELF requires GLIBC 2.38 and is not
a macOS or Windows bundle. Use only the separately qualified artifact and Python
runtime for the owner's actual platform.

Transfer the reviewed native extension file and its independently authenticated
SHA256 pin to the owner. The adapter loads that explicit file, not an arbitrary
installed package. It verifies the file hash before loading, rejects writable
group/world permissions, and requires `metadata_digest`,
`generate_extrinsic_proof` and `LedgerDevice`. The helper itself has a separate
SHA256 pin and is executed from its verified source bytes using Python isolated
mode. The Python executable and its runtime dependencies are part of the
reviewed owner bundle. No SDK network client is used.

Place owner files in a private directory with mode 0700; public request, helper,
reply and state files use mode 0600. Use a new dedicated owner-local state path
for this approved request, separate from any operator journal. Retain that
directory permanently through signing and reconciliation. Examples below use
placeholders for independently reviewed pins, not usable production approvals.

Before signing, use the public `storage-owner-prepare plan` and `apply` sequence
in [OWNER-CUSTODY-PREPARATION.md](OWNER-CUSTODY-PREPARATION.md) to provision the
exact `mainnet-owner-signing` snapshot. A private directory alone cannot satisfy
the signing-store constructor. Keep the portable inputs outside its fresh root,
and retain the returned owner-local declaration and exact hash. Host bootstrap
and trim commands similarly need their separate daemon declaration through the
common `--durable-volumes` and `--durable-volumes-sha256` flags; those host inputs
are not transferred as owner signing authority.

## Inspect, sign and return

Inspect the exact reviewed request first:

```sh
sn-mainnet owner-signing inspect --request /owner/trim/request.json \
  --accept-request-hash sha256:REQUEST_CONTENT_HASH \
  --trim-approval-key 0xINDEPENDENT_APPROVAL_KEY \
  --owner-account-id 0xEXISTING_OWNER_ACCOUNT_ID32 --expected-genesis 0xGENESIS
```

Review the subnet, maximum UIDs, account, derivation, runtime, metadata digest,
nonce and era. The signer must derive **that existing AccountId32**; being
Ledger-derived alone does not establish the correct account or path. Open the
reviewed Polkadot generic app on the owner's Ledger, then run:

```sh
sn-mainnet owner-signing sign --request /owner/trim/request.json \
  --accept-request-hash sha256:REQUEST_CONTENT_HASH \
  --trim-approval-key 0xINDEPENDENT_APPROVAL_KEY \
  --owner-account-id 0xEXISTING_OWNER_ACCOUNT_ID32 --expected-genesis 0xGENESIS \
  --owner-state /owner/trim/custody/device-state.json \
  --durable-volumes /owner/trim/durable-volumes.json \
  --durable-volumes-sha256 sha256:EXACT_OWNER_DECLARATION \
  --ledger-python /reviewed/python3 \
  --ledger-helper /owner/trim/owner_ledger_adapter.py \
  --ledger-helper-sha256 sha256:REVIEWED_HELPER \
  --ledger-backend /owner/sdk/bittensor_core.abi3.so \
  --ledger-backend-sha256 sha256:REVIEWED_NATIVE_EXTENSION \
  --ledger-app-version 100.0.5 > /owner/trim/reply.json
```

The version is an exact independently reviewed installed version, with generic
app major 100 and at least 100.0.5; it is not an automatic device qualification.
The adapter computes and checks the approved RFC78 digest and shortened proof
offline before opening USB HID. The command then durably records signing intent.
It requires the exact app version, asks the Ledger to display its derived
address, compares the returned public key to the independently pinned existing
owner, and calls the SDK clear-signing operation with the **unhashed original**
payload plus proof. The device handles native payload hashing when required.
The command never falls back to raw or blind signing. Payload plus proof is
bounded at 16 KiB.

Only `00 || signature64` is accepted from this Ledger path. Go verifies the
Ed25519 signature over the approved native signing bytes before producing the
reply. The reply binds request/action hashes, owner, signature scheme, signature,
exact signed extrinsic and its native transaction hash. Return the original
public reply file and independently communicate its exact file SHA256. Verify
locally using `owner-signing verify` with the same four independent pins and
`--reply /owner/trim/reply.json --reply-sha256 sha256:EXACT_REPLY_FILE`.

The operator imports that reply without the owner's device or keys:

```sh
sn-mainnet bootstrap-chain trim-import-reply --config /private/chain.json \
  --run-dir /private/custody --accept-plan-hash sha256:ORIGINAL_PLAN \
  --durable-volumes /private/durable-volumes.json \
  --durable-volumes-sha256 sha256:EXACT_HOST_DECLARATION \
  --trim-config /private/trim-best-effort.json --trim-approval-key 0xINDEPENDENT_APPROVAL_KEY \
  --request /private/owner-request.json \
  --accept-request-hash sha256:REQUEST_CONTENT_HASH \
  --reply /private/owner-reply.json --reply-sha256 sha256:EXACT_REPLY_FILE
```

Import reconstructs the approved original action and exact reply before entering
the existing signature custody transition. It never submits. Reimport of the
same signature is idempotent; another signature cannot replace retained bytes.
Existing unknown host signing cannot be resolved by importing a new reply.

## Recovery and remaining qualification

The owner journal has reserved, signing and signed phases, a permanent exclusive
marker and complete-claim marker. It binds the request, software paths and pins,
exact app version and proof hash. The adapter persists its exact device response
before stdout. Repeating the **same** command returns a retained verified reply,
or recovers the original durable response without accessing the device again.
Missing/partial response after intent is unresolved; the command never signs
again. Missing established journal, changed pins or a concurrent signer fail
closed. Preserve every original file. Do not change the state path, delete the
claim, regenerate an era/nonce or create a fresh request to escape uncertainty.
These local locks do not prove a global owner-account nonce fence across other
computers, software or independently approved requests.

`owner-signing reply` can wrap an already retained public signature and
`ledger-plan` can emit a bounded APDU transcript for a supplied proof. Neither
provides missing device provenance or current authority. The transcript retains
`device_qualified: false` and `metadata_proof_verified: false`.

The source basis is the pinned Subtensor
[Ledger HID signer](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/sdk/bittensor-core/src/signers/ledger.rs),
[Python device binding](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/sdk/bittensor-core-py/src/ledger.rs),
[RFC78 implementation and cross-implementation fixtures](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/sdk/bittensor-core/src/digest/mod.rs),
and Zondax's pinned
[app signature implementation](https://github.com/Zondax/ledger-polkadot/blob/644ec63851072f6eac9c3911ea23a811f3a1b15c/app/src/crypto.c).
The SDK's `merkleized-metadata` 0.5.1 accepts metadata15. Zondax raw signing
requires a different `<Bytes>`-wrapped message and is not a substitute for this
native extrinsic. No conclusion is based on generic app marketing.

Launch qualification still requires the owner's actual device/firmware/app and
existing key/path match, independently authenticated native extension and
passing native RFC78/adapter qualification for the owner's platform, and both
approved metadata artifacts against the actual runtime. In particular, Subtensor's
[runtime build](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/runtime/build.rs)
bakes the TAO/9-decimal RFC78 digest only when its metadata-hash feature is
enabled. Merely finding the signed extension in metadata does not establish
that deployed Wasm accepts mode 1. Current owner/subnet generation, canonical
era/nonce, enforced trim authority, global custody fencing, submission and
activation remain separate gates. No device or chain operation was performed
to qualify this source increment.
