#!/usr/bin/env python3
# Generates a detailed SVG diagram of WHITEPAPER.md "## 1. Summary of the mechanism"
# and rasterizes to PNG with cairosvg.
import math, cairosvg, os

W, H = 1740, 1180
S = []

# ---------- palette ----------
INK="#0f172a"; SUB="#475569"; FAINT="#94a3b8"; WHITE="#ffffff"
BG="#ffffff"
# channel colors: (stroke, fill, dark)
DEP  =("#2563eb","#eff6ff","#1d4ed8")  # deposits  / blue
EMI  =("#0d9488","#ecfdf5","#0f766e")  # emission   / teal-green
SET  =("#7c3aed","#f5f3ff","#6d28d9")  # settlement / purple
EVAL =("#d97706","#fffbeb","#b45309")  # evaluation / amber
BNTY =("#e11d48","#fff1f2","#be123c")  # bounty     / rose
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
T(40,86,"How money moves in three coupled channels (all denominated in the subnet's α token).  §1 of the whitepaper.",14.5,"normal",SUB)
S.append(f'<line x1="40" y1="104" x2="{W-40}" y2="104" stroke="#e2e8f0" stroke-width="1.5"/>')

# ================= NODES =================
# Customers (top-left)
card(40,150,250,96,OFF,"Customers","carries paid VPN/privacy\ntraffic — the real demand".split("\n"),)
# Network Operator (left)
card(40,372,300,184,DEP,"Network Operator (NO)",
     ["runs privacy servers + the","/verify server (co-signs trails)","","holds NO α only — never holds","others' emission; directs the split"],bsize=12)
# Providers (bottom-left)
card(96,904,452,150,SET,"Providers  (miners, 100k+)",
     ["carry ingress / egress traffic. Inside a NO's pool — NOT on-chain UIDs",
      "(subnet cap ≈ 256), so each is a client_id paid by Merkle claim.",
      "claim α directly from the contract with an O(log N) proof."],bsize=12)
# Coinbase (top center-right)
card(772,138,486,134,EMI,"Bittensor coinbase  (α emission)",[],tsize=15.5)
# emission split bar inside coinbase
sb_x,sb_y,sb_w,sb_h=786,196,458,30
segs=[("owner 18%",0.18,OFF[0]),("miner 41%",0.41,EMI[0]),("validator 41%",0.41,EVAL[0])]
cx=sb_x
for lab,frac,c in segs:
    wseg=sb_w*frac
    S.append(f'<rect x="{cx:.1f}" y="{sb_y}" width="{wseg:.1f}" height="{sb_h}" fill="{c}" opacity="0.9"/>')
    T(cx+wseg/2,sb_y+sb_h*0.66,lab,11.5,"bold","white",anchor="middle")
    cx+=wseg
S.append(f'<rect x="{sb_x}" y="{sb_y}" width="{sb_w}" height="{sb_h}" rx="4" fill="none" stroke="{EMI[0]}" stroke-width="1.2"/>')
T(786,250,"standard 18 / 41 / 41 split — we do not fight the coinbase",11.5,"italic",SUB)
# Owner (top-right)
card(1438,150,262,96,OFF,"Subnet owner",
     ["18% owner cut. A slice ω","co-funds the effort bounty."],bsize=11.5)
# Validators (right)
card(1438,330,262,180,EVAL,"Independent validators",
     ["anyone who stakes own α and","runs /verify trails. No NO, no pool.","","earn native dividends (by","stake × vtrust)  +  effort bounty"],bsize=11.5)
# Yuma (right, below validators)
card(1438,540,262,112,EVAL,"Yuma consensus",
     ["stake-weighted median +","clipping + vtrust, under","commit–reveal weights"],bsize=11.5,tsize=14.5)
# Anti-gaming (right, below yuma)
box(1438,684,262,110,12,EVAL[1],EVAL[0],1.4,dash="5 4")
T(1452,706,"Anti-gaming stack — ON",12.5,"bold",EVAL[2])
TL(1452,726,["commit–reveal · clip + vtrust","self-weight mask","bonds / Liquid Alpha"],11,"normal",INK,lh=15)

