#!/usr/bin/env python3
"""README companion figures (diagrams/readme/*.svg).

One small figure per README section. Plain SVG, no dependencies; re-run to regenerate.
Numbers are the release-1.0 / testnet policy-v2 values cited in the README; edit the
constants below if the policy changes.
"""
import os

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "readme")
os.makedirs(OUT, exist_ok=True)

SANS = "Inter,-apple-system,'Segoe UI',Helvetica,Arial,sans-serif"
MONO = "'JetBrains Mono',SFMono-Regular,Menlo,Consolas,monospace"
INK, SUB, FAINT, LINE, PANEL, WHITE = "#0D0D0D", "#5F5F5F", "#8A8A8A", "#D9DDD9", "#F8F7F5", "#FFFFFF"
G, GT, DK, PN = "#155E3B", "#E7F0EA", "#14231B", "#F4F4F2"
# flow palette, matching mechanism.svg: (accent, tint)
DEP = (INK, PN)                # deposits (neutral)
EMI = (G, GT)                  # emission
SET = (G, GT)                  # settlement / pool tier
HEAD = (DK, PN)                # head tier
VAL = ("#8A5A00", "#FBF1DC")   # validation
RES = ("#C93A3F", "#FBE9EA")   # reserve
OWN = (SUB, PN)                # owner / neutral


def esc(s):
    return str(s).replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


class Fig:
    def __init__(self, h, title, kicker, desc):
        self.w, self.h, self.b, self.desc = 900, h, [], desc
        self.marks = set()
        self.k(30, 32, kicker)
        self.t(30, 60, title, 22, INK, 500)

    def r(self, x, y, w, h, fill=WHITE, stroke=LINE, rx=10, sw=1.4, dash=None):
        d = f' stroke-dasharray="{dash}"' if dash else ""
        self.b.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{rx}" fill="{fill}" stroke="{stroke}" stroke-width="{sw}"{d}/>')

    def t(self, x, y, s, size=13, fill=INK, weight=400, anchor="start", mono=False):
        f = MONO if mono == "code" else SANS
        self.b.append(f'<text x="{x}" y="{y}" font-family="{f}" font-size="{size}" font-weight="{weight}" fill="{fill}" text-anchor="{anchor}">{esc(s)}</text>')

    def k(self, x, y, s, fill=SUB, anchor="start"):
        self.b.append(f'<text x="{x}" y="{y}" font-family="{MONO}" font-size="10.5" font-weight="600" letter-spacing="1.2" fill="{fill}" text-anchor="{anchor}">{esc(s.upper())}</text>')

    def a(self, d, col=FAINT, sw=1.8, dash=None, head=True):
        self.marks.add(col)
        m = f' marker-end="url(#m{col[1:]})"' if head else ""
        ds = f' stroke-dasharray="{dash}"' if dash else ""
        self.b.append(f'<path d="{d}" fill="none" stroke="{col}" stroke-width="{sw}"{ds}{m}/>')

    def c(self, x, y, r, fill, stroke="none"):
        self.b.append(f'<circle cx="{x}" cy="{y}" r="{r}" fill="{fill}" stroke="{stroke}" stroke-width="1.6"/>')

    def chip(self, x, y, s, pal, anchor="start", mono=True):
        w = len(s) * (7.0 if mono else 6.6) + 18
        x0 = x - w / 2 if anchor == "middle" else (x - w if anchor == "end" else x)
        self.r(x0, y - 14, w, 21, pal[1], pal[0], 10.5, 1)
        self.t(x0 + w / 2, y + 1, s, 11.5, pal[0], 600, "middle", mono)
        return w

    def box(self, x, y, w, h, title, lines=(), pal=OWN, bold=False, tsize=14):
        neutral = pal[0] in (INK, DK, SUB)
        hot = bold and not neutral
        self.r(x, y, w, h, pal[1] if hot else WHITE, pal[0] if hot else LINE, 12, 1.5 if hot else 1.4)
        self.t(x + 14, y + 23, title, tsize, pal[0] if hot else INK, 600)
        for i, ln in enumerate(lines):
            self.t(x + 14, y + 44 + i * 17, ln, 12.5, SUB, 400)

    def save(self, name):
        marks = "".join(
            f'<marker id="m{c[1:]}" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0L10 5L0 10z" fill="{c}"/></marker>'
            for c in sorted(self.marks))
        svg = (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {self.w} {self.h}" width="{self.w}" height="{self.h}" role="img" aria-label="{esc(self.desc)}">'
               f'<title>{esc(self.desc)}</title><defs>{marks}</defs>'
               f'<rect width="{self.w}" height="{self.h}" rx="16" fill="{PANEL}"/>' + "".join(self.b) + "</svg>\n")
        with open(os.path.join(OUT, name), "w") as f:
            f.write(svg)


