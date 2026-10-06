package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"

	nativeTypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/crv4"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
)

type fleetRenewalObservation struct {
	Renewal        FleetRenewal
	Records        map[[16]byte]fleetBindingVersionRead
	Native         map[[32]byte]ExistingUIDFact
	Accounts       map[[32]byte]subtensorAccountInfo
	Evidence       map[[16]byte][]FleetBindingEvidence
	JournalEntries []JournalEntry
}

// STCoordinator retains an effective oracle schedule in its pending fields.
// Accept a completed restore to the original oracle, but no future reroute or
// inconsistent address/epoch pair, during both planning and each new write.
func fleetRenewalOriginalOracleReady(state fleetRefreshOracleState, original common.Address) bool {
	if original == (common.Address{}) || state.Immutable != original || state.Active != original {
		return false
	}
	if state.Pending == (common.Address{}) {
		return state.PendingEpoch == 0
	}
	return state.Pending == original && state.PendingEpoch != 0 && state.PendingEpoch <= state.CurrentEpoch
}

// Every retained binding remains an input. A refresh wrapper changes the wire
// names, not the underlying dual-signed binding; normalize only that wrapper.
func readFleetRenewalBindingEvidence(stateDir string) (map[[16]byte][]FleetBindingEvidence, error) {
	paths, err := filepath.Glob(filepath.Join(stateDir, "public", "fleet-*.binding.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) > 20000 {
		return nil, errors.New("renewal binding evidence exceeds the deployment bound")
	}
	result := map[[16]byte][]FleetBindingEvidence{}
	for _, path := range paths {
		raw, err := readValidatorEvidenceHistoricalFile(stateDir, filepath.ToSlash(filepath.Join("public", filepath.Base(path))), 1<<20)
		if err != nil {
			return nil, err
		}
		var binding FleetBindingEvidence
		if err := json.Unmarshal(raw, &binding); err != nil {
			return nil, err
		}
		if binding.Schema == fleetRefreshBindingEvidenceSchema {
			var refresh FleetRefreshBindingEvidence
			if err := json.Unmarshal(raw, &refresh); err != nil {
				return nil, err
			}
			binding.Schema = "urnetwork-fleet-binding-evidence-v1"
			binding.Generation = refresh.ReplacementGeneration
		}
		if binding.Schema != "urnetwork-fleet-binding-evidence-v1" {
			return nil, fmt.Errorf("unsupported retained binding schema in %s", filepath.Base(path))
		}
		client, ok := evidenceFixedHex(binding.ClientID, 16)
		if !ok {
			return nil, errors.New("retained renewal client identity is malformed")
		}
		var id [16]byte
		copy(id[:], client)
		result[id] = append(result[id], binding)
	}
	return result, nil
}

func readFleetRenewalLatestAt(ctx context.Context, manager *EvmTxManager, address common.Address, clients [][16]byte, block uint64) (map[[16]byte]fleetBindingVersionRead, error) {
	coordinator := stabi.NewSTCoordinator()
	calls := make([][]byte, 0, 2*len(clients))
	for _, client := range clients {
		calls = append(calls, coordinator.PackBindingVersionCount(client), coordinator.PackGetFleetBinding(client))
	}
	outputs, err := rawCoordinatorBatchCallAt(ctx, manager, address, calls, block)
	if err != nil {
		return nil, err
	}
	result := map[[16]byte]fleetBindingVersionRead{}
	for index, client := range clients {
		count, err := coordinator.UnpackBindingVersionCount(outputs[2*index])
		if err != nil || count == nil || !count.IsUint64() || count.Sign() == 0 {
			return nil, stateMismatchError(err, "renewal client %x has no retained generation", client)
		}
		record, err := coordinator.UnpackGetFleetBinding(outputs[2*index+1])
		if err != nil {
			return nil, err
		}
		result[client] = fleetBindingVersionRead{Count: count, Record: record}
	}
	return result, nil
}

func fleetRenewalRoleForHotkey(roles *RoleSecrets, hotkey [32]byte) (string, error) {
	label := ""
	for name, role := range roles.Substrate {
		if !strings.HasSuffix(name, "-hotkey") || role.PublicKeyHex != hex.EncodeToString(hotkey[:]) {
			continue
		}
		if label != "" {
			return "", errors.New("renewal hotkey has ambiguous retained custody")
		}
		label = name
	}
	if label == "" {
		return "", errors.New("renewal hotkey is not an existing deployment role")
	}
	return label, nil
}

func prepareFleetRenewal(cfg *ResolvedConfig, stateDir string, base *SetupPlan, roles *RoleSecrets, observation fleetRenewalObservation) (FleetRenewal, error) {
	renewal := observation.Renewal
	if renewal.ValidFromEpoch <= renewal.ObservedEpoch || renewal.ValidToEpoch < renewal.ValidFromEpoch || renewal.ValidToEpoch-renewal.ValidFromEpoch+1 > cfg.Policy.Binding.MaximumValidityEpochs {
		return renewal, errors.New("renewal validity must be a future window within the policy maximum")
	}
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		// Stable UR client membership is independent of the currently registered
		// fleet hotkey, which lifecycle takeover may have legitimately replaced.
		initial, _, _, err := fleetManifestForGeneration(cfg, stateDir, roles, fleet, 1)
		if err != nil {
			return renewal, err
		}
		first, ok := observation.Records[initial.Members[0].ClientID]
		if !ok {
			return renewal, fmt.Errorf("fleet %d has no current binding", fleet)
		}
		if first.Record.Generation == 0 || first.Record.Generation == ^uint64(0) {
			return renewal, errors.New("renewal generation is zero or overflows")
		}
		label, err := fleetRenewalRoleForHotkey(roles, first.Record.Hotkey)
		if err != nil {
			return renewal, fmt.Errorf("fleet %d: %w", fleet, err)
		}
		native, live := observation.Native[first.Record.Hotkey]
		coldkey, err := roleBytes32(roles, strings.TrimSuffix(label, "-hotkey")+"-coldkey")
		if err != nil {
			return renewal, err
		}
		if !live || native.UID != first.Record.Uid || native.Coldkey != fleetLifecycleHex(coldkey) {
			return renewal, fmt.Errorf("fleet %d live UID/coldkey differs from retained binding custody", fleet)
		}
		account, ok := observation.Accounts[first.Record.Hotkey]
		minimum, addOK := checkedAdd(base.NativeTransactionFeeLimitRao, base.LiveFacts.ExistentialDepositRao)
		if !ok || !addOK || uint64(account.Data.Free) < minimum || uint32(account.Nonce) == ^uint32(0) {
			return renewal, fmt.Errorf("fleet %d existing hotkey cannot cover fee plus keep-alive without funding", fleet)
		}
		initial.Hotkey, initial.Generation = first.Record.Hotkey, first.Record.Generation+1
		canonical, err := initial.Canonical()
		if err != nil {
			return renewal, err
		}
		manifest, err := protocol.ParseFleetManifest(canonical)
		if err != nil {
			return renewal, err
		}
		prepared := FleetRenewalFleet{Fleet: fleet, HotkeyRole: label, Coldkey: native.Coldkey, UID: native.UID, NativeNonce: uint32(account.Nonce), Manifest: canonical}
		hotkey, err := crv4.KeypairFromSeedHex(roles.Substrate[label].SeedHex)
		if err != nil {
			return renewal, err
		}
		for _, member := range manifest.Members {
			read, ok := observation.Records[member.ClientID]
			if !ok || read.Count == nil || !read.Count.IsUint64() || read.Count.Sign() == 0 || read.Record.Generation != first.Record.Generation || read.Record.Hotkey != manifest.Hotkey || read.Record.Uid != prepared.UID || read.Record.FleetId != manifest.FleetID || read.Record.ClientKey != member.ClientKey || read.Record.Cleaned || read.Record.CleanedAtEpoch != 0 {
				return renewal, fmt.Errorf("fleet %d member %x has divergent or cleaned current identity", fleet, member.ClientID)
			}
			priorManifest := *manifest
			priorManifest.Generation--
			var prior *FleetBindingEvidence
			for _, candidate := range observation.Evidence[member.ClientID] {
				if candidate.Generation != priorManifest.Generation {
					continue
				}
				binding, err := fleetRenewalBinding(priorManifest, member, candidate)
				if err != nil || read.Record.ValidToEpoch > candidate.ValidToEpoch || !fleetBindingRecordMatches(read.Record, binding, read.Record.ValidToEpoch, candidate.UID) {
					continue
				}
				if prior != nil && (prior.TransactionHash != candidate.TransactionHash || prior.BindingDigest != candidate.BindingDigest) {
					return renewal, errors.New("renewal predecessor evidence is ambiguous")
				}
				copy := candidate
				prior = &copy
			}
			if prior == nil {
				return renewal, fmt.Errorf("fleet %d member %x has no exact retained signed predecessor", fleet, member.ClientID)
			}
			priorRevocation, err := fleetRenewalObservedPriorRevocation(base, observation.JournalEntries, fleet, member, *prior, read.Record.ValidToEpoch, renewal.EVMHead.Number)
			if err != nil {
				return renewal, err
			}
			miner := 0
			for index := 1; index <= cfg.Config.Topology.ClientsPerHeadFleet; index++ {
				candidate := fleetMemberMinerIndex(cfg, fleet, index)
				if roles.Clients[fmt.Sprintf("miner-%d", candidate)].ClientIDHex == hex.EncodeToString(member.ClientID[:]) {
					miner = candidate
					break
				}
			}
			if miner == 0 {
				return renewal, errors.New("renewal client has no retained miner role")
			}
			seed, err := hex.DecodeString(roles.Clients[fmt.Sprintf("miner-%d", miner)].SeedHex)
			if err != nil || len(seed) != ed25519.SeedSize {
				return renewal, errors.New("renewal client seed is invalid")
			}
			binding, err := manifest.Binding(member, renewal.ValidFromEpoch, renewal.ValidToEpoch)
			if err != nil {
				return renewal, err
			}
			private := ed25519.NewKeyFromSeed(seed)
			clientSig, err := binding.SignClient(private)
			if err != nil {
				return renewal, err
			}
			digest, err := binding.Digest()
			if err != nil {
				return renewal, err
			}
			hotSig, err := hotkey.Sign(digest[:])
			if err != nil {
				return renewal, err
			}
			next := FleetBindingEvidence{Schema: "urnetwork-fleet-binding-evidence-v1", ClientID: fleetLifecycleHex16(member.ClientID), ClientKey: fleetLifecycleHex(member.ClientKey), FleetID: fleetLifecycleHex(manifest.FleetID), Hotkey: fleetLifecycleHex(manifest.Hotkey), Generation: manifest.Generation, ValidFromEpoch: renewal.ValidFromEpoch, ValidToEpoch: renewal.ValidToEpoch, CommitmentHash: fleetLifecycleHex(binding.CommitmentHash), BindingDigest: fleetLifecycleHex(digest), ClientSignature: "0x" + hex.EncodeToString(clientSig), HotkeySignature: "0x" + hex.EncodeToString(hotSig), UID: prepared.UID}
			preparedMember := FleetRenewalMember{Miner: miner, VersionCount: read.Count.Uint64(), Prior: *prior, PriorRevocation: priorRevocation, Binding: next}
			if read.Record.ValidToEpoch >= renewal.ValidFromEpoch {
				revoke := protocol.FleetRevoke{ChainID: manifest.ChainID, Netuid: manifest.Netuid, Coordinator: manifest.Coordinator, ClientID: member.ClientID, Generation: prior.Generation, EffectiveEpoch: renewal.ValidFromEpoch}
				sig, err := revoke.SignClient(private)
				if err != nil {
					return renewal, err
				}
				preparedMember.RevokeSignature = "0x" + hex.EncodeToString(sig)
			}
			prepared.Members = append(prepared.Members, preparedMember)
		}
		renewal.Fleets = append(renewal.Fleets, prepared)
	}
	return renewal, nil
}

