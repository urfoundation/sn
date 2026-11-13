# Offline successor Safe review qualification

Qualified source is `d0207448345360fc98343abb0b28af1b7547cd4f`, tree
`9140b6054d9e6a53093038bc3ade92d734660ce9`, based on
`4a855364f440605bf68bb4cdefff19b9123391ad`. Its author worktree is
`/home/by/urnetwork/temp/contract-successor-safe-offline-20260929/sn`.
Sol independently tested the clean detached copy at
`/mnt/data/sn-testnet/worktrees/sn-contract-successor-safe-sol-20260929`.
The documentation child changes no Go source or module resolution.

All nine focused and four adjacent roots pass in both normal and race modes.
Each of the four streams contains the exact expected root census, no failed or
skipped roots, and PASS/package-ok terminals. Sol reports exit 0 for all four
processes. All seven isolated causal controls fail at the intended named assertion
in both modes, with root/package FAIL and process exit 1: fourteen intended
causal executions. Astra independently checked every verbose raw log, the exact
root census, assertions and reported process results; all seven saved patches;
the 43 sealed evidence files; and both test worktrees' clean status and exact
commit/tree. Author test compilation and vet also pass; the author ran no
behavioral tests. This qualifies only the selected roots on the pinned graph.

Evidence is retained at
`/mnt/data/sn-testnet/qualification/mg08-successor-safe-sol-20260929`:

| Positive stream | Roots | Package duration | SHA-256 |
| --- | --- | --- | --- |
| `focused-normal.log` | 9/9 | 54.843s | `9934b6feb400b00a3657ffb8d88b15245cb529fd8d3a6feec1e991dfb2870632` |
| `focused-race.log` | 9/9 | 322.480s | `5c07a9a32860c823bfc6b2c6a791defc4bde0cff67b14e35fb781947261f5349` |
| `adjacent-normal.log` | 4/4 | 46.852s | `d093d4b57280ba06186722988eaa67564590bc27921de3f8a3dadb2d6648ae19` |
| `adjacent-race.log` | 4/4 | 277.882s | `755b2f1797467aeb5a53a8ac74ddcd0ed49e7527d414b8897076cbc32eaddcb6` |

The final sealed receipts are:

| Artifact | SHA-256 |
| --- | --- |
| `SOL-RESULT.md` | `a7b9f324a474e3ef4a49d88e32e9c6d719382cb4f7994c8c3b0f388f25a790b5` |
| `SOL-RESULT.json` | `3c76bbec1dec387753bccaee112b5b50e432909416b5ca151018ef85434b79df` |
| `SHA256SUMS` | `dad992084989eb5aec9131fc71a50eb9a17a05eacc7692112ff6c8d635b7143b` |

`sha256sum --check SHA256SUMS` verifies all 43 retained files. The JSON receipt
records each exact command, source/dependency fence, log hash and process result.
These raw logs are verbose Go test output, not JSON event streams. All test
commands use `-count=1 -p 1 -parallel 1 -v ./mainnet`; race runs add `-race`.
The focused selector is `^TestBootstrapSuccessor(SafeReview|PreparationReader)`;
the adjacent selector is the exact union of the four adjacent roots below.

Focused roots:

- `TestBootstrapSuccessorPreparationReaderRequiresCompleteClaim`
- `TestBootstrapSuccessorPreparationReaderFencesPublication`
- `TestBootstrapSuccessorPreparationReaderRejectsUnsafeOrReplacedCustody`
- `TestBootstrapSuccessorSafeReviewCommandReconstructsPreparedV3Custody`
- `TestBootstrapSuccessorSafeReviewMatchesPublishedAnchorDigest`
- `TestBootstrapSuccessorSafeReviewConservesOriginalLiabilities`
- `TestBootstrapSuccessorSafeReviewSeparatesNoncesAndWindow`
- `TestBootstrapSuccessorSafeReviewRejectsReboundAnchor`
- `TestBootstrapSuccessorSafeReviewRequestBounds`

Adjacent roots:

- `TestBootstrapSuccessorPreparationCommandRetainsActualV3Prefix`
- `TestBootstrapSuccessorPreparationSeparatesOriginalInputsAndOwners`
- `TestSafeExecutionDigestMatchesPinnedProxyAndSingleton`
- `TestSafeReleaseCommandVerifiesPinnedVersionsAndVariants`

