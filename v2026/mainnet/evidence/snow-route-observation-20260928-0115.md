# Snow owned-route observation

The signer-free `eth_chainId` probe against
`http://172.28.208.185:9944` ran from **2026-09-28 01:14:44 UTC** through
**01:19:47 UTC**. All 21 attempts returned HTTP 502 with the nginx error body.
The final curl exit was 22. There is no returned chain ID, genesis, runtime or
finality evidence in this capture.

This probe used a 300-second retry window, a 15-second per-attempt timeout,
5-second connection timeout and 15-second recovery delay, with at most 20
retries. Actual elapsed time was 303 seconds: curl's retry window permits the
last admitted attempt to finish. It is a bounded read-only diagnostic, not a
production retry-policy or availability qualification. No public fallback or
chain write was used.

The raw directory is
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T0115Z`.
Its rounded directory label is separate from the exact timestamps above.

| Retained file | SHA-256 |
| --- | --- |
| `chain-id-response.body` | `61b30d408583991fd69f3dec694e154cb652471e663328ad9c8482c9021ab5db` |
| `http-status.txt` | `38500003733a95b652379a66ee15b3192b116bfcfcdb1aa830956af22a82c66c` |
| `curl.stderr` | `a3403a7ced4d95cce806a963ce19600e1b20807857723c4d0b2e8916ecfde0f2` |

The [preceding observation](snow-route-observation-20260928-0030.md) also
returned HTTP 502. The last successful identity observation remains testnet
chain 945. Mainnet route cutover, expected chain 964 and independently admitted
mainnet identity are still unestablished.

## Follow-up recheck

The same read-only request and retry settings were repeated from
**02:01:36 through 02:06:40 UTC** on September 28. All 21 attempts again returned
HTTP 502; curl exited 22 after 304 seconds. The raw directory is
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T0201Z`.
Its `body`, `http-status` and `curl.stderr` hashes respectively match the three
hashes above. The exact timestamps, exit and `response.sha256` are retained
there. This supplies no new chain identity or evidence of the upstream cause;
mainnet cutover is still unconfirmed. No public fallback or transaction was used.

A further request ran from **02:34:18 through 02:39:24 UTC** on September 28
with the same retry bounds. All 21 attempts returned HTTP 502 and curl exited
22 after 306 seconds. The status and stderr hashes match those above; this
invocation used `--fail` and retained no error response body. Its raw directory
is `/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T0235Z`.
Mainnet identity remains unverified.

The next check ran from **04:00:29 through 04:05:32 UTC** on September 28
with the same retry bounds. All 21 requests again returned HTTP 502; curl
exited 22 after 303 seconds. The error body, status and stderr match the three
hashes above. Raw timestamps and results are retained under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T0400Z`.
This remains an availability observation with no new chain identity.

The **04:42:10 through 04:47:13 UTC** check again returned HTTP 502 on all
21 attempts, with curl exit 22 after 303 seconds. It used the same explicit
owned route, request and retry bounds. The retained `response.body`,
`http-status.txt` and `curl.stderr` reproduce the three hashes above. Exact
timestamps and exit are in
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T044210Z`.
No mainnet identity, public fallback or transaction is present in this capture.

The **05:29:35 through 05:34:38 UTC** check again returned HTTP 502 on all
21 attempts; curl exited 22 after 303 seconds using the same request and retry
bounds. The raw directory is
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T0531Z`;
its directory label is separate from the exact retained start/end timestamps.
The response body, status and stderr hashes match those above. This supplies
no mainnet identity or additional evidence about the upstream failure.

The **06:33:54 through 06:38:57 UTC** check returned HTTP 502 on all
21 attempts; curl exited 22 after 303 seconds under the same retry bounds.
Raw timestamps, body, status, stderr and exit are retained under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T0634Z`.
The body, status and stderr hashes again match the three hashes above. No
chain identity or further upstream diagnosis was obtained.

The **07:38:18 through 07:43:21 UTC** check returned HTTP 502 on all
21 attempts; curl exited 22 after 303.113 seconds with the same retry bounds.
Exact command, timestamps, response, status, stderr and result are retained in
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T073818Z`.
The response body, status and stderr hashes match the preceding captures.
The route remains unavailable; no mainnet identity or transaction was observed.

The **08:45:07 through 08:50:11 UTC** check again returned HTTP 502 on all
21 attempts; curl exited 22 after 303.088 seconds. The owned endpoint, request,
per-attempt timeouts and retry window are retained in
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T084500Z`.
This capture uses verbose timestamped stderr and retains all retry response
bodies concatenated, so its hashes differ from the preceding single-body
captures: `response.txt` is
`d17f2add25f5d68f08ff9b3556fe3edcc4cdcffa0b5a7cf61ac6e22583b75634`;
`curl.log` is
`b18f57a38c9b795114a174fd1014d1fb1b8f6f795d384bf42bdbafd49aa8c776`.
The exact timestamps, command, status census and file hashes are in
`result.json`. No chain identity was returned.

