# UR Subnet — Whitepaper

**A Bittensor subnet for a decentralized privacy network.**

Version 2 (launch specification with settlement v2). Target chain: Bittensor / Subtensor — **testnet first, then mainnet**
(D28). The mainnet subnet is **SN25 (netuid 25)**; the testnet campaign ran on netuid 521 and closed on
2026‑09‑28. Version 1.0
promotes the completed v0.5 design to the normative release specification without changing its scope;
version 2 changes only the pool tier's payout settlement, on mainnet from the switch epoch **E_s** (below).

> **v2 — usage‑proportional, cumulative pool settlement (`VERSION2-FINAL.md`).** From the switch epoch
> **E_s** (the first epoch starting at least two days after the last activation gate passes), an operator
> pool pays its providers **in proportion to verified usage only** — no per‑provider reliability factor,
> no validator‑measurement gate. Each payout coldkey has **one cumulative account per pool that never
> expires**; each epoch's ledger is committed as a position‑binding **Merkle sum tree** over accounts at
> fixed positions; a root becomes claimable only after a quorum of **owner‑admitted auditors** (the
> validator alone, at launch) approves it **and** a **14,400‑block** (≈ 2‑day) window open to challenges
> and path demands passes; claims are **cumulative and permissionless** and always pay the account's
> coldkey. Two new contracts — immutable **`STSettlementVault2`** ("vault2") and owner‑upgradeable
> **`STSettlement2`** — sit beside the **unchanged** v1 contracts; protocol policy v1 is unchanged, its
> claim‑TTL and bps fields inert for v2 epochs. Release 1.0's per‑epoch bps roots, with their claim
> expiry, remain in force only for epochs before E_s. Validators still weight pools by
> `implied_usage × quality`; validator dividends and head fleets are unchanged. Capacity target and
> activation gate: **≥ 1,000,000 providers per operator pool**. See §1, §6, §8.2–8.3, §11.

> **v1.0 — validator effort bounty removed from scope (D29).** The fee‑funded validator effort bounty is
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
> the pools themselves** by `implied_usage × quality` (`implied_usage` = audited usage at the baseline rate) from
> published data. The **head tier ranks fleets by split-adjusted distinct routable egress-IP count** (not
> traffic; shared IPs split among claimants), head weight ∝ that score's EMA. Validators run measurement with
> **guardrails off** (own rate). See §7, §8, §10, §12; prior layers: v0.3 buybacks (D23), v0.2 two tiers
> (D16–D20).

---

## Executive summary

The **UR Subnet** (Bittensor **SN25**, netuid 25) runs a **decentralized privacy/VPN network** entirely through on‑chain incentives.
**Network Operators (NOs)** run the servers; independent **providers** carry ingress/egress traffic; and
independent **validators** run the `VALIDATOR.md` cryptographic routing‑verification protocol — walking
server‑assigned chains of providers to prove real‑time transit and measure **which providers are the
weakest links**. That measurement is the core: each tempo, **validators weight every NO's pool
themselves** — `implied_usage × quality`, where implied usage is the NO's audited usage priced at
the conviction‑zero rate (§8.1) — and Bittensor's **Yuma Consensus** turns those scores into the miners'
emission. As in canonical Bittensor, **validators' evaluation drives the payout** — the deposit
(α a NO locks, a costly, revenue‑backed signal of real demand) anchors the split, and the measured
`quality` (`Q_n`, from the trails) is the earned modulator. The **contract computes no weight** (D25); it
custodies and settles only.

Because one NO may serve **100k+ providers — far beyond a subnet's ~256 UID cap** — each NO is a single
**pool UID**, and its providers are paid *inside* the pool by **Merkle claim**. A smart contract on the
**Subtensor EVM** (the **ST contract**, denominated in the subnet's **α** token) is the ledger, the
emission custodian, and the **7‑day settlement** engine: providers **claim their α directly from the
contract with cryptographic proofs**, so a NO *reports* the usage its pool's rewards follow but **never
holds anyone else's funds**. Under **settlement v2** (from the switch epoch **E_s**) every eligible
provider's share, **in proportion to its verified usage**, is credited at any population size to its
payout coldkey's **cumulative account in that pool, which never expires**; one permissionless claim pays
everything the account accrued since its last (§1).
Validators earn Bittensor‑native **dividends** (∝ stake × scoring accuracy) —
**v1's only validator reward**; the fee‑funded effort bounty of earlier drafts is **out of scope** (D29), a
post‑launch open question, its spec parked in §9.3/§13.6 as a future‑iteration candidate.

**Deposits are conviction stake — never distributed.** The contract moves each deposit into a **buyback
reserve** — α staked on the owner's validator hotkey, where it compounds native dividends — and **no code
path re‑liquefies it**. So a deposit does two things: it **pays for the demand signal** validators weight,
the audited usage priced at the baseline rate (§8.1), and it **permanently removes α from liquid supply**, in
proportion to real usage. An NO's cumulative
locked α (its **conviction**) sets its **tier**, and the tier sets the deposit rate it must post —
**zero conviction = the baseline rate; more conviction = a lower rate**, so committed operators onboard
with less up‑front α (§7.3). Miners in both tiers are paid **from emission only** (§7.4, §12.4).

**Two miner tiers, in parallel.** The pool is the **on-ramp** — a place to start, with a **baseline
reward** and a low barrier (the provider needs no UID or registration burn; the vault owns one shared
pool UID per NO), weighted by `implied_usage × quality`. Above
it sits the **supply apex**: the **top ~200 fleets by distinct routable egress-IP count** ("**top-level
miners**") each claim their own miner UID and are **steered emission directly by validators on that
routable-IP breadth** — not on traffic volume; a fleet's score is its count of unique routable exit IPs,
**split when two fleets share an IP** — paid **natively** to their own hotkey, no contract custody, no
Merkle claim. A fleet is matched to its UID by a **signed `client_id`s → hotkey binding** validators read
(§11.4); its providers **graduate out of the pool into the head, and fall back if their breadth slips**,
with deregistration churn running that tournament. Both tiers share **one** mechanism's 256-UID metagraph
(§14); a governance split **θ** sets how much of the providers' share of the 41% miner emission flows to
the head vs. the pools (§8.4–8.5). On SN25 that share, the **momentum** `μ`, is 1/10 at launch, and the
native treasury multisig `ur‑reserve` receives the rest (§8.5). From epoch 8 (`E_c`) three fixed shares
replace θ: the pools take 4/10, the head 5/10 (the pools' while no fleet is weighted), and 1/10 funds the NOs'
**capture the flag** pools, split by their pools' weights: a per‑country auction that pays miners for being
present in every country (§8.5, §8.7).

The subnet **launches centralized‑but‑bounded** — an upgradeable policy coordinator owned by a dedicated
testnet key and, on mainnet, a Safe multisig (1-of-1 at the SN25 launch; a Safe transaction can add owners and
raise the threshold) — while the reserve and settlement vault are
**non-upgradeable from day one**. Finalized claims are therefore un-clawbackable even by a hostile
coordinator upgrade. Settlement v2 keeps that split — an immutable vault (**vault2**) beside an
owner‑upgradeable coordinator (**`STSettlement2`**): vault2 keeps every finalized root, never stops claims,
and lets an account's accrued balance survive even a hostile `STSettlement2` upgrade, as long as someone
challenges the bad root, or demands that account's path, within 14,400 blocks of its commit. Governance
later adds a timelock around coordinator changes. v1 rewards independently *measured liveness*;
closing the gap to honest‑relay, payout‑grade verification is the `VALIDATOR.md` §10 roadmap.

**At a glance**
- **Bittensor‑aligned:** validators evaluate miners (pools) → Yuma Consensus → emission; standard
  **18% owner / 41% miner / 41% validator** α split.
- **Real‑usage anchored:** the audited usage that a costly, revenue‑backed α **deposit** must pay for
  weights the cross‑operator split (validators compute `implied_usage × quality` off published data — the
  contract weighs nothing, D25);
  the network's unit of account is the subnet's **α**.
- **Deposits are conviction stake:** every deposit is locked into a contract **buyback reserve**, never
  paid back out; its cumulative amount sets the NO's **tier → deposit rate** (staking lowers the rate —
  onboarding + alignment); miners are paid from **emission only** (§7.3, §7.4, §12.4).
- **Scales to 1M+ providers per pool:** pool UIDs + an off‑chain ledger committed on chain as a **Merkle
  sum tree** of **cumulative, never‑expiring accounts** (settlement v2, whose design target and activation
  gate is ≥ 1,000,000 providers per operator pool). Providers inside a pool are *not* UIDs; validators
  remain independent native UIDs.
- **Two miner tiers:** a per-NO **pool** (on-ramp — `implied_usage × quality`, Merkle claim) *and*
  **top-level miners** (the top ~200 **fleets by distinct routable egress-IP count**, split-adjusted, as
  direct native UIDs), split by a governed share **θ** (§8.4–8.5).
- **Trust‑minimized custody:** no operator holds others' emission; every pool provider is paid by direct
  on‑chain claim against a committed Merkle root. Under settlement v2 a root is claimable only after a quorum of
  owner‑admitted auditors (at launch, the validator) approves it **and** a 14,400‑block window open to
  anyone's challenge or path demand passes; claims are permissionless and always pay the account's coldkey.
- **Validator data:** native dividends reward consensus‑accurate scoring; at launch the owner is the
  majority validator (by α holdings + the reserve) and runs the trail volume itself. A coverage‑weighted
  **effort bounty** is **out of v1 scope** (D29) — a possible future iteration, not a committed phase.
- **Progressive decentralization:** multisig + upgradeable coordinator with a pause‑only guardian →
  timelock → broader governance.

---

## 0. Reading guide

This document specifies, in implementable detail, a Bittensor subnet that coordinates a
decentralized privacy/VPN network through on‑chain incentives. It is the synthesis of four
inputs:

- **`seed/INCENTIVES.md`** — the economic intent (network operators, miners, validators; deposit‑weighted
  payouts; the validator prisoner's dilemma; the 7‑day block).
- **`VALIDATOR.md`** — the off‑chain routing‑verification protocol (`/verify`) that produces the
  signed **validated paths** this subnet pays for.
- **Current Bittensor mechanics** (Yuma Consensus, dTAO, the Subtensor EVM, the commitments pallet,
  the precompiles) as release-pinned at RaoFoundation/subtensor **v454**
  (`14cde6410fe8ec81a940e290c56f94a632a0988d`); settlement v2 targets the runtime chain 964 runs, **478**
  (`850f2755`) since 2026‑10‑13, checked against `docs/spec/runtime-475-audit.md`, `runtime-477-audit.md`
  and `runtime-478-audit.md` and rehearsed on nodes built from each of those sources; any mainnet runtime
  change repeats the rehearsal before activation.
- **`VERSION2-FINAL.md`** — the plan of record for **settlement v2**, the pool tier's usage‑proportional,
  cumulative‑account settlement from the switch epoch **E_s**: its contracts, ledger, audit, migration,
  activation gates and tests.

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
   them) score the pools `implied_usage × measured‑quality`, where implied usage is
   the audited usage priced at the conviction‑zero rate — so **validators' evaluation drives the miner
   emission, the Bittensor way** — and Yuma's median/clipping/vtrust/bonds do real work. (Validators earn
   **native dividends only**; a fee‑funded **effort bounty** for third‑party trail volume is **out of scope**
   — the owner is the majority validator at launch and runs the trails itself, §9.2; the bounty is a parked
   future‑iteration candidate, §9.3, D29.)
4. **Two clocks.** The chain's native **tempo** (~360 blocks ≈ 72 min) drives weight‑setting and
   emission; a **7‑day epoch** (≈ 50 400 blocks) is the application‑layer accounting/settlement
   period.
5. **Two miner tiers in one mechanism.** Alongside the per-NO pools (the on-ramp), the **top ~200
   fleets hold their own miner UIDs** and are steered **directly** by validators on split-adjusted
   distinct routable egress-prefix breadth, paid **natively** (§8.4). Both tiers live in **one** mechanism — a second mechanism would
   halve the 256-UID space below 200 (§14) — and a governance split **θ** divides the providers' share of
   the 41% miner emission between the head and the pools (§8.5). On SN25 that share, the momentum `μ`, is
   1/10 at launch; the native treasury multisig `ur‑reserve` receives the rest.

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
   post (§7.3). The coordinator keeps only cap/audit counters; canonical `Deposit` and
   `ConvictionAdded` events are the public weighting record (§7.5, D25).

2. **Emission (Yuma consensus over NO pools).** Every NO has **one contract‑custodied miner‑pool UID**
   (all its providers). **Independent validators** — anyone who stakes α and runs `VALIDATOR.md` trails —
   compute **every** NO's pool score **themselves** as `implied_usage × measured‑quality` (implied usage
   = the NO's audited usage priced at the conviction‑zero rate, §8.1) and submit those weights, so the **validators' evaluation
   drives the miner emission — the Bittensor way**. With many independent validators, **Yuma Consensus
   does real work**: stake‑weighted median + **clipping**, so no NO can inflate its own pool or knife a
   rival. Pool emission lands on miner‑pool UIDs **owned by the immutable vault coldkey** — no
   emission ever touches a NO's keys. **The providers' share of the 41% splits across two tiers**
   (§8.4–8.5): a governance share **θ** to **top-level miners** (the top ~200 fleets by **split-adjusted
   distinct routable egress-IP count**, as their own native UIDs), and `1−θ` to the NO pools above. On SN25
   that share, the **momentum** `μ`, is 1/10 at launch: the signed treasury policy has validators weight
   the other 9/10 to two recipient UIDs of `ur‑reserve`, a native treasury multisig separate from the
   buyback reserve (§8.5). From `E_c` (epoch 8) a tenth of the providers' share instead funds the NOs'
   **capture the flag** pools, each NO's second vault‑owned UID, split by their pools' weights, and the pools
   and the head share the rest 4 : 5 (§8.5, §8.7). Validator emission (41%) flows **natively** to the independent validators
   ∝ stake × vtrust.

3. **Settlement (contract).** Over a 7‑day epoch the contract accrues the **captured miner emission**,
   then distributes it. A NO's **miner pool** — its UID's earned emission, nothing else (deposits are
   reserved, never distributed, §7.4) — goes to its providers **in proportion to verified usage only**
   (no per‑provider reliability factor, no validator‑measurement gate). Since the **100k+ providers are not
   on‑chain UIDs** (the subnet cap is ~256) they are paid *inside* the pool by **Merkle claim**: each payout
   coldkey has **one cumulative account per pool that never expires**, the NO commits each epoch's ledger as
   a position‑binding **Merkle sum tree** over those accounts, and one **permissionless** claim with an
   O(log N) proof pays everything an account accrued since its last claim, always to its coldkey. A root is
   claimable only once a quorum of **owner‑admitted auditors** (at launch, the validator alone) approves it
   **and** 14,400 blocks pass after its commit, in which anyone may **challenge** a root that lowers,
   replaces or hides an existing account (deleting it) or **demand** the on‑chain opening of any existing
   account's path (blocking finalization until answered). The NO builds the ledger from usage and wallet
   records it attests, as in release 1.0 (the auditor re‑checks everything derived from them), but never
   holds the α.
   This is **settlement v2** (vault2 + `STSettlement2`, §6); epochs before the switch epoch **E_s** keep
   release 1.0's per‑epoch bps roots and claim expiry (§8.3).

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
consensus), consensus **clipping + vtrust** (a validator can't inflate a pool) + the **self‑weight mask**
(a validator scores neither its own UID nor a NO it declares it controls; the SN25 launch's one validator
declares none, §10), and **bonds / Liquid Alpha** (rewarding validators who back good pools early). The
**deposit**, which must pay for the audited usage that implied usage prices, is the *objective*
anchor that ties the across‑NO split to revenue‑backed
demand (§7–§8.1); **quality** is the Yuma‑measured factor that makes validators' evaluation the thing that
moves the money (§10). At bootstrap, when `Q_n` is still noisy, governance **caps the quality swing** and
widens it as the validator set and data mature (§12.3).

```
                     INDEPENDENT VALIDATORS  (stake α, run /verify trails)
         each tempo → score BOTH tiers → commit-reveal → Yuma (stake-median + clip + vtrust)
             pools (tail):  weight = implied_usage_n × quality Q_n   (usage at baseline rate)
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
          └────────── start in a pool ─▶ graduate to a top slot ─▶ fall back if breadth slips ──┘

 Bittensor coinbase: 18% owner · 41% miner (Yuma; providers' share μ split θ / 1−θ above) · 41% validator (NATIVE ∝ stake×vtrust)
 SN25 launch: μ = 1/10 of the 41%; the validators' row gives the other 9/10 to ur-reserve's two recipient UIDs (§8.5)
 buyback reserve: ΣD staked to the owner-validator hotkey — locked, dividend-compounding, never distributed (§7.4)
```

---

## 2. Background: the Bittensor primitives this design uses

A condensed, current (dTAO‑era) reference. Identifiers are from `opentensor/subtensor` `main`.

### 2.1 Subnet, neurons, UIDs

- A **subnet** (identified by `netuid`) is registered permissionlessly with `register_network`; the
  registrant becomes the **owner** (an owner coldkey + an owner hotkey auto‑registered at **UID 0**;
  pruning skips every UID whose hotkey the owner coldkey owns, which on SN25 is UID 1). Creation pays a
  dynamic TAO lock that **seeds the subnet's liquidity pool**.
- A **neuron** is a `(coldkey, hotkey)` holding a **UID** (slot). Default capacity
  `max_allowed_uids = 256`. A hotkey **registers** by paying a recycled **burn**. Release automation
  uses `register_limit`/`registerLimit` so the runtime rejects a price above the signed per-action
  ceiling; when slots are full the lowest‑emission non‑immune neuron is deregistered.
- A neuron is **not** intrinsically "miner" or "validator." It earns **incentive** (miner reward) if
  validators weight it, and **dividends** (validator reward) if it holds a **validator permit** and
  sets weights. Roles are emergent from stake + permit + behavior.

### 2.2 Weights, Yuma Consensus, emission

- Each **tempo** (default **360 blocks**, ≈ 72 min; per‑subnet, range `[360, 50 400]`), the chain runs
  an **epoch** = one Yuma Consensus pass that converts the validator **weight matrix** + **stake** into
  two emission streams.
- **Validators set weights** with `set_weights(netuid, dests, weights, version_key)` (`u16` values,
  rescaled so the max entry is `65535`). Only neurons with a **validator permit** (top‑k by stake,
  `max_allowed_validators`: root‑set, runtime default 128, 64 on testnet and SN25) and stake ≥ a small
  threshold have weight influence; others'
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
- **Stake weight** = `alpha_stake + tao_weight × tao_stake`. `tao_weight` is global, root-governed,
  and encoded as `u64 / u64::MAX`; testnet currently resolves to **0.018** (1.8%), while the
  v452-origin genesis fallback retained by v454 is about 5.27%. Deployments compatibility-gate the
  finalized live value and observe mainnet independently rather than assuming either value.

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
  `transferStake`. Two mechanics matter for §7.4: stake carries **no unbonding period** —
  `removeStake`/`transferStake`/`moveStake` act immediately for the owning coldkey on any part of a
  position not held by a conviction lock or by registration collateral (§6.3) — so any
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
- **Runtime precompiles used by the release contracts and conformance harness** (address → purpose):

  | Address | Name | What the contract calls it for |
  |---|---|---|
  | `0x…080c` | **AddressMapping** | the conformance probe's check of the exact H160 → AccountId32 mirror (runtime 455 routes `0x…0009` to BN128 addition, not Blake2f); each release contract takes its own mapped coldkey as a constructor immutable |
  | `0x…0402` | **Ed25519Verify** | verify client consent for fleet binding and revocation |
  | `0x…0403` | **Sr25519Verify** | verify the registered Bittensor hotkey's fleet-binding signature |
  | `0x…0804` | **Neuron** | limit-register vault-owned pool hotkeys and prove live `(netuid, hotkey) → UID` bindings |
  | `0x…0805` | **Staking V2** | exact `getStake`, `moveStake`, and `transferStake` operations for isolated deposits, immutable reserve custody, emission capture, and claims |
  | `0x…0802` | **Metagraph** | read-only conformance/observation of UID 0 and metagraph identity; production settlement does not trust a point-in-time emission getter |
  | `0x…0808` | **Alpha** | the release 1.0 vault's spot α price (`getAlphaPrice`), for the TAO‑equivalent transfer floor of captures and claim payments |

  Independent validators submit CRv4 through native Substrate extrinsics, not through an EVM
  precompile. The v452 commitments API surface retained by runtime 454 exposes no stable EVM getter for
  the commitments pallet, so a narrowly scoped finalized indexer mirrors `(hotkey, commitment hash,
  finalized block/hash)` into the coordinator; the coordinator then checks the mirror, both signatures,
  freshness and the live UID atomically.

  Neuron registration burns from the EVM caller's funded SS58 mirror. A contract receives/funds that
  native balance and calls `registerLimit` with **zero precompile call value**; forwarding the burn to
  `0x…0804` would move it away before the runtime dispatch charges the caller. Automated contract
  registration supplies the full approved ceiling and atomically returns the difference after the
  runtime burns the actual price.

  The version 2 vault, vault2 (§3), depends on Neuron, to register its escrow and pool hotkeys (and to let
  anyone re-register a pruned pool hotkey), and on Staking V2, for capture, claims and `absorbStake`,
  including runtime 475's `getMinerCollateral` view on the same address, so that it moves and counts only
  stake that registration collateral does not lock (§6.3). It does not depend on Ed25519Verify,
  Sr25519Verify or the alpha-price precompile (`0x…0808`): it accepts no signed
  permits, root admission uses auditor approvals sent as ordinary transactions from owner-admitted
  addresses, and a claim that the runtime's transfer floor refuses simply reverts and stays claimable.
  Because vault2 is immutable, it depends for good on the ABIs of `getStake`, `getMinerCollateral`,
  `moveStake` and `transferStake`: a runtime that changes one of them freezes every capture and claim
  (`VERSION2-FINAL.md` §17).

  > Precompile ABIs are **not formally versioned** (issue #2455). Pin a Subtensor release tag, target
  > **Staking V2** (`0x805`, not the legacy `0x801`), and re‑verify addresses/ABIs before launch.

### 2.6 Release runtime compatibility pin

Release 1.0 started pinned to the deployed testnet **runtime spec 454**, transaction version 1, source tag
`v454`, source commit `14cde6410fe8ec81a940e290c56f94a632a0988d`, and finalized on-chain Wasm
storage hash `0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef`. The
identity first appeared at finalized testnet block 7,934,387
(`0x5b3f3455125d78812299002a1926792a6876b03ac636ae53e93e4115f15a392b`); the release asset's
published SHA-256 is `0xa55e76b4f4620bcdb4c787e499c87a35abb9913ba4cde001b08a00d1945ac4db`.
The exact SCALE metadata bytes at that same finalized hash are pinned by BLAKE2b-256
`0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702`.
The official compressed Wasm is 2,515,968 bytes; independently hashing those exact bytes yielded both
the published SHA-256 and the finalized chain's BLAKE2-256 `System.Code` hash, establishing artifact-to-live
byte identity.
The upstream release proposal separately binds call hash
`0x5a1c30f0387796da59522d4b84a71395533a4ee676e06c52eedb14262ae9c3c6` at finney
multisig timepoint `8996567:7`; those two proposal fields authenticate the official source release and
are **not** evidence of its testnet inclusion. Testnet inclusion is proved independently by its finalized
spec/transaction/state versions, `System.Code` hash and exact metadata digest at the testnet block above.
Doctor reads the complete runtime version, code hash and metadata at one explicit finalized hash per endpoint.
Every operational native current-state/signing boundary then re-checks the complete version and code hash at
its explicit finalized block and selects only metadata whose exact SCALE bytes were already hash-authenticated
on that independently dialed connection for the same complete reviewed artifact tuple. This content-addressed
reuse is limited to the reviewed testnet identities (v451–v455, v458–v461 and v467, `crv4/reviewed_runtime.go`).
A mandatory release gate resolves their pinned testnet blocks, authenticates
the on-chain `System.Code` hashes, obtains the exact chain or official-release Wasm bytes, and executes
`Core_version` plus `Metadata_metadata` in a pinned Wasm executor exposing only allocator, logging and hashing
host functions. Any attempted storage/offchain host call is a hard failure. The gate requires the exact
version tuple, code size/SHA-256/BLAKE2b-256, metadata size/SHA-256/BLAKE2b-256, complete SCALE decode and
FRAME metadata v14 recorded in `docs/spec/runtime-metadata-artifacts.json`. Pinned upstream metadata tests
without `TestExternalities` independently corroborate the result. Caches are per provider, singleflighted,
failure-free and bounded to 24 decoded entries, evicting the least recently used; historical identities remain
read-only and cannot satisfy the current signing boundary.
Secretless semantic replay intentionally downloads and records all three raw responses at every evidence
checkpoint from its signed v454 campaign start onward; carried pre-campaign receipts retain their exact
v451/v452/v453 identities instead of being relabeled. The continuous sentinel repeats complete version and
code-hash checks on both endpoints.
The reviewed testnet artifact later moved through 455, 458–461 and 467 (`docs/spec/runtime-<n>-audit.md`)
until the campaign closed on 2026‑09‑28. The SN25 mainnet launch on 2026‑10‑08 pinned mainnet runtime 475
(source `d1718c99`, code `0x557634c8…d3a0`, metadata `0x983cfdab…95ff`), and its validator admits a strictly
higher successor of that artifact whose consumed interfaces equal the signed
`urnetwork-validator-producer-interface-v1` profile (`mainnet/VALIDATOR-PRODUCTION-RUNTIME.md`); mainnet has
since run 477 and, from 2026‑10‑13, 478.

The v452→v453 audit is one upstream release commit and adds five security/economic boundaries. The release
gate hashes all 12 changed upstream Rust source/test files at the pinned commit; Terra's clean pinned-source
run passed all ten focused Rust regressions with none failed or ignored. Deterministic local decision models
supplement those results but do not execute FRAME,
while shared-testnet actors remain read-only for chain-wide attacks:

1. A drand pulse's public randomness must equal SHA-256 of its authenticated signature; a valid pairing
   cannot authenticate a separately substituted randomness field.
2. Direct and announced nested proxies intersect the inherited outer call filter with the inner proxy
   filter before the final effect.
3. Balance-transfer precompile `0x800` adds an explicit true-caller coldkey-swap check to payable
   `transfer(bytes32)` before its internal-account dispatch. The adjacent unchanged `transferKeepAlive`
   path already dispatches as the true caller through the centralized runtime filter.
4. User stake transitions reject the protocol beta-basket escrow as a destination, preventing holdings
   without matching basket shares.
5. A deferred subnet registration reserves its hotkey and consumes last-lock/rate-limit state when it is
   queued; later materialization does not price it a second time.

The diff does not change the commitments registration encoding, the CRv4 call shape, or the release's
Neuron/Staking/signature-precompile selectors. Historical names and observations below which say
"runtime 452" identify the wire format or behavior originally audited there; they remain intentionally
preserved where v454 is byte- or semantics-compatible. The exact v453 evidence and upstream comparison
are recorded in `docs/spec/runtime-v453-audit.md`.

The v453→v454 audit adds five adjacent custody and authorization boundaries while retaining the v453
protections above:

1. The pallet-contracts filter restores only the inherited `transfer_stake` call behind an exact
   `Proxy::proxy` envelope; the user's explicit proxy delegation remains required and sibling calls stay denied.
2. A coldkey-wide root claim selects only root-staked or negative-watermark hotkeys, independently bounds the
   relationship scan, counts selected hotkeys plus every raw Alpha/AlphaV2 row within 256 work units, and declares
   both the full-claim and scan weight.
3. A positive marked non-root entitlement whose proportional alpha take floors to zero sells one atomic alpha
   and caps a nonfinal claimant's payout to the marked entitlement instead of burning shares for no payout.
4. A fully withdrawn share worth less than one rao is canonicalized to zero and removed from the denominator so
   it cannot revive after later emissions.
5. Storage GC includes zero AlphaV2 rows and starts dependent relationship cleanup only after a positive,
   versioned completion marker; a missing cursor is not treated as completion.

The exact source, artifact, upstream-test and live-conformance requirements are recorded in
`docs/spec/runtime-v454-audit.md`. The concurrent custody actor executes bounded decision models for all five
groups while the runtime sentinel independently pins the deployed version and Wasm. These models do not replace
the pinned FRAME tests or the disposable live contract/precompile battery.

Version 2 settlement is pinned separately, to the mainnet (chain 964) runtime it deploys on: runtime 478,
source commit `850f2755bb05a3851f948489f0ab27b0645cebce`, live since block 9,276,925 (2026‑10‑13). The
runtime facts vault2 and `STSettlement2` (§3) rely on come from the local v451–v454 sources and the sources of
mainnet runtimes 475, 477 and 478, checked against `docs/spec/runtime-475-audit.md`, `runtime-477-audit.md`
and `runtime-478-audit.md`, and were measured on nodes built from them (`scripts/runtime-rehearsal/`,
measurements in its `README.md`, whose runtime 478 section supersedes the earlier gas limits). Before
deployment their exact bytecode must pass a rehearsal on such a node (R-1 to R-6, `VERSION2-FINAL.md` §15.8,
driven by the Go harness in `rehearsal/`); it has passed on 475, and with the claim batcher's R-7 on 477 and
478, and any mainnet runtime change repeats it.

---

## 3. Roles and on‑chain identity

| Design role (`seed/INCENTIVES.md`) | On‑chain identity | Notes |
|---|---|---|
| **Subnet owner** (BringYour, Inc.) | owner coldkey (on SN25 `ur‑owner`, a native multisig) + owner hotkey, which pruning skips (**UID 1** on SN25, its sole launch validator); deploys & governs the ST contract; operates the **reserve validator hotkey** (§7.4) | receives the 18% owner cut; the **majority validator** in the early network (§9.2); the governance **referee** (§9.6). |
| **Network Operator (NO)** | a contract registration (`noId`) with **one miner‑pool UID** (its providers, contract‑owned; from `E_s` a new pool UID owned by vault2). Holds **no emission**; runs no validator. | a per‑NO *pool operator*: deposits; runs the `/verify` server (co‑signs trails); commits the Merkle payout root that splits its miner pool — from `E_s` a sum-tree root over per-coldkey accounts, split by verified usage alone, whose ledger it publishes and whose paths it opens on demand. Directs flow before `E_s` (from `E_s` it attests only the usage and wallet records, §8.2); the contract holds and pays. |
| **Provider (miner)** | a `client_id` **inside** a NO's miner pool — **not a UID** (100k+ providers can't each be a UID); from `E_s` its earnings accrue to its payout coldkey's one **account** in that pool | carries traffic; **claims its α directly from the contract** with a Merkle proof against its NO's payout root: for epochs before `E_s`, per epoch and before its expiry at the end of epoch `e+9` (TTL 8 + grace 1); from `E_s`, cumulatively and without expiry — one claim, which anyone may send and which always pays the account's coldkey, collects all accrual since the last. The **on-ramp / tail** tier; can **graduate** to a top-level slot (§8.4). |
| **Top-level miner (head)** | **its own miner UID**; a **fleet** — its `client_id`s **bound to its hotkey** (§11.4) | the **supply apex**: the top ~200 fleets by **split-adjusted distinct routable egress-IP count**, steered **directly** by validators (weight = an EMA of the score); **native** emission to its own coldkey — no contract custody, no Merkle claim; maintained by deregistration churn (§8.4). |
| **Validator** (was "verifier") | an **independent** Bittensor validator UID; stakes its **own** α | runs `/verify` trails (the failure‑data signal), scores pools `implied_usage × Q_n` under commit‑reveal (§10). Earns **native dividends** (∝ stake × vtrust) — its **only** reward; a validator effort bounty is **out of scope** (D29, parked in §9.3). No NO, no pool. |
| **Auditor** (version 2) | an owner-admitted EVM address in `STSettlement2`'s auditor set (at most 16, with a quorum), not a hotkey or `vpk` binding; at launch a single address whose key the validator holds, quorum 1 | approves the exact pending v2 root before the next epoch ends. A root finalizes only with a quorum of approvals, and never while vault2 blocks it (inside its challenge window or with a demand open); challenges and path demands are open to anyone (§5.2). |
| **ST contract set** | three distinct mapped coldkeys: coordinator, immutable settlement vault, immutable reserve sink, beside an immutable evidence journal (`STValidatorEvidence`) that holds no stake. Version 2 adds the immutable `STSettlementVault2` ("vault2") and the owner-upgradeable `STSettlement2` beside them, leaving every v1 contract unchanged | the vault owns pool UIDs and claims escrow; the reserve sink owns permanently locked conviction; the coordinator owns neither and, beyond operators' staged deposits on their isolated deposit hotkeys (§6.3), only applies policy. vault2 owns each v2 pool UID (from `E_s`) and its own claims escrow; `STSettlement2` owns neither, holds the auditor set and approvals, and reads operators, windows and epoch boundaries from the coordinator. Does **not** custody validator emission (it is native). |

**Why pools (miner side only).** A NO may have 100k+ providers (settlement v2 targets ≥ 1,000,000 per pool) —
they cannot be UIDs (subnet cap ≈ 256),
so each NO gets **one miner‑pool UID** and its providers are paid *inside* it by Merkle claim. That
miner‑pool UID is **owned outright by the immutable vault's coldkey** (a pure accrual slot), so the NO never
holds the emission destined for its providers. **Validators are not pooled** — they are independent
Bittensor validator UIDs (own hotkey, own stake, native dividends), which is both simpler and the
**independence** the measurement needs (§9.5). This removes the per‑NO validator pool, the take‑0 custody
binding, the intersection split, and the VT of earlier drafts (§13.6).

**Why also a head tier (top-level miners).** Pools solve *scale* (100k+ providers), but inside a pool a
provider trusts its NO — in v1 for a discretionary payout split, from `E_s` for the usage records that alone
set the split — and is one of thousands. The **top ~200 fleets by
routable-IP breadth** therefore *also* get the canonical Bittensor treatment — **their own UID, steered
directly by validators, paid natively** (§8.4) — so the biggest routable-supply operators compete
head-to-head on breadth, trust-minimized, while the pool stays the low-barrier on-ramp everyone starts
in. A provider (`client_id`) is in **exactly one** tier at a time
(promoted out of its pool's payout list while its binding to a live head UID is active at either epoch
boundary — no double-pay, §8.4). The two tiers share one
256-UID metagraph (§14).

**Binding the validator identity.** `seed/INCENTIVES.md` says a validator "uses their wallet PK as their
validation path key," while `VALIDATOR.md` signs trails with an Ed25519 `vpk`. The bounty's on‑chain
binding — `registerValidator(vpk, sig)`, an Ed25519 signature checked via the `0x402` precompile — would make
submitted **completed‑trail proofs** attributable for an effort bounty, so it belongs to the **parked
bounty design** (§9.3, D29); v1 deploys no such registry. Release 1.0 binds a validator's hotkey and `vpk`
on chain only for its evidence journal: `STValidatorEvidence.publishActivation` checks both keys' consent
(`0x402`, `0x403`) and pays nothing (§11.1). (Reusing the BT wallet key directly as `vpk` also works but
couples key rotation to the wallet; binding is preferred.)

---

## 4. Notation

| Symbol | Meaning |
|---|---|
| `netuid` | the UR subnet id — 25 (SN25) on mainnet; 521 on the testnet campaign |
| `T_tempo` | tempo length in blocks (360) |
| `T_epoch` | UR settlement epoch in blocks (50 400 ≈ 7 days on mainnet from epoch 1, after a 7,200‑block epoch 0) |
| `e` | epoch index (monotone counter in the contract) |
| `E_s` | the **switch epoch**: a pool's first version 2 epoch, its `firstEpoch` as set by `registerOperatorPool`; earlier epochs settle on v1 (§5.2) |
| `D_n` | α deposited by NO `n` during epoch `e` (the coordinator's epoch deposit counter, which finalized `Deposit` events reproduce; audited and capped, not weighted, §7.5) |
| `conviction_n` | NO `n`'s cumulative locked α (all‑time deposits + voluntary stake) → its tier, taken as the conviction before the epoch (§7.2, §7.5) |
| `rate(tier_n)` | published α‑per‑usage rate for `n`'s conviction tier; zero tier = baseline (§7.3) |
| `implied_usage_n` | NO `n`'s audited usage priced at the zero tier's rate, truncated as its capped deposit is; 1 at zero price — the demand signal validators weight (§8.1) |
| `w_n` / `ŵ_n` | NO `n`'s pool weight ∝ `implied_usage_n × Q_n` (validator‑computed, §8.1/§10) |
| `E_mine, E_val, E_own` | α emission to the miner / validator / owner pools over the epoch (41/41/18% of subnet α emission) |
| `Q_n` | NO `n`'s consensus‑measured pool quality (from validators' `VALIDATOR.md` trails) |
| `score(u)` | top‑level miner (fleet) `u`'s **split‑adjusted count of distinct routable egress‑IPs** — its EMA **is** its head weight (§8.4) |
| `θ` | governance **head share**: fraction of the providers' share `μ` of the 41% miner emission steered to top-level miners; `1−θ` goes to the pools (§8.5) |
| `μ` | **momentum**: the providers' share (head + pools) of the native miner allocation — 1/10 on SN25 at launch, pinned by the signed treasury policy, which gives the other `1 − μ` to `ur‑reserve`'s recipient UIDs; 1 without a treasury policy (testnet) (§8.5) |
| `client_id`s ⇄ hotkey | the signed binding mapping a fleet's `client_id`s to a top-level miner's UID (§11.4) |
| `R` | the **buyback reserve**: cumulative locked deposits + voluntary conviction + their compounded dividends (§7.4) |
| `φ` / `ω` / `FeePool` / `effort_v` | effort‑bounty quantities — **out of scope** (parked, §9.3/§13.6, D29) |
| `s_{n,p}` | NO `n`'s payout share for provider `p` (Σ_p s_{n,p} = 1): before `E_s` from usage × reliability, paid as 10,000‑bps leaf shares; from `E_s` its verified‑usage share (§8.2) |
| `(coldkey_i, cumulative_i)` | account `i` of a pool's v2 ledger: a payout coldkey (one account per coldkey per pool) and its cumulative accrual in rao, which never decreases or expires; claimable = latest finalized cumulative − `claimed` |

---

## 5. Time: tempo vs. the 7‑day epoch

Two cadences run concurrently.

The values in this section are the **mainnet 1.0 cadence**. Testnet acceptance
was to use a deliberately shortened, future-effective 360-block UR epoch
(approximately 72 minutes), a 60-block root window, a 180-block finalize
offset, and a 6-block close grace, and to complete three consecutive fully
observed epochs; the campaign closed on 2026‑09‑28 with known exceptions before
that acceptance (`FINALIZE.md`). This test-only snapshot exercises the identical
state machine and must never be promoted as the mainnet policy, whose epoch is
50,400 blocks (approximately seven days) with separately reviewed +4h/+48h
windows from epoch 1, after a one-day epoch 0 (7,200 blocks, finalized 2,400
blocks in).
Version 2 settlement (§5.2) cannot run at the testnet cadence: vault2 fixes a
14,400-block challenge window, so `T_epoch` must be at least the root commit
window plus 14,400 blocks. Mainnet's 50,400 satisfies this. `STSettlement2`
checks the rule only when a pool is registered, for its first epoch's policy,
so every later v1 policy that covers a v2 epoch must keep it: a governance
constraint on the owner's policy schedule.

### 5.1 Tempo cadence (chain‑native; weights + emission)

Each tempo (~72 min) the chain runs Yuma and drains α emission. Weight‑setting is **decentralized across
independent validators** — the change that makes Yuma do real work. Each tempo every validator (§9)
scores **every NO pool** from its own `VALIDATOR.md` trails and submits it under commit‑reveal:

```
each tempo, every independent validator v:
    for each NO pool p:  score[p] = implied_usage_p × quality_v(p)    // audited usage at the baseline rate (§8.1)
    normalize to u16; submit native CRv4 commit extrinsic            // reveal auto-fires later (§10)
```

The chain takes the **stake‑weighted median** of the validators' scores, **clips** outliers to
consensus, and emits to the miner‑pool UIDs ∝ the clipped result (§10) — so the miners' reward *is* the
validators' evaluation, the Bittensor way. A validator earns a permit by stake
(top‑`max_allowed_validators`) from its **own** α; honest, consensus‑aligned scoring builds vtrust and
dividends while divergence is clipped. There is **no off‑chain keeper or trusted weight authority**;
each validator runs the open UR validator (`validator/`) and submits its own native CRv4 weights.

> **Commit‑reveal is enabled** (`commit_reveal_weights_enabled = true`). Weights carry a *subjective*
> quality signal, so a lazy validator could copy others' fresh weights; commit‑reveal (drand timelock,
> §2.4) hides each validator's scores until they are stale, so copying earns low vtrust — the standard
> Bittensor anti‑weight‑copying posture.

