# `sim-testnet` release 1.0 harness

`sim-testnet` is the only supported release-1.0 testnet installer and integration
test. It converges an **existing** Bittensor testnet subnet, deploys the reviewed
reserve/vault/coordinator contract set, provisions two operators, eight miners,
two validators, two independently keyed three-client head fleets and two tail
miners, then leaves the topology running for inspection and named scenarios.

It never creates a subnet. Every write is bounded by an approved, content-hashed
plan. `doctor`, `plan`, `status`, `inspect` and `analyze` are read-only. `setup`,
`launch`, `resume`, `scenario` and `retire` are dry-runs unless both `--apply`
and the exact `--plan-hash` are supplied.

## Pre-launch pause

The repository intentionally ships with empty/zero launch authority in
`../vault/main/st.yml`. Do not run `setup --apply` or `launch --apply` until the
values below are filled, `doctor` is green, and the printed plan hash and maximum
spend have been reviewed. Filling the file does not itself write to either chain.

Required `testnet-` keys:

| key | required value |
|---|---|
| `testnet-wallet` | The subnet-owner signer as a Substrate secret URI/mnemonic, `env:VARIABLE`, or `file:/absolute/owner-only/path`. Secret files must be regular, non-symlink, absolute, nonempty, and have no group/other permission bits. It is never accepted as a CLI flag or emitted in evidence. |
| `testnet-netuid` | The existing nonzero netuid owned by that wallet. |
| `testnet-spending-limit-tao-rao` | Maximum total testTAO outflow, as an integer number of rao. |
| `testnet-spending-limit-alpha-rao` | Maximum existing subnet-alpha transferred into release roles, as integer rao. The wallet must already control a staking hotkey with at least this topology's planned alpha. |
| `testnet-spending-limit-evm-gas-wei` | Maximum aggregate EVM gas funding/use, as integer wei. |
| `testnet-operator-api-origins` | Exactly two distinct bare `http(s)://host[:port]` origins, in NO 1/NO 2 order. Each must externally route to the corresponding API port and expose `/status`, `/verify/*`, `/sn/artifact*`, and `/sn/evidence*`. Launch verifies the signed content and history through these origins before publishing a portable manifest. |

The checked-in testnet governance value is `single-owner`; the harness generates
a dedicated capped testnet owner and a separate guardian. Unprefixed values are
mainnet-only and retain `safe-2-of-3`; `sim-testnet` refuses to resolve them.

`testnet-authority` uses `BRINGYOUR_SUBTENSOR_HOSTNAME` and must reach the deployed
runtime-447 RPC gateway on port 9944 from the execution host. Evidence uses the
existing `server/blob` MinIO configuration and bucket; no second object store is
started.

## Host prerequisites

- Linux amd64, Go 1.26.x, Git, and a running user systemd manager.
- Docker with permission for the invoking user. PostgreSQL 16.4 and Redis 7.4
  containers are created from the exact digests in `deploy/testnet/release.lock.yml`.
- The locked `sn`, `server`, `vault`, platform `config`, `connect`, `sdk`, `glog`,
  `goidenticons`, `proxy`, `userwireguard`, and `xops` repositories checked out
  beneath one parent. Repository discovery uses Go module identity plus required
  resource files; `--sn-repo`, `--server-repo`, `--vault-repo`, and
  `--platform-config-repo` are available when the layout differs. Both executable
  Go sources and the non-secret operator config tree are content-locked.
- Network reachability to the private Substrate/EVM gateway, public comparison
  endpoints, and existing MinIO service.
- Foundry 1.7.1 only for developer rebuild/review. A launch embeds locked bytecode
  and never compiles Solidity at runtime.

On this checkout Foundry is installed at `/home/by/.foundry/bin`. Docker is not
currently installed and requires host-administrator action before `doctor` can pass.

## Build and read-only preflight

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

`doctor` checks the release lock, repository source hashes, wallet proof,
ownership, balances, budget, runtime/genesis/chain identity, metadata and call
shapes, gateway methods, precompiles, MinIO, Docker and systemd. `plan` repeats
those gates, reads finalized setup facts, and prints every intended action,
dependency, maximum spend and the canonical `plan_hash`. Neither command submits
a transaction or extrinsic.

## Approved setup and launch

