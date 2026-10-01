#!/usr/bin/env python3
# Generates a detailed SVG diagram of WHITEPAPER.md "## 1. Summary of the mechanism"
# and rasterizes to PNG with cairosvg.
import math, cairosvg, os

W, H = 1900, 1290
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

# ================= BACKGROUND =================
S.append(f'<rect width="{W}" height="{H}" fill="{BG}"/>')
# subtle frame
S.append(f'<rect x="8" y="8" width="{W-16}" height="{H-16}" rx="16" fill="none" stroke="#eef2f7" stroke-width="2"/>')

# ================= TITLE =================
T(40,58,"UR Subnet — Mechanism at a glance",30,"bold",INK)
T(40,86,"Two miner tiers in ONE mechanism (41% miner emission split by θ: routable-IP-breadth HEAD / implied_usage x Qn TAIL) — and every deposit is CONVICTION STAKE: locked in a buyback reserve, never distributed.  §1, §7.4, §8.4–8.5.",14.5,"normal",SUB)
S.append(f'<line x1="40" y1="104" x2="{W-40}" y2="104" stroke="#e2e8f0" stroke-width="1.5"/>')

# ================= NODES =================
# Customers (top-left)
card(40,150,250,92,OFF,"Customers",["carries paid VPN / privacy","traffic — the real demand"])
# Network Operator (left)
card(40,372,300,184,DEP,"Network Operator (NO)",
     ["runs privacy servers + the","/verify server (co-signs trails)","","holds NO α only — never holds","others' emission; directs the split"],bsize=12)
# Providers — the pool tier / TAIL recipients (bottom-left)
card(40,1000,470,152,SET,"Providers — the pool tier  (TAIL, 100k+)",
     ["carry ingress / egress traffic inside a NO's pool — NOT on-chain UIDs",
      "(subnet cap ≈ 256), so each is a client_id paid by Merkle claim.",
      "claim α directly from the contract with an O(log N) proof.",
      "a provider STARTS here (the low-barrier on-ramp / baseline)."],bsize=11.5)
# Coinbase (top center)
card(756,138,470,134,EMI,"Bittensor coinbase  (α emission)",[],tsize=15.5)
# emission split bar inside coinbase
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
# Owner (top-right)
card(1560,150,300,86,OFF,"Subnet owner",
     ["18% owner cut. Runs the majority","validator early; the reserve backs it."],bsize=11.5)
# Independent validators (right)
card(1560,256,300,178,EVAL,"Validators  (owner-majority v1)",
     ["anyone who stakes own α, runs","/verify trails — no NO, no pool;","scores BOTH miner tiers.","native dividends (stake × vtrust);","effort bounty: out of scope (D29).","anti-gaming ON: commit–reveal,","clip + vtrust · self-mask · bonds"],bsize=11)
# Yuma + theta split node (right, below validators) — the heart of the split
card(1560,454,300,188,EVAL,"Yuma  +  θ split",
     ["stake-weighted median + clip +","vtrust over commit–reveal weights","— real, independent consensus.","",
      "it allocates the 41% miner emission","across the two tiers of UIDs:","   1−θ  to NO pool UIDs  (tail)","   θ    to top-miner UIDs (head)"],bsize=10.5,tsize=15)

# Top-level miner UIDs — the HEAD channel (bottom-right, wide, cyan accent)
card(1300,662,560,210,HEAD,"Top-level miner UIDs   (~200)",
     ["the top ~200 providers — each claims its OWN miner UID: the canonical",
      "Bittensor treatment, MORE trust-minimized than the pool (no operator in path).",
      "NATIVE emission straight to the provider's own hotkey each tempo —",
      "no contract custody · no Merkle claim · no NO take, not shared.",
      "identity: client_id <-> hotkey binding (§11.4) — dual-signed, fail-closed."],bsize=11.5,sub="HEAD")
# head weight highlight
S.append(f'<rect x="1316" y="822" width="424" height="32" rx="6" fill="white" stroke="{HEAD[0]}" stroke-width="1.3"/>')
T(1326,843,"weight = IP score",12.5,"bold",HEAD[2],family=MONO)
T(1476,843,"— split-adjusted routable egress IPs, NO deposit term (D27)",10.5,"italic",SUB)

# Legend (top-center)
lg_x,lg_y,lg_w,lg_h=372,128,352,288
box(lg_x,lg_y,lg_w,lg_h,12,"#ffffff","#cbd5e1",1.4)
T(lg_x+16,lg_y+26,"Flows  (all in α)",14,"bold",INK)
rows=[("1",DEP[0],"Deposits","conviction stake (from events)",False),
      ("2",EMI[0],"Emission (Yuma)","split θ head / 1−θ tail",False),
      ("3",SET[0],"Settlement","per-NO Merkle claims (tail)",False),
      (None,HEAD[0],"Top-level miners","native, routable-IP score (head)",False),
      (None,EVAL[0],"Evaluation / quality","VALIDATOR.md /verify trails",True),
      (None,BUY[0],"Buyback reserve","deposits staked + locked (§7.4)",False),
      (None,OFF[0],"Off-chain","revenue & operations",True)]
