# `sim-testnet` release 1.0 harness

`sim-testnet` is the only supported release-1.0 testnet installer and integration
test. It converges an **existing** Bittensor testnet subnet, deploys the reviewed
reserve/vault/coordinator contract set, provisions two operators, 1,000 real
miner identities in 20 production swarms, two validators, 202 independently
keyed four-client head-candidate fleets, and 192 long-tail miners, then leaves
the topology running for inspection and named scenarios. Exactly 200 of the 202
candidate fleets receive native head slots. Each fleet stays within one operator
and whole fleets are balanced across
operators, so the affiliated-validator self-dealing mask leaves an independent
head and pool instead of contaminating every head UID.

It never creates a subnet. Every write is bounded by an approved, content-hashed
plan. `doctor`, `plan`, `status`, `inspect` and `analyze` are read-only. `setup`,
`launch`, `resume`, `scenario` and `retire` are dry-runs unless both `--apply`
and the exact `--plan-hash` are supplied.

## Pre-launch approval

The testnet inputs are stored under testnet-prefixed keys in
`../vault/main/st.yml`. Do not run `setup --apply` or `launch --apply` until
`doctor` is green and the printed plan hash and maximum spend have been reviewed.
Loading the configuration does not itself write to either chain.

Required `testnet-` keys:

| key | required value |
|---|---|
| `testnet-wallet` | A portable `vault-wallet:relative/path` to a standard encrypted Bittensor wallet directory, or the legacy signer forms `env:VARIABLE` and `file:/absolute/owner-only/path`. The coldkey is decrypted only in memory and must match `coldkeypub.txt`; its public default hotkey is also identity-checked. |
| `testnet-wallet-password` | A contained, non-symlink `vault-file:relative/path` to the encrypted wallet password. On execution hosts it must be owner-readable with no group/other permission bits (for example `chmod 600 vault/subtensor/testnet_wallet.password`). It is never accepted as a CLI flag or emitted in evidence. |
| `testnet-netuid` | The existing nonzero netuid owned by that wallet. |
| `testnet-spending-limit-tao-rao` | Maximum total testTAO outflow, as an integer number of rao. |
| `testnet-spending-limit-alpha-rao` | Maximum existing subnet-alpha transferred into release roles, as integer rao. The wallet must already control a registered staking hotkey with enough transferable alpha after conviction-lock and miner-collateral restrictions. The release profile reserves 20,000 alpha for demand custody plus reserve/independent validator bootstrap. |
| `testnet-spending-limit-evm-gas-wei` | Maximum aggregate EVM gas funding/use, as a canonical nonnegative decimal integer in wei. Quote values above `uint64` in YAML; the release profile uses `"100000000000000000000"` (100 testTAO). |
| `testnet-operator-api-origins` | Exactly two distinct bare `http(s)://host[:port]` origins, in NO 1/NO 2 order. Each must externally route to the corresponding API port and expose `/status`, `/verify/*`, `/sn/artifact*`, and `/sn/evidence*`. Launch verifies the signed content and history through these origins before publishing a portable manifest. |

The checked-in testnet governance value is `single-owner`; the harness generates
a dedicated capped testnet owner and a separate guardian. Unprefixed values are
mainnet-only and retain `safe-2-of-3`; `sim-testnet` refuses to resolve them.

`testnet-authority` remains the private fallback and must resolve
`sim-testnet:9944` from any execution host. The primary profile currently sets
both `public_substrate_rpc_override` and `public_evm_rpc_override`, so operational
reads, writes and workload proxies use the official public testnet services while
the private archive syncs. The fields are a pair: set both typed URLs or neither.
Removing both selects the private authority without changing vault data. The
lightnode profile deliberately omits them and therefore exercises private
fallback routing.

Public override mode is testnet acceptance mode, not independent infrastructure
proof. The official service is shared and rate limited, and its operational and
postcondition routes may be the same backend. `doctor` reports that distinction
as non-hard, and every postcondition/public manifest records
`independent_rpc=false`. Current-state checks, bounded release-window event reads
and testnet transactions may proceed; archive/history stress, high-rate campaigns
and the final mainnet-promotion soak must be repeated against the synced private
node plus a physically independent observer. Evidence uses the existing shared
`server/blob` MinIO configuration and bucket; no second object store is started.
MinIO and Subtensor are the only external shared services.

