# UR Subnet — Whitepaper

**A Bittensor subnet for a decentralized privacy network.**

Version 2 (launch specification with settlement v2). Target chain: Bittensor / Subtensor — **testnet first, then mainnet**
(D28). The mainnet subnet is **SN25 (netuid 25)**; the testnet campaign runs on netuid 521. Version 1.0
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
> the pools themselves** by `implied_usage × quality` (`implied_usage = deposit / rate(tier)`) from
> published data. The **head tier ranks fleets by split-adjusted distinct routable egress-IP count** (not
> traffic; shared IPs split among claimants), head weight ∝ that score. Validators run measurement with
> **guardrails off** (own rate). See §7, §8, §10, §12; prior layers: v0.3 buybacks (D23), v0.2 two tiers
> (D16–D20).

---

## Executive summary

The **UR Subnet** (Bittensor **SN25**, netuid 25) runs a **decentralized privacy/VPN network** entirely through on‑chain incentives.
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
path re‑liquefies it**. So a deposit does two things: it is the **demand signal** validators weight (§8.1)
and it **permanently removes α from liquid supply**, in proportion to real usage. An NO's cumulative
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
(§14); a governance split **θ** sets how much of the 41% miner emission flows to the head vs. the pools
(§8.4–8.5).

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
- **Real‑usage anchored:** a costly, revenue‑backed α **deposit** weights the cross‑operator split
  (validators compute `implied_usage × quality` off published data — the contract weighs nothing, D25);
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
- **Trust‑minimized custody:** no operator holds others' emission; everyone is paid by direct on‑chain
  claim against a committed Merkle root. Under settlement v2 a root is claimable only after a quorum of
  owner‑admitted auditors (at launch, the validator) approves it **and** a 14,400‑block window open to
  anyone's challenge or path demand passes; claims are permissionless and always pay the account's coldkey.
- **Validator data:** native dividends reward consensus‑accurate scoring; at launch the owner is the
  majority validator (by α holdings + the reserve) and runs the trail volume itself. A coverage‑weighted
  **effort bounty** is **out of v1 scope** (D29) — a possible future iteration, not a committed phase.
- **Progressive decentralization:** multisig + upgradeable → timelock + pause‑only guardian → broader
  governance.

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
  (`14cde6410fe8ec81a940e290c56f94a632a0988d`); settlement v2 targets runtime **475** (`d1718c99`) on
  chain 964, checked against `docs/spec/runtime-475-audit.md` and rehearsed on a node built from that
  source before activation.
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
   `deposit / rate(conviction tier)` — so **validators' evaluation drives the miner
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
   post (§7.3). The coordinator keeps only cap/audit counters; canonical `Deposit` and
   `ConvictionAdded` events are the public weighting record (§7.5, D25).

2. **Emission (Yuma consensus over NO pools).** Every NO has **one contract‑custodied miner‑pool UID**
   (all its providers). **Independent validators** — anyone who stakes α and runs `VALIDATOR.md` trails —
   compute **every** NO's pool score **themselves** as `implied_usage × measured‑quality` (implied usage
   = the NO's deposits ÷ its tier rate, §8.1) and submit those weights, so the **validators' evaluation
   drives the miner emission — the Bittensor way**. With many independent validators, **Yuma Consensus
   does real work**: stake‑weighted median + **clipping**, so no NO can inflate its own pool or knife a
   rival. Miner emission (41%) lands on miner‑pool UIDs **owned by the immutable vault coldkey** — no
   emission ever touches a NO's keys. **The 41% splits across two tiers** (§8.4–8.5): a governance share
   **θ** to **top-level miners** (the top ~200 fleets by **split-adjusted distinct routable egress-IP
   count**, as their own native UIDs), and `1−θ` to the NO pools above. Validator emission (41%) flows
   **natively** to the independent validators ∝ stake × vtrust.

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
          └────────── start in a pool ─▶ graduate to a top slot ─▶ fall back if breadth slips ──┘

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
- **Runtime precompiles used by the release contracts and conformance harness** (address → purpose):

  | Address | Name | What the contract calls it for |
  |---|---|---|
  | `0x…0009` | **Blake2f** | derive the exact H160 → AccountId32 mirror used by every contract coldkey |
  | `0x…0402` | **Ed25519Verify** | verify client consent for fleet binding and revocation |
  | `0x…0403` | **Sr25519Verify** | verify the registered Bittensor hotkey's fleet-binding signature |
  | `0x…0804` | **Neuron** | limit-register vault-owned pool hotkeys and prove live `(netuid, hotkey) → UID` bindings |
  | `0x…0805` | **Staking V2** | exact `getStake`, `moveStake`, and `transferStake` operations for isolated deposits, immutable reserve custody, emission capture, and claims |
  | `0x…0802` | **Metagraph** | read-only conformance/observation of UID 0 and metagraph identity; production settlement does not trust a point-in-time emission getter |

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

Release 1.0 is pinned to the deployed testnet **runtime spec 454**, transaction version 1, source tag
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
reuse is limited to v451/v452/v453/v454. A mandatory release gate resolves their pinned testnet blocks, authenticates
the on-chain `System.Code` hashes, obtains the exact chain or official-release Wasm bytes, and executes
`Core_version` plus `Metadata_metadata` in a pinned Wasm executor exposing only allocator, logging and hashing
host functions. Any attempted storage/offchain host call is a hard failure. The gate requires the exact
version tuple, code size/SHA-256/BLAKE2b-256, metadata size/SHA-256/BLAKE2b-256, complete SCALE decode and
FRAME metadata v14 recorded in `docs/spec/runtime-metadata-artifacts.json`. Pinned upstream metadata tests
without `TestExternalities` independently corroborate the result. Caches are per provider, singleflighted,
failure-free and hard-bounded to those four identities; historical identities remain read-only and cannot
satisfy the v454 signing boundary.
Secretless semantic replay intentionally downloads and records all three raw responses at every evidence
checkpoint from its signed v454 campaign start onward; carried pre-campaign receipts retain their exact
v451/v452/v453 identities instead of being relabeled. The continuous sentinel repeats complete version and
code-hash checks on both endpoints.

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