# ---------------------------------------------------------------- 01 roles
f = Fig(330, "Who does what", "Overview", "Customers buy privacy traffic from network operators; providers carry it; validators walk provider chains, score both miner tiers and set weights; Yuma turns weights into alpha emission.")
f.box(30, 84, 170, 70, "Customers", ["buy VPN / privacy", "traffic (the demand)"])
f.box(260, 84, 200, 70, "Network Operator (NO)", ["runs servers + /verify", "commits payout roots"], DEP, True)
f.box(520, 84, 350, 70, "Providers  (100k+)", ["carry ingress / egress traffic", "client_id, not a UID (pool tier)"], SET, True)
f.a("M200 119H256", SUB); f.a("M460 119H516", SUB)
f.t(228, 110, "$", 12, SUB, 600, "middle")
f.t(488, 110, "routes", 10.5, SUB, 400, "middle", True)
# validator trail
f.box(30, 196, 230, 112, "Validators", ["stake their own α", "walk server-assigned", "provider chains every tempo"], VAL, True)
xs = [330, 430, 530, 630]
for i, x in enumerate(xs):
    f.c(x, 244, 13, WHITE, SET[0]); f.t(x, 248, f"p{i+1}" if i < 3 else "exit", 10, SET[0], 600, "middle", True)
    if i < 3:
        f.a(f"M{x+14} 244H{xs[i+1]-16}", VAL[0], 1.8, "5 4")
f.a("M260 244H314", VAL[0], 1.8, "5 4")
f.t(480, 282, "signed hop proofs: liveness, latency, egress-IP hash", 11, VAL[0], 500, "middle", True)
f.box(680, 196, 190, 112, "Yuma Consensus", ["weights (commit-reveal)", "→ stake-weighted", "median + clipping", "→ α emission"], EMI, True)
f.a("M646 244H676", EMI[0])
f.save("01-roles.svg")

# ---------------------------------------------------------------- 02 channels
f = Fig(250, "Three money channels, all in α", "Mechanism at a glance", "Three channels: deposits are locked forever in a reserve; emission is set by validators every tempo; settlement pays pool providers by Merkle claim every seven-day epoch.")
C = [("1", "Deposits", DEP, ["NO stakes α sized to usage", "→ locked in the reserve", "never distributed"], "per epoch, if priced"),
     ("2", "Emission", EMI, ["validators score both tiers", "Yuma pays 18 / 41 / 41", "the 41% miner share is steered"], "every tempo ≈ 72 min"),
     ("3", "Settlement", SET, ["vault captures pool emission", "NO commits a payout root", "providers claim with a proof"], "every epoch = 7 days")]
for i, (n, title, pal, lines, cad) in enumerate(C):
    x = 30 + i * 284
    f.r(x, 80, 268, 150, WHITE, LINE if pal[0] == INK else pal[0], 12, 1.5)
    f.c(x + 24, 104, 12, pal[0]); f.t(x + 24, 108.5, n, 12, WHITE, 700, "middle")
    f.t(x + 44, 109, title, 15, pal[0], 600)
    for j, ln in enumerate(lines):
        f.t(x + 16, 138 + j * 19, ln, 12, INK, 400, "start", True)
    f.chip(x + 16, 212, cad, pal)
f.save("02-channels.svg")

