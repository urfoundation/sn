# Bounded validator proof and worker observation

`sn-mainnet activate-validators admit-health` uses the original independently
signed activation envelope and its permanent operation allowance. It accepts
the same `--approval`, `--accept-approval-hash` and `--independent-public-key`
arguments as the other admission modes. Both static units must already be
installed. The command only reads chain, operator and host evidence and writes
its existing activation custody journal. It cannot start a service or sign.

The observation composes the qualified native checkpoint, current executable
and contract views, both roles' registered operator client keys and conservative
stake-capacity census at the same current native/EVM point. It then replays each
role's two **original pinned activation histories** through the real signed
record, trail and EMA verifier. Actual historical native eligibility, activation
publication, initial policy windows and every legacy terminal boundary are read
through the existing canonical native/EVM readers. The current client-key
response census remains a separate committed domain; it does not supply old
proof keys or replace the original activation.

This bounded increment proves the approved historical prefix. It does **not**
inventory the service UID's current mutable measurements, settlement closures,
unfinished trails, steering intents or successor generations. The existing live
history reader requires ownership by that service UID; root admission does not
weaken that guard, copy mutable state into a new authority, or interpret missing
files as empty history. Only a canonical, explicitly empty, pinned history can
prove a pristine prefix. `current_mutable_prefix_proven` remains false.

The proof reader admits exactly two operators, at most 16 MiB of combined pinned
history and 256 legacy closures per member, in addition to the original signed
runtime limits. Public verify-key replies are capped at 256 KiB. Historical RPC
attempts have a 60 second budget and one 300 second retry owner; verify-key GETs
use the existing 60/300 second owner. A pure timeout may retry against completed
mathematical work. Mixed transport/integrity errors and cancellation stop.
The enclosing original route deadline and sample-age limit remain authoritative
and can shorten these budgets; the command never extends a signed allowance.

Each completed role is synced separately in `completed_proof_checkpoints`, with
its original observation clock, exact config bytes, policy, operator domain,
generation, root and EMA digest. A later role/read refusal preserves completed
checkpoints. They are retained history, not a current readiness cache. A later
command performs fresh current reads and cannot substitute a different original
prefix. Ordinary `admit`, `admit-evidence` and `admit-stake` retain their scopes.

Worker observations consume the actual standard `urnetwork-validator-progress-v1`
file through protected physical paths and the approved service UID. Its progress
source hash is checked separately from the exact config-file hash. Acknowledged
manager invocation, source, instance, original start time and semantic progress
clocks must agree. A stopped fresh role can have no progress. Missing/stale
progress, publication retry and receipt/RPC wait are operational warnings;
wrong ownership, aliases, changed source/generation, rewritten protocol ages,
future clocks and reported hard errors refuse the observation. Read failure
retains the previous successful record without refreshing any protocol age.

The standard record exposes process, native scheduler, settlement and steering
health. It has no independent per-operator live worker attestation. The result
therefore retains explicit gates for current mutable-prefix completeness,
per-operator worker health, global signer custody, applied weights influence and
signed launch authority. Public `start` still supplies no activation authority.
Passing this command is not launch authorization.

Qualification uses synthetic signed fixtures, actual local HTTP/RPC transports,
protected files and deterministic deadline/cancellation transitions. No physical
device, deployed mainnet contract, live operator or production service was used.

## Service-owned committed prefixes

`sn-mainnet activate-validators admit-committed` accepts the same original
approval arguments and composes every `admit-health` stage with a bounded
observation of each role's **committed** current protocol history. The expected
UID comes only from that role's originally signed unit. The config's protocol
`state_dir` is distinct from the unit's operational `StateDirectory` and progress
file; substituting one for the other refuses.

The protocol root must already exist, be private, physical and service-owned.
Every ancestor must be protected against group/other writes and owned by root
or that service. The new read-only scope reads only `measurements/inputs` and
`settlement-closures-v2`; it cannot create either namespace, repair permissions,
change ownership, open a signing key or acquire a producer ledger. Both complete
namespace censuses stay held through capture, replay, historical source reads
and actual closes. Symlinks, extra hard links, unknown files, replacement,
removal, later publication and mixed close/cancellation failures refuse. An
absent suffix is observed under its retained existing parent; it never replaces
the independently pinned explicit activation origin.

