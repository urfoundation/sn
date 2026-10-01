package main

// Accounted traffic is a separate owner from deployment and validation. Its
// immutable reservations survive crashes; an uncertain send consumes its byte
// allowance rather than being silently repeated outside the approved cap.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/urnetwork/connect/v2026"
)

const paidTrafficSchema = "urnetwork-paid-traffic-v1"

// This sidecar descriptor references the approved run without replacing its
// supervisor manifest. Credentials remain in the operator database and seed
// file; no credential is a manifest or result field.
type PaidTrafficManifest struct {
	Schema              string                `json:"schema"`
	ParentPlanHash      string                `json:"parent_plan_hash"`
	DeploymentId        string                `json:"deployment_id"`
	Operator            uint64                `json:"operator"`
	DatabaseContainer   string                `json:"database_container"`
	ApiUrl              string                `json:"api_url"`
	ConnectUrl          string                `json:"connect_url"`
	ClientId            string                `json:"client_id"`
	NetworkId           string                `json:"network_id"`
	ClientKeySeedFile   string                `json:"client_key_seed_file"`
	ClientPublicKeyHex  string                `json:"client_public_key_hex"`
	Providers           []PaidTrafficProvider `json:"providers"`
	TargetPayloadBytes  uint64                `json:"target_payload_bytes"`
	HardPayloadCapBytes uint64                `json:"hard_payload_cap_bytes"`
	BatchPayloadBytes   uint64                `json:"batch_payload_bytes"`
	MaximumAttempts     uint64                `json:"maximum_attempts"`
	InflightMessages    int                   `json:"inflight_messages"`
	BatchTimeoutSeconds uint64                `json:"batch_timeout_seconds"`
	RetryMinimumSeconds uint64                `json:"retry_minimum_seconds"`
	RetryMaximumSeconds uint64                `json:"retry_maximum_seconds"`
	ExpiresAt           time.Time             `json:"expires_at"`
}

type PaidTrafficProvider struct {
	ClientId  string `json:"client_id"`
	NetworkId string `json:"network_id"`
}

type PaidTrafficAllocation struct {
	ClientId     string `json:"client_id"`
	PayloadBytes uint64 `json:"payload_bytes"`
}

type paidTrafficReservation struct {
	Schema              string                  `json:"schema"`
	ManifestHash        string                  `json:"manifest_hash"`
	Sequence            uint64                  `json:"sequence"`
	OwnerPid            int                     `json:"owner_pid"`
	OwnerStartTimeTicks uint64                  `json:"owner_start_time_ticks"`
	ReservedAt          time.Time               `json:"reserved_at"`
	Allocations         []PaidTrafficAllocation `json:"allocations"`
}

type PaidTrafficBatchResult struct {
	Acknowledged  []PaidTrafficAllocation `json:"acknowledged"`
	CleanupJoined bool                    `json:"cleanup_joined"`
}

type paidTrafficCompletion struct {
	Schema       string                 `json:"schema"`
	ManifestHash string                 `json:"manifest_hash"`
	Sequence     uint64                 `json:"sequence"`
	CompletedAt  time.Time              `json:"completed_at"`
	Result       PaidTrafficBatchResult `json:"result"`
	Error        string                 `json:"error,omitempty"`
}

// Acknowledgements demonstrate peer delivery. Only operator sweeps and the
// later on-chain receipts establish settled usage, deposits, or acceptance.
type PaidTrafficStatus struct {
	Schema                   string                  `json:"schema"`
	ManifestHash             string                  `json:"manifest_hash"`
	ParentPlanHash           string                  `json:"parent_plan_hash"`
	Operator                 uint64                  `json:"operator"`
	Stage                    string                  `json:"stage"`
	Attempts                 uint64                  `json:"attempts"`
	FailedAttempts           uint64                  `json:"failed_attempts"`
	ReservedPayloadBytes     uint64                  `json:"reserved_payload_bytes"`
	AcknowledgedPayloadBytes uint64                  `json:"acknowledged_payload_bytes"`
	UncertainPayloadBytes    uint64                  `json:"uncertain_payload_bytes"`
	Providers                []PaidTrafficAllocation `json:"providers"`
	LastError                string                  `json:"last_error,omitempty"`
	CleanupPending           bool                    `json:"cleanup_pending"`
	FinalAcceptance          bool                    `json:"final_acceptance"`
	SettlementVerified       bool                    `json:"settlement_verified"`
}

var paidTrafficContainerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var paidTrafficCredentialPattern = regexp.MustCompile(`[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`)

func paidTrafficSafeError(err error) string {
	if err == nil {
		return ""
	}
	message := paidTrafficCredentialPattern.ReplaceAllString(err.Error(), "[credential redacted]")
	if strings.Contains(strings.ToLower(message), "private key") || strings.Contains(strings.ToLower(message), "secret") {
		return "sensitive traffic diagnostic omitted"
	}
	if len(message) > 1024 {
		message = message[:1024]
	}
	return message
}

func (self PaidTrafficManifest) Validate() error {
	if self.Schema != paidTrafficSchema || self.DeploymentId == "" || self.Operator == 0 {
		return errors.New("paid traffic deployment identity is incomplete")
	}
	if value, err := hex.DecodeString(strings.TrimPrefix(self.ParentPlanHash, "0x")); err != nil || len(value) != 32 {
		return errors.New("paid traffic parent plan hash is invalid")
	}
	if !paidTrafficContainerPattern.MatchString(self.DatabaseContainer) || !filepath.IsAbs(self.ClientKeySeedFile) {
		return errors.New("paid traffic credential location is invalid")
	}
	if value, err := hex.DecodeString(self.ClientPublicKeyHex); err != nil || len(value) != 32 {
		return errors.New("paid traffic client public key is invalid")
	}
	for _, endpoint := range []struct{ value, scheme string }{{value: self.ApiUrl, scheme: "http"}, {value: self.ConnectUrl, scheme: "ws"}} {
		parsed, err := url.Parse(endpoint.value)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != endpoint.scheme && parsed.Scheme != endpoint.scheme+"s") {
			return errors.New("paid traffic endpoint is invalid")
		}
	}
	validId := func(value string) bool {
		id, err := connect.ParseId(value)
		return err == nil && id != (connect.Id{}) && id.String() == value
	}
	if !validId(self.ClientId) || !validId(self.NetworkId) || len(self.Providers) == 0 || len(self.Providers) > 16 {
		return errors.New("paid traffic consumer or provider identity is invalid")
	}
	seen := map[string]bool{}
	for _, provider := range self.Providers {
		if !validId(provider.ClientId) || !validId(provider.NetworkId) || provider.ClientId == self.ClientId || provider.NetworkId == self.NetworkId || seen[provider.ClientId] {
			return errors.New("paid traffic requires distinct, unique providers outside the consumer network")
		}
		seen[provider.ClientId] = true
	}
	if self.TargetPayloadBytes < uint64(len(self.Providers)) || self.HardPayloadCapBytes < self.TargetPayloadBytes || self.HardPayloadCapBytes > 16*1024*1024*1024*1024 || self.BatchPayloadBytes < uint64(len(self.Providers)) || self.BatchPayloadBytes > 16*1024*1024*1024 || self.BatchPayloadBytes > self.HardPayloadCapBytes {
		return errors.New("paid traffic payload allowance is invalid")
	}
	if self.MaximumAttempts == 0 || self.MaximumAttempts > 100000 || self.InflightMessages < len(self.Providers) || self.InflightMessages > 1024 || self.BatchTimeoutSeconds == 0 || self.BatchTimeoutSeconds > 3600 || self.RetryMinimumSeconds == 0 || self.RetryMaximumSeconds < self.RetryMinimumSeconds || self.RetryMaximumSeconds > 300 || self.ExpiresAt.IsZero() {
		return errors.New("paid traffic retry or lifetime bounds are invalid")
	}
	return nil
}

func paidTrafficManifestHash(manifest PaidTrafficManifest) (string, error) {
	if err := manifest.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func readPaidTrafficJson(path string, value any) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 1024*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("paid traffic file has trailing data")
	}
	return nil
}

