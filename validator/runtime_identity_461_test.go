// Runtime461 keeps a new current artifact separate from every retained owner.
package validator

import "github.com/urfoundation/sn/crv4"

// Independent literals bind the reviewed source artifact rather than mirroring
// the moving current-selection constants in the production catalog.
func runtime461ValidatorTestConfig() ReleaseConfig {
	return ReleaseConfig{RuntimeSpec: 461, TransactionVersion: 1, StateVersion: 1,
		RuntimeCodeHash: "0x15cf19d2f4f8e2a8a6f46cb735db8f9f03ba3775866188fa93799ad3a040da2e",
		RuntimeMetadataHash: "0x98b2cfd0d6633488dfe5b3b70b869d5753aa3c42396533013df131e4e0e5ca68"}
}

// The former current owner's tuple stays immutable when461 signs new work.
func releaseHistorical460TestArtifact() crv4.RuntimeArtifactIdentity {
	cfg := runtime460ValidatorTestConfig()
	return crv4.RuntimeArtifactIdentity{Version: crv4.RuntimeVersionIdentity{SpecName: "node-subtensor", SpecVersion: 460, TransactionVersion: 1, StateVersion: 1}, CodeHash: cfg.RuntimeCodeHash, MetadataHash: cfg.RuntimeMetadataHash}
}
