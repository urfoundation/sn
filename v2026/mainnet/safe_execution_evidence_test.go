// Pure evidence is compared with maintained Safe bytecode under synthetic state.
// No test depends on a network, deadline, external compiler or real identity.
package main

import (
	"bytes"
	"math/big"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

// A release catalog is broader than its materialized member set. Missing, empty
// and malformed unselected variants must never reach the oracle decoder.
func TestSafeExecutionOracleFiltersVariantBeforeDecoding(t *testing.T) {
	for _, c := range safeExecutionTestProfiles {
		pin, _, members := safeReleaseTestInputs(t, c.version, c.variant)
		var selected, unselected safeReleaseArtifactPin
		for _, artifact := range pin.Artifacts {
			if artifact.Name == c.variant {
				selected = artifact
			} else if artifact.Name != "SafeProxy" {
				unselected = artifact
			}
		}
		if selected.Name == "" || unselected.Name == "" || len(members[unselected.ArchivePath]) != 0 {
			t.Fatal("oracle regression did not retain a broader catalog than selected members")
		}
		for _, unselectedBytes := range [][]byte{nil, {}, []byte("synthetic ignored non-JSON member")} {
			if unselectedBytes == nil {
				delete(members, unselected.ArchivePath)
			} else {
				members[unselected.ArchivePath] = unselectedBytes
			}
			raw, singleton, proxy := safeExecutionOracleArtifacts(t, pin, c.variant, members)
			if safeReleaseHash(raw) != selected.ArtifactSha256 || singleton.Name != c.variant || proxy.Name != "SafeProxy" ||
				len(singleton.Runtime) < 4 || len(proxy.Runtime) < 4 {
				t.Fatal("unselected artifact changed the selected oracle census")
			}
		}
	}
}

// Both maintained versions and variants must match the actual proxy domain.
func TestSafeExecutionDigestMatchesPinnedProxyAndSingleton(t *testing.T) {
	for _, c := range safeExecutionTestProfiles {
		fixture := newSafeExecutionFixture(t, c.version, c.variant)
		transaction := fixture.transaction
		transaction.Value, transaction.GasPrice = big.NewInt(713), big.NewInt(9)
		transaction.GasToken = fixture.owners[0]
		transaction.RefundReceiver = fixture.owners[1]
		transaction.Operation = 1
		digest, err := fixture.profile.transactionDigest(transaction)
		if err != nil || digest.Hash != fixture.oracleDigest(transaction) {
			t.Fatalf("pure digest differs from published proxy oracle for %s/%s: %v %+v", c.version, c.variant, err, digest)
		}
		if len(digest.Preimage) != 66 || !bytes.Equal(digest.Preimage[:2], []byte{0x19, 0x01}) ||
			crypto.Keccak256Hash(digest.Preimage) != digest.Hash || digest.Scope != (safeExecutionScope{}) ||
			digest.Version != c.version || digest.Variant != c.variant || digest.ArtifactSha256 != safeReleaseHash(fixture.rawArtifact) {
			t.Fatal("pure digest lost its exact artifact binding or inferred authority")
		}
	}
}

// Every signed field contributes independently; mutable input and output buffers
// cannot change a prior result or the caller's baseline transaction.
func TestSafeExecutionDigestBindsEveryField(t *testing.T) {
	fixture := newSafeExecutionFixture(t, "1.5.0", "Safe")
	original := fixture.transaction
	baseline, err := fixture.profile.transactionDigest(original)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		mutate func(*safeExecutionTransaction)
	}{
		{name: "chain", mutate: func(value *safeExecutionTransaction) { value.ChainId.Add(value.ChainId, big.NewInt(1)) }},
		{name: "proxy", mutate: func(value *safeExecutionTransaction) { value.Safe[0] ^= 1 }},
		{name: "to", mutate: func(value *safeExecutionTransaction) { value.To[0] ^= 1 }},
		{name: "value", mutate: func(value *safeExecutionTransaction) { value.Value.SetInt64(1) }},
		{name: "data", mutate: func(value *safeExecutionTransaction) { value.Data[0] ^= 1 }},
		{name: "operation", mutate: func(value *safeExecutionTransaction) { value.Operation = 1 }},
		{name: "safe gas", mutate: func(value *safeExecutionTransaction) { value.SafeTxGas.Add(value.SafeTxGas, big.NewInt(1)) }},
		{name: "base gas", mutate: func(value *safeExecutionTransaction) { value.BaseGas.Add(value.BaseGas, big.NewInt(1)) }},
		{name: "gas price", mutate: func(value *safeExecutionTransaction) { value.GasPrice.SetInt64(1) }},
		{name: "gas token", mutate: func(value *safeExecutionTransaction) { value.GasToken[0] = 1 }},
		{name: "refund receiver", mutate: func(value *safeExecutionTransaction) { value.RefundReceiver[0] = 1 }},
		{name: "nonce", mutate: func(value *safeExecutionTransaction) { value.Nonce.Add(value.Nonce, big.NewInt(1)) }},
	}
	for _, c := range cases {
		changed := cloneSafeExecutionTransaction(original)
		c.mutate(&changed)
		digest, err := fixture.profile.transactionDigest(changed)
		if err != nil || digest.Hash == baseline.Hash {
			t.Fatalf("signed Safe field was not bound: %s: %v", c.name, err)
		}
		if c.name != "proxy" && digest.Hash != fixture.oracleDigest(changed) {
			t.Fatalf("changed field disagrees with published oracle: %s", c.name)
		}
	}
	baseline.Preimage[2] ^= 1
	again, err := fixture.profile.transactionDigest(original)
	if err != nil || again.Hash != baseline.Hash || bytes.Equal(baseline.Preimage, again.Preimage) {
		t.Fatal("digest output ownership mutated a fresh result")
	}
}

