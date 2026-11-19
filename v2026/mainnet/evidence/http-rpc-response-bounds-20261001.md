# Shared native HTTP response admission — 2026-10-01

The owned Substrate HTTP client now admits a complete, bounded response before
publishing a JSON-RPC result. This closes the direct/unmarked transport bypass;
configured CRV4 metadata reads and mainnet discovery already had finite response
admission. The adjacent server subscription writers now agree with the client's
decimal string identity and preserve numeric unsubscribe compatibility.

## Exact source and dependency boundary

- SN source: `58852c47c4fedc005213a0fff350d01390e4eb90`.
- Source tree: `6c6f12688443d44ac2662752ec1ead21733f10c1`.
- Base: `ad3490f8d824eac2d7fe1ec1f3953508735bc4c7`, including the public
  cross-endpoint observation documentation.
- HTTP source: `e36dd12ebbb9baccb23028f3b57652a3a94e49bb`; subscription source:
  `abd55159f6912ecfffa54a9d74de38729472ec64`. The final commit changes only the
  new test contexts to preserve the owned module's declared Go 1.21 compatibility.
- Server: `6c39d3079700c779e026a8e0244775abb7e458b4`, tree
  `07e523d05bbed1e0b6b7e1b69b52963c1814f7c1`.
- Effective Connect/SCTP `e1b5d77b5029` and SDK `5d37be3876e5` pins are unchanged.
  SN resolves GSRPC through its checked-in `third_party/go-substrate-rpc-client`.
  The receipt retains the resolved graph and unchanged module-manifest hashes.

The frozen source worktree is
`/home/by/urnetwork/temp/sn-http-rpc-response-bounds-astra-20261001/sn-final`,
on `fix/http-rpc-response-final-20261001`. Source and dependency checkouts were
clean at qualification and sealing.

## Admission and compatibility

`crv4/substrate_read_retry_http.go` already bounded marked allowlisted reads,
including the actual `RuntimeMetadataAtContext` path. Unmarked submissions and
unknown methods bypass that wrapper. Direct owned GSRPC calls and batches also
reached its generic HTTP decoder, which previously buffered without a ceiling.
Mainnet's separate discovery `rpcClient` already used finite per-call reads.

The shared owned GSRPC transport now caps each decompressed HTTP response at
33,619,970 bytes: 32 MiB + 64 KiB + 2. Declared excessive lengths reject before
reading; unknown/chunked bodies receive the same cap plus one probe byte. The
standard HTTP transport's expanded gzip body is subject to the cap. A full
16 MiB native events field still fits after hex expansion and JSON framing.

The cap is aggregate for a batch, independent of request cardinality. No
production owned-GSRPC batch caller was found in this source audit; separate
validator EVM batching uses go-ethereum. A future read owner must split a larger
batch before issuing it. The transport does not split or retry submissions.

Every acquired body is closed before publication. Non-success HTTP status bodies
are closed without being read or copied into diagnostics. Complete JSON framing,
late read/close errors, cancellation and exact response IDs are checked before
publishing any result. Batches require the complete expected unique ID set and
permit reordering and ordinary JSON-RPC errors. Notifications do not acquire a
reply-channel owner.

The inherited server subscription reply was numeric, its notification ID used a
rune conversion, and the client expected a string. Replies and notifications now
emit the same canonical decimal string. Unsubscribe decoding accepts either a
legacy uint32 number or a canonical decimal string; invalid/null owners cannot
mutate an existing ID. Paired local codecs exercise actual `Client.Subscribe`,
`Subscription` and `Notifier` writers, buffered/live notifications and the
unsubscribe handler for IDs 0, 65 and the maximum uint32. This does not enable
the generic server subscription routing disabled in the imported fork.

## Author qualification and causal controls

On exact source `58852c47`, 69 selected top-level roots across owned `gethrpc`,
`crv4` and `mainnet` pass in both normal and race modes, with zero skips. Default
`go vet` for all three packages and the `mainnet` binary build exit zero. No vet
analyzer is disabled in this final gate. Commands, test lists, raw JSONL streams,
module identities and the built binary are retained under `qualified/` in the
evidence directory below. The toolchain is Go 1.26.6 on Linux/amd64; the owned
module remains Go 1.21 and uses compatible explicit test contexts.

The selected roots cover physical bounds, gzip expansion, status handling,
complete framing, late faults, batch identity, notifications, large events,
unmarked submissions, marked metadata, existing retry/disconnect semantics,
self-sealed discovery rejection and valid runtime470 discovery export.

Restoring only the predecessor shared `http.go` causes 11 roots to fail at their
intended assertions in each mode, while six unchanged positive controls pass,
including marked metadata and the large-event allowance. Four subscription
omission variants also fail deterministically in both modes: original behavior,
notification conversion only, handshake reply only and unsubscribe decoder only.
These omission runs intentionally disable vet to reach behavioral assertions in
the reverted rune-conversion code. They produce no build failure, timeout or data
race. Earlier failed fixtures and intermediate vet diagnostics remain separately
retained; they are not final-source qualification.

Reversing the exact final `UPSTREAM.patch` in a temporary copy verifies all 212
original imported file hashes. The original provenance manifest and license
files remain unchanged.

## Sealed evidence

Evidence directory:
`/mnt/data/sn-testnet/http-rpc-response-bounds-astra-20261001`.

- `receipt.json` SHA256:
  `687d551f6170b4cf366bd5a96adb3c698818178e4fccd3e57fcbf0a6f9941d0a`.
- `SHA256SUMS` SHA256:
  `7604ab6a527f6f51e360725270c740a5740ce0a487abb4ff5827b35ec228ae24`.
- All 99 retained entries verify with `sha256sum --check SHA256SUMS`;
  `sha-check.log` records the readback.

This is local source qualification. Separate EVM/go-ethereum HTTP transports,
aggregate concurrent allocations, whole-process memory and host capacity remain
separate gates. The frozen SN `689938d6` release predates this change and the
metadata decoder hardening; selecting them requires a fresh exact-source build,
inventory and image qualification. Runtime, source/configuration, release and
deployment approvals are not inferred. No live signing, publication, transaction,
deployment or service start was performed.
