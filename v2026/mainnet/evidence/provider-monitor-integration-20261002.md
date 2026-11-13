# Provider readiness integration — October 2

Main merge `83d92f75` integrates the exact 14 provider-monitor files from
SN `e4fca9e0f95323d716e4ca711e8f37b12b8bfb54`, preserving all 12 newer
fleet changes and unchanged module files. Root verified the changed files
against the [integration manifest](provider-monitor-main-integration-20261002.json).
The test-only e4 correction changes only the monitor test file relative to
production candidate `98fcff06`.

The [independent receipt](provider-monitor-independent-20261002.json), SHA-256
`bc7275dfdaff791394e94d447860929715e1ec4f252834b5455b136187792c48`,
passes 20 unique tests in each of normal/race modes and vet in miner, mainnet
and protocol. Root verified all 52 manifest entries. The original 19-test
runner remained intact; the admitted-handler shutdown test was appended in a
separate normal/race phase on identical source. Two old c525 capacity controls
fail at their intended assertions in each mode. Author 42-test qualification
remains a separate scope.

The tests cover actual public readiness observation, retirement during a read,
owned shutdown, expected-roster admission, bounded maximum-roster export,
lost-ack recovery, outages, stale sequences and protocol ambiguity. A running
process or ready device is not authenticated proof or settlement evidence.

The qualified graph consumes Connect0a5/Server10a with GOWORK off; its 20,028
tracked source blobs passed terminal byte readback. Current-main fleet changes
retain their own qualification. A final joined release, current server intake,
production configuration, deployment and alert/recovery rehearsal remain open.
No live transaction or activation follows from this source merge.
