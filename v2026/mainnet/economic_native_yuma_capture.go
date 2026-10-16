// Each allocation witness is decoded from original Wasm memory at independently
// reviewed callsites. A complete UID, stake graph, weights and bonds census is
// required; an omitted row is never interpreted as an observed empty row.
package main

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
)

const nativeYumaSchema = "urnetwork-original-yuma-allocation-witness-v1"
const nativeYumaLayout = "original-integer-stake-graph+uid-ordered-inputs+legacy-or-yuma3-sparse+fixed32-taylor31+complete-miner-and-validator-u64-v1"

// The original signed execution/producer policy binds the source family and
// memory-layout review separately from any block's calculated amounts.
type nativeYumaPolicy struct {
	Schema              string              `json:"schema"`
	LayoutSha256        string              `json:"layout_sha256"`
	ReviewSha256        string              `json:"independent_complete_algorithm_review_sha256"`
	MaximumWitnessBytes uint64              `json:"maximum_retained_witness_bytes"`
	HotBlockReserve     uint64              `json:"forecast_hot_original_blocks"`
	MaximumEdges        uint64              `json:"maximum_edges"`
	MaximumOperations   uint64              `json:"maximum_arithmetic_operations"`
	Workload            *nativeYumaWorkload `json:"populated_workload,omitempty"`
}

func (self *nativeYumaPolicy) validate() error {
	if self == nil {
		return nil
	}
	maximumEdges, maximumOperations := uint64(262144), uint64(64000000)
	if self.Workload != nil {
		maximumEdges, maximumOperations = nativeYumaPopulatedMaximumEdges, nativeYumaPopulatedMaximumOperations
	}
	if self.Schema != nativeYumaSchema || self.LayoutSha256 != monitorReadDigest([]byte(nativeYumaLayout)) || !planSha256(self.ReviewSha256) || self.MaximumEdges == 0 || self.MaximumEdges > maximumEdges || self.MaximumOperations == 0 || self.MaximumOperations > maximumOperations || self.MaximumWitnessBytes < 1024 || self.MaximumWitnessBytes > 32*1024*1024 || self.HotBlockReserve == 0 || self.HotBlockReserve > 64 {
		return errors.New("native Yuma lacks independent complete source/layout/finite work authority")
	}
	return self.validateWorkload()
}

func historicalYumaPurpose(purpose string) bool {
	return purpose == "native-yuma-meta" || purpose == "native-yuma-settings" || purpose == "native-yuma-node" || purpose == "native-yuma-weights" || purpose == "native-yuma-bonds"
}

// A decoder instance retains the first error and checks every vector's finite
// dimensions before allocating a larger semantic representation.
type nativeYumaDecoder struct {
	record historicalReplayObservation
	err    error
}

func (self *nativeYumaDecoder) scalar(name string, width int) uint64 {
	value, err := nativeCaptureUint(self.record, name, width)
	self.err = errors.Join(self.err, err)
	return value
}

func (self *nativeYumaDecoder) flag(name string) bool {
	value := self.scalar(name, 1)
	if value > 1 {
		self.err = errors.Join(self.err, errors.New("native Yuma flag is not canonical"))
	}
	return value == 1
}

func (self *nativeYumaDecoder) bytes(name string, width int) []byte {
	value, err := nativeCapture(self.record, name, width)
	self.err = errors.Join(self.err, err)
	return value
}

func (self *nativeYumaDecoder) vector(name string, width int) []uint64 {
	raw := self.bytes(name, -1)
	if len(raw)%width != 0 || len(raw)/width > rootCensusLimit {
		self.err = errors.Join(self.err, errors.New("native Yuma vector exceeds original dimension"))
		return nil
	}
	result := make([]uint64, len(raw)/width)
	for index := range result {
		if width == 2 {
			result[index] = uint64(binary.LittleEndian.Uint16(raw[index*width:]))
		} else {
			result[index] = binary.LittleEndian.Uint64(raw[index*width:])
		}
	}
	return result
}

func (self *nativeYumaDecoder) shares(parent bool) []nativeYumaShare {
	label := "child"
	if parent {
		label = "parent"
	}
	proportions := self.vector(label+"-proportions", 8)
	hotkeys := self.bytes(label+"-hotkeys", len(proportions)*32)
	alpha, tao := make([]uint64, len(proportions)), make([]uint64, len(proportions))
	if parent {
		alpha = self.vector("parent-alpha", 8)
		tao = self.vector("parent-tao", 8)
	}
	if self.err != nil || len(alpha) != len(proportions) || len(tao) != len(proportions) {
		self.err = errors.Join(self.err, errors.New("native Yuma inherited stake identities differ"))
		return nil
	}
	seen := map[string]bool{}
	result := make([]nativeYumaShare, len(proportions))
	for index, proportion := range proportions {
		hotkey := "0x" + hex.EncodeToString(hotkeys[index*32:(index+1)*32])
		if seen[hotkey] {
			self.err = errors.Join(self.err, errors.New("native Yuma inherited stake identity repeats"))
		}
		seen[hotkey] = true
		result[index] = nativeYumaShare{Hotkey: hotkey, Proportion: proportion, Alpha: alpha[index], Tao: tao[index]}
	}
	return result
}

