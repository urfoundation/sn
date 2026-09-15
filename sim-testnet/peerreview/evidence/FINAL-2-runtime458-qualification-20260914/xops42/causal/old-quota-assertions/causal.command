#!/usr/bin/env bash
set -euo pipefail
cd /home/by/urnetwork/temp/xops-rpc-vulnerability-assertion-correction-20260914/terra-runtime/vulnerability-assertion-qualification-20260914T232905Z-r4/causal/old-quota-assertions/worktree
exec env PYTHONDONTWRITEBYTECODE=1 /home/by/urnetwork/.virtualenv/brien/bin/python3 -m unittest -v main.ansible.tests.test_vulnscan2_resolved.Vulnscan2ResolvedInfrastructureTests.test_vs2_011_subtensor_local_rpc_and_restricted_gateway_render main.ansible.tests.test_vulnscan2_resolved.Vulnscan2ResolvedInfrastructureTests.test_vs2_011_unpaced_gateway_rejects_request_and_connection_quotas
