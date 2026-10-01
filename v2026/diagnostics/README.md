# Bounded daemon diagnostics

`Exporter` owns one worker per destination. Producers offer complete records
without waiting for output. Each of at most 16 configured domains has two
16 KiB slots; one further record may be in flight. The maximum retained record
payload is 528 KiB per exporter. Offers copy only after admission. Counts
saturate at `uint64` maximum. A returned `true` means queued, not delivered.

The Linux adapter accepts journal/stream sockets, pipes and terminals. It uses
nonblocking operations on owned descriptors, preserves the caller's descriptor
flags, and never closes the caller's original descriptor. Other platforms
explicitly refuse file descriptors. Ordinary regular files and arbitrary
`io.Writer` callbacks are unavailable, including typed-nil implementations.
Explicit `ContextWriter` embeddings must cancel their actual operation and join
its effects; wrapping a blocking writer in an abandoned goroutine violates this
contract. A known `bytes.Buffer` has a separate 1 MiB capture limit and may be
inspected only after the owner closes. `io.Discard` is admitted intentionally.

Writes have a 250 ms budget. A zero-byte failure drops that record and waits one
second before another queued attempt. A partial failure disables the destination
and discards queued records: another JSON record must never splice into an
ambiguous prefix. A broken context writer that panics also disables its
destination. No log write can authorize a protocol operation or stop an
independent sampling worker.

The enclosing service must immediately defer `Close`, including on startup
errors. The exporter deliberately retains an owned context after core
cancellation so final cleanup diagnostics may be queued. `Close` allows at most
250 ms for a final drain, then cancels the actual write and joins descriptor
cleanup. It never abandons a live writer. Counts include queued records discarded
at shutdown; acknowledgments count only completed destination writes.

These are local delivery observations. They prove neither remote Loki ingestion
nor alert delivery. Unsupported or full log destinations remain visible through
independent textfile metrics; missing/stale-source alerts are still required.
Finite CLI inspect/plan/bootstrap output keeps its existing synchronous contract.
