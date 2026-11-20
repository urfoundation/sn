# Bounded owner-trim qualification

`owner-trim-qualify` is a separate signer-free qualification mode for a partial
SN25 owner trim. It tests whether every generation that can become removable
before a proposed mortal era expires belongs to the approved old-miner set.
Protected identities must remain immune for the whole window. Emission order
then cannot expose a protected identity, so an atomic hotkey predicate is not
an unconditional requirement for this bounded case.

**Exit 0 means the observed predicates pass under the listed unproved window
assumptions. It is never an execution token.** The result always retains
`WINDOW_ASSUMPTIONS_NOT_AUTHENTICATED`, source-to-Wasm, custody-effects,
signing/execution and actual-subset receipt/reconciliation gates.
`reset_ready`, `apply_authority` and `full_reset_completed` remain false.
No key, signer, submission, apply or caller-supplied proof boolean is accepted.

```sh
go run ./mainnet owner-trim-qualify \
  --rpc https://rpc.example \
  --policy /secure/sn25-census-policy.json \
  --window /secure/owner-trim-window.json \
  --retry-window 5m
```

Use the independently reviewed [census policy](SUBNET-CENSUS.md), including
its exact old-miner generations, protected roles and target
`trim_maximum_uids`. The separate window proposal has this shape; the hash
placeholders must be replaced with approved input bytes and an observed
canonical finalized anchor, not copied as live values:

```json
{
  "schema": "urnetwork-mainnet-owner-trim-window-v1",
  "policy_file_sha256": "sha256:<exact-policy-file-digest>",
  "finalized_hash": "0x<exact-finalized-block-hash>",
  "finalized_number": 100,
  "mortal_period_blocks": 8,
  "selection_rule": "any-subset-of-approved-old-generations"
}
```

The selection rule explicitly permits any runtime-selected subset of the
policy's approved old miners at the fixed capacity. It does not broaden that
set to new registrations, renamed identities or newly protected roles. A
policy or proposal hash identifies bytes; it does not establish independent
approval. The result retains both hashes, the complete SN25/root census,
additional raw storage and authenticated metadata defaults, and a
schema-prefixed canonical JSON SHA256 seal with empty `content_hash`.

Only power-of-two periods from 4 through 256 blocks are supported. They have
unit phase quantization, so the finalized anchor `B` is the exact proposed era
birth, with birth hash equal to `finalized_hash` and exclusive death `D=B+period`.
The output includes the exact two-byte `proposed_mortal_era_hex`. All checks
cover potential inclusion blocks `B+1` through `D−1`, including already
unfinalized predecessors and earlier extrinsics in the inclusion block. A
future execution owner must bind these exact bytes and birth hash into the
actual signed call; no such owner is implemented here. A failed or expired
proposal is not automatically extended or re-signed.

The reader obtains a fresh complete census, requires its anchor to match the
proposal, and rereads runtime identity/metadata and the additional storage at
that same hash. Overlapping timing reads must agree. Final canonical and
latest-finalized-head checks reject a moving head. One 60-second through
15-minute deadline covers all reads; the existing eight-worker and 4096-seat
bounds apply. Increase the default five-minute RPC deadline up to 15 minutes
for archive latency if needed; this never extends the proposed mortal era.
An unavailable, stale, changed-runtime or interrupted observation publishes no
partial result. Files are bounded to 1 MiB and use the existing strict JSON
and regular-file reader.

The implemented predicates are deliberately conservative:

- Both recorded registration flags must be closed. The reviewed registration
  setter requires chain `Root`; this command does not imply the owner can
  close them. Privileged reopening remains an unproved window assumption.
- Each protected seat must be in the runtime's actual owner-immune subset or
  have temporary immunity through `D−1`. The subset is limited and not equal
  to all owner-associated keys. Temporary protection requires expiry at or
  after `D`. The calculation uses runtime block arithmetic, not a caller's
  expiry or immunity boolean.
- Every seat that becomes nonimmune through expiry must exactly match a
  removable old hotkey/coldkey/registration generation, with no owner,
  validator, reserve, pool, escrow or other protected role. An approved miner
  may lose immunity during the window without expanding the approved set.
