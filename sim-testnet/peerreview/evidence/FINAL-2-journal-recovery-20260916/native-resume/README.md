# Retained startup refusal — 2026-09-16

The runtime-460 strict resume ran from **01:15:08 to 02:13:45 UTC** and
naturally exited **1**. Its full-work deadline expired during preparation:
the final error reports finalized block **8,015,211**, **7,570** required
remaining blocks and fixed end **8,022,775**. The latest admissible entry block
was **8,015,205**. No termination signal was sent.

The same invocation completed its **4,672 carried-action checks**, verified
`config.render` at **02:08:38 UTC**, completed the two database migrations,
and started five temporary RPC/API processes. Those processes were cleaned
up before exit. The persistent supervisor and release campaign did not start.
The completed render is retained as journal sequence **22,426**.

These are local execution receipts, not a claim of newly verified on-chain
acceptance. The journal gained only the render intent and verified rows;
the watched plan, supervisor files, redacted configuration and public
identities remained byte-identical. Binary and release-lock comparisons
also passed. The earlier finalized renewal, repair and funding remain valid
historical work.

- [Raw stderr and final refusal](stderr)
- [Body/wrapper comparison results](result.status) and [joined exit](join.exit)
- [Retained render journal rows](config-render-journal.jsonl)
- [State before](state.before.sha256) and [state after](state.after.sha256)
- [Execution review](REVIEW.json)

The source was `9a1af956ced89307f978f650609ea5838b910c9b`, with executable
SHA-256 `068a2a4d44a78afc0799b94da6b95afa00414a52327e61f96219ecc5a8d26b0a`.
All RPC used the owned LAN endpoint `192.168.1.162:9944`;
`independent_rpc=false`. This failed invocation is not counted as a passing
startup or final testnet acceptance.
