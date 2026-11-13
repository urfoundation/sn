# Final meta review of the version-2 decision

Reviewed 2026-10-09 against [VERSION2-REVIEW-RESPONSE.md](VERSION2-REVIEW-RESPONSE.md)
(SHA-256 `15d9d4359f0acda460d64d99d19801d333347357d1037a3c206b5eea7d86a87c`),
with [VERSION2-REVIEW.md](VERSION2-REVIEW.md) and [VERSION2.md](VERSION2.md)
read in full. References `R:lines` below identify the response, including its
superseding Part 2. Local source references are at `31dd6fd5`; the inspected
implementation paths are unchanged from the documents' `e1500f15` pin.
`server/` references were read at `f08a0223` from the local server repository.

**Recommendation: retain the architectural decision, but do not yet approve
the document as an implementation-complete or activation-ready protocol.**
Usage-based allocation, cumulative amount claims, a separate settlement
coordinator and continued Yuma operation are the right first-release choices.
The response substantially improves the original proposal. Its remaining
problems concern the conditions under which money becomes someone's durable
entitlement, who can stop that transition, and how unpaid people recover it.
Those conditions require a short, explicit specification revision before the
phase A contracts and acceptance tests can be frozen.

**Does this design pay all providers? It removes the measurement-based
exclusion, but it does not establish universal payment.** An eligible provider
can still be omitted from the earning evidence; a missed epoch can transfer
its allocation to later earners; a bad cumulative successor can consume old
backing; and an honest, correctly credited balance can remain below the
transfer minimum forever. The design also deliberately leaves the stopgap's
unpaid epochs uncompensated. These are different outcomes and should not all
be described as eventual payment.

**What the final decision gets right**

The separation of usage, quality and settlement is substantive. A provider's
chance of appearing in a quality cohort no longer determines its allocation.
Validators can disagree about operator quality while reproducing the same
allocation from the same usage inputs. Keeping the stopgap measurement path
during phase A is reasonable for the current single-pool deployment, provided
its statistics have no remaining eligibility or allocation effect
(`R:475-479,711-717,1250-1269`). Reintroducing attested per-provider reliability
would reopen the central failure in the original proposal; it should remain
outside phase A.

The new evidence companion and `settlementAt` are appropriate answers to the
immutable v1 domain and pool-identity constraints. A sidecar settlement
coordinator avoids adding functions to the nearly full v1 implementation.
This resolves the architecture choice, while deployment size, authority and
historical routing still require qualification. Avoiding a coordinator upgrade
does not itself mean that the three new contracts have passed the bytecode
gate (`R:325-344,509-516,1215-1228`).

Coldkey aggregation before largest-remainder allocation, credit-time
watermarks, explicit funding conservation and a specified complete-tree
layout are useful concrete corrections. The Merkle description now identifies
the ordering, proof index and multiproof behavior needed for implementation.
These are improvements to the design, rather than reasons to reconsider the
basic amount-ledger approach (`R:886-899,951-1028`).

Null is correctly separated. Its expanded head, payload limits, dividend
accounting and constrained return path do not need to delay pool eligibility.
The no-retroactive-compensation decision and acceptance of quantified v1
residue are economic decisions, not technical solutions to the original
no-stranding requirement. The response now identifies them as such; the
payment promise must do the same (`R:1179-1211,1241-1248,1348-1354`).

