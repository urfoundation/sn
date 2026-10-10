// Individual original requests use separately approved custody. They do not
// replace whole-work windows or let a retained leaf select its own authority.
package miner

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urnetwork/connect/v2026"
	"github.com/urnetwork/sdk/v2026"
)

const ProviderContractCaptureSchema = "urnetwork-provider-original-contract-capture-v1"

// The exact externally approved profile selects the persistent source namespace
// and provider key. No field supplies the SDK manager's fresh request generation.
type ProviderContractCaptureProfile struct {
	Schema    string                         `json:"schema"`
	ApiUrl    string                         `json:"api_url"`
	Providers []ProviderContractCaptureOwner `json:"providers"`
}

// SourceGeneration is the prepared custody identity. Signed request Generation
// remains the actual SDK lifecycle that observed the request and admission.
type ProviderContractCaptureOwner struct {
	Slot             string                          `json:"slot"`
	ClientId         [16]byte                        `json:"client_id"`
	PublicKey        [32]byte                        `json:"public_key"`
	Domain           protocol.ClientKeyHistoryDomain `json:"domain"`
	Directory        string                          `json:"directory"`
	SourceGeneration [16]byte                        `json:"source_generation"`
}

// A missing optional profile preserves ordinary providing and unknown original
// source coverage. Explicit and required profiles retain physical path checks.
func ReadProviderContractCaptureProfile(ctx context.Context, path, expectedSha256 string, required bool) (*ProviderContractCaptureProfile, error) {
	raw, err := readProviderCaptureProfileBytes(ctx, path, expectedSha256, required)
	if err != nil || raw == nil {
		return nil, err
	}
	profile, err := DecodeProviderContractCaptureProfile(ctx, raw, expectedSha256)
	if err != nil {
		return nil, err
	}
	if err := profile.validateCustody(); err != nil {
		return nil, err
	}
	for _, provider := range profile.Providers {
		if providerCapturePathsOverlap(path, provider.Directory) {
			return nil, errors.New("original contract profile overlaps its source custody")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return profile, nil
}

// DecodeProviderContractCaptureProfile borrows exact independently approved
// bytes without opening source directories. Offline preparation retains its own
// complete original and destination inventories; decode grants no live custody.
func DecodeProviderContractCaptureProfile(ctx context.Context, raw []byte, expectedSha256 string) (*ProviderContractCaptureProfile, error) {
	if ctx == nil {
		return nil, errors.New("original contract profile requires its caller context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expected, err := providerWorkCaptureDigest(expectedSha256)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > maximumProviderWorkCaptureBytes {
		return nil, errors.New("original contract profile exceeds its finite byte scope")
	}
	digest := sha256.Sum256(raw)
	if !bytes.Equal(digest[:], expected) {
		return nil, errors.New("original contract profile differs from its approved bytes")
	}
	if err := protocol.ValidateUniqueJsonKeys(raw); err != nil {
		return nil, err
	}
	var profile ProviderContractCaptureProfile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		return nil, err
	}
	if err := profile.validateStructure(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &profile, nil
}

// Validate checks the complete approved census and currently named private
// directories. Only the Core admission helper validates prepared source heads.
func (self ProviderContractCaptureProfile) Validate() error {
	if err := self.validateStructure(); err != nil {
		return err
	}
	return self.validateCustody()
}

func (self ProviderContractCaptureProfile) validateStructure() error {
	endpoint, err := url.Parse(self.ApiUrl)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Opaque != "" || endpoint.Fragment != "" || endpoint.Path != "" || endpoint.RawPath != "" {
		return errors.New("original contract profile requires its exact https operator origin")
	}
	if self.Schema != ProviderContractCaptureSchema || len(self.Providers) == 0 || len(self.Providers) > 64 {
		return errors.New("original contract profile requires its complete bounded provider census")
	}
	slotKVs, clientKVs, generationKVs := map[string]bool{}, map[[16]byte]bool{}, map[[16]byte]bool{}
	for index, provider := range self.Providers {
		if provider.Slot == "" || len(provider.Slot) > 96 || strings.TrimSpace(provider.Slot) != provider.Slot || strings.ContainsAny(provider.Slot, "/\\\x00\r\n") || slotKVs[provider.Slot] || provider.ClientId == ([16]byte{}) || clientKVs[provider.ClientId] || provider.PublicKey == ([32]byte{}) || provider.SourceGeneration == ([16]byte{}) || generationKVs[provider.SourceGeneration] {
			return errors.New("original contract profile repeats or omits approved identity or source generation")
		}
		slotKVs[provider.Slot], clientKVs[provider.ClientId], generationKVs[provider.SourceGeneration] = true, true, true
		if _, err := provider.Domain.Digest(); err != nil {
			return err
		}
		if !filepath.IsAbs(provider.Directory) || filepath.Clean(provider.Directory) != provider.Directory || provider.Directory == string(filepath.Separator) {
			return errors.New("original contract directory must be canonical and absolute")
		}
		for _, prior := range self.Providers[:index] {
			if providerCapturePathsOverlap(provider.Directory, prior.Directory) {
				return errors.New("original contract source directories overlap")
			}
		}
	}
	return nil
}

// Physical directory custody follows the same ownership and alias rules as
// complete-cut outboxes, while preserving distinct schemas and namespaces.
func (self ProviderContractCaptureProfile) validateCustody() error {
	physical := ProviderWorkCaptureProfile{}
	for _, provider := range self.Providers {
		physical.Providers = append(physical.Providers, ProviderWorkCaptureOwner{OutboxDirectory: provider.Directory})
	}
	return physical.validateCustody()
}

func providerCapturePathsOverlap(first, second string) bool {
	for _, pair := range [][2]string{{first, second}, {second, first}} {
		relative, err := filepath.Rel(pair[0], pair[1])
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Original-contract capture must name the actual entire role. When whole-work
// is configured, the two independent profiles must agree on each original owner.
func (self *ProviderContractCaptureProfile) validateRole(apiUrl string, slots []string, domainHash [32]byte, work *ProviderWorkCaptureProfile) error {
	if self == nil {
		return nil
	}
	if self.ApiUrl != apiUrl || len(self.Providers) != len(slots) {
		return errors.New("original contract profile differs from actual operator or complete role")
	}
	for _, slot := range slots {
		matched := false
		for _, provider := range self.Providers {
			if provider.Slot != slot {
				continue
			}
			matched = true
			digest, err := provider.Domain.Digest()
			if err != nil || domainHash != ([32]byte{}) && domainHash != digest {
				return errors.New("original contract profile differs from the approved signing domain")
			}
			if work != nil {
				workMatched := false
				for _, owner := range work.Providers {
					if providerCapturePathsOverlap(provider.Directory, owner.OutboxDirectory) {
						return errors.New("original contract source overlaps complete-cut custody")
					}
					if owner.Slot == slot {
						if owner.ClientId != provider.ClientId || owner.PublicKey != provider.PublicKey || owner.Domain != provider.Domain {
							return errors.New("original contract and whole-work approved identities disagree")
						}
						workMatched = true
					}
				}
				if !workMatched || work.ApiUrl != self.ApiUrl {
					return errors.New("original contract and whole-work profile rosters disagree")
				}
			}
		}
		if !matched {
			return errors.New("original contract profile omits an actual provider slot")
		}
	}
	return nil
}

// Capture binds to the already retained key and actual authenticated client.
// SourceGeneration belongs to durable custody and never replaces SDK generation.
func (self *ProviderContractCaptureProfile) apply(settings *sdk.DeviceLocalSettings, slot string, clientId connect.Id) error {
	if self == nil {
		return nil
	}
	if settings == nil || settings.ContractManagerSettings == nil || settings.KeyMaterial == nil {
		return errors.New("original contract capture has no retained SDK identity")
	}
	seed := settings.KeyMaterial.GetClientKeySeed()
	if len(seed) != ed25519.SeedSize {
		return errors.New("original contract capture requires its retained provider seed")
	}
	key := ed25519.NewKeyFromSeed(seed)
	for _, provider := range self.Providers {
		if provider.Slot != slot {
			continue
		}
		if provider.ClientId != [16]byte(clientId) || !bytes.Equal(provider.PublicKey[:], key[ed25519.SeedSize:]) {
			return errors.New("original contract profile differs from retained provider identity")
		}
		domainHash, err := provider.Domain.Digest()
		if err != nil || settings.ContractManagerSettings.CloseReportDomainHash != ([32]byte{}) && settings.ContractManagerSettings.CloseReportDomainHash != domainHash {
			return errors.New("original contract profile differs from retained signing domain")
		}
		settings.ContractManagerSettings.CloseReportDomainHash = domainHash
		settings.ContractManagerSettings.OriginalContractCapture = &connect.OriginalContractCaptureSettings{Directory: provider.Directory, PublicKey: provider.PublicKey, SourceGeneration: provider.SourceGeneration}
		settings.ClientKeyRegistrationRequired = true
		return nil
	}
	return errors.New("original contract profile omits the actual provider")
}
