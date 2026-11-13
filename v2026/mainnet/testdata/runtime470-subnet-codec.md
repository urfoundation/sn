# Runtime 470 subnet protocol fixture

`runtime470-subnet-codec.scale.gz.base64` contains a projection of the public
runtime 470 metadata wire schema. It contains no observed chain state,
accounts, endpoints, signatures or keys. All test state and code bytes are
synthetic. This is not a deployable or approved runtime artifact.

The projection retains the portable type registry, the SubtensorModule and
AdminUtils pallets, the 26 storage definitions consumed by `subnetStorageSpecs`,
and `MaxImmuneUidsPercentage`. All type, field, variant, storage and constant
documentation is removed, along with other pallets and constants. The consumed
key/value types, hashers, defaults, call indices and argument shapes remain
unchanged. It is 99,214 bytes before gzip and has Blake2b-256
`cdb975f33cf23ba0df2279208feeacdcf0e629f4cd0d0b3e972ee63d1ebdc0a4`.

The input metadata is identified by the [runtime 470 audit](../../docs/spec/runtime-470-audit.md):
354,056 bytes, Blake2b-256
`8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34`,
bound to the official artifact at source
`923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d`. Full-metadata qualification and the
projection generator are retained outside the repository under
`/mnt/data/sn-testnet/mainnet-runtime470-census-20261001/`.

The checked-in tests qualify synthetic state through this protocol projection.
Separate external tests exercise the exact full official metadata; neither
test supplies independent network, runtime, owner or removal approval.
