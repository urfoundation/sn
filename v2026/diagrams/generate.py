#!/usr/bin/env python3
# Generates a detailed SVG diagram of WHITEPAPER.md "## 1. Summary of the mechanism" under
# settlement v2 (VERSION2-FINAL.md) and rasterizes it to PNG with cairosvg.
import math, cairosvg, os

W, H = 1900, 1450
S = []

# ---------- palette ----------
INK="#0f172a"; SUB="#475569"; FAINT="#94a3b8"; WHITE="#ffffff"
BG="#ffffff"
# channel colors: (stroke, fill, dark)
DEP  =("#2563eb","#eff6ff","#1d4ed8")  # deposits  / blue
EMI  =("#0d9488","#ecfdf5","#0f766e")  # emission   / teal-green
SET  =("#7c3aed","#f5f3ff","#6d28d9")  # settlement / purple
EVAL =("#d97706","#fffbeb","#b45309")  # evaluation / amber
BUY  =("#e11d48","#fff1f2","#be123c")  # buyback reserve / rose
HEAD =("#0891b2","#ecfeff","#0e7490")  # head / top-level miners (native, merit) / cyan
OFF  =("#64748b","#f8fafc","#475569")  # off-chain  / slate
CON_BORDER="#334155"; CON_FILL="#f8fafc"; CON_HEAD="#1e293b"
MONO="'Menlo','Monaco',monospace"
SANS="'Helvetica Neue','Helvetica','Arial',sans-serif"

def esc(t):
    return t.replace("&","&amp;").replace("<","&lt;").replace(">","&gt;")

def T(x,y,s,size=13,weight="normal",fill=INK,anchor="start",family=SANS,opacity=1,ls=None):
    extra=f' letter-spacing="{ls}"' if ls is not None else ""
    op=f' opacity="{opacity}"' if opacity!=1 else ""
    S.append(f'<text x="{x:.1f}" y="{y:.1f}" font-family="{family}" font-size="{size}" '
             f'font-weight="{weight}" fill="{fill}" text-anchor="{anchor}"{op}{extra}>{esc(s)}</text>')

def TL(x,y,lines,size=12,weight="normal",fill=INK,anchor="start",family=SANS,lh=None):
    lh = lh if lh else size*1.35
    for i,ln in enumerate(lines):
        T(x,y+i*lh,ln,size,weight,fill,anchor,family)

def shadow(x,y,w,h,rx):
    S.append(f'<rect x="{x+3:.1f}" y="{y+5:.1f}" width="{w}" height="{h}" rx="{rx}" '
             f'fill="#0f172a" opacity="0.07"/>')

def box(x,y,w,h,rx=12,fill=WHITE,stroke=CON_BORDER,sw=1.6,dash=None,sh=True):
    if sh: shadow(x,y,w,h,rx)
    d=f' stroke-dasharray="{dash}"' if dash else ""
    S.append(f'<rect x="{x:.1f}" y="{y:.1f}" width="{w}" height="{h}" rx="{rx}" '
             f'fill="{fill}" stroke="{stroke}" stroke-width="{sw}"{d}/>')

def card(x,y,w,h,col,title,body,rx=13,bsize=12,tsize=15.5,sub=None):
    """Actor card: white with colored left accent + accent top tab + colored title."""
    stroke,fill,dark=col
    shadow(x,y,w,h,rx)
    S.append(f'<rect x="{x:.1f}" y="{y:.1f}" width="{w}" height="{h}" rx="{rx}" '
             f'fill="{WHITE}" stroke="{stroke}" stroke-width="1.8"/>')
    # left accent bar
    S.append(f'<path d="M {x+5:.1f} {y+rx:.1f} a {rx-5} {rx-5} 0 0 1 {rx-5} {-(rx-5)} '
             f'L {x+w-rx:.1f} {y+5:.1f}" fill="none"/>')  # placeholder noop
    # accent header strip
    S.append(f'<path d="M {x:.1f} {y+rx:.1f} Q {x:.1f} {y:.1f} {x+rx:.1f} {y:.1f} '
             f'L {x+w-rx:.1f} {y:.1f} Q {x+w:.1f} {y:.1f} {x+w:.1f} {y+rx:.1f} '
             f'L {x+w:.1f} {y+30:.1f} L {x:.1f} {y+30:.1f} Z" fill="{fill}"/>')
    S.append(f'<line x1="{x:.1f}" y1="{y+30:.1f}" x2="{x+w:.1f}" y2="{y+30:.1f}" stroke="{stroke}" stroke-width="1"/>')
    T(x+14,y+21,title,tsize,"bold",dark)
    if sub: T(x+w-12,y+21,sub,11.5,"bold",stroke,anchor="end")
    TL(x+14,y+50,body,bsize,"normal",INK,lh=bsize*1.42)