// Integer and byte limits fail before encoding; exact supported boundaries stay
// mathematical inputs without becoming actual account or transaction authority.
func TestSafeExecutionProfileAndInputBoundsRefuseSubstitution(t *testing.T) {
	fixture := newSafeExecutionFixture(t, "1.4.1", "Safe")
	for _, change := range []struct{ version, variant string }{
		{version: "1.5.0", variant: "Safe"}, {version: "1.4.1", variant: "SafeL2"},
		{version: "1.4.0", variant: "Safe"}, {version: "1.4.1", variant: "Other"},
	} {
		if _, err := newSafeExecutionProfile(change.version, change.variant, fixture.rawArtifact); err == nil {
			t.Fatal("execution profile accepted substituted release or variant")
		}
	}
	mutated := slices.Clone(fixture.rawArtifact)
	mutated[len(mutated)-1] ^= 1
	if _, err := newSafeExecutionProfile("1.4.1", "Safe", mutated); err == nil {
		t.Fatal("execution profile accepted changed artifact bytes")
	}
	for _, profile := range []*safeExecutionProfile{nil, {}} {
		if _, err := profile.transactionDigest(fixture.transaction); err == nil {
			t.Fatal("absent execution profile admitted a digest")
		}
	}
	maximum := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	for index := 0; index < 6; index++ {
		for _, value := range []*big.Int{nil, big.NewInt(-1), new(big.Int).Add(maximum, big.NewInt(1)), maximum} {
			transaction := cloneSafeExecutionTransaction(fixture.transaction)
			fields := []**big.Int{&transaction.ChainId, &transaction.Value, &transaction.SafeTxGas, &transaction.BaseGas, &transaction.GasPrice, &transaction.Nonce}
			*fields[index] = value
			_, err := fixture.profile.transactionDigest(transaction)
			if (value == maximum) != (err == nil) {
				t.Fatalf("Safe uint256 boundary differs for field %d: %v", index, err)
			}
		}
	}
	for _, size := range []int{maximumSafeExecutionBytes, maximumSafeExecutionBytes + 1} {
		transaction := cloneSafeExecutionTransaction(fixture.transaction)
		transaction.Data = make([]byte, size)
		_, err := fixture.profile.transactionDigest(transaction)
		if (size <= maximumSafeExecutionBytes) != (err == nil) {
			t.Fatal("Safe calldata byte bound differs")
		}
		_, err = fixture.profile.encodeTransaction(fixture.transaction, make([]byte, size))
		if (size <= maximumSafeExecutionBytes) != (err == nil) {
			t.Fatal("Safe encoded signature byte bound differs")
		}
	}
	transaction := cloneSafeExecutionTransaction(fixture.transaction)
	transaction.Operation = 2
	if _, err := fixture.profile.transactionDigest(transaction); err == nil {
		t.Fatal("Safe unsupported operation admitted")
	}
}

