# UR Subnet vs. the Bittensor field — a macro design comparison

**What this is.** A neutral, first-principles comparison of the UR Subnet design (this repo's
`WHITEPAPER.md` / `seed/INCENTIVES.md` / `VALIDATOR.md`) against ~12 well-respected or instructive Bittensor
subnets, at the level of **macro mechanism-design themes** — not implementation detail. The goal is to
see **where we follow Bittensor best practice** and **where we diverge in direction**. Divergence here is
a *choice*, not a deficiency: we note the trade-off each side is making and the bet we are taking.

**Companion visual:** `diagrams/comparison_matrix.png` renders the matrix in §3 at a glance
(green = aligned, amber = divergent, purple = novel).

> **Confidence & method.** Built from three parallel research passes (cross-cutting protocol norms; deep
> per-subnet profiles read from GitHub/docs; and an adversarially-verified claim synthesis), **extended
> 2026-07-04** with primary-source profiles of **Hippius (SN75)** and **BlockMachine (SN19)** — identity
> confirmed on taostats, mechanism read from each project's whitepaper/docs/GitHub (the deep-research run's
> adversarial-verify stage was interrupted, so the two new subnets' *mechanism* claims were re-checked by
> hand against primary sources rather than by the full 3-vote panel; identity/status for both is
> taostats-confirmed). Today is **mid-2026 (dTAO era)**. Where a fact is protocol-level and primary-sourced it is treated as **high
> confidence**; per-subnet specifics are sourced to repos/docs but the landscape moves fast. Honored
> caveats: (a) precise market-cap rankings/standings are **volatile and could not be reliably verified** —
> treat all "standing" language as illustrative; (b) the cross-subnet *emission-allocation* rule
> flip-flopped in 2025–26 (price → Taoflow net-staking-flow → price by ~June 2026), but the **within-subnet
> 18/41/41 split, Yuma Consensus, and anti-gaming core were unchanged** throughout; (c) Yuma rewards
> consensus-*conformity* weighted by stake, which is only a *proxy* for objective quality (arXiv
> 2507.02951); (d) "commit-reveal" is a per-subnet **opt-in** (default off), not a protocol default.

---

## 1. Executive summary

**The one-line takeaway:** the UR Subnet is **orthodox on the emissions/consensus backbone** and
**deliberately heterodox on demand-coupling, miner-tiering, and settlement/custody**. We keep the Bittensor
coinbase and Yuma exactly as-is, and we spend our "novelty budget" on **two genuinely novel bets** — tying
emission to *costly, revenue-backed demand* (now carried in the pooled **tail**), and **tiering a native,
direct-steered "head" of the top ~200 providers above that trustlessly-settled pooled tail** — plus the
divergent choice to settle *100k+ off-chain tail providers trustlessly through a contract*. The new
identity plumbing the head needs — binding an off-chain worker to a hotkey — is, by contrast, **well-precedented**.

**Biggest similarities (we follow the norm):**
- **Standard 18 / 41 / 41 owner/miner/validator split** — protocol-fixed; we adopt it verbatim ("we do not fight the coinbase").
- **Validators' evaluation drives miner emission via Yuma Consensus** — the canonical Bittensor mechanism; our contract sits *downstream* of Yuma (tail only), not in place of it.
- **Native direct steering of top miners.** `set_weights → Yuma → incentive`, credited **natively to the miner's own hotkey/coldkey (no take, not shared)**, with the **top-N kept for free by lowest-emission deregistration churn** — this is exactly how the field pays direct-UID miners; our head channel uses it verbatim.
- **The native anti-gaming stack** — stake-weighted-median clipping, vtrust, bonds/Liquid Alpha, self-weight masking — all on; we also opt into commit-reveal.
- **Native validator dividends.** Validators earn Bittensor-native dividends (∝ stake × vtrust), exactly the field-standard validator reward; v1 adds **no** validator-side reward of its own.
- **α/dTAO-denominated economics**, slippage-free internal stake transfers, α as the value-capture token.
- **Oracle-free** — like the field, we avoid on-chain oracles and use off-chain reference data.
- **Real-world/DePIN output** and **pool-style scaling past the 256-UID cap** — both are *established* (if minority) patterns, not inventions; the head and pooled tail **share one 256-UID metagraph**.
- **Off-chain-worker identity binding.** To steer the head we reuse the field-standard **signed-proof + ss58 + metagraph-membership check, fail-closed** (Epistula / ORO-AI `bittensor-auth`), specialized to a Celium-style **dual-signed `client_id`-`hotkey` association** (the `associate_evm_key` shape). This piece is *aligned*, not novel.

**Biggest divergences and novel bets (same goal, different direction):**
- **Demand coupling — novel bet #1.** The field couples emission to **token-market/speculative demand** (α price or net TAO staking flow); we couple it to **costly, revenue-backed operator deposits**: validators weight each pool **`implied_usage × quality`** (implied usage = a NO's epoch deposit ÷ its conviction-tier rate), computed off the **on-chain deposit events + a published tier-rate schedule** — the contract itself weighs nothing (v0.4/D25). *Closest precedents: **BlockMachine (SN19)**, which couples emission to **metered priced usage** (`RU_served × self-declared USD/RU`) — the nearest thing to on-chain demand-coupled emission the field has, but via a **usage meter + price bid**, not a costly stake — and **Chutes**, whose revenue→buyback coupling is only **indirect**.* We differ from BlockMachine on *how demand is proven*: it can **directly meter** usage because RPC requests flow through a **protocol gateway**; VPN bandwidth has no trustless meter, so we substitute a **costly sunk deposit** as the demand proxy (§4.15). Since WHITEPAPER v0.3 (D23) we ALSO run the buy-and-lock flywheel that both Chutes and BlockMachine (its 1:1 α buyback → Protocol Stability Reserve) demonstrate: every deposit is itself a **buy-and-lock buyback** — now **conviction stake**, locked into a dividend-compounding reserve, never distributed, its cumulative amount setting the NO's rate tier; miners are paid from emission only (WHITEPAPER §7.4/§12.4) — so the deposit is simultaneously the **direct steering signal** (novel) and the **indirect price support** (now field-corroborated, not just field-adjacent). The bet now lives in the **pooled tail** (the `1−θ` share); the head is deliberately demand-agnostic — ranked on **routable-IP breadth** (§4.16). — **our headline bet.**
- **Tiered head/tail miner side — novel bet #2.** No subnet **tiers** individual top-N providers into their own native-emission UIDs *above* a pooled/off-chained tail. The field does the **opposite** — it *consolidates* behind one UID (Chutes: "never register more than one UID… just add capacity to one miner"; ComputeHorde fronts many executors behind one UID; TPN/Vanta pool many workers behind one UID) — and pays the pooled tail **off-chain at operator discretion** (TPN: "the mining pools get to decide how they pay their workers"). Our trustless on-chain pool (tail) **plus** native direct head is **without Bittensor precedent**.
- **Reward custody.** The field pays **native emission straight to hotkeys**; we keep exactly that for the **head** (top providers paid natively, no contract in the loop), but route **within-tail** payout through an **EVM contract that custodies deposits + emission** and settles by Merkle claim. EVM custody exists elsewhere only for **validator-scoped slashable collateral**, never operator-scoped demand deposits or pool settlement.
- **Worker payout trust.** Where operators normally **pay their off-chain workers at their own discretion**, our **head** providers are paid **natively-direct** (the most trust-minimized, canonical case — no operator in the loop) and our **tail** providers **claim α trustlessly on-chain** against a committed Merkle root (no-custody).
- **Cryptographic verification of a real-world service.** We push a **cryptographic routing-verification** protocol for bandwidth-class work that the field generally verifies with **heuristics** (geo-IP/latency) — closer to Targon's TEE attestation than to a VPN subnet's connectivity checks.

---

## 2. The subnets we compared (and why)

