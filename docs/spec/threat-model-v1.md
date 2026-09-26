# Release 1.0 threat model

The release-1.0 design of the UR subnet (Bittensor SN25, netuid 25) assumes Subtensor finalized state and runtime-454 precompiles are
correct, at least two independently operated validators measure each NO, server
Ed25519 and hotkey sr25519 secrets remain uncompromised, and the shared egress
hash key is distributed only to authorized NOs/validators. Public artifacts are
untrusted until their content hash, signature, finalized boundaries, and Merkle
root reconstruct successfully.

The system defends against cross-NO deposit credit, replayed/expired bindings,
coordinator upgrades attempting custody theft, operator root omission, partial
claims and expiry, process crashes around broadcast/finality, pre-finality
reorgs, stale UIDs, self-weighting, policy/runtime mismatch, and artifact
tampering. It fails closed for writers on uncertain identity, finality, history,
or policy.

Under the zero-price launch mode (every deposit rate zero, declared in the signed
policy as `zero_rate_action: equal_demand`) the deposit audit is deliberately not
a defense: no deposit is required or audited, every active pool receives the same
implied demand, and the pool channel is steered by measured quality alone, so the
only cross-NO economic anchor in that period is the quality measurement itself.
The mode cannot arise from a misconfigured rate (an all-zero schedule is rejected
without the declaration, and a schedule with zero rates in some tiers only is
always rejected), and a validator rejects a priced-style audit under a zero-price
policy and a zero-price audit under a priced one. Outside that mode the
artifact's `total_users` is an operator attestation priced by the deposit but,
like `total_usage_bytes`, not independently verified; demand above the per-epoch
deposit cap is truncated from the weight exactly as it is from the deposit.

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
at testnet scale. Value caps, independent monitoring, key rotation, and a
mainnet 2-of-3 Safe bound these risks; they do not eliminate them.
