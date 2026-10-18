# Proposed current-policy custody revision

This increment implements the durable local custody boundary for the separately
signed [current-authority proposal](SAFE-CURRENT-AUTHORITY-PROPOSAL.md). It does
not install a current-policy submission capability, expose a public policy import
flag, or enable public `--submit`. Its
[scoped qualification](evidence/safe-current-custody-qualification-20260930.md)
passes eight new and thirty-one adjacent roots normal/race, eight normal and
exactly five selected race causal controls, with source/dependency evidence sealed.

The immutable execution and original canonical authorization remain the base.
The original complete-history statement and its evidence remain retained and
authenticated as signed statements; this increment does not claim that their
historical assertions have been proven. The existing history authenticator
requirement is unchanged for the original policy.

## Separate acceptance and retained authority

`bootstrapSuccessorSafeCurrentRevisionApproval` wraps the independently signed
current-only proposal in a second domain-separated acceptance. Its original
independent approver signs the sequence, exact predecessor, complete proposal
and explicit acceptance policy. A proposal signature cannot replace the separate
acceptance signature. Both signatures and all private evidence are revalidated
on import, reopen and owner checkpoints.

The acceptance expressly retains the proposal's owned-node finality, non-atomic
scoped pending and all-signer-cutover assumptions. It does not claim a complete
pending proof. A concrete production route still needs separate implementation,
qualification and explicit policy approval before installation.

One hash-bound ordinal journal lives under the existing exclusive execution
owner. New imports must bind the complete retained runtime tip. Earlier policy
revisions retain their exact authenticated runtime prefixes when later runtime
approvals arrive; later revisions cannot roll that context backward. A partial
policy stage fixes one signed acceptance even when no bytes were written.
Competing revisions, gaps, changed encodings/signatures, extra stages and missing
counted authority are refused. Live owner checkpoints also detect an altered or
removed unreferenced suffix. The local custody threat model remains the same:
these files do not provide an external hardware-backed monotonic rollback log.

## Attempts, outcomes and recovery

New counted attempts reference the exact completed policy revision with an
optional `safe_current_revision_hash`. Omitted fields preserve old event bytes
and seals. Policy import never rewrites an event, releases either nonce claim,
changes the signed Safe/relayer transaction, replenishes attempts or reduces
maximum liabilities.

An outcome retains the policy of the exact counted attempt that produced it,
even when a newer current-policy revision was subsequently imported. Its runtime
reference may include later independently reviewed inclusion artifacts. An
already included transaction remains reconcilable under retained historical
authority even when current submission capability is unavailable.

An interrupted counted reservation is reconstructed and remains consumed. A
partial terminal intent must complete from exact canonical reconciliation before
new policy or runtime authority can arrive. An incomplete runtime revision blocks
new current-policy import, and an incomplete current-policy revision blocks new
runtime import and every reservation/send. Publication errors close the owner;
recovery must reopen all original locks and custody.

## Remaining production path

The next isolated slice must connect the qualified native proof collector to a
distinct concrete current-policy capability, select it only from complete signed
acceptance under the owner, and bind admission to the exact retained revision.
It must finish expensive prefix/runtime proof work before final scoped pending
Safe/relayer state, nonce, balance and exact-hash checks, followed by the one
retained exact-byte send. This current-only adapter must not implement the
complete-history interface or report historical provenance as verified.

Public command construction must remain closed until the user approves the
explicit boundary and a separately qualified production route is installed.
There is no caller-supplied flag or signed file that can install that route in
this increment. The journal can be reviewed and qualified independently while
that concrete adapter is completed; no policy approval is requested yet.

Eight deterministic roots cover independent acceptance and legacy encoding,
preserved original/nonce/count/outcome custody, unavailable capability,
publication recovery, event substitution, lost/forked history and interrupted
event ordering. Astra max owns implementation, fixes, compile-only checks and
vet. Sol medium alone runs behavioral qualification and causal controls.
