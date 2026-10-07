# Proposed Safe current-authority proof

This isolated implementation proves the complete finalized storage of one exact
Safe account and the code that interprets it. It is a **policy proposal and
read-only observation capability**. It neither satisfies the retained
complete-history attestation nor enables public successor submission. Activation
requires explicit approval of a different policy, a qualified production
authenticator, and custody integration. [Independent scoped qualification](evidence/safe-current-storage-qualification-20260930.md)
passes all eleven new and ten adjacent roots normal/race, all ten normal controls
and exactly five selected race controls, with exact source/dependency evidence
sealed. That result does not approve the proposed policy or activate submission.

## What can be proved with the existing native RPC

The reviewed Subtensor codec source is
`67dcf7f791dc495064c293f080a0702cb433e51e`; the reviewed SDK source is
`cacb4310f20c7cac83eb3ccd8ed5a5ad4212608a`.

- `chain_getHeader(nativeHash)` returns the header whose SCALE hash binds the
  native state root. The caller already selected this finalized hash through the
  independently approved owned route.
- `state_getReadProof(keys, nativeHash)` returns raw native `StorageProof` blobs
  and `at`. The returned hash must equal the selected hash. The proof root comes
  from the authenticated header, never from a witness-supplied root field.
- `chain_getBlockHash(nativeHeight)` rechecks the original hash after all proof
  work. Ordinary advancement of the finalized head does not force a restart.

These RPC definitions and implementation are in the pinned SDK's
`substrate/client/rpc-api/src/state/mod.rs` and
`substrate/client/rpc/src/state/state_full.rs`. The production collector adds a
single specialized bounded read; the general RPC allowlist remains unchanged.
Each RPC gets its own retry budget under caller cancellation. There is no live
RPC invocation in this implementation or its fixtures.

The pinned Frontier `vendor/frontier/frame/evm/src/lib.rs` defines:

| Item | Key after `Twox128("EVM") ++ Twox128(item)` | Value |
| --- | --- | --- |
| `AccountStorages` | `Blake2_128Concat(H160) ++ Blake2_128Concat(H256)` | raw H256 |
| `AccountCodes` | `Blake2_128Concat(H160)` | SCALE Vec<u8> |
| `AccountCodesMetadata` | `Blake2_128Concat(H160)` | little-endian u64 size followed by H256 Keccak code hash |

The runtime calls these same storage maps for code/storage reads. Its EVM runner
removes zero storage words. The deployed native `:code` value is proven at the
same root and must match an independently retained runtime artifact. The codec
source pin identifies reviewed storage semantics; it does not by itself prove
source-to-Wasm reproducibility. The existing signed runtime review remains
necessary. Unapproved upgrades and incompatible same-version artifact changes
remain closed.

## Complete account coverage

The collector requests fourteen keys: native runtime code, proxy and singleton
code/metadata, and nine permitted Safe storage locations. No node enumeration,
pagination EOF, `eth_getProof`, tracing API or transaction-pool census is needed.

The proof verifier traverses **every intersecting subtree** under the Safe's
`AccountStorages` prefix. A missing hashed child or external value refuses the
proof. Sibling subtrees outside that prefix do not require disclosure. If the
node omits a hidden mapping from the requested key set, the unresolved branch
still causes refusal. If the node supplies it, the extra storage word causes
refusal. Successful point reads alone never imply complete coverage.

Only these nonzero words are admitted for the pinned 1.4.1/1.5.0 Safe/SafeL2
layouts:

- slot zero: the exact approved singleton, with zero address padding;
- slots three/four: owner count three and threshold two;
- slot five: the exact retained Safe nonce, or proven absence when it is zero;
- module mapping slot one: sentinel points to itself;
- owner mapping slot two: sentinel and three approved owners form one complete
  acyclic list, in any order, ending at sentinel.

All other storage must be absent. This includes orphan owner/module entries,
guards, fallback handlers, deprecated domain storage, signed messages, approved
hashes and unknown storage baggage. The implementation does not attempt to
reverse arbitrary Solidity mapping hashes. A previously used Safe with extra
nonzero message/hash storage is explicitly outside this strict proposal and
requires a separately reviewed extension; the verifier will not ignore it.

Both proxy and singleton code must match the published release pins. Their
native code metadata must exist and match exact byte length and code hash. This
strict requirement also refuses legacy accounts with absent metadata, although
Frontier may otherwise compute missing metadata lazily. The shared proof binds
all these facts and native runtime code to the same root.

This is a current-state fact. An account can have this exact clean state after
an unsafe initialization or a past delegatecall that later restored the state.
It does not establish clean deployment, constructor provenance, every historical
storage write, or absence of past unauthorized actions. The observation reports
`deployment_history_verified: false` and cannot implement
`bootstrapSuccessorSafeProvenanceAuthenticator` honestly.

## Proposed independent policy and custody boundary

