# Verify wire v1

The canonical binary domains and layouts for `SEED`, `EXTEND`, `ASSIGN`, and
`FINAL` are owned by `github.com/urnetwork/connect/verify_wire.go`. Every integer
is big-endian and every identifier has its fixed wire width. Requests and
assignments are Ed25519-signed over the raw canonical message.

Release 1.0 signs the raw canonical `FINAL` message. `coverage` is signed inside
that message; no effort-bounty digest or effort leaf is active. Validators must
verify the server signature before persisting a proof and co-sign the same raw
message for audit publication. The golden messages and signatures in
`connect/verify_wire_test.go` are normative fixtures; changing one domain byte,
width, order, or endian must fail both server and validator tests.

Poison and normal paths use the same routable transport, response schema,
timing envelope, and key lookup. Invalid final proof is classified as unknown,
never attributed to the last provider.
