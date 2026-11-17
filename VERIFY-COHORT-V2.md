# Verify cohort v2: retire idle members, admit by rank

**Status:** approved 2026-11-17 ~20:15Z ("Approve, ride 11-27"): the
recommendations of §9 items 1-8 and 12, built on branches for the validator
release before 2026-11-27 18:28Z. Not live. Items 9 (UID 210), 10 (the seven
fleets) and 11 (board precision) stay open. On 2026-11-17 ~19:45Z the owner chose
"durable fix only": no stopgap admissions and no limit raise in the meantime.

**Built differently from the proposal:**
- **No lineage monotonicity.** §2.1 proposed refusing a keep-all fold after an
  R1 fold in the same lineage. That would also refuse the planned rollback target
  (§2.3), which produces keep-all folds again, so the verifier accepts either
  fold for every transition. A validator that stops retiring is caught by the
  census interlock (§2.5), not by the verifier.
- **The census field is an optional v2 member, not a schema step.** The head
  summary carries `census_providers` and, for the coverage metric (§5.3),
  `bound_clients_observed`: how many of the recipient cut's eligible bindings
  served a verified hop that tempo. The ratio to the cut's bindings is the
  claimed-fleet coverage (`docs/spec/head-summary-v2.md`).

**Sources:** sn `39aecf4f`, server `eb946dd84`. Production figures are read-only
reads of the operator's database and of Finney on 2026-11-17, with the board
snapshot of 18:33Z. Paths without a repo prefix are in sn. The operator's
per-network evidence is in a private note (factury
`research/verify-cohort-evidence-1117.md`); this public document carries only
aggregates and on-chain facts.

## 0. Summary

**The problem.** The server's verify cohort (the measured group) admits a new
unclaimed provider only while its census, every provider ever admitted, is under
2,000. That census has been full since 2026-10-31 and can never shrink, because
the validator never forgets a provider it has scored and fails hard past 4,096.
About 130 of its 2,001 members still get measured. 171 of the board's top 200
networks aren't measurable, and the claim guide tells them not to claim until
they are.

**Recommendation: option A.**
- **Validator.** At each settlement fold, drop the carried quality of every
  provider that had no assignment in the whole closing window (rule R1, §2.1).
  No wire format, bound, config field or setting changes.
- **Server.** Replace the lifetime census with a rolling one. A member counts
  from its first assignment until the validator has folded two settlement
  windows after its last (§2.4). This bound holds by construction and needs no
  messages between server and validator. The limit stays 2,000, so even the
  worst case stays at 4,000 of the validator's 4,096 (§2.5).
- **Admission.** Freed room goes in this order (§2.6):
  1. claimed fleets, at most 7 devices each, as today;
  2. one device for each unclaimed network ranked in the board's top 400;
  3. carried members;
  4. a uniform sample.

**Alternative: option B**, the VRF sampling cohort of VERSION2 §5.2. It is
verifiable but needs a new artifact surface, a new signed config and weeks of
work, and it gives no rank priority. Keep it as Phase B (§3).

**For miners (§4):** nothing a miner is paid changes, and the 7-per-fleet rule
stays. Unclaimed top-ranked networks become measurable. A device idle for two
whole epochs leaves the measured group. That calls for a notice update, not a
signed amendment.

**When (§6):** the top 200 become measurable within about two hours of the
validator's first retiring fold. That fold is 2026-11-27 ~18:28Z if R1 rides the
next validator release, or 2026-11-20 ~18:28Z with a minimal release before then.

**The owner's decisions are in §9.**

## 1. The problem, verified

### 1.1 How the bound works today

- **The validator's census** is the union of four sets:
  - this settlement window's providers;
  - every provider with a quality EMA;
  - the reporting EMA;
  - this tempo's egress providers.

  It is bounded by the signed `max_providers`, 4,096, a fixed capacity dimension
  (`validator/measurement_stats_v2.go:36-80`,
  `validator/attempt_settlement_v2_runtime.go:225-262`, `VERSION2.md` §3.1).
  Past the bound the measurement and the settlement fail and the run exits.
- **The fold never drops a provider.** It updates the EMA of each provider with
  `a_min` or more assignments, carries every other EMA forward and clears the
  window (`validator/stats.go:482-501`). In v2 the post-fold is every provider
  with a quality (`validator/attempt_transition_v2_verify.go:276-289`), and the
  signed transition must equal the engine's fold
  (`validator/attempt_settlement_v2_authority.go:298-345`). So the census holds
  every provider the lineage has ever scored.
- **The server therefore bounds what it ever admits**
  (`server/model/verify_cohort_model.go:243-286`):
  - 1,500 providers per settlement epoch;
  - a new unclaimed provider only while the census is under the lifetime limit,
    2,000: it is refused at `ever >= bound` (`:269`);
  - claimed fleets' members up to 3,072 (`server/controller/verify_controller.go:322-343`).