### 5.2 Epoch cadence (application‑layer; settlement)

The 7‑day epoch is the unit `seed/INCENTIVES.md` calls a "block." It is tracked by the contract as an
incrementing counter with the block length its policy snapshot sets (`T_epoch`); version 2 reads the same
counter, boundaries and
windows from the v1 coordinator, whose policy is unchanged. Epochs before a pool's switch epoch `E_s` keep
the v1 lifecycle (timeline from `README.md`):

```
  t = 0         Epoch e ends; D_n is its deposit total. Within the 120-block close grace anyone closes it
                (closeOperatorEpoch): the vault captures the miner-pool emission, and a missed close defers it.
  t ≤ +4h       Each NO must commit its payout-list root for e (the share tree).  [README: "data ... 4h after"]
  t < +48h      Audit window: committed roots and content-addressed payout artifacts are public and reproducible (§11.3).
  +48h          coordinator.finalizeOperatorEpoch(e,n): immutable per-NO entitlement opens in the vault.
                Claims stay open through the end of epoch e+9 (claim_ttl_epochs 8 + claim_grace_epochs 1);
                anyone may then expire the unclaimed remainder into the same NO's carry, which its next
                finalized entitlement adds.
                (The +24h effort-claim step + challenge window belong to the parked bounty design, §9.3/D29 — not in v1.)
```

From `E_s` (the pool's `firstEpoch`, set when the owner calls `registerOperatorPool`) each epoch follows the
version 2 lifecycle (`VERSION2-FINAL.md` §5), in blocks after the epoch's end block `end(e)`:

```
  ≤ +1,200 (≈4h)     STSettlement2.closeOperatorEpoch(e,n), by anyone: vault2 captures the pool hotkey's
                     movable stake, first deferring any earlier unfunded epoch. The v2 close window is this
                     root commit window, not v1's 120-block close grace.
  ≤ +1,200           After the close, the operator publishes e's ledger and its root signer commits the root
                     (commitDistribution): a Merkle sum tree over the pool's cumulative per-coldkey accounts,
                     covering every epoch since the last finalized root.
  < end(e+1)         Auditors approve the exact pending root (approveDistribution); at launch the quorum is the
                     validator alone.
  < commit + 14,400  Challenge window (≈48h from the commit, fixed in vault2): anyone may demand the path
                     opening of an account proven against any finalized root (demandPath, once per account of
                     each finalized root per pending root), which blocks finalization until the operator
                     answers. At any time while the root is pending, anyone may prove that it lowers, replaces
                     or hides an existing account (checkPath, which deletes the root).
  ≥ commit + 14,400  finalizeDistribution(n), by anyone, once a quorum has approved and no demand is open. Each
                     account's cumulative becomes claimable with no expiry; anyone may send the claim, which
                     always pays the account's coldkey.
  ≥ end(e+1)         A root still unfinalized can be replaced by the next commit, which covers its epochs too.
                     vault2 refuses any commit within 14,400 blocks of the pool's previous commit.
```

Emission weighting, dividend capture and the deposit audit happen continuously across the epoch at tempo
cadence (§7.1); only **settlement** waits for the epoch boundary + windows.

---

## 6. The ST contract

Release 1.0 is a three-contract custody system on the Subtensor EVM, beside an immutable, ownerless hash
journal (`STValidatorEvidence`) that holds no stake and is anchored once in the coordinator (§11.1). The split
is a custody boundary, not merely code organization:

1. **`STReserveSink` is non-upgradeable.** Its mapped coldkey owns all deposit and voluntary-conviction
   principal on one immutable reserve hotkey. It can record additional principal but contains no call,
   move, transfer, withdrawal, proxy or upgrade path that can source that stake.
2. **`STSettlementVault` is non-upgradeable.** Its mapped coldkey owns each NO pool UID and the escrow
   hotkey. It captures pool emission, fixes each entitlement once, pays permissionless direct Merkle
   claims, expires unclaimed amounts into the same NO's carry, and has no pause, sweep or upgrade path.
3. **`STCoordinator` is UUPS-upgradeable.** It contains admission, future-effective policy/operator
   versions, isolated deposit attribution, roots, finalized commitment mirrors and fleet bindings. Its
   address is fixed once as the sink's recorder and vault's coordinator. An upgrade can change policy for
   open/future work, but immutable vault state prevents it from rewriting or blocking an already-finalized
   entitlement, and immutable sink bytecode prevents it from moving reserve stake.

Version 2 adds two contracts beside this set and leaves the release 1.0 contracts unmodified. A pool's
epochs from its registered `firstEpoch` (the switch epoch E_s) settle through them; the release 1.0
vault keeps paying the epochs before E_s and the credits it has accepted (§6.3):

4. **`STSettlementVault2` ("vault2") is non-upgradeable.** Its mapped coldkey owns each v2 pool UID and
   its own escrow hotkey. It snapshots each epoch's funding once, holds at most one pending root per pool,
   keeps every finalized root, and pays each account its cumulative balance less what it has already been
   paid. It fixes the 14,400-block challenge window itself and has no owner, pause or upgrade path once its
   coordinator link is fixed, so its guarantees (`VERSION2-FINAL.md` §4.2) hold whatever `STSettlement2`
   does.
5. **`STSettlement2` is UUPS-upgradeable by the owner.** It reads operators, windows, epoch boundaries and
   the pause flag from the coordinator. It registers v2 pools, answers `settlementAt` (which vault and pool
   hotkey settle an epoch), closes epochs, takes the root signer's commits, counts approvals from an
   owner-set auditor quorum, and finalizes. It holds no funds: native value enters it only with a pool
   registration, whose burn the owner sends and it forwards to vault2, and as vault2's refund of the unused
   burn, which it passes back to the owner; `receive()` accepts value only from vault2. An upgrade can change
   how roots are admitted, but it cannot finalize a root that vault2 blocks, change a finalized root or stop
   a claim.

The contract set is **not** the subnet validator. Independent validators (§9–§10) compute and submit
native weights and receive native dividends. Only tail miner emission enters the settlement vaults; head
emission is native (§8.4), deposits enter only the reserve, and the effort bounty is out of scope (§9.3/D29).

### 6.1 State (essential)

Essential state is deliberately partitioned:

- The coordinator stores block-derived policy snapshots, versioned operator roles, per-NO nonces and
  caps, epoch demand totals, cumulative conviction, content-hashed payout-root commitments, finalized
  commitment mirrors, and versioned many-client fleet bindings. Its demand counters enforce caps and
  audit events; the contract never turns them into weights.
- The reserve sink stores only immutable identity, cumulative principal and per-NO principal audit
  totals. `liveStake() >= principal` is executable at every finalized block.
- The release 1.0 settlement vault stores immutable pool identities and one entitlement per `(epoch,noId)`:
  `payoutRoot`, `artifactHash`, captured funding, total (including same-NO carry), claimed amount,
  expiry block and state. It also stores an immutable finalized-runtime `minimumTransferTaoRao`,
  durable per-coldkey `claimCredit`, and the exact custody counters. Global
  `totalCaptured = totalPaid + escrowAccounted` and
  `escrowAccounted = pendingFunding + outstandingLiability` are executable conservation identities.
- vault2 stores its immutable identity and the fixed 14,400-block challenge window and, per pool, the pool
  hotkey and `firstEpoch`; one cumulative funding snapshot per epoch (`fundedThrough`), written once when
  the epoch is captured or deferred; every finalized root (`root`, `total`, `leafCount`, `epoch`,
  `ledgerHash`), append-only; at most one pending root, with the demand state on it of each account of each
  finalized root (none, open until answered, answered); and a `claimed` watermark per account. It has no
  per-epoch entitlement, expiry or claim credit. Its conservation identities are
  `totalCaptured = totalPaid + escrowAccounted` and `escrowAccounted = Σ_pools (capturedTotal − claimedTotal)`,
  with `claimedTotal <= finalizedTotal <= capturedTotal` per pool, and every call that moves or assigns stake
  ends by checking that the escrow's movable stake (§6.3) covers `escrowAccounted`.
- `STSettlement2` stores only its links to the coordinator and vault2, the auditor set (at most 16
  addresses) with its quorum and version, and per-pool approvals. v2 roots and funding live in vault2, not
  in the coordinator.

There is no on-chain tier/rate/weight computation and no validator-effort registry.

### 6.2 Interfaces (selected)

Selected release interfaces are:

```solidity
// Coordinator: scoped roles, exact nonce/deadline, and per-NO deposit hotkey.
registerOperator(noId, coldkey, poolHotkey, depositHotkey,
                 depositSigner, rootSigner, effectiveEpoch, maximumBurnRao)
deposit(noId, amount, nonce, deadlineBlock)
addConviction(noId, amount, nonce, deadlineBlock)
closeOperatorEpoch(epoch, noId)
deferMissedEmission(epoch, noId)
commitOperatorRoot(epoch, noId, payoutRoot, artifactHash)
finalizeOperatorEpoch(epoch, noId)

// Versioned commitment and binding authorization.
mirrorCommitment(hotkey, commitmentHash, finalizedBlock, finalizedBlockHash)
bindFleetMember(binding, clientEd25519Signature, hotkeySr25519Signature)
revokeFleetBinding(clientId, generation, effectiveEpoch, clientSignature)
cleanupFleetBinding(clientId)

// Immutable vault; claim has no coordinator/pause authorization check.
claim(epoch, noId, coldkey, shareBps, proof)
withdrawClaimCredit(coldkey)
expireEntitlement(epoch, noId)

// STSettlement2 (owner-upgradeable UUPS): a pool's epochs from its firstEpoch (E_s).
registerOperatorPool(noId, hotkey, maxBurn, firstEpoch)
setAuditors(auditors, quorum)
settlementAt(noId, epoch)            // view: (vault, pool hotkey, version)
closeOperatorEpoch(e, noId)
commitDistribution(e, noId, prevRootCount, (root, total, leafCount, ledgerHash))
approveDistribution(noId, seq, root, ledgerHash)
finalizeDistribution(noId)

// Immutable vault2: anyone may call these; none has a coordinator or pause check.
// account = (index, coldkey, cumulative) with its (hash, sum) proof; demandPath and checkPath
// take an account of any finalized root, named by rootIndex.
claim(noId, rootIndex, account, proof)
demandPath(noId, rootIndex, account, oldProof)  // payable: the immutable demandFeeWei
checkPath(noId, rootIndex, account, oldProof, newPath)
absorbStake(hotkey)
registerPoolUid(noId, maxBurn)
```

Policy, operator roles, guardian and commitment-oracle rotations are future-effective, and so is a pool's
move to v2 (`firstEpoch` must follow the current epoch). A `setAuditors` change applies at once and voids
approvals given under the previous set. Independent validators submit native CRv4 themselves; no EVM
function sets subnet weights.

### 6.3 How the contract holds and moves α

- **Emission capture (the key property).** The immutable vault's coldkey owns every NO miner-pool UID.
  At the exact epoch boundary (within a bounded close grace), `captureEmission` reads the complete stake
  on that pool hotkey and moves it to the vault's escrow hotkey. A missed close records zero for that
  epoch and defers the still-on-pool stake to the next timely boundary; a late keeper can never assign a
  multi-epoch delta to the first missed epoch. Runtime 454 retains v452's rejection of a same-subnet move whose live
  TAO-equivalent value is below `DefaultMinTransfer`. The vault binds that value (100,000 rao on mainnet) as
  an immutable constructor word: the testnet deployment read it from the finalized runtime's
  `InitialMinTransfer` metadata constant, and the mainnet contract phase takes it from the reviewed plan,
  which pins the runtime's code and metadata hashes. If a pool observation is below it, the epoch records zero
  captured funding and emits an
  explicit dust deferral while the α remains on the same pool hotkey to accumulate; a price-read failure
  reverts for retry. Only an exact measured pool decrease and escrow increase enter the accounting ledger.
  From E_s, vault2 captures each v2 pool the same way, except that the close window is the 1,200-block
  (+4h) root commit window, closing an epoch first defers every earlier unfunded epoch of the pool, and a
  capture the runtime refuses (dust, or a runtime failure) reverts: the epoch is later deferred with zero
  funding, and its α stays on the pool hotkey for the next close. vault2 observes only the pool hotkey's
  movable stake (below). The pool must lose exactly the observed stake; escrow may gain one rao less, and
  only its measured gain is funded. Each epoch's cumulative funding is snapshotted once (`fundedThrough`),
  and a root for that epoch must total exactly that snapshot.
- **Validator emission is native.** Independent validators stake their **own** α and earn their **41%
  dividends natively** to their own hotkeys ∝ stake × vtrust — the contract neither stakes for them nor
  custodies their dividends. (Native dividends are the **whole** validator reward; a fee‑funded effort
  bounty **(X)** is out of scope, §9.3/§13.6/D29.)
- **Deposits in → isolated staging → immutable reserve.** Each NO has a distinct coordinator-owned
  `depositHotkey`. The operator first sends the amount plus the two-rao allowance to that isolated position
  through a native,
  signer-scoped transfer intent. Its distinct `depositSigner` then calls `deposit` or `addConviction`
  with the next nonce and deadline. In one EVM transaction the coordinator verifies signer, nonce,
  policy/caps and available stake; stages `amount` plus a bounded two-rao runtime rounding allowance;
  moves it to the reserve hotkey; transfers it to the reserve sink's coldkey; records exactly `amount`
  principal; and emits the policy-bound event. Runtime 454 retains v452's possible one-rao floor at each of those two
  destination share-pool transitions, so the sink must receive between `amount` and `amount+2`; any
  wider delta or failed step reverts all EVM accounting. No NO can attribute another NO's staged funds.
- **Payouts out — emission only.** A valid Merkle claim first fixes the provider's entitlement as durable
  credit for that exact coldkey. The vault aggregates credits across NOs/epochs because runtime 454 also
  rejects a sub-`DefaultMinTransfer` `transferStake`. Once the credit's live TAO-equivalent value reaches
  the immutable floor, the vault transfers the full credit from escrow to that coldkey and admits the
  payment only after measuring exact source and destination deltas. A sub-floor credit, price outage, or
  runtime failure emits a payment-deferral event and preserves the credit for permissionless retry via
  `withdrawClaimCredit`; it cannot be redirected and survives entitlement expiry. From E_s, vault2 pays
  v2 accounts instead. A claim proves an account's `(coldkey, cumulative)` leaf in any finalized root and
  pays `cumulative` less the account's `claimed` watermark, so one claim against the latest root collects
  everything accrued since the last. Payment goes only to the leaf's coldkey, whoever sends the claim, and
  a pool's claims never exceed its latest finalized total. Escrow must lose exactly the amount and the
  coldkey gain it or one rao less. vault2 keeps no credit: a payment the runtime refuses (below its
  100,000-rao TAO-equivalent transfer floor, or blocked by a full staking-hotkey list) reverts the whole
  claim, which stays claimable, and nothing expires. The release 1.0 vault keeps paying epochs before E_s
  until their expiry, and its credits through `withdrawClaimCredit` forever. Recipients receive α as stake.
  Deposit/conviction principal is held by a different contract coldkey whose bytecode has no outbound
  operation.
- **Surplus in vault2.** Anyone can call `absorbStake` to move the movable stake that vault2's coldkey holds
  on another hotkey (never its escrow or a pool hotkey) into escrow, where it is surplus that no account is
  owed. Stake the escrow position gains outside a capture is surplus too: the escrow hotkey earns nothing
  while a validator sets weights, but in a tempo without weights Yuma's fallback pays validator emission by
  stake, and the rehearsal escrow earned that way. Only `assignSurplus`, a coordinator call capped at the
  escrow's surplus, credits surplus to a pool's funding. The first `STSettlement2` release never calls it;
  a later upgrade adds a wrapper for the release 1.0 carry sweep (`VERSION2-FINAL.md` §16).
- **Locked stake in vault2.** Since runtime 475 (unchanged in 478) the runtime can lock part of each
  registration's price as collateral on the
  registering coldkey's position (a subnet's collateral lock share, 0 by default). `getStake` includes the
  lock, a move or transfer may not dig into it, and only emission the hotkey earns releases it. vault2
  therefore moves and counts only **movable** stake, `getStake` less the lock it reads with
  `getMinerCollateral`, in captures, `absorbStake` and the backing check, while its delta checks still
  compare whole positions. Accounts also reject incoming conviction-locked alpha unless they opt in, and
  vault2 never does, so no third party can push a lock onto its positions (`VERSION2-FINAL.md` §7.3).
- **TAO is only touched** for gas, registration burns (paid as call value) and, from E_s, vault2's demand
  fee, besides the owner's discretionary conversions. Every internal move is a same-subnet `moveStake` or
  `transferStake`, so participants pay AMM slippage only where α is bought for a deposit (§7.4) or sold to
  exit α→TAO.

### 6.4 Control, custody, and the launch governance model

Because the contract set is custody-critical (§13.1), *how each component is controlled is part of the
spec.* The upgrade authority reaches only the coordinator and, under version 2, `STSettlement2`. The reserve
sink and both settlement vaults have no owner or upgrade authority after their one-time recorder/coordinator
links are fixed. Coordinator control is a bundle of admission, future-effective role/policy changes,
finalized commitment mirroring, pause of new-risk surfaces, and UUPS upgrade authority. `STSettlement2`
control is the auditor set and quorum, v2 pool registration and its own UUPS upgrade; v2 settlement stops
while the coordinator is paused. We launch these policy powers centralized but
bounded (Phase 0), then add delay (Phase 1). Deeper decentralization —
trustless inputs, on‑chain governance, immutability, and handing off the Bittensor subnet‑owner role —
is deferred (§6.4.3) until the mechanism is proven (notably the `VALIDATOR.md` §10 defenses).

**Invariant across every phase — earned claims are sacrosanct.** Once
`finalizeOperatorEpoch(e,noId)` causes the vault to fix an entitlement, its root, artifact hash, total and
expiry cannot be rewritten. `claim` is implemented in the non-upgradeable vault and has no pause/admin
gate, so **no coordinator upgrade, pause, operator deactivation, owner or guardian action may block or
claw back that finalized claim.**

Under version 2 the same boundary holds in vault2. A finalized root is kept forever and cannot be changed,
and vault2's `claim` has no pause or admin gate, so no `STSettlement2` upgrade, pause, owner or guardian
action can rewrite a finalized root or stop a claim. Anyone holding a successor root's tree can delete it
with `checkPath` if it lowers an account of a finalized root, gives that account another coldkey or hides
it behind an inconsistent node. If the tree is withheld, anyone can `demandPath` the on-chain opening of an
account, proven against any finalized root, and the root cannot finalize until each demand is answered by
an opening that shows no fault. An accrued v2 balance therefore survives a bad successor as long as someone
challenges or demands during the 14,400-block window (`VERSION2-FINAL.md` §4.2).

**One runtime switch sits outside every contract.** The SN25 owner key (the `ur-owner` multisig), or the
chain's root, can disable stake transfers on the subnet (`sudo_set_toggle_transfer`). Every `transferStake`
then fails, so no claim of either version pays until transfers are enabled again (a v1 claim keeps its
amount as claim credit). That freezes claims but cannot take or redirect a balance: a frozen v2 claim reverts
and stays claimable, nothing expires, and captures, which move stake without the transfer check, continue.
The SN25 owner key should therefore be kept under the Safe's governance; SN25 ownership itself stays with
`ur-owner`.

**Sibling invariant — the buyback reserve is one‑way.** The reserve sink has no outbound stake function,
generic call, delegatecall, owner, proxy or upgrade path. Deposits and dividends can only enter its
mapped coldkey position. This is a physical bytecode boundary from day one, not a promise made by an
upgradeable proxy. The reserve is publicly auditable every finalized block:
`getStake(reserveHotkey, sinkColdkey, netuid) >= principal`.

#### 6.4.1 Phase 0 — Launch (central control, fast bug‑fixes)

- **Testnet owner = one dedicated, generated, value-capped EOA; mainnet owner = a distinct Safe**, 1-of-1 at
  the SN25 launch by owner decision (October 7); a Safe transaction can add owners and raise the threshold.
  Neither key is used by an operator, validator, depositor, root task, keeper or server process.
- **Upgradeable UUPS coordinators only.** The owner can patch open/future coordination logic in the
  coordinator and, under version 2, in `STSettlement2`. It cannot replace the reserve sink or either vault's
  bytecode and cannot modify finalized entitlement state or a finalized vault2 root. Even an upgraded
  `STSettlement2` cannot commit a v2 root whose total differs from the pool's funding snapshot, finalize
  one before its 14,400-block window ends or while a demand is open, or stop a challenge from deleting it.
- **Owner-gated admission** (`registerOperator` owner-only; under version 2 also `setAuditors` and
  `registerOperatorPool`, which commits a pool to v2 from its `firstEpoch`). Per-NO deposit and root signers
  are distinct scoped roles; finalization/cleanup are permissionless; the guardian can pause only new-risk
  coordinator surfaces and only the owner can unpause. Under version 2 the reviewed `STSettlement2`
  finalizes a root only after an auditor quorum (at launch the validator alone) approves it, and the
  coordinator's pause also stops v2 closes, commits and finalization, never claims, approvals, challenges
  or demands.
- **Prices.** On SN25 the key pinned to sign the price sheet (§7.1) is the sole owner of the launch Safe. A
  signed entry can set any price up to the coordinator's on‑chain cap, for all operators; it moves no funds
  and does not change the policy.
- **Treasury:** the 18% owner cut accrues to SN25's owner coldkey, the `ur‑owner` native multisig, not to
  the Safe (a governance treasury later, §6.4.3). The native treasury `ur‑reserve`, a separate 2‑of‑3
  multisig, receives the miner allocation beyond the providers' momentum share (9/10 at launch) through its
  own recipient UIDs (§8.5). The buyback reserve is **not** treasury — it is contract‑locked (§7.4).

#### 6.4.2 Phase 1 — Harden custody (bound the owner without losing bug‑fixes)

The highest‑leverage step; it answers "the owner can affect the 7‑day rewards" by adding *delay and
visibility*, not by removing the ability to fix bugs.

- **Timelock on every coordinator upgrade and parameter change**, delay **≥ 1 epoch (target 2 epochs ≈ 14 days)**.
  Any change to reward logic or parameters is queued in public for a full epoch before it can take
  effect, so participants can audit it and `claim`/exit ahead of it. With the §6.4 invariant, the owner
  cannot alter finalized vault entitlements or vault2 roots; ordinary policy/operator changes are already
  constrained to future epochs (a v2 auditor-set change is not, §6.2).
- **Role split (least privilege):**
  - a **larger M‑of‑N multisig** holds the **timelock** (proposes/executes upgrades + parameter
    changes); and
  - a separate **guardian** holds **pause‑only** power — it can halt `deposit`
    (and, if necessary, the *opening* of new claims) to stop an exploit in progress, but **cannot move
    funds, change parameters, or upgrade**, and **cannot block claims for already‑finalized epochs or
    v2 roots**.
- **Emergency power is therefore strictly *pause, never seize or rewrite*:** the worst a compromised
  guardian can do is freeze new activity (a liveness incident; a pause across an epoch's close window, or
  across a v1 epoch's root window, moves that epoch's pool emission to a later epoch's earners), not cause a
  custody loss.

#### 6.4.3 Deferred to later phases (designed‑for, not in this version)

Recorded so the seam is built now: making the **inputs** trustless (median / α-native oracle,
permissionless bonded admission and stronger cryptographic inputs for operator-epoch finalization);
**on-chain governance** of the timelock (α-stake-weighted via the staking / `0x80D`
precompiles, or participant/reputation‑weighted — basis TBD); and transferring the **Bittensor
subnet-owner role** to broader governance. The designed-for seam already exists: custody/settlement and
reserve are immutable, while admission, oracle and parameters live in the replaceable coordinator. Version 2
keeps that seam: vault2 holds v2 custody and fixes the challenge window and its guarantees, while root
admission (the auditor set and quorum) lives in the replaceable `STSettlement2`.

---

## 7. Deposits (conviction stake — the demand signal)

### 7.1 No on‑chain oracle — a published per‑tier rate schedule

`seed/INCENTIVES.md` frames deposits as "per used GB and active user … based on the global fixed rate set by
an oracle." We **drop the on‑chain oracle entirely**: per‑GB / per‑user usage is **self‑reported and
unverifiable on‑chain**, so pricing it on‑chain buys nothing — the only quantity the protocol can act on
is *the α actually deposited*. A NO stages α on its isolated deposit hotkey and its scoped signer calls
`deposit(noId,amount,nonce,deadline)`.

The **"global fixed rate"** survives as an **off‑chain published reference** — now a **schedule of
deposit rates per conviction tier** (§7.3): `rate(tier)` has **two components**, `rate_gib` (α per GiB of
real usage) and `rate_user` (α per distinct user), over one shared denominator, and a NO's required
deposit for an epoch is `floor(bytes × rate_gib / GiB + users × rate_user)`, capped per epoch. The rates
and the cap are those in force for that epoch, and they price the previous epoch's usage. `bytes` and
`users` are the totals of the NO's **signed usage** for the previous epoch (`total_usage_bytes`
and `total_users` — the latter the same distinct‑users figure its public stats feed publishes per block).
An "active user" means `total_users`: a distinct top‑level client with contract usage in the epoch window.
The reported usage is signed and audited against the deposit (§8.1), but it still enters no on‑chain
computation. In release 1.0 (D25) this schedule is read by **the validators** (who set the weights, §8.1)
as well as by NOs (to size their deposits); it is **never a value the contract consumes**. This keeps a
whole subsystem (`setRates`, on‑chain rate storage, the TAO/USD feed) and a trusted input out of the
contract.

