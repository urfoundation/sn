# Runtime 481 metadata fixture

`runtime481-metadata.scale.gz.base64` contains the exact metadata v14 captured
at finalized Finney block 9,320,004
(`0xad1eba66370988bd104b88f5d8e72053a9623fcd00ed7fa1fed7f1a248814ec3`)
on 2026-10-19. The node reported `node-subtensor/481/1/1`. These are the
node's assertions, not a light-client proof or an independent second-node
confirmation. The fixture contains metadata only, without an RPC envelope,
endpoint, account state, key, signature or transaction authority.

The raw metadata is 364,065 bytes, SHA-256
`4dc0d25bc11d1ea15118b433ba34e78f40ae2bfbaa188757e6f67f0a3240266a`, and
BLAKE2b-256
`40db5b7feac3a8e9ab2ab000234257428826d9827ab326034a3329fdd0d50088`.
The single gzip member is 113,781 bytes, compressed at level 9 with
no name and a zero timestamp, then wrapped as one base64 line. Tests bound
decompression and check exact length and both raw digests before decoding.

At that block, the 2,652,697-byte runtime Wasm has SHA-256
`48225f3199bf317966d0214588bc45fb49c28c3e7bd510293bb4da7d6aea224b` and
BLAKE2b-256
`5fb504c1c793a3ddb5456f189ddef98b5146853aea6ff998501f1b6c6eb720bc`.
The captured runtime version reply declares `SubnetInfoRuntimeApi` version 3.
Metadata v14 does not describe that API's method signatures, so matching
metadata interfaces alone cannot admit its version change.

The reviewed source is `6a972693ae9580541225bd37696ac2fee26b8ae9`, pinned by
[the source file manifest](../../docs/spec/runtime-v481-source.sha256). The
[official release](https://github.com/RaoFoundation/subtensor/releases/tag/v481)
and [immutable source commit](https://github.com/RaoFoundation/subtensor/commit/6a972693ae9580541225bd37696ac2fee26b8ae9)
are separate provenance records. This fixture does not establish reproducible
source-to-Wasm equivalence, execute that Wasm or provide deployment approval.

The complete structural comparison with 479 records 17 added items and 25
changed items. The additions are one admin call, twelve errors, one event and
three storage items. Changed wrappers embed RuntimeCall or RuntimeEvent; the
other changes are System.Version and the appended EmissionWeights rate-limit
variant. The tests refuse every other removal, addition or structural change.
The source review separately covers economic changes: metadata compatibility
alone does not imply identical emission allocation.

The selective-metagraph method and its complete 77-field SCALE layout are
unchanged. Its API3 declaration is admitted only with the exact runtime tuple
`node-subtensor/481/1/1`; unknown tuples and API versions remain refused. Source
catalog membership and matching metadata grant no signing or deployment
authority. Artifact authentication, signed successor opt-in, block ownership,
response decoding and activation approval remain separate requirements.
