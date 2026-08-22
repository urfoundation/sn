# Adversarial review findings (2026-07-02)

Two read-only adversarial reviews run during the parallel build. **Contract fixes are
deferred to a consolidated fix round** because they change the ABI → force `sn/stabi`
regen → rebuild of every binder (stctl, snclaim, server st_controller, validator). Do the
fix round only after the validator + server-chain agents land, then regen stabi and rebuild.

Status legend: **FIX** = apply this iteration · **DOC** = correct the overstated/false doc ·
**DEFER** = design decision or known multi-phase gate; raise with user / leave a seam.

## Contract — FIX STATUS (applied 2026-07-02, 114 tests green, ABI byte-identical → no stabi regen)

- **F3/B fundFeePool** → FIXED: now `onlyOwner` (was permissionless). Regression: `test_fundFeePool_guards` asserts non-owner rejection.
- **C treasury-hotkey** → FIXED: `registerOperator` rejects `minerHotkey == treasuryHotkey`. Regression: `test_registerOperator_guards`.
- **F2 retroactive windows** → FIXED: window params snapshotted per-epoch at close (`epochWindows`); guards read the snapshot, so `setEpochParams` only affects future epochs.
- **E operator cap** → FIXED: `MAX_OPERATORS = 256` bound in `registerOperator` (internal const, ABI-stable).
- **D strict window inequality** → ACCEPTED (config guidance, not enforced): strict `trailsW < finalizeOff` would make the finalized-guard defense-in-depth on commit/submit structurally unreachable and break legitimate overlap tests, for a 1-block edge. Left `<=` with a code comment advising `trailsW < finalizeOff` for a real dispute buffer.
- **F1 + A2 effort mechanism** → DEFERRED (user decision, see below): `claimedEffort` unbound + `coverage`/`pathId`/`index` unsigned. Fixed the FALSE README claim. Launch validators owner-run; fee-pool-only impact.
- **F4 UUPS timelock / F3 rival-NO multi-NO / setSelfColdkey / F5 sweep liveness** → DEFERRED (mainnet/multi-NO items; documented in code + here).

## Contract (`sn/evm/src/STSubnet.sol`) — original findings (114 tests green; gaps tests miss)

> **Two independent adversarial reviews converged on the same HIGH findings.** The second
> is backed by **5 passing exploit PoCs** against the unmodified contract, preserved at
> `docs/review-artifacts/AttackPoC.t.sol.ref` + `contract-review-poc.md`. PoC test names:
> `test_A_claimedEffort_inflation_capturesWholeFeePool`,
> `test_A2_signatureReplay_manyValidLeavesFromOneTrail`,
> `test_B_fundFeePool_stealsPushedDeposit_permissionless`,
> `test_B2_rivalOperator_stealsPushedDeposit`,
> `test_C_minerHotkeyEqualsTreasury_inflatesAccounting`. During the fix round, convert each
> into a regression test that asserts the attack now **reverts**.

### F1 — HIGH · `claimedEffort` fully decoupled from the committed tree — DEFER + DOC (+ raise decision)
`submitTrails` stores a validator-chosen `claimedEffort` (bounded only by uint128); it is
credited verbatim and `claimValidator` pays `feePool[e]·eff/totalEffort[e]`. The
`TrailLeaf.coverage` field is in `trailLeafHash` but **read nowhere else** — the k-sample
check proves only that sampled leaves carry valid sigs at seeded indices; it never sums
coverage or relates the tree to `claimedEffort`. **No dispute predicate examines magnitude**
(`disputeTrailLeaf`/`disputeTrailLeafPair` void only on unknown key / bad sig / index≥count /
duplicate index|pathId). A validator submits one honest small tree + `claimedEffort=2^128-1`
→ captures ~100% of every fee pool, undisputable.
- **Bound:** fee pool only (φ·deposits default 10% + voluntary ω) — **miner principal is
  untouched** (needs NO-committed Merkle proof). Sybil-limited by UID registration.