| Prior review area | Meta assessment of the response |
| --- | --- |
| F1 and the common payout input | Main allocation flaw resolved by removing the measured/attested factor. Complete usage admission remains necessary. |
| F2 and challenge effect | Correct off-chain invariants and credit accounting are specified. Root admission and the claimed loss bound remain unsafe or underspecified; see A1-A2. |
| F3 and F7 | Correct companion/sidecar direction. Journal publication is not settlement authority, and the sweep needs a routing exception; see A1 and A5. |
| F4 | Relevant evidence is named and budgeted, but completeness, chunk-local verification and the audit schedule are not yet a complete protocol; see A3-A4. |
| F5-F6 and head measurement | Better population weighting and epoch-wide churn bounds. Remaining estimator and aggregate-bound issues belong to phase B. |
| F8 | Conservation buckets and indefinite v1 withdrawal support are improvements. The actual switch and sweep procedure still needs repair. |
| F9 | Appropriately moved off the critical path. The immutable vault's proposed surplus function still needs a decision before deployment. |
| F10-F11 | Tree construction and integer allocation are substantially clarified. Resource claims and the universal-positive-payment wording still need narrowing. |

**Which providers receive value, and which remain unpaid**

The payment lifecycle is: authenticated earning evidence, eligibility,
integer allocation, finalized entitlement, claim credit, then a successful
runtime transfer. Passing an earlier stage does not establish the later ones.
The following is the actual scope of the proposed guarantee, even assuming
honest operation except where a failure is stated.

| Provider or coldkey | Outcome under the written design |
| --- | --- |
| Unmeasured provider with admitted positive usage, an effective payout wallet and no active head binding | Enters the same usage allocation as a measured provider after phase A. This is the central improvement. Merely renaming the exclusion reason in phase 0 does not confer eligibility. |
| Provider whose work, identity or wallet evidence is omitted | Can receive zero without a provider-row audit ever selecting the missing item. The independent completeness obligation in A3 is essential. |
| Provider joining after the quality frame was captured | Intended to earn immediately from usage, with a late-join flag. Phase A must support this without requiring phase B's frame, and later verification must authenticate the exception (`R:729-730,931-934`). |
| Provider without an effective payout wallet, with no admitted earning work, or outside the earning window | Excluded. The design specifies no later catch-up rule for usage excluded because wallet authority was missing. Work that is still open is not yet closed-work earnings. |
| Provider with an active head binding | Excluded from the pool; native rewards go to its fleet hotkey according to head eligibility and weight. This is not a promise of a separate transfer to every member's coldkey. |
| Very small positive usage share | May receive zero rao repeatedly under deterministic rounding. Cumulative storage accumulates integer deltas, not the discarded fractional entitlement. |
| Provider that earns only in a missed or vetoed v2 epoch | Its share is not preserved by aggregate funding carry as written; the next epoch's earners can receive all of it. See A2. |
| Dormant provider, changed wallet, or provider that later enters the head | Its old coldkey should keep its finalized cumulative balance. That requires admitted continuity and accessible current proofs; a four-root ring alone does not establish either. |
| Coldkey with claim credit below the runtime minimum that never earns again | May never receive a runtime transfer, even if someone pays the transaction gas. No top-up or terminal small-balance payout mechanism is specified. |
| Coldkey whose balance clears the transfer minimum but not its economic claim threshold | Can claim at a high cost if someone supplies gas, or wait. No bounded waiting time or guaranteed relayer subsidy is offered. |
| Provider excluded during stopgap epochs before `E_s` | Receives no retroactive compensation under the recorded owner decision. v2 cumulative accounting begins at its own start. |
| v1 coldkey left with sweep residue or separate sub-floor credit | Remains in the expressly accepted residual population unless separately funded; v2 credit cannot combine with it. |

For rounding, `F = 10` rao and coldkey weights `1, 1000` give `0, 10` under
the revised largest-remainder rule. Repeating that epoch never pays the first
coldkey. This is compatible with the narrow guarantee in `R:896-897`, but not
an unqualified reading of requirement 5 at `R:630-631`. The assertion that the
one-rao threshold is below one byte of a petabyte is also arithmetically wrong:
at `2.07 × 10^12` rao of funding, one decimal petabyte of total usage gives a
threshold of about **483 bytes**, not less than one byte (`R:467-470`). The
10,000-bps ceiling is removed; integer starvation is not.