// The signature-bearing call uses published ABI encoding; its nonce is bound
// through the digest and cannot be mistaken for an explicit calldata argument.
func TestSafeExecutionCallMatchesPinnedAbi(t *testing.T) {
	for _, c := range safeExecutionTestProfiles {
		fixture := newSafeExecutionFixture(t, c.version, c.variant)
		transaction := fixture.transaction
		signatures := fixture.signatures(transaction, "raw", "eth-sign")
		call, err := fixture.profile.encodeTransaction(transaction, signatures)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := fixture.oracleAbi.Pack("execTransaction", transaction.To, transaction.Value, transaction.Data,
			transaction.Operation, transaction.SafeTxGas, transaction.BaseGas, transaction.GasPrice,
			transaction.GasToken, transaction.RefundReceiver, signatures)
		if err != nil || !bytes.Equal(call, expected) || !bytes.Equal(call[:4], common.FromHex("0x6a761202")) {
			t.Fatal("Safe execution calldata differs from published ABI")
		}
		changed := cloneSafeExecutionTransaction(transaction)
		changed.Nonce.Add(changed.Nonce, big.NewInt(1))
		sameCall, err := fixture.profile.encodeTransaction(changed, signatures)
		if err != nil || !bytes.Equal(call, sameCall) || fixture.oracleDigest(changed) == fixture.oracleDigest(transaction) {
			t.Fatal("Safe calldata and signed nonce were conflated")
		}
		signatures[0] ^= 1
		transaction.Data[0] ^= 1
		if !bytes.Equal(call, expected) {
			t.Fatal("Safe encoded calldata aliases caller data")
		}
	}
}

// Published bytecode admits raw, personal-sign and high-s signatures alike;
// successful recovery cannot prove current owner membership or threshold.
func TestSafeExecutionSignaturesRecoverAndKeepAuthorityUnresolved(t *testing.T) {
	for _, c := range safeExecutionTestProfiles {
		fixture := newSafeExecutionFixture(t, c.version, c.variant)
		for _, mode := range []string{"raw", "eth-sign", "high-s"} {
			signatures := fixture.signatures(fixture.transaction, mode, mode)
			if err := fixture.oracleCheck(fixture.transaction, signatures, fixture.owners[2]); err != nil {
				t.Fatalf("published Safe rejected valid %s signature: %v", mode, err)
			}
			result, err := fixture.profile.inspectSignatures(fixture.transaction, signatures, 2)
			if err != nil || len(result.Prefix) != 2 || !result.EcdsaPrefixVerified {
				t.Fatalf("Safe compatible ECDSA prefix rejected for %s: %v", mode, err)
			}
			for i, entry := range result.Prefix {
				if entry.Signer != fixture.owners[i] || !entry.EcdsaRecoveryVerified || entry.ContractStateRequired || entry.ApprovedHashOrExecutorRequired {
					t.Fatalf("Safe compatible ECDSA prefix rejected for %s: recovered signer differs from published oracle", mode)
				}
			}
			if result.Digest != fixture.oracleDigest(fixture.transaction) || result.SignaturesSha256 != safeReleaseHash(signatures) || result.RequiredSignatures != 2 || result.Scope != (safeExecutionScope{}) {
				t.Fatal("Safe ECDSA recovery inferred live owner or execution authority")
			}
		}
	}
}

