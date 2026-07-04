# evm — the ST settlement contract

Foundry project for `STSubnet`, the custody/settlement core of the UR Bittensor subnet
(WHITEPAPER §6, PLAN.md §2 + the v0.3/D23 amendment). UUPS-upgradeable, solc **0.8.24**,
`evm_version = "cancun"` (both pins are load-bearing — do not bump without re-verifying
against the subtensor EVM).

**v0.3 (D23) + v0.4 (D25) in one line:** deposits are **conviction stake** — `deposit()`
moves the **full amount** onto the locked, dividend-compounding `reserveHotkey` and emits
`Deposited`; there is **no DT ledger** (D25 dropped it — the contract does custody +
settlement only; validators weight the pools `implied_usage × quality` off the event log);
`poolTotal` is **emission-only**; the whole validator effort-bounty subsystem (fee
pool/φ/ω, vpk registry, submitTrails/prove/disputes, claimValidator) is **deferred** to the
independent-validator phase (WHITEPAPER §9.3) — the hardened v0.2 implementation is parked
at `../docs/parked/`.

```
evm/
  foundry.toml            solc 0.8.24 / cancun pins, via-IR, rpc profiles
  remappings.txt
  src/STSubnet.sol        custody/settlement core (policy surface grouped in-file, §6.4.3)
  src/lib/Blake2b.sol     single-block blake2b-256 via EIP-152 blake2f (0x09) — the H160 mirror
  src/interfaces/         VENDORED subtensor precompile interfaces (byte-identical, see below)
  src/probe/STSubnetProbe.sol  SP-1 conformance probe (throwaway; NOT production — see below)
  script/Deploy.s.sol     UUPS impl + ERC1967 proxy deploy; testnet/mainnet profiles
  script/SP1Conformance.s.sol  deploys the probe + prints the on-node battery playbook
  test/                   smoke + area suites (vm.etch precompile mocks; utils/STBase.sol harness)
  abi/STSubnet.abi.json   exported ABI (input to the Go bindings generator, sn/stabi)
  lib/                    vendored deps (no git submodules)
```

## Build / test / ABI

```sh
export PATH="$PATH:$HOME/.foundry/bin"   # forge 1.7.1
forge build
forge test
jq .abi out/STSubnet.sol/STSubnet.json > abi/STSubnet.abi.json   # regenerate after ABI changes
../stabi/generate.sh                                             # then regen the Go bindings
```

## Deploy

```sh
ST_NETUID=… ST_OWNER=0x… ST_TREASURY_HOTKEY=0x… ST_RESERVE_HOTKEY=0x… \
forge script script/Deploy.s.sol --rpc-url testnet --broadcast --private-key $DEPLOYER_KEY
```

Profiles: testnet chain id **945** (`https://test.chain.opentensor.ai`) — **the v1 default:
launch is testnet-first (D28, `docs/LAUNCH.md`)**, the SP-3 localnet inheriting the testnet
defaults (any unrecognized chain id); mainnet **964** (`https://lite.chain.opentensor.ai`) is
the later Phase-E promotion. All parameters are
env-overridable (see the script header). Defaults: `T_EPOCH` 50 400 blocks mainnet / 300 testnet; windows +1 200/+7 200/+14 400
blocks mainnet (≈4 h/24 h/48 h @ 12 s) / +50/+100/+150 testnet (the trails window is a
**reserved dial** for the bounty phase — it gates nothing in v1).

`ST_RESERVE_HOTKEY` is the buyback reserve's staking target (§7.4/D23): the
owner-validator hotkey. It is set **once** at initialize (no setter), must differ from the
treasury hotkey, and its delegate take should run at **0** so reserve dividends compound
whole.

