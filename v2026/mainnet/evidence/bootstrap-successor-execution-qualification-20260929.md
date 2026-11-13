# Successor execution custody qualification

**Scoped independent qualification complete.** Twenty-one focused and six
adjacent roots pass in normal and race modes, with four package PASS/exit-zero
streams. Ten isolated causal controls reach their intended named assertions in
both modes: twenty executions with root/package FAIL and process exit one.
This qualifies conditional execution custody and the synthetic-adapter owner;
the production canonical adapter and live authority remain open.

Tested source is `75ea215859bc403121b1e722f8169dd0dbf04b0a`, tree
`0f4a854784632c24620c7a8fc82385a75326775a`, based on initial implementation
`00ec46db917d283e665dc0e6e813bcc792d87bb2` and original merge `97123f8`.
The author worktree is
`/home/by/urnetwork/sn-successor-execution-owner-20260929`.
Sol independently tested the clean detached positive copy at
`/mnt/data/sn-testnet/worktrees/sn-successor-execution-sol-20260929`.
This documentation child changes no Go source or module selection. Astra's
compile-only `go test -c -vet=off ./mainnet`, `go vet ./mainnet` and formatting
checks pass; Astra ran no behavioral tests.

Evidence is retained at
`/mnt/data/sn-testnet/qualification/mg08-successor-execution-sol-20260929`.
Astra independently read all four complete positive logs, compared the focused
root census to the source declarations and confirmed the exact twenty-one/six
RUN/PASS censuses, no failed/skipped roots, all PASS/package-ok terminals and
their SHA-256 hashes. Astra also read every causal patch and complete raw control
log, verified the named assertions against the source, and matched the process
exit, command, source and dependency records. Both Sol checkouts are clean at the
qualified source. The complete forty-five-file SHA-256 manifest verifies.

| Sealed evidence | SHA-256 |
| --- | --- |
| `SOL-RESULT.md` | `4b14ab2c3de179834d6f2912a283ab40c474287ce48872a146c028d288580a67` |
| `SOL-RESULT.json` | `0b37492aeb5dbd36a96c6961ef9f1f31613b1cd07d82c001d6f950eb8f6d361d` |
| `CONTROLS.json` | `633625bea642264212f8e4868787a3163b8aef7405df02aafc6fedd1d0446b22` |
| `SOURCE-FENCE.json` | `463a9a79751ba5e80227f8a717c998e4912f05bfb3244f4e2e3ea2206a0fc09f` |
| `SHA256SUMS` (45 files) | `455ab9a10c5726a37a2e7891e310f2f5f9dd62852cf8bc6ccaa489234744c0a2` |

| Positive stream | Roots | Package duration | SHA-256 |
| --- | --- | --- | --- |
| `focused-normal-75ea.log` | 21/21 | 125.232s | `dfc61a2d42e52b8d86c4768c7c7b24d4354b8d535b53313c4644bae58f0c5924` |
| `focused-race-75ea-20m.log` | 21/21 | 662.949s | `13d88086feb2f665c3661222704b90445a7fadce591c491a8ed155d39858e921` |
| `adjacent-normal-75ea.log` | 6/6 | 3.007s | `2163cda1d277adb19dd111fcbaf5c9446999d0f8ab2798406eaeb8367efb7271` |
| `adjacent-race-75ea.log` | 6/6 | 24.486s | `b1fbcca3fec70f666e534c7cc95d9dfa35a7b963faa299192a79a752e41ba1d7` |

The focused selector is `^TestBootstrapSuccessorExecution`. The complete race
retry adds an explicit `-timeout=20m` to the unchanged source and selector.
No production deadline or fixture send timeout was changed for this retry.

Focused roots:

