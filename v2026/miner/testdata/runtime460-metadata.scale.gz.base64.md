This is the runtime460 protocol metadata artifact, encoded as gzip with a zero
timestamp and then base64. It contains no account state, endpoint, request or
observed block fixture. All transport/state tests use synthetic heads and keys.

The decoded SCALE bytes are version14, 336358 bytes, SHA256
`0e18eed4701255a567411bdc646c76eba355cf41bcb5fcbed8f673a458118e1a`
and BLAKE2b256
`98574118d8447c31b72c57402bdda481203f58273ae175a3b6c1da44400e934c`.
The exact460 Wasm reproduces these bytes through the existing storage-free
metadata probe. Source/artifact provenance is recorded in
[runtime-460-audit.md](../../docs/spec/runtime-460-audit.md) and the separate
runtime artifact manifest. Historical455/458/459 fixture bytes are unchanged.
