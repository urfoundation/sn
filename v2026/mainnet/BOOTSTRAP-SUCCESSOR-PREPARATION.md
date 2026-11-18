# Signed local successor preparation

The `bootstrap-chain contract-successor-preview`, `contract-successor-prepare`
and `contract-successor-resume` commands add one independently approved **local
preparation** under the original custody root. They preserve the five original
preparation journals, eight contract receipts, signatures and counted attempts.
Its [scoped independent qualification](evidence/bootstrap-successor-preparation-qualification-20260929.md)
passes thirteen focused and three adjacent roots normal/race, with all thirty-two
intended causal executions. It neither authorizes additional sends nor makes the
evidence anchor executable.

The [offline Safe review](BOOTSTRAP-SUCCESSOR-SAFE-REVIEW.md) can now borrow a
completed claim under shared directory ownership, independently reconstruct its
original graph and compute the exact anchor digest from pinned Safe releases.
That reader refuses partial claims without repairing them and leaves execution
approval, signature import and all live authority unresolved.

```sh
sn-mainnet bootstrap-chain contract-successor-preview \
  --config /private/chain.json --run-dir /private/custody \
  --accept-plan-hash sha256:ORIGINAL_V3_DIGEST \
  --request /private/successor-request.json
```

Preview reloads the original accepted v3 config and its pinned inputs, takes the
five existing shared preparation locks, and reopens all eight complete action
journals. It rebuilds the [unsigned successor proposal](BOOTSTRAP-CONTRACT-SUCCESSOR.md)
from those actual retained records. A report supplied by an operator cannot
replace this reconstruction. The request retains its existing strict 16 KiB
grammar and exact initializer-owner comparison; the declared Safe is unverified.

The preview contains `plan`, `preparation_plan_hash` and hexadecimal
`preparation_signing_bytes`. Its distinct Ed25519 approval bytes are the ASCII
domain `urnetwork-mainnet-successor-preparation-approval-v1`, a NUL byte, and
the compact Go JSON encoding of the complete preparation plan. The original
accepted contract config independently pins the public approval key. A key
inside a supplied successor envelope cannot substitute for that pin.

An independent approval system may produce this public envelope:

```json
{
  "schema": "urnetwork-mainnet-successor-preparation-envelope-v1",
  "plan": "EXACT_PREVIEW_PLAN_OBJECT",
  "signature_ed25519": "128_LOWERCASE_HEX_CHARACTERS"
}
```

The displayed plan placeholder must be replaced by the exact object. This
command suite supplies no private key, signing implementation, approval claim
on behalf of an operator, Safe signature or transaction encoding. Original
contract-domain signatures cannot authorize the new local preparation domain.

```sh
sn-mainnet bootstrap-chain contract-successor-prepare \
  --config /private/chain.json --run-dir /private/custody \
  --accept-plan-hash sha256:ORIGINAL_V3_DIGEST \
  --request /private/successor-request.json \
  --approval /private/successor-preparation-approval.json \
  --approval-sha256 sha256:EXACT_APPROVAL_FILE_DIGEST \
  --accept-successor-hash sha256:EXACT_PREPARATION_PLAN_DIGEST
```

Use `contract-successor-resume` with those same exact inputs after interruption
or output loss. Original pinned inputs and the approval source file must remain
available. Prepare requires unused fixed successor custody; resume requires
an existing matching claim. Exit 0 confirms only the local operation. Exit 1
means unresolved custody, cancellation or output; exit 2 means invalid flags
or approval; exit 3 means a changed accepted successor hash. No command accepts
RPC, signer, transaction import, submission, executable allowance or service flags.

## One fixed local claim

The signed plan includes original config/request pins, the complete retained
proposal and five preparation seals, plus the original directory's device and
inode. The fixed new files are:

- `contract-successor-preparation.json.lock`: exact approval hash, then completion.
- `contract-successor-preparation.json`: immutable `prepared-offline` record
  containing the complete signed envelope and its own content seal.