func observeFleetRenewal(ctx context.Context, cfg *ResolvedConfig, stateDir string, base *SetupPlan, roles *RoleSecrets, entries []JournalEntry, o cliOptions) (fleetRenewalObservation, error) {
	result := fleetRenewalObservation{Records: map[[16]byte]fleetBindingVersionRead{}, Native: map[[32]byte]ExistingUIDFact{}, Accounts: map[[32]byte]subtensorAccountInfo{}, JournalEntries: entries}
	if len(entries) == 0 {
		return result, errors.New("renewal requires the retained deployment journal")
	}
	external, err := readFleetRenewalTransactionInput(o.RenewalTransactionEvidence)
	if err != nil {
		return result, err
	}
	queues, err := readFleetRenewalQueueTransactions(cfg, stateDir)
	if err != nil {
		return result, err
	}
	external = append(external, o.RenewalTransactions...)
	external = append(external, queues...)
	external, err = canonicalFleetRenewalTransactions(external)
	if err != nil {
		return result, err
	}
	exposure, err := fleetRenewalCampaignExposure(stateDir, base, entries, external)
	if err != nil {
		return result, err
	}
	evidence, err := readFleetRenewalBindingEvidence(stateDir)
	if err != nil {
		return result, err
	}
	result.Evidence = evidence
	native, err := DialSubstrateManager(cfg, stateDir, nil)
	if err != nil {
		return result, err
	}
	defer native.Close()
	manager, err := DialEvmTxManager(ctx, cfg, stateDir, nil, roles, "commitment-oracle")
	if err != nil {
		return result, err
	}
	defer manager.Close()
	coordinator := stabi.NewSTCoordinator()
	native, nativeHash, nativeNumber, err := native.finalizedManagerContext(ctx)
	if err != nil {
		return result, err
	}
	topology, err := readSubnetTopologyAt(native.chain, cfg.Netuid, nativeHash)
	if err != nil {
		return result, err
	}
	uids, err := readExistingUIDFactsAt(native.chain, cfg.Netuid, nativeHash, topology)
	if err != nil {
		return result, err
	}
	for _, uid := range uids {
		key, err := decodeHex32("renewal native hotkey", uid.Hotkey)
		if err != nil {
			return result, err
		}
		result.Native[key] = uid
	}
	head, err := finalizedEVMHead(ctx, manager.client)
	if err != nil {
		return result, err
	}
	oracle, err := readFleetRefreshOracleStateAt(ctx, manager, base.Deployment.CoordinatorProxy, coordinator, head.Number)
	if err != nil {
		return result, err
	}
	oracleRole, err := roles.EVMKey("commitment-oracle")
	if err != nil {
		return result, err
	}
	keeperRole, err := roles.EVMKey("keeper")
	if err != nil {
		return result, err
	}
	if !fleetRenewalOriginalOracleReady(oracle, common.HexToAddress(oracleRole.Address)) {
		return result, errors.New("renewal requires the retained original oracle with no pending reroute")
	}
	result.Renewal = FleetRenewal{Round: uint64(len(base.FleetRenewals) + 1), SourcePlanHash: base.PlanHash, JournalHash: entries[len(entries)-1].EntryHash, NativeHead: ChainHead{Number: nativeNumber, Hash: nativeHash.Hex()}, EVMHead: head, ObservedEpoch: oracle.CurrentEpoch, ValidFromEpoch: o.RenewalValidFrom, ValidToEpoch: o.RenewalValidTo, MaximumFeePerGasWei: o.RenewalFeePerGas, Oracle: oracle.Active, Keeper: common.HexToAddress(keeperRole.Address), CampaignLiabilityWei: exposure.Liability, TransactionEvidence: external}
	result.Renewal.EVMNonces, err = observeFleetRenewalNonces(ctx, manager, roles, head.Number)
	result.Renewal.BatchSize = fleetRenewalBatchSize
	result.Renewal.SupersededGasCoveredWei = exposure.SupersededCredit
	result.Renewal.MaximumInFlight = fleetRenewalMaximumInFlight
	if err != nil {
		return result, err
	}
	if err := validateFleetRenewalSignerNonceCoverage(roles, exposure, result.Renewal.EVMNonces, []common.Address{result.Renewal.Oracle, result.Renewal.Keeper}); err != nil {
		return result, err
	}
	if result.Renewal.MaximumFeePerGasWei == 0 {
		result.Renewal.MaximumFeePerGasWei = base.MaximumEVMFeePerGasWei
	}
	if result.Renewal.ValidToEpoch == 0 {
		result.Renewal.ValidToEpoch, err = fleetBindingValidityEnd(result.Renewal.ValidFromEpoch, cfg.Policy.Binding.MaximumValidityEpochs)
		if err != nil {
			return result, err
		}
	}
	for _, a := range base.Actions {
		if a.ID == "campaign.evm-gas-reserve" {
			result.Renewal.CampaignReserveBeforeWei = a.Spend.EVMGasWei
		}
	}
	result.Renewal.OracleNonce, err = manager.client.PendingNonceAt(ctx, result.Renewal.Oracle)
	if err != nil {
		return result, err
	}
	result.Renewal.KeeperNonce, err = manager.client.PendingNonceAt(ctx, result.Renewal.Keeper)
	if err != nil {
		return result, err
	}
	for _, pair := range []struct {
		address common.Address
		nonce   uint64
	}{{result.Renewal.Oracle, result.Renewal.OracleNonce}, {result.Renewal.Keeper, result.Renewal.KeeperNonce}} {
		finalized, err := manager.client.NonceAt(ctx, pair.address, new(big.Int).SetUint64(head.Number))
		if err != nil || finalized != pair.nonce {
			return result, stateMismatchError(err, "renewal signer %s has unfinalized nonce activity", pair.address)
		}
		if err := validateFleetRenewalUnusedSignerNonce(exposure, pair.address, pair.nonce); err != nil {
			return result, err
		}
	}
	var clients [][16]byte
	for fleet := 1; fleet <= cfg.Config.Topology.fleetCandidates(); fleet++ {
		manifest, _, _, err := fleetManifestForGeneration(cfg, stateDir, roles, fleet, 1)
		if err != nil {
			return result, err
		}
		for _, member := range manifest.Members {
			clients = append(clients, member.ClientID)
		}
	}
	result.Records, err = readFleetRenewalLatestAt(ctx, manager, base.Deployment.CoordinatorProxy, clients, head.Number)
	if err != nil {
		return result, err
	}
	accountKeys := []nativeTypes.StorageKey{}
	hotkeys := [][32]byte{}
	seen := map[[32]byte]bool{}
	for _, client := range clients {
		hotkey := result.Records[client].Record.Hotkey
		if seen[hotkey] {
			continue
		}
		seen[hotkey] = true
		key, err := nativeTypes.CreateStorageKey(native.chain.Meta, "System", "Account", hotkey[:])
		if err != nil {
			return result, err
		}
		accountKeys = append(accountKeys, key)
		hotkeys = append(hotkeys, hotkey)
	}
	accounts, err := queryStorageAtExact(native.chain, accountKeys, nativeHash)
	if err != nil {
		return result, err
	}
	for index, key := range accountKeys {
		var account subtensorAccountInfo
		if err := decodeRequiredStorageQueryValue(accounts, key, "System.Account", &account); err != nil {
			return result, err
		}
		result.Accounts[hotkeys[index]] = account
		label, err := fleetRenewalRoleForHotkey(roles, hotkeys[index])
		if err != nil {
			return result, err
		}
		for _, entry := range entries {
			if entry.TransactionHash == "" || entry.Signer != roles.Substrate[label].SS58 || entry.Nonce == "" {
				continue
			}
			nonce, err := strconv.ParseUint(entry.Nonce, 10, 32)
			if err != nil {
				return result, err
			}
			if nonce >= uint64(account.Nonce) {
				return result, fmt.Errorf("renewal native signer %s already owns retained signed nonce %d at or above current nonce %d", label, nonce, account.Nonce)
			}
		}
	}
	return result, nil
}