// Signature syntax rejects malformed prefixes, duplicates and recovery values.
// A different digest may recover a different key; only the stateful oracle can
// reject that recovered key as absent from its synthetic owner set.
func TestSafeExecutionSignaturesBindOrderBoundsAndRecovery(t *testing.T) {
	fixture := newSafeExecutionFixture(t, "1.5.0", "Safe")
	valid := fixture.signatures(fixture.transaction, "raw", "raw")
	cases := []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "reversed", mutate: func(value []byte) []byte { return append(slices.Clone(value[65:]), value[:65]...) }},
		{name: "duplicate", mutate: func(value []byte) []byte { copy(value[65:], value[:65]); return value }},
		{name: "short", mutate: func(value []byte) []byte { return value[:129] }},
		{name: "zero r", mutate: func(value []byte) []byte { clear(value[:32]); return value }},
		{name: "zero s", mutate: func(value []byte) []byte { clear(value[32:64]); return value }},
		{name: "curve r", mutate: func(value []byte) []byte { crypto.S256().Params().N.FillBytes(value[:32]); return value }},
		{name: "curve s", mutate: func(value []byte) []byte { crypto.S256().Params().N.FillBytes(value[32:64]); return value }},
		{name: "invalid v", mutate: func(value []byte) []byte { value[64] = 29; return value }},
		{name: "invalid eth v", mutate: func(value []byte) []byte { value[64] = 33; return value }},
	}
	for _, c := range cases {
		signatures := c.mutate(slices.Clone(valid))
		if _, err := fixture.profile.inspectSignatures(fixture.transaction, signatures, 2); err == nil {
			t.Fatalf("Safe malformed signature prefix accepted: %s", c.name)
		}
		if err := fixture.oracleCheck(fixture.transaction, signatures, fixture.owners[2]); err == nil {
			t.Fatalf("published oracle admitted malformed signature: %s", c.name)
		}
	}
	for _, count := range []int{0, 3, maximumSafeExecutionSignatures + 1} {
		if _, err := fixture.profile.inspectSignatures(fixture.transaction, valid, count); err == nil {
			t.Fatal("Safe signature prefix count admitted")
		}
	}
	if _, err := fixture.profile.inspectSignatures(fixture.transaction, make([]byte, maximumSafeExecutionBytes+1), 2); err == nil {
		t.Fatal("Safe oversized signatures admitted")
	}
	trailing := append(slices.Clone(valid), []byte("synthetic ignored suffix")...)
	result, err := fixture.profile.inspectSignatures(fixture.transaction, trailing, 2)
	if err != nil || len(result.Prefix) != 2 || result.SignaturesSha256 == safeReleaseHash(valid) || fixture.oracleCheck(fixture.transaction, trailing, fixture.owners[2]) != nil {
		t.Fatal("Safe trailing bytes changed threshold-prefix semantics or lost exact input hash")
	}
	changed := cloneSafeExecutionTransaction(fixture.transaction)
	changed.ChainId.Add(changed.ChainId, big.NewInt(1))
	result, err = fixture.profile.inspectSignatures(changed, valid[:65], 1)
	if err != nil || result.Prefix[0].Signer == fixture.owners[0] || result.Scope != (safeExecutionScope{}) {
		t.Fatal("changed Safe domain was mistaken for authenticated ownership")
	}
	if fixture.oracleCheck(changed, valid, fixture.owners[2]) == nil {
		t.Fatal("published oracle admitted signatures from another domain")
	}
}

