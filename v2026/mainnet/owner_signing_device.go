// The owner-local signer invokes a pinned SDK adapter with exact public bytes.
// Its exclusive journal retains issuance intent before any device operation.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const ownerSigningStateSchema = "urnetwork-mainnet-owner-device-custody-v1"
const ownerSigningAdapterSchema = "urnetwork-mainnet-owner-ledger-adapter-v1"

// All executable/backend pins are independently provisioned on the owner's
// computer. They remain bound to this one request across every restart.
type ownerSigningDeviceConfig struct {
	StatePath   string    `json:"owner_state_path"`
	PythonPath  string    `json:"python_path"`
	HelperPath  string    `json:"helper_path"`
	HelperHash  string    `json:"helper_sha256"`
	BackendPath string    `json:"backend_path"`
	BackendHash string    `json:"backend_sha256"`
	AppVersion  [3]uint16 `json:"app_version"`
}

// The SDK receives both signature-payload seams and the raw metadata15 bytes;
// no network endpoint, key material or alternate call reaches the adapter.
type ownerSigningAdapterInput struct {
	Schema             string    `json:"schema"`
	Mode               string    `json:"mode"`
	RequestHash        string    `json:"request_hash"`
	BackendPath        string    `json:"backend_path"`
	BackendHash        string    `json:"backend_sha256"`
	MetadataHex        string    `json:"metadata_scale"`
	MetadataDigest     string    `json:"metadata_digest"`
	SpecName           string    `json:"spec_name"`
	SpecVersion        uint32    `json:"spec_version"`
	Owner              string    `json:"owner_account_id"`
	Account            uint32    `json:"account"`
	Index              uint32    `json:"index"`
	Call               string    `json:"call_scale"`
	IncludedExtrinsic  string    `json:"included_in_extrinsic"`
	IncludedSignedData string    `json:"included_in_signed_data"`
	ResponsePath       string    `json:"response_path"`
	ProofHash          string    `json:"proof_sha256"`
	AppVersion         [3]uint16 `json:"expected_app_version"`
}

// Prepared contains no device result. Signed contains the durable exact device
// response; a status flag is never evidence of identity without native verify.
type ownerSigningAdapterResult struct {
	Schema         string    `json:"schema"`
	Mode           string    `json:"mode"`
	RequestHash    string    `json:"request_hash"`
	SourceCommit   string    `json:"sdk_source_commit"`
	MetadataDigest string    `json:"metadata_digest"`
	ProofHash      string    `json:"proof_sha256"`
	PublicKey      string    `json:"public_key,omitempty"`
	AppVersion     [3]uint16 `json:"app_version"`
	Response       string    `json:"multisignature,omitempty"`
}

// A bounded writer stops child output amplification without truncating JSON
// into a plausible successful result. It is owned by one exec stream.
type ownerSigningBoundedOutput struct {
	bytes.Buffer
}

// Only the declared finite response size is accepted from the adapter process.
func (self *ownerSigningBoundedOutput) Write(raw []byte) (int, error) {
	if len(raw) > ownerSigningReplyLimit-self.Len() {
		return 0, errors.New("owner Ledger adapter output exceeds bound")
	}
	return self.Buffer.Write(raw)
}

