# Runtime458 metadata fixture

This is the exact metadata version14 protocol artifact, encoded as one gzip
member with mtime0, then base64. It contains no account state, endpoint, signed
transaction, RPC response envelope or observed block identity.

Decoded length:335,297 bytes. SHA256:
`17ebfa2551978a1567da990ac9650e6f01802f578696c552a7c6f9f4b1391405`.
BLAKE2b256:
`040088e73e34ed5561372aa51b07b56e41cf7f390312837b074434f30452593d`.

The [runtime458 audit](../../docs/spec/runtime-458-audit.md) records exact
source/artifact provenance and the official-build seed difference. The
[artifact manifest](../../docs/spec/runtime-metadata-artifacts.json) pins the
exact code that produces these bytes. Tests bound decompression before
hashing, refuse trailing members/bytes, and use synthetic accounts and heads.
The historical455 fixture remains unchanged.
