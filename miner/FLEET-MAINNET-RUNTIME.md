# Mainnet fleet runtime authority

Production `provider fleet register`, `publish`, `bind`, `status`, and `revoke`
require a separately reviewed authority document and its approved SHA-256:

```text
--mainnet-runtime-authority=/absolute/path/reviewed-fleet-runtime.json
--mainnet-runtime-authority-sha256=<64 lowercase hex digits from approval>
```

There is no shipped mainnet genesis, runtime pin, or approval. An endpoint
observation, testnet release lock, testnet profile receipt, or successful dry
run cannot supply approval. The digest argument is the operator's explicit
selection of previously reviewed bytes; it is not a signature or a replacement
for the external approval process. Do not obtain it automatically from the
endpoint or from a newly generated observation.

The document is a strict JSON object, at most 16 KiB, with these fields:

| Field | Required meaning |
| --- | --- |
| `schema` | `urnetwork-mainnet-fleet-runtime-authority-v1` |
| `native_chain` | Independently approved exact native chain name |
| `genesis_hash` | Independently approved native genesis; lowercase nonzero `0x` plus 64 hex digits; known testnet genesis refused |
| `evm_chain_id` | `964` |
| `netuid` | Approved subnet, exactly matching the fleet manifest |
| `coordinator` | Approved coordinator, exactly matching the manifest; lowercase nonzero `0x` plus 40 hex digits |
| `runtime_source_commit` | Reviewed immutable runtime source commit; 40 lowercase nonzero hex digits |
| `runtime_review_scope` | `urnetwork-fleet-register-commitment-frontier-v1` |
| `runtime_review_sha256` | SHA-256 of the independently retained source/build/interface review; 64 lowercase nonzero hex digits |
| `runtime_version` | Complete object with `specName`, `specVersion`, `transactionVersion`, `stateVersion`; nonempty name and nonzero versions |
| `runtime_code_hash` | Exact reviewed `:code` BLAKE2b-256, lowercase nonzero `0x` plus 64 hex digits |
| `runtime_metadata_hash` | Exact reviewed SCALE metadata BLAKE2b-256, same hash grammar |

[`mainnet/fleet-runtime-authority/README.md`](../mainnet/fleet-runtime-authority/README.md)
says how a document gets reviewed, approved and published, and which source
checks each value. None is approved yet. `urnetwork-mainnet-fleet-runtime-authority-v2`
(`miner/fleet_runtime_catalog.go`) instead lists up to 8 reviewed runtimes, each with
the purposes it is reviewed for.

The review must establish source-to-Wasm provenance and bind those exact code
and metadata bytes. It must cover the consumed signed extensions, account and
registration storage/calls, fee quotation, commitment encoding/storage, finality
and dispatch events, the Subtensor `System.Account` u64 balance layout, plus
Frontier's first insertion of `Ethereum.BlockHash` during native finalization
and contract execution used by fleet commands. The loader verifies the document's
bytes and required coordinates; it does not perform that source/build audit.
Version numbers alone, or matching endpoint hashes without the independent
review, are insufficient. The reviewed coordinator deployment and subnet are
separate necessary deployment inputs; runtime admission is not a contract
implementation/deployment audit.

Each command loads the document once. Native genesis/name and the exact runtime
version/code/metadata tuple are checked at explicit finalized blocks. Registration
passes the independent artifact to the existing bounded-burn/fee signer, checks
again before signing and broadcast, and authenticates receipt state before its
registration readback. Publication similarly checks before signing and broadcast
and uses authenticated metadata at the exact write block. Status authenticates
both the native commitment and the EVM route.

Bind and revoke use native identity/runtime methods on the **actual EVM
connection**, as well as EVM chain ID 964. Therefore those routes must expose the
Substrate read methods too. They authenticate before producing client/hotkey
consent, on the submitter's connection before preflight/signing/broadcast, and
at the native block proven to contain the finalized EVM receipt. The coordinator
address must match the approved manifest scope. Revoke also compares the
remote finalized digest against the exact locally encoded revocation domain.
A reached wrong EVM identity does not trigger fallback to another route.

Chain 945 retains its exact release pin and explicit testnet provisional path.
Production flags cannot combine with testnet provisional flags, and production
cannot use a connection carrying provisional authority. A new runtime requires
a new independent review and approved document; no runtime is auto-adopted.
The offline `fleet manifest` command remains independent of network admission.

## Preparing storage for mainnet writes

