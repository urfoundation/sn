// The explicitly selected single-owner profile runs published Safe code with one
// owner at threshold one. Every key, account, signature and approval is synthetic.
package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm/runtime"
	"github.com/ethereum/go-ethereum/crypto"
)

// The light custody fixture selects the single-owner request schema explicitly.
func newBootstrapSuccessorExecutionSingleOwnerFixture(t *testing.T) *bootstrapSuccessorExecutionFixture {
	t.Helper()
	return newBootstrapSuccessorExecutionOwnerFixture(t, "17", 42, true)
}

// Real setup installs one owner at threshold one; only the scope selects it.
func newSafeCurrentSingleOwnerProofFixture(t *testing.T, version, variant string) *safeCurrentProofFixture {
	t.Helper()
	f := safeCurrentProofFixtureFromOracle(t, newSafeExecutionOwnerFixture(t, version, variant, true), version, variant)
	f.scope.SingleOwner = true
	return f
}

// Solidity mapping slots are derived here without the production helpers.
func safeSingleOwnerTestSlot(key []byte, slot int64) common.Hash {
	return crypto.Keccak256Hash(common.LeftPadBytes(key, 32), common.LeftPadBytes(big.NewInt(slot).Bytes(), 32))
}

// Distinct sorted addresses keep census mutations independent of key order.
func safeSingleOwnerTestSorted(addresses ...common.Address) []common.Address {
	sorted := slices.Clone(addresses)
	slices.SortFunc(sorted, func(a, b common.Address) int { return bytes.Compare(a[:], b[:]) })
	return sorted
}

// All four published releases/variants prove a complete seven-word one-owner
// account; nonce zero proves the absence of slot five without widening it.
func TestSafeCurrentStorageProvesExactSingleOwnerSafe(t *testing.T) {
	sentinel := common.HexToAddress("0x1")
	for _, profile := range safeExecutionTestProfiles {
		f := newSafeCurrentSingleOwnerProofFixture(t, profile.version, profile.variant)
		owner := f.scope.Owners[0]
		getters := map[string]any{}
		for _, name := range []string{"getOwners", "getThreshold"} {
			input, err := f.oracle.oracleAbi.Pack(name)
			if err != nil {
				t.Fatal(err)
			}
			output, _, err := runtime.Call(f.scope.Safe, input, &f.oracle.vm)
			if err != nil {
				t.Fatal(err)
			}
			values, err := f.oracle.oracleAbi.Unpack(name, output)
			if err != nil || len(values) != 1 {
				t.Fatal("published one-owner Safe getter failed", name, err)
			}
			getters[name] = values[0]
		}
		owners, ownersOk := getters["getOwners"].([]common.Address)
		threshold, thresholdOk := getters["getThreshold"].(*big.Int)
		if !ownersOk || !thresholdOk || !slices.Equal(owners, []common.Address{owner}) || threshold.Int64() != 1 {
			t.Fatal("published setup did not install one owner at threshold one", profile)
		}
		expected := map[common.Hash]common.Hash{
			{}:                                      common.BytesToHash(f.scope.Singleton[:]),
			common.BigToHash(big.NewInt(3)):         common.BigToHash(big.NewInt(1)),
			common.BigToHash(big.NewInt(4)):         common.BigToHash(big.NewInt(1)),
			common.BigToHash(big.NewInt(5)):         common.BigToHash(big.NewInt(17)),
			safeSingleOwnerTestSlot(sentinel[:], 1): common.BytesToHash(sentinel[:]),
			safeSingleOwnerTestSlot(sentinel[:], 2): common.BytesToHash(owner[:]),
			safeSingleOwnerTestSlot(owner[:], 2):    common.BytesToHash(sentinel[:]),
		}
		witness := safeCurrentTestWitness(t, f.entries)
		result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness)
		if err != nil || result == nil || len(result.Words) != len(expected) || len(f.scope.keys()) != 12 || !result.CompleteFinalizedStorage ||
			result.DeploymentHistoryVerified || result.CompletePendingVerified || result.SendAuthorized {
			t.Fatal("clean published one-owner Safe proof failed or overclaimed authority", profile, err)
		}
		for _, word := range result.Words {
			if expected[word.Slot] != word.Value {
				t.Fatal("one-owner Safe proof reported a different census word", profile, word.Slot)
			}
		}
		f.scope.Nonce = "0"
		slot := common.BigToHash(big.NewInt(5))
		delete(f.entries, string(safeCurrentTestNativeKey("AccountStorages", f.scope.Safe, &slot)))
		witness = safeCurrentTestWitness(t, f.entries)
		if result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness); err != nil || len(result.Words) != 6 {
			t.Fatal("zero one-owner Safe nonce did not prove absent storage", profile, err)
		}
	}
}

