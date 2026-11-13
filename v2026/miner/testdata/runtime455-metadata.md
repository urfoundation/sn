# Runtime 455 protocol metadata

`runtime455-metadata.scale.gz.base64` is the exact reviewed protocol metadata,
encoded as deterministic gzip (`gzip -n -9`) and wrapped base64. It contains
only the metadata artifact, not the Rpc response envelope, endpoint, observation
boundary, account state, credentials or unrelated deployment configuration.

The source provenance is exact commit
`67dcf7f791dc495064c293f080a0702cb433e51e`. The existing reviewed artifact manifest
is `../../docs/spec/runtime-metadata-artifacts.json`; its separate exact-Wasm
attestation remains mandatory. This fixture does not replace that attestation.

Decoded metadata is 334642 bytes, SHA256
`74c4067de4bf2eba95156e8a46c793b52fcd9862dfeb28502632e46416979ec7`,
Blake2b-256
`16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc`.
Tests bound decompression and authenticate the size and both digests against
that manifest before serving bytes at synthetic exact heads over local Http.

The release lock is not modified. Its independent runtime/source equality
test remains a failure until genuine regeneration on qualified promoted source.
