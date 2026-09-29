# Historical runtime views for validator source receipts

A signed source can be included in the block that installs a compatible runtime
upgrade. Its signature belongs to the original runtime, while the block's final
state reports the successor. The previous production recovery and semantic
archive readers rebound one chain view to that post-state before calling
`VerifyFinalizedSourceContext`. Its `ValidatePreparedSource` check then rejected
the unchanged signature because the two spec versions differed.

Production pending recovery, semantic source verification and native capture
now keep three independently authenticated views:

- The original preparation block selects the complete original signed config
  and exact signing artifact. Original extrinsic and sidecar bytes stay fixed.
- The inclusion header's parent selects execution metadata. The original
  signature must still validate under this execution runtime; an old signature
  cannot be relabeled for a later execution spec.
- The inclusion block selects the approved post-state artifact and commitment
  storage decoder. Its spec may differ from the execution spec.

The existing signed finite runtime windows and producer capability profile gate
each artifact. Complete canonical headers bind the parent and heights; the
receipt reader verifies the committed body, dispatch and source events before
reading the exact commitment slot. Generic historical binding cannot acquire
current signing authority. Missing approvals, changed consumed interfaces,
unavailable archive state and conflicting canonical evidence remain errors.
Native capture explicitly retains the parent's metadata even when cached.

The receipt path accepts a complete `RuntimeEnvironmentUpdated` digest without
asking the older SDK to decode it again for commitment readback. Other native
readers still use that SDK for their current-head header; the digest regression
observes the upgrade receipt from the following finalized block. This change
does not qualify all current-head consumers or automatic runtime approval.

Deterministic local regressions are selected by
`^TestProductionSourceReceipt`. They cover ordinary and digest-bearing upgrade
receipts, separated proof ownership, changed execution spec, independently
approved incompatible storage, missing approvals, header/code substitution,
archive failure, cancellation, dispatch failure, commitment mismatch, native
capture and durable pending recovery. Behavioral qualification is recorded
separately; source implementation and compilation do not establish mainnet
deployment or upgrade acceptance.
