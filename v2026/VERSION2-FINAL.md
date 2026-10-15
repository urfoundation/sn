# Version 2: final plan

Prepared 2026-10-11. This is the plan of record for version 2. It supersedes
[VERSION2.md](VERSION2.md) and Part 2 of
[VERSION2-REVIEW-RESPONSE.md](VERSION2-REVIEW-RESPONSE.md), resolves the meta
review ([VERSION2-REVIEW-RESPONSE-META.md](VERSION2-REVIEW-RESPONSE-META.md),
A1-A6) and records the owner decisions of 2026-10-11.

Two rules govern every choice:

1. **Simplest mechanism that meets the requirement.** Where two designs both
   work, this plan takes the one with less code, fewer contracts and fewer
   moving parts.
2. **Every behaviour has a correctness test.** §15 is the acceptance list. A
   behaviour without a passing test that names it is not done.

Pins: this repository at `56f44577`; the operator server (`server/`) at
`0ef73084`; the app SDK (`sdk/`) at `14625631`; runtime 478 (`850f2755`) on
chain 964, which mainnet has run since block 9,276,925 and which the code
targets (`scripts/runtime-rehearsal/build.sh`;
`docs/spec/runtime-478-audit.md`). The plan was first written against runtime
475 (`d1718c99`). Unprefixed paths are in this repository. The version 2
implementation was written on branch `snv2` in every repository (this
repository at `2f406bac`), and it is on `main` in each: this repository
merged it as `ff186b4c` (VERSION2-HANDOFF.md, section 1). Apart from the
runtime facts and gas figures measured on the rehearsal node
(`scripts/runtime-rehearsal/README.md`; §7.3, §8.1), nothing in this plan
was run against a chain.

## 1. Summary

Version 2 pays every eligible provider in proportion to its verified usage, at
any population size, through a per-account cumulative balance that never
expires.

- **Allocation uses usage only.** The validator's measurement no longer gates
  payment, so the verify cohort stops limiting who is paid.
- **Two new contracts sit beside the unchanged v1 set:**
  - `STSettlementVault2` ("vault2"), immutable, which holds the funds;
  - `STSettlement2`, an owner-upgradeable settlement coordinator.
- **The ledger.** Each epoch the operator publishes a ledger: chunked files
  committed on chain as a Merkle sum tree over accounts. An account is one
  payout coldkey in one pool.
- **Admission.** A root becomes claimable only after a quorum of
  owner-admitted auditors approves it and a 14,400-block window passes. At
  launch the quorum is the validator alone. During the window anyone can
  either prove that the root lowers, replaces or hides an existing account,
  which deletes the root, or demand that the operator open an existing
  account's path on chain, which blocks finalization until it is answered.
- **Claims are cumulative.** One claim pays everything an account accrued
  since its last claim, across any number of epochs. Anyone can claim for
  anyone; the payment always goes to the account's coldkey.

Phase A, the scope of this plan, ships the contracts, ledger, approvals,
claims and migration. Later phases are listed in §16.

**Capacity.**

- The design target and an activation gate is at least 1,000,000 providers
  per operator pool.
- Structural ceilings are far higher. vault2's tree takes up to 2^29
  accounts per pool, and the 256 KiB header lists about 3,500 chunk
  references: about 460M accounts in a single-epoch root, or about 4.7M
  providers in a root covering 96 epochs. Each root's download is bounded by
  the validator's budget (§6.6).
- The verify cohort no longer bounds payment.

## 2. Decisions

### 2.1 Owner decisions (2026-10-11)

| # | Question | Decision | Where |
| --- | --- | --- | --- |
| 1 | What vault2 guarantees by itself | Structural guard: sum tree, fixed account positions, and a challenge window fixed in vault2. A data-availability demand makes the guard hold when the operator withholds data (§2.2) | §4.2, §6.5, §7.1 |
| 2 | Root admission | Affirmative approval by owner-admitted auditors; the validator with quorum 1 at launch; no pool-weight penalty while there is one operator | §7.2, §9.3 |
| 3 | Missed or unadmitted epochs | Preserved: the next root allocates each such epoch from its own usage | §6.4 |
| 4 | Phase A usage authority | Operator usage records and consent-selected wallets, as v1 runs today; full consistency checks by the validator | §6.2, §9.2 |
| 5 | Payment promise; relayer permits | Accrual promise; no permits (§2.2) | §3 |
| 6 | v1 leftovers and escrow surplus | vault2 can absorb stake it owns and take owner-assigned surplus; the v1 sweep pays vault2 | §7.1, §16 |
| 7 | `STValidatorEvidence2` | Not built; approvals live in `STSettlement2`; the v1 evidence journal continues unchanged | §9.4 |
| 8 | Apps and the switch epoch | Activation needs an honest accrual display and v2 claims in the apps, through the SDK. E_s is the first epoch that starts at least two days after the last gate passes | §11, §14 |

The owner decisions of 2026-10-09 stand. There is no retroactive compensation
for the stopgap epochs before E_s, and the earning cutoff stays October 6 with
no new off-chain fallback (VERSION2-REVIEW-RESPONSE.md:1290-1293).

### 2.2 Changes found while writing this plan

Each of these departs from the accepted recommendations, because the code
showed something that was not visible at decision time.

- **A data-availability demand in vault2.** Decision 1's guard only works
  when the pending tree is published. Without the tree, nobody can challenge
  a root. An upgraded `STSettlement2`, or a compromised root signer together
  with the auditor, could then finalize an unpublished root and drain the
  pool. vault2 therefore lets anyone demand, during the window, the on-chain
  opening of an existing account's path. A root cannot finalize while a
  demand is open. A garbage or withheld node can never be opened, so it can
  never be answered. The demand names a finalized root and carries the
  account's leaf and its proof against that root, so a padding or otherwise
  unprovable position, which no successor could answer, can never be
  demanded and block every later root. Any finalized root will do, not
  only the latest. Otherwise an attacker who bypassed approval could
  finalize one root with its tree withheld, leaving watchers proofs for
  almost no account, and then lower the rest where no demand could reach
  them (gate 1 review, H1). Each account of each finalized root can be
  demanded once per pending root, and an answer counts once.
- **No relayer permits.** The sr25519 precompile verifies a signature over an
  exact 32-byte message (`evm/src/interfaces/sr25519Verify.sol`). Talisman
  and some WalletConnect wallets such as Nova sign `<Bytes>…</Bytes>` around
  the message (`sdk/bittensor_wallet.go:289-295`;
  `server/model/auth_bittensor.go:22-25`). Other wallets sign raw bytes, but
  the apps cannot tell in advance which kind a user has. Under the
  simplicity rule vault2 has no permits.
- **No protocol policy change in phase A.** The policy hash, v1 coordinator
  and v1 vault are part of the client-key history domain. Every provider
  wallet consent, network consent and hotkey delegation is signed under that
  domain (`protocol/client_key_history.go:29-58`;
  `protocol/wallet_mapping_consent.go:39`). Changing any of the three would
  orphan every existing consent. Production also refuses a policy succession
  (`validator/recycle_approval.go:128-131`). Policy v1 therefore stays in
  force; §13 lists its fields that are inert for v2 epochs. Any later policy
  change, including a priced deposit policy, faces the same consent-domain
  constraint and needs its own design.
- **The validator does not verify wallet consent chains in phase A.** The v1
  validator does not either; only the offline economic-conservation tool does
  (`mainnet/economic_conservation_provider_originals.go:249-404`). Checking a
  million chains means shipping every consent original as evidence, which
  belongs with the roster phase (§16).
- **Auditors are owner-admitted EVM addresses.** The proposal was a relayed
  hotkey-plus-VPK dual signature. Both seeds sit on the validator host, so a
  second signature adds no defence. An approval is now a transaction from an
  admitted address, which removes the digest and the signature precompiles
  from the path to finalization.

### 2.3 What was cut relative to Part 2 of the response

| Part 2 | This plan |
| --- | --- |
| Three new contracts, policy v2 at E_s | Two contracts; policy v1 throughout |
| OpenZeppelin tree, multiproofs, a ring of four roots | Position-binding sum tree, single proofs, every finalized root kept |
| Claim credit, deferral reasons, batch claims, permits | A claim pays or reverts; batching is an optional helper contract that calls `claim` per item (§10.2) |
| Live `unfinalizedFunding` counter | One funding snapshot per epoch |
| Veto from journal activations | Affirmative approval plus objective challenges and demands |
| Large header, manifests, a signed artifact | One header listing chunk hashes, committed by hash on chain |
| Frame, VRF cohort, stratified estimator, evidence chunks, bonds, Null | Later phases (§16) |
| `captureEscrowSurplus` formula | `absorbStake`, with surplus assigned by the owner from phase C |

## 3. What providers are promised

**Promise.** Every eligible provider's share of an epoch's provider pool, in
proportion to its verified usage, is credited to its payout coldkey's account
and never expires. It is delivered on chain when anyone claims the account.
That is worth doing once the balance is large relative to the claim's gas.

**Eligible** for an epoch means all three of:

- settled work in the epoch's earning window (§6.2);
- a payout coldkey selected by a signed consent. The order is the provider's
  own, then its network's, then a hotkey delegation
  (`protocol/earning_wallet.go:81-109,190-212`);
- no active fleet binding at either epoch boundary. Head fleets are paid
  natively.

**Not promised:**

- payment within a fixed time;
- any amount for a coldkey whose share rounds to zero rao every epoch;
- the share of an epoch whose close was missed by more than its window
  (§6.4);
- compensation for stopgap epochs before E_s;
- payment for a provider without a paying consent. Wallets connected through
  the apps today are login proofs, which release settlement never pays
  (`server/controller/sn_earnings_controller.go:43-45`). Those providers stay
  unpaid, with a zero coldkey in their row, until their network's owner signs
  a network payout consent in an app (§11.1), and only from the epoch after
  the signing on.
  The v1 stopgap of server 9719333c, which pays a provider without signed
  wallet evidence its account-history wallet when no roster is configured,
  does not apply to v2: v2 stays consent-only (owner decision, 2026-10-13).

**Account states** shown to users:

| State | Meaning |
| --- | --- |
| pending | the cumulative in a committed root that is not yet finalized |
| finalized | the cumulative in the latest finalized root |
| claimable | finalized minus `claimed` |
| paid | `claimed`, already transferred |

**Latency.** The provider share is about 2,070 α per epoch, about 1,450 α to
the pool after head fleets' θ = 3/10 (VERSION2-REVIEW-RESPONSE.md:602-608).
That is about 0.0145 α per provider per week at 100k providers. At the
250k-gas planning figure (R-6 measured 187,332 gas for an account's first
claim at depth 29 on runtime 478, and 170,243 for a later one),
a 5 gwei base fee and 0.006587 TAO/α (VERSION2-REVIEW-RESPONSE.md:604-605),
a claim costs about 0.19 α.
That is about 13 weeks of an average 100k-population provider's earnings, and
gas is under 10% of a claim after about 131 weeks. At 1M providers both take
ten times longer. Claimed through the claim batcher (§10.2), twenty or more
claims in one transaction use 147,000 to 149,000 gas each, 17 to 18% less
than alone (R-7). A third party can raise the cost by filling vault2's
staking-hotkey list (§7.3), paying a transfer above the floor per entry:
every claim then uses about 1.1M gas more (R-3: 1,257,365 against 154,831).
At the same prices a claim then costs about 1 α, about 69 weeks of that
provider's earnings, and gas falls under 10% of a claim only above about
10 α. A claim below the runtime's 100,000-rao TAO-equivalent transfer floor,
about 0.0152 α, reverts. An average account crosses that floor after about a
week at 100k providers and after about 10.5 weeks at 1M.

## 4. Trust model and guarantees

### 4.1 Actors

