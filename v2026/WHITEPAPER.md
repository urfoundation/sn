# UR Subnet — Whitepaper

**A Bittensor subnet for a decentralized privacy network.**

Version 0.5 (design). Target chain: Bittensor / Subtensor — **testnet first, then mainnet** (D28).

> **v0.5 — validator effort bounty removed from scope (D29).** The fee‑funded validator effort bounty is
> **no longer a committed deferred phase** — it is **out of v1 scope entirely**, and whether to add any
> validator‑effort incentive at all (and in what shape) is a **post‑launch open question**, to be decided
> from what the live network shows about independent‑validator coverage. v1 pays validators **native Yuma
> dividends only** (∝ stake × vtrust) — the plain Bittensor norm. The `(X)` fee‑funded / `(Y)`
> emission‑routed designs of §9.3/§13.6 are retained below as **parked reference** (the formulas and the
> already‑built machinery are not lost), but they are **candidates a future iteration might explore, not a
> roadmap**. This supersedes the "deferred, ships with the independent‑validator phase" framing of v0.3
> (D23) wherever it appears below.

> **v0.4 — conviction staking, validator-computed weights, IP-breadth head.** Simplification pass
> (`WHITEPAPER_DISCUSS.md` D25–D28): the **contract stops weighting/validating deposits** — deposits are
> **conviction stake** (locked in the reserve, D23), their cumulative amount sets a NO's **tier → deposit
> rate** (zero conviction = baseline; staking lowers it — the onboarding lever), and **validators weight
> the pools themselves** by `implied_usage × quality` (`implied_usage = deposit / rate(tier)`) from
> published data. The **head tier ranks fleets by split-adjusted distinct routable egress-IP count** (not
> traffic; shared IPs split among claimants), head weight ∝ that score. Validators run measurement with
> **guardrails off** (own rate). See §7, §8, §10, §12; prior layers: v0.3 buybacks (D23), v0.2 two tiers
> (D16–D20).

---

## Executive summary

The **UR Subnet** runs a **decentralized privacy/VPN network** entirely through on‑chain incentives.
**Network Operators (NOs)** run the servers; independent **providers** carry ingress/egress traffic; and
independent **validators** run the `VALIDATOR.md` cryptographic routing‑verification protocol — walking
server‑assigned chains of providers to prove real‑time transit and measure **which providers are the
weakest links**. That measurement is the core: each tempo, **validators weight every NO's pool
themselves** — `implied_usage × quality`, where implied usage is the NO's α deposit divided by its
conviction‑tier rate (§8.1) — and Bittensor's **Yuma Consensus** turns those scores into the miners'
emission. As in canonical Bittensor, **validators' evaluation drives the payout** — the deposit
(α a NO locks, a costly, revenue‑backed signal of real demand) anchors the split, and the measured
`quality` (`Q_n`, from the trails) is the earned modulator. The **contract computes no weight** (D25); it
custodies and settles only.

Because one NO may serve **100k+ providers — far beyond a subnet's ~256 UID cap** — each NO is a single
**pool UID**, and its providers are paid *inside* the pool by **Merkle claim**. A smart contract on the
**Subtensor EVM** (the **ST contract**, denominated in the subnet's **α** token) is the ledger, the
emission custodian, and the **7‑day settlement** engine: providers **claim their α directly from the
contract with cryptographic proofs**, so a NO *directs* where its pool's rewards go but **never holds
anyone else's funds**. Validators earn Bittensor‑native **dividends** (∝ stake × scoring accuracy) —
**v1's only validator reward**; the fee‑funded effort bounty of earlier drafts is **out of scope** (D29), a
post‑launch open question, its spec parked in §9.3/§13.6 as a future‑iteration candidate.

**Deposits are conviction stake — never distributed.** The contract moves each deposit into a **buyback
reserve** — α staked on the owner's validator hotkey, where it compounds native dividends — and **no code
path re‑liquefies it**. So a deposit does two things: it is the **demand signal** validators weight (§8.1)
and it **permanently removes α from liquid supply**, in proportion to real usage. An NO's cumulative
locked α (its **conviction**) sets its **tier**, and the tier sets the deposit rate it must post —
**zero conviction = the baseline rate; more conviction = a lower rate**, so committed operators onboard
with less up‑front α (§7.3). Miners in both tiers are paid **from emission only** (§7.4, §12.4).

**Two miner tiers, in parallel.** The pool is the **on-ramp** — a place to start, with a **baseline
reward** and a low barrier (no UID, no registration burn), weighted by `implied_usage × quality`. Above
it sits the **supply apex**: the **top ~200 fleets by distinct routable egress-IP count** ("**top-level
miners**") each claim their own miner UID and are **steered emission directly by validators on that
routable-IP breadth** — not on traffic volume; a fleet's score is its count of unique routable exit IPs,
**split when two fleets share an IP** — paid **natively** to their own hotkey, no contract custody, no
Merkle claim. A fleet is matched to its UID by a **signed `client_id`s → hotkey binding** validators read
(§11.4); its providers **graduate out of the pool into the head, and fall back if their breadth slips**,
with deregistration churn running that tournament. Both tiers share **one** mechanism's 256-UID metagraph
(§14); a governance split **θ** sets how much of the 41% miner emission flows to the head vs. the pools
(§8.4–8.5).

The subnet **launches centralized‑but‑bounded** — an owner multisig behind an upgradeable contract, with
**finalized claims made un‑clawback‑able from day one** — and hardens to a **timelocked,
guardian‑protected** contract, then broader governance. v1 rewards independently *measured liveness*;
closing the gap to honest‑relay, payout‑grade verification is the `VALIDATOR.md` §10 roadmap.

**At a glance**
- **Bittensor‑aligned:** validators evaluate miners (pools) → Yuma Consensus → emission; standard
  **18% owner / 41% miner / 41% validator** α split.
- **Real‑usage anchored:** a costly, revenue‑backed α **deposit** weights the cross‑operator split
  (validators compute `implied_usage × quality` off published data — the contract weighs nothing, D25);
  the network's unit of account is the subnet's **α**.
- **Deposits are conviction stake:** every deposit is locked into a contract **buyback reserve**, never
  paid back out; its cumulative amount sets the NO's **tier → deposit rate** (staking lowers the rate —
  onboarding + alignment); miners are paid from **emission only** (§7.3, §7.4, §12.4).
- **Scales to 100k+ providers:** pool UIDs + off‑chain **Merkle** payout claims (providers and validators
  inside a pool are *not* UIDs).
- **Two miner tiers:** a per-NO **pool** (on-ramp — `implied_usage × quality`, Merkle claim) *and*
  **top-level miners** (the top ~200 **fleets by distinct routable egress-IP count**, split-adjusted, as
  direct native UIDs), split by a governed share **θ** (§8.4–8.5).
- **Trust‑minimized custody:** no operator holds others' emission; everyone is paid by direct on‑chain
  claim against a committed Merkle root.
- **Validator data:** native dividends reward consensus‑accurate scoring; at launch the owner is the
  majority validator (by α holdings + the reserve) and runs the trail volume itself. A coverage‑weighted
  **effort bounty** is **out of v1 scope** (D29) — a possible future iteration, not a committed phase.
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
   contract**) custodies the buyback reserve (§7.4) and the captured pool emission and settles payouts
   by Merkle claim; protocol **emissions** are delivered through the chain's coinbase. We do **not**
   fight the coinbase.
2. **Everything is denominated in the subnet's α (alpha) token.** Deposits and payouts are α. This is
   why `seed/INCENTIVES.md` calls it the **ST (subnet‑token) contract**.
3. **Miner pools, scored by real Yuma consensus.** Each NO is **one miner‑pool UID**; its 100k+
   providers are paid *inside* the pool by Merkle claim. **Many independent validators** (no NO owns
   them) score the pools `deposit × measured‑quality` — so **validators' evaluation drives the miner
   emission, the Bittensor way** — and Yuma's median/clipping/vtrust/bonds do real work. (Validators earn
   **native dividends only**; a fee‑funded **effort bounty** for third‑party trail volume is **out of scope**
   — the owner is the majority validator at launch and runs the trails itself, §9.2; the bounty is a parked
   future‑iteration candidate, §9.3, D29.)
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

1. **Deposits — the demand signal, conviction stake, and a buyback.** Each NO deposits α into the ST
   contract, sized to its real usage at the network's **off‑chain published rate for its conviction
   tier** (no on‑chain oracle — §7.1). Deposits per NO are the costly **signal of real demand**; the α is
   **never distributed** — the contract locks every deposit into the **buyback reserve** (§7.4), and a
   NO's cumulative locked α (its **conviction**) sets the **tier → rate** that governs how much it must
   post (§7.3). The contract keeps **no deposit ledger** — the `Deposited` events are the published record
   (§7.5, D25).

2. **Emission (Yuma consensus over NO pools).** Every NO has **one contract‑custodied miner‑pool UID**
   (all its providers). **Independent validators** — anyone who stakes α and runs `VALIDATOR.md` trails —
   compute **every** NO's pool score **themselves** as `implied_usage × measured‑quality` (implied usage
   = the NO's deposits ÷ its tier rate, §8.1) and submit those weights, so the **validators' evaluation
   drives the miner emission — the Bittensor way**. With many independent validators, **Yuma Consensus
   does real work**: stake‑weighted median + **clipping**, so no NO can inflate its own pool or knife a
   rival. Miner emission (41%) lands on the miner‑pool UIDs, **owned by the contract's coldkey** — no
   emission ever touches a NO's keys. **The 41% splits across two tiers** (§8.4–8.5): a governance share
   **θ** to **top-level miners** (the top ~200 fleets by **split-adjusted distinct routable egress-IP
   count**, as their own native UIDs), and `1−θ` to the NO pools above. Validator emission (41%) flows
   **natively** to the independent validators ∝ stake × vtrust.

3. **Settlement (contract).** Over a 7‑day epoch the contract accrues the **captured miner emission**,
   then distributes it. A NO's **miner pool** — its UID's earned emission, nothing else (deposits are
   reserved, never distributed, §7.4) — goes to its providers. Since the **100k+ providers are not
   on‑chain UIDs** (the subnet cap is ~256) they are paid *inside* the pool by **Merkle claim**: the NO
   commits a payout root and every provider **claims its α directly from the contract** with an
   O(log N) proof. The NO directs the split but never holds the α.

Top-level miners need **no settlement** — Yuma pays their UID natively each tempo (§8.4); the contract
holds and settles only the **pool tier** (the tail), exactly as above.

In v1 validators earn **native dividends only** — full stop. The subnet owner — holding the majority of α
and therefore the majority validator seat in the early network, a position further compounded by the buyback
reserve staked to its hotkey (§7.4) — runs the trail volume itself, so the failure data does not need a
protocol‑level subsidy. Whether a validator **effort bounty** (∝ verified, coverage‑weighted completed
trails) is ever worth adding is an **open post‑launch question**, not a committed phase (D29); its design is
parked in §9.3/§13.6 as a future‑iteration candidate.

Because the weights carry a *subjective* signal — measured provider quality — the standard Bittensor
anti‑gaming stack applies and is **switched on**: **commit‑reveal** (a lazy validator can't copy fresh
consensus), consensus **clipping + vtrust** + the **self‑weight mask** (a validator can't inflate a pool
or score its own NO), and **bonds / Liquid Alpha** (rewarding validators who back good pools early). The
**deposit** (as implied usage) is the *objective* anchor that ties the across‑NO split to revenue‑backed
demand (§7–§8.1); **quality** is the Yuma‑measured factor that makes validators' evaluation the thing that
moves the money (§10). At bootstrap, when `Q_n` is still noisy, governance **caps the quality swing** and
widens it as the validator set and data mature (§12.3).