// Execute the already hash-checked helper source itself, never a shell command
// or a subsequently reopened script path. Python isolated mode omits PYTHONPATH.
func runOwnerLedgerAdapter(ctx context.Context, config ownerSigningDeviceConfig, input ownerSigningAdapterInput) (ownerSigningAdapterResult, error) {
	var result ownerSigningAdapterResult
	helper, actual, err := readBootstrapRootFile(ctx, config.HelperPath, 128*1024)
	if err != nil || actual != config.HelperHash {
		return result, errors.Join(errors.New("owner Ledger helper differs from its independent file pin"), err)
	}
	inputRaw, err := json.Marshal(input)
	if err != nil {
		return result, err
	}
	operationCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	command := exec.CommandContext(operationCtx, config.PythonPath, "-I", "-c", string(helper))
	command.Stdin = bytes.NewReader(inputRaw)
	var stdout, stderr ownerSigningBoundedOutput
	command.Stdout, command.Stderr, command.WaitDelay = &stdout, &stderr, 2*time.Second
	if err := command.Run(); err != nil {
		return result, fmt.Errorf("owner Ledger adapter: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return result, decodePlanJson(stdout.Bytes(), &result)
}

// Public tests can substitute a finite device boundary without installing any
// test flag, test signer or private key in the production command.
type ownerSigningAdapter func(context.Context, ownerSigningDeviceConfig, ownerSigningAdapterInput) (ownerSigningAdapterResult, error)

// A complete marker distinguishes a missing established journal from an
// interrupted initial reservation. No missing response proves never-issued.
type ownerSigningDeviceStore struct {
	path    string
	binding string
	request ownerSigningRequest
	lock    *os.File
	failed  error
}

// Journal hashes include the independent adapter pins, request and phase.
type ownerSigningDeviceRecord struct {
	Schema      string             `json:"schema"`
	BindingHash string             `json:"binding_hash"`
	RequestHash string             `json:"request_hash"`
	Phase       string             `json:"phase"`
	ProofHash   string             `json:"proof_sha256,omitempty"`
	Reply       *ownerSigningReply `json:"reply,omitempty"`
	ContentHash string             `json:"content_hash"`
}

// One durable directory sync covers journal and initial lock publication.
func (self *ownerSigningDeviceStore) syncParent() error {
	directory, err := os.Open(filepath.Dir(self.path))
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}

// Missing, changed or partial records remain errors after initial claim.
func (self *ownerSigningDeviceStore) load() (ownerSigningDeviceRecord, error) {
	var record ownerSigningDeviceRecord
	raw, _, err := readBootstrapRootFile(context.Background(), self.path, ownerSigningReplyLimit)
	if err != nil {
		return record, err
	}
	if err := decodePlanJson(raw, &record); err != nil {
		return record, err
	}
	claimed := record.ContentHash
	record.ContentHash = ""
	if record.Schema != ownerSigningStateSchema || record.BindingHash != self.binding || record.RequestHash != self.request.ContentHash || claimed != rootObjectHash(record) {
		return record, errors.New("owner device journal identity or content seal differs")
	}
	record.ContentHash = claimed
	switch record.Phase {
	case "reserved":
		if record.Reply != nil || record.ProofHash != "" {
			return record, errors.New("owner reserved device journal carries signing state")
		}
	case "signing":
		if record.Reply != nil || !planSha256(record.ProofHash) {
			return record, errors.New("owner unresolved device journal has invalid proof or reply")
		}
	case "signed":
		if record.Reply == nil || !planSha256(record.ProofHash) {
			return record, errors.New("owner signed journal lacks original proof or reply")
		}
		if _, err := record.Reply.validate(self.request); err != nil {
			return record, err
		}
	default:
		return record, errors.New("owner device journal phase is unknown")
	}
	return record, nil
}

// Any publication failure poisons this instance, including after rename.
func (self *ownerSigningDeviceStore) save(record ownerSigningDeviceRecord) (resultErr error) {
	if self.failed != nil {
		return self.failed
	}
	defer func() {
		if resultErr != nil {
			self.failed = resultErr
		}
	}()
	record.ContentHash = ""
	record.ContentHash = rootObjectHash(record)
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(self.path), ".owner-signing-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	written, writeErr := file.Write(raw)
	if written != len(raw) && writeErr == nil {
		writeErr = io.ErrShortWrite
	}
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	if err := os.Rename(file.Name(), self.path); err != nil {
		return err
	}
	return self.syncParent()
}

// A single owner directory claims one request once; a new packet, executable,
// backend or derivation cannot replace it even after expiry or uncertain I/O.
func openOwnerSigningDeviceStore(ctx context.Context, config ownerSigningDeviceConfig, request ownerSigningRequest) (_ *ownerSigningDeviceStore, resultErr error) {
	for _, path := range []string{config.StatePath, config.PythonPath, config.HelperPath, config.BackendPath} {
		if !bootstrapRootAbsolutePath(path) {
			return nil, errors.New("owner Ledger custody and tools require explicit canonical absolute paths")
		}
	}
	if !planSha256(config.HelperHash) || !planSha256(config.BackendHash) || config.StatePath == request.Config.Action.StatePath ||
		config.StatePath == request.Config.Action.StatePath+".lock" || filepath.Dir(config.StatePath) == filepath.Dir(request.Config.Action.StatePath) ||
		config.AppVersion[0] != 100 || config.AppVersion[1] == 0 && config.AppVersion[2] < 5 {
		return nil, errors.New("owner Ledger state must be independently located with pinned helper/backend")
	}
	for _, input := range []string{config.PythonPath, config.HelperPath, config.BackendPath} {
		if input == config.StatePath || input == config.StatePath+".lock" || input == config.StatePath+".ledger-response" {
			return nil, errors.New("owner Ledger executable overlaps custody")
		}
	}
	if err := errors.Join(ctx.Err(), bootstrapRootDirectory(filepath.Dir(config.StatePath))); err != nil {
		return nil, err
	}
	binding := rootObjectHash(struct {
		Config      ownerSigningDeviceConfig `json:"device_config"`
		RequestHash string                   `json:"request_hash"`
	}{Config: config, RequestHash: request.ContentHash})
	self := &ownerSigningDeviceStore{path: config.StatePath, binding: binding, request: request}
	defer func() {
		if resultErr != nil && self.lock != nil {
			resultErr = errors.Join(resultErr, self.lock.Close())
		}
	}()
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	fd, err := syscall.Open(self.path+".lock", flags|syscall.O_CREAT|syscall.O_EXCL, 0600)
	create := err == nil
	if errors.Is(err, os.ErrExist) {
		fd, err = syscall.Open(self.path+".lock", flags, 0)
	}
	if err != nil {
		return nil, err
	}
	self.lock = os.NewFile(uintptr(fd), self.path+".lock")
	info, err := self.lock.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("owner device marker must be a private regular file")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.New("owner device request already has a local owner")
	}
	marker := binding + "\n"
	if create {
		for _, path := range []string{self.path, self.path + ".ledger-response"} {
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				return nil, errors.New("unclaimed owner state or response already exists; restore original custody")
			}
		}
		written, err := self.lock.WriteString(marker)
		if err != nil || written != len(marker) {
			return nil, errors.Join(io.ErrShortWrite, err)
		}
		if err := errors.Join(self.lock.Sync(), self.syncParent()); err != nil {
			return nil, err
		}
	} else {
		raw, err := io.ReadAll(io.LimitReader(self.lock, int64(len(marker)+len(bootstrapRootClaimComplete)+1)))
		if err != nil {
			return nil, err
		}
		if string(raw) == marker+bootstrapRootClaimComplete {
			if _, err := self.load(); err != nil {
				return nil, err
			}
			return self, nil
		}
		if string(raw) != marker {
			return nil, errors.New("owner device marker differs; retain original custody")
		}
	}
	if _, err := os.Lstat(self.path + ".ledger-response"); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("interrupted owner reservation already has a device response")
	}
	record, err := self.load()
	if errors.Is(err, os.ErrNotExist) {
		record = ownerSigningDeviceRecord{Schema: ownerSigningStateSchema, BindingHash: binding, RequestHash: request.ContentHash, Phase: "reserved"}
		if err := self.save(record); err != nil {
			return nil, err
		}
	} else if err != nil || record.Phase != "reserved" {
		return nil, errors.Join(errors.New("interrupted owner reservation has ambiguous device state"), err)
	}
	written, err := self.lock.WriteAt([]byte(bootstrapRootClaimComplete), int64(len(marker)))
	if err != nil || written != len(bootstrapRootClaimComplete) {
		return nil, errors.Join(io.ErrShortWrite, err)
	}
	if err := self.lock.Sync(); err != nil {
		return nil, err
	}
	return self, nil
}

