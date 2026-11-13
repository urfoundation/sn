# Runtime 477 metadata fixture

`runtime477-metadata.scale.gz.base64` is the exact metadata v14 of Bittensor
mainnet runtime `node-subtensor/477/1/1`. It is one gzip member written with
`gzip -n -9` (no name or timestamp), wrapped as a single line of base64. It
contains only the metadata bytes: no RPC reply envelope, account state,
endpoint, signature, key or observed block fixture.

The bytes were read with `state_getMetadata` from the public RPC
`https://lite.chain.opentensor.ai` at finalized block 9,276,785
(`0x763f3c5a4e4441b65729feba2ee36711668a935697bc57ed7d0b77288ec9c315`). The
public archive `https://archive.chain.opentensor.ai` returned the same bytes
at that block and at block 9,267,632, the first block whose state holds the
477 code. Both are third-party nodes: their replies are assertions, not
light-client proofs.

Decoded metadata is 357,499 bytes, SHA-256
`1d9a056217f440400df497ad70d4a6a6148abc19871d1b28e52464c507c11260`, BLAKE2b-256
`5a268863a7f24c626e362103afa4f2856a589ee157ab915d91c289b1b5a6f11e`. The gzip
member is 111,140 bytes. A node built from the v477 source, started on a dev
genesis whose `:code` is the exact official runtime (code BLAKE2b-256
`0x18b52484d9c5d7523f704c86b17b2ab959fffbc6edb6766a77e9877f0ad19387`),
executed that Wasm and reproduced these bytes. The
[runtime 477 review](../../docs/spec/runtime-477-audit.md) records the source,
artifact and interface provenance.

The bytes are the runtime 475 fixture's with three changes: the spec version
byte, and two variants, `MinDifficulty` 37 and `Difficulty` 38, appended to
the `Hyperparameter` enum with its variant count.

Tests bound decompression, refuse trailing data and check the size and both
digests before decoding. Neither the fixture nor any test output approves a
runtime, metadata digest, owner, device, signature or transaction.
