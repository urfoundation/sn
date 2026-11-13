# Peer review of VERSION2.md

Reviewed 2026-10-08–09 using GPT-6 Astra at max reasoning, as requested. This review
covers the 949-line proposal with SHA-256
`ec0639d94b2439dd48ce51f2b525b760fc23bbf23e19f2aac0163b7800981615`, against SN
commit `e1500f150584a3b39ba9753b20f7e2eeb50f1147`. Server references below were
read with `git show 5342fd76:<path>`; the current server checkout is newer.
References use `path:lines`, with `server/` identifying that pinned server source.

**Recommendation: retain the direction, revise the protocol before implementation.**
Chunked evidence, amount-denominated allocations, cumulative claims and a new
immutable vault address real limits in the launched system. The proposal does
not yet establish its promises of unchanged fraud resistance, non-expiring
individual entitlements, bounded sampling state or a safe migration. Several
gaps are concrete incompatibilities with the cited implementation, rather than
parameters that simulation can settle.

My preferred first release keeps Yuma, uses bounded sampling to determine
operator quality, pays all eligible providers from a common, independently
verifiable usage ledger, and introduces cumulative claims with explicit
continuity and custody invariants. Per-provider attestation scoring and Null
expansion should follow their own qualification. Alternatives and their
tradeoffs appear after the findings.

The strongest parts of the proposal are its separation of measurement,
allocation and settlement bottlenecks; recognition that raising one census cap
cannot solve them all; identification of the 10,000-bps allocation ceiling; and
acknowledgment that batching cannot eliminate a runtime transfer per recipient.
The existing allocator, artifact reader and vault support those diagnoses
(`protocol/payout.go:98-126`, `validator/artifact.go:270-287`,
`evm/src/STSettlementVault.sol:365-388,488-510`). Preserving immutable custody
while deploying a successor is also the appropriate starting point.

The findings use **P1** for issues to resolve before relying on the design or
activating it, and **P2** for material specification errors to resolve before
implementation. Proposed acceptance cases are recommendations for future
deterministic regression tests; this review changes no implementation.

**F1 — P1: the operator learns the cohort before making the attestations being audited.**

`VERSION2.md:515-525` says the VRF hides the cohort from the server until the
epoch ends. The same protocol asks that server for particular indexed members
and then routes assignments only to them. The server knows its own index-to-ID
mapping, so the requests disclose the cohort immediately. Keeping the VRF
output secret does not conceal the requested recipients. At epoch end the
operator can report accurate reliability for every observed member and inflate
every other provider; the proposed `delta_n` can be zero. This directly defeats
the justification at `VERSION2.md:572-578`. An operator can also preferentially
serve the known cohort throughout the epoch.

Specify the temporal commitment protocol: which usage, reliability and signal
records become immutable, where their commitments are anchored, and whether
that happens before the relevant audit selection is observable. Separate
online quality probes from retrospective audits of already committed evidence.
A public random value revealed after an input commitment can select a
retrospective audit; it does not make an announced online cohort hidden.

The verifiability transcript also needs a VRF proof, fixed key and input
selection rules, and index-authenticating inclusion proofs. Revealing a seed
only proves consistency with that seed. An ordinary commutative Merkle
membership proof proves membership, not that the member occupied the requested
index. Commit `(stratum, index, client_id)` or specify an ordered indexed tree.
Anchor one eligible-set commitment before selection and define equivocation,
replacement and nonresponse handling.

Acceptance cases: an operator observes all cohort requests and fabricates only
the complementary set; a server returns another eligible member with a valid
membership proof; a validator substitutes an arbitrary seed; and a server
changes its roster after seeing the requests. Each must be detected or must
have its remaining trust assumption stated explicitly.

**F2 — P1: cumulative-total checks do not protect a provider's previous entitlement.**

The new vault retains only `latest`, and the proposed checks are monotonicity
of the aggregate total plus aggregate backing (`VERSION2.md:626-629,640-668`).
The streaming verifier is not specified to compare each new cumulative amount
with the previous authenticated ledger.

