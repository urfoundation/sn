# Runtime467 compatibility review

Runtime467 is the current reviewed testnet artifact for SN finalization. This
review binds one finalized LAN observation, the exact upstream commit, and the
interfaces SN consumes. Earlier reviewed runtimes remain historical evidence;
they do not authorize new signing or execution.

At finalized block 8,035,539
`0x616f0e3c91b64e88b4815d760e068f9fd53c8beb7ac854fdb06bc5c34c9d5064`,
our archive-capable LAN RPC `http://192.168.1.162:9944` returned
`node-subtensor/467/1/1`. The observed Wasm is 2,541,439 bytes with SHA-256
`5a4218a3198cf276bf531643ca6813438781b72dc9fac57fa83a3ffe49f9a81a` and
BLAKE2b-256 `0x2f175dcc64196ec8a6b9235f8d7cfd84efef6c68bb925c4455949591cef9f6d2`.
The metadata is 347,304 bytes with SHA-256
`1dcdc906dc30bba1a07323fd48bd0554fdedff0ef5b5391a171a0d1b24f19093` and
BLAKE2b-256 `0xb0fae6d022b74faf948e3b98463b98b46c4738e87348e24340f91146ededa4bf`.
The raw observation and metadata are retained in
`/mnt/data/sn-testnet/qualification/runtime-467-20260918/`.

The source is immutable commit `c6bcb4a7400764c94c1d1b1938514c6c2dd3d33b`
(tag `v467`) in `https://github.com/RaoFoundation/subtensor`. The source
attestation in [runtime-v467-source.sha256](runtime-v467-source.sha256)
retains the complete prior consumed scope and adds every changed runtime,
Subtensor pallet, transaction-fee, commitments, precompile, shared primitive,
and chain-extension source path that can affect SN reads, submissions, fees, or
metadata. `scripts/check-runtime-v454-source.sh` verifies all pinned source
bytes without accepting a branch head.

The upstream change includes transaction-fee coefficients and staking/root
claim fee policies. SN does not reuse prior fee estimates: the finalization
planner reads and quotes live native fees at the selected finalized block. The
reviewed metadata baseline was regenerated from this exact artifact. The
consumed storage, calls, events, signed extensions, validator schedule layout,
and CRv4 source encoding remain guarded by `crv4` tests; any changed consumed
shape rejects the runtime before a transaction is submitted.
