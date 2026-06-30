# UR Subnet — Discussion Notes & Decision Log

Companion to `WHITEPAPER.md`. The whitepaper is the *what*; this file is the *why* — the decisions, the
alternatives we rejected (and reverted), the axes we circled and settled, and what's still open. Read
this before proposing a change so we don't re-litigate something already decided.

Last updated: 2026-06-28. Inputs: `seed/INCENTIVES.md`, `VALIDATOR.md`, `README.md`, and a deep research pass
on current (post‑dTAO) Bittensor.

---

## 0. Current design in one paragraph

Each **Network Operator (NO)** is **one contract‑owned miner‑pool UID** (its 100k+ providers live
*inside* the pool, paid by Merkle claim — they can't be UIDs). **Independent validators** stake α, run
`VALIDATOR.md` trails, and set Yuma weights **`deposit × Q_n`** (deposit = on‑chain demand anchor; `Q_n` =
measured pool quality) — so **validators' evaluation drives the miner emission, the Bittensor way**.
Miner emission (41%) accrues to the contract (custody) → providers claim via Merkle. Validator emission
(41%) is **native** ∝ stake × vtrust. On top, validators earn a **fee‑funded effort bounty**
(`φ·ΣD + ω·OwnerCut`) ∝ verified, coverage‑weighted trails — the engine that keeps the failure data
flowing. Everything is denominated in the subnet's **α**. The **ST contract** (Subtensor EVM) is the
ledger, miner‑emission custodian, bounty payer, and settlement engine — **not** the validator.

**v0.2 — two tiers (D16–D20).** The miner side now also runs a **head** channel beside the pools: the **top
~200 providers hold their own UIDs** and are steered **directly** on pure quality `Q_p` (no deposit), paid
**natively**, matched by a signed `client_id ⇄ hotkey` binding; the pool is the **on-ramp** they graduate
from. Both tiers share one mechanism's 256 UIDs, split by a governed head share **θ**. See `WHITEPAPER.md`
§8.4–8.5, §10, §11.4, §14.

---

## 1. Settled decisions (don't re-open without a strong reason)

Format: **decision** — why — *rejected alternative(s)*.

**D1 — Settlement = EVM contract + native Yuma.** A Solidity contract on the Subtensor EVM custodies
deposits + miner emission and settles by Merkle claim; the chain's coinbase still delivers emission. We
do not fight the coinbase. *Rejected:* Full‑EVM (route every α through the contract — extra churn/keys);
commitments‑pallet‑only (can't verify Merkle claims or Ed25519 disputes on‑chain). §13.1.

**D2 — Everything denominated in the subnet α.** This is why `seed/INCENTIVES.md` calls it the "ST
(subnet‑token) contract." Internal transfers use `transferStake` (slippage‑free within‑netuid); only
α→TAO exit hits the AMM. *Rejected:* TAO/USDC settlement (α isn't a liquid EVM token; loses alignment;
USDC adds a bridge). §13.2.

**D3 — The NO never holds emission destined for others; everyone claims from the contract.** Miner
emission accrues to **contract‑owned** miner‑pool UIDs; providers/validators are paid by direct Merkle
claim. The NO only *directs* (commits a payout root); it never custodies others' α. (User directive,
explicit.) §1, §3, §6.

**D4 — Two clocks.** Native **tempo** (~360 blocks) drives weights/emission; a **7‑day epoch**
(~50 400 blocks) is the application‑layer settlement period with +4h/+24h/+48h windows. §5.

**D5 — Pool design: UID = a NO's pool (scale).** A NO has up to **100k+ providers** — far beyond the
~256 subnet UID cap — so providers **cannot** be UIDs. Each NO = **one miner‑pool UID**; providers are
paid *inside* it by Merkle claim. (User correction that anchored the whole architecture.) §3, §8, §13.1.

**D6 — Validators are independent; quality drives the miner payout (`weight = deposit × Q_n`).** This is
the **Bittensor mechanism** — validators evaluate, consensus turns evaluation into miner pay. Validator
input being *off* the payout path "misses the point of Bittensor" (user). The deposit is the objective
demand anchor; `Q_n` is the earned modulator. **This axis was circled twice (see §2) and is LOCKED.**
§8, §10, §13.3, §13.4.

**D7 — Community validators are first‑class.** An independent validator (no NO, no pool) that stakes its
own α, runs trails, scores pools, earns native dividends + the bounty. They are the **disinterested
baseline** that `VALIDATOR.md` §1 needs (a verifier population independent of what it measures) and the
biggest lever against self‑dealing/collusion. (User asked for them explicitly.) §9.

**D8 — Validator effort reward: (X) now, (Y) later.** Validators must be *strongly* incentivized because
their output — *which providers are the weakest links* — is the product (user). Yuma pays ∝ stake×vtrust
(effort‑agnostic), so we add an explicit effort reward.
- **(X), chosen for v1:** a **fee‑funded bounty** (`φ·ΣD + ω·OwnerCut`) paid ∝ verified
  coverage‑weighted trails, *on top of* native dividends. Keeps validators as independent on‑chain UIDs
  with real Yuma consensus; no emission capture.
- **(Y), the escalation:** route the 41% validator emission itself through the effort split (contract
  captures it, pays ∝ trails) — strongest incentive, but moves the quality consensus into the contract.
  We move to (Y) only if (X)'s observed trail coverage is too thin. §13.6.

**D9 — No on‑chain oracle.** Per‑GB/per‑user usage is self‑reported and unverifiable on‑chain, so an
on‑chain rate has no teeth — the only quantity the protocol acts on is *α deposited*. The "global fixed
rate" survives as an **off‑chain published reference**. §7.1, §13.5.

**D10 — `φ`, the non‑refundable deposit fraction.** A hard cost floor against deposit self‑dealing
(round‑tripping a deposit through your own providers), and it **funds the effort bounty**. §7.2, §9.3.

**D11 — Governance: Phase 0 → Phase 1 (committed); deeper deferred.** Phase 0: owner M‑of‑N multisig +
upgradeable proxy (fast bug‑fixes, central control). Phase 1: **timelock ≥ 1 epoch** on
upgrades/params + a **pause‑only guardian**. Invariant from day one: **finalized claims are
sacrosanct** (no upgrade/pause can block or claw back an earned claim). §6.4.

**D12 — Optimistic effort verification (scales).** Verifying *every* trail on‑chain via `0x402` won't
scale. `submitTrails` commits a **Merkle root** of `(trail, coverage)` leaves + a claimed total; the
contract **spot‑checks a random sample** and **anyone may dispute any leaf** in the window (one bad sig
voids the claim + forfeits stake). O(1) on‑chain. §9.3, §11.3.

**D13 — Coverage weighting = under‑sampling (not "suspected‑weak").** Weighting by how *under‑sampled* a
provider is is well‑defined and non‑circular; "suspected‑weak" was circular (depends on the data it
produces) and mis‑aligned (trails through weak providers fail → no completion credit). Failures are read
as the *byproduct* of maximal effort (`VALIDATOR.md` §7.1). §9.3.

**D14 — Quality ramps at bootstrap (not deferred).** `Q_n` is noisy until the validator set + data
mature, so governance **caps the quality swing** early (closer to deposit‑weighted) and widens it as the
independent‑validator stake share grows. Quality is on the payout path from day one; we ramp its
*strength*. §12.3, §13.4.

**D15 — No global claim roots; settle from on‑chain state (+ drop `depositSummaryHash`).** Providers
claim **per‑NO** against that NO's committed `payoutRoot` (fractional shares, Σ=1), scaled by the
on‑chain `poolTotal_n = emission_n + (1−φ)D_n` (capped so a pool can't be over‑drained). The validator
bounty is computed on‑chain (`feePool·effort/Σeffort`) — no root. So **nothing is computed off‑chain at
finalize**, removing the last "who computed this root" trust step (this subsumes review‑item *B*).
`depositSummaryHash` is dropped (redundant with on‑chain `Deposit` events). Trade: a multi‑NO provider
claims once per NO. (Deferred: review‑item *A*, the `Q_n` aggregation + sampling spec — pending
implementation detail.) §6, §8.3, §11.

**D16 — Two miner tiers in parallel: pool on-ramp + direct top-level miners.** Keep the per-NO pool
(`deposit × quality`, Merkle) as the **on-ramp / baseline** tier, and add a **head**: the **top ~200
providers hold their own miner UIDs**, steered **directly** by validators on pure measured quality
(`weight = Q_p`, no deposit), paid **natively** to their own hotkey. *Why:* people need a place to start +
a baseline reward (the pool), and the best providers deserve the canonical, trust-minimized treatment
(their own UID). A provider is in **exactly one** tier (promoted out of its pool's payout list when it holds
a UID — no double-pay) and **graduates / falls back** via native deregistration churn. (User decision,
2026-06-30.) `WHITEPAPER.md` §8.4, §13.7.

**D17 — Head weight is pure `Q_p` (no deposit); demand-coupling stays in the pool.** The top-level-miner
channel is "weight just set by validators" — pure measured quality, EMA-smoothed (§8.4). The
`deposit × quality` demand-coupling bet (D6) lives entirely in the tail. (User: "top level miner pools with
weight just set by validators.") §8.4.

**D18 — `client_id ⇄ hotkey` binding (identity).** A top-level miner publishes a **dual-signed** association
(client Ed25519 + hotkey sr25519) — commitments pallet + ST-contract anchor, disputable via `0x402` — so
validators attribute a measured `client_id` to its UID. Field-standard "signed proof → registered hotkey"
(Epistula / ORO-AI `bittensor-auth`), with the dual signature (cf. SN51 Celium `associate_evm_key`)
preventing quality theft. Opt-in self-deanonymization; the tail stays pseudonymous. §11.4.

**D19 — One mechanism, head/tail split θ (not two mechanisms, not owner-burn).** Both tiers share one
mechanism's 256 UIDs; the 41% miner emission divides by a governed head share **θ** baked into validator
software (SN13-style weight reservation). *Rejected:* two sub-mechanisms (`mechanism_count × max_UIDs < 256`
halves UID space below 200; mechanisms stay reserved for the Pool 0 / Pool 1 product split) and burning to
an owner UID (post-Spec-421 `(1 − miner_burned)` shrinks the subnet's cross-subnet allocation). §8.5, §13.8.

**D20 — θ is the load-bearing new dial; start tail-weighted and ramp.** θ trades demand-coupling (tail)
against the merit apex (head): large θ weakens NO deposit incentives and decouples emission from demand;
small θ makes the apex a weak carrot. Start θ ≈ 0.3, instrument realized per-tier pay, widen as the
top-miner set + validator quality-consensus mature (cf. D14). Constraint: lowest-paid top miner ≥
highest-paid pool provider. §8.5.

**D21 — No-custody + trustless on-chain provider payout: v1 must-have (spirit, not immutability).** The
**foundation and NOs never hold or distribute α.** The contract is the sole custodian of in-transit α (the
tail pool's captured emission, deposits, the fee pool); every payout is a **direct on-chain pull claim**
(`transferStake`), and the **head is native** (top-level miners receive their own emission to their own
coldkey — the earner holding its own pay, not custody). All α transfers happen on-chain. This is a v1
**requirement**, not a v2 hardening (the "start TPN-style off-chain, add trustless claims later" fallback is
rejected). **Crucially this is no-custody *in spirit*, not contract immutability:** the contract stays
**upgradeable + owner-multisig + guardian** for v1 (normal bug-fix latitude for a new subnet) and is
progressively locked down over time — **D11 / §6.4 are unchanged**. (User decision + clarification,
2026-06-30; resolves the `COMPARISON.md` §8.3 open question.) §6, §8.3, §13.1.

---

## 2. Rejected / reverted — do NOT re-open these

These were explored and closed. Re‑proposing them is the "going in circles" we want to avoid.

- **Deposit‑only cross‑NO weight (REVERTED).** Briefly adopted as a simplification, then reverted: it
  takes validator evaluation *off* the miner payout path and reduces validators to a side‑channel —
  un‑Bittensor. **The weight is `deposit × Q_n`. Settled (D6).** The legitimate concern behind it
  (bootstrap `Q_n` noise) is handled by the swing‑cap ramp (D14), not by removing the mechanism.
- **Per‑NO validator pool + NO↔V intersection split + per‑path VT + verifier bond + `attestedPathsRoot`
  + the take‑0 custody hack (ELIMINATED).** The intersection split was redundant for fraud detection (a
  valid path is co‑signed = agreed by construction; an invalid one is caught by the `0x402` check) and a
  weak effort proxy; the take‑0 "NO operates / contract custodies the validator hotkey" binding was
  fragile and may not even hold cleanly on‑chain. Replaced by **independent validators + the §9.3
  bounty** (D8). §13.6.
- **Per‑provider miner UIDs (REJECTED).** 100k+ ≫ the ~256 UID cap. This is the entire reason for the
  pool design (D5).
- **Single contract miner UID / contract as sole validator (REJECTED).** Collapses Yuma to nothing
  (no consensus) — the degeneracy the multi‑validator design exists to avoid.
- **Full‑EVM and commitments‑only settlement (NOT CHOSEN).** See D1.
- **Collapsing the validator side to pure native dividends, no effort reward (REJECTED).** Would gut the
  validator incentive and the failure data — the product (user). See D8.

**The circular axis, named:** *quality‑in‑weight vs deposit‑only* was circled twice (deposit‑only →
"use Yuma maximally" put quality in → deposit‑only simplification → reverted to quality‑in‑weight). The
user's instinct was right each time it mattered: **quality‑in‑weight is the spine. Locked. Stop
re‑opening the weight formula.**

---

## 3. Open questions / deferred to later revisions

- **(Y) escalation.** If the (X) bounty proves too small to pull enough trail coverage (native dividends
  are ∝ stake, so a high‑stake validator can coast), escalate to (Y). Trigger = observed coverage too
  thin. §13.6.
- **`Q_n` aggregation + sampling spec (UNDER‑SPECIFIED — needs design).** How per‑provider reliability
  aggregates to the pool scalar (a flat **mean hides bad providers**; a **sum rewards count** — likely
  traffic/usage‑weighted reliability), plus an **EMA across epochs** and the sampling/coverage model for
  100k‑provider pools. This is the most important remaining under‑specification **for the tail** — the **head needs no such
aggregation**: per-provider `Q_p` *is* the top-level miner's weight (D17, §8.4).
- **`VALIDATOR.md` §10 roadmap (payout‑grade).** Proof‑of‑routing, destination diversity, validator Sybil
  resistance. Until these land, validator rewards are **provisional**; v1 leans on the independent
  validator population + `φ` + the swing cap. `VALIDATOR.md` §1 is explicit that trails prove *transit*,
  not honest relay of real traffic ("teaching to the test") — so `Q_n` measures liveness, not service
  quality. Don't over‑read it.
- **Within‑pool provider payout is NO‑discretionary.** The NO commits the payout root for its providers;
  it's *auditable* against validated paths but the NO assembles it, so providers trust their NO (and can
  exit). Inherent to pools.
- **Validator‑set plutocracy / bootstrap.** The independence assumption is load‑bearing, but the permit
  cap (top‑k by stake) + stake‑weighted dividends concentrate validation among large stakers. Mitigate:
  low min‑stake, raise `max_allowed_validators`, owner‑run independent validators at launch, and the
  (capital‑lighter) bounty. State it as the central assumption it is.
- **Multi‑pool (Pool 1 / "VPN factory").** Deferred to a one‑liner (§14); add via a second mechanism.
- **Exact validator‑hotkey / key custody.** Verify against a live chain before launch (childkey vs
  proxy vs nomination semantics; "contract address = coldkey" custody). §16.4.
- **`ω` governance tension.** The owner sets `ω` but loses that slice of its cut, so a short‑term‑greedy
  owner underfunds the data bounty — make `ω` a governed parameter, not owner discretion.

- **(v0.2) θ ramp schedule.** θ is the new load-bearing dial (D20); the start value and the signals that
  justify widening it (per-tier realized pay; validator quality-consensus maturity) need tuning in practice.
- **(v0.2) Head size vs. UID budget.** Pin `V` (validator slots) and `P` (NO-pool slots); is "~200" firm or
  "256 − V − P"? (§14.)
- **(v0.2) Binding granularity.** One hotkey may bind many `client_id`s (simple) vs. one UID per `client_id`
  (D18, §11.4) — confirm.
- **(v0.2) Demotion grace.** Beyond `immunity_period` + the `Q_p` EMA, is an explicit "recently-top" buffer
  wanted, or is native lowest-emission churn acceptable? (§8.4.)

---

## 4. Load-bearing research facts + verify-before-launch flags

From the deep research pass; current (post‑dTAO) Bittensor. **Bittensor changed a lot in 2024–2025 and
many public docs are stale — pin a `subtensor` release tag and verify against a live chain.**

**Mechanics we rely on:**
- Emission split **18% owner / 41% miner / 41% validator** (α). Tempo default **360 blocks**; block
  ~12 s. Emission accrues to hotkeys as **α stake** per epoch — it **cannot be pushed into an EVM
  contract**; that's why miner UIDs are contract‑*owned* (their stake accrues to the contract coldkey).
- **α is not a liquid ERC‑20** — it's stake keyed `(coldkey, hotkey, netuid)`. `transferStake` /
  `moveStake` within‑netuid are **slippage‑free**; only α↔TAO hits the AMM.
- **Precompiles** (pin + re‑verify ABIs — they're not formally versioned, issue #2455): Staking V2
  **`0x805`** (use V2, not legacy `0x801`), Neuron **`0x804`** (`setWeights`/`commitWeights`/
  `burnedRegister`), Metagraph **`0x802`** (`getIncentive`/`getEmission`/…), Alpha **`0x808`** (price/
  emission/sim‑swap), BalanceTransfer **`0x800`**, **Ed25519Verify `0x402`** (verify `VALIDATOR.md`
  proofs on‑chain). EVM = Frontier/Cancun, Solidity 0.8.24, chain **964** mainnet / **945** testnet,
  permissionless deploy, gas in TAO.
- **Commitments pallet** is `Pays::No` (free), Merkle‑friendly (32‑byte roots), keyed `(netuid, hotkey)`
  — a free public mirror for roots; the *contract* holds the roots that gate claims.

**Specific stale‑doc / live‑value flags (query the live chain, set explicitly):**
- `commit_reveal_weights_enabled` default flipped across versions — we want **true** (D6); confirm.
- `tao_weight` is **0.18** live (genesis constant differs). `SubnetOwnerCut` ≈ 18% (configurable).
- Subnet cap is **128** (not 256, a common misconception); `max_allowed_uids` **256**,
  `max_allowed_validators` **64** — relevant to NO/validator counts.
- Subnet creation cost is dynamic/volatile — quote `btcli subnet burn-cost`.
- Cross‑subnet emission allocation flipped (price → flow → price) through 2025–2026; it affects subnet
  *totals*, not the within‑subnet 41/41/18 we depend on.

**v0.2 (two-tier) load-bearing facts — verified against `subtensor` `main`, 2026-06-30:**
- **`max_allowed_uids` = 256 is a HARD ceiling** (owners may lower, never raise); **`mechanism_count ×
  max_UIDs < 256`**, so two mechanisms halve UID space to ~127 — the two tiers must share **one** mechanism
  (D19). `max_allowed_validators` default is **128** (root-only), not 64 — but it is a permit count, not a
  slot partition.
- **Deregistration evicts the lowest-*emission* neuron** (incentive + dividends; tie → oldest reg-block →
  lowest UID; owner/immune skipped). No history; **no native promote/demote** — the "top-N" is purely
  emergent from weights + churn. `immunity_period` ≈ 4096 blocks default (tunable). A top miner can be pruned
  on a quality dip once immunity expires (mitigate: high immunity + `Q_p` EMA + θ headroom).
- **Miner incentive is credited to the miner's own hotkey/coldkey — no take, not shared with nominators**
  (clean direct payout). **Child hotkeys CANNOT route miner emission** ("only the validation emission is
  split amongst parents") — so each top miner needs its own UID; you cannot pool miner emission via childkeys.
- **`set_weights` is the only steering lever**; any tier/quota/θ logic must be encoded in the weight vector
  and adopted by a **stake-majority of validators** to survive Yuma's κ=0.5 median clipping.
- **`max_weight_limit` defaults to no cap (65535)** — must be set. Proportional (not winner-take-all)
  weighting fits ~200 concurrent providers; Wilson-interval + latency scoring (FileTAO / TPN) is field
  best-practice and matches `VALIDATOR.md` §7.
- **Identity binding standard:** signed proof + ss58 + metagraph-membership check, fail-closed (Epistula /
  ORO-AI `bittensor-auth`); dual-signed association for client↔hotkey (Celium `associate_evm_key`).
- **June-2026 Spec 421** reverted cross-subnet allocation to price-based with a `(1 − miner_burned)` term —
  so **don't burn miner emission to an owner/immune UID** to reserve the head/tail split (D19).
- **No precedent** for a tiered "top-N direct UIDs + pooled/off-chain tail" — D16 is novel; the field
  consolidates behind one UID and pays pooled tails off-chain.

---

## 5. Conversation arc (for context)

1. Deep Bittensor research → `WHITEPAPER.md` v0.1.
2. Direction set (AskUserQuestion): EVM‑contract+Yuma settlement, α token, Yuma‑weighted, native
   tempo + 7‑day epoch.
3. User: contract custodies all emission; NO holds none; claim from contract (D3).
4. User: Phase 0/1 governance (D11).
5. Review #1 → user: remove oracle (D9), strengthen self‑dealing/settlement; **more miners + use Yuma
   maximally**; rejected the "collapse to 1 UID" simplification.
6. Scale correction → **pool design** (D5); multi‑validator Yuma with quality‑in‑weight; commit‑reveal
   on. Community validators added (D7).
7. Review #2 → user: validators must be **strongly** incentivized for the data → kept an effort‑linked
   reward (not pure native dividends).
8. **(X)/(Y)** decision → chose (X); eliminated validator pool / intersection split / VT / bond / take‑0
   hack (D8, §2).
9. Review #3 → applied **optimistic effort verification** (D12) + under‑sampling coverage (D13). User
   flagged we were circling.
10. Brief **deposit‑only** detour → **reverted** to `deposit × Q_n` (D6); kept the **bootstrap swing
    cap** (D14). Settled the circular axis.

---

11. **Top-level miners iteration (v0.2).** User: add a direct top-200 head beside the pool, steered
    natively, identified by `client_id ⇄ hotkey`; keep the pool as the on-ramp/baseline (`deposit ×
    quality`). Four research passes (native steering, identity binding, tiered designs, weight-setting) →
    `iterations/ITERATION_TOP_MINERS.md` → folded into the docs as D16–D20. `VERIFIER.md` renamed `VALIDATOR.md`.

*Maintenance: when a decision changes, update the matching `D#` here and the referenced `WHITEPAPER.md`
section together. When a §3 open question is resolved, move it into §1 as a new `D#`.*