**The signed price sheet.** Rates come from the signed price sheet published at
`https://ur.xyz/price.yml`; the owner's steps for turning prices on are in `mainnet/DEPOSIT-PRICES.md`. Each
entry in its `signed_schedule` is signed (EIP‑191 `personal_sign`) by a key
pinned in the software, over one line of plain text that names its chain, subnet, effective epoch, cap and
every tier's rates, so a person signing on a hardware wallet reads exactly what they approve:

```
UR price sheet v1: chain <chain>, sn <netuid>, from epoch <epoch>, cap <cap> alpha per epoch, tier <threshold> alpha <rate_gib> per GiB <rate_user> per user
```

The line has one `tier` clause per tier, and every amount in it is an exact decimal of α. On SN25 the pinned
key is the sole owner of the Safe that owns the coordinator, the same authority that schedules its
policy (§6.4.1). An entry applies from its effective epoch until a later one takes effect. The owner's signing
tool refuses an effective epoch earlier than the next one; readers do not check this. A signed entry is
permanent, so only a later entry can correct it. NOs and validators read the sheet the same way. An
unverified file is never used: a reader keeps the last good sheet, and it refuses a file that drops or
changes a signed entry or adds one before an entry it holds. A reader that has never verified a sheet and
cannot fetch one has no price from it: an NO pays nothing until it can verify one, and a validator steers
that pass under the signed policy's schedule and logs the problem, so an outage cannot stop steering. The top‑level `tiered_per_block_demand_deposits` is an unsigned mirror
for people and has no authority.

**With no signed entry in force, the signed policy's schedule stays in force.** At launch that is the
explicit zero‑price mode below. A deployment with no pinned key, such as the testnet, has no published price
and stays on its signed policy's schedule. The signed policy document and its hash do not change when prices
change, so consents signed under the client‑key history domain stay valid (`VERSION2-FINAL.md` §2.2). An entry
replaces the schedule and the cap; the policy's usage lag, tier snapshot and mismatch action still apply.

**Caps.** An entry names the per‑epoch cap the formula uses. The coordinator's own on‑chain cap
(`epochDepositCapRao`), set through `schedulePolicy` under the same policy hash, must be at least the cap in
force: the entry's, or the signed policy's while no entry is in force. Its lifetime `campaignDepositCapRao`,
which the same call sets, must be at least the larger of the signed policy's campaign cap and that on‑chain
epoch cap. Otherwise validators stop on the mismatch, as they stop on other policy mismatches.
`schedulePolicy` takes effect only from a later epoch, so a higher on‑chain cap must be scheduled to take
effect no later than the entry. A deposit reverts once the campaign cap is used up.

**Usage.** Epochs before `E_s` keep the signed payout artifact. Settlement v2 has no payout artifact, so for
each epoch from `E_s` the NO publishes a signed **usage statement** (schema `urnetwork-epoch-usage-v1`,
`docs/spec/epoch-usage-v1.schema.json`): its
artifact key signs it, and its public `/sn/evidence` routes serve it as kind `epoch-usage-v1`, run
`<noId>-<epoch>`. The NO publishes it after the epoch's root commit, built from the provider chunks the
committed header lists, with the user count taken at the epoch boundary. Validators take the latest root
committed for the epoch in its root window (§11.1); a later replacement of that root does not change the audit.
They use the statement that names that root's chunks for the epoch. Its rows, eligible or not, must match its
`provider_rows` and sum to its `total_usage_bytes`, so the bytes can be checked against the ledger; the rows
themselves stay operator‑attested (§8.2). Two statements that verify for the same chunks with different totals
leave the pool without weight. `total_users` remains an operator attestation, as in a payout artifact: priced by
the deposit but not independently verified. A statement adds to the ledger and changes none of its bytes, so
a dispute over users cannot hold up settlement. Only a signed entry in force switches the audit to the statement;
a deployment priced by its signed policy alone keeps the payout artifact.

**Timing.** A deposit for epoch `e` prices epoch `e−1`'s usage and must land before `e`'s end block, because a
deposit counts toward the epoch it lands in. It is audited at every steering pass, not only at the end: until
the deposit so far this epoch equals the amount owed, the pool has no weight on that pass, and overpaying
fails too. So the NO pays as early in `e` as it can, once `e−1`'s root is committed and its statement
published, and retries until the deposit lands; the end of the epoch is only the backstop. From the start of each priced epoch until the
source epoch's usage is committed on chain, its totals are published and the deposit has landed, the pool
carries no weight, and its share goes as §8.5 says for an empty tier. A commit late in the 1,200‑block root
window (≈4 h, §5.2) lengthens that gap.

**Zero price — the launch mode.** The schedule in force may be **entirely zero** (every component of
every tier), but only when that is explicit: the signed policy says `zero_rate_action: equal_demand`, or a
signed entry of all zero rates returns to this mode. The policy
default (`halt`) rejects an all‑zero schedule, and a schedule with zero rates in *some* tiers only is
always invalid, so a zero price can never happen by accident. While the price is zero, NOs make **no
demand deposits**: none are required and none are audited. Every registered pool is given the **same
implied demand** (exactly 1, §8.1), so the validators' measured quality alone steers the pool channel; a
voluntary deposit or conviction lock neither helps nor hurts a pool. The head channel, θ, the quality
clamp and the cede rule are unchanged. This is a deliberate launch mode used while bugs are flushed out
of the network. Version 2 settles each pool's epochs from E_s through the v2 ledger rather than a payout
artifact, and the signed policy's `deposit.usage_artifact` field is inert for those epochs, whose usage comes
from the usage statement. The deposit audit stays dormant at zero price and starts with the first signed
entry that has nonzero rates, which takes no policy change (`VERSION2-FINAL.md` §9.4).

### 7.2 A deposit is conviction stake (D25)

The deposit anchors the claim: `D_n` (α) is the NO's costly, on‑chain input, and validators audit it
against the usage the NO signed for the previous epoch (§7.1) and weight that usage, priced at the
conviction‑zero rate (§8.1). The protocol
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
- Each component's rate is **non‑increasing with conviction**, and a component is priced in **every tier
  or in none**. The schedule is governance‑set and published off‑chain in the signed price sheet (§7.1),
  read by validators and NOs. A signed entry holds one to eight tiers, begins at conviction zero and has
  strictly increasing thresholds, and a file with an entry that breaks these rules or has no cap is refused. A
  schedule that is **zero in every tier** is the explicit **zero‑price launch mode** (§7.1): no deposit is
  required or audited and every pool carries the same implied demand. Because validators price the
  *audited usage* rather than dividing the deposit by the rate, a zero rate no longer implies unbounded
  usage (§8.1) — it simply prices nothing.

The stake and the deposit are the **same locked pool** (§7.4): both are conviction, both are sunk. The
tier is thus a smooth function of an NO's accumulated commitment — depositing over time *is* staking
conviction.

### 7.4 The reserve — staked, locked, compounding (the buyback)

Every deposit is a **buyback** in the sense the leading revenue‑generating subnets use (Chutes'
buy‑and‑**lock**, `COMPARISON.md`): usage revenue is converted to α and permanently removed from liquid
supply, rather than recycled to participants who would re‑sell it.

- **The flow.** The NO funds deposits by **buying α on‑market** from customer revenue — the buy leg is
  the actual demand pressure. No contract or validator enforces that purchase: on‑chain code cannot see where
  α came from, and a correct deposit may come from any source of α. A sourcing commitment, which makes the
  buyback claim falsifiable, is to be published alongside the §7.1 price sheet once deposits are priced; the
  sheet's signed entries name only prices and carry none. The reference server's demand deposit task pays from
  its alpha wallet, which is the coordinator's deposit signer. When that wallet cannot pay in full, the task
  can convert USDC from a Solana wallet to SN25 α over an on‑chain route (Jupiter, a LayerZero OFT, wTAO, the
  staking precompile). That route is a choice of operator software, not a protocol rule.
  `deposit(noId, amount)` stages the **full amount** plus a two‑rao rounding allowance, `moveStake`s it to the
  **reserve hotkey** — the owner's validator hotkey (§6.3) — and transfers it into the immutable sink
  coldkey. The two destination share-pool views may each floor one rao; finalized sink credit must still
  cover the full principal, while any zero-to-two-rao excess is an irreversible reserve donation. It emits
  `Deposit` plus `ReservePrincipalAdded`. `addConviction` follows the same one-way path but emits `ConvictionAdded` and
  does not increase epoch demand. Coordinator counters enforce exact nonces/caps; neither contract
  computes weights from them (D25). The finalized events are the public weighting record.
- **The lock.** No contract code path ever sources a transfer from the reserve (the §6.4 sibling
  invariant). Staking per se enforces nothing — dTAO stake has **no unbonding period** (§2.3) — so the
  lock is the immutable sink's *missing exit path* (§6), and it is auditable on‑chain every block
  (`getStake(reserveHotkey, sinkColdkey, netuid) ≥` the sink's recorded `principal`).
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
needed: each deposit is already an **on‑chain event** (`Deposit(noId,e,funder,amount,policyHash,nonce)`)
authorized by the scoped deposit signer, so a NO's per‑epoch deposits and cumulative conviction are publicly and authoritatively
**summable straight from the event log** — that *is* the signed, published deposit list. At every steering
pass validators read the coordinator's counters that these events increment, at a hash‑pinned finalized
block (§8.1): this
epoch's deposit total for the deposit audit, and cumulative conviction less this epoch's deposits and added
conviction for the tier (the policy's `conviction_before_epoch` snapshot). The coordinator stores only
nonces and cap/audit totals, not a weighting oracle (D25); the finalized logs reproduce those totals for
independent replayers.

---

## 8. Miner channel: per‑NO pool, validator‑weighted by demand × quality, Merkle‑claimed

### 8.1 Across operators — validators weight the pools; Yuma emits ∝ implied‑usage × quality (D25)

**The contract does no weighting.** Independent validators (§9–§10) compute each NO miner‑pool UID's
score **themselves**, from **published data**, and submit it as Yuma weights; Yuma medians/clips the
scores and emits to the pool UIDs — so the miners' reward *is* the validators' evaluation. Each validator
reads, per NO `n`, straight off chain state, signed published data and its own trails:

- **`epoch_deposit_n`** — this epoch's deposit so far, read from the coordinator (§7.5), together with the
  NO's signed usage for the previous epoch (§7.1: its payout artifact before `E_s`, its usage statement from
  `E_s`) and its audited **`bytes_n`** and **`users_n`**: the
  deposit must equal `floor(bytes_n × rate_gib(tier_n) / GiB + users_n × rate_user(tier_n))`, capped, or
  the pool is zero‑weighted on that steering pass (the deposit audit, §7.1). The audit is dormant at zero
  price (`VERSION2-FINAL.md` §9.4);
- **`tier_n`** — the NO's conviction tier, from its cumulative locked α before the epoch (§7.2, §7.5), and the
  published
  two‑component **`rate(tier_n)`** (§7.3), at the rates in force for this epoch (§7.1), which price the
  previous epoch's usage;
- **`Q_n`** — the pool's aggregate provider quality, from the validator's own `VALIDATOR.md` trails.

and sets

```
weight_n  ∝  implied_demand_n × clamp(Q_n)                                  # 0 at Q_n = 0; else Q_n clamped to the policy's
                                                                            # bounds, [0.75, 1] at launch (§12.3)
implied_demand_n = (bytes_n × rate_gib(0) / GiB + users_n × rate_user(0))   # audited usage priced at the conviction-zero tier
                   × min(1, cap / owed_n)                                   # truncated exactly as the capped deposit was
implied_demand_n = 1                                                        # while the price in force is zero (§7.1)
```

**Implied demand, not raw deposit** (decision A, D25): the audited usage is priced at the **baseline
(conviction‑zero) rate** for every NO. Below the per‑epoch cap a NO on a lower tier rate posts less α for
the same real usage and gets the **same** weight — the conviction stake that bought the lower rate is a
*discount, not a penalty*, and the weight still tracks **real revenue‑backed usage** (the headline
thesis) rather than raw α. With `users_n = 0` this is the earlier `epoch_deposit_n / rate(tier_n)`
ranking, up to the deposit's floor. When the cap truncates the deposit a NO owes at its own tier
(`owed_n`), its demand is truncated by the same factor, so usage the NO did not pay for buys no weight.
A capped NO's demand is `cap × baseline / owed_n`, with one priced component `cap × rate(0) / rate(tier_n)`:
it no longer grows with usage, and a lower tier rate raises it, so at the cap the tier does change the
weight. Under the testnet's policy v2 (`deploy/testnet/policy-v2.yml`: a 10 α cap; 40, 32 and 24 α per GiB
at conviction 0, 1 α and 10 α) a pool is
capped above 0.25, 0.3125 or about 0.42 GiB by tier, and 1 GiB carries an implied demand of 1·10^10,
1.25·10^10 or about 1.67·10^10 rao. Under the zero‑price launch mode every pool's implied demand is
exactly 1 and `Q_n` alone steers (§7.1). NO `n`'s miner‑pool UID then accrues
over the epoch

```
emission_n  ≈  0.41 · E_epoch · μ · (1−θ) · ŵ_n,   ŵ_n = consensus(implied_usage_n · Q_n) / Σ_m consensus(implied_usage_m · Q_m)
```

