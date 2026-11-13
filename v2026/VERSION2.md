# Pool payments at scale: version 2

Prepared 2026-10-08 for the SN25 protocol and contract engineers. It describes
why the launched pool-payment protocol cannot pay 100,000 providers, let alone
the 1,000,000 expected within six months, and proposes the version-2 design
that does. Read it with [mainnet/LAUNCH.md](mainnet/LAUNCH.md) and
[WHITEPAPER.md](WHITEPAPER.md) §8 and §11.

Citations are `path:lines`. Unprefixed paths are this repository at `e1500f15`;
`server/` paths are the operator server at `5342fd76`; `server-census/` is the
uncommitted `feat/sn25-verify-cohort` worktree at `/Users/brien/urnetwork/server-census`
(the measurement stopgap); `subtensor/` paths are upstream subtensor at
`5c6e83e` (spec 477, `subtensor/runtime/src/lib.rs:241`), checked where noted
against the runtime-475 source `d1718c99` that mainnet runs
(`mainnet/LAUNCH.md:23-25`). Live chain-964 values were read from Snow's Finney
archive at block 9,243,029 (epoch 0). Gas figures marked *measured* come from
the repository's Foundry suite built in a scratch copy with the pinned
libraries; its precompile mocks do not charge runtime dispatch, so they are
lower bounds. Alpha amounts use the live price 0.006587 TAO/α (alpha precompile
`getAlphaPrice(25)` = 6,587,154,000,000,000 at 18 decimals) and are approximate.

## 1. Summary

The launched protocol scores every provider a validator ever touched, publishes
one Merkle leaf per provider with its proof inside one JSON artifact, divides the
pool into exactly 10,000 basis points, and pays each provider by its own claim
transaction that performs a runtime stake transfer. Each of those is linear in
the provider count and most are hard-bounded at launch:

| Limit | Binds at | Failure |
| --- | --- | --- |
| Validator census, signed `max_providers` 4,096 (`validator/config_evidence_v2.go:35`, not revisable: `validator/production_capacity.go:85-94`) | ~4,100 measured providers, reached in about 20 minutes at 100k | Settlement raw measurement errors (`validator/attempt_settlement_v2_runtime.go:225-262`); the run exits (`validator/release_run.go:1136-1139`); systemd restarts it (`mainnet/LAUNCH.md:820`) into the same persisted window (`validator/stats.go:188-191`). No pool weight for the epoch. |
| Settlement transition bytes, signed `max_transition_bytes` 2 MiB (`validator/attempt_transition_v2_verify.go:137,170-173`) | ~5,000 providers at ~414 B each | Transition refused |
| Share granularity, 10,000 bps (`evm/src/STSettlementVault.sol:17,373,383`; `protocol/payout.go:106-126`) | 10,000 eligible coldkeys | Every further coldkey gets a 0-bps leaf that the vault cannot pay; the losers are chosen by client-id order |
| Payout artifact, 32 MiB (`validator/artifact.go:29,271`; `server/startifact/artifact.go:41,95`) | ~15,000 providers (measured 2,281 B/provider at 20k) | Validator cannot audit; server cannot serve claims |
| One claim transaction per provider per epoch (`evm/src/STSettlementVault.sol:365-390`) | economic: 100k claims cost ~125 TAO at 5 gwei against a ~13.6 TAO provider allocation | Gas is 9× the pool at 100k and 92× at 1M |

The measurement bound failed first at launch, exactly as the table predicts.
The stopgap now in progress, a server-side cohort of 1,500 providers per epoch
and 2,000 for the life of the deployment, keeps the validator alive but pays
only cohort members (section 3.6): the other ~98.5% of providers are
ineligible by construction, and after the lifetime set fills no new provider
can ever be measured or paid.

One chain-level change matters for the balance between the head and the pool
but not for the pool's own scaling. Subtensor's per-subnet epoch mode
`SubnetEpochConsensus` has a `Null` variant that uses the largest-stake
validator's weights and pays dividends in proportion to stake
(`subtensor/pallets/subtensor/src/lib.rs:505-513,2726-2728`), and it lifts the
per-subnet UID budget from 256 to `NULL_UID_BUDGET` = 2,500, shared across up
to `MaxMechanismCount` mechanisms (`subtensor/pallets/subtensor/src/subnets/mechanism.rs:35,101-121`;
live `MaxMechanismCount` = 8). It is in mainnet's runtime 475 (`d1718c99`:
`mechanism.rs:35,119`). SN25 runs Yuma at 256 of 256 UIDs with 64 validator
permits and one mechanism; the owner can switch and then raise `MaxAllowedUids`
to 2,500. That lets roughly 2,400 top fleets hold natively paid UIDs instead of
about 200, which is the cheapest settlement there is, but it is 40× short of
100k providers and 400× short of 1M. Our validator's signed treasury approval
pins the subnet at 256 UIDs and one mechanism and refuses every decision beyond
it (`validator/recycle_observation.go:187-192,201-208,210`), so using the budget
needs a new approval and a review of the validator under Null, where stake rank
replaces Yuma's stake-majority as the security assumption (sections 3.7, 5.7).

Version 2 replaces the full census with validator-seeded, verifiable random
cohorts whose size is independent of the population; aggregates them into a
per-operator quality estimator with a stated confidence interval; pays
unmeasured providers from receipt-backed usage and operator-attested
reliability that the cohort audits; publishes a streamed, chunked payout ledger
with amount-denominated cumulative leaves instead of 10,000 bps and inline
proofs; and settles through a new immutable vault whose cumulative roots let
any provider claim its whole accrued balance in one transaction, at any time,
in relayer batches. Mandatory on-chain cost per epoch becomes three operator
transactions (~0.54 Mgas, measured); claim cost becomes proportional to claims
actually made, not to providers. The treasury row (45%/45%/10%), θ and the
validator-independence, fraud-resistance and evidence guarantees are preserved.

## 2. Background: launched pool payments, end to end

Launch state (`mainnet/LAUNCH.md:17-77`): one validator, UID 1; one operator,
`no_id` 1, pool hotkey UID 169; reserve recipients UIDs 170 and 250; epoch 0 of
7,200 blocks, then 50,400-block epochs from block 9,247,228.

Subnet consensus mode and UID budget, live: `SubnetEpochConsensus(25)` is unset,
so Yuma (`subtensor/pallets/subtensor/src/lib.rs:2726-2728`); `MaxAllowedUids` 256
and `SubnetworkN` 256 (full); `MaxAllowedValidators` 64; `MechanismCountCurrent` 1;
global `MaxMechanismCount` 8 (pallet default 2, `subtensor/pallets/subtensor/src/lib.rs:3558-3565`;
hard ceiling 16, `subtensor/pallets/subtensor/src/subnets/mechanism.rs:30-31`).
Under Yuma the UID budget is `DefaultMaxAllowedUids` = 256
(`subtensor/runtime/src/lib.rs:916`); under Null it is 2,500, and
`max_uids × mechanism_count` must stay within it
(`subtensor/pallets/subtensor/src/subnets/mechanism.rs:101-121`). The whitepaper's
"256 hard ceiling, owners may lower, never raise" (`WHITEPAPER.md:1617`) is
superseded by Null.

```
 providers (100k)        operator server (no_id 1)                validator (UID 1)                 chain 964
 ───────────────         ──────────────────────────               ─────────────────                 ─────────
 serve contracts ──────► closed work, usage bytes                                                  
 relay verify hops ◄──── assign next hop uniformly from  ◄──────── seed/extend trails (M=8)
                         verify_eligible_v2 (Redis)               30 seeds/min, 7 hops/trail
                         record assignments/confirmations         per-provider window, EMA,
                         per client (verify_provider_stats)       egress hashes (census ≤ 4096)
                                                                       │
                                                                       ▼
                                                                  Q_n = exposure-weighted mean
                                                                  reliability over measured set
                                                                  weights: head θ / pool 1−θ,
                                                                  treasury row 45/45/10 ──────────► CRv4 weights
                                                                                                    Yuma → α on pool UID 169
 epoch end E:
                         closeOperatorEpoch(E) ──────────────────────────────────────────────────► vault.captureEmission
                         build payout artifact: per provider                                      pool hotkey → escrow
                           usage × Wilson(reliability), largest-
                           remainder to 10,000 bps, one leaf per
                           coldkey, proof per leaf, sign, MinIO
                         commitOperatorRoot(E, root, artifactHash) ─────────────────────────────► coordinator (≤ end+1,200)
                                                                  fetch artifact (≤ 32 MiB), rebuild
                                                                  root, deposit audit, evidence hash
                         finalizeOperatorEpoch(E) (≥ end+14,400) ───────────────────────────────► vault entitlement
 GET /sn/pool/claim ────► leaf + proof for the caller's coldkey
 claim(E, 1, coldkey,
   shareBps, proof) ─────────────────────────────────────────────────────────────────────────────► vault.claim: verify,
   one tx per provider, provider pays gas                                                          credit, transferStake
                                                                                                    unclaimed after 9 epochs
                                                                                                    → carry[no_id]
```

