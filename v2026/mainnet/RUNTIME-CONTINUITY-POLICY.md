# Production runtime continuity: signed policy and inspection boundary

This is a concrete RT-04 proposal and read-only validator verification path.
It does **not** complete automatic compatible-upgrade admission or authorize a
new production runtime. The [scoped independent qualification](evidence/runtime-continuity-policy-qualification-20260930.md)
passes fifty positive root executions and seven normal/three selected race causal
controls. This qualifies the proposal and inspection boundary only.

The standard validator reaches `authenticateOwnerRecycleProductionRuntimeAtContext`
through startup, preparation and the current-runtime gate. Its independently
signed schema-3 config selects one exact runtime within a finite native window.
A later artifact still stops new production work, even when its consumed metadata
looks identical. Existing approved historical windows and retained source receipts
keep their original authority. The mainnet miner's durable recovery already uses
`recoveryNetwork` and the original retained artifact; changing its dial gate would
not fix an absent problem. Fresh miner work remains exact-artifact gated.

Metadata proves encodings, not economic or execution equivalence. No available
repository component proves a new Wasm preserves staking, emission denominators,
timelocks, fees, source atomicity or Frontier behavior. The testnet provisional
profile is not mainnet authority. The additive bootstrap runtime-revision journal
also remains an independently approved artifact path, not automatic compatibility.

## Independently approved envelope

`validator.ProductionRuntimeContinuityPolicy` is signed by the approver already
authenticated in the original schema-3 config. Its envelope binds:

- Exact original config and detached approval hashes, genesis and base artifact.
- The existing validator producer interface profile.
- A separate semantic-verifier Ed25519 key, executable SHA-256 and rules SHA-256.
- All seven required semantic domains listed below.
- A finite native interval contained entirely in the original approval window.

The verifier key must differ from the original approver, validator census, owner
hotkeys and subnet owner. These public-key checks do not establish physical device
or off-node custody. No shipped key, mainnet pin or fallback is supplied. The
policy cannot change policy economics, routes, keys, completed work, original
economic activation or liabilities because it binds the full original authority.

Both signers use domain-separated canonical JSON digests returned by the exported
`SigningMessage` methods. Signatures use lowercase 64-byte hex; hashes stored as
`[32]byte` use the repository's JSON byte-array encoding. Candidate code/metadata
hashes are canonical nonzero `0x` hex, and source commits are lowercase 20-byte
hex. Documents reject duplicate/unknown keys, trailing JSON and sizes over 64 KiB.

## Semantic verifier request and output

`ProductionRuntimeSemanticVerifier.VerifyProductionRuntime` is the explicit
interface for a separately qualified verifier. **No genuine implementation ships.**
The request binds exact signed-envelope bytes, the candidate's complete
version/code/metadata identity, source commit, source-to-Wasm evidence hash and
native validity window. The result binds that whole request, the pinned verifier
executable and rules, compatible outcome, retained evidence hash and every domain:

1. Consumed calls, storage, events and runtime APIs.
2. Signed extensions, fees, nonces and mortality.
3. Atomic source commitment and dispatch.
4. CRv4 timelock domain and role.
5. Native epoch clock and schedule.
6. Stake eligibility and emission denominator.
7. Frontier native/EVM mapping.

The result is signed by the separate verifier key. A node assertion, copied
metadata or unsigned boolean cannot replace it. Unknown future spec numbers need
no compiled entry; the supported family, transaction/state encodings and producer
interface remain fixed. This initial proposal refuses rollback, same-version
changed artifacts and unchanged-code relabeling. Those require separate review.
One certificate is checked at a time; there is no ten-revision lifecycle ceiling.

A valid signature authenticates a semantic assertion. It does not prove that the
claimed verifier ran, that its evidence exists, or that the new Wasm has equivalent
economics. A qualified verifier must replay/check retained source/build and Wasm
evidence for every required domain, produce auditable output provenance, and have
independently approved signing custody. Those are concrete missing proofs.

## Read-only production-aligned inspection

`InspectProductionRuntimeContinuityContext` accepts an already independently
loaded schema-3 config, its owned connection and bounded policy/certificate bytes.
It validates original authority and both independent signatures before Rpc,
then checks fresh native/genesis/EVM identity, complete hashed native headers,
canonical finality, certificate window and the candidate's exact artifact.
The existing producer capability checker independently verifies consumed metadata,
signed extensions and runtime APIs. A certificate cannot waive those checks.

The snapshot remains fixed while a later head advances. The initial head and
selected snapshot must remain canonical at the end, and local original authority
is revalidated before output. Returned evidence names both snapshot and later head.
Finality and artifact observation retain the owned-Rpc assumption; independent
native finality or account-state proof is not claimed.

The inspector returns only a report. It never binds metadata to a signing view,
opens custody, writes a runtime revision, signs, submits or saves a certificate as
authority. `semantic_certificate_authenticated` may be true;
`semantics_independently_replayed`, `production_selection_installed` and
`signing_authority` remain false. The existing production selector continues to
require the original exact approved artifact after successful inspection.

## Remaining production selection and retention path

Before automatic continuity can be enabled, independently approve this envelope
and qualify a genuine semantic verifier against changed-code and economic controls.
Install a separate selection owner only after it authenticates/replays the verifier
output and independently approved original authority. That owner must atomically
retain policy, exact certificate/proof output and source/build inputs before any
new runtime view becomes eligible; interrupted publication must remain ineligible.

Current construction must receive one immutable purpose-bound view. Immediately
before signing/sending it must re-admit current artifact, nonce and applicable
authority. Pending original signed bytes keep their original signing context;
historical receipts select exact inclusion and execution-parent artifacts from
retained history. A new certificate cannot rewrite original config/approval bytes,
nonce claims, counted attempts, liabilities, economic activation or completed work.
History selection must not inherit a fixed compiled artifact-count cap.

The same boundary then needs separately qualified standard-validator startup,
preparation, recovery and upload integration; both validator roles, miner native
and EVM mutations, bootstrap and other consumed profiles require their own scope.
No format, fixture certificate, metadata comparison or successful inspection closes
MG-04/RT-04. No live mainnet action or Safe public route is part of this increment.