The payment-latency concession is material. Using the documents' conditional
13.6-TAO weekly pool and 0.00125-TAO claim cost, equal average entitlements take
about **92 weeks at 100,000 providers** or **919 weeks at one million** for gas
to be 10% of a payment. Allocating 30% to the head increases those pool figures
by `1/0.7`, before any further long-tail skew. These are arithmetic scenarios,
not current-chain forecasts or a per-provider bound. A provider that stops
earning does not necessarily approach the threshold at all. Define the
recommended threshold using the complete runtime transfer and relayer cost;
`R:903-906` currently names only the EVM part, while `R:1030-1036` says the
runtime part remains unmeasured.

If the intended product promise is correct non-expiring accrual for eligible
coldkeys, the architecture can support it after the following repairs. If it
is that every contributing provider actually receives value within a finite
time, a funded treatment of terminal small balances, gas and any rounding
shortfall is a missing product decision. Relayer permits and longer waiting
alone do not supply that guarantee.

**A1 — Root admission must establish both auditor authority and completion.**

The proposed `finalizeDistribution` accepts a pending root whenever it is not
challenged and `vetoCount == 0`; it does not require any successful audit
(`R:1061-1065`). An unavailable validator, withheld ledger, failed audit
transaction or incomplete audit can therefore look identical to a clean
epoch. The current threat model does not specify the timely, continuously
available watcher assumption needed to make this optimistic rule safe.

The four-root ring does not repair that gap. Suppose the latest root owes
Alice 100, none credited yet, and a new epoch captures 10. A successor omits
Alice and contains Mallory 110 plus a zero-valued leaf to satisfy leaf-count
monotonicity. The total is correctly funded. With no timely audit/veto, Mallory
can credit 110 first; Alice's retained proof then fails the pool credit cap.
There is no two-membership-proof decrease challenge for an omitted Alice.
Thus old backing, not only the latest epoch's funding, is exposed. The claim
that a bad successor cannot erase a finalized allocation because four roots
remain is false (`R:532-536`), and the one-epoch loss bound in `R:1111-1113`
needs this qualification. Repeated undetected usage fraud is not limited to
one epoch either.

There is also a concrete authority gap. `R:1080-1084` counts vetoes from hotkeys
with published v2 activations using the existing dual-signature design. The
existing journal explicitly authenticates consent, **not historical validator
eligibility** (`evm/src/STValidatorEvidence.sol:9-13,88-119`). Its activation
checks domain, policy, an active operator and possession of the two supplied
keys; it does not establish a permitted validator or owner-approved committee.
Copying that admission rule would let arbitrary key owners acquire veto power.
Multiple activations must also not count as multiple votes from one authority.

For the stated single-validator launch, the simplest repair is an explicit
owner-admitted audit key or committee snapshot and a required affirmative
audit of the exact pending root, ledger hash, predecessor and funding snapshot.
No completed audit means no new authoritative root. Keep permissionless
decrease proofs as an additional safeguard. Define the committee's effective
epoch, unique-vote rule, audit deadline, missing-data outcome and recovery from
a false veto. A false veto can recur every epoch, so it is not intrinsically a
one-epoch delay (`R:667,1336-1337`).

An optimistic alternative is possible, but then the design must explicitly
accept watcher availability, define failure handling and price the full
outstanding-balance exposure. It cannot describe the mere passage of the
pending window as successful verification. The existing owner-upgradeable
coordinator also remains a trusted part of admission; immutable custody does
not make that authority irrelevant.

**A2 — Preserve earned allocations across misses and freeze each funding transition.**

The formula in `R:888-895` allocates all `epoch_funding_rao` using the current
earning window. `R:933-934,1099-1105` makes that funding include earlier missed
epochs and says providers lose time, not value. Consider Alice doing all work
in epoch e with funding 100, followed by a veto. Alice then leaves; Bob does
all work in e+1 with funding 100. The written next-epoch formula allocates 200
to Bob and zero to Alice. Conservation holds while Alice loses her earnings.
The explicit decision not to compensate pre-v2 stopgap epochs does not resolve
this separate v2 recovery case.