Fund the deployer H160 via its ss58 mirror first (`blake2_256("evm:"‖H160)`, prefix 42 —
PLAN.md §3.6). The **proxy** must also hold TAO before `registerOperator` (the
`burnedRegister` burn is deducted from the contract's mirror account).

## SP-1 conformance harness (run BEFORE genesis)

`docs/LAUNCH.md` Phase B. v1 launches on **testnet first** (D28), so the precompile-ABI
assumptions (SP-1) are verified against the **live test.finney runtime** via a throwaway
probe, before the subnet exists (re-run against finney at the Phase-E mainnet promotion):

- `src/probe/STSubnetProbe.sol` — reproduces STSubnet's exact precompile shapes +
  `mirror(this)` coldkey custody, so what it observes is what the real contract will.
  Deployed to chain 945, driven with `cast` (the subtensor precompiles are runtime-only,
  so `cast call`/`cast send` — which execute ON the node — are the faithful path; forge's
  local sim can't run them). `readBattery(sampleHotkey)` returns the whole conformance
  matrix (blake2f/ed25519/metagraph/getStake) in one free eth_call; the dust value checks
  (`seedFromTao`/`moveRoundTrip`/`transferOut` + the `snapshot`/`dividendDelta` §7.4
  reserve two-step) are owner-gated. **Not production** — do not import it into STSubnet.
- `script/SP1Conformance.s.sol` — `run()` does the local blake2f KAT + prints the exact
  `cast` command playbook; `deploy()` broadcasts the probe.
- `test/SP1Probe.t.sol` — CI proof the battery logic is correct before it touches a live chain
  (a conformance tool you can't test is just another unverified assumption).

The Ed25519 (0x402) KAT is a real signature over a 32-byte message (deterministic
keypair, seed `sha256("urnetwork/sp1/ed25519-kat/v1")`); the blake2f (0x09) KAT is the
`mirror(0x1111…1111)` value pinned in `test/STSubnet.t.sol`.

## Vendored dependencies (no submodules)

| dep | tag |
|---|---|
| OpenZeppelin/openzeppelin-contracts | v5.6.1 |
| OpenZeppelin/openzeppelin-contracts-upgradeable | v5.6.1 |
| foundry-rs/forge-std | v1.16.2 |

Cloned with `git clone --depth 1 --branch <tag>` then `.git` removed.
Note: OZ upgradeable v5.6.1 no longer ships a storage-based `ReentrancyGuardUpgradeable`
(only transient-storage variants) — `STSubnet` uses a minimal in-contract storage guard
instead, because EIP-1153 support on the subtensor Frontier EVM is unverified (SP-1).

## Vendored subtensor precompile interfaces — **UNVERIFIED ABIs (SP-1)**

Pinned tag: **`opentensor/subtensor` v3.2.7**, files fetched byte-identical from
`precompiles/src/solidity/` (sha256 of the vendored copies):

| file (interface, address) | sha256 |
|---|---|
| `stakingV2.sol` (`IStaking`, 0x805) | `a9557cdd639329419cf9fda5744f699b8e89095ad34b6752fe0619201cc08194` |
| `neuron.sol` (`INeuron`, 0x804) | `a2162273baf9eea735048f19f2527f22a48a142d9faa160cfb063440e08f5a4e` |
| `metagraph.sol` (`IMetagraph`, 0x802) | `46afb389c3ffb04d6252fce8285c6e820ba1061e56af293160d2337834e51ce4` |
| `alpha.sol` (`IAlpha`, 0x808) | `991731a407a50fdf4d9b8bd3fd9ca9594bcf54e02cdec53aa9fc88268c82cc97` |
| `ed25519Verify.sol` (`IEd25519Verify`, 0x402) | `a452a7d3a4b66383558ce297edd359473f7236825ed62358497f456e7576c774` |
| `balanceTransfer.sol` (`ISubtensorBalanceTransfer`, 0x800) | `a086058f6ddb63fdbb60d22d4368d2a36ded05e527167265a2dede31a7659020` |

These ABIs are **not verified against the live runtime** (PLAN.md §2/§10; subtensor issue
#2455 — ABIs are not formally versioned). Consequences taken in code:

- Every precompile call flows through a small **internal `virtual` accessor** in
  `STSubnet` (`_staking()`, `_getStake`, `_tryMoveStake`, `_moveStakeStrict`,
  `_transferStake`, `_burnedRegister`, `_ed25519Verify`, `_mgColdkey`, `_findUid`,
  `_mirror`) so addresses/ABIs are swappable in one place and mockable with `vm.etch`.
- `_tryMoveStake` (the epoch-roll sweep) is non-reverting: a precompile failure cannot
  brick the epoch machine; the measurement baseline makes re-sweeps exact
  (`sweepPool(noId)` retries permissionlessly). `_moveStakeStrict` (the deposit→reserve
  leg) is the opposite by design: a deposit either fully reserves or reverts the credit.
- `IAlpha` (0x808) and `ISubtensorBalanceTransfer` (0x800) are vendored per the plan but
  **unused** by v1 logic.
- v3.2.7 also ships a `uidLookup` precompile (0x806) — not vendored/used; noted as a
  possible SP-1 alternative for hotkey→UID binding via `associate_evm_key`.
- SP-1 must additionally confirm: contract-as-coldkey custody, rao vs 18-dec units for
  every amount-bearing call (all contract amounts are rao passthrough `uint256`),
  `burnedRegister` cost/denomination, `0x402` gas (the head-binding check), blake2f
  (0x09) availability, `getStake` semantics on `(hotkey, coldkey, netuid)`, and that
  **delegated-stake dividends auto-compound** onto `(reserveHotkey, selfColdkey)` with
  the expected take semantics (the §7.4 compounding leg).

## Deviations (st_abi.json / WHITEPAPER vs this contract)

`stctl/st_abi.json` is generated output kept in sync with this contract (see
`../stabi/generate.sh`), so there is no drift by construction. The deviations below are
against the WHITEPAPER/PLAN sketches:

1. **Constructor → `initialize(...)`.** The contract is UUPS (D-12):
   `initialize(uint16 netuid, address owner, address guardian, bytes32 treasuryHotkey,
   bytes32 reserveHotkey, uint64 tEpoch, uint64 commitWindowBlocks,
   uint64 trailsWindowBlocks, uint64 finalizeOffsetBlocks, bytes32 selfColdkey)` called
   through the proxy constructor (see `script/Deploy.s.sol`). `selfColdkey = 0` ⇒
   computed on-chain as `mirror(proxy)` via blake2f.
2. **Deposit mechanism.** The whitepaper/plan flow (`approve` +
   `transferStakeFrom` pull) **does not exist in the StakingV2 precompile at v3.2.7**
   (it has `transferStake`/`moveStake`/`getStake`/`addProxy` only — no approvals).
   `deposit(noId, alphaAmount)` keeps its pinned signature but is **push-then-credit**:
   the NO first pushes α to the contract with
   `StakingV2.transferStake(mirror(proxy), treasuryHotkey, netuid, netuid, amount)`
   (from a wallet holding stake on `treasuryHotkey`), then calls `deposit`, which
   verifies `getStake(treasuryHotkey, selfColdkey) ≥ accountedStake + amount`, emits
   `Deposited(e, noId, from, amount)` (the per-NO deposit record — **no DT ledger**, D25), and **moveStakes the full amount onto `reserveHotkey`**
   (v0.3/D23 — the buyback; `buybackTotal` + the `BuybackReserved` event are the audit
   trail; `accountedStake` deliberately never counts it, so the escrow ledger tracks
   exactly the claimable emission). Still slippage-free end to end.
   *Trust note (v1):* attribution is caller-claimed — `deposit`/`commitOperator` are
   restricted to the NO's registered `operatorAddress` (or owner), but with multiple
   NOs a pushed-but-uncredited amount could be attributed by whichever authorized NO
   calls first (the α is reserved either way; only the `Deposited` event's `noId` label
   is mis-attributable). Acceptable for the single-NO launch (owner-gated admission);
   upgrade paths: `transferStakeFrom` if SP-1 finds a pull ABI on a newer runtime, else
   per-NO deposit hotkeys.
3. **Effort-bounty subsystem: deferred (v0.3/D23).** `registerValidator[For]` + the vpk
   registry, `setOperatorServerKey`, `submitTrails`/`trailSampleSeed`/`sampleIndices`/
   `proveTrailSamples`/`reseedTrailSamples`, both dispute functions, `claimValidator`,
   and the fee params (`phiBps`/`omegaBps`/`sampleK`/`feePool`/`fundFeePool`) are **not
   in the v1 contract**. The hardened v0.2 implementation (sample-estimator credit F1,
   coverage-bound A2 signatures over `sha256(finalDigest ‖ coverage)`, HF-2 reseed caps,
   the 9-field committed leaf) is parked byte-for-byte at
   `../docs/parked/STSubnet-v0.2-effort.sol.ref` (+ its test suites) and returns with
   the independent-validator phase. The wire-level coverage attestation REMAINS live in
   `/verify` (`connect.VerifyEffortDigest`), so proofs minted today stay consumable then.
4. **`T_EPOCH` constant → `tEpoch` parameter**, plus governance-settable
   `commitWindowBlocks`/`trailsWindowBlocks`/`finalizeOffsetBlocks` (D-11; whitepaper
   §6.1 declared a constant; the trails window is a reserved bounty-phase dial). Missed
   commit ⇒ the pool's total (emission + previous carry) accumulates in `carry[noId]`
   at `finalizeEpoch` and lands in the pool's next committed epoch. Per-epoch window
   snapshots at close (`epochWindows`) keep `setEpochParams` future-only (F2 fix).
5. **Emission measurement (D-4).** `poolTotal_n = poolEmission[e][n] + carry[n]` —
   **emission-only** (v0.3: deposits are reserved, never distributed) — where
   `poolEmission` is the **stake delta** on the pool's own `minerHotkey` between epoch
   boundaries, measured at each (lazy) `rollEpochs` and swept to `treasuryHotkey` via
   `moveStake` — never `getEmission` point reads. Lazy rolls attribute late-accrued
   emission to the first unrolled epoch (documented approximation);
   `MAX_ROLLS_PER_CALL = 32` bounds catch-up gas, with state-mutating entry points
   requiring a fully-rolled epoch.
6. **Miner claim leaf (Go parity — must match `sn/merkle` exactly).**
   `leaf = keccak256(bytes.concat(keccak256(abi.encode(bytes32 coldkey, uint256
   shareBps))))` — the PLAN §2 OZ double-hash **overrides** the single-hash snippet in
   WHITEPAPER §11.2. Sorted-pair internal hashing, `MerkleProof.verify`. Dedup key
   `keccak256(abi.encode(noId, coldkey))`; amount `shareBps·poolTotal/10_000`;
   cumulative per-pool cap. Reference: `minerLeafHash(coldkey, shareBps)` (public).
7. **`epoch` view is the last *rolled* epoch** — the counter advances lazily
   (`rollEpochs` is permissionless and implicit in every time-gated call);
   `pendingEpoch()` shows the chain-time epoch.

## Custody / accounting model (v0.3)

All held α is stake under the contract's coldkey (`selfColdkey = mirror(proxy)`), on
**two hotkeys with different jobs**:

- **`treasuryHotkey` — the claims escrow.** Deposit-push landing pad + swept pool
  emission awaiting `claimMiner`. `accountedStake` tracks every attributed rao
  (sweeps − payouts); `getStake(treasury) − accountedStake` is the pushed-but-uncredited
  buffer. Payouts are `transferStake(recipientColdkey, treasuryHotkey, netuid, netuid,
  amount)` — recipients receive α **as stake** under their coldkey (whitepaper §6.3),
  keep it earning or `removeStake` at their own discretion.
- **`reserveHotkey` — the buyback reserve (§7.4/D23).** Every credited deposit, in
  full, forever; on the live chain validator dividends auto-compound on top. Reserve
  audit: `getStake(reserveHotkey) ≥ buybackTotal`, plus the `BuybackReserved` events.

**Structural invariants (§6.4, D-12/D23):** finalize is append-only and in-order
(`nextFinalizeEpoch`); nothing writable by owner/guardian touches a finalized epoch's
`poolTotal`/`noCommit` or claim state; `claimMiner` carries **no pause gate**. And the
**reserve is one-way**: no function sources a stake transfer from `reserveHotkey` —
payouts source exclusively from the escrow. The guardian's only power is `setPaused`
over `deposit` and `finalizeEpoch`. Covered by
`test_pauseAndUpgrade_cannotBlockFinalizedClaims` (upgrade-under-fire) and
`test_ownerCannotClawBackFinalizedFundsOrReserve`.

## Epoch lifecycle (blocks; WHITEPAPER §5.2)

```
close(e) = epochCloseBlock[e]                     (set at the e→e+1 roll, = intended boundary)
[close, close+commitWindowBlocks]                 commitOperator(e, …)   re-commit allowed
[close+finalizeOffsetBlocks, ∞)                   finalizeEpoch(e)  → claims open forever
(trailsWindowBlocks: reserved for the bounty phase — no v1 gate)
```

## Test vectors / mocks

`test/STSubnet.t.sol` (smoke): deploy/init guards (incl. the reserve-hotkey checks),
deposit→reserve movement + strict-move revert, happy-path epoch (deposit reserved →
emission accrual → commit → finalize emission-only → miner claims, with exact
conservation across escrow AND reserve), pause + upgrade-under-fire, missed-commit
carry, and blake2b known-answer vectors (generated with Python `hashlib.blake2b`). Area
suites: `EpochLifecycle` (deposits/reserve, windows, carry, stake-delta sweeps, lazy
rolls, finalize), `Claims` (shared Go↔Solidity Merkle vectors, caps, dedup),
`Registration` (operator + initialize guards), `Governance` (invariants incl. the
one-way reserve under hostile upgrades), `HeadBinding`. Precompile mocks
(`test/mocks/PrecompileMocks.sol`) are `vm.etch`ed at the canonical addresses;
`STSubnet`'s accessors are `virtual` for harness overrides.
