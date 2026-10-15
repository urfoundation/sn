# Signed owner-recycle successor admission

This is a read-only admission and approval-custody slice. It authenticates a
separately approved mainnet successor, an exact finalized runtime, and that
runtime's registered owner destinations and explicit Recycle mode. It does not
activate the successor, submit weights, migrate policy history, or demonstrate a
10% native outcome. Both `ActivationReady` and `NativeOutcomeVerified` remain
false. The [pure planner](OWNER-RECYCLE-PLANNER.md) remains non-executable.

The [measured decision capsule](OWNER-RECYCLE-MEASUREMENT.md) now carries these
exact retained bytes and census through full V2 provider replay into a distinct
blocked unsigned intent. It leaves every production signing/submission gate and
the original V1/V2 history unchanged.

## Independent authority and durable custody

`ReleaseConfig.owner_recycle_approval` contains an absolute content-addressed
`approval` file reference and an independently configured Ed25519 `signer` public
key (canonical `0x` plus 64 lowercase hex digits). There is no default production
key, embedded document-selected trust anchor, signing command, or key generation.
The production deployment owner must approve and pin this public key through its
configuration-distribution process. The code authenticates that selected key; it
does not establish the human's organizational authority or substitute for native
subnet-owner/coordinator governance. Tests generate synthetic keys only.

`OwnerRecycleApproval.SigningMessage()` produces the external signer's 32-byte
message: SHA256 of `urnetwork-owner-recycle-approval-signature-v1\n` followed by
Go's canonical JSON encoding of the approval body. The envelope is the canonical
JSON encoding of `OwnerRecycleApprovalEnvelope` followed by exactly one newline.
Its signature is 128 lowercase hex digits, without `0x`. Unknown fields,
duplicate keys, trailing documents, noncanonical bytes and oversized documents
are refused. The retained envelope is limited to 64 KiB.

The signed body binds:

- The full resolved `ReleaseConfig` hash, including the selected signer, parent
  policy, deployment/validator/contracts, routes, masks, custody paths and evidence
  settings. Only the self-referential envelope file reference is cleared before
  hashing; `OwnerRecycleConfigHash` computes the exact domain-separated value.
- The unchanged mainnet parent-policy hash and later successor policy id/effective
  policy epoch. This is a separate proposal, not a rewritten `protocol.Policy`.
- Independently approved mainnet genesis, native chain name, EVM chain 964, netuid
  25, complete runtime version, code/metadata hashes, and the exact reviewed source
  commit `67dcf7f791dc495064c293f080a0702cb433e51e`.
- `RuntimeReviewHash`, the digest of the external source-to-Wasm review record.
  The signature attests this reference; this code does not reproduce a runtime
  build or read/validate that review document. Its original bytes must be retained
  independently by the approver. A nonzero digest is not itself build proof.
- Validator hotkey, subnet-owner coldkey and the exact sorted recognized owner
  hotkey set; a declared first native epoch; a finite native-block observation
  window; and explicit UID/owned-hotkey census bounds.

The public API sequence is `LoadOwnerRecycleAdmissionConfig`, independent
approval/signature selection, `RetainOwnerRecycleApproval`, then
`ObserveOwnerRecycleAdmission` (also exposed on `ReleaseSteerer`). Load and hash
the resolved configuration before external signing; normalization is part of the
signed identity. The preparation config may leave the envelope reference empty
while pinning the independent signer; retention and observation reject that draft.
After external signing, fill its exact path/byte-count/SHA256 reference, then reload
and authenticate it. The loader bounds YAML to 2 MiB and grants only observation
admission. `LoadReleaseConfig` retains its existing production requirements.
The observer consumes an explicitly supplied connection whose URL must exactly
match an approved native route; it has no public-RPC fallback.

Retention verifies the exact source bytes and signature before mutation and uses
the existing descriptor-owned immutable writer to install
`StateDir/owner-recycle-approved-successor.json`. File and directory fsync,
physical private paths, no replacement, and exact retry checks come from that
custody owner. Restart reads the retained bytes even if the original source file
is removed. Neither this API nor a new signed envelope can overwrite the retained
approval. Key rotation, revocation, successor replacement and storage rollback
protection require a separate reviewed history transition; there is no silent
rotation, deletion/reinstall workflow, or claim that local files resist an
administrator rolling back the filesystem.

Testnet genesis, provisional runtime compatibility/deferral, history adoption,
previous-policy and source-role predecessor inputs are refused. Testnet history
does not acquire mainnet authority. Existing original policy, measurement,
intent, activation and archive bytes are neither rewritten nor adopted.

## Finalized observation and economic limits

All runtime/storage reads use one finalized native hash under a two-minute caller
deadline, with canonical-height checks before and after the census. The same
connection supplies the approved native name/genesis and EVM id. There is no EVM
transaction/receipt in this slice and no EVM-height-to-native-finality inference.
RPC finality remains an assertion of the independently selected node, not a
light-client storage proof. The reader never changes a connection's signing view.