Measurement. The validator seeds trails whose hops the server assigns uniformly
at random from every eligible connected provider (`server/model/verify_model.go:562-647`,
set `verify_eligible_v2` at `:266`, called from `server/controller/verify_controller.go:552,934`).
Seeds are paced at 3/4 of the policy's 40/min hard limit (`validator/release_run.go:450-462`;
`deploy/mainnet/policy-v1.yml:105`), and a trail of depth 8 covers 7 provider hops
(`validator/trail.go:168`; `deploy/mainnet/policy-v1.yml:93`). The server records
each client's assignments and confirmations per 900 s period
(`server/model/verify_model.go:1719-1745`). The validator keeps its own per-provider
window, EMA and egress-hash sets, and persists them (`validator/stats.go:179-192`).

Scoring. A provider has a quality only once it has `a_min` = 8 assignments
(`validator/measurement_stats.go:115-116`; `deploy/mainnet/policy-v1.yml:99`);
quality is the exact 95% Wilson lower bound of confirmations over assignments
(`protocol/reliability.go:12-30`) times a latency factor, smoothed by EMA
(`validator/measurement_stats.go:125-132`). The pool quality `Q_n` is the
exposure-weighted mean over measured, non-head providers
(`validator/measurement_stats.go:532-553`), clamped to [0.75, 1.0]
(`validator/release_math.go:148-157`; `deploy/mainnet/policy-v1.yml:54-58`), and
multiplies implied demand (`validator/release_measurement.go:719-733`). Head
fleets get θ = 3/10 and pools 1−θ; an empty channel cedes to the other
(`validator/release_math.go:24-66`; `deploy/mainnet/policy-v1.yml:50-53`). The
treasury row fixes providers at 1/10 and the reserve at 9/10
(`validator/treasury_policy.go:50-51`; `validator/TREASURY-PRODUCTION.md:17`).
With one pool the pool share is normalized to itself, so at launch `Q_n`
changes no weight; measurement bears on per-provider eligibility and on the
validator's audit of the artifact.

Settlement artifact. At the epoch close the server joins every provider with
usage in the window to its wallet, its server-recorded assignments and
confirmations, and its head binding (`server/controller/st_controller.go:3291-3336`,
`3338-3420`); a provider is eligible only if it has usage, at least `a_min`
assignments, a confirmation and a coldkey, and is not a bound head
(`server/controller/st_controller.go:3331`). `payoutartifact.Build` sorts providers,
computes reliability, allocates exactly 10,000 bps by largest remainder with
client-id tie-break after aggregating by coldkey (`protocol/payout.go:53-57,98-126`),
builds the OpenZeppelin-compatible tree (`merkle/merkle.go:162-225`), and emits
one `Leaf` per coldkey with its full proof inside the artifact
(`payoutartifact/artifact.go:53-60,179-203`). Fixed byte arrays serialize as JSON
number arrays. The signed artifact is published to content-addressed MinIO
(`server/startifact/artifact.go:49-72`), reread with a 32 MiB cap (`:41,95`), and
its leaves are stored one row per `(epoch, no_id, coldkey)`
(`server/model/st_model.go:11-14,531`).

Chain. The operator's root signer commits `(payoutRoot, artifactHash)` within
1,200 blocks of the epoch end (`evm/src/STCoordinator.sol:609-633`;
`deploy/mainnet/policy-v1.yml:45`), the pool stake is captured into the
immutable escrow (`evm/src/STCoordinator.sol:586-600`; `evm/src/STSettlementVault.sol:251-307`),
and after 14,400 blocks the entitlement is fixed with expiry at the start of
epoch E+10 minus one (`evm/src/STCoordinator.sol:635-654`; `deploy/mainnet/policy-v1.yml:33-34,46`).
Measured operator gas: `closeOperatorEpoch` 246k, `commitOperatorRoot` 108k,
`finalizeOperatorEpoch` 184k (avg), all O(1) per operator per epoch.

Validator audit. The validator fetches the committed artifact (≤ 32 MiB,
`validator/artifact.go:271`; `validator/release_decision_history_v2.go:375`),
refuses one whose provider or leaf count exceeds its admitted census
(`validator/artifact.go:277-289`), rebuilds root, shares and proofs
(`payoutartifact/artifact.go:335-352`), audits the deposit from the artifact
totals (`payoutartifact/artifact.go:86-93`), and anchors its closed-census
evidence hash on chain (`evm/src/STValidatorEvidence.sol:126-177`). The policy's
remedy for a bad artifact is zero pool weight (`deploy/mainnet/policy-v1.yml:72-73`).

Claims. A provider's daemon polls `GET /sn/pool/claim`
(`server/controller/sn_controller.go:216-250`, leaf selection
`server/controller/sn_claim_owner.go:18-66`), receives `(coldkey, shareBps, proof)`,
and submits `claim(epoch, noId, coldkey, shareBps, proof)` with its own EVM key
(`miner/claim_daemon.go:39-52,801-850`; `miner/onchain/run.go:1-12`), gas estimated
plus 20% (`miner/onchain/claim.go:93-109`). Any funded key may relay; payment
always lands on the leaf's coldkey (`evm/src/STSettlementVault.sol:392-398`).
The vault verifies the proof, marks `(noId, coldkey)` claimed, credits
`total × shareBps / 10,000`, and transfers the credit when its TAO equivalent
is at least `minimumTransferTaoRao` (live: 100,000 rao), otherwise defers it
(`evm/src/STSettlementVault.sol:365-390,446-511`). Unclaimed value after expiry
becomes the same operator's carry (`evm/src/STSettlementVault.sol:400-408`),
re-entering the next finalized total (`:334-336`).

## 3. The scaling problem

Baseline economics, live. UIDs 170 and 250 each receive 73.8 α per 360-block
tempo, so the miner allocation is ≈147.6 α per tempo, ≈0.41 α per block,
≈20,700 α per 50,400-block epoch (consistent with `WHITEPAPER.md:1653-1657`).
Providers' 1/10 is ≈2,066 α ≈ 13.6 TAO per epoch; the pool is all of it with
no head and 0.7 × that (≈1,446 α) with a head. Average weekly entitlement per
provider is therefore ≈0.021 α (1.4 × 10⁻⁴ TAO) at 100k and ≈0.0021 α
(1.4 × 10⁻⁵ TAO) at 1M. The vault's minimum transfer, 100,000 rao TAO-equivalent,
is ≈0.015 α at the live price: the 100k average clears it by 1.4×, the 1M
average does not.

### 3.1 Measurement

The validator's settlement census is the union of its window, prior-quality and
egress-hash provider sets (`validator/measurement_stats_v2.go:42-73`;
`validator/attempt_settlement_v2_runtime.go:225-262`), bounded by the signed
`max_providers` and `max_egress_hashes` (4,096 and 32,768 at launch;
`validator/config_evidence_v2.go:35-36`). A capacity revision may grow only
aggregate history dimensions; these are fixed (`validator/production_capacity.go:85-94,117-120`).
The window and prior qualities persist across restarts (`validator/stats.go:188-191`),
and a prior quality never expires, so the census is every provider the
lineage has ever scored.

