# Snow owned-route observation, September 29

At **2026-09-29 00:14:33 UTC**, a signer-free `eth_chainId` request to the
owned Snow VPN route `http://172.28.208.185:9944` returned HTTP **502** with
the nginx error body (SHA-256
`61b30d408583991fd69f3dec694e154cb652471e663328ad9c8482c9021ab5db`).
The single request used a five-second connection bound and 15-second total
bound. Exact timestamp, response, status and curl result are retained under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260929T0015Z`.

This adds no mainnet chain identity or upstream-cause evidence. The
[September 28 observations](snow-route-observation-20260928-0115.md) also
returned HTTP 502 after the last successful route identification of testnet
EVM chain 945. No public fallback, signature or chain write was used.

The **00:42:38 UTC** single read-only recheck also returned HTTP 502 with the
same response-body SHA-256. Its exact timestamp, status and response are under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260929T0045Z`;
the rounded directory label is separate from the exact timestamp. Mainnet
identity remains unavailable.
