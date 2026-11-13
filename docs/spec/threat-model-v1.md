# Release 1.0 threat model

The release-1.0 design of the UR subnet (Bittensor SN25, netuid 25) assumes Subtensor finalized state and runtime-454 precompiles are
correct, at least two independently operated validators measure each NO, server
Ed25519 and hotkey sr25519 secrets remain uncompromised, and the shared egress
hash key is distributed only to authorized NOs/validators. The SN25 mainnet
launch does not meet the two-validator assumption: by owner decision (October 7)
it has one NO, UR, measured by one validator, UR's own, with no independent
cross-check. Public artifacts are untrusted until their content hash,
signature, finalized boundaries, and Merkle root reconstruct successfully.

The system defends against cross-NO deposit credit, replayed/expired bindings,
coordinator upgrades attempting custody theft, operator root omission, partial
claims and expiry, process crashes around broadcast/finality, pre-finality
reorgs, stale UIDs, self-weighting, policy/runtime mismatch, and artifact
and price sheet tampering. It fails closed for writers on uncertain identity,
finality, history, or policy.

Under the zero-price launch mode (every deposit rate zero, declared in the signed
policy as `zero_rate_action: equal_demand` or by a signed price sheet entry of all
zero rates) the deposit audit is deliberately not
a defense: no deposit is required or audited, every active pool receives the same
implied demand, and the pool channel is steered by measured quality alone, so the
only cross-NO economic anchor in that period is the quality measurement itself.
The mode cannot arise from a misconfigured rate (an all-zero schedule is rejected
without the declaration, and a schedule with zero rates in some tiers only is
always rejected), and a validator rejects a priced-style audit under a zero price
and a zero-price audit under a nonzero one. Outside that mode `total_users` is an
operator attestation priced by the deposit but not independently verified, in a
payout artifact and in a usage statement alike. A payout artifact's
`total_usage_bytes` is not independently verified either. A usage statement's
`total_usage_bytes` must equal the sum of the provider rows in the chunks listed
by the ledger header of the latest root committed in the epoch's root window,
though those rows remain the operator's attested usage. A statement adds to the
ledger and changes none of its bytes, so a dispute over users cannot hold up
settlement. Demand above the per-epoch deposit cap is
truncated from the weight exactly as it is from the deposit. The audit runs at
every steering pass, so a late or wrong deposit costs the pool its weight on that
pass. Nothing an operator controls can stop a validator's steering pass: a
missing or malformed commit, ledger object, statement or deposit is a verdict for
that operator's pool alone.

A known limitation: the deposit audit uses the latest root committed in the
source epoch's root window, and a later replacement of that root does not change
the audit. A root can be replaced only while it is still unfinalized at the end of
the next epoch, as a root the ledger audit refused is. So an operator could commit
a deflated root it expects to be refused, pay the smaller deposit that root
implies, and replace the root later. Both commits are visible on chain. This is
accepted while the subnet owner runs the only operator, and is to be revisited
before other operators are admitted.

The price sheet at `https://ur.xyz/price.yml` is untrusted until verified. Only the
entries of its `signed_schedule` count, each signed by a key pinned in the software
over a plain-text line naming its chain, subnet, effective epoch, cap and every tier's
rates; the file's unsigned top-level mirror has no authority. Unsigned or forged
files are ignored: a reader keeps the last good sheet and refuses a file that drops
or changes a signed entry or adds one before an entry it holds. A compromised site
can therefore only delay news of a price or hide it, not change one. While a delay
leaves an operator and a validator on different sheets, a deposit sized on one fails
the audit on the other, which costs the pool its weight on those passes. A reader
that has never verified a sheet and cannot fetch one has no price from it: an
operator pays nothing until it can verify one, and a validator steers under the
signed policy's schedule and logs the problem. So a site that a new validator cannot
read keeps that validator from enforcing a price, but not from steering.

The pinned key can set any price up to the coordinator's on-chain cap for all
operators. That is the same authority that schedules the coordinator's policy, and
at the SN25 launch it is one key. A signed entry sets what an operator must deposit
to keep its pool weight and, through the rates and the cap, each pool's implied
demand; it moves no funds and does not touch settlement.
Readers enforce no notice period: the owner's signing tool refuses an effective
epoch earlier than the next one, but the key can still sign an entry naming an epoch
already under way. A reader refuses an entry added before one it already holds, but a
reader with no earlier copy cannot tell when an entry was added. An entry whose cap exceeds the on-chain cap stops validators until
that cap is raised or a later entry takes effect.

Runtime transfer economics are an explicit custody boundary. The reviewed
runtime's `DefaultMinTransfer`—not `InitialMinStake`—is derived from the
`InitialMinTransfer` metadata constant at an exact finalized Substrate block,
after authenticating that block's Wasm hash, checked against the public
manifest, and embedded immutably in the settlement vault. Sub-floor emission remains on its pool to accumulate;
sub-floor provider payouts become non-expiring, non-redirectable coldkey credit.
Price/precompile failures cannot advance capture or payment accounting, and
source/destination stake deltas are measured before any successful transition is
admitted. Concurrent custody and RPC adversaries continuously exercise this
boundary while the live happy path settles claims.

Residual release-1.0 risks are validator collusion/Sybil stake, routing proofs
that do not cryptographically prove honest transit, shared-hash key enumeration
or leakage, runtime governance changes, and economic parameters validated only
at testnet scale. Value caps, independent monitoring, key rotation, and the
mainnet governance Safe bound these risks; they do not eliminate them. At the
SN25 launch that Safe is 1-of-1 (owner decision, October 7), so one owner key
governs the coordinator until owners are added and the threshold raised.
