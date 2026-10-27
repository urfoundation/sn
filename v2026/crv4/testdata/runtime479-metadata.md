# Runtime 479 metadata fixture

`runtime479-metadata.scale.gz.base64` is the exact metadata v14 of Bittensor
mainnet runtime `node-subtensor/479/1/1`. It is one gzip member written with
`gzip -n -9` (no name or timestamp), wrapped as a single line of base64. It
contains only the metadata bytes: no RPC reply envelope, account state,
endpoint, signature, key or observed block fixture.

The bytes were read with `state_getMetadata` from Snow's Finney archive, the
launch's mainnet route (`VERSION2-LAUNCH.md` O2), at block 9,286,202
(`0x363041781fa3e8b171c2c5f16db31a3fe1ff7b3447b1deacf3df381c3c82a3d2`), the
first block whose state holds the 479 code, and again at finalized blocks
9,291,312 (`0xa60233c65b7e5a65764e0b43c382fc5cb8438fbfadd5e961100a11c9dc58048f`)
and 9,292,811, with the same bytes. Extrinsic 28 of block 9,286,202, a 2-of-3
`Multisig.as_multi` of `Sudo.sudo(System.set_code)`, carries 2,645,818 bytes of
code whose BLAKE2b-256 is the code hash below. The node is the operator's own
archive: its replies are that node's assertions, not light-client proofs, and
no second node was read.

Decoded metadata is 359,018 bytes, SHA-256
`075ee9614fee27fb83a44bda737f6803bd3f6642d57ab492321aa47dad359080`, BLAKE2b-256
`3ef17424a54b53d3630c2fde92ebff918364a2f4730c9c23e22ebdd7c21575d1`. The gzip
member is 111,865 bytes. The runtime's code BLAKE2b-256 is
`0x44e952fd1cebe9bd15c0c55cee6e1d7c6c97b44d941b79127343118dbbd342bc`. The
[runtime 479 review](../../docs/spec/runtime-479-audit.md) records the source,
artifact and interface provenance.

Unlike 478's, these bytes are not 478's with one byte changed. Runtime 479 adds
one call, two errors, two events and two storage items, and the RuntimeCall and
RuntimeEvent enums that embed them change in every item that carries them
(`TestNativeOwnerRuntime479MetadataDeltaFrom478`).

Tests bound decompression, refuse trailing data and check the size and both
digests before decoding. Neither the fixture nor any test output approves a
runtime, metadata digest, owner, device, signature or transaction.
