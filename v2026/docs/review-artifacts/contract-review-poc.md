# Adversarial security review — STSubnet custody & claims

Scope: `sn/evm/src/STSubnet.sol` (the whole custody/settlement/claims surface).
Read-only review. Context read first: WHITEPAPER §6 + §9.3 + §11.2/§11.3, PLAN.md
D-4/D-10/D-11/D-12, IMPLEMENTATION_STATUS.md, `evm/README.md` deviation notes.
The shipped 114-test suite is green; every finding below is a gap that suite does
**not** cover.

All five PoCs run **green against the unmodified contract** (temp copy of the repo,
real `STSubnet.sol`, mocked precompiles exactly as the repo's own harness mocks them):

```
scratchpad/evm-poc/test/AttackPoC.t.sol   (5/5 pass)
  test_A_claimedEffort_inflation_capturesWholeFeePool   -> cheater got 99.99999999% of feePool, honest got 0
  test_A2_signatureReplay_manyValidLeavesFromOneTrail   -> one signed digest -> N valid leaves, no dispute
  test_B_fundFeePool_stealsPushedDeposit_permissionless -> rando steals a NO's pushed deposit
  test_B2_rivalOperator_stealsPushedDeposit             -> rival NO steals a pushed deposit
  test_C_minerHotkeyEqualsTreasury_inflatesAccounting   -> whole treasury booked as fake emission
run: export PATH=$HOME/.foundry/bin:$PATH; cd scratchpad/evm-poc; forge test --match-path test/AttackPoC.t.sol -vv
```

## Severity summary

| # | Sev | Finding | Location |
|---|-----|---------|----------|
| A | **HIGH** | `claimedEffort` is never bound to the committed tree; a validator credits arbitrary effort and captures the whole per-epoch fee pool — undisputable | `submitTrails`/`proveTrailSamples` L645-746, `claimValidator` L987-998 |
| A2 | **HIGH** | Trail-leaf signatures cover only `finalDigest`; `coverage`/`pathId`/`index` are unsigned and one signed digest replays into unlimited "valid" leaves | `_requireValidLeaf` L838-854, `trailLeafHash` L933-951, `disputeTrailLeafPair` L800-821 |
| B | **HIGH** | Push-then-credit deposits are grabbable: `fundFeePool` (permissionless) and rival-NO `deposit` steal a pushed-but-uncredited deposit; total loss to victim | `deposit` L566-588, `fundFeePool` L594-605 |
| C | **MEDIUM** | `registerOperator` does not reject `minerHotkey == treasuryHotkey`; sweep then books the entire treasury as that pool's emission and double-counts custody | `registerOperator` L428-449, `_sweepPool` L392-416 |
| D | **LOW** | `_checkEpochParams` allows `trailsWindow == finalizeOffset`, collapsing the dispute window to zero at the boundary block | `_checkEpochParams` L1033-1039 vs L728/L833/L887 |
| E | **LOW** | No on-chain cap on `operatorIds`; `finalizeEpoch`/`rollEpochs` loop them — a large set can brick finalize (claims never open) | `finalizeEpoch` L891-904, `_rollEpochs` L364-370 |
| F | **INFO** | `setSelfColdkey` desyncs reads from writes: `getStake` uses stored `selfColdkey`, `transferStake`/`moveStake` use the runtime caller-mirror | `_getStake` L1120-1122 vs `_transferStake` L1141-1143, `setSelfColdkey` L1090-1094 |
| G | **INFO** | Owner UUPS upgrade can rewrite everything incl. custody; the "sacrosanct claims" invariant is a property of *current* code only | `_authorizeUpgrade` L1186 |

Verified-safe (checked, not findings): claim dedup + per-pool cap (`claimMiner` L972-978),
`totalEffort` under/overflow (`_voidEffort` L861-868, uint128 bound L659), no double-credit
of effort, `nonReentrant` on every value-moving entrypoint, atomic proxy+initialize
(no init front-run — `Deploy.s.sol` / `STBase`), payout ordering (state before external
call in `_payout` L1000-1004), fee-pool conservation across roll/carry.

---

## FINDING A — `claimedEffort` is unbound; a validator drains the whole fee pool (HIGH)

**Location:** `submitTrails` L645-677 (takes `claimedEffort` as a free arg, stores it
L668); `proveTrailSamples` L714-746 (credits `sub.claimedEffort` verbatim at L744);
`claimValidator` L987-998 (`amount = feePool[e]*eff/totalEffort[e]`, L994).

**The gap.** `claimedEffort` is a caller-supplied number. The sampled verification
(`proveTrailSamples` → `_requireValidLeaf`) checks that *k random leaves are individually
valid* (in tree + both Ed25519 sigs over `finalDigest`). It **never sums the leaves'
`coverage`, and never relates `claimedEffort` to the tree at all.** `_creditEffort`
(L856-859) writes `effort[e][valId] = claimedEffort`. The README (deviation 6) even
admits *"claimedEffort vs Σ coverage is not verified on-chain"* and claims it is
*"mitigated by the dispute window"* — but **no dispute type challenges the aggregate**:

- `disputeTrailLeaf` (L771-795) voids only if a *single named leaf* is invalid
  (unknown key / bad sig / `index >= leafCount`). Every leaf in an inflated claim is valid.
- `disputeTrailLeafPair` (L800-821) voids only two distinct leaves sharing `index` or
  `pathId`. A cheater uses distinct indices/pathIds.

So the stated mitigation does not cover this attack, and there is **no bond**
(README deviation 7: *"No stake-at-risk forfeiture in v1"*) — the on-chain downside of
inflation is exactly zero.

**Attack (PoC `test_A`, passes):**
1. Honest validator submits a real 7-leaf tree, Σcoverage = 91, proves → `effort = 91`.
2. Cheater (`VAL_ID`) submits a fully valid canonical 4-leaf tree but with
   `claimedEffort = 1e27` (true Σ is 46). `proveTrailSamples` samples the real valid
   leaves → passes → `effort[0][VAL_ID] = 1e27`, `totalEffort = 1e27 + 91`.
3. `disputeTrailLeaf` reverts `"ST: leaf valid"`; `disputeTrailLeafPair` reverts
   `"ST: no double-count"`. Nothing can reduce it.
4. `finalizeEpoch`, then `claimValidator`: cheater receives `feePool*1e27/(1e27+91)` =
   **99.99999999% of the fee pool**; the honest validator's floor-divided share is **0**.

`sampleK == 0` (a supported config, testnet default is 2 but mainnet-tunable via
`setSampleK`, and `submitTrails` L672-675 credits immediately when `sampleK==0`) is
strictly worse: effort is credited at submit with *no* sampling — pure `claimedEffort`.

**Impact.** Total capture of the per-epoch `feePool` (φ·ΣD + ω·OwnerCut) by one validator,
starving all honest validators. This defeats the effort-bounty — "the engine for the
data" (§9.4) — which is the entire economic purpose of the validator channel, and
directly contradicts §9.3's marketed property "effort cannot be fabricated without real
trail-walking." Blast radius is bounded to `feePool` (does not touch miner pools), which
keeps it HIGH rather than critical — but for the bounty subsystem it is a complete break.

**Fix options.** (a) Require `claimedEffort == Σ coverage` and make the sampling
estimate/verify it — e.g. bind `claimedEffort` to `leafCount × sampled-mean-coverage`
with a challenge for over-claim; or credit `effort` as a function of the *proven* sampled
leaves rather than a free scalar. (b) Add a dispute that proves `Σcoverage < claimedEffort`
by supplying the full (bounded) leaf set, or commit `Σcoverage` into the root and check it.
(c) At minimum, post a validator bond that a successful over-claim dispute forfeits, and
add an aggregate-mismatch dispute — otherwise the "optimistic + dispute" model has no
teeth here.

---

## FINDING A2 — signatures cover only `finalDigest`; one signed trail → unlimited valid leaves (HIGH)

**Location:** `_requireValidLeaf` L838-854 and the dispute check L787-790 verify
`ed25519(finalDigest, serverKey, serverSig)` and `ed25519(finalDigest, vpk, vpkSig)` —
**both messages are `leaf.finalDigest` (32 bytes) only.** `coverage`, `pathId`, `index`
are committed in `trailLeafHash` (L933-951) but are **not** in either signed message.

**The gap.** Because the server signs only a 32-byte `finalDigest`, and the contract
cannot see inside it:
- `coverage` is an unsigned, validator-chosen field — forgeable per leaf.
- The *same* `(finalDigest, serverSigR/S, vpkSigR/S)` triple validates in *any* number of
  leaves that differ only in `index`/`pathId`/`coverage`. Reusing one digest across leaves
  is **not** a dispute condition (`disputeTrailLeafPair` fires on same `index` or same
  `pathId`, never on same `finalDigest`).

So a validator who obtains a *single* server-signed trail (or who runs/colludes with any
registered server key — `serverKeys` are global, `setOperatorServerKey` L461-470) can mint
unlimited "valid" leaves with arbitrary coverage, making `leafCount` and the leaf set look
real to the sampler while the underlying work is one trail (or none). This removes the last
assumption behind Finding A ("you at least need N real trails").

**Attack (PoC `test_A2`, passes):** 4 leaves all reuse one `finalDigest`+one sig pair,
distinct `index`/`pathId`, `coverage = 1_000_000` each. `proveTrailSamples` passes;
`disputeTrailLeafPair` on two of them reverts `"ST: no double-count"`.

**Fix.** Sign over the leaf content that matters: make the server/vpk signature cover
`keccak(finalDigest, pathId, coverage, index)` (or the full leaf preimage), and add a
duplicate-`finalDigest` dispute so the same trail can't be counted twice. Reject leaves
whose `coverage` exceeds a governance cap. (This is necessary even if Finding A's
`claimedEffort` binding is fixed, because a `Σcoverage`-bound claim is still forgeable
while `coverage` itself is unsigned.)

---

## FINDING B — push-then-credit deposits are stealable / total-loss griefable (HIGH)

**Location:** `deposit` L566-588 and `fundFeePool` L594-605. Both attribute unaccounted
α — `getStake(treasury) − accountedStake` — to the caller's chosen target, using only the
global `accountedStake` counter (L578-579 / L600-601). There is no per-depositor tracking.

**The gap.** The v1 flow is two transactions: the NO first `StakingV2.transferStake`s α
onto `(selfColdkey, treasuryHotkey)`, then calls `deposit`. Between those, the pushed α is
unattributed and grabbable. README deviation 3 documents this for `deposit`/`commitOperator`
(gated to `operatorAddress`) and calls it *"acceptable for the single-NO launch."* But:

- **`fundFeePool` is permissionless and is not covered by that mitigation.** *Anyone*
  (no operator role) can call `fundFeePool(e, amount)` to absorb the victim's pushed α into
  the fee pool. The victim's later `deposit` then reverts `"ST: stake not received"`, and
  the victim is credited **nothing** — its capital silently becomes validator bounty. This
  holds even in the single-NO launch, so the documented assumption does not save it. A
  validator-attacker can grief-then-claim (Finding A) to convert the stolen deposit into
  its own bounty.
- The rival-NO variant is live the moment there is a 2nd operator (the test harness itself
  registers two; the "promote top miners" roadmap is multi-operator): a rival calls
  `deposit(rivalNoId, amount)` and books the victim's push into its *own* pool.

**Attacks (PoCs `test_B`, `test_B2`, both pass):** rando `fundFeePool` steals a pushed
deposit (victim `DT = 0`, `feePool = DEPOSIT`); rival operator `deposit` books it to pool 2.
In both, the victim's own `deposit` then reverts.

**Fix.** Make attribution non-stealable: either (a) restore an atomic pull
(`transferStakeFrom` if a newer StakingV2 exposes it), or (b) per-NO deposit hotkeys /
per-NO custody slots so a push can only be credited to its owner (README lists both as
"upgrade paths" — they should be prerequisites, not upgrades, given `fundFeePool`), or
(c) at minimum gate `fundFeePool` and require deposits to carry a caller-bound push (e.g.
deposit measures the delta on a per-`msg.sender` sub-slot).

---

## FINDING C — `minerHotkey == treasuryHotkey` inflates custody (MEDIUM)

**Location:** `registerOperator` L428-449 checks `!minerHotkeyUsed[minerHotkey]` (L435)
but never checks `minerHotkey != treasuryHotkey`. `_sweepPool` L392-416 then reads
`getStake(op.minerHotkey)` (= the whole treasury), computes `delta = cur − baseline`,
`poolAccrued += delta`, and on a successful (same-hotkey) move does `accountedStake += cur`
(L406).

**Attack (PoC `test_C`, passes):** after a legit `deposit` of D, the owner registers an
operator with `minerHotkey = TREASURY`. A permissionless `sweepPool(badNo)` books
`poolAccrued[badNo] = D` (fake emission) and inflates `accountedStake` to `2·D`. That pool's
`poolTotal` then includes D it never earned; claiming it drains the real depositor's α.
Custody conservation (the property the Claims tests assert) is broken.

**Impact.** Catastrophic if triggered (drains other NOs' deposits), but owner-gated
(registration is `onlyOwner`), so it is a missing-input-validation footgun rather than a
permissionless exploit → MEDIUM.

**Fix.** In `registerOperator` (and `initialize`/any treasury setter) require
`minerHotkey != treasuryHotkey` and that miner hotkeys are pairwise distinct from the
treasury. Consider also asserting `treasuryHotkey` is not itself a registered subnet UID
that accrues emission.

---

## FINDING D — dispute window can collapse to zero at the boundary (LOW)

**Location:** `_checkEpochParams` L1033-1039 permits `trailsWindow == finalizeOffset`
(`trailsW <= finalizeOff`, L1038). `proveTrailSamples` allows crediting at
`block <= closeB + trailsWindow` (L728); disputes require `block < closeB + finalizeOffset`
(L833); `finalizeEpoch` requires `block >= closeB + finalizeOffset` (L887).

**Scenario.** If governance sets `trailsWindow == finalizeOffset`, a validator can call
`proveTrailSamples` at exactly `block == closeB + finalizeOffset` (prove uses `<=`),
crediting effort at a block where disputes are already closed (`<` is strict), and
`finalizeEpoch` is callable at that same block. Result: credited effort with **no dispute
opportunity at all**. Defaults keep trails (+24h) < finalize (+48h), so this is a
misconfiguration/hardening issue, but it silently removes the only defense (weak as it is,
per Finding A) at the boundary.

**Fix.** Require `trailsWindow < finalizeOffset` (strict), guaranteeing a nonzero
pure-dispute period; optionally require `commitWindow < trailsWindow` too.

---

## FINDING E — unbounded operator set can brick finalize (LOW)

**Location:** `finalizeEpoch` L891-904 and `_rollEpochs` L364-370 both loop over all
`operatorIds`. `registerOperator` `push`es without any cap (L447).

**Scenario.** The design relies on `max_uids <= 256` (PLAN §3.8) to bound the loop, but
nothing on-chain enforces the operator count. If the owner ever registers enough operators
(or a future subnet raises the UID cap), `finalizeEpoch` can exceed the block gas limit and
**revert permanently** — no epoch can finalize, so no epoch's claims ever open. `rollEpochs`
compounds it (up to 32 rolls × N operators × precompile calls per catch-up).

**Fix.** Cap `operatorIds.length` in `registerOperator`, and/or make `finalizeEpoch`
paginate over operators (finalize a range, then flip `finalized[e]` once all ranges done).

---

## FINDING F — `setSelfColdkey` desyncs stake reads from stake writes (INFO)

`_getStake` (L1120-1122) reads `getStake(hotkey, selfColdkey, netuid)` using the *stored*
`selfColdkey`, but `_transferStake`/`_tryMoveStake`/`_burnedRegister` (L1127-1147) call
precompiles whose source coldkey the runtime derives from the caller (the proxy's real
mirror) — see the precompile ABI: `transferStake(destination_coldkey, hotkey, …)` and
`moveStake(origin_hotkey, …)` take **no source coldkey**. If the owner sets `selfColdkey`
to anything other than the real `mirror(proxy)` (the SP-1 escape hatch, L1090-1094), reads
and writes target different substrate accounts and the whole ledger silently misaccounts
(deposits check a 0-balance account; payouts move real α). Normal init keeps them equal
(L317), so this is an owner footgun, not an external attack — but the escape hatch is a
custody-critical setter with no invariant tying the two together. Worth a guard or a loud
doc that `selfColdkey` MUST equal the runtime caller-mirror.

## FINDING G — owner upgrade supersedes the "sacrosanct claims" invariant (INFO)

`_authorizeUpgrade` is `onlyOwner` (L1186). The contract's structural invariant
(no admin path writes finalized claim state) is enforced only by the *current* bytecode; a
malicious/compromised owner can UUPS-upgrade to an implementation that drains the treasury
or rewrites finalized `poolTotal`/`feePool`/`effort`. This is the documented Phase-0 trust
model (WHITEPAPER §6.4: owner multisig, timelock deferred to Phase 1), so it is expected —
noted for completeness because it bounds every custody guarantee above. The Phase-1 timelock
+ guardian split is the intended mitigation and is not yet wired.
