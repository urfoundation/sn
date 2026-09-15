#!/usr/bin/env bash
set -euo pipefail
cd /home/by/urnetwork/temp/xops-rpc-vulnerability-assertion-correction-20260914/xops
exec env PYTHONDONTWRITEBYTECODE=1 /home/by/urnetwork/.virtualenv/brien/bin/python3 -m unittest -v main.ansible.tests.test_vulnscan2_resolved