Version 2 settlement is pinned separately, to mainnet (chain 964) runtime 475, source commit
`d1718c99c34cf96abbf2bf09c0e9e48b945c76f5`. The runtime facts vault2 and `STSettlement2` (§3) rely on come
from the local v451–v454 sources and that source, checked against `docs/spec/runtime-475-audit.md`, and were
measured on a node built from that source (`scripts/runtime-rehearsal/`, measurements in its `README.md`).
Before deployment their exact bytecode must pass a rehearsal on that node (R-1 to R-6, `VERSION2-FINAL.md`
§15.8, driven by the Go harness in `rehearsal/`); any mainnet runtime change repeats it.

---

## 3. Roles and on‑chain identity

| Design role (`seed/INCENTIVES.md`) | On‑chain identity | Notes |
|---|---|---|
| **Subnet owner** (BringYour, Inc.) | owner coldkey + owner hotkey at **UID 0** (immune); deploys & governs the ST contract; operates the **reserve validator hotkey** (§7.4) | receives the 18% owner cut; the **majority validator** in the early network (§9.2); the governance **referee** (§9.6). |
| **Network Operator (NO)** | a contract registration (`noId`) with **one miner‑pool UID** (its providers, contract‑owned; from `E_s` a new pool UID owned by vault2). Holds **no emission**; runs no validator. | a per‑NO *pool operator*: deposits; runs the `/verify` server (co‑signs trails); commits the Merkle payout root that splits its miner pool — from `E_s` a sum-tree root over per-coldkey accounts, split by verified usage alone, whose ledger it publishes and whose paths it opens on demand. Directs flow; the contract holds and pays. |
| **Provider (miner)** | a `client_id` **inside** a NO's miner pool — **not a UID** (100k+ providers can't each be a UID); from `E_s` its earnings accrue to its payout coldkey's one **account** in that pool | carries traffic; **claims its α directly from the contract** with a Merkle proof against its NO's payout root: for epochs before `E_s`, per epoch and before a TTL; from `E_s`, cumulatively and without expiry — one claim, which anyone may send and which always pays the account's coldkey, collects all accrual since the last. The **on-ramp / tail** tier; can **graduate** to a top-level slot (§8.4). |
| **Top-level miner (head)** | **its own miner UID**; a **fleet** — its `client_id`s **bound to its hotkey** (§11.4) | the **supply apex**: the top ~200 fleets by **split-adjusted distinct routable egress-IP count**, steered **directly** by validators (`weight = score`); **native** emission to its own coldkey — no contract custody, no Merkle claim; maintained by deregistration churn (§8.4). |
| **Validator** (was "verifier") | an **independent** Bittensor validator UID; stakes its **own** α | runs `/verify` trails (the failure‑data signal), scores pools `implied_usage × Q_n` under commit‑reveal (§10). Earns **native dividends** (∝ stake × vtrust) — its **only** reward; a validator effort bounty is **out of scope** (D29, parked in §9.3). No NO, no pool. |
| **Auditor** (version 2) | an owner-admitted EVM address in `STSettlement2`'s auditor set (at most 16, with a quorum), not a hotkey or `vpk` binding; at launch a single address whose key the validator holds, quorum 1 | approves the exact pending v2 root before the next epoch ends. A root finalizes only with a quorum of approvals, and never while vault2 blocks it (inside its challenge window or with a demand open); challenges and path demands are open to anyone (§5.2). |
| **ST contract set** | three distinct mapped coldkeys: coordinator, immutable settlement vault, immutable reserve sink. Version 2 adds the immutable `STSettlementVault2` ("vault2") and the owner-upgradeable `STSettlement2` beside them, leaving every v1 contract unchanged | the vault owns pool UIDs and claims escrow; the reserve sink owns permanently locked conviction; the coordinator owns neither and only applies policy. vault2 owns each v2 pool UID (from `E_s`) and its own claims escrow; `STSettlement2` owns neither, holds the auditor set and approvals, and reads operators, windows and epoch boundaries from the coordinator. Does **not** custody validator emission (it is native). |

**Why pools (miner side only).** A NO has up to 100k providers — they cannot be UIDs (subnet cap ≈ 256),
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
| `netuid` | the UR subnet id — 25 (SN25) on mainnet; 521 on the testnet campaign |
| `T_tempo` | tempo length in blocks (360) |
| `T_epoch` | UR settlement epoch in blocks (50 400 ≈ 7 days) |
| `e` | epoch index (monotone counter in the contract) |
| `E_s` | the **switch epoch**: a pool's first version 2 epoch, its `firstEpoch` as set by `registerOperatorPool`; earlier epochs settle on v1 (§5.2) |
| `D_n` | α deposited by NO `n` during epoch `e` (summed from finalized `Deposit` events; coordinator counters enforce caps but do not weight, §7.5) |
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
| `s_{n,p}` | NO `n`'s payout share for provider `p` (Σ_p s_{n,p} = 1); v1 epochs only, before `E_s` |
| `(coldkey_i, cumulative_i)` | account `i` of a pool's v2 ledger: a payout coldkey (one account per coldkey per pool) and its cumulative accrual in rao, which never decreases or expires; claimable = latest finalized cumulative − `claimed` |

---

## 5. Time: tempo vs. the 7‑day epoch

Two cadences run concurrently.

