# Release 1.0 threat model

The design assumes Subtensor finalized state and runtime-451 precompiles are
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

Residual release-1.0 risks are validator collusion/Sybil stake, routing proofs
that do not cryptographically prove honest transit, shared-hash key enumeration
or leakage, runtime governance changes, and economic parameters validated only
at testnet scale. Value caps, independent monitoring, key rotation, and a
mainnet 2-of-3 Safe bound these risks; they do not eliminate them.