// Every changed census is genuinely committed, so refusal cannot be a proof
// accident. At threshold one an orphan owner alone authorizes the actual Safe.
func TestSafeCurrentStorageSingleOwnerRejectsChangedCensus(t *testing.T) {
	f := newSafeCurrentSingleOwnerProofFixture(t, "1.4.1", "Safe")
	sentinel, owner := common.HexToAddress("0x1"), f.scope.Owners[0]
	stranger, err := crypto.ToECDSA(crypto.Keccak256([]byte("synthetic orphan single Safe signer")))
	if err != nil {
		t.Fatal(err)
	}
	strangerAddress := crypto.PubkeyToAddress(stranger.PublicKey)
	digest := f.oracle.oracleDigest(f.oracle.transaction)
	strangerSignature, err := crypto.Sign(digest[:], stranger)
	if err != nil {
		t.Fatal(err)
	}
	strangerSignature[64] += 27
	if err := f.oracle.oracleCheck(f.oracle.transaction, f.oracle.signatures(f.oracle.transaction, "raw"), owner); err != nil {
		t.Fatal("published one-owner Safe refused its single EIP-712 signature", err)
	}
	if err := f.oracle.oracleCheck(f.oracle.transaction, strangerSignature, strangerAddress); err == nil {
		t.Fatal("published one-owner Safe accepted a non-owner signature")
	}
	orphan := safeSingleOwnerTestSlot(strangerAddress[:], 2)
	f.oracle.state.SetState(f.scope.Safe, orphan, common.BigToHash(big.NewInt(1)))
	if err := f.oracle.oracleCheck(f.oracle.transaction, strangerSignature, strangerAddress); err != nil {
		t.Fatal("published one-owner Safe did not let an orphan owner sign alone", err)
	}
	f.oracle.state.SetState(f.scope.Safe, orphan, common.Hash{})
	number := func(value int64) common.Hash { return common.BigToHash(big.NewInt(value)) }
	word := func(address common.Address) common.Hash { return common.BytesToHash(address[:]) }
	module := common.BytesToAddress(crypto.Keccak256([]byte("synthetic single Safe module")))
	operation := crypto.Keccak256Hash([]byte("synthetic preapproved single Safe operation"))
	padded := word(owner)
	padded[0] = 1
	ownerLink, sentinelLink := safeSingleOwnerTestSlot(owner[:], 2), safeSingleOwnerTestSlot(sentinel[:], 2)
	for _, change := range []struct {
		name  string
		words map[common.Hash]common.Hash
	}{
		{name: "owner count three", words: map[common.Hash]common.Hash{number(3): number(3)}},
		{name: "owner count two", words: map[common.Hash]common.Hash{number(3): number(2)}},
		{name: "owner count absent", words: map[common.Hash]common.Hash{number(3): {}}},
		{name: "threshold two", words: map[common.Hash]common.Hash{number(4): number(2)}},
		{name: "threshold absent", words: map[common.Hash]common.Hash{number(4): {}}},
		{name: "orphan owner", words: map[common.Hash]common.Hash{orphan: number(1)}},
		{name: "second owner link", words: map[common.Hash]common.Hash{ownerLink: word(strangerAddress), orphan: word(sentinel)}},
		{name: "second counted owner", words: map[common.Hash]common.Hash{ownerLink: word(strangerAddress), orphan: word(sentinel), number(3): number(2)}},
		{name: "owner cycle", words: map[common.Hash]common.Hash{ownerLink: word(owner)}},
		{name: "missing owner link", words: map[common.Hash]common.Hash{ownerLink: {}}},
		{name: "owner padding", words: map[common.Hash]common.Hash{sentinelLink: padded}},
		{name: "orphan module", words: map[common.Hash]common.Hash{safeSingleOwnerTestSlot(module[:], 1): number(1)}},
		{name: "enabled module", words: map[common.Hash]common.Hash{safeSingleOwnerTestSlot(sentinel[:], 1): word(module), safeSingleOwnerTestSlot(module[:], 1): word(sentinel)}},
		{name: "guard", words: map[common.Hash]common.Hash{common.HexToHash("0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8"): word(module)}},
		{name: "module guard", words: map[common.Hash]common.Hash{common.HexToHash("0xb104e0b93118902c651344349b610029d694cfdec91c589c91ebafbcd0289947"): word(module)}},
		{name: "fallback handler", words: map[common.Hash]common.Hash{common.HexToHash("0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5"): word(module)}},
		{name: "approved hash", words: map[common.Hash]common.Hash{crypto.Keccak256Hash(operation[:], safeSingleOwnerTestSlot(owner[:], 8).Bytes()): number(1)}},
		{name: "signed message", words: map[common.Hash]common.Hash{safeSingleOwnerTestSlot(operation[:], 7): number(1)}},
		{name: "deprecated domain", words: map[common.Hash]common.Hash{number(6): number(1)}},
		{name: "missing singleton", words: map[common.Hash]common.Hash{{}: {}}},
	} {
		entries := maps.Clone(f.entries)
		for slot, value := range change.words {
			key := string(safeCurrentTestNativeKey("AccountStorages", f.scope.Safe, &slot))
			if value == (common.Hash{}) {
				delete(entries, key)
			} else {
				entries[key] = slices.Clone(value[:])
			}
		}
		witness := safeCurrentTestWitness(t, entries)
		if result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness); err == nil || result != nil {
			t.Fatal("complete one-owner Safe proof accepted changed authority", change.name)
		}
	}
}

