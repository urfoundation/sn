// Translate the signed protocol records into generated Solidity tuples. These
// helpers authorize calldata only: sending, spending, finalized inclusion,
// historical validator eligibility and public proof replay remain separate.
package stabi

import (
	"github.com/ethereum/go-ethereum/common"

	"github.com/urfoundation/sn/v2026/protocol"
)

// Keep all signed fixed-width fields, including the two independent snapshot
// clocks and the operator-scoped migration prefix. No signature enters a tuple.
func ValidatorEvidenceActivationRecordFromProtocol(record protocol.ValidatorEvidenceActivation) ValidatorEvidenceActivationRecord {
	return ValidatorEvidenceActivationRecord{
		Domain: ValidatorEvidenceActivationDomain{
			ChainId: record.Domain.ChainID, GenesisHash: record.Domain.GenesisHash,
			Netuid: record.Domain.Netuid, Coordinator: common.Address(record.Domain.Coordinator),
			SettlementVault:  common.Address(record.Domain.SettlementVault),
			DeploymentIdHash: record.Domain.DeploymentIDHash, PolicyHash: record.Domain.PolicyHash,
			Epoch: record.Domain.Epoch,
		},
		Hotkey: record.Hotkey, NoId: record.NoID, Vpk: record.VPK,
		FirstSequence: record.FirstSequence, PriorRoot: record.PriorRoot,
		NativeBlock: record.NativeBlock, NativeHash: record.NativeHash,
		EvmBlock: record.EVMBlock, EvmHash: record.EVMHash,
	}
}

// Decoding a contract return value is not validation or an inclusion proof.
// The caller must compare this complete record to independent authority.
func (self ValidatorEvidenceActivationRecord) ProtocolRecord() protocol.ValidatorEvidenceActivation {
	return protocol.ValidatorEvidenceActivation{
		Domain: protocol.ValidatorEvidenceActivationDomain{
			ChainID: self.Domain.ChainId, GenesisHash: self.Domain.GenesisHash,
			Netuid: self.Domain.Netuid, Coordinator: [20]byte(self.Domain.Coordinator),
			SettlementVault:  [20]byte(self.Domain.SettlementVault),
			DeploymentIDHash: self.Domain.DeploymentIdHash, PolicyHash: self.Domain.PolicyHash,
			Epoch: self.Domain.Epoch,
		},
		Hotkey: self.Hotkey, NoID: self.NoId, VPK: self.Vpk,
		FirstSequence: self.FirstSequence, PriorRoot: self.PriorRoot,
		NativeBlock: self.NativeBlock, NativeHash: self.NativeHash,
		EVMBlock: self.EvmBlock, EVMHash: self.EvmHash,
	}
}

// Audit coordinates, not an outcome-derived subject hash, belong in the ABI.
// Solidity recomputes the same signed subject hash from these coordinates.
func ValidatorEvidenceHeaderFromProtocol(header protocol.ValidatorEvidenceHeader) ValidatorEvidenceHeader {
	return ValidatorEvidenceHeader{
		Domain: ValidatorEvidenceDomain{
			ChainId: header.Domain.ChainID, GenesisHash: header.Domain.GenesisHash,
			Netuid: header.Domain.Netuid, Coordinator: common.Address(header.Domain.Coordinator),
			SettlementVault:  common.Address(header.Domain.SettlementVault),
			DeploymentIdHash: header.Domain.DeploymentIDHash, PolicyHash: header.Domain.PolicyHash,
			ActivationEpoch: header.Domain.ActivationEpoch, ActivationHash: header.Domain.ActivationHash,
		},
		Hotkey: header.Hotkey, NoId: header.NoID, Epoch: header.Epoch, Kind: header.Kind,
		Subject: ValidatorEvidenceSubject{ObservationEpoch: header.Subject.ObservationEpoch, NativeEpoch: header.Subject.NativeEpoch},
		Vpk:     header.VPK, BoundaryBlock: header.BoundaryBlock, BoundaryHash: header.BoundaryHash,
		CensusHash: header.CensusHash, PayloadHash: header.PayloadHash, PayloadBytes: header.PayloadBytes,
	}
}

// Preserve every field before a caller verifies the exact domain, window,
// stored activation, signatures and public proof bytes.
func (self ValidatorEvidenceHeader) ProtocolHeader() protocol.ValidatorEvidenceHeader {
	return protocol.ValidatorEvidenceHeader{
		Domain: protocol.ValidatorEvidenceDomain{
			ChainID: self.Domain.ChainId, GenesisHash: self.Domain.GenesisHash,
			Netuid: self.Domain.Netuid, Coordinator: [20]byte(self.Domain.Coordinator),
			SettlementVault:  [20]byte(self.Domain.SettlementVault),
			DeploymentIDHash: self.Domain.DeploymentIdHash, PolicyHash: self.Domain.PolicyHash,
			ActivationEpoch: self.Domain.ActivationEpoch, ActivationHash: self.Domain.ActivationHash,
		},
		Hotkey: self.Hotkey, NoID: self.NoId, Epoch: self.Epoch, Kind: self.Kind,
		Subject: protocol.ValidatorEvidenceSubject{ObservationEpoch: self.Subject.ObservationEpoch, NativeEpoch: self.Subject.NativeEpoch},
		VPK:     self.Vpk, BoundaryBlock: self.BoundaryBlock, BoundaryHash: self.BoundaryHash,
		CensusHash: self.CensusHash, PayloadHash: self.PayloadHash, PayloadBytes: self.PayloadBytes,
	}
}

// Both keys authorize the exact independently expected activation. A relayer
// cannot substitute its own migration prefix merely by signing that prefix.
func PackValidatorEvidenceActivation(expected, record protocol.ValidatorEvidenceActivation, vpkSignature, hotkeySignature []byte) ([]byte, error) {
	if err := record.Verify(expected, vpkSignature, hotkeySignature); err != nil {
		return nil, err
	}
	return NewSTValidatorEvidence().PackPublishActivation(ValidatorEvidenceActivationRecordFromProtocol(record), vpkSignature, hotkeySignature), nil
}

// The expected deployment and window must be independently authenticated.
// This produces bounded calldata without giving the helper custody or a key.
func PackValidatorEvidenceCommitment(expected protocol.ValidatorEvidenceDomain, window protocol.ValidatorEvidenceWindow, header protocol.ValidatorEvidenceHeader, vpkSignature, hotkeySignature []byte) ([]byte, error) {
	if err := header.Verify(expected, window, vpkSignature, hotkeySignature); err != nil {
		return nil, err
	}
	return NewSTValidatorEvidence().PackCommitEvidence(ValidatorEvidenceHeaderFromProtocol(header), vpkSignature, hotkeySignature), nil
}