| Actor | Can | Cannot |
| --- | --- | --- |
| **Owner Safe** (1-of-1) | Set the auditor set; register pools; upgrade `STSettlement2`; pause and unpause v1 settlement, and with it v2 settlement | Finalize a root that is challenged, has an open demand, or is younger than the window; change a finalized root; stop claims through the contracts |
| **SN25 owner key** (`ur-owner`, a native multisig) | Disable stake transfers on SN25 (`sudo_set_toggle_transfer`). Every `transferStake` then fails `TransferDisallowed`, which freezes every v1 and v2 claim until transfers are enabled again. The chain's root holds the same switch and can also dissolve SN25 (§17) | Redirect a payment; change a root. The switch does not stop captures, which move stake without the transfer check |
| **Guardian** | Pause v1, and with it v2, settlement (`evm/src/STCoordinator.sol:914-922`) | Unpause; anything else |
| **Operator** (UR server: root signer, ledger publisher) | Close epochs; commit roots; answer demands | Finalize without a quorum of approvals |
| **Auditors** (launch: the validator's admitted address) | Approve the exact pending root before its deadline | Finalize a root that vault2 blocks |
| **Anyone** | Close epochs (a close defers earlier unfunded ones); finalize; challenge; demand a path; claim for any account; absorb vault2-owned stake; re-register a pruned pool hotkey | Redirect a payment |

At launch the owner, the operator and the only validator are one
organisation. Approval protects against bugs and single-key compromise. The
vault's guarantees (§4.2) protect accrued balances against an upgraded
`STSettlement2`, or a compromised root signer plus auditor, as long as
someone challenges or demands during the window. The transfer switch sits
outside every contract, so the SN25 owner key, which controls it, should be
kept under the Safe's governance (§17).

### 4.2 Guarantees enforced by vault2

vault2 is immutable. These hold whatever `STSettlement2` does.

- **G1 Conservation.**
  - `totalCaptured == totalPaid + escrowAccounted`.
  - `escrowAccounted == Σ_pools (capturedTotal − claimedTotal)`.
  - Live escrow stake, the escrow's movable stake (§7.3), is at least
    `escrowAccounted`.
  - Per pool, `claimedTotal <= finalizedTotal <= capturedTotal`.
- **G2 Accrued balances survive a bad successor.** Let F be a pool's latest
  finalized root and P its pending root. Disputes name the finalized root
  they protect, any of the pool's, by its index.
  - Suppose P lowers an account of a finalized root, gives it another
    coldkey, or hides it behind an inconsistent node. Then anyone holding
    P's tree can prove it against that root (`checkPath`), and P is deleted.
  - If P's tree is not published, anyone can demand the opening of any
    account of any finalized root, proven against that root (`demandPath`).
    P cannot finalize until each demand is answered by an opening that shows
    no fault.
  - A withheld newest root does not disable demands. Were demands proven
    against F alone, a bypass could finalize one root with its tree
    withheld. Watchers would then hold proofs against F for almost no
    account, and a successor could lower all the others with nothing left
    to demand them. Watchers instead protect the newest finalized root whose
    tree they hold (§9.3). What a withheld root added over that root is
    protected only by those who hold its tree, and a bypass that withheld
    it could have misallocated that value anyway.
  - If no account of F is faulted, every one of them is claimable in P at its
    old value or more. P adds at most `P.total` minus F's account values,
    which is the new funding whenever F is tight; approval checks tightness.
  - Residual risk: an account that is faulted but never demanded, under a
    withheld tree and bypassed approval, is not protected.
    - A theft of a fraction `f` of F's value escapes `m` value-weighted
      demands with probability `(1−f)^m`.
    - vault2 allows one commit per pool per 14,400 blocks, so a sample
      cannot be re-rolled quickly.
    - Watchers start at `m = 256` and double `m` after each failed root over
      the same F (§9.3).
    - §6.5 gives the argument.
- **G3 Destination.** A claim pays only the coldkey in the account's leaf,
  whoever sends it.
- **G4 No expiry.** Every finalized root is kept. An account can claim
  against any finalized root, up to that root's value less `claimed`, within
  the pool's `finalizedTotal`.
- **G5 Funding binding.** A root's total equals the pool's funding through
  its epoch, snapshotted when each epoch was closed or deferred.
- **G6 Window.** No root finalizes earlier than 14,400 blocks after its
  commit, or while any demand is open.

### 4.3 Properties enforced by `STSettlement2`

`STSettlement2` is owner-upgradeable. These hold for the reviewed
implementation.

- **S1** Only the epoch's root signer commits, inside the commit window, and
  only into a free or dead pending slot.
- **S2** A root finalizes only with a quorum of approvals of its exact
  commit, given before its deadline.
- **S3** A root still unfinalized at the end of the next epoch can be
  replaced. Its epochs' funding stays for the replacement.
- **S4** `settlementAt` returns v1 identities before a pool's first v2 epoch
  and v2 identities from it.
- **S5** v2 settlement stops while the v1 coordinator is paused. The pause
  never stops claims, approvals, challenges or demands. Only the runtime's
  transfer switch, which no contract holds, can freeze claims (§4.1).

## 5. Epoch lifecycle

Windows come from the v1 coordinator's `policyAt(e)`, which is unchanged:
epoch 50,400 blocks and root commit window 1,200. v1's 120-block close grace
does not apply to v2 epochs. vault2 fixes
the challenge window at 14,400 blocks from the commit. v2 needs
`epochBlocks >= rootCommitWindowBlocks + 14,400`; mainnet's 50,400 satisfies
it, and testnet's 360-block cadence does not. `registerOperatorPool` checks
the rule only once, against `policyAt(firstEpoch)`. Nothing checks a later
v1 policy, so keeping the rule for every v2 epoch is a governance constraint
on the v1 policy schedule (§13).

| Blocks after end(e) | Step | Caller |
| --- | --- | --- |
| 0-1,200 | `closeOperatorEpoch(e)` captures the pool hotkey's movable stake (§7.3). The v2 close window is the commit window, not v1's 120 blocks | anyone (server) |
| 0-1,200, after the close | `commitDistribution(e)` | root signer |
| until end(e+1) | `approveDistribution` | auditors |
| until commit + 14,400 | `demandPath`, at most once per account of each finalized root | anyone |
| while pending | `checkPath` challenges or answers | anyone; server answers |
| at or after commit + 14,400 | `finalizeDistribution`, once approved with no open demand | anyone (server) |
| at or after end(e+1) | a root still unfinalized can be replaced by the next commit; vault2 refuses any commit within 14,400 blocks of the pool's previous commit | root signer |

**Ordering.**

- Per pool, epochs are funded strictly in order. Closing epoch e first
  records every earlier unfunded epoch as deferred.
- A pool has at most one pending root.
- The next root covers every epoch since the last finalized root, each
  allocated from its own usage (§6.4).
- The server always finalizes or replaces the old root, reads the latest
  finalized root from chain, then builds and commits. A late change can
  therefore never alter a root's predecessor.

**Senders.**

- A refusal that shows another transaction already did the work counts as
  done: an approval refused `AlreadyApproved`, a demand `AlreadyDemanded`,
  an answer `NoFault` or `NoPendingRoot`, a challenge or finalize
  `NoPendingRoot`, and a claim `NothingToClaim` after a third party claimed.
- A preflight `eth_call` or `eth_estimateGas` runs at the latest block,
  while the transaction runs in a later one, so a window-bound call must
  hold in both. Senders do not aim at a window's last block.

## 6. Ledger

### 6.1 Accounts

A pool's ledger is an ordered list of accounts. Account `i` is
`(coldkey_i, cumulative_i)`, with the cumulative in rao.

- An account's position and coldkey never change. Its cumulative never
  decreases.
- A coldkey has at most one account per pool.
- An account is created the first time a root gives its coldkey a positive
  delta. A root's new accounts are appended in ascending coldkey order:
  unsigned bytewise over the 32 bytes.
- A wallet change sends future deltas to the new coldkey's account. The old
  account keeps its balance and stays claimable.
- A provider promoted to the head stops earning pool deltas. Its account is
  unchanged.
- Accounts are never removed. A pool holds at most 2^29 accounts.

### 6.2 Provider rows

Each covered epoch whose inputs exist has one set of provider rows. A row is
57 bytes, with integers unsigned big-endian.

| Field | Bytes |
| --- | --- |
| `client_id` | 16 |
| `coldkey` (zero when no wallet is selected) | 32 |
| `usage_bytes` | 8 |
| `head_excluded` (0 or 1) | 1 |

Rules, all checked by the validator:

- every row has `usage_bytes > 0`. That the rows are exactly the providers
  with positive settled bytes in the window is attested (below);
- rows are sorted by `client_id` and unique, with no zero `client_id`;
- the usage of all the epoch's rows, eligible or not, sums to less than
  2^64, as v1 requires (`payoutartifact/artifact.go:161-163`);
- `head_excluded` is 1 exactly when the provider has an active fleet binding
  at the epoch's start or end boundary;
- a row is eligible when `coldkey != 0` and `head_excluded == 0`.

**Earning window.** For epoch `k`, the window is contract `close_time` in
`[timestamp(start), timestamp(end))`. The timestamps come from the EVM
headers at the epoch's boundary blocks, as v1 does
(`server/controller/st_payout_epoch_authority.go:116-157`;
`server/model/st_provider_usage.go:55,60,192`). v1 clips the start to
`cutoff_utc` (`server/controller/st_controller.go:3357`). Every v2 epoch starts after the
`cutoff_utc` in `sn.yml`, and the builder refuses a window the cutoff would
clip.

**Attested by the operator** and not checkable in phase A (decision 4): the
population, the usage amounts, window membership and wallet selection. The
sources are v1's (§8.2). Reliability and `a_min` no longer appear.

### 6.3 Allocation

The rule is `coldkey_usage_largest_remainder_rao_v1`. Its inputs are an
amount `A` in rao and an epoch's eligible rows. All arithmetic is exact
integer arithmetic.

1. `W_c` is the sum of `usage_bytes` over eligible rows with coldkey `c`, and
   `ΣW` is the sum of the `W_c`. An epoch whose rows' total usage reaches
   2^64 is refused (§6.2), as v1 does. `ΣW = 0` is refused here; §6.4
   carries such an epoch.
2. `q_c = floor(A·W_c / ΣW)` and `r_c = (A·W_c) mod ΣW`.
3. `R = A − Σ q_c`. The `R` coldkeys with the largest `r_c` each get one more
   rao, ties going to the ascending coldkey.
4. The result `delta_c` sums to `A` exactly, and each value is within one rao
   of the exact share.

Coldkeys are aggregated first, as in v1 (`protocol/payout.go:53-80`), so
splitting a provider does not change its coldkey's amount.

### 6.4 Covered epochs and carry

A root for epoch `e` covers epochs `p+1 … e`, at most 96 of them. `p` is the
previous finalized root's epoch, or `firstEpoch − 1` for the pool's first
root. The funding for epoch `k` is
`f_k = fundedThrough[k] − fundedThrough[k−1]`, read from vault2, with
`fundedThrough[firstEpoch − 1] = 0`.

**Allocation walk.** Start with carry `c = 0` and walk `k = p+1 … e`:

- If `k` has provider rows with `ΣW > 0`, allocate `f_k + c` from its rows
  (§6.3) and set `c = 0`.
- Otherwise add `f_k` to `c`.

Epoch `e` must allocate, so the walk ends with `c = 0`. A root is committed
only when its total exceeds the previous root's total, so every root has at
least one account.

**When value moves to later earners.** Only in these cases:

- **No eligible usage.** The epoch has no rows (`providers: []`) or `ΣW = 0`.
- **Unrecoverable.** The operator declares the epoch's inputs unrecoverable,
  shown as `providers: null`. It does so only by an explicit command naming
  the epoch, never as a fallback on error. The validator records every
  declaration and refuses it for an epoch whose valid rows it already holds.
- **Missed close.** A close missed beyond its 1,200-block window defers the
  epoch with `f_k = 0`. Its emission stays on the pool hotkey, and the next
  close captures it. A capture that the runtime refuses (dust, or a runtime
  failure) reverts and has the same outcome. The server alerts on any missed
  close.

Every other missed, dropped or challenged epoch is allocated later from its
own usage. This is decision 3.

**Fixed inputs.** A covered epoch's provider rows are built once, at the first
root that covers it. Later roots list the same chunks. Rebuilding an epoch's
rows needs an explicit operator command naming the epoch (§8.6), which the
validator records, and the validator accepts new rows only for an epoch
whose earlier rows it refused (§9.2).

**Deltas.** An account's delta is the sum of its coldkey's deltas over the
root's allocated epochs.

### 6.5 Objects and the sum tree

**Objects.** A root's ledger is a set of content-addressed objects in the
operator's blob store, served publicly and retained by the validator. The
objects are:

- **Header** (JSON, at most 256 KiB). `ledgerHash = sha256(header bytes)` is
  committed on chain:

  ```
  {"schema":"urnetwork-payout-ledger-v2","no_id":N,"epoch":E,
   "epochs":[{"epoch":K,"providers":["sha256:…",…] | null},…],
   "accounts":["sha256:…",…]}
  ```

- **Provider chunks**: 57-byte rows (§6.2), globally sorted by `client_id`,
  131,072 rows per chunk except the last. `providers: []` means no rows.
- **Account chunks**: 48-byte rows `coldkey(32) ‖ cumulative(16)` in account
  order, 131,072 rows per chunk except the last.

**Canonical form.** A header is canonical only if decoding it with unknown
fields refused, then re-encoding it, reproduces the bytes exactly. The rules:

- compact JSON with no whitespace and no trailing newline;
- keys in the order shown, all present;
- `null` only for `providers`;
- integers as JSON numbers;
- chunk references as `sha256:` followed by 64 lowercase hex digits.

Everything else a verifier needs it reads from chain: boundaries, block
hashes, funding, the previous root and the pending root.

**Writing objects.** The operator writes every object with put-if-absent
under its sha256 (`server/startifact/artifact.go:109-153`). Objects are not
signed: the root signer's on-chain commitment authenticates the header, and
the header authenticates the chunks.

**Sum tree** `urnetwork-ledger-sum-tree-v1`. Let `n` be the number of
accounts (`1 <= n <= 2^29`), and `d = 0` if `n == 1`, else `⌈log2 n⌉`.

```
leafHash(c, v)        = keccak256(0x00 ‖ c ‖ uint256(v))                        65 bytes hashed
nodeHash(hL,sL,hR,sR) = keccak256(0x01 ‖ hL ‖ uint256(sL) ‖ hR ‖ uint256(sR))   129 bytes hashed
level 0, position i   = (leafHash(c_i, v_i), v_i) for i < n;  (0x00…00, 0) for n <= i < 2^d
level k+1, position j = (nodeHash(L.h, L.s, R.h, R.s), L.s + R.s) for L, R = positions 2j, 2j+1 of level k
root = level d, position 0; committed with total = root sum and n
```

Values are below 2^128; on chain the sums are uint256 with checked arithmetic.

**Claim proof** (bottom-up): `d` siblings `(hash, sum)` for levels 0 … d−1.
Bit `k` of the index says whether the running node is the right child at
level `k`. A proof is valid if and only if:

- `index < n`;
- it has exactly `d` siblings;
- folding from `(leafHash(c, v), v)` reproduces both the root and the total.

**Path check** (top-down), used by `checkPath` for an account of a finalized
root `F_k`, any of the pool's, named by its index `k`, at an index `i` below
`F_k`'s account count. Leaf counts never decrease, so `i` is also below P's:

- **Inputs.** `F_k`'s leaf `(c, v)` at `i`, with a valid claim proof against
  `F_k`, and `d` preimages `(hL, sL, hR, sR)` for P, from the root down along
  `i`'s path.
- **Walk.** Start from `(P.root, P.total)`.
  - Each preimage must hash to the current node, or the call reverts as
    malformed.
  - The node is a fault if `sL > s` or `sR != s − sL`.
  - Otherwise descend to the child on `i`'s side.
- **Leaf level.** The stated node `(h, s)` is a fault if `s < v` or
  `h != leafHash(c, s)`. When `d = 0`, the leaf level is `(P.root, P.total)`.
- **Outcome.** A fault deletes P. A complete opening with no fault answers
  an open demand on `(k, i)`, once. With no open demand on `(k, i)`, never
  made or already answered, the call reverts and changes nothing, as it does
  on any malformed input. Demands on one account against two roots are two
  demands, each answered against its own root.

**Why G2 holds.**

- Call a position of P *provable* if a claim proof for it verifies.
- **Provable values never exceed the total.** Every internal node commits to
  both children's stated sums. A proof through a node verifies only if those
  sums add to the sum its parent states for it. Inducting from the root
  downward, the provable leaves under any node sum to at most its stated sum.
- **Unfaulted accounts are provable.** If an account of a finalized root has
  no fault in P, it is provable in P with that root's coldkey and at least
  that root's value.
- **Bound on new value.** Together these bound the value P places outside F's
  accounts, plus every increase of F's accounts, by `P.total` minus F's
  values.
- **Faults are found where someone looks.** A faulted account is caught by
  a challenge when the published account rows reproduce the tree, and
  otherwise by a demand when its index is sampled. A tree rebuilt from rows
  is always consistent, so an inconsistent node is never reproduced from
  published rows: watchers report such a root unverifiable and demand. An
  inconsistent, garbage or withheld node on the path can never be opened
  without showing the fault, so any opening under it deletes P. An index of
  a withheld tree that nobody demands is §4.2's residual risk.
- **The pool cap holds.** Absent an unchallenged decrease, the latest root
  dominates every older one, so `claimedTotal <= finalizedTotal` never stops
  an honest claim. V-16 covers the exception.

### 6.6 Bounds

| Bound | Value |
| --- | --- |
| Header | at most 256 KiB, at most 96 covered epochs, about 3,500 chunk references |
| Account chunks | 6 MiB each |
| Provider chunks | about 7.1 MiB each |
| Accounts at 1M | 48 MB (about 46 MiB, 8 chunks) |
| Provider rows at 1M, per covered epoch with rows | 57 MB (about 54 MiB, 8 chunks) |

The validator fetches at most `--ledger_max_bytes` of objects that are new to
its custody per root (§9). The default is 2 GiB. That is a single-epoch root
of about 20M providers when the previous root's accounts are already held.
At 1M providers it covers about 36 epochs of rows not yet fetched, so a
longer commit outage needs the flag raised. It is a runtime flag, raised
operationally, and needs no re-sign. Time budgets (§15): the build fits the
1,200-block commit window, and the audit fits the 14,400-block challenge
window. `leafCount` is on chain, so account growth is visible without a
separate report.

## 7. Contracts

There are two new contracts. The v1 coordinator, vault, reserve sink and
evidence journal are not modified or upgraded. Both use the pinned toolchain
(`evm/README.md:28-48`): solc 0.8.24, cancun, via-IR, OpenZeppelin 5.6.1.

### 7.1 `STSettlementVault2`: immutable custody

The contract is `evm/src/STSettlementVault2.sol`; its external surface is
`evm/src/interfaces/ISTSettlementVault2.sol`.

**Immutables**, each nonzero or the constructor refuses:

- `netuid`;
- `escrowHotkey`;
- `selfColdkey`, the contract's mapped SS58 account, predicted from the
  deployer nonce as `evm/script/Deploy.s.sol:53-59,113-115` does;
- `bootstrap`;
- `demandFeeWei`, fixed at deployment at 750,000 rao (7.5·10^14 wei): one
  answer's gas at the planning fee, the smallest fee that still makes
  griefing cost the griefer at least what each answer costs the operator
  (owner decision, 2026-10-12: minimize the gas needed to run and use the
  protocol). R-6 measured an answer at depth 29 at 149,074 gas, and 149,074 ×
  5 gwei is 745,370 rao, rounded up. With the demander's own 98,268 gas, a
  demand costs at least the answer's gas at prices up to about three times
  the planning fee. It must be a multiple of 10^9 wei, one rao, because the
  runtime moves only whole rao of a call's value (§7.3).

The challenge window is the constant `CHALLENGE_BLOCKS` = 14,400.
`coordinator` is set once by the bootstrap to the `STSettlement2` proxy.

**State per pool `noId`** (`noId < 2^64`):

| Field | Meaning |
| --- | --- |
| `pools[noId]` | `{hotkey, firstEpoch, registered}`, plus the reverse map `poolHotkeyUsed[hotkey]` |
| `lastFundedEpoch[noId]` | starts at `firstEpoch − 1` |
| `fundedThrough[noId][e]` | cumulative funding through `e`, written once when `e` is captured or deferred |
| `capturedTotal[noId]` | all funding captured or assigned |
| `claimedTotal[noId]` | sum of the pool's `claimed` watermarks |
| `roots[noId][]` | every finalized root `{root, total, leafCount, epoch, ledgerHash}`, append-only; `finalizedTotal` is the last root's total |
| `commitSeq[noId]` | per-pool commit counter; never reset |
| `lastCommitBlock[noId]` | block of the pool's latest commit; never cleared |
| `pending[noId]` | at most one: `{seq, root, total, leafCount, epoch, ledgerHash, committedBlock, openDemands}` |
| `demanded[noId][seq][rootIndex][index]` | the `DemandState` under `seq` of account `index` of the finalized root `rootIndex`: `None`, `Open` until a clean opening answers it, then `Answered`. Each account of each finalized root is demanded at most once per `seq` |
| `claimed[noId][index]` | amount already paid to account `index` |

**Vault-wide state**: `totalCaptured`, `totalPaid`, `escrowAccounted`,
`escrowRegistered`, and a reentrancy flag.

**Storage.** The fields above are what the views return; vault2 packs them by
use so that each call touches few slots. Each pool takes four: its hotkey;
`claimedTotal` and the root count, the slot a claim reads and writes;
`capturedTotal`, `firstEpoch` and the number of funded epochs
(`lastFundedEpoch` is `firstEpoch` plus that number, less one), the slot a
capture or deferral reads and writes; and `commitSeq`, `lastCommitBlock`, the
pending root's seq and `openDemands`. A pool is registered exactly when its
`firstEpoch` is nonzero. A root takes three slots, its total in 128 bits. The
pending root is stored at index `rootCount` of the pool's roots, where
finalization admits it without a copy; its `committedBlock` is
`lastCommitBlock`, and `pending` reads zero while no root is pending. Two
epochs' `fundedThrough` snapshots, and two accounts' `claimed` watermarks,
share a slot. `totalCaptured` and `totalPaid` share one, and `escrowAccounted`
is their difference; the reentrancy flag shares `coordinator`'s slot. It stays
in persistent storage, because the runtime's EVM, on 475 as on 478, keeps a
transient write made in a call that reverts
(`scripts/runtime-rehearsal/README.md`; `docs/spec/runtime-478-audit.md`).
Amounts are held in 128 bits: a capture moves less than 2^64, and a funding
total of 2^128 or more, which no real stake reaches, is refused
(`SafeCastOverflowedUintDowncast`) rather than truncated.

**Functions.** C means coordinator only, B bootstrap only, A anyone. Every
state-changing function shares one `nonReentrant` guard. The functions that
move or assign stake, `captureEmission`, `claim`, `absorbStake` and
`assignSurplus`, end with the backing check from v1
(`evm/src/STSettlementVault.sol:513-515`), made against the escrow's movable
stake (§7.3). `checkPath` and `demandPath` move no stake and skip that
check, so a backing deficit can never freeze disputes.

| Function | Caller | Behaviour |
| --- | --- | --- |
| `setCoordinatorOnce(c)` | B | as v1 (`:178-186`) |
| `registerEscrow(maxBurn)` | B, payable | as v1 (`:191-209`); sent right after vault2's creation (§8.5) |
| `registerPool(noId, hotkey, maxBurn, firstEpoch)` | C, payable | as v1 (`:219-239`); requires `escrowRegistered` and `firstEpoch >= 1`; sets `firstEpoch` and `lastFundedEpoch = firstEpoch − 1`. Had someone else registered the escrow hotkey first, its owner could swap it away with every position on it, so no pool exists before `registerEscrow` succeeds (§8.5) |
| `registerPoolUid(noId, maxBurn)` | A, payable | Re-registers a pool hotkey that has no UID. The caller pays the burn and gets the surplus refunded, and the call reverts if vault2's native balance would end below its balance before `msg.value` (v1's pattern, `:171-176`) |
| `captureEmission(e, noId)` | C | Requires `e == lastFundedEpoch + 1`. Observes the pool hotkey's movable stake: `getStake` less the registration collateral locked on vault2's position (§7.3). If it is zero, the epoch funds 0 with no runtime call. Otherwise moves it to escrow; measured on whole positions, the pool must lose exactly the observed amount, and escrow must gain the observed amount or one rao less. Adds the escrow gain to `capturedTotal`, `totalCaptured` and `escrowAccounted`, then sets `fundedThrough[e] = capturedTotal` and `lastFundedEpoch = e`. A runtime refusal reverts. |
| `deferEmission(e, noId)` | C | Same ordering. Sets `fundedThrough[e] = capturedTotal` with no capture. |
| `commitPending(e, noId, prevRootCount, commitment)` | C | `commitment` is `{root, total, leafCount, ledgerHash}`. Replaces any pending root. Requires: `block.number >= lastCommitBlock + 14,400`, so a sample cannot be re-rolled; `prevRootCount == roots[noId].length`, so a finalize between build and commit cannot leave a stale predecessor; `e` above the last root's epoch and at most `lastFundedEpoch`; `total == fundedThrough[e] > finalizedTotal`; `leafCount` from the last root's count up to 2^29, and at least 1; `root != 0` and `ledgerHash != 0`. Sets `pending.seq = ++commitSeq` and `lastCommitBlock = block.number`. An honest root deleted by a fault is therefore succeeded only in a later epoch, which §6.4 allocates. |
| `demandPath(noId, rootIndex, account, oldProof)` | A, payable | Requires `msg.value == demandFeeWei`, a pending root with `block.number < committedBlock + 14,400`, and the finalized root `rootIndex`, any of the pool's, against which `account = {index, coldkey, cumulative}` verifies with `oldProof`, so only an existing account of a finalized root can be demanded. A `rootIndex` at or above `rootCount`, which is every index while no root is finalized, reverts `UnknownRoot`; `NoFinalizedRoot` is never raised. Requires the state of `(seq, rootIndex, index)` to be `None` and sets it `Open`, so each account of each finalized root is demanded at most once per `seq`. vault2 keeps the fee, and it is never paid to the answerer. `checkPath` can answer at any time while the root is pending. |
| `checkPath(noId, rootIndex, account, oldProof, newPath)` | A | The §6.5 path check of `account` of the finalized root `rootIndex`, proven by `oldProof`, against the pending root; an unknown `rootIndex` reverts `UnknownRoot`. A fault deletes the pending root. A clean opening answers an `Open` demand on `(seq, rootIndex, index)` and sets it `Answered`; without one it reverts `NoFault` and changes nothing. Anything else reverts. |
| `finalizePending(noId)` | C | Requires a pending root at least 14,400 blocks old with no open demand. Appends it to `roots`. |
| `claim(noId, rootIndex, account, proof)` | A | Verifies `account = {index, coldkey, cumulative}` against `roots[noId][rootIndex]`. Requires `coldkey` nonzero and not `selfColdkey`, `amount = cumulative − claimed` positive, and `claimedTotal + amount <= finalizedTotal`. Then sets `claimed = cumulative`, adds `amount` to `claimedTotal` and `totalPaid`, subtracts it from `escrowAccounted`, and calls `transferStake(coldkey, escrowHotkey, netuid, netuid, amount)`. Escrow must lose exactly `amount`, and the coldkey must gain `amount` or one rao less. Any failure reverts the whole claim, which stays claimable. |
| `absorbStake(hotkey)` | A | Moves all the movable stake `selfColdkey` holds on `hotkey` (not the escrow, not a pool hotkey) into escrow, with the same delta rules. Refuses a zero hotkey and zero movable stake. No accounting change: the stake becomes surplus. |
| `assignSurplus(noId, amount)` | C | `0 < amount <= liveEscrowStake − escrowAccounted`, with `liveEscrowStake` the escrow's movable stake. Adds to `capturedTotal`, `totalCaptured` and `escrowAccounted`. Emits `SurplusAssigned(noId, lastFundedEpoch + 1, amount)`. Phase A's `STSettlement2` never calls it (§16). |