Specify how an unaudited or missing epoch's earning obligations survive:
for example, retain and later admit separate epoch usage/allocation components
before adding their deltas to the cumulative ledger. If evidence is missing,
define how it is recovered and what remains pending. If the chosen policy
instead redistributes unallocated funding to future earners, say so explicitly
and remove the individual no-loss guarantee. A timely root commitment and an
accepted entitlement must have distinct meanings.

Funding also needs an epoch-specific snapshot and a recovery state machine.
`captureEmission` is described as v1 plus additions to one pool-wide
`unfinalizedFunding` counter, while finalization compares the committed total
with that counter's live value (`R:994-1004`). If e's finalization is delayed
until after e+1 captures, a previously correct pending total no longer equals
the required sum. Captures are not specified to wait for `nextEpoch`, and the
old commitment is write-once. Define which captured epochs each root consumes,
the frozen cutoff, and how a stale or invalid committed root becomes missed
without blocking every later epoch. Exercise repeated misses, delayed
finalization, zero funding, zero usage, and recovery after weight has been
zeroed. A new clean epoch must be possible while the penalty is in force.

**A3 — Admission must cover omitted providers and complete evidence dependencies.**

The response correctly rejects treating a SQL projection as authenticated work.
But a header containing chunk hashes plus checks on selected provider rows is
not yet a completeness protocol. A provider omitted from both the rows and
the operator's evidence index has no chunk to select. A false `eligible` or
`missing_wallet` flag can likewise change who gets paid. Recomputing allocation
from those same rows only reproduces the operator's selected population
(`R:839-847,923-945`).

The existing authority path has stronger obligations that must survive the
rewrite: an independently selected authority signer distinct from the payout
publisher, an expected-provider roster, every required SDK owner's boundary
cuts, reservation/report joins and complete service attribution
(`payoutartifact/whole_work_inventory.go:51-91,189-230,392-419`;
`payoutartifact/whole_work_authority.go:46-75`). The ordinary HTTP artifact
reader cited in the response tolerates unavailable optional report evidence
and calls the report verifier with a zero expected root signer; it is not by
itself a full payment-authority gate (`validator/artifact.go:300-305`;
`payoutartifact/closed_work_reports.go:81-111`). “Exactly as the v1 reader”
therefore needs a precise list of mandatory authority checks and refused
incomplete outcomes.

Define how the independently authenticated population and earning inventory
are reconciled against every inclusion and exclusion, which checks are full
and which are sampled, and how a missing provider or receipt is challenged
before allocation is finalized. Sampling receipt correctness is an explicit
security tradeoff; sampling only an operator-selected population cannot prove
that every entitled person was included.

Chunk boundaries also need a real dependency graph. One original contract can
contain several provider contributions (`payoutartifact/closed_work_inventory.go`
and `payoutartifact/whole_work_inventory.go:396-420`), and one provider can have
more than 8 MiB of original work. An 8-MiB provider range therefore need not
have one 8-MiB evidence partner. Specify authenticated one-to-many references,
cross-chunk uniqueness and completeness, wallet and SDK authority chunks, and
the byte cost of the entire dependency closure of an audited unit. Name how
the existing authority/owner limits are replaced as well as the receipt caps.
These are necessary parts of the million-provider fixture, not merely larger
JSON download limits.

**A4 — The audit budget, coverage promise and manifest bounds must agree.**

The defaults permit 16 GiB of evidence but fetch only 512 MiB per epoch,
while requiring every chunk to be selected at least once every four epochs
(`R:879-882,936-945`). At 8 MiB per chunk that is 2,048 chunks and capacity for
64 chunks per audit epoch. Even one fixed snapshot needs 32 such budgets for
complete coverage. Four budgets cover at most 2 GiB. New evidence arriving
each epoch makes a rolling guarantee more demanding, and auditing old work
after finalization cannot undo its payment. This is a contradictory default,
not a parameter that an implementation benchmark alone resolves.