```
                     INDEPENDENT VALIDATORS  (stake α, run /verify trails)
         each tempo → score BOTH tiers → commit-reveal → Yuma (stake-median + clip + vtrust)
             pools (tail):  weight = implied_usage_n × quality Q_n   (deposit / tier-rate)
             top miners:    weight = routable-IP score (split-adjusted)   combined into ONE vector,
                                       │                split by governance share θ
                                       ▼  drives 41% miner emission
                        ┌──────────────┴───────────────┐
                  (1−θ) │ TAIL                     HEAD │ θ
                        ▼                               ▼
 ┌──────────────────┐   ┌───────────────────────────┐  ┌───────────────────────────────┐
 │ Network Operator │   │ ST CONTRACT (Subtensor EVM)│  │ TOP-LEVEL MINER UIDs  (~200)   │
 │ runs servers +   │dep│  owns one POOL UID per NO  │  │  fleet client_ids ⇄ hotkey     │
 │ /verify; commits │──▶│  deposits → BUYBACK RESERVE│  │  weight = routable-IP score    │
 │ payout root;     │   │  + custodies miner emission│  │  NATIVE emission → own hotkey  │
 │ holds NO α only  │   │  → per-NO Merkle roots     │  │  (no take, not shared, no      │
 └────────┬─────────┘   └─────────────┬──────────────┘  │   contract custody)            │
          │ commits root              ▼ claim α          └───────────────┬───────────────┘
          ▼ (never holds α)   providers (100k+, TAIL)                    ▼ direct, trust-minimized
   customers ($) ──▶ revenue         (Merkle proof)          a top provider's own coldkey
          └────────── start in a pool ─▶ graduate to a top slot ─▶ fall back if quality slips ──┘

 Bittensor coinbase: 18% owner · 41% miner (Yuma, split θ / 1−θ above) · 41% validator (NATIVE ∝ stake×vtrust)
 buyback reserve: ΣD staked to the owner-validator hotkey — locked, dividend-compounding, never distributed (§7.4)
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
  `transferStake`. Two mechanics matter for §7.4: stake carries **no time lock** — there is no unbonding
  period; `removeStake`/`transferStake`/`moveStake` act immediately for the owning coldkey — so any
  "locked" property must come from *who the coldkey is* (a contract with no exit path), not from staking
  per se; and delegated stake on a validating hotkey earns dividends that **auto‑compound as stake**
  (less the hotkey's delegate `take`).

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
| **Subnet owner** (BringYour, Inc.) | owner coldkey + owner hotkey at **UID 0** (immune); deploys & governs the ST contract; operates the **reserve validator hotkey** (§7.4) | receives the 18% owner cut; the **majority validator** in the early network (§9.2); the governance **referee** (§9.6). |
| **Network Operator (NO)** | a contract registration (`noId`) with **one miner‑pool UID** (its providers, contract‑owned). Holds **no emission**; runs no validator. | a per‑NO *pool operator*: deposits; runs the `/verify` server (co‑signs trails); commits the Merkle payout root that splits its miner pool. Directs flow; the contract holds and pays. |
| **Provider (miner)** | a `client_id` **inside** a NO's miner pool — **not a UID** (100k+ providers can't each be a UID) | carries traffic; **claims its α directly from the contract** with a Merkle proof against its NO's payout root. The **on-ramp / tail** tier; can **graduate** to a top-level slot (§8.4). |
| **Top-level miner (head)** | **its own miner UID**; a **fleet** — its `client_id`s **bound to its hotkey** (§11.4) | the **supply apex**: the top ~200 fleets by **split-adjusted distinct routable egress-IP count**, steered **directly** by validators (`weight = score`); **native** emission to its own coldkey — no contract custody, no Merkle claim; maintained by deregistration churn (§8.4). |
| **Validator** (was "verifier") | an **independent** Bittensor validator UID; stakes its **own** α | runs `/verify` trails (the failure‑data signal), scores pools `implied_usage × Q_n` under commit‑reveal (§10). Earns **native dividends** (∝ stake × vtrust) — its **only** reward; a validator effort bounty is **out of scope** (D29, parked in §9.3). No NO, no pool. |
| **ST contract** | a coldkey (mapped SS58) that **owns each NO's miner‑pool UID** and the **buyback reserve** | custodies **miner** emission and stakes every deposit into the locked reserve (§7.4); settles every pool by Merkle claim. Does **not** custody validator emission (it is native). |

**Why pools (miner side only).** A NO has up to 100k providers — they cannot be UIDs (subnet cap ≈ 256),
so each NO gets **one miner‑pool UID** and its providers are paid *inside* it by Merkle claim. That
miner‑pool UID is **owned outright by the contract's coldkey** (a pure accrual slot), so the NO never
holds the emission destined for its providers. **Validators are not pooled** — they are independent
Bittensor validator UIDs (own hotkey, own stake, native dividends), which is both simpler and the
**independence** the measurement needs (§9.5). This removes the per‑NO validator pool, the take‑0 custody
binding, the intersection split, and the VT of earlier drafts (§13.6).

**Why also a head tier (top-level miners).** Pools solve *scale* (100k+ providers), but inside a pool a
provider trusts its NO's discretionary payout split and is one of thousands. The **top ~200 fleets by
routable-IP breadth** therefore *also* get the canonical Bittensor treatment — **their own UID, steered
directly by validators, paid natively** (§8.4) — so the biggest routable-supply operators compete
head-to-head on breadth, trust-minimized, while the pool stays the low-barrier on-ramp everyone starts
in. A provider (`client_id`) is in **exactly one** tier at a time
(promoted out of its pool's payout list once it holds a UID — no double-pay, §8.4). The two tiers share one
256-UID metagraph (§14).

**Binding the validator identity.** `seed/INCENTIVES.md` says a validator "uses their wallet PK as their
validation path key," while `VALIDATOR.md` signs trails with an Ed25519 `vpk`. The on‑chain binding —
`registerValidator(vpk, sig)`, an Ed25519 signature checked via the `0x402` precompile — exists only to make
submitted **completed‑trail proofs** attributable for an effort bounty, so it belongs to the **parked
bounty design** (§9.3, D29); v1 deploys no contract validator registry. (Reusing the BT wallet key directly
as `vpk` also works but couples key rotation to the wallet; binding is preferred.)

---

## 4. Notation

| Symbol | Meaning |
|---|---|
| `netuid` | the UR subnet id |
| `T_tempo` | tempo length in blocks (360) |
| `T_epoch` | UR settlement epoch in blocks (50 400 ≈ 7 days) |
| `e` | epoch index (monotone counter in the contract) |
| `D_n` | α deposited by NO `n` during epoch `e` (summed from the `Deposited` event log — no on‑chain ledger, §7.5) |
| `conviction_n` | NO `n`'s cumulative locked α (all‑time deposits + voluntary stake) → its tier (§7.2) |
| `rate(tier_n)` | published α‑per‑usage rate for `n`'s conviction tier; zero tier = baseline (§7.3) |
| `implied_usage_n` | `D_n / rate(tier_n)` — the demand signal validators weight (§8.1) |
| `w_n` / `ŵ_n` | NO `n`'s pool weight ∝ `implied_usage_n × Q_n` (validator‑computed, §8.1/§10) |
| `E_mine, E_val, E_own` | α emission to the miner / validator / owner pools over the epoch (41/41/18% of subnet α emission) |
| `Q_n` | NO `n`'s consensus‑measured pool quality (from validators' `VALIDATOR.md` trails) |
| `score(u)` | top‑level miner (fleet) `u`'s **split‑adjusted count of distinct routable egress‑IPs** — **is** its head weight (§8.4) |
| `θ` | governance **head share**: fraction of the 41% miner emission steered to top-level miners; `1−θ` goes to the pools (§8.5) |
| `client_id`s ⇄ hotkey | the signed binding mapping a fleet's `client_id`s to a top-level miner's UID (§11.4) |
| `R` | the **buyback reserve**: cumulative locked deposits + voluntary conviction + their compounded dividends (§7.4) |
| `φ` / `ω` / `FeePool` / `effort_v` | effort‑bounty quantities — **out of scope** (parked, §9.3/§13.6, D29) |
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
dividends while divergence is clipped. There is **no off‑chain keeper or trusted weight authority**;
each validator runs standard Bittensor validator software.

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
  t < +48h      Audit window: committed roots are public; a bad head binding is disputable on-chain (§11.3).
  +48h          contract.finalizeEpoch(e): snapshot per-NO poolTotal (emission-only, §8.3). Claims open (no global root).
                Unclaimed α rolls into epoch e+1 (or a grace pool) after a TTL.
                (The +24h effort-claim step + challenge window belong to the parked bounty design, §9.3/D29 — not in v1.)
```

Deposits, emission weighting, and dividend capture happen continuously across the epoch at tempo
cadence; only **settlement** waits for the epoch boundary + windows.

---

## 6. The ST contract

A single Solidity contract (upgradeable behind a proxy; control & governance model in §6.4) on the
Subtensor EVM. It is simultaneously: a **coldkey** custodying α, the **deposit ledger**, the **buyback
reserve** (every deposit staked and locked, §7.4), the **emission custodian** (it owns the miner‑pool
UIDs and captures their incentive), and the **settlement/claims** engine. It is **not** the subnet's
validator — **independent validators** (§9) set the weights and earn dividends natively; the contract
holds and pays out the **miner emission only** (an effort bounty is out of scope, §9.3/D29).

**No-custody is a v1 must-have — in spirit, not immutability (D21).** The *owner and NOs never hold or
distribute α*: the contract is the sole custodian of in-transit α and every payout is a **direct on-chain
pull claim** (`transferStake`), with the **head paid natively** (§8.4). All α moves on-chain; no person ever
holds participants' α in an off-chain wallet. This property is required at v1 — but it does **not** mean the
contract is frozen: for v1 the contract stays **upgradeable + owner-multisig + guardian** (normal bug-fix
latitude) and is progressively locked down (§6.4).

### 6.1 State (essential)

```solidity
uint16  public netuid;
bytes32 public treasuryHotkey;         // contract's own hotkey for staking idle/treasury α (NOT the subnet validator)
address public owner;                  // BringYour, Inc. governance (multisig)
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
// (validator effort registry, trailsRoot/effort/feePool: OUT OF SCOPE — parked bounty design, §9.3/D29)

// --- deposits: NO on-chain weighting ledger (D25) ---
// DT[e][noId] / totalDT are GONE — the contract does no deposit weighting or
// attribution. Deposits are published by the Deposited(e, noId, from, amount)
// event log; validators sum per-NO (this epoch -> demand signal; all-time ->
// conviction/tier) and weight the pools themselves (§8.1).

// --- conviction buyback reserve (§7.4): every deposit is staked here, never distributed ---
bytes32 public reserveHotkey;   // the owner-validator hotkey the reserve is staked to (set at initialize)
uint256 public buybackTotal;    // cumulative deposits moved to the reserve; dividends compound on top,
                                // so live reserve = getStake(reserveHotkey) ≥ buybackTotal, auditable every block

// --- per-epoch operator commitment, keyed (epoch, noId) ---
struct NoCommit { bytes32 payoutRoot; bytes off; }          // payoutRoot = Merkle root of (provider_coldkey, share) leaves, Σ share = 1
mapping(uint256 => mapping(uint256 => NoCommit)) public noCommit;

// --- per-epoch settlement: snapshotted at finalizeEpoch; NO global claim roots ---
mapping(uint256 => mapping(uint256 => uint256)) public poolTotal;   // epoch -> noId -> miner pool = emission_n ONLY (deposits are reserved, §7.4)
mapping(uint256 => mapping(uint256 => uint256)) public claimedMiner; // epoch -> noId -> α already paid from pool n (≤ poolTotal)
mapping(uint256 => mapping(bytes32 => bool)) public minerClaimedBy; // epoch -> keccak(noId,coldkey) -> claimed
```

