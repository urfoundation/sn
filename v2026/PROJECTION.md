# PROJECTION — Demand-Deposit Financial Projection (Year 1)

> **Status:** financial model, v0.1 · 2026-07-03
> **α price basis:** SN25 (Mainframe) spot = **$0.8047 / α** (CoinGecko, 2026-07-03; ≈ 0.00386 TAO at TAO ≈ $208.55)

---

## 1. What this models

Network operators (NOs) deposit α **every block** — where a *block* is the subnet's 7-day settlement
epoch (≈ 50,400 chain blocks; see `WHITEPAPER.md` §5) — priced on two usage meters: **GB transferred**
and **active users**. Per the buyback design (§7.4), every deposit is staked into the contract-locked
reserve and never re-liquefied, so the α modeled here is *cumulatively removed from liquid supply*.

This document converts the USD price target into fixed α rates, then projects the α staked per block
over one year of 10× usage growth.

## 2. Inputs

| Input | Value | Note |
|---|---|---|
| Baseline data transferred | **50 TB / block** (50,000 GB) | current network throughput |
| Baseline active users | **100,000 / block** | current network |
| Price target | **$10,000 USDC per 4 blocks** = $2,500 / block | at baseline usage |
| Revenue split (data : users) | **50 : 50** | modeling default; totals are split-invariant while both meters grow together |
| USDC/α peg | **$0.8047 / α** | fixed at launch to the SN25 spot price; occasionally re-pegged (§7) |
| Growth | **10× in both meters over 52 blocks** (1 year) | geometric, +4.618 % per block |

## 3. Rate card

The peg converts the USD targets into the two on-chain rates:

| Meter | USDC rate | α rate (at $0.8047 peg) |
|---|---|---|
| Data | $0.0250 / GB | **0.031067 α / GB** |
| Users | $0.0125 / active user · block | **0.015534 α / active user · block** |

**Baseline check:** 50,000 GB × 0.031067 + 100,000 users × 0.015534 = 1,553.4 + 1,553.4 =
**3,106.7 α / block** = $2,500 at peg → **$10,000 per 4 blocks** ✓

## 4. Growth model

Usage multiplier for block `t` (t = 1…52):

```
M(t) = 10^((t−1)/51)          g = 10^(1/51) = 1.046183   (+4.618 % / block)
```

Block 1 is exactly the baseline (50 TB, 100 k users, $2,500) and block 52 is exactly 10×
(500 TB, 1 M users, $25,000). α staked in block `t`:

```
A(t) = $2,500 × M(t) / $0.8047        →  A(1) = 3,107 α   …   A(52) = 31,067 α
```

## 5. Projection — α staked per block, by 4-block period

“% of emission absorbed” compares α staked to the α emitted to participants over the same 4 blocks
(4 × 50,400 = 201,600 α; owner 18 % / validators 41 % / miners 41 %).

| Period | Blocks | TB / block (end) | Users / block (end) | USDC deposited | α staked | α / block (avg) | Cumulative α reserve | % of emission absorbed |
|---|---|---|---|---|---|---|---|---|
| 1 | 1–4 | 57 | 114,505 | $10,714 | 13,315 | 3,329 | 13,315 | 6.6% |
| 2 | 5–8 | 69 | 137,169 | $12,835 | 15,950 | 3,988 | 29,265 | 7.9% |
| 3 | 9–12 | 82 | 164,318 | $15,375 | 19,107 | 4,777 | 48,372 | 9.5% |
| 4 | 13–16 | 98 | 196,842 | $18,419 | 22,889 | 5,722 | 71,261 | 11.4% |
| 5 | 17–20 | 118 | 235,803 | $22,064 | 27,419 | 6,855 | 98,680 | 13.6% |
| 6 | 21–24 | 141 | 282,475 | $26,431 | 32,846 | 8,212 | 131,526 | 16.3% |
| 7 | 25–28 | 169 | 338,386 | $31,663 | 39,348 | 9,837 | 170,874 | 19.5% |
| 8 | 29–32 | 203 | 405,362 | $37,930 | 47,136 | 11,784 | 218,010 | 23.4% |
| 9 | 33–36 | 243 | 485,595 | $45,438 | 56,465 | 14,116 | 274,475 | 28.0% |
| 10 | 37–40 | 291 | 581,709 | $54,431 | 67,642 | 16,910 | 342,117 | 33.6% |
| 11 | 41–44 | 348 | 696,847 | $65,205 | 81,030 | 20,257 | 423,146 | 40.2% |
| 12 | 45–48 | 417 | 834,773 | $78,111 | 97,068 | 24,267 | 520,214 | 48.1% |
| 13 | 49–52 | 500 | 1,000,000 | $93,571 | 116,281 | 29,070 | 636,495 | 57.7% |

