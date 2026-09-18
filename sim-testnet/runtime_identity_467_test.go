// Runtime467 is the current reviewed testnet identity. Runtime461 remains
// historical evidence and never supplies current execution authority.
package main

func runtime467ReviewedTestLock() *ReleaseLock {
	return &ReleaseLock{SchemaVersion: 1, Release: "1.0", Runtime: ReleaseRuntimeLock{
		SourceRepository: "https://github.com/RaoFoundation/subtensor",
		SourceRefKind:    "commit", SourceRefName: "c6bcb4a7400764c94c1d1b1938514c6c2dd3d33b",
		SourceCommit:         "c6bcb4a7400764c94c1d1b1938514c6c2dd3d33b",
		CodeHash:             "0x2f175dcc64196ec8a6b9235f8d7cfd84efef6c68bb925c4455949591cef9f6d2",
		MetadataHash:         "0xb0fae6d022b74faf948e3b98463b98b46c4738e87348e24340f91146ededa4bf",
		CompressedWasmSHA256: "0x5a4218a3198cf276bf531643ca6813438781b72dc9fac57fa83a3ffe49f9a81a",
		SpecVersion:          467, TransactionVersion: 1, StateVersion: 1,
	}}
}
