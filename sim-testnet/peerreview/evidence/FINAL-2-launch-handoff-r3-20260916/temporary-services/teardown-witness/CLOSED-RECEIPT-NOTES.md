# Closed helper-teardown receipt

`SHA256SUMS` is preserved as the teardown script's original invocation
manifest. It is not an authoritative closed seal because its `find` operation
could enumerate its own manifest and files that were still open during the
owner invocation.

`CLOSED-RECEIPT-MANIFEST.tsv` was generated only after the teardown owner
joined and its stdout/stderr were final. It lists every closed receipt file in
this directory except itself and `CLOSED-RECEIPT-MANIFEST.sha256`, which are
explicitly excluded in `CLOSED-RECEIPT-EXCLUSIONS.tsv`.

The four helpers were orphaned after their transient launch shell exited, so
helper `wait(2)` exit codes are unavailable. `terminated-observed` means the
pre-signal PID/start-tick identities were bound, the exact isolated groups
received TERM, every original PID became absent from `/proc`, and all six
owned listeners closed. It does not claim service exit code zero.

`owner-launch-attempt-1.txt` preserves the first wrapper redirection failure;
no teardown script executed and no helper signal was sent in that attempt.
`preflight-attempt-2.txt` preserves the subsequent local receipt-field parse
failure, also before any helper signal. The successful witness follows in the
remaining files.
