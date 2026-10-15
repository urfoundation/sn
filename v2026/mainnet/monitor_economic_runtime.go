// Economic runtime renewal extends a reviewed read catalog; it does not amend
// the original economic domain, cursor, fee payers or historical authority.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/centrifuge/go-substrate-rpc-client/v4/types"
)

const economicRuntimeStatePurpose = "native-economic-state-v1"
const economicRuntimeEventsPurpose = "native-economic-events-v1"
const economicRuntimeFeePurpose = "native-transaction-fees-v1"

// The existing root artifact coordinates are reused. A review digest identifies
// the explicit semantic review, not an automatically established source build.
type monitorEconomicRuntimeEntry struct {
	Profile      rootReceiptProfile `json:"profile"`
	ReviewSha256 string             `json:"review_sha256"`
	Purposes     []string           `json:"purposes"`
}

// Catalog capacity is reviewed separately from the immutable earning domain.
// Renewal can grow these finite limits; no retained artifact may be evicted.
type monitorEconomicRuntimeCapacity struct {
	Entries uint64 `json:"entries"`
	Bytes   uint64 `json:"bytes"`
}

func (self monitorEconomicNativePolicy) runtimeCapacity() monitorEconomicRuntimeCapacity {
	if self.RuntimeCapacity != nil {
		return *self.RuntimeCapacity
	}
	return monitorEconomicRuntimeCapacity{Entries: 8, Bytes: 8 * 1024}
}

func monitorEconomicReadSeconds(seconds uint64) uint64 {
	if seconds == 0 {
		return 300
	}
	return seconds
}

// A renewal explicitly names the original serialized budget. The checkpoint's
// original policy hash authenticates that basis, including every other field.
func (self monitorEconomicNativePolicy) identityHash() string {
	self.Observation = nativeProducerOriginalPolicy(self.Observation)
	self.RuntimeCatalog = nil
	self.RuntimeCapacity = nil
	if self.ReadBudgetBasisSeconds != nil {
		self.ReadBudgetSeconds = *self.ReadBudgetBasisSeconds
	}
	self.ReadBudgetBasisSeconds = nil
	return rootObjectHash(self)
}

func (self monitorEconomicNativePolicy) profiles() []rootReceiptProfile {
	if len(self.RuntimeCatalog) == 0 {
		return []rootReceiptProfile{self.Observation.Runtime}
	}
	result := make([]rootReceiptProfile, 0, len(self.RuntimeCatalog))
	for _, entry := range self.RuntimeCatalog {
		if !slices.Contains(result, entry.Profile) {
			result = append(result, entry.Profile)
		}
	}
	return result
}

func (self monitorEconomicNativePolicy) validateCatalog() error {
	capacity := self.runtimeCapacity()
	if capacity.Entries < 8 || capacity.Entries > 64 || capacity.Bytes < 8*1024 || capacity.Bytes > 64*1024 {
		return errors.New("native economic runtime catalog requires reviewed capacity of 8..64 artifacts and 8192..65536 bytes")
	}
	if uint64(len(self.RuntimeCatalog)) > capacity.Entries {
		return errors.New("native economic runtime catalog exceeds its reviewed artifact capacity")
	}
	raw, err := json.Marshal(self.RuntimeCatalog)
	if err != nil || uint64(len(raw)) > capacity.Bytes {
		return errors.New("native economic runtime catalog exceeds its reviewed byte capacity")
	}
	if self.ReadBudgetBasisSeconds != nil {
		basis := *self.ReadBudgetBasisSeconds
		if basis != 0 && (basis < 60 || basis > 900) || monitorEconomicReadSeconds(self.ReadBudgetSeconds) < monitorEconomicReadSeconds(basis) {
			return errors.New("native economic read renewal must retain or increase its original finite budget")
		}
	}
	if len(self.RuntimeCatalog) == 0 {
		return nil
	}
	if self.RuntimeCatalog[0].Profile != self.Observation.Runtime {
		return errors.New("native economic runtime renewal must retain the original first artifact")
	}
	for _, purpose := range []string{economicRuntimeStatePurpose, economicRuntimeEventsPurpose, economicRuntimeFeePurpose} {
		if purpose == economicRuntimeFeePurpose && len(self.Observation.FeePayers) == 0 {
			continue
		}
		if !slices.Contains(self.RuntimeCatalog[0].Purposes, purpose) {
			return errors.New("native economic renewal removed an original consumed read purpose")
		}
	}
	seen := map[string]rootReceiptProfile{}
	grants := map[rootReceiptProfile]map[string]bool{}
	for _, entry := range self.RuntimeCatalog {
		p := entry.Profile
		v := p.RuntimeVersion
		if !planSha256(entry.ReviewSha256) || !mainnetRuntimeCodecSource(p.RuntimeSourceCommit) || v.SpecName == "" || v.SpecVersion == 0 || v.TransactionVersion == 0 || v.StateVersion != 1 || !rootCanonicalHash(p.RuntimeCodeHash) || !rootCanonicalHash(p.RuntimeMetadataHash) || len(entry.Purposes) == 0 || len(entry.Purposes) > 3 {
			return errors.New("native economic runtime requires exact reviewed artifacts and bounded read purposes")
		}
		key := rootObjectHash(struct {
			Version any
			Code    string
		}{Version: v, Code: p.RuntimeCodeHash})
		if previous, exists := seen[key]; exists && previous != p {
			return errors.New("native economic runtime tuple has contradictory artifact coordinates")
		}
		seen[key] = p
		if grants[p] == nil {
			grants[p] = map[string]bool{}
		}
		added := false
		purposes := map[string]bool{}
		for _, purpose := range entry.Purposes {
			if purpose != economicRuntimeStatePurpose && purpose != economicRuntimeEventsPurpose && purpose != economicRuntimeFeePurpose || purposes[purpose] {
				return errors.New("native economic runtime read purpose is unknown or repeated")
			}
			purposes[purpose] = true
			if !grants[p][purpose] {
				added = true
				grants[p][purpose] = true
			}
		}
		if !added {
			return errors.New("native economic runtime review adds no consumed read purpose")
		}
	}
	return nil
}

