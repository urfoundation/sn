//go:build linux || darwin

// Decision observations come from one canonical finalized coordinator state.
// They contain chain facts, not proof replay, historical API-key authority or
// permission to mutate head EMA or submit a prepared native transaction.
package validator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"slices"

	"github.com/ethereum/go-ethereum/common"
	"github.com/urfoundation/sn/v2026/protocol"
	"github.com/urfoundation/sn/v2026/stabi"
	"github.com/urnetwork/connect/v2026"
)

// Provider membership is supplied by independently replayed immutable inputs.
// The whole configured operator census, including empty providers, is required.
type releaseDecisionChainV2OperatorQuery struct {
	noID        uint64
	providerIDs []connect.Id
}

// No candidate binding, pool, deposit or current UID is accepted as a query.
// These explicit bounds are the enclosing release owner's existing allowance.
type releaseDecisionChainV2Query struct {
	domain          protocol.ValidatorEvidenceDomain
	boundary        AttemptBoundary
	policy          protocol.Policy
	operators       []releaseDecisionChainV2OperatorQuery
	maxOperators    uint64
	maxProviders    uint64
	maxControlBytes uint64
}

// Uint256 values retain exact owned integers; no float or candidate audit is
// used to reconstruct conviction before this epoch's two separate additions.
type releaseDecisionChainV2Operator struct {
	noID             uint64
	version          stabi.STCoordinatorOperatorVersion
	deposit          *big.Int
	convictionAdded  *big.Int
	conviction       *big.Int
	convictionBefore *big.Int
	sourceVersion    stabi.STCoordinatorOperatorVersion
	commitment       stabi.RootCommitmentsOutput
}

// LocalClientKey stays zero until a separate real key observation is owned.
// A pinned coordinator key alone does not prove a past API lookup's result.
type releaseDecisionChainV2Observation struct {
	boundary         AttemptBoundary
	policy           stabi.STCoordinatorPolicySnapshot
	epochStart       uint64
	artifactDeadline uint64
	sourceEpoch      uint64
	sourceStart      uint64
	sourceEnd        uint64
	sourceStartHash  [32]byte
	sourceEndHash    [32]byte
	hotkeyUIDs       map[[32]byte]uint16
	bindings         []ReleaseBindingMeasurement
	pools            []ReleasePoolMeasurement
	operators        []releaseDecisionChainV2Operator
}

// Only fixed-width coordinator views are used. Canonical re-encoding rejects
// trailing words and ABI aliases before a decoded value becomes an observation.
func canonicalReleaseDecisionV2View(method string, encoded []byte) error {
	if len(encoded) == 0 || len(encoded) > 14*32 {
		return errors.New("decision coordinator view exceeds its fixed ABI shape")
	}
	contractABI, err := stabi.STCoordinatorMetaData.ParseABI()
	if err != nil {
		return err
	}
	view, exists := contractABI.Methods[method]
	if !exists {
		return errors.New("decision coordinator view is unknown")
	}
	values, err := view.Outputs.Unpack(encoded)
	if err != nil {
		return err
	}
	canonical, err := view.Outputs.Pack(values...)
	if err != nil || !bytes.Equal(encoded, canonical) {
		return errors.Join(fmt.Errorf("decision %s view is noncanonical", method), err)
	}
	return nil
}