Specify the actual sampling unit, selection without or with replacement,
inclusion probabilities, treatment of zero/understated usage, deterministic
coverage obligations and behavior when their dependency bytes exceed budget.
Then state a detection target for economically significant fraud within the
pending window. A bound on bytes alone does not state that target. At launch
there is no active bond to offset residual fraud risk, and lost pool weight
primarily withholds future provider funding; it is not automatically a loss of
the operator's own capital (`R:659,1099-1114,1271-1274`).

The audit draw shown in §7 also has no validator-specific input:
`PRF(b_e, e, no_id, j)`. Validators following that expression and the same
budget select the same chunks. Adding validators does not create the
independent samples implied in `R:1332-1335`. Either define distinct,
verifiable per-validator draws with keys fixed before the beacon, or honestly
specify a shared sample and its detection probability. This is separate from
the corrected rule that quality-cohort membership has no payout effect.

Finally, a 64-KiB JSON header cannot contain every allowed chunk index
(`R:851,864-867`). The 2,048 SHA-256 values for 16 GiB of evidence alone need
131,072 hex characters, before JSON, ranges, totals or any other chunk class.
Use an authenticated paged manifest or revise the admitted bounds together.
Require maximum-size manifest and dependency tests, rather than testing only
the 70-MiB provider and 53-MiB leaf files.

**A5 — Correct the switch chronology and make the recovery sweep discoverable.**

`R:1140-1142` says `E_s` is chosen only after `E_s−1` has finalized. But
`end(E_s−1) = start(E_s)`, and finalization is at least 14,400 blocks later.
The v1 coordinator requires a policy successor to be scheduled before its
effective epoch (`evm/src/STCoordinator.sol:302-325,635-645`). The response also
requires advance pool registration and epoch-2 scheduling for an epoch-3
switch (`R:1147-1152,1258-1263`). These conditions cannot all be true. Choose
and authorize the future boundary after qualification, then allow the last v1
epoch to finish through its normal post-boundary windows. Specify what happens
if that close, commitment or finalization fails; do not make the future switch
depend on a completion that necessarily occurs after it.

The carry sweep introduces another direct contradiction. It is a **v1** root
for `E_sweep > E_s`, while `settlementAt(noId, epoch)` returns v2 for every
such epoch (`R:1046-1048,1155-1162`). Readers following the prescribed sole
identity lookup will therefore misroute the recovery claim and its evidence.
Give the sweep an explicitly authenticated recovery identity/exception that
all claim, replay and conservation readers can discover, including any v2
distribution for the same epoch.

Also specify the actual recovery artifact. Existing v1 verification rebuilds
usage-times-reliability shares; it does not have an expired-remainder
allocation mode (`payoutartifact/artifact.go:164-172,335-351`). A raw root can
be accepted by the vault without being a valid ordinary v1 earning artifact.
The sweep must authenticate its remainder-based allocation honestly, rather
than manufacture usage or reliability rows to fit that format. Carry from
missed roots, leaf rounding and late emissions has no automatic attribution
to “expired remainders”; define those recipients and the zero-denominator case.

The runbook must call `expireEntitlement` for the actual expired roots before
the sweep: expiry does not itself move funds into carry
(`evm/src/STSettlementVault.sol:400-407`). Use recorded expiry blocks, since a
late v1 finalizer can extend expiry to the minimum claim window
(`evm/src/STCoordinator.sol:642-645`). Preserve v1 proof/claim support as well
as credit withdrawal until the sweep's own expiry. Quantify residual value by
bucket and beneficiary, including old hotkey dust and any unallocated carry.
The detailed sweep implementation can ship later, but its identity and
authority must be compatible with the contracts and readers chosen for phase A.