// Require explicit empty edge records too. Sorted unique columns match the
// supported original sparse branch; a different layout remains unclosed.
func (self *nativeYumaDecoder) edges(count uint16) []nativeYumaEdge {
	columns, values := self.vector("columns", 2), self.vector("values", 2)
	if len(columns) != len(values) {
		self.err = errors.Join(self.err, errors.New("native Yuma sparse edge dimensions differ"))
		return nil
	}
	result := make([]nativeYumaEdge, len(columns))
	for index, column := range columns {
		if column >= uint64(count) || index > 0 && column <= columns[index-1] {
			self.err = errors.Join(self.err, errors.New("native Yuma sparse columns are outside the admitted unique ordered domain"))
		}
		result[index] = nativeYumaEdge{Column: uint16(column), Value: uint16(values[index])}
	}
	return result
}

// The enclosing original replay derives this join from actual storage reads.
// These checks bind its use to the same epoch and dense recipient census; they
// do not turn a caller-supplied hash into independent execution authority.
func nativeYumaJoinedEpochTotal(epoch historicalReplayObservation, joined *nativeEpochInputProvenance, recipients []nativeExecutionRecipient) (uint64, error) {
	if joined == nil || joined.Schema != nativeEpochStorageLayoutSchema || epoch.Ordinal == 0 || joined.EpochObservationOrdinal != epoch.Ordinal || epoch.Purpose != "native-epoch" || epoch.Operation != "host" || epoch.Native == nil || epoch.Native.ExecutionPhaseHex == nil || *epoch.Native.ExecutionPhaseHex != "0x02" || len(joined.DrainOrdinals) != 3 || len(recipients) > rootCensusLimit || len(joined.UidReadOrdinals) != len(recipients) {
		return 0, errors.New("native Yuma storage join differs from its original epoch or recipient census")
	}
	for _, field := range epoch.Native.Memory {
		switch field.Name {
		case "total-alpha", "hotkeys", "uids", "subnet-epoch":
			return 0, errors.New("native Yuma storage join cannot substitute legacy memory fields")
		}
	}
	if len(joined.TotalAlpha) == 0 || len(joined.TotalAlpha) > 20 {
		return 0, errors.New("native Yuma original joined total exceeds u64")
	}
	total, err := strconv.ParseUint(joined.TotalAlpha, 10, 64)
	if err != nil || strconv.FormatUint(total, 10) != joined.TotalAlpha {
		return 0, errors.New("native Yuma original joined total is not canonical u64")
	}
	seen := map[uint64]bool{}
	last := uint64(0)
	for _, ordinal := range joined.DrainOrdinals {
		if ordinal <= last || ordinal >= epoch.Ordinal {
			return 0, errors.New("native Yuma original drain order differs")
		}
		seen[ordinal], last = true, ordinal
	}
	if joined.EpochWriteOrdinal <= last || joined.EpochWriteOrdinal >= epoch.Ordinal || seen[joined.EpochWriteOrdinal] {
		return 0, errors.New("native Yuma original epoch counter write differs")
	}
	seen[joined.EpochWriteOrdinal] = true
	for index, ordinal := range joined.UidReadOrdinals {
		if ordinal <= joined.EpochWriteOrdinal || ordinal >= epoch.Ordinal || seen[ordinal] || recipients[index].Uid != uint16(index) {
			return 0, errors.New("native Yuma original UID read census is not dense and distinct")
		}
		seen[ordinal] = true
	}
	return total, nil
}