- **Rotation** (`server/model/verify_cohort_rotation.go`) frees active room only
  for claimed members waiting for it. Rotated members stay in the census.

### 1.2 Production, 2026-11-17

| Read | Value |
| --- | --- |
| Census (`verify_cohort_census`) | 2,001. The first rotation seeded 2,000 at 2026-10-31 06:49:57.311Z, and UID 145's bound client entered at 06:49:57.593Z through the claimed path. None since. |
| Its clients | 421 deleted, 1,092 deactivated, 488 active, 118 connected |
| Who holds it | 85 networks. One holds 251 places, all on deactivated clients. 117 of the 118 connected members belong to the 24 measurable top-200 networks. |
| Deployed sizes | 1,500 per epoch (every board snapshot), 2,000 lifetime (rotation's first log) |
| This epoch's cohort | epoch 5: 17 → 155; epoch 6: 114 → 139, of 1,500 places |
| Providers assigned per epoch (server records) | epoch 3: 318, epoch 4: 285, epoch 5: 155, epoch 6 so far: 139; on 200k-360k assignments per epoch, about 3,700 a tempo |
| Verify-eligible providers | 31k-51k |
| Validator's own census | 1,403 on 2026-10-31 (`server/COHORT-ROTATION.md`). Not read today: the state file is root-only. It is at most 2,001 plus anything scored outside the cohort. |

Connection history can't tell idle members apart: closed connection rows are
reaped within about a day, and 1,871 of the 2,001 have none. Any idle rule must
use the cohort's own assignment records.

### 1.3 Effect on the Top 200 (snapshot 18:33Z)

- 24 measurable, 171 `not_in_cohort` and 5 `shared_egress`.
- Of the 171, 154 have no active census client. An unclaimed network outside the
  census can never be drawn, so it can never become measurable.
- Server `eb946dd84` makes "You qualify" and the claim prompt follow
  measurability, and the guide says to bind only when the network is measurable.
  So these networks have no path in.
- Ranks 201-400: none measurable.
- The line at 200 sits in a near-tie. The projected 200th score is 0.99999938,
  one unshared prefix, and 82 of the top 200 have exactly one prefix.

## 2. Design (option A)

### 2.1 Validator: retire idle priors at the settlement fold (R1)

**Rule R1.** At the fold of settlement window `w`, a provider whose quality was
carried into `w` and that had no assignment in `w` leaves the post-fold: its
exact and reporting EMAs are dropped. There is one exception: if no provider had
an assignment in `w`, the fold retires nobody. That covers a validator that
measured nothing for a whole epoch, which would otherwise wipe every prior and
give the pool `Q_n = 0`, an empty channel, until providers reach `a_min` again.

Everything else is today's rule:
- A provider with 1 to `a_min − 1` assignments keeps its EMA untouched. One
  sparse epoch still doesn't decay an established provider (`VALIDATOR.md` §11.1).
- A provider with `a_min` or more is folded at α = 1/10.
- A new provider below `a_min` stays absent.

**Where it goes** (the v2 path only; legacy and testnet folds are unchanged):

- `attemptSettlementV2Qualities` derives the post-fold with R1. On the verify
  side, `replayEach` accepts exactly two post-folds for a transition:
  - the keep-all fold, which every transition before the release has;
  - the R1 fold.

  Any other subset is refused (`validator/attempt_transition_v2_verify.go:276-330`).
  As built there is no lineage monotonicity (see Status). The provisional startup
  reuse check does the same (`validator/provisional_startup_reuse_v2.go:41`).
- `attemptSettlementV2Postimage` applies R1 to the engine's fold when the
  terminal's post-fold is the R1 fold, so the journal's `stats.json` postimage
  keeps matching (`validator/attempt_settlement_v2_authority.go:298-345`).
  `ema` and `emaPPM` are retired together: the census check requires equal key
  sets (`validator/attempt_settlement_v2_state.go:57`).
- Nothing else needs to change. A successor's priors must equal its
  predecessor's post-fold, whatever it holds
  (`validator/attempt_closure_v2.go:124-153`,
  `validator/release_startup_history_v2.go:237-255`,
  `validator/attempt_settlement_v2_state.go:60-90`).
- **No activation epoch and no config field.** `ReleaseStatsConfig` can't carry
  it: a successor's config must equal its predecessor's
  (`validator/attempt_closure_v2.go:125`). Because the verifier accepts both
  folds, the rule takes effect at the first fold the new build performs, and a
  release that slips past a boundary can't strand the startup replay on a fold
  it expected to retire.
- **The census count is published.** The per-tempo head summary gains an optional
  `census_providers`, the provider count of the tempo's raw measurement: the
  union above, so the census exactly (`validator/head_summary_upload.go`, an
  optional v2 member in `protocol/headsummary`). The server uses it as a safety
  interlock (§2.5) and for monitoring. A summary without it reads as "retirement
  unknown".

**What it bounds.** With R1, while the validator measures, its census in window
`v` is contained in the providers assigned in windows `v − 1` and `v`. Egress
sets rotate at every settlement fold
(`validator/attempt_settlement_v2_authority.go:335`), so they add nobody. The
census is set by the last two windows, never by history.

### 2.2 A provider's history and weights when it leaves and returns

- **Quality.** The EMA is dropped at the fold that ends its first fully idle
  window.
  - On return, its first window with `a_min` or more assignments sets a fresh EMA
    from that window alone.
  - Until then it is unscored, like any new provider.
  - With α = 1/10 the EMA's memory is about 10 epochs; it is lost after one idle
    epoch.
- **Weights.** Per-provider quality feeds only the pool scalar `Q_n` (WHITEPAPER
  §8.1, §10; `validator/measurement_stats.go:531-555`). It is clamped to
  [0.75, 1] and normalised within the pool channel. With one network operator
  the pool's weight doesn't depend on `Q_n` above zero, and the guard keeps it
  above zero. An idle prior weighs 1 in `Q_n` today, so retiring the stale ones
  moves `Q_n` itself a little. The row a validator commits doesn't change.
- **Pay.** The v2 ledger allocates by usage only, and measurement no longer gates
  payment (`VERSION2-FINAL.md` §1). Retirement takes nothing from a pool miner.
- **Head.** The head score is per-tempo prefix breadth from verified hops, smoothed
  per fleet by the head EMA store, which is not part of this census. A retired
  member's fleet keeps its head EMA, and an unobserved live fleet decays by ¾ per
  tempo as today. A returning member's hops count from the tempo it is drawn
  again.
- **Server membership.** The member leaves the rolling census two windows after
  its last assignment (§2.4). When it returns it is a new admission, and a
  claimed fleet's members are first in line.

### 2.3 Scoring state, nonce and timelock

- **Scoring state.** `stats.json` shrinks. The startup replay, about 30 minutes,
  re-verifies mixed history: keep-all folds before the release and R1 folds
  after.
  - Replay memos and cut replay are unaffected, since only the post-fold
    comparison changes.
  - The production capacity is unchanged: `max_providers` stays 4,096, and the
    transition stays under `max_transition_bytes` (about 414 B a provider).
- **Nonces.** R1 adds no extrinsic. Commits, reveals, activation and evidence
  writes are unchanged.
  - The stopgap fallback still shares UID 1's hotkey. A build without the
    nonce-wait fix (sn `361a7004`) still needs the stopgap timer stopped across
    the deploy.
- **Timelocks.**
  - CRv4: the committed and revealed row is unchanged (see §2.2), and so is the
    "applied" check.
  - STSettlement2: its 72 h timelock governs ledger roots, not statistics.
    Unaffected.
- **Rollback.**
  - The pre-release build can't replay an R1 fold, so after the first one it is
    not a valid rollback target.
  - The rollback target is the release with R1's producer line reverted: it
    keeps accepting R1 folds and produces keep-all folds again. Build it before
    the deploy.

### 2.4 Server: the rolling census

**Definitions.**
- `S(e)`: the providers the server assigned in its settlement epoch `e`. These
  are today's per-epoch sets `{verify_cohort:<deployment>}e<e>`, whose 30-day ttl
  outlasts the three-epoch horizon.
- `last(p)`: the latest `e` with `p ∈ S(e)`.
- `v*`: the validator's settlement epoch in its latest signed head summary. The
  server already stores these: `sn_head_summary.settlement_epoch`, about 40
  minutes after each boundary. A summary exists only after the fold into its
  epoch, so `v*` never runs ahead of the validator.

**The rule.** A member counts against the limit `L` while `v* < last(p) + 3`,
that is until the validator has folded two windows after its last assignment. An
epoch in which the server assigned nobody doesn't count toward the two. That
mirrors R1's guard, since assignments come only from trails that validators walk.

**State.**
- The census set becomes the rolling set, and the lifetime set merges into it.
- `verify_cohort_census` gains `last_epoch`: NULL for the legacy rows. The
  taskworker updates it in bulk each run, and deletes rows on expiry.
- Redis loss still fails closed: no new provider until restored, as today. A
  restore treats every durable member as assigned in the current epoch, which
  only delays expiry.

**Admission** (the one script, as today):
- A new provider enters only while the rolling set is under its tier's bound
  (§2.6) and the per-epoch set is under 1,500.
- **Interlock:** it also needs the validator's reported census plus new
  admissions since that report under 3,584, 7/8 of 4,096. A counter in the same
  script holds the admissions since the report, and the taskworker resets it
  when it reads a new summary.

**Expiry.**
- The taskworker runs it after each binding sync, about every minute, replacing
  the stale rotation.
- It runs only while the latest summary carries a census count. With an older or
  stale summary nothing expires.
- One script removes a batch and re-checks the current epoch set, so a member
  re-drawn during expiry is never lost from the count.

**The legacy release.** The 2,001 legacy rows (no `last_epoch`) count until the
validator has folded once with R1: `v*` at least one more than the epoch of the
first summary carrying a census count. Then they expire unless they are in a
recent `S(e)`. This needs no constant and no setting: the summaries say when.

**Settings.** No new setting.
- `cohort_lifetime_limit` keeps its key and value, 2,000, and becomes `L`.
- `cohort_size` stays 1,500.
- The 3,072 claimed census limit goes. Claimed members are protected by
  headroom instead (§2.6).

### 2.5 How server and validator agree without a race

They don't exchange membership. The server keeps a superset by construction.

1. **Boundary skew.** A validator window can include assignments the server
   labelled one epoch either side. Attempts that straddle a boundary finish in
   the window they started, and the validator can see a boundary seconds before
   the server's mirror does. So `A_V(w) ⊆ S(w−1) ∪ S(w) ∪ S(w+1)`.
2. **R1 bounds the census.** While the validator measures, its census in window
   `v` is within `A_V(v−1) ∪ A_V(v)`. Every member has `last(p) ≥ v − 2 ≥ v* − 2`,
   so the rolling set holds the whole validator census.
3. **Stale or missing summaries only delay expiry.** Nothing is freed on a guess.
4. **The guard's edge.**
   - A validator alive for the first minutes of an epoch and then down for the
     rest can show the server a non-empty `S(w)` while its own window is empty.
     It then keeps one extra window of priors the server stopped counting.
   - Each window's members were once counted under `L`, so the validator holds
     at most `2L`.
   - With `L = 2,000` that is 4,000 < 4,096. The same margin covers a second
     validator on its own outage pattern.
   - Raising `L` above 2,048 needs that edge closed first, for example by
     counting only epochs the validator's summaries show it measured in.
5. **The interlock is independent of all of this.** If the validator stops
   retiring, for example after a rollback to a keep-all build, its reported
   census climbs and admission stops at 3,584 before the hard bound.

### 2.6 Admission once room frees

The draw order. Each draw takes the first source with an eligible member that
isn't on dead-hop probation (`server/model/verify_cohort_model.go:426-546`):

| Tier | Who | Bound | Growth gate |
| --- | --- | --- | --- |
| A | Claimed fleets' live members, at most 7 each (cohort size / 200 seats), as today (`SetVerifyCohortPriority`) | `L` | skipped |
| B | One representative per unclaimed network in the board's top 400 (`SnTopPagedPositions`) | `L − H` | skipped |
| Carried | Rolling members not yet drawn this epoch | already counted | skipped |
| Sample | Uniform over eligible providers, as today's new draw | `L − H` | as today |

- **Headroom H.** Room held for claimed members: the claimed members waiting now
  (rotation's demand today) plus 7 × the fleets whose bindings start at the next
  epoch boundary. The binding sync knows both, so there is no constant. A newly
  bound fleet finds room the day its binding starts.
- **One per network first.** Tier B gives each ranked network exactly one
  measured device: enough for "measurable" and for the board's trail
  calibration.
  - Carried members from large networks come after tier B, so they can't crowd
    it out.
  - They stay only while active. Today one network holds 251 places, all
    deactivated.
- **Choosing the representative**, recomputed at each board fold (once a tempo)
  into a priority set with a 2 h ttl:
  1. the network's eligible provider already in the rolling set, if any (it costs
     nothing);
  2. otherwise one whose prefix isn't held by a higher-ranked network's
     representative;
  3. not on dead-hop probation;
  4. the lowest client id.

  A representative on probation is replaced at the next fold.
- **Why the top 400 and not the top 200.** The 200 line falls inside a near-tie.
  Over 100 one-prefix networks sit between 0.9996 and 1.0, and a fresh network at
  exactly 1.0 outranks older ones whose EMA sits just below. Covering 400 means a
  network's measurability doesn't flip on float noise at the line.

**Anti-sybil:**
- **Shared egress.** An exact address shared by clients attributes to none of
  them, so none is eligible, as today (`server/model/verify_model.go:711`).
- **Shared prefixes.** One tier-B representative per /29 or /48 prefix. The
  board already splits a shared prefix's score among the units holding it.
- **Fresh networks.** Tier B requires a top-400 rank in at least 3 of the last 4
  folds, about 3.6 h. That mirrors the "likely" rule's 3-of-4 and limits churn
  at the boundary.
  - No network-age gate. It would refuse new miners, which the owner has turned
    down for sign-ups (2026-11-13, 11-14).
- **Claimed fleets** pay a UID burn each, which is the head's native sybil cost.
  The 7 cap keeps one fleet from taking another's share. Splitting a fleet into
  one-device fleets costs one burn per fleet, and split-adjusted scoring makes
  the split earn no more than one fleet with the same prefixes.

**Sizing today.**
- Tier A: 9 members. Every bound fleet has one device: UID 145, UID 210 (not
  eligible, §7), and seven one-device fleets from epoch 7.
- Tier B: at most 400.
- Carried: about 130.
- Total: about 540 of the 1,500 per epoch, against `L = 2,000`.
- At a full head, 200 × 7 = 1,400 claimed plus tier B exceeds 1,500. Revisit the
  per-epoch size, and `L` with the census count in hand, once claimed fleets
  pass about 100.

**Per-tempo coverage.**
- At about 3,700 assignments a tempo, a cohort of 540 draws each member about
  7 times a tempo. A cohort of 1,500 draws each about 2.5 times, so about 8% of
  members would miss a tempo.
- Add a metric for claimed members' per-tempo coverage. If it falls below 95%,
  draw tier A first within each tempo.

## 3. Option B: the VRF sampling cohort

`VERSION2.md` §5.2 and its review (`VERSION2-REVIEW-RESPONSE.md` F6) describe a
cohort sized to measurement capacity:
- the server commits a sorted, stratified eligible frame each tempo;
- the validator draws `m` members per epoch by a VRF of a finalized block hash,
  plus a replacement budget `R`, and verifiers recompute the draw;
- no per-provider EMA is carried (K = 0), and the operator's estimate gets a
  scalar EMA instead;
- its bounds are `m = 3,072` and `m + R = 4,096`.

`VERSION2-FINAL.md` §16 schedules it as Phase B. There are two ways to take it:

- **B1, light.** The server samples each epoch with no census at all, and the
  validator carries no per-provider EMA.
- **B2, full.** As written: frame, VRF, proofs, new bounds.

| | A: retire idle, rolling census | B1: per-epoch sample, K = 0 | B2: VRF cohort |
| --- | --- | --- | --- |
| Validator change | One fold rule, a two-way verifier, a summary field | `Q_n` gets a carried operator scalar. That means a new transition field and new lineage checks. Without it, `Q_n` can be 0, an empty pool channel, in each epoch's first tempos, until members reach `a_min`. | B1, plus VRF draws, frame and membership-proof verification, and a new signed bounds schema with the owner's production approval |
| Server change | Rolling set, expiry, tier B, interlock, one column | Per-epoch sampling with tiers; no census | Frame commitments each tempo, index-based assignment with proofs, new endpoints |
| Wire formats | None (a subset post-fold; an optional summary field) | Transition schema | Several new artifacts |
| Validator census | At most `L` in practice, `2L` worst case | At most the per-epoch size plus skew | At most 4,096 by design |
| Validator load | At or below today's | Lower | Lower state, more verification |
| Selection verifiable by third parties | No, the operator chooses, as today | No | Yes |
| Rank priority for the Top 200 | Yes, tiers A and B | Possible with tiers | Only through strata the operator labels |
| Effort | About a week of work, then the deploy gates | 2-3 weeks | Months; scheduled after phases A and C |

With one operator and one validator, B's verifiability buys little today, and
the Top 200 needs rank priority, which A gives directly. A is also a step toward
B: R1 is the eviction F6 says any rotation needs ("Phase 0's Redis rotation also
needs the validator to expire its quality maps"), and B2 can later replace the
server's tiers with strata.

## 4. The published rules

| What miners were told | Where | With A |
| --- | --- | --- |
| The runtime 491 authority and its rules: fleet writes need the reviewed document for the chain's runtime | `mainnet/fleet-runtime-authority/runtime-491.json`, `miner/FLEET-MAINNET-RUNTIME.md` | Untouched. Nothing here involves the chain runtime or fleet commands. |
| Claiming is optional; the pool is split by completed usage | binding-open notice | Unchanged |
| A fleet is scored on the devices the validator measures; at most 7 per fleet draw ahead into the measured cohort; bind at most 7 unless the others are already measured | binding-open notice, ur.xyz /mainnet, ur.io claim guide | The 7-first rule is unchanged. **Changed:** an "already measured" device beyond 7 stays measured only while active; idle for two whole epochs, it leaves and returns as a new admission. No fleet has more than one bound device today. |
| Bind only when the network shows as measurable and is prompted to claim | ur.io claim guide, `/sn/head` (`eb946dd84`) | Unchanged, and now reachable: top-400 networks are measured |
| Per-provider quality is EMA-smoothed across epochs; an established provider below `a_min` carries its EMA unchanged | `VALIDATOR.md` §11.1 | **Amended:** a provider with no assignment in a whole window, while the validator measured anyone, leaves the census and starts fresh |
| Head score: split-adjusted routable prefixes from the validator's own trails, ¼ EMA, top 200 | WHITEPAPER §8.4, §10 | Unchanged |

**Is an amendment needed?** No signed amendment. Amendments
(`amendment/amendment.go`, the §8.6 process) change how settlement counts from
an epoch, and none of this changes settlement, the head formula or any payout
input. What changes is the validator's per-provider quality bookkeeping
(`VALIDATOR.md` §11.1, edited with the code) and the operator's admission policy.

**Notice.** Update the binding-open notice (sn
`mainnet/notices/2026-11-17-top200-binding-open.md`, ur.xyz `mainnet.js`, the
ur.io claim guide's "Bind at most 7" line), and add a /mainnet timeline item for
the first R1 epoch. It should say:
- the top-ranked unclaimed networks are measured;
- an idle device leaves after two epochs.

Publish it when the validator release is scheduled, before the first R1 fold.

## 5. Rollout

### 5.1 Order

Both orders are safe. Server first is better, because it is inert until the
validator's summaries carry a census count, so room appears by itself at the
first R1 fold.

1. **sn:** R1, the two-way verifier, the summary's `census_providers` and
   `bound_clients_observed`, `VALIDATOR.md` §11.1, and tests (§5.4). Merge to main.
2. **server:**
   - the rolling set, expiry, tier B, headroom and the interlock;
   - the `last_epoch` migration and its monitor migration contract;
   - metrics with dashboard panels;
   - tests.

   It builds on (1) for the summary type. Merge.
3. **warp:** the alert rules (§5.3). They ride the next grafana roll, not during
   an incident.
4. **server deploy:**
   - The migration applies with the nightly or with `db migrate`.
   - Then every api and alt, since alt mounts the api router and old scripts must
     not run beside the new taskworker.
   - Then the taskworker.
   - Each roll goes through the validator gate (intent finalized or applied, at
     least 15 min to the next native boundary) and the version guard.
   - Until v3 summaries arrive nothing changes: the census stays the legacy 2,001.
5. **The validator release** through `playbook-validator.yml`, with the owner's go
   since it sets weights:
   - right after an intent reads finalized or applied, never while pending;
   - the stopgap timer stopped if the build lacks `361a7004`;
   - expect about 30 min of startup and a 30-60 min cold first pass, so deploy
     at least half a day before the settlement boundary meant to be its first
     R1 fold.
6. **Watch:**
   - v3 summaries arrive, and their census count reads the current census;
   - at the next settlement boundary the validator folds with R1, and the
     epoch's first summary shows the drop to about the last window's providers;
   - the taskworker releases legacy rows, tier B admits;
   - the next board fold shows the top 200 measurable.
7. **The notices** (§4), merged when step 5 is scheduled.

### 5.2 No hidden settings

No new verify.yml key, flag or knob:
- `L` is the existing `cohort_lifetime_limit`;
- `H` comes from bindings;
- 400, 3 of 4 and 3,584 are constants with their reasons in code;
- the validator rule has no switch.

The merge and the deploy are the switch.

### 5.3 Monitoring and alerts

Server metrics, each with a dashboard panel (`go test ./grafana/`):
- `urnetwork_verify_cohort_rolling_members`, `_rolling_limit`;
- `_admissions_total{tier}`;
- `_admission_refusals_total{reason}`, where reason is `rolling_full`,
  `epoch_full` or `interlock`;
- `_expirations_total`;
- `_ranked_members`;
- `_validator_census` and `_validator_census_age_seconds`, from summaries;
- `_claimed_tempo_coverage_ratio`;
- `urnetwork_sn_top_top200_measurable`.

Alert rules (warp grafana):

| Alert | Severity | Condition |
| --- | --- | --- |
| VerifyCohortValidatorCensusHigh | page | the reported validator census > 3,584 |
| VerifyCohortValidatorCensusStale | warn | no summary with a census count for 3 tempos while the validator steers |
| VerifyCohortRollingFull | warn | the rolling set ≥ 95% of `L` for 2 h |
| SnTopTop200Unmeasurable | warn | under 90% of the top 200 networks with an eligible provider measurable at two folds after an epoch's first tempo |

New monitor alert classes go in `server/monitor/alert_classes.go`, so that
`TestEveryAlertClassIsClassified` passes. The monitor's operator gets the build
to run.

### 5.4 Tests

All deterministic, with injected clocks and fixtures. Root-cause tests fail on
today's main.

**sn**, as built (pure Go; the real-trail tests use genuine M8 trails, signed
disk records and the real commit path, with no database):
1. *Root cause:* `TestAttemptSettlementRuntimeV2RetiresIdlePriorAcrossRealWindows`.
   Window 42 scores seven providers of each of two operators. In window 43
   operator 9 measures six of them and one new provider, and operator 11 measures
   nobody. Then:
   - the idle provider leaves operator 9's signed post-fold and engine;
   - the sparse priors carry unchanged;
   - operator 11 keeps every prior;
   - the commit postimage, the journal, an independent replay, the lineage
     verifier and a restart all agree.

   On main the idle provider stays.
2. `TestAttemptSettlementV2RetiresIdlePriorsAfterMeasuredWindow`: the producer
   retires idle priors and keeps a sparse one; a re-signed keep-all post-fold
   verifies; a partial one is refused.
3. `TestFoldRetiresPriorAfterIdleSettlementWindow`: the engine fold. An idle prior
   leaves with both EMAs, a sparse one carries, a scored one folds, a new one
   enters, and the legacy fold keeps all.
4. `TestFoldWithEmptyWindowRetiresNothing`, with `Q_n > 0` after it. The existing
   `TestAttemptSettlementV2EmptySignedWindowRetainsIdleAndZeroPriors` and
   `TestAttemptSettlementRuntimeV2EmptySuccessorPreservesRealPrior` pin the same
   at the closure and runtime levels.
5. `TestPostFoldVerifierAcceptsKeepAllOrR1Only`: the verifier and the engine's
   fold matcher accept exactly the R1 or keep-all fold.
6. `TestValidatorCensusWithinRollingCount`: a fixed-seed simulation over 40
   windows with assignments labelled one epoch early or late near boundaries.
   Every census member was assigned within the server's last three epochs, and
   the census never exceeds two windows.
7. `TestRetiredPriorLeavesProviderIDs`.
8. `TestHeadSummaryCarriesCensusCount`: each operator's envelope carries its own
   census and coverage.
9. `protocol/headsummary`: `TestCensusSummarySignsCensusAndCoverage`,
   `TestCensusSummaryAbsentFromOlderEnvelopes`, `TestCensusSummaryRejectsInvalidCensus`.

**server**, pure:
1. `TestRollingCensusExpiryHorizon`: expiry at `v* ≥ last + 3` and not before;
   empty epochs skipped; legacy rows only after the first R1 fold.
2. `TestRankedRepresentatives`: one per network, rank-stable 3 of 4, prefix
   dedup, probation, counted members first.
3. `TestHeadroomForUpcomingFleets`.

**server**, against the test environment's Redis and Postgres (run when the
local launcher is back):
1. *Root cause:* `TestFullLegacyCensusAdmitsRankedAfterValidatorRetires`. On main,
   an all-idle 2,001 census refuses a top-200 network's provider forever.
2. `TestAdmissionInterlockStopsAtValidatorCensus`.
3. `TestExpiryRaceKeepsRedrawnMember`.
4. `TestRedisLossRestoresRollingCensusConservatively`.
5. `TestOldScriptDuringRoll`.
6. The existing cohort and rotation tests, updated.

**Also:** the server merge set (`./grafana/` and the alert-class test), and web
`ur.xyz/test.sh` for the notice.

### 5.5 Rollback

- **Server:** the taskworker first, which stops expiry and tier B, then apis and
  alt.
  - The old code treats the census as monotone again from its current members,
    which hold the validator's census, so it is safe.
- **Validator:** to the prepared target (R1's producer reverted, its verifier
  kept), never to the pre-release build.
  - Roll the server back first, or rely on the interlock, because a keep-all
    validator no longer sheds what the server expires.
- **Retired EMAs are not restored.** They weigh nothing in today's row (§2.2).

## 6. Measurable outcomes and timeline

**What happens at the first R1 fold:**
- Within the first tempo after the server reads the next epoch's summary
  (about 40 min after the boundary), every eligible unclaimed network ranked in
  the top 400 gets its representative drawn.
- Priority draws come first, and at about 3,700 assignments a tempo the roughly
  410 tier-A and tier-B members are drawn within minutes.
- The next board fold marks them measurable.
- The 171 `not_in_cohort` networks of 18:33Z all had an eligible provider. The 5
  `shared_egress` ones can't be measured until they have an address of their
  own (§7).

**Targets:**
- at least 95% of the top 200 networks with an eligible provider measurable by
  the second fold of each epoch;
- the rolling set within `L`;
- the validator census reported under 2,000, never over 3,584.

**Dates** (boundaries read from the chain head on 2026-11-17: epoch 7 at block
9,549,628, ~11-20 18:28Z; epoch 8 at 9,600,028, ~11-27 18:28Z; epoch 9 ~12-04
18:28Z):

| Path | Validator deployed by | First R1 fold | Top 200 measurable |
| --- | --- | --- | --- |
| T1: minimal release (the running build + R1 + the summary field) | 11-20 ~06:00Z | 11-20 ~18:28Z | ~11-20 20:30Z |
| T2: the next main-based release (with `361a7004`) | 11-27 ~06:00Z | 11-27 ~18:28Z | ~11-27 20:30Z |
| T3: later | after 11-27 | 12-04 ~18:28Z | ~12-04 20:30Z |

The server change must be merged and rolled before the release in each path.
T1 leaves about two and a half days for both changes, their review and an extra
validator restart.

## 7. UID 210, UR's own fleet: correct by design

UID 210's one bound device connects through a NAT address that other providers
of UR's own also use. Verify eligibility needs a live address family that
attributes to exactly one client (`VALIDATOR.md` §8.2;
`server/model/verify_model.go:711`), so no provider behind that address is
eligible. The board marks the network `shared_egress`.

This is the rule working: the validator can't attribute a hop from a shared
address to one device.

- **Consequences.** From epoch 7 the device enters the claimed priority set but
  is never drawn, because draws keep only eligible members. It earns no head
  score, and as a bound device no pool share.
- **Fix.** Give the device an address of its own. IPv6 on the device is enough,
  since one family that attributes suffices, or a unique IPv4.

The details are in the private note.

## 8. The seven fleets bound for epoch 7

UIDs 80, 167, 44, 98, 155, 31 and 247.

**On chain:**
- seven hotkeys under seven distinct coldkeys;
- registered at blocks 9,524,529-9,525,340 (06:48-09:30Z on 2026-11-17), mostly
  about 126 blocks (25 minutes) apart;
- each bound one client for epochs 7-18, at blocks 9,524,878-9,525,370
  (07:58-09:36Z).

**From the operator's records:**
- each fleet's network is new, with one device;
- they rank 136-466 on the board with one prefix each, and four of them share
  their prefix with three other units;
- none is in the cohort, and all are verify-eligible.

**What happens to them.** Under today's rules each enters the claimed priority set
when its binding goes live at epoch 7 and is admitted past 2,000 through the
claimed path, taking a permanent census place. Under this design they are tier A
the same way, and their places return once idle.

The full data is in the private note. This design doesn't treat them differently,
and the owner decides what it means.

## 9. Owner decisions

1. **Option.**
   - (a) A now, B2 staying Phase B. *Recommended.*
   - (b) B1.
   - (c) B2 now.
2. **Validator rule.**
   - (a) R1: retire after one fully idle window, with the empty-window guard.
     *Recommended.*
   - (b) Retire after two idle windows. Needs a per-provider idle count in the
     transition schema, and holds about one more window of census.
3. **Rolling limit `L`.**
   - (a) 2,000, today's value: the worst case stays at 4,000. *Recommended.*
   - (b) 3,072, today's claimed limit. Needs the §2.5 edge closed first.
4. **Admission tiers.**
   - (a) A, B (top 400), carried, sample. *Recommended.*
   - (b) Tier B limited to the top 200, though the line is a near-tie.
   - (c) One per network for every ranked network.
5. **Tier B anti-sybil.**
   - (a) Prefix dedup, plus rank-stable 3 of 4 folds. *Recommended.*
   - (b) (a) plus a network age of one epoch. This refuses new miners.
   - (c) None.
6. **Timing.**
   - (a) T2: R1 rides the next main-based release, first fold 11-27. *Recommended.*
   - (b) T1: a minimal release before 11-20.
   - (c) T3: later.
7. **Census interlock** through the head summary.
   - (a) Include it. *Recommended.*
   - (b) Rely on the deploy order and monitoring alone.
8. **Public notice.**
   - (a) Update the binding-open notice, the /mainnet timeline and the ur.io guide
     line before the first R1 fold, with no signed amendment. *Recommended.*
   - (b) Treat it as an amendment.
9. **UID 210.**
   - (a) Give UR's fleet device an address of its own. *Recommended if UR's fleet
     should be measured.*
   - (b) Leave it unmeasured.
   - (c) Unbind it.
10. **The seven fleets.** The owner's call. The design needs nothing.
11. **Board tie at the line (an observation, not part of this design).** Compare
    projected scores at a fixed precision, so that one-prefix ties fall to network
    age and not to float noise.
12. **Claimed per-tempo coverage.**
    - (a) Add the metric now, and tier-A-first draws per tempo if it falls under
      95%. *Recommended.*
    - (b) Build the draws now.
