# Miner claim EVM finality and original signed custody

The claim correction is frozen at SN
`6dcb94a18bec7f7600f73c1657e2d885eb5f87aa`, tree
`063e54d35e4755d0b25d6f7dddcbb6503e3702cd`, based on main `3217e734`.
Its author graph uses server `6c39d3079700c779e026a8e0244775abb7e458b4`,
Connect `e1b5d77b5029`, SDK `5d37be3876e5`, and the checked-in SN forks.
There is no module version or queue schema change.

## Causal defect and correction

The original claim daemon used the native finalized header number to select
EVM entitlement, claimed-state, nonce and replay-preflight reads. It also compared
that native number with an EVM receipt number. Independent and author synthetic
controls reproduce native finalized 100 admitting canonical EVM receipt 90 while
EVM finality is only 70. The same error selects EVM state at 100 and can falsely
mark the original signed nonce consumed.

The corrected claim owner opens the endpoint's explicit EVM finalized identity,
checks its exact canonical number/hash, and uses only that EVM number for state
reads. Before returning claimed state, reporting a stable payout-root mismatch,
disposing a consumed nonce or replaying the saved raw bytes, it rereads the
actual EVM finalized tag and closes the original canonical identity. Normal
head advancement is allowed. Regression, unavailable finality, changed hashes
and cancellation return no partial evidence or send permission. Transient read
errors remain distinct from the typed stable payout-root mismatch.

Receipt recovery additionally closes the canonical inclusion identity. A
canonical receipt 90 remains unresolved until EVM finality covers it; EVM 95
admits the same receipt. An independently unchanged block 95 cannot supply
finality after the actual finalized tag regresses to 70.

Fresh `submitClaimDirect` calls also reconcile the exact retained transaction
after the shared submitter returns. Publication requires unchanged receipt
identity and a verified `Claimed` event from the reconciled receipt. A failed
closing read leaves the original prepared bytes, hash, attempts and nonce floor
available for recovery. No replacement signature or historical API intent is
introduced. Restart tests use the actual private queue store and original
synthetic signed transaction.

## Local qualification

All 16 new top-level causal/adjacent roots and the matched 115-root normal/race
claim/onchain selection pass. Two-package build and vet pass. The author receipt
is `/mnt/data/sn-testnet/miner-claim-evm-finality-astra-20261001/receipt.json`,
SHA256 `2ef41870050a50f88bc7bb6b6a40e02ab4b2c5721543aadf87ed898193abafb8`.
Its 58-entry `SHA256SUMS` is
`71884c28a008ce2767d28f0e030afbd2b522fffd72ac48ed1ae8ef07a007786a`;
all entries pass readback. Exact source/dependency fences, external modfile,
commands, named test selections and terminal logs are retained alongside it.
The selection excludes the subprocess-only test helper, which is exercised by
its owning process-exclusion root. Fixtures use generated test keys, synthetic
identities and local Rpc servers; they do not access live chains or services.

The omission controls retain the current tests and independently remove the
original clock correction, the finalized-tag closing read, or the fresh
publication reconciliation. All three omissions fail at the intended
receipt/state, custody, finality-regression and publication assertions in normal
and race: nine distinct roots, eleven root executions in each mode.
Earlier exploratory compile diagnostics are retained separately from terminal
qualification streams.

Independent Sol qualification on exact `6dcb94a1` / server `6c39d307` passes 41
committed normal roots and five independent overlay roots, the combined 46-root
race selection, and two-package build/vet. The overlays retain the original
native/EVM divergence and finalized-tag regression cases, including the actual
fresh-submit/queue path. The report is
`/mnt/data/sn-testnet/sol-claim-evm-finality-6dcb94a1/report.txt`, SHA256
`c74e2adc78854d7d870f37eab90e42f3cb5df7d4436ca86bbcd5d9a31d0726c4`.

## Remaining gates

At this claim source, the generic shared `miner/onchain/eth.go` `waitFinalized`
helper still read one finalized tag and then the canonical receipt block.
The separately qualified [shared successor at `222e45a8`](shared-evm-finality-closure-20261001.md)
adds the closing bracket and transient/missing-evidence retries. This claim
receipt is not relabeled as covering that later helper source or all independent
EVM recovery paths.

Complete semantic runtime approval, both deployed validator roles, controlled
live upgrade/restart acceptance and native state/finality proof verification
remain separate MG-04/PH-04 gates. Rpc finality assertions are not an independent
consensus proof. Cross-host queue ownership and authority to operate the live
relayer remain independent requirements.

A fresh exact source/image release is required after this source. Prior release
SN `233ea2be` / server `94229abb` and the later header-only qualification cannot
supply its provenance, reproducibility or release approval. Release/deployment
approval stays false. No live signing, transaction, service start, deployment
or publication occurred.
