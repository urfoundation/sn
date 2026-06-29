# UR Subnet vs. the Bittensor field — a macro design comparison

**What this is.** A neutral, first-principles comparison of the UR Subnet design (this repo's
`WHITEPAPER.md` / `INCENTIVES.md` / `VERIFIER.md`) against ~10 well-respected, established Bittensor
subnets, at the level of **macro mechanism-design themes** — not implementation detail. The goal is to
see **where we follow Bittensor best practice** and **where we diverge in direction**. Divergence here is
a *choice*, not a deficiency: we note the trade-off each side is making and the bet we are taking.

**Companion visual:** `diagrams/comparison_matrix.png` renders the matrix in §3 at a glance
(green = aligned, amber = divergent, purple = novel).

> **Confidence & method.** Built from three parallel research passes (cross-cutting protocol norms; deep
> per-subnet profiles read from GitHub/docs; and an adversarially-verified claim synthesis). Today is
> **mid-2026 (dTAO era)**. Where a fact is protocol-level and primary-sourced it is treated as **high
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
**deliberately heterodox on demand-coupling and settlement/custody**. We keep the Bittensor coinbase and
Yuma exactly as-is, and we spend our "novelty budget" on two things: tying emission to *costly,
revenue-backed demand* and settling *100k+ off-chain providers trustlessly through a contract*.

**Biggest similarities (we follow the norm):**
- **Standard 18 / 41 / 41 owner/miner/validator split** — protocol-fixed; we adopt it verbatim ("we do not fight the coinbase").
- **Validators' evaluation drives miner emission via Yuma Consensus** — the canonical Bittensor mechanism; our contract sits *downstream* of Yuma, not in place of it.
- **The native anti-gaming stack** — stake-weighted-median clipping, vtrust, bonds/Liquid Alpha, self-weight masking — all on; we also opt into commit-reveal.
- **α/dTAO-denominated economics**, slippage-free internal stake transfers, α as the value-capture token.
- **Oracle-free** — like the field, we avoid on-chain oracles and use off-chain reference data.
- **Real-world/DePIN output** and **pool-style scaling past the 256-UID cap** — both are *established* (if minority) patterns, not inventions.

**Biggest divergences (same goal, different direction):**
- **Demand coupling.** The field couples emission to **token-market/speculative demand** (α price or net TAO staking flow); we couple it to **costly, revenue-backed operator deposits**. *Closest precedent: Chutes' revenue→buyback, and even that is indirect.* — **our headline bet.**
- **Reward custody.** The field pays **native emission straight to hotkeys**; we route within-pool payout through an **EVM contract that custodies emission + deposits** and settles by Merkle claim. EVM custody exists elsewhere only for **validator-scoped slashable collateral**, never operator-scoped demand deposits or pool settlement.
- **Worker payout trust.** Where operators normally **pay their off-chain workers at their own discretion**, we make providers **claim α trustlessly on-chain** against a committed Merkle root (no-custody).
- **Explicit validator effort bounty.** The field pays validators **native dividends only** (stake × vtrust); we add a **fee-funded, coverage-weighted bounty** for verified trail volume.
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