# ---------------------------------------------------------------- 03 deposits
f = Fig(400, "Deposits: one-way into the reserve", "Channel 1 · deposits", "A network operator stakes alpha on its deposit hotkey; the coordinator moves the exact amount into the immutable reserve sink, staked on the owner-validator hotkey; nothing ever leaves; cumulative locked alpha sets a cheaper rate tier.")
f.box(30, 84, 170, 72, "NO coldkey", ["stakes α on its", "deposit hotkey"], DEP)
f.box(250, 84, 190, 72, "STCoordinator", ["deposit() by the", "NO's deposit signer"], DEP, True)
f.box(490, 84, 230, 72, "STReserveSink  (immutable)", ["staked on reserve hotkey", "= owner-validator hotkey"], RES, True)
f.a("M200 116H246", DEP[0]); f.a("M440 116H486", RES[0])
f.t(463, 108, "exact", 10.5, RES[0], 600, "middle", True)
f.r(750, 84, 120, 72, WHITE, RES[0], 10, 1.4, "4 3")
f.t(810, 110, "no outbound", 12, RES[0], 600, "middle"); f.t(810, 128, "code path", 12, RES[0], 600, "middle")
f.a("M720 116H746", RES[0], 1.6, None, False)
# dividends loop and consensus weight
f.a("M560 156V178H650V160", RES[0], 1.6)
f.t(605, 192, "dividends compound in place", 11, RES[0], 500, "middle", True)
f.a("M700 156Q740 196 770 206", VAL[0], 1.6, "5 4")
f.t(790, 226, "adds to the owner", 11, VAL[0], 500, "middle", True)
f.t(790, 242, "validator's stake weight", 11, VAL[0], 500, "middle", True)
# tiers
f.k(30, 196, "Conviction → rate tier (testnet policy-v2)")
T = [("0 α", "40 α / GiB"), ("≥ 1 α", "32 α / GiB"), ("≥ 10 α", "24 α / GiB")]
for i, (cv, rate) in enumerate(T):
    x = 30 + i * 150
    f.r(x, 208, 138, 56, WHITE, LINE, 12, 1.3)
    f.t(x + 12, 230, "locked " + cv, 12, SUB, 400, "start", True)
    f.t(x + 12, 251, rate, 14, G, 600)
f.t(30, 292, "Required deposit = last epoch's audited usage × the NO's tier rate (tier snapshotted before the epoch).", 12.5, INK)
f.t(30, 312, "A missing or mismatched deposit sets that NO's pool weight to 0.", 12.5, INK)
f.r(30, 330, 840, 50, OWN[1], "none", 10, 0)
f.t(46, 352, "Zero-price launch mode (zero_rate_action: equal_demand): no deposits are required, every pool carries", 12, SUB)
f.t(46, 369, "the same implied demand, and quality alone steers the pool channel. The shipped testnet policy instead halts on a zero rate.", 12, SUB)
f.save("03-deposits.svg")

# ---------------------------------------------------------------- 04 emission
f = Fig(396, "Emission: who gets each tempo's α", "Channel 2 · emission", "Each tempo the coinbase splits alpha 18 percent owner, 41 percent miners, 41 percent validators. Validators steer the miner share: theta to head fleets by routable-IP breadth, one minus theta to pool UIDs by implied demand times clamped quality.")
X0, WB = 30, 840
segs = [(0.18, "owner 18%", OWN[0]), (0.41, "miners 41%", EMI[0]), (0.41, "validators 41%", VAL[0])]
x = X0
for p, lab, col in segs:
    f.r(x, 84, WB * p - 3, 34, col, "none", 8, 0); f.t(x + (WB * p - 3) / 2, 106, lab, 13, WHITE, 600, "middle"); x += WB * p
