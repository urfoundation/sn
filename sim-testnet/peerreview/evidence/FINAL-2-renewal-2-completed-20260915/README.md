# Completed renewal 2 receipt

This compact public bundle records the completed second fleet-renewal owner
through its CLI result and its local completed-journal projection. The plan is
`0x09ac683bae8bf99362bfc427776987fce951db58b71b3f01966236abbf7c91f1`,
round 2, with validity epochs 393 through 424 and 202 fleets.

`CLI-STDOUT.json` is the owner's public stdout result. The copied terminal
receipts show body and outer exit 0, with `postcondition_verified`. Its
`result.status` records binary and release-lock comparisons of 0. The state
comparison is 1, as expected for the completed adoption and transactions; it
is retained as an observed receipt rather than recast as an equality claim.

`COMMAND-REQUEST-HASHES.tsv` carries the actual hashes of the private command
and request inputs without copying either input. `JOURNAL-STAGES.tsv` and
`ACTIONS.tsv` are a post-completion projection of the local journal, scoped
only to the plan above. The five recorded stages each contain 1,212 distinct
action IDs: intent, broadcast, included, finalized, and postcondition-verified.
There are zero failed or error actions in that plan scope.

`ACTIONS.tsv` contains only action ID, derived chain, transaction hash, and
inclusion block number and hash. The chain label is derived from the approved
action namespace: `commitment` is native; `mirror` and `bind` are EVM. It has
202 native rows and 1,010 EVM rows, 1,212 total; every row has one transaction
hash and one inclusion block. It omits the private plan, raw journal, action
intent data, signatures, signed bytes, nonces, and secrets.

`DIRECT-ONCHAIN-SAMPLES.json` reuses the existing two direct receipt samples
from `FINAL-2-renewal-2-first-onchain-20260915`, retaining the source summary
hash. Those samples are separate direct native and EVM observations. Their
`independent_rpc` value is false, and this bundle made no fresh RPC request.
They do not independently validate the complete 1,212-action journal summary;
the CLI/journal provenance and direct-sample provenance remain distinct.

Verify this bundle with `sha256sum --check --strict SHA256SUMS` from this
directory.
