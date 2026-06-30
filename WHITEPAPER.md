# UR Subnet — Whitepaper

**A Bittensor subnet for a decentralized privacy network.**

Version 0.2 (design). Target chain: Bittensor / Subtensor mainnet (finney), dTAO era.

> **v0.2 — two-tier miner side.** The miner side now runs **two channels in parallel**: the per-NO
> **pool** (the on-ramp — `deposit × quality`, Merkle claim, *unchanged* from v0.1) *and* **top-level
> miners** (the merit apex — the top ~200 providers each hold their own UID, steered directly by
> validators on pure measured quality and paid natively). See §8.4–8.5, §10, §11.4, §14; rationale and
> the decision log in `WHITEPAPER_DISCUSS.md` D16–D20.

---

## Executive summary

The **UR Subnet** runs a **decentralized privacy/VPN network** entirely through on‑chain incentives.
**Network Operators (NOs)** run the servers; independent **providers** carry ingress/egress traffic; and
independent **validators** run the `VALIDATOR.md` cryptographic routing‑verification protocol — walking
server‑assigned chains of providers to prove real‑time transit and measure **which providers are the
weakest links**. That measurement is the core: each tempo, validators score every NO's pool
**`deposit × quality`**, and Bittensor's **Yuma Consensus** turns those scores into the miners'
emission. As in canonical Bittensor, **validators' evaluation drives the payout** — the `deposit` term
(α a NO stakes, a costly, revenue‑backed signal of real demand) anchors the split, and the measured
`quality` (`Q_n`, from the trails) is the earned modulator.

Because one NO may serve **100k+ providers — far beyond a subnet's ~256 UID cap** — each NO is a single
**pool UID**, and its providers are paid *inside* the pool by **Merkle claim**. A smart contract on the
**Subtensor EVM** (the **ST contract**, denominated in the subnet's **α** token) is the ledger, the
emission custodian, and the **7‑day settlement** engine: providers and validators **claim their α
directly from the contract with cryptographic proofs**, so a NO *directs* where its pool's rewards go but
**never holds anyone else's funds**. Validators earn Bittensor‑native **dividends** (∝ stake × scoring
accuracy) **plus a fee‑funded effort bounty** (∝ verified trail volume) — the engine that keeps the
failure data flowing.

**Two miner tiers, in parallel.** The pool above is the **on-ramp** — a place to start, with a **baseline
reward** and a low barrier (no UID, no registration burn). Above it sits the **merit apex**: the **top
~200 providers each claim their own miner UID** ("**top-level miners**") and are **steered emission
directly by validators on pure measured quality** (`weight = Q_p`, no deposit term), paid **natively** to
their own hotkey — no contract custody, no Merkle claim, no operator in the loop. A provider is matched to
its UID by a **signed `client_id → hotkey` binding** the validators read (§11.4); it **starts in a pool,
graduates to a direct slot, and falls back to the pool if it slips**, with the chain's own deregistration
churn running that tournament. Both tiers share **one** mechanism's 256-UID metagraph (§14); a governance
split **θ** sets how much of the 41% miner emission flows to the head vs. the pools (§8.4–8.5).

The subnet **launches centralized‑but‑bounded** — an owner multisig behind an upgradeable contract, with
**finalized claims made un‑clawback‑able from day one** — and hardens to a **timelocked,
guardian‑protected** contract, then broader governance. v1 rewards independently *measured liveness*;
closing the gap to honest‑relay, payout‑grade verification is the `VALIDATOR.md` §10 roadmap.

**At a glance**
- **Bittensor‑aligned:** validators evaluate miners (pools) → Yuma Consensus → emission; standard
  **18% owner / 41% miner / 41% validator** α split.
- **Real‑usage anchored:** a costly, revenue‑backed α **deposit** weights the cross‑operator split; the
  network's unit of account is the subnet's **α**.
- **Scales to 100k+ providers:** pool UIDs + off‑chain **Merkle** payout claims (providers and validators
  inside a pool are *not* UIDs).
- **Two miner tiers:** a per-NO **pool** (on-ramp — `deposit × quality`, Merkle claim) *and* **top-level
  miners** (the top ~200 providers as direct UIDs, steered on pure quality, paid natively), split by a
  governed share **θ** (§8.4–8.5).
- **Trust‑minimized custody:** no operator holds others' emission; everyone is paid by direct on‑chain
  claim against a committed Merkle root.
- **Validator data, strongly incentivized:** native dividends **+** a coverage‑weighted **effort bounty**
  over cryptographically verified routing trails.
- **Progressive decentralization:** multisig + upgradeable → timelock + pause‑only guardian → broader
  governance.

---

## 0. Reading guide

This document specifies, in implementable detail, a Bittensor subnet that coordinates a
decentralized privacy/VPN network through on‑chain incentives. It is the synthesis of three
inputs:

- **`seed/INCENTIVES.md`** — the economic intent (network operators, miners, validators; deposit‑weighted
  payouts; the validator prisoner's dilemma; the 7‑day block).
- **`VALIDATOR.md`** — the off‑chain routing‑verification protocol (`/verify`) that produces the
  signed **validated paths** this subnet pays for.
- **Current Bittensor mechanics** (Yuma Consensus, dTAO, the Subtensor EVM, the commitments pallet,
  the precompiles) as they exist on the `opentensor/subtensor` `main` branch in mid‑2026.

It is written so that an engineer can build the smart contract, the off‑chain operator/validator
software, and the chain configuration from it. Where a parameter or chain default is load‑bearing
and time‑sensitive, it is flagged; verify against a live chain before launch (see §15).

**Five design decisions are fixed up front** (these were chosen deliberately; §13 records the
alternatives):

1. **Settlement = EVM contract + native Yuma.** A Solidity contract on the Subtensor EVM (the **ST
   contract**) custodies deposits and the captured pool emission and settles payouts by Merkle claim;
   protocol **emissions** are delivered through the chain's coinbase. We do **not** fight the coinbase.
2. **Everything is denominated in the subnet's α (alpha) token.** Deposits, validation fees, and
   payouts are α. This is why `seed/INCENTIVES.md` calls it the **ST (subnet‑token) contract**.
3. **Miner pools, scored by real Yuma consensus.** Each NO is **one miner‑pool UID**; its 100k+
   providers are paid *inside* the pool by Merkle claim. **Many independent validators** (no NO owns
   them) score the pools `deposit × measured‑quality` — so **validators' evaluation drives the miner
   emission, the Bittensor way** — and Yuma's median/clipping/vtrust/bonds do real work. A fee‑funded
   **effort bounty** keeps their trail volume (and so the failure data) high (§9).
4. **Two clocks.** The chain's native **tempo** (~360 blocks ≈ 72 min) drives weight‑setting and
   emission; a **7‑day epoch** (≈ 50 400 blocks) is the application‑layer accounting/settlement
   period.
5. **Two miner tiers in one mechanism.** Alongside the per-NO pools (the on-ramp), the **top ~200
   providers hold their own miner UIDs** and are steered **directly** by validators on pure measured
   quality, paid **natively** (§8.4). Both tiers live in **one** mechanism — a second mechanism would
   halve the 256-UID space below 200 (§14) — and a governance split **θ** divides the 41% miner emission
   between the head and the pools (§8.5).

---

## 1. Summary of the mechanism

A **Network Operator (NO)** runs privacy servers. Independent **providers** (miners) attach to one
or more NOs and carry ingress/egress traffic. Independent **verifiers** (validators) attach to one
or more NOs and run the `VALIDATOR.md` trail protocol, producing cryptographically signed
**validated paths** that prove provider liveness.

Money flows in three coupled channels, all in α:

1. **Deposits (DT).** Each NO deposits α into the ST contract, sized to its real usage at the network's
   **off‑chain published reference rate** (no on‑chain oracle — §7.1). `SUM(DT)` per NO is the costly,
   on‑chain **signal of real demand**. It is the single quantity that weights everything else.

2. **Emission (Yuma consensus over NO pools).** Every NO has **one contract‑custodied miner‑pool UID**
   (all its providers). **Independent validators** — anyone who stakes α and runs `VALIDATOR.md` trails —
   score **every** NO's miner pool `deposit × measured‑quality` and submit those weights, so the
   **validators' evaluation drives the miner emission — the Bittensor way**. With many independent
   validators, **Yuma Consensus does real work**: stake‑weighted median + **clipping**, so no NO can
   inflate its own pool or knife a rival. Miner emission (41%) lands on the miner‑pool UIDs ∝
   `deposit × consensus‑quality`, **owned by the contract's coldkey** — no emission ever touches a NO's
   keys. **The 41% miner emission now splits across two tiers** (§8.4–8.5): a governance share **θ** to
   **top-level miners** (the top ~200 providers as their own UIDs, weighted on pure quality `Q_p`, paid
   **natively** to their own hotkeys), and `1−θ` to the NO pools above. Validator emission (41%) flows **natively** to the independent validators ∝ stake × vtrust.

3. **Settlement (contract).** Over a 7‑day epoch the contract holds the deposit balance **and the
   captured miner emission**, then distributes it. A NO's **miner pool** (its UID's earned α + the
   refundable part of its deposit) goes to its providers. Since the **100k+ providers are not on‑chain
   UIDs** (the subnet cap is ~256) they are paid *inside* the pool by **Merkle claim**: the NO commits a
   payout root and every provider **claims its α directly from the contract** with an O(log N) proof. The
   NO directs the split but never holds the α.

Top-level miners need **no settlement** — Yuma pays their UID natively each tempo (§8.4); the contract
holds and settles only the **pool tier** (the tail), exactly as above.

Validators earn a **second** stream for the work that produces the subnet's core data — *which providers
are the weakest links* (`VALIDATOR.md` §7). On top of native dividends, the contract pays an **effort
bounty** ∝ each validator's verified, coverage‑weighted completed trails, funded from a **fee pool** (the
non‑refundable deposit fraction `φ` + a slice of the owner cut). The bounty pulls validators to run more
trails through under‑sampled providers — the more it is funded, the more complete the failure data (§9).

Because the weights carry a *subjective* signal — measured provider quality — the standard Bittensor
anti‑gaming stack applies and is **switched on**: **commit‑reveal** (a lazy validator can't copy fresh
consensus), consensus **clipping + vtrust** + the **self‑weight mask** (a validator can't inflate a pool
or score its own NO), and **bonds / Liquid Alpha** (rewarding validators who back good pools early). The
**deposit** is the *objective* anchor that ties the across‑NO split to revenue‑backed demand (§7);
**quality** is the Yuma‑measured factor that makes validators' evaluation the thing that moves the money
(§10). At bootstrap, when `Q_n` is still noisy, governance **caps the quality swing** and widens it as the
validator set and data mature (§12.3) — so quality is on the payout path from day one without a thin
sample wildly misallocating.

```
                     INDEPENDENT VALIDATORS  (stake α, run /verify trails)
         each tempo → score BOTH tiers → commit-reveal → Yuma (stake-median + clip + vtrust)
             pools (tail):  weight = deposit_n × quality Q_n
             top miners:    weight = quality Q_p        combined into ONE weight vector,
                                       │                split by governance share θ
                                       ▼  drives 41% miner emission
                        ┌──────────────┴───────────────┐
                  (1−θ) │ TAIL                     HEAD │ θ
                        ▼                               ▼
 ┌──────────────────┐   ┌───────────────────────────┐  ┌───────────────────────────────┐
 │ Network Operator │   │ ST CONTRACT (Subtensor EVM)│  │ TOP-LEVEL MINER UIDs  (~200)   │
 │ runs servers +   │DT │  owns one POOL UID per NO  │  │  client_id ⇄ hotkey  (§11.4)   │
 │ /verify; commits │──▶│  custodies deposits +      │  │  weight = Q_p                  │
 │ payout root;     │   │  miner emission + FeePool  │  │  NATIVE emission → own hotkey  │
 │ holds NO α only  │   │  → per-NO Merkle roots     │  │  (no take, not shared, no      │
 └────────┬─────────┘   └─────────────┬──────────────┘  │   contract custody)            │
          │ commits root              ▼ claim α          └───────────────┬───────────────┘
          ▼ (never holds α)   providers (100k+, TAIL)                    ▼ direct, trust-minimized
   customers ($) ──▶ revenue         (Merkle proof)          a top provider's own coldkey
          └────────── start in a pool ─▶ graduate to a top slot ─▶ fall back if quality slips ──┘

 Bittensor coinbase: 18% owner · 41% miner (Yuma, split θ / 1−θ above) · 41% validator (NATIVE ∝ stake×vtrust)
 effort bounty (φ·ΣD + ω·OwnerCut, ∝ coverage-weighted /verify trails) → validators, on top of dividends
```