*Also referenced:* **SN26 Storb** (storage; Proof-of-Data-Possession) and **Beam** (orchestrator UID +
Merkle-verified chunks). *(Excluded after verification: Rayon's former **Nineteen / SN19** inference subnet
appears **wound down** — netuid 19 was transferred ~Jan 2026 and now hosts an unrelated RPC/archive subnet,
and the `rayonlabs/nineteen` repo is gone; we don't rely on it.)* An illustrative — **unverified** — mid-2026
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
| 2 | **Consensus engine** | Validators score miners; Yuma drives emission | Independent validators score pools; Yuma drives miner emission | **ALIGNED** |
| 3 | **Anti-gaming stack** | Always-on Yuma core; commit-reveal & Liquid Alpha opt-in | Full stack ON incl. commit-reveal + Liquid Alpha | **ALIGNED** |
| 4 | **Validator independence** | Many independent validators; stake-weighted permits | Independent validator UIDs; no operator owns one | **ALIGNED** |
| 5 | **Token & economics** | α-denominated, dTAO; stake/price = demand proxy | All α; slippage-free transferStake; α buy/stake pressure | **ALIGNED** |
| 6 | **On-chain oracles** | Avoided; validators fetch off-chain, Yuma median reconciles | No oracle; off-chain published reference rate | **ALIGNED** |
| 7 | **Multi-mechanism subnets** | ≤2 mechanisms/subnet, each own Yuma + bonds | Pool 0 (core) / Pool 1 (VPN factory) via sub-mechanisms | **ALIGNED** |
| 8 | **Scaling past the 256-UID cap** | 1 UID fronts many off-chain workers (ComputeHorde, TPN, Vanta) | 1 operator = 1 pool-UID; up to 100k providers inside | **ALIGNED** |
| 9 | **Real-world / DePIN output** | Respected minority: compute, storage, VPN/bandwidth | Privacy/VPN — providers carry ingress/egress traffic | **ALIGNED** |
| 10 | **Verification rigor** | Trending crypto/deterministic; heuristic for real-world work | Cryptographic routing-verification (signed proof-of-transit) | **ALIGNED** (leading edge) |
| 11 | **Reward settlement & custody** | Pure native emission to hotkeys; no contract in the reward loop | EVM contract custodies emission + deposits, then settles | **DIVERGENT** |
| 12 | **Worker payout trust model** | Operator pays its off-chain workers at its discretion | Trustless on-chain Merkle claim; operator never holds the α | **DIVERGENT** |
| 13 | **Validator effort reward** | Native dividends only (stake × vtrust) — effort-agnostic | Dividends + explicit fee-funded, coverage-weighted bounty | **DIVERGENT** |
| 14 | **Miner reward basis / demand coupling** | Pure measured work; emission decoupled from real paying demand | deposit × quality — costly, revenue-backed demand weights pay | **NOVEL** |

**Tally: 10 aligned · 3 divergent · 1 novel.**

---

## 4. Per-theme analysis

### 4.1 Emission split — ALIGNED (high confidence)
The within-subnet split is a protocol-fixed **18% owner / 41% miners / 41% validators(+stakers)** (owner cut
`SubnetOwnerCut ≈ 11796/65535`); it survived dTAO, Taoflow, and the May-2026 Conviction change. We adopt it
verbatim and deliver emission through the chain coinbase — the contract only *steers distribution* and
*pays the bounty*. Adopting this is "following the default" almost tautologically.
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
arXiv 2507.02951) — so we should not oversell Yuma as rewarding objective quality; our *deposit × quality*
design is partly an attempt to add an objective anchor. **(b)** Commit-reveal is **not** a protocol default
(it defaults off, per-subnet opt-in) — turning it on is our deliberate choice for a subjective-quality
signal, not "following the majority." [docs.bittensor.com/subnets/consensus-based-weights;
learnbittensor.org/concepts/commit-reveal]

### 4.4 Validator independence — ALIGNED (high)
The field runs many independent validators earning permits by stake; we do the same and explicitly forbid a
Network Operator from owning a validator. This is the structural source of the "independent measurement" our
mechanism needs, and it is standard.

### 4.5 Token & economics — ALIGNED (high)
α/dTAO is the universal model: per-subnet alpha, price/stake as the chain's value signal, slippage-free
within-netuid stake transfers. We are fully inside it (everything in α; `transferStake` for internal moves;
α buy/stake pressure as the value-capture path). Our one wrinkle — an **off-chain reference rate** so
operators can target a *fixed real price* despite α volatility — is an application-layer convenience, not a
protocol change.

### 4.6 On-chain oracles — ALIGNED (high)
Subnets avoid on-chain oracles by architecture: validators fetch external data off-chain, and Yuma's
stake-weighted median reconciles (e.g., Foundry's S&P oracle, Synth's Pyth pulls). We match this — there is
**no on-chain oracle**; the "global rate" is an off-chain governance-published reference, and the *costly
deposit*, not the rate, is the enforced signal. [learnbittensor.org/learn/yuma-consensus]

### 4.7 Multi-mechanism (Pool 0 / Pool 1) — ALIGNED (high)
Bittensor shipped **multiple incentive mechanisms per subnet** (~Sept 2025, cap 2/subnet), each with its own
Yuma + bond pool and an owner-set emission split — exactly the primitive our Pool 0 (core) / Pool 1 (VPN
factory) design assumes. We are using a supported feature, not bending the chain.
[learnbittensor.org/subnets/understanding-multiple-mech-subnets]

