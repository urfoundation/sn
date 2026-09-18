# Diagrams

Visual companions to `WHITEPAPER.md`.

## `mechanism.*` — "Mechanism at a glance" (§1)

A detailed, color-coded layout of the components and money flows described in
**§1. Summary of the mechanism** (the v0.2 **two-tier miner side**). The 41% miner emission now
splits by a governance share **θ** into two tiers of miner UIDs inside **one** mechanism:

- **TAIL / pool** (`1−θ`): the contract-owned **miner-pool UID per NO**; validators weight it
  `implied_usage × Qn` (implied usage = deposit ÷ conviction-tier rate — the contract weighs nothing,
  D25), settled to the 100k+ providers by **per-NO Merkle claim** — the low-barrier on-ramp / baseline.
- **HEAD / top-level miners** (`θ`): the top ~200 fleets as **their own UIDs**, ranked and weighted by
  **routable-IP breadth** (split-adjusted distinct egress-IP count, D27 — no deposit, no quality term),
  paid by **native** emission straight to their own hotkey — no contract custody, no Merkle claim, no take.

Independent validators score **both** tiers into one commit-reveal weight vector (split by θ) → Yuma.
The diagram also shows the ST contract internals (**deposits are conviction stake read from the
`Deposited` events — no DT ledger**; the contract does custody + settlement only), the pool UIDs, the
Merkle payout roots, and the **buyback reserve** (every deposit staked + locked, §7.4); the provider
lifecycle (start in a pool → graduate to a top slot → fall back if it slips); native validator
dividends; and the off-chain `VALIDATOR.md` measurement trails. (The fee-funded effort bounty is
out of v1 scope, §9.3/D29.)

| File | Use |
|---|---|
| `mechanism.png` | Raster export, 3480×2360 (2×). Drop into docs/slides. |
| `mechanism.svg` | Vector source. **Import into Figma** (File → Place/Import, or drag the `.svg` onto the canvas) to get fully editable vector layers and text. |
| `generate.py` | The generator. Edit and re-run to regenerate both files. |

### Regenerate

```bash
python3 generate.py        # writes mechanism.svg + mechanism.png into this folder
```

Requires `cairosvg` (`pip install cairosvg`). The script has no other dependencies.

### Editing notes

- Layout is driven by named coordinates near the top of `generate.py`; colors are in the
  `palette` block (one tuple per channel: deposits/blue, emission/teal, settlement/purple,
  evaluation/amber, bounty/rose, off-chain/slate).
- A few glyphs (`∝`, `→`, subscripts, `①`, `◀`, `⊕`, `⊥`, `⇄`) don't render in cairosvg's default
  font, so the scripts draw circled numbers and arrowheads themselves and use `Dn`/`Qn`/`wn`/`Qp` in
  place of subscripts (and words like "drives", or `<->` for `⇄`, in place of arrows). Glyphs that
  **do** render and are used freely: `θ  α φ ω  × · Σ  § − – ≈ ≥ ≤`. Keep that in mind when adding labels.

## `comparison_matrix.*` — design-decision alignment matrix

Companion to `COMPARISON.md`. A color-coded, at-a-glance matrix of the major design decisions
with the **Bittensor majority pattern** and **UR Subnet direction** side by side, each row tagged
**ALIGNED** (green) / **DIVERGENT** (amber) / **NOVEL** (purple). The visual story: a large green
block (we follow the Bittensor core), a small amber block (intentional divergences), and **two** purple
rows (the novel **demand-coupling** and **head/tail-tiering** bets). Tally: **12 aligned · 2 divergent · 2 novel**
(16 rows) — the **validator effort bounty is out of v1 scope** (D29), so validator rewards read as **aligned**
(native dividends only; `COMPARISON.md` §3).

| File | Use |
|---|---|
| `comparison_matrix.png` | Raster export, 3800×2920 (2×). |
| `comparison_matrix.svg` | Vector source — import into Figma. |
| `comparison_matrix.py` | Generator — `python3 comparison_matrix.py`. |

Verdicts are grounded in the research synthesized in `COMPARISON.md`; edit the `DATA` list in the
generator to adjust rows/wording.
