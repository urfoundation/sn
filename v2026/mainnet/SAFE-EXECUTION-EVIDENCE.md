# Pure Safe execution evidence

`safe_execution_evidence.go` adds bounded internal calculations for the published
Safe and SafeL2 1.4.1 and 1.5.0 profiles. This layer has no command, network client,
signer, selected live account, custody owner or broadcast path. Its
[scoped independent qualification](evidence/safe-execution-evidence-qualification-20260929.md)
passes thirteen focused and four adjacent roots in normal and race modes,
with thirteen causal controls reaching their intended assertion in both modes.

The constructor authenticates exact singleton artifact bytes against the immutable
[release catalog](SAFE-RELEASE-VERIFY.md), then owns an immutable parsed ABI. The
existing release verifier separately authenticates the complete published archive,
proxy, source, compiler inputs and storage layout. Neither operation independently
rebuilds the release or verifies current account code or storage.

The calculations provide:

- The EIP-712 domain, struct and transaction hashes and 66-byte preimage. Chain ID,
  proxy address and every SafeTx field, including nonce, contribute to the digest.
- Canonical `execTransaction` calldata with supplied signature bytes. The nonce is
  absent from calldata because Safe reads its current storage nonce on execution.
- Ordered signer recovery for direct and personal-sign ECDSA, including high-s
  signatures accepted by these Safe releases. Recovery does not prove membership
  in a current owner set or the threshold represented by the supplied prefix count.
- Bounded contract-signature offsets, dynamic bytes and exact required callback.
  Version 1.4.1 uses `isValidSignature(bytes,bytes)` over the preimage and magic
  `0x20c13b0b`; version 1.5.0 uses `isValidSignature(bytes32,bytes)` over the digest
  and magic `0x1626ba7e`. Callback execution and approval state stay unresolved.
- Approved-hash signature structure, with an explicit unresolved requirement for
  the real executor or authenticated `approvedHashes` storage. The unused high
  bits of the encoded owner and unused s word retain published cast semantics.
- Interpretation of internally consistent receipt claims as Safe inner success,
  committed inner failure, or outer revert. A successful outer status requires one
  exact matching Safe outcome event; it cannot stand in for inner success.

The receipt classifier binds the declared transaction hash, Safe recipient,
nonzero block hash, log metadata and ordering, event emitter, digest and payment
shape. It rejects removed, missing, malformed, duplicate or contradictory matching
events. Other well-formed digests may occur through nested execution and do not
replace the exact matching outcome. An outer revert must retain no logs. A claimed
committed failure with both Safe gas and gas price zero is impossible under these
profiles and is rejected.

Committed inner success and failure both consume the invocation's Safe nonce.
An outer revert rolls back that increment, including failures after inner execution
caused by a guard or refund. The reported increment follows the published solc
0.7.6 uint256 wrap behavior. It describes this invocation's immediate increment;
it does not claim a canonical block's final nonce. Nested or later calls may
advance it, and delegate calls can change storage. Executable policy may impose
stricter nonce admission in a later phase.

All results keep current-chain verification, canonical inclusion, initializer-owner
binding, owner membership, current threshold, Safe authority, permission to sign,
custody, executability, evidence-anchor verification and installation completion
false. In particular, declared receipt hashes are not inclusion proofs, and an
expected outer hash is not proof that the outer transaction contained this call.
Current modules, guards and fallback paths remain outside this pure layer.

Inputs are limited to uint256 integers, the two operation values, 64 KiB calldata,
64 KiB signature bytes, a 1–32 signature prefix, 128 logs, four topics per log,
64 KiB per log and 256 KiB total log data. Safe's accepted trailing signature bytes
are preserved and included in the input hash. Returned byte slices own their data.

The tests install the exact published proxy and singleton runtime in an in-memory
geth EVM, run the published setup with synthetic owners, and compare digest,
callback, signature, event and storage behavior. They use no HTTP fixture, clock
deadline or external compiler. Both versions and variants are covered. Synthetic
signing exists only in the test fixture; production helpers consume supplied bytes.

## Retaining the eight completed actions

Full Safe authority is necessary for an executable successor and does not by itself
authorize adopting original receipts. A new independently approved executable
successor schema must reference the original bootstrap and contract approvals,
custody ID, exact eight sealed canonical receipts and postconditions. It must reconcile any
already reserved or signed ninth action and keep cumulative attempt and lifetime
spend exposure. Budget only unfinished work plus retry margin; never replay the
completed prefix to satisfy a new plan's action count.

The separately qualified [signed local preparation](BOOTSTRAP-SUCCESSOR-PREPARATION.md)
now authenticates those retained local records and additive proposed floors under
one original-root claim. It grants no executable allowance or Safe authority;
canonical historical reauthentication and the executable transition remain open.

An executable successor must bind the actual proxy to the recorded
`initializerOwner`. A different new Safe requires separately authorized ownership
migration and proof of that migration. It also needs exact current singleton/code,
owners and 2-of-3 threshold, all module/guard/fallback paths, chain finality and
pending Safe and relayer nonces. One durable owner must hold both the original and
successor liabilities. The anchor action then needs the approved inner call/value,
digest and signatures, canonical outer receipt, committed Safe inner success,
coordinator binding event/getter and the immutable evidence domain. These helpers
provide none of that approval or custody and do not make the unsigned successor
proposal executable.
