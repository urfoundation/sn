# Owner-recycle proposal preview

`PreviewOwnerRecycle` is a signer-free successor-policy planning seam. It produces
one validator's proposed head/tail/owner row and refuses activation through
`AdmissionError()`. It has no chain client, signer, transaction method or connection
to `ReleaseSteerer.SubmitOnce`. The existing release policy and its canonical hash
remain unchanged. No production configuration accepts this draft schema.

The [signed admission slice](OWNER-RECYCLE-ADMISSION.md) now adds independently
selected approval custody and an authenticated finalized owner/mode reader.
It remains read-only: measurement, envelope, intent and archive transitions are
still required, and every mainnet steering entry remains blocked.

The selected policy is a **10% target for providers within the native miner
allocation**, with the other 90% intended for runtime owner-recycle. A weight row
can express that target; it cannot enforce that allocation after Yuma. The preview
is neither a native outcome forecast nor a provider payout cap. It creates no
reserve credit, owner-wallet payment or deferred provider entitlement.

## Draft and arithmetic

The separate `OwnerRecycleProposal` binds the complete mainnet parent-policy hash,
a later policy id and effective epoch, exact `1/10` provider share, equal allocation
among usable owner destinations, and explicit network/runtime artifact pins. Its
hash identifies the draft; no signature or approval is inferred. The source profile
is pinned to Subtensor `67dcf7f791dc495064c293f080a0702cb433e51e`. A different source
needs review and a new profile; no runtime version number establishes authority.
The exact code and metadata hashes are caller assertions until an authenticated
adapter and source-to-code review exist.

1. Validate the unchanged parent policy, declared runtime/finality pins, explicit
   Recycle encoding, one mechanism, parent head-fleet limit, unique registration
   census, live-validator minimum and healthy
   operator minimum. Duplicate accounts cannot satisfy those counts. Counts alone
   do not establish operator or validator independence. Before iteration or map
   allocation, each census and the combined provider input are limited to 65,536
   entries, the full u16 uid domain. This is a preview resource bound; a global
   `OwnedHotkeys` list can be larger in the runtime and such a census is currently
   unsupported. A future adapter must additionally bound encoded replies and
   score representations before constructing these borrowed Go inputs.
2. Recognize registered `OwnedHotkeys[SubnetOwner]` and registered
   `SubnetOwnerHotkey`, as the pinned runtime does. An explicit owner hotkey already
   in the owned list is counted once. Unregistered hotkeys confer no destination.
   Registration recency and uid tie ordering match the source; the wire row is
   always sorted by uid.
3. Require provider uid/hotkey mappings to agree with the census. Reject duplicate
   providers, head/tail identity overlap and every provider/owner overlap, including
   zero-scored or masked entries. Apply existing self and controlled-recipient
   masks. The runtime's owner self-weight exception does not relax the UR mask.
4. Reuse `BuildWeightVectorExact`: normalize head/tail independently and apply the
   parent's theta **within the provider tenth**. An empty provider channel cedes
   only to the other provider channel. Both empty, zero or fully masked channels
   refuse the plan. Divide the remaining exact `9/10` equally among unmasked owner
   destinations.
5. Enforce the parent's signed `MaxWeightLimitU16` against every exact score. The
   existing cap of `32768/65535` requires at least two usable owner destinations for
   a 90% proposal. A missing second destination is a blocker, not permission to
   increase the cap, register a substitute or count one hotkey twice.
6. Reuse production `crv4.NormalizeRationalToU16`: max-upscale to 65535 and round
   half-up. Reject lost positive recipients and any integer cap violation. Global
   cap water-filling or integer repair could change the channel allocation; this
   narrow successor refuses those cases. It does not secretly rebalance them.

For head theta `3/10`, one tail and one head recipient, and two owner destinations,
the exact row is `[7/100, 3/100, 9/20, 9/20]`. The integer row is
`[10194, 4369, 65535, 65535]`. Its provider fraction is `14563/145633`, differing
from `1/10` by `-3/1456330`. Both fractions are retained explicitly. They describe
the proposed row before runtime masking and fixed-point consensus.

## Source trace and limits

All paths below are in the pinned Subtensor commit, not an assertion that the
future mainnet endpoint runs that source:

| Source | Relevant behavior |
| --- | --- |
| `pallets/subtensor/src/coinbase/run_coinbase.rs`, `get_owner_hotkeys` | Recognizes registered owned hotkeys plus the registered explicit subnet-owner hotkey. |
| Same file, `distribute_dividends_and_incentives` | Recycles actual owner mining incentives only in Recycle mode; Burn or missing storage burns. Both modes count those incentives as withheld for the miner-emission penalty. |
| Same file, `distribute_emission`, zero `incentive_sum` branch | When all mining incentives are zero, the pending server allocation joins validator alpha; an empty row does not demonstrate 90% recycling. |
| `pallets/subtensor/src/coinbase/alpha.rs`, `recycle_subnet_alpha` | Recycles alpha and reduces outstanding subnet alpha; this is not a credit to the UR reserve. |
| `pallets/subtensor/src/subnets/weights.rs`, `internal_set_weights` | Max-upscales the submitted row, checks destinations and limits, then stores it. The nearby `normalize_weights` sum-to-u16 helper is not this submission path. |
| `pallets/subtensor/src/epoch/math.rs`, `vec_u16_max_upscale_to_u16` | An already max-upscaled row with maximum 65535 is unchanged by the runtime's max-upscale. |
| `pallets/subtensor/src/epoch/run_epoch.rs`, sparse epoch | Applies validator permits, owner/self exceptions, registration and commit masks, fixed-point row normalization, stake-weighted consensus clipping, ranks and dividends. |

The preview does not implement Yuma or claim that equal 10/90 proposals yield a
10/90 final incentive allocation. Independent validators retain their own
measurements, weights and native dividends. Their stake, permits, activity,
registration/commit masks, consensus, mechanism settings, integer accounting and
other native state affect actual outcomes. The pinned sparse and dense paths also
differ in their owner diagonal handling; the UR self mask stays conservative.

**Multiple owner recipients are source-feasible.** The pinned `do_register` in
`subnets/registration.rs` permits distinct hotkeys of one coldkey on a non-root
subnet such as 25, subject to registration being enabled, correct ownership,
non-system hotkeys, registration cost/collateral and available uid capacity or a
prune candidate. `staking/helpers.rs::create_account_if_non_existent` appends each
new hotkey to that coldkey's `OwnedHotkeys`. If that coldkey equals `SubnetOwner[25]`
and both hotkeys remain registered on 25, coinbase recognizes both. A registered
explicit `SubnetOwnerHotkey[25]` is also recognized by its separate source rule.
An arbitrary registered account does not qualify. The emission recognition
function does **not** truncate to `ImmuneOwnerUidsLimit`; the similarly named
registration helper uses that limit for pruning protection. Recognition therefore
does not guarantee both recipients remain immune or registered. Pinned runtime
tests `test_incentive_to_subnet_owners_hotkey_is_burned` and
`test_burn_key_sorting` also exercise multiple recognized owner hotkeys; those
tests were inspected, not executed as part of this Go preview qualification.

No live SN25 owner census was read or registration attempted. Consequently,
source feasibility plus the two-owner arithmetic is not a claim that the actual
mainnet has two usable destinations. A single remaining usable destination
blocks this successor under the unchanged signed cap.

This is an explicit economic-policy successor to the current WHITEPAPER §13.8,
which rejects owner-burn reservation. Recycle still incurs the pinned source's
withheld-incentive accounting. The existing whitepaper, signed policy, production
configuration and submission behavior are not silently reinterpreted.

## Required before activation

- Independently approved mainnet genesis and a reviewed source-to-Wasm binding for
  the observed complete runtime identity, code hash and metadata hash. The Snow
  route's present testnet identity cannot establish future mainnet identity.
- An authenticated, metadata-checked adapter reading `SubnetOwner`, `OwnedHotkeys`,
  `SubnetOwnerHotkey`, both uid/hotkey lookup directions, registration blocks,
  explicit `RecycleOrBurn`, mechanism count, active validator permits/stake and
  native submission constraints at one exact finalized hash. The snapshot accepted
  by this pure preview is only a declared census; a matching hash in it is no proof.
- Independently verified measurement artifacts, controlled-identity masks,
  operator health and live independent-validator admission under the unchanged
  parent safety requirements. Extra owner destinations must fit the existing uid
  budget without displacing those requirements.
- A reviewed signed policy/configuration transition, scheduled activation and
  draining/reconciling existing CRv4 intents. The draft digest is not that approval.
- Causal runtime-state qualification and observation of final native miner
  allocation, provider incentive, recycled owner incentive and zero-incentive
  fallback, including independent-validator variation and withheld-emission effects.
  Proposal percentages alone cannot close this outcome gate.

Every preview remains blocked while these inputs and integrations are missing.