# Legend (top-center)
lg_x,lg_y,lg_w,lg_h=372,150,372,250
box(lg_x,lg_y,lg_w,lg_h,12,"#ffffff","#cbd5e1",1.4)
T(lg_x+16,lg_y+26,"Flows  (all in α)",14,"bold",INK)
rows=[("1",DEP[0],"Deposits (DT)","the costly demand signal",False),
      ("2",EMI[0],"Emission","Yuma consensus over NO pools",False),
      ("3",SET[0],"Settlement","per-NO Merkle payout claims",False),
      (None,EVAL[0],"Evaluation / quality","VERIFIER.md /verify trails",True),
      (None,BNTY[0],"Effort bounty","fee-funded validator reward",False),
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

# ================= CONTRACT (center hub) =================
cX,cY,cW,cH=404,470,812,362
box(cX,cY,cW,cH,14,CON_FILL,CON_BORDER,2.0)
# header
S.append(f'<path d="M {cX} {cY+14} Q {cX} {cY} {cX+14} {cY} L {cX+cW-14} {cY} '
         f'Q {cX+cW} {cY} {cX+cW} {cY+14} L {cX+cW} {cY+46} L {cX} {cY+46} Z" fill="{CON_HEAD}"/>')
T(cX+18,cY+30,"ST CONTRACT",16.5,"bold","white")
T(cX+162,cY+30,"— Subtensor EVM · custodian + deposit ledger + 7-day settlement",12.5,"normal","#cbd5e1")
T(cX+cW-16,cY+30,"owns each NO's miner-pool UID",11.5,"italic","#94a3b8",anchor="end")

# compartments
pad=16; gut=16
iy=cY+58; ih1=140; ih2=126
iw=(cW-2*pad-gut)/2
Lx=cX+pad; Rx=cX+pad+iw+gut
# L-top: deposit ledger
box(Lx,iy,iw,ih1,10,DEP[1],DEP[0],1.5,sh=False)
T(Lx+12,iy+24,"Deposit ledger",13.5,"bold",DEP[2])
TL(Lx+12,iy+46,["Dn = SUM(DT) per NO, per epoch.","The single quantity that weights",
                "everything else:","   w_n = Dn / Σ Dm   (demand share)"],11.5,"normal",INK,lh=17)
T(Lx+12,iy+ih1-12,"objective, revenue-backed anchor",10.5,"italic",DEP[2])
# R-top: miner-pool UIDs
box(Rx,iy,iw,ih1,10,EMI[1],EMI[0],1.5,sh=False)
T(Rx+12,iy+24,"Miner-pool UIDs  (one per NO)",13.5,"bold",EMI[2])
TL(Rx+12,iy+46,["contract-owned accrual slots — no","emission ever touches a NO's keys.","",
                "weight  w_n = deposit_n × Q_n"],11.5,"normal",INK,lh=17)
# emphasize formula
S.append(f'<rect x="{Rx+10}" y="{iy+ih1-40}" width="{iw-20}" height="26" rx="5" fill="white" stroke="{EMI[0]}" stroke-width="1.2"/>')
T(Rx+18,iy+ih1-22,"deposit × quality",12,"bold",EMI[2],family=MONO)
T(Rx+iw-14,iy+ih1-22,"drives 41% miner emission",10.5,"italic",SUB,anchor="end")
# L-bot: merkle roots
iy2=iy+ih1+gut
box(Lx,iy2,iw,ih2,10,SET[1],SET[0],1.5,sh=False)
T(Lx+12,iy2+24,"Per-NO Merkle payout roots",13.5,"bold",SET[2])
TL(Lx+12,iy2+46,["pool = earned α + refundable deposit.","NO commits a payout root each epoch;",
                 "it directs the split but never holds α."],11.5,"normal",INK,lh=17)
# R-bot: feepool
box(Rx,iy2,iw,ih2,10,BNTY[1],BNTY[0],1.5,sh=False)
T(Rx+12,iy2+24,"FeePool — effort bounty",13.5,"bold",BNTY[2])
TL(Rx+12,iy2+46,["FeePool = φ·Σ Dn  +  ω·OwnerCut","paid out by each validator's verified,",
                 "coverage-weighted completed trails."],11.5,"normal",INK,lh=17)

# ================= EDGES =================
# helper midpoints
def mid(a,b,t=0.5): return (a[0]+(b[0]-a[0])*t, a[1]+(b[1]-a[1])*t)

# 1) customers -> NO   (off-chain revenue)
la(165,246,165,372,OFF[0],2.4)
pill(165,309,["usage revenue ($)","off-chain reference rate"],11,SUB,OFF[0])

# 2) NO -> deposit ledger  (CHANNEL 1)
elbow([(340,452),(372,452),(372,iy+60),(Lx-2,iy+60)],DEP[0],2.8)
pill(372,420,["deposit α  (DT)","SUM = demand signal"],11.5,DEP[2],DEP[0],weight="bold")
circnum(372,452,"1",DEP[0])

# 3) coinbase -> miner-pool UIDs (CHANNEL 2)
la(1014,272,Rx+iw*0.5,iy-2,EMI[0],2.8)
pill((1014+Rx+iw*0.5)/2+10,402,["41% miner emission","accrues to contract"],11.5,EMI[2],EMI[0],weight="bold")
circnum(1014,300,"2",EMI[0])

# 4) coinbase -> validators (native validator emission)
elbow([(1258,196),(1410,196),(1410,360),(1438-2,360)],EMI[0],2.4)
pill(1360,300,["41% validator","emission (native)"],11,EMI[2],EMI[0])

# 5) coinbase -> owner (18%)
la(1258,168,1438-2,180,EMI[0],2.2)
pill(1348,150,["18%"],11,EMI[2],EMI[0],weight="bold")

# 6) validators -> yuma  (submit scores)
la(1569,510,1569,540,EVAL[0],2.4,hs=11)
pill(1569,525,["scores: deposit × quality  (commit–reveal)"],10.5,EVAL[2],EVAL[0])

# 7) yuma -> miner-pool UIDs  (consensus sets emission)  CHANNEL 2/eval
elbow([(1438-2,596),(1300,596),(1300,iy+ih1*0.5),(Rx+iw+2,iy+ih1*0.5)],EVAL[0],2.6)
pill(1335,iy+ih1*0.5-22,["consensus weight","sets miner emission"],11,EVAL[2],EVAL[0])

# 8) feepool -> validators  (effort bounty)
elbow([(Rx+iw+2,iy2+ih2*0.5),(1360,iy2+ih2*0.5),(1360,470),(1438-2,470)],BNTY[0],2.6)
pill(1392,iy2+ih2*0.5+6,["effort bounty"],11,BNTY[2],BNTY[0],weight="bold")

# 9) merkle roots -> providers (CHANNEL 3 settlement)
elbow([(Lx+iw*0.45,iy2+ih2),(Lx+iw*0.45,860),(360,860),(360,904-2)],SET[0],2.8)
pill(360,878,["claim α directly","O(log N) Merkle proof"],11,SET[2],SET[0],weight="bold")
circnum(Lx+iw*0.45,iy2+ih2-2,"3",SET[0])

# 10) NO -> providers (runs server, commits root)
elbow([(150,556),(150,904-2)],OFF[0],2.2,dash="6 5")
pill(150,730,["/verify server","commits payout root","(never holds α)"],10.5,SUB,OFF[0])

# 11) validators -> providers (measurement trails, the core signal) big arc
curve(1500,510,1380,840,820,1000,548,978,EVAL[0],2.6,dash="7 5")
pill(980,1010,["VERIFIER.md /verify trails:  walk provider chains, measuring liveness & quality Q_n"],11.5,EVAL[2],EVAL[0])

# key-insight callout (bottom banner)
kb_x,kb_y,kb_w,kb_h=476,1092,908,64
box(kb_x,kb_y,kb_w,kb_h,12,"#f8fafc","#cbd5e1",1.4)
T(kb_x+kb_w/2,kb_y+27,"deposit  =  objective demand anchor      ×      quality (Q_n)  =  validator-measured modulator      —      Yuma turns it into miner emission",
  12.5,"bold",INK,anchor="middle")
T(kb_x+kb_w/2,kb_y+48,"at bootstrap, governance caps the quality swing and widens it as the validator set and data mature",
  11,"italic",SUB,anchor="middle")

# ================= RENDER =================
svg='<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d">%s</svg>'%(W,H,W,H,"".join(S))
out_dir="/Users/brien/urfoundation/sn/diagrams"
os.makedirs(out_dir,exist_ok=True)
open(os.path.join(out_dir,"mechanism.svg"),"w").write(svg)
cairosvg.svg2png(bytestring=svg.encode(),write_to=os.path.join(out_dir,"mechanism.png"),output_width=W*2,output_height=H*2)
print("wrote",out_dir)