### 4.8 Scaling past the 256-UID cap — ALIGNED (high)
The UID cap is **256** (validators 64). The sanctioned scale-out pattern is **"1 UID fronts many off-chain
workers, operator settles with them off-chain"**: ComputeHorde spawns many dockerized GPU *executors* under
one miner UID; **TPN (SN65) pools many off-chain VPN workers (no neuron each) behind one UID**; Vanta (SN8)
maps up to 10,000 subaccounts onto ≤10 UIDs; Beam uses an orchestrator UID. Our "1 operator = 1 pool-UID,
100k providers inside" is the **same pattern**. (The *trustless on-chain* settlement of those workers is the
divergent part — see §4.11–4.12.) [github.com/backend-developers-ltd/ComputeHorde; github.com/taofu-labs/tpn-subnet]

### 4.9 Real-world / DePIN output — ALIGNED with the respected minority (high)
Most flagship subnets are purely digital (AI/data), but a **respected DePIN minority** delivers real-world
services: compute (SN51 Lium, SN27, SN64 Chutes, SN12 ComputeHorde), storage (SN26 Storb, FileTAO),
bandwidth/VPN (**SN65 TPN**), scraping (SN13, SN22, SN42). A privacy/VPN bandwidth network sits squarely in
this camp — unusual versus the AI majority, but well-precedented.

### 4.10 Verification rigor — ALIGNED, at the leading edge (medium–high)
The 2026 trend is **away from gameable LLM-judge toward deterministic/cryptographic ground truth**: zkML
(SN2 Omron), deterministic re-execution (SN4 Targon's legacy logprob check; SN9's recompute+cosine),
realized-outcome scoring (SN8 Taoshi, SN6), and **hardware attestation / TEE** (SN4 Targon, SN64 Chutes).
Importantly, for **externally-delivered real-world work** (bandwidth/VPN/scraping) the field generally
*falls back to heuristics* (geo-IP, latency, sampling) — "you can't cryptographically prove a packet
traversed a residential line." Our `VERIFIER.md` cryptographic routing-verification (signed validated paths,
Ed25519, proof-of-transit) deliberately tries to bring **Targon-class rigor to bandwidth-class work** — which
is both our differentiation and our hard problem (the honest-relay gap we acknowledge in v1). [arxiv.org/abs/2507.02951;
manifold.inc/releases/targon-v6]

### 4.11 Reward settlement & custody — DIVERGENT (high)
**Norm:** rewards are pure native α emission to hotkeys via `setWeights → Yuma → per-block emission`; **no
smart contract sits in the reward loop.** The only EVM-custody precedent is **validator-scoped slashable
collateral** — `bactensor/collateral-contracts` (one contract *per validator per subnet*, miner posts its
own TAO bond, validators slash), live on **SN12 ComputeHorde** and **SN51 Lium/Celium**. **Us:** a *single
subnet-wide* contract that **owns the miner-pool UIDs**, custodies operator **demand deposits + captured
miner emission**, and settles by Merkle claim. **Trade-off:** we gain trust-minimized payout to 100k
providers, a hard no-custody guarantee, and clean 7-day accounting; we pay with a **custody-critical
contract** (audit + governance burden) and a departure from the "native emission is the whole story"
simplicity. [github.com/bactensor/collateral-contracts; docs.lium.io/bittensor-subnet/collateral/overview]

### 4.12 Worker payout trust model — DIVERGENT, toward trustlessness (high)
In the pool pattern (§4.8) the **operator pays its off-chain workers at its own discretion** — workers trust
the operator. We invert that: providers **claim α directly from the contract** with an O(log N) Merkle proof
against a committed root; the operator *directs* the split but **never holds the α**, and finalized claims
are un-clawback-able from day one. No verified precedent exists for an **on-chain Merkle-distributor paying
thousands of off-chain participants** — this is the genuinely new piece of our custody design. **Trade-off:**
much stronger trust-minimization for providers; more on-chain machinery and a smaller intra-pool surface
that Yuma does *not* directly score (we lean on the auditable payout list + reputation there).