// Neither profile is inferred from storage. A genuine one-owner Safe fails each
// three-owner scope and a genuine three-owner Safe fails every one-owner scope.
func TestSafeCurrentStorageOwnerProfileIsExplicit(t *testing.T) {
	single := newSafeCurrentSingleOwnerProofFixture(t, "1.4.1", "Safe")
	triple := newSafeCurrentProofFixture(t, "1.4.1", "Safe")
	singleWitness, tripleWitness := safeCurrentTestWitness(t, single.entries), safeCurrentTestWitness(t, triple.entries)
	check := func(scope safeCurrentStorageScope, witness safeCurrentStorageWitness) error {
		result, err := verifySafeCurrentStorage(t.Context(), scope, witness.At, 703, witness)
		if err == nil && result == nil {
			return errors.New("Safe proof returned no observation")
		}
		return err
	}
	if err := errors.Join(check(single.scope, singleWitness), check(triple.scope, tripleWitness)); err != nil {
		t.Fatal("explicitly selected owner profiles refused their genuine Safes", err)
	}
	unselected := single.scope
	unselected.SingleOwner = false
	widened := unselected
	widened.Owners = safeSingleOwnerTestSorted(single.scope.Owners[0], triple.scope.Owners[0], triple.scope.Owners[1])
	listed := triple.scope
	listed.SingleOwner = true
	if unselected.validate() == nil || listed.validate() == nil {
		t.Fatal("owner count selected a profile that the scope did not name")
	}
	if check(unselected, singleWitness) == nil || check(widened, singleWitness) == nil || check(listed, tripleWitness) == nil {
		t.Fatal("three-owner scope accepted a genuine one-owner Safe")
	}
	for _, owner := range triple.scope.Owners {
		narrowed := triple.scope
		narrowed.SingleOwner, narrowed.Owners = true, []common.Address{owner}
		if check(narrowed, tripleWitness) == nil {
			t.Fatal("one-owner scope accepted a genuine three-owner Safe", owner)
		}
	}
}

// The request schema alone selects one owner and one 65-byte signature. Owner
// count never selects a profile, and the original three-owner grammar is unchanged.
func TestBootstrapSuccessorSingleOwnerExecutionRequestGrammar(t *testing.T) {
	f := newBootstrapSuccessorExecutionSingleOwnerFixture(t)
	request := f.approval.Plan.Request
	if request.Schema != bootstrapSuccessorExecutionSingleOwnerRequestSchema || len(request.Owners) != 1 || request.validate() != nil {
		t.Fatal("single-owner request did not validate its explicit profile")
	}
	others := []common.Address{common.BytesToAddress(crypto.Keccak256([]byte("synthetic request owner a"))), common.BytesToAddress(crypto.Keccak256([]byte("synthetic request owner b")))}
	for name, change := range map[string]func(*bootstrapSuccessorExecutionRequest){
		"no owner": func(r *bootstrapSuccessorExecutionRequest) { r.Owners = nil },
		"two owners": func(r *bootstrapSuccessorExecutionRequest) {
			r.Owners = safeSingleOwnerTestSorted(r.Owners[0], others[0])
		},
		"three owners": func(r *bootstrapSuccessorExecutionRequest) {
			r.Owners = safeSingleOwnerTestSorted(r.Owners[0], others[0], others[1])
		},
		"zero owner": func(r *bootstrapSuccessorExecutionRequest) { r.Owners = []common.Address{{}} },
		"sentinel owner": func(r *bootstrapSuccessorExecutionRequest) {
			r.Owners = []common.Address{common.BytesToAddress([]byte{1})}
		},
		"three-owner schema": func(r *bootstrapSuccessorExecutionRequest) { r.Schema = bootstrapSuccessorExecutionRequestSchema },
		"unknown schema": func(r *bootstrapSuccessorExecutionRequest) {
			r.Schema = "urnetwork-mainnet-successor-execution-request-v2"
		},
	} {
		changed := bootstrapSuccessorExecutionTestCopy(t, request)
		change(&changed)
		if err := changed.validate(); err == nil {
			t.Fatal("single-owner request grammar admitted a changed profile", name)
		}
	}
	triple := newBootstrapSuccessorExecutionFixture(t).approval.Plan.Request
	if err := triple.validate(); err != nil || len(triple.Owners) != 3 {
		t.Fatal("three-owner request no longer validates", err)
	}
	narrowed := bootstrapSuccessorExecutionTestCopy(t, triple)
	narrowed.Owners = narrowed.Owners[:1]
	listed := bootstrapSuccessorExecutionTestCopy(t, triple)
	listed.Schema = bootstrapSuccessorExecutionSingleOwnerRequestSchema
	if narrowed.validate() == nil || listed.validate() == nil {
		t.Fatal("owner count selected a request profile that its schema did not name")
	}
}