// The live collector and historical observer share the actual canonical ABI
// reader. Neither can reinterpret an incomplete result as an inactive binding.
// Callers reserve their provider arrays before invoking this bounded reader.
func (self *ChainClient) readReleaseProviderBindingsV2Context(ctx context.Context, block uint64, hash [32]byte, providerIDs [][16]byte, epoch uint64, maximum uint64) (result []stabi.BindingAtOutput, resultErr error) {
	if ctx == nil || self == nil || self.coordinator == nil || maximum == 0 || uint64(len(providerIDs)) > maximum {
		return nil, errors.New("decision provider binding census or bound is invalid")
	}
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	if err := errors.Join(ctx.Err(), self.requireRelease()); err != nil {
		return nil, err
	}
	coordinator, address := self.coordinator, self.contractAddr
	// The caller retains lifecycle ownership; this private synchronous borrow
	// prevents a header-read callback from replacing the later RPC transport.
	chain := &ChainClient{client: self.client, coordinator: coordinator, contractAddr: address, release: true}
	for index, id := range providerIDs {
		if id == ([16]byte{}) || index > 0 && bytes.Compare(providerIDs[index-1][:], id[:]) >= 0 {
			return nil, errors.New("decision provider binding census is not canonical")
		}
	}
	// Own the original ordered values before the first external RPC callback.
	providerIDs = slices.Clone(providerIDs)
	calls := make([]chainBatchCall, len(providerIDs))
	for index, id := range providerIDs {
		calls[index] = chainBatchCall{address: address, calldata: coordinator.PackBindingAt(id, new(big.Int).SetUint64(epoch))}
	}
	encoded, err := chain.batchCallsAtHashContext(ctx, block, hash, calls)
	if err != nil {
		return nil, err
	}
	if len(encoded) != len(providerIDs) {
		return nil, errors.New("decision provider binding response census differs")
	}
	result = make([]stabi.BindingAtOutput, len(encoded))
	for index, raw := range encoded {
		if err := canonicalReleaseDecisionV2View("bindingAt", raw); err != nil {
			return nil, err
		}
		binding, err := coordinator.UnpackBindingAt(raw)
		if err != nil {
			return nil, err
		}
		result[index] = binding
	}
	return result, ctx.Err()
}

// Complete native-UID coverage is read through the EVM metagraph at the same
// decision hash. Count-dependent control storage is reserved before any member
// calldata or map allocation; the native wire ceiling is not a guessed census.
func (self *ChainClient) readReleaseDecisionHotkeysV2Context(ctx context.Context, block uint64, hash [32]byte, netuid uint16, budget *releaseHeadV2Budget) (map[[32]byte]uint16, error) {
	selector := evmSelector("getUidCount(uint16)")
	argNetuid := evmUint16Word(netuid)
	encoded, err := self.ethCallAtHashContext(ctx, metagraphAddress, append(selector[:], argNetuid[:]...), block, hash)
	if err != nil {
		return nil, err
	}
	if len(encoded) != 32 || !bytes.Equal(encoded[:30], make([]byte, 30)) {
		return nil, errors.New("decision metagraph count is not canonical uint16")
	}
	count := uint64(encoded[30])<<8 | uint64(encoded[31])
	if count == 0 || count > uint64(releaseNativeValidatorMaximumUIDs) {
		return nil, errors.New("decision metagraph count is empty or exceeds the native bound")
	}
	width := uint64(reflect.TypeFor[chainBatchCall]().Size()) + 68 + uint64(reflect.TypeFor[[]byte]().Size()) + 32 + 32 + 2
	if err := budget.charge(count, width); err != nil {
		return nil, err
	}
	calls := make([]chainBatchCall, int(count))
	for index := range calls {
		selector := evmSelector("getHotkey(uint16,uint16)")
		argUID := evmUint16Word(uint16(index))
		calldata := make([]byte, 0, 68)
		calldata = append(calldata, selector[:]...)
		calldata = append(calldata, argNetuid[:]...)
		calldata = append(calldata, argUID[:]...)
		calls[index] = chainBatchCall{address: metagraphAddress, calldata: calldata}
	}
	outputs, err := self.batchCallsAtHashContext(ctx, block, hash, calls)
	if err != nil || len(outputs) != len(calls) {
		return nil, errors.Join(errors.New("decision metagraph member census differs"), err)
	}
	keys := make(map[[32]byte]uint16, len(outputs))
	for index, output := range outputs {
		if len(output) != 32 {
			return nil, errors.New("decision metagraph hotkey is not exactly 32 bytes")
		}
		var key [32]byte
		copy(key[:], output)
		if _, found := keys[key]; found || key == ([32]byte{}) {
			return nil, errors.New("decision metagraph repeats or omits a hotkey")
		}
		keys[key] = uint16(index)
	}
	return keys, ctx.Err()
}