A concrete counterexample is an old root `{Alice: 100}` and a successor
`{Alice: 0, Mallory: 100}`, with 100 captured and neither account previously
paid. The total remains 100 and all stated aggregate checks pass. Alice's old
proof no longer verifies; Mallory can consume the backing. The same loss occurs
without malice if a builder drops an inactive provider or resets a cumulative
balance after a wallet change. An audit finding after root replacement does not
restore a claim. Unlike v1's independently retained epoch roots, each v2 update
can affect every previous unpaid epoch.

Specify an authenticated transition from the previous ledger, including:

- For every coldkey, `new_cumulative = old_cumulative + allocated_delta`;
  dormant, promoted and disconnected providers retain their prior balance.
- Unique coldkeys, equality between the committed total and the sum of leaves,
  strictly ordered epoch/root succession, and rules for missing epochs,
  zero-usage epochs, wallet changes and operator retirement.
- A funding calculation that subtracts previously allocated value, rather than
  allocating the gross captured amount again. The `allocated` comment at line
  644 cannot mean summing successive cumulative totals: totals 100 then 150
  represent 150 allocated, not 250.
- Separate counters for entitlement accepted into claim credit and actual
  runtime payments. The cumulative claim watermark must advance when credit is
  created, even if transfer is deferred, or the same entitlement can be credited
  repeatedly. Preserve `captured = paid_out + escrow_accounted` and an explicit
  decomposition of pending funds, unclaimed entitlements and claim credits.

Define what prevents an invalid successor becoming authoritative: verified
admission, an enforceable dispute process or a validity proof. Retaining older
claimable checkpoints also improves exit liveness, but alone does not stop a
bad new root consuming their shared backing. Test the example above, omission
of an inactive account, a repeated subminimum claim, out-of-order finalization,
and a missing epoch followed by recovery.

**F3 — P1: the existing evidence journal cannot support the specified successor unchanged.**

`VERSION2.md:588-594` adds `AUDIT_CHALLENGE`, while lines 699-701 and 891-893
say the existing immutable evidence contract remains unchanged for both vaults.
There are two hard incompatibilities:

- `STValidatorEvidence` captures `settlementVault` in its constructor and
  rejects activation records naming another vault
  (`evm/src/STValidatorEvidence.sol:25-29,55-68,97-103`). It is not anchored only
  to the coordinator proxy.
- Its compiled `ValidatorEvidence.valid` accepts only `CLOSED_CENSUS = 1` and
  `DEPOSIT_AUDIT = 2`, rejecting every other kind
  (`evm/src/lib/ValidatorEvidence.sol:13-14,87-100`). Adding an off-chain enum
  cannot add a new on-chain kind.

Choose an explicit compatibility design. One option is a separately anchored
v2 companion with its own domain and an epoch-aware evidence lookup in the
coordinator, preserving the original companion for v1. Another is retaining
the old journal's literal domain and existing kinds while defining a carefully
versioned payload that authenticates the v2 vault; that is a different protocol
from the proposed new kind and needs all consumers updated. Do not simply
replace the original pointer: the original contract's `anchored` modifier
checks `coordinator.validatorEvidence() == address(this)` on every new write
(`evm/src/STValidatorEvidence.sol:73-78`).

Acceptance requires real calls showing v1 evidence still works during overlap,
v2 activation authenticates the intended vault, and a challenge is accepted in
the intended domain while a replay in the other domain is rejected.

**F4 — P1: the payment evidence is neither fully specified nor covered by the new byte budget.**

The proposal calls `ClosedWorkCensus` receipt-backed usage and treats provider
work as nonbinding (`VERSION2.md:318-320,562-582`). Its cited implementation
explicitly distinguishes a reproducible operator database projection from
authenticated traffic or provider ownership
(`payoutartifact/closed_work.go:1-3,40-43,67-68`). Separate report, reservation,
service-party and wallet evidence supplies the stronger checks; signed report
increments alone are insufficient
(`payoutartifact/closed_work_reports.go:81-111`). The current server invokes
those additional authority paths before building payouts
(`server/controller/st_controller.go:3383-3399,3442-3447`).

Matching the sum of usage does not prove its allocation: usage can be moved
between providers without changing the total. Comparing a provider's real
customer usage with the validator's observed probe-hop volume is also not a
valid upper bound. Those are different workloads; an honest busy provider can
serve far more customer bytes than the probes observe.

