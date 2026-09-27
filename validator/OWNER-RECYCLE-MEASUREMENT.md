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
into chain proofs. The independently approved source-review digest is still an
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
- Operator health and complete authenticated native/EVM decision history.
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
