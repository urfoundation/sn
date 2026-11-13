# Snow route observation — 2026-09-28 00:29 UTC

The owned route `http://172.28.208.185:9944` returned HTTP **502** on all
seven read-only `eth_chainId` attempts. The first request began at 00:29 UTC;
the capture directory uses the rounded 00:30 label. Curl exited 22 after its
six retries. This supplies no new chain ID, genesis or finalized position.
The earlier successful identity observation remains testnet chain ID 945.

Raw evidence is retained under
[/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T0030Z](/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T0030Z).

| File | SHA-256 |
| --- | --- |
| `eth-chain-id-response.txt` | `61b30d408583991fd69f3dec694e154cb652471e663328ad9c8482c9021ab5db` |
| `http-status.txt` | `38500003733a95b652379a66ee15b3192b116bfcfcdb1aa830956af22a82c66c` |
| `curl-errors.txt` | `9239afba262abc2146ee96f6085911824cef7584a95d9dd66eda9ee2bda12548` |

The request used only the owned VPN route, a ten-second per-attempt timeout
and a sixty-second retry ceiling. No signer or chain mutation was involved.
MG-01 remains open while offline implementation and qualification continue.