ry=lg_y+54
for num,c,name,desc,dash in rows:
    da=' stroke-dasharray="6 4"' if dash else ''
    if num: circnum(lg_x+28,ry,num,c,r=10)
    S.append(f'<line x1="{lg_x+46}" y1="{ry}" x2="{lg_x+76}" y2="{ry}" stroke="{c}" stroke-width="3.6" stroke-linecap="round"{da}/>')
    arrowhead(lg_x+76,ry,0,c,9)
    T(lg_x+90,ry-3,name,12.5,"bold",INK)
    T(lg_x+90,ry+13,desc,11,"normal",SUB)
    ry+=33

# ================= CONTRACT (center hub — the TAIL custody) =================
cX,cY,cW,cH=440,460,720,372
box(cX,cY,cW,cH,14,CON_FILL,CON_BORDER,2.0)
# header
S.append(f'<path d="M {cX} {cY+14} Q {cX} {cY} {cX+14} {cY} L {cX+cW-14} {cY} '
         f'Q {cX+cW} {cY} {cX+cW} {cY+14} L {cX+cW} {cY+46} L {cX} {cY+46} Z" fill="{CON_HEAD}"/>')
T(cX+18,cY+30,"ST CONTRACT",16.5,"bold","white")
T(cX+150,cY+30,"— Subtensor EVM · custodian + 7-day settlement",12,"normal","#cbd5e1")
T(cX+cW-16,cY+30,"owns the pool UIDs (TAIL only)",11,"italic","#94a3b8",anchor="end")

# compartments
pad=16; gut=16
iy=cY+58; ih1=140; ih2=126
iw=(cW-2*pad-gut)/2
Lx=cX+pad; Rx=cX+pad+iw+gut
# L-top: deposit ledger
box(Lx,iy,iw,ih1,10,DEP[1],DEP[0],1.5,sh=False)
T(Lx+12,iy+24,"Deposits  (conviction stake)",13.5,"bold",DEP[2])
TL(Lx+12,iy+46,["NO deposit -> Deposited event + full","amount into the reserve. NO DT ledger:",
                "the contract weighs nothing (D25).","validators read deposits from events."],11.5,"normal",INK,lh=17)
T(Lx+12,iy+ih1-12,"cumulative locked α sets the NO's rate tier",10.5,"italic",DEP[2])
# R-top: miner-pool UIDs (TAIL)
box(Rx,iy,iw,ih1,10,EMI[1],EMI[0],1.5,sh=False)
T(Rx+12,iy+24,"Miner-pool UIDs  (one per NO) — TAIL",12.5,"bold",EMI[2])
TL(Rx+12,iy+46,["contract-owned accrual slots — no","emission ever touches a NO's keys.","",
                "validator weight  w_n = implied_usage x Qn"],11.5,"normal",INK,lh=17)
# emphasize formula
S.append(f'<rect x="{Rx+10}" y="{iy+ih1-40}" width="{iw-20}" height="26" rx="5" fill="white" stroke="{EMI[0]}" stroke-width="1.2"/>')
T(Rx+18,iy+ih1-22,"implied_usage x Qn",12,"bold",EMI[2],family=MONO)
T(Rx+iw-14,iy+ih1-22,"impl = dep / tier-rate",10.5,"italic",SUB,anchor="end")
# L-bot: merkle roots
iy2=iy+ih1+gut
box(Lx,iy2,iw,ih2,10,SET[1],SET[0],1.5,sh=False)
T(Lx+12,iy2+24,"Per-NO Merkle payout roots",13.5,"bold",SET[2])
TL(Lx+12,iy2+46,["pool = earned emission ONLY (§8.3).","NO commits a payout root each epoch;",
                 "it directs the split but never holds α."],11.5,"normal",INK,lh=17)
# R-bot: buyback reserve
box(Rx,iy2,iw,ih2,10,BUY[1],BUY[0],1.5,sh=False)
T(Rx+12,iy2+24,"BUYBACK RESERVE  (§7.4)",13.5,"bold",BUY[2])
TL(Rx+12,iy2+46,["every deposit, in full — staked to the","owner-validator hotkey. LOCKED (no",
                 "exit path) · dividends auto-compound."],11.5,"normal",INK,lh=17)

# ================= EDGES =================
# 1) customers -> NO   (off-chain revenue)
la(165,242,165,372,OFF[0],2.4)
pill(165,307,["usage revenue ($)","off-chain reference rate"],11,SUB,OFF[0])

# 2) NO -> deposit ledger  (CHANNEL 1)
elbow([(340,470),(396,470),(396,iy+70),(Lx-2,iy+70)],DEP[0],2.8)
T(396,452,"deposit α — conviction stake",11,"bold",DEP[2],anchor="middle")
circnum(396,470,"1",DEP[0])
# 2b) deposit ledger -> buyback reserve (full amount, internal moveStake)
la(Lx+iw*0.62,iy+ih1,Rx+iw*0.30,iy2-2,BUY[0],2.6)
pill((Lx+iw*0.62+Rx+iw*0.30)/2,iy+ih1+7,["full amount -> reserve"],10.5,BUY[2],BUY[0],weight="bold")