### 6.2 Interfaces (selected)

```solidity
// --- registration ---
function registerOperator(uint256 noId, bytes32 coldkey, bytes32 minerHotkey) external; // owner-gated v1; contract burnedRegisters the miner-pool UID (owns it)
// (registerValidator / the vpk registry: out of scope — parked bounty design, §9.3/D29)

// --- deposits (α held as stake; see §6.3) ---
function deposit(uint256 noId, uint256 alphaAmount) external;   // DT: credits the steering signal (gb/users optional
                                                                // off-chain metadata, §7.1), then the FULL amount is
                                                                // moved to the buyback reserve (moveStake -> reserveHotkey, §7.4)

// Weights are NOT set here: each INDEPENDENT validator signs its OWN commit/reveal setWeights via the
// Neuron precompile (§10), earning native dividends. The contract custodies only the MINER emission.

// --- per-epoch operator publishing (within +4h) ---
function commitOperator(uint256 e, uint256 noId, bytes32 payoutRoot, bytes calldata off) external; // payoutRoot over (provider, share) leaves, Σ share = 1

// --- settlement (no global claim roots; amounts derive from on-chain state) ---
function finalizeEpoch(uint256 e) external;   // after +48h: snapshot per-NO poolTotal = measured emission (via 0x802) — deposits are NOT added
function claimMiner(uint256 e, uint256 noId, bytes32 coldkey, uint256 shareBps, bytes32[] calldata proof) external; // verify (coldkey,shareBps) vs payoutRoot[e][noId]; pay shareBps·poolTotal[e][noId], capped

// --- governance ---
function setHyperparam(...) external;                          // owner relays to subnet precompile
// (setFeeParams(φ, ω): out of scope — parked bounty design, §9.3/D29. reserveHotkey is set once at initialize — §7.4)
```

### 6.3 How the contract holds and moves α

- **Emission capture (the key property).** The contract's coldkey **owns every NO's miner‑pool UID**
  outright (a pure accrual slot), so Yuma credits its incentive as **α stake the contract holds** — the
  **41% miner emission** lands in the contract automatically, with no action by and no custody by any NO.
  The contract reads realized per‑pool emission on‑chain (Metagraph `0x802` `getIncentive`/`getEmission`)
  to build settlement (§8).
- **Validator emission is native.** Independent validators stake their **own** α and earn their **41%
  dividends natively** to their own hotkeys ∝ stake × vtrust — the contract neither stakes for them nor
  custodies their dividends. (Native dividends are the **whole** validator reward; a fee‑funded effort
  bounty **(X)** is out of scope, §9.3/§13.6/D29.)
- **Custody as stake, on two hotkeys.** All α the contract holds is staked under its coldkey (no AMM
  exposure; α stays α), split across **two hotkeys with different jobs**: `treasuryHotkey` is the
  **claims escrow** (swept pool emission awaiting `claimMiner`, its balance exactly tracked by the
  push‑then‑credit ledger), and `reserveHotkey` — the owner's validator hotkey — holds the **buyback
  reserve** (§7.4). The split is load‑bearing: dividends compound on the reserve hotkey, and mixing them
  into the escrow would break the exact `getStake(treasury) ≥ accountedStake + amount` deposit check
  (an un‑attributable surplus an authorized caller could book as a phantom deposit).
- **Deposits in → the reserve.** A NO pushes α onto `(coldkey = mirror(contract), treasuryHotkey)` with
  `transferStake` (**slippage‑free**), then calls `deposit`, which attributes it, credits `DT` (the
  steering signal), and immediately `moveStake`s the **full amount** to `reserveHotkey` (same coldkey,
  same netuid — slippage‑free). From that point the α is **reserve, not budget**: it compounds validator
  dividends (auto‑restaked by the chain) and is **never paid out** (§7.4).
- **Payouts out — emission only.** On claim, the contract pays with
  `transferStake(recipientColdkey, treasuryHotkey, netuid, netuid, alpha)` — again slippage‑free —
  sourced **only from swept pool emission**. Recipients receive α **as stake** they can keep (earning)
  or `removeStake` to TAO (their slippage). **No function sources a transfer from `reserveHotkey`.**
- **TAO is only touched** for gas and the owner's discretionary conversions. Participants never pay AMM
  slippage to *use* the system; only to exit α→TAO.

### 6.4 Control, custody, and the launch governance model

Because the contract is **custody‑critical** (§13.1) — it holds all deposits and all captured emission —
*how it is controlled is part of the spec.* Control is a bundle of distinct privileged powers: **upgrade
authority** (the proxy admin), **admission** (`registerOperator` gating), **dispute/referee** decisions
(§9.6, §11.3), **parameters** (epoch windows, θ, the reference rate, hyperparameters), and the
**treasury** (the owner cut; the buyback reserve is contract‑locked, not owner‑spendable, §7.4). We launch with these centralized
but *bounded* (Phase 0), then harden custody (Phase 1). Deeper decentralization —
trustless inputs, on‑chain governance, immutability, and handing off the Bittensor subnet‑owner role —
is deferred (§6.4.3) until the mechanism is proven (notably the `VALIDATOR.md` §10 defenses).

**Invariant across every phase — earned claims are sacrosanct.** Once `finalizeEpoch(e)` writes the
claim roots for epoch `e`, the α backing those claims is committed: **no upgrade, pause, or admin action
may block or claw back a finalized claim.** Per‑epoch settlement is append‑only; admin power reaches only
*future* epochs. This single invariant bounds the blast radius of every privileged power below, and it is
implemented from day one.

**Sibling invariant — the buyback reserve is one‑way.** No contract function sources a transfer from
`reserveHotkey`: deposits (and the dividends they compound) can only ever *enter* the reserve (§7.4).
Like the claims invariant this is **code‑enforced now and governance‑credible over time** — the chain
itself imposes no stake lock (no unbonding period; `removeStake`/`transferStake` are immediate for
whoever controls a coldkey, §2.3), so the lock is exactly as strong as the contract having no exit path
plus the upgrade governance around it (Phase 0 multisig → Phase 1 timelock → §6.4.3 immutabilization).
The reserve is publicly auditable every block: `getStake(reserveHotkey)` vs. `buybackTotal`.

#### 6.4.1 Phase 0 — Launch (central control, fast bug‑fixes)

- **Owner = an M‑of‑N multisig** (BringYour, Inc. + signers): the proxy admin and holder of every
  privileged role.
- **Upgradeable proxy** (transparent or UUPS) → the owner can patch settlement/claim logic and tune
  parameters. This is intentional: early bug‑fix and tuning capability, and central control. Honest
  consequence: **the owner can change the rules for *future* epochs** (never the in‑flight one, per the
  invariant). Accepted for launch.
- **Owner‑gated admission** (`registerOperator` owner‑only; verifiers permissionless‑with‑bond or
  gated) and **owner as referee** for the non‑cryptographic
  disputes (§9.6) — the cryptographic disputes (§11.3) already need no owner.
- **Treasury:** the 18% owner cut accrues to the owner multisig (a governance treasury later, §6.4.3).
  The buyback reserve is **not** treasury — it is contract‑locked (§7.4).

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
  - a separate **guardian** holds **pause‑only** power — it can halt `deposit`
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

## 7. Deposits (conviction stake — the demand signal)

### 7.1 No on‑chain oracle — a published per‑tier rate schedule

`seed/INCENTIVES.md` frames deposits as "per used GB and active user … based on the global fixed rate set by
an oracle." We **drop the on‑chain oracle entirely**: per‑GB / per‑user usage is **self‑reported and
unverifiable on‑chain**, so pricing it on‑chain buys nothing — the only quantity the protocol can act on
is *the α actually deposited*. A NO just calls `deposit(α)`.

The **"global fixed rate"** survives as an **off‑chain published reference** — now a **schedule of
deposit rates per conviction tier** (§7.3): `rate(tier)` = the α a NO posts per unit of real usage.
Under v0.4 (D25) this schedule is read by **the validators** (who set the weights, §8.1) as well as by
NOs (to size their deposits); it is **never a value the contract consumes**. NOs may still report
`(gb_n, users_n)` as **optional, unverified metadata** for transparency, but it enters no on‑chain
computation. This keeps a whole subsystem (`setRates`, on‑chain rate storage, the TAO/USD feed) and a
trusted input out of the contract.

### 7.2 A deposit is conviction stake (D25)

The deposit *is* the claim: `D_n` (α) is the NO's costly, on‑chain bid for emission weight. The protocol
never verifies GB — infeasible and a non‑goal. The deposit is a **costly signal** funded by the NO's
**real customer revenue**, and it is **conviction stake**: staked into the locked reserve (§7.4) and
**sunk** — never returned through any path, in particular not by routing it back through self‑owned
providers. An NO's **conviction** = its **cumulative locked α** (deposits + any voluntary up‑front stake —
**one pool**), and conviction sets its **tier** → its deposit rate (§7.3). Conviction is the *locked
amount* (the lock is the alignment, not a time‑integral). Because the deposit is fully sunk **and** the
conviction that lowers a NO's rate is itself sunk α, buying cheaper weight costs real locked capital —
the self‑dealing round‑trip is closed structurally (§12.1). The constraint is the one
`seed/INCENTIVES.md` names — *deposit cost is bounded by the NO's revenue, which reflects real usage* —
and the remaining **independence assumptions** are in §12.

### 7.3 Tiered deposit rates — staking to onboard and align

