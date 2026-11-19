# Response to the review of VERSION2.md, and the final version-2 design

Prepared 2026-10-09. Part 1 answers [VERSION2-REVIEW.md](VERSION2-REVIEW.md)
finding by finding, checking each cited line against the code. Part 2 is the
version-2 design that results; it is self-contained and supersedes
[VERSION2.md](VERSION2.md).

Inputs and pins. Unprefixed paths are this repository at `e1500f15`.
`server/` paths are the operator server at main `f08a0223`; the reviewer read
`5342fd76`, and `git diff --stat 5342fd76 f08a0223` is empty for every server
file cited below, so those line numbers hold at both pins. `subtensor/` paths
are upstream `5c6e83e` (spec 477, released October 8); where the runtime-475
source `d1718c99` that mainnet runs is cited, it is named. Contract sizes and
gas come from the proposal's Foundry scratch copy with the pinned libraries
(OpenZeppelin 5.6.1, solc 0.8.24, cancun, via-IR), and tree measurements from a
Go program against this repository's `merkle` package; both are described
where their numbers appear. Nothing here was run against a chain.

Facts since the proposal, taken as given: SN25 launched October 8-9 with one
validator (UID 1), one operator (`no_id` 1, pool hotkey UID 169), reserve
recipients UIDs 170 and 250, momentum 1/10 fixed in code and in the signed
treasury approval, epoch 1 from block 9,247,228 with 50,400-block epochs
(`mainnet/LAUNCH.md:19-58`). The measurement stopgap is merged: the server
samples verify next hops from a cohort of at most 1,500 providers per
settlement epoch and 2,000 over the deployment's life, admitted by one atomic
Redis script, with the epoch taken from the st sync mirror
(`server/model/verify_cohort_model.go:16-32,103-130,166-178`;
`server/controller/verify_controller.go:289-311`). Only cohort members can
satisfy the artifact's eligibility rule (`server/controller/st_controller.go:3329-3331`),
so about 98.5k of 100k providers earn nothing from SN25 after the October 6
cutoff until version 2 is live. Runtime 475 is live with `SubnetEpochConsensus`
and `NULL_UID_BUDGET` 2,500; SN25 is Yuma at 256 of 256 UIDs; the validator
refuses `SubnetworkN` above the approval's 256 (`validator/recycle_observation.go:199-206`).

Overall verdict. The review is correct in substance on every finding. Nine
findings are accepted as written; two (F4, F7) are accepted with a
modification that the code forces. The two findings that most change the
design are F1 (the online cohort is visible to the operator, so it cannot audit
operator attestations) and F2 (a cumulative root needs a per-coldkey
continuity rule, and something must stop an invalid successor from becoming
authoritative). The review's recommended combination, usage-based allocation
with sampled operator quality and cumulative claims now, is adopted, together
with the cheapest enforceable part of its option D (objective on-chain
decrease proofs plus a validator veto inside the existing 48-hour finalization
window). Per-provider attested reliability leaves the payout formula until a
replayable committed signal exists (option B, phase C). Null consensus is a
separate track. One correction to the review: the Null return-path constraint
is in the live runtime's code, not only in the repository's audit note.

| Finding | Verdict | Effect on the design |
| --- | --- | --- |
| F1 cohort visible to operator | Accepted | attestation out of the payout formula; retrospective audits on committed evidence; sampling frame instead of membership proofs |
| F2 cumulative continuity | Accepted | per-coldkey continuity rule, authenticated transition, funding equality on chain, watermark semantics, decrease proofs and validator veto before finalization |
| F3 evidence journal | Accepted | new companion `STValidatorEvidence2` anchored to the v2 settlement contract; v1 journal untouched |
| F4 payment evidence | Accepted with modification | chunked evidence graph with the existing authority checks; validator work bounded by a signed audit budget, not population-independent |
| F5 estimator bias | Accepted | population-weighted stratified estimator with design variance; estimand defined |
| F6 state bound | Accepted | exact apportionment, sampling without replacement, epoch replacement budget, separate bounds, no carried per-provider EMA |
| F7 pool identity | Accepted with modification | no coordinator upgrade (12-byte margin); `STSettlement2.settlementAt(noId, epoch)` as the single identity lookup |
| F8 stranding | Accepted | conservation table, a designated v1 carry-sweep epoch, tooling retained, quantified residue as an explicit exception |
| F9 Null limits | Accepted | mode-aware CRv4 payload admission; return path rehearsed with trimming; surplus attribution formula; Null off the critical path |
| F10 Merkle pipeline | Accepted | OpenZeppelin-layout tree over coldkey order, measured at 1M leaves; resource classes restated |
| F11 rounding | Accepted | aggregate by coldkey, largest remainder in rao, guarantee narrowed and stated |

## Part 1. Response to the review

### F1 (P1): the operator learns the cohort before making the attestations being audited

Restated. The proposal keeps the VRF output secret, but the validator asks
the server for specific indexed members and routes assignments to them, so
the server knows the cohort as soon as it is drawn; the attestation error
`δ_n` can then be zero while unmeasured providers are overstated. The
transcript also lacks a VRF proof, index-authenticating proofs and an
equivocation rule.

Verified. The server assigns every next hop itself: under the stopgap it draws
from its own Redis sets and returns the member (`server/model/verify_cohort_model.go:247-328`),
and without a cohort it draws uniformly (`server/model/verify_model.go:562-647`).
Any protocol in which the validator names a member by index gives the server
that member's identity at request time. The proposal's claim at
`VERSION2.md:519-523` that the cohort is "unpredictable to providers and the
server until the validator reveals `s`" is false for the server. The
repository's Merkle proofs are commutative sorted-pair folds that prove
membership, not position (`merkle/merkle.go:13-15,275-285`), so an ordinary
membership proof cannot authenticate an index, as the reviewer says.

Verdict: accepted.

Changes. (1) Per-provider attested reliability is removed from the payout
formula. The allocation input is verified usage only (Part 2 §6); validator
measurement produces operator-level quality and audits, never a per-provider
payout factor, until a committed and replayable per-provider signal exists
(Part 2 §15, phase C). With nothing per-provider to inflate, cohort knowledge
no longer pays. (2) The online cohort's purpose is restated honestly: it
estimates operator quality for cross-operator weighting and gating, with the
explicit residual assumption that an operator can favour members it sees
probed, so the estimate is an upper bound under an adversarial operator (Part
2 §3). (3) Retrospective audits of committed evidence use a public beacon
revealed after the commitment: the finalized native block hash at
`end + root_commit_window + 1`, after the ledger and evidence index are bound
on chain (Part 2 §7). A public value revealed after commitment is sufficient
for a retrospective audit, as the review notes; the VRF is kept only for the
online draw. (4) The membership-proof protocol is replaced by a sampling
frame: the operator commits one sorted eligible set per settlement epoch, the
validator downloads it whole (17 bytes per row, 17 MB at 1M), checks
uniqueness and stratum labels, draws indices by VRF with a published proof,
and checks that every assignment returned is the frame member at the drawn
index. Equivocation (two signed frames for one epoch) and an assignment that
is not the drawn member are audit findings with the zero-weight remedy (Part 2
§5). The four acceptance cases at `VERSION2-REVIEW.md:66-70` are in the test
plan (Part 2 §16).

### F2 (P1): cumulative-total checks do not protect a provider's previous entitlement

Restated. With only `latest` retained and only aggregate monotonicity checked,
`{Alice: 100}` followed by `{Alice: 0, Mallory: 100}` passes every stated
check and Alice's entitlement is gone. The transition from the previous ledger
must be authenticated per coldkey, funding must subtract what was already
allocated, and claim credit and runtime payment need separate counters.

Verified. The proposal's checks are `cumulativeTotal ≥ latest.cumulativeTotal`
and `≤ captured` (`VERSION2.md:648,666-668`); neither inspects a coldkey. The
proposal's `allocated` comment at `VERSION2.md:644` ("Σ cumulativeTotal ever
finalized") is wrong as the reviewer says: successive cumulative totals 100
and 150 mean 150 allocated. The v1 vault shows why the credit/payment
distinction matters: `claim` credits `claimCredit[coldkey]` and marks the
leaf claimed before attempting the transfer (`evm/src/STSettlementVault.sol:379-388`),
and a deferred transfer leaves the credit withdrawable later (`:394-398,446-482`);
a cumulative design that only advanced its watermark on successful transfer
would re-credit the same entitlement on every deferred attempt.

Verdict: accepted.

Changes (Part 2 §6, §9, §10). The ledger carries every coldkey ever paid,
forever; for each coldkey `cumulative_e = cumulative_{e-1} + delta_e` with
`delta_e ≥ 0`, so dormant, promoted, disconnected and wallet-changed providers
keep their balance; leaf count is monotone; the header links to the previous
root and total. The validator verifies the transition by a streaming merge of
the previous and new leaf chunks, both sorted by coldkey, in O(N) time and
O(1) memory. On chain, `finalizeDistribution` requires
`cumulativeTotal == finalizedTotal[noId] + unfinalizedFunding[noId]` exactly,
so gross funding is never allocated twice, and keeps `credited[noId] ≤ finalizedTotal[noId]`
so an over-allocating ledger can at most spend its own pool's funds. The
watermark `paidRao[noId][coldkey]` advances when credit is created; payment is
a separate counter, exactly as v1 separates `Claimed`, `ClaimPaymentDeferred`
and `ClaimPaid`. The conservation identity is extended per pool. What stops an
invalid successor becoming authoritative: a root is pending between the commit
deadline and finalization (the existing 1,200 and 14,400-block offsets,
`deploy/mainnet/policy-v1.yml:45-46`); during that window anyone may submit an
objective decrease proof (two Merkle proofs showing a coldkey's pending
cumulative is below its finalized one), and any activated validator may
publish a veto evidence record after its continuity, sum and funding audit;
either blocks finalization and the epoch's funding carries into the next
distribution. The vault also retains a ring of the last four finalized roots so
an older proof remains claimable (exit liveness), which is safe because a
coldkey's cumulative never decreases. The review's test list (the Alice
example, omission of an inactive account, repeated sub-minimum claim,
out-of-order finalization, missing epoch then recovery) is in §16.

### F3 (P1): the existing evidence journal cannot support the specified successor unchanged

Restated. `STValidatorEvidence` captures the settlement vault at construction
and rejects activations naming another vault; its library accepts only kinds
1 and 2; the `anchored` modifier checks the coordinator's single pointer.

Verified. `settlementVault` is an immutable read from
`coordinator_.settlementVault()` in the constructor (`evm/src/STValidatorEvidence.sol:26,60,64`);
`publishActivation` rejects `record.domain.settlementVault != settlementVault`
(`:101`), and `commitEvidence` derives the header domain from the activation
record (`:138`), so no v2 vault can be named. `ValidatorEvidence.valid`
returns false for any kind other than `CLOSED_CENSUS = 1` and `DEPOSIT_AUDIT = 2`
(`evm/src/lib/ValidatorEvidence.sol:13-14,95-99`). `anchored` requires
`coordinator.validatorEvidence() == address(this)` on every write (`:73-78`),
and `fixValidatorEvidence` is one-shot (`evm/src/STCoordinator.sol:928-932`).
The proposal's `AUDIT_CHALLENGE` kind and "unchanged journal" (`VERSION2.md:588-594,699-701,891-893`)
are incompatible with all three.