Growth: 30 seeds/min × 7 hops = 210 distinct new providers per minute while
almost every uniform draw is new, so 4,096 is reached in ≈19.5 minutes. The
raw measurement is taken on every settlement commit and activation check
(`validator/attempt_settlement_v2_commit.go:300`; `validator/attempt_settlement_v2_authority.go:73,144,182`);
its error is not a transport cause, the run returns it, `runReleaseConfig`
panics (`validator/release_run.go:1136-1139`), and the unit restarts into the
same persisted census (`mainnet/LAUNCH.md:820`). No decision is produced for the
epoch; with `empty_channel: cede_to_nonempty` and no head, the treasury row
falls back to reserve-only (`validator/TREASURY-PRODUCTION.md:17`), which is what
the live emission shows.

Even without the bound, one validator's assignment budget is about
30 × 7 × 10,080 ≈ 2.1 million assignments per epoch (extends add more). At
100k providers that is ≈21 per provider per epoch, above `a_min` = 8; at 1M it
is ≈2.1, and `a_min` × 1M = 8M assignments would be needed. A full census is
not only unbounded in state; it is unaffordable in measurement.

| | 100k providers | 1M providers |
| --- | --- | --- |
| Census entries needed | 100,000 (24× the bound) | 1,000,000 (244×) |
| Time to overflow at launch pacing | ≈20 min | ≈20 min |
| Assignments per provider per epoch, uniform | ≈21 | ≈2.1 (< a_min) |
| Validator settlement transition (`PreFold`, `PostFold`; ≈414 B/provider per `server-census/controller/verify_controller.go:280-283`) | ≈41 MB vs 2 MiB cap | ≈414 MB |

### 3.2 Settlement artifacts

Measured artifact size with `payoutartifact.Build` (synthetic, one coldkey per
provider): 1,731 B/provider at 1,000 providers (proof depth 10) and
2,281 B/provider at 20,000 (depth 15; 45.6 MB). The fit is ≈631 B + 110 B per
proof level, because each 32-byte proof node is a 32-number JSON array.

| | 100k (depth 17) | 1M (depth 20) |
| --- | --- | --- |
| Payout artifact | ≈250 MB | ≈2.8 GB |
| Caps | 32 MiB validator fetch (`validator/artifact.go:29,271`), 32 MiB server read (`server/startifact/artifact.go:95`), `max_artifact_bytes` (`validator/release_measurement_v2.go:232`) | same |
| Binds at | ≈15,000 providers | |
| Validator artifact census (`validator/artifact.go:277-289`; `mainnet/economic_conservation_entitlement.go:21`) | 4,096 providers or leaves | |
| Nonzero leaves (`protocol/payout.go:120-126`; measured: 20,000 providers → exactly 10,000) | 10,000 | 10,000 |
| Payout roster, complete population, 64 MiB request (`payoutroster/types.go:12`; `mainnet/PAYOUT-ROSTER.md:9-14,66-67`) | ≈15 MB | ≈150 MB, over the bound |
| Head-binding lookup, one `bindingAt` per provider per epoch (`server/controller/st_controller.go:3320`) | 100k eth_call rows | 1M |
| Release measurement envelope, 64 MiB (`validator/release_measurement_envelope.go:34`) | fits | fits only if per-provider rows stay under ~64 B |

Verification cost follows bytes: `payoutartifact.Verify` reparses the whole
artifact, rebuilds every proof and `reflect.DeepEqual`s the leaf list
(`payoutartifact/artifact.go:335-352`), so a 2.8 GB artifact needs several GB
of validator memory. The server's claim lookup also rereads the whole artifact
for one provider (`server/controller/sn_claim_owner.go:50`).

### 3.3 On-chain claims

Share granularity. `AllocateShares` floors each coldkey's share to whole basis
points and distributes the remainder one point each to the largest remainders,
ties broken by ascending client id (`protocol/payout.go:102-126`). With more
than 10,000 eligible coldkeys of similar weight every floor is 0 and exactly
10,000 coldkeys receive 1 bps; the rest receive 0-bps leaves
(`payoutartifact/artifact.go:179-203`), which `claim` rejects
(`evm/src/STSettlementVault.sol:373`). The excluded set is deterministic, so the
same providers lose every epoch. The policy pins this (`deploy/mainnet/policy-v1.yml:37-38`;
`protocol/policy.go` schema) and the vault hard-codes `BPS = 10_000`
(`evm/src/STSettlementVault.sol:17,383`). One basis point of the no-head pool
is ≈0.21 α ≈ 1.4 × 10⁻³ TAO.

Gas per claim. Measured on the vault suite: `claim` median 179,837 gas, max
216,008, with shallow proofs and mocked precompiles. A depth-17 proof adds
≈11k (544 bytes of calldata plus hashing). The five staking/alpha precompile
reads and the `transferStake` runtime dispatch (`evm/src/STSettlementVault.sol:488-511`)
are not charged by the mocks. Planning figure: 250,000 gas, 0.00125 TAO at the
live 5 gwei base fee (≈0.19 α). Measure it at the first finalized epoch.

| | 100k claims per epoch | 1M claims per epoch |
| --- | --- | --- |
| Gas | 25 Ggas | 250 Ggas |
| Cost at 5 gwei | ≈125 TAO ≈ 19,000 α | ≈1,250 TAO ≈ 190,000 α |
| Provider allocation per epoch | ≈13.6 TAO ≈ 2,066 α | same |
| Gas / allocation | 9.2× | 92× |
| Gas / average entitlement | 9.2× | 92× |
| Chain capacity consumed (75 Mgas blocks, 12 s) | 333 full blocks ≈ 67 min of the 7-day epoch | 3,333 blocks ≈ 11 h |
| Average entitlement vs 0.015 α minimum transfer | 1.4× (paid) | 0.14× (deferred every epoch, still one tx) |

The gas is paid by whoever submits, normally the provider
(`miner/claim_daemon.go:44,828`). Claims are open from finalization (48 h after
the epoch end) until the start of epoch E+10 (`evm/src/STCoordinator.sol:642-645`),
about nine weeks, after which the remainder carries to the operator
(`evm/src/STSettlementVault.sol:400-408`). Chain throughput is not the binding
constraint; cost is. At 100k providers a weekly on-chain claim per provider
costs nine times what it pays; at 1M it costs 92 times, and most entitlements
are below the transfer floor, so the claim only records credit.

### 3.4 Other linear places

- Provider work. Receipts are per provider and per-receipt bounded
  (`protocol/provider_work_admission.go:18-21`); storage grows with providers but
  no consumer reads them all per epoch. Not binding.
- Custody and the attempt ledger. Record and trail counts are revisable
  aggregates (`validator/production_capacity.go:87-90`); the upload authority
  admits ≤ 32 MiB per window, ≤ 1 GiB original, ≤ 1,000,000 objects
  (`validator/provider_attempt_authority.go:197`). These follow trail volume, not
  providers. Not binding.
- Egress hashes. `max_egress_hashes` 32,768 is fixed with `max_providers`
  (`validator/measurement_stats_v2.go:67-73`; `validator/production_capacity.go:85-94`).
  Binds with the census.
- Client-key registrations and history. Statements are ≤ 8 KiB
  (`protocol/client_key_history.go:24`); batches are ≤ 512 clients and 1 MiB per
  request with 64 MiB of control (`protocol/client_key_history_batch.go:12-15`). A
  full-population capture is ⌈N/512⌉ requests: 196 at 100k, 1,954 at 1M.
  Linear but bounded per request; the validator captures only decision inputs
  (`validator/release_client_key_history_v2.go`).
- Settlement journal. The validator's per-epoch transition carries one row per
  census provider (`validator/attempt_transition_v2_verify.go:137,170-173`); it
  binds at ≈5k. Server-side, `st_payout_leaf` and `verify_provider_stats` are one
  row per provider per epoch or period (`server/model/st_model.go:11-14`;
  `server/model/verify_model.go:1731`); Postgres scale, not binding.
- Deposit flow. One `deposit` per operator per epoch (`evm/src/STCoordinator.sol:484-584`)
  audited from artifact totals (`payoutartifact/artifact.go:86-93`). O(1) on
  chain, but the audit must fetch and reverify the whole artifact (3.2).
