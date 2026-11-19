# Owner-recycle operator evidence, v2 blocked capsule

SN commit `301ba9c2` adds a distinct v2 measurement capsule after replaying
real V2 provider proofs and reading canonical coordinator operator state. It
checks active registry versions, pool/hotkey/UID and provider bindings, exact
deposit/conviction amounts, policy and prior source commitment/window, then
rechecks network identity and the decision block hash. The record is bound to
the original provider bytes; v1 capsule bytes remain compatible. It emits an
unsigned blocked intent, not activation authority.

The affected selector passed 138 normal and 138 race tests. Final-source
happy-path and vet passed after a naming-only field change, with the serialized
`no_id` field unchanged. Darwin arm64 CLI compilation passed. The exact test
logs, source diff, input pins and checksum manifest are retained in the
[qualification record](/mnt/data/sn-testnet/evidence/mainnet-owner-recycle-readiness-20260927/RESULT.md).
Its `SHA256SUMS` hash is
`754efc7bc57913fec7eadcbc08a9153f50b533f512965b8e06f4325b87dac83f`.

Decision-time coordinator facts are now observed, but API health, historical
key/payout custody, native validator eligibility, complete native/EVM history,
signing/custody and actual 10%/90% chain outcomes remain unproved. No live
mainnet RPC, key or transaction was used.
