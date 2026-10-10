# Operator receipt commitment qualification and integration

Astra max implemented the additive offline `strecovery verify-receipts`
command in server commit `fbe0c039949618ca9b9c66a7ebd7b160a37e25ce`, tree
`653b1020abd185c4c82ac3f922d4dd2c5547e7a3`. The server root
`codex/mainnet-server-hardening-20260927` now includes that exact qualified
commit by fast-forward from its sole parent `05fee56f3051b8b96ff00f16bbf7f994ad57404d`.
The integrated tree matches the qualified tree exactly and retains the complete
[MG03/R48 lineage](operator-mg03-r48-composition-20260929.md).

The [command contract](https://github.com/urnetwork/server/blob/fbe0c039949618ca9b9c66a7ebd7b160a37e25ce/strecovery/RECEIPT-COMMITMENTS.md)
joins the validated signature census and pinned observation file with bounded
raw Frontier headers and transaction/receipt trie proofs. At each claimed
inclusion index, the transaction must contain the archive's exact signed bytes
and the receipt must supply the claimed type, status and cumulative gas. Gas
used comes from the committed receipt minus its proven predecessor, including
when that predecessor belongs to an unrelated transaction. Consecutive raw
headers must link the inclusion block to the exact supplied EVM boundary.
The conditional join retains every original, replacement, cancellation and
unavailable sibling. The command has no live RPC, signing, submission, database
mutation or custody-restoration port.

Sol medium qualified the frozen source in the isolated physical module graph:

| Scope | Normal | Race |
| --- | ---: | ---: |
| New commitment verifier and command roots | 14/14 | 14/14 |
| All declared recovery/CLI roots, including those new roots | 52/52 | 52/52 |
| Of that declared total: private PostgreSQL census roots | 3/3 | 3/3 |

Every declared root appears once per mode with no failure or skip. Four isolated
causal controls compiled and failed at their intended assertions: accepting
forged gas, accepting forged status, substituting another signed candidate at
the receipt position, and accepting disconnected header ancestry. Vet,
formatting, diff checks and source/module fences passed. Disposable fixture
command, cleanup and source/module checks all exited zero. No shared database,
live RPC, signer or deployment was used.

The [sealed Sol receipt](/mnt/data/sn-testnet/qualification/mg03-receipt-commitments-20260929/SOL-RESULT.md)
has SHA-256
`e1a9ba4928731ecdfe3db77069e16003dd26c9707201dfc50fe2ee984383aa3e`.
Its [per-root inventory](/mnt/data/sn-testnet/qualification/mg03-receipt-commitments-20260929/sol/per-root-results.json)
and raw normal/race, fixture and causal-control logs remain alongside it.
The [read-only integration review](/mnt/data/sn-testnet/qualification/mg03-receipt-commitments-20260929/INTEGRATION-REVIEW.md)
has SHA-256
`11d5fa3cc4953843a8ee0998478f13904558c1c3513fefc61962fb2de4bfb8ec`.

The qualified graph retains SN `5198f6c9`, SDK `516521fb` and Connect
`b163f9dd`, with the other previously pinned physical siblings. Absolute local
replacements occur only in the external qualification modfile. Five indirect
declarations required by the existing geth trie package were added without
upgrading existing versions; tracked `go.sum` is unchanged. The final candidate's
before/after module lists are byte-identical. Earlier compile refusals before
those declarations remain retained and are not test results. This qualification
does not attest the current SN/SDK/Connect roots as a new composed release.

This closes source-level verification of receipt/transaction commitments and
gas intervals relative to a supplied EVM boundary. It does not authenticate the
boundary itself: an internally consistent fabricated branch still needs
independent native consensus finality and runtime-qualified native-to-EVM
mapping. Account nonces also remain unauthenticated. Actual fees remain null;
receipt bytes contain no effective gas price, raw Frontier RLP15 headers do not
commit the RPC-rendered base fee, and actual runtime/native debits remain
unproven. All finality, canonical-accounting and spending-authority flags stay
false. A bounded production observation collector, fee evidence, service
adoption, release artifacts and live custody/restart qualification remain open.
MG03/PF03 are not closed, and no deployment or chain effect was performed.