- Head bindings. One `bindFleetMember` per client (`evm/src/STCoordinator.sol:725-794`),
  ≤ 200 fleets (`deploy/mainnet/policy-v1.yml:61`; `protocol/policy.go:423`), and
  the testnet batcher binds 10 fleets × 4 members per transaction
  (`evm/src/STFleetBatcher.sol:11-12`). O(head members), fine. The server's
  per-provider `bindingAt` batch and the validator's `CurrentBindingKVs ≤ max_providers`
  (`validator/attempt_cut_v2_head.go:52`) are the linear parts.

### 3.5 Which binds first

1. Validator census, 4,096: fails in minutes, fatally, before any provider
   reaches `a_min`. This is the launch failure.
2. Settlement transition, 2 MiB: ≈5,000.
3. Share granularity, 10,000 bps: silent exclusion of every coldkey beyond
   10,000, biased by client id.
4. Payout artifact, 32 MiB: ≈15,000; the validator cannot audit and the server
   cannot serve claims.
5. Payout roster, 64 MiB: a few hundred thousand.
6. Claims: not a hard failure but 9× to 92× uneconomic, and below the transfer
   floor at 1M.

Raising the signed bounds does not help: `max_providers` is a fixed dimension
of the production capacity schema, the census is the validator's memory and
disk footprint, the 10,000 bps live in the immutable vault, and the
measurement budget cannot reach `a_min` for 1M providers.

### 3.6 The stopgap cohort and payment correctness

The uncommitted `feat/sn25-verify-cohort` change samples next hops from a
bounded cohort instead of the whole eligible set
(`server-census/model/verify_model.go` hooks `SampleVerifyNextHop`;
`server-census/model/verify_cohort_model.go:6-45`). Defaults on mainnet are 1,500
providers per settlement epoch and 2,000 over the deployment's life
(`server-census/controller/verify_controller.go:289-290,294-315`). Admission is one
Redis script enforcing both caps (`server-census/model/verify_cohort_model.go:100-124`):
members of the lifetime set are readmitted first, a new provider enters only
after the cohort has averaged `a_min` draws and only while the lifetime set has
room (`:114-118,286-293`). The lifetime set has no TTL (`:83`). With a cohort,
a trail that finds no member is refused rather than given a synthetic hop
(`server-census/controller/verify_controller.go:617-624`).

What it does to payments:

- Only cohort members accumulate assignments, so only they can satisfy the
  artifact's eligibility rule (`server/controller/st_controller.go:3331`). At
  100k providers at most 1,500 coldkeys (1.5%) share the entire pool each
  epoch; the other 98.5% are excluded with `reliability_exposure_floor`
  (`server/controller/st_controller.go:3332-3334`). Their usage still counts in
  `TotalUsageBytes` for the deposit audit but earns nothing.
- Carried-first admission re-selects last epoch's cohort, so the paid set is
  the same ≈1,500 providers every epoch. Once 2,000 providers have ever been
  admitted no new provider can be measured or paid without clearing Redis
  state; the set is closed for the life of the deployment.
- The cohort is not a random sample after the first epoch. `Q_n` is the
  exposure-weighted mean over a fixed, self-selected subset; with one pool it
  does not move weights, but it is not an estimate of pool quality.
- The validator cannot tell a cohort from a census. `assign_n` becomes the
  cohort size (`server-census/model/verify_cohort_model.go:120-124,318-319`),
  which changes the recorded assignment probability without a signed policy.

As an emergency measure it keeps the validator producing weights. As a payment
basis it is not sound: it pays a closed set and pays them for being measured,
not for serving.

### 3.7 What Null consensus and the 2,500-UID budget change

Semantics (upstream `5c6e83e`; in the runtime-475 source `d1718c99` the budget,
`do_set_epoch_consensus`, the sole-permit switch and the Null epoch load are at
`mechanism.rs:35,124,163-167`, `run_epoch.rs:839,1336` and the setter at
`admin-utils/src/lib.rs:1483`):

- Switching modes is `sudo_set_epoch_consensus`, owner-or-root with the owner
  rate limit, inside the admin window
  (`subtensor/pallets/admin-utils/src/lib.rs:1513-1540`). The switch refuses
  while any timelocked weight commit is pending, clamps `MaxAllowedUids` to
  `budget / mechanism_count`, and, entering Null, saves `MaxAllowedValidators`,
  sets it to 1 and gives the sole permit to the largest-stake UID
  (`subtensor/pallets/subtensor/src/subnets/mechanism.rs:124-187`). Leaving Null
  restores the saved validator count (`:177-185`).
- Raising `MaxAllowedUids` is `sudo_set_max_allowed_uids`, same authority and
  window, bounded by the budget for the current mode and by
  `max_uids × mechanisms ≤ budget` (`subtensor/pallets/admin-utils/src/lib.rs:620-670`).
- Each epoch, Null loads only the elected validator's weight row
  (`subtensor/pallets/subtensor/src/epoch/run_epoch.rs:838-854`); the winner is
  the largest unrounded stake, first UID on ties (`:1335-1344`), and the permit
  vector is exactly that UID (`:804-808`). Incentive is the winner's masked
  integer row, normalized; with an empty row every registered UID shares
  equally (`:938-965`). Dividends go to every UID with positive stake, in
  proportion to stake (`:940-951`; `subtensor/pallets/subtensor/src/coinbase/run_coinbase.rs:533-547`).
  Bonds are frozen (`:1291-1293`; `subtensor/pallets/subtensor/src/lib.rs:2478-2480`),
  so Liquid Alpha and consensus/trust columns are inert. Commit-reveal and
  registration masks are unchanged (`run_epoch.rs:930-931`). Trimming removes
  the lowest emitters, under Null in explicit owner batches of 64
  (`subtensor/pallets/subtensor/src/subnets/uids.rs:9,196-245,323-327`).

What it changes for SN25:

| | Yuma, today | Null at 2,500 UIDs, 1 mechanism | Null, 2 mechanisms |
| --- | --- | --- | --- |
| UIDs for head fleets after validator, pool, escrow and two reserve UIDs | ≈250 (policy caps fleets at 200, `deploy/mainnet/policy-v1.yml:61`) | ≈2,490 | ≈1,245 per mechanism |
| Providers that can be paid natively, no claim gas (`WHITEPAPER.md:970-974`) | ≤ 200 fleets | ≈2,400 fleets | ≈2,400 across both |
| Share of 100k / 1M providers | 0.2% / 0.02% | 2.4% / 0.24% | same |
| Who decides the row | κ-stake-weighted median of permitted validators | the single largest-stake UID | same, per mechanism |
| θ enforcement | needs a stake majority running the same θ (`WHITEPAPER.md:1024-1026`) | exact: the row is paid as submitted | can also be chain-enforced as the owner's emission split between mechanisms (`subtensor/pallets/admin-utils/src/lib.rs:2119`; `subtensor/pallets/subtensor/src/subnets/mechanism.rs:321-334`) |
| Our validator | permitted, one of 64 | must be the largest-stake UID or it loses the only permit | same, and it must submit one row per mechanism |

The budget is still 40× to 400× short of the population, so the pool, its
artifact and its claims remain the settlement path for almost every provider;
what changes is the head/pool balance (5.7). Two local bounds block the change
today: the signed treasury approval pins `maximum_subnet_uids` (256 at launch,
`validator/recycle_approval.go:48`) and the validator refuses every decision once
`SubnetworkN` exceeds it (`validator/recycle_observation.go:201-208`); its snapshot
and planner require exactly one mechanism (`validator/recycle_observation.go:187-192,210`;
`validator/recycle_planner.go:188`). The release path's own cap is 65,535
(`validator/release_native_validator.go:19`), so only the approval and the
mechanism assumption bind.

Null also pays stake-proportional dividends to UIDs that are not validators.
The vault's escrow hotkey is a registered UID (`evm/src/STSettlementVault.sol:191-209`)
holding every provider's unclaimed α; the two reserve UIDs hold their
accumulated emission as stake; under Null all of them earn a stake-proportional
share of the 41% validator allocation. The reserve's share lands on the reserve
coldkey; the escrow's share raises `liveEscrowStake` above `escrowAccounted`,
which conservation permits (`evm/src/STSettlementVault.sol:412-416`) but no vault
function can distribute. The treasury accounting of 9/10 must treat these
dividends explicitly, and v2's vault needs a surplus capture (5.7).

