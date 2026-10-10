# Runtime metadata resource admission — October 1, 2026

Qualified source is SN `b9ee4c91fb9113db0a134df8f43a47094db3502b`, tree
`8d51056eb7450f2fb1ddfa391632f213dc67102e`, based on `2b81e214`.
Its effective graph retains server `6c39d3079700c779e026a8e0244775abb7e458b4`,
Connect/SCTP `e1b5d77b5029` and SDK `5d37be3876e5`; the owned Substrate fork is
selected from that exact SN source. Qualification used Go 1.26.6, Linux/amd64,
offline dependencies and finite local test resources. No chain write, signing,
service operation or release publication occurred.

## Defect and correction

The old shared `crv4.DecodeRuntimeMetadata` delegated untrusted metadata to an
unbounded reflected SCALE decoder. Independently reproduced 9-, 10- and 11-byte
v14 registry/pallet/nested-path claims for 65,536 entries allocated approximately
27.8, 14.2 and 16.8 MB before EOF; the latter two and truncated compact lengths
also panicked. A caller-computed metadata hash and snapshot seal did not prevent
these effects in subnet discovery. Those expected-failure controls remain at
`/mnt/data/sn-testnet/sol-metadata-decoder-baseline-2b81e214/`.

The shared boundary now checks the encoded length before hex allocation, accepts
at most 8 MiB raw metadata and uses an opt-in decoder with finite collection,
requested-storage, work and depth limits. Each collection count is bounded by
the raw input's byte length; aggregate requested storage is 64 MiB, decoded
values 2,097,152 and depth 64. A pointer-owned budget survives all decoder value
copies and becomes unusable after exhaustion. Reflected backing storage,
compact temporaries, string copies and the derived v14 lookup map reserve their
capacity before allocation. The generic decoder also propagates compact and
option read errors, rejects compact counts that would truncate through `Uint64`,
and handles custom fixed arrays without creating a slice holder.

Direct RPC metadata, initialization and discovery import use the same shared
boundary. Independently pinned owner/root preparation and native receipts still
authenticate their expected digest first. Full consumption and exact raw-byte
hashing remain required; no runtime authority is inferred. The metadata API
retains v4 and v7–v14 support. The separate, independently pinned native SDK
continues to own v15 signing metadata; this change does not add v15 to CRV4.

## Qualification

The author receipt retains exact commands, source/dependency hashes, raw logs,
test-root census and causal overlay bytes at
`/mnt/data/sn-testnet/runtime-metadata-bounds-astra-20261001/receipt.json`.
Its SHA-256 is
`a5ca31ab916bf309c3b6341f764368b4df16841a0ab3b5847f253ff821d2e0c2`.
The sibling `SHA256SUMS`, SHA-256
`29d05a465918bdb8482b50d3ff4b1e75481cdd71fc2aae6f2124eb07811293fd`,
authenticates 103 evidence files; a separate checksum readback passes all 103.

- Normal and race: **55 selected roots, four packages, zero skips**, all pass.
  These include self-hashed/sealed standalone and combined discovery snapshots,
  direct RPC, three nested vector forgeries, truncated compact forms, full
  consumption, shared aggregate storage/work/depth, derived map allocation,
  overflow, option and custom-array cases; adjacent metadata cache,
  cancellation, historical initialization and discovery consistency also pass.
- Valid consumed metadata profile, captured runtime454, runtime470 discovery
  and preview, root pin-before-storage and real payment metadata pass. All four
  offline native SDK-v15 controls execute and pass using the retained exact
  SDK artifact and source pins. An earlier diagnostic omitted the SDK fixture
  variables and skipped those four roots; it is not counted as qualification.
- The mainnet binary builds and affected `scale`, `types`, `crv4` and `mainnet`
  vet exits zero. This build artifact is test evidence, not a release.
- Ten precise omissions fail at their intended assertion in **both normal and
  race modes**: central budget, encoded size admission, aggregate allocation,
  work, custom recursion depth, derived map reservation, compact read error,
  compact integer truncation, option read error and fixed-array holder.
  No omission failure is a build error, timeout or data-race report.
- Reversing `UPSTREAM.patch` in a separate temporary copy verifies all **212**
  original imported file hashes from the unchanged `UPSTREAM.sha256` manifest.

## Remaining scope

HTTP JSON-RPC response parsing can still allocate an arbitrarily large string
before reaching the metadata boundary. Bounded HTTP response admission remains
a separate implementation/test gate; the existing WebSocket frame limit is not
an HTTP guarantee. The decoder counts requested storage and work, not exact
allocator overhead or process RSS, and custom decoders must cooperate when
allocating derived storage. No general custom-code sandbox is claimed.

The frozen release at SN `689938d6` / server `6c39d307` predates this owned-fork
source change. Selecting this hardening requires a fresh exact-source release
and dependency/image inventory; prior build and reproducibility receipts keep
their original scope. Independent network/runtime/source authority, release
approval and live role qualification remain open.