Runtime 452 distinguishes atomic alpha transfers (`TransferToggle`, managed by
`sudo_set_toggle_transfer`) from the one-time trading/emission activation
(`SubtokenEnabled`, managed by the subnet owner's `start_call`). The harness
checks these as distinct storage postconditions.

Plan schema v5 also keeps demand deposits and validator stake economically
separate. The campaign deposit cap does not size validators. At one finalized
checkpoint the harness reads the runtime transfer floor and alpha price, every
registered hotkey's stake, the source position, coldkey-wide stake, stored
conviction lock, and position/coldkey miner collateral. It then allocates the
reserve validator to a 65% target share, allocates 1,000 alpha to the independent
validator, requires the reserve to remain above 60%, and retains at least 2,000
alpha at the source. The exact amounts are approval-bound and rechecked against
live price, registration, lock, collateral, majority, and remainder immediately
before signing. A changed or unavailable constraint stops without broadcasting.
Runtime 452 stores each coldkey's entitlement as a `SafeFloat` share even though
`transfer_stake_and_hotkey` conserves the exact integer amount in the hotkey
aggregate. `getStake` may consequently floor the destination entitlement by one
rao. Plan schema v8 binds that maximum shortfall explicitly, adds one rao to
fresh bootstrap allocations, and sizes reserve majority from the minimum credit.
Recovery verifies the destination at the transaction's parent and inclusion
blocks; it never derives a pre-state from an already-mutated live balance. A
finalized v5-v7 transfer that stopped at the old exact-balance observer is locally
reconciled and may execute only one separately budgeted runtime-minimum repair,
never the campaign allocation a second time.

If later emissions dilute an already verified reserve below its 65% target, a
revision preserves the bootstrap transfer and appends a fixed repair tranche.
The tranche is capped at 3,000 alpha and by the cumulative vault alpha ceiling;
it is not resized from the moving emission snapshot between review and apply.
Planning fails unless that fixed amount can restore 65%. Immediately before
signing, the harness rechecks price, transferable source capacity, the retained
source position, and the full live registered-alpha composition at 65%; the
postcondition proves the same share at the finalized transaction block. A
separate 60% barrier then protects the remainder of setup from later dilution.

Generation-2 fleet refresh intentionally consumes generation-1 mirror and
binding live state. Resume accepts an older receipt historically only when the
append-only journal contains its exact later generation-1 install/convergence
batch and the same-range generation-2 refresh, with ordered operational and
comparison checkpoints. The original mirror or binding is replayed at its
recorded EVM block; an adjacent or partial batch cannot authorize it. Challenger
fleets are not refreshed and therefore retain ordinary live revalidation.
Carried-plan preflight groups the resulting historical block headers and
block-pinned mirror/binding calls into at-most-50-element JSON-RPC batches. This
changes only transport cardinality: every response is still matched to its
exact action, receipt hash, recorded height, canonical hash and observed state.
Private mode repeats the batches through the independent observer; public
override mode requires identical detached comparison evidence. The temporary
receipt-keyed audit cache is populated only after the complete batch succeeds
and is discarded when that preflight returns, so a timeout, partial response or
adjacent receipt cannot suppress ordinary verification.

Two authenticated atomic-alias receipt formats exist. Current aliases name the
exact source batch receipt and clone its finalized checkpoints. The first five
migration fleets instead recorded separate live mirror/binding reads after their
batch. Resume recognizes that old format only when all source metadata is absent,
the receipt is strictly ordered after the exact install and before the exact
refresh in both journal and checkpoint domains, and its original state replays at
the recorded block. Partial metadata or differing observer formats fail closed.

Demand custody crosses two runtime share pools: a same-coldkey `moveStake` to
the reserve hotkey and a `transferStake` to the immutable sink coldkey. Runtime
452 may floor each destination entitlement by one rao, so every reserve call
stages exactly two allowance rao and requires the final sink delta to remain in
`[principal, principal+2]`. The plan binds the number of reserve calls and this
per-call allowance in schema v9. Schema v8 remains byte-for-byte authenticatable
as a revision ancestor; its meaning is not strengthened in place. Revisions
retain every verified repair in cumulative spend and add only one runtime-minimum
top-up if conservative verified credit is below the stricter absolute campaign
requirement. Schema v10 additionally requires a duplicate-conviction
reconciliation to retain the exact authenticated original action intent. A
later gas-ceiling refresh or custody-repair dependency may gate new work, but
cannot turn that already-finalized one-shot action into a new intent.
Schema v11 retains v10 as authenticated history and binds `config.render` and
`topology.launch` to the exact approved config and policy hashes. Rendering also
writes `runtime-config-manifest.json`, whose sorted inventory, modes and SHA-256
digests cover every immutable operator, miner, validator, swarm and relayer
input. Launch fails closed on a missing, changed, additional or symlinked static
file.

An already-live immutable custody generation is never redeployed merely because
the coordinator changes. A repeated UUPS revision binds the exact next deployer
nonce and CREATE address, the finalized ERC-1967 implementation slot and active
runtime, every prior full runtime hash, and normalized executable hashes for the
reserve, vault, and precompile probe. Normalization removes only constructor
immutables and Solidity metadata; any executable custody drift fails closed.
The new implementation is additive, while the reserve/vault/proxy addresses and
their historical evidence remain unchanged.

Runtime 452 also raises a subnet's burn after successful registration. The
release plan therefore reserves at most `100000000` rao per registration and
binds that same ceiling into every native `register_limit` and EVM
`registerLimit` action. EVM callers are funded at their SS58 mirrors and pass
zero value to the neuron precompile; the runtime deducts the burn from the
caller mirror. Contract registrations supply the full ceiling and return the
unburned surplus, so an in-flight price increase cannot produce an underfunded
call below the approved cap.

Plan schema v5 binds every EVM transaction in two dimensions:
`maximum_gas_units` and `maximum_fee_per_gas_wei`. The checked-in fee ceiling is
100 gwei. Fixed setup unit limits are derived from the locked Foundry gas report
and include the manager's 20% plus 25,000-unit live-estimate margin. Signer
funding first covers the exact sum of that signer's explicit action ceilings;
only the remaining campaign allowance is weighted across keeper, deposit, root,
and claim-relayer roles. The aggregate campaign ceiling uses an arbitrary-size
canonical decimal so it cannot wrap or stop at the former `uint64` limit of
approximately 18.45 testTAO. Immediately before signing, the manager rejects a
fee spike, padded gas growth, aggregate mismatch, or value-plus-gas balance
shortfall without persisting or broadcasting transaction bytes.

## Host prerequisites

- Linux amd64, Go 1.26.x, Git, and a running user systemd manager.
- Both `net.core.rmem_max` and `net.core.wmem_max` at least 7 MiB, matching
  quic-go's release socket requirement. `doctor` fails closed below that floor;
  on Linux set a 16 MiB margin before launch with
  `sudo sysctl -w net.core.rmem_max=16777216 net.core.wmem_max=16777216`.
- At least 20 GiB free on the simulator state filesystem. Immediately before a
  launch/resume can construct a chain-capable executor, the harness also binds
  every required loopback process port and rejects any unrelated or stale listener.
- Docker with direct permission for the invoking user or passwordless `sudo -n
  docker`. The harness prefers direct access and never opens an interactive sudo
  prompt. One isolated PostgreSQL 18 and Redis 8 pair is created per operator from the exact digests in
  `deploy/testnet/release.lock.yml`. Their locale, database initialization,
  connection capacity, Redis threading, and persistence settings mirror
  `server/local`; they never use shared PG or Redis services. PostgreSQL data
  volumes and containers carry the same complete release/config hash, and stale
  or unlabelled volumes are rejected instead of silently reusing old init hooks.
- The locked `sn`, `server`, `vault`, platform `config`, `connect`, `sdk`, `glog`,
  `goidenticons`, `proxy`, `userwireguard`, and `xops` repositories checked out
  beneath one parent. Repository discovery uses Go module identity plus required
  resource files; `--sn-repo`, `--server-repo`, `--vault-repo`, and
  `--platform-config-repo` are available when the layout differs. Both executable
  Go sources and the non-secret operator config tree are content-locked.
- Network reachability to the selected operational Substrate/EVM pair, public
  comparison endpoints, and existing MinIO service. Private fallback additionally
  requires the overlay gateway.
- Foundry 1.7.1 only for developer rebuild/review. A launch embeds locked bytecode
  and never compiles Solidity at runtime.

On this checkout Foundry is installed at `/home/by/.foundry/bin`.

## Build and read-only preflight

From the `sn` repository:

```bash
go build -trimpath -o build/sim-testnet ./sim-testnet
go build -trimpath -o build/sim-testnet-light ./sim-testnet

./build/sim-testnet doctor \
  --config sim-testnet/testnet.yml \
  --format json

# Same release checks and topology, isolated state/artifact names, and the
# side-by-side warp-synced lightnode RPC selected by the executable name.
./build/sim-testnet-light doctor --format json

./build/sim-testnet plan \
  --config sim-testnet/testnet.yml \
  --format json > /tmp/ur-subnet-testnet-plan.json
```

`doctor` checks the release lock, repository source hashes, wallet proof,
ownership, balances, budget, runtime/genesis/chain identity, metadata and call
shapes, the exact finalized runtime Wasm hash, finalized subnet-token/emission
activation, recent historical EVM state, gateway methods, the signed finalized-head
lag bound, a canonical common checkpoint, precompiles, MinIO's exact HTTP live
endpoint, the Docker daemon and systemd. Private mode additionally hard-requires
connected consensus peers and distinct physical operational/observation backends;
public override mode records those unavailable assurances without overstating them.
`plan` repeats those gates, reads finalized setup facts, and prints every intended
action, dependency, maximum spend and the canonical `plan_hash`. That approval
hash binds the complete release lock, harness/public/hyperparameter manifests and
all non-secret values resolved from the vault, not only their YAML references. The
signed policy has its own canonical hash. Neither command submits a transaction or
extrinsic.

Runtime-452 transfer economics are approval-bound explicitly: the exact
finalized Wasm hash must match the release lock, and that block's
`SubtensorModule.InitialMinTransfer` metadata constant—the value used by the
runtime's internal `DefaultMinTransfer` function—must equal
`public.yml:chain.expected_default_min_transfer_rao`. Every planned alpha
transfer is sized from it at the same finalized snapshot, and the value is an
immutable settlement-vault constructor/runtime word. Historical v5-v10 plans
keep their authenticated wire semantics; current approvals use plan schema v11.

The public-chain integration probes are opt-in:

```bash
SIM_TESTNET_LIVE_WALLET=1 go test ./sim-testnet -run TestLiveVaultWalletResolution -v
SIM_TESTNET_LIVE_READ=1 go test ./sim-testnet -run TestLiveBalanceProbe -v
```

The alpha-bootstrap integration test additionally requires its exact
`SIM_TESTNET_STAKE_ALPHA` confirmation string. It is idempotent once the target
alpha position exists and otherwise journals activation and staking before
checking finalized storage. It is not part of the ordinary unit-test suite.

## Approved setup and launch

Use the exact hash from the reviewed plan. A changed config, resolved vault input,
policy, release lock, role derivation, source checkout, artifact, runtime fact, or
persisted plan fails closed. Every apply reruns `doctor` and rechecks finalized
economic facts against the exact unverified remainder. Docker dependencies and
all release binaries are preflighted before a transaction-capable executor opens.

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

Head-fleet setup is bounded but not artificially serialized. All ten
independently signed Substrate commitment writes run concurrently inside an
explicit ten-fleet plan group; every write retains its own fee ceiling, raw
transaction, journal lineage, finalized storage proof, and idempotent recovery.
The testnet-only `STFleetBatcher` then installs or refreshes that group in one
atomic EVM transaction. It accepts at most ten fleets with four members each,
only from the immutable original commitment oracle, rejects duplicate fleet or
member identities, and exercises the coordinator's normal dual-signature and
client-revocation checks. The owner activates it through the ordinary
future-epoch oracle schedule and restores the original oracle before topology
launch. Maximum-size Foundry calls use 9,535,582 gas for install and 9,080,115
for refresh, below the approval-bound 18,000,000 and 24,000,000 ceilings.
Existing verified per-member writes remain charged verbatim across a formal
plan revision; absent or partial batches fail closed instead of being inferred.
A later release revision authenticates the helper's extra deployer nonce and
runtime before advancing the CREATE boundary. Any verified EVM action replaced
by that revision moves once into cumulative superseded gas, so acceleration
cannot erase historical spend from the approval envelope.

Every historical coordinator read in a batch remains pinned to one exact EVM
block, but the HTTP transport groups at most 50 `eth_call` elements, matching
the public endpoint's enforced limit. The individual mirror/member plan actions
then derive their receipts from the authenticated batch receipt and their
canonical signed artifacts; they do not repeat the batch's live RPC surface.
Resume still revalidates the source batch on chain before any new mutation.

Generation-2 refreshes use the same rule on both execution and replay. Before a
fresh atomic refresh is signed, all 40 predecessor count/record pairs are read
as two HTTP batches of 50 and 30 elements. A carried 10-fleet refresh checks its
ten mirrors, 40 version counts, 40 truncated predecessors, 40 successors and ten
fleet cardinalities as three batches of 50, 50 and 40 elements. Oracle routing's
five independent fields share one block-pinned request. Batching changes only
transport: every returned field is decoded at its original position and checked
against the signed evidence, deterministic manifest member and exact selected
block. Provider/context failures remain operational failures and are never
reported as evidence that a miner supplied a dishonest generation.

At the pinned 12-second public-testnet cadence, this changes head-fleet setup
from a many-hour serialized transaction chain to roughly 1--3 hours, including
boundary alignment: 400 native commitments run in ten-wide waves, 40 EVM
batches replace 1,600 per-member install/refresh calls, historical reads use
bounded RPC batches, and two future-epoch oracle handoffs remain.
The five accelerated acceptance epochs require about five hours. `release-1.0`
first discards the post-preparation partial accelerated epoch and waits through
the final 150-block settlement offset. The three 360-block production UR blocks
retain about 3.6 hours of complete chain observation after a second
post-preparation partial epoch, plus the final 180-block settlement window. The
combined live acceptance path is expected to take roughly 12--15 hours including
future-effective transition and boundary alignment. Those protocol-time gates
are not shortened or simulated off-chain.

There is no separate one-hour M1 wait before that five-epoch interval. Launch
hands directly to `release-1.0`; its first complete reconciled epoch is both the
M1 end-to-end proof and epoch 1 of M2. On a clean release marker,
`production-soak` starts immediately and schedules its future-effective policy
without an operator pause. The five-epoch campaign keeps the deployed
300-block accelerated cadence. Its future-effective transition to the
release-locked 360-block acceptance policy is a new, explicitly hashed and
journaled approval lineage.

Host reboot is an intentional stop boundary. The supervisor unit is started but
never enabled, managed PostgreSQL/Redis containers use Docker restart policy
`no`, and loginctl linger is not required. After a reboot, run `resume` explicitly;
it re-runs doctor and reconciles the journal and finalized chain before starting
any dependency or process. Provisioning helpers also persist PID, process-group,
kernel start-time, executable-hash and argv-hash ownership. If the parent exits
abnormally, resume reaps only an exact orphan identity and never a reused PID.
Topology readiness is deliberately plan-generation-local: a revised plan
authenticates the ancestor launch receipt as history but cannot use it to satisfy
the new plan's launch dependency. Only after the current binaries, supervisor
generation, children, fresh validator proofs and fenced logs pass readiness does
the current plan record `topology.launch`. Same-plan resume likewise rechecks the
new live generation without duplicating its terminal journal entry.

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

# Recommended uninterrupted M2 -> M3 release-candidate campaign. It adopts
# only exact signed clean phase markers and runs the first missing phase.
./build/sim-testnet scenario --name release-candidate \
  --config sim-testnet/testnet.yml \
  --apply --plan-hash 0xREVIEWED_PLAN_HASH
```

`release-candidate` is a resumable orchestration name, not a weaker scenario.
It runs `release-1.0`, independently reloads and authenticates the signed result,
complete marker and every named evidence file, then starts `production-soak`
without an operator handoff gap. If M2 is already valid it runs only M3; if both
are valid it performs no scenario action. A failed or unsigned M3 is never
adopted: the next invocation retains M2 and starts a fresh three-epoch soak.

`release-1.0` requires five accelerated epochs, real two-NO verification,
independently applied CRv4 vectors and self masks, isolated deposits and conviction,
public roots, claims from both pools, cryptographically reconstructed head bindings,
and a fresh, independently reconstructed and signature-verified validator path
proof from every validator/operator pair in every required epoch. The live custody
actor also submits a mutated real payout leaf by read-only `eth_call` against both
NO entitlements and requires the exact invalid-proof error with no state change.
It further requires
a nonzero native head weight, a real promotion/demotion transition across the
200-slot boundary, exact selected/rejected native reward channels, one-tier payout
exclusion, actual `ClaimPaid` settlement (distinct from accepted/deferred claim
credit), exact signed-policy max-weight-cap compliance, reserve principal plus
auto-compounded yield, process fault recovery and both exact vault conservation
identities (`captured = paid + escrow`, `escrow = pending + outstanding`).
Each validator's immutable intent includes all 202 canonical rational fleet
scores computed from its own trails. The simulator independently ranks those
scores and reconstructs that validator's exact 200 selected and two rejected
UIDs; the two validators are allowed to reach different boundaries from their
own evidence. Every unmasked UID selected by a validator must have positive
weight in that validator's applied vector, while each UID it rejects must have
zero weight there. A positive weight for any UID outside that validator's
selected set and the two live pool UIDs is a hard failure. Finalized native
reward vectors must pay every unanimously selected fleet and pay no fleet
rejected by every validator; a disputed boundary is left to Yuma's stake-weighted
median and clipping and is reported from the chain. The same checks cover every
applied intent created after the acceptance baseline, so a later valid decision
cannot erase an intermediate boundary or weight violation. During M2 a private,
testnet-only operator API filter withholds fleet 4 from validator 1 but not
validator 2. A common native epoch must show validator 1 rejecting that UID at
zero and positively weighting its replacement while validator 2 makes the
opposite decision; a later common epoch must prove restoration. The filter is
mode- and deployment-confined, atomically persisted, ledger-recovered, and is
restored immediately after authenticated applied-decision evidence proves the
divergence. Native head windows rotate atomically, same-epoch retries reuse the
same evidence, and the terminal interval budgets a complete fresh trail and a
strict recovery fold. All 808 providers
bound to
the 202 live fleet UIDs—including rejected but still registered fleets—must be
absent from operator payout leaves, preventing double pay until actual
deregistration returns a fleet to its pool.
The runner snapshots finalized contract geometry after preparation, discards
that containing epoch, accepts only the next five complete epochs, then waits
through terminal finalization. Its signed result binds the baseline observation,
exact start/end/terminal blocks and terminal status for both operator positions;
the campaign verifier reconstructs those boundaries independently. Every M2
fault must also trigger and restore inside the accepted five-epoch interval.
`production-soak` schedules the testnet-only 360-block (approximately 72-minute)
policy and immunity period, deliberately under-deposits one operator and proves
that both validators zero its pool until an exact later deposit recovers it,
rotates each operator verification key while retaining old proof verification,
runs three consecutive fully observed production epochs, and genuinely restarts
(new PID, healthy replacement) every operator service, miner/claim daemon and
validator without overlapping faults. Mainnet remains locked to the whitepaper's
separately reviewed 50,400-block/seven-day cadence.

While the topology is live, one supervised non-faulted loopback EVM egress owns
the configured upstream quota. Workloads reach it through their faultable proxy;
scenario writers, observers, adversarial actors and concurrent
`status`/`inspect`/`analyze` commands reach it directly. A live supervisor may
not fall back around a missing or unhealthy gate. Before launch and after stop,
read-only commands use the canonical configured endpoint.

## Continuous adversarial campaign

The `release-1.0` and `production-soak` scenarios always load the release-locked
[`adversarial-matrix-v1.json`](../docs/spec/adversarial-matrix-v1.json). Its 56
rows cover Yuma/YC3 cabals, stale and reveal-following weight copies, liquid-alpha
bond timing and validator-permit churn, all eight published Subtensor security
advisories, historical runtime atomicity/accounting/identity/resource failures,
subnet reserve/registration/liquidity/eviction pressure, hidden root-basket rewards,
proxy-stake MEV/slippage, four security-relevant Bittensor SDK/transport issue
families (missing signatures, finality-era expiry, plaintext unauthenticated
transport, and constant body hashes), runtime/precompile drift, identity
and proxy churn, commitment-field parser confusion, operator/verification abuse, artifact equivocation, contract
authorization/custody, settlement, runtime transfer-floor/durable-credit, and
dependency failures.

Seven attributed actors start before the happy path and remain active until
after its final reconciliation:

- bounded operator API and real `/verify` pressure, including simultaneous
  identical EXTENDs, replays, invalid signatures, poison-shape comparisons, and
  per-source vpk rotation;
- independent private/public finalized-RPC agreement, observed runtime-spec and
  transaction-version identity, plus common-height subnet UID,
  spot/moving-price, and TAO/alpha-reserve reads;
- artifact fetch/reconstruction/tamper pressure and fleet identity-generation
  mutations; and
- deterministic consensus, liquid-alpha, custody, unit/domain, rounding,
  root-index, and upstream reserve-flow emulation.

Every fifth sample is a control and the other four are adversarial. Release
configuration requires at least 100 non-skipped samples per actor, both phases,
zero unexpected actor errors, p99 latency at most 15 seconds, attack/control p95
latency no worse than 20×, at most eight
operator requests/second and two RPC requests/second. Expected 400/409/429
rejections are recorded separately from faults. Campaign evidence includes the
matrix hash, lifecycle overlap, request/in-flight totals, latency distributions,
per-vector required and actually sampled metric names, and full-run minima/maxima
for on-chain numeric sentinels in
`runs/<deployment-id>/runs/<run-id>/adversaries.json`.

The release schedules non-overlapping outages for every simulator-owned
PostgreSQL/Redis pair and the simulator-owned loopback Subtensor RPC proxy, then
rolls every persistent process. It records exact downstream impact windows and
requires healthy replacement PIDs. The external shared Subtensor and MinIO
services are not destructively faulted; MinIO remains under continuous
history/reconstruction/tamper pressure.

Every run also writes `anomalies.json`. It is built from failed assertions,
deployment warnings, component errors, unresolved claims, supervisor health and
restart deltas, incomplete faults, and adversary actor/vector failures. Scheduled
restart faults are reconciled exactly; any excess or missing restart is an
anomaly. A release run passes only when this append-only ledger is `clean` with
zero entries. Failed runs leave entries `open` for the root-cause, minimized
reproduction, regression, and clean-rerun evidence required by the mainnet
readiness dossier.

Shared-testnet safety is structural: live actors touch only loopback operator
endpoints, our deployment/netuid identities, and capped read RPC. Chain-wide
flooding, proxy takeover, cooldown bypass, and global state-bloat exploits run
only against the exact pinned local runtime; their live actors are read-only
sentinels or bounded state-machine emulators. Any unexplained error, drift,
latency breach, missing sample, process restart, or happy-path discrepancy fails
the scenario and remains a root-cause investigation item—it is never waived as
“adversarial noise.”

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

These commands are safe before launch approval and perform no testnet writes:

```bash
go test ./...
go test -race ./crv4 ./miner/... ./protocol ./sim-testnet ./validator

PATH=/home/by/.foundry/bin:$PATH \
  bash -c 'cd evm && forge fmt --check && forge build --sizes && forge test --summary'

cd ../server
WARP_ENV=main \
BRINGYOUR_MINIO_HOSTNAME=172.28.208.177 \
SIM_TESTNET_LIVE_BLOB=1 \
go test . -run '^TestLiveBlobStoreContentAddressedCanary$' -count=1
```

The opt-in blob test writes one fixed content-addressed canary, then reads and
lists it through the real server/blob service account. Repeated runs overwrite
the same bytes at the same key; ordinary tests never access external storage.

Database-backed server tests additionally need the hermetic PostgreSQL/Redis/vault
profile that `launch` materializes after verified contract addresses exist. Running
them with only `RUN_SERVER_DB_TESTS=1` and no `WARP_ENV` is an expected fail-closed
configuration error, not a database-health result. The release campaign runs them
against both rendered managed-operator databases before the final go/no-go decision.
