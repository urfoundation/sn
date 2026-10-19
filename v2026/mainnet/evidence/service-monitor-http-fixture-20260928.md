# MG07 blocked HTTP fixture correction

Follow-up: the corrected 48-root normal/race census and five controls completed,
and service/legacy alert rules passed. See
[the completed receipt](service-monitor-qualification-20260928.md). The source
receipt below preserves the original failure and pre-qualification scope.

The first Terra normal capture of `a511e00aeffc85387b120b22955bda13a0568c47`
reached the real source-mismatch command assertions, then blocked while closing
its synthetic HTTP server. The handler had waited for request cancellation
without consuming the POST body. Go's HTTP/1 server starts its background
disconnect read after request-body EOF; client cancellation could complete while
this synthetic handler still waited. This is a fixture boundary defect, not
evidence of a production monitor cancellation failure.

The shared blocked-chain fixture now consumes and closes at most 8 KiB of real
JSON request bytes before signaling entry. It still exits only on actual request
context cancellation. The adjacent legacy restart test uses this same boundary
and joins the server handler; it no longer manually releases a synthetic server
wait. Existing command tests observe the actual cancellation and joined result.
Production monitor code and its cancellation/retry behavior are unchanged.

The original source tree and incomplete capture remain frozen at
`/mnt/data/sn-testnet/evidence/mainnet-service-monitor-20260928/terra-a511e00/`.
The original capture is not a passing package or causal-control result. The
corrected candidate must complete the same 48-root normal/race census and five
explicit decision controls in the Terra lane. Astra performs compile-only and
vet checks; execution results remain pending in this source receipt.

`SERVICE-MONITOR.md` also records the independent review caveat: synchronous
shared stdout/stderr can block all workers and their cancellation join. Stale
textfile alerts remain independent; bounded log export is later work.