Selected for **diversity of category** and **sustained reputation** (teams, longevity, real usage). Subnet
numbers are the commonly-attributed identities (verified via each project's repo/docs); **exact emission/
market-cap standings are volatile and intentionally not asserted here**.

| # | Name (team) | Category | Why it's here |
|---|---|---|---|
| SN64 | **Chutes** (Rayon Labs) | Serverless compute / inference | The field's clearest **revenue→token** flywheel; the revenue-coupling benchmark |
| SN4 | **Targon** (Manifold Labs) | Confidential compute / inference | The field's most **cryptographic verification** (Intel TDX / NVIDIA CC attestation) |
| SN8 | **Taoshi / Vanta** | Finance (trading signals) | Oldest credible finance subnet; **realized-PnL scoring**; "Entity-miner" pooling |
| SN1 | **Apex** (Macrocosmos) | LLM / algorithmic competitions | Bittensor's oldest flagship; **deterministic sandboxed scoring** |
| SN9 | **IOTA** (Macrocosmos) | Decentralized pretraining | Flagship **collaborative training**; pivoted off winner-take-all |
| SN13 | **Data Universe** (Macrocosmos) | Data collection / scraping | Long-running **data DePIN**; credibility-gated, burn-weighted scoring |
| SN51 | **Celium / Lium** (Datura) | GPU compute marketplace | **EVM collateral contract** precedent; real compute rental |
| SN12 | **ComputeHorde** | GPU compute (for other subnets) | **EVM collateral + executor-pooling** precedent (one UID, many workers) |
| SN65 | **TPN — Tao Private Network** | VPN / bandwidth DePIN | **Our closest analog**: decentralized WireGuard exits, mining-pool topology |
| SN56 | **Gradients** (Rayon Labs) | Model training / AutoML | **Burned TAO entry-fee** — the closest thing to a costly demand-deposit |
| SN75 | **Hippius** (The Nerve Lab) | Decentralized cloud storage / compute | **DePIN with real paid usage** that solves our four problems the *opposite* way — its **own L1 chain** off Bittensor; ZK proof-of-storage; revenue-share flywheel |
| SN19 | **BlockMachine** | Decentralized RPC / archive-node marketplace | **The nearest demand-coupling precedent**: emission ∝ *metered priced usage* (`RU × USD/RU`) + a **1:1 α buyback**; the RPC subnet that took over netuid 19 after Nineteen wound down |

*Also referenced:* **SN26 Storb** (storage; Proof-of-Data-Possession), **FileTAO** (storage; Wilson-score
provider scoring) and **Beam** (orchestrator UID + Merkle-verified chunks). *(Note on netuid 19: Rayon's former
**Nineteen** inference subnet is **wound down** — the `rayonlabs/nineteen` repo is gone and netuid 19 was
re-registered ~2026; the "unrelated RPC/archive subnet" now on netuid 19 is **BlockMachine**, which we profile
below as SN19 — so the loose end is resolved, not excluded.)* An illustrative — **unverified** — mid-2026
market-cap top-5 floated by secondary sources put Chutes (SN64), τemplar (SN3), Targon (SN4), affine (SN120),
and lium (SN51) near the top (with Chutes' own α down ~85% from its mid-2025 ATH); we cite this only to show
roughly who is in the conversation, **not as a ranking** — exact standings are volatile and were not reliably
verifiable.

---

## 3. The comparison matrix

Rows = major design decisions. **Verdict:** ALIGNED (follow the norm) · DIVERGENT (same goal, different
direction) · NOVEL (little/no precedent). Rendered visually in `diagrams/comparison_matrix.png`.

| # | Design decision | Bittensor majority pattern | UR Subnet direction | Verdict |
|---|---|---|---|---|
| 1 | **Emission split** | Fixed 18 / 41 / 41, protocol-enforced | Standard 18 / 41 / 41 — "don't fight the coinbase" | **ALIGNED** |
| 2 | **Consensus engine** | Validators score miners; Yuma drives emission | Independent validators score pools + top miners; Yuma drives miner emission | **ALIGNED** |
| 3 | **Anti-gaming stack** | Always-on Yuma core; commit-reveal & Liquid Alpha opt-in | Full stack ON incl. commit-reveal + Liquid Alpha | **ALIGNED** |
| 4 | **Validator independence** | Many independent validators; stake-weighted permits; owner-heavy at launch is the norm | Permissionless independent validator UIDs; no NO owns one; v1 launches **owner-majority by construction** (the reserve compounds it, §4.4) | **ALIGNED** (owner-majority launch caveat) |
| 5 | **Token & economics** | α-denominated, dTAO; stake/price = demand proxy | All α; slippage-free transferStake; α buy/stake pressure | **ALIGNED** |
| 6 | **On-chain oracles** | Avoided; validators fetch off-chain, Yuma median reconciles | No oracle; off-chain published reference rate | **ALIGNED** |
| 7 | **Multi-mechanism subnets** | ≤2 mechanisms/subnet, each own Yuma + bonds | Pool 0 (core) / Pool 1 (VPN factory) via sub-mechanisms | **ALIGNED** |
| 8 | **Scaling past the 256-UID cap** | 1 UID fronts many off-chain workers (ComputeHorde, TPN, Vanta); or move workers off Bittensor onto your own L1 (Hippius SN75) | Pool UIDs (tail) + top ~200 direct provider UIDs (head) share one 256-UID metagraph — **stay in-metagraph, no own chain** | **ALIGNED** (in-metagraph pooling; Hippius takes the own-L1 fork) |
| 9 | **Real-world / DePIN output** | Respected minority: compute, storage, VPN/bandwidth | Privacy/VPN — providers carry ingress/egress traffic | **ALIGNED** |
| 10 | **Verification rigor** | Trending crypto/deterministic (Targon TEE, Hippius Plonky3 ZK proof-of-storage, BlockMachine Merkle-proof + re-execution); heuristic for bandwidth-class work | Cryptographic routing-verification (signed proof-of-transit) — Targon-class rigor aimed at bandwidth | **ALIGNED** (leading edge) |
| 11 | **Off-chain-worker identity binding** | Signed proof + ss58 + metagraph-membership check, fail-closed (Epistula / ORO-AI) | Celium-style dual-signed client_id-hotkey association; same fail-closed metagraph check | **ALIGNED** |
| 12 | **Reward settlement & custody** | Pure native emission to hotkeys; no contract in the reward loop | Head: pure native emission, no contract. Tail: EVM contract custodies deposits + emission, Merkle-settles | **DIVERGENT** (partial — head native) |
| 13 | **Worker payout trust model** | Operator pays its off-chain workers at its discretion | Head = native direct (canonical, most trust-minimized); tail = trustless on-chain Merkle claim | **DIVERGENT** |
| 14 | **Validator rewards** | Native dividends only (∝ stake × vtrust) | Native dividends only (∝ stake × vtrust) — the Bittensor norm; v1 adds no validator-side reward | **ALIGNED** |
| 15 | **Miner reward basis / demand coupling** | Mostly pure measured work / speculative-token-coupled; an emerging minority couples to real usage (BlockMachine SN19: `RU × USD/RU` metered; Chutes: revenue→buyback) | implied_usage × quality in the tail (validators compute it off published deposit events — the contract weighs nothing) — a **costly sunk deposit** as the demand proxy where usage can't be metered | **NOVEL** (deposit-as-proxy; BlockMachine is the nearest neighbor via metered usage) |
| 16 | **Miner tiering (head / tail)** | Consolidate behind one UID (Chutes: "never register more than one UID"); pool the tail off-chain | Top-N providers promoted to their own native UID (head) above a trustless pooled tail — tiered, one metagraph | **NOVEL** |

**Tally: 12 aligned · 2 divergent · 2 novel.** (Validator rewards, row 14, are plain native dividends — the
Bittensor norm — so the miner-side custody rows are the only two divergences, alongside the two novel bets.)

---

## 4. Per-theme analysis

### 4.1 Emission split — ALIGNED (high confidence)
The within-subnet split is a protocol-fixed **18% owner / 41% miners / 41% validators(+stakers)** (owner cut
`SubnetOwnerCut ≈ 11796/65535`); it survived dTAO, Taoflow, and the May-2026 Conviction change. We adopt it
verbatim and deliver emission through the chain coinbase — the contract only *custodies and settles the pool
tier* (it computes no weights, D25). Adopting this is "following the default" almost tautologically.
[learnbittensor.org/learn/emissions; bittensor.com/dtao-whitepaper]

### 4.2 Consensus engine — ALIGNED (high)
On Bittensor, `coinbase → drain_pending_emission() → full Yuma Consensus` over validators' stake-weighted
weight matrices allocates miner emission (`R_j = Σ_i S_i·W̄_ij`; validator impact ∝ stake). Our premise —
"validators' evaluation drives miner emission, the Bittensor way" — *is* this mechanism. Crucially, our EVM
contract handles **downstream within-pool Merkle settlement, not cross-UID allocation**, which stays with
Yuma. [learnbittensor.org/learn/anatomy-of-incentive-mechanism]

### 4.3 Anti-gaming stack — ALIGNED, with two honest caveats (high)
We switch on the protocol-native machinery everyone leans on: stake-weighted-median clipping (κ=0.5), vtrust
penalties for consensus departure, EMA bonds / Liquid Alpha, self-weight masking. Two caveats we keep in
view: **(a)** Yuma rewards consensus-*conformity* as a proxy for accuracy, and rewards are empirically
**stake-dominated** (a critical analysis finds stake–reward correlation ≈ 0.80–0.95 vs performance ≈ 0.50;
arXiv 2507.02951) — so we should not oversell Yuma as rewarding objective quality; our *implied_usage × quality*
design is partly an attempt to add an objective anchor. **(b)** Commit-reveal is **not** a protocol default
(it defaults off, per-subnet opt-in) — turning it on is our deliberate choice for a subjective-quality
signal, not "following the majority." [docs.bittensor.com/subnets/consensus-based-weights;
learnbittensor.org/concepts/commit-reveal]

### 4.4 Validator independence — ALIGNED (high), with an owner-majority launch posture
The field runs many independent validators earning permits by stake; we do the same and explicitly forbid a
Network Operator from owning a validator. This is the structural source of the "independent measurement" our
mechanism needs, and it is standard. One honest caveat since v0.3 (D23): **v1 launches owner-majority by
construction** — the owner holds most α at genesis, and the buyback reserve is **staked to the owner-validator
hotkey** (WHITEPAPER §7.4), compounding that majority every tempo. An owner-dominant validator at launch is
itself normal in the dTAO era (a new subnet's owner holds most α on day one), and the reserve-staking leg is
the same auto-staking move as Chutes' buyback — but it does make decentralizing the validator set a
**deliberate, budgeted later step** (re-delegating reserve slices across independent validators is the lever),
not a passive default. Entry stays permissionless throughout (stake α → permit), the
anti-gaming stack is on from day one, and the full Yuma machinery does real work as independents join.

### 4.5 Token & economics — ALIGNED (high)
α/dTAO is the universal model: per-subnet alpha, price/stake as the chain's value signal, slippage-free
within-netuid stake transfers. We are fully inside it (everything in α; `transferStake` for internal moves;
α buy/stake pressure as the value-capture path). Our one wrinkle — an **off-chain reference rate** (since
v0.4/D25 a **tier→rate schedule**: locked conviction stake lowers a NO's deposit rate) so operators can
target a *fixed real price* despite α volatility — is an application-layer convenience, not a protocol
change.

### 4.6 On-chain oracles — ALIGNED (high)
Subnets avoid on-chain oracles by architecture: validators fetch external data off-chain, and Yuma's
stake-weighted median reconciles (e.g., Foundry's S&P oracle, Synth's Pyth pulls). We match this — there is
**no on-chain oracle**; the "global rate" is an off-chain governance-published reference (a **tier→rate
schedule** since v0.4/D25, read by validators to compute implied usage — never consumed by the contract), and
the *costly deposit*, not the rate, is the enforced signal. [learnbittensor.org/learn/yuma-consensus]

### 4.7 Multi-mechanism (Pool 0 / Pool 1) — ALIGNED (high)
Bittensor shipped **multiple incentive mechanisms per subnet** (~Sept 2025, cap 2/subnet), each with its own
Yuma + bond pool and an owner-set emission split — exactly the primitive our Pool 0 (core) / Pool 1 (VPN
factory) design assumes. We are using a supported feature, not bending the chain.
[learnbittensor.org/subnets/understanding-multiple-mech-subnets]

### 4.8 Scaling past the 256-UID cap — ALIGNED (high)
The UID cap is **256**. The sanctioned scale-out pattern is **"1 UID fronts many off-chain workers, operator
settles with them off-chain"**: ComputeHorde spawns many dockerized GPU *executors* under one miner UID;
**TPN (SN65) pools many off-chain VPN workers (no neuron each) behind one UID**; Vanta (SN8) maps up to
10,000 subaccounts onto ≤10 UIDs; Beam uses an orchestrator UID. Our **tail** is exactly this pattern
(1 operator = 1 pool-UID, up to 100k providers inside), and the **head** adds a set of ordinary direct miner
UIDs, so head and pooled tail **share one 256-UID metagraph** (`256 ≥ head ~200 + one pool-UID per NO +
validator UIDs`; a *second mechanism* would halve the budget to ~127, too few for ~200 head UIDs, so
mechanisms stay reserved for the Pool 0 / Pool 1 product split, §4.7). The *pooling* primitive is aligned;
the **tiering** of direct head UIDs *above* the pooled tail is the novel part (§4.16), and the *trustless
on-chain* settlement of the tail is the divergent part (§4.12–4.13).

There is also a **second, architecturally opposite** way to beat the 256 cap, and **Hippius (SN75)** takes
it: **run your own L1**. Hippius keeps a Bittensor subnet only as the α-emission source and pushes
**registration, per-node accounting, and settlement onto its own Substrate chain** (BABE + staking), where
storage workers register via a `registration` pallet (`registerNodeWithColdkey`/`registerNodeWithHotkey`),
never taking a Bittensor UID at all — the metagraph cap simply doesn't apply to them. On the Bittensor side
it *also* pools: weights are assigned per **account "family"** (one owner, many child nodes), and only the
**top 10 nodes count, each contributing 80% of the previous** (a geometric within-identity cap). We
deliberately take the **opposite fork from the own-L1 move**: we do **not** fork a chain — head and tail
both live **inside Bittensor's 256-UID metagraph** and settle on the **Subtensor EVM**, so we inherit
Bittensor's security and liquidity rather than bootstrapping our own validator set and token bridge. (The
own-L1 route buys unbounded worker scale and native economic enforcement at the cost of standing up and
securing a whole second chain — the §4.12 custody trade in a different guise.)
[github.com/backend-developers-ltd/ComputeHorde; github.com/taofu-labs/tpn-subnet; github.com/thenervelab/thebrain; docs.hippius.com/learn/weights]

### 4.9 Real-world / DePIN output — ALIGNED with the respected minority (high)
Most flagship subnets are purely digital (AI/data), but a **respected DePIN minority** delivers real-world
services: compute (SN51 Lium, SN27, SN64 Chutes, SN12 ComputeHorde), storage (SN26 Storb, FileTAO,
**SN75 Hippius**), bandwidth/VPN (**SN65 TPN**), RPC/archive-node infrastructure (**SN19 BlockMachine**),
scraping (SN13, SN22, SN42). A privacy/VPN bandwidth network sits squarely in this camp — unusual versus the
AI majority, but well-precedented, and the minority is **growing and increasingly revenue-bearing** (Hippius
sells storage; BlockMachine sells RPC calls) — the same real-demand footing we build on.

### 4.10 Verification rigor — ALIGNED, at the leading edge (medium–high)
The 2026 trend is **away from gameable LLM-judge toward deterministic/cryptographic ground truth**: zkML
(SN2 Omron), deterministic re-execution (SN4 Targon's legacy logprob check; SN9's recompute+cosine),
realized-outcome scoring (SN8 Taoshi, SN6), **hardware attestation / TEE** (SN4 Targon, SN64 Chutes), and —
newly confirmed in this pass — **ZK proof-of-storage** (**Hippius SN75**: its "Arion" layer runs a **"Warden"
audit service issuing Plonky3 zero-knowledge proof-of-storage circuits** against Reed-Solomon shards, feeding
a reputation score) and **Merkle-proof + re-execution** (**BlockMachine SN19**: the gateway verifies each RPC
response against a **trusted state root via Merkle proof in real time**, and validators **re-execute sampled
queries against reference nodes**, a confirmed mismatch → permanent coldkey ban). Importantly, for
**externally-delivered real-world work** the field's rigor **tracks how checkable the work is**: storage
(possession is provable) and RPC (responses are deterministically re-executable) get cryptographic proofs,
while **bandwidth/VPN** generally *falls back to heuristics* (geo-IP, latency, sampling) — "you can't
cryptographically prove a packet traversed a residential line." Our `VALIDATOR.md` cryptographic
routing-verification (signed validated paths, Ed25519, proof-of-transit) deliberately tries to bring
**Targon-/Hippius-class rigor to the hardest-to-verify DePIN class** — which is both our differentiation and
our hard problem (the honest-relay gap we acknowledge in v1). [arxiv.org/abs/2507.02951;
manifold.inc/releases/targon-v6; github.com/thenervelab/arion; blockmachine.io/whitepaper]

### 4.11 Off-chain-worker identity binding — ALIGNED (high)
To steer the **head**, a validator must turn its per-provider `VALIDATOR.md` measurements (keyed by
`client_id`) into a `set_weights` on the right miner UID — it needs a trustworthy `client_id → UID` map.
This is a **well-precedented** problem, and we use the **field-standard** solution: sign the binding with the
worker's key, carry the ss58, verify the signature, then **confirm the ss58 is a live UID in the metagraph
and fail-closed on a stale/absent membership** (Epistula; ORO-AI `bittensor-auth`; Taoshi even namespaces
`synthetic_hotkey = {hotkey}_{client_id}`). Our one specialization is a **dual-signed association** — both
the `client_id` key *and* the hotkey sign the linkage, exactly the shape of SN51 Celium's `associate_evm_key`
— so a miner cannot claim a `client_id` it does not operate and steal another provider's measured quality.
Binding proves *ownership*; `VALIDATOR.md` proves *quality*; the two **compose, never merge** (the field is
unanimous that attestation proves work is *real*, never *whose* — Targon keeps the hotkey out of its TEE
quote). Unlike the tiering it serves (§4.16), **this piece invents nothing** — it assembles the standard
auth pattern. [epistula docs; github.com/ORO-AI/bittensor-auth; github.com/Datura-ai/celium-collateral-contracts]

### 4.12 Reward settlement & custody — DIVERGENT, now partial (high)
**Norm:** rewards are pure native α emission to hotkeys via `setWeights → Yuma → per-block emission`; **no
smart contract sits in the reward loop.** The only EVM-custody precedent is **validator-scoped slashable
collateral** — `bactensor/collateral-contracts` (one contract *per validator per subnet*, miner posts its
own TAO bond, validators slash), live on **SN12 ComputeHorde** and **SN51 Lium/Celium**. **Us — and this is
now *partial*:** the **head matches the norm exactly** — top miners are paid **pure native emission to their
own coldkey, with no contract in the loop**. Only the **tail** diverges: a *single subnet-wide* contract
**owns the NO-pool UIDs**, custodies operator **demand deposits + captured pool emission**, and settles by
Merkle claim. (Since v0.3/D23 the deposits it custodies are the **locked buyback reserve** — staked to the
owner-validator hotkey, dividend-compounding, never distributed — so the distributable surface is
emission-only, but the reserve makes the contract a **growing honeypot**, which is why the §6.4
timelock/guardian phases carry real weight.) The two-tier iteration therefore **shrinks the contract's
custody-critical surface** — it no
longer owns or custodies the head's emission at all. **Trade-off:** in the tail we gain trust-minimized
payout to 100k providers, a hard no-custody guarantee, and clean 7-day accounting; we pay with a
**custody-critical contract** (audit + governance burden) and a departure from the "native emission is the
whole story" simplicity — a departure the head now avoids entirely.

The two new comparanda **reinforce the norm** and sharpen how alone our tail-custody sits: **BlockMachine
(SN19)** pays **native α straight to miner hotkeys** (no reward contract; its only protocol treasury is the
buyback **Protocol Stability Reserve**, which is a *sink*, not per-worker custody), and **Hippius (SN75)**
**auto-stakes** each node's reward to its owner account **on Hippius's own chain** every ~3,600 blocks — a
push settlement with **no manual claim, no EVM custody, no Merkle distributor** (its only Solidity is a small
~1.7% **bridge**, not a reward custodian). So across the field — native-to-hotkey (BlockMachine), auto-stake
on an own-L1 (Hippius), and EVM *collateral* (ComputeHorde, Celium) — **no one custodies operator-scoped
demand deposits or runs an on-chain Merkle distributor to pay off-chain workers**; that remains our tail's
distinctive (and custody-critical) surface.
[github.com/bactensor/collateral-contracts; docs.lium.io/bittensor-subnet/collateral/overview; blockmachine.io/whitepaper; docs.hippius.com/learn/weights]

### 4.13 Worker payout trust model — DIVERGENT, toward trustlessness (high)
In the pool pattern (§4.8) the **operator pays its off-chain workers at its own discretion** — workers trust
the operator (TPN verbatim: *"the mining pools get to decide how they pay their workers"*). We remove that
discretion in **both** tiers, by opposite routes. The **head** is the **canonical, most trust-minimized**
case: top providers are paid **natively and directly by Yuma to their own coldkey — no operator in the path
at all**. The **tail** keeps the pool but makes its payout **trustless**: providers **claim α directly from
the contract** with an O(log N) Merkle proof against a committed root; the operator *directs* the split but
**never holds the α**, and finalized claims are un-clawback-able from day one. No verified precedent exists
for an **on-chain Merkle-distributor paying thousands of off-chain participants** — the trustless tail is the
genuinely new piece of our custody design (the native head is simply the field norm applied to the apex).
**Trade-off:** much stronger trust-minimization for providers; in the tail, more on-chain machinery and a
smaller intra-pool surface that Yuma does *not* directly score (we lean on the auditable payout list +
reputation there).

### 4.14 Validator rewards — ALIGNED (high)
**Norm:** validators earn **native Yuma dividends** (∝ stake × vtrust) — the standard, effort-agnostic
Bittensor validator reward. **Us:** exactly that, and **nothing more** — v1 adds no validator-side reward.
Validators stake α, run the `VALIDATOR.md` trails, score both miner tiers under commit-reveal, and are paid
**native dividends** flowing straight to their own hotkeys (the contract never custodies validator emission).
At launch the owner is the **majority validator** (α holdings + the reserve staked to its hotkey, §4.4) with
an intrinsic motive to run the trail volume — the failure data is the product its network runs on — and
commit-reveal already forces any dividend-earning validator to run real trails, so native dividends are a
**sufficient** effort signal for the launch configuration. Whether a richer, coverage-steered validator-effort
incentive is ever worth adding is an **open question we deliberately leave to a post-launch iteration**,
informed by what the live network shows about independent-validator coverage — not a v1 design element.
**Trade-off:** effort-agnostic dividends could in principle let a high-stake validator under-measure, but in
v1 that high-stake validator **is the owner**, whose business depends on the measurement — so the concern
only becomes real once the validator set is broadly independent, which is exactly when a future iteration
would revisit it.

### 4.15 Miner reward basis / demand coupling — NOVEL (medium–high), and the field is starting to move here
**Norm:** emission *is* the reward, and for most subnets it is coupled to **token-market/speculative demand** —
a subnet's α price (or, during the Taoflow interlude, net TAO staking inflows). **The protocol has no on-chain
revenue oracle and cannot distinguish real usage from speculation.** But this pass surfaced a **real, growing
minority that couples emission to genuine usage**, and it moves our claim from "unprecedented" to "leading a
nascent trend by a *different mechanism*":

- **BlockMachine (SN19) — the nearest neighbor, and a strong one.** Its miner weight is
  **`ask_i = RU_i × t_i`** — Request Units *actually served to paying customers* times the miner's
  **self-declared USD price per RU** — normalized against the epoch's α-value of emissions
  (`scale = min(1, M/ASK)`), with **unearned emission burned**. Customers **pay USD per RU through a
  protocol-operated gateway**, and — exactly like our reserve — **"for every α paid to miners the protocol
  buys an equal amount of α on the open market"** into a locked **Protocol Stability Reserve** (a 1:1
  buy-and-lock buyback). So BlockMachine couples emission to **real metered demand *and* runs the buyback leg**
  — it is arguably a **closer precedent than Chutes on both counts**.
- **Chutes (SN64) — indirect.** Funnels platform revenue into an **auto-staking buyback**, nudging the
  *market* signal only; it books customer payments to an **off-chain USD ledger** and sets miner emission
  **separately** via the dTAO market, so revenue and payout are decoupled by construction. (Independently-
  verified external revenue ~$1.3–2.4M/yr against a far larger emission subsidy — a ~22–40:1 ratio; the
  self-reported ~$10M ARR is disputed.)
- **Hippius (SN75) — revenue-share, off-Bittensor.** Splits **marketplace revenue 60/30/10** (miners /
  validators+stakers / treasury) on **its own chain**, and floats its Bittensor miner-vs-validator emission
  split by **"how much real storage the network holds relative to token emissions"** — a real-usage-indexed
  split, though of capacity, not customer payment.

**Us — the distinction is *how demand is proven*.** BlockMachine can **directly meter** usage because RPC
calls are cheap to proxy through a **trusted protocol gateway**; **VPN bandwidth has no such trustless meter**
(privacy, volume, and no honest party in the path), so we **cannot** price metered work the way SN19 does.
Instead we make the operator post a **costly, fully-sunk α deposit** as the demand *proxy*: validators turn
it into the cross-operator split, each weighting a pool **`implied_usage × quality`** (implied usage = the
NO's epoch deposit ÷ its conviction-tier rate) read off the **on-chain deposit events + a published tier-rate
schedule**; the contract weighs nothing (v0.4/D25). After the two-tier iteration this governs the **`1−θ`
tail share** (the **head** is demand-agnostic, ranked on **routable-IP breadth**, §4.16). So the honest verdict
is now sharper than "no precedent": **demand-coupled emission is an emerging edge of the field** (BlockMachine
metered, Chutes indirect, Hippius revenue-share), and our specific mechanism — a **costly sunk deposit
weighting Yuma where usage is unmeterable** — remains **without direct precedent**. The buyback leg is no
longer field-adjacent either: BlockMachine's 1:1 PSR buyback and Chutes' buy-and-lock both **corroborate**
our conviction-stake reserve (§4.12). **Trade-off:** a harder-to-fake demand signal at the cost of a
"pay-to-play" surface (mitigated structurally since v0.3/D23 — the **whole deposit is sunk** into the locked
reserve, so a wash-deposit round-trip recovers zero — plus independent-validator quality consensus + the
self-weight mask) and the assumption that deposits ≤ revenue long-run; plus the new θ tension — a larger head
dilutes how much emission this signal governs (§4.16, §8). *One caution worth carrying forward:* where a
subnet **can** meter usage (RPC, storage, compute), metered-price coupling (BlockMachine) is cleaner than a
deposit proxy — our deposit design is the right tool *specifically because bandwidth resists metering*, and
that's the framing to keep.
[bittensor.com/dtao-whitepaper; coingecko.com/learn/top-bittensor-subnets-dtao; blockmachine.io/whitepaper; docs.hippius.com/learn/substrate-staking]

### 4.16 Miner tiering (head / tail) — NOVEL (high)
**Norm:** the field **consolidates** — Chutes' own docs say *"Never register more than one UID… just add
capacity to one miner"*; ComputeHorde fronts many executors behind one UID; TPN and Vanta pool many off-chain
workers behind one UID. The standard move is to put *everything* behind a single UID and (for the pooled
workers) **pay them off-chain at the operator's discretion**. **Us:** we run the **opposite** — a **tiered**
miner side in one metagraph: the best ~200 providers are **promoted to their own native-emission UID** (the
**head**, ranked and steered on **routable-IP breadth** — split-adjusted distinct routable egress-IP count (v0.4/D27) — paid natively, no operator in the loop), sitting **above** a
**pooled tail** (one contract-owned UID per NO, `implied_usage × Q_n`, providers paid by trustless Merkle claim). A
provider **starts in the tail and graduates to the head**, the chain's **lowest-emission deregistration
churn** running that tournament for free — there is **no native promote/demote primitive**, and child hotkeys
**cannot** route miner emission, so each head slot is genuinely its own UID. No Bittensor subnet tiers direct
top-N UIDs *above* a pooled/off-chained tail — this is **without precedent**. **Trade-off:** a clean merit
ladder and a trust-minimized apex, governed by the head share **θ** (§8) — but θ trades the demand-coupling
bet (which now lives only in the `1−θ` tail, §4.15) against a routable-IP-breadth head, and a **subnet-funded head can
weaken NO deposit incentives** (a NO's best providers earn from the head, not its deposit-funded pool). We
start θ tail-weighted (~0.3) and ramp it as the head set and validator quality-consensus mature.
[github.com/rayonlabs/chutes-api; github.com/backend-developers-ltd/ComputeHorde; github.com/taofu-labs/tpn-subnet; SN13 weight reservation]

---

## 5. Per-subnet capsules

Each capsule: mechanism in brief + the single sharpest contrast with us. (Per-subnet specifics sourced to
each project's repo/docs; standings omitted as unverifiable.)

**SN64 — Chutes (Rayon Labs) · serverless compute/inference.** Miners run GPU fleets behind one API/UID (a
single UID fronts thousands of GPUs via an **off-chain instance registry**); Chutes' own docs are explicit —
*"Never register more than one UID… just add capacity to one miner"* — the canonical **consolidate-behind-
one-UID** posture our head/tail tiering deliberately inverts (§4.16). Reward = normalized verified
GPU-compute, **not** stake-weighted; verification is **GraVal hardware-bound GPU attestation + Intel TDX /
NVIDIA TEE** (it attests the *hardware*, not output correctness); there is **no commit-reveal** (plain
`set_weights`; the defense is a reproducible audit). Distinctively, Rayon funnels **product revenue into an
auto-staking α buyback** — the field's strongest revenue→token flywheel (buy-and-**lock**, not burn).
Tellingly, **customer payments are booked to an off-chain USD ledger and miners are settled entirely
separately in native emission** — the dollar size of pay is set by the dTAO market, not by usage.
*Sharpest contrast:* Chutes is the closest thing to "revenue-coupled," yet the coupling is **indirect
(revenue→price)**; we couple **directly (deposit→emission weight)** — now via validator-computed **`implied_usage × quality`** off the published deposit events, the contract itself weighing nothing (v0.4/D25) — and since WHITEPAPER v0.3 (D23) we
also adopt their buy-and-lock leg (the deposit is **conviction stake**, locked into a compounding reserve,
WHITEPAPER §7.4), so the designs now differ in the steering, not the flywheel. Note our miners are, like
theirs, settled in native emission — the deliberate v0.3 trade (WHITEPAPER §12.4).

**SN4 — Targon (Manifold Labs) · confidential compute/inference.** Pivoted from a 2024 deterministic logprob
verifier to **cryptographic hardware attestation** (Intel TDX / AMD SEV-SNP + NVIDIA Confidential Computing,
re-attested ~every 72 min); rewards via a **demand-tiered compute auction**. *Sharpest contrast:* Targon
proves *what hardware/model runs* cryptographically; we try to prove *that traffic was actually relayed*
cryptographically — same rigor ethos, applied to bandwidth instead of GPUs.

**SN8 — Taoshi / Vanta · finance.** Miners submit trading signals scored on **deterministically-replayable,
high-water-marked USD PnL** (currently 100% PnL weight); validators **burn** the gap between emission and real
performance (P20). Its **"Entity-miner"** model **consolidates** thousands of subaccounts onto ≤10 UIDs (the
pool-behind-few-UIDs norm again), and it namespaces workers as `synthetic_hotkey = {hotkey}_{client_id}` — a
concrete **worker→hotkey binding** precedent we lean on for the head (§4.11). A **TIP-P22 EVM contract** locks
Theta as collateral. *Sharpest contrast:* even this finance subnet pays on **simulated** performance with **no
real capital**, *burns* overpayment rather than coupling to revenue, and **pools rather than tiers** — the
inverse of both our deposit-coupling and our head/tail split.

**SN1 — Apex (Macrocosmos) · LLM/competitions.** Now a **winner-take-all** platform of **deterministic,
sandboxed objective-function competitions** (miners submit code/model artifacts), with a time-decay emission
burn to prevent leader stagnation. *Sharpest contrast:* purely digital, winner-take-all, emission-as-reward —
versus our continuous, deposit-weighted, real-world-service payout to a whole pool.

**SN9 — IOTA (Macrocosmos) · pretraining.** Abandoned winner-take-all for **collaborative pipeline/data-
parallel training**: many miners train one large model, paid **proportionally to validated work** (backward
passes), with Shapley-style contribution attribution; honesty via **recompute + cosine-similarity**.
*Sharpest contrast:* a centralized **orchestrator** assigns and verifies work; we push verification to
**independent validators** and trust-minimize **payout** on-chain.

**SN13 — Data Universe (Macrocosmos) · data/scraping.** Credibility-gated, **burn-weighted** scoring
(`source × job × freshness × dedup-bytes × credibility²·⁵`), with ~70% of miner emission redirected to the
owner UID and validation by **live re-scrape**. *Sharpest contrast:* paying demand here only changes **which**
data is rewarded (re-weighting a fixed pie), never **how much** — exactly the coupling we make load-bearing.

**SN51 — Celium / Lium (Datura) · GPU marketplace.** P2P GPU rental with SSH challenge-response + benchmarks,
and an **EVM collateral contract** (miners lock slashable TAO). Its `associate_evm_key` flow — where **both
keys sign the linkage** — is the **dual-signed association** pattern we adopt verbatim for the
`client_id ⇄ hotkey` binding that steers the head (§4.11). *Sharpest contrast:* its EVM contract holds
**validator-scoped security collateral**; ours holds **operator demand deposits + emission** and *settles*
payouts.

**SN12 — ComputeHorde · GPU for other subnets.** One miner UID spawns many **executors** (explicit
256-cap workaround) with **collateral/slashing** — it **consolidates** every executor behind that one UID,
never tiering the best ones into their own UIDs. *Sharpest contrast:* the **canonical "one UID, many off-chain
workers" precedent** we build on for the **tail** — but it settles with workers off-chain and uses the contract
for **collateral**, where we settle the tail **trustlessly on-chain by Merkle claim** and tier the head above
it as native UIDs (§4.16).

**SN65 — TPN / Tao Private Network · VPN/bandwidth DePIN.** **Our closest analog:** decentralized WireGuard
**exit nodes**, a **mining-pool topology** where one UID aggregates many off-chain VPN workers, scored with
**robust per-run latency statistics** (the field-standard DePIN scoring our pool quality `Q_n` matches) and verified
by **geo-IP + latency/connectivity heuristics**. Crucially, **"the mining pools get to decide how they pay
their workers"** — the pooled tail is paid **off-chain at operator discretion**, the norm we invert.
*Sharpest contrast:* same shape and domain, but TPN verifies liveness **heuristically**, **pools without
tiering**, and pays workers **off-chain**; we aim for **cryptographic proof-of-transit**, a **native-UID head
tiered above** the pool, and **trustless on-chain** tail payout, anchored by **operator demand deposits**.

**SN56 — Gradients (Rayon Labs) · training/AutoML.** Tournament-style training with a **burned TAO entry-fee**.
*Sharpest contrast:* that entry-fee is the field's closest cousin to a **costly demand-deposit** — but it is a
**gate/sink**, not a per-operator weight on emission; we turn the costly deposit into the **emission-weighting
signal** itself.

**SN19 — BlockMachine · decentralized RPC / archive-node marketplace.** The subnet now on **netuid 19** (the
slot Rayon's wound-down *Nineteen* vacated): miners run **blockchain RPC/archive nodes** (Bittensor
subtensor lite/archive; Ethereum via reth/erigon) behind an **authenticated protocol gateway**, and — the
part that matters to us — **emission is coupled to real metered demand at a declared price**:
weight `ask_i = RU_i × t_i` (Request Units *served to paying customers* × the miner's **self-set USD/RU**),
scaled to the epoch's α-value of emissions with **unearned α burned**, and a **1:1 α buyback into a Protocol
Stability Reserve** on top. Verification is genuinely rigorous: the gateway checks each response against a
**trusted state root by Merkle proof in real time**, and independent validators **re-execute sampled queries
against reference nodes** (mismatch → permanent coldkey ban); settlement is **native α to hotkeys** (no
contract custody). *Sharpest contrast:* BlockMachine is the **nearest thing to us in the whole field** —
demand-coupled emission *plus* a buy-and-lock reserve — but it can **directly meter** usage because RPC flows
through a trusted gateway and its price is a **self-declared bid**; we serve **unmeterable VPN bandwidth**, so
we replace the meter+bid with a **costly, fully-sunk deposit** as the demand proxy and weight
`implied_usage × quality` (§4.15). Same destination (emission that answers to real demand, backed by a
buyback), opposite tool for proving the demand.

**SN75 — Hippius (The Nerve Lab) · decentralized cloud storage / compute.** A **revenue-bearing DePIN** that
solves *our* four problems — real demand, scale past 256, cryptographic verification of real-world work, and
reliability rewards — by the **architecturally opposite route: it runs its own L1**. Hippius keeps a Bittensor
subnet (SN75) only as the α-emission source and pushes **registration, per-node accounting, and settlement
onto its own Substrate chain** (BABE + staking); storage miners register via a `registration` pallet and
**never take a Bittensor UID**, sidestepping the 256 cap entirely. Miners are scored on **measured service —
`70% bandwidth-served + 30% storage-held`, each on a log2 diminishing-returns curve, × uptime** — not raw
capacity; storage is proven **cryptographically** (its "Arion" layer's **"Warden" service issues Plonky3 ZK
proof-of-storage** against Reed-Solomon shards, `k=10/m=20`); rewards **auto-stake** to the owner every ~6h on
Hippius's chain (no claim, no EVM custody, no Merkle); and **marketplace revenue is shared 60/30/10** with a
**credit-based** payment layer. It even soft-pools and soft-tiers: weights are per **account "family"**, only
the **top 10 nodes** count with **80% geometric decay**. *Sharpest contrast:* Hippius and UR want the same
things, but Hippius **forks a chain** to get them (own L1 for accounting + native economic enforcement),
whereas UR **stays entirely inside Bittensor** — one 256-UID metagraph + the Subtensor EVM + Merkle claims —
inheriting Bittensor's security/liquidity instead of bootstrapping a second chain, and coupling demand through
a **costly deposit** rather than a **capacity-indexed split + revenue-share**.

**SN26 — Storb (storage, referenced).** S3-like object storage verified by **Proof-of-Data-Possession + erasure
coding** — an example of cryptographic verification where the work is intrinsically checkable, the rigor bar we
target for routing.

**FileTAO (storage, referenced).** Scores providers on **Wilson-score confidence intervals** with tiers — the
robust real-world-DePIN scoring our pool quality `Q_n` uses verbatim (Wilson liveness + latency percentiles,
EMA-smoothed; `VALIDATOR.md` §7). With TPN's robust latency statistics, it confirms our quality measurement is
field best-practice, not bespoke.

---

## 6. Where we diverge — and the bet behind it

Each divergence is intentional. Stated as *bet → risk accepted*.

1. **Deposit-weighted emission (the headline).** *Bet:* a costly, revenue-backed deposit is a harder-to-fake
   demand signal than measured output alone, and auctioning the emission subsidy to real usage makes the
   cross-operator split track genuine demand. A small but growing minority now couples emission to real usage —
   **BlockMachine (SN19)** meters priced RPC work directly, **Chutes** approximates it via buyback — but our
   mechanism (a **costly sunk deposit** as the demand proxy, for a service that **can't be metered** trustlessly)
   is still without direct precedent (§4.15). *Risk:* "pay-to-play" optics and wash/self-deposits — mitigated
   structurally: since v0.3 (D23) the **entire deposit is sunk** (locked into the compounding reserve, never
   distributed), so the round-trip recovers zero; plus quality consensus by **independent** validators and
   the self-weight mask; and the assumption that `deposit ≤ revenue` long-run. After the
   two-tier iteration this bet lives in the **`1−θ` tail** (the head is ranked on routable-IP breadth), so the share of emission
   it governs is now set by θ (bet #2).

2. **Tiered head/tail miner side (the second novel bet).** *Bet:* a newcomer needs a **low-barrier on-ramp
   with a baseline reward** (the pooled tail — no UID, no burn) *and* the best providers deserve the
   **canonical, trust-minimized** Bittensor treatment (their own native UID, steered directly, paid natively).
   So we **tier** a direct head above a pooled tail in **one metagraph**, letting the chain's lowest-emission
   deregistration churn run the graduation tournament for free. No subnet does this — the field *consolidates*
   behind one UID (Chutes: "never register more than one UID") and pays the pooled tail off-chain at operator
   discretion. *Risk:* the head share **θ** is a new, load-bearing economic dial. Too large and most emission
   goes to the IP-breadth head, **diluting the demand-coupling bet** (now only in the `1−θ` tail) and
   **weakening NO deposit incentives** — a NO's best providers earn from the subnet-funded head, not its
   deposit-funded pool; too small and graduating is a pay cut that breaks the ladder. Mitigation: govern θ,
   **start tail-weighted (~0.3)**, instrument realized per-tier pay, ramp as the head set and validator
   quality-consensus mature, and hold the constraint *lowest-paid head miner ≥ highest-paid tail provider*.

3. **EVM-contract custody + on-chain Merkle pool payout (tail only).** *Bet:* it is the only trust-minimized
   way to pay 100k off-chain **tail** providers, enforce no-custody, and run clean 7-day settlement — using the
   Subtensor EVM rather than fighting the coinbase. The head needs none of it (it is native emission), so the
   two-tier iteration **shrank** this surface to the tail. *Risk:* a custody-critical contract (audit +
   timelock/guardian governance), and more moving parts than "native emission to a hotkey."

4. **Trustless worker payout (no-custody).** *Bet:* providers shouldn't have to trust an operator to pay them.
   The **head** is the strongest form — native, direct, no operator in the loop at all; the **tail** keeps the
   pool but pays by direct on-chain claims against a committed root, un-clawback-able once finalized — both
   strictly more trust-minimizing than the discretionary off-chain norm. *Risk:* intra-tail quality is not
   *directly* Yuma-scored (we lean on auditable payout lists + reputation); more on-chain surface.

5. **Cryptographic verification of a real-world service.** *Bet:* verifiable proofs beat the heuristic
   (geo-IP/latency) verification used by bandwidth/VPN peers, and match the field's verifiable-compute trend
   (now including Hippius's ZK proof-of-storage and BlockMachine's Merkle-proof + re-execution).
   *Risk:* v1 proves **liveness, not honest-relay**; closing that gap (Sybil resistance, proof-of-routing,
   destination diversity) is an explicit roadmap, and rewards stay provisional until then.

6. **Off-chain reference rate instead of an on-chain oracle.** *Bet:* usage is self-reported and unverifiable
   on-chain, so an on-chain oracle would have no enforcement power; the **costly deposit**, not the published
   rate, is the real signal. *Risk:* the rate is a governance-published off-chain input — but its abuse is
   bounded because deposits cost real α regardless of the rate.

**Net read.** We are conservative exactly where the Bittensor community has strong, battle-tested consensus
(coinbase, Yuma, anti-gaming, α economics, oracle-avoidance, **native direct-UID steering**, and the
**signed-proof identity pattern**) and we concentrate our novelty on the places our first principles demand
it: **making emission answer to real demand** (now in the tail), **tiering a native merit head above a
trustlessly-settled pooled tail**, and **paying a 100k-provider real-world network trustlessly**. The field's
own trajectory — toward verifiable proofs (now including **Hippius's ZK proof-of-storage** and **BlockMachine's
Merkle-proof + re-execution**), toward **revenue-bearing DePIN** (Hippius storage, BlockMachine RPC), toward
**demand-coupled emission and buy-and-lock reserves** (BlockMachine's metered `RU×price` + 1:1 buyback is the
nearest neighbor to our design), and toward EVM economic primitives and pooled off-chain fleets — is moving
*in our direction*, and faster than the last pass showed. We are still ahead of it on **deposit-as-demand-proxy
for unmeterable work**, on **tiering**, and on **trustless in-metagraph on-chain settlement** — where the
field's alternative (Hippius) is to **fork its own L1** — and we accept the corresponding complexity and
verification-hardness as the price of those bets. The honest adjustment from this pass: "no one couples
emission to real demand" is no longer true — so the claim is now the sharper, still-defensible one that **no one
does it by our *mechanism*, for our *hardest-to-verify* class of work**.

---

## 7. Sources

**Protocol / cross-cutting (primary unless noted):**
- Emissions & coinbase — https://docs.learnbittensor.org/learn/emissions · https://docs.learnbittensor.org/navigating-subtensor/emissions-coinbase
- Incentive anatomy & Yuma — https://docs.learnbittensor.org/learn/anatomy-of-incentive-mechanism · https://docs.learnbittensor.org/yuma-consensus/ · https://github.com/opentensor/subtensor/blob/main/docs/consensus.md
- Consensus-based weights / Liquid Alpha — https://docs.bittensor.com/subnets/consensus-based-weights
- Commit-reveal — https://docs.learnbittensor.org/concepts/commit-reveal
- Multiple mechanisms / sub-mechanisms — https://docs.learnbittensor.org/subnets/understanding-multiple-mech-subnets
- Hyperparameters (256 UID cap, toggles) — https://docs.learnbittensor.org/subnets/subnet-hyperparameters
- dTAO whitepaper (price-guided emission) — https://bittensor.com/dtao-whitepaper
- The Bittensor Standard — https://bittensor.com/content/the-bittensor-standard
- Critical analysis (stake-dominance of rewards) — https://arxiv.org/abs/2507.02951 (html: /html/2507.02951v1)
- Subnet landscape / standings (secondary, treat as illustrative) — https://taostats.io/subnets · https://www.coingecko.com/learn/top-bittensor-subnets-dtao · https://oakresearch.io/en/analyses/fundamentals/bittensor-tao-dynamic-tao-dtao-upgrade-changes-everything

**EVM custody / collateral precedent (primary):**
- https://github.com/bactensor/collateral-contracts · https://docs.lium.io/bittensor-subnet/collateral/overview · https://github.com/Datura-ai/celium-collateral-contracts

**Pooling / scale-out (primary):**
- ComputeHorde — https://github.com/backend-developers-ltd/ComputeHorde
- TPN (VPN) — https://github.com/taofu-labs/tpn-subnet
- Beam — https://subnetalpha.ai/subnet/beam/

**Identity binding & native steering (primary unless noted):**
- Epistula signed-request standard — https://epistula.sybil.com
- ORO-AI `bittensor-auth` (signed-proof + metagraph-membership check, fail-closed) — https://github.com/ORO-AI/bittensor-auth
- Celium dual-signed association (`associate_evm_key`) — https://github.com/Datura-ai/celium-collateral-contracts
- Native steering, lowest-emission dereg churn, child-key limits (subtensor `main`) — `run_coinbase.rs` · `registration.rs` · `set_children.rs` (https://github.com/opentensor/subtensor)
- FileTAO storage subnet — Wilson-score interval provider scoring (repo: `filetao-storage-subnet`)
- SN13 weight reservation (head/tail θ precedent) — https://github.com/macrocosm-os/data-universe (`apply_burn_to_weights`)

**Revenue / demand coupling:**
- Chutes revenue→buyback (secondary/blog) — https://www.coingecko.com/learn/top-bittensor-subnets-dtao · https://ownyourmind.ai/tokenomics/bittensor-subnets-where-the-revenue-is/ · https://pineanalytics.substack.com/p/the-bear-case-for-bittensor-tao (disputes self-reported ARR)
- BlockMachine (SN19) metered `RU×USD/RU` emission + 1:1 α buyback → Protocol Stability Reserve (primary) — https://blockmachine.io/whitepaper · https://blockmachine.io/technology (verified 2026-07-04; SN19 = "decentralized RPC and archive node network", https://taostats.io/subnets/19)
- Hippius (SN75) marketplace revenue-share 60/30/10 + credit-based payment (primary) — https://docs.hippius.com/learn/substrate-staking

**Per-subnet (primary repos/docs):**
- SN1 Apex — https://docs.macrocosmos.ai/subnets/subnet-1-apex · https://github.com/macrocosm-os/apex
- SN13 Data Universe — https://github.com/macrocosm-os/data-universe · https://docs.macrocosmos.ai/subnets/subnet-13-data-universe
- SN9 IOTA — https://arxiv.org/abs/2507.17766 · https://github.com/macrocosm-os/iota · https://docs.macrocosmos.ai/subnets/subnet-9-iota
- SN8 Taoshi/Vanta — https://github.com/taoshidev/proprietary-trading-network · https://docs.taoshi.io/
- SN4 Targon — https://github.com/manifold-inc/targon · https://manifold.inc/releases/targon-v6 · https://simplytao.ai/blog/targon-sn4-and-intel-tdx-confidential-compute-on-bittensor
- SN64 Chutes — https://chutes.ai/docs/core-concepts/security-architecture · https://github.com/rayonlabs/chutes-api ("never register more than one UID") · https://oakresearch.io/en/analyses/innovations/rayon-labs-subnet-leader-bittensor-tao
- SN51 Lium/Celium — https://github.com/Datura-ai/compute-subnet · https://docs.lium.io
- SN12 ComputeHorde — https://github.com/backend-developers-ltd/ComputeHorde
- SN65 TPN — https://github.com/taofu-labs/tpn-subnet
- SN56 Gradients — https://github.com/rayonlabs/G.O.D (miner docs)
- SN26 Storb — https://github.com/storb-tech/storb
- SN19 BlockMachine — https://blockmachine.io/whitepaper · https://github.com/taostat/blockmachine (netuid 19 confirmed via https://taostats.io/subnets/19 — the RPC/archive subnet now on the slot Rayon's *Nineteen* vacated)
- SN75 Hippius (The Nerve Lab) — https://taostats.io/subnets/75 · https://docs.hippius.com/learn/weights · https://github.com/thenervelab/thebrain (own Substrate L1) · https://github.com/thenervelab/arion (Iroh + Reed-Solomon + Plonky3 ZK proof-of-storage "Warden")

> **Reproduce / extend:** the verdicts above feed `diagrams/comparison_matrix.py`. Open items to re-confirm
> before relying on standings: live mid-2026 emission/market-cap ranks (volatile, JS-gated); per-subnet
> commit-reveal/Liquid-Alpha enablement; and a full 3-vote adversarial re-verification of the **BlockMachine/Hippius mechanism**
> claims (this pass hand-checked them against primary sources after the automated verify stage was interrupted —
> identity/status are taostats-confirmed, but e.g. BlockMachine's UID-topology / 256-cap handling is
> **unstated in its whitepaper** and Hippius's Bittensor-side validator count is inferential).

---

## 8. Analysis — do our divergences make sense, or should we follow the leader?

> A strategic assessment — **judgment, not verified research** — synthesizing the findings above against our
> first principles. Opinionated by request; the trade-offs it names are the load-bearing ones.

**Verdict.** Our divergences make sense — they are concentrated exactly where our *situation* genuinely
differs from the field's, not scattered out of contrarianism — and we should **not** be more conservative
wholesale. Doing so would discard our structural advantages. But the bets carry very different risk; "be
conservative" is the right instinct in exactly one spot, and the two-tier iteration adds **one new tension to
watch — the head/tail share θ** (it can dilute the very demand-coupling that is our edge).

### 8.1 What "following the leader" would actually mean

Most leaders share a pathology we would be *adopting*, not escaping: emission is **decoupled from real demand**
(~20–40× emission-to-revenue subsidy, even at Chutes); rewards are **stake-dominated, not quality-driven**
(stake↔reward correlation ~0.80–0.95 vs performance ~0.50, arXiv 2507.02951); scoring is **frequently gamed**
(SN1's envelope exploit, SN13's 15 anti-exploit resets, LLM-judge attacks); and control planes are
**operationally centralized**. Those designs are excellent *for their objective* — "emission is the product,
token price is the scoreboard." Our objective differs: we are an incentive layer for a **real business with
real revenue and 100k real providers**. Real paying demand is no longer *unique* on Bittensor — this pass found
a growing revenue-bearing minority (**Hippius** sells storage, **BlockMachine** sells RPC, Chutes sells
inference), and **BlockMachine even couples emission to it directly** — but it is still **rare**, and coupling
emission to it for a service you **can't meter trustlessly** is rarer still. Copying a demand-decoupled design
to look normal would throw away our scarcest asset. That is conservatism in the wrong place.

The operative rule: **conserve the plumbing, innovate the economics** — which is precisely what the matrix
shows we do (11 of 16 decisions are straight best-practice).

### 8.2 Where we are (rightly) conservative — keep it

We do not fork Yuma, change the 18/41/41 split, invent a consensus, or replace the anti-gaming stack. All
novelty is spent on economics, none on the battle-tested safety machinery. This is the correct risk-budget
allocation; leave it alone.

### 8.3 The three bets, ranked by risk

**1. Deposit-weighted emission — highest conviction *and* highest risk. Keep it; red-team it hardest.**
The right bet *because* we have real revenue (rarer than it looks, even if no longer unique — see §8.1), and a
costly, revenue-backed deposit is Sybil-resistant where measured-output scoring is cheap to fake. A new
data-point sharpens the bet rather than undercutting it: **BlockMachine (SN19)** proves demand-coupled emission
*works on-chain today* — but it can only do so because RPC is **meterable through a trusted gateway**; our
deposit design exists **precisely because bandwidth isn't**, so BlockMachine is validation of the *goal* and a
reminder of *why our tool differs*, not a cheaper path we're ignoring (§4.15). The sharpest critique to respect:
the field is already criticized for capital-beating-merit, and `implied_usage × quality` leans into
capital-weighting. Our
defense is real — our capital is **productive and revenue-bounded** (the deposit is fully sunk into the locked
reserve, sized to usage), not speculative stake — and since v0.3 (D23) the wash-deposit **round-trip is closed
structurally** (recovery through the payout channel is zero; a higher conviction tier costs *more* locked
capital, not less). What remains is the *steering* channel, and it holds only if **(a)** independent
validators are numerous/honest enough that quality consensus can't be captured, and **(b)** quality `Q_n`
genuinely bites at maturity rather than collapsing to "biggest depositor wins." Both are tunable, and our
bootstrap ramp (cap quality early, widen as
validators mature) is the right *conservative-within-the-novel* move. Note the iteration narrows this bet's
reach to the `1−θ` tail (see #2). Action item: **instrument the deposit:quality balance explicitly**, and
treat self-dealing as something to empirically disprove, not argue away on paper.

**2. Head/tail tiering and the θ dial — the new structural bet, and the one new economic tension. Keep; watch θ.**
Tiering itself is low-risk, high-value: a native, trust-minimized merit apex over a low-barrier on-ramp is
strictly better for providers than either alone, the dereg churn runs the ladder for free, and a per-UID burn
makes the head *more* Sybil-resistant than one pool-UID per NO. The subtlety is **θ**, the head share, because
it **trades our headline bet against the merit apex.** A large head is more meritocratic and more
trust-minimized, but it **demand-*de*couples** most of the 41% (the deposit signal then governs only the
`1−θ` tail) **and** can **weaken NO deposit incentives** — a NO whose best providers earn from the
subnet-funded head has less reason to deposit, since its pool then funds only the baseline tail. That is a
genuine tension, not a wording nit: push θ too high and we hollow out the demand-coupling that is our edge. The
conservative-within-the-novel move is the recommended one — **govern θ, start tail-weighted (~0.3), instrument
realized per-tier pay, and ramp only as the head set and validator quality-consensus mature**, holding
*lowest-paid head miner ≥ highest-paid tail provider* so graduation is never a pay cut. Action item: **treat θ
as the dial to watch**, alongside the deposit:quality balance.

**3. EVM-contract custody + Merkle payout (tail only) — mostly *entailed*, not a standalone gamble.**
Not to be judged in isolation: once we commit to deposits (#1) and face the 100k-provider / 256-UID reality,
we need a contract to custody deposits and own the pool UIDs *anyway*, and pooling forces *someone* to split
rewards. The iteration confines this to the **tail** — the head is native — so the custody surface shrank. The
only genuinely optional piece is **trustless on-chain Merkle payout vs. the field norm of operators paying
workers off-chain at discretion** (TPN, ComputeHorde). That question is now **resolved and locked: no-custody +
trustless on-chain provider payout is a v1 must-have**, not a v2 hardening — for a decentralized privacy
network, providers must not have to trust an operator to be paid. Crucially this is *no-custody in spirit*,
**not** contract immutability: the **owner and NOs never hold or distribute α** (the contract is the sole
custodian and pays out only by on-chain pull claims, `transferStake`; the head is native), while the contract
itself stays **upgradeable + owner-multisig + guardian** for v1 — normal bug-fix latitude for a new subnet —
and is progressively locked down (`WHITEPAPER.md` §6.4). The earlier "start TPN-style off-chain and add the
claim later" fallback is **rejected**.

### 8.4 The risks that actually decide this (not mechanism soundness)

Two execution risks dwarf the design ones: **(1) dTAO emission tracks alpha price = market perception** — a
mechanism the market can't easily value can mean lower price → lower emission → less provider subsidy; our
real-revenue story is a *better* narrative, but only if we sell it. **(2) Validator recruitment** —
independent validators must run our bespoke `VALIDATOR.md` protocol, a heavier lift than generic validating.
v1 sidesteps this by construction (the owner-majority validator runs the volume, D23), but that only *defers*
the go-to-market: a later phase — when the quality-swing ramp needs an owner-independent baseline — has to
actually recruit independent validators (re-delegating reserve slices is the lever, and a coverage-steered
validator-effort incentive is one thing a next iteration could add if the live network shows it is needed),
and bespoke designs usually struggle exactly there.

### 8.5 What the July-2026 pass (Hippius, BlockMachine) challenges — and confirms

Adding two revenue-bearing DePIN subnets sharpened the picture more than it moved it. Net: they mostly
**corroborate** us, but BlockMachine lands one genuine challenge worth holding consciously.

**What they confirm (confidence up).**
- **The buyback reserve.** BlockMachine runs a **1:1 α buyback into a locked Protocol Stability Reserve**
  ("for every α paid to miners, buy an equal amount and lock it"); Chutes does buy-and-lock. Two independent
  designs converge on exactly our conviction-stake reserve (§4.15) — our most "field-adjacent" bet is now
  field-corroborated.
- **Native settlement is the norm.** BlockMachine pays native α to hotkeys; Hippius auto-stakes on its own
  chain. Neither runs a reward-custody contract or a Merkle distributor for off-chain workers — so our tail
  EVM-custody remains the genuinely distinctive (and heaviest) surface, exactly as §4.12 says.
- **Demand-coupling is the right direction.** BlockMachine proves emission *can* answer to real usage
  on-chain today. We are early on a real trend, not contrarian.

**The one genuine challenge — from BlockMachine — to the deposit (bet #1).**
BlockMachine couples emission to `RU_served × self-declared price`, **verified by re-execution against
reference nodes, with no costly deposit at all**. The signal is non-gameable for two reasons we lack: (a)
requests flow through a **protocol-operated gateway** that meters them, and (b) that gateway **routes
customers by price/quality**, so a miner that declares a silly price gets no traffic and earns zero — the
market disciplines the price for free. This reframes our deposit honestly: **it is not the ideal demand
signal, it is a substitute for usage-verification we can't get**, because we deliberately keep the protocol
*out* of the traffic/payment path (privacy + NO-as-independent-business + decentralization). Our single
load-bearing premise is *"VPN bandwidth can't be metered/billed through a trustless gateway."* If that
premise is even partly soft, BlockMachine's approach dominates ours on every axis (no pay-to-play surface,
no wash-deposit worry, no `deposit ≤ revenue` assumption, weight tracks served work not capital). The design
survives — the premise is real — but this is **the assumption to stress-test hardest before mainnet**,
because it is the whole reason we prefer a costly deposit to a clean meter.

**A milder challenge — from Hippius — to staying in-metagraph (#1) and the per-UID head (#2).**
Hippius **sidesteps the 256-cap entirely** by running its own L1 (storage miners aren't UIDs), which makes
our cap-gymnastics (~200 head UIDs + one pool UID per NO + validators, `mechanism_count=1`, dereg-churn) a
*self-imposed* constraint. Our reasons to stay in-metagraph are strong — inherit Bittensor security + α
liquidity instead of bootstrapping a whole chain, and Hippius arguably takes on the worst of both worlds (a
Bittensor slot *and* a full L1) — but it proves the escape hatch works if the metagraph ever gets too tight.
Separately, Hippius's **"family / top-10 / 80% geometric decay"** is a UID-*cheap* soft-tier; our per-UID
head is UID-*expensive*, but deliberately so — it buys **native-direct pay with no operator in the payout
path**, which the family model gives up (it pays the account owner, who redistributes). We're trading UID
budget for trust-minimization, on purpose.

**Alternatives worth carrying (not adopting now).** (1) A **partial usage meter as a deposit cross-check**:
have the NO `/verify` server attest per-provider served-byte counts (it already knows `client_id ⇄ traffic`),
validator-sample them, and use them to *bound* deposits (flag a NO depositing 100× its attested usage) — it
stays NO-trusted so it can't *replace* the deposit, but it narrows the wash-deposit surface that is our
headline risk. (2) Keep the head as UIDs, but **document a soft-tier fallback** (Hippius-style) if the head's
~200 UIDs ever over-pressure the metagraph. (3) Treat **own-L1 as a consciously-rejected option**, not a
default — revisit only if cap/custody pressure becomes acute. None forces a change now; each is a named lever
if a premise shifts.

### 8.6 Bottom line

Stay conservative on the consensus plumbing (we are), stay aggressive on demand-coupling (it is our edge), and
concentrate validation on the genuine unknowns — the **deposit-vs-quality balance / self-dealing defense**,
the **head/tail share θ** (how much of the 41% stays demand-coupled vs. flows to the IP-breadth head, and
whether it weakens NO deposits), and the **owner-majority → independent-validator handoff** (the reserve
deliberately compounds the owner's consensus seat, D23; decentralizing it is a budgeted step, its lever
re-delegating reserve slices). **No-custody + trustless on-chain payout is now locked as a v1 must-have**
(§8.3), not an open question.
Divergence here is a considered bet with a named trade-off, not a deficiency — and on our first principles, it
is the right one.