// One EIP-712 owner signature is accepted by the published Safe and bound by the
// plan. Two signatures, other kinds, other keys and changed lengths all refuse.
func TestBootstrapSuccessorSingleOwnerSignatureBinding(t *testing.T) {
	f := newBootstrapSuccessorExecutionSingleOwnerFixture(t)
	plan := f.approval.Plan
	signatures := common.FromHex(plan.SafeSignatures)
	if len(signatures) != 65 || signatures[64] != 27 && signatures[64] != 28 {
		t.Fatal("single-owner plan does not retain one 65-byte EIP-712 signature")
	}
	if err := f.oracle.oracleCheck(plan.transaction(), signatures, plan.Review.Relayer.Sender); err != nil {
		t.Fatal("published one-owner Safe refused the retained signature", err)
	}
	inspection, err := f.profile.inspectSignatures(plan.transaction(), signatures, 1)
	if err != nil || len(inspection.Prefix) != 1 || inspection.Prefix[0].Kind != "eip712-ecdsa" || inspection.Prefix[0].Signer != plan.Request.Owners[0] {
		t.Fatal("single signature did not recover the selected owner", err)
	}
	message, err := plan.signingBytes(f.profile)
	if err != nil || !bytes.Contains(message, []byte(`"schema":"`+bootstrapSuccessorExecutionSingleOwnerRequestSchema+`"`)) || f.approval.validate(plan, f.profile) != nil {
		t.Fatal("independent approval does not cover the selected single-owner request", err)
	}
	outer, err := plan.outer(f.profile)
	if err != nil {
		t.Fatal(err)
	}
	arguments, err := f.oracle.oracleAbi.Methods["execTransaction"].Inputs.Unpack(common.FromHex(outer.Data)[4:])
	if err != nil || len(arguments) != 10 || !bytes.Equal(arguments[9].([]byte), signatures) {
		t.Fatal("outer calldata does not carry exactly the single signature", err)
	}
	stranger, err := crypto.ToECDSA(crypto.Keccak256([]byte("synthetic single-owner stranger")))
	if err != nil {
		t.Fatal(err)
	}
	digest := plan.Review.Transaction.Digest
	strangerSignature, err := crypto.Sign(digest[:], stranger)
	if err != nil {
		t.Fatal(err)
	}
	strangerSignature[64] += 27
	later := plan.transaction()
	later.Nonce = big.NewInt(18)
	approvedHash := make([]byte, 65)
	copy(approvedHash[12:32], plan.Request.Owners[0][:])
	approvedHash[64] = 1
	recovery := slices.Clone(signatures)
	recovery[64] = 29
	for name, raw := range map[string][]byte{
		"empty":           {},
		"truncated":       signatures[:64],
		"trailing byte":   append(slices.Clone(signatures), 0),
		"two signatures":  append(slices.Clone(signatures), strangerSignature...),
		"eth_sign":        f.oracle.signatures(plan.transaction(), "eth-sign"),
		"contract":        append(make([]byte, 64), 0),
		"approved hash":   approvedHash,
		"recovery id":     recovery,
		"stranger":        strangerSignature,
		"later operation": f.oracle.signatures(later, "raw"),
	} {
		changed := bootstrapSuccessorExecutionTestCopy(t, plan)
		changed.SafeSignatures, changed.Request.SafeSignatures.Sha256 = "0x"+hex.EncodeToString(raw), safeReleaseHash(raw)
		if _, err := changed.outer(f.profile); err == nil {
			t.Fatal("single-owner outer envelope admitted changed signature bytes", name)
		}
		if err := changed.validate(f.profile); err == nil {
			t.Fatal("single-owner plan admitted changed signature bytes", name)
		}
	}
	unselected := bootstrapSuccessorExecutionTestCopy(t, plan)
	unselected.Request.Schema = bootstrapSuccessorExecutionRequestSchema
	if _, err := unselected.outer(f.profile); err == nil || unselected.validate(f.profile) == nil {
		t.Fatal("three-owner schema admitted one owner signature")
	}
	request := plan.Request
	request.SafeSignatures = bootstrapSuccessorExecutionTestRaw(t, "synthetic-two-single-owner-signatures.bin", append(slices.Clone(signatures), strangerSignature...))
	if _, err := buildBootstrapSuccessorExecution(t.Context(), plan.Review, request, plan.RequestReference, f.profile); err == nil {
		t.Fatal("single-owner builder read more than one pinned signature")
	}
	rebuilt, err := buildBootstrapSuccessorExecution(t.Context(), plan.Review, plan.Request, plan.RequestReference, f.profile)
	if err != nil || rebuilt.hash() != plan.hash() {
		t.Fatal("single-owner builder changed the approved plan", err)
	}
}

// Admission requires the exact owner at threshold one; the unchanged custody
// machine then sends the exact bytes once and reconciles inner success.
func TestBootstrapSuccessorSingleOwnerAdmissionAndExactSend(t *testing.T) {
	f := newBootstrapSuccessorExecutionSingleOwnerFixture(t)
	plan := f.approval.Plan
	if err := plan.admit(f.observation); err != nil {
		t.Fatal("single-owner observation refused", err)
	}
	other := common.BytesToAddress(crypto.Keccak256([]byte("synthetic other single owner")))
	for name, change := range map[string]func(*bootstrapSuccessorExecutionObservation){
		"threshold two":  func(o *bootstrapSuccessorExecutionObservation) { o.Threshold = 2 },
		"threshold zero": func(o *bootstrapSuccessorExecutionObservation) { o.Threshold = 0 },
		"other owner":    func(o *bootstrapSuccessorExecutionObservation) { o.Owners = []common.Address{other} },
		"added owner": func(o *bootstrapSuccessorExecutionObservation) {
			o.Owners = safeSingleOwnerTestSorted(o.Owners[0], other)
		},
		"no owner":         func(o *bootstrapSuccessorExecutionObservation) { o.Owners = nil },
		"module":           func(o *bootstrapSuccessorExecutionObservation) { o.Modules = []common.Address{other} },
		"guard":            func(o *bootstrapSuccessorExecutionObservation) { o.Guard = other },
		"module guard":     func(o *bootstrapSuccessorExecutionObservation) { o.ModuleGuard = other },
		"fallback handler": func(o *bootstrapSuccessorExecutionObservation) { o.FallbackHandler = other },
	} {
		observation := bootstrapSuccessorExecutionTestCopy(t, f.observation)
		change(&observation)
		if err := plan.admit(observation); err == nil {
			t.Fatal("single-owner admission accepted changed Safe authority", name)
		}
	}
	triple := newBootstrapSuccessorExecutionFixture(t)
	for _, owners := range [][]common.Address{triple.observation.Owners, triple.observation.Owners[:1]} {
		observation := bootstrapSuccessorExecutionTestCopy(t, triple.observation)
		observation.Owners, observation.Threshold = owners, 1
		if err := triple.approval.Plan.admit(observation); err == nil {
			t.Fatal("three-owner plan admitted a threshold-one Safe", len(owners))
		}
	}
	owner := f.open(true, nil)
	result, err := advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
	if err != nil || !result.SubmissionAttempted || len(f.writes) != 1 || !bytes.Equal(f.writes[0], common.FromHex(plan.SignedRelayer)) || owner.last.CumulativeAttempts != 9 {
		t.Fatal("single-owner execution did not send its exact bytes once", err)
	}
	f.resolution = bootstrapSuccessorExecutionReconciliation{Status: "included", Receipt: f.receipt()}
	result, err = advanceBootstrapSuccessorExecution(t.Context(), owner, f, true)
	if err != nil || !result.InstallationComplete || result.ActivationReady || len(f.writes) != 1 || owner.last.Phase != "installed" {
		t.Fatal("single-owner canonical outcome resent or lost completion", err)
	}
}

