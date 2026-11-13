# Production read-cause qualification

The source-finality and adjacent read-cause fixes are integrated through
`61a3a23d`. Astra authored the changes and causal fixtures; Terra medium ran
the frozen candidate `29399fe2` and the isolated fixture correction `8bf62355`.
No live transaction or production deployment was part of these checks.

An RPC failure previously acquired an additional error asserting a changed
canonical block, unfinalized receipt, wrong owner or invalid activation. The
retry classifier then correctly rejected the combined error as a contradiction.
The corrected readers return the I/O cause before evaluating any returned
value. Source finality also reuses the body, extrinsic index and decoded events
already admitted for dispatch verification, avoiding a second inconsistent or
unavailable read. Missing receipt fields have a typed unavailable outcome;
complete invalid evidence and actual dispatch failure remain hard errors.

| Frozen positive scope | Normal | Race |
| --- | --- | --- |
| Shared receipt/source readers, 47 roots | Pass, 10.255 s | Pass, 92.687 s |
| Miner mainnet/recovery, 64 roots | Pass, 61.700 s | Pass, 528.251 s |
| Validator source, owner eligibility and activation, 65 roots | 64 pass, one fixture failure; 42.270 s | 64 pass, the same fixture failure; 354.399 s |
| Isolated source-role fixture correction | Pass, 1.372 s | Pass, 13.363 s |

The source-role fixture used an arbitrary hash and incomplete SDK header
instead of the real RPC wire representation. The two-line correction uses the
existing genuine SCALE header/hash and `0x` quantity helper. Only its failed
root was rerun; the original package failures remain retained. Vet passed for
`crv4`, `miner` and `validator`.

Five source-reader predecessor controls reached their intended assertions in
both modes, normal 2.673 seconds and race 24.223 seconds. Four further control
families cover miner EVM recovery, owner census, production eligibility and
activation preparation/recovery. Each failed its intended timeout-as-contradiction
assertion in both modes. No compile failure, panic or package timeout was used
as causal proof.

The first source control mistakenly supplied `08bb86fc` bytes, identical to
the corrected source file, and all five tests passed. That invocation is an
invalid regression control, preserved separately. The corrected control uses
exact `6720f152:crv4/source_commitment.go` in a new overlay. It does not modify
the tested source or overwrite the earlier evidence.

Author commits `9ae7f1f4`, `08bb86fc`, `29399fe2`, `8bf62355` map to integrated
commits `38fc2d34`, `40d65ad1`, `ecea8438`, `61a3a23d`. The only integration
conflict was documentation; both prior lessons and the new reader scope were
retained. Changed production files and the corrected source-role fixture match
the qualified source byte-for-byte. The integrated installer exercises a shared
reader dependency separately; no whole-suite restart is implied.

The frozen candidate's final check confirmed all 19 source-manifest entries,
an unchanged clean `29399fe2` worktree and no whitespace errors. The resolved
module graph is byte-identical before and after. This records the selected
module graph, not an assertion that every dependency file is covered by that
19-file manifest.

On integrated `61a3a23d`, compile-only checks passed for `mainnet`,
`sim-testnet/gencontracts`, `crv4`, `miner` and `validator`. The two composed
roots `TestEvmCreateCommandExecutesReviewedReserveAndResumes` and
`TestRootSubmissionOfflineCustodyServiceComposition` also passed normally
(1.740 seconds) and with race detection (11.172 seconds). The head and consumed
source hashes were unchanged across that execution. Documentation was edited
during the check; the final status records those documentation-only changes.
The compile-only invocation is not counted as test-body coverage.

Raw evidence is under
`/mnt/data/sn-testnet/qualification/source-finality-read-20260928/`: `terra/`
contains the original package results, `fixture-correction/` the isolated
correction, and `terra/causal/` the predecessor controls. Corrected source
controls are named `source-6720-normal.jsonl` and `source-6720-race.jsonl`.
The `integrated/` directory retains the composed results and source fences.

| Retained input | SHA-256 |
| --- | --- |
| Original 19-file source manifest | `e7969061f87a9171e035a39888cd7a5088888d7a63fb9cd46f297231db76e2e0` |
| Original resolved module graph | `8c69ab31f7c8746f98a89ab5856b21c5df1fbc15f1a62711657e1bc0654f2140` |
| Correct source predecessor file | `033b0a6c2b17788becd5d1ff8f51988adc0905f5fd843e57cc24ba479b8ea90b` |
| Integrated consumed-source manifest | `c96be43cef2bde53e641cc98a6d374dceab8cad779f3460fab0401dbe888db9a` |
| Integrated two-root normal event stream | `237f145a8783ef603c2c606f2ad3f2b3a19d4eabcfb44fab42ecb7b465c30941` |
| Integrated two-root race event stream | `e73ee00a7923f4700d86952509634c9447d12bcb453b3cd03aa00529f2624065` |

These changes preserve error meaning; they do not yet implement the complete
production continuation loop. Retained-intent reconciliation before fresh
snapshots, durable waits across epoch changes, complete startup composition,
bounded miner scan checkpoints, exact shared receipt/nonce coverage, native
HTTP status preservation and deployed progress monitoring remain tracked in
PH-03 and the launch plan. An expired local window does not revoke an immortal
signed transaction or permit a new signature while its outcome is unknown.
