# Snow route observation — 2026-09-27 23:55 UTC

The owned route `http://172.28.208.185:9944` still returned HTTP **502** for
`eth_chainId`. The read-only request was retried six times across approximately
one minute; all seven attempts returned the same HTTP status. No mainnet
identity, finalized position or genesis was obtained. No key was loaded and
no chain write was attempted.

The response body, terminal HTTP status and retry errors are retained in
[/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260927T2355Z](/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260927T2355Z).
This unavailable route does not change the earlier testnet observation or
supply an independently approved mainnet genesis. Offline implementation and
local qualification continue while the node is being prepared.