// Link an already synced private file into the append-only attempt journal.
// Neither a retry nor a second owner can replace an earlier reservation.
func writePaidTrafficRecord(path string, value any) (returnErr error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".traffic-record-*")
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Link(file.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// Reconstruct progress from durable records, never from a process-health or
// cached-status assertion. This read does not acquire the writer's lock, query
// RPC, repair history, or block a running consumer.
func ReadPaidTrafficSidecarStatus(stateDir string) (*PaidTrafficStatus, error) {
	var manifest PaidTrafficManifest
	if err := readPaidTrafficJson(filepath.Join(stateDir, "manifest.json"), &manifest); err != nil {
		return nil, err
	}
	hash, err := paidTrafficManifestHash(manifest)
	if err != nil {
		return nil, err
	}
	status := &PaidTrafficStatus{Schema: paidTrafficSchema, ManifestHash: hash, ParentPlanHash: manifest.ParentPlanHash, Operator: manifest.Operator, Stage: "ready"}
	providerAcknowledged := map[string]uint64{}
	for _, provider := range manifest.Providers {
		providerAcknowledged[provider.ClientId] = 0
	}
	entries, err := os.ReadDir(filepath.Join(stateDir, "journal"))
	if err != nil {
		return nil, err
	}
	var reservations []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".reserved.json") {
			reservations = append(reservations, entry.Name())
		}
	}
	sort.Strings(reservations)
	for index, name := range reservations {
		var reservation paidTrafficReservation
		if err := readPaidTrafficJson(filepath.Join(stateDir, "journal", name), &reservation); err != nil {
			return nil, err
		}
		sequence := uint64(index + 1)
		if reservation.Schema != paidTrafficSchema || reservation.ManifestHash != hash || reservation.Sequence != sequence || reservation.OwnerPid <= 1 || reservation.OwnerStartTimeTicks == 0 || name != fmt.Sprintf("%08d.reserved.json", sequence) || sequence > manifest.MaximumAttempts {
			return nil, errors.New("paid traffic reservation identity or sequence changed")
		}
		allocated := map[string]uint64{}
		var reserved uint64
		for _, allocation := range reservation.Allocations {
			if _, ok := providerAcknowledged[allocation.ClientId]; !ok || allocation.PayloadBytes == 0 || allocated[allocation.ClientId] != 0 || allocation.PayloadBytes > manifest.BatchPayloadBytes-reserved {
				return nil, errors.New("paid traffic reservation allocation is invalid")
			}
			allocated[allocation.ClientId] = allocation.PayloadBytes
			reserved += allocation.PayloadBytes
		}
		if reserved == 0 || reserved > manifest.HardPayloadCapBytes-status.ReservedPayloadBytes {
			return nil, errors.New("paid traffic reservation exceeds its durable allowance")
		}
		status.Attempts++
		status.ReservedPayloadBytes += reserved
		var completion paidTrafficCompletion
		err := readPaidTrafficJson(filepath.Join(stateDir, "journal", fmt.Sprintf("%08d.completed.json", sequence)), &completion)
		if errors.Is(err, os.ErrNotExist) {
			status.UncertainPayloadBytes += reserved
			continue
		}
		if err != nil {
			return nil, err
		}
		if completion.Schema != paidTrafficSchema || completion.ManifestHash != hash || completion.Sequence != sequence {
			return nil, errors.New("paid traffic completion identity changed")
		}
		seen := map[string]bool{}
		for _, acknowledged := range completion.Result.Acknowledged {
			if allocated[acknowledged.ClientId] == 0 || seen[acknowledged.ClientId] || acknowledged.PayloadBytes > allocated[acknowledged.ClientId] {
				return nil, errors.New("paid traffic acknowledgement exceeds its reservation")
			}
			seen[acknowledged.ClientId] = true
			providerAcknowledged[acknowledged.ClientId] += acknowledged.PayloadBytes
			status.AcknowledgedPayloadBytes += acknowledged.PayloadBytes
		}
		if completion.Error != "" || !completion.Result.CleanupJoined {
			status.FailedAttempts++
			status.LastError = completion.Error
		}
		if !completion.Result.CleanupJoined {
			// A different kernel generation may safely continue after the old
			// sidecar process exits. Never reuse a still-live unjoined owner.
			observed, err := processStartTimeTicks(reservation.OwnerPid)
			if err == nil && observed == reservation.OwnerStartTimeTicks {
				status.CleanupPending = true
			} else if err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("observe prior paid traffic cleanup owner: %w", err)
			}
		}
	}
	for _, provider := range manifest.Providers {
		status.Providers = append(status.Providers, PaidTrafficAllocation{ClientId: provider.ClientId, PayloadBytes: providerAcknowledged[provider.ClientId]})
	}
	switch {
	case status.CleanupPending:
		status.Stage = "cleanup_pending"
	case status.AcknowledgedPayloadBytes >= manifest.TargetPayloadBytes:
		status.Stage = "payload_complete"
	case status.ReservedPayloadBytes == manifest.HardPayloadCapBytes:
		status.Stage = "payload_budget_exhausted"
	case status.Attempts == manifest.MaximumAttempts:
		status.Stage = "retry_budget_exhausted"
	case status.Attempts > 0:
		status.Stage = "partial"
	}
	return status, nil
}

