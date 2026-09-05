package validator

// Exercises the private bounded authentication scope with real Ed25519
// signatures; no timer, synthetic verifier success or global hook is used.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/binary"
	"testing"

	"github.com/urnetwork/connect"
)

// Builds a distinct exact assignment using deterministic keys and nonce bytes.
// Depth 16 reaches the largest retained message; depth 17 exercises bypass.
func attemptAssignmentVerificationFixture(t *testing.T, sequence uint64, depth int) (ed25519.PrivateKey, []byte, []byte) {
	t.Helper()
	serverKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x73}, ed25519.SeedSize))
	validatorKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x74}, ed25519.SeedSize))
	nonce := make([]byte, connect.VerifyNonceSize)
	binary.LittleEndian.PutUint64(nonce, sequence+1)
	trail := make([]connect.Id, depth)
	for index := range trail {
		trail[index] = connect.Id{15: byte(index + 1)}
	}
	message, err := connect.BuildVerifyAssignMessage(255, connect.Id{0: 1}, nonce, validatorKey.Public().(ed25519.PublicKey), byte(depth), trail)
	if err != nil {
		t.Fatal(err)
	}
	return serverKey, message, ed25519.Sign(serverKey, message)
}

// A repeated public operation must authenticate its own seven unique tuples,
// even when callers reuse the same cut pointer and server-key map.
func TestAttemptCutAssignmentVerificationIsCutLocal(t *testing.T) {
	cut, validatorKey, serverKeys := attemptBoundaryLifecycleFixture(t, 8)
	verifyCount := 0
	verify := func(key, message, signature []byte) bool {
		verifyCount++
		return connect.VerifyVerifyMessageSignature(key, message, signature)
	}
	for pass := 1; pass <= 2; pass++ {
		if err := verifyAttemptLedgerCutWithAssignVerifier(cut, validatorKey.Public().(ed25519.PublicKey), serverKeys, true, verify); err != nil {
			t.Fatal(err)
		}
		if verifyCount != pass*7 {
			t.Fatalf("cut pass %d authenticated %d total tuples, want %d", pass, verifyCount, pass*7)
		}
	}
}

// A hit does not refresh FIFO age; the 65th distinct success evicts the first.
// Arbitrarily longer cuts retain the same fixed count and recheck evictions.
func TestAttemptAssignmentReuseCapacityEvictsAndReverifies(t *testing.T) {
	if attemptAssignVerificationCapacity != 64 || attemptAssignMessageMaxBytes != 359 {
		t.Fatal("assignment reuse changed its reviewed count or message-byte bound")
	}
	verifyCount := 0
	cache := attemptAssignVerificationCache{verifySignature: func(key, message, signature []byte) bool {
		verifyCount++
		return connect.VerifyVerifyMessageSignature(key, message, signature)
	}}
	verify := func(sequence uint64) {
		key, message, signature := attemptAssignmentVerificationFixture(t, sequence, 16)
		if len(message) != attemptAssignMessageMaxBytes || !cache.verify(key.Public().(ed25519.PublicKey), message, signature) {
			t.Fatalf("maximum-width assignment %d did not authenticate", sequence)
		}
		if len(cache.verifiedKVs) > 64 {
			t.Fatal("assignment reuse grew beyond 64 entries")
		}
	}
	for index := range 64 {
		verify(uint64(index))
	}
	verify(0)
	if verifyCount != 64 || len(cache.verifiedKVs) != 64 {
		t.Fatal("full cache did not reuse its oldest exact tuple")
	}
	verify(64)
	verify(0)
	verify(64)
	if verifyCount != 66 {
		t.Fatalf("FIFO eviction authenticated %d tuples, want 66", verifyCount)
	}
	verify(1)
	if verifyCount != 67 {
		t.Fatal("reinsertion failed to evict the next oldest tuple")
	}
	for index := 65; index < 4*attemptAssignVerificationCapacity; index++ {
		verify(uint64(index))
	}
	if len(cache.verifiedKVs) != 64 {
		t.Fatal("long cut lost the fixed retained tuple census")
	}
	for _, tuple := range cache.verificationTuples {
		if _, found := cache.verifiedKVs[tuple]; !found {
			t.Fatal("FIFO ring differs from the retained authentication map")
		}
	}
}

// Caller-owned slices may change after a success. Each changed key, message
// or signature must be rechecked, while restoring the original bytes is a hit.
func TestAttemptAssignmentReuseOwnsExactTupleBytes(t *testing.T) {
	key, message, signature := attemptAssignmentVerificationFixture(t, 0, 16)
	publicKey := key.Public().(ed25519.PublicKey)
	originalKey, originalMessage, originalSignature := bytes.Clone(publicKey), bytes.Clone(message), bytes.Clone(signature)
	replacementKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x75}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	verifyCount := 0
	cache := attemptAssignVerificationCache{verifySignature: func(key, message, signature []byte) bool {
		verifyCount++
		return connect.VerifyVerifyMessageSignature(key, message, signature)
	}}
	if !cache.verify(publicKey, message, signature) {
		t.Fatal("initial exact tuple failed")
	}
	for _, mutation := range []struct {
		name string
		edit func()
	}{
		{name: "key", edit: func() { copy(publicKey, replacementKey) }},
		{name: "message", edit: func() { message[len(connect.VerifyCtx)+2+16] ^= 1 }},
		{name: "signature", edit: func() { signature[0] ^= 1 }},
	} {
		before := verifyCount
		mutation.edit()
		if cache.verify(publicKey, message, signature) || verifyCount != before+1 {
			t.Fatalf("changed %s bypassed genuine authentication", mutation.name)
		}
		copy(publicKey, originalKey)
		copy(message, originalMessage)
		copy(signature, originalSignature)
		if !cache.verify(publicKey, message, signature) || verifyCount != before+1 || len(cache.verifiedKVs) != 1 {
			t.Fatalf("caller mutation changed owned %s bytes", mutation.name)
		}
	}
}

