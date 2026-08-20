# stctl — legacy STSubnet diagnostic

`stctl` is retained solely to inspect and regression-test deployments of the
pre-1.0 monolithic `STSubnet`. It is not compatible with the release-1.0
`STCoordinator` / `STSettlementVault` / `STReserveSink` topology and must not
be used for release deployment, deposits, roots, finalization, or claims.

Release operations use:

- `sim-testnet` for spend-bounded setup, contract installation, topology,
  scenarios, evidence, and retirement;
- the server’s scoped deposit/root/keeper tasks for operator settlement; and
- the miner claim daemon for finalized vault claims.

The CLI fails closed unless its YAML explicitly contains:

```yaml
contract_generation: legacy-pre-1.0-stsubnet
```

That acknowledgement does not make it safe to point at a 1.0 coordinator. Its
packaged ABI and generated Go binding intentionally remain the legacy
`STSubnet` ABI so historical diagnostics are internally coherent.

Build and test the retained utility with:

```bash
go test ./stctl
go build ./stctl
```

Run `stctl --help` only when maintaining an old monolith. The supported 1.0
runbook is [`docs/LAUNCH.md`](../docs/LAUNCH.md).