mx0, mw = X0 + WB * 0.18, WB * 0.41
f.t(X0 + WB * 0.59 + WB * 0.205, 140, "native: ∝ stake × vtrust (incl. the reserve)", 11.5, VAL[0], 500, "middle", True)
f.a(f"M{mx0 + mw/2} 118V156", EMI[0])
f.t(mx0 + mw / 2, 176, "validators steer the miner share by θ", 11.5, EMI[0], 600, "middle", True)
# theta split bar
f.r(30, 190, 840 * 0.3 - 3, 30, HEAD[0], "none", 8, 0); f.t(30 + 126, 210, "θ = 0.3  head fleets", 12.5, WHITE, 600, "middle")
f.r(30 + 840 * 0.3, 190, 840 * 0.7, 30, SET[0], "none", 8, 0); f.t(30 + 252 + 294, 210, "1 − θ = 0.7  pool UIDs (one per NO)", 12.5, WHITE, 600, "middle")
f.box(30, 236, 300, 92, "Head: up to 200 fleets", ["weight = split-adjusted count of", "routable egress-prefix hashes", "paid natively to the fleet hotkey"], HEAD, True, 13)
f.box(346, 236, 524, 92, "Pool: per-NO UID", ["weight = implied_usage × clamp(Q, 0.75, 1.0)", "implied_usage = audited usage × baseline rate (1 at zero price)", "paid into the vault, then to providers by Merkle claim"], SET, True, 13)
f.r(30, 340, 840, 40, RES[1], RES[0], 10, 1, "4 3")
f.t(46, 365, "Mainnet bootstrap plan (mainnet/MAINNET.md): provider weights fill 10% of the miner row; 90% goes to recognized owner hotkeys (owner-recycle).", 11.5, RES[0], 500)
f.save("04-emission.svg")

# ---------------------------------------------------------------- 05 scoring
f = Fig(330, "How a validator scores the two tiers", "Channel 2 · scoring", "Validators measure providers on server-assigned hops: Wilson-score liveness and latency percentiles, EMA-smoothed. Pools get an exposure-weighted mean quality per NO, clamped to 0.75 to 1. Head fleets get a split-adjusted count of distinct routable prefix hashes.")
f.box(30, 84, 250, 110, "Trail measurements", ["server-assigned, non-seed hops", "liveness: Wilson-score interval", "latency: median / p95 / p99", "EMA-smoothed per provider"], VAL, True, 13)
f.a("M280 114H330V106H366", SET[0]); f.a("M280 150H330V214H366", HEAD[0])
f.box(370, 74, 500, 92, "Pool quality Q_n  (tail)", ["exposure-weighted mean of the NO's tail providers", "(head-bound clients excluded); Q > 0 is clamped to [0.75, 1]", "→ live pools differ by at most 1.33× on quality"], SET, True, 13)
f.box(370, 178, 500, 92, "Head score  (fleets)", ["count distinct routable /29 IPv4 · /48 IPv6 prefix hashes", "a hash shared by k fleets counts 1/k to each", "no separate quality term: routable = it completed a hop"], HEAD, True, 13)
f.t(30, 302, "Each validator scores from its own trails, normalizes head to θ and pools to 1 − θ, then commits one vector (commit-reveal).", 12, SUB)
f.save("05-scoring.svg")

# ---------------------------------------------------------------- 06 settlement timeline
f = Fig(330, "One settlement epoch (mainnet: 50,400 blocks ≈ 7 days)", "Channel 3 · settlement", "Settlement timeline: at the epoch boundary anyone closes the epoch and the vault moves pool emission to escrow; the NO commits a payout root within four hours; after 48 hours anyone finalizes and claims open; claims expire after epoch e plus nine and the remainder carries to the same NO.")
L, R, Y = 100, 830, 150
f.r(L, Y - 3, R - L, 6, LINE, "none", 3, 0)
P = [(100, "t = 0", "close (≤ 120 blocks)", "pool hotkey → escrow", OWN[0]),
     (290, "+4 h", "root deadline", "NO commits root", SET[0]),
     (480, "+48 h", "finalize", "total fixed · claims open", EMI[0]),
     (790, "end of e+9", "expiry", "unclaimed → carry", RES[0])]
for x, when, what, sub, col in P:
    f.c(x, Y, 9, col); f.t(x, Y - 20, when, 12, col, 700, "middle", True)
    f.t(x, Y + 32, what, 12.5, INK, 600, "middle"); f.t(x, Y + 50, sub, 11, SUB, 400, "middle", True)