**Amounts.** Every amount passed to the staking precompile is below
2^64 − 1, which the precompile would read as "the whole position".

**Views**:

- per pool: `pools`, `poolHotkeyUsed`, `lastFundedEpoch`, `fundedThrough`,
  `capturedTotal`, `claimedTotal`, `finalizedTotal`, `rootCount`, `roots`,
  `pending`, `commitSeq`, `lastCommitBlock`;
- per account and demand: `demanded(noId, seq, rootIndex, index)`, which
  returns the `DemandState`, and `claimed`;
- vault-wide: `coordinator`, `escrowRegistered`, `totalCaptured`,
  `totalPaid`, `escrowAccounted`, `conservationHolds`, `liveEscrowStake`
  (the escrow's movable stake), the immutables and `CHALLENGE_BLOCKS`.

**Events**: `CoordinatorFixed`, `EscrowRegistered`, `PoolRegistered`,
`PoolUidRegistered`, `EmissionCaptured`, `EmissionDeferred`,
`DistributionCommitted`, `PathDemanded(noId, seq, rootIndex, index, demander)`,
`PathAnswered(noId, seq, rootIndex, index, answerer)`,
`DistributionDeleted(noId, seq, rootIndex, index, fault, level, challenger)`,
`DistributionFinalized`, `Claimed(noId, rootIndex, index, coldkey, cumulative, amount, caller)`,
`StakeAbsorbed`, `SurplusAssigned`.

**Library.** `evm/src/LedgerTree.sol`, the twin of
`merkle/ledger_sum_tree.go`, provides `leafHash`, `nodeHash`, `depth`,
`verifyLeaf` and `walkPath`. All five are tested against the Go vectors
(T-1 to T-3, X-1).

### 7.2 `STSettlement2`: settlement coordinator (owner-upgradeable UUPS)

The contract is `evm/src/STSettlement2.sol`, behind an `ERC1967Proxy`.

**Reads from the v1 proxy**: operators (`operatorAt`), windows (`policyAt`),
boundaries (`epochEndBlock`, `currentEpoch`), the v1 vault
(`settlementVault`) and `paused`. The owner is the governance Safe.

**State**:

- `v1Coordinator`, `vault`;
- the auditor set `auditors[]`, at most 16 addresses, with its `quorum` and
  `auditorSetVersion`;
- per-pool approvals keyed by `(seq, auditorSetVersion)`;
- a storage gap. The layout is pinned in `evm/layout/STSettlement2.json`
  (S-9).

**Functions.** O means owner, R the root signer of `operatorAt(noId, e)`
(active or not), A anyone. "Not paused" means the v1 coordinator's `paused`
is false.

| Function | Caller | Behaviour |
| --- | --- | --- |
| `initialize(owner, v1Coordinator, vault, auditors, quorum)` | proxy | once; a nonzero owner, and a v1 coordinator and vault with code; implementation initializers disabled |
| `setAuditors(auditors, quorum)` | O | `1 <= quorum <= n <= 16`; no zero or duplicate address; bumps the version, so earlier approvals stop counting |
| `registerOperatorPool(noId, hotkey, maxBurn, firstEpoch)` | O, payable | Requires the v1 operator active now, `firstEpoch > currentEpoch()`, and `policyAt(firstEpoch)` with `epochBlocks >= rootCommitWindowBlocks + 14,400`, the only place the cadence is checked (§5). Calls `vault.registerPool` with `msg.value` and passes vault2's refund of the burn surplus on to the caller. This call is the pool's commitment to v2 from `firstEpoch`. |
| `settlementAt(noId, epoch)` | view | `(vault, hotkey, 2)` if the pool is registered and `epoch >= firstEpoch`; otherwise `(v1 vault, operatorAt(noId, epoch).poolHotkey, 1)` |
| `closeOperatorEpoch(e, noId)` | A, not paused | Requires `e >= firstEpoch` and a block in `[end(e), end(e) + rootCommitWindowBlocks]`; a deactivated operator's emission is still captured. Defers every earlier unfunded epoch, then calls `vault.captureEmission`. |
| `commitDistribution(e, noId, prevRootCount, commitment)` | R, not paused | Requires a block in `[end(e), end(e) + rootCommitWindowBlocks]`, and either no pending root or `block.number >= end(pending.epoch + 1)`. A pending root still unfinalized by then is replaced, because no approval can arrive any more; the replacement covers its epochs. The driver finalizes a finalizable root before committing (§8.1). Calls `vault.commitPending`. |
| `approveDistribution(noId, seq, root, ledgerHash)` | auditor | The arguments must equal the pending root, and `block < end(pending.epoch + 1)`. Counts once per auditor per version. |
| `finalizeDistribution(noId)` | A, not paused | Requires at least `quorum` approvals for the pending `seq` at the current version. Calls `vault.finalizePending`, which enforces the window and demands. |
| `_authorizeUpgrade` | O | |
| `receive()` | vault2 | Accepts native value only from vault2, as the registration refund; any other sender reverts. |

**Views**: `approvalCount(noId)`, `hasApproved(noId, auditor)`, `auditors()`,
`quorum()`, `auditorSetVersion()`, `vault()`, `v1Coordinator()`.

Wrappers for `assignSurplus` and the v1 sweep record are added by the phase C
upgrade (§16).

### 7.3 Runtime facts the contracts rely on

These come from the local subtensor sources (v451-v454) and the upstream
runtime-475 source `d1718c99`, checked against `docs/spec/runtime-475-audit.md`.
They hold on runtime 478, source `850f2755`, which changes only how the
staking precompile admits `moveStake` and `transferStake` (below;
`docs/spec/runtime-478-audit.md`, "What settlement v2 relies on"). The
rehearsal node, built by `scripts/runtime-rehearsal/`, measured them on 475 on
2026-10-11 and on 478 on 2026-10-13 (`scripts/runtime-rehearsal/README.md`).
All of them are qualified on the real runtime before deployment (R-1 to R-6).

- **Stake moves.** Capture by `moveStake` and claims by `transferStake` move
  exactly the requested amount out of the source. The destination's
  share-pool read can be one rao short (upstream `staking/move_stake.rs:131-143`).
  On the rehearsal node it was short only onto hotkeys whose share price had
  risen above 1, and exact in 16 of 16 calls at share price 1.
  The escrow hotkey earns nothing under Yuma while a validator sets weights,
  so its share price stays 1. In a tempo without weight-derived emission,
  Yuma's fallback pays validator emission by stake to the active permit
  holders (to every UID if none is active): the rehearsal escrow earned
  3,595,812,938 rao over two such tempos. vault2's share of that is surplus
  that no account is owed, and claims already accept a destination one rao
  short. R-3 runs with a weight-setting validator. Third-party stake
  operations on the escrow hotkey never lowered vault2's position (R-3).
- **Registration collateral.** A subnet's `CollateralLockShare` (0 by
  default; the subnet owner or root sets it) makes each registration stake
  that share of its price to the registered hotkey and lock it on the
  registering coldkey's position (upstream
  `pallets/subtensor/src/subnets/collateral.rs`). Only emission the hotkey
  earns releases the lock. `getStake` includes it, and a move or transfer
  that would dig into it fails `StakeUnavailable`. The staking precompile
  reports it as `getMinerCollateral(netuid, hotkey, coldkey).locked`
  (upstream `precompiles/src/staking.rs:1720-1745`), declared in
  `evm/src/interfaces/stakingCollateral.sol`. vault2 therefore moves and
  counts only movable stake, `getStake` less `locked` and 0 below it:
  capture and `absorbStake` move exactly that, and `liveEscrowStake` is the
  escrow's. Its delta checks still compare whole positions. On the rehearsal
  node, at lock share 2^15/65535, a move of the free stake plus 1 rao was
  refused, and a close moved exactly `getStake − locked` and left the lock
  (R-2).
- **Locked alpha.** Every account, a contract's mapped coldkey included,
  rejects incoming conviction-locked alpha unless it opts in (`AccountFlags`;
  upstream `staking/lock.rs:498-531`). vault2 never opts in or locks, so no
  third party can push a lock onto its positions; the rehearsal node refused
  such a transfer with `AccountRejectsLockedAlpha`.
- **Call value.** The runtime moves `floor(value / 10^9)` rao of a call's
  native value, and the sub-rao part never moves
  (`SubtensorEvmBalanceConverter`, upstream `runtime/src/lib.rs`).
