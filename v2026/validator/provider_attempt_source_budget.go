//go:build linux || darwin

// The caller's retained-frame allowance bounds acquisition as well as the
// final serialization. The original authority document is never rewritten.
package validator

import (
	"encoding/base64"
	"encoding/json"
	"sync"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Concurrent original-object reads reserve their complete serialized size
// before transport. Failed or duplicate reads release only their own reserve.
type providerAttemptOriginalBudget struct {
	stateLock sync.Mutex
	maximum   uint64
	used      uint64
}

// Reserve arithmetic is subtraction based so it cannot wrap at the boundary.
func (self *providerAttemptOriginalBudget) reserve(size uint64) error {
	if self == nil {
		return nil
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	if size > self.maximum-self.used {
		return protocol.ErrProviderAttemptsCapacity
	}
	self.used += size
	return nil
}

// Only an operation's uncommitted reservation can be released.
func (self *providerAttemptOriginalBudget) release(size uint64) {
	if self == nil {
		return
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	self.used -= size
}

// Sequential receipt reads use only the frame space still available.
func (self *providerAttemptOriginalBudget) remaining(maximum uint64) uint64 {
	if self == nil {
		return maximum
	}
	self.stateLock.Lock()
	defer self.stateLock.Unlock()
	return min(maximum, self.maximum-self.used)
}

// Byte bodies are base64 in JSON. Include one conservative array comma; the
// initial empty array brackets are already in the frame's initial charge.
func providerAttemptObjectWireBytes(origin, kind, hash string, size uint64) (uint64, error) {
	if size > 1024*1024*1024 {
		return 0, protocol.ErrProviderAttemptsCapacity
	}
	raw, err := json.Marshal(ProviderAttemptOriginalObject{Origin: origin, Kind: kind, Hash: hash, Body: []byte{}})
	if err != nil {
		return 0, err
	}
	return uint64(len(raw)) + uint64(base64.StdEncoding.EncodedLen(int(size))) + 1, nil
}
