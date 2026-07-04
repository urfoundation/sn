# Launch runbook — testnet first, then mainnet

**v1 launches on the Bittensor TESTNET first (D28, `WHITEPAPER_DISCUSS.md`).** The
mechanism (v0.4: conviction-staking deposits, validator-computed weights, the
routable-IP head) shakes out on testnet, then the *same* runbook re-runs against mainnet
(finney) as the promotion step (Phase E). Testnet registration is cheap and the rate
limits are short, so the "do not register until rehearsed" mainnet gate relaxes here —
you can iterate. Research-verified facts are in `PLAN.md` §3; every load-bearing on-chain
value is marked **verify live** — query the chain, do not trust docs or this file's
snapshot. This is the operational companion to `PLAN.md` §3 and the component READMEs
(`evm/README.md`, `stctl/README.md`, `crv4/README.md`, `validator/README.md`).

The sequence is five phases:

```
A. Localnet rehearsal   (docker subtensor, fast blocks — the full bring-up dry-run)
B. Probes               (SP-1/SP-2 against testnet, before the subnet exists)
C. Testnet subnet       (register -> configure -> deploy -> start)
D. Ramp + e2e           (short epochs + tiny caps -> run full epochs on testnet)
E. Promote to mainnet   (re-run B–D against finney once testnet is clean — the later step)
```

The SP-1/SP-2/SP-3 harness is **endpoint-parameterized**, so every command below takes
`--rpc-url testnet` for v1 and `--rpc-url mainnet` verbatim for Phase E. On **mainnet**
the gate hardens: registration there locks real TAO and starts the emission clock — do
not register on finney until testnet (C–D) is clean.

## 0. Endpoints

| Surface | Testnet (v1 target) | Mainnet (Phase E) |
|---|---|---|
| Substrate WS | `wss://test.finney.opentensor.ai:443` (`btcli --network test`) | `wss://entrypoint-finney.opentensor.ai:443` |
| EVM JSON-RPC | `https://test.chain.opentensor.ai` — **chain id 945** (`0x3b1`) | `https://lite.chain.opentensor.ai` — 964 |

One subtensor node serves *both* the substrate WS and the EVM JSON-RPC surface (the
Frontier EVM layer is inside the node binary). **Do not** use
`evm-testnet.dev.opentensor.ai` (dead endpoint in the stale examples repo). Note
test.finney may run a **different runtime version** than finney, so re-run the SP-1/SP-2
conformance battery against mainnet at Phase E before the real deploy.

## 1. Prerequisites

- `btcli` (Bittensor CLI) — subnet create, hyperparameters, start call, wallet transfers.
- `forge`/`anvil` 1.7.x — contract deploy (`evm/`).
- Built subnet tooling: `stctl` (ops/inspection + `initialize`), `snclaim` (provider
  claim submit), `validator` (the validator binary), `cmd/sp2` (CRv4 conformance).
- A coldkey holding **testTAO** (from the Bittensor Discord faucet) sized for: the
  subnet lock/burn (`btcli subnet burn-cost --network test`, dynamic — verify at
  go-time), UID registration burns, EVM gas (via H160 mirrors), the genesis α
  acquisition (§C4), and the first deposits. *(Phase E on mainnet needs **real TAO** —
  that is where the hard cost gate applies.)*
- The **owner multisig and guardian keys exist before deploy** — Phase-0 governance is
  launch reality, not a milestone (custody is real from block one).

## 2. Phase A — Localnet rehearsal (SP-3, required)

Stand up the docker subtensor localnet **pinned to the runtime tag finney is running**
(verify live: `state_getRuntimeVersion`), fast blocks, Alice-funded. Then execute this
entire runbook's Phase C + D against it, end to end, at least once cleanly:

subnet create → hyperparams → owner UIDs → contract deploy (`stctl initialize` path and
the forge-script path both) → `registerOperator` → start → validator steering →
deposits → `BuybackReserved` → epoch close → commit → finalize → `claimMiner` →
`setEpochParams` ramp — plus the failure drills: guardian pause/unpause, a deliberate
missed commit (carry), an upgrade between finalize and claim (must not affect claims),
and a `sweepPool` retry after a forced move failure.

