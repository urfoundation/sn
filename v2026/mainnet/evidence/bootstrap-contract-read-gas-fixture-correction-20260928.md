# Contract fixture read-gas correction

Base: `14afe75d951b1ecc464f84d6ebc2ecb3fb86cfc6`. The containing commit on
`codex/mainnet-reserve-read-gas-fix-20260928` is the frozen correction candidate,
isolated at `/home/by/urnetwork/temp/sn-mainnet-reserve-read-gas-fix-20260928`.
It changes the shared test fixture and two test roots, with no production code,
approved graph, journal schema, binding, generated artifact or module change.

Sol's original positive run and exact isolated reproduction failed
`TestEvmReserveLinkRevertRetainsConsumedNonce` before its first selected send:
`eth_call: HTTP 400`. Raw evidence remains in
`/mnt/data/sn-testnet/qualification/sol-reserve-link-20260928/focus-normal.log`
and `repro-revert-normal.log`. The aggregate's later fifteen-minute timeout is
preserved separately; it does not establish a second product failure or a pass.

`signSelectedAction` sets the fixture's execution gas limit to the selected
approved transaction's limit. The revert root deliberately chooses 21,000 gas.
The shared `eth_call` handler reused that execution config for historical,
current and pending getter simulation. Initialized proxy policy reads require
more than that low limit, so prerequisite admission failed at an unrelated read
before the intended genuine EVM out-of-gas call could be sent. This is a test
fixture budget coupling; no production admission check is weakened.

The handler now uses a local config copy with an independent 2,000,000-gas read
budget. Historical state copies and exact block numbers remain unchanged.
The original VM execution limit, signed envelope and transaction gas remain
unchanged. Both the history-backed and legacy history-nil fixture branches use
the separate read config. Earlier call/CREATE execution modeling is preserved;
this patch does not change intrinsic transaction-gas modeling.

The failing reserve root now advances to a distinct current head and explicitly
requires successful original-proxy, current-canonical and pending policy reads
before the sole original reserve-binding send. It still requires genuine call
gas exhaustion, zero binding state, one consumed original nonce, retained
status-zero receipt, no duplicate spend and an offline restart. It additionally
asserts that both transaction and execution gas remain exactly 21,000 and the
original signed bytes were sent.

`TestEvmCreateGetterReadBudgetPreservesExecutionBudget` covers the adjacent
history-nil path. It genuinely deploys the reviewed reserve, then lowers only
the fixture's execution budget to one before receipt reconciliation. Canonical
getter reads must still complete without changing that budget, the signed gas,
code, nonce or single original write. The previous handler deterministically
cannot execute the first getter with one gas. Neither root uses sleeps or
positive subtests.

The author ran gofmt, `git diff --check` and compile-only `go test -c -p=2`;
the binary was not executed. External module files, compile log and binary are
under `/mnt/data/sn-testnet/worktrees/sn-mainnet-reserve-read-gas-fix-20260928/`.
The eight physical dependency pins and source graph remain those recorded in
`bootstrap-contract-reserve-link-source-20260928.md`. No live signing, RPC, DB,
chain or Safe-inner action occurred.

Sol should first run these exact roots normally and under race:

```
^Test(EvmReserveLinkRevertRetainsConsumedNonce|EvmCreateGetterReadBudgetPreservesExecutionBudget)$
```

Then run all reserve-link roots and the adjacent low-gas/refusal paths across
reserve CREATE, vault CREATE, coordinator CREATE, escrow and proxy CREATE, with
appropriate broader normal/race/static gates. Preserve the original failure
evidence and fence every invocation to this exact successor, rather than
carrying a pass from the failed predecessor.

After establishing the positive roots, remove only
`config.GasLimit = 2_000_000` from the `eth_call` handler in an isolated causal
control. Both roots must fail at their real getter-read boundary. Restoring the
transaction gas to a higher value would invalidate the intended control and is
not a correction. Behavioral qualification is independently owned by Sol.