// The checksum authenticates the old capacities and acknowledged budget. A
// config-only increase is not acknowledged until its checkpoint is published.
func (self monitorEconomicNativePolicy) retainsRuntimePolicy(record monitorEconomicNativeCheckpoint) error {
	if err := self.retainsCatalog(record.RuntimeCatalog); err != nil {
		return err
	}
	previous := monitorEconomicRuntimeCapacity{Entries: 8, Bytes: 8 * 1024}
	if record.RuntimeCapacity != nil {
		previous = *record.RuntimeCapacity
	}
	capacity := self.runtimeCapacity()
	if previous.Entries < 8 || previous.Entries > 64 || previous.Bytes < 8*1024 || previous.Bytes > 64*1024 || capacity.Entries < previous.Entries || capacity.Bytes < previous.Bytes {
		return errors.New("native economic runtime renewal shrank retained capacity")
	}
	raw, err := json.Marshal(record.RuntimeCatalog)
	if err != nil || uint64(len(record.RuntimeCatalog)) > previous.Entries || uint64(len(raw)) > previous.Bytes {
		return errors.New("native economic retained runtime catalog exceeds its original capacity")
	}
	budget := self.ReadBudgetSeconds
	if self.ReadBudgetBasisSeconds != nil {
		budget = *self.ReadBudgetBasisSeconds
	}
	budget = monitorEconomicReadSeconds(budget)
	if record.ReadBudgetSeconds != nil {
		if *record.ReadBudgetSeconds < budget || *record.ReadBudgetSeconds < 60 || *record.ReadBudgetSeconds > 900 {
			return errors.New("native economic retained read budget differs from its original basis")
		}
		budget = *record.ReadBudgetSeconds
	}
	if monitorEconomicReadSeconds(self.ReadBudgetSeconds) < budget {
		return errors.New("native economic read renewal shrank an acknowledged budget")
	}
	return nil
}

// Renewal is an exact prefix extension. An old review or consumed purpose
// cannot be replaced, removed or widened by rewriting its existing entry.
func (self monitorEconomicNativePolicy) retainsCatalog(previous []monitorEconomicRuntimeEntry) error {
	if err := self.validateCatalog(); err != nil {
		return err
	}
	if len(self.RuntimeCatalog) < len(previous) {
		return errors.New("native economic runtime renewal removed retained reviews")
	}
	for index, entry := range previous {
		if rootObjectHash(entry) != rootObjectHash(self.RuntimeCatalog[index]) {
			return errors.New("native economic runtime renewal changed a retained review")
		}
	}
	return nil
}

// Only consumed layouts are checked. Root signing, zero-tip and account rules
// remain in nativeRuntimeAt and are never relaxed by this observational reader.
func economicRuntimeFor(ctx context.Context, chain *rootCanonicalChain, block string, policy economicEmissionPolicy, catalog []monitorEconomicRuntimeEntry, purpose string) (rootReceiptRuntime, error) {
	if purpose != economicRuntimeStatePurpose && purpose != economicRuntimeEventsPurpose && purpose != economicRuntimeFeePurpose {
		return rootReceiptRuntime{}, errRootReceiptProfileUnavailable
	}
	runtime, err := chain.authenticatedRuntimeAt(ctx, block)
	if err != nil {
		return runtime, err
	}
	if len(catalog) != 0 {
		permitted := false
		for _, entry := range catalog {
			if entry.Profile == runtime.profile && slices.Contains(entry.Purposes, purpose) {
				permitted = true
				break
			}
		}
		if !permitted {
			return rootReceiptRuntime{}, errRootReceiptProfileUnavailable
		}
	}
	if err := validateEconomicRuntimePurpose(runtime.metadata, policy, purpose); err != nil {
		return rootReceiptRuntime{}, errors.Join(errRootReceiptProfileUnavailable, err)
	}
	return runtime, ctx.Err()
}

func validateEconomicRuntimePurpose(metadata *types.Metadata, policy economicEmissionPolicy, purpose string) error {
	switch purpose {
	case economicRuntimeStatePurpose:
		if _, err := observationStorageProfile(metadata, economicEmissionStorageSpecs); err != nil {
			return err
		}
		_, _, err := recycleModeStorage(metadata, policy.Netuid)
		return err
	case economicRuntimeEventsPurpose, economicRuntimeFeePurpose:
		_, err := economicEmissionEventProfile(metadata)
		return err
	default:
		return errRootReceiptProfileUnavailable
	}
}
