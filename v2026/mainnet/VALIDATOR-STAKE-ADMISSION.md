# Initial validator stake capacity admission

`sn-mainnet activate-validators admit-stake` adds a bounded native stake
observation to the original two-validator activation journal. It checks the
majority and secondary registrations from the original signed bootstrap plan;
it does not accept a replacement plan, imported stake report or separate UID
selection. It makes read-only RPC calls and consumes the existing signed
operation allowance. Both original static units must already be installed.

```text
sn-mainnet activate-validators admit-stake \
  --approval /absolute/activation-approval.json \
  --accept-approval-hash sha256:APPROVED_FILE_DIGEST \
  --independent-public-key 0xINDEPENDENT_APPROVAL_KEY
```

Success records `admitted-stake-capacity` and the optional `stake_capacity`
projection. `status` retains that projection. Original `admit` keeps its native
prerequisite behavior. Older journals without a stake projection remain
readable. The public command still constructs no fresh-start authority;
`activation_ready` remains false. Stake capacity cannot start a validator,
authorize a signer or erase an original consumed-start liability.

## Exact current scope

The observation first performs the existing complete bootstrap and native
checkpoint admission. The added reads reuse the independently approved RPC
route under one finite signed read budget and authenticate the exact original
runtime version, code and metadata at the same finalized block. Runtime source
must match the reviewed `67dcf7f791dc495064c293f080a0702cb433e51e` profile. A
runtime upgrade requires the existing separately approved continuity process.

The owned adapter admits only the exact
`SubnetInfoRuntimeApi_get_selective_metagraph` request for the approved netuid,
fields `0,30,52,57,69` and that block. It adds no generic runtime-execution,
submission or subscription permission. The one-MiB transport ceiling and the
smaller of both signed census limits, capped at 4096 UIDs, bound the read.
Every returned hotkey and permit is compared with the complete current native
census, including non-permitted peers. Duplicate/absent hotkeys, contradictory
owner mappings, malformed peers, extra fields and incomplete vectors refuse
the whole observation. The original activation checkpoint and current numeric
block hashes are checked again after the activity reads. Cancellation discards
the whole projection.

The runtime API's `TotalStake` values are integer floors of the runtime-derived
weighted stake, which includes the runtime's parent/child and TAO weighting.
They are not raw alpha balances or exact fractional consensus weights.
The implementation uses the selective metagraph layout at the
[pinned source](https://raw.githubusercontent.com/RaoFoundation/subtensor/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/rpc_info/metagraph.rs).

The reviewed production epoch filters stake at the native threshold, preserves
the registered-owner exception, normalizes before applying validator/activity
masks, converts to I32F32, then normalizes the masked stake again. Consequently
an inactive or non-permitted peer can quantize a small validator to zero before
that peer is masked. Kappa is converted from its u16 proportion. Applied weights
also undergo separate masking and clipping, which this admission does not
authenticate. These rules come from the
[production epoch](https://raw.githubusercontent.com/RaoFoundation/subtensor/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/epoch/run_epoch.rs)
and [fixed-point normalization](https://raw.githubusercontent.com/RaoFoundation/subtensor/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/epoch/math.rs).

Current activity is recomputed from `LastUpdate` using
`max(1, ActivityCutoffFactorMilli * Tempo / 1000)`; equality at the cutoff is
eligible. The metadata must authenticate those exact storage shapes. The
deprecated `ActivityCutoff` and stored prior-epoch `Active` flags do not select
this boundary. See the
[pinned activity calculation](https://raw.githubusercontent.com/RaoFoundation/subtensor/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/utils/misc.rs).

## Conservative bounds and retained meaning

For each threshold-qualified or registered-owner slot, let `f[i]` be the API
floor. Its unknown exact stake lies in `[f[i], f[i]+1)`. With `Q = 2^32`, the
implementation bounds the first normalized integer coefficient by:

```text
Slo = sum(f[i])
Shi = sum(f[i]+1)
L[i] = floor(f[i] * Q / Shi)
U[i] = min(Q, floor((f[i]+1) * Q / Slo))
```

Zero `Slo` or a possible signed fixed-point sum overflow is refused. These sums
include eligible stake from non-permitted and inactive peers. The lower bound
for a role after the second normalization is
`floor(L[role] * Q / (L[role] + sum(U[other included peer])))`. All arithmetic
uses bounded integers; no floating-point percentage is an admission input.
The formula is a conservative interval derivation, not an assertion that the
runtime API exposes exact shares. The test reference independently enumerates
fractional I64F64 inputs and performs the actual two normalization truncations.

The projection distinguishes three facts:

- `capacity_share_lower_q32` includes every threshold/permit-eligible peer and
  the registered owner, irrespective of current activity. Both UR roles need
  eligible stake/permit and a strictly positive first coefficient. The majority
  role must strictly exceed `max(Q/2, kappaQ, Q-kappaQ)`, where
  `kappaQ = floor(Kappa * Q / 65535)`. This conservatively dominates both median
  tails; endpoint kappa cannot satisfy it.
- `active_share_lower_q32` includes only currently activity-eligible peers; an
  inactive role gets zero. `active_majority_bound_observed` reports this
  independently. Fresh registrations can pass capacity admission with zero
  active share.
- `applied_weights_influence_proven` is always false. The observation does not
  prove an applied row, current consensus influence, emissions or a 10/90 result.

The retained content hash seals the typed projection; its evidence digest
binds the complete consumed native census and storage observations. These are
assertions from the approved owned RPC route, not independent storage/finality
proofs. Source and peer equality checks do not change that trust boundary.

Full producer health, proof/client-key and deployed-contract admission, native
signer/global custody exclusion, applied effective-majority influence and signed
launch authority remain separate gates. This increment performs no live chain
observation and supplies no physical signer qualification. Independent normal,
race and causal qualification is required for the frozen source and again for
any integration that changes its source bytes.
