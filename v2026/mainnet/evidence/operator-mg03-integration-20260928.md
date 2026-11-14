# MG03 operator source integration

The qualified operator census and conditional receipt/fee join are preserved
without source edits in `codex/mg03-integrated-hardening-20260928` at server
commit `26008e5859991b0649896466b094aff50b4a3440`. Its exact lineage is
`5dc11761` → `71efeb1f` → `26008e58`; the branch has tree
`20791e914b91fa283bf88c83f850aef3a89d58f2`. The branch was pushed and
verified against origin. The [census](operator-signature-census-qualification-20260928.md)
and [receipt/fee](operator-receipt-fee-qualification-20260928.md) source
qualifications apply to those exact unchanged commits.

This is a branch integration, not a merge into the current server root or a
production release. The server root was clean at `0633780c`. It diverges from
the qualified lineage: a merge preview has six conflicts, while narrowly
cherry-picking only the two MG03 commits would omit earlier controller
receipt-handling fixes `e2a3be6f`, `1939c779` and `7bf88d79`. Keep the
qualified lineage together for release composition; any source rewrite or
different physical dependency graph needs fresh qualification.

The isolated read-only integration report is
`/mnt/data/sn-testnet/qualification/mg03-server-integration-20260928/RESULT.md`
(SHA-256 `3b13996c6ec14cefeb824fa0c2aa01a156efd5d54fb1d37c9c28f2a665856542`).
Its `DIVERGENCE.md` and `divergence-evidence.json` enumerate the branches and
conflict paths. No live database, signer, RPC or chain action occurred.
