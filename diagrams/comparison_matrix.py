#!/usr/bin/env python3
# Visual comparison matrix: UR Subnet vs Bittensor norm, color-coded by verdict.
import cairosvg, os

SANS="'Helvetica Neue','Helvetica','Arial',sans-serif"
INK="#0f172a"; SUB="#475569"; FAINT="#94a3b8"
S=[]

# verdict palette: (light fill, mid/accent, dark/chip)
V={
 "ALIGNED":  ("#ecfdf5","#10b981","#047857"),
 "DIVERGENT":("#fff7ed","#f59e0b","#b45309"),
 "NOVEL":    ("#f5f3ff","#8b5cf6","#6d28d9"),
}
GROUP_BLURB={
 "ALIGNED":  "we follow Bittensor best practice",
 "DIVERGENT":"same goal, different direction",
 "NOVEL":    "little / no precedent on Bittensor",
}

def esc(t): return t.replace("&","&amp;").replace("<","&lt;").replace(">","&gt;")
def T(x,y,s,size=13,weight="normal",fill=INK,anchor="start",family=SANS,italic=False):
    st=' font-style="italic"' if italic else ''
    S.append(f'<text x="{x:.1f}" y="{y:.1f}" font-family="{family}" font-size="{size}" '
             f'font-weight="{weight}" fill="{fill}" text-anchor="{anchor}"{st}>{esc(s)}</text>')
def rect(x,y,w,h,rx=0,fill="#fff",stroke=None,sw=1.4,dash=None,op=None):
    s=f' stroke="{stroke}" stroke-width="{sw}"' if stroke else ''
    d=f' stroke-dasharray="{dash}"' if dash else ''
    o=f' opacity="{op}"' if op is not None else ''
    S.append(f'<rect x="{x:.1f}" y="{y:.1f}" width="{w:.1f}" height="{h:.1f}" rx="{rx}" fill="{fill}"{s}{d}{o}/>')
def wrap(s,n):
    words=s.split(); lines=[]; cur=""
    for w in words:
        if len(cur)+len(w)+(1 if cur else 0)<=n: cur=(cur+" "+w).strip()
        else: lines.append(cur); cur=w
    if cur: lines.append(cur)
    return lines or [""]
def chip(cx,cy,label,verdict):
    _,mid,dark=V[verdict]
    w=len(label)*8.6+26; h=28
    rect(cx-w/2,cy-h/2,w,h,14,fill=dark)
    T(cx,cy+5,label,13,"bold","white",anchor="middle")

# ---------- data ----------
# (title, sub, norm, ours)
DATA=[
 ("ALIGNED",[
  ("Emission split","owner / miner / validator",
   "Fixed 18 / 41 / 41, protocol-enforced; not owner-customizable",
   "Standard 18 / 41 / 41 — “we do not fight the coinbase”"),
  ("Consensus engine","what moves the money",
   "Validators score miners; Yuma Consensus drives emission",
   "Independent validators score pools + head; Yuma drives emission"),
  ("Anti-gaming stack","",
   "Always-on Yuma core; commit-reveal & Liquid Alpha are opt-in",
   "Full stack ON: commit-reveal, clip+vtrust, self-mask, bonds"),
  ("Validator independence","",
   "Many independent validators; stake-weighted permits",
   "Independent validator UIDs; no Network Operator owns one"),
  ("Token & economics","",
   "α-denominated, dTAO; stake / price is the demand proxy",
   "All α; slippage-free transferStake; α buy & stake pressure"),
  ("On-chain oracles","",
   "Avoided; validators fetch off-chain, Yuma median reconciles",
   "No oracle; off-chain governance-published reference rate"),
  ("Multi-mechanism subnets","",
   "≤2 mechanisms / subnet, each own Yuma + bonds (Sept 2025)",
   "Pool 0 (core) / Pool 1 (VPN factory) via sub-mechanisms"),
  ("Scaling past the 256-UID cap","",
   "1 UID fronts many off-chain workers (ComputeHorde, TPN, Vanta)",
   "Pool UIDs (tail) + top ~200 direct UIDs (head) in one 256-UID metagraph"),
  ("Real-world / DePIN output","",
   "Respected minority: compute, storage, VPN / bandwidth",
   "Privacy / VPN — providers carry ingress & egress traffic"),
  ("Verification rigor","",
   "Trending crypto/deterministic; heuristic for real-world work",
   "Cryptographic routing-verification (signed proof-of-transit)"),
  ("Off-chain-worker identity binding","",
   "Signed proof + ss58 + metagraph-membership check, fail-closed (Epistula / ORO-AI)",
   "Celium-style dual-signed client_id-hotkey association; same fail-closed check"),
 ]),
 ("DIVERGENT",[
  ("Reward settlement & custody","partial — head native",
   "Pure native emission to hotkeys; no contract in the reward loop",
   "Head: pure native emission, no contract. Tail: contract custodies + Merkle-settles"),
  ("Worker payout trust model","",
   "Operator pays its off-chain workers at its own discretion",
   "Head = native direct (canonical); tail = trustless on-chain Merkle claim"),
  ("Validator effort reward","",
   "Native dividends only (stake × vtrust) — effort-agnostic",
   "Dividends + explicit fee-funded, coverage-weighted effort bounty"),
 ]),
 ("NOVEL",[
  ("Miner reward basis / demand coupling","headline bet (now in the tail)",
   "Pure measured work; emission decoupled from real paying demand",
   "deposit × quality (tail) — costly, revenue-backed demand weights pay"),
  ("Miner tiering (head / tail)","the second novel bet",
   "Consolidate behind one UID (Chutes: “never register more than one”); pool the tail",
   "Top-N promoted to own native UID (head) above a trustless pooled tail — tiered"),
 ]),
]