## 4. Requirements for version 2

1. Scale: at least 1,000,000 providers per operator, with validator state and
   per-epoch work independent of the provider count.
2. Security model unchanged: validators compute scores from their own trails
   and reproduce every operator claim they rely on; operators direct the split
   but never hold the funds (`WHITEPAPER.md:924-925`); a bad artifact or root
   cannot overdraw escrow and costs the operator weight; every decision input
   is retained, hash-anchored and publicly reproducible
   (`evm/src/STValidatorEvidence.sol`).
3. Bounded validator resources: memory, disk and transition bytes bounded by
   signed cohort sizes, not by `max_providers`.
4. Bounded mandatory on-chain cost per epoch: O(operators) transactions.
   Claim cost proportional to claims made, each amortizing arbitrary accrual.
5. Payout latency: claimable at finalization (epoch end + 48 h) as today; no
   provider's accrued balance expires because it was too small to claim.
6. Economics preserved: providers 1/10 and reserve 9/10 (`validator/treasury_policy.go:50-51`),
   θ inside the provider component, pool quality modulating pool weight
   across operators, head channel unchanged in mechanism, under either Yuma or
   Null and at any UID budget the owner selects.
7. Every provider with receipt-backed usage is payable, measured or not, and
   paying an unmeasured provider stays accountable.
8. Migration without stranding v1 entitlements, carry or evidence.

## 5. Proposed design

### 5.1 Overview

```
 server: per tempo, commit the sorted eligible set E_t (root, n, strata)        ─► artifact v2 header
 validator: VRF(hotkey, finalized block hash at tempo start ‖ epoch ‖ no_id)     (secret until reveal)
            draw cohort C_e of m providers per stratum from E_t; assign hops
            only within C_e; replace churned members by the next VRF index
 server: assign the requested member, return membership proof against E_t
 validator: per-provider stats for C_e only; carry K prior cohorts' EMA;
            Q̂_n with confidence interval; audit operator attestations on C_e
 operator: payout ledger v2 = usage × (measured | attested) reliability,
            cumulative amount per coldkey, streamed chunks, root, totals
 chain:    vault v2: finalizeDistribution(cumulativeRoot, cumulativeTotal);
            claim(coldkey, cumulativeRao, proof) pays cumulative − paid;
            claimBatch via multiproof; relayer fee by coldkey permit
```

### 5.2 Measurement: verifiable random cohorts

Selection. The server publishes, once per native tempo, the Merkle root of its
sorted eligible set `E_t` (client ids, each tagged with stratum: operator,
egress-prefix family, tenure bucket), its size `n_t` and per-stratum counts.
The validator derives a seed `s = VRF_sk(H(finalized_block_hash(t₀) ‖ epoch ‖ no_id))`
with its hotkey; the inputs are already pinned in its envelope
(`validator/release_measurement_envelope.go:52-55`). It draws cohort indices
`i_k = H(s ‖ stratum ‖ k) mod n_stratum`, requests those members by index, and
the server answers with the member and an O(log n) membership proof against
the committed root. A pure public beacon would let a provider know when it is
measured; the VRF keeps the cohort unpredictable to providers and the server
until the validator reveals `s` in its sealed measurement at the epoch end,
after which anyone can recompute the cohort and check that every assignment
was a cohort member. Poison trails and padding stay as they are
(`server/model/verify_model.go:654-660`).

Stratification and quotas. Per operator, per stratum, `m_s = max(q_min, m · √(n_s) / Σ√(n_j))`
with `m = max_cohort_members` (4,096, the same number the census used) and a
replacement allowance of 25% for churn. Sqrt allocation gives small strata
(new providers, IPv6, rare prefixes) coverage without starving large ones;
head prefix coverage (`WHITEPAPER.md:949-955`) comes from the egress-prefix
stratum.

Epoch stability and rotation. A cohort is drawn at the settlement epoch start
and held for the epoch so members reach `a_min`; each tempo redraws only the
replacement allowance for members that left `E_t`. The next epoch draws a
fresh cohort; providers in the previous `K` cohorts keep their EMA, everyone
else's quality is dropped. Validator state is therefore `≤ m × (1 + K)`
entries (20,480 at K = 4) regardless of N, and `max_providers`,
`max_egress_hashes` and `max_transition_bytes` become functions of `m` and `K`
only. `a_min` reachability: 2.1M assignments per epoch over 4,096 members is
≈510 per member, so `a_min` can rise to 32 or 64 and tighten the Wilson bound
(`protocol/reliability.go`) while still measuring every member within the
first day.

Estimator. `Q̂_n` stays the exposure-weighted mean over measured, non-head
cohort members (`validator/measurement_stats.go:532-553`), but it is now a
stratified random sample of the operator's population with a reported
interval: for reliabilities in [0,1] and `m_eff` = 2,000 measured members the
standard error is ≤ 0.5/√2000 ≈ 0.011, so the 95% half-width is ≈2.2
points, well inside the [0.75, 1.0] clamp band. The measurement publishes
`m_eff`, the per-stratum counts and the interval; a verifier rejects a
transition whose cohort does not match the revealed seed and committed set.

Churn and unmeasured strata. If a stratum's members leave faster than the
allowance replaces them the stratum reports `m_eff` below quota and its
contribution is widened, not invented; the operator's attestations for that
stratum are then audited more heavily next epoch.

### 5.3 Paying unmeasured providers accountably

The payout basis for every eligible provider becomes
`weight_p = usage_p × r_p`, where `usage_p` is receipt-backed closed work
(`payoutartifact/closed_work.go`; `ClosedWorkCensus`) and `r_p` is:

- the validator-measured Wilson lower bound when `p` is in the epoch's cohort
  with ≥ `a_min` assignments (as today), or
- the operator's attested reliability `r̂_p` otherwise, derived from signals the
  operator already holds for every provider (contract close reports, client
  disconnect causes, egress liveness) under a published formula in the policy.

Audit. The artifact carries `r̂_p` for every provider, including cohort
members. The validator computes, over the cohort, the attestation error
`δ_n = Σ_{p∈C} w_p (r̂_p − r_p)⁺ / Σ w_p` and its interval. Because the operator
could not know the cohort when it attested, overstating unmeasured providers
is caught in expectation; the penalty is the existing economic one, `Q_n`
multiplied by `max(0, 1 − λδ_n)` with λ in the policy, plus a `zero_pool_weight`
threshold on `δ_n`, in the same place the deposit audit already zeroes weight
(`deploy/mainnet/policy-v1.yml:72-73`). The validator also checks the two
usage invariants it can see: Σ usage in the ledger equals the closed-work
census total, and no cohort member's attested usage exceeds its own observed
hop volume by more than the tolerance.

Eligibility. The `a_min` rule moves from "assignments ≥ a_min" to "usage > 0 and
coldkey mapped and not head-bound"; measured providers additionally carry the
measured `r_p`. The exposure floor no longer excludes anyone.

Spot-check challenges. Any party may publish a signed challenge naming an
artifact hash and a leaf whose amount does not reproduce from the published
inputs; validators verify it against the retained artifact and, if correct,
apply the `zero_pool_weight` remedy for the epoch and record it in their
evidence slot (`evm/src/STValidatorEvidence.sol:126-177`, a new `AUDIT_CHALLENGE`
kind). No new on-chain dispute logic is needed; the chain already binds the
artifact hash to the root (`evm/src/STSettlementVault.sol:340-341`).

Operator bond. Optional, phase 2: the coordinator holds a per-operator bond in
α that the owner Safe may slash on a validator-published, replayable
challenge. With one operator at launch it adds little; with independent
operators it makes `δ_n` fraud cost more than weight. Specified but not
required for v2 activation.

### 5.4 Payout ledger v2

Schema `urnetwork-payout-ledger-v2`, replacing the artifact's inline proofs:

