# Contract graph candidate: retained failure and review block

The separate candidate `75ae5c14a9fb94644fc20c44f6e92235ddcd2db0` is frozen at
`/mnt/data/sn-testnet/worktrees/sn-mainnet-contract-graph-20260928/sn`.
It is not integrated into the selected production branch. The exact shared
cadence source is already integrated separately as `a58878ea`.

The public nine-action command passed normally at `b2fb2cdd` in 33.71 seconds
and under race at `75ae5c14` in 163.36 seconds. Those are scoped local results,
with genuine Safe execution and a declared model of the native registration
precompile. They do not prove live native rollback or mainnet deployment.
Raw evidence is under
`/mnt/data/sn-testnet/evidence/mainnet-contract-graph-20260928`.

All planned test bodies have now completed. The affected selection remains
failed, with `TestEvmGraphOuterSuccessCannotHideSafeInnerFailure` failing in
both modes. Each shard 9 retained three passes and that one failure. The
neighboring `TestEvmGraphLostRepliesRetainEveryCompletedAction` passed,
including recovery of the original final transaction after a receipt-reader
failure.

| Retained scope | Result |
| --- | --- |
| Normal, frozen `75ae5c14` | 46 passed, one failed; all 47 selected roots completed |
| Reused normal, unchanged `b2fb2cdd` full command | One passed; aggregate coverage is 47 passed and one failed across 48 roots |
| Race, frozen `75ae5c14` | 47 passed, one failed; all 48 selected roots completed |
| Regression controls, normal and race | Five controls distinguish their passing positive roots from the intended failures in each mode |
| Safe-inner control, normal and race | The expected failure is retained but is **non-discriminating**: its positive root already fails |
| Source and module checks | Both maintained reports record unchanged source. The completed candidate module seal matches its original; completed control modules retain their exact physical roots and approved revisions |
| Vet | `go vet -p=2 ./mainnet` passed from the actual candidate module root; `terra-vet-final.exit` is 0 |

The control runner reports 14 successful stages, including two builds. That
does not establish six causal proofs: the Safe-inner pair remains unresolved.
The two earlier vet invocations used incorrect working directories and never
performed the package check. Their logs are retained alongside the corrected
`terra-vet-final.cwd`, `.log` and `.exit`; they are harness failures, not
product failures. Final module collection was completed separately without
repeating test bodies. The original outer wrapper's final disposition was not
captured and is not inferred from its absence.

The positive report SHA-256 is
`79ffaebc49d0e83a52abc547b1d5ac2bd87e638d53a35cfe244ca53ea428bbf6`;
the control report SHA-256 is
`997db36b82425df66f11d161d41b7df6600a49bc442f902d3aef1ce1a5cfb555`.

The failed fixture expected a lower signed `safeTxGas` to force inner
out-of-gas. The vendored Safe v1.4.1 source instead forwards nearly all remaining
gas when reimbursement `gasPrice` is zero, as this profile requires. The actual
call therefore succeeded. Its signed gas field is a reserve/minimum guard in
this mode; the approved outer transaction gas limit supplies the overall gas
ceiling. The test does not yet establish correct handling of a genuine inner
failure. Preserve its original result rather than relabeling it as a pass.

Automatic review stopped Astra's attempted correction of this failure fixture,
citing possible cybersecurity risk. No correction was implemented or tested.
The new correction worktree was created at unchanged `75ae5c14`; the original
candidate, isolated control source and existing captures remain intact. Do not
retry or route that rejected correction through another agent or tool.

Retain these failures and successful scopes. The correction, affected
qualification, production integration and live deployment gates remain open.
Independent receipt recovery and diagnostic-export implementation can continue.
