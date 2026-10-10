# Runtime 475 metadata fixture

`runtime475-metadata.scale.gz.base64` is the exact metadata v14 of Bittensor
mainnet runtime `node-subtensor/475/1/1`. It is one gzip member written with
`gzip -n -9` (no name or timestamp), wrapped as a single line of base64. It
contains only the metadata bytes: no RPC reply envelope, account state,
endpoint, signature, key or observed block fixture.

The bytes were read with `state_getMetadata` from the snow Finney archive at
finalized block 9,234,811
(`0x5b4bb52cc2b35b226782dd0490e94d35ab06e888732ba9db72faef6ff89f1782`). They are
byte-identical to the metadata at block 9,233,781, the first block whose state
holds the 475 code.

Decoded metadata is 357,468 bytes, SHA-256
`e181ddacd13d1050e82a2afea8e58ba5d3fc84504fa92d2884c91df8c9dd8020`, BLAKE2b-256
`983cfdabc62b0c6b08faafb47f24303999236e0022598d1b5a3ec70fbde895ff`. The gzip
member is 111,284 bytes. The pinned SN metadata probe executed the exact
official runtime (code BLAKE2b-256
`0x557634c8c31bc639ea6552e297dcfb781cd7d9bdd491a5352c151305db33d3a0`) and
reproduced these bytes. The [runtime 475 review](../../docs/spec/runtime-475-audit.md)
records the source, artifact and interface provenance.

Tests bound decompression, refuse trailing data and check the size and both
digests before decoding. Neither the fixture nor any test output approves a
runtime, metadata digest, owner, device, signature or transaction.
