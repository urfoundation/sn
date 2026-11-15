# Owner-side Ledger signing qualification

The subnet owner signing candidate was independently tested at frozen commit
`b63329b0516f34a5cb227b96ac55419078bb8e60` (tree
`4c68a8d31b084c4502652b618c1176783d716e40`). The integrated merge was
independently tested at `f1390e9a17fea7bf48b49a63761a8bd0911c758b`
(tree `21a644f7bbea2fea9e427106493b4df1717a066a`). All owner candidate
paths in shared commits `196d83d5` and `633ccb13` are byte-identical to the
tested integration. The three resolved merge files retain the shared
root-service dispatcher and native census/approval test hooks.

For the frozen candidate, Sol medium verified all **20 focused roots** normally
and with race detection under both umask 0002 and 0077, all **146 adjacent
roots** normally, and **28 selected adjacent roots** under race detection.
All seven causal controls failed at their intended assertions. A 12-root race
shard timed out; its partial log is retained and excluded from the passed count.
The synthetic Python SDK boundary passed.

For the integrated merge, all **43 selected roots** passed normally and under
race detection with exact RUN/PASS counts and package exits. Five integration
controls proved owner override, native census, native approval, owner command
dispatch and root command dispatch remain effective. An initial control aimed
at a test without an approval override did not trigger; the reviewer retained
that attempt, then selected the applicable signed-age test and obtained its
intended failure against a passing baseline.

The independent [candidate receipt](/home/by/urnetwork/owner-signing-qualification-sol/b633-receipt.json)
and [integration receipt](/home/by/urnetwork/owner-signing-qualification-sol/f139-receipt.json)
bind exact source, dependencies, selectors, logs, exits and controls. Their
evidence manifests have SHA-256
`5c9e33907a7cb1bccb62390e73b17948f9328b1a35c12890f6e7696486c605df`
and `30a98bcb6cc07fc9696f7d3af02d6a667ace51607b592500df9073351d7b841d`.
The author integration handoff manifest SHA-256 is
`f1c7fb79155e48e32397d6be50f613861925e84268a1d9113daa085214f9b540`.

This qualifies an offline software handoff only. The exact native SDK build
artifact, physical Ledger/app/firmware, current on-chain owner identity and
signature scheme, deployed Wasm's RFC78 metadata digest, global owner nonce
fence, enforced trim authority and live submission remain unqualified. No
hardware signing, live RPC, transaction or host deployment was used.