Use the exact hash from the reviewed plan. A changed config, policy, role
derivation, source checkout, artifact, runtime fact, or persisted plan fails
closed.

```bash
# Optional: converge chain/contracts/config without starting services.
./build/sim-testnet setup \
  --config sim-testnet/testnet.yml \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH

# Converge setup, start the persistent topology, run the mandatory M0B
# precompile-conformance gate, prove readiness, and run smoke.
./build/sim-testnet launch \
  --config sim-testnet/testnet.yml \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH \
  --detach
```

The journal records intent, signed bytes/nonce, broadcast, inclusion, finality and
postcondition. If the command is interrupted, use the same approval:

```bash
./build/sim-testnet resume \
  --config sim-testnet/testnet.yml \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH \
  --detach
```

Before smoke, `launch` automatically runs the named `precompile-conformance`
scenario. It finalized-reads and replaces/restores a native commitment, deploys
the locked disposable probe, checks Blake2/Ed25519/sr25519/metagraph/neuron/staking,
converts only the plan-approved TAO dust into alpha, performs an exact two-hotkey
round trip, observes a take-zero dividend cycle, and transfers every attributable
alpha unit to a controlled provider coldkey. Each phase has a separate transaction
intent, ceiling, finalized receipt, postcondition and signed evidence record.

## Observe and run release campaigns

```bash
./build/sim-testnet status  --config sim-testnet/testnet.yml --format json
./build/sim-testnet inspect --config sim-testnet/testnet.yml --format json
./build/sim-testnet analyze --config sim-testnet/testnet.yml --format json
./build/sim-testnet tail    --config sim-testnet/testnet.yml

./build/sim-testnet scenario --name precompile-conformance \
  --config sim-testnet/testnet.yml \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH

./build/sim-testnet scenario --name release-1.0 \
  --config sim-testnet/testnet.yml \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH

./build/sim-testnet scenario --name production-soak \
  --config sim-testnet/testnet.yml \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH
```

`release-1.0` requires 20 accelerated epochs, real two-NO verification,
independently applied CRv4 vectors and self masks, isolated deposits and conviction,
public roots, claims from both pools, cryptographically reconstructed head bindings,
a nonzero native head weight, reserve principal plus auto-compounded yield, process
fault recovery and exact rao conservation. `production-soak` schedules the canonical
50,400-block policy and immunity period, rotates each operator verification key while
retaining old proof verification, runs two complete production epochs, and genuinely
restarts (new PID, healthy replacement) every operator service, miner/claim daemon
and validator without overlapping faults.

For independent inspection, use any signed deployment-manifest evidence URL from
`runs/ur-subnet-testnet-v1/public/deployment-manifest.locators.json` on a clean
compatible checkout:

```bash
./build/sim-testnet inspect \
  --config sim-testnet/testnet.yml \
  --manifest 'https://NO/sn/evidence?hash=sha256:...'

./build/sim-testnet analyze \
  --config sim-testnet/testnet.yml \
  --manifest 'https://NO/sn/evidence?hash=sha256:...'
```

## Stop and retire

`stop` terminates only local supervised processes; it preserves containers,
secrets, evidence and all chain state. Retirement is a separate future-effective,
hash-approved on-chain plan and is dry-run by default:

```bash
./build/sim-testnet stop --config sim-testnet/testnet.yml
./build/sim-testnet retire --config sim-testnet/testnet.yml --format json
./build/sim-testnet retire --config sim-testnet/testnet.yml \
  --apply --plan-hash 0xREVIEWED_RETIREMENT_PLAN_HASH
```

Retirement deactivates operator versions at the next epoch. It never deletes the
immutable vault, reserve, prior entitlements, claims, MinIO history, role store, or
local run evidence.

## Local verification

These commands are safe before the launch values are filled and perform no testnet
writes:

```bash
go test ./...
go test -race ./crv4 ./miner/... ./protocol ./sim-testnet ./validator

PATH=/home/by/.foundry/bin:$PATH \
  bash -c 'cd evm && forge fmt --check && forge build --sizes && forge test --summary'
```

Database-backed server tests additionally need the hermetic PostgreSQL/Redis/vault
profile that `launch` materializes. Their absence on a development shell is not
treated as live evidence; the release campaign must run them against both managed
operator databases before the final go/no-go decision.
