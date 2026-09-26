# Release 1.0 feature inventory

Active in release 1.0 of the UR subnet (Bittensor SN25, netuid 25): atomic per-operator demand deposits, priced per GiB and per distinct user from the signed payout artifact (`total_usage_bytes`, `total_users`) and weighted as that audited usage at the conviction-zero rate; an explicit zero-price launch mode (`zero_rate_action: equal_demand`: no deposit required or audited, every pool at implied demand 1); prospective conviction; scheduled
policy; independent per-NO verification; two-channel theta steering; CRv4;
self/owned-NO masking; many-client dual-signed fleet bindings and commitments;
immutable settlement and reserve custody; provider-level payout artifacts;
exact 10,000-bps Merkle shares; claim TTL and same-operator carry; finalized
event indexing; public content-addressed history; and the real-testnet harness.

Explicitly deferred: validator effort bounty/fees, validator registration in the
EVM contract, proof-of-honest-routing, payout-grade destination diversity, fully
on-chain governance, and mature mainnet timelock policy. Deferred symbols must
not appear in active contract/server/validator interfaces.
