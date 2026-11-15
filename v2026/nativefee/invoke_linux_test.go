//go:build linux

// Exercises owned verifier output, process joins and immutable proof custody.
package nativefee

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/blake2b"
)

type protocolFixture struct {
	Statement Statement `json:"statement"`
	Mode      string    `json:"mode"`
}

// The pinned test ELF exercises process ownership, not native Wasm semantics.
// Production Invoke has no runner replacement or public result constructor.
func init() {
	if len(os.Args) != 0 && os.Args[0] == "urnetwork-native-fee-test-orphan" {
		for {
			syscall.Pause()
		}
	}
	if len(os.Args) < 2 || os.Args[0] != "urnetwork-native-fee-verifier" {
		return
	}
	flags := flag.NewFlagSet("owned-protocol-fixture", flag.ContinueOnError)
	path := flags.String("request", "", "")
	digest := flags.String("request-sha256", "", "")
	flags.String("transaction", "", "")
	flags.String("policy-json", "", "")
	flags.String("budget", "", "")
	if os.Args[1] != "verify-native-fee-outcome" || flags.Parse(os.Args[2:]) != nil {
		os.Exit(2)
	}
	raw, err := os.ReadFile(*path)
	if err != nil {
		os.Exit(2)
	}
	var fixture protocolFixture
	if json.Unmarshal(raw, &fixture) != nil {
		os.Exit(2)
	}
	fixture.Statement.RequestSha256 = *digest
	fixture.Statement.Originals[0].Reference = Reference{Path: *path, Sha256: *digest}
	switch fixture.Mode {
	case "failed":
		json.NewEncoder(os.Stdout).Encode(fixture.Statement)
		os.Exit(1)
	case "overflow":
		// The statement and trailing whitespace remain valid JSON; only the
		// output bound may reject them, not a later syntax error.
		json.NewEncoder(os.Stdout).Encode(fixture.Statement)
		os.Stdout.Write(bytes.Repeat([]byte{' '}, maximumStatementBytes+1))
		os.Exit(0)
	case "stderr-overflow":
		os.Stderr.Write(bytes.Repeat([]byte{'x'}, 64*1024+1))
		json.NewEncoder(os.Stdout).Encode(fixture.Statement)
		os.Exit(0)
	case "orphan":
		command := exec.Command("/proc/self/exe")
		command.Args[0] = "urnetwork-native-fee-test-orphan"
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if command.Start() != nil {
			os.Exit(2)
		}
	}
	json.NewEncoder(os.Stdout).Encode(fixture.Statement)
	if fixture.Mode == "trailing" {
		os.Stdout.Write([]byte("{}"))
	}
	os.Exit(0)
}

