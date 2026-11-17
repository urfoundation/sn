# Offline monitor deployment integration, October 2

MG-07 now has an offline deployment path at xops
`b98f876999c2ad3f057ca8d32d97a23cd7a535ba` (tree
`0b77986eb2ee92b5aacb7a8991387667db411857`), based on freshly fetched
`da09ab5ab313acd1ea37eba259011e19e9035c1a`. It is integrated and pushed as xops
main `5d48b0502b6d80f5c2d2f376793ba89188c49565`, with the identical tree; this
integration adds no deployment or approval. The
[deployment runbook](../../../xops/main/ansible/SN-MAINNET-MONITOR.md) describes
its exact input, staging, activation and unsent delivery-drill interfaces.
SN application source remains `6cfc4773038fae8b1de07807eeeb4a97c32d07d3`
(tree `89ba29e90b731e5e344fbef539d3be9497fa0ba4`); these documentation changes
add no binary behavior or release qualification. The scoped SN tests consume
server `0aa1e2449b92fe62b10c6a287e5492dd5a19b792` and the other exact retained
sibling pins in the author receipt.

The [earlier absence audit](monitor-deployment-gap-20261002.md) remains the
baseline. Its shared Fluent Bit configuration has no textfile collector; the
new implementation leaves that fleet collector census unchanged. A dedicated
monitor instance owns private durable checkpoints and atomically published
metrics. A separate Fluent Bit instance collects only its textfile directory
and supplies fixed env/host/job labels. A physically distinct observer owns
constant expected-host/role recording rules, a loopback-only Prometheus
evaluator, and an explicitly configured HTTPS Alertmanager route.

Controller admission re-renders the exact bundle and refuses overlapping,
missing or mismatched inventory groups, addresses, machine IDs, testnet host
configuration, changed binary/manifest/source membership, service policy or
new-production scheduler profile. The new policy must explicitly select
`urnetwork-subtensor-tempo-drift-v1`; historical absent-profile policies are
unchanged. GET retry is fixed at 60 seconds. Both hosts finish staging before
activation, and the observer activates first. Staging installs no unit and
cannot notify a start handler. A separate affirmative approval must bind the
exact rendered bundle, hosts, genesis/EVM 964, release manifest and service
policy before any unit is installed, enabled or started.

Every restart rechecks physical custody, hashes, machine identity and approval.
Activation verification runs as each actual service identity, including its
root-owned, group-readable 0640 credential files. Missing or replaced staged
assets fail integrity checks; checkpoints, locks and unresolved incidents are
never reset by deployment. The code provisions no signer or repair capability.
Snow's checked testfinney/runtime-471 configuration remains untouched and is
explicitly refused as a mainnet target.

A real loopback Prometheus fixture caught an adjacent integration requirement:
`remote_read.required_matchers` only permits matching selectors; it does not
inject labels. Rendered SN queries therefore select the exact env/host/job,
while expected-roster and observer-health queries remain local. The test
proves that an unscoped query makes no backend request, the scoped query makes
one, and the self-health query makes no additional remote request. Separately,
removing the independent roster causes the expected missing-host alert fixture
to fail with no actual alert; retaining it produces the alert without any
monitor series. A healthy matching series suppresses the missing-host alert.

| Author qualification | Exact scoped result |
| --- | --- |
| New deployment tests | 27 admission/custody/unit tests and 5 real Prometheus/config/rule tests pass, zero skips. |
| Adjacent xops coverage | 19 existing telemetry-cardinality, service-metrics and shipper tests pass. |
| SN monitor application | 13 selected command/ownership/restart/profile roots pass normal and race; `go vet ./mainnet` passes. |
| Existing SN alert fixtures | Chain, validator service, native deadline and operator suites pass with pinned promtool 3.5.0. |
| Deployment grammar | Ansible syntax and all three rendered systemd unit checks pass. Controller-only admission changes zero hosts; unapproved activation fails at the controller. |
| Credential permissions | A real unprivileged UID/GID reads a synthetic root-owned 0640 credential but cannot overwrite it. Root-only read and world-readable credential controls are refused; no service account is created. |

The final observer-first staging order was followed by its targeted unit test
and Ansible syntax check. Earlier fixture assertion/port/whitespace and command
working-directory failures remain in the evidence; the corrected affected
checks pass. They are not reclassified as production failures. Baseline source
absence and executable causal omission controls are recorded separately.

Author evidence is retained under
`/mnt/data/sn-testnet/mainnet-monitor-deployment-20261002/`:

- `evidence/receipt.json`: SHA-256
  `6effb657654e46d7dd160f3be4715826259c103b5135ac2eaaddd77015a63e0d`.
- The 58-entry `evidence/manifest.json`: SHA-256
  `d6b28dced03efc7f913eff3a48b679f8bf266a882afc6997659f6d2aa8cee69c`.
- `monitor-deployment-author-evidence.tar.gz`: SHA-256
  `6cb681a9126ab943aca6423db481dde07b84ea50a9c280f1694469a0baa3b461`.
- Incremental `xops-monitor-deployment.bundle`: SHA-256
  `679eb5edf350fd7f0496a5943bd42bb1b3a79084b296be0130535293486cd06f`.

The Prometheus 3.5.0 Linux/amd64 archive is pinned to its published SHA-256
`e811827af26d822afb09a4f28314f61b618b12cff5369835a67f674d8b46f39a`;
exact executable hashes and controller versions are retained in `tools.json`.

Independent Sol qualification is **PASS_SCOPED** on clean physical checkouts of
the same xops `b98f8769` / SN `6cfc4773` pins. Its separate
[receipt](monitor-deployment-independent-20261002.json) has SHA-256
`27b85627b3870cdb60236c0565ce5383cc455c7db4c02c406f25b8d4a0d40843`,
retained originally at
`/mnt/data/sn-testnet/sol-mainnet-monitor-independent-20261002/receipt.json`.
It independently passes 27 new xops tests, five real loopback Prometheus tests,
19 adjacent telemetry tests, all 13 SN monitor roots in normal and race, and
scoped vet. Ansible syntax, controller-only preflight, unapproved-activation
refusal, cross-user credential controls and all three systemd unit checks also
pass. All 58 author raw evidence entries, the author receipt, archive and source
bundle were separately rehashed. The first adjacent run lacked sibling pins;
the first credential fixture correctly refused its group-writable scratch
parent. Both setup failures remain retained with corrected passes on the exact
frozen source; neither required a product edit. This scope uses synthetic
credentials and loopback services, and supplies no host or delivery approval.

No live inventory target, host/chain/config/release approval, deployment,
systemd start, chain action or notification send occurred. The selected actual
Fluent Bit binary has not been executed in this scope. Its host acceptance,
real producer census/permissions, remote-write ingestion and remote-read route,
alert evaluation, delivered firing/resolution receipts, external observer
watchdog and primary/backup on-call rehearsal remain launch gates. The delivery
hook writes a request with `sent=false`; it cannot establish delivered paging.
Root-specific monitoring, unobserved domains and the repair controller remain
separate open scope.
