# Diagrams

Visual companions to `WHITEPAPER.md`.

## `mechanism.*` — "Mechanism at a glance" (§1, settlement v2)

A detailed, color-coded layout of the components and money flows of **§1** under **settlement v2**
(`VERSION2-FINAL.md`). Version 2 changes only the pool tier's payout settlement, from the switch epoch
**E_s**; the figure numbers its six flows:

1. **Emission.** The 41% miner row is one weight vector: head `θ·μ` to the top-miner UIDs, pool
   `(1−θ)·μ` to the operator's **v2 pool UID**, and on SN25 the treasury row `1 − μ` to `ur-reserve`'s
   two recipient UIDs (μ = 1/10, θ = 3/10; §8.5).
2. **Capture.** At each epoch's close, vault2 captures the pool hotkey's movable stake into its one escrow
   hotkey.
3. **Ledger root.** The operator allocates each epoch's funding by usage only (operator-attested in phase A)
   into cumulative `(coldkey, cumulative)` accounts, one per payout coldkey per pool, and commits a Merkle
   **sum** tree root.
4. **Audit and approval.** Owner-admitted auditors (the validator alone at launch) approve the exact root.
5. **Challenge or demand.** While a root is pending anyone may prove a fault (`checkPath`, which deletes
   the root), and within its 14,400-block window anyone may demand an account's path (`demandPath`); a root
   finalizes only when it is approved, the window has passed and no demand is open.
6. **Claim.** One cumulative claim pays everything an account accrued since its last, only to the leaf's
   coldkey; anyone may send it, and balances never expire.

Unchanged and shown as such: the head fleets (their own UIDs, native emission on routable-IP breadth, D27),
validator dividends, and the v1 contracts, which still pay every epoch before E_s and keep deposits as
conviction stake in the buyback reserve (§7.4). The bottom strip shows one pool epoch's lifecycle; the
banner states vault2's guarantee (VERSION2-FINAL §4.2).

| File | Use |
|---|---|
| `mechanism.png` | Raster export, 3800×2900 (2×). Drop into docs/slides. |
| `mechanism.svg` | Vector source. **Import into Figma** (File → Place/Import, or drag the `.svg` onto the canvas) to get fully editable vector layers and text. |
| `generate.py` | The generator. Edit and re-run to regenerate both files. |

### Regenerate

```bash
python3 generate.py        # writes mechanism.svg + mechanism.png next to the script
```

Requires `cairosvg` (`pip install cairosvg`). The script has no other dependencies.

### Editing notes

- Layout is driven by named coordinates in `generate.py`; colors are in the `palette` block (one tuple per
  channel: emission/teal (capture dashed), ledger/blue (`LED`), approval and evaluation/amber, disputes/rose
  (`DIS`), claims/purple, head/cyan, treasury/lime (`TRE`), off-chain/slate).
- The SVG sets `xml:space="preserve"`, so runs of spaces in labels are kept; the aligned monospace rows in
  the Yuma card depend on it.
- A few glyphs (`∝`, `→`, subscripts, `①`, `◀`, `⊕`, `⊥`, `⇄`) don't render in cairosvg's default
  font, so the scripts draw circled numbers and arrowheads themselves and use `Dn`/`Qn`/`wn`/`Qp` in
  place of subscripts (and words like "drives", or `<->` for `⇄`, in place of arrows). Glyphs that
  **do** render and are used freely: `θ  μ  α φ ω  × · Σ  § − – ≈ ≥ ≤`. Keep that in mind when adding labels.

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