- Header (one JSON object, ≤ 64 KiB): identity and boundaries as today
  (`payoutartifact/artifact.go:64-101`), `eligible_set_roots[]` per tempo,
  `cohort_seed_reveal`, `total_usage_bytes`, `total_users`,
  `cumulative_total_rao`, `epoch_delta_rao`, `leaf_count`, chunk index
  (`sha256`, byte range, first/last coldkey), signer, signature.
- Provider chunks (binary, 8 MiB): rows of `client_id(16) ‖ coldkey(32) ‖ usage_bytes(8) ‖ assignments(4) ‖ confirmations(4) ‖ attested_r_ppm(4) ‖ flags(1)` = 69 B; 1M providers ≈ 69 MB.
- Leaf chunks (binary, 8 MiB): rows sorted by coldkey,
  `coldkey(32) ‖ cumulative_rao(16) ‖ epoch_delta_rao(8)` = 56 B; 1M coldkeys ≈ 56 MB.
- Leaf hash `keccak256(bytes.concat(keccak256(abi.encode(coldkey, cumulativeRao))))`,
  tree in the canonical sorted shape (`merkle/merkle.go:17-36`), root in the
  header. Proofs are not stored; the server computes one on demand in
  O(log N) from the leaf stream, and anyone with the leaf chunks can do the
  same.

Allocation is exact in rao: `delta_p = floor(epoch_pool_rao × w_p / Σw)`, with
the remainder of at most `leaf_count − 1` rao left in the operator's next
epoch (not to the largest remainders), so no coldkey is ever rounded to zero
and the 10,000-leaf cap disappears. `cumulative_p` is the running sum since
the operator's v2 start.

Verification is streaming: the validator hashes each chunk against the index,
folds leaves into the root with O(log N) memory, sums deltas against
`epoch_delta_rao`, checks `cumulative_total_rao ≤ Σ captured` for the pool on
chain, recomputes `delta_p` for every row (O(N) time, constant memory), and
audits `attested_r` on the cohort. Byte bounds become aggregate and revisable
(`max_ledger_bytes`, e.g. 1 GiB); the retained artifact is the chunk set, so
the 32 MiB caps (`validator/artifact.go:29`; `server/startifact/artifact.go:41`)
are replaced by per-chunk caps.

### 5.5 Contracts v2

`STSettlementVault2` (new, immutable, same custody invariants as
`evm/src/STSettlementVault.sol:410-416`):

```solidity
struct Distribution { bytes32 cumulativeRoot; bytes32 ledgerHash; uint256 cumulativeTotal; uint64 epoch; }
mapping(uint256 noId => Distribution)                     public latest;      // one live root per pool
mapping(uint256 noId => mapping(bytes32 coldkey => uint256)) public paidRao;
mapping(uint256 noId => uint256)                          public allocated;   // Σ cumulativeTotal ever finalized ≤ Σ captured

function captureEmission(uint256 epoch, uint256 noId) external onlyCoordinator;            // unchanged
function finalizeDistribution(uint256 epoch, uint256 noId, bytes32 root, bytes32 ledgerHash, uint256 cumulativeTotal) external onlyCoordinator;
    // requires cumulativeTotal ≥ latest.cumulativeTotal and ≤ captured[noId]; replaces latest
function claim(uint256 noId, bytes32 coldkey, uint256 cumulativeRao, bytes32[] calldata proof) external returns (uint256 amount);
    // amount = cumulativeRao − paidRao[noId][coldkey]; requires paid + amount ≤ latest.cumulativeTotal − Σ others (tracked as distributed[noId])
function claimBatch(uint256 noId, bytes32[] calldata coldkeys, uint256[] calldata cumulativeRao, bytes32[] calldata proof, bool[] calldata flags) external;
    // OpenZeppelin MerkleProof.multiProofVerifyCalldata; credits each coldkey; transfers those ≥ minimum
function claimWithPermit(uint256 noId, bytes32 coldkey, uint256 cumulativeRao, bytes32[] calldata proof, uint256 feeRao, uint64 deadline, bytes calldata sr25519Sig) external;
    // fee ≤ policy cap, authorized by the recipient coldkey (precompile 0x403), paid to msg.sender from the claimed amount
function withdrawClaimCredit(bytes32 coldkey) external;                                     // unchanged
```

Properties:

- A root is cumulative per coldkey, so one claim pays everything accrued since
  the provider's last claim. Nothing expires; `RootMissed`, `Carried`, `carry`
  and `expireEntitlement` (`evm/src/STSettlementVault.sol:19-25,349-359,400-408`)
  are not needed. A missed root leaves `latest` in place and the captured
  stake accrues to the next root. `claimTTLEpochs` and `claimGraceEpochs`
  (`evm/src/STCoordinator.sol:40-41`) become unused for v2 pools.
- Over-allocation cannot overdraw: `distributed[noId] + amount ≤ latest.cumulativeTotal`
  and `latest.cumulativeTotal ≤ captured[noId]`; a root that lowers a coldkey's
  cumulative is unclaimable for the difference and is an audit finding.
- Credits below the transfer floor accrue exactly as today
  (`evm/src/STSettlementVault.sol:446-482`); a batch that only credits costs
  ≈25-45k gas per leaf (two warm or one cold SSTORE plus multiproof hashing),
  and the runtime `transferStake` is paid only when a credit is actually
  transferred.
- Mandatory per-epoch cost is unchanged and O(operators): capture, commit,
  finalize ≈ 0.54 Mgas measured, ≈0.0027 TAO.
- Claim cost is O(claims). The per-recipient runtime transfer is inherent to
  paying α to distinct coldkeys and batching does not remove it; what v2
  removes is the need to claim every epoch and the need to pay for proofs,
  intrinsic gas and bookkeeping per epoch. A relayer (the operator's sweep, a
  fleet owner, or anyone) claims for many coldkeys in one transaction when
  their accrual justifies it; the permit lets the relayer recover gas from
  the claim. Providers sharing a payout coldkey already collapse to one leaf
  (`protocol/payout.go:53-57`), which v2 keeps and documents as the intended
  way for fleets to be paid.

Economic honesty: at 1M providers the average weekly entitlement is
1.4 × 10⁻⁵ TAO, 1/92 of one claim's gas. No on-chain α settlement can make a
weekly per-provider payment of that size economic; v2 makes it correct (no
expiry, no exclusion, batched and amortized) rather than pretending it is
cheap. The claim-threshold and relayer-fee policy should be published with
the ledger.

`STCoordinator` changes (UUPS upgrade, `evm/src/STCoordinator.sol:924`, storage
gap `:934`): add `settlementVaultV2` and `vaultSwitchEpoch` through a
`reinitializer(2)`; route `closeOperatorEpoch`, `deferMissedEmission`,
`commitOperatorRoot` (extended with `cumulativeTotal`) and `finalizeOperatorEpoch`
by epoch; add `registerOperatorPoolV2(noId, poolHotkey, burn)` (owner) that
calls `vault2.registerPool`; keep `_validateSettlementWindow` (`:351-357`)
against the v1 vault only for epochs before the switch. `STValidatorEvidence`
is unchanged: it is anchored to the proxy address (`evm/src/STValidatorEvidence.sol:73-78`)
and reads only `policyAt`, `epochStartBlock` and `epochEndBlock` (`:139-143`).

### 5.6 Head bindings

The head channel is unchanged on chain. Two server/validator changes make its
cost independent of the pool population: head exclusion is computed from an
event-indexed set of bound client ids (`FleetBound`, `FleetBindingRevoked`,
`FleetBindingCleaned`, `evm/src/STCoordinator.sol:183-193`) instead of one
`bindingAt` per provider per epoch (`server/controller/st_controller.go:3320`),
and the validator's `CurrentBindingKVs` bound is `max_head_fleets × max_members`
rather than `max_providers` (`validator/attempt_cut_v2_head.go:52`). Head fleets'
client ids are excluded from the ledger as today (`server/controller/st_controller.go:3325-3329`).

### 5.7 Head/pool balance under Null consensus and mechanisms

Recommendation: adopt Null with `MaxAllowedUids` 2,500 and one mechanism, and
grow the head toward ≈2,000 fleets, after the approvals and validator review in
section 7. Reasons:

