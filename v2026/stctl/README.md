# stctl — subtensor ops CLI for the UR subnet

Runtime operations + inspection for the `STSubnet` settlement contract
(PLAN.md §2), over the subtensor EVM json-rpc via the generated `sn/stabi`
bindings. Deployment itself stays in `forge script` (see `sn/evm`); stctl
covers everything after: initialize (for a proxy deployed uninitialized),
operator registration, deposits (buybacks), payout-root commits, finalize,
miner claims, and epoch/state inspection, plus the H160 → SS58 mirror
funding helper (PLAN.md §3.6).

## Build

```sh
make -C stctl build          # multi-arch, output in stctl/build/<os>/<arch>/stctl
# or, single-arch:
cd stctl && CGO_ENABLED=0 go build -ldflags "-X main.Version=dev" -o stctl .
```

## Usage

```
Usage:
    stctl deploy-status [--config=<path>] [-v...]
    stctl initialize --owner=<h160> --treasury_hotkey=<key> --reserve_hotkey=<key>
        [--guardian=<h160>] [--t_epoch=<blocks>] [--commit_window=<blocks>]
        [--trails_window=<blocks>] [--finalize_offset=<blocks>] [--self_coldkey=<key>]
        [--config=<path>] [-v...]
    stctl register-operator --no_id=<id> --coldkey=<key> --miner_hotkey=<key>
        [--config=<path>] [-v...]
    stctl deposit --no_id=<id> --alpha=<rao> [--push]
        [--config=<path>] [-v...]
    stctl commit-root --epoch=<e> --no_id=<id> --root=<hex32> [--off=<hex>]
        [--config=<path>] [-v...]
    stctl finalize --epoch=<e> [--config=<path>] [-v...]
    stctl claim-miner --epoch=<e> --no_id=<id> --coldkey=<key> --share_bps=<n> --proof=<proof>
        [--force] [--config=<path>] [-v...]
    stctl epoch [--config=<path>] [-v...]
    stctl state [--epoch=<e>] [--no_id=<id>] [--config=<path>] [-v...]
    stctl evm-address <h160>
```

Conventions:

- **Amounts are rao** (`1 α = 1e9 rao`); outputs render both, e.g.
  `1234567890 rao (1.234567890 α)`.
- **Account keys** (`--coldkey`, `--miner_hotkey`, `--treasury_hotkey`,
  `--reserve_hotkey`, `--self_coldkey`) accept an ss58 address (prefix 42)
  or raw 32-byte hex (0x-optional).
- **`--proof`** is a comma-separated list of 32-byte hex nodes; pass an empty
  string for a single-leaf tree.
- Writes print the tx hash, mined status, gas used, and decoded `STSubnet`
  events; reverts are re-executed as an `eth_call` to recover the reason
  where the endpoint supports it.

## Config

Default path `~/.urnetwork/stctl.yml` (`--config` overrides). When the file
is missing, `stctl deploy-status` prints this commented example:

```yaml
# stctl config (default path: ~/.urnetwork/stctl.yml)
#
# EVM json-rpc endpoints, tried in order until one answers.
# Testnet: https://test.chain.opentensor.ai (chain 945) — the v1 default (D28, testnet-first)
# Mainnet: https://lite.chain.opentensor.ai (chain 964) — the later Phase-E promotion
rpc_urls:
  - https://test.chain.opentensor.ai

# Asserted against eth_chainId before any call.
chain_id: 945

# STSubnet proxy address (from the forge script deploy).
contract_address: "0x0000000000000000000000000000000000000000"

# Subnet netuid; cross-checked against the contract's netuid().
netuid: 1

# Path to a hex-encoded 32-byte EVM private key (0x-optional).
# Only required for state-changing commands. Fund the key's H160 through its
# ss58 mirror ("stctl evm-address <h160>", then btcli wallet transfer).
key_file: ~/.urnetwork/stctl.key
```

Schema:

| field | type | meaning |
|---|---|---|
| `rpc_urls` | list of strings | EVM json-rpc endpoints, tried in order until one answers `eth_chainId` |
| `chain_id` | int | asserted against the endpoint (945 testnet — the v1 default; 964 mainnet for the Phase-E promotion) |
| `contract_address` | 0x-hex H160 | the STSubnet **proxy** address |
| `netuid` | int | subnet id; cross-checked against the contract |
| `key_file` | path | hex-encoded 32-byte EVM private key (0x-optional); reads work without it |

Unknown fields are rejected (typo protection).

## Command notes

- **`deploy-status`** — config + key + rpc + contract sanity: prints the
  signer H160 and its funding mirror, checks code at `contract_address`,
  cross-checks `netuid()`, warns when `selfColdkey() != mirror(proxy)`, and
  dumps owner/guardian/pause, the custody hotkeys (`treasuryHotkey` escrow +
  `reserveHotkey` buyback reserve), epoch params, `accountedStake`,
  `buybackTotal`, and the operator count.
- **`initialize`** — `initialize(netuid, owner, guardian, treasuryHotkey,
  reserveHotkey, tEpoch, commitWindow, trailsWindow, finalizeOffset,
  selfColdkey)` with netuid from the config. The normal path is the forge
  deploy script's atomic proxy-constructor initialize; this command
  completes a proxy deployed with empty initializer calldata. The
  `--reserve_hotkey` is the owner-validator hotkey every deposit is staked
  to (WHITEPAPER §7.4/D23) — set once, no setter, and pre-validated nonzero
  and ≠ `--treasury_hotkey` before sending. Window flags default to the
  Deploy.s.sol profile for the config's chain id (mainnet 964:
  50400/1200/7200/14400; otherwise 300/50/100/150); the trails window is a
  reserved dial for the deferred effort-bounty phase (gates nothing in v1).