// Published one-owner getters and storage pass only under the single-owner plan;
// each fault changes one authority fact while all other reads stay genuine.
func TestBootstrapSuccessorCanonicalSingleOwnerSafeAuthority(t *testing.T) {
	adapter, model, chain := bootstrapSuccessorCanonicalSafeFixtureFor(t, newBootstrapSuccessorExecutionSingleOwnerFixture(t))
	plan := model.approval.Plan
	state, err := adapter.safeState(t.Context(), plan, "pending")
	if err != nil || state.Threshold != 1 || len(state.Owners) != 1 || !slices.Equal(state.Owners, plan.Request.Owners) || state.SafeNonce != "17" || len(state.Modules) != 0 {
		t.Fatal("published one-owner Safe authority refused", state, err)
	}
	others := []common.Address{common.BytesToAddress(crypto.Keccak256([]byte("synthetic canonical owner a"))), common.BytesToAddress(crypto.Keccak256([]byte("synthetic canonical owner b")))}
	three := safeSingleOwnerTestSorted(plan.Request.Owners[0], others[0], others[1])
	for _, owners := range [][]common.Address{plan.Request.Owners, three} {
		unselected := bootstrapSuccessorExecutionTestCopy(t, plan)
		unselected.Request.Schema, unselected.Request.Owners = bootstrapSuccessorExecutionRequestSchema, owners
		if _, err := adapter.safeState(t.Context(), unselected, "pending"); err == nil {
			t.Fatal("three-owner request admitted a genuine one-owner Safe", len(owners))
		}
	}
	tripleAdapter, tripleModel, _ := bootstrapSuccessorCanonicalSafeFixture(t)
	narrowed := bootstrapSuccessorExecutionTestCopy(t, tripleModel.approval.Plan)
	narrowed.Request.Schema, narrowed.Request.Owners = bootstrapSuccessorExecutionSingleOwnerRequestSchema, narrowed.Request.Owners[:1]
	if _, err := tripleAdapter.safeState(t.Context(), narrowed, "pending"); err == nil {
		t.Fatal("single-owner request admitted a genuine three-owner Safe")
	}
	type fault struct {
		name   string
		method string
		change func([]any, any) any
	}
	getter := func(name, method string, output func() string) fault {
		selector := "0x" + common.Bytes2Hex(model.oracle.oracleAbi.Methods[method].ID)
		return fault{name: name, method: "eth_call", change: func(p []any, result any) any {
			if strings.HasPrefix(p[0].(map[string]any)["data"].(string), selector) {
				return output()
			}
			return result
		}}
	}
	word := func(name, slot string, value common.Hash) fault {
		return fault{name: name, method: "eth_getStorageAt", change: func(p []any, result any) any {
			if p[1] == slot {
				return value.Hex()
			}
			return result
		}}
	}
	ownersOutput := func(owners []common.Address) func() string {
		return func() string {
			encoded, err := model.oracle.oracleAbi.Methods["getOwners"].Outputs.Pack(owners)
			if err != nil {
				t.Error(err)
			}
			return "0x" + hex.EncodeToString(encoded)
		}
	}
	for _, c := range []fault{
		getter("three owners", "getOwners", ownersOutput(three)),
		getter("other owner", "getOwners", ownersOutput(others[:1])),
		getter("threshold two", "getThreshold", func() string { return common.BigToHash(big.NewInt(2)).Hex() }),
		getter("module census", "getModulesPaginated", func() string { return "0x" + common.Bytes2Hex(make([]byte, 128)) }),
		word("owner count storage", common.BigToHash(big.NewInt(3)).Hex(), common.BigToHash(big.NewInt(3))),
		word("threshold storage", common.BigToHash(big.NewInt(4)).Hex(), common.BigToHash(big.NewInt(2))),
		word("guard", "0x4a204f620c8c5ccdca3fd54d003badd85ba500436a431f0cbda4f558c93c34c8", common.Hash{1}),
		word("module guard", "0xb104e0b93118902c651344349b610029d694cfdec91c589c91ebafbcd0289947", common.Hash{1}),
		word("fallback handler", "0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5", common.Hash{1}),
	} {
		chain.stateLock.Lock()
		chain.override = func(method string, p []any, result any) any {
			if method == c.method {
				return c.change(p, result)
			}
			return result
		}
		chain.stateLock.Unlock()
		if _, err := adapter.safeState(t.Context(), plan, "pending"); err == nil {
			t.Fatal("canonical one-owner Safe authority admitted", c.name)
		}
	}
	chain.stateLock.Lock()
	chain.override = nil
	chain.stateLock.Unlock()
	f := safeCurrentProofFixtureFromOracle(t, model.oracle, plan.Review.Request.Version, plan.Review.Request.Variant)
	f.scope.SingleOwner = true
	witness := safeCurrentTestWitness(t, f.entries)
	finalized, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := adapter.readSafeCurrentPending(t.Context(), f.scope, finalized); err != nil || result == nil || !result.ScopedWordsMatched || result.CompletePendingVerified || result.SendAuthorized {
		t.Fatal("one-owner pending scope failed or overclaimed complete authority", err)
	}
	for _, change := range []struct {
		slot  common.Hash
		value common.Hash
	}{{slot: common.BigToHash(big.NewInt(3)), value: common.BigToHash(big.NewInt(2))}, {slot: common.BigToHash(big.NewInt(4)), value: common.BigToHash(big.NewInt(2))},
		{slot: common.HexToHash("0x6c9a6c4a39284e37ed1cf53d337577d14212a4870fb976a4366c693b939918d5"), value: common.Hash{31: 1}}} {
		chain.stateLock.Lock()
		previous := chain.state.GetState(f.scope.Safe, change.slot)
		chain.state.SetState(f.scope.Safe, change.slot, change.value)
		chain.stateLock.Unlock()
		if result, err := adapter.readSafeCurrentPending(t.Context(), f.scope, finalized); err == nil || result != nil {
			t.Fatal("one-owner pending recheck accepted changed authority", change.slot)
		}
		chain.stateLock.Lock()
		chain.state.SetState(f.scope.Safe, change.slot, previous)
		chain.stateLock.Unlock()
	}
}