The **09:50:15 through 09:55:18 UTC** check returned HTTP 502 on all
21 attempts; curl exited 22 after 303.064 seconds with the same request and
retry bounds. Exact command, timestamps and exit are retained under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T095000Z`.
The concatenated `response.txt` hash matches the preceding capture;
timestamped `curl.log` has SHA-256
`35f6dbe6fbc647a1ff4ef4b4e79442e3aeb96f37287eee2d47ce1ede7ee22484`,
and `result.json` has SHA-256
`1fe3b3d93109a9f3b87665aceaa482d4412d7741fc8a22e05c2c73a81ef7bac4`.
The HTTP status census comes from the retained verbose log. No new chain
identity or evidence of the upstream cause was returned.

The **10:51:58 through 10:57:01 UTC** check again returned HTTP 502 for all
21 requests, exiting 22 after 303.031 seconds. The same owned route and
read-only `eth_chainId` request used a five-second connect timeout, 15-second
attempt timeout, 15-second retry delay and 300-second total retry window.
Raw evidence is in
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T105158Z`.
The concatenated response hash remains
`d17f2add25f5d68f08ff9b3556fe3edcc4cdcffa0b5a7cf61ac6e22583b75634`;
`curl.log` is `9af0bf18524ab1ca1fa07b01e8e313409630a043c1569c2736f9d884ee96304e`;
`result.json` is `51459c91772c982d52733edf0eb40ca94512f0ab0d59218c33c2437d6590a547`.

The latest check, **13:42:28 through 13:47:31 UTC**, returned HTTP 502 on all
21 attempts under the same bounds. Curl exited 22 after 303.127 seconds.
Raw evidence is in
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T134228Z`.
The response hash again matches above; `curl.log` is
`aaec6dd6876f7f99969ae03c503348d7c2aafe06797c6b6ff1e3ddd868d7ca7c`,
and `result.json` is
`99eb92bba63a7058d77db8d6a1b454aa15abe6ebae61c3f23422c6618912aa51`.
The HTTP census is in the verbose log. No new chain identity, evidence of the
upstream cause, public fallback or transaction was obtained in either check.

A later **14:17:47 UTC single read-only check** of the same owned route and
`eth_chainId` method also returned HTTP 502. Its response body has SHA-256
`61b30d408583991fd69f3dec694e154cb652471e663328ad9c8482c9021ab5db`;
raw response and timestamped observation are retained under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T1417Z`.
One failed request confirms continued unavailability at that instant but does
not establish the upstream cause or a new chain identity.

The **14:48:40 UTC single read-only check** again returned HTTP 502 and the
same response-body SHA-256. Its raw response and timestamped observation are
retained under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T1448Z`.
This adds no mainnet identity or evidence of the upstream cause.

The **15:18:04 UTC single read-only check** also returned HTTP 502 with the
same response-body SHA-256. Raw response and timestamped observation are under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T1518Z`.
The owned route still supplied no mainnet identity at that instant.

The **15:48:19 UTC single read-only check** returned HTTP 502 with the same
response-body SHA-256. Raw response and the timestamped observation are under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T1548Z`.
It supplies no new chain identity or upstream-cause evidence.

The **16:17:54 UTC single read-only check** also returned HTTP 502 with the
same response-body SHA-256. Raw response and timestamped observation are under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T1618Z`.
It supplies no mainnet identity or upstream-cause evidence.

The **17:18:38 UTC single read-only check** again returned HTTP 502 with the
same response-body SHA-256. Its raw response and timestamped observation are
under `/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T1718Z`.
The owned route supplied no mainnet chain identity at that instant.

The **17:48:02 UTC single read-only check** again returned HTTP 502 with the
same response-body SHA-256. The exact timestamp, HTTP status and response body
are retained under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T1748Z`.
The owned route still supplied no mainnet identity or upstream-cause evidence.

The **18:18:06 UTC single read-only check** also returned HTTP 502 with the
same response-body SHA-256. The exact timestamp, HTTP status and response body
are retained under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T1818Z`.
No chain identity or upstream-cause evidence was returned.

The **18:48:00 UTC single read-only check** again returned HTTP 502 with the
same response-body SHA-256. Exact timestamp, status and response body are
retained under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T1848Z`.
It supplies no mainnet chain identity.

The **19:18:40 UTC single read-only check** returned HTTP 502 with the same
response-body SHA-256. Exact timestamp, status and response body are retained
under `/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T1918Z`.
It supplies no new chain identity or upstream-cause evidence.

The **19:48:20 UTC single read-only check** again returned HTTP 502 with the
same response-body SHA-256. Exact timestamp, status and response body are
retained under `/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T1948Z`.
The owned route still supplied no mainnet identity.

The **20:18:45 UTC single read-only check** also returned HTTP 502 with the
same response-body SHA-256. Exact timestamp, status and response body are
retained under `/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T2018Z`.
It supplies no new mainnet identity evidence.

The **20:48:48 UTC single read-only check** again returned HTTP 502 with the
same response-body SHA-256. Exact timestamp, status and response body are
retained under `/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T2048Z`.
No mainnet identity was obtained.

The **21:18:31 UTC single read-only check** again returned HTTP 502 with the
same response-body SHA-256. Exact timestamp, status and response body are
retained under `/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T2118Z`.
No mainnet identity was obtained.

The **21:48:04 UTC single read-only check** also returned HTTP 502 with the
same response-body SHA-256. Exact timestamp, status and response body are
retained under `/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T2148Z`.
No mainnet identity was obtained.

The **22:17:54 UTC single read-only check** returned HTTP 502 with the same
response-body SHA-256. Exact timestamp, status and response body are retained
under `/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T2218Z`.
No mainnet identity was obtained.

The **22:47:41 UTC single read-only check** returned HTTP 502 with the same
response-body SHA-256. Exact timestamp, status and response body are retained
under `/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T2248Z`.
No mainnet identity was obtained.

The **23:16:53 UTC single read-only check** also returned HTTP 502 with the
same response-body SHA-256. Exact timestamp, status and response body are
retained under
`/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T2317Z`.
The owned route still supplied no mainnet identity.

The **23:45:37 UTC single read-only check** returned HTTP 502 with the same
response-body SHA-256. Exact timestamp, status and response body are retained
under `/mnt/data/sn-testnet/evidence/mainnet-route-observation-20260928T2346Z`.
No mainnet identity was obtained.
