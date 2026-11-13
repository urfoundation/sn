# Runtime 470 recycle protocol fixture

`runtime470-recycle-codec.scale.gz.base64` is an identity-free projection of the
public runtime 470 metadata. It retains the type registry, pallet/call/event
indices, extrinsic signed extensions, and 11 consumed storage definitions:
NetworksAdded, SubnetOwner, NetworkRegisteredAt, Tempo, LastEpochBlock,
PendingEpochAt, AdminFreezeWindow, OwnerHyperparamRateLimit,
LastRateLimitedBlock, RecycleOrBurn and System.Account. System.Events is also
retained for exact dispatch/fee decoding. All constants and documentation are
removed. No observed state, endpoint, account, signature or key is included.

The projection is 99,605 bytes before gzip; gzip is 30,825 bytes. Its
Blake2b-256 is `9d4e74ec7e5e4712675bd6cdfd2e3701cb413d5294bcacb3e13118df1c05c92d`.
The full public input is 354,056 bytes, Blake2b-256
`8b1c467c05efc33e2a8f546bd63ca263d24fc11e89284c072ee7b18e58b4cb34`,
as identified in the [runtime 470 audit](../../docs/spec/runtime-470-audit.md).
Source is official `923fd1fa7d6eadad3ec16f3941826b86c9c3aa1d`.

Generator and separate full-artifact qualification are retained under
`/mnt/data/sn-testnet/astra-owner-recycle-20261001/`. The synthetic metadata15
placeholder used in offline framing tests is deliberately not a qualified
RFC78 artifact. Neither fixture nor test output approves any production runtime,
metadata digest/proof, owner, device or transition.