**A6 — Make long-lived claims independent of continued earning and operator service.**

The ledger correctly retains old coldkeys, but the public claim lifecycle still
needs explicit qualification. `R:915-917,970-971,1088-1095` names content-addressed
storage, validator retention and a claim endpoint. Specify how a user obtains
a current proof after the operator disappears, after more than four roots, or
after their provider account or wallet mapping changes. Name the retained
complete claim dataset, its public retrieval path and the retention obligation
for the operator's last root. A commitment proves integrity, not availability.
An independently retained current leaf set can provide this exit without
storing every historical root on chain.

Do not simply reuse current contributor-based API admission. The existing
server selects a proof through a provider's original eligible contribution;
its ordinary path rejects missing client identity, and its compatibility
coldkey lookup is narrowly restricted (`server/controller/sn_claim_owner.go:16-65`).
The new path must also return old cumulative balances when the current delta
is zero or the provider is now head-excluded. A public proof for a coldkey need
not authorize a redirected payment, because the vault already fixes the
recipient.

Capacity planning must count **all historical coldkeys**, not only this epoch's
active providers. The promise to retain dormant and changed-wallet leaves
forever makes leaf storage grow with that history (`R:133-138,894-899`). State
the signed capacity expansion or successor procedure before the 1-GiB ledger
budget is exhausted, including continuity and proof availability through that
procedure. The million-active-provider tree benchmark does not bound a
multi-year cumulative ledger.

**Other decisions to settle at the correct phase**

- **Phase A must be a complete protocol on its own.** It explicitly defers
  sampling frames to phase B, but mandates §7's full checks, including frame
  consistency, and a ledger header with frame commitments
  (`R:863,931-934,1250-1257`). Define the phase A schema/policy profile and its
  complete population authority; activate frame-dependent checks only with B.
  Test A with no frame or VRF artifacts and with unmeasured and late-joining
  earners. A new signature alone is not a wire-format admission implementation.
- **Keep permit authorization explicit.** §11 binds recipient, relayer, fee,
  deadline and nonce, but still does not bind a cumulative ceiling, authorized
  increment or exact root (`R:1118-1135`). Either add the intended amount bound
  or expressly define this as a one-use authorization for any available
  cumulative amount before the deadline. Specify cancellation and stable root
  identification, and test a later root arriving between signing and use.
- **Resolve the immutable surplus function before deploying it.** The proposed
  attribution denominator excludes accepted unpaid credits
  (`R:1019-1027`). A pool with `finalizedTotal = credited = 100`,
  `claimCredit = 100`, and zero unfinalized funding still backs 100 of unpaid
  value in escrow but gets weight zero; if every pool is in that state, the
  denominator is zero. Define credit attribution, zero-liability behavior and
  the corresponding captured/accounted counter updates. Null activation can
  wait, but a public function in an immutable phase A vault cannot be repaired
  by upgrading the sidecar later. Qualify it now or explicitly plan a different
  later custody design.
- **Phase B's estimator is only partly finished.** Population stratum weights
  fix the original square-root-allocation bias for fully observed scores.
  However, replacing only those weights with usage shares does not produce a
  usage-weighted estimator within each stratum: one stratum with qualities
  `0,1` and usage `1,99` still yields `0.5` instead of `0.99`. Selective
  nonresponse is not corrected by multiplying a prior variance by four, and
  first-epoch/tiny strata need a rule when there is no previous estimate
  (`R:786-803`). State the assumptions under which the interval applies;
  operator favoritism makes the measurement potentially optimistic, not a
  mathematically certified upper bound on population quality.