- `TestBootstrapSuccessorExecutionCommandReconstructsAndRetainsV3Custody`
- `TestBootstrapSuccessorExecutionSeparatelyFencesGlobalNonceDomains`
- `TestBootstrapSuccessorExecutionSerializesRootAndRegistryOwners`
- `TestBootstrapSuccessorExecutionRefusesMissingReboundAndUnsafeCustody`
- `TestBootstrapSuccessorExecutionPendingOutcomeCannotBecomeAnotherSend`
- `TestBootstrapSuccessorExecutionFencesRegistryReplacement`
- `TestBootstrapSuccessorExecutionFixturesUsePrivateInputDirectories`
- `TestBootstrapSuccessorExecutionRequiresIndependentExactApproval`
- `TestBootstrapSuccessorExecutionBindsSafeSignaturesAndOuterEnvelope`
- `TestBootstrapSuccessorExecutionAdoptsExactFloorsWithoutSending`
- `TestBootstrapSuccessorExecutionRecoversInterruptedCreation`
- `TestBootstrapSuccessorExecutionRecoversCountedAttemptInterruptions`
- `TestBootstrapSuccessorExecutionRecoversInterruptedCanonicalOutcome`
- `TestBootstrapSuccessorExecutionRequiresCanonicalEightReceiptAdoption`
- `TestBootstrapSuccessorExecutionFencesCurrentAuthorityAndBothNonces`
- `TestBootstrapSuccessorExecutionRechecksAfterReservation`
- `TestBootstrapSuccessorExecutionConservesFundingAndAttemptCeilings`
- `TestBootstrapSuccessorExecutionReconcilesAmbiguousSendBeforeRetry`
- `TestBootstrapSuccessorExecutionRequiresCanonicalInnerSuccessAndBinding`
- `TestBootstrapSuccessorExecutionRetainsRevertedOuterAndLiveInnerClaim`
- `TestBootstrapSuccessorExecutionCancellationAndMissingClaimFailClosed`

Adjacent roots:

- `TestBootstrapSuccessorPreparationReaderRequiresCompleteClaim`
- `TestBootstrapSuccessorPreparationBindsIndependentDomainAndScope`
- `TestBootstrapSuccessorSafeReviewConservesOriginalLiabilities`
- `TestSafeExecutionSignaturesBindOrderBoundsAndRecovery`
- `TestSafeExecutionOutcomesFollowPinnedContractAndNonce`
- `TestSafeExecutionOutcomeRejectsForgedOrIncompleteEvents`

## Causal scope

Each mutation is confined to the separate detached control checkout
`/mnt/data/sn-testnet/worktrees/sn-successor-execution-sol-controls-20260929` at
the same source commit. All ten valid control pairs reach the named root's
intended assertion and package FAIL in the raw normal/race logs, with exit one
in the sealed process records. The control checkout is restored after each
mutation; no control enters the positive source.

Root names below have the common `TestBootstrapSuccessorExecution` prefix.
Each row identifies the concrete removed barrier and observed assertion, rather
than treating any failed package invocation as causal proof.

| Control | Named root suffix | Removed barrier / observed assertion |
| --- | --- | --- |
| `approval` | `RequiresIndependentExactApproval` | Disable independent Ed25519 verification; a signature for the preparation domain is admitted for execution. |
| `adoption` | `RequiresCanonicalEightReceiptAdoption` | Remove exact count and ordered receipt-seal comparisons; missing eighth canonical seal is admitted. |
| `safe_nonce` | `FencesCurrentAuthorityAndBothNonces` | Remove Safe nonce comparison; case 0 admits the outer nonce as the inner nonce. |
| `relayer_nonces-fixed` | `FencesCurrentAuthorityAndBothNonces` | Remove relayer confirmed/pending nonce comparisons; case 1 admits the inner nonce as the outer nonce. |
| `funding` | `ConservesFundingAndAttemptCeilings` | Remove balance-versus-liability comparison; insufficient funding reserves and sends. |
| `second_admission` | `RechecksAfterReservation` | Remove admission after durable reservation; changed native window permits a send. |
| `pending` | `ReconcilesAmbiguousSendBeforeRetry` | Remove pending-state refusal; an unresolved pending signature is sent again. |
| `receipt_binding` | `RequiresCanonicalInnerSuccessAndBinding` | Remove coordinator/evidence address comparison; outer success and inner event bypass exact evidence binding. |
| `attempt_ceiling` | `ConservesFundingAndAttemptCeilings` | Remove cumulative maximum-attempt comparison; the original eight-attempt floor admits a third added attempt. |
| `private_input` | `FixturesUsePrivateInputDirectories` | Remove fixture-parent chmod under explicit umask `0022`; exact binary input admission reproduces the private-parent failure. |