def circnum(cx,cy,n,color,r=13):
    S.append(f'<circle cx="{cx:.1f}" cy="{cy:.1f}" r="{r}" fill="{color}" stroke="white" stroke-width="2"/>')
    T(cx,cy+r*0.36,str(n),r*1.05,"bold","white",anchor="middle")

def arrowhead(x,y,ang,color,size=12):
    a1=ang+math.radians(150); a2=ang-math.radians(150)
    p1=(x+size*math.cos(a1), y+size*math.sin(a1))
    p2=(x+size*math.cos(a2), y+size*math.sin(a2))
    S.append(f'<path d="M {x:.1f} {y:.1f} L {p1[0]:.1f} {p1[1]:.1f} L {p2[0]:.1f} {p2[1]:.1f} Z" fill="{color}"/>')

def la(x1,y1,x2,y2,color,sw=2.4,dash=None,head=True,hs=12):
    d=f' stroke-dasharray="{dash}"' if dash else ""
    ang=math.atan2(y2-y1,x2-x1)
    # pull line back so it ends at arrowhead base
    bx,by=(x2-(hs*0.7)*math.cos(ang), y2-(hs*0.7)*math.sin(ang)) if head else (x2,y2)
    S.append(f'<line x1="{x1:.1f}" y1="{y1:.1f}" x2="{bx:.1f}" y2="{by:.1f}" '
             f'stroke="{color}" stroke-width="{sw}" stroke-linecap="round"{d}/>')
    if head: arrowhead(x2,y2,ang,color,hs)

def elbow(pts,color,sw=2.4,dash=None,head=True,hs=12):
    d=f' stroke-dasharray="{dash}"' if dash else ""
    p=" ".join(f"{x:.1f},{y:.1f}" for x,y in pts)
    S.append(f'<polyline points="{p}" fill="none" stroke="{color}" stroke-width="{sw}" '
             f'stroke-linejoin="round" stroke-linecap="round"{d}/>')
    if head:
        (x1,y1),(x2,y2)=pts[-2],pts[-1]
        arrowhead(x2,y2,math.atan2(y2-y1,x2-x1),color,hs)

def curve(x1,y1,c1x,c1y,c2x,c2y,x2,y2,color,sw=2.4,dash=None,head=True,hs=13):
    d=f' stroke-dasharray="{dash}"' if dash else ""
    S.append(f'<path d="M {x1:.1f} {y1:.1f} C {c1x:.1f} {c1y:.1f} {c2x:.1f} {c2y:.1f} {x2:.1f} {y2:.1f}" '
             f'fill="none" stroke="{color}" stroke-width="{sw}" stroke-linecap="round"{d}/>')
    if head: arrowhead(x2,y2,math.atan2(y2-c2y,x2-c2x),color,hs)

def pill(cx,cy,lines,size=12,tc=INK,bc=None,weight="normal",pad=8,family=SANS,mono=False):
    f=0.62 if mono else 0.545
    wmax=max(len(l) for l in lines)
    w=wmax*size*f+pad*2
    lh=size*1.32
    h=len(lines)*lh+pad*1.5
    x=cx-w/2; y=cy-h/2
    bc=bc if bc else "#e2e8f0"
    S.append(f'<rect x="{x:.1f}" y="{y:.1f}" width="{w:.1f}" height="{h:.1f}" rx="7" '
             f'fill="white" stroke="{bc}" stroke-width="1.1" opacity="0.97"/>')
    ty=y+pad+size*0.82
    for i,l in enumerate(lines):
        T(cx,ty+i*lh,l,size,weight,tc,anchor="middle",family=family)
    return w,h

