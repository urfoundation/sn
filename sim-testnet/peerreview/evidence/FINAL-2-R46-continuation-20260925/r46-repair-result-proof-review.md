# R46 signed corrective result chronology qualification

The R46 historical collector admitted the owner-signed corrective activation
after the action-reader fix, but offline chronology still expected a static
plan action and an ordinary `StageVerified` postcondition. The corrective
operation has neither: its source plan predates the repair, and the original
owner signed a separate result containing its exact finalized journal rows.

The new regression uses a synthetic signed corrective request and result.
The old source failed at the expected static-plan lookup (exit 1 in
`repair-result-prefix-red.exit`). The fix keeps ordinary postconditions
unchanged and carries a separate `repair_result_artifact` for the corrective
activation. The offline verifier compares that artifact to the result in the
approved descendant plan, authenticates the original request/result and
journal coordinates, and replays exact calldata and the `Upgraded` event.
It rejects a changed block, missing carry, invented verified row, altered
result artifact and changed calldata. The artifact census includes the new
proof locator, and a corrective activation derives its post-runtime from the
signed request rather than the predecessor's older upgrade.

The final source passed both commands (normal 32.363 seconds; race 314.178
seconds; exits 0):

```sh
go test ./sim-testnet -run 'TestFinalHistoricalCoordinatorJournalArtifactAcceptsSignedRepairResult|TestFinalSemanticHistoricalCoordinator|TestFinalSemanticHistoricalCapture|TestFinalSemanticHistoricalArtifactCensus' -count=1 -timeout=20m
go test -race ./sim-testnet -run 'TestFinalHistoricalCoordinatorJournalArtifactAcceptsSignedRepairResult|TestFinalSemanticHistoricalCoordinator|TestFinalSemanticHistoricalCapture|TestFinalSemanticHistoricalArtifactCensus' -count=1 -timeout=25m
```

The prior carried-action census regression was repeated after the shared
helper refactor. It passed normally in 1.792 seconds and with race detection
in 14.626 seconds, both exit 0.

The original R46 owner result and sealed 39-check diagnostic remain
unchanged. This qualification establishes behavior on synthetic proof inputs;
the next read-only terminal diagnostic must test the complete R46 archive and
report any later acceptance failures separately.