- A head UID is the cheapest settlement in the system: native α each tempo
  on the fleet's own hotkey, no artifact row, no claim, no gas
  (`WHITEPAPER.md:970-974`). Every provider the budget moves into the head is
  one fewer pool leaf and, more important, it is the providers with the
  largest breadth, which are also the ones whose pool entitlements would
  justify claims. The pool is then purely the long tail, where cumulative
  roots and coldkey aggregation (5.5) are the right tool.
- Under Null the submitted row is paid exactly, so θ, the treasury row and
  the pool/head split are what the validator computes, not what a stake
  majority happens to run (`WHITEPAPER.md:1024-1026` no longer applies). This
  is also the cost: the single largest-stake UID decides everything, so the
  owner's stake on UID 1 must stay the largest on SN25 and be monitored; an
  attacker who out-stakes it takes the sole permit and the whole row until
  the owner switches back to Yuma, which is itself rate-limited and needs an
  empty commit queue (`subtensor/pallets/subtensor/src/subnets/mechanism.rs:124-141`).
- Head size and θ are coupled by the whitepaper's constraint that the
  lowest-paid head UID should out-earn the highest-paid pool provider
  (`WHITEPAPER.md:1040-1042`). With 2,400 head fleets at θ = 0.3 the average
  head UID earns ≈0.26 α per epoch against a pool average of ≈0.015 α at 100k
  providers; the policy should raise θ with the head count under the same
  constraint, as a scheduled policy, not per-validator discretion.

Changes this needs beyond section 7's approvals: `maximum_head_fleets` lifted
from its [1, 200] range (`protocol/policy.go:423`; `deploy/mainnet/policy-v1.yml:61`)
and the validator's `max_head_entries` with it (`validator/release_head_v2.go:116-131`);
a mainnet fleet-binding batcher, since `bindFleetMember` is one transaction per
client (`evm/src/STCoordinator.sol:725-794`) and the existing batcher is
testnet-only (`evm/src/STFleetBatcher.sol:1-12`); head-score EMA and immunity
settings sized for ≈2,000 fleets, since trimming still removes the lowest
emitters (`subtensor/pallets/subtensor/src/subnets/uids.rs:323-327`); and in the
v2 vault a coordinator-only `captureEscrowSurplus(noId)` that moves
`liveEscrowStake − escrowAccounted` into the next distribution, so Null's
stake-proportional dividends on the escrow UID (3.7) are paid to providers
rather than stranded.

Mechanisms. The budget is shared, so two mechanisms give 1,250 UIDs each.
Putting the head in its own mechanism would make θ the owner's on-chain
emission split (`subtensor/pallets/admin-utils/src/lib.rs:2119`;
`subtensor/pallets/subtensor/src/subnets/mechanism.rs:321-334`) instead of a
validator-software convention, and would isolate head and pool
rows. It costs the validator a second row per epoch (CRv4 per mechanism), the
removal of the single-mechanism assumption (`validator/recycle_observation.go:187-192,210`;
`validator/recycle_planner.go:188`), and half the head capacity. Under Null the
split is already paid exactly from one row, so the mechanism buys little
today. Keep one mechanism; revisit when independent validators join and θ
needs enforcement that does not depend on which validator holds the permit.

### 5.8 What changes, by component

| Component | Change |
| --- | --- |
| Policy schema v2 | remove `shares_total_bps`, `rounding`, `claim_ttl_epochs`, `claim_grace_epochs` for v2 pools; add `cohort { members, carried_cohorts, replacement_bps, strata, a_min }`, `attestation { formula, lambda, zero_weight_delta }`, `claim { min_claim_rao, max_relayer_fee_bps }`; new `policy_id`, new hash scheduled by the Safe (`evm/src/STCoordinator.sol:312-327`) |
| Verify API | eligible-set commitment per tempo; assignment by validator-supplied index with membership proof; cohort-member-only assignment; `assign_n` = cohort size with the seed reveal in the sealed measurement |
| Validator | cohort draw and reveal; bounded state `m × (1+K)`; `Q̂_n` with interval; attestation audit; streaming ledger verification; evidence kind `AUDIT_CHALLENGE`; bounds schema v3 with `max_cohort_members`, `max_carried_cohorts`, aggregate `max_ledger_bytes` |
| Server | ledger v2 builder (streaming, chunked, content-addressed), on-demand proofs, `GET /sn/pool/claim` v2 returning `(noId, coldkey, cumulativeRao, proof, vaultV2)`, eligibility without the exposure floor, attestation feed, event-indexed head set, roster v2 chunked |
| Contracts | `STSettlementVault2` with `captureEscrowSurplus`; coordinator upgrade as in 5.5; a mainnet fleet-binding batcher |
| Provider tooling | claim daemon v2 (one cumulative claim, threshold, optional permit); `snclaim` v2 selector |
| Subnet hyperparameters (owner) | `sudo_set_epoch_consensus(25, Null)`, then `sudo_set_max_allowed_uids(25, 2500)`; one mechanism |
| Treasury approval | re-signed with `maximum_subnet_uids` 2,500 (`validator/recycle_approval.go:48`); validator reviewed under Null permit, dividend and bond semantics |

## 6. Alternatives considered

- Raise `max_providers` to the population. Requires a new economic approval
  for a fixed dimension (`validator/production_capacity.go:138-154`), makes
  validator memory and the transition O(N) (≈414 MB at 1M), and still cannot
  reach `a_min` for 1M providers with 2.1M assignments per epoch. Rejected.
- Shard the census across validators. 1M / 4,096 ≈ 244 validators; SN25's
  validator permit count is at most 64 (`WHITEPAPER.md:1618`). Rejected.
- Pay only measured providers (the stopgap, permanently). Closed set, biased
  estimator, pays for being sampled. Rejected beyond the emergency window.
- One UID per provider. 256 UIDs (`WHITEPAPER.md:1617`). Rejected, as in
  `WHITEPAPER.md:927-929`.
- Operator-held off-chain payment of small balances. Breaks "every α of the
  miner channel flows contract → provider" (`WHITEPAPER.md:924-925`) and the
  post-cutoff rule against new off-chain fallback (`mainnet/LAUNCH.md:931-934`).
  Rejected.
- Keep per-epoch roots with a wider share denominator (e.g. 1e18). Removes
  the 10,000-leaf cap but keeps one claim per provider per epoch and the
  expiry/carry machinery. Subsumed by cumulative amounts.
- Per-epoch roots plus multiproof batching only. Saves ≈30-40% per claim; the
  runtime transfer and the per-epoch cadence remain. Insufficient alone;
  batching is kept as a component.
- Zero-knowledge proof of the ledger computation. Validators can recompute the
  ledger in O(N) streaming; a proof system adds a trusted toolchain for no
  present need. Deferred.
- Native batch stake transfer. A runtime change outside this project; it
  would lower the per-recipient floor and should be requested, but v2 cannot
  depend on it.
- Public-beacon cohorts without a validator VRF. Verifiable but predictable
  by providers before measurement. Rejected in favor of VRF-then-reveal.
- Replace pool payments with Null's 2,500 UIDs. The budget covers 2.4% of
  100k providers and 0.24% of 1M (`subtensor/pallets/subtensor/src/subnets/mechanism.rs:35`),
  each UID costs a registration burn and a public, opt-in binding
  (`WHITEPAPER.md:1356-1358`), and the chain still prunes by emission. Adopted
  as the head's growth path (5.7), rejected as a replacement for the pool.
- A separate head mechanism now. Chain-enforced θ and row isolation, at the
  price of half the UID capacity, a second validator row and the removal of
  the single-mechanism assumption. Deferred (5.7).

## 7. Migration and rollout

Phase 0, now (epochs 1-3): the stopgap. Keep the cohort, but (a) rotate the
lifetime set: make membership expire after K epochs out of cohort so the
validator's carried quality and the server's lifetime set agree and the set is
not closed; (b) record the cohort size and seed in the server's published
epoch summary so the artifact's exclusions are explainable; (c) do not pay on
the basis of this cohort beyond the emergency window without stating in the
artifact that eligibility was cohort-limited. The per-provider share cap and
the artifact caps are not reached at ≤ 2,000 eligible providers.