- **Pruning.** Pruning removes only subnet membership. Stake and the hotkey's
  `Owner` entry remain, and moves need only the `Owner` entry. Only the
  owning coldkey can re-register, which vault2 does in `registerPoolUid`.
  The escrow hotkey earns no emission, so it is among the first UIDs pruned
  once its immunity ends. vault2 works unchanged with it pruned: on the
  rehearsal node a close moved a pruned pool's stake into the pruned escrow
  (R-4).
- **Transfer floor.** A same-subnet move or transfer needs a TAO-equivalent
  of at least 100,000 rao, or it reverts (`AmountTooLow`).
- **Staking-hotkey caps.** Runtime 478, as 475 did, refuses a transfer to
  another coldkey that would add the escrow hotkey to that coldkey's
  `StakingHotkeys` list once the list holds `MAX_THIRD_PARTY_STAKING_HOTKEYS`
  entries (`TooManyStakingHotkeys`). The source is upstream `850f2755`
  `pallets/subtensor/src/staking/stake_utils.rs:1865-1874` (`:1808-1822` in
  475's `d1718c99`). A coldkey's own moves face the larger
  `MAX_STAKING_HOTKEYS` cap (`:1403-1418` in both). For such an
  account the claim reverts and stays claimable. Third parties can likewise
  fill vault2's own list, up to `MAX_THIRD_PARTY_STAKING_HOTKEYS`, 128
  entries. Claims and `absorbStake` keep working, but the runtime charges a
  walk of the list: on the 478 rehearsal node a claim then used 1,257,365
  gas instead of 154,831 and needed a limit of 1,273,292 instead of 159,817,
  and `absorbStake` used 1,221,499 (R-3).
- **Whole-position sentinel.** The staking precompile reads an amount of
  2^64−1 or more as "the whole position".
- **Gas.** The runtime charges a precompile dispatch's actual weight as gas,
  but admits it only if the gas forwarded covers its declared weight. On 475
  and 477 that weight, for `move_stake` and `transfer_stake`, included a walk
  of a full 256-entry `StakingHotkeys` list, so a transaction that moved
  stake used about 105,000 gas but needed a gas limit of about 2.4M. Runtime
  478 admits them against a walk of the caller's own list plus one entry, at
  most 256: 65,517 gas plus 8,750 an entry for `moveStake`, and 63,085 plus
  8,750 an entry for `transferStake`. Each also uses 625 gas more
  (`docs/spec/runtime-478-audit.md`, "The admission rule, before and after").
  While vault2's list is short, a claim needs a limit of about 192,000 gas
  and a capturing close about 236,000. Third parties can lengthen the list
  between an estimate and the inclusion (above), so senders never sign a
  stake move below its pre-478 size (§8.1). A refused precompile call
  consumes all the gas forwarded to it. Measured inside a contract:
  `getStake` 4,748 gas, `getUid` 995, `getMinerCollateral` 1,013 and
  `registerLimit` 1,409,550 on 475 and 478; `moveStake` 81,753 and
  `transferStake` 79,321 on 475, and 82,378 and 79,946 on 478. §8.1 gives
  each v2 transaction's gas (R-6).

## 8. Operator server

Paths in this section are in `server/` unless marked.

### 8.1 v2 epoch driver

- **Engine.** The driver reuses the existing engine: `StSyncChain` every
  5 s; durable transaction intents with logical keys; the root key signs; the
  signed operator gas policy applies (`taskworker/work/st_work.go`;
  `controller/st_controller.go:1561-1648`).
- **Operations.** The new logical keys are `v2close:e:no`,
  `v2commit:e:no`, `v2finalize:e:no` and `v2answer:no:seq:rootIndex:index`.
- **Gas policy.** It binds the v1 coordinator, the policy hash and the
  accounts, not call targets (`controller/st_transaction_gas_policy.go:100-106`).
  Its validity window, `maximum_gas`, and attempt and liability caps must
  cover v2 traffic, which today's policy does not (below).
- **v1 cutoff.** Epochs below the pool's `firstEpoch` keep the v1 tasks. The
  v1 driver never closes or commits a v1 epoch at or after `firstEpoch`,
  except in the phase C sweep.
- **Order per epoch.**
  1. Close inside the 1,200-block window.
  2. Finalize the previous root if it can be finalized.
  3. Read the latest finalized root and the root count from chain.
  4. Build the ledger.
  5. Publish the chunks, then the header.
  6. Commit with that root count.
- **Answering.** The server watches `PathDemanded` and answers each demand
  with `checkPath` from its cached trees, proving the account against the
  finalized root the demand names, the latest or an older one. The demand
  fee keeps griefing at least as costly to the griefer as the answers are to
  the operator. An answer refused `NoFault` or `NoPendingRoot` is done (§5).
- **Preflight.** Each new v2 operation is first simulated with `eth_call`,
  so a call that would revert never takes a nonce.
- **Alerts.** A missed close, a missed commit window, an open demand older
  than one day, a root not finalized by `end(e) + 14,400 + 1,200`, and SN25
  nearing the prune threshold: among the lowest `v2_prune_alert_positions`
  (`st.yml`, default 3) non-immune subnets by moving price, the order in
  which registrations at the subnet limit dissolve subnets (§17, dissolution
  row). The driver reads that census from Subtensor storage at the finalized
  head at most every ten minutes.

**Gas.** R-6 measured each v2 transaction on runtime 478, on ledgers of 2^29
accounts, the deepest vault2 takes, with vault2's staking-hotkey list at one
or two entries, as at launch: the gas used, and the least gas limit at which
an `eth_call` of it succeeds (`docs/spec/runtime-478-audit.md`, "Runtime
rehearsal on 478"):

| Transaction | Sender | Gas used | Least gas limit |
| --- | --- | ---: | ---: |
| close, capturing | operator | 226,055 | 236,399 |
| close, nothing to capture | operator | 126,610 | 133,826 |
| close deferring 1 epoch | operator | 131,633 | 141,343 |
| close deferring 144 epochs | operator | 2,409,622 | 2,862,246 |
| commit | operator | 205,288 | 214,491 |
| answer (`checkPath`) | operator | 149,026 | 153,963 |
| finalize | operator | 94,389 | 100,822 |
| approve | auditor | 132,524 | 134,165 |
| demand | anyone | 98,256 | 103,252 |
| claim, an account's first | anyone | 187,332 | 192,318 |
| claim, after the account's neighbour's first | anyone | 170,208 | 175,194 |
| claim, a repeat | anyone | 170,243 | 175,229 |

Each deferred epoch costs about 15,900 gas and 19,000 of limit.
`eth_estimateGas` gave a capturing close 246,899 and an account's first claim
206,967; on the rehearsal node its search stops within 10% of the least
sufficient limit (`scripts/runtime-rehearsal/README.md`). On 475 and 477 the
two needed limits of about 2.5M and 2.4M, and `eth_estimateGas` gave them
about 2.7M and 2.6M, because the runtime admitted a stake move only against
a full 256-entry walk (§7.3). On 478 the limit follows vault2's own list,
which third parties can lengthen after an estimate. So no sender signs a
stake move below its pre-478 size, by the owner's sizing rule
(VERSION2-LAUNCH.md section 2.6; `docs/spec/runtime-478-audit.md`, "Gas
limits on 478"):

- a close at the larger of its estimate plus 20% and 3,200,000 gas, which
  covers every length of the list for a capturing close that defers at most
  94 epochs. The server caps that floor at the gas the operator gas policy
  still accepts;
- a claim at the larger of its estimate with margin and 2,600,000 (§10.2,
  §11).

The engine signs commit, finalize and answer at the estimate plus 20%. The
SN25 operator gas policy (`local/sn25golive/sn25-mainnet-gas-params.json`)
allows a `maximum_gas` of 1,000,000, below the close floor, and a per-intent
liability of 5·10^16 wei, below the 9.6·10^16 wei of one floored close at its
30 gwei fee cap. It is therefore re-signed for v2 (§13). Anyone can still
send a close that defers more epochs than the policy covers.

### 8.2 Ledger build

For each covered epoch not already built, the builder reuses v1's inputs:

- the usage census (`model/st_provider_usage.go:74-219`);
- wallet selection, by the roster path or retained consents
  (`model/st_provider_wallet_epoch.go:56-301`;
  `model/st_provider_wallet_retained.go:70-134`);
- head exclusion at both boundaries.

It then produces provider rows (§6.2), runs the allocation walk (§6.4) with
funding read from vault2, builds the accounts from the previous finalized
root's account chunks, then the tree and the objects. An epoch's rows are
built once and reused by later roots (§6.4).

Scale changes needed for a million providers:

| v1 | v2 |
| --- | --- |
| Two `bindingAt` calls per provider (`controller/st_controller.go:2203-2361`) | Candidates are the client ids whose `FleetBound` validity covers the epoch; `bindingAt` is called only for them. The count is bounded by the cost of binding, not by the head size |
| Consent-chain verification per provider per epoch under one 300 s timeout (`model/st_provider_wallet_epoch.go:67,120-146,214-296`) | Verify each chain once per head hash and store the result; epoch selection is a bulk query |
| One insert plus a select per provider for wallet resolutions (`model/st_payout_wallet_resolution.go:58-100`) | Batched |
| One JSON artifact holding every proof, read whole, under a 32 MiB read cap | Binary chunks, put-if-absent |
| Whole-tree rebuild per claim request (`controller/sn_controller.go:269-278`) | Tree cached per finalized root |
| One `st_payout_leaf` row per leaf per epoch | An account-index table that grows only with new accounts |

### 8.3 Endpoints

Both are public. A proof only reveals published data and cannot redirect a
payment. Clients do not trust the endpoint: the SDK checks each proof
against vault2's on-chain root before it claims (§11).

- `GET /sn/ledger/object?hash=sha256:…` returns an immutable object.
- `GET /sn/ledger/account?coldkey=<ss58|hex>` returns, per pool:
  - `vault`, `no_id`, `root_index`, `index`, `cumulative` and `proof`, as
    `{hash, sum}` siblings;
  - `claimed`, read at the finalized tag;
  - the pending cumulative.

An unknown coldkey returns an empty list. An account that exists only in the
pending root returns its pending cumulative without a proof. The
authenticated `GET /sn/pool/claim` stays for v1 epochs.

### 8.4 Readers moved to v2 for v2 epochs

- **Earnings email** (`controller/epoch_earnings_email.go:207-237`). Amounts
  come from the ledger, not `share_bps`. For a root covering several epochs,
  each epoch's amount is recomputed from its rows. The email and the
  leaderboard rebuild (`:122-131`) fire on v2 finalization.
- **`/account/epochs`** and the earnings model
  (`model/sn_earnings_model.go:84,108`) read the same per-epoch amounts.
- **The st sync mirror** decodes the `STSettlement2` and vault2 events and
  mirrors funding, roots, the pending root and demands
  (`controller/st_controller.go:4118-4230`).
- **Deferred.** Deposit sizing (`controller/st_controller.go:2984-3016`) and
  the provider-work prior chain are dormant at zero price and without a
  roster. They move when pricing or the roster ships.

### 8.5 Deployment tooling (this repository)

`sn-mainnet bootstrap-contracts` accepts a fixed list of nine action ids, of
which eight can execute (`mainnet/evm_phase.go:174,180,201`;
`mainnet/bootstrap_contract_command.go:32,42`). Phase A adds a
v2 contract phase:

1. vault2 creation;
2. `registerEscrow`, with one burn;
3. `STSettlement2` implementation creation;
4. proxy creation with `initialize`;
5. vault2 `setCoordinatorOnce`;
6. optionally, the claim batcher's creation (§10.2).

One deployer sends them all at consecutive nonces, and the first two are
broadcast back to back. vault2's creation reveals the escrow hotkey, and
anyone who registers that hotkey first makes `registerEscrow` fail for good.
That costs a redeploy, with no funds at risk.

It uses the same builder checks, plan hash, approval and receipts
(`mainnet/BOOTSTRAP-CONTRACTS.md:63-142,359-431`). Completion requires
readback of exact runtime code, getters and storage slots. v1's launch
skipped readback (`mainnet/LAUNCH.md:50-58`); v2 must not. The receipts are
the v2 deployment record. The launch readiness digest is unchanged.

### 8.6 Operator commands

The server's `bringyourctl st` commands for a v2 epoch of the configured
pool. Each names the epoch, and `--by` names who runs it. `--by` is
recorded, not authenticated: running a command at all takes the operator's
database access, so any operator may withdraw any request. A refused command
panics with its reason and exits non-zero.

| Command | Does |
| --- | --- |
| `v2-unrecoverable --epoch=<e> --by=<operator>` | Declares the epoch's inputs unrecoverable, so roots list it as `providers: null` (§6.4), and records who declared it. Refused once the epoch's rows are recorded; a repeat changes nothing |
| `v2-rebuild --epoch=<e> --by=<operator>` | Requests that the epoch's recorded rows, or its null declaration, be built again from v1's inputs (§6.4 fixed inputs), and records and logs who requested it. The next root builds them again unless the pending root lists them and can still finalize; the new rows replace the record and end the request. Refused for an epoch the latest finalized root covers and for one with nothing recorded |
| `v2-rebuild-withdraw --epoch=<e> --by=<operator>` | Withdraws the epoch's pending rebuild request, which leaves its record as before the request, so the driver treats the epoch as if none had been made, and logs who requested and who withdrew it. A compare-and-set clears the request only while it is the one read and the record holds the rows or declaration read with it. Rows built for the request but recorded after the withdrawal never replace the record: when they differ, the driver's step is refused and its retry lists the recorded rows. Refused for an epoch with nothing recorded in the pool, for one without a pending request, as once rows rebuilt for it have ended the request, and when rows are rebuilt, or the request changes, between the read and the withdrawal |

## 9. Validator

Facts that shape the change:

- **Zero price.** The validator never fetches the payout artifact
  (`validator/release_steer.go:475-478`), so the ledger audit is new work.
- **Identity.** The v1 coordinator and vault are part of the evidence,
  client-key and provider-attempt domains, and of the measurement envelope
  (`protocol/validator_evidence.go:29-39`; `validator/close_report_domain.go:17-20`;
  `validator/provider_attempt_source.go:388`;
  `validator/release_measurement_envelope.go:44-45`). Production authority
  continuity forbids changing them
  (`validator/production_authority_history.go:297-298`). The config keeps
  them, and the v2 fields are an addition (§13).
- **Sending.** The validator does not broadcast evidence
  (`validator/release_evidence_census_v2.go:3-6`); activation was sent with
  `validator activate --relayer_key_file` (`validator/native_commands.go:314`).
  v2 sends approvals, demands and challenges from the auditor key given by
  `--ledger_auditor_key_file`.
- **The cohort.** The verify cohort (`server/model/verify_cohort_model.go`)
  stays as the measurement state bound until phase B. It no longer affects
  who is paid.

### 9.1 Weight destination

Two sites resolve the pool UID today:

- live steering, in `validator/release_steer.go:439-455`;
- replay, audits and the production row, in
  `validator/release_decision_chain_v2.go:380-416`.

The production row requires the two to agree (`validator/recycle_operators.go:189`).
Both move to one shared resolver. For a registered pool, epochs at or after
its `firstEpoch` (read from vault2 at the finalized head) use
`STSettlement2.settlementAt` at the pinned block. The pool hotkey always
comes from the immutable `vault2.pools(noId)`, and a `settlementAt` answer
that differs is refused, so an upgrade cannot redirect emission. All other
epochs, and every epoch of an unregistered pool, use `operatorAt`, so replay
never calls a contract that did not exist yet.
From E_s the v1 pool hotkey is no longer consulted, so its pruned UID causes
no error.

### 9.2 Ledger audit

For each pending root, the validator first reads chain state at a finalized,
hash-pinned block:

- the pending root;
- `fundedThrough` for every covered epoch;
- the latest finalized root;
- the epochs' boundaries and EVM block hashes.

It then fetches the header and checks its sha256 against `ledgerHash`. Next
it fetches the chunks it does not already hold, up to `--ledger_max_bytes`
of new bytes, and keeps everything in durable custody. It refuses approval
on any failure below, and each check has a GL-9 mutation.

1. **Header.**
   - It is at most 256 KiB and canonical.
   - `no_id` and `epoch` match the pending root.
   - Its covered epochs are exactly `p+1 … e`, at most 96 of them.
2. **Previous root.**
   - The validator holds the latest finalized root's account chunks, or
     fetches and verifies them against that root.
   - That root must be tight: its accounts sum to its total.
3. **Fixed inputs.**
   - If custody holds rows for a covered epoch that the validator found
     valid, the header lists the same chunks.
   - `providers: null` is refused for such an epoch, and recorded for any
     other.
4. **Chunks.**
   - Each hash matches its listed chunk.
   - Every chunk is full except a non-empty last one.
   - Row encodings are valid.
5. **Provider rows.** Each row follows §6.2.
6. **Head exclusion.**
   - Candidates are the client ids with a `FleetBound` event whose validity
     covers the epoch.
   - For each candidate, `bindingAt` at both pinned boundaries decides the
     flag. These reads need archive state, because `bindingAt` reads live
     `getUid` (`evm/src/STCoordinator.sol:812-829`).
   - Rows outside the candidate set must have `head_excluded == 0`.
7. **Allocation.**
   - Run the §6.4 walk, recomputing each allocated epoch (§6.3).
   - Each coldkey's summed delta equals its account's increase over the
     previous root, or 0 if it has no account.
   - Every coldkey with a positive sum has exactly one account.
8. **Accounts.**
   - The first `previous.leafCount` rows keep the previous coldkeys, by
     position.
   - The remaining rows are new: coldkeys absent before, with positive
     deltas, sorted ascending.
   - No coldkey appears twice.
9. **Tree.** Rebuilt from the account rows, it matches the committed root,
   total and leaf count. Together with vault2's `total == fundedThrough[e]`,
   this proves the root is tight.

The operator alone attests population, usage, window membership and wallet
selection (decision 4).

### 9.3 Approval, challenge and demand

The validator takes one of three actions. Challenges and demands protect the
newest finalized root whose accounts the validator holds and has verified,
the *protected root*. That is F, which check 2 verifies, or, when F's
objects cannot be obtained or do not rebuild it, the newest older root whose
objects custody holds. Each challenge and demand names the protected root's
index and proves its account against it (§7.1).

- **Approve.** If every check passes, it sends
  `approveDistribution(noId, seq, root, ledgerHash)` before `end(e+1)`. The
  send is idempotent across restarts.
- **Challenge.** If the published account rows reproduce the root and show a
  fault that §6.5 can prove at an account of the protected root, it sends
  `checkPath`. That covers a changed coldkey and a lower value. A tree
  rebuilt from rows is always consistent, so a root with an inconsistent
  node never reproduces: it takes the demand path, and any opening under the
  bad node deletes it.
- **Demand.** If the objects are missing or do not reproduce the root by
  commit + 7,200, it refuses approval and sends `demandPath` calls, each
  with an account of the protected root and its proof against that root:
  - for every such account whose published row is below its value, names
    another coldkey or is missing;
  - for a value-weighted random sample, drawn without replacement, of at
    least 256 such accounts. The sample doubles for each earlier pending
    root over the same F that was replaced or deleted while a demand was
    open, and every unanswered index of that root is demanded again.
  - The randomness comes from the validator's local CSPRNG, drawn after it
    sees the commit, so the operator cannot predict it.

`ledger.DemandSet` computes this set for the validator and for `snclaim`, in
the order they send it: the sample, drawn over all of the protected root's
accounts with faulted ones included, in the order drawn; then the unanswered
indexes; then the faulted rows. Demands cut short by the window's end or a
fee budget are therefore still a value-weighted sample, whatever rows the
operator published or demands anyone left unanswered (§4.2 G2). The window's
end stops the demands. A send refused because another transaction already
did the work is done (§5).

A refused or deleted root does not change the operator's pool weight while
there is one operator. Zeroing the pool would hand the pool channel to head
fleets (`validator/release_math.go:44-51`), punishing providers for the
operator's fault.

### 9.4 Evidence journal and deposit audit

- **Evidence journal.** The policy hash and v1 vault do not change, so the
  existing activation and the v1 evidence journal keep working
  (`evm/src/STValidatorEvidence.sol:92-177`), and CLOSED_CENSUS continues.
- **Deposit audit.** DEPOSIT_AUDIT stays dormant at zero price. A priced
  policy needs its own v2 deposit design (§16).

## 10. Claims and provider tooling

### 10.1 Miner claim daemon v2 (`miner/`)

The v1 daemon is keyed by epoch and hard-codes the v1 call
(`miner/claim_daemon.go:363-382`; `miner/onchain/parse.go:21-115`). v2 adds a
mode beside it, `provider claim-daemon-v2 --config=<path>
[--v1-config=<path>] [--threshold-rao=<rao>] [--claim-now]`, which can also
run the unchanged v1 daemon in the same process with the same relayer key:

- **State.** A new queue schema, `urnetwork-provider-claim-queue-v2`, in its
  own `state_dir`. The v1 daemon and its queue are unchanged.
- **Unit of work.** `(vault, noId, account index)`. Unlike v1's terminal
  states, a unit reopens whenever a newer root raises the account above
  `claimed`.
- **One transaction at a time.** At most one claim per account is in flight.
  Signed bytes are never replaced, and the existing rebroadcast rules apply.
- **Threshold.** The daemon claims when `claimable` is at least its
  threshold, or on demand (`--claim-now`). The default of 1.9 α keeps a
  claim's gas at most 10% of it at the §3 planning figures. While a third
  party keeps vault2's staking-hotkey list full, a claim costs about 1 α
  (§3), so a daemon then needs a threshold of about 10 α for the same 10%.
  A threshold below the 0.0152 α transfer floor is refused.
- **Refusals.** A claim refused because a third party claimed first is done
  (§5). Any other refusal, such as the floor or a staking-hotkey cap, leaves
  the unit open under a backoff.
- **Batching.** Given a claim batcher's address (`batcher` in its config),
  the daemon claims the due units of one pass in one `claimMany` transaction
  when two or more are due, at most 32 (`onchain.ClaimBatchMaxClaims`): every
  unit keeps the batch's whole signed transaction, so the queue grows with
  the square of the batch, and at depth 29 about 62 claims would fill its
  16 MiB durable head. The daemon refuses to start unless the batcher's code
  is exactly the release runtime. Each unit is verified against its
  finalized root and preflighted alone, counting an attempt as a lone claim
  does; a refused preflight is a refused claim. The batch's gas limit is the
  least at which an `eth_call` of it attempts every claim, plus 20%, within
  the most gas the tools name (§10.2). Every unit of the batch keeps its
  exact signed bytes until its finalized receipt settles each one by the
  batcher's event: paid, by vault2's `Claimed` with the batcher as caller;
  refused, under the refusal rules above; not attempted, open again at once.
  Those bytes name the batcher, so a daemon whose `batcher` was changed or
  removed while a batch is in flight stops at start: the bytes no longer
  authenticate and nonce custody fails closed. Restore the setting until the
  batch settles. A lone due unit is claimed directly.
- **Reused as they are:** locks, durable head, nonce admission, finality
  helpers and the relayer key.

### 10.2 `snclaim`

| Command | Does |
| --- | --- |
| `v2 status --coldkey` | account, claimable, claimed, pending |
| `v2 claim` | one claim; a third party's earlier claim counts as done |
| `v2 claim --all [--batch=<batcher>]` | every finalized account of the coldkey with anything claimable, each verified against vault2's root first: one claim each, or with `--batch` two or more per transaction through the claim batcher |
| `v2 check --all` | Rebuilds the pending root from published objects and compares every account of the protected root (§9.3): the newest finalized root whose objects it fetched and verified, F or, when F's objects are missing or do not rebuild it, an older root, which it reports. Each account is reported as having no fault, a provable fault, or as unverifiable when the objects are missing or do not reproduce the root. With `--challenge` it sends `checkPath` for a fault. With `--demand`, in the unverifiable case, it sends the §9.3 demand set (`ledger.DemandSet`) in its order until the window's end, with the same sample size and doubling, each demand naming the protected root, carrying the account's proof against it and paying the fee. Unlike the validator, it does not wait until commit + 7,200, and a sender draws one sample per pending root and protected root. A root that has left the pending slot, or an account already demanded, counts as done (§5). |
| `withdraw --vault v1` | `withdrawClaimCredit` for v1 credits |

UR runs `snclaim v2 check --all --challenge --demand` as an independent
watcher, on a host other than the validator's. A UR sweep relayer is
optional and can be added later without touching vault2.

**Claim batcher.** `STClaimBatcher` (`evm/src/STClaimBatcher.sol`) is an
optional, immutable, ownerless helper with no storage, persistent or
transient, and no funds, which changes nothing in vault2. `claimMany(vault, claims, gasPerClaim)` calls
vault2's `claim` for each item with exactly `gasPerClaim` gas inside
try/catch, so a refused item never reverts the others, and reports it,
`ClaimPaid(item, amount)` or `ClaimRefused(item, selector)`. The runtime
consumes all the gas forwarded to a precompile call it refuses; the per-item
cap keeps one such item from starving the rest. An item is attempted only
while the gas left covers `gasPerClaim` at the call, which keeps 1/64 of it
(EIP-150), plus 20,000 gas of bookkeeping; the items left are reported once,
`ClaimsNotAttempted(item)`, and `claimMany` returns how many it attempted.
vault2 pays each leaf's coldkey whoever calls, so the batcher can redirect
nothing. A batch succeeds at any gas limit, so `eth_estimateGas` cannot size
it: senders find the least limit at which an `eth_call` attempts every claim
and add 20%. They cap each claim at the larger of 2,600,000 gas, the claim
floor, which was a single claim's least limit with margin on 475 and 477
(§8.1), and the batch's largest single-claim estimate. The tools name at most
13/15 of the latest block's gas limit less 1,500 gas, the most the pool
takes, and at most 64,998,500, less a 10,000-gas margin; they put at most 32
claims in a batch (§10.1), and refuse a batcher whose code is not exactly the
release runtime. Measured on runtime 478 at depth 29 (R-7): a transaction may
name at most 64,998,500 gas, below the 75,000,000 block gas limit, and so
holds at most 416 claims (418 on 475); from ten claims on a batch uses 15 to
18% less gas than the same claims alone (149,232 per claim in a batch of 20
and 148,378 in one of 32, against 170,605 to 187,740 alone), and its gas
limit grows by about 150,000 per claim, where each single claim is signed at
2,600,000 or more (§8.1). A batch of one costs 6% more than a claim alone, so
the tools send a lone claim directly. The deployment phase can create the
batcher (§8.5).

## 11. Apps and SDK

The apps reach SN25 through the shared SDK, which today reads the v1 vault
and can sign v1 claims (`sdk/sn_claims.go:32-117,1062-1091`;
`sdk/sn/evm/abi.go:26,92`). The apps are `android/`, `apple/`, `windows/`,
`linux/` and ur.io in `mmm/`.

**At activation**, through the SDK, every app (decision 8):

- shows the v2 finalized cumulative, claimable and paid amounts, with a
  pending root shown as pending, from `GET /sn/ledger/account`
  (`SnLedgerAccounts`, `SnLedgerAccountsFor`);
- shows v1 balances separately, read as today;
- shows "unavailable" when the proof service cannot be reached, never zero;
- claims v2 accounts. `SnLedgerClaim` claims each finalized account of the
  connected coldkey with a positive claimable, one transaction per account,
  signed by the SDK's gas key. `SnLedgerClaimTransactions` and
  `SnLedgerClaimTransactionsFor` return the same claims unsigned, for
  external signers. vault2's address is pinned in
  `SnChainSettings.Vault2Address`; without it an app shows v2 balances but
  no v2 claim.

The apps' new v2 strings, the `sn_ledger_*` keys and each app's
equivalents, must be in the shared localization store before the apps ship.

**Claim rules.** The endpoint is not trusted:

- an account is claimed only if its vault is the pinned vault2 and its proof
  verifies in the SDK against `roots(noId, rootIndex)` read from vault2;
  `claimed` is read from chain too;
- the transaction goes only to vault2, with no value, and vault2 pays the
  leaf's coldkey;
- each claim is preflighted with `eth_call` from the gas key, and signed
  with the larger of `eth_estimateGas` plus a quarter and 2,600,000 gas, the
  limit a claim needed before runtime 478 (§8.1). On 478 a claim's estimate
  follows vault2's staking-hotkey list, which third parties can lengthen
  before the claim is included (§7.3), so the SDK never signs below that
  limit and gives it to external signers, and ur.io sends it to the wallet
  (`sdk/sn_ledger_claim.go:56-59,318-339`; mmm
  `ur.io/react/src/app/sn/ledgerV2.js:168-175`). The gas key's balance must
  cover the limit at the gas price: 0.0143 TAO at 5 gwei with legacy
  pricing, the node's price plus 10%. About 0.02 TAO on the device gas key
  therefore holds up to about 7 gwei, and EIP-1559 pricing, a fee cap of
  twice the base fee plus the tip, needs about 0.031 TAO
  (`sdk/sn_claims.go:907-940`). With less, the app offers its existing gas
  top-up (`needs_gas`);
- `NothingToClaim` after a third party's claim counts as done (§5). A claim
  below the transfer floor or refused by a staking-hotkey cap leaves the
  account claimable.

### 11.1 Network payout consent

Owner decision, 2026-10-13: the SDK and every app ship network payout consent
signing. One sr25519 signature by the coldkey, requested by the network's
owner, covers every provider client of the network
(`protocol/network_wallet_mapping.go`).

- Today app wallet connect stores a login proof, which release settlement
  never pays (`server/controller/sn_earnings_controller.go:43-45`). Until a
  network signs, its app-only providers stay unpaid, in v2 as in v1.
- This is the largest single increase in providers paid. It is independent of
  phase A and is not an activation gate.

**Server, unchanged.**

- `GET /sn/epoch` reports the contract epoch.
- `POST /sn/wallet/network-consent` takes `{coldkey_ss58, from_epoch,
  through_epoch}` and returns the challenge `message`. Only the network
  owner's session may ask, and the message expires 300 s after issue.
- `POST /sn/wallet` takes `{coldkey_ss58, message, signature}` without
  `client_id`. It checks the coldkey's signature over the exact bytes, then
  stores the consent.
- `POST /sn/wallet/network-consent/history` returns the consent chain from an
  independently pinned head (domain, network, head hash and generation). The
  apps don't use it.
- Protocol limits (`protocol/network_wallet_mapping.go:56`,
  `protocol/wallet_mapping_consent.go:20`):
  - `through_epoch` is at least `from_epoch`, and the span is at most 65,535
    epochs;
  - each new consent's `from_epoch` is after the previous one's;
  - a network has at most 4,096 generations.

**Earnings range.**

- The operator accepts only a prospective consent: `from_epoch` must exceed the
  coordinator's current epoch at the finalized block (protocol
  `prospectiveDigest`). So `from_epoch` is the current epoch + 1, or the
  previous consent's `from_epoch` + 1 when that is later. A consent never
  covers the epoch it is signed in, and a change of wallet takes effect from
  the next epoch. The sn CLI uses the same default
  (`miner/hotkey_wallet_cmd.go`).
- `through_epoch` is `from_epoch` + 65,535. The consent lasts until the network
  changes it, so there is no renewal.
- Epochs before a network's first consent takes effect stay unpaid, because
  committed roots never change.

**SDK** (`sdk/`), exported to gomobile, cgo and wasm:

- `SnNetworkConsentEpochSpan = 65535`;
- `SnNetworkConsentChallenge(coldkey)` applies the range and returns the
  message, the range and the expiry;
- `SnNetworkConsentSubmit(coldkey, message, signature)` submits through
  `POST /sn/wallet`;
- `SnNetworkConsentStatus()` reads the network entry of `GET /sn/wallet`, the
  newest accepted network consent. The history endpoint needs an
  independently pinned head. The status is active while that consent's
  `through_epoch` has not passed, so it is in force or starts next epoch.
- The challenge and submission always use the network credential. A device
  token is refused.
- `BittensorWalletSession`, which every native app's wallet connector signs
  through, gains the `consent` purpose and `SetNetworkConsentChallenge`. A
  consent session signs only a message with the network wallet mapping
  prefix, verbatim, for the requested coldkey and before the challenge
  expires.

**Apps** (Apple, Android, Windows, Linux, ur.io), in the settlement v2
earnings area:

- **Without a consent:**
  1. The app says earnings are not paid until the owner signs, and offers to
     sign.
  2. It connects the Bittensor wallet through the app's existing connector.
  3. It requests the challenge, and the wallet signs exactly the server's
     message.
  4. The app submits the signature and refreshes the status.
- **With a consent:** the app names the coldkey and offers to change it,
  effective from the next epoch.
- An expired challenge, a signature mismatch and a session that is not the
  network owner's each show their own message.
- The app never signs anything but the server's message.
- The eleven `sn_consent_*` keys are in the localization store for every
  platform.

**Acceptance.** §15's rules apply.

| ID | Behaviour |
| --- | --- |
| CS-1 | Range: a first consent runs from the current epoch + 1 through 65,535 epochs on |
| CS-2 | Range: a change starts at the later of the current epoch + 1 and the previous `from_epoch` + 1 |
| CS-3 | The challenge is requested with the network session and no `client_id` |
| CS-4 | The submission carries no `client_id`, and the stored consent appears in the status |
| CS-5 | The status is active while the newest network consent's `through_epoch` has not passed, and shows a consent that starts next epoch |
| CS-6 | Refusals are mapped: expired challenge, signature mismatch, not the network owner |
| CS-7 | Each app goes from no consent through signing to active |
| CS-8 | Each app changes the wallet and shows that it applies from the next epoch |
| CS-9 | Each app signs only the server-issued message, verbatim |
| CS-10 | Every platform and locale has the strings, and the generator reproduces each app's catalog |
| CS-11 | The wallet session refuses a consent message for a login session, a login challenge for a consent session, a message without the prefix, another coldkey and an expired challenge |

The work, done on branch `v2/consent-signing` in sdk, the apps, ur.io,
localizations and this repository, is on `main` in each (VERSION2-HANDOFF.md,
section 1).

## 12. Migration

### 12.1 Order of operations

1. Gates 1-4 of §14 are met, and the gate 5 software is released.
2. Deploy vault2 and `STSettlement2` through the v2 contract phase (§8.5),
   with readback. This completes gate 5.
3. Adopt the validator config (§13), re-sign the treasury approval and the
   operator gas policy, and ship and configure the server. This completes
   gate 6.
4. Check that the validator runs the adopted config and the server runs the
   v2 build.
5. One to two days before E_s starts, the owner Safe calls
   `registerOperatorPool(1, poolHotkeyV2, maxBurn, E_s)`.
   - This is the commitment point.
   - It falls inside the 21,600-block registration immunity, so the new UID
     earns weight before it can be pruned.
   - If it fails, nothing has changed, and E_s moves.
6. From E_s:
   - the validator weights the v2 pool UID (§9.1);
   - the server runs the v2 driver;
   - the v1 driver stops at E_s.
7. Epoch `E_s − 1` completes on v1 after the boundary, in its normal windows.
   If a step fails, v1 rules apply: deferred emission, or a missed root that
   becomes carry. The phase C sweep recovers it (§16).
8. v1 claims stay open until each v1 expiry. v1 credit withdrawal stays
   available through `snclaim withdraw --vault v1` (§10.2).

### 12.2 Conservation at the switch

| Bucket | Where | Action | Recipient |
| --- | --- | --- | --- |
| Reveal-lag emission on the v1 pool hotkey after E_s | runtime stake | captured by the sweep close, or by any v1 close into that epoch's funding, which becomes carry when the epoch finalizes without a root | v1 earners, through the sweep |
| Funded, unfinalized v1 epochs | vault1 `pendingFunding` | finalized, or marked missed into carry, in the v1 windows; the sweep finalizes any left | entitlement or carry |
| Finalized, unclaimed v1 entitlements | vault1 | claimable until v1 expiry, then `expireEntitlement` into carry | v1 coldkeys |
| Accepted, unpaid v1 credits | vault1 `claimCredit` | `withdrawClaimCredit`, forever | v1 coldkeys |
| v1 carry | vault1 `carry[1]` | the sweep root pays vault2, then `absorbStake` and assigned surplus | v1 coldkeys, non-expiring in vault2 (§16) |
| Sweep claim below the transfer minimum | vault1 `claimCredit[vault2]` | deferred, withdrawable later | vault2 |
| Dust at the sweep close; v1 escrow surplus | v1 pool hotkey; vault1 escrow | residue | none |
| Stopgap exclusions before E_s | none | none (owner decision, 2026-10-09) | none |

## 13. Configuration and approvals

Nothing that names the deployment changes. The protocol policy, v1
coordinator, v1 vault, deployment id and evidence journal keep their values
everywhere. The v2 identities are additions.

| Artifact | Change |
| --- | --- |
| **Validator config** | One optional block, `settlement_v2: {settlement, vault}`, a pointer field tagged `omitempty`, with lowercase addresses. The field is absent today, so today's `validator.yml` and history bundle must hash identically under the new binary. At its first finalized snapshot, the validator checks `STSettlement2.vault()`, `v1Coordinator()` and vault2's `coordinator()` at that pinned block (`validator/release_runtime_v2.go:379-387`). E_s is read from the chain, never from config. The auditor key and `--ledger_max_bytes` are runtime flags. |
| **Validator successor class** | New in `validator/production_authority_history.go`: "settlement v2 adoption". It permits a predecessor without the block and a successor that adds exactly it; one appended history entry; and an approval equal to the old one except `config_hash`. It refuses re-pointing or removing the block, any other `stable()` difference (`:292-311`), and combining with the capacity, rendering or runtime classes. Today's continuity check admits only runtime renewal, including the owner-recycle-to-treasury adoption (`:321-334`), evidence rendering (`:276-286`) and capacity revision (`:273`); poll-interval changes also pass (`:300`). |
| **Treasury production approval** | Re-signed over the new `config_hash`, nothing else changed (`validator/treasury_policy.go:131-144`), using `sn-mainnet treasury approval-plan` and `approval-envelope` (`mainnet/treasury_approval.go:20-31,192-292`). The live approval expires at native block 9,739,882, about 2026-12-17 UTC (deployment vault: `vault/main/sn25-validator/treasury-production-approval-*.json`). That renewal is a separate successor, and it comes after adoption. |
| **Server** | Rebuild against the sn revision with the new successor class. Ship the binary before the config, because `sn.yml` is parsed with known fields only (`server/provider_payout_transition.go:57-58`). `vault/main/st.yml` and `config/main/sn.yml`, in the deployment vault and config repositories, add `settlement_v2_address` and `vault_v2_address`. `reserved_attempt_upload` pins the adopted validator config, with the new approval and authority files beside it (`mainnet/VALIDATOR-UPLOAD-RUNTIME.md:3-33`). |
| **Operator gas policy** | Re-signed with the same bindings, because today's `maximum_gas` of 1,000,000 and per-intent liability of 5·10^16 wei are below a capturing close (§8.1). `maximum_gas` rises to about 3.5M, which covers a capturing close signed at about 3.2M with room for a few deferred epochs, and the intent and lifetime liability caps rise to match at the 30 gwei fee cap. The validity window and attempt caps are checked against v2 traffic at the same time |
| **Unchanged** | `verify.yml`, `provider_work.yml`, the launch readiness digest, the policy file |
| **On chain** | `registerOperatorPool`, sent by the Safe. The launch auditor set comes from `initialize` (§8.5) |

**Inert for v2 epochs.** These policy v1 fields keep their v1 meaning for v1
epochs and the sweep:

- `claim_ttl_epochs`, `claim_grace_epochs` and `expired_claim_action`;
- `shares_total_bps`, `rounding` and `missed_root_action`;
- `close_grace_blocks` and `finalize_offset_blocks`: v2 closes inside the
  commit window and finalizes 14,400 blocks after the commit;
- `deposit.usage_artifact`, and `verify.reliability_a_min` as a payment
  input.

**Governance constraint on the v1 policy schedule.** Every v1 policy that
applies to a v2 epoch must keep
`epochBlocks >= rootCommitWindowBlocks + 14,400`. `registerOperatorPool`
checks it only for `policyAt(firstEpoch)` (§5), and no contract checks a
later policy, so the owner must not schedule a policy that breaks it while a
v2 pool exists. Otherwise a root committed late in its window would not
clear its challenge window before the next epoch ends, and vault2's commit
spacing could block the next epoch's commit window.

The deployer, guardian, Safe relayer and Safe owner keys are all on the
owner's single Ledger (`mainnet/LAUNCH.md:161-166`). The owner's availability
is a dependency of steps 2, 3 and 5 of §12.1.

## 14. Activation gates and schedule

Phase A activates, meaning `registerOperatorPool` is sent, only when all six
gates hold:

1. **Contracts.**
   - The Foundry suite (§15) is green at its coverage bars, with
     `forge fmt --check`, `forge build --deny warnings --sizes` and
     `scripts/test-solidity-static.sh` clean.
   - Sizes and margins are recorded.
   - An independent review of vault2, `LedgerTree.sol` and `STSettlement2`
     is resolved.
2. **Runtime rehearsal (R-1 to R-7).** The exact bytecode runs on a node
   built from the source of the runtime mainnet runs, now 478 (`850f2755`).
   No other environment can run it:
   - the testnet campaign closed on 2026-09-28 (`FINALIZE.md:3-11`);
   - there is no localnet;
   - an Anvil fork cannot run the precompiles.

   Phase A builds the node (§18): `scripts/runtime-rehearsal/build.sh`
   builds it with `fast-runtime`, `run-node.sh` starts a one-node dev chain
   with mainnet's EVM chain ID, and the Go harness in `rehearsal/` (build
   tag `runtime_rehearsal`) runs the R battery on it. Any runtime change on
   mainnet repeats this gate.
3. **Ledger and components.** The GT, GA, GL, SV, VA and CL suites are green
   at the §15 Go coverage bar, including GL-10 and SV-6 inside their
   budgets.
4. **End-to-end.** The E suite passes (E-1 to E-7).
5. **Software released and contracts deployed.** The server, validator,
   daemon, `snclaim` and SDK display and claim changes, and the apps' v2
   display and claims (§11), are released, and §8.5 has run with readback.
6. **Approvals.** The validator config is adopted, the treasury approval
   and the operator gas policy are re-signed, and the server is configured.

**Schedule.** E_s is the first epoch that starts at least two days after the
last gate passes. Each epoch of delay leaves about 1,450 α of pool
allocation to the at most 2,000 stopgap providers.

- **E_s is epoch 3,** by the owner's decision (VERSION2-LAUNCH.md O18). It
  starts at block 9,348,028, about 2026-10-23 18:10 UTC.
- **Every gate must pass by block 9,333,628,** 14,400 blocks before that,
  so the rule above holds.
- **The registration window** of §12.1 step 5 is blocks 9,333,628 to
  9,340,828, two days to one day before E_s.
- **A gate that passes later** is a departure from the rule, which the owner
  accepts anew, or E_s moves to epoch 4, which starts at block 9,398,428. A
  missed window moves E_s to epoch 4 (VERSION2-LAUNCH.md sections 9.3 and
  9.4).
- **Gate 1's independent review** was confirmed resolved by the owner on
  2026-10-13 (VERSION2-LAUNCH.md O26).

## 15. Test plan

**Rules.**

- Every behaviour in §5-§12 is tested once, at the layer that owns it, plus
  one wiring test in each layer above.
- Every refusal (Solidity revert or Go error) has a test that triggers
  exactly that refusal.
- Every event is asserted with its exact fields.
- Each test names its ID in a comment.
- A behaviour marked procedural, meaning a ceremony step, is checked in the
  §12 rehearsal instead.

**Coverage.** `forge coverage` does not run on this repository today. The
default mode stops on a stack-too-deep at `evm/src/STCoordinator.sol:758`,
and `--ir-minimum` fails on the v1 evidence library. The gate therefore pins
this command:

```
forge coverage --ir-minimum --report lcov \
  --skip STValidatorEvidence.sol --skip ValidatorEvidenceStorage.t.sol --skip ValidatorEvidenceActivation.t.sol
```

- The new contracts must compile under `--ir-minimum`. Wide argument lists
  are passed as structs, and they do not reuse `ValidatorEvidenceActivation`.
- vault2, `STSettlement2`, `LedgerTree.sol` and `STClaimBatcher` need 100%
  line and branch coverage in that lcov report. A reviewed list explains each
  instrumentation artifact or unreachable branch.
- Plain `forge test` must also pass.
- Sizes come from the release `forge build --sizes`, never from a coverage
  build.
- New Go packages: every error return covered, with statement coverage of at
  least 95%.

**Harnesses.**

- **Foundry.** The etched precompile mocks
  (`evm/test/mocks/PrecompileMocks.sol`; `evm/test/utils/ReleaseBase.sol:65-72`),
  with the vault in the standalone pattern of
  `evm/test/ReleaseSettlementMinimums.t.sol:10-26`. `MockNeuron` gains a
  reentry hook and a registration-failure knob, and the staking mock answers
  `getMinerCollateral` with a settable lock.
- **Shared vectors.** Go writes `merkle/testdata/ledger_v2_vectors.json`,
  which `evm/foundry.toml:29-30` already allows tests to read.
- **Static analysis.** `scripts/test-solidity-static.sh` lists its analysis
  roots explicitly, and the new contracts are added to that list.
- **End-to-end in Go.** The exact creation bytecode
  (`mainnet/evm_create_fixture_test.go`) runs in geth with Go fake
  precompiles (`mainnet/economic_conservation_capture_fixture_test.go:27-107,166-189`).
  - The fakes gain `transferStake` and per-caller coldkeys. geth hands a
    precompile no caller, so attribution uses a tracer `OnEnter` hook.
  - The suite runs in the server-database shard.
- **Scale tests.** Behind a new `scale` build tag, in their own release-gate
  shard. Time budgets come from the windows:
  - the builder takes at most 1 hour at 1M providers, inside the 1,200-block
    commit window;
  - the verifier takes at most 4 hours, inside the challenge window.

  Peak memory is recorded and must fit the production host with a 2× margin.

### 15.1 Tree: Go (GT) and Solidity (T)

| ID | Behaviour |
| --- | --- |
| GT-1 | Roots for n = 1, 2, 3, 4, 5, 8, 9, 1,000; the depth rule; padding |
| GT-2 | Every index's claim proof verifies, with exactly `d` siblings |
| GT-3 | Each single change fails: a sibling hash or sum, an index bit, the coldkey, the cumulative, the proof length, an index at or above `n`, the total one above or one below the root's sum |
| GT-4 | No fault on any index of an honest tree |
| GT-5 | Each fault is found: a lower value; a changed coldkey; a garbage leaf; an inconsistent node at every level; root sums not equal to the total |
| GT-6 | Values at or above 2^128 refused; more than 2^29 accounts refused |
| GT-7 | Property: on random adversarial trees (garbage hashes, inconsistent sums, filled padding), the provable values sum to at most the total |
| GT-8 | Property: an account with no fault is provable in the new tree at at least its old value |
| T-1 | `leafHash`, `nodeHash` and `depth` equal the vectors |
| T-2 | `verifyLeaf` accepts every vector and rejects every GT-3 change |
| T-3 | `walkPath` returns each GT-5 fault, reverts on a malformed path, and finds no fault on fuzzed honest trees built in Solidity |
| T-4 | A uint256 sum overflow in a proof reverts |
| X-1 | The Go vectors for trees, proofs and paths pass `LedgerTree.sol` |
| X-2 | Go and Solidity agree on each fault kind, and on the level where it is found |

### 15.2 Allocation and ledger: Go (GA, GL)

| ID | Behaviour |
| --- | --- |
| GA-1 | Sums to `A` exactly (fuzzed) |
| GA-2 | Each delta is within one rao of the exact share |
| GA-3 | Splitting a provider leaves the result unchanged |
| GA-4 | Ties go to the ascending coldkey |
| GA-5 | `ΣW = 0` refused; one coldkey takes all; `A = 0` gives zeros; `A` near 2^128; total usage of 2^64 − 1 accepted and of 2^64 refused |
| GA-6 | Input order does not matter |
| GL-1 | Canonical header round trip. Refused: spacing, key order, hex case, a missing `sha256:` prefix, unknown fields, trailing data, `null` outside `providers`, a missing key, a non-canonical integer, a bad reference prefix or length, a header over 256 KiB, more than 96 epochs |
| GL-2 | Builder output is byte-identical across runs |
| GL-3 | First root; continuity root; new accounts appended sorted; no commit when the total does not grow |
| GL-4 | The Alice case: Alice works only in a dropped epoch e, Bob only in e+1, and each is paid their own epoch's funding |
| GL-5 | The carry walk for no eligible usage, an unrecoverable epoch and a deferred close; `providers: []`; no root when e does not allocate |
| GL-6 | Fixed inputs: a later root lists the same chunks; `providers: null` is refused when valid rows are held and recorded otherwise |
| GL-7 | Head exclusion from the candidate set matches per-provider `bindingAt` |
| GL-8 | A wallet change, head promotion, a dormant account and a shared coldkey each behave as §6.1 says |
| GL-9 | Each §9.2 check is triggered, exactly, by its own mutation. Header: sha256 not equal to `ledgerHash`, non-canonical bytes, `no_id`, `epoch`, a covered range that is wrong or too long. Previous root: non-tight, or chunks that do not rebuild it. Fixed inputs: rows changed, or a `null` against held rows. Chunks: a hash, a short middle chunk, an empty last chunk, a bad row length. Rows: unsorted, duplicate, zero id, zero usage. Head flag: set without a binding, or cleared with one. Allocation: a delta, an omitted new account. Accounts: a moved coldkey, a lowered cumulative, an unsorted new account, a duplicate coldkey, a new account with a zero delta. Tree: root, total, leaf count. The byte budget. |
| GL-10 (scale) | 100k and 1M providers with 1M accounts, single-epoch and ten-epoch roots, builder and verifier inside the §15 budgets, with bytes fetched recorded |

### 15.3 vault2 (V)

| ID | Behaviour |
| --- | --- |
| V-1 | The constructor refuses a zero argument, and a `demandFeeWei` that is not a multiple of 10^9 wei. `setCoordinatorOnce` and `registerEscrow`: bootstrap only, once each; a zero or codeless coordinator refused; `registerEscrow`'s refund, and a failed refund refused |
| V-2 | `registerPool`: refused before `registerEscrow`, and for a zero or 2^64 `noId`, a zero, duplicate or escrow hotkey, a zero `maxBurn`, and a UID that is missing after registration; sets `lastFundedEpoch = firstEpoch − 1`; `firstEpoch` 0 refused; an unregistered pool reads empty |
| V-3 | `registerPoolUid`: unknown pool refused; refused while a UID exists; the caller pays; refund; a call that would spend vault2's own native balance reverts |
| V-4 | Capture and defer in strict order; out of order refused; unknown pool refused |
| V-5 | Capture: zero stake funds 0; exact source delta; escrow gains the amount or one rao less, and that gain is accounted; a larger gap or a runtime refusal reverts; an amount of 2^64 − 1 or more refused; a funding total of 2^128 or more refused. With registration collateral on the pool position, only the movable stake moves, a position holding only its lock funds 0, and the deltas are checked on whole positions |
| V-6 | `fundedThrough` is written once and is monotone |
| V-7 | `commitPending` refusals, one each: epoch range; total not equal to the snapshot; total not above `finalizedTotal`; `leafCount` 0, above 2^29, or below the previous; zero root; zero `ledgerHash`. A replaced pending root bumps `seq` and discards its demands on the accounts of every finalized root; a commit within 14,400 blocks of the pool's previous commit refused, including after a self-proven fault; a `prevRootCount` mismatch refused; `seq` after a finalization or deletion exceeds every earlier `seq` |
| V-8 | `checkPath` deletes the pending root for each fault kind, including at depth 0, against any finalized root: a value kept above an older root's but lowered below a later root's is faulted against the later root, and the clean answer of a demand against the older root neither answers nor bars a demand against the later one |
| V-9 | `checkPath` reverts on an honest index (fuzzed), a malformed path, an invalid old proof, an index out of range of the named root, no pending root, or an unknown root index: one at or above the root count, which is every index while no root is finalized |
| V-10 | `demandPath`: only inside the window and for an account proven against the finalized root it names, any of the pool's, once per `(seq, rootIndex, index)`. A clean `checkPath` against that root answers it, once: a second clean opening, or one with no open demand, reverts `NoFault` and changes nothing; an answered account cannot be demanded again under that `seq`. Demands on one account against two roots are two demands, each answered against its own root, and an account that a later root appended is out of range of an earlier one. An unanswerable demand (garbage or withheld node) blocks finalization; an unprovable position of a finalized root cannot be demanded; refused with no pending root or an unknown root index (every index while no root is finalized); a fee other than `demandFeeWei` refused; an index at or above the named root's leaf count refused; an account that does not verify refused; a demand in the window's last block answered after the window. Regression for the gate 1 review's H1: the newest finalized root's tree is withheld, and a successor lowers accounts of the older root; a demand against the older root still blocks finalization, and its opening deletes the successor |
| V-11 | `finalizePending`: refused with no pending root, before 14,400 blocks, and with an open demand; appends exactly the last commit, after a deletion or a replacement, and clears the slot; `roots` refuses an index at or past the root count, a pending root's included |
| V-12 | Claim against the latest root: pays exactly; one rao short at the destination is accepted, two is not; a refusal (floor, staking-hotkey cap) reverts and leaves the account claimable; depth-0 claim; an escrow loss other than the amount reverts; the escrow's own registration collateral stays in place |
| V-13 | A repeat claim on the same root refused; after a newer root only the increment is paid; neighbouring accounts keep their own watermarks |
| V-14 | An older root after a newer claim refused; the older root first, then the newer, pays the difference |
| V-15 | Claim refusals: an invalid proof; an unknown root index; a zero coldkey or `selfColdkey`; an amount of 2^64−1 or more |
| V-16 | Pool cap: after an unchallenged decrease, the second claimant's claim reverts at `finalizedTotal`; with honest roots it never binds, even for a claim against an older root once claims against a newer one exceed that older root's total |
| V-17 | The payment goes to the leaf's coldkey whoever calls |
| V-18 | `absorbStake`: moves vault2-owned movable stake from a foreign hotkey and leaves every counter unchanged; refuses the escrow hotkey, pool hotkeys and zero; a zero hotkey, stake that is all collateral, and an amount of 2^64 − 1 or more refused |
| V-19 | `assignSurplus`: refuses zero and an amount above the surplus, and a pool or vault funding total of 2^128 or more; the escrow's collateral is never surplus; event attribution |
| V-20 | Reentry through the staking and neuron mocks, or through a registration refund, is refused on every function that calls the runtime |
| V-21 | Two pools: each pool's claims are capped by its own `finalizedTotal` |
| V-22 | A backing deficit makes every stake-moving function revert `Underfunded`, never `checkPath` or `demandPath`; the escrow's collateral is not backing |
| V-23 | Every coordinator-only and bootstrap-only function refuses other callers |
| V-EV | Every vault2 event, with its exact fields |
| V-SIZE | vault2's runtime size is below 24,576 bytes and its creation size below 49,152, from the release build; margins recorded |

### 15.4 `STSettlement2` (S)

| ID | Behaviour |
| --- | --- |
| S-1 | `initialize` runs once; the implementation's initializers are disabled |
| S-2 | `setAuditors`: owner only; empty, too many, a zero address, duplicates and a bad quorum all refused; a version bump voids earlier approvals |
| S-3 | `registerOperatorPool`: owner only; the v1 operator must be active; `firstEpoch` must be in the future; the cadence precondition `epochBlocks >= rootCommitWindowBlocks + 14,400`; exactly vault2's refund is passed on to the caller, and a failed refund reverts; `receive()` accepts native value only from vault2 |
| S-4 | `settlementAt`: unregistered pool, and epochs before, at and after `firstEpoch` |
| S-5 | Close: below `firstEpoch` or for an unregistered pool refused; window edges at 0 and 1,200; gap deferral; an inactive operator's epoch is still closed; paused |
| S-6 | Commit: the root signer of the epoch's version, active or not; window edges; a pending root refused before `end(pending.epoch + 1)` and replaced after it, including an approved root with an open demand; paused |
| S-7 | Approve: auditor only; nothing pending refused; arguments must match the pending root; deadline; a second approval by the same auditor reverts `AlreadyApproved` |
| S-8 | Finalize: no pending root; below quorum; approvals from an old version; vault2 refusals propagate; paused; an approval of an earlier `seq` never counts |
| S-9 | Upgrade: owner only; the storage layout is pinned |
| S-10 | A hostile implementation cannot finalize early, finalize with an open demand, commit a wrong total or assign more than the surplus (pattern: `evm/test/ReleaseSettlement.t.sol:277-350`) |
| S-EV | Every event, with its exact fields |
| S-SIZE | `STSettlement2`'s size and margin, from the release build |

### 15.5 Invariants (I)

A lifecycle handler drives random sequences of: capture and defer; honest and
faulty commits; demand; answer; challenge; approve; finalize; claim; absorb;
and assign. It injects mock rounding and failures. Today's only invariant
suite never exercises the vault lifecycle (`evm/test/ReleaseInvariant.t.sol`).

This one runs in its own deeper profile, `v2_invariant`, with `show_metrics`.
`afterInvariant` asserts minimum counts of finalizations and claims, so a
handler that never reaches a claim fails instead of passing silently.

| ID | Invariant |
| --- | --- |
| I-1 | G1, with the ghost sums of `capturedTotal` and `claimedTotal` |
| I-2 | Per pool, `claimedTotal <= finalizedTotal <= capturedTotal` |
| I-3 | Every `claimed` is at most the largest cumulative that account has in any finalized root |
| I-4 | No root finalizes inside its window, with an open demand, or after a fault was shown |
| I-5 | Funding snapshots are monotone and immutable |
| I-6 | With honest roots only, the pool cap never refuses a claim |

### 15.6 End-to-end in Go (E)

| ID | Behaviour |
| --- | --- |
| E-1 | The last v1 and first v2 epochs through the public readers. Weights follow §9.1; v1 claims still work; the first v2 root is approved, finalized and claimed. A permissionless v1 close at or after E_s puts reveal-lag stake into v1 funding |
| E-2 | Ten epochs, then one claim; later only the increment |
| E-3 | Failure drills: a missed commit; an unfinalized root replaced; a challenged root; recovery with the Alice case; a missed close |
| E-4 | Bad successors: an omitted account, a lowered one, a garbage leaf, an over-allocation hidden in an inconsistent subtree, and a moved coldkey. When the tree is published, `snclaim v2 check` challenges each fault the published account rows reproduce. An over-allocation hidden in an inconsistent subtree is never reproduced from published account rows, so the watcher reports it unverifiable and demands, and any opening under the bad node deletes the root. When the tree is withheld, a demand blocks each. A re-roll drill in which the attacker self-challenges and recommits, refused by vault2's commit spacing, with the watchers' doubled sample |
| E-5 | Daemon: threshold claim; reopen on a newer root; a newer root during an in-flight claim; a third party claiming first; v1 and v2 side by side; restart mid-claim; one claim in flight per account; signed bytes never replaced; an on-demand claim |
| E-6 | Pause stops settlement; claims, approvals, demands and challenges continue |
| E-7 | Sweep path on phase-A bytecode: v1 roots expired, a single-leaf v1 root paying vault2, `absorbStake` from the v1 escrow hotkey, then `assignSurplus` through a test coordinator upgrade. Conservation holds across both vaults |

### 15.7 Components (SV, VA, CL)

| ID | Behaviour |
| --- | --- |
| SV-1 | v2 driver: the §8.1 order; retries; a deferral on a missed close; demand answering; `providers: null` only by explicit command; no commit unless the total grows; every §6.4 and §8.1 alert |
| SV-2 | For an epoch both versions see, v2 usage and wallets equal v1's; no reliability input; earning-window edges `[start, end)`; the cutoff-clip refusal |
| SV-3 | Endpoints: object immutability; account proofs verify; ss58 and hex coldkeys; malformed input; several pools; zero-delta and head-excluded accounts; unknown coldkey |
| SV-4 | The v1 driver never touches epochs at or after `firstEpoch`, except the phase C sweep epoch; a v1 close is signed with at least min(3,200,000, `maximum_gas`) gas, on a first attempt and on a fee replacement, and commit, finalize and deferral keep their estimate plus 20% |
| SV-5 | The email, `/account/epochs` and the mirror show per-epoch v2 amounts; the leaderboard rebuild fires on v2 finalization |
| SV-6 (scale) | Bounded binding candidates and bulk wallet selection at 1M providers inside the §15 budgets; the consent cache per head hash, batched resolutions, the tree cache and account-index growth |
| SV-7 | Consecutive epochs' finalizes and answers each broadcast, with logical keys that include the epoch or `seq`; a v2 close is signed with at least min(3,200,000, `maximum_gas`) gas, on a first attempt and on a fee replacement, and commit, finalize and answer keep their estimate plus 20% |
| SV-8 | v2 contract phase: each builder check, the plan hash, the approval, and each readback mismatch refused |
| SV-9 | The activation record (migration 832). The first successful read of vault2's registration of the pool, under an `STSettlement2` that vault2 names as its coordinator, records the deployment's activation with the configured v2 pair, once; an unregistered pool, a failed read and another coordinator record nothing. After it, a missing or changed pair refuses the sync turn, the event mirror, whose checkpoint stays, every chain write, the v2 operator commands and payout admission, naming each key with its recorded value; the recorded first epoch stays the v1 cutoff whatever a later read answers; restoring the pair resumes from the checkpoint. A different later observation is refused. `bringyourctl st v2-activation` shows the record, `st v2-activation-clear` removes it only when it names every value exactly, and `sn25golive st-check --v2-activation` checks a candidate st.yml against it |
| VA-1 | The shared resolver serves live steering, the production row and replay across E_s, and the production row agrees; an unregistered pool; a pruned v1 pool UID; a `settlementAt` answer that differs from `vault2.pools(noId)` is refused |
| VA-2 | The audit accepts honest fixtures and refuses each GL-9 mutation through the full validator path |
| VA-3 | Approval before the deadline; idempotent across restart |
| VA-4 | A fault in rows that reproduce the root leads to a challenge; missing or unreproducible data, an inconsistent node included, leads to refusal plus a demand set; each demand names the protected root (§9.3), F or, when F's objects are withheld or do not rebuild it, the newest older root custody holds, and carries the account and its proof against it; the sample is weighted by value and drawn without replacement over all of that root's accounts, starts at 256 and doubles after a failed root over the same F, and is sent first, in the order drawn; rows that show a fault are always demanded |
| VA-5 | No weight change after a refused root with one operator |
| VA-6 | Config: today's config hashes identically; the adoption successor admits exactly the block and refuses every other change; load-time address checks; a non-lowercase address refused |
| VA-7 | Custody: objects retained; findings and declarations recorded; a fresh validator re-verifies the previous root's chunks |
| VA-8 | A send that another transaction made unnecessary counts as success (§5): a root already deleted or replaced, an index already demanded, an approval already counted |
| VA-9 | The existing activation and CLOSED_CENSUS keep working under the adopted config |
| CL-1 | `snclaim` commands, including `withdraw --vault v1` and `v2 claim --all` |
| CL-2 | SDK display: every account state; v1 shown separately; "unavailable" rather than zero |
| CL-3 | `snclaim v2 check` reports "unverifiable", never "no fault", when objects are missing or do not reproduce the root; a challenge against a root that has left the pending slot counts as done |
| CL-4 | SDK and app v2 claims (§11): an account is claimed only when its vault is the pinned vault2 and its proof verifies against vault2's root; `claimed` is read from chain; preflight, `eth_estimateGas` and the gas key's balance; a third party's earlier claim counts as done; a floor or staking-hotkey refusal leaves the account claimable; the confirmed amount comes from the `Claimed` event; each app shows v2 beside v1, offers no v2 claim without `Vault2Address`, and presents every claim outcome |
| CL-5 | Batched claims (§10.2): `snclaim v2 claim --all --batch` and the daemon with a batcher claim two or more accounts in one transaction, at most 32; each claim is verified and preflighted alone; the batch is sized by `eth_call` within the most gas the tools name, 13/15 of the block gas limit less 1,500 gas and a 10,000-gas margin; each item's outcome settles its own account (paid, refused, not attempted); a third party's earlier claim counts as done; a refused preflight backs off as a lone claim's does; the daemon keeps the batch's exact signed bytes in every unit until its finalized receipt, and a daemon whose `batcher` changed or went away while a batch is in flight stops at start; both tools refuse a batcher whose code is not the release runtime; a lone claim goes directly to vault2 |

### 15.8 Runtime rehearsal (R)

These run on a node built from runtime 478's source (`850f2755`), with the
exact release bytecode.

| ID | Behaviour |
| --- | --- |
| R-1 | Deploy through §8.5, with readback, the optional claim batcher included |
| R-2 | Capture: exact deltas, a zero epoch, a dust refusal; with registration collateral on the pool position, the close moves exactly `getStake − locked` and leaves the lock |
| R-3 | Claim with a real `transferStake`; a floor refusal; a staking-hotkey cap refusal; third-party stake operations on the escrow hotkey never lower vault2's position, and a transfer of conviction-locked alpha to vault2 is refused; the escrow hotkey earns nothing across a tempo in which a validator sets weights, and by stake in one without; the whole-position sentinel; the signer's own staking-hotkey cap; third parties filling vault2's own staking-hotkey list raise the gas of claims and `absorbStake` without stopping them |
| R-4 | Pool registration, also at a nonzero collateral lock share, whose lock vault2 never moves or counts; re-registration after pruning; stake on a pruned hotkey, the escrow's included; `absorbStake` from the v1 escrow hotkey |
| R-5 | Approvals from an auditor address; a demand paying `demandFeeWei`, and its answer |
| R-6 | Gas used and gas limit needed, at depth 29, for a claim, a demand, an approval, a capturing close, a close deferring one epoch and one deferring many, a commit, an answer and a finalize; the operator's transactions, a capturing close with a few deferred epochs included, fit the re-signed operator gas policy (§8.1, §13) |
| R-7 | Batched claims (§10.2), at depth 29: the block gas limit and the most gas one transaction may name, exactly 13/15 of it less 1,500 gas whatever the calldata; the gas used and the least gas limit of single claims and of batches of 1, 5, 10, 20, 32 and the most one transaction holds, each batch capped at the default gas per claim and paying every item; a batch whose broken proof and claim below the transfer floor are each refused alone, with exact events, while the claims around them pay |

### 15.9 Guarantee traceability

| Guarantee | Tests |
| --- | --- |
| G1 conservation | I-1, I-2, V-5, V-12, V-19, V-22, V-16, V-21 |
| G2 accrued balances | GT-5, GT-7, GT-8, T-3, V-8, V-9, V-10, E-4, GT-4 |
| G3 destination | V-17 |
| G4 no expiry | V-14, E-2, V-13, V-16, I-3 |
| G5 funding binding | V-4, V-6, V-7, I-5 |
| G6 window | V-11, I-4, V-10 |
| S1 commit authority | S-6 |
| S2 approval | S-7, S-8, S-2 |
| S3 replacement and carry | S-6, E-3, GL-4 |
| S4 identity | S-4, VA-1, E-1 |
| S5 pause | S-5, S-6, S-8, E-6 |

### 15.10 Claim batcher (CB)

| ID | Behaviour |
| --- | --- |
| CB-1 | Every item pays its account's claimable to the leaf's coldkey, and after a newer root only the increment; vault2's `Claimed` names the batcher as caller |
| CB-2 | A refused item (a third party's earlier claim, an invalid proof, an unknown root, the runtime's refusals) reports its selector and leaves the account claimable; the other items pay |
| CB-3 | A refusal that consumes all the gas forwarded to the precompile uses at most its item's cap, and later items still pay; a claim that needs more than the cap is refused, not the batch |
| CB-4 | An item the gas left cannot cover is not attempted, nor any later one, reported once; whatever the batch's gas (fuzzed), each attempted item receives exactly its cap, and an item is attempted from the gas its cap needs at the call plus the bookkeeping |
| CB-5 | Payments go only to each leaf's coldkey, whoever calls; the batcher takes no value, touches no storage, and its code has no storage opcode, persistent or transient |
| CB-6 | An empty batch calls nothing; a cap whose requirement overflows reverts |
| CB-7 | The deployment phase's optional sixth action creates the batcher at the deployer's next nonce after the binding; anything but the exact argument-free release creation is refused, and it completes only on readback of its exact runtime (SV-8) |
| CB-EV | Every batcher event, with its exact fields |
| CB-8 | A `vault` argument other than vault2, which only the sender chooses, harms only that sender's batch: much revert data, no code or a short answer reverts it, and re-entry interleaves its events, which the claim tools refuse |
| CB-SIZE | The batcher's sizes from the release build |

### 15.11 Pool registration tool (PR)

`sn-mainnet register-operator-pool` reviews and reads back the activation call
of §12.1 step 5 (`mainnet/BOOTSTRAP-CONTRACTS.md`, "Settlement v2
activation"). It never signs or sends.

| ID | Behaviour |
| --- | --- |
| PR-1 | Review, send and readback against the binding: `review` prints the Safe's exact call, its calldata with the decoded arguments, the contracts `STSettlement2` names, the head, E_s's start block and the blocks remaining, after an `eth_call` preflight from the Safe. The Safe's send of the printed call registers the pool with the UID the preflight predicted. `readback` confirms the event, `vault2.pools` and `settlementAt`, and a second review is refused |
| PR-2 | Local refusals: before any read, a zero pool hotkey and a value other than `maximumBurnRao × 10^9` wei; then a route that is not EVM chain 964, vault2's escrow hotkey as the pool hotkey, and a first epoch whose start block the head has reached, each before the preflight and each exit 3 with nothing printed. One block before E_s starts, review passes. A usage error exits 2 before any read |
| PR-3 | Preflight mappings: the preflight from the Safe names each refusal of `registerOperatorPool`, `InactiveOperator`, `InvalidEpoch`, `CadenceTooShort`, `NativeRefundFailed`, vault2's `InvalidConfiguration`, `OwnableUnauthorizedAccount` and the v1 coordinator's `UnknownOperator`; an unnamed revert and one without data are refused. Each exits 3 and prints nothing |
| PR-4 | Readback differences: no receipt; a reverted transaction; no `PoolRegistered` event of `noId` from vault2, or one with another hotkey or first epoch; `pools(noId)` unregistered or with another first epoch or hotkey; `settlementAt(noId, E_s)` with another vault, hotkey or version. Each exits 3 with exactly its differences |
| PR-5 | Read failures: an RPC error at each read, an answer its binding cannot decode and an output that cannot be written each stop both modes with exit 1 and no output. The read profile refuses every other method, so neither mode can send |

## 16. Later phases

- **Roster and evidence.** When clients produce signed work requests and SDK
  cuts, `provider_work.yml` engages (`server/controller/provider_work_admission.go:33-106`).
  The ledger schema then gains evidence and wallet-authority sections, and
  the validator verifies consent chains. The meta review's A4 audit-sampling
  design belongs to this phase.
- **Phase B, measurement.** Sampling frame, VRF cohort, stratified estimator,
  head budget and validator state bounds, with the meta review's open items.
- **Phase C, the v1 carry sweep.** The sweep epoch satisfies
  `E_sweep >= E_s + 8`, so its close follows the last v1 expiry,
  `epochStartBlock(E_s + 9) − 1`:
  1. Call `finalizeOperatorEpoch` for every still-funded v1 epoch from E_s
     on, and `expireEntitlement` for every expired v1 root, using the
     recorded expiry blocks.
  2. In `E_sweep`'s close window, close on the v1 proxy.
  3. Commit and finalize a v1 root with the single leaf
     `(vault2 selfColdkey, 10,000 bps)`. v1 folds all carry into it
     (`evm/src/STSettlementVault.sol:334-336`). Its `artifactHash` is the
     sha256 of a sweep record listing each coldkey's expired remainder,
     computed from vault1 events after the last ordinary
     `EntitlementFinalized`: `total × share_bps / 10,000 − Claimed`.
  4. Claim the leaf, then `vault2.absorbStake(v1 escrow hotkey)`.
  5. The phase C `STSettlement2` upgrade adds an `assignSurplus` wrapper and
     a sweep-epoch record for replay readers.
  6. The next v2 root carries a recovery component. Expired remainders are
     allocated pro rata. Missed-root, deferred and reveal-lag captures are
     allocated by their source epoch's rows. Phase C allocation uses
     `f_k − surplus_k` for usage.
- **Phase C, other items.**
  - A second operator, with an auditor set per pool and the weight remedy.
  - An approval quorum as a fraction of the auditor set.
  - Operator bonds.
  - A priced deposit policy, which must also respect the consent-domain
    constraint (§2.2).
- **Null consensus:** a separate track, unchanged from
  VERSION2-REVIEW-RESPONSE.md §13.
- **Optional.** A UR sweep relayer; daemon self-checks; a third-party mirror
  of the ledger objects.

## 17. Risks

| Risk | Mitigation |
| --- | --- |
| A vault2 defect is permanent for the funds it holds | Gates 1 and 2. After the first v2 finalization, claim at least one account before relying on it |
| The rehearsal node is a fast-runtime dev chain, not mainnet | It keeps mainnet's call set, weight formulas and gas schedule (`scripts/runtime-rehearsal/README.md`); gate 2 cannot be waived, and any runtime change repeats it |
| vault2 depends for good on the ABIs of the staking precompile's `getStake`, `getMinerCollateral`, `moveStake` and `transferStake`, which are not formally versioned. A runtime that changes or drops one freezes every capture and claim, and immutable vault2 cannot adapt; v1's vault shares the dependency on all but `getMinerCollateral` | Each runtime release is rehearsed with vault2's exact bytecode on the gate 2 node when it is published, so a break can be raised upstream before it reaches mainnet. Claims are encouraged, which keeps custody small |
| SN25 is dissolved. A subnet registration at the subnet limit dissolves the non-immune subnet with the lowest moving alpha price, and root can dissolve any subnet. Dissolution turns every SN25 position into free TAO on its coldkey. vault2 cannot pay out native TAO (its only native outflow is a registration's refund of the caller's own value), so its custody would be stranded and every claim would revert. v1's vault has the same exposure | Accepted by the owner (2026-10-11). The server alerts when SN25's moving price nears the lowest among non-immune subnets, the one a registration at the limit dissolves. Claims are encouraged (the daemon, the apps' v2 claims), which keeps custody small |
| The SN25 owner key (`ur-owner`) or the chain's root disables stake transfers on SN25 (`sudo_set_toggle_transfer`; upstream `pallets/admin-utils/src/lib.rs:1649`). Every `transferStake` then fails `TransferDisallowed` (`staking/stake_utils.rs:1795-1807`), so no v1 or v2 claim pays until transfers are enabled again | Nothing is taken or redirected. A frozen v2 claim reverts and stays claimable, nothing expires, and captures continue, because `moveStake` skips the transfer check. Recommended: hold the SN25 owner key under the Safe's governance, so that a toggle is decided and announced like a Safe action. That needs no coldkey swap: SN25 ownership stays with `ur-owner` (`mainnet/LAUNCH.md`). Root's switch belongs to the chain |
| Someone registers vault2's escrow hotkey between vault2's creation, which reveals it, and `registerEscrow` | Both are broadcast back to back at consecutive nonces (§8.5). If it still happens, `registerEscrow` fails for good and vault2 is redeployed. `registerPool` requires the escrow registered, so that vault2 never holds a pool, and no funds are at risk |
| Admission bypassed (an owner upgrade, or root signer plus auditor keys) with the tree withheld | Demands block finalization (§6.5). They may name any finalized root, so withholding the newest root's tree does not stop them (§4.2). Watchers sample by value; a faulted account that is never demanded is lost |
| A root finalized after a bypass that is not tight, or that lowered an account of an older root, halts honest successors | The validator refuses to build on a root that is not tight, and anyone can delete a successor that keeps an account below an older root's value; recovery needs a reviewed validator release and is outside phase A |
| Demand griefing | Each demand costs the demander its gas and the fee. An answer costs about 1.5 times a demand's gas (R-6 on 478: 149,026 against 98,256), and the fee is one answer's gas at the planning fee: 750,000 rao (§7.1). Each account of each finalized root can be demanded once per pending root, so the demands possible on one pending root grow with the number of roots; each still pays the fee. Answers are automatic (§8.1) |
| On 478 the gas limit a stake-moving transaction needs follows vault2's own staking-hotkey list, about 8,750 gas an entry, which third parties can lengthen after an estimate, and a refused precompile call consumes all the gas forwarded to it (§7.3) | Senders preflight with `eth_call` and never sign a stake move below its pre-478 size: a claim at the larger of its estimate with margin and 2,600,000 gas, a close at the larger of its estimate plus 20% and 3,200,000 (§8.1); the operator gas policy is re-signed to cover the close floor (§13); anyone can send a close that defers more epochs than the policy covers |
| Third parties fill vault2's own staking-hotkey list, up to 128 entries | Claims and `absorbStake` keep working; a claim then uses about 1.26M gas instead of 0.15M and needs a limit of about 1.27M, within the 2,600,000 claim floor (R-3 on 478), about 1 α at the §3 figures, so the daemon's threshold should rise to match (§10.1). Each entry costs the griefer a transfer above the floor, which vault2 can absorb as surplus |
| A missed close moves an epoch's share to the next epoch's earners | The 1,200-block window and alerts |
| App users without a paying consent stay unpaid | Network payout consent in the SDK and every app (§11.1), independent of phase A. Epochs before a network's first consent stay unpaid |
| The owner's availability for Ledger-signed steps | Plan steps 2, 3 and 5 of §12.1 with the owner |
| Treasury approval expiry about 2026-12-17 UTC | A separate successor after adoption (§13) |
| Small balances are uneconomic to claim | Accrual never expires; the threshold is published; an optional UR subsidy |
| The escrow hotkey earns emission and its share price rises: Yuma pays by stake in a tempo in which no validator sets weights, and a future consensus change could pay it too | Yuma pays the escrow nothing while a validator sets weights, which the validator does every tempo. Escrow earnings are surplus, and claims accept a destination one rao short. The Null track must revisit this |
| A later v1 policy breaks `epochBlocks >= rootCommitWindowBlocks + 14,400` for v2 epochs | A governance constraint on the v1 policy schedule (§13); `registerOperatorPool` checks only the first epoch's policy |

## 18. Implementation order

These tracks run in parallel. Dependencies are noted.

1. **Protocol** (first): the Go tree and vectors
   (`merkle/ledger_sum_tree.go`, `merkle/testdata/ledger_v2_vectors.json`),
   the allocation rule (`protocol/ledger_allocation.go`), and the ledger
   codec, builder, verifier and demand set (package `ledger`). This unblocks
   tracks 2, 4 and 5.
2. **Contracts**: `evm/src/LedgerTree.sol`, `evm/src/STSettlementVault2.sol`,
   `evm/src/STSettlement2.sol`, and the T, V, S and I suites.
3. **Runtime and deployment**: a node built from runtime 478's source with
   fast blocks (`scripts/runtime-rehearsal/`); the R battery (`rehearsal/`);
   the v2 contract phase in `sn-mainnet bootstrap-contracts`.
4. **Server**. The scale changes to bindings, wallets and resolutions can
   start now. Then the builder (needs track 1); the driver, demand answering
   and endpoints (need track 2's ABI); and the readers.
5. **Validator**: the shared resolver, the audit, approval, challenge and
   demand sending, and the adoption successor class.
6. **Miner, SDK and apps**: the claim daemon v2
   (`provider claim-daemon-v2`), `snclaim v2 status|claim|check` and
   `snclaim withdraw --vault v1`, the SDK display and claims, and the apps'
   v2 display and claims (§11).
7. **End-to-end and ceremony**: the E suite, the scale fixtures, adoption,
   the re-sign, then §12.1.