**as α stake the immutable vault holds** (the vault owns the pool UID — the NO never receives it). Here `μ`,
the **momentum**, is the providers' share of the native miner allocation: on SN25 it is 1/10 at launch,
pinned by the signed treasury policy, which has validators weight `ur‑reserve`'s recipient UIDs for the
other `1 − μ` (§8.5); without a treasury policy (testnet) `μ = 1`. With no head fleet weighted, θ cedes and
the pools share `μ` whole. At about 1 α per block, one pool, a weighted head and θ = 3/10, that is
0.41 × 50,400 × 1/10 × 7/10 ≈ 1,446 α per epoch. From the capture the flag start epoch `E_c` (epoch 8)
the factor `(1−θ)` is the pool share of §8.5's three‑way split, 4/10, or 9/10 while no head fleet is
weighted, and NO `n`'s CTF pool UID accrues `0.41 · E_epoch · μ · 1/10 · ŵ_n` beside it (§8.7). Demand (implied
usage) anchors `ŵ_n` to revenue‑backed usage (§7); the consensus‑measured `Q_n` modulates it — a NO with
poor providers earn less even at high demand (swing capped at bootstrap, §12.3). The immutable vault
captures the realized stake on each pool hotkey at the epoch boundary; it never estimates settlement
from a point-in-time incentive/emission view. (Validators each apply the same published rate schedule + their own `Q_n`; Yuma's
median/clip/vtrust over many independent validators is what turns those into the pool's pay, §10.)

### 8.2 Within an operator — the payout ledger (usage‑proportional, auditor‑approved)

The NO does not hold the pool, and in version 2 it no longer chooses the split either:
`seed/INCENTIVES.md`'s "the network operator determines the payout from their mining slots" now means
only that the NO supplies the usage and wallet records. For every epoch from the switch epoch `E_s` (the
pool's v2 `firstEpoch`, `VERSION2-FINAL.md` §3, §6), provider `p` is **eligible** in NO `n`'s pool when
it has settled usage in the epoch's earning window, a payout coldkey selected by a signed consent (its
own, else its network's, else a hotkey delegation), and no active fleet binding at either epoch boundary
(head fleets are paid natively, §8.4). Its share is proportional to its **verified usage and nothing
else**, allocated in exact integer rao by `coldkey_usage_largest_remainder_rao_v1` for the amount `A` the
pool allocates for the epoch (its funding plus any carry, §8.3):

```
s_{n,p}     =  usage_{n,p} / Σ_{eligible p'} usage_{n,p'}  for eligible p, else 0;      Σ_p s_{n,p} = 1
U_{n,c}     =  Σ usage_{n,p} over eligible p whose payout coldkey is c;                ΣU_n = Σ_c U_{n,c}
base_{n,c}  =  floor(A · U_{n,c} / ΣU_n),      rem_{n,c} = (A · U_{n,c}) mod ΣU_n
δ_{n,c}     =  base_{n,c} + (1 rao if c is among the A − Σ_c' base_{n,c'} coldkeys with the largest rem, else 0)
               ties → ascending coldkey;   Σ_c δ_{n,c} = A exactly, each within one rao of A·U_{n,c}/ΣU_n
```

where `usage_{n,p}` is the settled payout bytes `p` served for NO `n` on contracts that closed between
the epoch's boundary-block timestamps. Aggregating by coldkey first means splitting a provider never
changes its coldkey's amount. The per‑provider `reliability_{n,p}` and release 1.0's measured‑cohort
eligibility (`a_min` minimum exposure, `VALIDATOR.md` §7.4) are **gone from the payout**: quality stays
operator‑level (`Q_n` weights the pool against other pools, §8.1, and cancels while there is one
operator), so measurement no longer gates who is paid or limits how many are.

The NO publishes each epoch's inputs as provider rows `(client_id, coldkey, usage_bytes, head_excluded)`
in its ledger (§8.3). The validator checks every row (each head flag against chain state), recomputes
the allocation and the accounts, and approves nothing that does not reproduce exactly. The NO alone still
attests the population, the usage amounts, window membership and wallet selection (owner decision,
October 11) — its remaining lever, published but not yet checkable. Committing the ledger's root (§8.3)
is the NO's only privileged on‑chain act in the miner channel — a *record*, never custody.

### 8.3 Settlement — providers claim per‑NO, cumulatively, directly from the contract

From `E_s` the pool settles through two contracts beside the unchanged release 1.0 set: the immutable
**vault2** (`STSettlementVault2`), which owns the pool UID and holds the funds, and an owner‑upgradeable
coordinator, `STSettlement2`. Within 1,200 blocks (≈ 4 h) of each epoch's end, vault2 moves the pool
hotkey's realized emission (its movable stake, never registration collateral, §6.3) into escrow and
snapshots the pool's cumulative funding, so epoch `e` funds

```
f_e  =  fundedThrough_n[e] − fundedThrough_n[e−1]      // a missed close funds 0; the next close captures that α
```

**One cumulative account per payout coldkey per pool, never expiring.** NO `n`'s ledger is an ordered
list of accounts `(coldkey_i, cumulative_i)` in rao. An account's position and coldkey never change, its
cumulative never decreases, new accounts are appended in ascending coldkey order, and none is ever
removed. Each root covers every epoch since the last finalized root (at most 96), allocates each from
its own provider rows (§8.2) and adds each coldkey's `δ` to its account; a wallet change sends later
deltas to the new coldkey's account, and the old one keeps its balance. Value moves to later earners only
when an epoch has no eligible usage, when the NO explicitly declares its inputs unrecoverable, or when
its close is missed; an epoch whose root is missed, refused or deleted is allocated later from its own
usage (owner decision, October 11). The accounts are committed as a **Merkle sum tree**, each node
committing to its children's hashes and sums:

```
leafHash(c, v)            =  keccak256(0x00 ‖ c ‖ uint256(v))
nodeHash(hL, sL, hR, sR)  =  keccak256(0x01 ‖ hL ‖ uint256(sL) ‖ hR ‖ uint256(sR))     // node sum = sL + sR
commitDistribution(e, n, prevRootCount, (root, total, leafCount, ledgerHash)):  total = root sum = fundedThrough_n[e] > finalizedTotal_n
                                                                                  root ≠ 0, ledgerHash ≠ 0
```

`ledgerHash` is the sha256 of a header listing each covered epoch's provider‑row chunks and the account
chunks — public, content‑addressed objects that the validator retains.

**Admission.** A root becomes claimable only once a quorum of owner‑admitted auditors has approved it
before the next epoch ends (at launch: quorum 1, the validator, after its audit reproduces the ledger,
§8.2) **and** a 14,400‑block (≈ 48 h) window fixed in vault2 has passed since the commit. During the
window anyone may **challenge** — prove on chain that the root lowers, replaces or hides an existing
account, which deletes the root — or **demand** that the operator open an account's path on chain, which
blocks finalization until answered. A demand proves its account against a finalized root it names, any of
the pool's, so only an existing account can be demanded, each at most once per root and pending root, and
withholding the newest root's ledger cannot shield an older root's accounts. A withheld or garbage node can
never be opened, so a demanded account is protected even when the tree is not published. A root still
unfinalized at the end of the next epoch can be replaced, and its epochs' funding stays for the replacement.
A refused or deleted root does not by itself change the pool's weight while there is one operator: zeroing the pool
would hand the pool channel to the head (§8.5), or with no head fleet on SN25 the whole row to `ur‑reserve`,
punishing providers for the operator's fault. The deposit audit is a separate rule (§10).

**Claims are cumulative and permissionless**, and the contract derives the α from **on‑chain state**:

```
vault2.claim(n, rootIndex, (i, coldkey, cumulative), proof):         // anyone may call; claims never pause
    verify (coldkey, cumulative) is leaf i of finalized root roots[n][rootIndex]
    amount = cumulative − claimed[n][i]                               // everything accrued since the last claim
    require amount > 0 and claimedTotal_n + amount ≤ finalizedTotal_n // a pool can't be over-drained
    claimed[n][i] = cumulative
    transfer amount escrow → coldkey     // escrow loses exactly amount, coldkey gains it or 1 rao less; else revert
```

One claim therefore pays everything an account accrued since its last claim, across any number of
epochs, always to the leaf's coldkey whoever sends it. Every finalized root is kept, so nothing expires;
a claim the runtime refuses (for example below its transfer floor) reverts and stays claimable — there is
no deferred claim credit. The promise is accrual, not timing: a balance is delivered when anyone claims
it, which is worth doing once it is large relative to the claim's gas. A provider attached to several NOs
has **one account, and makes one claim, per NO** (the trade for dropping the global root). **Every α of
the miner channel flows contract → provider; the operator holds none of it.**

Epochs before `E_s` keep release 1.0 settlement (§6, §11.2): per‑epoch `payoutRoot`s of 10,000 bps shares
built from usage × reliability, claims into durable `claimCredit`, and TTL+grace expiry into same‑NO
carry. Those claims stay open until each epoch's expiry, and accepted credit stays withdrawable.

> **The pool is the unit of scale.** Providers number 100k+ and cannot be UIDs, so the *pool* is the
> on‑chain miner (one UID per NO) and the providers are paid *inside* it by Merkle claim. Yuma operates
> at the pool granularity (§10); the within‑pool split is §8.2.

### 8.4 Top-level miners — the direct head channel (routable-IP breadth, D27)

The pool tier scales to 100k+ providers but pays them by a NO-published Merkle ledger. The **top ~200
fleets by routable-IP breadth** *also* get the **canonical Bittensor treatment**: each claims **its own
miner UID**, is **steered directly by validators**, and is **paid natively**. This is the supply apex of
§1 — and it is *more* trust-minimized than the pool (no operator in the payout path).

**The metric — routable egress breadth, not traffic (D27).** Exact source addresses are used only for
unambiguous hop attribution: one eligible exact address maps to one `client_id`, and ambiguity excludes
it (`VALIDATOR.md` §8.2). The public scoring unit is separately
`HMAC‑SHA256(egress_hash_key, "urnetwork/egress-prefix/v1" ‖ masked_prefix)`, where `masked_prefix` is the 4‑ or
16‑byte address masked to the policy granularity (initially /29 IPv4 and /48 IPv6) and the policy names the
key by `egress_hash_key_id`. A fleet's score is the count of **distinct routable scoring hashes** its bound
clients serve,
**split when shared**: each hash contributes **1.0 total, divided equally among every eligible head fleet
observed on it**. Thus distinct exact IPs in one co-located prefix deliberately collapse and cannot
double-count supply. A score unit counts only if it is *routable* — the validator completed a real
trail hop through it (`VALIDATOR.md` §7), so liveness is baked into the metric and there is no separate
quality term.

```
for top-level-miner UID u (a fleet — a hotkey with bound client_ids C_u, §11.4):
    IPs(u)   = { distinct keyed prefix hash h : some c ∈ C_u routed a verified trail hop from h } # VALIDATOR.md §7
    claim(h) = #{ bound fleets u' : h ∈ IPs(u') }                                                # split across sharers
    score(u) = Σ over h ∈ IPs(u) of  1 / claim(h)         # split-adjusted unique routable-IP count
    head_weight[u] = EMA(score(u))                        # weight IS the smoothed IP score (decision B):
                                                          #   ¼·score + ¾·prior per tempo (head_score_ema), for the
                                                          #   top maximum_head_fleets (200) by it; other fleets get 0
```

Every validator computes `score(u)` **from its own trails** — the trail/proof wire carries a per-hop
**egress-IP-hash** (`VALIDATOR.md` §8, a keyed hash at a configurable subnet granularity — default /29
IPv4, /48 IPv6 — never the raw IP), so a validator can count each fleet's routable score units and the
sharing splits without trusting anyone. This is the **verify-the-top-200** requirement: a validator only weights
a claimed top miner as high as its *own* observed IP score justifies.

**Identity — the fleet `client_id`s ⇄ hotkey binding (§11.4).** A fleet commits the SHA‑256 of its
canonical member manifest from its head hotkey and binds each `client_id` to that hotkey with its own
**dual-signed binding** (§11.4), proving it controls both the `client_id`s (their `VALIDATOR.md` Ed25519 keys)
and the hotkey (its BT key);
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
  UID; validators weight it on `score`; its bound `client_id`s are **flagged `head_excluded` in any NO's
  ledger** while the binding is active at either epoch boundary, so they earn no pool share (promoted
  out — **no double-pay**; a `client_id` earns in exactly one tier at a time — the server sets the flag
  and the validator checks it, §8.2). Their coldkeys' existing pool accounts stay claimable (§8.3).
- **Demote.** A fleet whose routable-IP breadth shrinks earns the lowest emission, is pruned on the next
  registration, and its `client_id`s **fall back to earning via their NO's pool** — the baseline catches
  them.
- **Score-dip protection (a real risk).** Pruning reads only *current* emission rank, no history, so once
  a UID's `immunity_period` expires one thin measurement stretch can evict a good fleet. Mitigations, all
  owner-tunable: a **high `immunity_period`** (a full measurement ramp for new top miners), an **EMA on
  `score`** (`head_score_ema` 1/4 per tempo, so a single noisy tempo doesn't thrash emission), and a **θ large
  enough that the lowest top
  miner clears the tail** (else the head thrashes against the pools, §8.5). Each (re-)registration pays
  the continuous burn auction (≈ ×1.26 / registration), so churn has a real, tunable cost — and a per-UID
  burn makes the head **more** Sybil-resistant than one pool UID per NO.

**Weight shaping (best practice for ~200 concurrent fleets).** Steer **proportionally** to `score`, *not*
winner-take-all; apply the signed policy's `max_weight_limit_u16` before every CRv4 commit; and drive
`VALIDATOR.md` trails at a rate (validator-configurable, §D26) that gives every top UID regular coverage
so honest-but-idle fleets don't stale-decay. Runtime v454 retains v452's native getter at `65535`
(no cap), despite retaining a `MaxWeightsLimit` storage item, so release 1.0 validators enforce the
policy cap locally and finalized-vector analysis rejects violations. The two-NO testnet bootstrap uses
`32768` (the smallest feasible two-recipient cap), and so does SN25, whose treasury policy requires exactly
`32768` (its two recipients take 45% each, or half each in a reserve-only row, §8.5); a later policy lowers
the cap toward a low single-digit percentage only after enough positive recipients exist, which on SN25 also
needs a newly signed treasury policy. Deterministic `u16` conversion
repairs any one-unit rounding deficit before signing and rechecks the exact integer cap. A native
runtime cap remains the preferred defense against non-conforming validators. This matches the strongest DePIN precedents —
FileTAO's live-scoring and TPN's robust statistics.

### 8.5 The head/tail emission split θ

Both tiers are miner UIDs in **one** mechanism (§14), so the 41% miner emission is divided by the
**weights validators set** — and to make that a controllable policy rather than an accident, validators run
common software that **reserves a governance share θ of the providers' share `μ` to the head and `1−θ` to
the pools**, exactly as Data Universe (SN13) reserves a fixed share to one UID by rewriting weights before
`set_weights`:

```
head[]      =  { EMA(score(u)) }          normalized so Σ head = θ·μ        # split-adjusted routable-IP breadth (§8.4)
pool[]      =  { implied_usage_n × Q_n }  normalized so Σ pool = (1 − θ)·μ  # implied_usage = audited usage at the baseline rate (§8.1)
treasury[]  =  { (1 − μ)/k each }         # SN25: ur-reserve's k = 2 recipient UIDs; none where μ = 1
w           =  head ⊕ pool ⊕ treasury     # one vector; apply signed max_weight_limit_u16; commit-reveal
```

**Momentum and the treasury row (SN25).** `μ`, the **momentum**, is the share of the native miner allocation
paid to providers. On SN25 the signed treasury policy pins it at **1/10** and has validators weight the other
`1 − μ` in equal parts to the recipient UIDs of `ur‑reserve` (SN25 UIDs 170 and 250, 9/20 each), which
receives it for future network improvements; `ur‑reserve` is a 2‑of‑3 native multisig, outside the subnet
owner's hotkeys and separate from the buyback reserve (§7.4). An empty tier cedes its share to the other
(`cede_to_nonempty`); with no provider weight at all the row is reserve-only, the recipients sharing it
equally. A missing or masked recipient, or an entry over the cap, refuses the row rather than clipping or
repairing it. Changing `μ` takes a newly signed treasury policy, and the current validator admits only 1/10;
without a treasury policy (testnet) `μ = 1`. At θ = 3/10, one pool and one head fleet, the row (pool, head,
the two recipients) is [7/100, 3/100, 9/20, 9/20], u16 [10194, 4369, 65535, 65535].

**From `E_c`: three shares replace θ (capture the flag, §8.7).** From the CTF start epoch `E_c` (epoch 8, the
owner's decision of 2026‑11‑11) the providers' share splits three ways. Of `μ`, the provider pools take
**4/10**, the CTF pools **1/10** and the top‑level miners **5/10**:

```
pool[]      =  { implied_usage_n × Q_n }  normalized so Σ pool = s_pool·μ,  s_pool = 4/10   # the provider pools (§8.1)
ctf[]       =  { implied_usage_n × Q_n }  normalized so Σ ctf  = s_ctf·μ,   s_ctf  = 1/10   # each NO's CTF pool UID, at its pool's weight (§8.7)
head[]      =  { EMA(score(u)) }          normalized so Σ head = s_head·μ,  s_head = 5/10   # top-level miners (§8.4)
treasury[]  =  { (1 − μ)/k each }         # unchanged
```

A NO's CTF pool takes the same fraction of the CTF channel that its provider pool takes of the pool channel,
`ctf[n] = s_ctf · μ · ŵ_n` with the validator's own pool weights (§8.1, §10). No CTF‑specific measurement is
needed: a validator's self‑mask covers both of a NO's UIDs, and a pool that the deposit audit zero‑weights
zeroes its CTF pool too. The cede rules apply in this order:

1. **CTF is fixed.** When the CTF channel is non‑empty, it gets exactly `s_ctf · μ`.
2. **Pool and head share the rest in the ratio 4 : 5.** The rest is `(1 − s_ctf) · μ`, or all of `μ` when the
   CTF channel is empty. When either channel is empty it cedes to the other (`cede_to_nonempty`), so with no
   head fleet the pools take `9/10 · μ`: the head cedes to the pools only, and CTF stays at 1/10.
3. **The CTF channel is empty** when the validator does not support CTF (its `ctf` setting is `off`), when no
   NO has a CTF pool registered for the epoch, or when no NO's pool has weight. Its share then joins rule 2.
4. **With no provider weight at all** the row is reserve‑only, as before.

On SN25 with no head fleet, the row (pool, CTF, the two recipients) is [9/100, 1/100, 9/20, 9/20], about u16
[13107, 1456, 65535, 65535]. Once fleets are weighted it is pool 4/100, CTF 1/100, head 5/100 and the
recipients 9/20 each. The pool, CTF and head entries stay far under the 32768 cap, the recipients stay at 9/20
each as before, and the treasury policy (μ = 1/10, cap 32768) is unchanged. In epoch 4 the providers' share funded the pool with 1,195.84 α, all of `μ`. At that
scale the CTF pools get about 119.58 α an epoch and the pool about 1,076.26 α. Once fleets are weighted, the
pool gets about 478.34 α and the head about 597.92 α.

The three shares are fixed by the signed amendment entry v2 (§8.7.5), not by a successor policy, whose new hash
would leave every payout consent outside the new epochs' domain. Changing them takes a later signed entry. The
nominal head share rises from θ = 3/10 (policy v1) to 5/10, and the nominal pool share falls from 7/10 to 4/10.
That changes nothing until a fleet is weighted, and it makes the hard constraint below easier to meet. Each
validator chooses whether to support CTF, and Yuma makes that a stake‑majority choice (§8.7.4).

Every share goes to **real recipients** (top miners; vault-owned pool UIDs; on SN25, `ur‑reserve`'s
ordinary recipient UIDs, outside the owner's hotkeys), so the **June-2026
`(1 − miner_burned)` penalty does *not* apply** — that penalty only bites emission *withheld to an
owner/immune key* (Spec 421, subtensor PR #2781). **Do not "reserve baseline" by burning to an owner UID** —
it would shrink the subnet's whole cross-subnet allocation. Because Yuma clips to the κ-stake-weighted
median, θ takes effect only if a **stake-majority of validators run the same θ** — so θ is a *published
governance parameter*, not per-validator discretion.

**θ is the load-bearing new decision, because it trades the two bets against each other.** Demand-coupling
(`implied_usage × quality`, the headline bet of `COMPARISON.md`) lives entirely in the **`1−θ` tail`; the
head is pure routable-supply breadth (§8.4), *decoupled* from deposits.
- **Large θ (head-heavy):** a big, trust-minimized meritocracy — but most emission is demand-*de*coupled,
  and a NO's best providers then earn from the subnet-funded head, not the NO's demand-weighted pool, so NO
  **deposit incentives weaken**.
- **Small θ (tail-heavy):** demand-coupling stays dominant and deposits stay meaningful — but the apex is a
  small carrot, and graduating can be a *pay cut* (breaking the ladder).

**Recommendation:** govern θ; **start tail-weighted (θ ≈ 0.3)** — the pool is the stated on-ramp/baseline
and demand-coupling is the strategic edge — **instrument realized per-tier pay**, and **widen θ as the
top-miner set and the independent-validator quality consensus mature** (the same "ramp the strength, not the
mechanism" posture as the bootstrap quality-swing cap, §12.3 / D14). Hard constraint: size θ so the
**lowest-paid top miner ≥ the highest-paid pool provider**, or graduation is a pay cut and the head thrashes
against the tail.

### 8.6 Intra‑platform traffic credited to extenders — amendment (approved 2026‑11‑10)

> **Amendment approved by the owner on 2026‑11‑10.** It takes effect from epoch 8 (`E_x = 8`) once the
> signed amendment entry is published (§8.6.7). The billing change in §8.6.7 has been live since 2026‑11‑11.
> It amends settlement v2 only (epochs from `E_s`). From `E_x` it also amends §8.2's provider usage for
> same‑network traffic (§8.6.2). Release 1.0 epochs and their `share_bps` leaves are untouched. The base rule
> is the owner's request of 2026‑11‑10: contracts that settle with zero participants, because the data was
> between the network operator and a client, spread their traffic weight evenly across all extenders, since
> extenders make the api and platform surface reachable from anywhere. The owner's decisions of the same day
> are folded in (§8.6.8), and so is the owner's answer of 2026‑11‑11 to the last open question, Q7: on every
> contract an extender counts as one of the contract's `n_c` hops (§8.6.2, decision 16). Everything marked
> *option* below is an alternative with trade‑offs, not a replacement.

**Extenders, as built** (`connect/EXTENDER.md`). An extender is a relay that a provider runs on TCP and UDP
443 and a DNS carrier. It forwards only to its operator's own api and connect hosts, behind camouflage meant
to resist address and SNI blocking, and the client's inner TLS to the platform stays end to end. A client
that cannot reach the platform directly reaches it through an extender. An extender registers itself
(`POST /network/extender-activate` with its client JWT). The role is on by default for desktop providers,
and the miner runs it on one slot per run. The operator probes an extender back before storing it
(`EXTENDER.md` C2a: today a public TCP challenge and a verified request through it), signs its record with
the operator's root key, and challenges it every five minutes after that (C3: six failures in a row
deactivate an address). The record names the network that first stored its key and the provider client of
that network that activated it last. Extenders the operator runs on its own account networks form the
*gated* tier (`directory.durable_network_ids`); every other extender is a volunteer in the *open* tier.

Today an extender earns pool weight only as a **contract party** (`EXTENDER.md` J1–J3). When a contract is
created, every active extender on either endpoint's connected connections joins the contract's
participants and takes one equal hop share, like an intermediary or the egress.

#### 8.6.1 The problem

A contract's completed bytes are credited to its **participants**: the service endpoint (the egress), the
intermediaries of its stream and its extender parties, never the paying origin. They are split evenly into
the contract's immutable usage snapshot, which the epoch's provider rows sum (§8.2). Traffic between a
client and the operator's own platform has no such participant: the service side is the operator, which is
not a miner in its own pool, and a directly connected client has no extender party.

Such traffic is either not contracted at all (api calls, control messages, platform storage), or, if it is
contracted, the current rules mishandle it in one of two ways:

- **Misattributed.** A platform client on the service end, such as a sidecar edge (§8.6.6), is listed as
  the contract's egress participant, so the operator's own network is credited as a provider in its own
  pool.
- **Unsettled.** If the platform end is not counted and no participant is left, the contract cannot settle.
  The usage snapshot refuses positive bytes with no provider, ordinary settlement fails, and the deadline
  close retires the contract as `expired_unconfirmed` with zero credited bytes and no charge.

Uncontracted or unsettled, the weight is **dropped**: no miner is credited, and the pool's funding follows
the providers' eligible usage as if the traffic had not happened.

**Same‑network traffic** is the other case that no third party serves. A contract between two clients of
one network counts today as provider usage: its receiving client is the egress participant and earns pool
weight like any provider (§8.2), though no one outside the network served it. Until 2026‑11‑11 it was
also free: the server created it without escrow, so it charged no balance and drew on no data allowance,
yet it still settled with a usage snapshot that credits its receiving client. Since the billing change of
that day (§8.6.7) it escrows from the sender's data balance like other traffic.

That under‑credits what extenders do. They keep the platform reachable everywhere, for every client,
whether or not a given client went through one. And as products such as **sidecar** (§8.6.6) move work
onto the platform itself, the share of real usage with no provider participant grows.

#### 8.6.2 The rule

For NO `n` and every covered epoch `k ≥ E_x`:

```
P_c       =  c's participants, each distinct client once, less the origin: its service endpoint (the egress), its
               stream's intermediaries and its extender parties, platform endpoints and same-network ones included
s_{c,i}   =  floor(b_c / n_c) + (1 if i < b_c mod n_c, else 0)    for the i-th of P_c by client_id, n_c = |P_c|
                                                                  (Σ_i s_{c,i} = b_c; an extender is one of n_c, Q7)
z_c       =  the part of contract c's completed bytes b_c that no provider earns: the shares s_{c,i} held by
               n's platform endpoints                                                       (intra-platform)
               and by c's participants on the origin's network                              (intra-network)
             every other share, an extender party's included, is provider usage (§8.2)
Z_{n,k}   =  Σ z_c over n's funded contracts c that closed in k's earning window, free and paid alike (pool bytes)
Z'        =  min(Z, ζ · W_k),   ζ = 1        W_k = the epoch's eligible provider usage, after the rule below  (cap, M3)
U_{n,k}   =  the units holding at least one of n's eligible extenders for k, by unit hash ascending;  M = |U_{n,k}|
z_j       =  floor(Z' / M) + (1 if j < Z' mod M, else 0)      for the j-th unit, j = 0 … M−1         (Σ z_j = Z')
s_{j,i}   =  floor(z_j / m_j) + (1 if i < z_j mod m_j, else 0)
                                  for the i-th of unit j's m_j eligible extenders, by provider client_id  (Σ_i s_{j,i} = z_j)
row_{j,i} =  (client_id, coldkey, usage_bytes = s_{j,i}, head_excluded = 0)    (extender share row; none where s = 0)
```

The epoch then allocates its amount `A` (§8.2) with `coldkey_usage_largest_remainder_rao_v1`, unchanged,
over its provider rows **and** its extender share rows. Each unit carries `Z'/M` bytes of usage weight
beside the providers' settled bytes, split evenly among the eligible extenders in it. `ΣW` grows by `Z'`,
and an extender's coldkey gets `δ ≈ A · s_{j,i} / ΣW`, summed with any provider usage the same coldkey has.
Where every unit holds one extender, this is exactly the even split the owner asked for. Extenders that
share a block share its unit. Because `Z' ≤ W_k`, extenders take at most half a pool: every miner can be
both a provider and an extender (owner decision). Bytes above the cap enter no weight.

**Unit.** A unit is a keyed /29 IPv4 or /48 IPv6 prefix hash: the scoring unit of routable‑IP breadth
(§8.4), computed with the same key, domain and granularity. The signed policy sets that granularity once
for both (§15.2), so the two cannot drift. Each eligible extender belongs to exactly one unit: its IPv4
address's /29 when that address is active, else its IPv6 address's /48. This mirrors a provider's head
score, which pins its IPv4 hash first and is never doubled by a second family (`VALIDATOR.md` §8.1).
"Active" here is the test of E1.

**Intra‑platform contract.** A contract of NO `n` is intra‑platform when its service endpoint is one of
`n`'s registered **platform endpoints**: a client identity the operator itself runs to serve a platform
feature. The operator registers it, and it is recorded on the contract when the contract is created, never
inferred from a missing participant. At `E_x` the platform endpoints are sidecar's edge platform clients:
tlsedge, smtpedge and mxedge (§8.6.6). A platform endpoint is one of the contract's `n_c` hops, but it earns
no share of any contract: its share is intra‑platform bytes in `z_c`, even when the origin is on its network.
When it is the contract's only hop, that share is all of `b_c`.

**Extenders count as one of `n_c` (Q7).** The owner decided on 2026‑11‑11: "extenders count for one of n in
payout weighting for contracts". From `E_x`, on every contract, each extender party is one of the contract's
`n_c` hops and keeps its share `s_{c,i}` as provider usage, exactly as it does today (J3), unless it is on the
origin's network (below).

- **How `n_c` is counted.** `P_c` is the usual gathering: the service endpoint, the stream's intermediaries
  and the extender parties of both endpoints, less the origin. Each distinct client counts once. An extender
  counts as its provider client, so an extender whose provider client is already a hop, or a second extender
  record of the same provider client, adds no hop (J3). Platform endpoints and participants on the origin's
  network count like any other hop; only their shares go to `z_c`.
- **Extender‑carried platform contracts.** A platform contract whose client is connected through an extender
  has that extender as a party (J2). The extender keeps its one share of `n_c`, and the platform endpoint's
  share joins `Z`. Such a contract neither stays whole with its extender nor moves whole into the even split:
  with one extender and one platform endpoint, each holds half of `b_c` (the remainder byte going to the lower
  client id), and the platform endpoint's half is intra‑platform.
- **No byte is counted twice.** The amendment never re‑splits a contract. It takes the even shares that
  the contract's usage snapshot already holds and sends each share to exactly one place: a provider row, or
  `z_c`. Per contract, the provider shares and `z_c` sum to `b_c`. So no byte is in both the provider rows and
  `Z`, and `total_usage_bytes`, their sum (§8.6.4), counts each credited byte at most once: a `z_c` that `Z`
  leaves out, unfunded or measurement, enters neither. An extender can hold a hop share of a contract, in its
  provider row, and a share row of `Z'` in the same epoch. Those are different bytes: its own hop shares never
  enter `Z`, and `Z` holds only shares that no provider holds.
- **From `E_x`, with the rest of the amendment.** Before `E_x` nothing changes: §8.2 counts every hop share as
  provider usage, as today. Which shares are an extender's, a platform endpoint's or on the origin's network
  is operator‑attested, like the rest of the classification (§8.6.4).

**Intra‑network shares: an amendment to §8.2 from `E_x`.** No client is a paid provider for the clients of
its own network. On every contract, in any product, the even shares that the split gives to participants on
the origin's network are **not provider usage** from `E_x`. Those participants are the receiving client of a
same‑network contract, and any same‑network intermediary or extender on any contract. Their shares leave
the provider rows and become intra‑network bytes in `z_c`. The other participants' shares do not change: the
split still counts every hop, and no same‑network share passes to another participant. (The server's billing
split, `allocateContractParticipantPayouts`, also leaves these shares out, but it is customer‑billing
bookkeeping, which the SN ledger ignores.) Until `E_x`, §8.2 counts these participants as providers, their
bytes being settled payout bytes like any other; from `E_x` it does not.

**What counts toward `Z`.** A contract counts when it closed in the earning window with a credited outcome
(`settled`, `dispute_resolved_to_source` or `dispute_resolved_to_destination`). Its `b_c` is the completed
usage that every contract's credit already uses: the lower of the two completed reports, capped by the
contract's capacity, or the report a dispute resolved to. Then:

- **Funded by the payer's data balance, free or paid alike** (owner decisions).
  - Every intra‑network and intra‑platform contract escrows from the payer network's data balance, the free
    plan's included, exactly like other traffic.
  - Same‑network contracts therefore stopped being free when the billing change went live on 2026‑11‑11,
    before `E_x` (§8.6.7).
  - A free‑plan byte counts toward `Z` exactly as it counts toward provider usage (§8.2).
  - A contract that no balance funded, such as one made by the degradation valve's zero‑escrow fallback for an
    exhausted payer, adds nothing to `Z`.
  - So in each epoch a network adds to `Z` at most what its balance funded.
- **Never the operator's own measurement or test traffic.** A contract whose payer network the operator
  declares for measurement or testing never counts toward `Z`: validators' trail clients, the qualityprobe,
  the operator's probes, the proxy‑test account. The exclusion is from the extender pool only. Providers
  that carry this traffic keep earning from it as provider usage under §8.2.
- **Contracts only.** Uncontracted api, push and storage traffic stays out at launch. Storage is the first
  candidate to add, once its usage records are published each epoch.

Each contract's usage snapshot records the split: its providers (the participants that are neither platform
endpoints nor on the origin's network, extender parties included, with their shares), its intra‑platform and
intra‑network bytes `z_c`, the origin's network, whether a balance funded it, and whether its payer is a
declared measurement network. This is a new snapshot version: today a snapshot cannot carry bytes without a
provider.

**Eligible extender.** An extender `x` of NO `n` is eligible for epoch `k` when all of these hold:

- **(E1) In the active pool at `end(k)`** (owner decision: "if the extender is in the active pool it is
  counted").
  - At the epoch's end boundary (the timestamp of its end block, which the ledger already pins), the
    extender's record, signed by `n`'s root key, is active and unrevoked: `network_extender.active` with no
    `revoke_time`.
  - At least one of its addresses is active, that is, not deactivated by the five‑minute challenge (C3: six
    failures in a row).
  - The census captures this once, at the boundary, as the users count already is. There is no minimum
    age and no uptime threshold.
  - *Why the end boundary:* one snapshot at an instant the ledger already pins is reproducible and needs no
    history. "Active at any point in `k`" would count a single drive‑by activation, and would have to be
    rebuilt from activation and revocation records.
  - *The trade‑off:* an extender that is up only around `end(k)` counts for the whole epoch. Activation is
    self‑service and the boundary is predictable, so this is cheap to game. The gain is limited by the
    units (a block counts once, however many hosts appear in it) and by the cap.
- (E2, the first draft's hourly forwarding record, is removed by the same decision.)
- **(E3) Distinct.** One record per provider client and per address: records that share a provider client,
  an IPv4 address or an IPv6 /64 count once, for the lowest client id. Each eligible extender then belongs
  to one unit (above).
- **(E4) Payable.** Its provider client (`network_extender.client_id` at `end(k)`) has a payout coldkey under
  the wallet selection that provider rows use (§8.2: the client's own consent, else its network's, else a
  hotkey delegation). It has no active fleet binding at either boundary, because a head client earns in one
  tier only (§8.4). The share accrues to that coldkey's v2 account in `n`'s pool. An extender without a
  wallet is not counted in `N`, so no share is stranded on it.
- **(E5) Independent** (confirmed by the owner). Its network is not one of `n`'s own: not in `n`'s gated,
  durable fleet (`durable_network_ids`), not a canary, and not any other network `n` declares it controls.
  This is the extender analogue of J3's rule that nothing on the payer's network earns: the NO does not pay
  itself.

**Per pool.** Each NO's pool spreads its own `Z` over the extenders in that NO's active pool. Nothing moves
between pools, and there is no global root (§11.1). An extender forwards only to its own operator's
hosts and holds a separate record with each operator, so a host that serves two NOs counts as two
extenders, one in each pool, paid through two accounts.

**Rounding and the empty set.** Bytes split twice, first over units and then within each unit, by the same
rule at each level: the floor, with the remainder going to the lowest keys (unit hashes, then client ids).
This is the rule a contract already uses to split its bytes across its participants
(`evenContractPayoutShare`). α splits by the unchanged largest‑remainder rule, so `Σ δ = A` exactly. With
`N = 0` (no eligible extender, and so no unit), `Z` is dropped: it enters no weight, and the epoch allocates
exactly as it does today. If the epoch also has no eligible provider usage, §8.3's carry applies.

**How it fits the ledger.** Leaves do not change. Accounts stay `(coldkey, cumulative)` and are appended
and credited as §8.3 says, so an extender whose coldkey already holds an account (because it also runs
providers) adds to that account. What changes is one per‑epoch component of the ledger header and of the
usage statement (§8.6.4):

```
header (urnetwork-payout-ledger-v3; epochs k ≥ E_x):
  {"epoch":K,"providers":[…],"extenders":{"bytes":Z,"census":"sha256:…","rows":["sha256:…",…]}}
extender share rows: 57-byte rows exactly as provider rows (client_id ‖ coldkey ‖ usage_bytes ‖ head_excluded),
                     sorted by client_id, one per eligible extender with s_{j,i} > 0, usage_bytes = s_{j,i};
                     "rows": [] when N = 0 or Z = 0
census:              one row per extender of n known in epoch k, eligible or not, sorted by client_id:
                     extender_id ‖ provider client_id ‖ coldkey ‖ unit (keyed /29 or /48 prefix hash) ‖ unit family ‖
                     flags (active at end(k), gated, canary, declared own, revoked, duplicate, head-bound);
                     captured at the end boundary, operator-attested, never with an address
```

Release 1.0's `share_bps` leaves are not involved, because `E_x ≥ E_s`.

#### 8.6.3 Incentives and attacks

**Sybil: an even split pays per unit.** Whoever holds `k` of the `M` units takes about `k/M` of `Z'`.
Because a unit is a /29 or /48 block, packing extenders into one block gains nothing: they share its unit.
What still pays is spreading extenders across blocks, and a cloud fleet does that cheaply, since a hosting
provider's address space spans many /29s. An extender costs a host with a public address on port 443 that
passes the probe, which a small cloud VM provides. So as `Z` grows, the cheapest way to earn is to add
hosts in new blocks rather than to serve clients. Activation budgets limit activations per hour, not hosts,
and the decided rule adds no minimum age or uptime threshold (E1). The options:

- (S1, eligibility costs such as a minimum age or an uptime record, is removed: E1 is the active pool.)
- **(S2) Coarser units** (*option*). A unit per origin ASN would collapse a cloud fleet spread across
  blocks, but needs an ASN table that validators can reproduce. A coarser prefix would penalize honest
  neighbours, and would part from §8.4's unit.
- **(S3) Per‑operator limits** are **out**, by owner decision. A cap or a square‑root weight per payout
  coldkey or network is evaded by splitting, because wallets and networks are free to create.
- **(S4) Weighting in place of a pure even split.**
  - (a) *By use:* `Z` in proportion to each extender's J3‑credited bytes in the epoch. Fake extenders
    attract no clients, but this concentrates on the few extenders clients happen to pick and pays nothing
    for standby reach.
  - (b) *By measured capacity or quality*, such as forwarding success or latency. A Sybil that is a real
    host still passes.
  - (c) *By need:* weight units that serve regions where direct reach fails. This favors censorship‑heavy
    regions, but needs location data that validators cannot check.
  - A hybrid, half even per unit and half by use, keeps paying for standby reach and blunts Sybils.
  - Both use and capacity would be operator‑measured, like the census.
- (S5, validator sampling of extenders, is removed: validators do not check extenders, §8.6.4.)

*As decided:* E1 and E3–E5 over /29 and /48 units, with the cap M3 (`ζ = 1`). S3 stays out. ASN units (S2)
and the weightings of S4 are later options, by signed entry (§8.6.7), if one operator's share of units keeps
growing.

**Manufactured traffic.** Every intra‑network and intra‑platform contract now draws on the payer network's
data balance (owner decision), so manufacturing `Z` takes funded balance. Free and paid balance count alike,
so free networks can still raise `Z`, each by at most what its plan funds. At today's allowances that is:

- **Free:** 30 GiB a day, at most 210 GiB an epoch.
- **Pro:** 10 TiB a month, granted at the start of the month. That is about 2.6 TB an epoch on average,
  though one network can spend its whole month's grant in a single epoch.

The cheapest route is same‑network traffic between a network's own devices, spending its allowance.
Sidecar's edges, the platform endpoints at `E_x`, are a second route, drawing on the same balance.

**How much this bounds the attack:**

- **Who gains.** `Z` is spread over every unit, so a manufacturer who holds `k` of the `M` units gains only
  about `k/M` of the weight it adds. The rest goes to everyone else's extenders. So manufacturing pays in
  proportion to the share of units already held: the Sybil and manufacturing attacks compound.
- **What reaching the cap takes.** At epoch 4's scale, `Z'` reaches the cap at its 19.01 TB of eligible
  provider usage.
  - About 85 free networks, each spending its whole daily 30 GiB on same‑network or platform traffic every
    day of the epoch, reach it.
  - So do about 8 Pro networks sustained epoch after epoch, roughly $40 a month.
  - One Pro network's whole month, spent in one epoch, reaches 57.8% of it.
  - Without the balance rule, one well‑connected pair of hosts would have been enough, because same‑network
    contracts were free.
- **Compared with the rules before 2026‑11‑11, for a same‑network self‑dealer, it narrows.** Then its own
  provider earned full credit for those bytes, and no balance was spent (§8.2, §8.6.1). Since the billing
  change the bytes cost allowance. From `E_x` the provider also earns nothing for them, and the self‑dealer
  gets back only `k/M` through `Z`.
- **The unmetered route to provider credit is closed.** Until 2026‑11‑11, contracts to a provider in
  friends‑and‑family mode were created without escrow, like same‑network contracts, so no balance limited
  them. Since the billing change they reserve escrow from the sender's (dialing network's) transfer balance,
  as public contracts do (owner decision). That is the only change. Their bytes count as provider usage like
  all other traffic, paid or free (§8.2), and the sender's payment status changes no provider's credit.
  Routing bytes through one's own provider on a second network still earns provider credit for all of them,
  but the sender's balance now pays for them, as on any cross‑network contract.

**Accepted risk (owner decision).** All of a network's traffic shares its one transfer balance, and there is
no separate per‑network limit on `Z`. So tens of funded networks, or a few Pro subscriptions, can move up to
half of a pool from every provider to the extenders. Against that risk, the cap (M3) stops the extenders at
half of every pool, and publication (M4) shows the jump and which payer networks drive it. The units, E5, and
the deposit once a price is in force (M1) limit who collects it and what it costs the operator. None of this
is a better route for any one attacker than the funded cross‑network route that already exists.

**What bounds it:**

- **(M1) The deposit.** `Z` counts in `total_usage_bytes` (owner decision), so at a nonzero price the NO
  deposits for every byte of it, free bytes included, as for any usage (§7.1). Manufactured `Z` then costs the
  NO as well. At zero price, the launch mode, M1 does nothing.
- **(M2) Funded bytes only** (owner decision; this replaces the withdrawn paid‑only rule). A network adds to
  `Z` only what its own data balance funded, free plan or paid, and unfunded contracts add nothing.
- **(M3) A cap on `Z`'s weight** (owner decision). `Z` enters the allocation as at most the epoch's eligible
  provider usage (`ζ = 1`), so extenders take at most half a pool, however much is manufactured.
- **The units.** Only spreading across /29 and /48 blocks raises a manufacturer's share.
- **E5** keeps the NO's declared fleet from collecting it.
- **(M4) Audit.** The statement publishes `Z` split into its intra‑platform and intra‑network parts, with the
  number of contracts and of payer networks behind it. A jump, or one network driving most of `Z`, is visible
  to validators and watchers.

With one operator at zero price the pool's weight cannot rise (implied demand is 1, §7.1). Manufactured `Z`
therefore only moves funding from providers to extenders within the pool, and M3 stops that at half.

**Collusion between an NO and extenders.** The NO attests everything the extender pool rests on:

- `Z`, and with it which contracts are intra‑platform, intra‑network, funded or measurement;
- the active pool at `end(k)`;
- the extenders' wallets;
- its own declared networks.

No validator can check any of it. The extender directory is built so that no one learns every extender's
address at once, and so the census cannot be sampled (§8.6.4). An NO could therefore inflate `Z`, count
unfunded or measurement traffic, list extenders that are down, omit independent ones, or run its own
extenders under networks it does not declare. Each of these moves pool funding to extenders it chooses.

What remains to bound it:

- **E5** keeps the NO's *declared* fleet out. Undeclared networks are not caught.
- **The cap** (M3, `ζ = 1`) keeps extenders, honest or not, to at most half of each epoch's pool funding.
- **M2** ties `Z` to balances the NO's own billing debited. That is visible to the NO's customers, but it is
  still attested to everyone else.
- **M1** makes inflated `Z` cost a deposit, once the price is nonzero.
- **Publication** puts the census and `Z`, with its parts, contract counts and payer counts, in the epoch's
  evidence. The census uses keyed prefix hashes, never addresses. Anyone can see the totals,
  how concentrated the units and coldkeys are, and jumps between epochs, but not whether any one entry is
  true.

This is the same class of trust as §12.3's usage fraud on new funding: the NO already attests the provider
population and its usage, so it could already misallocate all of a root's new funding. The extender pool
adds an input that is less visible still, since no validator observes extenders at all. While one
organisation is owner, operator and validator, publication guards against bugs and compromised keys, not
against the owner. The extender pool should be reviewed before other operators are admitted, as §12.3
already requires for the deposit audit.

**Other effects.**

- **Same‑network earners lose that credit.** From `E_x`, a provider earns nothing for the bytes it serves to
  clients of its own network. Those bytes go to the extenders through `Z` instead, free or paid. Its
  cross‑network work earns as before.
- J2 is fuzzy by design. Every active extender on an endpoint's connections is a party, so the extender a
  client is attached to earns one hop share of each of that client's contracts, platform contracts
  included, whether or not their bytes crossed it.
- **Boundary gaming.** An extender that is up only around `end(k)` counts for the whole epoch (E1). It only
  has to pass an activation probe shortly before the boundary. An address stays active until six challenges
  in a row fail, about half an hour, so a host that activated within that half hour counts even if it is
  already down at `end(k)`.
- Revocation loops inflate nothing: the census is one snapshot, so an extender counts once however often it
  re‑activates.
- The census carries keyed prefix hashes, never addresses. Gated (operator) extenders are excluded (E5), so
  no hidden address needs to be published.

#### 8.6.4 Verification and evidence

**The ledger audit** gains one check, run after head exclusion (check 6) and before allocation (check 7)
in each covered epoch `k ≥ E_x`:

1. The header's `extenders.bytes` equals the statement's `extender_pool_bytes` (`Z`) for the epoch. The
   share rows sum to `Z' = min(Z, W_k)`, with `W_k` taken from the epoch's own eligible provider rows, or
   to 0 when `N = 0`.
2. The census is well formed: chunk hashes match, rows are sorted and unique, and its head flags agree with
   `bindingAt` at both boundaries, as in check 6.
3. The eligible set and its units are exactly the census rows that pass E1 and E3–E5 by the census's own
   flags, grouped by their unit hashes. The share rows are the two‑level even split of `Z'` over them, and
   each row carries the coldkey its census row names.
4. The share rows then enter check 7's allocation like provider rows.

**What validators cannot check** (owner decision). Validators do not verify extenders. The directory is
built so that no one learns every extender's address at once: tiers, partitioned directories and rationed
releases (`connect/EXTENDER.md` R). So a validator cannot sample the pool, and a census of addresses would
undo that design.

The census is therefore published as keyed prefix hashes, never addresses. Several inputs stay
operator‑attested:

- membership of the active pool;
- the classification of contracts as intra‑platform, intra‑network, funded or measurement, and of their hop
  shares as an extender's, a platform endpoint's or on the origin's network;
- the wallets;
- the declared networks.

Validators recompute the arithmetic only, plus the head flags they can read from chain. As with provider
rows, a component whose arithmetic fails is refused, and is rebuilt only on the operator's explicit command
naming the epoch.

**Evidence in the usage statement.** Schema `urnetwork-epoch-usage-v2`, kind `epoch-usage-v2`, holds every
v1 field plus:

- `intra_platform_bytes` and `intra_network_bytes`, and their sum `extender_pool_bytes` (`Z`);
- `extender_pool_weight_bytes` (`Z'`, after the cap);
- `extender_pool_contracts` and `extender_pool_payers`, the number of payer networks (M4);
- `extender_census` (a reference), `extender_units` (`M`) and `eligible_extenders` (`N`);
- `extender_chunks` (`[]` when `N = 0`).

`total_usage_bytes` becomes the sum of the provider rows plus all of `Z`, not only `Z'` (owner decision). The
deposit prices the extender pool's usage, and at a nonzero price it raises implied usage. Validators check
both sums. Two statements for the same chunks with different `Z` are equivocation, as today.

**Fixed inputs.** An epoch's extender component is built once, at the first root that covers it, like its
provider rows (§8.3). A rebuild needs the same explicit operator command, which the validator accepts only
for an epoch whose component it refused.

#### 8.6.5 Worked example

One epoch of one pool with `A = 1,000 α = 10^12 rao`:

- **Provider usage by coldkey:**
  - `a` 6.0 TB and `b` 2.5 TB, all of it cross‑network;
  - `c` 0.5 TB cross‑network, plus 0.4 TB served to clients of its own network, on free or paid contracts;
  - a provider without a wallet has 1.0 TB; its row is kept but is not eligible.
- **Extender pool bytes:** from `E_x`, `c`'s 0.4 TB is intra‑network. With 600,000,000,001 bytes of
  intra‑platform traffic, `Z = 1,000,000,000,001`. That is below the cap of `W_k = 9.0 TB`, so
  `Z' = Z`.
- **The census:**
  - `x1` and `x2` are in the active pool at `end(k)` in one /29 block, unit `u1`. `x3`, also active, is
    alone in another block, unit `u2`, whose hash sorts after `u1`'s.
  - `x2` is paid to coldkey `b`, which also runs providers.
  - `x4` has no wallet (E4), so it is not counted.
  - So there are `M = 2` units and `N = 3` eligible extenders.

The split runs at two levels:

- **Over units:** `z_1 = 500,000,000,001`, since `u1`, with the lower hash, takes the remainder byte; and
  `z_2 = 500,000,000,000`.
- **Within `u1`:** `x1` gets 250,000,000,001, taking the remainder byte as the lower client id, and `x2`
  gets 250,000,000,000.
- **Within `u2`:** `x3` gets 500,000,000,000.

The weights total `ΣW = 9,000,000,000,000 + 1,000,000,000,001 = 10,000,000,000,001`.

| Coldkey | `W_c` (bytes) | `δ_c` (rao) | `δ_c` (α) | Today (α) |
|---|---:|---:|---:|---:|
| `a` | 6,000,000,000,000 | 600,000,000,000 | 600.000000000 | 638.297872340 |
| `b` (provider and `x2`) | 2,750,000,000,000 | 275,000,000,000 | 275.000000000 | 265.957446809 |
| `c` | 500,000,000,000 | 50,000,000,000 | 50.000000000 | 95.744680851 |
| `d` (`x1`) | 250,000,000,001 | 25,000,000,000 | 25.000000000 | — |
| `e` (`x3`) | 500,000,000,000 | 50,000,000,000 | 50.000000000 | — |
| **Σ** | 10,000,000,000,001 | 1,000,000,000,000 | 1,000 | 1,000 |

"Today" counts `c`'s same‑network 0.4 TB as provider usage, for a `ΣW` of 9.4 TB, and drops the
intra‑platform bytes. Alone in its block, `x3` gets twice what `x1` and `x2` each get.

The leaves: `a`, `b` and `c` keep their positions and add their `δ`, and `d` and `e` become new accounts,
appended in ascending coldkey order. Say the previous cumulatives were `a` 12.345678901 α, `b` 2 α and `c`
0.5 α. The root's leaves are then `(a, 612.345678901)`, `(b, 277)`, `(c, 50.5)`, `(d, 25)` and `(e, 50)` α.
Their total, 1,014.845678901 α, must equal `fundedThrough[k]` (§8.3).

**At scale, from the public epoch 4 ledger.**

- *The epoch:* `f_4 = 1,195.838935717 α`. Its 1,048,456 provider rows carry 31.38 TB; the 416,607
  eligible rows, across 90 coldkeys, carry 19,010,949,105,496 bytes.
- *A small `Z`:* had `Z` been 5% of the eligible usage, extenders would have shared 56.94 α, 4.76% of the
  pool: 0.57 α per unit at `M = 100`, or 0.28 α per unit at `M = 200`.
- *A large `Z`:* had `Z` equalled the eligible usage, they would have shared 597.92 α, half the pool. That
  is the most the cap (`ζ = 1`) allows; any larger `Z` is cut back to it.
- *What it depends on:* the extenders' share is `Z' / (W_eligible + Z')`. It grows with the ratio of the
  extender pool's bytes to eligible provider usage. Units and `N` only divide it, which is why M3 bounds the
  ratio.

**Hop shares (Q7).** Three contracts close in an epoch `k ≥ E_x`. Their hops are listed in client id order.
`x` is an extender party on a third network, `t` is a tlsedge platform endpoint, and `r` is a client on the
origin's network.

| Contract | `b_c` (bytes) | Hops (`n_c`) | Provider usage (bytes) | `z_c` (bytes) |
|---|---:|---|---|---:|
| `c1`, ordinary | 3,000,000,002 | egress `p`, intermediary `q`, extender `x` (3) | `p` 1,000,000,001; `q` 1,000,000,001; `x` 1,000,000,000 | 0 |
| `c2`, platform | 3,000,000,001 | `t`, extender `x` (2) | `x` 1,500,000,000 | 1,500,000,001, intra‑platform |
| `c3`, same‑network | 1,000,000,001 | receiving client `r`, extender `x` (2) | `x` 500,000,000 | 500,000,001, intra‑network |
| **Σ** | 7,000,000,004 | | 5,000,000,002 | 2,000,000,002 |

- `c1` is `k = 2` providers plus an extender, split into `k + 1 = 3` equal shares, with the two remainder
  bytes going to the lowest client ids. `E_x` changes nothing here.
- In `c2`, `x` keeps one share of two, and `t`'s share joins `Z`. The two alternatives that Q7 weighed would
  have given `x` all 3,000,000,001 bytes (the contract staying whole with its extender) or none of them (the
  contract moving whole into the even split).
- In every row, the provider usage and `z_c` sum to `b_c`. `x`'s provider row gets its three hop shares,
  3,000,000,000 bytes. If `x` is also an eligible extender, its share row adds its unit's part of `Z'`, and
  `Z` holds the shares of `t` and `r`, never `x`'s own.
- Before `E_x`, every share is provider usage, `t`'s and `r`'s included: 7,000,000,004 bytes of provider rows,
  and no `Z`.

#### 8.6.6 Sidecar

Sidecar, the end‑to‑end encrypted substrate under recURd and burner email, is in development on the
`sidecar` branches and has not shipped (`sidecar/DESIGN.md`). Nearly all of its traffic runs between a
customer's devices and the operator's own services:

- **No contract (today's design):** control traffic, storage, keys, sockets, DNS and certificates go over
  HTTPS to platform hosts, and pushes travel as control frames or a long‑poll.
- **Service‑mode contracts** connect a customer sidecar to the operator's edges (TLS, SMTP and MX). One end
  of each is a platform client in the operator's edge network. The design has them carry no IP and escrow
  nothing.
- **Relayed contracts** carry peer data between a customer's own sidecars, relay‑only through the
  platform's connect relays and never peer to peer. Both ends are on one network, so from `E_x` their bytes
  are intra‑network (§8.6.2).

**The transfer client is the base of every sidecar feature** (owner decision). So sidecar's traffic is
ordinary contracts, which escrow from the payer network's data balance and settle exactly as other contracts
do. That holds for edge and relayed contracts alike, and it replaces the design's separate no‑escrow service
debits. What sidecar moves onto contracts with the platform's endpoints is intra‑platform. What still goes
over plain HTTPS stays out of `Z` until it is contracted or its usage records are published (§8.6.2).
- **No egress by default:** a sidecar declares no egress, is refused public provider contracts, and agents
  leave only through exits the customer runs. Its required hosts are the operator's own api, auth, storage,
  key, connect, sockets and DNS hosts.

Sidecar therefore produces almost no provider participants. Unless a customer opts into an exit, an
extender is the only third party that can appear on a sidecar contract. Without §8.6, sidecar's volume
reaches the miner pool not at all (storage over HTTPS) or only as misattribution: an edge's platform client
would be listed as the egress participant (§8.6.1), or a customer's own receiving device would count as a
same‑network provider. Under §8.6, Service‑mode edge contracts are intra‑platform by their platform
endpoint, and relayed sidecar‑to‑sidecar contracts are intra‑network (§8.6.2). Both reach the extenders
through `Z`, free or paid, except the hop share that an extender party of the contract keeps (Q7).

**Extenders in sidecar** (owner decision). Sidecar uses connect's client strategy as it is, with its
existing extender fallback. Extenders are allowed by default, and a per‑customer switch turns them off. A
platform contract that an extender carried keeps crediting that extender one of its `n_c` hop shares (J3,
Q7), and the edge's share joins `Z`. A customer that turns extenders off reaches the platform directly, so
the whole of its platform contracts joins `Z`.

**Why extender availability matters globally.** All of sidecar's services sit at one site behind shared
load‑balancer addresses, and its design accepts that a block of those addresses also blocks api and connect
ingress. Its whole surface is therefore one set of addresses that a national firewall, an enterprise filter
or a blocklist can cut, and extenders are the path around such a cut:

- Each extender is another address, usually on another network, that forwards to exactly that surface.
- The platform transport and the api client run through extenders (`EXTENDER.md` §2), with the inner TLS end
  to end. Sidecar's data is end‑to‑end encrypted besides, so an extender sees only ciphertext addressed to
  platform hosts.
- The more networks, prefixes and countries the open tier spans, the harder it is for a censor to block all
  of it. Prefix units reward that spread, not the number of extenders packed into one block.

The expected traffic shape is a phone or agent host holding long‑lived connections (keepalives every 60 s,
contracts of up to an hour) with small frames, and storage segments of up to 8 MiB. Clients in
censorship‑heavy regions are expected to attach through extenders and pay them a hop share directly under
J3, the edge's share joining `Z`. Everyone else reaches the platform directly and, under §8.6, pays all
extenders evenly for keeping that path open.

#### 8.6.7 Changes needed elsewhere, effort and rollout

| Component | Change | Effort | Needed for |
|---|---|---|---|
| server: billing of network and friends‑and‑family contracts (owner decisions) | Network (same‑network) and friends‑and‑family contracts leave the no‑escrow path. Until 2026‑11‑11 the connect controller sent both provide modes to `createContractNoEscrowInTx`, with no `transfer_escrow` row, no `payer_network_id` and no balance change, settling through the no‑escrow close. Since the change both reserve escrow from the sender's (dialing network's) transfer balance, as public contracts do, and platform contracts escrow the same way. All of a network's traffic shares its one transfer balance, with no separate limit. The degradation valve's zero‑escrow fallback for exhausted balances stays as it is. | 2–4 days | live since 2026‑11‑11, before `E_x` (step 0) |
| server: same‑network rule (amends §8.2 from `E_x`) | The epoch reader classifies each contract from its live row (origin network, payer, escrow). It moves same‑network shares out of the provider rows, adds those of funded contracts to `Z`, free and paid alike, and skips unfunded contracts and declared measurement and test networks. It never re‑splits a contract: each share of the usage snapshot goes to one place, and an extender party's share on another network stays in the provider rows (Q7). The 35‑day contract horizon keeps every contract of an epoch live through its build, so settlement does not change. | 2–3 days | epoch 8 |
| server: extender census | Captured once at the end boundary, with the users count: the active pool (E1); units from the policy's egress‑prefix setting and key; E3 deduplication; E5 flags; the payout identity through the provider‑row wallet selection; and head flags. | 3–4 days | epoch 8 |
| server: epoch builder | `Z` and `Z'` per epoch, eligibility, the two‑level split, share rows, the census object, header v3 and statement v2. Metrics and alerts for `Z` and its parts, `M`, `N` and the extenders' share of the pool. | 4–5 days | epoch 8 |
| server: contracts | A registry of platform endpoints (sidecar's edges), the intra‑platform class set at contract creation, and usage snapshot v2 (providers, `z_c`, the origin's network, the funded and measurement flags). A platform endpoint stays one of the contract's `n_c` hops, and its share is `z_c` (Q7). Settlement and the deadline close accept the new snapshot. | 5–7 days | sidecar |
| sn: `ledger`, `protocol` | Header v3, the census and share‑row codecs, the two‑level split and the cap, the new Build and Verify check with fixed inputs, and GL‑style mutation tests, including million‑row fixtures. The server's builder uses this package, so it lands first. | 5–7 days | epoch 8 |
| sn: `demand/usage` | Statement v2, its JSON schema, and the deposit audit reading `Z` (dormant at zero price). | 1–2 days | epoch 8 |
| sn: the amendment entry | The signed line format, its file beside the price sheet, and the readers in the server, the validator and `snclaim`, as the price sheet's readers work (§7.1). | 1–2 days | epoch 8 |
| sn: validator and `snclaim` | Header v3 decoding, the arithmetic recompute (census form, head flags, units, split, cap), reading `E_x` from the amendment entry, and the approval path. `snclaim v2 check` gets the same audit through `ledger.Verify`. UR's watcher demands on any root it cannot rebuild, so it ships with the validator. | 3–5 days | epoch 8 |
| docs | This section made normative, with cross‑references in §7.1, §8.2, §11.1, §11.3, §12.3 and §15.2. §8.2 and `VERSION2-FINAL.md` §6.2 say that from `E_x` same‑network participants are not providers. §15.2 says one granularity sets both the head unit and the extender unit. The same‑network rule of `mainnet/PRELAUNCH-FIXES.md` is marked superseded for SN credit from `E_x`. Also the ledger check in `VALIDATOR.md`, an addendum to `VERSION2-FINAL.md`, the README glossary, `docs/spec/epoch-usage-v2.schema.json`, and `EXTENDER.md` J and §6. For the billing change: the server's comments on network and friends‑and‑family contracts as no‑escrow; the "no customer charge" note on same‑network contracts in `mainnet/PRELAUNCH-FIXES.md`; sidecar's no‑escrow service debits and Service mode in `sidecar/DESIGN.md`; and the apps' and support articles' description of what uses the data allowance. | 3–5 days | with each release |

The whole set is about 5.5–7.5 engineer‑weeks. The part needed for epoch 8 is about 4.5–5.5 engineer‑weeks:
11–16 days of server work and 10–16 of sn. The critical path runs through the sn `ledger` codec and then the server
builder, about 9–12 working days. Sidecar's per‑customer extender switch belongs to sidecar's own plan and
is not counted here. vault2 and `STSettlement2` do not change: they commit only `(root, total, leafCount,
ledgerHash)`.

**Rollout to `E_x = 8`.** The owner moved the start from epoch 6 to epoch 8, while a new validator release
goes out to partners.

*The dates,* at the measured 12.0 s per block:

| Epoch | Blocks | Ends about | Root committed by |
|---|---|---|---|
| 6 | 9,499,228–9,549,627 | 2026‑11‑20 18:28Z | block 9,550,828, about 22:28Z |
| 7 | 9,549,628–9,600,027 | 2026‑11‑27 18:29Z | block 9,601,228, about 22:29Z |
| 8 | 9,600,028–9,650,427 | 2026‑12‑04 18:30Z | block 9,651,628, about 22:30Z |
| 9 | 9,650,428–9,700,827 | 2026‑12‑11 18:30Z | block 9,702,028, about 22:30Z |

- Epoch 8's earning window opens at about 2026‑11‑27 18:29Z.
- Its statement follows its root commit on 12‑04.
- Its root must be approved before `end(9)`, about 2026‑12‑11 18:30Z.

*What epoch 8 carries.* Unless sidecar is live by then, epoch 8's `Z` comes from same‑network contracts
alone. By then the billing change funds them from their networks' balances, free or paid, so their shares
move from the provider rows into `Z`. They are rare, so `Z` will be small. The activation mainly moves
same‑network shares from providers to extenders, and publishes the first census.

*What has to ship, and by when* (the rows marked "epoch 8" above):

0. **The billing change: live since 2026‑11‑11** (owner decision). Server `586673c74` reached the network
   operator's api at 05:43Z and every service by 11:39Z.
   - Since the deploy, network and friends‑and‑family contracts reserve escrow from the sender's transfer
     balance, so every traffic kind draws on that one balance.
   - Contracts opened before the deploy close unfunded through the no‑escrow path.
   - Until `E_x`, nothing else changes in settlement credit. Same‑network bytes stay provider usage under
     §8.2, now paid for from the sender's balance. Friends‑and‑family bytes stay provider usage, before and
     after `E_x`, paid or free alike.
   - The user notice goes out with the change: device‑to‑device traffic between a network's own clients and
     providers, and traffic to friends‑and‑family providers, now use the sender's data allowance. Support
     answers say so from 2026‑11‑11, and the mainnet timeline and ur.io's FAQ with their next releases.
   - The extender pool still starts at epoch 8. By then every contract created since the deploy is funded,
     and the shadow runs of epochs 6 and 7 see real funded flags.
1. **By about 11‑16:** the sn `ledger` and `protocol` changes, which the server's builder imports.
2. **Before `end(6)`, about 11‑20 18:28Z:** the server's same‑network rule, census capture and builder, running
   in shadow so that epoch 6 is the first live shadow run (below).
3. **Before epoch 8 starts, about 11‑27 18:29Z:**
   - The amendment entry, signed and published. The owner signs it during epoch 7 at the latest, because the
     signing tool refuses an entry for an epoch that has started.
   - The notice on the mainnet timeline. The same‑network change applies to every contract that closes in
     epoch 8's window, and what some providers earn changes with it.
4. **By about 12‑01:** the validator and `snclaim` release with header v3, the recompute and the entry reader,
   deployed when the validator is idle. It follows the partner validator release that is going out now,
   rather than being folded into it.
5. **Before `end(8)`, about 12‑04 18:30Z:** the server's production build, with the census capture running
   at the boundary. Epoch 8's root, with header v3, is committed by about 22:30Z.

*The shadow runs.*

- **Epochs 6 and 7 run live in shadow.** After each boundary, the server builds the epoch's extender
  component from the real census capture, without committing it, and publishes it in an unsigned report. The
  validator's recompute runs on it.
- **An offline replay of epoch 4.** Its contracts are still live inside the 35‑day horizon. The replay checks
  that its rows under the new rule differ from the committed ones by exactly the same‑network shares.
- If the server misses `end(6)`, epoch 7 alone is the live shadow.

*What would move the start to epoch 9:*

- The entry is not signed before about 11‑27 18:29Z. The signing tool would then require epoch 9.
- The sn release is not deployed before epoch 8's root commit.
- The census capture is not running at `end(8)`.
- A shadow mismatch is not resolved.
- The notice is not out before epoch 8 starts.

Epoch 9's window runs from 12‑04 18:30Z to 12‑11 18:30Z.

*How `E_x` is fixed: by the signed amendment entry.*

- **Why.** Epoch 8 leaves about two weeks: enough to build the line format and its readers (one to two days
  of work) and for the owner to sign during epoch 7. The entry gives watchers a record they can check,
  which configuration alone does not. The same mechanism carries every later change, such as ASN units or
  storage usage.
- **Where.** It is published in its own file beside the price sheet, `amendments.yml`
  (`https://ur.xyz/amendments.yml`), signed with the price sheet's pinned key in the same plain‑text style
  (§7.1), so the deposit's price readers are untouched. The `amendment` package reads it in the server, the
  validator and `snclaim`, and `cli/amendment` signs it (`mainnet/SETTLEMENT-AMENDMENTS.md`). For example:

  ```
  UR settlement amendment v1: chain 964, sn 25, no 1, from epoch 8, intra-platform and intra-network extenders, units egress prefix, free and paid, zeta 1
  ```

- **The fallback.** Configuration (the server's `sn.yml` and the validator's adopted config) is used only if
  the signing cannot happen before epoch 8 starts.

*Every epoch from `E_x`:* the server emits header v3 and statement v2, and builds the provider rows without
same‑network shares. A root that covers epochs on both sides of `E_x` applies each epoch's own rule.

*Past epochs are not disputed.* Their rows are fixed inputs (check 3) and carry no extender component, so
nothing before `E_x` is rebuilt, reopened or open to challenge. Same‑network usage before `E_x` keeps the
credit it earned.

#### 8.6.8 Owner decisions and open questions

**Decided (owner, 2026‑11‑10, with follow‑ups at about 23:00Z, 23:10Z and 23:25Z, and Q7 on 2026‑11‑11).** The question
numbers are those of the first draft.

1. **The unit (Q3).**
   - /29 IPv4 and /48 IPv6 prefix units are the base rule from `E_x`, under the granularity setting of
     §8.4.
   - Each unit counts once, and its share is split evenly among the eligible extenders in it.
   - Per‑coldkey caps (S3) stay out.
2. **Free bytes count (Q2).** Free and paid bytes count toward `Z` exactly as they count toward provider
   usage, symmetrically everywhere. This reverses the earlier paid‑only decision. M2 now means funded bytes
   (decision 12).
3. **E5 (Q4).** The NO's own extenders are excluded: the gated fleet (`durable_network_ids`), canaries and
   declared networks.
4. **An empty set (Q6).** With `N = 0`, `Z` is dropped: it enters no weight, and the epoch allocates as it
   does today.
5. **Sidecar and extenders (Q11, first half).**
   - Sidecar uses connect's client strategy as it is, with its existing extender fallback.
   - Extenders are allowed by default, with a per‑customer switch to turn them off.
   - Platform contracts that an extender carried keep crediting that extender one of their `n_c` hop shares
     (J3, decision 16).
   - Connect's transfer client is the base of every sidecar feature, so sidecar's traffic is ordinary
     contracts that escrow and settle as all others do (decision 12).
6. **Which traffic counts (Q10, and Q11's second half).**
   - Platform endpoints are sidecar's edge platform clients (tlsedge, smtpedge and mxedge), registered by
     the operator.
   - No two clients on one network count as paid providers. Every same‑network contract, in any product, is
     intra‑network, and its bytes count toward `Z`. This amends §8.2 from `E_x`.
   - Uncontracted api, push and storage traffic stays out at launch. Storage is the first candidate to add,
     once its usage records are published each epoch.
   - The operator's own measurement and test traffic is excluded from the extender pool: probes, the
     qualityprobe and the proxy‑test account. Providers keep earning from it under §8.2.
7. **Pricing `Z` (Q1).** `Z` counts in `total_usage_bytes`, so the deposit prices it and, at a nonzero
   price, it raises implied usage. The owner expects extenders to be an important part of the operator's
   reachability.
8. **Liveness (Q5).** An extender in the active pool is counted, with no uptime threshold and no minimum
   age. The test is E1's: active at `end(k)`.
9. **The cap (Q8).** `ζ = 1`: `Z` enters as at most the epoch's eligible provider usage, so extenders take
   at most half a pool. The owner: "all miners can be both providers and extenders".
10. **Verification (Q9).** Validators do not check extenders: not every extender address can be revealed
    at once, by design. There is no REACH message and no sampling. The census is operator‑attested and
    published as keyed prefix hashes; validators recompute the arithmetic only.
11. **The start (Q12).** Epoch 8, while a new validator release goes out to partners. This replaces the
    earlier "epoch 6, ideally". The owner did not choose the mechanism separately. The draft's signed amendment
    entry, which epoch 8 leaves time for, fixes `E_x` (§8.6.7).
12. **The manufactured‑traffic bound (23:10Z).** Option A: a network's intra traffic counts only up to what
    its plan's data allowance funds.
    - All intra‑network and intra‑platform traffic counts against the network's data balance, free plan or
      paid, through ordinary escrow.
    - This ends the free no‑escrow treatment of same‑network contracts.
13. **One balance, no separate limit (23:25Z).** "All traffic shares the same network transfer balance." The
    only bound on a network's contribution to `Z` is its one transfer balance. The rest is the accepted risk
    of §8.6.3.
14. **Friends‑and‑family contracts escrow (23:25Z).** They reserve escrow from the sender's (dialing
    network's) transfer balance, as public contracts do, which closes the unmetered route to provider credit.
    Nothing else changes for them. As the owner confirmed on 2026‑11‑10, "all traffic counts as paid
    traffic": their bytes count as provider usage like any other, paid or free, and only the sender's
    balance now pays for the data.
15. **Billing as soon as possible (23:25Z).** The billing change goes live as soon as it deploys, before
    `E_x`. Contracts opened before the deploy close unfunded, and the extender pool still starts at epoch 8
    (§8.6.7). It went live on 2026‑11‑11 (§8.6.7, step 0).
16. **Extenders count as one of `n_c` (Q7, 2026‑11‑11).** The owner: "extenders count for one of n in payout
    weighting for contracts". Q7 asked whether extender‑carried platform contracts should stay with their
    extender (J3) or move into the even split. They do neither whole (§8.6.2):
    - On every contract, each extender party is one of the contract's `n_c` hops and keeps its even share as
      provider usage (J3), unless it is on the origin's network.
    - `n_c` counts every hop once, less the origin, platform endpoints and same‑network participants
      included. Their shares, and only theirs, are `z_c`, so a platform endpoint's share joins `Z` and no
      share passes to another hop.
    - No byte is credited twice: each share of a contract goes to a provider row or to `z_c`, never both.
    - It takes effect with the amendment, from `E_x = 8`. Before `E_x` nothing changes.

**Still open.** Nothing: Q7, the last open question, is decision 16.

### 8.7 Capture the flag — a per‑country coverage auction beside the provider pool (decided 2026‑11‑11)

> **Amendment decided by the owner on 2026‑11‑11.** Capture the flag (CTF) takes effect from epoch 8
> (`E_c = 8`), the extender pool's first epoch too, once the signed amendment entry v2 is published (§8.7.5).
> From `E_c` it amends §8.5's split and adds a second settlement v2 pool per network operator, with its own UID
> (§14). vault2, the provider pool's ledger and the extender pool do not change. This section summarizes the
> design, and `CAPTURETHEFLAG.md` gives it in full, with its formats, amounts, attacks, evidence and deployment
> plan. The contracts are in `VERSION2-FINAL.md` §19, and the public routes in `api/operator.yml`. The owner's
> decisions are listed in §8.7.6.

**Why.** The provider pool pays for usage (§8.2), the head for routable breadth (§8.4), and from epoch 8 the
extender pool spreads platform traffic evenly over prefix units (§8.6). None of them pays for being present in
a country. Usage concentrates where users are, and breadth and units reward many address blocks wherever they
sit. Censorship resistance, though, is decided country by country: a national firewall can cut the platform's
whole surface, and providers and extenders inside that country are the way around it (§8.6.6). On 2026‑11‑10
the network had providers in 109 countries, 74 of them with at least five. CTF pays a fixed bounty per country
and role, so the first miner to reach a thin country earns far more than one more miner in a dense one.

#### 8.7.1 The pool

- **Its id and UID.** NO `n`'s CTF pool is the vault2 pool `2^32 + n`, so UR's is **4,294,967,297**. The id
  sits above every v1 operator id and below 2^53, so JavaScript clients keep it exact, and vault2 itself does
  not change. `STSettlement2Ctf`, an upgrade of `STSettlement2`, registers it (`registerCtfPool`) under a hotkey
  derived at execution, and the registration burns one UID (§14).
- **Its funding.** It takes `s_ctf · μ · ŵ_n` (§8.5): a tenth of the providers' share, divided across NOs by
  each validator's own pool weights. No validator measures anything for it.
- **Its settlement.** It settles exactly like the provider pool (§8.3): a close within 1,200 blocks of the
  epoch's end, a root committed by NO `n`'s root signer, auditor approval before the next epoch ends, the
  14,400‑block window with its challenges and demands, and cumulative, permissionless claims that never expire.
  Its ledger keeps v2's accounts, sum tree and claim proofs, and lists each covered epoch's census, awards, pots
  and pending awards where the provider pool lists provider rows (`CAPTURETHEFLAG.md` §5).
- **The same registration.** A winner is paid to the coldkey that the provider rows' wallet selection gives its
  client: its own consent, else its network's, else a hotkey delegation (§8.2). Payout consents name the NO, not
  a vault2 pool, so one consent covers both of NO `n`'s pools and no miner signs anything new. The same coldkey
  then holds one account in each pool.

#### 8.7.2 Slots, eligibility and the draw

- **Ten slots per country.** Each of the 249 ISO 3166‑1 alpha‑2 codes of `server/model/iso_country_names.go`
  has 5 provider slots and 5 extender slots, so `S = 2,490`. Kosovo (XK) is not an ISO 3166‑1 code and has
  none. The seven territories without a permanent population (AQ, BV, GS, HM, IO, TF, UM) have slots like
  every other code. Each slot is worth the same amount: the CTF pool's fresh funding for the epoch divided by
  2,490, rounded down to the rao, with the remainder going one rao each to the first slots in slot order
  (country code, then provider before extender, then rank).
- **The operator nominates.** The NO lists, from its own data, the providers and extenders that were online,
  passing its checks and in one country for at least half of the epoch, and active at its end. The server
  samples every provider and extender each quarter hour, 672 times an epoch. A candidate needs half of the
  samples that the operator does not excuse for a platform‑wide outage it declares, so 336 when none is
  excused. Like the provider population and the extender census, the nomination is operator‑attested (§8.6.4).
  - **A provider** (P1–P6) is, at `end(k)`, an active, top‑level, connected provider with a Public provide key
    and a valid location that passes the operator's provider eligibility, and its published country is the
    country. At least half of the samples found it so. It is one candidate per IPv4 address or IPv6 /64 (E3),
    has a payout coldkey and no active fleet binding at either boundary (E4), and is not on one of the NO's own
    networks (E5).
  - **An extender** (X1–X6) meets §8.6.2's E1 at `end(k)`, and the country is its record's country then and at
    every activation in the epoch. At least half of the samples found it active there, with an address whose
    challenge passed, and it meets E3–E5.
- **One ticket per unit.** Candidates are grouped by the keyed /29 IPv4 or /48 IPv6 unit of §8.4 and §8.6.2.
  A unit holds one ticket per role, whatever its size, and all its members are equivalent. It enters one
  country's draw per role, the country of the lowest client id among its candidates left in the draw, once
  fleet‑bound ones are dropped. A miner may be a candidate in both roles and win both: every miner can be both
  a provider and an extender (§8.6.8, decision 9).
- **The census and its commitment.** Right after the end boundary, the server builds epoch `k`'s **census**:
  one row per candidate (role, its own country, unit, unit family, client id), sorted, with per‑country
  counts of the miners each rule excluded. It holds no coldkeys and no addresses. Within 600 blocks of
  `end(k)`, NO `n`'s root signer commits `censusHash = sha256(census header)` with
  `commitCtfCensus(k, n, censusHash)`, once per epoch, so the candidates are fixed on chain before any
  randomness for the draw exists. No census can be committed while the v1 coordinator is paused, so a pause
  across that window means no draw for the epoch.
- **The draw.** A drand round published after the commitment seeds it:

  ```
  ρ_k      = the first drand quicknet round whose time is ≥ ts(B_k) + 60 s        B_k = the commitment's block
  σ_k      = keccak256("urnetwork/ctf-draw/v1" ‖ chain_id ‖ netuid ‖ pool_id ‖ k ‖ censusHash ‖ rand_k)
  winners  = in each country and role, the min(5, units) units with the smallest keccak256(σ_k ‖ role ‖ country ‖ unit)
  member   = in each winning unit, the candidate with the smallest keccak256(σ_k ‖ role ‖ unit ‖ client_id)
  ```

  `rand_k` is the round's randomness, verified against quicknet's public key. Candidates with an active fleet
  binding at either boundary are left out first. Taking the smallest tickets draws a uniformly random set of
  up to five units. Block producers cannot bias drand, and a second commitment is refused. An operator that
  withholds its commitment gets no draw, and so does a committed census that is not canonical: everything then
  carries.

#### 8.7.3 Pots and the carry contract

- **Unfilled slots carry.** A slot with no winner adds its amount to its country's **pot** for that role.
- **A fifth of the pot per winner.** Each winner takes its own slot and a fifth of the pot carried into its
  country and role. The rest keeps carrying, with the epoch's unfilled slots added, and five winners empty the
  pot.
  - A lone miner in an unclaimed country takes a fifth of what is left each epoch: about 67% of the pot in five
    epochs and 89% in ten. If it stays alone, the pot settles at twenty slot amounts, and it takes five slot
    amounts each epoch.
  - Every newcomer who becomes eligible there takes its own fifth, so a pot never goes all at once to whoever
    arrives first.
- **Fresh amounts only.** Slot amounts come from the epoch's fresh funding. Returned carry pays only the pots
  and pending awards it is owed, never new slots. All amounts are exact integers in rao (`CAPTURETHEFLAG.md`
  §4.5, implemented by the package `ctf`).
- **The carry contract.** vault2 requires each root's total to equal the pool's funding through its epoch, so
  every captured rao sits in some account at every root, and a pot cannot wait inside the pool. Each root pays
  the pots and pending awards to the account of the **carry contract**, `STCtfCarry`: immutable, ownerless, one
  per CTF pool. Once the root finalizes, anyone calls `recycle`. It claims that account and moves the α
  straight back onto the CTF pool hotkey, where the next close captures it with the epoch's emission. It
  records each return under the epoch whose close captures it (`recycledFor`), so the ledger tells returned
  carry from fresh funding. Beyond a small residual of its own, it holds stake on the escrow and pool hotkeys
  only until the next recycle.
- **Pending awards.** If the carry has not come back before an epoch's close (a late root, or the relay down),
  the epoch is not matured. Its winners take their slot amounts at once and their pot shares as pending awards,
  paid in the first matured epoch, so a delay does not cost a winner its pot share.
- **Why the carry keeps a residual.** vault2's claim pays the carry by a transfer from another coldkey, which
  the runtime refuses once third parties have filled the receiving coldkey's staking‑hotkey list to 128
  entries, unless the hotkey is already on it. So the carry keeps a residual of its own on vault2's escrow
  hotkey, which keeps that hotkey on its list, pins the CTF pool hotkey there as its auto‑stake destination,
  so its own moves land on a listed hotkey too, and opts out of third‑party list growth, which keeps the list
  short. Its list's length then never refuses it. It stakes the residual from the TAO it is created with, by its own
  `addStake`, and anyone can restore it with `seed()` if root's sweep of small nominations ever clears it.
  The residual never returns to the pool, so the ledger is unchanged (`CAPTURETHEFLAG.md` §4.6;
  `VERSION2-FINAL.md` §19.4).
- **Hotkey accounts against swaps and reservations.** A coldkey swap could merge into the carry's coldkey or
  vault2's, and a swap's announcement reserves its destination, which refuses every transfer into it. The
  runtime does neither to a coldkey that is a hotkey account, so a guardian coldkey makes both of them hotkey
  accounts. What can still delay the carry, taking nothing, is vault2's own list held full while the CTF UID
  earns nothing (`CAPTURETHEFLAG.md` §4.6, §12.4).

At epoch 4's scale, with no head fleet, the CTF pool's fresh amount is 119,583,893,572 rao (119.58 α). A slot
is then worth 48,025,660 rao, about 0.048 α, and a country with no miners grows its two pots by about 0.48 α an
epoch, roughly 25 α a year. A first provider in a country whose provider pot holds 50 epochs of empty slots
takes 2.45 α in its first epoch (`CAPTURETHEFLAG.md` §6).

#### 8.7.4 Validators, audit and claims

- **No probing.** No validator measures, samples or probes a CTF miner. CTF has no quality term and no REACH
  message.
- **One decision.** Each validator decides whether to weight the CTF pools. The release has a setting
  (`ctf: support | off`), and its sample configuration sets `support`. A validator set to `off` treats the CTF
  channel as empty (§8.5, rule 3).
- **Yuma decides what is paid.** Each CTF UID's incentive is the κ‑stake‑weighted median of the validators'
  weights for it, so a CTF pool is paid only while validators holding more than half of the active stake
  support it. Validators that support CTF while consensus does not have their CTF weight clipped, and those
  that do not while consensus does are clipped where they overweight the pool or head. Either way the gap is
  about 1/100 of the row, so the vtrust cost is small. Partner
  validators need nothing for epoch 8: until a release of theirs supports CTF, Yuma clips their weights by
  about 1/100 of the row. `weights_version_key` is not bumped, since support is each validator's choice.
- **Resolving the UID.** A validator finds NO `n`'s CTF pool with `STSettlement2.ctfSettlementAt(n, epoch)` and
  checks the hotkey against vault2's own `pools(2^32 + n)` record, as it does for the provider pool (§10). An
  unregistered or pruned CTF pool leaves the channel empty for that NO.
- **The audit is arithmetic.** A CTF root finalizes only with auditor approval, which `STSettlement2` requires
  (§8.3), and at launch the auditor key is the validator's. Before approving, the validator recomputes from
  public objects and chain state: the
  census against its commitment, the seed from the drand pulse, the draw, the amounts and the accounts. It is
  the same kind of check it runs on the extender census, not a measurement. Presence, the checks behind P1 and
  X1, the countries, units and address dedupe, the wallets and the declared networks are attested and not
  checkable (`CAPTURETHEFLAG.md` §8). `snclaim v2 check --no_id 4294967297` runs the same audit, `ctf.Verify`,
  with the fleet bindings the validator reads, so a fleet‑bound client that a census lists is left out of the
  draw for both.
- **Claims.** Apps claim a CTF account beside the pool account, in the same claim flow. Each entry of
  `GET /sn/ledger/account` gains `kind` (`pool` or `ctf`) and `label`. Old apps show a CTF account as
  "Pool 4294967297" and claim it like any pool.
- **Inspection.** The census, the draw, the awards and the pots are public objects. `api/operator.yml`
  documents the routes under `/sn/ctf` that the ur.xyz `/ctf` page reads, and a miner can see its own status and
  the reason with `GET /sn/ctf/client`.

#### 8.7.5 How it is fixed, and the rollout

**By a signed amendment entry, not a successor policy.** The server resolves each epoch's payout consents in a
domain that includes the epoch's policy hash, so a successor policy would leave every existing consent outside
the new epochs' domain. `E_c`, the shares and the CTF parameters are therefore fixed by the amendment entry v2,
the mechanism that fixes `E_x` (§8.6.7): one line, signed with the price sheet's pinned key and published in
`amendments.yml` beside the price sheet.

```
UR settlement amendment v2: chain 964, sn 25, no 1, from epoch 8, capture the flag, ctf pool 4294967297, carry 0x<carry contract>, shares pool 4/10 ctf 1/10 head 5/10, head cedes to pool, slots 5 providers 5 extenders, countries iso 3166-1 249, presence half epoch, units egress prefix, draw drand quicknet 60s after census
```

Only the chain, sn, operator, first epoch and carry contract vary. The `amendment` package reads the entry in
the server, the validator and `snclaim`. `cli/amendment` signs it and refuses an entry whose first epoch has
started, so the owner signs it during epoch 7 (`mainnet/SETTLEMENT-AMENDMENTS.md`).

**Rollout to `E_c = 8`** (`CAPTURETHEFLAG.md` §11.2):

- **One timelock operation** upgrades `STSettlement2` to `STSettlement2Ctf` and registers UR's CTF pool:
  `upgradeToAndCall` with `registerCtfPool(1, maxBurn, 8)` as its call. It has to execute before epoch 8
  starts at block 9,600,028. Anyone can take the derived hotkey inside the executing block, since it is known
  once the parent block is sealed, so the Safe schedules the operation days early, by about 2026‑11‑22. The
  execute is retried with a bounded gas limit, and a fallback operation with first epoch 9, under another
  salt, is scheduled beside it.
- **The carry contract** is created after the operation, at an address known in advance that depends on its
  exact build, so its artifact is pinned before the entry names the address. It is created with a 1 TAO seed,
  once R-9 has passed on the runtime mainnet runs, the guardian holds its coldkey and vault2's as hotkey
  accounts, and the deterministic deployment proxy's account is funded, within the CTF UID's immunity window,
  and `sn-mainnet ctf-carry review` checks it before and after (`CAPTURETHEFLAG.md` §11.2).
- **The server** samples presence from before `end(6)`, so that epoch 7 runs in shadow. Epoch 8's census is
  committed by block 9,651,028, and its root with the provider root by block 9,651,628.
- **UR's validator** weights the CTF UID from epoch 8's first tempo. It holds the stake majority, so it alone
  makes CTF paid. Its CTF audit and approval ship by about 2026‑12‑01, before epoch 8's root is committed.
- **The first CTF claims** open about 2026‑12‑07, once the root is approved and its 14,400‑block window has
  passed. The notice is `mainnet/notices/2026-11-11-capture-the-flag.md`.
- **What would not move the start.** A late driver or a shadow mismatch: the operator then commits no census for
  epoch 8, every slot carries, and the first winners are drawn in epoch 9. A late validator release only
  shrinks epoch 8's funding, as long as it lands before the CTF UID's immunity ends.

No key changes: the census commitment and the CTF roots use NO 1's root signer, and the auditor set stays as it
is.

#### 8.7.6 Owner decisions (2026‑11‑11)

These are `CAPTURETHEFLAG.md` §12.1.

1. **A new pool per network operator, with its own UID**, beside the current mechanism.
2. **The shares.** Of the providers' share: provider pool 0.4, CTF pool 0.1, top‑level miners 0.5.
3. **Head cede.** While no top‑level miner is weighted, the head's share goes to the provider pool only, and CTF
   stays at 0.1.
4. **Slots.** A uniform amount to up to 5 providers and up to 5 extenders per country.
5. **Nomination.** The operator nominates candidates from its own reliability data and picks winners at random
   per country: an auction among eligible miners.
6. **Eligibility.** Online, passing the operator's checks and in the same country for at least half of the
   epoch, and active at its end.
7. **Carry.** Slots that cannot be awarded carry forward to the next epoch, so a country can build a large pot.
8. **Carry custody.** The carry is held by an immutable carry contract.
9. **Countries.** All 249 ISO 3166‑1 codes have slots.
10. **Units.** The IPv4 and IPv6 subnet uniqueness rules of the head and extender pools apply: all miners in one
    subnet are equivalent.
11. **Validators** do not probe the CTF pool. Each chooses whether to allocate to it.
12. **Registration.** Miners use the provider pool's registration.
13. **Claims.** The apps claim CTF rewards in the same claim flow as pool rewards.
14. **Inspection.** The pool is externally inspectable, through an API at `sn/api/operator.yml` and a flag grid
    on ur.xyz.
15. **The `/ctf` page.** Flags held are countries whose ten slots were all won. A role is "not fully captured"
    where at least one of its slots went unclaimed, so a newcomer wins by default. The chance to win shows only
    where all five of a role's slots were won: the draw's exact odds, 5/entries, or 100% at five entries or
    fewer.
16. **A fifth of the pot.** Each winner takes its own slot and a fifth of the pot carried into its country and
    role, and the rest keeps carrying. A lone miner in an unclaimed country takes a fifth per epoch, so it has
    to stay about five epochs to capture most of the reward.
17. **Start.** `E_c = 8`, the extender pool's first epoch too, with epoch 7 in shadow.
18. **The carry's residual.** The carry contract keeps a residual of its own on vault2's escrow hotkey, pins the
    CTF pool hotkey as its auto‑stake destination and opts out of third‑party staking‑hotkey growth, and a
    guardian coldkey makes vault2's coldkey and the carry's hotkey accounts (§8.7.3). It replaces the first
    carry, which third parties could block for good.

Every other question the design raised is resolved in `CAPTURETHEFLAG.md` §12.2, each with its reason. The one
left open, third parties blocking the carry contract, the owner settled the same day with decision 18
(`CAPTURETHEFLAG.md` §12.4). Its §12.3 lists later options, each of which would need its own signed entry: ASN
units, a cap on pots or a release of territories' pots, per‑country slot counts, a derived‑location requirement
for large pots, a wind‑down rule, and CTF pools for other operators.

---

## 9. Validator channel: native dividends only (v1)

Validators are **independent** — no NO owns or pools them (the per‑NO validator pool, the NO↔V
intersection split, and the per‑path VT of earlier drafts are gone, §13.6). A validator is the source of
the subnet's core data — **which providers are the weakest links** (`VALIDATOR.md` §7). In v1 it is paid
**only** for accurate, consensus‑aligned scoring, via **native Yuma dividends** — the plain Bittensor
norm; there is **no** separate protocol reward for trail *effort* (D29 — the effort bounty is out of scope,
its design parked in §9.3 as a future‑iteration candidate). ("Verifier" and "validator" are now one role.)
From version 2's switch epoch E_s, UR's validator is also the launch **auditor** of payout roots: from an
owner-admitted EVM address it audits each pool's published ledger and approves the root, challenges a
provable fault, or demands openings (§11.3).

### 9.1 What a "validated path" is

A completed `VALIDATOR.md` trail yields a **published proof** `{header, hops[(client_id, time, egress_ip_hash)],
final_sig (NO server), verifier_sig (vpk)}`, with identity

```
pathId = keccak256(trail_id ‖ vpk ‖ server_key_id)
```

Because it carries **both** the NO server's `FINAL` signature and the validator's `vpk` signature, a
path that verifies is **self‑proving** — anyone checks both Ed25519 signatures against the server's
published key and the `vpk`, off chain (each signs its raw canonical message, which the 32‑byte‑message
`0x402` precompile cannot take). This is
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
> **not a roadmap item**. v1 deploys no fee pool, no `submitTrails`/effort claims, no effort validator
> registry (the `STValidatorEvidence` journal records validators' activation consent and evidence hashes and
> gates no payment, §11.1), and no `claimValidator`, and there is **no committed trigger** to add them; a
> validator‑effort
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
attribution — *which providers are the weakest links*. This feeds each pool's cross‑operator quality
`Q_n` (§8.1, §10) — i.e. it drives the miner emission — and is published as a public good. Before E_s it
also fed each NO's within‑pool payout list (`reliability_{n,p}`, §8.2); from E_s per-provider reliability
no longer enters payouts — the within-pool split is by usage alone (§8.2) — so the measurement sets pool
quality but no longer gates who is paid. In v1 the majority of this data is
produced by the owner‑validator, whose business runs on it (§9.2). If a future iteration ever finds
independent‑validator coverage too thin, the parked §9.3 bounty is one lever that would pay validators
**precisely for producing this data** — weighted toward the coverage gaps — but v1 does not include it (D29).

### 9.5 Anti‑gaming for validators

- **Honest scoring** — on‑chain Yuma: a validator inflating a pool (or knifing a rival) is **clipped** to
  the κ‑median and **loses vtrust → loses dividends**; the **self‑weight mask** (§10) stops it scoring a
  NO it declares it controls (the SN25 launch validator declares none and scores UR's own pool, §10);
  **commit‑reveal** stops it copying fresh consensus.
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
approval, no pool. (Approving v2 payout roots is separate: auditor addresses are owner-admitted, while
anyone may challenge or demand, §11.3.) Most validators run no NO, which is exactly the **independence**
`VALIDATOR.md` §1 needs and the structural defense against self‑dealing (§9.5, §12.3). In v1 the owner is the majority
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
        implied_usage_n = usage_n × rate(0) × min(1, cap / owed_n)   // usage the NO signed for the previous epoch (§7.1); the deposit must pay owed_n = usage_n × rate(tier_n) (§7.5, §8.1)
                                                                     // = 1 while the price in force is zero (§7.1)
        pool[p] = implied_usage_n · clamp(quality_v(p))    // §9.4 aggregate, clamped to the policy's [0.75, 1], 0 at 0;
                                                           // = 0 if v declares it controls NO n (self-mask)
    # HEAD — top-level miners: split-adjusted routable-IP breadth (§8.4, D27)
    for each top-level-miner UID u (fleet — client_ids C_u bound to u, §11.4):
        score_v(u) = Σ_{h ∈ v's routable IP-hashes for C_u} 1 / (# bound fleets v sees on h)
        head[u] = EMA(score_v(u)) for the top maximum_head_fleets (200) by it, else 0   // = 0 for v's own UID
    # split the providers' share μ by governance share θ, into ONE vector (§8.5)
    normalize head so Σ head = θ·μ ;   normalize pool so Σ pool = (1 − θ)·μ   // an empty tier cedes to the other
    #   from E_c (§8.5, §8.7): Σ pool = 4/10·μ, Σ ctf = 1/10·μ (each NO's CTF pool UID at its pool's weight;
    #   empty if v's ctf setting is off), Σ head = 5/10·μ; CTF is fixed, and pool and head cede to each other
    SN25: add ur-reserve's k recipient UIDs at (1 − μ)/k each (the whole row when no provider has weight)
    w_v = apply signed policy max_weight_limit_u16 to the row
    w_v = half-up u16(w_v); deterministically repair rounding under the same cap
                          // SN25 treasury row: no clipping or repair; an entry over the cap refuses the row
    commit / reveal w_v   (native CRv4 commit_timelocked_weights extrinsic, drand timelock — §2.4, §5.1)
```

The self-mask covers the NOs a validator declares it controls. The SN25 launch's one UR validator declares none and
scores UR's own pool (owner decision, October 7).

Version 2 changes only *which* UID is NO `n`'s pool. For epochs at or after the pool's v2 `firstEpoch` (E_s for
UR's pool), a validator resolves it with `STSettlement2.settlementAt(n, epoch)`, which returns the pool hotkey
registered in vault2 (`STSettlementVault2`). The validator checks that hotkey against vault2's own
`pools(n)` record and refuses a mismatch, so an upgrade cannot redirect emission. For earlier epochs it uses the v1 coordinator's `operatorAt`, so
replay never calls a contract that did not yet exist. Live steering and replay share this one resolver, and from
E_s the v1 pool hotkey is no longer consulted. A refused or deleted v2 payout root (§11.3) does not by itself change the
pool's weight while there is one operator: zeroing the pool would hand the pool channel to the head fleets,
or with no head fleet on SN25 the whole row to `ur‑reserve` (§8.5), punishing providers for the operator's
fault. The deposit audit is a separate rule: at a nonzero price it checks the usage statement against the
latest root committed in the source epoch's root window, and a later replacement of that root does not change it
(§7.1).

From `E_c` (capture the flag, §8.7) a validator whose `ctf` setting is `support` also weights each NO's CTF pool
UID, at its pool's weight within the CTF channel's 1/10 (§8.5). It resolves the UID with
`STSettlement2.ctfSettlementAt(n, epoch)` and checks the hotkey against vault2's own `pools(2^32 + n)` record, as
for the provider pool. It measures nothing for it, and an unregistered or pruned CTF pool leaves the channel empty
for that NO.

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
  hotkey; the θ split (§8.5) sets how the providers' share of the 41% (`μ`, 1/10 on SN25 at launch)
  divides between the head and the pools.
- **Validator rewards = dividends, and only dividends.** Native dividends grow with bonds on pools
  consensus later rewards (Liquid Alpha) and with vtrust (accurate scoring) — the **Bittensor‑native**
  reward for good evaluation, and v1's **whole** validator reward. Commit‑reveal makes accurate scoring
  require real trail volume; there is no separate effort reward (the §9.3 bounty is out of scope, D29).
- **Anti‑copying.** Commit‑reveal hides fresh scores, so a lazy validator copying stale consensus drifts
  from current quality and loses vtrust (§5.1).

Hyperparameters: `commit_reveal_weights_enabled = true`, `liquid_alpha_enabled = true` (reward early
pool discovery), `mechanism_count = 1` (a 2nd mechanism would halve the 256-UID space, §14), and
`weights_version_key` bumped to force validator-software upgrades (§15.1). Runtime v454 cannot impose a
lower native `max_weight_limit`; the signed policy cap and finalized-vector audit described above are
therefore mandatory release gates.

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
| finalized `Deposit` / `ConvictionAdded` / `ReservePrincipalAdded` events and cap counters | coordinator + reserve sink | demand and conviction audit inputs; validators reproduce them from finalized history |
| immutable `entitlement[e][noId]` and conservation counters (epochs before E_s) | v1 settlement vault | captured/carry total, root, artifact hash, claimed amount, expiry and exact custody accounting |
| funding snapshots `fundedThrough[noId][e]`, every finalized root and the one pending root (each `root`, `total`, `leafCount`, `epoch`, `ledgerHash`), the pending root's demand state for each account of each finalized root, per-account `claimed` and conservation counters (epochs from E_s) | vault2 (`STSettlementVault2`, immutable) | each root's total is bound to captured funding; finalized roots are append-only, so accruals never expire; exact custody accounting |
| auditor set, quorum and per-root approvals | `STSettlement2` (owner-upgradeable) | admission: a pending root finalizes only with a quorum of approvals of its exact commit (§11.3) |
| validator activation anchors and write-once validator/operator evidence hashes | coordinator's pool namespace | domain-bound consent and immutable closed-epoch census/audit commitments, independent of payout-root publication |
| v1 payout artifacts, v2 ledger objects (header, provider and account chunks) and completed-trail/statistics artifacts | **server API + content-addressed `server/blob` MinIO** | canonical bulk inputs; publicly retrievable by SHA-256 and independently reproducible; the validator also retains each v2 ledger it audits |
| signed price sheet | **`https://ur.xyz/price.yml`**; readers keep the last good copy | deposit rates and cap by epoch; only entries signed by the pinned key count, so the file itself is not trusted (§7.1) |
| signed usage statements (epochs from E_s) | **the NO's public `/sn/evidence` routes** | the bytes and users a deposit prices; bytes are checked against the committed ledger, users are attested (§7.1) |
| fleet manifest hash | **commitments pallet + finalized coordinator mirror** | hotkey-authorized native anchor used by every binding member |

This directly answers `seed/INCENTIVES.md`'s open question: **yes**, each NO commits a **Merkle root** of its
payout table so every provider verifies *its own* payout with an `O(log N)` proof, with no bulk data on
chain. Before E_s the table holds fractional shares; the v1 vault keeps each NO's per-epoch payout root with
the on‑chain pool total and derives the α at claim time (§8.3). From E_s it is the pool's public **ledger**:
a canonical JSON header listing content-addressed binary chunks of each covered epoch's provider rows
(`client_id`, coldkey, usage bytes, head flag) and of the accounts (coldkey, cumulative rao). Each commit
puts on chain the root, total and leaf count of a **sum tree** over the accounts, plus
`ledgerHash = sha256(header)`, and vault2 requires the total to equal the pool's captured funding through
the root's epoch (§11.2). The objects are unsigned: the root signer's on-chain commitment authenticates the
header, and the header authenticates the chunks. Either way each NO has its own root — there is **no global
claim root**.

Validator evidence uses immutable on-chain hashes, not bulk proof bytes on-chain.
Its full signed artifacts and referenced proof streams must be publicly available
through the server API and content-addressed MinIO. The separate evidence slot
binds the validator, operator, closed epoch, evidence kind, deterministic audit
subject, activation and chain context. Either the validator or a relayer may
submit both required key consents; a dishonest operator cannot veto publication.
No-payout and missed-root windows still require their complete evidence census,
and later demand-deposit audits use separate slots to avoid circular commitments.
An anchor proves a byte commitment, not proof truth, historical eligibility or
availability: independent readers must retrieve and verify the complete signed
history. These commitments do not alter pool entitlements, payout deadlines,
shares, carry, or the parked validator-bounty design.

### 11.2 Claiming

From E_s a pool pays through **cumulative accounts**. An account is one payout coldkey in one pool, at a fixed
position `index`; its leaf carries the coldkey's **cumulative** accrual in rao — the running sum of its shares
of the pool's funding, epoch by epoch, allocated by usage alone (§8.2) — which never decreases. Accounts are
never removed: after a wallet change new shares accrue to the new coldkey's account, and the old one stays
claimable. A root commits the pool's `n` accounts as a **Merkle sum tree** of depth `d = ⌈log2 n⌉` (`d = 0`
when `n = 1`):

```
leafHash(c, v)        = keccak256(0x00 ‖ c ‖ uint256(v))
nodeHash(hL,sL,hR,sR) = keccak256(0x01 ‖ hL ‖ uint256(sL) ‖ hR ‖ uint256(sR))
level 0, position i   = (leafHash(c_i, v_i), v_i) for i < n;  (0x00…00, 0) for n <= i < 2^d
level k+1, position j = (nodeHash(L.h, L.s, R.h, R.s), L.s + R.s) for L, R = positions 2j, 2j+1 of level k
root = level d, position 0; committed with total = root sum and leafCount = n

vault2.claim(noId, rootIndex, (index, coldkey, cumulative), proof):   // anyone may send it
    r = roots[noId][rootIndex]                       // any finalized root; every one is kept
    require index < r.leafCount and exactly d (hash, sum) siblings, d from r.leafCount
    fold (leafHash(coldkey, cumulative), cumulative) up the siblings  // bit k of index: right child at level k
    require the fold reproduces both r.root and r.total
    require coldkey ≠ 0 and coldkey ≠ vault2's own coldkey
    amount = cumulative − claimed[noId][index];  require 0 < amount < 2^64 − 1
    require claimedTotal[noId] + amount ≤ finalizedTotal[noId]       // the latest finalized root's total
    claimed[noId][index] = cumulative;  claimedTotal[noId] += amount  // and the conservation counters
    transferStake(coldkey, escrowHotkey, netuid, netuid, amount)     // escrow −amount exactly; coldkey +amount or 1 rao less
    any failure reverts the whole claim, and the account stays claimable
```

One claim pays everything the account accrued since its last claim, across any number of epochs, or it
reverts — there is no per-epoch claim, no deferred credit and no expiry. Anyone may send it for any account;
the payment always goes to the coldkey in the leaf, so a proof — served publicly by
`GET /sn/ledger/account?coldkey=…` or rebuilt from the published ledger — cannot redirect a payment. The apps
still treat that endpoint as untrusted and check each proof against vault2's on-chain root before they claim
(§16.1). A runtime refusal (a transfer below the `DefaultMinTransfer` floor of 100,000 rao TAO-equivalent,
or a destination coldkey whose staking-hotkey list is full) reverts the claim and leaves the balance
claimable; since balances never expire, a provider, its app or the miner claim daemon simply claims once the
balance is large relative to gas. A claim uses about 170,000 to 188,000 gas. Runtime 478, on mainnet since
2026‑10‑13, admits its stake transfer only if the forwarded gas covers a walk of vault2's own staking-hotkey
list, so a claim's least gas limit is about 192,000 while that list is short and about 1.27M once third
parties fill it (about 2.44M whatever the list on runtimes 475 and 477). Anyone can lengthen the list between
an estimate and the claim's inclusion, so the claim tools and the SDK sign a claim at no less than 2.6M gas;
only the gas used is paid (`docs/spec/runtime-478-audit.md`; `VERSION2-FINAL.md` §11). An optional,
stateless claim batcher (`STClaimBatcher`) claims many
accounts in one transaction, each capped at its own gas so that a claim the runtime refuses cannot starve
the others, and still only to each leaf's coldkey: from ten claims on it saves 15–18% of their gas, and
a batch's gas limit grows by about 149,000 per claim where each claim sent alone is signed at 2.6M. One
transaction holds up to 416 claims at the deepest tree on runtime 478 (418 on 475 and 477), and the claim
tools send at most 32, under 1% more gas a claim (`VERSION2-FINAL.md` §10.2, R-7).
Pausing settlement never stops claims. Claims are pull‑based, so settlement is `O(1)` on‑chain regardless of
participant count, and each pool has its own root — **there is no global claim root**.

Epochs before E_s keep the v1 per-epoch claim, `claim(epoch, noId, coldkey, shareBps, proof)`, worth
`share × entitlement.total` against that epoch's immutable entitlement (§8.3) and open until that
entitlement's expiry; v1 credit stays withdrawable through `withdrawClaimCredit`. (`claimValidator` — the
bounty claim, `feePool · effort / Σ effort` — is part of the parked bounty design, not in v1, §9.3/D29.)

### 11.3 Disputes

From E_s a committed payout root is only *pending*. Anyone may finalize it, and it becomes claimable, only
once a quorum of owner-admitted auditor EVM addresses has approved its exact commit
(`approveDistribution(noId, seq, root, ledgerHash)`) before the next epoch ends, and the 14,400-block window
from its commit has passed with no open demand. At launch the quorum is the validator alone. It fetches the
public ledger, checks the header against `ledgerHash` and every chunk against its hash, and re-runs the build
against chain state — covered epochs, row rules, head exclusion from the fleet bindings (§11.4), the
allocation from on-chain funding, unchanged account positions and coldkeys, and the rebuilt root, total and
leaf count. Population, usage, window membership and wallet selection stay operator-attested in this phase.
Approval, in the upgradeable `STSettlement2`, vouches for how new funding is allocated. The window and two
objective, permissionless checks live in immutable vault2, so they hold whatever `STSettlement2` does; both
protect the accounts of every finalized root, each named by its index:

- **`checkPath` — challenge.** Anyone submits an account's leaf and proof from a finalized root, any of the
  pool's, plus a top-down opening of the pending root along that index. A lowered value, a changed coldkey,
  a garbage leaf or an inconsistent node (child sums that do not add up to the node's) deletes the pending
  root. An opening with no fault answers an open demand on that account of that root, once; without one it
  reverts and changes nothing.
- **`demandPath` — demand.** Anyone may demand, for the immutable fee, the on-chain opening of an existing
  account's path, the remedy when the ledger is withheld. The demand carries the account's leaf and proof
  against a finalized root it names, so a padding or otherwise unprovable position, which no honest
  successor could answer, can never be demanded, and each account of each finalized root can be demanded
  once per pending root. Any finalized root will do, not only the latest, so withholding the newest root's
  ledger cannot stop demands on the accounts of an older one. The root cannot finalize until each demand is
  answered by a clean `checkPath`, which the operator sends from its copies of the trees; a garbage or
  withheld node can never be answered.

The validator approves only if every check passes, challenges any fault that published account rows
reproducing the root can prove, and, if the objects are missing or do not reproduce the root, refuses
approval and demands three sets of accounts. A tree rebuilt from rows is always consistent, so an
over-allocation hidden in an inconsistent subtree is never reproduced from published account rows: it takes
the demand route, and any opening under the bad node deletes the root. All three are accounts of the newest
finalized root whose ledger the validator holds and has verified: the latest, or an older one when the
latest's objects are missing or do not rebuild it. One demand set is every such account whose published row
shows a fault. Another is a value-weighted random sample of 256 of them, or all of them when there are fewer,
drawn without replacement with randomness the operator cannot predict and sent first; it doubles for each
earlier root over the same predecessor that was replaced or deleted with a demand still unanswered, and the
third set demands those unanswered accounts again. vault2 allows one commit per pool per 14,400 blocks, so the
sample cannot be re-rolled quickly,
and each demand carries an immutable fee that keeps griefing costly. `snclaim v2 check` lets anyone do the
same, protecting the newest finalized root whose ledger it can rebuild and reporting that root's accounts as
unverifiable, never as faultless, whenever the objects are missing or do not reproduce the root; UR runs it
as an independent watcher on a host other than the validator's. Every sender treats a refusal that shows
another transaction already did the work (an approval already counted, an account already demanded, a root
already answered, deleted, replaced or finalized, a claim a third party already paid) as done. The residual
risk is an account that is faulted but never demanded, under a withheld ledger *and* a bypassed approval;
demands are weighted by value, so the larger a theft, the likelier it is caught. A pool's first root has no
predecessor and rests on approval alone. A root still unfinalized at the end of the next epoch can be
replaced by the next commit, and a deleted root frees the pending slot; either way no value moves, since the
next root allocates their epochs from their own usage, and while there is one operator neither changes the
pool's weight (§10). Pausing settlement stops finalization but never approvals, challenges or demands.

Otherwise the on‑chain dispute surface is deliberately small — deposits are reserved (nothing distributable
to dispute), pool amounts are read from chain state, and effort claims do not exist yet:

- **Bad payout artifact/share (epochs before E_s).** The operator API and validators reject any artifact that
  does not reproduce its content hash/root or exactly allocate 10,000 bps. Independently, every claim proves
  `(coldkey,share)` against the immutable entitlement and the vault cannot pay more than its total.
- **Bad usage statement (epochs from E_s).** Validators use only a statement that names the committed chunks
  and verifies; with none, or with two that disagree, the pool has no weight on that steering pass (§7.1).
  `total_users` stays attested, and a statement changes no ledger bytes, so a dispute over users cannot hold
  up settlement.
- **Bad `client_id ⇄ hotkey` binding.** Invalid consent never enters state: the coordinator checks the
  complete replay-bound payload, Ed25519 and sr25519 signatures, matching finalized commitment, validity
  interval and live UID before recording a future-effective version (§11.4). Client-signed revocation
  and permissionless stale cleanup cover later compromise/deregistration.
- **(Parked bounty design, not in v1 — §9.3/D29.)** Effort claims would be **optimistic**: a validator commits a Merkle
  root of its trails + a claimed effort total; the contract `0x402`‑verifies a **random sample** of the
  committed leaves (`FINAL` against the NO's server key, `verifier_sig` against the validator's `vpk`),
  and during the +24h…+48h window **anyone may dispute any leaf** with the same check — a single failing
  leaf voids the whole claim and forfeits the validator's stake at risk, so a fabricated trail is never
  worth the gamble and the contract never verifies *every* trail (it scales).

Statistical disputes (a validator's trails look self-dealt or coverage-gamed, `VALIDATOR.md` §7.7) are
**not** resolved on-chain, in v1 or v2. They remain public evidence and affect Yuma trust naturally; the
contract layer handles only deterministic custody, cryptographic authorization and, from E_s, the
objective root checks above.

### 11.4 The fleet `client_id`s ⇄ hotkey binding (top-level miners)

To steer the head (§8.4), a validator must map each measured `client_id` to a top-level miner's UID — and
because a head miner is a **fleet** (D27), one head hotkey binds **many** `client_id`s (the fleet's
providers), across which the validator counts the fleet's distinct routable egress IPs. The binding is
**published, signed, and cheap to read**, using the standard Bittensor "signed proof → registered hotkey"
pattern (Epistula / ORO-AI `bittensor-auth`) — with a **dual signature** so a fleet cannot claim a
`client_id` (hence an IP) it does not operate.

**Dual-signed association.** A provider claiming a top slot proves control of **both** keys:

The canonical byte payload is the ASCII domain `urnetwork/fleet-binding/v1`, followed without padding
by `chain_id:u64be`, `netuid:u16be`, `coordinator:20`, `fleet_id:32`, `hotkey:32`, `client_id:16`,
`client_key:32`, `generation:u64be`, `valid_from_epoch:u64be`, `valid_to_epoch:u64be`, and
`commitment_hash:32`. Both keys sign `keccak256(payload)`:

```
sig_client = Ed25519.Sign(client_sk, keccak256(payload))
sig_hotkey = sr25519.SignSubstrateContext(hotkey_sk, keccak256(payload))
```

Chain, netuid, coordinator, epochs, generation and commitment hash are signed replay boundaries.
`client_id` is the 16-byte UR identity; `client_key` is its distinct 32-byte Ed25519 public key.

This is the shape of SN51 Celium's anti-theft `associate_evm_key` (both keys sign the linkage); a *single*
hotkey signature would let a miner **steal another provider's measured quality** by claiming its `client_id`.

**Where it lives.**
- **Commitments pallet** (free, `Pays::No`, keyed `(netuid,hotkey)`, §2.4): the hotkey publishes the
  canonical fleet-manifest hash. The finalized chain index mirrors that exact value and finalized
  block/hash through the coordinator's narrowly scoped commitment-oracle role.
- **Coordinator anchor:** each client member is inserted incrementally. The coordinator requires the
  mirrored commitment to match and be fresh, verifies client consent at `0x402`, hotkey authorization at
  `0x403`, and live UID resolution at `0x804`, then records the generation and epoch interval. Versions
  cannot overlap; membership starts no earlier than the next epoch and is bounded in duration.

**Identity ⊥ score.** The binding proves *ownership* only; `VALIDATOR.md` proves *routable-IP breadth*.
They stay separate (as every comparable subnet does — Targon keeps the hotkey out of its TEE
attestation): a validator attributes each trail hop to a fleet UID via the binding, then scores that UID
by the split-adjusted count of distinct routable egress-IP-hashes it observed across the fleet's
`client_id`s (`VALIDATOR.md` §7, §8.4).

**Privacy — opt-in self-deanonymization.** Publishing `client_id → hotkey` (→ egress IP, `VALIDATOR.md`
§8.1) **does** deanonymize, so it is **voluntary and only for providers claiming a public top slot** —
claiming the public UID *is* the consent. The long tail stays `client_id`-pseudonymous inside the pools.
(The NO `/verify` server — which already co-signs trails and authoritatively knows
`client_id ⇄ egress-IP` — could add a third attesting signature, strengthening the binding at the cost of
NO-trust; release 1.0 does not implement it, and the coordinator accepts exactly the two signatures above.)

---

## 12. Economic analysis

### 12.1 Operator equilibrium

Let a NO with real customer revenue `R_n` (α‑equivalent) choose its conviction tier and its per‑epoch
deposit `D_n`. Over an epoch the **contract pays that NO's providers** `0.41·E·μ·ŵ_n·(1−θ)` (the NO's pool
emission, `μ` the providers' momentum share, §8.1, `ŵ_n ∝ implied_usage_n × Q_n`, `implied_usage_n` the audited
usage priced at the baseline rate, §8.1); the deposit and
any conviction stake are both locked in the reserve (§7.4), and the NO keeps `R_n − D_n` off‑chain.
Providers attach where expected pay is highest. Two forces balance:

- **Serving more real usage**, and depositing `D_n` for it at `rate(tier_n)` (a higher tier lowers `D_n`
  and, below the cap, leaves the weight unchanged), lifts the NO's implied usage → its
  emission share `ŵ_n` → the α its providers earn → attracts capacity → more real usage → more `R_n`
  **and** higher measured `Q_n` (which feeds back into `ŵ_n`). At the per‑epoch cap more usage stops
  lifting implied usage, and a higher tier raises it to `cap × rate(0) / rate(tier_n)` (§8.1).
- **`D_n ≤ R_n`** in the long run: both the deposit and the conviction stake are **fully sunk** — locked
  in the reserve, recoverable by no one — so α not backed by customer revenue is pure loss.

The emission `E` is a subsidy auctioned ∝ implied usage: in equilibrium NOs commit α (deposits + tier
conviction) up to where the marginal emission steered to their providers equals the marginal cost of the
sunk α. Staking for a lower rate is a **capital‑for‑cashflow trade** (lock more up front → post less per
epoch) that nets to the same weight per unit of real usage below the cap, so it lowers the onboarding
barrier without distorting the usage signal there; a capped NO, whose weight no longer tracks its usage,
gains weight from a lower rate (§8.1). This ties total commitment to total real demand and makes `w_n` track
genuine usage share — the intended outcome. (It is the deposit‑for‑weight pattern Bittensor already
trusts in burned registration, with the burn replaced by a transparent, compounding lock.)

**Self‑dealing — closed structurally.** The worry is a NO that runs its *own* providers so its deposit
round‑trips to itself. The round‑trip **does not exist**: no deposit or conviction α is ever distributed
(§7.4), so recovery through the payout channel is **zero** — the full commitment is sunk for attacker and
honest NO alike. Buying cheaper weight via a higher tier costs *more* locked capital, not less, so the
tier discount does not open a self‑dealing edge. What remains is the *steering* channel — a deposit still
buys pool emission `ŵ_n`, captured by whoever the NO's providers are — bounded by
`0.41·E·μ·(1−θ)·(implied_usage/Σ) − (sunk α) − (real infra to pass live trails)`, gated by winning
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

- **Secured:** cross‑operator emission tracks `implied_usage × consensus‑quality`, with implied usage
  the audited usage priced at the baseline rate, paid for by the deposit (costly, revenue‑backed demand × independently‑measured
  liveness) — **validators' evaluation drives the miner payout**, the
  Bittensor mechanism — via median + clipping + vtrust over many **independent** validators, plus the
  self‑weight mask and the fully‑sunk deposit (§10, §12.1); provider quality also bites **within the
  pool** under v1 settlement (auditable payout list, §8.2) and via reputation; v2 settlement splits each
  pool by usage alone (below, §13.4).
- **Bootstrap caveat (not a removal — a ramp):** `Q_n` is noisy until the validator set and data mature,
  so governance **caps the quality swing** early (closer to demand‑weighted; the launch policy clamps
  `Q_n` to [0.75, 1]) and widens it as the independent‑validator stake share grows. Quality is on the payout
  path from day one; its *magnitude*
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
- **Secured by v2 settlement** (epochs from the switch epoch `E_s`, from which a pool settles on v2;
  §13.1, `VERSION2-FINAL.md` §3–§4): the promise is **accrual** — each eligible provider's usage share of
  its pool is credited to its payout coldkey's account, never expires, and is delivered whenever anyone
  claims it. Whatever the owner-upgradeable `STSettlement2` coordinator does, the immutable
  `STSettlementVault2` (vault2) enforces (G1–G6): conservation
  (`totalCaptured = totalPaid + escrowAccounted`; per pool, claimed ≤ finalized ≤ captured); survival of
  accrued balances under a bad successor root (a challenge deletes it; an unanswered path demand blocks
  it); payment only to the account's coldkey, whoever claims; no expiry; root totals bound to the pool's
  snapshotted funding; and no finalization within 14,400 blocks of the commit or with a demand open.
- **Not secured by v2 settlement** (beyond the residuals above): **usage fraud on new funding** —
  population, usage, earning-window membership and wallet selection are operator-attested and not yet
  checkable, so a dishonest operator can misallocate each root's new funding, though G2 keeps it from
  lowering existing balances; an account that is faulted but **never demanded** while the tree is
  withheld *and* admission is bypassed (an owner upgrade, or the root-signer and auditor keys) — watchers
  demand a value-weighted sample, so the larger the theft, the likelier it is caught; and an epoch whose
  **close is missed** beyond its 1,200-block window, whose share moves to later earners. Nor is delivery
  time promised: at live rates about 1,450 α per epoch reaches the pool — the providers' momentum share,
  1/10 of the miner allocation, less θ = 3/10 for the head (§8.5) — ≈ 0.0145 α per
  provider per week at 100k providers, so a 250k-gas claim at a 5 gwei base fee (≈ 0.19 α; the rehearsal
  measured 186,707 gas for an account's first claim and 169,606 for a later one) costs about 13 weeks of
  average earnings, and gas falls under 10% of a claim only after ≈ 131 weeks; both take ten times longer
  at 1M providers (`VERSION2-FINAL.md` §3). A third party can also raise that cost: by filling vault2's own
  staking-hotkey list (up to 128 entries, each costing it a transfer above the floor) it makes every claim
  use about 1.26M gas instead of 0.15M, about 1 α at the same prices.
- **Not secured by the deposit audit:** it uses the latest root committed in the source epoch's root window, and a later
  replacement does not change it, so an operator could commit a deflated root it expects to be refused, pay the smaller
  deposit, and replace the root while it is still unfinalized at the end of the next epoch (§7.1). Both commits are
  visible on chain; this is accepted while the subnet owner runs the only operator and is to be revisited before other
  operators are admitted.
- **Outside any contract** (chain-level powers that the release 1.0 vault faces too): the SN25 owner key
  or root can **freeze every claim** by disabling stake transfers (§6.4), though it cannot take or
  redirect a balance; and SN25 can be **dissolved**. A new subnet registration at the subnet limit
  dissolves the non-immune subnet with the lowest moving α price, and root can dissolve any subnet.
  Dissolution turns every SN25 position into free TAO on its coldkey, which neither vault can pay out, so
  custody would be stranded and every claim would revert. The owner accepts that risk (October 11): the
  operator server alerts when SN25 nears the lowest moving price among non-immune subnets, and claims are
  encouraged so that custody stays small. vault2 also depends for good on four staking-precompile ABIs
  (§2.5).

### 12.4 The buyback reserve and the demand crossover

The reserve turns the token flow one‑directional. Per epoch, in α units:

```
locked in:   B_e  =  Σ_n D_n                       // deposits: market-bought, staked to the reserve (§7.4)
             Y_e  =  0.41·E_e · s_e · (1 − take)   // reserve dividends: its share s_e of subnet validator stake
liquid out:  L_e  ≈  0.41·E_e                      // miner emission: providers' μ (both tiers) + ur-reserve's 1 − μ (§8.5)
                   + 0.41·E_e · (1 − s_e)          // outside-validator dividends
                   + 0.18·E_e                      // owner cut
```

`E_e` (α issuance) is on a fixed, halving schedule; `B_e` scales with **usage revenue**. Define the
**demand ratio** `R_e = (B_e + Y_e) / L_e` — publishable every epoch straight from chain state
(finalized `Deposit` events + `getStake(reserveHotkey,sinkColdkey,netuid)`; the §16.1 indexer's headline stat). Three regimes:

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
the old deposit‑funded floor is gone, §7.4). **(2)** The reserve is a **growing honeypot**, but release
1.0 places it behind a non-upgradeable sink with no outbound bytecode; the remaining risk is a runtime
change to contract-coldkey/precompile semantics, continuously conformance-gated. **(3)** dTAO stake has
no native unbonding lock (§2.3), so the physical lock is supplied by the immutable sink's missing exit
path. **(4)** Reserve‑as‑consensus‑stake
concentrates Yuma power with the owner **by design** (§7.4); decentralizing the validator set later must be
a deliberate, budgeted step, its committed lever **re‑delegating reserve slices** (§6.4.3).

---

## 13. Design decisions and alternatives

### 13.1 Settlement: contract custodies the miner pools; validators are independent (chosen)

The immutable settlement vault **owns each NO's miner‑pool UID** outright, so the **tail's share of the
41% miner emission** accrues to the vault and is paid out by direct Merkle claim (the **head is native**
— top-level miners own their UIDs and are paid to their own coldkey, §8.4) — *a network operator never
custodies emission destined for its providers* (the hard requirement). The **weights are set by
independent validators** (§9, §10),
not the contract, so Yuma consensus does real work; their **41% dividends are native** (no middleman to
remove; a fee‑funded effort bounty is out of scope, §9.3/§13.6/D29). Implications: the contract is
custody-critical for miner emission while a separate immutable sink owns the buyback reserve; the
upgradeable coordinator owns neither. The vault owns **one miner-pool UID per NO**, so budget
`max_allowed_uids` and registration burns to the **NO count** — providers are *not* UIDs, they live
inside the pools. No α→TAO→α churn. *Rejected:* **per‑provider UIDs** (100k+ ≫ the ~256 cap — the reason
for pools, though the **top ~200 do get their own UID** — the head tier, §8.4); letting emission land
on NO hotkeys (violates no‑custody); a **single** contract miner UID with the contract as sole validator
(collapses Yuma); and the earlier **per‑NO validator pool with a take‑0 custody hack** (fragile, and
redundant with crypto‑validity — replaced by plain independent validators earning native dividends, §13.6).

**v2 settlement (from `E_s`; owner decisions, October 11; `VERSION2-FINAL.md`).** The release-1.0 (v1)
vault fixes a per-epoch entitlement of expiring per-coldkey shares, so a provider claims once per epoch,
and only providers the validator has measured can be paid (at launch, a closed cohort of at most 2,000).
v2 leaves the v1 contract set and policy v1 unchanged — they settle every earlier epoch and a later sweep
of v1 carry — and adds two contracts beside them: the immutable **`STSettlementVault2`** (vault2), which
owns the v2 pool and escrow hotkeys and holds the funds, and the owner-upgradeable **`STSettlement2`**
coordinator, which holds no funds. Each root is a **Merkle sum tree** over cumulative, never-expiring
**accounts**, one per payout coldkey per pool at a fixed position, credited by usage alone (§13.4). A root
becomes claimable only after a quorum of owner-admitted auditors approves it (the validator alone at
launch) **and** a 14,400-block window passes, in which anyone can prove that it lowers, replaces or hides
an existing account, which deletes the root, or demand the on-chain opening of an existing account's
path, which blocks finalization until answered. One permissionless claim pays everything accrued since the
account's last claim, only to its coldkey; it pays or reverts, and a reverted claim stays claimable. The
apps claim through the SDK from activation. Claims, approvals, challenges and demands never pause. At
launch one organisation is owner, operator and validator, so approval guards against bugs and single-key
compromise; vault2's guarantees protect accrued balances against an upgraded `STSettlement2` or a
compromised root signer plus auditor, as long as someone challenges or demands in the window (§12.3). *Rejected:* changing the policy or the v1 contract
identities (every wallet consent and hotkey delegation is signed under them, so any change orphans them
all); an OpenZeppelin multiproof tree with a ring of four roots (the sum tree makes a lowered or hidden
account provable on one path); relayer permits (wallets differ in the bytes they sign); and an
evidence-journal veto in place of affirmative approval.

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

`weight_n ∝ implied_usage_n × Q_n`, with `implied_usage_n`, the audited usage priced at the baseline rate, the demand anchor
(§8.1, D25) and `Q_n` the consensus‑measured pool quality (§8.1, §10): demand is the objective anchor,
quality is the earned modulator, and together they make validators' evaluation the thing that moves the
money. The one nuance is **magnitude at bootstrap**: `Q_n` is noisy until the
validator set + data mature, so governance **caps the quality swing** early (closer to demand‑weighted)
and widens it as the independent‑validator stake share grows (§12.3). So quality is on the payout path
from day one — we ramp its *strength*, we do not defer the mechanism.

**v2 settlement confines quality to this cross-operator weight** (§13.1). From `E_s` the within-pool split
is by usage alone, with no per-provider reliability factor (cf. §8.2), so the validator's measurement no
longer gates which providers inside a pool are paid. Root approval is an objective ledger check, not a
quality lever: while there is one operator, a refused or deleted root by itself leaves the pool weight unchanged,
because zeroing the pool would hand the pool channel to the head fleets, or with no head fleet on SN25 the
whole row to `ur‑reserve` (§8.5), and punish providers for the operator's fault (owner decisions,
October 11).

### 13.5 No on‑chain oracle (simplified out)

Because per‑GB / per‑user usage is self‑reported and unverifiable on‑chain, an on‑chain rate has no
enforcement power. The objective on-chain input is *α deposited*; validators check it against the
signed usage priced at the governance-published conviction-tier rate, and weight that usage at the
baseline rate (§7.1, §8.1). v1 therefore has **no
oracle**: the rate schedule is the signed price sheet, or the signed, content-addressed off-chain policy while
no signed entry is in force (§7.1), and NOs use it to size deposits and validators independently use it to
reproduce weights.
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
co‑signed = agreed by construction; an invalid one fails the Ed25519 check, §9.1) and was a weak
effort proxy. v1's validator side is simply: **stake α, run trails, earn native dividends.**

### 13.7 Two miner tiers: pool on-ramp + direct top-level miners (chosen)

The miner side runs **both** a per-NO pool (§8.1–8.3) *and* a direct top-level-miner channel (§8.4), in
parallel. *Why both:* a new provider needs a low-barrier **place to start with a baseline reward** (the
provider needs no UID or burn; the vault limit-registers the shared NO pool UID), while the best
providers deserve the **canonical, trust-minimized** Bittensor treatment (their own UID, steered
directly, paid natively). A provider **starts in a pool and graduates**
to a top slot, the chain's deregistration churn running the tournament. This is **novel** on Bittensor: the
field has the pool pattern (ComputeHorde, TPN, Vanta) *and* the direct-UID pattern, but **no subnet tiers
them** — the norm is the opposite, *consolidate* behind one UID (Chutes: "never register more than one
UID"); and every pooled tail elsewhere is paid **off-chain at operator discretion**, where ours is
trustless (`COMPARISON.md`). *Rejected:* head-only (discards the 100k-provider on-ramp and the demand
signal); pool-only (the v0.1 design — leaves the best providers trusting a NO's split, one of thousands);
and making top-level miners *contract-pooled* too (redundant — that is just another NO pool, and child
hotkeys **cannot** route miner emission to sub-workers anyway, §8.4).

### 13.8 Head/tail split θ in one mechanism (chosen); not two mechanisms, not owner-burn

The two tiers share **one mechanism's** 256-UID metagraph, and the providers' share of the 41% miner emission
(`μ`, 1/10 on SN25 at launch, §8.5) divides between them by a governance share **θ** baked into validator
software (SN13-style weight reservation, §8.5).
*Rejected:* **two sub-mechanisms** (mechanism 0 = pools, mechanism 1 = top miners) split by
`mechanism_emission_split` — clean in theory, but `mechanism_count × max_UIDs < 256` would **halve the UID
space to ~127**, too few for ~200 top miners (so release 1.0 fixes `mechanism_count = 1`, §14);
and **reserving the split by burning to an owner/immune UID** — post-Spec-421 the `(1 − miner_burned)` term
would shrink the subnet's whole cross-subnet allocation (§8.5). θ trades demand-coupling (in the tail)
against the merit apex (the head); it is a governed dial, started tail-weighted and ramped (§8.5, §12.3).
From `E_c` the signed amendment entry v2 fixes the shares instead, and the same vector carries a third channel,
each NO's capture the flag pool (§8.5, §8.7).

---

## 14. One mechanism and its UID budget

Release 1.0 uses exactly **one** mechanism. The pool on-ramp and direct head are two scoring channels in
one CRv4 vector, divided by θ (§8.5), not Subtensor sub-mechanisms; from `E_c` the CTF pools are a third
channel in the same vector, and three fixed shares divide it (§8.5, §8.7). A second mechanism would divide the
global ~256-UID capacity and make the target head impossible; the release preflight therefore requires
`mechanism_count = 1` and refuses to register roles unless all missing identities fit the intended
`max_allowed_uids` snapshot.

The budget is `max_allowed_uids ≥ head fleets + one vault-owned pool UID per NO + validator UIDs + the
owner/escrow identities that require registration + the treasury recipient
UIDs` (two on SN25, §8.5); from `E_c` each NO holds two vault-owned pool UIDs, its provider pool and its CTF
pool (below). Validators are not a separate fixed partition: they
are live UIDs holding permits according to native stake. New product lines remain an off-chain/operator
classification unless a future whitepaper revision supplies a new economic model and explicit UID
budget; they are not a latent release-1.0 mechanism switch.

v2 settlement (§13.1) registers two more vault-owned identities, one burn each: vault2's escrow hotkey at
deployment, and the v2 pool hotkey one to two days before `E_s`, so that it earns weight before its
registration immunity ends. vault2's creation reveals the escrow hotkey, and whoever registers it first
makes vault2's own registration fail for good, so the deployment sends `registerEscrow` at the very next
nonce, back to back with the creation. Losing that race costs a redeploy, with no funds at risk: vault2
registers no pool until its escrow is registered, since whoever owned the escrow hotkey could swap it away
with every position on it. During the switch a NO holds two pool UIDs. From `E_s` validators weight only
the v2 one, and the v1 pool UID may be pruned without error: pruning removes only subnet membership, and
stake moves need only the hotkey's `Owner` entry. The escrow hotkey earns no emission, so it is among the
first UIDs pruned once its immunity ends, and vault2 keeps working with it pruned. Anyone can re-register a
pruned v2 pool hotkey through `registerPoolUid`, paying the burn.

Capture the flag (§8.7) registers one more vault-owned identity per NO, one burn each: its CTF pool hotkey,
registered by `STSettlement2Ctf.registerCtfPool` one to two days before `E_c`, as the v2 pool was, so that it
earns weight before its registration immunity ends. The hotkey is derived when the timelock operation
executes, from the proxy, the pool id `2^32 + n`, the block number and the previous block's hash. A hotkey
named in the scheduled call would be public for the timelock's 72 hours, and whoever registered it on SN25
first would make vault2's registration fail. The derived hotkey is still known once the parent block is
sealed, and anyone can take it inside the executing block, so the operation is made ready days early, with a
fallback for the next epoch (`CAPTURETHEFLAG.md` §3.1, §11.2). The registration takes one of the 256 UIDs,
and with no owner trim a full metagraph loses its lowest-ranked UID that is not immune; the live
`immunity_period` has to outlast the time from the registration to the first revealed CTF weight, a little over
two days. A pruned CTF pool earns nothing, and leaves the CTF channel empty for its NO, until anyone
re-registers its hotkey through `registerPoolUid`, paying the burn.

---

## 15. Concrete parameters

### 15.1 Subnet hyperparameters (owner‑set at creation; verify live before launch)

| Hyperparameter | Value | Rationale |
|---|---|---|
| `tempo` | **360** | native ~72‑min weight/emission cadence (§5.1) |
| `max_allowed_uids` | **256** (hard ceiling — owners may lower, never raise) | one metagraph shared by **~200 top-level miner UIDs + 1 pool UID per NO + validator UIDs** (+ the treasury recipient UIDs on SN25, and from `E_c` a CTF pool UID per NO, §14); tail providers are NOT UIDs (§3) |
| `max_allowed_validators` | root-controlled/runtime-dependent; target **≤ 56** so ~200 miner slots fit | observe and compatibility-gate the live value; permit count (top-k by stake, §9.7) is *not* a fixed slot partition |
| `mechanism_count` | **1** | a 2nd mechanism halves the 256-UID space below 200 (§13.8, §14) |
| native `max_weight_limit` / signed policy cap | v454 native **65535** (no cap), unchanged through runtime 478; release policy **32768**, the two-NO testnet's bootstrap cap and SN25's, which its treasury policy requires; a later policy lowers it as breadth grows | v454 retains v452's getter behavior which ignores the retained storage item; validators must apply the signed cap and analysis must reject finalized violations. A low-single-digit cap requires enough positive recipients and should become native when the runtime supports it (§8.4). |
| `commit_reveal_weights_enabled` | **true** | weights carry the subjective quality signal — anti‑copying (§10) |
| `liquid_alpha_enabled` | **true** | reward validators who back good pools early (§10) |
| `immunity_period` | **high (≫ 4096 default)**, and **> reveal interval** | protect new pools **and new top-level miners** (the §8.4 breadth-sampling dip risk); must exceed `commit_reveal_period × tempo` |
| `min_allowed_weights` | **1** | a validator scores all miner UIDs (pools + top-level miners); avoid the 1024 default |
| `weights_version_key` | bump on scoring‑logic upgrades | force validator‑software upgrades (§10) |
| `serving_rate_limit` | default 50 | axons optional (custom HTTP protocol, §16) |
| `registration` | burn‑based, `min_burn`/`max_burn` tuned | Sybil cost on pool, fleet, validator, and escrow hotkeys; automation uses the runtime limit call and every burn is plan-bounded |
| `bonds_penalty` / `alpha_low`/`alpha_high` | tune (Liquid Alpha) | shape early‑discovery reward vs. stability (§2.2) |

> Several genesis defaults are governance‑mutable and have drifted from docs (e.g. testnet's global
> `tao_weight` is 1.8% while the v454 fallback retained from v452 is about 5.27%; `max_validators` is 64 vs 128;
> the `commit_reveal` default flipped). Query and compatibility-gate the live finalized values; set
> only owner-controlled fields explicitly and do not rely on documented defaults (§16 checklist).

### 15.2 Contract / economic parameters

| Parameter | Symbol | Suggested | Notes |
|---|---|---|---|
| Epoch length | `T_epoch` | 50 400 blocks (7 d), on mainnet from epoch 1 (epoch 0: 7,200 blocks) | settlement period |
| Operator data deadline | — | +4 h (1,200 blocks) | `README.md`; the root commit window, unchanged in v2 settlement |
| Close window | — | v1: 120 blocks after the epoch end; **v2: the 1,200-block commit window** | a v2 close missed beyond it defers the epoch with zero funding, and the next close captures its emission for later earners (§12.3) |
| Challenge window (v2) | `CHALLENGE_BLOCKS` | **14,400 blocks** (≈ 48 h) from the commit, a constant of vault2 | no v2 root finalizes earlier or with a path demand open; needs `T_epoch` ≥ commit window + 14,400 (mainnet's 50,400 holds; testnet's 360-block cadence does not), checked at pool registration; every later v1 policy must keep it, which no contract checks (§5). v1 instead finalizes 14,400 blocks after the epoch end |
| Demand fee (v2) | `demandFeeWei` | 750,000 rao (7.5·10^14 wei): one answer's gas at the planning fee, 149,074 gas at 5 gwei, rounded up; a nonzero multiple of 10^9 wei | immutable in vault2 and kept by it, never paid to the answerer; the runtime moves only whole rao of a call's value. The rehearsal measured an answer at depth 29 at 149,074 gas and a demand at 98,268. Each account of each finalized root can be demanded once per pending root, so the possible demands grow with the number of roots, each paying the fee |
| Root approval (v2) | — | owner-admitted auditor addresses (≤ 16); **quorum 1 at launch** (the validator); approve before the end of epoch `e+1` | a root still unfinalized at the end of epoch `e+1` can be replaced by the next commit, and its epochs' funding stays for that root (§13.1) |
| Claim expiry | — | v1: `claim_ttl_epochs` 8 + `claim_grace_epochs` 1; **v2: never** | policy v1 stays in force: `claim_ttl_epochs`, `claim_grace_epochs`, `expired_claim_action`, `shares_total_bps`, `rounding` and `deposit.usage_artifact` keep their meaning for v1 epochs and the v1 carry sweep and are **inert for v2 epochs**, which allocate in rao by `coldkey_usage_largest_remainder_rao_v1` and take deposit usage from the signed usage statement (§7.1) |
| Ledger bounds (v2) | — | ≤ 96 covered epochs per root; header ≤ 256 KiB (about 3,500 chunk references); 131,072 rows per chunk (48-byte account rows, 6 MiB; 57-byte provider rows, ≈ 7.1 MiB); ≤ 2^29 accounts per pool in vault2's tree | design target and activation gate: ≥ 1,000,000 providers per pool; the validator fetches at most `--ledger_max_bytes` of new objects per root (default 2 GiB, a runtime flag) |
| Reserve hotkey | — | the owner‑validator hotkey, fixed when the reserve sink is created (an immutable of `STReserveSink`) | the buyback reserve's staking target (§7.4); run its delegate **take = 0** so reserve yield compounds whole |
| `φ` / `ω` / effort‑claim deadline / dispute window | — | — | **out of scope** — parked bounty design (§9.3/§13.6, D29) |
| **Head share** | `θ` | governance; start ~0.3, ramp | fraction of the providers' share `μ` of the 41% miner emission to top-level miners; `1−θ` to pools — **the load-bearing new dial** (§8.5); from `E_c` the three shares below replace it |
| **Capture the flag shares** (from `E_c`) | `s_pool` / `s_ctf` / `s_head` | **4/10 / 1/10 / 5/10** of `μ` | fixed by the signed amendment entry v2, not by the policy; CTF gets exactly 1/10 when its channel is non‑empty, and while no head fleet is weighted the head's share goes to the pools only (§8.5, §8.7) |
| **CTF slots** (from `E_c`) | `S` | 5 provider and 5 extender slots in each of the 249 ISO 3166‑1 countries: **2,490** | each worth the epoch's fresh CTF funding ÷ 2,490; candidates present for at least half of the epoch; one ticket per /29 or /48 unit; drawn on the first drand quicknet round at least 60 s after the census commitment; unwon slots carry, and each winner takes its slot and a fifth of its pot (§8.7) |
| **Momentum** (provider share) | `μ` | 1/10 on SN25 at launch, pinned by the signed treasury policy | the providers' share of the native miner allocation; `1 − μ` goes in equal parts to `ur‑reserve`'s recipient UIDs 170 and 250, and the whole row when no provider has weight; changing it takes a newly signed treasury policy (§8.5) |
| Coverage weighting | — | (parked bounty design only) | up‑weights under‑sampled / weak providers (§9.3) — not in v1 (D29) |
| Validator min stake | — | governance | permit qualifier + Sybil ante (§9.7) |
| **Deposit‑rate schedule per conviction tier** | `rate(tier)` | governance, **off‑chain published** in the signed price sheet, with the signed policy's schedule in force until a signed entry takes effect; zero tier = baseline; each component priced in every tier or in none; all zero only under `zero_rate_action: equal_demand`, the launch mode, or a signed entry of all zero rates (§7.1) | α‑per‑usage by conviction; validators read it to weight (§7.3, §8.1). Staking lowers the rate — the onboarding/alignment lever (D25). |
| **Per‑epoch deposit cap** | `cap` | named by the signed entry in force, else the signed policy's `epoch_cap_rao_per_operator` | caps the formula per operator; the coordinator's `epochDepositCapRao`, set through `schedulePolicy`, must be at least this, or validators stop (§7.1) |
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
into the reserve (§7.4, §12.4). The providers' share `μ` of the 41% miner share (1/10 on SN25 at launch) is
split **head/tail by θ** (§8.5) — steered by validator weights to top-level-miner UIDs (native) and NO pools
(Merkle), and from `E_c` three ways, with 1/10 to the NOs' CTF pools (§8.5, §8.7); on SN25 the rest goes to
`ur‑reserve`'s two ordinary recipient UIDs. None is withheld to an owner
key (which would trigger the post-Spec-421 `miner_burned` penalty).

---

## 16. Implementation plan

### 16.1 Components

1. **ST contract set (Solidity, Cancun / 0.8.24).** Non-upgradeable `STReserveSink` and
   `STSettlementVault`, UUPS `STCoordinator`, exact runtime-454 bindings retained from v452 (`0x402`, `0x403`,
   `0x804`, `0x805`, `0x808`), OZ Merkle verification, scoped roles and scheduled policy (§6). The reviewed
   artifacts and deployed runtime hashes are release-locked. (No effort verifier — §9.3/D29.)
2. **Subnet convergence.** Validate the supplied, wallet-owned existing testnet netuid; explicitly set
   and verify every launch hyperparameter (§15.1); during installation the immutable vault first
   limit-registers its claims-escrow hotkey under its own mapped coldkey, and coordinator initialization
   fails unless that live registration exists. As each NO onboards, the immutable vault calls
   `registerLimit` for its **miner‑pool UID** with the reviewed rao ceiling; stand up an initial set of
   **validators** (owner-run at first) so consensus has measurement from day one. At the SN25 mainnet launch
   that set is one UR validator scoring UR's own pool, with no independent cross-check (owner decision,
   October 7). The release remains one mechanism. **Top-level miners self-`register_limit`** their own
   (provider-owned, not contract-owned) UIDs with a locally approved ceiling and publish the §11.4 binding.
3. **Validator software (independent).** Stake α; run `VALIDATOR.md` trails; each tempo score **both tiers** (pools `implied_usage × quality` —
   implied usage = audited usage at the baseline rate, the deposit audited from the coordinator's finalized counters at each pass;
   head on its **routable‑IP breadth score**), read the `client_id ⇄ hotkey` binding (§11.4), split the
   providers' share by θ (on SN25 the signed treasury policy gives providers 1/10 of the row and `ur‑reserve`'s
   recipient UIDs 9/10, §8.5), and submit
   commit-reveal weights (standard Bittensor validator loop → native dividends) — **no central
   keeper sets weights**. A separate **permissionless settlement poke** triggers per-NO finalization after
   the +48h window. (No trail‑proof submission — the effort bounty is out of scope, §9.3/D29.)
4. **Network‑Operator software.** Runs the privacy servers + the `VALIDATOR.md` `/verify` server
   (SEED/EXTEND/FINAL, poisoning, idempotency, the four Ed25519 signatures, the egress‑IP index);
   while the price in force is nonzero, stages and `deposit`s through its isolated hotkey/signer as early in each
   epoch as it can, retrying until the deposit lands
   (conviction stake; no contract weighting, D25; none at the zero‑price launch, §7.1); for release 1.0 epochs
   (before E_s), computes provider reliability + payout list, publishes a
   content-addressed artifact, commits the **`payoutRoot`** (fractional shares) and serves leaves; from E_s it builds
   and publishes the v2 ledger instead (item 8), and after each epoch's root commit a signed usage statement built
   from the committed header's provider chunks, with the user count taken at the epoch boundary; it sizes the next
   deposit from that statement (§7.1). (No validator pool — it co‑signs trails as the `/verify` server.)
5. **Provider software.** Carries ingress/egress; registers a `client_id`; for release 1.0 epochs, verifies its
   payout leaf against `payoutRoot` and calls the immutable vault's `claim` (from E_s its coldkey's account is
   claimed from vault2, item 8). **If it reaches the top ~200:**
   limit-registers its own UID, publishes the dual-signed `client_id ⇄ hotkey` binding (§11.4), and
   earns **natively** (no claim, §8.4).
6. **Validator client (was "verifier").** Stake α; run `/verify` trails; submit commit‑reveal pool
   scores (native dividends); independently reject stale/invalid bindings. (No `registerValidator(vpk)` /
   `submitTrails` / `claimValidator` — those belong to the parked bounty design, §9.3/D29.)
7. **Indexer/explorer** (planned; not built). Surfaces `D_n`, pool quality `Q_n`, consensus weights, vtrust, the
   **independent‑validator stake share**, the **buyback reserve + demand ratio `R_e`** (§12.4), and
   roots — the public audit surface.
8. **Version 2 payout settlement (`VERSION2-FINAL.md`; epochs from E_s).** Each epoch's pool funding is
   allocated by usage alone and credited to per-account cumulative balances that never expire; the release
   1.0 contracts above are not modified or upgraded.
   - **Contracts.** The immutable `STSettlementVault2` ("vault2", `evm/src/STSettlementVault2.sol`) and the
     owner-upgradeable UUPS `STSettlement2` (`evm/src/STSettlement2.sol`), with the `LedgerTree.sol`
     sum-tree library (`evm/src/LedgerTree.sol`, the twin of `merkle/ledger_sum_tree.go`; same pinned
     toolchain). A new v2 phase of `sn-mainnet bootstrap-contracts` creates vault2 and registers its escrow
     (one burn) at the very next nonce, back to back (§14), then creates the `STSettlement2` implementation
     and the proxy with `initialize` and sets vault2's coordinator once, and can then create the optional
     claim batcher (`evm/src/STClaimBatcher.sol`); it completes only on readback of exact runtime code,
     getters and storage slots.
   - **Ledger.** The `ledger` package serves the operator, the auditors and `snclaim` alike: the canonical
     header and chunk codec, the root build, the nine audit checks and the §11.3 demand set.
   - **Operator server.** Builds each root's v2 ledger (provider rows per covered epoch, accounts, sum
     tree, header), publishes the chunks and then the header, and closes, commits and finalizes through
     `STSettlement2`. It answers every data-availability demand with `checkPath` and serves the public
     `GET /sn/ledger/object` and `GET /sn/ledger/account`. Its signed operator gas policy is re-signed for
     v2: a capturing close uses about 226,000 gas on runtime 478, but the limit a stake move needs grows with
     vault2's staking-hotkey list, which third parties can lengthen before the close is included, so the
     server signs every close at no less than 3,200,000 gas (what a capturing close needed through runtime
     477), capped at the policy's `maximum_gas`, and today's `maximum_gas` is 1,000,000 (`VERSION2-FINAL.md`
     §8.1, §13; `docs/spec/runtime-478-audit.md`).
   - **Validator.** From the pool's `firstEpoch` it weights the pool UID resolved through
     `STSettlement2.settlementAt`. It audits each pending root's ledger and, from its admitted auditor
     address, approves the root only if every check passes, challenges a provable fault with `checkPath`,
     and demands a value-weighted sample of 256 previous accounts (all of them when there are fewer) with
     `demandPath` when the objects are missing or do not reproduce the root. Each demand names the newest
     finalized root whose ledger the validator holds and carries the account's proof against it. The sample
     doubles for each earlier root over the same predecessor that was replaced or deleted with a demand
     unanswered, and those unanswered accounts are demanded again.
   - **Claims and apps.** The miner claim daemon v2 (`provider claim-daemon-v2`), beside the unchanged v1
     daemon, claims an account whenever its claimable amount reaches the daemon's threshold (1.9 α by
     default, never below the 0.0152 α transfer floor). `snclaim v2` provides `status`, `claim`
     (one account, or with `--all` every account of a coldkey) and `check --all` (with `--challenge` and
     `--demand`), and `snclaim withdraw --vault v1` withdraws v1 claim credit. Given the claim batcher's
     address, `snclaim v2 claim --all --batch` and the daemon claim two or more due accounts in one
     transaction, each verified and preflighted alone first; UR runs `snclaim v2 check --all --challenge --demand` as an independent watcher on a
     host other than the validator's. Through the SDK the apps (Android, Apple, Windows, Linux and ur.io)
     show the v2 finalized, claimable and paid amounts, a pending root as pending, v1 balances
     separately, and "unavailable" rather than zero when the proof service cannot be reached, and from
     activation they claim v2 accounts: each claim goes only to the pinned vault2 address, after the SDK
     verifies the account's proof against vault2's on-chain root, and signs it at the larger of
     `eth_estimateGas` plus a quarter and 2,600,000 gas, because on runtime 478 a claim's estimate follows
     vault2's staking-hotkey list, which third parties can lengthen before inclusion. A v2 claim needs about
     0.02 TAO of gas on the device's gas key.

### 16.2 The chain is identity + stake + weights only

Per current Bittensor practice, **`serve_axon` is optional**: the runtime stores but never interprets
the axon protocol, and Yuma consumes only weights + stake. The UR network's transport is the custom
`VALIDATOR.md` HTTP protocol; participants discover each other out‑of‑band (the NO directory in
`README.md`) or via the commitments pallet, and touch the chain only for **registration, the contract,
weights, and emission**. This is a supported, common pattern (model‑commit subnets, orchestrator
subnets) — no Synapse/dendrite required.

### 16.3 Milestones

*(D28: v1 is **testnet-first** — the ladder below (M0 → M3) runs on **testnet** (chain 945 /
test.finney); **mainnet is the later Phase‑E promotion** (M3), planned behind a clean testnet run, after
which the M4+ production phases run on finney. The owner closed the testnet campaign on 2026‑09‑28 with
known exceptions: its last attempt, R48, completed no acceptance epoch (`sim-testnet/FINAL-4.md`), so M2's
and M3's testnet criteria were never met, and SN25 launched on 2026‑10‑08 from mainnet qualification
(`mainnet/MAINNET.md`, `mainnet/LAUNCH.md`). The SP‑1/SP‑2/SP‑3 harness is endpoint‑parameterized, so
it re‑targets with zero code change. Operational detail: `docs/LAUNCH.md`.)*

For every M0–M3 integration/soak run, the release‑locked adversarial campaign is
part of the normative test environment, not a separate quiet‑path exercise. Its
actors start before the happy path, remain active through weights, faults,
settlement, claims and final reconciliation, and stop afterward. The campaign
covers known Yuma/weight‑copying/liquid‑alpha and identity‑churn behavior,
published Subtensor advisories and open runtime/economic risks, RPC/archive
inconsistency, operator verification replay/poison/resource pressure, artifact
equivocation, custody/authorization and dependency failure. Shared‑testnet
activity is limited to our identities/netuid, loopback services and capped reads;
chain‑wide exploit reproduction runs only on the pinned local runtime. A release
run requires interleaved controls, bounded load, zero unexpected errors, explicit
latency/resilience evidence and a root‑caused disposition for every anomaly. The
v1.0 release catalogue contains 61 mandatory vectors, including all published
Subtensor advisories and the known Bittensor SDK missing‑signature,
finality‑era‑expiry, plaintext unauthenticated transport and constant‑body‑hash
failures. Every vector must name a checked‑in oracle/test and produce at least one
of its own declared metrics during the overlapping campaign; actor health alone
is not evidence. Simulator‑owned PostgreSQL, Redis and the workload RPC path are
faulted on a non‑overlapping block schedule while happy‑path traffic continues;
shared Subtensor and MinIO infrastructure is observed but not destructively
faulted.

1. **M0 — Rehearsal + probes.** (a) **SP‑3 localnet** (docker subtensor pinned to the reviewed runtime,
   fast blocks): the full convergence dry-run — deploy, pool-UID registration +
   custody/move α (`0x805`), **≥ 2 validators** scoring `implied_usage × quality` under commit‑reveal
   through native Substrate CRv4 so consensus/clipping/vtrust behavior is exercised, deposits → reserve, the epoch
   lifecycle, and the failure drills (pause, missed commit, upgrade‑under‑fire, sweep retry).
   (b) **Testnet dust probes** (chain 945, against the supplied existing testnet netuid): the SP-1
   battery — custody semantics, rao units, `0x402`/`0x403`, the `0x80c` address mapping,
   metagraph/neuron/staking, exact
   move/transfer conservation, commitment replace/restore, and **reserve-hotkey
   dividend auto‑compounding + take** — plus SP‑2 `check-metadata` against the test.finney
   runtime (`docs/LAUNCH.md` Phases A/B).
2. **M1 — Testnet subnet convergence (Phase 0 governance from block one).** One approved,
   content-hashed `sim-testnet` plan validates ownership, applies defensive hyperparameters, registers
   scoped UIDs, and deploys under a dedicated testnet single owner with **short
   epochs** and a **dust deposit cap** (D‑3); `start`; first CRv4 commit; then prove ON TESTNET with
   tiny values: isolated `deposit` → `ReservePrincipalAdded` (full amount into the immutable sink), miner emission
   accrual to the vault-owned pool UID, **per-NO** vault `claim` against `payoutRoot` ×
   emission-only entitlement end-to-end. **Head:** register a provider-owned **top-level-miner UID**,
   publish the §11.4 binding, and verify **routable‑IP‑breadth** native steering split from the pools by θ.
   The demand-deposit cap does **not** size validator stake. Bootstrap the reserve validator to a
   majority of registered α and fund a separately keyed independent validator position; bind both exact
   transfers to a finalized runtime minimum/price snapshot and stop before signing if source stake,
   conviction locks, miner collateral, registration, retained-source balance, or the reserve-majority
   postcondition has drifted. Testnet values may be simulator-funded, but the positions have distinct
   coldkeys/hotkeys and the independent validator executes and signs its own scoring lifecycle.
3. **M2 — Buyback reserve verified live (testnet).** Five consecutive 300-block testnet epochs green: dividends
   **auto‑compound** onto the reserve stake (`getStake(reserveHotkey,sinkColdkey,netuid) > principal`), the **one‑way
   invariant** + on‑chain audit hold, and the upgrade/pause drills leave finalized claims and the
   immutable reserve untouched. (The effort-bounty rail — `registerValidator`/`submitTrails`/`claimValidator` — is
   **out of scope**, so there is no such milestone in v1; it stays parked, §9.3/D29.)
4. **M3 — Ramp on testnet, then promote to mainnet (Phase E).** schedule the
   testnet-only 360-block/approximately-72-minute policy (+60-block root window,
   +180-block finalize offset and +6-block close grace, F2‑snapshotted so in‑flight epochs are untouched)
   and require three consecutive fully observed UR blocks under concurrent
   adversarial load; the deposit cap raised stepwise toward the sized policy;
   settlement‑poke automation; the reference rate + sourcing commitment
   published; the `R_e` demand‑ratio dashboard live (§12.4). Then generate and
   rehearse the separate mainnet policy: a one‑day 7,200‑block epoch 0 (+4h/+8h
   windows), then 50,400‑block/seven‑day epochs with reviewed +4h/+48h windows
   from epoch 1. After the three clean testnet UR blocks and MR closure,
   **promote to mainnet**: re‑run the M0 probes + M1 genesis against **finney** (chain 964), now under the hard‑gate
   posture (real TAO; genesis is one irreversible window). By owner decision (October 7) the SN25 mainnet genesis has
   one operator and one validator, so it has no separately keyed independent validator position. The M4+ production
   phases run on mainnet.
5. **M4 — Production rollout (one mechanism; Phase 0 governance, §6.4.1).** Full parameters, **quality‑factor swing
   capped until the independent‑validator stake share is healthy** and `VALIDATOR.md` §10 advances
   (§12.3).
6. **M5 — Harden coordinator governance (Phase 1, §6.4.2):** timelock (≥ 1 epoch) on
   upgrades/params; immutable custody and the pause-only guardian already exist from release 1.0.
7. **M6 — Decentralize further (deferred, §6.4.3):** trustless oracle (§13.5), permissionless bonded
   admission, on‑chain governance; advance the `VALIDATOR.md` §10 roadmap.

**Version 2 (`VERSION2-FINAL.md`)** changes payout settlement on mainnet beside the ladder above. Its
runtime rehearsal (gate 2 of §16.4) runs on a node built from the source of mainnet's runtime (475 at
first; rerun and passed on 477 and on 478, mainnet's runtime since 2026‑10‑13) (`scripts/runtime-rehearsal/`, a
one-node `fast-runtime` dev chain with mainnet's EVM chain ID, driven by
the Go harness in `rehearsal/`), not testnet: testnet's 360-block cadence cannot satisfy
`epochBlocks >= rootCommitWindowBlocks + 14,400`, and the testnet campaign closed on 2026-09-28.

- **Version 2 phase A — activation at E_s** (`VERSION2-FINAL.md` §12.1). Ships §16.1 item 8. With gates
  1–4 of §16.4 met and the gate 5 software released, deploy vault2 and `STSettlement2` through the v2
  contract phase, with readback; adopt the validator config, re-sign the treasury approval and the operator
  gas policy, ship and configure the server, and check that the validator runs the adopted config and the
  server the v2 build.
  One to two days before E_s the owner Safe calls `registerOperatorPool(1, poolHotkeyV2, maxBurn, E_s)`,
  the commitment point: it falls inside the 21,600-block registration immunity, and if it fails nothing
  has changed and E_s moves. From E_s the validator weights the v2 pool UID, the server runs the v2 driver
  and the v1 driver stops; epoch `E_s − 1` completes on v1 in its normal windows, and v1 claims stay open
  until each v1 expiry.
- **Later version 2 phases** (`VERSION2-FINAL.md` §16). Roster and evidence (signed work requests; the
  validator verifies consent chains); phase B, measurement (sampling frame, VRF cohort, stratified
  estimator, head budget, validator state bounds); phase C, the v1 carry sweep into vault2 at or after
  `E_s + 8`, once the last v1 expiry has passed, plus a second operator, a fractional approval quorum and
  operator bonds.

### 16.4 Pre‑launch verification checklist (load‑bearing live values)

*(D28, release 1.0: these checks run against **testnet** (chain 945 dust probes) + the SP‑3 localnet — they are the
release 1.0 testnet gates (`docs/LAUNCH.md`, `sim-testnet/README.md`), and all must be green before the supplied
subnet is modified; re-run them
against finney at the Phase‑E mainnet promotion.)*

- Precompile addresses/ABIs at the pinned Subtensor release (address mapping `0x80c`, Ed25519 `0x402`, sr25519
  `0x403`, Metagraph `0x802`, Neuron `0x804`, Staking V2 `0x805`, Alpha `0x808`) using good/bad vectors and
  value-bearing exact-delta tests. CRv4 is checked through native Substrate metadata/calls.
- `tao_weight` (testnet expects raw `332041393326771929`, i.e. 1.8%; observe mainnet independently),
  `max_allowed_validators`, `min_allowed_weights`,
  `commit_reveal_weights_enabled` default, `SubnetOwnerCut` — query live, set explicitly.
- Existing-netuid ownership, capacity and every registration burn bound; prove both native
  `register_limit` and EVM `registerLimit`, including funded-mirror/zero-precompile-value semantics.
- Confirm `transferStake`/`moveStake` within‑netuid are slippage‑free on the live runtime; confirm the
  staking precompile's "contract address = coldkey" custody semantics.
- Confirm a validator earns a permit at expected stake and that its **native dividends** accrue to
  its own hotkey (no contract capture). Use an **independent validator** where one exists; at the
  SN25 launch the only validator is UID 1, the SN25 owner hotkey (coldkey `ur-owner`); `ur-mainnet` holds
  root seat UID 21. Confirm delegated stake on the reserve
  hotkey **auto‑compounds** to the contract coldkey's stake and that hotkey `take` behaves as expected
  (target take = 0, §7.4).
- Confirm **`max_allowed_uids` = 256 is a hard ceiling** and `mechanism_count = 1` (a 2nd mechanism halves
  UID space, §14); confirm a **provider-owned top-level-miner UID** earns **native** emission to its own
  coldkey (no take, not shared) and that the §11.4 binding verifies via `0x402` + `0x403`, a matching
  finalized commitment and live UID.
- Confirm vault-owned **pool UIDs are not treated as owner/immune**, so the head/tail θ split does **not**
  trigger the post-Spec-421 `(1 − miner_burned)` penalty (§8.5); enforce the signed
  `max_weight_limit_u16`, audit finalized vectors, and set a high `immunity_period` to protect the head
  from breadth-sampling-dip eviction.

**Version 2 activation gates** (`VERSION2-FINAL.md` §14; test IDs from its §15). Version 2 phase A
activates (the owner Safe sends `registerOperatorPool`) only when all six hold; E_s is the first epoch that
starts at least two days after the last one passes.

1. **Contracts.** The Foundry suite is green at its coverage bars, with `forge fmt --check`,
   `forge build --deny warnings --sizes` and `scripts/test-solidity-static.sh` clean; sizes and margins
   are recorded; an independent review of vault2, `LedgerTree.sol` and `STSettlement2` is resolved.
2. **Runtime rehearsal (R-1 to R-6).** The exact bytecode runs on a node built from the source of mainnet's
   runtime (it has run on 475, 477 and 478), which phase A builds (`scripts/runtime-rehearsal/`, harness in
   `rehearsal/`). Any runtime change on
   mainnet repeats this gate.
3. **Ledger.** The Go suites are green, including the million-provider fixtures (GL-10, SV-6) inside
   budget.
4. **End-to-end.** The E suite passes (E-1 to E-7).
5. **Software released and contracts deployed.** The server, validator, claim daemon, `snclaim`, the SDK's
   v2 display and claims and the apps' v2 display and claims are released, and the v2 contract phase of
   `sn-mainnet bootstrap-contracts` has run with readback.
6. **Approvals.** The validator config is adopted, the treasury approval and the operator gas policy are
   re-signed, and the server is configured.

---

## 17. Open questions from `seed/INCENTIVES.md` — resolved

| Question | Resolution |
|---|---|
| **How is oracle data stored/charged on Subtensor? Can the NO payout table be a Merkle tree so each miner validates its payout without storing it on chain?** | **Yes.** Release 1.0 commits a 32-byte Merkle root plus artifact hash per NO/epoch in the coordinator; immutable entitlement state gates claims; canonical bulk bytes live in `server/blob` MinIO and are served by the public server API. Each provider verifies an `O(log N)` proof (§11). From E_s, version 2 instead commits the root of a Merkle **sum tree** over the pool's cumulative accounts, with its total, leaf count and ledger-header hash, into vault2 through `STSettlement2`; the ledger objects are served at `/sn/ledger/object`, and each account verifies an `O(log n)` proof of `(hash, sum)` siblings. The commitments pallet anchors fleet manifests, not payout bulk data. |
| **Are smart contracts standard EVM?** | **Yes** — Frontier EVM, Cancun, Solidity 0.8.24: chain 945 testnet and 964 mainnet. Subtensor precompiles let the contracts stake/transfer α and verify binding identities; validators submit CRv4 through native Substrate extrinsics (§2.5). |
| **How to adapt to standard BT payout formulas?** | Independent validators set standard Yuma weights `= implied_usage × quality` (implied usage = audited usage priced at the baseline rate, the deposit audited from the coordinator's finalized counters + the signed price sheet — contracts weigh nothing, D25) on per-NO miner-pool UIDs; the chain delivers emission to vault-owned pools (re-split by Merkle claim) and native dividends to validators. A second head tier holds provider-owned UIDs and receives native emission by split-adjusted routable-prefix breadth under θ (§8.4–8.5). |

---

## 18. Glossary

- **NO (Network Operator)** — runs servers; operates **one miner-pool UID** (vault-owned) but holds no
  emission; posts isolated conviction deposits; runs the `/verify` server (co-signs trails); commits the payout root for its
  providers. No validator.
- **Miner-pool UID** — the on-chain miner: one per NO, immutable-vault-owned (from E_s, a new vault2-owned UID
  that validators resolve through `settlementAt`). The 100k+ providers are **inside** it (not UIDs) and are
  paid by Merkle claim.
- **Provider / miner (tail)** — carries traffic; inside a NO's miner pool (the on-ramp tier); **claims its
  α directly from the contract** — per the NO payout root for release 1.0 epochs, and from E_s through its
  payout coldkey's **account** in vault2. Can **graduate** to a top-level slot (§8.4).
- **Top-level miner (head)** — a top ~200 **fleet** (an operator of many routable exit IPs) that holds
  **its own miner UID**, steered **directly** by validators on its **split-adjusted distinct routable
  egress-IP count** (`score`, §8.4) and paid **natively** (no contract custody, no Merkle claim); matched
  to its UID by the §11.4 binding; maintained by deregistration churn.
- **`score(u)`** — a top-level miner's split-adjusted breadth: the count of distinct routable egress-prefix
  hashes (/29 IPv4, /48 IPv6) across its bound `client_id`s, each split equally among every bound fleet
  observed on it; its head weight is the score's EMA (`head_score_ema` 1/4), over the top 200 fleets by that
  EMA (§8.4, D27).
- **fleet `client_id`s ⇄ hotkey binding** — the dual-signed (client Ed25519 + hotkey sr25519) association a
  top-level miner publishes (commitments pallet + contract anchor) binding its fleet's `client_id`s to its
  UID, so validators count its routable IPs (§11.4).
- **θ (head share)** — the governed fraction of the providers' share `μ` of the 41% miner emission steered
  to top-level miners; `1−θ` goes to the pools (§8.5). From `E_c` the three shares of §8.5 replace it: pools
  4/10, CTF pools 1/10, head 5/10.
- **μ (momentum) / `ur‑reserve`** — the providers' share of the native miner allocation, 1/10 on SN25 at
  launch, pinned by the signed treasury policy; validators weight the other `1 − μ` in equal parts to the
  recipient UIDs of `ur‑reserve` (SN25 UIDs 170 and 250), a 2‑of‑3 native treasury multisig separate from
  the buyback reserve (§8.5).
- **Validator** (was "verifier") — an **independent** Bittensor validator UID: stakes α, runs
  `VALIDATOR.md` trails, scores pools, and earns **native dividends** — its **only** reward (a validator
  effort bounty is out of scope, §9.3/D29). No NO, no pool — the disinterested consensus baseline; the
  owner is the majority validator early (§9.2).
- **Deposit / conviction stake / buyback reserve** — an NO's deposit (∝ usage) is locked in full to the
  **reserve hotkey** (never distributed, dividend‑compounding, §7.4); its cumulative locked α = its
  **conviction**, which sets its **tier → deposit rate** (§7.3). Coordinator counters enforce caps but do
  not weight; finalized `Deposit`, `ConvictionAdded`, and sink-principal events are the portable record (§7.5, D25).
- **implied usage** — NO `n`'s audited usage priced at the conviction‑zero rate, truncated as its capped
  deposit is (§8.1): the demand signal validators weight for the pool tier; staking for a lower rate
  lowers the deposit and, below the cap, leaves the weight tracking real usage rather than raw α; at the
  cap the demand is `cap × baseline_n / owed_n` (with one priced component, `cap × rate(0) / rate(tier_n)`),
  so a lower rate raises it.
- **Price sheet / signed entry / price in force** — the file at `https://ur.xyz/price.yml`. Only the entries of
  its `signed_schedule`, each signed by the pinned key, set rates and the cap; the **price in force** for an
  epoch is the latest entry effective by then, or the signed policy's schedule when there is none (§7.1).
- **Usage statement** — a NO's signed statement of one epoch's provider chunks, `total_usage_bytes` and
  `total_users` (kind `epoch-usage-v1`): the usage that sizes and audits the next epoch's deposit, for epochs
  from E_s (§7.1).
- **Effort bounty** — a fee‑funded reward that *would* pay ∝ verified, coverage‑weighted, server‑assigned
  completed trails; **out of v1 scope** and **not a committed phase** — a parked future‑iteration candidate
  (§9.3, §13.6, D29).
- **ST contract set** — release 1.0's UUPS policy coordinator plus an immutable settlement-vault coldkey and
  an immutable reserve-sink coldkey, beside an immutable evidence journal that holds no stake; version 2 leaves
  it unmodified and adds **vault2** and **`STSettlement2`**
  beside it. Custody is physically separated from upgradeable policy. **Not** the validator (§9–§10).
- **`D_n` / `Q_n` / `ŵ_n`** — NO's epoch deposit total (the coordinator's counter, which the event log
  reproduces) / its consensus‑measured pool
  quality / its resulting validator‑computed weight (∝ `implied_usage_n × Q_n`, §8.1).
- **Validated path** — a completed `VALIDATOR.md` trail proof, id `keccak256(trail_id‖vpk‖server_key_id)`;
  self‑proving (NO `FINAL` + validator `vpk` sigs), verified off chain with Ed25519 (§9.1).
- **Epoch (7 d)** — application settlement period; **tempo (360 blk)** — chain weight/emission cadence.
- **Version 2 (v2) / `firstEpoch` / E_s** — the second version of payout settlement (`VERSION2-FINAL.md`).
  It settles a pool's epochs from its `firstEpoch`, fixed when the owner Safe registers the pool through
  `STSettlement2`; earlier epochs settle under release 1.0 (v1). **E_s**, the switch epoch, is pool 1's
  `firstEpoch`: the first epoch that starts at least two days after the last activation gate passes (§16.4).
- **vault2 (`STSettlementVault2`)** — the immutable v2 custody contract. Its coldkey owns the v2 pool hotkey
  and the escrow hotkey; it snapshots each epoch's funding, holds the pending root and every finalized root,
  runs challenges and demands, enforces the 14,400-block challenge window, and pays claims. It moves and
  counts only movable stake, never registration collateral (§6.3). Its guarantees hold whatever
  `STSettlement2` does (`VERSION2-FINAL.md` §4.2).
- **`STSettlement2`** — the owner-upgradeable (UUPS) v2 settlement coordinator beside the release 1.0 set. It
  reads operators, windows and pause from the v1 coordinator, holds the auditor set and approvals, and drives
  close, commit and finalize into vault2; `settlementAt(noId, epoch)` names the vault and pool hotkey that
  settle an epoch.
- **Ledger (header, chunks)** — a v2 root's published, content-addressed objects: a canonical JSON
  **header**, whose sha256 is committed on chain as `ledgerHash` and which lists every chunk by hash;
  **provider chunks** of 57-byte rows per covered epoch; and **account chunks** of 48-byte
  `coldkey ‖ cumulative` rows. Served at `GET /sn/ledger/object` and retained by the validator.
- **Account / cumulative / claimed watermark** — an **account** is one payout coldkey in one pool, at a fixed
  position in the pool's ledger. Its **cumulative** is the total rao that v2 roots have credited to it; it
  never decreases or expires. vault2's **`claimed` watermark** is the amount already paid, and the claimable
  amount is the latest finalized cumulative minus `claimed`. A claim pays `cumulative − claimed` to the
  account's coldkey, whoever sends it, and raises `claimed` to `cumulative`.
- **Sum tree** — the position-binding Merkle tree a v2 root commits (`urnetwork-ledger-sum-tree-v1`). Each
  node hashes both children's hashes and value sums, so the root binds the pool total, and a claim proof of
  `d` `(hash, sum)` siblings proves an account's coldkey and cumulative at its index.
- **Pending root / finalized root** — a pool has at most one **pending root**. Committed by the operator's
  root signer for an epoch `e`, it is deleted by a successful challenge, can be replaced by the next commit if
  it is still unfinalized once `end(e+1)` passes, and finalizes once a quorum of auditors has approved it,
  14,400 blocks have passed since its commit and no demand is open. vault2 keeps every **finalized root**;
  claims verify against them.
- **Auditor** — an owner-admitted EVM address (at most 16, with a quorum; at launch only the validator's,
  quorum 1) that audits a pending root's ledger and approves the exact root for epoch `e` with
  `approveDistribution` before `end(e+1)`.
- **Challenge (`checkPath`) / demand (`demandPath`)** — while a root is pending, anyone holding its tree can
  **challenge** it by opening its path at an account of a finalized root, any of the pool's, named by its
  index; a lowered value, a changed coldkey or an inconsistent node deletes it. Anyone without the tree can
  instead **demand** that opening on chain, proving the account against a finalized root it names, once per
  account of each root and pending root; the root cannot finalize until the operator answers with a clean
  `checkPath`, which a withheld or garbage node never allows.
- **Carry** — release 1.0: an expired or unallocated remainder, or a missed root's funding, folded into the
  same NO's next finalized entitlement (§8.3). Version 2: within one root, the funding of a covered epoch
  with no eligible usage or with inputs declared unrecoverable, allocated with the next covered epoch that has
  eligible usage (`VERSION2-FINAL.md` §6.4). Release 1.0's remaining carry reaches vault2 through the phase C
  sweep.
- **Payout list / `share_bps` / claim expiry (release 1.0)** — for epochs before E_s, the NO's per-epoch
  `payoutRoot` over `(coldkey, share_bps)` leaves allocating exactly 10,000 bps; a leaf claims
  `share_bps × total / 10,000` of that epoch's entitlement, and the unclaimed remainder expires after the
  snapshotted TTL+grace into carry (§8.2–8.3, §11.2). Version 2 replaces them with accounts that never
  expire.

*Terms of §8.6 (amendment approved 2026‑11‑10, Q7 decided 2026‑11‑11; in effect from `E_x = 8`):*

- **Extender** — a relay a provider runs on TCP and UDP 443 and a DNS carrier. It forwards only to its NO's
  own api and connect hosts, so a client that cannot reach the platform directly can reach it through one
  (`connect/EXTENDER.md`). Its record names the network that first stored its key and the provider client
  that activated it last. It earns as a contract party (J2–J3) and, under §8.6, its unit's share of
  its NO's extender pool bytes `Z`.
- **Extender party** — an active extender on either endpoint's connected connections when a contract is
  created. It joins the contract's participants with one equal hop share (`EXTENDER.md` J2–J3); on the
  payer's own network it earns nothing. It is one of the contract's `n_c` hops on every contract, platform
  contracts included (Q7).
- **Hop count `n_c`** (§8.6.2, Q7) — the number of a contract's participants: its service endpoint, its
  stream's intermediaries and its extender parties, each distinct client once, less the origin, platform
  endpoints and same‑network participants included. Each hop holds an even share of the contract's bytes,
  the remainder bytes going to the lowest client ids. An extender is one hop.
- **Intra‑platform contract** (§8.6.2) — a contract whose service endpoint is one of the NO's
  registered platform endpoints. The NO is not a miner in its own pool, so the platform endpoint's hop
  share is not provider usage: it counts toward `Z`, free or paid, and is all of the contract's bytes when
  the contract has no other hop. Every other hop, an extender party included, keeps its share (Q7). The
  contract escrows from the payer's data balance like any contract.
- **Intra‑network share** (§8.6.2) — the even share of a contract's bytes held by a participant on the
  origin's own network: the receiving client of a same‑network contract, or a same‑network intermediary or
  extender. From `E_x` it is not provider usage, which amends §8.2. Same‑network contracts escrow from the
  payer's data balance, and their shares count toward `Z`, free or paid.
- **Extender pool bytes `Z`** (§8.6.2) — an epoch's funded intra‑platform and intra‑network bytes, free and
  paid alike, leaving out the operator's own measurement and test traffic. A network adds at most what its
  data balance funded. `Z` counts in `total_usage_bytes`.
  - For weight it is capped at the epoch's eligible provider usage (`Z'`, `ζ = 1`), so extenders take at
    most half a pool.
  - `Z'` is split evenly over the extender units, then evenly over the eligible extenders in each unit.
  - With no eligible extender, `Z` is dropped.
- **Platform endpoint** (§8.6.2) — a client identity the NO itself runs to serve a platform feature.
  At `E_x` these are sidecar's edge platform clients: tlsedge, smtpedge and mxedge. The operator registers
  them, and they are recorded on a contract when the contract is created. A platform endpoint counts among a
  contract's `n_c` hops but never earns its share, which goes to `Z` (Q7).
- **Extender unit** (§8.6.2) — the keyed /29 IPv4 or /48 IPv6 prefix hash of an eligible extender's
  active IPv4 address, or of its IPv6 address if it has no active IPv4 address. It is §8.4's scoring unit,
  under the same granularity setting. Each unit holding an eligible extender takes an even share of `Z'`.
- **Active pool** (§8.6.2, E1) — the NO's extenders whose records are active and unrevoked, with at
  least one address the five‑minute challenge has not deactivated. The census takes it once, at the
  epoch's end boundary.
- **Eligible extender** (§8.6.2) — for one epoch, an extender that meets these:
  - E1, in the active pool at `end(k)`;
  - E3, one record per provider client and per address, in one unit;
  - E4, its provider client has a payout coldkey and no head binding;
  - E5, not on the NO's own networks.

  (E2, an uptime record, was removed by owner decision.)
- **Extender census / extender share row** (§8.6.2) — the census is the NO's per‑epoch,
  content‑addressed list of its extenders, each with its unit and its eligibility flags. It is
  operator‑attested and published as keyed prefix hashes, never addresses. A share row is the 57‑byte row
  that gives one eligible extender its bytes in the epoch's allocation: its unit's `Z'/M`, split among the
  unit's extenders.
- **`E_x`** (§8.6.7) — the amendment's activation epoch: epoch 8, by the owner's decision. It is fixed by
  a signed amendment entry, which must be signed before epoch 8 starts, and later changes go by entry too. It
  must be a future epoch whose provider rows no root has built yet, and earlier epochs keep their fixed
  inputs.

*Terms of §8.7 (capture the flag, decided 2026‑11‑11; in effect from `E_c = 8`):*

- **Capture the flag (CTF)** — a per‑country coverage auction beside the provider pool (`CAPTURETHEFLAG.md`).
  Each epoch every one of the 249 ISO 3166‑1 countries has 5 provider and 5 extender slots, drawn among the
  NO's eligible miners there. A tenth of the providers' share `μ` funds it (§8.5).
- **CTF pool** — NO `n`'s second vault2 pool, `2^32 + n` (UR's is 4,294,967,297), with its own vault‑owned
  UID. It settles, and its accounts are claimed, exactly like the provider pool's, and its winners are paid to
  the payout coldkey that the provider pool already selects.
- **Slot** — one `(country, role, rank)` prize, rank 1–5, so `S = 2,490` in all. Each is worth the epoch's
  fresh CTF funding ÷ 2,490 in rao, the remainder going one rao each to the first slots in slot order.
- **Pot** — what a country's unwon slots of one role carry forward. Each winner takes its own slot and a fifth
  of the pot carried into its country and role, and the rest keeps carrying.
- **CTF census** — the NO's per‑epoch, operator‑attested list of its CTF candidates: role, country, keyed unit
  and client id, with exclusion counts but no coldkeys or addresses. Its hash is committed on chain
  (`commitCtfCensus`) within 600 blocks of the epoch's end, before the drand round that seeds the draw exists.
- **Draw / ticket** — in each country and role, the up to five units with the smallest tickets win. Tickets are
  keyed by a seed from the first drand quicknet round at least 60 s after the census commitment, and a unit
  holds one ticket per role, whatever its size.
- **Pending award** — a winner's pot share while the carry has not come back before the epoch's close. It is
  paid in the first epoch whose carry is back.
- **Carry contract (`STCtfCarry`)** — the immutable, ownerless contract, one per CTF pool, to whose account
  each root pays the pots and pending awards. After the root finalizes, anyone calls its `recycle`, which claims
  that account and returns the α to the CTF pool hotkey for the next close; `recycledFor` records each return.
- **`E_c`** (§8.7.5) — capture the flag's first epoch: epoch 8, by the owner's decision, which is the extender
  pool's `E_x` too. It is fixed, with the shares and the CTF parameters, by the signed amendment entry v2,
  which must be signed before epoch 8 starts.

---

*End of WHITEPAPER.md version 2 — settlement v2 for epochs from E_s, specified in `VERSION2-FINAL.md` — on
v1.0: validator effort bounty removed from scope (v1 = native dividends only; the
`(X)`/`(Y)` bounty designs are parked, not a committed phase; §9, §13.6; decision D29 in
`WHITEPAPER_DISCUSS.md`) — layered on v0.4 conviction staking + validator-computed weights + IP-breadth head
(D25–D28), v0.3 deposits-as-buybacks (locked, dividend-compounding reserve; miner pay = emission-only; §7.4,
§12.4; D23), and the v0.2 two-tier miner side (pool on-ramp + direct top-level miners; §8.4–8.5, §10, §11.4,
§14; D16–D20). This document fixes the architecture and the formulas; the contract source, the
chain‑config tooling and the operator/validator daemons that implement them are built (§16).*
