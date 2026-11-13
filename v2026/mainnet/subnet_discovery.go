// Signer-free discovery supplies the missing owner/generation and complete UID
// maps for independent policy review. It cannot manufacture a reset policy.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/urfoundation/sn/v2026/crv4"
)

const subnetDiscoverySchema = "urnetwork-mainnet-subnet-discovery-v1"

// A permit is observed state, not a role classification or permission to prune.
type subnetDiscoveredSeat struct {
	subnetRegistration
	Active           bool   `json:"active"`
	ValidatorPermit  bool   `json:"validator_permit"`
	EmissionAlphaRao string `json:"emission_alpha_rao"`
	Disposition      string `json:"disposition"`
}

// Membership completeness concerns the two UID maps only. Custody, roles,
// locks, stake, source provenance and independent chain authority remain open.
type subnetDiscovery struct {
	Schema                  string                      `json:"schema"`
	Admission               string                      `json:"admission"`
	SnapshotFileHash        string                      `json:"snapshot_file_hash"`
	FinalityAuthority       string                      `json:"finality_authority"`
	Identity                chainIdentity               `json:"identity"`
	RuntimeVersion          crv4.RuntimeVersionIdentity `json:"runtime_version"`
	RuntimeCodeHash         string                      `json:"runtime_code_hash"`
	RuntimeMetadataHash     string                      `json:"runtime_metadata_hash"`
	CodecSourceCommit       string                      `json:"codec_source_commit"`
	RuntimeSourceProven     bool                        `json:"runtime_source_proven"`
	Netuid                  uint16                      `json:"netuid"`
	SubnetOwnerColdkey      string                      `json:"subnet_owner_coldkey_account_id"`
	SubnetOwnerHotkey       *string                     `json:"subnet_owner_hotkey_account_id"`
	SubnetRegistrationBlock uint64                      `json:"subnet_registration_block"`
	SubnetGeneration        uint64                      `json:"subnet_generation"`
	MinimumUids             uint16                      `json:"minimum_uids"`
	MaximumUids             uint16                      `json:"maximum_uids"`
	RegistrationAllowed     bool                        `json:"registration_allowed"`
	PowRegistrationAllowed  bool                        `json:"pow_registration_allowed"`
	Seats                   []subnetDiscoveredSeat      `json:"seats"`
	RootRegistrations       []subnetRegistration        `json:"root_registrations_excluded_from_reset"`
	Storage                 []rootStorageValue          `json:"storage"`
	MembershipComplete      bool                        `json:"membership_complete"`
	ResetReady              bool                        `json:"reset_ready"`
	ApplyAuthority          bool                        `json:"apply_authority"`
	Blockers                []string                    `json:"blockers"`
}

// This separate envelope cannot be decoded as a census policy or trim plan.
type subnetDiscoveryEnvelope struct {
	Discovery   subnetDiscovery `json:"discovery"`
	ContentHash string          `json:"content_hash"`
}