// Owns a protected verifier copy independently of go test's temporary image.
func invocationFixture(t *testing.T, mode string) (Authority, Reference, string) {
	t.Helper()
	sha := "sha256:" + strings.Repeat("31", 32)
	hash := "0x" + strings.Repeat("32", 32)
	policy := NativePolicy{ApprovalPublicKey: hash, Genesis: hash, EvmChainId: 964, EngineSha256: sha, CheckpointSha256: sha, ReviewSha256: sha, ProfileSha256: sha}
	key, err := crypto.HexToECDSA(strings.Repeat("01", 32))
	if err != nil {
		t.Fatal(err)
	}
	destination := common.HexToAddress("0x" + strings.Repeat("42", 20))
	transaction, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(964), Nonce: 7, Gas: 21000, GasFeeCap: big.NewInt(20), GasTipCap: big.NewInt(2), To: &destination}), types.LatestSignerForChainID(big.NewInt(964)), key)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	payer := blake2b.Sum256(append([]byte("evm:"), sender.Bytes()...))
	statement := Statement{Schema: StatementSchema, RequestHash: sha, PolicyHash: policy.Hash(), ApprovalHash: sha, ProofHash: sha, Genesis: hash, EvmChainId: 964, RuntimeCodeSha256: sha, EngineSha256: sha, ProfileSha256: sha, PayerProfile: "subtensor-fron-parent-context-hashed-h160-v1", PayerRuntimeSource: "67dcf7f791dc495064c293f080a0702cb433e51e", NativeBlockNumber: 11, NativeBlockHash: hash, NativeParentNumber: 10, NativeParentHash: hash, NativeStateRoot: hash, NativeParentStateRoot: hash, NativeFinalizedNumber: 11, NativeFinalizedHash: hash, TransactionHash: strings.ToLower(transaction.Hash().Hex()), Sender: strings.ToLower(sender.Hex()), Nonce: 7, RawTransaction: signature, ReceiptStatus: 1, ReceiptBytesHash: sha, EvmBlockNumber: 8, EvmBlockHash: hash, Payer: "0x" + hex.EncodeToString(payer[:]), WithdrawalRao: "1000", RefundRao: "250", DebitRao: "750"}
	directory := t.TempDir()
	path := filepath.Join(directory, "request.json")
	statement.Originals = []Original{{Kind: "request", Reference: Reference{Path: path, Sha256: sha}}}
	for _, kind := range originalKinds[1:] {
		input := []byte("synthetic protocol input: " + kind)
		digest := sha256.Sum256(input)
		inputPath := filepath.Join(directory, kind+".json")
		if err := os.WriteFile(inputPath, input, 0600); err != nil {
			t.Fatal(err)
		}
		statement.Originals = append(statement.Originals, Original{Kind: kind, Reference: Reference{Path: inputPath, Sha256: "sha256:" + hex.EncodeToString(digest[:])}})
	}
	raw, err := json.Marshal(protocolFixture{Statement: statement, Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	requestDigest := sha256.Sum256(raw)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	sourceInfo, err := file.Stat()
	if err != nil {
		t.Fatal(errors.Join(err, file.Close()))
	}
	// A go test image can have shared cache links or writable build modes.
	// Copy it into a fresh inode; never chmod the running or cached image.
	verifierPath := filepath.Join(directory, "verifier")
	verifier, err := os.OpenFile(verifierPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(errors.Join(err, file.Close()))
	}
	engineDigest := sha256.New()
	copied, copyErr := io.Copy(io.MultiWriter(verifier, engineDigest), io.LimitReader(file, maximumExecutableBytes+1))
	if err := errors.Join(copyErr, file.Close(), verifier.Chmod(0500), verifier.Close()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(verifierPath)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0500 || stat.Nlink != 1 || os.SameFile(sourceInfo, info) || copied < 4 || copied > maximumExecutableBytes || copied != sourceInfo.Size() || copied != info.Size() {
		t.Fatal("fixture does not own a protected bounded verifier copy")
	}
	return Authority{Verifier: Reference{Path: verifierPath, Sha256: "sha256:" + hex.EncodeToString(engineDigest.Sum(nil))}, NativePolicy: policy}, Reference{Path: path, Sha256: "sha256:" + hex.EncodeToString(requestDigest[:])}, statement.TransactionHash
}

func TestInvokeNativeFeeOwnsImmutableResult(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "complete")
	verified, err := Invoke(t.Context(), authority, request, transaction, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := verified.Check(t.Context(), authority); err != nil {
		t.Fatal(err)
	}
	facts := verified.Facts()
	facts.RawTransaction[0] ^= 1
	facts.Originals[0].Reference.Path = "/caller/changed"
	if verified.Facts().Originals[0].Reference.Path == facts.Originals[0].Reference.Path {
		t.Fatal("caller rewrote original proof custody")
	}
	if verified.Facts().RawTransaction[0] == facts.RawTransaction[0] {
		t.Fatal("caller rewrote original signature")
	}
	raw, err := json.Marshal(verified)
	if err != nil {
		t.Fatal(err)
	}
	var imported Verified
	if err := json.Unmarshal(raw, &imported); err != nil {
		t.Fatal(err)
	}
	if imported.Check(t.Context(), authority) == nil {
		t.Fatal("JSON manufactured verifier authority")
	}
	authority.NativePolicy.CheckpointSha256 = "sha256:" + strings.Repeat("51", 32)
	if verified.Check(t.Context(), authority) == nil {
		t.Fatal("completed proof changed independent checkpoint")
	}
}

func TestInvokeNativeFeeFailureCannotPublishPlausibleOutput(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "failed")
	verified, err := Invoke(t.Context(), authority, request, transaction, time.Minute)
	if err == nil || verified != nil {
		t.Fatal("failed child published a usable outcome")
	}
}

// A valid statement cannot hide an overbound stream behind legal whitespace.
func TestInvokeNativeFeeOutputOverflowCancelsAndJoins(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "overflow")
	verified, err := Invoke(t.Context(), authority, request, transaction, time.Minute)
	if !errors.Is(err, context.Canceled) || verified != nil || !strings.Contains(err.Error(), "native fee verifier output exceeds bound") {
		t.Fatal("overbound child output did not cancel its owner", err)
	}
}

// Diagnostics have their own cap even when the child supplies a valid result.
func TestInvokeNativeFeeStderrOverflowCancelsAndJoins(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "stderr-overflow")
	verified, err := Invoke(t.Context(), authority, request, transaction, time.Minute)
	if !errors.Is(err, context.Canceled) || verified != nil || !strings.Contains(err.Error(), "native fee verifier output exceeds bound") {
		t.Fatal("overbound child diagnostics did not cancel its owner", err)
	}
}

// The command copy dispatch must visit the owner cancellation boundary.
func TestInvokeNativeFeeCancellationAfterOutputCannotSettle(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "complete")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	visited := false
	verified, err := invoke(ctx, authority, request, transaction, time.Minute, invocationHooks{afterOutput: func() {
		visited = true
		cancel()
	}})
	if !visited {
		t.Fatal("command output bypassed its owner cancellation hook")
	}
	if !errors.Is(err, context.Canceled) || verified != nil {
		t.Fatal("canceled owner accepted completed-looking bytes", err)
	}
}

