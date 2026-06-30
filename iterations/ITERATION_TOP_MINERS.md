# UR Subnet — Iteration: Top-Level Miners (direct-steered head + pool tail)

**Status: proposal (not yet locked).** A miner-side iteration of `WHITEPAPER.md`. It *adds* a second
miner reward channel; it does **not** replace the per-NO pool. Companion to `WHITEPAPER_DISCUSS.md`
(if accepted, it becomes decisions **D16–D20** and edits `WHITEPAPER.md` §§3, 8, 10, 15).

Built from a four-pass deep-research sweep on how the best subnets steer rewards *directly* to miners
(native steering & top-N churn; off-chain-worker→hotkey identity binding; pooling vs. tiered designs;
weight-setting best practice). Findings are cited inline; values verified against `opentensor/subtensor`
`main` on **2026-06-30** are flagged, and every numeric default is governance-mutable — re-verify live.

---

## 0. The idea, in one paragraph

Keep today's design — **each Network Operator is one contract-owned miner-pool UID, `weight =
deposit × quality`, providers paid *inside* by Merkle claim** — exactly as-is, and run a **second channel
in parallel**: the **top ~200 providers each claim their own miner UID** ("**top-level miners**") and are
**steered emission directly by validators on pure measured quality** (`weight = Q_p`, no deposit term),
paid **natively** to their own hotkey. A provider is identified across the two worlds by a **signed
`client_id → hotkey` binding** that validators read to turn the `VERIFIER.md` per-provider stats into a
`set_weights` on the right UID. The **pool is the on-ramp** (low barrier, baseline reward, no UID, no
registration burn); the **top-level slot is the merit apex** (scarce, direct, trust-minimized). A provider
**starts in a pool, graduates to a direct UID, and falls back to the pool if it slips** — the chain's own
deregistration churn runs that tournament for free.

```
                          41% MINER EMISSION  (Yuma, one metagraph, one mechanism)
                                     │
              ┌──────────────────────┴───────────────────────┐
   governance split θ (head)                          1−θ (tail)            ← set in validator software,
              │   "merit apex"                          │  "on-ramp"          SN13-style; both go to REAL
              ▼                                          ▼                    recipients (no owner-burn)
   ┌───────────────────────────┐              ┌──────────────────────────────┐
   │ TOP-LEVEL MINER UIDs (~200)│              │ NO POOL UIDs  (one per NO)   │
   │ weight = Q_p (validators)  │              │ weight = deposit_n × Q_n     │
   │ NATIVE emission → hotkey   │              │ contract-custodied → Merkle  │
   │ (no take, not shared)      │              │ claim by the tail providers  │
   └───────────┬───────────────┘              └───────────────┬──────────────┘
               │ direct, trust-minimized                       │ baseline, NO-directed split
               ▼                                               ▼
      a top provider's own coldkey               the NO's 100k+ tail providers
               ▲                                               │
               └──────── promote out / fall back ──────────────┘   (one channel at a time, no double-pay)