The positive authority and signature roots also exercise owner, threshold,
module, guard, fallback, proxy/singleton code, signature and exact transaction
refusals. These ten mutations do not claim a separate causal deletion for every
one of those checks, or canonical correctness of an unimplemented live adapter.

## Dependency and source custody

The toolchain is Go 1.26.6 on Linux/amd64. The unchanged `go.mod` SHA-256 is
`2a948e40658bb403c440ea649140b5ffae53d8b338b0dfc61c90625c708dbc5c` and
`go.sum` SHA-256 is
`2a8d74108e1c331b4b9ebdd8595c02f4b8a98e76f51fedf4c7498c7b41d974ca`.
The resolved `GOPROXY=off go list -m all` output hashes to
`19db0607a41328b8dc09c13292e40a0ee7024650c80ffc3e85e9a870ae225f9b`.
Connect and its SCTP replacement remain at versioned `b163f9dd9ac3`; SDK remains
at versioned `516521fb16da`. `go mod verify` reports all modules verified.
No module or Go source changes accompany this documentation child.

The six local replacement repositories remain clean and independently match
the author and Sol positive worktrees' selection:

| Repository | HEAD | Tree |
| --- | --- | --- |
| glog | `892ade4a6be396b32ea82a550f243190b5992180` | `4b3c242905aed44db5879b3d22e860697f666abe` |
| goidenticons | `325750b38314313dc5f44c880ab6f12f6c1ecb3c` | `553487079352d9544edb62850328305e97668790` |
| proxy | `6204ae7df2a9868bbb3a7b61231917a36e4f5c9f` | `730ffeba453bea95f5f23fcf4b3fbc0b45f204ce` |
| server | `80c0e1b7d9fb48ee6f929bca8158ac925b114fe0` | `e78d5078d1b5d5cc39c6167c8e94639bd4f97ea0` |
| userwireguard | `85fb1ca4086fa5dbfcda526bec7a17a894e691b9` | `e83cf65488b192388571b25b1ac85ecebc36eea0` |
| warp | `7498864c7cd3605aad3c43eabfab9008ed7f7228` | `5a54f2db5582403b7b732c8602adeabaacc1a269` |

## Preserved failed attempts and harness sizing

The [initial fixture failure](bootstrap-successor-execution-fixture-correction-20260929.md)
on `00ec46db` remains a separate twenty-root failed normal stream. Each root
stopped before its intended behavior because the new binary fixture writer
created private files in temporary directories with shared permissions. Go's
numbered temporary children inherit ambient creation permissions. The test-only
correction explicitly selects `0700` for private input parents and adds exact-byte
admission plus a deliberate `0755`-parent refusal. Production permissions remain
strict. Those failed prerequisites are not causal proof about execution custody.

The first race stream on corrected `75ea2158`, `focused-race-75ea.log`, reached
Go's default ten-minute package timer after fifteen passing roots, with zero
failed root assertions. It interrupted
`TestBootstrapSuccessorExecutionRechecksAfterReservation` about three seconds
into its fixture setup while decoding a published Safe archive. The stream
retains the timeout panic, package FAIL and 600.167s duration. Its SHA-256 is
`d581c80bb8d207121aedb905c10c405b2fc7231d11a98c3a23b23aaa0008ce0d`.
It is an incomplete harness attempt, not completed race coverage or a demonstrated
product failure.

The exact twenty-one-root retry with a bounded twenty-minute package budget
passes all roots in 662.949s. The genuine public-v3 fixture takes 46.87s normal
and 277.17s race; the thirty-boundary claim-recovery root takes 24.31s and
121.08s. Their combined cost already exceeds the default race budget once all
remaining roots are included. Size whole-package budgets from measured serial
fixture cost, isolate long roots when appropriate, and preserve every original
timer and completed result. Do not extend production send deadlines to resolve
a qualification harness timeout. The public fixture retains the previously
approved sixty-second synthetic success-path send bound.