// Copy's ReaderFrom fast path must not bypass chunk admission or its error latch.
func TestNativeFeeOutputCopyPreservesBoundAndError(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	visits := 0
	output := boundedOutput{maximum: 4, cancel: cancel, after: func() { visits++ }}
	// Hide the source's WriterTo to exercise destination copy dispatch.
	source := struct{ io.Reader }{Reader: strings.NewReader("abcd")}
	if n, err := io.Copy(&output, source); err != nil || n != 4 || output.buffer.String() != "abcd" || visits != 1 || ctx.Err() != nil {
		t.Fatal("exact-bound copied output did not pass chunk admission", n, err, visits)
	}
	source.Reader = strings.NewReader("e")
	_, copyErr := io.Copy(&output, source)
	if copyErr == nil || copyErr != output.err || !errors.Is(ctx.Err(), context.Canceled) || output.buffer.String() != "abcd" || visits != 1 {
		t.Fatal("copy dispatch bypassed the output bound", copyErr, visits)
	}
	if n, err := output.Write(nil); n != 0 || err != copyErr || visits != 1 {
		t.Fatal("failed output accepted a later write", n, err, visits)
	}
}

func TestInvokeNativeFeeSealedExecutableCannotChangeAfterHash(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "complete")
	visited := false
	verified, err := invoke(t.Context(), authority, request, transaction, time.Minute, invocationHooks{beforeStart: func(_ context.Context, executable *os.File) {
		visited = true
		if _, err := executable.WriteAt([]byte{0}, 0); !errors.Is(err, syscall.EPERM) {
			t.Errorf("retained executable was mutable: %v", err)
		}
	}})
	if err != nil || verified == nil || !visited {
		t.Fatal("retained original executable did not finish", err)
	}
}

func TestInvokeNativeFeeRejectsUnreapedSeparateGroupDescendant(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "orphan")
	verified, err := Invoke(t.Context(), authority, request, transaction, time.Minute)
	if err == nil || verified != nil {
		t.Fatal("surviving separate-group descendant escaped owned join")
	}
}

func TestInvokeNativeFeeRejectsTrailingOutput(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "trailing")
	verified, err := Invoke(t.Context(), authority, request, transaction, time.Minute)
	if err == nil || verified != nil {
		t.Fatal("multiple statements became a single verified result")
	}
}

func TestNativeFeeStatementRejectsUnknownAndOverwidthAmounts(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "complete")
	raw, err := os.ReadFile(request.Path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture protocolFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	fixture.Statement.RequestSha256 = request.Sha256
	fixture.Statement.Originals[0].Reference = request
	fixture.Statement.RefundRao = ""
	if fixture.Statement.Validate(authority.NativePolicy, request, transaction) == nil {
		t.Fatal("unknown refund became zero")
	}
	fixture.Statement.WithdrawalRao, fixture.Statement.RefundRao, fixture.Statement.DebitRao = "18446744073709551616", "0", "18446744073709551616"
	if fixture.Statement.Validate(authority.NativePolicy, request, transaction) == nil {
		t.Fatal("amount exceeded original u64 event width")
	}
}

func TestNativeFeeOriginalCustodyStreamsCompleteOrderedProofs(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "complete")
	verified, err := Invoke(t.Context(), authority, request, transaction, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	previous := ""
	count := 0
	err = verified.RetainOriginals(t.Context(), func(kind string, ref Reference, reader io.Reader) error {
		if ref.Sha256 < previous {
			t.Fatal("original custody would acquire content locks out of order")
		}
		previous = ref.Sha256
		count++
		_, err := io.Copy(io.Discard, reader)
		return err
	})
	if err != nil || count != 7 {
		t.Fatal("complete original proof closure was not retained", err, count)
	}
}

func TestNativeFeeOriginalCustodyRejectsPartialSinkAndMissingFile(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "complete")
	verified, err := Invoke(t.Context(), authority, request, transaction, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := verified.RetainOriginals(t.Context(), func(string, Reference, io.Reader) error { return nil }); err == nil {
		t.Fatal("partial sink claimed complete original proof custody")
	}
	if err := os.Remove(verified.Facts().Originals[2].Reference.Path); err != nil {
		t.Fatal(err)
	}
	if err := verified.RetainOriginals(t.Context(), func(_ string, _ Reference, reader io.Reader) error { _, err := io.Copy(io.Discard, reader); return err }); err == nil {
		t.Fatal("missing original proof could release a fee ceiling")
	}
}

func TestNativeFeeOriginalCustodyRejectsChangedInput(t *testing.T) {
	authority, request, transaction := invocationFixture(t, "complete")
	verified, err := Invoke(t.Context(), authority, request, transaction, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(verified.Facts().Originals[2].Reference.Path, []byte("changed archive"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verified.RetainOriginals(t.Context(), func(_ string, _ Reference, reader io.Reader) error { _, err := io.Copy(io.Discard, reader); return err }); err == nil {
		t.Fatal("changed original bytes retained old proof authority")
	}
}