An NO can **lower the α it must deposit per unit of usage by holding more conviction stake** — the
onboarding + long‑term‑alignment lever (change #4, D25). Governance publishes a **tier → rate schedule**:

- The **zero tier** (a NO with **zero conviction stake**) pays the **baseline rate** — the current, full
  deposit schedule.
- Higher tiers (more cumulative locked α) pay **progressively lower rates**, so a committed NO needs less
  up‑front α to signal the same usage. A brand‑new NO can either start small at the baseline rate, or
  **pre‑stake conviction** to jump to a better tier from day one.
- The schedule is **floored above zero** (a zero rate would make any deposit imply unbounded usage,
  §8.1). It is governance‑set and published off‑chain (§7.1), read by validators.

The stake and the deposit are the **same locked pool** (§7.4): both are conviction, both are sunk. The
tier is thus a smooth function of an NO's accumulated commitment — depositing over time *is* staking
conviction.

### 7.4 The reserve — staked, locked, compounding (the buyback)

Every deposit is a **buyback** in the sense the leading revenue‑generating subnets use (Chutes'
buy‑and‑**lock**, `COMPARISON.md`): usage revenue is converted to α and permanently removed from liquid
supply, rather than recycled to participants who would re‑sell it.

- **The flow.** The NO funds deposits by **buying α on‑market** from customer revenue — the buy leg is
  the actual demand pressure, and the sourcing commitment is published alongside the §7.1 rate schedule
  (on‑chain code cannot see where α came from; the policy makes the buyback claim falsifiable).
  `deposit(noId, amount)` `moveStake`s the **full amount** to the **reserve hotkey** — the owner's
  validator hotkey (§6.3) — and emits `Deposited(e, noId, from, amount)` + `BuybackReserved`. It records
  **no on‑chain weighting ledger** (D25: `DT`/`totalDT` are gone); the events are the published record.
- **The lock.** No contract code path ever sources a transfer from the reserve (the §6.4 sibling
  invariant). Staking per se enforces nothing — dTAO stake has **no unbonding period** (§2.3) — so the
  lock is the contract's *missing exit path* plus the upgrade governance around it, and it is auditable
  on‑chain every block (`getStake(reserveHotkey)` vs. the claims escrow).
- **The compounding.** Staked to a validating hotkey, the reserve earns its pro‑rata share of the 41%
  validator emission as **auto‑restaked α** — it grows every tempo even with zero new deposits, and
  every α of that yield is validator emission that **never reaches liquid supply** (§12.4). The reserve
  hotkey's delegate **take** skims reserve yield to the hotkey owner — run it at **take = 0** to keep
  the flywheel whole (§15.2).
- **The side‑effect (intended).** Reserve stake is consensus stake: it compounds the owner‑validator's
  Yuma weight and dividend share, hardening the owner's **majority‑validator** posture in the early
  network (§9.2). Decentralizing the validator set later is therefore a deliberate governance step —
  e.g. re‑delegating reserve slices across independent validators — with a built‑in budget (§6.4.3).
- **What miners get instead.** Nothing from deposits — by design. Both tiers are paid from **emission
  only** (§8.3); revenue reaches miners through the token (recurring buy pressure + a shrinking float →
  the α they earn is worth more), not through pass‑through payouts. The base supply‑side relationship
  stays a business‑layer concern (the NO's own off‑chain payouts), and the subnet is the α upside
  (§12.4 states the bet and its caveats honestly).

### 7.5 Publishing deposits — the events are the record

`seed/INCENTIVES.md`: "NO publishes list of their deposits and signs with wallet." No extra commitment is
needed: each deposit is already an **on‑chain event** (`Deposited(e, noId, from, amount)`) signed by the
operator's tx, so a NO's per‑epoch deposits and cumulative conviction are publicly and authoritatively
**summable straight from the event log** — that *is* the signed, published deposit list. Validators read
it directly (§8.1): this‑epoch deposits for the demand signal, all‑time for the conviction/tier. The
contract stores **no** per‑NO deposit ledger (D25) — the log is the single source of truth.

---

## 8. Miner channel: per‑NO pool, validator‑weighted by demand × quality, Merkle‑claimed

### 8.1 Across operators — validators weight the pools; Yuma emits ∝ implied‑usage × quality (D25)

**The contract does no weighting.** Independent validators (§9–§10) compute each NO miner‑pool UID's
score **themselves**, from **published data**, and submit it as Yuma weights; Yuma medians/clips the
scores and emits to the pool UIDs — so the miners' reward *is* the validators' evaluation. Each validator
reads, per NO `n`, straight off chain state and its own trails:

- **`epoch_deposit_n`** — this epoch's deposits, summed from the `Deposited` event log (§7.5);
- **`tier_n`** — the NO's conviction tier, from its cumulative locked α (§7.2), and the published
  **`rate(tier_n)`** (§7.3);
- **`Q_n`** — the pool's aggregate provider quality, from the validator's own `VALIDATOR.md` trails.

and sets

```
weight_n  ∝  implied_usage_n × Q_n,   implied_usage_n = epoch_deposit_n / rate(tier_n)
```

**Implied usage, not raw deposit** (decision A, D25): a NO on a lower tier rate posts less α for the same
real usage, so dividing by `rate(tier_n)` gives it the **same** weight — the conviction stake that bought
the lower rate is a *discount, not a penalty*, and the weight still tracks **real revenue‑backed usage**
(the headline thesis) rather than raw α. NO `n`'s miner‑pool UID then accrues over the epoch

```
emission_n  ≈  0.41 · E_epoch · (1−θ) · ŵ_n,   ŵ_n = consensus(implied_usage_n · Q_n) / Σ_m consensus(implied_usage_m · Q_m)
```

**as α stake the contract holds** (it owns the pool UID — the NO never receives it). Demand (implied
usage) anchors `ŵ_n` to revenue‑backed usage (§7); the consensus‑measured `Q_n` modulates it — a NO with
poor providers earns less even at high demand (swing capped at bootstrap, §12.3). The contract reads
`emission_n` on‑chain (`0x802 getIncentive`/`getEmission`) for settlement and stores no demand signal of
its own. (Validators each apply the same published rate schedule + their own `Q_n`; Yuma's
median/clip/vtrust over many independent validators is what turns those into the pool's pay, §10.)

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
poolTotal_n  =  emission_n        // miner emission ONLY (read on-chain, 0x802); deposits are reserved (§7.4)
```

There is **no global claim root**: a provider `p`
claims against its NO's *own* committed `payoutRoot`, which holds **fractional shares** `s_{n,p}` (Σ = 1),
and the contract derives the α amount from **on‑chain state**:

```
claimMiner(e, n, p, s_{n,p}, proof):
    verify (p, s_{n,p}) ∈ payoutRoot[e][n]
    pay   s_{n,p} · poolTotal_n              (slippage-free transferStake)
    require claimedMiner[e][n] + amount ≤ poolTotal_n      // a pool can't be over-drained
```

So the amount is a deterministic function of on‑chain state (the pool's swept emission) × the NO's
committed share — **nothing is computed off‑chain at finalize**, removing the one remaining "who computed
this root" trust step. The contract caps cumulative payout per pool at `poolTotal_n`: a NO whose shares
sum to > 1 just drains its own pool early (hurting its own providers — a reputation cost); shares < 1
leave a remainder that rolls over. A provider attached to several NOs makes **one claim per NO** (the
trade for dropping the global root). **Every α of the miner channel flows contract → provider; the
operator holds none of it.**

> **The pool is the unit of scale.** Providers number 100k+ and cannot be UIDs, so the *pool* is the
> on‑chain miner (one UID per NO) and the providers are paid *inside* it by Merkle claim. Yuma operates
> at the pool granularity (§10); the within‑pool split is §8.2.

### 8.4 Top-level miners — the direct head channel (routable-IP breadth, D27)

The pool tier scales to 100k+ providers but pays them by a NO-directed Merkle split. The **top ~200
fleets by routable-IP breadth** *also* get the **canonical Bittensor treatment**: each claims **its own
miner UID**, is **steered directly by validators**, and is **paid natively**. This is the supply apex of
§1 — and it is *more* trust-minimized than the pool (no operator in the payout path).

**The metric — distinct routable egress IPs, not traffic (D27).** A "top miner" is ranked by **how many
unique IPs it can route through**, not how much traffic flows — the real VPN supply signal. Because
eligibility enforces **one provider (`client_id`) ⇄ one egress IP** (`VALIDATOR.md` §8.2), the ranked
unit is a **fleet**: an operator that presents **many** routable exit IPs across the `client_id`s it
runs. A fleet's score is the count of **distinct routable egress-IP-hashes** its `client_id`s serve,
**split when shared**: each distinct IP-hash contributes **1.0 total, divided equally among every top
miner claiming it** (if fleets A and B both route IP Q → 0.5 each), so overlapping IP pools can't be
double-counted to inflate rank. An IP counts only if it is *routable* — the validator completed a real
trail hop through it (`VALIDATOR.md` §7), so liveness is baked into the metric and there is no separate
quality term.

```
for top-level-miner UID u (a fleet — a hotkey with bound client_ids C_u, §11.4):
    IPs(u)   = { distinct egress-IP-hash h : some c ∈ C_u routed a verified trail hop from h }   # VALIDATOR.md §7
    claim(h) = #{ top miners u' : h ∈ IPs(u') }                                                  # split across sharers
    score(u) = Σ over h ∈ IPs(u) of  1 / claim(h)         # split-adjusted unique routable-IP count
    head_weight[u] = score(u)                             # weight IS the IP score (decision B)
```

Every validator computes `score(u)` **from its own trails** — the trail/proof wire carries a per-hop
**egress-IP-hash** (`VALIDATOR.md` §8, a hash at a configurable subnet granularity — default /29 IPv4,
/48 IPv6 — never the raw IP), so a validator can count each fleet's distinct routable IPs and the sharing
splits without trusting anyone. This is the **verify-the-top-200** requirement: a validator only weights
a claimed top miner as high as its *own* observed IP score justifies.

**Identity — the fleet `client_id`s ⇄ hotkey binding (§11.4).** A fleet publishes a **dual-signed
binding** (§11.4) of its `client_id`s (a short list or a Merkle root) to its head hotkey, proving it
controls both the `client_id`s (their `VALIDATOR.md` Ed25519 keys) and the hotkey (its BT key);
validators read it as a free state query and **fail-closed** if the hotkey is not a live UID. The dual
signature (cf. SN51 Celium's `associate_evm_key`) stops a fleet from claiming `client_id`s — hence IPs —
it does not operate.

**Emission — native, direct to the fleet.** Yuma credits a miner's incentive as **α stake on its own
hotkey under its own coldkey — no take, not shared with nominators** (verified against `run_coinbase.rs`).
So a top-level miner is paid **directly**: no contract custody, no Merkle claim, no NO middleman. (Child
hotkeys **cannot** route miner incentive — *"only the validation emission is split amongst parents"* — so
each top miner is genuinely its own UID.)

**Promotion / demotion — the chain's native tournament.** There is **no native "top-N keeps the slot"**
primitive; the only on-chain UID reallocation is **deregistration of the lowest-*emission* neuron** (tie →
oldest `reg_block` → lowest UID; owner/immune skipped) when a new `burned_register` hits the full subnet.
That *is* the tournament, driven by the weights validators set:

- **Promote.** A fleet whose measured IP `score` would out-earn the eviction floor `burned_register`s a
  UID; validators weight it on `score`; its bound `client_id`s are **removed from any NO's `payoutRoot`**
  (promoted out — **no double-pay**; a `client_id` earns in exactly one tier at a time — the server's
  head-exclusion, §8.3).
- **Demote.** A fleet whose routable-IP breadth shrinks earns the lowest emission, is pruned on the next
  registration, and its `client_id`s **fall back to earning via their NO's pool** — the baseline catches
  them.
- **Score-dip protection (a real risk).** Pruning reads only *current* emission rank, no history, so once
  a UID's `immunity_period` expires one thin measurement stretch can evict a good fleet. Mitigations, all
  owner-tunable: a **high `immunity_period`** (a full measurement ramp for new top miners), an **EMA on
  `score`** (so a single noisy epoch doesn't thrash emission), and a **θ large enough that the lowest top
  miner clears the tail** (else the head thrashes against the pools, §8.5). Each (re-)registration pays
  the continuous burn auction (≈ ×1.26 / registration), so churn has a real, tunable cost — and a per-UID
  burn makes the head **more** Sybil-resistant than one pool UID per NO.

**Weight shaping (best practice for ~200 concurrent fleets).** Steer **proportionally** to `score`, *not*
winner-take-all; **set `max_weight_limit`** to a real cap (the chain default is *no cap*, so one fleet
could dominate the head); and drive `VALIDATOR.md` trails at a rate (validator-configurable, §D26) that
gives every top UID regular coverage so honest-but-idle fleets don't stale-decay. This matches the
strongest DePIN precedents — FileTAO's live-scoring and TPN's robust statistics.

### 8.5 The head/tail emission split θ

Both tiers are miner UIDs in **one** mechanism (§14), so the 41% miner emission is divided by the
**weights validators set** — and to make that a controllable policy rather than an accident, validators run
common software that **reserves a governance share θ to the head and `1−θ` to the pools**, exactly as Data
Universe (SN13) reserves a fixed share to one UID by rewriting weights before `set_weights`:

```
head[]  =  { score(u) }               normalized so Σ head = θ      # split-adjusted routable-IP breadth (§8.4)
pool[]  =  { implied_usage_n × Q_n }  normalized so Σ pool = 1 − θ  # implied_usage = deposit / rate(tier) (§8.1)
w       =  head ⊕ pool                # one vector over all miner UIDs; commit-reveal; apply max_weight_limit
```

Both shares go to **real recipients** (top miners; contract-owned pool UIDs), so the **June-2026
`(1 − miner_burned)` penalty does *not* apply** — that penalty only bites emission *withheld to an
owner/immune key* (Spec 421, subtensor PR #2781). **Do not "reserve baseline" by burning to an owner UID** —
it would shrink the subnet's whole cross-subnet allocation. Because Yuma clips to the κ-stake-weighted
median, θ takes effect only if a **stake-majority of validators run the same θ** — so θ is a *published
governance parameter*, not per-validator discretion.

**θ is the load-bearing new decision, because it trades the two bets against each other.** Demand-coupling
(`implied_usage × quality`, the headline bet of `COMPARISON.md`) lives entirely in the **`1−θ` tail`; the
head is pure routable-supply breadth (§8.4), *decoupled* from deposits.
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

## 9. Validator channel: native dividends only (v1)

Validators are **independent** — no NO owns or pools them (the per‑NO validator pool, the NO↔V
intersection split, and the per‑path VT of earlier drafts are gone, §13.6). A validator is the source of
the subnet's core data — **which providers are the weakest links** (`VALIDATOR.md` §7). In v1 it is paid
**only** for accurate, consensus‑aligned scoring, via **native Yuma dividends** — the plain Bittensor
norm; there is **no** separate protocol reward for trail *effort* (D29 — the effort bounty is out of scope,
its design parked in §9.3 as a future‑iteration candidate). ("Verifier" and "validator" are now one role.)

### 9.1 What a "validated path" is

A completed `VALIDATOR.md` trail yields a **published proof** `{header, hops[(client_id, time)],
final_sig (NO server), verifier_sig (vpk)}`, with identity

```
pathId = keccak256(trail_id ‖ vpk ‖ server_key_id)
```

Because it carries **both** the NO server's `FINAL` signature and the validator's `vpk` signature, a
path that verifies is **self‑proving** — anyone checks it with the `0x402` precompile (§11.3). This is
what the failure statistics are built from (§9.4) — and what the parked bounty design would count as
effort, were it ever built (§9.3, D29).

### 9.2 The v1 reward stream: native dividends, owner‑majority

A validator stakes its **own** α (the Sybil ante + the Bittensor permit qualifier — this absorbs the old
"verifier bond"), runs `/verify` trails, and each tempo scores every NO miner pool `implied_usage × Q_n`
under commit‑reveal (§10). It earns **exactly one** stream: **native Yuma dividends** (∝ stake × **vtrust**) —
its share of the 41% validator emission, flowing **natively** to its own hotkey (the contract does
**not** custody it). Dividends reward **accurate, consensus‑aligned scoring**, and commit‑reveal makes
that scoring require *running real trails* — copying stale consensus loses vtrust (§5.1/§10).

**Why no effort subsidy — and why it is out of scope, not merely deferred.** The owner is expected to be the
**majority validator** in the early network — it holds the majority of α, and the buyback reserve staked to
its hotkey (§7.4) compounds that position every tempo — and it has an *intrinsic* motive to run trail
volume: the failure data is the product its network runs on. A protocol‑level effort reward would only pay
for verification the owner does **not** control, so it has no customer until a broad independent validator
set exists. Earlier drafts (D23) kept a *committed* bounty phase in reserve; **D29 removes that commitment**:
whether independent‑validator coverage is even thin enough to need an effort subsidy — and if so, what shape
it should take — is a question we will **answer from the live launch**, not pre‑commit before the network
exists. Independent validators are welcome from day one (§9.7) on the same bar as any subnet: dividends must
cover their measurement costs.

### 9.3 The effort bounty — parked design (out of v1 scope; a future‑iteration candidate)

> **Status: OUT OF SCOPE for v1, and no longer a committed future phase (D29).** This subsection is retained
> as **parked reference** — the formulas and the already‑built machinery (coverage‑bound digests, sampled
> proofs, HF‑2 reseed caps, `snclaim`) are preserved so a future iteration *could* pick them up — but it is
> **not a roadmap item**. v1 deploys no fee pool, no `submitTrails`/effort claims, no contract validator
> registry, and no `claimValidator`, and there is **no committed trigger** to add them; a validator‑effort
> incentive would be designed (in this or another shape) only if the launched network shows it is needed.

The bounty pool each epoch is

```
FeePool  =  φ · Σ_n D_n        (a deposit fraction φ, reintroduced with this phase — carved from the buyback flow)
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
it drives the miner emission — and is published as a public good. In v1 the majority of this data is
produced by the owner‑validator, whose business runs on it (§9.2). If a future iteration ever finds
independent‑validator coverage too thin, the parked §9.3 bounty is one lever that would pay validators
**precisely for producing this data** — weighted toward the coverage gaps — but v1 does not include it (D29).

### 9.5 Anti‑gaming for validators

- **Honest scoring** — on‑chain Yuma: a validator inflating a pool (or knifing a rival) is **clipped** to
  the κ‑median and **loses vtrust → loses dividends**; the **self‑weight mask** (§10) stops it scoring its
  own NO at all; **commit‑reveal** stops it copying fresh consensus.
- **Honest effort** — not an on‑chain concern in v1 (no effort reward to game); the parked §9.3 bounty, if
  ever built, would make effort crypto‑verified (sample + dispute), server‑assigned, under‑sampling‑weighted.
- **Independence** — because most validators run no NO, the κ‑median tracks ground truth (`VALIDATOR.md`
  §1) — the structural defense against a NO colluding with a validator to fake its own `Q_n` (further
  bounded by `VALIDATOR.md` §5.4).

### 9.6 The owner's role

The owner is the governance **referee**, not a party to a per‑path split (the old "disagreement → owner"
went away with the intersection split). In v1 it is also the **majority validator** (§9.2) — it runs the
trail volume and the steering that depends on it — and it tunes θ, the reference rate, and the §12.3
caps. It reviews the
**statistical** disputes (`VALIDATOR.md` §7.7) that the on‑chain crypto layer can't decide (self‑dealing
patterns, adversarial abandonment) and drives the `VALIDATOR.md` §10 roadmap. Per `seed/INCENTIVES.md`, the
residual "how much the owners mistrust the NOs / NOs mistrust each other" is now read from **consensus
divergence + the disputes**, not from a contested‑value pot.

### 9.7 Validators are permissionless and Bittensor‑native

Entry is the standard path: **stake α, earn a permit (top‑k by stake), validate** — no NO, no owner
approval, no pool. Most validators run no NO, which is exactly the **independence** `VALIDATOR.md` §1
needs and the structural defense against self‑dealing (§9.5, §12.3). In v1 the owner is the majority
validator by construction (§9.2, §7.4); cultivating a broad, independent validator set is a stated goal of
a **later** phase — **re‑delegating reserve slices** (§6.4.3) is the committed lever, and a
validator‑effort incentive (the parked §9.3 bounty, or another shape) is a *possible* addition if launch
shows coverage is thin (D29) — and a down payment on the `VALIDATOR.md` §10 roadmap. (Earlier drafts split
validators into "NO pools" vs "community"; there is now **one** kind — independent — so the distinction is gone.)

---

## 10. Setting weights: two channels (implied‑usage × quality + routable‑IP breadth), by validator consensus

Each tempo **every independent validator** (§9) scores **both miner tiers** — the NO pools *and* the
top-level miners (§8.4–8.5) — from **published data + its own** `VALIDATOR.md` trails, and submits the
vector under commit‑reveal, so the validators' evaluation is what moves the miner emission (the Bittensor
mechanism). The contract computes nothing (D25):

```
for validator v:
    # TAIL — NO pools: implied-usage × quality (D25)
    for each NO pool p (miner-pool UID of NO n):
        implied_usage_n = epoch_deposit_n / rate(tier_n)   // deposits from the event log (§7.5); rate from the published schedule (§7.3)
        pool[p] = implied_usage_n · quality_v(p)           // §9.4 aggregate; = 0 if v operates NO n (self-mask)
    # HEAD — top-level miners: split-adjusted routable-IP breadth (§8.4, D27)
    for each top-level-miner UID u (fleet — client_ids C_u bound to u, §11.4):
        head[u] = score_v(u) = Σ_{h ∈ v's routable IP-hashes for C_u} 1 / (# top miners v sees claiming h)   // = 0 for v's own UID
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
  tracks `implied_usage_p × consensus‑quality_p`: **implied usage anchors it to revenue‑backed demand
  (§7–§8.1), and the measured pool quality modulates it** — a NO with poor providers earns less even at
  high demand (swing capped at bootstrap, §12.3). The *within*‑pool split to providers is the separate
  per‑provider step (§8.2). **Head:** `incentive_u ∝ Σ_v stake_v · clipped score_v(u)`, so a top-level
  miner's emission tracks its **consensus routable-IP breadth** (§8.4), paid **natively** to its own
  hotkey; the θ split (§8.5) sets how the 41% divides between the head and the pools.
- **Validator rewards = dividends, and only dividends.** Native dividends grow with bonds on pools
  consensus later rewards (Liquid Alpha) and with vtrust (accurate scoring) — the **Bittensor‑native**
  reward for good evaluation, and v1's **whole** validator reward. Commit‑reveal makes accurate scoring
  require real trail volume; there is no separate effort reward (the §9.3 bounty is out of scope, D29).
- **Anti‑copying.** Commit‑reveal hides fresh scores, so a lazy validator copying stale consensus drifts
  from current quality and loses vtrust (§5.1).

Hyperparameters: `commit_reveal_weights_enabled = true`, `liquid_alpha_enabled = true` (reward early
pool discovery), `max_weight_limit` set to a real cap (chain default is *no cap*) so no single UID dominates either tier, `mechanism_count = 1` (a 2nd mechanism would halve the 256-UID space, §14), `weights_version_key` bumped to
force validator‑software upgrades (§15.1).

> **Why this is real Yuma.** With a sole validator the consensus would be inert; with **many independent
> validators** — most running no NO — scoring the pools, median / clip / vtrust / bonds all do their job,
> and that independence is the disinterested baseline that keeps the consensus honest (§9.5). v1 starts
> owner‑majority by construction (§9.2, §7.4); the machinery (commit‑reveal, clipping, vtrust, the
> self‑weight mask) is switched on from day one so the consensus does real work as independents join
> (§9.3, §13.6).

---

## 11. The data layer: commitments, Merkle, and disputes

### 11.1 What goes on‑chain vs. off‑chain

| Datum | Where | Why |
|---|---|---|
| `D_n`, deposit events, `buybackTotal`, `poolTotal[e][n]` | contract storage | the demand signal, the reserve, and claim *amounts* — all on‑chain |
| `payoutRoot[e][noId]` (fractional shares, Σ = 1) | contract storage (in `commitOperator`) | the contract verifies each provider's *share* against it at claim time |
| payout‑share leaves, completed‑trail proof blobs | **off‑chain** (IPFS/HTTPS, pointer in `off`) | bulk data; only the committed roots are trusted (trail proofs would go on‑chain only under the parked bounty design, §9.3/D29 — not in v1) |
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

```

Claims are pull‑based, so settlement is `O(1)` on‑chain regardless of participant count. The **miner**
amount is `share × poolTotal` (share proven against the NO's committed root, pool total read from
on‑chain state) — **no global claim root is computed off‑chain**. (`claimValidator` — the bounty claim,
`feePool · effort / Σ effort` — is part of the parked bounty design, not in v1, §9.3/D29.)

### 11.3 Disputes

The v1 on‑chain dispute surface is deliberately small — deposits are reserved (nothing distributable to
dispute), pool amounts are read from chain state, and effort claims do not exist yet:

- **Bad payout share.** A provider's claim must prove `(coldkey, share)` against its NO's committed
  `payoutRoot`; the per‑pool cap (`claimedMiner ≤ poolTotal`) means a NO whose shares sum to > 1 only
  drains its own pool.
- **Bad `client_id ⇄ hotkey` binding.** A contested/stolen head binding is adjudicated on‑chain via the
  `0x402` Ed25519 check + a metagraph read (§11.4).
- **(Parked bounty design, not in v1 — §9.3/D29.)** Effort claims would be **optimistic**: a validator commits a Merkle
  root of its trails + a claimed effort total; the contract `0x402`‑verifies a **random sample** of the
  committed leaves (`FINAL` against the NO's server key, `verifier_sig` against the validator's `vpk`),
  and during the +24h…+48h window **anyone may dispute any leaf** with the same check — a single failing
  leaf voids the whole claim and forfeits the validator's stake at risk, so a fabricated trail is never
  worth the gamble and the contract never verifies *every* trail (it scales).

Statistical disputes (a validator's trails look self‑dealt or coverage‑gamed, `VALIDATOR.md` §7.7) are
**not** resolved on‑chain in v1; they inform governance (validator de‑listing, stake forfeiture). The
on‑chain layer handles only what is cryptographically decidable.

### 11.4 The fleet `client_id`s ⇄ hotkey binding (top-level miners)

To steer the head (§8.4), a validator must map each measured `client_id` to a top-level miner's UID — and
because a head miner is a **fleet** (D27), one head hotkey binds **many** `client_id`s (the fleet's
providers), across which the validator counts the fleet's distinct routable egress IPs. The binding is
**published, signed, and cheap to read**, using the standard Bittensor "signed proof → registered hotkey"
pattern (Epistula / ORO-AI `bittensor-auth`) — with a **dual signature** so a fleet cannot claim a
`client_id` (hence an IP) it does not operate.

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

**Identity ⊥ score.** The binding proves *ownership* only; `VALIDATOR.md` proves *routable-IP breadth*.
They stay separate (as every comparable subnet does — Targon keeps the hotkey out of its TEE
attestation): a validator attributes each trail hop to a fleet UID via the binding, then scores that UID
by the split-adjusted count of distinct routable egress-IP-hashes it observed across the fleet's
`client_id`s (`VALIDATOR.md` §7, §8.4).

**Privacy — opt-in self-deanonymization.** Publishing `client_id → hotkey` (→ egress IP, `VALIDATOR.md`
§8.1) **does** deanonymize, so it is **voluntary and only for providers claiming a public top slot** —
claiming the public UID *is* the consent. The long tail stays `client_id`-pseudonymous inside the pools.
(Optionally the NO `/verify` server — which already co-signs trails and authoritatively knows
`client_id ⇄ egress-IP` — can add a third attesting signature; it strengthens the binding at the cost of
NO-trust, and is not required.)

---

## 12. Economic analysis

### 12.1 Operator equilibrium

Let a NO with real customer revenue `R_n` (α‑equivalent) choose its conviction tier and its per‑epoch
deposit `D_n`. Over an epoch the **contract pays that NO's providers** `0.41·E·ŵ_n·(1−θ)` (the NO's pool
emission, `ŵ_n ∝ implied_usage_n × Q_n`, `implied_usage_n = D_n / rate(tier_n)`, §8.1); the deposit and
any conviction stake are both locked in the reserve (§7.4), and the NO keeps `R_n − D_n` off‑chain.
Providers attach where expected pay is highest. Two forces balance:

- **Raising `D_n`** (or raising the tier to lower `rate(tier_n)`) lifts the NO's implied usage → its
  emission share `ŵ_n` → the α its providers earn → attracts capacity → more real usage → more `R_n`
  **and** higher measured `Q_n` (which feeds back into `ŵ_n`).
- **`D_n ≤ R_n`** in the long run: both the deposit and the conviction stake are **fully sunk** — locked
  in the reserve, recoverable by no one — so α not backed by customer revenue is pure loss.

The emission `E` is a subsidy auctioned ∝ implied usage: in equilibrium NOs commit α (deposits + tier
conviction) up to where the marginal emission steered to their providers equals the marginal cost of the
sunk α. Staking for a lower rate is a **capital‑for‑cashflow trade** (lock more up front → post less per
epoch) that nets to the same weight per unit of real usage, so it lowers the onboarding barrier without
distorting the usage signal. This ties total commitment to total real demand and makes `w_n` track
genuine usage share — the intended outcome. (It is the deposit‑for‑weight pattern Bittensor already
trusts in burned registration, with the burn replaced by a transparent, compounding lock.)

**Self‑dealing — closed structurally.** The worry is a NO that runs its *own* providers so its deposit
round‑trips to itself. The round‑trip **does not exist**: no deposit or conviction α is ever distributed
(§7.4), so recovery through the payout channel is **zero** — the full commitment is sunk for attacker and
honest NO alike. Buying cheaper weight via a higher tier costs *more* locked capital, not less, so the
tier discount does not open a self‑dealing edge. What remains is the *steering* channel — a deposit still
buys pool emission `ŵ_n`, captured by whoever the NO's providers are — bounded by
`0.41·E·(1−θ)·(implied_usage/Σ) − (sunk α) − (real infra to pass live trails)`, gated by winning
**quality consensus** `Q_n` against independent measurement with the **self‑weight mask** on (§10). The
honest residual (a determined self‑dealer with real infra *and* stake) is the `VALIDATOR.md` §10 class,
exactly what a broad independent validator set + that roadmap close (§12.3).

### 12.2 Validator equilibrium

In v1 a validator earns **native dividends** (∝ stake × vtrust — the Bittensor‑native reward for
accurate scoring); its profit is `dividends_v − (cost of running trails)`. Commit‑reveal makes stale
copying lose vtrust, so accurate scoring (hence trails) is needed to hold dividends (§5.1). The
**majority validator is the owner** (§9.2), whose measurement cost is already sunk in operating the
network — so v1 needs no protocol‑level effort subsidy, and does not include one (D29). Whether a future
iteration ever adds a validator‑effort lever (the parked **(X)** fee‑funded bounty / **(Y)** emission‑routed
designs, §9.3/§13.6) is an open post‑launch question, not a committed roadmap.

### 12.3 What this does and does not secure

- **Secured:** cross‑operator emission tracks `deposit × consensus‑quality` (costly, revenue‑backed
  demand × independently‑measured liveness) — **validators' evaluation drives the miner payout**, the
  Bittensor mechanism — via median + clipping + vtrust over many **independent** validators, plus the
  self‑weight mask and the fully‑sunk deposit (§10, §12.1); provider quality also bites **within the
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
- **Effort‑incentive posture (v1 is dividends‑only, by choice):** native dividends are ∝ stake×vtrust, which
  is effort‑agnostic — a high‑stake validator could in principle under‑measure and coast. v1 accepts
  this because the high‑stake validator **is the owner** (§9.2), whose business depends on the
  measurement. Whether owner‑independent coverage ever becomes thin enough to need an effort fix — and if
  so, whether the parked **(X)**/**(Y)** designs are the right one — is left to a post‑launch iteration
  (D29), not pre‑committed here.

### 12.4 The buyback reserve and the demand crossover

The reserve turns the token flow one‑directional. Per epoch, in α units:

```
locked in:   B_e  =  Σ_n D_n                       // deposits: market-bought, staked to the reserve (§7.4)
             Y_e  =  0.41·E_e · s_e · (1 − take)   // reserve dividends: its share s_e of subnet validator stake
liquid out:  L_e  ≈  0.41·E_e                      // miner emission (both tiers)
                   + 0.41·E_e · (1 − s_e)          // outside-validator dividends
                   + 0.18·E_e                      // owner cut
```

`E_e` (α issuance) is on a fixed, halving schedule; `B_e` scales with **usage revenue**. Define the
**demand ratio** `R_e = (B_e + Y_e) / L_e` — publishable every epoch straight from chain state
(`Deposited` events + `getStake(reserveHotkey)`; the §16.1 indexer's headline stat). Three regimes:

- **Bootstrap (`R_e ≪ 1`).** Emission dominates; miners are paid an issuance subsidy while usage grows —
  the standard subnet posture, with the buyback as a visible, growing bid under the token.
- **Crossover (`R_e → 1`).** The point the design aims at — **the buyback is expected to eventually
  exceed what emission can provide**, because one side halves on a schedule while the other compounds
  with revenue. Past it, net float *shrinks* every epoch that usage holds: α becomes structurally
  demand‑dominant, and miner pay (emission‑denominated) is backed by a shrinking‑supply token rather
  than by pass‑through revenue.
- **Staking pulls the crossover in from both sides.** This is what staking the reserve *does*: an
  unstaked reserve **melts** (zero yield, diluted by issuance), while the staked reserve earns `Y_e`,
  which grows the locked side **and** shrinks the liquid side — every α the reserve earns is validator
  emission that never reaches a would‑be seller. As the reserve's validator‑stake share `s_e` compounds
  toward 1, liquid issuance falls toward `0.41·E + 0.18·E`: the validator 41% is progressively recycled
  into the lock, independent of new revenue.

Honest caveats, stated once: **(1)** miner income is now **price‑mediated** — if the market does not
price the buyback, high usage no longer lifts miner pay directly (the bet every revenue‑subnet makes;
the old deposit‑funded floor is gone, §7.4). **(2)** The reserve is a **growing honeypot** custodied by
an upgradeable contract — the §6.4 phases (timelock → immutabilization) carry real weight. **(3)** The
lock is **governance‑credible, not physical**: dTAO stake has no unbonding (§2.3), so credibility = no
exit path in code + the upgrade process around it (§6.4). **(4)** Reserve‑as‑consensus‑stake
concentrates Yuma power with the owner **by design** (§7.4); decentralizing the validator set later must be
a deliberate, budgeted step, its committed lever **re‑delegating reserve slices** (§6.4.3).

---

## 13. Design decisions and alternatives

### 13.1 Settlement: contract custodies the miner pools; validators are independent (chosen)

The contract **owns each NO's miner‑pool UID** outright, so the **tail's share of the 41% miner emission**
accrues to the contract and is paid out by direct Merkle claim (the **head is native** — top-level miners
own their UIDs and are paid to their own coldkey, §8.4) — *a network operator never custodies emission destined
for its providers* (the hard requirement). The **weights are set by independent validators** (§9, §10),
not the contract, so Yuma consensus does real work; their **41% dividends are native** (no middleman to
remove; a fee‑funded effort bounty is out of scope, §9.3/§13.6/D29). Implications: the contract is
**custody‑critical** for the miner emission + the buyback reserve (audited code; §6.4 timelock/guardian
governance), and it owns **one miner‑pool UID per NO**, so budget `max_allowed_uids` and registration
burns to the **NO count** — providers are *not* UIDs, they live inside the pools. No α→TAO→α churn.
*Rejected:* **per‑provider UIDs** (100k+ ≫ the ~256 cap — the reason for pools, though the **top ~200 do get their own UID** — the head tier, §8.4); letting emission land on
NO hotkeys (violates no‑custody); a **single** contract miner UID with the contract as sole validator
(collapses Yuma); and the earlier **per‑NO validator pool with a take‑0 custody hack** (fragile, and
redundant with crypto‑validity — replaced by plain independent validators earning native dividends, §13.6).

### 13.2 Payment token: α (chosen) vs. TAO vs. USDC

α aligns the contract with subnet value, creates buy/stake pressure, and keeps all internal transfers
**slippage‑free** (`transferStake` within‑netuid). Cost: participants bear α price risk while holding,
and exit (α→TAO) has AMM slippage. TAO/USDC would remove volatility but α is not a liquid EVM token,
TAO settlement forfeits the alignment, and USDC adds a bridge dependency. α chosen per the approved
direction; the off‑chain reference rate (§7.1) lets NOs target a *fixed real* price despite α volatility.

### 13.3 Emission steering: multi‑validator Yuma consensus (chosen)

The cross‑NO split is a genuine on‑chain **consensus output**: many independent validators score
`implied_usage × quality` (§10), so median / clipping / vtrust / bonds all operate. This is the
**Bittensor mechanism** — validators evaluate, and their evaluation (not a fixed formula) drives the
miners' pay; a
design where validator input is *off* the payout path would miss the point of Bittensor. The cost is the
standard subjective‑weight toolkit (commit‑reveal, self‑mask, Liquid Alpha), switched on, plus a healthy
independent validator set — which §9.7 cultivates (re‑delegating reserve slices is the committed lever;
native dividends are the reward). *Rejected (briefly explored):* a **deposit‑only** weight — simpler, but
it takes validators off the miner payout path and reduces them to a side‑channel, so it was reverted.

### 13.4 Quality in the cross‑operator weight: **adopted** (ramped at bootstrap)

`weight_n ∝ implied_usage_n × Q_n`, with `implied_usage_n = deposit_n / rate(tier_n)` the demand anchor
(§8.1, D25) and `Q_n` the consensus‑measured pool quality (§8.1, §10): demand is the objective anchor,
quality is the earned modulator, and together they make validators' evaluation the thing that moves the
money. The one nuance is **magnitude at bootstrap**: `Q_n` is noisy until the
validator set + data mature, so governance **caps the quality swing** early (closer to deposit‑weighted)
and widens it as the independent‑validator stake share grows (§12.3). So quality is on the payout path
from day one — we ramp its *strength*, we do not defer the mechanism.

### 13.5 No on‑chain oracle (simplified out)

Because per‑GB / per‑user usage is self‑reported and unverifiable on‑chain, an on‑chain rate has no
enforcement power — the weight is just *α deposited* (§7.1). v1 therefore has **no oracle**: the "global
fixed rate" is an off‑chain governance‑published reference NOs use to price customers and size deposits.
(If a future version ever needs an on‑chain α/USD value — e.g. to denominate the deposit fee in USD —
the `0x808` α price is already trustless and only TAO/USD would need a committed validator‑median feed.)

### 13.6 Validator effort reward: v1 is native dividends only; the effort bounty is out of scope (D29)

The validators' output — *which providers are the weakest links* — is the product. Yuma pays validators
**∝ stake × vtrust**, which is effort‑agnostic, and **v1 ships exactly that and nothing more.** Earlier
drafts kept a *committed* effort‑reward ladder in reserve; **D29 retires that commitment** — whether any
explicit validator‑effort reward is ever added, and in what shape, is a **post‑launch open question**, to be
decided from what the live network shows about independent‑validator coverage. The two designs below are kept
as **parked reference** (formulas + already‑built machinery preserved), **not a roadmap**:

- **(W) — v1 as shipped: native dividends only.** No fee pool, no effort claims. The owner is the
  majority validator (α holdings + the compounding reserve, §7.4/§9.2) and runs the trail volume out of
  intrinsic interest; commit‑reveal already forces any dividend‑earning validator to measure. This is the
  plain Bittensor norm and the **whole** v1 validator reward.
- **(X) — parked candidate: a fee‑funded bounty.** `φ·ΣD + ω·OwnerCut` paid ∝ verified coverage‑weighted
  trails (§9.3), **on top of** native dividends. Would keep validators as **independent on‑chain UIDs
  running real Yuma consensus** (median/clip/vtrust intact) and needs **no emission capture** — the simplest,
  most Yuma‑native option *if* an effort reward is ever wanted. **Not in v1**, and no committed trigger to
  add it.
- **(Y) — parked candidate: route the 41% validator emission itself through the effort split.** The contract
  would capture it and pay ∝ trails — the strongest effort incentive, but capturing requires contract‑owned
  validator UIDs, which moves the quality consensus **into the contract** instead of on‑chain Yuma. A
  heavier last resort, kept only as reference. **Not in v1.**

*Independently eliminated (and **staying** eliminated — not resurrected by any future effort reward):* the
per‑NO validator pool, the NO↔V **intersection split**, **VT**, the verifier **bond**, `attestedPathsRoot`,
and the **take‑0 custody hack**. The intersection split was redundant for fraud detection (a valid path is
co‑signed = agreed by construction; an invalid one is caught by the `0x402` check, §11.3) and was a weak
effort proxy. v1's validator side is simply: **stake α, run trails, earn native dividends.**

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
| Reserve hotkey | — | the owner‑validator hotkey, set at initialize | the buyback reserve's staking target (§7.4); run its delegate **take = 0** so reserve yield compounds whole |
| `φ` / `ω` / effort‑claim deadline / dispute window | — | — | **out of scope** — parked bounty design (§9.3/§13.6, D29) |
| **Head share** | `θ` | governance; start ~0.3, ramp | fraction of the 41% miner emission to top-level miners; `1−θ` to pools — **the load-bearing new dial** (§8.5) |
| Coverage weighting | — | (parked bounty design only) | up‑weights under‑sampled / weak providers (§9.3) — not in v1 (D29) |
| Validator min stake | — | governance | permit qualifier + Sybil ante (§9.7) |
| **Deposit‑rate schedule per conviction tier** | `rate(tier)` | governance, **off‑chain published**; zero tier = baseline; **floored > 0** | α‑per‑usage by conviction; validators read it to weight (§7.3, §8.1). Staking lowers the rate — the onboarding/alignment lever (D25). |
| **Egress‑IP‑hash granularity** (head IP‑score) | — | **configurable subnet param**; default /29 IPv4, /48 IPv6 | the "distinct routable IP" unit for the head score (§8.4, D27); trails hash egress IPs at this prefix (`VALIDATOR.md` §8) |
| **Validator measurement rate** | — | **validator‑configurable; guardrails off by default** (D26) | each validator drives its own trail cadence; only a loose hard per‑IP DoS backstop remains (`VALIDATOR.md` §5.3/§9) |
| Upgrade / param timelock | — | ≥ 1 epoch (target 2 ≈ 14 d) | Phase 1, §6.4.2 |
| Owner / timelock multisig | — | M‑of‑N | §6.4 |
| Guardian (pause‑only) | — | small multisig | §6.4.2 |

### 15.3 Emission (inherited from the chain)

α emission ≈ 1 α/block participant reward (subnet‑uniform), split **18% owner / 41% miners / 41%
validators**, 21M α cap with halvings. The contract does **not** set these (the owner cut is
governance‑settable, the 41/41 is hard‑coded); it steers the *distribution within* the miner pools —
validator **dividends flow natively**, and the slice earned by the buyback reserve's stake auto‑compounds
into the reserve (§7.4, §12.4). The 41% miner share is split **head/tail by θ** (§8.5) — steered by
validator weights to top-level-miner UIDs (native) and NO pools (Merkle), never withheld to an owner key
(which would trigger the post-Spec-421 `miner_burned` penalty).

---

## 16. Implementation plan

### 16.1 Components

1. **ST contract (Solidity, Cancun / 0.8.24).** State + interfaces of §6; precompile bindings
   (Staking V2 `0x805`, Neuron `0x804`, Metagraph `0x802`, Alpha `0x808`, Ed25519 `0x402`); Merkle
   verifier (OZ); the **buyback reserve** (deposit → `moveStake` → locked, §7.4); proxy + owner multisig
   governance. **New work.** (No `submitTrails` effort verifier — the bounty is out of scope, §9.3/D29.)
2. **Subnet bootstrap.** `register_network`; set hyperparameters (§15.1); as each NO onboards, the
   contract `burnedRegister`s its **miner‑pool UID** (owned outright); stand up an initial set of
   **independent validators** (owner‑run at first) so consensus has measurement from day one; register
   mechanism 1 for Pool 1 later. **Top-level miners self-`burnedRegister`** their own (provider-owned, not
   contract-owned) UIDs and publish the §11.4 binding.
3. **Validator software (independent).** Stake α; run `VALIDATOR.md` trails; each tempo score **both tiers** (pools `implied_usage × quality` —
   implied usage = deposit ÷ conviction‑tier rate, computed off the published deposit events; head on its **routable‑IP breadth score**), read the `client_id ⇄ hotkey` binding (§11.4), split by θ, and submit
   commit-reveal weights (standard Bittensor validator loop → native dividends) — **no central
   keeper sets weights**. A separate **permissionless settlement poke** triggers `finalizeEpoch` after
   the +48h window. (No trail‑proof submission — the effort bounty is out of scope, §9.3/D29.)
4. **Network‑Operator software.** Runs the privacy servers + the `VALIDATOR.md` `/verify` server
   (SEED/EXTEND/FINAL, poisoning, idempotency, the four Ed25519 signatures, the egress‑IP index);
   `deposit`s each epoch (conviction stake — the contract keeps no DT ledger, D25); computes provider reliability + payout list; commits the **`payoutRoot`**
   (fractional shares); serves leaves. (No validator pool — it co‑signs trails as the `/verify` server.)
5. **Provider software.** Carries ingress/egress; registers a `client_id`; verifies its payout leaf
   against `payoutRoot`; `claimMiner`s. **If it reaches the top ~200:** `burnedRegister`s its own UID,
   publishes the dual-signed `client_id ⇄ hotkey` binding (§11.4), and earns **natively** (no claim, §8.4).
6. **Validator client (was "verifier").** Stake α; run `/verify` trails; submit commit‑reveal pool
   scores (native dividends); participate in binding disputes. (No `registerValidator(vpk)` /
   `submitTrails` / `claimValidator` — those belong to the parked bounty design, §9.3/D29.)
7. **Indexer/explorer.** Surfaces `D_n`, pool quality `Q_n`, consensus weights, vtrust, the
   **independent‑validator stake share**, the **buyback reserve + demand ratio `R_e`** (§12.4), and
   roots — the public audit surface.

### 16.2 The chain is identity + stake + weights only

Per current Bittensor practice, **`serve_axon` is optional**: the runtime stores but never interprets
the axon protocol, and Yuma consumes only weights + stake. The UR network's transport is the custom
`VALIDATOR.md` HTTP protocol; participants discover each other out‑of‑band (the NO directory in
`README.md`) or via the commitments pallet, and touch the chain only for **registration, the contract,
weights, and emission**. This is a supported, common pattern (model‑commit subnets, orchestrator
subnets) — no Synapse/dendrite required.

### 16.3 Milestones

*(D28: v1 is **testnet-first** — the ladder below (M0 → M3) runs on **testnet** (chain 945 /
test.finney); **mainnet is the later Phase‑E promotion** (M3), gated behind a clean testnet run, after
which the M4+ production phases run on finney. The SP‑1/SP‑2/SP‑3 harness is endpoint‑parameterized, so
it re‑targets with zero code change. Operational detail: `docs/LAUNCH.md`.)*

1. **M0 — Rehearsal + probes (no subnet yet).** (a) **SP‑3 localnet** (docker subtensor pinned to the
   LIVE finney runtime tag, fast blocks): the full genesis dry‑run — deploy, pool‑UID registration +
   custody/move α (`0x805`), **≥ 2 validators** scoring `implied_usage × quality` under commit‑reveal
   (`0x804`) so consensus/clipping/vtrust behavior is exercised, deposits → reserve, the epoch
   lifecycle, and the failure drills (pause, missed commit, upgrade‑under‑fire, sweep retry).
   (b) **Testnet dust probes** (chain 945, against an *existing* testnet netuid, before our subnet
   exists): the SP‑1 battery — custody semantics, rao units, `0x402` gas, blake2f, and **reserve‑hotkey
   dividend auto‑compounding + take** — plus SP‑2 `check-metadata` against the test.finney
   runtime (`docs/LAUNCH.md` Phases A/B).
2. **M1 — Testnet subnet bring‑up (Phase 0 governance from block one).** One scripted window
   (`docs/LAUNCH.md` Phase C): register the subnet + defensive hyperparameters; **own UIDs first**
   (owner‑validator hotkey = reserve hotkey, take 0); deploy under the owner multisig with **short
   epochs** and a **dust deposit cap** (D‑3); `start`; first CRv4 commit; then prove ON TESTNET with
   tiny values: `deposit` → `BuybackReserved` (full amount onto the reserve), miner emission
   accrual to the contract‑owned pool UID, **per‑NO** `claimMiner` against `payoutRoot` ×
   emission‑only `poolTotal` end‑to‑end. **Head:** register a provider‑owned **top‑level‑miner UID**,
   publish the §11.4 binding, and verify **routable‑IP‑breadth** native steering split from the pools by θ.
3. **M2 — Buyback reserve verified live (testnet).** Several short testnet epochs green: dividends
   **auto‑compound** onto the reserve stake (`getStake(reserveHotkey) > buybackTotal`), the **one‑way
   invariant** + on‑chain audit hold, and the upgrade/pause drills leave finalized claims and the
   reserve untouched. (The effort‑bounty rail — `registerValidator`/`submitTrails`/`claimValidator` — is
   **out of scope**, so there is no such milestone in v1; it stays parked, §9.3/D29.)
4. **M3 — Ramp on testnet, then promote to mainnet (Phase E).** `setEpochParams` to the 7‑day epoch
   (+4h/+48h windows, F2‑snapshotted so in‑flight epochs are untouched); the deposit cap raised stepwise
   toward the sized policy; settlement‑poke automation; the reference rate + sourcing commitment
   published; the `R_e` demand‑ratio dashboard live (§12.4). After ≥ N clean testnet epochs, **promote to
   mainnet**: re‑run the M0 probes + M1 genesis against **finney** (chain 964), now under the hard‑gate
   posture (real TAO; genesis is one irreversible window). The M4+ production phases run on mainnet.
5. **M4 — Production Pool 0 (Phase 0 governance, §6.4.1).** Full parameters, **quality‑factor swing
   capped until the independent‑validator stake share is healthy** and `VALIDATOR.md` §10 advances
   (§12.3).
6. **M5 — Harden custody (Phase 1, §6.4.2):** timelock (≥ 1 epoch) on upgrades/params + a pause‑only
   guardian; then **Pool 1 (VPN factory)** via mechanism 1.
7. **M6 — Decentralize further (deferred, §6.4.3):** trustless oracle (§13.5), permissionless bonded
   admission, on‑chain governance; advance the `VALIDATOR.md` §10 roadmap.

### 16.4 Pre‑launch verification checklist (load‑bearing live values)

*(D28: these checks run against **testnet** (chain 945 dust probes) + the SP‑3 localnet — they are the
Phase A/B gates of `docs/LAUNCH.md`, and all must be green before the subnet is registered; re‑run them
against finney at the Phase‑E mainnet promotion.)*

- Precompile addresses/ABIs at the pinned Subtensor release (Staking **V2** `0x805`; Neuron `0x804`
  `setWeights`; Ed25519 `0x402`; Alpha `0x808`).
- `tao_weight` (expect 0.18), `max_allowed_validators`, `min_allowed_weights`,
  `commit_reveal_weights_enabled` default, `SubnetOwnerCut` — query live, set explicitly.
- Subnet creation cost (`btcli subnet burn-cost`) and registration burn bounds.
- Confirm `transferStake`/`moveStake` within‑netuid are slippage‑free on the live runtime; confirm the
  staking precompile's "contract address = coldkey" custody semantics.
- Confirm an **independent validator** earns a permit at expected stake and that its **native
  dividends** accrue to its own hotkey (no contract capture); confirm delegated stake on the reserve
  hotkey **auto‑compounds** to the contract coldkey's stake and that hotkey `take` behaves as expected
  (target take = 0, §7.4).
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
| **How to adapt to standard BT payout formulas?** | Independent validators set standard Yuma weights `= implied_usage × quality` (implied usage = deposit ÷ conviction‑tier rate, computed by the validator off the published event log + rate schedule — the contract weighs nothing, D25) on the per‑NO miner‑pool UIDs; the chain's incentive/dividend split delivers emission to the **miner pools** (which the contract owns → re‑splits to providers per Merkle payout roots, §§8, 11) and ∝ stake × vtrust to **validators natively**. (Native dividends are the whole validator reward; a validator *effort* subsidy is out of scope, §9.3/D29.) No deviation from standard Yuma — it *is* Yuma, with many independent validators (§9). **Plus a second tier:** the top ~200 **fleets by split‑adjusted routable‑IP breadth** hold their own UIDs, steered on that score with native emission, split from the pools by θ (§8.4–8.5). |

---

## 18. Glossary

- **NO (Network Operator)** — runs servers; operates **one miner‑pool UID** (contract‑owned) but holds no
  emission; deposits DT; runs the `/verify` server (co‑signs trails); commits the payout root for its
  providers. No validator.
- **Miner‑pool UID** — the on‑chain miner: one per NO, contract‑owned. The 100k+ providers are **inside**
  it (not UIDs) and are paid by Merkle claim.
- **Provider / miner (tail)** — carries traffic; inside a NO's miner pool (the on-ramp tier); **claims its
  α directly from the contract** per the NO payout root. Can **graduate** to a top-level slot (§8.4).
- **Top-level miner (head)** — a top ~200 **fleet** (an operator of many routable exit IPs) that holds
  **its own miner UID**, steered **directly** by validators on its **split-adjusted distinct routable
  egress-IP count** (`score`, §8.4) and paid **natively** (no contract custody, no Merkle claim); matched
  to its UID by the §11.4 binding; maintained by deregistration churn.
- **`score(u)`** — a top-level miner's head weight: the count of distinct routable egress-IP-hashes across
  its bound `client_id`s, each IP split equally among the top miners claiming it (§8.4, D27).
- **fleet `client_id`s ⇄ hotkey binding** — the dual-signed (client Ed25519 + hotkey sr25519) association a
  top-level miner publishes (commitments pallet + contract anchor) binding its fleet's `client_id`s to its
  UID, so validators count its routable IPs (§11.4).
- **θ (head share)** — the governed fraction of the 41% miner emission steered to top-level miners; `1−θ`
  goes to the pools (§8.5).
- **Validator** (was "verifier") — an **independent** Bittensor validator UID: stakes α, runs
  `VALIDATOR.md` trails, scores pools, and earns **native dividends** — its **only** reward (a validator
  effort bounty is out of scope, §9.3/D29). No NO, no pool — the disinterested consensus baseline; the
  owner is the majority validator early (§9.2).
- **Deposit / conviction stake / buyback reserve** — an NO's deposit (∝ usage) is locked in full to the
  **reserve hotkey** (never distributed, dividend‑compounding, §7.4); its cumulative locked α = its
  **conviction**, which sets its **tier → deposit rate** (§7.3). The contract keeps no deposit ledger —
  the `Deposited` events are the record (§7.5, D25).
- **implied usage** — `deposit_n / rate(tier_n)`: the demand signal validators weight for the pool tier
  (§8.1); staking for a lower rate keeps weight tracking real usage rather than raw α.
- **Effort bounty** — a fee‑funded reward that *would* pay ∝ verified, coverage‑weighted, server‑assigned
  completed trails; **out of v1 scope** and **not a committed phase** — a parked future‑iteration candidate
  (§9.3, §13.6, D29).
- **ST contract** — the subnet‑token (α) EVM contract: ledger + **custodian of miner emission and the
  buyback reserve** + settlement. **Not** the validator (§9–§10).
- **`D_n` / `Q_n` / `ŵ_n`** — NO's epoch deposit total (from the event log) / its consensus‑measured pool
  quality / its resulting validator‑computed weight (∝ `implied_usage_n × Q_n`, §8.1).
- **Validated path** — a completed `VALIDATOR.md` trail proof, id `keccak256(trail_id‖vpk‖server_key_id)`;
  self‑proving (NO `FINAL` + validator `vpk` sigs), verified via `0x402`.
- **Epoch (7 d)** — application settlement period; **tempo (360 blk)** — chain weight/emission cadence.

---

*End of WHITEPAPER.md v0.5 — validator effort bounty removed from scope (v1 = native dividends only; the
`(X)`/`(Y)` bounty designs are parked, not a committed phase; §9, §13.6; decision D29 in
`WHITEPAPER_DISCUSS.md`) — layered on v0.4 conviction staking + validator-computed weights + IP-breadth head
(D25–D28), v0.3 deposits-as-buybacks (locked, dividend-compounding reserve; miner pay = emission-only; §7.4,
§12.4; D23), and the v0.2 two-tier miner side (pool on-ramp + direct top-level miners; §8.4–8.5, §10, §11.4,
§14; D16–D20). This document fixes the architecture and the formulas; the next artifacts are the contract
source, the chain‑config script, and the operator/validator reference daemons (§16).*