The initial relayer-nonce causal patch replaced two mismatch operands with
`false || false`. Vet rejected that redundant expression before any root ran.
Its original patch and `control-relayer_nonces-normal.log` are retained; the log
SHA-256 is `28b2194e4dd4a7535941c3721978c9e83c34427234f02870437f00c4f7388b70`.
This is a failed control prerequisite, not a causal assertion or production
failure. Astra supplied a corrected isolated patch that deletes only the two
relayer nonce mismatch operands, preserving the independent Safe nonce gate.
The corrected patch SHA-256 is
`79b88a409092c93e267638a7abc3b075309284b0e4e88f3ac6a2b10e3ea70ccc`;
apply-check, compile-only, vet and formatting checks pass in a separate detached
author control checkout. Sol's corrected normal/race runs reach the intended
case-1 relayer assertion, named root/package FAIL and exit one. Their log hashes
are `40826ad03d65f5c7167ae0cee9a174fe29fa34328a1c47be194629dc872315c1`
and `6ac252573d2a511d39c5ffc61039b66a3921f14f84a7c1145b8f50fb9b9e7393`.
The qualified production source is unchanged by either control.

## Implemented scope and remaining gates

The [execution owner](../BOOTSTRAP-SUCCESSOR-EXECUTION.md) adds a separate
Ed25519 execution approval covering the reconstructed original eight receipts,
their exact journal/full-record and predecessor seals, the completed preparation,
the pinned Safe release, owner census, exact imported Safe signatures and signed
outer relayer transaction, original financial/attempt floors and additive caps,
and physical original/nonce-registry identities. It does not re-sign or replay
the original eight actions.

Public preview/claim/resume retain these conditional execution inputs offline.
One exclusive local owner follows the original five shared preparation locks,
claims separate Safe-inner and relayer-outer nonce domains, and publishes
immutable adoption/attempt/result intents. Same-approval recovery preserves
partial claims, original liability and counted attempts. Copied directories,
competing owners, changed or missing completed custody and unknown stages refuse
execution. A local registry is not independently enforced cross-host signer
custody, and privileged rollback of the whole filesystem remains outside it.

The internal state machine requires a canonical adapter, reconciles the exact
retained transaction before retry, retains ambiguous-send liability, reserves an
attempt before sending and repeats admission after publication. Current Safe
code, owners/threshold, modules, guards, fallback, pending operations, separate
nonces, funding, native window and evidence state are mandatory adapter checks.
Only canonical Safe inner success plus exact evidence binding can establish
installation. Outer revert retains the still-live inner signature claim; expiry
does not erase signatures, financial liability or canonical success.

**The production canonical adapter is still unimplemented.** The model tests
supply explicitly synthetic canonical responses. The full public-v3 fixture
executes and reloads genuine synthetic original eight-action custody, then checks
offline execution claim commands; it does not supply a production Safe executor.
Published Safe proxy bytecode independently supplies the model signatures' digest
oracle. These scopes do not prove live mainnet inclusion or current authority.

Remaining code must authenticate the original signed transactions and eight
canonical historical postconditions through the actual native/EVM mapping;
independently reviewed Safe compiler/release provenance and complete current
finalized/pending storage authority; every original unexecuted reservation;
signer cutover to one enforced registry; exact canonical Safe receipt logs and
coordinator/evidence binding; and one bounded submission over the approved owned
route. Only then can an online/read/submit command be wired and qualified with
real contract/proxy and interrupted native/EVM transport fixtures. Independently
proved external-send adoption, replacement transactions, fee changes and later
successor/filesystem migrations are unsupported and need separate approved
liability-preserving transitions.

Operator-approved mainnet genesis/runtime, actual Safe and relayer custody,
independent build approval, funding and live signatures remain absent. No live
RPC, mainnet signing, transaction, installation, validator activation or native
10/90 acceptance occurred. This scope does not close MG-08 or expand prior
broader package, SDK or composed-release coverage.