// Every captured record precedes the matching original epoch output. Multiple
// mechanisms or incomplete input shapes require a separately admitted schema.
func decodeNativeYuma(policy nativeYumaPolicy, netuid uint16, boundary economicEmissionBoundary, records []historicalReplayObservation, epoch historicalReplayObservation, joined *nativeEpochInputProvenance, recipients []nativeExecutionRecipient) (nativeYumaInput, error) {
	input := nativeYumaInput{Netuid: netuid}
	if len(records) < 2 || len(records) > 2+3*rootCensusLimit || records[0].Purpose != "native-yuma-meta" || records[1].Purpose != "native-yuma-settings" {
		return input, errors.New("native Yuma complete input census is absent")
	}
	last := uint64(0)
	for _, record := range records {
		if record.Ordinal == 0 || record.Ordinal <= last || record.Ordinal >= epoch.Ordinal {
			return input, errors.New("native Yuma inputs do not precede their exact original epoch")
		}
		actual, err := nativeCaptureUint(record, "netuid", 2)
		if err != nil || actual != uint64(netuid) {
			return input, errors.New("native Yuma input substituted original subnet/phase")
		}
		last = record.Ordinal
	}
	d := nativeYumaDecoder{record: records[0]}
	input.Count = uint16(d.scalar("uid-count", 2))
	input.CurrentBlock = d.scalar("current-block", 8)
	input.Tempo = d.scalar("tempo", 8)
	input.ActivityCutoff = d.scalar("activity-cutoff", 8)
	input.LastStep = d.scalar("last-step", 8)
	input.MinimumStake = d.scalar("minimum-stake", 8)
	input.OwnerUid = uint16(d.scalar("owner-uid", 2))
	input.TaoWeight = d.scalar("tao-weight", 8)
	input.Kappa = uint16(d.scalar("kappa", 2))
	input.BondsPenalty = uint16(d.scalar("bonds-penalty", 2))
	input.MovingAverage = d.scalar("moving-average", 8)
	if d.err != nil {
		return input, d.err
	}
	if input.Count == 0 || input.Count > rootCensusLimit || input.CurrentBlock != boundary.Number || input.OwnerUid != 65535 && input.OwnerUid >= input.Count || input.MovingAverage > 1000000 || len(records) != 2+3*int(input.Count) {
		return input, errors.New("native Yuma original branch, boundary or row census is unclosed")
	}
	d = nativeYumaDecoder{record: records[1]}
	input.Yuma3 = d.flag("yuma3")
	input.LiquidAlpha = d.flag("liquid-alpha")
	input.CommitReveal = d.flag("commit-reveal")
	input.ConsensusMode = uint8(d.scalar("consensus-mode", 1))
	input.AlphaLow = uint16(d.scalar("alpha-low", 2))
	input.AlphaHigh = uint16(d.scalar("alpha-high", 2))
	input.Steepness = int16(d.scalar("steepness", 2))
	for _, value := range d.vector("previous-consensus", 2) {
		input.PreviousConsensus = append(input.PreviousConsensus, uint16(value))
	}
	if d.err != nil {
		return input, d.err
	}
	if input.ConsensusMode > 2 || input.AlphaLow > input.AlphaHigh {
		return input, errors.New("native Yuma liquid-alpha branch is unclosed")
	}
	count := int(input.Count)
	if joined != nil && len(recipients) != count {
		return input, errors.New("native Yuma storage join omitted an original recipient")
	}
	input.Nodes = make([]nativeYumaNode, count)
	input.Weights = make([][]nativeYumaEdge, count)
	input.Bonds = make([][]nativeYumaEdge, count)
	edges := uint64(0)
	seen := map[string]bool{}
	for index := 0; index < count; index++ {
		d = nativeYumaDecoder{record: records[2+index]}
		if d.record.Purpose != "native-yuma-node" || d.scalar("uid", 2) != uint64(index) {
			return input, errors.New("native Yuma original UID census is incomplete")
		}
		node := nativeYumaNode{Uid: uint16(index), Hotkey: "0x" + hex.EncodeToString(d.bytes("hotkey", 32)), Registered: d.scalar("registered", 8), LastUpdate: d.scalar("last-update", 8), Permit: d.flag("permit"), Alpha: d.scalar("alpha", 8), Tao: d.scalar("tao", 8), CommitBlock: d.scalar("commit-block", 8)}
		node.Parents, node.Children = d.shares(true), d.shares(false)
		if d.err != nil {
			return input, d.err
		}
		if !rootCanonicalHash(node.Hotkey) || seen[node.Hotkey] || node.Registered > boundary.Number || node.LastUpdate > boundary.Number {
			return input, errors.New("native Yuma original registration generation changed")
		}
		seen[node.Hotkey] = true
		input.Nodes[index] = node
		edges += uint64(len(node.Parents) + len(node.Children))
		for offset, purpose := range []string{"native-yuma-weights", "native-yuma-bonds"} {
			d = nativeYumaDecoder{record: records[2+count*(offset+1)+index]}
			if d.record.Purpose != purpose || d.scalar("uid", 2) != uint64(index) {
				return input, errors.New("native Yuma omitted or repeated an original matrix row")
			}
			row := d.edges(input.Count)
			if d.err != nil {
				return input, d.err
			}
			if offset == 0 {
				input.Weights[index] = row
			} else {
				input.Bonds[index] = row
			}
			edges += uint64(len(row))
		}
		if edges > policy.MaximumEdges {
			return input, errors.Join(errMonitorEconomicCapacity, errors.New("native Yuma edge census exceeds independent finite authority"))
		}
	}
	registered, err := nativeCaptureVector(epoch, "registered", count)
	if err != nil {
		return input, err
	}
	var hotkeys []byte
	if joined == nil {
		hotkeys, err = nativeCapture(epoch, "hotkeys", count*32)
		if err != nil {
			return input, err
		}
	}
	for index, node := range input.Nodes {
		if registered[index] != node.Registered {
			return input, errors.New("native Yuma input/output UID generation differs")
		}
		if joined != nil {
			recipient := recipients[index]
			if recipient.Uid != node.Uid || recipient.Hotkey != node.Hotkey || recipient.Registered != node.Registered {
				return input, errors.New("native Yuma input differs from its original storage-joined UID generation")
			}
		} else if fmt.Sprintf("0x%x", hotkeys[index*32:(index+1)*32]) != node.Hotkey {
			return input, errors.New("native Yuma input/output UID generation differs")
		}
	}
	return input, policy.admitWorkload(input)
}