func bindFleetRenewalRuntimeIdentity(cfg *ResolvedConfig, plan *SetupPlan) (*SetupPlan, error) {
	if cfg == nil || plan == nil {
		return nil, errors.New("renewal runtime identity is unavailable")
	}
	if cfg.PolicyHash != plan.PolicyHash || cfg.ownedRPCAuthority != plan.OwnedRPCAuthority {
		return nil, errors.New("renewal runtime identity cannot change policy or owned Rpc authority")
	}
	hash, err := resolvedInputsHash(cfg)
	if err != nil {
		return nil, err
	}
	return rebindFleetRenewalRuntimePlan(plan, cfg.ConfigHash, hash)
}

func buildFleetRenewalPlan(ctx context.Context, cfg *ResolvedConfig, stateDir string, o cliOptions) (*SetupPlan, error) {
	cfg, err := prepareProvisionalRetainedReader(ctx, cfg, stateDir, "fleet-renew", o)
	if err != nil {
		return nil, err
	}
	base, err := loadFleetRenewalBase(cfg, stateDir)
	if err != nil {
		return nil, fmt.Errorf("renewal requires an admitted current setup plan: %w", err)
	}
	roles, err := loadExistingProvisionalRoles(cfg, stateDir)
	if err != nil {
		return nil, err
	}
	entries, err := readJournalEntries(stateDir)
	if err != nil {
		return nil, err
	}
	if err := validateFleetLifecycleRenewalPending(stateDir, base, entries); err != nil {
		return nil, err
	}
	observation, err := observeFleetRenewal(ctx, cfg, stateDir, base, roles, entries, o)
	if err != nil {
		return nil, err
	}
	renewal, err := prepareFleetRenewal(cfg, stateDir, base, roles, observation)
	if err != nil {
		return nil, err
	}
	actions, err := fleetRenewalActions(base, renewal)
	if err != nil {
		return nil, err
	}
	manager, err := DialEvmTxManager(ctx, cfg, stateDir, nil, roles, "commitment-oracle")
	if err != nil {
		return nil, err
	}
	defer manager.Close()
	if err := verifyFleetRenewalDeadline(ctx, manager, base.Deployment.CoordinatorProxy, renewal, "", actions, nil); err != nil {
		return nil, err
	}
	spend, err := maximumActionSpend(actions)
	if err != nil {
		return nil, err
	}
	renewal.AllowanceExtensionWei = spend.EVMGasWei
	renewal.AllowanceTotalTAORao = cfg.MaximumTAORao
	renewal.AllowanceTotalEVMWei = cfg.MaximumEVMGasWei
	plan, err := appendFleetRenewalPlan(base, renewal)
	if err != nil {
		return nil, err
	}
	return bindFleetRenewalRuntimeIdentity(cfg, plan)
}