These paths and the `.contract-successor-preparation-` staged namespace cannot
overlap original inputs or validator custody. A directory descriptor holds one
exclusive nonblocking local lock. Reads and publication use that descriptor,
and every publication boundary rechecks the signed physical directory identity.
The original five shared locks remain held throughout; the original reserve
lock also fences every cooperating contract action writer in the existing graph.

Staged filenames include the exact approval hash. Even an empty stage reserves
the claimant once its directory entry is synced. The stage name is synced before
contents; complete contents are file-synced before a Linux no-replace rename,
then the directory is synced. Journal publication precedes marker completion.
An error closes the owner, retaining every file. The same approval may repair
only an exact prefix of its immutable staged bytes. Unknown content, competing
stages, links, extra hardlinks and nonprivate files refuse recovery. The root
census is limited to 512 entries; approval and record files are at most 256 KiB.

A complete marker with a missing or changed record refuses recovery. A partial
completion suffix also requires its already-published exact record. A pending
base marker may finish initial journal publication under the same approval.
Resume with no marker or matching claim stage refuses a fresh preparation.
Repeated complete resume leaves the journal and marker bytes unchanged. Another
signed successor or revised budget cannot overwrite the existing fixed claim.
This first preparation schema has no revision, deletion, migration or release
operation. A later transition needs independent approval binding this seal,
all original and successor liabilities, and its own qualification.

Ordinary process restart on the same physical root can resume. Copying the
directory, replacing its inode at the same path, moving it to another path, or
restoring onto a different volume cannot reuse this signed preparation. A
legitimate filesystem restore or volume move requires a separately approved
migration preserving prior seals; no such migration is implemented here.
This fence assumes a trusted private local filesystem and cooperating owners.
It is not cross-host custody, an unerasable anti-rollback record, or protection
against a privileged actor rewriting or deleting the entire custody root.
Crash hooks test process interruption around real fsync/publication boundaries;
they do not claim to emulate hardware power-loss behavior or unsupported filesystems.

## Conserved floors and unresolved authority

The separate [execution custody owner](BOOTSTRAP-SUCCESSOR-EXECUTION.md) can now
adopt this exact record under another signed domain, retain approved signatures
and both nonce claims, and conserve durable attempts through interrupted sends.
Its [scoped behavioral qualification](evidence/bootstrap-successor-execution-qualification-20260929.md)
is complete; its new concrete canonical adapter awaits independent qualification.
This preparation command's scope and flags do not change.

The new record retains the original complete approval/config and receipt seals
by reference. It never rewrites old plans or journals, signs old actions again,
or allocates nine fresh transactions. Eight retained attempts plus a proposed
increment of two yield a cumulative ceiling of ten, two remaining attempts,
one unfinished anchor and one retry. The proposed lifetime ceiling is original
maximum wei plus the approved preparation increment, preserving both completed
envelope reservations and any unexecuted ninth reservation. Receipt gas does
not replace original maximum exposure. The record has no executable allowance,
mutable send counter or new transaction signature field.

Only `preparation_approval_verified` and `local_preparation_complete` become
true. The nested unsigned proposal keeps its original false authority flags.
Execution approval, current chain verification, Safe authority, global signing
custody, executability, signing, network effects, installation and activation
remain false. Actual adoption for execution still requires canonical historical
receipt/postcondition reauthentication, exact selected Safe provenance/current
owner-threshold/module/guard/fallback state, Safe and relayer nonce custody,
the inner digest/signatures and approved outer envelope, funding and cumulative
durable send accounting. Installation requires canonical inner success and
exact coordinator/evidence binding. A new Safe must match the recorded
initializer owner or have separately authorized ownership migration.

Deterministic source tests cover independent approval, additive floors,
idempotent same-root recovery, competing claims, interrupted stage/file/directory
publication, partial/missing/rebound records, copied/replaced roots, prepublication
ownership barriers, unsafe files and a complete public v3 eight-action fixture.
The full graph reuses the qualified fixture's 60-second success-path send bound,
approved before original custody. No production timeout or lost-reply fixture
is changed. Behavioral results are supplied separately by the independent tester.
