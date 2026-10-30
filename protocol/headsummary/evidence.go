package headsummary

// The v2 envelope authenticates one private recipient census and declares the
// complete global operator scope. Missing or partial evidence never means zero.

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Evidence is absent from v1's canonical bytes. A v2 envelope identifies every
// cut explicitly; missing evidence cannot be interpreted as an empty tempo.
func (self *Summary) validateEvidence() error {
	if self.Schema == Schema {
		if self.PreviousMeasurementArtifactHash != "" || self.EvidenceEmptyPrior || self.EvidenceRecipient != 0 || self.EvidenceOperators != nil || self.EvidenceCuts != nil || self.EvidencePolicy != nil {
			return errors.New("head summary v1 contains v2 evidence")
		}
		return nil
	}
	if self.PreviousMeasurementArtifactHash != "" {
		if self.EvidenceEmptyPrior {
			return errors.New("head summary empty prior claim has a predecessor")
		}
		if err := parseContentHash(self.PreviousMeasurementArtifactHash); err != nil || self.PreviousMeasurementArtifactHash == self.MeasurementArtifactHash {
			return errors.New("head summary previous measurement artifact hash is invalid")
		}
	}
	if self.EvidencePolicy == nil || self.EvidenceRecipient == 0 || len(self.EvidenceOperators) == 0 || len(self.EvidenceOperators) > MaxEvidenceCuts || len(self.EvidenceCuts) != 1 || self.EvidenceCuts[0].NoId != self.EvidenceRecipient {
		return errors.New("head summary v2 evidence is incomplete or exceeds its wire bound")
	}
	recipientFound := false
	for index, noId := range self.EvidenceOperators {
		if noId == 0 || index > 0 && noId <= self.EvidenceOperators[index-1] {
			return errors.New("head summary evidence operator census is zero, repeated or unordered")
		}
		recipientFound = recipientFound || noId == self.EvidenceRecipient
	}
	if !recipientFound {
		return errors.New("head summary evidence recipient is absent from the operator census")
	}
	policyHash, err := self.EvidencePolicy.Hash()
	if err != nil || Hex32(policyHash) != self.PolicyHash || self.EvidencePolicy.PolicyID != self.PolicyID || self.EvidencePolicy.Steering.MaximumHeadFleets != self.MaximumHeadFleets {
		return errors.New("head summary evidence policy differs from its signed policy identity")
	}
	clients := map[string]bool{}
	cutHashes := map[string]bool{}
	for index, cut := range self.EvidenceCuts {
		if cut.NoId == 0 || index > 0 && cut.NoId <= self.EvidenceCuts[index-1].NoId || cut.NativeBlock == 0 || cut.NativeBlock > self.NativeSnapshotBlock || cut.Bindings == nil {
			return fmt.Errorf("head summary evidence cut %d has an invalid identity or census", index)
		}
		if _, err := nonzeroHex32("head summary evidence native hash", cut.NativeBlockHash); err != nil {
			return err
		}
		if _, err := nonzeroHex32("head summary evidence content hash", cut.ContentHash); err != nil {
			return err
		}
		if cutHashes[cut.ContentHash] {
			return errors.New("head summary evidence repeats a cut hash")
		}
		cutHashes[cut.ContentHash] = true
		for position, binding := range cut.Bindings {
			if len(clients) == MaxEvidenceBindings {
				return errors.New("head summary evidence binding census exceeds its wire bound")
			}
			client := binding.ClientId
			if len(client) != 36 || client != strings.ToLower(client) || client[8] != '-' || client[13] != '-' || client[18] != '-' || client[23] != '-' {
				return errors.New("head summary evidence client id is not canonical")
			}
			var decoded [16]byte
			encoded := strings.ReplaceAll(client, "-", "")
			if len(encoded) != 32 {
				return errors.New("head summary evidence client id is not canonical")
			}
			if _, err := hex.Decode(decoded[:], []byte(encoded)); err != nil || decoded == ([16]byte{}) || clients[client] || position > 0 && client <= cut.Bindings[position-1].ClientId {
				return errors.New("head summary evidence clients are zero, repeated or unordered")
			}
			if _, err := nonzeroHex32("head summary evidence fleet id", binding.FleetId); err != nil {
				return err
			}
			if _, err := nonzeroHex32("head summary evidence hotkey", binding.Hotkey); err != nil {
				return err
			}
			if binding.Generation == 0 {
				return errors.New("head summary evidence binding generation is zero")
			}
			clients[client] = true
		}
	}
	return nil
}
