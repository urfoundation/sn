# Provider monitoring and current dependency intake — October 2

Frozen provider source `98fcff06b2abfd697a72c120c3cfc037f86b2dc1`, tree
`3782114824002c8050cc94a610f44c1eb524cb97`, passes 42 selected normal and
42 race roots plus three-package vet. The [author receipt](provider-monitor-author-20261002.json)
has SHA-256 `fa3a978f0d2e30fce8c6c1382e53ef32ff62c923bec40aad22dd3ea601dfc916`;
root rehashed all 113 bindings. Two old public-route controls fail as intended
in each mode. Intermediate capacity results are retained, with a timeout-only
negative explicitly excluded from causal proof. A separate test-only successor
will use the actual worker-exit barrier.

The actual provider endpoint reports owned generation and SDK readiness facts.
The independent monitor uses its own expected roster and stable configuration
identity, retains restart/sequence/freshness checks and per-role incidents, and
does not derive eligibility from connection or packet counts. Proof and
settlement remain explicitly unknown until their actual producers are wired.
This is author qualification; independent review, main integration and full
MG-07 remain open. No release or deployment is qualified.

Server successor `22e3c1ba438b7888dccfc04244bcafaae0bcad3d` preserves the
qualified local-blob bytes and current upstream module floors, changing only
Connect to published `v0.0.0-20261002163946-e0d75562aa23` and its checksums.
The [source/module join](current-server-module-join-20261002.json) has SHA-256
`8912e40cf1540b4b0cf54d6eaf6ea53e0e19c4703653a58b500c907dd15acbc2`;
root verified all 29 bindings. Its 18 durablevolume files match qualified
Connect `71df099c`. This join proves selected source/dependency bytes, not
the complete consumer graph or passing server tests. The actual independent
server graph stages local SN `69f4bbdd` and all other local siblings explicitly,
uses GOWORK=off and selects published Connect e0d/SCTP644. Product qualification
is still pending. The original server `cebf154f` missing-package failure remains
retained and is not converted into a passing result.
