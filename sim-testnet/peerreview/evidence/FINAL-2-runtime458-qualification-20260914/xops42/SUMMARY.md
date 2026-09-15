# Xops RPC vulnerability assertion correction — closed qualification

Candidate `42bfe0be2a7a7c51bbda87fb44886424604f509e` changes only `main/ansible/tests/test_vulnscan2_resolved.py`. The corrected shared assertion rejects request- and connection-quota directives in the unpaced gateway render while retaining the existing gateway restrictions.

The source-derived and handoff module censuses both contain 17 methods. The complete module passed 17/17 at 23:29:12–23:29:19 UTC. Fresh p2 and p3 confirmations each passed the seven affected and formerly fixture-blocked methods at 23:29:35–23:29:40 and 23:29:47–23:29:52 UTC. Candidate source and all fixture inputs were clean before and after every qualifying body.

The causal applies only the old four quota-required assertions in a disposable worktree. It produced the required 1 FAIL / 1 PASS: the old gateway-render test failed on `limit_req zone=subtensor_rpc_rate burst=200 nodelay;`, while the synthetic quota-rejection control passed. The candidate checkout was never mutated.

Three earlier captures are retained and are not qualification evidence: one pre-body lexical-vs-definition-order admission refusal, then 12 PASS / 5 ERROR with absent Warp/Vault aliases, then 16 PASS / 1 ERROR with absent Config. All three aliases were bound from the already authorized physical dependency projection before the authoritative r4 body.

No deployment, RPC, node operation, or product-source change occurred in this qualification.