# v2 channel aliases (same palette families as release 1.0's figure)
LED = DEP   # ledger data: provider rows, accounts, roots (blue)
DIS = BUY   # disputes: challenges and path demands (rose)
TRE = ("#65a30d","#f7fee7","#4d7c0f")  # treasury row, ur-reserve (lime)

def halo_elbow(pts,color,sw=2.6,dash=None,head=True,hs=12):
    """An elbow drawn over a white halo, so it reads as passing over a crossing line."""
    p=" ".join(f"{x:.1f},{y:.1f}" for x,y in pts)
    S.append(f'<polyline points="{p}" fill="none" stroke="white" stroke-width="{sw+5}" '
             f'stroke-linejoin="round" stroke-linecap="round"/>')
    elbow(pts,color,sw,dash,head,hs)

# ================= BACKGROUND =================
S.append(f'<rect width="{W}" height="{H}" fill="{BG}"/>')
S.append(f'<rect x="8" y="8" width="{W-16}" height="{H-16}" rx="16" fill="none" stroke="#eef2f7" stroke-width="2"/>')

# ================= TITLE =================
T(40,58,"UR Subnet — Mechanism at a glance  (settlement v2)",30,"bold",INK)
T(40,86,"From E_s the pool tier is paid by verified usage into cumulative, never-expiring coldkey accounts: one Merkle sum-tree root per pool each epoch, approved by auditors and final after a 14,400-block challenge window.  §1, §6, §8.2–8.5.",14.5,"normal",SUB)
S.append(f'<line x1="40" y1="104" x2="{W-40}" y2="104" stroke="#e2e8f0" stroke-width="1.5"/>')

# ================= TOP ROW =================
card(40,150,260,92,OFF,"Customers",["send paid VPN / privacy","traffic — the real demand"])

# Legend
lg_x,lg_y,lg_w,lg_h=330,124,380,334
box(lg_x,lg_y,lg_w,lg_h,12,"#ffffff","#cbd5e1",1.4)
T(lg_x+16,lg_y+26,"Flows  (on-chain in α)",14,"bold",INK)
rows=[("1",EMI[0],"Emission (Yuma)","the 41% miner row: head / pool / treasury",False),
      ("2",EMI[2],"Capture","pool hotkey -> vault2 escrow at the close",True),
      ("3",LED[0],"Ledger root","rows -> accounts -> sum tree -> commit",False),
      ("4",EVAL[0],"Audit + approval","auditors approve the exact root",False),
      ("5",DIS[0],"Challenge / demand","anyone; demands within 14,400 blocks",False),
      ("6",SET[0],"Claim","cumulative, to the leaf's coldkey",False),
      (None,HEAD[0],"Head (native)","routable-IP breadth, own hotkey",False),
      (None,TRE[0],"Treasury","1 − μ to ur-reserve (SN25)",False),
      (None,OFF[0],"Off-chain","revenue ($) to the operator",True)]
ry=lg_y+52
for num,c,name,desc,dash in rows:
    da=' stroke-dasharray="6 4"' if dash else ''
    if num: circnum(lg_x+28,ry,num,c,r=10)
    S.append(f'<line x1="{lg_x+46}" y1="{ry}" x2="{lg_x+76}" y2="{ry}" stroke="{c}" stroke-width="3.6" stroke-linecap="round"{da}/>')
    arrowhead(lg_x+76,ry,0,c,9)
    T(lg_x+90,ry-3,name,12.5,"bold",INK)
    T(lg_x+90,ry+12,desc,11,"normal",SUB)
    ry+=30