// The actual singleton callback proves the release-specific preimage and magic
// value. Even a successful synthetic callback leaves external state unresolved.
func TestSafeExecutionContractSignatureChecksAreVersionedAndUnresolved(t *testing.T) {
	for _, c := range safeExecutionTestProfiles {
		fixture := newSafeExecutionFixture(t, c.version, c.variant)
		signatures := fixture.contractSignatures([]byte("synthetic contract signature"))
		result, err := fixture.profile.inspectSignatures(fixture.transaction, signatures, 2)
		if err != nil || len(result.Prefix) != 2 {
			t.Fatalf("contract signature structure rejected: %v", err)
		}
		entry := result.Prefix[0]
		magic := common.FromHex("0x20c13b0b")
		if c.version == "1.5.0" {
			magic = common.FromHex("0x1626ba7e")
		}
		if !entry.ContractStateRequired || result.EcdsaPrefixVerified || entry.EcdsaRecoveryVerified ||
			entry.ApprovedHashOrExecutorRequired || entry.Signer != fixture.owners[0] || entry.Kind != "contract" ||
			!bytes.Equal(entry.ContractReturnMagic[:], magic) || result.Scope != (safeExecutionScope{}) {
			t.Fatal("contract signature inferred state authority or lost versioned magic")
		}
		word := make([]byte, 32)
		copy(word, magic)
		code := append([]byte{0x7f}, word...)
		code = append(code, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3)
		fixture.state.SetCode(fixture.owners[0], code, tracing.CodeChangeUnspecified)
		var observed []byte
		fixture.vm.EVMConfig.Tracer = &tracing.Hooks{OnEnter: func(depth int, typ byte, from, to common.Address, input []byte, gas uint64, value *big.Int) {
			if typ == byte(vm.STATICCALL) && to == fixture.owners[0] {
				observed = slices.Clone(input)
			}
		}}
		if err := fixture.oracleCheck(fixture.transaction, signatures, fixture.owners[2]); err != nil || !bytes.Equal(observed, entry.ContractCall) {
			t.Fatalf("contract callback differs from published versioned oracle: %s/%s %v", c.version, c.variant, err)
		}
		code[1] ^= 1
		fixture.state.SetCode(fixture.owners[0], code, tracing.CodeChangeUnspecified)
		if fixture.oracleCheck(fixture.transaction, signatures, fixture.owners[2]) == nil {
			t.Fatal("published oracle accepted wrong contract return magic")
		}
		signatures[len(signatures)-1] ^= 1
		if !bytes.Equal(observed, entry.ContractCall) {
			t.Fatal("contract callback encoding aliases caller signature bytes")
		}
	}
}

// Offsets cannot enter the fixed prefix or overflow. Approved-hash signatures
// retain their executor/storage requirement even when a test executor succeeds.
func TestSafeExecutionContractOffsetsAndApprovalsCannotInferAuthority(t *testing.T) {
	for _, c := range safeExecutionTestProfiles {
		fixture := newSafeExecutionFixture(t, c.version, c.variant)
		valid := fixture.contractSignatures([]byte("synthetic signature"))
		maximum := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
		cases := []struct {
			name   string
			mutate func([]byte)
		}{
			{name: "prefix overlap", mutate: func(value []byte) {
				clear(value[65:130])
				copy(value[77:97], fixture.owners[1][:])
				value[129] = 1
				big.NewInt(98).FillBytes(value[32:64])
			}},
			{name: "length word", mutate: func(value []byte) { big.NewInt(int64(len(value) - 31)).FillBytes(value[32:64]) }},
			{name: "huge offset", mutate: func(value []byte) { maximum.FillBytes(value[32:64]) }},
			{name: "long payload", mutate: func(value []byte) { big.NewInt(int64(len(value))).FillBytes(value[130:162]) }},
			{name: "huge length", mutate: func(value []byte) { maximum.FillBytes(value[130:162]) }},
			{name: "zero owner", mutate: func(value []byte) { clear(value[:32]) }},
			{name: "sentinel owner", mutate: func(value []byte) { clear(value[:32]); value[31] = 1 }},
		}
		for _, test := range cases {
			changed := slices.Clone(valid)
			test.mutate(changed)
			if _, err := fixture.profile.inspectSignatures(fixture.transaction, changed, 2); err == nil {
				t.Fatalf("Safe contract signature boundary admitted: %s", test.name)
			}
		}
		approved := fixture.signatures(fixture.transaction, "raw", "raw")
		clear(approved[:65])
		copy(approved[12:32], fixture.owners[0][:])
		approved[0], approved[64] = 255, 1
		maximum.FillBytes(approved[32:64])
		result, err := fixture.profile.inspectSignatures(fixture.transaction, approved, 2)
		if err != nil || result.EcdsaPrefixVerified || len(result.Prefix) != 2 ||
			result.Prefix[0].Signer != fixture.owners[0] || !result.Prefix[0].ApprovedHashOrExecutorRequired ||
			result.Prefix[0].EcdsaRecoveryVerified || result.Prefix[0].ContractStateRequired || result.Scope != (safeExecutionScope{}) {
			t.Fatalf("approved hash signature inferred executor or storage authority: %v", err)
		}
		if fixture.oracleCheck(fixture.transaction, approved, fixture.owners[0]) != nil || fixture.oracleCheck(fixture.transaction, approved, fixture.owners[2]) == nil {
			t.Fatal("published oracle did not require matching executor or approved hash")
		}
	}
}

