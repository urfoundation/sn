// Native treasury custody is a public descriptor. Parsing never opens a Ledger
// reference, creates a key, or supplies authority to spend or publish weights.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"

	"github.com/urfoundation/sn/v2026/crv4"
	"gopkg.in/yaml.v3"
)

const treasuryCustodySchema = "urnetwork-native-treasury-custody-v1"
const treasuryDescriptorLimit = 128 * 1024

// References identify public owner-local device configuration, never commands.
type treasuryDeviceReference struct {
	Path   string `json:"path" yaml:"path"`
	Bytes  uint64 `json:"bytes" yaml:"bytes"`
	Sha256 string `json:"sha256" yaml:"sha256"`
}

// The complete signer set is ordered by raw AccountId32, as pallet_multisig is.
type treasurySignatory struct {
	AccountId       string                  `json:"account_id" yaml:"account_id"`
	SignatureScheme string                  `json:"signature_scheme" yaml:"signature_scheme"`
	DeviceConfig    treasuryDeviceReference `json:"device_config" yaml:"device_config"`
}

// Receiving native incentives does not require an online hotkey signer.
type treasuryHotkey struct {
	AccountId    string                  `json:"account_id" yaml:"account_id"`
	DeviceConfig treasuryDeviceReference `json:"device_config" yaml:"device_config"`
}

// Device references remain local; the economic projection contains public IDs.
type treasuryDescriptor struct {
	Schema      string `json:"schema" yaml:"schema"`
	Profile     string `json:"profile" yaml:"profile"`
	Netuid      uint16 `json:"netuid" yaml:"netuid"`
	GenesisHash string `json:"genesis_hash" yaml:"genesis_hash"`
	Multisig    struct {
		AccountId   string              `json:"account_id" yaml:"account_id"`
		Threshold   uint16              `json:"threshold" yaml:"threshold"`
		Signatories []treasurySignatory `json:"signatories" yaml:"signatories"`
	} `json:"multisig" yaml:"multisig"`
	RecipientHotkeys []treasuryHotkey `json:"recipient_hotkeys" yaml:"recipient_hotkeys"`
}

// This bound admits only canonical, separately pinned public file references.
func (self treasuryDeviceReference) validate() error {
	if !filepath.IsAbs(self.Path) || filepath.Clean(self.Path) != self.Path || self.Bytes == 0 || self.Bytes > treasuryDescriptorLimit || !rootCanonicalHash(self.Sha256) {
		return errors.New("treasury device reference requires an absolute path, exact bounded byte count and canonical SHA-256")
	}
	return nil
}

// The same public derivation helper is consumed by the validator policy.
func (self treasuryDescriptor) validate() error {
	if self.Schema != treasuryCustodySchema || self.Profile != "mainnet" || self.Netuid != 25 || !rootCanonicalHash(self.GenesisHash) || !rootCanonicalHash(self.Multisig.AccountId) || len(self.RecipientHotkeys) < 2 || len(self.RecipientHotkeys) > 100 {
		return errors.New("treasury requires its mainnet schema, native genesis, derived multisig and two through 100 recipients")
	}
	accounts := make([][32]byte, len(self.Multisig.Signatories))
	seen := map[string]bool{self.Multisig.AccountId: true}
	for i, signer := range self.Multisig.Signatories {
		if !rootCanonicalHash(signer.AccountId) || signer.SignatureScheme != "ed25519" || seen[signer.AccountId] {
			return errors.New("treasury requires distinct public Ledger Ed25519 signatories")
		}
		if err := signer.DeviceConfig.validate(); err != nil {
			return err
		}
		raw, _ := hex.DecodeString(signer.AccountId[2:])
		copy(accounts[i][:], raw)
		seen[signer.AccountId] = true
	}
	derived, err := crv4.DeriveNativeMultisigAccount(accounts, self.Multisig.Threshold)
	if err != nil || self.Multisig.AccountId != "0x"+hex.EncodeToString(derived[:]) {
		return errors.Join(errors.New("treasury account differs from exact native multisig derivation"), err)
	}
	previous := ""
	for _, recipient := range self.RecipientHotkeys {
		if !rootCanonicalHash(recipient.AccountId) || seen[recipient.AccountId] || recipient.AccountId <= previous {
			return errors.New("treasury recipient hotkeys must be sorted, unique and distinct from coldkeys")
		}
		if err := recipient.DeviceConfig.validate(); err != nil {
			return err
		}
		seen[recipient.AccountId], previous = true, recipient.AccountId
	}
	return nil
}

// Alias, duplicate and merge keys cannot hide unknown or private-key fields.
func decodeTreasuryDescriptor(raw []byte) (treasuryDescriptor, error) {
	var result treasuryDescriptor
	if err := decodeTreasuryYaml(raw, &result); err != nil {
		return result, err
	}
	return result, result.validate()
}

// Receiving and sending descriptors share strict syntax without sharing authority.
func decodeTreasuryYaml(raw []byte, result any) error {
	if len(raw) == 0 || len(raw) > treasuryDescriptorLimit {
		return errors.New("treasury descriptor exceeds bound")
	}
	var document yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&document); err != nil {
		return err
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("treasury descriptor requires exactly one YAML document")
	}
	var inspect func(*yaml.Node, int) error
	inspect = func(node *yaml.Node, depth int) error {
		if depth > 16 || node.Kind == yaml.AliasNode || node.Anchor != "" {
			return errors.New("treasury aliases, anchors or excessive nesting are forbidden")
		}
		if node.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(node.Content); i += 2 {
				key := node.Content[i]
				if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" || seen[key.Value] {
					return errors.New("treasury duplicate or non-string field")
				}
				seen[key.Value] = true
			}
		}
		for _, child := range node.Content {
			if err := inspect(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := inspect(&document, 0); err != nil {
		return err
	}
	decoder = yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(result); err != nil {
		return err
	}
	return nil
}

// Only an explicitly named local public descriptor is opened, under its pin.
func readTreasuryDescriptor(ctx context.Context, path, expected string) (treasuryDescriptor, error) {
	var result treasuryDescriptor
	if !planSha256(expected) {
		return result, errors.New("treasury descriptor requires an independent file SHA-256")
	}
	raw, actual, err := readBootstrapRootFile(ctx, path, treasuryDescriptorLimit)
	if err != nil || actual != expected {
		return result, errors.Join(errors.New("treasury descriptor file differs from independent pin"), err)
	}
	return decodeTreasuryDescriptor(raw)
}