---

## 2. Background: the Bittensor primitives this design uses

A condensed, current (dTAO‑era) reference. Identifiers are from `opentensor/subtensor` `main`.

### 2.1 Subnet, neurons, UIDs

- A **subnet** (identified by `netuid`) is registered permissionlessly with `register_network`; the
  registrant becomes the **owner** (an owner coldkey + an owner hotkey auto‑registered at **UID 0**,
  immune from deregistration). Creation pays a dynamic α‑denominated lock that **seeds the subnet's
  liquidity pool**.
- A **neuron** is a `(coldkey, hotkey)` holding a **UID** (slot). Default capacity
  `max_allowed_uids = 256`. A hotkey **registers** by paying a recycled **burn** (`burned_register`);
  when slots are full the lowest‑emission non‑immune neuron is deregistered.
- A neuron is **not** intrinsically "miner" or "validator." It earns **incentive** (miner reward) if
  validators weight it, and **dividends** (validator reward) if it holds a **validator permit** and
  sets weights. Roles are emergent from stake + permit + behavior.

### 2.2 Weights, Yuma Consensus, emission

- Each **tempo** (default **360 blocks**, ≈ 72 min; per‑subnet, range `[360, 50 400]`), the chain runs
  an **epoch** = one Yuma Consensus pass that converts the validator **weight matrix** + **stake** into
  two emission streams.
- **Validators set weights** with `set_weights(netuid, dests, weights, version_key)` (`u16` values,
  rescaled so the max entry is `65535`). Only neurons with a **validator permit** (top‑k by stake,
  `max_allowed_validators`, default 64) and stake ≥ a small threshold have weight influence; others'
  rows are masked out.
- **Consensus** per miner `j` is the κ‑stake‑weighted median of weights (κ ≈ 0.5). Each validator's
  weight on `j` is **clipped** to consensus (`min(W_ij, C_j)`); the surplus is discarded and lowers
  that validator's **vtrust**. **Incentive** `I_j` = normalized stake‑weighted column‑sum of clipped
  weights → miner reward. **Bonds** (EMA of stake‑weighted clipped weights) drive **dividends**
  `D_i = Σ_j B_ij · I_j` → validator reward.
- **Emission split (current, per block, in α):** owner **18%** (`SubnetOwnerCut = 11796/65535`,
  governance‑settable), then the remaining 82% split **50/50** (hard‑coded) → **41% miners** (by
  incentive) / **41% validators + their stakers** (by dividends). Emission accrues to hotkeys as **α
  stake** and is drained each tempo.
- **Stake weight** = `alpha_stake + 0.18 × tao_stake` (the `tao_weight = 0.18` factor down‑weights
  root/TAO stake relative to the subnet's own α).

### 2.3 dTAO economics

- Every subnet has its own **α token** with a TAO↔α AMM pool; α **price** = `SubnetTAO / SubnetAlphaIn`.
  Staking TAO into the subnet swaps TAO→α (slippage on the curve); **`transferStake`/`moveStake`
  within the same subnet do not touch the AMM** (no slippage) — they move stake ownership/delegation.
- Each subnet mints ~1 α/block of **participant reward** (`alpha_out`) regardless of size; subnets
  compete for **TAO inflow** (which sets α price), not for α emission rate. α is hard‑capped at 21M
  with the same halving curve as TAO.
- **α is not a liquid ERC‑20.** It exists only as **stake** keyed `(coldkey, hotkey, netuid)`. An
  ERC‑20‑like `approve`/`allowance`/`transferStakeFrom` layer exists **over stake**, per netuid
  (§2.5). This shapes the contract design: the ST contract custodies α **as stake** and pays out with
  `transferStake`.

### 2.4 Commitments pallet (the free data layer)

- `set_commitment(netuid, info)` is **`Pays::No` (zero fee)** with a **zero deposit**. Keyed by
  `(netuid, hotkey)`. Up to **3 fields/commit**; field types include typed 32‑byte hashes
  (`Keccak256`, `Sha256`, …), `Raw` (≤128 B), `BigRaw` (≤512 B), and `TimelockEncrypted` (≤1024 B,
  drand commit‑now/reveal‑later). Budget **3100 bytes per `(netuid, hotkey)` per subnet epoch**.
- Reads are **free state queries** (no tx). SDK: `set_commitment`, `get_commitment`,
  `get_all_commitments`, `get_commitment_metadata`. This is the canonical "publish a 32‑byte Merkle
  root each epoch, serve the leaves off‑chain" facility, and there is in‑ecosystem precedent
  (root‑on‑chain + proofs‑off‑chain).

### 2.5 The Subtensor EVM and precompiles

- Standard **Frontier EVM**, **Cancun** opcodes, Solidity **0.8.24**; mainnet **chain ID 964**
  (testnet 945), RPC `https://lite.chain.opentensor.ai`, gas paid in **TAO**, **permissionless**
  deployment, 75M block gas. Contract storage is pay‑once gas, **no rent** → cheap Merkle roots.
- An H160 deterministically maps to an SS58 account: `AccountId32 = blake2_256("evm:" ‖ H160)`. **A
  contract is therefore a coldkey** (its mapped SS58). The staking precompile uses *the calling
  contract's address as the coldkey* — so a pooling contract centrally custodies α and must do its
  own share accounting.
- **Precompiles the ST contract uses** (address → purpose):

  | Address | Name | What the contract calls it for |
  |---|---|---|
  | `0x…0805` | **Staking V2** | `addStake`/`removeStake` (α↔TAO), `transferStake`/`moveStake` (slippage‑free α moves), `getStake`, `approve`/`transferStakeFrom` (pull deposits), `getAlphaStakedValidators` |
  | `0x…0804` | **Neuron** | `setWeights` / `commitWeights` / `revealWeights` — **each independent validator submits its pool scores** under commit‑reveal; `burnedRegister` (miner‑pool UIDs); `serveAxon` |
  | `0x…0802` | **Metagraph** | `getEmission`, `getDividends`, `getIncentive`, `getVtrust`, `getValidatorStatus`, `getHotkey`/`getColdkey`, `getStake` (read consensus results) |
  | `0x…0808` | **Alpha** | `getAlphaPrice`/`getMovingAlphaPrice`, `getAlphaOutEmission`, pool reserves, `simSwapAlphaForTao` (α price / emission / slippage reads) |
  | `0x…0800` | **BalanceTransfer** | move TAO EVM↔Substrate (`transfer(bytes32 ss58)`) |
  | `0x…0402` | **Ed25519Verify** | `verify(message, pubkey, r, s)` — **verify `VALIDATOR.md` Ed25519 proofs on‑chain** for disputes |
  | `0x…0807` | **StorageQuery** | raw reads of e.g. `Commitments.CommitmentOf` if the contract must read a commitment (brittle; prefer passing roots in directly) |

  > Precompile ABIs are **not formally versioned** (issue #2455). Pin a Subtensor release tag, target
  > **Staking V2** (`0x805`, not the legacy `0x801`), and re‑verify addresses/ABIs before launch.

---

## 3. Roles and on‑chain identity

| Design role (`seed/INCENTIVES.md`) | On‑chain identity | Notes |
|---|---|---|
| **Subnet owner** (UR Foundation) | owner coldkey + owner hotkey at **UID 0** (immune); deploys & governs the ST contract | receives the 18% owner cut (a slice `ω` of which **co‑funds the validator effort bounty**, §9.3); the governance **referee** (§9.6). |
| **Network Operator (NO)** | a contract registration (`noId`) with **one miner‑pool UID** (its providers, contract‑owned). Holds **no emission**; runs no validator. | a per‑NO *pool operator*: deposits; runs the `/verify` server (co‑signs trails); commits the Merkle payout root that splits its miner pool. Directs flow; the contract holds and pays. |
| **Provider (miner)** | a `client_id` **inside** a NO's miner pool — **not a UID** (100k+ providers can't each be a UID) | carries traffic; **claims its α directly from the contract** with a Merkle proof against its NO's payout root. The **on-ramp / tail** tier; can **graduate** to a top-level slot (§8.4). |
| **Top-level miner (head)** | **its own miner UID**; its `client_id` is **bound to its hotkey** (§11.4) | the **merit apex**: the top ~200 providers by measured quality, steered **directly** by validators (`weight = Q_p`); **native** emission to its own coldkey — no contract custody, no Merkle claim; maintained by deregistration churn (§8.4). |
| **Validator** (was "verifier") | an **independent** Bittensor validator UID; stakes its **own** α; binds its `VALIDATOR.md` Ed25519 `vpk` in the contract | runs `/verify` trails (the failure‑data signal), scores pools `deposit × Q_n` under commit‑reveal (§10). Earns **native dividends** (∝ stake × vtrust) **+ an effort bounty** (∝ verified trails, §9.3). No NO, no pool. |
| **ST contract** | a coldkey (mapped SS58) that **owns each NO's miner‑pool UID** and holds the **fee pool** | custodies **miner** emission + deposits; pays the validator **effort bounty**; settles every pool by Merkle claim. Does **not** custody validator emission (it is native). |

**Why pools (miner side only).** A NO has up to 100k providers — they cannot be UIDs (subnet cap ≈ 256),
so each NO gets **one miner‑pool UID** and its providers are paid *inside* it by Merkle claim. That
miner‑pool UID is **owned outright by the contract's coldkey** (a pure accrual slot), so the NO never
holds the emission destined for its providers. **Validators are not pooled** — they are independent
Bittensor validator UIDs (own hotkey, own stake, native dividends), which is both simpler and the
**independence** the measurement needs (§9.5). This removes the per‑NO validator pool, the take‑0 custody
binding, the intersection split, and the VT of earlier drafts (§13.6).

**Why also a head tier (top-level miners).** Pools solve *scale* (100k+ providers), but inside a pool a
provider trusts its NO's discretionary payout split and is one of thousands. The **top ~200 providers**
therefore *also* get the canonical Bittensor treatment — **their own UID, steered directly by validators,
paid natively** (§8.4) — so the best providers compete head-to-head on merit, trust-minimized, while the
pool stays the low-barrier on-ramp everyone starts in. A provider is in **exactly one** tier at a time
(promoted out of its pool's payout list once it holds a UID — no double-pay, §8.4). The two tiers share one
256-UID metagraph (§14).

**Binding the validator identity.** `seed/INCENTIVES.md` says a validator "uses their wallet PK as their
validation path key," while `VALIDATOR.md` signs trails with an Ed25519 `vpk`. We bind them:
`registerValidator(vpk, sig)` proves control of `vpk` (an Ed25519 signature checked via the `0x402`
precompile) from the validator's BT wallet, so its submitted **completed‑trail proofs** (for the effort
bounty, §9.3) are attributable to that wallet and vice‑versa. (Reusing the BT wallet key directly as
`vpk` also works but couples key rotation to the wallet; binding is preferred.)

---

## 4. Notation

| Symbol | Meaning |
|---|---|
| `netuid` | the UR subnet id |
| `T_tempo` | tempo length in blocks (360) |
| `T_epoch` | UR settlement epoch in blocks (50 400 ≈ 7 days) |
| `e` | epoch index (monotone counter in the contract) |
| `D_n = SUM(DT)_n` | total α deposited by NO `n` during epoch `e` |
| `w_n` | deposit weight of NO `n` = `D_n / Σ_m D_m` |
| `E_mine, E_val, E_own` | α emission to the miner / validator / owner pools over the epoch (41/41/18% of subnet α emission) |
| `B_DT` | contract deposit balance from DTs over the epoch (= `Σ_n D_n`) |
| `Q_n` | NO `n`'s consensus‑measured pool quality (from validators' `VALIDATOR.md` trails) |
| `ŵ_n` | NO `n`'s consensus weight ∝ `deposit_n × Q_n` (pool / tail tier) |
| `Q_p` | top-level miner `p`'s consensus-measured quality (per-provider, from `VALIDATOR.md` trails) — **is** its head weight |
| `θ` | governance **head share**: fraction of the 41% miner emission steered to top-level miners; `1−θ` goes to the pools (§8.5) |
| `client_id ⇄ hotkey` | the signed binding mapping a measured `client_id` to a top-level miner's UID (§11.4) |
| `φ` / `ω` | non‑refundable deposit fraction / owner‑cut slice — both fund the effort bounty |
| `FeePool` | the epoch's effort‑bounty pool = `φ·Σ_n D_n + ω·OwnerCut` |
| `effort_v` | validator `v`'s verified, coverage‑weighted completed‑trail effort |
| `s_{n,p}` | NO `n`'s payout share for provider `p` (Σ_p s_{n,p} = 1) |