// Actual published execution distinguishes committed inner failure from outer
// rollback, including both zero-gas failure encodings and SafeL2's extra event.
func TestSafeExecutionOutcomesFollowPinnedContractAndNonce(t *testing.T) {
	for _, c := range safeExecutionTestProfiles {
		for _, outcome := range []string{"safe-inner-success", "safe-inner-failure", "outer-reverted"} {
			fixture := newSafeExecutionFixture(t, c.version, c.variant)
			transaction := fixture.transaction
			if outcome != "safe-inner-success" {
				fixture.state.SetCode(transaction.To, common.FromHex("0x60006000fd"), tracing.CodeChangeUnspecified)
			}
			if outcome == "outer-reverted" {
				transaction.SafeTxGas = big.NewInt(0)
			}
			receipt, output, executionError := fixture.execute(transaction, fixture.signatures(transaction, "raw", "eth-sign"))
			result, err := fixture.profile.classifyReceipt(transaction, receipt.TransactionHash, receipt)
			if err != nil || result.Outcome != outcome || result.Scope != (safeExecutionScope{}) {
				t.Fatalf("published Safe outcome was not preserved: %s/%s %s %v %+v", c.version, c.variant, outcome, err, result)
			}
			stored := fixture.state.GetState(transaction.Safe, common.BigToHash(big.NewInt(5)))
			if outcome == "outer-reverted" {
				if executionError == nil || result.SafeNonceEffect != "rolled-back" || result.NonceAfterIncrement != nil || result.Payment != nil || stored != common.BigToHash(transaction.Nonce) || len(receipt.Logs) != 0 {
					t.Fatal("outer revert did not roll back Safe nonce and events")
				}
				if c.version == "1.4.1" && !bytes.Contains(output, []byte("GS013")) || c.version == "1.5.0" && len(output) != 0 {
					t.Fatal("published zero-gas failure encoding differs")
				}
			} else {
				expectedNonce := new(big.Int).Add(transaction.Nonce, big.NewInt(1))
				if executionError != nil || result.SafeNonceEffect != "committed-increment" || result.NonceAfterIncrement == nil || result.NonceAfterIncrement.Cmp(expectedNonce) != 0 ||
					stored != common.BigToHash(expectedNonce) || result.Payment == nil || result.Payment.Sign() != 0 || len(output) != 32 || (output[31] == 1) != (outcome == "safe-inner-success") {
					t.Fatal("committed Safe outcome lost inner result or nonce consumption")
				}
			}
		}
	}
}

// Unchecked uint256 increment is a published source fact, not permission to use
// a wrapped nonce or a claim about the final state of a canonical block.
func TestSafeExecutionNonceWrapHasNoCurrentStateAuthority(t *testing.T) {
	for _, c := range safeExecutionTestProfiles {
		fixture := newSafeExecutionFixture(t, c.version, c.variant)
		transaction := fixture.transaction
		transaction.Nonce = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
		fixture.state.SetState(transaction.Safe, common.BigToHash(big.NewInt(5)), common.BigToHash(transaction.Nonce))
		receipt, _, executionError := fixture.execute(transaction, fixture.signatures(transaction, "raw", "raw"))
		result, err := fixture.profile.classifyReceipt(transaction, receipt.TransactionHash, receipt)
		if executionError != nil || err != nil || result.NonceAfterIncrement == nil || result.NonceAfterIncrement.Sign() != 0 ||
			fixture.state.GetState(transaction.Safe, common.BigToHash(big.NewInt(5))) != (common.Hash{}) || result.Scope != (safeExecutionScope{}) {
			t.Fatalf("Safe nonce wrap disagrees with published unchecked increment: %v %v", executionError, err)
		}
	}
}

