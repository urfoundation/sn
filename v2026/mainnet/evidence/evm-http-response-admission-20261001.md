# Direct EVM HTTP response admission — 2026-10-01

The miner and `stctl` direct EVM clients now share a finite physical HTTP
response owner. Geth previously decoded a successful prefix, drained the
remaining body during cleanup and retained non-success status bodies without
a size ceiling. This also let a status-body phrase such as `already known`
masquerade as the claim replay owner's recognized transaction acknowledgment.

## Frozen source and scope

- Production source: `d7689890e81a51bcf69b730dbfc8e318eec345d4`.
- Final test successor: `0dea3f26911f4fb71df68996f9dccf0050340d12`, tree
  `964f10b619fb3477f6788eb72c005f78b003da8a`. This successor changes only tests.
- Base: `bcecc30aa0e141b27dfd2bd827eb41802d53035f`, retaining the earlier bounded
  metadata and native HTTP transport source and cross-endpoint documentation.
- Server: `6c39d3079700c779e026a8e0244775abb7e458b4`, unchanged; effective
  Connect/SCTP `e1b5d77b5029`, SDK `5d37be3876e5` and go-ethereum `v1.17.0` pins
  and module manifests remain unchanged.
- Branch: `fix/evm-http-response-final-20261001`; clean source worktree:
  `/home/by/urnetwork/temp/sn-evm-http-response-astra-20261001/sn-final`.

The shared `evmrpc` constructor replaces seven direct production dial sites:
fleet current authority, fleet historical recovery, claim finalized state,
claim exact replay, claim receipt reconciliation, the shared onchain submit/view
constructor and the `stctl` session. The same fleet connection can carry native
runtime metadata/events, so its allowance must preserve that existing traffic.

Validator already has a separate 4 MiB EVM HTTP/envelope transport. Mainnet's
custom RPC profiles and the owned native GSRPC transport already have separate
finite admission. The server's operator receipt collector separately bounds
each response at 16 MiB, a collection at 128 MiB and 32,768 requests with finite
deadlines. These owners are not changed or requalified by this increment.

## Physical admission and retained authority

Each HTTP response admits at most 33,619,970 bytes (32 MiB + 64 KiB + 2), plus
one over-limit probe byte. Declared excess rejects before reading. The same cap
bounds both encoded gzip bytes and decoded bytes, including gzip header/footer
processing. The transport explicitly requests gzip so the default HTTP transport
leaves decompression to this bounded owner. Unsupported encodings are refused.

The allowance preserves a full 16 MiB native events field after hex expansion;
a valid receipt with a 4 MiB log-data field also passes. An aggregate batch has
the same fixed ceiling. Arbitrarily large valid log pages/batches are not promised
admission: the read owner must select smaller pages before issue. The transport
does not truncate, split, change the endpoint or retry a request.

The physical body is completely read and closed before geth receives an admitted
finite reader. Complete JSON framing, duplicate keys, exact singleton/batch
shape and the complete unique response-ID set are checked before any result is
published. Null results, explicit `error:null` beside a result, reordered batch
replies and typed JSON-RPC revert data retain their existing interpretation.

Non-success HTTP bodies are closed without reading or retaining diagnostics.
`rpc.HTTPError` preserves the actual status through the `http.Client` wrapper:
the existing finality reader still retries 429/503 and refuses 400. Signed POSTs
cannot follow redirects. No new retry or signing authority is created.

An oversized receipt cannot finalize or replace an original signed claim.
Existing authenticated replay may still send exactly its saved bytes; restart
can reconcile the original receipt without another send once admission succeeds.
An error-status diagnostic cannot turn that unresolved send into an acknowledgment.
WebSocket and IPC keep geth's existing scheme and transport semantics; a real
local WebSocket handshake/chain-ID control passes through the shared constructor.

## Qualification

The author gate selects 85 top-level roots across `evmrpc`, `miner`,
`miner/onchain` and `stctl`: all 23 new roots plus adjacent receipt/finality,
signed custody, nonce, fleet current/historical runtime and recovery controls.
All 85 roots pass in normal and race modes, with zero skips. Default vet and
all four package builds also pass. The toolchain is Go 1.26.6 on Linux/amd64;
no vet analyzer is disabled.

Two causal omission controls run in both normal and race modes. Bypassing only
the physical admission produces 15 intended failing roots and eight unchanged
positive roots. Restoring the seven prior dial sites produces five intended
failing roots and 18 positive roots; every miner dial owner is exercised. Final
controls terminate at their assertions, with no build failure, panic, timeout or
data race. Earlier exploratory diagnostics remain retained separately.

## Read-only compatibility observation

At 19:07:52 UTC, the exact final source queried Rao archive at
`https://archive.chain.opentensor.ai`. `eth_chainId` returned 964. Its finalized
block 9,189,924 (`0x8c3a24`) was
`0x087c49ecf3ac774cc187d06a9fc4572a0eb799b7ff9acc29045d6e2ec3cf6be7`;
both the pinned-by-hash read and the subsequent canonical-by-number read matched.
The retained probe calls only these read methods with a 90-second context.

This is compatibility with one public endpoint at one observation boundary.
It does not independently prove finality, a runtime approval or deployment
authority. It performs no signer or transaction call.

## Sealed evidence and remaining gates

Evidence directory: `/mnt/data/sn-testnet/evm-http-response-astra-20261001`.
`qualified/` retains exact commands, source identity, module graph, complete raw
test streams, exits and omission overlays. `rao-readonly-final.json` and `probe.go`
retain the live read-only observation and its exact method sequence.

- `receipt.json` SHA256:
  `542aa819a43371a2743df6c17ac43f80864d4c1d3f7bdad7e6569ab1a744e3a6`.
- `SHA256SUMS` SHA256:
  `25f7c788afb4fda30d2acf4fc096f334ad8bbaa5ced23c95752a1fc933c2dd4f`.
- All 70 retained entries verify with `sha256sum --check SHA256SUMS`;
  `sha-check.log` records the readback.

Aggregate process/concurrency memory, host capacity, independent runtime/route
and configuration authority, release approval and deployment remain separate
gates. The frozen SN `689938d6` release predates this source and the prior native
HTTP/metadata changes; selecting them requires a fresh exact-source build,
inventory and image qualification. No live signing, publication, transaction,
deployment or service start was performed.