`provider fleet storage` prepares, on the machine that will run the fleet writes,
the two journal directories and the one declaration they take. It reads no key and
sends nothing. It runs on Linux (ext4 or xfs) and macOS (a local APFS volume with
ownership enforced); elsewhere it refuses, as mainnet fleet writes do.

| Directory below the state directory | Opened by |
| --- | --- |
| `fleet-mainnet-recovery` | `register`, `publish`, `bind`, `revoke` |
| `fleet-native` | `register` |

**1. Read the filesystem's facts.** On Linux, for the mount point, uuid and type:

```sh
mkdir -p -m 700 ~/.urnetwork
findmnt -no TARGET,UUID,FSTYPE -T ~/.urnetwork
```

On macOS, home directories are on the data volume, which custody reaches by its
physical path. Set the state directory for these steps and for every later fleet
write. The mount point is `/System/Volumes/Data` and the type is `apfs`, and this
prints the uuid ([other volumes](../mainnet/MACOS.md#custody-volume)):

```sh
export URNETWORK_STATE_DIR="/System/Volumes/Data$HOME/.urnetwork"
mkdir -p -m 700 "$URNETWORK_STATE_DIR"
diskutil info -plist /System/Volumes/Data | plutil -extract VolumeUUID raw - | tr A-F a-f
```

**2. Write the request.**

```sh
provider fleet storage request \
  --mount_path=<mount point> --filesystem_uuid=<uuid> --filesystem_type=<type> \
  --former-writers-stopped --no-previous-owner-state \
  --evidence="what you checked"
```

The two flags and `--evidence` are your statement, and they are kept in the request
files. `--former-writers-stopped` says nothing else writes these directories and no
other machine signs with this fleet's keys. `--no-previous-owner-state` says these
signers have no earlier mainnet fleet journal anywhere, which
[the journal's guarantee depends on](#durable-mainnet-fleet-recovery). A local check
can't establish either one. If either isn't true, don't prepare new storage: a retained
journal must continue where it is.

This creates the directories it needs below the state directory, each with mode 0700,
and writes the request files to `fleet-custody`. It never changes an existing
directory's mode, and it refuses a journal directory that already holds files. It prints
a `{"path", "sha256"}` reference to the request and, on stderr, the next command with
that exact file and digest.

**3. Plan, and read what it plans.**

```sh
provider fleet storage plan --request=<path> --request-sha256=sha256:<digest>
```

This checks the request against the filesystem and changes nothing in the journal
directories. It refuses, among others: facts that differ from the kernel's for that mount
point, a directory that isn't private or isn't empty, leftover custody metadata, a full
or read-only filesystem, and any owner or capacity other than each journal's fixed
format. It prints the same kind of reference, to the plan, and the next command.

**4. Apply the plan.**

```sh
provider fleet storage apply --plan=<path> --plan-sha256=sha256:<digest>
```

The result's `declaration.path` and `declaration.sha256` are the values of
`--durable-volumes` and `--durable-volumes-sha256` on every later fleet write.

Each directory goes through its own control journal, with every step synced and read
back. The declaration appears only once both are complete. If apply is interrupted, or
its output is lost, run the same command again: until the first fleet write it continues,
and returns the same declaration. A plan never runs again over a journal in use.

**Afterwards,** keep the state directory and everything in `fleet-custody`, and use the
same `URNETWORK_STATE_DIR` on every fleet write. Don't restore any of it from a backup:
an older copy is refused, or rolls a journal back. Keep testnet fleet writes in another
state directory, since they use `fleet-native` too. On macOS, don't browse these directories
in Finder. To correct a request before any apply has run, remove `fleet-custody` and start
again. Once apply has run, don't remove anything: these directories can't be prepared a
second time.

This is the same preparation as `sn-mainnet storage-owner-prepare`, with the same checks
and the same fixed owners. That command produces one declaration per directory, and a
write takes only one, so `register` needs this one.

## Durable mainnet fleet recovery

Native absence checkpoints carry a signed semantic proof identifier,
`urnetwork-native-receipt-absence-v1`, bound to the same exact original transaction,
authority, signer and start block as the recovery record. Before recording such a
checkpoint, recovery authenticates the full raw header hash, canonical ancestry
and complete ordered extrinsics commitment. Null, omitted, shortened or malformed
bodies leave the outcome unresolved. Runtime-update digest tag 8 is preserved
without depending on the older SDK's header re-encoding.

Older records retain their transaction custody and final outcomes. An unresolved
old checkpoint without that proof identifier requires one rescan from its original
start; a qualified smaller cursor may replace it once. Thereafter cursors remain
monotone and the proof cannot be downgraded. This is independent of executable
hashes. [The candidate receipt](../mainnet/evidence/miner-receipt-prefix-candidate-20260928.md)
tracks qualification; source and tests are sealed before any pass is claimed.

All four production write commands own the same private
`$URNETWORK_STATE_DIR/fleet-mainnet-recovery` directory (default
`~/.urnetwork/fleet-mainnet-recovery`), and `register` also owns `fleet-native` beside
it. One directory-descriptor lock excludes
other processes for the entire operation. Every process using these fleet
signers must use that same retained state directory. Changing or deleting the
state directory, restoring an older backup, or independently operating the same
keys elsewhere is outside this local custody guarantee. Use a filesystem with
working exclusive locks and file/directory fsync; failures stop the command.

Both directories must be roots of one owner-local durable declaration
(`urnetwork-owner-local-volumes-v2`), named on every write with `--durable-volumes`
and `--durable-volumes-sha256`; without them a write stops with "explicit
durable-volume declaration and hash are required".
[`provider fleet storage`](#preparing-storage-for-mainnet-writes) prepares them and
produces it. `fleet status` and `fleet sign` open no journal and need no declaration.

Before the first send, the owner durably stores the exact signed transaction,
actor, nonce, transaction hash, native finalized checkpoint, original approved
authority bytes and digest, and canonical manifest/command/client/epoch intent.
The signing actor also signs these custody records and the complete inventory.
Files are private, regular and opened without following symlinks. Publication
uses file fsync, atomic rename and directory fsync, including newly created
parents. Interrupted valid candidates may complete that same local commit;
invalid candidates, missing initialized journals and signature changes fail
closed without erasing the previous journal.

The logical operation includes genesis/network, subnet, manifest generation,
fleet/hotkey and complete commitment semantics, command and client/epoch target.
Changing runtime approvals or fees cannot create a fresh operation. A pending
operation blocks every different local fleet write. Repeating a completed
operation reports its recorded result without another signature or broadcast.
Intentional republication requires a new canonical manifest/generation; this
interface does not silently refresh the same commitment at a new block.

After a crash or unknown send result, invoke the same command with the same
state directory, manifest, semantic arguments and original transaction signer.
Recovery first seeks the original exact transaction. Native recovery checks
canonical header/parent continuity, original runtime metadata, dispatch outcome,
and exact UID/coldkey or commitment-write readback. EVM recovery checks canonical
finalized EVM inclusion, exact signed bytes at the receipt's transaction index,
and the first insertion of that EVM hash into native runtime storage. Both the
native insertion block and its parent require the original approved artifact.
Matching heights are insufficient; different heights are accepted only when
that storage proof authenticates the mapping. Success also requires exactly one
matching coordinator event and binding/revocation readback at the receipt hash.

An absent receipt can permit replay only of the **identical signed bytes** after
original-runtime and finalized-nonce checks. Native replay additionally requires
a complete canonical absence scan. Consumed nonce, unavailable archive data,
missing mapping, conflicting readback or execution under a different artifact
retain the original liability and refuse replacement. A later current runtime
upgrade does not prevent historical recovery under the original artifact, but
does prevent retransmission unless the original current-runtime approval holds.
A new approval cannot retroactively reinterpret an old signature or receipt.

Canonical scans process at most 4,096 native blocks per invocation and persist
signed progress; rerun to continue a longer range. The journal is bounded at
256 operations and 16 MiB and does not prune old liabilities or completed
identities. Capacity exhaustion fails closed; durable archive/rotation authority
is a separate operational gate. No lost/deleted legacy transaction is backfilled:
deployment must establish that these signers have no preexisting unrecorded
mainnet liability, or reconcile it independently before using this writer.

Proven finalized dispatch/revert failures remain terminal failures on repeat,
with original bytes and inclusion evidence retained. A failure whose runtime or
mapping cannot be authenticated remains unresolved. Missing historical authority
or archive proof needs separately reviewed recovery authority, not a new nonce.
Terminal custody signatures attest the local verified observation; they are not
independent chain or economic acceptance attestations.

No mainnet transaction, deployment, or production pin selection is part of this
source qualification. Independent genesis, runtime source/build review, exact
code/metadata/version approval, approved coordinator/subnet, and live economic
and deployment qualification remain required.