# Coinbase with the standard split
card(756,138,470,134,EMI,"Bittensor coinbase  (α emission)",[],tsize=15.5)
sb_x,sb_y,sb_w,sb_h=770,196,442,30
segs=[("owner 18%",0.18,OFF[0]),("miner 41%",0.41,EMI[0]),("validator 41%",0.41,EVAL[0])]
cx=sb_x
for lab,frac,c in segs:
    wseg=sb_w*frac
    S.append(f'<rect x="{cx:.1f}" y="{sb_y}" width="{wseg:.1f}" height="{sb_h}" fill="{c}" opacity="0.9"/>')
    T(cx+wseg/2,sb_y+sb_h*0.66,lab,11.5,"bold","white",anchor="middle")
    cx+=wseg
S.append(f'<rect x="{sb_x}" y="{sb_y}" width="{sb_w}" height="{sb_h}" rx="4" fill="none" stroke="{EMI[0]}" stroke-width="1.2"/>')
T(770,250,"standard 18 / 41 / 41 split — we do not fight the coinbase",11.5,"italic",SUB)

card(1560,150,300,86,OFF,"Subnet owner",
     ["18% owner cut. Runs the majority","validator early (UID 1 on SN25)."],bsize=11.5)
card(1560,256,300,200,EVAL,"Validators  (the auditor at launch)",
     ["stake own α; native dividends","(stake × vtrust) — unchanged.",
      "/verify trails -> quality Qn (pools)","and routable-IP breadth (head).",
      "audit each pending root from its","published objects; approve with the",
      "auditor key, or challenge / demand.","commit–reveal · clip · vtrust"],bsize=11)

# Yuma + the miner row
card(1560,472,300,222,EVAL,"Yuma  +  the miner row",
     ["stake-weighted median + clip over","commit–reveal weights. The 41% miner","row is one weight vector:"],bsize=11,tsize=15)
for i,(a,b,c) in enumerate([("head    ","θ·μ    ","top-miner UIDs"),
                            ("pool    ","(1−θ)·μ","v2 pool UID"),
                            ("treasury","1 − μ  ","ur-reserve")]):
    T(1576,582+i*16,a+"  "+b+"  "+c,11,"bold",INK,family=MONO)
TL(1574,640,["SN25: μ = 1/10 (signed treasury policy),","θ = 3/10; an empty tier cedes its share;","pools split (1−θ)·μ by implied_usage × Qn."],10.5,"normal",SUB,lh=15)

# v2 pool UID (tail)
card(1250,540,250,104,EMI,"v2 pool UID",
     ["pool hotkey fixed by immutable vault2;","(1−θ)·μ accrues here as stake;","weighted from E_s (settlementAt)"],bsize=11,sub="TAIL")

# Treasury
card(1700,712,160,108,TRE,"ur-reserve",["treasury row 1 − μ","UIDs 170 and 250","2-of-3 multisig"],bsize=10.5,tsize=14)

# Head (unchanged)
card(1240,840,620,176,HEAD,"Top-level miner UIDs   (head, ~200)  — unchanged",
     ["the top fleets by split-adjusted routable egress-IP breadth (D27) each hold",
      "their OWN UID: NATIVE emission to the fleet's hotkey — no contract custody,",
      "no ledger, no claim. client_id <-> hotkey binding (§11.4): a bound client's",
      "pool row is flagged head_excluded (at either boundary): no pool share."],bsize=11.5,sub="HEAD")
S.append(f'<rect x="1256" y="966" width="470" height="32" rx="6" fill="white" stroke="{HEAD[0]}" stroke-width="1.3"/>')
T(1266,987,"weight = IP score",12.5,"bold",HEAD[2],family=MONO)
T(1416,987,"— its EMA; no deposit or quality term (D27)",10.5,"italic",SUB)

# ================= LEFT: OPERATOR =================
card(40,480,330,320,LED,"Network Operator  (UR server)",
     ["runs privacy servers + /verify;","settles traffic contracts, records usage.","",
      "each epoch, as root signer and publisher:",
      "· rows: client_id, consent-selected",
      "  coldkey, usage bytes, head flag",
      "· allocate each epoch's funding by",
      "  coldkey usage (largest remainder)",
      "· add to cumulative accounts ->",
      "  Merkle sum-tree root -> commit",
      "· publish the ledger objects; answer",
      "  every path demand on chain",
      "","usage, population and wallet choice are","attested, not yet checkable (phase A);",
      "never holds anyone's α"],bsize=11.5)