// All reads share one deadline and the retained native hash. Snapshot pins
// are observations, used for equality checks only, never as operator approval.
func (self *rpcClient) readSubnetDiscovery(ctx context.Context, snapshot runtimeSnapshot, snapshotFileHash string) (result subnetDiscovery, resultErr error) {
	if ctx == nil {
		return subnetDiscovery{}, errors.New("discovery context is unavailable")
	}
	if err := validateSubnetDiscoveryRuntime(snapshot); err != nil {
		return subnetDiscovery{}, err
	}
	sampleCtx, cancel := context.WithTimeout(ctx, self.retryWindow)
	defer cancel()
	defer func() {
		resultErr = errors.Join(resultErr, sampleCtx.Err())
		if resultErr != nil {
			result = subnetDiscovery{}
		}
	}()
	observed := snapshot.Identity
	identity, metadata, err := self.readApprovedRuntimeAt(sampleCtx,
		identityExpectation{NativeChain: observed.NativeChain, GenesisHash: observed.GenesisHash, EvmChainId: observed.EvmChainId},
		snapshot.Version, snapshot.CodeHash, snapshot.MetadataHash, observed.FinalizedHash)
	if err != nil {
		return subnetDiscovery{}, err
	}
	if identity.FinalizedNumber != observed.FinalizedNumber {
		return subnetDiscovery{}, fmt.Errorf("%w: discovery retained height differs from authenticated header", errRpcIntegrity)
	}
	entries, err := observationStorageProfile(metadata, subnetStorageSpecs)
	if err != nil {
		return subnetDiscovery{}, fmt.Errorf("%w: discovery storage profile: %v", errRpcIntegrity, err)
	}
	reader := &rootStorageReader{client: self, metadata: metadata, entries: entries, specs: subnetStorageSpecs, block: identity.FinalizedHash, valueKVs: map[string]rootStorageValue{}, batchRegistrations: true}
	registrations, maximum, err := reader.subnetRegistrations(sampleCtx, 25, identity.FinalizedNumber)
	if err != nil {
		return subnetDiscovery{}, err
	}
	rootRegistrations, _, err := reader.subnetRegistrations(sampleCtx, 0, identity.FinalizedNumber)
	if err != nil {
		return subnetDiscovery{}, err
	}
	valueKVs := map[string]rootStorageValue{}
	for _, name := range []string{"SubnetOwner", "SubnetOwnerHotkey", "NetworkRegisteredAt", "RegisteredSubnetCounter", "MinAllowedUids", "NetworkRegistrationAllowed", "NetworkPowRegistrationAllowed", "Active", "ValidatorPermit", "Emission"} {
		value, err := reader.read(sampleCtx, name, []byte{25, 0})
		if err != nil {
			return subnetDiscovery{}, err
		}
		valueKVs[name] = value
	}
	owner, born, ownerHotkey := valueKVs["SubnetOwner"], valueKVs["NetworkRegisteredAt"], valueKVs["SubnetOwnerHotkey"]
	if owner.RawStorage == nil || !subnetAccountValid(owner.EffectiveScale) || born.RawStorage == nil {
		return subnetDiscovery{}, fmt.Errorf("%w: discovery owner or subnet registration is not recorded", errRpcIntegrity)
	}
	registeredAt := binary.LittleEndian.Uint64(born.data)
	minimum := binary.LittleEndian.Uint16(valueKVs["MinAllowedUids"].data)
	if registeredAt > identity.FinalizedNumber || minimum > maximum {
		return subnetDiscovery{}, fmt.Errorf("%w: discovery subnet generation or capacity is inconsistent", errRpcIntegrity)
	}
	var explicitOwnerHotkey *string
	if ownerHotkey.RawStorage != nil {
		if !subnetAccountValid(ownerHotkey.EffectiveScale) {
			return subnetDiscovery{}, fmt.Errorf("%w: discovery explicit owner hotkey is zero", errRpcIntegrity)
		}
		explicitOwnerHotkey = &ownerHotkey.EffectiveScale
	}
	active, activeCount, _ := rootVector(valueKVs["Active"].data, 1, 0)
	permits, permitCount, _ := rootVector(valueKVs["ValidatorPermit"].data, 1, 0)
	emissions, emissionCount, _ := rootVector(valueKVs["Emission"].data, 8, 0)
	if activeCount != len(registrations) || permitCount != len(registrations) || emissionCount != len(registrations) {
		return subnetDiscovery{}, fmt.Errorf("%w: discovery vectors do not cover the complete membership", errRpcIntegrity)
	}
	seats := make([]subnetDiscoveredSeat, len(registrations))
	for index, registration := range registrations {
		if registration.RegistrationBlock < registeredAt {
			return subnetDiscovery{}, fmt.Errorf("%w: discovery registration predates the subnet generation", errRpcIntegrity)
		}
		seats[index] = subnetDiscoveredSeat{subnetRegistration: registration, Active: active[index] == 1, ValidatorPermit: permits[index] == 1,
			EmissionAlphaRao: strconv.FormatUint(binary.LittleEndian.Uint64(emissions[index*8:]), 10), Disposition: "unclassified"}
	}
	// Recheck network and canonical identity after the complete storage read.
	for _, step := range []struct {
		method string
		params []any
		want   string
	}{
		{method: "chain_getBlockHash", params: []any{identity.FinalizedNumber}, want: identity.FinalizedHash},
		{method: "chain_getBlockHash", params: []any{0}, want: identity.GenesisHash},
		{method: "system_chain", params: []any{}, want: identity.NativeChain},
		{method: "state_getStorageHash", params: []any{runtimeCodeStorageKey, identity.FinalizedHash}, want: snapshot.CodeHash},
	} {
		var value string
		if err := self.call(sampleCtx, step.method, step.params, &value); err != nil {
			return subnetDiscovery{}, err
		}
		if !strings.EqualFold(value, step.want) || step.method == "system_chain" && value != step.want {
			return subnetDiscovery{}, fmt.Errorf("%w: discovery closing %s differs", errRpcIntegrity, step.method)
		}
	}
	var chainHex string
	if err := self.call(sampleCtx, "eth_chainId", []any{}, &chainHex); err != nil {
		return subnetDiscovery{}, err
	}
	if chainId, err := parseHexNumber(chainHex); err != nil || chainId != identity.EvmChainId {
		return subnetDiscovery{}, fmt.Errorf("%w: discovery closing EVM identity differs", errRpcIntegrity)
	}
	if err := self.closeSnapshotFinality(sampleCtx, identity); err != nil {
		return subnetDiscovery{}, err
	}
	return subnetDiscovery{
		Schema: subnetDiscoverySchema, Admission: "unapproved_observation", SnapshotFileHash: snapshotFileHash, FinalityAuthority: "rpc-assertion", Identity: identity,
		RuntimeVersion: snapshot.Version, RuntimeCodeHash: snapshot.CodeHash, RuntimeMetadataHash: snapshot.MetadataHash, CodecSourceCommit: rootPassiveSource,
		Netuid: 25, SubnetOwnerColdkey: owner.EffectiveScale, SubnetOwnerHotkey: explicitOwnerHotkey, SubnetRegistrationBlock: registeredAt,
		SubnetGeneration: binary.LittleEndian.Uint64(valueKVs["RegisteredSubnetCounter"].data), MinimumUids: minimum, MaximumUids: maximum,
		RegistrationAllowed: valueKVs["NetworkRegistrationAllowed"].data[0] == 1, PowRegistrationAllowed: valueKVs["NetworkPowRegistrationAllowed"].data[0] == 1,
		Seats: seats, RootRegistrations: rootRegistrations, Storage: reader.evidence(), MembershipComplete: true,
		Blockers: []string{"INDEPENDENT_NETWORK_AND_RUNTIME_APPROVAL_REQUIRED", "INDEPENDENT_OWNER_GENERATION_AND_ROLE_POLICY_REQUIRED", "CUSTODY_STAKE_LOCKS_CLAIMS_AND_HISTORY_NOT_AUDITED", "OWNER_TRIM_APPROVAL_AND_EXECUTION_UNAVAILABLE"},
	}, nil
}