```

---

## 1. Why both — the design rationale (user, 2026-06-30)

> *"people need a place to start, and they need some baseline reward for starting."*

The two channels are **complementary, not redundant**, because they sit at opposite ends of a provider's
lifecycle:

| | **NO pool (tail)** — the on-ramp | **Top-level miner (head)** — the apex |
|---|---|---|
| Barrier to entry | join a NO; **no UID, no registration burn** | claim a scarce UID; **burn + win quality consensus** |
| Reward basis | `deposit × quality` (NO-mediated, demand-coupled) | `Q_p` (pure validator-measured merit) |
| Who splits it | the NO (auditable Merkle payout list) | nobody — Yuma pays the hotkey **directly** |
| Custody | contract custodies, provider claims | **native** to the provider's own coldkey |
| Trust | provider trusts the NO's split (can exit) | **trust-minimized** (no operator in the loop) |
| Population | the long tail (100k+) | the best ~200 |

The pool guarantees a **baseline** so a newcomer never earns zero while it builds reputation; the top slot
is the **upside** it climbs toward. The jump in pay at graduation is exactly the incentive to improve.
The demand-coupling bet (`deposit × quality`, **locked D6**) is preserved **in the pool tier**; the head is
deliberately demand-agnostic pure merit — see the θ trade-off in §8, the one genuinely new economic dial.

---

## 2. Where this sits vs. the field (deep-research synthesis)

**The tiered head/tail architecture is novel — no Bittensor precedent.** Across compute (SN12, SN27, SN51,
SN64), finance (SN8), storage and the core docs, **no subnet promotes individual top workers to their own
native-emission UID while pooling/off-chaining the tail.** The ecosystem norm is the *opposite* —
**consolidate** everything behind one UID (Chutes docs: *"Never register more than one UID… just add
capacity to one miner"*; ComputeHorde fronts many executors behind one UID; TPN/Vanta pool many workers
behind one UID). [github.com/rayonlabs/chutes-api; backend-developers-ltd/ComputeHorde; taofu-labs/tpn-subnet]

**And in *every* pooling precedent the tail is paid off-chain at operator discretion** — TPN verbatim:
*"the mining pools get to decide how they pay their workers."* So UR's trustless **on-chain Merkle pool**
is already a differentiator (`COMPARISON.md` §4.12); this iteration adds a **native direct** channel on top
— the part the field *does* do well, just never tiered above a pool.

**But the pieces are all well-precedented** — we assemble, we don't invent:
- *Native top-N selection* is free (UID-cap + burn-auction + lowest-emission deregistration already
  produce a rolling "top-N by emission" — §6). [subtensor `registration.rs`]
- *Direct steering* is the universal mechanism: `set_weights → Yuma → incentive → per-block α to the
  hotkey` (§4). [subtensor `run_coinbase.rs`]
- *Identity binding* reuses the field standard (§5): hotkey-signed binding + metagraph anchor (Epistula /
  ORO-AI bittensor-auth), with Celium's **dual-signed association** for client↔hotkey robustness.
- *On-chain emission reservation* between channels copies Data Universe SN13's weight-rewrite (§8).
- *Robust DePIN scoring* (Wilson intervals, latency percentiles) is exactly FileTAO's and TPN's practice —
  and exactly what `VERIFIER.md` §7 already computes (§4.2).

---

## 3. On-chain identity & the UID budget

### 3.1 Roles (delta from `WHITEPAPER.md` §3)

| Role | On-chain identity | Change |
|---|---|---|
| **Network Operator** | one contract-owned **NO-pool UID** | **unchanged** — still deposits, runs `/verify`, commits the payout root for its **tail** |
| **Tail provider** | a `client_id` *inside* a NO pool — **not a UID** | **unchanged** — Merkle-claims from the contract |
| **Top-level miner** | **its own miner UID** (`client_id` bound to its hotkey) | **new** — native emission to its own coldkey; not in any pool's payout list while promoted |
| **Validator** | independent validator UID | **unchanged** — but now scores *two* things (§4) |
| **ST contract** | coldkey owning the **NO-pool UIDs** only | **shrinks** — it does **not** own or custody top-level-miner UIDs |

A top-level miner is an **individual provider UID**, paid **natively**. (The phrase "top-level miner pool"
is used loosely — each is effectively a pool-of-one. Native is the right reading: research confirms
**child hotkeys cannot route miner emission** — *"Only the validation emission should be split amongst
parents"* — so a top miner that wanted real sub-workers would need the **same contract-Merkle machinery
as a NO pool**, which is redundant. If you ever want that, it's a NO pool by another name. Recommend:
individual UIDs.) [subtensor `set_children.rs`, `run_coinbase.rs`]

### 3.2 The UID budget — **single mechanism, hard 256 ceiling**

This is the load-bearing constraint, and it forces one decision up front. **`max_allowed_uids` is a hard
ceiling of 256** (owners may lower it, never raise it). Multi-mechanism subnets are bound by
`mechanism_count × max_UIDs < 256`, so splitting head and tail into **two mechanisms would halve the UID
space to ~127 each** — which **cannot hold 200 top miners.** Therefore:

> **Run one mechanism. All UIDs share the 256-slot metagraph:**
> `256 ≥ (top-level miners) + (NO-pool UIDs, one per NO) + (validator UIDs)`.

So "top 200" is a **target inside a shared budget**, not a guarantee. Reserve `V` validator slots and `P`
NO-pool slots; the head is `256 − V − P`. Example: `V=36`, `P=20` → **200** top-level miners. More NOs or
validators → fewer head slots. Validators are not a fixed partition — they are simply the UIDs that hold a
**validator permit** (top-k by stake; `max_allowed_validators` default **128**, root-only to change), so in
practice you stand up far fewer than 128 and the rest of the 256 is miners.
[verified: `runtime/src/lib.rs` — `MaxAllowedUids=256`, `MaxAllowedValidators=128`; multi-mech constraint]

---

## 4. Direct steering — how validators pay the head

### 4.1 The native mechanism (what "steer directly" means)

`set_weights` is the **only** native per-miner steering lever; everything else is encoded in the weight
value. A validator submits a `u16` weight vector over UIDs; **Yuma** takes the **stake-weighted median
(κ=0.5)**, **clips** each validator to consensus, and emits `incentive_j ∝ Σ_i stake_i · clipped_weight_ij`.
For a **miner** hotkey the incentive is credited as **α stake on its own hotkey under its own coldkey —
no take, not shared with nominators** (unlike validator dividends). That is the clean direct-to-provider
payout the head wants. [subtensor `run_coinbase.rs`, `consensus.md`; learnbittensor.org/learn/yuma-consensus]

**Consensus consequence (important):** because Yuma clips to the stake-weighted median, **a single
validator cannot push emission anywhere.** Both channel rules below are therefore a **validator-software
convention that a stake-majority of validators must run in common** — not a chain primitive. Publish the
scoring spec and the split θ; honest validators converge, divergent ones get clipped and lose vtrust.

### 4.2 The two-channel weight vector (each tempo, each validator)

```
# HEAD — top-level miners, pure measured quality
for each top-level-miner UID u:
    Q_p(u) = EMA_e( aggregate VERIFIER.md §7 stats over u's bound client_id(s) )   # §4.3
    head[u] = Q_p(u)
normalize head so Σ head = θ                          # θ = governance head share (§8)

# TAIL — NO pools, unchanged deposit × quality
for each NO-pool UID n:
    pool[n] = deposit_n × Q_n        # Q_n now aggregates the NO's TAIL providers only (promoted ones left)
    if v operates NO n: pool[n] = 0  # self-weight mask (unchanged)
normalize pool so Σ pool = 1 − θ

w = head ⊕ pool                                        # one vector over all miner UIDs
apply max_weight_limit (MUST set — chain default is NO cap), min_allowed_weights
commit / reveal w                                      # commit-reveal ON (subjective signal, anti-copy)
```

This **resolves the biggest open under-specification in `DISCUSS.md` §3 for the head**: there is **no
pool-quality aggregation `Q_n` problem** for top-level miners — **per-provider `Q_p` *is* the weight.**
The `Q_n` aggregation question now only matters for the tail (where it already lives).

### 4.3 Scoring is `VERIFIER.md` §7, EMA-smoothed — and it's already best-practice

Research confirms the strongest real-world DePIN scoring is exactly what `VERIFIER.md` §7 does: **FileTAO**
scores storage on **Wilson-score confidence intervals** with tiers; **TPN** (closest analog) uses **robust
per-run statistics** (median latency, capped ratios) and leans on the chain bond-EMA. So `Q_p` =
`VERIFIER.md` §7's per-provider Wilson liveness + latency percentiles, **EMA'd across epochs** (template
default α≈0.1) to stop emission/dereg thrashing. Best-practice for ~200 concurrent providers:
**proportional weighting (not winner-take-all** — that's for single-best-answer contests like Apex), a
**real `max_weight_limit`** (default is no cap, so one provider could dominate), and **regular sampling
coverage** of all ~200 UIDs so honest-but-idle providers don't stale-decay.
[github: filetao-storage-subnet, taofu-labs/tpn-subnet, bittensor-subnet-template; arXiv 2507.02951]

---

## 5. The new primitive — `client_id → hotkey` binding (your "expose wallet per `client_id`")

### 5.1 What's needed and why the field's standard fits

A validator measures providers by **`client_id`** (the `VERIFIER.md` proof's hops; the server derives each
hop from the **unspoofable source IP**, §8.1 — the verifier never asserts it). To steer the head it needs
`client_id → UID`. Research shows the **de facto standard**: sign the binding with the hotkey, carry the
ss58, verify the signature, then **confirm the ss58 is a live UID in the metagraph (fail-closed on a stale
snapshot)** — Epistula / ORO-AI `bittensor-auth`. Taoshi literally namespaces `synthetic_hotkey =
{hotkey}_{client_id}`. [epistula.sybil.com; github.com/ORO-AI/bittensor-auth; taoshidev PTN]

### 5.2 Use a **dual-signed association** (Celium's anti-theft pattern)

A *single* hotkey signature is not enough here: a miner could claim a `client_id` it doesn't operate and
**steal another provider's measured quality**. So bind with **two signatures**, exactly like Celium's
`associate_evm_key` (both keys sign the linkage) — the provider proves it controls **both** the `client_id`
**and** the hotkey:

```
msg            = "urnetwork/bind/v1" ‖ client_id(16) ‖ hotkey_ss58(32)
sig_client     = Ed25519.Sign(client_sk, msg)     # client_sk = the existing per-client key (vpk), VERIFIER.md §2
sig_hotkey     = sr25519.Sign(hotkey_sk, msg)     # proves UID ownership
```

- **Publish via the commitments pallet** (free, `Pays::No`, keyed by `(netuid, hotkey)`): the miner hotkey
  commits its `client_id`(s) — a small list, or a Merkle root if it runs several. Validators read it as a
  **free state query** and build `client_id → UID`. (`WHITEPAPER.md` §2.4 already relies on this pallet.)
- **Anchor in the ST contract for disputes**: the contract verifies `sig_client` on-chain via the **`0x402`
  Ed25519 precompile** and the hotkey via a metagraph read — so a contested or stolen binding is
  cryptographically adjudicable, reusing the exact dispute rail `WHITEPAPER.md` §11.3 already has.
- **Keep binding orthogonal to quality** (the field is unanimous: TEE/attestation proves work is *real*,
  never *whose* — Targon deliberately keeps the hotkey out of the TEE quote). Binding proves *ownership*;
  `VERIFIER.md` proves *quality*; they compose, never merge.

### 5.3 Privacy: binding is **opt-in self-deanonymization**

`VERIFIER.md` §9 works hard to stop `/verify` becoming an oracle that links IP↔`client_id`↔provider.
Publishing `client_id → hotkey` (→ egress IP, via §8.1) **does** deanonymize — so it is **voluntary and
only for providers claiming a public top-level slot.** The long tail stays `client_id`-pseudonymous inside
the pools. Claiming the public UID *is* the consent. (Optionally the NO `/verify` server, which already
co-signs trails and authoritatively knows `client_id ↔ egress-IP`, can add a third attesting signature —
strengthens the binding at the cost of NO-trust; not required.)

---

## 6. Native top-N maintenance — promotion, demotion, churn

There is **no native "promote/demote" or "top-N keeps the slot" primitive.** The only on-chain UID
reallocation is **deregistration of the lowest-*emission* neuron** (emission = incentive + dividends; tie →
oldest reg-block → lowest UID; owner/immune keys skipped) when a new registration hits a full subnet. That
*is* the top-N tournament — and it's driven entirely by **the weights validators set**. [subtensor
`registration.rs`, verified] So the lifecycle is an **off-chain-driven ladder on a native rail:**

- **Promotion** = a provider whose measured `Q_p` is high enough to out-earn the eviction floor calls
  `burned_register` to claim a UID; validators weight it on `Q_p`; the contract/NO **removes it from the
  NO's payout list** (promoted out — **no double-pay**). It now earns natively.
- **Demotion** = a top miner whose `Q_p` decays earns the lowest emission, is pruned on the next
  registration, and **falls back to earning via its NO's pool** (the baseline catches it).
- **The catch — quality-dip eviction (confirmed risk).** Pruning reads only *current* emission rank, with
  no memory; once a UID's **`immunity_period` (~4096 blocks ≈ 13.7h default)** expires, one bad stretch can
  evict a genuinely good provider. Mitigations, all owner-tunable: **(a)** set `immunity_period` high (give
  new top miners a full measurement ramp); **(b)** EMA-smooth `Q_p` (§4.3) so a single bad epoch doesn't
  crater it; **(c)** the apex naturally earns well above the floor — keep θ large enough that the *lowest*
  top miner still clears the *highest* pool UID, or the head will thrash against the tail. **(d)** Up to
  `ImmuneOwnerUidsLimit` (default 1, max 10) owner-immortal UIDs exist but are far too few to protect 200 —
  do not rely on them.
- **Sybil cost** rises with the **continuous burn auction** (×1.26 per registration, ~72-min half-life,
  bounded `min_burn`..`max_burn`); each fake top miner pays its own burn — **strictly more Sybil-resistant
  than one pool UID per NO**, and a 256-cap with ~200 real providers leaves little headroom.

---

## 7. What changes, what's eliminated, what's untouched

**Untouched (per the user's constraint):** the entire per-NO pool tier — deposits, `deposit × quality`
weighting, contract custody of pool emission, Merkle payout roots, per-provider `claimMiner`, the effort
bounty (`WHITEPAPER.md` §§7–9, 11). The validator **effort bounty** (§9.3) and **native dividends** are
unchanged; validators just compute one extra sub-vector.

**Eliminated/simplified — for the head only:** no contract custody, no Merkle claim, no NO-discretionary
split, **no `Q_n` aggregation** (per-provider `Q_p` *is* the weight). For a promoted provider the
"within-pool payout is NO-discretionary" caveat (`DISCUSS.md` §3) **disappears** — it's paid directly by
consensus.

**New:** the `client_id → hotkey` binding registry (§5); a head/tail split θ in validator software (§8);
top-level-miner registration/dereg lifecycle (§6); validator software computes the two-channel vector (§4).

**Contract blast-radius shrinks:** the ST contract no longer owns or custodies the head's emission — it
owns only the NO-pool UIDs and the fee pool, and gains a (read-only-ish) binding-dispute view. Less
custody-critical surface.

---

## 8. The one new economic dial — θ, and the demand-coupling trade-off

θ is the fraction of the 41% miner emission steered to the head; `1−θ` goes to the pools. It is set
**SN13-style** — validators run common software that reserves θ to the head UIDs (split by `Q_p`) and
`1−θ` to the pool UIDs (split by `deposit × Q_n`), exactly as Data Universe rewrites weights to reserve a
fixed share to one UID. **Both shares go to real recipients** (top miners; contract-owned pools), so the
**June-2026 `(1 − miner_burned)` penalty does *not* apply** — that penalty only bites emission *withheld to
an owner/immune key*. **Do not "reserve baseline by burning to an owner UID"** — post-Spec-421 that shrinks
the subnet's whole cross-subnet allocation. [verified: subtensor PR #2781; SN13 `apply_burn_to_weights`]

**θ is the load-bearing decision of this iteration**, because it trades the two bets against each other:

- **Large θ (head-heavy):** a big, meritocratic, trust-minimized direct channel — but most emission is then
  **demand-*de*coupled** (pure quality), diluting the `deposit × quality` headline bet (`COMPARISON.md` §8)
  into the minority tail share. It also **weakens NO deposit incentives**: a NO's best providers earn from
  the subnet-funded head, not the NO's deposit-funded pool, so the NO may deposit less (it funds only the
  baseline tail). The demand signal then governs only `1−θ` of the money.
- **Small θ (tail-heavy):** preserves demand-coupling as the dominant force and keeps NO deposits
  meaningful — but the merit apex becomes a small carrot, and a provider that graduates may earn *less*
  than it did in a well-funded pool, breaking the ladder.

**Recommendation:** treat θ as a governed parameter, start **tail-weighted** (the pool is the stated
on-ramp + baseline, and demand-coupling is the strategic edge — e.g. θ ≈ 0.3), **instrument the realized
per-provider pay in each tier**, and **widen θ as the top-level-miner set and the independent-validator
quality consensus mature** — the same "ramp the strength, not the mechanism" posture as the bootstrap
quality-swing cap (`DISCUSS.md` D14). Constraint to hold: size θ so the *lowest-paid top miner ≥
highest-paid pool provider*, or graduation is a pay cut and the head thrashes.

---

## 9. Anti-gaming (delta from `WHITEPAPER.md` §9.5, §12)

- **Stolen quality** — defeated by the **dual-signed binding** (§5.2): you cannot weight a `client_id` you
  can't prove you own.
- **Self-dealing, now at provider granularity** — the `VERIFIER.md` §7.7 per-hop self-dealing risk now
  maps to a UID. Same defenses, sharper: the **self-weight mask** (a validator can't weight its own UID),
  the **independent-validator baseline** (D7), **server-assigned hops only** (a verifier can't farm its own
  providers, `VERIFIER.md` §7.6), per-UID **registration burn**, and the `VERIFIER.md` §10 roadmap. Head
  rewards stay **provisional** until §10 lands, same as today.
- **Stake-domination** — the arXiv 2507.02951 critique (reward∼stake ≫ reward∼performance) bites *validator
  dividends*; **miners are not stake-gated** (only burn), and the head is weighted on pure measured `Q_p`,
  so the head is **less** stake-captured than typical. The objective `VERIFIER` measurement is the antidote
  the paper calls for. [arXiv 2507.02951 §5.3, verified]
- **Copy-weighting** — commit-reveal ON (Drand timelock, auto-reveal v4) hides the subjective `Q_p`/θ
  signal until stale, so copiers drift from consensus and lose vtrust.

---

## 10. Parameters & verify-before-launch (delta from `WHITEPAPER.md` §15)

| Parameter | Suggested | Note |
|---|---|---|
| `mechanism_count` | **1** | two mechanisms halve UID space < 200 (§3.2) |
| `max_allowed_uids` | **256** | hard ceiling; budget = head + pools + validators |
| head size (target) | `256 − V − P` (~200) | reserve `V` validators, `P` NO pools |
| **θ (head share)** | **governed, start ≈ 0.3, ramp** | the §8 dial — the key new decision |
| `max_weight_limit` | **set a real cap** (e.g. low single-digit %) | chain default is **no cap** — must override |
| `min_allowed_weights` | per design (often 1) | runtime default 1024; validators score all UIDs |
| `immunity_period` | **high (≫ 4096)** | protect new top miners through the measurement ramp (§6) |
| `commit_reveal_weights_enabled` | **true** | subjective `Q_p`/θ signal (default is false) |
| `Q_p` EMA α | ≈ 0.1 | smoothing vs. responsiveness (§4.3) |
| binding store | commitments pallet + ST-contract anchor | free public read + on-chain dispute (§5) |

**Re-verify live before launch:** the 256 cap and the `mechanism_count × max_UIDs < 256` rule; that
contract-owned pool UIDs are **not** treated as owner/immune (no `miner_burned` penalty on the tail share);
`commit_reveal` default; `immunity_period`/burn-auction live values; that a top-level-miner hotkey's
incentive accrues to its **own** coldkey with **no take** (so direct payout works as designed).

---

## 11. Open decisions (for you)

1. **θ and its ramp schedule** (§8) — the central call: how much of the 41% is pure-merit head vs.
   demand-coupled pool, and how it widens over time. Everything else is downstream of this.
2. **Head size vs. validator/NO budget** (§3.2) — pin `V` and `P`; is "~200" firm, or "as many as the
   budget allows after validators + pools"?
3. **Binding granularity** (§5.2) — one hotkey may bind *many* `client_id`s (simple); confirm that's
   acceptable, or require a separate UID per `client_id`.
4. **Demotion grace** (§6) — beyond `immunity_period` + EMA, do you want an explicit "recently-top" buffer,
   or accept native churn?
5. **Does this become D16–D20** in `DISCUSS.md` + edits to `WHITEPAPER.md` §§3, 8, 10, 15? (Say the word
   and I'll wire the doc changes + update the `diagrams/`.)

---

*End of proposal. Net shape: conserve the pool tier and the Yuma/anti-gaming plumbing untouched; add a
native, trust-minimized merit channel on top; spend the novelty budget on the `client_id→hotkey` binding
(well-precedented) and the θ split (the new economic dial). The tiered head/tail is without Bittensor
precedent — assembled from native top-N churn, field-standard identity binding, SN13 emission reservation,
and FileTAO/TPN-class measurement you already have.*