// Freshly signed changed bytes are a new successful tuple, even with the same
// server key, key ID, trail and policy depth. Their old signature still fails.
func TestAttemptAssignmentReuseAuthenticatesChangedValidMessage(t *testing.T) {
	key, firstMessage, firstSignature := attemptAssignmentVerificationFixture(t, 0, 16)
	_, secondMessage, secondSignature := attemptAssignmentVerificationFixture(t, 1, 16)
	publicKey := key.Public().(ed25519.PublicKey)
	verifyCount := 0
	cache := attemptAssignVerificationCache{verifySignature: func(key, message, signature []byte) bool {
		verifyCount++
		return connect.VerifyVerifyMessageSignature(key, message, signature)
	}}
	if !cache.verify(publicKey, firstMessage, firstSignature) || !cache.verify(publicKey, secondMessage, secondSignature) || verifyCount != 2 {
		t.Fatal("valid changed message did not receive distinct authentication")
	}
	if cache.verify(publicKey, secondMessage, firstSignature) || verifyCount != 3 || len(cache.verifiedKVs) != 2 {
		t.Fatal("changed message accepted its predecessor's signature")
	}
	if !cache.verify(publicKey, firstMessage, firstSignature) || !cache.verify(publicKey, secondMessage, secondSignature) || verifyCount != 3 {
		t.Fatal("exact successful tuples did not retain their independent results")
	}
}

// Invalid signatures never occupy a FIFO slot or suppress a later primitive
// check. A subsequent valid tuple receives its own complete authentication.
func TestAttemptAssignmentReuseDoesNotCacheFailure(t *testing.T) {
	key, message, signature := attemptAssignmentVerificationFixture(t, 0, 16)
	publicKey := key.Public().(ed25519.PublicKey)
	invalidSignature := bytes.Clone(signature)
	invalidSignature[0] ^= 1
	verifyCount := 0
	cache := attemptAssignVerificationCache{verifySignature: func(key, message, signature []byte) bool {
		verifyCount++
		return connect.VerifyVerifyMessageSignature(key, message, signature)
	}}
	for attempt := 1; attempt <= 2; attempt++ {
		if cache.verify(publicKey, message, invalidSignature) || verifyCount != attempt || len(cache.verifiedKVs) != 0 || cache.nextIndex != 0 {
			t.Fatal("invalid signature acquired retained authentication state")
		}
	}
	if !cache.verify(publicKey, message, signature) || !cache.verify(publicKey, message, signature) || verifyCount != 3 || len(cache.verifiedKVs) != 1 {
		t.Fatal("valid signature did not recover after failed checks")
	}
}

// The reuse adapter is not an alternate wire validator. Correct Ed25519
// signatures outside the fixed assignment shape bypass storage on every call;
// the containing record verifier still rejects their noncanonical context.
func TestAttemptAssignmentReuseBypassesOtherMessageShapes(t *testing.T) {
	key, ordinary, _ := attemptAssignmentVerificationFixture(t, 0, 16)
	_, oversized, _ := attemptAssignmentVerificationFixture(t, 1, 17)
	publicKey := key.Public().(ed25519.PublicKey)
	verifyCount := 0
	cache := attemptAssignVerificationCache{verifySignature: func(key, message, signature []byte) bool {
		verifyCount++
		return connect.VerifyVerifyMessageSignature(key, message, signature)
	}}
	for _, shape := range []struct {
		name    string
		message []byte
		edit    func([]byte)
	}{
		{name: "oversize", message: oversized},
		{name: "short", message: ordinary[:attemptAssignMessagePrefixBytes]},
		{name: "partial client id", message: ordinary[:len(ordinary)-1]},
		{name: "domain", message: ordinary, edit: func(message []byte) { message[0] ^= 1 }},
		{name: "message type", message: ordinary, edit: func(message []byte) { message[len(connect.VerifyCtx)] = connect.VerifyMsgTypeFinal }},
		{name: "policy depth", message: ordinary, edit: func(message []byte) { message[attemptAssignMessagePrefixBytes-2] = 17 }},
		{name: "walked depth", message: ordinary, edit: func(message []byte) { message[attemptAssignMessagePrefixBytes-1]-- }},
	} {
		message := bytes.Clone(shape.message)
		if shape.edit != nil {
			shape.edit(message)
		}
		signature := ed25519.Sign(key, message)
		before := verifyCount
		if !cache.verify(publicKey, message, signature) || !cache.verify(publicKey, message, signature) || verifyCount != before+2 || len(cache.verifiedKVs) != 0 {
			t.Fatalf("%s did not bypass bounded reuse", shape.name)
		}
	}
	_, message, signature := attemptAssignmentVerificationFixture(t, 2, 16)
	for _, input := range []struct{ key, signature []byte }{
		{key: publicKey[:len(publicKey)-1], signature: signature},
		{key: publicKey, signature: signature[:len(signature)-1]},
	} {
		before := verifyCount
		if cache.verify(input.key, message, input.signature) || cache.verify(input.key, message, input.signature) || verifyCount != before+2 || len(cache.verifiedKVs) != 0 {
			t.Fatal("invalid key/signature width acquired a retained tuple")
		}
	}
}
