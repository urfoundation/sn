# Runtime identity fixture correction

Frozen source is `5a411d2bf57e03c60a6ea3b3998d1cf401877342`, parent
`85c09583ce8a0728b4b33b04b19c9e7d7ea1272a`, in the adjacent `sn/` checkout.
The sole changed/formatter path is `sim-testnet/runtime_config_identity_test.go`.
Terra's `terra-runtime/formatter-5a411d2` confirms no gofmt delta and a clean
source tree. File SHA256:
`20b158587f44bce15137e92a694683c710fb3649dd0c8e006e26e95b60a749d2`.
Root reviewed this one-file correction. No production, dependency, release lock,
public manifest, fixture helper or other test body changed. Astra ran no test,
build, formatter or RPC.

## Actual failure and correction

The retained 85c normal and race migration20 bodies both failed only
`TestCoordinatorRepairCarryRuntimeIdentityAuthenticatesOriginalRepair` with
`setup migration changed original action or budget authority: <nil>`.
Their adjacent50 bodies passed, and all source/dependency/binary fences matched.
Original capture:
`/home/by/urnetwork/temp/sn-runtime-config-identity-correction-20260915/terra-runtime/qualification-runtime-config-identity-85c0958`.

The fixture approved its source plan before injecting mock HTTP endpoints:
`evidence_carry_fixture_test.go:339` and `coordinator_repair_carry_test.go:294`
later replace `OperationalEVM`. The failing root already preserved the original
public manifest, but copied that later endpoint into its fresh planning config.
`resolvedPlanInputs` (`plan.go:695`) correctly hashes this endpoint, and
`config.render` (`plan.go:1569`) correctly binds that resolved-input hash. The
fixture therefore compared two different launch routes as if they were equal.
Original signed-repair authentication had already passed before that comparison.

The corrected root snapshots resolved inputs before either HTTP override,
restores only the original planning endpoint, independently reproduces the
archived resolved-input hash, and diagnoses identity, inputs, actions and budgets
separately. It keeps the exact historical archive/hash/signature checks and
provided mock clients. A plain in-root control changes a synthetic endpoint and
requires a new resolved-input hash and exactly one changed `config.render`
intent; ConfigHash and all unrelated actions remain identical. No real endpoint
is dialed by this pure changed-route planning control. No test name or descendant
inventory changed.

## Exact bounded execution

Use `handoff/selector.txt` and `handoff/roots.txt`: one root, zero descendants,
package `github.com/urfoundation/sn/sim-testnet`. The changed-route control runs
within the same root. Normal and race both expect body exit0 and exactly one
top-level PASS. Use the existing 600-second test / 660-second outer limits.

Compile one binary per mode on this frozen source. Run p1, then fresh p2, then
fresh p3 sequentially in each mode, retaining that mode's binary and source.
The normal/race lanes may operate independently; a failure stops its streak.
Package CWD remains `sn/sim-testnet`; use the existing testnet.yml, policy/release,
coordinator-repair testdata and the same 13 reviewed repository snapshots as85c.
Reuse the accepted streaming capture owners with exact lists, `-test.v`, numeric
body exits, events and complete pre/post source/dependency/binary fences. If the
existing replay tool is used, its BODY_EXIT operand is the canonical absolute
`body.exit` path, not the numeric status. This root has no descendant metadata.
No new runner, broad matrix or causal compile is requested.

## Scoped reuse and causal evidence

The original70 bodies are retained as69 PASS / one FAIL per mode, not as complete
passes. The other69 selected top-level bodies, their helpers and their production
paths are byte-identical; the five other runtime-identity roots in the edited file
are untouched. Retain their scoped results rather than rerunning all70. After
this root qualifies, report the combined scoped coverage with both source heads.
Production Go, release-lock implementation/inputs and the updated parser bytes
are unchanged, so the existing c437900d lock and parser evidence remain reusable.

The completed production causal remains applicable without mutation or execution
here. `git apply --check` succeeds for the existing
`sn-runtime-config-identity-correction-20260915/handoff/CAUSAL-original-runtime-hashing.patch`;
it changes only unchanged `sim-testnet/config.go`. Its retained capture is
`.../qualification-runtime-config-identity-85c0958/causal-original-runtime-hashing`.
The sealed RESULT and `r2/body.raw:10` show the repair root failed at the unchanged
authentication call with `original signed repair lost authority after explicit
runtime migration: coordinator repair configured strict domain differs`, before
the changed planning section. The other two expected failures and two passing
controls have entirely unchanged bodies. This correction's planning endpoint
does not enter releaseConfigHash; authentication uses the supplied mock clients.
Retain that exact3 FAIL / two PASS causal as source-scoped evidence; no rerun is
required for this fixture correction. The two original85c body failures are the
retained pre-fix fixture reproductions.
