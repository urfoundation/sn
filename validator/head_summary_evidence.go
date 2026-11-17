package validator

// Head publication retains only the public cut locator and an eligible binding
// projection; the asynchronous uploader scopes that projection per recipient.

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/urfoundation/sn/protocol/headsummary"
	"github.com/urnetwork/connect"
)

// Detaches only already public cut references and the minimal eligible binding
// census from a completely verified, durably begun measurement. Eligibility is
// the same key, generation and native uid check used for the actual head fold;
// the operator must not reconstruct this historical census from current state.
func headSummaryEvidence(artifact *ReleaseMeasurementArtifact) ([]headsummary.EvidenceCut, error) {
	if artifact == nil || artifact.Schema != ReleaseMeasurementSchemaV2 || len(artifact.Inputs) == 0 || len(artifact.Inputs) > headsummary.MaxEvidenceCuts || len(artifact.Bindings) > headsummary.MaxEvidenceBindings {
		return nil, errors.New("head summary measurement evidence is incomplete or exceeds its wire bound")
	}
	providers := make(map[uint64]map[connect.Id]bool, len(artifact.Inputs))
	providerCount := 0
	for index, input := range artifact.Inputs {
		if input.NoID == 0 || index > 0 && input.NoID <= artifact.Inputs[index-1].NoID || input.AttemptCutV2 == nil || input.AttemptCutV2.Context.Identity.NoID != input.NoID {
			return nil, errors.New("head summary measurement cut identity is incomplete or unordered")
		}
		providerCount += len(input.Stats.Providers)
		if providerCount > headsummary.MaxEvidenceBindings {
			return nil, errors.New("head summary measurement provider census exceeds its wire bound")
		}
		providers[input.NoID] = make(map[connect.Id]bool, len(input.Stats.Providers))
		for _, provider := range input.Stats.Providers {
			clientId, err := connect.ParseId(provider.ClientID)
			if err != nil || clientId.String() != provider.ClientID || providers[input.NoID][clientId] {
				return nil, errors.New("head summary measurement provider identity is invalid or repeated")
			}
			providers[input.NoID][clientId] = true
		}
	}
	_, _, _, _, current, err := releaseMeasurementBindingObservations(artifact, providers)
	if err != nil {
		return nil, err
	}
	cuts := make([]headsummary.EvidenceCut, len(artifact.Inputs))
	byOperator := make(map[uint64]int, len(cuts))
	for index, input := range artifact.Inputs {
		// CanonicalJSON renders this exact struct and newline after validation. The
		// accepted measurement already validated and replayed it; hashing it
		// again performs no storage, network, private-key or steering operation.
		encoded, err := json.Marshal(input.AttemptCutV2)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, '\n')
		cuts[index] = headsummary.EvidenceCut{
			NoId: input.NoID, NativeBlock: input.CutNativeBlock, NativeBlockHash: input.CutNativeBlockHash,
			ContentHash: headsummary.Hex32(sha256.Sum256(encoded)), Bindings: []headsummary.EvidenceBinding{},
		}
		byOperator[input.NoID] = index
	}
	for _, binding := range artifact.Bindings {
		key, eligible := current[fmt.Sprintf("%020d:%s", binding.NoID, binding.ClientID)]
		if !eligible {
			continue
		}
		index := byOperator[binding.NoID]
		cuts[index].Bindings = append(cuts[index].Bindings, headsummary.EvidenceBinding{
			ClientId: binding.ClientID, FleetId: headsummary.Hex32(key.FleetID), Hotkey: headsummary.Hex32(key.Hotkey), Generation: key.Generation, Uid: key.UID,
		})
	}
	return cuts, nil
}

// One operator's census and bound-client coverage at an intent
// (VERIFY-COHORT-V2.md §2.1, §5.3).
type headSummaryCensus struct {
	// providers in the operator's raw statistics: its settlement window,
	// every carried quality and this tempo's egress, the set max_providers bounds
	providers uint64
	// the operator cut's eligible bindings whose client has an egress hash in
	// those statistics, i.e. served a verified trail hop this tempo
	observed uint64
}

// Counts each operator's census from the same verified measurement the cuts
// come from. The operator bounds its verify cohort by this number, so it is
// the exact raw census, never an estimate.
func headSummaryCensusByOperator(artifact *ReleaseMeasurementArtifact, cuts []headsummary.EvidenceCut) map[uint64]headSummaryCensus {
	census := make(map[uint64]headSummaryCensus, len(artifact.Inputs))
	for _, input := range artifact.Inputs {
		served := make(map[string]bool, len(input.Stats.Providers))
		for _, provider := range input.Stats.Providers {
			if 0 < len(provider.EgressIPHashHexes) {
				served[provider.ClientID] = true
			}
		}
		value := headSummaryCensus{providers: uint64(len(input.Stats.Providers))}
		for _, cut := range cuts {
			if cut.NoId != input.NoID {
				continue
			}
			for _, binding := range cut.Bindings {
				if served[binding.ClientId] {
					value.observed++
				}
			}
		}
		census[input.NoID] = value
	}
	return census
}
