# Offline successor Safe review

`bootstrap-chain contract-successor-safe-review` joins the completed
[signed local preparation](BOOTSTRAP-SUCCESSOR-PREPARATION.md) to exact published
Safe release bytes and the [pure Safe digest calculation](SAFE-EXECUTION-EVIDENCE.md).
It produces a sealed, unsigned execution review for the one remaining evidence
anchor. A separate [execution custody and owner](BOOTSTRAP-SUCCESSOR-EXECUTION.md)
now retains independently approved signatures, cumulative attempts and separate
nonce claims. Its concrete canonical adapter awaits independent qualification;
this review command retains its existing offline scope.

```sh
sn-mainnet bootstrap-chain contract-successor-safe-review \
  --config /private/chain.json --run-dir /private/custody \
  --accept-plan-hash sha256:ORIGINAL_V3_DIGEST \
  --request /private/successor-request.json \
  --safe-request /private/safe-review-request.json
```

The new private, regular request file is strictly decoded and limited to 16 KiB.
It uses schema `urnetwork-mainnet-successor-safe-review-request-v1`:

| Field | Meaning |
| --- | --- |
| `preparation_plan_hash` | Exact independently signed local preparation plan hash. |
| `preparation_record_hash` | Exact immutable, completed preparation record seal. |
| `version`, `variant` | Explicit published version `1.4.1` or `1.5.0`, and singleton `Safe` or `SafeL2`. |
| `archive` | `path` and `sha256` for the exact selected published archive from the release catalog. |
| `relayer_gas` | Proposed outer gas limit, 21,000 through 100,000,000. This does not prove sufficient execution gas. |
| `relayer_fee_cap_wei`, `relayer_tip_cap_wei` | Canonical uint256 fee limits; fee is positive and tip cannot exceed fee. |
| `start_native_number`, `start_native_hash` | Declared native review point at or after every retained original receipt; its canonicality remains unverified. |
| `valid_through_native` | Greater than the start, at most 7,200 native blocks later. |

The review reads the original accepted v3 configuration and pinned inputs, holds
the five existing shared preparation locks, and reconstructs all eight completed
contract actions. It then takes a shared lock on the signed physical custody
directory and reads the fixed successor claim. It validates the record seal,
independent preparation signature and exact completion marker. Missing files,
partial completion, any staged claimant, changed approval, another publication
owner or a replaced physical directory refuse the review. This reader cannot
finish publication or repair a claim; use the original preparation resume command.
Every successful or refused review preserves retained custody bytes.

Both new input paths are checked against original inputs, journals, markers,
validator custody and the preparation staging namespace. The release archive
is a public local input; the review request and preparation custody stay private.
The full selected archive, source/compiler inputs, singleton/proxy code, ABI
and storage layout are authenticated using the same verifier as
[`safe-release-verify`](SAFE-RELEASE-VERIFY.md). No network or compiler is invoked.

The output schema is `urnetwork-mainnet-successor-safe-review-v1`, with status
`offline-review-authority-unresolved`. It retains the complete signed preparation,
its original eight action/receipt seals, release provenance and exact request
reference. The Safe, relayer and their separate nonces come from the retained
preparation request. The inner target and calldata are exactly the original
coordinator's `fixValidatorEvidence` call for the retained evidence CREATE address.
The command compares both CREATE receipt addresses and reconstructs the calldata.

The inner Safe transaction is a zero-value `CALL`, with `safeTxGas`, `baseGas`,
`gasPrice`, gas token and refund receiver all zero. Those fields select the
published releases' requirement that the inner operation succeed. The exact
mainnet chain ID, original owner address and intended Safe nonce enter the
EIP-712 domain/struct hashes, digest and 66-byte preimage. These are mathematical
review results; they do not authorize signing or prove any current Safe account.

The outer review retains the intended relayer sender/nonce, Safe target,
zero value and proposed fees. Its maximum liability is gas limit times fee cap.
The cumulative calculation adds **both** completed original maximum-envelope
reservations and any unexecuted ninth reservation before this proposed liability,
and refuses uint256 overflow or the preparation's additive lifetime ceiling.
Original attempt counts, remaining proposed attempts and lifetime ceilings remain
unchanged. Unreserved headroom is a calculation, not executable or allocated budget.
Safe signatures are absent, so outer calldata remains incomplete; the command
emits no empty-signature transaction, placeholder signature or outer signing hash.

The declared native window is retained in the complete review seal. It is **not**
part of the Safe EIP-712 digest and cannot expire a Safe signature. Changing only
that window or the outer nonce changes the review seal without changing the inner
digest. A later executor needs independently approved window enforcement and
lifetime custody for signatures that could remain valid after the review window.

Only local preparation approval/completion, static release verification and
digest computation become true. Execution approval, current chain verification,
Safe authority, original reservation reconciliation, global signing custody,
signing authorization, execution, network effects, installation and activation
remain false. A complete review exits 3 because these prerequisites remain open;
changed preparation pins also exit 3 but emit no result. Exit 1 means unresolved
custody, artifact, calculation, cancellation or output; exit 2 means invalid
flags or request grammar. There are no RPC, signature import, signer, approval,
apply, resume, submission or service flags.

Canonical eight-receipt adoption, selected Safe authority/state, independently
reviewed build provenance, exact owner signatures and outer envelope, global
Safe/relayer custody, funding, execution-time enforcement and durable cumulative
send accounting remain required. Completion must authenticate canonical Safe
inner success and the one-shot coordinator/evidence domain binding. This review
does not grant any of those authorities. Its deterministic tests include actual
published Safe bytecode and a full public v3 eight-action fixture. The
[scoped independent qualification](evidence/bootstrap-successor-safe-review-qualification-20260929.md)
on frozen `d0207448` passes nine focused and four adjacent roots in both normal
and race modes, with fourteen intended causal failures. It does not establish
full-package or later composed-release coverage.