// A domain-separated integrity seal conveys no new authority.
func sealSubnetDiscovery(discovery subnetDiscovery) (subnetDiscoveryEnvelope, error) {
	raw, err := json.Marshal(discovery)
	if err != nil {
		return subnetDiscoveryEnvelope{}, err
	}
	digest := sha256.Sum256(append([]byte(subnetDiscoverySchema+"\x00"), raw...))
	return subnetDiscoveryEnvelope{Discovery: discovery, ContentHash: "sha256:" + hex.EncodeToString(digest[:])}, nil
}

// A zero exit means the declared membership observation completed, not that
// runtime, role, trim or activation approval has been obtained.
func runSubnetDiscoveryCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("subnet-discover", flag.ContinueOnError)
	flags.SetOutput(stderr)
	rpcUrl := flags.String("rpc", "", "explicit HTTP(S) observation route")
	snapshotPath := flags.String("snapshot", "", "retained unapproved runtime-snapshot or finalized-snapshot JSON")
	retryWindow := flags.Duration("retry-window", 300*time.Second, "one complete bounded observation window, 60s through 15m")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 || *rpcUrl == "" || *snapshotPath == "" || *retryWindow < time.Minute || *retryWindow > 15*time.Minute {
		fmt.Fprintln(stderr, "subnet-discover requires --rpc URL --snapshot FILE and a 60s..15m retry-window")
		return 2
	}
	snapshot, hash, err := readSubnetDiscoveryInput(*snapshotPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	client, err := newRpcClient(*rpcUrl, *retryWindow)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer client.httpClient.CloseIdleConnections()
	discovery, err := client.readSubnetDiscovery(ctx, snapshot, hash)
	if err != nil {
		fmt.Fprintln(stderr, err)
		if errors.Is(err, errRpcIntegrity) || errors.Is(err, errRpcIdentityMismatch) {
			return 3
		}
		return 1
	}
	envelope, err := sealSubnetDiscovery(discovery)
	if err == nil {
		err = json.NewEncoder(stdout).Encode(envelope)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