# ================= CENTER: SETTLEMENT V2 CONTRACTS =================
cX,cY,cW,cH=430,500,760,472
box(cX,cY,cW,cH,14,CON_FILL,CON_BORDER,2.0)
S.append(f'<path d="M {cX} {cY+14} Q {cX} {cY} {cX+14} {cY} L {cX+cW-14} {cY} '
         f'Q {cX+cW} {cY} {cX+cW} {cY+14} L {cX+cW} {cY+46} L {cX} {cY+46} Z" fill="{CON_HEAD}"/>')
T(cX+18,cY+30,"SETTLEMENT V2",16.5,"bold","white")
T(cX+168,cY+30,"— Subtensor EVM, beside the unchanged v1 contracts",12,"normal","#cbd5e1")
T(cX+cW-16,cY+30,"owner: governance Safe · a pause never stops claims",11,"italic","#94a3b8",anchor="end")

# STSettlement2 band
bx,by,bw,bh=cX+16,cY+58,cW-32,108
box(bx,by,bw,bh,10,"#f1f5f9","#475569",1.5,sh=False)
T(bx+12,by+22,"STSettlement2",13.5,"bold",INK)
T(bx+126,by+22,"— settlement coordinator  (UUPS proxy, upgradeable by the Safe)",11.5,"italic",SUB)
TL(bx+12,by+46,["· auditor set + quorum (1 at launch); approvals","· registerOperatorPool (Safe) -> firstEpoch = E_s"],11.5,"normal",INK,lh=17)
TL(bx+384,by+46,["· settlementAt: v1 before E_s, v2 from E_s","· windows from the v1 policy (epoch 50,400 blocks)"],11.5,"normal",INK,lh=17)
T(bx+12,by+97,"· a pause (guardian or Safe; only the Safe unpauses) stops close, commit, finalize — never claims, approvals or disputes",11.5,"normal",INK)

# vault2
vx,vy,vw,vh=cX+16,by+bh+12,cW-32,cY+cH-(by+bh+12)-14
box(vx,vy,vw,vh,10,"#ffffff",CON_BORDER,1.6,sh=False)
T(vx+12,vy+22,"STSettlementVault2  (vault2) — IMMUTABLE custody",13.5,"bold",INK)
T(vx+vw-12,vy+22,"G1–G6 hold whatever STSettlement2 does",11,"italic",SUB,anchor="end")
gx0=vx+16; gy0=vy+36; gg=12
gw=(vw-32-gg)/2; gh=(vh-36-16-gg)/2
def comp(ix,iy,col,title,lines):
    x=gx0+ix*(gw+gg); y=gy0+iy*(gh+gg)
    box(x,y,gw,gh,9,col[1],col[0],1.4,sh=False)
    T(x+11,y+21,title,12.5,"bold",col[2])
    TL(x+11,y+41,lines,11,"normal",INK,lh=16)
    return x,y
lr=comp(0,0,LED,"Per-pool ledger roots",["Merkle SUM tree over accounts at fixed","positions; one pending root per pool;","every finalized root is kept (G4);","root total = captured funding (G5)"])
es=comp(1,0,EMI,"Escrow  — one hotkey",["closeOperatorEpoch captures the pool","hotkey's movable stake (moveStake);","captured = paid + escrow (G1)"])
cl=comp(0,1,SET,"Cumulative claims",["one claim pays everything accrued since","the last, only to the leaf's coldkey","(G3); anyone may send; no expiry;","never paused; SN25's transfer switch can freeze"])
wd=comp(1,1,DIS,"14,400-block window  (≈ 2 days)",["checkPath: a proven fault deletes the root (G2);","demandPath forces an opening (750,000-rao fee);","finalize: approved, window over, no open demand (G6)"])

# ================= BOTTOM ROW =================
card(40,1030,500,184,SET,"Providers — the pool tier  (TAIL, ≥ 1M per pool by design)",
     ["carry traffic inside a NO's pool — no UID, no registration burn.",
      "paid in proportion to verified usage only: no reliability factor,",
      "no measurement gate. One account per payout coldkey per pool:",
      "(coldkey, cumulative) — never decreases, never expires.",
      "paid only under a signed payout consent (own, network's, delegation);",
      "claim anytime: the apps / SDK, snclaim, or the claim daemon.",
      "no timing promise: a claim costs ≈ 0.19 α of gas at 5 gwei."],bsize=11.5)