// Fleet renewal is the one successor which must outlive the release which
// installed its fleets.  The source plan remains byte-authenticated and its
// immutable chain, policy, RPC route, and public custody identities must
// still agree with this invocation.  A changed release-lock or operational
// configuration fingerprint alone cannot force an expired binding generation
// to be abandoned.  Current runtime approval and every live predecessor are
// checked again before planning and before the first write.
func loadFleetRenewalBase(cfg *ResolvedConfig, stateDir string) (*SetupPlan, error) {
	base, err := loadPersistedPlan(cfg, stateDir)
	if err == nil {
		return base, nil
	}
	if !errors.Is(err, errPersistedPlanIdentityMismatch) {
		return nil, err
	}
	base, readErr := readPersistedPlan(stateDir)
	if readErr != nil {
		return nil, readErr
	}
	if cfg == nil || cfg.Config == nil || cfg.Public == nil ||
		base.Schema != currentSetupPlanSchema || base.Release != "1.0" ||
		base.DeploymentID != cfg.Config.Deployment.DeploymentID ||
		base.ChainID != testnetChainID || base.GenesisHash != testnetGenesis ||
		base.Netuid != cfg.Netuid || base.PolicyHash != cfg.PolicyHash ||
		base.OwnedRPCAuthority != cfg.ownedRPCAuthority {
		return nil, errors.New("retained renewal source differs in immutable testnet identity")
	}
	if err := validateRuntimeConfigIdentityPlan(cfg, base); err != nil {
		return nil, err
	}
	roles, err := derivePublicRoles(cfg)
	if err != nil {
		return nil, err
	}
	roleHash, err := canonicalHashHex(roles)
	if err != nil {
		return nil, err
	}
	baseRoleHash, err := canonicalHashHex(base.Roles)
	if err != nil || roleHash != baseRoleHash {
		return nil, errors.New("retained renewal source differs in public custody identity")
	}
	if err := validatePlanBudget(base); err != nil {
		return nil, fmt.Errorf("retained renewal source budget: %w", err)
	}
	return base, nil
}

