# Operator recovery after an incomplete receipt census

Scope: the production server account reconciler, MG-03/PF-03. No live signing,
transaction submission, deployment or production database mutation was
performed. Qualification used private test databases and synthetic keys.

## Root cause and bounded change

`CoreStClient.reconcileAccountIntents` checked every signed attempt but discarded
the returned error when no receipt was pending. If the finalized account nonce
had advanced, it marked the intent and its attempts `superseded`.
`GetUnresolvedStTransactionIntents` excludes that status, so later restarts never
retried the missing canonical receipt. Originals, fee replacements and
cancellations could all disappear from active recovery through this path.

Server `9f70d394` returns an incomplete-observation error
before nonce-based supersession. The original rows, exact signed bytes, fees and
attempt numbers remain available to the next owner. A genuinely canonical
candidate still takes precedence over another candidate's failed read. The
adjacent `waitFinalizedAttempt` branch now requires a complete census and a live
caller before returning permission for a bounded fee replacement.

A successful read that disproves an old inclusion is different from a failed
read. Only the reconciler's private orphan diagnostic carries that distinction.
Every leaf of a joined error must be such a proved orphan before either caller
may proceed. The observer retains all error causes, so a later orphan cannot
hide an earlier transport or integrity failure. Caller cancellation remains
part of the returned error.

The model already enumerates all attempts for a selected intent, newest first,
without filtering their statuses. Its account query excludes finalized,
reverted, canceled and superseded intents. Neither query nor any historical
row is changed here. Complete status-independent cross-database/store export,
historical wrongly-superseded reconciliation and complete fee reporting remain
open. This is not MG-03/PF-03 closure or deployment authorization.

## Deterministic qualification

The baseline fixture commit is server
`3a0cb0db11701902cf799ee86805db4ea0bd1db8`, built on
`0633780c`. It contains the new tests with unchanged production code. The
initial corrected commit was `3cc93894eac94e9d0e9a177b74fd2de2b14f464e`;
the qualified correction is its follow-up
`9f70d394eb9321018f0a7eac8a85e22da0724e76`.

Four new top-level tests use isolated test databases and a local HTTP Rpc
fixture. Two operator identities each retain all three genuinely signed
synthetic transactions under one nonce. The winning original, replacement or
cancellation first returns a serialized Rpc error or transplanted receipt;
an explicit second phase supplies the same winning canonical receipt to a
fresh owner. Assertions require unchanged durable rows on refusal, every
candidate still discoverable, unchanged signature/gas bytes after recovery,
exact terminal winner, idempotent restart and zero send/fee/nonce-allocation
calls. A pre-canceled finality wait and an already-eligible replacement age
force the adjacent replacement branch without sleeps.

The mixed-error test combines a proved orphan with a failed transport or
integrity read in both candidate orders. It requires the orphan's stale
inclusion to be removed while every exact signed candidate remains available;
the unknown winner is reconciled on the next successful observation.

The first independent run stopped before assertions because another lane had
closed its private database fixture. A fresh owned PostgreSQL/Redis fixture
then exposed a real regression in the initial correction:
`TestStAccountReconcileCancelsEveryStaleAttemptState` could no longer cancel a
proved orphan. That expectation was retained. The follow-up distinguishes
proved orphans and adds the mixed-error test above. The original failure is
retained in [the first corrected-run log](/mnt/data/sn-testnet/operator-recovery-qual.uMupUO/controller-normal.log).

The independent coordinator ran qualification directly after the requested
Terra lane was refused by the agent thread limit. Fresh owned PostgreSQL/Redis
services were removed by the harness trap after qualification. The baseline
run returned exit 1 and reproduced all three expected failures: the two
account-recovery tests lost the signed candidates after an inconclusive
census, and the finality wait offered replacement after a failed read. That
run was observed in interactive tool output; no separate baseline log was
retained. The exact fixture commit above retains the failing assertions.

The final affected selector contains 19 top-level tests, including the existing
proved-orphan cancellation control that rejected the first correction:

| Check at server `9f70d394` | Result | Retained evidence |
| --- | --- | --- |
| Affected normal selector | PASS, 99.631 seconds, exit 0 | [Normal log](/mnt/data/sn-testnet/operator-recovery-qual2.L4exlt/controller-normal.log) |
| Same selector with `-race` | PASS, 137.093 seconds, exit 0 | [Race log](/mnt/data/sn-testnet/operator-recovery-qual2.L4exlt/controller-race.log) |
| `go vet ./controller` | PASS, exit 0, no output | [Vet log](/mnt/data/sn-testnet/operator-recovery-qual2.L4exlt/controller-vet.log) |
| `gofmt -l` on the three changed Go files | PASS, no output | Independent coordinator check |
| `git diff --check 0633780c..9f70d394` | PASS | Independent coordinator check |

Retained log SHA-256 values:

```text
ebad9ea410ab90fe6bb382cb26bfcb1f50b633179ba7f15d298afc771b7c3397  corrected normal
08cc5a1aa489dcc28dcae68863e2fb0204a6f6b2ea0a50e552e6071696d78deb  corrected race
e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  corrected vet
fba183f43d165c216fb2accb1907d70826755b7d3a5c82fd327f86c56d44ffd0  rejected first correction
```

The baseline selector is:

```text
^(TestStAccountRecovery(ReadError|MalformedReceipt)RetainsEverySignedCandidate|TestStReplacementWaitReadErrorCannotAuthorizeReplacement)$
```

The affected selector also covers existing account reconciliation, exact
receipt identity, candidate ordering, orphan and pending-inclusion controls:

```text
^(TestStAccountRecovery.*|TestStReplacementWaitReadErrorCannotAuthorizeReplacement|TestStAccountReconcile.*|TestStReceiptCandidates.*|TestStReceiptObservation.*)$
```

The qualification environment used the server's isolated database harness,
`WARP_ENV=local`, `WARP_TEST_ENV_FAIL_FAST=1`, `GOWORK=off` and `GOMAXPROCS=2`,
with its private `environment.sh` and the server's `test-env.sh` sourced. After
the harness supplied database/cache configuration, the corrected commands were:

```sh
selector='^(TestStAccountRecovery.*|TestStReplacementWaitReadErrorCannotAuthorizeReplacement|TestStAccountReconcile.*|TestStReceiptCandidates.*|TestStReceiptObservation.*)$'
go test ./controller -count=1 -timeout=5m -run "$selector"
go test -race ./controller -count=1 -timeout=10m -run "$selector"
go vet ./controller
gofmt -l controller/st_controller.go controller/st_transaction_observation.go controller/st_transaction_recovery_errors_test.go
git diff --check 0633780c..9f70d394
```

No actual chain endpoint is used. These checks qualify the bounded correction,
not complete MG-03/PF-03 recovery discovery or deployment readiness.
