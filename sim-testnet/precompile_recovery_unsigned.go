// Unsigned recovery checks close the crash window between persisting a signed
// transaction and appending its broadcast journal record.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/core/types"
)

// Previously journaled transactions remain historical; any unjournaled probe
// signature prevents replacing the pending operation, even without a receipt.
func requireUnsignedPrecompileRecovery(stateDir string, evidence *PrecompileConformanceEvidence, entries []JournalEntry) error {
	if evidence == nil || evidence.Recovery == nil || len(evidence.Recovery.Steps) != 1 {
		return errors.New("probe gas revision unsigned source is absent")
	}
	prior := evidence.Recovery.Steps[0].Action
	known := map[string]bool{}
	for _, entry := range entries {
		if strings.HasPrefix(entry.ActionID, precompileRecoveryActionPrefix) && (entry.ActionID != prior.ID || entry.IntentHash != prior.IntentHash || entry.TransactionHash != "" || entry.Signer != "" || entry.Nonce != "" || (entry.Stage != StageIntent && entry.Stage != StageFailed)) {
			return errors.New("probe gas revision cannot supersede existing recovery signatures or another action")
		}
		if entry.TransactionHash != "" {
			known[entry.TransactionHash] = true
		}
	}
	files, err := os.ReadDir(filepath.Join(stateDir, "transactions"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(files) > 20000 {
		return errors.New("probe gas revision signed transaction census exceeds its existing bound")
	}
	request := evidence.Recovery.Authorization.Request
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".rlp") {
			continue
		}
		hash := "0x" + strings.TrimSuffix(file.Name(), ".rlp")
		if !validCanonicalHashHex(hash) {
			return errors.New("probe gas revision transaction filename is not canonical")
		}
		raw, err := readValidatorEvidenceHistoricalFile(stateDir, "transactions/"+file.Name(), 64*1024)
		if err != nil {
			return err
		}
		var tx types.Transaction
		if err := tx.UnmarshalBinary(raw); err != nil || tx.Hash().Hex() != hash {
			return errors.Join(errors.New("probe gas revision retained signature differs from its filename"), err)
		}
		if known[hash] || tx.To() == nil || *tx.To() != request.Probe {
			continue
		}
		signer, err := types.Sender(types.LatestSignerForChainID(tx.ChainId()), &tx)
		if err != nil {
			return err
		}
		if signer == request.Deployer {
			return errors.New("probe gas revision found an unjournaled signed probe transaction")
		}
	}
	return nil
}