Verdict: accepted.

Changes (Part 2 §9.3). A new companion `STValidatorEvidence2` is anchored to
the v2 settlement contract and vault, with `/v2` domain strings, kinds 1, 2
and `LEDGER_AUDIT = 3` (subject: audited epoch; a verdict field), and its own
`anchored` check against `STSettlement2.validatorEvidence()`. The v1 companion
and the v1 coordinator pointer are not touched, so v1 evidence keeps working
for epochs before the switch. A header signed for one domain cannot be
replayed in the other because the domain strings and the vault address differ
and both are inside the signed payload (`evm/src/lib/ValidatorEvidence.sol:72-84,144-162`).
Acceptance: real calls for v1 evidence during overlap, v2 activation naming
vault2, a `LEDGER_AUDIT` accepted in the v2 domain and rejected by the v1
contract (§16).

### F4 (P1): the payment evidence is neither fully specified nor covered by the new byte budget

Restated. `ClosedWorkCensus` is an operator database projection, not
authenticated traffic; the stronger checks come from report, reservation,
service-party and wallet evidence; usage can move between providers with an
unchanged total; probe-hop volume is not an upper bound on customer usage;
and the census has hard caps of 32,768 records and 8 MiB of originals that
chunked rows do not remove.

Verified. The package header says exactly that (`payoutartifact/closed_work.go:1-3`).
`MaxClosedWorkRecords = 32768`, `MaxClosedWorkOriginalBytes = 8 MiB` and a
1 MiB per-record cap are enforced on clone (`:92-113`) and on verification
(`:251-261,273-302`); the whole-work inventory adds `MaxWholeWorkOwners = 8192`
and 8 MiB (`payoutartifact/whole_work_authority.go:33-36`). The server runs
the work-authority and whole-inventory paths before building
(`server/controller/st_controller.go:3383-3399,3442-3447`), and
`VerifyClosedWorkReports` is the report-chain check (`payoutartifact/closed_work_reports.go:81-111`).
The validator's artifact reader calls it (`validator/artifact.go:301-305`).
The proposal's hop-volume bound (`VERSION2.md:580-582`) compares different
workloads and is withdrawn.

Verdict: accepted with modification. Everything the reviewer lists is
adopted. The modification concerns resource classes: the review asks for the
originals to be budgeted. They are, and the honest consequence is that
verifying every original receipt is O(work) per epoch, not population-
independent. The design therefore commits all evidence (so it is available
and challengeable) and lets each validator audit a VRF-selected,
usage-weighted sample of evidence chunks with a coverage floor, bounded by a
signed `max_audit_bytes` per epoch; a validator with the capacity may verify
everything. Requirement 1 is restated accordingly (Part 2 §2).

Changes (Part 2 §7). The evidence graph: provider rows, leaf rows and
original closed-work records are all chunked (8 MiB) and indexed in the
signed ledger header; evidence chunks are partitioned by the same provider-id
ranges as provider chunks, so the join "ledger usage for provider p equals
the verified receipt sum for p" is local to one pair of chunks. The verifier
joins are those the code already performs: original receipts, report chains,
reservations, service party, wallet authority, earning window, head exclusion
and allocation. Redistribution with an unchanged total is caught by the
per-row join on any audited chunk; wrong coldkeys by the wallet-authority
join; the honest high-usage provider is no longer compared with probe volume.
Tests in §16 include a census beyond both existing limits.

### F5 (P1): the proposed estimator is biased under its own sampling design

Restated. Square-root quotas oversample small strata, but `Q̂_n` is the
exposure-weighted mean of sampled rows with no inclusion correction, so the
estimand changes; the 1%/99% example gives 0.9087 against a population mean
of 0.99, a bias far larger than the stated interval.

Verified. `ExactPoolQualityFromReleaseStats` weights each row by `Exposure`
only (`validator/measurement_stats.go:531-555`). The arithmetic reproduces:
sqrt allocation gives the 1% stratum 9.13% of the sample and a sample mean of
0.9087, bias 0.0813; the population-weighted stratified estimator returns
0.99. With m = 4,096 and the sqrt split (374/3,722) the worst-case standard
error of the population-weighted estimator is 0.0081 (95% half-width 0.016),
and with the proportional split (41/4,055) it is 0.0078, so stratification
costs little precision once the estimator is right. The second point stands
too: `protocol.ReliabilityPPM` is a 95% Wilson lower bound (`protocol/reliability.go:12-30`),
systematically below the success rate, so comparing it with an honest
point-estimate attestation would produce a positive one-sided error.

Verdict: accepted.

Changes (Part 2 §5.5). The estimand is the provider-mean quality over the
sampling frame (each eligible provider once); a usage-weighted variant is
specified as a policy option. The estimator is `Q̂ = Σ_s (n_s/N) q̂_s` with
`q̂_s` the mean Wilson lower bound over measured members of stratum s with at
least `a_min` assignments; exposure weighting is removed; the design variance
`Σ_s W_s² s_s²/m_s` with finite-population correction is published with
`n_s`, `m_s`, measured counts and the interval. Nonresponse is explicit: a
stratum with fewer than `q_min` measured members uses its previous estimate
with its variance widened by the policy factor, or the measurement reports
"unavailable" and the previous operator-level EMA holds. The attestation
error estimator is gone with attestation; when phase C introduces a committed
per-provider signal, the audit compares like with like (the same statistic
computed from the same committed inputs). Synthetic populations with known
means under unequal strata are in §16.

### F6 (P1): cohort quotas and churn do not establish the claimed state bound

Restated. The quota formula does not sum to m, is not capped at `n_s`, has no
integer apportionment; counter-mod-n hashing samples with replacement; a 25%
per-tempo replacement allowance can touch 146,432 distinct providers in an
epoch; and the `m × (1+K)` bound omits departed members. Phase 0's Redis
rotation also needs the validator to expire its quality maps.

Verified. The formula at `VERSION2.md:527-529` sums to m only when no floor or
cap binds; with strata {10; 1,000; 98,990}, `q_min` 64 and caps it sums to
4,069. The validator persists `Ema`, `EmaPPM`, `Window` and `Egress` maps
(`validator/stats.go:178-191`) and unions all of them into the census
(`validator/measurement_stats_v2.go:42-73`; `validator/attempt_settlement_v2_runtime.go:225-262`),
so any provider ever scored stays in the bound until the validator evicts it;
server-side rotation alone would overflow `max_providers` again, as the
stopgap's own comment says (`server/model/verify_cohort_model.go:6-14`).

Verdict: accepted.

Changes (Part 2 §5.3-5.4, §5.7). Strata are at most 8, labelled in the frame.
Quotas are Hamilton apportionment of m across strata with weights `√n_s`,
floors `q_min`, caps `n_s` and deterministic redistribution of any surplus.
Draws are without replacement: the k-th index of stratum s is
`PRF(seed, s, k) mod n_s` with duplicates skipped and k continuing. The
replacement budget is epoch-wide, `R = m/3`, spent only on members the server
reports unavailable; when exhausted the cohort shrinks and `m_eff` falls. No
per-provider EMA is carried across epochs (K = 0): with about 500 assignments
per member per epoch the Wilson bound is tight without smoothing, and the
operator-level estimate gets a scalar EMA instead. Bounds are separate:
active members ≤ m = 3,072; distinct measured per epoch ≤ m + R = 4,096;
egress hashes ≤ 8 per measured provider; head entries bounded by the head
budget (§5.8), summed over operators. These fit the existing fixed
`max_providers` 4,096 and `max_egress_hashes` 32,768, and a transition of
about 1.7 MB fits `max_transition_bytes` 2 MiB. At activation the validator
evicts `EmaPPM`/`Ema` entries not in the current cohort in one recorded
transition, which is what the stopgap's lifetime set was standing in for.

### F7 (P1): the proposed migration does not publish the new pool identity to existing readers

Restated. `scheduleOperator` preserves `prior.poolHotkey`; the validator finds
the weight destination through `operatorAt(...).PoolHotkey`; the claim endpoint
fills the vault address from global configuration; `_validatePolicy` still
requires nonzero `claimTTLEpochs`.

Verified. `scheduleOperator` copies `prior.poolHotkey` into every new version
(`evm/src/STCoordinator.sol:438,446`); only `registerOperator` sets a pool
hotkey and it registers it in the v1 vault (`:379-410`). The validator's
decision chain reads `operatorAt` and resolves `version.PoolHotkey` to a UID
(`validator/release_decision_chain_v2.go:380-412`). The claim result's
`ContractAddress` and `SettlementVaultAddress` come from `cfg.SettlementVault`
(`server/controller/sn_controller.go:293-297`) regardless of epoch.
`_validatePolicy` reverts on `claimTTLEpochs == 0` (`evm/src/STCoordinator.sol:337`)
and `_validateSettlementWindow` reads the v1 vault's `minimumClaimTTLBlocks`
(`:351-357`).

Verdict: accepted with modification. The modification is forced by a fact the
reviewer also flagged under admission gates: the coordinator implementation is
24,564 bytes with a 12-byte runtime margin (`forge build --sizes` on the
scratch copy; `evm/CLAIM-RECOVERY.md:52-54`). The proposal's UUPS upgrade with
new functions (`VERSION2.md:693-701`) cannot be compiled into the proxy
without removing or relocating code, so the design does not upgrade the
coordinator at all.

Changes (Part 2 §9.2, §9.4). A new contract `STSettlement2` is the v2 vault's
coordinator. It reads operators, policy and epoch boundaries from the v1 proxy
(`operatorAt`, `policyAt`, `epochStartBlock`, `epochEndBlock`), holds the v2
pool hotkey per operator, and exposes one authenticated lookup
`settlementAt(noId, epoch) → (vault, poolHotkey, schema, evidence)` that returns
v1 identities before the switch epoch and v2 after it. The validator's chain
reader, the server's claim endpoint, the claim daemon, historical replay and
signed configuration selection consume that lookup. The v1 proxy's policy
schedule remains the single clock; policy v2 keeps `claim_ttl_epochs` and
`claim_grace_epochs` populated (inert for v2 pools) so `_validatePolicy` and
`_validateSettlementWindow` continue to hold. Tests exercise the last v1 and
first v2 epochs through the public readers (§16).

### F8 (P1): the rollout does not satisfy the stated no-stranding requirement

Restated. The final v1 epoch can only include carry present when it finalizes;
later expiries add carry with no successor epoch; a finalized entitlement
cannot be finalized again; deferred claim credits stay withdrawable in v1
independently of expiry and cannot combine with v2 credits to clear the floor;
removing v1 tooling would strand users operationally.