- **Why deferred, not fixed silently:** (1) `PLAN.md` explicitly defers "effort-bounty
  coverage curve tuning" this iteration; (2) launch validators are **owner-run** (§10/§11.2)
  so no adversarial validator exists at launch; (3) the real fix is a protocol design
  choice. **README deviation-6 claims this is "mitigated by the dispute window" — that is
  FALSE; correct the doc now.**
- **Recommended fix (for the decision):** derive credited effort from the sampled leaves
  instead of trusting the submitted number — credit `(Σ_sampled coverage)·leafCount/k` (the
  seed is post-submission so the sample can't be cherry-picked), and/or bound
  `claimedEffort ≤ leafCount·maxCoveragePerLeaf` with per-sampled-leaf `coverage ≤ max`
  checked, making over-claim disputable.

### F1b/A2 — HIGH · trail-leaf signatures cover only `finalDigest`; coverage/pathId/index are unsigned — DEFER + DOC (raise with F1)
**PoC-backed** (`test_A2_signatureReplay_manyValidLeavesFromOneTrail`). The server + vpk
Ed25519 sigs in a `TrailLeaf` sign only `finalDigest`; `coverage`, `pathId`, `index` are
free. So (1) per-leaf coverage is forgeable, and (2) **one** legitimately server-signed
`finalDigest` replays into unlimited "valid" leaves with distinct `(index, pathId)` — and
same-`finalDigest` is not a `disputeTrailLeafPair` condition (it dedups on index/pathId only).
This removes the "you need N real trails" assumption behind F1, so the two compound: one real
trail → whole fee pool. **Fix belongs with F1's decision:** the server signature must attest
`coverage` (and `pathId`), not just the digest — a wire + `verify_controller.go` signing
change + leaf-struct + contract change. Since F1 is deferred to the user's effort-mechanism
decision, this rides with it. Note: my FINAL-digest-signing change (sign `sha256(finalMessage)`
for 0x402-checkability) is correct and unrelated — it protects the digest; A2 is about fields
*outside* the digest.

### F2 — MEDIUM · `setEpochParams` retroactively moves deadlines of not-yet-finalized epochs — FIX
Every window guard reads the *live* storage param + fixed `epochCloseBlock[e]` (commit/
trails/prove/reseed/dispute/finalize). `setEpochParams` has no per-epoch versioning, so the
owner can shrink `finalizeOffsetBlocks` to finalize a **pending** epoch before its dispute
window elapses (dispute reverts while finalize succeeds) — locking in an un-disputed
fraudulent effort. Directly contradicts the contract's own stated invariant ("setters affect
FUTURE epochs only", D-12). Also `_checkEpochParams` permits `trailsWindow == finalizeOffset`,
collapsing the post-prove dispute window (prove is `≤`, dispute is `<`).
- **Fix:** snapshot the four window params into per-epoch storage at roll time; guards read
  the snapshot, not live params. Require **strict** `trailsWindow < finalizeOffset`.
- External function selectors unchanged → **likely no ABI change** (storage-layout only), but
  regen stabi anyway to be safe.

### F3/B — HIGH · pushed-but-uncredited deposits are stealable — **FIX the `fundFeePool` gate now**, DEFER the rest
**PoC-backed** (`test_B_fundFeePool_stealsPushedDeposit_permissionless`,
`test_B2_rivalOperator_stealsPushedDeposit`). `deposit`/`fundFeePool` validate only aggregate
`getStake(treasury) ≥ accountedStake+amount` and credit the caller's chosen target — never
check who pushed. Two variants: (1) rival-NO `deposit(NO2,amt)` front-runs NO1's push into a
competing pool (documented as acceptable for single-NO launch,
`test_deposit_unattributedPush_...`, README dev-3 — **defer** to multi-NO: per-NO deposit
hotkeys or a `transferStakeFrom` pull if SP-1 finds one); (2) **`fundFeePool` is
permissionless**, so *anyone* absorbs a NO's pushed-but-uncredited α into the fee pool — this
breaks the single-NO mitigation **even with one NO** and is elevated to HIGH. **FIX now:**
gate `fundFeePool` to `onlyOperatorOrOwner` (or track per-depositor credit). Leave the seam
for full per-depositor attribution.

### C — MEDIUM · `registerOperator` accepts `minerHotkey == treasuryHotkey` → `accountedStake` double-count — FIX (one line)
**PoC-backed** (`test_C_minerHotkeyEqualsTreasury_inflatesAccounting`). If a pool's
`minerHotkey` equals `treasuryHotkey`, the boundary stake-delta sweep books the whole treasury
as fake pool emission and double-counts `accountedStake` (deposit D → `accountedStake` = 2D),
letting that pool drain other NOs' deposits. Owner-gated misconfig, catastrophic blast radius.
**Fix:** in `registerOperator` require `minerHotkey != treasuryHotkey` (and likely
`minerHotkey` distinct from every other pool's + from `selfColdkey`).

### F4 — MEDIUM/LOW · UUPS upgrade is the real escape hatch around "finalized claims sacrosanct" — DOC (+ D-12 mainnet timelock)
`_authorizeUpgrade` is `onlyOwner`, empty body. A hostile V2 could rewrite claim logic /
zero claim storage. The invariant holds only for the current impl; the contract comments
overstate immutability. Guardian *pause* genuinely cannot freeze finalized claims (verified —
claim fns carry no `whenNotPausedST`), so that half of D-12 holds. **Soften the overstated
comments; add an upgrade timelock as a D-12 mainnet item (owner→multisig already planned).**

### F5 — LOW · failed emission sweep → finalized `poolTotal` temporarily unbacked — DOC
`_sweepPool` bumps `poolAccrued` unconditionally but only `accountedStake` on a successful
`moveStake`; `finalizeEpoch` sets `poolTotal` from measured emission even if α is stranded on
the pool hotkey. Claims for that slice revert until the permissionless `sweepPool(noId)` runs.
Self-healing, no double-pay, no loss — claims-liveness only. Add a comment.

### Hardening (LOW/INFO) — note for later
- **E — LOW:** no on-chain cap on `operatorIds`; `finalizeEpoch`/`rollEpochs` loop them, so a
  large set can brick finalize (claims never open). Relies on the unenforced 256-UID cap. Same
  family as the roll-gas item. Consider a MAX_OPERATORS bound.
- **F(self) — INFO/footgun:** `setSelfColdkey` desyncs stake *reads* (stored `selfColdkey`)
  from *writes* (runtime caller-mirror) — custody-critical owner footgun; guard or remove.
- `_rollEpochs` inner sweep is O(min(backlog,32)×operatorCount); MAX_ROLLS bounds boundaries
  not the inner loop — could stall rolls at many pools (256-UID cap). Trivial at launch.
- `sampleK` read live at prove time; `sampleK=0` lets anyone "prove" with empty arrays
  (subsumed by F1's family).
- rao(uint64) vs uint256 ABI at the 0x805 boundary — SP-1-gated; contract arithmetic is
  uint256-safe.
- no zero-check on `guardian` (benign: only owner can pause); native TAO `receive()` has no
  withdraw (locked, by design).

### Verified SOUND (no issue)
Registration binding D-10 (domain-separated replay-proof `vpkBindDigest`, `coldkey==mirror`
enforced); dedup+reentrancy (flags set before `_payout`, both `nonReentrant`); epoch machine
(no double/early finalize, exact non-compounding carry); Merkle OZ double-hash (64-byte
second-preimage defeated) for both leaf types; initializer (`_disableInitializers` + atomic
proxy init). The `Σ floor(shareBps·poolTotal/BPS) ≤ poolTotal` invariant holds by the
cumulative cap (load-bearing line), not by any on-chain `Σ shareBps ≤ 10000` check.

## Effort-hardening + head-tier review (2026-07-02, v1 second push)

Cryptography is SOUND (verified): coverage-binding (both sigs over `sha256(finalDigest‖coverage)`,
Go/Solidity byte-identical, golden-pinned; k=0 credits 0; no path credits unsigned coverage),
head-binding identity (Ed25519 clientId sig — can't steal a victim's clientId), rebind re-point
(proves both keys, touches no unrelated binding), full domain separation (chainid+contract+sender+
hotkey+clientId), steering fail-closed (dead/unbound UID skipped, pool provider never wrongly dropped).
Two coordination/economic issues:

### HF-1 — HIGH · head-tier double-pay: server excludes by point-in-time `active` at close, validator pays per-tempo across the epoch — FIX (server)
`server/model/st_model.go:888` (`WHERE active`, no epoch window) vs `sn/validator/steer.go` (weights the
live binding every tempo). A head provider stays bound all epoch (earns head emission ~every tempo),
calls permissionless `unbindHead` at `close-1`, and at payout-compute time is no longer `active` → its
network is INCLUDED in the pool payoutRoot for E's full window → **pool leaf + head emission = double
pay**, repeatable every epoch. **Fix:** exclude any ckey that was head-bound at ANY block in
[E_start, E_close], not just active-now — reconstruct from the `HeadBound`/`HeadUnbound` event log
(already synced to `st_event`): excluded if (most-recent event before E_start is a Bound) OR (any Bound
in [E_start,E_close]). (Equivalently track `last_bound_block` + `update_block` and test interval overlap;
the event-log form is fully correct incl. multi-cycle.) Both server + validator must agree on the epoch's
head set.

### HF-2 — MEDIUM · effort estimator grindable via `reseedTrailSamples` (~28 draws), leafCount unverified — FIX (contract, ABI-stable)
`STSubnet.sol` `reseedTrailSamples` (guard `block>seedBlock+256`) is a keep-the-best resample oracle:
~⌊trailsWindow/256⌋≈28 draws on mainnet. Inflates credit two ways, both uncapped by `min(estimate,
claimedEffort)` (declare high): coverage-variance bias (~1.4× at k=8, σ≈6 for M∈[4,16]) and **leafCount
inflation** (declare `leafCount>` real tree size, grind until all k samples land in-range; undisputable —
no on-chain check that the tree has `leafCount` leaves). The in-code "can't cherry-pick the sample" claim
(`:903-905`,`:792-794`) is FALSE. Bounded to the fee pool, Sybil-limited, owner-run at launch (no
adversarial validator yet) → MEDIUM. **Fix:** cap reseeds per (e,valId) at a small constant (internal
`reseedCount` mapping + `MAX_RESEEDS=2`, no ABI change) → grind drops from ~28 to 2 draws (≈2× → ≈1.25×
residual); correct the false comment. **Residual documented:** full close needs `leafCount` committed at
submit (ABI change) — deferred to the permissionless-validator hardening (owner-run launch is safe).

### HF-3 — LOW/MED · server head-exclusion is network-granular (multi-provider under-pay) — DOC (confirmed conservative, never double-pays; the known v1 caveat).
### HF-4 — LOW · validator self-mask fails OPEN on metagraph flakiness (`steer.go` `selfUid()` nil→not deleted) — FIX (validator, small): treat a self-UID read error as fail-closed (skip the tempo's commit or cache the self-UID) so the validator can't self-weight.
### HF-5/HF-6 — LOW/INFO · stale `active` binding on un-unbound dead hotkey (self-inflicted, LOW); no reorg rollback + same-block bind/unbind ordering rests on log-order (DB guard is block-granular — add `log_index` to the guard or assert ordering). DOC/minor.

### Effort/head — FIX STATUS (applied 2026-07-02; all repos green, ABI byte-identical → no stabi regen)

- **HF-1** → FIXED (server): the epoch close now excludes every ckey whose bound interval
  overlaps [E_start, E_close], replayed from the synced event log — `GetHeadBoundCkeysInEpoch`
  (`ORDER BY block_number, log_index`) wired into `StComputeEpochPayout`; the point-in-time
  `GetActiveHeadBindingCkeys` is REMOVED (its active-at-close semantics was exactly the bug).
  The replay core is factored pure (`StHeadBoundCkeysFromEvents`) with serviceless tests incl.
  the unbind-at-close-1 dodge (`model/st_model_test.go`). The validator side needs no change:
  it weights the live binding per tempo, which is exactly "bound at that tempo" — both sides
  now agree on the epoch's head set.
- **HF-2** → FIXED (contract, ABI-stable): `reseedCount[e][valId]` + `MAX_RESEEDS = 2`, and
  `reseedTrailSamples` is now validator-only; the false "can't cherry-pick" comments are
  replaced with the honest bounded residual (coverage-variance within ≤2 draws + unverified
  `leafCount`; full close = commit `leafCount` at submit, deferred to permissionless-validator
  hardening). Regression `test_reseed_cappedAtMaxReseeds` (forge 133/133).
- **HF-3** → DOC: stays the known v1 caveat (conservative, never double-pays).
- **HF-4** → FIXED (validator): `selfMask` caches the last definitive metagraph answer; a read
  error reuses it, and with none `SubmitOnce` SKIPS the tempo's commit — fail closed (CRv4
  keeps the last committed vector active, so a missed tempo is recoverable). A definitive
  not-found clears the mask so a stale UID never zeroes its new holder. Test
  `TestSelfMaskFailsClosed`.
- **HF-5** → DOC: self-inflicted (a dead-hotkey binding earns neither tier until the provider
  calls `unbindHead`); noted in the status caveats.
- **HF-6** → FIXED (minor) + DOC: `stApplyHeadBindings` sorts explicitly by (block, log_index)
  instead of trusting eth_getLogs ordering end-to-end, so the block-granular `update_block`
  guard only ever sees ordered applies; `st_head_binding` is an ops/debug mirror — the payout
  exclusion replays `st_event` directly with its own ORDER BY. Reorg rollback stays out of
  scope: the sync is cursor-forward and the exclusion read runs at epoch close, long after the
  window's blocks finalize.

## `/verify` subsystem — R2 complete

> **Integration coverage (2026-07-03):** the applied V-fixes now carry live pg/redis
> regressions — `server/model/verify_model_db_test.go` (V3 lock, V4/V5 meters, egress
> bijection, reaper/rollup) and `server/controller/verify_controller_db_test.go`
> (end-to-end SEED→…→FINAL with all four sigs verified, §4.3 replay, V1 poison/real
> error equality, V2 stable synthetic seed, V5 hard limit).

Core is **sound**: identity binding (`vpk==GetClientPublicKey(client_id)` before any real
trust; wrong client_id→poison), no cross-type sig replay (CTX‖msg_type distinct), cached-
response idempotency can't be poisoned (re-verifies sig, exact-match branch), egress bijection
forward-claim atomic (Lua script → ambiguous marker; CGNAT resolves to nothing; read-time
reverse re-check), seed exclusion applied (§7.6), SQL parameterized + idempotent rollup,
reaper wired. Fixes below; V-priority = apply this iteration.

### V1 — HIGH · poison trails distinguishable: EXTEND source-IP check skipped for poison — FIX
`controller/verify_controller.go:531-536`. Real trail: EXTEND whose source IP ≠ pending hop →
400. Poison trail: the whole `if !trail.Poison {…}` block is skipped → 200 + ASSIGN. Two
requests at depth 1 classify real-vs-poison, breaking §9. **Fix:** the poison path must apply
an equivalent (synthetic) source-check branch with identical timing/return shape — better,
never branch observably on `poison`. (package controller — hold until chain agent lands.)

### V2 — MED-HIGH · poison seed returns unstable synthetic hop id in trail[0] — FIX
`controller/verify_controller.go:327-335` → surfaced `:404`. Unresolved-IP poison seed returns
a fresh `server.NewId()` every call; real seed returns the stable provider client_id. Seed
twice, compare `trail[0]`. **Fix:** derive the poison synthetic seed id deterministically from
(trail inputs) so it's stable across identical seeds, matching the real payload shape. (pkg
controller.)

### V3 — MED-HIGH · concurrent EXTENDs double-count confirmations + corrupt hop list (no lock) — FIX
`controller/verify_controller.go:431-707`. Read-modify-write `GetVerifyTrail`…`ConfirmVerifyHopAndAssign`
with no optimistic lock. Two concurrent identical valid EXTENDs both confirm: `c_Y` += 2 for
one `a_Y` (breaks Wilson, `r_Y>1`), hop list `[A,B,B]`, latency histogram stuffed. Corrupts
`Q_p`/liveness (drives 41% miner emission); also trips on honest retry races. **Fix:** per-trail
optimistic lock — Redis `SetNX` guard or `WATCH` on the header around confirm+assign; the
model-side primitive can go in `verify_model.go` (pkg model — safe to add now), wiring in
controller (hold).

### V4 — MED · per-vpk caps bypassable (vpk is attacker input) + EXTEND unmetered — FIX
`:290-302` seed limits keyed on attacker-chosen `vpk` → rotate vpk to reset; `verifyExtend` has
no rate limit + final-EXTEND replay re-fetches cached proof unboundedly. **Fix:** the per-vpk
cap can't be a security control on unauth input — keep per-source-IP as the real limit and add
a per-source-IP EXTEND meter; treat per-vpk as best-effort only. (pkg controller + model.)

### V5 — LOW-MED · SEED does Ed25519 verify + ~4 Redis RTs before the rate limit — FIX (cheap)
`:262-289`. Move the per-IP rate-limit check first (before sig verify / egress resolve) for
early load-shedding. (pkg controller.)

### V6 — LOW · proxy egress release relies on TTL aging, not active clearing — FIX (cheap)
`model/network_client_proxy_model.go:816` feeds the index but has NO `ClearVerifyEgress` on
proxy release (the connection feeder DOES clear on disconnect, `transport_announce.go:281`).
Add a release hook. Mostly contained by the read-time reverse re-check. (pkg model — safe now.)

### V7/V8 — LOW · sampling doesn't re-check egress bijection (V7); validator can slow-roll EXTEND to inflate a provider's latency up to StepTimeout (V8) — DEFER/note
V7: stats noise only (confirm-time backstop prevents miscredit). V8: §7.7-class, bounded by
random assignment + validator-independence; note in spec.

### V9/V10 — INFO · residual timing side-channels (real path hits Postgres for provide-modes + terminal INSERT; poison never touches PG) (V9); FINAL signed over bare 32-byte digest — safe today via CTX‖0x04 in preimage but fragile (V10) — note
V10 also flags: poison trails emit a valid server FINAL sig over synthetic hops; safety rests
on effort-leaf consumers requiring a `verify_trail` row (never written for poison), NOT trusting
a bare FINAL sig. Keep that invariant explicit in the validator's leaf-building.

### V11 — CRITICAL DEPENDENCY (currently satisfied) · everything rests on the nginx source-IP override — ADD ASSERTION
`warp/warpctl/config.go:2125` (+2278,2322,2468): nginx overwrites `X-UR-Forwarded-For`/
`X-Forwarded-For` with `$remote_addr`, so source IP isn't spoofable through normal ingress —
this is what makes §8 attribution + per-IP limits sound. If ANY ingress path reaches
`POST /verify` without it (second ingress, misconfig, direct-to-app, header rename): arbitrary
egress attribution / provider-identity theft, rate-limit bypass, targeted eligibility eviction,
full deanonymization oracle. **Add an explicit test/assertion that `/verify` can never be
served trusting a client-supplied forwarded-for**, and a deploy check.