There is also an omitted hard cap: `ClosedWorkCensus` accepts at most 32,768
contract records and 8 MiB of original snapshots/reports, with these limits
enforced during cloning and verification
(`payoutartifact/closed_work.go:22-25,92-113,251-261,273-302`). Chunking the
69-byte provider rows and 56-byte leaves does not remove it. For example, an
epoch with one distinct closed contract per 100,000 providers already exceeds
the record cap. Original receipts, ownership evidence and inventories are not
included in the proposed 125 MB payload estimate.

Specify a chunked evidence graph and the exact verifier joins from original
service receipts through provider identity, wallet authority, earning window,
head exclusion and allocation. Carry forward the relevant existing checks and
budget the originals, not just the derived rows. Test redistributed usage with
an unchanged total, a wrong coldkey, an honest high-usage provider with few
probe bytes, and a complete census beyond both existing limits.

**F5 — P1: the proposed estimator is biased under its own sampling design.**

The quotas use square-root stratification, but `Q_hat` remains an
exposure-weighted mean of sampled providers
(`VERSION2.md:527-553`). The existing function weights each sampled row by
exposure and has no inverse-inclusion correction
(`validator/measurement_stats.go:531-555`). Oversampling small strata therefore
changes the estimand.

For two strata with 1% and 99% of the population, qualities 0 and 1, and equal
exposure within the sample, the population mean is 0.99. Square-root allocation
gives the small stratum about 9.13% of samples, so the proposed mean is about
0.9087. The 8.13-point bias is much larger than the stated 2.2-point interval;
more samples do not remove it.

Define whether the target is provider-, usage-, assignment- or prefix-weighted
quality. For a provider mean, combine stratum estimates with population weights
`n_s/N`, with a design-appropriate variance estimate. For other targets, publish
the inclusion probabilities and use an appropriate weighted estimator. A
count of measured members is not automatically an effective sample size when
weights, shared failure domains, replacements and nonresponse matter. Treat
churn-related missingness explicitly. The attestation error estimator needs
the same treatment and a specified economic weighting.

Also calibrate the compared reliability quantities. A Wilson lower confidence
bound is systematically below the corresponding success-rate estimate; an
honest attestation of the latter can produce a positive one-sided error.
Deterministic synthetic populations should reproduce known means under
unequal strata and exercise this honest-operator case before selecting lambda
or a zero-weight threshold.

**F6 — P1: cohort quotas and churn do not establish the claimed state bound.**

The formula `m_s = max(q_min, m * sqrt(n_s) / sum(sqrt(n_j)))` does not generally
sum to `m`, does not cap a quota at `n_s`, and does not specify integer
apportionment. Hashing successive counters modulo `n_s` samples with
replacement unless duplicates are explicitly rejected. These omissions affect
both reproducibility and the claim that every cohort member receives the
stated assignment budget (`VERSION2.md:517-543`).

The replacement limit also needs a precise lifetime. If 25% may be replaced
each tempo, as lines 534-536 can be read, a 140-tempo epoch can encounter
`4096 + 139 * 1024 = 146432` distinct providers. Even a single replacement
round makes the distinct measured set larger than 4,096 if departed members'
measurements remain needed. The asserted `m * (1 + K)` bound omits them.

Specify a fixed maximum number of strata, exact distinct-member apportionment,
sampling without replacement, an epoch-wide replacement budget, and separate
bounds for active members, retained measurements, carried EMA and egress
hashes. Define what happens when the budget is exhausted. Phase 0's Redis
rotation also needs a coordinated validator transition that expires the old
quality maps: those maps are persisted and included in the current census
(`validator/stats.go:178-191`, `validator/measurement_stats_v2.go:42-73`). Server
rotation alone would recreate the overflow.

Test tiny strata, quota floors exceeding the budget, duplicate draws, sustained
churn and restart at every eviction boundary. State the bounds over all
operators and any separately measured head providers, not just one pool.

**F7 — P1: the proposed migration does not publish the new pool identity to existing readers.**

