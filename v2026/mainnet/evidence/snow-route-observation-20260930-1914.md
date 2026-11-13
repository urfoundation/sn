# Snow VPN RPC read-only observation — 2026-09-30 19:14 UTC

Two read-only JSON-RPC POSTs to `http://172.28.208.185:9944` were made with
`curl --max-time 7` from the SN workspace. `chain_getBlockHash(0)` and
`eth_chainId` each returned **HTTP 502** with the same 150-byte nginx HTML
body (SHA-256
`61b30d408583991fd69f3dec694e154cb652471e663328ad9c8482c9021ab5db`).
No JSON-RPC value, genesis hash, EVM chain ID, finalized block or runtime
identity was observed. This does not establish the route's backend, its sync
state, or its mainnet readiness. The operator separately reports that the
mainnet node is still synchronizing. No signer, transaction or deployment was
used.

Follow-up read-only probes at approximately **19:41 UTC** and **20:16 UTC**
repeated both methods against the same VPN route with the same seven-second
client limit. Each returned HTTP 502. A third follow-up at approximately
**20:50 UTC** also returned HTTP 502 for both methods. A fourth at approximately
**21:25 UTC** again returned HTTP 502 for both methods. A fifth at approximately
**21:55 UTC** returned the same HTTP 502 for both methods. No follow-up supplied chain identity
or sync evidence.

At approximately **22:13 UTC**, a further `chain_getBlockHash(0)` read-only
probe again returned HTTP 502. The EVM method was not repeated in that probe.

Further `chain_getBlockHash(0)` read-only probes at approximately **22:20**,
**22:45**, **23:11** and **23:30 UTC** each returned HTTP 502 with the same
150-byte response body and SHA-256 as above. Each used an eight-second client
limit. These responses add no chain identity or synchronization evidence; the
EVM method was not repeated in these probes.

At approximately **23:40 UTC**, both `chain_getBlockHash(0)` and `eth_chainId`
were retried with eight-second client limits. Each returned HTTP 502 with the
same 150-byte nginx response and SHA-256. Neither method returned chain data.

The same two read-only methods at approximately **23:50 UTC** again returned
HTTP 502 with the identical response body and SHA-256. Mainnet identity and
sync state remain unobserved.

At approximately **00:00 UTC on October 1**, both methods again returned HTTP
502 with the identical 150-byte response and SHA-256. This still supplies no
chain identity or synchronization evidence.

At approximately **00:20 UTC on October 1**, both methods still returned HTTP
502 with the same 150-byte response and SHA-256. No route cutover or mainnet
identity was observed.

At approximately **00:40 UTC on October 1**, both native genesis and EVM
chain-ID reads again returned HTTP 502 with the same 150-byte body and SHA-256.
Mainnet chain identity remains unobserved.

At approximately **00:58 UTC on October 1**, both methods returned HTTP 502
with the same 150-byte response and SHA-256. The mainnet chain remains
unidentified from this route.

At approximately **01:18 UTC on October 1**, both identity methods again
returned HTTP 502 with the same 150-byte response and SHA-256. No mainnet
genesis or EVM chain ID was observed.