Exact metadata14 authentication is followed by consumed-interface checks for map
hashers, scalar/account/vector layouts, query semantics, and the Burn=0/Recycle=1
enum. The adapter reads `SubnetworkN`, every UID's forward and reverse mapping,
registration block, `SubnetOwner`, bounded `OwnedHotkeys`, optional
`SubnetOwnerHotkey`, explicit `RecycleOrBurn`, mechanism count, native epoch,
minimum weights and stored maximum weight limit. Missing required rows,
duplicates, future registrations, owner drift, multiple mechanisms, expired
observation windows and late first epochs fail closed. No partial observation
escapes a canceled or failed census.

Registered owner hotkeys are ordered by registration recency and UID as the
pinned `get_owner_hotkeys` source specifies; the registered explicit owner is
prepended only when absent from that list. Its complete recognized set must equal
the signed set. The self hotkey cannot be an approved destination. This is owner
recognition only: active permits, stake, validator independence, operator health,
controlled-recipient masks and provider measurements remain unproved.

The current signed cap is enforced independently. With cap `32768/65535`, a 90%
proposal needs at least two usable owner destinations. Observation does not
relax the cap or register substitutes. The pinned source's
`utils/misc.rs::get_max_weight_limit` returns `u16::MAX` rather than reading the
`MaxWeightsLimit` storage item; both values and the separate signed cap are
reported. A stored limit alone is not evidence of native enforcement.

Owned-hotkey storage is bounded before decoding by the approved count (at most
65,536); compact lengths must be minimal and exact. Full UID census uses the
source's u16 cardinality and signed tighter bound. Decoded/retained responses are
bounded here; the underlying RPC transport still owns its envelope-byte limit.
Runtime metadata/code bounds remain those of the existing artifact authenticator.

The first native epoch is an approved proposed boundary, not evidence that prior
emissions drained there. Neither provider-only parent weights nor owner-directed
proposals prove final Yuma incentives, actual recycling, `PendingServerEmission`
drain, zero-incentive behavior, runtime-derived tolerance, or a 10%/90% outcome.

## Production boundary and next implementation

`ownerRecycleProductionBoundary` refuses any mainnet policy, EVM chain 964, or
selected owner-recycle approval at these actual paths:

| Entry | Behavior |
| --- | --- |
| `NewReleaseSteerer`, `newReleaseSteererV2` | Refuse before acquiring state or a signer. |
| `ReleaseSteerer.SubmitOnce`, `submitOnceV2` | Refuse before measurement/RPC work. |
| `submitPreparedNativeRuntimeContext` | Refuse before the last shared fresh-send boundary. |
| `reconcilePending`, `reconcilePendingV2` | Preserve original authenticated receipt lookup; if absent, refuse before replay, new nonce use or broadcast. |

These gates leave the existing testnet path unchanged. They do not introduce a
mainnet historical-runtime reader: original receipt recovery still requires the
existing original runtime and intent authority. The observer's exact mainnet pin
cannot authorize historical source relabeling or producer startup.

The next coherent implementation must carry the same approved successor and
finalized decision through all these owners before removing any fence:

1. **Runtime and config admission:** `runtime_identity.go` production/historical
   validators and authenticators, plus signing/prepared checks in
   `provisional_runtime_compatibility.go`, need independent mainnet runtime
   authority with exact original artifact retention. Do not reuse the testnet
   provisional profile or relabel a runtime as release467.
2. **Decision and measurement:** `release_measurement.go` identity checks,
   `assembleReleaseMeasurement`, seal/verify; compact
   `release_measurement_v2.go::{verifyOwnedReleaseMeasurementV2,
   VerifyReleaseMeasurementIntentV2,VerifyReleaseMeasurementLineageV2}`; and
   `release_submission_replay_v2.go` must bind approval/proposal/census hashes,
   provider artifacts, independent-validator admission and controlled masks to
   the exact quantized row. Replacing scores after verification is invalid.
3. **Signatures and intents:** legacy/compact measurement envelopes, `SubmitOnce`,
   `submitOnceV2`, both pending reconcilers and application checks must retain the
   same decision and original signed extrinsic. `IntentStore`/compact intent
   custody need versioned successor references and a proved drained transition.
   Existing pending parent intents must reconcile under their original policy.
4. **History and archive:** activation/config history, source replay and
   `release_archive_decision_v2.go::{ObserveSources,ReplayDecisions,decisionOptions}`
   must replay both sides with original artifacts, policy, signatures and exact
   boundary. The fixed approval record needs an explicit append-only transition
   owner before rotation. Coordinator policy activation, operator/client-key
   domains and signed artifact publication must agree with that transition.
5. **Outcome qualification:** exact native interval accounting must prove drained
   pre-activation emissions and observe actual provider incentive, owner recycle,
   rounding and zero-incentive behavior under the admitted independent validators.
   Approval and weight percentages cannot close this gate.

No live mainnet pins, approval key, owner census, approval signature, source-build
attestation, registration, mode change, signing or submission were obtained by
this implementation. The deterministic suite uses generated synthetic authority
and scripted clients only.