### 4.13 Validator effort reward — DIVERGENT (high; precedent likely absent)
**Norm:** validators earn **native dividends only** (∝ stake × vtrust), which is **effort-agnostic** — a
high-stake validator can coast on consensus. No clean precedent was found for an explicit, fee-funded
validator *effort* reward that steers sampling toward under-covered work (the closest analogs are
subnet-custom validator incentives and slashing, not a coverage bounty). **Us:** dividends **plus** a
FeePool-funded (`φ·ΣD + ω·ownerCut`) **effort bounty** ∝ verified, coverage-weighted completed trails.
**Trade-off:** because our *product is the failure data*, we pay directly for the trail volume that produces
it and steer it to under-sampled providers; the cost is added complexity and an incentive **bounded by the
fee pool** (with an "(Y)" escalation — routing validator emission through the effort split — held in reserve).

### 4.14 Miner reward basis / demand coupling — NOVEL (medium–high)
**Norm:** emission *is* the reward, and it is coupled to **token-market/speculative demand** — a subnet's α
price (or, during the Taoflow interlude, net TAO staking inflows). **The protocol has no on-chain revenue
oracle and cannot distinguish real usage from speculation.** The single closest precedent is **Chutes (SN64)**,
which couples revenue only **indirectly**: it funnels platform revenue into an **auto-staking buyback** of its
own α, nudging the *market* signal — not a deposit that directly weights Yuma. Concretely, Chutes even books
customer payments to an **off-chain USD ledger** and sets miner emission **separately** via the dTAO market,
so revenue and miner payout are decoupled by construction. (Independently-verified Chutes external revenue is
~$1.3–2.4M/yr against a far larger emission subsidy — a ~22–40:1 ratio; the self-reported ~$10M ARR is
disputed, and Chutes itself purged ~40B tokens/day of unprofitable free traffic in 2026.) **Us:** per-operator **α deposits, sized to real usage at an off-chain reference rate**,
are the **objective anchor** that weights the cross-operator emission split (`deposit × quality`). Costly,
revenue-backed, on-chain demand directly steering miner emission is **without standard precedent on
Bittensor** — this is our defining bet. **Trade-off:** a harder-to-fake demand signal and emission that
tracks genuine usage, at the cost of a "pay-to-play" surface (mitigated by the non-refundable `φ` floor +
independent-validator quality consensus + self-weight mask) and the assumption that deposits ≤ revenue in the
long run. [bittensor.com/dtao-whitepaper; coingecko.com/learn/top-bittensor-subnets-dtao]

---

## 5. Per-subnet capsules