func validatePaidTrafficBatchResult(allocations []PaidTrafficAllocation, result PaidTrafficBatchResult) (bool, error) {
	reserved := map[string]uint64{}
	for _, allocation := range allocations {
		reserved[allocation.ClientId] = allocation.PayloadBytes
	}
	var expected, acknowledged uint64
	for _, amount := range reserved {
		expected += amount
	}
	seen := map[string]bool{}
	for _, allocation := range result.Acknowledged {
		if reserved[allocation.ClientId] == 0 || seen[allocation.ClientId] || allocation.PayloadBytes > reserved[allocation.ClientId] {
			return false, errors.New("paid traffic acknowledgement exceeds its reservation")
		}
		seen[allocation.ClientId] = true
		acknowledged += allocation.PayloadBytes
	}
	return acknowledged == expected, nil
}

type paidTrafficDependencies struct {
	now          func() time.Time
	wait         func(context.Context, time.Duration) error
	send         func(context.Context, PaidTrafficManifest, []PaidTrafficAllocation) (PaidTrafficBatchResult, error)
	afterReserve func(paidTrafficReservation) error
}

// The caller reviews the manifest hash and binds its parent plan before launch.
// The separate directory is this command's entire mutation and lock scope.
func RunPaidTrafficSidecar(ctx context.Context, manifestPath, stateDir, approvedManifestHash string) (*PaidTrafficStatus, error) {
	return runPaidTrafficSidecar(ctx, manifestPath, stateDir, approvedManifestHash, paidTrafficDependencies{
		now: time.Now,
		wait: func(ctx context.Context, delay time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
				return nil
			}
		},
		send: sendPaidTrafficProtocolBatch,
	})
}