The coordinator changes list `registerOperatorPoolV2` calling the new vault and
epoch routing for settlement methods (`VERSION2.md:693-701`). They do not
specify how the operator's public identity changes. Existing `scheduleOperator`
preserves `prior.poolHotkey` (`evm/src/STCoordinator.sol:423-461`), and the
validator finds the weight destination through `operatorAt(...).PoolHotkey`
(`validator/release_decision_chain_v2.go:380-415`). Registering a new pool only
inside vault2 would leave this reader pointing at the old hotkey.

The claim endpoint is likewise not already epoch-aware about the vault. The
cited line 207 declares a response field; the implementation fills it from
the current global `cfg.SettlementVault`
(`server/controller/sn_controller.go:293-296`). Updating that configuration
alone would advertise the wrong destination for retained v1 claims.

Define one authenticated settlement-identity lookup by operator and epoch,
covering vault, pool hotkey, ABI/schema and evidence domain. Update the
validator, server, claim tooling, historical replay and signed configuration
selection to consume it while preserving old identities. Include policy v2's
effect on existing ABI fields: `_validatePolicy` still requires nonzero
`claimTTLEpochs`, separately from `_validateSettlementWindow`
(`evm/src/STCoordinator.sol:332-355`). Test the last v1 and first v2 epochs
through actual public readers, not only direct vault calls.

**F8 — P1: the rollout does not satisfy the stated no-stranding requirement.**

Requirement 8 promises no stranded v1 entitlements or carry, but migration
steps 5-6 allow dust write-off and eventual acceptance of inaccessible carry
(`VERSION2.md:489,878-890`). The final v1 epoch can include only carry already
present when it finalizes. Unclaimed v1 entitlements expiring later add more
carry (`evm/src/STSettlementVault.sol:400-407`), after the switch has directed
new epochs to v2. A finalized v1 entitlement cannot be finalized again
(`evm/src/STSettlementVault.sol:327-336`). "Finalize one last v1 root" is not a
complete exit procedure under this routing rule: it needs a specifically
reserved, timely committed and funded/deferred v1 epoch and a treatment for
that root's own later remainder.

Further, v1 does not necessarily hold only carry after the compatibility
window. An accepted claim whose transfer was deferred leaves a
`claimCredit[coldkey]` that remains withdrawable independently of epoch expiry
(`evm/src/STSettlementVault.sol:383-397,446-479`). Small credits in two different
vaults cannot automatically combine to clear the runtime minimum. Removing v1
tooling after nine weeks can strand users operationally even while custody
remains in the contract.

Reconcile the requirement with an explicit migration conservation table:
uncaptured stake, funded/unfinalized epochs, finalized/unclaimed amounts,
accepted unpaid credits, carry and any surplus. Name the available action and
recipient for each. Retain v1 credit withdrawal support for as long as credits
exist. If residual value is accepted, record it as a quantified exception to
the requirement. Include delayed CRv4/native emissions at the old pool in the
boundary rehearsal; scheduling `E_s` alone does not prove an instantaneous
native weight change.

**F9 — P1: Null expansion has an omitted local limit and a constrained return path.**

Raising head and UID bounds is insufficient to submit a row for about 2,000
fleets (`VERSION2.md:716-749,840-862`). The local CRv4 implementation still caps
ciphertext at 5,000 bytes (`crv4/tlock.go:66-71`, `crv4/crv4.go:148-150`,
`crv4/chain.go:1257-1258`). Two thousand `u16` UIDs and two thousand `u16`
weights already require 8,000 payload bytes before encryption overhead. The
runtime's larger Null limit cannot help while the producer and replay readers
reject the submission. Add consensus- and mechanism-aware payload admission
to the migration scope and test the actual maximum head row end to end.

The proposed emergency return to Yuma also needs more than an empty commit
queue. The repository's runtime-475 review says switching requires the UID
count to fit the target mode's budget
(`docs/spec/runtime-475-audit.md:222-237`). After growing beyond 256, a return
therefore depends on reducing the active UID count, with fleet disruption and
potentially many trim operations. This contradicts treating Yuma as a readily
available response to stake-rank capture. Rehearse that exact path on the
selected runtime, including permissions, rate limits, pending commits and
protected custody/reserve identities, before presenting it as a fallback.

