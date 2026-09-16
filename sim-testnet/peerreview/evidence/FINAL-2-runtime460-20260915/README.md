# Runtime460 interrupted runtime459 admission

The runtime459 plan revision stopped on2026-09-15 at22:38:46UTC. Its stdout
was empty and it emitted no new plan. The following diagnostic doctor reproduced
four hard failures at one finalized block because the chain carried460 while
the released executable required459. It did not identify a transaction ABI
incompatibility. All other hard doctor checks passed.

Both operations exited1. Their recorded state, binary and release-lock
comparisons are0 (unchanged). These are operational refusals, not passing
launches or soak evidence. Runtime459's completed test receipts retain their
original scope; the observed new runtime requires review of changed inputs.
Finalized renewal/funding history is preserved in its existing evidence bundles.

The doctor JSON here is an explicit extraction of every hard check from the
original stdout, including passing hard checks. Its original complete stdout
hash and local capture locator are recorded separately; the extraction is not
represented as the original raw stdout. Requests, stderr, exit/status and
before/after hashes are byte copies. All chain reads used the owned LAN
192.168.1.162:9944; independent_rpc=false.

Original captures:
/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/native-recovery-batch-20260915-r1/plan-revision
/home/by/urnetwork/temp/sn-infrastructure-release-integration-20260914/native-recovery-batch-20260915-r1/doctor-after-plan-refusal