The values in this section are the **mainnet 1.0 cadence**. Testnet acceptance
uses a deliberately shortened, future-effective 360-block UR epoch
(approximately 72 minutes), a 60-block root window, a 180-block finalize
offset, and a 6-block close grace. It must complete three consecutive fully
observed epochs. This test-only snapshot exercises the identical state machine
and must never be promoted as the mainnet policy, whose epoch remains 50,400
blocks (approximately seven days) with separately reviewed +4h/+48h windows.
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
    for each NO pool p:  score[p] = (deposit_p / rate(tier_p)) × quality_v(p)
    normalize to u16; submit native CRv4 commit extrinsic            // reveal auto-fires later (§10)
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
incrementing counter with a fixed block length `T_epoch`; version 2 reads the same counter, boundaries and
windows from the v1 coordinator, whose policy is unchanged. Epochs before a pool's switch epoch `E_s` keep
the v1 lifecycle (timeline from `README.md`):

```
  t = 0         Epoch e closes. Snapshot D_n (per-NO deposit totals) and the miner-pool emissions.
  t ≤ +4h       Each NO must commit its payout-list root for e (the share tree).  [README: "data ... 4h after"]
  t < +48h      Audit window: committed roots and content-addressed payout artifacts are public and reproducible (§11.3).
  +48h          coordinator.finalizeOperatorEpoch(e,n): immutable per-NO entitlement opens in the vault.
                Unclaimed α rolls into epoch e+1 (or a grace pool) after a TTL.
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
  < commit + 14,400  Challenge window (≈48h from the commit, fixed in vault2): anyone may prove that the root
                     lowers, replaces or hides an existing account (checkPath, which deletes the root), or demand
                     the path opening of an account proven against any finalized root (demandPath, once per
                     account of each root), which blocks finalization until the operator answers.
  ≥ commit + 14,400  finalizeDistribution(n), by anyone, once a quorum has approved and no demand is open. Each
                     account's cumulative becomes claimable with no expiry; anyone may send the claim, which
                     always pays the account's coldkey.
  ≥ end(e+1)         A root still unfinalized can be replaced by the next commit, which covers its epochs too.
                     vault2 refuses any commit within 14,400 blocks of the pool's previous commit.
```

Deposits, emission weighting, and dividend capture happen continuously across the epoch at tempo
cadence; only **settlement** waits for the epoch boundary + windows.

---

## 6. The ST contract

Release 1.0 is a three-contract system on the Subtensor EVM. The split is a custody boundary, not merely
code organization:

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
   owner-set auditor quorum, and finalizes. It holds no funds: it accepts native value only from vault2, as
   the refund of a pool registration's unused burn, which it passes on to the owner. An upgrade can change
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
  TAO-equivalent value is below `DefaultMinTransfer`. The vault binds that value as an immutable after
  the deployment path authenticates the exact finalized runtime Wasm and its `InitialMinTransfer`
  metadata constant. If a pool observation is below it, the epoch records zero captured funding and emits an
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
  `depositHotkey`. The operator first sends an exact amount to that isolated position through a native,
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
- **Locked stake in vault2.** Runtime 475 can lock part of each registration's price as collateral on the
  registering coldkey's position (a subnet's collateral lock share, 0 by default). `getStake` includes the
  lock, a move or transfer may not dig into it, and only emission the hotkey earns releases it. vault2
  therefore moves and counts only **movable** stake, `getStake` less the lock it reads with
  `getMinerCollateral`, in captures, `absorbStake` and the backing check, while its delta checks still
  compare whole positions. Accounts also reject incoming conviction-locked alpha unless they opt in, and
  vault2 never does, so no third party can push a lock onto its positions (`VERSION2-FINAL.md` §7.3).
- **TAO is only touched** for gas and the owner's discretionary conversions. Participants never pay AMM
  slippage to *use* the system; only to exit α→TAO.

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
- **Treasury:** the 18% owner cut accrues to the owner multisig (a governance treasury later, §6.4.3).
  The buyback reserve is **not** treasury — it is contract‑locked (§7.4).

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
  guardian can do is freeze new activity (a liveness incident), not cause a custody loss.

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
deposit for an epoch is `floor(bytes × rate_gib / GiB + users × rate_user)`, capped per epoch. `bytes` and
`users` are the totals of the NO's **signed payout artifact** for the previous epoch (`total_usage_bytes`
and `total_users` — the latter the same distinct‑users figure its public stats feed publishes per block),
so the reported usage is signed and audited against the deposit (§8.1), but it still enters no on‑chain
computation. In release 1.0 (D25) this schedule is read by **the validators** (who set the weights, §8.1)
as well as by NOs (to size their deposits); it is **never a value the contract consumes**. This keeps a
whole subsystem (`setRates`, on‑chain rate storage, the TAO/USD feed) and a trusted input out of the
contract.

**Zero price — the launch mode.** The published schedule may be **entirely zero** (every component of
every tier), but only when the signed policy says so explicitly: `zero_rate_action: equal_demand`. The
default (`halt`) rejects an all‑zero schedule, and a schedule with zero rates in *some* tiers only is
always invalid, so a zero price can never happen by accident. While the price is zero, NOs make **no
demand deposits**: none are required and none are audited. Every registered pool is given the **same
implied demand** (exactly 1, §8.1), so the validators' measured quality alone steers the pool channel; a
voluntary deposit or conviction lock neither helps nor hurts a pool. The head channel, θ, the quality
clamp and the cede rule are unchanged. This is a deliberate launch mode used while bugs are flushed out
of the network. Version 2 settles each pool's epochs from E_s through the v2 ledger rather than a payout
artifact, and the signed policy's `deposit.usage_artifact` field is inert for those epochs. The deposit
audit stays dormant at zero price, and a priced policy needs its own v2 deposit design
(`VERSION2-FINAL.md` §9.4).