Verified. `expireEntitlement` adds the unclaimed remainder to `carry[noId]`
(`evm/src/STSettlementVault.sol:400-408`), and carry is folded into a total
only by a later `finalizeEntitlement` (`:334-336`), which requires `Funded`
status (`:328`), set only by `captureEmission` or `deferEmission` for a new
epoch (`:251-318`). `claimCredit` is a global per-coldkey balance withdrawable
by anyone through `withdrawClaimCredit` (`:65,386,394-398`). Claims open at
finalization and close at the start of epoch E+10 (`evm/src/STCoordinator.sol:641-645`;
`deploy/mainnet/policy-v1.yml:33-34`), so expiries of v1 epochs continue for
about nine weeks after any switch.

Verdict: accepted.

Changes (Part 2 §12). A migration conservation table names every bucket
(uncaptured stake on the v1 pool hotkey, funded but unfinalized v1 epochs,
finalized and unclaimed v1 entitlements, accepted unpaid v1 credits, v1
carry, Null surplus) with its action and recipient. The exit procedure adds
a designated carry-sweep epoch on the v1 vault after the last v1 expiry: the
v1 proxy needs no change for it, because `closeOperatorEpoch` for an unused
epoch observes whatever stake sits on UID 169 (including emission that
arrived after the switch from reveal lag), `Funded` is set, and
`finalizeEntitlement` folds all carry into that root, which pays the v1
coldkeys pro rata to their expired remainders. Its own remainder after expiry
and sub-minimum v1 credits are the residue, recorded as a quantified exception
to requirement 8. v1 claim and credit-withdrawal tooling stays in the daemon
indefinitely.

### F9 (P1): Null expansion has an omitted local limit and a constrained return path

Restated. The local CRv4 producer and readers cap ciphertext at 5,000 bytes,
below a 2,000-UID row; returning to Yuma after growing past 256 UIDs requires
trimming; capture-surplus attribution across operators is undefined.

Verified. `MaxCommitSizeBytes = 5000` (`crv4/tlock.go:66-71`), enforced on
prepared payloads (`crv4/crv4.go:148-150`) and on the extrinsic (`crv4/chain.go:1257-1258`).
A SCALE-encoded row of 2,000 UIDs and weights is about 8,044 bytes before
encryption; 1,200 UIDs is about the most that fits 5,000. The runtime itself
enforces 5,000 bytes for Yuma at dispatch and 32 KiB divided by the mechanism
count for Null (`subtensor/pallets/subtensor/src/lib.rs:63-68`;
`subtensor/pallets/subtensor/src/subnets/weights.rs:450-452,539-545`). The
return-path constraint is not only in `docs/spec/runtime-475-audit.md:226-229`:
`do_set_epoch_consensus` requires `get_subnetwork_n(netuid) <= budget(mode) / mechanism_count`
in the runtime-475 source (`d1718c99` `pallets/subtensor/src/subnets/mechanism.rs:124,146-147`;
identical at upstream `5c6e83e:124,146-147`), so a return to Yuma from more
than 256 UIDs fails with `TooManyUIDsPerMechanism` until the owner trims in
batches of 64 under an immune-percentage guard (`subtensor/pallets/subtensor/src/subnets/uids.rs:9,175-191,196-245`).
The proposal's `captureEscrowSurplus(noId)` (`VERSION2.md:749-753`) indeed let
one `noId` take a shared surplus.

Verdict: accepted.

Changes (Part 2 §13). Null is a separate track with its own gates. CRv4
payload admission becomes consensus- and mechanism-aware in the producer and
in every replay reader, and the maximum head row is tested end to end. The
return path is rehearsed on the selected runtime from the actual expanded
population, including trimming, rate limits, pending commits and the
protected custody and reserve UIDs, before Null is presented as reversible.
Escrow surplus is attributed pro rata to each pool's outstanding liability by
a fixed on-chain formula. Nothing in the pool's census, ledger or claim path
depends on Null.

### F10 (P2): the stated streaming Merkle pipeline cannot use the cited construction as written

Restated. The ledger sorts leaves by coldkey but the canonical tree sorts by
leaf hash, so a coldkey-ordered frontier fold yields a different root; O(log N)
proof service needs retained internal nodes; the repository's odd-leaf
promotion differs from OpenZeppelin's layout, and single-proof compatibility
does not establish multiproofs.

Verified. `NewTree` sorts leaf hashes (`merkle/merkle.go:22-25,174-184`),
promotes a trailing odd node (`:205-220`) and documents that OpenZeppelin's
JS layout differs for some leaf counts (`:31-36`); proofs come from retained
levels (`:239-273`). OpenZeppelin's multiproof requires a complete tree with
leaves supplied in the reverse of their tree order
(`lib/openzeppelin-contracts/contracts/utils/cryptography/MerkleProof.sol:350-357`
and its CAUTION note), which the promotion layout does not guarantee.

Verdict: accepted.

Changes (Part 2 §8). The ledger tree is the OpenZeppelin StandardMerkleTree
layout (array heap, root at 0, children of k at 2k+1 and 2k+2, leaf i at
2n−2−i) over leaves in ascending coldkey order, with the same double-hashed
leaf and commutative pair hash as today, so `MerkleProof.verify` accepts
single proofs unchanged and `multiProofVerifyCalldata` accepts multiproofs.
Measured with the `v2measure` program on this machine: at 1,000,000 leaves,
sorting and leaf hashing take 1.25 s, the tree builds in 0.41 s into a 61 MiB
node array, peak heap 168 MiB including the ledger, proofs are 19-20 nodes
and take 9 µs each from the retained array; multiproofs for batches of 8, 64
and 256 leaves (132, 839 and 2,820 proof nodes) verify against a Go port of
`processMultiProof`. The verifier's resource classes are stated: O(N) time
and about 64 bytes per leaf of memory to rebuild a root; O(N) disk per epoch
to retain chunks; O(log N) proof service from the retained array. Cross-
language vectors come from the OpenZeppelin JS library, as the repository
already does for v1 (`merkle/testdata/gen_oz_fixtures.mjs`).

### F11 (P2): flooring to integer rao does not guarantee a positive payout for every provider

Restated. `floor(pool × w/Σw)` is zero for any weight below `Σw/pool`; a
10-rao pool with weights 1 and 1,000 pays 0 and 9; aggregate carry is not the
small account's fraction; whether aggregation by coldkey precedes flooring
changes the remainder bound.

Verified. The arithmetic is as stated. v1 aggregates by coldkey before
allocating and uses largest remainder with a client-id tie-break
(`protocol/payout.go:53-80,102-126`); the proposal's "no coldkey is ever
rounded to zero" (`VERSION2.md:620-624`) is false in general.

Verdict: accepted.