- **`deposit`** — the contract's flow is **push-then-credit**
  (`evm/README.md` deviation 3): stake must sit on `treasuryHotkey` under
  the contract's mirror coldkey before `deposit(noId, alphaAmount)`
  attributes it. With `--push`, stctl first calls the StakingV2 precompile
  (`0x805`) `transferStake(mirror(proxy), treasuryHotkey, netuid, netuid,
  amount)` from the signer key — the signer's own mirror must hold that
  stake on `treasuryHotkey` — then calls `deposit()`. Without `--push` it
  assumes the push already happened (e.g. via btcli) and only credits.
  The contract mirror ss58 is always printed as the btcli funding target.
  Deposits are **buybacks** (WHITEPAPER §7.4/D23): the contract moves the
  full amount onto the locked `reserveHotkey` buyback reserve (watch the
  `BuybackReserved` event); pool payouts (`poolTotal`) are emission-only.
  Under v0.4/D25 the contract keeps **no DT ledger** — deposits are **conviction
  stake** recorded by the `Deposited` events; validators weight the pools
  `implied_usage × quality` off that log (the contract weighs nothing).
- **`commit-root`** — `commitOperator(e, noId, payoutRoot, off)`; build the
  root with `sn/merkle` (OZ double-hash `keccak256(bytes.concat(keccak256(
  abi.encode(bytes32 coldkey, uint256 shareBps))))`, sorted pairs).
- **`finalize`** — `finalizeEpoch(e)`; append-only and in-order, from
  `close(e) + finalizeOffsetBlocks`.
- **`claim-miner`** — pre-flight before sending (skippable with `--force`):
  epoch finalized, payout root committed, dedup key unclaimed, and the
  merkle proof verified locally against the committed root via `sn/merkle`.
- **`epoch` / `state`** — window deadlines are printed as block numbers with
  an ETA at 12 s/block. `epoch` is the last *rolled* epoch; `pendingEpoch`
  is chain-time (any time-gated tx rolls first). `state --epoch=<e>` shows
  finalized/close/windows, the cumulative `buybackTotal` +
  `reserveHotkey`, and per-NO
  `poolEmission`/`poolTotal`/`claimedMiner`/`carry`/commit root (per-NO deposits
  come from the `Deposited` event log, not a contract ledger — D25 dropped
  `DT`/`totalDT`).
- **`evm-address <h160>`** — prints the substrate mirror
  (`pubkey = blake2b_256("evm:" ‖ h160)`, ss58 prefix 42). Fund that ss58
  with btcli to fund the H160 on the EVM; the mapping is one-way.

## Testnet genesis crib (docs/LAUNCH.md — D28: testnet-first, mainnet later)

The full runbook is `docs/LAUNCH.md` (localnet rehearsal -> testnet dust probes ->
one scripted genesis window -> ramp -> Phase-E mainnet promotion). v1 endpoints: EVM
json-rpc `https://test.chain.opentensor.ai` (chain id **945**), substrate
`wss://test.finney.opentensor.ai:443`. Mainnet (964 /
`https://lite.chain.opentensor.ai` / `wss://entrypoint-finney.opentensor.ai:443`) is
the later Phase-E target; do **not** use `evm-testnet.dev.opentensor.ai` (dead).

1. Phases A/B green first (localnet dry-run + SP-1 dust probes + `sp2 check-metadata`
   against test.finney) — do NOT register the subnet before that.
2. `btcli subnet burn-cost --network test` -> `btcli subnet create --network test`
   (testTAO; starts the start_call clock).
3. Hyperparams via `btcli sudo set` (commit-reveal v4, `max_weight_limit`,
   `min_allowed_weights = 1`, high `immunity_period`, conservative registration
   intake).
4. Register the owner-validator UID first (its hotkey = the contract's reserve
   hotkey; set delegate take to 0 NOW) — the pool UID comes via the contract.
5. Fund the EVM deployer and the stctl ops key:
   `stctl evm-address <h160>` -> `btcli wallet transfer --destination <mirror ss58>`.
6. Deploy with SHORT rehearsal epochs: `forge script evm/script/Deploy.s.sol
   --rpc-url testnet --broadcast` (needs `ST_RESERVE_HOTKEY`; solc 0.8.24 /
   cancun). Put the **proxy** address in `contract_address`, then
   `stctl deploy-status` (shows reserve hotkey + buybackTotal).
7. Fund the **proxy's** mirror with TAO (`deploy-status` prints it) — the
   `registerOperator` burnedRegister burn is deducted from it (denomination
   pinned by the SP-1 probes).
8. `stctl register-operator --no_id=... --coldkey=... --miner_hotkey=...`
9. `btcli subnets check-start --network test` / `btcli subnets start --network test` when eligible; stake the
   genesis alpha to the owner-validator; first CRv4 commit.
10. Short-cadence epoch loop: `stctl deposit --push ...` (watch `BuybackReserved`)
    -> (windows pass) -> `stctl commit-root ...` -> `stctl finalize --epoch=<e>` ->
    claims; after N clean cycles, `setEpochParams` to production 7-day windows and
    raise the deposit cap (docs/LAUNCH.md Phase D).

## Tests

```sh
go vet ./stctl/ && go test ./stctl/
```

No-network unit tests: config round-trip + example-config pinning,
flag→calldata goldens (`deposit` `0xe2bbb158`, `claimMiner` `0x4c207962`,
`initialize` `0xd7a9b3db`, `transferStake` `0x17ce5f62` with hand-derived
ABI encodings), the initialize pre-send validation (reserve ≠ treasury,
window order, profile defaults), evm-address mirror derivation against
`sn/ss58`, rao↔α formatting, and docopt usage parsing for every subcommand.
