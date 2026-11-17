# Runtime 478 metadata fixture

`runtime478-metadata.scale.gz.base64` is the exact metadata v14 of Bittensor
mainnet runtime `node-subtensor/478/1/1`. It is one gzip member written with
`gzip -n -9` (no name or timestamp), wrapped as a single line of base64. It
contains only the metadata bytes: no RPC reply envelope, account state,
endpoint, signature, key or observed block fixture.

The bytes were read with `state_getMetadata` from the public RPC
`https://lite.chain.opentensor.ai` at finalized block 9,277,117
(`0xe34591e9ffef093c8a6fb53d5a781cecd6e81bce94e17d7ddbdcd8b14b93522b`). The
public archive `https://archive.chain.opentensor.ai` returned the same bytes
at that block and at blocks 9,276,925 and 9,276,926, the first block whose
state holds the 478 code and the first block it executed. Both are third-party
nodes: their replies are assertions, not light-client proofs.

Decoded metadata is 357,499 bytes, SHA-256
`2513780705c9e9c1b9fb7e701670fee88cf8419b945100fc26ab87218b03e35a`, BLAKE2b-256
`4f96a9cb954ebe3ae0e364fe7e34ec8a0e902239832f68dfe90aaa1e4511c747`. The gzip
member is 111,140 bytes. A node built from the v478 source, started on a dev
genesis whose `:code` is the exact on-chain runtime (code BLAKE2b-256
`0x7e722b3aa5c9192c98665a5d65fa91364bf9055e21fc90efce6d59bf4ef414e1`),
executed that Wasm and reproduced these bytes. The
[runtime 478 review](../../docs/spec/runtime-478-audit.md) records the source,
artifact and interface provenance.

The bytes are the runtime 477 fixture's with one byte changed, the spec
version's.

Tests bound decompression, refuse trailing data and check the size and both
digests before decoding. Neither the fixture nor any test output approves a
runtime, metadata digest, owner, device, signature or transaction.