// All retained request values and output payload capacity precede the first
// RPC. Private map ownership also rejects a provider shared between operators.
func ownReleaseDecisionChainV2Query(ctx context.Context, query releaseDecisionChainV2Query) (releaseDecisionChainV2Query, releaseHeadV2Budget, error) {
	budget := releaseHeadV2Budget{limit: query.maxControlBytes}
	if ctx == nil || query.maxOperators == 0 || query.maxProviders == 0 || query.maxControlBytes == 0 || query.maxControlBytes > maxReleaseMeasurementArtifactBytes || len(query.operators) == 0 || uint64(len(query.operators)) > query.maxOperators {
		return query, budget, errors.New("decision chain census or finite control allowance is invalid")
	}
	if err := errors.Join(ctx.Err(), query.domain.Validate()); err != nil {
		return query, budget, err
	}
	if query.boundary.EVMBlock == 0 {
		return query, budget, errors.New("decision chain boundary is absent")
	}
	if _, err := canonicalAttemptHex32("decision EVM hash", query.boundary.EVMBlockHash, false); err != nil {
		return query, budget, err
	}
	remaining := query.maxControlBytes
	if err := releaseMeasurementV2ControlStorage(ctx, reflect.ValueOf(query.policy), &remaining); err != nil {
		return query, budget, err
	}
	budget.used = query.maxControlBytes - remaining
	width := uint64(reflect.TypeFor[releaseDecisionChainV2OperatorQuery]().Size()) + uint64(reflect.TypeFor[releaseDecisionChainV2Operator]().Size()) + 4*(uint64(reflect.TypeFor[big.Int]().Size())+32) + uint64(reflect.TypeFor[ReleasePoolMeasurement]().Size()) + 66 + 8 + uint64(reflect.TypeFor[int]().Size()) + 8 + 1 + 2 + 1
	if err := budget.charge(uint64(len(query.operators)), width); err != nil {
		return query, budget, err
	}
	if err := budget.charge(1, uint64(reflect.TypeFor[releaseDecisionChainV2Observation]().Size())+uint64(reflect.TypeFor[releaseDecisionChainV2Query]().Size())+3*uint64(reflect.TypeFor[ChainClient]().Size())+66+4*(uint64(reflect.TypeFor[big.Int]().Size())+32)); err != nil {
		return query, budget, err
	}
	for index, operator := range query.operators {
		if operator.noID == 0 || index > 0 && query.operators[index-1].noID >= operator.noID || uint64(len(operator.providerIDs)) > query.maxProviders {
			return query, budget, errors.New("decision configured operator/provider census is invalid")
		}
		// Owned ids, cross-operator membership, binding ABI results, generated
		// canonical observations, and the actual bounded RPC request payload.
		width := uint64(4*16+8) + uint64(reflect.TypeFor[stabi.BindingAtOutput]().Size()) + uint64(reflect.TypeFor[ReleaseBindingMeasurement]().Size()) + 36 + 5*66 + uint64(reflect.TypeFor[chainBatchCall]().Size()) + 68
		if err := budget.charge(uint64(len(operator.providerIDs)), width); err != nil {
			return query, budget, err
		}
	}
	policyHash, err := query.policy.Hash()
	if err != nil || policyHash != query.domain.PolicyHash {
		return query, budget, errors.Join(errors.New("decision policy differs from its independent deployment domain"), err)
	}
	query.policy.Deposit.Tiers = slices.Clone(query.policy.Deposit.Tiers)
	query.operators = slices.Clone(query.operators)
	providers := map[connect.Id]uint64{}
	for index, operator := range query.operators {
		query.operators[index].providerIDs = slices.Clone(operator.providerIDs)
		for providerIndex, id := range operator.providerIDs {
			if id == (connect.Id{}) || providerIndex > 0 && !operator.providerIDs[providerIndex-1].LessThan(id) {
				return query, budget, errors.New("decision provider membership is not canonical")
			}
			if _, found := providers[id]; found {
				return query, budget, errors.New("decision provider belongs to multiple operator inputs")
			}
			providers[id] = operator.noID
		}
	}
	return query, budget, ctx.Err()
}