f.r(300, 92, 170, 26, VAL[1], "none", 8, 0); f.t(385, 109, "audit window", 11, VAL[0], 600, "middle", True)
f.a("M480 222H530", SET[0])
f.t(536, 226, "providers claim: proof of (coldkey, bps) → α", 11.5, SET[0], 500, "start", True)
f.r(30, 248, 840, 66, OWN[1], "none", 10, 0)
f.t(46, 270, "Missed close → that epoch's capture is deferred (recorded as 0) to the next boundary.", 12, SUB)
f.t(46, 288, "Missed root, or unclaimed shares at expiry → carried to the same NO's next epoch.", 12, SUB)
f.t(46, 306, "Pause blocks close and finalize, never claims. Roots are not challengeable on-chain; the audit is off-chain.", 12, SUB)
f.save("06-settlement.svg")

# ---------------------------------------------------------------- 07 tiers ladder + UID budget
f = Fig(380, "Two tiers: a pooled tail and a direct head", "Two miner tiers", "A fleet starts paid inside a pool, registers its own UID and binds its client_ids to graduate to the head, and is pruned back to the pool by native lowest-emission deregistration. The 256-UID budget is shared by head fleets, pool UIDs, validators and owner identities.")
f.box(30, 84, 360, 108, "Pool tier (tail)", ["one vault-owned UID per NO", "providers are client_ids, no UID, no burn", "paid by Merkle claim inside the pool", "low barrier, baseline pay"], SET, True, 13)
f.box(510, 84, 360, 108, "Head tier (≤ 200 fleets)", ["a fleet = one hotkey binding many client_ids", "own UID, paid natively each tempo", "ranked by routable-prefix breadth", "no contract, no operator in the payout path"], HEAD, True, 13)
f.a("M390 112H506", HEAD[0]); f.t(448, 104, "burn-register", 10.5, HEAD[0], 600, "middle", True)
f.t(448, 128, "+ dual-signed bind", 10.5, HEAD[0], 600, "middle", True)
f.a("M510 162H394", RES[0], 1.8, "5 4"); f.t(452, 178, "pruned", 10.5, RES[0], 600, "middle", True)
f.t(30, 214, "A client_id earns in one tier at a time: the NO server drops bound clients from its payout root (off-chain check).", 11.5, SUB)
# UID budget
f.k(30, 240, "256-UID budget (example: 64 validators, 2 NOs)")
B = [(64, "validators", VAL), (2, "", OWN), (2, "", SET), (188, "head fleets  (≤ 188 here, cap 200)", HEAD)]
x = 30
for n, lab, pal in B:
    w = 840 * n / 256
    f.r(x, 252, w - 2, 30, pal[0], "none", 6, 0)
    if lab:
        f.t(x + (w - 2) / 2, 272, lab, 12, WHITE, 600, "middle")
    x += w
f.t(30 + 840 * 64 / 256 + 6, 300, "owner + escrow (2) · pool UIDs (1 per NO)", 11, SUB, 400, "start", True)
f.r(30, 314, 840, 48, OWN[1], "none", 10, 0)
f.t(46, 334, "The head gets what is left. With 56 validators (the whitepaper target) and 200 fleets, no room remains for pools,", 12, SUB)
f.t(46, 351, "so the practical head is 256 − validators − pools − 2. Losing fleets earn 0 and are pruned when a new fleet registers.", 12, SUB)
f.save("07-tiers.svg")

# ---------------------------------------------------------------- 08 custody
f = Fig(300, "Custody: three contracts, only one upgradeable", "Custody and trust model", "Three contracts: the reserve sink and settlement vault are immutable; only the coordinator is UUPS-upgradeable by its owner, a 2-of-3 Safe on mainnet; a timelock is planned for phase 1.")
K = [("STReserveSink", "immutable", RES, ["holds every deposit", "no outbound transfer", "staked on reserve hotkey"]),
     ("STSettlementVault", "immutable", SET, ["owns pool UIDs + escrow", "captures pool emission", "claims cannot be paused"]),
     ("STCoordinator", "UUPS upgradeable", DEP, ["admits NOs (owner only)", "policy + deposit checks", "guardian can pause new risk"])]
