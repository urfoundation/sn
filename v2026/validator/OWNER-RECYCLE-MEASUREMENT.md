# Owner-recycle measured decision capsule

This signer-free slice binds the signed successor and native owner census to an
actual compact provider measurement, then reconstructs an exact 10/90 proposed
row and its quantized values. Its only status is `verified_proposal_blocked`.
`activation_ready` and `native_outcome_verified` are always false. Neither the
capsule nor its separate unsigned intent is accepted by the production intent
store or a native signing/submission method.

## Concrete API path

1. Load and retain the independently selected approval using
   [the admission APIs](OWNER-RECYCLE-ADMISSION.md).
2. Pass the exact independently observed `ReleaseMeasurementV2Decision` to
   `ObserveOwnerRecycleMeasurementAuthority`. It rereads the retained signed
   approval, detaches the approved configuration, and invokes the actual native
   owner-census reader at that decision's native hash. Callers cannot manufacture
   this authority from exported observation booleans.
3. `SealOwnerRecycleMeasurement` consumes original canonical V2 provider bytes
   and their independently authenticated `ReleaseMeasurementV2Options`. The
   existing V2 verifier replays every signed attempt/proof stream and reconstructs
   provider head/pool scores, deposits and self/controlled masks. The signed
   configuration's operator census must match the replay's complete census.
4. `ReplayOwnerRecycleMeasurement` decodes the canonical capsule, compares its
   exact approval/census bytes against independently obtained authority, and
   repeats the full provider replay. Every declared rational and integer value
   must equal the newly reconstructed row.
5. `VerifyOwnerRecycleDecisionIntent` compares the entire separate unsigned
   intent against that replay, including its original decision, all content
   hashes, row, blocked status and unresolved gates. A hash match alone does not
   substitute for proof replay.

Between steps 2 and 3, `ObserveOwnerRecycleMeasurementOperators` can upgrade
that exact private authority with actual decision-time coordinator observations.
It requires an EVM connection on a route already bound by the signed config,
checks chain 964 and the approved native genesis over that transport, then
replays the complete provider measurement before reading contract state. Every
coordinator/metagraph call selects the decision's EVM hash with canonical
EIP-1898 semantics. It verifies the complete operator registry, active versions,
unique pool UID/hotkeys, every provider binding, exact uint256 deposit and
conviction values, policy/cadence and the previous source window. Positive or
priced-mismatch deposit audits must name the actual source root, artifact hash,
historical root signer/committer, commit block and source boundary hashes.

The reader compares the full EVM UID/hotkey census with the independently
observed native census, rechecks source boundaries, and finally rereads network
identity and the exact decision hash. It does not infer a native/EVM block
mapping from equal numbers. Typed transient RPC failures use the existing
bounded retry owner under a two-minute overall context; cancellation, malformed
state and changed canonical hashes cannot publish partial authority. A retry
must keep the original approved decision and create fresh proof replay scratch.

The authority currently admits only the explicitly approved first native epoch,
within the signed observation block window, at or after the proposal's effective
settlement epoch. It is a single-decision evidence owner, not authority for a
continuous mainnet service. Advancing through later epochs needs the still-missing
activation/history transition.

## Retained meaning and trust

The new `urnetwork-owner-recycle-measurement-v1` capsule contains exact original
approval-envelope bytes, canonical census bytes, exact original V2 measurement
bytes and the reconstructed row. Byte strings use JSON base64 to preserve every
byte, including final newlines. The new
`urnetwork-owner-recycle-decision-intent-v1` intent binds their content hashes.
Existing V1/V2 schemas, policy hashes, signatures and historical files are not
rewritten or reinterpreted. Old measurement readers refuse the new capsule.

The operator-observed variant uses the distinct
`urnetwork-owner-recycle-measurement-v2` and
`urnetwork-owner-recycle-decision-intent-v2` schemas. Its canonical
`operator_evidence` bytes retain both selected decision hashes, source window,
policy, current/source operator versions, commitments and decimal uint256 facts.
The intent includes their content hash. This evidence is bound to the exact
original provider measurement bytes: a newly hashed candidate cannot substitute
another transcript, replace an observed fact or downgrade the schema. Replay
requires a newly obtained independent operator authority and repeats provider
proof replay. The prior v1 owner remains usable for its original v1 evidence;
it cannot verify a v2 capsule. Empty optional fields preserve v1 bytes exactly.

`ObserveOwnerRecycleAdmissionAt` supports restart replay at an explicit original
hash after the latest finalized head advances. It requires the requested height
to be at or below finalized and checks `chain_getBlockHash(height) == requested`
both before and after the runtime/storage census. This is an **approved-RPC
canonical/finality assertion**, not an independent ancestry or light-client
storage proof. The exact original runtime artifact and approval remain required;
newer runtime numbers confer no historical authority. Restart can use the durable
approval even after its originally supplied source file is removed.

V2 replay options retain their existing trust contract: activation keys, original
coordinator bindings, deposit observations and history must be authenticated
independently by their callers. This capsule does not upgrade supplied options
into chain proofs. The optional operator observer replaces current/source
coordinator claims with exact approved-RPC observations and compares the original
measurement against them; it does not prove historical HTTP availability,
client-key custody or payout artifact bytes/signatures. Its EVM canonical and
finality assertions retain the approved-RPC trust boundary, not an independent
light-client proof. The independently approved source-review digest is still an
attested reference, not a reproduced source-to-Wasm build.

## Row derivation and remaining gates

All observed pool hotkeys and live provider UID/hotkey mappings must agree with
the exact native census and remain separate from recognized owners, including
zero-score or masked pools. The existing measured provider row retains the
parent's head/pool theta and masks, then receives exactly one tenth of the proposed
row. The remaining nine tenths are divided equally among unmasked recognized
owner destinations. Shared production quantization rejects lost recipients and
signed-cap violations; native minimum weight count is checked too. No cap repair,
zero-provider fallback, reserve credit or final-incentive claim is inferred.

The following remain explicit blockers:

- Decision-time active-validator stake, permits, liveness and independence.
- Operator API health, authenticated client-key/payout custody and complete
  native/EVM history. The optional v2 capsule resolves decision-time registry,
  bindings, amounts and the source commitment window only.
- A drained activation boundary; original-parent pending-intent recovery;
  successor hotkey envelopes, transaction custody and archive/history transition.
- Final Yuma incentives, actual owner recycling, native rounding and observed
  10%/90% outcome within the native miner allocation.

The existing mainnet producer, prepared-send and pending-rebroadcast fences remain
unchanged. No caller-supplied health list or verified flag can clear those gates.
Canonical capsule/control bytes must fit the caller's existing V2 artifact and
control allowances; there is no implicit capacity increase. Cancellation and
failed replay return no partial capsule or intent. Test authority and RPC storage
are synthetic; no live mainnet identity, signing key or transaction was used.

The [operator-observation qualification record](/mnt/data/sn-testnet/evidence/mainnet-owner-recycle-readiness-20260927/RESULT.md)
retains exact source pins, normal/race selectors, route/state/tampering controls,
vet and the Darwin arm64 compile check: 138 affected normal and 138 race tests
passed. Final naming-only cleanup preserved the wire; its production happy
path and final package vet passed separately. Native validator eligibility is still a
separate capability: this slice does not relabel the stake reader's reviewed old
layouts or explicitly testnet-only provisional admission as approved mainnet,
and it does not invent independent validator identities or liveness history.