// Hash pinning applies to every current/source-epoch view, not just the first
// snapshot read. The source boundary headers are separately rechecked before
// publishing owned observations. Actual proof/intent authority remains outer.
func (self *ChainClient) readReleaseDecisionChainV2Context(ctx context.Context, query releaseDecisionChainV2Query) (result *releaseDecisionChainV2Observation, resultErr error) {
	query, budget, err := ownReleaseDecisionChainV2Query(ctx, query)
	if err != nil {
		return nil, err
	}
	return self.readOwnedReleaseDecisionChainV2Context(ctx, query, budget)
}

// Keeps the actual transport and deployment routing fixed before another
// native or EVM reader callback. This borrow never acquires Close authority.
func (self *ChainClient) ownReleaseDecisionChainV2Client(domain protocol.ValidatorEvidenceDomain) (*ChainClient, error) {
	if self == nil || self.client == nil || self.coordinator == nil || self.chainId == nil || !self.release || self.chainId.Cmp(new(big.Int).SetUint64(domain.ChainID)) != 0 || self.contractAddr != common.Address(domain.Coordinator) {
		return nil, errors.New("decision chain owner differs from its independent deployment domain")
	}
	return &ChainClient{client: self.client, coordinator: self.coordinator, chainId: new(big.Int).Set(self.chainId), contractAddr: self.contractAddr, release: true}, nil
}