# v1 note (dashed)
nx,ny,nw,nh=560,1002,250,150
box(nx,ny,nw,nh,12,"#ffffff",OFF[0],1.5,dash="6 4")
T(nx+14,ny+24,"v1 contracts — unchanged",13.5,"bold",OFF[2])
TL(nx+14,ny+46,["ST coordinator + v1 vault pay every","epoch before E_s (bps roots, TTL).",
                "Deposits stay conviction stake ->","buyback reserve (§7.4), dormant at",
                "zero price. Phase C sweeps v1 carry","into vault2."],11,"normal",INK,lh=15.5)

card(830,1002,330,150,DIS,"Watchers  — anyone",
     ["rebuild each pending root from the published",
      "objects; check every account of the newest",
      "finalized root they hold:",
      "· fault -> checkPath deletes the root",
      "· data withheld -> demandPath: a value-weighted",
      "  sample of ≥ 256, doubling per failed root"],bsize=11,tsize=15)

# ================= EDGES =================
# customers -> operator (off-chain revenue)
la(170,242,170,478,OFF[0],2.4,dash="6 5")
pill(170,360,["usage revenue ($)","off-chain reference rate"],11,SUB,OFF[0])

# 1) coinbase miner 41% -> Yuma
elbow([(940,272),(940,314),(1500,314),(1500,520),(1558,520)],EMI[0],2.8)
pill(1232,314,["41% miner emission"],11,EMI[2],EMI[0],weight="bold")
circnum(940,294,"1",EMI[0])
# coinbase -> validators (native) and owner
elbow([(1226,212),(1492,212),(1492,300),(1558,300)],EMI[0],2.4)
pill(1392,212,["41% validator","emission (native)"],10.5,EMI[2],EMI[0])
elbow([(1226,162),(1512,162),(1512,186),(1558,186)],EMI[0],2.2)
pill(1440,158,["18%"],11,EMI[2],EMI[0],weight="bold")

# validators -> Yuma: the weight vector
la(1710,456,1710,471,EVAL[0],2.6,hs=8)
T(1700,468,"weights",10.5,"bold",EVAL[2],anchor="end")

# Yuma -> v2 pool UID, head, treasury
la(1558,600,1502,600,EMI[0],2.8,hs=11)
T(1530,592,"(1−θ)·μ",10.5,"bold",EMI[2],anchor="middle")
la(1620,696,1620,838,HEAD[0],3.0,hs=11)
T(1630,770,"θ·μ  (head)",12.5,"bold",HEAD[2])
la(1780,696,1780,710,TRE[0],2.8,hs=9)
T(1790,708,"1 − μ",11,"bold",TRE[2])

# 2) capture: pool hotkey -> vault2 escrow, at the close
cap_y=es[1]+gh*0.55
elbow([(1375,644),(1375,cap_y),(es[0]+gw+2,cap_y)],EMI[2],2.8,dash="8 5")
circnum(1375,690,"2",EMI[2])
pill(1290,cap_y-16,["capture at the close"],10.5,EMI[2],EMI[0],weight="bold")

# 3) operator -> STSettlement2: close + commit the root
la(372,by+46,bx-2,by+46,LED[0],2.8)
circnum(404,by+46,"3",LED[0])
T(400,by+74,"close +",10.5,"bold",LED[2],anchor="middle")
T(400,by+88,"commit",10.5,"bold",LED[2],anchor="middle")

# 4) validators -> STSettlement2: approveDistribution (auditor key)
halo_elbow([(1558,436),(1210,436),(1210,by+46),(bx+bw+2,by+46)],EVAL[0],2.6)
circnum(1210,470,"4",EVAL[0])
pill(1385,436,["approve (auditor key)"],10.5,EVAL[2],EVAL[0],weight="bold")

