# Release 1.0 testnet launch

The supported release-1.0 installer and integration test is `sim-testnet`. The
complete operator runbook is [`sim-testnet/README.md`](../sim-testnet/README.md).
Older instructions that created a subnet or deployed the monolithic `STSubnet`
contract are retired and must not be used.

## Current gate

Implementation and local verification can run without launch authority. All
required `testnet-` inputs are populated in `../vault/main/st.yml`: the contained
encrypted wallet/password references, existing netuid 521, three spend ceilings,
two loopback operator API origins, MinIO overlay host, private RPC hostname and
single-owner testnet governance. Unprefixed settings remain mainnet-only; mainnet
contract custody remains 2-of-3 Safe governance.

The checked-in `sim-testnet/testnet.yml` currently selects the official public
Substrate and EVM testnet RPCs as its operational pair. Both typed override URLs
must be present; deleting both returns deterministically to `testnet-authority`.
The private archive at `sim-testnet:9944` remains the required fallback for heavy
workloads, historical/archive proof and the final independent-backend campaign
after it finishes syncing. Public mode records the shared observation backend as
non-independent in doctor, postconditions and manifests; that assurance gap is
non-blocking for bounded testnet acceptance but blocking for mainnet promotion.
Runtime spec 451 and its exact finalized Wasm code hash remain hard gates in both
modes.

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
particular, Docker (direct or passwordless-sudo), user systemd, the locked repository
checkouts (including the platform `config` resources), the selected Substrate/EVM
pair, canonical finalized head, wallet ownership/balances, runtime identity,
precompiles and MinIO HTTP liveness must pass. Private mode additionally requires
physically independent observation, consensus peers, and a fully synchronized
archive; public override mode records those unavailable assurances as explicit
non-hard warnings. The subnet token and first-emission block must already be activated and
finalized, and the private EVM RPC must serve historical state for transaction
balance-delta proofs. Operator origins are proved after the
two local APIs start and before public evidence is accepted.

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
publishes the native fleet commitments, starts two operators, 1,000 miner
identities in 20 real production swarms, two validators, claim daemons, 202
independently keyed four-client head-candidate fleets competing for exactly 200
native head slots, and 192 long-tail miners, proves readiness, runs the mandatory value-capped
`precompile-conformance` scenario, and then runs smoke. The conformance gate
replace/restores a native commitment, exercises both signature precompiles plus
metagraph/neuron/staking, proves exact stake moves, waits for a dividend cycle and
recovers all probe-attributable alpha to a controlled provider coldkey.
Every registration uses runtime `register_limit`/`registerLimit` with the reviewed
`100000000` rao ceiling. EVM registrations fund the caller mirror and send zero
value to the neuron precompile, matching runtime-451 deduction semantics.
Contract registrations supply that full ceiling and atomically refund the
unburned difference.
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

The first campaign proves the accelerated 20-epoch release matrix while the full
adversarial actor set remains active, including promotion/demotion at the 200-slot
head boundary. The second schedules the testnet-only 2,400-block/eight-hour policy
and proves three consecutive fully observed production epochs, dishonest-deposit
penalty and recovery, verification-key rotation with historical proof availability,
plus supervised new-PID rolling recovery of every role. Mainnet retains the
separately reviewed 50,400-block/seven-day policy. Evidence is content-addressed in the existing
`server/blob` MinIO store, indexed by both operator APIs, signed, and independently
reconstructable from the public deployment manifest. No live claim is waived by a
local unit-test result.

Use `stop` to stop local processes without deleting evidence or chain state. Use
the separately planned, future-effective, hash-approved `retire` command to
deactivate operators. Retirement does not remove the immutable vault, reserve,
entitlements, claims, artifact history, role store, or journal.
