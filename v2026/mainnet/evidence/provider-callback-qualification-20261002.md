# Miner callback and fixture integration — October 2

Miner source `3664820382ba38c8672fea09e58381eb5e541ed1` retains independent
members after classified temporary startup failure. An actual SDK JWT refresh
that cannot persist its token quarantines its generation and joins it outside
the callback; healthy members continue. Authentication and unknown joined
causes retain terminal handling. Startup is not blindly retried because it may
have issued a signed wallet request.

The [author receipt](provider-callback-author-20261002.json) records 40 normal
and 40 race roots plus vet. Its SHA-256 is
`0ce7279352fc1e196d0b92ec3a4bc2e0971e21b673cd4a6f3d5dfc4d8ef40848`.
The [independent receipt](provider-callback-independent-20261002.json) records
12 selected roots in both modes plus vet, not an independent repeat of all 40.
Its SHA-256 is
`0e861e58bcff9c5c4d7a78a1bb74c04fa345528888275d4dc3ac02a1c8215fa7`.
Old-body controls retain one intended healthy-member cancellation failure and
two passing cause/authentication controls in each mode. Root rehashed all 51
author bindings and all 20 independent manifest entries before integration.

Test-only source `28c970db29ede27b1b1177f5f04b49c62ff4ed1d` explicitly creates
private durable fixture roots. Go's temporary child directory creation can
produce mode 0775 under umask 002 even when its parent is private. This change
does not weaken production admission. The [fixture receipt](private-root-fixtures-author-20261002.json)
records seven normal/race roots under each of umask 002 and 077; the old
permission-sensitive controls fail as intended. SHA-256:
`649e7418398f9aa86a08da66cc3a99b54d5b0442bd3597d1dfa3d282b04a8e5f`.
Root verified all 58 file bindings.

Both merges preserve main's existing documentation and other source changes.
No broad composed suite, current dependency graph, production image, deployment,
restore or live activation is qualified by these scoped receipts.