# 3) coinbase 41% miner -> Yuma/theta node  (CHANNEL 2 emission)
elbow([(940,272),(940,314),(1500,314),(1500,512),(1560-2,512)],EMI[0],2.8)
pill(1232,314,["41% miner emission"],11,EMI[2],EMI[0],weight="bold")
circnum(940,294,"2",EMI[0])

# 4) node -> miner-pool UIDs  (1-theta, tail emission)
elbow([(1560,600),(1330,600),(1330,iy+ih1*0.5),(Rx+iw+2,iy+ih1*0.5)],EMI[0],2.6)
T(1452,592,"1−θ to pool UIDs (tail)",11,"bold",EMI[2],anchor="middle")

# 5) node -> top-level miner UIDs  (theta, head emission, NATIVE)
la(1700,640,1700,664,HEAD[0],3.0,hs=11)
T(1718,660,"θ  (head)",13,"bold",HEAD[2])

# 6) coinbase -> validators (native validator emission)
elbow([(1226,212),(1492,212),(1492,300),(1560-2,300)],EMI[0],2.4)
pill(1392,212,["41% validator","emission (native)"],10.5,EMI[2],EMI[0])

# 7) coinbase -> owner (18%)
elbow([(1226,162),(1512,162),(1512,186),(1560-2,186)],EMI[0],2.2)
pill(1440,158,["18%"],11,EMI[2],EMI[0],weight="bold")

# 8) buyback reserve -> owner-validator hotkey (staked; compounds)
la(Rx+iw,iy2+10,1560-2,430,BUY[0],2.6)
pill(1378,560,["staked to the owner-validator hotkey","locked · dividends auto-compound (take 0)"],10.5,BUY[2],BUY[0],weight="bold")

# 9) merkle roots -> providers (CHANNEL 3 settlement, tail)
elbow([(Lx+iw*0.4,iy2+ih2),(Lx+iw*0.4,930),(300,930),(300,1000-2)],SET[0],2.8)
pill(300,966,["claim α directly","O(log N) Merkle proof"],11,SET[2],SET[0],weight="bold")
circnum(Lx+iw*0.4,iy2+ih2-2,"3",SET[0])

# 10) NO -> providers (runs server, commits root)
elbow([(160,556),(160,1000-2)],OFF[0],2.2,dash="6 5")
pill(160,760,["/verify server","commits payout root","(never holds α)"],10.5,SUB,OFF[0])

# 11) validators -> providers (measurement trails, the core signal) big arc
# routed through the contract<->head gap, then below the contract, to clear both cards
curve(1600,442,1210,720,680,1055,512,1002,EVAL[0],2.6,dash="7 5")
pill(806,930,["VALIDATOR.md /verify trails: walk provider chains,","measuring liveness/quality (Qn) + egress-IP breadth (head)"],11,EVAL[2],EVAL[0])

# 12) top-level miner UIDs -> own hotkey (native, direct)
la(1580,872,1580,910,HEAD[0],2.8)
pill(1580,932,["a top provider's own coldkey","paid directly — no NO middleman, trust-minimized"],10.5,HEAD[2],HEAD[0],weight="bold")

# 13) provider lifecycle: pool -> graduate to top slot -> fall back
curve(516,1058,910,1206,1150,1010,1322,858,HEAD[0],2.4,dash="2 7")
pill(936,1196,["a provider starts in a pool, GRADUATES to a top slot, and FALLS BACK if quality slips"],11,HEAD[2],HEAD[0],weight="bold")

# key-insight callout (bottom banner)
kb_x,kb_y,kb_w,kb_h=470,1212,960,66
box(kb_x,kb_y,kb_w,kb_h,12,"#f8fafc","#cbd5e1",1.4)
T(kb_x+kb_w/2,kb_y+27,"pool = implied_usage x quality (baseline)    ·    head = routable-IP breadth, native (merit apex)    ·    both tiers paid from EMISSION ONLY",
  12.5,"bold",INK,anchor="middle")
T(kb_x+kb_w/2,kb_y+48,"deposits are conviction stake (read from events, no DT ledger); validators weight the pools; each is a buy-and-lock buyback (§7.4); θ ≈ 0.3 then widen (§8.5)",
  11,"italic",SUB,anchor="middle")

# ================= RENDER =================
svg='<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">%s</svg>'%(W,H,W,H,"".join(S))
out_dir="/Users/brien/urfoundation/sn/diagrams"
os.makedirs(out_dir,exist_ok=True)
open(os.path.join(out_dir,"mechanism.svg"),"w").write(svg)
cairosvg.svg2png(bytestring=svg.encode(),write_to=os.path.join(out_dir,"mechanism.png"),output_width=W*2,output_height=H*2)
print("wrote",out_dir)