Phase 1, validator and server measurement (epochs 3-6, no contract change):
eligible-set commitments, VRF cohorts, bounded validator state, attestation
feed and audit, `Q̂_n` interval. Requires a new signed validator config (bounds
schema v3; the production approval binds the config hash,
`validator/production_capacity.go:158-169`) and a signed server `verify.yml`.
The policy hash does not change yet, so the treasury approval and coordinator
policy stay. Eligibility still needs `a_min` from the server's own assignment
records until phase 2, so the cohort governs who is paid during this phase;
make the cohort as large as the bound allows (4,096) and stratify.

Phase 1b, subnet mode and UID budget (owner, in parallel with phase 1):

1. Confirm UID 1 holds the largest stake on SN25; under Null it is the only
   permit (`subtensor/pallets/subtensor/src/subnets/mechanism.rs:166-176`).
2. Re-sign the treasury approval with `maximum_subnet_uids` 2,500
   (`validator/recycle_approval.go:48,227`) and review the validator under Null:
   permit and threshold checks (`validator/TREASURY-PRODUCTION.md:13`), the
   dividend columns it reads (`crv4/runtime_profile.go:54`), the treasury
   accounting of stake-proportional dividends on the reserve and escrow UIDs
   (3.7), and head bounds (5.7). The owner-validator form stays as approved
   (`validator/TREASURY-PRODUCTION.md:13`).
3. In an admin window with no pending timelocked commit (schedule the CRv4
   reveal cadence around it; `mechanism.rs:131-141`), `sudo_set_epoch_consensus(25, Null)`
   through the `ur-owner` multisig, then after the owner rate limit
   `sudo_set_max_allowed_uids(25, 2500)` (`subtensor/pallets/admin-utils/src/lib.rs:620-670,1513-1540`).
   Neither call moves ownership or alpha; the coldkey-swap rule stands
   (`mainnet/LAUNCH.md:222-235`).
4. Schedule policy and validator config successors that raise
   `maximum_head_fleets`, `max_head_entries` and θ together (5.7); grow the head
   over several epochs so immunity and EMA cover each cohort of new fleets.
5. Keep the Yuma return path rehearsed: `sudo_set_epoch_consensus(25, Yuma)`
   restores the saved validator count (`mechanism.rs:177-185`) and needs the
   same empty commit queue.

Phase 2, ledger and contracts (epochs 6-10):

1. Deploy `STSettlementVault2` with its bootstrap: `registerEscrow` (one burn;
   `evm/src/STSettlementVault.sol:191-209` pattern), `setCoordinatorOnce(proxy)`
   (`:178-186`). Record the plan and receipts as in `mainnet/BOOTSTRAP-CONTRACTS.md`.
2. Coordinator upgrade through the Safe (1-of-1, `mainnet/LAUNCH.md:47-52`):
   `upgradeToAndCall` with `reinitializer(2)` setting `settlementVaultV2` and
   `vaultSwitchEpoch = E_s`. Verify the storage layout against the generator
   (`evm/README.md:50-70`) and the 12-byte size margin (`evm/CLAIM-RECOVERY.md:52-54`).
3. Register the v2 pool hotkey per operator (`registerOperatorPoolV2`; new
   UID, burn, immunity) at least one epoch before `E_s`.
4. `schedulePolicy` with policy v2 effective at `E_s` (`evm/src/STCoordinator.sol:312-327`);
   re-sign validator, server and treasury configs for the new policy hash
   (`mainnet/LAUNCH.md:581-585`).
5. From `E_s` the validator weights the v2 pool UID; emission to UID 169 stops.
   The last v1 epoch `E_s − 1` is closed, committed and finalized on the v1
   vault as usual; its entitlement total includes all v1 carry
   (`evm/src/STSettlementVault.sol:334-336`), so nothing is stranded if that root
   allocates it. Dust deferred on the v1 pool hotkey below the transfer floor
   (`:271-282`) is captured by a final timely close or written off.
6. Compatibility window: v1 claims stay open until `E_s − 1 + 10`
   (`evm/src/STCoordinator.sol:642-645`), about nine weeks; the server serves v1
   proofs for epochs `< E_s` and v2 leaves for `≥ E_s`; the claim daemon reads
   `settlement_vault_address` per epoch (`server/controller/sn_controller.go:207`).
   After the window the v1 vault holds only expired remainders as carry, which
   can never leave it unless another v1 epoch is finalized; the owner accepts
   that residue or finalizes one last v1 root that pays it.
7. Evidence: no change; `fixValidatorEvidence` is one-shot
   (`evm/src/STCoordinator.sol:928-932`), so the existing contract remains the
   journal for both vaults.

Constraints checked: the v1 vault cannot be re-pointed (`setCoordinatorOnce`),
upgraded (`evm/CLAIM-RECOVERY.md:35-39`) or made to push funds to v2; the
coordinator's `settlementVault` is set only in `initialize`
(`evm/src/STCoordinator.sol:261`), hence the new field rather than a reassignment;
`_validateSettlementWindow` depends on the v1 vault's immutable 57,600-block
minimum (`:351-357`; live `minimumClaimTTLBlocks` = 57,600) and must not gate v2
policies.

## 8. Open questions and risks

- Runtime gas. The `transferStake` dispatch and precompile reads are the
  unmeasured part of the 250k planning figure; measure at the first finalized
  epoch with `cast estimate` and recalibrate the claim threshold.
- Attestation formula. Which operator signals define `r̂_p`, their tolerance
  λ and the zero-weight threshold on `δ_n` need simulation against the
  launch data before they enter the policy.
- Cohort size versus `a_min`. With 4,096 members and ≈510 assignments each,
  `a_min` can rise; the right value trades tighter Wilson bounds against
  coverage of small strata.
- Multi-operator cohorts. The budget of 2.1M assignments per epoch is per
  validator; with many operators the per-operator cohort shrinks. More
  validators or a higher seed pace are the levers; the pace is a signed
  policy value (`deploy/mainnet/policy-v1.yml:105`).
- Server commitment honesty. The eligible-set root is the operator's claim;
  a validator can detect omitted members only through usage it sees in the
  ledger but never in `E_t`. The audit should include "attested usage from a
  provider never committed as eligible" as a `δ`-type finding.
- Alpha price. The transfer floor is TAO-denominated; a falling α price raises
  the α minimum and defers more credits. Cumulative roots make this harmless
  for correctness but lengthen time-to-payment.
- Relayer fee abuse. A permit bounds the fee and names the relayer; a
  provider that signs a careless permit can still overpay. Default clients
  should sign permits only for the operator's published sweep relayer.
- Head promotion. A client promoted mid-epoch is excluded from the ledger
  from that epoch; its cumulative continues from the pool balance it had.
  Demotion resumes accrual. The ledger must carry the binding generation as
  the artifact does (`payoutartifact/artifact.go:50`).
- Residual v1 carry. Whether to finalize a last v1 root purely to pay carry,
  or to accept the residue, is an owner decision at `E_s`.
- Operator bond. Deferred; decide before admitting a second operator.
- Stake-rank capture under Null. The largest-stake UID decides the entire row
  (`subtensor/pallets/subtensor/src/epoch/run_epoch.rs:838-854,1335-1344`); the
  owner's stake must stay the largest and the return to Yuma is rate-limited.
  Define the monitoring threshold and the response before switching.
- Null dividends on non-validator UIDs. Reserve, escrow, pool and head UIDs
  all earn stake-proportional dividends under Null (`run_epoch.rs:940-951`).
  The reserve's share changes the measured 9/10; the escrow's share is
  stranded until `captureEscrowSurplus` exists. Quantify at the first Null
  epoch.
- Null trimming. Lowering `MaxAllowedUids` under Null is an explicit,
  batched owner operation (`subtensor/pallets/subtensor/src/subnets/uids.rs:196-245`);
  plan it before raising the budget, not after.
- Runtime drift. Null is in runtime 475 and spec 477; every runtime change
  stops automated writes until revalidated (`evm/README.md:183-186`), and the
  validator's runtime-successor admission applies (`mainnet/LAUNCH.md:26-28`).
