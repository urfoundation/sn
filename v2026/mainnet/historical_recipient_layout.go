// The only dynamic observer read is the original hotkey's Owner mapping at an
// exact recycle callback. It retains presence separately from Wasm memory.
package main

import (
	"encoding/hex"
	"errors"

	"github.com/centrifuge/go-substrate-rpc-client/v4/xxhash"
	"golang.org/x/crypto/blake2b"
)

// A new storage-pair layout cannot silently borrow the legacy capture grammar.
func (self *historicalReplayObservationProfile) validateRecipientLayout() error {
	if self.RecipientLayout != nil && (*self.RecipientLayout != nativeRecipientStorageLayoutSchema || self.Schema != historicalNativeProfileSchema) {
		return errors.New("native recipient layout scope differs")
	}
	for _, rule := range self.Rules {
		context := rule.Purpose == "native-recipient-owner-hotkey" || rule.Purpose == "native-recipient-auto-stake" || rule.Purpose == "native-recipient-owner"
		component := rule.Purpose == "native-miner-capture" || rule.Purpose == "native-miner-credit" || rule.Purpose == "native-owner-recycle"
		if (context || rule.Purpose == "native-miner-capture" || rule.RecipientOwner) && self.RecipientLayout == nil {
			return errors.New("native recipient context omitted its explicit layout")
		}
		if self.RecipientLayout != nil && (context || component) {
			call := rule.StorageCall
			if call == nil || rule.HostSnapshot != nil || len(rule.StateReads) != 0 || context && call.Operation != "get" || component && call.Operation != "get" && call.Operation != "set" || rule.RecipientOwner != (rule.Purpose == "native-owner-recycle") {
				return errors.New("native recipient context changed its exact original storage scope")
			}
		}
		if self.RecipientLayout != nil && (context || component) {
			for _, capture := range rule.Memory {
				if capture.Name == "captured" || capture.Name == "subnet-owner-hotkey" || capture.Name == "auto-stake-destination" || rule.Purpose == "native-owner-recycle" && capture.Name == "coldkey" {
					return errors.New("native recipient storage join cannot invent legacy memory fields")
				}
			}
		}
		if rule.RecipientOwner {
			found := false
			for _, capture := range rule.Memory {
				found = found || capture.Name == "hotkey" && capture.Bytes == 32 && capture.Repeat == nil
			}
			if rule.Purpose != "native-owner-recycle" || !found {
				return errors.New("native recipient Owner read omitted its exact hotkey")
			}
		}
	}
	return nil
}

// The runtime metadata is independently checked by the economic decoder; this
// fixed host recipe provides no caller-controlled storage namespace.
func historicalRecipientOwnerKey(hotkey []byte) string {
	hash, _ := blake2b.New(16, nil)
	_, _ = hash.Write(hotkey)
	key := append(xxhash.New128([]byte("SubtensorModule")).Sum(nil), xxhash.New128([]byte("Owner")).Sum(nil)...)
	key = append(key, hash.Sum(nil)...)
	key = append(key, hotkey...)
	return "0x" + hex.EncodeToString(key)
}

// Proven absence remains nil; only an actual present AccountId32 is retained.
func validateHistoricalRecipientOwnerState(record historicalNativeObservation) error {
	var hotkey []byte
	for _, value := range record.Memory {
		if value.Name != "hotkey" {
			continue
		}
		var err error
		hotkey, err = historicalReplayHex(value.BytesHex, 32)
		if err != nil || len(hotkey) != 32 || value.ElementCount != nil {
			return errors.New("native Owner read hotkey differs")
		}
	}
	if len(hotkey) != 32 || record.ExecutionState == nil || len(*record.ExecutionState) != 1 {
		return errors.New("native Owner execution-state read is absent or exceeds its exact bound")
	}
	value := (*record.ExecutionState)[0]
	if value.KeyHex != historicalRecipientOwnerKey(hotkey) {
		return errors.New("native Owner execution-state key differs from original hotkey")
	}
	if value.ValueHex != nil {
		raw, err := historicalReplayHex(*value.ValueHex, 32)
		if err != nil || len(raw) != 32 {
			return errors.New("native Owner execution-state value is not AccountId32")
		}
	}
	return nil
}
