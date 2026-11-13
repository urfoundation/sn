# Runtime455 testnet source and artifact review

On2026-09-08, public testnet finalized block7961421
(`0x93a1140a57ad2996e6a7bf52f64b7e25025b11a929d7bc08d734598e7eaa64e3`)
served node-subtensor/455/1/1. The exact compressed Wasm and metadata identities
are in runtime-metadata-artifacts.json. Genesis and Evm chain945 are unchanged.

The code bytes match the [official runtime455 CI artifact](https://api.github.com/repos/RaoFoundation/subtensor/actions/artifacts/10034099580)
from [release train34165123193](https://github.com/RaoFoundation/subtensor/actions/runs/34165123193),
commit67dcf7f791dc495064c293f080a0702cb433e51e. Its ZIP SHA256 is
`1e13625966816db9ee1140e9255a62b35c9717885be400793a11c7537b40c708`.
Unauthenticated GitHub ZIP download returned401; mirror-supplied ZIP bytes were
accepted only after matching that exact digest from the official GitHub Api.
Extracted subtensor.wasm has SHA256
`232bfc0d65ec2dbe4280b152e23f13879df9692d2286dd08c6ba14483deee00f`
and its Blake2b-256 equals the finalized on-chain code hash. The srtool digest's
commit/spec/code bytes match. An exact-Wasm stateless metadata execution also
reproduced both metadata hashes and its334642-byte size.

The exact [454-to455 source comparison](https://github.com/RaoFoundation/subtensor/compare/14cde6410fe8ec81a940e290c56f94a632a0988d...67dcf7f791dc495064c293f080a0702cb433e51e)
changes only runtime/src/lib.rs and two proxy-filter files. The version changes;
indirect value calls are denied for NonTransfer/NonFungible and preserved for
Any/NonCritical. All previously consumed staking, native commitments, weights,
storage/call layout and SDK dependency source remain unchanged in that diff.
This bounded comparison supports encoding compatibility, not permission to
accept unknown future versions or reuse455 authority at a454 block.

The [official Rust CI](https://github.com/RaoFoundation/subtensor/actions/runs/34165122997)
passed workspace/all-features nextest and documentation tests on the exact
commit. Those upstream results and the local metadata probe corroborate the
artifact; neither replaces our deterministic client tests or complete release
gates. The current client must still authenticate exact version/code/metadata
at its selected finalized block before signing.

At observation time, testnet deployment/smoke checks succeeded but the upstream
mainnet multisig proposal was waiting and no v455 tag was published. Therefore
the455 release lock uses source_ref_kind=commit, source_ref_name equal to the
exact commit and source_commit equal to that same value. source_tag and both
upstream mainnet proposal/timepoint fields must be empty. Never reuse454's tag
or mainnet timepoint. Historical451–454 artifacts stay available only through
their strict evidence/replay identities.

The source checker retains its historical454 command name and29-file
attestation. Its455 manifest now includes33 files: the former29, both changed
proxy files, and the precompile dispatcher and address-mapping implementation.
It also attests metadata source for all five artifacts. The existing
exact-Wasm checker runs all five finite probes concurrently. The live identity
and stake tests remain explicit read-only opt-ins and now pin455.

Original raw Rpc/CI responses, ZIP, digest and probe receipts were retained under
`temp/sn-runtime-455-audit-v1-OlFoph89` and
`temp/sn-runtime-455-wasm-v1-T5OEknEB` during implementation. They are audit
inputs, not the final signed campaign archive or independent storage-trie
proofs. The live campaign must retain its own original observations and satisfy
the final on-chain and artifact replay requirements.

## Custody mapping correction

Runtime455's [pinned dispatcher](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/precompiles/src/lib.rs#L280-L290)
routes address `0x09` to BN128 addition. It does not expose Ethereum's EIP-152
Blake2 compression precompile. A correct 213-byte EIP-152 request therefore
cannot establish custody on this runtime; Foundry's native Ethereum mapping
previously hid that mismatch from the local probe tests.

The [address-mapping precompile](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/precompiles/src/address_mapping.rs#L38-L72)
is at index2060 (`0x080c`). `addressMapping(address)` has selector `0x0494cd9a`
and returns the runtime's raw32-byte AccountId. The [runtime configuration](https://github.com/RaoFoundation/subtensor/blob/67dcf7f791dc495064c293f080a0702cb433e51e/runtime/src/lib.rs#L1074-L1084)
uses `HashedAddressMapping<BlakeTwo256>`. The probe keeps the known answer
`mirror(0x1111111111111111111111111111111111111111)` =
`0x32f955c958e51189a4921aed41ef00818f7368dfaec8d9969f091006f8066228`,
and the Go harness independently checks the probe's own coldkey.

`Blake2b.mirror` now calls that runtime mapping and requires exactly32 returned
bytes. `hash256` remains a local Ethereum reference used by tests and Foundry
scripts; Subtensor contract custody never falls back to it. A failed mapping
leaves the battery's custody checks unsuccessful while preserving diagnostics
from the other precompile families. The full conformance gate still rejects it.

The probe constructor, immutable owner/netuid, storage and battery ABI remain
the same. Its deployed code is immutable, so an existing failed probe needs a
new, explicitly bound deployment and fresh battery/value observations. Native
commitment evidence belongs to its original source/probe generation and must
be carried with that provenance rather than replayed over a later fleet
renewal. Release coordinator, reserve, settlement vault and fleet contracts do
not import this library; this correction does not require replacing them.