Keep Null as a separate decision. It helps the head, but changes who controls
emissions and how dividends flow; it is not necessary to remove the pool's
census, artifact or claim-cadence limits. Capture-surplus accounting also needs
a policy for attributing one shared escrow's surplus among multiple operators,
rather than allowing an arbitrary `noId` to receive the entire difference.

**F10 — P2: the stated streaming Merkle pipeline cannot use the cited construction as written.**

The ledger sorts leaves by coldkey but references a canonical tree that sorts
by leaf hash before pairing (`VERSION2.md:612-629` versus
`merkle/merkle.go:17-36,174-206`). Hash order is not coldkey order. Folding the
coldkey stream with an O(log N) frontier generally produces a different root.
Keeping the old construction requires sorting/storage or an additional
authenticated ordering, and verifying its correspondence with the ledger.

Likewise, producing a proof from only a leaf stream takes a scan or tree
construction; O(log N) proof service requires a retained internal-node index.
The repository explicitly notes that its odd-leaf promotion differs from
OpenZeppelin's complete-tree layout. Single-proof compatibility does not by
itself establish the multiproof construction assumed for `claimBatch`.

Choose and version the exact tree format, leaf order, padding, duplicate and
empty-tree rules, plus the proof index and multiproof algorithm. A fixed,
coldkey-ordered complete tree is one plausible new format, but it must have
cross-language vectors and verified batch leaf ordering. Describe the two-pass
allocation and sorted provider-to-coldkey aggregation needed to recompute
weights with bounded RAM, and account for external storage if used.

Finally, distinguish resource classes: verifying every row is O(N) work and
retaining all chunks is O(N) disk/network per epoch. That can be entirely
reasonable at one million providers, but it does not meet requirement 1's
population-independent per-epoch work or requirement 3's cohort-only disk
bound (`VERSION2.md:469-478`). State the achievable bounds and benchmark them.

**F11 — P2: flooring to integer rao does not guarantee a positive payout for every provider.**

`VERSION2.md:620-624` says flooring leaves no coldkey rounded to zero. For any
positive weight smaller than `sum_weights / epoch_pool_rao`, the stated formula
returns zero. For example, a 10-rao pool and weights 1 and 1,000 pay 0 and 9
rao. Repeating the same allocation, even while carrying the aggregate dust,
need never award the small account anything: aggregate carry is not that
account's fractional entitlement.

Specify whether allocation first aggregates weights by coldkey, as v1 does
(`protocol/payout.go:53-80`). If flooring happens per provider, the aggregate
remainder bound is based on provider count, not leaf count, and splitting one
provider can change its payout. Amounts remove the severe 10,000-bps ceiling;
they do not remove integer quantization. Either keep a per-coldkey fractional
remainder with a defined exact transition, choose another explicitly fair
rounding rule, or narrow the guarantee. Test sub-rao accrual and multiple
providers sharing one coldkey.

**Decisions still needed, distinct from the concrete contradictions above.**

- **One canonical payout in a multi-validator system.** Each validator has its
  own seed, cohort, Wilson score and prior EMA. Which measurement is the common
  allocation input at `VERSION2.md:566` when their results differ? Specify a
  common reproducible source or aggregation rule. Independent operator-quality
  estimates can disagree without making the payout ledger itself ambiguous.
- **Head measurement.** Sampling 4,096 of one million providers does not ensure
  coverage of 2,000 head fleets. With one provider per fleet, only about 8.2
  fleets are selected in expectation under a uniform draw. Existing head
  scoring requires observed prefixes attributable to the current fleet binding
  (`validator/attempt_cut_v2_head.go:75-100`). Budget a separate head measurement
  path or demonstrate sufficient inclusion and prefix coverage; "unchanged"
  head scoring does not follow from increasing UID limits.
- **Challenge effect and timing.** Existing finalization has a time guard but
  no challenge/evidence veto (`evm/src/STCoordinator.sol:635-653`). A late
  challenge cannot undo an already paid root or already emitted epoch. A
  nonzero quality multiplier also cancels out when a pool is the only pool,
  because channel scores are normalized
  (`validator/release_math.go:31-66`). State the future epoch a penalty affects,
  how zeroing persists, and whether fraud can be profitable before operator
  exit. A discretionary future bond is not an implemented remedy.