Known localnet caveats to plan around (do not silently skip):
- **drand/CRv4**: the localnet drand pallet may be stubbed or empty; the CRv4 wire
  format is already conformant against test.finney (SP-2), so the localnet pass may
  substitute `sp2 check-metadata` + a commit that is never revealed. The first *real*
  reveal happens in Phase C — budget a spare tempo for it.
- **Fast blocks change every window**: use block counts, not wall-clock intuition.

Deliverable: a written genesis script (the exact commands of Phase C with values filled
in) that ran green on localnet. Phase C on testnet should be *reading that script*.

## 3. Phase B — Testnet dust probes (before the subnet exists)

Everything here runs against **existing** testnet state with dust amounts — none of it
needs our subnet. (At Phase E, re-run this entire phase against mainnet.)

### B1. SP-1 precompile conformance (blocks first real value movement)

The harness is `evm/src/probe/STSubnetProbe.sol` (a throwaway probe that reproduces
STSubnet's exact precompile shapes + `mirror(this)` custody) driven by
`evm/script/SP1Conformance.s.sol`; its battery LOGIC is CI-proven against the mocks in
`evm/test/SP1Probe.t.sol`. **Why `cast`, not `forge` alone:** the subtensor precompiles
(0x402/0x802/0x805) live in the node runtime, so forge's local simulation can't execute
them — the battery must run **on the node**, which `cast call`/`cast send` do (raw
eth_call / raw tx, no local sim). Custody is contract-scoped, so it runs from the
deployed probe, not an EOA.

```sh
export PATH="$PATH:$HOME/.foundry/bin"
cd evm

# 0. local blake2f sanity + the full command playbook (no chain writes, no key):
SP1_NETUID=<existing_testnet_netuid> forge script script/SP1Conformance.s.sol --rpc-url testnet

# 1. deploy the throwaway probe (broadcast); prints its address + coldkey mirror:
SP1_NETUID=<existing_testnet_netuid> forge script script/SP1Conformance.s.sol \
    --sig "deploy()" --rpc-url testnet --broadcast --private-key $DEPLOYER_KEY

# 2. READ BATTERY — free, ON THE NODE (sampleHotkey = any live hotkey on the netuid,
#    e.g. uid0's from the metagraph). Returns the whole conformance matrix in one call:
cast call <probe> \
  "readBattery(bytes32)((bool,bytes32,bool,bytes32,bool,bool,bool,bool,uint16,bytes32,bytes32,bool,uint256))" \
  <sampleHotkey> --rpc-url testnet
```

The `readBattery` tuple, field by field — every one must hold:

| field | precompile | must be |
|---|---|---|
| `blakeOk` / `blakeKatMatch` | 0x09 blake2f (the H160→ss58 mirror = the whole custody model) | true / true |
| `selfColdkey` | — | `mirror(probe)` (the coldkey the pallet attributes the contract's stake to) |
| `edOk` / `edVerifyGood` / `edVerifyBad` | 0x402 Ed25519 | true / true (KAT verifies) / true (tampered rejected) |
| `mgOk` / `uidCount` / `uid0*` | 0x802 Metagraph | true / >0 / the live uid0 keys |
| `stakeViewOk` / `sampleSelfStake` | 0x805 `getStake` | true / the probe's own stake |

If 0x09 is absent (`blakeOk=false`), plan the `ST_SELF_COLDKEY` override (§C5). Each
check is independently try/caught, so a missing precompile shows as a clean `false`,
never a battery-wide revert.

**Value-bearing checks (dust real α, `cast send` from the funded probe mirror):**

```sh
# fund the probe's ss58 mirror (printed by step 1) with dust TAO, then:
cast send <probe> "seedFromTao(bytes32,uint256)"  <validatingHotkey> <raoDust> --value <taoWei> ...
cast call <probe> "selfStake(bytes32)(uint256)"   <validatingHotkey> --rpc-url testnet   # RAO-vs-18-dec: compare to raoDust
cast send <probe> "moveRoundTrip(bytes32,bytes32,uint256)" <hkA> <hkB> <amt> ...          # slippage-free: out-delta == in-delta == amt
cast send <probe> "snapshot(bytes32)"  <validatingHotkey> ...                             # §7.4 dividend two-step, step 1
#   ... wait >= 1 tempo (~72 min) ...
cast call <probe> "dividendDelta(bytes32)(uint256,uint256,uint64)" <validatingHotkey> --rpc-url testnet  # current > baseline => auto-compounds
cast send <probe> "transferOut(bytes32,bytes32,uint256)" <recoverColdkey> <hk> <amt> ...  # recover dust + exercise transferStake-from-contract
```

The **unit scale (rao vs 18-dec)** — the single most load-bearing SP-1 finding for the
"amounts in rao" assumption — falls straight out of step 2 of the value checks: you know
the `raoDust` you passed; `selfStake` reports what the pallet recorded; the ratio is the
scale. The **`0x402` KAT** can also be checked directly (no probe) with the `cast call
0x0402 "verify(...)"` line the script prints. `0x804` `burnedRegister` cost + **TAO-vs-α
burn denomination** is deliberately NOT exercised here (it burns TAO) — it is pinned by
the first real `registerOperator` at genesis (§C6), whose burn you eyeball on the proxy
mirror's balance.

Pin the subtensor release the interfaces were vendored from (`evm/README.md` records
the tag, v3.2.7) and **re-run the battery before every deploy** — the ABIs are not
formally versioned (the single biggest external risk, `PLAN.md` §10).

### B2. SP-2 CRv4 preflight (read-only against test.finney)

```sh
sp2 check-metadata --substrate wss://test.finney.opentensor.ai:443   # expect: "RESULT: conformant"
```

Verifies the `commit_timelocked_weights` call index + arg codecs, the 5 signed
extensions, and `CommitRevealWeightsVersion == 4` on the **test.finney** runtime we
launch v1 on. (Re-pin it against **finney** at Phase E — test.finney may run a
different runtime.) If the runtime upgrades, re-run. Details + unverified
items in `crv4/README.md`. The first commit on *our* subnet can only happen post-start
(C7) — a malformed first commit costs a tempo, never funds.

## 4. Phase C — Genesis (one planned window)

Execute the localnet-rehearsed script. Order matters; several steps are rate-limited or
irreversible.

### C1. Create the subnet

```sh
btcli subnet burn-cost --network test           # dynamic: decays over time, doubles per recent creation
btcli subnet create --network test              # pays the lock from the coldkey — testTAO (cheap on testnet)
```

Record the assigned **netuid**. The clock is now running: check
`btcli subnets check-start --netuid <N> --network test` immediately — `StartCallDelay`
(**verify live**; testnet is typically shorter than mainnet's ~7 days) sets your window
for C2–C6. On testnet this window is cheap and repeatable; the **hard** genesis gate
(registration locks real TAO + starts the emission clock — do not register until
rehearsed) applies at **Phase E on mainnet**.

### C2. Set hyperparameters (owner-gated; per-param cooldown = `tempo × OwnerHyperparamRateLimit`)

`btcli sudo set --netuid <N> --param <exact-chain-metadata-name> --value <v> --network test`. Set
explicitly (WHITEPAPER §15.1 — **verify live**, several genesis defaults have drifted
from docs):

| Param | Value | Note |
|---|---|---|
| `tempo` | 360 | ~72-min cadence |
| `max_allowed_uids` | 256 | hard ceiling: ~200 top miners + 1 pool UID/NO + validators |
| `commit_reveal_weights_enabled` | **true** | once B2 passed (§B2) |
| `commit_reveal_weights_version` | **4** | must equal the runtime `CommitRevealWeightsVersion` — verify live |
| `max_weight_limit` | real cap (low single-digit %) | chain default is *no cap* (65535) |
| `min_allowed_weights` | 1 | avoid the 1024 default |
| `immunity_period` | high (≫ 4096), and `>` reveal interval | protects new pools + top miners |
| `liquid_alpha_enabled` | true | |
| registration posture | conservative `max_regs_per_block` / `target_regs_per_interval`; let the burn bite | **mainnet-only concern**: new-subnet UID snipers arrive at start — register OUR UIDs first (C3) while burn is minimal, and keep the intake slow until the mechanism is ramped |

### C3. Register the owner UIDs — first, before outsiders

- **Owner-validator UID:** register the validator hotkey. This hotkey doubles as the
  contract's **buyback reserve hotkey** (`ST_RESERVE_HOTKEY`, C5) — every deposit is
  staked to it, locked (WHITEPAPER §7.4/D23). **Set its delegate take to 0 now**
  (take changes are rate-limited; it must be 0 before the first deposit or the take
  skims reserve yield to the hotkey owner instead of compounding).
- **Pool UID:** registered *through the contract* (`registerOperator`, C6), not by
  hand — the contract must own it. Only ensure the contract's mirror is funded (C5).

### C4. Acquire the genesis α position (the first buyback, and the majority seat)

The v1 posture — dividends-only validators, owner-majority (§9.2), the reserve
compounding that majority (§7.4) — assumes the owner **holds** the majority validator
seat. α is cheapest at genesis and new-subnet snipers buy early: acquire and stake the
owner-validator's α position in the first hours after start (C7), sized to hold a
comfortable majority of validator stake. This purchase is not overhead — it *is* the
first buy pressure under the token, and every subsequent deposit compounds the seat.

### C5. Deploy the ST contract (rehearsal-mode parameters)

Fund the EVM deployer, then deploy the proxy + impl:

```sh
# H160 -> ss58 mirror (one-way; no substrate key exists for the mirror):
stctl evm-address <deployer_H160>        # prints the prefix-42 ss58 mirror + pubkey
btcli wallet transfer --dest <mirror_ss58> --amount <testTAO> --network test
# the H160 now sees the balance; deploy pointing at chain 945 with SHORT epochs (§D):
cd evm && ST_NETUID=<N> ST_OWNER=0x<multisig> ST_GUARDIAN=0x<guardian> \
  ST_TREASURY_HOTKEY=0x<hotkey32> ST_RESERVE_HOTKEY=0x<owner_validator_hotkey32> \
  ST_T_EPOCH=7200 ST_COMMIT_WINDOW=600 ST_TRAILS_WINDOW=1200 ST_FINALIZE_OFFSET=2400 \
  forge script script/Deploy.s.sol --rpc-url testnet --broadcast --private-key $DEPLOYER_KEY
```

`initialize` is (v0.3/D23) `initialize(netuid, owner, guardian, treasuryHotkey,
**reserveHotkey**, tEpoch, commitWindowBlocks, trailsWindowBlocks, finalizeOffsetBlocks,
selfColdkey)` — the `phiBps`/`omegaBps`/`sampleK` fee/bounty parameters are gone with
the deferred effort bounty. Two custody hotkeys:

- **`ST_TREASURY_HOTKEY`** — the exact push-then-credit claims escrow (unchanged).
- **`ST_RESERVE_HOTKEY`** — the **owner-validator hotkey** (C3): the contract stakes
  **every deposit in full** onto it as the locked, dividend-compounding **buyback
  reserve** (WHITEPAPER §7.4, D23). Must be nonzero and **≠ the treasury hotkey**, and
  it is **set once at initialize — there is no setter** (re-pointing it is an
  upgrade-grade decision). Delegate take **0** (C3). Pool payouts (`poolTotal`) are
  **emission-only**.

The trails window (`ST_TRAILS_WINDOW`) still exists but is a **reserved dial** for the
deferred effort-bounty phase — it gates nothing in v1 (the bounty surface is parked at
`docs/parked/`).

Record the **proxy address** — that is the contract address for every tool below. The
proxy must itself hold TAO before `registerOperator` (the pool-UID `burnedRegister`
burn is charged to the contract's mirror). Fund it the same way
(`stctl evm-address <proxy_H160>` → `btcli wallet transfer`).

**Rehearsal-mode dials (short epochs + tiny caps — exercise the machine fast before the ramp):**
- **Short epochs**: the example above is `tEpoch = 7 200` (~1 day) with proportionally
  short windows — the epoch machine gets exercised end to end several times in week
  one. Windows are per-epoch-snapshotted (F2), so the later ramp (§D) is clean.
- **Tiny deposit cap**: set the server's `deposit_epoch_cap_rao` (D-3) to dust for the
  first epochs — it is the custody blast-radius bound while the pipeline proves itself.

> **Units caveat (SP-1).** Do not move real value past dust until the B1 battery has
> passed against the live runtime; all internal amounts are typed as α-rao.

### C6. Register the operator (pool UID)

```sh
stctl register-operator --no_id 0 --coldkey <NO_coldkey_ss58> --miner_hotkey <pool_hotkey_ss58>
```

The contract `burnedRegister`s the pool UID and owns it outright (pure accrual slot;
the NO never receives the emission). The B1 battery already pinned the burn
denomination — still eyeball the charge on the first real call.

### C7. Start + first steering

```sh
btcli subnets check-start --netuid <N> --network test   # StartCallDelay countdown from registration
btcli subnets start       --netuid <N> --network test   # owner, once eligible
```

Immediately: stake the C4 α to the owner-validator (permit = top-k by stake), start the
validator (§5), and land the **first CRv4 commit** (the one step no rehearsal could
fully cover — verify the reveal lands a round later; a failed first commit costs one
tempo). Then run §6's epoch loop on the short cadence.

## 5. Run validators

```sh
validator auth --user_auth <email>                          # writes ~/.urnetwork/jwt
validator run --contract 0x<proxy> --netuid <N> --theta 0.3 \
  --rpc https://test.chain.opentensor.ai \
  --substrate wss://test.finney.opentensor.ai:443 \
  --evm_key_file <key> --hotkey_seed_file <seed> -v
```

The validator walks `/verify` trails through provider egress, scores each pool
`deposit × quality`, and commits CRv4 weights every tempo (θ-split head/tail; head
empty until top miners register). There is no on-chain validator registration anymore —
the `registerValidator`/vpk surface went with the deferred effort bounty (D23); the
hotkey only needs its subnet UID (C3). Each chain consumer takes an **ordered**
`--rpc`/`--substrate` list for failover (`PLAN.md` §11.1).

## 6. Run a full epoch (e2e gate, on the short cadence)

```
t=0      epoch e closes        stctl epoch                    # confirm boundary + snapshot
≤ +2h    commit payout roots   (server st_work auto-commits; manual: stctl commit-root …)
+2–8h    review window         stctl state --epoch e          # inspect committed roots
+8h      finalize (anyone)     stctl finalize --epoch e       # snapshots poolTotal (emission-only)
         claims open           provider claim --epoch e --rpc …   # verify, then:
                               snclaim submit …
```

(Times shown for the C5 rehearsal-mode windows; production is +4h/+48h. The trails
window is a **reserved dial** for the deferred effort-bounty phase — no
`submit-trails`/`claim-validator` steps in v1.)

Verify, every rehearsal epoch: weights → emission accrues on the contract-owned pool
UID; a deposit moves **in full** onto the reserve hotkey (`BuybackReserved` event;
`stctl deploy-status` shows `buybackTotal`, and `getStake(reserveHotkey)` ≥
`buybackTotal` — strictly `>` once dividends land, the §7.4 compounding check); an
upgrade between finalize and claim must **not** affect finalized claims (the §6.4
invariant).

## 7. Phase D — Ramp to production

Only after ≥ N clean short epochs (pick N up front; 3–5):

1. Owner call `setEpochParams(50_400, 1_200, 7_200, 14_400)` — production 7-day epochs
   (+4h commit / +48h finalize; trails reserved). Applies to epochs that close after
   the call (F2 snapshots).
2. Raise `deposit_epoch_cap_rao` toward the sized deposit policy (server config, D-3),
   stepwise — the cap stays the standing blast-radius dial, not a launch-only one.
3. Publish the deposit reference rate (§7.1) + sourcing commitment (§7.4) and stand up
   the demand-ratio dashboard (`R_e`, §12.4 — `Deposited`/`BuybackReserved` events +
   `getStake(reserveHotkey)`).
4. Governance hardening per §6.4 Phase 1 (timelock + guardian posture) on its own
   schedule — the multisig + guardian existed from C5.

## Single-NO bootstrap note

With UR as the only NO, pool-tier Yuma weight is trivially 1.0 — the real
cross-operator consensus signal starts with multiple NOs / the head tier. Fine for
launch; it sets the multi-NO sequencing (`PLAN.md` §10).
