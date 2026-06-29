# Diagrams

Visual companions to `WHITEPAPER.md`.

## `mechanism.*` — "Mechanism at a glance" (§1)

A detailed, color-coded layout of the components and money flows described in
**§1. Summary of the mechanism**: the three coupled channels (deposits, emission via Yuma,
settlement by Merkle claim), the ST contract internals (deposit ledger, contract-owned
miner-pool UIDs, Merkle payout roots, FeePool), independent validators + Yuma consensus,
the effort bounty, and the off-chain `VERIFIER.md` measurement trails.

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
- A few glyphs (`∝`, `→`, subscripts, `①`, `◀`) don't render in cairosvg's default font, so the
  scripts draw circled numbers and arrowheads themselves and use `Dn`/`Qn`/`wn` in place of
  subscripts (and words like "drives" in place of `→`). Keep that in mind when adding labels.

## `comparison_matrix.*` — design-decision alignment matrix

Companion to `COMPARISON.md`. A color-coded, at-a-glance matrix of the major design decisions
with the **Bittensor majority pattern** and **UR Subnet direction** side by side, each row tagged
**ALIGNED** (green) / **DIVERGENT** (amber) / **NOVEL** (purple). The visual story: a large green
block (we follow the Bittensor core), a small amber block (intentional divergences), one purple
row (the novel deposit-weighting bet).

| File | Use |
|---|---|
| `comparison_matrix.png` | Raster export, 3800×2656 (2×). |
| `comparison_matrix.svg` | Vector source — import into Figma. |
| `comparison_matrix.py` | Generator — `python3 comparison_matrix.py`. |

Verdicts are grounded in the research synthesized in `COMPARISON.md`; edit the `DATA` list in the
generator to adjust rows/wording.
