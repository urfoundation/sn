# UR Subnet — Discussion Notes & Decision Log

Companion to `WHITEPAPER.md`. The whitepaper is the *what*; this file is the *why* — the decisions, the
alternatives we rejected (and reverted), the axes we circled and settled, and what's still open. Read
this before proposing a change so we don't re-litigate something already decided.

Last updated: 2026-07-04. Inputs: `seed/INCENTIVES.md`, `VALIDATOR.md`, `README.md`, and a deep research pass
on current (post‑dTAO) Bittensor (extended 2026-07-04 with primary-source profiles of Hippius SN75 and
BlockMachine SN19 — see `COMPARISON.md`).

---

## 0. Current design in one paragraph

Each **Network Operator (NO)** is **one contract‑owned miner‑pool UID** (its 100k+ providers live
*inside* the pool, paid by Merkle claim — they can't be UIDs). **Independent validators** stake α, run
`VALIDATOR.md` trails, and set Yuma weights **`deposit × Q_n`** (deposit = on‑chain demand anchor; `Q_n` =
measured pool quality) — so **validators' evaluation drives the miner emission, the Bittensor way**.
Miner emission (41%) accrues to the contract (custody) → providers claim via Merkle. Validator emission
(41%) is **native** ∝ stake × vtrust — **v1's only validator reward, full stop**; a validator-effort
incentive is **out of v1 scope**, a post-launch learning, not a committed deferred feature (D29). Everything
is denominated in the subnet's **α**. The **ST contract** (Subtensor EVM) is the ledger, miner‑emission
custodian, buyback‑reserve custodian, and settlement engine — **not** the validator.

**v0.2 — two tiers (D16–D20).** The miner side now also runs a **head** channel beside the pools: the **top
~200 providers hold their own UIDs** and are steered **directly** on pure quality `Q_p` (no deposit), paid
**natively**, matched by a signed `client_id ⇄ hotkey` binding; the pool is the **on-ramp** they graduate
from. Both tiers share one mechanism's 256 UIDs, split by a governed head share **θ**. See `WHITEPAPER.md`
§8.4–8.5, §10, §11.4, §14.

**v0.3 — deposits are buybacks (D23).** Deposits keep the steering role but are **never distributed**:
the contract stakes **every deposit in full** into a locked, dividend‑compounding **buyback reserve** on
the **owner‑validator hotkey** (one‑way invariant; no exit path in code). `poolTotal = emission‑only` —
both miner tiers are paid from emission; revenue supports miners through the token (buy‑and‑lock, the
Chutes flywheel) instead of pass‑through payouts. The **effort bounty and its whole contract surface
leave v1** (owner = majority validator early, with intrinsic motive to measure). *(v0.5/D29 goes further:
the bounty is no longer a **committed** deferred phase — it is removed from the roadmap and reframed as a
post-launch open question; v1 is native-dividends-only.)* See `WHITEPAPER.md` §6.3–6.4, §7.4, §8.3, §9, §12.4.

**v0.4 — conviction staking, validator-computed weights, IP-breadth head, testnet-first (D25–D28).**
A simplification pass that moves the mechanism's judgment OUT of the contract and INTO the validators:
- **The contract stops weighting/validating deposits (D25).** It drops the `DT`/`totalDT` ledger and
  becomes custody + settlement only. Deposits are **conviction stake** — locked α in the reserve (the
  D23 buyback), whose cumulative amount sets the NO's **tier**, which sets its published
  **deposit-rate** (zero conviction = today's baseline; more → a lower rate — the onboarding lever).
  **Validators weight the pools themselves**: `weight_n ∝ (epoch_deposit_n / rate(tier_n)) × Q_n` =
  implied-usage × quality (staking is a discount, not a penalty; weight still tracks real usage).
- **Validators set their own measurement rate (D26).** The §5.3 eligibility throttle + soft seed
  limits go off/configurable (UR is the largest validator); only a loose hard per-IP DoS backstop stays.
- **Head tier ranks by routable-IP breadth (D27).** Head = top-200 NOs by **split-adjusted distinct
  routable egress-IP count** (shared IPs split equally among claimants); head weight ∝ that score;
  trails carry a per-hop egress-IP-hash so validators verify it from their own paths; IP-hash
  granularity is a subnet-configurable param (default /29 v4, /48 v6).
- **Testnet-first (D28).** Reverts D24 — v1 shakes out on testnet, then mainnet.
See `WHITEPAPER.md` §7, §8.1/§8.4/§8.5, §10, §12; `VALIDATOR.md` §5/§7/§8.

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
(~50 400 blocks) is the application‑layer settlement period with +4h/+24h/+48h windows. §5. *(v0.3/D23:
the +24h effort‑claim window is deferred with the bounty — v1 runs +4h commit / +48h finalize; the
trails‑window dial stays reserved for the bounty phase.)*

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

*(v0.3 update — D23: v1 ships **(W) dividends‑only** — the owner is the majority validator with an
intrinsic motive to measure, so the explicit effort reward has no customer yet. (X) is unchanged as the
first escalation, triggered when owner‑independent coverage is wanted; (Y) stays the final escalation.)*

**D9 — No on‑chain oracle.** Per‑GB/per‑user usage is self‑reported and unverifiable on‑chain, so an
on‑chain rate has no teeth — the only quantity the protocol acts on is *α deposited*. The "global fixed
rate" survives as an **off‑chain published reference**. §7.1, §13.5.

**D10 — `φ`, the non‑refundable deposit fraction.** A hard cost floor against deposit self‑dealing
(round‑tripping a deposit through your own providers), and it **funds the effort bounty**. §7.2, §9.3.
*(Superseded by D23: deposits are now **fully sunk buybacks** — the round‑trip is closed structurally
(recovery = 0, not `1−φ`), so `φ`'s anti‑gaming job is subsumed and it is retired from v1; it returns
only as the bounty‑phase funding split carved from the buyback flow.)*

**D11 — Governance: Phase 0 → Phase 1 (committed); deeper deferred.** Phase 0: owner M‑of‑N multisig +
upgradeable proxy (fast bug‑fixes, central control). Phase 1: **timelock ≥ 1 epoch** on
upgrades/params + a **pause‑only guardian**. Invariant from day one: **finalized claims are
sacrosanct** (no upgrade/pause can block or claw back an earned claim). §6.4.

**D12 — Optimistic effort verification (scales).** Verifying *every* trail on‑chain via `0x402` won't
scale. `submitTrails` commits a **Merkle root** of `(trail, coverage)` leaves + a claimed total; the
contract **spot‑checks a random sample** and **anyone may dispute any leaf** in the window (one bad sig
voids the claim + forfeits stake). O(1) on‑chain. §9.3, §11.3. *(Deferred with the bounty — D23. The
mechanism as specified — including the built‑and‑hardened implementation (coverage‑bound signatures,
sample estimator credit, HF‑2 reseed caps) — is the parked (X)‑phase implementation, preserved under
`docs/parked/`.)*

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
on‑chain `poolTotal_n = emission_n + (1−φ)D_n` *(v0.3/D23: now `poolTotal_n = emission_n` only —
deposits are reserved; the no‑off‑chain‑compute property is unchanged)* (capped so a pool can't be
over‑drained). The validator bounty computation (`feePool·effort/Σeffort`, on‑chain, no root) is
deferred with the bounty. So **nothing is computed off‑chain at
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

**D22 — v1 implementation decisions settled (PLAN.md, 2026-07-01).** The cross-repo implementation
decisions are recorded in `PLAN.md` §9 (namespace `D-1..D-13`, distinct from this log). Two touch the
design layer: **`Q_n` v1 aggregation = usage-weighted mean** of per-provider `q_p`, EMA-smoothed (resolves
the §3 open item *for v1*; the multi-NO-grade spec stays open), and the **epoch windows become
governance-settable contract parameters** (commit window default +4h per §5.2; a missed commit rolls the
pool total into the next epoch). Also notable: commit-reveal is implemented **Go-native (drand tlock) from
day one** (no CR-off interim, no Python sidecar); validator coldkeys are EVM-mirror accounts with real
sr25519 hotkeys (permissionless `registerValidator` via a metagraph coldkey check); trail eligibility
includes **residential** egress IPs behind the `VALIDATOR.md` §8.2 bijection from day one; provider claim
wallets are native ss58 coldkeys.

**D23 — Deposits are buybacks (staked, locked, compounding); miner pay = emission-only; effort bounty
deferred out of v1 (user decision, 2026-07-03; WHITEPAPER v0.3).** Three linked changes:
1. **Every deposit is a buyback** (the Chutes buy-and-lock pattern, `COMPARISON.md` SN64) — the contract
   moves the **full deposit** into a **buyback reserve**: α staked to the **owner's validator hotkey**
   (`reserveHotkey`), with **no code path out** (a §6.4 sibling invariant to "finalized claims are
   sacrosanct"). dTAO stake has **no unbonding**, so the lock is the contract's missing exit path +
   upgrade governance — not the staking itself. Deposits keep their steering role (`D_n × Q_n`)
   unchanged; **`poolTotal = emission_n` only** — the `(1−φ)·D` pass-through to pool miners is gone, and
   both tiers are paid from emission alone. Custody splits across two hotkeys (`treasuryHotkey` = exact
   push-then-credit claims escrow; `reserveHotkey` = compounding reserve) because dividend accrual on the
   escrow hotkey would corrupt the deposit attribution check. Reserve-hotkey delegate take = 0.
2. **Why staked to the owner-validator** (user choice (b) over inert/spread/burn): the reserve earns its
   pro-rata share of the 41% validator emission (auto-restaked → compounds instead of melting), recycles
   validator emission out of liquid supply, and **deliberately hardens the owner's majority-validator
   posture** early. Long-run thesis (§12.4): buyback `B_e` compounds with revenue while issuance `E_e`
   halves — the demand ratio `R_e = (B_e + Y_e)/L_e` is expected to cross 1, after which float shrinks
   structurally. Caveats recorded in §12.4: miner pay becomes price-mediated (the deposit-funded floor is
   gone); the reserve is a growing honeypot (governance phases matter more); the lock is
   governance-credible, not physical; consensus concentration is intended and makes later validator
   decentralization a deliberate, budgeted step.
3. **Effort bounty (X) deferred out of v1** — with it go `φ`/`ω`/`feePool`, `registerValidator`/the vpk
   registry, `submitTrails` + sampled verification + effort disputes, and `claimValidator`. Rationale:
   native dividends suffice while the **owner is the majority validator** (α holdings + the reserve) with
   an intrinsic motive to run trails; the bounty pays for verification the owner does NOT control, so it
   ships with the independent-validator phase (trigger: §12.3's quality-swing ramp needs an
   owner-independent baseline). §13.6 ladder is now **W (dividends-only, v1) → X (fee-funded bounty) → Y
   (emission-routed)**. This **defers, does not re-open**, the §2 (X)-design closures (intersection
   split/VT/bond eliminations stand; §9.3's bounty spec is unchanged, waiting). Self-dealing: the buyback
   **closes the deposit round-trip structurally** (recovery = 0, vs `1−φ` before), so `φ`'s anti-gaming
   role is subsumed (§12.1 rewritten).
   *Implementation note:* the already-built effort machinery (coverage-bound digests, sampled proofs,
   HF-2 reseed caps, `snclaim` submit) is **parked as the (X)-phase implementation**, not discarded.

**D24 — Launch directly on mainnet; no public-testnet phase (user decision, 2026-07-03).**
*(REVERTED by D28, 2026-07-03 same-day: v1 goes back to a testnet-first launch. The rationale below
is retained for the record; the SP-1/SP-2/SP-3 harness work it drove all stays — it simply runs
against testnet first, then mainnet, instead of mainnet-direct. Everything below is superseded except
the harness artifacts, which are chain-agnostic.)* v1 deploys
straight to finney. Rationale: the code is chain-agnostic (endpoints/windows/caps are config; the 964
profile already exists), the v1 posture is safe-by-construction (owner-run everything, the D-3 deposit
cap, guardian pause, UUPS, governance-settable epoch windows per D-11/F2), and public testnet was a
low-fidelity proxy anyway — test.finney frequently runs a DIFFERENT runtime than finney, so "SP-1
verified on testnet" could still break at launch; verifying against the real runtime is stronger.
Replacements for what testnet was load-bearing for:
- **SP-1** → **mainnet dust probes** before the subnet exists: a throwaway probe contract on chain 964
  against an existing netuid (custody semantics, rao units, 0x402 gas, blake2f, and the §7.4 reserve
  leg — dividend auto-compounding + take). Harness **BUILT + CI-green**:
  `evm/src/probe/STSubnetProbe.sol` + `script/SP1Conformance.s.sol` + `test/SP1Probe.t.sol` (the
  subtensor precompiles are runtime-only → the battery runs on-node via `cast` against the deployed
  probe; forge sim can't execute them). `docs/LAUNCH.md` B1 has the exact command sequence.
- **SP-2** → `sp2 check-metadata` re-pinned read-only against finney (format already live-verified vs
  test.finney). The first real commit on our subnet lands at genesis — costs a tempo, never funds.
- **e2e rehearsal** → **SP-3 localnet promoted to required** (docker subtensor pinned to the live
  finney runtime tag, fast blocks): full genesis dry-run + failure drills; localnet drand may be
  stubbed (noted — the reveal is the one genesis-only step).
- **soak** → **mainnet ramp**: launch with short epochs (e.g. tEpoch 7 200) + a dust deposit cap,
  N clean cycles, then `setEpochParams(50_400, 1_200, 7_200, 14_400)` and raise the cap stepwise.
Mainnet-only additions: genesis is ONE scripted window (subnet registration locks real TAO and starts
the start_call/emission clock — do not register until rehearsal + probes are green); defensive
registration hyperparams + OWN UIDs registered first (new-subnet UID snipers are a real meta); the
owner-validator α position is acquired in the first hours after start (α is cheapest at genesis — the
purchase IS the first buyback and secures the §9.2 majority seat, which §7.4 then compounds);
reserve-hotkey delegate take set to 0 before the first deposit (rate-limited change); the owner
multisig + guardian exist at deploy, not later. Accepted residuals, stated once: a custody bug now
costs real funds (bounded by the per-epoch cap + the one-way reserve); the first CRv4 commit and first
`finalizeEpoch` on real infra are unrehearsed-on-mainnet moments; hyperparameter mistakes are
rate-limited/expensive to unwind. Docs: WHITEPAPER §16.3 milestones reframed (M0 = rehearsal + probes,
M1 = scripted genesis in rehearsal mode, M2 = reserve verified live, M3 = ramp), §16.4 gates now run
against finney + localnet; `docs/TESTNET.md` → **`docs/LAUNCH.md`**; PLAN.md amended. Zero code
changes.

**D25 — Conviction staking: the contract stops weighting/validating deposits; validators weight pools
from published data; tiered deposit rates (user decision, 2026-07-03; v0.4).** Bundles simplification
changes #1 + #4. Three linked moves:
1. **The contract does NO deposit weighting or validation.** It drops the per-NO deposit ledger
   (`DT[e][noId]`, `totalDT`) entirely. `deposit(noId, amount)` still stakes the **full amount into the
   locked reserve** (D23 buyback — kept, chosen 1(a)) and emits `Deposited(e, noId, from, amount)` +
   `BuybackReserved`; that event log IS the authoritative, published per-NO deposit record. The
   contract's miner-channel role shrinks to **custody + settlement only** — it never computes a weight.
2. **Deposits are CONVICTION STAKE.** A deposit is locked α in the reserve, never distributed, never
   returned. An NO's **total conviction** = its cumulative locked α (deposits + any voluntary
   up-front stake — **one pool**; a new NO can pre-stake to jump tiers or accumulate conviction by
   depositing over time). Conviction = the **locked amount** (the lock is the alignment — NOT a
   time-integral `amount × time`; kept simple, reuses the reserve already built).
3. **Tiered deposit rates (change #4).** Governance publishes a **deposit-rate schedule per conviction
   tier**: `rate(tier)` = α required per unit of real usage. **Zero conviction = the zero tier = today's
   baseline (full) rate**; more conviction → a lower rate → less α needed up front (the onboarding +
   long-term-alignment lever). The schedule is an off-chain published reference (like the §7.1 rate),
   read by validators; the contract does not consume it.
4. **Validators weight the pools themselves (change #1).** Each validator reads (a) each NO's on-chain
   deposits (this epoch) + cumulative conviction (from the event log), (b) the published tier→rate
   schedule, and (c) its OWN measured quality `Q_n`, and sets Yuma weights as
   **`weight_n ∝ implied_usage_n × Q_n`** where **`implied_usage_n = epoch_deposit_n / rate(tier_n)`**
   (decision A). This is the key economic fix: a staker on a lower rate posts LESS α for the SAME
   implied usage and gets the SAME weight, so the stake is a genuine discount, not a penalty, and the
   pool weight still tracks **real revenue-backed usage** (the headline thesis) rather than raw α.
   *Requires* the rate schedule floored above zero (else implied usage → ∞ for any deposit).
   `BuildWeightVector`'s pool term changes from `deposit × quality` to `implied_usage × quality`; the
   validator loads the rate schedule from config. Self-dealing stays bounded: the deposit is a full
   sink (D23) AND the conviction that lowers the rate is itself sunk α — buying a low rate costs locked
   capital. Supersedes the D6 `deposit × Q_n` formula (the *inputs* move off-contract; the deposit↔demand
   coupling and validator-consensus core are unchanged). Rewrites WHITEPAPER §7, §8.1, §10, §12.1,
   §15.2; drops `DT`/`totalDT` from §6.1 + the contract; `EpochFinalized` loses `totalDT`.

**D26 — Validators set their own measurement rate; guardrails off (user decision, 2026-07-03; v0.4;
change #2).** We want to experiment with the measurement throttles OFF, since UR is the largest
validator by far. The `VALIDATOR.md` §5.3 **eligibility token bucket** (one measurement per provider
per `EligibilityInterval`) and the §9 **soft seed limits** become permissive / **validator-configurable
with an off default** — each validator drives its own trail/testing rate as fast as the network allows
(server-push sampling stays, so the equal-probability baseline is intact). We KEEP only a **loose hard
per-source-IP DoS backstop** (the state-creation bound — `/verify` still faces non-validator callers;
the nginx-forced `$remote_addr` is the real limit, V11). Accepted trade-off, documented: §5.3's throttle
also bounded how often a self-dealer harvests its own node's measurements (§7.7 per-hop self-dealing);
running it off re-opens that cadence — fine while validators are owner-run, flagged for the
independent-validator phase. Rewrites `VALIDATOR.md` §5.3/§5.5/§9.

**D27 — Head tier ranks NOs by split-adjusted unique routable egress-IP count; head weight ∝ that score
(user decision, 2026-07-03; v0.4; change #3).** The head-tier ranking metric changes from measured
quality to **breadth of routable exit IPs** — the real VPN supply metric ("not how much traffic is
routed, but how many unique IPs are routable"). Because §8.2 enforces **one provider ⇄ one egress IP**,
the ranked unit is the **network operator (fleet)**, not a single provider: an NO's score = the count of
**distinct routable egress-IP-hashes** across its providers. **Shared IPs are split:** each distinct
IP-hash contributes **1.0 total, divided equally among every top miner claiming it** (A and B both route
IP Q → 0.5 each; score_n = Σ over n's IP-hashes of `1 / (#top-miners claiming that hash)`). To let each
validator compute the counts and splits **from its own paths**, the `VALIDATOR.md` trail/proof wire
gains a **per-hop egress-IP-hash** (a hash, not the raw IP — privacy preserved). The head **emission
weight ∝ the split-adjusted IP score itself** (decision B — more routable breadth → more emission; the
score is both the top-200 gate AND the weight), replacing pure `Q_p`. Validators **verify** a claimed
top-200 miner actually ranks by their own trail-observed IP score (self-endorsed, trust-minimized).
**IP-hash granularity is a configurable subnet parameter**, default **/29 for IPv4, /48 for IPv6** (what
UR uses today), so the "distinct IP" unit is tunable. Rewrites `WHITEPAPER.md` §8.4/§8.5/§11.4 (head
identity binds a fleet, not one client_id; weight = IP score) + `VALIDATOR.md` §7 (the IP-score
measurement) + §8 (the wire's egress-IP-hash) + the head steering in `sn/validator/steer.go`.

**D28 — Revert D24: v1 launches on TESTNET first, then mainnet (user decision, 2026-07-03; change #5).**
Undoes the mainnet-direct decision. `docs/LAUNCH.md` goes back to the testnet-bootstrap-then-mainnet
runbook (M0→M6 roadmap; mainnet stays the eventual target, gated behind a clean testnet run). The SP-1
probe harness (`evm/src/probe/STSubnetProbe.sol` + script + test) and the SP-2/SP-3 work are
endpoint-parameterized, so they **re-target to testnet with zero code change** (`SP1_NETUID` +
`--rpc-url testnet`, chain 945, `wss://test.finney`). WHITEPAPER §16.3 milestones revert to
testnet-first. Rationale for the reversal: with the v0.4 mechanism changes (D25–D27) reshaping the
economic + measurement core, a live testnet shakeout de-risks more than mainnet-direct speed saves.

**D29 — Validator effort bounty removed from scope entirely; it is a post-launch learning, not a committed
deferred feature (user decision, 2026-07-04; v0.5).** D23 *deferred* the effort bounty but kept it as a
**specified, committed** next phase with a named trigger and a **W → X → Y** escalation ladder. D29 goes
further: **the effort bounty is out of scope, period.** v1 pays validators **native Yuma dividends only**
(∝ stake × vtrust) — the plain Bittensor norm — and **whether to add any validator-effort incentive at all
(and if so, what shape) is deliberately left open, to be decided from what the live launch teaches us about
independent-validator coverage.** Rationale: the owner is the majority validator early with an intrinsic
motive to run trails (D23's logic stands), and pre-committing a specific bounty design — before the network
exists — over-specifies a future we should learn our way into. What this changes:
- **Positioning / comparison.** `COMPARISON.md` no longer lists the effort bounty as a design divergence.
  With v1 paying plain native dividends, "validator rewards" is now an **ALIGNED** row (12 aligned · 2
  divergent · 2 novel), not a divergence. All `φ`/`FeePool`/`(X)`/`(Y)`/coverage-bounty language is removed
  from that doc; the matrix diagram is regenerated.
- **Supersedes the forward-looking parts of D8, D10, D12, D23-pt3, and the §3 "(X) then (Y)" open item** —
  those entries stay as the historical record of *how* the bounty was designed, but they are **no longer a
  roadmap commitment**. The already-built (X)-phase machinery (coverage-bound digests, sampled proofs, HF-2
  reseed caps, `snclaim`) stays **parked** exactly as D23 noted — raw material for a *possible* future
  iteration, not a promised one.
- **NOT changed:** the eliminations those decisions also made — the per-NO validator pool, the NO↔V
  **intersection split**, **VT**, the verifier **bond**, and the take-0 custody hack — **stay eliminated**
  (they were rejected on their own merits, §2; D29 does not resurrect them). Native-dividends-only is the
  whole validator reward.
- **WHITEPAPER.md follow-up (flagged, not yet applied):** §9.3 still carries the full bounty spec and §13.6
  the W→X→Y ladder, written as a "deferred phase." To match D29 those should be demoted from "committed
  deferred design" to "a candidate a future iteration *might* explore" (or moved to a parked/appendix note).
  Left for a deliberate whitepaper pass so the formulas aren't lost — call it out before editing.

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
- **Collapsing the validator side to pure native dividends *as the permanent design* (REJECTED).** Removing
  the effort reward *forever* would gut the long‑run validator incentive and the failure data — the product
  (user). See D8. *(Nuance added by D29: this rejection was about making "no effort reward ever" the
  **permanent** design. v1 nonetheless **ships native‑dividends‑only** — legitimately, because the owner is
  the majority validator early and needs no subsidy — and D29 leaves the long‑run effort question **open**
  rather than answering it "never." So D29 is not a re‑opening of this rejection: it neither commits to a
  bounty nor forecloses one; it defers the call to post‑launch evidence.)*

**The circular axis, named:** *quality‑in‑weight vs deposit‑only* was circled twice (deposit‑only →
"use Yuma maximally" put quality in → deposit‑only simplification → reverted to quality‑in‑weight). The
user's instinct was right each time it mattered: **quality‑in‑weight is the spine. Locked. Stop
re‑opening the weight formula.**

---

## 3. Open questions / deferred to later revisions

- **Validator-effort incentive — fully open post-launch (was the "(X) then (Y)" ladder; D29).** v1 ships
  **dividends-only** and stays there. Whether any validator-effort reward is ever added — and if so, whether
  it looks anything like the parked fee-funded bounty (X) / emission-routed (Y) design — is **deliberately
  undecided**: we launch, watch whether independent-validator trail coverage is actually thin once the
  owner is no longer the whole validator set, and decide then. The old ladder framing (a committed
  W→X→Y) is retired; the (X)-phase code stays parked as raw material, not a plan. §13.6 (to be demoted to
  match — see D29).
- **`Q_n` aggregation + sampling spec** *(v1 RESOLVED → D22: usage-weighted mean of per-provider `q_p`,
  EMA-smoothed — `PLAN.md` D-9. The multi-NO-grade version below stays open.)* How per‑provider reliability
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