### 7.2 A deposit is conviction stake (D25)

The deposit anchors the claim: `D_n` (α) is the NO's costly, on‑chain input, and validators turn it into
the weightable demand signal `implied_usage_n = D_n / rate(tier_n)`. The protocol
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
  or in none**. The schedule is governance‑set and published off‑chain (§7.1), read by validators. A
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
  the actual demand pressure, and the sourcing commitment is published alongside the §7.1 rate schedule
  (on‑chain code cannot see where α came from; the policy makes the buyback claim falsifiable).
  `deposit(noId, amount)` stages the **full amount** plus at most two rounding rao, `moveStake`s it to the
  **reserve hotkey** — the owner's validator hotkey (§6.3) — and transfers it into the immutable sink
  coldkey. The two destination share-pool views may each floor one rao; finalized sink credit must still
  cover the full principal, while any zero-to-two-rao excess is an irreversible reserve donation. It emits
  `Deposit` plus `ReservePrincipalAdded`. `addConviction` follows the same one-way path but emits `ConvictionAdded` and
  does not increase epoch demand. Coordinator counters enforce exact nonces/caps; neither contract
  computes weights from them (D25). The finalized events are the public weighting record.
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
needed: each deposit is already an **on‑chain event** (`Deposit(noId,e,funder,amount,policyHash,nonce)`)
authorized by the scoped deposit signer, so a NO's per‑epoch deposits and cumulative conviction are publicly and authoritatively
**summable straight from the event log** — that *is* the signed, published deposit list. Validators read
it directly (§8.1): this‑epoch deposits for the demand signal, all‑time for the conviction/tier. The
coordinator stores only nonces and cap/audit totals, not a weighting oracle (D25); finalized logs are the
portable source of truth used by validators and independent replayers.

---

## 8. Miner channel: per‑NO pool, validator‑weighted by demand × quality, Merkle‑claimed

### 8.1 Across operators — validators weight the pools; Yuma emits ∝ implied‑usage × quality (D25)

**The contract does no weighting.** Independent validators (§9–§10) compute each NO miner‑pool UID's
score **themselves**, from **published data**, and submit it as Yuma weights; Yuma medians/clips the
scores and emits to the pool UIDs — so the miners' reward *is* the validators' evaluation. Each validator
reads, per NO `n`, straight off chain state and its own trails:

- **`epoch_deposit_n`** — this epoch's deposit, read from the coordinator (§7.5), together with the NO's
  signed payout artifact for the previous epoch and its audited **`bytes_n`** and **`users_n`**: the
  deposit must equal `floor(bytes_n × rate_gib(tier_n) / GiB + users_n × rate_user(tier_n))`, capped, or
  the pool is zero‑weighted (the deposit audit). This is the release 1.0 audit, dormant at zero price;
  epochs from `E_s` have no signed payout artifact, so a priced policy needs its own v2 deposit design
  (`VERSION2-FINAL.md` §9.4);
- **`tier_n`** — the NO's conviction tier, from its cumulative locked α (§7.2), and the published
  two‑component **`rate(tier_n)`** (§7.3);
- **`Q_n`** — the pool's aggregate provider quality, from the validator's own `VALIDATOR.md` trails.

and sets

```
weight_n  ∝  implied_demand_n × Q_n
implied_demand_n = (bytes_n × rate_gib(0) / GiB + users_n × rate_user(0))   # audited usage priced at the conviction-zero tier
                   × min(1, cap / owed_n)                                   # truncated exactly as the capped deposit was
implied_demand_n = 1                                                        # while the published price is zero (§7.1)
```

**Implied demand, not raw deposit** (decision A, D25): the audited usage is priced at the **baseline
(conviction‑zero) rate** for every NO, so a NO on a lower tier rate posts less α for the same real usage
and gets the **same** weight — the conviction stake that bought the lower rate is a *discount, not a
penalty*, and the weight still tracks **real revenue‑backed usage** (the headline thesis) rather than raw
α. With `users_n = 0` this is exactly the earlier `epoch_deposit_n / rate(tier_n)` ranking. When the
per‑epoch cap truncates the deposit a NO owes at its own tier (`owed_n`), its demand is truncated by the
same factor, so usage the NO did not pay for buys no weight. Under the zero‑price launch mode every
pool's implied demand is exactly 1 and `Q_n` alone steers (§7.1). NO `n`'s miner‑pool UID then accrues
over the epoch

```
emission_n  ≈  0.41 · E_epoch · (1−θ) · ŵ_n,   ŵ_n = consensus(implied_usage_n · Q_n) / Σ_m consensus(implied_usage_m · Q_m)
```