- Every registered coldkey and the subnet owner need a nonzero authenticated
  last hotkey swap whose saturating cooldown end covers `D−1`. Both subnet
  and global hotkey swaps enforce this cooldown. A zero value permits a first
  swap and therefore fails this mode. No custody or lineage assertion bypass
  exists; those alternative proofs would need separate qualification.
- Existing coldkey-swap announcements may not become executable before `D`,
  and the authenticated announcement delay must keep a new announcement at
  `B+1` from maturing before `D`. An owner announcement or dispute blocks this
  mode because the owner can be restricted from dispatching the trim.
- No native epoch may occur before expiry: check `LastEpochBlock + Tempo`,
  `PendingEpochAt`, and the `BlocksSinceLastStep > Tempo` fallback after each
  block's increment. `Tempo=0` follows the source's disabled-epoch behavior.
  A pending future manual trigger can already close the admin window, even
  when its epoch would occur after expiry. The admin window must stay open
  through `D−1`; conflicting owner triggers/settings remain unproved fences.
- An active subnet lease is unsupported. Its beneficiary can change ownership
  independently of the epoch route. A prior owner trim is also unsupported
  until the build-selected trimming rate limit is independently authenticated.
- Capacity must be a permitted strict reduction and pass the exact runtime
  whole-percent, saturating immunity test. The largest immune count occurs at
  the first possible inclusion under the unchanged-state assumptions; later
  approved-miner immunity expiry can only reduce it. Equality with the
  threshold fails. No majority-validator weight can increase this ceiling.

The no-epoch condition preserves validator permits and excludes the runtime's
conviction-based owner takeover, which can call registration directly despite
the ordinary registration flag. It is not a claim that every safe-set proof
must exclude epochs. A broader proof could allow emission changes while also
proving role, membership and ownership safety; that proof is not implemented.

Public subnet registration is another generation-level path: at the network
limit it can prune/dissolve a nonimmune subnet, independently of SN25's neuron
registration flag and epoch timing. The result explicitly leaves protection
against that pruning and netuid reuse unproved. A future execution qualifier
must establish this lifecycle invariant, for example with applicable network
immunity or a separately authenticated bound on public network registration.
It must not describe all generation churn as requiring privileged governance.

A majority SN25 validator can influence ranking only after native processing
writes the aggregate `Emission` vector. Its normal weights, commit/reveal
timing, other active stake and mechanism aggregation still matter. The
conservative workflow is to observe the finalized native result and then
qualify a short window before another epoch. High predicted emission never
substitutes for protected immunity in this mode, and the mode does not alter
the validator's standard weight logic.

Every requested original generation is retained in the output as
`may-be-removed-or-retained`, `retained-through-runtime-immunity`, or
`unresolved-original-generation`. When conditional predicates pass, removal
and residual **counts** are stated conditionally on successful dispatch;
actual residual identities are deliberately not predicted. Finalized receipt
attribution and reconciliation must establish the actual removed subset,
protected survivors, compressed UID mappings, custody effects and excluded
root behavior. The existing [fixed-plan reconciliation](OWNER-TRIM-GUARD.md)
does not accept a different subset automatically and is not that future adapter.

Exit 3 means failed predicates (with complete evidence) or an integrity refusal
(without evidence). Exit 2 means invalid input; exit 1 means transport,
interruption or output failure. A stalled finalized head is not independently
proved live by matching hashes; the finalized-progress monitor is still needed.
The observation trust boundary is the approved owned RPC and exact runtime
pins, not independent GRANDPA or storage-proof verification.

The reviewed source is pinned to
`67dcf7f791dc495064c293f080a0702cb433e51e`:
[owner call](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/admin-utils/src/lib.rs#L1979),
[trim selection](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/uids.rs#L171),
[owner immunity](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/registration.rs#L217),
[hotkey swaps](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/swap/swap_hotkey.rs#L101),
[coldkey announcements](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/macros/dispatches.rs#L2125),
[epoch scheduler](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/coinbase/run_coinbase.rs#L1195),
[conviction takeover](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/staking/lock.rs#L1180),
[lease termination](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/leasing.rs#L179),
[public subnet registration/pruning](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/pallets/subtensor/src/subnets/subnet.rs#L191),
[mortality extension](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/runtime/src/check_mortality.rs).
