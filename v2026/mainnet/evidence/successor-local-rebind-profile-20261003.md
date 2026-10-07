# Explicit local successor restore approval

Production checkpoint `88ce38f5` adds a public local-root restore review and
retained execution path. Its preflight and compile-only results are separate
from behavioral qualification, which must identify its exact test-file layout
and dependency graph. This profile does not sign, broadcast, deploy, start a
service or approve a live restore. Storage preparation continues to return
`restart_authorized: false`.

`bootstrap-chain contract-successor-execution-local-rebind-preview` accepts the
original execution inputs, pinned independent execution approval and accepted
execution hash, plus `--local-restore-plan` and
`--local-restore-plan-sha256`. The separately pinned runtime volume declaration
must include the reviewed restored local root and all other required owners.
The single-root preparation output cannot silently replace that declaration.

The preview verifies the original signed execution, derives the unsigned local
member census from original bytes and reviewed transferred inodes, and accounts
for every co-owned snapshot file and checkpoint. It refuses omitted or
overlapping owners, repeated sources, changed capacities, unrelated members,
missing physical heads and a changed generation. This profile admits at
most 32 fixed owners. The settled-source checkpoint required no pending local
publication. Its pending-outcome successor additionally accepts one exact
original terminal event, as described below; it still refuses an unresolved
outer head or unrelated publication.
Co-owned snapshots must retain their exact original bytes and derived physical
heads. Actual passive preparation then reads the original signed record under
the restored root; unsigned inspection cannot borrow an exclusive writer.

The original independent approver may review and sign the emitted bytes in
domain `urnetwork-mainnet-successor-local-rebind-v1`. The envelope schema is
`urnetwork-mainnet-successor-local-rebind-envelope-v1`, with `plan` and
`signature_ed25519`. Its plan binds the original execution approval, old and new
physical roots, restored generation, complete restore plan, original inventory
and former-writer assertion, both census digests and runtime declaration.
There is no new key, signer, transaction, nonce or fee-allowance authority.

`contract-successor-execution-resume` and
`contract-successor-execution-readback` consume the envelope using
`--local-rebind-approval` and `--local-rebind-approval-sha256`. Fresh claim refuses
it. The writer revalidates the independent signature and complete physical
lineage, then requires the original completed claim, nonce claims and history.
Original signatures, execution hash, physical coordinates inside signed
records and nonce payloads remain byte-identical. One immutable local receipt
is added through the retained member publisher. Exact authenticated registry
and local receipt inventory is shared by replay and live checkpoints. Joined
retry may finish only that receipt's retained publication; an unknown receipt
or loss of a completed member refuses without recreation.

The pending-outcome successor adds optional `pending_outcome_sha256` to the
separately approved plan. Omission preserves the settled plan's signing bytes.
The digest binds the full original installed or outer-reverted event already
retained in the authenticated restored member census. An original reservation
may acquire its first stage inode, but an acknowledged inode, name, payload and
event authority cannot change. Offline resume reports
`physical_rebind_pending: true` and `local_custody_complete: false`; it retains
the original pending slot without inserting a new physical receipt. Missing or
pending canonical inclusion cannot cause another send. Exact canonical outcome
reconciliation completes the original journal first, then retains the physical
receipt. Publication uncertainty closes the affected owner and requires a
joined reopen of the exact original bytes. This source extension awaits its own
behavioral qualification and does not inherit a settled-root test result.

Ordinary online canonical/runtime/current-policy gates remain required.
Combined local-and-registry recovery is not qualified merely by the individual
source paths. Rebind chaining, remaining outer-head recovery,
retained in-place preparation, joined capacity/retention revision, current
published dependency composition and release/host approval remain required
work. Copied local files prove no remote database or object storage recovery.
