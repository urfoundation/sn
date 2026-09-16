# Lossless r3 software-only plan revision review

The retained b7fd2eb5 plan and proposed 49ddbc49 plan differ at exactly 11 JSON paths, all listed with complete values in `semantic-differences.json`. No action, allowance, identity, renewal or retained evidence field changed.

Changes: the release-lock hash is 361b0c70… -> 1024ab90…; plan hash b7fd2eb5… -> 49ddbc49…; b7fd2eb5 is appended at prior_plan_hashes[89], retaining every earlier ancestor in order; generated_at advances from 04:09:03Z to 08:00:35Z. Native and EVM finalized heights both advance from 8015788 to 8016946 with their respective domain hashes. The three alpha observation fields (available, transferable, wallet-netuid total) move together from 55561436992549 to 55834140895655 rao.

All 4733 actions are equal in order and every nested value, including exact spending, repair tranches, intents, dependencies and receipt/source authority. Limits remain 205 EVM TAO, 225 total TAO, 37250 alpha, 262 registrations and zero new subnets; active and retired spending are unchanged. The existing repair-limit parameters, including the retained 6000-alpha tranche limit, are recorded explicitly in `unchanged-invariants.json`.

Both fleet renewals (359–390 and 393–424), lifecycle renewal, full continuation and all validator evidence/source/carry data are equal. Continuation remains END 8024100, required work 7570 and 1024 new slots. Chain/genesis/netuid/owner, roles, deployment/contracts, config/policy/resolved hashes, RPC authority, upgrades and historical repair/retired deployments remain equal. The unchanged continuation is retained authorization, not a claim of sufficient future launch runway; the planned fresh continuation remains a separate native operation.

The existing Python JSON renderer was used only after rejecting duplicate keys and all non-integer numeric tokens. Both inputs contain 171382 integer tokens, including 1881 above 2^53. Every original integer lexeme and every non-whitespace token is identical after pretty rendering. `render-receipt.json` binds original/pretty SHA-256 values and checks. `plan.pretty.diff` is the complete 60-line / 2930-byte diff; the original 22 MB single-line diff was not printed or modified.

Original files, live state, source and chain were not changed. No test or project build ran. This comparison identifies no semantic obstacle to software-only setup adoption; it does not bypass the command's native validation.