// The signed execution request selects the census text. A correctly signed
// proposal naming the other profile's text refuses before any scope is derived.
func TestBootstrapSuccessorSafeCurrentPolicyFollowsOwnerProfile(t *testing.T) {
	resign := func(t *testing.T, proposal bootstrapSuccessorSafeCurrentPolicyApproval, key ed25519.PrivateKey, policy string) bootstrapSuccessorSafeCurrentPolicyApproval {
		t.Helper()
		proposal.Authorization.Policy = policy
		message, err := proposal.Authorization.signingBytes()
		if err != nil {
			t.Fatal(err)
		}
		proposal.Signature = hex.EncodeToString(ed25519.Sign(key, message))
		return proposal
	}
	single := newBootstrapSuccessorExecutionSingleOwnerFixture(t)
	base := bootstrapSuccessorCanonicalTestApproval(t, single.approval.Plan, single.key, bootstrapSuccessorCanonicalTestRuntime())
	proposal := bootstrapSuccessorSafeCurrentTestProposal(t, single, base)
	scope, err := proposal.validate(t.Context(), single.approval.Plan, base, bootstrapSuccessorRuntimeHistory{})
	if err != nil || proposal.Authorization.Policy != bootstrapSuccessorSafeCurrentSingleOwnerPolicy || !scope.SingleOwner || !slices.Equal(scope.Owners, single.approval.Plan.Request.Owners) {
		t.Fatal("single-owner current policy did not derive its explicit scope", err)
	}
	wrong := resign(t, proposal, single.key, bootstrapSuccessorSafeCurrentPolicy)
	if _, err := wrong.validate(t.Context(), single.approval.Plan, base, bootstrapSuccessorRuntimeHistory{}); err == nil || !strings.Contains(err.Error(), "owner profile") {
		t.Fatal("three-owner census text covered a single-owner execution", err)
	}
	unknown := proposal
	unknown.Authorization.Policy = strings.Replace(bootstrapSuccessorSafeCurrentSingleOwnerPolicy, "one-of-one", "one-of-three", 1)
	if _, err := unknown.Authorization.signingBytes(); err == nil {
		t.Fatal("unreviewed census text produced signing bytes")
	}
	triple := newBootstrapSuccessorExecutionFixture(t)
	tripleBase := bootstrapSuccessorCanonicalTestApproval(t, triple.approval.Plan, triple.key, bootstrapSuccessorCanonicalTestRuntime())
	tripleProposal := bootstrapSuccessorSafeCurrentTestProposal(t, triple, tripleBase)
	if tripleScope, err := tripleProposal.validate(t.Context(), triple.approval.Plan, tripleBase, bootstrapSuccessorRuntimeHistory{}); err != nil || tripleScope.SingleOwner || len(tripleScope.Owners) != 3 {
		t.Fatal("three-owner current policy changed its scope", err)
	}
	widened := resign(t, tripleProposal, triple.key, bootstrapSuccessorSafeCurrentSingleOwnerPolicy)
	if _, err := widened.validate(t.Context(), triple.approval.Plan, tripleBase, bootstrapSuccessorRuntimeHistory{}); err == nil || !strings.Contains(err.Error(), "owner profile") {
		t.Fatal("single-owner census text covered a three-owner execution", err)
	}
	owner := single.open(true, nil)
	if err := owner.retainCanonicalAuthority(t.Context(), base); err != nil {
		t.Fatal(err)
	}
	mismatched := bootstrapSuccessorSafeCurrentTestSign(t, bootstrapSuccessorSafeCurrentRevisionAuthorization{Schema: bootstrapSuccessorSafeCurrentRevisionSchema, Sequence: 1,
		PreviousHash: rootObjectHash(base), Proposal: wrong, Policy: bootstrapSuccessorSafeCurrentRevisionPolicy}, single.key)
	if err := owner.retainSafeCurrentRevision(t.Context(), mismatched); err == nil || len(owner.safeCurrentHistory.approvals) != 0 {
		t.Fatal("custody imported a revision for the other owner profile", err)
	}
	revision := bootstrapSuccessorSafeCurrentTestPublicAcceptance(t, bootstrapSuccessorSafeCurrentTestNext(t, single, base, owner.runtimeHistory, nil), single.key)
	if err := owner.retainSafeCurrentRevision(t.Context(), revision); err != nil || !revision.Authorization.permitsPublicSubmission() {
		t.Fatal("single-owner public acceptance was not retained", err)
	}
	adapter := &bootstrapSuccessorCanonicalChain{owner: owner, approval: base, currentRevisionHash: rootObjectHash(revision)}
	if err := adapter.selectCurrentPolicy(t.Context(), bootstrapSuccessorSafeCurrentPublicRoute); err != nil || adapter.currentPolicy == nil ||
		!adapter.currentPolicy.scope.SingleOwner || !slices.Equal(adapter.currentPolicy.scope.Owners, single.approval.Plan.Request.Owners) {
		t.Fatal("public current-policy capability did not select the single-owner scope", err)
	}
}