**as α stake the immutable vault holds** (the vault owns the pool UID — the NO never receives it). Demand (implied
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
A refused or deleted root does not change the pool's weight while there is one operator: zeroing the pool
would hand the pool channel to the head (§8.5), punishing providers for the operator's fault.

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
`HMAC(policy_key, family || masked_prefix || domain)` at the policy granularity (initially /29 IPv4 and
/48 IPv6). A fleet's score is the count of **distinct routable scoring hashes** its bound clients serve,
**split when shared**: each hash contributes **1.0 total, divided equally among every eligible head fleet
observed on it**. Thus distinct exact IPs in one co-located prefix deliberately collapse and cannot
double-count supply. A score unit counts only if it is *routable* — the validator completed a real
trail hop through it (`VALIDATOR.md` §7), so liveness is baked into the metric and there is no separate
quality term.

```
for top-level-miner UID u (a fleet — a hotkey with bound client_ids C_u, §11.4):
    IPs(u)   = { distinct keyed prefix hash h : some c ∈ C_u routed a verified trail hop from h } # VALIDATOR.md §7
    claim(h) = #{ top miners u' : h ∈ IPs(u') }                                                  # split across sharers
    score(u) = Σ over h ∈ IPs(u) of  1 / claim(h)         # split-adjusted unique routable-IP count
    head_weight[u] = score(u)                             # weight IS the IP score (decision B)
```

Every validator computes `score(u)` **from its own trails** — the trail/proof wire carries a per-hop
**egress-IP-hash** (`VALIDATOR.md` §8, a keyed hash at a configurable subnet granularity — default /29
IPv4, /48 IPv6 — never the raw IP), so a validator can count each fleet's routable score units and the
sharing splits without trusting anyone. This is the **verify-the-top-200** requirement: a validator only weights
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
  `score`** (so a single noisy epoch doesn't thrash emission), and a **θ large enough that the lowest top
  miner clears the tail** (else the head thrashes against the pools, §8.5). Each (re-)registration pays
  the continuous burn auction (≈ ×1.26 / registration), so churn has a real, tunable cost — and a per-UID
  burn makes the head **more** Sybil-resistant than one pool UID per NO.

**Weight shaping (best practice for ~200 concurrent fleets).** Steer **proportionally** to `score`, *not*
winner-take-all; apply the signed policy's `max_weight_limit_u16` before every CRv4 commit; and drive
`VALIDATOR.md` trails at a rate (validator-configurable, §D26) that gives every top UID regular coverage
so honest-but-idle fleets don't stale-decay. Runtime v454 retains v452's native getter at `65535`
(no cap), despite retaining a `MaxWeightsLimit` storage item, so release 1.0 validators enforce the
policy cap locally and finalized-vector analysis rejects violations. The two-NO testnet bootstrap uses
`32768` (the smallest feasible two-recipient cap); a production policy lowers the cap toward a low
single-digit percentage only after enough positive recipients exist. Deterministic `u16` conversion
repairs any one-unit rounding deficit before signing and rechecks the exact integer cap. A native
runtime cap remains the preferred defense against non-conforming validators. This matches the strongest DePIN precedents —
FileTAO's live-scoring and TPN's robust statistics.

### 8.5 The head/tail emission split θ

Both tiers are miner UIDs in **one** mechanism (§14), so the 41% miner emission is divided by the
**weights validators set** — and to make that a controllable policy rather than an accident, validators run
common software that **reserves a governance share θ to the head and `1−θ` to the pools**, exactly as Data
Universe (SN13) reserves a fixed share to one UID by rewriting weights before `set_weights`:

```
head[]  =  { score(u) }               normalized so Σ head = θ      # split-adjusted routable-IP breadth (§8.4)
pool[]  =  { implied_usage_n × Q_n }  normalized so Σ pool = 1 − θ  # implied_usage = deposit / rate(tier) (§8.1)
w       =  head ⊕ pool                # one vector; apply signed max_weight_limit_u16; commit-reveal
```

Both shares go to **real recipients** (top miners; vault-owned pool UIDs), so the **June-2026
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
From version 2's switch epoch E_s, UR's validator is also the launch **auditor** of payout roots: from an
owner-admitted EVM address it audits each pool's published ledger and approves the root, challenges a
provable fault, or demands openings (§11.3).

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
        implied_usage_n = epoch_deposit_n / rate(tier_n)   // deposits from the event log (§7.5); rate from the published schedule (§7.3)
        pool[p] = implied_usage_n · quality_v(p)           // §9.4 aggregate; = 0 if v operates NO n (self-mask)
    # HEAD — top-level miners: split-adjusted routable-IP breadth (§8.4, D27)
    for each top-level-miner UID u (fleet — client_ids C_u bound to u, §11.4):
        head[u] = score_v(u) = Σ_{h ∈ v's routable IP-hashes for C_u} 1 / (# top miners v sees claiming h)   // = 0 for v's own UID
    # split by governance share θ, into ONE vector (§8.5)
    normalize head so Σ head = θ ;   normalize pool so Σ pool = 1 − θ
    w_v = apply signed policy max_weight_limit_u16 to (head ⊕ pool)
    w_v = half-up u16(w_v); deterministically repair rounding under the same cap
    commit / reveal w_v   (Neuron 0x804, drand timelock — §2.4)
```

The self-mask covers the NOs a validator declares it controls. The SN25 launch's one UR validator declares none and
scores UR's own pool (owner decision, October 7).

Version 2 changes only *which* UID is NO `n`'s pool. For epochs at or after the pool's v2 `firstEpoch` (E_s for
UR's pool), a validator resolves it with `STSettlement2.settlementAt(n, epoch)`, which returns the pool hotkey
registered in vault2 (`STSettlementVault2`). The validator checks that hotkey against vault2's own
`pools(n)` record and refuses a mismatch, so an upgrade cannot redirect emission. For earlier epochs it uses the v1 coordinator's `operatorAt`, so
replay never calls a contract that did not yet exist. Live steering and replay share this one resolver, and from
E_s the v1 pool hotkey is no longer consulted. A refused or deleted v2 payout root (§11.3) does not change the
pool's weight while there is one operator: zeroing the pool would hand the pool channel to the head fleets,
punishing providers for the operator's fault.

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
balance is large relative to gas. A claim uses about 170,000 to 187,000 gas but needs a gas limit of about 2.5M,
because the runtime admits a stake transfer only if the forwarded gas covers its declared weight
(`VERSION2-FINAL.md` §7.3, §8.1). An optional, stateless claim batcher (`STClaimBatcher`) claims many
accounts in one transaction, each capped at its own gas so that a claim the runtime refuses cannot starve
the others, and still only to each leaf's coldkey: from ten claims on it saves 15–18% of their gas, and
a batch's gas limit grows by about 149,000 per claim where a single claim needs about 2.44M. One
transaction holds up to 418 claims at the deepest tree, and the claim tools send at most 32, under 1% more gas a
claim (`VERSION2-FINAL.md` §10.2, R-7).
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
approval and demands two sets of accounts. A tree rebuilt from rows is always consistent, so an
over-allocation hidden in an inconsistent subtree is never reproduced from published account rows: it takes
the demand route, and any opening under the bad node deletes the root. Both sets are accounts of the newest
finalized root whose ledger the validator holds and has verified: the latest, or an older one when the
latest's objects are missing or do not rebuild it. One demand set is every such account whose published row
shows a fault. The other is a value-weighted random sample of at least 256 of them, drawn without
replacement with randomness the operator cannot predict, which doubles after each failed root over the same
predecessor. vault2 allows one commit per pool per 14,400 blocks, so the sample cannot be re-rolled quickly,
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

- **Secured:** cross‑operator emission tracks `implied_usage × consensus‑quality`, with implied usage
  equal to `deposit / rate(conviction tier)` (costly, revenue‑backed demand × independently‑measured
  liveness) — **validators' evaluation drives the miner payout**, the
  Bittensor mechanism — via median + clipping + vtrust over many **independent** validators, plus the
  self‑weight mask and the fully‑sunk deposit (§10, §12.1); provider quality also bites **within the
  pool** under v1 settlement (auditable payout list, §8.2) and via reputation; v2 settlement splits each
  pool by usage alone (below, §13.4).
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
  time promised: at live rates about 1,450 α per epoch reaches the pool at θ = 3/10, ≈ 0.0145 α per
  provider per week at 100k providers, so a 250k-gas claim at a 5 gwei base fee (≈ 0.19 α; the rehearsal
  measured 186,707 gas for an account's first claim and 169,606 for a later one) costs about 13 weeks of
  average earnings, and gas falls under 10% of a claim only after ≈ 131 weeks; both take ten times longer
  at 1M providers (`VERSION2-FINAL.md` §3). A third party can also raise that cost: by filling vault2's own
  staking-hotkey list (up to 128 entries, each costing it a transfer above the floor) it makes every claim
  use about 1.26M gas instead of 0.15M, about 1 α at the same prices.
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
liquid out:  L_e  ≈  0.41·E_e                      // miner emission (both tiers)
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

`weight_n ∝ implied_usage_n × Q_n`, with `implied_usage_n = deposit_n / rate(tier_n)` the demand anchor
(§8.1, D25) and `Q_n` the consensus‑measured pool quality (§8.1, §10): demand is the objective anchor,
quality is the earned modulator, and together they make validators' evaluation the thing that moves the
money. The one nuance is **magnitude at bootstrap**: `Q_n` is noisy until the
validator set + data mature, so governance **caps the quality swing** early (closer to deposit‑weighted)
and widens it as the independent‑validator stake share grows (§12.3). So quality is on the payout path
from day one — we ramp its *strength*, we do not defer the mechanism.

**v2 settlement confines quality to this cross-operator weight** (§13.1). From `E_s` the within-pool split
is by usage alone, with no per-provider reliability factor (cf. §8.2), so the validator's measurement no
longer gates which providers inside a pool are paid. Root approval is an objective ledger check, not a
quality lever: while there is one operator, a refused or deleted root leaves the pool weight unchanged,
because zeroing the pool would hand the pool channel to the head fleets and punish providers for the
operator's fault (owner decisions, October 11).

### 13.5 No on‑chain oracle (simplified out)

Because per‑GB / per‑user usage is self‑reported and unverifiable on‑chain, an on‑chain rate has no
enforcement power. The objective on-chain input is *α deposited*; validators combine it with the
governance-published conviction-tier rate to derive implied usage (§7.1, §8.1). v1 therefore has **no
oracle**: the rate schedule is a signed, content-addressed off-chain policy that NOs use to size deposits
and validators independently use to reproduce weights.
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

The two tiers share **one mechanism's** 256-UID metagraph, and the 41% miner emission divides between them
by a governance share **θ** baked into validator software (SN13-style weight reservation, §8.5).
*Rejected:* **two sub-mechanisms** (mechanism 0 = pools, mechanism 1 = top miners) split by
`mechanism_emission_split` — clean in theory, but `mechanism_count × max_UIDs < 256` would **halve the UID
space to ~127**, too few for ~200 top miners (so release 1.0 fixes `mechanism_count = 1`, §14);
and **reserving the split by burning to an owner/immune UID** — post-Spec-421 the `(1 − miner_burned)` term
would shrink the subnet's whole cross-subnet allocation (§8.5). θ trades demand-coupling (in the tail)
against the merit apex (the head); it is a governed dial, started tail-weighted and ramped (§8.5, §12.3).

---

## 14. One mechanism and its UID budget

Release 1.0 uses exactly **one** mechanism. The pool on-ramp and direct head are two scoring channels in
one CRv4 vector, divided by θ (§8.5), not Subtensor sub-mechanisms. A second mechanism would divide the
global ~256-UID capacity and make the target head impossible; the release preflight therefore requires
`mechanism_count = 1` and refuses to register roles unless all missing identities fit the intended
`max_allowed_uids` snapshot.

The budget is `max_allowed_uids ≥ head fleets + one vault-owned pool UID per NO + validator UIDs + the
owner/escrow identities that require registration`. Validators are not a separate fixed partition: they
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

---

## 15. Concrete parameters

### 15.1 Subnet hyperparameters (owner‑set at creation; verify live before launch)

| Hyperparameter | Value | Rationale |
|---|---|---|
| `tempo` | **360** | native ~72‑min weight/emission cadence (§5.1) |
| `max_allowed_uids` | **256** (hard ceiling — owners may lower, never raise) | one metagraph shared by **~200 top-level miner UIDs + 1 pool UID per NO + validator UIDs** (§14); tail providers are NOT UIDs (§3) |
| `max_allowed_validators` | root-controlled/runtime-dependent; target **≤ 56** so ~200 miner slots fit | observe and compatibility-gate the live value; permit count (top-k by stake, §9.7) is *not* a fixed slot partition |
| `mechanism_count` | **1** | a 2nd mechanism halves the 256-UID space below 200 (§13.8, §14) |
| native `max_weight_limit` / signed policy cap | v454 native **65535** (no cap); release policy **32768** for two-NO testnet, then lower as breadth grows | v454 retains v452's getter behavior which ignores the retained storage item; validators must apply the signed cap and analysis must reject finalized violations. A low-single-digit cap requires enough positive recipients and should become native when the runtime supports it (§8.4). |
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
| Epoch length | `T_epoch` | 50 400 blocks (7 d) | settlement period |
| Operator data deadline | — | +4 h (1,200 blocks) | `README.md`; the root commit window, unchanged in v2 settlement |
| Close window | — | v1: 120 blocks after the epoch end; **v2: the 1,200-block commit window** | a v2 close missed beyond it defers the epoch with zero funding, and the next close captures its emission for later earners (§12.3) |
| Challenge window (v2) | `CHALLENGE_BLOCKS` | **14,400 blocks** (≈ 48 h) from the commit, a constant of vault2 | no v2 root finalizes earlier or with a path demand open; needs `T_epoch` ≥ commit window + 14,400 (mainnet's 50,400 holds; testnet's 360-block cadence does not), checked at pool registration and kept by every later v1 policy (§5). v1 instead finalizes 14,400 blocks after the epoch end |
| Demand fee (v2) | `demandFeeWei` | 750,000 rao (7.5·10^14 wei): one answer's gas at the planning fee, 149,074 gas at 5 gwei, rounded up; a nonzero multiple of 10^9 wei | immutable in vault2 and kept by it, never paid to the answerer; the runtime moves only whole rao of a call's value. The rehearsal measured an answer at depth 29 at 149,074 gas and a demand at 98,268. Each account of each finalized root can be demanded once per pending root, so the possible demands grow with the number of roots, each paying the fee |
| Root approval (v2) | — | owner-admitted auditor addresses (≤ 16); **quorum 1 at launch** (the validator); approve before the end of epoch `e+1` | a root still unfinalized at the end of epoch `e+1` can be replaced by the next commit, and its epochs' funding stays for that root (§13.1) |
| Claim expiry | — | v1: `claim_ttl_epochs` 8 + `claim_grace_epochs` 1; **v2: never** | policy v1 stays in force: `claim_ttl_epochs`, `claim_grace_epochs`, `expired_claim_action`, `shares_total_bps`, `rounding` and `deposit.usage_artifact` keep their meaning for v1 epochs and the v1 carry sweep and are **inert for v2 epochs**, which allocate in rao by `coldkey_usage_largest_remainder_rao_v1` |
| Ledger bounds (v2) | — | ≤ 96 covered epochs per root; header ≤ 256 KiB (about 3,500 chunk references); 131,072 rows per chunk (48-byte account rows, 6 MiB; 57-byte provider rows, ≈ 7.1 MiB); ≤ 2^29 accounts per pool in vault2's tree | design target and activation gate: ≥ 1,000,000 providers per pool; the validator fetches at most `--ledger_max_bytes` of new objects per root (default 2 GiB, a runtime flag) |
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

1. **ST contract set (Solidity, Cancun / 0.8.24).** Non-upgradeable `STReserveSink` and
   `STSettlementVault`, UUPS `STCoordinator`, exact runtime-454 bindings retained from v452 (`0x09`, `0x402`, `0x403`,
   `0x804`, `0x805`), OZ Merkle verification, scoped roles and scheduled policy (§6). The reviewed
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
   implied usage = deposit ÷ conviction‑tier rate, computed off the published deposit events; head on its **routable‑IP breadth score**), read the `client_id ⇄ hotkey` binding (§11.4), split by θ, and submit
   commit-reveal weights (standard Bittensor validator loop → native dividends) — **no central
   keeper sets weights**. A separate **permissionless settlement poke** triggers per-NO finalization after
   the +48h window. (No trail‑proof submission — the effort bounty is out of scope, §9.3/D29.)
4. **Network‑Operator software.** Runs the privacy servers + the `VALIDATOR.md` `/verify` server
   (SEED/EXTEND/FINAL, poisoning, idempotency, the four Ed25519 signatures, the egress‑IP index);
   stages and `deposit`s each epoch through its isolated hotkey/signer (conviction stake; no contract
   weighting, D25); for release 1.0 epochs (before E_s), computes provider reliability + payout list, publishes a
   content-addressed artifact, commits the **`payoutRoot`** (fractional shares) and serves leaves; from E_s it builds
   and publishes the v2 ledger instead (item 8). (No validator pool — it co‑signs trails as the `/verify` server.)
5. **Provider software.** Carries ingress/egress; registers a `client_id`; for release 1.0 epochs, verifies its
   payout leaf against `payoutRoot` and calls the immutable vault's `claim` (from E_s its coldkey's account is
   claimed from vault2, item 8). **If it reaches the top ~200:**
   limit-registers its own UID, publishes the dual-signed `client_id ⇄ hotkey` binding (§11.4), and
   earns **natively** (no claim, §8.4).
6. **Validator client (was "verifier").** Stake α; run `/verify` trails; submit commit‑reveal pool
   scores (native dividends); independently reject stale/invalid bindings. (No `registerValidator(vpk)` /
   `submitTrails` / `claimValidator` — those belong to the parked bounty design, §9.3/D29.)
7. **Indexer/explorer.** Surfaces `D_n`, pool quality `Q_n`, consensus weights, vtrust, the
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
     v2: a capturing close needs a gas limit of about 2.6M while using about 300,000 gas, because the
     runtime admits a stake move only if the forwarded gas covers its declared weight, and today's
     `maximum_gas` is 1,000,000 (`VERSION2-FINAL.md` §8.1, §13).
   - **Validator.** From the pool's `firstEpoch` it weights the pool UID resolved through
     `STSettlement2.settlementAt`. It audits each pending root's ledger and, from its admitted auditor
     address, approves the root only if every check passes, challenges a provable fault with `checkPath`,
     and demands a value-weighted sample of at least 256 previous accounts with `demandPath` when the
     objects are missing or do not reproduce the root. Each demand names the newest finalized root whose
     ledger the validator holds and carries the account's proof against it. The sample doubles after each
     failed root.
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
     verifies the account's proof against vault2's on-chain root and sizes the gas limit with
     `eth_estimateGas`. A v2 claim needs about 0.02 TAO of gas on the device's gas key.

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
   battery — custody semantics, rao units, `0x402`/`0x403`, blake2f, metagraph/neuron/staking, exact
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
   rehearse the separate 50,400-block/seven-day mainnet policy with reviewed
   +4h/+48h windows. After the three clean testnet UR blocks and MR closure,
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
runtime rehearsal (gate 2 of §16.4) runs on a node built from runtime 475's source
(`scripts/runtime-rehearsal/`, a one-node `fast-runtime` dev chain with mainnet's EVM chain ID, driven by
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
  `E_s + 8`, once the last v1 expiry has passed, plus a second operator, a fractional approval quorum,
  operator bonds and a priced deposit policy.

### 16.4 Pre‑launch verification checklist (load‑bearing live values)

*(D28, release 1.0: these checks run against **testnet** (chain 945 dust probes) + the SP‑3 localnet — they are the
Phase A/B gates of `docs/LAUNCH.md`, and all must be green before the supplied subnet is modified; re-run them
against finney at the Phase‑E mainnet promotion.)*

- Precompile addresses/ABIs at the pinned Subtensor release (Blake2f `0x09`, Ed25519 `0x402`, sr25519
  `0x403`, Metagraph `0x802`, Neuron `0x804`, Staking V2 `0x805`) using good/bad vectors and
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
  SN25 launch the only validator is UR's `ur-mainnet`. Confirm delegated stake on the reserve
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
2. **Runtime rehearsal (R-1 to R-6).** The exact bytecode runs on a node built from runtime 475's source,
   which phase A builds (`scripts/runtime-rehearsal/`, harness in `rehearsal/`). Any runtime change on
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
| **How to adapt to standard BT payout formulas?** | Independent validators set standard Yuma weights `= implied_usage × quality` (implied usage = deposit ÷ conviction-tier rate, computed from finalized events + signed policy — contracts weigh nothing, D25) on per-NO miner-pool UIDs; the chain delivers emission to vault-owned pools (re-split by Merkle claim) and native dividends to validators. A second head tier holds provider-owned UIDs and receives native emission by split-adjusted routable-prefix breadth under θ (§8.4–8.5). |

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
  **conviction**, which sets its **tier → deposit rate** (§7.3). Coordinator counters enforce caps but do
  not weight; finalized `Deposit`, `ConvictionAdded`, and sink-principal events are the portable record (§7.5, D25).
- **implied usage** — `deposit_n / rate(tier_n)`: the demand signal validators weight for the pool tier
  (§8.1); staking for a lower rate keeps weight tracking real usage rather than raw α.
- **Effort bounty** — a fee‑funded reward that *would* pay ∝ verified, coverage‑weighted, server‑assigned
  completed trails; **out of v1 scope** and **not a committed phase** — a parked future‑iteration candidate
  (§9.3, §13.6, D29).
- **ST contract set** — release 1.0's UUPS policy coordinator plus an immutable settlement-vault coldkey and
  an immutable reserve-sink coldkey; version 2 leaves it unmodified and adds **vault2** and **`STSettlement2`**
  beside it. Custody is physically separated from upgradeable policy. **Not** the validator (§9–§10).
- **`D_n` / `Q_n` / `ŵ_n`** — NO's epoch deposit total (from the event log) / its consensus‑measured pool
  quality / its resulting validator‑computed weight (∝ `implied_usage_n × Q_n`, §8.1).
- **Validated path** — a completed `VALIDATOR.md` trail proof, id `keccak256(trail_id‖vpk‖server_key_id)`;
  self‑proving (NO `FINAL` + validator `vpk` sigs), verified via `0x402`.
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

---

*End of WHITEPAPER.md v1.0 — validator effort bounty removed from scope (v1 = native dividends only; the
`(X)`/`(Y)` bounty designs are parked, not a committed phase; §9, §13.6; decision D29 in
`WHITEPAPER_DISCUSS.md`) — layered on v0.4 conviction staking + validator-computed weights + IP-breadth head
(D25–D28), v0.3 deposits-as-buybacks (locked, dividend-compounding reserve; miner pay = emission-only; §7.4,
§12.4; D23), and the v0.2 two-tier miner side (pool on-ramp + direct top-level miners; §8.4–8.5, §10, §11.4,
§14; D16–D20). This document fixes the architecture and the formulas; the next artifacts are the contract
source, the chain‑config script, and the operator/validator reference daemons (§16). Version 2 payout
settlement, for epochs from E_s, is specified in `VERSION2-FINAL.md`.*
