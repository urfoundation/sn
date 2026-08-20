# Release 1.0 testnet launch

The supported release-1.0 installer and integration test is `sim-testnet`. The
complete operator runbook is [`sim-testnet/README.md`](../sim-testnet/README.md).
Older instructions that created a subnet or deployed the monolithic `STSubnet`
contract are retired and must not be used.

## Current gate

Implementation and local verification can run without launch authority. The first
live command is deliberately paused until the placeholders in
`../vault/main/st.yml` are filled and reviewed:

- `testnet-wallet`
- `testnet-netuid`
- `testnet-spending-limit-tao-rao`
- `testnet-spending-limit-alpha-rao`
- `testnet-spending-limit-evm-gas-wei`
- `testnet-operator-api-origins`

`BRINGYOUR_SUBTENSOR_HOSTNAME` must also resolve to the deployed runtime-447 RPC
gateway from the execution host. Testnet uses the isolated `testnet-` namespace
and single-owner governance. Unprefixed settings are mainnet-only; mainnet contract
custody remains 2-of-3 Safe governance.

Filling configuration is not approval to spend. Every mutating command is a dry
run unless it receives both `--apply` and the exact hash of the reviewed plan.

## Read-only preflight

From the `sn` repository:

```bash
go build -trimpath -o build/sim-testnet ./sim-testnet

./build/sim-testnet doctor \
  --config sim-testnet/testnet.yml \
  --format json

./build/sim-testnet plan \
  --config sim-testnet/testnet.yml \
  --format json > /tmp/ur-subnet-testnet-plan.json
```

Both commands are read-only. Review every action, dependency and maximum spend in
the plan, then preserve its `plan_hash`. `doctor` must be completely green. In
particular, Docker, user systemd, the locked repository checkouts (including the
platform `config` resources), Substrate and
EVM RPC, wallet ownership/balances, runtime identity, precompiles, MinIO and the two
public operator origins must be available.

The harness operates only on the existing configured netuid. It refuses subnet
creation and checks that the wallet owns the subnet before planning writes.

## Approved convergence and persistent launch

Only after explicit approval of the generated hash:

```bash
./build/sim-testnet launch \
  --config sim-testnet/testnet.yml \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH \
  --detach
```

This idempotently configures the existing subnet, deploys and verifies the immutable
reserve and settlement vault plus UUPS coordinator, registers/funds release roles,
publishes the native fleet commitment, starts two operators, eight miners, two
validators, claim daemons, two independently keyed three-client head fleets and
two tail miners, proves readiness, runs the mandatory value-capped
`precompile-conformance` scenario, and then runs smoke. The conformance gate
replace/restores a native commitment, exercises both signature precompiles plus
metagraph/neuron/staking, proves exact stake moves, waits for a dividend cycle and
recovers all probe-attributable alpha to a controlled provider coldkey.
Transactions are journaled through intent, signed bytes and nonce,
broadcast, inclusion, finality and postcondition. An interrupted convergence is
continued with `resume` and the same approval hash.

## Release evidence

```bash
./build/sim-testnet scenario --name release-1.0 \
  --config sim-testnet/testnet.yml \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH

./build/sim-testnet scenario --name production-soak \
  --config sim-testnet/testnet.yml \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH

./build/sim-testnet inspect --config sim-testnet/testnet.yml --format json
./build/sim-testnet analyze --config sim-testnet/testnet.yml --format json
```

The first campaign proves the accelerated 20-epoch release matrix. The second
schedules the canonical 50,400-block policy and proves two production epochs,
verification-key rotation with historical proof availability, plus supervised
new-PID rolling recovery of every role. Evidence is content-addressed in the existing
`server/blob` MinIO store, indexed by both operator APIs, signed, and independently
reconstructable from the public deployment manifest. No live claim is waived by a
local unit-test result.

Use `stop` to stop local processes without deleting evidence or chain state. Use
the separately planned, future-effective, hash-approved `retire` command to
deactivate operators. Retirement does not remove the immutable vault, reserve,
entitlements, claims, artifact history, role store, or journal.
