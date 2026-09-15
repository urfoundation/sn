# Xops vulnerability assertion correction

Source: `xops/`, clean commit 42bfe0b, parent qualified 446cbdb.
Only `main/ansible/tests/test_vulnscan2_resolved.py` changed. No production
template, variable, playbook, node image, RPC capacity or release hash input
changed. Astra ran no tests, builds or formatters. Root/TF own qualification.

The retained aggregate failure was deterministic: the old VS2-011 test demanded
four request/connection quota directives that the approved unpaced template
deliberately removed. The corrected assertion refuses all req/conn directive
families on the actual render. Every existing bind, source allowlist, deny,
loopback backend, safe-method, image, native 512-connection capacity, body and
idle assertion remains. A synthetic method calls that same assertion for eight
quota families and preserves a clean method-control/comment-text case.

Run the existing pinned Python environment from the xops root, preserving its
existing dependency/fixture paths and source fences:

`python -m unittest -v main.ansible.tests.test_vulnscan2_resolved`

Exact module census: 17 methods in `module-methods.txt` (16 original plus one
new). The old aggregate's 30 tests were its composed xops phase, not this
single module. Its full module p1
must pass. Then run both methods below in two fresh sequential processes p2
and p3 on unchanged source/environment:

- `main.ansible.tests.test_vulnscan2_resolved.Vulnscan2ResolvedInfrastructureTests.test_vs2_011_subtensor_local_rpc_and_restricted_gateway_render`
- `main.ansible.tests.test_vulnscan2_resolved.Vulnscan2ResolvedInfrastructureTests.test_vs2_011_unpaced_gateway_rejects_request_and_connection_quotas`

The existing qualified 30-root Subtensor playbook scope at 446cbdb is unchanged
and need not be repeated. The separate old aggregate remains failed evidence.

Optional bounded causal confirmation uses the forward patch
`CAUSAL-current-to-stale-quota-assertions.patch` in a disposable checkout of
42bfe0b only. It restores exactly the original four assertions, retaining the
new helper and control. Require apply --check before mutation, exact one-file
diff and reverse-apply check afterward. The two methods above must yield one
expected FAIL (old render assertion containing `limit_req zone=subtensor_rpc_rate
burst=200 nodelay;`) and one PASS (synthetic quota-refusal control). No actual
gateway render, deployment or RPC is modified. Record exit1 as expected causal
evidence, with separate owner/test exits and preserved original full-gate log.

Adjacent review: all req/conn quota-name consumers under main/ansible/tests and
the Subtensor templates/variables were searched. The only stale positive
quota assertions were these four. test_subtensor_playbook already has the
qualified synthetic unpaced render and both-route custody controls; its legacy
knobs intentionally make old-template mutation causal, not current settings.
VULNSCAN2.md is the retained August 6/7 historical audit and describes that
historical quota policy; it is not a current deployment claim and was not
rewritten. Other service rate limits and firewall controls are outside this
authorized owned-RPC change and remain untouched.