Each capsule: mechanism in brief + the single sharpest contrast with us. (Per-subnet specifics sourced to
each project's repo/docs; standings omitted as unverifiable.)

**SN64 — Chutes (Rayon Labs) · serverless compute/inference.** Miners run GPU fleets behind one API/UID (a
single UID fronts thousands of GPUs via an **off-chain instance registry**); reward = normalized verified
GPU-compute, **not** stake-weighted; verification is **GraVal hardware-bound GPU attestation + Intel TDX /
NVIDIA TEE** (it attests the *hardware*, not output correctness); there is **no commit-reveal** (plain
`set_weights`; the defense is a reproducible audit). Distinctively, Rayon funnels **product revenue into an
auto-staking α buyback** — the field's strongest revenue→token flywheel (buy-and-**lock**, not burn).
Tellingly, **customer payments are booked to an off-chain USD ledger and miners are settled entirely
separately in native emission** — the dollar size of pay is set by the dTAO market, not by usage.
*Sharpest contrast:* Chutes is the closest thing to "revenue-coupled," yet the coupling is **indirect
(revenue→price)**; we couple **directly (deposit→emission weight)**.

**SN4 — Targon (Manifold Labs) · confidential compute/inference.** Pivoted from a 2024 deterministic logprob
verifier to **cryptographic hardware attestation** (Intel TDX / AMD SEV-SNP + NVIDIA Confidential Computing,
re-attested ~every 72 min); rewards via a **demand-tiered compute auction**. *Sharpest contrast:* Targon
proves *what hardware/model runs* cryptographically; we try to prove *that traffic was actually relayed*
cryptographically — same rigor ethos, applied to bandwidth instead of GPUs.

**SN8 — Taoshi / Vanta · finance.** Miners submit trading signals scored on **deterministically-replayable,
high-water-marked USD PnL** (currently 100% PnL weight); validators **burn** the gap between emission and real
performance (P20). Its **"Entity-miner"** model maps thousands of subaccounts onto ≤10 UIDs, and a **TIP-P22
EVM contract** locks Theta as collateral. *Sharpest contrast:* even this finance subnet pays on **simulated**
performance with **no real capital**, and *burns* overpayment rather than coupling to revenue — the inverse of
our deposit-coupling.

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
and an **EVM collateral contract** (miners lock slashable TAO). *Sharpest contrast:* its EVM contract holds
**validator-scoped security collateral**; ours holds **operator demand deposits + emission** and *settles*
payouts.

**SN12 — ComputeHorde · GPU for other subnets.** One miner UID spawns many **executors** (explicit
256-cap workaround) with **collateral/slashing**. *Sharpest contrast:* the **canonical "one UID, many off-chain
workers" precedent** we build on — but it settles with workers/validators off-chain and uses the contract for
**collateral**, where we settle **trustlessly on-chain by Merkle claim**.

**SN65 — TPN / Tao Private Network · VPN/bandwidth DePIN.** **Our closest analog:** decentralized WireGuard
**exit nodes**, a **mining-pool topology** where one UID aggregates many off-chain VPN workers, verified by
**geo-IP + latency/connectivity heuristics**. *Sharpest contrast:* same shape and domain, but TPN verifies
liveness **heuristically** and pays workers **off-chain**; we aim for **cryptographic proof-of-transit** and
**trustless on-chain** provider payout, anchored by **operator demand deposits**.

**SN56 — Gradients (Rayon Labs) · training/AutoML.** Tournament-style training with a **burned TAO entry-fee**.
*Sharpest contrast:* that entry-fee is the field's closest cousin to a **costly demand-deposit** — but it is a
**gate/sink**, not a per-operator weight on emission; we turn the costly deposit into the **emission-weighting
signal** itself.

**SN26 — Storb (storage, referenced).** S3-like object storage verified by **Proof-of-Data-Possession + erasure
coding** — an example of cryptographic verification where the work is intrinsically checkable, the rigor bar we
target for routing.

---

## 6. Where we diverge — and the bet behind it

Each divergence is intentional. Stated as *bet → risk accepted*.

1. **Deposit-weighted emission (the headline).** *Bet:* a costly, revenue-backed deposit is a harder-to-fake
   demand signal than measured output alone, and auctioning the emission subsidy to real usage makes the
   cross-operator split track genuine demand — something no Bittensor subnet does on-chain today (Chutes only
   approximates it via buyback). *Risk:* "pay-to-play" optics and wash/self-deposits — mitigated by the
   non-refundable `φ` floor (a hard cost that never round-trips), quality consensus by **independent**
   validators, and the self-weight mask; and the assumption that `deposit ≤ revenue` long-run.

2. **EVM-contract custody + on-chain Merkle pool payout.** *Bet:* it is the only trust-minimized way to pay
   100k off-chain providers, enforce no-custody, and run clean 7-day settlement — using the Subtensor EVM
   rather than fighting the coinbase. *Risk:* a custody-critical contract (audit + timelock/guardian
   governance), and more moving parts than "native emission to a hotkey."

3. **Trustless worker payout (no-custody).** *Bet:* providers shouldn't have to trust an operator to pay them;
   direct on-chain claims against a committed root, un-clawback-able once finalized, are strictly more
   trust-minimizing than the discretionary off-chain norm. *Risk:* intra-pool quality is not *directly*
   Yuma-scored (we lean on auditable payout lists + reputation); more on-chain surface.

4. **Explicit validator effort bounty.** *Bet:* native dividends are effort-agnostic, but our **product is the
   failure-data**, so effort must be paid for directly and steered (coverage-weighting) to under-sampled
   providers. *Risk:* the bounty is bounded by the fee pool; if too thin, validators could coast — hence the
   `(Y)` escalation (route validator emission through the effort split) held in reserve.

5. **Cryptographic verification of a real-world service.** *Bet:* verifiable proofs beat the heuristic
   (geo-IP/latency) verification used by bandwidth/VPN peers, and match the field's verifiable-compute trend.
   *Risk:* v1 proves **liveness, not honest-relay**; closing that gap (Sybil resistance, proof-of-routing,
   destination diversity) is an explicit roadmap, and rewards stay provisional until then.

6. **Off-chain reference rate instead of an on-chain oracle.** *Bet:* usage is self-reported and unverifiable
   on-chain, so an on-chain oracle would have no enforcement power; the **costly deposit**, not the published
   rate, is the real signal. *Risk:* the rate is a governance-published off-chain input — but its abuse is
   bounded because deposits cost real α regardless of the rate.

**Net read.** We are conservative exactly where the Bittensor community has strong, battle-tested consensus
(coinbase, Yuma, anti-gaming, α economics, oracle-avoidance) and we concentrate our divergence on the two
places our first principles demand it: **making emission answer to real demand** and **paying a 100k-provider
real-world network trustlessly**. The field's own trajectory — toward verifiable proofs, toward DePIN, toward
EVM economic primitives (collateral today) and pooled off-chain fleets — is moving *in our direction*; we are
ahead of it on demand-coupling and on-chain settlement, and we accept the corresponding complexity and
verification-hardness as the price of those bets.

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

**Revenue / demand coupling:**
- Chutes revenue→buyback (secondary/blog) — https://www.coingecko.com/learn/top-bittensor-subnets-dtao · https://ownyourmind.ai/tokenomics/bittensor-subnets-where-the-revenue-is/ · https://pineanalytics.substack.com/p/the-bear-case-for-bittensor-tao (disputes self-reported ARR)

**Per-subnet (primary repos/docs):**
- SN1 Apex — https://docs.macrocosmos.ai/subnets/subnet-1-apex · https://github.com/macrocosm-os/apex
- SN13 Data Universe — https://github.com/macrocosm-os/data-universe · https://docs.macrocosmos.ai/subnets/subnet-13-data-universe
- SN9 IOTA — https://arxiv.org/abs/2507.17766 · https://github.com/macrocosm-os/iota · https://docs.macrocosmos.ai/subnets/subnet-9-iota
- SN8 Taoshi/Vanta — https://github.com/taoshidev/proprietary-trading-network · https://docs.taoshi.io/
- SN4 Targon — https://github.com/manifold-inc/targon · https://manifold.inc/releases/targon-v6 · https://simplytao.ai/blog/targon-sn4-and-intel-tdx-confidential-compute-on-bittensor
- SN64 Chutes — https://chutes.ai/docs/core-concepts/security-architecture · https://oakresearch.io/en/analyses/innovations/rayon-labs-subnet-leader-bittensor-tao
- SN51 Lium/Celium — https://github.com/Datura-ai/compute-subnet · https://docs.lium.io
- SN12 ComputeHorde — https://github.com/backend-developers-ltd/ComputeHorde
- SN65 TPN — https://github.com/taofu-labs/tpn-subnet
- SN56 Gradients — https://github.com/rayonlabs/G.O.D (miner docs)
- SN26 Storb — https://github.com/storb-tech/storb

> **Reproduce / extend:** the verdicts above feed `diagrams/comparison_matrix.py`. Open items to re-confirm
> before relying on standings: live mid-2026 emission/market-cap ranks (volatile, JS-gated); per-subnet
> commit-reveal/Liquid-Alpha enablement; whether any subnet has shipped an effort-style validator bounty since
> this was written.

---

## 8. Analysis — do our divergences make sense, or should we follow the leader?

> A strategic assessment — **judgment, not verified research** — synthesizing the findings above against our
> first principles. Opinionated by request; the trade-offs it names are the load-bearing ones.

**Verdict.** Our divergences make sense — they are concentrated exactly where our *situation* genuinely
differs from the field's, not scattered out of contrarianism — and we should **not** be more conservative
wholesale. Doing so would discard our one structural advantage. But the three divergences carry very different
risk, and "be conservative" is the right instinct in exactly one spot.

### 8.1 What "following the leader" would actually mean

The leaders share a pathology we would be *adopting*, not escaping: emission is **decoupled from real demand**
(~20–40× emission-to-revenue subsidy, even at Chutes); rewards are **stake-dominated, not quality-driven**
(stake↔reward correlation ~0.80–0.95 vs performance ~0.50, arXiv 2507.02951); scoring is **frequently gamed**
(SN1's envelope exploit, SN13's 15 anti-exploit resets, LLM-judge attacks); and control planes are
**operationally centralized**. Those designs are excellent *for their objective* — "emission is the product,
token price is the scoreboard." Our objective differs: we are an incentive layer for a **real business with
real revenue and 100k real providers**. Copying a demand-decoupled design to look normal would throw away the
rarest asset on Bittensor — actual paying demand. That is conservatism in the wrong place.

The operative rule: **conserve the plumbing, innovate the economics** — which is precisely what the matrix
shows we do (10 of 14 decisions are straight best-practice).

### 8.2 Where we are (rightly) conservative — keep it

We do not fork Yuma, change the 18/41/41 split, invent a consensus, or replace the anti-gaming stack. All
novelty is spent on economics, none on the battle-tested safety machinery. This is the correct risk-budget
allocation; leave it alone.

### 8.3 The three divergences, ranked by risk

**1. Deposit-weighted emission — highest conviction *and* highest risk. Keep it; red-team it hardest.**
The right bet *because* we have real revenue almost no one else does, and a costly, revenue-backed deposit is
Sybil-resistant where measured-output scoring is cheap to fake. The sharpest critique to respect: the field is
already criticized for capital-beating-merit, and `deposit × quality` leans into capital-weighting. Our
defense is real — our capital is **productive and revenue-bounded** (non-refundable `φ`, sized to usage), not
speculative stake — but it holds only if **(a)** `φ` is high enough and independent validators numerous/honest
enough to make wash-deposits unprofitable, and **(b)** quality `Q_n` genuinely bites at maturity rather than
collapsing to "biggest depositor wins." Both are tunable, and our bootstrap ramp (cap quality early, widen as
validators mature) is the right *conservative-within-the-novel* move. Action item: **instrument the
deposit:quality balance explicitly**, and treat self-dealing as something to empirically disprove, not argue
away on paper.

**2. EVM-contract custody + Merkle payout — mostly *entailed*, not a standalone gamble.**
Not to be judged in isolation: once we commit to deposits (#1) and face the 100k-provider / 256-UID reality,
we need a contract to custody deposits and own the pool UIDs *anyway*, and pooling forces *someone* to split
rewards. The only genuinely optional piece is **trustless on-chain Merkle payout vs. the field norm of
operators paying workers off-chain at discretion** (TPN, ComputeHorde). That reduces to one product question:
**is no-custody / trustless provider payout a v1 must-have?** For a decentralized privacy network where
providers shouldn't have to trust operators, it plausibly is — but if it is a nice-to-have, the conservative
path is to start TPN-style (off-chain payout) and add the trustless claim later. This is the one place a "be
more conservative for v1" decision is legitimately on the table.

**3. Validator effort bounty — additive, low blast-radius. Keep and tune.**
It does not touch Yuma; if it underperforms we adjust `φ`/`ω` or escalate to (Y). It targets a real gap the
field does not have: our measurement (walking provider chains) is expensive and **coverage** matters, so
dividends-only would let validators coast on a thin sample. Safest of the three — no reason to drop it.

### 8.4 The risks that actually decide this (not mechanism soundness)

Two execution risks dwarf the design ones: **(1) dTAO emission tracks alpha price = market perception** — a
mechanism the market can't easily value can mean lower price → lower emission → less provider subsidy; our
real-revenue story is a *better* narrative, but only if we sell it. **(2) Validator recruitment** —
independent validators must run our bespoke `VERIFIER.md` protocol, a heavier lift than generic validating;
the effort bounty helps, but validator go-to-market is where bespoke designs usually struggle.

### 8.5 Bottom line

Stay conservative on the consensus plumbing (we are), stay aggressive on demand-coupling (it is our edge), and
concentrate validation on the two genuine unknowns — the **deposit-vs-quality balance / self-dealing defense**,
and **whether no-custody payout is a v1 requirement or a v2 hardening**. Divergence here is a considered bet
with a named trade-off, not a deficiency — and on our first principles, it is the right one.
