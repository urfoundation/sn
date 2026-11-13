# Composed server source check, 2026-09-27

The local server branch `codex/mainnet-composed-hardening-20260927` at
`7bf88d79` starts from v11 `77cb401e`, adds the retained-usage timestamp
barrier and append-only migration 728 (`6e2bcfa7`), then the operator
receipt-census regression and two-step correction (`e2a3be6f`, `1939c779`,
`7bf88d79`). The earlier current-main integration `9f860731` is an ancestor
of v11. Cherry-picks applied without conflict; the worktree was clean before
this check.

The branch subsequently advanced to `b6f49bdb` with atomic payer escrow
admission and checked settlement arithmetic. Its separate causal, normal,
race and vet qualification is retained in the
[atomic admission evidence](/mnt/data/sn-testnet/evidence/mainnet-netescrow-admission-20260927/RESULT.md).
The controller selector below was run at `7bf88d79`; it is not a claim that
the refreshed complete server release gate ran at `b6f49bdb`.

An independently owned disposable PostgreSQL/Redis fixture ran the combined
controller selector below at `GOWORK=off`, `GOMAXPROCS=2` and
`WARP_TEST_ENV_FAIL_FAST=1`:

```sh
go test ./controller -count=1 -timeout=10m -run '^(TestStAccountRecovery.*|TestStReplacementWaitReadErrorCannotAuthorizeReplacement|TestStAccountReconcile.*|TestStReceiptCandidates.*|TestStReceiptObservation.*|TestStReleasePayoutMissingCloseTimeStopsBeforeChain)$'
go test -race ./controller -count=1 -timeout=10m -run '^(TestStAccountRecovery.*|TestStReplacementWaitReadErrorCannotAuthorizeReplacement|TestStAccountReconcile.*|TestStReceiptCandidates.*|TestStReceiptObservation.*|TestStReleasePayoutMissingCloseTimeStopsBeforeChain)$'
go vet ./controller
```

Normal passed in 106.142s, race passed in 145.073s, and vet exited zero.
Retained logs are
[`composed-normal.log`](/mnt/data/sn-testnet/server-composed-qual.uiGoLw/composed-normal.log)
(SHA-256 `a98431c11ca075c863f2f18d642dde50c5537ca4b1e3c3603af9d4f79881f379`),
[`composed-race.log`](/mnt/data/sn-testnet/server-composed-qual.uiGoLw/composed-race.log)
(SHA-256 `f74214e2e153a4cbdf2d65c0fa0146f854fb0ec381b79de57e0c512107906b31`),
and an empty vet log (SHA-256
`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`).
The fixture process exited after its owned-service cleanup.

The separate [timestamp-custody qualification](/mnt/data/sn-testnet/evidence/mainnet-usage-time-custody-20260927/RESULT.md)
passed 140 selected checks in normal and race modes; the
[operator recovery qualification](operator-recovery-census-20260927.md)
records its causal failure and correction. This combined selector verifies the
two changed controller paths coexist. It is not a full server suite, runtime
deployment, migration execution on a production database, or refreshed release
manifest. No live RPC, signing, transaction or mainnet activation occurred.