// Use both deterministic public roles and the unchanged key store on apply.
func validateFleetRenewalCustody(cfg *ResolvedConfig, stateDir string, roles *RoleSecrets, renewal FleetRenewal) error {
	if len(renewal.Fleets) != cfg.Config.Topology.fleetCandidates() || renewal.ValidToEpoch-renewal.ValidFromEpoch+1 > cfg.Policy.Binding.MaximumValidityEpochs {
		return errors.New("renewal fleet count or validity differs from configured policy")
	}
	for _, fleet := range renewal.Fleets {
		miners := make([]int, len(fleet.Members))
		for i, member := range fleet.Members {
			miners[i] = member.Miner
		}
		parsed, err := protocol.ParseFleetManifest(fleet.Manifest)
		if err != nil {
			return err
		}
		manifest, canonical, _, err := fleetManifestForMembers(cfg, stateDir, roles, derive32(cfg, fmt.Sprintf("fleet-id/%d", fleet.Fleet)), fleet.HotkeyRole, parsed.Generation, miners)
		if err != nil {
			return err
		}
		want, _ := parsed.Canonical()
		if string(canonical) != string(want) {
			return errors.New("renewal manifest changes existing client or fleet custody")
		}
		for _, member := range fleet.Members {
			found := false
			for index := 1; index <= cfg.Config.Topology.ClientsPerHeadFleet; index++ {
				found = found || member.Miner == fleetMemberMinerIndex(cfg, fleet.Fleet, index)
			}
			if !found {
				return errors.New("renewal redirects a client to a different fleet")
			}
		}
		label, err := fleetRenewalRoleForHotkey(roles, manifest.Hotkey)
		if err != nil || label != fleet.HotkeyRole {
			return errors.New("renewal hotkey role is ambiguous")
		}
	}
	oracle, _ := roles.EVMKey("commitment-oracle")
	keeper, _ := roles.EVMKey("keeper")
	if oracle == nil || keeper == nil || renewal.Oracle != common.HexToAddress(oracle.Address) || renewal.Keeper != common.HexToAddress(keeper.Address) {
		return errors.New("renewal EVM roles differ from retained custody")
	}
	return nil
}
