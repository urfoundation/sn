// Reviewed native artifacts share one exact catalog. Current signing selects
// one pin; archive readers explicitly restrict which predecessors they admit.
package crv4

const (
	ReviewedRuntimeSpecVersion  = uint32(461)
	ReviewedRuntimeCodeHash     = "0x15cf19d2f4f8e2a8a6f46cb735db8f9f03ba3775866188fa93799ad3a040da2e"
	ReviewedRuntimeMetadataHash = "0x98b2cfd0d6633488dfe5b3b70b869d5753aa3c42396533013df131e4e0e5ca68"
)

// This array is private so callers cannot redefine reviewed authority or its
// finite cache bound. Historical entries keep their original exact bytes.
var reviewedRuntimeArtifacts = [...]RuntimeArtifactIdentity{
	{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 451, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0xf3554a22dfcefa9b42b3a0a5e58c1e6c871795ecc9ea9da78bf0900e23e57c08", MetadataHash: "0xeecd7e7c00377caec23c3dc754fd621963cc456fa5d02a4f66ff267b0494bd9d"},
	{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 452, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0x40a8c3c99a47d6739b086236308535fab26d5fd4cc5c88eb83f6a3c8b928f7cc", MetadataHash: "0x2e1d4f992a978fdd58652c8cf434c26bb8f89170e6a0fdbc9362b29e8fe8a835"},
	{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 453, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0xabe169cc148e2a63068772788c191fa6566f02aa2ea9afb80cdeb28217bab4d4", MetadataHash: "0xb00e7e0188d537136a973df4d5c5f2c86ef903ffff49c1cf8d129dabc98b07ce"},
	{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 454, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0x725e3d1eca8d5c29c1f0fa6476d5360661b852f52aebad979d6636e227a431ef", MetadataHash: "0x4d17516b694ef8d18f8a565dcb2df0117e7a0018a3ffa40812c91a1621225702"},
	{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 455, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0xbca85925668cabb2880164610d64eda2e4d9bf2777994f9cdfdb9d36253ce74a", MetadataHash: "0x16da562c347a354c55eb1ad5cd5094343afe7acdc12e5b526bf6c8cb12e866bc"},
	{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 458, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0x2fdb28e5c3fe4e79844b25dee09ed960e90004432ea2bd98079aba4c5530c51a", MetadataHash: "0x040088e73e34ed5561372aa51b07b56e41cf7f390312837b074434f30452593d"},
	{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 459, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0x558275958401c026fa4a4159466d49eabd08c761f0c801390593fcba91dee69b", MetadataHash: "0xcf97fac54fee756137f42e53deeeca828959a74c6d87274898db2c36a33c4fef"},
	{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 460, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0xa2ba599cc0ee97abaa078cf54498ad020957a32cdc2cb7c1e5b9fa14bf5cad3d", MetadataHash: "0x98574118d8447c31b72c57402bdda481203f58273ae175a3b6c1da44400e934c"},
	{Version: RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 461, TransactionVersion: 1, StateVersion: 1}, CodeHash: "0x15cf19d2f4f8e2a8a6f46cb735db8f9f03ba3775866188fa93799ad3a040da2e", MetadataHash: "0x98b2cfd0d6633488dfe5b3b70b869d5753aa3c42396533013df131e4e0e5ca68"},
}

// Returns a copy of the reviewed catalog; presence alone grants no signing or
// historical authority. Each consumer selects its own permitted domain.
func ReviewedRuntimeArtifacts() []RuntimeArtifactIdentity {
	return append([]RuntimeArtifactIdentity(nil), reviewedRuntimeArtifacts[:]...)
}

// Selects by the complete version identity; altered transaction/state versions
// cannot inherit a reviewed spec's code or metadata.
func ReviewedRuntimeArtifact(version RuntimeVersionIdentity) (RuntimeArtifactIdentity, bool) {
	for _, artifact := range reviewedRuntimeArtifacts {
		if artifact.Version == version {
			return artifact, true
		}
	}
	return RuntimeArtifactIdentity{}, false
}