The actual current cuts select content-addressed record and proof streams from
both original operator origins. Both copies must authenticate. The existing
archive verifier reconstructs complete signed record ancestry, lifetime trails,
generations, ordinary cuts, terminal transitions and EMA state in fresh private
scratch under the activation journal's host custody directory. It then observes
the original activation and every committed historical native/EVM boundary.
No journal header, imported ready projection or service self-report can supply
that verdict. Source controls are capped at 16 MiB, tape content at 48 MiB,
combined unique retained content at 64 MiB and source identities at 8192. Each
physical namespace has an 8192-entry census cap; all files including recognized
temporary names consume the 16 MiB history bound. Existing signed replay/disk
limits remain in force. Oversized history refuses this bounded mode.

Pure remote transport failures retain captured immutable chunks and completed
mathematical replay within the invocation. Remote attempts retain the existing
60/300 second retry policy, subject to the unchanged original route deadline
and sample-age limit. Local ownership/census changes are integrity refusals and
are not retried as absent state. The isolated scratch is removed after its
actual replay owners close, without deleting any producer or custody input.

Completed current observations are synced separately in
`completed_committed_checkpoints`. They bind the exact approved checkpoint,
service UID, current native/EVM and client-key domains, complete captured-source
census, and both replayed operator cursors. A later role failure retains the
completed earlier role. Re-observation proves every retained prior prefix is
an actual replayed ancestor; it cannot shorten history or reset observation,
generation or protocol clocks. Original approved checkpoints remain separate.

This closes the **committed control-prefix** subgate. The later
[bounded unsealed inventory](evidence/validator-unsealed-inventory-qualification-20261001.md)
also snapshots the actual signed ledger tails, unfinished trails, original
nonlegacy import receipts and an absent or canonically empty intent boundary
under service-UID custody. It retains that liability separately from committed
history. The bounded extension below authenticates every signed tail boundary;
nonempty intent graphs remain unverified. The standard progress format still
provides no per-operator live-worker attestation.
The result explicitly retains `UNSEALED_LEDGER_AND_INTENT_STATE_UNVERIFIED`,
`PER_OPERATOR_LIVE_WORKER_UNVERIFIED`, `GLOBAL_SIGNER_CUSTODY_UNVERIFIED`,
`APPLIED_WEIGHTS_INFLUENCE_UNVERIFIED` and
`SIGNED_LAUNCH_AUTHORITY_UNAVAILABLE`. MG-08 therefore remains incomplete until
the actual running generation supplies attributable worker evidence and the
unsealed/intent scope has an independently authenticated boundary. Public starts
remain closed. Owners continue signing on their own devices without Snow
access; each operator's demand-deposit wallet remains in its separate vault.

Deployment qualification must supply the original signed service UIDs and
protected protocol paths, reachable original replicas, and canonical historical
RPC sources. Synthetic qualification additionally runs one dedicated fixture
as root to prove a genuinely different service UID is readable without changing
the ordinary producer's current-user policy. That test only chowns its own
temporary fixture. No live deployment or worker-health closure is claimed.

## Canonical unsealed tail boundaries

The [independent qualification](evidence/validator-tail-boundary-qualification-20261001.md)
covers the frozen implementation and test-only causal-control successor.

`admit-committed` now derives a complete boundary census from the actual verified
records strictly after each replayed committed cut. Up to 256 distinct EVM
boundaries per operator are supported; repeated records share a boundary read
only within that same operator. Exceeding the bound refuses the observation.
Canonical hash-pinned historical reads authenticate each boundary's finalized
block identity, settlement epoch, approved policy window and operator
eligibility through the existing production reader. Neither an original signed
record nor its current-head ceiling supplies those historical facts.

Each ledger's optional `tail_boundary_proof` contains the ordered boundaries and
exact record counts. It is published only after every boundary for both
operators succeeds. An empty tail receives an explicit empty proof. Older
inventory checkpoints retain their exact original bytes and narrower scope;
later observations cannot discard a retained proof or alter the census under
an unchanged committed cut and ledger head. A new committed cut may consume
previously unsealed records without erasing their retained prior checkpoint.

The same protected service-owned source files and directories remain held
through the reads and actual closes. Every remote attempt joins local custody
checks before retry classification. Pure transport failures retain completed
signed replay and use the unchanged 60/300 second retry owner, bounded by the
original route deadline; cancellation and mixed local/integrity failures stop.
A later role failure preserves an earlier completed role's durable checkpoint.

This closes the bounded canonical **tail-boundary** subcase only. It does not
authenticate a nonempty pending/applied/current/history intent graph, historical
provider bindings inside individual records, independent live workers, global
signer custody or applied weights influence. The existing unsealed/intent and
launch gates remain open, and public `start` remains unavailable. Qualification
uses synthetic signed ledgers and local RPC transports; it supplies no deployed
mainnet or live-chain evidence.
