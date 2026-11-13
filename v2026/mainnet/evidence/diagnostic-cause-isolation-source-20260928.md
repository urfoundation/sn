# Diagnostic cause isolation: source candidate

A finite error-tree node count did not make optional cause classification
nonblocking. A custom `Unwrap`, `Is`, `As`, `Error`, `String` or network-error
method can execute arbitrary code before a diagnostic record reaches its
bounded queue. Miner callback output and validator trail/read diagnostic helpers
contained this adjacent gap.

The successor reads direct sentinel identities and an explicit set of standard
library wrapper fields, bounded to 32 nodes. It invokes no error methods. Opaque
joins, custom wrappers, typed nils and cycles report `unknown`. Known concrete
trail kinds and receipt-unavailable/transport tags remain useful scalar facts.
Required miner token/rejection-file work and original error retention still
precede cancellation; cancellation now precedes every optional failure offer.

Core retry, custody and protocol predicates are unchanged. They retain their
own error contracts; this change does not claim to eliminate their traversal or
make arbitrary application implementations cancellation-safe. Root output and
the shared exporter already avoid producer-error traversal and are unchanged;
the diagnostics package only gains the common closed cause reader.

The existing miner diagnostic schema, optional HTTP status extension and
validator five-domain progress/event wire are unchanged. Consumers must continue
to accept `unknown`: more opaque errors may now use that existing category.
This includes the existing trail step's opaque fmt wrapper: its concrete seed
kind remains known while its nested timeout cause becomes unknown. Delivery
remains a prior sink acknowledgment, not service readiness, successful
authentication, native progress or an alert-delivery claim. SDK/internal logging
and deployment remain outside this slice.

Deterministic regressions place explicit barriers inside foreign methods and
exercise the actual miner and validator serializers. Real token rename and
rejection-marker failures prove completed file effects and original errors are
visible at the cancellation barrier before an optional event is offered.
Concrete standard wrappers, typed nils, non-comparable values, cycles and exact
depth limits preserve the finite no-callback contract.

The source composes root `a089a28ca0f3c72e91821448f02fee90ba03a902` with unchanged
miner dependencies `1b671769ddf801117ef0e197ff97e4bb2cd59b8d` and
`dc84ec47dfb2393f03d1941422cee65a0cb9ce44` as separate commits. Qualified trail
fixture and root-output fixes remain present; registration `d3` is not part of
this graph. Qualification was pending at this source seal; the subsequent
[normal/race/control qualification](diagnostic-cause-isolation-qualification-20260928.md)
passed and the source is integrated.