## 6. Year-1 summary

- **$512,188 USDC-equivalent deposited → 636,495 α staked** into the locked reserve
  (average **12,240 α / block**; run rate grows 3,107 → 31,067 α / block).
- **Back-loaded:** ~49 % of the year's α is staked in the final 13 blocks — compounding growth means
  the first months look thin (≈ 13.3 k α in period 1) even though the year total is large.
- **Vs emission:** the reserve absorbs **24.3 %** of year-1 participant α emission
  (52 × 50,400 = 2,620,800 α). The *weekly* offset climbs from **6.2 % at launch to 61.6 %** at the
  10× run rate — i.e. by year-end, demand deposits re-lock roughly three-fifths of everything the
  subnet emits to owner + validators + miners each block.
- **Vs today's float (context):** at SN25's current $3.76 M market cap the implied circulating
  supply is ≈ 4.68 M α; the year-1 reserve (636 k α) equals ≈ **13.6 %** of that float.

## 7. Peg vs market price

The on-chain rates are fixed **in α**, so an NO's *realized* USD cost per block is
`A(t) × market price`, which equals the $-target only while market ≈ peg:

- **α appreciates above peg** → NOs overpay in USD terms (and deposits become stronger buy pressure);
  the scheduled **re-peg raises the peg**, cutting the α rates proportionally and restoring the USD target.
- **α falls below peg** → NOs underpay; re-pegging downward restores the target (and increases α
  staked per block).

At any re-peg the USD flow is held constant by construction; only the α flow changes, inversely with
the peg. Note the deposits themselves are structural buy demand — ~$512 k of α must be acquired over
year 1 against a token with a $3.76 M market cap today.

## 8. Sensitivity

**Peg level** (α flow scales as 1/peg at constant USD target):

| Peg (USDC/α) | α/GB | α/user·block | α/block at launch | α/block at 10× | Year-1 α staked |
|---|---|---|---|---|---|
| $0.4000 | 0.062500 | 0.031250 | 6,250 | 62,500 | 1,280,469 |
| **$0.8047** | **0.031067** | **0.015534** | **3,107** | **31,067** | **636,495** |
| $1.6000 | 0.015625 | 0.007812 | 1,562 | 15,625 | 320,117 |
| $3.2000 | 0.007812 | 0.003906 | 781 | 7,812 | 160,059 |

**Growth shape:** a *linear* ramp to 10× (instead of geometric) front-loads usage and yields
**$715,000 → 888,530 α** for year 1 (33.9 % of participant emission). The geometric base case is the
conservative one.

## 9. Formulas (re-derive on re-peg or new targets)

With target `T` $/4-blocks, split `s` (data share), baseline `GB₀` GB/block and `U₀` users/block,
peg `P` $/α:

```
rate_GB   = (T/4) × s      / GB₀ / P          [α per GB]
rate_user = (T/4) × (1−s)  / U₀  / P          [α per active user · block]
A(t)      = (T/4) × M(t) / P                  [α staked in block t]
Year α    = (T/4)/P × Σₜ M(t) = (T/4)/P × (10g − 1)/(g − 1) = (T/4)/P × 204.88
```

## 10. Caveats

1. **The peg is a snapshot.** $0.8047 is the 2026-07-03 SN25 spot; subnet-α prices routinely move
   double-digit % daily. Fix the actual peg from the launch-day quote and re-run §9.
2. **Emission assumptions:** 1 α per 12-s chain block to participants (41/41/18), pool injection
   excluded. Any future α-emission halving roughly doubles the "% absorbed" figures.
3. **Reserve dividends not modeled.** The reserve is staked to the locked owner-validator and
   compounds dividends (§7.4), so the actual reserve balance will exceed the cumulative-deposit
   column above.
4. **Uniform growth assumed:** both meters grow 10× together (data per user constant at
   0.5 GB/user·block), which keeps totals independent of the 50:50 split. If the meters diverge,
   totals shift toward the faster-growing meter's share.

---

*Sources: [CoinGecko — Bittensor Subnet Tokens](https://www.coingecko.com/en/categories/bittensor-subnets)
(SN25 Mainframe $0.8047, mcap $3.76 M, 2026-07-03) ·
[CoinGecko — Bittensor (TAO)](https://www.coingecko.com/en/coins/bittensor) ($208.55) ·
[taostats — subnet 25](https://taostats.io/subnets/25).*
