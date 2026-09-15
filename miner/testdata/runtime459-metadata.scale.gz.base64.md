This is the runtime459 protocol metadata artifact, encoded as gzip with a zero
timestamp and then base64. It contains no account state, endpoint, request or
observed block fixture. All transport/state tests use synthetic heads and keys.

The decoded SCALE bytes are version14, 336358 bytes, SHA256
`52256b0b4a5c682e94e1d68a7b7a5dc1ba4114cfde4fb39057be808a8443673d`
and BLAKE2b256
`cf97fac54fee756137f42e53deeeca828959a74c6d87274898db2c36a33c4fef`.
The exact459 Wasm reproduces these bytes through the existing storage-free
metadata probe. Source/artifact provenance is recorded in
[runtime-459-audit.md](../../docs/spec/runtime-459-audit.md) and the separate
runtime artifact manifest. Historical455/458 fixture bytes are unchanged.
