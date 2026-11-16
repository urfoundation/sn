The successor changes three test files only. Production and module bytes remain
exactly b1c5fb232508c80128902abe985dee7790a10077. Original failed and successful
results remain separate; this document is not a behavioral pass receipt.

The signed-sidecar control had reused a measurement-only configuration whose
operators supplied only their IDs. It now declares endpoints, distinct private
state/credential paths, concurrency and complete evidence bounds before the
original approval. The actual public loader must accept that original config
before the test creates a sidecar or proposes a capacity revision. Provider
transcripts and already signed sidecar bytes are not patched afterward.

The namespace refusal control now compares against the fixture state after its
deliberate checkpoint-xattr removal. Active-writer refusal checks the typed
BusyError and public exit 4, not an invented diagnostic substring. Both still
require no draft, unchanged retained state, and released snapshot leases.

The shared identity fixture explicitly protects only its own numbered TempDir
and child with mode 0700. It does not rely on the runner umask or modify external
ancestry. The selected successor scope is the four affected capacity roots plus
the original signed-config and private-identity neighbors. Independent execution
must retain b1's original results and qualify this exact source separately.
