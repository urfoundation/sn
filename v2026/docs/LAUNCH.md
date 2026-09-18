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
Runtime spec 454 and its exact finalized Wasm code hash remain hard gates in both
modes.

Filling configuration is not approval to spend. Every mutating command is a dry
run unless it receives both `--apply` and the exact hash of the reviewed plan.

## Read-only preflight

From the `sn` repository:

```bash
SN_REPO="$(pwd -P)"
WORKSPACE="$(dirname "$SN_REPO")"
release_head="$(git rev-parse HEAD)"
build_utc="$(date -u +%Y%m%dT%H%M%SZ)"
SIM_TESTNET_RELEASE_DIR="$WORKSPACE/temp/sim-testnet-${release_head}-${build_utc}"
SIM_TESTNET_BINARY="$SIM_TESTNET_RELEASE_DIR/sim-testnet"
SIM_TESTNET_STATE_DIR="$SN_REPO/sim-testnet/runs/ur-subnet-testnet-v1-attempt-4"
mkdir -p "$SIM_TESTNET_RELEASE_DIR"
go build -trimpath -buildvcs=true -o "$SIM_TESTNET_BINARY" ./sim-testnet
go version -m "$SIM_TESTNET_BINARY"
sha256sum "$SIM_TESTNET_BINARY"

"$SIM_TESTNET_BINARY" doctor \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --format json

"$SIM_TESTNET_BINARY" plan \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --format json > /home/by/urnetwork/temp/ur-subnet-testnet-plan.json
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
It independently sizes validator bootstrap stake rather than reusing the demand-
deposit cap: the reserve validator targets 65% of registered alpha, the second
validator receives 1,000 alpha, and a finalized barrier requires the reserve to
retain at least 60%. Plan/apply read and recheck the runtime transfer floor,
price, source balance, coldkey total, stored conviction lock, miner collateral,
registration set, and 2,000-alpha source remainder before any transfer is signed.
The transfer floor is runtime `DefaultMinTransfer` (not `InitialMinStake`). Its
value is resolved from `InitialMinTransfer` in the exact finalized block's
metadata after authenticating that block's Wasm hash, and must equal the public
manifest's `expected_default_min_transfer_rao`. The same value is embedded
immutably in the settlement vault. Sub-floor pool emission remains on its pool to accumulate;
sub-floor provider entitlements remain durable coldkey credit until an exact
runtime payment succeeds. Public history and scenario evidence distinguish
logical `Claimed`, `ClaimPaymentDeferred`, and measured `ClaimPaid` events.

## Approved convergence and persistent launch

Only after explicit approval of the generated hash:

```bash
"$SIM_TESTNET_BINARY" launch \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
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
value to the neuron precompile, matching the deduction semantics retained by
runtime 454.
Contract registrations supply that full ceiling and atomically refund the
unburned difference.
Transactions are journaled through intent, signed bytes and nonce,
broadcast, inclusion, finality and postcondition. An interrupted convergence is
continued with `resume` and the same approval hash.

## Release evidence

```bash
"$SIM_TESTNET_BINARY" scenario --name release-1.0 \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH

"$SIM_TESTNET_BINARY" scenario --name production-soak \
  --config sim-testnet/testnet.yml \
  --state-dir "$SIM_TESTNET_STATE_DIR" \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH

"$SIM_TESTNET_BINARY" inspect --config sim-testnet/testnet.yml --state-dir "$SIM_TESTNET_STATE_DIR" --format json
"$SIM_TESTNET_BINARY" analyze --config sim-testnet/testnet.yml --state-dir "$SIM_TESTNET_STATE_DIR" --format json
```

The first campaign proves five consecutive 300-block accelerated epochs while the full
adversarial actor set remains active, including promotion/demotion at the 200-slot
head boundary. The second schedules the testnet-only 360-block/approximately-72-minute policy
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