- **Permit and batch semantics.** Define the signed domain, chain/vault/pool,
  exact cumulative ceiling or authorized increment, named relayer, fee unit,
  maximum fee, replay protection and deadline. Define fee treatment when a
  recipient or relayer credit is below the runtime floor, and whether one
  invalid leaf or failing transfer reverts a whole batch. The sample signature
  alone does not specify these safety properties.
- **Admission and rollout gates.** Phase 1 changes signed configuration and
  measurement semantics while saying the treasury approval stays. The existing
  capacity path preserves economic authority and forbids changing fixed
  dimensions (`validator/production_capacity.go:82-94,117-120,158-170`). Specify
  the new authenticated successor path and any required approval, rather than
  assuming the old approval covers schema v3. Replace epoch-number deadlines
  with qualification gates, including a deployable coordinator build: its
  reported 12-byte code-size margin is a real feasibility constraint
  (`evm/CLAIM-RECOVERY.md:52-54`).

**Alternate approaches and their tradeoffs.**

The core design has three separable choices: how to establish earned usage,
how to measure service quality, and how to settle accumulated value. They do
not need to migrate together. The comparisons below assume every payout still
needs authenticated provider/wallet attribution and publicly available data.

| Approach | Security and correctness versus VERSION2 | Scale and economics | Complexity and preferred use |
| --- | --- | --- | --- |
| **A. Usage-based provider allocations; sampled operator quality; cumulative claims** | Uses one canonical allocation input. Removes the mixed measured/attested per-provider reliability rule and its covert-cohort assumption. Receipt attribution still needs independent verification; operator-level quality does not punish an individual poor provider directly. | Bounded quality sampling; O(N plus receipt volume) streamed verification; O(operators) settlement and amortized claims. Same gas floor as VERSION2. | Best first release if authenticated closed work is an acceptable reward basis. Smaller trust and coordination change than introducing a new per-provider reliability oracle. |
| **B. Precommitted service evidence with retrospective random audits** | Freezes attestations before audit indices become observable. Active online probes remain a separate quality signal. Audits require independent, replayable original evidence and a remedy for omissions, not only a commitment to operator assertions. | Sampling can be bounded; evidence collection/publication remains proportional to work. Audit probability must cover economically significant fraud, potentially using amount-weighted sampling plus a coverage floor. | Prefer when per-provider reliability materially improves incentives and an independently verifiable signal can be defined. More evidence and timing machinery than A, but a defensible replacement for the proposed blind-cohort claim. |
| **C. Non-expiring per-epoch amount roots with a multi-epoch claim** | Once finalized, an epoch allocation cannot be erased by a later root. Avoids cross-epoch cumulative-state transitions; still requires correct roots and funding. Aggregate several earned epochs into one recipient credit and runtime transfer. | O(operators) new roots each epoch; O(number of claimed epochs) proof/claim-state work. Saves repeated transaction and transfer overhead, but proofs grow with waiting time and old roots remain stored. | A simpler bridge if historical entitlement safety is the main concern or cumulative transitions cannot be qualified promptly. Worse than cumulative roots for providers waiting many years; cap batch sizes and retain proof availability. Requires a new vault too. |
| **D. Cumulative roots with enforceable admission/disputes** | Makes root correctness an activation condition. A fixed audit quorum can gate activation, or an objective challenge system can freeze an invalid transition during a delay. Bonds need sufficient backing and an exit delay. Neither scheme proves physical service from fabricated inputs. | Normal on-chain work remains O(operators), plus disputes. Added latency, bond capital and data availability obligations; no reduction in per-recipient gas. | Prefer before untrusted operators can overwrite lifetime balances. A quorum is operationally simpler but introduces approval/censorship trust; a permissionless fraud-proof system is substantially harder. |
| **E. Validity-proved cumulative ledger transitions** | Proves per-coldkey continuity, sums and deterministic allocation from authenticated inputs. It does not prove the truth or completeness of operator-supplied traffic, and it does not solve withheld claim data. | Moves O(N plus work) computation to a prover and can make validator transition verification succinct. Claims still pay native transfer costs. | Reconsider if population-independent validator verification is a firm requirement. Highest implementation/prover/circuit burden; unnecessary for a measured, affordable O(N) verifier, but not dismissible solely because streaming is possible. |
| **F. A cheaper settlement domain or native transfer improvement** | A new chain/rollup/bridge changes custody and exit assumptions; a native batch primitive could preserve them if correctly designed. These are separate designs requiring their own review. | Only approaches that lower the actual transfer/transaction cost, subsidize it, or share a payout coldkey can materially shorten economic payment latency. A native batch may still retain per-recipient runtime work. | Explore if timely small payments to distinct owners are a requirement. Substantial external dependency; cumulative accrual remains useful while that work proceeds. |