- **Phase B's total state and head budgets need a composed test.** A full
  pool census of 4,096 plus 200 fleets × 8 members exceeds the existing shared
  raw-measurement bound unless the head is separated through the actual stats,
  replay and transition paths (`validator/attempt_settlement_v2_runtime.go:223-256`).
  Changing `CurrentBindingKVs` alone is insufficient. Under the Null defaults,
  2,000 × 8 × 16 head assignments require 256,000 assignments, above the stated
  10% budget of 211,680 (`R:767-832`). Qualify the combined limits, not each
  component independently. Preserve epoch-effective binding and live UID
  semantics when replacing `bindingAt` with an event index; v1 requires new
  bindings to start in a future epoch and checks current UID identity
  (`evm/src/STCoordinator.sol:730-740,812-828`).

**Readiness conditions and the preferred fallback**

I would keep phase A first and avoid adding phase B or Null work to its critical
path. Before choosing `E_s`, amend the existing gates with deterministic cases
that establish the payment outcomes above:

1. An unauthorized activation cannot vote; duplicate activations cannot add
   votes; no successful audit, unavailable data and an omitted prior coldkey
   cannot authorize a successor. Include a malicious claim arriving before
   the old claimant and repeated false-veto recovery.
2. Alice earns only in a missed/vetoed epoch and is later inactive; her
   specified entitlement survives recovery. Include delayed finalization
   after the next capture, repeated misses, zero funding and zero usage.
3. Remove an entire provider, receipt dependency or wallet authority; the
   admission result follows an independently authenticated population rather
   than the publisher's remaining rows. Include shared contracts, more than
   one evidence chunk per provider, maximum manifests and an audit dependency
   closure exceeding budget.
4. Run the actual phase A profile without phase B inputs. Rehearse the
   corrected switch chronology, late v1 emission, explicit expiry calls, and
   the v1 sweep through the same public readers users will use.
5. Pay an old coldkey after wallet change, head promotion, long inactivity and
   operator endpoint failure. Demonstrate the intended outcome for a terminal
   below-floor balance and repeated sub-rao earnings; do not count a stored
   zero or indefinitely deferred credit as a completed payment.

The response's existing Foundry, fork, exact-artifact, runtime, approval and
million-provider gates remain necessary. Attach end-to-end results for source
authority, evidence acquisition, allocation, historical continuity, signatures,
storage and claim service. The cited `v2measure` figures measure tree-related
work; they do not establish that the whole verifier, including up to 512 MiB
of audited evidence and its dependencies, runs in the same seconds or heap
budget (`R:432-446,612-618,966-974`). Treat the epoch-3 target as conditional,
as the response already says, rather than relaxing these gates to meet it.

If cumulative admission cannot be qualified promptly, non-expiring per-epoch
amount roots with a multi-epoch credit/transfer remain the relevant fallback.
They avoid allowing a later root to rewrite earlier allocations, at the cost
of growing proofs and claim bookkeeping. They still need correct usage,
funding, availability and an honest small-payment policy. For the current
deployment, a small explicit audit committee with affirmative root admission
is the preferable first attempt to retain the cumulative design; neither
validity proofs nor another settlement domain is required merely to close the
identified gaps.

The remaining release decision is therefore precise: approve usage-based
cumulative settlement on Yuma as the direction; require A1-A6 and the phase A
profile to be resolved and tested before activation; and publish an accurate
payment promise. “All eligible usage enters allocation regardless of probing”
is attainable. “Every provider gets paid” requires additional decisions about
omissions, missed earning epochs, fractional amounts, terminal small balances
and who funds practical delivery.

**Verification limits**

This review read the three complete documents and the cited local contract,
allocation, evidence, authority, capacity and claim-reader paths. It checked
the rounding, missed-epoch allocation, shared-backing counterexample, audit
coverage, header size, threshold and head-budget arithmetic with a small
offline calculation. Those examples exercise the written rules; they are not
tests of a v2 implementation. I did not reproduce the scratch Foundry or Merkle
benchmarks, run an implementation test suite, inspect a live chain, or
independently certify the stated runtime-475/477 deployments and Null behavior.
Only this meta-review file was added; no implementation, policy, input document
or deployment was changed.