# 5) watchers -> vault2 window: challenge / demand
la(995,1000,995,wd[1]+gh+2,DIS[0],2.8)
circnum(995,986,"5",DIS[0])
pill(1085,986,["checkPath / demandPath"],10.5,DIS[2],DIS[0],weight="bold")

# 6) claims -> providers
elbow([(cl[0]+90,cl[1]+gh),(cl[0]+90,990),(300,990),(300,1028)],SET[0],2.8)
circnum(cl[0]+90,cl[1]+gh+16,"6",SET[0])
pill(400,990,["claim: pays the leaf's coldkey"],10.5,SET[2],SET[0],weight="bold")

# provider lifecycle: pool -> graduate to a head slot -> fall back
curve(542,1190,860,1250,1180,1214,1300,1018,HEAD[0],2.4,dash="2 7")
arrowhead(542,1190,math.atan2(1190-1250,542-860),HEAD[0],12)
pill(905,1222,["a provider starts in a pool, GRADUATES to a head slot, and FALLS BACK if its breadth slips"],10.5,HEAD[2],HEAD[0],weight="bold")

# ================= EPOCH TIMELINE =================
tl_x,tl_y,tl_w,tl_h=40,1244,1820,112
box(tl_x,tl_y,tl_w,tl_h,12,"#f8fafc","#cbd5e1",1.4)
T(tl_x+16,tl_y+24,"Each pool epoch e, from E_s",14,"bold",INK)
T(tl_x+226,tl_y+24,"— epoch = 50,400 blocks (≈ 7 days); E_s = the first epoch starting at least two days after the last activation gate (SN25 plan: epoch 3, ≈ 2026-10-23) · missed close -> later earners; missed root -> next root",11,"italic",SUB)
steps=[(EMI,"end(e)","the pool hotkey holds the epoch's (1−θ)·μ"),
       (LED,"close (anyone) + capture · commit","within 1,200 blocks of end(e)"),
       (EVAL,"audit + approve","auditors, from the commit to end(e+1)"),
       (DIS,"challenge / demand window","14,400 blocks from the commit"),
       (SET,"finalize  (anyone)","approved · window over · no open demand"),
       (SET,"claimable","cumulative — never expires")]
sw_,sg=268,34
sx=tl_x+16
for i,(col,a,b) in enumerate(steps):
    box(sx,tl_y+40,sw_,56,9,col[1],col[0],1.4,sh=False)
    T(sx+sw_/2,tl_y+62,a,12.5,"bold",col[2],anchor="middle")
    T(sx+sw_/2,tl_y+81,b,11,"normal",INK,anchor="middle")
    if i<len(steps)-1:
        la(sx+sw_+3,tl_y+68,sx+sw_+sg-3,tl_y+68,"#94a3b8",2.2,hs=9)
    sx+=sw_+sg

# key-insight banner
kb_x,kb_y,kb_w,kb_h=40,1366,1820,64
box(kb_x,kb_y,kb_w,kb_h,12,"#ffffff","#cbd5e1",1.4)
T(kb_x+kb_w/2,kb_y+26,"pool = verified-usage share, cumulative per coldkey    ·    head = routable-IP breadth, native    ·    SN25 treasury row 1 − μ = 9/10 to ur-reserve",
  12.5,"bold",INK,anchor="middle")
T(kb_x+kb_w/2,kb_y+47,"vault2 is immutable: accrued balances survive a bad STSettlement2 upgrade or a compromised root signer and auditor, as long as each fault is challenged or its account demanded within the window (VERSION2-FINAL §4.2)",
  11,"italic",SUB,anchor="middle")

# ================= RENDER =================
svg='<svg xmlns="http://www.w3.org/2000/svg" xml:space="preserve" width="%d" height="%d" viewBox="0 0 %d %d">%s</svg>'%(W,H,W,H,"".join(S))
out_dir=os.path.dirname(os.path.abspath(__file__))
open(os.path.join(out_dir,"mechanism.svg"),"w").write(svg)
cairosvg.svg2png(bytestring=svg.encode(),write_to=os.path.join(out_dir,"mechanism.png"),output_width=W*2,output_height=H*2)
print("wrote",out_dir)
