# Proposed current-policy native capability

This slice makes the signed current-policy custody proposal concrete. Public
`contract-successor-execution-resume --submit` remains unavailable. The public
constructor supplies route zero, and no flag, evidence file or accepted signature
can install a production submission route. Installation requires a separate
reviewed release change after explicit policy approval and independent behavioral
qualification. No live RPC or real transaction was used to author this slice.

## Authority and custody

The existing independent reviewer must sign both the narrower current-storage
proposal and its separate acceptance envelope. The immutable acceptance journal
binds the original execution plan, original complete-history statement, exact
Safe/profile, runtime journal prefix and distinct reviewed evidence. Its original
history statement remains retained and authenticated as a signed statement; this
capability never claims that the statement's complete history was proved.

Public online resume may import one acceptance with `--safe-current-revision`
and `--safe-current-revision-sha256`. This changes local authority custody only.
It cannot submit. Bad or missing file pins refuse, and the original journal
validates both signatures, predecessor and publication recovery. Historical
receipt reconciliation remains available with public read-only construction.

An internal release route selects the concrete native capability only when the
latest accepted policy binds the complete retained runtime tip. Adding a runtime
revision requires a later independent current-policy acceptance before another
write; it cannot silently widen old policy authority. History and current-policy
capabilities cannot be mixed. Counted attempts and terminal outcomes keep the
exact policy hashes established by the custody slice, with original signatures,
nonce claims, counted attempts and maximum liabilities unchanged.

## Concrete proof and final admission

The adapter uses the original owned RPC client to obtain a native state proof at
one pinned finalized header. The previously reviewed verifier proves the full
Safe account-storage prefix, exact runtime code, proxy/singleton code and native
code metadata. Orphan owner/module mappings and other nonzero storage refuse.
An omitted intersecting proof branch cannot be treated as an empty prefix.

All expensive original-receipt, storage-proof and runtime-artifact work precedes
the final scoped pending pass. That pass checks known Safe words and empty
authority slots, proxy/singleton code, contract state, Safe getters, relayer nonce,
available balance and the exact known transaction hash. Cheap native identity,
runtime code-hash and canonical-continuity checks then re-admit the window. Every
read retains its existing separate bounded retry budget under caller cancellation.
The durable counted reservation is followed by another complete observation
before the one exact transport write; a lost reply does not cause a retry.

The admission head can advance while the original proof hash stays canonical.
The observation carries the exact proof block/hash/root separately from its
latest admission-window block/hash, and the pending observation names the exact
proof observation hash. Head advancement never relabels an older proof. A true
canonical mismatch refuses and clears previously admitted state.

The accepted policy explicitly owns the original RPC's finality assertions,
exclusive signer/relayer cutover and non-atomic pending checks. The node provides
no complete authenticated pending storage snapshot. An unknown pending orphan
or a change between independent reads cannot be excluded by these scoped checks.
The proof and pending observations continue to report history, complete pending
storage and independent send authorization as false. They are evidence for this
separately accepted policy, not a replacement complete-history attestation.

## Qualification status

Implementation and compile/vet work belong to Astra max. Sol medium owns all
behavioral qualification. The source is frozen only after compile and vet pass;
those checks alone do not qualify deployment or public route installation.

The deterministic fixture uses the published Safe proxy/singleton, original
eight-action local graph, independent native trie witness and synthetic signed
acceptance. It covers uninstalled/mixed/runtime-stale routes; public read-only
import and public submit refusal; moving-head evidence and true reorg refusal;
actual orphan owner/module words; late Safe nonce, relayer nonce and same-version
code changes; mutation during the second proof after durable reservation; one
exact lost-reply Safe execution; and later public read-only receipt recovery with
the original custody, policy references, nonce claims and liabilities intact.

The [sealed qualification note](evidence/safe-current-capability-qualification-20260930.md)
records two new and thirty-five adjacent roots passing normal/race (74 root
executions), eight normal and exactly five selected race controls causal, and
the exact source/dependency fence. Two original control-oracle mismatches remain
unresolved and preserved; fresh corrected reproductions use identical source,
tests and mutation patches. The complete heavy fixture supplies race coverage
for the three heavy mutations selected only in normal mode.
Public route installation and explicit approval of the narrower policy remain
separate open gates. Automatic runtime compatibility also remains a P0: retained
additive revisions are an incremental signed-authority path, not automatic
admission of arbitrary compatible upgrades.