---

## 5. Time: tempo vs. the 7‑day epoch

Two cadences run concurrently.

### 5.1 Tempo cadence (chain‑native; weights + emission)

Each tempo (~72 min) the chain runs Yuma and drains α emission. Weight‑setting is **decentralized across
independent validators** — the change that makes Yuma do real work. Each tempo every validator (§9)
scores **every NO pool** from its own `VALIDATOR.md` trails and submits it under commit‑reveal:

```
each tempo, every independent validator v:
    for each NO pool p:  score[p] = deposit_p  ×  quality_v(p)      // quality from v's OWN trails
    normalize to u16 (max → 65535);  Neuron(0x804).commitWeights(...)   // reveal auto-fires later (§10)
```

The chain takes the **stake‑weighted median** of the validators' scores, **clips** outliers to
consensus, and emits to the miner‑pool UIDs ∝ the clipped result (§10) — so the miners' reward *is* the
validators' evaluation, the Bittensor way. A validator earns a permit by stake
(top‑`max_allowed_validators`) from its **own** α; honest, consensus‑aligned scoring builds vtrust and
dividends while divergence is clipped — and the §9.3 bounty additionally pays for the trail volume the
scoring requires. There is **no off‑chain keeper or trusted weight authority**; each validator runs
standard Bittensor validator software.

> **Commit‑reveal is enabled** (`commit_reveal_weights_enabled = true`). Weights carry a *subjective*
> quality signal, so a lazy validator could copy others' fresh weights; commit‑reveal (drand timelock,
> §2.4) hides each validator's scores until they are stale, so copying earns low vtrust — the standard
> Bittensor anti‑weight‑copying posture.

### 5.2 Epoch cadence (application‑layer; settlement)

The 7‑day epoch is the unit `seed/INCENTIVES.md` calls a "block." It is tracked by the contract as an
incrementing counter with a fixed block length `T_epoch`. The epoch lifecycle (timeline from
`README.md`):

```
  t = 0         Epoch e closes. Snapshot D_n (per-NO deposit totals) and the miner-pool emissions.
  t ≤ +4h       Each NO must commit its payout-list root for e (the share tree).  [README: "data ... 4h after"]
  t ≤ +24h      Each validator must submitTrails (its effort claims) for e.  [README: "validators ... 24h after"]
  +24h … +48h   Challenge window: anyone may dispute a committed root / effort leaf (§11.3).
  +48h          contract.finalizeEpoch(e): snapshot per-NO poolTotal + totalEffort. Claims open (no global root).
                Unclaimed α rolls into epoch e+1 (or a grace pool) after a TTL.
```

Deposits, emission weighting, and dividend capture happen continuously across the epoch at tempo
cadence; only **settlement** waits for the epoch boundary + windows.

---

## 6. The ST contract

A single Solidity contract (upgradeable behind a proxy; control & governance model in §6.4) on the
Subtensor EVM. It is simultaneously: a **coldkey** custodying α, the **deposit ledger**, the **emission
custodian** (it owns the miner‑pool UIDs and captures their incentive), the **bounty payer** (the
fee‑funded validator effort reward), and the **settlement/claims** engine. It is **not** the subnet's
validator — **independent validators** (§9) set the weights and earn dividends natively; the contract
holds and pays out the miner emission + the bounty.

**No-custody is a v1 must-have — in spirit, not immutability (D21).** The *foundation and NOs never hold or
distribute α*: the contract is the sole custodian of in-transit α and every payout is a **direct on-chain
pull claim** (`transferStake`), with the **head paid natively** (§8.4). All α moves on-chain; no person ever
holds participants' α in an off-chain wallet. This property is required at v1 — but it does **not** mean the
contract is frozen: for v1 the contract stays **upgradeable + owner-multisig + guardian** (normal bug-fix
latitude) and is progressively locked down (§6.4).

### 6.1 State (essential)

```solidity
uint16  public netuid;
bytes32 public treasuryHotkey;         // contract's own hotkey for staking idle/treasury α (NOT the subnet validator)
address public owner;                  // UR Foundation governance (multisig)
uint256 public epoch;                  // current epoch index e
uint64  public epochStartBlock;        // start of current epoch
uint64  public constant T_EPOCH = 50_400;

// --- registries ---
struct Operator {
    bytes32 coldkey;
    uint16  minerUid;  bytes32 minerHotkey;   // miner-pool UID: owned outright by THIS contract (accrual slot)
    bool    active;
}
mapping(uint256 => Operator) public operators;              // noId -> Operator
// validators: INDEPENDENT (no NO); own UID + own stake; earn NATIVE dividends + the effort bounty
struct Validator { bytes32 hotkey; bytes32 vpk; address operator; bool active; }
mapping(uint256 => Validator) public validators;            // valId -> Validator
mapping(bytes32 => uint256)  public vpkToValidator;         // vpk-hash -> valId

// --- per-epoch deposit ledger (α) ---
mapping(uint256 => mapping(uint256 => uint256)) public DT;  // epoch -> noId -> SUM(DT)_n
mapping(uint256 => uint256) public totalDT;                 // epoch -> Σ_n D_n
uint256 public phiBps;                                      // non-refundable deposit fraction φ (bps) -> fee pool
uint256 public omegaBps;                                    // slice ω of the owner cut -> fee pool

// --- per-epoch validator effort + bounty pool ---
mapping(uint256 => mapping(uint256 => bytes32)) public trailsRoot; // epoch -> valId -> committed Merkle root of (trail,coverage) leaves
mapping(uint256 => mapping(uint256 => uint256)) public effort;     // epoch -> valId -> claimed coverage-weighted effort (final after sample+dispute, §9.3/§11.3)
mapping(uint256 => uint256) public feePool;                 // epoch -> bounty pool = φ·ΣD + ω·OwnerCut

// --- per-epoch operator commitment, keyed (epoch, noId) ---
struct NoCommit { bytes32 payoutRoot; bytes off; }          // payoutRoot = Merkle root of (provider_coldkey, share) leaves, Σ share = 1
mapping(uint256 => mapping(uint256 => NoCommit)) public noCommit;

// --- per-epoch settlement: snapshotted at finalizeEpoch; NO global claim roots ---
mapping(uint256 => mapping(uint256 => uint256)) public poolTotal;   // epoch -> noId -> miner pool = emission_n + (1-φ)·D_n
mapping(uint256 => mapping(uint256 => uint256)) public claimedMiner; // epoch -> noId -> α already paid from pool n (≤ poolTotal)
mapping(uint256 => uint256) public totalEffort;                     // epoch -> Σ_v effort  (bounty denominator)
mapping(uint256 => mapping(bytes32 => bool)) public minerClaimedBy; // epoch -> keccak(noId,coldkey) -> claimed
mapping(uint256 => mapping(uint256 => bool)) public valClaimed;     // epoch -> valId -> bounty claimed
```

### 6.2 Interfaces (selected)

```solidity
// --- registration ---
function registerOperator(uint256 noId, bytes32 coldkey, bytes32 minerHotkey) external; // owner-gated v1; contract burnedRegisters the miner-pool UID (owns it)
function registerValidator(bytes32 hotkey, bytes32 vpk, bytes calldata ed25519Sig) external; // INDEPENDENT validator; binds vpk<->wallet; permissionless-with-stake

// --- deposits (α held as stake; see §6.3) ---
function deposit(uint256 noId, uint256 alphaAmount) external;   // DT  (gb/users optional off-chain metadata, §7.1)

// Weights are NOT set here: each INDEPENDENT validator signs its OWN commit/reveal setWeights via the
// Neuron precompile (§10), earning native dividends. The contract custodies only the MINER emission.

// --- per-epoch validator effort claims (within +24h) ---
function submitTrails(uint256 e, uint256 valId, bytes32 trailsRoot, uint256 claimedEffort) external; // optimistic: contract spot-checks a random sample via 0x402; any leaf disputable in-window (§9.3, §11.3)

// --- per-epoch operator publishing (within +4h) ---
function commitOperator(uint256 e, uint256 noId, bytes32 payoutRoot, bytes calldata off) external; // payoutRoot over (provider, share) leaves, Σ share = 1

// --- settlement (no global claim roots; amounts derive from on-chain state) ---
function finalizeEpoch(uint256 e) external;   // after +48h: feePool = φ·ΣD + ω·OwnerCut; snapshot per-NO poolTotal (emission via 0x802) + totalEffort
function claimMiner(uint256 e, uint256 noId, bytes32 coldkey, uint256 shareBps, bytes32[] calldata proof) external; // verify (coldkey,shareBps) vs payoutRoot[e][noId]; pay shareBps·poolTotal[e][noId], capped
function claimValidator(uint256 e, uint256 valId) external; // pays feePool[e]·effort[e][valId]/totalEffort[e] — computed on-chain, no root

// --- governance ---
function setHyperparam(...) external;                          // owner relays to subnet precompile
function setFeeParams(uint256 phiBps_, uint256 omegaBps_) external; // owner tunes φ, ω (§9.3)
```

### 6.3 How the contract holds and moves α

- **Emission capture (the key property).** The contract's coldkey **owns every NO's miner‑pool UID**
  outright (a pure accrual slot), so Yuma credits its incentive as **α stake the contract holds** — the
  **41% miner emission** lands in the contract automatically, with no action by and no custody by any NO.
  The contract reads realized per‑pool emission on‑chain (Metagraph `0x802` `getIncentive`/`getEmission`)
  to build settlement (§8).
- **Validator emission is native.** Independent validators stake their **own** α and earn their **41%
  dividends natively** to their own hotkeys ∝ stake × vtrust — the contract neither stakes for them nor
  custodies their dividends. **(X):** their *effort* is rewarded **additionally** by the fee‑funded
  bounty below, not by capturing this emission (§9.2, §13.6).