Changes (Part 2 §6.3). Allocation aggregates usage by coldkey first (v1
semantics, so splitting a provider does not change its coldkey's amount),
floors in rao, then distributes the remaining `F − Σ floors` rao one each to
the largest fractional remainders with coldkey-ascending tie-break, so the
sum is exact and there is no residue. The guarantee is narrowed to what is
true: every coldkey with `W_c ≥ ΣW/F` receives at least one rao; below that
the deterministic remainder distribution decides. At the live pool size
(about 2.07 × 10¹² rao per epoch) the threshold is a 5 × 10⁻¹³ share of usage,
below one byte of a petabyte, so the case is theoretical today; it is stated
rather than hidden. Sub-rao accrual and shared coldkeys are tested (§16).

### Decisions still needed

One canonical payout in a multi-validator system. Resolved by the F1 change:
the allocation input is the operator's verified-usage ledger, which every
validator verifies from the same committed evidence; validators' own cohorts,
Wilson scores and EMAs feed only their weights, as Yuma intends. The ledger
is never ambiguous because no validator measurement enters it.

Head measurement. The reviewer is right that 4,096 uniform draws from a
million providers cover about eight bound fleet members, and head scoring
requires hops through currently bound members (`validator/attempt_cut_v2_head.go:75-100`).
The design budgets a separate head assignment path: the validator requests
hops by bound client id from the on-chain binding set (bounded by
`maximum_head_fleets × members`), with its own `a_min_head`, independent of
the pool cohort (Part 2 §5.8). "Unchanged head scoring" was an overstatement.

Challenge effect and timing. `finalizeOperatorEpoch` has a time guard and no
veto (`evm/src/STCoordinator.sol:635-654`), and with one pool a quality
multiplier cancels in `BuildWeightVectorExact` (`validator/release_math.go:31-66`).
The design makes the remedy binary and enforceable: a decrease proof or a
validator veto during the pending window blocks finalization of that root;
zero pool weight applies from the first decision the validator computes after
the finding and persists until the operator's next clean finalized epoch;
fraud detected before finalization pays nothing; fraud that escapes the
sample pays only providers the operator chose, which is the operator's own
exposure, so the gain requires operator-controlled coldkeys and is bounded by
one epoch's pool funding; the bond that makes expected loss exceed that gain
is phase C (Part 2 §10). A discretionary future bond is not counted as a
remedy today.

Permit and batch semantics. Fully specified in Part 2 §11: signed domain,
chain, vault, pool, coldkey, relayer, fee cap, minimum net amount, deadline,
nonce; fee paid into the relayer's claim credit rather than transferred;
below-floor treatment; batch atomicity for proof verification and per-leaf
deferral for transfers.

Admission and rollout gates. Accepted. Any validator configuration change
requires a new signed production approval because the approval binds the
config hash and only aggregate bounds are revisable
(`validator/production_capacity.go:85-94,117-120,158-170`); the design says
so instead of assuming the old approval covers schema v3, and it needs no
change to the fixed `max_providers` dimension. Epoch-number deadlines are
replaced by gates (Part 2 §14), and the bytecode gate is met by not
upgrading the coordinator.

### Alternate approaches and the recommended combination

The review's decomposition into earned usage, quality measurement and
settlement is adopted as the architecture's spine. On the options:

- A (usage-based allocation, sampled operator quality, cumulative claims):
  adopted as the first release. Its stated weakness, that operator-level
  quality does not punish a poor provider directly, is accepted: the operator
  already routes demand away from poor providers for its own reasons, and the
  subnet's per-provider lever returns in phase C.
- B (precommitted service evidence with retrospective random audits): adopted
  now for usage evidence (Part 2 §7), where the committed signal already
  exists (receipts, report chains, reservations), and specified for a
  per-provider reliability signal in phase C, where it does not yet.
- C (non-expiring per-epoch roots with multi-epoch claims): not adopted as the
  primary design; cumulative roots are better for long accrual. Its property
  that a finalized allocation cannot be erased is obtained differently: a
  pending root is challengeable, and the vault retains the last four finalized
  roots so claims against them remain possible.
- D (enforceable admission or disputes): adopted in its cheapest form now
  because the 48-hour window between commit and finalization already exists.
  Objective decrease proofs are permissionless; the veto is a quorum of one
  activated validator at launch (the owner's) and becomes a policy fraction as
  validators join. Bonds and an exit delay are phase C, with the contract hook
  (`STSettlement2` is the natural holder) left in place now.
- E (validity proofs): deferred; the measured verifier at 1M providers is
  seconds of CPU and about 170 MiB of memory, and the audited part is bounded
  by a signed budget.
- F (cheaper settlement domain or native batch transfer): a native batch stake
  transfer is worth requesting upstream and would lower the per-recipient
  floor; v2 does not depend on it.

The review's latency table is confirmed: at 100k providers an average
entitlement equals one claim's gas after about 9.2 weeks and is 10× it after
about 92 weeks; at 1M the figures are 92 weeks and 917 weeks. These numbers
are the reason v2 does not promise weekly economic payment to every provider;
it promises correct, non-expiring accrual, batched and relayer-assisted
claims, and a published claim threshold (Part 2 §6.4).

### Suggested qualification sequence

Accepted in order and content, with one change of emphasis. Step 1 (threat
model, canonical formula, resource classes, latency target, cohort
transcript) is Part 2. Step 2 (million-provider replay fixtures including
originals, skewed strata, churn, dormant balances, shared coldkeys, with
end-to-end time, memory, disk, network and proof-service measurement) and
step 3 (vault conservation and successor failures, dual-vault routing, dual
evidence domains, late v1 carry, unpaid credits, a missed first v2 root,
compatible tooling and deployable bytecode before selecting the switch epoch)
are the phase A gates. Step 4 (Null with a full-size row, head coverage,
dividend attribution and a demonstrated return path) is the separate Null
track. The change of emphasis: because the stopgap pays only a closed set of
at most 2,000 providers, the gates are not allowed to drift into an open-ended
schedule; Part 2 §15 gives target epochs as estimates with the gate that each
depends on, and names what is deferred to make phase A small.

## Part 2. Version-2 design

### 1. The problem and the binding limits

The launched protocol scores every provider a validator ever touched,
publishes one Merkle leaf per provider with its proof inside one JSON
artifact, divides the pool into 10,000 basis points, and pays each provider
by its own claim transaction. Each is linear in the provider count and most
are hard-bounded.

| Limit | Where | Binds at | Failure |
| --- | --- | --- | --- |
| Validator census, signed `max_providers` 4,096 | `validator/config_evidence_v2.go:35`; fixed dimension `validator/production_capacity.go:85-94,117-120`; union `validator/measurement_stats_v2.go:42-73` | about 20 minutes of uniform sampling at 100k (30 seeds/min × 7 hops) | measurement error, run exits (`validator/release_run.go:1136-1138`), restart into the same persisted census (`validator/stats.go:178-191`); no pool weight |
| Settlement transition, `max_transition_bytes` 2 MiB | `validator/attempt_transition_v2_verify.go:137,170-173` | about 5,000 providers at 414 B each | transition refused |
| Share granularity, 10,000 bps | `evm/src/STSettlementVault.sol:17,373,383`; `protocol/payout.go:102-126` | 10,000 coldkeys | every further coldkey gets a 0-bps leaf the vault rejects; losers chosen by client id |
| Payout artifact, 32 MiB | `validator/artifact.go:29,271`; `server/startifact/artifact.go:42,95-97` | about 15,000 providers (2,281 B/provider at 20k) | validator cannot audit, server cannot serve claims |
| Closed-work census, 32,768 records and 8 MiB | `payoutartifact/closed_work.go:22-25` | one closed contract per provider at 32k providers | evidence refused |
| Reliability as payout input | `payoutartifact/artifact.go:157`; `protocol/payout.go:64`; `mainnet/economic_conservation_provider_measurements.go:165` | any unmeasured provider | zero reliability, excluded; v1 cannot pay an unmeasured provider under any configuration |
| One claim per provider per epoch | `evm/src/STSettlementVault.sol:365-390` | economic | 100k weekly claims cost about 9× the provider allocation, 1M about 92× |

The stopgap (`server/model/verify_cohort_model.go`; defaults
`server/controller/verify_controller.go:289-290`) keeps the validator inside
the first two bounds and, by the eligibility rule at
`server/controller/st_controller.go:3329-3331`, restricts payment to a closed
set of at most 2,000 providers for the life of the deployment. It is the
reason version 2 is urgent, and the last row of the table is the reason
nothing short of a new artifact schema and vault can pay the rest.

Economics, live. UIDs 170 and 250 receive about 73.8 α each per 360-block
tempo, so the miner allocation is about 20,700 α per 50,400-block epoch and
the provider share (momentum 1/10) about 2,070 α ≈ 13.6 TAO at 0.006587
TAO/α. The average weekly entitlement is 0.0207 α at 100k providers and
0.00207 α at 1M; the vault's minimum transfer (100,000 rao TAO-equivalent,
`evm/src/STSettlementVault.sol:53,271,460`) is 0.0152 α at that price. With
the head at θ = 3/10 the pool is 0.7 of the above.

### 2. Requirements

1. Scale: at least 1,000,000 providers per operator. Validator memory and
   transition bytes are bounded by signed cohort and head budgets, not by the
   population. Per-epoch validator work is bounded by signed budgets: O(N)
   streaming verification of the ledger (measured at seconds and about
   170 MiB at 1M) plus a signed `max_audit_bytes` of evidence sampling; a
   validator may verify all evidence if it has the capacity. This restates
   VERSION2.md requirement 1 honestly (F4, F10).
2. Security model: validators compute operator quality from their own trails;
   every payout input is committed, retained, hash-anchored and reproducible;
   operators direct the split but never hold funds (`WHITEPAPER.md:924-925`);
   an invalid ledger cannot overdraw escrow or another pool and cannot become
   authoritative without passing the pending window; the treasury row
   (45/45/10) and θ are unchanged (`validator/TREASURY-PRODUCTION.md:17`;
   `validator/treasury_policy.go:50-51`).
3. Bounded mandatory on-chain cost: O(operators) transactions per epoch.
   Claim cost proportional to claims made, each amortizing arbitrary accrual.
4. No entitlement expires. Claimable from finalization (epoch end + 14,400
   blocks) as today.
5. Every provider with verified usage and a payout coldkey that is not a bound
   head member is paid pro rata to usage, measured or not.
6. Migration with a conservation table; residue, if any, quantified and
   accepted explicitly.
7. Null consensus and UID expansion are independent of 1-6.

### 3. Threat model

Actors: the operator (server, root signer, artifact signer); providers
(including fleets with shared coldkeys); the validator set (one at launch,
the owner's UID 1, with the signed approvals); relayers; the owner Safe;
the chain and its runtime.

Assets: captured pool α in escrow; each coldkey's cumulative entitlement and
unpaid credit; the validator's weights; the evidence record.

Assumptions. Yuma: a stake majority of permitted validators runs the signed
policy (`WHITEPAPER.md:1024-1026`); at launch the sole validator is the
owner's, so the veto is a quorum of one. Operator: untrusted for allocation
and evidence, trusted to serve its own customers; it necessarily learns which
providers are probed online, so measured quality is an upper bound on
population quality under an adversarial operator (F1). Chain: finalized block
hashes are unpredictable to the operator at commit time and unbiasable by
validators, who do not produce blocks. Runtime: the stake precompiles and
`transferStake` behave as reviewed for runtime 475; every runtime change stops
automated writes until revalidated (`evm/README.md:184-186`).

| Attack | Mitigation |
| --- | --- |
| Operator inflates a provider's usage or invents providers | usage must reproduce from committed receipts, reports, reservations and wallet authority; chunks are audited by a post-commit beacon, usage-weighted with a coverage floor; a finding vetoes the root; phase C bond |
| Operator moves usage between providers, total unchanged | per-row join in audited chunks (§7) |
| Operator lowers or omits a coldkey's cumulative | leaf set is monotone; decrease provable on chain by anyone during the pending window; omission and sum errors found by the validator's streaming transition audit and vetoed (§9.2, §10) |
| Operator over-allocates (Σ leaves > cumulative total) | validator audit and veto; on chain `credited ≤ finalizedTotal` bounds damage to the pool's own funds (§9.1) |
| Operator equivocates on the sampling frame or assigns a non-member | validator holds the whole frame; any mismatch is a finding (§5.2) |
| Operator favours probed members | accepted residual; quality estimate is an upper bound; weights, not payouts, depend on it |
| Provider tampers with probes | unchanged: trails, poison trails and padding (`server/model/verify_model.go:650-662`) |
| Validator cherry-picks a cohort | seed is a VRF of a finalized block hash with a published proof; anyone recomputes the draw (§5.3) |
| Validator griefs by false veto | funding carries, nothing is lost; the operator re-finalizes next epoch; veto quorum becomes a policy fraction as validators join |
| Relayer steals a claim | payment lands on the leaf's coldkey; permits bound the fee and name the relayer (§11) |
| Replay across vaults or domains | evidence domains and vault addresses are inside every signed payload; permits bind chain, vault and pool |
| Stake-rank capture under Null | out of scope for v2; Null track (§13) |

### 4. Architecture

```
 server (operator)                      validator (UID 1)                      chain 964
 ─────────────────                      ─────────────────                      ─────────
 per settlement epoch e:
  frame F_e: sorted eligible client ids  download F_e (17 B/row), check
  + stratum labels, root in first        uniqueness and labels
  attempt-ledger record                  seed s = VRF(hotkey, H(finalized hash
                                         at epoch start ‖ e ‖ no_id)); publish
                                         proof; draw cohort C_e by stratum,
                                         without replacement
 assign requested member F_e[i] or  ◄──  request hops by index; replacements
 answer "unavailable" (signed)           from the VRF stream, budget R
 record assignments/confirmations        per-member window for C_e only;
                                         head hops by bound client id;
                                         Q̂_n stratified, interval, scalar EMA
 epoch end:
  closeOperatorEpoch(e) ──────────────────────────────────────────────────────► STSettlement2 → vault2.captureEmission
  ledger v2: provider chunks, leaf                                              pool hotkey v2 → escrow
  chunks (cumulative per coldkey),
  evidence chunks, header, sign
  commitDistribution(e, root, ledgerHash,
    cumulativeTotal, leafCount) ≤ end+1,200 ────────────────────────────────► pending root
                                         fetch chunks; streaming transition
                                         audit vs previous ledger; funding
                                         equality; audit sampled evidence by
                                         beacon = finalized hash(end+1,201)
                                         LEDGER_AUDIT evidence (ok | veto) ──► STValidatorEvidence2
                                         anyone: challengeDecrease(...) ─────► STSettlement2
  finalizeDistribution(e) ≥ end+14,400 ───────────────────────────────────► if no veto/challenge: vault2 latest;
                                                                               else funding carried
 GET /sn/ledger/claim → (noId, coldkey, cumulativeRao, proof, vault, root)
 provider / relayer: claim | claimBatch | claimWithPermit ─────────────────► vault2: credit cumulative − paid,
                                                                               transfer when ≥ floor, else defer
```

### 5. Measurement

#### 5.1 What measurement is for

Measurement produces, per operator, a quality estimate with an interval for
cross-operator pool weighting and gating, a head breadth score as today
(`WHITEPAPER.md:949-955`), and audits. It produces no per-provider payout
factor in this release (F1, F5). With one pool the estimate changes no weight
(`validator/release_math.go:31-66`); its first use is the next operator.

#### 5.2 The sampling frame

At each settlement epoch start the server snapshots its eligible provider set
(`verify_eligible_v2` members with a live egress and a provide mode, the
same conditions as `verifyCandidateAssignable`) into the frame `F_e`: rows of
`client_id(16) ‖ stratum(1)` sorted by client id, at most 8 strata, with
per-stratum counts `n_s` and `N = Σ n_s`. The frame is a chunked, content-
addressed artifact (≤ 8 MiB chunks); its root and counts are in the first
attempt-ledger record of the epoch, signed by the server key the validator
already authenticates (`validator/trail.go:170-173`). Providers joining
mid-epoch are not in the frame until the next epoch and fall in the "new"
stratum then. Two different frames for one epoch, or a frame whose counts
differ from its rows, is a finding.

Strata (policy v2): tenure {new (first epoch in any frame), established} ×
address family {IPv4, IPv6}. The operator labels; the validator verifies
labels for every measured member (first-seen epoch from client-key history,
family from observed egress) and treats a mislabel as a finding.

#### 5.3 Seed, proof and draw

`s_e = VRF_sk(H("urnetwork/cohort-seed/v2" ‖ finalized_native_hash(epoch_start_block) ‖ e ‖ no_id))`
with the validator hotkey's sr25519 key (schnorrkel VRF), proof published in
the sealed measurement with the frame root. Inputs are already pinned in the
measurement envelope (`validator/release_measurement_envelope.go:52-57`).
Draw for stratum s: `i_k = PRF(s_e, s, k) mod n_s` for k = 0, 1, ...,
skipping any index already drawn, until `m_s` distinct indices; the ordered
list of drawn indices is the stratum's replacement stream. Anyone with the
frame, the proof and the policy recomputes the cohort.

Apportionment of `m = 3,072` across strata: weights `√n_s`; floor `q_min = 128`;
cap `n_s`; Hamilton (largest remainder) on the uncapped strata, then
redistribute any shortfall caused by caps by repeating on the remaining
strata; deterministic; the algorithm and vectors ship in `protocol`.

#### 5.4 Assignment, replacement and bounds

The validator requests hops as `(e, s, k)`; the server assigns `F_e[s][i_k]`
if it is connected and token-eligible, otherwise returns a signed
"unavailable" for that index. The validator records the assignment under the
member's client id and checks it equals the frame entry; a mismatch is a
finding. An unavailable member is replaced by the next index in the stratum's
stream; replacements are limited to `R = m/3 = 1,024` per epoch across strata.
When R is exhausted, the stratum's `m_eff,s` falls and the estimate widens;
no further members join. The server's unavailable rate per stratum is
published; above the policy threshold it is a finding, because an operator
can otherwise steer the sample by declaring good members unavailable.

Assignment budget: 30 seeds/min × 7 hops × 10,080 min ≈ 2.1 M per epoch
(`validator/release_run.go:450-462`; `deploy/mainnet/policy-v1.yml:93,105`),
about 500 per cohort member after the head budget (§5.8). `a_min` rises from
8 to 32 for the pool cohort (policy v2), reached by every member in the first
day at the current pace.

Bounds (validator config v3; all within the existing fixed dimensions):
active members ≤ m = 3,072 per operator; distinct measured per epoch
≤ m + R = 4,096 = `max_providers`; egress hashes ≤ 8 per measured provider
≤ 32,768 = `max_egress_hashes`; head entries per §5.8; summed over
`max_operators`. Per-provider windows exist only for the current epoch's
cohort and head set; no per-provider EMA is carried (K = 0). The settlement
transition at 414 B per provider is about 1.7 MB, under the 2 MiB
`max_transition_bytes`. At activation, one recorded transition evicts
persisted `EmaPPM`/`Ema`/`Window` entries that are not current cohort or
head members.

#### 5.5 Estimator

Per measured member p: `q_p` = `ReliabilityPPM(confirmations, assignments, a_min)`
× latency factor as today (`validator/measurement_stats.go:125-128`), with
`assignments ≥ a_min`. Per stratum: `q̂_s` = mean of `q_p` over measured
members, `s_s²` their sample variance, `m_eff,s` their count. Operator
estimate: `Q̂_n = Σ_s (n_s/N) q̂_s`; variance
`V = Σ_s (n_s/N)² (1 − m_eff,s/n_s) s_s²/m_eff,s`; interval `Q̂_n ± 1.96√V`.
Estimand: provider-mean quality over `F_e`. Policy option
`estimand: usage_weighted` replaces `n_s/N` by stratum usage shares read from
the finalized ledger of the same epoch (a ratio estimator, published with its
linearized variance). Nonresponse: if `m_eff,s < q_min/2` the stratum uses
its previous `q̂_s` with variance multiplied by `nonresponse_widening`
(policy, default 4); if every stratum is below, the measurement reports
quality unavailable and the operator-level EMA holds. Operator EMA:
`Q̄_n,e = α Q̂_n,e + (1−α) Q̄_n,e−1`, α = 1/4, one scalar per operator. The
clamp [0.75, 1.0] and the implied-demand multiplication are unchanged
(`validator/release_math.go:148-157`; `validator/release_measurement.go:719-733`).
The measurement publishes `N`, `n_s`, `m_s`, `m_eff,s`, `q̂_s`, `s_s²`, `V`,
replacements used and unavailable counts.

#### 5.6 Verification of a measurement

A verifier of another validator's sealed measurement checks: the frame root
against the server's signed record; the VRF proof; the drawn indices; that
every recorded assignment is the frame member at its index; the apportionment;
the per-member statistics against the attempt ledger as today; the estimator
arithmetic. A transition whose cohort does not match is refused.

#### 5.7 Churn

Members that disconnect keep their window for the epoch; their partial
assignments count if `≥ a_min`, otherwise they are nonresponse, not
replaced beyond R. A provider measured in epoch e and drawn again in e+1
starts a fresh window; no quality is inherited. This is what bounds the
validator's state without a lifetime set.

#### 5.8 Head measurement

The head is measured on its own budget. The head set `H_e` is every client id
with an active fleet binding at the epoch start, read from the coordinator's
binding state (`evm/src/STCoordinator.sol:183-193,724-794`); its size is
bounded by `maximum_head_fleets × max_members_per_fleet` (policy v2; 200 × 8
today, 2,000 × 8 under the Null track). The validator requests hops by client
id for `H_e` members, round-robin, until each has `a_min_head` assignments
(policy v2, default 16), spending at most `head_assignment_share` (default
10%) of the epoch budget; the existing head projection consumes these trails
(`validator/attempt_cut_v2_head.go:75-100`), and `CurrentBindingKVs` is bounded
by `|H_e|` instead of `max_providers` (`:51-52`). Head members are excluded
from the pool frame.

### 6. Usage ledger and allocation

#### 6.1 Eligibility and usage

A provider row is eligible when it has verified usage in the earning window,
a payout coldkey, and no active head binding for the epoch
(`server/controller/st_controller.go:3315-3328` minus the `a_min` clause).
Usage is the closed-work census projection reconciled through the existing
authority paths: `stLoadProviderWorkAuthority`, `stProviderWorkUsages`,
wallet authority and `BindingsAt` (`:3383-3421`), with head exclusion from
an event-indexed binding set rather than one `bindingAt` per provider
(`VERSION2.md` §5.6 stands). The `reliability_exposure_floor` exclusion
reason disappears.

#### 6.2 Ledger v2

Schema `urnetwork-payout-ledger-v2`. Header (JSON, ≤ 64 KiB):

- identity: `deployment_id`, `chain_id`, `genesis_hash`, `netuid`,
  `settlement` (STSettlement2), `vault` (vault2), `epoch`, `no_id`,
  `policy_hash`, `start`/`end` boundaries, `earning_policy_sha256`,
  `window_start_utc`/`window_end_utc`;
- continuity: `previous_root`, `previous_total_rao`, `previous_leaf_count`
  (zero for the operator's first v2 epoch), `epoch_funding_rao`,
  `cumulative_total_rao = previous_total_rao + epoch_funding_rao`,
  `leaf_count ≥ previous_leaf_count`, `cumulative_root`;
- totals: `total_usage_bytes`, `eligible_usage_bytes`, `excluded_usage_bytes`,
  `total_users`;
- `frame_root`, `frame_counts[]`;
- chunk indexes, each entry `{index, sha256, bytes, first_key, last_key, rows, usage_bytes | delta_rao}`:
  `provider_chunks[]` (keyed by client id), `leaf_chunks[]` (keyed by
  coldkey), `evidence_chunks[]` (closed-work originals keyed by client-id
  range, aligned with `provider_chunks`), `whole_inventory_chunks[]`;
- `signer`, `content_hash`, `signature`.

Provider rows, binary, 73 B: `client_id(16) ‖ network_id(16) ‖ coldkey(32) ‖ usage_bytes(8) ‖ flags(1)`
(eligible, head_excluded, missing_wallet, binding generation present). Leaf
rows, binary, 56 B: `coldkey(32) ‖ cumulative_rao(16) ‖ delta_rao(8)`, sorted
by coldkey. Evidence rows: the existing `ClosedWorkRecord` JSON, one record
per closed contract, sorted by `(client_id, contract_id)`, chunked at 8 MiB
with the per-record 1 MiB cap retained; the whole-work inventory likewise.
Sizes at 1M providers: provider rows 70 MiB, leaf rows 53 MiB, evidence
proportional to work (about 500 B per closed contract). At 100k: 7 and 5 MiB.

Byte bounds (validator config v3, aggregate and revisable): `max_ledger_bytes`
(default 1 GiB) for provider and leaf chunks together, `max_evidence_bytes`
(default 16 GiB) for committed evidence, `max_audit_bytes` (default 512 MiB)
for the evidence a validator fetches per epoch, `max_chunk_bytes` 8 MiB. The
32 MiB caps (`validator/artifact.go:29`; `server/startifact/artifact.go:42`)
apply to v1 artifacts only.

#### 6.3 Allocation

`F` = `epoch_funding_rao`. Aggregate eligible usage by coldkey:
`W_c = Σ_{p: coldkey(p)=c, eligible} usage_p`, `ΣW = Σ_c W_c`. If `ΣW = 0`
the operator defers the epoch (§9.2) and no root is committed. Otherwise
`delta_c = floor(F × W_c / ΣW)`, then `F − Σ_c delta_c` (fewer than the
number of coldkeys with `W_c > 0`) rao are added one each to the coldkeys
with the largest `F × W_c mod ΣW`, ties broken by ascending coldkey, so
`Σ delta_c = F` exactly. Every coldkey present in the previous ledger appears
with `delta_c = 0` if it earned nothing; `cumulative_c = previous_c + delta_c`.
Guarantee: a coldkey with `W_c ≥ ΣW/F` receives at least one rao; below that
the remainder rule decides deterministically. A wallet change creates a new
coldkey leaf for future deltas and leaves the old leaf's cumulative intact;
balances are never moved between leaves.

#### 6.4 Payout latency and the claim threshold

Entitlements accrue weekly and never expire. The operator publishes a claim
policy with the ledger: a suggested minimum claim (default: the amount at
which the EVM part of a claim, measured below, is at most 10% of the amount
at the current base fee), the operator's sweep relayer address and its fee
cap. Daemons claim when the threshold is met or on demand. The arithmetic of
§1 is the stated consequence: weekly economic payment for every provider is
not a property any on-chain α settlement provides at these sizes.

### 7. Evidence chunking, byte budgets and retrospective audits

Commitment. The ledger header, with every chunk hash, is bound on chain by
`commitDistribution` (`ledgerHash = sha256(header bytes)`) no later than
`end + 1,200` blocks. Chunks are content-addressed in the operator's store and
served by `/sn/ledger/chunk?hash=`; the validator retains what it fetched in
its durable custody as today.

Beacon. `b_e = finalized_native_hash(end + root_commit_window + 1)`, read at
finality; unknowable to the operator at commit time and unbiasable by
validators.

Full checks (every validator, O(N)): header signature and identity; chunk
hashes; provider rows sorted and unique; leaf rows sorted and unique; the
tree root over leaf rows (§8); `Σ delta = epoch_funding_rao`;
`Σ cumulative = cumulative_total_rao`; continuity against the previous
ledger by streaming merge (every previous coldkey present, `cumulative_new = cumulative_old + delta`,
`delta ≥ 0`); allocation recomputed from provider rows (two passes: aggregate
by coldkey, then floors and remainders; coldkey aggregation uses a sorted
external merge when the coldkey count exceeds memory, bounded by
`max_ledger_bytes`); head exclusion against the binding set; frame
consistency (`eligible_usage_bytes` only from frame members or members who
joined after the frame, flagged); `epoch_funding_rao` equal to the vault's
unfinalized funding for the pool.

Sampled checks (each validator, bounded by `max_audit_bytes`): select
evidence chunks by `PRF(b_e, e, no_id, j)` with probability proportional to
the chunk's `usage_bytes`, plus a coverage floor so each chunk is selected at
least once every `coverage_epochs` (default 4); for every selected chunk,
replay the originals through `VerifyClosedWork`, `VerifyClosedWorkReports`,
reservation, service-party and wallet-authority verification exactly as the
v1 reader does for the whole census, and check that each provider row's
`usage_bytes` in the aligned provider chunk equals the verified receipt sum.
Any failure is a finding; findings are published in the `LEDGER_AUDIT`
evidence record and veto the root (§10).

Operator deposit audit (`validator/release_decision_history_v2.go:375`;
`EvaluateDepositArtifact`) reads `total_usage_bytes` and `total_users` from
the ledger header instead of the artifact; unchanged otherwise.

### 8. Merkle construction

Tree `urnetwork-ledger-tree-v2`: leaf `L_i = keccak256(bytes.concat(keccak256(abi.encode(bytes32 coldkey_i, uint256 cumulativeRao_i))))`
for leaf rows in ascending coldkey order; `n ≥ 1`; node array of `2n − 1`
with `nodes[2n−2−i] = L_i` and `nodes[k] = keccak256(sorted(nodes[2k+1], nodes[2k+2]))`
for `k` from `n − 2` down to 0 (the OpenZeppelin StandardMerkleTree layout);
root `nodes[0]`; a single leaf is its own root. Duplicate coldkeys are
forbidden. Single proof for leaf i: siblings along the path from `2n−2−i` to
the root; verified by `MerkleProof.verify` and by this repository's
`merkle.Verify`, both shape-blind sorted-pair folds. Multiproof: OpenZeppelin
`getMultiProof` semantics; leaves are supplied in descending node index,
which is ascending ledger order; verified by `multiProofVerifyCalldata`
(OpenZeppelin 5.6.1 `MerkleProof.sol:350-357`). Empty multiproofs are
rejected by the vault (`leaves.length == 0` reverts).

Measured (program `v2measure`, this machine): 1M leaves build in 0.41 s after
1.25 s of sorting and hashing, 61 MiB node array, 168 MiB peak heap with the
ledger; proofs 19-20 nodes, 9 µs each; batches of 8/64/256 need 132/839/2,820
proof nodes against 160/1,280/5,120 for separate proofs. At 100k: 42 ms,
6 MiB, depth 16-17. The server retains the node array per finalized epoch for
proof service and regenerates it from leaf chunks if lost. Cross-language
vectors: generated with `@openzeppelin/merkle-tree` as `merkle/testdata/gen_oz_fixtures.mjs`
does today, plus Go and Solidity multiproof vectors for batches of 1, 2, 8, 64
and the empty-set rejection.

### 9. Contracts

Three new contracts; the v1 coordinator, vault and evidence contracts are not
modified or upgraded.

#### 9.1 `STSettlementVault2` (immutable custody)

State: `pools[noId]` (hotkey, uid, active) as v1; per pool `captured`,
`finalizedTotal`, `credited`, `unfinalizedFunding`, `nextEpoch`; a ring
`history[noId][4]` of `Distribution{root, ledgerHash, cumulativeTotal, leafCount, epoch, finalizedBlock}`,
latest first; `paidRao[noId][coldkey]`; `claimCredit[coldkey]`;
`permitNonce[noId][coldkey]`; totals `totalCaptured`, `totalPaid`,
`escrowAccounted`. Immutables as v1 (`netuid`, `escrowHotkey`, `selfColdkey`,
`minimumTransferTaoRao`, `bootstrap`); `setCoordinatorOnce` names
`STSettlement2`.

Functions (coordinator-only unless stated):

- `captureEmission(epoch, noId)`: as v1 (`evm/src/STSettlementVault.sol:251-307`),
  including the below-minimum dust deferral; adds to `captured[noId]` and
  `unfinalizedFunding[noId]`.
- `deferEmission(epoch, noId)`: as v1.
- `finalizeDistribution(epoch, noId, root, ledgerHash, cumulativeTotal, leafCount)`:
  requires `epoch == nextEpoch[noId]`, `root != 0`,
  `cumulativeTotal == finalizedTotal[noId] + unfinalizedFunding[noId]`,
  `leafCount ≥ history[noId][0].leafCount`; moves `unfinalizedFunding` into
  `finalizedTotal`, pushes the distribution, `nextEpoch = epoch + 1`.
- `markDistributionMissed(epoch, noId)`: requires `epoch == nextEpoch[noId]`;
  leaves funding unfinalized for the next distribution; `nextEpoch = epoch + 1`.
- `claim(noId, coldkey, cumulativeRao, rootIndex, proof)` (anyone): the leaf
  must verify against `history[noId][rootIndex]`; `amount = cumulativeRao − paidRao`
  must be positive; `credited[noId] + amount ≤ finalizedTotal[noId]`;
  `paidRao = cumulativeRao`; `claimCredit[coldkey] += amount`;
  `credited += amount`; then `_settleClaimCredit(coldkey, deferOnFailure = true)`
  as v1 (`:446-482`). Claiming against an older ring entry pays less, never
  more, so it is safe.
- `claimBatch(noId, coldkeys[], cumulativeRao[], rootIndex, proof[], flags[])`
  (anyone): multiproof over all leaves (atomic: one bad leaf reverts), then
  per leaf the same credit rule, skipping leaves with nothing new to credit
  when `skipPaid` is set, and attempting a deferrable transfer per coldkey.
- `claimWithPermit(noId, coldkey, cumulativeRao, rootIndex, proof, permit, signature)`:
  §11.
- `withdrawClaimCredit(coldkey)`: as v1.
- `captureEscrowSurplus()` (anyone): `surplus = liveEscrowStake − escrowAccounted`;
  distributes it to `unfinalizedFunding[noId]` pro rata to
  `finalizedTotal[noId] − credited[noId] + unfinalizedFunding[noId]`
  (outstanding liability), remainder to the lowest `noId`; exists for Null
  dividends (§13) and is a no-op under Yuma.

Conservation: `totalCaptured == totalPaid + escrowAccounted` and
`escrowAccounted == Σ_noId unfinalizedFunding + Σ_noId (finalizedTotal − credited) + Σ_coldkey claimCredit`
and `liveEscrowStake ≥ escrowAccounted`, checked after every state change as
v1 does (`:513-515`).

Gas (probe `V2Probe.t.sol` on the scratch copy, no runtime transfer): single
claim with a depth-20 proof 81,555 gas with cold storage (proof verification
7,598); batches of 8 and 64 leaves 65,609 and 60,009 gas per leaf, dominated
by two cold storage writes per leaf. A repeat claim on a warm `paidRao` slot
is about 20k less. The per-recipient `transferStake` is unmeasured and
inherent; planning figure for a paid single claim stays 250k until measured
on the first finalized v2 epoch.

#### 9.2 `STSettlement2` (v2 settlement coordinator, owner-upgradeable UUPS, small)

Immutables/initialization: `coordinator` (the v1 proxy, for `operatorAt`,
`policyAt`, `epochStartBlock`, `epochEndBlock`, `currentEpoch`), `vault`
(vault2), `vault1`, `switchEpoch E_s`, `validatorEvidence` (fixed once).

- `registerOperatorPool(noId, poolHotkey, maximumBurnRao)` (owner, payable):
  requires an active operator version in the v1 proxy; `vault.registerPool`.
- `settlementAt(noId, epoch) → (vault, poolHotkey, schema, evidence)`: for
  `epoch < E_s` returns `(vault1, coordinator.operatorAt(noId, epoch).poolHotkey, 1, coordinator.validatorEvidence())`;
  otherwise `(vault, poolHotkeyV2[noId], 2, validatorEvidence)`.
- `closeOperatorEpoch(epoch, noId)`: `epoch ≥ E_s`; window
  `[end, end + closeGraceBlocks]` from the v1 policy; `vault.captureEmission`.
- `deferMissedEmission(epoch, noId)`: as the v1 proxy's.
- `commitDistribution(epoch, noId, root, ledgerHash, cumulativeTotal, leafCount)`:
  by `operatorAt(noId, epoch).rootSigner`; once; within
  `[end, end + rootCommitWindowBlocks]`; stores the pending root.
- `deferDistribution(epoch, noId)`: by the root signer within the commit
  window when `ΣW = 0`; marks the epoch to be finalized as missed.
- `challengeDecrease(epoch, noId, coldkey, oldCumulative, oldRootIndex, oldProof, newCumulative, newProof)`
  (anyone, pending window): verifies `oldProof` against a finalized ring root
  and `newProof` against the pending root for the same coldkey with
  `newCumulative < oldCumulative`; marks the pending root challenged; emits.
- `finalizeDistribution(epoch, noId)` (anyone, `block.number ≥ end + finalizeOffsetBlocks`):
  if a pending root exists, is not challenged and
  `STValidatorEvidence2.vetoCount(epoch, noId) == 0`, calls
  `vault.finalizeDistribution`; otherwise `vault.markDistributionMissed`.
  Epochs finalize in order through the vault's `nextEpoch`.
- Operator bond hooks (phase C): `bond[noId]`, `slash(...)` by the owner on a
  replayable finding; present as storage and events, inert until policy
  enables them.

#### 9.3 `STValidatorEvidence2`

Constructed with `(STSettlement2, genesisHash, deploymentIdHash)`; immutables
`settlement`, `coordinator = settlement.coordinator()`, `settlementVault = settlement.vault()`,
`chainId`, `netuid`. Library `ValidatorEvidence2` with domains
`urnetwork/validator-evidence-header/v2`, `/slot/v2`, `/audit-subject/v2`,
`/activation/v2`; kinds `CLOSED_CENSUS = 1`, `DEPOSIT_AUDIT = 2`,
`LEDGER_AUDIT = 3` with `Subject{observationEpoch, nativeEpoch, verdict}`
(`verdict` 1 = ok, 2 = veto) and the same dual-signature verification
(`evm/src/lib/ValidatorEvidence.sol:203-220`). `anchored` checks
`settlement.validatorEvidence() == address(this)`. `vetoCount(epoch, noId)`
counts `LEDGER_AUDIT` commitments with `verdict = veto` whose `observationEpoch = epoch`
by hotkeys with a published v2 activation; with `veto_quorum` in policy v2 the
coordinator compares the count to the activated-validator count. Activation
records name vault2; the v1 companion keeps serving epochs before `E_s`.

#### 9.4 Identity for readers

Every reader resolves identities through `settlementAt`: the validator's
decision chain for the weight destination and the vault to audit
(`validator/release_decision_chain_v2.go:380-412` gains the call for
`epoch ≥ E_s`); the server's claim endpoint returns `settlement_vault_address`,
`claim_schema` and `root_index` per epoch (`server/controller/sn_controller.go:195-208,293-297`);
the claim daemon switches calldata on `claim_schema`; historical replay and
economic conservation read the schema per epoch; signed configurations carry
`settlement2`, `vault2`, `evidence2` and `E_s`.

### 10. Challenge timing and remedies

Timeline per epoch e (blocks after `end`): 0-120 close; ≤ 1,200 commit (root,
ledger hash, totals bound on chain); 1,201 beacon available; 1,200-14,400
pending window: validators audit and publish `LEDGER_AUDIT`, anyone may submit
`challengeDecrease`; ≥ 14,400 finalize or miss. A missed or vetoed epoch
leaves its funding unfinalized; the next commit must include it
(`cumulativeTotal == finalizedTotal + unfinalizedFunding`), so providers lose
time, not value. Weight remedy: a validator that published a veto, or
observed a challenged root, computes zero pool weight for the operator from
its next decision until the operator's next clean finalized epoch, replacing
the `zero_pool_weight` discretion of `deploy/mainnet/policy-v1.yml:72-73`
with an evidence-anchored trigger. Profitability: fraud detected before
finalization pays nothing and costs the operator at least one epoch of pool
weight (about 2,070 α at launch); fraud missed by every validator's sample
pays at most one epoch's funding to coldkeys the operator chose, which is the
amount the phase C bond must exceed. A late finding (after finalization)
cannot undo the root; it still zeroes future weight and is recorded.

### 11. Permit and batch semantics

Permit `P = {chainId, vault, noId, coldkey, relayer, relayerColdkey, maxFeeRao, minNetRao, deadlineBlock, nonce}`;
message `sha256("urnetwork/claim-permit/v2" ‖ 0x00 ‖ abi.encodePacked(P))`;
signature sr25519 by `coldkey` verified through the 0x403 precompile
(`evm/src/interfaces/sr25519Verify.sol`). `relayer` is the EVM sender allowed
to use the permit and `relayerColdkey` the Substrate coldkey that receives the
fee; both are chosen by the relayer and shown to the signer, so no H160-to-
coldkey mapping is computed on chain. `claimWithPermit` requires
`msg.sender == relayer`, `block.number ≤ deadlineBlock`,
`nonce == permitNonce[noId][coldkey]` (then incremented), computes `amount` as
in `claim`, `fee = min(maxFeeRao, amount × max_relayer_fee_bps / 10,000)`
(policy cap), requires `amount − fee ≥ minNetRao`, credits `amount − fee` to
the coldkey and `fee` to `claimCredit[relayerColdkey]`, with no transfer for
the fee; the relayer withdraws its credit through `withdrawClaimCredit`. Credits below the runtime
floor remain credit for either party. Batches: the multiproof is verified
once and atomically; per-leaf credit creation is atomic with it; per-leaf
transfers are attempted with deferral, so one failing runtime transfer never
reverts the batch; a leaf with nothing new to credit reverts the batch unless
`skipPaid` is set, in which case it is skipped and emitted. Default daemons
sign permits only for the operator's published sweep relayer.

#### 11.1 Miner app support for cumulative claims

Every miner-facing app that displays rewards or submits claims must support
v2 cumulative claims before phase A activation. The app presents one accrued
balance per payout coldkey, operator and vault, including earnings from all
finalized v2 epochs. It obtains a proof against a currently accepted finalized
root; claiming accumulated earnings does not require a claim or proof for
each earning epoch. Epoch history remains available as an earnings breakdown,
not as separate v2 claim tasks.

The reward view distinguishes pending earnings, finalized cumulative
entitlement, newly claimable amount, accepted but unpaid claim credit, and
confirmed transfers. Newly claimable amount is
`max(0, cumulativeRao - paidRao[noId][coldkey])`. `paidRao` is a credit
watermark, not proof of a successful runtime transfer. Existing
`claimCredit[coldkey]` is vault-wide and must be displayed once, not counted
again for each operator or included in a new cumulative claim. A deferred
transfer is shown as unpaid credit with a withdrawal action, not as received
funds. Missing evidence or an unavailable proof is an explicit status, not a
zero earnings balance.

Before submission, the app refreshes the finalized root, verifies the proof,
reads the watermark and credit, and shows the expected claim increment, gas
or relayer fee, net credit, and transfer-minimum status. A newer root or a
claim by another client triggers refresh; submission and transfer results are
reconciled from finalized chain state. An on-demand claim remains available
when supported, alongside an optional economic threshold; balances below the
runtime transfer minimum are described as accruing or credited, without a
promise of immediate payment. Relayer permits show their fee cap, minimum net
amount, recipient, relayer, expiry and chain/vault identity before signing.

Apps resolve `claim_schema` and settlement identity for migration, retain v1
claim and credit-withdrawal support, and display v1 and v2 balances separately.
They do not combine credits across vaults to satisfy a transfer minimum.
Changing the payout wallet does not move old accrued balances: the app retains
access to the old coldkey's claim history and identifies the wallet needed to
authorize any permit. These app requirements do not resolve the missed-epoch
allocation and root-admission issues identified in the meta review.

### 12. Migration

Switch epoch `E_s`: the first epoch whose pool is the v2 pool hotkey. Chosen
only after the gates of §14; not before the last v1 epoch `E_s − 1` has been
closed, committed and finalized on the v1 vault.

Sequence. (1) Deploy vault2, `STSettlement2`, `STValidatorEvidence2` with the
bootstrap-contracts pattern (`mainnet/BOOTSTRAP-CONTRACTS.md`); `registerEscrow`
(one burn), `setCoordinatorOnce(settlement2)`, fix the evidence pointer.
(2) `registerOperatorPool(1, poolHotkeyV2, burn)` at least one epoch before
`E_s` (new UID, immunity). (3) `schedulePolicy` on the v1 proxy with policy v2
effective at `E_s` (`evm/src/STCoordinator.sol:312-327`); re-sign the
validator production approval, the treasury approval (policy hash), the
server `st.yml`/`verify.yml` and the validator config with `settlement2`,
`vault2`, `evidence2`, `E_s`. (4) Validators publish v2 activations. (5) From
`E_s` the validator weights the v2 pool UID; the operator closes, commits and
finalizes epochs `≥ E_s` through `STSettlement2`. (6) Epoch `E_s − 1` is
closed and finalized on v1 as usual. (7) After epoch `E_s − 1 + 10` (the last
v1 expiry, `evm/src/STCoordinator.sol:641-645`) the operator runs the carry
sweep: `closeOperatorEpoch(E_sweep, 1)` on the v1 proxy for the then-current
epoch inside its close window (the v1 pool hotkey's observed stake, including
reveal-lag emission, is captured or dust-deferred), builds a v1 artifact that
allocates `carry + captured` to the v1 coldkeys pro rata to their expired
remainders, `commitOperatorRoot`, `finalizeOperatorEpoch`. (8) Record the
residue after that root's own expiry.

Conservation table at `E_s`:

| Bucket | Where | Action | Recipient |
| --- | --- | --- | --- |
| Uncaptured stake on v1 pool UID 169 after the last close | runtime stake | captured by the sweep close (or dust-deferred and written off) | v1 coldkeys via the sweep root |
| Funded, unfinalized v1 epochs | `pendingFunding` | finalize or mark missed before `E_s` | entitlement or carry |
| Finalized, unclaimed v1 entitlements | `_entitlements` | claimable until expiry; v1 claim path retained | v1 coldkeys |
| Accepted, unpaid v1 credits | `claimCredit` (vault1) | `withdrawClaimCredit` forever; daemon keeps a v1 withdraw command | v1 coldkeys |
| v1 carry (missed roots, expiries) | `carry[1]` | sweep epoch root | v1 coldkeys pro rata |
| Sweep root remainder after its expiry; sub-floor v1 credits | vault1 | residue; quantified exception to requirement 6 | none |
| Null surplus | vault1 `liveEscrowStake − escrowAccounted` | cannot be distributed by vault1; residue if Null is adopted before the sweep | none |

v1 claim credits and v2 claim credits are separate balances in separate
vaults and cannot combine to clear the floor; the daemon reports both.

### 13. Null consensus and UID expansion (separate track)

What Null changes: the largest-stake UID holds the only permit and its row is
paid as submitted; dividends go to every staked UID pro rata, including the
reserve UIDs, the escrow UIDs and the pool UIDs
(`subtensor/pallets/subtensor/src/epoch/run_epoch.rs:804-808,838-854,938-965,1335-1344`;
`subtensor/pallets/subtensor/src/coinbase/run_coinbase.rs:533-547`); the UID
budget is 2,500 shared across mechanisms (`subtensor/pallets/subtensor/src/subnets/mechanism.rs:35,101-121`).
Local blockers: the treasury approval's `maximum_subnet_uids` 256
(`validator/recycle_approval.go:48`; `validator/recycle_observation.go:199-206`),
the single-mechanism assumption (`:187-192`), `maximum_head_fleets ≤ 200`
(`protocol/policy.go:422-424`), `MaxHeadEntries` (`validator/release_head_v2.go:116,131`),
and the CRv4 5,000-byte payload cap in producer and readers (`crv4/tlock.go:71`;
`crv4/crv4.go:148-150`; `crv4/chain.go:1257-1258`), which fits about 1,200
UIDs.

Gates before any switch: (1) mode-aware payload admission in `crv4`
(`YUMA_COMMIT_SIZE_BYTES` 5,000 under Yuma; `32 KiB / mechanism_count` under
Null) with an end-to-end commit and reveal of the maximum head row on the
selected runtime; (2) treasury approval re-signed at 2,500 and the validator
reviewed for sole-permit, dividend and bond semantics; (3) dividend
attribution: reserve dividends counted in the 9/10 accounting, escrow
dividends captured by `captureEscrowSurplus` (§9.1), v1 vault surplus recorded
as residue; (4) the return path rehearsed from the expanded population:
`sudo_set_epoch_consensus(Yuma)` fails above 256 UIDs (`mechanism.rs:146-147`),
so the rehearsal includes `trim_null_uids_batch` in batches of 64 with the
immune guard (`uids.rs:196-245`), the owner rate limit, an empty commit queue
(`mechanism.rs:131-141`), and proof that custody, reserve, pool and validator
UIDs survive trimming; (5) the stake-rank monitoring threshold and response.
Head growth under Null then follows `VERSION2.md` §5.7 (θ raised with head
count under the `WHITEPAPER.md:1040-1042` constraint; a mainnet fleet-binding
batcher, since `STFleetBatcher` is testnet-only, `evm/src/STFleetBatcher.sol:6-12`).
None of this gates §5-§12.

### 14. Admission and rollout gates

Phase A (contracts, ledger, claims) activates when all hold:

1. Contracts: Foundry suite green including §16 cases; `forge build --sizes`
   margins recorded for the three new contracts; deployment plan and receipts
   as in `mainnet/BOOTSTRAP-CONTRACTS.md`; exact-artifact creation and
   readback on the selected runtime; `transferStake` and permit precompile
   behaviour rehearsed on the runtime with the nested-frame rollback check
   (`evm/CLAIM-RECOVERY.md:44-51`).
2. Ledger: million-provider fixtures (§16) pass in server builder and
   validator verifier with measured time, memory, disk and network inside the
   signed budgets; cross-language tree vectors match.
3. Identity: `settlementAt` consumed by validator, server, daemon, `snclaim`,
   replay and conservation tools; the last v1 and first v2 epochs rehearsed
   through the public readers on a fork.
4. Approvals: owner-signed validator production approval for config v3;
   treasury approval re-signed for policy v2's hash; signed server
   configurations; the `ur-owner` ceremonies recorded.
5. Runtime: pinned runtime 475 or an admitted successor; any runtime change
   restarts gate 1's rehearsal (`evm/README.md:184-186`).
6. Miner apps: cumulative reward display and claim flows in §11.1 are
   implemented and qualified for every supported app. Deterministic fixtures
   cover ten epochs claimed once, a subsequent claim paying only the new
   increment, a concurrent claim/root refresh, below-minimum and failed
   transfers retaining credit, one vault-wide credit shared across operators,
   and separate v1/v2 balances through migration and wallet changes.

Phase B (measurement) activates on the same approval path with its own
fixtures (§16) and a recorded eviction transition. Phase C and the Null track
have their gates in §13 and §15.

### 15. Release phasing

Phase 0, running now: the stopgap. It stays until phase A activates. Two
small changes ship meanwhile: the artifact's exclusion reason for non-members
becomes `cohort_not_sampled`, and the server publishes the cohort size,
lifetime count and epoch in its epoch summary, so the exclusions are
explainable. Owner decision (2026-10-09): there is no retroactive
compensation of the ~98.5k unpaid providers for epochs 1 to `E_s − 1`; the
earnings cutoff stays October 6 with no new off-chain fallback
(`mainnet/LAUNCH.md:931-934`).

Phase A, the critical path: `STSettlementVault2`, `STSettlement2`,
`STValidatorEvidence2`; ledger v2 builder with chunked evidence; validator
ledger verification (full checks of §7, sampled checks with the beacon,
`LEDGER_AUDIT` evidence, veto); `settlementAt` in every reader; claim daemon
and `snclaim` v2 with threshold and permit; policy v2; configurations and
approvals; migration steps 1-6 of §12. Deferred out of phase A to keep it
small: stratified cohorts (the stopgap cohort continues to bound the census
and feeds only the inert single-pool estimate), head budget changes, Null.
Target (owner decision, 2026-10-09): `E_s` = epoch 3, which starts at
block 9,348,028 (`epochStartBlock(1)` 9,247,228 + 2 × 50,400; about
2026-10-23 13:08 CDT at 12 s blocks). Every step of §12 that must precede
`E_s` (v2 pool registration, `schedulePolicy` for policy v2, the re-signed
approvals and the v2 activations) therefore lands during epoch 2. The target
remains conditional on §14's gates: a gate that is not met moves `E_s` to the
next epoch, and each epoch of delay leaves about 2,070 α of provider
allocation paid to at most 2,000 providers.

Phase B, measurement: sampling frame, VRF cohort with proof, replacement
budget, stratified estimator with interval, K = 0 eviction, head budget,
`a_min` 32, validator config v3 bounds. Target: within two epochs after A.

Phase C: committed per-provider reliability signal with retrospective audits
(option B), optional reintroduction of a per-provider factor into §6.3 under
a policy successor; operator bond and slash; veto quorum as a policy fraction;
Null track gates; carry sweep (§12 step 7) at `E_s + 10`.

### 16. Qualification and test plan

Contracts (Foundry, pinned libraries): vault2 conservation after every
function under fuzzing; funding equality (missed epoch then recovery;
deferred epoch; out-of-order finalization refused); the Alice/Mallory
successor refused by `challengeDecrease` and by the validator's transition
audit; omission of an inactive account caught by the transition audit and
vetoed; repeated sub-minimum claims credit once; claims against each ring
entry; batch with one invalid leaf reverts, with one failing transfer defers;
permit replay, wrong relayer, expired deadline, fee above cap, net below
minimum; `captureEscrowSurplus` pro rata; dual-vault routing by `settlementAt`
across `E_s`; v1 evidence during overlap and `LEDGER_AUDIT` accepted in v2 and
rejected in v1; carry sweep on a fork with late expiries and reveal-lag
emission; code-size margins.

Ledger and tree (Go): fixtures at 100k and 1M providers with skewed strata,
churn, dormant balances, shared coldkeys (one coldkey, many providers; a
provider changing coldkey), sub-rao weights, zero-usage epoch, a census beyond
32,768 contracts and 8 MiB; builder and verifier time, memory, disk and
network recorded; redistributed usage with unchanged total, wrong coldkey,
and an honest high-usage provider with few probe bytes all behave as §7
specifies; cross-language vectors for single and multiproofs including the
empty set.

Measurement (Go, deterministic synthetic populations): known population
means under unequal strata reproduced by the stratified estimator and not by
the exposure-weighted one; tiny strata, floors exceeding the budget,
duplicate draws, sustained churn to budget exhaustion, restart at every
eviction boundary; the four F1 acceptance cases (operator fabricates only the
complement, server returns another member, validator substitutes a seed,
server changes the frame after requests); head coverage with 200 and 2,000
fleets.

Operational rehearsal on a fork of chain 964: the full migration sequence,
daemon behaviour for a coldkey with v1 credit and v2 accrual, a missed first
v2 root, a vetoed root, and gas measurement of a paid single claim and a
64-leaf batch including runtime transfers.

### 17. Change list by component

| Component | Change |
| --- | --- |
| Policy schema v2 | add `ledger { schema: v2, max_chunk_bytes }`, `cohort { members, replacement_budget, q_min, strata, a_min, estimand, nonresponse_widening, operator_ema }`, `head { a_min_head, assignment_share, max_members_per_fleet }`, `audit { coverage_epochs, veto_quorum }`, `claim { max_relayer_fee_bps }`; keep `claim_ttl_epochs`/`claim_grace_epochs` populated (inert for v2); remove `shares_total_bps` and `rounding` from the v2 allocation; new `policy_id` scheduled by the Safe |
| Contracts | `STSettlementVault2`, `STSettlement2`, `STValidatorEvidence2` with `ValidatorEvidence2`; no change to v1 contracts; mainnet fleet-binding batcher only with the Null track |
| Server | frame snapshot and signed record; indexed assignment and signed unavailable; ledger v2 builder (streaming, chunked, content-addressed, continuity link); evidence chunking aligned with provider chunks; node-array proof service; `GET /sn/ledger/claim` with `claim_schema`, `root_index`, vault per epoch; `/sn/ledger/chunk`; `settlementAt` in the st sync mirror; event-indexed head set; eligibility without `a_min`; `cohort_not_sampled` reason and epoch summary fields now |
| Validator | `settlementAt` reader; ledger verifier (full and sampled); beacon; `LEDGER_AUDIT` evidence and veto; zero-weight trigger from findings; frame download and checks; VRF draw and proof; apportionment; replacement budget; stratified estimator and scalar EMA; K = 0 eviction transition; head budget; bounds schema v3 (`max_ledger_bytes`, `max_evidence_bytes`, `max_audit_bytes`, `max_chunk_bytes`, head entries) |
| Protocol / merkle | `ledger-tree-v2` builder, proofs, multiproofs, vectors; rao allocation with largest remainder; frame and apportionment algorithms; permit encoding |
| Provider tooling and miner apps | claim daemon v2 (schema switch, threshold, permit, batch via relayer, v1 withdraw retained); `snclaim` v2; app cumulative reward view, multi-epoch claim, credit withdrawal, fee preview and migration support (§11.1), qualified by §14 gate 6 |
| crv4 | mode-aware payload admission (Null track) |
| Approvals | validator production approval for config v3; treasury approval for policy v2; Null track: `maximum_subnet_uids` 2,500 |

### 18. Open risks and owner decisions

- Runtime gas of `transferStake` and the sr25519 precompile under the permit
  path are unmeasured; the 250k planning figure and the claim threshold are
  recalibrated at the first finalized v2 epoch.
- The evidence sampling budget trades detection probability against validator
  cost; the defaults (512 MiB per epoch, coverage every 4 epochs) need the
  million-provider fixtures to set, and the single-validator launch means one
  sample per epoch until more validators join.
- The veto is a quorum of one until `veto_quorum` applies; a false veto delays
  one epoch of payment.
- The operator can favour probed members; the quality estimate is an upper
  bound and is not used for payouts in this release.
- Frame honesty: an operator can omit eligible providers from the frame; they
  are then unmeasured but still paid by usage; the validator flags usage from
  non-frame providers, and a high rate is a finding threshold to be chosen.
- Alpha price: the TAO-denominated floor rises in α as the price falls;
  cumulative accrual makes this harmless to correctness and slower to pay.
- Head promotion mid-epoch: excluded from the ledger from that epoch; the
  coldkey's cumulative continues from its pool balance; the ledger carries the
  binding generation as the artifact does (`payoutartifact/artifact.go:50`).
- Residual v1 value after the carry sweep and sub-floor v1 credits are a
  quantified exception; the owner accepts or funds them.
- Retroactive compensation for the stopgap epochs: decided (owner,
  2026-10-09), none.
- Operator bond and the second operator: decide before admitting one.
- Null: stake-rank capture, dividends on non-validator UIDs, trimming and the
  constrained return path are the Null track's risks and do not affect v2.
- Runtime drift: spec 477 is released; every runtime change stops automated
  writes until revalidated and the successor-admission profile applies
  (`mainnet/LAUNCH.md:23-28`).
