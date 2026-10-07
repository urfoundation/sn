# EVM public-header recovery fixture, 2026-10-01

The independent broad `./mainnet` baseline on SN `8436f946` found a
deterministic test failure in `TestEvmCreateUnavailableMappingRemainsResumable`.
The test made `debug_getRawHeader` return unsupported, which now correctly
selects the public `eth_getBlockByHash` fallback. The shared execution fixture
had no handler for that method and returned HTTP 400. The failure was fixture
drift, not evidence that production reclassified a contradictory chain read.

Test-only commit `793e01b00ed7238db0a256bff38105475e8b577d` adds a faithful
rendered block to the shared fixture, makes the unavailable-read controls
refuse both raw and public methods, and verifies full eight-action public
fallback, unchanged signed custody, retained scan checkpoints, and distinct
HTTP 400 refusal. Five `_test.go` files changed; production bytes did not.
The source tree was independently reviewed at
`cfb5bf9d053088f0dfd160a11e996b7fe9cf54ac`.

The previously failing test now passes in isolation. Forty-two unique
selected mainnet roots pass normally and under race detection, `go vet
./mainnet` passes, and three bypass controls fail at their intended
assertions. [Independent medium-test receipt](/mnt/data/sn-testnet/sol-mainnet-medium-20261001/receipt.md)
also confirms the isolated fix and focused race result. The unpartitioned
964-root baseline did **not** pass as a package: normal mode recorded the
pre-fix fixture failure and then reached its 20-minute package limit; race
mode reached its 30-minute limit without a reported race or test failure.
Its logs are retained with that receipt. This result qualifies the selected
changed paths; the broad package requires partitioned package-PASS execution
or a measured larger deadline before claiming full closure.