// Both the EVM-only and native-plus-EVM entrypoints finish the same owned
// request. Reusing a plan within this synchronous call is not cached authority.
func (self *ChainClient) readOwnedReleaseDecisionChainV2Context(ctx context.Context, query releaseDecisionChainV2Query, budget releaseHeadV2Budget) (result *releaseDecisionChainV2Observation, resultErr error) {
	defer func() {
		resultErr = errors.Join(resultErr, ctx.Err())
		if resultErr != nil {
			result = nil
		}
	}()
	chain, err := self.ownReleaseDecisionChainV2Client(query.domain)
	if err != nil {
		return nil, err
	}
	block, hash := query.boundary.EVMBlock, common.HexToHash(query.boundary.EVMBlockHash)
	finalized, finalizedHash, err := chain.FinalizedBlockContext(ctx)
	if err != nil || finalized < block || finalized == block && finalizedHash != hash {
		return nil, errors.Join(errors.New("decision EVM boundary is not finalized"), err)
	}
	read := func(method string, calldata []byte) ([]byte, error) {
		encoded, err := chain.ethCallAtHashContext(ctx, chain.contractAddr, calldata, block, hash)
		if err != nil {
			return nil, err
		}
		if err := canonicalReleaseDecisionV2View(method, encoded); err != nil {
			return nil, err
		}
		return encoded, nil
	}
	readUint := func(method string, calldata []byte) (*big.Int, error) {
		encoded, err := read(method, calldata)
		if err != nil {
			return nil, err
		}
		if len(encoded) != 32 {
			return nil, errors.New("decision integer view is not one uint256 word")
		}
		return new(big.Int).SetBytes(encoded), nil
	}
	encoded, err := read("netuid", chain.coordinator.PackNetuid())
	if err != nil {
		return nil, err
	}
	netuid, err := chain.coordinator.UnpackNetuid(encoded)
	if err != nil || netuid != query.domain.Netuid {
		return nil, errors.Join(errors.New("decision coordinator subnet differs"), err)
	}
	epoch, err := readUint("currentEpoch", chain.coordinator.PackCurrentEpoch())
	if err != nil || !epoch.IsUint64() || epoch.Uint64() != query.boundary.SettlementEpoch {
		return nil, errors.Join(errors.New("decision coordinator epoch differs"), err)
	}
	encoded, err = read("policyAt", chain.coordinator.PackPolicyAt(epoch))
	if err != nil {
		return nil, err
	}
	policy, err := chain.coordinator.UnpackPolicyAt(encoded)
	if err != nil {
		return nil, err
	}
	if err := validateReleaseDecisionChainV2Policy(query, policy); err != nil {
		return nil, err
	}
	start, err := readUint("epochStartBlock", chain.coordinator.PackEpochStartBlock(epoch))
	if err != nil || !start.IsUint64() || start.Sign() == 0 || policy.EpochBlocks > ^uint64(0)-start.Uint64() || block < start.Uint64() || block >= start.Uint64()+policy.EpochBlocks {
		return nil, errors.Join(errors.New("decision epoch boundary differs from its pinned policy window"), err)
	}
	wantStart := new(big.Int).Mul(new(big.Int).SetUint64(query.boundary.SettlementEpoch-policy.EffectiveEpoch), new(big.Int).SetUint64(policy.EpochBlocks))
	wantStart.Add(wantStart, new(big.Int).SetUint64(policy.EffectiveBlock))
	if start.Cmp(wantStart) != 0 || policy.RootCommitWindowBlocks > ^uint64(0)-start.Uint64() {
		return nil, errors.New("decision epoch start or artifact deadline differs")
	}
	count, err := readUint("operatorCount", chain.coordinator.PackOperatorCount())
	if err != nil || !count.IsUint64() || count.Uint64() > query.maxOperators || count.Uint64() != uint64(len(query.operators)) {
		return nil, errors.Join(errors.New("decision on-chain operator census differs from complete configuration"), err)
	}
	indices := make(map[uint64]int, len(query.operators))
	for index, operator := range query.operators {
		indices[operator.noID] = index
	}
	seen := make(map[uint64]bool, len(query.operators))
	for index := range query.operators {
		id, err := readUint("operatorIdAt", chain.coordinator.PackOperatorIdAt(new(big.Int).SetUint64(uint64(index))))
		if err != nil || !id.IsUint64() {
			return nil, errors.Join(errors.New("decision registry operator id is outside uint64"), err)
		}
		if _, found := indices[id.Uint64()]; !found || seen[id.Uint64()] {
			return nil, errors.New("decision registry substitutes or repeats a configured operator")
		}
		seen[id.Uint64()] = true
	}
	observed := &releaseDecisionChainV2Observation{boundary: query.boundary, policy: policy, epochStart: start.Uint64(), artifactDeadline: start.Uint64() + policy.RootCommitWindowBlocks, operators: make([]releaseDecisionChainV2Operator, len(query.operators)), bindings: []ReleaseBindingMeasurement{}, pools: []ReleasePoolMeasurement{}}
	observed.hotkeyUIDs, err = chain.readReleaseDecisionHotkeysV2Context(ctx, block, hash, query.domain.Netuid, &budget)
	if err != nil {
		return nil, err
	}
	poolUIDs := map[uint16]bool{}
	for index, operator := range query.operators {
		id := new(big.Int).SetUint64(operator.noID)
		encoded, err := read("operatorAt", chain.coordinator.PackOperatorAt(id, epoch))
		if err != nil {
			return nil, err
		}
		version, err := chain.coordinator.UnpackOperatorAt(encoded)
		if err != nil || version.EffectiveEpoch > query.boundary.SettlementEpoch {
			return nil, errors.Join(errors.New("decision operator version is from a later epoch"), err)
		}
		deposit, err := readUint("epochDeposits", chain.coordinator.PackEpochDeposits(epoch, id))
		if err != nil {
			return nil, err
		}
		added, err := readUint("epochConvictionAdded", chain.coordinator.PackEpochConvictionAdded(epoch, id))
		if err != nil {
			return nil, err
		}
		conviction, err := readUint("cumulativeConviction", chain.coordinator.PackCumulativeConviction(id))
		if err != nil {
			return nil, err
		}
		before := new(big.Int).Sub(new(big.Int).Set(conviction), deposit)
		before.Sub(before, added)
		if before.Sign() < 0 {
			return nil, errors.New("decision conviction before this epoch underflows")
		}
		observed.operators[index] = releaseDecisionChainV2Operator{noID: operator.noID, version: version, deposit: deposit, convictionAdded: added, conviction: conviction, convictionBefore: before}
		if version.Active {
			uid, found := observed.hotkeyUIDs[version.PoolHotkey]
			if !found || poolUIDs[uid] || version.Coldkey == ([32]byte{}) || version.DepositHotkey == ([32]byte{}) || version.DepositSigner == (common.Address{}) || version.RootSigner == (common.Address{}) {
				return nil, errors.New("decision active operator identity or unique pool UID is absent")
			}
			poolUIDs[uid] = true
			observed.pools = append(observed.pools, ReleasePoolMeasurement{NoID: operator.noID, UID: uid, PoolHotkey: releaseHex32(version.PoolHotkey)})
		}
		ids := make([][16]byte, len(operator.providerIDs))
		for index, id := range operator.providerIDs {
			ids[index] = [16]byte(id)
		}
		bindings, err := chain.readReleaseProviderBindingsV2Context(ctx, block, hash, ids, query.boundary.SettlementEpoch, query.maxProviders)
		if err != nil {
			return nil, err
		}
		for index, binding := range bindings {
			row := releaseDecisionBindingV2Observation(operator.noID, operator.providerIDs[index], binding, observed.hotkeyUIDs)
			if row.Active && (binding.Record.FleetId == ([32]byte{}) || binding.Record.Hotkey == ([32]byte{}) || binding.Record.ClientKey == ([32]byte{}) || binding.Record.Generation == 0 || binding.Record.Cleaned || binding.Record.CleanedAtEpoch != 0 || binding.Record.ValidFromEpoch > query.boundary.SettlementEpoch || binding.Record.ValidToEpoch < query.boundary.SettlementEpoch || query.policy.Binding.CommitmentsRequired && binding.Record.CommitmentHash == ([32]byte{})) {
				return nil, errors.New("decision active binding identity is incomplete or outside its epoch")
			}
			observed.bindings = append(observed.bindings, row)
		}
	}
	if len(observed.pools) < query.policy.Safety.MinimumHealthyNOCount {
		return nil, errors.New("decision active operator census is below the policy minimum")
	}
	if err := readReleaseDecisionChainV2Source(ctx, chain, query, observed, read, readUint); err != nil {
		return nil, err
	}
	// A final real canonical eth_call prevents a later header change from
	// being hidden behind the immutable hash-to-number pairing cache.
	rechecked, err := readUint("currentEpoch", chain.coordinator.PackCurrentEpoch())
	if err != nil || rechecked.Cmp(epoch) != 0 {
		return nil, errors.Join(errors.New("decision epoch changed during complete observation"), err)
	}
	finalBlock, finalHash, err := chain.FinalizedBlockContext(ctx)
	if err != nil || finalBlock < finalized || finalBlock == finalized && finalHash != finalizedHash {
		return nil, errors.Join(errors.New("decision finality regressed during complete observation"), err)
	}
	return observed, ctx.Err()
}

// Current and historical callers share exactly the coordinator/native fields.
// A live API caller must separately fill and compare its actual client key.
func releaseDecisionBindingV2Observation(noID uint64, clientID connect.Id, binding stabi.BindingAtOutput, hotkeys map[[32]byte]uint16) ReleaseBindingMeasurement {
	row := ReleaseBindingMeasurement{NoID: noID, ClientID: clientID.String(), Active: binding.Active, FleetID: releaseHex32(binding.Record.FleetId), Hotkey: releaseHex32(binding.Record.Hotkey), ClientKey: releaseHex32(binding.Record.ClientKey), LocalClientKey: releaseHex32([32]byte{}), CommitmentHash: releaseHex32(binding.Record.CommitmentHash), Generation: binding.Record.Generation, ValidFromEpoch: binding.Record.ValidFromEpoch, ValidToEpoch: binding.Record.ValidToEpoch, CleanedAtEpoch: binding.Record.CleanedAtEpoch, RecordUID: binding.Record.Uid, Cleaned: binding.Record.Cleaned}
	if binding.Active {
		row.LiveUID, row.LiveUIDFound = hotkeys[binding.Record.Hotkey]
	}
	return row
}