**Recommended combination: A now, with B or D added when their assumptions are justified.**

Keep the document's amount ledger, per-chunk integrity checks, custody-preserving
successor vault and user-chosen claim threshold. Use independently verified
closed work as the shared payment input. Keep bounded random probes for
operator quality and budget head measurements separately. A provider's chance
of being probed should not itself change its payout formula. If a common
per-provider reliability factor is necessary, first specify a replayable
committed signal and the retrospective audit protocol in B; validators can
independently replay that common input while maintaining their own quality
estimates.

For settlement, implement and qualify the per-coldkey continuity rules in F2
and decide how their verification gates root activation. Cumulative claims are
preferable to C for long accrual periods, but C is an honest fallback if the
team can deliver isolated amount roots sooner than a safe cumulative state
machine. D becomes more valuable when independent operators join; introducing
it later requires leaving an explicit governance/contract migration path now.
Keep Null out of the critical path for this release. Native head growth can be
qualified later without delaying payment eligibility for the remaining
hundreds of thousands of providers.

The economic product requirement should be decided before selecting F. Using
the proposal's own 13.6-TAO weekly pool and 0.00125-TAO claim assumption, with
equal average entitlements and no head allocation:

| Population | Accrual to equal one claim's gas | Accrual for gas to be 10% of payout |
| --- | ---: | ---: |
| 100,000 | about 9.2 weeks | about 92 weeks / 1.8 years |
| 1,000,000 | about 92 weeks / 1.8 years | about 919 weeks / 17.7 years |

These are conditional arithmetic examples, not forecasts: price, emissions,
usage distribution, actual runtime gas and batching change them. They show why
"claimable after 48 hours" is different from an economical payment after 48
hours. Moving the highest earners into the head can make the pool's remaining
long tail slower to reach a useful threshold. Shared coldkeys help fleets that
already share an owner; they are not a trust-free solution for unrelated users.

**Suggested qualification sequence.**

1. Publish the revised threat model, canonical allocation formula, exact
   resource classes, payout-latency target and cohort transcript. Resolve the
   concrete counterexamples above before using rollout epochs as commitments.
2. Build one million-provider replay fixtures including original receipts,
   skewed strata, churn, inactive accumulated balances and shared coldkeys.
   Measure end-to-end time, peak memory, disk, network and proof-service cost,
   including historical continuity, rather than only compact row bytes.
3. Exercise the new vault's funding/credit/payment conservation and all root
   successor failures, then rehearse dual-vault routing, dual evidence domains,
   late v1 carry, unpaid credits and a missed first v2 root. Require compatible
   claim tooling and deployable bytecode before selecting `E_s`.
4. Qualify Null separately with a full-size CRv4 row, head measurement coverage,
   dividend attribution and a demonstrated return path from the actual expanded
   UID population. Its security and economics deserve their own decision.

**Review limits.** I read the complete proposal and the cited local paths used
for these findings, inspected the pinned server source, and checked the
sampling, rounding, cumulative-reassignment and latency counterexamples with
small offline calculations. I did not reproduce the proposal's Foundry gas
measurements, query live chain state or run an implementation test suite. The
named `server-census` worktree and `subtensor` checkout were absent here; remote
source retrieval did not succeed. Consequently the stopgap's exact uncommitted
behavior is not independently certified by this review, and the Null switch
constraint is attributed to the repository's retained runtime-475 review and
must be verified on the selected runtime. No source, policy, configuration,
contract or live deployment was changed.
