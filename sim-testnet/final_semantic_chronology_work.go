package main

// Keeps historical decoder work observable at its actual execution boundary.
// Production callers use the zero value; no observer receives mutable inputs,
// chooses a verdict or retains authority outside one synchronous invocation.

// Carries immutable, call-local observation only. The verifier invokes it
// synchronously; observers must not mutate the verifier's owned input graph.
type finalHistoricalCoordinatorArtifactWork struct {
	decoded       func(kind string, inputBytes int)
	namespaceWork finalSemanticNamespaceWork
}

// Reports only the decoder identity and byte length, never the source bytes.
func (self finalHistoricalCoordinatorArtifactWork) observe(kind string, inputBytes int) {
	if self.decoded != nil {
		self.decoded(kind, inputBytes)
	}
}

// Counts execution of the full source-namespace decoder and content checks.
func (self finalHistoricalCoordinatorArtifactWork) lineage(evidence *FinalSemanticEvidence, data []byte) (map[string][]byte, error) {
	work := self.namespaceWork
	if self.decoded != nil {
		prior := work.decodedBytes
		work.decodedBytes = func(inputBytes int) {
			self.observe("lineage", inputBytes)
			if prior != nil {
				prior(inputBytes)
			}
		}
	}
	return finalFleetGenerationArtifactFilesWithWork(evidence, data, work, "historical-replay")
}

// Counts execution of complete persisted-plan decoding and hash validation.
func (self finalHistoricalCoordinatorArtifactWork) plan(data []byte) (*SetupPlan, error) {
	self.observe("plan", len(data))
	return decodePersistedPlanBytes(data)
}

// Counts execution of the full ordered journal hash-chain decoder.
func (self finalHistoricalCoordinatorArtifactWork) journal(data []byte) ([]JournalEntry, error) {
	self.observe("journal", len(data))
	return decodeFinalSemanticJournalBytes(data)
}

// Counts complete transaction, raw-log, ordering and receipt-hash validation.
func (self finalHistoricalCoordinatorArtifactWork) receipt(row *FinalHistoricalCoordinatorReceiptEvidence, data []byte) (FinalCollectedEVMTransaction, []finalCanonicalEVMLog, error) {
	self.observe("receipt", len(data))
	return finalHistoricalCoordinatorReceiptArtifactTransaction(row, data)
}