for i, (n, tag, pal, lines) in enumerate(K):
    x = 30 + i * 284
    f.box(x, 84, 268, 112, n, lines, pal, True, 14)
    f.chip(x + 254, 104, tag, pal if i == 2 else OWN, "end")
f.r(598, 222, 272, 56, WHITE, LINE, 12, 1.4)
f.t(612, 244, "owner: 2-of-3 Safe (mainnet)", 12.5, INK, 600)
f.t(612, 263, "timelock ≥ 1 epoch: planned, phase 1", 11.5, SUB, 400, "start", True)
f.a("M734 222V200", FAINT)
f.t(30, 244, "Finalized claims are paid by direct pull to the provider's coldkey.", 12.5, INK)
f.t(30, 264, "No owner, NO or upgrade can move another party's α.", 12.5, INK)
f.save("08-custody.svg")

# ---------------------------------------------------------------- 09 participate
f = Fig(344, "Three ways to take part", "Participate", "Network operators are admitted by the owner, run servers and commit payout roots. Providers register a client_id and claim from their NO's pool or graduate as a fleet. Validators register, stake and set weights every tempo.")
P = [("Network operator", DEP, ["1  owner admits the NO;", "   vault registers its pool UID", "2  run servers + /verify", "3  deposit each epoch (if priced)", "4  commit the payout root ≤ 4 h"]),
     ("Provider", SET, ["1  register a client_id", "2  carry traffic for an NO", "3  provider claim each epoch", "   or, as a fleet:", "   fleet register · publish · bind"]),
     ("Validator", VAL, ["1  validator init / register", "2  validator stake add", "3  run /verify trails", "4  commit-reveal weights", "   every tempo → dividends"])]
for i, (n, pal, lines) in enumerate(P):
    x = 30 + i * 284
    f.box(x, 84, 268, 164, n, [], pal, True, 15)
    for j, ln in enumerate(lines):
        f.t(x + 16, 134 + j * 22, ln, 12, INK, 400, "start", True)
f.r(30, 264, 840, 58, OWN[1], "none", 10, 0)
f.t(46, 287, "Validators are permissionless, but at launch the owner is the stake-majority validator:", 12, SUB)
f.t(46, 305, "the reserve is staked on its hotkey (WHITEPAPER.md §7.4, §9.2).", 12, SUB)
f.save("09-participate.svg")

# ---------------------------------------------------------------- 10 one mechanism
f = Fig(270, "One mechanism, two channels in one weight vector", "One mechanism", "Release 1.0 uses one mechanism: head and pool weights share one native weight vector, normalized to theta and one minus theta, so about 200 head UIDs fit in 256. A second mechanism would split the UID space.")
f.k(30, 92, "Release 1.0 · mechanism_count = 1")
f.r(30, 102, 840 * 0.3 - 2, 34, HEAD[0], "none", 8, 0); f.t(30 + 125, 124, "head fleets → θ", 12.5, WHITE, 600, "middle")
f.r(30 + 252, 102, 840 * 0.7, 34, SET[0], "none", 8, 0); f.t(282 + 294, 124, "pool UIDs → 1 − θ", 12.5, WHITE, 600, "middle")
f.t(30, 156, "one vector · one Yuma run · one 256-UID metagraph", 11.5, SUB, 400, "start", True)
f.k(30, 190, "Rejected · two sub-mechanisms")
f.r(30, 200, 418, 34, WHITE, FAINT, 8, 1.3, "4 3"); f.t(239, 222, "mechanism 0", 12, SUB, 500, "middle", True)
f.r(452, 200, 418, 34, WHITE, FAINT, 8, 1.3, "4 3"); f.t(661, 222, "mechanism 1", 12, SUB, 500, "middle", True)
f.t(30, 254, "Each mechanism gets its own share of the UID space, so neither could hold a ~200-fleet head.", 11.5, SUB, 400, "start", True)
f.save("10-one-mechanism.svg")

print(sorted(os.listdir(OUT)))