// Original public bytes recover without device access. Unknown issuance never
// causes a new call. The adapter persists its response before returning it.
func signOwnerRequest(ctx context.Context, config ownerSigningDeviceConfig, request ownerSigningRequest, adapter ownerSigningAdapter) (reply ownerSigningReply, resultErr error) {
	action := request.Config.Action
	if ctx == nil {
		return reply, errors.New("owner signing requires cancellation context")
	}
	if err := action.validate(); err != nil {
		return reply, err
	}
	path, err := ownerLedgerDerivationPath(action.DerivationPath)
	if err != nil || action.Schema != ownerTrimLedgerActionSchema || path[2]&0x80000000 == 0 || path[3] != 0x80000000 || path[4]&0x80000000 == 0 {
		return reply, errors.New("owner SDK signer requires Ed25519 v2 and exact m/44'/354'/account'/0'/index' path")
	}
	store, err := openOwnerSigningDeviceStore(ctx, config, request)
	if err != nil {
		return reply, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.lock.Close()) }()
	record, err := store.load()
	if err != nil {
		return reply, err
	}
	if record.Phase == "signed" {
		return *record.Reply, nil
	}
	if adapter == nil {
		adapter = runOwnerLedgerAdapter
	}
	var issueErr error
	if record.Phase == "reserved" {
		call, _ := hex.DecodeString(action.Call[2:])
		payload, _ := hex.DecodeString(action.Payload[2:])
		extraLength := 2 + len(rootCompact(uint64(action.Nonce))) + 2
		input := ownerSigningAdapterInput{Schema: ownerSigningAdapterSchema, Mode: "prepare", RequestHash: request.ContentHash,
			BackendPath: config.BackendPath, BackendHash: config.BackendHash, MetadataHex: request.LedgerMetadata,
			MetadataDigest: action.MetadataDigest, SpecName: action.Runtime.RuntimeVersion.SpecName, SpecVersion: action.Runtime.RuntimeVersion.SpecVersion,
			Owner: action.Coldkey, Account: path[2] & 0x7fffffff, Index: path[4] & 0x7fffffff,
			Call: action.Call, IncludedExtrinsic: "0x" + hex.EncodeToString(payload[len(call):len(call)+extraLength]),
			IncludedSignedData: "0x" + hex.EncodeToString(payload[len(call)+extraLength:]), ResponsePath: config.StatePath + ".ledger-response", AppVersion: config.AppVersion}
		prepared, err := adapter(ctx, config, input)
		if err != nil || prepared.Schema != ownerSigningAdapterSchema || prepared.Mode != "prepare" || prepared.RequestHash != request.ContentHash ||
			prepared.SourceCommit != rootActionV1Source || prepared.MetadataDigest != action.MetadataDigest || !planSha256(prepared.ProofHash) ||
			prepared.PublicKey != "" || prepared.Response != "" || prepared.AppVersion != [3]uint16{} {
			return reply, errors.Join(errors.New("owner Ledger metadata preparation failed before device access"), err)
		}
		record.Phase, record.ProofHash = "signing", prepared.ProofHash
		if err := store.save(record); err != nil {
			return reply, err
		}
		input.Mode, input.ProofHash = "sign", prepared.ProofHash
		// Even an error may follow a durable device response. Recovery below
		// reads that artifact and never retries the device call.
		_, issueErr = adapter(ctx, config, input)
	}
	raw, _, err := readBootstrapRootFile(context.Background(), config.StatePath+".ledger-response", ownerSigningReplyLimit)
	if err != nil {
		return reply, errors.Join(errors.New("owner device issuance unresolved; retain original state and response, never sign this request again"), issueErr, err)
	}
	var response ownerSigningAdapterResult
	if err := decodePlanJson(raw, &response); err != nil {
		return reply, err
	}
	if response.Schema != ownerSigningAdapterSchema || response.Mode != "sign" || response.RequestHash != request.ContentHash || response.SourceCommit != rootActionV1Source ||
		response.MetadataDigest != action.MetadataDigest || response.ProofHash != record.ProofHash || response.PublicKey != action.Coldkey ||
		response.AppVersion != config.AppVersion {
		return reply, errors.New("retained owner device response differs from the original request, owner, proof or supported app family")
	}
	encoded, err := rootReceiptHex(response.Response, 65)
	if err != nil {
		return reply, err
	}
	signature, err := ownerLedgerResponse(request, encoded)
	if err != nil {
		return reply, err
	}
	reply, err = newOwnerSigningReply(request, signature)
	if err != nil {
		return reply, err
	}
	record.Phase, record.Reply = "signed", &reply
	return reply, store.save(record)
}