# ---------- geometry ----------
W=1900
MX=36
# columns
ax,aw = MX, 372
bx,bw = ax+aw+12, 588
cx_,cw = bx+bw+12, 588
dx,dw = cx_+cw+12, W-(cx_+cw+12)-MX   # verdict
ROWH=66; GH=42; CH=46
y_title=64; y_sub=92
y_head=140
y0=y_head+CH+6

# compute height
nrows=sum(len(g[1]) for g in DATA); ngrp=len(DATA)
H = y0 + nrows*ROWH + ngrp*GH + 86

# ---------- background ----------
rect(0,0,W,H,fill="#ffffff")
rect(10,10,W-20,H-20,16,fill="none",stroke="#eef2f7",sw=2)

# ---------- title ----------
T(MX,y_title,"UR Subnet vs. the Bittensor norm — design-decision alignment matrix",30,"bold",INK)
T(MX,y_sub,"Where we follow prevailing practice, where we diverge in direction, and our two novel bets. Direction shown side by side; divergence is intentional, not deficiency.",14.5,"normal",SUB)

# ---------- column headers ----------
def colhead(x,w,label):
    T(x+4,y_head+30,label,14,"bold",INK)
T(ax+4,y_head+30,"Design decision",14.5,"bold",INK)
T(bx+4,y_head+30,"Bittensor majority pattern  (the norm’s direction)",14.5,"bold","#334155")
T(cx_+4,y_head+30,"UR Subnet direction",14.5,"bold","#334155")
T(dx+dw/2,y_head+30,"Verdict",14.5,"bold",INK,anchor="middle")
S.append(f'<line x1="{MX}" y1="{y_head+CH-2}" x2="{W-MX}" y2="{y_head+CH-2}" stroke="#cbd5e1" stroke-width="1.6"/>')
# vertical separators (light) — drawn per-row region below

# ---------- rows ----------
y=y0
num=0
for verdict,rows in DATA:
    light,mid,dark=V[verdict]
    # group header band
    rect(MX,y,W-2*MX,GH,8,fill=dark)
    T(MX+16,y+GH/2+5.5,f"{verdict}",15,"bold","white")
    T(MX+16+ (len(verdict)*10.5)+18, y+GH/2+5.5, "—  "+GROUP_BLURB[verdict],13,"normal","#e5e7eb")
    # count on right
    T(W-MX-16,y+GH/2+5.5,f"{len(rows)} decision"+("s" if len(rows)!=1 else ""),12.5,"bold","#e5e7eb",anchor="end")
    y+=GH
    for (title,sub,norm,ours) in rows:
        num+=1
        # row background (light verdict fill) + left accent
        rect(MX,y,W-2*MX,ROWH,0,fill=light)
        rect(MX,y,6,ROWH,0,fill=mid)
        # bottom separator
        S.append(f'<line x1="{MX}" y1="{y+ROWH:.1f}" x2="{W-MX}" y2="{y+ROWH:.1f}" stroke="#ffffff" stroke-width="1.6"/>')
        # subtle column separators
        for sxp in (bx-6,cx_-6,dx-6):
            S.append(f'<line x1="{sxp:.1f}" y1="{y+8:.1f}" x2="{sxp:.1f}" y2="{y+ROWH-8:.1f}" stroke="#ffffff" stroke-width="1.4"/>')
        # decision cell
        T(ax+22,y+27,f"{num}.",13,"bold",mid)
        T(ax+46,y+27,title,15,"bold",INK)
        if sub: T(ax+46,y+46,sub,11.5,"normal",FAINT,italic=True)
        # norm cell
        nl=wrap(norm,66)
        ty=y+ (ROWH-(len(nl)-1)*19)/2 +5
        for i,l in enumerate(nl): T(bx+8,ty+i*19,l,13.5,"normal","#334155")
        # ours cell
        ol=wrap(ours,66)
        ty=y+ (ROWH-(len(ol)-1)*19)/2 +5
        for i,l in enumerate(ol): T(cx_+8,ty+i*19,l,13.5,"500" if False else "normal",INK)
        # verdict chip
        chip(dx+dw/2,y+ROWH/2,verdict,verdict)
        y+=ROWH

# ---------- footer / legend ----------
ly=y+30
T(MX,ly,"At a glance:",13.5,"bold",INK)
lx=MX+108
for v in ("ALIGNED","DIVERGENT","NOVEL"):
    light,mid,dark=V[v]
    rect(lx,ly-13,18,18,4,fill=light,stroke=mid,sw=1.6)
    T(lx+26,ly,f"{v} — {GROUP_BLURB[v]}",12.5,"normal",SUB)
    lx+=26+ (len(v)+len(GROUP_BLURB[v]))*7.0 + 40
T(W-MX,ly,"11 aligned   ·   3 divergent   ·   2 novel",13,"bold","#334155",anchor="end")

# ---------- render ----------
svg='<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">%s</svg>'%(W,H,W,H,"".join(S))
out="/Users/brien/urfoundation/sn/diagrams"
os.makedirs(out,exist_ok=True)
open(os.path.join(out,"comparison_matrix.svg"),"w").write(svg)
cairosvg.svg2png(bytestring=svg.encode(),write_to=os.path.join(out,"comparison_matrix.png"),output_width=W*2,output_height=H*2)
print("wrote",out,"H=",H)
