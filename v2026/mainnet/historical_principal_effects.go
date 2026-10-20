// Original execution effects combine read-only boundary queries with a complete
// committed storage-mutation census. A net-zero balance alone cannot hide an
// unclassified deposit and withdrawal or authorize a causal economic statement.
package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
)

// Each entry is emitted by the original top-storage host, independently of
// callsite selection. A mutation outside a reviewed effect remains unmatched.
type historicalPrincipalMutation struct {
	Ordinal     uint64                  `json:"ordinal"`
	Operation   string                  `json:"operation"`
	KeyHex      string                  `json:"key_hex"`
	ValueSha256 *historicalReplayDigest `json:"value_sha256"`
}

// The profile admits a finite, non-overlapping original storage dependency
// census. A clear-prefix covering one of those prefixes is included as well.
func validateHistoricalPrincipalPrefixes(prefixes []string) error {
	if prefixes == nil {
		return nil
	}
	if len(prefixes) == 0 || len(prefixes) > 16 {
		return errors.New("historical principal storage scope is empty or exceeds bound")
	}
	var previous [][]byte
	for _, value := range prefixes {
		prefix, err := historicalReplayHex(value, 64)
		if err != nil || len(prefix) == 0 {
			return errors.New("historical principal storage prefix is invalid")
		}
		for _, prior := range previous {
			if bytes.HasPrefix(prefix, prior) || bytes.HasPrefix(prior, prefix) {
				return errors.New("historical principal storage prefixes overlap")
			}
		}
		previous = append(previous, prefix)
	}
	return nil
}

func historicalPrincipalEffectPurpose(purpose string) bool {
	switch purpose {
	case "native-principal-deposit", "native-principal-withdrawal", "native-principal-refund", "native-principal-vault-capture", "native-principal-earning", "native-principal-support":
		return true
	}
	return false
}

func validateHistoricalPrincipalEffectsRequest(enabled bool, queries []historicalPrincipalQuery) error {
	if enabled && queries == nil {
		return errors.New("historical principal effects omitted their original query census")
	}
	return nil
}

func validateHistoricalClosingPrincipal(job historicalReplayJob, report historicalReplayReport) error {
	if err := validateHistoricalPrincipalEffectsRequest(job.PrincipalEffects, job.PrincipalQueries); err != nil {
		return err
	}
	if !job.PrincipalEffects {
		if report.ClosingPrincipals != nil {
			return errors.New("historical principal closing report has no admitted job request")
		}
		return nil
	}
	return validateHistoricalPrincipalReport(job.PrincipalQueries, report.ClosingPrincipals)
}

// Verify the independent wire census and any coincident original observation.
// The economic decoder decides whether a matching callsite establishes a cause.
func validateHistoricalPrincipalMutations(profile *historicalReplayObservationProfile, trace *historicalReplayObservations) error {
	if (profile.PrincipalStoragePrefixes == nil) != (trace.PrincipalMutations == nil) || len(trace.PrincipalMutations) > 16384 {
		return errors.New("historical principal mutation census presence or bound differs")
	}
	var previous uint64
	observations := make(map[uint64]historicalReplayObservation, len(trace.Observations))
	for _, observation := range trace.Observations {
		observations[observation.Ordinal] = observation
	}
	for _, mutation := range trace.PrincipalMutations {
		if mutation.Ordinal <= previous || mutation.Ordinal > trace.HostCalls {
			return errors.New("historical principal mutation order differs")
		}
		previous = mutation.Ordinal
		key, err := historicalReplayHex(mutation.KeyHex, 512)
		if err != nil {
			return err
		}
		matched := false
		for _, value := range profile.PrincipalStoragePrefixes {
			prefix, err := historicalReplayHex(value, 64)
			if err != nil {
				return err
			}
			matched = matched || bytes.HasPrefix(key, prefix) || mutation.Operation == "clear_prefix" && bytes.HasPrefix(prefix, key)
		}
		valueBearing := mutation.Operation == "set" || mutation.Operation == "append"
		if !matched || !valueBearing && mutation.Operation != "clear" && mutation.Operation != "clear_prefix" || valueBearing != (mutation.ValueSha256 != nil) {
			return errors.New("historical principal mutation changed original storage scope or operation")
		}
		if observation, exists := observations[mutation.Ordinal]; exists {
			if observation.KeyHex != mutation.KeyHex || observation.Operation != mutation.Operation || (observation.ValueHex != nil) != valueBearing {
				return errors.New("historical principal mutation contradicts its original callsite")
			}
			if valueBearing {
				raw, err := historicalReplayHex(*observation.ValueHex, 1024*1024)
				if err != nil || *mutation.ValueSha256 != historicalReplayDigest(sha256.Sum256(raw)) {
					return errors.New("historical principal mutation changed original write bytes")
				}
			}
		}
	}
	return nil
}
