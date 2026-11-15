# Owner-side Ledger signing and public handoff

The subnet owner runs `sn-mainnet owner-signing sign` on their own computer.
The command consumes one approved public request, confirms the existing owner
account on the Ledger, and emits one exact signed reply. It does not require
Snow access, a network endpoint, a key file or a private-key export. The operator
exports the request from retained preparation custody and imports the returned
public reply into that same custody.

The implemented setup action is the native
`AdminUtils.sudo_trim_to_max_allowed_uids(netuid, max_n)` call for SN25. The
selected maximum comes from the retained owner-trim review. The actual SN25
owner is the `ur-owner` native multisig, so the launch trim uses the
[native multisig owner](#native-multisig-owner-ur-owner) workflow: two
signatories each sign their own exact request on their own computer and Ledger. This command does
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
runtime for the owner's actual platform. `sn-mainnet` and the adapter run on
macOS; a macOS owner still needs its own pinned `bittensor_core` build (see
[MACOS.md](MACOS.md)).

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

## Native multisig owner (`ur-owner`)

On October 6, finalized block 9,227,472 on runtime 473 reported
`SubtensorModule.SubnetOwner(25)` as the native multisig
`5HTeZ5168DjjGWZgbvzGfysEAj9fnexF24gYFKJHENU5cc8a` (AccountId32
`0xeeacb0f457acd4dca96b3545e7e165bad8e2f59040ba3fff24b34e4a051d5290`). It is
btcli preset `ur-owner`: threshold 2 over `brien-ur-owner`
(`5HnhcDkB7fQcbabDsmQXeP6N4fDKnciKjMkKCScUwkJYHH3u`, Brien's Ledger, Polkadot
generic app 100.0.26, Ed25519, `m/44'/354'/1'/0'/0'`), `jack-ur`
(`5GeoGiGEvUEQqTfTaUsvXqMQD4zVMNeYtYqMN8JLaMfiDp4J`) and `keith-ur`
(`5DFCNQmzRedo6hbZci4PTFMRuQJQ5PX6f6WrJBxDCDU3yBzS`). The account is
`blake2_256("modlpy/utilisuba" ++ compact(3) ++ sorted AccountIds ++ u16 2)`,
as pallet-multisig derives it.

A multisig has no key. The trim runs only when a second signatory approves the
same call, so each signatory signs **their own exact outer extrinsic** on their
own computer and Ledger:

| Step | Signer | Outer call | Effect |
| --- | --- | --- | --- |
| First approval | Any signatory, who becomes the depositor | `Multisig.as_multi(2, other_signatories, None, AdminUtils.sudo_trim_to_max_allowed_uids(25, max_n), max_weight)` | Opens the operation and reserves the deposit from the depositor. Emits `NewMultisig`. Trims nothing |
| Final approval | Another signatory | `Multisig.as_multi(2, other_signatories, Some(timepoint), same trim call, max_weight)` | Dispatches the trim with the multisig as origin. Emits `MultisigExecuted` with the inner result and releases the deposit |
| Cancellation | Only the original depositor | `Multisig.cancel_as_multi(2, other_signatories, timepoint, call_hash)` | Removes a stuck pending operation and releases the deposit |

Both approvals carry the complete nested trim call, not only its hash, so the
device can show `netuid` and `max_n`. For that reason the first approval uses
`as_multi` rather than `approve_as_multi`.

### Approved multisig domain

The workflow uses its own approval domain:
`urnetwork-mainnet-owner-trim-multisig-execution-v1` with action schema
`urnetwork-mainnet-owner-trim-multisig-action-v1`. It keeps the best-effort
selection rule, the Ed25519 Ledger envelope and the approved RFC78 digest.
`coldkey_account_id` is the subnet owner, the multisig account. The step's
outer signatory owns the nonce, mortal era, fee reserve, derivation path and
broadcast budget. The action adds a `multisig` object:

| Field | Meaning |
| --- | --- |
| `account_id` | The subnet owner multisig. It must equal `coldkey_account_id` and the derivation of the next two fields |
| `threshold` | Exactly 2 |
| `signatories` | The complete signer set as canonical AccountId32, sorted by raw bytes |
| `signatory_account_id` | This step's outer signer, a member of the set |
| `operation` | `as_multi` or `cancel_as_multi` |
| `timepoint` | `null` for the first approval. Later steps carry the first approval's finalized `{height, index}` |
| `inner_call_scale` | The exact trim call bytes, derived from the pinned metadata |
| `inner_call_hash` | `blake2_256(inner_call_scale)` |
| `max_ref_time`, `max_proof_size` | The inner weight bound passed to `as_multi`. Zero for a cancellation |
| `deposit_limit_rao` | First approval only. Bounds `DepositBase + 2 × DepositFactor` from the pinned metadata |

`call_scale` and `payload_scale` hold the outer call and its signing payload.
Preparation authenticates these in the approved metadata: `Multisig` at pallet
index 13 with `as_multi` = 1 and `cancel_as_multi` = 3, the nested
`RuntimeCall::AdminUtils` variant, the `Multisigs` storage shape and the
deposit constants. It refuses a signer set that does not derive the reviewed
owner. On runtime 473 the indices match, `DepositBase` is 132,000,000 rao and
`DepositFactor` is 32,000,000 rao, so the 2-of-3 deposit is 196,000,000 rao.

The portable request keeps its schema and fields. Its `approved_config` is one
step's independently approved config, which binds the inner call bytes and
hash. A multisig reply adds `signatory_account_id`, the outer signer;
`owner_account_id` stays the multisig owner. Direct-owner requests, replies
and adapter input are unchanged.

### Signing a step

Each signatory runs `owner-signing inspect`, `sign` and `verify` as above, with
two independent account pins:

```sh
sn-mainnet owner-signing sign --request /owner/trim/step-0/request.json \
  --accept-request-hash sha256:REQUEST_CONTENT_HASH \
  --trim-approval-key 0xINDEPENDENT_APPROVAL_KEY \
  --owner-account-id 0xMULTISIG_OWNER_ACCOUNT_ID32 \
  --signatory-account-id 0xTHIS_SIGNATORY_ACCOUNT_ID32 \
  --expected-genesis 0xGENESIS \
  --owner-state /owner/trim/step-0/custody/device-state.json \
  --durable-volumes /owner/trim/durable-volumes.json \
  --durable-volumes-sha256 sha256:EXACT_OWNER_DECLARATION \
  --ledger-python /reviewed/python3 \
  --ledger-helper /owner/trim/owner_ledger_adapter.py \
  --ledger-helper-sha256 sha256:REVIEWED_HELPER \
  --ledger-backend /owner/sdk/bittensor_core.abi3.so \
  --ledger-backend-sha256 sha256:REVIEWED_NATIVE_EXTENSION \
  --ledger-app-version 100.0.26 > /owner/trim/step-0/reply.json
```

A multisig request without `--signatory-account-id`, or a direct-owner request
with it, is refused. `inspect` shows the outer `Multisig` call. The adapter
receives the complete public signer set and threshold. Before it loads the SDK
or opens USB HID, it refuses unless the named signatory belongs to a sorted set
that derives the owner account. On the device it then requires the derived key
to equal the named signatory. Use a fresh owner-local state path for every
request, each with its own prepared `mainnet-owner-signing` snapshot (see
[OWNER-CUSTODY-PREPARATION.md](OWNER-CUSTODY-PREPARATION.md)). One state path
claims one request for good: signing again returns the retained reply without
the device, and another request is refused.

### RFC78 proof and display of the nested call

The adapter passes the complete outer call bytes to `generate_extrinsic_proof`.
The proof covers the whole extrinsic, so it carries the `Multisig` call types
and the nested `RuntimeCall::AdminUtils` call types. Per the pinned app source
(`app/src/metadata_parser.c` and `metadata_reader.c` in Zondax
`ledger-polkadot` at `644ec63851072f6eac9c3911ea23a811f3a1b15c`), the generic
app decodes the nested call from that proof like any other type and shows
`netuid` and `max_n`. It bounds recursion at depth 50, far above this nesting.
No physical device has displayed this call yet.

The opt-in `TestOwnerSigningNativeSdkMultisigNestedTrimProof` composes the
nested call with the native SDK. It checks the call bytes against this codec
and the proof's nested types, then prepares the proof through the unmodified
adapter. On October 6 it passed against an unqualified macOS build of
`bittensor_core` with the pinned upstream fixtures. A read-only check of that
build with runtime 473's metadata15 composed the identical outer call and gave
a 4,629-byte proof (4,824 bytes with the payload, within the 16 KiB bound). The
owner's qualified artifact must still pass these tests.

### Ledger signatories only

The treasury multisig flow supports only Ledger Ed25519 signatories, and this
workflow follows it. Every signatory who signs a trim step uses a Ledger with
the Polkadot generic app, at a hardened `m/44'/354'/account'/0'/index'` path
whose derived AccountId32 is that signatory. A software or sr25519 key cannot
sign a step through this tool, so the trim needs two such Ledger signatories.

This change edits `owner_ledger_adapter.py`. Re-review it and provision its new
SHA256 pin for every flow that runs the helper.

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
