# R46 carried repair history reader qualification

The sealed 39-check diagnostic rejected `repair.coordinator-rounding.deploy`
while deriving the historical release capture range. The archived source plan
intentionally has no static repair action. Its descendant plan carries the
owner-signed request and result, including the exact original finalized
journal entries. The old reader searched only static and relay actions.

The regression test builds a synthetic signed repair with the production
fixture and exercises capture, historical coordinator receipt selection, and
the transition's signed post-runtime identity. It rejects a changed intent,
duplicate finalized entry, and absent carry. The pre-fix checkout at
`a35ca277` failed for the expected static-plan error; exit 1 is retained in
`composed-repair-prefix-red.exit` and its output in
`composed-repair-prefix-red.log`. With the fix, the affected normal command
passed in 34.276 seconds and the same selector under race detection passed
in 308.177 seconds, both exit 0:

```sh
go test ./sim-testnet -run 'TestFinalCaptureReleaseContractCensusAcceptsExactCoordinatorRepairCarry|TestFinalSemanticHistoricalCoordinator' -count=1 -timeout=15m
go test -race ./sim-testnet -run 'TestFinalCaptureReleaseContractCensusAcceptsExactCoordinatorRepairCarry|TestFinalSemanticHistoricalCoordinator' -count=1 -timeout=20m
```

The earlier combined SN/validator focus passed in 126.317 and 4.304 seconds,
respectively, before this history-reader patch. It does not qualify the
subsequent patch. The final focused normal and race commands above cover the
changed history-reader files. The original R46 result and diagnostic are
unchanged.

This patch admits the exact signed corrective actions to the capture,
receipt-selection and transition readers. Offline chronology still requires
an ordinary `StageVerified` postcondition row. The corrective repair instead
has a distinct signed result; that verifier schema requires a separate
authenticated proof path before the full diagnostic can pass. No acceptance
waiver or synthesized postcondition is included here.
