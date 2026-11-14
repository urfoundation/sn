# Safe current-policy native capability

Public `contract-successor-execution-resume --submit` has a separate current-only
route gated by an independently signed **v2 risk-policy acceptance** and an exact
`--accept-safe-current-policy` revision hash. Without that opt-in it stays closed;
v1 acceptance remains public read-only even with the flag. This implementation
does not accept the production risk policy, sign an approval or authorize a live
transaction. No live RPC or real transaction was used to author this increment.

## Authority and custody

The existing independent reviewer must sign both the narrower current-storage
proposal and its separate acceptance envelope. The immutable acceptance journal
binds the original execution plan, original complete-history statement, exact
Safe/profile, runtime journal prefix and distinct reviewed evidence. Its original
history statement remains retained and authenticated as a signed statement; this
capability never claims that the statement's complete history was proved.

Public online resume may import one acceptance with `--safe-current-revision`
and `--safe-current-revision-sha256`. Import alone changes local authority custody
only. Bad or missing file pins refuse, and the original journal
validates both signatures, predecessor and publication recovery. Historical
receipt reconciliation remains available with public read-only construction.

The concrete native capability requires the latest accepted policy to bind the
complete retained runtime tip. Adding a runtime
revision requires a later independent current-policy acceptance before another
write; it cannot silently widen old policy authority. History and current-policy
capabilities cannot be mixed. Counted attempts and terminal outcomes keep the
exact policy hashes established by the custody slice, with original signatures,
nonce claims, counted attempts and maximum liabilities unchanged.

## Explicit public v2 acceptance

The v2 envelope keeps the existing fields and ordinal journal. Its schema is
`urnetwork-mainnet-successor-safe-current-revision-v2`; its Ed25519 signing domain
is `urnetwork-mainnet-successor-safe-current-revision-approval-v2`, followed by a
single zero byte and the compact Go JSON encoding of the authorization. The exact
`accepted_policy` is `bootstrapSuccessorSafeCurrentPublicRevisionPolicy` in
[`bootstrap_successor_safe_current_authority.go`](bootstrap_successor_safe_current_authority.go).
Unknown versions, weakened policy text, a reused v1/proposal signature or another
key refuse. The original independently pinned approver signs both the nested
proposal and this distinct acceptance. V1 bytes, domains and custody are unchanged.

V2 expressly accepts bounded public current-only submission on the original owned
route, owned-RPC finality, non-atomic scoped pending reads and exclusive signer and
relayer cutover. It names the absent complete deployment/delegatecall history and
pending proof, and the inability to exclude changes between reads. It retains all
original approvals, receipts, exact signed bytes, nonces, finite attempts, native
window, fees and maximum liabilities. The earlier proposal's deferred submission
condition is satisfied only through this separate acceptance and concrete route;
its signature alone never changes meaning.

Review and import the v2 envelope with read-only online resume first. The output's
`safe_current_revision_hash` is the canonical acceptance **object** hash, which
can differ from a whitespace-formatted input file's SHA-256. A later independently
authorized send adds both `--submit` and
`--accept-safe-current-policy sha256:EXACT_RETAINED_V2_OBJECT_DIGEST` to the same
original online resume invocation. This opt-in is required on every invocation
that requests submission. It cannot accompany preview, claim or read-only resume,
and cannot mix with the complete-history capability.

After import, the separate revision input file may be omitted: recovery uses the
immutable original journal. A missing acceptance, wrong opt-in, changed evidence,
partial policy/runtime stage or stale reviewed runtime refuses before another
attempt or send. An explicitly imported valid revision can remain durably retained
even when its caller supplied the wrong opt-in; that refusal sends nothing. After
a timeout or interrupted reservation, retain every original file and reconcile
with read-only online resume. Do not manufacture replacement signatures or reset
the nonce registry. A later revision never rewrites an earlier counted outcome.

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

The October 1 public v2 increment has deterministic synthetic author checks and
separate independent review. The [public-route qualification receipt](evidence/safe-current-public-qualification-20261001.md)
records its exact source, tests, controls and remaining production gates.
All 68 author-selected roots pass normal/race, all three causal control pairs reach
their intended assertions, and vet passes. Independent static review finds no
blocker; its 14 focused roots pass normal/race and vet on the separately retained
server graph. These results supply source qualification without production policy
acceptance or a live send.

The deterministic fixture uses the published Safe proxy/singleton, original
eight-action local graph, independent native trie witness and synthetic signed
acceptance. It covers uninstalled/mixed/runtime-stale routes; public read-only
import and public submit refusal; moving-head evidence and true reorg refusal;
actual orphan owner/module words; late Safe nonce, relayer nonce and same-version
code changes; mutation during the second proof after durable reservation; one
exact lost-reply Safe execution; and later public read-only receipt recovery with
the original custody, policy references, nonce claims and liabilities intact.

The earlier [sealed qualification note](evidence/safe-current-capability-qualification-20260930.md)
records two new and thirty-five adjacent roots passing normal/race (74 root
executions), eight normal and exactly five selected race controls causal, and
the exact source/dependency fence. Two original control-oracle mismatches remain
unresolved and preserved; fresh corrected reproductions use identical source,
tests and mutation patches. The complete heavy fixture supplies race coverage
for the three heavy mutations selected only in normal mode.
That earlier source deliberately had no public route; its receipt is unchanged.
Actual independent v2 risk-policy acceptance, signer cutover and live qualification
remain required. Automatic runtime compatibility also remains a P0: retained
additive revisions are an incremental signed-authority path, not automatic
admission of arbitrary compatible upgrades.