func runPaidTrafficSidecar(ctx context.Context, manifestPath, stateDir, approvedManifestHash string, dependencies paidTrafficDependencies) (*PaidTrafficStatus, error) {
	if dependencies.now == nil || dependencies.wait == nil || dependencies.send == nil {
		return nil, errors.New("paid traffic runner dependencies are incomplete")
	}
	var manifest PaidTrafficManifest
	if err := readPaidTrafficJson(manifestPath, &manifest); err != nil {
		return nil, err
	}
	hash, err := paidTrafficManifestHash(manifest)
	if err != nil || hash != approvedManifestHash {
		return nil, errors.Join(errors.New("paid traffic manifest differs from its approved hash"), err)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "journal"), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(stateDir, "traffic.lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, errors.New("another paid traffic owner holds this sidecar")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	ownerStartTimeTicks, err := processStartTimeTicks(os.Getpid())
	if err != nil {
		return nil, fmt.Errorf("observe paid traffic process generation: %w", err)
	}
	storedPath := filepath.Join(stateDir, "manifest.json")
	var stored PaidTrafficManifest
	if err := readPaidTrafficJson(storedPath, &stored); errors.Is(err, os.ErrNotExist) {
		if err := writePaidTrafficRecord(storedPath, manifest); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	} else if storedHash, err := paidTrafficManifestHash(stored); err != nil || storedHash != hash {
		return nil, errors.New("paid traffic continuation changed its durable manifest")
	}
	retryDelay := time.Duration(manifest.RetryMinimumSeconds) * time.Second
	for {
		status, err := ReadPaidTrafficSidecarStatus(stateDir)
		if err != nil {
			return nil, err
		}
		if status.Stage == "payload_complete" || status.Stage == "payload_budget_exhausted" || status.Stage == "retry_budget_exhausted" {
			return status, nil
		}
		if status.CleanupPending {
			return status, errors.New("prior paid traffic cleanup owner is still live")
		}
		if err := ctx.Err(); err != nil {
			return status, err
		}
		if !dependencies.now().Before(manifest.ExpiresAt) {
			status.Stage = "expired"
			return status, nil
		}
		remaining := min(manifest.BatchPayloadBytes, manifest.HardPayloadCapBytes-status.ReservedPayloadBytes)
		var allocations []PaidTrafficAllocation
		for index, provider := range status.Providers {
			target := manifest.TargetPayloadBytes / uint64(len(status.Providers))
			if index == len(status.Providers)-1 {
				target += manifest.TargetPayloadBytes % uint64(len(status.Providers))
			}
			if provider.PayloadBytes >= target || remaining == 0 {
				continue
			}
			// Each round visits every unfinished provider; one slow peer cannot
			// consume the whole first batch before the others receive work.
			share := max(uint64(1), remaining/uint64(len(status.Providers)-index))
			amount := min(target-provider.PayloadBytes, share)
			allocations = append(allocations, PaidTrafficAllocation{ClientId: provider.ClientId, PayloadBytes: amount})
			remaining -= amount
		}
		reservation := paidTrafficReservation{Schema: paidTrafficSchema, ManifestHash: hash, Sequence: status.Attempts + 1, OwnerPid: os.Getpid(), OwnerStartTimeTicks: ownerStartTimeTicks, ReservedAt: dependencies.now().UTC(), Allocations: allocations}
		if err := writePaidTrafficRecord(filepath.Join(stateDir, "journal", fmt.Sprintf("%08d.reserved.json", reservation.Sequence)), reservation); err != nil {
			return status, err
		}
		if dependencies.afterReserve != nil {
			if err := dependencies.afterReserve(reservation); err != nil {
				return status, err
			}
		}
		deadline := min(time.Duration(manifest.BatchTimeoutSeconds)*time.Second, manifest.ExpiresAt.Sub(dependencies.now()))
		batchCtx, cancel := context.WithTimeout(ctx, deadline)
		// The reservation owns its slice even when a sender reuses its input.
		result, sendErr := dependencies.send(batchCtx, manifest, append([]PaidTrafficAllocation(nil), allocations...))
		cancel()
		complete, resultErr := validatePaidTrafficBatchResult(allocations, result)
		if resultErr != nil {
			return status, resultErr
		}
		if !complete && sendErr == nil {
			sendErr = errors.New("paid traffic batch returned only partial acknowledgements")
		}
		completion := paidTrafficCompletion{Schema: paidTrafficSchema, ManifestHash: hash, Sequence: reservation.Sequence, CompletedAt: dependencies.now().UTC(), Result: result, Error: paidTrafficSafeError(sendErr)}
		if err := writePaidTrafficRecord(filepath.Join(stateDir, "journal", fmt.Sprintf("%08d.completed.json", reservation.Sequence)), completion); err != nil {
			return status, err
		}
		if !result.CleanupJoined {
			status, err := ReadPaidTrafficSidecarStatus(stateDir)
			return status, errors.Join(err, errors.New("paid traffic batch cleanup has not joined; refusing overlapping identity ownership"), sendErr)
		}
		if sendErr != nil {
			if err := dependencies.wait(ctx, retryDelay); err != nil {
				status, readErr := ReadPaidTrafficSidecarStatus(stateDir)
				return status, errors.Join(err, readErr)
			}
			retryDelay = min(retryDelay*2, time.Duration(manifest.RetryMaximumSeconds)*time.Second)
		} else {
			retryDelay = time.Duration(manifest.RetryMinimumSeconds) * time.Second
		}
	}
}
