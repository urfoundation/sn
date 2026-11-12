# Capture the flag (CTF)

**A per‑country coverage lottery beside the provider pool.**

Design, 2026‑11‑11. From the CTF start epoch `E_c` (epoch 8, the owner's decision; §11.2) it
amends the steering split of `WHITEPAPER.md` §8.5 and adds a second settlement v2 pool per network operator
(`VERSION2-FINAL.md`). The owner's decisions of 2026‑11‑11 are folded in (§12.1). Every other question the
design raised is resolved in §12.2, each with its reason, for the owner's review, except one that is open,
pending the owner's decision (§12.4). Where the code built since departs from this design's first draft, the
text follows the code and gives the reason; rows 9, 17 and 19–23 of §12.2 record the departures that change a
resolution.

## Summary

- **A new pool per network operator, with its own UID.** Each NO gets a second vault2 pool, the **CTF pool**.
  It settles exactly like the provider pool: cumulative per‑coldkey accounts, a sum‑tree root, auditor
  approval, the 14,400‑block challenge window, and permissionless claims that never expire.
- **The providers' share splits three ways.** Of the providers' share `μ` (1/10 of the miner allocation on
  SN25), the provider pools take **0.4**, the CTF pools **0.1** and the top‑level miners **0.5**. While no
  top‑level miner is weighted, the head's 0.5 goes to the provider pools only. The CTF pools therefore always
  get 0.1.
- **Ten slots per country.** Each of the 249 ISO 3166‑1 countries has 5 provider slots and 5 extender
  slots. Each slot is worth the same amount: the CTF pool's fresh funding for the epoch divided by 2,490.
- **The operator nominates, a public draw picks.** At the end of each epoch the NO lists the providers and
  extenders that its own data shows online, passing its checks and in one country for at least half of the
  epoch. It commits that census on chain. A drand round published after the commitment then seeds a draw of
  up to 5 winners per country and role.
- **One ticket per subnet.** Candidates are grouped by the keyed /29 IPv4 or /48 IPv6 unit of §8.4 and §8.6.
  A unit is one ticket, and its members are equivalent.
- **Unfilled slots carry forward.** A slot with no winner adds its amount to its country's pot for that role.
  Each winner takes its own slot and a fifth of that pot, and the rest keeps carrying. A miner who reaches a
  country nobody has reached therefore takes a fifth of its pot each epoch. It has to stay about five epochs
  to capture most of the pot, and every newcomer there takes a fifth too.
- **The carry goes round through a contract.** vault2 allocates every captured α at every root, so a pot
  cannot wait inside the pool. Each root pays the pots to the account of an immutable, ownerless **carry
  contract**. Anyone can trigger it to claim that account and move the α straight back onto the CTF pool
  hotkey, where the next close captures it again. It keeps a small residual of its own on vault2's escrow
  hotkey and opts out of third‑party staking‑hotkey growth, so no third party can block that claim for good
  (§4.6).
- **Validators do not probe it.** A validator's only CTF decision is whether to weight the CTF pools. That
  weight follows the provider pools' weight. The auditor approves each CTF root after recomputing the draw
  and its arithmetic.
- **Same registration, same claim flow.** Winners are paid to the payout coldkey that the provider pool
  already selects. Apps claim the CTF account next to the pool account.
- **Inspectable.** The census, the draw, the awards and the pots are public objects. `sn/api/operator.yml`
  documents the routes that the ur.xyz `/ctf` page reads.

## 1. Why

The provider pool pays for usage (§8.2). The head pays for routable breadth (§8.4). From epoch 8 the
extender pool spreads platform traffic evenly over prefix units (§8.6). None of them pays for being present
in a country. Usage concentrates where users are, and breadth and units reward many address blocks wherever
they sit. Censorship resistance, though, is decided country by country. §8.6.6 notes that a national firewall
can cut the platform's whole surface, and that providers and extenders inside that country are the way around
it.

On 2026‑11‑10 the network had providers in 109 countries: 74 with at least five and 35 with one to four
(ur.xyz dashboard history). 168 countries appeared over the previous 80 days, and about 210 extenders were
active. CTF pays a fixed bounty per country and role. So the first miner to reach a thin country earns far
more than one more miner in a dense one.

## 2. The emission split

### 2.1 Shares and cede rules

```
providers' share μ                 1/10 of the native miner allocation on SN25 (unchanged; treasury policy)
  provider pools     s_pool = 4/10   across NOs by ŵ_n = implied_usage_n × Q_n (§8.1)
  CTF pools          s_ctf  = 1/10   across NOs by the same ŵ_n (§2.2)
  top-level miners   s_head = 5/10   by EMA(score) (§8.4)
```

The cede rules apply in this order:

1. **CTF is fixed.** When the CTF channel is non‑empty, it gets exactly `s_ctf · μ`.
2. **Pool and head share the rest in the ratio 4 : 5.** "The rest" is `(1 − s_ctf) · μ`, or all of `μ` when
   the CTF channel is empty. When either channel is empty it cedes to the other, as `cede_to_nonempty` does
   today. So with no head fleet, the pools take `9/10 · μ`.
3. **When the CTF channel is empty.** It is empty when the validator does not support CTF (§2.3), when no NO
   has a CTF pool registered for the epoch, or when no NO's pool has weight. Its share then joins rule 2.
4. **With no provider weight at all** the SN25 row is reserve‑only, as today (§8.5).

Today's SN25 row, with no head fleet, is pool 9/100, CTF 1/100 and the two `ur‑reserve` recipients 9/20 each,
about u16 [13107, 1456, 65535, 65535]. Once fleets are weighted it is pool 4/100, CTF 1/100, head 5/100 and
the recipients 9/20 each. Every entry stays far under the 32768 cap. The treasury policy (μ = 1/10, cap
32768) is unchanged.

**Magnitudes at epoch 4's scale.** In epoch 4 the providers' share funded the pool with 1,195.84 α, all of
`μ`, since no fleet was weighted. Before fleets, CTF then gets about 119.58 α per epoch and the pool about
1,076.26 α. Once fleets are weighted, the pool gets about 478.34 α and the head about 597.92 α. A slot is
worth about 0.048 α. A country with no miners grows its two pots by about 0.48 α per epoch, roughly 25 α a
year.

**θ.** From `E_c` the three shares replace §8.5's θ (3/10 in policy v1). The nominal head share rises from
3/10 to 5/10 and the nominal pool share falls from 7/10 to 4/10. This changes nothing until a fleet is
weighted. It also makes §8.5's hard constraint easier to meet: the lowest‑paid top miner should earn at least
as much as the highest‑paid pool provider.

### 2.2 Across operators

```
ctf[n]  =  s_ctf · μ · ŵ_n          ŵ_n = pool_v[n] / Σ_m pool_v[m], the validator's own pool weights (§8.1, §10)
```

A NO's CTF pool takes the same fraction of the CTF channel that its provider pool takes of the pool channel.
No CTF‑specific measurement is needed:

- A validator that masks NO `n` (self‑mask) masks both of its UIDs.
- A pool that the deposit audit zero‑weights zeroes its CTF pool too.
- With one operator, the whole channel goes to UR's CTF pool.

### 2.3 Validators: support it or not, never probe it

- **No probing.** No validator measures, samples or probes a CTF miner. CTF has no quality term and no REACH
  message.
- **One decision.** Each validator decides whether to weight the CTF pools. The release has a setting
  (`ctf: support | off`), and its sample configuration sets `support`. A validator set to `off` treats the
  CTF channel as empty (rule 3).
- **Yuma decides what is paid.** Each CTF UID's incentive is the κ‑stake‑weighted median of the validators'
  weights for it. A CTF pool is paid only while validators holding more than half of the active stake
  support it.
  - Validators that support CTF while consensus does not have their CTF weight clipped.
  - Validators that do not support it while consensus does are clipped where they overweight the pool or
    head.
  - Either way the gap is about 1/100 of the row, so the vtrust cost is small.
  - The owner's validator holds the stake majority today, so CTF is paid.
- **Resolving the UID.** A validator finds NO `n`'s CTF pool with `STSettlement2.ctfSettlementAt(n, epoch)`.
  It checks the hotkey against vault2's own `pools(ctf(n))` record, as it does for the provider pool (§10).
  An unregistered or pruned CTF pool leaves the channel empty for that NO.
- **The audit is separate.** vault2 finalizes a CTF root only with auditor approval, and at launch the auditor
  key is the validator's. That audit recomputes the draw and the arithmetic from public objects and chain
  state (§8). It is the same kind of check the validator runs on the extender census, not a measurement.

### 2.4 How the split is fixed: a signed amendment entry

The split does **not** move by a successor protocol policy. The server resolves each epoch's payout consents
in a domain that includes that epoch's policy hash. It also refuses an epoch whose policy hash differs from
its configuration (`server/controller/provider_wallet_epoch.go`), and the contracts and configs bind the
same hash. A successor policy would leave every existing payout consent outside the new epochs' domain.

So `E_c`, the shares and the CTF parameters are fixed by a signed amendment entry, published beside the price
sheet with its pinned key. This is the mechanism §8.6.7 introduced for `E_x`, and its readers in the server,
the validator and `snclaim` are extended to read it:

```
UR settlement amendment v2: chain 964, sn 25, no 1, from epoch 8, capture the flag, ctf pool 4294967297, carry 0x<carry contract>, shares pool 4/10 ctf 1/10 head 5/10, head cedes to pool, slots 5 providers 5 extenders, countries iso 3166-1 249, presence half epoch, units egress prefix, draw drand quicknet 60s after census
```

The owner signs it before `E_c` starts, because the signing tool refuses an entry for an epoch that has
already started.

The entry is a line of `amendments.yml`, beside `price.yml`, published at `https://ur.xyz/amendments.yml` and
signed over its text with the price sheet's pinned key. The `amendment` package reads it, and `cli/amendment`
signs it (`mainnet/SETTLEMENT-AMENDMENTS.md`). Only the chain, sn, operator, first epoch and carry vary: the
carry must be in EIP‑55 checksum form, and the ctf pool must be `2^32 + no`.

## 3. The CTF pool

### 3.1 On chain

**Pool id.** NO `n`'s CTF pool is the vault2 pool `ctf(n) = 2^32 + n`, so UR's is **4,294,967,297**.

- The id sits above every v1 operator id and below 2^53, so JavaScript clients keep it exact.
- vault2 needs only `noId < 2^64`.
- vault2 itself does not change.

**`STSettlement2Ctf`.** The owner‑upgradeable `STSettlement2` gains an upgrade, built on the implementation
then live (phase C's when its upgrade has run). Its storage is appended after that implementation's slots and
pinned in `evm/layout/`: slots 0 to 50 stay phase C's, and the census mapping is slot 51
(`evm/layout/STSettlement2Ctf.json`). It restates phase A's `registerOperatorPool` and `commitDistribution`
rather than inheriting them, since they are not virtual and phase C's creation code is fixed by its CREATE2
address.

| Function | Caller | Behaviour |
| --- | --- | --- |
| `registerCtfPool(n, maxBurn, firstEpoch)` | O, payable | Requires v1 operator `n` active, `n < 2^32`, `n`'s v2 pool registered and `firstEpoch > currentEpoch()`, and applies the same cadence check as `registerOperatorPool`. Derives the pool hotkey (below), refusing a zero parent hash (`BlockHashUnavailable`), calls `vault.registerPool(ctf(n), hotkey, …)` and refunds the burn surplus to its caller, the timelock on mainnet. |
| `registerOperatorPool(n, …)` | O, payable | Unchanged, except that it now refuses `n ≥ 2^32`, so no provider pool can take a CTF id. |
| `commitDistribution(e, poolId, …)` | R | For a CTF pool id the root signer is `operatorAt(poolId − 2^32, e).rootSigner`. Everything else is unchanged. |
| `commitCtfCensus(e, n, censusHash)` | R, not paused | The root signer of `operatorAt(n, e)`, at a block in `[end(e), end(e) + rootCommitWindowBlocks/2]` (600 blocks on mainnet). Requires `n`'s CTF pool registered with `e ≥ firstEpoch`. Write‑once per `(n, e)`. Records the hash and block, and emits `CtfCensusCommitted(n, e, censusHash, block)`. While the v1 coordinator is paused it reverts `SettlementPaused`, as pause stops settlement, and the window does not wait: a pause across it means no census for `e`, so no draw (§4.7). |
| `ctfCensus(n, e)` | view | `(censusHash, block)`, zero when none was committed. |
| `ctfSettlementAt(n, epoch)` | view | `(vault, hotkey, firstEpoch)` when `n`'s CTF pool is registered and `epoch ≥ firstEpoch`, otherwise zeros. |
| `closeOperatorEpoch`, `approveDistribution`, `finalizeDistribution` | as today | They already take vault2 pool ids, so they serve the CTF pool unchanged. |

**Governance.** The upgrade and the registration go through the `TimelockController` (72 h) as one
operation: `upgradeToAndCall`, with `registerCtfPool` as its call and the burn ceiling `maxBurn`,
`maxBurn × 10^9` wei, as its value (`mainnet/BOOTSTRAP-CONTRACTS.md`, "Capture the flag upgrade"). As two
operations they would take 144 h, because review refuses to schedule a call that would revert at the head, and
`registerCtfPool` does not exist until the upgrade has run.

- **Funding.** Anyone first sends the timelock at least the value. It pays the call's value from its own
  balance, so the execute transaction carries no value and any key sends it.
- **The refund stays in the timelock.** vault2's refund of the burn surplus returns to the timelock, the
  caller, and moving it out takes another 72‑hour operation. `maxBurn`'s headroom over the live burn is sized
  with that in mind.
- **Review.** `sn-mainnet upgrade-settlement2 review` builds and preflights the schedule and the execute from
  the CTF artifact. It neither signs nor sends.

**The pool hotkey is derived at execution,** as `keccak256(proxy, ctf(n), block.number, blockhash(block.number − 1))`:

- A hotkey named in the scheduled call would be public for the timelock's 72 h. Whoever registered it on SN25
  first would own it, and vault2's registration would then fail, costing another 72 h.
- A derived hotkey is known once its parent block is sealed, about 12 s before the executing block. A failed
  execution changes nothing and leaves the operation ready, so it can run again in a later block, under
  another hotkey.
- **Those seconds are a window.** Anyone who sees the execute transaction pending can take the hotkey first
  inside the executing block: for a fee through the neuron precompile's `tryAssociateHotkey(bytes32)`, or for a
  burn through a registration on any subnet. vault2's registration then fails and the execute reverts. A
  griefer could repeat this block after block until the first epoch starts, which would move the start. The
  execution plan accounts for it (§11.2): the operation is ready days early, the execute is retried with a
  bounded gas limit, and a fallback operation for the next epoch is scheduled beside it.
- Without the parent's hash the hotkey would be known for every block in advance, so `registerCtfPool`
  refuses a zero `blockhash(block.number − 1)` (`BlockHashUnavailable`), and rehearsal R-9 qualifies
  `BLOCKHASH` on the runtime.
- Nothing signs with a pool hotkey. vault2, its coldkey, makes every call for it: the registration, the
  captures and any re‑registration.

**UID.** Registration burns one UID. The pool is registered one to two days before `E_c`, as the v2 pool
was, so that it earns weight before its immunity ends. §14's budget gains one UID per NO. Anyone can
re‑register a pruned CTF pool hotkey with `registerPoolUid`.

**Why not a second v1 operator?** Registering the CTF pool as v1 operator 2 would need no upgrade. But it
would cost two UID burns (a v1 pool UID and the v2 one), a deposit hotkey and two signers for something that
is not an operator. Its consents would also belong to another NO's domain (§3.2), and the census commitment
would still need a home.

### 3.2 Same registration

A winner is paid to the coldkey that the provider rows' wallet selection gives its client. That is the
client's own consent, else its network's, else a hotkey delegation (§8.2, E4). Payout consents name the NO
(`ClientKeyHistoryDomain.NoID`), not a vault2 pool. So one consent covers both of NO `n`'s pools, and no
miner signs anything new. The same coldkey then holds one account in each pool and makes one claim per pool,
or one batched claim for both (§10).

### 3.3 Settlement

The CTF pool follows the v2 lifecycle (`VERSION2-FINAL.md` §5):

- **Close.** Anyone may close it within 1,200 blocks of the epoch's end, and the server does so with the
  provider pool.
- **Commit.** The NO's root signer commits the root.
- **Approve.** The auditors approve it before the next epoch ends.
- **Finalize.** Anyone finalizes it once the 14,400‑block window has passed with no open demand.
- **Challenges and demands** protect every account exactly as in the provider pool.
- **Claims** are cumulative and permissionless. They always pay the leaf's coldkey and never expire.

## 4. Slots, the draw and the carry

### 4.1 Countries and slots

- **The country list.** `L` is the 249 codes of `server/model/iso_country_names.go`, every ISO 3166‑1
  alpha‑2 code (owner decision). Codes are lowercase, as the server stores them, and the site shows them
  uppercase.
  - Kosovo (XK) is not an ISO 3166‑1 code, so it is not in `L`.
  - The seven territories without a permanent population (AQ, BV, GS, HM, IO, TF, UM) are in `L`. Their
    pots grow every epoch and will probably never be paid (§7).
- **Slots.** `S = 10 · |L| = 2,490`. A slot is `(country, role, rank)`, where role is provider or extender and
  rank runs 1–5.
- **Slot order.** Slots are ordered by country code, then provider before extender, then rank. This order
  decides who gets the remainder rao (§4.5).

### 4.2 Eligibility: the operator's nomination

The NO nominates from its own data. It is operator‑attested, like the provider population and the extender
census (§8.6.4). Eligibility needs the miner online, passing the operator's checks and in one country for at
least half of the epoch, and active at its end (owner decision).

**Presence.** The server samples every provider and extender of NO `n` each quarter hour, 672 times an
epoch. A sample records whether the miner is present (P1 or X1, below) and its country. The threshold is
half of the epoch's samples, less any the operator excuses for a platform‑wide outage it declares; the
excused samples are listed with the census. The provider data already exists (the 1‑minute reliability blocks
and the score export behind `ClientScore`). The extender challenge (every 5 minutes, six failures in a row
deactivate an address) keeps only current state today, so the sampler is its first history.

**Providers.** A provider client `x` of NO `n` is a candidate in country `c` for epoch `k` when:

- **(P1) Online and passing at `end(k)`.** At the boundary capture it is an active, top‑level, connected
  provider with a Public provide key and a valid location (`network_client_location_reliability.valid`). It
  also passes the operator's provider eligibility (`ClientScore.Online`): the reliability minimums, risk,
  quarantine, zero‑carry and probe rules of `decideProviderEgress`.
- **(P2) Present in `c` for half the epoch.** At least half of the epoch's non‑excused samples found it as P1
  requires, with `c` as its published country.
- **(P3) Its country is `c` at `end(k)`.** That is its published country: the derived location from
  co‑signed pings, else the operator's egress probe, else GeoLite2.
- **(P4) Distinct (E3).** One candidate per IPv4 address or IPv6 /64 per role, the lowest client id, using
  the keyed address hashes of the verify index.
- **(P5) Payable (E4).** It has a payout coldkey under the wallet selection, and no active fleet binding at
  either boundary. A head client earns in one tier only.
- **(P6) Independent (E5).** It is not on one of `n`'s own networks: the gated, durable fleet
  (`durable_network_ids`), canaries, or any network `n` declares it controls.
- **Its unit** is the egress prefix that the verify head model pins for it in epoch `k`: its IPv4 /29 first,
  else its IPv6 /48 (`server/model/verify_head_model.go`). It is keyed exactly as §8.4 keys it.

**Extenders.** An extender record `x` of NO `n` is a candidate in country `c` for epoch `k` when:

- **(X1) Active at `end(k)`.** E1 of §8.6.2 holds: the record, signed by `n`'s root key, is active and
  unrevoked, and at least one of its addresses is active.
- **(X2) Present in `c` for half the epoch.** At least half of the non‑excused samples found it active with
  an address whose challenge passed within the sample's quarter hour, and with `c` as its country.
- **(X3) Its country is `c` at `end(k)`.** That is the record's country: the derived location's when it has
  one, else the activation's GeoLite2 country. Every activation of the record in epoch `k` was in `c`.
- **(X4–X6)** E3, E4 and E5 of §8.6.2: distinct by provider client and address, payable with no fleet
  binding, and independent.
- **Its unit** is the /29 of its active IPv4 address, else the /48 of its active IPv6 address, keyed exactly
  as §8.6.2 keys it.

**Units are tickets.** All candidates in one unit are equivalent: the unit holds one ticket per role, whatever
its size. A unit enters one country's draw for each role, the country of the lowest client id among its
candidates left in the draw (fleet‑bound ones are dropped first, §4.4); the other members follow it there. The census keeps each candidate's own country (§4.3), and the draw, not the
census, places the unit. So members of one unit may list different countries, and the auditor can check the
rule from the census (`ctf` package). A client is a candidate in at most one country per role. It may be a
candidate in both roles and win both: every miner can be both a provider and an extender (§8.6 decision 9).

### 4.3 The census and its commitment

Right after the end boundary is finalized, at the moment the server already captures the users count
(`stLedgerV2CaptureUsers`), the server builds epoch `k`'s **census**. It is one row per candidate, with the
candidate's own country at `end(k)`, sorted by role, country, unit and client id:

```
census row (52 bytes):  role (1: 1 provider, 2 extender) ‖ country (2, ASCII) ‖ unit (32) ‖ unit family (1: 4 or 6) ‖ client_id (16)
census header (JSON):   {"schema":"urnetwork-ctf-census-v1","no_id":N,"pool_id":P,"epoch":K,"countries":"sha256:…",
                         "rows":["sha256:…",…],"candidates":C,"excused_samples":[…],"excluded":"sha256:…"}
```

- **Rows** are in chunks of 131,072.
- **`excluded`** names an object of per‑country counts of the miners each rule excluded: not online at the
  end, too little presence, country changed, no valid location, duplicate address, no wallet, head‑bound, own
  network, not Public, and revoked or inactive. It holds counts only.
- **No coldkeys and no addresses** are in the census.
- **The commitment.** Within 600 blocks of `end(k)` the root signer commits `censusHash = sha256(census
  header)` with `commitCtfCensus(k, n, censusHash)` at some block `B_k`. The commitment is write‑once, so the
  candidates are fixed on chain before any randomness for the draw exists.
- **Canonical, or no draw.** A committed census that is not canonical gives no draw for `k`, exactly as an
  uncommitted one does (§4.7, §8 check 1). The server publishes every census object before it commits the
  hash, and keeps them: a committed census whose objects are lost can never be verified, and the roots that
  cover its epoch stall.

### 4.4 The draw

```
ρ_k        = the first drand quicknet round whose time is ≥ ts(B_k) + 60 s     time(r) = 1692803367 + 3·(r − 1)
rand_k     = that round's randomness: sha256 of its BLS signature, verified against quicknet's public key
σ_k        = keccak256("urnetwork/ctf-draw/v1" ‖ chain_id:u64 ‖ netuid:u16 ‖ pool_id:u64 ‖ k:u64 ‖ censusHash ‖ rand_k)
ticket(u)  = keccak256(σ_k ‖ role:u8 ‖ country:2 ‖ unit:32)                     one per unit, per role and country
winners    = the min(5, |U_{c,r}|) units with the smallest tickets, ranked 1… by ticket ascending
member     = in each winning unit, the candidate with the smallest keccak256(σ_k ‖ role:u8 ‖ unit:32 ‖ client_id:16)
```

- **Fleet‑bound candidates are left out first.** Before the draw, every candidate with an active fleet
  binding at either boundary (P5, X5) is left out, from the chain's bindings, which the builder and the
  auditor read alike. `U_{c,r}` is the units left in country `c` and role `r`. So no root is refused
  because its census lists a bound candidate: the draw never picks one (§8 check 3). Refusing roots for it
  instead would leave the epoch unbuildable, since a committed census cannot be rebuilt.
- **The randomness source.** The server and the validator read the pulse from the chain's Drand pallet when
  it is stored there, else from any drand relay. A pulse verifies against quicknet's key whatever its source.
  `sn/crv4` already carries quicknet's constants.
- **What the draw guarantees.** Taking the smallest tickets draws a uniformly random set of up to five
  units, and the member hash picks a uniform member within each.
- **Nobody can steer it.** The operator fixed the candidates at `B_k`, at least a minute before `ρ_k`
  existed. Block producers cannot bias drand. A second commitment is refused.
- **Withholding gains nothing.** An operator that withholds its commitment gets no draw, and everything
  carries forward (§4.7).

### 4.5 Amounts

A root's walk starts owing `X = O_p`, the pots and pending awards after the root it builds on (0 for the
pool's first root). Then, for each covered epoch `k` of the root, in order:

```
f_k      = fundedThrough[k] − fundedThrough[k−1]                     the CTF pool's funding, read from vault2
raw_k    = recycledFor[k] + returns rolled from uncaptured epochs     a deferred epoch (f_k = 0) rolls its raw to the next (§4.6)
R_k      = min(raw_k, X)                                              returned; stake beyond what is owed is a donation, fresh
M_k      = R_k + 8 ≥ X                                                matured: the carry is back, within two cycles' rounding
credit_k = X if M_k;  R_k + 8 if 0 < R_k;  else 0                     then X ← X − credit_k: a return writes off up to 8 rao
F_k      = f_k − credit_k, or 0 if credit_k > f_k                     the fresh amount; a shortfall credit_k − f_k comes from the
                                                                      pots, the largest first (ties to pot order), then pending awards
a_{k,s}  = floor(F_k / S) + (1 if s < F_k mod S, else 0)              slot s's amount in slot order; Σ_s a_{k,s} = F_k

for each country c and role r, with pot P = P_{c,r} and W = min(5, |U_{c,r}|) winners:
  q        = floor(P / 5)                                              a fifth of the pot
  winner of rank i ≤ W   gets  a_{k,(c,r,i)}  now, and  q  (plus 1 rao if W = 5 and i ≤ P mod 5)
                                now if M_k, else as a pending award to its coldkey
  P_{c,r} ← P − W·q − (W = 5 ? P mod 5 : 0) + Σ_{i=W+1..5} a_{k,(c,r,i)}     what is not won carries
pending awards           all paid in the first matured epoch, with or without a draw: in full, unless a shortfall
                         finds every pot empty
O_k      = Σ pots + Σ pending awards                                  what the CTF pool owes after k
```

- **A fifth of the pot per winner** (owner decision). Each winner takes its own slot and a fifth of the pot
  carried into its country and role. Five winners empty the pot. Fewer leave the rest to carry, with the
  epoch's unfilled slots added.
  - A lone miner in an unclaimed country takes a fifth of what is left each epoch. It captures most of a pot
    only by staying: about 67% of it in five epochs, and 89% in ten.
  - Every newcomer who becomes eligible there takes its own fifth, so a pot never goes all at once to
    whoever arrives first.
  - A lone winner that stays settles where the pot holds twenty slot amounts, and it then takes five slot
    amounts per epoch.
- **Fresh amounts only.** Slot amounts come from fresh funding only. Returned carry pays obligations, never
  new slots.
- **Exactness.** All amounts are exact integers in rao. Rounding goes to the lowest slot positions and the
  best ranks, by the rule a contract already uses for its byte shares (`evenContractPayoutShare`).
- **The carry account.** At the end of the root's walk the carry account receives everything the walk did
  not pay. That is the pots and pending awards, less what the carry contract still owes back. So the root's
  total equals `fundedThrough[e]` and vault2's invariant holds (§4.6).
- **Where the walk departs from this design's first draft, and why.** The `ctf` package implements the walk
  above (`ctf/doc.go`, "Amounts"). The first draft had `M_k = R_k ≥ X_k − 4 and f_k ≥ X_k` and
  `F_k = f_k − (M_k ? X_k : R_k)`, with `X_k` the carry account's cumulative less everything returned and
  written off. Where that broke, the package takes the closest correct rule:
  - **What is owed starts at the root built on's pots and pending awards.** That equals the first draft's
    `X_k`, and it needs no extra state.
  - **A shortfall comes from the largest pot.** The first draft's `M_k` also needed `f_k ≥ X_k`, and its
    `F_k` could be negative. With no fresh funding (validators not supporting CTF, §4.7), a single rao lost
    to rounding then left every later epoch unmatured, so pending awards never paid, and a capture a rao
    short of the return made `F_k` negative. A shortfall the funding cannot cover is now taken from the pots,
    the largest first: at most a few rao. Only once every pot is empty does it come from the pending awards,
    which are then paid that much short.
  - **The tolerance is 8 rao, two cycles' rounding, not 4.** With one cycle's rounding (§4.6) the residual
    could not clear: a partial return leaves the rounding of several cycles owed, a late root's return and
    the next one's can come back in one epoch, and whatever stays owed at a root's end is owed again by the
    next. At the bound of 4 rao a cycle the residual never shrank, and pending awards piled up for good. With
    8, and a partial return writing off as much, the rounding left owed shrinks by at least 4 rao with every
    single return and never grows with two.
  - A write‑off that comes back later counts as fresh. In every case the deltas sum to the funding.

### 4.6 The carry contract

vault2 requires each root's total to equal the pool's funding through its epoch (`commitPending`: `total ==
fundedThrough[e]`), so every captured rao sits in some account at every root. A pot that waits for a winner
therefore has to leave the CTF pool and come back. The **carry contract** does that with no custodian (owner
decision):

```
STCtfCarry                       immutable, ownerless, one per CTF pool; reads escrowHotkey, selfColdkey and the
                                 pool hotkey from vault2, which never changes them
constructor(vault2, poolId)      payable: created with at least seedMinimum() rao of TAO
    addStake(escrowHotkey, all its TAO);  residual = the gain                    // its own stake, never returned
    setColdkeyAutoStakeHotkey(netuid, poolHotkey)                                // pins the pool hotkey on its list
    setRejectStakingHotkeyGrowth(true);  setRejectLockedAlpha(true)              // no third party lengthens its list
recycle(rootIndex, account, proof)   anyone
    kept = min(residual, this coldkey's movable stake on the escrow hotkey)     // follows a residual the runtime lowered
    if an account is given: require account.coldkey == this contract's coldkey;
                            if vault2.claimed(poolId, account.index) < account.cumulative:   // not after anyone's claim
                                require kept > 0                                 // ResidualMissing: seed() first
                                vault2.claim(poolId, rootIndex, account, proof) // pays this coldkey on vault2's escrow hotkey
    amount = this coldkey's movable stake on the escrow hotkey − kept           // all above the residual, claimed by anyone
    moveStake(escrowHotkey → poolHotkey, amount)                                 // its own position
    transferStake(to vault2's coldkey, on poolHotkey, all of it)                 // vault2's position on the pool hotkey grows
    g = the growth of vault2's position, measured as vault2 measures its moves
    e = vault2.lastFundedEpoch(poolId) + 1;  recycledFor[e] += g;  emit Recycled(e, g)
seed()                               anyone, payable
    addStake(escrowHotkey, all its TAO, at least seedMinimum());  residual = kept + the gain
seedMinimum() = max(0.1 TAO, 10 × the runtime's nominator minimum)
no owner, no upgrade, no entry point but recycle and seed; beyond its residual it holds stake on the escrow and
pool hotkeys only until the next recycle
```

vault2's claims are anyone's and always pay the leaf's coldkey, so a third party's claim of the carry account
leaves stake on the carry's coldkey until the next recycle moves it. Third parties can send the carry's coldkey
stake only on hotkeys already on its list (below): the escrow hotkey and the pool hotkey, whose stake goes round
with the carry, and whatever a fill left before the creation, whose stake stays where it is.

**Why it keeps a residual.** vault2's claim pays the carry by a transfer from another coldkey, and runtime 486
lets such a transfer add a hotkey to the destination's staking‑hotkey list only while that list holds fewer
than 128 entries (`TooManyStakingHotkeys`). A move of a whole position leaves no dust, so a carry that
returned everything would drop the escrow hotkey from its list at every recycle. Anyone could then fill the
list with about 128 small transfers, before the first claim, since the address is known in advance, or
between claims, and vault2's claim would be refused for good: slot amounts would still pay, but the pots and
pending awards would never return. The contract closes that:

- **The residual keeps the escrow hotkey on the list.** A transfer to a hotkey already on the list passes
  whatever the list's length, so vault2's claim always lands.
- **The pool hotkey stays on the list too.** The constructor makes the CTF pool hotkey the carry coldkey's
  auto‑stake destination on the subnet (`setColdkeyAutoStakeHotkey`), which keeps that hotkey on its list
  with no stake, for good: pruning never clears a destination, and a coldkey swap never overwrites one. So
  the carry's own move lands on a listed hotkey too, which passes whatever the list's length. That matters
  because the opt‑out below does not count hotkeys a coldkey *owns*: a coldkey swap into the carry can give it
  registered hotkeys, and the runtime adds their emission to its list with no cap. Past 256 entries a carry
  without the pin could no longer move onto the pool hotkey. A destination must hold a UID, which the pool
  hotkey does once the operation of §3.1 has registered it; the escrow hotkey does not, which is why it keeps
  a residual instead. Emission of any hotkey the carry is given on SN25 lands on the pool hotkey and goes
  round as a donation.
- **The opt‑out keeps the list short.** With `setRejectStakingHotkeyGrowth` set, the runtime refuses every
  other third‑party transfer that would add a hotkey, and every coldkey swap into the carry's coldkey that
  would bring a staking hotkey, so the list, and the gas of the carry's moves, stay small. It does not touch
  the carry's own operations. `setRejectLockedAlpha` keeps refusing conviction‑locked alpha, as every account
  does by default.
- **The carry stakes the seed itself.** Its constructor stakes the TAO it is created with by its own
  `addStake`, which only the 256 cap limits, so a fill of the list before creation, at most 128 entries,
  cannot stop it. A seed transferred by a third party could be refused like the claim, and seeding from the
  carry account's own stake would need a first claim that the fill can already block.
- **The residual is outside money.** It is never returned to the pool and never enters vault2,
  `recycledFor` or the ledger, so what the ledger counts as owed (§4.5) is unchanged. A residual the runtime
  lowers, by rounding or by root's sweep below, is followed down and never refilled from the pool's stake.
- **Only root's sweep can clear it.** The runtime clears a position on a hotkey its coldkey does not own when
  it is worth less than the nominator minimum, and only on its owner's own unstake or when root raises that
  minimum, which sweeps every account: it unstakes the position to the coldkey's TAO, or deletes it if that
  unstake fails. So a seed stakes at least `seedMinimum()`: ten times the runtime's nominator minimum, and at
  least 0.1 TAO. The creation seeds 1 TAO (§11.2). If a sweep clears the residual anyway, `recycle` reverts
  `ResidualMissing` and the account stays claimable; anyone restores the residual with `seed()`, which stakes
  the TAO the sweep returned and any sent with it. A sweep that also caught carry not yet recycled would turn
  it into residual at that seed, and the ledger would then count it as never returned, so the relay recycles
  as soon as anyone claims the carry account.
- **Pruning changes nothing.** Moves and `addStake` need only the hotkey's owner, which pruning keeps, and the
  auto‑stake destination outlives a pruned UID. The escrow hotkey earns nothing and is pruned already; only
  vault2, which has no call for it, could register it again, so it could not be pinned like the pool hotkey.

- **Every epoch the carry cycles.** The root allocates the carry account its delta, the root finalizes about
  two days after the epoch ends, and the server's relay calls `recycle`. The next close then captures the
  returned stake with the epoch's emission, and the ledger tells them apart by `recycledFor` (`R_k`, §4.5).
  Gas is one claim and two stake moves an epoch, paid by the relay.
- **Deferred epochs.** A recycle recorded for an epoch that is later deferred (a missed close) is captured
  by the next close, so the ledger rolls its `R` forward to that epoch.
- **Donations are fresh funding.** Stake anyone sends the carry on the escrow hotkey, or vault2 on the CTF pool
  hotkey, is counted as fresh funding: `R_k` never exceeds what is owed.
- **Rounding.** Each stake move may lose a rao to the runtime's rounding (§7.3 of `VERSION2-FINAL.md`), at
  most four per cycle: the claim, the two moves and the next capture. The residual adds none: the carry
  moves everything above it, so the claim's rao fragments go round with the carry. The ledger tolerates two
  cycles' rounding, 8 rao: a matured epoch writes off what is still owed, and a partial return writes off up
  to 8 rao, against the fresh amount (§4.5).
- **What it trusts.** It relies only on vault2 and the runtime, and it changes nothing in vault2, which pays
  the leaf's coldkey whoever calls.
- **Creating it.** Anyone creates it after the operation of §3.1, through the deterministic deployment proxy,
  with the seed as the call's value and a fresh random salt that nobody sees before the creation transaction.
  - First, a guardian coldkey that never swaps the hotkeys it holds makes the carry's coldkey a hotkey account
    (`try_associate_hotkey`). That reveals the coldkey, not the address. Every coldkey swap into it, and every
    reservation of it (below), is then refused (`NewColdKeyIsHotkey`), before and after the creation: no swap
    can fill its list to 256 so that the seed's `addStake` is refused, bring it locked alpha, or give it
    hotkeys. Whoever holds the hotkey account can release it, by a hotkey swap, in the same batch as a coldkey
    swap's announcement into it, and the association succeeds without effect when someone else holds it. So
    the guardian sends it through `MevShield.submit_encrypted`, which nobody sees pending, and review requires
    the guardian to hold it; if someone else took it, the carry is created under another salt.
  - The proxy's mapped account must hold TAO above its existential deposit. The EVM checks the proxy's
    spendable balance, which the runtime reports less that deposit, before CREATE2 passes the value on
    (`OutOfFund`), so a proxy holding only the forwarded seed cannot create the carry. Anyone sends it 501 rao
    or more once.
  - It is created while the pool hotkey holds its UID, within the registration's immunity window, since the
    auto‑stake destination needs one. A failed creation reveals the salt, so a retry takes a fresh one.
  - `sn-mainnet ctf-carry review` (`mainnet/BOOTSTRAP-CONTRACTS.md`, "Capture the flag carry") pins the
    artifact, derives the CREATE2 address, requires each of these, vault2's coldkey among the guardian's
    hotkey accounts too, and preflights the creation; once the carry exists it checks the code, the residual,
    the auto‑stake destination, the opt‑outs and both hotkey accounts. The amendment entry names the address
    after the creation.
  - A fill of the list before the creation, which anyone who sees the transaction can still send in the same
    block, is at most 128 entries. The creation goes through, and the entries only add gas to the carry's
    moves, about 8,750 an entry on runtime 478 and later.
- **Coldkey‑swap reservations, and the hotkey accounts that refuse them.** For 0.1 TAO anyone can announce a
  coldkey swap that names a coldkey as its destination. While the reservation lasts, up to six days and
  renewable for another 0.1 TAO, the runtime refuses every transfer into that coldkey from another one: for
  the carry's coldkey, vault2's claim; for vault2's, the carry's return. Only the reserved account can release
  it, and on runtime 486 no precompile lets a contract do that. But the runtime refuses to reserve, or swap
  into, a coldkey that is a hotkey account. So the guardian makes vault2's coldkey a hotkey account now,
  before anyone reserves it and well before the operation of §3.1, and the carry's before its creation
  (§11.2). Without them a reservation would delay the carry, taking nothing: the account stays claimable,
  the epochs are not matured, and pot shares wait as pending awards (§4.7). With vault2's, no coldkey swap
  can fill vault2's own list either (below).
- **vault2's own list can stall a recycle.** The carry's transfer adds the CTF pool hotkey to vault2's list
  whenever vault2 holds nothing there, as after a capture with no collateral lock. With third parties holding
  vault2's list at 128, `recycle` reverts `TooManyStakingHotkeys` until an emission credit re‑adds the entry,
  about one tempo after a capture while the CTF UID earns. vault2's permissionless `absorbStake` frees an entry
  only for a position on SN25 worth at least the transfer floor, 100,000 rao of TAO; entries on other subnets,
  or below the floor, stay. So while the CTF UID earns nothing, third parties can hold the return back with
  such entries, and the pots and pending awards wait. The server's relay retries, and absorbs what it can.
  - **A coldkey swap into vault2 could make that permanent, and stop pool registrations.** vault2 opts out of
    nothing, so a swap could merge into its coldkey up to 256 entries that `absorbStake` cannot free, for a
    few hundred minimum stakes and 0.1 TAO with five days' notice. A new pool hotkey needs vault2's list
    below 256, so every later pool registration, the CTF pool's of §3.1 included, would be refused for good.
    vault2's coldkey as a hotkey account refuses such a swap, which is why the guardian makes it one now,
    encrypted as above, and checks with `getHotkeyOwner` that it holds it. It has to come first: while an
    announcement reserves vault2's coldkey the runtime refuses to make it a hotkey account too
    (`ColdkeySwapDestinationReserved`), and that swap could then go through; and anyone else who held it
    could release it with such a swap. vault2's coldkey cannot change, so that would be for good.
- **What the relay does when a recycle fails.** A refused precompile call returns no data, so the relay reads
  the state: no residual (`residual()` is 0, or `recycle` reverts `ResidualMissing`) means `seed()`; vault2's
  list at 128 without the pool hotkey means waiting a tempo, or `absorbStake` of a foreign SN25 entry worth
  the floor; a reservation, if a coldkey were not a hotkey account, means waiting. Every case leaves the carry
  account claimable. The relay also recycles at once after anyone claims the carry account.
- **Qualifying it.** It needs the same runtime qualification as vault2's moves. Rehearsal R-9
  (`rehearsal/r9_ctf_test.go`; `scripts/runtime-rehearsal/README.md`, "Capture the flag (R-9)") covers, on the
  live runtime, a creation that stakes its seed from the constructor and sets the opt‑outs over a list
  third parties filled first, a claim to the contract coldkey, a `moveStake` and a `transferStake` from it, and
  the capture of the returned position (gas, rounding, the staking‑hotkey lists), all with vault2's and the
  carry's coldkeys made hotkey accounts by a guardian first. It then tries the griefing again: third‑party
  transfers, and coldkey swaps and reservations naming either coldkey, are refused, the claim still pays,
  root's sweep of the residual is answered by `seed()`, and vault2's list held full by `absorbStake`. It has
  not run yet. It must pass before anyone creates the carry contract, and before the Safe schedules the
  operation of §3.1.

### 4.7 Missed steps

| What is missed | What happens |
| --- | --- |
| No census committed in `[end(k), end(k)+600]`, also when the v1 coordinator is paused across that window | No draw for `k`: no winners, and all of `F_k` joins the pots slot by slot. Pending awards are still paid if `k` matured. |
| A canonical census (the committed census is not canonical) | No draw for `k`, exactly as with none committed. The commitment is write‑once, so refusing the root instead would leave `k`, and every root after it, unbuildable (§8 check 1). |
| The root for `k` (late, refused, deleted or replaced) | The next root covers `k` with its committed census and draw, which are fixed inputs, as provider rows are. The carry never left the pool, so nothing is lost. |
| The close for `k` (deferred) | `f_k = 0`, and the epoch's emission and returned carry fund the next captured epoch (§6.4 of `VERSION2-FINAL.md`). `k`'s draw still runs, with zero slot amounts and, unmatured, its pot shares as pending awards. |
| The carry's return (late root, relay down, vault2's list held full, or a residual root's sweep cleared until `seed()`) | `k` is not matured. Winners get their slot amounts now and their pot shares as pending awards. |
| Validator support (no fresh funding) | `F_k = 0`, and draws continue. A rounding shortfall comes from the largest pot (§4.5). Pots and pending awards pay whenever the carry is back. |
| The CTF UID (pruned) | No emission until someone re‑registers it with `registerPoolUid`. |

## 5. The ledger

The CTF pool's ledger keeps v2's accounts, sum tree, claim proofs, path checks, objects and canonical‑JSON
rules (`VERSION2-FINAL.md` §6). Only the per‑epoch inputs differ:

```
header:   {"schema":"urnetwork-ctf-ledger-v1","no_id":N,"pool_id":P,"epoch":E,"countries":"sha256:…",
           "carry_coldkey":"0x…","epochs":[{"epoch":K,"census":"sha256:…"|null,"round":R,"awards":"sha256:…",
           "pots":"sha256:…","pending":"sha256:…"},…],"accounts":["sha256:…",…]}
award row (117 bytes):    role (1) ‖ country (2) ‖ rank (1) ‖ unit (32) ‖ client_id (16) ‖ coldkey (32) ‖ slot_rao (16) ‖
                          pot_rao (16) ‖ deferred (1: pot share paid now 0, pending 1)          ≤ 2,490 rows, by role, country, rank
pot row (40 bytes):       country (2) ‖ role (1) ‖ units (4) ‖ winners (1) ‖ pot_in (16) ‖ pot_out (16)   always 498 rows
pending row (60 bytes):   award epoch (8) ‖ role (1) ‖ country (2) ‖ rank (1) ‖ coldkey (32) ‖ amount (16)
account row (48 bytes):   coldkey (32) ‖ cumulative (16), as v2
```

- **Header fields.** `census` is the committed census header's reference, or null when none was committed.
  `round` is `ρ_k` for an epoch with a draw, else 0. `countries` names the list object, the 249 codes in
  order.
- **Pot and pending rows.** `pot_in` is the pot carried in, less any shortfall taken from it (§4.5). The
  pending rows are the pending awards outstanding after the epoch. An empty list is the empty object.
- **Accounts.** A coldkey's delta is the sum of its slot amounts, pot shares and paid pending awards across
  the covered epochs. The carry contract's coldkey (`carry_coldkey`) receives the walk's remainder (§4.5).
  New accounts are appended in ascending coldkey order. Positions, coldkeys and cumulatives never change,
  except to grow.
- **Commit.** `ledgerHash = sha256(header)`. The commit is `(root, total = fundedThrough[e], leafCount,
  ledgerHash)`, as for every v2 pool.
- **Fixed inputs.** An epoch's draw and awards are built once, at the first root that covers it. Rebuilding
  them needs the explicit operator command naming the epoch, which the validator accepts only for an epoch
  whose component it refused (§6.4 of `VERSION2-FINAL.md`). Its census is fixed by its write‑once commitment
  and is never rebuilt.
- **Bounds.** A root covers at most 96 epochs, and its header stays under 256 KiB. A census of 100,000
  candidates is about 5 MB.

## 6. Worked example

**The epoch.** Take one epoch at epoch 4's scale with no head fleet. The CTF pool's fresh amount is
`F_k = 119,583,893,572` rao (119.58 α), and `S = 2,490`, so `a = floor(F_k / S) = 48,025,660` rao. The
remainder of 172 rao goes one each to the first 172 slots: every slot of AD through BA (17 countries) and
BB's provider ranks 1 and 2. None of the rows below is among them. The carry from the epoch before has come
back, so the epoch is matured.

| Country, role | Units | Winners | Pot in (rao) | Each winner gets (rao) | Pot out (rao) |
|---|---:|---:|---:|---|---:|
| DE, provider | 1,840 | 5 | 0 | 48,025,660 | 0 |
| DE, extender | 3 | 3 | 96,051,320 | 67,235,924 | 134,471,848 |
| IS, provider | 2 | 2 | 144,076,980 | 76,841,056 | 230,523,168 |
| IS, extender | 0 | 0 | 960,513,200 | — | 1,200,641,500 |
| TM, provider | 1 | 1 | 12,006,415,000 | 2,449,308,660 (2.45 α) | 9,797,234,640 |
| KP, provider | 0 | 0 | 12,006,415,000 | — | 12,246,543,300 |

**Reading the rows.**

- **DE extenders.** The three units all win, and their tickets rank them. Each winner gets `a` plus a fifth
  of the 2‑slot pot that DE's extenders left the epoch before. The other two fifths carry, with the two empty
  slots.
- **TM.** Its first eligible provider unit takes its own slot plus a fifth of the 250 slot‑amounts that 50
  epochs of empty slots built: 2.45 α. The other four fifths carry, with its four empty slots.
  - If it stays alone, it takes a fifth of what is left each epoch: 8.63 α over five epochs and 12.26 α over
    ten.
  - Any other provider that becomes eligible in TM takes its own fifth from then on.
- **KP.** KP has no candidate, so both its pots grow by `5a` a role, 0.24 α each.

**If the carry were late.** Suppose the previous root finalized after `k`'s close. Then `R_k = 0`, `k` is not
matured, and `F_k = f_k`. TM's winner would get `48,025,660` rao now and a pending award of `2,401,283,000`
rao, paid in full in the first matured epoch. The pots move exactly as in the table.

**The root.** Every slot amount and pot share paid goes to its winner's coldkey. Everything else (every pot
out and any pending award) goes to the carry contract's account. That account's delta is the epoch's
funding less what was paid, so the root's total is `fundedThrough[k]`.

**At today's coverage** about 560 of the 2,490 slots could fill: about 440 provider slots in the 109 provider
countries and about 120 extender slots. So roughly three quarters of each epoch's fresh amount carries. At
epoch 4's scale that is about 90 α an epoch, on the order of 4,500 α within a year, cycling through the carry
contract.

## 7. Incentives and attacks

**Location spoofing is the main pressure.** A pot rewards whoever appears to be in its country. An address
block that GeoLite2 places in a thin country can be bought or claimed (geofeeds, misplaced cloud ranges).

- **What blunts it.** The server's location precedence ranks the derived location from co‑signed pings
  first, then the operator's egress probe, then GeoLite2. A valid location needs one location and one
  address hash per family. And the owner's presence rule needs half an epoch in the country.
- **Later options, by signed entry:**
  - require a derived location that agrees, for countries whose pot exceeds a threshold;
  - alert on every capture above a size;
  - publish the location source of each winning unit.

**Units turn Sybils into coverage.** Packing hosts into one /29 or /48 buys one ticket. Spreading across
blocks buys more, as in the extender pool (§8.6.3).

- In a dense country a slot is worth about 0.048 α, and a ticket's chance is about 5 in the number of units.
- In a thin country, a fleet holding five units takes all five slots. That is five blocks of real presence
  there, which is what CTF pays for.
- ASN units (§8.6.3, S2) remain a later option for both pools.

**Boundary gaming is closed** by the presence rule (owner decision). A host started near `end(k)` has none of
the 336 samples it needs.

**The fifth rule slows a pot grab.** A winner takes a fifth of its country's pot per epoch, so draining a big
pot takes many epochs of continued eligibility: five epochs alone take about two thirds of it. A spoofed or
short‑lived capture therefore pays little before the operator can see it. Honest miners who join in the
meantime take their own fifths.

**The operator attests the census.** It could list undeclared hosts of its own in empty countries and take
their pots, or drop competitors. Several things bound this:

- E5 excludes the networks it declares.
- The on‑chain commitment before drand rules out targeted picks. To favour anyone, the operator must exclude
  everyone else in that country and role, and that shows up in the published counts and exclusion reasons.
- A miner can see its own status and reason (`GET /sn/ctf/client`, §9.1).
- As §8.6.3 and §12.3 already say for the extender census and the deposit audit, this needs review before
  any other operator is admitted.

While one organisation is owner, operator and validator, publication guards against bugs and compromised
keys, not against the owner.

**Validators** pay CTF only by majority (§2.3). The cost of disagreeing with consensus is about 1/100 of a
row.

**Pots that never pay.** By owner decision all 249 codes have slots, so the pots of the seven territories
without a permanent population, and of countries with no realistic presence, grow without bound. They stay
visible on the grid. They cost only the carry cycle's gas, and every rao of them stays in the CTF pool's
accounts. A later signed entry could cap pots or release such pots to the countries that have winners.

**Privacy.** The census publishes a country and a keyed unit hash for every candidate's client id. Award rows
add the winners' coldkeys. No address is ever published.

- An extender's client id can now be tied to a country. That gives no route to its address, which the
  directory design keeps rationed (§8.6.4).
- A winner's coldkey is tied to its country in public. Miners who mind can consent a separate coldkey.

**Sanctions.** No country is excluded (owner decision). CTF payouts are screened as every payout is today, by
the SDN address list. The grid shows every flag.

**No double pay.** Head‑bound clients are excluded (P5). The CTF pool is separate from the provider pool's
usage rows and from the extender pool's share rows (§8.6). A miner can earn from all three: usage, its unit's
share of `Z'`, and a CTF slot.

## 8. Verification and evidence

**The auditor's CTF audit.** It runs for every covered epoch `k`, alongside the provider pool's ledger audit,
and approves nothing that does not reproduce exactly:

1. **Census.** If `ctfCensus(n, k)` is set, the header's census reference is it, and the census objects are
   available with the bytes their references name. Objects served with other bytes are missing, never a
   reason to cancel a draw. A committed census that is not canonical gives no draw, as an uncommitted one
   does: the commitment is write‑once, so refusing the root would leave the epoch, and every root after it,
   unbuildable. Canonical means sorted and unique, one country per client per role, unit families
   consistent, countries in `L`, a header for this pool and epoch, and no object longer than a canonical
   census holds (a 256 KiB header, 131,072‑row chunks, 5,000 exclusion rows). If `ctfCensus(n, k)` is not
   set, the epoch has no draw.
2. **Seed.** `ρ_k` follows from the timestamp of `B_k`. The pulse verifies against quicknet's key, and `σ_k`
   follows.
3. **Draw.** Leave out every candidate with a fleet binding at either boundary (`bindingAt`), from the same
   chain bindings the builder reads, then recompute the winning units, ranks and members. They must equal the
   award rows. A bound candidate is left out before the draw rather than refusing a root with a bound
   winner: a committed census cannot be rebuilt (§4.4).
4. **Amounts.** Take `f_k` from vault2 and `recycledFor[k]` from the carry contract. Walk §4.5 from the
   previous root's pot and pending rows: `R_k`, `M_k`, `F_k`, any shortfall, the slot amounts, the pots, the
   pending awards and the payments. The pot and pending rows of both roots must be published.
5. **Accounts.** Check the deltas, positions, coldkeys and the carry account, then rebuild the root, its
   total `fundedThrough[e]` and its leaf count.

**Attested and not checkable**, as for the extender census: presence, the checks behind P1 and X1, the
countries, units and address dedupe, the wallets, and the declared networks. A failed component is refused
and rebuilt only on the operator's explicit command.

**What anyone can check.** `snclaim v2 check --no_id 4294967297` runs the same audit from the published
objects, and UR's watcher runs it beside the provider pool's. It reads the fleet bindings where the validator
reads them, the v1 coordinator's `FleetBound` events and `bindingAt` at each drawn epoch's boundaries, so a
census that lists a fleet‑bound client draws alike for both: the client is left out, and the check names it.
Those reads are of past blocks' state, made once the audit draws an epoch, so the check needs an archive RPC,
as the validator does. On a node without one it says so and does not approve; it still challenges a provable
fault or demands an unverifiable root from the published objects, then fails. The CTF statement (§9.1)
publishes the per‑country counts and amounts, so concentration and jumps between epochs show.

## 9. External inspection

### 9.1 The API: `sn/api/operator.yml`

`sn/api/operator.yml` is a new OpenAPI 3.1 file for the operator's public SN routes. It starts with the
`/sn/ctf` routes, and the other `/sn` routes may move into it later. The server's
`api/spec_conformance_test.go` maps the routes it documents to it (`/sn/ctf` and `GET /sn/openapi.yml`, where
the server serves it), as it maps `/competition` routes to `sn/api/competition.yml`. The ledger route is in
`connect/api/bringyour.yml` too, with the same fields.

**The document is the contract.** It was written on 2026‑11‑11 from this section, and where the two differ it
governs:

- Each country row nests its roles (`providers`, `extenders`), with the page's `sentence_name`.
- Amounts are an alpha string and integer rao (`design/ctf/SEO.md` §5.3). The ledger route keeps its decimal
  strings of rao.
- The draw figures (slots won, units, near misses, chance to win) are null without a draw last epoch, so the API
  never claims a default win it has no basis for.
- `pending_draw` reports an ended epoch's census and round until its root is committed.
- A ledger entry's `kind` is `pool` or `ctf`, since from `E_x` the operator's pool pays extenders too.
- `unavailable_pools` applies with `partial=true`. Without it the answer stays whole or a 502, so a client that
  predates the field never takes a partial list for a whole one.
- It adds `GET /sn/ctf/country/{cc}` and `GET /sn/ctf/top` (`design/ctf/SEO.md` §5.3) and `GET /sn/openapi.yml`.

| Route | Access | Returns |
| --- | --- | --- |
| `GET /sn/ctf` | public; `Cache-Control: public, max-age=60` | The current state. Pool facts: `no_id`, `pool_id`, `vault`, `carry_contract`, `carry_coldkey`, `countries_version`, `current_epoch`, `draw_at` (the end of the current epoch). The latest drawn epoch: `last_epoch`, with its root's `status` (pending or finalized), `seq` and `ledger_hash`. That epoch's `funding_rao`, `returned_rao`, `fresh_rao`, `slot_rao`, `paid_rao`, `pending_rao` and `carried_rao`. And one row per country of `L`: `code`, `name`, `providers_paid`, `extenders_paid`, `paid_rao`, `provider_units`, `extender_units`, `provider_carry_rao`, `extender_carry_rao`, `available_rao`, `open_epochs` and `near_misses`. `available_rao` is the two pots plus ten slots at the last slot amount: an estimate of what the country's draw holds this epoch. `open_epochs` counts the epochs since the country last had a winner in either role. `near_misses` counts the miners there that the census left out only for presence or a change of country (§4.3). |
| `GET /sn/ctf/here` | public; `Cache-Control: private, no-store` | `{"country": "is"}`: the caller's country from GeoLite2 on the request's address, or null. Nothing is stored or logged. It lets the page open on the visitor's own country. |
| `GET /sn/ctf/epoch/{epoch}` | public; immutable once finalized | One epoch. The census (`hash`, `block`, `candidates`, exclusion counts), the draw (`round`, `signature`, `randomness`, `seed`), the amounts, the country rows, and the awards: role, country, rank, coldkey, client id, `slot_rao`, `pot_rao`, `pending`. Also the object references (header, census, awards, pots, pending), served by `GET /sn/ledger/object`. |
| `GET /sn/ctf/client` | Bearer client JWT, own client only | This client's CTF status for recent epochs: candidate or not and why, its country and unit family per role, and whether it won, with rank and amounts. |
| `GET /sn/ledger/account` | public, as today | Unchanged fields. Each entry gains `kind` (`pool` or `ctf`) and `label`. With `partial=true`, a pool whose tree cannot be built is left out and named in a new `unavailable_pools` list, rather than failing the whole answer with a 502. |

**Headers.** Every route sends exactly one `Access-Control-Allow-Origin` header, as the site's status feeds
require.

**"Last epoch" and "this epoch".** "Paid last epoch" is the latest epoch with a committed CTF root: its
winners per country and role. Its root may still be pending, and the status says so. "Carried into this
epoch" is that epoch's pot out per country and role.

### 9.2 The ur.xyz `/ctf` page

The page is a new English‑only page, `ur.xyz/ctf`, with a page per country at `ur.xyz/ctf/<cc>`.

- **Its name.** Its H1 is "Capture the flag". Its title leads with plain words, "Earn crypto by sharing your
  internet in 249 countries | UR", because in search "capture the flag" means hacking contests.
- **How it is built.** It follows `/mainnet`: Astro renders the React page to static HTML from each epoch's
  data, and islands add the interaction and the visitor's country. Every figure is then in the HTML that
  search engines and AI crawlers read.
- **Where the design is.** `design/ctf/design.html` is the page's design: the hub and a country page. It
  replaced the canvas "ur.xyz CTF page" on 2026‑11‑11. `design/ctf/SEO.md` has the search, compliance and
  agent findings.

Under the title, a mission statement reads "Providers and extenders in every country". From the top down:

- **Live bar.** The epoch and the time to its draw, the slot amount, the α carried into this epoch, and
  what the last draw paid and to how many winners. Three counts are also filters:
  - **Flags held:** countries whose ten slots were all won last epoch, five providers and five extenders.
  - **Not fully captured**, with one row for providers and one for extenders: countries where at least one of
    the role's five slots went unclaimed last epoch. These are the easy wins: a new miner in that role who
    meets the presence rule wins by default.
  - Tapping Flags held, or one of those rows, shows only its countries, directly under the bar. The other sections and cells animate out
    and the matching cells move up. Tapping it again, or "Show all", reverses it.
  - A not-fully-captured view sorts its countries by their provider and extender pots together, largest
    first.
- **Your country.** A card for the visitor's country, from `GET /sn/ctf/here`, with a picker to change it.
  - It shows the flag on a pole, waving while any slot is open, and what is up for grabs there this epoch.
  - For each role it shows how many of the 5 slots were won last epoch. Then it shows either the pot with
    "not fully captured, a newcomer wins by default", or the units in the draw with the chance to win.
  - The call to action is **Start mining**, with **Run an extender** beside it and the eligibility rule
    beneath: online, in the country, for half the epoch.
- **Easy wins.** Eight countries drawn at random from those with a big pot this epoch, weighted by pot size.
  - The draw repeats on each visit, or with Shuffle.
  - Each country drawn has an open slot, so a newcomer wins by default.
  - A card is marked "So close" where miners were present last epoch but fell short of the presence rule
    (`near_misses > 0`).
  - The seven territories without a permanent population are left out of this draw, though they stay in the
    grid.
- **The grid.** 249 cells, one per code in `L`. Each cell shows:
  - the flag as its icon and the country's name;
  - the provider pot and the extender pot, which are separate pots: each in its role's row, and their sum as
    the cell's headline;
  - one row per role with five pips for last epoch's winners, then an **Open** chip where the role is not
    fully captured, or the chance to win where all five slots were won;
  - the number won and the amount paid last epoch.
- **Next steps.** Hovering, focusing or tapping a "So close" ribbon or an "Open" chip opens a card. It names
  the role, what a newcomer takes this epoch and a button to start. For example: "Run an extender in Laos for
  half an epoch and you win by default: about 1.30 α this epoch, then a fifth of what is left each epoch you
  stay".
- **Chance to win.**
  - It is shown only for a role whose five slots were all won.
  - Its entries are last epoch's eligible units in that role plus the visitor.
  - It is the draw's exact odds (§4.4): 100% at five entries or fewer, else 5/entries. That is 83% at six
    entries, 50% at ten and 5% at a hundred.
- **Large pots are highlighted, in two tiers above a threshold (5 α in the mockup):**
  - **"So close"** cells (a near miss) glow in the site's pink, wave their flags, carry a pixel‑type ribbon,
    and sort first.
  - **"Big pot"** cells show the amount in pink with a "big pot" tag.
  - Motion stops under `prefers-reduced-motion`.
- **Sorting and filters.** The sorts are A–Z (the default), biggest pot, or easiest wins. The filters are all,
  big pots or held, and there is a search box, as /dashboard's country table has. Wherever countries sort by
  pot, it is the provider and extender pots together, and pots within 0.1 α of each other fall back to A–Z.
- **How it works**, in three steps. The fine print says the amounts are estimates and that every draw is
  verifiable (§8).
- **Data.** `GET /sn/ctf` from the site's configured `apiUrl`, refreshed every 5 minutes. When the route
  cannot be reached the page shows "unavailable", never zeros.
- **Flags.** The site has no flag assets today. Bundle an MIT‑licensed SVG set (flag‑icons 4×3) under the
  site's own path, because the network‑privacy gate refuses third‑party hosts. Names come from the API, so
  the site keeps no second country list.
- **Wiring.** `react/src/router.js` `PAGE_ROUTES`, the page map in `App.jsx`, `astro/src/pages/ctf.astro`,
  the route lists of the network‑privacy and visual‑parity tests, nginx's language redirect, and a footer
  link.
- **Number style.** "12.97 α", as /reserve formats amounts.
- **Sizes.** It is one responsive page, which the mockup draws at three widths. Nothing scrolls sideways.
  - **Desktop** is as described above.
  - **Tablet (601–1099 px):**
    - the stats wrap into a grid;
    - Easy wins flows in two columns;
    - the grid has five columns, with winners shown as a count ("2/5") instead of pips;
    - tap to expand replaces hover, because touch screens have none. Tapping any Easy wins card or grid card
      opens it to full width with what hover shows on desktop: each role's pot, chance or next step, and a
      button to start.
  - **Mobile (600 px and below):** as tablet, but Easy wins shows compact cards two across, and the grid
    shows compact tiles three across with the flag, the name, the pots and a P/E status.

## 10. Claims and apps

The claim path needs no new contract call: a CTF account is claimed from vault2 like any pool's. The clients
already loop over every entry of `GET /sn/ledger/account`, which is keyed by `no_id`. What blocks a second pool
today is on the server and in the presentation:

- **Server endpoint.** Today the server records and serves accounts for its own `NoId` only. It records the
  CTF pool's accounts too and serves both pools, each built independently, with `kind`, `label` and
  `unavailable_pools` (§9.1). The route and the claim relay read the CTF pool only while capture the flag is
  on: st.yml's `ctf` block runs it and ctf.yml enables its `api` block. With either off they read no CTF pool,
  so no fault of one reaches a client, and its balances are neither shown nor claimed by the relay until it
  is on again.
- **Claim relay** (`server/controller/sn_claim_relay.go`). Today it covers one pool and keys its candidates by
  coldkey.
  - Its units become (coldkey, pool).
  - A coldkey's "Claim now" claims all of its pools' accounts, and the 30‑day limit stays per coldkey.
  - When one of a coldkey's accounts is due, the relay claims every other account of that coldkey above the
    transfer floor in the same batch. A CTF account is often far below the 1.9 α automatic threshold, and the
    batch adds only about 150,000 gas per claim.
- **SDK.**
  - `SnLedgerAccount` gains `Kind` and `Label`.
  - It asks with `partial=true`, and a pool in `unavailable_pools` shows as unavailable without hiding the
    others.
  - With two or more claimable accounts, the SDK claims them in one `STClaimBatcher` transaction, which is
    already deployed on mainnet. Each claim sent alone is signed at 2.6M gas or more, so batching nearly
    halves the gas balance a device needs for two.
- **Apps** (Android, Apple, Windows, Linux, ur.io).
  - CTF accounts are labelled "Capture the flag", from new `sn_ctf_*` strings in the shared localization
    store.
  - One action claims both pools.
  - Apple, Windows, Linux and ur.io draw only the relay's top‑level status today, so they gain per‑pool rows,
    as Android already has.
  - Optionally, a line from `GET /sn/ctf/client` names the flag a provider captured.
- **Old apps keep working.** The SDK parses entries with `encoding/json`, which ignores the new fields. A CTF
  entry carries exactly the existing ones, so old apps show it as "Pool 4294967297" and claim it like any
  pool. The new labels only have to ship before the first CTF claims, about two days after `E_c` ends.
- **Miner daemon and `snclaim`.** Both already treat each `(vault, noId, index)` as its own unit, so they
  claim CTF accounts unchanged. `snclaim v2 check` needs the CTF audit (§8).

## 11. Changes needed elsewhere, effort and rollout

### 11.1 Components

| Component | Change | Effort | Needed for |
|---|---|---|---|
| sn: `ctf` package | The country list, the census, award, pot and pending codecs, the seed, tickets and members, the walk of §4.5, header v1, and Build and Verify with fixed inputs. Mutation tests and a 100,000‑candidate fixture. The server's builder imports it, so it lands first. | 6–8 days | shadow run, by ~11‑25 |
| sn: contracts | `STSettlement2Ctf` and its layout, `STCtfCarry`, tests, rehearsal cases on the live runtime, and `upgrade-settlement2 review` for the new artifact and its folded registration call | 6–9 days | scheduled by ~11‑22 |
| sn: validator | The three‑way split and its cede rules in the exact weight builder and the treasury production row and replay, CTF pool resolution, the `ctf` setting, the CTF audit and approval, and the amendment entry v2 reader | 6–9 days | weights before `E_c`; audit by ~12‑01 |
| sn: `snclaim`, amendment entry | `v2 check` for CTF, and the v2 line format and its signing | 2–3 days | before `E_c` |
| server: presence and census | The quarter‑hour sampler for both roles, the boundary census (P1–P6, X1–X6, units, each candidate's own country, exclusion counts), and metrics | 5–7 days | sampler by `end(6)` |
| server: CTF driver | Close, census commitment, the wait for `ρ_k`, the draw, build, commit and finalize, then `recycle` after finalization. Alerts on a late commitment, a late carry return, and pot captures above a size. | 4–6 days | shadow at `end(7)`; live by `end(8)` |
| server: API and relay | `/sn/ctf`, `/sn/ctf/epoch/{epoch}`, `/sn/ctf/client`, the ledger endpoint's `kind`, `label` and `unavailable_pools`, `sn/api/operator.yml` and conformance, and the multi‑pool relay | 5–7 days | before `E_c`; relay before the first CTF claims |
| SDK and apps | `Kind` and `Label`, per‑pool availability, the batched claim, `sn_ctf_*` strings, and per‑pool rows on five platforms | 5–8 days | before the first CTF claims |
| web: ur.xyz | `/ctf` and `/ctf/<cc>`, the flag assets, the per‑epoch data import, routes, structured data and the SEO gates (`design/ctf/SEO.md` §6) | 4–6 days | before `E_c` |
| docs | WHITEPAPER §8.5 and a new §8.7, §14's UID budget, an addendum to `VERSION2-FINAL.md`, the README glossary, the mainnet timeline notice | 2–3 days | with each release |

The whole set is about 45–66 engineer‑days, 9–13 engineer‑weeks. Epoch 8 leaves 16 days from 2026‑11‑11, so
the work needs three to four engineers in parallel. Two paths are critical. One runs from the contracts
through the rehearsal and the 72‑hour timelock to the pool's registration, and the operation has to be
scheduled by about 11‑22. The other runs from the server's sampler, live by `end(6)`, to the sn `ctf` package
and the builder for the shadow run at `end(7)`. vault2, the provider pool's ledger and the extender pool do not
change.

### 11.2 Deployment plan: `E_c = 8`

The owner set `E_c = 8` on 2026‑11‑11. It is also the extender pool's first epoch, `E_x` (§8.6.7), so
extenders compete from the first CTF epoch. CTF's extender conditions use E1 and E3–E5 of the extender
census, whose shadow runs in epochs 6 and 7 exercise them.

*The dates,* at the measured 12.0 s per block:

| Epoch | Blocks | Ends about | Root committed by |
|---|---|---|---|
| 6 | 9,499,228–9,549,627 | 2026‑11‑20 18:28Z | block 9,550,828, about 22:28Z |
| 7 | 9,549,628–9,600,027 | 2026‑11‑27 18:29Z | block 9,601,228, about 22:29Z |
| 8 | 9,600,028–9,650,427 | 2026‑12‑04 18:30Z | block 9,651,628, about 22:30Z |
| 9 | 9,650,428–9,700,827 | 2026‑12‑11 18:30Z | block 9,702,028, about 22:30Z |

- Epoch 8's earning window opens at about 2026‑11‑27 18:29Z.
- Its census is committed by block 9,651,028, about 12‑04 20:30Z. Its root is committed with the provider
  root, by about 22:30Z.
- The first CTF claims open once the auditor has approved that root and its 14,400‑block window has passed,
  about 12‑07. The approval's deadline is `end(9)`, about 12‑11 18:30Z.

*On chain and on the subnet.* No subnet hyperparameter changes, and the treasury policy (μ = 1/10, cap 32768)
stays as it is. The changes are the owner's:

1. **One timelock operation** upgrades `STSettlement2` to `STSettlement2Ctf` and registers UR's CTF pool:
   `upgradeToAndCall` with `registerCtfPool(1, maxBurn, 8)` as its call (§3.1).
   - Anyone first funds the timelock with at least the value, `maxBurn × 10^9` wei. The Safe schedules the
     operation, and anyone executes it once it is ready, 72 h later, sending no value.
   - It has to execute before epoch 8 starts, since `registerCtfPool` requires `firstEpoch > currentEpoch()`.
   - Its `maxBurn` leaves headroom over the live burn, because vault2 refunds the surplus and a burn above it
     would revert the operation. The surplus stays in the timelock: moving it out takes another 72‑hour
     operation.
   - **The execution plan,** since anyone can take the derived hotkey inside the executing block (§3.1):
     - the operation is ready days before epoch 8: the Safe schedules it by about 11‑22;
     - the execute is sent right after a block seals, with a gas limit that bounds a refused attempt's cost,
       and retried in the next block;
     - a fallback operation with first epoch 9, under another salt, is scheduled beside it, and the Safe
       cancels it once the first executes (`mainnet/BOOTSTRAP-CONTRACTS.md`, "Capture the flag upgrade").
2. **The CTF UID.** The registration pays SN25's burn and takes one of the 256 UIDs. With no owner trim, a full
   metagraph loses its lowest‑ranked UID that is not immune. The live `immunity_period` has to outlast the
   time from the registration to the first revealed CTF weight, a little over two days.
3. **The carry contract** is created through the deterministic deployment proxy after the operation, since it
   reads the pool hotkey from vault2, with 1 TAO as the call's value, which it stakes on vault2's escrow hotkey
   as its residual (§4.6).
   - Before it, the guardian makes the carry's coldkey a hotkey account, and anyone funds the proxy's mapped
     account above its existential deposit; it is created within the CTF UID's immunity window (§4.6).
   - Its address depends on the carry's exact build and the salt: `CREATE2(0x4e59b448…956c, salt,
     keccak256(creation code ‖ abi.encode(vault2, 4294967297)))`. `sn-mainnet ctf-carry review` pins the
     artifact and the address, preflights the creation, and afterwards checks the code, the residual and the
     opt‑outs (`mainnet/BOOTSTRAP-CONTRACTS.md`, "Capture the flag carry").
   - The salt is fresh, random and kept private until the creation. The amendment entry names the address
     after the creation.
4. **The amendment entry v2,** "from epoch 8", is signed and published during epoch 7. `E_x`'s entry v1 is due
   by the same time (§8.6.7).
5. **UR's validator** weights the CTF UID from epoch 8's first tempo.
   - It holds the stake majority, so it alone makes CTF paid (§2.3).
   - Its row becomes about pool 9/100, CTF 1/100 and the two `ur‑reserve` recipients 9/20 each (§2.1).
   - Commit‑reveal delays the new row, so the first tempos of epoch 8 still pay the old one, as they did at
     `E_s`.
6. **Partner validators need nothing for epoch 8.** Until a release of theirs supports CTF, Yuma clips their
   weights by about 1/100 of the row. `weights_version_key` is not bumped, since support is each validator's
   choice.
7. **The notice** on the mainnet timeline, with `E_x`'s. From epoch 8 the provider pool gets 9/10 of the
   providers' share instead of all of it: about 1,076.26 α instead of 1,195.84 α at epoch 4's scale. It gets
   4/10 once fleets are weighted.

No key changes: the census commitment and the CTF roots use NO 1's root signer, and the auditor set stays as
it is. One new key holds hotkey accounts only: the **guardian** coldkey, which makes vault2's coldkey, and the
carry's, hotkey accounts and never swaps them (§4.6).

*What has to ship, and by when:*

0. **Now:** the guardian makes vault2's coldkey a hotkey account, before anyone reserves it, swaps into it or
   takes it: through `MevShield.submit_encrypted`, then checked with `getHotkeyOwner`. A coldkey swap into
   vault2 needs five days' notice and could stop the operation's pool registration for good (§4.6).
1. **Before `end(6)`, about 11‑20 18:28Z:** the server's presence sampler for both roles, so that epoch 7 is a
   full shadow epoch. It has to be running when epoch 8 starts in any case.
2. **By about 11‑21:** the two contracts, their rehearsal on the live runtime, and `upgrade-settlement2
   review` for the folded call. Anyone then creates the implementation through the deterministic deployment
   proxy. R-9 has to pass before the Safe schedules the operation and before anyone creates the carry (§4.6).
3. **By about 11‑22 18:30Z:** the Safe schedules the operation, with the fallback for epoch 9 beside it. It is
   ready, and executed, about 11‑25 18:30Z, two days before `E_c`. Scheduled by 11‑23 18:30Z, it still executes a
   day before, but with less time to retry an execution whose hotkey was taken (§3.1).
4. **By about 11‑25:** the sn `ctf` package, and the server's census, draw and builder, for the shadow run.
5. **Before epoch 8 starts, about 11‑27 18:29Z:**
   - the operation executed and the carry contract created;
   - the entry signed and published;
   - UR's validator release with the split and its cede rules, the treasury row and replay, CTF pool
     resolution, `ctf: support` and the entry reader. It is deployed while the validator is idle, before
     `E_x`'s validator release (§8.6.7, step 4);
   - the notice;
   - the API and the `/ctf` page. They do not gate the start.
6. **After `end(7)`:** the shadow run. The server builds epoch 7's census, draw and ledger from the real
   samples without committing them, and publishes an unsigned report. The validator's audit runs on it.
7. **By about 12‑01:** the CTF audit and approval, in `E_x`'s validator release, and `snclaim v2 check`.
8. **Before `end(8)`, about 12‑04 18:30Z:** the CTF driver in production: the close, the census commitment,
   the draw, the build and the commit.
9. **Before the first CTF claims, about 12‑07:** the multi‑pool relay, then the SDK and app releases with the
   labels. Until then old apps show a CTF account as "Pool 4294967297" and claim it like any pool (§10).
   ctf.yml's `api` block has to be enabled by then, beside the live driver: the ledger route and the relay
   read the CTF pool only while both are on (§10). Enabled with the driver at `end(8)`, it also shows the
   pending CTF balances from the first root.

*What would move the start to epoch 9:*

- the operation not executed before epoch 8 starts. Scheduled after about 11‑24 18:29Z, it cannot be. A
  griefer who takes the derived hotkey at every attempt until then has the same effect, and the fallback
  operation then registers the pool from epoch 9 (§3.1);
- R-9 not passed in time for the Safe to schedule the operation, which it has to precede (§4.6);
- the carry not created before the entry is signed, since the entry names its address;
- the entry not signed before epoch 8 starts, since the signing tool would then require epoch 9;
- the sampler not running when epoch 8 starts;
- the notice not out before epoch 8 starts.

*What would not:*

- **A shadow mismatch or a late driver.** The operator commits no census for epoch 8, so every slot carries
  (§4.7), and the first winners are drawn in epoch 9.
- **A late validator release,** as long as it lands before the UID's immunity ends. It only shrinks epoch 8's
  funding.

## 12. Decisions

### 12.1 Owner decisions, 2026‑11‑11

1. **A new pool per network operator, with its own UID**, beside the current mechanism.
2. **The shares.** Of the providers' share: provider pool 0.4, CTF pool 0.1, top‑level miners 0.5.
3. **Head cede.** While no top‑level miner is weighted, the head's share goes to the provider pool only, and
   CTF stays at 0.1.
4. **Slots.** A uniform amount to up to 5 providers and up to 5 extenders per country.
5. **Nomination.** The operator nominates candidates from its own reliability data and picks winners at
   random per country: a lottery for eligible miners.
6. **Eligibility.** Online, passing the operator's checks and in the same country for at least half of the
   epoch, and active at its end.
7. **Carry.** Slots that cannot be awarded carry forward to the next epoch, so a country can build a large
   pot.
8. **Carry custody.** The carry is held by an immutable carry contract.
9. **Countries.** All 249 ISO 3166‑1 codes have slots.
10. **Units.** The IPv4 and IPv6 subnet uniqueness rules of the head and extender pools apply: all miners in
    one subnet are equivalent.
11. **Validators** do not probe the CTF pool. Each chooses whether to allocate to it.
12. **Registration.** Miners use the provider pool's registration.
13. **Claims.** The apps claim CTF rewards in the same claim flow as pool rewards.
14. **Inspection.** The pool is externally inspectable, through an API at `sn/api/operator.yml` and a flag grid
    on ur.xyz.
15. **The `/ctf` page.**
    - Flags held are countries whose ten slots were all won.
    - A role is "not fully captured" where at least one of its slots went unclaimed, so a newcomer wins by
      default.
    - The chance to win shows only where all five of a role's slots were won: the draw's exact odds,
      5/entries, or 100% at five entries or fewer.
16. **A fifth of the pot.** Each winner takes its own slot and a fifth of the pot carried into its country
    and role, and the rest keeps carrying. A lone miner in an unclaimed country takes a fifth per epoch, so
    it has to stay about five epochs to capture most of the reward.
17. **Start.** `E_c = 8`, the extender pool's first epoch too, with epoch 7 in shadow (§11.2).

### 12.2 Resolved in this design, for review

| # | Question | Resolution | Why |
|---|---|---|---|
| 1 | How is CTF divided across NOs? | By each validator's own pool weights `ŵ_n` | It needs no CTF measurement, and it follows demand and quality like the pool. |
| 2 | Where does an empty CTF channel's share go? | To pool and head 4 : 5, each ceding to the other when empty | That is today's behaviour whenever CTF is absent. |
| 3 | How does a validator choose? | A `ctf` setting, `support` in the sample config | The user's "support it or not". Yuma makes it a stake‑majority choice. |
| 4 | How are the split and `E_c` fixed? | A signed amendment entry v2, not a successor policy | A new policy hash would leave every payout consent outside the new epochs' domain. |
| 5 | Which on‑chain identity? | The vault2 pool `2^32 + n`, through an `STSettlement2` upgrade | One UID, no fake operator, and the consents' NO domain stays the same. |
| 6 | What sets the uniform amount? | Fresh funding ÷ 2,490, with the remainder by slot order | Returned carry pays only what it is owed. |
| 7 | How is a pot paid? | A fifth to each winner per epoch, and the rest carries | Owner decision (§12.1, item 16). It replaces an earlier even split among the epoch's winners. |
| 8 | Who is excluded? | Head‑bound clients, non‑Public providers, the NO's own networks, duplicate addresses | One tier per client, public coverage only, and no self‑dealing (E3–E5). |
| 9 | Which country for a unit? | Its lowest candidate client id's, one country per client per role. The census keeps each candidate's own country, and the draw places the unit | A unit is one ticket, and the auditor can check the rule from the census. |
| 10 | Can one client win both roles? | Yes | Every miner can be a provider and an extender (§8.6 decision 9). |
| 11 | How is the draw kept honest? | The census committed on chain, then a drand round at least 60 s later | The candidates are fixed before randomness exists, and drand cannot be biased. |
| 12 | What if no census is committed? | No draw, and everything carries | Withholding gains the operator nothing. |
| 13 | What if the carry is late? | Pending awards, paid when it returns (§4.5) | A winner never loses its pot to a delay. |
| 14 | What does the census publish? | Candidates' client ids, units and countries. Coldkeys only for winners, counts for exclusions. | The audit needs no more. |
| 15 | Does anyone check CTF roots? | The auditor's arithmetic and draw audit, needed for finalization | vault2 requires approval. This is not probing. |
| 16 | Where are the routes documented? | `/sn/ctf` in `sn/api/operator.yml`, mapped by the conformance test | As `competition.yml` is. |
| 17 | How does the pool register through the timelock? | One operation that upgrades and registers, with the hotkey derived at execution. It is ready days early, its execute is retried, and a fallback operation for the next epoch is scheduled beside it (§11.2) | It saves 72 h, and a hotkey named 72 h ahead could be registered first by anyone. The derived hotkey is still known once its parent block is sealed, so anyone can take it inside the executing block (§3.1). |
| 18 | Is Kosovo included? | No | XK is not an ISO 3166‑1 code, and the server refuses unnamed codes. |
| 19 | What if a committed census is not canonical? | No draw for that epoch, as with none committed (§4.3, §8 check 1) | The commitment is write‑once, so refusing the root would leave the epoch, and every root after it, unbuildable. |
| 20 | What if the census lists a fleet‑bound candidate? | It is left out before the draw, by the chain's bindings, which the builder and the auditor read alike (§4.4) | Refusing a root with a bound winner cannot be repaired: a committed census cannot be rebuilt. |
| 21 | How much rounding does the walk tolerate? | Two cycles', 8 rao, and a partial return writes off as much (§4.5) | With one cycle's 4 rao, rounding left owed at a root's end never shrank, and pending awards piled up for good. |
| 22 | What if returned carry credits more than the epoch's funding? | The shortfall comes from the pots, the largest first, then pending awards (§4.5) | It is at most a few rao. Without it, `F_k` could go negative, and with no fresh funding pending awards never paid. |
| 23 | Does a pause stop census commitments? | Yes, and the window does not wait: a pause across it means no census, so no draw, for that epoch (§3.1, §4.7) | Pause stops settlement, as it stops closes, commits and finalization (`VERSION2-FINAL.md` §4.3, S5). |
| 24 | How can third parties be kept from blocking the carry's claims for good? | The carry keeps a residual of its own on vault2's escrow hotkey, seeded from the TAO it is created with by its own `addStake`, pins the pool hotkey as its auto‑stake destination, and sets the runtime's opt‑out of third‑party staking‑hotkey growth. `seed()` restores a residual root's sweep cleared. A guardian coldkey makes the carry's coldkey and vault2's hotkey accounts (§4.6) | Both hotkeys the carry's claim and move land on stay on its list, which the runtime then admits whatever the list's length. The opt‑out keeps the list short. A coldkey that is a hotkey account can be neither swapped into nor reserved, so no swap can merge into either coldkey or hand them hotkeys, and no reservation can delay the carry. The seed is outside money, so the ledger's carry owed is unchanged. The escrow hotkey cannot be pinned like the pool hotkey: a destination needs a UID, which the escrow hotkey lost to pruning. |

### 12.3 Later options, not open questions

These are only options, each needing its own signed entry:

- ASN units for both the extender pool and CTF;
- a cap on pots, or a release of territories' pots to countries with winners;
- per‑country slot counts;
- a derived‑location requirement for large pots;
- a wind‑down rule for ending CTF, which must say where the outstanding pots go, since CTF α can leave only
  through CTF accounts;
- CTF pools for operators other than UR.

### 12.4 Open, pending the owner's decision

1. **The guardian's hotkey accounts** (§4.6, §11.2). Making vault2's coldkey a hotkey account now, from a
   coldkey that never swaps it, stops anyone reserving it to delay the carry's return, and stops a coldkey swap
   into vault2 that could fill its staking‑hotkey list for good and refuse every later pool registration, the
   CTF pool's included. The carry's coldkey gets the same before its creation. The owner decides the guardian
   key; whoever associates vault2's coldkey first holds that hotkey account, so it is done before anyone else
   does it.