The concrete signed format is
`bootstrapSuccessorSafeCurrentPolicyAuthorization`, with schema
`urnetwork-mainnet-successor-safe-current-policy-proposal-v1` and a separate
Ed25519 signing domain. The original independent approver must sign:

1. Exact execution-plan hash and immutable original canonical-authority hash.
2. Complete retained runtime-revision tip and one already approved full runtime
   profile. A partial revision or unapproved new artifact is refused.
3. Exact Safe and singleton addresses, release/variant, proxy/singleton code pins.
   Owners, threshold, nonce, transaction bytes, relayer and window remain bound
   through the immutable execution plan.
4. A separately pinned private review file. It cannot reuse original build,
   runtime, signer-cutover or history evidence or overlap retained custody.
5. The exact proposed policy text in
   `bootstrapSuccessorSafeCurrentPolicy`: current-only completeness; strict empty
   extra storage; no history claim; owned-node finality and scoped, non-atomic
   pending assertions; all-signer cutover; unchanged receipts, signatures, nonce
   claims, attempts, window, fees and liabilities; public submission still closed.

Validation reauthenticates the original canonical/history statements and every
runtime predecessor; the proposal cannot rewrite or erase them. Its signature
authorizes review of this different boundary, not an assertion that the original
complete-history requirement has been fulfilled. There is deliberately no CLI
flag, policy-journal import, counted-event reference or production authenticator
selection in this slice. A signed file cannot turn the existing gate off.

If the user approves this alternative policy, the next implementation must add a
distinct immutable authority-revision chain under the existing exclusive custody
owner, retain predecessor/history evidence, hash-bind interrupted publication,
reference the exact completed revision from new counted/outcome events, refuse
forks/deletion/rollback and block sends during partial publication. Historical
inclusions must remain reconcilable under their retained original authority. It
must not rebuild the execution, release nonces, reset attempts or replace signed
transaction bytes. That integration needs its own causal controls and independent
qualification before a public production selector can be considered.

## Pending-state limitation and final ordering

Pinned Frontier `vendor/frontier/client/rpc/src/eth/pending.rs` constructs a new
runtime overlay from the current best block, inherents and ready transactions for
each call. `eth_getStorageAt`, `eth_getCode` and `eth_call` can query that overlay,
but the reviewed RPC exports neither its complete native state root/proof nor an
atomic multi-read snapshot token. `state_getReadProof` addresses committed native
block hashes only. A complete finalized prefix proof therefore cannot be silently
extended to the pending overlay.

The separate scoped pending helper rechecks proxy/singleton code, every admitted
Safe word, empty guard/fallback namespaces and reserved layout words. A changed
nonce, link, guard or code refuses. An unknown newly created orphan can escape
those finite reads; deterministic tests preserve that fact. Its output sets
`complete_pending_verified: false` and `send_authorized: false`, including when
every scoped value matches.

An approved policy would need to accept the owned-node assertions and the
independently attested signer cutover explicitly: all three Safe signers and the
relayer are fenced to retained custody, there are no other live Safe signatures
or relayer transactions, and no other principal can introduce an authorized Safe
transition. This is an external trust condition, not a mathematical consequence
of the proof. Pending native administrative/runtime transitions and changes
between calls also remain outside the complete-prefix fact. If that boundary is
unacceptable, the owned node must provide a separately qualified atomic pending
proof/snapshot adapter; current APIs cannot manufacture one.

All expensive finalized proof/history work must finish before the final scoped
pending re-admission. A future production adapter must then recheck the exact
Safe nonce, relayer confirmed/pending nonce and balance, known transaction hash,
runtime/window, original canonical hash and custody revision, followed directly
by the one authorized exact-byte send. Work inserted after pending admission
must trigger renewed admission. These ordering and attempt-budget constraints
already exist in the qualified successor path and must be preserved.

## Scoped qualification

Eleven new top-level roots cover the independent SDK vectors and encoder
commitments, all four published Safe variants, genuine orphan owner/module
storage, missing intersecting branches/external values, extra words, malformed
proofs, root/header/snapshot changes, code/metadata/runtime substitutions, moving
heads and true canonical mismatches, distinct signed authority and pending limits.

The unchanged SDK oracle is copied from the independently qualified server
native-state work: SHA-256
`b875b9eb1aa497233f68bb4cae02b4331320fcaeeaaefbefe867658f2aebaecd`.
Its eighteen vectors use the pinned Rust SDK's recorder and distinguish raw
StorageProof from generated trie-proof encoding. The mutable Safe fixture's
independent test encoder must reproduce all nine layout-one SDK roots before its
proof mutations can qualify. Actual published Safe bytecode supplies owner/module
authorization and storage layout behavior.

Astra max owns implementation, debugging, compile-only checks and vet. Sol medium
ran the frozen new roots and ten existing Safe/provenance regressions normal/race,
plus all ten normal and five selected race causal controls. The exact matrix and
sealed evidence are in the qualification note. No live RPC, real key, signing
operation or transaction was involved. The result qualifies only the explicit
read-only fact and proposal boundary above; it does not approve a policy change.