// Values recorded from the unchanged three-owner implementation. Proofs, key
// census, getters, signatures, outer calldata, policy text and request grammar
// keep their exact bytes and diagnostics.
func TestSafeOwnerProfileKeepsThreeOwnerBytes(t *testing.T) {
	recorded := map[string][2]string{
		"1.4.1/Safe":   {"sha256:a22e32d494c6775e42ad33b414a5ace08f7e4422b855f48a706b75b43095bef1", "sha256:d0778941cb43c4f5b1b690515e9d395015be3abcd6800fd769895ae8cc71e020"},
		"1.4.1/SafeL2": {"sha256:5785fe4fa01447565130e7889daa92181e7044d78e78f196ee36c797c42a127f", "sha256:9a0c04217fc45ef5db7d23fb4c5ef05e398134d7fecbe469642c22d3e33f879f"},
		"1.5.0/Safe":   {"sha256:e69a8d1346a283613cccc36d16f194fad2b8bea10c097116079fd9cbebfd93d9", "sha256:f8c7676d135039564059471e7e9622719e6ad86d3be56ec3674cea61c5fa51da"},
		"1.5.0/SafeL2": {"sha256:296caf344dd53649035ff0b4659e9247e6f85954e997f46c40b99b92a3347722", "sha256:5e65acac62248e453d3aeabc40f92e09e595f8b1f9e516fd770dc2dbc4f5ed97"},
	}
	for _, profile := range safeExecutionTestProfiles {
		f := newSafeCurrentProofFixture(t, profile.version, profile.variant)
		witness := safeCurrentTestWitness(t, f.entries)
		result, err := verifySafeCurrentStorage(t.Context(), f.scope, witness.At, 703, witness)
		expected := recorded[profile.version+"/"+profile.variant]
		if err != nil || f.scope.SingleOwner || rootObjectHash(result) != expected[0] || rootObjectHash(f.scope.keys()) != expected[1] {
			t.Fatal("three-owner Safe proof or key census bytes changed", profile, err)
		}
	}
	adapter, model, _ := bootstrapSuccessorCanonicalSafeFixture(t)
	if state, err := adapter.safeState(t.Context(), model.approval.Plan, "pending"); err != nil || rootObjectHash(state) != "sha256:3c5ac5c45d120a18bceeb161a828d33abcdbb6152494d242187794141a2137b4" {
		t.Fatal("three-owner canonical Safe observation changed", err)
	}
	f := newBootstrapSuccessorExecutionFixture(t)
	plan := f.approval.Plan
	outer, err := plan.outer(f.profile)
	if err != nil || rootObjectHash(outer) != "sha256:cfab81432816743c55335610f1fce349c0d39f63f85d290c02b1e8ddf6c589d5" ||
		plan.TransactionHash.Hex() != "0x59ed2855d4112542b295be069bc533bb84af68879105817447ce74e88b1dcacb" ||
		plan.Review.Transaction.Digest.Hex() != "0x2cffc1246c22ae2c244a2ed1f59da38f91e4b439595fb2a4903788cd5cf2cd18" {
		t.Fatal("three-owner signatures, outer calldata or relayer bytes changed", err)
	}
	raw, err := json.Marshal(plan.Request)
	var fields map[string]json.RawMessage
	if err == nil {
		err = json.Unmarshal(raw, &fields)
	}
	if err != nil || plan.Request.Schema != "urnetwork-mainnet-successor-execution-request-v1" ||
		!slices.Equal(slices.Sorted(maps.Keys(fields)), []string{"owners", "registry_directory", "relayer_transaction", "safe_review_hash", "safe_signatures", "schema", "singleton"}) {
		t.Fatal("three-owner execution request grammar changed", err)
	}
	// The recorded digest is of the exact JSON-encoded original policy text.
	if rootObjectHash(bootstrapSuccessorSafeCurrentPolicy) != "sha256:bee887d30a7536f17292da82f6131912025e08d7ea56b57322d11db125617fc5" {
		t.Fatal("three-owner current policy text changed")
	}
	short := bootstrapSuccessorExecutionTestCopy(t, plan.Request)
	short.Owners = short.Owners[:2]
	if err := short.validate(); err == nil || err.Error() != "successor execution requires exact review, physical registry, three owners and pinned signature files" {
		t.Fatal("three-owner request diagnostic changed", err)
	}
	one := bootstrapSuccessorExecutionTestCopy(t, plan)
	one.SafeSignatures = "0x" + hex.EncodeToString(common.FromHex(plan.SafeSignatures)[:65])
	if _, err := one.outer(f.profile); err == nil || !strings.HasPrefix(err.Error(), "successor execution requires exactly two Safe signatures") {
		t.Fatal("three-owner signature diagnostic changed", err)
	}
}