// An outer success status cannot substitute for the exact Safe outcome event.
// Every mutation starts from a real pinned-code execution and changes one claim.
func TestSafeExecutionOutcomeRejectsForgedOrIncompleteEvents(t *testing.T) {
	fixture := newSafeExecutionFixture(t, "1.5.0", "Safe")
	transaction := fixture.transaction
	baseline, _, executionError := fixture.execute(transaction, fixture.signatures(transaction, "raw", "raw"))
	if executionError != nil || len(baseline.Logs) != 1 {
		t.Fatalf("published event fixture failed: %v %+v", executionError, baseline)
	}
	cases := []struct {
		name   string
		mutate func(*safeExecutionReceipt)
	}{
		{name: "outer hash", mutate: func(value *safeExecutionReceipt) { value.TransactionHash[0] ^= 1 }},
		{name: "empty block", mutate: func(value *safeExecutionReceipt) { value.BlockHash = common.Hash{} }},
		{name: "outer recipient", mutate: func(value *safeExecutionReceipt) { value.To[0] ^= 1 }},
		{name: "invalid status", mutate: func(value *safeExecutionReceipt) { value.Status = 2 }},
		{name: "reverted logs", mutate: func(value *safeExecutionReceipt) { value.Status = 0 }},
		{name: "missing outcome", mutate: func(value *safeExecutionReceipt) { value.Logs = nil }},
		{name: "nil log", mutate: func(value *safeExecutionReceipt) { value.Logs[0] = nil }},
		{name: "removed log", mutate: func(value *safeExecutionReceipt) { value.Logs[0].Removed = true }},
		{name: "wrong emitter", mutate: func(value *safeExecutionReceipt) { value.Logs[0].Address[0] ^= 1 }},
		{name: "wrong event", mutate: func(value *safeExecutionReceipt) { value.Logs[0].Topics[0][0] ^= 1 }},
		{name: "wrong digest", mutate: func(value *safeExecutionReceipt) { value.Logs[0].Topics[1][0] ^= 1 }},
		{name: "missing digest", mutate: func(value *safeExecutionReceipt) { value.Logs[0].Topics = value.Logs[0].Topics[:1] }},
		{name: "extra topic", mutate: func(value *safeExecutionReceipt) { value.Logs[0].Topics = append(value.Logs[0].Topics, common.Hash{}) }},
		{name: "short payment", mutate: func(value *safeExecutionReceipt) { value.Logs[0].Data = value.Logs[0].Data[:31] }},
		{name: "long payment", mutate: func(value *safeExecutionReceipt) { value.Logs[0].Data = append(value.Logs[0].Data, 0) }},
		{name: "unexpected payment", mutate: func(value *safeExecutionReceipt) { value.Logs[0].Data[31] = 1 }},
		{name: "log transaction", mutate: func(value *safeExecutionReceipt) { value.Logs[0].TxHash[0] ^= 1 }},
		{name: "log block", mutate: func(value *safeExecutionReceipt) { value.Logs[0].BlockHash[0] ^= 1 }},
		{name: "log number", mutate: func(value *safeExecutionReceipt) { value.Logs[0].BlockNumber++ }},
		{name: "log transaction index", mutate: func(value *safeExecutionReceipt) { value.Logs[0].TxIndex++ }},
		{name: "duplicate index", mutate: func(value *safeExecutionReceipt) {
			value.Logs = append(value.Logs, cloneSafeExecutionReceipt(*value).Logs[0])
		}},
		{name: "duplicate outcome", mutate: func(value *safeExecutionReceipt) {
			other := cloneSafeExecutionReceipt(*value).Logs[0]
			other.Index++
			value.Logs = append(value.Logs, other)
		}},
		{name: "conflicting outcome", mutate: func(value *safeExecutionReceipt) {
			other := cloneSafeExecutionReceipt(*value).Logs[0]
			other.Index++
			other.Topics[0] = fixture.oracleAbi.Events["ExecutionFailure"].ID
			value.Logs = append(value.Logs, other)
		}},
	}
	for _, c := range cases {
		changed := cloneSafeExecutionReceipt(baseline)
		c.mutate(&changed)
		if _, err := fixture.profile.classifyReceipt(transaction, baseline.TransactionHash, changed); err == nil {
			t.Fatalf("forged or incomplete Safe outcome accepted: %s", c.name)
		}
	}
	if _, err := fixture.profile.classifyReceipt(transaction, common.Hash{}, baseline); err == nil {
		t.Fatal("missing expected outer transaction admitted")
	}
	otherDigest := cloneSafeExecutionReceipt(baseline)
	other := cloneSafeExecutionReceipt(baseline).Logs[0]
	other.Index++
	other.Topics[1][0] ^= 1
	otherDigest.Logs = append(otherDigest.Logs, other)
	result, err := fixture.profile.classifyReceipt(transaction, baseline.TransactionHash, otherDigest)
	if err != nil || result.Outcome != "safe-inner-success" || result.Scope != (safeExecutionScope{}) {
		t.Fatal("another Safe digest invalidated the unique matching outcome")
	}
	zeroGas := cloneSafeExecutionTransaction(transaction)
	zeroGas.SafeTxGas.SetInt64(0)
	zeroGasDigest, err := fixture.profile.transactionDigest(zeroGas)
	if err != nil {
		t.Fatal(err)
	}
	impossible := cloneSafeExecutionReceipt(baseline)
	impossible.Logs[0].Topics = []common.Hash{fixture.oracleAbi.Events["ExecutionFailure"].ID, zeroGasDigest.Hash}
	if _, err := fixture.profile.classifyReceipt(zeroGas, baseline.TransactionHash, impossible); err == nil {
		t.Fatal("zero-gas committed inner failure admitted despite mandatory outer revert")
	}
}