Each control changes only its saved one-file patch in the isolated control
checkout. Both modes reach the following assertion, with the intended root and
package FAIL and process exit 1. Astra independently checked patch applicability
against the frozen source without applying changes to the positive checkout.

| Control | Weakened condition | Intended assertion in both modes |
| --- | --- | --- |
| `marker` | Exact completed marker equality | `read-only inspection accepted or repaired pending marker` |
| `lock` | Shared directory lock acquisition | `reader escaped active publication <nil>` |
| `signature` | Independent preparation approval and record validation | `read-only inspection accepted or repaired resealed forged approval` |
| `budget` | Additive lifetime ceiling comparison | `review spent the retained completed or unexecuted envelope reservation <nil>` |
| `nonce` | Use relayer nonce as Safe nonce | `successor anchor digest differs from published 1.4.1/Safe` |
| `archive` | Exact selected archive pin | `review admitted unbounded request archive pin` |
| `anchor` | Exact retained anchor calldata | `review admitted rebound anchor calldata` |

The budget control exercises the cumulative ceiling; its uint256 bound remains
present. The marker and signature controls exercise separate failed claims in
the same complete-claim root. Neither represents a setup failure. Both test
worktrees are restored clean at the qualified commit/tree after all executions.

The selected dependency graph is pinned by:

| File | SHA-256 |
| --- | --- |
| `go.mod` | `2a948e40658bb403c440ea649140b5ffae53d8b338b0dfc61c90625c708dbc5c` |
| `go.sum` | `2a8d74108e1c331b4b9ebdd8595c02f4b8a98e76f51fedf4c7498c7b41d974ca` |
| `modules.txt` | `19db0607a41328b8dc09c13292e40a0ee7024650c80ffc3e85e9a870ae225f9b` |

Sol's `GOPROXY=off go mod verify` returns `all modules verified`, exit 0.
Astra independently reproduces the exact `GOPROXY=off go list -m all` bytes and
module-file hashes. Connect resolves to `b163f9dd9ac3`, SDK to `516521fb16da`.
All six local repositories recorded in the sealed JSON are independently
checked clean at their recorded heads and trees: glog `892ade4a`, goidenticons
`325750b3`, proxy `6204ae7d`, server `80c0e1b7`, userwireguard `85fb1ca4`, warp
`7498864c`. The module listing determines which are selected; the repository
fence does not imply all six are imported by these roots.

The new full-v3 command fixture passes in 49.42s normal and 285.64s race; the
existing preparation command passes in 44.11s and 252.85s. Both exercise genuine
synthetic local eight-action execution before offline review, using the existing
finite 60-second success-path send bound. No production or lost-reply timeout is
changed. These are bounded selected-root results, not full-package qualification
or evidence that a live runtime uses this source.

The command reconstructs original accepted v3 custody before borrowing the fixed
completed preparation under shared directory ownership. It verifies the original
independent preparation signature, immutable record, exact complete marker and
signed physical root; it never runs recovery or repairs a partial claim. Both
new inputs are excluded from original and validator custody namespaces. The
selected published archive supplies source/compiler inputs, singleton/proxy code,
ABI and storage layout before the pure Safe digest is computed.

The retained proxy and evidence CREATE identities determine the exact
`fixValidatorEvidence` CALL. The Safe transaction has zero value, reimbursement
and Safe gas fields, with the original intended Safe nonce and mainnet EIP-712
domain. Actual published proxy bytecode independently checks the digest across
both supported releases and variants. The relayer's outer nonce remains separate.
Completed maximum-envelope liabilities and any unexecuted ninth reservation are
added before the proposed outer gas/fee maximum. Original attempt counters and
additive lifetime ceilings remain unchanged.

The complete review is unsigned and exits 3 with unresolved authority. No owner
signature, complete outer calldata, transaction, nonce reservation or executable
budget is created. The declared native review window is outside the Safe digest
and cannot expire a signature. Canonical original receipt adoption, current
Safe/evidence authority, independent compiler rebuild/release review, signature
lifetime/window enforcement, global Safe/relayer custody, exact approved outer
envelope, funding and durable cumulative execution accounting remain required.
Installation requires canonical inner success and the one-shot evidence binding.
No live RPC, mainnet signing, transaction, installation or activation occurred.

This increment does not close MG-08 or expand the previously reported broader
package, SDK or composed-release coverage. The executable successor adoption
owner and evidence-anchor executor remain unimplemented.