// Public v2 dispatch executes published one-owner Safe code with one EIP-712
// signature, loses its acknowledgement, then proves a one-owner installation.
func TestBootstrapContractInstallationSingleOwnerReadback(t *testing.T) {
	f := newBootstrapSuccessorCanonicalOwnerFixture(t, nil, true)
	chain, plan := f.original.contracts, f.approval.Plan
	if plan.Request.Schema != bootstrapSuccessorExecutionSingleOwnerRequestSchema || len(plan.Request.Owners) != 1 || len(common.FromHex(plan.SafeSignatures)) != 65 {
		t.Fatal("single-owner fixture did not select its explicit execution profile")
	}
	seal := bootstrapContractInstallationTestProofs(t, f)
	seal(true)
	model := &bootstrapSuccessorExecutionFixture{t: t, approval: f.approval, key: f.key}
	revision := bootstrapSuccessorSafeCurrentTestPublicAcceptance(t, bootstrapSuccessorSafeCurrentTestNext(t, model, f.canonical, bootstrapSuccessorRuntimeHistory{}, nil), f.key)
	if revision.Authorization.Proposal.Authorization.Policy != bootstrapSuccessorSafeCurrentSingleOwnerPolicy {
		t.Fatal("single-owner acceptance does not name its census text")
	}
	reference := bootstrapRootTestWrite(t, filepath.Join(bootstrapSuccessorExecutionTestDirectory(t), "synthetic-single-owner-acceptance.json"), revision)
	args := []string{"contract-successor-execution-resume", "--config", f.original.path, "--run-dir", f.original.config.RunDirectory, "--accept-plan-hash", f.original.preparation.Plan.ContentHash}
	args = append(append(args, f.paths...), f.approvalArgs...)
	args = append(args, "--online", "--canonical-approval", f.canonicalRef.Path, "--canonical-approval-sha256", f.canonicalRef.Sha256,
		"--safe-current-revision", reference.Path, "--safe-current-revision-sha256", reference.Sha256, "--accept-safe-current-policy", rootObjectHash(revision))
	invoke := func(input []string) (int, bootstrapContractInstallation, string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := runBootstrapSuccessorExecutionCommand(f.original.storageContext(t.Context()), input, &stdout, &stderr)
		var result bootstrapContractInstallation
		if stdout.Len() != 0 {
			if err := decodePlanJson(stdout.Bytes(), &result); err != nil {
				t.Fatal(err, stdout.String())
			}
		}
		return code, result, stderr.String()
	}
	chain.stateLock.Lock()
	chain.loseReply = true
	chain.stateLock.Unlock()
	if code, _, diagnostic := invoke(append(slices.Clone(args), "--submit")); code != 1 {
		t.Fatal("expected lost reply", code, diagnostic)
	}
	chain.stateLock.Lock()
	chain.loseReply = false
	sent := len(chain.writes) == 9 && bytes.Equal(chain.writes[8], common.FromHex(plan.SignedRelayer))
	chain.stateLock.Unlock()
	if !sent {
		t.Fatal("exact one-owner anchor was not sent once")
	}
	anchor := seal(false)
	args[0] = "contract-successor-execution-readback"
	code, result, diagnostic := invoke(args)
	if code != 0 {
		t.Fatal("one-owner installation readback", code, diagnostic)
	}
	if !result.InstallationComplete || !result.InitialContracts.CompleteStorage || !result.InitialSafe.CompleteFinalizedStorage || !result.CurrentSafe.CompleteFinalizedStorage ||
		result.Identity.AnchorReceipt.NativeHash.Hex() != anchor.At || result.Identity.SafeCurrentRevisionHash != rootObjectHash(revision) || result.CurrentSafeNonce != "18" ||
		result.CompleteSafeHistory || result.CompleteContractHistory || result.CompletePendingState || result.ActivationReady || result.NetworkEffects {
		t.Fatal("one-owner installation readback scope differs", result)
	}
	sentinel, owner := common.HexToAddress("0x1"), plan.Request.Owners[0]
	expected := map[common.Hash]common.Hash{
		common.BigToHash(big.NewInt(3)): common.BigToHash(big.NewInt(1)), common.BigToHash(big.NewInt(4)): common.BigToHash(big.NewInt(1)),
		common.BigToHash(big.NewInt(5)): common.BigToHash(big.NewInt(18)), safeSingleOwnerTestSlot(sentinel[:], 2): common.BytesToHash(owner[:]),
		safeSingleOwnerTestSlot(owner[:], 2): common.BytesToHash(sentinel[:]), safeSingleOwnerTestSlot(sentinel[:], 1): common.BytesToHash(sentinel[:]),
	}
	for _, observation := range []*safeCurrentStorageObservation{result.InitialSafe, result.CurrentSafe} {
		if len(observation.Words) != 7 {
			t.Fatal("one-owner installation proof has a different census", len(observation.Words))
		}
		for _, word := range observation.Words {
			if value, checked := expected[word.Slot]; checked && value != word.Value {
				t.Fatal("one-owner installation proof changed an authority word", word.Slot)
			}
		}
	}
	chain.stateLock.Lock()
	writes := len(chain.writes)
	chain.stateLock.Unlock()
	if writes != 9 {
		t.Fatal("one-owner readback resent the anchor")
	}
}