- **Fee pool + effort bounty.** Each epoch the contract reserves `φ·Σ_n D_n` (the non‑refundable deposit
  fraction) `+ ω·OwnerCut` (a governance slice of the owner's 18%) as the **`feePool`**, and at
  settlement pays it to validators **∝ their verified, coverage‑weighted trail effort** (`submitTrails`
  → `0x402` verify → `effort[e][valId]`; §9.3).
- **Custody as stake.** All α the contract holds (deposits + captured miner emission + the fee pool) is
  staked under its coldkey on `treasuryHotkey` (no AMM exposure; α stays α).
- **Deposits in.** A NO `approve`s the contract on the Staking‑V2 precompile, then calls `deposit`; the
  contract pulls with `transferStakeFrom(payer, contract, hotkey, netuid, netuid, amount)` —
  **slippage‑free** — credits `DT`, and routes `φ` of it to the fee pool.
- **Payouts out.** On claim, the contract pays with
  `transferStake(recipientColdkey, treasuryHotkey, netuid, netuid, alpha)` — again slippage‑free.
  Recipients receive α **as stake** they can keep (earning) or `removeStake` to TAO (their slippage).
- **TAO is only touched** for gas and the owner's discretionary conversions. Participants never pay AMM
  slippage to *use* the system; only to exit α→TAO.

### 6.4 Control, custody, and the launch governance model

Because the contract is **custody‑critical** (§13.1) — it holds all deposits and all captured emission —
*how it is controlled is part of the spec.* Control is a bundle of distinct privileged powers: **upgrade
authority** (the proxy admin), **admission** (`registerOperator` gating), **dispute/referee** decisions
(§9.6, §11.3), **parameters** (`φ`, `ω`, the coverage curve, epoch windows, hyperparameters), and the
**treasury** (the owner cut, less the `ω` slice that funds the bounty). We launch with these centralized
but *bounded* (Phase 0), then harden custody (Phase 1). Deeper decentralization —
trustless inputs, on‑chain governance, immutability, and handing off the Bittensor subnet‑owner role —
is deferred (§6.4.3) until the mechanism is proven (notably the `VALIDATOR.md` §10 defenses).

**Invariant across every phase — earned claims are sacrosanct.** Once `finalizeEpoch(e)` writes the
claim roots for epoch `e`, the α backing those claims is committed: **no upgrade, pause, or admin action
may block or claw back a finalized claim.** Per‑epoch settlement is append‑only; admin power reaches only
*future* epochs. This single invariant bounds the blast radius of every privileged power below, and it is
implemented from day one.

#### 6.4.1 Phase 0 — Launch (central control, fast bug‑fixes)

- **Owner = an M‑of‑N multisig** (UR Foundation + signers): the proxy admin and holder of every
  privileged role.
- **Upgradeable proxy** (transparent or UUPS) → the owner can patch settlement/claim logic and tune
  parameters. This is intentional: early bug‑fix and tuning capability, and central control. Honest
  consequence: **the owner can change the rules for *future* epochs** (never the in‑flight one, per the
  invariant). Accepted for launch.
- **Owner‑gated admission** (`registerOperator` owner‑only; verifiers permissionless‑with‑bond or
  gated) and **owner as referee** for the non‑cryptographic
  disputes (§9.6) — the cryptographic disputes (§11.3) already need no owner.
- **Treasury:** the 18% owner cut (less the `ω` slice that co‑funds the bounty, §9.3) accrues to the
  owner multisig (a governance treasury later, §6.4.3).

#### 6.4.2 Phase 1 — Harden custody (bound the owner without losing bug‑fixes)

The highest‑leverage step; it answers "the owner can affect the 7‑day rewards" by adding *delay and
visibility*, not by removing the ability to fix bugs.

- **Timelock on every upgrade and parameter change**, delay **≥ 1 epoch (target 2 epochs ≈ 14 days)**.
  Any change to reward logic or parameters is queued in public for a full epoch before it can take
  effect, so participants can audit it and `claim`/exit ahead of it. With the §6.4 invariant, the owner
  provably **cannot alter any epoch already in flight** — only announce changes to future ones, with
  notice.
- **Role split (least privilege):**
  - a **larger M‑of‑N multisig** holds the **timelock** (proposes/executes upgrades + parameter
    changes); and
  - a separate **guardian** holds **pause‑only** power — it can halt `deposit` / `submitTrails`
    (and, if necessary, the *opening* of new claims) to stop an exploit in progress, but **cannot move
    funds, change parameters, or upgrade**, and **cannot block claims for already‑finalized epochs**.
- **Emergency power is therefore strictly *pause, never seize or rewrite*:** the worst a compromised
  guardian can do is freeze new activity (a liveness incident), not cause a custody loss.

#### 6.4.3 Deferred to later phases (designed‑for, not in this version)

Recorded so the seam is built now: making the **inputs** trustless (median / α‑native oracle,
permissionless bonded admission, optimistic + cryptographic disputes so `finalizeEpoch` needs no owner
signature); **on‑chain governance** of the timelock (α‑stake‑weighted via the staking / `0x80D`
precompiles, or participant/reputation‑weighted — basis TBD); **immutabilizing** the custody/settlement
core behind bounded parameter governance; and transferring the **Bittensor subnet‑owner role** to a DAO
coldkey or via subnet **leasing/crowdloan**. To keep these cheap, **split the contract now** into an
(eventually frozen) **custody/settlement core** and a lighter‑governance **policy module** (admission,
oracle, parameters), so each future step is a module swap behind the timelock rather than a monolith
rewrite.

---

## 7. Deposits (the demand signal)

### 7.1 No on‑chain oracle — NOs simply deposit α

`seed/INCENTIVES.md` frames deposits as "per used GB and active user … based on the global fixed rate set by
an oracle." We **drop the on‑chain oracle entirely**: per‑GB / per‑user usage is **self‑reported and
unverifiable on‑chain**, so pricing it on‑chain buys nothing — the only quantity the protocol can act on
is *the α actually deposited*. A NO just calls `deposit(α)`; `SUM(DT)_n = D_n` is the signal, full stop.

The **"global fixed rate"** survives as an **off‑chain published reference** (a governance‑set
USD‑per‑GB / USD‑per‑user figure NOs use to *price their own customers and size their deposits*), never a
value the contract consumes. NOs may still report `(gb_n, users_n)` as **optional, unverified metadata**
for transparency, but it enters no contract computation. This removes a whole subsystem (`setRates`,
`rateGbAlpha`/`rateUserAlpha`, the TAO/USD feed) and a trusted input.

### 7.2 What a deposit means

The deposit *is* the claim: `D_n` (α) is the NO's costly, on‑chain bid for emission weight. The protocol
never verifies GB — infeasible and a non‑goal. The deposit is a **costly signal** funded by the NO's
**real customer revenue**, and a **non‑refundable fraction `φ`** of every deposit is taken as a true
cost (§12.1) so the signal cannot be cheaply round‑tripped through self‑dealing. The constraint is the
one `seed/INCENTIVES.md` names — *deposit cost is bounded by the NO's revenue, which reflects real usage* —
and its load‑bearing **independence assumptions** are stated explicitly in §12.

### 7.3 Publishing deposits

`seed/INCENTIVES.md`: "NO publishes list of their deposits and signs with wallet." No extra commitment is
needed: each DT is already an **on‑chain event** (`Deposit(epoch, noId, amount)`) signed by the
operator's tx, so `D_n` is publicly and authoritatively summable straight from chain state — that *is*
the signed, published deposit list. (Earlier drafts also committed a `depositSummaryHash`; redundant with
the events, now dropped.)

---

## 8. Miner channel: per‑NO pool, Yuma‑weighted by deposit × quality, Merkle‑claimed

### 8.1 Across operators — Yuma emits to the miner pools ∝ deposit × quality

Independent validators (§9–§10) score each NO miner‑pool UID `deposit_n × Q_n`, where `Q_n` is the
pool's aggregate provider quality from their `VALIDATOR.md` trails; Yuma medians/clips the scores and
emits to the pool UIDs — so the miners' reward *is* the validators' evaluation. NO `n`'s miner‑pool UID
accrues over the epoch

```
emission_n  ≈  0.41 · E_epoch · ŵ_n,   ŵ_n = consensus(deposit_n · Q_n) / Σ_m consensus(deposit_m · Q_m)
```

**as α stake the contract holds** (it owns the pool UID — the NO never receives it). Deposits anchor
`ŵ_n` to revenue‑backed demand (§7); the consensus‑measured `Q_n` modulates it — a NO with poor providers
earns less even at high deposit (with the swing capped at bootstrap, §12.3). The contract reads
`emission_n` on‑chain (`0x802 getIncentive`/`getEmission`) for settlement.

### 8.2 Within an operator — the payout list (the NO's lever, but auditable)

The NO does not hold the pool; it only **says how its pool splits** among its 100k+ providers —
`seed/INCENTIVES.md`: "The network operator determines the payout from their mining slots." The natural basis
(not mandated by the protocol) is

```
s_{n,p}  ∝  contracts_{n,p} · reliability_{n,p},        Σ_p s_{n,p} = 1
```

where `contracts_{n,p}` is the usage provider `p` served for NO `n` and `reliability_{n,p}` is the **same
`VALIDATOR.md` per‑provider signal** the validators aggregate into the pool quality `Q_n` (§9.4, §10). The
NO commits `payoutRoot` = a Merkle root over its `(provider_coldkey, share)` leaves (§11). Because the
validated paths are public, the list is **auditable**: a NO that pays idle providers over live ones is
detectable and bleeds quality consensus (lower `Q_n` → less emission, §8.1). This commitment is the NO's
entire on‑chain footprint for the miner channel — a *direction*, never custody.

### 8.3 Settlement — providers claim per‑NO, directly from the contract

At `finalizeEpoch` the contract snapshots, **per NO `n`**, its **pool total**

```
poolTotal_n  =  emission_n  +  (1−φ)·D_n        // miner emission (read on-chain, 0x802) + refundable deposit
```

(the non‑refundable `φ·D_n` funds the bounty, §9.3). There is **no global claim root**: a provider `p`
claims against its NO's *own* committed `payoutRoot`, which holds **fractional shares** `s_{n,p}` (Σ = 1),
and the contract derives the α amount from **on‑chain state**:

```
claimMiner(e, n, p, s_{n,p}, proof):
    verify (p, s_{n,p}) ∈ payoutRoot[e][n]
    pay   s_{n,p} · poolTotal_n              (slippage-free transferStake)
    require claimedMiner[e][n] + amount ≤ poolTotal_n      // a pool can't be over-drained
```

So the amount is a deterministic function of on‑chain state (the pool's emission + deposit) × the NO's
committed share — **nothing is computed off‑chain at finalize**, removing the one remaining "who computed
this root" trust step. The contract caps cumulative payout per pool at `poolTotal_n`: a NO whose shares
sum to > 1 just drains its own pool early (hurting its own providers — a reputation cost); shares < 1
leave a remainder that rolls over. A provider attached to several NOs makes **one claim per NO** (the
trade for dropping the global root). **Every α of the miner channel flows contract → provider; the
operator holds none of it.**

> **The pool is the unit of scale.** Providers number 100k+ and cannot be UIDs, so the *pool* is the
> on‑chain miner (one UID per NO) and the providers are paid *inside* it by Merkle claim. Yuma operates
> at the pool granularity (§10); the within‑pool split is §8.2.

### 8.4 Top-level miners — the direct head channel

The pool tier scales to 100k+ providers but pays them by a NO-directed Merkle split. The **top ~200
providers** *also* get the **canonical Bittensor treatment**: each claims **its own miner UID**, is
**steered directly by validators on pure measured quality**, and is **paid natively**. This is the merit
apex of §1 — and it is *more* trust-minimized than the pool (no operator in the payout path).

**Identity — the `client_id ⇄ hotkey` binding (§11.4).** Validators measure providers by `client_id` (the
`VALIDATOR.md` proof's hops, server-derived from the unspoofable source IP, `VALIDATOR.md` §8.1). To steer
the head they need `client_id → UID`. A provider that wants a top slot publishes a **dual-signed binding**
(§11.4) proving it controls **both** the `client_id` (its `VALIDATOR.md` Ed25519 key) **and** the hotkey
(its BT key); validators read it as a free state query and **fail-closed** if the hotkey is not a live UID.
This is the standard Bittensor "signed proof → registered hotkey" pattern (Epistula / ORO-AI
`bittensor-auth`); the dual signature (cf. SN51 Celium's `associate_evm_key`) stops a miner from claiming
a `client_id` it does not operate and stealing another provider's measured quality.

**Weight — pure quality, no deposit.** A top-level miner `p`'s weight is its **per-provider quality** `Q_p`
straight from `VALIDATOR.md` §7 — Wilson-scored step-completion (liveness) + latency percentiles,
**EMA-smoothed across epochs** (α ≈ 0.1) so a single noisy epoch doesn't thrash emission. There is **no
`deposit` term** — the head is a meritocracy; demand-coupling stays in the pool tier (§8.5). This also
**resolves the pool-quality-aggregation open question (§17) for the head**: per-provider `Q_p` *is* the
weight, so no roll-up to a pool scalar is needed (the `Q_n` aggregation question now bites only the tail).

```
for top-level-miner UID u (with bound client_id c):
    Q_p(u) = EMA_e( VALIDATOR.md §7 stats for c )      # Wilson liveness + latency percentiles, normalized
    head_weight[u] = Q_p(u)                            # no deposit factor
```

**Emission — native, direct to the provider.** Yuma credits a miner's incentive as **α stake on its own
hotkey under its own coldkey — no take, not shared with nominators** (verified against `run_coinbase.rs`).
So a top-level miner is paid **directly**: no contract custody, no Merkle claim, no NO middleman. (Child
hotkeys **cannot** route miner incentive — *"only the validation emission is split amongst parents"* — so
each top miner is genuinely its own UID; there is no native way to pool *miner* emission other than the
contract-Merkle machinery the pool tier already is.)

**Promotion / demotion — the chain's native tournament.** There is **no native "top-N keeps the slot"**
primitive; the only on-chain UID reallocation is **deregistration of the lowest-*emission* neuron** (tie →
oldest `reg_block` → lowest UID; owner/immune skipped) when a new `burned_register` hits the full subnet.
That *is* the tournament, driven by the weights validators set:

- **Promote.** A provider whose measured `Q_p` would out-earn the eviction floor `burned_register`s a UID;
  validators weight it on `Q_p`; it is **removed from its NO's `payoutRoot`** (promoted out — **no
  double-pay**; a provider is in exactly one tier at a time).
- **Demote.** A top miner whose `Q_p` decays earns the lowest emission, is pruned on the next registration,
  and **falls back to earning via its NO's pool** — the baseline catches it.
- **Quality-dip protection (a real risk).** Pruning reads only *current* emission rank, no history, so once
  a UID's `immunity_period` expires one bad stretch can evict a good provider. Mitigations, all
  owner-tunable: a **high `immunity_period`** (a full measurement ramp for new top miners), the **`Q_p`
  EMA** (above), and a **θ large enough that the lowest top miner clears the highest pool provider** (else
  the head thrashes against the tail, §8.5). Each (re-)registration pays the continuous burn auction
  (≈ ×1.26 / registration), so churn has a real, tunable cost — and a per-UID burn makes the head **more**
  Sybil-resistant than one pool UID per NO.

**Weight shaping (best practice for ~200 concurrent providers).** Steer **proportionally** to `Q_p`, *not*
winner-take-all (that suits single-best-answer contests, e.g. Apex SN1); **set `max_weight_limit`** to a
real cap (the chain default is *no cap*, so one provider could dominate the head); and sample/schedule
`VALIDATOR.md` trails so every top UID gets regular coverage (so honest-but-idle UIDs don't stale-decay).
This matches the strongest DePIN precedents — FileTAO's Wilson-interval scoring and TPN's robust latency
statistics.

### 8.5 The head/tail emission split θ

Both tiers are miner UIDs in **one** mechanism (§14), so the 41% miner emission is divided by the
**weights validators set** — and to make that a controllable policy rather than an accident, validators run
common software that **reserves a governance share θ to the head and `1−θ` to the pools**, exactly as Data
Universe (SN13) reserves a fixed share to one UID by rewriting weights before `set_weights`:

```
head[]  =  { Q_p(u) }            normalized so Σ head = θ
pool[]  =  { deposit_n × Q_n }   normalized so Σ pool = 1 − θ
w       =  head ⊕ pool           # one vector over all miner UIDs; commit-reveal; apply max_weight_limit
```

Both shares go to **real recipients** (top miners; contract-owned pool UIDs), so the **June-2026
`(1 − miner_burned)` penalty does *not* apply** — that penalty only bites emission *withheld to an
owner/immune key* (Spec 421, subtensor PR #2781). **Do not "reserve baseline" by burning to an owner UID** —
it would shrink the subnet's whole cross-subnet allocation. Because Yuma clips to the κ-stake-weighted
median, θ takes effect only if a **stake-majority of validators run the same θ** — so θ is a *published
governance parameter*, not per-validator discretion.

**θ is the load-bearing new decision, because it trades the two bets against each other.** Demand-coupling
(`deposit × quality`, the headline bet of `COMPARISON.md`) lives entirely in the **`1−θ` tail**; the head is
pure merit, *decoupled* from deposits.
- **Large θ (head-heavy):** a big, trust-minimized meritocracy — but most emission is demand-*de*coupled,
  and a NO's best providers then earn from the subnet-funded head, not the NO's deposit-funded pool, so NO
  **deposit incentives weaken**.
- **Small θ (tail-heavy):** demand-coupling stays dominant and deposits stay meaningful — but the apex is a
  small carrot, and graduating can be a *pay cut* (breaking the ladder).

**Recommendation:** govern θ; **start tail-weighted (θ ≈ 0.3)** — the pool is the stated on-ramp/baseline
and demand-coupling is the strategic edge — **instrument realized per-tier pay**, and **widen θ as the
top-miner set and the independent-validator quality consensus mature** (the same "ramp the strength, not the
mechanism" posture as the bootstrap quality-swing cap, §12.3 / D14). Hard constraint: size θ so the
**lowest-paid top miner ≥ the highest-paid pool provider**, or graduation is a pay cut and the head thrashes
against the tail.

---

## 9. Validator channel: independent validators, native dividends + an effort bounty

Validators are **independent** — no NO owns or pools them (the per‑NO validator pool, the NO↔V
intersection split, and the per‑path VT of earlier drafts are gone, §13.6). A validator is the source of
the subnet's core data — **which providers are the weakest links** (`VALIDATOR.md` §7) — so it is paid
for *both* accurate scoring **and** trail effort. ("Verifier" and "validator" are now one role.)

### 9.1 What a "validated path" is

A completed `VALIDATOR.md` trail yields a **published proof** `{header, hops[(client_id, time)],
final_sig (NO server), verifier_sig (vpk)}`, with identity

```
pathId = keccak256(trail_id ‖ vpk ‖ server_key_id)
```

Because it carries **both** the NO server's `FINAL` signature and the validator's `vpk` signature, a
path that verifies is **self‑proving** — anyone checks it with the `0x402` precompile (§11.3). This is
what the contract counts for the effort bounty (§9.3) and what the failure statistics are built from
(§9.4).

### 9.2 Two reward streams

A validator stakes its **own** α (the Sybil ante + the Bittensor permit qualifier — this absorbs the old
"verifier bond"), runs `/verify` trails, and each tempo scores every NO miner pool `deposit × Q_n` under
commit‑reveal (§10). It earns:

1. **Native Yuma dividends** (∝ stake × **vtrust**) — its share of the 41% validator emission, flowing
   **natively** to its own hotkey (the contract does **not** custody it; an independent validator has no
   NO middleman to route around). Rewards **accurate, consensus‑aligned scoring**.
2. **An effort bounty** (∝ verified, coverage‑weighted completed trails) — from a contract‑held **fee
   pool** (§9.3). Rewards **trail volume**, i.e. producing the failure data.

Stream 1 keeps the weights honest (on‑chain Yuma); stream 2 keeps the *data flowing*. (**(X), the chosen
start:** the bounty is funded from fees, on top of native dividends; if effort proves under‑incentivized
we escalate to routing the validator emission itself through the bounty — design note §13.6.)

### 9.3 The effort bounty (the engine for the data)

The bounty pool each epoch is

```
FeePool  =  φ · Σ_n D_n        (the non-refundable deposit fraction, §7.2)
          + ω · OwnerCut       (a governance slice ω of the 18% owner cut — the owner buys data)
```

A validator submits a **Merkle root** of its `(trail, coverage)` leaves plus a claimed effort total
(`submitTrails`); the contract accepts it **optimistically**, **spot‑checks a random sample** of leaves
(each via `0x402`), and lets anyone **dispute any leaf** in the +24–48h window — one invalid
`FINAL`/`vpk` signature voids the claim and forfeits the validator's stake at risk (§11.3). This keeps
the effort claim **`O(1)` on‑chain** at real trail volume (verifying *every* trail on‑chain would not
scale). Credited effort and the bounty are

```
effort_v  =  Σ over v's trails of  Σ over server-assigned completed hops  coverage(hop)
bounty_v  =  FeePool · effort_v / Σ_w effort_w
```

with three deliberate weightings:

- **Server‑assigned hops only** — exclude the validator‑chosen seed (`VALIDATOR.md` §7.6); the server
  assigns the trail's hops at random (`VALIDATOR.md` §5.1), so a validator **cannot farm the bounty
  through providers it favors**.
- **Coverage‑weighted by under‑sampling** — a hop through an **under‑sampled** provider (few recent
  samples, `VALIDATOR.md` §5.3) pays more, so effort is pulled toward the **gaps in coverage**, not raw
  volume. (Failures are read as the *byproduct* of this maximal effort, `VALIDATOR.md` §7.1 — we never
  pay for failures directly.)
- **Crypto‑verified (by sample + dispute)** — a trail credits effort only if its signatures verify, so
  effort **cannot be fabricated** without real trail‑walking (bounded by `VALIDATOR.md` §5.4).

The bounty needs **no claim root**: `effort[e][valId]` is on‑chain (after the sample + dispute), so
`claimValidator(e, valId)` pays `feePool[e] · effort[e][valId] / totalEffort[e]` computed on‑chain
(§11.2).

### 9.4 The failure data — what we derive (the point)

The completed and failed trails, aggregated with `VALIDATOR.md` §7 (per‑transition attribution,
Wilson‑smoothed liveness, latency percentiles), yield **per‑provider** liveness/latency/failure
attribution — *which providers are the weakest links*. This feeds **(a)** each NO's within‑pool payout
list (`reliability_{n,p}`, §8.2) **and (b)** each pool's cross‑operator quality `Q_n` (§8.1, §10) — i.e.
it drives the miner emission — and is published as a public good. Because §9.3 pays validators
**precisely for producing this** — weighted toward the coverage gaps — the data gets *more complete the
more the bounty is funded*, which is the whole reason to keep the validator incentive strong.

### 9.5 Anti‑gaming for validators

- **Honest scoring** — on‑chain Yuma: a validator inflating a pool (or knifing a rival) is **clipped** to
  the κ‑median and **loses vtrust → loses dividends**; the **self‑weight mask** (§10) stops it scoring its
  own NO at all; **commit‑reveal** stops it copying fresh consensus.
- **Honest effort** — §9.3: crypto‑verified (sample + dispute), server‑assigned, under‑sampling‑weighted
  trails can't be faked or farmed.
- **Independence** — because most validators run no NO, the κ‑median tracks ground truth (`VALIDATOR.md`
  §1) — the structural defense against a NO colluding with a validator to fake its own `Q_n` (further
  bounded by `VALIDATOR.md` §5.4).

### 9.6 The owner's role

The owner is the governance **referee**, not a party to a per‑path split (the old "disagreement → owner"
went away with the intersection split). It **co‑funds the bounty** — the slice `ω` of its 18% cut, its
*skin in the data* — and tunes `φ`, `ω`, the coverage weighting, and the §12.3 caps. It reviews the
**statistical** disputes (`VALIDATOR.md` §7.7) that the on‑chain crypto layer can't decide (self‑dealing
patterns, adversarial abandonment) and drives the `VALIDATOR.md` §10 roadmap. Per `seed/INCENTIVES.md`, the
residual "how much the owners mistrust the NOs / NOs mistrust each other" is now read from **consensus
divergence + the disputes**, not from a contested‑value pot.

### 9.7 Validators are permissionless and Bittensor‑native

Entry is the standard path: **stake α, earn a permit (top‑k by stake), validate** — no NO, no owner
approval, no pool. Most validators run no NO, which is exactly the **independence** `VALIDATOR.md` §1
needs and the structural defense against self‑dealing (§9.5, §12.3). Cultivating a broad, independent
validator set — and weighting that independence in governance — is a primary v1 goal and a down payment
on the `VALIDATOR.md` §10 roadmap. (Earlier drafts split validators into "NO pools" vs "community"; there
is now **one** kind — independent — so the distinction is gone.)

---

## 10. Setting weights: two channels (deposit × quality + pure quality), by validator consensus

Each tempo **every independent validator** (§9) scores **both miner tiers** — the NO pools *and* the top-level miners (§8.4–8.5) — from its own
`VALIDATOR.md` trails and submits the vector under commit‑reveal — so the validators' evaluation is what
moves the miner emission (the Bittensor mechanism):

```
for validator v:
    # TAIL — NO pools (unchanged): deposit × quality
    for each NO pool p (miner-pool UID of NO n):
        pool[p] = deposit_p · quality_v(p)              // §9.4 aggregate; = 0 if v operates NO n (self-mask)
    # HEAD — top-level miners: pure quality (§8.4)
    for each top-level-miner UID u (client_id c bound to u, §11.4):
        head[u] = Q_p,v(u) = EMA( v's VALIDATOR.md §7 stats for c )   // no deposit term; = 0 for v's own UID
    # split by governance share θ, into ONE vector (§8.5)
    normalize head so Σ head = θ ;   normalize pool so Σ pool = 1 − θ
    w_v = (head ⊕ pool) to u16 ;   apply max_weight_limit
    commit / reveal w_v   (Neuron 0x804, drand timelock — §2.4)
```

Yuma combines the validators' vectors with their stake:

- **Consensus & clipping.** Per pool the chain takes the κ‑stake‑weighted **median** of the scores and
  **clips** each validator to it. A validator inflating a pool — or scoring its own NO (masked) — is
  clipped away, earns nothing for the move, and loses vtrust. This is the **structural self‑dealing
  defense** (§12): no minority of stake can move a pool's reward.
- **Incentive → miner pools.** `incentive_p ∝ Σ_v stake_v · clipped_score_v(p)`, so a pool's emission
  tracks `deposit_p × consensus‑quality_p`: **deposits anchor it to revenue‑backed demand (§7), and the
  measured pool quality modulates it** — a NO with poor providers earns less even at high deposit (swing
  capped at bootstrap, §12.3). The *within*‑pool split to providers is the separate per‑provider step
  (§8.2). **Head:** `incentive_u ∝ Σ_v stake_v · clipped Q_p,v(u)`, so a top-level miner's emission tracks
  its **consensus quality** alone (no deposit), paid **natively** to its own hotkey (§8.4); the θ split
  (§8.5) sets how the 41% divides between the head and the pools.
- **Validator rewards = dividends + bounty.** Native dividends grow with bonds on pools consensus later
  rewards (Liquid Alpha) and with vtrust (accurate scoring) — the **Bittensor‑native** reward for good
  evaluation — **plus the §9.3 effort bounty** for the trail volume that scoring requires. Accurate
  scoring *and* trail effort both pay.
- **Anti‑copying.** Commit‑reveal hides fresh scores, so a lazy validator copying stale consensus drifts
  from current quality and loses vtrust (§5.1).

Hyperparameters: `commit_reveal_weights_enabled = true`, `liquid_alpha_enabled = true` (reward early
pool discovery), `max_weight_limit` set to a real cap (chain default is *no cap*) so no single UID dominates either tier, `mechanism_count = 1` (a 2nd mechanism would halve the 256-UID space, §14), `weights_version_key` bumped to
force validator‑software upgrades (§15.1).

> **Why this is real Yuma.** With a sole validator the consensus would be inert; with **many independent
> validators** — most running no NO — scoring the pools, median / clip / vtrust / bonds all do their job,
> and that independence is the disinterested baseline that keeps the consensus honest (§9.5). The §9.3
> bounty ensures they actually run the trails the scoring depends on; the consensus is what turns those
> scores into the miners' pay.

---

## 11. The data layer: commitments, Merkle, and disputes

### 11.1 What goes on‑chain vs. off‑chain

| Datum | Where | Why |
|---|---|---|
| `D_n`, deposit events, `effort[e][valId]`, `poolTotal[e][n]` | contract storage | pools, the bounty denominator, and claim *amounts* — all on‑chain |
| `payoutRoot[e][noId]` (fractional shares, Σ = 1) | contract storage (in `commitOperator`) | the contract verifies each provider's *share* against it at claim time |
| payout‑share leaves, completed‑trail proof blobs | **off‑chain** (IPFS/HTTPS, pointer in `off`); trails go to `submitTrails` | bulk data; only the roots / verified effort are trusted |
| public mirror of roots | **commitments pallet** (optional, free) | SDK‑native public audit without touching the contract |

This directly answers `seed/INCENTIVES.md`'s open question: **yes**, each NO commits a **Merkle root** of its
payout table (fractional shares) so every provider verifies *its own* payout with an `O(log N)` proof,
with no bulk data on chain. The contract holds each NO's payout root and the on‑chain pool total, and
derives the α at claim time (§8.3) — there is **no global, off‑chain‑computed claim root**.

### 11.2 Claiming

```solidity
function claimMiner(uint256 e, uint256 noId, bytes32 coldkey, uint256 shareBps, bytes32[] proof) external {
    bytes32 leaf = keccak256(abi.encode(coldkey, shareBps));
    require(MerkleProof.verify(proof, noCommit[e][noId].payoutRoot, leaf), "bad proof");
    bytes32 k = keccak256(abi.encode(noId, coldkey));
    require(!minerClaimedBy[e][k], "claimed");  minerClaimedBy[e][k] = true;
    uint256 amt = shareBps * poolTotal[e][noId] / 10_000;
    claimedMiner[e][noId] += amt;  require(claimedMiner[e][noId] <= poolTotal[e][noId], "pool over-drained");
    _payAlpha(coldkey, amt);       // slippage-free transferStake
}

function claimValidator(uint256 e, uint256 valId) external {   // no root — the amount is on-chain
    require(!valClaimed[e][valId], "claimed");  valClaimed[e][valId] = true;
    uint256 amt = feePool[e] * effort[e][valId] / totalEffort[e];
    _payAlpha(validatorColdkey(valId), amt);
}
```

Both are pull‑based, so settlement is `O(1)` on‑chain regardless of participant count. The **miner**
amount is `share × poolTotal` (share proven against the NO's committed root, pool total read from
on‑chain state); the **validator** bounty is `feePool · effort / Σ effort` — pure on‑chain arithmetic.
**Neither needs a global claim root computed off‑chain.**

### 11.3 Disputes

Effort claims are **optimistic**: a validator commits a Merkle root of its trails + a claimed effort
total, and the claim stands unless challenged in the +24h…+48h window. The checks are **cryptographic
and cheap**:

- **Sampled + disputable effort verification.** At submission the contract `0x402`‑verifies a **random
  sample** of the committed leaves (`FINAL` against the NO's server key, `verifier_sig` against the
  validator's `vpk`); during the window **anyone may dispute any leaf** with the same `0x402` check. A
  single failing leaf **voids the whole claim** and forfeits the validator's stake at risk — so a
  fabricated trail is never worth the gamble, and the contract never has to verify *every* trail
  (it scales).
- **Bad payout share.** A provider's claim must prove `(coldkey, share)` against its NO's committed
  `payoutRoot`; the per‑pool cap (`claimedMiner ≤ poolTotal`) means a NO whose shares sum to > 1 only
  drains its own pool. (Validator bounties need no proof — the amount is on‑chain.)

Statistical disputes (a validator's trails look self‑dealt or coverage‑gamed, `VALIDATOR.md` §7.7) are
**not** resolved on‑chain in v1; they inform governance (validator de‑listing, stake forfeiture). The
on‑chain layer handles only what is cryptographically decidable.

### 11.4 The `client_id ⇄ hotkey` binding (top-level miners)

To steer the head (§8.4), a validator must map each measured `client_id` to a top-level miner's UID. The
binding is **published, signed, and cheap to read**, using the standard Bittensor "signed proof →
registered hotkey" pattern (Epistula / ORO-AI `bittensor-auth`) — with a **dual signature** so a miner
cannot claim a `client_id` it does not operate.

**Dual-signed association.** A provider claiming a top slot proves control of **both** keys:

```
msg        = "urnetwork/bind/v1" ‖ client_id(16) ‖ hotkey_ss58(32)
sig_client = Ed25519.Sign(client_sk, msg)   // client_sk = the per-client key (VALIDATOR.md §2 vpk/ckey) → proves client_id ownership
sig_hotkey = sr25519.Sign(hotkey_sk, msg)   // proves UID / hotkey ownership
```

This is the shape of SN51 Celium's anti-theft `associate_evm_key` (both keys sign the linkage); a *single*
hotkey signature would let a miner **steal another provider's measured quality** by claiming its `client_id`.

**Where it lives.**
- **Commitments pallet** (free, `Pays::No`, keyed `(netuid, hotkey)`, §2.4): the miner hotkey commits its
  `client_id`(s) — a short list, or a Merkle root if it runs several. Validators read it as a **free state
  query** and build `client_id → UID`, **failing closed** if the hotkey is not a live UID (a stale-snapshot
  guard).
- **ST-contract anchor** (for disputes): the contract stores the binding (or its hash) and adjudicates a
  contested/stolen binding on-chain — verifying `sig_client` via the **`0x402` Ed25519 precompile** and
  hotkey ownership via a metagraph read — reusing the §11.3 dispute rail. A bad binding is slashable.

**Identity ⊥ quality.** The binding proves *ownership* only; `VALIDATOR.md` proves *quality*. They stay
separate (as every comparable subnet does — Targon deliberately keeps the hotkey out of its TEE
attestation): a validator attributes a trail to a UID via the binding, then scores that UID via
`VALIDATOR.md` §7.

**Privacy — opt-in self-deanonymization.** Publishing `client_id → hotkey` (→ egress IP, `VALIDATOR.md`
§8.1) **does** deanonymize, so it is **voluntary and only for providers claiming a public top slot** —
claiming the public UID *is* the consent. The long tail stays `client_id`-pseudonymous inside the pools.
(Optionally the NO `/verify` server — which already co-signs trails and authoritatively knows
`client_id ⇄ egress-IP` — can add a third attesting signature; it strengthens the binding at the cost of
NO-trust, and is not required.)

---

## 12. Economic analysis

### 12.1 Operator equilibrium

Let a NO with real customer revenue `R_n` (α‑equivalent) choose deposit `D_n`. Over an epoch the
**contract pays that NO's providers** `(1−φ)·D_n` (passed‑through deposit) `+ 0.41·E·ŵ_n` (the NO's
miner emission, `ŵ_n ∝ deposit_n × Q_n`), and the NO keeps `R_n − D_n` off‑chain. Providers attach where
expected pay is highest. Two forces balance:

- **Raising `D_n`** lifts the NO's emission share `ŵ_n` and the α the contract pays its providers →
  attracts providers and capacity → supports more real usage → more `R_n` **and** higher measured
  quality `Q_n` (which feeds back into `ŵ_n`).
- **`D_n ≤ R_n`** in the long run: deposits not backed by customer revenue are pure loss (the deposit
  flows to *providers*, never back to the NO; the NO only recovers value if that provider capacity
  earns it real off‑chain revenue).

The emission `E` is a subsidy auctioned ∝ deposits: in equilibrium NOs bid deposits up to where the
marginal emission α equals the marginal cost of fronting α not yet covered by revenue. This ties total
deposits to total real demand and makes `w_n` track genuine usage share — the intended outcome.

**Self‑dealing — defended in depth.** The worry is a NO that runs its *own* providers so its deposit
round‑trips to itself. Three things stand in the way: **(1)** the **non‑refundable `φ·D`** never
round‑trips (a hard cost floor, tunable); **(2)** emission is `deposit × Q_n`, so the self‑dealer must
also win **quality consensus** — fool a κ‑stake majority of **independent validators** (§9.7, who have no
pool and measure without bias), while the **self‑weight mask** (§10) stops it scoring its own NO at all;
and **(3)** what remains is bounded by `0.41·E·(D/ΣD) − φ·D − (real infra to pass live trails)`, which the
owner tunes via `φ`, `E` (TAO inflow), and the bootstrap cap on the quality swing. Pure wash deposits
with no real, independently‑verified service are unprofitable once `φ` and the independent‑validator
baseline are non‑trivial. The honest residual (a determined self‑dealer with real infra *and* stake) is
the `VALIDATOR.md` §10 class, exactly what a broad independent validator set + that roadmap close (§12.3).

### 12.2 Validator equilibrium

A validator earns **native dividends** (∝ stake × vtrust — the Bittensor‑native reward for accurate
scoring) **plus an effort bounty** (∝ verified, coverage‑weighted trails, §9.3) for trail volume; its
profit is `dividends_v + bounty_v − (cost of running trails)`. Two levers keep effort high: (1)
commit‑reveal makes stale copying lose vtrust, so accurate scoring (hence trails) is needed to hold
dividends (§5.1); (2) the bounty pays **directly** for trails, sized by `φ` and `ω`, with coverage
weighting steering effort to under‑sampled providers. **(X):** start with the bounty as the explicit
effort lever; if observed trail coverage is too thin, governance raises `φ`/`ω` or escalates to **(Y)** —
routing the validator emission itself through the effort split (§13.6).

### 12.3 What this does and does not secure

- **Secured:** cross‑operator emission tracks `deposit × consensus‑quality` (costly, revenue‑backed
  demand × independently‑measured liveness) — **validators' evaluation drives the miner payout**, the
  Bittensor mechanism — via median + clipping + vtrust over many **independent** validators, plus the
  self‑weight mask and the `φ` cost floor (§10, §12.1); **validator effort** via the coverage‑weighted
  bounty over crypto‑verified (sample + dispute) trails (§9.3); provider quality also bites **within the
  pool** (auditable payout list) and via reputation.
- **Bootstrap caveat (not a removal — a ramp):** `Q_n` is noisy until the validator set and data mature,
  so governance **caps the quality swing** early (closer to deposit‑weighted) and widens it as the
  independent‑validator stake share grows. Quality is on the payout path from day one; its *magnitude*
  ramps up safely.
- **Not fully secured in v1 (inherited from `VALIDATOR.md` §10):** per‑hop self‑dealing, adversarial
  abandonment, and "teaching to the test" — *to the degree the validator population is not yet
  independent.* Much weaker than a sole‑validator design but not eliminated. Rewards stay **provisional**
  until the §10 structural defenses (proof‑of‑routing, destination diversity, validator Sybil resistance)
  land; a broad independent validator set is the primary lever (§9.7).
- **Effort‑incentive risk (the (X) bet):** native dividends are ∝ stake×vtrust, so if the bounty is too
  small a high‑stake validator could under‑measure and coast on consensus. Sizing `φ`/`ω` so the bounty
  rivals the 41% dividends is the mitigation; **(Y)** (§13.6) is the escalation — the one place v1 starts
  light by choice.

---

## 13. Design decisions and alternatives

### 13.1 Settlement: contract custodies the miner pools; validators are independent (chosen)

The contract **owns each NO's miner‑pool UID** outright, so the **tail's share of the 41% miner emission**
accrues to the contract and is paid out by direct Merkle claim (the **head is native** — top-level miners
own their UIDs and are paid to their own coldkey, §8.4) — *a network operator never custodies emission destined
for its providers* (the hard requirement). The **weights are set by independent validators** (§9, §10),
not the contract, so Yuma consensus does real work; their **41% dividends are native** (no middleman to
remove), and their *effort* is paid by the fee‑funded bounty (§9.3, §13.6). Implications: the contract is
**custody‑critical** for the miner emission + deposits + fee pool (audited code; §6.4 timelock/guardian
governance), and it owns **one miner‑pool UID per NO**, so budget `max_allowed_uids` and registration
burns to the **NO count** — providers are *not* UIDs, they live inside the pools. No α→TAO→α churn.
*Rejected:* **per‑provider UIDs** (100k+ ≫ the ~256 cap — the reason for pools, though the **top ~200 do get their own UID** — the head tier, §8.4); letting emission land on
NO hotkeys (violates no‑custody); a **single** contract miner UID with the contract as sole validator
(collapses Yuma); and the earlier **per‑NO validator pool with a take‑0 custody hack** (fragile, and
redundant with crypto‑validity — replaced by independent validators + the §9.3 bounty, §13.6).

### 13.2 Payment token: α (chosen) vs. TAO vs. USDC

α aligns the contract with subnet value, creates buy/stake pressure, and keeps all internal transfers
**slippage‑free** (`transferStake` within‑netuid). Cost: participants bear α price risk while holding,
and exit (α→TAO) has AMM slippage. TAO/USDC would remove volatility but α is not a liquid EVM token,
TAO settlement forfeits the alignment, and USDC adds a bridge dependency. α chosen per the approved
direction; the off‑chain reference rate (§7.1) lets NOs target a *fixed real* price despite α volatility.

### 13.3 Emission steering: multi‑validator Yuma consensus (chosen)

The cross‑NO split is a genuine on‑chain **consensus output**: many independent validators score
`deposit × quality` (§10), so median / clipping / vtrust / bonds all operate. This is the **Bittensor
mechanism** — validators evaluate, and their evaluation (not a fixed formula) drives the miners' pay; a
design where validator input is *off* the payout path would miss the point of Bittensor. The cost is the
standard subjective‑weight toolkit (commit‑reveal, self‑mask, Liquid Alpha), switched on, plus a healthy
independent validator set — which §9.7 cultivates and the §9.3 bounty funds. *Rejected (briefly
explored):* a **deposit‑only** weight — simpler, but it takes validators off the miner payout path and
reduces them to a side‑channel, so it was reverted.

### 13.4 Quality in the cross‑operator weight: **adopted** (ramped at bootstrap)

`weight_n ∝ deposit_n × Q_n`, with `Q_n` the consensus‑measured pool quality (§8.1, §10): deposit is the
objective demand anchor, quality is the earned modulator, and together they make validators' evaluation
the thing that moves the money. The one nuance is **magnitude at bootstrap**: `Q_n` is noisy until the
validator set + data mature, so governance **caps the quality swing** early (closer to deposit‑weighted)
and widens it as the independent‑validator stake share grows (§12.3). So quality is on the payout path
from day one — we ramp its *strength*, we do not defer the mechanism.

### 13.5 No on‑chain oracle (simplified out)

Because per‑GB / per‑user usage is self‑reported and unverifiable on‑chain, an on‑chain rate has no
enforcement power — the weight is just *α deposited* (§7.1). v1 therefore has **no oracle**: the "global
fixed rate" is an off‑chain governance‑published reference NOs use to price customers and size deposits.
(If a future version ever needs an on‑chain α/USD value — e.g. to denominate the deposit fee in USD —
the `0x808` α price is already trustless and only TAO/USD would need a committed validator‑median feed.)

### 13.6 Validator effort reward: fee‑funded bounty now (X), emission‑routed later (Y)

The validators' output — *which providers are the weakest links* — is the product, so validator **effort**
must be strongly rewarded. Yuma pays validators **∝ stake × vtrust**, which is effort‑agnostic, so we add
an explicit effort reward. Two ways to fund it:

- **(X) — chosen for v1.** A **fee‑funded bounty** (`φ·ΣD + ω·OwnerCut`) paid ∝ verified
  coverage‑weighted trails (§9.3), **on top of** native dividends. Keeps validators as **independent
  on‑chain UIDs running real Yuma consensus** (median/clip/vtrust intact) and needs **no emission
  capture** — the simplest, most Yuma‑native option. Risk: the bounty is only as large as the fee pool,
  so the effort incentive is bounded (§12.3).
- **(Y) — the escalation.** Route the **41% validator emission itself** through the effort split — the
  contract captures it and pays ∝ trails. Strongest effort incentive, but capturing requires
  contract‑owned validator UIDs, which moves the quality consensus **into the contract** (robust median
  of submitted crypto‑verified scores) instead of on‑chain Yuma. We move to (Y) only if (X)'s observed
  trail coverage is too thin.

*Eliminated with this decision:* the per‑NO validator pool, the NO↔V **intersection split**, **VT**, the
verifier **bond**, `attestedPathsRoot`, and the **take‑0 custody hack** — replaced by one rule, *more
verified useful trails → more pay*. The intersection split was in any case redundant for fraud detection
(a valid path is co‑signed = agreed by construction; an invalid one is caught by the `0x402` check, §11.3)
and was a weak effort proxy; the bounty is a direct, stronger one.

### 13.7 Two miner tiers: pool on-ramp + direct top-level miners (chosen)

The miner side runs **both** a per-NO pool (§8.1–8.3) *and* a direct top-level-miner channel (§8.4), in
parallel. *Why both:* a new provider needs a low-barrier **place to start with a baseline reward** (the
pool — no UID, no burn), while the best providers deserve the **canonical, trust-minimized** Bittensor
treatment (their own UID, steered directly, paid natively). A provider **starts in a pool and graduates**
to a top slot, the chain's deregistration churn running the tournament. This is **novel** on Bittensor: the
field has the pool pattern (ComputeHorde, TPN, Vanta) *and* the direct-UID pattern, but **no subnet tiers
them** — the norm is the opposite, *consolidate* behind one UID (Chutes: "never register more than one
UID"); and every pooled tail elsewhere is paid **off-chain at operator discretion**, where ours is
trustless (`COMPARISON.md`). *Rejected:* head-only (discards the 100k-provider on-ramp and the demand
signal); pool-only (the v0.1 design — leaves the best providers trusting a NO's split, one of thousands);
and making top-level miners *contract-pooled* too (redundant — that is just another NO pool, and child
hotkeys **cannot** route miner emission to sub-workers anyway, §8.4).

### 13.8 Head/tail split θ in one mechanism (chosen); not two mechanisms, not owner-burn

The two tiers share **one mechanism's** 256-UID metagraph, and the 41% miner emission divides between them
by a governance share **θ** baked into validator software (SN13-style weight reservation, §8.5).
*Rejected:* **two sub-mechanisms** (mechanism 0 = pools, mechanism 1 = top miners) split by
`mechanism_emission_split` — clean in theory, but `mechanism_count × max_UIDs < 256` would **halve the UID
space to ~127**, too few for ~200 top miners (so mechanisms stay reserved for the product-line split, §14);
and **reserving the split by burning to an owner/immune UID** — post-Spec-421 the `(1 − miner_burned)` term
would shrink the subnet's whole cross-subnet allocation (§8.5). θ trades demand-coupling (in the tail)
against the merit apex (the head); it is a governed dial, started tail-weighted and ramped (§8.5, §12.3).

---

## 14. Multi‑pool structure (Pool 0 / Pool 1)

`README.md` describes **Mining Pool 0 / Validator Pool 0** ("the core network") and **Mining Pool 1 /
Validator Pool 1** ("the VPN factory", vpn.dev). Bittensor's **sub‑mechanism** feature (a subnet may
run multiple mechanisms, each with its own weight matrix and bond pool, with an owner‑set
`mechanism_emission_split`) maps onto this directly:

- **Pool 0 = mechanism 0 (core network).** Fully specified by this document.
- **Pool 1 = mechanism 1 (VPN factory).** Same contract, same α, same role types; its own miner‑pool
  UIDs and deposit/payout accounting, with validators scoring per‑mechanism (`set_mechanism_weights`).
  The owner sets the α split between pools.

The ST contract namespaces all per‑epoch state by `(poolId, epoch, …)`; everything in §§5–12 applies
per pool. v1 launches Pool 0; Pool 1 is added by registering the second mechanism and enabling its
accounting — no new mechanism design required.

**Tiers ≠ mechanisms — and the UID budget.** The two *miner tiers* (pool on-ramp + top-level miners,
§8.4–8.5) live **within one mechanism**, divided by the weight-level share θ — *not* by
`mechanism_emission_split`. Mechanisms are reserved for the **product-line** split (Pool 0 core / Pool 1
VPN factory), because `mechanism_count × max_UIDs < 256` means a 2nd mechanism halves the per-mechanism UID
space to ~127 — too few for ~200 top miners. So within Pool 0's one mechanism the **256 UIDs are shared**:
`256 ≥ (top-level miners ~200) + (one pool UID per NO) + (validator UIDs)`. Reserve `V` validator slots and
`P` NO-pool slots; the head is `256 − V − P` (e.g. V=36, P=20 → 200). Validators are not a fixed partition
— they are the UIDs holding a permit (top-k by stake), so stand up far fewer than `max_allowed_validators`
and the rest of the 256 are miners.

---

## 15. Concrete parameters

### 15.1 Subnet hyperparameters (owner‑set at creation; verify live before launch)

| Hyperparameter | Value | Rationale |
|---|---|---|
| `tempo` | **360** | native ~72‑min weight/emission cadence (§5.1) |
| `max_allowed_uids` | **256** (hard ceiling — owners may lower, never raise) | one metagraph shared by **~200 top-level miner UIDs + 1 pool UID per NO + validator UIDs** (§14); tail providers are NOT UIDs (§3) |
| `max_allowed_validators` | **128** default, root-only; reserve **≤ 56** so ~200 miner slots fit | permit count (top-k by stake, §9.7) — *not* a slot partition; unused permit slots are miner UIDs |
| `mechanism_count` | **1** | a 2nd mechanism halves the 256-UID space below 200 (§13.8, §14) |
| `max_weight_limit` | **set a real cap** (e.g. low single-digit %) | chain default is *no cap* (65535); without it one UID could dominate the head (§8.4) |
| `commit_reveal_weights_enabled` | **true** | weights carry the subjective quality signal — anti‑copying (§10) |
| `liquid_alpha_enabled` | **true** | reward validators who back good pools early (§10) |
| `immunity_period` | **high (≫ 4096 default)**, and **> reveal interval** | protect new pools **and new top-level miners** (the §8.4 quality-dip risk); must exceed `commit_reveal_period × tempo` |
| `min_allowed_weights` | **1** | a validator scores all miner UIDs (pools + top-level miners); avoid the 1024 default |
| `weights_version_key` | bump on scoring‑logic upgrades | force validator‑software upgrades (§10) |
| `serving_rate_limit` | default 50 | axons optional (custom HTTP protocol, §16) |
| `registration` | burn‑based, `min_burn`/`max_burn` tuned | Sybil cost on miner‑pool UIDs + validators |
| `bonds_penalty` / `alpha_low`/`alpha_high` | tune (Liquid Alpha) | shape early‑discovery reward vs. stability (§2.2) |

> Several genesis defaults are governance‑mutable and have drifted from docs (e.g. `tao_weight = 0.18`
> live; `max_validators` 64 vs 128; `commit_reveal` default flipped). Query the live chain and set
> explicitly; do not rely on documented defaults (§16 checklist).

### 15.2 Contract / economic parameters

| Parameter | Symbol | Suggested | Notes |
|---|---|---|---|
| Epoch length | `T_epoch` | 50 400 blocks (7 d) | settlement period |
| Operator data deadline | — | +4 h | `README.md` |
| Validator effort‑claim deadline | — | +24 h | `submitTrails` (§6.2) |
| Dispute window | — | +24 h…+48 h | §11.3 |
| Non‑refundable deposit fraction | `φ` | governance (e.g. 0.1–0.5) | self‑dealing floor **+ funds the effort bounty** (§9.3, §12.1) |
| Owner‑cut slice to bounty | `ω` | governance (e.g. 0–0.5) | owner co‑funds the failure‑data bounty (§9.3, §9.6) |
| **Head share** | `θ` | governance; start ~0.3, ramp | fraction of the 41% miner emission to top-level miners; `1−θ` to pools — **the load-bearing new dial** (§8.5) |
| Coverage weighting | — | governance curve | up‑weights under‑sampled / weak providers (§9.3) |
| Validator min stake | — | governance | permit qualifier + Sybil ante (§9.7) |
| Global fixed rate (USD/GB, USD/user) | — | governance, **off‑chain reference** | NOs price customers + size deposits; not consumed on‑chain (§7.1) |
| Upgrade / param timelock | — | ≥ 1 epoch (target 2 ≈ 14 d) | Phase 1, §6.4.2 |
| Owner / timelock multisig | — | M‑of‑N | §6.4 |
| Guardian (pause‑only) | — | small multisig | §6.4.2 |

### 15.3 Emission (inherited from the chain)

α emission ≈ 1 α/block participant reward (subnet‑uniform), split **18% owner / 41% miners / 41%
validators**, 21M α cap with halvings. The contract does **not** set these (the owner cut is
governance‑settable, the 41/41 is hard‑coded); it steers the *distribution within* the miner pools and
pays validators the §9.3 effort bounty — validator **dividends flow natively**. The 41% miner share is split **head/tail by θ** (§8.5) — steered by
validator weights to top-level-miner UIDs (native) and NO pools (Merkle), never withheld to an owner key
(which would trigger the post-Spec-421 `miner_burned` penalty).

---

## 16. Implementation plan

### 16.1 Components

1. **ST contract (Solidity, Cancun / 0.8.24).** State + interfaces of §6; precompile bindings
   (Staking V2 `0x805`, Neuron `0x804`, Metagraph `0x802`, Alpha `0x808`, Ed25519 `0x402`); Merkle
   verifier (OZ); the **`submitTrails` effort verifier**; proxy + owner multisig governance. **New work.**
2. **Subnet bootstrap.** `register_network`; set hyperparameters (§15.1); as each NO onboards, the
   contract `burnedRegister`s its **miner‑pool UID** (owned outright); stand up an initial set of
   **independent validators** (owner‑run at first) so consensus has measurement from day one; register
   mechanism 1 for Pool 1 later. **Top-level miners self-`burnedRegister`** their own (provider-owned, not
   contract-owned) UIDs and publish the §11.4 binding.
3. **Validator software (independent).** Stake α; run `VALIDATOR.md` trails; each tempo score **both tiers** (pools `deposit × quality`,
   top-level miners `Q_p`), read the `client_id ⇄ hotkey` binding (§11.4), split by θ, and submit
   commit-reveal weights (standard Bittensor validator loop → native dividends); submit completed‑trail proofs to `submitTrails` for the effort bounty — **no central
   keeper sets weights**. A separate **permissionless settlement poke** triggers `finalizeEpoch` after
   the +48h window.
4. **Network‑Operator software.** Runs the privacy servers + the `VALIDATOR.md` `/verify` server
   (SEED/EXTEND/FINAL, poisoning, idempotency, the four Ed25519 signatures, the egress‑IP index);
   `deposit`s DT each epoch; computes provider reliability + payout list; commits the **`payoutRoot`**
   (fractional shares); serves leaves. (No validator pool — it co‑signs trails as the `/verify` server.)
5. **Provider software.** Carries ingress/egress; registers a `client_id`; verifies its payout leaf
   against `payoutRoot`; `claimMiner`s. **If it reaches the top ~200:** `burnedRegister`s its own UID,
   publishes the dual-signed `client_id ⇄ hotkey` binding (§11.4), and earns **natively** (no claim, §8.4).
6. **Validator client (was "verifier").** `registerValidator(vpk)`; stake α; run `/verify` trails;
   submit commit‑reveal pool scores (native dividends) **and** completed‑trail proofs to `submitTrails`
   (effort bounty); `claimValidator`s the bounty; participates in disputes.
7. **Indexer/explorer.** Surfaces `D_n`, pool quality `Q_n`, consensus weights, vtrust, the
   **independent‑validator stake share**, the fee pool + per‑validator effort, and roots — the public
   audit surface.

### 16.2 The chain is identity + stake + weights only

Per current Bittensor practice, **`serve_axon` is optional**: the runtime stores but never interprets
the axon protocol, and Yuma consumes only weights + stake. The UR network's transport is the custom
`VALIDATOR.md` HTTP protocol; participants discover each other out‑of‑band (the NO directory in
`README.md`) or via the commitments pallet, and touch the chain only for **registration, the contract,
weights, and emission**. This is a supported, common pattern (model‑commit subnets, orchestrator
subnets) — no Synapse/dendrite required.

### 16.3 Milestones

1. **M0 — Testnet subnet + contract skeleton.** Register on testnet (chain 945); deploy the contract;
   register a **miner‑pool UID** and custody/move α (`0x805`); prove an **independent validator** sets
   commit‑reveal weights (`0x804`) and earns dividends; prove `submitTrails` verifies a trail via `0x402`.
2. **M1 — Deposit + multi‑validator emission.** `deposit`; **≥ 2 validators** score pools
   `deposit × quality`; verify miner emission **accrues to the contract** via the contract‑owned
   miner‑pool UIDs and that consensus/clipping/vtrust behave; **per‑NO** `claimMiner` against
   `payoutRoot` × `poolTotal` end‑to‑end (providers claim directly) with a mock payout list. **Head:** register a
   provider-owned **top-level-miner UID**, publish the §11.4 binding, and verify a validator steers it on
   `Q_p` with **native** emission to the provider's hotkey (no contract custody), split from the pools by θ.
3. **M2 — Validator rail + effort bounty.** Integrate `VALIDATOR.md` proofs; `registerValidator`;
   `submitTrails` → coverage‑weighted `effort`; `feePool` = `φ·ΣD + ω·OwnerCut`; `claimValidator` pays
   `feePool·effort/Σeffort` **on‑chain (no root)**; on‑chain Ed25519 verification via `0x402`.
4. **M3 — 7‑day settlement.** Full epoch lifecycle with the +4h/+24h/+48h
   windows; settlement‑poke automation; the **append‑only finalized‑claims invariant** (§6.4).
5. **M4 — Mainnet Pool 0 (Phase 0 governance, §6.4.1).** Mainnet (chain 964) launch under the owner
   multisig + upgradeable proxy, conservative parameters, **quality‑factor swing capped until the
   independent‑validator stake share is healthy** and `VALIDATOR.md` §10 advances (§12.3).
6. **M5 — Harden custody (Phase 1, §6.4.2):** timelock (≥ 1 epoch) on upgrades/params + a pause‑only
   guardian; then **Pool 1 (VPN factory)** via mechanism 1.
7. **M6 — Decentralize further (deferred, §6.4.3):** trustless oracle (§13.5), permissionless bonded
   admission, on‑chain governance; advance the `VALIDATOR.md` §10 roadmap.

### 16.4 Pre‑launch verification checklist (load‑bearing live values)

- Precompile addresses/ABIs at the pinned Subtensor release (Staking **V2** `0x805`; Neuron `0x804`
  `setWeights`; Ed25519 `0x402`; Alpha `0x808`).
- `tao_weight` (expect 0.18), `max_allowed_validators`, `min_allowed_weights`,
  `commit_reveal_weights_enabled` default, `SubnetOwnerCut` — query live, set explicitly.
- Subnet creation cost (`btcli subnet burn-cost`) and registration burn bounds.
- Confirm `transferStake`/`moveStake` within‑netuid are slippage‑free on the live runtime; confirm the
  staking precompile's "contract address = coldkey" custody semantics.
- Confirm an **independent validator** earns a permit at expected stake and that its **native
  dividends** accrue to its own hotkey (no contract capture under (X)); confirm `submitTrails` → `0x402`
  verification credits coverage‑weighted effort correctly.
- Confirm **`max_allowed_uids` = 256 is a hard ceiling** and `mechanism_count = 1` (a 2nd mechanism halves
  UID space, §14); confirm a **provider-owned top-level-miner UID** earns **native** emission to its own
  coldkey (no take, not shared) and that the §11.4 `client_id ⇄ hotkey` binding verifies via `0x402`.
- Confirm contract-owned **pool UIDs are not treated as owner/immune**, so the head/tail θ split does **not**
  trigger the post-Spec-421 `(1 − miner_burned)` penalty (§8.5); set `max_weight_limit` and a high
  `immunity_period` to protect the head from quality-dip eviction.

---

## 17. Open questions from `seed/INCENTIVES.md` — resolved

| Question | Resolution |
|---|---|
| **How is oracle data stored/charged on Subtensor? Can the NO payout table be a Merkle tree so each miner validates its payout without storing it on chain?** | **Yes.** Commit a 32‑byte Merkle root per NO per epoch; the contract stores roots that gate claims and the **free** commitments pallet can mirror them; bulk leaves are served off‑chain; each provider verifies its own payout with an `O(log N)` proof (§11). (No on‑chain oracle: the global rate is an off‑chain reference, §7.1.) |
| **Are smart contracts standard EVM?** | **Yes** — Frontier EVM, Cancun, Solidity 0.8.24, chain 964, permissionless deploy. With Subtensor‑specific **precompiles**, validators set commit‑reveal weights and the contract stakes/transfers α, reads the metagraph/α‑price, and verifies Ed25519 — everything this design needs (§2.5). |
| **How to adapt to standard BT payout formulas?** | Independent validators set standard Yuma weights `= deposit × quality` on the per‑NO miner‑pool UIDs; the chain's incentive/dividend split delivers emission ∝ `deposit × consensus‑quality` to the **miner pools** (which the contract owns → re‑splits to providers per Merkle payout roots, §§8, 11) and ∝ stake × vtrust to **validators natively**. Validator *effort* is paid by a fee‑funded bounty (§9.3). No deviation from standard Yuma — it *is* Yuma, with many independent validators (§9). **Plus a second tier:** the top ~200 providers hold their own UIDs, steered on pure `Q_p` with native emission, split from the pools by θ (§8.4–8.5). |

---

## 18. Glossary

- **NO (Network Operator)** — runs servers; operates **one miner‑pool UID** (contract‑owned) but holds no
  emission; deposits DT; runs the `/verify` server (co‑signs trails); commits the payout root for its
  providers. No validator.
- **Miner‑pool UID** — the on‑chain miner: one per NO, contract‑owned. The 100k+ providers are **inside**
  it (not UIDs) and are paid by Merkle claim.
- **Provider / miner (tail)** — carries traffic; inside a NO's miner pool (the on-ramp tier); **claims its
  α directly from the contract** per the NO payout root. Can **graduate** to a top-level slot (§8.4).
- **Top-level miner (head)** — a top ~200 provider that holds **its own miner UID**, steered **directly** by
  validators on pure quality `Q_p` and paid **natively** (no contract custody, no Merkle claim); matched to
  its UID by the §11.4 binding; maintained by deregistration churn (§8.4).
- **`client_id ⇄ hotkey` binding** — the dual-signed (client Ed25519 + hotkey sr25519) association a
  top-level miner publishes (commitments pallet + contract anchor) so validators attribute its measured
  `client_id` to its UID (§11.4).
- **θ (head share)** — the governed fraction of the 41% miner emission steered to top-level miners; `1−θ`
  goes to the pools (§8.5).
- **Validator** (was "verifier") — an **independent** Bittensor validator UID: stakes α, runs
  `VALIDATOR.md` trails, scores pools, and earns **native dividends + a fee‑funded effort bounty** (§9).
  No NO, no pool — the disinterested consensus baseline.
- **DT / φ / ω** — deposit transaction (NO, ∝ usage); `φ`·DT (non‑refundable) + `ω`·OwnerCut fund the
  validator **effort bounty** (§9.3).
- **Effort bounty** — fee‑funded reward paid ∝ verified, coverage‑weighted, server‑assigned completed
  trails — the engine that keeps the failure data flowing (§9.3).
- **ST contract** — the subnet‑token (α) EVM contract: ledger + **custodian of miner emission** +
  settlement + bounty payer. **Not** the validator (§9–§10).
- **`SUM(DT)` / `Q_n` / `ŵ_n`** — NO's epoch deposit total / its consensus‑measured pool quality / its
  resulting consensus weight (∝ `deposit × quality`).
- **Validated path** — a completed `VALIDATOR.md` trail proof, id `keccak256(trail_id‖vpk‖server_key_id)`;
  self‑proving (NO `FINAL` + validator `vpk` sigs), verified via `0x402`.
- **Epoch (7 d)** — application settlement period; **tempo (360 blk)** — chain weight/emission cadence.

---

*End of WHITEPAPER.md v0.2 — two-tier miner side (pool on-ramp + direct top-level miners; §8.4–8.5, §10, §11.4, §14; decisions D16–D20 in `WHITEPAPER_DISCUSS.md`). This document fixes the architecture and the formulas; the next artifacts
are the contract source, the chain‑config script, and the operator/validator reference daemons (§16).*