// Irrelevant logs are legitimate but remain bounded and internally consistent;
// they cannot hide malformed receipt identity or unbounded decoding work.
func TestSafeExecutionReceiptBoundsKeepUnrelatedEventsHonest(t *testing.T) {
	fixture := newSafeExecutionFixture(t, "1.4.1", "Safe")
	baseline, _, executionError := fixture.execute(fixture.transaction, fixture.signatures(fixture.transaction, "raw", "raw"))
	if executionError != nil || len(baseline.Logs) != 1 {
		t.Fatal("published event baseline failed")
	}
	withUnrelated := func(count, bytesPerLog, topicsPerLog int) safeExecutionReceipt {
		receipt := cloneSafeExecutionReceipt(baseline)
		for i := 0; i < count; i++ {
			other := *baseline.Logs[0]
			other.Address = fixture.owners[2]
			other.Index += uint(i + 1)
			other.Data, other.Topics = make([]byte, bytesPerLog), make([]common.Hash, topicsPerLog)
			receipt.Logs = append(receipt.Logs, &other)
		}
		return receipt
	}
	for _, c := range []struct {
		name         string
		receipt      safeExecutionReceipt
		expectAccept bool
	}{
		{name: "unrelated", receipt: withUnrelated(1, 1, 0), expectAccept: true},
		{name: "count boundary", receipt: withUnrelated(maximumSafeExecutionLogs-1, 0, 4), expectAccept: true},
		{name: "count overflow", receipt: withUnrelated(maximumSafeExecutionLogs, 0, 0)},
		{name: "data boundary", receipt: withUnrelated(1, maximumSafeExecutionBytes, 4), expectAccept: true},
		{name: "data overflow", receipt: withUnrelated(1, maximumSafeExecutionBytes+1, 0)},
		{name: "topic overflow", receipt: withUnrelated(1, 0, 5)},
		{name: "total overflow", receipt: withUnrelated(4, maximumSafeExecutionBytes, 0)},
	} {
		_, err := fixture.profile.classifyReceipt(fixture.transaction, baseline.TransactionHash, c.receipt)
		if c.expectAccept != (err == nil) {
			t.Fatalf("Safe receipt bound differs for %s: %v", c.name, err)
		}
	}
	unrelated := withUnrelated(1, 0, 0)
	unrelated.Logs[1].TxHash[0] ^= 1
	if _, err := fixture.profile.classifyReceipt(fixture.transaction, baseline.TransactionHash, unrelated); err == nil {
		t.Fatal("unrelated log bypassed outer receipt identity")
	}
}
